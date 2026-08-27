package doctaculous

import (
	"bytes"
	"context"
	"image"
	"testing"
)

// rasterHTML renders one HTML string to pixels at the golden DPI, for the
// filtered-vs-unfiltered comparisons below.
func rasterHTML(t *testing.T, src string) *image.RGBA {
	t.Helper()
	doc, err := OpenHTMLBytes([]byte(src), WithViewportWidth(300), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI, BundledFonts: true})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized image is %T, want *image.RGBA", img)
	}
	return rgba
}

// filterProbeHTML is a small document exercising the paint paths a filter bracket
// wraps — a background, a border, and text — parameterized by an extra declaration.
func filterProbeHTML(decl string) string {
	return `<html><body style="margin:0;background:#fff">` +
		`<div style="width:150px;height:60px;background-color:#369;border:4px solid #930;` + decl + `">` +
		`Filtered text</div>` +
		`<p style="margin:0">after</p></body></html>`
}

// TestHTMLFilterNoneIsPixelIdentical is the regression guard that keeps every
// existing golden from moving: `filter: none` and an UNPARSEABLE value must both
// render byte-identically to a document with no declaration at all.
//
// CSS error handling makes an invalid declaration ignored ENTIRELY, so a list with
// one bad entry must not apply the entries that did parse — hence the mixed cases.
func TestHTMLFilterNoneIsPixelIdentical(t *testing.T) {
	base := rasterHTML(t, filterProbeHTML(""))
	for _, decl := range []string{
		"filter:none",
		"filter:NONE",
		"filter:grayscale(1) hue-rotate(oops)", // invalid as a whole
		"filter:not-a-function(1)",
		"filter:blur(50%)", // a percentage is not a <length>
		"filter:grayscale(-1)",
	} {
		got := rasterHTML(t, filterProbeHTML(decl))
		if !bytes.Equal(base.Pix, got.Pix) {
			t.Errorf("%s did not render byte-identically to no declaration", decl)
		}
	}
}

// TestHTMLFilterIsPassThroughForNow pins this stage's deliberate scope: the brackets
// are emitted and the offscreen group opens and closes, but the chain is NOT yet
// applied — so a filtered document still renders EXACTLY as an unfiltered one.
//
// This test is expected to FAIL once the pixel chain is wired up, and that failure is
// the signal to replace it with a real filtered-output assertion. It exists so the
// pass-through is a pinned, deliberate state rather than an unnoticed gap.
func TestHTMLFilterIsPassThroughForNow(t *testing.T) {
	base := rasterHTML(t, filterProbeHTML(""))
	for _, decl := range []string{
		"filter:grayscale(1)",
		"filter:invert(1)",
		"filter:blur(3px)",
		"filter:sepia(1) saturate(2)",
	} {
		got := rasterHTML(t, filterProbeHTML(decl))
		if !bytes.Equal(base.Pix, got.Pix) {
			t.Errorf("%s changed the rendering; the chain is meant to be a PASS-THROUGH at this stage "+
				"(if the pixel chain has landed, replace this test with a real filtered-output assertion)", decl)
		}
	}
}
