package service

import (
	"os"
	"unsafe"

	"github.com/4fuu/agent-manager/internal/privatefs"
	"golang.org/x/sys/windows"
)

// PrepareProcess makes new files belong to the service user. WMI can supply an
// administrative token whose default owner is Administrators rather than its
// user SID. Existing ownership and private ACL checks remain unchanged.
func PrepareProcess() error {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_ADJUST_DEFAULT, &token); err != nil {
		return err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	owner := struct{ SID *windows.SID }{user.User.Sid}
	return windows.SetTokenInformation(token, windows.TokenOwner, (*byte)(unsafe.Pointer(&owner)), uint32(unsafe.Sizeof(owner)))
}

func currentAccount() (string, error) { return privatefs.UserSID() }

func lockControl(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}
