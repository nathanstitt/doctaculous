package css

import (
	"context"
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// TestClipFragmentFlaggedWithPaddingBox: an overflow:hidden box's fragment is flagged
// Clips with ClipRect == its padding box (border box deflated by border widths).
func TestClipFragmentFlaggedWithPaddingBox(t *testing.T) {
	eng := New(nil, nil, nil)
	clipStyle := gcss.ComputedStyle{
		Display:        "block",
		Overflow:       "hidden",
		Width:          gcss.Length{Value: 100, Unit: gcss.UnitPx},
		Height:         gcss.Length{Value: 50, Unit: gcss.UnitPx},
		BorderTopWidth: gcss.Length{Value: 5, Unit: gcss.UnitPx}, BorderTopStyle: "solid",
		BorderRightWidth: gcss.Length{Value: 5, Unit: gcss.UnitPx}, BorderRightStyle: "solid",
		BorderBottomWidth: gcss.Length{Value: 5, Unit: gcss.UnitPx}, BorderBottomStyle: "solid",
		BorderLeftWidth: gcss.Length{Value: 5, Unit: gcss.UnitPx}, BorderLeftStyle: "solid",
	}
	box := blockBox(clipStyle)
	root := blockBox(gcss.ComputedStyle{Display: "block"}, box)

	frag := eng.layoutTree(context.Background(), root, 200)
	if frag == nil || len(frag.Children) != 1 {
		t.Fatalf("expected 1 child fragment, got %v", frag)
	}
	clip := frag.Children[0]
	if !clip.Clips {
		t.Fatalf("clip fragment not flagged Clips")
	}
	if clip.ClipRect.x != 5 || clip.ClipRect.y != 5 || clip.ClipRect.w != 100 || clip.ClipRect.h != 50 {
		t.Errorf("ClipRect = %+v, want {5 5 100 50} (padding box)", clip.ClipRect)
	}
}

// TestClipAbsChildCBOwnedFlagged: an absolute child of an overflow:hidden positioned
// box is collected on that box's Positioned with PositionedClip=true (the box IS its
// CB), so it will be clipped.
func TestClipAbsChildCBOwnedFlagged(t *testing.T) {
	eng := New(nil, nil, nil)
	absChild := posBox(posStyle(), cssbox.PosAbsolute)
	absChild.Style.Top = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Left = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Width = gcss.Length{Value: 10, Unit: gcss.UnitPx}
	absChild.Style.Height = gcss.Length{Value: 10, Unit: gcss.UnitPx}

	contStyle := posStyle()
	contStyle.Overflow = "hidden"
	contStyle.Width = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	contStyle.Height = gcss.Length{Value: 60, Unit: gcss.UnitPx}
	container := posBox(contStyle, cssbox.PosRelative, absChild)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, container)
	frag := eng.layoutTree(context.Background(), root, 200)

	cont := frag.Children[0]
	if !cont.Clips {
		t.Fatalf("container not flagged Clips")
	}
	if len(cont.Positioned) != 1 {
		t.Fatalf("container Positioned len = %d, want 1", len(cont.Positioned))
	}
	if len(cont.PositionedClip) != 1 || !cont.PositionedClip[0] {
		t.Errorf("PositionedClip = %v, want [true] (abs child's CB is the container)", cont.PositionedClip)
	}
}

// clipBoundsReal: indices of the first ClipPush and the matching ClipPop.
func clipBoundsReal(items []layout.Item) (push, pop int) {
	push, pop = -1, -1
	for i := range items {
		if items[i].Kind == layout.ClipPushKind && push < 0 {
			push = i
		}
		if items[i].Kind == layout.ClipPopKind {
			pop = i
		}
	}
	return
}

// bgIndex: index of the first BackgroundKind item with the given color, or -1.
func bgIndex(items []layout.Item, c color.RGBA) int {
	for i := range items {
		if items[i].Kind == layout.BackgroundKind && items[i].Rule.Color == c {
			return i
		}
	}
	return -1
}

// TestClipInFlowChildClipped: an in-flow child taller than its overflow:hidden parent
// has its background painted INSIDE the clip bracket (between ClipPush and ClipPop).
func TestClipInFlowChildClipped(t *testing.T) {
	eng := New(nil, nil, nil)
	tall := blockBox(gcss.ComputedStyle{Display: "block",
		Height:          gcss.Length{Value: 200, Unit: gcss.UnitPx},
		BackgroundColor: color.RGBA{2, 2, 2, 255}})
	clipStyle := gcss.ComputedStyle{Display: "block", Overflow: "hidden",
		Height: gcss.Length{Value: 50, Unit: gcss.UnitPx}}
	clip := blockBox(clipStyle, tall)
	root := blockBox(gcss.ComputedStyle{Display: "block"}, clip)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	push, pop := clipBoundsReal(items)
	if push < 0 || pop < 0 {
		t.Fatalf("no clip bracket emitted (push=%d pop=%d)", push, pop)
	}
	bg := bgIndex(items, color.RGBA{2, 2, 2, 255})
	if !(push < bg && bg < pop) {
		t.Errorf("in-flow child bg at %d not inside the clip bracket [%d,%d]", bg, push, pop)
	}
}

// TestClipAbsChildOutsideCBNotClipped: an absolute child whose containing block is an
// OUTER relative ancestor (not the overflow:hidden box) paints OUTSIDE the clip
// bracket (after ClipPop). Structure: relative outer > overflow:hidden middle (static)
// > absolute child. The abs child's CB is the outer relative box, so the middle's clip
// must not clip it.
func TestClipAbsChildOutsideCBNotClipped(t *testing.T) {
	eng := New(nil, nil, nil)
	absChild := posBox(posStyle(), cssbox.PosAbsolute)
	absChild.Style.Top = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Left = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Width = gcss.Length{Value: 10, Unit: gcss.UnitPx}
	absChild.Style.Height = gcss.Length{Value: 10, Unit: gcss.UnitPx}
	absChild.Style.BackgroundColor = color.RGBA{9, 9, 9, 255}

	midStyle := gcss.ComputedStyle{Display: "block", Overflow: "hidden",
		Height: gcss.Length{Value: 50, Unit: gcss.UnitPx}}
	mid := blockBox(midStyle, absChild)

	outerStyle := posStyle()
	outerStyle.Width = gcss.Length{Value: 150, Unit: gcss.UnitPx}
	outerStyle.Height = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	outer := posBox(outerStyle, cssbox.PosRelative, mid)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, outer)
	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)

	push, pop := clipBoundsReal(items)
	if push < 0 || pop < 0 {
		t.Fatalf("no clip bracket emitted (push=%d pop=%d)", push, pop)
	}
	bg := bgIndex(items, color.RGBA{9, 9, 9, 255})
	if bg < 0 {
		t.Fatalf("abs child bg not found")
	}
	if push < bg && bg < pop {
		t.Errorf("abs child bg at %d is INSIDE the clip bracket [%d,%d]; its CB is outside, must not be clipped", bg, push, pop)
	}
}

// TestClipAbsChildCBOwnedClipped: an absolute child whose CB IS the overflow:hidden box
// (the box is relative + overflow:hidden) paints INSIDE the clip bracket.
func TestClipAbsChildCBOwnedClipped(t *testing.T) {
	eng := New(nil, nil, nil)
	absChild := posBox(posStyle(), cssbox.PosAbsolute)
	absChild.Style.Top = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Left = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Width = gcss.Length{Value: 10, Unit: gcss.UnitPx}
	absChild.Style.Height = gcss.Length{Value: 10, Unit: gcss.UnitPx}
	absChild.Style.BackgroundColor = color.RGBA{9, 9, 9, 255}

	contStyle := posStyle()
	contStyle.Overflow = "hidden"
	contStyle.Width = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	contStyle.Height = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	cont := posBox(contStyle, cssbox.PosRelative, absChild)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, cont)
	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)

	push, pop := clipBoundsReal(items)
	bg := bgIndex(items, color.RGBA{9, 9, 9, 255})
	if !(push >= 0 && pop >= 0 && push < bg && bg < pop) {
		t.Errorf("abs child bg at %d not inside the clip bracket [%d,%d]; CB is the clip box, must be clipped", bg, push, pop)
	}
}
