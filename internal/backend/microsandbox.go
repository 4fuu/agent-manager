package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/4fuu/agent-manager/internal/manager"
	ms "github.com/superradcompany/microsandbox/sdk/go"
)

type Microsandbox struct{}

func (Microsandbox) Create(ctx context.Context, name string, p manager.Project) (VM, error) {
	if err := manager.ValidateProject(p); err != nil {
		return nil, err
	}
	if err := RuntimeCheck(); err != nil {
		return nil, err
	}
	if p.Archive != "" {
		tag := "agent-manager-import:" + name
		if _, err := ms.Image.Load(ctx, p.Archive, tag); err != nil {
			return nil, fmt.Errorf("load image archive: %w", err)
		}
		p.Image = tag
	}
	mounts := map[string]ms.MountConfig{}
	for _, m := range p.Mappings {
		mounts[m.Guest] = ms.Mount.Bind(m.Host, ms.MountOptions{Readonly: !m.Writable, Nosuid: true, Nodev: true})
	}
	if p.AuthFile != "" {
		mounts["/run/manager/github-token"] = ms.Mount.Bind(p.AuthFile, ms.MountOptions{Readonly: true, Noexec: true, Nosuid: true, Nodev: true})
	}
	sb, err := ms.CreateSandbox(ctx, name, ms.WithImage(p.Image), ms.WithDetached(), ms.WithRootDisk(ms.RootDisk.Managed(16384)), ms.WithMemory(4096), ms.WithCPUs(2), ms.WithMounts(mounts), ms.WithEnv(p.Environment))
	if err != nil {
		return nil, err
	}
	return &microVM{sb}, nil
}
func handle(ctx context.Context, name, id string) (*ms.SandboxHandle, error) {
	h, err := ms.GetSandbox(ctx, name)
	if err != nil {
		var me *ms.Error
		if errors.As(err, &me) && me.Kind == ms.ErrSandboxNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if id != "" && h.ID() != id {
		return nil, errors.New("runtime identity mismatch; refusing to control replacement VM")
	}
	return h, nil
}
func (Microsandbox) Inspect(ctx context.Context, name, id string) (string, error) {
	h, err := handle(ctx, name, id)
	if err != nil {
		return "", err
	}
	return string(h.Status()), nil
}
func (Microsandbox) Control(ctx context.Context, name, id string, destroy bool) error {
	h, err := handle(ctx, name, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if destroy {
		return h.Destroy(ctx)
	}
	return h.Stop(ctx)
}
func (Microsandbox) Open(ctx context.Context, name, id string, start bool) (VM, error) {
	if err := RuntimeCheck(); err != nil {
		return nil, err
	}
	h, err := handle(ctx, name, id)
	if err != nil {
		return nil, err
	}
	var sb *ms.Sandbox
	if start {
		if h.Status() == ms.SandboxStatusRunning {
			sb, err = h.Connect(ctx)
		} else {
			sb, err = h.StartDetached(ctx)
		}
	} else {
		sb, err = h.Connect(ctx)
	}
	if err != nil {
		return nil, err
	}
	return &microVM{sb}, nil
}

type microVM struct{ sb *ms.Sandbox }

func (v *microVM) ID() string                      { return v.sb.ID() }
func (v *microVM) Release() error                  { return v.sb.Detach(context.Background()) }
func (v *microVM) Stop(c context.Context) error    { return v.sb.Stop(c) }
func (v *microVM) Destroy(c context.Context) error { return v.sb.Destroy(c) }
func (v *microVM) Run(c context.Context, cwd, command string, out io.Writer) error {
	h, err := v.sb.ExecStream(c, "/bin/sh", []string{"-c", command}, ms.WithExecCwd(cwd))
	if err != nil {
		return err
	}
	defer h.Close()
	err = receive(c, h, out)
	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.Kill(ctx)
	}
	return err
}
func receive(c context.Context, h *ms.ExecHandle, out io.Writer) error {
	code := 0
	for {
		e, err := h.Recv(c)
		if err != nil {
			return err
		}
		switch e.Kind {
		case ms.ExecEventStdout, ms.ExecEventStderr:
			if _, err := out.Write(e.Data); err != nil {
				return err
			}
		case ms.ExecEventExited:
			code = e.ExitCode
		case ms.ExecEventFailed:
			return errors.New("guest process failed to start")
		case ms.ExecEventStdinError:
			return errors.New("guest input transport failed")
		case ms.ExecEventDone:
			if code != 0 {
				return fmt.Errorf("guest command exited %d", code)
			}
			return nil
		}
	}
}
func (v *microVM) Terminal(c context.Context, cwd, command string, out io.Writer) (Process, error) {
	h, err := v.sb.ExecStream(c, "/bin/sh", []string{"-c", command}, ms.WithExecCwd(cwd), ms.WithExecTTY(true), ms.WithExecStdinPipe())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &microProcess{h: h, in: h.TakeStdin(), done: make(chan struct{}), cancel: cancel}
	go func() { p.err = receive(ctx, h, out); close(p.done) }()
	return p, nil
}

type microProcess struct {
	h      *ms.ExecHandle
	in     *ms.ExecSink
	done   chan struct{}
	err    error
	once   sync.Once
	cancel context.CancelFunc
}

func (p *microProcess) Input(b []byte) error { _, e := p.in.Write(b); return e }
func (p *microProcess) Resize(r, c int) error {
	return p.h.Resize(context.Background(), uint16(r), uint16(c))
}
func (p *microProcess) Wait() error { <-p.done; return p.err }
func (p *microProcess) Close() error {
	p.once.Do(func() {
		// Kill only the tmux attachment client. The guest tmux server retains the agent.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.h.Kill(ctx)
		p.cancel()
		<-p.done
		_ = p.in.Close()
		_ = p.h.Close()
	})
	return nil
}
