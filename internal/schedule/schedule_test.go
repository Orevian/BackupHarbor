package schedule

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

type call struct {
	name  string
	args  []string
	stdin []byte
}

type fakeRunner struct {
	calls   []call
	handler func(c call) (out []byte, errOut []byte, code int, err error)
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, stdin []byte) ([]byte, []byte, int, error) {
	c := call{name, args, stdin}
	f.calls = append(f.calls, c)
	return f.handler(c)
}

func (f *fakeRunner) schtasks(sub string) *call {
	for i := range f.calls {
		if f.calls[i].name == "schtasks.exe" && len(f.calls[i].args) > 0 && f.calls[i].args[0] == sub {
			return &f.calls[i]
		}
	}
	return nil
}

var spec = Spec{Frequency: "daily", Time: "03:30", ExePath: `C:\Program Files\BackupHarbor\bharbor.exe`, ConfigPath: `C:\Users\me\My & Config\config.yaml`, TimeLimit: 3 * time.Hour}

const ownedJSON = `{"installed":true,"state":"Ready","description":"BackupHarbor managed task. Edit...","next":"2026-10-10T00:30:00.0000000Z","last":null,"lastResult":0,"execute":"x","arguments":"y","trigger":"MSFT_TaskDailyTrigger","start":"2026-10-09T03:30:00","days":""}`
const foreignJSON = `{"installed":true,"state":"Ready","description":"someone else","execute":"x"}`

func TestBuildXML(t *testing.T) {
	x, err := BuildXML(spec, "S-1-5-21-1", time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<StartBoundary>2026-10-09T03:30:00</StartBoundary>", "<ScheduleByDay>", "<LogonType>InteractiveToken</LogonType>",
		"<RunLevel>LeastPrivilege</RunLevel>", "<UserId>S-1-5-21-1</UserId>", "<ExecutionTimeLimit>PT3H0M</ExecutionTimeLimit>",
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>", "BackupHarbor managed task",
		`<Command>C:\Program Files\BackupHarbor\bharbor.exe</Command>`,
		"My &amp; Config", "--trigger schedule", "--quiet",
	} {
		if !strings.Contains(x, want) {
			t.Errorf("XML lacks %q", want)
		}
	}
	if strings.Contains(x, "My & Config") {
		t.Error("ampersand not escaped")
	}
	weekly := spec
	weekly.Frequency, weekly.Day = "weekly", "friday"
	wx, _ := BuildXML(weekly, "", time.Now())
	if !strings.Contains(wx, "<ScheduleByWeek><DaysOfWeek><Friday/></DaysOfWeek>") {
		t.Errorf("weekly trigger wrong: %s", wx)
	}
}

func TestValidateSpec(t *testing.T) {
	bad := []Spec{
		{Frequency: "hourly", Time: "03:00", ExePath: "a", ConfigPath: "b"},
		{Frequency: "daily", Time: "3am", ExePath: "a", ConfigPath: "b"},
		{Frequency: "daily", Time: "24:00", ExePath: "a", ConfigPath: "b"},
		{Frequency: "weekly", Time: "03:00", Day: "someday", ExePath: "a", ConfigPath: "b"},
		{Frequency: "daily", Time: "03:00", ExePath: "", ConfigPath: "b"},
		{Frequency: "daily", Time: "03:00", ExePath: "a", ConfigPath: `b" --evil "`},
	}
	for i, s := range bad {
		if ValidateSpec(s) == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	if err := ValidateSpec(spec); err != nil {
		t.Error(err)
	}
}

func TestInstallFailureIsNeverReportedAsSuccess(t *testing.T) {
	fr := &fakeRunner{handler: func(c call) ([]byte, []byte, int, error) {
		if c.name == "powershell.exe" {
			return []byte(`{"installed":false}`), nil, 0, nil
		}
		return nil, []byte("ERROR: Access is denied."), 1, nil
	}}
	m := &Manager{TaskName: DefaultTaskName, Runner: fr}
	err := m.Install(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "Access is denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestInstallMustBeVerifiedByReadingTheTaskBack(t *testing.T) {
	// schtasks claims success but the task does not exist afterwards.
	fr := &fakeRunner{handler: func(c call) ([]byte, []byte, int, error) {
		if c.name == "powershell.exe" {
			return []byte(`{"installed":false}`), nil, 0, nil
		}
		return []byte("SUCCESS"), nil, 0, nil
	}}
	m := &Manager{TaskName: DefaultTaskName, Runner: fr}
	if err := m.Install(context.Background(), spec); err == nil {
		t.Fatal("install reported success without a task")
	}
}

func TestInstallSuccessUsesXMLAndRestrictedArguments(t *testing.T) {
	installed := false
	var xmlBytes []byte
	fr := &fakeRunner{}
	fr.handler = func(c call) ([]byte, []byte, int, error) {
		switch {
		case c.name == "powershell.exe":
			if installed {
				return []byte(ownedJSON), nil, 0, nil
			}
			return []byte(`{"installed":false}`), nil, 0, nil
		case c.args[0] == "/Create":
			installed = true
			for i, a := range c.args {
				if a == "/XML" {
					xmlBytes, _ = os.ReadFile(c.args[i+1])
				}
			}
			return nil, nil, 0, nil
		}
		return nil, nil, 0, nil
	}
	m := &Manager{TaskName: DefaultTaskName, Runner: fr}
	if err := m.Install(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	create := fr.schtasks("/Create")
	if create == nil || create.args[2] != DefaultTaskName {
		t.Fatalf("create call: %+v", create)
	}
	// The task name is passed as its own argument, never concatenated into a shell string.
	for _, a := range create.args {
		if strings.Contains(a, " /") && a != DefaultTaskName {
			t.Errorf("suspicious combined argument %q", a)
		}
	}
	if len(xmlBytes) < 2 || xmlBytes[0] != 0xFF || xmlBytes[1] != 0xFE {
		t.Error("task XML must be UTF-16LE with BOM")
	}
}

func TestForeignTaskIsNeverTouched(t *testing.T) {
	fr := &fakeRunner{handler: func(c call) ([]byte, []byte, int, error) {
		if c.name == "powershell.exe" {
			return []byte(foreignJSON), nil, 0, nil
		}
		t.Errorf("schtasks must not be called for a foreign task: %v", c.args)
		return nil, nil, 0, nil
	}}
	m := &Manager{TaskName: DefaultTaskName, Runner: fr}
	if err := m.Install(context.Background(), spec); !errors.Is(err, ErrForeignTask) {
		t.Errorf("install: %v", err)
	}
	if err := m.Remove(context.Background()); !errors.Is(err, ErrForeignTask) {
		t.Errorf("remove: %v", err)
	}
}

func TestRemove(t *testing.T) {
	present := true
	fr := &fakeRunner{}
	fr.handler = func(c call) ([]byte, []byte, int, error) {
		if c.name == "powershell.exe" {
			if present {
				return []byte(ownedJSON), nil, 0, nil
			}
			return []byte(`{"installed":false}`), nil, 0, nil
		}
		if c.args[0] == "/Delete" {
			present = false
		}
		return nil, nil, 0, nil
	}
	m := &Manager{TaskName: DefaultTaskName, Runner: fr}
	if err := m.Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(context.Background()); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("second remove: %v", err)
	}
	// Deletion that does not take effect is an error.
	present = true
	fr.handler = func(c call) ([]byte, []byte, int, error) {
		if c.name == "powershell.exe" {
			return []byte(ownedJSON), nil, 0, nil
		}
		return nil, nil, 0, nil
	}
	if err := m.Remove(context.Background()); err == nil {
		t.Error("remove reported success while the task still exists")
	}
}

func TestStatusParsing(t *testing.T) {
	fr := &fakeRunner{handler: func(c call) ([]byte, []byte, int, error) {
		return []byte("\xef\xbb\xbf" + ownedJSON), nil, 0, nil
	}}
	st, err := (&Manager{TaskName: DefaultTaskName, Runner: fr}).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed || !st.Owned || st.State != "Ready" || st.Frequency != "daily" || st.Time != "03:30" || st.NextRun == nil {
		t.Errorf("status: %+v", st)
	}
	fr.handler = func(call) ([]byte, []byte, int, error) { return []byte("garbage"), nil, 0, nil }
	if _, err := (&Manager{TaskName: DefaultTaskName, Runner: fr}).Status(context.Background()); err == nil {
		t.Error("unreadable status accepted")
	}
	fr.handler = func(call) ([]byte, []byte, int, error) { return nil, []byte("boom"), 1, nil }
	if _, err := (&Manager{TaskName: DefaultTaskName, Runner: fr}).Status(context.Background()); err == nil {
		t.Error("failing query accepted")
	}
	fr.handler = func(call) ([]byte, []byte, int, error) { return nil, nil, 0, errors.New("no powershell") }
	if _, err := (&Manager{TaskName: DefaultTaskName, Runner: fr}).Status(context.Background()); err == nil {
		t.Error("missing powershell accepted")
	}
}

// Real Task Scheduler round trip with a unique throw-away task. Opt in with BH_TEST_SCHEDULER=1.
func TestRealTaskScheduler(t *testing.T) {
	if os.Getenv("BH_TEST_SCHEDULER") != "1" {
		t.Skip("set BH_TEST_SCHEDULER=1 to run the real Task Scheduler test (creates and deletes a task)")
	}
	m := NewManager()
	m.TaskName = `\BackupHarbor\UnitTest-` + strings.ToLower(time.Now().Format("150405"))
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfg, []byte("version: 1\n"), 0o600)
	sp := Spec{Frequency: "weekly", Day: "monday", Time: "04:15", ExePath: filepath.Join(dir, "bharbor.exe"), ConfigPath: cfg, TimeLimit: time.Hour}
	t.Cleanup(func() { _ = m.schtasksIgnore(context.Background()) })
	if err := m.Install(context.Background(), sp); err != nil {
		t.Fatalf("install: %v", err)
	}
	st, err := m.Status(context.Background())
	if err != nil || !st.Installed || !st.Owned {
		t.Fatalf("status after install: %+v %v", st, err)
	}
	if err := m.Remove(context.Background()); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if st, _ := m.Status(context.Background()); st.Installed {
		t.Error("task still installed")
	}
}

func (m *Manager) schtasksIgnore(ctx context.Context) error {
	_, err := m.schtasks(ctx, nil, "/Delete", "/TN", m.TaskName, "/F")
	return err
}

// Read-only query of the real Task Scheduler through the production PowerShell path
// (the default task name). It creates and changes nothing.
func TestRealStatusQueryThroughPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	st, err := NewManager().Status(context.Background())
	if err != nil {
		t.Fatalf("status query failed: %v", err)
	}
	if st.Installed && !st.Owned {
		t.Log("a foreign task with this name exists; it is reported as not owned")
	}
}

func TestEncodedCommandRoundTrip(t *testing.T) {
	enc := encodePowerShell("Write-Output 'héllo'")
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || len(raw)%2 != 0 {
		t.Fatalf("bad encoding: %v", err)
	}
	var u []uint16
	for i := 0; i < len(raw); i += 2 {
		u = append(u, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	if got := string(utf16.Decode(u)); got != "Write-Output 'héllo'" {
		t.Errorf("decoded %q", got)
	}
}
