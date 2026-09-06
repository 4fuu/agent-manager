//go:build linux

package backend

import (
	"errors"
	"os"
)

func RuntimeCheck() error {
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return errors.New("microsandbox requires accessible /dev/kvm on Linux; enable KVM and grant this user read/write access; no host execution fallback")
	}
	return f.Close()
}
