package doctaculous

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenHTMLFileLoadsParentRelativeAsset renders a local HTML file whose linked
// stylesheet references a background image via url(../img/x.png). Before the
// DirLoader fix the image was refused (../ escape) and the box painted only its
// background-color; now the tiled image loads and the box carries image ink.
func TestOpenHTMLFileLoadsParentRelativeAsset(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel string, data []byte) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("img/tile.png", redPNG(t))
	mustWrite("css/main.css", []byte(`.box{width:40px;height:40px;background-image:url(../img/tile.png)}`))
	mustWrite("index.html", []byte(`<!DOCTYPE html><html><head>`+
		`<link rel="stylesheet" href="css/main.css"></head>`+
		`<body style="margin:0"><div class="box"></div></body></html>`))

	doc, err := OpenHTMLFile(filepath.Join(root, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	if !hasRedPixel(img, 0, 0, 40, 40) {
		t.Error("background image not loaded: no red pixels in the box region")
	}
}

// redPNG returns a 2x2 opaque-red PNG.
func redPNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// hasRedPixel reports whether any pixel in the region is red.
func hasRedPixel(img image.Image, x0, y0, x1, y1 int) bool {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a>>8 > 0 && r>>8 > 200 && g>>8 < 60 && b>>8 < 60 {
				return true
			}
		}
	}
	return false
}
