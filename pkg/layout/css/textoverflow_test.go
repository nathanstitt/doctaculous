package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// lastBox returns the deepest last-child fragment, which for the single-<div> pages
// below is the div itself.
func lastBox(f *Fragment) *Fragment {
	for len(f.Children) > 0 {
		f = f.Children[len(f.Children)-1]
	}
	return f
}

// hasEllipsis reports whether any emitted glyph is U+2026.
func hasEllipsis(items []layout.Item) bool {
	for _, it := range items {
		if it.Kind != layout.GlyphKind {
			continue
		}
		for _, r := range it.Glyph.Runes {
			if r == '…' {
				return true
			}
		}
	}
	return false
}

const overflowText = "alpha bravo charlie delta echo foxtrot golf hotel india"

func layoutDiv(t *testing.T, style, text string) *Fragment {
	t.Helper()
	return layoutWithLoader(t,
		`<body><div style="font-size:20px;`+style+`">`+text+`</div></body>`,
		400, resource.MapLoader{}, nil)
}

// text-overflow: ellipsis truncates an overflowing line and appends U+2026. Before
// this it was byte-identical to no ellipsis: the property parsed nowhere and the line
// was simply clipped mid-glyph.
func TestTextOverflowEllipsisEmitsEllipsisGlyph(t *testing.T) {
	root := layoutDiv(t, "width:120px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis", overflowText)
	if !hasEllipsis(root.AppendItems(nil)) {
		t.Error("no U+2026 glyph emitted for an overflowing text-overflow:ellipsis line")
	}
}

// text-overflow: clip is the initial value and must NOT add an ellipsis — the
// falsifiability control for the assertion above.
func TestTextOverflowClipEmitsNoEllipsis(t *testing.T) {
	root := layoutDiv(t, "width:120px;white-space:nowrap;overflow:hidden;text-overflow:clip", overflowText)
	if hasEllipsis(root.AppendItems(nil)) {
		t.Error("text-overflow:clip emitted an ellipsis")
	}
}

// An ellipsis needs something to hide the truncation behind, so it applies only where
// the box CLIPS. An overflow:visible box still overflows visibly, matching browsers.
func TestTextOverflowNeedsAClippingBox(t *testing.T) {
	root := layoutDiv(t, "width:120px;white-space:nowrap;text-overflow:ellipsis", overflowText)
	if hasEllipsis(root.AppendItems(nil)) {
		t.Error("ellipsis applied to a non-clipping box; the text should overflow visibly")
	}
}

// A line that fits is left alone.
func TestTextOverflowLeavesFittingTextAlone(t *testing.T) {
	root := layoutDiv(t, "width:300px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis", "short")
	if hasEllipsis(root.AppendItems(nil)) {
		t.Error("ellipsis added to text that already fits")
	}
}

// The truncated line stays within the box: the cut removes whole glyphs until the
// ellipsis fits, rather than appending it past the clip edge.
func TestTextOverflowStaysWithinTheBox(t *testing.T) {
	root := layoutDiv(t, "width:120px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis", overflowText)
	box := lastBox(root)
	right := box.X + box.W
	for _, it := range root.AppendItems(nil) {
		if it.Kind == layout.GlyphKind && it.Glyph.XPt > right {
			t.Fatalf("glyph at x=%v is past the box's right edge %v", it.Glyph.XPt, right)
		}
	}
}

// -webkit-line-clamp stops the box after N lines AND shrinks its height to them. It is
// a layout effect, not only a paint clip — a browser reports the shorter height, and a
// paint-only implementation would leave the box its full height with blank space.
func TestLineClampShrinksBoxHeight(t *testing.T) {
	full := lastBox(layoutDiv(t, "width:120px", overflowText)).H
	two := lastBox(layoutDiv(t, "width:120px;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden", overflowText)).H
	one := lastBox(layoutDiv(t, "width:120px;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:1;overflow:hidden", overflowText)).H

	if two >= full {
		t.Errorf("clamp:2 height %v is not less than the unclamped %v", two, full)
	}
	if one >= two {
		t.Errorf("clamp:1 height %v is not less than clamp:2's %v", one, two)
	}
	// Each clamp level is one line box tall, so two lines is twice one (within a
	// rounding epsilon).
	if d := two - 2*one; d > 0.5 || d < -0.5 {
		t.Errorf("clamp:2 height %v is not twice clamp:1's %v", two, one)
	}
}

// The clamped final line is marked with an ellipsis, because text was cut after it.
func TestLineClampMarksTheLastLine(t *testing.T) {
	root := layoutDiv(t, "width:120px;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden", overflowText)
	if !hasEllipsis(root.AppendItems(nil)) {
		t.Error("no ellipsis on the clamped line")
	}
}

// A clamp larger than the text's line count changes nothing and adds no ellipsis:
// nothing was cut, so nothing should signal a cut.
func TestLineClampLargerThanContentIsInert(t *testing.T) {
	plain := lastBox(layoutDiv(t, "width:120px", "two words"))
	clamped := layoutDiv(t, "width:120px;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:9;overflow:hidden", "two words")
	if h := lastBox(clamped).H; h != plain.H {
		t.Errorf("height %v != unclamped %v; an over-large clamp must not change layout", h, plain.H)
	}
	if hasEllipsis(clamped.AppendItems(nil)) {
		t.Error("an over-large clamp added an ellipsis though nothing was cut")
	}
}

// The unprefixed `line-clamp` spelling works the same as `-webkit-line-clamp`.
func TestStandardLineClampSpelling(t *testing.T) {
	webkit := lastBox(layoutDiv(t, "width:120px;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden", overflowText)).H
	std := lastBox(layoutDiv(t, "width:120px;display:-webkit-box;-webkit-box-orient:vertical;line-clamp:2;overflow:hidden", overflowText)).H
	if webkit != std {
		t.Errorf("line-clamp height %v != -webkit-line-clamp height %v", std, webkit)
	}
}

// Text with no clamp and no ellipsis is untouched, so the common path stays exactly
// as it was.
func TestUnclampedTextIsUnchanged(t *testing.T) {
	root := layoutDiv(t, "width:120px", overflowText)
	if hasEllipsis(root.AppendItems(nil)) {
		t.Error("plain wrapped text gained an ellipsis")
	}
}
