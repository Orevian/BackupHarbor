## BackupHarbor {{VERSION}}

<!-- Summarise the user-visible changes. Copy the matching section of CHANGELOG.md. -->

### Downloads

| File | Purpose |
|---|---|
| `BackupHarbor-{{VERSION}}-setup.exe` | Windows installer (English and Turkish) |
| `BackupHarbor-{{VERSION}}-win-x64.zip` | Portable folder with `BackupHarbor.exe` and `bharbor.exe` |
| `BackupHarbor-{{VERSION}}-SHA256SUMS.txt` | SHA-256 checksums of every file above |

Verify a download:

```powershell
(Get-FileHash .\BackupHarbor-{{VERSION}}-setup.exe -Algorithm SHA256).Hash.ToLower()
```

### Requirements

Windows 10/11 x64 and, for the desktop app, the Microsoft Edge WebView2 Runtime. The unmodified official [restic](https://restic.net) is included (version in `THIRD-PARTY-NOTICES.md`).

### Notes

The binaries are not code-signed; Windows SmartScreen may warn on first start. BackupHarbor sends no telemetry and does not update itself.
