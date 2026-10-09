package verify

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backupharbor/internal/config"
)

func writeBytes(t *testing.T, name string, b []byte) (string, int64) {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p, int64(len(b))
}

func run(t *testing.T, typ string, b []byte) fileCheck {
	t.Helper()
	p, n := writeBytes(t, "f", b)
	return checkFile(context.Background(), config.Rule{Type: typ}, p, n)
}

func pdfBytes(valid bool) []byte {
	body := "%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\n"
	x := len(body)
	body += "xref\n0 2\n0000000000 65535 f \n0000000009 00000 n \ntrailer<</Size 2/Root 1 0 R>>\nstartxref\n" + fmt.Sprint(x) + "\n%%EOF\n"
	if !valid {
		return []byte(body[:len(body)-20])
	}
	return []byte(body)
}

func TestFileCheckers(t *testing.T) {
	zbuf := new(bytes.Buffer)
	zw := zip.NewWriter(zbuf)
	w, _ := zw.Create("a.txt")
	w.Write([]byte(strings.Repeat("hello ", 50)))
	zw.Close()
	goodZip := zbuf.Bytes()
	badZip := append([]byte{}, goodZip...)
	badZip[40] ^= 0xFF // corrupt compressed data of the first entry

	cases := []struct {
		name string
		typ  string
		data []byte
		want Status
	}{
		{"exists", "exists", []byte{}, Passed},
		{"non_empty ok", "non_empty", []byte("x"), Passed},
		{"non_empty empty", "non_empty", []byte{}, Failed},
		{"text ok", "text", []byte("héllo wörld\nçok iyi\n"), Passed},
		{"text utf16", "text", []byte{0xFF, 0xFE, 'h', 0, 'i', 0}, Passed},
		{"text binary", "text", []byte{'a', 0, 'b'}, Failed},
		{"text bad utf8", "text", []byte{0xC3, 0x28}, Failed},
		{"text truncated rune", "text", []byte{'a', 0xE2, 0x82}, Failed},
		{"json ok", "json", []byte(` {"a":[1,2,{"b":null}]} `), Passed},
		{"json scalar", "json", []byte(`42`), Passed},
		{"json truncated", "json", []byte(`{"a":[1,2`), Failed},
		{"json trailing", "json", []byte(`{"a":1} {"b":2}`), Failed},
		{"json empty", "json", []byte(``), Failed},
		{"json garbage", "json", []byte(`not json`), Failed},
		{"xml ok", "xml", []byte(`<?xml version="1.0"?><r><c a="1"/>text</r>`), Passed},
		{"xml unclosed", "xml", []byte(`<r><c></r>`), Failed},
		{"xml truncated", "xml", []byte(`<r><c>`), Failed},
		{"xml empty", "xml", []byte(``), Failed},
		{"zip ok", "zip", goodZip, Passed},
		{"zip corrupt data", "zip", badZip, Failed},
		{"zip truncated", "zip", goodZip[:len(goodZip)/2], Failed},
		{"zip not an archive", "zip", []byte("PK nope"), Failed},
		{"pdf ok", "pdf", pdfBytes(true), Passed},
		{"pdf truncated", "pdf", pdfBytes(false), Failed},
		{"pdf not pdf", "pdf", []byte(strings.Repeat("hello world ", 10)), Failed},
		{"sqlite not a db", "sqlite", []byte("definitely not sqlite at all"), Failed},
	}
	for _, c := range cases {
		got := run(t, c.typ, c.data)
		if got.status != c.want {
			t.Errorf("%s: %s want %s (%s)", c.name, got.status, c.want, got.msg.Text)
		}
	}
}

func TestSQLiteIntegrity(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.db")
	db, err := sql.Open("sqlite", good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=DELETE; PRAGMA page_size=1024; CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT, w TEXT); CREATE INDEX i ON t(v);`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400; i++ {
		db.Exec(`INSERT INTO t(v,w) VALUES (?,?)`, fmt.Sprintf("value-%d", i), strings.Repeat("w", 200))
	}
	db.Close()
	st, _ := os.Stat(good)
	if r := checkFile(context.Background(), config.Rule{Type: "sqlite"}, good, st.Size()); r.status != Passed {
		t.Fatalf("healthy database: %s %s", r.status, r.msg.Text)
	}

	// Corrupt pages in the middle of the file while keeping the header.
	data, _ := os.ReadFile(good)
	for i := 4096; i < 4096+2048 && i < len(data); i++ {
		data[i] = 0xAB
	}
	bad := filepath.Join(dir, "bad.db")
	os.WriteFile(bad, data, 0o644)
	r := checkFile(context.Background(), config.Rule{Type: "sqlite"}, bad, int64(len(data)))
	if r.status != Failed {
		t.Errorf("corrupted database reported %s (%s)", r.status, r.msg.Text)
	}
	// Truncated database.
	trunc := filepath.Join(dir, "trunc.db")
	os.WriteFile(trunc, data[:len(data)/2], 0o644)
	if r := checkFile(context.Background(), config.Rule{Type: "sqlite"}, trunc, int64(len(data)/2)); r.status == Passed {
		t.Error("truncated database must not pass")
	}
	// The checked file itself must not be modified.
	after, _ := os.ReadFile(good)
	orig, _ := os.ReadFile(good)
	if !bytes.Equal(after, orig) {
		t.Error("database modified")
	}
}

func TestSHA256Rule(t *testing.T) {
	content := []byte("proof")
	sum := sha256.Sum256(content)
	p, n := writeBytes(t, "f", content)
	good := config.Rule{Type: "sha256", SHA256: strings.ToUpper(hex.EncodeToString(sum[:]))}
	if r := checkFile(context.Background(), good, p, n); r.status != Passed {
		t.Errorf("matching hash: %s", r.status)
	}
	bad := config.Rule{Type: "sha256", SHA256: strings.Repeat("0", 64)}
	if r := checkFile(context.Background(), bad, p, n); r.status != Failed {
		t.Errorf("wrong hash: %s", r.status)
	}
}

func TestCancelledContextIsNeverAFileFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, n := writeBytes(t, "f", []byte(`{"a":1}`))
	for _, typ := range []string{"json", "text", "xml", "zip", "pdf", "sqlite", "sha256", "exists"} {
		r := checkFile(ctx, config.Rule{Type: typ, SHA256: strings.Repeat("0", 64)}, p, n)
		if r.status != Cancelled {
			t.Errorf("%s with cancelled context: %s", typ, r.status)
		}
	}
}

func TestZIPBombIsBounded(t *testing.T) {
	// A highly compressible entry larger than the (lowered) limit is reported as partial, not read fully.
	zbuf := new(bytes.Buffer)
	zw := zip.NewWriter(zbuf)
	w, _ := zw.Create("big")
	chunk := bytes.Repeat([]byte{0}, 1<<20)
	for i := 0; i < 6; i++ {
		w.Write(chunk)
	}
	w2, _ := zw.Create("second")
	w2.Write([]byte("x"))
	zw.Close()
	p, n := writeBytes(t, "bomb.zip", zbuf.Bytes())
	restore := setZIPLimit(2 << 20)
	defer restore()
	r := checkFile(context.Background(), config.Rule{Type: "zip"}, p, n)
	if r.status != Partial {
		t.Errorf("status = %s (%s)", r.status, r.msg.Text)
	}
}

func setZIPLimit(n int64) func() {
	old := maxZIPUncompressed
	maxZIPUncompressed = n
	return func() { maxZIPUncompressed = old }
}
