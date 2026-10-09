# Changelog

All notable changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - not yet released

First version; it has not been published as a release.

### Added
- Verification engine over real restic commands: preflight, `restic check` (none, subset or all data), snapshot selection (latest, tag, ID), restore with `--verify` into a private work area, content rules (exists, not empty, text, JSON, XML, ZIP, PDF structure, SQLite integrity, SHA-256) and verified cleanup.
- Status model `passed`, `failed`, `not_run`, `cancelled`, `timed_out`, `partial` (plus `skipped` for disabled stages); untestable backups are never counted as passed.
- Command-line tool `bharbor`: `init`, `validate`, `run`, `report`, `schedule`, `secret`; JSON output, `NO_COLOR`, exit codes 0-3.
- Desktop application `BackupHarbor.exe` (Wails v2 / WebView2): overview, live verification, target editor with connection test, report browser and export, schedule, settings, first-run guide, demo mode; English and Turkish; light, dark and system themes.
- Reports in JSON (schema `backupharbor.run/1`), self-contained HTML and TXT; shared history for the GUI and the CLI.
- Windows Task Scheduler integration limited to its own task.
- Passwords via Windows Credential Manager, environment variable or file; masking of secrets everywhere.
- restic is shipped with the installer and the ZIP: the build downloads the pinned official release (`third_party/restic/PIN`), verifies its SHA-256 and version and places `restic.exe` next to the executables; lookup order is configured path, bundled copy, `PATH`. Licence notices in `THIRD-PARTY-NOTICES.md`.
- Windows distribution (onedir), Inno Setup installer (English and Turkish), PowerShell build script, SHA-256 checksums.
- GitHub Actions workflows for CI and tagged releases.
