package manager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMappingDefaultsAndSafety(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "auth.json")
	if e := os.WriteFile(file, []byte(`{"token":"never-log-this"}`), 0600); e != nil {
		t.Fatal(e)
	}
	global := []Mapping{{Host: file, Guest: "/root/.config/uri-agent/auth.json"}}
	override := []Mapping{{Host: file, Guest: global[0].Guest, Writable: true}}
	got := MergeMappings(global, override)
	if len(got) != 1 || !got[0].Writable || global[0].Writable {
		t.Fatal("override must not mutate defaults")
	}
	if e := ValidateMappings(global); e != nil {
		t.Fatal(e)
	}
	for _, m := range []Mapping{{Host: dir, Guest: "/config"}, {Host: "relative", Guest: "/config"}, {Host: file, Guest: "/workspace/repo/config"}, {Host: file, Guest: "/a/../config"}, {Host: "/dev/null", Guest: "/config"}} {
		if ValidateMappings([]Mapping{m}) == nil {
			t.Errorf("unsafe mapping accepted: %+v", m)
		}
	}
	if ValidateMappings(append(global, global...)) == nil {
		t.Fatal("duplicate mappings accepted")
	}
}
func TestProjectValidation(t *testing.T) {
	p := Project{Name: "test", URL: "https://github.com/4fuu/uri-agent", Image: DefaultImage, Command: "uri-agent"}
	if e := ValidateProject(p); e != nil {
		t.Fatal(e)
	}
	for _, url := range []string{"https://secret@github.com/a/b", "https://github.com/a/b?token=secret", "https://evil.test/a/b", "file:///host", "git@github.com:a/b", "https://github.com/a"} {
		q := p
		q.URL = url
		if ValidateProject(q) == nil {
			t.Errorf("accepted %s", url)
		}
	}
	p.Ref = "--upload-pack=host-command"
	if ValidateProject(p) == nil {
		t.Fatal("option injection")
	}
}
func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	s := State{Version: 1, Projects: []Project{{ID: "persisted", Name: "Reusable project"}}}
	if e := Save(dir, s); e != nil {
		t.Fatal(e)
	}
	got, e := Load(dir)
	if e != nil || got.Projects[0].ID != "persisted" {
		t.Fatal(got, e)
	}
	st, e := os.Stat(filepath.Join(dir, "state.json"))
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("metadata not private")
	}
	if e := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"Version":99}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Load(dir); e == nil {
		t.Fatal("unknown version accepted")
	}
}
