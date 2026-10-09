package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"backupharbor/internal/report"
	"backupharbor/internal/verify"
)

func parseDate(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q (use YYYY-MM-DD)", s)
}

func (x *ctx) cmdReport(args []string) int {
	fs, c := x.sub("report")
	last := fs.Bool("last", false, "")
	list := fs.Bool("list", false, "")
	runID := fs.String("run", "", "")
	target := fs.String("target", "", "")
	format := fs.String("format", "", "")
	output := fs.String("output", "", "")
	force := fs.Bool("force", false, "")
	since := fs.String("since", "", "")
	until := fs.String("until", "", "")
	limit := fs.Int("limit", 20, "")
	if code, ok := x.parse(fs, c, args); !ok {
		return code
	}
	cfg, code := x.loadConfig()
	if cfg == nil {
		return code
	}
	store := report.NewStore(cfg.Resolved().ReportDir)

	if *list {
		return x.reportList(store, *target, *since, *until, *limit)
	}

	fmtName := strings.ToLower(*format)
	if x.c.json && fmtName == "" {
		fmtName = report.FormatJSON
	}
	if fmtName == "" {
		fmtName = report.FormatTXT
	}
	if fmtName != report.FormatJSON && fmtName != report.FormatHTML && fmtName != report.FormatTXT {
		return x.fail(ExitError, x.t("cli.report_bad_format", *format))
	}
	_ = last // --last is the default selection

	var run *verify.Run
	var err error
	if *runID != "" {
		run, err = store.Load(*runID)
	} else {
		run, err = store.Last(*target)
	}
	if err != nil {
		if errors.Is(err, report.ErrNotFound) {
			return x.fail(ExitError, x.t("cli.report_none"))
		}
		return x.fail(ExitError, x.t("cli.report_load_failed", err.Error()))
	}
	if *target != "" {
		full := run
		run, err = report.FilterTarget(full, *target)
		if err != nil {
			return x.fail(ExitError, x.t("cli.report_target_missing", *target, full.RunID))
		}
	}
	if *output != "" {
		path, aerr := filepath.Abs(*output)
		if aerr != nil {
			return x.fail(ExitError, aerr.Error())
		}
		if err := report.Export(path, fmtName, run, x.lang, *force); err != nil {
			if errors.Is(err, report.ErrExists) {
				return x.fail(ExitError, x.t("cli.report_exists", path))
			}
			return x.fail(ExitError, x.t("cli.report_export_failed", err.Error()))
		}
		if x.c.json {
			x.jsonOut(map[string]string{"path": path, "format": fmtName, "run_id": run.RunID})
		} else {
			x.out("%s %s\n", x.t("cli.report_written"), path)
		}
		return ExitOK
	}
	if err := report.Render(x.io.Out, fmtName, run, x.lang); err != nil {
		return x.fail(ExitError, x.t("cli.report_export_failed", err.Error()))
	}
	return ExitOK
}

func (x *ctx) reportList(store *report.Store, target, since, until string, limit int) int {
	f := report.Filter{Target: target, Limit: limit}
	var err error
	if since != "" {
		if f.Since, err = parseDate(since); err != nil {
			return x.fail(ExitError, err.Error())
		}
	}
	if until != "" {
		if f.Until, err = parseDate(until); err != nil {
			return x.fail(ExitError, err.Error())
		}
	}
	items, err := store.List(f)
	if err != nil {
		return x.fail(ExitError, x.t("cli.report_load_failed", err.Error()))
	}
	if x.c.json {
		if items == nil {
			items = []report.Summary{}
		}
		x.jsonOut(items)
		return ExitOK
	}
	if len(items) == 0 {
		x.out("%s\n", x.t("cli.report_none"))
		return ExitOK
	}
	tw := tabwriter.NewWriter(x.io.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", x.t("rpt.run_id"), x.t("rpt.started"), x.t("rpt.status"), x.t("rpt.duration"), x.t("rpt.targets"))
	for _, it := range items {
		names := make([]string, 0, len(it.Targets))
		for _, t := range it.Targets {
			names = append(names, t.Name)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", it.RunID, it.StartedAt.Local().Format("2006-01-02 15:04"),
			x.statusText(it.Status), report.FormatDuration(it.DurationMS), strings.Join(names, ", "))
	}
	tw.Flush()
	return ExitOK
}
