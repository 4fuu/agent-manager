package privatefs

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsRejectsPublicCredentialACL(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "token")
	if err := os.WriteFile(file, []byte("test-fixture-not-a-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Check(file); err != nil {
		t.Fatal("new files did not inherit private ACL", err)
	}
	// os.Chmod(0600) cannot revoke this Windows Everyone read grant.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;OW)(A;;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(file, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if err := Check(file); err == nil {
		t.Fatal("public credential ACL accepted")
	}
	if err := Protect(file); err != nil {
		t.Fatal(err)
	}
	if err := Check(file); err != nil {
		t.Fatal(err)
	}
}
