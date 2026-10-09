package verify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"backupharbor/internal/config"
	"backupharbor/internal/fsx"
	"backupharbor/internal/i18n"
	"backupharbor/internal/restic"
	"backupharbor/internal/secret"
	"backupharbor/internal/version"
	"backupharbor/internal/workspace"
)

// Backend is the restic functionality the engine needs. *restic.Client
// implements it; tests substitute fakes.
type Backend interface {
	Version(ctx context.Context) (restic.Version, error)
	CatConfig(ctx context.Context, repo restic.Repo) error
	Snapshots(ctx context.Context, repo restic.Repo, f restic.SnapshotFilter) ([]restic.Snapshot, error)
	Check(ctx context.Context, repo restic.Repo, o restic.CheckOptions) (restic.CheckResult, error)
	Stats(ctx context.Context, repo restic.Repo, snapshotID string) (restic.Stats, error)
	Restore(ctx context.Context, repo restic.Repo, o restic.RestoreOptions, progress func(restic.RestoreProgress)) (restic.RestoreSummary, error)
}

// Options configure an Engine.
type Options struct {
	// Backend overrides the restic client (tests). When nil the engine locates
	// restic from the configuration.
	Backend Backend
	// Secrets is the credential store; nil selects Windows Credential Manager.
	Secrets secret.Store
	// Progress receives events; it is never called concurrently.
	Progress func(Event)
	// Logf receives log lines (level is debug, info, warn or error).
	Logf func(level, format string, args ...any)
}

// Engine verifies targets of one configuration.
type Engine struct {
	cfg     *config.Config
	paths   config.Paths
	backend Backend
	secrets secret.Store
	opts    Options

	mu            sync.Mutex
	resticVersion string
}

// ErrDependency is returned when a required dependency (restic) is unavailable.
type ErrDependency struct{ Err error }

func (e ErrDependency) Error() string { return e.Err.Error() }
func (e ErrDependency) Unwrap() error { return e.Err }

// New creates an engine. Unless opts.Backend is set it locates restic and
// returns ErrDependency if that fails.
func New(cfg *config.Config, opts Options) (*Engine, error) {
	e := &Engine{cfg: cfg, paths: cfg.Resolved(), opts: opts, secrets: opts.Secrets}
	if e.secrets == nil {
		e.secrets = secret.NewCredentialManager()
	}
	if opts.Backend != nil {
		e.backend = opts.Backend
		return e, nil
	}
	exe, err := restic.Locate(e.paths.Restic)
	if err != nil {
		return nil, ErrDependency{err}
	}
	c := restic.New(exe)
	c.Logf = func(f string, a ...any) { e.logf("debug", f, a...) }
	e.backend = c
	return e, nil
}

// ResticVersion returns the restic version observed by the last run.
func (e *Engine) ResticVersion() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.resticVersion
}

func (e *Engine) logf(level, format string, args ...any) {
	if e.opts.Logf != nil {
		e.opts.Logf(level, format, args...)
	}
}

func (e *Engine) emit(ev Event) {
	if e.opts.Progress == nil {
		return
	}
	ev.Time = time.Now().UTC()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.opts.Progress(ev)
}

// mode selects how far VerifyTarget goes.
type mode int

const (
	modeFull      mode = iota // all stages
	modePreflight             // restic, directories, password, repository, snapshot only
)

// VerifyTarget runs the complete verification of one target.
func (e *Engine) VerifyTarget(ctx context.Context, t config.Target, runID string) TargetResult {
	return e.execute(ctx, t, runID, modeFull)
}

// Preflight runs only the non-destructive readiness stages (used by "bharbor validate").
func (e *Engine) Preflight(ctx context.Context, t config.Target, runID string) TargetResult {
	return e.execute(ctx, t, runID, modePreflight)
}

// target execution state
type targetRun struct {
	e      *Engine
	ctx    context.Context
	t      config.Target
	runID  string
	tr     *TargetResult
	red    *secret.Redactor
	repo   restic.Repo
	snap   *restic.Snapshot
	ws     *workspace.Workspace
	cols   []*collector
	walk   walkStats
	walked bool

	blocked   bool // a prerequisite stage failed: everything but cleanup is not run
	noRestore bool // the scratch directory is unusable
	restoreOK bool // restore produced a tree that content rules may inspect
}

func (e *Engine) execute(parent context.Context, t config.Target, runID string, m mode) TargetResult {
	start := time.Now().UTC()
	tr := TargetResult{Name: t.Name, StartedAt: start, Repository: secret.RedactRepository(t.Repository), Stages: []Stage{}}
	ctx, cancel := context.WithTimeout(parent, e.cfg.TimeoutFor(t))
	defer cancel()

	x := &targetRun{e: e, ctx: ctx, t: t, runID: runID, tr: &tr, red: secret.NewRedactor()}
	x.repo = restic.Repo{Location: e.cfg.ResolveRepository(t), UseLock: t.UseLock}
	e.emit(Event{Type: "target_start", RunID: runID, Target: t.Name})

	x.stage(StageRestic, x.stResticVersion)
	x.stage(StageDirs, x.stDirectories)
	x.stage(StagePassword, x.stPassword)
	x.stage(StageRepository, x.stRepository)
	x.stage(StageSnapshot, x.stSnapshot)
	if m == modeFull {
		x.stage(StageCheck, x.stCheck)
		x.stage(StageRestore, x.stRestore)
		x.stage(StageContent, x.stContent)
		x.stage(StageCleanup, x.stCleanup)
	}

	tr.FinishedAt = time.Now().UTC()
	tr.DurationMS = tr.FinishedAt.Sub(start).Milliseconds()
	if m == modeFull {
		tr.Status = targetStatus(tr.Stages)
		x.buildCoverage()
	} else {
		tr.Status = preflightStatus(tr.Stages)
	}
	e.emit(Event{Type: "target_end", RunID: runID, Target: t.Name, Status: tr.Status})
	return tr
}

// stage runs fn as one recorded stage.
func (x *targetRun) stage(id string, fn func(*Stage)) {
	st := Stage{ID: id, StartedAt: time.Now().UTC(), Status: NotRun}
	x.e.emit(Event{Type: "stage_start", RunID: x.runID, Target: x.t.Name, Stage: id, Percent: -1})
	begin := time.Now()
	switch {
	case id == StageCleanup:
		fn(&st) // cleanup always runs
	case x.blocked && id != StageRestic:
		st.Message = i18n.M("stage.blocked")
	case x.ctx.Err() != nil:
		x.interrupted(&st)
	default:
		fn(&st)
	}
	st.DurationMS = time.Since(begin).Milliseconds()
	x.tr.Stages = append(x.tr.Stages, st)
	x.e.logf("info", "%s: stage %s: %s", x.t.Name, id, st.Status)
	x.e.emit(Event{Type: "stage_end", RunID: x.runID, Target: x.t.Name, Stage: id, Status: st.Status, Message: st.Message, Percent: -1})
}

// interrupted marks st as cancelled or timed out when the context is done.
func (x *targetRun) interrupted(st *Stage) bool {
	err := x.ctx.Err()
	if err == nil {
		return false
	}
	x.blocked = true
	if errors.Is(err, context.DeadlineExceeded) {
		st.Status = TimedOut
		st.Message = i18n.M("stage.timed_out", x.e.cfg.TimeoutFor(x.t))
	} else {
		st.Status = Cancelled
		st.Message = i18n.M("stage.cancelled")
	}
	return true
}

func (x *targetRun) setNotRun(st *Stage, code string, args ...any) {
	st.Status = NotRun
	st.Message = i18n.M(code, args...)
}

func (x *targetRun) detail(err error) string {
	var re *restic.Error
	if errors.As(err, &re) && re.Stderr != "" {
		return x.red.Redact(re.Stderr)
	}
	return x.red.Redact(err.Error())
}

// ---- preflight stages -------------------------------------------------------

func (x *targetRun) stResticVersion(st *Stage) {
	v, err := x.e.backend.Version(x.ctx)
	if err != nil {
		if x.interrupted(st) {
			return
		}
		x.blocked = true
		x.setNotRun(st, "restic.unavailable")
		st.Detail = x.detail(err)
		return
	}
	if !v.AtLeast(version.MinResticMajor, version.MinResticMinor) {
		x.blocked = true
		x.setNotRun(st, "restic.too_old", v.String(), fmt.Sprintf("%d.%d", version.MinResticMajor, version.MinResticMinor))
		return
	}
	x.e.mu.Lock()
	x.e.resticVersion = v.String()
	x.e.mu.Unlock()
	st.Status = Passed
	st.Message = i18n.M("restic.ok", v.String())
	st.Evidence = map[string]any{"version": v.String()}
}

func (x *targetRun) stDirectories(st *Stage) {
	if !x.t.Restore.On() {
		st.Status = Skipped
		st.Message = i18n.M("stage.disabled")
		return
	}
	base := x.e.paths.TempDir
	if err := workspace.CheckBase(base); err != nil {
		x.noRestore = true
		x.setNotRun(st, "dirs.unusable", base, err.Error())
		return
	}
	if err := fsx.WritableDir(base, true); err != nil {
		x.noRestore = true
		x.setNotRun(st, "dirs.unusable", base, err.Error())
		return
	}
	st.Status = Passed
	st.Message = i18n.M("dirs.ok", base)
	st.Evidence = map[string]any{"temp_dir": base}
	if free, err := fsx.FreeSpace(base); err == nil {
		st.Evidence["free_bytes"] = free
	}
}

func (x *targetRun) stPassword(st *Stage) {
	src := secret.Source{Kind: x.t.Password.Source, Credential: x.t.CredentialName(),
		Env: x.t.Password.Env, File: x.e.cfg.ResolvePasswordFile(x.t)}
	pw, err := secret.Resolve(src, x.e.secrets)
	if err != nil {
		x.blocked = true
		switch x.t.Password.Source {
		case secret.SourceCredential:
			if errors.Is(err, secret.ErrNotFound) {
				x.setNotRun(st, "password.credential_missing", secret.CredentialPrefix+src.Credential)
			} else {
				x.setNotRun(st, "password.credential_error")
				st.Detail = x.detail(err)
			}
		case secret.SourceEnv:
			x.setNotRun(st, "password.env_missing", src.Env)
		default:
			x.setNotRun(st, "password.file_unreadable", src.File)
			st.Detail = x.detail(err)
		}
		return
	}
	x.red.Add(pw)
	x.repo.Password = pw
	st.Status = Passed
	st.Message = i18n.M("password.ok", x.t.Password.Source)
	st.Evidence = map[string]any{"source": x.t.Password.Source}
}

func (x *targetRun) stRepository(st *Stage) {
	err := x.e.backend.CatConfig(x.ctx, x.repo)
	if err == nil {
		st.Status = Passed
		st.Message = i18n.M("repo.ok")
		return
	}
	if x.interrupted(st) {
		return
	}
	x.blocked = true
	x.resticAccessProblem(st, err, "repo.open_failed")
}

// resticAccessProblem records a failure that means "could not be tested".
func (x *targetRun) resticAccessProblem(st *Stage, err error, fallback string) {
	st.Detail = x.detail(err)
	var re *restic.Error
	if errors.As(err, &re) {
		switch re.Kind {
		case restic.KindNoRepo:
			x.setNotRun(st, "repo.not_found")
			return
		case restic.KindWrongPassword:
			x.setNotRun(st, "repo.wrong_password")
			return
		case restic.KindLocked:
			x.setNotRun(st, "repo.locked")
			return
		case restic.KindStart:
			x.setNotRun(st, "restic.unavailable")
			return
		}
	}
	x.setNotRun(st, fallback)
}

func (x *targetRun) stSnapshot(st *Stage) {
	sel := x.t.Snapshot
	filter := restic.SnapshotFilter{Host: sel.Host}
	if sel.Select == "tag" {
		filter.Tags = sel.Tags
	}
	snaps, err := x.e.backend.Snapshots(x.ctx, x.repo, filter)
	if err != nil {
		if x.interrupted(st) {
			return
		}
		x.blocked = true
		x.resticAccessProblem(st, err, "snapshot.list_failed")
		return
	}
	var chosen *restic.Snapshot
	switch sel.Select {
	case "id":
		want := strings.ToLower(sel.ID)
		var hits []int
		for i, s := range snaps {
			if strings.HasPrefix(strings.ToLower(s.ID), want) {
				hits = append(hits, i)
			}
		}
		switch len(hits) {
		case 0:
			x.blocked = true
			x.setNotRun(st, "snapshot.id_not_found", sel.ID)
			return
		case 1:
			chosen = &snaps[hits[0]]
		default:
			x.blocked = true
			x.setNotRun(st, "snapshot.id_ambiguous", sel.ID)
			return
		}
	default:
		if len(snaps) == 0 {
			x.blocked = true
			st.Status = Failed
			st.Message = i18n.M("snapshot.none")
			return
		}
		chosen = &snaps[len(snaps)-1] // snapshots are sorted by time
	}
	x.snap = chosen
	info := &SnapshotInfo{ID: chosen.ID, ShortID: chosen.ShortID, Time: chosen.Time.UTC(), Hostname: chosen.Hostname,
		Username: chosen.Username, Paths: chosen.Paths, Tags: chosen.Tags}
	if info.ShortID == "" && len(info.ID) >= 8 {
		info.ShortID = info.ID[:8]
	}
	x.tr.Snapshot = info
	st.Status = Passed
	st.Message = i18n.M("snapshot.ok", info.ShortID, info.Time.Format(time.RFC3339))
	st.Evidence = map[string]any{"id": info.ID, "candidates": len(snaps), "select": sel.Select}
}

// ---- integrity check --------------------------------------------------------

func (x *targetRun) stCheck(st *Stage) {
	if !x.t.Check.On() {
		st.Status = Skipped
		st.Message = i18n.M("stage.disabled")
		return
	}
	opts := restic.CheckOptions{ReadData: x.t.Check.ReadData, Subset: x.t.Check.Subset}
	st.Evidence = map[string]any{"read_data": opts.ReadData}
	if opts.ReadData == "subset" {
		st.Evidence["subset"] = opts.Subset
	}
	res, err := x.e.backend.Check(x.ctx, x.repo, opts)
	if err == nil {
		st.Status = Passed
		if opts.ReadData == "subset" {
			st.Message = i18n.M("check.passed_subset", opts.Subset)
		} else {
			st.Message = i18n.M("check.passed_" + opts.ReadData)
		}
		st.Detail = x.red.Redact(res.Output)
		return
	}
	if x.interrupted(st) {
		return
	}
	var re *restic.Error
	if errors.As(err, &re) && (re.Kind == restic.KindGeneric || re.Kind == restic.KindIncomplete) {
		st.Status = Failed
		st.Message = i18n.M("check.failed")
		st.Detail = x.detail(err)
		return
	}
	x.resticAccessProblem(st, err, "check.not_run")
}

// ---- helpers shared with other stage files ---------------------------------

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
