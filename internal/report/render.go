package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"backupharbor/internal/i18n"
	"backupharbor/internal/verify"
)

// helpers shared by the text and HTML renderers

func statusLabel(lang string, s verify.Status) string { return i18n.T(lang, "status."+string(s)) }

func stageLabel(lang, id string) string { return i18n.T(lang, "stage."+id) }

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04:05 MST")
}

func fmtDurMS(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Second:
		return fmt.Sprintf("%d ms", ms)
	case d < time.Minute:
		return fmt.Sprintf("%.1f s", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

// FormatBytes renders a byte count with binary units.
func FormatBytes(n int64) string {
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

// FormatDuration renders a duration given in milliseconds.
func FormatDuration(ms int64) string { return fmtDurMS(ms) }

func evidenceLines(e map[string]any) []string {
	keys := make([]string, 0, len(e))
	for k := range e {
		if k == "errors" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%v", k, e[k]))
	}
	return out
}

// RenderTXT writes a plain-text report.
func RenderTXT(w io.Writer, r *verify.Run, lang string) error {
	t := func(k string, a ...any) string { return i18n.T(lang, k, a...) }
	var err error
	p := func(format string, a ...any) {
		if err == nil {
			_, err = fmt.Fprintf(w, format, a...)
		}
	}
	line := strings.Repeat("=", 72)
	p("%s\n%s\n%s\n", line, t("rpt.title"), line)
	if r.Demo {
		p("%s\n", t("rpt.demo_banner"))
	}
	p("%-22s %s\n", t("rpt.run_id")+":", r.RunID)
	p("%-22s %s\n", t("rpt.overall")+":", i18n.Upper(lang, statusLabel(lang, r.Status)))
	p("%-22s %s\n", t("rpt.started")+":", fmtTime(r.StartedAt))
	p("%-22s %s\n", t("rpt.finished")+":", fmtTime(r.FinishedAt))
	p("%-22s %s\n", t("rpt.duration")+":", fmtDurMS(r.DurationMS))
	p("%-22s %s\n", t("rpt.trigger")+":", t("trigger."+r.Trigger))
	p("%-22s %s\n", t("rpt.app_version")+":", r.AppVersion)
	if r.ResticVersion != "" {
		p("%-22s %s\n", t("rpt.restic_version")+":", r.ResticVersion)
	}
	c := r.Summary
	p("%-22s %s\n\n", t("rpt.targets")+":", t("rpt.counts", c.Total, c.Passed, c.Failed, c.Partial, c.NotRun, c.Cancelled+c.TimedOut))

	for _, tg := range r.Targets {
		p("%s\n%s: %s  [%s]\n%s\n", strings.Repeat("-", 72), t("rpt.target"), tg.Name, i18n.Upper(lang, statusLabel(lang, tg.Status)), strings.Repeat("-", 72))
		p("%-22s %s\n", t("rpt.repository")+":", tg.Repository)
		if s := tg.Snapshot; s != nil {
			p("%-22s %s (%s)\n", t("rpt.snapshot")+":", s.ShortID, s.ID)
			p("%-22s %s\n", t("rpt.snapshot_time")+":", fmtTime(s.Time))
			if s.Hostname != "" {
				p("%-22s %s\n", t("rpt.snapshot_host")+":", s.Hostname)
			}
			if len(s.Paths) > 0 {
				p("%-22s %s\n", t("rpt.snapshot_paths")+":", strings.Join(s.Paths, "; "))
			}
			if len(s.Tags) > 0 {
				p("%-22s %s\n", t("rpt.snapshot_tags")+":", strings.Join(s.Tags, ", "))
			}
		}
		if rs := tg.Restore; rs != nil {
			p("%-22s %s\n", t("rpt.restore_time")+":", fmtDurMS(rs.DurationMS))
			p("%-22s %d / %d\n", t("rpt.files_dirs")+":", rs.Files, rs.Directories)
			p("%-22s %s\n", t("rpt.size")+":", FormatBytes(rs.Bytes))
			if rs.MetadataWarnings > 0 {
				p("%-22s %d\n", t("rpt.metadata_warnings")+":", rs.MetadataWarnings)
			}
		}
		p("\n")
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t("rpt.stage"), t("rpt.status"), t("rpt.time"), t("rpt.result"))
		for _, st := range tg.Stages {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", stageLabel(lang, st.ID), statusLabel(lang, st.Status), fmtDurMS(st.DurationMS), st.Message.In(lang))
		}
		if e := tw.Flush(); err == nil {
			err = e
		}
		for _, st := range tg.Stages {
			if len(st.Rules) == 0 {
				continue
			}
			p("\n%s\n", t("rpt.rules"))
			for _, ru := range st.Rules {
				name := ru.Name
				if name == "" {
					name = ru.Pattern
				}
				p("  [%s] %s (%s %s): %s\n", statusLabel(lang, ru.Status), name, ru.Type, ru.Pattern, ru.Message.In(lang))
				for _, f := range ru.Failures {
					p("      - %s: %s\n", f.Path, f.Msg.In(lang))
				}
			}
		}
		for _, st := range tg.Stages {
			if st.Detail != "" && (st.Status == verify.Failed || st.Status == verify.NotRun || st.Status == verify.Partial) {
				p("\n%s (%s):\n", t("rpt.details"), stageLabel(lang, st.ID))
				for _, l := range strings.Split(st.Detail, "\n") {
					p("    %s\n", l)
				}
			}
		}
		if len(tg.Coverage) > 0 {
			p("\n%s\n", t("rpt.coverage"))
			for _, cv := range tg.Coverage {
				p("  - %s\n", cv.In(lang))
			}
		}
		p("\n")
	}
	p("%s\n", t("rpt.disclaimer"))
	return err
}
