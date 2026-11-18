// Package brand defines the SuperFlow visual identity used by the app and docs.
package brand

import (
	"bytes"
	"image"
	"image/png"
	"strings"

	"github.com/fyne-io/oksvg"
	"github.com/srwiley/rasterx"
)

const upperFlow = "M77 24H44C31 24 23 32 23 43C23 51 27 57 34 60L43 50C38 49 35 47 35 43C35 39 39 36 44 36H67Z"
const lowerFlow = "M23 76H56C69 76 77 68 77 57C77 49 73 43 66 40L57 50C62 51 65 53 65 57C65 61 61 64 56 64H33Z"

const svgOpen = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">`
const SVG = svgOpen + `<rect x="4" y="4" width="92" height="92" rx="22" ry="22" fill="#203034"/><path d="` + upperFlow + `" fill="#FAF6EB"/><path d="` + lowerFlow + `" fill="#ADCDC2"/></svg>`
const TraySVG = svgOpen + `<path d="` + upperFlow + lowerFlow + `" fill="#FFFFFF"/></svg>`

func PNG(size int) ([]byte, error) {
	return Rasterize(SVG, size, size)
}

// Rasterize scales the SVG artwork to the requested output dimensions.
func Rasterize(svg string, width, height int) ([]byte, error) {
	icon, err := oksvg.ReadIconStream(strings.NewReader(svg), oksvg.StrictErrorMode)
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	icon.SetTarget(0, 0, float64(width), float64(height))
	scanner := rasterx.NewScannerGV(width, height, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(width, height, scanner), 1)
	var output bytes.Buffer
	err = png.Encode(&output, img)
	return output.Bytes(), err
}
