# BackupHarbor

**Prove that your restic backups can really be restored.**

BackupHarbor is a standalone Windows application that takes your existing
[restic](https://restic.net) backups, restores them into a private work area,
checks the restored files, and reports exactly what was and was not proven.
It consists of a desktop application (`BackupHarbor.exe`) and a command-line
tool (`bharbor.exe`) that share one verification engine, one configuration file
and one report history.

> A backup that "succeeded" is not a backup you can restore. BackupHarbor never
> assumes success: only checks that actually ran and passed are reported as passed.

[Turkce README](README.tr.md)

## The problem

Backup tools report that they *wrote* data. They do not tell you that the data
can be read back, decrypted, and used. Silent repository damage, a lost
password, a missing snapshot or a database that was copied mid-write are only
discovered on the worst possible day. Testing restores by hand is tedious, so it
is usually skipped.

## What BackupHarbor does

For every configured repository ("target") it runs real restic commands:

1. **Preflight** - finds restic and checks its version, checks the password source, opens the repository, selects the snapshot, checks the work directory.
2. **Integrity check** - `restic check`, optionally reading all or a sample of the stored data.
3. **Restore** - restores the snapshot into a private work area it created itself, with `restic restore --verify` (restored files are read back and compared with the repository hashes). Duration, file count and size are measured.
4. **Content rules** - checks you define on restored files: exists, not empty, readable text, valid JSON/XML, ZIP opens with CRC check, PDF structure, SQLite `integrity_check`, SHA-256.
5. **Cleanup** - removes the work area, but only after proving it created it.
6. **Report** - JSON, HTML and TXT, with an explicit statement of what the result does and does not prove.

Each stage ends as `passed`, `failed`, `not_run`, `cancelled`, `timed_out`, `partial` (or `skipped` if you disabled it).
A backup that could not be tested is never counted as passed.

## Features

- Desktop app with overview, live verification progress, target editor, report browser, schedule and settings; light, dark and system theme; English and Turkish.
- Command-line tool for scripts and servers: colour with `NO_COLOR` support, `--json` output, stable exit codes.
- Windows Task Scheduler integration managed by the app (only its own task is ever touched).
- Passwords are never stored in the configuration: Windows Credential Manager, an environment variable or a password file.
- Demo mode with clearly marked sample data that never touches a repository.
- No telemetry, no analytics, no auto-update, no hidden downloads.

## Requirements

- Windows 10 or 11, x64.
- [restic](https://restic.net) 0.16 or newer. The installer and the portable ZIP already include an unmodified restic (version in `third_party/restic/PIN`), so nothing else needs to be installed; you may choose a different restic in Settings.
- For the desktop app: the Microsoft Edge WebView2 Runtime (preinstalled on current Windows 11 and most Windows 10 systems). The command-line tool does not need it.

## Installation

BackupHarbor is built from source; no release has been published yet.
When releases exist they will contain an installer (`BackupHarbor-<version>-setup.exe`), a portable folder as ZIP and a `SHA256SUMS.txt`. Both ship `restic.exe` next to the application, so they work without installing anything else (the desktop app additionally needs the WebView2 Runtime).

Build it yourself (needs Go 1.26+ and MinGW `windres`; Inno Setup 6 optional for the installer):

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build.ps1
```

The script formats, vets and tests the code, embeds icon and version information,
downloads the pinned official restic release (SHA-256 verified against `third_party/restic/PIN`; `-ResticZip` builds offline) and builds both executables into `dist\BackupHarbor-<version>-win-x64\` (an *onedir*
layout, not a single self-extracting file), creates the ZIP, builds the installer
if Inno Setup is installed, and writes SHA-256 checksums. See [CONTRIBUTING.md](CONTRIBUTING.md).

The installer offers Program Files installation, a Start menu shortcut, an optional
desktop shortcut and an optional PATH entry for `bharbor`. Uninstalling removes only
the PATH entry the installer itself added and keeps your configuration and reports
unless you explicitly agree to delete them.

## Quick start

Command line:

```powershell
bharbor init                      # writes an example configuration (never overwrites)
bharbor secret set my-backup      # stores the repository password in Windows Credential Manager
notepad $env:APPDATA\BackupHarbor\config.yaml
bharbor validate                  # checks everything except the restore
bharbor run                       # verifies all enabled targets
bharbor report --last             # shows the latest report
```

Desktop app: start `BackupHarbor`, follow the setup guide on the overview page
(restic, configuration, first target), press **Test now**. Use **Demo mode** to look
around without any backup.

## Configuration

A YAML file at `%APPDATA%\BackupHarbor\config.yaml` (override with `--config` or
`BACKUPHARBOR_CONFIG`). Relative paths in the file are resolved against the
directory containing the file. Unknown keys are rejected. Minimal example:

```yaml
version: 1
targets:
  - name: documents
    repository: 'D:\Backups\restic'
    password: {source: credential}      # credential | env | file
    rules:
      - {name: accounts, path: '**/accounts.sqlite', type: sqlite}
```

Full reference: [docs/configuration.md](docs/configuration.md).

## Command-line reference

```
bharbor init [--force]
bharbor validate [--target NAME]...
bharbor run [--target NAME]...
bharbor report [--last | --list | --run ID] [--target NAME] [--format json|html|txt] [--output FILE] [--force]
bharbor schedule install|status|remove [--frequency daily|weekly] [--time HH:MM] [--day monday..sunday]
bharbor secret set|delete|status NAME [--stdin]
global: --config FILE  --json  --no-color  --lang en|tr  --verbose  --quiet  --version  --help
```

| Exit code | Meaning |
|---|---|
| 0 | Every target passed. |
| 1 | Verification ran and at least one target failed or was only partly proven. |
| 2 | Configuration, dependency or execution error, **or a target could not be tested**. |
| 3 | Cancelled or timed out. |

Precedence when results are mixed: 3, then 1, then 2, then 0. A cancelled run is never reported as a broken backup.

## Reports

Every run is stored as an immutable JSON record (schema `backupharbor.run/1`, see
[docs/report-schema.md](docs/report-schema.md)) in `%LOCALAPPDATA%\BackupHarbor\reports`.
HTML (single self-contained printable file) and TXT can be exported at any time or
saved automatically. Existing files are never overwritten silently. Both
front ends read the same history.

## Security and privacy

- The backup repository is only read. Restic repository locks are off by default so nothing is written to it.
- Restores go to a work area BackupHarbor creates; cleanup requires an ownership marker, never follows symlinks or junctions, and refuses to run when ownership cannot be proven.
- Passwords never appear on a command line, in logs, reports or error messages; known secrets and credential patterns are masked.
- Subprocesses are started directly (no shell) with separate arguments and are terminated as a process tree on cancellation or timeout.
- No telemetry, no network requests of its own.

Details and the threat model: [docs/security.md](docs/security.md) and [SECURITY.md](SECURITY.md).

## Tests

```powershell
go test ./...
```

Includes tests against a real restic repository when restic is available
(`BH_TEST_RESTIC` or `restic` on PATH); without restic those tests are reported as
skipped, not passed. `BH_TEST_SCHEDULER=1` additionally exercises the real Task
Scheduler. A browser-driven UI smoke test lives in `tools/uidev` (see
[CONTRIBUTING.md](CONTRIBUTING.md)).

## Known limitations

- Windows 10/11 x64 only. restic is the only supported backend.
- The scheduled task runs only while the user is signed in (interactive token), because Windows Credential Manager access requires the user's profile.
- Remote repositories need their credentials (for example `AWS_*`) in the environment of the user that runs BackupHarbor, including the scheduled task.
- restic's `--password-command` is intentionally not supported.
- Restoring a Windows snapshot restores the full original path below the work area, starting with the drive letter as a folder; content rules therefore start with `**/`.
- Restic may report attribute errors (timestamps, ACLs) for parent folders when restoring; they are counted and reported separately and do not turn a restore with intact content into a failure.
- A file that parses or opens does not prove the application using it will work; PDF checks are structural only.
- The binaries are not code-signed, so Windows SmartScreen may warn on first start.
- Screenshots in `docs/screenshots` were captured from the real user interface and back end through a headless browser (see their README); they are not photographs of the native window.

## Screenshots

See [docs/screenshots](docs/screenshots/README.md).

## License

[MIT](LICENSE)
