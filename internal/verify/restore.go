package verify

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backupharbor/internal/config"
	"backupharbor/internal/fsx"
	"backupharbor/internal/i18n"
	"backupharbor/internal/restic"
	"backupharbor/internal/workspace"
)

// defaultMaxFiles bounds how many files one rule inspects unless max_files says otherwise.
const defaultMaxFiles = 5000

// maxRecordedFailures bounds the per-rule failure list stored in reports.
const maxRecordedFailures = 50

type matchedFile struct {
	rel  string
	abs  string
	size int64
}

// collector gathers the restored files that match one rule while the tree is walked once.
type collector struct {
	rule  config.Rule
	m     *matcher
	limit int
	total int // all matches
	files []matchedFile
}

func (c *collector) offer(rel, abs string, size int64) {
	if !c.m.Match(rel) {
		return
	}
	c.total++
	if len(c.files) < c.limit {
		c.files = append(c.files, matchedFile{rel, abs, size})
	}
}

type walkStats struct {
	files, dirs, bytes, links int64
	errs                      int
}

// walkTree counts the restored tree without ever following links and feeds the collectors.
func walkTree(ctx context.Context, root string, cols []*collector) (walkStats, error) {
	var ws walkStats
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if n++; n%512 == 0 {
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
		}
		if err != nil {
			ws.errs++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		fi, ierr := d.Info()
		if ierr != nil {
			ws.errs++
			return nil
		}
		if fsx.IsReparsePoint(fi) {
			ws.links++
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			ws.dirs++
			return nil
		}
		if !fi.Mode().IsRegular() {
			ws.links++
			return nil
		}
		ws.files++
		ws.bytes += fi.Size()
		if len(cols) > 0 {
			rel, rerr := filepath.Rel(root, path)
			if rerr == nil {
				rel = filepath.ToSlash(rel)
				for _, c := range cols {
					c.offer(rel, path, fi.Size())
				}
			}
		}
		return nil
	})
	return ws, err
}

func (x *targetRun) stRestore(st *Stage) {
	t := x.t
	if !t.Restore.On() {
		st.Status = Skipped
		st.Message = i18n.M("stage.disabled")
		return
	}
	if x.noRestore {
		x.setNotRun(st, "restore.no_workspace")
		return
	}
	st.Evidence = map[string]any{"verify": t.Restore.VerifyOn()}
	base := x.e.paths.TempDir

	// Estimate the restore size to avoid filling the disk.
	if len(t.Restore.Include) == 0 {
		if s, err := x.e.backend.Stats(x.ctx, x.repo, x.snap.ID); err == nil {
			st.Evidence["estimated_bytes"] = s.TotalSize
			st.Evidence["estimated_files"] = s.TotalFileCount
			if free, ferr := fsx.FreeSpace(base); ferr == nil {
				st.Evidence["free_bytes"] = free
				if s.TotalSize/100*99 > free {
					x.setNotRun(st, "restore.no_space", humanBytes(int64(s.TotalSize)), humanBytes(int64(free)))
					return
				}
			}
		} else if x.interrupted(st) {
			return
		} else {
			x.e.logf("debug", "%s: restore size estimate unavailable: %v", t.Name, x.red.Redact(err.Error()))
		}
	}

	ws, err := workspace.Create(base, x.runID+"-"+t.Name)
	if err != nil {
		x.setNotRun(st, "restore.workspace_failed", err.Error())
		return
	}
	x.ws = ws

	for _, r := range t.Rules {
		limit := r.MaxFiles
		if limit <= 0 {
			limit = defaultMaxFiles
		}
		x.cols = append(x.cols, &collector{rule: r, m: newMatcher(r.Path), limit: limit})
	}

	opts := restic.RestoreOptions{SnapshotID: x.snap.ID, Target: ws.RestoreDir, Include: t.Restore.Include, Verify: t.Restore.VerifyOn()}
	begin := time.Now()
	sum, rerr := x.e.backend.Restore(x.ctx, x.repo, opts, func(p restic.RestoreProgress) {
		x.e.emit(Event{Type: "progress", RunID: x.runID, Target: t.Name, Stage: StageRestore, Percent: p.PercentDone * 100,
			FilesDone: p.FilesRestored, FilesTotal: p.TotalFiles, BytesDone: p.BytesRestored, BytesTotal: p.TotalBytes})
	})
	dur := time.Since(begin)
	if x.interrupted(st) {
		return
	}

	stats, werr := walkTree(x.ctx, ws.RestoreDir, x.cols)
	x.walk, x.walked = stats, true
	if x.interrupted(st) {
		return
	}
	info := &RestoreInfo{DurationMS: dur.Milliseconds(), Files: stats.files, Directories: stats.dirs, Bytes: stats.bytes,
		Links: stats.links, MetadataWarnings: sum.MetadataErrorCount, ContentErrors: sum.ErrorCount, Verified: t.Restore.VerifyOn() && rerr == nil}
	if secs := dur.Seconds(); secs > 0 {
		info.BytesPerSecond = uint64(float64(stats.bytes) / secs)
	}
	x.tr.Restore = info
	st.Evidence["duration_ms"] = info.DurationMS
	st.Evidence["files"] = stats.files
	st.Evidence["directories"] = stats.dirs
	st.Evidence["bytes"] = stats.bytes
	st.Evidence["metadata_warnings"] = sum.MetadataErrorCount
	st.Evidence["content_errors"] = sum.ErrorCount
	if stats.links > 0 {
		st.Evidence["links_not_followed"] = stats.links
	}
	if len(sum.Errors) > 0 {
		st.Evidence["errors"] = sum.Errors
		var lines []string
		for i, e := range sum.Errors {
			if i == 10 {
				break
			}
			lines = append(lines, strings.TrimSpace(e.Item+": "+e.Message))
		}
		st.Detail = x.red.Redact(strings.Join(lines, "\n"))
	}

	switch {
	case rerr == nil:
		st.Status = Passed
	default:
		var re *restic.Error
		if !errors.As(rerr, &re) {
			st.Status = Failed
			st.Message = i18n.M("restore.failed", x.red.Redact(rerr.Error()))
			st.Detail = x.detail(rerr)
			return
		}
		switch re.Kind {
		case restic.KindNoRepo, restic.KindLocked, restic.KindWrongPassword, restic.KindStart, restic.KindOutputLimit:
			x.resticAccessProblem(st, rerr, "restore.not_run")
			return
		}
		if st.Detail == "" {
			st.Detail = x.detail(rerr)
		}
		switch {
		case sum.ErrorCount == 0 && sum.MetadataErrorCount > 0 && sum.HaveSummary && stats.files > 0:
			// Content was restored; only attributes (timestamps, ACLs, ...) were not.
			st.Status = Passed
		case stats.files > 0:
			st.Status = Partial
			st.Message = i18n.M("restore.partial", stats.files, max(sum.ErrorCount, 1))
		default:
			st.Status = Failed
			st.Message = i18n.M("restore.failed", lastLineOf(st.Detail))
		}
	}
	if st.Status == Passed || st.Status == Partial {
		if min := int64(t.Restore.MinFiles); stats.files < min {
			st.Status = Failed
			st.Message = i18n.M("restore.too_few_files", stats.files, min)
			return
		}
		x.restoreOK = true
	}
	if st.Status == Passed {
		if sum.MetadataErrorCount > 0 {
			st.Message = i18n.M("restore.ok_metadata", stats.files, humanBytes(stats.bytes), dur.Round(time.Millisecond), sum.MetadataErrorCount)
		} else {
			st.Message = i18n.M("restore.ok", stats.files, humanBytes(stats.bytes), dur.Round(time.Millisecond))
		}
	}
	if werr != nil && st.Status == Passed {
		st.Status = Partial
		st.Message = i18n.M("restore.unreadable_tree", werr.Error())
	}
	if stats.errs > 0 && st.Status == Passed {
		st.Status = Partial
		st.Message = i18n.M("restore.unreadable_tree", stats.errs)
	}
}

func lastLineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

// ---- content rules ----------------------------------------------------------

func (x *targetRun) stContent(st *Stage) {
	if len(x.t.Rules) == 0 {
		st.Status = Skipped
		st.Message = i18n.M("content.no_rules")
		return
	}
	if !x.restoreOK || x.ws == nil {
		x.setNotRun(st, "content.no_restore")
		return
	}
	var statuses []Status
	for i, c := range x.cols {
		if err := x.ctx.Err(); err != nil {
			break
		}
		name := c.rule.Name
		if name == "" {
			name = c.rule.Path
		}
		x.e.emit(Event{Type: "progress", RunID: x.runID, Target: x.t.Name, Stage: StageContent, Percent: float64(i) / float64(len(x.cols)) * 100,
			Message: i18n.M("content.rule", name)})
		rr := x.evalRule(c)
		st.Rules = append(st.Rules, rr)
		statuses = append(statuses, rr.Status)
	}
	if x.interrupted(st) {
		return
	}
	var relevant []Status
	for _, s := range statuses {
		if s != Skipped {
			relevant = append(relevant, s)
		}
	}
	if len(relevant) == 0 {
		st.Status = Skipped
		st.Message = i18n.M("content.all_optional_empty")
		return
	}
	st.Status = Worst(relevant...)
	failed, ok := 0, 0
	for _, r := range st.Rules {
		switch r.Status {
		case Failed:
			failed++
		case Passed:
			ok++
		}
	}
	switch st.Status {
	case Passed:
		st.Message = i18n.M("content.passed", len(relevant))
	case Partial:
		st.Message = i18n.M("content.partial")
	default:
		st.Message = i18n.M("content.failed", failed, len(relevant))
	}
}

func (x *targetRun) evalRule(c *collector) RuleResult {
	begin := time.Now()
	r := c.rule
	rr := RuleResult{Name: r.Name, Type: r.Type, Pattern: r.Path, Matched: c.total}
	defer func() { rr.DurationMS = time.Since(begin).Milliseconds() }()

	if c.total == 0 {
		if r.IsRequired() {
			rr.Status = Failed
			rr.Message = i18n.M("rule.no_match")
		} else {
			rr.Status = Skipped
			rr.Message = i18n.M("rule.no_match_optional")
		}
		return rr
	}
	anyPartial := false
	for _, f := range c.files {
		if x.ctx.Err() != nil {
			break
		}
		fc := checkFile(x.ctx, r, f.abs, f.size)
		rr.Checked++
		switch fc.status {
		case Passed:
			rr.Passed++
		case Partial:
			rr.Passed++
			anyPartial = true
		case Cancelled:
			return rr
		default:
			rr.Failed++
			if len(rr.Failures) < maxRecordedFailures {
				rr.Failures = append(rr.Failures, FileFailure{Path: f.rel, Msg: fc.msg})
			}
		}
	}
	switch {
	case rr.Failed > 0:
		rr.Status = Failed
		rr.Message = i18n.M("rule.failed", rr.Failed, rr.Checked)
	case rr.Matched > rr.Checked || anyPartial:
		rr.Status = Partial
		rr.Message = i18n.M("rule.sampled", rr.Checked, rr.Matched)
	default:
		rr.Status = Passed
		rr.Message = i18n.M("rule.passed", rr.Checked)
	}
	return rr
}

// ---- cleanup ----------------------------------------------------------------

func (x *targetRun) stCleanup(st *Stage) {
	if x.ws == nil {
		st.Status = Skipped
		st.Message = i18n.M("cleanup.nothing")
		return
	}
	// Cleanup must complete even when the run was cancelled or timed out.
	if err := x.ws.Cleanup(); err != nil {
		st.Status = Failed
		st.Message = i18n.M("cleanup.failed", x.ws.Dir)
		st.Detail = x.red.Redact(err.Error())
		x.e.logf("error", "%s: cleanup failed: %v", x.t.Name, err)
		return
	}
	if _, serr := os.Stat(x.ws.Dir); serr == nil || !errors.Is(serr, fs.ErrNotExist) {
		st.Status = Failed
		st.Message = i18n.M("cleanup.failed", x.ws.Dir)
		return
	}
	st.Status = Passed
	st.Message = i18n.M("cleanup.ok")
}

// ---- aggregation ------------------------------------------------------------

func isProof(id string) bool { return id == StageCheck || id == StageRestore || id == StageContent }

// targetStatus derives the target outcome from its stages. A target is Passed
// only if nothing failed, nothing could not be run, and at least one proof
// stage (check, restore, content) actually passed.
func targetStatus(stages []Stage) Status {
	var cancelled, timedOut, failed, partialSeen, notRun, proofPassed, cleanupFailed bool
	for _, s := range stages {
		switch s.Status {
		case Cancelled:
			cancelled = true
		case TimedOut:
			timedOut = true
		case Failed:
			if s.ID == StageCleanup {
				cleanupFailed = true
			} else {
				failed = true
			}
		case Partial:
			partialSeen = true
		case NotRun:
			notRun = true
		case Passed:
			if isProof(s.ID) {
				proofPassed = true
			}
		}
	}
	switch {
	case cancelled:
		return Cancelled
	case timedOut:
		return TimedOut
	case failed:
		return Failed
	case partialSeen:
		return Partial
	case notRun:
		if proofPassed {
			return Partial
		}
		return NotRun
	case cleanupFailed:
		return Partial
	case !proofPassed:
		return NotRun
	}
	return Passed
}

// preflightStatus is Passed when every readiness stage passed or was skipped.
func preflightStatus(stages []Stage) Status {
	var ss []Status
	for _, s := range stages {
		if s.Status != Skipped {
			ss = append(ss, s.Status)
		}
	}
	return Worst(ss...)
}
