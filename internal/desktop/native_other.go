//go:build desktop && (!windows || ci)

package desktop

import "fyne.io/fyne/v2"

func prepareNativeProcess()    {}
func revealNative(fyne.Window) {}
