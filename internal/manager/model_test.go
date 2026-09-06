package manager

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/4fuu/agent-manager/internal/privatefs"
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
	if e := ValidateMappings([]Mapping{{Host: dir, Guest: "/config"}}); e != nil {
		t.Fatal("explicit directory mapping rejected", e)
	}
	for _, m := range []Mapping{{Host: "relative", Guest: "/config"}, {Host: file, Guest: "/workspace/repo/config"}, {Host: file, Guest: "/a/../config"}, {Host: "/dev/null", Guest: "/config"}, {Host: file, Guest: "/run"}, {Host: file, Guest: "/run/manager"}, {Host: file, Guest: `/root\config`}} {
		if ValidateMappings([]Mapping{m}) == nil {
			t.Errorf("unsafe mapping accepted: %+v", m)
		}
	}
	if ValidateMappings(append(global, global...)) == nil {
		t.Fatal("duplicate mappings accepted")
	}
	if ValidateMappings([]Mapping{{Host: dir, Guest: "/config"}, {Host: file, Guest: "/config/auth.json"}}) == nil {
		t.Fatal("overlapping mappings accepted")
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
	p := Project{ID: "persisted", Name: "Reusable project", Preset: "custom", Image: "local:tag",
		Command: "custom-agent", Environment: map[string]string{"TERM": "vt100"}}
	s := State{Version: StateVersion, Projects: []Project{p},
		Instances: []Instance{{ID: "instance", RuntimeID: "runtime", Config: p, SetupDone: true}}}
	if e := Save(dir, s); e != nil {
		t.Fatal(e)
	}
	s.Projects[0].Command = "updated-agent"
	if e := Save(dir, s); e != nil {
		t.Fatal(e)
	}
	got, e := Load(dir)
	if e != nil || !reflect.DeepEqual(got, s) {
		t.Fatal(got, e)
	}
	if e := privatefs.Check(filepath.Join(dir, "state.json")); e != nil {
		t.Fatal("metadata not private", e)
	}
	if e := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"Version":99}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Load(dir); e == nil {
		t.Fatal("unknown version accepted")
	}
}

func TestNativeCredentialFileValidation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "token 配置.txt")
	if err := os.WriteFile(file, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := privatefs.Protect(file); err != nil {
		t.Fatal(err)
	}
	p := Project{Name: "test", URL: "https://github.com/a/b", Preset: "uri", AuthFile: file}
	if _, err := ResolveProject(p); err != nil {
		t.Fatal("private native credential file rejected", err)
	}
	p.AuthFile = dir
	if _, err := ResolveProject(p); err == nil {
		t.Fatal("credential directory accepted")
	}
}

func TestInvalidStateDoesNotOverwrite(t *testing.T) {
	for _, data := range []string{`{}`, `{"Version":0}`, `{"Version":1}`, `{"Version":99}`, `{"Version":1,`} {
		t.Run(data, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "state.json")
			if err := os.WriteFile(file, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(dir); err == nil {
				t.Fatal("invalid state accepted")
			}
			if err := Save(dir, State{Version: 99}); err == nil {
				t.Fatal("invalid state saved")
			}
			after, err := os.ReadFile(file)
			if err != nil || string(after) != data {
				t.Fatal("invalid state overwritten", err)
			}
		})
	}
}

func TestPresetResolution(t *testing.T) {
	for _, preset := range []string{"uri", "pi", "omp", "claude", "codex", "custom", ""} {
		t.Run(preset, func(t *testing.T) {
			p := Project{Name: "test", URL: "https://github.com/a/b", Preset: preset}
			if preset == "custom" || preset == "" {
				p.Image = "local-agent:tested"
			}
			if preset == "custom" || preset == "" {
				p.Command = "my-agent --resume"
			}
			got, err := ResolveProject(p)
			if err != nil || got.Command == "" || got.Environment["TERM"] != "xterm-256color" {
				t.Fatal(got, err)
			}
			if (got.Environment["URI_AGENT_CONFIG_DIR"] != "") != (preset == "uri") {
				t.Fatal("URI defaults leaked into another preset", got)
			}
			p.Command, p.Image = "explicit --continue", "explicit:tag"
			p.Environment = map[string]string{"TERM": "vt100", "CONFIG_HOME": "/config"}
			got, err = ResolveProject(p)
			if err != nil || got.Command != p.Command || got.Image != p.Image || got.Environment["TERM"] != "vt100" {
				t.Fatal("explicit configuration lost", got, err)
			}
			p.Environment["CONFIG_HOME"] = "/changed"
			if got.Environment["CONFIG_HOME"] != "/config" {
				t.Fatal("resolved configuration aliases input")
			}
		})
	}
	for _, preset := range []string{"unknown"} {
		if _, err := ResolveProject(Project{Name: "test", URL: "https://github.com/a/b", Preset: preset}); err == nil {
			t.Fatal("accepted unknown preset or invented image", preset)
		}
	}
	for _, env := range []map[string]string{{"": "x"}, {"1NAME": "x"}, {"A=B": "x"}, {"A": "x\x00y"}} {
		if _, err := ResolveProject(Project{Name: "test", URL: "https://github.com/a/b", Preset: "uri", Environment: env}); err == nil {
			t.Fatal("invalid environment accepted")
		}
	}
}

func TestRepositoryAndImageProfiles(t *testing.T) {
	p := Project{Name: "saved", URL: "https://github.com/a/b"}
	if err := ValidateRepository(p); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProject(p); err == nil {
		t.Fatal("unresolved project accepted")
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "image.tar")
	if err := os.WriteFile(archive, []byte("archive"), 0600); err != nil {
		t.Fatal(err)
	}
	image := ImageProfile{ID: "custom", Name: "Custom", Archive: archive, Command: "agent"}
	if err := ValidateImage(image); err != nil {
		t.Fatal(err)
	}
	images := BuiltinImages()
	if len(images) != 5 {
		t.Fatalf("got %d builtins", len(images))
	}
	images[0].Environment["CHANGED"] = "yes"
	if BuiltinImages()[0].Environment["CHANGED"] != "" {
		t.Fatal("builtins alias caller data")
	}
	state, err := Load(filepath.Join(dir, "new"))
	if err != nil || len(state.Images) != 5 {
		t.Fatal(state, err)
	}
}
