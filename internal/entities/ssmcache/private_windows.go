package ssmcache

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

func secureCreated(f *os.File) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	// Only the current service identity, SYSTEM and administrators. Protect
	// inheritance when creating the owner directory; children inherit this ACL.
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	// Go's portable handle lacks WRITE_DAC. Resolve its kernel-selected final
	// name, open a metadata handle without following a reparse point, and prove
	// file identity before changing its DACL. A pathname race therefore cannot
	// authorize a different object. ReOpenFile is unsuitable for Go directory
	// handles (including os.Root's native handles).
	info, err := f.Stat()
	if err != nil {
		return err
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if info.IsDir() {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	buffer := make([]uint16, 32768)
	size, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buffer[0], uint32(len(buffer)), 0)
	if err != nil || size >= uint32(len(buffer)) {
		return errors.New("SSM_CACHE_STORAGE_UNAVAILABLE")
	}
	name, err := windows.UTF16PtrFromString(windows.UTF16ToString(buffer[:size]))
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.WRITE_DAC, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return err
	}
	opened := os.NewFile(uintptr(handle), "ssm-cache-metadata")
	defer opened.Close()
	actual, err := opened.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return errors.New("SSM_CACHE_STORAGE_CHANGED")
	}
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func checkPrivate(f *os.File) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	descriptor, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	trusted := map[string]bool{user.User.Sid.String(): true, "S-1-5-18": true, "S-1-5-32-544": true}
	if owner == nil || !trusted[owner.String()] {
		return errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
	}
	// Use the supported descriptor serialization, not private ACL fields or
	// unsafe casts. Unknown/object/conditional ACE shapes fail closed.
	text := descriptor.String()
	start := strings.Index(text, "D:")
	if start < 0 {
		return errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
	}
	text = text[start+2:]
	if end := strings.Index(text, "S:"); end >= 0 {
		text = text[:end]
	}
	count := 0
	for {
		start = strings.IndexByte(text, '(')
		if start < 0 {
			break
		}
		end := strings.IndexByte(text[start:], ')')
		if end < 0 {
			return errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
		}
		end += start
		fields := strings.Split(text[start+1:end], ";")
		if len(fields) != 6 || fields[0] != "A" {
			return errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
		}
		if !trustedSDDLTrustee(fields[5], trusted) {
			return errors.New("SSM_CACHE_STORAGE_UNTRUSTED_ACE")
		}
		count++
		text = text[end+1:]
	}
	if count == 0 {
		return errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 {
		return errors.New("SSM_CACHE_STORAGE_MULTIPLE_LINKS")
	}
	return nil
}

func trustedSDDLTrustee(value string, trusted map[string]bool) bool {
	if trusted[value] {
		return true
	}
	// Windows serializes well-known SIDs as two-letter SDDL aliases, including
	// LA for a local Administrator account. Resolve the OS's representation
	// before comparison; an alias grants no authority beyond the exact SID.
	if len(value) != 2 {
		return false
	}
	descriptor, err := windows.SecurityDescriptorFromString("O:" + value)
	if err != nil {
		return false
	}
	owner, _, err := descriptor.Owner()
	return err == nil && owner != nil && trusted[owner.String()]
}
