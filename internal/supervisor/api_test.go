package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/4fuu/agent-manager/internal/backend"
	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/4fuu/agent-manager/internal/privatefs"
)

func TestIPCOwnershipAndClientReconnect(t *testing.T) {
	dir, e := os.MkdirTemp("", "am-rpc-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	factory := func() (*Supervisor, error) { return New(dir, backend.Fake{Dir: filepath.Join(dir, "vms")}) }
	go func() { done <- Serve(ctx, dir, factory) }()
	deadline := time.Now().Add(2 * time.Second)
	probe := NewClient(dir)
	probe.http.Timeout = 100 * time.Millisecond
	for {
		if _, e := probe.Call(Request{Action: "state"}); e == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatal("server failed", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("private IPC did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := privatefs.Check(dir); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		c := NewClient(dir)
		out, e := c.Call(Request{Action: "state"})
		if e != nil || out.State.Version != manager.StateVersion {
			t.Fatal("RPC reconnect failed", e)
		}
	}
	if e := Serve(context.Background(), dir, factory); e == nil {
		t.Fatal("two supervisors acquired same state")
	}
	c := NewClient(dir)
	if _, e := c.Call(Request{Action: "state"}); e != nil {
		t.Fatal("second startup disturbed first socket", e)
	}
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
	// The OS lock and endpoint must be reusable without deleting lock files.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go func() { done <- Serve(ctx2, dir, factory) }()
	deadline = time.Now().Add(2 * time.Second)
	for {
		if _, err := probe.Call(Request{Action: "state"}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("could not reconnect after supervisor restart")
		}
		time.Sleep(time.Millisecond)
	}
	if err := probe.Probe(context.Background()); err != nil {
		t.Fatal("health probe failed", err)
	}
	if err := probe.Shutdown(context.Background()); err != nil {
		t.Fatal("graceful IPC shutdown failed", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := probe.Probe(context.Background()); err == nil {
		t.Fatal("stopped supervisor reported healthy")
	}
}
