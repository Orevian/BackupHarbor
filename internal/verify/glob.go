package verify

import (
	"path"
	"runtime"
	"strings"
)

// normalizeGlob converts a user pattern to slash form without leading "./".
func normalizeGlob(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// matcher matches slash-separated relative paths against a glob in which "**"
// matches any number of directories (including none). Other segments use the
// path.Match syntax (*, ?, [class]). Matching is case-insensitive on Windows.
type matcher struct{ segs []string }

func newMatcher(pattern string) *matcher {
	p := normalizeGlob(pattern)
	return &matcher{segs: strings.Split(p, "/")}
}

func (m *matcher) Match(rel string) bool {
	if runtime.GOOS == "windows" {
		rel = strings.ToLower(rel)
	}
	return matchSegs(m.segs, strings.Split(rel, "/"))
}

func matchSegs(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Collapse consecutive ** and try every split point.
			for len(pat) > 0 && pat[0] == "**" {
				pat = pat[1:]
			}
			if len(pat) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegs(pat, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		ok, err := path.Match(pat[0], name[0])
		if err != nil || !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}
