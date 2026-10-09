// Package secret handles password sources, Windows Credential Manager storage
// and masking of secret material in logs, errors and reports.
package secret

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Mask replaces redacted material.
const Mask = "********"

var (
	reURLUserinfo = regexp.MustCompile(`(?i)([a-z][a-z0-9+.\-]*://[^\s:/@]*):[^\s@/]+@`)
	reAssignment  = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:PASSWORD|PASSWD|SECRET|TOKEN|ACCESS_KEY|API_KEY)[A-Z0-9_]*)(\s*[=:]\s*)\S+`)
)

// Redactor masks a set of known secret values plus generic credential patterns.
// It is safe for concurrent use.
type Redactor struct {
	mu      sync.RWMutex
	secrets []string
}

// NewRedactor creates a Redactor for the given secret values.
func NewRedactor(secrets ...string) *Redactor {
	r := &Redactor{}
	for _, s := range secrets {
		r.Add(s)
	}
	return r
}

// Add registers another secret value to be masked.
func (r *Redactor) Add(s string) {
	if r == nil || s == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.secrets {
		if e == s {
			return
		}
	}
	r.secrets = append(r.secrets, s)
	// Longest first so a secret that contains another is masked completely.
	sort.Slice(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
}

// Redact masks all known secrets and generic credential patterns in s.
func (r *Redactor) Redact(s string) string {
	if r != nil {
		r.mu.RLock()
		for _, sec := range r.secrets {
			s = strings.ReplaceAll(s, sec, Mask)
		}
		r.mu.RUnlock()
	}
	return Redact(s)
}

// Redact masks generic credential patterns (URL userinfo, PASSWORD=... style assignments).
func Redact(s string) string {
	s = reURLUserinfo.ReplaceAllString(s, "${1}:"+Mask+"@")
	s = reAssignment.ReplaceAllString(s, "${1}${2}"+Mask)
	return s
}

// RedactRepository removes credentials embedded in a repository specification
// (for example rest:https://user:pass@host/path) so it can be shown or stored.
func RedactRepository(repo string) string { return Redact(repo) }
