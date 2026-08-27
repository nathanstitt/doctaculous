package draw

import (
	"image"
	"testing"
)

// alphaOfInk reports how far a pixel is from the white background, as a rough
// "how much was painted here" measure that does not depend on the exact hue.
func alphaOfInk(img *image.RGBA, x, y int) int {
	c := img.RGBAAt(x, y)
	d := 0
	for _, v := range []int{255 - int(c.R), 255 - int(c.G), 255 - int(c.B)} {
		if v > d {
			d = v
		}
	}
	return d
}

// TestGaussianBlurSoftensTheEdgeAndKeepsTheCentre proves the primitive end to
// end: the interior stays fully painted while the boundary becomes a gradient
// that bleeds OUTSIDE the original shape.
//
// Checking both halves matters. A blur that only softened would wash out the
// centre; one that only bled would be an offset.
func TestGaussianBlurSoftensTheEdgeAndKeepsTheCentre(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="100" height="100">
	  <filter id="f"><feGaussianBlur stdDeviation="4"/></filter>
	  <rect x="30" y="30" width="40" height="40" fill="rgb(0,0,255)" filter="url(#f)"/>
	</svg>`
	img, logs := renderFilterSVG(t, src, 100, 100)
	if len(logs) != 0 {
		t.Fatalf("feGaussianBlur logged a degradation: %v", logs)
	}

	if got := alphaOfInk(img, 50, 50); got < 200 {
		t.Errorf("centre ink = %d, want the interior still solid", got)
	}
	// Just outside the rect: the blur must have bled there.
	outside := alphaOfInk(img, 26, 50)
	if outside <= 0 {
		t.Errorf("ink at x=26 (outside the 30..70 rect) = %d, want the blur to bleed past the edge", outside)
	}
	if outside >= 200 {
		t.Errorf("ink at x=26 = %d, want a partial (faded) value, not a hard edge", outside)
	}
	// The gradient must be monotonic outward.
	if a, b := alphaOfInk(img, 27, 50), alphaOfInk(img, 24, 50); a <= b {
		t.Errorf("ink at x=27 (%d) not greater than at x=24 (%d); the falloff is not monotonic", a, b)
	}
}

// TestGaussianBlurNegativeStdDeviationRendersUnblurred pins the corpus's
// negative-stdDeviation fixture: an invalid value disables the PRIMITIVE, and
// the element still renders — it does not disappear, and it is not blurred.
func TestGaussianBlurNegativeStdDeviationRendersUnblurred(t *testing.T) {
	for _, bad := range []string{`stdDeviation="-50"`, `stdDeviation=""`, `stdDeviation="5 10 15 20"`, ``} {
		src := `<svg ` + filterHdr + ` width="100" height="100">
		  <filter id="f"><feGaussianBlur ` + bad + `/></filter>
		  <rect x="30" y="30" width="40" height="40" fill="rgb(0,0,255)" filter="url(#f)"/>
		</svg>`
		img, _ := renderFilterSVG(t, src, 100, 100)
		if got := alphaOfInk(img, 50, 50); got < 200 {
			t.Errorf("%s: centre ink = %d, want the element still painted", bad, got)
		}
		// A HARD edge: nothing outside the rect, everything inside it.
		if got := alphaOfInk(img, 26, 50); got != 0 {
			t.Errorf("%s: ink outside the rect = %d, want 0 (unblurred)", bad, got)
		}
		if got := alphaOfInk(img, 31, 50); got < 200 {
			t.Errorf("%s: ink just inside the rect = %d, want a hard edge", bad, got)
		}
	}
}

// TestDropShadowChainEndToEnd renders the canonical real-world graph by hand —
// feOffset, feGaussianBlur, feFlood, feComposite, feMerge — and checks the
// three things that make it a drop shadow: the source is on top and
// unmodified, a shadow appears offset from it, and the shadow is soft.
//
// Writing the chain out (rather than using <feDropShadow>) is the point: this
// is what proves the five primitives compose, and it is the graph
// expandDropShadow lowers to.
func TestDropShadowChainEndToEnd(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="120" height="120">
	  <filter id="f" x="-50%" y="-50%" width="200%" height="200%">
	    <feOffset in="SourceAlpha" dx="10" dy="10" result="off"/>
	    <feGaussianBlur in="off" stdDeviation="3" result="blur"/>
	    <feFlood flood-color="rgb(255,0,0)" result="flood"/>
	    <feComposite in="flood" in2="blur" operator="in" result="shadow"/>
	    <feMerge>
	      <feMergeNode in="shadow"/>
	      <feMergeNode in="SourceGraphic"/>
	    </feMerge>
	  </filter>
	  <rect x="30" y="30" width="40" height="40" fill="rgb(0,0,255)" filter="url(#f)"/>
	</svg>`
	img, logs := renderFilterSVG(t, src, 120, 120)
	if len(logs) != 0 {
		t.Fatalf("the drop-shadow chain logged a degradation: %v", logs)
	}

	// 1. The SOURCE is on top and unmodified: the rect's centre is pure blue,
	//    not tinted by the red shadow underneath.
	c := img.RGBAAt(50, 50)
	if c.B < 200 || c.R > 60 {
		t.Errorf("rect centre = %v, want the unmodified blue source on top", c)
	}

	// 2. A shadow exists, offset by (10,10) and therefore visible past the
	//    rect's bottom-right corner where the source is not.
	sc := img.RGBAAt(75, 75)
	if sc.R < 100 {
		t.Errorf("at (75,75) = %v, want the red shadow (offset +10,+10 past the rect's 70 edge)", sc)
	}
	// 3. The shadow is SOFT: its own edge fades rather than cutting.
	near, far := alphaOfInk(img, 76, 75), alphaOfInk(img, 84, 75)
	if near <= far {
		t.Errorf("shadow ink at x=76 (%d) not greater than at x=84 (%d); the shadow is not blurred", near, far)
	}
	// 4. Nothing above-left of the rect, since the shadow moved away from it.
	if got := alphaOfInk(img, 20, 20); got != 0 {
		t.Errorf("ink above-left = %d, want 0 — the shadow was offset the wrong way", got)
	}
}

// TestFeDropShadowMatchesTheHandWrittenChain pins that the SHORTHAND and the
// expanded graph produce the same pixels.
//
// This is the assertion that makes expandDropShadow's design pay off: the
// shorthand is not allowed to become a second implementation that drifts from
// the chain it claims to be equivalent to.
func TestFeDropShadowMatchesTheHandWrittenChain(t *testing.T) {
	const region = `x="-50%" y="-50%" width="200%" height="200%"`
	shorthand := `<svg ` + filterHdr + ` width="120" height="120">
	  <filter id="f" ` + region + `>
	    <feDropShadow dx="10" dy="10" stdDeviation="3" flood-color="rgb(255,0,0)"/>
	  </filter>
	  <rect x="30" y="30" width="40" height="40" fill="rgb(0,0,255)" filter="url(#f)"/>
	</svg>`
	expanded := `<svg ` + filterHdr + ` width="120" height="120">
	  <filter id="f" ` + region + `>
	    <feGaussianBlur in="SourceAlpha" stdDeviation="3" result="blur"/>
	    <feOffset in="blur" dx="10" dy="10" result="off"/>
	    <feFlood flood-color="rgb(255,0,0)" result="flood"/>
	    <feComposite in="flood" in2="off" operator="in" result="shadow"/>
	    <feMerge>
	      <feMergeNode in="shadow"/>
	      <feMergeNode in="SourceGraphic"/>
	    </feMerge>
	  </filter>
	  <rect x="30" y="30" width="40" height="40" fill="rgb(0,0,255)" filter="url(#f)"/>
	</svg>`

	a, _ := renderFilterSVG(t, shorthand, 120, 120)
	b, _ := renderFilterSVG(t, expanded, 120, 120)
	diff := 0
	for y := 0; y < 120; y++ {
		for x := 0; x < 120; x++ {
			ca, cb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			if ca != cb {
				diff++
			}
		}
	}
	if diff != 0 {
		t.Errorf("<feDropShadow> and the equivalent hand-written chain differ in %d pixels; the shorthand must lower to that chain, not reimplement it", diff)
	}
}

// TestFeMergeCompositesInDocumentOrder pins that the LAST feMergeNode paints on
// top. Reversing the order puts a drop shadow over the element it belongs to.
func TestFeMergeCompositesInDocumentOrder(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="60" height="60">
	  <filter id="f">
	    <feFlood flood-color="rgb(255,0,0)" result="red"/>
	    <feFlood flood-color="rgb(0,0,255)" result="blue"/>
	    <feMerge>
	      <feMergeNode in="red"/>
	      <feMergeNode in="blue"/>
	    </feMerge>
	  </filter>
	  <rect x="10" y="10" width="40" height="40" fill="black" filter="url(#f)"/>
	</svg>`
	img, _ := renderFilterSVG(t, src, 60, 60)
	c := img.RGBAAt(30, 30)
	if c.B < 200 || c.R > 60 {
		t.Errorf("centre = %v, want BLUE — the last feMergeNode paints on top", c)
	}
}

// TestFeCompositeArithmeticSaturates drives the corpus's
// operator=arithmetic-and-invalid-k1-4 case end to end: k4="100" must produce
// opaque WHITE over the region rather than being rejected as out of range.
func TestFeCompositeArithmeticSaturates(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="60" height="60">
	  <filter id="f">
	    <feFlood flood-color="rgb(0,0,255)"/>
	    <feComposite operator="arithmetic" in2="SourceGraphic" k1="-10" k2="-0.2" k3="2.3" k4="100"/>
	  </filter>
	  <rect x="10" y="10" width="40" height="40" fill="rgb(0,128,0)" filter="url(#f)"/>
	</svg>`
	img, _ := renderFilterSVG(t, src, 60, 60)
	c := img.RGBAAt(30, 30)
	if c.R != 255 || c.G != 255 || c.B != 255 || c.A != 255 {
		t.Errorf("centre = %v, want opaque white (every channel saturated by k4=100)", c)
	}
}

// TestFeColorMatrixSaturateDesaturates checks the primitive reaches the
// renderer at all and moves colour toward grey, which no parse-level test can
// confirm.
func TestFeColorMatrixSaturateDesaturates(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="60" height="60">
	  <filter id="f"><feColorMatrix type="saturate" values="0"/></filter>
	  <rect x="10" y="10" width="40" height="40" fill="rgb(255,0,0)" filter="url(#f)"/>
	</svg>`
	img, logs := renderFilterSVG(t, src, 60, 60)
	if len(logs) != 0 {
		t.Fatalf("feColorMatrix logged a degradation: %v", logs)
	}
	c := img.RGBAAt(30, 30)
	if c.R == c.G && c.G == c.B {
		return // fully grey, as saturate(0) should give
	}
	// Allow a little slack for the linearRGB round trip, but the channels must
	// have converged: pure red must not survive.
	spread := int(c.R) - int(c.B)
	if spread > 12 {
		t.Errorf("centre = %v (R-B spread %d), want a desaturated grey", c, spread)
	}
}

// TestFeBlendMultiplyDarkens pins that feBlend reaches the shared blend table
// rather than silently degrading to source-over — the failure a
// "looks-blended" assertion would miss.
func TestFeBlendMultiplyDarkens(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="60" height="60">
	  <filter id="f">
	    <feFlood flood-color="rgb(128,128,255)"/>
	    <feBlend mode="multiply" in2="SourceGraphic"/>
	  </filter>
	  <rect x="10" y="10" width="40" height="40" fill="rgb(0,200,0)" filter="url(#f)"/>
	</svg>`
	blended, _ := renderFilterSVG(t, src, 60, 60)

	normal := `<svg ` + filterHdr + ` width="60" height="60">
	  <filter id="f">
	    <feFlood flood-color="rgb(128,128,255)"/>
	    <feBlend mode="normal" in2="SourceGraphic"/>
	  </filter>
	  <rect x="10" y="10" width="40" height="40" fill="rgb(0,200,0)" filter="url(#f)"/>
	</svg>`
	plain, _ := renderFilterSVG(t, normal, 60, 60)

	bc, pc := blended.RGBAAt(30, 30), plain.RGBAAt(30, 30)
	if bc == pc {
		t.Fatalf("multiply and normal produced identical pixels (%v); the mode degraded to source-over", bc)
	}
	// Multiply can only darken.
	if int(bc.R)+int(bc.G)+int(bc.B) >= int(pc.R)+int(pc.G)+int(pc.B) {
		t.Errorf("multiply = %v, normal = %v; multiply must darken", bc, pc)
	}
}

// TestFilterAppliesBeforeClipPath pins SVG's filter → clip-path → mask →
// opacity order.
//
// A blur must be allowed to spread past the clip boundary and then be CUT OFF
// hard by it. Clipping the filter's INPUT instead removes the content the blur
// would spread from, so the shape's edge fades out INSIDE the clip rather than
// meeting it — which reads as a too-soft blur rather than as a mis-ordered
// clip, and is why the assertion is on the hardness of the boundary.
func TestFilterAppliesBeforeClipPath(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="100" height="100">
	  <filter id="f"><feGaussianBlur stdDeviation="4"/></filter>
	  <clipPath id="c"><rect x="20" y="20" width="30" height="60"/></clipPath>
	  <rect x="20" y="20" width="60" height="60" fill="rgb(0,0,255)"
	        clip-path="url(#c)" filter="url(#f)"/>
	</svg>`
	img, _ := renderFilterSVG(t, src, 100, 100)

	// The clip's right edge is x=50. Inside it, right up against the edge,
	// the blur must still be at full strength (it spread there from content
	// the clip does NOT remove, at x>50).
	if got := alphaOfInk(img, 48, 50); got < 180 {
		t.Errorf("ink just inside the clip edge = %d; the clip was applied to the filter's INPUT, so the blur had nothing to spread from", got)
	}
	// Outside it, nothing at all: the clip cuts hard.
	if got := alphaOfInk(img, 52, 50); got != 0 {
		t.Errorf("ink outside the clip edge = %d, want 0 — the clip must cut the FILTERED result hard", got)
	}
}
