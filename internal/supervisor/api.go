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
	"golang.org/x/sys/unix"
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
	s.mu.Lock()
	if s.instance(id) == nil {
		s.mu.Unlock()
		return Frame{}, errors.New("instance not found")
	}
	l := s.live[id]
	busy := l != nil && l.busy
	s.mu.Unlock()
	f := Frame{}
	if l != nil && !busy {
		l.mu.Lock()
		defer l.mu.Unlock()
		if t := l.term; t != nil {
			t.mu.Lock()
			defer t.mu.Unlock()
			f.Live = l.process != nil
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
	Action, ID, Ref, Image string
	Project                manager.Project
	Mappings               []manager.Mapping
	Input                  Input
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
			out.Frame, e = s.Frame(req.ID)
		case "project":
			e = s.Project(req.Project)
		case "defaults":
			e = s.Defaults(req.Mappings)
		case "delete-project":
			e = s.DeleteProject(req.ID)
		case "create":
			out.ID, e = s.Create(req.ID, req.Ref, req.Image)
		case "input":
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

// Serve locks the state directory before touching a stale socket. Access is same-user only.
func Serve(ctx context.Context, dir string, newSupervisor func() (*Supervisor, error)) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if e := os.Chmod(dir, 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(dir, "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return errors.New("supervisor already owns this state directory")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	s, e := newSupervisor()
	if e != nil {
		return e
	}
	defer s.Close()
	socket := filepath.Join(dir, "supervisor.sock")
	if e = os.Remove(socket); e != nil && !os.IsNotExist(e) {
		return e
	}
	ln, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer os.Remove(socket)
	defer ln.Close()
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	go func() { <-ctx.Done(); _ = server.Close() }()
	e = server.Serve(ln)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}

type Client struct{ http *http.Client }

func NewClient(dir string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "supervisor.sock"))
	}}
	return &Client{http: &http.Client{Transport: tr, Timeout: 10 * time.Second}}
}
func (c *Client) Call(req Request) (Reply, error) {
	b, e := json.Marshal(req)
	if e != nil {
		return Reply{}, e
	}
	r, e := c.http.Post("http://unix/rpc", "application/json", bytes.NewReader(b))
	if e != nil {
		return Reply{}, errors.New("supervisor unavailable; start agent-manager serve in a separate service")
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
