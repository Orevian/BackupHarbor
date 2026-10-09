// Package workspace creates and cleans the private scratch directories that
// restores are written to. A directory is only ever deleted when it carries an
// ownership marker written by BackupHarbor, so a misconfigured temp path can
// never cause unrelated user data to be removed.
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backupharbor/internal/fsx"
)

const (
	// MarkerName is the ownership marker file placed in every workspace.
	MarkerName  = ".backupharbor-workspace"
	markerMagic = "backupharbor-workspace/1"
	dirPrefix   = "run-"
	// RestoreSubdir is where snapshot content is restored inside a workspace.
	RestoreSubdir = "restore"
)

// ErrUnsafe is returned when a cleanup is refused because ownership or
// location of the directory cannot be proven.
var ErrUnsafe = errors.New("refusing to clean up: workspace ownership could not be verified")

type marker struct {
	Magic   string    `json:"magic"`
	RunID   string    `json:"run_id"`
	Nonce   string    `json:"nonce"`
	Created time.Time `json:"created"`
}

// Workspace is one run's private scratch directory.
type Workspace struct {
	// Dir is the workspace root (contains the marker).
	Dir string
	// RestoreDir is the directory snapshot content is restored into.
	RestoreDir string

	base  string
	nonce string
}

// CheckBase validates a configured scratch base directory.
func CheckBase(base string) error {
	if !filepath.IsAbs(base) {
		return fmt.Errorf("scratch directory %q must be an absolute path", base)
	}
	if fsx.IsRoot(base) {
		return fmt.Errorf("scratch directory %q must not be a drive or share root", base)
	}
	return nil
}

// Create makes a new workspace beneath base (created if missing).
func Create(base, runID string) (*Workspace, error) {
	if err := CheckBase(base); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, err
	}
	if fi, err := os.Lstat(base); err != nil {
		return nil, err
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("scratch path %q is not a directory", base)
	}
	safeID := sanitize(runID)
	dir := filepath.Join(base, dirPrefix+safeID+"-"+fsx.RandomHex(3))
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, err
	}
	w := &Workspace{Dir: dir, RestoreDir: filepath.Join(dir, RestoreSubdir), base: base, nonce: fsx.RandomHex(16)}
	mk, _ := json.Marshal(marker{Magic: markerMagic, RunID: runID, Nonce: w.nonce, Created: time.Now().UTC()})
	if err := os.WriteFile(filepath.Join(dir, MarkerName), mk, 0o600); err != nil {
		_ = os.Remove(dir)
		return nil, err
	}
	if err := os.Mkdir(w.RestoreDir, 0o700); err != nil {
		_ = fsx.RemoveAllNoFollow(dir)
		return nil, err
	}
	return w, nil
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 40 {
		out = out[:40]
	}
	if out == "" {
		out = "x"
	}
	return out
}

func readMarker(dir string) (*marker, error) {
	b, err := os.ReadFile(filepath.Join(dir, MarkerName))
	if err != nil {
		return nil, err
	}
	var m marker
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Magic != markerMagic {
		return nil, errors.New("not a BackupHarbor workspace")
	}
	return &m, nil
}

// verify proves that dir is a real (non-link) directory directly beneath base
// that carries a valid marker; expectNonce is checked when non-empty.
func verify(base, dir, expectNonce string) error {
	if filepath.Clean(filepath.Dir(dir)) != filepath.Clean(base) {
		return fmt.Errorf("%w: %s is not directly inside %s", ErrUnsafe, dir, base)
	}
	if !strings.HasPrefix(filepath.Base(dir), dirPrefix) {
		return fmt.Errorf("%w: unexpected directory name", ErrUnsafe)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() || fsx.IsReparsePoint(fi) {
		return fmt.Errorf("%w: %s is a link or not a directory", ErrUnsafe, dir)
	}
	m, err := readMarker(dir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafe, err)
	}
	if expectNonce != "" && m.Nonce != expectNonce {
		return fmt.Errorf("%w: ownership marker mismatch", ErrUnsafe)
	}
	return nil
}

// Cleanup removes the workspace after verifying ownership. It never follows
// links. A nil receiver is a no-op.
func (w *Workspace) Cleanup() error {
	if w == nil {
		return nil
	}
	if _, err := os.Lstat(w.Dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := verify(w.base, w.Dir, w.nonce); err != nil {
		return err
	}
	return fsx.RemoveAllNoFollow(w.Dir)
}

// CleanStale removes workspaces under base that BackupHarbor created more
// than olderThan ago (left behind by a crash or power loss). Only directories
// with a valid marker are touched. It returns the number removed.
func CleanStale(base string, olderThan time.Duration) (int, error) {
	if err := CheckBase(base); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	var first error
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), dirPrefix) {
			continue
		}
		dir := filepath.Join(base, e.Name())
		if verify(base, dir, "") != nil {
			continue
		}
		m, err := readMarker(dir)
		if err != nil || time.Since(m.Created) < olderThan {
			continue
		}
		if err := fsx.RemoveAllNoFollow(dir); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		removed++
	}
	return removed, first
}
