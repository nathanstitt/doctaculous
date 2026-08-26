package raster

import (
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// TestGroupOpacityNoSeamAtOverlap is the discriminating test for this
// primitive: two overlapping opaque shapes composited as a single group at
// 50% opacity must show the SAME color at the overlap as at the
// non-overlapping parts of each shape. Per-paint alpha (the wrong way to
// implement group opacity) double-darkens the overlap, because each shape's
// 50%-alpha paint blends independently onto the backdrop and then onto each
// other. A group composites the flattened, fully-opaque-inside-the-group
// result exactly once, so the overlap is not special.
func TestGroupOpacityNoSeamAtOverlap(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}

	dev.BeginGroup()
	// Two opaque, overlapping rectangles inside the group.
	dev.Fill(rectPath(10, 10, 50, 50), render.FillPaint{Color: red})
	dev.Fill(rectPath(30, 30, 70, 70), render.FillPaint{Color: blue})
	dev.EndGroup(0.5, "", nil)

	// (40,40) is inside both rectangles (the overlap, painted blue-over-red
	// inside the group). (60,60) is inside only the blue rectangle
	// (non-overlap). Both were fully opaque *inside* the group, so both must
	// composite onto the white backdrop identically: the group's uniform 50%
	// alpha is the only thing that touches either pixel's coverage.
	overlap := img.RGBAAt(40, 40)
	nonOverlap := img.RGBAAt(60, 60)

	t.Logf("overlap pixel (40,40) = %+v", overlap)
	t.Logf("non-overlap pixel (60,60) = %+v", nonOverlap)

	if overlap != nonOverlap {
		t.Errorf("seam detected: overlap = %+v, non-overlap = %+v; group opacity must composite once, not per child", overlap, nonOverlap)
	}
	// Both should be ~50% blue over white: (128, 128, 255).
	if nonOverlap.B < 200 || nonOverlap.R < 100 || nonOverlap.R > 160 {
		t.Errorf("50%% blue-in-group over white = %+v, want ~light blue (R≈128, B≈255)", nonOverlap)
	}
	// A pixel entirely outside the group's painted area stays untouched.
	if c := img.RGBAAt(90, 90); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("outside group = %+v, want unpainted white", c)
	}
}

// TestNestedGroupsComposite verifies a group inside a group flattens
// correctly: the inner group's own opacity applies once when it composites
// onto the outer group's scratch, and the outer group's opacity then applies
// once more onto the real surface.
func TestNestedGroupsComposite(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	black := color.RGBA{0, 0, 0, 255}

	dev.BeginGroup() // outer, alpha 0.5
	dev.BeginGroup() // inner, alpha 0.5
	dev.Fill(rectPath(10, 10, 50, 50), render.FillPaint{Color: black})
	dev.EndGroup(0.5, "", nil)
	dev.EndGroup(0.5, "", nil)

	// Combined coverage is 0.5*0.5 = 0.25: black at 25% over white ≈ (191,191,191).
	c := img.RGBAAt(20, 20)
	if c.R < 175 || c.R > 210 {
		t.Errorf("nested group opacity = %+v, want ~ (191,191,191) for 0.25 combined alpha", c)
	}
	if outside := img.RGBAAt(90, 90); outside.R != 255 || outside.G != 255 || outside.B != 255 {
		t.Errorf("outside nested groups = %+v, want unpainted white", outside)
	}
}

// TestGroupMaskRestrictsComposite verifies a non-nil GroupMask restricts the
// group's composite to the mask's coverage: content is dropped outside the
// mask even though it was fully painted inside the group's scratch.
func TestGroupMaskRestrictsComposite(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	red := color.RGBA{255, 0, 0, 255}

	// Mask covers only the left half of the canvas.
	mask := image.NewAlpha(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			if x < 50 {
				mask.SetAlpha(x, y, color.Alpha{A: 255})
			}
		}
	}

	dev.BeginGroup()
	// Paint a rectangle spanning both halves of the mask.
	dev.Fill(rectPath(0, 0, 100, 100), render.FillPaint{Color: red})
	dev.EndGroup(1, "", mask)

	if c := img.RGBAAt(25, 50); c.R < 200 || c.G > 50 {
		t.Errorf("inside mask = %+v, want red", c)
	}
	if c := img.RGBAAt(75, 50); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("outside mask = %+v, want unpainted white (mask should drop it)", c)
	}
}

// TestUnbalancedEndGroupNoPanic verifies an EndGroup with no matching
// BeginGroup degrades to a no-op rather than panicking, matching Restore's
// forgiving behavior on an empty stack.
func TestUnbalancedEndGroupNoPanic(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	dev.EndGroup(0.5, "", nil) // no BeginGroup: must not panic

	// Device must remain usable afterward.
	dev.Fill(rectPath(10, 10, 30, 30), render.FillPaint{Color: color.RGBA{255, 0, 0, 255}})
	if c := img.RGBAAt(20, 20); c.R < 200 {
		t.Errorf("device unusable after unbalanced EndGroup: fill = %+v, want red", c)
	}
}

// TestGroupSaveRestoreCannotCorruptOuterClip verifies that Save/Restore calls
// inside a group cannot pop past the clip depth captured at BeginGroup — an
// extra, unbalanced Restore inside the group must not remove the outer clip
// that was active before the group opened.
func TestGroupSaveRestoreCannotCorruptOuterClip(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	red := color.RGBA{255, 0, 0, 255}

	dev.Save()
	dev.PushClip(rectPath(10, 10, 30, 30), render.NonZero) // outer clip: 20x20 box

	dev.BeginGroup()
	dev.Save()
	dev.PushClip(rectPath(0, 0, 100, 100), render.NonZero) // inner clip (no-op vs outer, still valid)
	dev.Restore()                                          // balanced
	dev.Restore()                                          // one EXTRA restore: must be clamped, not corrupt outer clip
	dev.Restore()                                          // another extra one for good measure
	dev.Fill(rectPath(0, 0, 100, 100), render.FillPaint{Color: red})
	dev.EndGroup(1, "", nil)

	dev.Restore() // pop the outer Save

	// The outer clip (10,10)-(30,30) must still have constrained the group's
	// fill: only inside that box is red, everywhere else stays white.
	if c := img.RGBAAt(20, 20); c.R < 200 {
		t.Errorf("inside outer clip = %+v, want red", c)
	}
	if c := img.RGBAAt(60, 60); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("outside outer clip = %+v, want unpainted white (extra Restores inside group corrupted the outer clip)", c)
	}

	// After the group and the final Restore, the clip stack must be back to
	// empty (no leaked entries from the extra Restores or the group).
	if len(dev.clip) != 0 {
		t.Errorf("clip stack leaked entries: len = %d, want 0", len(dev.clip))
	}
}
