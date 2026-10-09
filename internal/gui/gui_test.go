package gui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backupharbor/internal/config"
	"backupharbor/internal/restic"
	"backupharbor/internal/schedule"
	"backupharbor/internal/secret"
	"backupharbor/internal/verify"
)

type fakeBackend struct {
	block bool
	panic bool
	mu    sync.Mutex
	calls int
}

func (f *fakeBackend) hit() {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.panic {
		panic("the backend must not be touched in demo mode")
	}
}
func (f *fakeBackend) Version(context.Context) (restic.Version, error) {
	f.hit()
	return restic.Version{Minor: 19}, nil
}
func (f *fakeBackend) CatConfig(context.Context, restic.Repo) error { f.hit(); return nil }
func (f *fakeBackend) Snapshots(context.Context, restic.Repo, restic.SnapshotFilter) ([]restic.Snapshot, error) {
	f.hit()
	return []restic.Snapshot{{ID: "aaaa1111aaaa1111", ShortID: "aaaa1111", Time: time.Now().Add(-time.Hour)}}, nil
}
func (f *fakeBackend) Check(context.Context, restic.Repo, restic.CheckOptions) (restic.CheckResult, error) {
	f.hit()
	return restic.CheckResult{}, nil
}
func (f *fakeBackend) Stats(context.Context, restic.Repo, string) (restic.Stats, error) {
	f.hit()
	return restic.Stats{TotalSize: 5}, nil
}
func (f *fakeBackend) Restore(ctx context.Context, _ restic.Repo, o restic.RestoreOptions, _ func(restic.RestoreProgress)) (restic.RestoreSummary, error) {
	f.hit()
	if f.block {
		<-ctx.Done()
		return restic.RestoreSummary{}, ctx.Err()
	}
	p := filepath.Join(o.Target, "C")
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "a.txt"), []byte("x"), 0o644)
	return restic.RestoreSummary{}, nil
}

type fakeRunner struct{ fail bool }

func (f fakeRunner) Run(_ context.Context, name string, args []string, _ []byte) ([]byte, []byte, int, error) {
	if name == "schtasks.exe" && f.fail {
		return nil, []byte("ERROR: Access is denied."), 1, nil
	}
	return []byte(`{"installed":false}`), nil, 0, nil
}

type harness struct {
	t      *testing.T
	a      *App
	dir    string
	be     *fakeBackend
	store  *secret.MemoryStore
	events []verify.Event
	done   chan DoneEvent
	mu     sync.Mutex
	logs   []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "local"))
	h := &harness{t: t, dir: dir, be: &fakeBackend{}, store: secret.NewMemoryStore(), done: make(chan DoneEvent, 4)}
	h.a = New()
	h.a.ConfigOverride = filepath.Join(dir, "config.yaml")
	h.a.Backend, h.a.Secrets = h.be, h.store
	h.a.Scheduler = &schedule.Manager{TaskName: schedule.DefaultTaskName, Runner: fakeRunner{}}
	h.a.ExePath = func() (string, error) { return filepath.Join(dir, "BackupHarbor.exe"), nil }
	h.a.Emit = func(ev string, data any) {
		h.mu.Lock()
		defer h.mu.Unlock()
		switch ev {
		case EventProgress:
			h.events = append(h.events, data.(verify.Event))
		case EventDone:
			h.done <- data.(DoneEvent)
		}
	}
	t.Cleanup(func() { h.a.shutdown() })
	return h
}

func (h *harness) cfg(extra string) {
	y := "version: 1\ntemp_dir: '" + filepath.Join(h.dir, "work") + "'\nreports: {dir: '" + filepath.Join(h.dir, "reports") + "'}\n" + extra
	if err := os.WriteFile(h.a.ConfigOverride, []byte(y), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) target(name string) config.Target {
	repo := filepath.Join(h.dir, "repo-"+name)
	os.MkdirAll(repo, 0o755)
	t := h.a.NewTarget()
	t.Name, t.Repository = name, repo
	t.Password = config.PasswordConfig{Source: "credential"}
	return t
}

func (h *harness) wait() DoneEvent {
	select {
	case d := <-h.done:
		return d
	case <-time.After(60 * time.Second):
		h.t.Fatal("run did not finish")
		return DoneEvent{}
	}
}

func TestFirstRunBootstrap(t *testing.T) {
	h := newHarness(t)
	b := h.a.Bootstrap()
	if b.ConfigExists || !b.FirstRun || b.TargetCount != 0 || b.Catalog["status.passed"] == "" || b.Version == "" {
		t.Errorf("bootstrap: %+v", b)
	}
	path, err := h.a.InitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("config not created")
	}
	// A second call never overwrites.
	os.WriteFile(path, append(mustRead(t, path), []byte("# keep me\n")...), 0o644)
	h.a.InitConfig()
	if !strings.Contains(string(mustRead(t, path)), "# keep me") {
		t.Error("InitConfig overwrote an existing configuration")
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSettingsAreValidatedBeforeSaving(t *testing.T) {
	h := newHarness(t)
	h.cfg("")
	s, _ := h.a.GetSettings()
	before := mustRead(t, h.a.ConfigOverride)
	s.Concurrency = 99
	res, err := h.a.SaveSettings(s)
	if err != nil || res.Saved || len(res.Messages) == 0 {
		t.Fatalf("invalid settings saved: %+v %v", res, err)
	}
	if string(before) != string(mustRead(t, h.a.ConfigOverride)) {
		t.Error("file changed by a rejected save")
	}
	s.Concurrency, s.Language, s.Theme, s.Timeout = 3, "tr", "dark", "90m"
	if res, err = h.a.SaveSettings(s); err != nil || !res.Saved {
		t.Fatalf("%+v %v", res, err)
	}
	b := h.a.Bootstrap()
	if b.Language != "tr" || b.Theme != "dark" || b.Catalog["status.passed"] != "başarılı" {
		t.Errorf("language/theme not applied: %+v", b.Language)
	}
	s.Timeout = "soon"
	if res, _ := h.a.SaveSettings(s); res.Saved {
		t.Error("garbage timeout accepted")
	}
}

func TestTargetLifecycleAndPasswordsStayOutOfTheConfigFile(t *testing.T) {
	h := newHarness(t)
	h.cfg("")
	tg := h.target("docs")
	bad := tg
	bad.Name = "bad name!"
	if res, _ := h.a.SaveTarget(bad, "", ""); res.Saved || len(res.Messages) == 0 {
		t.Fatal("invalid target saved")
	}
	res, err := h.a.SaveTarget(tg, "", "hunter2-gui-secret")
	if err != nil || !res.Saved {
		t.Fatalf("%+v %v", res, err)
	}
	if strings.Contains(string(mustRead(t, h.a.ConfigOverride)), "hunter2-gui-secret") {
		t.Fatal("password written to the configuration file")
	}
	if v, err := h.store.Get(secret.CredentialPrefix + "docs"); err != nil || v != "hunter2-gui-secret" {
		t.Errorf("credential not stored: %q %v", v, err)
	}
	view, _ := h.a.ListTargets()
	if len(view.Targets) != 1 || !view.HasPassword["docs"] {
		t.Errorf("view: %+v", view)
	}
	// Duplicate names are rejected.
	if res, _ := h.a.SaveTarget(h.target("DOCS"), "", ""); res.Saved {
		t.Error("duplicate name accepted")
	}
	// Renaming carries the default credential along.
	renamed := view.Targets[0]
	renamed.Name = "documents"
	if res, err := h.a.SaveTarget(renamed, "docs", ""); err != nil || !res.Saved {
		t.Fatalf("rename: %+v %v", res, err)
	}
	if _, err := h.store.Get(secret.CredentialPrefix + "documents"); err != nil {
		t.Error("credential lost on rename")
	}
	if _, err := h.store.Get(secret.CredentialPrefix + "docs"); err == nil {
		t.Error("orphaned credential left behind")
	}
	// Enable / disable.
	if err := h.a.SetTargetEnabled("documents", false); err != nil {
		t.Fatal(err)
	}
	v2, _ := h.a.ListTargets()
	if v2.Targets[0].IsEnabled() {
		t.Error("target still enabled")
	}
	if err := h.a.SetTargetEnabled("nope", true); err == nil {
		t.Error("unknown target accepted")
	}
	if err := h.a.DeleteTarget("documents", true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.Get(secret.CredentialPrefix + "documents"); err == nil {
		t.Error("credential not deleted with the target")
	}
	if v3, _ := h.a.ListTargets(); len(v3.Targets) != 0 {
		t.Error("target still listed")
	}
}

func TestTestTargetUsesTypedPasswordWithoutSavingIt(t *testing.T) {
	h := newHarness(t)
	h.cfg("")
	tg := h.target("probe")
	res := h.a.TestTarget(tg, "")
	if res.Result.Status == verify.Passed {
		t.Error("no stored password: the test must not pass")
	}
	res = h.a.TestTarget(tg, "typed-only")
	if res.Result.Status != verify.Passed {
		t.Fatalf("typed password should be usable for the test: %+v %s", res.Result.Status, res.Error)
	}
	if _, err := h.store.Get(secret.CredentialPrefix + "probe"); err == nil {
		t.Error("testing stored the password")
	}
	for _, s := range res.Result.Stages {
		if s.ID == verify.StageRestore || s.ID == verify.StageCheck {
			t.Errorf("a connection test must not run %s", s.ID)
		}
	}
	if h.be.calls == 0 {
		t.Error("backend never used")
	}
	// Invalid targets are reported, not attempted.
	bad := tg
	bad.Repository = ""
	if r := h.a.TestTarget(bad, ""); len(r.Messages) == 0 {
		t.Error("invalid target produced no messages")
	}
}

func TestRealRunPublishesEventsAndStoresHistory(t *testing.T) {
	h := newHarness(t)
	h.cfg("")
	h.a.SaveTarget(h.target("docs"), "", "pw")
	id, err := h.a.StartRun(nil)
	if err != nil {
		t.Fatal(err)
	}
	d := h.wait()
	if d.Error != "" || d.Run == nil || d.Run.Status != verify.Passed || d.Run.RunID != id || d.Run.Trigger != verify.TriggerGUI || d.Demo {
		t.Fatalf("done: %+v", d)
	}
	if len(d.Saved) == 0 {
		t.Error("run not saved")
	}
	h.mu.Lock()
	kinds := map[string]bool{}
	for _, e := range h.events {
		kinds[e.Type] = true
	}
	h.mu.Unlock()
	for _, k := range []string{"run_start", "target_start", "stage_start", "stage_end", "target_end", "run_end"} {
		if !kinds[k] {
			t.Errorf("no %s event", k)
		}
	}
	dash, err := h.a.GetDashboard()
	if err != nil || dash.Demo || len(dash.Targets) != 1 || dash.Targets[0].Status != "passed" || dash.Targets[0].LastVerifiedAt == nil {
		t.Errorf("dashboard: %+v %v", dash, err)
	}
	list, _ := h.a.ListReports(ReportFilter{})
	if len(list) != 1 {
		t.Fatalf("history: %d", len(list))
	}
	rep, err := h.a.GetReport(list[0].RunID)
	if err != nil || rep.Targets[0].Name != "docs" {
		t.Errorf("report: %v", err)
	}
	if st := h.a.GetRunState(); st.Running || len(st.Events) == 0 {
		t.Errorf("run state: %+v", st.Running)
	}
}

func TestOnlyOneRunAtATimeAndCancelling(t *testing.T) {
	h := newHarness(t)
	h.cfg("")
	h.a.SaveTarget(h.target("docs"), "", "pw")
	h.be.block = true
	if _, err := h.a.StartRun(nil); err != nil {
		t.Fatal(err)
	}
	if !h.a.IsRunning() {
		t.Fatal("not running")
	}
	if _, err := h.a.StartRun(nil); err == nil {
		t.Error("second concurrent run accepted")
	}
	if res, err := h.a.SaveTarget(h.target("other"), "", ""); err == nil && res.Saved {
		t.Error("configuration changed during a run")
	}
	time.Sleep(500 * time.Millisecond)
	h.a.CancelRun()
	d := h.wait()
	if d.ExitCode != verify.ExitCancelled || d.Run == nil || d.Run.Status != verify.Cancelled {
		t.Errorf("done: exit=%d run=%+v", d.ExitCode, d.Run)
	}
	if es, _ := os.ReadDir(filepath.Join(h.dir, "work")); len(es) != 0 {
		t.Errorf("cancelled run left files: %v", es)
	}
	if h.a.IsRunning() {
		t.Error("still running")
	}
}

func TestStartRunErrors(t *testing.T) {
	h := newHarness(t)
	h.cfg("")
	if _, err := h.a.StartRun(nil); err == nil {
		t.Error("run without targets accepted")
	}
	h.a.SaveTarget(h.target("docs"), "", "pw")
	if _, err := h.a.StartRun([]string{"ghost"}); err == nil {
		t.Error("unknown target accepted")
	}
}

func TestDemoModeNeverTouchesRepositoriesOrHistory(t *testing.T) {
	h := newHarness(t)
	h.cfg("")
	h.a.SaveTarget(h.target("real"), "", "pw")
	callsBefore := h.be.calls
	h.be.panic = true // any backend access would panic the test
	if err := h.a.SetDemo(true); err != nil {
		t.Fatal(err)
	}
	dash, _ := h.a.GetDashboard()
	if !dash.Demo || len(dash.Targets) < 4 {
		t.Errorf("demo dashboard: %+v", dash)
	}
	for _, c := range dash.Targets {
		if c.Name == "real" {
			t.Error("real target mixed into the demo view")
		}
	}
	list, _ := h.a.ListReports(ReportFilter{})
	if len(list) == 0 || !strings.HasPrefix(list[0].RunID, "DEMO-") {
		t.Errorf("demo history: %+v", list)
	}
	if _, err := h.a.StartRun(nil); err != nil {
		t.Fatal(err)
	}
	d := h.wait()
	if !d.Demo || d.Run == nil || !d.Run.Demo || len(d.Saved) != 0 {
		t.Fatalf("demo run: %+v", d)
	}
	if h.be.calls != callsBefore {
		t.Error("demo mode called restic")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "reports", "runs")); err == nil {
		es, _ := os.ReadDir(filepath.Join(h.dir, "reports", "runs"))
		if len(es) != 0 {
			t.Errorf("demo run was stored: %v", es)
		}
	}
	if es, _ := os.ReadDir(filepath.Join(h.dir, "work")); len(es) != 0 {
		t.Error("demo created work directories")
	}
	if err := h.a.InstallSchedule(config.ScheduleConfig{Frequency: "daily", Time: "03:00"}); err == nil {
		t.Error("demo mode may not change the scheduled task")
	}
	h.a.SetDemo(false)
	h.be.panic = false
	if d2, _ := h.a.GetDashboard(); d2.Demo {
		t.Error("demo flag stuck")
	}
}

func TestScheduleInstallFailureIsSurfaced(t *testing.T) {
	h := newHarness(t)
	h.cfg("")
	h.a.SaveTarget(h.target("docs"), "", "pw")
	os.WriteFile(filepath.Join(h.dir, "bharbor.exe"), []byte("x"), 0o755)
	h.a.Scheduler = &schedule.Manager{TaskName: schedule.DefaultTaskName, Runner: fakeRunner{fail: true}}
	err := h.a.InstallSchedule(config.ScheduleConfig{Frequency: "daily", Time: "03:00"})
	if err == nil || !strings.Contains(err.Error(), "Access is denied") {
		t.Fatalf("err = %v", err)
	}
	// Missing CLI next to the app is also an error, not a silent success.
	os.Remove(filepath.Join(h.dir, "bharbor.exe"))
	h.a.Scheduler = &schedule.Manager{TaskName: schedule.DefaultTaskName, Runner: fakeRunner{}}
	if err := h.a.InstallSchedule(config.ScheduleConfig{Frequency: "daily", Time: "03:00"}); err == nil {
		t.Error("installed without bharbor.exe")
	}
	if err := h.a.InstallSchedule(config.ScheduleConfig{Frequency: "daily", Time: "99:99"}); err == nil {
		t.Error("invalid time accepted")
	}
	info, err := h.a.GetSchedule()
	if err != nil || info.Status.Installed {
		t.Errorf("schedule info: %+v %v", info, err)
	}
}

func TestExportNeverWritesWithoutDestination(t *testing.T) {
	h := newHarness(t)
	h.cfg("")
	h.a.SaveTarget(h.target("docs"), "", "pw")
	h.a.StartRun(nil)
	d := h.wait()
	dest := filepath.Join(h.dir, "out", "r")
	h.a.SaveFile = func(title, name, filter string) (string, error) { return dest, nil }
	for _, f := range []string{"json", "html", "txt"} {
		p, err := h.a.ExportReport(d.Run.RunID, f, "")
		if err != nil || !strings.HasSuffix(p, "."+f) {
			t.Fatalf("%s: %q %v", f, p, err)
		}
		if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
			t.Errorf("%s not written", f)
		}
	}
	h.a.SaveFile = func(string, string, string) (string, error) { return "", nil }
	if p, err := h.a.ExportReport(d.Run.RunID, "json", ""); p != "" || err != nil {
		t.Errorf("cancelled dialog must be a no-op: %q %v", p, err)
	}
	if _, err := h.a.ExportReport("20000101T000000Z-000000", "json", ""); err == nil {
		t.Error("unknown run exported")
	}
}

func TestAppInfo(t *testing.T) {
	h := newHarness(t)
	h.cfg("")
	i := h.a.AppInfo()
	if i.Product != "BackupHarbor" || i.Version == "" || i.GoVersion == "" || i.Platform == "" {
		t.Errorf("%+v", i)
	}
}
