// Package version holds build-time identity of BackupHarbor binaries.
package version

import "fmt"

// Overridden at link time via -ldflags "-X backupharbor/internal/version.Version=..."
var (
	Version = "0.1.0"
	Commit  = "unknown"
	Date    = "unknown"
)

const (
	// ProductName is the user-facing product name.
	ProductName = "BackupHarbor"
	// CLIName is the executable name of the command-line tool.
	CLIName = "bharbor"
	// ReportSchema identifies the JSON run-report schema.
	ReportSchema = "backupharbor.run/1"
	// ConfigVersion is the current configuration schema version.
	ConfigVersion = 1
	// MinResticMajor/Minor is the oldest restic release the engine is written against
	// (restore --verify and JSON restore summaries need 0.16).
	MinResticMajor = 0
	MinResticMinor = 16
)

// String returns a one-line human readable version.
func String() string {
	return fmt.Sprintf("%s %s (commit %s, built %s)", ProductName, Version, Commit, Date)
}
