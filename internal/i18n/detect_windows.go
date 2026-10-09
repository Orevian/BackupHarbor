//go:build windows

package i18n

import (
	"syscall"
	"unsafe"
)

// Detect returns the language of the Windows user locale (en or tr; English otherwise).
func Detect() string {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	proc := k32.NewProc("GetUserDefaultLocaleName")
	buf := make([]uint16, 85)
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return EN
	}
	name := syscall.UTF16ToString(buf)
	if len(name) >= 2 && (name[:2] == "tr" || name[:2] == "TR") {
		return TR
	}
	return EN
}
