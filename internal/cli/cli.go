// Package cli implements the bharbor command-line interface.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"backupharbor/internal/app"
	"backupharbor/internal/config"
	"backupharbor/internal/i18n"
	"backupharbor/internal/schedule"
	"backupharbor/internal/secret"
	"backupharbor/internal/verify"
	"backupharbor/internal/version"
)

// Exit codes (see verify.Exit*).
const (
	ExitOK        = verify.ExitOK
	ExitFailed    = verify.ExitFailed
	ExitError     = verify.ExitError
	ExitCancelled = verify.ExitCancelled
)

// IO bundles everything the commands touch so tests can replace it.
type IO struct {
	Out, Err io.Writer
	In       io.Reader
	Getenv   func(string) string
	// IsTerminal reports whether w is an interactive console.
	IsTerminal func(w io.Writer) bool
	// ReadPassword reads a secret without echo.
	ReadPassword func(prompt string) (string, error)
	Secrets      secret.Store
	Scheduler    func() *schedule.Manager
	ExePath      func() (string, error)
	// Backend replaces restic (tests).
	Backend verify.Backend
}

// DefaultIO returns the real process environment.
func DefaultIO() IO {
	return IO{Out: os.Stdout, Err: os.Stderr, In: os.Stdin, Getenv: os.Getenv,
		IsTerminal: isTerminal, ReadPassword: readPassword,
		Secrets: secret.NewCredentialManager(), Scheduler: schedule.NewManager, ExePath: os.Executable}
}

// common holds the flags every command accepts.
type common struct {
	config  string
	json    bool
	noColor bool
	lang    string
	verbose bool
	quiet   bool
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.config, "config", c.config, "")
	fs.BoolVar(&c.json, "json", c.json, "")
	fs.BoolVar(&c.noColor, "no-color", c.noColor, "")
	fs.StringVar(&c.lang, "lang", c.lang, "")
	fs.BoolVar(&c.verbose, "verbose", c.verbose, "")
	fs.BoolVar(&c.quiet, "quiet", c.quiet, "")
}

// ctx is the per-invocation state shared by command implementations.
type ctx struct {
	io    IO
	c     common
	lang  string
	color bool
	cfg   *config.Config
}

func (x *ctx) t(key string, args ...any) string { return i18n.T(x.lang, key, args...) }

func (x *ctx) out(format string, a ...any) { fmt.Fprintf(x.io.Out, format, a...) }
func (x *ctx) errf(format string, a ...any) {
	fmt.Fprintf(x.io.Err, "bharbor: "+format+"\n", a...)
}

// fail prints an error and returns the exit code.
func (x *ctx) fail(code int, msg string) int {
	if x.c.json {
		x.jsonOut(map[string]any{"error": msg, "exit_code": code})
		return code
	}
	fmt.Fprintf(x.io.Err, "%s %s\n", x.paint("1;31", x.t("cli.error_prefix")), msg)
	return code
}

func (x *ctx) jsonOut(v any) {
	enc := json.NewEncoder(x.io.Out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (x *ctx) paint(code, s string) string {
	if !x.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (x *ctx) statusText(s verify.Status) string {
	label := i18n.Upper(x.lang, x.t("status."+string(s)))
	switch s {
	case verify.Passed:
		return x.paint("32", label)
	case verify.Failed:
		return x.paint("1;31", label)
	case verify.Partial, verify.NotRun:
		return x.paint("33", label)
	case verify.Cancelled, verify.TimedOut:
		return x.paint("35", label)
	}
	return x.paint("2", label)
}

// loadConfig loads the configuration, mapping errors to messages.
func (x *ctx) loadConfig() (*config.Config, int) {
	path := config.ResolvePath(x.c.config)
	if x.c.config == "" {
		if env := x.io.Getenv("BACKUPHARBOR_CONFIG"); env != "" {
			path = env
		}
	}
	cfg, err := app.LoadConfig(path)
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			return nil, x.fail(ExitError, x.t("cli.config_not_found", path))
		}
		return nil, x.fail(ExitError, x.t("cli.config_load_failed", err.Error()))
	}
	if x.c.lang == "" {
		x.lang = app.Language(cfg, "")
	}
	x.cfg = cfg
	return cfg, -1
}

// reportConfigIssues prints validation issues; it returns true if any is an error.
func (x *ctx) printIssues(issues []config.Issue) bool {
	bad := false
	for _, is := range issues {
		tag := x.paint("33", x.t("cli.warning"))
		if is.Severity == config.SevError {
			tag = x.paint("1;31", x.t("cli.error_tag"))
			bad = true
		}
		fmt.Fprintf(x.io.Err, "  [%s] %s\n", tag, is.Text(x.lang))
	}
	return bad
}

func (x *ctx) logf(level, format string, a ...any) {
	if x.c.verbose {
		fmt.Fprintf(x.io.Err, "%s\n", secret.Redact(fmt.Sprintf("["+level+"] "+format, a...)))
	}
}

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// Run executes bharbor with args (without the program name) and returns the exit code.
func Run(cx context.Context, args []string, io IO) int {
	if io.Getenv == nil {
		io.Getenv = os.Getenv
	}
	x := &ctx{io: io}

	// Common flags may precede the command.
	pre := flag.NewFlagSet("bharbor", flag.ContinueOnError)
	pre.SetOutput(io.Err)
	pre.Usage = func() {}
	var help, ver bool
	pre.BoolVar(&help, "help", false, "")
	pre.BoolVar(&help, "h", false, "")
	pre.BoolVar(&ver, "version", false, "")
	pre.BoolVar(&ver, "v", false, "")
	x.c.register(pre)
	if err := pre.Parse(args); err != nil {
		x.setup()
		return x.fail(ExitError, x.t("cli.bad_flags", err.Error())+"\n"+x.t("cli.try_help"))
	}
	x.setup()
	rest := pre.Args()
	switch {
	case ver:
		return x.cmdVersion()
	case len(rest) == 0 && help:
		x.usage(io.Out)
		return ExitOK
	case len(rest) == 0:
		x.usage(io.Err)
		return ExitError
	}
	cmd, cargs := rest[0], rest[1:]
	switch cmd {
	case "help":
		x.usage(io.Out)
		return ExitOK
	case "version":
		return x.cmdVersion()
	case "init":
		return x.cmdInit(cargs)
	case "validate":
		return x.cmdValidate(cx, cargs)
	case "run":
		return x.cmdRun(cx, cargs)
	case "report":
		return x.cmdReport(cargs)
	case "schedule":
		return x.cmdSchedule(cx, cargs)
	case "secret":
		return x.cmdSecret(cargs)
	}
	return x.fail(ExitError, x.t("cli.unknown_command", cmd)+"\n"+x.t("cli.try_help"))
}

// setup resolves language and colour from flags and environment.
func (x *ctx) setup() {
	x.lang = i18n.Normalize(x.c.lang)
	if x.c.lang == "" || x.c.lang == "auto" {
		if env := x.io.Getenv("BACKUPHARBOR_LANG"); env != "" {
			x.lang = i18n.Normalize(env)
		} else {
			x.lang = i18n.Detect()
		}
	}
	x.color = !x.c.noColor && !x.c.json && x.io.Getenv("NO_COLOR") == "" &&
		x.io.IsTerminal != nil && x.io.IsTerminal(x.io.Out)
}

// sub creates a FlagSet for a command, seeded with the globally parsed values.
func (x *ctx) sub(name string) (*flag.FlagSet, *common) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	c := x.c
	c.register(fs)
	return fs, &c
}

// parse parses command flags and refreshes language/colour settings.
func (x *ctx) parse(fs *flag.FlagSet, c *common, args []string) (int, bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			x.usage(x.io.Out)
			return ExitOK, false
		}
		return x.fail(ExitError, x.t("cli.bad_flags", err.Error())+"\n"+x.t("cli.try_help")), false
	}
	x.c = *c
	x.setup()
	return 0, true
}

func (x *ctx) usage(w io.Writer) {
	fmt.Fprintf(w, "%s %s\n", version.ProductName, version.Version)
	fmt.Fprintln(w, x.t("cli.usage"))
}

func (x *ctx) cmdVersion() int {
	if x.c.json {
		x.jsonOut(map[string]string{"name": version.CLIName, "product": version.ProductName,
			"version": version.Version, "commit": version.Commit, "date": version.Date})
		return ExitOK
	}
	x.out("%s %s (%s, %s)\n", version.CLIName, version.Version, version.Commit, version.Date)
	return ExitOK
}
