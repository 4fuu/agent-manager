package supervisor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/4fuu/agent-manager/internal/backend"
	"github.com/4fuu/agent-manager/internal/manager"
	uv "github.com/charmbracelet/ultraviolet"
)

// This fixture installs terminal dependencies INSIDE the real disposable guest.
// It verifies the production supervisor/PTY path, not the five Agent recipes.
type liveWorkspaceBackend struct{ backend.Microsandbox }

func (b liveWorkspaceBackend) Create(ctx context.Context, name string, p manager.Project) (backend.VM, error) {
	v, err := b.Microsandbox.Create(ctx, name, p)
	if err != nil {
		return nil, err
	}
	var log bytes.Buffer
	err = v.Run(ctx, "/", "set -eu; apk add --no-cache bash tmux git util-linux; mkdir -p /workspace/repo/.agents; printf 'cat /trusted/config.txt; echo SETUP_VISIBLE; touch /workspace/setup-ran\\n' > /workspace/repo/.agents/setup", &log)
	if err != nil {
		_ = v.Destroy(context.Background())
		_ = v.Release()
		return nil, err
	}
	return v, nil
}

func TestLiveWorkspaceTitlesDirectoryAndReconnect(t *testing.T) {
	if os.Getenv("AGENT_MANAGER_LIVE_TEST") != "1" {
		t.Skip("opt-in real native microVM test")
	}
	dir := t.TempDir()
	host := filepath.Join(dir, "可信 config")
	if err := os.Mkdir(host, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(host, "config.txt"), []byte("trusted-config-value"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(filepath.Join(dir, "state"), liveWorkspaceBackend{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Project(manager.Project{Name: "Native workspace", URL: "https://github.com/octocat/Hello-World"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Image(manager.ImageProfile{ID: "native", Name: "Native fixture", Image: "docker.io/library/alpine:3.22", Command: "/bin/bash --noprofile --norc", Mappings: []manager.Mapping{{Host: host, Guest: "/trusted", Writable: true}}}); err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateProfile(s.State().Projects[0].ID, "native")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture repository avoids relying on a third-party repository's setup.
	if err := s.update(id, func(i *manager.Instance) { i.Cloned = true }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if e := (backend.Microsandbox{}).Control(ctx, "am-"+id, "", true); e != nil {
			t.Error(e)
		}
	})
	if err := s.Action(id, "start"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		in := s.instanceCopy(id)
		if in.Status == "running" {
			break
		}
		if in.Status == "failed" {
			f, _ := s.Frame(id)
			t.Fatal(f.Logs)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if in := s.instanceCopy(id); in.Status != "running" {
		t.Fatal("native setup timed out", in)
	}
	f, _ := s.Frame(id)
	if !strings.Contains(f.Logs, "SETUP_VISIBLE") || !strings.Contains(f.Logs, "trusted-config-value") {
		t.Fatal("directory setup output missing", f.Logs)
	}
	pane, err := s.AddPane(id, "/bin/bash -l")
	if err != nil {
		t.Fatal(err)
	}
	command := "printf '\\033]2;native-left\\007'; echo from-guest > /trusted/result.txt; cat /proc/sys/kernel/random/boot_id > /tmp/session-boot-id\n"
	if err := s.Input(Input{ID: id, PaneID: "agent", Paste: &command}); err != nil {
		t.Fatal(err)
	}
	enter := uv.Key{Code: uv.KeyEnter}
	if err := s.Input(Input{ID: id, PaneID: "agent", Key: &enter}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s.State().Instances[0].Title == "native-left" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if s.State().Instances[0].Title != "native-left" {
		f, _ := s.Frame(id)
		var contents strings.Builder
		for _, c := range f.Cells {
			contents.WriteString(c.Text)
		}
		t.Fatal("OSC title did not cross real tmux/PTY", s.State().Instances[0].Title, contents.String())
	}
	if b, err := os.ReadFile(filepath.Join(host, "result.txt")); err != nil || !strings.Contains(string(b), "from-guest") {
		t.Fatal("native directory write", err, string(b))
	}
	// /tmp is guest-local, and boot_id identifies the running kernel, not a mount.
	command = "cmp /tmp/session-boot-id /proc/sys/kernel/random/boot_id && echo same-sandbox > /trusted/sibling.txt\n"
	if err := s.Input(Input{ID: id, PaneID: pane, Paste: &command}); err != nil {
		t.Fatal(err)
	}
	if err := s.Input(Input{ID: id, PaneID: pane, Key: &enter}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(host, "sibling.txt")); err == nil && strings.Contains(string(b), "same-sandbox") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if b, err := os.ReadFile(filepath.Join(host, "sibling.txt")); err != nil || !strings.Contains(string(b), "same-sandbox") {
		t.Fatal("sibling terminal did not share the agent's guest filesystem and kernel", err, string(b))
	}
	if err := s.ClosePane(id, pane); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.PaneFrame(id, "agent"); !f.Live {
		t.Fatal("close sibling killed agent")
	}
	s.Close()
	reopened, err := New(s.Dir, liveWorkspaceBackend{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	act(t, reopened, id, "start", "running")
	if f, _ := reopened.Frame(id); !f.Live {
		t.Fatal("native tmux reconnect failed")
	}
}
