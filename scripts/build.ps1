<#
.SYNOPSIS
  Builds, tests and packages BackupHarbor for Windows x64.

.DESCRIPTION
  Steps: tool check, gofmt, go vet, go test, icon, Windows resources, bharbor.exe,
  BackupHarbor.exe, onedir distribution folder, ZIP archive, optional Inno Setup
  installer and SHA-256 checksums. Any failing step stops the script with a
  non-zero exit code.

  Nothing outside the output folder is modified. An existing output folder is only
  replaced with -Clean and only if it carries the marker file this script writes,
  so user files and unrelated folders can never be deleted by mistake.

.PARAMETER Version
  Release version (default: contents of the VERSION file). A leading "v" is removed.
.PARAMETER Output
  Output root (default: <repo>\dist).
.PARAMETER SkipTests
  Skip "go test". Use only for quick local iterations; releases must run the tests.
.PARAMETER SkipInstaller
  Do not build the Inno Setup installer.
.PARAMETER ResticZip
  Use this local copy of the pinned restic archive instead of downloading it (still verified against the pinned SHA-256).
.PARAMETER Clean
  Replace a previous dist folder created by this script.
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\build.ps1
#>
[CmdletBinding()]
param(
    [string]$Version = '',
    [string]$Output = '',
    [switch]$SkipTests,
    [switch]$SkipInstaller,
    [switch]$Clean,
    [string]$ResticZip = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $Root
if (-not $Output) { $Output = Join-Path $Root 'dist' }
$Output = [System.IO.Path]::GetFullPath($Output)

$script:Summary = New-Object System.Collections.Generic.List[string]
function Step([string]$Name) { Write-Host ""; Write-Host "==> $Name" -ForegroundColor Cyan }
function Note([string]$Text) { $script:Summary.Add($Text) | Out-Null; Write-Host $Text }
function Fail([string]$Text) { Write-Host "ERROR: $Text" -ForegroundColor Red; exit 1 }
function Run([string]$Exe, [string[]]$Arguments) {
    & $Exe @Arguments
    if ($LASTEXITCODE -ne 0) { Fail "$Exe $($Arguments -join ' ') failed with exit code $LASTEXITCODE" }
}
function Find-Tool([string]$Name, [string[]]$Extra) {
    $c = Get-Command $Name -ErrorAction SilentlyContinue
    if ($c) { return $c.Source }
    foreach ($p in $Extra) { if ($p -and (Test-Path $p)) { return $p } }
    return $null
}

# ---- version ---------------------------------------------------------------
if (-not $Version) { $Version = (Get-Content (Join-Path $Root 'VERSION') -Raw).Trim() }
$Version = $Version.TrimStart('v')
if ($Version -notmatch '^\d+\.\d+\.\d+([-+][0-9A-Za-z.-]+)?$') { Fail "Invalid version '$Version' (expected MAJOR.MINOR.PATCH)" }
$core = ($Version -split '[-+]')[0]
$parts = $core.Split('.')
$Version4 = "$core.0"
$VersionComma = ($parts -join ',') + ',0'

# ---- tools -----------------------------------------------------------------
Step 'Checking tools'
$go = Find-Tool 'go' @()
if (-not $go) { Fail 'Go is not installed or not on PATH (https://go.dev/dl/).' }
$gofmt = Find-Tool 'gofmt' @((Join-Path (Split-Path $go) 'gofmt.exe'))
if (-not $gofmt) { Fail 'gofmt was not found next to go.exe.' }
$windres = Find-Tool 'windres' @('C:\msys64\ucrt64\bin\windres.exe', 'C:\msys64\mingw64\bin\windres.exe')
if (-not $windres) { Fail 'windres (MinGW binutils) is required to embed the icon, manifest and version information. Install MSYS2 mingw-w64 binutils or add windres to PATH.' }
$iscc = $null
if (-not $SkipInstaller) {
    $iscc = Find-Tool 'iscc' @("$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe", "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe", "$env:ProgramFiles\Inno Setup 6\ISCC.exe")
}
$goVersion = (& $go version)
Note "Go:       $goVersion"
Note "windres:  $windres"
if ($iscc) { Note "Inno:     $iscc" } elseif (-not $SkipInstaller) { Write-Warning 'Inno Setup (iscc) was not found: the installer will NOT be built.' ; Note 'Installer: NOT BUILT (Inno Setup not found)' }
$restic = Find-Tool 'restic' @($env:BH_TEST_RESTIC)
if ($restic) { Note "restic:   $restic (used for the integration tests)" } else { Write-Warning 'restic not found: integration tests will be skipped (not counted as passed).' ; Note 'Integration tests: SKIPPED (restic not found)' }
$env:CGO_ENABLED = '0'

# ---- format, vet, tests --------------------------------------------------------
Step 'gofmt'
$unformatted = & $gofmt -l cmd internal tools frontend
if ($LASTEXITCODE -ne 0) { Fail 'gofmt failed' }
if ($unformatted) { Fail ("These files are not gofmt-formatted:`n" + ($unformatted -join "`n")) }
Note 'gofmt: clean'

Step 'go vet'
Run $go @('vet', './...')
Note 'go vet: clean'

if ($SkipTests) { Write-Warning 'Tests skipped by request.'; Note 'go test: SKIPPED by -SkipTests' }
else {
    Step 'go test'
    $env:BH_TEST_SCHEDULER = ''
    Run $go @('test', './...', '-count=1')
    Note 'go test: passed'
}

# ---- output folder -----------------------------------------------------------------
$name = "BackupHarbor-$Version-win-x64"
$distDir = Join-Path $Output $name
# The ownership marker is a sibling file so that it is never part of the shipped folder.
$marker = Join-Path $Output ".$name.build-marker"
if (Test-Path $distDir) {
    if (-not $Clean) { Fail "$distDir already exists. Use -Clean to replace a previous build, or pick another -Output." }
    if (-not (Test-Path $marker)) { Fail "$distDir exists but was not created by this script (marker file missing); refusing to delete it." }
    Remove-Item -LiteralPath $distDir -Recurse -Force
}
New-Item -ItemType Directory -Path $distDir -Force | Out-Null
Set-Content -LiteralPath $marker -Value 'Created by scripts\build.ps1. The folder next to this file may be replaced with -Clean.' -Encoding ascii

# ---- icon and resources --------------------------------------------------------------
Step 'Icon and Windows resources'
$icon = Join-Path $Root 'build\windows\icon.ico'
if (-not (Test-Path $icon)) { & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $Root 'scripts\make-icon.ps1'); if ($LASTEXITCODE -ne 0) { Fail 'icon generation failed' } }
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("rp-build-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
$generated = @()

function New-Syso([string]$PkgDir, [string]$ManifestSrc, [string]$Description, [string]$Internal, [string]$FileName, [string]$Extra = '') {
    $manifest = Join-Path $tmp ([System.IO.Path]::GetFileName($ManifestSrc))
    (Get-Content -LiteralPath $ManifestSrc -Raw).Replace('@VERSION4@', $Version4) | Set-Content -LiteralPath $manifest -Encoding utf8
    $rc = (Get-Content -LiteralPath (Join-Path $Root 'build\windows\resource.rc.in') -Raw)
    $rc = $rc.Replace('@ICON@', ($icon -replace '\\', '/')).Replace('@MANIFEST@', ($manifest -replace '\\', '/')).Replace('@VERSION_COMMA@', $VersionComma).Replace('@VERSION@', $Version).Replace('@DESCRIPTION@', $Description).Replace('@INTERNAL@', $Internal).Replace('@FILENAME@', $FileName).Replace('@EXTRA@', $Extra)
    $rcFile = Join-Path $tmp "$Internal.rc"
    [System.IO.File]::WriteAllText($rcFile, $rc, (New-Object System.Text.UTF8Encoding $false))
    $syso = Join-Path $PkgDir 'rsrc_windows_amd64.syso'
    Run $windres @('-O', 'coff', '-F', 'pe-x86-64', '-i', $rcFile, '-o', $syso)
    return $syso
}

try {
    $generated += New-Syso (Join-Path $Root 'cmd\bharbor') (Join-Path $Root 'build\windows\cli.manifest') 'BackupHarbor command-line interface' 'bharbor' 'bharbor.exe'
    $generated += New-Syso (Join-Path $Root 'cmd\backupharbor') (Join-Path $Root 'build\windows\gui.manifest') 'BackupHarbor' 'BackupHarbor' 'BackupHarbor.exe' ('3 ICON "' + ($icon -replace '\\', '/') + '"')
    Note 'Resources: icon, manifest and version information embedded'

    # ---- build -------------------------------------------------------------------------
    $commit = 'unknown'
    $git = Find-Tool 'git' @()
    if ($git -and (Test-Path (Join-Path $Root '.git'))) { $c = (& $git rev-parse --short HEAD 2>$null); if ($LASTEXITCODE -eq 0 -and $c) { $commit = $c.Trim() } }
    $date = (Get-Date).ToUniversalTime().ToString('yyyy-MM-dd')
    $ld = "-s -w -X backupharbor/internal/version.Version=$Version -X backupharbor/internal/version.Commit=$commit -X backupharbor/internal/version.Date=$date"

    Step 'Building bharbor.exe (CLI)'
    Run $go @('build', '-trimpath', '-ldflags', $ld, '-o', (Join-Path $distDir 'bharbor.exe'), './cmd/bharbor')
    Step 'Building BackupHarbor.exe (GUI)'
    # wv2runtime.error: if the WebView2 Runtime is missing, show an error. Never download anything.
    Run $go @('build', '-trimpath', '-tags', 'desktop,production,wv2runtime.error', '-ldflags', "$ld -H windowsgui", '-o', (Join-Path $distDir 'BackupHarbor.exe'), './cmd/backupharbor')
}
finally {
    foreach ($g in $generated) { if ($g -and (Test-Path -LiteralPath $g)) { Remove-Item -LiteralPath $g -Force } }
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

# ---- smoke-check the binaries -------------------------------------------------------------
Step 'Checking the built binaries'
$cliVersion = (& (Join-Path $distDir 'bharbor.exe') --version)
if ($LASTEXITCODE -ne 0 -or $cliVersion -notmatch [regex]::Escape($Version)) { Fail "bharbor.exe --version printed '$cliVersion'" }
Note "bharbor.exe --version: $cliVersion"
$vi = (Get-Item (Join-Path $distDir 'BackupHarbor.exe')).VersionInfo
if ($vi.ProductName -ne 'BackupHarbor' -or $vi.ProductVersion -ne $Version) { Fail "BackupHarbor.exe version resource is wrong: $($vi.ProductName) $($vi.ProductVersion)" }
Note "BackupHarbor.exe resources: $($vi.ProductName) $($vi.ProductVersion), $($vi.FileDescription)"

# ---- distribution layout --------------------------------------------------------------------
Step 'Assembling the distribution folder'
Copy-Item (Join-Path $Root 'LICENSE') $distDir
Copy-Item (Join-Path $Root 'README.md') $distDir
Copy-Item (Join-Path $Root 'README.tr.md') $distDir
Copy-Item (Join-Path $Root 'CHANGELOG.md') $distDir
Copy-Item (Join-Path $Root 'SECURITY.md') $distDir
Copy-Item $icon (Join-Path $distDir 'icon.ico')
New-Item -ItemType Directory -Path (Join-Path $distDir 'docs') | Out-Null
foreach ($d in 'architecture.md', 'configuration.md', 'security.md', 'report-schema.md') {
    $p = Join-Path $Root "docs\$d"
    if (Test-Path $p) { Copy-Item $p (Join-Path $distDir 'docs') }
}
# The example configuration is produced by the CLI itself so it can never drift from the code.
$exampleCfg = Join-Path $distDir 'config.example.yaml'
& (Join-Path $distDir 'bharbor.exe') init --config $exampleCfg --no-color --lang en | Out-Null
if ($LASTEXITCODE -ne 0 -or -not (Test-Path $exampleCfg)) { Fail 'bharbor init could not create the example configuration' }

# ---- bundled restic ---------------------------------------------------------------------------
# The official restic release named in third_party\restic\PIN is downloaded at BUILD time, must match
# the pinned SHA-256 and report the pinned version. BackupHarbor itself never downloads anything.
Step 'Bundling restic (pinned, checksum-verified)'
$pin = @{}
Get-Content (Join-Path $Root 'third_party\restic\PIN') | Where-Object { $_ -match '^\s*[a-z0-9]+=' } | ForEach-Object { $k, $v = $_ -split '=', 2; $pin[$k.Trim()] = $v.Trim() }
foreach ($k in 'version', 'url', 'sha256') { if (-not $pin[$k]) { Fail "third_party\restic\PIN lacks '$k'" } }
$resticArchive = $ResticZip
if (-not $resticArchive) {
    $resticArchive = Join-Path ([System.IO.Path]::GetTempPath()) ("restic-" + $pin.version + "-" + [guid]::NewGuid().ToString('N') + ".zip")
    $ProgressPreference = 'SilentlyContinue'
    try { Invoke-WebRequest -UseBasicParsing -Uri $pin.url -OutFile $resticArchive } catch { Fail "Could not download restic from $($pin.url): $_ (use -ResticZip <file> to build offline)" }
}
$gotHash = (Get-FileHash -LiteralPath $resticArchive -Algorithm SHA256).Hash.ToLower()
if ($gotHash -ne $pin.sha256.ToLower()) { Fail "restic archive checksum mismatch: expected $($pin.sha256), got $gotHash" }
$resticTmp = Join-Path ([System.IO.Path]::GetTempPath()) ("bh-restic-" + [guid]::NewGuid().ToString("N"))
Expand-Archive -LiteralPath $resticArchive -DestinationPath $resticTmp
$resticExe = Get-ChildItem $resticTmp -Filter 'restic*.exe' | Select-Object -First 1
if (-not $resticExe) { Fail 'the restic archive does not contain restic.exe' }
Copy-Item -LiteralPath $resticExe.FullName -Destination (Join-Path $distDir 'restic.exe')
Remove-Item -LiteralPath $resticTmp -Recurse -Force -ErrorAction SilentlyContinue
if (-not $ResticZip) { Remove-Item -LiteralPath $resticArchive -Force -ErrorAction SilentlyContinue }
$resticOut = (& (Join-Path $distDir 'restic.exe') version)
if ($LASTEXITCODE -ne 0 -or $resticOut -notmatch ('restic ' + [regex]::Escape($pin.version) + ' ')) { Fail "bundled restic reports '$resticOut', expected version $($pin.version)" }
New-Item -ItemType Directory -Path (Join-Path $distDir 'licenses') | Out-Null
Copy-Item (Join-Path $Root 'third_party\restic\LICENSE') (Join-Path $distDir 'licenses\restic-LICENSE.txt')
Copy-Item (Join-Path $Root 'THIRD-PARTY-NOTICES.md') $distDir
Note "Bundled restic: $resticOut (SHA-256 of the archive verified: $gotHash)"

$notes = @"
BackupHarbor $Version

BackupHarbor.exe     desktop application (needs the Microsoft Edge WebView2 Runtime)
bharbor.exe          command-line tool (works without WebView2)
restic.exe           restic $($pin.version), unmodified official build, used by default (BSD 2-Clause, see licenses\)
config.example.yaml  example configuration, see docs\configuration.md

BackupHarbor uses the restic.exe next to it unless you choose another restic in Settings
(or in the configuration file). BackupHarbor sends no telemetry, never downloads anything
at run time and never updates itself.
Licence: see LICENSE (MIT) and THIRD-PARTY-NOTICES.md.
"@
Set-Content -LiteralPath (Join-Path $distDir 'README.txt') -Value $notes -Encoding utf8
Note "Distribution folder: $distDir"

# ---- archive and installer -------------------------------------------------------------------
Step 'Creating the ZIP archive'
$zip = Join-Path $Output "$name.zip"
if (Test-Path $zip) { Remove-Item -LiteralPath $zip -Force }
Compress-Archive -Path (Join-Path $distDir '*') -DestinationPath $zip -CompressionLevel Optimal
Note "Archive: $zip"

$installer = $null
if ($iscc) {
    Step 'Building the installer (Inno Setup)'
    Run $iscc @("/DMyAppVersion=$Version", "/DSourceDir=$distDir", "/DOutputDir=$Output", '/Q', (Join-Path $Root 'installer\backupharbor.iss'))
    $installer = Join-Path $Output "BackupHarbor-$Version-setup.exe"
    if (-not (Test-Path $installer)) { Fail 'Inno Setup finished but the installer was not created.' }
    Note "Installer: $installer"
}

# ---- checksums ------------------------------------------------------------------------------------
Step 'SHA-256 checksums'
$sumFile = Join-Path $Output "BackupHarbor-$Version-SHA256SUMS.txt"
$lines = @()
$targets = @((Join-Path $distDir 'BackupHarbor.exe'), (Join-Path $distDir 'bharbor.exe'), (Join-Path $distDir 'restic.exe'), $zip)
if ($installer) { $targets += $installer }
foreach ($t in $targets) {
    $h = (Get-FileHash -LiteralPath $t -Algorithm SHA256).Hash.ToLower()
    $rel = if ($t.StartsWith($distDir + '\')) { "$name/" + $t.Substring($distDir.Length + 1).Replace('\', '/') } else { [System.IO.Path]::GetFileName($t) }
    $lines += "$h  $rel"
}
Set-Content -LiteralPath $sumFile -Value $lines -Encoding ascii
$lines | ForEach-Object { Write-Host $_ }
Note "Checksums: $sumFile"

Write-Host ''
Write-Host '==== Build summary ====' -ForegroundColor Green
$script:Summary | ForEach-Object { Write-Host $_ }
Write-Host 'Build succeeded.' -ForegroundColor Green
exit 0
