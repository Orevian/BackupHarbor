//go:build !windows

package fsx

import (
	"os"
	"syscall"
)

// IsReparsePoint reports whether fi (from Lstat) is a symlink.
func IsReparsePoint(fi os.FileInfo) bool { return fi.Mode()&os.ModeSymlink != 0 }

// ResetAccess makes p writable by its owner so it can be deleted.
func ResetAccess(p string) error { return os.Chmod(p, 0o700) }

// FreeSpace returns the bytes available to the current user on dir's volume.
func FreeSpace(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
