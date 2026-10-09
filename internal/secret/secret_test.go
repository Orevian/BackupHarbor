package secret

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactorMasksKnownSecretsAndPatterns(t *testing.T) {
	r := NewRedactor("hunter2", "hunter2-long")
	in := "open rest:https://bob:pa55w0rd@example.org/repo failed; RESTIC_PASSWORD=hunter2-long AWS_SECRET_ACCESS_KEY=abcd and hunter2 again"
	out := r.Redact(in)
	for _, leak := range []string{"hunter2", "pa55w0rd", "abcd", "hunter2-long"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked in %q", leak, out)
		}
	}
	if !strings.Contains(out, "bob:"+Mask+"@") {
		t.Errorf("userinfo not masked properly: %q", out)
	}
	if got := RedactRepository("sftp://user:pw@host/path"); strings.Contains(got, "pw@") {
		t.Errorf("repository credentials leaked: %q", got)
	}
	if got := RedactRepository(`D:\Backups\repo`); got != `D:\Backups\repo` {
		t.Errorf("plain path altered: %q", got)
	}
	var nilR *Redactor
	if nilR.Redact("x") != "x" {
		t.Error("nil redactor must pass through")
	}
}

func TestResolveSources(t *testing.T) {
	dir := t.TempDir()
	pw := filepath.Join(dir, "pw.txt")
	os.WriteFile(pw, []byte("\xef\xbb\xbffilepass\r\n"), 0o600)
	empty := filepath.Join(dir, "empty.txt")
	os.WriteFile(empty, []byte("\n"), 0o600)
	big := filepath.Join(dir, "big.txt")
	os.WriteFile(big, []byte(strings.Repeat("a", 5000)), 0o600)
	t.Setenv("RP_T_PW", "envpass")
	t.Setenv("RP_T_EMPTY", "")
	st := NewMemoryStore()
	st.Set(CredentialPrefix+"t1", "credpass")

	got, err := Resolve(Source{Kind: SourceFile, File: pw}, st)
	if err != nil || got != "filepass" {
		t.Errorf("file: %q %v", got, err)
	}
	if got, err := Resolve(Source{Kind: SourceEnv, Env: "RP_T_PW"}, st); err != nil || got != "envpass" {
		t.Errorf("env: %q %v", got, err)
	}
	if got, err := Resolve(Source{Kind: SourceCredential, Credential: "t1"}, st); err != nil || got != "credpass" {
		t.Errorf("credential: %q %v", got, err)
	}
	for name, src := range map[string]Source{
		"env unset":     {Kind: SourceEnv, Env: "RP_T_UNSET"},
		"env empty":     {Kind: SourceEnv, Env: "RP_T_EMPTY"},
		"file empty":    {Kind: SourceFile, File: empty},
		"file missing":  {Kind: SourceFile, File: filepath.Join(dir, "nope")},
		"file dir":      {Kind: SourceFile, File: dir},
		"file too big":  {Kind: SourceFile, File: big},
		"unknown":       {Kind: "bogus"},
		"cred missing":  {Kind: SourceCredential, Credential: "none"},
		"cred no store": {Kind: SourceCredential, Credential: "t1"},
	} {
		s := st
		var store Store = s
		if name == "cred no store" {
			store = nil
		}
		if _, err := Resolve(src, store); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := Resolve(Source{Kind: SourceCredential, Credential: "none"}, st); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing credential should be ErrNotFound, got %v", err)
	}
}
