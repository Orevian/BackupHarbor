package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backupharbor/internal/i18n"
	"backupharbor/internal/verify"
)

func sampleRun(id string, start time.Time, status verify.Status) *verify.Run {
	r := verify.NewRun(id, verify.TriggerCLI, start)
	r.ResticVersion = "0.19.1"
	r.Targets = []verify.TargetResult{{
		Name: "daily-backup", Status: status, StartedAt: start, FinishedAt: start.Add(5 * time.Second), DurationMS: 5000,
		Repository: `D:\repo`,
		Snapshot:   &verify.SnapshotInfo{ID: "abcdef0123456789", ShortID: "abcdef01", Time: start.Add(-time.Hour), Hostname: "PC", Paths: []string{`C:\data`}, Tags: []string{"daily"}},
		Restore:    &verify.RestoreInfo{DurationMS: 2500, Files: 12, Directories: 3, Bytes: 4096, MetadataWarnings: 1},
		Stages: []verify.Stage{
			{ID: verify.StageCheck, Status: verify.Passed, DurationMS: 100, Message: i18n.M("check.passed_none")},
			{ID: verify.StageRestore, Status: status, DurationMS: 2500, Message: i18n.M("restore.ok", 12, "4.0 KiB", "2.5s"), Detail: "line one\nline two"},
			{ID: verify.StageContent, Status: verify.Failed, Message: i18n.M("content.failed", 1, 1), Rules: []verify.RuleResult{{
				Name: "<b>evil</b>", Type: "json", Pattern: "**/x.json", Status: verify.Failed, Message: i18n.M("rule.failed", 1, 1),
				Failures: []verify.FileFailure{{Path: `C/<script>alert(1)</script>.json`, Msg: i18n.M("check.json_invalid", `<img src=x onerror=alert(1)>`)}},
			}}},
		},
		Coverage: []i18n.Msg{i18n.M("cov.limits")},
	}}
	r.Finish(start.Add(6 * time.Second))
	return r
}

func TestJSONRoundTripAndSchema(t *testing.T) {
	r := sampleRun("20260101T000000Z-aaaaaa", time.Now(), verify.Failed)
	var buf bytes.Buffer
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	for _, k := range []string{"schema", "run_id", "app_version", "started_at", "finished_at", "status", "summary", "targets", "trigger", "demo"} {
		if _, ok := m[k]; !ok {
			t.Errorf("stable field %q missing", k)
		}
	}
	if m["schema"] != "backupharbor.run/1" {
		t.Errorf("schema = %v", m["schema"])
	}
	var back verify.Run
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil || back.RunID != r.RunID || back.Targets[0].Restore.Files != 12 {
		t.Errorf("round trip failed: %v", err)
	}
}

func TestHTMLEscapesUserControlledText(t *testing.T) {
	r := sampleRun("20260101T000000Z-bbbbbb", time.Now(), verify.Failed)
	r.Targets[0].Name = `<script>alert("t")</script>`
	r.Targets[0].Repository = `"><svg onload=alert(1)>`
	r.Targets[0].Stages[1].Detail = `</pre><script>alert(2)</script>`
	var buf bytes.Buffer
	if err := RenderHTML(&buf, r, "en"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, raw := range []string{"<script>alert", "<svg onload", "<img src=x", "<b>evil</b>", "</pre><script>"} {
		if strings.Contains(out, raw) {
			t.Errorf("unescaped content in HTML: %q", raw)
		}
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("expected escaped markup")
	}
}

func TestHTMLIsSelfContainedAndPrintable(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, sampleRun("20260101T000000Z-cccccc", time.Now(), verify.Passed), "tr"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, banned := range []string{"http://", "https://", "<script", "<link", "<img", "<iframe", "<object", "@import", "url("} {
		if strings.Contains(out, banned) {
			t.Errorf("report depends on something external: %q", banned)
		}
	}
	if !strings.Contains(out, "@media print") || !strings.Contains(out, `lang="tr"`) || !strings.Contains(out, "doğrulama raporu") {
		t.Error("print styles or Turkish localisation missing")
	}
}

func TestTXTRendersBothLanguages(t *testing.T) {
	r := sampleRun("20260101T000000Z-dddddd", time.Now(), verify.Partial)
	var en, tr bytes.Buffer
	RenderTXT(&en, r, "en")
	RenderTXT(&tr, r, "tr")
	for _, want := range []string{"BackupHarbor verification report", "daily-backup", "abcdef01", "PARTIAL", "Technical details"} {
		if !strings.Contains(en.String(), want) {
			t.Errorf("English TXT lacks %q", want)
		}
	}
	for _, want := range []string{"doğrulama raporu", "KISMİ", "Teknik ayrıntılar"} {
		if !strings.Contains(tr.String(), want) {
			t.Errorf("Turkish TXT lacks %q", want)
		}
	}
	if strings.Contains(en.String(), "%!") || strings.Contains(tr.String(), "%!") {
		t.Error("formatting noise in report")
	}
}

func TestDemoBannerShown(t *testing.T) {
	r := sampleRun("demo", time.Now(), verify.Passed)
	r.Demo = true
	var buf bytes.Buffer
	RenderTXT(&buf, r, "en")
	if !strings.Contains(buf.String(), "DEMONSTRATION") {
		t.Error("demo banner missing")
	}
}

func TestStoreSaveListLoadAndNeverOverwrites(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "reports"))
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	r1 := sampleRun("20260301T100000Z-111111", t0, verify.Passed)
	r2 := sampleRun("20260302T100000Z-222222", t0.Add(24*time.Hour), verify.Failed)
	for _, r := range []*verify.Run{r1, r2} {
		if _, err := s.Save(r, []string{"html", "txt"}, "en"); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"json", "html", "txt"} {
		if _, err := os.Stat(s.RunPath(r1.RunID, f)); err != nil {
			t.Errorf("%s not written: %v", f, err)
		}
	}
	// A run record is immutable.
	if _, err := s.Save(r1, nil, "en"); err == nil {
		t.Fatal("saving the same run id twice must fail")
	}
	list, err := s.List(Filter{})
	if err != nil || len(list) != 2 || list[0].RunID != r2.RunID {
		t.Fatalf("list newest first: %+v %v", list, err)
	}
	if got, _ := s.List(Filter{Target: "DAILY-backup"}); len(got) != 2 {
		t.Error("target filter should be case-insensitive")
	}
	if got, _ := s.List(Filter{Target: "other"}); len(got) != 0 {
		t.Error("unknown target matched")
	}
	if got, _ := s.List(Filter{Since: t0.Add(12 * time.Hour)}); len(got) != 1 || got[0].RunID != r2.RunID {
		t.Errorf("since filter: %+v", got)
	}
	if got, _ := s.List(Filter{Until: t0.Add(12 * time.Hour)}); len(got) != 1 || got[0].RunID != r1.RunID {
		t.Errorf("until filter: %+v", got)
	}
	if got, _ := s.List(Filter{Limit: 1}); len(got) != 1 {
		t.Error("limit ignored")
	}
	last, err := s.Last("")
	if err != nil || last.RunID != r2.RunID {
		t.Errorf("last: %v %v", last, err)
	}
	if _, err := s.Load("20990101T000000Z-zzzzzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing report error: %v", err)
	}
	if _, err := s.Load(`..\..\evil`); err == nil {
		t.Error("path traversal in run id accepted")
	}
}

func TestIndexIsRebuiltWhenMissingOrStale(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "reports"))
	t0 := time.Now().Add(-time.Hour)
	s.Save(sampleRun("20260301T100000Z-aaaaaa", t0, verify.Passed), nil, "en")
	s.Save(sampleRun("20260302T100000Z-bbbbbb", t0.Add(time.Minute), verify.Passed), nil, "en")

	os.Remove(filepath.Join(s.Dir, "index.jsonl"))
	if l, _ := s.List(Filter{}); len(l) != 2 {
		t.Errorf("rebuild from records: %d", len(l))
	}
	// A record written by another process without an index line is picked up.
	r3 := sampleRun("20260303T100000Z-cccccc", t0.Add(2*time.Minute), verify.Failed)
	f, _ := os.Create(s.RunPath(r3.RunID, "json"))
	RenderJSON(f, r3)
	f.Close()
	if l, _ := s.List(Filter{}); len(l) != 3 {
		t.Errorf("stale index not repaired: %d", len(l))
	}
	// Damaged index lines and partial records are skipped, not fatal.
	os.WriteFile(filepath.Join(s.Dir, "index.jsonl"), []byte("{broken\n"), 0o644)
	os.WriteFile(s.RunPath("20260304T000000Z-dddddd", "json"), []byte(`{"run_id":`), 0o644)
	if l, err := s.List(Filter{}); err != nil || len(l) != 3 {
		t.Errorf("damaged data: %d %v", len(l), err)
	}
}

func TestSaveRefusesDemoAndUnwritableDirectory(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "reports"))
	r := sampleRun("20260301T100000Z-eeeeee", time.Now(), verify.Passed)
	r.Demo = true
	if _, err := s.Save(r, nil, "en"); err == nil {
		t.Error("demo run stored in the real history")
	}
	file := filepath.Join(t.TempDir(), "iamafile")
	os.WriteFile(file, []byte("x"), 0o600)
	bad := NewStore(filepath.Join(file, "reports"))
	r.Demo = false
	if _, err := bad.Save(r, nil, "en"); err == nil {
		t.Error("an unwritable report directory must be reported")
	}
	if err := NewStore(`relative\dir`).Check(); err == nil {
		t.Error("relative report dir accepted")
	}
}

func TestExportNeverSilentlyOverwrites(t *testing.T) {
	r := sampleRun("20260301T100000Z-ffffff", time.Now(), verify.Passed)
	out := filepath.Join(t.TempDir(), "r.html")
	if err := Export(out, "html", r, "en", false); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(out)
	err := Export(out, "html", sampleRun("20260301T100000Z-gggggg", time.Now(), verify.Failed), "en", false)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite must be refused: %v", err)
	}
	if now, _ := os.ReadFile(out); !bytes.Equal(first, now) {
		t.Error("existing export modified")
	}
	if err := Export(out, "html", r, "en", true); err != nil {
		t.Errorf("forced overwrite failed: %v", err)
	}
	if err := Export(out+"2", "pdf", r, "en", false); err == nil {
		t.Error("unknown format accepted")
	}
	if entries, _ := os.ReadDir(filepath.Dir(out)); len(entries) != 1 {
		t.Errorf("stray files: %v", entries)
	}
}

func TestFilterTarget(t *testing.T) {
	r := sampleRun("20260301T100000Z-hhhhhh", time.Now(), verify.Passed)
	extra := r.Targets[0]
	extra.Name = "second"
	extra.Status = verify.Failed
	r.Targets = append(r.Targets, extra)
	r.Finish(r.FinishedAt)
	one, err := FilterTarget(r, "SECOND")
	if err != nil || len(one.Targets) != 1 || one.Status != verify.Failed || one.Summary.Total != 1 {
		t.Errorf("%+v %v", one, err)
	}
	if len(r.Targets) != 2 {
		t.Error("original run modified")
	}
	if _, err := FilterTarget(r, "nope"); err == nil {
		t.Error("unknown target accepted")
	}
}

func TestLatestPerTarget(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "reports"))
	t0 := time.Now().Add(-time.Hour)
	s.Save(sampleRun("20260301T100000Z-aaaaaa", t0, verify.Passed), nil, "en")
	s.Save(sampleRun("20260302T100000Z-bbbbbb", t0.Add(time.Minute), verify.Failed), nil, "en")
	m, err := s.LatestPerTarget()
	if err != nil || m["daily-backup"].Status != verify.Failed {
		t.Errorf("%+v %v", m, err)
	}
}
