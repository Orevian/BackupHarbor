package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"backupharbor/internal/fsx"
	"backupharbor/internal/i18n"
	"backupharbor/internal/version"
)

// Severity of a validation issue.
const (
	SevError   = "error"
	SevWarning = "warning"
)

// Issue is one problem found in a configuration.
type Issue struct {
	Severity string   `json:"severity"`
	Field    string   `json:"field"` // e.g. targets[0].repository
	Msg      i18n.Msg `json:"msg"`
}

// Text renders the issue in lang.
func (i Issue) Text(lang string) string {
	if i.Field == "" {
		return i.Msg.In(lang)
	}
	return i.Field + ": " + i.Msg.In(lang)
}

// HasErrors reports whether any issue is an error.
func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == SevError {
			return true
		}
	}
	return false
}

// IssueError wraps issues as an error.
type IssueError []Issue

// Error implements error.
func (e IssueError) Error() string {
	var parts []string
	for _, i := range e {
		if i.Severity == SevError {
			parts = append(parts, i.Text(i18n.EN))
		}
	}
	return "invalid configuration: " + strings.Join(parts, "; ")
}

var (
	reTargetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	reTime       = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	reHex64      = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	reSnapshotID = regexp.MustCompile(`^[0-9a-fA-F]{8,64}$`)
	reEnvName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	reSubset     = regexp.MustCompile(`^([0-9]+/[0-9]+|[0-9]+(\.[0-9]+)?%|[0-9]+(\.[0-9]+)?[kKmMgGtT]?)$`)
	days         = map[string]bool{"monday": true, "tuesday": true, "wednesday": true, "thursday": true, "friday": true, "saturday": true, "sunday": true}
)

func errIssue(field, code string, args ...any) Issue {
	return Issue{Severity: SevError, Field: field, Msg: i18n.M(code, args...)}
}
func warnIssue(field, code string, args ...any) Issue {
	return Issue{Severity: SevWarning, Field: field, Msg: i18n.M(code, args...)}
}

// Validate checks the configuration without touching the network or restic.
// It does check that locally referenced files exist where that is cheap.
func (c *Config) Validate() []Issue {
	issues := c.ValidateStatic(c.BaseDir())
	for i, t := range c.Targets {
		if !t.IsEnabled() {
			continue
		}
		f := fmt.Sprintf("targets[%d]", i)
		if t.Password.Source == "file" && t.Password.File != "" {
			if _, err := os.Stat(c.ResolvePasswordFile(t)); err != nil {
				issues = append(issues, errIssue(f+".password.file", "cfg.password_file_unreadable", c.ResolvePasswordFile(t)))
			}
		}
		if IsLocalRepository(t.Repository) && t.Repository != "" {
			if fi, err := os.Stat(c.ResolveRepository(t)); err != nil || !fi.IsDir() {
				issues = append(issues, errIssue(f+".repository", "cfg.repo_missing", c.ResolveRepository(t)))
			}
		}
	}
	return issues
}

// ValidateStatic performs checks that need no filesystem access to referenced
// repositories or password files. base is used to resolve relative paths.
func (c *Config) ValidateStatic(base string) []Issue {
	var out []Issue
	add := func(is ...Issue) { out = append(out, is...) }

	if c.Version != version.ConfigVersion {
		add(errIssue("version", "cfg.version_unsupported", c.Version, version.ConfigVersion))
	}
	if !i18n.Valid(c.Language) {
		add(errIssue("language", "cfg.language_invalid", c.Language))
	}
	switch c.Theme {
	case "system", "light", "dark":
	default:
		add(errIssue("theme", "cfg.theme_invalid", c.Theme))
	}
	if c.Concurrency < 1 || c.Concurrency > MaxConcurrency {
		add(errIssue("concurrency", "cfg.concurrency_range", MaxConcurrency))
	}
	if c.Timeout.D() < MinTimeout || c.Timeout.D() > MaxTimeout {
		add(errIssue("timeout", "cfg.timeout_range", MinTimeout, MaxTimeout))
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		add(errIssue("log_level", "cfg.log_level_invalid", c.LogLevel))
	}
	for _, f := range c.Reports.Formats {
		if f != "html" && f != "txt" {
			add(errIssue("reports.formats", "cfg.report_format_invalid", f))
		}
	}
	switch c.Schedule.Frequency {
	case "daily":
	case "weekly":
		if !days[strings.ToLower(c.Schedule.Day)] {
			add(errIssue("schedule.day", "cfg.schedule_day_invalid", c.Schedule.Day))
		}
	default:
		add(errIssue("schedule.frequency", "cfg.schedule_frequency_invalid", c.Schedule.Frequency))
	}
	if !reTime.MatchString(c.Schedule.Time) {
		add(errIssue("schedule.time", "cfg.schedule_time_invalid", c.Schedule.Time))
	}

	// Paths
	p := Paths{}
	func() {
		cc := *c
		cc.Path = filepath.Join(base, "config.yaml")
		p = cc.Resolved()
	}()
	if !filepath.IsAbs(p.TempDir) || fsx.IsRoot(p.TempDir) {
		add(errIssue("temp_dir", "cfg.temp_dir_unsafe", p.TempDir))
	} else if protected(p.TempDir) {
		add(errIssue("temp_dir", "cfg.temp_dir_protected", p.TempDir))
	}
	if fsx.Overlaps(p.TempDir, p.ReportDir) {
		add(errIssue("reports.dir", "cfg.report_temp_overlap"))
	}
	if c.Restic.Path != "" {
		if fi, err := os.Stat(p.Restic); err == nil && fi.IsDir() {
			add(errIssue("restic.path", "cfg.restic_path_dir", p.Restic))
		}
	}

	// Targets
	seen := map[string]int{}
	for i, t := range c.Targets {
		f := fmt.Sprintf("targets[%d]", i)
		switch {
		case t.Name == "":
			add(errIssue(f+".name", "cfg.name_required"))
		case !reTargetName.MatchString(t.Name):
			add(errIssue(f+".name", "cfg.name_invalid", t.Name))
		default:
			key := strings.ToLower(t.Name)
			if j, dup := seen[key]; dup {
				add(errIssue(f+".name", "cfg.name_duplicate", t.Name, j))
			}
			seen[key] = i
		}
		add(validateTarget(c, f, t, p)...)
	}
	return out
}

func validateTarget(c *Config, f string, t Target, p Paths) []Issue {
	var out []Issue
	add := func(is ...Issue) { out = append(out, is...) }

	if t.Repository == "" {
		add(errIssue(f+".repository", "cfg.repo_required"))
	} else if IsLocalRepository(t.Repository) {
		repo := c.ResolveRepository(t)
		if fsx.Overlaps(repo, p.TempDir) {
			add(errIssue(f+".repository", "cfg.repo_temp_overlap"))
		}
	}
	switch t.Password.Source {
	case "credential":
		if t.CredentialName() == "" {
			add(errIssue(f+".password.credential", "cfg.credential_name_required"))
		}
	case "env":
		if !reEnvName.MatchString(t.Password.Env) {
			add(errIssue(f+".password.env", "cfg.password_env_invalid", t.Password.Env))
		}
	case "file":
		if t.Password.File == "" {
			add(errIssue(f+".password.file", "cfg.password_file_required"))
		}
	default:
		add(errIssue(f+".password.source", "cfg.password_source_invalid", t.Password.Source))
	}
	switch t.Snapshot.Select {
	case "latest":
	case "tag":
		if len(t.Snapshot.Tags) == 0 {
			add(errIssue(f+".snapshot.tags", "cfg.snapshot_tags_required"))
		}
		for _, tag := range t.Snapshot.Tags {
			if strings.TrimSpace(tag) == "" || strings.ContainsAny(tag, ",\n\r") {
				add(errIssue(f+".snapshot.tags", "cfg.snapshot_tag_invalid", tag))
			}
		}
	case "id":
		if !reSnapshotID.MatchString(t.Snapshot.ID) {
			add(errIssue(f+".snapshot.id", "cfg.snapshot_id_invalid", t.Snapshot.ID))
		}
	default:
		add(errIssue(f+".snapshot.select", "cfg.snapshot_select_invalid", t.Snapshot.Select))
	}
	if t.Timeout != 0 && (t.Timeout.D() < MinTimeout || t.Timeout.D() > MaxTimeout) {
		add(errIssue(f+".timeout", "cfg.timeout_range", MinTimeout, MaxTimeout))
	}
	switch t.Check.ReadData {
	case "none", "all":
	case "subset":
		if !reSubset.MatchString(t.Check.Subset) {
			add(errIssue(f+".check.subset", "cfg.subset_invalid", t.Check.Subset))
		}
	default:
		add(errIssue(f+".check.read_data", "cfg.read_data_invalid", t.Check.ReadData))
	}
	if !t.Check.On() && !t.Restore.On() {
		add(errIssue(f, "cfg.nothing_to_verify"))
	}
	if t.Restore.MinFiles < 0 {
		add(errIssue(f+".restore.min_files", "cfg.min_files_negative"))
	}
	for _, inc := range t.Restore.Include {
		if strings.TrimSpace(inc) == "" {
			add(errIssue(f+".restore.include", "cfg.include_empty"))
		}
	}
	if len(t.Rules) > 0 && !t.Restore.On() {
		add(errIssue(f+".rules", "cfg.rules_need_restore"))
	}
	for j, r := range t.Rules {
		add(validateRule(fmt.Sprintf("%s.rules[%d]", f, j), r)...)
	}
	return out
}

func validateRule(f string, r Rule) []Issue {
	var out []Issue
	known := false
	for _, t := range RuleTypes {
		if r.Type == t {
			known = true
		}
	}
	if !known {
		out = append(out, errIssue(f+".type", "cfg.rule_type_invalid", r.Type))
	}
	p := strings.TrimSpace(r.Path)
	if p == "" {
		out = append(out, errIssue(f+".path", "cfg.rule_path_required"))
	} else {
		if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || strings.Contains(p, ":") {
			out = append(out, errIssue(f+".path", "cfg.rule_path_relative", p))
		}
		for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
			if seg == ".." {
				out = append(out, errIssue(f+".path", "cfg.rule_path_dotdot", p))
			}
		}
	}
	if r.Type == RuleSHA256 {
		if !reHex64.MatchString(r.SHA256) {
			out = append(out, errIssue(f+".sha256", "cfg.rule_sha256_invalid"))
		}
	} else if r.SHA256 != "" {
		out = append(out, errIssue(f+".sha256", "cfg.rule_sha256_type"))
	}
	if r.MaxFiles < 0 {
		out = append(out, errIssue(f+".max_files", "cfg.rule_max_files"))
	}
	return out
}

// protected reports whether p is a location that must never be used as scratch space.
func protected(p string) bool {
	var dirs []string
	for _, e := range []string{"USERPROFILE", "SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramData", "SystemDrive"} {
		if v := os.Getenv(e); v != "" {
			dirs = append(dirs, v)
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, h)
		for _, sub := range []string{"Desktop", "Documents", "Downloads", "Pictures"} {
			dirs = append(dirs, filepath.Join(h, sub))
		}
	}
	for _, d := range dirs {
		// Equal to a protected directory, or an ancestor of one.
		if strings.EqualFold(filepath.Clean(d), filepath.Clean(p)) || fsx.IsWithin(p, d) {
			return true
		}
	}
	return false
}

// TimeoutFor returns the effective timeout of a target.
func (c *Config) TimeoutFor(t Target) time.Duration {
	if t.Timeout != 0 {
		return t.Timeout.D()
	}
	return c.Timeout.D()
}
