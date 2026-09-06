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
	"time"

	"github.com/4fuu/agent-manager/internal/backend"
	"github.com/4fuu/agent-manager/internal/buildinfo"
	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/4fuu/agent-manager/internal/privatefs"
	"github.com/4fuu/agent-manager/internal/service"
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
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		fmt.Printf("agent-manager %s (%s)\n", buildinfo.Version, buildinfo.Commit)
		return nil
	}
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		mode = args[0]
		args = args[1:]
	}
	base, e := defaultStateBase()
	if e != nil {
		return e
	}
	fs := flag.NewFlagSet("agent-manager "+mode, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: agent-manager [ui|install|start|stop|status|uninstall|serve|doctor|runtime-install|image-load|version] [options]")
		fmt.Fprintln(fs.Output(), "install registers and starts a current-user login service; uninstall retains all Session data. serve runs in the foreground.")
		fs.PrintDefaults()
	}
	dir := fs.String("state", filepath.Join(base, "agent-manager"), "private supervisor state directory")
	fake := fs.Bool("fake", false, "explicit synthetic test backend (serve or install only)")
	archive := fs.String("archive", "", "OCI or docker-save archive for image-load")
	tag := fs.String("tag", manager.DefaultImage, "local microsandbox cache tag for image-load")
	if e := fs.Parse(args); e != nil {
		if e == flag.ErrHelp {
			return nil
		}
		return e
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *fake && mode != "serve" && mode != "install" && mode != "service-run" {
		return fmt.Errorf("--fake is only valid with serve or install")
	}
	if mode == "ui" {
		return ui.Run(supervisor.NewClient(*dir))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	switch mode {
	case "install", "start", "stop", "status", "uninstall":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		client := supervisor.NewClient(*dir)
		out, err := service.Control(mode, service.Config{State: *dir, Executable: exe, Fake: *fake, Probe: client.Probe, Shutdown: client.Shutdown})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	case "serve", "service-run":
		var b backend.Backend = backend.Microsandbox{}
		if *fake {
			fmt.Fprintln(os.Stderr, "FAKE backend: no microVM, URI Agent, or host commands")
			b = backend.Fake{Dir: filepath.Join(*dir, "fake-vms")}
		}
		serve := func() error {
			return supervisor.Serve(ctx, *dir, func() (*supervisor.Supervisor, error) { return supervisor.New(*dir, b) })
		}
		if mode == "service-run" {
			if err := service.PrepareProcess(); err != nil {
				return fmt.Errorf("prepare service process: %w", err)
			}
			if err := privatefs.EnsureDir(*dir); err != nil {
				return err
			}
			// Only lifecycle diagnostics belong here, never guest terminal output.
			log, err := os.OpenFile(filepath.Join(*dir, "supervisor-service.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			defer log.Close()
			fmt.Fprintf(log, "%s supervisor starting (fake=%t)\n", time.Now().Format(time.RFC3339), *fake)
			err = serve()
			fmt.Fprintf(log, "%s supervisor exited: %v\n", time.Now().Format(time.RFC3339), err)
			return err
		}
		return serve()
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
		fs.Usage()
		return fmt.Errorf("unknown command %q", mode)
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
