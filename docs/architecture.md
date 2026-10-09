# Architecture

BackupHarbor is one verification engine with several front ends. The CLI, the desktop
application and the scheduled task all call the same code; there is no second implementation.

```
 bharbor.exe ─────┐                                       ┌──────────────┐
 (cmd/bharbor)    │                                       │ restic.exe   │
                 ▼                                       │ (external)   │
        internal/cli ──┐                                 └──────▲───────┘
                       ├──► internal/app ──► internal/verify ───┤ internal/restic
 BackupHarbor.exe│     │        │   │            │   │          (no shell, env-only secrets,
 (cmd/backupharbor)    │        │   │            │   │           process-tree kill)
        internal/gui ──┘        │   │            │   └──► internal/workspace ──► internal/fsx
        ▲ (Wails bindings)      │   │            │         (ownership marker, link-safe delete)
        │                       │   │            └──► checks (JSON/XML/ZIP/PDF/SQLite/SHA-256)
 frontend/ (HTML/CSS/JS)        │   └──► internal/report (history store, JSON/HTML/TXT)
                                └──► internal/config, internal/secret, internal/schedule, internal/i18n
```

## Packages and responsibilities

| Package | Responsibility |
|---|---|
| `internal/verify` | The engine. Runs stages per target, classifies results, aggregates statuses and exit codes. The only place that decides passed/failed. |
| `internal/restic` | Starts restic directly, passes repository and password via environment, limits output sizes, parses JSON, classifies exit codes (10 no repository, 11 locked, 12 wrong password), kills the whole process tree on cancel. |
| `internal/config` | YAML schema (strict: unknown keys are errors), defaults, validation, relative path resolution, atomic save. |
| `internal/secret` | Password sources (Credential Manager, environment, file) and masking of secrets. |
| `internal/workspace` | Creates private work areas with an ownership marker; cleanup is refused unless ownership is proven. Removes abandoned areas older than 48 hours. |
| `internal/fsx` | No-clobber and atomic file writes, reparse-point detection, containment checks, deletion that never follows links, ACL reset for restored directories. |
| `internal/report` | Immutable run records, index for fast listing, JSON/HTML/TXT renderers, no-overwrite export. |
| `internal/schedule` | One scheduled task in its own `\BackupHarbor` folder, identified by an ownership marker; created from XML via `schtasks`, read back via PowerShell. |
| `internal/app` | Composition used by every front end: load configuration, run the engine, save the result, validate. |
| `internal/cli` | The `bharbor` commands, exit codes, colours. |
| `internal/gui` | Methods bound to the web UI; events for live progress; demo mode. Does not import Wails, so it is unit-testable. |
| `internal/demo` | Sample data. A test asserts that the package cannot import anything able to reach a repository. |
| `internal/i18n` | English and Turkish catalogs; messages carry a code plus arguments so stored reports can be rendered in either language. |

## Data flow of a run

1. A front end builds `app.Options` (targets, trigger) and calls `app.Execute`.
2. `Execute` validates the configuration structure, checks that the report directory is writable (so a result is never lost silently), creates the engine (locating restic) and runs the targets with the configured concurrency.
3. For each target the engine runs the stages in order: `restic`, `directories`, `password`, `repository`, `snapshot`, `check`, `restore`, `content`, `cleanup`. A failed prerequisite marks later stages `not_run`; cleanup always runs, even after cancellation or timeout, using a context that cannot be cancelled.
4. Events (stage start/end, restore progress, log lines) stream to the front end. Restore progress comes from restic's real counters; no percentage is invented.
5. The finished `Run` is written as an immutable JSON file (never overwritten), optionally also HTML/TXT, then appended to the history index.
6. The CLI maps the run to an exit code; the GUI displays it.

## Status model

`passed` means the check ran and succeeded. Everything else means the claim is **not** established.
A target is `passed` only if nothing failed, nothing could not be run, and at least one proof stage
(`check`, `restore`, `content`) actually passed. Precedence for a target:
cancelled, timed out, failed, partial, not run (partial when some proof stage passed), passed.
A failed cleanup downgrades an otherwise passed target to `partial`: the backup is restorable but restored data may remain on disk.

For a run, exit codes are: 3 if anything was cancelled or timed out; 1 if any target failed or is partial;
2 if any target could not be tested; otherwise 0. (A proven problem outranks "could not test", so automation
never mistakes a broken backup for a tooling problem.)

## Decisions and trade-offs

- **Wails without the Wails CLI.** The desktop app is built with plain `go build` plus tags; icon, manifest and version information are embedded with `windres`. This keeps the toolchain small. The `wv2runtime.error` tag makes a missing WebView2 Runtime an error dialog instead of Wails' default of downloading the bootstrapper.
- **restic is shipped with the application.** The build downloads the official release named in `third_party/restic/PIN`, checks its SHA-256 and version, and places `restic.exe` next to the executables. BackupHarbor never downloads at run time. Lookup order: configured path, bundled copy, `PATH`.
- **No frontend build tooling.** The UI is static HTML/CSS/JS embedded in the binary, with a strict Content-Security-Policy-friendly style (no inline scripts or styles).
- **Read-only repository access.** `--no-lock` is passed to restic unless `use_repo_lock: true`, so verification never writes lock files into the repository.
- **`restore --verify`.** Restored files are re-read and compared against repository hashes. It costs time but proves that what reached the disk matches the backup.
- **Metadata errors are separate.** Restoring the parent folders of a Windows path can fail to set timestamps/ACLs (for example below `C:\Users`). Such errors are counted and reported but do not fail a restore whose content was written; any other restore error makes the result `partial` or `failed`.
- **Scheduled task uses an interactive token.** Credential Manager entries need the user's profile; the task therefore runs only while the user is signed in. It is created from an XML definition and read back; installation failure is never shown as success.
- **Report index.** `index.jsonl` allows fast listing; it is rebuilt automatically from the run records if missing, damaged or stale.
- **SQLite via a pure-Go driver** (`modernc.org/sqlite`), opened read-only on the restored copy, so no CGO and no external DLL.
- **Single source of version.** The `VERSION` file feeds the build script; a test keeps `internal/version` in step.

## Extending to other backends

The engine depends on the small `verify.Backend` interface (`Version`, `CatConfig`, `Snapshots`, `Check`, `Stats`, `Restore`),
implemented by `restic.Client` and by fakes in tests. A second backend would implement that interface and extend
the configuration schema; nothing speculative is implemented today.
