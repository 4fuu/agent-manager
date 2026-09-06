package privatefs

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

func UserSID() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}

// Only the current user and LocalSystem may access state. Inheritance is
// protected on this object, while newly created children inherit these ACEs.
func Descriptor() (string, error) {
	sid, err := UserSID()
	if err != nil {
		return "", err
	}
	return "D:P(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)", nil
}

func Protect(path string) error {
	sid, err := UserSID()
	if err != nil {
		return err
	}
	current, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := current.Owner()
	if err != nil {
		return err
	}
	if owner == nil || owner.String() != sid {
		return errors.New("private path must be owned by the current user")
	}
	sddl, err := Descriptor()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func Check(path string) error {
	sid, err := UserSID()
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if owner == nil || owner.String() != sid {
		return errors.New("private path must be owned by the current user")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if dacl == nil {
		return errors.New("private path must have a restricted Windows DACL")
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("private path has an unsupported Windows access rule")
		}
		trustee := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if ace.Mask != 0 && trustee != sid && trustee != "S-1-5-18" {
			return errors.New("private path grants access to another Windows principal; restrict its ACL to the current user and SYSTEM")
		}
	}
	return nil
}

// The caller flushes and closes the temporary file first. MoveFileEx replaces
// within the same volume with write-through; directory Sync is not a Windows API.
func Replace(source, target string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
