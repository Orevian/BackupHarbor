// Package demo produces clearly marked sample verification results for the
// demonstration mode of the GUI. It is pure data: it never runs restic, never
// touches the filesystem or the network, and its results are flagged Demo so
// they can never be mistaken for, or stored as, real verification results.
package demo

import (
	"time"

	"backupharbor/internal/i18n"
	"backupharbor/internal/verify"
	"backupharbor/internal/version"
)

// RunID prefix of every demonstration run.
const idPrefix = "DEMO-"

func stage(id string, st verify.Status, start time.Time, ms int64, msg i18n.Msg) verify.Stage {
	return verify.Stage{ID: id, Status: st, StartedAt: start, DurationMS: ms, Message: msg}
}

// Targets returns the names of the demonstration targets.
func Targets() []string {
	return []string{"documents-daily", "photos-weekly", "database-nightly", "offsite-nas"}
}

func documents(at time.Time) verify.TargetResult {
	snapTime := at.Add(-9 * time.Hour)
	t := verify.TargetResult{
		Name: "documents-daily", Status: verify.Passed, StartedAt: at, FinishedAt: at.Add(41 * time.Second), DurationMS: 41000,
		Repository: `D:\Backups\documents`,
		Snapshot: &verify.SnapshotInfo{ID: "7c1f4a9e2b6d83105f0c9a7e4d2b1c6f8a3e5d7b9c0f1a2b3c4d5e6f708192a3", ShortID: "7c1f4a9e", Time: snapTime,
			Hostname: "WORKSTATION", Paths: []string{`C:\Users\demo\Documents`}, Tags: []string{"daily"}},
		Restore: &verify.RestoreInfo{DurationMS: 12400, Files: 4182, Directories: 311, Bytes: 1395864371, Verified: true, BytesPerSecond: 112570000},
		Coverage: []i18n.Msg{i18n.M("cov.check_structure"), i18n.M("cov.restore_full"), i18n.M("cov.restore_verified"),
			i18n.M("cov.content", 3, 87), i18n.M("cov.limits")},
	}
	t.Stages = []verify.Stage{
		stage(verify.StageRestic, verify.Passed, at, 120, i18n.M("restic.ok", "0.19.1")),
		stage(verify.StageDirs, verify.Passed, at, 4, i18n.M("dirs.ok", `C:\Users\demo\AppData\Local\BackupHarbor\work`)),
		stage(verify.StagePassword, verify.Passed, at, 9, i18n.M("password.ok", "credential")),
		stage(verify.StageRepository, verify.Passed, at, 340, i18n.M("repo.ok")),
		stage(verify.StageSnapshot, verify.Passed, at, 410, i18n.M("snapshot.ok", "7c1f4a9e", snapTime.Format(time.RFC3339))),
		stage(verify.StageCheck, verify.Passed, at, 6800, i18n.M("check.passed_none")),
		stage(verify.StageRestore, verify.Passed, at, 12400, i18n.M("restore.ok", 4182, "1.3 GiB", "12.4s")),
		stage(verify.StageContent, verify.Passed, at, 3100, i18n.M("content.passed", 3)),
		stage(verify.StageCleanup, verify.Passed, at, 900, i18n.M("cleanup.ok")),
	}
	t.Stages[7].Rules = []verify.RuleResult{
		{Name: "accounting database", Type: "sqlite", Pattern: "**/accounts.sqlite", Status: verify.Passed, Matched: 1, Checked: 1, Passed: 1, Message: i18n.M("rule.passed", 1)},
		{Name: "invoices", Type: "pdf", Pattern: "**/Invoices/*.pdf", Status: verify.Passed, Matched: 62, Checked: 62, Passed: 62, Message: i18n.M("rule.passed", 62)},
		{Name: "settings", Type: "json", Pattern: "**/settings/*.json", Status: verify.Passed, Matched: 24, Checked: 24, Passed: 24, Message: i18n.M("rule.passed", 24)},
	}
	return t
}

func photos(at time.Time) verify.TargetResult {
	snapTime := at.Add(-30 * time.Hour)
	t := verify.TargetResult{
		Name: "photos-weekly", Status: verify.Partial, StartedAt: at, FinishedAt: at.Add(3*time.Minute + 8*time.Second), DurationMS: 188000,
		Repository: `E:\Backups\photos`,
		Snapshot: &verify.SnapshotInfo{ID: "b2e8d0417a95c3f6e1d2b4a8c7f90e5d3a1b2c4d6e8f0a1b3c5d7e9f1a2b4c6d", ShortID: "b2e8d041", Time: snapTime,
			Hostname: "WORKSTATION", Paths: []string{`C:\Users\demo\Pictures`}, Tags: []string{"weekly"}},
		Restore:  &verify.RestoreInfo{DurationMS: 151000, Files: 18204, Directories: 940, Bytes: 48318382080, ContentErrors: 3, MetadataWarnings: 12},
		Coverage: []i18n.Msg{i18n.M("cov.check_subset", "5%"), i18n.M("cov.restore_incomplete"), i18n.M("cov.limits")},
	}
	t.Stages = []verify.Stage{
		stage(verify.StageRestic, verify.Passed, at, 118, i18n.M("restic.ok", "0.19.1")),
		stage(verify.StageDirs, verify.Passed, at, 5, i18n.M("dirs.ok", `C:\Users\demo\AppData\Local\BackupHarbor\work`)),
		stage(verify.StagePassword, verify.Passed, at, 8, i18n.M("password.ok", "credential")),
		stage(verify.StageRepository, verify.Passed, at, 500, i18n.M("repo.ok")),
		stage(verify.StageSnapshot, verify.Passed, at, 620, i18n.M("snapshot.ok", "b2e8d041", snapTime.Format(time.RFC3339))),
		stage(verify.StageCheck, verify.Passed, at, 27000, i18n.M("check.passed_subset", "5%")),
		stage(verify.StageRestore, verify.Partial, at, 151000, i18n.M("restore.partial", 18204, 3)),
		stage(verify.StageContent, verify.Skipped, at, 0, i18n.M("content.no_rules")),
		stage(verify.StageCleanup, verify.Passed, at, 4200, i18n.M("cleanup.ok")),
	}
	t.Stages[6].Detail = "\\C\\Users\\demo\\Pictures\\2019\\IMG_4411.CR2: read error: unexpected EOF\n\\C\\Users\\demo\\Pictures\\2019\\IMG_4412.CR2: read error: unexpected EOF"
	return t
}

func database(at time.Time) verify.TargetResult {
	snapTime := at.Add(-5 * time.Hour)
	t := verify.TargetResult{
		Name: "database-nightly", Status: verify.Failed, StartedAt: at, FinishedAt: at.Add(24 * time.Second), DurationMS: 24000,
		Repository: `\\nas\backups\db`,
		Snapshot: &verify.SnapshotInfo{ID: "0f93a1c5d7e2b4968a0c1d3e5f7a9b2c4d6e8f0a1b3c5d7e9f2a4b6c8d0e1f3a", ShortID: "0f93a1c5", Time: snapTime,
			Hostname: "SQLSRV01", Paths: []string{`D:\Data`}},
		Restore:  &verify.RestoreInfo{DurationMS: 8900, Files: 212, Directories: 14, Bytes: 2147483648, Verified: true},
		Coverage: []i18n.Msg{i18n.M("cov.check_structure"), i18n.M("cov.restore_full"), i18n.M("cov.content_not_proven"), i18n.M("cov.limits")},
	}
	t.Stages = []verify.Stage{
		stage(verify.StageRestic, verify.Passed, at, 110, i18n.M("restic.ok", "0.19.1")),
		stage(verify.StageDirs, verify.Passed, at, 3, i18n.M("dirs.ok", `C:\Users\demo\AppData\Local\BackupHarbor\work`)),
		stage(verify.StagePassword, verify.Passed, at, 7, i18n.M("password.ok", "env")),
		stage(verify.StageRepository, verify.Passed, at, 800, i18n.M("repo.ok")),
		stage(verify.StageSnapshot, verify.Passed, at, 900, i18n.M("snapshot.ok", "0f93a1c5", snapTime.Format(time.RFC3339))),
		stage(verify.StageCheck, verify.Passed, at, 3100, i18n.M("check.passed_none")),
		stage(verify.StageRestore, verify.Passed, at, 8900, i18n.M("restore.ok", 212, "2.0 GiB", "8.9s")),
		stage(verify.StageContent, verify.Failed, at, 5200, i18n.M("content.failed", 1, 2)),
		stage(verify.StageCleanup, verify.Passed, at, 700, i18n.M("cleanup.ok")),
	}
	t.Stages[7].Rules = []verify.RuleResult{
		{Name: "orders database", Type: "sqlite", Pattern: "**/orders.db", Status: verify.Failed, Matched: 1, Checked: 1, Failed: 1, Message: i18n.M("rule.failed", 1, 1),
			Failures: []verify.FileFailure{{Path: "D/Data/orders.db", Msg: i18n.M("check.sqlite_integrity", "*** in database main *** Page 412: btreeInitPage() returns error code 11")}}},
		{Name: "config", Type: "json", Pattern: "**/app.json", Status: verify.Passed, Matched: 1, Checked: 1, Passed: 1, Message: i18n.M("rule.passed", 1)},
	}
	return t
}

func offsite(at time.Time) verify.TargetResult {
	t := verify.TargetResult{
		Name: "offsite-nas", Status: verify.NotRun, StartedAt: at, FinishedAt: at.Add(6 * time.Second), DurationMS: 6000,
		Repository: "sftp:backup@nas.example.org:/srv/restic",
		Coverage:   []i18n.Msg{i18n.M("cov.check_not_proven"), i18n.M("cov.restore_not_proven"), i18n.M("cov.limits")},
	}
	blocked := i18n.M("stage.blocked")
	t.Stages = []verify.Stage{
		stage(verify.StageRestic, verify.Passed, at, 115, i18n.M("restic.ok", "0.19.1")),
		stage(verify.StageDirs, verify.Passed, at, 4, i18n.M("dirs.ok", `C:\Users\demo\AppData\Local\BackupHarbor\work`)),
		stage(verify.StagePassword, verify.Passed, at, 8, i18n.M("password.ok", "credential")),
		stage(verify.StageRepository, verify.NotRun, at, 5900, i18n.M("repo.not_found")),
		stage(verify.StageSnapshot, verify.NotRun, at, 0, blocked),
		stage(verify.StageCheck, verify.NotRun, at, 0, blocked),
		stage(verify.StageRestore, verify.NotRun, at, 0, blocked),
		stage(verify.StageContent, verify.NotRun, at, 0, blocked),
		stage(verify.StageCleanup, verify.Skipped, at, 0, i18n.M("cleanup.nothing")),
	}
	t.Stages[3].Detail = "Fatal: unable to open config file: ssh: connect to host nas.example.org port 22: Connection timed out"
	return t
}

// build assembles a run from the given target builders.
func build(id string, at time.Time, targets ...verify.TargetResult) *verify.Run {
	r := verify.NewRun(idPrefix+id, verify.TriggerSchedule, at)
	r.Demo = true
	r.ResticVersion = "0.19.1"
	r.AppVersion = version.Version
	r.Targets = targets
	end := at
	for _, t := range targets {
		if t.FinishedAt.After(end) {
			end = t.FinishedAt
		}
	}
	r.Finish(end)
	return r
}

// Latest returns the most recent demonstration run, anchored at now.
func Latest(now time.Time) *verify.Run {
	at := now.Add(-2 * time.Hour).Truncate(time.Minute)
	return build("0001", at, documents(at), photos(at), database(at), offsite(at))
}

// History returns demonstration runs, newest first, with plausible variety.
func History(now time.Time) []*verify.Run {
	latest := Latest(now)
	day := 24 * time.Hour
	at2 := latest.StartedAt.Add(-1 * day)
	at3 := latest.StartedAt.Add(-2 * day)
	at4 := latest.StartedAt.Add(-3 * day)
	okDocs := func(at time.Time) verify.TargetResult { return documents(at) }
	brokenDb := func(at time.Time) verify.TargetResult {
		d := database(at)
		d.Status = verify.Passed
		for i := range d.Stages {
			if d.Stages[i].Status == verify.Failed {
				d.Stages[i].Status = verify.Passed
				d.Stages[i].Message = i18n.M("content.passed", 2)
				d.Stages[i].Rules = nil
			}
		}
		return d
	}
	return []*verify.Run{
		latest,
		build("0002", at2, okDocs(at2), brokenDb(at2)),
		build("0003", at3, okDocs(at3), brokenDb(at3)),
		build("0004", at4, okDocs(at4)),
	}
}

// Progress is one scripted step of a simulated run.
type Progress struct {
	Target  string
	Stage   string
	Status  verify.Status
	Message i18n.Msg
	// Percent of the simulated restore, or -1.
	Percent float64
	// Delay before this step is reported.
	Delay time.Duration
}

// Script returns the steps of a simulated run of the first demo target. The
// timings are artificial; no real work happens.
func Script() []Progress {
	t := "documents-daily"
	return []Progress{
		{t, verify.StageRestic, verify.Passed, i18n.M("restic.ok", "0.19.1"), -1, 300 * time.Millisecond},
		{t, verify.StagePassword, verify.Passed, i18n.M("password.ok", "credential"), -1, 250 * time.Millisecond},
		{t, verify.StageRepository, verify.Passed, i18n.M("repo.ok"), -1, 400 * time.Millisecond},
		{t, verify.StageSnapshot, verify.Passed, i18n.M("snapshot.ok", "7c1f4a9e", "demo"), -1, 350 * time.Millisecond},
		{t, verify.StageCheck, verify.Passed, i18n.M("check.passed_none"), -1, 900 * time.Millisecond},
		{t, verify.StageRestore, "", i18n.Msg{}, 20, 500 * time.Millisecond},
		{t, verify.StageRestore, "", i18n.Msg{}, 55, 700 * time.Millisecond},
		{t, verify.StageRestore, "", i18n.Msg{}, 90, 700 * time.Millisecond},
		{t, verify.StageRestore, verify.Passed, i18n.M("restore.ok", 4182, "1.3 GiB", "12.4s"), -1, 400 * time.Millisecond},
		{t, verify.StageContent, verify.Passed, i18n.M("content.passed", 3), -1, 500 * time.Millisecond},
		{t, verify.StageCleanup, verify.Passed, i18n.M("cleanup.ok"), -1, 300 * time.Millisecond},
	}
}
