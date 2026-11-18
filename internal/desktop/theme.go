//go:build desktop

package desktop

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
)

var background = color.NRGBA{R: 249, G: 249, B: 246, A: 255}
var sidebarColor = color.NRGBA{R: 239, G: 240, B: 236, A: 255}
var panelColor = color.NRGBA{R: 255, G: 255, B: 253, A: 255}
var lineColor = color.NRGBA{R: 221, G: 224, B: 219, A: 255}
var textColor = color.NRGBA{R: 38, G: 43, B: 45, A: 255}
var mutedColor = color.NRGBA{R: 112, G: 120, B: 121, A: 255}
var accentColor = color.NRGBA{R: 55, G: 72, B: 80, A: 255}
var successColor = color.NRGBA{R: 83, G: 119, B: 99, A: 255}
var warningColor = color.NRGBA{R: 155, G: 104, B: 50, A: 255}

type superTheme struct{}

func (superTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return background
	case theme.ColorNameForeground:
		return textColor
	case theme.ColorNamePrimary:
		return accentColor
	case theme.ColorNameSuccess:
		return successColor
	case theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnSuccess:
		return panelColor
	case theme.ColorNameButton, theme.ColorNameInputBackground, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground, theme.ColorNameHeaderBackground:
		return panelColor
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return mutedColor
	case theme.ColorNameSeparator, theme.ColorNameInputBorder:
		return lineColor
	case theme.ColorNameHover, theme.ColorNameFocus, theme.ColorNameSelection:
		return color.NRGBA{R: 55, G: 72, B: 80, A: 15}
	case theme.ColorNamePressed:
		return color.NRGBA{R: 55, G: 72, B: 80, A: 28}
	case theme.ColorNameWarning:
		return warningColor
	}
	return theme.DefaultTheme().Color(name, theme.VariantLight)
}
func (superTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 14
	case theme.SizeNameCaptionText:
		return 11
	case theme.SizeNameHeadingText:
		return 24
	case theme.SizeNameSubHeadingText:
		return 17
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameInnerPadding:
		return 10
	case theme.SizeNameInputRadius, theme.SizeNameButtonRadius:
		return 5
	case theme.SizeNameDialogRadius:
		return 8
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameSeparatorThickness:
		return 1
	}
	return theme.DefaultTheme().Size(name)
}
func (superTheme) Font(style fyne.TextStyle) fyne.Resource    { return theme.DefaultTheme().Font(style) }
func (superTheme) Icon(name fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(name) }
func text(value string, size float32, c color.Color, bold bool) *canvas.Text {
	t := canvas.NewText(value, c)
	t.TextSize = size
	t.TextStyle = fyne.TextStyle{Bold: bold}
	return t
}
func inset(content fyne.CanvasObject, pad float32) *fyne.Container {
	return container.New(layout.NewCustomPaddedLayout(pad, pad, pad, pad), content)
}
func space(height float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(1, height))
	return r
}
func card(content fyne.CanvasObject) fyne.CanvasObject {
	r := canvas.NewRectangle(panelColor)
	r.CornerRadius = 6
	r.StrokeColor = lineColor
	r.StrokeWidth = 1
	return container.NewStack(r, inset(content, 20))
}

func rule() fyne.CanvasObject {
	r := canvas.NewRectangle(lineColor)
	r.SetMinSize(fyne.NewSize(1, 1))
	return r
}
