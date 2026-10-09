package demo

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backupharbor/internal/i18n"
	"backupharbor/internal/verify"
)

// The demo package must be incapable of reaching a repository: it may not import
// anything that runs processes, touches the filesystem or the network, or the restic client.
func TestDemoCannotTouchRealRepositories(t *testing.T) {
	banned := []string{"os/exec", "backupharbor/internal/restic", "net", "net/http", "os", "backupharbor/internal/workspace",
		"backupharbor/internal/fsx", "backupharbor/internal/secret", "backupharbor/internal/schedule", "io/ioutil"}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, b := range banned {
				if path == b {
					t.Errorf("%s imports %s", f, b)
				}
			}
		}
	}
	_ = os.Getpid
}

func TestEveryDemoResultIsMarkedAndCoversEveryStatus(t *testing.T) {
	now := time.Now()
	seen := map[verify.Status]bool{}
	for _, r := range History(now) {
		if !r.Demo || !strings.HasPrefix(r.RunID, "DEMO-") {
			t.Errorf("run %s not clearly marked as demo", r.RunID)
		}
		for _, tg := range r.Targets {
			seen[tg.Status] = true
			for _, s := range tg.Stages {
				// Messages must exist in both languages.
				for _, lang := range i18n.Languages {
					if s.Message.Code != "" && s.Message.In(lang) == s.Message.Code {
						t.Errorf("%s/%s: message %s untranslated in %s", tg.Name, s.ID, s.Message.Code, lang)
					}
				}
			}
		}
	}
	for _, want := range []verify.Status{verify.Passed, verify.Partial, verify.Failed, verify.NotRun} {
		if !seen[want] {
			t.Errorf("demo data lacks a %s example", want)
		}
	}
	if Latest(now).Status == verify.Passed {
		t.Error("the demo must not suggest everything is always fine")
	}
}

func TestScriptHasNoRealWork(t *testing.T) {
	var total time.Duration
	for _, s := range Script() {
		total += s.Delay
	}
	if total > 10*time.Second || len(Script()) == 0 {
		t.Errorf("simulated run takes %v", total)
	}
}
