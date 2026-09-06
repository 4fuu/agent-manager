package service

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func decodePS(t *testing.T, encoded string) string {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u))
}

func TestDefinitionsPreserveArgumentsAndGuestLifetimes(t *testing.T) {
	for _, platform := range []string{"windows", "linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			r := registration{Config: Config{Executable: `/a b/'中文&$x%/agent`, State: `/s '中文&$x%`, Fake: true}, platform: platform, name: "agent-manager-test", account: "501", msbHome: `/m '中文&$x%`}
			text := r.definition()
			if platform == "linux" {
				for _, want := range []string{`ExecStart=:/bin/sh -c "exec \"$@\"" -- "/a b/'中文&$x%%/agent"`, `"--state" "/s '中文&$x%%" "--fake"`, `Environment="MSB_HOME=/m '中文&$x%%"`, "KillMode=process", "Restart=on-failure"} {
					if !strings.Contains(text, want) {
						t.Fatalf("missing %q in %s", want, text)
					}
				}
				return
			}
			decoder := xml.NewDecoder(strings.NewReader(text))
			for {
				_, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if platform == "windows" {
				var task struct {
					Command   string `xml:"Actions>Exec>Command"`
					Arguments string `xml:"Actions>Exec>Arguments"`
				}
				if err := xml.Unmarshal([]byte(text), &task); err != nil {
					t.Fatal(err)
				}
				args := strings.Fields(task.Arguments)
				launcher := decodePS(t, strings.Trim(args[len(args)-1], "\""))
				if !strings.Contains(launcher, "$startup.CreateFlags = 150994944") || !strings.Contains(launcher, "$worker.WaitForExit()") {
					t.Fatal("worker must escape the scheduler job and be supervised")
				}
				if task.Command != HostPath(r.Executable) {
					t.Fatal("task must start the GUI-subsystem host")
				}
				worker := windowsCommandLine(HostPath(r.Executable), r.Executable, "service-run", "--state", r.State, "--fake")
				if !strings.Contains(launcher, "$commandLine = "+psText(worker)) || !strings.Contains(launcher, "$env:MSB_HOME="+psText(r.msbHome)) {
					t.Fatal("lost worker arguments or environment", launcher)
				}
				for _, value := range []string{"InteractiveToken", "LeastPrivilege", "IgnoreNew", "<ExecutionTimeLimit>PT0S"} {
					if !strings.Contains(text, value) {
						t.Fatal("missing", value)
					}
				}
			} else {
				var plist struct {
					Args []string `xml:"dict>array>string"`
				}
				if err := xml.Unmarshal([]byte(text), &plist); err != nil {
					t.Fatal(err)
				}
				if len(plist.Args) != 5 || plist.Args[0] != r.Executable || plist.Args[3] != r.State || plist.Args[4] != "--fake" {
					t.Fatal(plist.Args)
				}
				if !strings.Contains(text, "<key>AbandonProcessGroup</key><true/>") {
					t.Fatal("would kill guest process group")
				}
			}
		})
	}
}

type fakeNative struct {
	installed, loaded, ready bool
	starts, stops, removals  int
	fail                     string
}

func fakeRegistration(t *testing.T, platform string) (*registration, *fakeNative) {
	t.Helper()
	n := &fakeNative{}
	r := &registration{Config: Config{State: t.TempDir(), Executable: "/agent", Fake: true}, platform: platform, name: "agent-manager-test", account: "501"}
	r.path = filepath.Join(r.State, "registration")
	r.Probe = func(context.Context) error {
		if n.ready {
			return nil
		}
		return errors.New("offline")
	}
	r.Shutdown = func(context.Context) error { n.stops++; n.ready = false; return nil }
	r.run = func(command string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if n.fail != "" && strings.Contains(joined, n.fail) {
			return "", errors.New("native operation failed")
		}
		switch command {
		case "powershell.exe":
			if !n.installed {
				return "", nil
			}
			if n.ready {
				return "state=4; last-result=0", nil
			}
			return "state=3; last-result=0", nil
		case "schtasks.exe":
			switch args[0] {
			case "/Create":
				n.installed = true
			case "/Run":
				n.ready = true
				n.starts++
			case "/Delete":
				n.installed = false
				n.removals++
			}
		case "systemctl":
			switch args[1] {
			case "start":
				n.ready = true
				n.starts++
			case "stop":
				n.ready = false
				n.stops++
			case "disable":
				n.removals++
			case "show":
				return "ActiveState=active", nil
			}
		case "launchctl":
			switch args[0] {
			case "print":
				if n.loaded {
					return "services = {\n 123 0 " + r.name + "\n}\n", nil
				}
				return "services = {\n}\n", nil
			case "bootstrap", "kickstart":
				n.loaded = true
				n.ready = true
				n.starts++
			case "bootout":
				n.loaded = false
				n.ready = false
				n.stops++
			}
		default:
			t.Fatal("unexpected command", command, args)
		}
		return "", nil
	}
	return r, n
}

func TestLifecycleAllPlatforms(t *testing.T) {
	for _, platform := range []string{"windows", "linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			r, n := fakeRegistration(t, platform)
			if _, err := r.control("start"); err == nil {
				t.Fatal("started without installation")
			}
			for _, action := range []string{"status", "stop", "uninstall", "install", "start", "install", "stop", "stop", "start", "status", "uninstall", "uninstall"} {
				if _, err := r.control(action); err != nil {
					t.Fatalf("%s: %v", action, err)
				}
			}
			if n.starts != 3 || n.ready {
				t.Fatalf("starts=%d ready=%t", n.starts, n.ready)
			}
			if _, err := os.Stat(r.State); err != nil {
				t.Fatal("deleted state", err)
			}
			if installed, err := r.installed(); err != nil || installed {
				t.Fatal(installed, err)
			}
		})
	}
}

func TestFailuresDoNotRemoveRegistration(t *testing.T) {
	for _, platform := range []string{"windows", "linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			r, n := fakeRegistration(t, platform)
			n.ready = true
			if _, err := r.control("install"); err == nil {
				t.Fatal("replaced foreground supervisor")
			}
			n.ready = false
			if err := r.register(); err != nil {
				t.Fatal(err)
			}
			n.fail = map[string]string{"windows": "/Run", "linux": "start", "darwin": "bootstrap"}[platform]
			if _, err := r.control("start"); err == nil {
				t.Fatal("swallowed start failure")
			}
			if installed, _ := r.installed(); !installed {
				t.Fatal("lost registration after failure")
			}
			n.fail = ""
			if _, err := r.control("start"); err != nil {
				t.Fatal(err)
			}
			if platform == "windows" {
				r.Shutdown = func(context.Context) error { return errors.New("IPC failure") }
			} else {
				n.fail = map[string]string{"linux": "stop", "darwin": "bootout"}[platform]
			}
			if _, err := r.control("uninstall"); err == nil {
				t.Fatal("swallowed stop failure")
			}
			if installed, _ := r.installed(); !installed {
				t.Fatal("removed registration without stopping")
			}
		})
	}
}

func TestControlLockAndStableIdentity(t *testing.T) {
	t.Setenv("MSB_HOME", "relative runtime")
	c := Config{State: t.TempDir(), Executable: "agent-manager"}
	r, err := newRegistration(c)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(r.msbHome) {
		t.Fatal("runtime home changed with service working directory")
	}
	c.Executable = "new-agent-manager"
	other, err := newRegistration(c)
	if err != nil || r.name != other.name {
		t.Fatal("executable change altered service identity", err)
	}
	f, err := os.OpenFile(filepath.Join(c.State, "service-control.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = lockControl(f); err != nil {
		t.Fatal(err)
	}
	if _, err = Control("install", c); err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatal("concurrent mutation allowed", err)
	}
}

func TestStoppedWaitsForLifetimeLock(t *testing.T) {
	r, _ := fakeRegistration(t, "linux")
	f, err := os.OpenFile(filepath.Join(r.State, "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := lockControl(f); err != nil {
		t.Fatal(err)
	}
	if r.stopped() {
		t.Fatal("reported stopped while guest clients are detaching")
	}
	f.Close()
	if !r.stopped() {
		t.Fatal("reported running after lifetime lock release")
	}
}

func TestNativeStatusErrorsAndPrivacy(t *testing.T) {
	r, _ := fakeRegistration(t, "darwin")
	r.run = func(_ string, args ...string) (string, error) {
		if args[1] == "gui/501" {
			return "services = {\n 123 0 " + r.name + "\n}\n", nil
		}
		return "state = running\npid = 123\nenvironment = {\nTOKEN = secret-value\n}\n", nil
	}
	status, err := r.nativeStatus()
	if err != nil || !strings.Contains(status, "state = running") || strings.Contains(status, "secret") {
		t.Fatal(status, err)
	}
	for _, platform := range []string{"windows", "darwin", "linux"} {
		r.platform = platform
		r.run = func(string, ...string) (string, error) { return "", errors.New("access denied") }
		if _, err := r.nativeStatus(); err == nil {
			t.Fatal("hid native status failure on", platform)
		}
	}
}

func TestTaskRegistrationUsesUTF16(t *testing.T) {
	r, _ := fakeRegistration(t, "windows")
	if err := r.register(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(r.path)
	if err != nil || len(b) < 2 || b[0] != 0xff || b[1] != 0xfe {
		t.Fatal("missing UTF-16 BOM", err)
	}
	xml := decodePS(t, base64.StdEncoding.EncodeToString(b[2:]))
	if !strings.Contains(xml, `encoding="UTF-16"`) {
		t.Fatal("wrong XML encoding")
	}
}
