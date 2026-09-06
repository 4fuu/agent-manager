package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
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
	base, e := defaultStateBase()
	if e != nil {
		return e
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
		fmt.Printf("Host: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		if e := backend.RuntimeCheck(); e != nil {
			return fmt.Errorf("runtime prerequisite: %w", e)
		}
		fmt.Println("Runtime prerequisite: available")
		if !ms.IsInstalled() {
			return fmt.Errorf("microsandbox runtime %s is not installed; run agent-manager runtime-install", ms.SDKVersion())
		}
		fmt.Printf("Microsandbox runtime: installed (version %s)\n", ms.SDKVersion())
		fmt.Println("Prerequisites and installed files are available; an actual VM boot is still required to verify the runtime.")
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

func defaultStateBase() (string, error) {
	if runtime.GOOS == "windows" {
		// Runtime identities are machine-local, not roaming profile settings.
		return os.UserCacheDir()
	}
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return base, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state"), nil
}
