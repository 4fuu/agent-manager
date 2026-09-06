// Package service manages the current user's native login service, not guest commands.
package service

import (
	"context"
	"crypto/sha256"
	"debug/pe"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/4fuu/agent-manager/internal/buildinfo"
	"github.com/4fuu/agent-manager/internal/privatefs"
)

// HostPath is versioned so an installer can publish the helper before atomically
// replacing the CLI without altering a previous release's running service host.
func HostPath(executable string) string {
	return filepath.Join(filepath.Dir(executable), "agent-manager-service-"+buildinfo.Version+".exe")
}

func validateHost(executable string) error {
	path := HostPath(executable)
	f, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("Windows service host unavailable at %s; reinstall the complete Windows bundle: %w", path, err)
	}
	defer f.Close()
	h, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || h.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI || f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return fmt.Errorf("%s must be the Windows amd64 GUI-subsystem service host", path)
	}
	return nil
}

type Config struct {
	State, Executable string
	Fake              bool
	Probe             func(context.Context) error
	Shutdown          func(context.Context) error
}

type registration struct {
	Config
	platform, home, account, name, path, msbHome string
	run                                          func(string, ...string) (string, error)
}

func newRegistration(c Config) (*registration, error) {
	state, err := filepath.Abs(c.State)
	if err != nil {
		return nil, err
	}
	if err = privatefs.EnsureDir(state); err != nil {
		return nil, err
	}
	state, err = filepath.EvalSymlinks(state)
	if err != nil {
		return nil, err
	}
	c.State = state
	c.Executable, err = filepath.Abs(c.Executable)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	account, err := currentAccount()
	if err != nil {
		return nil, err
	}
	key := state
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	r := &registration{Config: c, platform: runtime.GOOS, home: home, account: account,
		name: fmt.Sprintf("agent-manager-%x", sha256.Sum256([]byte(key)))[:30], msbHome: os.Getenv("MSB_HOME"), run: runCommand}
	if r.msbHome != "" {
		r.msbHome, err = filepath.Abs(r.msbHome)
		if err != nil {
			return nil, err
		}
	}
	switch r.platform {
	case "windows":
		r.path = filepath.Join(state, "login-task.xml")
	case "linux":
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		if !filepath.IsAbs(base) {
			return nil, errors.New("XDG_CONFIG_HOME must be absolute")
		}
		r.path = filepath.Join(base, "systemd", "user", r.name+".service")
	case "darwin":
		r.path = filepath.Join(home, "Library", "LaunchAgents", r.name+".plist")
	default:
		return nil, fmt.Errorf("background services are unsupported on %s", r.platform)
	}
	return r, nil
}

func runCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	NoConsole(cmd)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return string(b), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}

// Control serializes registration changes for a state directory. Installation
// never relocates state or runtime caches, and uninstall never deletes them.
func Control(action string, c Config) (string, error) {
	// Reject an incomplete bundle before stopping or replacing a registration.
	if action == "install" && runtime.GOOS == "windows" {
		if err := validateHost(c.Executable); err != nil {
			return "", err
		}
	}
	r, err := newRegistration(c)
	if err != nil {
		return "", err
	}
	lock, err := os.OpenFile(filepath.Join(r.State, "service-control.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err = lockControl(lock); err != nil {
		return "", errors.New("another service operation is in progress")
	}
	return r.control(action)
}

func (r *registration) ready() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	return r.Probe(ctx) == nil
}

func (r *registration) waitReady(want bool) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		if (want && r.ready()) || (!want && r.stopped()) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("supervisor readiness did not become %t within 30s; inspect %s and native service status", want, filepath.Join(r.State, "supervisor-service.log"))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (r *registration) stopped() bool {
	if r.ready() {
		return false
	}
	// IPC closes before guest clients finish detaching. Wait for the lifetime
	// lock too, so start/reinstall cannot race the previous supervisor's cleanup.
	f, err := os.OpenFile(filepath.Join(r.State, "supervisor.lock"), os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	defer f.Close()
	return lockControl(f) == nil
}

func (r *registration) control(action string) (string, error) {
	installed, err := r.installed()
	if err != nil {
		return "", err
	}
	if action == "status" {
		detail := "not installed"
		if installed {
			detail, err = r.nativeStatus()
			if err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("Service: %s\nInstalled: %t\nSupervisor ready: %t\nNative status: %s\nState: %s\nLog: %s", r.name, installed, r.ready(), detail, r.State, filepath.Join(r.State, "supervisor-service.log")), nil
	}
	if action == "install" {
		if installed {
			if err = r.stop(); err != nil {
				return "", err
			}
		} else if r.ready() {
			return "", errors.New("a foreground supervisor owns this state; stop it before installing the login service")
		}
		if err = r.register(); err != nil {
			return "", err
		}
		installed = true
	}
	if !installed {
		if action == "stop" || action == "uninstall" {
			return "Service is not installed; no data changed.", nil
		}
		return "", errors.New("service is not installed; run agent-manager install with the same --state directory")
	}
	switch action {
	case "install", "start":
		if !r.ready() {
			if err = r.start(); err != nil {
				return "", err
			}
			if err = r.waitReady(true); err != nil {
				return "", err
			}
		}
		return "User service started; launch agent-manager to connect.", nil
	case "stop", "uninstall":
		if err = r.stop(); err != nil {
			return "", err
		}
		if action == "uninstall" {
			if err = r.remove(); err != nil {
				return "", err
			}
			return "User service uninstalled; state, images and Session disks retained.", nil
		}
		return "Supervisor stopped; Session disks retained. Login autostart remains installed.", nil
	default:
		return "", fmt.Errorf("unknown service action %q", action)
	}
}

func writeRegistration(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".agent-manager-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	if _, err = f.WriteString(text); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return privatefs.Replace(tmp, path)
}
