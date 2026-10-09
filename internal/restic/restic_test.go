package restic

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	buildOnce sync.Once
	fakeExe   string
	buildErr  error
)

// fake builds the fake restic helper once per test binary.
func fake(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "fakerestic")
		if err != nil {
			buildErr = err
			return
		}
		fakeExe = filepath.Join(dir, "restic.exe")
		goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
		out, err := exec.Command(goBin, "build", "-o", fakeExe, "./testdata/fakerestic").CombinedOutput()
		if err != nil {
			buildErr = errors.New(string(out))
		}
	})
	if buildErr != nil {
		t.Fatalf("building fake restic: %v", buildErr)
	}
	return fakeExe
}

func client(t *testing.T, mode string) *Client {
	t.Helper()
	t.Setenv("FAKE_MODE", mode)
	return New(fake(t))
}

var repo = Repo{Location: `C:\repo`, Password: "hunter2-secret"}

func TestVersion(t *testing.T) {
	v, err := client(t, "version").Version(context.Background())
	if err != nil || v.Major != 0 || v.Minor != 19 || v.Patch != 1 {
		t.Fatalf("v=%+v err=%v", v, err)
	}
	if !v.AtLeast(0, 16) || v.AtLeast(0, 20) || v.AtLeast(1, 0) {
		t.Error("AtLeast wrong")
	}
	if _, err := ParseVersion("garbage"); err == nil {
		t.Error("garbage version accepted")
	}
}

func TestExitCodesAreClassified(t *testing.T) {
	cases := map[string]Kind{"exit12": KindWrongPassword, "exit10": KindNoRepo, "exit11": KindLocked, "legacy-wrongpw": KindWrongPassword}
	for mode, want := range cases {
		err := client(t, mode).CatConfig(context.Background(), repo)
		var re *Error
		if !errors.As(err, &re) || re.Kind != want {
			t.Errorf("%s: err=%v kind=%v want %v", mode, err, re, want)
		}
	}
}

func TestPasswordNeverOnCommandLineAndAmbientEnvStripped(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("FAKE_ARGS_FILE", argsFile)
	t.Setenv("RESTIC_PASSWORD_COMMAND", "evil-command")
	c := client(t, "snapshots")
	if _, err := c.Snapshots(context.Background(), repo, SnapshotFilter{Tags: []string{"a", "b"}, Host: "h"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(argsFile)
	got := string(b)
	lines := strings.Split(got, "\n")
	if strings.Contains(lines[0], "hunter2") || strings.Contains(lines[0], `C:\repo`) {
		t.Errorf("secret or repository on the command line: %q", lines[0])
	}
	for _, want := range []string{"snapshots --json", "--tag a,b", "--host h", "--no-lock", "PW=hunter2-secret", `REPO=C:\repo`, "AMBIENT_PWCMD=\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestUseLockOmitsNoLock(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("FAKE_ARGS_FILE", argsFile)
	r := repo
	r.UseLock = true
	_, _ = client(t, "snapshots").Snapshots(context.Background(), r, SnapshotFilter{})
	b, _ := os.ReadFile(argsFile)
	if strings.Contains(string(b), "--no-lock") {
		t.Error("--no-lock present although locking was requested")
	}
}

func TestSnapshotsSortedByTime(t *testing.T) {
	s, err := client(t, "snapshots").Snapshots(context.Background(), repo, SnapshotFilter{})
	if err != nil || len(s) != 2 || s[1].ShortID != "bbbb0000" {
		t.Fatalf("s=%+v err=%v", s, err)
	}
}

func TestSecretsAreRedactedInErrors(t *testing.T) {
	err := client(t, "leak").CatConfig(context.Background(), repo)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	var re *Error
	errors.As(err, &re)
	all := msg + "|" + re.Stderr
	for _, secret := range []string{"hunter2-secret", "s3cretpw"} {
		if strings.Contains(all, secret) {
			t.Errorf("secret %q leaked: %s", secret, all)
		}
	}
}

func TestCancellationKillsProcess(t *testing.T) {
	c := client(t, "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.CatConfig(ctx, repo)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if time.Since(start) > 15*time.Second {
		t.Error("process was not terminated promptly")
	}
}

func TestOutputLimit(t *testing.T) {
	old := maxCollect
	maxCollect = 1000
	defer func() { maxCollect = old }()
	_, err := client(t, "bigjson").Snapshots(context.Background(), repo, SnapshotFilter{})
	var re *Error
	if !errors.As(err, &re) || re.Kind != KindOutputLimit {
		t.Fatalf("err = %v", err)
	}
}

func TestLongLinesDoNotBlockOrBreakParsing(t *testing.T) {
	sum, err := client(t, "longline").Restore(context.Background(), repo, RestoreOptions{SnapshotID: "x", Target: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.HaveSummary || sum.FilesRestored != 3 {
		t.Errorf("summary after a 3 MiB line not parsed: %+v", sum)
	}
}

func TestRestoreParsesProgressAndSeparatesMetadataErrors(t *testing.T) {
	var last RestoreProgress
	sum, err := client(t, "restore-partial").Restore(context.Background(), repo, RestoreOptions{SnapshotID: "x", Target: t.TempDir(), Verify: true},
		func(p RestoreProgress) { last = p })
	var re *Error
	if !errors.As(err, &re) || re.ExitCode != 1 {
		t.Fatalf("err = %v", err)
	}
	if last.BytesRestored != 50 || last.PercentDone != 0.5 {
		t.Errorf("progress = %+v", last)
	}
	if sum.ErrorCount != 1 || sum.MetadataErrorCount != 1 || len(sum.Errors) != 2 {
		t.Errorf("summary = %+v", sum)
	}
	if !sum.Errors[0].Metadata || sum.Errors[1].Metadata {
		t.Errorf("metadata classification wrong: %+v", sum.Errors)
	}
}

func TestStartFailure(t *testing.T) {
	err := New(filepath.Join(t.TempDir(), "missing.exe")).CatConfig(context.Background(), repo)
	var re *Error
	if !errors.As(err, &re) || re.Kind != KindStart {
		t.Fatalf("err = %v", err)
	}
}

func TestLocate(t *testing.T) {
	if _, err := Locate(filepath.Join(t.TempDir(), "nope.exe")); err == nil {
		t.Error("missing configured path accepted")
	}
	if _, err := Locate(t.TempDir()); err == nil {
		t.Error("directory accepted as executable")
	}
	exe := fake(t)
	if got, err := Locate(exe); err != nil || got != exe {
		t.Errorf("got %q err %v", got, err)
	}
}

func TestIsMetadataError(t *testing.T) {
	for msg, want := range map[string]bool{
		"failed to restore timestamp of x: denied": true,
		"failed to restore security descriptor":    true,
		"unable to write file":                     false,
		"verify: hash mismatch":                    false,
	} {
		if IsMetadataError(msg) != want {
			t.Errorf("%q: want %v", msg, want)
		}
	}
}

func TestLocateOrder(t *testing.T) {
	appDir := t.TempDir()
	old := executableDir
	executableDir = func() string { return appDir }
	defer func() { executableDir = old }()

	// Nothing bundled and (in this sandbox) nothing on PATH: not found, with a clear message.
	t.Setenv("PATH", t.TempDir())
	if _, err := Locate(""); err == nil || !strings.Contains(err.Error(), "next to the application") {
		t.Fatalf("expected a not-found error, got %v", err)
	}

	// A copy next to the application is used.
	bundled := filepath.Join(appDir, BundledName)
	if err := os.WriteFile(bundled, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := Locate(""); err != nil || got != bundled {
		t.Fatalf("bundled copy not preferred: %q %v", got, err)
	}
	if p, ok := Bundled(); !ok || p != bundled {
		t.Errorf("Bundled() = %q %v", p, ok)
	}

	// The user's configured path always wins over the bundled copy.
	custom := filepath.Join(t.TempDir(), "my-restic.exe")
	os.WriteFile(custom, []byte("x"), 0o755)
	if got, err := Locate(custom); err != nil || got != custom {
		t.Fatalf("configured path must win: %q %v", got, err)
	}

	// A directory named restic.exe is not an executable.
	os.Remove(bundled)
	os.Mkdir(bundled, 0o755)
	if _, ok := Bundled(); ok {
		t.Error("a directory must not count as the bundled restic")
	}
}
