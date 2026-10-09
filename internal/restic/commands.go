package restic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"backupharbor/internal/secret"
)

// Snapshot is the subset of restic snapshot metadata BackupHarbor records.
type Snapshot struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname"`
	Username string    `json:"username,omitempty"`
	Paths    []string  `json:"paths"`
	Tags     []string  `json:"tags,omitempty"`
	Program  string    `json:"program_version,omitempty"`
}

// SnapshotFilter narrows the snapshot list.
type SnapshotFilter struct {
	Tags []string // all must be present
	Host string
}

// Version runs "restic version".
func (c *Client) Version(ctx context.Context) (Version, error) {
	res, err := c.run(ctx, runSpec{command: "version", args: []string{"version"}, collect: true, noRepo: true})
	if err != nil {
		return Version{}, err
	}
	return ParseVersion(string(res.stdout))
}

// CatConfig opens the repository (verifying location and password) by reading
// its config object.
func (c *Client) CatConfig(ctx context.Context, repo Repo) error {
	_, err := c.run(ctx, runSpec{command: "cat config", args: []string{"cat", "config"}, repo: repo, collect: true})
	return err
}

// Snapshots lists snapshots, optionally filtered, in chronological order.
func (c *Client) Snapshots(ctx context.Context, repo Repo, f SnapshotFilter) ([]Snapshot, error) {
	args := []string{"snapshots", "--json"}
	if len(f.Tags) > 0 {
		args = append(args, "--tag", strings.Join(f.Tags, ","))
	}
	if f.Host != "" {
		args = append(args, "--host", f.Host)
	}
	res, err := c.run(ctx, runSpec{command: "snapshots", args: args, repo: repo, collect: true})
	if err != nil {
		return nil, err
	}
	var snaps []Snapshot
	if err := json.Unmarshal(res.stdout, &snaps); err != nil {
		return nil, &Error{Command: "snapshots", ExitCode: 0, Err: fmt.Errorf("unreadable snapshot list: %w", err)}
	}
	sort.SliceStable(snaps, func(i, j int) bool { return snaps[i].Time.Before(snaps[j].Time) })
	return snaps, nil
}

// CheckOptions configures "restic check".
type CheckOptions struct {
	ReadData string // none | subset | all
	Subset   string
}

// CheckResult carries the diagnostics of a successful check.
type CheckResult struct{ Output string }

// Check runs "restic check". A returned *Error with KindGeneric and exit code 1
// means integrity problems were reported.
func (c *Client) Check(ctx context.Context, repo Repo, o CheckOptions) (CheckResult, error) {
	args := []string{"check"}
	switch o.ReadData {
	case "all":
		args = append(args, "--read-data")
	case "subset":
		args = append(args, "--read-data-subset="+o.Subset)
	}
	res, err := c.run(ctx, runSpec{command: "check", args: args, repo: repo, onLine: func([]byte) {}})
	out := CheckResult{}
	if res != nil {
		out.Output = res.tail
	}
	return out, err
}

// Stats is the restore-size estimate of a snapshot.
type Stats struct {
	TotalSize      uint64 `json:"total_size"`
	TotalFileCount uint64 `json:"total_file_count"`
}

// Stats runs "restic stats --mode restore-size" for one snapshot.
func (c *Client) Stats(ctx context.Context, repo Repo, snapshotID string) (Stats, error) {
	res, err := c.run(ctx, runSpec{command: "stats", repo: repo, collect: true,
		args: []string{"stats", "--mode", "restore-size", "--json", snapshotID}})
	if err != nil {
		return Stats{}, err
	}
	var s Stats
	if err := json.Unmarshal(res.stdout, &s); err != nil {
		return Stats{}, &Error{Command: "stats", Err: fmt.Errorf("unreadable stats: %w", err)}
	}
	return s, nil
}

// RestoreOptions configures "restic restore".
type RestoreOptions struct {
	SnapshotID string
	Target     string
	Include    []string
	Verify     bool
}

// RestoreProgress is a live progress sample (restic reports real numbers).
type RestoreProgress struct {
	PercentDone   float64
	TotalFiles    uint64
	FilesRestored uint64
	TotalBytes    uint64
	BytesRestored uint64
}

// RestoreItemError is one per-item error reported by restic.
type RestoreItemError struct {
	Item    string `json:"item,omitempty"`
	Message string `json:"message"`
	// Metadata is true when only file attributes (timestamps, ownership,
	// security descriptors, extended attributes) could not be restored while
	// the content itself was written.
	Metadata bool `json:"metadata,omitempty"`
}

// RestoreSummary is the final report of a restore. Restic counts directories
// as "files"; BackupHarbor therefore measures the restored tree itself and uses
// these numbers only for progress.
type RestoreSummary struct {
	TotalFiles    uint64             `json:"total_files"`
	FilesRestored uint64             `json:"files_restored"`
	FilesSkipped  uint64             `json:"files_skipped"`
	TotalBytes    uint64             `json:"total_bytes"`
	BytesRestored uint64             `json:"bytes_restored"`
	HaveSummary   bool               `json:"-"`
	Errors        []RestoreItemError `json:"errors,omitempty"`
	// ErrorCount counts content errors; MetadataErrorCount counts metadata-only errors.
	ErrorCount         int `json:"error_count"`
	MetadataErrorCount int `json:"metadata_error_count"`
}

var metadataMarkers = []string{
	"restore timestamp", "restore ownership", "restore security descriptor",
	"restore extended attributes", "restore generic attributes", "restore permissions",
	"chown", "chmod", "set file times",
}

// IsMetadataError reports whether a restic restore error message concerns only
// file metadata rather than file content.
func IsMetadataError(msg string) bool {
	l := strings.ToLower(msg)
	for _, m := range metadataMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

const maxRecordedErrors = 100

type restoreMsg struct {
	MessageType   string  `json:"message_type"`
	PercentDone   float64 `json:"percent_done"`
	TotalFiles    uint64  `json:"total_files"`
	FilesRestored uint64  `json:"files_restored"`
	FilesSkipped  uint64  `json:"files_skipped"`
	TotalBytes    uint64  `json:"total_bytes"`
	BytesRestored uint64  `json:"bytes_restored"`
	Item          string  `json:"item"`
	Error         *struct {
		Message string `json:"message"`
	} `json:"error"`
	// Some restic versions report failures this way.
	Message string `json:"message"`
}

// Restore runs "restic restore --json" into o.Target, streaming progress.
// On failure the summary is still returned (it may describe a partial restore).
func (c *Client) Restore(ctx context.Context, repo Repo, o RestoreOptions, progress func(RestoreProgress)) (RestoreSummary, error) {
	args := []string{"restore", o.SnapshotID, "--target", o.Target, "--json"}
	if o.Verify {
		args = append(args, "--verify")
	}
	for _, inc := range o.Include {
		args = append(args, "--include", inc)
	}
	var sum RestoreSummary
	red := secret.NewRedactor(repo.Password)
	_, err := c.run(ctx, runSpec{command: "restore", args: args, repo: repo, onLine: func(line []byte) {
		if len(line) == 0 || line[0] != '{' {
			return
		}
		var m restoreMsg
		if json.Unmarshal(line, &m) != nil {
			return
		}
		switch m.MessageType {
		case "status":
			if progress != nil {
				progress(RestoreProgress{PercentDone: m.PercentDone, TotalFiles: m.TotalFiles,
					FilesRestored: m.FilesRestored, TotalBytes: m.TotalBytes, BytesRestored: m.BytesRestored})
			}
		case "summary":
			sum.HaveSummary = true
			sum.TotalFiles, sum.FilesRestored, sum.FilesSkipped = m.TotalFiles, m.FilesRestored, m.FilesSkipped
			sum.TotalBytes, sum.BytesRestored = m.TotalBytes, m.BytesRestored
		case "error":
			msg := m.Message
			if m.Error != nil {
				msg = m.Error.Message
			}
			meta := IsMetadataError(msg)
			if meta {
				sum.MetadataErrorCount++
			} else {
				sum.ErrorCount++
			}
			if len(sum.Errors) < maxRecordedErrors {
				sum.Errors = append(sum.Errors, RestoreItemError{Item: red.Redact(m.Item), Message: red.Redact(msg), Metadata: meta})
			}
		}
	}})
	var rerr *Error
	if errors.As(err, &rerr) {
		return sum, err
	}
	return sum, err
}
