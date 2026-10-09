// Package report stores the verification history shared by the CLI and the GUI
// and renders runs as JSON, HTML and plain text.
//
// Layout of the report directory:
//
//	runs\<run-id>.json   one immutable record per run (never overwritten)
//	runs\<run-id>.html   optional extra formats (reports.formats)
//	index.jsonl          one summary line per run, for fast listing
package report

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"backupharbor/internal/fsx"
	"backupharbor/internal/verify"
)

// Formats.
const (
	FormatJSON = "json"
	FormatHTML = "html"
	FormatTXT  = "txt"
)

// ErrNotFound is returned when a run does not exist.
var ErrNotFound = errors.New("report not found")

// ErrExists is returned when an export would overwrite an existing file.
var ErrExists = errors.New("file already exists")

var reRunID = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,80}$`)

// maxRunFile bounds how large a stored record may be when loaded.
const maxRunFile = 64 << 20

// TargetSummary is the per-target line of an index entry.
type TargetSummary struct {
	Name              string        `json:"name"`
	Status            verify.Status `json:"status"`
	SnapshotID        string        `json:"snapshot_id,omitempty"`
	SnapshotTime      *time.Time    `json:"snapshot_time,omitempty"`
	RestoreDurationMS int64         `json:"restore_duration_ms,omitempty"`
	Files             int64         `json:"files,omitempty"`
	Bytes             int64         `json:"bytes,omitempty"`
	DurationMS        int64         `json:"duration_ms"`
	FinishedAt        time.Time     `json:"finished_at"`
}

// Summary is one entry of the history index.
type Summary struct {
	RunID      string          `json:"run_id"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
	DurationMS int64           `json:"duration_ms"`
	Status     verify.Status   `json:"status"`
	Trigger    string          `json:"trigger"`
	Counts     verify.Counts   `json:"counts"`
	Targets    []TargetSummary `json:"targets"`
	Formats    []string        `json:"formats,omitempty"`
}

// Summarize derives the index entry of a run.
func Summarize(r *verify.Run) Summary {
	s := Summary{RunID: r.RunID, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, DurationMS: r.DurationMS,
		Status: r.Status, Trigger: r.Trigger, Counts: r.Summary}
	for _, t := range r.Targets {
		ts := TargetSummary{Name: t.Name, Status: t.Status, DurationMS: t.DurationMS, FinishedAt: t.FinishedAt}
		if t.Snapshot != nil {
			ts.SnapshotID = t.Snapshot.ShortID
			tm := t.Snapshot.Time
			ts.SnapshotTime = &tm
		}
		if t.Restore != nil {
			ts.RestoreDurationMS, ts.Files, ts.Bytes = t.Restore.DurationMS, t.Restore.Files, t.Restore.Bytes
		}
		s.Targets = append(s.Targets, ts)
	}
	return s
}

// Filter narrows a history listing.
type Filter struct {
	Target string    // run contains this target (case-insensitive)
	Since  time.Time // started at or after
	Until  time.Time // started before
	Status verify.Status
	Limit  int // newest first; 0 = unlimited
}

func (f Filter) match(s Summary) bool {
	if !f.Since.IsZero() && s.StartedAt.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && !s.StartedAt.Before(f.Until) {
		return false
	}
	if f.Status != "" {
		hit := s.Status == f.Status
		for _, t := range s.Targets {
			if t.Status == f.Status {
				hit = true
			}
		}
		if !hit {
			return false
		}
	}
	if f.Target != "" {
		hit := false
		for _, t := range s.Targets {
			if strings.EqualFold(t.Name, f.Target) {
				hit = true
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// Store is the report history in one directory.
type Store struct{ Dir string }

// NewStore returns a store rooted at dir.
func NewStore(dir string) *Store { return &Store{Dir: dir} }

func (s *Store) runsDir() string   { return filepath.Join(s.Dir, "runs") }
func (s *Store) indexPath() string { return filepath.Join(s.Dir, "index.jsonl") }

// RunPath returns the path of a run's file in the given format.
func (s *Store) RunPath(runID, format string) string {
	return filepath.Join(s.runsDir(), runID+"."+format)
}

// Check verifies that the store is usable (directory exists and is writable).
func (s *Store) Check() error {
	if s.Dir == "" || !filepath.IsAbs(s.Dir) {
		return fmt.Errorf("report directory %q must be an absolute path", s.Dir)
	}
	return fsx.WritableDir(s.runsDir(), true)
}

// SaveResult describes what Save wrote.
type SaveResult struct {
	Files []string
}

// Save stores a run as JSON plus the extra formats. A run record is never
// overwritten. If the record cannot be saved an error is returned and nothing
// is added to the index.
func (s *Store) Save(r *verify.Run, extra []string, lang string) (SaveResult, error) {
	var res SaveResult
	if r.Demo {
		return res, errors.New("demo runs are never stored")
	}
	if !reRunID.MatchString(r.RunID) {
		return res, fmt.Errorf("invalid run id %q", r.RunID)
	}
	if err := s.Check(); err != nil {
		return res, fmt.Errorf("report directory not writable: %w", err)
	}
	jp := s.RunPath(r.RunID, FormatJSON)
	if err := fsx.WriteNoClobber(jp, func(w io.Writer) error { return RenderJSON(w, r) }); err != nil {
		return res, fmt.Errorf("saving report: %w", err)
	}
	res.Files = append(res.Files, jp)
	sum := Summarize(r)
	sum.Formats = []string{FormatJSON}
	for _, f := range extra {
		if f != FormatHTML && f != FormatTXT {
			continue
		}
		p := s.RunPath(r.RunID, f)
		err := fsx.WriteNoClobber(p, func(w io.Writer) error { return Render(w, f, r, lang) })
		if err != nil {
			// The JSON record is the source of truth; report the extra failure but keep it.
			return res, fmt.Errorf("saving %s report: %w", f, err)
		}
		res.Files = append(res.Files, p)
		sum.Formats = append(sum.Formats, f)
	}
	if err := s.appendIndex(sum); err != nil {
		return res, fmt.Errorf("updating report index (the run record was saved): %w", err)
	}
	return res, nil
}

func (s *Store) appendIndex(sum Summary) error {
	line, err := json.Marshal(sum)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.indexPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	// One write call per record keeps concurrent CLI/GUI appends from interleaving.
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// List returns history entries, newest first, honouring the filter. A missing
// or damaged index is rebuilt from the run records.
func (s *Store) List(f Filter) ([]Summary, error) {
	all, err := s.readIndex()
	if err != nil || s.indexStale(all) {
		all, err = s.Rebuild()
		if err != nil {
			return nil, err
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].StartedAt.After(all[j].StartedAt) })
	var out []Summary
	for _, e := range all {
		if f.match(e) {
			out = append(out, e)
			if f.Limit > 0 && len(out) >= f.Limit {
				break
			}
		}
	}
	return out, nil
}

func (s *Store) readIndex() ([]Summary, error) {
	f, err := os.Open(s.indexPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Summary
	br := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var e Summary
			if json.Unmarshal(line, &e) == nil && e.RunID != "" {
				out = append(out, e)
			}
		}
		if err != nil {
			break
		}
	}
	return out, nil
}

// indexStale is true when run files exist that the index does not know about.
func (s *Store) indexStale(idx []Summary) bool {
	entries, err := os.ReadDir(s.runsDir())
	if err != nil {
		return false
	}
	known := make(map[string]bool, len(idx))
	for _, e := range idx {
		known[e.RunID] = true
	}
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".json") && !known[strings.TrimSuffix(name, ".json")] {
			return true
		}
	}
	return false
}

// Rebuild recreates the index from the run records.
func (s *Store) Rebuild() ([]Summary, error) {
	entries, err := os.ReadDir(s.runsDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		r, err := s.Load(id)
		if err != nil {
			continue // skip unreadable or partial records
		}
		sum := Summarize(r)
		sum.Formats = []string{FormatJSON}
		for _, f := range []string{FormatHTML, FormatTXT} {
			if _, err := os.Stat(s.RunPath(id, f)); err == nil {
				sum.Formats = append(sum.Formats, f)
			}
		}
		out = append(out, sum)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	if len(out) > 0 {
		err := fsx.ReplaceStream(s.indexPath(), func(w io.Writer) error {
			enc := json.NewEncoder(w)
			for _, e := range out {
				if err := enc.Encode(e); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return out, nil // listing still works from memory
		}
	}
	return out, nil
}

// Load reads one run record.
func (s *Store) Load(runID string) (*verify.Run, error) {
	if !reRunID.MatchString(runID) {
		return nil, fmt.Errorf("invalid run id %q", runID)
	}
	f, err := os.Open(s.RunPath(runID, FormatJSON))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, runID)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r verify.Run
	if err := json.NewDecoder(io.LimitReader(f, maxRunFile)).Decode(&r); err != nil {
		return nil, fmt.Errorf("report %s is damaged: %w", runID, err)
	}
	return &r, nil
}

// Last returns the most recent run (optionally containing a target).
func (s *Store) Last(target string) (*verify.Run, error) {
	l, err := s.List(Filter{Target: target, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(l) == 0 {
		return nil, ErrNotFound
	}
	return s.Load(l[0].RunID)
}

// LatestPerTarget returns, for every target name, its most recent result and
// the run it came from. Used by dashboards.
func (s *Store) LatestPerTarget() (map[string]TargetSummary, error) {
	l, err := s.List(Filter{})
	if err != nil {
		return nil, err
	}
	out := map[string]TargetSummary{}
	for _, e := range l { // newest first
		for _, t := range e.Targets {
			if _, ok := out[strings.ToLower(t.Name)]; !ok {
				out[strings.ToLower(t.Name)] = t
			}
		}
	}
	return out, nil
}

// FilterTarget returns a copy of the run containing only the named target.
func FilterTarget(r *verify.Run, name string) (*verify.Run, error) {
	cp := *r
	cp.Targets = nil
	for _, t := range r.Targets {
		if strings.EqualFold(t.Name, name) {
			cp.Targets = append(cp.Targets, t)
		}
	}
	if len(cp.Targets) == 0 {
		return nil, fmt.Errorf("%w: target %q is not part of run %s", ErrNotFound, name, r.RunID)
	}
	cp.Finish(r.FinishedAt)
	cp.StartedAt, cp.DurationMS = r.StartedAt, r.FinishedAt.Sub(r.StartedAt).Milliseconds()
	return &cp, nil
}

// Export writes a rendering of r to path. Existing files are never replaced
// unless overwrite is set; the file appears atomically and complete.
func Export(path, format string, r *verify.Run, lang string, overwrite bool) error {
	write := func(w io.Writer) error { return Render(w, format, r, lang) }
	if overwrite {
		return fsx.ReplaceStream(path, write)
	}
	err := fsx.WriteNoClobber(path, write)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %s (use --force to replace it)", ErrExists, path)
	}
	return err
}

// Render writes the run in the given format.
func Render(w io.Writer, format string, r *verify.Run, lang string) error {
	switch format {
	case FormatJSON:
		return RenderJSON(w, r)
	case FormatHTML:
		return RenderHTML(w, r, lang)
	case FormatTXT:
		return RenderTXT(w, r, lang)
	}
	return fmt.Errorf("unknown report format %q (use json, html or txt)", format)
}

// RenderJSON writes the stable machine-readable record (schema backupharbor.run/1).
func RenderJSON(w io.Writer, r *verify.Run) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}
