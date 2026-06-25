package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
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
