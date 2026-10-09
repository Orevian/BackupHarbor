//go:build windows

package secret

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	advapi32      = windows.NewLazySystemDLL("advapi32.dll")
	procCredReadW = advapi32.NewProc("CredReadW")
	procCredWrite = advapi32.NewProc("CredWriteW")
	procCredDel   = advapi32.NewProc("CredDeleteW")
	procCredFree  = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errNotFoundCode         = 1168 // ERROR_NOT_FOUND
	maxBlob                 = 2560 // CRED_MAX_CREDENTIAL_BLOB_SIZE
)

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// CredentialManager stores secrets as generic credentials in Windows Credential
// Manager. Entries are protected by the user's DPAPI master key.
type CredentialManager struct{}

// NewCredentialManager returns the Windows Credential Manager store.
func NewCredentialManager() Store { return CredentialManager{} }

// Get implements Store.
func (CredentialManager) Get(name string) (string, error) {
	target, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	var p *credential
	r, _, e := procCredReadW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&p)))
	if r == 0 {
		if errno, ok := e.(syscall.Errno); ok && errno == errNotFoundCode {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("CredReadW: %w", e)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(p)))
	if p.CredentialBlobSize == 0 || p.CredentialBlob == nil {
		return "", nil
	}
	blob := unsafe.Slice(p.CredentialBlob, p.CredentialBlobSize)
	out := make([]byte, len(blob))
	copy(out, blob)
	return string(out), nil
}

// Set implements Store.
func (CredentialManager) Set(name, value string) error {
	if value == "" {
		return errors.New("empty secret")
	}
	b := []byte(value)
	if len(b) > maxBlob {
		return fmt.Errorf("secret is larger than %d bytes", maxBlob)
	}
	target, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	user, _ := windows.UTF16PtrFromString("BackupHarbor")
	c := credential{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(b)),
		CredentialBlob:     &b[0],
		Persist:            credPersistLocalMachine,
		UserName:           user,
	}
	r, _, e := procCredWrite.Call(uintptr(unsafe.Pointer(&c)), 0)
	for i := range b {
		b[i] = 0
	}
	if r == 0 {
		return fmt.Errorf("CredWriteW: %w", e)
	}
	return nil
}

// Delete implements Store.
func (CredentialManager) Delete(name string) error {
	target, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	r, _, e := procCredDel.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if r == 0 {
		if errno, ok := e.(syscall.Errno); ok && errno == errNotFoundCode {
			return ErrNotFound
		}
		return fmt.Errorf("CredDeleteW: %w", e)
	}
	return nil
}
