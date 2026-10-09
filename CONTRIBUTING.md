# Contributing to BackupHarbor

Thanks for helping. BackupHarbor's one promise is *"only what was actually proven is reported as passed"*;
every change should protect that.

## Development setup

Requirements: Windows 10/11 x64, Go (version in `go.mod`), Git. To build the executables with icon and version
information you also need MinGW `windres` (for example from MSYS2) and, for the installer, Inno Setup 6.
The build script downloads and bundles the restic release pinned in `third_party/restic/PIN` (checksum verified; `-ResticZip <file>` builds offline). For the integration tests use that restic or any other via `BH_TEST_RESTIC`.

```powershell
git clone <your fork>
cd BackupHarbor
go build ./...
go test ./...                    # integration tests are skipped (and reported as skipped) without restic
```

Useful environment variables:

| Variable | Effect |
|---|---|
| `BH_TEST_RESTIC` | Path of a restic executable used by integration tests. |
| `BH_TEST_SCHEDULER=1` | Also runs the test that creates and deletes a real Task Scheduler task. |

## Layout

```
cmd/bharbor         command-line entry point
cmd/backupharbor   desktop application entry point (Wails)
frontend/          static web UI (HTML, CSS, JavaScript; no build step)
internal/verify    verification engine (the only place that decides pass or fail)
internal/restic    runs restic and parses its output
internal/config    configuration schema, loading, validation
internal/report    history store, JSON/HTML/TXT rendering
internal/schedule  Windows Task Scheduler integration
internal/secret    password sources, Credential Manager, masking
internal/workspace safe scratch areas and cleanup
internal/fsx       atomic writes, link-safe deletion, path checks
internal/app       composition shared by the CLI and the GUI
internal/cli       bharbor commands
internal/gui       desktop back end bound to the web UI
internal/demo      sample data for demo mode (cannot reach repositories)
internal/i18n      English and Turkish catalogs
tools/uidev        development-only UI test bridge
scripts/           build.ps1, make-icon.ps1
installer/         Inno Setup script
docs/              architecture, configuration, security, report schema
```

See [docs/architecture.md](docs/architecture.md) for the design and data flow.

## Rules for changes

- **Never turn "not tested" into "passed".** New checks must report `not_run` when they cannot run.
- No telemetry, no network access of BackupHarbor's own, no hidden downloads, no auto-update.
- Subprocesses: no shell, separate arguments, secrets via environment only. Add tests for new restic calls.
- Anything that deletes files must go through `internal/workspace` and `internal/fsx` (ownership proof, no link following).
- User-visible text goes into `internal/i18n/locales/*.json` in **both** languages. `go test ./internal/i18n` fails on missing keys or placeholder mismatches.
- Do not add features that are not wired to real behaviour, and do not add configuration keys the code does not honour.
- Keep `gofmt` clean and `go vet ./...` quiet. Add tests next to the code you change.

## UI changes

The UI is plain HTML/CSS/JS embedded into the binary. To try it with a normal browser and the real back end:

```powershell
go build -o uidev.exe ./tools/uidev
$env:BACKUPHARBOR_CONFIG = 'C:\path\to\config.yaml'
.\uidev.exe -addr 127.0.0.1:8765      # then open http://127.0.0.1:8765/
```

The automated smoke test drives that bridge with headless Edge, a real restic repository and the real engine:

```powershell
$env:BH_TEST_RESTIC = 'C:\tools\restic.exe'
$env:UIDEV_BIN = (Resolve-Path .\uidev.exe)
node tools\uidev\smoke.mjs        # set SHOTS_DIR to also save screenshots
```

Note that global names in the front end must not collide with Wails (`window.go`, `window.runtime`, `window.wails`).

## Pull requests

1. Fork, create a topic branch.
2. `powershell -File scripts\build.ps1 -SkipInstaller` must succeed (format, vet, tests, build).
3. Describe what changed and how you verified it; include test output for behaviour changes.
4. Update `CHANGELOG.md` under *Unreleased*.

## Reporting security problems

Please do not open a public issue; see [SECURITY.md](SECURITY.md).
