package cli

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"backupharbor/internal/app"
	"backupharbor/internal/report"
	"backupharbor/internal/verify"
)

func (x *ctx) cmdRun(cx context.Context, args []string) int {
	fs, c := x.sub("run")
	var targets stringList
	fs.Var(&targets, "target", "")
	trigger := fs.String("trigger", verify.TriggerCLI, "")
	if code, ok := x.parse(fs, c, args); !ok {
		return code
	}
	if *trigger != verify.TriggerCLI && *trigger != verify.TriggerSchedule && *trigger != verify.TriggerGUI {
		return x.fail(ExitError, x.t("cli.bad_flags", "--trigger"))
	}
	cfg, code := x.loadConfig()
	if cfg == nil {
		return code
	}

	live := !x.c.quiet && !x.c.json
	var mu sync.Mutex
	lastProgress := time.Time{}
	tty := x.io.IsTerminal != nil && x.io.IsTerminal(x.io.Out)
	progress := func(ev verify.Event) {
		if !live {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch ev.Type {
		case "run_start":
			x.out("%s\n", x.t("cli.run_started", ev.RunID))
		case "stage_end":
			if ev.Status == verify.Skipped {
				return
			}
			if tty {
				x.out("%c%s%c", 13, strings.Repeat(" ", 110), 13)
			}
			x.out("  %-16s %-14s %s  %s\n", ev.Target, x.t("stage."+ev.Stage), x.statusText(ev.Status), ev.Message.In(x.lang))
		case "progress":
			if !tty || time.Since(lastProgress) < time.Second {
				return
			}
			lastProgress = time.Now()
			if ev.Percent >= 0 && ev.BytesTotal > 0 {
				x.out("\r  %-16s %s %.0f%%  %-40s", ev.Target, x.t("stage."+ev.Stage), ev.Percent,
					report.FormatBytes(int64(ev.BytesDone))+" / "+report.FormatBytes(int64(ev.BytesTotal)))
			} else if ev.Message.Code != "" {
				x.out("\r  %-16s %-90s", ev.Target, ev.Message.In(x.lang))
			}
		}
	}

	res, err := app.Execute(cx, cfg, app.Options{
		RunOptions: verify.RunOptions{Targets: targets, Trigger: *trigger},
		Progress:   progress, Logf: x.logf, Secrets: x.io.Secrets, Backend: x.io.Backend, Lang: x.lang,
	})
	if err != nil {
		return x.errorExit(err)
	}
	run := res.Run
	exit := run.ExitCode()

	if x.c.json {
		x.jsonOut(run)
	} else if !x.c.quiet {
		x.printRunSummary(run)
	}
	if res.SaveErr != nil {
		x.errf("%s", x.t("cli.report_save_failed", res.SaveErr.Error()))
		if exit == ExitOK {
			exit = ExitError
		}
	} else if !x.c.json && !x.c.quiet {
		for _, f := range res.Saved {
			x.out("%s %s\n", x.t("cli.report_saved"), f)
		}
	}
	return exit
}

func (x *ctx) printRunSummary(run *verify.Run) {
	x.out("\n")
	tw := tabwriter.NewWriter(x.io.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", x.t("rpt.target"), x.t("rpt.status"), x.t("rpt.snapshot"),
		x.t("rpt.restore_time"), x.t("rpt.files"), x.t("rpt.size"))
	for _, t := range run.Targets {
		snap, rt, files, size := "-", "-", "-", "-"
		if t.Snapshot != nil {
			snap = t.Snapshot.ShortID
		}
		if t.Restore != nil {
			rt, files, size = report.FormatDuration(t.Restore.DurationMS), fmt.Sprint(t.Restore.Files), report.FormatBytes(t.Restore.Bytes)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", t.Name, x.statusText(t.Status), snap, rt, files, size)
	}
	tw.Flush()

	for _, t := range run.Targets {
		if t.Status == verify.Passed {
			continue
		}
		x.out("\n%s %s\n", x.t("cli.problems_in"), t.Name)
		for _, st := range t.Stages {
			if st.Status == verify.Passed || st.Status == verify.Skipped {
				continue
			}
			x.out("  - %s: %s %s\n", x.t("stage."+st.ID), x.statusText(st.Status), st.Message.In(x.lang))
			if st.Detail != "" {
				x.out("      %s\n", firstLine(st.Detail))
			}
			for _, r := range st.Rules {
				if r.Status == verify.Failed || r.Status == verify.Partial {
					x.out("      %s [%s]: %s\n", ruleName(r), r.Type, r.Message.In(x.lang))
					for i, f := range r.Failures {
						if i >= 5 {
							x.out("        ... %s\n", x.t("cli.more_in_report"))
							break
						}
						x.out("        %s: %s\n", f.Path, f.Msg.In(x.lang))
					}
				}
			}
		}
	}
	x.out("\n%s %s (%s)\n", x.t("rpt.overall")+":", x.statusText(run.Status), report.FormatDuration(run.DurationMS))
	if run.Status == verify.Passed {
		x.out("%s\n", strings.TrimSpace(x.t("cli.passed_notice")))
	}
}

func ruleName(r verify.RuleResult) string {
	if r.Name != "" {
		return r.Name
	}
	return r.Pattern
}
