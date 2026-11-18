//go:build desktop && darwin && !ci

package desktop

import (
	"fyne.io/fyne/v2"
	"github.com/ebitengine/purego/objc"
)

// Called by Fyne on the native main thread after its application delegate exists.
// A second Finder launch should reveal the window hidden by close-to-tray.
func installReopen(show func()) {
	app := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
	delegate := app.Send(objc.RegisterName("delegate"))
	if delegate == 0 {
		return
	}
	delegate.Class().AddMethod(objc.RegisterName("applicationShouldHandleReopen:hasVisibleWindows:"),
		objc.NewIMP(func(_ objc.ID, _ objc.SEL, _ objc.ID, _ bool) bool {
			fyne.Do(show)
			return true
		}), "c@:@c")
}
