//go:build windows

package secret

import (
	"errors"
	"testing"

	"backupharbor/internal/fsx"
)

// Exercises the real Windows Credential Manager with a unique throw-away entry.
func TestCredentialManagerRoundTrip(t *testing.T) {
	cm := NewCredentialManager()
	name := CredentialPrefix + "unit-test-" + fsx.RandomHex(6)
	t.Cleanup(func() { _ = cm.Delete(name) })

	if _, err := cm.Get(name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound before Set, got %v", err)
	}
	if err := cm.Set(name, "pässwörd € 123"); err != nil {
		t.Skipf("Credential Manager not writable here: %v", err)
	}
	got, err := cm.Get(name)
	if err != nil || got != "pässwörd € 123" {
		t.Fatalf("round trip = %q, %v", got, err)
	}
	if err := cm.Set(name, "second"); err != nil {
		t.Fatal(err)
	}
	if got, _ := cm.Get(name); got != "second" {
		t.Errorf("overwrite failed: %q", got)
	}
	if err := cm.Delete(name); err != nil {
		t.Fatal(err)
	}
	if err := cm.Delete(name); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
	if err := cm.Set(name, ""); err == nil {
		t.Error("empty secret accepted")
	}
}
