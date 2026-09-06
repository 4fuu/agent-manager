//go:build !windows

package privatefs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func Protect(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return errors.New("private path must be owned by the current user")
	}
	mode := os.FileMode(0600)
	if st.IsDir() {
		mode = 0700
	}
	return os.Chmod(path, mode)
}

func Check(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Mode().Perm()&0077 != 0 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return errors.New("private path requires current-user ownership and mode 0600 (0700 for directories)")
	}
	return nil
}

func Replace(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
