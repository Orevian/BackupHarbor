package verify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backupharbor/internal/config"
	"backupharbor/internal/restic"
	"backupharbor/internal/secret"
)

// fakeBackend is a scriptable restic stand-in.
type fakeBackend struct {
	mu        sync.Mutex
	version   restic.Version
	versionEr error
	catErr    error
	snaps     []restic.Snapshot
	snapErr   error
	checkErr  error
	stats     restic.Stats
	statsErr  error
	restore   func(ctx context.Context, o restic.RestoreOptions) (restic.RestoreSummary, error)
	filters   []restic.SnapshotFilter
	restored  []restic.RestoreOptions
	checked   []restic.CheckOptions
}

func newFake() *fakeBackend {
	return &fakeBackend{
		version: restic.Version{Major: 0, Minor: 19, Patch: 1},
		snaps: []restic.Snapshot{
			{ID: "aaaa1111aaaa1111", ShortID: "aaaa1111", Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Hostname: "h", Paths: []string{`C:\old`}},
			{ID: "bbbb2222bbbb2222", ShortID: "bbbb2222", Time: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Hostname: "h", Paths: []string{`C:\new`}, Tags: []string{"daily"}},
		},
		stats: restic.Stats{TotalSize: 1000, TotalFileCount: 3},
		restore: func(_ context.Context, o restic.RestoreOptions) (restic.RestoreSummary, error) {
			writeFiles(o.Target, map[string]string{"C/data/a.json": `{"a":1}`, "C/data/b.txt": "hello", "C/data/c.txt": "world"})
			return restic.RestoreSummary{HaveSummary: true, TotalFiles: 3, FilesRestored: 3}, nil
		},
	}
}

func writeFiles(root string, files map[string]string) {
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(content), 0o644)
	}
}

func (f *fakeBackend) Version(context.Context) (restic.Version, error) { return f.version, f.versionEr }
func (f *fakeBackend) CatConfig(context.Context, restic.Repo) error    { return f.catErr }
func (f *fakeBackend) Snapshots(_ context.Context, _ restic.Repo, fl restic.SnapshotFilter) ([]restic.Snapshot, error) {
	f.mu.Lock()
	f.filters = append(f.filters, fl)
	f.mu.Unlock()
	return f.snaps, f.snapErr
}
func (f *fakeBackend) Check(_ context.Context, _ restic.Repo, o restic.CheckOptions) (restic.CheckResult, error) {
	f.mu.Lock()
	f.checked = append(f.checked, o)
	f.mu.Unlock()
	return restic.CheckResult{Output: "no errors were found"}, f.checkErr
}
func (f *fakeBackend) Stats(context.Context, restic.Repo, string) (restic.Stats, error) {
	return f.stats, f.statsErr
}
func (f *fakeBackend) Restore(ctx context.Context, _ restic.Repo, o restic.RestoreOptions, _ func(restic.RestoreProgress)) (restic.RestoreSummary, error) {
	f.mu.Lock()
	f.restored = append(f.restored, o)
	f.mu.Unlock()
	return f.restore(ctx, o)
}

func ptr[T any](v T) *T { return &v }

func testCfg(t *testing.T, mod func(*config.Target)) (*config.Config, config.Target) {
	t.Helper()
	t.Setenv("RP_UNIT_PW", "pw-unit-secret")
	cfg := config.Default()
	cfg.Path = filepath.Join(t.TempDir(), "config.yaml")
	cfg.TempDir = filepath.Join(t.TempDir(), "work")
	tg := config.Target{Name: "t1", Repository: filepath.Join(t.TempDir(), "repo"),
		Password: config.PasswordConfig{Source: "env", Env: "RP_UNIT_PW"}}
	if mod != nil {
		mod(&tg)
	}
	cfg.Targets = []config.Target{tg}
	cfg.Normalize()
	return cfg, cfg.Targets[0]
}

func engineFor(t *testing.T, cfg *config.Config, fb *fakeBackend) *Engine {
	t.Helper()
	e, err := New(cfg, Options{Backend: fb, Secrets: secret.NewMemoryStore()})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func st(t *testing.T, tr TargetResult, id string) Status {
	t.Helper()
	s := tr.Stage(id)
	if s == nil {
		t.Fatalf("stage %s missing", id)
	}
	return s.Status
}

func work(cfg *config.Config) []string {
	es, _ := os.ReadDir(cfg.Resolved().TempDir)
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestAllStagesPass(t *testing.T) {
	cfg, tg := testCfg(t, func(tg *config.Target) {
		tg.Rules = []config.Rule{{Name: "j", Path: "**/a.json", Type: "json"}, {Name: "t", Path: "**/*.txt", Type: "text"}}
	})
	fb := newFake()
	tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r1")
	if tr.Status != Passed {
		t.Fatalf("status=%s stages=%+v", tr.Status, tr.Stages)
	}
	if tr.Snapshot.ShortID != "bbbb2222" {
		t.Errorf("latest snapshot not selected: %+v", tr.Snapshot)
	}
	if tr.Restore.Files != 3 || tr.Restore.Directories != 2 || tr.Restore.Bytes != int64(len(`{"a":1}`)+10) {
		t.Errorf("restore info: %+v", tr.Restore)
	}
	cs := tr.Stage(StageContent)
	if len(cs.Rules) != 2 || cs.Rules[1].Checked != 2 {
		t.Errorf("rules: %+v", cs.Rules)
	}
	if len(work(cfg)) != 0 {
		t.Errorf("workspace left behind: %v", work(cfg))
	}
	if len(fb.restored) != 1 || !fb.restored[0].Verify {
		t.Errorf("restore must use --verify by default: %+v", fb.restored)
	}
	if !strings.HasPrefix(fb.restored[0].Target, cfg.Resolved().TempDir) {
		t.Errorf("restore target outside the work directory: %s", fb.restored[0].Target)
	}
	if len(tr.Coverage) < 3 {
		t.Errorf("coverage: %+v", tr.Coverage)
	}
}

func TestSnapshotSelection(t *testing.T) {
	t.Run("tag filter is forwarded", func(t *testing.T) {
		cfg, tg := testCfg(t, func(tg *config.Target) {
			tg.Snapshot = config.SnapshotConfig{Select: "tag", Tags: []string{"daily"}, Host: "h"}
		})
		fb := newFake()
		engineFor(t, cfg, fb).Preflight(context.Background(), tg, "r")
		if len(fb.filters) != 1 || fb.filters[0].Host != "h" || len(fb.filters[0].Tags) != 1 {
			t.Errorf("filters: %+v", fb.filters)
		}
	})
	t.Run("explicit id", func(t *testing.T) {
		cfg, tg := testCfg(t, func(tg *config.Target) { tg.Snapshot = config.SnapshotConfig{Select: "id", ID: "aaaa1111"} })
		tr := engineFor(t, cfg, newFake()).Preflight(context.Background(), tg, "r")
		if tr.Snapshot == nil || tr.Snapshot.ShortID != "aaaa1111" {
			t.Errorf("snapshot: %+v", tr.Snapshot)
		}
	})
	t.Run("unknown id is not_run", func(t *testing.T) {
		cfg, tg := testCfg(t, func(tg *config.Target) { tg.Snapshot = config.SnapshotConfig{Select: "id", ID: "deadbeef"} })
		tr := engineFor(t, cfg, newFake()).VerifyTarget(context.Background(), tg, "r")
		if tr.Status != NotRun || st(t, tr, StageSnapshot) != NotRun {
			t.Errorf("status=%s", tr.Status)
		}
	})
	t.Run("ambiguous id", func(t *testing.T) {
		cfg, tg := testCfg(t, func(tg *config.Target) { tg.Snapshot = config.SnapshotConfig{Select: "id", ID: "aaaa1111"} })
		fb := newFake()
		fb.snaps = append(fb.snaps, restic.Snapshot{ID: "aaaa1111ffff0000", ShortID: "aaaa1111", Time: time.Now()})
		tr := engineFor(t, cfg, fb).Preflight(context.Background(), tg, "r")
		if st(t, tr, StageSnapshot) != NotRun {
			t.Error("ambiguous id must not be guessed")
		}
	})
	t.Run("empty repository is a failure", func(t *testing.T) {
		cfg, tg := testCfg(t, nil)
		fb := newFake()
		fb.snaps = nil
		tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r")
		if tr.Status != Failed {
			t.Errorf("a repository without snapshots has nothing restorable: %s", tr.Status)
		}
		if st(t, tr, StageRestore) != NotRun {
			t.Error("restore must not run without a snapshot")
		}
	})
}

func TestCheckOutcomes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Status
	}{
		{"integrity errors", &restic.Error{Command: "check", ExitCode: 1, Kind: restic.KindGeneric, Stderr: "Fatal: repository contains errors"}, Failed},
		{"locked", &restic.Error{Command: "check", ExitCode: 11, Kind: restic.KindLocked}, NotRun},
		{"unreachable", &restic.Error{Command: "check", ExitCode: 10, Kind: restic.KindNoRepo}, NotRun},
		{"wrong password", &restic.Error{Command: "check", ExitCode: 12, Kind: restic.KindWrongPassword}, NotRun},
	}
	for _, c := range cases {
		cfg, tg := testCfg(t, func(tg *config.Target) { tg.Restore.Enabled = ptr(false) })
		fb := newFake()
		fb.checkErr = c.err
		tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r")
		if got := st(t, tr, StageCheck); got != c.want {
			t.Errorf("%s: check=%s want %s", c.name, got, c.want)
		}
		if tr.Status == Passed {
			t.Errorf("%s: target must never pass", c.name)
		}
	}
}

func TestCheckOptionsAreForwarded(t *testing.T) {
	cfg, tg := testCfg(t, func(tg *config.Target) {
		tg.Check.ReadData = "subset"
		tg.Check.Subset = "5%"
		tg.Restore.Enabled = ptr(false)
	})
	fb := newFake()
	tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r")
	if len(fb.checked) != 1 || fb.checked[0].ReadData != "subset" || fb.checked[0].Subset != "5%" {
		t.Errorf("checked: %+v", fb.checked)
	}
	if tr.Status != Passed {
		t.Errorf("status=%s", tr.Status)
	}
	if st(t, tr, StageRestore) != Skipped || st(t, tr, StageDirs) != Skipped {
		t.Error("disabled restore must be recorded as skipped")
	}
}

func TestRestoreOutcomes(t *testing.T) {
	two := func(_ context.Context, o restic.RestoreOptions) (restic.RestoreSummary, error) {
		writeFiles(o.Target, map[string]string{"C/a.txt": "x", "C/b.txt": "y"})
		return restic.RestoreSummary{}, nil
	}
	genericErr := &restic.Error{Command: "restore", ExitCode: 1, Kind: restic.KindGeneric, Stderr: "Fatal: There were 1 errors"}
	cases := []struct {
		name string
		fn   func(context.Context, restic.RestoreOptions) (restic.RestoreSummary, error)
		mod  func(*config.Target)
		want Status
	}{
		{"metadata errors only keep contents", func(c context.Context, o restic.RestoreOptions) (restic.RestoreSummary, error) {
			s, _ := two(c, o)
			s.HaveSummary, s.MetadataErrorCount = true, 3
			return s, genericErr
		}, nil, Passed},
		{"content errors are partial", func(c context.Context, o restic.RestoreOptions) (restic.RestoreSummary, error) {
			s, _ := two(c, o)
			s.HaveSummary, s.ErrorCount = true, 1
			return s, genericErr
		}, nil, Partial},
		{"nothing restored is failed", func(context.Context, restic.RestoreOptions) (restic.RestoreSummary, error) {
			return restic.RestoreSummary{ErrorCount: 1}, genericErr
		}, nil, Failed},
		{"unknown failure with files is partial, never passed", func(c context.Context, o restic.RestoreOptions) (restic.RestoreSummary, error) {
			s, _ := two(c, o)
			return s, genericErr
		}, nil, Partial},
		{"too few files fails", two, func(tg *config.Target) { tg.Restore.MinFiles = 5 }, Failed},
		{"empty restore fails", func(context.Context, restic.RestoreOptions) (restic.RestoreSummary, error) {
			return restic.RestoreSummary{}, nil
		}, nil, Failed},
		{"wrong password is not_run", func(context.Context, restic.RestoreOptions) (restic.RestoreSummary, error) {
			return restic.RestoreSummary{}, &restic.Error{Command: "restore", ExitCode: 12, Kind: restic.KindWrongPassword}
		}, nil, NotRun},
	}
	for _, c := range cases {
		cfg, tg := testCfg(t, c.mod)
		fb := newFake()
		fb.restore = c.fn
		tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r")
		if got := st(t, tr, StageRestore); got != c.want {
			t.Errorf("%s: restore=%s want %s (%s)", c.name, got, c.want, tr.Stage(StageRestore).Message.Text)
		}
		if c.want != Passed && tr.Status == Passed {
			t.Errorf("%s: target passed although restore did not", c.name)
		}
		if st(t, tr, StageCleanup) == Failed || len(work(cfg)) != 0 {
			t.Errorf("%s: cleanup problem: %v", c.name, work(cfg))
		}
	}
}

func TestInsufficientSpaceIsNotRunAndKeepsOtherProof(t *testing.T) {
	cfg, tg := testCfg(t, nil)
	fb := newFake()
	fb.stats = restic.Stats{TotalSize: 1 << 62}
	tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r")
	if st(t, tr, StageRestore) != NotRun {
		t.Fatalf("restore=%s", st(t, tr, StageRestore))
	}
	if len(fb.restored) != 0 {
		t.Error("restore ran although it would not fit")
	}
	if tr.Status != Partial { // the integrity check passed, the restore could not be tested
		t.Errorf("status=%s want partial", tr.Status)
	}
}

func TestCancellationDuringRestoreCleansUp(t *testing.T) {
	cfg, tg := testCfg(t, nil)
	fb := newFake()
	started := make(chan struct{})
	fb.restore = func(ctx context.Context, o restic.RestoreOptions) (restic.RestoreSummary, error) {
		writeFiles(o.Target, map[string]string{"C/partial.bin": "data"})
		close(started)
		<-ctx.Done()
		return restic.RestoreSummary{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan TargetResult, 1)
	go func() { done <- engineFor(t, cfg, fb).VerifyTarget(ctx, tg, "r") }()
	<-started
	cancel()
	tr := <-done
	if tr.Status != Cancelled {
		t.Fatalf("status=%s", tr.Status)
	}
	if st(t, tr, StageRestore) != Cancelled {
		t.Errorf("restore=%s", st(t, tr, StageRestore))
	}
	if st(t, tr, StageCleanup) != Passed || len(work(cfg)) != 0 {
		t.Errorf("cancelled run must still clean up: %v %v", st(t, tr, StageCleanup), work(cfg))
	}
	if tr.Status == Failed {
		t.Error("cancellation must never look like a broken backup")
	}
}

func TestTimeoutDuringRestore(t *testing.T) {
	cfg, tg := testCfg(t, nil)
	cfg.Timeout = config.Duration(150 * time.Millisecond)
	fb := newFake()
	fb.restore = func(ctx context.Context, o restic.RestoreOptions) (restic.RestoreSummary, error) {
		<-ctx.Done()
		return restic.RestoreSummary{}, ctx.Err()
	}
	tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r")
	if tr.Status != TimedOut || st(t, tr, StageRestore) != TimedOut {
		t.Fatalf("status=%s restore=%s", tr.Status, st(t, tr, StageRestore))
	}
	if len(work(cfg)) != 0 {
		t.Errorf("leftovers: %v", work(cfg))
	}
}

func TestPasswordProblemsAreNotRun(t *testing.T) {
	cfg, tg := testCfg(t, func(tg *config.Target) { tg.Password = config.PasswordConfig{Source: "credential"} })
	tr := engineFor(t, cfg, newFake()).VerifyTarget(context.Background(), tg, "r")
	if tr.Status != NotRun || st(t, tr, StagePassword) != NotRun {
		t.Fatalf("status=%s", tr.Status)
	}
	if st(t, tr, StageRepository) != NotRun {
		t.Error("later stages must be marked not_run")
	}
}

func TestResticTooOldOrMissing(t *testing.T) {
	cfg, tg := testCfg(t, nil)
	fb := newFake()
	fb.version = restic.Version{Major: 0, Minor: 12}
	if tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r"); tr.Status != NotRun {
		t.Errorf("old restic: %s", tr.Status)
	}
	fb = newFake()
	fb.versionEr = errors.New("boom")
	if tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r"); tr.Status != NotRun {
		t.Errorf("missing restic: %s", tr.Status)
	}
}

func TestPasswordIsRedactedFromEverythingStored(t *testing.T) {
	cfg, tg := testCfg(t, nil)
	fb := newFake()
	fb.checkErr = &restic.Error{Command: "check", ExitCode: 1, Kind: restic.KindGeneric,
		Stderr: "Fatal: failed with password pw-unit-secret for rest:https://u:topsecret@h/r"}
	tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r")
	b, _ := json.Marshal(tr)
	for _, leak := range []string{"pw-unit-secret", "topsecret"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("%q leaked into the stored result: %s", leak, b)
		}
	}
}

func TestContentRulesAgainstRestoredFiles(t *testing.T) {
	cfg, tg := testCfg(t, func(tg *config.Target) {
		tg.Rules = []config.Rule{
			{Name: "good", Path: "**/a.json", Type: "json"},
			{Name: "badjson", Path: "**/broken.json", Type: "json"},
			{Name: "required-missing", Path: "**/ghost.db", Type: "sqlite"},
			{Name: "optional-missing", Path: "**/ghost2.db", Type: "sqlite", Required: ptr(false)},
			{Name: "sampled", Path: "**/*.txt", Type: "text", MaxFiles: 1},
		}
	})
	fb := newFake()
	fb.restore = func(_ context.Context, o restic.RestoreOptions) (restic.RestoreSummary, error) {
		writeFiles(o.Target, map[string]string{"C/a.json": `{"ok":true}`, "C/broken.json": `{"ok":`, "C/1.txt": "a", "C/2.txt": "b"})
		return restic.RestoreSummary{}, nil
	}
	tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r")
	want := map[string]Status{"good": Passed, "badjson": Failed, "required-missing": Failed, "optional-missing": Skipped, "sampled": Partial}
	for _, r := range tr.Stage(StageContent).Rules {
		if r.Status != want[r.Name] {
			t.Errorf("rule %s = %s want %s (%s)", r.Name, r.Status, want[r.Name], r.Message.Text)
		}
	}
	if tr.Status != Failed {
		t.Errorf("status=%s", tr.Status)
	}
	bad := tr.Stage(StageContent).Rules[1]
	if len(bad.Failures) != 1 || !strings.HasSuffix(bad.Failures[0].Path, "broken.json") {
		t.Errorf("failure list: %+v", bad.Failures)
	}
}

func TestJunctionsInRestoredTreeAreNotFollowed(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.json"), []byte(`{}`), 0o600)
	cfg, tg := testCfg(t, func(tg *config.Target) {
		tg.Rules = []config.Rule{{Name: "j", Path: "**/secret.json", Type: "json", Required: ptr(false)}}
	})
	fb := newFake()
	fb.restore = func(_ context.Context, o restic.RestoreOptions) (restic.RestoreSummary, error) {
		writeFiles(o.Target, map[string]string{"C/a.txt": "x"})
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(o.Target, "C", "link"), outside).CombinedOutput(); err != nil {
			t.Skipf("no junction support: %v %s", err, out)
		}
		return restic.RestoreSummary{}, nil
	}
	tr := engineFor(t, cfg, fb).VerifyTarget(context.Background(), tg, "r")
	if r := tr.Stage(StageContent).Rules[0]; r.Matched != 0 {
		t.Errorf("walk followed a junction out of the workspace: %+v", r)
	}
	if tr.Restore.Links != 1 {
		t.Errorf("links = %d", tr.Restore.Links)
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.json")); err != nil {
		t.Error("cleanup deleted data behind the junction")
	}
}

func TestRunKeepsTargetsIndependentAndOrdered(t *testing.T) {
	cfg, good := testCfg(t, nil)
	bad := good
	bad.Name = "bad"
	cfg.Targets = []config.Target{bad, good}
	cfg.Concurrency = 2
	fb := newFake()
	var n int
	var mu sync.Mutex
	fb.restore = func(_ context.Context, o restic.RestoreOptions) (restic.RestoreSummary, error) {
		mu.Lock()
		n++
		first := n == 1
		mu.Unlock()
		if first {
			return restic.RestoreSummary{}, &restic.Error{Command: "restore", ExitCode: 1, Kind: restic.KindGeneric}
		}
		writeFiles(o.Target, map[string]string{"C/a.txt": "x"})
		return restic.RestoreSummary{}, nil
	}
	run, err := engineFor(t, cfg, fb).Run(context.Background(), RunOptions{Trigger: TriggerCLI})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Targets) != 2 || run.Targets[0].Name != "bad" || run.Targets[1].Name != "t1" {
		t.Fatalf("order not preserved: %+v", run.Targets)
	}
	passed := 0
	for _, tr := range run.Targets {
		if tr.Status == Passed {
			passed++
		}
	}
	if passed != 1 || run.Summary.Total != 2 {
		t.Errorf("one failure must not hide the other result: %+v", run.Summary)
	}
}

func TestSelectTargets(t *testing.T) {
	cfg, tg := testCfg(t, nil)
	off := tg
	off.Name = "off"
	off.Enabled = ptr(false)
	cfg.Targets = []config.Target{tg, off}
	got, err := SelectTargets(cfg, nil)
	if err != nil || len(got) != 1 || got[0].Name != "t1" {
		t.Errorf("default selection: %+v %v", got, err)
	}
	if got, err := SelectTargets(cfg, []string{"OFF"}); err != nil || len(got) != 1 {
		t.Errorf("explicit disabled target must run: %+v %v", got, err)
	}
	if _, err := SelectTargets(cfg, []string{"nope"}); err == nil {
		t.Error("unknown target accepted")
	} else {
		var u UnknownTargetError
		if !errors.As(err, &u) {
			t.Errorf("wrong error type %T", err)
		}
	}
	cfg.Targets = []config.Target{off}
	if _, err := SelectTargets(cfg, nil); !errors.Is(err, ErrNoTargets) {
		t.Errorf("err = %v", err)
	}
}

func TestExitCodeMapping(t *testing.T) {
	mk := func(statuses ...Status) *Run {
		r := NewRun("x", TriggerCLI, time.Now())
		for _, s := range statuses {
			r.Targets = append(r.Targets, TargetResult{Name: "t", Status: s})
		}
		r.Finish(time.Now())
		return r
	}
	cases := []struct {
		r    *Run
		want int
	}{
		{mk(Passed), ExitOK},
		{mk(Passed, Passed), ExitOK},
		{mk(Failed), ExitFailed},
		{mk(Partial), ExitFailed},
		{mk(Passed, Failed), ExitFailed},
		{mk(NotRun), ExitError},
		{mk(Passed, NotRun), ExitError},
		{mk(Failed, NotRun), ExitFailed},
		{mk(Cancelled), ExitCancelled},
		{mk(TimedOut), ExitCancelled},
		{mk(Failed, Cancelled), ExitCancelled},
		{mk(), ExitError},
	}
	for i, c := range cases {
		if got := c.r.ExitCode(); got != c.want {
			t.Errorf("case %d (%s): exit %d want %d", i, c.r.Status, got, c.want)
		}
	}
}

func TestTargetStatusAggregation(t *testing.T) {
	s := func(id string, status Status) Stage { return Stage{ID: id, Status: status} }
	cases := []struct {
		name   string
		stages []Stage
		want   Status
	}{
		{"all pass", []Stage{s(StageCheck, Passed), s(StageRestore, Passed), s(StageCleanup, Passed)}, Passed},
		{"nothing proven", []Stage{s(StageRestic, Passed), s(StageCheck, Skipped), s(StageRestore, Skipped)}, NotRun},
		{"preflight not run", []Stage{s(StagePassword, NotRun), s(StageCheck, NotRun)}, NotRun},
		{"proof plus not_run is partial", []Stage{s(StageCheck, Passed), s(StageRestore, NotRun)}, Partial},
		{"failed wins over partial", []Stage{s(StageCheck, Failed), s(StageRestore, Partial)}, Failed},
		{"cleanup failure downgrades", []Stage{s(StageCheck, Passed), s(StageCleanup, Failed)}, Partial},
		{"cancel wins", []Stage{s(StageCheck, Failed), s(StageRestore, Cancelled)}, Cancelled},
		{"timeout", []Stage{s(StageCheck, Passed), s(StageRestore, TimedOut)}, TimedOut},
	}
	for _, c := range cases {
		if got := targetStatus(c.stages); got != c.want {
			t.Errorf("%s: %s want %s", c.name, got, c.want)
		}
	}
}

func TestGlobMatching(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{"**/a.json", "C/Users/me/a.json", true},
		{"**/a.json", "a.json", true},
		{"**/a.json", "C/x/b.json", false},
		{"C/data/*.txt", "C/data/x.txt", true},
		{"C/data/*.txt", "C/data/sub/x.txt", false},
		{"C/**/x.txt", "C/a/b/c/x.txt", true},
		{"**", "any/thing", true},
		{"**/*.SQLITE", "d/e/f.sqlite", true},
		{`C\data\*.txt`, "C/data/x.txt", true},
		{"./C/data/x.txt", "C/data/x.txt", true},
		{"a/[bc]/d", "a/b/d", true},
		{"a/[bc]/d", "a/x/d", false},
	}
	for _, c := range cases {
		if got := newMatcher(c.pat).Match(c.path); got != c.want {
			t.Errorf("%q vs %q = %v want %v", c.pat, c.path, got, c.want)
		}
	}
}
