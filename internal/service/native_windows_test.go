package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsCommandLineRoundTrip(t *testing.T) {
	args := []string{`C:\Program Files\host.exe`, "", `a b'中文&$%=`, `quote"slash\`, `\\server\share\`, "line\nbreak"}
	actual, err := windows.DecomposeCommandLine(windowsCommandLine(args...))
	if err != nil || !reflect.DeepEqual(args, actual) {
		t.Fatalf("%q != %q: %v", args, actual, err)
	}
}

func TestWindowsNativeCommandsNeverAllocateConsole(t *testing.T) {
	cmd := exec.Command("unused")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	NoConsole(cmd)
	if cmd.SysProcAttr.CreationFlags != windows.CREATE_NEW_PROCESS_GROUP|windows.CREATE_NO_WINDOW {
		t.Fatal("console flag missing or existing flags lost")
	}
	out, err := runCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `
Add-Type -TypeDefinition 'using System; using System.Runtime.InteropServices; public static class ConsoleProbe { [DllImport("kernel32.dll")] public static extern IntPtr GetConsoleWindow(); }'
if ([ConsoleProbe]::GetConsoleWindow() -ne [IntPtr]::Zero) { throw 'A console was allocated' }
Write-Output 'no-console'
`)
	if err != nil || out != "no-console" {
		t.Fatalf("native command console or output regression: %s %v", out, err)
	}
}

func TestMissingHostDoesNotModifyRegistration(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	_, err := Control("install", Config{State: state, Executable: filepath.Join(dir, "agent-manager.exe")})
	if err == nil || !strings.Contains(err.Error(), "complete Windows bundle") {
		t.Fatal("missing helper accepted", err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("incomplete installation mutated state")
	}
}
