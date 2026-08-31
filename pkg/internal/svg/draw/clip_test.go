package draw

import (
	"image"
	"testing"
	"time"
)

const clipSVGHdr = `xmlns="http://www.w3.org/2000/svg"`

// TestClipPathUnionShowsBothNonOverlappingChildren is THE discriminating
// test: two non-overlapping circles inside one <clipPath> must clip their
// target to the UNION (both circles show through). Under a naive
// repeated-PushClip implementation the two circles would INTERSECT to
// empty, and the rect would vanish entirely everywhere, including at both
// circle centers.
func TestClipPathUnionShowsBothNonOverlappingChildren(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="c1">
	    <circle cx="20" cy="50" r="15"/>
	    <circle cx="80" cy="50" r="15"/>
	  </clipPath>
	  <rect width="100" height="100" fill="red" clip-path="url(#c1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)

	left := img.RGBAAt(20, 50)
	right := img.RGBAAt(80, 50)
	between := img.RGBAAt(50, 50)

	t.Logf("left circle center (20,50) = %+v", left)
	t.Logf("right circle center (80,50) = %+v", right)
	t.Logf("between circles (50,50) = %+v", between)

	if left.R < 200 || left.G > 50 {
		t.Errorf("left circle center = %+v, want red (union must show BOTH non-overlapping children)", left)
	}
	if right.R < 200 || right.G > 50 {
		t.Errorf("right circle center = %+v, want red (union must show BOTH non-overlapping children)", right)
	}
	if between.R != 255 || between.G != 255 || between.B != 255 {
		t.Errorf("between circles = %+v, want unpainted white (outside both children)", between)
	}
}

// TestClipPathMixedClipRule verifies a clipPath with one nonzero and one
// evenodd child unions correctly, each child evaluated under its OWN rule.
func TestClipPathMixedClipRule(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="c1">
	    <path clip-rule="evenodd" d="M10,10 L50,10 L50,50 L10,50 Z M20,20 L40,20 L40,40 L20,40 Z"/>
	    <rect x="60" y="10" width="30" height="30" clip-rule="nonzero"/>
	  </clipPath>
	  <rect width="100" height="100" fill="red" clip-path="url(#c1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)

	if c := img.RGBAAt(30, 30); c.R != 255 || c.G != 255 {
		t.Errorf("evenodd donut hole at (30,30) = %+v, want unpainted white", c)
	}
	if c := img.RGBAAt(15, 15); c.R < 200 {
		t.Errorf("evenodd donut ring at (15,15) = %+v, want red", c)
	}
	if c := img.RGBAAt(75, 25); c.R < 200 {
		t.Errorf("nonzero rect at (75,25) = %+v, want red", c)
	}
}

// TestClipPathOverlappingEvenOdd verifies two overlapping children under
// evenodd union to cover the overlap (each child's evenodd interior is
// computed independently, then unioned) rather than the overlap being
// punched out by a jointly-evaluated evenodd rule over concatenated
// geometry.
func TestClipPathOverlappingEvenOdd(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="c1" clip-rule="evenodd">
	    <rect x="10" y="10" width="50" height="50"/>
	    <rect x="40" y="40" width="50" height="50"/>
	  </clipPath>
	  <rect width="100" height="100" fill="red" clip-path="url(#c1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)

	if c := img.RGBAAt(50, 50); c.R < 200 {
		t.Errorf("overlap of two evenodd rects = %+v, want red (union, not a jointly-punched hole)", c)
	}
	if c := img.RGBAAt(20, 20); c.R < 200 {
		t.Errorf("rect A only = %+v, want red", c)
	}
	if c := img.RGBAAt(80, 80); c.R < 200 {
		t.Errorf("rect B only = %+v, want red", c)
	}
}

// TestClipPathUnitsObjectBoundingBox verifies clipPathUnits="objectBoundingBox"
// maps clip geometry through the CLIPPED element's own bounding box: a unit
// circle at (0.5,0.5) r=0.5 clips to the shape's inscribed circle.
func TestClipPathUnitsObjectBoundingBox(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="c1" clipPathUnits="objectBoundingBox">
	    <circle cx="0.5" cy="0.5" r="0.5"/>
	  </clipPath>
	  <rect x="10" y="10" width="80" height="80" fill="red" clip-path="url(#c1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)

	// Center of the rect's bounding box (50,50): inside the inscribed circle.
	if c := img.RGBAAt(50, 50); c.R < 200 {
		t.Errorf("bbox center = %+v, want red (inside objectBoundingBox clip circle)", c)
	}
	// Corner of the rect's bounding box (12,12): outside the inscribed circle
	// but still inside the 10..90 rect itself, so this only shows red if the
	// clip is applied and its geometry excludes the corner.
	if c := img.RGBAAt(12, 12); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("bbox corner = %+v, want unpainted white (outside the inscribed clip circle)", c)
	}
}

// TestClipPathUserSpaceOnUseIsAbsolute verifies the userSpaceOnUse default
// treats clip geometry as absolute user-space coordinates, independent of
// the clipped element's own position/size (contrast the objectBoundingBox
// test above).
func TestClipPathUserSpaceOnUseIsAbsolute(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="c1">
	    <circle cx="50" cy="50" r="20"/>
	  </clipPath>
	  <rect x="0" y="0" width="100" height="100" fill="red" clip-path="url(#c1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	if c := img.RGBAAt(50, 50); c.R < 200 {
		t.Errorf("clip circle center = %+v, want red", c)
	}
	if c := img.RGBAAt(5, 5); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("outside clip circle = %+v, want unpainted white", c)
	}
}

// TestClipPathEmptyHidesElement verifies an empty <clipPath> (no valid
// children) clips its target to NOTHING -- the element must be entirely
// invisible, not rendered as if no clip-path were present at all.
func TestClipPathEmptyHidesElement(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="c1"></clipPath>
	  <rect width="100" height="100" fill="red" clip-path="url(#c1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	if c := img.RGBAAt(50, 50); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("empty clipPath at (50,50) = %+v, want unpainted white (empty clipPath clips to NOTHING, not \"no clip\")", c)
	}
}

// TestClipPathInvalidChildIgnoredAtRender verifies a <g> child inside a
// <clipPath> contributes nothing to the union: a clip whose ONLY child is
// invalid behaves the same as an empty clipPath (hides the target), proving
// the invalid child isn't silently treated as valid geometry.
func TestClipPathInvalidChildIgnoredAtRender(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="c1"><g><rect x="0" y="0" width="100" height="100"/></g></clipPath>
	  <rect width="100" height="100" fill="red" clip-path="url(#c1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	if c := img.RGBAAt(50, 50); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("clipPath with only a <g> child at (50,50) = %+v, want unpainted white (invalid child ignored, clip is effectively empty)", c)
	}
}

// TestClipPathNoneNoClipping verifies clip-path="none" and an unresolvable
// url() both leave the target fully visible.
func TestClipPathNoneNoClipping(t *testing.T) {
	for _, cp := range []string{`none`, `url(#missing)`} {
		src := `<svg ` + clipSVGHdr + ` width="100" height="100"><rect width="100" height="100" fill="red" clip-path="` + cp + `"/></svg>`
		img := renderSVG(t, src, 100, 100)
		if c := img.RGBAAt(50, 50); c.R < 200 {
			t.Errorf("clip-path=%q at (50,50) = %+v, want red (unclipped)", cp, c)
		}
	}
}

// TestClipPathOnGroupClipsAllChildren verifies a clip-path on a <g> applies
// to the whole group's composited content as a unit.
func TestClipPathOnGroupClipsAllChildren(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="c1"><circle cx="50" cy="50" r="20"/></clipPath>
	  <g clip-path="url(#c1)">
	    <rect x="0" y="0" width="50" height="100" fill="red"/>
	    <rect x="50" y="0" width="50" height="100" fill="blue"/>
	  </g>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	if c := img.RGBAAt(45, 50); c.R < 200 {
		t.Errorf("inside clip, red half = %+v, want red", c)
	}
	if c := img.RGBAAt(55, 50); c.B < 200 {
		t.Errorf("inside clip, blue half = %+v, want blue", c)
	}
	if c := img.RGBAAt(5, 5); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("outside clip = %+v, want unpainted white", c)
	}
}

// TestClipPathOnTransformedGroupUsesPostTransformSpace verifies clip-path
// geometry on a <g transform="..."> resolves in the GROUP's OWN user space
// (i.e. after its own transform), not the ambient space before the
// transform was applied -- a regression test for a bug where the group case
// composed the clip mask against the pre-M matrix instead of gm (M.Mul(m)).
func TestClipPathOnTransformedGroupUsesPostTransformSpace(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="c1"><circle cx="10" cy="10" r="8"/></clipPath>
	  <g transform="translate(40,40)" clip-path="url(#c1)">
	    <rect x="0" y="0" width="20" height="20" fill="red"/>
	  </g>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	// The clip circle is defined at LOCAL (10,10) r=8, inside the g's own
	// post-translate space -- so in device space it sits at (50,50). If the
	// clip mask were wrongly built in the PRE-transform space, the circle
	// would clip around device (10,10) instead, and (50,50) would be blank.
	if c := img.RGBAAt(50, 50); c.R < 200 {
		t.Errorf("device (50,50), inside the g's own post-transform clip circle = %+v, want red", c)
	}
	if c := img.RGBAAt(10, 10); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("device (10,10), outside the translated group entirely = %+v, want unpainted white", c)
	}
}

// TestClipPathChildOwnClipPathIntersectsBeforeUnion verifies a clipPath
// child with its own clip-path is restricted to that region before joining
// the union with the other children.
func TestClipPathChildOwnClipPathIntersectsBeforeUnion(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="inner"><circle cx="30" cy="30" r="10"/></clipPath>
	  <clipPath id="outer">
	    <rect x="0" y="0" width="60" height="60" clip-path="url(#inner)"/>
	    <rect x="70" y="70" width="20" height="20"/>
	  </clipPath>
	  <rect width="100" height="100" fill="red" clip-path="url(#outer)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	// Inside the first child's own clip-path circle: visible.
	if c := img.RGBAAt(30, 30); c.R < 200 {
		t.Errorf("inside child's own clip-path region = %+v, want red", c)
	}
	// Inside the first child's rect but OUTSIDE its own clip-path circle:
	// must NOT be visible (the child's own clip-path restricts it before
	// the union, not after).
	if c := img.RGBAAt(5, 5); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("inside child rect but outside child's own clip-path = %+v, want unpainted white", c)
	}
	// The second, unrestricted child still shows through.
	if c := img.RGBAAt(80, 80); c.R < 200 {
		t.Errorf("second (unrestricted) child region = %+v, want red", c)
	}
}

// TestClipPathSelfIntersectsWholeUnion verifies a <clipPath> element's own
// clip-path attribute intersects the WHOLE union of its children.
func TestClipPathSelfIntersectsWholeUnion(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="inner"><circle cx="50" cy="50" r="15"/></clipPath>
	  <clipPath id="outer" clip-path="url(#inner)">
	    <circle cx="20" cy="50" r="15"/>
	    <circle cx="80" cy="50" r="15"/>
	  </clipPath>
	  <rect width="100" height="100" fill="red" clip-path="url(#outer)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	// Neither child circle overlaps the self-clip circle at (50,50) r=15
	// enough to leave anything visible at either child's own center once
	// intersected with a circle at (50,50): both centers are 30px from
	// (50,50), well outside the 15px self-clip radius, so the whole result
	// must be empty.
	if c := img.RGBAAt(20, 50); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("left child center after self-clip intersection = %+v, want unpainted white (outside self clip-path)", c)
	}
	if c := img.RGBAAt(80, 50); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("right child center after self-clip intersection = %+v, want unpainted white (outside self clip-path)", c)
	}
	if c := img.RGBAAt(50, 50); c.R != 255 {
		t.Errorf("self-clip region with no child geometry = %+v, want unpainted (children don't cover this point)", c)
	}
}

// TestClipPathSelfReferenceDoesNotHang is a render-level smoke test that a
// self-referencing clipPath (already proven to terminate at parse time in
// pkg/svg) also renders without hanging or panicking end to end. svg.Parse
// itself is where the cycle guard lives (see pkg/svg's buildingClip), so
// this test bounds the WHOLE render (parse+paint) with a timeout as a
// belt-and-suspenders check that nothing downstream reintroduces unbounded
// recursion over the already-resolved *ClipPath tree.
func TestClipPathSelfReferenceDoesNotHang(t *testing.T) {
	src := `<svg ` + clipSVGHdr + ` width="100" height="100">
	  <clipPath id="c1" clip-path="url(#c1)"><circle cx="50" cy="50" r="20"/></clipPath>
	  <rect width="100" height="100" fill="red" clip-path="url(#c1)"/>
	</svg>`
	done := make(chan *image.RGBA, 1)
	go func() { done <- renderSVG(t, src, 100, 100) }()
	select {
	case img := <-done:
		if c := img.RGBAAt(50, 50); c.R < 200 {
			t.Errorf("self-referencing clipPath center = %+v, want red (self-reference on Self ignored, geometry still applies)", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("render did not terminate on a self-referencing clipPath")
	}
}
