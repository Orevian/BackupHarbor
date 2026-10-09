//go:build windows

// Command backupharbor is the BackupHarbor desktop application (Wails + WebView2).
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows/registry"

	"backupharbor/frontend"
	"backupharbor/internal/config"
	"backupharbor/internal/gui"
	"backupharbor/internal/i18n"
)

// uiLanguage and uiTheme are read once to localise native dialogs and avoid a
// white flash before the web content paints.
func uiPreferences() (lang string, dark bool) {
	lang, theme := i18n.Detect(), "system"
	if cfg, err := config.Load(config.ResolvePath("")); err == nil {
		lang, theme = i18n.Normalize(cfg.Language), cfg.Theme
	}
	switch theme {
	case "dark":
		return lang, true
	case "light":
		return lang, false
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return lang, false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return lang, err == nil && v == 0
}

func filterFor(pattern string) []wr.FileFilter {
	if pattern == "" || pattern == "*" {
		return nil
	}
	return []wr.FileFilter{{DisplayName: pattern, Pattern: pattern}}
}

func main() {
	lang, dark := uiPreferences()
	a := gui.New()
	startup, shutdown := gui.Lifecycle(a)
	var appCtx context.Context

	bg := &options.RGBA{R: 243, G: 245, B: 248, A: 255}
	if dark {
		bg = &options.RGBA{R: 14, G: 19, B: 25, A: 255}
	}
	userData := filepath.Join(config.DataDir(), "webview")
	_ = os.MkdirAll(userData, 0o755)

	err := wails.Run(&options.App{
		Title:              "BackupHarbor",
		Width:              1240,
		Height:             820,
		MinWidth:           900,
		MinHeight:          620,
		BackgroundColour:   bg,
		AssetServer:        &assetserver.Options{Assets: frontend.Assets},
		Bind:               []any{a},
		LogLevel:           logger.ERROR,
		LogLevelProduction: logger.ERROR,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "backupharbor-5c1d6a3e-2f4b-4a77-9e0c-7d3b1f8a9c42",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if appCtx != nil {
					wr.WindowUnminimise(appCtx)
					wr.Show(appCtx)
				}
			},
		},
		OnStartup: func(ctx context.Context) {
			appCtx = ctx
			startup(ctx)
			a.Emit = func(event string, data any) { wr.EventsEmit(ctx, event, data) }
			a.PickFile = func(title, filter string) (string, error) {
				return wr.OpenFileDialog(ctx, wr.OpenDialogOptions{Title: title, Filters: filterFor(filter)})
			}
			a.PickDir = func(title string) (string, error) {
				return wr.OpenDirectoryDialog(ctx, wr.OpenDialogOptions{Title: title, CanCreateDirectories: true})
			}
			a.SaveFile = func(title, name, filter string) (string, error) {
				return wr.SaveFileDialog(ctx, wr.SaveDialogOptions{Title: title, DefaultFilename: name, Filters: filterFor(filter)})
			}
		},
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			if !a.IsBusy() {
				return false
			}
			yes, no := i18n.T(lang, "ui.quit_yes"), i18n.T(lang, "ui.quit_no")
			choice, err := wr.MessageDialog(ctx, wr.MessageDialogOptions{
				Type: wr.QuestionDialog, Title: "BackupHarbor", Message: i18n.T(lang, "ui.quit_running"),
				Buttons: []string{yes, no}, DefaultButton: no, CancelButton: no,
			})
			return err != nil || !strings.EqualFold(choice, yes)
		},
		OnShutdown: func(context.Context) { shutdown() },
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.SystemDefault,
			WebviewUserDataPath:  userData,
			DisablePinchZoom:     true,
			Messages: &windows.Messages{
				Webview2NotInstalled: i18n.T(lang, "ui.wv2_missing"),
				ContactAdmin:         i18n.T(lang, "ui.wv2_help"),
				Error:                i18n.T(lang, "ui.wv2_title"),
			},
		},
	})
	if err != nil {
		os.Exit(1)
	}
}
