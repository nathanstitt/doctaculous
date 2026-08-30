package omnidoc

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"
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
	t.Parallel()
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

// filterProbeInk samples the filtered <div>'s own background well inside its
// border box. At the golden DPI the div spans roughly x∈[4,154], y∈[4,64] in
// device pixels (a 4px border inside a 150x60 content box at the body's origin),
// so (75, 34) sits in its interior and away from the text.
func filterProbeInk(img *image.RGBA) color.RGBA { return img.RGBAAt(75, 34) }

// TestHTMLFilterAppliesTheChainEndToEnd is the end-to-end counterpart of the
// per-function mapping tests in pkg/layout/paint: it drives real HTML through
// parse → cascade → layout → bracket emission → paint, and asserts on the
// resulting PIXELS of the filtered box.
//
// The `want` values are the Filter Effects spec's own formula applied by hand to
// the probe's background #369 (51, 102, 153) — the same derivation the paint-level
// suite documents — which is what makes this a check on the whole pipeline rather
// than a restatement of the mapping code.
//
// It replaces the pass-through pin the plumbing stage carried, whose purpose was
// exactly to fail once the chain landed.
func TestHTMLFilterAppliesTheChainEndToEnd(t *testing.T) {
	t.Parallel()
	base := filterProbeInk(rasterHTML(t, filterProbeHTML("")))
	if want := (color.RGBA{0x33, 0x66, 0x99, 0xff}); base != want {
		t.Fatalf("unfiltered probe sampled %v at (75,34), want the div's own %v — "+
			"the sample point has drifted off the box", base, want)
	}
	for _, tc := range []struct {
		decl string
		want color.RGBA
	}{
		{"filter:grayscale(1)", color.RGBA{95, 95, 95, 255}},
		{"filter:invert(1)", color.RGBA{204, 153, 102, 255}},
		{"filter:brightness(0.5)", color.RGBA{26, 51, 77, 255}},
		{"filter:sepia(1)", color.RGBA{127, 113, 88, 255}},
		{"filter:saturate(0)", color.RGBA{95, 95, 95, 255}},
		{"filter:hue-rotate(0deg)", color.RGBA{51, 102, 153, 255}},
		// Composition, in the written order: brighten to (102,204,255) and THEN
		// take that result's luminance (186), not the reverse (190).
		{"filter:brightness(2) grayscale(1)", color.RGBA{186, 186, 186, 255}},
		{"filter:grayscale(1) brightness(2)", color.RGBA{190, 190, 190, 255}},
	} {
		got := filterProbeInk(rasterHTML(t, filterProbeHTML(tc.decl)))
		if !closeRGBA(got, tc.want, 2) {
			t.Errorf("%s sampled %v at (75,34), want %v", tc.decl, got, tc.want)
		}
	}
}

// TestHTMLFilterBlurIsSpatial: blur() is the one function whose effect is visible
// as a SPREAD rather than a recolouring. It must soften the div's own border edge
// and reach outside the border box, since CSS does not clip a filter to the box.
func TestHTMLFilterBlurIsSpatial(t *testing.T) {
	t.Parallel()
	plain := rasterHTML(t, filterProbeHTML(""))
	blurred := rasterHTML(t, filterProbeHTML("filter:blur(4px)"))
	if bytes.Equal(plain.Pix, blurred.Pix) {
		t.Fatal("blur(4px) rendered identically to no filter")
	}
	// Above the div's top edge the unfiltered page is bare white; the blur must
	// tint it.
	if got := blurred.RGBAAt(75, 1); got.R == 0xff && got.G == 0xff && got.B == 0xff {
		t.Error("blur(4px) did not spread above the border box; the surface is cropping it")
	}
}

// TestHTMLFilterDropShadowUsesCurrentColor: drop-shadow() with NO colour argument
// means the element's own `color` property, not black — the one part of the
// function that cannot be resolved without the cascade, which is why the colour is
// resolved at layout time and carried on the item.
func TestHTMLFilterDropShadowUsesCurrentColor(t *testing.T) {
	t.Parallel()
	img := rasterHTML(t, filterProbeHTML("color:#f00;filter:drop-shadow(20px 20px)"))
	// The shadow is the div's silhouette shifted 20px down-right, so a point
	// just past the div's bottom edge but within the shifted silhouette is pure
	// shadow.
	got := img.RGBAAt(75, 75)
	if !closeRGBA(got, color.RGBA{0xff, 0, 0, 0xff}, 3) {
		t.Errorf("shadow sampled %v at (75,75), want the element's own color #f00 "+
			"(an omitted drop-shadow colour means currentColor, not black)", got)
	}
}

// closeRGBA reports whether got matches want within a per-channel tolerance,
// absorbing the float32→uint8 rounding of the sRGB round trip.
func closeRGBA(got, want color.RGBA, tol int) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a) - int(b)
		}
		return int(b) - int(a)
	}
	return d(got.R, want.R) <= tol && d(got.G, want.G) <= tol &&
		d(got.B, want.B) <= tol && d(got.A, want.A) <= tol
}

// TestHTMLFilterOverCapReachesTheCallersLogf is the end-to-end guard on the
// painter's diagnostics wiring: an over-cap filter must reach the LOGGER THE
// PUBLIC API CALLER PASSED, not merely a logger somewhere inside pkg/layout/paint.
//
// It is the degradation a real user actually hits — maxCSSFilterPixels is 4M and
// a full-page filter at 300 DPI is well past it, so the same document filters at
// 150 DPI and stops filtering at 300 with nothing in the output to explain why.
// The unit tests in pkg/layout/paint prove the notice is emitted; only this one
// proves RasterOptions.Logf is actually threaded through to the painter.
func TestHTMLFilterOverCapReachesTheCallersLogf(t *testing.T) {
	t.Parallel()
	// A full-page filtered box. At 300 DPI this page rasterizes to far more than
	// the 4M-pixel cap, so the bracket degrades to unfiltered.
	src := `<html><body style="margin:0;background:#fff">` +
		`<div style="width:100%;height:1000px;background-color:#369;filter:grayscale(1)">x</div>` +
		`</body></html>`
	doc, err := OpenHTMLBytes([]byte(src), WithViewportWidth(1200), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	var lines []string
	if _, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		DPI:          300,
		BundledFonts: true,
		Logf:         func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) },
	}); err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	var found string
	for _, l := range lines {
		if strings.Contains(l, "CSS filter surface unavailable") {
			found = l
			break
		}
	}
	if found == "" {
		t.Fatalf("the over-cap filter degradation never reached the caller's Logf; got %q", lines)
	}
	if !strings.Contains(found, "pixel cap") {
		t.Errorf("notice %q should name the pixel cap so the reader knows a lower DPI would filter it", found)
	}
}
