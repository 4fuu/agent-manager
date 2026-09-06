//go:build !windows

package supervisor

import (
	"context"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func lockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func listen(dir string) (net.Listener, error) {
	name := filepath.Join(dir, "supervisor.sock")
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	ln, err := net.Listen("unix", name)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(name, 0600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func dial(ctx context.Context, dir string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "supervisor.sock"))
}
