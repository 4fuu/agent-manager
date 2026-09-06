package supervisor

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/4fuu/agent-manager/internal/backend"
	"github.com/4fuu/agent-manager/internal/manager"
	uv "github.com/charmbracelet/ultraviolet"
)

func fixture(t *testing.T, image string) (*Supervisor, string) {
	t.Helper()
	dir := t.TempDir()
	s, e := New(dir, backend.Fake{Dir: filepath.Join(dir, "vms")})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	if e = s.Project(manager.Project{Name: "URI", URL: "https://github.com/4fuu/uri-agent", Image: image, Ref: "main", Command: "uri-agent"}); e != nil {
		t.Fatal(e)
	}
	id, e := s.Create(s.State().Projects[0].ID, "main", image)
	if e != nil {
		t.Fatal(e)
	}
	return s, id
}
func await(t *testing.T, s *Supervisor, id, status string) manager.Instance {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		in := s.instance(id)
		var got manager.Instance
		if in != nil {
			got = *in
		}
		l := s.live[id]
		busy := l != nil && l.busy
		s.mu.Unlock()
		if !busy && got.Status == status {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("instance did not reach %s: %+v", status, s.State())
	return manager.Instance{}
}
func act(t *testing.T, s *Supervisor, id, a, status string) manager.Instance {
	t.Helper()
	if e := s.Action(id, a); e != nil {
		t.Fatal(e)
	}
	return await(t, s, id, status)
}
func TestFailureShellRetryStopResume(t *testing.T) {
	s, id := fixture(t, "fixture:fail-setup")
	in := act(t, s, id, "start", "failed")
	if !in.Cloned || in.SetupDone || in.RuntimeID == "" {
		t.Fatal("failure lost lifecycle state", in)
	}
	f, e := s.Frame(id)
	if e != nil || f.Live || !strings.Contains(f.Logs, "exited 17") {
		t.Fatal("failure not inspectable", f, e)
	}
	act(t, s, id, "shell", "shell")
	if s.State().Instances[0].SetupDone {
		t.Fatal("shell incorrectly completed setup")
	}
	in = act(t, s, id, "retry", "running")
	if !in.SetupDone {
		t.Fatal("retry did not complete setup")
	}
	f, _ = s.Frame(id)
	log := f.Logs
	act(t, s, id, "stop", "stopped")
	if _, e := os.Stat(filepath.Join(s.Dir, "vms", "am-"+id)); e != nil {
		t.Fatal("stop deleted disk")
	}
	resumed := act(t, s, id, "start", "running")
	if resumed.RuntimeID != in.RuntimeID {
		t.Fatal("resume replaced guest")
	}
	f, _ = s.Frame(id)
	if f.Logs != log {
		t.Fatal("resume reran clone/setup")
	}
}
func TestUIReattachAndSupervisorReconcile(t *testing.T) {
	s, id := fixture(t, "fixture:ok")
	in := act(t, s, id, "start", "running")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	// Independent clients are stateless. Neither closing transport owns the PTY.
	call := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"Action":"frame","ID":"`+id+`"}`))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if !strings.Contains(call().Body.String(), `"Live":true`) {
		t.Fatal("first client not attached")
	}
	if !strings.Contains(call().Body.String(), `"Live":true`) {
		t.Fatal("second client lost PTY")
	}
	s.Close()
	reopened, e := New(s.Dir, backend.Fake{Dir: filepath.Join(s.Dir, "vms")})
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if reopened.State().Instances[0].Status != "disconnected" {
		t.Fatal("did not reconcile running guest")
	}
	got := act(t, reopened, id, "start", "running")
	if got.RuntimeID != in.RuntimeID || !got.SetupDone {
		t.Fatal("reconnect reset durable state")
	}
}
func TestIndependentInstancesAndSnapshot(t *testing.T) {
	s, id := fixture(t, "fixture:ok")
	pid := s.State().Projects[0].ID
	id2, e := s.Create(pid, "feature/second", "fixture:second")
	if e != nil {
		t.Fatal(e)
	}
	p := s.State().Projects[0]
	p.Command = "changed command"
	p.Environment = map[string]string{"TERM": "vt100"}
	if e = s.Project(p); e != nil {
		t.Fatal(e)
	}
	p.Environment["TERM"] = "caller mutation"
	if s.State().Projects[0].Environment["TERM"] != "vt100" {
		t.Fatal("project retains caller-owned environment")
	}
	for _, in := range s.State().Instances {
		if in.Config.Environment["TERM"] != "xterm-256color" {
			t.Fatal("project edit mutated existing instance environment")
		}
	}
	first := act(t, s, id, "start", "running")
	second := act(t, s, id2, "start", "running")
	if first.RuntimeID == second.RuntimeID || first.Config.Command != "uri-agent" || second.Config.Ref != "feature/second" {
		t.Fatal("instances share configuration/state")
	}
	act(t, s, id, "stop", "stopped")
	if f, _ := s.Frame(id2); !f.Live {
		t.Fatal("stopping one stopped another")
	}
	if e = s.Action(id, "delete"); e != nil {
		t.Fatal(e)
	}
	s.wg.Wait()
	if len(s.State().Instances) != 1 {
		t.Fatal("delete did not remove metadata")
	}
	if _, e = os.Stat(filepath.Join(s.Dir, "vms", "am-"+id)); !os.IsNotExist(e) {
		t.Fatal("delete retained disk")
	}
	if e = s.DeleteProject(pid); e == nil {
		t.Fatal("deleted project with live instances")
	}
}
func TestDeleteNeverBootedAndMissingIdentity(t *testing.T) {
	s, id := fixture(t, "fixture:ok")
	if e := s.Action(id, "delete"); e != nil {
		t.Fatal(e)
	}
	s.wg.Wait()
	if len(s.State().Instances) != 0 {
		t.Fatal("new metadata cannot be deleted")
	}
	id, e := s.Create(s.State().Projects[0].ID, "main", "")
	if e != nil {
		t.Fatal(e)
	}
	act(t, s, id, "start", "running")
	act(t, s, id, "stop", "stopped")
	if e = os.Remove(filepath.Join(s.Dir, "vms", "am-"+id)); e != nil {
		t.Fatal(e)
	}
	act(t, s, id, "start", "failed")
	if _, e = os.Stat(filepath.Join(s.Dir, "vms", "am-"+id)); !os.IsNotExist(e) {
		t.Fatal("silently recreated missing disk")
	}
}

type recordingBackend struct {
	backend.Fake
	mu       sync.Mutex
	p        *recordingProcess
	commands []string
}
type recordingVM struct {
	backend.VM
	b *recordingBackend
}

func (b *recordingBackend) Create(c context.Context, n string, p manager.Project) (backend.VM, error) {
	v, e := b.Fake.Create(c, n, p)
	return &recordingVM{v, b}, e
}
func (v *recordingVM) Run(c context.Context, cwd, cmd string, w io.Writer) error {
	v.b.mu.Lock()
	v.b.commands = append(v.b.commands, cwd+":"+cmd)
	v.b.mu.Unlock()
	return v.VM.Run(c, cwd, cmd, w)
}
func (v *recordingVM) Terminal(c context.Context, cwd, cmd string, w io.Writer) (backend.Process, error) {
	p, e := v.VM.Terminal(c, cwd, cmd, w)
	if e != nil {
		return nil, e
	}
	r := &recordingProcess{Process: p}
	v.b.mu.Lock()
	v.b.p = r
	v.b.mu.Unlock()
	return r, nil
}

type recordingProcess struct {
	backend.Process
	mu         sync.Mutex
	input      bytes.Buffer
	rows, cols int
}

func (p *recordingProcess) Input(b []byte) error {
	p.mu.Lock()
	p.input.Write(b)
	p.mu.Unlock()
	return p.Process.Input(b)
}
func (p *recordingProcess) Resize(r, c int) error {
	p.mu.Lock()
	p.rows, p.cols = r, c
	p.mu.Unlock()
	return p.Process.Resize(r, c)
}
func TestTerminalInputPasteMouseResizeAndConcurrentFrames(t *testing.T) {
	s, id := fixture(t, "fixture:ok")
	b := &recordingBackend{Fake: backend.Fake{Dir: filepath.Join(s.Dir, "vms")}}
	s.backend = b
	act(t, s, id, "start", "running")
	k := uv.Key{Code: 'x', Text: "x"}
	paste := "hello pasted 世界"
	m := uv.Mouse{X: 2, Y: 3, Button: uv.MouseLeft}
	for _, in := range []Input{{ID: id, Key: &k}, {ID: id, Paste: &paste}, {ID: id, Mouse: &m, MouseKind: "press"}, {ID: id, Rows: 30, Cols: 100}} {
		if e := s.Input(in); e != nil {
			t.Fatal(e)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		p := b.p
		b.mu.Unlock()
		p.mu.Lock()
		text := p.input.String()
		r, c := p.rows, p.cols
		p.mu.Unlock()
		if strings.Contains(text, "\x1b[200~"+paste+"\x1b[201~") && strings.Contains(text, "\x1b[<0;3;4M") && strings.Contains(text, "x") && r == 30 && c == 100 {
			break
		}
		time.Sleep(time.Millisecond)
		if time.Now().After(deadline) {
			t.Fatalf("forwarding mismatch %q %dx%d", text, r, c)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				_, _ = s.Frame(id)
				_ = s.Input(Input{ID: id, Key: &k})
			}
		}()
	}
	wg.Wait()
	f, e := s.Frame(id)
	if e != nil || f.Width != 100 || f.Height != 30 {
		t.Fatal("frame did not resize", f.Width, f.Height, e)
	}
}
func TestGuestCommandsAndSecretRedaction(t *testing.T) {
	s, id := fixture(t, "fixture:ok")
	file := filepath.Join(t.TempDir(), "auth.json")
	secret := "private-value-split-across-chunks"
	if e := os.WriteFile(file, []byte(`{"token":"`+secret+`"}`), 0600); e != nil {
		t.Fatal(e)
	}
	w := s.log(id, manager.Project{Mappings: []manager.Mapping{{Host: file}}})
	_, _ = w.Write([]byte("before " + secret[:12]))
	_, _ = w.Write([]byte(secret[12:] + " after\n"))
	w.Flush()
	f, _ := s.Frame(id)
	if strings.Contains(f.Logs, secret) || !strings.Contains(f.Logs, "[REDACTED]") {
		t.Fatal("stream redaction failed")
	}
	cmd := cloneCommand(manager.Project{URL: "https://github.com/a/b", Ref: "feature/'quote", AuthFile: file})
	if strings.Contains(cmd, file) || !strings.Contains(cmd, "GIT_ASKPASS") || !strings.Contains(cmd, "checkout --detach FETCH_HEAD") {
		t.Fatal("unsafe clone contract", cmd)
	}
	if !strings.Contains(setupScript(false), "flock -n") || strings.Contains(setupScript(false), "rm -f /workspace/.manager-setup-done") || !strings.Contains(setupScript(true), "rm -f /workspace/.manager-setup-done") {
		t.Fatal("setup marker/retry contract")
	}
	if !strings.Contains(terminalCommand(manager.Project{Command: "uri-agent"}, false), "tmux -L manager new-session -A") {
		t.Fatal("guest PTY reconnect contract")
	}
}
