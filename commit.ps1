<#
.SYNOPSIS
  Commits the working tree as a long series of small, meaningful, conventional commits.

.DESCRIPTION
  Every file belongs to a step of a built-in plan (one package part, one source file, one test file, one
  document ...). Each step becomes its own commit with a descriptive subject and body, in a sensible order:
  scaffolding, core packages, restic runner, verification engine, reports, scheduler, CLI, desktop app,
  packaging, tooling, CI, documentation.

  * First run (no commits yet): creates the repository (branch "main"), adds the "origin" remote
    https://github.com/Orevian/BackupHarbor.git and records the whole project as ~85 commits.
  * Later runs: commits only what changed, again one commit per plan step (so usually one per file or
    small file group). Files the plan does not know yet get their own commit each.
  * Files the project needs are guaranteed: missing hygiene files (.gitignore, .gitattributes,
    .editorconfig, LICENSE, VERSION) are created with safe defaults, missing documents are reported as an
    error, and needed files that .gitignore would hide are added with "git add -f".

  Commits use the author configured in git (user.name / user.email). The script never adds co-author
  trailers or any tool attribution and refuses messages containing them. It never pushes unless -Push is
  given, never rewrites history and never uses --no-verify. Binaries, key files, local config.yaml and
  files over 10 MB are never committed.

.PARAMETER Message
  Make ONE commit for everything with this message instead of using the plan.
.PARAMETER Type
  Conventional-commit type for modified files (feat, fix, refactor, perf, docs, test, build, ci, chore).
  Default: the plan's type for added files; chore for edits (docs, test, ci and build keep their type).
.PARAMETER Why
  Optional sentence added to the body of every commit (the reason for the change).
.PARAMETER Push
  After committing, push the branch to origin (git push -u origin <branch>).
.PARAMETER DryRun
  Show the commits that would be created and change nothing (not even git init).
.PARAMETER Remote
  URL for the "origin" remote when it does not exist yet.
.PARAMETER Branch
  Branch name for a new repository.
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File commit.ps1 -DryRun
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File commit.ps1 -Push
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File commit.ps1 -Type fix -Why "restore failed on junctions"
#>
[CmdletBinding()]
param(
    [string]$Message = '',
    [ValidateSet('', 'feat', 'fix', 'refactor', 'perf', 'docs', 'test', 'build', 'ci', 'chore')]
    [string]$Type = '',
    [string]$Why = '',
    [switch]$Push,
    [switch]$DryRun,
    [string]$Remote = 'https://github.com/Orevian/BackupHarbor.git',
    [string]$Branch = 'main'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $Root

function Fail([string]$Text) { Write-Host "ERROR: $Text" -ForegroundColor Red; exit 1 }
function Info([string]$Text) { Write-Host $Text -ForegroundColor Cyan }

$gitCmd = Get-Command git -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $gitCmd) { Fail 'git is not installed or not on PATH (https://git-scm.com/).' }
$script:GitExe = $gitCmd.Source

# In a dry run on a folder that is not a repository yet, use a throw-away git dir so nothing is created here.
$script:GitPrefix = @()
$hasRepo = Test-Path (Join-Path $Root '.git')
$tmpGitDir = $null
if (-not $hasRepo -and $DryRun) {
    $tmpGitDir = Join-Path ([System.IO.Path]::GetTempPath()) ('bh-dry-' + [guid]::NewGuid().ToString('N'))
    & $script:GitExe init --quiet --bare $tmpGitDir | Out-Null
    $script:GitPrefix = @("--git-dir=$tmpGitDir", "--work-tree=$Root")
}

# NOTE: the helpers must not be named "Git": command lookup is case-insensitive and would recurse.
function Invoke-Git {
    & $script:GitExe @script:GitPrefix @args
    if ($LASTEXITCODE -ne 0) { Fail ("git " + ($args -join ' ') + " failed with exit code $LASTEXITCODE") }
}
function Invoke-GitOut {
    $old = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    try { return (& $script:GitExe @script:GitPrefix @args 2>$null) } finally { $ErrorActionPreference = $old }
}

# ---- repository, remote, identity -------------------------------------------------------------
if (-not $hasRepo -and -not $DryRun) {
    Info "Creating a new repository (branch '$Branch')"
    Invoke-Git init --quiet
    Invoke-Git symbolic-ref HEAD "refs/heads/$Branch"
    $hasRepo = $true
}
if ($hasRepo -and -not $DryRun) {
    $origin = (Invoke-GitOut remote get-url origin) | Select-Object -First 1
    if (-not $origin) {
        Info "Adding remote 'origin' -> $Remote"
        Invoke-Git remote add origin $Remote
    } elseif ($origin -ne $Remote) {
        Write-Warning "Remote 'origin' is $origin (not $Remote); leaving it unchanged."
    }
}
$name = (Invoke-GitOut config user.name) | Select-Object -First 1
$email = (Invoke-GitOut config user.email) | Select-Object -First 1
if (-not $DryRun -and (-not $name -or -not $email)) {
    Fail "git has no author identity. Run once:`n  git config --global user.name  ""Your Name""`n  git config --global user.email ""you@example.com"""
}
$headExists = $false
if ($hasRepo -or $tmpGitDir) { $null = Invoke-GitOut rev-parse --verify --quiet HEAD; $headExists = ($LASTEXITCODE -eq 0) }
$initial = -not $headExists

# ---- files the project needs ---------------------------------------------------------------------------
$defaults = @{
    '.gitignore' = "# Build output`n/dist/`n/bin/`n*.exe`n*.test`n*.out`n*.syso`n/coverage.*`n`n# Editors and OS`n.vscode/`n.idea/`nThumbs.db`n.DS_Store`n`n# Local configuration, secrets and reports must never be committed`n/config.yaml`n*.local.yaml`n*.pw`n*.password`n/reports/`n/work/`n`n# Node tooling used only by tools/uidev`nnode_modules/`n"
    '.gitattributes' = "* text=auto eol=lf`n*.ps1 text eol=crlf`n*.iss text eol=crlf`n*.ico binary`n*.png binary`n*.exe binary`n"
    '.editorconfig' = "root = true`n`n[*]`ncharset = utf-8`nend_of_line = lf`ninsert_final_newline = true`ntrim_trailing_whitespace = true`nindent_style = space`nindent_size = 2`n`n[*.go]`nindent_style = tab`n`n[*.{ps1,iss}]`nend_of_line = crlf`nindent_size = 4`n`n[*.md]`ntrim_trailing_whitespace = false`n"
    'VERSION' = "0.1.0`n"
    'LICENSE' = "MIT License`n`nCopyright (c) $((Get-Date).Year) BackupHarbor contributors`n`nPermission is hereby granted, free of charge, to any person obtaining a copy`nof this software and associated documentation files (the `"Software`"), to deal`nin the Software without restriction, including without limitation the rights`nto use, copy, modify, merge, publish, distribute, sublicense, and/or sell`ncopies of the Software, and to permit persons to whom the Software is`nfurnished to do so, subject to the following conditions:`n`nThe above copyright notice and this permission notice shall be included in all`ncopies or substantial portions of the Software.`n`nTHE SOFTWARE IS PROVIDED `"AS IS`", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR`nIMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,`nFITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE`nAUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER`nLIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,`nOUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE`nSOFTWARE.`n"
}
$mustExist = @('go.mod', 'go.sum', 'README.md', 'README.tr.md', 'CHANGELOG.md', 'CONTRIBUTING.md', 'SECURITY.md',
    'docs/architecture.md', 'docs/configuration.md', 'docs/security.md', 'docs/report-schema.md', 'THIRD-PARTY-NOTICES.md',
    'scripts/build.ps1', 'installer/backupharbor.iss', 'build/windows/icon.ico', 'third_party/restic/PIN', 'third_party/restic/LICENSE',
    'frontend/index.html', 'cmd/bharbor/main.go', 'cmd/backupharbor/main.go') + @($defaults.Keys)
$missing = @()
foreach ($f in $mustExist) {
    if (Test-Path -LiteralPath (Join-Path $Root $f)) { continue }
    if ($defaults.ContainsKey($f)) {
        if ($DryRun) { Write-Host "  would create missing $f" -ForegroundColor Yellow }
        else {
            [System.IO.File]::WriteAllText((Join-Path $Root $f), $defaults[$f], (New-Object System.Text.UTF8Encoding $false))
            Write-Host "  created missing $f" -ForegroundColor Yellow
        }
    } else { $missing += $f }
}
if ($missing.Count -gt 0) { Fail ("Required project files are missing and cannot be generated:`n  " + ($missing -join "`n  ")) }

# ---- what changed ------------------------------------------------------------------------------------
$lines = @(Invoke-GitOut -c core.quotepath=off status --porcelain=v1 --untracked-files=all)
# Files the project needs but .gitignore would hide are added explicitly (git add -f).
$mustTrack = '^(go\.sum|VERSION|LICENSE|THIRD-PARTY-NOTICES\.md|third_party/.+|build/windows/.+|build/appicon\.png|frontend/.+|docs/.+|\.github/.+|installer/.+|scripts/.+|cmd/.+|internal/.+|tools/.+)$'
$ignoredNeeded = @(Invoke-GitOut -c core.quotepath=off ls-files --others --ignored --exclude-standard | Where-Object { $_ -match $mustTrack -and $_ -notmatch '\.(exe|dll|syso|pem|pfx|key|test|out|log|pw|password)$' -and $_ -notmatch '(^|/)config\.yaml$' })
$entries = @()
foreach ($l in $lines) {
    if ($l.Length -lt 4) { continue }
    $code = $l.Substring(0, 2)
    $path = $l.Substring(3).Trim('"')
    if ($path -like '* -> *') { $path = ($path -split ' -> ')[-1].Trim('"') }
    $kind = if ($code -match 'D') { 'D' } elseif ($code -eq '??' -or $code -match 'A') { 'A' } else { 'M' }
    $entries += [pscustomobject]@{ Path = $path.Replace('\', '/'); Kind = $kind }
}
$forced = @{}
foreach ($p in $ignoredNeeded) {
    $p = $p.Replace('\', '/')
    $forced[$p] = $true
    $entries += [pscustomobject]@{ Path = $p; Kind = 'A' }
}
if ($forced.Count -gt 0) { Write-Host "  adding $($forced.Count) needed file(s) that .gitignore would hide (git add -f)" -ForegroundColor Yellow }
if ($entries.Count -eq 0) { Write-Host 'Nothing to commit: the working tree is clean.'; if ($tmpGitDir) { Remove-Item -LiteralPath $tmpGitDir -Recurse -Force }; exit 0 }

# Never commit binaries, secrets-looking files or huge files by accident.
$blocked = @()
foreach ($e in $entries) {
    if ($e.Kind -eq 'D') { continue }
    $item = Get-Item -LiteralPath (Join-Path $Root $e.Path) -ErrorAction SilentlyContinue
    if ($e.Path -match '\.(exe|dll|syso|pw|password|pfx|pem|key)$' -or $e.Path -match '(^|/)config\.yaml$' -or ($item -and $item.Length -gt 10MB)) { $blocked += $e }
}
if ($blocked.Count -gt 0) {
    Write-Warning ("Skipping files that must not be committed (binaries, keys, local config, > 10 MB):`n  " + (($blocked | ForEach-Object { $_.Path }) -join "`n  "))
    $entries = @($entries | Where-Object { $blocked.Path -notcontains $_.Path })
    if ($entries.Count -eq 0) { Fail 'Only blocked files changed; nothing to commit.' }
}

# ---- the plan: ordered steps, first match wins ------------------------------------------------------------
# S <regex> <type> <scope> <short description> <first-commit subject> <first-commit body>
$plan = New-Object System.Collections.Generic.List[object]
function S([string]$re, [string]$type, [string]$scope, [string]$desc, [string]$subject, [string]$body) {
    $plan.Add([pscustomobject]@{ Re = $re; Type = $type; Scope = $scope; Desc = $desc; Subject = $subject; Body = $body })
}

# scaffolding
S '^LICENSE$' chore license 'MIT licence' 'add MIT licence' 'BackupHarbor is released under the MIT licence.'
S '^(\.gitignore|\.gitattributes)$' chore git 'git ignore and attributes' 'ignore build output, local config and keys' 'Keep build output, local configuration, reports and key material out of the repository; LF line endings for sources, CRLF for PowerShell and Inno Setup scripts.'
S '^\.editorconfig$' chore editor 'editor settings' 'add editorconfig' 'UTF-8, LF, tabs for Go and two spaces elsewhere.'
S '^(VERSION|go\.mod|go\.sum)$' build go 'Go module and version' 'add Go module, dependencies and VERSION' 'Wails v2, modernc SQLite, yaml.v3 and x/sys; VERSION is the single source for the release number.'
# version, i18n
S '^internal/version/version\.go$' feat version 'build identity' 'add build identity and product constants' 'Version, commit and date are set at link time; product name, CLI name and report schema id live here.'
S '^internal/i18n/(i18n|detect_windows|detect_other)\.go$' feat i18n 'message catalog loader' 'add catalog loader with Windows locale detection' 'Embedded English and Turkish catalogs, positional placeholders, message codes that stored reports can render in either language.'
S '^internal/i18n/locales/en\.json$' feat i18n 'English messages' 'add English engine, CLI and report messages' 'Stage, check, restore, report, configuration and CLI texts.'
S '^internal/i18n/locales/tr\.json$' feat i18n 'Turkish messages' 'add Turkish engine, CLI and report messages' 'Full Turkish translation with correct argument order.'
S '^internal/i18n/locales/en\.ui\.json$' feat i18n 'English UI strings' 'add English desktop UI strings' 'Labels, hints and explanations for the desktop application.'
S '^internal/i18n/locales/tr\.ui\.json$' feat i18n 'Turkish UI strings' 'add Turkish desktop UI strings' 'Turkish translation of the desktop application texts.'
S '^internal/i18n/i18n_test\.go$' test i18n 'catalog tests' 'check catalog parity, placeholders and used keys' 'Fails on missing translations, placeholder mismatches and message keys used by code or UI but absent from the catalog.'
# filesystem safety
S '^internal/fsx/fsx\.go$' feat fsx 'safe file operations' 'add atomic, no-clobber writes and link-safe deletion' 'Never leave a half-written file under its final name, never overwrite silently, never follow links when deleting.'
S '^internal/fsx/fsx_(windows|other)\.go$' feat fsx 'platform file helpers' 'add reparse-point detection, free space and ACL reset' 'Detect symlinks and junctions, read free disk space and reset restrictive ACLs of restored folders.'
S '^internal/fsx/fsx_test\.go$' test fsx 'fsx tests' 'cover no-clobber writes, containment and deletion' 'Includes failed writers, case-insensitive containment and read-only files.'
# secrets
S '^internal/secret/redact\.go$' feat secret 'secret masking' 'add masking of secrets and credentials' 'Masks known values, URL credentials and NAME=value pairs in logs, reports and errors.'
S '^internal/secret/store\.go$' feat secret 'password sources' 'add password sources and in-memory store' 'Credential Manager, environment variable or password file; bounded file reads.'
S '^internal/secret/wincred_(windows|other)\.go$' feat secret 'Credential Manager store' 'add Windows Credential Manager store' 'Generic credentials protected by the user DPAPI keys.'
S '^internal/secret/(secret_test|wincred_windows_test)\.go$' test secret 'secret tests' 'cover masking, sources and the real Credential Manager' 'Real round trip with a throw-away credential.'
# workspace
S '^internal/workspace/workspace\.go$' feat workspace 'work areas' 'add private work areas with ownership proof' 'Cleanup requires a marker with a matching nonce, a direct child of the scratch base and no link; abandoned areas are removed only when old and marked.'
S '^internal/workspace/workspace_test\.go$' test workspace 'work area tests' 'cover cleanup refusal, junctions and restrictive ACLs' 'Ensures nothing outside the work area can be deleted.'
# config
S '^internal/config/config\.go$' feat config 'configuration schema' 'add strict YAML schema, loading and atomic save' 'Unknown keys are errors, defaults are safe, relative paths resolve against the config directory.'
S '^internal/config/validate\.go$' feat config 'configuration validation' 'add validation rules' 'Unique target names, scratch directory safety, snapshot selection, check and rule syntax, schedule values.'
S '^internal/config/template\.go$' feat config 'example configuration' 'add the example configuration written by init' 'Commented example with a disabled target.'
S '^internal/config/config_test\.go$' test config 'configuration tests' 'cover parsing, validation and saving' 'Strict keys, scratch directory rules, path resolution and atomic save.'
# restic
S '^internal/restic/exec\.go$' feat restic 'process runner' 'add restic runner without a shell' 'Separate arguments, repository and password through the environment only, output size limits, exit-code classification and bundled-restic lookup.'
S '^internal/restic/proc_(windows|other)\.go$' feat restic 'process tree control' 'terminate the whole restic process tree on cancel' 'Windows job object with kill-on-close; no console window.'
S '^internal/restic/commands\.go$' feat restic 'restic commands' 'add version, snapshots, check, stats and restore' 'JSON progress and error parsing; metadata-only restore errors are counted separately.'
S '^internal/restic/(restic_test\.go|testdata/.+)$' test restic 'runner tests' 'add fake restic and runner tests' 'Exit codes, secret masking, cancellation, output limits and lookup order.'
# verification engine
S '^internal/verify/types\.go$' feat verify 'result types' 'add result types, statuses and exit codes' 'passed, failed, not_run, cancelled, timed_out, partial and skipped; exit-code precedence 3, 1, 2, 0.'
S '^internal/verify/glob\.go$' feat verify 'glob matcher' 'add glob matcher with ** for content rules' 'Case-insensitive matching on slash-separated restore paths.'
S '^internal/verify/checks\.go$' feat verify 'content checks' 'add file content checks' 'Text, JSON, XML, ZIP with CRC, PDF structure, SQLite integrity_check and SHA-256, all streaming and cancellable.'
S '^internal/verify/engine\.go$' feat verify 'engine' 'add the per-target engine and preflight stages' 'restic, directories, password, repository, snapshot and check stages with failure versus could-not-test classification.'
S '^internal/verify/restore\.go$' feat verify 'restore stage' 'add restore, content rules and verified cleanup' 'Space estimate, restore with verification, link-safe tree walk, rule evaluation and cleanup that always runs.'
S '^internal/verify/coverage\.go$' feat verify 'coverage statements' 'state what a result does and does not prove' 'Only passed checks are claimed.'
S '^internal/verify/run\.go$' feat verify 'multi-target runs' 'add parallel runs where one target never hides another' 'Concurrency limit, ordered results, panic isolation and stale work area cleanup.'
S '^internal/verify/checks_test\.go$' test verify 'checker tests' 'cover every file checker including corrupt SQLite' 'ZIP bomb limit and cancelled contexts are covered.'
S '^internal/verify/engine_test\.go$' test verify 'engine tests' 'cover status classification with a fake backend' 'Cancellation, timeout, partial restores, junctions and exit codes.'
S '^internal/verify/integration_test\.go$' test verify 'integration tests' 'add integration tests against real restic' 'Real repository, corrupted pack, wrong password, cancellation and timeout.'
S '^internal/demo/demo\.go$' feat demo 'demo data' 'add clearly marked demonstration data' 'Pure data with every status represented; cannot reach a repository.'
S '^internal/demo/demo_test\.go$' test demo 'demo tests' 'prove the demo package cannot reach repositories' 'Import allow-list and localisation checks.'
# reports
S '^internal/report/store\.go$' feat report 'report store' 'add immutable run records with an index' 'No-clobber saves, rebuildable index, filters and no-overwrite export.'
S '^internal/report/render\.go$' feat report 'text report' 'add plain-text report renderer' 'Localised, with stages, rules, details and coverage.'
S '^internal/report/html\.go$' feat report 'HTML report' 'add self-contained printable HTML report' 'html/template escaping, no scripts, no external resources, print styles.'
S '^internal/report/report_test\.go$' test report 'report tests' 'cover escaping, immutability and index repair' 'HTML injection, no overwrite, damaged index.'
# scheduler
S '^internal/schedule/schedule\.go$' feat schedule 'task scheduler' 'add Windows Task Scheduler integration' 'One owned task, XML-defined, verified by reading it back.'
S '^internal/schedule/sys_(windows|other)\.go$' feat schedule 'scheduler platform helpers' 'add hidden-window execution and user SID lookup' 'Used to create the task for the current user.'
S '^internal/schedule/schedule_test\.go$' test schedule 'scheduler tests' 'cover failure behaviour and the real scheduler' 'Foreign tasks are never touched; install failures are never shown as success.'
# app + cli
S '^internal/app/app\.go$' feat app 'shared composition' 'add run, validate and schedule composition' 'One code path for the CLI and the desktop app; fails before running if the report directory is unusable.'
S '^internal/app/logger\.go$' feat app 'logging' 'add redacting rotating log' 'Size-based rotation; every line passes through secret masking.'
S '^internal/cli/cli\.go$' feat cli 'CLI framework' 'add command framework, colours and exit codes' 'Global flags, NO_COLOR, language and terminal handling.'
S '^internal/cli/(errors|term_windows|term_other)\.go$' feat cli 'CLI helpers' 'add error mapping and console handling' 'ANSI enablement and echo-free password entry.'
S '^internal/cli/cmd_init_validate\.go$' feat cli 'init and validate' 'add init and validate commands' 'init never overwrites; validate checks everything except the restore and explains scope and cost.'
S '^internal/cli/cmd_run\.go$' feat cli 'run command' 'add run command with live progress' 'Real progress only; summary table and problem details.'
S '^internal/cli/cmd_report\.go$' feat cli 'report command' 'add report command' 'Show, list and export JSON, HTML or TXT without overwriting.'
S '^internal/cli/cmd_schedule_secret\.go$' feat cli 'schedule and secret' 'add schedule and secret commands' 'Install, status and remove the managed task; store passwords in Credential Manager.'
S '^internal/cli/cli_test\.go$' test cli 'CLI tests' 'cover exit codes, colours, reports and scheduling' 'Fake backend and fake scheduler.'
S '^cmd/bharbor/' feat cli 'entry point' 'add bharbor entry point' 'Ctrl+C cancels and cleans up; a second one terminates.'
# desktop back end and front end
S '^internal/gui/gui\.go$' feat gui 'application back end' 'add desktop back end: bootstrap, settings and about' 'Bound to the web UI; does not import Wails so it is testable.'
S '^internal/gui/targets\.go$' feat gui 'target management' 'add target editor back end with connection test' 'Passwords go to Credential Manager, never the config file.'
S '^internal/gui/run\.go$' feat gui 'live runs' 'add live verification events, cancel and demo simulation' 'One run at a time; demo mode never touches a repository.'
S '^internal/gui/reports\.go$' feat gui 'reports and schedule' 'add dashboard, history, export and schedule back end' 'Real and demo data are never mixed.'
S '^internal/gui/(util|sys_windows|sys_other)\.go$' feat gui 'platform helpers' 'add shell open and WebView2 detection' 'Used by about and report opening.'
S '^internal/gui/gui_test\.go$' test gui 'back end tests' 'cover settings, targets, runs, demo isolation and export' 'Demo mode must never call the backend.'
S '^frontend/style\.css$' feat ui 'styles' 'add design system with light and dark themes' 'Status badges with icon and text, visible focus, reduced motion.'
S '^frontend/index\.html$' feat ui 'page shell' 'add page shell, icons and dialog' 'Accessible landmarks, no external resources.'
S '^frontend/logo\.svg$' feat ui 'logo' 'add logo' 'Restore arrow with check mark.'
S '^frontend/core\.js$' feat ui 'core script' 'add API bridge, i18n, theme and dialogs' 'Stale page output is ignored after navigation.'
S '^frontend/pages\.js$' feat ui 'pages' 'add the six application pages' 'Real progress only; demo mode clearly separated.'
S '^frontend/app\.js$' feat ui 'navigation' 'add navigation and start-up' 'Does not collide with the Wails window.go object.'
S '^frontend/embed\.go$' feat ui 'embedded assets' 'embed the web UI' 'Static files compiled into the executable.'
S '^cmd/backupharbor/' feat gui 'desktop entry point' 'add Wails entry point' 'Single instance, native dialogs, WebView2 error instead of any download.'
# packaging
S '^build/windows/(cli|gui)\.manifest$|^build/windows/resource\.rc\.in$' build resources 'Windows resources' 'add manifests and version resource template' 'DPI awareness, long paths, version information.'
S '^build/(appicon\.png|windows/icon\.ico)$' build icon 'application icon' 'add multi-resolution application icon' 'Generated by scripts/make-icon.ps1.'
S '^scripts/make-icon\.ps1$' build icon 'icon generator' 'add icon generator' 'Renders the logo geometry at 16-256 px into an ICO.'
S '^scripts/build\.ps1$' build script 'build script' 'add build script' 'Format, vet, tests, resources, onedir package, ZIP, installer and SHA-256 checksums; refuses to delete folders it did not create.'
S '^third_party/restic/' build restic 'bundled restic pin' 'pin the bundled restic release' 'Download URL and SHA-256 verified at build time; BSD 2-Clause licence included.'
S '^THIRD-PARTY-NOTICES\.md$' docs notices 'third-party notices' 'add third-party notices' 'restic and Go library licences.'
S '^installer/' build installer 'installer' 'add Inno Setup installer' 'English and Turkish, optional PATH entry removed only if added, scheduled task removed on uninstall.'
# tooling
S '^tools/uidev/main\.go$' test uidev 'UI test bridge' 'add UI test bridge' 'Serves the real front end over HTTP backed by the real Go back end.'
S '^tools/uidev/(cdp|probe)\.mjs$' test uidev 'browser driver' 'add DevTools protocol driver' 'Dependency-free.'
S '^tools/uidev/smoke\.mjs$' test uidev 'smoke test' 'add browser smoke test' 'Real restic repository end to end, accessibility and scaling checks.'
S '^internal/version/repo_test\.go$' test repo 'repository checks' 'check version, required files, workflows and legacy names' 'Keeps VERSION, CHANGELOG, restic pin and the product name consistent.'
# CI and templates
S '^\.github/workflows/ci\.yml$' ci workflow 'CI workflow' 'add CI workflow' 'Format, vet, tests with real restic and scheduler, UI smoke test and packaging.'
S '^\.github/workflows/release\.yml$' ci workflow 'release workflow' 'add tag-triggered release workflow' 'Publishes only after every earlier step succeeded.'
S '^\.github/ISSUE_TEMPLATE/' chore github 'issue templates' 'add issue templates' 'Bug report and feature request forms.'
S '^\.github/(pull_request_template|RELEASE_TEMPLATE)\.md$' chore github 'pull request and release templates' 'add pull request and release templates' 'Checklist and release notes skeleton.'
# documentation
S '^docs/architecture\.md$' docs architecture 'architecture' 'add architecture and data flow' 'Packages, run data flow, status model and decisions.'
S '^docs/configuration\.md$' docs configuration 'configuration reference' 'add configuration reference' 'Every key, default and content rule type.'
S '^docs/security\.md$' docs security 'security design' 'add security design' 'Protections, password source comparison and limits.'
S '^docs/report-schema\.md$' docs schema 'report schema' 'document the report schema' 'backupharbor.run/1 with statuses and messages.'
S '^README\.md$' docs readme 'English README' 'add English README' 'Problem, features, requirements, installation, CLI, security and limitations.'
S '^README\.tr\.md$' docs readme 'Turkish README' 'add Turkish README' 'Turkish version of the README.'
S '^CHANGELOG\.md$' docs changelog 'changelog' 'add changelog' 'Keep a Changelog format.'
S '^CONTRIBUTING\.md$' docs contributing 'contributing guide' 'add contributing guide' 'Setup, layout, rules and pull requests.'
S '^SECURITY\.md$' docs security 'security policy' 'add security policy' 'Private vulnerability reporting and scope.'
S '^docs/screenshots/00-native-window\.png$' docs screenshots 'native window screenshot' 'add real native window screenshot' 'Captured from BackupHarbor.exe.'
S '^docs/screenshots/README\.md$' docs screenshots 'screenshot notes' 'explain how the screenshots were made' 'Native window versus headless browser captures.'
S '^docs/screenshots/.+\.png$' docs screenshots 'UI screenshots' 'add UI screenshots' 'Real captures of the interface in both languages and themes.'
S '^commit\.ps1$' chore tooling 'commit script' 'add commit script' 'Creates small meaningful commits without any co-author trailer.'

# ---- message building ----------------------------------------------------------------------------------------
function Test-Forbidden([string]$text) {
    return ($text -match '(?i)co-authored-by' -or $text -match '(?i)generated with' -or $text -match '(?i)noreply@anthropic')
}
function Get-Prefix([string]$type, [string]$scope) { if ($scope -eq $type) { return $type } else { return "$type($scope)" } }

# Generic step for files the plan does not know yet: one commit per file.
function New-GenericStep([string]$path) {
    $leaf = [System.IO.Path]::GetFileName($path)
    $parts = $path.Split('/')
    $scope = if ($parts.Count -ge 2 -and $parts[0] -in 'internal', 'cmd', 'tools') { $parts[1] } elseif ($parts.Count -ge 2) { $parts[0] } else { 'repo' }
    $type = if ($path -match '_test\.go$') { 'test' } elseif ($path -match '\.md$|^docs/') { 'docs' } elseif ($path -match '^\.github/workflows/') { 'ci' } elseif ($path -match '^(scripts|installer|build)/') { 'build' } else { 'feat' }
    return [pscustomobject]@{ Re = '^' + [regex]::Escape($path) + '$'; Type = $type; Scope = $scope; Desc = $leaf; Subject = "add $leaf"; Body = "New file $path." }
}

function Build-Message($step, $items) {
    $adds = @($items | Where-Object { $_.Kind -eq 'A' }).Count
    $dels = @($items | Where-Object { $_.Kind -eq 'D' }).Count
    $mods = @($items | Where-Object { $_.Kind -eq 'M' }).Count
    if ($initial) {
        $subject = "$(Get-Prefix $step.Type $step.Scope): $($step.Subject)"; $body = $step.Body
    } else {
        $verb = if ($dels -eq $items.Count) { 'remove' } elseif ($adds -eq $items.Count) { 'add' } else { 'update' }
        $type = if ($Type) { $Type } elseif ($adds -gt 0) { $step.Type } elseif ($step.Type -in 'docs', 'ci', 'build', 'test') { $step.Type } else { 'chore' }
        $paths = @($items | ForEach-Object { $_.Path })
        if (@($paths | Where-Object { $_ -notmatch '_test\.go$' }).Count -eq 0) { $type = 'test' }
        elseif (@($paths | Where-Object { $_ -notmatch '\.(md|txt|png)$' }).Count -eq 0) { $type = 'docs' }
        $subject = "$(Get-Prefix $type $step.Scope): $verb $($step.Desc)"
        $parts = @(); if ($adds) { $parts += "$adds added" }; if ($mods) { $parts += "$mods modified" }; if ($dels) { $parts += "$dels removed" }
        $body = "Files ($($parts -join ', ')):`n" + (($items | Select-Object -First 25 | ForEach-Object { "- $($_.Kind) $($_.Path)" }) -join "`n")
        if ($items.Count -gt 25) { $body += "`n- ... and $($items.Count - 25) more" }
    }
    if ($Why) { $body = "$Why`n`n$body" }
    if ($subject.Length -gt 72) { $subject = $subject.Substring(0, 69).TrimEnd() + '...' }
    return @{ Subject = $subject; Body = $body }
}

function Invoke-Commit([string]$subject, [string]$body, $items) {
    $full = "$subject`n`n$body`n"
    if (Test-Forbidden $full) { Fail 'The commit message contains a co-author or tool attribution; refusing to commit.' }
    Write-Host ("  {0}  ({1} file(s))" -f $subject, $items.Count) -ForegroundColor Green
    if ($DryRun) { return }
    $paths = @($items | ForEach-Object { $_.Path })
    for ($i = 0; $i -lt $paths.Count; $i += 100) {
        $chunk = @($paths[$i..([Math]::Min($i + 99, $paths.Count - 1))])
        $force = @($chunk | Where-Object { $forced.ContainsKey($_) })
        $normal = @($chunk | Where-Object { -not $forced.ContainsKey($_) })
        if ($normal.Count -gt 0) { Invoke-Git add -A -- @normal }
        if ($force.Count -gt 0) { Invoke-Git add -f -- @force }
    }
    $msgFile = Join-Path ([System.IO.Path]::GetTempPath()) ('bh-msg-' + [guid]::NewGuid().ToString('N') + '.txt')
    [System.IO.File]::WriteAllText($msgFile, $full, (New-Object System.Text.UTF8Encoding $false))
    try { Invoke-Git commit --quiet -F $msgFile } finally { Remove-Item -LiteralPath $msgFile -Force -ErrorAction SilentlyContinue }
}

# ---- do it ------------------------------------------------------------------------------------------------------------
Info ($(if ($DryRun) { 'Plan (dry run, nothing will be changed):' } else { 'Creating commits:' }))
$made = 0
if ($Message) {
    if (Test-Forbidden $Message) { Fail 'The message contains a co-author or tool attribution; refusing to commit.' }
    Invoke-Commit $Message ($(if ($Why) { $Why } else { "Files changed: $($entries.Count)" })) $entries
    $made = 1
} else {
    # Assign each entry to the first matching step; unknown files get a step of their own.
    $assigned = @{}
    $steps = New-Object System.Collections.Generic.List[object]
    foreach ($s in $plan) { $steps.Add($s) }
    foreach ($e in $entries) {
        $hit = $null
        foreach ($s in $plan) { if ($e.Path -match $s.Re) { $hit = $s; break } }
        if (-not $hit) { $hit = New-GenericStep $e.Path; $steps.Add($hit) }
        $assigned[$e.Path] = $hit
    }
    foreach ($s in $steps) {
        $items = @($entries | Where-Object { [object]::ReferenceEquals($assigned[$_.Path], $s) })
        if ($items.Count -eq 0) { continue }
        $m = Build-Message $s $items
        Invoke-Commit $m.Subject $m.Body $items
        $made++
    }
}

if ($tmpGitDir) { Remove-Item -LiteralPath $tmpGitDir -Recurse -Force -ErrorAction SilentlyContinue }
if ($DryRun) { Write-Host "`nDry run finished: $made commit(s) would be created."; exit 0 }

$left = @(Invoke-GitOut status --porcelain=v1 --untracked-files=all | Where-Object { $_ })
if ($left.Count -gt 0) { Write-Warning ("Not committed (skipped on purpose or ignored by the guard):`n  " + ($left -join "`n  ")) }
Write-Host "`nCreated $made commit(s):"
Invoke-Git log --oneline -n $made
$trailer = @(Invoke-GitOut log -n $made --format=%B | Where-Object { $_ -match '(?i)co-authored-by' })
if ($trailer.Count -gt 0) { Fail 'A co-author trailer was found in the new commits.' }

if ($Push) {
    $branch = (Invoke-GitOut rev-parse --abbrev-ref HEAD) | Select-Object -First 1
    Info "Pushing '$branch' to origin"
    Invoke-Git push -u origin $branch
} else {
    Write-Host "`nNot pushed. When ready:  git push -u origin $((Invoke-GitOut rev-parse --abbrev-ref HEAD) | Select-Object -First 1)   (or run commit.ps1 -Push)"
}
exit 0
