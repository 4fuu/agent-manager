//go:build windows

package backend

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const whvCapabilityCodeHypervisorPresent = 0

var queryHypervisorPresent = windowsHypervisorPresent

func windowsHypervisorPresent() (bool, error) {
	dll := windows.NewLazySystemDLL("WinHvPlatform.dll")
	if err := dll.Load(); err != nil {
		return false, err
	}
	proc := dll.NewProc("WHvGetCapability")
	if err := proc.Find(); err != nil {
		return false, err
	}
	var present uint32
	var written uint32
	hr, _, _ := proc.Call(
		whvCapabilityCodeHypervisorPresent,
		uintptr(unsafe.Pointer(&present)),
		unsafe.Sizeof(present),
		uintptr(unsafe.Pointer(&written)),
	)
	if int32(hr) < 0 {
		return false, fmt.Errorf("WHvGetCapability(HypervisorPresent) failed (HRESULT 0x%08x)", uint32(hr))
	}
	if written < uint32(unsafe.Sizeof(present)) {
		return false, fmt.Errorf("WHvGetCapability returned %d bytes, want %d", written, unsafe.Sizeof(present))
	}
	return present != 0, nil
}

func RuntimeCheck() error {
	present, err := queryHypervisorPresent()
	if err == nil && present {
		return nil
	}
	guidance := "install/enable the Windows optional feature HypervisorPlatform, reboot, and enable hardware virtualization in firmware"
	if err != nil {
		return fmt.Errorf("microsandbox requires Windows Hypervisor Platform; %s: %w", guidance, err)
	}
	return errors.New("microsandbox requires an active Windows hypervisor; " + guidance)
}
