package paint

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// black is the colour every shadow below paints in, so a pixel assertion reads
// as "is there ink here" rather than as a colour comparison.
var black = color.RGBA{A: 0xff}

// shadowPage wraps one shadow item (and optionally the box's own background
// after it, in CSS paint order) into a page for rasterization.
func shadowPage(w, h float64, s layout.ShadowItem, bg *layout.RuleItem) *layout.Page {
	items := []layout.Item{{Kind: layout.ShadowKind, Shadow: s}}
	if bg != nil {
		items = append(items, layout.Item{Kind: layout.BackgroundKind, Rule: *bg})
	}
	return &layout.Page{WidthPt: w, HeightPt: h, Items: items}
}

// inkAt reports whether the pixel at (x,y) carries shadow ink — i.e. is
// meaningfully darker than the white canvas. It is a coverage test, not a
// colour test, so a blurred (partially covered) pixel counts.
func inkAt(img *image.RGBA, x, y int) bool {
	c := img.RGBAAt(x, y)
	return c.R < 0xf0 || c.G < 0xf0 || c.B < 0xf0
}

// coverageAt returns the shadow's coverage at (x,y) as a 0..255 value, derived
// from how far the pixel has been darkened from white. It is what a blur
// gradient assertion needs: an absolute colour would also depend on the
// rasterizer's own antialiasing.
func coverageAt(img *image.RGBA, x, y int) int {
	return 0xff - int(img.RGBAAt(x, y).R)
}

// countInk counts every pixel carrying shadow ink on the whole canvas.
func countInk(img *image.RGBA) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if inkAt(img, x, y) {
				n++
			}
		}
	}
	return n
}

// TestPaintShadowSpreadPaintsTheInflatedShape is the headline pixel claim: a
// 40x40 box with `box-shadow: 0 0 0 10px black` covers a 60x60 area.
//
// The expected count is exactly 3600 and not "more than 1600": the spread grows
// the shape by 10 on ALL FOUR sides, so 40+2*10 = 60 per axis. The shadow is a
// RING (the box's own area is cut out of it, so a transparent box does not show
// the shadow through itself), which is why the box's own background is painted
// on top here — together they must cover the full 60x60 with no seam at the
// ring's inner edge.
func TestPaintShadowSpreadPaintsTheInflatedShape(t *testing.T) {
	// The box sits at (20,20) so the whole 60x60 shadow fits on the canvas —
	// at the page corner the top and left thirds would simply fall off it.
	box := layout.RuleItem{XPt: 20, YPt: 20, WPt: 40, HPt: 40, Color: black}
	page := shadowPage(100, 100, layout.ShadowItem{
		XPt: 20, YPt: 20, WPt: 40, HPt: 40, Spread: 10, Color: black,
	}, &box)
	img := newRasterPage(100, 100, page)

	if got := countInk(img); got != 3600 {
		t.Errorf("shadow+box ink = %d px, want 3600 (a 60x60 area: 40 + 2*10 spread per axis)", got)
	}
	// The exact edges: ink from 10 to 69 inclusive on each axis, nothing outside.
	for _, tc := range []struct {
		x, y int
		want bool
	}{
		{10, 40, true}, {9, 40, false}, // left edge of the spread
		{69, 40, true}, {70, 40, false}, // right edge
		{40, 10, true}, {40, 9, false}, // top edge
		{40, 69, true}, {40, 70, false}, // bottom edge
		{40, 40, true}, // the box's own centre, filled by the background
		{15, 15, true}, // the spread's corner
	} {
		if got := inkAt(img, tc.x, tc.y); got != tc.want {
			t.Errorf("ink at (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

// TestPaintShadowIsARingUnderTheBox: an outer shadow does NOT paint under the
// box's own border box. A browser renders `background:transparent;
// box-shadow:0 0 0 10px black` as a ring, and painting a filled blob instead
// would show through every box with a transparent or partly transparent
// background.
func TestPaintShadowIsARingUnderTheBox(t *testing.T) {
	page := shadowPage(100, 100, layout.ShadowItem{
		XPt: 20, YPt: 20, WPt: 40, HPt: 40, Spread: 10, Color: black,
	}, nil) // no background: the hole must be visible
	img := newRasterPage(100, 100, page)

	if inkAt(img, 40, 40) {
		t.Error("the box's own area carries shadow ink; an outer shadow must be a ring")
	}
	if !inkAt(img, 15, 40) {
		t.Error("the spread band carries no ink; the ring did not paint")
	}
	// 60x60 minus the 40x40 hole.
	if got := countInk(img); got != 3600-1600 {
		t.Errorf("ring ink = %d px, want %d (60x60 minus the 40x40 box)", got, 3600-1600)
	}
}

// TestPaintShadowOffsetMovesTheShape: a plain offset shadow with no spread and
// no blur is the box's shape displaced, minus the part hidden under the box.
func TestPaintShadowOffsetMovesTheShape(t *testing.T) {
	page := shadowPage(100, 100, layout.ShadowItem{
		XPt: 20, YPt: 20, WPt: 40, HPt: 40, OffsetX: 10, OffsetY: 10, Color: black,
	}, nil)
	img := newRasterPage(100, 100, page)

	// The displaced shape spans [30,70)x[30,70); the box hides [30,60)x[30,60).
	if !inkAt(img, 65, 65) {
		t.Error("no ink at the offset shape's far corner")
	}
	if inkAt(img, 25, 25) {
		t.Error("ink above-left of the box; the shadow moved the wrong way")
	}
	if inkAt(img, 40, 40) {
		t.Error("ink under the box; an outer shadow is clipped to outside the border box")
	}
	if got := countInk(img); got != 40*40-30*30 {
		t.Errorf("offset shadow ink = %d px, want %d (40x40 shape minus the 30x30 overlap)",
			got, 40*40-30*30)
	}
}

// TestPaintShadowNegativeSpreadShrinks pins that a negative spread SHRINKS the
// shadow (unlike a negative blur, which is a parse error). With no offset it
// can shrink the shape inside the box entirely, leaving nothing to paint.
func TestPaintShadowNegativeSpreadShrinks(t *testing.T) {
	// A 40x40 box at (20,20), shrunk by 5 per side and pushed 20 down-right:
	// the shape spans [45,75) on each axis — 30x30, where an unshrunk shadow
	// would have been 40x40 at [40,80). It overlaps the box (which ends at 60)
	// in [45,60), a 15x15 square that is cut out.
	page := shadowPage(100, 100, layout.ShadowItem{
		XPt: 20, YPt: 20, WPt: 40, HPt: 40, OffsetX: 20, OffsetY: 20, Spread: -5, Color: black,
	}, nil)
	img := newRasterPage(100, 100, page)
	if got := countInk(img); got != 30*30-15*15 {
		t.Errorf("ink = %d px, want %d (a 30x30 shrunk shape minus its 15x15 overlap with the box)",
			got, 30*30-15*15)
	}

	// Shrunk away entirely: nothing paints, and nothing panics.
	gone := shadowPage(100, 100, layout.ShadowItem{
		XPt: 20, YPt: 20, WPt: 40, HPt: 40, Spread: -30, Color: black,
	}, nil)
	if got := countInk(newRasterPage(100, 100, gone)); got != 0 {
		t.Errorf("a shadow shrunk past nothing painted %d px, want 0", got)
	}
}

// TestPaintInsetShadowStaysInsideTheBox: an inset shadow fills the padding box
// MINUS the offset shape, so it hugs the inside edges the offset moves away
// from — and it can never escape the box, however large the offset.
func TestPaintInsetShadowStaysInsideTheBox(t *testing.T) {
	// A 40x40 box at (20,20) with `inset 10px 0 0`: the interior shifts 10 right,
	// leaving a 10-wide band down the LEFT inside edge.
	page := shadowPage(100, 100, layout.ShadowItem{
		XPt: 20, YPt: 20, WPt: 40, HPt: 40, OffsetX: 10, Color: black, Inset: true,
	}, nil)
	img := newRasterPage(100, 100, page)

	if !inkAt(img, 25, 40) {
		t.Error("no ink in the left band; the inset shadow did not paint")
	}
	if inkAt(img, 45, 40) {
		t.Error("ink in the lit interior; the inset shadow's shape was not subtracted")
	}
	if inkAt(img, 15, 40) || inkAt(img, 65, 40) {
		t.Error("ink outside the box; an inset shadow must stay inside its padding box")
	}
	if got := countInk(img); got != 10*40 {
		t.Errorf("inset band = %d px, want %d (a 10-wide band down a 40-tall box)", got, 10*40)
	}
}

// TestPaintInsetShadowSpreadThickensTheBand pins the spec's INVERTED spread
// sign for an inner shadow: a positive spread shrinks the lit interior, which
// makes the shadow itself thicker. This is the arithmetic that would be exactly
// backwards if inset were implemented as a sign flip of the outer case.
func TestPaintInsetShadowSpreadThickensTheBand(t *testing.T) {
	page := shadowPage(100, 100, layout.ShadowItem{
		XPt: 20, YPt: 20, WPt: 40, HPt: 40, Spread: 5, Color: black, Inset: true,
	}, nil)
	img := newRasterPage(100, 100, page)

	// The interior shrinks to 30x30 centred, leaving a 5-wide ring all round.
	if !inkAt(img, 22, 40) || !inkAt(img, 40, 22) {
		t.Error("no ink in the ring; a positive inset spread must thicken the shadow")
	}
	if inkAt(img, 40, 40) {
		t.Error("ink at the centre; the 30x30 interior must stay lit")
	}
	if got := countInk(img); got != 40*40-30*30 {
		t.Errorf("inset ring = %d px, want %d (a 40x40 padding box minus a 30x30 interior)",
			got, 40*40-30*30)
	}
}

// TestPaintInsetShadowClampsAnEscapingInterior: an interior displaced clear of
// the padding box must fill the box entirely, NOT punch a hole in the page.
// Under the even-odd rule an unclamped subpath outside the box would invert the
// surrounding region — a shadow that erases its neighbours.
func TestPaintInsetShadowClampsAnEscapingInterior(t *testing.T) {
	page := shadowPage(100, 100, layout.ShadowItem{
		XPt: 20, YPt: 20, WPt: 40, HPt: 40, OffsetX: 500, Color: black, Inset: true,
	}, nil)
	img := newRasterPage(100, 100, page)

	if got := countInk(img); got != 40*40 {
		t.Errorf("ink = %d px, want %d (the whole padding box filled, nothing outside)", got, 40*40)
	}
	if !inkAt(img, 40, 40) {
		t.Error("the box is not filled; a fully-displaced interior leaves the box all shadow")
	}
	if inkAt(img, 5, 5) {
		t.Error("ink outside the box; the escaping interior inverted the page")
	}
}

// TestPaintBlurredShadowSoftensTheEdge: with a blur the shadow's coverage falls
// off across the edge instead of stopping dead, and it reaches BEYOND the
// unblurred shape.
//
// The gradient is asserted as a monotone falloff rather than against exact
// values, because the exact numbers belong to the box-blur approximation in
// pkg/svg/filter — which has its own spec-formula tests, and which this path
// deliberately reuses rather than reimplementing.
func TestPaintBlurredShadowSoftensTheEdge(t *testing.T) {
	page := shadowPage(120, 120, layout.ShadowItem{
		XPt: 40, YPt: 40, WPt: 40, HPt: 40, Blur: 12, Color: black,
	}, nil)
	img := newRasterPage(120, 120, page)

	// The blur reaches outside the shape's own 40x40 extent.
	if !inkAt(img, 34, 60) {
		t.Error("no ink outside the shape; the blur did not spread")
	}
	// Coverage decreases monotonically walking away from the shape's edge.
	prev := coverageAt(img, 39, 60)
	if prev == 0 {
		t.Fatal("no coverage just outside the shape edge; the shadow did not paint")
	}
	for x := 38; x >= 30; x-- {
		c := coverageAt(img, x, 60)
		if c > prev {
			t.Fatalf("coverage rose walking away from the edge: %d at x=%d after %d", c, x, prev)
		}
		prev = c
	}
	// And it still does not paint under the box.
	if inkAt(img, 60, 60) {
		t.Error("ink under the box; a blurred outer shadow is still clipped outside the border box")
	}
}

// TestPaintBlurredInsetShadowFadesInward: an inner shadow's soft edge runs
// INWARD from the padding edge — its ink is where the interior is NOT, which is
// why the blurred silhouette's alpha is inverted rather than the ring being
// blurred directly. Blurring the ring would soften BOTH its edges and leave a
// bright seam at the padding box.
func TestPaintBlurredInsetShadowFadesInward(t *testing.T) {
	page := shadowPage(120, 120, layout.ShadowItem{
		XPt: 40, YPt: 40, WPt: 40, HPt: 40, Blur: 10, Spread: 4, Color: black, Inset: true,
	}, nil)
	img := newRasterPage(120, 120, page)

	// Full strength right at the inside edge, fading toward the centre.
	edge := coverageAt(img, 41, 60)
	mid := coverageAt(img, 50, 60)
	centre := coverageAt(img, 60, 60)
	if edge <= mid || mid <= centre {
		t.Errorf("inset blur coverage edge=%d mid=%d centre=%d, want a monotone inward falloff",
			edge, mid, centre)
	}
	if edge == 0 {
		t.Error("no coverage at the padding edge; a blurred inset shadow must be darkest there")
	}
	// Nothing escapes the box.
	if inkAt(img, 38, 60) || inkAt(img, 82, 60) {
		t.Error("ink outside the box; a blurred inset shadow must be clipped to its padding box")
	}
}

// TestPaintShadowStackingOrder: with several shadows, the item stream's LAST
// one paints on top. The layout engine reverses the source list into this
// stream (see pkg/layout/css's appendShadows), so this is the painter half of
// the spec's "the first shadow is on top".
func TestPaintShadowStackingOrder(t *testing.T) {
	red := color.RGBA{R: 0xff, A: 0xff}
	blue := color.RGBA{B: 0xff, A: 0xff}
	page := &layout.Page{WidthPt: 120, HeightPt: 120, Items: []layout.Item{
		// Emitted first => painted first => underneath.
		{Kind: layout.ShadowKind, Shadow: layout.ShadowItem{
			XPt: 40, YPt: 40, WPt: 40, HPt: 40, Spread: 20, Color: blue}},
		{Kind: layout.ShadowKind, Shadow: layout.ShadowItem{
			XPt: 40, YPt: 40, WPt: 40, HPt: 40, Spread: 10, Color: red}},
	}}
	img := newRasterPage(120, 120, page)

	// The red shadow's narrower band overlaps the blue one's, and wins there.
	if got := img.RGBAAt(35, 60); !isColor(got, red, 4) {
		t.Errorf("overlap pixel = %+v, want red on top (the later item paints over the earlier)", got)
	}
	// Outside red's reach, only blue.
	if got := img.RGBAAt(25, 60); !isColor(got, blue, 4) {
		t.Errorf("outer band pixel = %+v, want blue (only the wider shadow reaches here)", got)
	}
}

// TestPaintShadowDegeneratesQuietly pins every input that must paint nothing
// and never panic — the painter's contract for a malformed or hand-built item
// stream.
func TestPaintShadowDegeneratesQuietly(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    layout.ShadowItem
	}{
		{"transparent colour", layout.ShadowItem{XPt: 20, YPt: 20, WPt: 40, HPt: 40, Spread: 10}},
		{"zero width", layout.ShadowItem{XPt: 20, YPt: 20, WPt: 0, HPt: 40, Spread: 10, Color: black}},
		{"negative height", layout.ShadowItem{XPt: 20, YPt: 20, WPt: 40, HPt: -5, Spread: 10, Color: black}},
		{"NaN offset", layout.ShadowItem{XPt: 20, YPt: 20, WPt: 40, HPt: 40,
			OffsetX: nan(), Color: black}},
		{"infinite blur", layout.ShadowItem{XPt: 20, YPt: 20, WPt: 40, HPt: 40,
			Blur: inf(), Color: black}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := newRasterPage(100, 100, shadowPage(100, 100, tc.s, nil))
			if got := countInk(img); got != 0 {
				t.Errorf("painted %d px, want 0", got)
			}
		})
	}
}

// TestPaintBlurredShadowDegradesWithoutAnOffscreen: on a backend whose
// RenderOffscreen returns nil — pdfwrite, by design, because PDF has no blur
// operator and a blur has no vector representation — the shadow still paints,
// hard-edged and correctly placed.
//
// This is the SAME degradation convention the CSS `filter` path follows, and it
// is the one that makes a PDF stay vector rather than becoming a picture of
// itself. It must never be a blank: the shadow is part of the box's design.
func TestPaintBlurredShadowDegradesWithoutAnOffscreen(t *testing.T) {
	s := layout.ShadowItem{XPt: 20, YPt: 20, WPt: 40, HPt: 40, Spread: 10, Blur: 8, Color: black}
	dev := &recordDevice{} // its RenderOffscreen returns nil
	PaintPage(dev, shadowPage(100, 100, s, nil), render.Identity)

	if len(dev.fills) != 1 {
		t.Fatalf("got %d fills, want 1 (the hard-edged degradation)", len(dev.fills))
	}
	if dev.fills[0].paint.Rule != render.EvenOdd {
		t.Errorf("fill rule = %v, want EvenOdd (the shadow is a ring)", dev.fills[0].paint.Rule)
	}
	if dev.fills[0].paint.Color != black {
		t.Errorf("fill colour = %+v, want the shadow's own colour", dev.fills[0].paint.Color)
	}
	// The degradation is a plain vector fill: no offscreen image is drawn, which
	// is what keeps the PDF vector-native.
	if len(dev.clips) != 0 {
		t.Errorf("pushed %d clips, want 0 (the hard-edged path needs no clip)", len(dev.clips))
	}
}

// TestPaintUnblurredShadowIsAlwaysVector: a shadow with no blur never touches
// an offscreen surface even on a backend that HAS one, so the common
// `inset 3px 0 0 <color>` spine and every `0 0 0 <spread>` ring stay vector in
// a PDF at no cost.
func TestPaintUnblurredShadowIsAlwaysVector(t *testing.T) {
	s := layout.ShadowItem{XPt: 20, YPt: 20, WPt: 40, HPt: 40, OffsetX: 3, Color: black, Inset: true}
	dev := &offscreenCountingDevice{}
	PaintPage(dev, shadowPage(100, 100, s, nil), render.Identity)

	if dev.offscreens != 0 {
		t.Errorf("allocated %d offscreen surfaces for an unblurred shadow, want 0", dev.offscreens)
	}
	if len(dev.fills) != 1 {
		t.Errorf("got %d fills, want 1 vector fill", len(dev.fills))
	}
}

// offscreenCountingDevice is a recordDevice that also counts RenderOffscreen
// calls, so a test can prove a path never reaches for one.
type offscreenCountingDevice struct {
	recordDevice
	offscreens int
}

func (d *offscreenCountingDevice) RenderOffscreen(size image.Point, paint func(render.Device)) *image.RGBA {
	d.offscreens++
	return nil
}

func nan() float64 { return math.NaN() }
func inf() float64 { return math.Inf(1) }
