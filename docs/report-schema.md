# Report schema `backupharbor.run/1`

Every run is stored as one JSON document, `<run-id>.json`, in the report directory under `runs\`. The document is
never modified after it has been written. The schema identifier changes (`/2`) if a field is removed or its meaning changes;
fields may be added without changing it.

Times are RFC 3339 in UTC; durations are milliseconds; sizes are bytes.

```jsonc
{
  "schema": "backupharbor.run/1",
  "run_id": "20261009T183133Z-e5fc0a",   // sortable, unique
  "app_version": "0.1.0",
  "restic_version": "0.19.1",
  "trigger": "cli",                       // cli | gui | schedule
  "demo": false,                          // true only for demonstration data, which is never stored
  "started_at": "2026-10-09T18:31:33Z",
  "finished_at": "2026-10-09T18:31:37Z",
  "duration_ms": 3600,
  "status": "passed",                     // worst status of all targets
  "summary": { "total": 1, "passed": 1, "failed": 0, "not_run": 0, "cancelled": 0, "timed_out": 0, "partial": 0 },
  "targets": [
    {
      "name": "documents",
      "status": "passed",
      "started_at": "...", "finished_at": "...", "duration_ms": 3500,
      "repository": "D:\\Backups\\restic",          // credentials removed
      "snapshot": { "id": "...", "short_id": "...", "time": "...", "hostname": "...", "username": "...", "paths": ["..."], "tags": ["..."] },
      "restore": {
        "duration_ms": 729, "files": 1, "directories": 12, "bytes": 19,
        "links_skipped": 0, "metadata_warnings": 1, "content_errors": 0,
        "verified": true, "bytes_per_second": 26000
      },
      "stages": [
        {
          "id": "restore",                          // restic | directories | password | repository | snapshot | check | restore | content | cleanup
          "status": "passed",
          "started_at": "...", "duration_ms": 729,
          "message": { "code": "restore.ok", "args": ["1", "19 B", "729ms"], "text": "Restored 1 files (19 B) in 729ms." },
          "detail": "technical output, secrets masked (only present when useful)",
          "evidence": { "files": 1, "bytes": 19 },  // stage specific facts
          "rules": [                                // content stage only
            {
              "name": "accounts", "type": "sqlite", "pattern": "**/accounts.sqlite", "status": "passed",
              "matched": 1, "checked": 1, "passed": 1, "failed": 0, "duration_ms": 12,
              "message": { "code": "rule.passed", "args": ["1"], "text": "1 files passed." },
              "failures": [ { "path": "C/...", "message": { "code": "...", "args": [], "text": "..." } } ]   // at most 50 per rule
            }
          ]
        }
      ],
      "coverage": [ { "code": "cov.check_structure", "args": [], "text": "..." } ]   // what the result does and does not prove
    }
  ]
}
```

## Statuses

| Value | Meaning |
|---|---|
| `passed` | The check ran and succeeded. |
| `failed` | The check ran and found a problem. |
| `not_run` | The check could not be performed (for example wrong password, repository unreachable, not enough disk space). Not a success. |
| `cancelled` | The user or the system cancelled the operation. Not a finding about the backup. |
| `timed_out` | The time limit was reached. |
| `partial` | Part succeeded, part did not (for example a restore with content errors, a sampled rule, or a failed cleanup). |
| `skipped` | Disabled in the configuration; proves nothing. |

## Messages

`message` objects carry a catalog `code`, string `args` and the English `text`. Renderers translate by `code` and
fall back to `text`, so a report written today stays readable in either language.

## Reading reports

`bharbor report --run ID --format json` prints a stored record; `bharbor report --list --json` prints the index entries
(`run_id`, `started_at`, `status`, `counts`, per-target summaries). `index.jsonl` is a derived cache and is rebuilt from the
run files when missing or stale.
