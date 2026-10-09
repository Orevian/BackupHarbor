package report

import (
	"html/template"
	"io"
	"strings"

	"backupharbor/internal/i18n"
	"backupharbor/internal/verify"
)

// RenderHTML writes a single self-contained HTML document: inline CSS, no
// scripts, no external resources, print friendly. All dynamic values pass
// through html/template's contextual escaping.
func RenderHTML(w io.Writer, r *verify.Run, lang string) error {
	l := i18n.Normalize(lang)
	funcs := template.FuncMap{
		"t":      func(k string, a ...any) string { return i18n.T(l, k, a...) },
		"status": func(s verify.Status) string { return statusLabel(l, s) },
		"stage":  func(id string) string { return stageLabel(l, id) },
		"msg":    func(m i18n.Msg) string { return m.In(l) },
		"time":   fmtTime,
		"dur":    fmtDurMS,
		"bytes":  FormatBytes,
		"join":   strings.Join,
		"cls":    func(s verify.Status) string { return "st-" + strings.ReplaceAll(string(s), "_", "-") },
		"ev":     evidenceLines,
		"trig":   func(s string) string { return i18n.T(l, "trigger."+s) },
		"counts": func(c verify.Counts) string {
			return i18n.T(l, "rpt.counts", c.Total, c.Passed, c.Failed, c.Partial, c.NotRun, c.Cancelled+c.TimedOut)
		},
		"showDetail": func(s verify.Stage) bool {
			return s.Detail != "" && (s.Status == verify.Failed || s.Status == verify.NotRun || s.Status == verify.Partial)
		},
	}
	tpl, err := template.New("report").Funcs(funcs).Parse(htmlTemplate)
	if err != nil {
		return err
	}
	return tpl.Execute(w, struct {
		Run  *verify.Run
		Lang string
	}{r, l})
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{t "rpt.title"}} - {{.Run.RunID}}</title>
<style>
:root{--bg:#f6f7f9;--card:#fff;--fg:#1b2430;--muted:#5b6675;--line:#d9dee5;--ok:#0b7a3b;--okbg:#e3f4ea;--bad:#b3261e;--badbg:#fbe7e5;--warn:#8a5a00;--warnbg:#fdf1d8;--info:#33507a;--infobg:#e6edf7}
@media (prefers-color-scheme:dark){:root{--bg:#10151c;--card:#171e28;--fg:#e6ebf2;--muted:#9aa6b5;--line:#2b3644;--ok:#58d38a;--okbg:#12301f;--bad:#ff8a80;--badbg:#3a1a18;--warn:#f2c14e;--warnbg:#35290c;--info:#8fb2e8;--infobg:#1b2a42}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.5 "Segoe UI",system-ui,sans-serif}
main{max-width:980px;margin:0 auto;padding:24px 16px 48px}
h1{font-size:24px;margin:0 0 4px}h2{font-size:19px;margin:0}h3{font-size:15px;margin:20px 0 8px}
.muted{color:var(--muted)}
.card{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:16px 20px;margin:16px 0}
.head{display:flex;gap:12px;align-items:center;justify-content:space-between;flex-wrap:wrap}
.badge{display:inline-block;padding:2px 10px;border-radius:999px;font-weight:600;font-size:13px;border:1px solid currentColor}
.st-passed{color:var(--ok);background:var(--okbg)}
.st-failed{color:var(--bad);background:var(--badbg)}
.st-partial,.st-not-run{color:var(--warn);background:var(--warnbg)}
.st-cancelled,.st-timed-out,.st-skipped{color:var(--info);background:var(--infobg)}
dl{display:grid;grid-template-columns:max-content 1fr;gap:4px 16px;margin:12px 0}
dt{color:var(--muted)}dd{margin:0;word-break:break-word}
table{border-collapse:collapse;width:100%;margin:8px 0}
th,td{text-align:left;padding:6px 8px;border-bottom:1px solid var(--line);vertical-align:top}
th{font-size:13px;color:var(--muted);font-weight:600}
pre{background:var(--bg);border:1px solid var(--line);border-radius:6px;padding:10px;overflow:auto;white-space:pre-wrap;word-break:break-word;font-size:13px}
.demo{background:var(--warnbg);color:var(--warn);border:1px solid var(--warn);border-radius:6px;padding:8px 12px;font-weight:600}
ul{margin:6px 0 6px 20px;padding:0}
footer{color:var(--muted);font-size:13px;margin-top:24px}
@media print{body{background:#fff;color:#000}.card{break-inside:avoid;border-color:#999}details{display:block}details>summary{display:none}details pre{display:block}}
</style>
</head>
<body>
<main>
<header class="head">
<div><h1>{{t "rpt.title"}}</h1><div class="muted">{{.Run.RunID}}</div></div>
<span class="badge {{cls .Run.Status}}">{{status .Run.Status}}</span>
</header>
{{if .Run.Demo}}<p class="demo">{{t "rpt.demo_banner"}}</p>{{end}}
<section class="card">
<dl>
<dt>{{t "rpt.started"}}</dt><dd>{{time .Run.StartedAt}}</dd>
<dt>{{t "rpt.finished"}}</dt><dd>{{time .Run.FinishedAt}}</dd>
<dt>{{t "rpt.duration"}}</dt><dd>{{dur .Run.DurationMS}}</dd>
<dt>{{t "rpt.trigger"}}</dt><dd>{{trig .Run.Trigger}}</dd>
<dt>{{t "rpt.app_version"}}</dt><dd>{{.Run.AppVersion}}</dd>
{{if .Run.ResticVersion}}<dt>{{t "rpt.restic_version"}}</dt><dd>{{.Run.ResticVersion}}</dd>{{end}}
<dt>{{t "rpt.targets"}}</dt><dd>{{counts .Run.Summary}}</dd>
</dl>
</section>
{{range .Run.Targets}}
<section class="card">
<div class="head"><h2>{{.Name}}</h2><span class="badge {{cls .Status}}">{{status .Status}}</span></div>
<dl>
<dt>{{t "rpt.repository"}}</dt><dd>{{.Repository}}</dd>
{{with .Snapshot}}
<dt>{{t "rpt.snapshot"}}</dt><dd>{{.ShortID}} <span class="muted">({{.ID}})</span></dd>
<dt>{{t "rpt.snapshot_time"}}</dt><dd>{{time .Time}}</dd>
{{if .Hostname}}<dt>{{t "rpt.snapshot_host"}}</dt><dd>{{.Hostname}}</dd>{{end}}
{{if .Paths}}<dt>{{t "rpt.snapshot_paths"}}</dt><dd>{{join .Paths "; "}}</dd>{{end}}
{{if .Tags}}<dt>{{t "rpt.snapshot_tags"}}</dt><dd>{{join .Tags ", "}}</dd>{{end}}
{{end}}
{{with .Restore}}
<dt>{{t "rpt.restore_time"}}</dt><dd>{{dur .DurationMS}}</dd>
<dt>{{t "rpt.files_dirs"}}</dt><dd>{{.Files}} / {{.Directories}}</dd>
<dt>{{t "rpt.size"}}</dt><dd>{{bytes .Bytes}}</dd>
{{if .MetadataWarnings}}<dt>{{t "rpt.metadata_warnings"}}</dt><dd>{{.MetadataWarnings}}</dd>{{end}}
{{end}}
</dl>
<h3>{{t "rpt.stages"}}</h3>
<table>
<thead><tr><th>{{t "rpt.stage"}}</th><th>{{t "rpt.status"}}</th><th>{{t "rpt.time"}}</th><th>{{t "rpt.result"}}</th></tr></thead>
<tbody>
{{range .Stages}}<tr><td>{{stage .ID}}</td><td><span class="badge {{cls .Status}}">{{status .Status}}</span></td><td>{{dur .DurationMS}}</td><td>{{msg .Message}}</td></tr>
{{end}}</tbody>
</table>
{{range .Stages}}{{if .Rules}}
<h3>{{t "rpt.rules"}}</h3>
<table>
<thead><tr><th>{{t "rpt.rule"}}</th><th>{{t "rpt.status"}}</th><th>{{t "rpt.result"}}</th></tr></thead>
<tbody>
{{range .Rules}}<tr><td>{{if .Name}}{{.Name}}{{else}}{{.Pattern}}{{end}}<br><span class="muted">{{.Type}} &middot; {{.Pattern}}</span></td><td><span class="badge {{cls .Status}}">{{status .Status}}</span></td><td>{{msg .Message}}{{if .Failures}}<ul>{{range .Failures}}<li>{{.Path}}: {{msg .Msg}}</li>{{end}}</ul>{{end}}</td></tr>
{{end}}</tbody>
</table>
{{end}}{{end}}
{{range .Stages}}{{if showDetail .}}
<details><summary>{{t "rpt.details"}}: {{stage .ID}}</summary><pre>{{.Detail}}</pre></details>
{{end}}{{end}}
{{if .Coverage}}
<h3>{{t "rpt.coverage"}}</h3>
<ul>{{range .Coverage}}<li>{{msg .}}</li>{{end}}</ul>
{{end}}
</section>
{{end}}
<footer>{{t "rpt.disclaimer"}}</footer>
</main>
</body>
</html>
`
