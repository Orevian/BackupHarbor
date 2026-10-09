// Package restic runs the restic command-line tool and interprets its output.
//
// Safety properties: restic is always started directly (never through a shell),
// arguments are passed separately, the repository location and password are
// handed over via environment variables (never on the command line), output is
// read with hard size limits, and on cancellation the whole process tree is
// terminated.
package restic

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"backupharbor/internal/secret"
)

// Repo identifies a repository and the credentials needed to open it.
type Repo struct {
	Location string
	Password string
	// UseLock makes restic take its normal repository locks. By default
	// BackupHarbor passes --no-lock so verification never writes to the repository.
	UseLock bool
}

// Kind classifies a restic failure.
type Kind int

// Failure kinds.
const (
	KindGeneric       Kind = iota // exit 1 / unknown
	KindIncomplete                // exit 3: source data could not be read (restore partially failed)
	KindNoRepo                    // repository does not exist or is unreachable
	KindLocked                    // repository is locked
	KindWrongPassword             // password rejected
	KindStart                     // restic could not be started
	KindOutputLimit               // output exceeded safety limits
)

// Error is a failed restic invocation. Stderr is already redacted.
type Error struct {
	Command  string // sub-command, e.g. "check"
	ExitCode int
	Kind     Kind
	Stderr   string
	Err      error
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("restic %s failed (exit code %d)", e.Command, e.ExitCode)
	if e.Err != nil {
		msg = fmt.Sprintf("restic %s: %v", e.Command, e.Err)
	}
	if e.Stderr != "" {
		msg += ": " + lastLine(e.Stderr)
	}
	return msg
}

// Unwrap exposes the underlying cause.
func (e *Error) Unwrap() error { return e.Err }

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

// Version is a parsed restic version.
type Version struct {
	Major, Minor, Patch int
	Raw                 string
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// AtLeast reports whether v >= major.minor.
func (v Version) AtLeast(major, minor int) bool {
	return v.Major > major || (v.Major == major && v.Minor >= minor)
}

var reVersion = regexp.MustCompile(`restic\s+(\d+)\.(\d+)(?:\.(\d+))?`)

// ParseVersion parses the output of "restic version".
func ParseVersion(out string) (Version, error) {
	m := reVersion.FindStringSubmatch(out)
	if m == nil {
		return Version{}, fmt.Errorf("unrecognised restic version output: %q", strings.TrimSpace(out))
	}
	v := Version{Raw: strings.TrimSpace(out)}
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		v.Patch, _ = strconv.Atoi(m[3])
	}
	return v, nil
}

// Limits protecting the application from runaway output.
const (
	maxCollectDefault = 128 << 20 // largest accepted JSON document
	maxLine           = 1 << 20   // longest accepted streaming line
	stderrTailMax     = 32 << 10  // bytes of stderr/diagnostics kept
)

// Client runs a specific restic executable.
type Client struct {
	Exe string
	// Logf receives debug lines (already redacted). May be nil.
	Logf func(format string, args ...any)
}

// New returns a client for the executable at exe.
func New(exe string) *Client { return &Client{Exe: exe} }

// BundledName is the file name of the restic copy shipped next to the application.
const BundledName = "restic.exe"

// executableDir returns the directory of the running executable (a variable so tests can change it).
var executableDir = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// Bundled returns the restic that is installed next to the application, if any.
func Bundled() (string, bool) {
	dir := executableDir()
	if dir == "" {
		return "", false
	}
	p := filepath.Join(dir, BundledName)
	if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
		return p, true
	}
	return "", false
}

// Locate resolves the restic executable. Order: the path configured by the user,
// the copy shipped next to the application, then the first restic on PATH.
func Locate(configured string) (string, error) {
	if configured != "" {
		fi, err := os.Stat(configured)
		if err != nil {
			return "", fmt.Errorf("restic executable %q: %w", configured, err)
		}
		if fi.IsDir() {
			return "", fmt.Errorf("restic executable %q is a directory", configured)
		}
		return configured, nil
	}
	if p, ok := Bundled(); ok {
		return p, nil
	}
	p, err := exec.LookPath("restic")
	if err != nil {
		return "", fmt.Errorf("restic was not found (neither next to the application nor in PATH): %w", err)
	}
	return p, nil
}

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// baseEnv returns the environment for restic: the parent environment minus
// variables that would redirect repository or password handling, plus ours.
func baseEnv(repo Repo) []string {
	drop := map[string]bool{
		"RESTIC_REPOSITORY": true, "RESTIC_REPOSITORY_FILE": true,
		"RESTIC_PASSWORD": true, "RESTIC_PASSWORD_FILE": true, "RESTIC_PASSWORD_COMMAND": true,
		"RESTIC_KEY_HINT": true, "RESTIC_FROM_REPOSITORY": true, "RESTIC_FROM_PASSWORD": true,
		"RESTIC_FROM_PASSWORD_FILE": true, "RESTIC_FROM_PASSWORD_COMMAND": true,
		"RESTIC_FROM_REPOSITORY_FILE": true, "RESTIC_FROM_KEY_HINT": true,
		"RESTIC_PROGRESS_FPS": true, "RESTIC_READ_CONCURRENCY": false,
	}
	var env []string
	for _, kv := range os.Environ() {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		if drop[strings.ToUpper(name)] {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "RESTIC_REPOSITORY="+repo.Location, "RESTIC_PROGRESS_FPS=2")
	if repo.Password != "" {
		env = append(env, "RESTIC_PASSWORD="+repo.Password)
	}
	return env
}

// result of one invocation.
type result struct {
	stdout   []byte // only when collect is true
	tail     string // redacted diagnostics tail (stderr + non-JSON stdout)
	exitCode int
}

type runSpec struct {
	command string
	args    []string
	repo    Repo
	collect bool              // collect stdout up to maxCollect
	onLine  func(line []byte) // stream stdout lines
	noRepo  bool              // command does not need a repository (version)
}

func (c *Client) run(ctx context.Context, sp runSpec) (*result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	args := append([]string{}, sp.args...)
	if !sp.noRepo && !sp.repo.UseLock {
		args = append(args, "--no-lock")
	}
	red := secret.NewRedactor(sp.repo.Password)
	c.logf("restic %s", red.Redact(strings.Join(args, " ")))

	cmd := exec.Command(c.Exe, args...)
	if sp.noRepo {
		cmd.Env = os.Environ()
	} else {
		cmd.Env = baseEnv(sp.repo)
	}
	cmd.Stdin = nil
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	hideWindow(cmd)
	grp, err := newProcGroup()
	if err != nil {
		return nil, err
	}
	defer grp.Close()

	if err := cmd.Start(); err != nil {
		return nil, &Error{Command: sp.command, ExitCode: -1, Kind: KindStart, Err: err}
	}
	grp.Attach(cmd)

	var (
		wg       sync.WaitGroup
		out      bytes.Buffer
		overflow bool
		tail     = &tailBuffer{max: stderrTailMax}
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if sp.collect {
			n, _ := io.Copy(&out, io.LimitReader(stdout, maxCollect+1))
			if n > maxCollect {
				overflow = true
			}
			_, _ = io.Copy(io.Discard, stdout) // always drain
			return
		}
		readLines(stdout, func(line []byte) {
			if sp.onLine != nil {
				sp.onLine(line)
			}
			// Non-JSON stdout (e.g. "restic check") is diagnostics too.
			if len(line) > 0 && line[0] != '{' {
				tail.Write(append(append([]byte{}, line...), '\n'))
			}
		})
	}()
	go func() {
		defer wg.Done()
		// With --json restic reports errors as JSON lines on stderr.
		readLines(stderr, func(line []byte) {
			if sp.onLine != nil && len(line) > 0 && line[0] == '{' {
				sp.onLine(line)
			}
			tail.Write(append(append([]byte{}, line...), '\n'))
		})
	}()

	waitCh := make(chan error, 1)
	go func() { wg.Wait(); waitCh <- cmd.Wait() }()

	var waitErr error
	select {
	case waitErr = <-waitCh:
	case <-ctx.Done():
		grp.Kill()
		select {
		case waitErr = <-waitCh:
		case <-time.After(10 * time.Second):
			// Pipes held open by an orphaned grandchild: stop waiting.
			_ = stdout.Close()
			_ = stderr.Close()
			waitErr = ctx.Err()
		}
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
	}

	res := &result{stdout: out.Bytes(), tail: red.Redact(strings.TrimSpace(tail.String()))}
	if overflow {
		return res, &Error{Command: sp.command, Kind: KindOutputLimit, ExitCode: -1,
			Err: errors.New("output exceeded the safety limit")}
	}
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			res.exitCode = ee.ExitCode()
			return res, &Error{Command: sp.command, ExitCode: res.exitCode,
				Kind: classify(res.exitCode, res.tail), Stderr: res.tail}
		}
		return res, &Error{Command: sp.command, ExitCode: -1, Kind: KindGeneric, Err: waitErr, Stderr: res.tail}
	}
	return res, nil
}

// classify maps a restic exit code (and, for older versions, its message) to a Kind.
func classify(code int, msg string) Kind {
	switch code {
	case 3:
		return KindIncomplete
	case 10:
		return KindNoRepo
	case 11:
		return KindLocked
	case 12:
		return KindWrongPassword
	}
	l := strings.ToLower(msg)
	switch {
	case strings.Contains(l, "wrong password"):
		return KindWrongPassword
	case strings.Contains(l, "is there a repository at the following location") ||
		strings.Contains(l, "repository does not exist"):
		return KindNoRepo
	case strings.Contains(l, "unable to create lock") || strings.Contains(l, "repository is already locked"):
		return KindLocked
	}
	return KindGeneric
}

// readLines reads newline separated records, never blocking the child: lines
// longer than maxLine are truncated and the rest of the line is skipped.
func readLines(r io.Reader, fn func([]byte)) {
	br := bufio.NewReaderSize(r, 64<<10)
	var cur []byte
	truncated := false
	for {
		chunk, err := br.ReadSlice('\n')
		if len(chunk) > 0 && !truncated {
			if len(cur)+len(chunk) > maxLine {
				chunk = chunk[:maxLine-len(cur)]
				truncated = true
			}
			cur = append(cur, chunk...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if len(cur) > 0 || truncated {
			fn(bytes.TrimRight(cur, "\r\n"))
		}
		cur = cur[:0]
		truncated = false
		if err != nil {
			return
		}
	}
}

// tailBuffer keeps only the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// maxCollect is the largest accepted JSON document (a variable so tests can lower it).
var maxCollect int64 = maxCollectDefault
