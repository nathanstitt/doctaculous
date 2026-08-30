package draw

import (
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"strings"
	"testing"
	"time"

	"github.com/nathanstitt/omnidoc/pkg/render"
	"github.com/nathanstitt/omnidoc/pkg/render/raster"
	"github.com/nathanstitt/omnidoc/pkg/svg"
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

// TestLinearGradientFillObjectBoundingBoxOffsetRect verifies the same
// left-to-right red->blue transition as TestLinearGradientFillObjectBoundingBox,
// but for a rect NOT anchored at the origin. objectBoundingBox composes a
// Translate(minX,minY) with a Scale(w,h): with the rect at (0,0),
// Translate(0,0) is the identity, so composing the two in the wrong order
// (translate-then-scale vs. scale-then-translate) is invisible — this test
// pins a rect at (10,10) specifically so a regression in that composition
// order (which blows up the translation by the scale factor) fails loudly
// instead of only manifesting on off-origin shapes.
func TestLinearGradientFillObjectBoundingBoxOffsetRect(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="60" height="60">
	  <linearGradient id="g">
	    <stop offset="0" stop-color="red"/>
	    <stop offset="1" stop-color="blue"/>
	  </linearGradient>
	  <rect x="10" y="10" width="40" height="40" fill="url(#g)"/>
	</svg>`
	img := renderSVG(t, src, 60, 60)

	left := img.RGBAAt(15, 30)
	mid := img.RGBAAt(30, 30)
	right := img.RGBAAt(45, 30)
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
	// Outside the rect entirely: must stay the white background, not a
	// bogus color from a mis-translated gradient painting the wrong region.
	if outside := img.RGBAAt(2, 2); outside != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("outside rect = %+v, want white background untouched", outside)
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

// nestedDistinctPatternsSVG builds a chain of depth DISTINCT patterns
// (p0's tile fills with p1, p1's with p2, ..., p(depth-1) is a plain solid
// tile), each tile a 10x10 grid of ~10x10 cells over the 200x200 fill rect
// (~400 cells/level): p0's tile fills with p1, not itself, so
// sceneBuilder.buildingPattern's cycle guard (which only fires when a
// pattern id recurs) never trips for this chain, regardless of depth.
func nestedDistinctPatternsSVG(depth int) string {
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200">`)
	for i := 0; i < depth; i++ {
		fmt.Fprintf(&sb, `<pattern id="p%d" patternUnits="userSpaceOnUse" width="10" height="10">`, i)
		if i == depth-1 {
			sb.WriteString(`<rect x="0" y="0" width="5" height="5" fill="red"/>`)
		} else {
			fmt.Fprintf(&sb, `<rect x="0" y="0" width="10" height="10" fill="url(#p%d)"/>`, i+1)
		}
		sb.WriteString(`</pattern>`)
	}
	sb.WriteString(`<rect x="0" y="0" width="200" height="200" fill="url(#p0)"/></svg>`)
	return sb.String()
}

// TestNestedDistinctPatternsDoNotBlowUp verifies a chain of DISTINCT nested
// patterns (not a cycle: every pattern id along the chain is unique, so
// pkg/svg's buildingPattern cycle guard never fires) is bounded by
// pkg/svg/draw's nesting-depth guard. Absent that guard, draw calls multiply
// by each level's own tile cell count (~400/level here) and blow up
// exponentially — depth 8 would otherwise take well over 20s.
//
// The assertion is on the GUARD FIRING, not on wall-clock time: a timing
// threshold is machine- and instrumentation-dependent (it flaked in CI under
// -race at 1.04s against a 1s bound) and only infers the mechanism, whereas
// the log line proves the depth cap actually stopped the descent.
func TestNestedDistinctPatternsDoNotBlowUp(t *testing.T) {
	doc, err := svg.Parse([]byte(nestedDistinctPatternsSVG(8)), nil)
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	stddraw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	r := New(doc)
	r.Logf = func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	r.DrawVector(raster.New(img), render.Identity)

	var tripped bool
	for _, l := range logs {
		if strings.Contains(l, "nesting exceeded") {
			tripped = true
			t.Logf("guard fired: %s", l)
		}
	}
	if !tripped {
		t.Errorf("depth-8 nested distinct patterns did not trip the nesting-depth guard; logs=%v", logs)
	}
}

// TestNestedDistinctPatternsShallowChainStillPaints verifies the
// nesting-depth guard does not trip on a realistic, shallow chain: a 3-level
// nested-distinct-pattern fill must still paint its innermost tile's color,
// not degrade to unpainted.
func TestNestedDistinctPatternsShallowChainStillPaints(t *testing.T) {
	img := renderSVG(t, nestedDistinctPatternsSVG(3), 200, 200)
	got := img.RGBAAt(2, 2)
	if got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("center = %+v, want red from the innermost tile (a shallow, non-cyclic chain must still paint)", got)
	}
}

// nestedGroupsSVG builds depth levels of nested <g opacity="0.999">, each
// wrapping the next, with a single leaf rect at the center. opacity is kept
// just under 1 (never exactly 1) so every level takes the real
// BeginGroup/EndGroup compositing path (see paint's Group case: opacity>=1
// with no clip/mask skips group-opening entirely) — the path
// maxGroupNestingDepth guards — while the cumulative opacity stays
// indistinguishable from fully opaque for pixel-level assertions.
func nestedGroupsSVG(depth int) string {
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">`)
	for i := 0; i < depth; i++ {
		sb.WriteString(`<g opacity="0.999">`)
	}
	sb.WriteString(`<rect x="10" y="10" width="20" height="20" fill="#000000"/>`)
	for i := 0; i < depth; i++ {
		sb.WriteString(`</g>`)
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}

// TestDeeplyNestedGroupsDoNotExhaustMemory is MUST-FIX 2's regression test:
// every BeginGroup allocates a full-canvas scratch RGBA that lives until its
// matching EndGroup (see raster.Device.BeginGroup), so unbounded nesting
// depth is unbounded concurrently-live scratch memory, reachable from
// Open/OpenBytes on untrusted input via nested <g opacity>/clip-path/mask.
// This renders a document nested far deeper (200 levels) than
// maxGroupNestingDepth (16) and asserts it completes at all (the real risk
// is an OOM kill or multi-GB allocation, not a hang — a timeout would not
// catch a memory blowup the way it catches a runaway loop) AND that the
// depth-cap guard actually fired, the same log-line-based assertion
// TestNestedDistinctPatternsDoNotBlowUp uses for the analogous pattern-depth
// guard.
func TestDeeplyNestedGroupsDoNotExhaustMemory(t *testing.T) {
	doc, err := svg.Parse([]byte(nestedGroupsSVG(200)), nil)
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	stddraw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	r := New(doc)
	r.Logf = func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }

	done := make(chan struct{})
	go func() {
		r.DrawVector(raster.New(img), render.Identity)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("200-deep nested groups did not complete within 10s (unbounded recursion/allocation?)")
	}

	var tripped bool
	for _, l := range logs {
		if strings.Contains(l, "nesting exceeded") {
			tripped = true
			t.Logf("guard fired: %s", l)
		}
	}
	if !tripped {
		t.Errorf("200-deep nested groups did not trip the group-nesting-depth guard; logs=%v", logs)
	}

	// The leaf rect must still have painted (degraded past the cap, not
	// dropped): center of the 20x20 rect at (10,10) is (20,20).
	got := img.RGBAAt(20, 20)
	if got.R > 20 || got.G > 20 || got.B > 20 {
		t.Errorf("center = %+v, want ~black (leaf content must still paint once the depth cap trips, just without further isolation)", got)
	}
}

// TestModestlyNestedGroupsStillRenderCorrectly verifies a realistic nesting
// depth (4 levels, well under maxGroupNestingDepth) is unaffected by the
// cap and still composites its cumulative opacity correctly end to end —
// the guard must not trip, or degrade fidelity, for ordinary documents.
func TestModestlyNestedGroupsStillRenderCorrectly(t *testing.T) {
	img := renderSVG(t, nestedGroupsSVG(4), 40, 40)
	got := img.RGBAAt(20, 20)
	t.Logf("4-deep nested opacity=0.999 groups, center = %+v", got)
	// Cumulative opacity 0.999^4 ~= 0.996, i.e. still essentially opaque
	// black -- this is a correctness check (the content survived 4 levels
	// of real compositing), not a precision check on the opacity math.
	if got.R > 5 || got.G > 5 || got.B > 5 {
		t.Errorf("center = %+v, want ~black (near-fully-opaque black through 4 nested compositing groups)", got)
	}
}

// TestGroupOpacityCompositesOnce is the discriminating test for group
// opacity: two OVERLAPPING opaque shapes inside <g opacity="0.5"> must
// composite to the SAME color at the overlap as at a non-overlap area. Under
// the old (dropped) behavior, group opacity did nothing at all, so both
// squares painted fully opaque and the overlap would show whichever square
// painted last, not a blend — this test's real job is pinning that group
// opacity blends correctly now. If group opacity were instead approximated
// by threading it into each child's own paint alpha (the artifact this
// feature exists to avoid), the overlap would come out TWICE as dark as a
// non-overlap area, since two 50%-alpha black-ish paints stacked over white
// do not equal one 50%-alpha paint. Both pixel values are logged so the
// reported artifact is visible directly in test output.
func TestGroupOpacityCompositesOnce(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <g opacity="0.5">
	    <rect x="5" y="5" width="20" height="20" fill="#000000"/>
	    <rect x="15" y="5" width="20" height="20" fill="#000000"/>
	  </g>
	</svg>`
	img := renderSVG(t, src, 40, 40)

	overlap := img.RGBAAt(17, 15)   // x in [15,25): covered by BOTH rects
	nonOverlap := img.RGBAAt(7, 15) // x in [5,15): covered by only the first rect
	t.Logf("overlap=%+v nonOverlap=%+v", overlap, nonOverlap)

	if overlap != nonOverlap {
		t.Errorf("overlap = %+v, nonOverlap = %+v; want EQUAL (group opacity must composite once, not per child — a per-paint approximation would double-darken the overlap)", overlap, nonOverlap)
	}
	// Both should show the 50%-black-on-white blend (~127,127,127), not full
	// black (100% opacity, opacity ignored) or full white (nothing painted).
	if nonOverlap.R < 100 || nonOverlap.R > 155 {
		t.Errorf("nonOverlap = %+v, want ~127 gray (50%% black over white)", nonOverlap)
	}
}

// TestNestedGroupOpacityMultiplies verifies <g opacity="0.5"><g
// opacity="0.5"> gives an effective 0.25, not 0.5 (nesting must multiply,
// not just take the innermost or outermost value).
func TestNestedGroupOpacityMultiplies(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <g opacity="0.5">
	    <g opacity="0.5">
	      <rect x="0" y="0" width="40" height="40" fill="#000000"/>
	    </g>
	  </g>
	</svg>`
	img := renderSVG(t, src, 40, 40)
	got := img.RGBAAt(20, 20)
	t.Logf("nested 0.5*0.5 = %+v", got)
	// Effective alpha 0.25 over white: ~191 gray (255*0.75).
	if got.R < 180 || got.R > 202 {
		t.Errorf("center = %+v, want ~191 gray (effective opacity 0.25 = 0.5*0.5)", got)
	}
}

// TestPlainGroupDoesNotOpenOffscreenGroup verifies a <g> with no opacity
// attribute (or opacity="1") takes the cheap per-paint path and never calls
// BeginGroup/EndGroup: opening an offscreen group allocates a full-page
// scratch buffer (see raster.Device.BeginGroup), so doing that for every
// plain <g> — the overwhelming common case — would be a serious, needless
// performance regression. This is asserted directly against a
// render.Device double that panics on BeginGroup/EndGroup, rather than
// inferred from timing, so a regression fails loudly and unambiguously.
func TestPlainGroupDoesNotOpenOffscreenGroup(t *testing.T) {
	doc, err := svg.Parse([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <g>
	    <rect x="0" y="0" width="10" height="10" fill="red"/>
	    <g opacity="1">
	      <rect x="10" y="10" width="10" height="10" fill="blue"/>
	    </g>
	  </g>
	</svg>`), nil)
	if err != nil {
		t.Fatal(err)
	}
	dev := &noGroupDevice{Device: raster.New(image.NewRGBA(image.Rect(0, 0, 40, 40)))}
	New(doc).DrawVector(dev, render.Identity)
	if dev.opened {
		t.Errorf("BeginGroup was called for a plain <g> / opacity=1 group; want no offscreen group at all")
	}
}

// noGroupDevice wraps a real render.Device and records whether
// BeginGroup/EndGroup were ever invoked, without altering any other
// behavior (every other method delegates to the embedded Device).
type noGroupDevice struct {
	render.Device
	opened bool
}

func (d *noGroupDevice) BeginGroup() {
	d.opened = true
	d.Device.BeginGroup()
}

// TestShapeFillAndStrokeOpacityNoSeam verifies a single shape with BOTH a
// fill and a stroke at opacity < 1 shows no seam where the stroke overlaps
// the fill: the stroke's inner half overlaps the fill along the fill's
// border, and folding opacity into each paint independently would double
// them there (the same double-darkening artifact group opacity produces),
// giving three visibly different bands (fill-only, fill+stroke overlap,
// stroke-only) instead of two. This mirrors the resvg
// painting/{fill,stroke}-opacity/with-opacity.svg fixtures, which carry
// exactly this fill+stroke+opacity combination.
func TestShapeFillAndStrokeOpacityNoSeam(t *testing.T) {
	// A 20x20 black-filled rect centered in a 40x40 canvas, with an 8-wide
	// black stroke (4 inside + 4 outside the fill edge) at opacity 0.5.
	// Sampling straight down the middle of the top edge:
	//   y=12: outside the fill, only the stroke's outer half -> stroke-only.
	//   y=17: inside the fill AND under the stroke's inner half -> overlap.
	//   y=20: well inside the fill, past the stroke -> fill-only.
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <rect x="10" y="10" width="20" height="20" fill="#000000" stroke="#000000" stroke-width="8" opacity="0.5"/>
	</svg>`
	img := renderSVG(t, src, 40, 40)

	strokeOnly := img.RGBAAt(20, 12)
	overlap := img.RGBAAt(20, 17)
	fillOnly := img.RGBAAt(20, 20)
	t.Logf("strokeOnly=%+v overlap=%+v fillOnly=%+v", strokeOnly, overlap, fillOnly)

	if overlap != strokeOnly {
		t.Errorf("overlap = %+v, strokeOnly = %+v; want EQUAL (opacity must apply once to the composited shape, not per-paint)", overlap, strokeOnly)
	}
	if overlap != fillOnly {
		t.Errorf("overlap = %+v, fillOnly = %+v; want EQUAL (opacity must apply once to the composited shape, not per-paint)", overlap, fillOnly)
	}
	// All three should show the 50%-black-on-white blend (~127 gray), not
	// full black or a double-darkened ~64 gray at the overlap.
	if overlap.R < 100 || overlap.R > 155 {
		t.Errorf("overlap = %+v, want ~127 gray (50%% black over white, not double-darkened)", overlap)
	}
}

// TestShapeOpacityZeroAndOneAndAbsent verifies the boundary behavior for
// element opacity on a shape carrying both a fill and a stroke (the grouped
// path): opacity="0" paints nothing, and opacity="1" (or the attribute
// entirely absent) renders identically to the ungrouped baseline.
func TestShapeOpacityZeroAndOneAndAbsent(t *testing.T) {
	shape := func(opacityAttr string) string {
		return `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
		  <rect x="10" y="10" width="20" height="20" fill="#112233" stroke="#445566" stroke-width="4"` + opacityAttr + `/>
		</svg>`
	}

	t.Run("opacity=0 paints nothing", func(t *testing.T) {
		img := renderSVG(t, shape(` opacity="0"`), 40, 40)
		got := img.RGBAAt(20, 20)
		if got != (color.RGBA{255, 255, 255, 255}) {
			t.Errorf("center = %+v, want untouched white (opacity=0 must paint nothing)", got)
		}
	})

	t.Run("opacity=1 matches opacity absent", func(t *testing.T) {
		withOne := renderSVG(t, shape(` opacity="1"`), 40, 40)
		absent := renderSVG(t, shape(""), 40, 40)
		p1 := withOne.RGBAAt(20, 20)
		p2 := absent.RGBAAt(20, 20)
		fillEdge1 := withOne.RGBAAt(10, 20)
		fillEdge2 := absent.RGBAAt(10, 20)
		t.Logf("center: opacity=1 -> %+v, absent -> %+v", p1, p2)
		t.Logf("stroke edge: opacity=1 -> %+v, absent -> %+v", fillEdge1, fillEdge2)
		if p1 != p2 {
			t.Errorf("center opacity=1 = %+v, absent = %+v; want identical", p1, p2)
		}
		if fillEdge1 != fillEdge2 {
			t.Errorf("stroke-edge opacity=1 = %+v, absent = %+v; want identical", fillEdge1, fillEdge2)
		}
	})
}

// TestRootOpacityApplies verifies a root <svg opacity="0.5"> applies to the
// whole document's painted content, matching a <g opacity="0.5"> wrapping
// the same content.
func TestRootOpacityApplies(t *testing.T) {
	rootOpacity := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" opacity="0.5">
	  <rect x="0" y="0" width="40" height="40" fill="#000000"/>
	</svg>`
	groupOpacity := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <g opacity="0.5"><rect x="0" y="0" width="40" height="40" fill="#000000"/></g>
	</svg>`

	imgRoot := renderSVG(t, rootOpacity, 40, 40)
	imgGroup := renderSVG(t, groupOpacity, 40, 40)

	got := imgRoot.RGBAAt(20, 20)
	want := imgGroup.RGBAAt(20, 20)
	t.Logf("root opacity=%+v, group opacity=%+v", got, want)
	if got != want {
		t.Errorf("root <svg opacity> = %+v, want to match <g opacity> equivalent = %+v", got, want)
	}
	if got.R < 100 || got.R > 155 {
		t.Errorf("root opacity result = %+v, want ~127 gray (50%% black over white)", got)
	}
}

// describableShader is a minimal render.ShadingDescriber fake, used to prove
// alphaShader delegates DescribeShading rather than hiding it (the "wrapper
// trap" render.ShadingDescriber's doc comment warns about). Its ColorAt is
// unused by these tests; only DescribeShading matters.
type describableShader struct {
	desc render.ShadingDesc
}

func (d describableShader) ColorAt(float64, float64) (color.RGBA, bool) {
	return color.RGBA{}, false
}

func (d describableShader) DescribeShading() (render.ShadingDesc, bool) {
	return d.desc, true
}

// TestAlphaShaderDelegatesDescribeShading is the regression guard for the
// wrapper trap render.ShadingDescriber's doc comment calls out: alphaShader
// wraps a gradient shader on the common path (any gradient under a <g
// opacity> or carrying its own opacity), so if it only forwarded ColorAt, a
// PDF writer's type-assertion for render.ShadingDescriber would see the
// alphaShader value (which wouldn't implement it), miss the describable
// shader underneath, and silently fall back to rasterizing exactly the
// documents most likely to have gradients. This asserts the description
// still comes through AND that every stop's alpha is scaled by the wrapper's
// factor, via the same scaleAlpha helper ColorAt uses.
func TestAlphaShaderDelegatesDescribeShading(t *testing.T) {
	inner := describableShader{desc: render.ShadingDesc{
		Kind:   render.ShadingAxial,
		Coords: [6]float64{0, 0, 10, 0, 0, 0},
		Stops: []render.ShadingStop{
			{Offset: 0, Color: color.RGBA{255, 0, 0, 200}},
			{Offset: 1, Color: color.RGBA{0, 0, 255, 100}},
		},
		Spread: render.SpreadPad,
	}}
	wrapped := alphaShader{inner: inner, alpha: 0.5}

	describer, ok := render.Shader(wrapped).(render.ShadingDescriber)
	if !ok {
		t.Fatalf("alphaShader does not implement render.ShadingDescriber")
	}
	desc, ok := describer.DescribeShading()
	if !ok {
		t.Fatalf("DescribeShading reported !ok; delegation to a describable inner shader must succeed")
	}

	if desc.Kind != render.ShadingAxial {
		t.Errorf("Kind = %v, want ShadingAxial (geometry must pass through unchanged)", desc.Kind)
	}
	if desc.Coords != inner.desc.Coords {
		t.Errorf("Coords = %v, want %v (geometry must pass through unchanged)", desc.Coords, inner.desc.Coords)
	}
	if desc.Spread != render.SpreadPad {
		t.Errorf("Spread = %v, want SpreadPad (spread must pass through unchanged)", desc.Spread)
	}

	if len(desc.Stops) != len(inner.desc.Stops) {
		t.Fatalf("Stops = %v, want %d entries", desc.Stops, len(inner.desc.Stops))
	}
	wantAlphas := []uint8{scaleAlpha(200, 0.5), scaleAlpha(100, 0.5)}
	for i, want := range wantAlphas {
		got := desc.Stops[i]
		src := inner.desc.Stops[i]
		if got.Offset != src.Offset {
			t.Errorf("Stops[%d].Offset = %v, want %v", i, got.Offset, src.Offset)
		}
		if got.Color.R != src.Color.R || got.Color.G != src.Color.G || got.Color.B != src.Color.B {
			t.Errorf("Stops[%d] RGB = %+v, want RGB from %+v unchanged", i, got.Color, src.Color)
		}
		if got.Color.A != want {
			t.Errorf("Stops[%d].Color.A = %d, want %d (scaleAlpha(%d, 0.5))", i, got.Color.A, want, src.Color.A)
		}
	}
}
