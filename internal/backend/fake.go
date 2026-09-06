package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/4fuu/agent-manager/internal/manager"
)

// Fake is opt-in test infrastructure. It never executes commands on the host.
// Disk markers model persistent VM identity; terminal output is synthetic.
type Fake struct{ Dir string }

func (f Fake) DownloadImage(ctx context.Context, p manager.ImageProfile, report func(manager.ImageDownload)) (string, error) {
	for step := int64(0); step <= 20; step++ {
		report(manager.ImageDownload{Status: "downloading", Completed: step * 5_000_000, Total: 100_000_000})
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if strings.Contains(p.Image, "fail-pull") {
		return "", errors.New("[FAKE] registry download failed; edit the reference and retry")
	}
	report(manager.ImageDownload{Status: "importing"})
	return "sha256:fake-image", nil
}

func (f Fake) Create(_ context.Context, name string, p manager.Project) (VM, error) {
	if e := os.MkdirAll(f.Dir, 0700); e != nil {
		return nil, e
	}
	file := filepath.Join(f.Dir, name)
	h, e := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	_, e = h.WriteString(p.Image)
	h.Close()
	if e != nil {
		return nil, e
	}
	return &fakeVM{file: file, name: name, image: p.Image}, nil
}
func (f Fake) Open(_ context.Context, name, id string, start bool) (VM, error) {
	b, e := os.ReadFile(filepath.Join(f.Dir, name))
	if e != nil {
		if os.IsNotExist(e) {
			return nil, ErrNotFound
		}
		return nil, e
	}
	if id != "" && id != name {
		return nil, errors.New("identity mismatch")
	}
	if start {
		_ = os.Remove(filepath.Join(f.Dir, name) + ".stopped")
	}
	return &fakeVM{file: filepath.Join(f.Dir, name), name: name, image: string(b)}, nil
}

func (f Fake) Inspect(ctx context.Context, name, id string) (string, error) {
	_, e := f.Open(ctx, name, id, false)
	if e != nil {
		return "", e
	}
	if _, e = os.Stat(filepath.Join(f.Dir, name) + ".stopped"); e == nil {
		return "stopped", nil
	}
	return "running", nil
}
func (f Fake) Control(ctx context.Context, name, id string, destroy bool) error {
	v, e := f.Open(ctx, name, id, false)
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	if destroy {
		return v.Destroy(ctx)
	}
	return v.Stop(ctx)
}

type fakeVM struct{ file, name, image string }

func (v *fakeVM) ID() string                 { return v.name }
func (v *fakeVM) Release() error             { return nil }
func (v *fakeVM) Stop(context.Context) error { return os.WriteFile(v.file+".stopped", nil, 0600) }
func (v *fakeVM) Destroy(context.Context) error {
	_ = os.Remove(v.file + ".failed")
	_ = os.Remove(v.file + ".stopped")
	return os.Remove(v.file)
}
func (v *fakeVM) Run(c context.Context, cwd, cmd string, w io.Writer) error {
	if e := c.Err(); e != nil {
		return e
	}
	if strings.Contains(cmd, "if [ -f .agents/setup ]") {
		fmt.Fprintln(w, "[FAKE guest] setup cwd: "+cwd)
		if strings.Contains(v.image, "fail-setup") {
			marker := v.file + ".failed"
			if _, e := os.Stat(marker); os.IsNotExist(e) {
				_ = os.WriteFile(marker, []byte("1"), 0600)
				fmt.Fprintln(w, "[FAKE guest] dependency installation failed; retry will succeed")
				return errors.New("guest command exited 17")
			}
		}
		fmt.Fprintln(w, "[FAKE guest] setup complete")
	} else {
		fmt.Fprintln(w, "[FAKE guest] cloned into independent workspace")
	}
	return nil
}
func (v *fakeVM) Terminal(_ context.Context, cwd, cmd string, w io.Writer) (Process, error) {
	p := &fakeProcess{out: w, done: make(chan struct{}), input: make(chan []byte, 256)}
	fmt.Fprint(w, "\x1b[2J\x1b[H\x1b[36;1mFAKE BACKEND — terminal integration fixture\x1b[0m\r\nNo URI Agent or host command is running.\r\nGuest: "+v.name+"\r\nWorkspace: "+cwd+"\r\nType to exercise PTY input; Ctrl+] returns to manager.\r\n\x1b[?2004h\x1b[?1000h\x1b[?1006h\r\n$ ")
	go func() {
		for {
			select {
			case <-p.done:
				return
			case b := <-p.input:
				_, _ = p.out.Write(b)
			}
		}
	}()
	return p, nil
}

type fakeProcess struct {
	out   io.Writer
	done  chan struct{}
	once  sync.Once
	input chan []byte
}

func (p *fakeProcess) Input(b []byte) error {
	select {
	case <-p.done:
		return io.ErrClosedPipe
	case p.input <- append([]byte(nil), b...):
		return nil
	}
}
func (p *fakeProcess) Resize(r, c int) error { return nil }
func (p *fakeProcess) Wait() error           { <-p.done; return nil }
func (p *fakeProcess) Close() error          { p.once.Do(func() { close(p.done) }); return nil }
