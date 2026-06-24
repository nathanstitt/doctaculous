package doctaculous

// WPT-style reftests: reference-comparison tests in the Web Platform Tests reftest
// style. Each case is a (test, reference) pair of HTML files written differently but
// designed to lay out to the IDENTICAL pixels; we rasterize both of our own renders
// and assert they match (within the shared golden tolerance). This sidesteps having
// to match a real browser pixel-for-pixel — it asserts self-consistency between two
// of our renders, which is what a WPT `<link rel="match">` pair expresses.
//
// These pairs are AUTHORED FOR THIS PROJECT, not vendored from the W3C WPT suite.
// The pattern follows WPT reftests (the WPT suite is BSD-3-Clause); no WPT files are
// copied here. See testdata/wpt/README.md.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// wptReftestDir holds the in-house WPT-style normal-flow reftest pairs.
var wptReftestDir = filepath.Join("testdata", "wpt", "css21-normal-flow")

// wptReftests lists each reftest by base name; the harness loads NAME.html (test)
// and NAME-ref.html (reference) and renders both at viewportPx. Each pair is a CSS
// 2.1 normal-flow equivalence our engine implements, so test and reference must
// rasterize identically.
var wptReftests = []struct {
	// name is the base file name (NAME.html / NAME-ref.html).
	name string
	// viewportPx is the layout viewport width both pages render at. It must match any
	// viewport-dependent geometry baked into the pair (e.g. auto-width fills it, and
	// the percentage/parent widths assume there is room for them).
	viewportPx float64
	// what documents the equivalence under test, for failure messages.
	what string
	// loader resolves external refs (e.g. <img src>) for both pages of the pair; nil
	// for pairs with no external resources.
	loader resource.ResourceLoader
}{
	{"margin-collapse", 200, "adjacent vertical margins collapse to their max (CSS 2.1 8.3.1)", nil},
	{"shorthand", 300, "margin/border/padding shorthands expand to their longhands", nil},
	{"box-sizing", 200, "box-sizing:border-box equals the content-box equivalent", nil},
	{"auto-width", 200, "an auto-width block fills its containing block", nil},
	{"percent-width", 400, "a percentage width resolves against the containing block width", nil},
	{"padding-shorthand", 320, "the 2-value padding shorthand equals the 4-value form", nil},
	{"img-vs-div", 200, "a solid <img> sized W×H equals a <div> of the same size and background", solidSwatchLoader()},
}

// swatchColor is the solid color shared by the img-vs-div reftest's image and the
// reference div's background (#3366cc).
var swatchColor = color.RGBA{0x33, 0x66, 0xcc, 0xff}

// solidSwatchLoader serves a solid swatchColor PNG at "swatch.png" matching the
// reference div's background, so an object-fit:fill <img> of it renders identical
// pixels to the div. It panics on encode failure (never happens for a tiny RGBA).
func solidSwatchLoader() resource.MapLoader {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, swatchColor)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return resource.MapLoader{"swatch.png": {Data: buf.Bytes(), ContentType: "image/png"}}
}

// TestWPTReftests renders each (test, reference) pair at a fixed viewport and DPI and
// asserts the two rasterize identically (compareImages reports no difference). A
// failure means either the engine regressed on the equivalence under test or the
// reference is not actually equivalent to the test.
func TestWPTReftests(t *testing.T) {
	for _, rt := range wptReftests {
		t.Run(rt.name, func(t *testing.T) {
			testImg := renderReftestPage(t, rt.name+".html", rt.viewportPx, rt.loader)
			refImg := renderReftestPage(t, rt.name+"-ref.html", rt.viewportPx, rt.loader)

			if testImg.Bounds() != refImg.Bounds() {
				t.Fatalf("%s: bounds differ: test %v vs ref %v (%s)",
					rt.name, testImg.Bounds(), refImg.Bounds(), rt.what)
			}
			if differ, n := compareImages(refImg, testImg); differ {
				total := refImg.Bounds().Dx() * refImg.Bounds().Dy()
				t.Errorf("%s: test and reference render differently: %d/%d pixels beyond tolerance (max %d) — %s",
					rt.name, n, total, int(maxDifferingFraction*float64(total)), rt.what)
			}
		})
	}
}

// renderReftestPage reads an HTML file from the reftest directory and rasterizes its
// single page at the golden DPI and the given viewport width, returning the RGBA.
func renderReftestPage(t *testing.T, file string, viewportPx float64, loader resource.ResourceLoader) *image.RGBA {
	t.Helper()
	path := filepath.Join(wptReftestDir, file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	opts := []HTMLOption{WithViewportWidth(viewportPx)}
	if loader != nil {
		opts = append(opts, WithResourceLoader(loader))
	}
	doc, err := OpenHTMLBytes(data, opts...)
	if err != nil {
		t.Fatalf("OpenHTMLBytes(%s): %v", file, err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI})
	if err != nil {
		t.Fatalf("RasterizePage(%s): %v", file, err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized %s is %T, want *image.RGBA", file, img)
	}
	return rgba
}
