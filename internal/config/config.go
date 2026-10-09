// Package config defines the BackupHarbor configuration file: its schema,
// defaults, strict loading, validation and atomic saving. The CLI and the GUI
// both go through this package so they apply identical rules.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"backupharbor/internal/fsx"
	"backupharbor/internal/version"
)

// Default values.
const (
	DefaultConcurrency = 1
	MaxConcurrency     = 8
	DefaultTimeout     = 2 * time.Hour
	MinTimeout         = time.Minute
	MaxTimeout         = 48 * time.Hour
	DefaultLogLevel    = "info"
)

// Duration is a time.Duration written as a Go duration string ("90m", "2h").
type Duration time.Duration

// D returns the value as time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("invalid duration %q (use values like 30m or 2h)", s)
	}
	*d = Duration(v)
	return nil
}

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// UnmarshalJSON implements json.Unmarshaler (used by the GUI).
func (d *Duration) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*d = 0
		return nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q", s)
	}
	*d = Duration(v)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Duration(d).String() + `"`), nil
}

// Config is the root of the configuration file.
type Config struct {
	Version     int            `yaml:"version" json:"version"`
	Language    string         `yaml:"language" json:"language"`
	Theme       string         `yaml:"theme" json:"theme"`
	Restic      ResticConfig   `yaml:"restic" json:"restic"`
	TempDir     string         `yaml:"temp_dir" json:"temp_dir"`
	Concurrency int            `yaml:"concurrency" json:"concurrency"`
	Timeout     Duration       `yaml:"timeout" json:"timeout"`
	LogLevel    string         `yaml:"log_level" json:"log_level"`
	Reports     ReportsConfig  `yaml:"reports" json:"reports"`
	Schedule    ScheduleConfig `yaml:"schedule" json:"schedule"`
	Targets     []Target       `yaml:"targets" json:"targets"`

	// Path is the file the config was loaded from (not serialised).
	Path string `yaml:"-" json:"-"`
}

// ResticConfig locates the restic executable.
type ResticConfig struct {
	// Path of restic.exe. Empty means: the restic.exe next to the application, then PATH.
	Path string `yaml:"path" json:"path"`
}

// ReportsConfig controls where run reports are stored.
type ReportsConfig struct {
	// Dir is the report/history directory. Empty means the default data directory.
	Dir string `yaml:"dir" json:"dir"`
	// Formats lists additional formats written next to the JSON record after
	// every run: "html" and/or "txt". JSON is always written.
	Formats []string `yaml:"formats" json:"formats"`
}

// ScheduleConfig is the schedule used by "bharbor schedule install".
type ScheduleConfig struct {
	Frequency string `yaml:"frequency" json:"frequency"` // daily | weekly
	Time      string `yaml:"time" json:"time"`           // HH:MM, 24 hour
	Day       string `yaml:"day,omitempty" json:"day"`   // weekly only: monday..sunday
}

// Target is one restic repository to verify.
type Target struct {
	Name       string         `yaml:"name" json:"name"`
	Enabled    *bool          `yaml:"enabled" json:"enabled"`
	Repository string         `yaml:"repository" json:"repository"`
	Password   PasswordConfig `yaml:"password" json:"password"`
	Snapshot   SnapshotConfig `yaml:"snapshot" json:"snapshot"`
	Timeout    Duration       `yaml:"timeout,omitempty" json:"timeout"` // 0 = global timeout
	UseLock    bool           `yaml:"use_repo_lock" json:"use_repo_lock"`
	Check      CheckConfig    `yaml:"check" json:"check"`
	Restore    RestoreConfig  `yaml:"restore" json:"restore"`
	Rules      []Rule         `yaml:"rules,omitempty" json:"rules"`
}

// IsEnabled reports whether the target takes part in runs.
func (t Target) IsEnabled() bool { return t.Enabled == nil || *t.Enabled }

// PasswordConfig names where the repository password is read from. The password
// itself is never part of the configuration.
type PasswordConfig struct {
	Source     string `yaml:"source" json:"source"`                   // credential | env | file
	Credential string `yaml:"credential,omitempty" json:"credential"` // credential name suffix (default: target name)
	Env        string `yaml:"env,omitempty" json:"env"`               // environment variable name
	File       string `yaml:"file,omitempty" json:"file"`             // password file path
}

// SnapshotConfig selects the snapshot to verify.
type SnapshotConfig struct {
	Select string   `yaml:"select" json:"select"` // latest | tag | id
	ID     string   `yaml:"id,omitempty" json:"id"`
	Tags   []string `yaml:"tags,omitempty" json:"tags"`
	Host   string   `yaml:"host,omitempty" json:"host"`
}

// CheckConfig configures "restic check".
type CheckConfig struct {
	Enabled  *bool  `yaml:"enabled" json:"enabled"`
	ReadData string `yaml:"read_data" json:"read_data"`     // none | subset | all
	Subset   string `yaml:"subset,omitempty" json:"subset"` // n/t, N%, or a size such as 500M
}

// On reports whether the integrity check runs.
func (c CheckConfig) On() bool { return c.Enabled == nil || *c.Enabled }

// RestoreConfig configures the real restore.
type RestoreConfig struct {
	Enabled  *bool    `yaml:"enabled" json:"enabled"`
	Include  []string `yaml:"include,omitempty" json:"include"` // restic --include patterns
	Verify   *bool    `yaml:"verify" json:"verify"`             // restic restore --verify
	MinFiles int      `yaml:"min_files" json:"min_files"`       // minimum restored file count
}

// On reports whether the restore runs.
func (r RestoreConfig) On() bool { return r.Enabled == nil || *r.Enabled }

// VerifyOn reports whether restored files are read back and verified by restic.
func (r RestoreConfig) VerifyOn() bool { return r.Verify == nil || *r.Verify }

// Rule is one content verification rule evaluated on the restored files.
type Rule struct {
	Name     string `yaml:"name,omitempty" json:"name"`
	Path     string `yaml:"path" json:"path"` // glob relative to the restore root; ** matches directories
	Type     string `yaml:"type" json:"type"`
	SHA256   string `yaml:"sha256,omitempty" json:"sha256"`
	Required *bool  `yaml:"required" json:"required"` // default true
	MaxFiles int    `yaml:"max_files,omitempty" json:"max_files"`
}

// IsRequired reports whether a rule with no matching file is a failure.
func (r Rule) IsRequired() bool { return r.Required == nil || *r.Required }

// Rule types.
const (
	RuleExists   = "exists"
	RuleNonEmpty = "non_empty"
	RuleText     = "text"
	RuleJSON     = "json"
	RuleXML      = "xml"
	RuleZIP      = "zip"
	RulePDF      = "pdf"
	RuleSQLite   = "sqlite"
	RuleSHA256   = "sha256"
)

// RuleTypes lists accepted rule types.
var RuleTypes = []string{RuleExists, RuleNonEmpty, RuleText, RuleJSON, RuleXML, RuleZIP, RulePDF, RuleSQLite, RuleSHA256}

func ptr[T any](v T) *T { return &v }

// Default returns a configuration with all defaults applied and no targets.
func Default() *Config {
	c := &Config{Version: version.ConfigVersion}
	c.Normalize()
	return c
}

// Normalize fills defaults for omitted values. It is idempotent.
func (c *Config) Normalize() {
	if c.Version == 0 {
		c.Version = version.ConfigVersion
	}
	if c.Language == "" {
		c.Language = "auto"
	}
	if c.Theme == "" {
		c.Theme = "system"
	}
	if c.Concurrency == 0 {
		c.Concurrency = DefaultConcurrency
	}
	if c.Timeout == 0 {
		c.Timeout = Duration(DefaultTimeout)
	}
	if c.LogLevel == "" {
		c.LogLevel = DefaultLogLevel
	}
	if c.Schedule.Frequency == "" {
		c.Schedule.Frequency = "daily"
	}
	if c.Schedule.Time == "" {
		c.Schedule.Time = "03:00"
	}
	if c.Schedule.Frequency == "weekly" && c.Schedule.Day == "" {
		c.Schedule.Day = "sunday"
	}
	if c.Reports.Formats == nil {
		c.Reports.Formats = []string{}
	}
	if c.Targets == nil {
		c.Targets = []Target{}
	}
	for i := range c.Targets {
		c.Targets[i].normalize()
	}
}

func (t *Target) normalize() {
	t.Name = strings.TrimSpace(t.Name)
	t.Repository = strings.TrimSpace(t.Repository)
	if t.Enabled == nil {
		t.Enabled = ptr(true)
	}
	if t.Password.Source == "" {
		t.Password.Source = "credential"
	}
	if t.Snapshot.Select == "" {
		t.Snapshot.Select = "latest"
	}
	if t.Check.Enabled == nil {
		t.Check.Enabled = ptr(true)
	}
	if t.Check.ReadData == "" {
		t.Check.ReadData = "none"
	}
	if t.Restore.Enabled == nil {
		t.Restore.Enabled = ptr(true)
	}
	if t.Restore.Verify == nil {
		t.Restore.Verify = ptr(true)
	}
	if t.Restore.MinFiles == 0 {
		t.Restore.MinFiles = 1
	}
	for i := range t.Rules {
		if t.Rules[i].Required == nil {
			t.Rules[i].Required = ptr(true)
		}
	}
	if t.Snapshot.Tags == nil {
		t.Snapshot.Tags = []string{}
	}
	if t.Restore.Include == nil {
		t.Restore.Include = []string{}
	}
	if t.Rules == nil {
		t.Rules = []Rule{}
	}
}

// DefaultConfigPath returns %APPDATA%\BackupHarbor\config.yaml.
func DefaultConfigPath() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		if d, err := os.UserConfigDir(); err == nil {
			base = d
		}
	}
	return filepath.Join(base, "BackupHarbor", "config.yaml")
}

// DataDir returns the per-user data directory (%LOCALAPPDATA%\BackupHarbor).
func DataDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if d, err := os.UserCacheDir(); err == nil {
			base = d
		}
	}
	return filepath.Join(base, "BackupHarbor")
}

// ResolvePath chooses the config file: explicit flag > BACKUPHARBOR_CONFIG > default.
func ResolvePath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("BACKUPHARBOR_CONFIG"); env != "" {
		return env
	}
	return DefaultConfigPath()
}

// ErrNotFound is returned by Load when the file does not exist.
var ErrNotFound = errors.New("configuration file not found")

// Load reads and strictly parses a configuration file. Unknown keys are errors.
// It normalises the result but does not validate it; call Validate.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4<<20))
	if err != nil {
		return nil, err
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, aerr := filepath.Abs(path)
	if aerr != nil {
		abs = path
	}
	c.Path = abs
	return c, nil
}

// Parse parses YAML configuration bytes strictly.
func Parse(data []byte) (*Config, error) {
	c := &Config{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	c.Normalize()
	return c, nil
}

// Marshal renders the configuration as YAML.
func (c *Config) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Save validates and atomically writes the configuration to path (or c.Path).
func (c *Config) Save(path string) error {
	if path == "" {
		path = c.Path
	}
	if path == "" {
		return errors.New("no configuration path")
	}
	c.Normalize()
	if issues := c.ValidateStatic(filepath.Dir(path)); HasErrors(issues) {
		return IssueError(issues)
	}
	data, err := c.Marshal()
	if err != nil {
		return err
	}
	header := []byte("# BackupHarbor configuration (schema version " + fmt.Sprint(version.ConfigVersion) + ")\n")
	if err := fsx.WriteFileAtomic(path, append(header, data...)); err != nil {
		return err
	}
	if abs, err := filepath.Abs(path); err == nil {
		c.Path = abs
	}
	return nil
}

// BaseDir is the directory relative paths in the file are resolved against.
func (c *Config) BaseDir() string {
	if c.Path != "" {
		return filepath.Dir(c.Path)
	}
	wd, _ := os.Getwd()
	return wd
}

// Paths holds fully resolved absolute paths.
type Paths struct {
	Restic    string // empty = bundled restic.exe, then PATH
	TempDir   string
	ReportDir string
}

// Resolved returns absolute paths. Relative values in the file are resolved
// against the directory containing the configuration file.
func (c *Config) Resolved() Paths {
	base := c.BaseDir()
	abs := func(p, def string) string {
		if strings.TrimSpace(p) == "" {
			return def
		}
		if r, err := fsx.Clean(os.ExpandEnv(p), base); err == nil {
			return r
		}
		return p
	}
	return Paths{
		Restic:    abs(c.Restic.Path, ""),
		TempDir:   abs(c.TempDir, filepath.Join(DataDir(), "work")),
		ReportDir: abs(c.Reports.Dir, filepath.Join(DataDir(), "reports")),
	}
}

// ResolvePasswordFile returns the absolute password file path of a target.
func (c *Config) ResolvePasswordFile(t Target) string {
	if t.Password.File == "" {
		return ""
	}
	if r, err := fsx.Clean(os.ExpandEnv(t.Password.File), c.BaseDir()); err == nil {
		return r
	}
	return t.Password.File
}

// ResolveRepository returns the repository specification with a relative local
// path resolved against the configuration directory.
func (c *Config) ResolveRepository(t Target) string {
	if !IsLocalRepository(t.Repository) {
		return t.Repository
	}
	repo := strings.TrimPrefix(t.Repository, "local:")
	if r, err := fsx.Clean(os.ExpandEnv(repo), c.BaseDir()); err == nil {
		return r
	}
	return t.Repository
}

// FindTarget returns the target with the given name (case-insensitive).
func (c *Config) FindTarget(name string) (Target, bool) {
	for _, t := range c.Targets {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return Target{}, false
}

// CredentialName is the Credential Manager entry name for a target.
func (t Target) CredentialName() string {
	if t.Password.Credential != "" {
		return t.Password.Credential
	}
	return t.Name
}

// IsLocalRepository reports whether repo denotes a local filesystem path
// (as opposed to sftp:, s3:, rest:, ... backends).
func IsLocalRepository(repo string) bool {
	if strings.HasPrefix(repo, "local:") {
		return true
	}
	i := strings.Index(repo, ":")
	if i <= 1 { // no scheme, or a single-letter drive
		return true
	}
	// "host:path" style schemes are 2+ letters/digits; a Windows path never has them.
	for _, r := range repo[:i] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '+' || r == '-') {
			return true
		}
	}
	return false
}
