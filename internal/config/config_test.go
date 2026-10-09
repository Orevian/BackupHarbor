package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backupharbor/internal/i18n"
)

const minimal = `
version: 1
targets:
  - name: a
    repository: 'C:\repos\a'
    password: {source: env, env: RP_A}
`

func mustParse(t *testing.T, y string) *Config {
	t.Helper()
	c, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func errorsOf(issues []Issue) []string {
	var out []string
	for _, i := range issues {
		if i.Severity == SevError {
			out = append(out, i.Field+": "+i.Msg.Code)
		}
	}
	return out
}

func hasCode(issues []Issue, code string) bool {
	for _, i := range issues {
		if i.Msg.Code == code {
			return true
		}
	}
	return false
}

func TestDefaultsAreSafe(t *testing.T) {
	c := mustParse(t, minimal)
	tg := c.Targets[0]
	if !tg.IsEnabled() || !tg.Check.On() || !tg.Restore.On() || !tg.Restore.VerifyOn() {
		t.Error("check, restore, verify and enabled must default to on")
	}
	if tg.UseLock {
		t.Error("repository locking must default to off so the repository is never written")
	}
	if tg.Snapshot.Select != "latest" || tg.Check.ReadData != "none" || tg.Restore.MinFiles != 1 {
		t.Errorf("defaults: %+v", tg)
	}
	if c.Concurrency != 1 || c.Timeout.D() != 2*time.Hour || c.Language != "auto" || c.Theme != "system" {
		t.Errorf("global defaults: %+v", c)
	}
	if issues := c.ValidateStatic(t.TempDir()); HasErrors(issues) {
		t.Errorf("minimal config should be valid: %v", errorsOf(issues))
	}
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	if _, err := Parse([]byte("version: 1\nlangauge: tr\n")); err == nil {
		t.Error("typo in a key must be an error")
	}
	if _, err := Parse([]byte("version: 1\ntargets:\n  - name: a\n    repository: x\n    pasword: {}\n")); err == nil {
		t.Error("unknown nested key must be an error")
	}
	if _, err := Parse([]byte("version: [")); err == nil {
		t.Error("malformed yaml must be an error")
	}
}

func TestPlainTextPasswordFieldDoesNotExist(t *testing.T) {
	_, err := Parse([]byte("version: 1\ntargets:\n  - name: a\n    repository: x\n    password: {source: env, env: X, value: hunter2}\n"))
	if err == nil {
		t.Fatal("a literal password value must not be accepted in the configuration")
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"cfg.version_unsupported":     "version: 7\n",
		"cfg.language_invalid":        "language: de\n",
		"cfg.theme_invalid":           "theme: neon\n",
		"cfg.concurrency_range":       "concurrency: 99\n",
		"cfg.timeout_range":           "timeout: 5s\n",
		"cfg.log_level_invalid":       "log_level: loud\n",
		"cfg.name_duplicate":          "targets:\n - {name: a, repository: x, password: {source: env, env: A}}\n - {name: A, repository: x, password: {source: env, env: A}}\n",
		"cfg.name_invalid":            "targets:\n - {name: 'bad name!', repository: x, password: {source: env, env: A}}\n",
		"cfg.repo_required":           "targets:\n - {name: a, password: {source: env, env: A}}\n",
		"cfg.password_env_invalid":    "targets:\n - {name: a, repository: x, password: {source: env, env: '1 bad'}}\n",
		"cfg.password_source_invalid": "targets:\n - {name: a, repository: x, password: {source: plain}}\n",
		"cfg.snapshot_tags_required":  "targets:\n - {name: a, repository: x, password: {source: env, env: A}, snapshot: {select: tag}}\n",
		"cfg.snapshot_id_invalid":     "targets:\n - {name: a, repository: x, password: {source: env, env: A}, snapshot: {select: id, id: zz}}\n",
		"cfg.subset_invalid":          "targets:\n - {name: a, repository: x, password: {source: env, env: A}, check: {read_data: subset, subset: lots}}\n",
		"cfg.nothing_to_verify":       "targets:\n - {name: a, repository: x, password: {source: env, env: A}, check: {enabled: false}, restore: {enabled: false}}\n",
		"cfg.rule_type_invalid":       "targets:\n - {name: a, repository: x, password: {source: env, env: A}, rules: [{path: '**/x', type: magic}]}\n",
		"cfg.rule_path_dotdot":        "targets:\n - {name: a, repository: x, password: {source: env, env: A}, rules: [{path: '../x', type: json}]}\n",
		"cfg.rule_path_relative":      "targets:\n - {name: a, repository: x, password: {source: env, env: A}, rules: [{path: 'C:/x', type: json}]}\n",
		"cfg.rule_sha256_invalid":     "targets:\n - {name: a, repository: x, password: {source: env, env: A}, rules: [{path: x, type: sha256, sha256: abc}]}\n",
		"cfg.rule_sha256_type":        "targets:\n - {name: a, repository: x, password: {source: env, env: A}, rules: [{path: x, type: json, sha256: " + strings.Repeat("a", 64) + "}]}\n",
		"cfg.rules_need_restore":      "targets:\n - {name: a, repository: x, password: {source: env, env: A}, restore: {enabled: false}, rules: [{path: x, type: json}]}\n",
		"cfg.schedule_time_invalid":   "schedule: {frequency: daily, time: '25:99'}\n",
		"cfg.schedule_day_invalid":    "schedule: {frequency: weekly, time: '03:00', day: funday}\n",
		"cfg.report_format_invalid":   "reports: {formats: [pdf]}\n",
	}
	for code, y := range cases {
		c := mustParse(t, y)
		if !hasCode(c.ValidateStatic(t.TempDir()), code) {
			t.Errorf("expected issue %s for:\n%s\ngot %v", code, y, errorsOf(c.ValidateStatic(t.TempDir())))
		}
	}
}

func TestTempDirSafety(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, bad := range []string{`C:\`, home, filepath.Join(home, "Documents"), `relative\path`} {
		c := mustParse(t, minimal)
		c.TempDir = bad
		issues := c.ValidateStatic(t.TempDir())
		if !hasCode(issues, "cfg.temp_dir_unsafe") && !hasCode(issues, "cfg.temp_dir_protected") {
			// Relative paths resolve against the config dir and are acceptable; others are not.
			if bad == `relative\path` {
				continue
			}
			t.Errorf("temp_dir %q should be rejected", bad)
		}
	}
	c := mustParse(t, minimal)
	c.TempDir = filepath.Join(t.TempDir(), "scratch")
	if HasErrors(c.ValidateStatic(t.TempDir())) {
		t.Error("a private scratch directory must be accepted")
	}
}

func TestRepositoryMustNotOverlapTempDir(t *testing.T) {
	base := t.TempDir()
	c := mustParse(t, minimal)
	c.TempDir = filepath.Join(base, "work")
	c.Targets[0].Repository = filepath.Join(base, "work", "repo")
	if !hasCode(c.ValidateStatic(base), "cfg.repo_temp_overlap") {
		t.Error("repository inside the scratch directory must be rejected")
	}
	c.Targets[0].Repository = base // contains the scratch dir
	if !hasCode(c.ValidateStatic(base), "cfg.repo_temp_overlap") {
		t.Error("scratch directory inside the repository must be rejected")
	}
}

func TestRelativePathsResolveAgainstConfigDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.yaml")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`
version: 1
restic: {path: bin\restic.exe}
temp_dir: work
reports: {dir: reports}
targets:
  - name: a
    repository: repo
    password: {source: file, file: secret.pw}
`), 0o644)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "sub")
	p := c.Resolved()
	if p.Restic != filepath.Join(base, "bin", "restic.exe") || p.TempDir != filepath.Join(base, "work") || p.ReportDir != filepath.Join(base, "reports") {
		t.Errorf("resolved paths: %+v", p)
	}
	if got := c.ResolveRepository(c.Targets[0]); got != filepath.Join(base, "repo") {
		t.Errorf("repository = %q", got)
	}
	if got := c.ResolvePasswordFile(c.Targets[0]); got != filepath.Join(base, "secret.pw") {
		t.Errorf("password file = %q", got)
	}
}

func TestRemoteRepositoriesAreNotTreatedAsPaths(t *testing.T) {
	for repo, local := range map[string]bool{
		`C:\backups`: true, `D:/x`: true, `relative`: true, `\\nas\share\r`: true, `local:C:\x`: true,
		`sftp:user@host:/srv/r`: false, `s3:s3.amazonaws.com/b`: false, `rest:https://h/r`: false, `rclone:remote:p`: false, `b2:bucket:p`: false,
	} {
		if IsLocalRepository(repo) != local {
			t.Errorf("IsLocalRepository(%q) = %v, want %v", repo, !local, local)
		}
	}
}

func TestSaveRoundTripAndAtomicity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	c := mustParse(t, minimal)
	c.TempDir = filepath.Join(dir, "work")
	c.Language = "tr"
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Language != "tr" || len(back.Targets) != 1 || back.Targets[0].Name != "a" || !back.Targets[0].IsEnabled() {
		t.Errorf("round trip: %+v", back)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".partial") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	// An invalid configuration is never written.
	bad := mustParse(t, "concurrency: 50\n")
	before, _ := os.ReadFile(path)
	if err := bad.Save(path); err == nil {
		t.Error("invalid configuration must not be saved")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("existing file modified by a rejected save")
	}
}

func TestExampleTemplateParsesAndValidates(t *testing.T) {
	c, err := Parse([]byte(ExampleYAML))
	if err != nil {
		t.Fatalf("example config does not parse: %v", err)
	}
	if len(c.Targets) != 1 || c.Targets[0].IsEnabled() {
		t.Error("the example target must be present but disabled")
	}
	if issues := c.ValidateStatic(t.TempDir()); HasErrors(issues) {
		t.Errorf("example config invalid: %v", errorsOf(issues))
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestIssueTextIsLocalized(t *testing.T) {
	c := mustParse(t, "theme: neon\n")
	for _, is := range c.ValidateStatic(t.TempDir()) {
		if is.Msg.Code == "cfg.theme_invalid" {
			if !strings.Contains(is.Text(i18n.TR), "Bilinmeyen tema") || !strings.Contains(is.Text(i18n.EN), "Unknown theme") {
				t.Errorf("not localized: %q / %q", is.Text(i18n.TR), is.Text(i18n.EN))
			}
			return
		}
	}
	t.Error("issue not produced")
}
