//go:build darwin

package backend

import (
	"errors"
	"runtime"
)

func RuntimeCheck() error {
	if runtime.GOARCH != "arm64" {
		return errors.New("microsandbox on macOS requires an arm64 (Apple silicon) host; this host is unsupported")
	}
	return nil
}
