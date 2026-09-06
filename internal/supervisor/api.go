package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/color"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/4fuu/agent-manager/internal/privatefs"
)

type Cell struct {
	Text      string
	FG, BG    string
	Attr      uint8
	Underline bool
	Width     int
}
type Frame struct {
	Width, Height, CursorX, CursorY int
	Cells                           []Cell
	Logs                            string
	Live                            bool
	Title                           string
}

func rgb(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	const h = "0123456789abcdef"
	out := []byte{'#', 0, 0, 0, 0, 0, 0}
	for i, v := range []uint32{r >> 8, g >> 8, b >> 8} {
		out[1+i*2] = h[v>>4]
		out[2+i*2] = h[v&15]
	}
	return string(out)
}
func (s *Supervisor) Frame(id string) (Frame, error) {
	return s.PaneFrame(id, "")
}
func (s *Supervisor) PaneFrame(id, paneID string) (Frame, error) {
	s.mu.Lock()
	in := s.instance(id)
	if in == nil {
		s.mu.Unlock()
		return Frame{}, errors.New("instance not found")
	}
	l := s.live[id]
	if paneID == "" && len(in.Panes) > 0 {
		paneID = in.Panes[0].ID
	}
	busy := l != nil && l.busy
	s.mu.Unlock()
	f := Frame{}
	if l != nil && !busy {
		l.mu.Lock()
		defer l.mu.Unlock()
		pane := l.panes[paneID]
		if pane != nil {
			if t := pane.term; t != nil {
				t.mu.Lock()
				defer t.mu.Unlock()
				f.Title = t.title
				f.Live = pane.process != nil
				f.Width = t.Width()
				f.Height = t.Height()
				pos := t.CursorPosition()
				f.CursorX = pos.X
				f.CursorY = pos.Y
				for y := 0; y < f.Height; y++ {
					for x := 0; x < f.Width; x++ {
						cell := Cell{}
						if c := t.CellAt(x, y); c != nil {
							cell = Cell{Text: c.Content, FG: rgb(c.Style.Fg), BG: rgb(c.Style.Bg), Attr: c.Style.Attrs, Underline: c.Style.Underline != 0, Width: c.Width}
						}
						f.Cells = append(f.Cells, cell)
					}
				}
			}
		}
	}
	// Preview is bounded; complete redacted logs remain in the private state directory.
	file, e := os.Open(filepath.Join(s.Dir, id+".log"))
	if e == nil {
		defer file.Close()
		st, _ := file.Stat()
		if st.Size() > 128*1024 {
			_, _ = file.Seek(-128*1024, io.SeekEnd)
		}
		b, _ := io.ReadAll(file)
		f.Logs = string(b)
	}
	return f, nil
}

type Request struct {
	Action, ID, Ref, Image, PaneID, Name, Command string
	Width                                         int
	Project                                       manager.Project
	Profile                                       manager.ImageProfile
	Mappings                                      []manager.Mapping
	Input                                         Input
}
type Reply struct {
	Error string
	ID    string
	State manager.State
	Frame Frame
}

func (s *Supervisor) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" || r.URL.Path != "/rpc" {
			http.Error(w, "not found", 404)
			return
		}
		var req Request
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&req); e != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		var out Reply
		var e error
		switch req.Action {
		case "state":
			out.State = s.State()
		case "frame":
			out.Frame, e = s.PaneFrame(req.ID, req.PaneID)
		case "project":
			e = s.Project(req.Project)
		case "defaults":
			e = s.Defaults(req.Mappings)
		case "delete-project":
			e = s.DeleteProject(req.ID)
		case "create":
			out.ID, e = s.CreateProfile(req.ID, req.Image)
		case "image":
			e = s.Image(req.Profile)
		case "delete-image":
			e = s.DeleteImage(req.ID)
		case "download-image":
			e = s.DownloadImage(req.ID)
		case "cancel-image-download":
			e = s.CancelImageDownload(req.ID)
		case "add-pane":
			out.ID, e = s.AddPane(req.ID, req.Command)
		case "close-pane":
			e = s.ClosePane(req.ID, req.PaneID)
		case "rename":
			e = s.Rename(req.ID, req.Name)
		case "width":
			e = s.Width(req.ID, req.PaneID, req.Width)
		case "input":
			if req.Input.PaneID == "" {
				req.Input.PaneID = req.PaneID
			}
			e = s.Input(req.Input)
		default:
			e = s.Action(req.ID, req.Action)
		}
		if e != nil {
			out.Error = e.Error()
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}

// Serve holds an OS lock for its lifetime. Closing the handle releases the lock
// even after a crash; closing a TUI never closes this server.
func Serve(ctx context.Context, dir string, newSupervisor func() (*Supervisor, error)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if e := privatefs.EnsureDir(dir); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(dir, "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = lockFile(lock); e != nil {
		return errors.New("supervisor already owns this state directory")
	}
	ln, e := listen(dir)
	if e != nil {
		return e
	}
	defer ln.Close()
	s, e := newSupervisor()
	if e != nil {
		return e
	}
	defer s.Close()
	rpc := s.Handler()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/health":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "POST" && r.URL.Path == "/shutdown":
			w.WriteHeader(http.StatusNoContent)
			cancel()
		default:
			rpc.ServeHTTP(w, r)
		}
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	defer server.Close()
	done := make(chan struct{})
	defer close(done)
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = server.Shutdown(shutdownCtx)
		case <-done:
		}
	}()
	e = server.Serve(ln)
	if errors.Is(e, http.ErrServerClosed) {
		// Let the shutdown response reach the caller before closing connections.
		<-shutdownDone
		return nil
	}
	return e
}

type Client struct{ http *http.Client }

func NewClient(dir string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dial(ctx, dir)
	}}
	return &Client{http: &http.Client{Transport: tr, Timeout: 10 * time.Second}}
}

// Probe and Shutdown use the same private, user-authenticated IPC as the UI.
func (c *Client) Probe(ctx context.Context) error    { return c.lifecycle(ctx, "GET", "/health") }
func (c *Client) Shutdown(ctx context.Context) error { return c.lifecycle(ctx, "POST", "/shutdown") }
func (c *Client) lifecycle(ctx context.Context, method, path string) error {
	defer c.http.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, method, "http://supervisor"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return errors.New("supervisor rejected lifecycle request")
	}
	return nil
}

func (c *Client) Call(req Request) (Reply, error) {
	b, e := json.Marshal(req)
	if e != nil {
		return Reply{}, e
	}
	r, e := c.http.Post("http://supervisor/rpc", "application/json", bytes.NewReader(b))
	if e != nil {
		return Reply{}, errors.New("supervisor unavailable; run agent-manager install (first use) or agent-manager start with the same --state; serve runs in the foreground")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return Reply{}, errors.New("supervisor rejected request")
	}
	var out Reply
	e = json.NewDecoder(r.Body).Decode(&out)
	if e == nil && out.Error != "" {
		e = errors.New(strings.TrimSpace(out.Error))
	}
	return out, e
}
