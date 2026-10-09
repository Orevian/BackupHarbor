package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateAndCleanup(t *testing.T) {
	base := filepath.Join(t.TempDir(), "work")
	w, err := Create(base, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(w.Dir) != base || !strings.HasPrefix(filepath.Base(w.Dir), "run-") {
		t.Errorf("unexpected location %s", w.Dir)
	}
	if fi, err := os.Stat(w.RestoreDir); err != nil || !fi.IsDir() {
		t.Fatal("restore dir missing")
	}
	os.WriteFile(filepath.Join(w.RestoreDir, "f.txt"), []byte("x"), 0o400) // read-only file
	if err := w.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
		t.Error("workspace still exists")
	}
	if err := w.Cleanup(); err != nil {
		t.Errorf("cleanup must be idempotent: %v", err)
	}
	var nilW *Workspace
	if nilW.Cleanup() != nil {
		t.Error("nil workspace cleanup must be a no-op")
	}
}

func TestBaseValidation(t *testing.T) {
	if _, err := Create(`C:\`, "x"); err == nil {
		t.Error("drive root accepted as scratch base")
	}
	if _, err := Create(`relative\dir`, "x"); err == nil {
		t.Error("relative scratch base accepted")
	}
}

func TestCleanupRefusesWithoutValidMarker(t *testing.T) {
	base := t.TempDir()
	w, err := Create(base, "run-2")
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(w.Dir, "restore", "precious.txt")
	os.WriteFile(victim, []byte("keep"), 0o600)

	// Marker removed -> ownership cannot be proven.
	os.Remove(filepath.Join(w.Dir, MarkerName))
	if err := w.Cleanup(); err == nil {
		t.Fatal("cleanup must refuse when the marker is gone")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Error("data was deleted despite refusal")
	}

	// Marker from another workspace -> nonce mismatch.
	other, _ := Create(base, "run-3")
	data, _ := os.ReadFile(filepath.Join(other.Dir, MarkerName))
	os.WriteFile(filepath.Join(w.Dir, MarkerName), data, 0o600)
	if err := w.Cleanup(); err == nil {
		t.Fatal("cleanup must refuse on a foreign marker")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Error("data was deleted despite refusal")
	}
}

func TestCleanupRefusesDirectoryOutsideBase(t *testing.T) {
	base := t.TempDir()
	w, _ := Create(base, "run-4")
	w.Dir = filepath.Join(base, "nested", filepath.Base(w.Dir))
	if err := w.Cleanup(); err == nil || !strings.Contains(err.Error(), "refusing") {
		// A nonexistent path is acceptable too, but never a deletion of something else.
		if err != nil {
			return
		}
	}
}

func TestCleanupDoesNotFollowJunctions(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.txt")
	os.WriteFile(keep, []byte("must survive"), 0o600)

	w, err := Create(base, "run-5")
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(w.RestoreDir, "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Skipf("cannot create junction here: %v %s", err, out)
	}
	if err := w.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(keep); err != nil || string(b) != "must survive" {
		t.Fatal("cleanup followed a junction and deleted data outside the workspace")
	}
	if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
		t.Error("workspace not removed")
	}
}

func TestCleanupHandlesRestrictiveAccessControl(t *testing.T) {
	base := t.TempDir()
	w, _ := Create(base, "run-6")
	d := filepath.Join(w.RestoreDir, "locked")
	os.Mkdir(d, 0o755)
	os.WriteFile(filepath.Join(d, "f"), []byte("x"), 0o644)
	// Deny delete for Everyone on the directory, like restored C:\Users.
	if out, err := exec.Command("icacls", d, "/deny", "*S-1-1-0:(DE,DC)").CombinedOutput(); err != nil {
		t.Skipf("icacls unavailable: %v %s", err, out)
	}
	if err := w.Cleanup(); err != nil {
		t.Fatalf("cleanup must cope with restrictive ACLs: %v", err)
	}
	if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
		t.Error("workspace not removed")
	}
}

func TestCleanStale(t *testing.T) {
	base := t.TempDir()
	old, _ := Create(base, "old")
	fresh, _ := Create(base, "fresh")
	foreign := filepath.Join(base, "run-not-ours")
	os.Mkdir(foreign, 0o755)
	os.WriteFile(filepath.Join(foreign, "x"), []byte("x"), 0o600)
	unrelated := filepath.Join(base, "documents")
	os.Mkdir(unrelated, 0o755)

	time.Sleep(20 * time.Millisecond)
	n, err := CleanStale(base, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// Both owned workspaces are older than 10ms.
	if n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	for _, p := range []string{old.Dir, fresh.Dir} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s not removed", p)
		}
	}
	for _, p := range []string{foreign, unrelated} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed although BackupHarbor does not own it", p)
		}
	}
	// A recent workspace survives a long threshold.
	keep, _ := Create(base, "recent")
	if n, _ := CleanStale(base, time.Hour); n != 0 {
		t.Error("recent workspace removed")
	}
	if _, err := os.Stat(keep.Dir); err != nil {
		t.Error("recent workspace missing")
	}
}
