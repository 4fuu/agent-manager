//go:build !windows

package service

import (
	"os"
	"os/exec"
	"strconv"

	"golang.org/x/sys/unix"
)

// NoConsole is unnecessary on Unix; exec does not allocate terminal windows.
func NoConsole(cmd *exec.Cmd) {}

// PrepareProcess is unnecessary on Unix, where new files already use the UID.
func PrepareProcess() error { return nil }

func currentAccount() (string, error) { return strconv.Itoa(os.Getuid()), nil }

func lockControl(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
