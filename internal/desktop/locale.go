//go:build desktop

package desktop

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/lang"
)

var translationsOnce sync.Once

func loadTranslations() {
	translationsOnce.Do(func() {
		// Supplement the built-in widget translations for simplified Chinese.
		_ = lang.AddTranslationsForLocale([]byte(`{"(Select one)":"请选择"}`), fyne.Locale("zh-Hans"))
	})
}
