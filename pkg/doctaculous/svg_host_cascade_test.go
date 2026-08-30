package doctaculous

import (
	"context"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// svgProbeColor rasterizes html and returns the pixel at (x,y). The probe samples a
// SPECIFIC colour rather than counting coverage: an unstyled SVG shape paints BLACK
// by default, so "something was painted" is true both when the cascade works and when
// it silently does not. Only the colour distinguishes them — the same trap that made
// this gap expensive to find in the field.
func svgProbeColor(t *testing.T, html string, x, y int) color.RGBA {
	t.Helper()
	doc, err := OpenHTMLBytes([]byte(html), WithPageSize(200, 100))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		MaxWidthPx: 200, MaxHeightPx: 100, Background: color.White})
	if err != nil {
		t.Fatalf("raster: %v", err)
	}
	r, g, b, a := img.At(x, y).RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

// probeBlue is the fill the cases below apply. It is not a colour any other part of
// the pipeline emits by default, so a matching pixel can only come from the rule.
var probeBlue = color.RGBA{R: 0x7e, G: 0xc8, B: 0xf0, A: 0xff}

const svgProbeRect = `<svg width="200" height="100"><rect id="r" class="k" x="0" y="0" width="200" height="100"/></svg>`

// An author stylesheet in the HOST document styles inline <svg> children. Before this,
// the SVG subtree was re-serialized to markup and re-parsed as a standalone document,
// so the page's CSS never reached it and every shape painted the default black.
func TestHostStylesheetCascadesIntoInlineSVG(t *testing.T) {
	cases := []struct{ name, selector string }{
		{"class", ".k{fill:#7ec8f0}"},
		{"element", "rect{fill:#7ec8f0}"},
		{"id", "#r{fill:#7ec8f0}"},
		{"descendant-within-svg", "svg rect{fill:#7ec8f0}"},
		{"grouped", "p, .k, li{fill:#7ec8f0}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			html := `<html><head><style>` + c.selector + `</style></head><body style="margin:0">` + svgProbeRect + `</body></html>`
			if got := svgProbeColor(t, html, 100, 50); got != probeBlue {
				t.Errorf("centre = %v, want %v (host rule %q did not reach the SVG)", got, probeBlue, c.selector)
			}
		})
	}
}

// Falsifiability control: with no rule, the shape keeps SVG's default black fill. If
// this ever went blue the assertions above would be meaningless.
func TestInlineSVGWithoutHostRuleStaysDefault(t *testing.T) {
	html := `<html><body style="margin:0">` + svgProbeRect + `</body></html>`
	if got := svgProbeColor(t, html, 100, 50); got != (color.RGBA{A: 0xff}) {
		t.Errorf("centre = %v, want opaque black (the SVG default fill)", got)
	}
}

// A descendant selector rooted OUTSIDE the <svg> matches its children: the ancestor
// chain continues past the SVG root into the host tree. Without it, `.k` would work
// while `#a .k` silently did nothing — partial support that reads as a styling bug.
func TestHostDescendantSelectorCrossesSVGBoundary(t *testing.T) {
	html := `<html><head><style>#a .k{fill:#7ec8f0}</style></head><body style="margin:0">` +
		`<div id="a">` + svgProbeRect + `</div></body></html>`
	if got := svgProbeColor(t, html, 100, 50); got != probeBlue {
		t.Errorf("centre = %v, want %v (an ancestor selector did not cross the SVG boundary)", got, probeBlue)
	}
}

// The same selector must NOT match an SVG outside the named ancestor, or the boundary
// crossing above would be matching everything rather than the right subtree.
func TestHostDescendantSelectorDoesNotOvermatch(t *testing.T) {
	html := `<html><head><style>#a .k{fill:#7ec8f0}</style></head><body style="margin:0">` +
		`<div id="other">` + svgProbeRect + `</div></body></html>`
	if got := svgProbeColor(t, html, 100, 50); got != (color.RGBA{A: 0xff}) {
		t.Errorf("centre = %v, want black: #a .k must not match an SVG outside #a", got)
	}
}

// Two BYTE-IDENTICAL <svg> subtrees under different host rules must not collide in
// the inline-SVG parse cache. The cache is keyed by markup, so before the host
// context joined the key these two would have shared one parse and the second would
// have silently painted with the first's styling.
func TestInlineSVGCacheDoesNotCollideAcrossHostStyles(t *testing.T) {
	const r = `<svg width="100" height="40"><rect class="k" x="0" y="0" width="100" height="40"/></svg>`
	html := `<html><head><style>#a .k{fill:#ff0000} #b .k{fill:#00ff00}</style></head><body style="margin:0">` +
		`<div id="a">` + r + `</div><div id="b">` + r + `</div></body></html>`
	doc, err := OpenHTMLBytes([]byte(html), WithPageSize(100, 80))
	if err != nil {
		t.Fatal(err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		MaxWidthPx: 100, MaxHeightPx: 80, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	at := func(x, y int) color.RGBA {
		r, g, b, a := img.At(x, y).RGBA()
		return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
	}
	if got, want := at(50, 20), (color.RGBA{R: 0xff, A: 0xff}); got != want {
		t.Errorf("first svg = %v, want %v", got, want)
	}
	if got, want := at(50, 60), (color.RGBA{G: 0xff, A: 0xff}); got != want {
		t.Errorf("second svg = %v, want %v (identical markup reused the first's parse)", got, want)
	}
}

// currentColor resolves against the `color` the <svg> box inherits from the host,
// which needs the host's computed style to cross the boundary — not just its sheets.
func TestCurrentColorInheritsFromHostBox(t *testing.T) {
	html := `<html><body style="margin:0;color:#7ec8f0">` +
		`<svg width="200" height="100"><rect fill="currentColor" x="0" y="0" width="200" height="100"/></svg>` +
		`</body></html>`
	if got := svgProbeColor(t, html, 100, 50); got != probeBlue {
		t.Errorf("centre = %v, want %v (currentColor did not inherit the host's color)", got, probeBlue)
	}
}

// Two identical currentColor SVGs under different inherited colours must also stay
// distinct in the cache — the same collision risk, via a different host input.
func TestCurrentColorCacheDoesNotCollide(t *testing.T) {
	const r = `<svg width="100" height="40"><rect fill="currentColor" x="0" y="0" width="100" height="40"/></svg>`
	html := `<html><body style="margin:0">` +
		`<div style="color:#ff0000">` + r + `</div>` +
		`<div style="color:#00ff00">` + r + `</div></body></html>`
	doc, err := OpenHTMLBytes([]byte(html), WithPageSize(100, 80))
	if err != nil {
		t.Fatal(err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		MaxWidthPx: 100, MaxHeightPx: 80, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	at := func(x, y int) color.RGBA {
		r, g, b, a := img.At(x, y).RGBA()
		return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
	}
	if got, want := at(50, 20), (color.RGBA{R: 0xff, A: 0xff}); got != want {
		t.Errorf("first = %v, want %v", got, want)
	}
	if got, want := at(50, 60), (color.RGBA{G: 0xff, A: 0xff}); got != want {
		t.Errorf("second = %v, want %v (inherited colour missing from the cache key)", got, want)
	}
}

// The SVG's OWN <style> outranks a host rule of equal specificity: host sheets
// cascade below the SVG's, because the SVG is the more specific context for its own
// content. Both rules are a single class here, so only source order decides.
func TestSVGInternalStyleBeatsHostRuleOnTies(t *testing.T) {
	html := `<html><head><style>.k{fill:#ff0000}</style></head><body style="margin:0">` +
		`<svg width="200" height="100"><style>.k{fill:#7ec8f0}</style>` +
		`<rect class="k" x="0" y="0" width="200" height="100"/></svg></body></html>`
	if got := svgProbeColor(t, html, 100, 50); got != probeBlue {
		t.Errorf("centre = %v, want %v (the SVG's own <style> must win a specificity tie)", got, probeBlue)
	}
}

// A presentation attribute has zero specificity, so ANY host rule outranks it — the
// same ordering SVG applies to its own sheets.
func TestHostRuleBeatsPresentationAttribute(t *testing.T) {
	html := `<html><head><style>.k{fill:#7ec8f0}</style></head><body style="margin:0">` +
		`<svg width="200" height="100"><rect class="k" fill="#ff0000" x="0" y="0" width="200" height="100"/></svg>` +
		`</body></html>`
	if got := svgProbeColor(t, html, 100, 50); got != probeBlue {
		t.Errorf("centre = %v, want %v (a host rule must outrank a presentation attribute)", got, probeBlue)
	}
}

// An inline style="" on the SVG child still wins over a host rule, as it does over
// the SVG's own sheets.
func TestInlineStyleAttributeBeatsHostRule(t *testing.T) {
	html := `<html><head><style>.k{fill:#ff0000}</style></head><body style="margin:0">` +
		`<svg width="200" height="100"><rect class="k" style="fill:#7ec8f0" x="0" y="0" width="200" height="100"/></svg>` +
		`</body></html>`
	if got := svgProbeColor(t, html, 100, 50); got != probeBlue {
		t.Errorf("centre = %v, want %v (an inline style= must outrank a host rule)", got, probeBlue)
	}
}

// Host CSS reaches stroke as well as fill, so the whole paint vocabulary crosses the
// boundary rather than a special-cased property. The probe samples a point ON the
// stroke band, which is inset from the viewport edge (a stroke centred on the edge of
// a full-bleed rect paints half outside the viewport and would read as "not painted").
func TestHostStyleReachesStroke(t *testing.T) {
	html := `<html><head><style>.k{fill:none;stroke:#7ec8f0;stroke-width:60}</style></head><body style="margin:0">` +
		`<svg width="200" height="100"><rect class="k" x="60" y="20" width="80" height="60"/></svg></body></html>`
	if got := svgProbeColor(t, html, 100, 50); got != probeBlue {
		t.Errorf("stroke band = %v, want %v", got, probeBlue)
	}
}

// An <img src="*.svg"> is a SEPARATE document: CSS explicitly does not cascade into
// it, so a host rule must NOT restyle its contents. This is the boundary the fix must
// not overreach — inline SVG is part of the host tree; a referenced one is not.
func TestHostStyleDoesNotReachImgSVG(t *testing.T) {
	svgFile := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100">` +
		`<rect class="k" x="0" y="0" width="200" height="100"/></svg>`)
	html := `<html><head><style>.k{fill:#7ec8f0}</style></head><body style="margin:0">` +
		`<img src="x.svg" width="200" height="100"></body></html>`
	doc, err := OpenHTMLBytes([]byte(html),
		WithPageSize(200, 100),
		WithResourceLoader(resource.MapLoader{"x.svg": {Data: svgFile, ContentType: "image/svg+xml"}}))
	if err != nil {
		t.Fatal(err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		MaxWidthPx: 200, MaxHeightPx: 100, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(100, 50).RGBA()
	got := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff}
	if got != (color.RGBA{A: 0xff}) {
		t.Errorf("centre = %v, want black: the page's CSS must not cascade into a referenced SVG document", got)
	}
}
