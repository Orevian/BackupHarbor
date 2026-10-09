package gui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"backupharbor/internal/app"
	"backupharbor/internal/config"
	"backupharbor/internal/demo"
	"backupharbor/internal/report"
	"backupharbor/internal/schedule"
	"backupharbor/internal/secret"
	"backupharbor/internal/verify"
)

// TargetCard is one target on the dashboard.
type TargetCard struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Repository string `json:"repository"`
	// Status is the latest result, or "never" if the target was never verified.
	Status            string     `json:"status"`
	LastRunID         string     `json:"last_run_id,omitempty"`
	LastVerifiedAt    *time.Time `json:"last_verified_at,omitempty"`
	SnapshotID        string     `json:"snapshot_id,omitempty"`
	SnapshotTime      *time.Time `json:"snapshot_time,omitempty"`
	RestoreDurationMS int64      `json:"restore_duration_ms,omitempty"`
	Files             int64      `json:"files,omitempty"`
	Bytes             int64      `json:"bytes,omitempty"`
}

// Dashboard is the overview shown on the main panel.
type Dashboard struct {
	Demo      bool             `json:"demo"`
	HasConfig bool             `json:"has_config"`
	Targets   []TargetCard     `json:"targets"`
	Counts    map[string]int   `json:"counts"`
	LastRun   *report.Summary  `json:"last_run,omitempty"`
	Recent    []report.Summary `json:"recent"`
}

func (a *App) store() (*report.Store, *config.Config, error) {
	cfg, _, err := a.loadConfig()
	if err != nil {
		return nil, nil, err
	}
	return report.NewStore(cfg.Resolved().ReportDir), cfg, nil
}

// GetDashboard returns the overview. In demo mode it returns demonstration data only.
func (a *App) GetDashboard() (Dashboard, error) {
	d := Dashboard{Targets: []TargetCard{}, Counts: map[string]int{}, Recent: []report.Summary{}, Demo: a.IsDemo()}
	if d.Demo {
		hist := demo.History(time.Now())
		latest := report.Summarize(hist[0])
		d.HasConfig, d.LastRun = true, &latest
		for _, t := range hist[0].Targets {
			card := cardFrom(t.Name, true, t.Repository, report.Summarize(hist[0]).Targets, hist[0].RunID)
			d.Targets = append(d.Targets, card)
			d.Counts[card.Status]++
		}
		for _, r := range hist {
			d.Recent = append(d.Recent, report.Summarize(r))
		}
		return d, nil
	}
	st, cfg, err := a.store()
	if err != nil {
		return d, err
	}
	_, statErr := fileExists(cfg.Path)
	d.HasConfig = statErr == nil
	latest, err := st.LatestPerTarget()
	if err != nil {
		return d, err
	}
	for _, t := range cfg.Targets {
		c := TargetCard{Name: t.Name, Enabled: t.IsEnabled(), Repository: secret.RedactRepository(t.Repository), Status: "never"}
		if ts, ok := latest[strings.ToLower(t.Name)]; ok {
			c.Status = string(ts.Status)
			fin := ts.FinishedAt
			c.LastVerifiedAt = &fin
			c.SnapshotID, c.SnapshotTime = ts.SnapshotID, ts.SnapshotTime
			c.RestoreDurationMS, c.Files, c.Bytes = ts.RestoreDurationMS, ts.Files, ts.Bytes
		}
		d.Targets = append(d.Targets, c)
		if c.Enabled {
			d.Counts[c.Status]++
		}
	}
	recent, err := st.List(report.Filter{Limit: 5})
	if err != nil {
		return d, err
	}
	if recent != nil {
		d.Recent = recent
	}
	if len(recent) > 0 {
		d.LastRun = &recent[0]
	}
	return d, nil
}

func cardFrom(name string, enabled bool, repo string, targets []report.TargetSummary, runID string) TargetCard {
	c := TargetCard{Name: name, Enabled: enabled, Repository: repo, Status: "never"}
	for _, ts := range targets {
		if ts.Name == name {
			c.Status, c.LastRunID = string(ts.Status), runID
			fin := ts.FinishedAt
			c.LastVerifiedAt = &fin
			c.SnapshotID, c.SnapshotTime = ts.SnapshotID, ts.SnapshotTime
			c.RestoreDurationMS, c.Files, c.Bytes = ts.RestoreDurationMS, ts.Files, ts.Bytes
		}
	}
	return c
}

func fileExists(p string) (bool, error) {
	_, err := osStat(p)
	return err == nil, err
}

// ReportFilter is the query of the reports page.
type ReportFilter struct {
	Target string `json:"target"`
	Since  string `json:"since"` // YYYY-MM-DD
	Until  string `json:"until"` // YYYY-MM-DD, inclusive
	Status string `json:"status"`
	Limit  int    `json:"limit"`
}

// ListReports returns the history, newest first.
func (a *App) ListReports(f ReportFilter) ([]report.Summary, error) {
	flt := report.Filter{Target: strings.TrimSpace(f.Target), Status: verify.Status(f.Status), Limit: f.Limit}
	var err error
	if f.Since != "" {
		if flt.Since, err = time.ParseInLocation("2006-01-02", f.Since, time.Local); err != nil {
			return nil, fmt.Errorf("invalid date %q", f.Since)
		}
	}
	if f.Until != "" {
		u, perr := time.ParseInLocation("2006-01-02", f.Until, time.Local)
		if perr != nil {
			return nil, fmt.Errorf("invalid date %q", f.Until)
		}
		flt.Until = u.AddDate(0, 0, 1)
	}
	if a.IsDemo() {
		var out []report.Summary
		for _, r := range demo.History(time.Now()) {
			s := report.Summarize(r)
			if matchDemo(flt, s) {
				out = append(out, s)
			}
		}
		if out == nil {
			out = []report.Summary{}
		}
		return out, nil
	}
	st, _, err := a.store()
	if err != nil {
		return nil, err
	}
	l, err := st.List(flt)
	if l == nil {
		l = []report.Summary{}
	}
	return l, err
}

func matchDemo(f report.Filter, s report.Summary) bool {
	if !f.Since.IsZero() && s.StartedAt.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && !s.StartedAt.Before(f.Until) {
		return false
	}
	if f.Target != "" {
		hit := false
		for _, t := range s.Targets {
			if strings.EqualFold(t.Name, f.Target) {
				hit = true
			}
		}
		return hit
	}
	return true
}

// GetReport returns the full record of a run.
func (a *App) GetReport(runID string) (*verify.Run, error) {
	if a.IsDemo() || strings.HasPrefix(runID, "DEMO-") {
		for _, r := range demo.History(time.Now()) {
			if r.RunID == runID {
				return r, nil
			}
		}
		return nil, report.ErrNotFound
	}
	st, _, err := a.store()
	if err != nil {
		return nil, err
	}
	return st.Load(runID)
}

// ExportReport asks for a destination and writes the run in the given format
// (json, html or txt). It returns the written path, or "" if the user cancelled.
// The save dialog confirms overwriting; nothing is replaced silently.
func (a *App) ExportReport(runID, format, targetName string) (string, error) {
	if a.SaveFile == nil {
		return "", errors.New("dialogs are not available")
	}
	run, err := a.GetReport(runID)
	if err != nil {
		return "", err
	}
	if targetName != "" {
		if run, err = report.FilterTarget(run, targetName); err != nil {
			return "", err
		}
	}
	cfg, _, _ := a.loadConfig()
	lang := a.lang(cfg)
	name := runID
	if targetName != "" {
		name += "-" + targetName
	}
	path, err := a.SaveFile("BackupHarbor", name+"."+format, "*."+format)
	if err != nil || path == "" {
		return "", err
	}
	if strings.ToLower(filepath.Ext(path)) != "."+format {
		path += "." + format
	}
	// The dialog already asked about replacing an existing file.
	if err := report.Export(path, format, run, lang, true); err != nil {
		return "", err
	}
	return path, nil
}

// OpenReport opens the HTML rendering of a run in the default browser. The
// rendering is created next to the JSON record if it does not exist yet.
func (a *App) OpenReport(runID string) error {
	cfg, _, err := a.loadConfig()
	if err != nil {
		return err
	}
	run, err := a.GetReport(runID)
	if err != nil {
		return err
	}
	lang := a.lang(cfg)
	var path string
	if run.Demo {
		path = filepath.Join(config.DataDir(), "demo", runID+"."+lang+".html")
		if err := report.Export(path, report.FormatHTML, run, lang, true); err != nil {
			return err
		}
	} else {
		st := report.NewStore(cfg.Resolved().ReportDir)
		path = st.RunPath(runID, report.FormatHTML)
		if err := report.Export(path, report.FormatHTML, run, lang, false); err != nil && !errors.Is(err, report.ErrExists) {
			return err
		}
	}
	return a.open(path)
}

// ---- schedule --------------------------------------------------------------------

// ScheduleInfo combines the configured schedule with the observed task state.
type ScheduleInfo struct {
	Config    config.ScheduleConfig `json:"config"`
	Status    schedule.Status       `json:"status"`
	Error     string                `json:"error,omitempty"`
	CLIExists bool                  `json:"cli_exists"`
	CLIPath   string                `json:"cli_path"`
}

func (a *App) cliPath() string {
	exe, err := a.exePath()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "bharbor.exe")
}

func (a *App) exePath() (string, error) {
	if a.ExePath != nil {
		return a.ExePath()
	}
	return osExecutable()
}

// GetSchedule returns the schedule configuration and the live task status.
func (a *App) GetSchedule() (ScheduleInfo, error) {
	cfg, _, err := a.loadConfig()
	if err != nil {
		return ScheduleInfo{}, err
	}
	info := ScheduleInfo{Config: cfg.Schedule, CLIPath: a.cliPath()}
	_, serr := osStat(info.CLIPath)
	info.CLIExists = serr == nil
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	st, err := a.scheduler().Status(ctx)
	if err != nil {
		info.Error = err.Error()
	}
	info.Status = st
	return info, nil
}

// InstallSchedule saves the schedule settings and installs the Windows task.
// An error is returned (and nothing is reported as installed) if the task
// cannot be created or cannot be read back.
func (a *App) InstallSchedule(sc config.ScheduleConfig) error {
	if a.IsDemo() {
		return errors.New("demo mode does not change the scheduled task")
	}
	cfg, _, err := a.loadConfig()
	if err != nil {
		return err
	}
	cfg.Schedule = sc
	cfg.Normalize()
	if issues := cfg.ValidateStatic(cfg.BaseDir()); config.HasErrors(issues) {
		return errors.New(a.result(cfg, issues, false).Messages[0])
	}
	cli := a.cliPath()
	if _, err := osStat(cli); err != nil {
		return fmt.Errorf("bharbor.exe was not found next to the application (%s)", cli)
	}
	spec, err := app.ScheduleSpec(cfg, cli)
	if err != nil {
		return err
	}
	// The configuration must exist on disk because the task points at it.
	if err := cfg.Save(cfg.Path); err != nil {
		return err
	}
	ctx, cancel := ctxTimeout(2 * time.Minute)
	defer cancel()
	return a.scheduler().Install(ctx, spec)
}

// RemoveSchedule deletes the managed task.
func (a *App) RemoveSchedule() error {
	ctx, cancel := ctxTimeout(2 * time.Minute)
	defer cancel()
	err := a.scheduler().Remove(ctx)
	if errors.Is(err, schedule.ErrNotInstalled) {
		return nil
	}
	return err
}
