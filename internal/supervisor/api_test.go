package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/4fuu/agent-manager/internal/backend"
)

func TestUnixSocketOwnershipAndClientReconnect(t *testing.T) {
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
	for {
		if st, e := os.Stat(filepath.Join(dir, "supervisor.sock")); e == nil && st.Mode().Perm() == 0600 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("private socket did not start")
		}
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 2; i++ {
		c := NewClient(dir)
		out, e := c.Call(Request{Action: "state"})
		if e != nil || out.State.Version != 1 {
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
}
