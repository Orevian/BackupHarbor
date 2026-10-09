package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backupharbor/internal/restic"
	"backupharbor/internal/schedule"
	"backupharbor/internal/secret"
	"backupharbor/internal/verify"
)

type fakeBackend struct {
	mu       sync.Mutex
	catErr   map[string]error // by repository
	checkErr map[string]error
	block    bool // restore blocks until the context ends
	calls    int
}

func (f *fakeBackend) Version(context.Context) (restic.Version, error) {
	return restic.Version{Major: 0, Minor: 19, Patch: 1}, nil
}
func (f *fakeBackend) CatConfig(_ context.Context, r restic.Repo) error { return f.catErr[r.Location] }
func (f *fakeBackend) Snapshots(context.Context, restic.Repo, restic.SnapshotFilter) ([]restic.Snapshot, error) {
	return []restic.Snapshot{{ID: "aaaa1111aaaa1111", ShortID: "aaaa1111", Time: time.Now().Add(-time.Hour), Hostname: "h", Paths: []string{`C:\d`}}}, nil
}
func (f *fakeBackend) Check(_ context.Context, r restic.Repo, _ restic.CheckOptions) (restic.CheckResult, error) {
	return restic.CheckResult{}, f.checkErr[r.Location]
}
func (f *fakeBackend) Stats(context.Context, restic.Repo, string) (restic.Stats, error) {
	return restic.Stats{TotalSize: 10}, nil
}
func (f *fakeBackend) Restore(ctx context.Context, _ restic.Repo, o restic.RestoreOptions, _ func(restic.RestoreProgress)) (restic.RestoreSummary, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		return restic.RestoreSummary{}, ctx.Err()
	}
	p := filepath.Join(o.Target, "C", "d")
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "a.json"), []byte(`{"a":1}`), 0o644)
	return restic.RestoreSummary{}, nil
}

type env struct {
	t       *testing.T
	dir     string
	cfgPath string
	out     *bytes.Buffer
	err     *bytes.Buffer
	io      IO
	be      *fakeBackend
	store   *secret.MemoryStore
	vars    map[string]string
	tty     bool
	sched   *fakeSched
}

type fakeSched struct {
	installed  bool
	foreign    bool
	failCreate bool
}

func (f *fakeSched) Run(_ context.Context, name string, args []string, _ []byte) ([]byte, []byte, int, error) {
	if name == "powershell.exe" {
		switch {
		case f.foreign:
			return []byte(`{"installed":true,"state":"Ready","description":"not ours"}`), nil, 0, nil
		case f.installed:
			return []byte(`{"installed":true,"state":"Ready","description":"BackupHarbor managed task.","next":"2026-10-10T00:30:00Z","lastResult":0,"trigger":"MSFT_TaskDailyTrigger","start":"2026-10-09T03:00:00"}`), nil, 0, nil
		}
		return []byte(`{"installed":false}`), nil, 0, nil
	}
	switch args[0] {
	case "/Create":
		if f.failCreate {
			return nil, []byte("ERROR: Access is denied."), 1, nil
		}
		f.installed = true
	case "/Delete":
		f.installed = false
	}
	return nil, nil, 0, nil
}

// newEnv builds an isolated configuration with two targets.
func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{t: t, dir: dir, cfgPath: filepath.Join(dir, "config.yaml"), out: new(bytes.Buffer), err: new(bytes.Buffer),
		be: &fakeBackend{catErr: map[string]error{}, checkErr: map[string]error{}}, store: secret.NewMemoryStore(),
		vars: map[string]string{}, sched: &fakeSched{}}
	e.write(`
version: 1
temp_dir: '` + filepath.Join(dir, "work") + `'
reports: {dir: '` + filepath.Join(dir, "reports") + `'}
targets:
  - name: alpha
    repository: '` + filepath.Join(dir, "repo-a") + `'
    password: {source: env, env: RP_CLI_PW}
    rules: [{name: j, path: '**/a.json', type: json}]
  - name: beta
    repository: '` + filepath.Join(dir, "repo-b") + `'
    password: {source: env, env: RP_CLI_PW}
`)
	os.MkdirAll(filepath.Join(dir, "repo-a"), 0o755)
	os.MkdirAll(filepath.Join(dir, "repo-b"), 0o755)
	t.Setenv("RP_CLI_PW", "cli-secret-pw")
	return e
}

func (e *env) write(y string) { os.WriteFile(e.cfgPath, []byte(y), 0o644) }

func (e *env) run(ctx context.Context, args ...string) int {
	e.out.Reset()
	e.err.Reset()
	exe := filepath.Join(e.dir, "bharbor.exe")
	io := IO{Out: e.out, Err: e.err, In: strings.NewReader(""),
		Getenv:       func(k string) string { return e.vars[k] },
		IsTerminal:   func(io.Writer) bool { return e.tty },
		ReadPassword: func(string) (string, error) { return "typed-secret", nil },
		Secrets:      e.store, Backend: e.be,
		Scheduler: func() *schedule.Manager {
			return &schedule.Manager{TaskName: schedule.DefaultTaskName, Runner: e.sched}
		},
		ExePath: func() (string, error) { return exe, nil }}
	e.io = io
	return Run(ctx, append([]string{"--config", e.cfgPath, "--lang", "en"}, args...), io)
}

func (e *env) runWithInput(in string, args ...string) int {
	code := e.run(context.Background(), args...)
	_ = in
	return code
}

func TestHelpAndVersion(t *testing.T) {
	e := newEnv(t)
	if code := e.run(context.Background(), "--help"); code != 0 || !strings.Contains(e.out.String(), "Exit codes") || !strings.Contains(e.out.String(), "bharbor") {
		t.Errorf("help: %d %q", code, e.out.String())
	}
	for _, want := range []string{"init", "validate", "run", "report", "schedule", "--config", "--json", "--no-color", "NO_COLOR"} {
		if !strings.Contains(e.out.String(), want) {
			t.Errorf("help lacks %q", want)
		}
	}
	if code := e.run(context.Background(), "--version"); code != 0 || !strings.HasPrefix(e.out.String(), "bharbor ") {
		t.Errorf("version: %d %q", code, e.out.String())
	}
	if code := e.run(context.Background(), "--version", "--json"); code != 0 {
		t.Fatal(code)
	}
	var m map[string]string
	if json.Unmarshal(e.out.Bytes(), &m) != nil || m["version"] == "" || m["name"] != "bharbor" {
		t.Errorf("version json: %q", e.out.String())
	}
	if code := e.run(context.Background()); code != ExitError {
		t.Errorf("no command should be a usage error, got %d", code)
	}
	if code := e.run(context.Background(), "frobnicate"); code != ExitError || !strings.Contains(e.err.String(), "Unknown command") {
		t.Errorf("unknown command: %d %q", code, e.err.String())
	}
	if code := e.run(context.Background(), "run", "--bogus"); code != ExitError {
		t.Errorf("bad flag: %d", code)
	}
}

func TestInitDoesNotOverwrite(t *testing.T) {
	e := newEnv(t)
	e.cfgPath = filepath.Join(e.dir, "new", "config.yaml")
	if code := e.run(context.Background(), "init"); code != 0 {
		t.Fatalf("init: %d %s", code, e.err.String())
	}
	b1, _ := os.ReadFile(e.cfgPath)
	os.WriteFile(e.cfgPath, append(b1, []byte("# my edit\n")...), 0o644)
	if code := e.run(context.Background(), "init"); code != ExitError || !strings.Contains(e.err.String(), "already exists") {
		t.Fatalf("second init must refuse: %d %q", code, e.err.String())
	}
	if b, _ := os.ReadFile(e.cfgPath); !strings.Contains(string(b), "# my edit") {
		t.Fatal("existing configuration was modified")
	}
	if code := e.run(context.Background(), "init", "--force"); code != 0 {
		t.Fatalf("force: %d", code)
	}
	matches, _ := filepath.Glob(e.cfgPath + ".bak-*")
	if len(matches) != 1 {
		t.Fatalf("backup copy missing: %v", matches)
	}
	if b, _ := os.ReadFile(matches[0]); !strings.Contains(string(b), "# my edit") {
		t.Error("backup does not contain the previous content")
	}
}

func TestExitCodes(t *testing.T) {
	ctx := context.Background()
	t.Run("all targets pass: 0", func(t *testing.T) {
		e := newEnv(t)
		if code := e.run(ctx, "run"); code != 0 {
			t.Fatalf("exit %d\n%s%s", code, e.out, e.err)
		}
	})
	t.Run("a failing target: 1, and the other result is still shown", func(t *testing.T) {
		e := newEnv(t)
		e.be.checkErr[filepath.Join(e.dir, "repo-b")] = &restic.Error{Command: "check", ExitCode: 1, Kind: restic.KindGeneric, Stderr: "Fatal: repository contains errors"}
		if code := e.run(ctx, "run"); code != ExitFailed {
			t.Fatalf("exit %d", code)
		}
		out := e.out.String()
		if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") || !strings.Contains(out, "PASSED") || !strings.Contains(out, "FAILED") {
			t.Errorf("both targets must be reported:\n%s", out)
		}
	})
	t.Run("a target that could not be tested: 2", func(t *testing.T) {
		e := newEnv(t)
		e.be.catErr[filepath.Join(e.dir, "repo-a")] = &restic.Error{Command: "cat config", ExitCode: 12, Kind: restic.KindWrongPassword}
		if code := e.run(ctx, "run", "--target", "alpha"); code != ExitError {
			t.Fatalf("exit %d", code)
		}
		if !strings.Contains(e.out.String(), "NOT RUN") {
			t.Errorf("output:\n%s", e.out)
		}
	})
	t.Run("failure outranks not-run: 1", func(t *testing.T) {
		e := newEnv(t)
		e.be.catErr[filepath.Join(e.dir, "repo-a")] = &restic.Error{Command: "cat config", ExitCode: 10, Kind: restic.KindNoRepo}
		e.be.checkErr[filepath.Join(e.dir, "repo-b")] = &restic.Error{Command: "check", ExitCode: 1, Kind: restic.KindGeneric}
		if code := e.run(ctx, "run"); code != ExitFailed {
			t.Fatalf("exit %d", code)
		}
	})
	t.Run("cancellation: 3, never reported as a broken backup", func(t *testing.T) {
		e := newEnv(t)
		e.be.block = true
		c, cancel := context.WithCancel(ctx)
		go func() { time.Sleep(400 * time.Millisecond); cancel() }()
		if code := e.run(c, "run", "--target", "alpha"); code != ExitCancelled {
			t.Fatalf("exit %d\n%s%s", code, e.out, e.err)
		}
		if strings.Contains(e.out.String(), "FAILED") {
			t.Errorf("cancel shown as failure:\n%s", e.out)
		}
		if es, _ := os.ReadDir(filepath.Join(e.dir, "work")); len(es) != 0 {
			t.Errorf("work area not cleaned: %v", es)
		}
	})
	t.Run("timeout: 3", func(t *testing.T) {
		e := newEnv(t)
		e.be.block = true
		b, _ := os.ReadFile(e.cfgPath)
		os.WriteFile(e.cfgPath, []byte(strings.Replace(string(b), "targets:", "timeout: 1m\ntargets:", 1)), 0o644)
		// shorten via target-level override below the validated minimum is not allowed, so use ctx deadline
		c, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		if code := e.run(c, "run", "--target", "alpha"); code != ExitCancelled {
			t.Fatalf("exit %d", code)
		}
	})
	t.Run("configuration errors: 2", func(t *testing.T) {
		e := newEnv(t)
		e.write("version: 1\nconcurrency: 99\n")
		if code := e.run(ctx, "run"); code != ExitError {
			t.Errorf("invalid config: %d", code)
		}
		e.cfgPath = filepath.Join(e.dir, "missing.yaml")
		if code := e.run(ctx, "run"); code != ExitError || !strings.Contains(e.err.String(), "not found") {
			t.Errorf("missing config: %d %q", code, e.err)
		}
	})
	t.Run("unknown target and no enabled target: 2", func(t *testing.T) {
		e := newEnv(t)
		if code := e.run(ctx, "run", "--target", "nope"); code != ExitError || !strings.Contains(e.err.String(), "Unknown target") {
			t.Errorf("unknown target: %d %q", code, e.err)
		}
		e.write("version: 1\ntargets:\n - {name: x, enabled: false, repository: r, password: {source: env, env: A}}\n")
		if code := e.run(ctx, "run"); code != ExitError {
			t.Errorf("no enabled target: %d", code)
		}
	})
	t.Run("unwritable report directory: 2 before any restore", func(t *testing.T) {
		e := newEnv(t)
		blocker := filepath.Join(e.dir, "blocker")
		os.WriteFile(blocker, []byte("x"), 0o600)
		b, _ := os.ReadFile(e.cfgPath)
		os.WriteFile(e.cfgPath, []byte(strings.Replace(string(b), filepath.Join(e.dir, "reports"), filepath.Join(blocker, "sub"), 1)), 0o644)
		if code := e.run(ctx, "run"); code != ExitError {
			t.Fatalf("exit %d", code)
		}
		if e.be.calls != 0 {
			t.Error("restore started although the result could not be saved")
		}
	})
}

func TestRunJSONOutputIsMachineReadable(t *testing.T) {
	e := newEnv(t)
	code := e.run(context.Background(), "run", "--json", "--target", "alpha")
	if code != 0 {
		t.Fatalf("exit %d %s", code, e.err)
	}
	var run verify.Run
	if err := json.Unmarshal(e.out.Bytes(), &run); err != nil {
		t.Fatalf("stdout is not a single JSON document: %v\n%s", err, e.out)
	}
	if run.Schema != "backupharbor.run/1" || run.Status != verify.Passed || len(run.Targets) != 1 {
		t.Errorf("run: %+v", run)
	}
	if strings.Contains(e.out.String(), "\x1b[") {
		t.Error("ANSI escapes in JSON output")
	}
	if strings.Contains(e.out.String()+e.err.String(), "cli-secret-pw") {
		t.Error("password leaked")
	}
}

func TestColourHandling(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	e.run(context.Background(), "run", "--target", "alpha")
	if !strings.Contains(e.out.String(), "\x1b[") {
		t.Error("expected colour on a terminal")
	}
	e.vars["NO_COLOR"] = "1"
	e.run(context.Background(), "run", "--target", "alpha")
	if strings.Contains(e.out.String(), "\x1b[") {
		t.Error("NO_COLOR ignored")
	}
	delete(e.vars, "NO_COLOR")
	e.run(context.Background(), "run", "--target", "alpha", "--no-color")
	if strings.Contains(e.out.String(), "\x1b[") {
		t.Error("--no-color ignored")
	}
	e.tty = false
	e.run(context.Background(), "run", "--target", "alpha")
	if strings.Contains(e.out.String(), "\x1b[") {
		t.Error("colour written to a non-terminal")
	}
}

func TestTurkishOutput(t *testing.T) {
	e := newEnv(t)
	e.run(context.Background(), "run", "--target", "alpha", "--lang", "tr")
	out := e.out.String()
	if !strings.Contains(out, "BAŞARILI") || !strings.Contains(out, "Geri yükleme") {
		t.Errorf("Turkish output expected:\n%s", out)
	}
	e.run(context.Background(), "report", "--lang", "tr", "--format", "txt")
	if !strings.Contains(e.out.String(), "doğrulama raporu") {
		t.Error("Turkish report expected")
	}
}

func TestReportCommand(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if code := e.run(ctx, "report"); code != ExitError || !strings.Contains(e.err.String(), "No reports found") {
		t.Fatalf("no reports: %d %q", code, e.err)
	}
	if code := e.run(ctx, "report", "--format", "json"); code != ExitError {
		t.Errorf("json with no reports: %d", code)
	}
	e.run(ctx, "run")
	for format, marker := range map[string]string{"json": `"schema": "backupharbor.run/1"`, "html": "<!DOCTYPE html>", "txt": "BackupHarbor verification report"} {
		if code := e.run(ctx, "report", "--last", "--format", format); code != 0 || !strings.Contains(e.out.String(), marker) {
			t.Errorf("%s: exit %d, lacks %q", format, code, marker)
		}
	}
	if code := e.run(ctx, "report", "--json"); code != 0 || !json.Valid(e.out.Bytes()) {
		t.Error("--json report is not valid JSON")
	}
	// Export to a file, never overwrite silently.
	out := filepath.Join(e.dir, "export.html")
	if code := e.run(ctx, "report", "--format", "html", "--output", out); code != 0 {
		t.Fatalf("export: %d %s", code, e.err)
	}
	first, _ := os.ReadFile(out)
	if code := e.run(ctx, "report", "--format", "html", "--output", out); code != ExitError || !strings.Contains(e.err.String(), "already exists") {
		t.Errorf("silent overwrite: %d %q", code, e.err)
	}
	if now, _ := os.ReadFile(out); !bytes.Equal(first, now) {
		t.Error("export file changed")
	}
	if code := e.run(ctx, "report", "--format", "html", "--output", out, "--force"); code != 0 {
		t.Error("--force failed")
	}
	// Selection.
	if code := e.run(ctx, "report", "--list"); code != 0 || !strings.Contains(e.out.String(), "alpha") {
		t.Errorf("list: %d %s", code, e.out)
	}
	if code := e.run(ctx, "report", "--list", "--json"); code != 0 {
		t.Fatal(code)
	}
	var list []map[string]any
	if err := json.Unmarshal(e.out.Bytes(), &list); err != nil || len(list) != 1 {
		t.Errorf("list json: %v %s", err, e.out)
	}
	if code := e.run(ctx, "report", "--run", "20000101T000000Z-000000"); code != ExitError {
		t.Errorf("unknown run: %d", code)
	}
	if code := e.run(ctx, "report", "--target", "ghost"); code != ExitError {
		t.Errorf("unknown target in report: %d", code)
	}
	if code := e.run(ctx, "report", "--format", "pdf"); code != ExitError {
		t.Errorf("bad format: %d", code)
	}
	if code := e.run(ctx, "report", "--target", "alpha", "--format", "json"); code != 0 || strings.Contains(e.out.String(), `"name": "beta"`) {
		t.Errorf("target filter: %d", code)
	}
}

func TestValidateCommand(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if code := e.run(ctx, "validate"); code != 0 {
		t.Fatalf("valid setup: %d\n%s%s", code, e.out, e.err)
	}
	if e.be.calls != 0 {
		t.Error("validate must never restore")
	}
	e.run(ctx, "validate", "--json")
	var res struct {
		OK      bool `json:"ok"`
		Targets []struct{ Name string }
	}
	if err := json.Unmarshal(e.out.Bytes(), &res); err != nil || !res.OK || len(res.Targets) != 2 {
		t.Errorf("json: %v %s", err, e.out)
	}
	t.Setenv("RP_CLI_PW", "")
	if code := e.run(ctx, "validate"); code != ExitError || !strings.Contains(e.out.String(), "NOT RUN") {
		t.Errorf("missing password: %d\n%s", code, e.out)
	}
	t.Setenv("RP_CLI_PW", "x")
	e.write("version: 1\ntheme: neon\ntargets: []\n")
	if code := e.run(ctx, "validate"); code != ExitError || !strings.Contains(e.out.String(), "Unknown theme") {
		t.Errorf("invalid config: %d\n%s", code, e.out)
	}
}

func TestScheduleCommands(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if code := e.run(ctx, "schedule", "status"); code != 0 || !strings.Contains(e.out.String(), "not installed") {
		t.Errorf("status before install: %d %s", code, e.out)
	}
	e.sched.failCreate = true
	if code := e.run(ctx, "schedule", "install"); code != ExitError || !strings.Contains(e.err.String(), "Access is denied") {
		t.Errorf("failed install must be an error: %d %q", code, e.err)
	}
	e.sched.failCreate = false
	if code := e.run(ctx, "schedule", "install", "--frequency", "weekly", "--day", "friday", "--time", "02:45"); code != 0 {
		t.Fatalf("install: %d %s", code, e.err)
	}
	if code := e.run(ctx, "schedule", "status", "--json"); code != 0 {
		t.Fatal(code)
	}
	var st schedule.Status
	if json.Unmarshal(e.out.Bytes(), &st) != nil || !st.Installed {
		t.Errorf("status json: %s", e.out)
	}
	if code := e.run(ctx, "schedule", "install", "--time", "99:99"); code != ExitError {
		t.Errorf("invalid time accepted: %d", code)
	}
	if code := e.run(ctx, "schedule", "remove"); code != 0 {
		t.Errorf("remove: %d", code)
	}
	if code := e.run(ctx, "schedule", "remove"); code != 0 || !strings.Contains(e.out.String(), "not installed") {
		t.Errorf("remove when absent: %d", code)
	}
	e.sched.foreign = true
	if code := e.run(ctx, "schedule", "install"); code != ExitError || !strings.Contains(e.err.String(), "did not create") {
		t.Errorf("foreign task: %d %q", code, e.err)
	}
	if code := e.run(ctx, "schedule", "remove"); code != ExitError {
		t.Errorf("foreign remove: %d", code)
	}
	if code := e.run(ctx, "schedule"); code != ExitError {
		t.Errorf("missing subcommand: %d", code)
	}
}

func TestSecretCommands(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	// Interactive entry (injected reader) keyed by the target's credential name.
	if code := e.run(ctx, "secret", "set", "alpha"); code != 0 {
		t.Fatalf("set: %d %s", code, e.err)
	}
	if v, err := e.store.Get(secret.CredentialPrefix + "alpha"); err != nil || v != "typed-secret" {
		t.Errorf("stored %q %v", v, err)
	}
	if strings.Contains(e.out.String()+e.err.String(), "typed-secret") {
		t.Error("password echoed")
	}
	if code := e.run(ctx, "secret", "status", "alpha"); code != 0 {
		t.Errorf("status: %d", code)
	}
	if code := e.run(ctx, "secret", "delete", "alpha"); code != 0 {
		t.Errorf("delete: %d", code)
	}
	if code := e.run(ctx, "secret", "status", "alpha"); code != ExitError {
		t.Errorf("status after delete: %d", code)
	}
	if code := e.run(ctx, "secret", "delete", "alpha"); code != ExitError {
		t.Errorf("delete missing: %d", code)
	}
	if code := e.run(ctx, "secret", "set"); code != ExitError {
		t.Errorf("usage: %d", code)
	}
}

func TestSecretFromStdin(t *testing.T) {
	e := newEnv(t)
	dio := DefaultIO()
	dio.Out, dio.Err = e.out, e.err
	dio.In = strings.NewReader("piped-secret\r\n")
	dio.Secrets = e.store
	dio.IsTerminal = func(io.Writer) bool { return false }
	if code := Run(context.Background(), []string{"--config", e.cfgPath, "--lang", "en", "secret", "set", "beta", "--stdin"}, dio); code != 0 {
		t.Fatalf("exit %d %s", code, e.err)
	}
	if v, _ := e.store.Get(secret.CredentialPrefix + "beta"); v != "piped-secret" {
		t.Errorf("stored %q", v)
	}
}

func TestValidateExplainsScopeAndCost(t *testing.T) {
	e := newEnv(t)
	if code := e.run(context.Background(), "validate"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, e.out, e.err)
	}
	out := e.out.String()
	for _, want := range []string{"Scope:", "integrity check: repository structure only", "restore: the whole snapshot", "content rule(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("validate output lacks %q:\n%s", want, out)
		}
	}
	e.run(context.Background(), "validate", "--lang", "tr")
	if !strings.Contains(e.out.String(), "Kapsam:") {
		t.Error("Turkish scope expected")
	}
}
