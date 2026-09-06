package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/4fuu/agent-manager/internal/buildinfo"
)

func TestVersionWithoutSupervisor(t *testing.T) {
	for _, arg := range []string{"--version", "version"} {
		t.Run(arg, func(t *testing.T) {
			oldArgs, oldOut := os.Args, os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()
			defer func() { os.Args, os.Stdout = oldArgs, oldOut }()
			os.Args, os.Stdout = []string{"agent-manager", arg}, w
			if err := run(); err != nil {
				t.Fatal(err)
			}
			w.Close()
			out, err := io.ReadAll(r)
			if err != nil || !strings.HasPrefix(string(out), "agent-manager "+buildinfo.Version+" (") {
				t.Fatalf("version output %q: %v", out, err)
			}
		})
	}
}
