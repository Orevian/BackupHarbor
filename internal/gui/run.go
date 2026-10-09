package gui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"backupharbor/internal/app"
	"backupharbor/internal/demo"
	"backupharbor/internal/i18n"
	"backupharbor/internal/report"
	"backupharbor/internal/verify"
)

// Event names delivered to the front end.
const (
	EventProgress = "verify:event"
	EventLog      = "verify:log"
	EventDone     = "verify:done"
)

type runState struct {
	id        string
	demo      bool
	cancel    context.CancelFunc
	started   time.Time
	done      chan struct{}
	events    []verify.Event
	targets   []string
	cancelled bool
}

// RunState is the snapshot of the current (or last) run for UI recovery.
type RunState struct {
	Running   bool           `json:"running"`
	RunID     string         `json:"run_id"`
	Demo      bool           `json:"demo"`
	StartedAt time.Time      `json:"started_at"`
	Targets   []string       `json:"targets"`
	Events    []verify.Event `json:"events"`
	Log       []LogLine      `json:"log"`
}

// DoneEvent is delivered when a run ends.
type DoneEvent struct {
	Run       *verify.Run `json:"run"`
	Saved     []string    `json:"saved"`
	SaveError string      `json:"save_error,omitempty"`
	Error     string      `json:"error,omitempty"`
	ExitCode  int         `json:"exit_code"`
	Demo      bool        `json:"demo"`
}

func (a *App) appendLog(level, line string) {
	a.mu.Lock()
	a.log = append(a.log, LogLine{Time: time.Now(), Level: level, Line: line})
	if len(a.log) > maxLogLines {
		a.log = a.log[len(a.log)-maxLogLines:]
	}
	a.mu.Unlock()
	a.emit(EventLog, LogLine{Time: time.Now(), Level: level, Line: line})
}

// IsRunning reports whether a verification is in progress.
func (a *App) IsRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.run != nil && !isClosed(a.run.done)
}

func isClosed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// IsDemo reports whether demonstration mode is on.
func (a *App) IsDemo() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.demo
}

// SetDemo switches demonstration mode. Demo data is never mixed with real results.
func (a *App) SetDemo(on bool) error {
	if a.IsRunning() {
		return errors.New(i18n.T(a.currentLang(), "gui.busy"))
	}
	a.mu.Lock()
	a.demo = on
	a.mu.Unlock()
	return nil
}

// GetRunState returns the state of the current or most recent run.
func (a *App) GetRunState() RunState {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := RunState{Log: append([]LogLine{}, a.log...), Events: []verify.Event{}, Targets: []string{}}
	if a.run != nil {
		st.Running = !isClosed(a.run.done)
		st.RunID, st.Demo, st.StartedAt = a.run.id, a.run.demo, a.run.started
		st.Events = append(st.Events, a.run.events...)
		st.Targets = append(st.Targets, a.run.targets...)
	}
	return st
}

// StartRun begins verifying the named targets (all enabled targets if empty).
// It returns immediately; progress arrives as events.
func (a *App) StartRun(targets []string) (string, error) {
	a.mu.Lock()
	if a.run != nil && !isClosed(a.run.done) {
		a.mu.Unlock()
		return "", errors.New(i18n.T(a.currentLang(), "gui.busy"))
	}
	isDemo := a.demo
	a.mu.Unlock()

	cfg, _, err := a.loadConfig()
	if err != nil {
		return "", err
	}
	id := verify.NewRunID()
	if isDemo {
		id = "DEMO-" + id
	}
	ctx, cancel := context.WithCancel(context.Background())
	rs := &runState{id: id, demo: isDemo, cancel: cancel, started: time.Now(), done: make(chan struct{})}

	if !isDemo {
		sel, serr := verify.SelectTargets(cfg, targets)
		if serr != nil {
			cancel()
			return "", a.explain(serr)
		}
		for _, t := range sel {
			rs.targets = append(rs.targets, t.Name)
		}
	} else {
		rs.targets = []string{"documents-daily"}
	}

	a.mu.Lock()
	a.run = rs
	a.log = nil
	a.mu.Unlock()

	if a.logger == nil {
		a.logger = app.NewLogger(cfg.LogLevel)
		a.logger.AddSink(func(level, line string) { a.appendLog(level, line) })
	}
	progress := func(ev verify.Event) {
		a.mu.Lock()
		// Keep the replay buffer small: drop intermediate progress samples.
		if ev.Type == "progress" && len(rs.events) > 0 && rs.events[len(rs.events)-1].Type == "progress" &&
			rs.events[len(rs.events)-1].Target == ev.Target && rs.events[len(rs.events)-1].Stage == ev.Stage {
			rs.events[len(rs.events)-1] = ev
		} else if len(rs.events) < 2000 {
			rs.events = append(rs.events, ev)
		}
		a.mu.Unlock()
		a.emit(EventProgress, ev)
	}

	go func() {
		defer close(rs.done)
		defer cancel()
		var done DoneEvent
		if isDemo {
			done = a.simulate(ctx, rs, progress)
		} else {
			done = a.runReal(ctx, rs, targets, progress)
		}
		a.emit(EventDone, done)
	}()
	return id, nil
}

func (a *App) explain(err error) error {
	l := a.currentLang()
	var u verify.UnknownTargetError
	switch {
	case errors.As(err, &u):
		return fmt.Errorf("%s", i18n.T(l, "cli.unknown_target", u.Name))
	case errors.Is(err, verify.ErrNoTargets):
		return fmt.Errorf("%s", i18n.T(l, "cli.no_targets"))
	}
	return err
}

func (a *App) runReal(ctx context.Context, rs *runState, targets []string, progress func(verify.Event)) DoneEvent {
	conf, _, err := a.loadConfig()
	if err != nil {
		return DoneEvent{Error: err.Error(), ExitCode: verify.ExitError}
	}
	res, err := app.Execute(ctx, conf, app.Options{
		RunOptions: verify.RunOptions{Targets: targets, Trigger: verify.TriggerGUI, RunID: rs.id},
		Progress:   progress, Logf: a.log_, Secrets: a.Secrets, Backend: a.Backend, Lang: a.lang(conf),
	})
	if err != nil {
		l := a.lang(conf)
		var ce *app.ConfigError
		msg := err.Error()
		if errors.As(err, &ce) && len(ce.Issues) > 0 {
			msg = ce.Issues[0].Text(l)
		}
		var dep verify.ErrDependency
		if errors.As(err, &dep) {
			msg = i18n.T(l, "cli.restic_missing", err.Error())
		}
		return DoneEvent{Error: msg, ExitCode: verify.ExitError}
	}
	d := DoneEvent{Run: res.Run, Saved: res.Saved, ExitCode: res.Run.ExitCode()}
	if res.SaveErr != nil {
		d.SaveError = res.SaveErr.Error()
		if d.ExitCode == verify.ExitOK {
			d.ExitCode = verify.ExitError
		}
	}
	return d
}

// simulate plays back a scripted demonstration run without touching any repository.
func (a *App) simulate(ctx context.Context, rs *runState, progress func(verify.Event)) DoneEvent {
	const target = "documents-daily"
	progress(verify.Event{Type: "run_start", RunID: rs.id, Percent: -1, Message: i18n.M("run.started", 1)})
	progress(verify.Event{Type: "target_start", RunID: rs.id, Target: target, Percent: -1})
	started := map[string]bool{}
	for _, s := range demo.Script() {
		select {
		case <-ctx.Done():
			progress(verify.Event{Type: "target_end", RunID: rs.id, Target: target, Status: verify.Cancelled, Percent: -1})
			return DoneEvent{Demo: true, ExitCode: verify.ExitCancelled}
		case <-time.After(s.Delay):
		}
		if !started[s.Stage] {
			started[s.Stage] = true
			progress(verify.Event{Type: "stage_start", RunID: rs.id, Target: target, Stage: s.Stage, Percent: -1})
		}
		if s.Status == "" {
			progress(verify.Event{Type: "progress", RunID: rs.id, Target: target, Stage: s.Stage, Percent: s.Percent,
				BytesDone: uint64(s.Percent / 100 * 1395864371), BytesTotal: 1395864371,
				FilesDone: uint64(s.Percent / 100 * 4182), FilesTotal: 4182})
			continue
		}
		progress(verify.Event{Type: "stage_end", RunID: rs.id, Target: target, Stage: s.Stage, Status: s.Status, Message: s.Message, Percent: -1})
	}
	progress(verify.Event{Type: "target_end", RunID: rs.id, Target: target, Status: verify.Passed, Percent: -1})
	run, _ := report.FilterTarget(demo.Latest(time.Now()), target)
	run.RunID = rs.id
	progress(verify.Event{Type: "run_end", RunID: rs.id, Status: run.Status, Percent: -1})
	return DoneEvent{Run: run, Demo: true, ExitCode: run.ExitCode()}
}

// CancelRun asks the running verification to stop. Cleanup still completes.
func (a *App) CancelRun() {
	a.mu.Lock()
	r := a.run
	if r != nil && !isClosed(r.done) {
		r.cancelled = true
		r.cancel()
	}
	a.mu.Unlock()
}
