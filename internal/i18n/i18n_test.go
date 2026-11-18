package i18n

import "testing"

func TestResolveLanguage(t *testing.T) {
	for _, test := range []struct{ preference, systemLocale, want string }{
		{System, "zh_CN.UTF-8", Chinese},
		{System, "zh-Hant-TW", Chinese},
		{System, "fr-FR", English},
		{System, "C", English},
		{English, "zh-CN", English},
		{Chinese, "en-US", Chinese},
	} {
		if got := Resolve(test.preference, test.systemLocale); got != test.want {
			t.Errorf("Resolve(%q, %q) = %s, want %s", test.preference, test.systemLocale, got, test.want)
		}
	}
}
