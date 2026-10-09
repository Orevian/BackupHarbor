// Package fsx contains the small filesystem primitives BackupHarbor relies on
// for safety: atomic and no-clobber writes, reparse-point detection, path
// containment checks and a delete that never follows links.
package fsx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// RandomHex returns n random bytes as 2n hex characters.
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("fsx: no entropy: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// tempSibling creates a uniquely named temp file next to path.
func tempSibling(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name := filepath.Join(dir, "."+filepath.Base(path)+"."+RandomHex(4)+".partial")
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
}

func writeTemp(path string, write func(io.Writer) error) (string, error) {
	f, err := tempSibling(path)
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	werr := write(f)
	if werr == nil {
		werr = f.Sync()
	}
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp)
		if werr != nil {
			return "", werr
		}
		return "", cerr
	}
	return tmp, nil
}

// WriteFileAtomic writes data to path via a temp file and rename, replacing any
// existing file. A reader never observes a partially written file.
func WriteFileAtomic(path string, data []byte) error {
	tmp, err := writeTemp(path, func(w io.Writer) error { _, e := w.Write(data); return e })
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ReplaceStream writes path via a temp file and rename, replacing any existing
// file, streaming the content from write.
func ReplaceStream(path string, write func(io.Writer) error) error {
	tmp, err := writeTemp(path, write)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// WriteNoClobber writes a new file at path using write. It fails with an error
// satisfying errors.Is(err, fs.ErrExist) if path already exists, and never
// leaves a partial file under the final name.
func WriteNoClobber(path string, write func(io.Writer) error) error {
	if _, err := os.Lstat(path); err == nil {
		return &fs.PathError{Op: "write", Path: path, Err: fs.ErrExist}
	}
	tmp, err := writeTemp(path, write)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	// A hard link is an atomic create-if-absent; fall back to rename where
	// the filesystem has no hard links.
	if err := os.Link(tmp, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return &fs.PathError{Op: "write", Path: path, Err: fs.ErrExist}
		}
		if _, serr := os.Lstat(path); serr == nil {
			return &fs.PathError{Op: "write", Path: path, Err: fs.ErrExist}
		}
		return os.Rename(tmp, path)
	}
	return nil
}

// Clean returns an absolute, cleaned form of p. Relative paths are resolved
// against base when base is not empty, otherwise against the working directory.
func Clean(p, base string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if !filepath.IsAbs(p) && base != "" {
		p = filepath.Join(base, p)
	}
	return filepath.Abs(p)
}

func foldPath(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// resolveExisting evaluates symlinks for the longest existing prefix of p.
func resolveExisting(p string) string {
	p = filepath.Clean(p)
	var rest []string
	cur := p
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			for i := len(rest) - 1; i >= 0; i-- {
				r = filepath.Join(r, rest[i])
			}
			return r
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = append(rest, filepath.Base(cur))
		cur = parent
	}
}

// IsWithin reports whether child equals parent or lies beneath it, after
// resolving existing symlinks and junctions.
func IsWithin(parent, child string) bool {
	pp := foldPath(resolveExisting(parent))
	cc := foldPath(resolveExisting(child))
	if pp == cc {
		return true
	}
	rel, err := filepath.Rel(pp, cc)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Overlaps reports whether either path contains the other.
func Overlaps(a, b string) bool { return IsWithin(a, b) || IsWithin(b, a) }

// IsRoot reports whether p is a filesystem or UNC share root.
func IsRoot(p string) bool {
	p = filepath.Clean(p)
	vol := filepath.VolumeName(p)
	rest := strings.TrimPrefix(p, vol)
	return rest == `\` || rest == "/" || rest == "" || p == filepath.Dir(p)
}

// RemoveAllNoFollow deletes root and everything below it without ever
// following symlinks or junctions: links are unlinked, never traversed.
func RemoveAllNoFollow(root string) error {
	fi, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if IsReparsePoint(fi) || !fi.IsDir() {
		return removeOne(root)
	}
	err = removeChildren(root)
	if err != nil {
		// Restored directories can carry restrictive ACLs or attributes copied
		// from the original (for example C:\Users). We own this tree, so reset
		// access on this directory and try once more.
		if ResetAccess(root) == nil {
			err = removeChildren(root)
		}
	}
	if err != nil {
		return err
	}
	return removeOne(root)
}

func removeChildren(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var first error
	for _, e := range entries {
		if err := RemoveAllNoFollow(filepath.Join(dir, e.Name())); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func removeOne(p string) error {
	err := os.Remove(p)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	// Restored files may be read-only or carry restrictive ACLs.
	_ = os.Chmod(p, 0o666)
	_ = ResetAccess(p)
	err = os.Remove(p)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// WritableDir verifies that dir exists (creating it when create is true) and
// that a file can be created inside it.
func WritableDir(dir string, create bool) error {
	if create {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	f, err := os.OpenFile(filepath.Join(dir, ".rp-write-test-"+RandomHex(4)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}
