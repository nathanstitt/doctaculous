package draw

import (
	"image"
	"image/color"
	stddraw "image/draw"
	"testing"
	"time"

	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/render/raster"
	"github.com/nathanstitt/doctaculous/pkg/svg"
)

func TestDrawVector(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <rect x="10" y="10" width="20" height="20" fill="#ff0000"/>
	  <rect x="0" y="0" width="40" height="5" fill="none" stroke="blue" stroke-width="2"/>
	</svg>`
	doc, err := svg.Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	stddraw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	dev := raster.New(img)
	New(doc).DrawVector(dev, render.Identity)

	if got := img.RGBAAt(20, 20); got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("center = %+v, want red", got)
	}
	if got := img.RGBAAt(20, 35); got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("outside = %+v, want white", got)
	}
	// Stroke of width 2 on the y=5 edge: (20,5) is on the stroked line.
	if got := img.RGBAAt(20, 5); got.B < 200 || got.R > 100 {
		t.Errorf("stroke = %+v, want blue-ish", got)
	}
	// Scaled CTM: stroke width scales with it.
	img2 := image.NewRGBA(image.Rect(0, 0, 80, 80))
	stddraw.Draw(img2, img2.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	New(doc).DrawVector(raster.New(img2), render.Scale(2, 2))
	if got := img2.RGBAAt(40, 40); got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("scaled center = %+v", got)
	}
	// Proves stroke Width is scaled by sm.ScaleFactor(), not left in user
	// units: the bottom edge (user y=5) sits at device y=10 under Scale(2,2),
	// with a correctly-scaled 4px-wide band covering device y in [8,12). If
	// Width were left unscaled (2px), the band would be [9,11) instead and
	// (40,8) would be white, not blue.
	if got := img2.RGBAAt(40, 8); got.B < 200 || got.R > 100 {
		t.Errorf("scaled stroke at y=8 = %+v, want blue-ish (stroke width must scale with ctm)", got)
	}
}

// renderSVG parses src and paints it into a white w x h canvas at identity
// CTM, returning the resulting image for pixel sampling.
func renderSVG(t *testing.T, src string, w, h int) *image.RGBA {
	t.Helper()
	doc, err := svg.Parse([]byte(src), func(f string, a ...any) { t.Logf(f, a...) })
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	stddraw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	dev := raster.New(img)
	New(doc).DrawVector(dev, render.Identity)
	return img
}

// TestLinearGradientFillObjectBoundingBox verifies a left-to-right red->blue
// linear gradient fill, in the default objectBoundingBox unit space, paints a
// smooth left-to-right transition: red near the left edge, a red/blue mix at
// the midpoint, blue near the right edge.
func TestLinearGradientFillObjectBoundingBox(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <linearGradient id="g">
	    <stop offset="0" stop-color="red"/>
	    <stop offset="1" stop-color="blue"/>
	  </linearGradient>
	  <rect x="0" y="0" width="40" height="40" fill="url(#g)"/>
	</svg>`
	img := renderSVG(t, src, 40, 40)

	left := img.RGBAAt(5, 20)
	mid := img.RGBAAt(20, 20)
	right := img.RGBAAt(35, 20)
	t.Logf("left=%+v mid=%+v right=%+v", left, mid, right)

	if left.R < 200 || left.B > 60 {
		t.Errorf("left = %+v, want strongly red", left)
	}
	if right.B < 200 || right.R > 60 {
		t.Errorf("right = %+v, want strongly blue", right)
	}
	if mid.R < 60 || mid.R > 200 || mid.B < 60 || mid.B > 200 {
		t.Errorf("mid = %+v, want a red/blue mix (purple-ish)", mid)
	}
	// Monotonic transition: R decreases left->right, B increases.
	if left.R <= mid.R || mid.R <= right.R {
		t.Errorf("R channel not monotonically decreasing: left=%d mid=%d right=%d", left.R, mid.R, right.R)
	}
	if left.B >= mid.B || mid.B >= right.B {
		t.Errorf("B channel not monotonically increasing: left=%d mid=%d right=%d", left.B, mid.B, right.B)
	}
}

// TestLinearGradientUserSpaceOnUse verifies gradientUnits="userSpaceOnUse"
// coordinates are read as user-space lengths, not objectBoundingBox
// fractions: a gradient spanning only x in [0,20] over a 40-wide rect should
// reach its end color by the rect's midpoint, not its right edge.
func TestLinearGradientUserSpaceOnUse(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="20" y2="0">
	    <stop offset="0" stop-color="red"/>
	    <stop offset="1" stop-color="blue"/>
	  </linearGradient>
	  <rect x="0" y="0" width="40" height="40" fill="url(#g)"/>
	</svg>`
	img := renderSVG(t, src, 40, 40)

	// At x=20 (the gradient's end, half way across the rect) it should
	// already be fully blue, and stay blue out to the right edge (pad spread).
	at20 := img.RGBAAt(20, 20)
	at35 := img.RGBAAt(35, 20)
	t.Logf("at20=%+v at35=%+v", at20, at35)
	if at20.B < 200 || at20.R > 60 {
		t.Errorf("at x=20 = %+v, want strongly blue (gradient end reached at user-space x=20)", at20)
	}
	if at35.B < 200 || at35.R > 60 {
		t.Errorf("at x=35 = %+v, want strongly blue (pad spread beyond gradient end)", at35)
	}
}

// TestLinearGradientTransform verifies gradientTransform visibly shifts the
// gradient: translating it by the full width should make the whole visible
// rect show (approximately) the start color rather than the ramp.
func TestLinearGradientTransform(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <linearGradient id="g" gradientTransform="translate(-2 0)">
	    <stop offset="0" stop-color="red"/>
	    <stop offset="1" stop-color="blue"/>
	  </linearGradient>
	  <rect x="0" y="0" width="40" height="40" fill="url(#g)"/>
	</svg>`
	img := renderSVG(t, src, 40, 40)

	baseSrc := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <linearGradient id="g">
	    <stop offset="0" stop-color="red"/>
	    <stop offset="1" stop-color="blue"/>
	  </linearGradient>
	  <rect x="0" y="0" width="40" height="40" fill="url(#g)"/>
	</svg>`
	base := renderSVG(t, baseSrc, 40, 40)

	shifted := img.RGBAAt(20, 20)
	unshifted := base.RGBAAt(20, 20)
	t.Logf("shifted=%+v unshifted=%+v", shifted, unshifted)
	if shifted.B <= unshifted.B {
		t.Errorf("gradientTransform did not visibly shift the gradient: shifted=%+v unshifted=%+v", shifted, unshifted)
	}
}

// TestRadialGradientFill verifies a radial gradient paints its start color at
// the center and its end color out toward the edge.
func TestRadialGradientFill(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <radialGradient id="g">
	    <stop offset="0" stop-color="red"/>
	    <stop offset="1" stop-color="blue"/>
	  </radialGradient>
	  <rect x="0" y="0" width="40" height="40" fill="url(#g)"/>
	</svg>`
	img := renderSVG(t, src, 40, 40)

	center := img.RGBAAt(20, 20)
	edge := img.RGBAAt(39, 20)
	t.Logf("center=%+v edge=%+v", center, edge)
	if center.R < 200 || center.B > 60 {
		t.Errorf("center = %+v, want strongly red", center)
	}
	if edge.B < 150 || edge.R > 120 {
		t.Errorf("edge = %+v, want blue-leaning", edge)
	}
}

// TestGradientDegenerateBoundingBoxDoesNotPanic verifies a shape with zero
// geometric extent along one axis (a horizontal line has zero height) used
// with an objectBoundingBox gradient fill does not panic, and per spec paints
// nothing extra (the degenerate gradient is simply not rendered).
func TestGradientDegenerateBoundingBoxDoesNotPanic(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <linearGradient id="g">
	    <stop offset="0" stop-color="red"/>
	    <stop offset="1" stop-color="blue"/>
	  </linearGradient>
	  <line x1="0" y1="20" x2="40" y2="20" stroke="url(#g)" stroke-width="4"/>
	  <rect x="0" y="0" width="0" height="20" fill="url(#g)"/>
	</svg>`
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on degenerate bbox: %v", r)
		}
	}()
	_ = renderSVG(t, src, 40, 40)
}

// TestStrokeGradientDegradesToFallbackColor documents and locks in the
// current stroke-gradient decision: pkg/render/raster has no stroke-to-path
// outline conversion to clip a shading against, so stroke="url(#g) green"
// degrades to the fallback color green (and stroke="url(#g)" with no
// fallback paints no stroke at all), rather than a gradient. This is a
// documented follow-up, not the final behavior.
func TestStrokeGradientDegradesToFallbackColor(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <linearGradient id="g">
	    <stop offset="0" stop-color="red"/>
	    <stop offset="1" stop-color="blue"/>
	  </linearGradient>
	  <line x1="2" y1="20" x2="38" y2="20" stroke="url(#g) green" stroke-width="6"/>
	</svg>`
	img := renderSVG(t, src, 40, 40)
	got := img.RGBAAt(20, 20)
	t.Logf("stroke fallback color = %+v", got)
	if got.G < 100 || got.R > 60 || got.B > 60 {
		t.Errorf("stroke = %+v, want the fallback color green (gradient strokes degrade to fallback)", got)
	}
}

// checkerPatternSVG builds a 40x40 document filled with a 10x10
// userSpaceOnUse pattern tile: a red square in the tile's left half, nothing
// (background white) in its right half. The tile repeats 4x along each axis
// over the 40x40 fill rect, so x=2 (left half of cell 0), x=12 (left half of
// cell 1) and x=22 (left half of cell 2) should all read red, while x=7
// (right half of cell 0) should read white (untouched fill background).
func checkerPatternSVG(extra string) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <pattern id="p" patternUnits="userSpaceOnUse" width="10" height="10" ` + extra + `>
	    <rect x="0" y="0" width="5" height="10" fill="red"/>
	  </pattern>
	  <rect x="0" y="0" width="40" height="40" fill="url(#p)"/>
	</svg>`
}

// TestPatternFillTiles verifies a pattern fill repeats its tile content
// across the shape: two positions at the same offset within different tile
// cells read the same color (both inside the tile's red half), while a
// position between tiles (the tile's transparent half) differs.
func TestPatternFillTiles(t *testing.T) {
	img := renderSVG(t, checkerPatternSVG(""), 40, 40)

	cell0 := img.RGBAAt(2, 5)  // left half of tile cell (0,0): red
	cell1 := img.RGBAAt(12, 5) // left half of tile cell (1,0): red
	cell2 := img.RGBAAt(22, 5) // left half of tile cell (2,0): red
	gap := img.RGBAAt(7, 5)    // right half of tile cell (0,0): untouched white

	t.Logf("cell0=%+v cell1=%+v cell2=%+v gap=%+v", cell0, cell1, cell2, gap)

	if cell0 != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("cell0 = %+v, want red", cell0)
	}
	if cell1 != cell0 {
		t.Errorf("cell1 = %+v, want to match cell0 (%+v) — pattern must tile", cell1, cell0)
	}
	if cell2 != cell0 {
		t.Errorf("cell2 = %+v, want to match cell0 (%+v) — pattern must tile", cell2, cell0)
	}
	if gap == cell0 {
		t.Errorf("gap = %+v, want to DIFFER from the tile's red half (%+v) — same color everywhere would mean it isn't really tiling", gap, cell0)
	}
	if gap != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("gap = %+v, want untouched white", gap)
	}
}

// TestPatternTransformApplies verifies patternTransform visibly shifts the
// tile grid: translating by half a tile width should swap which half of each
// cell reads red.
func TestPatternTransformApplies(t *testing.T) {
	img := renderSVG(t, checkerPatternSVG(`patternTransform="translate(5 0)"`), 40, 40)

	// Under the untransformed pattern (see TestPatternFillTiles), x=2 is red
	// and x=7 is white within the first cell. Shifting the pattern right by
	// 5 should swap that: x=2 becomes white, x=7 becomes red.
	shiftedLeft := img.RGBAAt(2, 5)
	shiftedRight := img.RGBAAt(7, 5)
	t.Logf("shiftedLeft=%+v shiftedRight=%+v", shiftedLeft, shiftedRight)

	if shiftedLeft != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("shiftedLeft = %+v, want white (patternTransform must shift the grid)", shiftedLeft)
	}
	if shiftedRight != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("shiftedRight = %+v, want red (patternTransform must shift the grid)", shiftedRight)
	}
}

// TestPatternWithNoChildrenPaintsNothing verifies a <pattern> with zero
// children paints nothing at all (the shape's fill is simply absent), per
// SVG's rule that an invalid/empty paint server is treated as unpainted
// rather than falling back to any default color.
func TestPatternWithNoChildrenPaintsNothing(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <pattern id="p" patternUnits="userSpaceOnUse" width="10" height="10"></pattern>
	  <rect x="0" y="0" width="40" height="40" fill="url(#p)"/>
	</svg>`
	img := renderSVG(t, src, 40, 40)
	got := img.RGBAAt(20, 20)
	if got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("center = %+v, want untouched white (empty pattern paints nothing)", got)
	}
}

// TestPatternZeroSizePaintsNothing verifies a <pattern> with zero width or
// height paints nothing, per SVG's explicit "width or height of zero
// disables rendering of the element referencing it" rule.
func TestPatternZeroSizePaintsNothing(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <pattern id="p" patternUnits="userSpaceOnUse" width="0" height="10">
	    <rect x="0" y="0" width="10" height="10" fill="red"/>
	  </pattern>
	  <rect x="0" y="0" width="40" height="40" fill="url(#p)"/>
	</svg>`
	img := renderSVG(t, src, 40, 40)
	got := img.RGBAAt(20, 20)
	if got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("center = %+v, want untouched white (zero-size pattern tile paints nothing)", got)
	}
}

// TestSelfReferencingPatternTerminates verifies a <pattern> whose tile
// content references the pattern itself (fill="url(#p)" on a shape inside
// <pattern id="p">) does not recurse forever: the self-referencing shape's
// own fill resolves to nothing (the cycle guard stops it), but the pattern
// still paints via its OTHER, non-cyclic tile content. This must complete
// quickly, not hang.
func TestSelfReferencingPatternTerminates(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <pattern id="p" patternUnits="userSpaceOnUse" width="10" height="10">
	    <rect x="0" y="0" width="10" height="10" fill="red"/>
	    <rect x="0" y="0" width="5" height="5" fill="url(#p)"/>
	  </pattern>
	  <rect x="0" y="0" width="40" height="40" fill="url(#p)"/>
	</svg>`

	done := make(chan *image.RGBA, 1)
	go func() { done <- renderSVG(t, src, 40, 40) }()

	select {
	case img := <-done:
		// The tile's first rect (solid red, no self-reference) still paints;
		// only the second, self-referencing rect's own fill is dropped.
		got := img.RGBAAt(2, 2)
		if got != (color.RGBA{255, 0, 0, 255}) {
			t.Errorf("center = %+v, want red from the tile's non-cyclic rect", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("self-referencing pattern did not terminate within 5s (infinite recursion?)")
	}
}
