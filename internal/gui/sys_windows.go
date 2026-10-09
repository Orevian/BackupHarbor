//go:build windows

package gui

import (
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// shellOpen opens a file or folder with its default handler.
func shellOpen(path string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// webView2Version reads the installed WebView2 runtime version from the registry.
func webView2Version() string {
	const guid = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	for _, k := range []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\` + guid},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + guid},
		{registry.CURRENT_USER, `Software\Microsoft\EdgeUpdate\Clients\` + guid},
	} {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := key.GetStringValue("pv")
		key.Close()
		if err == nil && v != "" {
			return v
		}
	}
	return ""
}
