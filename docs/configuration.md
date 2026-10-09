# Configuration reference

The configuration is a YAML file, schema version 1.

Location, in order of precedence: `--config FILE`, the `BACKUPHARBOR_CONFIG` environment variable,
`%APPDATA%\BackupHarbor\config.yaml`. `bharbor init` creates an example and never overwrites an existing file
(`--force` keeps a timestamped backup). The desktop app writes the same file; comments are not preserved when the
app saves it.

**Strict parsing.** Unknown keys are errors. There is no key for a plain-text password.

**Relative paths** (`restic.path`, `temp_dir`, `reports.dir`, `password.file`, a local `repository`) are resolved against the
directory that contains the configuration file. Environment variables written as `$VAR` or `${VAR}` (for example `${LOCALAPPDATA}`) are expanded in these values; `%VAR%` is not.

Default data locations: reports `%LOCALAPPDATA%\BackupHarbor\reports`, work area `%LOCALAPPDATA%\BackupHarbor\work`,
log `%LOCALAPPDATA%\BackupHarbor\logs\backupharbor.log` (rotated at 5 MiB).

## Top level

| Key | Default | Meaning |
|---|---|---|
| `version` | `1` | Schema version. Other values are rejected. |
| `language` | `auto` | `auto`, `en` or `tr`. `auto` follows the Windows user locale. |
| `theme` | `system` | `system`, `light` or `dark` (desktop app). |
| `restic.path` | empty | Full path of `restic.exe`. Empty uses the `restic.exe` shipped next to the application, then the first restic on `PATH`. |
| `temp_dir` | `%LOCALAPPDATA%\BackupHarbor\work` | Parent of the private restore work areas. Must be absolute after resolution, not a drive root, not a system or personal folder (profile, Documents, Desktop, Downloads, Windows, Program Files...), and must not contain or be inside a local repository. |
| `concurrency` | `1` | Targets verified in parallel, 1-8. |
| `timeout` | `2h` | Time limit per target, `1m` to `48h`. |
| `log_level` | `info` | `debug`, `info`, `warn`, `error`. |
| `reports.dir` | default above | Report history directory. |
| `reports.formats` | `[]` | Extra files written after each run: `html`, `txt`. The JSON record is always written. |
| `schedule.frequency` | `daily` | `daily` or `weekly`; used by `bharbor schedule install` and the app. |
| `schedule.time` | `03:00` | 24-hour `HH:MM`. |
| `schedule.day` | `sunday` for weekly | `monday` ... `sunday`. |
| `targets` | `[]` | List of targets. |

## Target

| Key | Default | Meaning |
|---|---|---|
| `name` | required | Unique (case-insensitive); letters, digits, `.`, `_`, `-`; max 64 characters. |
| `enabled` | `true` | Disabled targets are skipped unless named explicitly with `--target`. |
| `repository` | required | A local folder or any restic repository address (`sftp:`, `s3:`, `rest:`, ...). Credentials embedded in URLs are masked in output. Passed to restic through the environment, not the command line. |
| `password.source` | `credential` | `credential`, `env` or `file`. |
| `password.credential` | target name | Entry `BackupHarbor/<name>` in Windows Credential Manager. Set with `bharbor secret set` or the app. |
| `password.env` | - | Name of an environment variable holding the password. |
| `password.file` | - | Path of a password file (max 4 KiB, trailing newline and BOM removed). |
| `snapshot.select` | `latest` | `latest`, `tag` or `id`. |
| `snapshot.tags` | - | For `tag`: the snapshot must carry **all** tags. |
| `snapshot.id` | - | For `id`: 8-64 hex characters; ambiguous prefixes are rejected, never guessed. |
| `snapshot.host` | - | Optional host filter for `latest` and `tag`. |
| `timeout` | global | Per-target override. |
| `use_repo_lock` | `false` | `false` passes `--no-lock`, so nothing is written to the repository. |
| `check.enabled` | `true` | Run `restic check`. |
| `check.read_data` | `none` | `none` (structure and metadata only - fast, does **not** read data blocks), `subset` (needs `check.subset`), `all` (reads and verifies every data block - slow, the only check that proves the data itself). |
| `check.subset` | - | `n/t`, a percentage like `5%`, or a size like `500M`. |
| `restore.enabled` | `true` | Restore into the private work area. |
| `restore.verify` | `true` | `restic restore --verify`. |
| `restore.min_files` | `1` | Fewer restored files than this is a failure (an empty restore proves nothing). |
| `restore.include` | - | restic include patterns; only that part is restored (and tested). The report says so. |
| `rules` | - | Content rules, see below. Require `restore.enabled`. |

At least one of `check.enabled` and `restore.enabled` must be on.

Before restoring a whole snapshot, BackupHarbor asks restic for the restore size and refuses to start (`not_run`) if it would not fit into the free space of the work volume.

## Content rules

Evaluated on the restored files. The restore root contains the original path, with the drive letter as a folder
(`C:\Users\me\file.txt` becomes `C\Users\me\file.txt`), so patterns normally start with `**/`.
Globs use `/` or `\`, `*`, `?`, `[a-z]`, and `**` for any number of folders; matching is case-insensitive.

| Key | Default | Meaning |
|---|---|---|
| `name` | pattern | Label in reports. |
| `path` | required | Glob relative to the restore root. No absolute paths, no `..`. |
| `type` | required | See below. |
| `sha256` | - | Required for `type: sha256` (64 hex characters), invalid otherwise. |
| `required` | `true` | `true`: no matching file fails the rule; `false`: the rule is skipped. |
| `max_files` | `5000` | Files inspected per rule; if more match, the rule is `partial` and says so. |

| `type` | What it proves - and what it does not |
|---|---|
| `exists` | At least one matching file was restored. |
| `non_empty` | The file has content. |
| `text` | Valid UTF-8 (or UTF-16 with BOM) without NUL bytes. Not that the text is meaningful. |
| `json` | A single, complete, syntactically valid JSON document, parsed as a stream. |
| `xml` | Well-formed XML (no schema validation). |
| `zip` | Central directory readable and every entry decompresses with a matching CRC-32 (decompression capped at 2 GiB per archive, then `partial`). |
| `pdf` | Header, `%%EOF`, and that `startxref` points to a cross-reference table or stream. Structural only; the document is not rendered. |
| `sqlite` | SQLite header, then `PRAGMA integrity_check` on the restored copy, opened read-only. Does not prove application-level consistency. |
| `sha256` | The file's SHA-256 equals the given value. |

A file that opens or parses is not proof that the application using it will work.

## Example

```yaml
version: 1
language: auto
restic:
  path: 'C:\Tools\restic\restic.exe'
concurrency: 2
timeout: 3h
reports:
  formats: [html]

targets:
  - name: documents-daily
    repository: 'D:\Backups\documents'
    password: {source: credential}
    snapshot: {select: tag, tags: [daily]}
    check: {read_data: subset, subset: 5%}
    rules:
      - {name: accounting, path: '**/accounts.sqlite', type: sqlite}
      - {name: invoices,   path: '**/Invoices/*.pdf',  type: pdf, max_files: 200}
      - {name: settings,   path: '**/settings/*.json', type: json}

  - name: nas-offsite
    repository: 'sftp:backup@nas.example.org:/srv/restic'
    password: {source: env, env: RESTIC_PASSWORD_NAS}
    check: {read_data: none}
    restore:
      include: ['/C/Users/me/Documents/Contracts']
```
