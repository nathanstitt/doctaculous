package css

import (
	"testing"

	gcss "github.com/nathanstitt/omnidoc/pkg/css"
	"github.com/nathanstitt/omnidoc/pkg/layout/cssbox"
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

// outOfFlowFrag builds a child fragment at the given band, optionally out-of-flow.
func outOfFlowFrag(y, h float64, isFloat, isPos bool) *Fragment {
	return &Fragment{Y: y, H: h, W: 100, IsFloat: isFloat, IsPositioned: isPos,
		Box: &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}}
}

// TestSplitMixedKeepsOutOfFlowChildren is a content-loss regression test.
//
// splitMixedBlock rebuilds head/tail child lists from inFlowChildren, which filters
// out floats and positioned boxes. Without redistributing them afterwards they are in
// NEITHER fragment — the float and the absolutely-positioned box simply disappear from
// the rendered document, silently and with no log.
func TestSplitMixedKeepsOutOfFlowChildren(t *testing.T) {
	parent := &Fragment{Y: 0, H: 200, W: 100,
		Box: &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}}
	inflowTop := outOfFlowFrag(0, 50, false, false)
	floatTop := outOfFlowFrag(50, 20, true, false)
	posBottom := outOfFlowFrag(120, 20, false, true)
	inflowBottom := outOfFlowFrag(150, 50, false, false)
	parent.Children = []*Fragment{inflowTop, floatTop, posBottom, inflowBottom}

	res := splitMixedBlock(parent, 100, 0, 0)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split; got head=%v tail=%v", res.head, res.tail)
	}
	total := len(res.head.Children) + len(res.tail.Children)
	if total != 4 {
		t.Errorf("split kept %d of 4 children; out-of-flow children must not be dropped", total)
	}
	// Each out-of-flow child goes to the fragment whose band contains it.
	if !containsFrag(res.head.Children, floatTop) {
		t.Error("the float at y=50 should ride the HEAD (it sits above the split)")
	}
	if !containsFrag(res.tail.Children, posBottom) {
		t.Error("the positioned box at y=120 should ride the TAIL (it sits below the split)")
	}
}

// TestSplitMixedNoOutOfFlowUnchanged pins that a block with only in-flow children is
// unaffected — the common case must stay byte-identical.
func TestSplitMixedNoOutOfFlowUnchanged(t *testing.T) {
	parent := &Fragment{Y: 0, H: 200, W: 100,
		Box: &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}}
	parent.Children = []*Fragment{
		outOfFlowFrag(0, 50, false, false),
		outOfFlowFrag(150, 50, false, false),
	}
	res := splitMixedBlock(parent, 100, 0, 0)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split; got head=%v tail=%v", res.head, res.tail)
	}
	if len(res.head.Children) != 1 || len(res.tail.Children) != 1 {
		t.Errorf("in-flow only: head=%d tail=%d children, want 1 and 1",
			len(res.head.Children), len(res.tail.Children))
	}
}

func containsFrag(list []*Fragment, want *Fragment) bool {
	for _, f := range list {
		if f == want {
			return true
		}
	}
	return false
}

// TestSplitDetachesSharedExtras is a paint-correctness regression test.
//
// A split is `head := *b; tail := *b`, which copies the BgImage POINTER and the
// ClipChain slice header. shiftFragmentExtras moves a fragment into its page's local
// frame by mutating exactly those in place — so without detaching, shifting the head
// also moves the tail's background origin by the head page's offset, and the
// continuation page paints its background in the wrong place.
func TestSplitDetachesSharedExtras(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	parent := &Fragment{Y: 0, H: 200, W: 100, Box: box,
		BgImage: &BackgroundImageContent{OriginX: 5, OriginY: 7},
	}
	parent.Children = []*Fragment{
		{Y: 0, H: 50, W: 100, Box: box},
		{Y: 150, H: 50, W: 100, Box: box},
	}
	res := splitAnyBlockForPage(parent, 100, 0, 0)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split; got head=%v tail=%v", res.head, res.tail)
	}
	if res.head.BgImage == res.tail.BgImage {
		t.Fatal("head and tail share one BgImage struct; a per-page shift of either " +
			"would move the other's background too")
	}
	// Shifting one must not disturb the other.
	res.head.BgImage.OriginY += 1000
	if res.tail.BgImage.OriginY != 7 {
		t.Errorf("shifting the head moved the tail's background origin to %v, want 7",
			res.tail.BgImage.OriginY)
	}
}

// TestSplitWholeDoesNotCopy pins that a "fits whole" / "moves whole" result hands back
// the ORIGINAL pointer — the bucketer relies on that identity, and copying there would
// silently detach a fragment from the tree it belongs to.
func TestSplitWholeDoesNotCopy(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	parent := &Fragment{Y: 0, H: 50, W: 100, Box: box,
		BgImage: &BackgroundImageContent{OriginX: 5, OriginY: 7},
	}
	parent.Children = []*Fragment{{Y: 0, H: 50, W: 100, Box: box}}
	res := splitAnyBlockForPage(parent, 1000, 0, 0) // fits entirely
	if res.head != parent {
		t.Errorf("a whole-fit must return the original fragment pointer, got %p want %p",
			res.head, parent)
	}
}

// TestSplitClampsClipRect: a split overflow:hidden block must clip to each fragment's
// own extent. Both halves inherit the whole original box's clip rect from the struct
// copy, so without clamping each page clips to the full pre-split height and content
// belonging to the other fragment can paint through.
func TestSplitClampsClipRect(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	parent := &Fragment{Y: 0, H: 200, W: 100, Box: box,
		Clips: true, ClipRect: rect{x: 0, y: 0, w: 100, h: 200},
	}
	parent.Children = []*Fragment{
		{Y: 0, H: 50, W: 100, Box: box},
		{Y: 150, H: 50, W: 100, Box: box},
	}
	res := splitAnyBlockForPage(parent, 100, 0, 0)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split; got head=%v tail=%v", res.head, res.tail)
	}
	for _, c := range []struct {
		name string
		f    *Fragment
	}{{"head", res.head}, {"tail", res.tail}} {
		top, bottom := c.f.Y, c.f.Y+c.f.H
		if c.f.ClipRect.y < top-0.01 || c.f.ClipRect.y+c.f.ClipRect.h > bottom+0.01 {
			t.Errorf("%s clip rect [%v,%v] escapes its own extent [%v,%v]",
				c.name, c.f.ClipRect.y, c.f.ClipRect.y+c.f.ClipRect.h, top, bottom)
		}
	}
	// The horizontal extent is untouched — a page break divides the block axis only.
	if res.head.ClipRect.w != 100 {
		t.Errorf("head clip width = %v, want 100 (unchanged)", res.head.ClipRect.w)
	}
}

// linedBlock builds a block fragment of n lines at lineH each, starting at y.
func linedBlock(y, lineH float64, n int) *Fragment {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	f := &Fragment{Y: y, H: lineH * float64(n), W: 100, Box: box}
	for i := 0; i < n; i++ {
		f.Lines = append(f.Lines, LineFragment{BaselineY: y + lineH*float64(i) + lineH*0.8})
	}
	return f
}

// TestSplitRecursesIntoStraddlingChild is the headline N1a case: a section > div > p
// spine where the page boundary falls mid-paragraph.
//
// Before recursion, a child straddling the boundary rode the tail WHOLE, leaving a gap
// on the head page the height of its above-boundary portion — here 50pt of blank page
// even though five of the paragraph's eight lines would have fit.
func TestSplitRecursesIntoStraddlingChild(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	p := linedBlock(50, 10, 8) // y=50..130
	div := &Fragment{Y: 50, H: 80, W: 100, Box: box, Children: []*Fragment{p}}
	section := &Fragment{Y: 0, H: 130, W: 100, Box: box,
		Children: []*Fragment{{Y: 0, H: 50, W: 100, Box: box}, div}}

	res := splitAnyBlockForPage(section, 100, 0, 0)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split; got head=%v tail=%v", res.head, res.tail)
	}
	// The head must reach the boundary, not stop at the last whole child.
	if gap := 100 - (res.head.Y + res.head.H); gap > 1 {
		t.Errorf("head ends at %v, leaving a %vpt gap before the boundary at 100; "+
			"the straddling child should have been split", res.head.Y+res.head.H, gap)
	}
	// Both halves of the spine survive: head has the first block plus the div's head.
	if len(res.head.Children) != 2 {
		t.Errorf("head has %d children, want 2 (the fitting block + the split div's head)",
			len(res.head.Children))
	}
	if len(res.tail.Children) != 1 {
		t.Errorf("tail has %d children, want 1 (the split div's tail)", len(res.tail.Children))
	}
	// No line is lost or duplicated across the two halves of the paragraph.
	headLines := countLines(res.head)
	tailLines := countLines(res.tail)
	if headLines+tailLines != 8 {
		t.Errorf("lines head=%d tail=%d, total %d, want 8 (no line dropped or duplicated)",
			headLines, tailLines, headLines+tailLines)
	}
	if headLines == 0 || tailLines == 0 {
		t.Errorf("expected the paragraph to split; got head=%d tail=%d lines", headLines, tailLines)
	}
}

// TestSplitDoesNotRecurseIntoAvoidChild: `break-inside: avoid` on the straddling child
// stops the recursion — the child rides the tail whole, as the author asked.
func TestSplitDoesNotRecurseIntoAvoidChild(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	avoidBox := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Style: gcss.ComputedStyle{BreakInside: "avoid"}}
	p := linedBlock(50, 10, 8)
	p.Box = avoidBox
	section := &Fragment{Y: 0, H: 130, W: 100, Box: box,
		Children: []*Fragment{{Y: 0, H: 50, W: 100, Box: box}, p}}

	res := splitAnyBlockForPage(section, 100, 0, 0)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split; got head=%v tail=%v", res.head, res.tail)
	}
	if countLines(res.head) != 0 {
		t.Errorf("break-inside:avoid child was split (%d lines on the head); it must ride "+
			"the tail whole", countLines(res.head))
	}
	if countLines(res.tail) != 8 {
		t.Errorf("tail has %d lines, want all 8", countLines(res.tail))
	}
}

// TestSplitStraddlerNotSplittableRidesTail: a straddling child with nothing to split on
// (a single line, no block children) still moves whole rather than being clipped.
func TestSplitStraddlerNotSplittableRidesTail(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	single := linedBlock(50, 80, 1) // one tall line: nothing to split between
	section := &Fragment{Y: 0, H: 130, W: 100, Box: box,
		Children: []*Fragment{{Y: 0, H: 50, W: 100, Box: box}, single}}

	res := splitAnyBlockForPage(section, 100, 0, 0)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split; got head=%v tail=%v", res.head, res.tail)
	}
	if countLines(res.head) != 0 {
		t.Errorf("an unsplittable straddler should ride the tail; head has %d lines",
			countLines(res.head))
	}
}

// TestSplitRecursionKeepsNestedOutOfFlow: an out-of-flow child of the STRADDLING box
// survives the recursive split (N1b's fix must hold at depth, not just at the top).
func TestSplitRecursionKeepsNestedOutOfFlow(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	inner1 := &Fragment{Y: 50, H: 30, W: 100, Box: box}
	inner2 := &Fragment{Y: 100, H: 30, W: 100, Box: box}
	floatChild := &Fragment{Y: 55, H: 10, W: 20, Box: box, IsFloat: true}
	div := &Fragment{Y: 50, H: 80, W: 100, Box: box,
		Children: []*Fragment{inner1, floatChild, inner2}}
	section := &Fragment{Y: 0, H: 130, W: 100, Box: box,
		Children: []*Fragment{{Y: 0, H: 50, W: 100, Box: box}, div}}

	res := splitAnyBlockForPage(section, 100, 0, 0)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split; got head=%v tail=%v", res.head, res.tail)
	}
	if !fragTreeContains(res.head, floatChild) && !fragTreeContains(res.tail, floatChild) {
		t.Error("the float nested inside the straddling child was dropped by the recursive split")
	}
}

// countLines totals the line count over a fragment subtree.
func countLines(f *Fragment) int {
	if f == nil {
		return 0
	}
	n := len(f.Lines)
	for _, c := range f.Children {
		n += countLines(c)
	}
	return n
}

// fragTreeContains reports whether want appears anywhere in f's subtree.
func fragTreeContains(f, want *Fragment) bool {
	if f == nil {
		return false
	}
	if f == want {
		return true
	}
	for _, c := range f.Children {
		if fragTreeContains(c, want) {
			return true
		}
	}
	return false
}
