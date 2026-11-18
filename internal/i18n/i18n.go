// Package i18n provides the application catalog. English messages are the
// source keys and serve as a fallback for unsupported or missing translations.
package i18n

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync/atomic"

	"golang.org/x/text/language"
)

const (
	System  = "system"
	English = "en"
	Chinese = "zh-Hans"
)

//go:embed zh-Hans.json
var chineseJSON []byte

var chinese = loadCatalog(chineseJSON)
var useChinese atomic.Bool

func loadCatalog(data []byte) map[string]string {
	var catalog map[string]string
	if err := json.Unmarshal(data, &catalog); err != nil {
		panic(err)
	}
	return catalog
}

func Resolve(preference, systemLocale string) string {
	switch preference {
	case English, Chinese:
		return preference
	}
	// POSIX locale names may include an encoding or modifier.
	systemLocale, _, _ = strings.Cut(systemLocale, ".")
	systemLocale, _, _ = strings.Cut(systemLocale, "@")
	tag, _ := language.Parse(systemLocale)
	base, _ := tag.Base()
	if base.String() == "zh" {
		return Chinese
	}
	return English
}

func Set(value string) { useChinese.Store(value == Chinese) }

func T(message string) string {
	if useChinese.Load() {
		if translated, ok := chinese[message]; ok {
			return translated
		}
	}
	return message
}

// Error is useful for sentinel errors created before the UI selects a language.
type Error string

func (e Error) Error() string { return T(string(e)) }
