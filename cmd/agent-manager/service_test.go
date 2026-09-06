package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/4fuu/agent-manager/internal/backend"
	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/4fuu/agent-manager/internal/service"
	"github.com/4fuu/agent-manager/internal/supervisor"
)

// Opt-in: creates and removes a native login registration for a temporary state
// directory. Guests are synthetic unless an explicit live image is supplied.
func TestNativeLoginServiceLifecycle(t *testing.T) {
	if os.Getenv("AGENT_MANAGER_SERVICE_TEST") != "1" {
		t.Skip("set AGENT_MANAGER_SERVICE_TEST=1 to exercise native login registration")
	}
	var base string
	if runtime.GOOS == "windows" {
		base = t.TempDir()
	} else {
		// macOS's default test temp path can exceed the AF_UNIX socket limit.
		var err error
		base, err = os.MkdirTemp("/tmp", "am-svc-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(base); err != nil {
				t.Error(err)
			}
		})
	}
	dir := filepath.Join(base, "service '中文 & $HOME %=path")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "agent-manager")
	buildArgs := []string{"build", "-o"}
	if runtime.GOOS == "windows" {
		exe += ".exe"
		// Match release bundles: login tasks do not inherit the build shell's
		// compiler DLL search path.
		buildArgs = []string{"build", "-ldflags", "-extldflags=-static", "-o"}
	}
	if published := os.Getenv("AGENT_MANAGER_SERVICE_BINARY"); published != "" {
		files := map[string]string{published: exe}
		if runtime.GOOS == "windows" {
			files[service.HostPath(published)] = service.HostPath(exe)
		}
		for from, to := range files {
			b, err := os.ReadFile(from)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(to, b, 0700); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		if out, err := exec.Command("go", append(buildArgs, exe, ".")...).CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
		if runtime.GOOS == "windows" {
			cmd := exec.Command("go", "build", "-ldflags", "-H=windowsgui -s -w", "-o", service.HostPath(exe), "../agent-manager-service")
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("build GUI service host: %v\n%s", err, out)
			}
		}
	}
	state := filepath.Join(dir, "state")
	liveImage := os.Getenv("AGENT_MANAGER_SERVICE_LIVE_IMAGE")
	installArgs := []string{"--fake"}
	if liveImage == "" {
		t.Setenv("MSB_HOME", filepath.Join(dir, "runtime"))
	} else {
		installArgs = nil
	}
	command := func(action string, extra ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		args := append([]string{action, "--state", state}, extra...)
		cmd := exec.CommandContext(ctx, exe, args...)
		service.NoConsole(cmd)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	defer func() {
		if out, err := command("uninstall"); err != nil {
			t.Errorf("cleanup: %v\n%s", err, out)
		}
	}()
	call := func(action string, extra ...string) string {
		t.Helper()
		out, err := command(action, extra...)
		if err != nil {
			status, statusErr := command("status")
			log, logErr := os.ReadFile(filepath.Join(state, "supervisor-service.log"))
			t.Logf("failure status: %s (%v)\nlifecycle log: %s (%v)", status, statusErr, log, logErr)
			t.Fatalf("%s: %v\n%s", action, err, out)
		}
		t.Log(action + ": " + out)
		return out
	}
	if out := call("status"); !strings.Contains(out, "Installed: false") {
		t.Fatal(out)
	}
	if _, err := command("start"); err == nil {
		t.Fatal("start succeeded without registration")
	}
	call("install", installArgs...)
	call("start")
	if out := call("status"); !strings.Contains(out, "Supervisor ready: true") {
		t.Fatal(out)
	}
	if runtime.GOOS == "windows" {
		// Probe from a disposable process so the test runner keeps its console.
		script := "$ErrorActionPreference='Stop'; $exePath='" + strings.ReplaceAll(exe, "'", "''") + "'; $path='" + strings.ReplaceAll(filepath.Join(state, "login-task.xml"), "'", "''") + "';\n" + `
[xml]$task = Get-Content -LiteralPath $path -Raw
$encoded = $task.Task.Actions.Exec.Arguments.Split(' ')[-1].Trim('"')
$processes = @(Get-CimInstance Win32_Process | Where-Object { $_.ProcessId -ne $PID -and ($_.ExecutablePath -eq $task.Task.Actions.Exec.Command -or $_.ExecutablePath -eq $exePath -or ($_.CommandLine -and $_.CommandLine.TrimEnd('"').EndsWith($encoded))) })
if ($processes.Count -ne 4) { throw ('Expected host, PowerShell, worker host and supervisor; found ' + $processes.Count) }
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class LauncherConsoleProbe {
    [DllImport("kernel32.dll")] static extern bool FreeConsole();
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool AttachConsole(uint pid);
    public static void Check(uint pid) {
        FreeConsole();
        if (AttachConsole(pid)) {
            FreeConsole();
            throw new Exception("Service process allocated a console: " + pid);
        }
        if (Marshal.GetLastWin32Error() != 6) throw new Exception("Console probe failed");
    }
}
'@
foreach ($process in $processes) { [LauncherConsoleProbe]::Check($process.ProcessId) }
`
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
		service.NoConsole(cmd)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("headless launcher: %v\n%s", err, out)
		}
	}
	client := supervisor.NewClient(state)
	if _, err := client.Call(supervisor.Request{Action: "project", Project: manager.Project{Name: "retained project", URL: "https://github.com/octocat/Hello-World"}}); err != nil {
		t.Fatal(err)
	}
	var guestID, runtimeID, bootID string
	readBoot := func() string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		vm, err := (backend.Microsandbox{}).Open(ctx, "am-"+guestID, runtimeID, false)
		if err != nil {
			t.Fatal("guest did not survive supervisor stop", err)
		}
		defer vm.Release()
		var out bytes.Buffer
		if err := vm.Run(ctx, "/", "cat /proc/sys/kernel/random/boot_id", &out); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out.String())
	}
	if liveImage != "" {
		if _, err := client.Call(supervisor.Request{Action: "image", Profile: manager.ImageProfile{ID: "live", Name: "Service live fixture", Image: liveImage, Command: "/bin/bash -l"}}); err != nil {
			t.Fatal(err)
		}
		s, err := client.Call(supervisor.Request{Action: "state"})
		if err != nil {
			t.Fatal(err)
		}
		created, err := client.Call(supervisor.Request{Action: "create", ID: s.State.Projects[0].ID, Image: "live"})
		if err != nil {
			t.Fatal(err)
		}
		guestID = created.ID
		defer func() {
			if out, err := command("stop"); err != nil {
				t.Errorf("stop before guest cleanup: %v %s", err, out)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := (backend.Microsandbox{}).Control(ctx, "am-"+guestID, runtimeID, true); err != nil {
				t.Error(err)
			}
		}()
		if _, err := client.Call(supervisor.Request{Action: "start", ID: guestID}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Minute)
		for {
			s, err := client.Call(supervisor.Request{Action: "state"})
			if err != nil {
				t.Fatal(err)
			}
			in := s.State.Instances[0]
			if in.Status == "running" {
				runtimeID = in.RuntimeID
				break
			}
			if in.Status == "failed" || time.Now().After(deadline) {
				frame, _ := client.Call(supervisor.Request{Action: "frame", ID: guestID})
				t.Fatalf("guest startup: %+v\n%s", in, frame.Frame.Logs)
			}
			time.Sleep(100 * time.Millisecond)
		}
		bootID = readBoot()
		if bootID == "" {
			t.Fatal("empty guest boot ID")
		}
	}
	call("install", installArgs...)
	call("stop")
	call("stop")
	if out := call("status"); !strings.Contains(out, "Installed: true") || !strings.Contains(out, "Supervisor ready: false") {
		t.Fatal(out)
	}
	log, err := os.ReadFile(filepath.Join(state, "supervisor-service.log"))
	if err != nil || !strings.Contains(string(log), "supervisor exited: <nil>") {
		t.Fatalf("unclean shutdown: %s %v", log, err)
	}
	if liveImage != "" && readBoot() != bootID {
		t.Fatal("stopping service restarted the guest")
	}
	call("start")
	reply, err := client.Call(supervisor.Request{Action: "state"})
	if err != nil || len(reply.State.Projects) != 1 || reply.State.Projects[0].Name != "retained project" {
		t.Fatalf("lost state: %+v %v", reply, err)
	}
	if liveImage != "" && (reply.State.Instances[0].RuntimeID != runtimeID || readBoot() != bootID) {
		t.Fatal("restart lost runtime identity")
	}
	call("uninstall")
	if liveImage != "" && readBoot() != bootID {
		t.Fatal("uninstall killed the guest")
	}
	call("uninstall")
	if out := call("status"); !strings.Contains(out, "Installed: false") || !strings.Contains(out, "Supervisor ready: false") {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(state, "state.json")); err != nil {
		t.Fatal("uninstall removed state", err)
	}
	if liveImage == "" {
		// A registered task/unit is not success if the supervisor itself fails.
		path := filepath.Join(state, "state.json")
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("invalid-json"), 0600); err != nil {
			t.Fatal(err)
		}
		if out, err := command("install", installArgs...); err == nil || !strings.Contains(out, "within 30s") {
			t.Fatalf("startup failure was not reported: %v %s", err, out)
		}
		if out := call("status"); !strings.Contains(out, "Supervisor ready: false") {
			t.Fatal(out)
		} else if runtime.GOOS == "windows" && !strings.Contains(out, "last-result=1") {
			t.Fatal("task lost worker failure exit code", out)
		}
		call("stop")
		if runtime.GOOS == "windows" {
			logPath := filepath.Join(state, "supervisor-service.log")
			before, err := os.Stat(logPath)
			if err != nil {
				t.Fatal(err)
			}
			// Cross the scheduler's one-minute failure retry deadline. Stop must
			// cancel it, not merely observe that the failed worker is absent.
			time.Sleep(40 * time.Second)
			after, err := os.Stat(logPath)
			if err != nil || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("stopped task restarted its failed worker", err)
			}
		}
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		call("install", installArgs...)
		call("uninstall")
	}
}

func TestLifecycleCLIValidation(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	for _, args := range [][]string{{"start", "--fake"}, {"stop", "extra"}, {"install", "extra"}} {
		os.Args = append([]string{"agent-manager"}, args...)
		if err := run(); err == nil {
			t.Fatal("accepted", args)
		}
	}
}
