//go:build desktop

package desktop

import (
	"fyne.io/fyne/v2/lang"
	"fyne.io/fyne/v2/widget"

	"superflow.local/superflow/internal/i18n"
)

const languagePreference = "interface-language"

func tr(message string) string { return i18n.T(message) }

func setLanguage(preference string) {
	i18n.Set(i18n.Resolve(preference, lang.SystemLocale().String()))
}

func newSelect(options []string, changed func(string)) *widget.Select {
	control := &widget.Select{Options: options, OnChanged: changed, PlaceHolder: tr("Select an option")}
	control.ExtendBaseWidget(control)
	return control
}

// Language names are shown in their own language so users can find them when
// the system selects a language they do not understand.
func (g *gui) languagePicker(reopen func()) *widget.Select {
	optionLabels := []string{tr("Follow system"), "English", "简体中文"}
	optionIDs := []string{i18n.System, i18n.English, i18n.Chinese}
	prefs := g.app.Preferences()
	preference := prefs.StringWithFallback(languagePreference, i18n.System)
	picker := newSelect(optionLabels, nil)
	picker.SetSelectedIndex(0)
	for index, id := range optionIDs {
		if id == preference {
			picker.SetSelectedIndex(index)
			break
		}
	}
	picker.OnChanged = func(string) {
		index := picker.SelectedIndex()
		if index < 0 || preference == optionIDs[index] {
			return
		}
		prefs.SetString(languagePreference, optionIDs[index])
		setLanguage(optionIDs[index])
		g.rebuildInterface()
		if reopen != nil {
			reopen()
		}
	}
	return picker
}

func (g *gui) rebuildInterface() {
	g.buildContent(g.page)
	g.lastTray = ""
	g.updateTray()
}
