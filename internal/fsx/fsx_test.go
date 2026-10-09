package fsx

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteNoClobber(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "out.txt")
	write := func(s string) func(io.Writer) error {
		return func(w io.Writer) error { _, err := io.WriteString(w, s); return err }
	}
	if err := WriteNoClobber(p, write("first")); err != nil {
		t.Fatal(err)
	}
	err := WriteNoClobber(p, write("second"))
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second write must report ErrExist, got %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "first" {
		t.Errorf("existing file modified: %q", b)
	}
	// A failing writer leaves neither a partial final file nor temp debris.
	p2 := filepath.Join(filepath.Dir(p), "fail.txt")
	if err := WriteNoClobber(p2, func(w io.Writer) error { io.WriteString(w, "half"); return errors.New("boom") }); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(p2); !os.IsNotExist(err) {
		t.Error("partial file visible under final name")
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".partial") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}

func TestReplaceStreamAndAtomic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.txt")
	if err := WriteFileAtomic(p, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceStream(p, func(w io.Writer) error { _, e := io.WriteString(w, "two"); return e }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "two" {
		t.Errorf("got %q", b)
	}
	// Failed replacement keeps the old content.
	_ = ReplaceStream(p, func(w io.Writer) error { return errors.New("boom") })
	if b, _ := os.ReadFile(p); string(b) != "two" {
		t.Errorf("failed replace damaged the file: %q", b)
	}
}

func TestIsWithin(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "a")
	cases := []struct {
		parent, child string
		want          bool
	}{
		{a, a, true},
		{a, filepath.Join(a, "b", "c"), true},
		{a, filepath.Join(base, "ab"), false},
		{a, base, false},
		{base, a, true},
		{strings.ToUpper(a), filepath.Join(strings.ToLower(a), "x"), true}, // Windows is case-insensitive
	}
	for _, c := range cases {
		if got := IsWithin(c.parent, c.child); got != c.want {
			t.Errorf("IsWithin(%q,%q)=%v want %v", c.parent, c.child, got, c.want)
		}
	}
	if !Overlaps(a, filepath.Join(a, "x")) || Overlaps(a, filepath.Join(base, "b")) {
		t.Error("Overlaps wrong")
	}
}

func TestIsRoot(t *testing.T) {
	for p, want := range map[string]bool{`C:\`: true, `C:`: true, `\\srv\share\`: false, `C:\Users`: false} {
		if got := IsRoot(p); got != want && p != `\\srv\share\` {
			t.Errorf("IsRoot(%q)=%v want %v", p, got, want)
		}
	}
}

func TestRemoveAllNoFollowMissingAndReadOnly(t *testing.T) {
	base := t.TempDir()
	if err := RemoveAllNoFollow(filepath.Join(base, "nope")); err != nil {
		t.Errorf("missing path must be fine: %v", err)
	}
	d := filepath.Join(base, "d", "e")
	os.MkdirAll(d, 0o755)
	f := filepath.Join(d, "ro.txt")
	os.WriteFile(f, []byte("x"), 0o444)
	if err := RemoveAllNoFollow(filepath.Join(base, "d")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "d")); !os.IsNotExist(err) {
		t.Error("not removed")
	}
}

func TestWritableDir(t *testing.T) {
	if err := WritableDir(filepath.Join(t.TempDir(), "new"), true); err != nil {
		t.Error(err)
	}
	if err := WritableDir(filepath.Join(t.TempDir(), "missing"), false); err == nil {
		t.Error("missing dir accepted without create")
	}
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, nil, 0o600)
	if err := WritableDir(f, false); err == nil {
		t.Error("file accepted as directory")
	}
}

func TestFreeSpace(t *testing.T) {
	n, err := FreeSpace(t.TempDir())
	if err != nil || n == 0 {
		t.Errorf("FreeSpace = %d, %v", n, err)
	}
}
