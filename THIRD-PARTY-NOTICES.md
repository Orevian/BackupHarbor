# Third-party notices

BackupHarbor is MIT licensed (see [LICENSE](LICENSE)). The distribution contains, or is built from, the following
third-party software.

## restic (shipped as `restic.exe`)

The Windows installer and the portable ZIP include the unmodified official `restic.exe`, which BackupHarbor runs as a
separate process to perform all backup-repository operations.

- Version and download: pinned in [third_party/restic/PIN](third_party/restic/PIN); the build script downloads the official
  release archive from github.com/restic/restic and refuses to continue unless its SHA-256 matches the pin.
- Project: https://restic.net
- Licence: BSD 2-Clause, see [third_party/restic/LICENSE](third_party/restic/LICENSE) (shipped as `licenses\restic-LICENSE.txt`).

You may point BackupHarbor at a different restic in Settings; the bundled copy is only the default.

## Go libraries compiled into the executables

| Library | Licence |
|---|---|
| github.com/wailsapp/wails/v2 and its dependencies (desktop shell) | MIT |
| modernc.org/sqlite and its dependencies (SQLite integrity checks) | BSD-3-Clause |
| gopkg.in/yaml.v3 | MIT and Apache-2.0 |
| golang.org/x/sys, golang.org/x/text | BSD-3-Clause |
| Go standard library | BSD-3-Clause |

The exact module versions are recorded in `go.mod` and `go.sum`. The desktop application also uses the Microsoft Edge
WebView2 Runtime installed on the machine; it is not redistributed by BackupHarbor.
