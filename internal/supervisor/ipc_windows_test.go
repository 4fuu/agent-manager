package supervisor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/4fuu/agent-manager/internal/privatefs"
)

func TestWindowsLockReleasedAfterProcessExit(t *testing.T) {
	if file := os.Getenv("AGENT_MANAGER_TEST_LOCK"); file != "" {
		f, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := lockFile(f); err != nil {
			t.Fatal(err)
		}
		fmt.Println("locked")
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
		return
	}
	file := filepath.Join(t.TempDir(), "supervisor.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsLockReleasedAfterProcessExit$")
	cmd.Env = append(os.Environ(), "AGENT_MANAGER_TEST_LOCK="+file)
	stdin, _ := cmd.StdinPipe()
	defer stdin.Close()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "locked" {
		t.Fatal("lock helper did not start", scanner.Text())
	}
	f, err := os.OpenFile(file, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := lockFile(f); err == nil {
		t.Fatal("another process acquired active lock")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if err := lockFile(f); err != nil {
		t.Fatal("crashed process retained lock", err)
	}
}

func TestWindowsPipeIdentityAndExclusivity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state 中文")
	if err := privatefs.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	a, err := endpoint(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := endpoint(strings.ToUpper(dir))
	if err != nil || a != b {
		t.Fatal("case alias changed Windows endpoint", a, b, err)
	}
	ln, err := listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if duplicate, err := listen(dir); err == nil {
		duplicate.Close()
		t.Fatal("duplicate named-pipe server accepted")
	}
	accepted := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			<-accepted
		}
	}()
	defer close(accepted)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dial(ctx, dir)
	if err != nil {
		t.Fatal("same-user server authentication failed", err)
	}
	defer conn.Close()
	if err := authenticatePipe(conn, "S-1-5-18"); err == nil {
		t.Fatal("server accepted with a different expected identity")
	}
}
