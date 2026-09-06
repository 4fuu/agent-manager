//go:build !linux && !windows && !darwin

package backend

import (
	"fmt"
	"runtime"
)

func RuntimeCheck() error {
	return fmt.Errorf("microsandbox is unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
}
