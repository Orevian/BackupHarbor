package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var reVerb = regexp.MustCompile(`%(\[\d+\])?[a-z]`)

func TestCatalogsHaveIdenticalKeys(t *testing.T) {
	en, tr := Catalog(EN), Catalog(TR)
	for k := range en {
		if _, ok := tr[k]; !ok {
			t.Errorf("missing Turkish translation for %q", k)
		}
	}
	for k := range tr {
		if _, ok := en[k]; !ok {
			t.Errorf("Turkish key %q has no English counterpart", k)
		}
	}
	if len(en) < 100 {
		t.Errorf("English catalog suspiciously small: %d", len(en))
	}
}

func TestPlaceholderCountsMatchAcrossLanguages(t *testing.T) {
	en, tr := Catalog(EN), Catalog(TR)
	for k, e := range en {
		tt := tr[k]
		if ce, ct := len(reVerb.FindAllString(e, -1)), len(reVerb.FindAllString(tt, -1)); ce != ct {
			t.Errorf("%s: %d placeholders in English but %d in Turkish", k, ce, ct)
		}
	}
}

func TestNoEmptyTranslations(t *testing.T) {
	for _, lang := range Languages {
		for k, v := range Catalog(lang) {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s/%s is empty", lang, k)
			}
		}
	}
}

// Every message key referenced from Go source must exist in the catalog.
func TestAllKeysUsedInCodeExist(t *testing.T) {
	prefixes := `cfg|check|cleanup|cli|content|cov|dirs|password|repo|restic|restore|rpt|rule|run|scope|snapshot|stage|status|trigger|gui|ui`
	re := regexp.MustCompile(`"((?:` + prefixes + `)\.[a-z0-9_]+)"`)
	en := Catalog(EN)
	root := filepath.Join("..", "..")
	used := map[string][]string{}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return fs.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			used[m[1]] = append(used[m[1]], filepath.Base(p))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for k, files := range used {
		// "x_" is a dynamic prefix; restic.path and restic.exe are a field name and a file name, not messages.
		if strings.HasSuffix(k, "_") || k == "restic.path" || k == "restic.exe" {
			continue
		}
		if _, ok := en[k]; !ok {
			missing = append(missing, k+" (in "+files[0]+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("keys used in code but missing from the catalog:\n  %s", strings.Join(missing, "\n  "))
	}
	// Dynamic keys built at run time.
	for _, k := range []string{
		"stage.restic", "stage.directories", "stage.password", "stage.repository", "stage.snapshot", "stage.check",
		"stage.restore", "stage.content", "stage.cleanup", "stage.internal",
		"status.passed", "status.failed", "status.not_run", "status.cancelled", "status.timed_out", "status.partial", "status.skipped",
		"trigger.cli", "trigger.gui", "trigger.schedule",
		"check.passed_none", "check.passed_subset", "check.passed_all",
	} {
		if _, ok := en[k]; !ok {
			t.Errorf("dynamic key %q missing", k)
		}
	}
}

// Every data-i18n / t('...') key used by the GUI front end must exist.
func TestFrontendKeysExist(t *testing.T) {
	dir := filepath.Join("..", "..", "frontend")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("no frontend directory")
	}
	reAttr := regexp.MustCompile(`data-i18n(?:-[a-z]+)?="([a-z0-9_.]+)"`)
	// Any quoted catalog-looking literal ("ui.save", 'nav.reports', ...) counts as a use.
	reCall := regexp.MustCompile(`['"]((?:nav|ui|rpt|cli|restic|cfg|gui|check|cov)\.[a-z0-9_]*[a-z0-9])['"]`)
	en := Catalog(EN)
	// Keys the front end builds at run time.
	for _, k := range []string{"ui.rt_exists", "ui.rt_non_empty", "ui.rt_text", "ui.rt_json", "ui.rt_xml", "ui.rt_zip", "ui.rt_pdf", "ui.rt_sqlite", "ui.rt_sha256",
		"ui.rd_cost_none", "ui.rd_cost_subset", "ui.rd_cost_all", "ui.scope_check_none", "ui.scope_check_subset", "ui.scope_check_all",
		"ui.pw_source_credential", "ui.pw_source_env", "ui.pw_source_file",
		"ui.day_monday", "ui.day_tuesday", "ui.day_wednesday", "ui.day_thursday", "ui.day_friday", "ui.day_saturday", "ui.day_sunday"} {
		if _, ok := en[k]; !ok {
			t.Errorf("dynamic front end key %q missing", k)
		}
	}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(p); ext != ".html" && ext != ".js" {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, re := range []*regexp.Regexp{reAttr, reCall} {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				if _, ok := en[m[1]]; !ok {
					t.Errorf("%s uses missing key %q", filepath.Base(p), m[1])
				}
			}
		}
		return nil
	})
}

func TestMsgRendering(t *testing.T) {
	m := M("rule.failed", 2, 5)
	if m.Text != "2 of 5 file(s) failed." {
		t.Errorf("English text: %q", m.Text)
	}
	if got := m.In(TR); got != "5 dosyadan 2 tanesi başarısız." {
		t.Errorf("Turkish argument order wrong: %q", got)
	}
	if got := (Msg{Code: "no.such.key", Text: "fallback"}).In(TR); got != "fallback" {
		t.Errorf("fallback = %q", got)
	}
	if got := T(EN, "no.such.key"); got != "no.such.key" {
		t.Errorf("missing key should be visible, got %q", got)
	}
	// Numeric args work with %s; surplus args never produce EXTRA noise.
	if got := T(EN, "rpt.counts", 1, 1, 0, 0, 0, 0); strings.Contains(got, "%!") {
		t.Errorf("formatting noise: %q", got)
	}
	if got := T(EN, "check.exists", "surplus"); strings.Contains(got, "%!") {
		t.Errorf("surplus arg noise: %q", got)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"tr": TR, "TR-tr": TR, "tr_TR": TR, "en": EN, "en-US": EN, "de": EN, "fr-FR": EN} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q)=%q want %q", in, got, want)
		}
	}
	if !Valid("auto") || !Valid("tr") || Valid("de") {
		t.Error("Valid wrong")
	}
}
