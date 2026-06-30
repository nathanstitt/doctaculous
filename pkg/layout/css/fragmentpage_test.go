package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// makeLineBlock builds a synthetic line-splittable block fragment: n lines of height lh
// each, top-left at (0, y0), no border/padding (so content top == y0). widows/orphans
// are set on its Box style. Each line gets one dummy glyph so it is non-empty.
func makeLineBlock(y0, lh float64, n, widows, orphans int) *Fragment {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	box.Style = gcss.ComputedStyle{Widows: widows, Orphans: orphans}
	f := &Fragment{X: 0, Y: y0, W: 200, H: float64(n) * lh, Box: box}
	for i := 0; i < n; i++ {
		// baseline ~= top of line + 0.8*lh (a representative ascent); the splitter reads
		// baseline DELTA (== lh) and the block edges, not the absolute ascent.
		f.Lines = append(f.Lines, LineFragment{
			BaselineY: y0 + float64(i)*lh + 0.8*lh,
			Glyphs:    []GlyphFragment{{X: 0, AdvancePt: 5, SizePt: 10}},
		})
	}
	return f
}

func TestSplitBlockBasic(t *testing.T) {
	// 10 lines of 10pt at y0=0; page bottom at 55 ⇒ 5 lines fit (bottoms at 10..50 ≤ 55).
	// widows=orphans=2: head=5, tail=5 (both satisfy the minimums).
	b := makeLineBlock(0, 10, 10, 2, 2)
	res := splitBlockForPage(b, 55, 2, 2)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split, got head=%v tail=%v", res.head, res.tail)
	}
	if len(res.head.Lines) != 5 {
		t.Errorf("head lines = %d, want 5", len(res.head.Lines))
	}
	if len(res.tail.Lines) != 5 {
		t.Errorf("tail lines = %d, want 5", len(res.tail.Lines))
	}
	// The tail keeps lines at their ORIGINAL page-space Y (line 5 baseline = 5*10+0.8*10
	// = 58); tail.Y is moved to the first kept line's top (line 5 top = 50).
	if got := res.tail.Lines[0].BaselineY; got < 58-0.5 || got > 58+0.5 {
		t.Errorf("tail first baseline = %.1f, want ~58 (original position, unmoved)", got)
	}
	if got := res.tail.Y; got < 50-0.5 || got > 50+0.5 {
		t.Errorf("tail.Y = %.1f, want 50 (top of first kept line)", got)
	}
	// The head's bottom border is suppressed; head height = top edge(0) + 5 lines = 50.
	if res.head.H != 50 {
		t.Errorf("head H = %.1f, want 50", res.head.H)
	}
}

func TestSplitBlockOrphans(t *testing.T) {
	// Only 1 line fits above the page bottom (bottom at 10 ≤ 15, line 2 bottom 20 > 15).
	// orphans=2 forbids leaving just 1 line ⇒ move the whole block to the next page.
	b := makeLineBlock(0, 10, 6, 2, 2)
	res := splitBlockForPage(b, 15, 2, 2)
	if res.head != nil {
		t.Errorf("orphans violation: expected whole-block move (head nil), got head with %d lines", len(res.head.Lines))
	}
	if res.tail != b {
		t.Errorf("expected tail == whole block")
	}
	// With orphans=1 the same split is allowed (1 line may stay).
	res1 := splitBlockForPage(makeLineBlock(0, 10, 6, 2, 1), 15, 2, 1)
	if res1.head == nil || len(res1.head.Lines) != 1 {
		t.Errorf("orphans=1: want head of 1 line, got %v", res1.head)
	}
}

func TestSplitBlockWidows(t *testing.T) {
	// 6 lines; 5 fit (bottoms ≤ 55). widows=2 forbids carrying just 1 line ⇒ pull one
	// back so the tail gets 2: head=4, tail=2.
	b := makeLineBlock(0, 10, 6, 2, 2)
	res := splitBlockForPage(b, 55, 2, 2)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected split, got head=%v tail=%v", res.head, res.tail)
	}
	if len(res.tail.Lines) != 2 {
		t.Errorf("tail lines = %d, want 2 (widows pulled a line back)", len(res.tail.Lines))
	}
	if len(res.head.Lines) != 4 {
		t.Errorf("head lines = %d, want 4", len(res.head.Lines))
	}
}

func TestSplitBlockTooShortMovesWhole(t *testing.T) {
	// 3 lines, widows=2 orphans=2 ⇒ 3 < 2+2, the block cannot satisfy both ⇒ move whole.
	b := makeLineBlock(0, 10, 3, 2, 2)
	// Page bottom at 25 ⇒ 2 lines fit, but widows would pull to head=1 < orphans.
	res := splitBlockForPage(b, 25, 2, 2)
	if res.head != nil || res.tail != b {
		t.Errorf("3-line block with widows+orphans=4 should move whole; got head=%v", res.head)
	}
}

func TestSplitBlockAllFit(t *testing.T) {
	// Page bottom past the last line ⇒ no split (head=b, tail=nil).
	b := makeLineBlock(0, 10, 4, 2, 2)
	res := splitBlockForPage(b, 1000, 2, 2)
	if res.head != b || res.tail != nil {
		t.Errorf("all-fit should yield head=b,tail=nil; got head=%v tail=%v", res.head, res.tail)
	}
}

func TestLineSplittableGuards(t *testing.T) {
	// break-inside: avoid disqualifies.
	b := makeLineBlock(0, 10, 4, 2, 2)
	b.Box.Style.BreakInside = "avoid"
	if lineSplittable(b) {
		t.Errorf("break-inside:avoid block must not be line-splittable")
	}
	// A block with an in-flow block child IS splittable now (mixed block+inline split at a
	// child boundary via splitMixedBlock), even with fewer than two lines of its own.
	b2 := &Fragment{Y: 0, H: 10, Box: &cssbox.Box{Kind: cssbox.BoxBlock}}
	b2.Children = []*Fragment{{Y: 5, H: 5}}
	if !lineSplittable(b2) {
		t.Errorf("block with an in-flow block child should be splittable (mixed split)")
	}
	// break-inside: avoid still disqualifies a mixed block.
	b2avoid := &Fragment{Y: 0, H: 10, Box: &cssbox.Box{Kind: cssbox.BoxBlock}}
	b2avoid.Box.Style.BreakInside = "avoid"
	b2avoid.Children = []*Fragment{{Y: 5, H: 5}}
	if lineSplittable(b2avoid) {
		t.Errorf("break-inside:avoid mixed block must not be splittable")
	}
	// A single-line block with no in-flow block child is not splittable.
	if lineSplittable(makeLineBlock(0, 10, 1, 2, 2)) {
		t.Errorf("single-line block must not be line-splittable")
	}
	// A block whose only children are out-of-flow (a float) is not splittable on that
	// basis (its in-flow content is its lines, which here number fewer than two).
	bFloat := makeLineBlock(0, 10, 1, 2, 2)
	bFloat.Children = []*Fragment{{Y: 5, H: 5, IsFloat: true}}
	if lineSplittable(bFloat) {
		t.Errorf("single-line block with only a float child must not be splittable")
	}
	// A plain multi-line block IS splittable.
	if !lineSplittable(makeLineBlock(0, 10, 4, 2, 2)) {
		t.Errorf("plain multi-line block should be line-splittable")
	}
}

// A mixed block: a 4-line paragraph fragment child, then a block child, both in flow.
// Splitting at the boundary after the paragraph keeps the paragraph on page 0 and moves
// the block child to page 1.
func TestSplitMixedBlock(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	box.Style = gcss.ComputedStyle{Widows: 1, Orphans: 1}
	parent := &Fragment{Y: 0, H: 80, Box: box}
	para := makeLineBlock(0, 10, 4, 1, 1) // 4 lines at y 0..40
	child := &Fragment{Y: 40, H: 40, Box: &cssbox.Box{Kind: cssbox.BoxBlock}}
	parent.Children = []*Fragment{para, child}
	// Page bottom at 45 ⇒ the paragraph (ends 40) fits, the child (40..80) doesn't.
	res := splitMixedBlock(parent, 45, 1, 1)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a mixed split, got head=%v tail=%v", res.head, res.tail)
	}
	if len(res.head.Children) != 1 || res.head.Children[0] != para {
		t.Errorf("head should hold the paragraph only")
	}
	if len(res.tail.Children) != 1 || res.tail.Children[0] != child {
		t.Errorf("tail should hold the block child only")
	}
}
