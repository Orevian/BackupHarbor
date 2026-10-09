package gui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"backupharbor/internal/app"
	"backupharbor/internal/config"
	"backupharbor/internal/i18n"
	"backupharbor/internal/secret"
	"backupharbor/internal/verify"
)

// TargetsView is the configured targets plus context for the editor.
type TargetsView struct {
	Targets    []config.Target `json:"targets"`
	ConfigPath string          `json:"config_path"`
	Exists     bool            `json:"exists"`
	// HasPassword maps target name to whether its credential entry exists.
	HasPassword map[string]bool `json:"has_password"`
}

// ListTargets returns the configured targets.
func (a *App) ListTargets() (TargetsView, error) {
	cfg, exists, err := a.loadConfig()
	if err != nil {
		return TargetsView{}, err
	}
	v := TargetsView{Targets: cfg.Targets, ConfigPath: cfg.Path, Exists: exists, HasPassword: map[string]bool{}}
	for _, t := range cfg.Targets {
		if t.Password.Source == secret.SourceCredential {
			_, gerr := a.secrets().Get(secret.CredentialPrefix + t.CredentialName())
			v.HasPassword[t.Name] = gerr == nil
		}
	}
	return v, nil
}

// NewTarget returns a target pre-filled with the defaults for the editor.
func (a *App) NewTarget() config.Target {
	c := config.Default()
	c.Targets = []config.Target{{}}
	c.Normalize()
	return c.Targets[0]
}

// overlay serves one typed (not yet saved) password on top of the real store.
type overlay struct {
	base  secret.Store
	key   string
	value string
}

func (o overlay) Get(name string) (string, error) {
	if name == o.key && o.value != "" {
		return o.value, nil
	}
	return o.base.Get(name)
}
func (o overlay) Set(n, v string) error { return o.base.Set(n, v) }
func (o overlay) Delete(n string) error { return o.base.Delete(n) }

// SaveTarget validates and stores a target. originalName is the name before
// editing ("" for a new target). A non-empty password is stored in Windows
// Credential Manager (never in the configuration file).
func (a *App) SaveTarget(t config.Target, originalName, password string) (SaveResult, error) {
	if a.IsRunning() {
		return SaveResult{}, errors.New(i18n.T(a.currentLang(), "gui.busy"))
	}
	cfg, _, err := a.loadConfig()
	if err != nil {
		return SaveResult{}, err
	}
	t.Name = strings.TrimSpace(t.Name)
	idx := -1
	if originalName != "" {
		for i, e := range cfg.Targets {
			if strings.EqualFold(e.Name, originalName) {
				idx = i
			}
		}
		if idx < 0 {
			return SaveResult{}, fmt.Errorf("target %q no longer exists", originalName)
		}
		cfg.Targets[idx] = t
	} else {
		cfg.Targets = append(cfg.Targets, t)
	}
	cfg.Normalize()
	issues := cfg.ValidateStatic(cfg.BaseDir())
	if config.HasErrors(issues) {
		return a.result(cfg, issues, false), nil
	}
	saved := cfg.Targets[len(cfg.Targets)-1]
	if idx >= 0 {
		saved = cfg.Targets[idx]
	}
	store := a.secrets()
	newKey := secret.CredentialPrefix + saved.CredentialName()
	// A rename of a target using the default credential name carries its password along.
	if idx >= 0 && saved.Password.Source == secret.SourceCredential && password == "" && saved.Password.Credential == "" &&
		!strings.EqualFold(originalName, saved.Name) {
		oldKey := secret.CredentialPrefix + originalName
		if v, gerr := store.Get(oldKey); gerr == nil {
			if serr := store.Set(newKey, v); serr == nil {
				_ = store.Delete(oldKey)
			}
		}
	}
	if err := cfg.Save(cfg.Path); err != nil {
		return SaveResult{}, err
	}
	if password != "" && saved.Password.Source == secret.SourceCredential {
		if err := store.Set(newKey, password); err != nil {
			return SaveResult{}, fmt.Errorf("the target was saved but its password could not be stored: %w", err)
		}
	}
	return a.result(cfg, issues, true), nil
}

// DeleteTarget removes a target. If deleteCredential is set and the target
// used a Credential Manager entry, that entry is removed as well.
func (a *App) DeleteTarget(name string, deleteCredential bool) error {
	if a.IsRunning() {
		return errors.New(i18n.T(a.currentLang(), "gui.busy"))
	}
	cfg, _, err := a.loadConfig()
	if err != nil {
		return err
	}
	out := cfg.Targets[:0:0]
	var removed *config.Target
	for i := range cfg.Targets {
		if strings.EqualFold(cfg.Targets[i].Name, name) {
			removed = &cfg.Targets[i]
			continue
		}
		out = append(out, cfg.Targets[i])
	}
	if removed == nil {
		return fmt.Errorf("target %q does not exist", name)
	}
	cfg.Targets = out
	if err := cfg.Save(cfg.Path); err != nil {
		return err
	}
	if deleteCredential && removed.Password.Source == secret.SourceCredential {
		_ = a.secrets().Delete(secret.CredentialPrefix + removed.CredentialName())
	}
	return nil
}

// SetTargetEnabled enables or disables a target.
func (a *App) SetTargetEnabled(name string, enabled bool) error {
	if a.IsRunning() {
		return errors.New(i18n.T(a.currentLang(), "gui.busy"))
	}
	cfg, _, err := a.loadConfig()
	if err != nil {
		return err
	}
	for i := range cfg.Targets {
		if strings.EqualFold(cfg.Targets[i].Name, name) {
			cfg.Targets[i].Enabled = &enabled
			return cfg.Save(cfg.Path)
		}
	}
	return fmt.Errorf("target %q does not exist", name)
}

// ClearPassword removes a target's stored credential.
func (a *App) ClearPassword(name string) error {
	cfg, _, err := a.loadConfig()
	if err != nil {
		return err
	}
	t, ok := cfg.FindTarget(name)
	if !ok {
		return fmt.Errorf("target %q does not exist", name)
	}
	err = a.secrets().Delete(secret.CredentialPrefix + t.CredentialName())
	if errors.Is(err, secret.ErrNotFound) {
		return nil
	}
	return err
}

// TestResult is the outcome of testing a target without restoring anything.
type TestResult struct {
	Result   verify.TargetResult `json:"result"`
	Messages []string            `json:"messages"`
	Issues   []config.Issue      `json:"issues"`
	Error    string              `json:"error,omitempty"`
}

// TestTarget checks an edited (possibly unsaved) target: configuration rules,
// restic, password source, repository access and snapshot selection. Nothing is
// restored and nothing is written to the repository.
func (a *App) TestTarget(t config.Target, password string) TestResult {
	cfg, _, err := a.loadConfig()
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	t.Name = strings.TrimSpace(t.Name)
	cfg.Targets = []config.Target{t}
	cfg.Normalize()
	t = cfg.Targets[0]
	issues := cfg.ValidateStatic(cfg.BaseDir())
	l := a.lang(cfg)
	res := TestResult{Issues: issues, Messages: []string{}}
	for _, is := range issues {
		res.Messages = append(res.Messages, is.Text(l))
	}
	if config.HasErrors(issues) {
		return res
	}
	var store secret.Store = a.secrets()
	if password != "" && t.Password.Source == secret.SourceCredential {
		store = overlay{base: store, key: secret.CredentialPrefix + t.CredentialName(), value: password}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tr, err := app.PreflightTarget(ctx, cfg, t, app.Options{Secrets: store, Backend: a.Backend})
	if err != nil {
		res.Error = i18n.T(l, "cli.restic_missing", err.Error())
		return res
	}
	res.Result = tr
	return res
}

// StorePassword saves a password for a credential name (used by the first-run guide).
func (a *App) StorePassword(credentialName, password string) error {
	if strings.TrimSpace(credentialName) == "" || password == "" {
		return errors.New("name and password are required")
	}
	return a.secrets().Set(secret.CredentialPrefix+credentialName, password)
}
