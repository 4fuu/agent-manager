package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/4fuu/agent-manager/internal/backend"
	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/4fuu/agent-manager/internal/supervisor"
	"github.com/4fuu/agent-manager/internal/ui"
	ms "github.com/superradcompany/microsandbox/sdk/go"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	mode := "ui"
	args := os.Args[1:]
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		mode = args[0]
		args = args[1:]
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(home, ".local", "state")
	}
	fs := flag.NewFlagSet("agent-manager "+mode, flag.ContinueOnError)
	dir := fs.String("state", filepath.Join(base, "agent-manager"), "private supervisor state directory")
	fake := fs.Bool("fake", false, "explicit synthetic test backend (serve only)")
	archive := fs.String("archive", "", "OCI or docker-save archive for image-load")
	tag := fs.String("tag", manager.DefaultImage, "local microsandbox cache tag for image-load")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if mode == "ui" {
		return ui.Run(supervisor.NewClient(*dir))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	switch mode {
	case "serve":
		var b backend.Backend = backend.Microsandbox{}
		if *fake {
			fmt.Fprintln(os.Stderr, "FAKE backend: no microVM, URI Agent, or host commands")
			b = backend.Fake{Dir: filepath.Join(*dir, "fake-vms")}
		}
		return supervisor.Serve(ctx, *dir, func() (*supervisor.Supervisor, error) { return supervisor.New(*dir, b) })
	case "doctor":
		if e := backend.RuntimeCheck(); e != nil {
			return e
		}
		fmt.Println("KVM access available; live image boot is still required to verify runtime.")
		return nil
	case "runtime-install":
		return ms.EnsureInstalled(ctx)
	case "image-load":
		if *archive == "" {
			return fmt.Errorf("image-load requires --archive FILE")
		}
		_, e := ms.Image.Load(ctx, *archive, *tag)
		if e == nil {
			fmt.Println("Image imported as " + *tag)
		}
		return e
	default:
		return fmt.Errorf("usage: agent-manager [ui|serve|doctor|runtime-install|image-load] [--state DIR]; serve accepts --fake")
	}
}
