package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"

	"backupharbor/internal/app"
	"backupharbor/internal/config"
	"backupharbor/internal/schedule"
	"backupharbor/internal/secret"
)

func (x *ctx) cmdSchedule(cx context.Context, args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return x.fail(ExitError, x.t("cli.schedule_usage"))
	}
	sub, rest := args[0], args[1:]
	fs, c := x.sub("schedule " + sub)
	var freq, at, day *string
	if sub == "install" {
		freq = fs.String("frequency", "", "")
		at = fs.String("time", "", "")
		day = fs.String("day", "", "")
	}
	if code, ok := x.parse(fs, c, rest); !ok {
		return code
	}
	mgr := x.io.Scheduler()

	switch sub {
	case "install":
		cfg, code := x.loadConfig()
		if cfg == nil {
			return code
		}
		exe, err := x.io.ExePath()
		if err != nil {
			return x.fail(ExitError, err.Error())
		}
		spec, err := app.ScheduleSpec(cfg, exe)
		if err != nil {
			return x.fail(ExitError, err.Error())
		}
		if *freq != "" {
			spec.Frequency = *freq
		}
		if *at != "" {
			spec.Time = *at
		}
		if *day != "" {
			spec.Day = *day
		}
		if issues := cfg.ValidateStatic(cfg.BaseDir()); config.HasErrors(issues) {
			x.printIssues(issues)
			return x.fail(ExitError, x.t("cli.config_invalid"))
		}
		if err := mgr.Install(cx, spec); err != nil {
			if errors.Is(err, schedule.ErrForeignTask) {
				return x.fail(ExitError, x.t("cli.schedule_foreign"))
			}
			return x.fail(ExitError, x.t("cli.schedule_install_failed", err.Error()))
		}
		st, _ := mgr.Status(cx)
		if x.c.json {
			x.jsonOut(st)
			return ExitOK
		}
		x.out("%s\n", x.t("cli.schedule_installed", spec.Frequency, spec.Time))
		x.printSchedule(st)
		return ExitOK

	case "status":
		st, err := mgr.Status(cx)
		if err != nil {
			return x.fail(ExitError, x.t("cli.schedule_status_failed", err.Error()))
		}
		if x.c.json {
			x.jsonOut(st)
			return ExitOK
		}
		if !st.Installed {
			x.out("%s\n", x.t("cli.schedule_not_installed"))
			return ExitOK
		}
		x.printSchedule(st)
		return ExitOK

	case "remove":
		if err := mgr.Remove(cx); err != nil {
			switch {
			case errors.Is(err, schedule.ErrNotInstalled):
				if x.c.json {
					x.jsonOut(map[string]any{"removed": false, "installed": false})
					return ExitOK
				}
				x.out("%s\n", x.t("cli.schedule_not_installed"))
				return ExitOK
			case errors.Is(err, schedule.ErrForeignTask):
				return x.fail(ExitError, x.t("cli.schedule_foreign"))
			}
			return x.fail(ExitError, x.t("cli.schedule_remove_failed", err.Error()))
		}
		if x.c.json {
			x.jsonOut(map[string]any{"removed": true})
			return ExitOK
		}
		x.out("%s\n", x.t("cli.schedule_removed"))
		return ExitOK
	}
	return x.fail(ExitError, x.t("cli.schedule_usage"))
}

func (x *ctx) printSchedule(st schedule.Status) {
	x.out("  %s: %s\n", x.t("cli.schedule_task"), schedule.DefaultTaskName)
	x.out("  %s: %s\n", x.t("cli.schedule_state"), st.State)
	if st.Frequency != "" {
		when := st.Time
		if st.Day != "" {
			when = st.Day + " " + st.Time
		}
		x.out("  %s: %s %s\n", x.t("cli.schedule_when"), st.Frequency, when)
	}
	if st.NextRun != nil {
		x.out("  %s: %s\n", x.t("cli.schedule_next"), st.NextRun.Local().Format("2006-01-02 15:04"))
	}
	if st.LastRun != nil {
		res := ""
		if st.LastResult != nil {
			res = fmt.Sprintf(" (0x%X)", uint32(*st.LastResult))
		}
		x.out("  %s: %s%s\n", x.t("cli.schedule_last"), st.LastRun.Local().Format("2006-01-02 15:04"), res)
	}
}

func (x *ctx) cmdSecret(args []string) int {
	if len(args) < 2 || strings.HasPrefix(args[0], "-") {
		return x.fail(ExitError, x.t("cli.secret_usage"))
	}
	sub, rest := args[0], args[1:]
	fs, c := x.sub("secret " + sub)
	fromStdin := fs.Bool("stdin", false, "")
	// The target name may come before the flags.
	name := rest[0]
	if strings.HasPrefix(name, "-") {
		return x.fail(ExitError, x.t("cli.secret_usage"))
	}
	if code, ok := x.parse(fs, c, rest[1:]); !ok {
		return code
	}
	credName := name
	if cfg, err := x.tryConfig(); err == nil {
		if t, ok := cfg.FindTarget(name); ok {
			credName = t.CredentialName()
		}
	}
	key := secret.CredentialPrefix + credName
	store := x.io.Secrets

	switch sub {
	case "set":
		var pw string
		var err error
		if *fromStdin {
			line, rerr := bufio.NewReader(x.io.In).ReadString('\n')
			if rerr != nil && line == "" {
				return x.fail(ExitError, x.t("cli.secret_read_failed"))
			}
			pw = strings.TrimRight(line, "\r\n")
		} else {
			pw, err = x.io.ReadPassword(x.t("cli.secret_prompt", name))
			if err != nil {
				return x.fail(ExitError, x.t("cli.secret_read_failed"))
			}
			again, err := x.io.ReadPassword(x.t("cli.secret_prompt_again"))
			if err != nil || again != pw {
				return x.fail(ExitError, x.t("cli.secret_mismatch"))
			}
		}
		if pw == "" {
			return x.fail(ExitError, x.t("cli.secret_empty"))
		}
		if err := store.Set(key, pw); err != nil {
			return x.fail(ExitError, x.t("cli.secret_store_failed", err.Error()))
		}
		x.out("%s\n", x.t("cli.secret_saved", key))
		return ExitOK
	case "delete":
		if err := store.Delete(key); err != nil {
			if errors.Is(err, secret.ErrNotFound) {
				return x.fail(ExitError, x.t("cli.secret_missing", key))
			}
			return x.fail(ExitError, x.t("cli.secret_store_failed", err.Error()))
		}
		x.out("%s\n", x.t("cli.secret_deleted", key))
		return ExitOK
	case "status":
		_, err := store.Get(key)
		switch {
		case err == nil:
			x.out("%s\n", x.t("cli.secret_present", key))
			return ExitOK
		case errors.Is(err, secret.ErrNotFound):
			x.out("%s\n", x.t("cli.secret_missing", key))
			return ExitError
		}
		return x.fail(ExitError, x.t("cli.secret_store_failed", err.Error()))
	}
	return x.fail(ExitError, x.t("cli.secret_usage"))
}

// tryConfig loads the configuration without reporting errors.
func (x *ctx) tryConfig() (*config.Config, error) {
	return config.Load(config.ResolvePath(x.c.config))
}
