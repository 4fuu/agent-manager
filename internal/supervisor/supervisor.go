// Package supervisor owns guest processes independently of any UI connection.
package supervisor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/4fuu/agent-manager/internal/backend"
	"github.com/4fuu/agent-manager/internal/manager"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

type live struct {
	mu      sync.Mutex
	vm      backend.VM
	process backend.Process
	term    *screen
	cancel  context.CancelFunc
	busy    bool
	done    chan struct{}
}

// Caller holds l.mu. Unblock terminal replies before closing the transport.
func (l *live) detachTerminal() {
	t, p := l.term, l.process
	l.term = nil
	l.process = nil
	if t != nil {
		_ = t.Close()
	}
	if p != nil {
		_ = p.Close()
	}
}

// Snapshot and output share this lock; the library's CellAt returns borrowed cells.
type screen struct {
	*vt.SafeEmulator
	mu     sync.Mutex
	closed bool
}

func (t *screen) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return 0, io.ErrClosedPipe
	}
	return t.SafeEmulator.Write(b)
}

func (t *screen) Close() error {
	// Close the pipe first: Write may hold mu while replying to a device query.
	err := t.InputPipe().(io.Closer).Close()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	// vt's inherited Close races with its unlocked Read. Closing the exposed
	// input pipe wakes Read without mutating the emulator's internal closed flag.
	return err
}

type Supervisor struct {
	mu      sync.Mutex
	wg      sync.WaitGroup
	closing bool
	Dir     string
	state   manager.State
	backend backend.Backend
	live    map[string]*live
}

func New(dir string, b backend.Backend) (*Supervisor, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	st, err := manager.Load(dir)
	if err != nil {
		return nil, err
	}
	s := &Supervisor{Dir: dir, state: st, backend: b, live: map[string]*live{}}
	// Query persisted runtime state without booting. A live VM is not a live UI PTY.
	for i := range s.state.Instances {
		in := &s.state.Instances[i]
		if in.Status == "new" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		status, e := b.Inspect(ctx, "am-"+in.ID, in.RuntimeID)
		cancel()
		switch {
		case errors.Is(e, backend.ErrNotFound):
			if in.RuntimeID == "" {
				in.Status = "new"
			} else {
				in.Status = "missing"
			}
		case e != nil:
			in.Status = "unavailable"
		case status == "running":
			in.Status = "disconnected"
		default:
			in.Status = "stopped"
		}
	}
	return s, s.save()
}
func (s *Supervisor) save() error {
	err := manager.Save(s.Dir, s.state)
	if err != nil {
		// Reconcile to what is actually on disk after a failed atomic replacement.
		if stored, e := manager.Load(s.Dir); e == nil {
			s.state = stored
		}
	}
	return err
}
func (s *Supervisor) State() manager.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.state)
	var out manager.State
	_ = json.Unmarshal(b, &out)
	return out
}
func id() string {
	var b [12]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func (s *Supervisor) Project(p manager.Project) error {
	if err := manager.ValidateProject(p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == "" {
		p.ID = id()
		s.state.Projects = append(s.state.Projects, p)
	} else {
		found := false
		for i := range s.state.Projects {
			if s.state.Projects[i].ID == p.ID {
				s.state.Projects[i] = p
				found = true
				break
			}
		}
		if !found {
			return errors.New("project not found")
		}
	}
	return s.save()
}
func (s *Supervisor) Defaults(ms []manager.Mapping) error {
	if err := manager.ValidateMappings(ms); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Defaults = ms
	return s.save()
}
func (s *Supervisor) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, in := range s.state.Instances {
		if in.ProjectID == id {
			return errors.New("delete project instances first")
		}
	}
	for i, p := range s.state.Projects {
		if p.ID == id {
			s.state.Projects = append(s.state.Projects[:i], s.state.Projects[i+1:]...)
			return s.save()
		}
	}
	return errors.New("project not found")
}
func (s *Supervisor) Create(project, ref, image string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.state.Projects {
		if p.ID == project {
			p.Ref = ref
			if image != "" {
				p.Image = image
			}
			p.Mappings = manager.MergeMappings(s.state.Defaults, p.Mappings)
			if err := manager.ValidateProject(p); err != nil {
				return "", err
			}
			in := manager.Instance{ID: id(), ProjectID: p.ID, Config: p, Status: "new"}
			s.state.Instances = append(s.state.Instances, in)
			return in.ID, s.save()
		}
	}
	return "", errors.New("project not found")
}
func (s *Supervisor) instance(id string) *manager.Instance {
	for i := range s.state.Instances {
		if s.state.Instances[i].ID == id {
			return &s.state.Instances[i]
		}
	}
	return nil
}
func (s *Supervisor) update(id string, f func(*manager.Instance)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	in := s.instance(id)
	if in == nil {
		return errors.New("instance not found")
	}
	f(in)
	return s.save()
}
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// Clone into a staging directory: interrupted clones never look like ready workspaces.
func cloneCommand(p manager.Project) string {
	auth := "export GIT_TERMINAL_PROMPT=0; "
	if p.AuthFile != "" {
		auth = `export GIT_ASKPASS=/usr/local/bin/manager-git-askpass GIT_TERMINAL_PROMPT=0; `
	}
	ref := ""
	if p.Ref != "" {
		ref = "git -C /workspace/repo.init fetch origin " + quote(p.Ref) + " && git -C /workspace/repo.init checkout --detach FETCH_HEAD && "
	}
	return "set -eu; " + auth + "mkdir -p /workspace; if [ ! -d /workspace/repo/.git ]; then rm -rf /workspace/repo.init; git clone -- " + quote(p.URL) + " /workspace/repo.init; " + ref + "mv /workspace/repo.init /workspace/repo; fi"
}

const setupCommand = "if [ -f .agents/setup ]; then if [ -x .agents/setup ]; then ./.agents/setup; else /bin/bash .agents/setup; fi; else printf 'No .agents/setup; no setup required.\\n'; fi"

func setupScript(retry bool) string {
	reset := ""
	if retry {
		reset = "rm -f /workspace/.manager-setup-done; "
	}
	return "mkdir -p /run/manager; exec 9>/run/manager/setup.lock; flock -n 9 || { echo 'Setup still running; inspect shell or stop VM before retry.'; exit 75; }; " + reset + "if [ -f /workspace/.manager-setup-done ]; then echo 'Setup already complete in this environment.'; else " + setupCommand + "; result=$?; if [ \"$result\" -ne 0 ]; then exit \"$result\"; fi; touch /workspace/.manager-setup-done; fi"
}

func terminalCommand(p manager.Project, shell bool) string {
	name, cmd := "agent", p.Command
	if shell {
		name, cmd = "recovery", "/bin/bash -l"
	}
	// tmux lives in the guest. Its server preserves the PTY after an SDK handle is lost.
	return "exec tmux -L manager new-session -A -s " + name + " " + quote(cmd)
}

func (s *Supervisor) Action(id, action string) error {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return errors.New("supervisor shutting down")
	}
	in := s.instance(id)
	if in == nil {
		s.mu.Unlock()
		return errors.New("instance not found")
	}
	l := s.live[id]
	if l == nil {
		l = &live{}
		s.live[id] = l
	}
	if l.busy {
		if action == "stop" {
			l.cancel()
			done := l.done
			s.mu.Unlock()
			go func() { <-done; _ = s.Action(id, "stop") }()
			return nil
		}
		s.mu.Unlock()
		return errors.New("instance operation in progress")
	}
	switch action {
	case "start", "retry", "shell", "stop", "delete":
	default:
		s.mu.Unlock()
		return errors.New("unknown action")
	}
	if action == "retry" && in.Status == "running" {
		s.mu.Unlock()
		return errors.New("stop the running agent before retrying setup")
	}
	copy := *in
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.busy = true
	l.done = make(chan struct{})
	in.Status = "working"
	in.Error = ""
	if err := s.save(); err != nil {
		l.busy = false
		close(l.done)
		cancel()
		s.mu.Unlock()
		return err
	}
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		err := s.perform(ctx, copy, l, action)
		cancel()
		if err != nil {
			_ = s.update(id, func(in *manager.Instance) {
				in.Status = "failed"
				in.Error = "Operation failed; inspect logs, then Retry setup, Shell, or Start."
			})
			s.log(id, copy.Config).Write([]byte("ERROR: " + err.Error() + "\n"))
		}
		s.mu.Lock()
		l.busy = false
		l.cancel = nil
		close(l.done)
		s.mu.Unlock()
	}()
	return nil
}

// Close detaches guest clients, not the guests. The next supervisor can reconnect.
func (s *Supervisor) Close() {
	s.mu.Lock()
	s.closing = true
	for _, l := range s.live {
		if l.cancel != nil {
			l.cancel()
		}
	}
	s.mu.Unlock()
	s.wg.Wait()
	for _, l := range s.live {
		l.mu.Lock()
		l.detachTerminal()
		if l.vm != nil {
			_ = l.vm.Release()
			l.vm = nil
		}
		l.mu.Unlock()
	}
}

func (s *Supervisor) perform(ctx context.Context, in manager.Instance, l *live, action string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	log := s.log(in.ID, in.Config)
	defer log.Flush()
	if action == "start" && l.process != nil && in.Status != "shell" {
		return s.update(in.ID, func(i *manager.Instance) { i.Status = in.Status })
	}
	if action == "delete" || action == "stop" {
		l.detachTerminal()
		if e := s.backend.Control(ctx, "am-"+in.ID, in.RuntimeID, action == "delete"); e != nil {
			return e
		}
		if l.vm != nil {
			_ = l.vm.Release()
			l.vm = nil
		}
		if action == "stop" {
			return s.update(in.ID, func(i *manager.Instance) { i.Status = "stopped" })
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.state.Instances {
			if s.state.Instances[i].ID == in.ID {
				s.state.Instances = append(s.state.Instances[:i], s.state.Instances[i+1:]...)
				break
			}
		}
		delete(s.live, in.ID)
		// Logs are deliberately retained for inspection after deletion.
		return s.save()
	}
	if l.vm == nil {
		var v backend.VM
		var err error
		if in.RuntimeID != "" || in.Status != "new" {
			v, err = s.backend.Open(ctx, "am-"+in.ID, in.RuntimeID, true)
			// A known identity must never be silently replaced with a fresh disk.
			if err != nil && (in.RuntimeID != "" || !errors.Is(err, backend.ErrNotFound)) {
				return err
			}
		}
		if v == nil {
			fmt.Fprintln(log, "Booting OCI image in microsandbox...")
			v, err = s.backend.Create(ctx, "am-"+in.ID, in.Config)
		}
		if err != nil {
			return err
		}
		l.vm = v
		if err = s.update(in.ID, func(i *manager.Instance) { i.RuntimeID = v.ID() }); err != nil {
			return err
		}
	}
	if !in.Cloned && action != "shell" {
		fmt.Fprintln(log, "Cloning GitHub repository inside guest...")
		if err := l.vm.Run(ctx, "/", cloneCommand(in.Config), log); err != nil {
			return err
		}
		if err := s.update(in.ID, func(i *manager.Instance) { i.Cloned = true }); err != nil {
			return err
		}
	}
	if action == "retry" && l.process != nil && in.Status != "shell" {
		return errors.New("stop the running terminal before retrying setup")
	}
	if l.process != nil && in.Status == "shell" {
		l.detachTerminal()
	}
	if action != "shell" && (!in.SetupDone || action == "retry") {
		if err := s.update(in.ID, func(i *manager.Instance) { i.SetupDone = false; i.Status = "setup" }); err != nil {
			return err
		}
		fmt.Fprintln(log, "Running .agents/setup inside guest at /workspace/repo")
		if err := l.vm.Run(ctx, "/workspace/repo", setupScript(action == "retry"), log); err != nil {
			return err
		}
		if err := s.update(in.ID, func(i *manager.Instance) { i.SetupDone = true }); err != nil {
			return err
		}
		fmt.Fprintln(log, "Setup complete. Starting URI Agent PTY.")
	}
	l.detachTerminal()
	term := &screen{SafeEmulator: vt.NewSafeEmulator(80, 24)}
	cwd := "/workspace/repo"
	if action == "shell" && !in.Cloned {
		cwd = "/"
	}
	p, err := l.vm.Terminal(context.Background(), cwd, terminalCommand(in.Config, action == "shell"), term)
	if err != nil {
		term.Close()
		return err
	}
	l.term = term
	l.process = p
	go func() { _, _ = io.Copy(inputWriter{p}, term) }()
	go func() {
		err := p.Wait()
		_ = term.Close()
		_ = p.Close()
		l.mu.Lock()
		defer l.mu.Unlock()
		s.mu.Lock()
		defer s.mu.Unlock()
		if l.process != p {
			return
		}
		l.process = nil
		if i := s.instance(in.ID); i != nil {
			i.Status = "exited"
			if err != nil {
				i.Error = "PTY disconnected; Start to reconnect. See runtime diagnostics."
			}
			_ = s.save()
		}
	}()
	return s.update(in.ID, func(i *manager.Instance) {
		if action == "shell" {
			i.Status = "shell"
		} else {
			i.Status = "running"
		}
	})
}

type inputWriter struct{ p backend.Process }

func (w inputWriter) Write(b []byte) (int, error) {
	e := w.p.Input(b)
	if e != nil {
		return 0, e
	}
	return len(b), nil
}

type Input struct {
	ID         string
	Key        *uv.Key
	Paste      *string
	Mouse      *uv.Mouse
	MouseKind  string
	Rows, Cols int
}

func (s *Supervisor) Input(in Input) error {
	s.mu.Lock()
	l := s.live[in.ID]
	if l == nil || l.busy {
		s.mu.Unlock()
		return errors.New("no attached terminal")
	}
	s.mu.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.process == nil {
		return errors.New("no attached terminal")
	}
	t, p := l.term, l.process
	t.mu.Lock()
	defer t.mu.Unlock()
	if in.Rows > 0 && in.Cols > 0 {
		if in.Rows > 300 || in.Cols > 500 {
			return errors.New("terminal exceeds 500x300")
		}
		t.Resize(in.Cols, in.Rows)
		return p.Resize(in.Rows, in.Cols)
	}
	if in.Key != nil {
		t.SendKey(uv.KeyPressEvent(*in.Key))
	}
	if in.Paste != nil {
		t.Paste(*in.Paste)
	}
	if in.Mouse != nil {
		switch in.MouseKind {
		case "press":
			t.SendMouse(uv.MouseClickEvent(*in.Mouse))
		case "release":
			t.SendMouse(uv.MouseReleaseEvent(*in.Mouse))
		case "motion":
			t.SendMouse(uv.MouseMotionEvent(*in.Mouse))
		case "wheel":
			t.SendMouse(uv.MouseWheelEvent(*in.Mouse))
		}
	}
	return nil
}

// Redact complete lines so values split across stream chunks cannot bypass filtering.
// Mapped JSON strings and token/config lines are treated as sensitive, not logged.
type logWriter struct {
	mu      sync.Mutex
	dir, id string
	secrets []string
	pending string
}

func (s *Supervisor) log(id string, p manager.Project) *logWriter {
	w := &logWriter{dir: s.Dir, id: id}
	files := []string{p.AuthFile}
	for _, m := range p.Mappings {
		files = append(files, m.Host)
	}
	var collect func(any)
	collect = func(v any) {
		switch x := v.(type) {
		case string:
			if len(x) > 3 {
				w.secrets = append(w.secrets, x)
			}
		case map[string]any:
			for _, v := range x {
				collect(v)
			}
		case []any:
			for _, v := range x {
				collect(v)
			}
		}
	}
	for _, f := range files {
		if f == "" {
			continue
		}
		b, e := os.ReadFile(f)
		if e != nil {
			continue
		}
		var v any
		if json.Unmarshal(b, &v) == nil {
			collect(v)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if line = strings.TrimSpace(line); len(line) > 3 {
				w.secrets = append(w.secrets, line)
			}
		}
	}
	return w
}
func (w *logWriter) emit(line string) error {
	for _, secret := range w.secrets {
		line = strings.ReplaceAll(line, secret, "[REDACTED]")
	}
	// Logs are text, not a route for guest escape sequences into the host terminal.
	line = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, line)
	f, e := os.OpenFile(filepath.Join(w.dir, w.id+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = io.WriteString(f, line)
	return e
}
func (w *logWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(b)
	for {
		i := strings.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		if e := w.emit(w.pending[:i+1]); e != nil {
			return 0, e
		}
		w.pending = w.pending[i+1:]
	}
	if len(w.pending) > 1024*1024 {
		w.pending = "[oversized log line omitted]\n"
		_ = w.emit(w.pending)
		w.pending = ""
	}
	return len(b), nil
}
func (w *logWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending != "" {
		_ = w.emit(w.pending + "\n")
		w.pending = ""
	}
}
