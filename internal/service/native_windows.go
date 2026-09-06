package service

import (
	"os"

	"github.com/4fuu/agent-manager/internal/privatefs"
	"golang.org/x/sys/windows"
)

func currentAccount() (string, error) { return privatefs.UserSID() }

func lockControl(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}
