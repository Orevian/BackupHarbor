// Package gui is the back end of the BackupHarbor desktop application. The
// exported methods of App are bound to the web front end by Wails. The package
// itself does not import Wails, so it can be tested directly; window dialogs and
// event delivery are injected by cmd/backupharbor.
//
// Every method delegates to the same packages the CLI uses (config, verify,
// report, schedule), so both front ends apply identical rules and share one
// configuration file and one report history.
package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"backupharbor/internal/app"
	"backupharbor/internal/config"
	"backupharbor/internal/i18n"
	"backupharbor/internal/restic"
	"backupharbor/internal/schedule"
	"backupharbor/internal/secret"
	"backupharbor/internal/verify"
	"backupharbor/internal/version"
)

// App is the object bound to the front end.
type App struct {
	// Injected by the shell (cmd/backupharbor); all optional.
	Emit     func(event string, data any)
	PickFile func(title, filter string) (string, error)
	PickDir  func(title string) (string, error)
	SaveFile func(title, defaultName, filter string) (string, error)
	OpenPath func(path string) error

	// Injected by tests.
	Backend        verify.Backend
	Secrets        secret.Store
	Scheduler      *schedule.Manager
	ExePath        func() (string, error)
	ConfigOverride string

	mu     sync.Mutex
	ctx    context.Context
	demo   bool
	run    *runState
	log    []LogLine
	logger *app.Logger
}

// LogLine is one entry of the live log.
type LogLine struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Line  string    `json:"line"`
}

const maxLogLines = 500

// New creates the application back end.
func New() *App { return &App{} }

// startup receives the application context (cancelled when the window closes).
func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// shutdown cancels a running verification and releases resources.
func (a *App) shutdown() {
	a.CancelRun()
	a.mu.Lock()
	r := a.run
	a.mu.Unlock()
	if r != nil {
		select {
		case <-r.done:
		case <-time.After(30 * time.Second):
		}
	}
	a.logger.Close()
}

func (a *App) emit(event string, data any) {
	if a.Emit != nil {
		a.Emit(event, data)
	}
}

func (a *App) secrets() secret.Store {
	if a.Secrets != nil {
		return a.Secrets
	}
	return secret.NewCredentialManager()
}

func (a *App) configPath() string {
	if a.ConfigOverride != "" {
		return a.ConfigOverride
	}
	return config.ResolvePath("")
}

func (a *App) scheduler() *schedule.Manager {
	if a.Scheduler != nil {
		return a.Scheduler
	}
	return schedule.NewManager()
}

// loadConfig returns the configuration, or a default one (not yet saved) if
// no file exists. exists reports whether a file was read.
func (a *App) loadConfig() (cfg *config.Config, exists bool, err error) {
	path := a.configPath()
	cfg, err = config.Load(path)
	if err == nil {
		return cfg, true, nil
	}
	if errors.Is(err, config.ErrNotFound) {
		cfg = config.Default()
		abs, _ := filepath.Abs(path)
		cfg.Path = abs
		return cfg, false, nil
	}
	return nil, true, err
}

// lang resolves the interface language.
func (a *App) lang(cfg *config.Config) string {
	if cfg == nil {
		return i18n.Detect()
	}
	return app.Language(cfg, "")
}

func (a *App) log_(level, format string, args ...any) {
	if a.logger != nil {
		a.logger.Logf(level, format, args...)
	}
}

// ---- bootstrap ---------------------------------------------------------------

// ResticStatus describes the restic installation.
type ResticStatus struct {
	Found   bool   `json:"found"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

// Bootstrap is everything the front end needs at start-up.
type Bootstrap struct {
	Version      string            `json:"version"`
	Language     string            `json:"language"`
	LanguageSet  string            `json:"language_setting"`
	Theme        string            `json:"theme"`
	Catalog      map[string]string `json:"catalog"`
	ConfigPath   string            `json:"config_path"`
	ConfigExists bool              `json:"config_exists"`
	ConfigError  string            `json:"config_error,omitempty"`
	TargetCount  int               `json:"target_count"`
	FirstRun     bool              `json:"first_run"`
	Restic       ResticStatus      `json:"restic"`
	Demo         bool              `json:"demo"`
	Running      bool              `json:"running"`
}

// Bootstrap loads the initial application state.
func (a *App) Bootstrap() Bootstrap {
	cfg, exists, err := a.loadConfig()
	b := Bootstrap{Version: version.Version, ConfigPath: a.configPath(), ConfigExists: exists, Demo: a.IsDemo(), Running: a.IsRunning()}
	if err != nil {
		b.ConfigError = err.Error()
		b.Language = i18n.Detect()
		b.LanguageSet, b.Theme = "auto", "system"
		b.Catalog = i18n.Catalog(b.Language)
		b.Restic = a.CheckRestic()
		return b
	}
	if a.logger == nil {
		a.logger = app.NewLogger(cfg.LogLevel)
		a.logger.AddSink(func(level, line string) { a.appendLog(level, line) })
	}
	b.Language = a.lang(cfg)
	b.LanguageSet, b.Theme = cfg.Language, cfg.Theme
	b.Catalog = i18n.Catalog(b.Language)
	b.TargetCount = len(cfg.Targets)
	b.FirstRun = !exists || len(cfg.Targets) == 0
	b.Restic = a.CheckRestic()
	return b
}

// CheckRestic locates restic and reads its version.
func (a *App) CheckRestic() ResticStatus {
	cfg, _, err := a.loadConfig()
	configured := ""
	if err == nil {
		configured = cfg.Resolved().Restic
	}
	exe, lerr := restic.Locate(configured)
	if lerr != nil {
		return ResticStatus{Error: lerr.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	v, verr := restic.New(exe).Version(ctx)
	if verr != nil {
		return ResticStatus{Path: exe, Error: verr.Error()}
	}
	st := ResticStatus{Found: true, Path: exe, Version: v.String()}
	if !v.AtLeast(version.MinResticMajor, version.MinResticMinor) {
		st.Found = false
		st.Error = i18n.M("restic.too_old", v.String(), fmt.Sprintf("%d.%d", version.MinResticMajor, version.MinResticMinor)).Text
	}
	return st
}

// InitConfig creates an empty configuration file at the default location if none exists.
func (a *App) InitConfig() (string, error) {
	cfg, exists, err := a.loadConfig()
	if err != nil {
		return "", err
	}
	if exists {
		return cfg.Path, nil
	}
	if err := cfg.Save(cfg.Path); err != nil {
		return "", err
	}
	return cfg.Path, nil
}

// ---- settings ------------------------------------------------------------------

// Settings are the application-wide options.
type Settings struct {
	Language      string   `json:"language"`
	Theme         string   `json:"theme"`
	ResticPath    string   `json:"restic_path"`
	TempDir       string   `json:"temp_dir"`
	ReportDir     string   `json:"report_dir"`
	Concurrency   int      `json:"concurrency"`
	Timeout       string   `json:"timeout"`
	LogLevel      string   `json:"log_level"`
	ReportFormats []string `json:"report_formats"`
	// Effective (resolved) locations, read-only.
	EffectiveTempDir   string `json:"effective_temp_dir"`
	EffectiveReportDir string `json:"effective_report_dir"`
	ConfigPath         string `json:"config_path"`
	LogPath            string `json:"log_path"`
}

func settingsOf(cfg *config.Config) Settings {
	p := cfg.Resolved()
	return Settings{Language: cfg.Language, Theme: cfg.Theme, ResticPath: cfg.Restic.Path, TempDir: cfg.TempDir,
		ReportDir: cfg.Reports.Dir, Concurrency: cfg.Concurrency, Timeout: cfg.Timeout.D().String(), LogLevel: cfg.LogLevel,
		ReportFormats: append([]string{}, cfg.Reports.Formats...), EffectiveTempDir: p.TempDir, EffectiveReportDir: p.ReportDir,
		ConfigPath: cfg.Path, LogPath: app.LogPath()}
}

// GetSettings returns the current settings.
func (a *App) GetSettings() (Settings, error) {
	cfg, _, err := a.loadConfig()
	if err != nil {
		return Settings{}, err
	}
	return settingsOf(cfg), nil
}

// SaveResult is returned by operations that validate before saving.
type SaveResult struct {
	Saved  bool           `json:"saved"`
	Issues []config.Issue `json:"issues"`
	// Messages are the issues rendered in the current language.
	Messages []string `json:"messages"`
}

func (a *App) result(cfg *config.Config, issues []config.Issue, saved bool) SaveResult {
	l := a.lang(cfg)
	r := SaveResult{Saved: saved, Issues: issues, Messages: []string{}}
	if r.Issues == nil {
		r.Issues = []config.Issue{}
	}
	for _, is := range issues {
		r.Messages = append(r.Messages, is.Text(l))
	}
	return r
}

// SaveSettings validates and stores the settings.
func (a *App) SaveSettings(s Settings) (SaveResult, error) {
	if a.IsRunning() {
		return SaveResult{}, errors.New(i18n.T(a.currentLang(), "gui.busy"))
	}
	cfg, _, err := a.loadConfig()
	if err != nil {
		return SaveResult{}, err
	}
	cfg.Language, cfg.Theme = s.Language, s.Theme
	cfg.Restic.Path, cfg.TempDir, cfg.Reports.Dir = strings.TrimSpace(s.ResticPath), strings.TrimSpace(s.TempDir), strings.TrimSpace(s.ReportDir)
	cfg.Concurrency, cfg.LogLevel = s.Concurrency, s.LogLevel
	cfg.Reports.Formats = s.ReportFormats
	d, perr := time.ParseDuration(strings.TrimSpace(s.Timeout))
	if perr != nil {
		return a.result(cfg, []config.Issue{{Severity: config.SevError, Field: "timeout",
			Msg: i18n.M("cfg.timeout_range", config.MinTimeout, config.MaxTimeout)}}, false), nil
	}
	cfg.Timeout = config.Duration(d)
	cfg.Normalize()
	issues := cfg.ValidateStatic(cfg.BaseDir())
	if config.HasErrors(issues) {
		return a.result(cfg, issues, false), nil
	}
	if err := cfg.Save(cfg.Path); err != nil {
		return SaveResult{}, err
	}
	return a.result(cfg, issues, true), nil
}

func (a *App) currentLang() string {
	cfg, _, err := a.loadConfig()
	if err != nil {
		return i18n.Detect()
	}
	return a.lang(cfg)
}

// Catalog returns the message catalog of a language (used after switching language).
func (a *App) Catalog(lang string) map[string]string { return i18n.Catalog(lang) }

// ---- about ---------------------------------------------------------------------

// AppInfo describes the build and its dependencies.
type AppInfo struct {
	Product      string            `json:"product"`
	Version      string            `json:"version"`
	Commit       string            `json:"commit"`
	BuildDate    string            `json:"build_date"`
	GoVersion    string            `json:"go_version"`
	Platform     string            `json:"platform"`
	WebView2     string            `json:"webview2"`
	Restic       ResticStatus      `json:"restic"`
	Dependencies map[string]string `json:"dependencies"`
	ConfigPath   string            `json:"config_path"`
	ReportDir    string            `json:"report_dir"`
	LogPath      string            `json:"log_path"`
}

// AppInfo returns version and dependency information.
func (a *App) AppInfo() AppInfo {
	info := AppInfo{Product: version.ProductName, Version: version.Version, Commit: version.Commit, BuildDate: version.Date,
		GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, WebView2: webView2Version(),
		Restic: a.CheckRestic(), Dependencies: map[string]string{}, ConfigPath: a.configPath(), LogPath: app.LogPath()}
	if cfg, _, err := a.loadConfig(); err == nil {
		info.ReportDir = cfg.Resolved().ReportDir
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		want := map[string]string{"github.com/wailsapp/wails/v2": "Wails", "gopkg.in/yaml.v3": "yaml.v3",
			"modernc.org/sqlite": "SQLite (modernc.org/sqlite)", "golang.org/x/sys": "golang.org/x/sys", "golang.org/x/text": "golang.org/x/text"}
		for _, d := range bi.Deps {
			if name, ok := want[d.Path]; ok {
				info.Dependencies[name] = d.Version
			}
		}
	}
	return info
}

// ---- opening locations ----------------------------------------------------------

// OpenFolder opens a well-known folder in Explorer: "reports", "logs", "config" or "data".
func (a *App) OpenFolder(which string) error {
	cfg, _, err := a.loadConfig()
	if err != nil {
		return err
	}
	var dir string
	switch which {
	case "reports":
		dir = cfg.Resolved().ReportDir
	case "logs":
		dir = filepath.Dir(app.LogPath())
	case "config":
		dir = filepath.Dir(a.configPath())
	case "data":
		dir = config.DataDir()
	default:
		return fmt.Errorf("unknown folder %q", which)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return a.open(dir)
}

func (a *App) open(path string) error {
	if a.OpenPath != nil {
		return a.OpenPath(path)
	}
	return shellOpen(path)
}

// BrowseFile lets the user choose a file (restic.exe, password file).
func (a *App) BrowseFile(title, filter string) (string, error) {
	if a.PickFile == nil {
		return "", errors.New("dialogs are not available")
	}
	return a.PickFile(title, filter)
}

// BrowseFolder lets the user choose a directory.
func (a *App) BrowseFolder(title string) (string, error) {
	if a.PickDir == nil {
		return "", errors.New("dialogs are not available")
	}
	return a.PickDir(title)
}

// Lifecycle returns the start-up and shut-down hooks for the window shell. They
// are deliberately not methods of App, so they are not callable from JavaScript.
func Lifecycle(a *App) (startup func(context.Context), shutdown func()) { return a.startup, a.shutdown }

// IsBusy reports whether closing the window would interrupt a verification.
func (a *App) IsBusy() bool { return a.IsRunning() }
