# Security design

This describes what BackupHarbor protects, how, and where the limits are.

## Protecting your data

| Risk | Protection |
|---|---|
| Modifying or deleting the backup repository | Only read-oriented restic commands are used (`version`, `cat config`, `snapshots`, `check`, `stats`, `restore`). `--no-lock` is passed by default so not even lock files are written (`use_repo_lock: true` opts in). BackupHarbor never runs `forget`, `prune`, `backup`, `rebuild-index` or `repair`. |
| Restored data mixed with user files | Every run restores into a new `run-...` directory below `temp_dir`, created with an ownership marker. `temp_dir` is validated: absolute, not a drive root, not a profile/system/personal folder, not overlapping a local repository. |
| Deleting the wrong directory | Cleanup deletes a directory only if it is a direct child of the configured scratch base, is not a link, carries a marker file written by BackupHarbor, and the marker's random nonce matches the one created in memory by this run. Otherwise cleanup is refused and reported (`cleanup` stage failed). Abandoned areas (crash, power loss) are removed only after 48 hours and only with a valid marker. |
| Symlinks and junctions | Deletion unlinks reparse points and never descends into them. The content walk skips them and counts them in the report. |
| Restored folders with restrictive ACLs | restic restores ACLs of parent folders (for example `C:\Users`) which can block deletion. Cleanup resets access on directories inside the verified work area only, then retries. |
| Path traversal in rules | Rule paths must be relative and may not contain `..`; matching works on paths relative to the work area. |
| Decompression bombs | ZIP checks stop after 2 GiB of decompressed data and report `partial`. |
| Huge or hostile process output | Output is read with hard limits (128 MiB for JSON documents, 1 MiB per streamed line, 32 KiB of retained diagnostics) and pipes are always drained so restic can never block on BackupHarbor. |
| Reports | Results are written to a temp file and then linked/renamed into place: a half-written report never looks complete. Existing files are never overwritten silently. HTML output uses `html/template` contextual escaping, has no scripts and no external resources. |

## Passwords and secrets

- The configuration has no field for a password; unknown keys are rejected.
- restic receives the repository and password through its **environment** (`RESTIC_REPOSITORY`, `RESTIC_PASSWORD`), never on the command line, so they do not appear in process listings. Ambient `RESTIC_PASSWORD_*` / `RESTIC_REPOSITORY*` variables of the parent are removed so they cannot redirect a run.
- Logs, reports, error messages and events pass through masking: known secret values, URL credentials (`scheme://user:pass@host`) and `NAME=value` pairs where the name contains PASSWORD, SECRET, TOKEN, ACCESS_KEY or API_KEY.
- Report and log text contains the repository location with credentials removed. Snapshot metadata (host name, user name, paths) is recorded because it identifies what was verified; share reports accordingly.

### Password sources compared

| Source | Protection | Caveats |
|---|---|---|
| Windows Credential Manager (default) | Encrypted with the user's DPAPI keys; not readable by other users or other machines. | Any process running as the same user can read the entry. Needs the user's profile, so the scheduled task runs only while the user is signed in. |
| Environment variable | Nothing is stored by BackupHarbor. | Visible to other processes of the same user (and to administrators); inherited by child processes; must be set for scheduled runs too. |
| Password file | Protected by NTFS permissions you set. | Plain text at rest. Restrict the file to your account; anyone who can read it can open the repository. |

## Subprocesses

restic is started with `exec.Command` and a separate argument list: no `cmd.exe`, no PowerShell, no string
concatenation of user input. On Windows each process is placed in a Job Object configured to kill the whole tree when
the job closes, so cancellation and timeouts also terminate helper programs such as `ssh` or `rclone`, and no
console window appears. The scheduled-task query runs PowerShell with a fixed script that contains no user input; the
task is created from an XML file whose values are XML-escaped, and the executable and configuration paths must not contain quotes or line breaks.

## Scheduled task

Only the task `\BackupHarbor\Verify` is managed. It carries a marker in its description; install, update and remove
refuse to touch a task of that name that lacks the marker. Installation is verified by reading the task back, and any
failure is returned as an error. The task runs with the user's interactive token and least privilege (never elevated).

## Privacy

BackupHarbor contains no telemetry or analytics, performs no update checks and downloads nothing. It makes no network
requests of its own. Network traffic only occurs inside restic when your repository is remote, and from the WebView2
Runtime displaying the local user interface (which only loads embedded files). Neither the application nor the installer downloads anything at run
time: a missing WebView2 Runtime is explained to the user, not fetched.

### The bundled restic

The installer and ZIP ship the unmodified official `restic.exe`. The build script downloads it from the official restic
release page and refuses to continue unless the archive matches the SHA-256 pinned in `third_party/restic/PIN` and the
binary reports the pinned version; the licence (BSD 2-Clause) is shipped in `licenses\`. At run time the search order is:
the path set in the configuration or Settings, then `restic.exe` next to the application, then `PATH`. Updating restic
means changing the pin in a reviewed commit and releasing a new BackupHarbor version.

## Limits - what BackupHarbor cannot protect against

- A malicious or compromised restic executable: the configured `restic.path` or the first `restic` on `PATH` is trusted. Prefer an absolute path in a protected folder.
- An attacker who already runs code as your Windows user.
- Information in repository metadata that restic itself exposes in output.
- The binaries are not code-signed; verify downloads with the published SHA-256 checksums.
