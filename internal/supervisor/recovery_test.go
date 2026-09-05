package supervisor

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/4fuu/agent-manager/internal/backend"
	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/charmbracelet/x/vt"
)

type waitingBackend struct {
	backend.Fake
	started chan struct{}
}
type waitingVM struct {
	backend.VM
	started chan struct{}
}

func (b waitingBackend) Create(c context.Context, n string, p manager.Project) (backend.VM, error) {
	v, e := b.Fake.Create(c, n, p)
	return waitingVM{v, b.started}, e
}
func (v waitingVM) Run(c context.Context, cwd, cmd string, w io.Writer) error {
	if strings.Contains(cmd, setupCommand) {
		close(v.started)
		<-c.Done()
		return c.Err()
	}
	return v.VM.Run(c, cwd, cmd, w)
}
func TestStopCancelsSetupWithoutCompletingIt(t *testing.T) {
	s, id := fixture(t, "fixture:ok")
	b := waitingBackend{Fake: backend.Fake{Dir: filepath.Join(s.Dir, "vms")}, started: make(chan struct{})}
	s.backend = b
	if e := s.Action(id, "start"); e != nil {
		t.Fatal(e)
	}
	select {
	case <-b.started:
	case <-time.After(time.Second):
		t.Fatal("setup not started")
	}
	if e := s.Action(id, "retry"); e == nil {
		t.Fatal("concurrent setup accepted")
	}
	act(t, s, id, "stop", "stopped")
	if s.State().Instances[0].SetupDone {
		t.Fatal("canceled setup marked complete")
	}
}
func TestTerminalCloseUnblocksDeviceQuery(t *testing.T) {
	term := &screen{SafeEmulator: vt.NewSafeEmulator(80, 24)}
	done := make(chan struct{})
	go func() { _, _ = term.Write([]byte("\x1b[6n")); close(done) }()
	// No input reader: the device-status reply blocks until Close breaks the pipe.
	if e := term.Close(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("terminal close deadlocked")
	}
}
func TestLiveRetryRejectedWithoutStatusChange(t *testing.T) {
	s, id := fixture(t, "fixture:ok")
	act(t, s, id, "start", "running")
	if e := s.Action(id, "retry"); e == nil {
		t.Fatal("retry accepted with live agent")
	}
	if s.State().Instances[0].Status != "running" {
		t.Fatal("invalid action changed status")
	}
}
