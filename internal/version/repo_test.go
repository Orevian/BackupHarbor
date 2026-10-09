package version

import (
	"encoding/xml"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestVersionFileMatchesCode(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b)); got != Version {
		t.Errorf("VERSION file says %q but internal/version.Version defaults to %q", got, Version)
	}
	cl, err := os.ReadFile(filepath.Join(repoRoot(t), "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cl), "## ["+Version+"]") {
		t.Errorf("CHANGELOG.md has no section for %s", Version)
	}
}

func TestRequiredProjectFilesExist(t *testing.T) {
	for _, f := range []string{
		"README.md", "LICENSE", "CHANGELOG.md", "CONTRIBUTING.md", "SECURITY.md", ".gitignore", ".editorconfig",
		"docs/architecture.md", "docs/configuration.md", "docs/security.md", "docs/report-schema.md",
		".github/pull_request_template.md", ".github/RELEASE_TEMPLATE.md", ".github/workflows/ci.yml", ".github/workflows/release.yml",
		".github/ISSUE_TEMPLATE/bug_report.yml", ".github/ISSUE_TEMPLATE/feature_request.yml",
		"scripts/build.ps1", "scripts/make-icon.ps1", "installer/backupharbor.iss", "build/windows/icon.ico",
	} {
		if _, err := os.Stat(filepath.Join(repoRoot(t), filepath.FromSlash(f))); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}

func TestGitHubYAMLIsValid(t *testing.T) {
	root := filepath.Join(repoRoot(t), ".github")
	n := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yml") {
			return nil
		}
		n++
		b, _ := os.ReadFile(p)
		var v any
		if err := yaml.Unmarshal(b, &v); err != nil {
			t.Errorf("%s: %v", p, err)
		}
		return nil
	})
	if n < 4 {
		t.Errorf("expected at least 4 workflow/template files, found %d", n)
	}
}

func TestReleaseWorkflowOnlyPublishesAfterBuilding(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	build, publish := strings.Index(s, "scripts\\build.ps1"), strings.Index(s, "gh release create")
	if build < 0 || publish < 0 || build > publish {
		t.Error("the release must be created after the build and test step")
	}
	if !strings.Contains(s, "tags:") {
		t.Error("release workflow must be tag triggered")
	}
}

func TestManifestsAreWellFormedXML(t *testing.T) {
	for _, f := range []string{"gui.manifest", "cli.manifest"} {
		b, err := os.ReadFile(filepath.Join(repoRoot(t), "build", "windows", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := xml.Unmarshal(b, new(struct{ XMLName xml.Name })); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

// The product is BackupHarbor and its command is "bharbor". The names used before the
// rename (and the very first CLI name) must not survive anywhere in the repository.
// The literals are assembled at run time so this file does not contain them.
func TestLegacyNamesAreGone(t *testing.T) {
	root := repoRoot(t)
	legacy := []string{"Restore" + "Proof", "restore" + "proof", "RESTORE" + "PROOF", "r" + "proof", "RP_" + "TEST"}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, "repo_test.go") {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".go", ".md", ".yml", ".yaml", ".ps1", ".iss", ".json", ".html", ".js", ".mjs", ".txt", ".in", ".manifest":
			b, _ := os.ReadFile(p)
			low := string(b) // case-sensitive: "OtherProof" must not match the old CLI name
			for _, old := range legacy {
				if strings.Contains(low, old) {
					t.Errorf("%s mentions the legacy name %q", p, old)
				}
			}
		}
		// File and folder names too.
		for _, old := range legacy {
			if strings.Contains(filepath.Base(p), old) {
				t.Errorf("%s carries the legacy name %q", p, old)
			}
		}
		return nil
	})
}

// The CI/release workflows install restic for the tests; they must use the very release the build bundles.
func TestResticPinIsConsistent(t *testing.T) {
	root := repoRoot(t)
	pin := map[string]string{}
	b, err := os.ReadFile(filepath.Join(root, "third_party", "restic", "PIN"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			pin[k] = v
		}
	}
	for _, k := range []string{"version", "url", "sha256"} {
		if pin[k] == "" {
			t.Fatalf("PIN lacks %s", k)
		}
	}
	if len(pin["sha256"]) != 64 || !strings.Contains(pin["url"], "v"+pin["version"]+"/") || !strings.HasPrefix(pin["url"], "https://github.com/restic/restic/releases/download/") {
		t.Errorf("PIN looks wrong: %v", pin)
	}
	for _, wf := range []string{"ci.yml", "release.yml"} {
		w, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", wf))
		if !strings.Contains(string(w), pin["sha256"]) || !strings.Contains(string(w), `"`+pin["version"]+`"`) {
			t.Errorf("%s does not use the pinned restic %s", wf, pin["version"])
		}
	}
	if _, err := os.Stat(filepath.Join(root, "third_party", "restic", "LICENSE")); err != nil {
		t.Error("restic licence text missing")
	}
	n, _ := os.ReadFile(filepath.Join(root, "THIRD-PARTY-NOTICES.md"))
	if !strings.Contains(string(n), "restic") {
		t.Error("THIRD-PARTY-NOTICES.md does not mention restic")
	}
}

// The README is a single bilingual file (English first, Turkish below) whose screenshots live in the
// top-level screenshots folder and are shown inline.
func TestReadmeIsSingleBilingualFileWithInlineScreenshots(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "README.tr.md")); err == nil {
		t.Error("README.tr.md must not exist; the Turkish text belongs in README.md below the English text")
	}
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	en := strings.Index(text, "## The problem")
	tr := strings.Index(text, "## Sorun")
	if en < 0 || tr < 0 || en > tr {
		t.Fatalf("README.md must contain the English text before the Turkish text (English at %d, Turkish at %d)", en, tr)
	}
	refs := regexp.MustCompile(`screenshots/[A-Za-z0-9._-]+\.png`).FindAllString(text, -1)
	if len(refs) < 8 {
		t.Errorf("README.md shows only %d screenshots", len(refs))
	}
	if strings.Contains(text, "docs/screenshots") {
		t.Error("README.md must not point into docs/screenshots")
	}
	for _, ref := range refs {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ref))); err != nil {
			t.Errorf("README.md references %s, which does not exist", ref)
		}
	}
}
