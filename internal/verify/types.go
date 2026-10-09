// Package verify is the BackupHarbor verification engine. It proves (or fails to
// prove) that a backup can be restored by running real restic commands in a
// private workspace. The CLI, the GUI and the scheduled task all call this
// package; there is no second implementation.
package verify

import (
	"time"

	"backupharbor/internal/i18n"
	"backupharbor/internal/version"
)

// Status is the outcome of a stage, a target or a whole run.
type Status string

// Statuses. Only Passed means "proven"; every other value means the claim was
// not established.
const (
	Passed    Status = "passed"    // the check ran and succeeded
	Failed    Status = "failed"    // the check ran and found a problem
	NotRun    Status = "not_run"   // the check could not be performed
	Cancelled Status = "cancelled" // the user or system cancelled the operation
	TimedOut  Status = "timed_out" // the time limit was reached
	Partial   Status = "partial"   // some of it succeeded, some did not
	Skipped   Status = "skipped"   // disabled by configuration; proves nothing
)

// Stage identifiers, in execution order.
const (
	StageRestic     = "restic"
	StageDirs       = "directories"
	StagePassword   = "password"
	StageRepository = "repository"
	StageSnapshot   = "snapshot"
	StageCheck      = "check"
	StageRestore    = "restore"
	StageContent    = "content"
	StageCleanup    = "cleanup"
)

// AllStages lists stage IDs in execution order.
var AllStages = []string{StageRestic, StageDirs, StagePassword, StageRepository, StageSnapshot,
	StageCheck, StageRestore, StageContent, StageCleanup}

// Trigger values.
const (
	TriggerCLI      = "cli"
	TriggerGUI      = "gui"
	TriggerSchedule = "schedule"
)

// FileFailure is one file that failed a content rule.
type FileFailure struct {
	Path string   `json:"path"`
	Msg  i18n.Msg `json:"message"`
}

// RuleResult summarises one content rule.
type RuleResult struct {
	Name       string        `json:"name"`
	Type       string        `json:"type"`
	Pattern    string        `json:"pattern"`
	Status     Status        `json:"status"`
	Matched    int           `json:"matched"`
	Checked    int           `json:"checked"`
	Passed     int           `json:"passed"`
	Failed     int           `json:"failed"`
	DurationMS int64         `json:"duration_ms"`
	Message    i18n.Msg      `json:"message"`
	Failures   []FileFailure `json:"failures,omitempty"`
}

// Stage is the recorded result of one verification stage.
type Stage struct {
	ID         string         `json:"id"`
	Status     Status         `json:"status"`
	StartedAt  time.Time      `json:"started_at"`
	DurationMS int64          `json:"duration_ms"`
	Message    i18n.Msg       `json:"message"`
	Detail     string         `json:"detail,omitempty"` // redacted technical output
	Evidence   map[string]any `json:"evidence,omitempty"`
	Rules      []RuleResult   `json:"rules,omitempty"`
}

// SnapshotInfo is the metadata of the verified snapshot.
type SnapshotInfo struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname,omitempty"`
	Username string    `json:"username,omitempty"`
	Paths    []string  `json:"paths,omitempty"`
	Tags     []string  `json:"tags,omitempty"`
}

// RestoreInfo summarises the real restore.
type RestoreInfo struct {
	DurationMS       int64  `json:"duration_ms"`
	Files            int64  `json:"files"`
	Directories      int64  `json:"directories"`
	Bytes            int64  `json:"bytes"`
	Links            int64  `json:"links_skipped"`
	MetadataWarnings int    `json:"metadata_warnings"`
	ContentErrors    int    `json:"content_errors"`
	Verified         bool   `json:"verified"`
	BytesPerSecond   uint64 `json:"bytes_per_second,omitempty"`
}

// TargetResult is the result of verifying one target.
type TargetResult struct {
	Name       string        `json:"name"`
	Status     Status        `json:"status"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
	DurationMS int64         `json:"duration_ms"`
	Repository string        `json:"repository"` // credentials removed
	Snapshot   *SnapshotInfo `json:"snapshot,omitempty"`
	Restore    *RestoreInfo  `json:"restore,omitempty"`
	Stages     []Stage       `json:"stages"`
	// Coverage states in plain words what was and was not proven.
	Coverage []i18n.Msg `json:"coverage,omitempty"`
}

// Stage returns the stage with the given ID, or nil.
func (t *TargetResult) Stage(id string) *Stage {
	for i := range t.Stages {
		if t.Stages[i].ID == id {
			return &t.Stages[i]
		}
	}
	return nil
}

// Counts tallies targets by status.
type Counts struct {
	Total     int `json:"total"`
	Passed    int `json:"passed"`
	Failed    int `json:"failed"`
	NotRun    int `json:"not_run"`
	Cancelled int `json:"cancelled"`
	TimedOut  int `json:"timed_out"`
	Partial   int `json:"partial"`
}

// Run is one complete verification run over one or more targets. It is the
// unit stored in the report history.
type Run struct {
	Schema        string         `json:"schema"`
	RunID         string         `json:"run_id"`
	AppVersion    string         `json:"app_version"`
	ResticVersion string         `json:"restic_version,omitempty"`
	Trigger       string         `json:"trigger"`
	Demo          bool           `json:"demo"`
	StartedAt     time.Time      `json:"started_at"`
	FinishedAt    time.Time      `json:"finished_at"`
	DurationMS    int64          `json:"duration_ms"`
	Status        Status         `json:"status"`
	Summary       Counts         `json:"summary"`
	Targets       []TargetResult `json:"targets"`
}

// NewRun creates an empty run with identity fields set.
func NewRun(id, trigger string, start time.Time) *Run {
	return &Run{Schema: version.ReportSchema, RunID: id, AppVersion: version.Version, Trigger: trigger,
		StartedAt: start.UTC(), Targets: []TargetResult{}}
}

// Finish computes the aggregate fields.
func (r *Run) Finish(end time.Time) {
	r.FinishedAt = end.UTC()
	r.DurationMS = r.FinishedAt.Sub(r.StartedAt).Milliseconds()
	r.Summary = Counts{Total: len(r.Targets)}
	statuses := make([]Status, 0, len(r.Targets))
	for _, t := range r.Targets {
		statuses = append(statuses, t.Status)
		switch t.Status {
		case Passed:
			r.Summary.Passed++
		case Failed:
			r.Summary.Failed++
		case NotRun:
			r.Summary.NotRun++
		case Cancelled:
			r.Summary.Cancelled++
		case TimedOut:
			r.Summary.TimedOut++
		case Partial:
			r.Summary.Partial++
		}
	}
	r.Status = Worst(statuses...)
}

// Process exit codes shared by every bharbor command.
const (
	ExitOK        = 0 // everything requested succeeded
	ExitFailed    = 1 // verification ran and at least one target failed or was only partly proven
	ExitError     = 2 // configuration, dependency or execution error; or a target could not be tested
	ExitCancelled = 3 // cancelled or timed out
)

// ExitCode maps a run to a process exit code. Precedence: interrupted (3) >
// proven problem (1) > could not test (2) > success (0).
func (r *Run) ExitCode() int {
	if r.Summary.Cancelled > 0 || r.Summary.TimedOut > 0 {
		return ExitCancelled
	}
	if r.Summary.Failed > 0 || r.Summary.Partial > 0 {
		return ExitFailed
	}
	if r.Summary.NotRun > 0 || r.Summary.Total == 0 {
		return ExitError
	}
	return ExitOK
}

// Worst returns the most severe of the statuses. Skipped values are ignored;
// with nothing left the result is Passed only if at least one status was given
// and none was Skipped-only... an empty set yields NotRun (nothing was proven).
func Worst(statuses ...Status) Status {
	rank := map[Status]int{Passed: 1, NotRun: 2, Partial: 3, Failed: 4, TimedOut: 5, Cancelled: 6}
	best, bestRank := NotRun, 0
	for _, s := range statuses {
		if r, ok := rank[s]; ok && r > bestRank {
			best, bestRank = s, r
		}
	}
	if bestRank == 0 {
		return NotRun
	}
	return best
}

// Event is a progress notification for UIs.
type Event struct {
	Type    string    `json:"type"` // run_start, target_start, stage_start, stage_end, progress, target_end, run_end, log
	RunID   string    `json:"run_id"`
	Target  string    `json:"target,omitempty"`
	Stage   string    `json:"stage,omitempty"`
	Status  Status    `json:"status,omitempty"`
	Message i18n.Msg  `json:"message,omitempty"`
	Time    time.Time `json:"time"`
	// Progress fields; Percent is -1 when restic reported no ratio.
	Percent    float64 `json:"percent"`
	FilesDone  uint64  `json:"files_done,omitempty"`
	FilesTotal uint64  `json:"files_total,omitempty"`
	BytesDone  uint64  `json:"bytes_done,omitempty"`
	BytesTotal uint64  `json:"bytes_total,omitempty"`
	// Level and Line carry log lines (type "log").
	Level string `json:"level,omitempty"`
	Line  string `json:"line,omitempty"`
}
