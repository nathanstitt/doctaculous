package css

import (
	"context"
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// TestRelativeChildOfPositionedClipIsClipped: a position:relative child of a
// POSITIONED (position:relative) overflow:hidden box — the box is a stacking context
// that CONSUMES the relative child onto its own Positioned. Because the box IS the
// child's containing block AND clips, the child must paint INSIDE the box's own clip
// bracket (CBOwned), not in the escaped band after ClipPop. Adversarial: the child is
// offset past the clip edge, so an unclipped render would paint outside the box.
//
// This is sub-case B (the positioned-clip-box relative escape): before the fix the box
// set CBOwned=false for every consumed relative descendant, so this child painted in the
// escaped band after ClipPop, unclipped.
func TestRelativeChildOfPositionedClipIsClipped(t *testing.T) {
	eng := New(nil, nil, nil)
	childBG := color.RGBA{88, 0, 0, 255}

	// Relative child, offset down+right past the clip box edge (z:auto, big offset).
	child := posBox(zfill(60, childBG, 40, 40, 0, true), cssbox.PosRelative)

	// A POSITIONED (position:relative) overflow:hidden clip box — BOTH a stacking
	// context (so it consumes the relative child) AND a clipping box (its content,
	// including this in-flow relative child, is clipped to its padding box).
	clipStyle := posStyle()
	clipStyle.Width = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	clipStyle.Height = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	clipStyle.Overflow = "hidden"
	clip := posBox(clipStyle, cssbox.PosRelative, child)
	root := posBox(posStyle(), cssbox.PosStatic, clip)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	push, pop := clipBoundsReal(items)
	ci := bgIndex(items, childBG)
	if push < 0 || pop < 0 {
		t.Fatalf("expected a clip bracket; push=%d pop=%d", push, pop)
	}
	if ci < 0 {
		t.Fatal("child background not painted")
	}
	// INSIDE the bracket: push < ci < pop. De-Morgan'd (golangci-lint QF1001 forbids
	// if !(a && b)).
	if ci <= push || ci >= pop {
		t.Errorf("relative child background at %d must be INSIDE the positioned clip box's bracket (push=%d, pop=%d)", ci, push, pop)
	}
}

// TestRelativeGrandchildOfPositionedClipNotOverClipped: a relative grandchild whose
// nearer positioned ancestor is a relative box BETWEEN it and an outer positioned
// overflow:hidden box must NOT be clipped by the OUTER box — it is consumed by the nearer
// positioned ancestor (a stacking context) and never reaches the outer box's positioned
// layer, so the outer box's clip does not reach it. This is the scoping that keeps "every
// relative descendant a clipping positioned box consumes is clipped by it" correct: a
// relative descendant only reaches the outer box's consume list if NO positioned box sits
// between them (a positioned box, being a stacking context, consumes it first).
func TestRelativeGrandchildOfPositionedClipNotOverClipped(t *testing.T) {
	eng := New(nil, nil, nil)
	innerBG := color.RGBA{0, 0, 99, 255}

	// inner: a relative grandchild with a background, offset.
	inner := posBox(zfill(30, innerBG, 20, 20, 0, true), cssbox.PosRelative)

	// middle: a relative box (a stacking context, NOT clipping) containing inner. It is
	// inner's NEAREST positioned ancestor, so it CONSUMES inner onto its own Positioned.
	midStyle := posStyle()
	midStyle.Height = gcss.Length{Value: 40, Unit: gcss.UnitPx}
	middle := posBox(midStyle, cssbox.PosRelative, inner)

	// outer: position:relative + overflow:hidden clip box containing middle. middle is a
	// direct relative child of outer; inner is a grandchild that never reaches outer's
	// pending list as a direct child (middle consumes it first).
	outerStyle := posStyle()
	outerStyle.Width = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	outerStyle.Height = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	outerStyle.Overflow = "hidden"
	outer := posBox(outerStyle, cssbox.PosRelative, middle)
	root := posBox(posStyle(), cssbox.PosStatic, outer)

	frag := eng.layoutTree(context.Background(), root, 200)
	outerFrag := findByBox(frag, outer)
	if outerFrag == nil {
		t.Fatal("outer fragment not found")
	}

	// Structural fact that matters: middle is consumed onto outer's Positioned; inner is
	// consumed onto middle's Positioned and is NOT directly in outer's Positioned. This
	// proves the grandchild stays scoped to its nearer positioned ancestor (so outer's
	// clip cannot reach it — it never enters outer's consume list).
	if !inPositioned(outerFrag, middle) {
		t.Fatalf("outer.Positioned must contain middle")
	}
	if inPositioned(outerFrag, inner) {
		t.Errorf("outer.Positioned must NOT contain inner directly (the grandchild is scoped to middle)")
	}
	midFrag := findByBox(frag, middle)
	if midFrag == nil {
		t.Fatal("middle fragment not found")
	}
	if !inPositioned(midFrag, inner) {
		t.Errorf("middle.Positioned must contain inner (its nearest positioned ancestor consumes it)")
	}
}

// TestPositionedClipRelativeWithSiblingZOrder: sub-case B combined with a sibling
// z-order. A position:relative + overflow:hidden clip box (z-index:1) holds a relative
// child offset past its edge (clipped via CBOwned, inside the box's bracket); a sibling
// positioned box (z-index:2) overlaps it. Assert BOTH: (a) the relative child paints
// INSIDE the clip box's bracket, and (b) the z:2 sibling paints AFTER the clip box's
// whole subtree (its bracket and contents) — the CBOwned clip composes with the z-index
// band sort.
func TestPositionedClipRelativeWithSiblingZOrder(t *testing.T) {
	eng := New(nil, nil, nil)
	childBG := color.RGBA{0, 0, 91, 255}
	sibBG := color.RGBA{91, 0, 0, 255}

	// Relative child inside the clip box, offset past its 50px edge.
	child := posBox(zfill(60, childBG, 40, 40, 0, true), cssbox.PosRelative)

	// The clip box: position:relative (a stacking context that consumes the relative
	// child) + overflow:hidden + z-index:1.
	clipStyle := zfill(50, color.RGBA{0, 0, 0, 0}, 0, 0, 1, false)
	clipStyle.Overflow = "hidden"
	clip := posBox(clipStyle, cssbox.PosRelative, child)

	// A sibling positioned box with a higher z-index, overlapping the clip box.
	sib := posBox(zfill(50, sibBG, 20, 20, 2, false), cssbox.PosRelative)

	root := posBox(posStyle(), cssbox.PosStatic, clip, sib)
	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)

	push, pop := clipBoundsReal(items)
	ci := bgIndex(items, childBG)
	si := bgIndex(items, sibBG)
	if push < 0 || pop < 0 {
		t.Fatalf("expected a clip bracket; push=%d pop=%d", push, pop)
	}
	if ci < 0 || si < 0 {
		t.Fatalf("missing backgrounds: child=%d sib=%d", ci, si)
	}
	// (a) The relative child is clipped INSIDE the clip box's bracket (sub-case B).
	if ci <= push || ci >= pop {
		t.Errorf("relative child at %d must be INSIDE the clip bracket (push=%d, pop=%d)", ci, push, pop)
	}
	// (b) The z:2 sibling paints AFTER the clip box's whole subtree (after ClipPop): the
	// clip box is z:1, the sibling z:2, so the sibling sorts later in the positioned band.
	if si <= pop {
		t.Errorf("z:2 sibling at %d must paint AFTER the z:1 clip box's ClipPop at %d", si, pop)
	}
}

// TestFloatInternalClipChainReTranslated: a position:relative descendant that bubbles
// OUT OF a non-positioned overflow:hidden box, where BOTH sit inside a float, carries a
// ClipChain whose rect must be re-translated by the float's placement delta — so the
// clip brackets the descendant at the float's FINAL placed position, not the float's
// pre-translation local frame. A RIGHT float gives a large, observable placement delta.
func TestFloatInternalClipChainReTranslated(t *testing.T) {
	eng := New(nil, nil, nil)

	// rel: a relative descendant, offset past the inner clip box edge so it bubbles out
	// of the clip box (gaining the clip box's padding rect in its chain).
	rel := posBox(zfill(40, color.RGBA{7, 7, 7, 255}, 30, 30, 0, true), cssbox.PosRelative)

	// clipbox: a NON-positioned overflow:hidden box (a BFC but not a stacking context),
	// so rel bubbles PAST it, growing its clip chain by clipbox's padding box.
	clipStyle := gcss.ComputedStyle{Display: "block", Overflow: "hidden",
		Width:  gcss.Length{Value: 30, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 30, Unit: gcss.UnitPx}}
	clipbox := blockBox(clipStyle, rel)

	// A RIGHT float (large placement delta) of width 50 in a 200 CB → content-box left
	// 150. The clip box and rel are laid out at the float's provisional origin (x=0),
	// then translateFragment moves the whole float subtree right by ~150.
	fStyle := posStyle()
	fStyle.Float = "right"
	fStyle.Width = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	fStyle.Height = gcss.Length{Value: 80, Unit: gcss.UnitPx}
	floated := posBox(fStyle, cssbox.PosStatic, clipbox)
	floated.Float = cssbox.FloatRight

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated)
	frag := eng.layoutTree(context.Background(), root, 200)

	if len(frag.Floats) != 1 {
		t.Fatalf("root.Floats = %d, want 1", len(frag.Floats))
	}
	ff := frag.Floats[0]
	// The relative descendant bubbled onto the float's own Positioned layer with a
	// non-empty ClipChain (clipbox's padding box).
	if len(ff.Positioned) != 1 || len(ff.PositionedInfo) != 1 {
		t.Fatalf("float.Positioned=%d PositionedInfo=%d, want 1 each", len(ff.Positioned), len(ff.PositionedInfo))
	}
	chain := ff.PositionedInfo[0].ClipChain
	if len(chain) != 1 {
		t.Fatalf("ClipChain len = %d, want 1 (clipbox padding box)", len(chain))
	}
	// The clip box is at the float's content-box origin; the float placed at right edge
	// has X≈150, so the re-translated clip rect's x must be ≈150 (the FINAL position),
	// NOT ≈0 (the pre-translation local frame). This is the re-translation under test.
	if !approx(chain[0].x, ff.X) {
		t.Errorf("ClipChain[0].x = %v, want %v (float final X — the rect must be re-translated by the placement delta, not left at the pre-translation ~0)", chain[0].x, ff.X)
	}
}

// TestRelativeDescendantThroughStaticOfPositionedClipIsClipped: a position:relative
// descendant separated from a position:relative + overflow:hidden box by a STATIC block
// (outer > plain static > rel) is still in-flow content of the clip box, so the box must
// clip it. The static intermediate is transparent: the relative descendant bubbles past
// it (a static box is not a stacking context, so it does not consume the descendant) and
// is consumed by the positioned clip box, which is its nearest positioned ancestor — so
// it must paint INSIDE the box's clip bracket, not in the escaped band. Adversarial: the
// descendant is offset past the clip edge.
func TestRelativeDescendantThroughStaticOfPositionedClipIsClipped(t *testing.T) {
	eng := New(nil, nil, nil)
	relBG := color.RGBA{0, 70, 0, 255}

	// rel: relative descendant, offset past the clip edge.
	rel := posBox(zfill(60, relBG, 40, 40, 0, true), cssbox.PosRelative)
	// plain: a STATIC block between the clip box and rel (transparent to stacking/CB).
	plain := posBox(func() gcss.ComputedStyle {
		s := posStyle()
		s.Height = gcss.Length{Value: 10, Unit: gcss.UnitPx}
		return s
	}(), cssbox.PosStatic, rel)
	// clip: position:relative + overflow:hidden (a stacking context + clip box).
	clipStyle := posStyle()
	clipStyle.Width = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	clipStyle.Height = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	clipStyle.Overflow = "hidden"
	clip := posBox(clipStyle, cssbox.PosRelative, plain)
	root := posBox(posStyle(), cssbox.PosStatic, clip)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	push, pop := clipBoundsReal(items)
	ri := bgIndex(items, relBG)
	if push < 0 || pop < 0 {
		t.Fatalf("expected a clip bracket; push=%d pop=%d", push, pop)
	}
	if ri < 0 {
		t.Fatal("relative descendant background not painted")
	}
	if ri <= push || ri >= pop {
		t.Errorf("relative descendant (through a static block) at %d must be INSIDE the positioned clip box's bracket (push=%d, pop=%d)", ri, push, pop)
	}
}

// inPositioned reports whether the fragment whose Box == target appears directly in
// f.Positioned (one level, not recursively).
func inPositioned(f *Fragment, target *cssbox.Box) bool {
	for _, p := range f.Positioned {
		if p.Box == target {
			return true
		}
	}
	return false
}
