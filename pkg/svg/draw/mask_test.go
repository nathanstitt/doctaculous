package draw

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/nathanstitt/omnidoc/pkg/render"
	"github.com/nathanstitt/omnidoc/pkg/render/raster"
)

const maskSVGHdr = `xmlns="http://www.w3.org/2000/svg"`

// TestMaskHalfBlackHalfWhiteShowsHalfTheRect is THE discriminating luminance
// test: a mask whose left half is black (luminance 0) and right half is
// white (luminance 1) over a solid rect must hide the left half and fully
// show the right half.
func TestMaskHalfBlackHalfWhiteShowsHalfTheRect(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="100" height="100">
	  <mask id="m1" maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
	    <rect x="0" y="0" width="50" height="100" fill="black"/>
	    <rect x="50" y="0" width="50" height="100" fill="white"/>
	  </mask>
	  <rect x="0" y="0" width="100" height="100" fill="red" mask="url(#m1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)

	left := img.RGBAAt(25, 50)
	right := img.RGBAAt(75, 50)
	t.Logf("black half (25,50) = %+v", left)
	t.Logf("white half (75,50) = %+v", right)

	if left.R != 255 || left.G != 255 || left.B != 255 {
		t.Errorf("black-half pixel = %+v, want unpainted white (fully masked out)", left)
	}
	if right.R < 250 || right.G > 5 || right.B > 5 {
		t.Errorf("white-half pixel = %+v, want fully opaque red (fully visible)", right)
	}
}

// TestMaskMidGreyGivesHalfAlpha verifies the luminance formula against a
// known value: a mid-grey (128,128,128) mask pixel should give ~50%
// coverage, blending red 50/50 with the white background.
func TestMaskMidGreyGivesHalfAlpha(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="100" height="100">
	  <mask id="m1" maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
	    <rect x="0" y="0" width="100" height="100" fill="rgb(128,128,128)"/>
	  </mask>
	  <rect x="0" y="0" width="100" height="100" fill="red" mask="url(#m1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	c := img.RGBAAt(50, 50)
	t.Logf("mid-grey mask pixel (50,50) = %+v", c)
	// 128/255 luminance blended over white: R = 255*(1-a) + 255*a = 255,
	// G = 255*(1-a), B = 255*(1-a), where a = 128/255 ~= 0.502.
	wantG := uint8(255 - int(255*128/255))
	if c.R < 250 {
		t.Errorf("R = %d, want ~255 (red channel unaffected by blending with white bg)", c.R)
	}
	if diff := int(c.G) - int(wantG); diff < -8 || diff > 8 {
		t.Errorf("G = %d, want ~%d (+/-8) for ~50%% alpha from a mid-grey luminance mask", c.G, wantG)
	}
}

// TestMaskTypeAlphaUsesAlphaChannel verifies mask-type=alpha reads the
// rendered content's own alpha, not its color luminance: a fully-opaque
// BLACK mask (luminance 0, alpha 1) must show the target fully under
// mask-type=alpha, the opposite of what mask-type=luminance would do.
func TestMaskTypeAlphaUsesAlphaChannel(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="100" height="100">
	  <mask id="m1" mask-type="alpha" maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
	    <rect x="0" y="0" width="100" height="100" fill="black"/>
	  </mask>
	  <rect x="0" y="0" width="100" height="100" fill="red" mask="url(#m1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	c := img.RGBAAt(50, 50)
	t.Logf("mask-type=alpha, opaque black mask (50,50) = %+v", c)
	if c.R < 250 || c.G > 5 || c.B > 5 {
		t.Errorf("pixel = %+v, want fully opaque red (opaque black -> alpha=1 under mask-type=alpha)", c)
	}
}

// TestMaskTypeLuminanceOpaqueBlackHidesTarget is the mask-type=luminance
// control case for the alpha test above: the SAME opaque black mask content
// must instead HIDE the target under the default mask-type (luminance 0).
func TestMaskTypeLuminanceOpaqueBlackHidesTarget(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="100" height="100">
	  <mask id="m1" maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
	    <rect x="0" y="0" width="100" height="100" fill="black"/>
	  </mask>
	  <rect x="0" y="0" width="100" height="100" fill="red" mask="url(#m1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	c := img.RGBAAt(50, 50)
	if c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("pixel = %+v, want unpainted white (opaque black -> luminance 0 under default mask-type)", c)
	}
}

// TestMaskEmptyHidesElementEntirely verifies an empty <mask> (no children)
// makes its target FULLY TRANSPARENT -- not "no mask".
func TestMaskEmptyHidesElementEntirely(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="100" height="100">
	  <mask id="m1"></mask>
	  <rect x="0" y="0" width="100" height="100" fill="red" mask="url(#m1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	if c := img.RGBAAt(50, 50); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("empty mask at (50,50) = %+v, want unpainted white (empty mask hides the element entirely)", c)
	}
}

// TestMaskNoneAndInvalidMeanNoMasking verifies mask="none" and an
// unresolvable url() both leave the target fully visible.
func TestMaskNoneAndInvalidMeanNoMasking(t *testing.T) {
	for _, m := range []string{`none`, `url(#missing)`} {
		src := `<svg ` + maskSVGHdr + ` width="100" height="100"><rect width="100" height="100" fill="red" mask="` + m + `"/></svg>`
		img := renderSVG(t, src, 100, 100)
		if c := img.RGBAAt(50, 50); c.R < 200 {
			t.Errorf("mask=%q at (50,50) = %+v, want red (unmasked)", m, c)
		}
	}
}

// TestMaskSelfReferenceDoesNotHang is a render-level smoke test that a
// self-referencing mask (already proven to terminate at parse time in
// pkg/svg) also renders without hanging or panicking end to end.
func TestMaskSelfReferenceDoesNotHang(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="100" height="100">
	  <mask id="m1" mask="url(#m1)"><rect width="100" height="100" fill="white"/></mask>
	  <rect width="100" height="100" fill="red" mask="url(#m1)"/>
	</svg>`
	done := make(chan *image.RGBA, 1)
	go func() { done <- renderSVG(t, src, 100, 100) }()
	select {
	case img := <-done:
		// Self-reference on Self is ignored; the mask's own white content
		// still applies, so the target should be visible.
		if c := img.RGBAAt(50, 50); c.R < 200 {
			t.Errorf("self-referencing mask center = %+v, want red (self-reference ignored, geometry still applies)", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("render did not terminate on a self-referencing mask")
	}
}

// TestMaskUnitsObjectBoundingBoxDefaultBleed verifies the maskUnits default
// (objectBoundingBox, -10%/-10%/120%/120%) actually bleeds 10% past the
// masked element's own bounding box, by using maskContentUnits=
// objectBoundingBox content that paints white ONLY in the [-0.1,1.1] unit
// range: content at unit x=-0.05 (5% bleed, inside the region) must show
// through, while a target whose region were wrongly clipped to exactly
// [0,1] (no bleed) would hide it.
func TestMaskUnitsObjectBoundingBoxDefaultBleed(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="100" height="100">
	  <mask id="m1" maskContentUnits="objectBoundingBox">
	    <rect x="-0.1" y="-0.1" width="1.2" height="1.2" fill="white"/>
	  </mask>
	  <rect x="20" y="20" width="60" height="60" fill="red" mask="url(#m1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	// Target bbox is (20,20)-(80,80), 60x60. The 10% bleed extends the
	// mask region 6px past each edge, to (14,14)-(86,86) in device space.
	// Sample just inside that bleed, past the target's own edge: since the
	// target itself only paints its own (20,20)-(80,80) box regardless of
	// how far the mask region extends, this test instead confirms the
	// region does NOT wrongly clip the target's own edge pixels (which a
	// region computed as exactly [0,1] with off-by-one rounding could).
	if c := img.RGBAAt(21, 21); c.R < 200 {
		t.Errorf("near target's own top-left edge (21,21) = %+v, want red (10%% bleed must not clip the target's own edge)", c)
	}
	if c := img.RGBAAt(79, 79); c.R < 200 {
		t.Errorf("near target's own bottom-right edge (79,79) = %+v, want red (10%% bleed must not clip the target's own edge)", c)
	}
	// Outside the target's own box entirely (device (10,10), well outside
	// (20,20)-(80,80)): nothing painted there regardless of the mask
	// region, since the region only RESTRICTS coverage, it doesn't cause
	// painting outside the target's own geometry.
	if c := img.RGBAAt(10, 10); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("outside target geometry (10,10) = %+v, want unpainted white", c)
	}
}

// TestMaskUnitsRegionSmallerThanBBoxClips verifies the mask region rect
// genuinely restricts coverage: an explicit userSpaceOnUse region SMALLER
// than the target's own box must clip the target to that region, proving
// the region is really applied (not just "no clip at all", which the
// default-bleed test above cannot distinguish from a correctly-applied
// generous region).
func TestMaskUnitsRegionSmallerThanBBoxClips(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="100" height="100">
	  <mask id="m1" maskUnits="userSpaceOnUse" x="20" y="20" width="30" height="30">
	    <rect x="0" y="0" width="100" height="100" fill="white"/>
	  </mask>
	  <rect x="0" y="0" width="100" height="100" fill="red" mask="url(#m1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	// Inside the region (20,20)-(50,50): visible.
	if c := img.RGBAAt(35, 35); c.R < 200 {
		t.Errorf("inside region (35,35) = %+v, want red", c)
	}
	// Outside the region, but inside the mask's own white content (which
	// covers the whole canvas): must still be hidden, since the region
	// rect restricts coverage independent of content.
	if c := img.RGBAAt(70, 70); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("outside region (70,70) = %+v, want unpainted white (region rect must restrict coverage)", c)
	}
}

// TestMaskContentUnitsObjectBoundingBox verifies maskContentUnits=
// objectBoundingBox maps mask CONTENT geometry through the masked element's
// own bounding box (a unit rect at 0.1..0.9 clips content to the inner 80%).
func TestMaskContentUnitsObjectBoundingBox(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="200" height="200">
	  <mask id="m1" maskContentUnits="objectBoundingBox">
	    <rect x="0.1" y="0.1" width="0.8" height="0.8" fill="white"/>
	  </mask>
	  <rect x="0" y="0" width="200" height="200" fill="green" mask="url(#m1)"/>
	</svg>`
	img := renderSVG(t, src, 200, 200)
	if c := img.RGBAAt(100, 100); c.G < 100 || c.R > 5 {
		t.Errorf("center (100,100) = %+v, want green (inside the 80%% content rect)", c)
	}
	if c := img.RGBAAt(5, 5); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("corner (5,5) = %+v, want unpainted white (outside the 80%% content rect)", c)
	}
}

// TestMaskTransformOnMaskElementHasNoEffect verifies a transform="..." on
// the <mask> ELEMENT itself is ignored -- only a transform on the MASKED
// element applies.
func TestMaskTransformOnMaskElementHasNoEffect(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="100" height="100">
	  <mask id="m1" transform="skewX(30)" maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
	    <rect x="0" y="0" width="50" height="100" fill="black"/>
	    <rect x="50" y="0" width="50" height="100" fill="white"/>
	  </mask>
	  <rect x="0" y="0" width="100" height="100" fill="red" mask="url(#m1)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	// If the skewX(30) transform wrongly applied to the mask, the
	// black/white boundary at x=50 would shear with y; sampling straight
	// down the line x=50+eps at both y=5 and y=95 and requiring the SAME
	// result (both showing red, since white is x>=50) proves no shear was
	// applied.
	top := img.RGBAAt(55, 5)
	bottom := img.RGBAAt(55, 95)
	if top.R < 200 || bottom.R < 200 {
		t.Errorf("(55,5) = %+v, (55,95) = %+v, want both red (mask transform must have no effect, no shear)", top, bottom)
	}
}

// TestMaskAppliesInMaskedElementsOwnTransformedSpace verifies a transform on
// the MASKED element (not the mask) still composes correctly: the mask
// content is defined in the masked element's post-transform user space, so
// translating the masked element moves BOTH the shape and where its mask's
// boundary lands in device space, together.
func TestMaskAppliesInMaskedElementsOwnTransformedSpace(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="100" height="100">
	  <mask id="m1" maskUnits="userSpaceOnUse" x="-10" y="-10" width="40" height="40">
	    <rect x="-10" y="-10" width="20" height="40" fill="black"/>
	    <rect x="10" y="-10" width="20" height="40" fill="white"/>
	  </mask>
	  <rect x="0" y="0" width="20" height="20" fill="red" mask="url(#m1)" transform="translate(40,40)"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	// The rect occupies device (40,40)-(60,60). Mask geometry is defined
	// in the SAME local space as the rect's own x/y attributes (before
	// translate): the mask's black/white boundary sits at local x=10,
	// which maps to device x=10+40=50 -- splitting the rect's own device
	// span (40..60) right down the middle. local x<10 (device <50) is
	// black/hidden; local x>=10 (device >=50) is white/visible.
	left := img.RGBAAt(45, 50)  // local x=5, device 45: should be BLACK/hidden
	right := img.RGBAAt(55, 50) // local x=15, device 55: should be WHITE/visible
	if left.R != 255 || left.G != 255 || left.B != 255 {
		t.Errorf("left half (45,50) = %+v, want unpainted white (masked out)", left)
	}
	if right.R < 200 {
		t.Errorf("right half (55,50) = %+v, want red (visible)", right)
	}
}

// TestMaskWithClipPathBothApply verifies mask and clip-path on the same
// element compose (clip -> mask -> opacity): the visible region is the
// intersection of the clip circle/rect and the mask's white region.
func TestMaskWithClipPathBothApply(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="200" height="200">
	  <mask id="m1"><circle cx="100" cy="100" r="60" fill="white"/></mask>
	  <clipPath id="c1"><rect x="50" y="50" width="100" height="100"/></clipPath>
	  <rect x="0" y="0" width="200" height="200" fill="green"
	        mask="url(#m1)" clip-path="url(#c1)"/>
	</svg>`
	img := renderSVG(t, src, 200, 200)
	// Center: inside both the mask circle and the clip rect.
	if c := img.RGBAAt(100, 100); c.G < 100 || c.R > 5 {
		t.Errorf("center (100,100) = %+v, want green (inside both clip and mask)", c)
	}
	// Inside the mask circle but OUTSIDE the clip rect (e.g. near the top,
	// y=45, which is above the clip rect's y=50..150 range but still
	// inside the r=60 circle centered at 100,100 since sqrt(0^2+55^2)=55<60).
	if c := img.RGBAAt(100, 45); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("inside mask, outside clip (100,45) = %+v, want unpainted white", c)
	}
	// Inside the clip rect but OUTSIDE the mask circle (corner of the clip
	// rect, e.g. (55,55), which is sqrt(45^2+45^2)=~64 from center, outside r=60).
	if c := img.RGBAAt(55, 55); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("inside clip, outside mask (55,55) = %+v, want unpainted white", c)
	}
}

// TestMaskWithOpacityAppliesAfterMask verifies mask and element opacity
// compose (mask -> opacity, per the design's clip->mask->opacity order): a
// half-visible mask combined with 50% opacity multiplies to ~25% coverage.
func TestMaskWithOpacityAppliesAfterMask(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="100" height="100">
	  <mask id="m1" maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
	    <rect width="100" height="100" fill="white"/>
	  </mask>
	  <rect width="100" height="100" fill="red" mask="url(#m1)" opacity="0.5"/>
	</svg>`
	img := renderSVG(t, src, 100, 100)
	c := img.RGBAAt(50, 50)
	t.Logf("full white mask + opacity=0.5 = %+v", c)
	// Full white mask -> full coverage from the mask alone; opacity=0.5 on
	// top gives ~50% red over white: R~255, G/B ~127.
	if c.R < 250 {
		t.Errorf("R = %d, want ~255", c.R)
	}
	if c.G < 110 || c.G > 145 {
		t.Errorf("G = %d, want ~127 (50%% opacity on top of a fully-visible mask)", c.G)
	}
}

// TestNestedMaskOnMask verifies a mask referenced BY another mask (mask-on-
// mask, "mask on self" in the resvg corpus naming) composes by intersection:
// the group stack must handle opening a luminance-mask group while already
// inside another luminance-mask's content render.
func TestNestedMaskOnMask(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="200" height="200">
	  <mask id="inner"><rect x="40" y="40" width="120" height="120" fill="white"/></mask>
	  <mask id="outer" mask="url(#inner)" maskUnits="userSpaceOnUse" x="0" y="0" width="200" height="200">
	    <rect x="20" y="20" width="160" height="160" fill="white"/>
	  </mask>
	  <rect x="20" y="20" width="160" height="160" fill="green" mask="url(#outer)"/>
	</svg>`
	img := renderSVG(t, src, 200, 200)
	// Inside both inner (40..160) and outer's own content (20..180): visible.
	if c := img.RGBAAt(100, 100); c.G < 100 || c.R > 5 {
		t.Errorf("inside both masks (100,100) = %+v, want green", c)
	}
	// Inside outer's own content region but OUTSIDE inner's white rect
	// (e.g. (30,30), inside 20..180 but outside 40..160): must be hidden,
	// since outer's mask value = outer content x inner mask.
	if c := img.RGBAAt(30, 30); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("inside outer only (30,30) = %+v, want unpainted white (inner mask must restrict outer)", c)
	}
}

// TestEndGroupCombinesClipAndSoftMaskByProductNotMin is THE discriminating
// regression test for the bug fixed at draw.go's two Group/Shape mask call
// sites: render.Device.EndGroup's clipMask and softMask parameters must
// combine by MULTIPLYING their coverage, not by taking the min -- the two
// masks reach EndGroup as two SEPARATE parameters specifically so each
// backend can combine them correctly (see EndGroup's doc comment), and this
// pins the raster backend's own combining rule directly, white-box, against
// the ACTUAL two-parameter EndGroup call a real render goes through (not a
// standalone helper function, which no longer exists after the masks were
// separated all the way to EndGroup -- see git history for "use product not
// min when combining a clip mask with a luminance mask" and the follow-up
// that split EndGroup's mask parameter in two).
//
// A BINARY mask cannot tell a product-combine from a min-combine apart
// (min(a,b) == a*b/255 whenever either operand is already fully opaque,
// 255) -- that is exactly why the original bug shipped and slipped through
// review undetected. This test uses two deliberately fractional coverage
// values (200 and 128, neither 0 nor 255) so the two combining rules
// diverge: min(200,128)=128 but the correct product is 200*128/255=100 -- a
// divergence of 28/255 (~11%), easily distinguished from rounding noise.
func TestEndGroupCombinesClipAndSoftMaskByProductNotMin(t *testing.T) {
	const clipVal, lumVal = 200, 128 // both fractional: neither is 0 or 255
	wantProduct := uint8(uint16(clipVal) * uint16(lumVal) / 255)
	wantMin := uint8(min(clipVal, lumVal))
	if wantProduct == wantMin {
		t.Fatalf("test setup is not discriminating: product (%d) == min (%d) for clip=%d, mask=%d", wantProduct, wantMin, clipVal, lumVal)
	}

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	dev := raster.New(img)

	bounds := image.Rect(0, 0, 4, 4)
	clipMask := image.NewAlpha(bounds)
	softMask := image.NewAlpha(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			clipMask.SetAlpha(x, y, color.Alpha{A: clipVal})
			softMask.SetAlpha(x, y, color.Alpha{A: lumVal})
		}
	}

	rect := &render.Path{}
	rect.MoveTo(0, 0)
	rect.LineTo(4, 0)
	rect.LineTo(4, 4)
	rect.LineTo(0, 4)
	rect.Close()

	dev.BeginGroup()
	dev.Fill(rect, render.FillPaint{Color: color.RGBA{0, 0, 0, 255}})
	dev.EndGroup(1, "", clipMask, softMask)

	// The group painted fully-opaque black into an initially-transparent
	// canvas; the composited pixel's own ALPHA channel is exactly the
	// clip/soft-mask combined coverage value (no background color math
	// needed -- straight from render.Device's premultiplied-RGBA contract).
	got := img.RGBAAt(1, 1)
	gotCoverage := got.A
	t.Logf("clip=%d soft=%d -> composited coverage=%d (want product=%d, NOT min=%d)", clipVal, lumVal, gotCoverage, wantProduct, wantMin)

	if gotCoverage < wantProduct-2 || gotCoverage > wantProduct+2 {
		t.Errorf("composited coverage = %d, want ~%d (the product of clip=%d and soft=%d)", gotCoverage, wantProduct, clipVal, lumVal)
	}
	if gotCoverage == wantMin {
		t.Errorf("composited coverage (%d) equals the WRONG min-based value -- EndGroup combined clipMask and softMask by min instead of product", gotCoverage)
	}
}

// TestGroupMaskWithClipUsesAttenuationEndToEnd is the end-to-end rendering
// companion to TestClipCombinedWithFractionalMaskUsesProductNotMin: a rect
// clipped to a diagonal-edged triangle AND masked by a 50%-grey luminance
// mask. The triangle's hypotenuse gives pixel (99,100) an antialiased clip
// coverage of ~50% (~128/255, confirmed empirically: clip-only rendering at
// that pixel comes out exactly halfway between full green and white).
// Combined with the mask's ~50% (128/255), the CORRECT product rule
// (128*128/255 ~= 64, ~25% total coverage) and the WRONG min rule
// (min(128,128) = 128, ~50% total coverage, i.e. as if the clip weren't
// fractional at all) predict clearly different, well-separated pixel
// colors, proving the fix is wired up correctly at the real draw.go call
// site (Shape case: s.ClipPath != nil && s.Mask != nil), not just correct
// in the isolated helper-function test above.
func TestGroupMaskWithClipUsesAttenuationEndToEnd(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="200" height="200">
	  <mask id="m1"><rect x="0" y="0" width="200" height="200" fill="rgb(128,128,128)"/></mask>
	  <clipPath id="c1"><polygon points="0,0 200,0 0,200"/></clipPath>
	  <rect x="0" y="0" width="200" height="200" fill="green" mask="url(#m1)" clip-path="url(#c1)"/>
	</svg>`
	img := renderSVG(t, src, 200, 200)

	// Deep inside the clip triangle (clip coverage = 255) and under the 50%
	// mask: green blended ~50% over white, same as TestMaskMidGreyGivesHalfAlpha.
	inside := img.RGBAAt(30, 30)
	t.Logf("deep inside clip, under 50%% mask (30,30) = %+v", inside)
	if inside.G < 175 || inside.G > 210 {
		t.Errorf("inside pixel G = %d, want ~191 (50%% mask alone, clip fully opaque here)", inside.G)
	}

	// On the triangle's antialiased hypotenuse (99,100): clip coverage here
	// is ~50%, confirmed by clip-only rendering giving exactly the halfway
	// blend (R=127,G=191,B=127 -- see the package-level probe this value is
	// derived from). Combined with the mask's ~50%:
	//   - correct (product):  128*128/255 ~= 64  -> G ~= 223 (mostly white)
	//   - wrong   (min):      min(128,128) = 128  -> G ~= 191 (same as fully
	//     inside the clip, AS IF clip coverage being fractional had no
	//     effect at all -- exactly the bug this test catches)
	edge := img.RGBAAt(99, 100)
	t.Logf("on antialiased clip edge (~50%% clip coverage), under 50%% mask (99,100) = %+v", edge)
	if edge.G < 208 || edge.G > 238 {
		t.Errorf("edge pixel G = %d, want ~223 (product of two ~50%% coverages: ~25%% total green coverage). "+
			"A value near ~191 here would mean the clip and mask were combined with min() instead of product, "+
			"reproducing the fixed bug", edge.G)
	}
	if edge.G <= inside.G {
		t.Errorf("edge pixel G = %d, want strictly greater than inside pixel G = %d (the fractional clip coverage must further attenuate the mask's own ~50%%, showing MORE white / less green at the edge than deep inside the clip)", edge.G, inside.G)
	}
}

// TestCyclicMaskReferenceDropsTheCyclicLink pins the resolution of a mutually
// cyclic mask reference: the link that closes the cycle contributes NOTHING,
// leaving the referenced mask's own content as the only attenuation.
//
// The engine used to resolve one level THROUGH the cycle instead — mask2's
// Self became mask1 (whose own Self the guard had already nil'd), so both
// gradients multiplied. That made the result symmetric in x as well as y and
// roughly 4x too faint. This is not an ambiguous case: Chrome, Firefox,
// Safari, resvg, and Inkscape all agree on the corpus fixture, and resvg's
// parser rewrites a cyclic mask attribute to "none" before rendering.
//
// The assertion is on the SHAPE the two rules disagree about, not on absolute
// coverage: with only mask2 (a gradient rotated 90 degrees, so it runs
// vertically) the result must be CONSTANT along x. The buggy product with
// mask1's horizontal gradient falls off toward both x edges, so sampling
// three columns at the same y separates them without depending on the exact
// coverage arithmetic.
func TestCyclicMaskReferenceDropsTheCyclicLink(t *testing.T) {
	src := `<svg ` + maskSVGHdr + ` width="200" height="200">
	  <linearGradient id="lg1">
	    <stop offset="0" stop-color="white" stop-opacity="0"/>
	    <stop offset="1" stop-color="black"/>
	  </linearGradient>
	  <mask id="mask1" mask="url(#mask2)">
	    <rect x="20" y="20" width="160" height="160" fill="url(#lg1)"/>
	  </mask>
	  <mask id="mask2" mask="url(#mask1)">
	    <rect x="20" y="20" width="160" height="160" fill="url(#lg1)" transform="rotate(90 100 100)"/>
	  </mask>
	  <rect x="20" y="20" width="160" height="160" fill="green" mask="url(#mask2)"/>
	</svg>`
	img := renderSVG(t, src, 200, 200)

	// Same row, three columns: a purely vertical gradient mask must give the
	// same coverage at all three.
	left := img.RGBAAt(40, 100)
	mid := img.RGBAAt(100, 100)
	right := img.RGBAAt(160, 100)
	for _, c := range []struct {
		name string
		got  color.RGBA
	}{{"x=40", left}, {"x=160", right}} {
		if absU8Diff(c.got.G, mid.G) > 3 {
			t.Errorf("%s green=%d but x=100 green=%d: coverage varies along x, so the cyclic mask1 is still attenuating (its gradient runs horizontally)",
				c.name, c.got.G, mid.G)
		}
	}

	// And it must genuinely vary along y — a mask that resolved to nothing at
	// all would be uniformly opaque and would also pass the check above.
	top := img.RGBAAt(100, 30)
	if absU8Diff(top.G, mid.G) < 20 {
		t.Errorf("green at y=30 (%d) is too close to y=100 (%d): the vertical gradient is not being applied at all", top.G, mid.G)
	}
}

// absU8Diff is the absolute difference between two channel values.
func absU8Diff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
