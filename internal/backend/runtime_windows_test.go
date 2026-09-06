//go:build windows

package backend

import (
	"errors"
	"strings"
	"testing"
)

func TestRuntimePrerequisiteCheckWindows(t *testing.T) {
	original := queryHypervisorPresent
	t.Cleanup(func() { queryHypervisorPresent = original })

	queryHypervisorPresent = func() (bool, error) { return true, nil }
	if err := RuntimeCheck(); err != nil {
		t.Fatalf("present hypervisor rejected: %v", err)
	}

	for _, query := range []func() (bool, error){
		func() (bool, error) { return false, nil },
		func() (bool, error) { return false, errors.New("missing DLL") },
	} {
		queryHypervisorPresent = query
		err := RuntimeCheck()
		if err == nil || !strings.Contains(err.Error(), "HypervisorPlatform") || !strings.Contains(err.Error(), "reboot") {
			t.Fatalf("error is not actionable: %v", err)
		}
	}
}

func TestWindowsHypervisorCapabilityNative(t *testing.T) {
	present, err := windowsHypervisorPresent()
	if err != nil {
		t.Logf("Windows Hypervisor Platform capability unavailable: %v", err)
		return
	}
	t.Logf("Windows Hypervisor Platform HypervisorPresent=%t", present)
}
