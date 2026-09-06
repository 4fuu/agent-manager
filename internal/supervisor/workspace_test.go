package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/4fuu/agent-manager/internal/backend"
	"github.com/4fuu/agent-manager/internal/manager"
)

func TestProfileSnapshotDefaultBranchAndTrustedDirectories(t *testing.T) {
	s, _ := fixture(t, "fixture:ok")
	project := s.State().Projects[0]
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(`{"token":"trusted"}`), 0600); err != nil {
		t.Fatal(err)
	}
	profile := manager.ImageProfile{Name: "Custom", Image: "fixture:ok", Command: "my-agent", Mappings: []manager.Mapping{{Host: dir, Guest: "/config"}}, Environment: map[string]string{"CONFIG_HOME": "/config"}}
	if err := s.Image(profile); err != nil {
		t.Fatal(err)
	}
	images := s.State().Images
	profileID := images[len(images)-1].ID
	profile.Environment["CONFIG_HOME"] = "mutated"
	id, err := s.CreateProfile(project.ID, profileID)
	if err != nil {
		t.Fatal(err)
	}
	in := s.instanceCopy(id)
	if in.Config.Ref != "" || strings.Contains(cloneCommand(in.Config), "checkout") || in.Config.Environment["CONFIG_HOME"] != "/config" {
		t.Fatal("not an immutable default-branch snapshot", in)
	}
	if err := manager.ValidateProject(in.Config); err != nil {
		t.Fatal("custom image snapshot cannot boot", err)
	}
	if err := s.DeleteImage(profileID); err != nil {
		t.Fatal(err)
	}
	act(t, s, id, "start", "running")
	f, _ := s.Frame(id)
	if !strings.Contains(f.Logs, "[FAKE guest] setup complete") || strings.Contains(f.Logs, "suppressed") {
		t.Fatal("trusted directory suppressed setup output", f.Logs)
	}
}

func TestWorkspacePaneIsolationTitlesCloseAndRestart(t *testing.T) {
	s, id := fixture(t, "fixture:ok")
	act(t, s, id, "start", "running")
	pane, err := s.AddPane(id, "/bin/bash -l")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Width(id, pane, 50); err != nil {
		t.Fatal(err)
	}
	write := func(pane, text string) {
		t.Helper()
		l := s.live[id]
		l.mu.Lock()
		defer l.mu.Unlock()
		if _, err := l.panes[pane].term.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	write("agent", "\x1b]2;left title\x07FIRST_ONLY")
	write(pane, "\x1b]0;right title\x1b\\SECOND_ONLY")
	if in := s.State().Instances[0]; in.Title != "left title" {
		t.Fatal("not anchored to leftmost", in.Title)
	}
	f, _ := s.PaneFrame(id, pane)
	if f.Title != "right title" {
		t.Fatal("pane has another pane's title", f.Title)
	}
	var text strings.Builder
	for _, c := range f.Cells {
		text.WriteString(c.Text)
	}
	if strings.Contains(text.String(), "FIRST_ONLY") || !strings.Contains(text.String(), "SECOND_ONLY") {
		t.Fatal("cross-pane output", text.String())
	}
	if err := s.Rename(id, "fixed name"); err != nil {
		t.Fatal(err)
	}
	write("agent", "\x1b]2;changed\x07")
	if in := s.State().Instances[0]; in.Name != "fixed name" {
		t.Fatal("OSC overwrote custom name")
	}
	if err := s.ClosePane(id, "agent"); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.PaneFrame(id, pane); !f.Live {
		t.Fatal("close killed neighbour")
	}
	if err := s.Input(Input{ID: id, PaneID: "agent", Rows: 30, Cols: 50}); err == nil {
		t.Fatal("stale pane input redirected")
	}
	if in := s.State().Instances[0]; in.Title != "right title" || in.Name != "fixed name" {
		t.Fatal("new leftmost title", in)
	}
	before := s.instanceCopy(id)
	act(t, s, id, "stop", "stopped")
	act(t, s, id, "start", "running")
	after := s.instanceCopy(id)
	if len(after.Panes) != 1 || after.Panes[0].ID != pane || after.Panes[0].Width != 50 || after.RuntimeID != before.RuntimeID {
		t.Fatal("restart lost workspace", after)
	}
	s.Close()
	reopened, err := New(s.Dir, backend.Fake{Dir: filepath.Join(s.Dir, "vms")})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	act(t, reopened, id, "start", "running")
	if f, _ := reopened.PaneFrame(id, pane); !f.Live {
		t.Fatal("supervisor reconnect lost pane")
	}
	if err := reopened.ClosePane(id, pane); err != nil {
		t.Fatal(err)
	}
	act(t, reopened, id, "stop", "stopped")
	act(t, reopened, id, "start", "running")
	if len(reopened.State().Instances[0].Panes) != 0 {
		t.Fatal("last closed pane was silently recreated")
	}
}

func TestRecoveryShellIsVisibleWithoutStartingAgent(t *testing.T) {
	s, id := fixture(t, "fixture:fail-setup")
	act(t, s, id, "start", "failed")
	act(t, s, id, "shell", "shell")
	in := s.instanceCopy(id)
	if len(in.Panes) != 2 || in.Panes[1].ID != "recovery" {
		t.Fatal("recovery is absent from workspace", in)
	}
	if f, _ := s.PaneFrame(id, "agent"); f.Live {
		t.Fatal("agent started after failed setup")
	}
	if f, _ := s.PaneFrame(id, "recovery"); !f.Live {
		t.Fatal("recovery shell is not reachable")
	}
	act(t, s, id, "retry", "running")
	if f, _ := s.PaneFrame(id, "agent"); !f.Live {
		t.Fatal("successful retry did not start agent")
	}
}
