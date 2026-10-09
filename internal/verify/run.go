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
	"backupharbor/internal/workspace"
)

// RunOptions select what a run covers.
type RunOptions struct {
	// Targets limits the run to these target names. Empty means every enabled
	// target; naming a disabled target runs it explicitly.
	Targets []string
	Trigger string
	// RunID may be preset (the GUI needs it before the run starts).
	RunID string
}

// ErrNoTargets is returned when there is nothing to verify.
var ErrNoTargets = errors.New("no enabled targets to verify")

// UnknownTargetError names a --target that does not exist.
type UnknownTargetError struct{ Name string }

func (e UnknownTargetError) Error() string { return fmt.Sprintf("unknown target %q", e.Name) }

// NewRunID returns a sortable unique run identifier.
func NewRunID() string {
	return time.Now().UTC().Format("20060102T150405Z") + "-" + fsx.RandomHex(3)
}

// SelectTargets resolves the targets a run covers.
func SelectTargets(cfg *config.Config, names []string) ([]config.Target, error) {
	var out []config.Target
	if len(names) == 0 {
		for _, t := range cfg.Targets {
			if t.IsEnabled() {
				out = append(out, t)
			}
		}
	} else {
		seen := map[string]bool{}
		for _, n := range names {
			t, ok := cfg.FindTarget(n)
			if !ok {
				return nil, UnknownTargetError{n}
			}
			if !seen[strings.ToLower(t.Name)] {
				seen[strings.ToLower(t.Name)] = true
				out = append(out, t)
			}
		}
	}
	if len(out) == 0 {
		return nil, ErrNoTargets
	}
	return out, nil
}

// staleAge is how old an abandoned workspace must be before it is removed.
const staleAge = 48 * time.Hour

// Run verifies the selected targets (in parallel up to the configured
// concurrency) and returns the complete run record. One target's failure never
// hides another's result.
func (e *Engine) Run(ctx context.Context, ro RunOptions) (*Run, error) {
	targets, err := SelectTargets(e.cfg, ro.Targets)
	if err != nil {
		return nil, err
	}
	id := ro.RunID
	if id == "" {
		id = NewRunID()
	}
	trigger := ro.Trigger
	if trigger == "" {
		trigger = TriggerCLI
	}
	run := NewRun(id, trigger, time.Now())
	e.emit(Event{Type: "run_start", RunID: id, Percent: -1, Message: i18n.M("run.started", len(targets))})

	if n, err := workspace.CleanStale(e.paths.TempDir, staleAge); err != nil {
		e.logf("warn", "stale workspace cleanup: %v", err)
	} else if n > 0 {
		e.logf("info", "removed %d abandoned workspace(s)", n)
	}

	results := make([]TargetResult, len(targets))
	sem := make(chan struct{}, max(1, e.cfg.Concurrency))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					now := time.Now().UTC()
					results[i] = TargetResult{Name: t.Name, Status: Failed, StartedAt: now, FinishedAt: now,
						Stages: []Stage{{ID: "internal", Status: Failed, StartedAt: now,
							Message: i18n.M("run.internal_error", fmt.Sprint(r))}}}
					e.logf("error", "%s: internal error: %v", t.Name, r)
				}
			}()
			results[i] = e.VerifyTarget(ctx, t, id)
		}()
	}
	wg.Wait()

	run.Targets = results
	run.ResticVersion = e.ResticVersion()
	run.Finish(time.Now())
	e.emit(Event{Type: "run_end", RunID: id, Status: run.Status, Percent: -1})
	return run, nil
}
