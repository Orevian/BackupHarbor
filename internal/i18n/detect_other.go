//go:build !windows

package i18n

import (
	"os"
	"strings"
)

// Detect returns the language from the LANG environment variable (en or tr; English otherwise).
func Detect() string {
	if strings.HasPrefix(strings.ToLower(os.Getenv("LANG")), "tr") {
		return TR
	}
	return EN
}
