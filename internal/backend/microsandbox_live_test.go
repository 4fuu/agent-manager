package backend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/4fuu/agent-manager/internal/manager"
)

// Opt-in: downloads a public image and creates/deletes only this test's VM.
func TestMicrosandboxLiveDiskAndFileMount(t *testing.T) {
	if os.Getenv("AGENT_MANAGER_LIVE_TEST") != "1" {
		t.Skip("set AGENT_MANAGER_LIVE_TEST=1 with an installed native runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	file := filepath.Join(t.TempDir(), "配置 file.txt")
	if err := os.WriteFile(file, []byte("explicit-file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writable := filepath.Join(filepath.Dir(file), "writable.txt")
	if err := os.WriteFile(writable, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := manager.ResolveProject(manager.Project{Name: "native-check", URL: "https://github.com/a/b",
		Image: "docker.io/library/alpine:3.22", Command: "/bin/sh",
		Mappings: []manager.Mapping{{Host: file, Guest: "/config.txt"}, {Host: writable, Guest: "/writable.txt", Writable: true}}})
	if err != nil {
		t.Fatal(err)
	}
	b := Microsandbox{}
	name := fmt.Sprintf("am-native-test-%d", time.Now().UnixNano())
	var v VM
	var id string
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if err := b.Control(cleanup, name, id, true); err != nil {
			t.Errorf("cleanup VM %s: %v", name, err)
		}
		if v != nil {
			_ = v.Release()
		}
	})
	v, err = b.Create(ctx, name, p)
	if err != nil {
		t.Fatal(err)
	}
	id = v.ID()
	var output bytes.Buffer
	if err := v.Run(ctx, "/", "set -eu; test \"$(cat /config.txt)\" = explicit-file; if echo forbidden > /config.txt; then exit 99; fi; echo durable > /native-check; uname -s; printf native-ok", &output); err != nil {
		t.Fatalf("guest file isolation: %v, output: %s", err, &output)
	}
	if !strings.Contains(output.String(), "Linux") || !strings.Contains(output.String(), "native-ok") {
		t.Fatal("guest did not confirm Linux execution", &output)
	}
	if err := v.Run(ctx, "/", "printf changed > /writable.txt", &output); err != nil {
		t.Fatal(err)
	}
	ptyOutput := make(terminalOutput, 64)
	proc, err := v.Terminal(ctx, "/", "read value; printf 'PTY:%s\\n' \"$value\"", ptyOutput)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proc.Close() })
	if err := proc.Resize(35, 100); err != nil {
		t.Fatal(err)
	}
	if err := proc.Input([]byte("native-中文\n")); err != nil {
		t.Fatal(err)
	}
	terminalText := ""
	for !strings.Contains(terminalText, "PTY:native-中文") {
		select {
		case part := <-ptyOutput:
			terminalText += part
		case <-ctx.Done():
			t.Fatal("guest PTY input/output did not complete", terminalText)
		}
	}
	if err := proc.Wait(); err != nil {
		t.Fatal(err)
	}
	_ = proc.Close()
	if err := v.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	_ = v.Release()
	v, err = b.Open(ctx, name, id, true)
	if err != nil {
		t.Fatal(err)
	}
	if v.ID() != id {
		t.Fatal("resume changed runtime identity")
	}
	output.Reset()
	if err := v.Run(ctx, "/", "test \"$(cat /native-check)\" = durable && printf resumed-ok", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "resumed-ok") {
		t.Fatal("persistent guest disk not restored", &output)
	}
	if err := b.Control(ctx, name, id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Inspect(ctx, name, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted guest still exists", err)
	}
	content, err := os.ReadFile(file)
	if err != nil || string(content) != "explicit-file\n" {
		t.Fatal("guest modified read-only host source", err)
	}
	content, err = os.ReadFile(writable)
	if err != nil || string(content) != "changed" {
		t.Fatal("explicit writable mapping did not persist writes", err)
	}
}

// Tests an exported Agent image through the production archive importer and VM.
func TestMicrosandboxLiveAgentArchive(t *testing.T) {
	archive := os.Getenv("AGENT_MANAGER_IMAGE_ARCHIVE")
	if os.Getenv("AGENT_MANAGER_LIVE_TEST") != "1" || archive == "" {
		t.Skip("set AGENT_MANAGER_LIVE_TEST=1 and AGENT_MANAGER_IMAGE_ARCHIVE to a built image tar")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	b := Microsandbox{}
	name := fmt.Sprintf("am-image-test-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if err := b.Control(cleanup, name, "", true); err != nil {
			t.Error(err)
		}
	})
	v, err := b.Create(ctx, name, manager.Project{Name: "image-check", URL: "https://github.com/a/b", Archive: archive, Command: "/bin/bash"})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Release()
	var output bytes.Buffer
	err = v.Run(ctx, "/workspace", `set -eu
for tool in python pip node npm git gh rg fd fzf jq yazi ya tmux bash ssh less file ps ip ping dig zip unzip gcc g++ make; do
  command -v "$tool"
done
python --version
python -m venv /tmp/image-venv
/tmp/image-venv/bin/python -c 'import ssl, sqlite3; print("python-ok")'
/tmp/image-venv/bin/pip --version
node -e 'console.log("node-ok", process.version)'
for agent in uri-agent pi omp claude codex; do
  if command -v "$agent"; then "$agent" --version; echo agent-ok; exit 0; fi
done
exit 1`, &output)
	t.Log(output.String())
	if err != nil || !strings.Contains(output.String(), "agent-ok") {
		t.Fatalf("built image guest smoke test: %v", err)
	}
}

type terminalOutput chan string

func (out terminalOutput) Write(b []byte) (int, error) {
	out <- string(b)
	return len(b), nil
}
