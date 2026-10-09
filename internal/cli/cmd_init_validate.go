package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"backupharbor/internal/app"
	"backupharbor/internal/config"
	"backupharbor/internal/fsx"
	"backupharbor/internal/verify"
)

func (x *ctx) cmdInit(args []string) int {
	fs, c := x.sub("init")
	force := fs.Bool("force", false, "")
	if code, ok := x.parse(fs, c, args); !ok {
		return code
	}
	path := config.ResolvePath(x.c.config)
	abs, err := filepath.Abs(path)
	if err != nil {
		return x.fail(ExitError, err.Error())
	}
	backup := ""
	if _, err := os.Stat(abs); err == nil {
		if !*force {
			return x.fail(ExitError, x.t("cli.init_exists", abs))
		}
		backup = abs + ".bak-" + time.Now().Format("20060102-150405")
		data, rerr := os.ReadFile(abs)
		if rerr != nil {
			return x.fail(ExitError, rerr.Error())
		}
		if werr := fsx.WriteFileAtomic(backup, data); werr != nil {
			return x.fail(ExitError, x.t("cli.init_backup_failed", werr.Error()))
		}
	}
	if err := fsx.WriteFileAtomic(abs, []byte(config.ExampleYAML)); err != nil {
		return x.fail(ExitError, x.t("cli.init_write_failed", abs, err.Error()))
	}
	if x.c.json {
		x.jsonOut(map[string]any{"path": abs, "created": true, "backup": backup})
		return ExitOK
	}
	x.out("%s\n", x.t("cli.init_created", abs))
	if backup != "" {
		x.out("%s\n", x.t("cli.init_backed_up", backup))
	}
	x.out("%s\n", x.t("cli.init_next"))
	return ExitOK
}

func (x *ctx) cmdValidate(cx context.Context, args []string) int {
	fs, c := x.sub("validate")
	var targets stringList
	fs.Var(&targets, "target", "")
	if code, ok := x.parse(fs, c, args); !ok {
		return code
	}
	cfg, code := x.loadConfig()
	if cfg == nil {
		return code
	}
	res, err := app.Validate(cx, cfg, targets, app.Options{Secrets: x.io.Secrets, Backend: x.io.Backend, Logf: x.logf})
	if err != nil {
		return x.errorExit(err)
	}
	if x.c.json {
		x.jsonOut(res)
		if !res.OK {
			return ExitError
		}
		return ExitOK
	}
	x.out("%s %s\n", x.t("cli.config_file"), cfg.Path)
	if len(res.Issues) == 0 {
		x.out("  %s\n", x.t("cli.config_ok"))
	}
	bad := false
	for _, is := range res.Issues {
		tag := x.paint("33", x.t("cli.warning"))
		if is.Severity == config.SevError {
			tag = x.paint("1;31", x.t("cli.error_tag"))
			bad = true
		}
		x.out("  [%s] %s\n", tag, is.Text(x.lang))
	}
	if res.ResticVersion != "" {
		x.out("restic %s\n", res.ResticVersion)
	}
	for _, t := range res.Targets {
		x.out("\n%s %s\n", x.t("cli.target"), t.Name)
		tw := tabwriter.NewWriter(x.io.Out, 0, 4, 2, ' ', 0)
		for _, st := range t.Stages {
			if st.Status == verify.Skipped {
				continue
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", x.t("stage."+st.ID), x.statusText(st.Status), st.Message.In(x.lang))
		}
		tw.Flush()
		for _, st := range t.Stages {
			if st.Detail != "" && st.Status != verify.Passed {
				x.out("    %s\n", firstLine(st.Detail))
			}
		}
		if tg, ok := cfg.FindTarget(t.Name); ok {
			x.out("  %s\n", x.t("cli.scope"))
			for _, l := range x.scopeLines(tg) {
				x.out("    - %s\n", l)
			}
		}
	}
	ok := !bad && res.OK
	if len(res.Targets) == 0 && !bad {
		ok = false
	}
	if ok {
		x.out("\n%s\n", x.paint("32", x.t("cli.validate_ok")))
		return ExitOK
	}
	x.out("\n%s\n", x.paint("1;31", x.t("cli.validate_failed")))
	return ExitError
}

// scopeLines explains in plain words what a run of the target will test and what it costs.
func (x *ctx) scopeLines(t config.Target) []string {
	var out []string
	if t.Check.On() {
		out = append(out, x.t("scope.check_"+t.Check.ReadData, t.Check.Subset))
	} else {
		out = append(out, x.t("scope.check_off"))
	}
	if t.Restore.On() {
		if len(t.Restore.Include) > 0 {
			out = append(out, x.t("scope.restore_filtered", len(t.Restore.Include)))
		} else {
			out = append(out, x.t("scope.restore_full"))
		}
		if t.Restore.VerifyOn() {
			out = append(out, x.t("scope.restore_verified"))
		}
		if len(t.Rules) > 0 {
			out = append(out, x.t("scope.rules", len(t.Rules)))
		}
	} else {
		out = append(out, x.t("scope.restore_off"))
	}
	return out
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			return s[:i]
		}
	}
	return s
}

// errorExit maps run-level errors to messages and exit codes.
func (x *ctx) errorExit(err error) int {
	var ce *app.ConfigError
	switch {
	case asErr(err, &ce):
		if len(ce.Issues) > 0 && !x.c.json {
			x.printIssues(ce.Issues)
		}
		return x.fail(ExitError, x.t("cli.config_invalid"))
	case isDep(err):
		return x.fail(ExitError, x.t("cli.restic_missing", err.Error()))
	case isUnknownTarget(err, x):
		return ExitError
	case isNoTargets(err):
		return x.fail(ExitError, x.t("cli.no_targets"))
	}
	return x.fail(ExitError, err.Error())
}
