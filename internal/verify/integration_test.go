package verify_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"backupharbor/internal/config"
	"backupharbor/internal/secret"
	"backupharbor/internal/verify"
)

// resticBin returns a real restic executable or skips the test, saying why.
func resticBin(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("BH_TEST_RESTIC"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("restic"); err == nil {
		return p
	}
	t.Skip("real restic not available (set BH_TEST_RESTIC or put restic on PATH): integration test NOT run")
	return ""
}

const testPassword = "correct horse battery staple"

func runRestic(t *testing.T, bin, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "RESTIC_REPOSITORY="+repo, "RESTIC_PASSWORD="+testPassword)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restic %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func minimalPDF() []byte {
	body := "%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]>>endobj\n"
	xref := len(body)
	body += "xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000052 00000 n \n0000000101 00000 n \n"
	body += fmt.Sprintf("trailer<</Size 4/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", xref)
	return []byte(body)
}

type fixture struct {
	bin, repo, src string
	hash           string // sha256 of notes.txt
}

// newFixture builds a real restic repository with one snapshot of varied files.
func newFixture(t *testing.T) fixture {
	t.Helper()
	bin := resticBin(t)
	root := t.TempDir()
	f := fixture{bin: bin, repo: filepath.Join(root, "repo"), src: filepath.Join(root, "src")}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(f.src, "docs"), 0o755))
	notes := []byte("hello BackupHarbor\nsecond line\n")
	must(os.WriteFile(filepath.Join(f.src, "docs", "notes.txt"), notes, 0o644))
	sum := sha256.Sum256(notes)
	f.hash = hex.EncodeToString(sum[:])
	must(os.WriteFile(filepath.Join(f.src, "docs", "data.json"), []byte(`{"a":[1,2,3],"b":"x"}`), 0o644))
	must(os.WriteFile(filepath.Join(f.src, "docs", "doc.xml"), []byte(`<?xml version="1.0"?><root><item id="1"/></root>`), 0o644))
	must(os.WriteFile(filepath.Join(f.src, "docs", "doc.pdf"), minimalPDF(), 0o644))

	zf, err := os.Create(filepath.Join(f.src, "docs", "archive.zip"))
	must(err)
	zw := zip.NewWriter(zf)
	w, err := zw.Create("inner.txt")
	must(err)
	_, err = w.Write([]byte(strings.Repeat("zip content ", 100)))
	must(err)
	must(zw.Close())
	must(zf.Close())

	db, err := sql.Open("sqlite", filepath.Join(f.src, "docs", "app.db"))
	must(err)
	_, err = db.Exec(`CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT); INSERT INTO t(v) VALUES ('a'),('b');`)
	must(err)
	must(db.Close())

	runRestic(t, bin, f.repo, "init")
	runRestic(t, bin, f.repo, "backup", f.src, "--tag", "daily")
	return f
}

func (f fixture) config(t *testing.T, mod func(*config.Target)) (*config.Config, config.Target) {
	t.Helper()
	t.Setenv("BH_TEST_PASSWORD", testPassword)
	tmp := filepath.Join(t.TempDir(), "work")
	cfg := config.Default()
	cfg.Path = filepath.Join(t.TempDir(), "config.yaml")
	cfg.Restic.Path = f.bin
	cfg.TempDir = tmp
	tgt := config.Target{
		Name: "it", Repository: f.repo,
		Password: config.PasswordConfig{Source: "env", Env: "BH_TEST_PASSWORD"},
		Rules: []config.Rule{
			{Name: "json", Path: "**/data.json", Type: "json"},
			{Name: "xml", Path: "**/doc.xml", Type: "xml"},
			{Name: "pdf", Path: "**/doc.pdf", Type: "pdf"},
			{Name: "zip", Path: "**/archive.zip", Type: "zip"},
			{Name: "db", Path: "**/app.db", Type: "sqlite"},
			{Name: "txt", Path: "**/notes.txt", Type: "text"},
			{Name: "hash", Path: "**/notes.txt", Type: "sha256", SHA256: f.hash},
			{Name: "nonempty", Path: "**/*.json", Type: "non_empty"},
		},
	}
	if mod != nil {
		mod(&tgt)
	}
	cfg.Targets = []config.Target{tgt}
	cfg.Normalize()
	if issues := cfg.ValidateStatic(cfg.BaseDir()); config.HasErrors(issues) {
		t.Fatalf("test config invalid: %v", issues)
	}
	return cfg, cfg.Targets[0]
}

func newEngine(t *testing.T, cfg *config.Config) *verify.Engine {
	t.Helper()
	e, err := verify.New(cfg, verify.Options{Secrets: secret.NewMemoryStore()})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func stageStatus(t *testing.T, tr verify.TargetResult, id string) verify.Status {
	t.Helper()
	s := tr.Stage(id)
	if s == nil {
		t.Fatalf("stage %q missing; stages=%v", id, tr.Stages)
	}
	return s.Status
}

func dumpOnFail(t *testing.T, tr verify.TargetResult) {
	if !t.Failed() {
		return
	}
	for _, s := range tr.Stages {
		t.Logf("stage %-11s %-9s %s | %s", s.ID, s.Status, s.Message.Text, strings.ReplaceAll(s.Detail, "\n", " / "))
		for _, r := range s.Rules {
			t.Logf("   rule %-8s %-8s %s %v", r.Name, r.Status, r.Message.Text, r.Failures)
		}
	}
}

func leftovers(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	entries, err := os.ReadDir(cfg.Resolved().TempDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestRealRestic_FullVerificationPasses(t *testing.T) {
	f := newFixture(t)
	cfg, tgt := f.config(t, nil)
	tr := newEngine(t, cfg).VerifyTarget(context.Background(), tgt, "it-run")
	defer dumpOnFail(t, tr)

	if tr.Status != verify.Passed {
		t.Fatalf("target status = %s, want passed", tr.Status)
	}
	for _, id := range []string{verify.StageRestic, verify.StageDirs, verify.StagePassword, verify.StageRepository,
		verify.StageSnapshot, verify.StageCheck, verify.StageRestore, verify.StageContent, verify.StageCleanup} {
		if s := stageStatus(t, tr, id); s != verify.Passed {
			t.Errorf("stage %s = %s, want passed", id, s)
		}
	}
	if tr.Snapshot == nil || tr.Snapshot.ID == "" || len(tr.Snapshot.Paths) == 0 {
		t.Errorf("snapshot metadata missing: %+v", tr.Snapshot)
	}
	if tr.Restore == nil || tr.Restore.Files < 6 || tr.Restore.Bytes <= 0 || tr.Restore.DurationMS < 0 {
		t.Errorf("restore info implausible: %+v", tr.Restore)
	}
	if got := leftovers(t, cfg); len(got) != 0 {
		t.Errorf("workspace not cleaned up: %v", got)
	}
	if len(tr.Coverage) == 0 {
		t.Error("coverage statement missing")
	}
	// The password must appear nowhere in the stored record.
	if b := fmt.Sprintf("%+v", tr); strings.Contains(b, testPassword) {
		t.Error("password leaked into result")
	}
}

func TestRealRestic_WrongPasswordIsNotRunNeverPassed(t *testing.T) {
	f := newFixture(t)
	cfg, tgt := f.config(t, nil)
	t.Setenv("BH_TEST_PASSWORD", "definitely wrong")
	tr := newEngine(t, cfg).VerifyTarget(context.Background(), tgt, "it-run")
	defer dumpOnFail(t, tr)
	if tr.Status != verify.NotRun {
		t.Fatalf("status = %s, want not_run", tr.Status)
	}
	if s := stageStatus(t, tr, verify.StageRepository); s != verify.NotRun {
		t.Errorf("repository stage = %s", s)
	}
	if s := stageStatus(t, tr, verify.StageRestore); s != verify.NotRun {
		t.Errorf("restore stage = %s, want not_run", s)
	}
	if strings.Contains(fmt.Sprintf("%+v", tr), "definitely wrong") {
		t.Error("password leaked")
	}
}

func TestRealRestic_MissingRepositoryIsNotRun(t *testing.T) {
	f := newFixture(t)
	cfg, tgt := f.config(t, func(tg *config.Target) { tg.Repository = filepath.Join(filepath.Dir(f.repo), "nope") })
	tr := newEngine(t, cfg).VerifyTarget(context.Background(), tgt, "it-run")
	defer dumpOnFail(t, tr)
	if tr.Status != verify.NotRun {
		t.Fatalf("status = %s, want not_run", tr.Status)
	}
}

func TestRealRestic_CorruptedPackFailsDeepCheck(t *testing.T) {
	f := newFixture(t)
	// Flip bytes inside a data pack: a structural check can miss it, read-data must not.
	var pack string
	_ = filepath.WalkDir(filepath.Join(f.repo, "data"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && pack == "" {
			pack = p
		}
		return nil
	})
	if pack == "" {
		t.Fatal("no pack file found")
	}
	b, err := os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	for i := 10; i < 30 && i < len(b); i++ {
		b[i] ^= 0xFF
	}
	if err := os.WriteFile(pack, b, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, tgt := f.config(t, func(tg *config.Target) {
		tg.Check.ReadData = "all"
		tg.Restore.Enabled = ptrBool(false)
		tg.Rules = nil
	})
	tr := newEngine(t, cfg).VerifyTarget(context.Background(), tgt, "it-run")
	defer dumpOnFail(t, tr)
	if tr.Status != verify.Failed {
		t.Fatalf("status = %s, want failed", tr.Status)
	}
	if s := stageStatus(t, tr, verify.StageCheck); s != verify.Failed {
		t.Errorf("check = %s, want failed", s)
	}
}

func ptrBool(b bool) *bool { return &b }

func TestRealRestic_ContentRuleFailureIsReported(t *testing.T) {
	f := newFixture(t)
	cfg, tgt := f.config(t, func(tg *config.Target) {
		tg.Rules = []config.Rule{
			{Name: "badhash", Path: "**/notes.txt", Type: "sha256", SHA256: strings.Repeat("0", 64)},
			{Name: "missing", Path: "**/does-not-exist.bin", Type: "exists"},
			{Name: "optional", Path: "**/nothing.bin", Type: "exists", Required: ptrBool(false)},
			{Name: "json", Path: "**/data.json", Type: "json"},
		}
	})
	tr := newEngine(t, cfg).VerifyTarget(context.Background(), tgt, "it-run")
	defer dumpOnFail(t, tr)
	if tr.Status != verify.Failed {
		t.Fatalf("status = %s, want failed", tr.Status)
	}
	cs := tr.Stage(verify.StageContent)
	if cs == nil || len(cs.Rules) != 4 {
		t.Fatalf("content rules: %+v", cs)
	}
	want := map[string]verify.Status{"badhash": verify.Failed, "missing": verify.Failed, "optional": verify.Skipped, "json": verify.Passed}
	for _, r := range cs.Rules {
		if r.Status != want[r.Name] {
			t.Errorf("rule %s = %s, want %s", r.Name, r.Status, want[r.Name])
		}
	}
	if s := stageStatus(t, tr, verify.StageCleanup); s != verify.Passed {
		t.Errorf("cleanup = %s", s)
	}
}

func TestRealRestic_CancellationIsNotAFailureAndCleansUp(t *testing.T) {
	f := newFixture(t)
	cfg, tgt := f.config(t, nil)
	e := newEngine(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan verify.TargetResult, 1)
	go func() { done <- e.VerifyTarget(ctx, tgt, "it-run") }()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case tr := <-done:
		defer dumpOnFail(t, tr)
		if tr.Status != verify.Cancelled && tr.Status != verify.Passed {
			t.Fatalf("status = %s, want cancelled (or passed if it finished first)", tr.Status)
		}
		if tr.Status == verify.Failed {
			t.Fatal("cancellation classified as failure")
		}
		if got := leftovers(t, cfg); len(got) != 0 {
			t.Errorf("workspace not cleaned after cancel: %v", got)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("engine did not stop after cancellation")
	}
}

func TestRealRestic_TimeoutIsTimedOut(t *testing.T) {
	f := newFixture(t)
	cfg, tgt := f.config(t, nil)
	cfg.Targets[0].Timeout = 0
	// Bypass validation limits: engine honours any positive timeout.
	cfg.Timeout = config.Duration(5 * time.Millisecond)
	tr := newEngine(t, cfg).VerifyTarget(context.Background(), tgt, "it-run")
	defer dumpOnFail(t, tr)
	if tr.Status != verify.TimedOut {
		t.Fatalf("status = %s, want timed_out", tr.Status)
	}
	if got := leftovers(t, cfg); len(got) != 0 {
		t.Errorf("leftovers: %v", got)
	}
}

func TestRealRestic_RunAggregatesMultipleTargets(t *testing.T) {
	f := newFixture(t)
	cfg, good := f.config(t, nil)
	bad := good
	bad.Name = "broken"
	bad.Repository = filepath.Join(filepath.Dir(f.repo), "missing-repo")
	cfg.Targets = []config.Target{bad, good}
	cfg.Normalize()
	run, err := newEngine(t, cfg).Run(context.Background(), verify.RunOptions{Trigger: verify.TriggerCLI})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Targets) != 2 {
		t.Fatalf("targets = %d", len(run.Targets))
	}
	if run.Targets[0].Name != "broken" || run.Targets[0].Status != verify.NotRun {
		t.Errorf("broken target: %+v", run.Targets[0].Status)
	}
	if run.Targets[1].Name != "it" || run.Targets[1].Status != verify.Passed {
		t.Errorf("good target hidden by failing one: %s", run.Targets[1].Status)
	}
	if run.ExitCode() != verify.ExitError {
		t.Errorf("exit code = %d, want %d (one target could not be tested)", run.ExitCode(), verify.ExitError)
	}
	if run.ResticVersion == "" || run.RunID == "" || run.Schema == "" {
		t.Errorf("run identity incomplete: %+v", run)
	}
}
