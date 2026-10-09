// Package schedule manages the single Windows Task Scheduler task that runs
// "bharbor run" periodically. It only ever touches the task it created: the
// task lives in its own \BackupHarbor folder and carries an ownership marker
// that is checked before a task is replaced or removed.
package schedule

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	// DefaultTaskName is the full path of the managed task.
	DefaultTaskName = `\BackupHarbor\Verify`
	// Marker is stored in the task description and proves ownership.
	Marker = "BackupHarbor managed task. Edit the schedule with bharbor or the BackupHarbor app."
)

// ErrForeignTask is returned when a task of that name exists but was not created by BackupHarbor.
var ErrForeignTask = errors.New("a scheduled task with this name exists but was not created by BackupHarbor; it will not be modified")

// ErrNotInstalled is returned by Remove when there is nothing to remove.
var ErrNotInstalled = errors.New("the BackupHarbor scheduled task is not installed")

// Spec describes the task to install.
type Spec struct {
	Frequency  string        // daily | weekly
	Time       string        // HH:MM
	Day        string        // weekly: monday..sunday
	ExePath    string        // absolute path of bharbor.exe
	ConfigPath string        // absolute path of the configuration file
	TimeLimit  time.Duration // task execution time limit
}

// Status is the observed state of the managed task.
type Status struct {
	Installed  bool       `json:"installed"`
	State      string     `json:"state,omitempty"`
	Owned      bool       `json:"owned"`
	NextRun    *time.Time `json:"next_run,omitempty"`
	LastRun    *time.Time `json:"last_run,omitempty"`
	LastResult *int64     `json:"last_result,omitempty"`
	Command    string     `json:"command,omitempty"`
	Arguments  string     `json:"arguments,omitempty"`
	Frequency  string     `json:"frequency,omitempty"`
	Time       string     `json:"time,omitempty"`
	Day        string     `json:"day,omitempty"`
}

// Runner executes an external command. Production uses exec; tests inject fakes.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout, stderr []byte, code int, err error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	hide(cmd)
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code, err = ee.ExitCode(), nil
	}
	return out.Bytes(), errb.Bytes(), code, err
}

// Manager installs, inspects and removes the managed task.
type Manager struct {
	TaskName string
	Runner   Runner
}

// NewManager returns a Manager using the real Task Scheduler.
func NewManager() *Manager { return &Manager{TaskName: DefaultTaskName, Runner: execRunner{}} }

func (m *Manager) schtasks(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	out, errb, code, err := m.Runner.Run(ctx, "schtasks.exe", args, stdin)
	if err != nil {
		return nil, fmt.Errorf("schtasks.exe could not be started: %w", err)
	}
	if code != 0 {
		msg := strings.TrimSpace(string(errb))
		if msg == "" {
			msg = strings.TrimSpace(string(out))
		}
		return out, fmt.Errorf("schtasks.exe failed (exit code %d): %s", code, msg)
	}
	return out, nil
}

// ValidateSpec checks a Spec.
func ValidateSpec(s Spec) error {
	if s.Frequency != "daily" && s.Frequency != "weekly" {
		return fmt.Errorf("invalid frequency %q", s.Frequency)
	}
	if _, _, err := parseTime(s.Time); err != nil {
		return err
	}
	if s.Frequency == "weekly" {
		if _, ok := dayElement(s.Day); !ok {
			return fmt.Errorf("invalid day %q", s.Day)
		}
	}
	if s.ExePath == "" || s.ConfigPath == "" {
		return errors.New("executable and configuration paths are required")
	}
	if strings.ContainsAny(s.ExePath+s.ConfigPath, "\"\r\n") {
		return errors.New("paths must not contain quotes or line breaks")
	}
	return nil
}

func parseTime(s string) (int, int, error) {
	var h, mi int
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, fmt.Errorf("invalid time %q (use HH:MM)", s)
	}
	if _, err := fmt.Sscanf(s, "%02d:%02d", &h, &mi); err != nil || h < 0 || h > 23 || mi < 0 || mi > 59 {
		return 0, 0, fmt.Errorf("invalid time %q (use HH:MM)", s)
	}
	return h, mi, nil
}

func dayElement(day string) (string, bool) {
	d := strings.ToLower(day)
	switch d {
	case "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
		return strings.ToUpper(d[:1]) + d[1:], true
	}
	return "", false
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// BuildXML renders the Task Scheduler definition. userSID may be empty, in
// which case the task runs as whoever registers it.
func BuildXML(s Spec, userSID string, now time.Time) (string, error) {
	if err := ValidateSpec(s); err != nil {
		return "", err
	}
	h, mi, _ := parseTime(s.Time)
	start := fmt.Sprintf("%s:00", now.Format("2006-01-02T")+fmt.Sprintf("%02d:%02d", h, mi))
	var trigger string
	if s.Frequency == "weekly" {
		d, _ := dayElement(s.Day)
		trigger = fmt.Sprintf("<ScheduleByWeek><DaysOfWeek><%s/></DaysOfWeek><WeeksInterval>1</WeeksInterval></ScheduleByWeek>", d)
	} else {
		trigger = "<ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay>"
	}
	limit := s.TimeLimit
	if limit <= 0 {
		limit = 4 * time.Hour
	}
	lh := int(limit.Hours())
	lm := int(limit.Minutes()) % 60
	principal := ""
	if userSID != "" {
		principal = "<UserId>" + esc(userSID) + "</UserId>"
	}
	args := fmt.Sprintf(`run --config "%s" --trigger schedule --quiet --no-color`, s.ConfigPath)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>%s</Description></RegistrationInfo>
  <Triggers>
    <CalendarTrigger><StartBoundary>%s</StartBoundary><Enabled>true</Enabled>%s</CalendarTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">%s<LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <ExecutionTimeLimit>PT%dH%dM</ExecutionTimeLimit>
    <Priority>7</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec><Command>%s</Command><Arguments>%s</Arguments></Exec>
  </Actions>
</Task>
`, esc(Marker), start, trigger, principal, lh, lm, esc(s.ExePath), esc(args)), nil
}

// encodePowerShell returns script as the base64 of its UTF-16LE form (-EncodedCommand).
func encodePowerShell(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// toUTF16LE encodes s with a byte order mark, as schtasks /XML expects.
func toUTF16LE(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 0, 2+len(u)*2)
	b = append(b, 0xFF, 0xFE)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return b
}

// Install creates or replaces the managed task. It refuses to replace a task
// BackupHarbor did not create.
func (m *Manager) Install(ctx context.Context, spec Spec) error {
	xmlText, err := BuildXML(spec, currentUserSID(), time.Now())
	if err != nil {
		return err
	}
	st, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if st.Installed && !st.Owned {
		return ErrForeignTask
	}
	f, err := os.CreateTemp("", "backupharbor-task-*.xml")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.Write(toUTF16LE(xmlText)); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if _, err := m.schtasks(ctx, nil, "/Create", "/TN", m.TaskName, "/XML", path, "/F"); err != nil {
		return err
	}
	// Never report success unless the task can actually be read back.
	after, err := m.Status(ctx)
	if err != nil {
		return fmt.Errorf("task was created but could not be verified: %w", err)
	}
	if !after.Installed || !after.Owned {
		return errors.New("task was created but could not be found afterwards")
	}
	return nil
}

const statusScript = `$ErrorActionPreference='Stop'
$p = '\BackupHarbor\'
try { $t = Get-ScheduledTask -TaskPath $p -TaskName 'Verify' } catch { Write-Output '{"installed":false}'; exit 0 }
$i = Get-ScheduledTaskInfo -InputObject $t
function Iso($d){ if($d -and $d.Year -gt 1999){ $d.ToUniversalTime().ToString('o') } else { $null } }
$tr = $t.Triggers | Select-Object -First 1
$a = $t.Actions | Select-Object -First 1
[pscustomobject]@{
  installed = $true
  state = [string]$t.State
  description = [string]$t.Description
  next = (Iso $i.NextRunTime)
  last = (Iso $i.LastRunTime)
  lastResult = [int64]$i.LastTaskResult
  execute = [string]$a.Execute
  arguments = [string]$a.Arguments
  trigger = [string]$tr.CimClass.CimClassName
  start = [string]$tr.StartBoundary
  days = [string]$tr.DaysOfWeek
} | ConvertTo-Json -Compress`

type statusJSON struct {
	Installed   bool    `json:"installed"`
	State       string  `json:"state"`
	Description string  `json:"description"`
	Next        *string `json:"next"`
	Last        *string `json:"last"`
	LastResult  *int64  `json:"lastResult"`
	Execute     string  `json:"execute"`
	Arguments   string  `json:"arguments"`
	Trigger     string  `json:"trigger"`
	Start       string  `json:"start"`
	Days        string  `json:"days"`
}

// Status queries the managed task.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	if m.TaskName != DefaultTaskName {
		// Non-default names (tests) are queried through schtasks.
		return m.statusViaSchtasks(ctx)
	}
	// The script is a constant without user input. It is passed encoded: reading a
	// multi-line script from stdin ("-Command -") silently produces no output.
	out, errb, code, err := m.Runner.Run(ctx, "powershell.exe",
		[]string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(statusScript)}, nil)
	if err != nil {
		return Status{}, fmt.Errorf("powershell.exe could not be started: %w", err)
	}
	if code != 0 {
		return Status{}, fmt.Errorf("querying the scheduled task failed (exit code %d): %s", code, strings.TrimSpace(string(errb)))
	}
	return parseStatus(out)
}

func parseStatus(out []byte) (Status, error) {
	out = bytes.TrimSpace(bytes.TrimPrefix(out, []byte("\xef\xbb\xbf")))
	var j statusJSON
	if err := json.Unmarshal(out, &j); err != nil {
		return Status{}, fmt.Errorf("unreadable task status: %w", err)
	}
	if !j.Installed {
		return Status{}, nil
	}
	s := Status{Installed: true, State: j.State, Owned: strings.Contains(j.Description, "BackupHarbor managed task"),
		Command: j.Execute, Arguments: j.Arguments, LastResult: j.LastResult}
	if j.Next != nil {
		if t, err := time.Parse(time.RFC3339Nano, *j.Next); err == nil {
			s.NextRun = &t
		}
	}
	if j.Last != nil {
		if t, err := time.Parse(time.RFC3339Nano, *j.Last); err == nil {
			s.LastRun = &t
		}
	}
	if len(j.Start) >= 16 {
		s.Time = j.Start[11:16]
	}
	if strings.Contains(j.Trigger, "Weekly") {
		s.Frequency = "weekly"
		var mask int
		fmt.Sscanf(j.Days, "%d", &mask)
		for i, name := range []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"} {
			if mask&(1<<i) != 0 {
				s.Day = name
				break
			}
		}
	} else if strings.Contains(j.Trigger, "Daily") {
		s.Frequency = "daily"
	}
	return s, nil
}

// statusViaSchtasks is a minimal existence/ownership probe using schtasks.
func (m *Manager) statusViaSchtasks(ctx context.Context) (Status, error) {
	out, errb, code, err := m.Runner.Run(ctx, "schtasks.exe", []string{"/Query", "/TN", m.TaskName, "/XML"}, nil)
	if err != nil {
		return Status{}, fmt.Errorf("schtasks.exe could not be started: %w", err)
	}
	if code != 0 {
		_ = errb
		return Status{}, nil
	}
	return Status{Installed: true, Owned: bytes.Contains(out, []byte("BackupHarbor managed task"))}, nil
}

// Remove deletes the managed task. A task not created by BackupHarbor is never removed.
func (m *Manager) Remove(ctx context.Context) error {
	st, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if !st.Installed {
		return ErrNotInstalled
	}
	if !st.Owned {
		return ErrForeignTask
	}
	if _, err := m.schtasks(ctx, nil, "/Delete", "/TN", m.TaskName, "/F"); err != nil {
		return err
	}
	after, err := m.Status(ctx)
	if err != nil {
		return fmt.Errorf("task was deleted but removal could not be verified: %w", err)
	}
	if after.Installed {
		return errors.New("the task is still present after deletion")
	}
	return nil
}
