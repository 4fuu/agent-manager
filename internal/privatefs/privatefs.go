// Package privatefs protects manager state and explicit credential files using
// native permissions. Windows ACLs are not represented by os.FileMode.
package privatefs

import (
	"errors"
	"os"
)

func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("state directory must be a directory, not a link")
	}
	return Protect(dir)
}
