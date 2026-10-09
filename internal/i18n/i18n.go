// Package i18n provides the English/Turkish message catalogs shared by the CLI,
// the GUI back end and the report renderers.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"strings"
	"sync"
)

//go:embed locales/*.json
var localeFS embed.FS

// Supported language codes.
const (
	EN = "en"
	TR = "tr"
)

// Languages lists the supported languages in display order.
var Languages = []string{EN, TR}

var (
	once     sync.Once
	catalogs map[string]map[string]string
)

func load() {
	catalogs = map[string]map[string]string{}
	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		panic("i18n: " + err.Error())
	}
	// Files are named <lang>.json or <lang>.<part>.json (for example en.ui.json).
	for _, lang := range Languages {
		merged := map[string]string{}
		for _, e := range entries {
			name := e.Name()
			if name != lang+".json" && !(strings.HasPrefix(name, lang+".") && strings.HasSuffix(name, ".json")) {
				continue
			}
			data, err := localeFS.ReadFile("locales/" + name)
			if err != nil {
				panic("i18n: " + err.Error())
			}
			m := map[string]string{}
			if err := json.Unmarshal(data, &m); err != nil {
				panic("i18n: invalid locale file " + name + ": " + err.Error())
			}
			for k, v := range m {
				if _, dup := merged[k]; dup {
					panic("i18n: duplicate key " + k + " in " + name)
				}
				merged[k] = v
			}
		}
		catalogs[lang] = merged
	}
}

// Catalog returns a copy of the message catalog for lang (falling back to English).
func Catalog(lang string) map[string]string {
	once.Do(load)
	src, ok := catalogs[Normalize(lang)]
	if !ok {
		src = catalogs[EN]
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// Normalize maps a user setting or locale name to a supported language code.
// "auto", empty and unknown values resolve through Detect.
func Normalize(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch {
	case l == EN || strings.HasPrefix(l, "en-") || strings.HasPrefix(l, "en_"):
		return EN
	case l == TR || strings.HasPrefix(l, "tr-") || strings.HasPrefix(l, "tr_"):
		return TR
	case l == "" || l == "auto":
		return Detect()
	}
	return EN
}

// Valid reports whether s is an accepted language setting.
func Valid(s string) bool { return s == "auto" || s == EN || s == TR }

// T returns the message for key in lang, formatted with args.
// A missing key returns the key itself so gaps are visible, never silent.
func T(lang, key string, args ...any) string {
	once.Do(load)
	l := Normalize(lang)
	s, ok := catalogs[l][key]
	if !ok {
		s, ok = catalogs[EN][key]
		if !ok {
			return key
		}
	}
	return format(s, args)
}

// Has reports whether key exists in the English catalog.
func Has(key string) bool {
	once.Do(load)
	_, ok := catalogs[EN][key]
	return ok
}

// Msg is a translatable message: a catalog code plus string arguments.
// Text always holds the English rendering so stored reports stay readable
// even if a catalog entry is later renamed.
type Msg struct {
	Code string   `json:"code"`
	Args []string `json:"args,omitempty"`
	Text string   `json:"text"`
}

// M builds a Msg with its English text filled in.
func M(code string, args ...any) Msg {
	m := Msg{Code: code}
	for _, a := range args {
		m.Args = append(m.Args, fmt.Sprint(a))
	}
	m.Text = m.In(EN)
	return m
}

// IsZero reports whether the message is empty.
func (m Msg) IsZero() bool { return m.Code == "" && m.Text == "" }

// In renders the message in lang, falling back to the stored English text.
func (m Msg) In(lang string) string {
	if m.Code == "" {
		return m.Text
	}
	once.Do(load)
	l := Normalize(lang)
	tmpl, ok := catalogs[l][m.Code]
	if !ok {
		tmpl, ok = catalogs[EN][m.Code]
		if !ok {
			if m.Text != "" {
				return m.Text
			}
			return m.Code
		}
	}
	args := make([]any, len(m.Args))
	for i, a := range m.Args {
		args[i] = a
	}
	return format(tmpl, args)
}

// Error makes Msg usable as an error carrying a translatable message.
func (m Msg) Error() string { return m.In(EN) }

// format applies args to a catalog template. Arguments are rendered with
// fmt.Sprint so numbers and durations work with %s, and surplus arguments or
// templates without verbs never produce "%!(EXTRA ...)" noise.
func format(tmpl string, args []any) string {
	if len(args) == 0 || !strings.Contains(tmpl, "%") {
		return tmpl
	}
	strs := make([]any, len(args))
	for i, a := range args {
		strs[i] = fmt.Sprint(a)
	}
	return fmt.Sprintf(tmpl, strs...)
}

// Upper upper-cases s using the rules of lang (Turkish maps i to İ and ı to I).
func Upper(lang, s string) string {
	if Normalize(lang) == TR {
		return cases.Upper(language.Turkish).String(s)
	}
	return strings.ToUpper(s)
}
