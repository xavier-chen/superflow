// superflow-brand exports the app icon and documentation logo from Go artwork.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"superflow.local/superflow/internal/brand"
)

func main() {
	if err := export("docs/assets"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func export(directory string) error {
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	wordmark, err := wordmarkPath()
	if err != nil {
		return err
	}
	// A solid warm background keeps the logo legible in both GitHub themes.
	_, iconArtwork, _ := strings.Cut(brand.SVG, ">")
	iconArtwork = strings.TrimSuffix(iconArtwork, "</svg>")
	lockup := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 380 112" role="img" aria-label="SuperFlow">
  <rect x="1" y="1" width="378" height="110" rx="24" ry="24" fill="#F2F3ED" stroke="#D9E0D9" stroke-width="2"/>
  <g transform="translate(15 14) scale(.84 .84)">%s</g>
  <path fill="#203034" d="%s"/>
</svg>
`, iconArtwork, wordmark)
	png, err := brand.PNG(512)
	if err != nil {
		return err
	}
	logo, err := brand.Rasterize(lockup, 760, 224)
	if err != nil {
		return err
	}
	for _, asset := range []struct {
		name string
		data []byte
	}{
		{"app-icon.svg", []byte(brand.SVG + "\n")},
		{"app-icon.png", png},
		{"superflow-logo.svg", []byte(lockup)},
		{"superflow-logo.png", logo},
	} {
		if err := os.WriteFile(filepath.Join(directory, asset.name), asset.data, 0644); err != nil {
			return err
		}
	}
	return nil
}

// Use outlines so the documentation wordmark is consistent on any system.
func wordmarkPath() (string, error) {
	typeface, err := sfnt.Parse(gomedium.TTF)
	if err != nil {
		return "", err
	}
	var buffer sfnt.Buffer
	var path strings.Builder
	size := fixed.I(45)
	x, baseline := fixed.I(117), fixed.I(69)
	var previous sfnt.GlyphIndex
	for _, char := range "SuperFlow" {
		glyph, err := typeface.GlyphIndex(&buffer, char)
		if err != nil {
			return "", err
		}
		if previous != 0 {
			kern, err := typeface.Kern(&buffer, previous, glyph, size, font.HintingNone)
			if err != nil && err != sfnt.ErrNotFound {
				return "", err
			}
			x += kern
		}
		segments, err := typeface.LoadGlyph(&buffer, glyph, size, nil)
		if err != nil {
			return "", err
		}
		for i, segment := range segments {
			var command string
			var points int
			switch segment.Op {
			case sfnt.SegmentOpMoveTo:
				if i > 0 {
					path.WriteByte('Z')
				}
				command, points = "M", 1
			case sfnt.SegmentOpLineTo:
				command, points = "L", 1
			case sfnt.SegmentOpQuadTo:
				command, points = "Q", 2
			case sfnt.SegmentOpCubeTo:
				command, points = "C", 3
			}
			path.WriteString(command)
			for _, point := range segment.Args[:points] {
				fmt.Fprintf(&path, "%.2f %.2f ", float64(x+point.X)/64, float64(baseline+point.Y)/64)
			}
		}
		path.WriteByte('Z')
		advance, err := typeface.GlyphAdvance(&buffer, glyph, size, font.HintingNone)
		if err != nil {
			return "", err
		}
		x += advance - 10 // Close the spacing slightly for the display wordmark.
		previous = glyph
	}
	return path.String(), nil
}
