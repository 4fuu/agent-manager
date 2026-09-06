// The service host must be linked with -H=windowsgui. Task Scheduler starts it
// without allocating a console; each child is also created without a console.
package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/4fuu/agent-manager/internal/service"
)

func main() {
	if len(os.Args) < 2 || !filepath.IsAbs(os.Args[1]) {
		os.Exit(1)
	}
	cmd := exec.Command(os.Args[1], os.Args[2:]...)
	service.NoConsole(cmd)
	// Nil streams attach to NUL, not inherited console handles. Diagnostics from
	// the supervisor belong in its lifecycle log, never a guest terminal log.
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() > 0 {
			os.Exit(exit.ExitCode())
		}
		os.Exit(1)
	}
}
