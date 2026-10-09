// Package app composes configuration, the verification engine and the report
// store. The CLI, the GUI back end and the scheduled task all go through it, so
// they share one configuration location, one engine and one report history.
package app

import (
	"path/filepath"
	"time"

	"backupharbor/internal/schedule"
	"context"
	"errors"
	"fmt"

	"backupharbor/internal/config"
	"backupharbor/internal/i18n"
	"backupharbor/internal/report"
	"backupharbor/internal/secret"
	"backupharbor/internal/verify"
)

// ConfigError wraps configuration problems (exit code 2).
type ConfigError struct {
	Issues []config.Issue
	Err    error
}

func (e *ConfigError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return config.IssueError(e.Issues).Error()
}

// Unwrap exposes the cause.
func (e *ConfigError) Unwrap() error { return e.Err }

// LoadConfig loads and normalises the configuration at path (see config.ResolvePath).
func LoadConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, &ConfigError{Err: err}
	}
	return cfg, nil
}

// Language resolves the effective UI language of a configuration.
func Language(cfg *config.Config, override string) string {
	if override != "" && override != "auto" {
		return i18n.Normalize(override)
	}
	return i18n.Normalize(cfg.Language)
}

// Options for Execute.
type Options struct {
	verify.RunOptions
	Progress func(verify.Event)
	Logf     func(level, format string, args ...any)
	Secrets  secret.Store
	Backend  verify.Backend // tests
	Lang     string         // language for the extra report formats written after the run
}

// Result of Execute.
type Result struct {
	Run *verify.Run
	// Saved lists the report files written.
	Saved []string
	// SaveErr is set when the run completed but its record could not be stored.
	SaveErr error
}

// Execute verifies the selected targets and stores the result. Errors returned
// here are run-level problems (configuration, missing restic, unwritable report
// directory); individual target failures are part of Result.Run.
func Execute(ctx context.Context, cfg *config.Config, o Options) (*Result, error) {
	if issues := cfg.ValidateStatic(cfg.BaseDir()); config.HasErrors(issues) {
		return nil, &ConfigError{Issues: issues}
	}
	paths := cfg.Resolved()
	store := report.NewStore(paths.ReportDir)
	// Fail before doing any work if the result could not be saved.
	if err := store.Check(); err != nil {
		return nil, fmt.Errorf("report directory %s is not usable: %w", paths.ReportDir, err)
	}
	eng, err := verify.New(cfg, verify.Options{Backend: o.Backend, Secrets: o.Secrets, Progress: o.Progress, Logf: o.Logf})
	if err != nil {
		return nil, err
	}
	run, err := eng.Run(ctx, o.RunOptions)
	if err != nil {
		return nil, err
	}
	res := &Result{Run: run}
	lang := o.Lang
	if lang == "" {
		lang = Language(cfg, "")
	}
	saved, serr := store.Save(run, cfg.Reports.Formats, lang)
	res.Saved, res.SaveErr = saved.Files, serr
	return res, nil
}

// ValidateResult is the outcome of "bharbor validate".
type ValidateResult struct {
	ConfigPath    string                `json:"config_path"`
	Issues        []config.Issue        `json:"issues"`
	ResticVersion string                `json:"restic_version,omitempty"`
	Targets       []verify.TargetResult `json:"targets"`
	OK            bool                  `json:"ok"`
}

// Validate checks the configuration and, for the selected targets, every
// precondition of a run (restic, directories, password source, repository
// access, snapshot selection) without restoring anything.
func Validate(ctx context.Context, cfg *config.Config, targets []string, o Options) (*ValidateResult, error) {
	res := &ValidateResult{ConfigPath: cfg.Path, Issues: cfg.Validate(), Targets: []verify.TargetResult{}}
	if config.HasErrors(res.Issues) {
		return res, nil
	}
	selected, err := verify.SelectTargets(cfg, targets)
	if err != nil {
		if errors.Is(err, verify.ErrNoTargets) {
			res.Issues = append(res.Issues, config.Issue{Severity: config.SevError, Msg: i18n.M("cfg.no_enabled_targets")})
			return res, nil
		}
		return nil, err
	}
	eng, err := verify.New(cfg, verify.Options{Backend: o.Backend, Secrets: o.Secrets, Progress: o.Progress, Logf: o.Logf})
	if err != nil {
		return nil, err
	}
	id := verify.NewRunID()
	for _, t := range selected {
		res.Targets = append(res.Targets, eng.Preflight(ctx, t, id))
	}
	res.ResticVersion = eng.ResticVersion()
	res.OK = true
	for _, t := range res.Targets {
		if t.Status != verify.Passed {
			res.OK = false
		}
	}
	return res, nil
}

// PreflightTarget checks a single (possibly unsaved) target against cfg.
func PreflightTarget(ctx context.Context, cfg *config.Config, t config.Target, o Options) (verify.TargetResult, error) {
	eng, err := verify.New(cfg, verify.Options{Backend: o.Backend, Secrets: o.Secrets, Progress: o.Progress, Logf: o.Logf})
	if err != nil {
		return verify.TargetResult{}, err
	}
	return eng.Preflight(ctx, t, verify.NewRunID()), nil
}

// ScheduleSpec builds the scheduled-task definition from the configuration.
// exe is the absolute path of bharbor.exe. The task time limit covers the sum of
// the enabled targets' timeouts (plus a margin) so a slow but healthy run is
// never cut off by the scheduler before BackupHarbor's own timeouts apply.
func ScheduleSpec(cfg *config.Config, exe string) (schedule.Spec, error) {
	cfgPath, err := filepath.Abs(cfg.Path)
	if err != nil {
		return schedule.Spec{}, err
	}
	var total time.Duration
	for _, t := range cfg.Targets {
		if t.IsEnabled() {
			total += cfg.TimeoutFor(t)
		}
	}
	return schedule.Spec{
		Frequency: cfg.Schedule.Frequency, Time: cfg.Schedule.Time, Day: cfg.Schedule.Day,
		ExePath: exe, ConfigPath: cfgPath, TimeLimit: min(total+30*time.Minute, 72*time.Hour),
	}, nil
}
