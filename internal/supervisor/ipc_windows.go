package supervisor

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/4fuu/agent-manager/internal/privatefs"
	winio "github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func endpoint(dir string) (string, error) {
	path, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	sid, err := privatefs.UserSID()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(sid + "\x00" + strings.ToLower(path)))
	return fmt.Sprintf(`\\.\pipe\agent-manager-%x`, sum), nil
}

func lockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &windows.Overlapped{})
}

func listen(dir string) (net.Listener, error) {
	name, err := endpoint(dir)
	if err != nil {
		return nil, err
	}
	sddl, err := privatefs.Descriptor()
	if err != nil {
		return nil, err
	}
	// go-winio reserves the first instance and rejects remote pipe clients.
	return winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: sddl})
}

func dial(ctx context.Context, dir string) (net.Conn, error) {
	name, err := endpoint(dir)
	if err != nil {
		return nil, err
	}
	conn, err := winio.DialPipeContext(ctx, name)
	if err != nil {
		return nil, err
	}
	sid, err := privatefs.UserSID()
	if err == nil {
		err = authenticatePipe(conn, sid)
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// A private DACL prevents unauthorized clients but not pipe-name squatting.
// Verify the connected server before sending any project or terminal payload.
func authenticatePipe(conn net.Conn, expectedSID string) error {
	f, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return errors.New("named-pipe connection does not expose its native handle")
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(f.Fd()), &pid); err != nil {
		return err
	}
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(p)
	var token windows.Token
	if err := windows.OpenProcessToken(p, windows.TOKEN_QUERY, &token); err != nil {
		return err
	}
	defer token.Close()
	u, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	if u.User.Sid.String() != expectedSID {
		return errors.New("named-pipe server is not owned by the current user")
	}
	return nil
}
