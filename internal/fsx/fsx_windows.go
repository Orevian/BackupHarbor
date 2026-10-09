//go:build windows

package fsx

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// IsReparsePoint reports whether fi (from Lstat) is a symlink, junction or any
// other reparse point.
func IsReparsePoint(fi os.FileInfo) bool {
	if fi.Mode()&os.ModeSymlink != 0 {
		return true
	}
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return d.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return false
}

// ResetAccess clears special attributes (read-only, system, hidden) and
// replaces the DACL of p with an open one so it can be deleted. It must only be
// used on objects BackupHarbor itself created, never on user data.
func ResetAccess(p string) error {
	pp, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return err
	}
	_ = windows.SetFileAttributes(pp, windows.FILE_ATTRIBUTE_NORMAL)
	return windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, nil, nil)
}

// FreeSpace returns the bytes available to the current user on dir's volume.
func FreeSpace(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return 0, err
	}
	return avail, nil
}
