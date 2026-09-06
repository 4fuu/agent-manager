package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func psText(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func utf16LE(s string) []byte {
	var b []byte
	for _, c := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	return b
}
func encodedPS(s string) string { return base64.StdEncoding.EncodeToString(utf16LE(s)) }

// Task Scheduler's job forbids microsandbox's mandatory detached breakaway.
// WMI creates a same-user worker outside that job; the task waits for its exit.
// The environment is transferred at launch, never written into the task XML.
func taskLauncher(commandLine, msbHome string) string {
	env := ""
	if msbHome != "" {
		env = "$env:MSB_HOME=" + psText(msbHome) + ";\n"
	}
	return "$commandLine = " + psText(commandLine) + ";\n" + env + `
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$startup = ([wmiclass]'Win32_ProcessStartup').CreateInstance()
$startup.CreateFlags = 150994944
$startup.ShowWindow = 0
$startup.EnvironmentVariables = [string[]]@(Get-ChildItem Env: | ForEach-Object { $_.Name + '=' + $_.Value })
$result = ([wmiclass]'Win32_Process').Create($commandLine, $null, $startup)
if ($result.ReturnValue -ne 0) { throw ('Background worker creation failed: WMI code ' + $result.ReturnValue) }
$worker = [Diagnostics.Process]::GetProcessById($result.ProcessId)
$null = $worker.Handle
$worker.WaitForExit()
if ($null -eq $worker.ExitCode) { throw 'Background worker exit code is unavailable' }
exit $worker.ExitCode
`
}

func (r *registration) powershell(script string) (string, error) {
	return r.run("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodedPS("$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue'; "+script))
}
func (r *registration) taskStatus() (string, error) {
	return r.powershell("$s=New-Object -ComObject Schedule.Service; $s.Connect(); $t=$s.GetFolder('\\').GetTasks(0) | Where-Object { $_.Name -eq " + psText(r.name) + " }; if ($t) { Write-Output ('state='+$t.State+'; last-result='+$t.LastTaskResult) }")
}
func (r *registration) loaded() (bool, error) {
	// Listing a domain distinguishes an absent job from an unavailable launchd.
	out, err := r.run("launchctl", "print", "gui/"+r.account)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[len(fields)-1] == r.name {
			return true, nil
		}
	}
	return false, nil
}
func (r *registration) installed() (bool, error) {
	if r.platform == "windows" {
		out, err := r.taskStatus()
		return out != "", err
	}
	_, err := os.Stat(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
func (r *registration) nativeStatus() (string, error) {
	switch r.platform {
	case "windows":
		status, err := r.taskStatus()
		return strings.NewReplacer("state=4;", "running;", "state=3;", "ready (not running);", "state=2;", "queued;", "state=1;", "disabled;", "state=0;", "unknown;").Replace(status), err
	case "linux":
		return r.run("systemctl", "--user", "show", r.name+".service", "--property=ActiveState,SubState,Result,UnitFileState")
	default:
		loaded, err := r.loaded()
		if err != nil {
			return "", err
		}
		if !loaded {
			return "unloaded", nil
		}
		out, err := r.run("launchctl", "print", "gui/"+r.account+"/"+r.name)
		if err != nil {
			return "", err
		}
		// launchctl also prints the environment; status must not expose credentials.
		lines := []string{"loaded"}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			for _, key := range []string{"state = ", "pid = ", "last exit code = ", "last terminating signal = "} {
				if strings.HasPrefix(line, key) {
					lines = append(lines, line)
				}
			}
		}
		return strings.Join(lines, "; "), nil
	}
}

// Unit arguments use C escapes and % specifiers, not shell quoting. ExecStart's
// colon prefix disables dollar-variable expansion for literal paths.
func unitText(s string) string {
	s = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r", "\t", "\\t", "%", "%%").Replace(s)
	return "\"" + s + "\""
}

// Quote for CreateProcess/CommandLineToArgvW, not PowerShell or cmd.exe.
func windowsCommandLine(args ...string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		var b strings.Builder
		b.WriteByte('"')
		slashes := 0
		for _, c := range arg {
			if c == '\\' {
				slashes++
				continue
			}
			if c == '"' {
				b.WriteString(strings.Repeat("\\", slashes*2+1))
			} else {
				b.WriteString(strings.Repeat("\\", slashes))
			}
			b.WriteRune(c)
			slashes = 0
		}
		b.WriteString(strings.Repeat("\\", slashes*2))
		b.WriteByte('"')
		quoted[i] = b.String()
	}
	return strings.Join(quoted, " ")
}

func (r *registration) definition() string {
	args := []string{r.Executable, "service-run", "--state", r.State}
	if r.Fake {
		args = append(args, "--fake")
	}
	switch r.platform {
	case "linux":
		quoted := make([]string, len(args))
		for i, arg := range args {
			quoted[i] = unitText(arg)
		}
		env := ""
		if r.msbHome != "" {
			env = "Environment=" + unitText("MSB_HOME="+r.msbHome) + "\n"
		}
		// systemd rejects some characters in the executable token even when
		// quoted. A fixed exec wrapper keeps paths in literal positional arguments.
		return "[Unit]\nDescription=Agent Manager user supervisor\n\n[Service]\nExecStart=:/bin/sh -c " + unitText(`exec "$@"`) + " -- " + strings.Join(quoted, " ") + "\n" + env + "Restart=on-failure\nRestartSec=2\nUMask=0077\nKillMode=process\nTimeoutStopSec=30\n\n[Install]\nWantedBy=default.target\n"
	case "darwin":
		var elements strings.Builder
		for _, arg := range args {
			elements.WriteString("<string>" + xmlText(arg) + "</string>")
		}
		env := ""
		if r.msbHome != "" {
			env = "<key>EnvironmentVariables</key><dict><key>MSB_HOME</key><string>" + xmlText(r.msbHome) + "</string></dict>"
		}
		return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>Label</key><string>" + r.name + "</string><key>ProgramArguments</key><array>" + elements.String() + "</array>" + env + "<key>RunAtLoad</key><true/><key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict><key>AbandonProcessGroup</key><true/><key>ExitTimeOut</key><integer>30</integer><key>ThrottleInterval</key><integer>2</integer></dict></plist>\n"
	default:
		host := HostPath(r.Executable)
		worker := windowsCommandLine(append([]string{host}, args...)...)
		command := encodedPS(taskLauncher(worker, r.msbHome))
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		launcher := windowsCommandLine(filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", command)
		return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<Task version=\"1.2\" xmlns=\"http://schemas.microsoft.com/windows/2004/02/mit/task\"><Triggers><LogonTrigger><Enabled>true</Enabled><UserId>" + xmlText(r.account) + "</UserId></LogonTrigger></Triggers><Principals><Principal id=\"User\"><UserId>" + xmlText(r.account) + "</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure></Settings><Actions Context=\"User\"><Exec><Command>" + xmlText(host) + "</Command><Arguments>" + xmlText(launcher) + "</Arguments></Exec></Actions></Task>\n"
	}
}
func (r *registration) register() error {
	definition := r.definition()
	if r.platform == "windows" {
		// schtasks imports XML as UTF-16; a UTF-8 declaration fails on non-ASCII paths.
		definition = "\xff\xfe" + string(utf16LE(strings.Replace(definition, "encoding=\"UTF-8\"", "encoding=\"UTF-16\"", 1)))
	}
	if err := writeRegistration(r.path, definition); err != nil {
		return err
	}
	var err error
	switch r.platform {
	case "windows":
		_, err = r.run("schtasks.exe", "/Create", "/TN", r.name, "/XML", r.path, "/F")
	case "linux":
		if _, err = r.run("systemctl", "--user", "daemon-reload"); err == nil {
			_, err = r.run("systemctl", "--user", "enable", r.name+".service")
		}
	case "darwin":
		_, err = r.run("launchctl", "enable", "gui/"+r.account+"/"+r.name)
	}
	return err
}
func (r *registration) start() error {
	var err error
	switch r.platform {
	case "windows":
		_, err = r.run("schtasks.exe", "/Run", "/TN", r.name)
	case "linux":
		_, err = r.run("systemctl", "--user", "start", r.name+".service")
	case "darwin":
		var loaded bool
		loaded, err = r.loaded()
		if err != nil {
			return err
		}
		if !loaded {
			_, err = r.run("launchctl", "bootstrap", "gui/"+r.account, r.path)
		} else {
			_, err = r.run("launchctl", "kickstart", "gui/"+r.account+"/"+r.name)
		}
	}
	return err
}
func (r *registration) stop() error {
	var err error
	switch r.platform {
	case "windows":
		deadline := time.Now().Add(30 * time.Second)
		for {
			if r.ready() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err = r.Shutdown(ctx)
				cancel()
				if err != nil {
					return err
				}
			}
			state, e := r.taskStatus()
			if e != nil {
				return e
			}
			if !strings.Contains(state, "state=4;") && !strings.Contains(state, "state=2;") {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("scheduled task did not stop gracefully; inspect %s (no process was forcibly killed)", r.State)
			}
			time.Sleep(300 * time.Millisecond)
		}
		// Cancel any delayed scheduler retry after a failed worker, too. The
		// worker has already exited; this does not terminate guests.
		_, err = r.run("schtasks.exe", "/End", "/TN", r.name)
	case "linux":
		var state string
		state, err = r.run("systemctl", "--user", "show", r.name+".service", "--property=ActiveState", "--value")
		if err == nil && state != "inactive" && state != "failed" {
			_, err = r.run("systemctl", "--user", "stop", r.name+".service")
		}
	case "darwin":
		var loaded bool
		loaded, err = r.loaded()
		if err == nil && loaded {
			_, err = r.run("launchctl", "bootout", "gui/"+r.account+"/"+r.name)
		}
	}
	if err != nil {
		return err
	}
	return r.waitReady(false)
}
func (r *registration) remove() error {
	var err error
	switch r.platform {
	case "windows":
		_, err = r.run("schtasks.exe", "/Delete", "/TN", r.name, "/F")
	case "linux":
		_, err = r.run("systemctl", "--user", "disable", r.name+".service")
	}
	if err != nil {
		return err
	}
	if err = os.Remove(r.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if r.platform == "linux" {
		_, err = r.run("systemctl", "--user", "daemon-reload")
	}
	return err
}
