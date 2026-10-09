# Screenshots

These images are real captures of BackupHarbor 0.1.0, not mock-ups:

| File | What it is |
|---|---|
| `00-native-window.png` | The real `BackupHarbor.exe` window (Wails/WebView2) on Windows 11, showing a verification recorded earlier by `bharbor run` (the desktop app and the CLI share one report history). |
| `01` to `16` | The same web user interface, driven by `tools/uidev/smoke.mjs`: real front-end JavaScript, the real Go back end and a real restic repository, rendered by headless Microsoft Edge (the same engine as WebView2). Screens: overview, a finished verification, a report, targets and the editor, schedule, settings (English and Turkish, light and dark), demo mode, and a 900 px window at 150 % scaling. |

Why the second group does not come from the native window: WebView2 inside Wails does not expose a debugging
port, so the window cannot be scripted. The headless browser shows exactly the same HTML, CSS and JavaScript
against the same back-end code, but it is not a photograph of the native window frame.

The demo screens (`13` to `15`) show sample data and carry the demonstration banner. All paths and the host name
in the images come from a throw-away test repository on a neutral drive letter.

To regenerate them see [CONTRIBUTING.md](../../CONTRIBUTING.md); use a `SUBST` drive via `LAB_ROOT` to avoid
personal paths in the images.
