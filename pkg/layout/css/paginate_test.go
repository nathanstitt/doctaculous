package css

import (
	"context"
	"image/color"
	"math"
	"strings"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// buildRoot parses src and builds a cssbox root, mirroring layoutTreeFor but
// returning the cssbox tree (the input LayoutPaged needs) rather than a fragment.
func buildRoot(t *testing.T, src string, logf func(string, ...any)) *cssbox.Box {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, logf)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return root
}

// measuredBlockHeights lays src out (single-tall) and returns the body's in-flow
// block fragments' Y and H, so a test can choose a page height relative to the real
// measured geometry (robust to default margins).
func measuredBlockHeights(t *testing.T, src string, viewportW float64) []*Fragment {
	t.Helper()
	root := buildRoot(t, src, nil)
	frag := New(nil, nil, nil).layoutTree(context.Background(), root, viewportW)
	if frag == nil {
		t.Fatalf("layoutTree returned nil for %q", src)
	}
	body := bodyOf(t, frag)
	return body.Children
}

// threeBlocks is three stacked divs of height h px with zero margin (so each
// border box is exactly h pt at px:pt 1:1).
func threeBlocks(h int) string {
	div := `<div style="height:` + itoa(h) + `px;margin:0">x</div>`
	return `<html><body>` + div + div + div + `</body></html>`
}

func TestPaginateByHeight(t *testing.T) {
	const w = 400
	src := threeBlocks(100)

	// Measure the real block geometry first.
	blocks := measuredBlockHeights(t, src, w)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 in-flow blocks, got %d", len(blocks))
	}
	bh := blocks[0].H
	t.Logf("measured block H=%.2f, block Ys=%.2f,%.2f,%.2f", bh, blocks[0].Y, blocks[1].Y, blocks[2].Y)

	// Page height that fits exactly two blocks but not three.
	pageH := 2*bh + bh/2 // 2.5 blocks tall => blocks 1+2 on page 0, block 3 on page 1

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, pageH)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages.Pages))
	}
	for i, p := range pages.Pages {
		if p.HeightPt != pageH {
			t.Errorf("page %d HeightPt = %.2f, want %.2f", i, p.HeightPt, pageH)
		}
		if len(p.Items) == 0 {
			t.Errorf("page %d has no items", i)
		}
	}

	// Pure bucketing: assert which fragment lands on which page by pointer identity.
	buckets := bucketBlocks(blocks, pageH, w, func(string, ...any) {})
	if len(buckets) != 2 {
		t.Fatalf("bucketBlocks: expected 2 buckets, got %d", len(buckets))
	}
	if len(buckets[0].blocks) != 2 || buckets[0].blocks[0] != blocks[0] || buckets[0].blocks[1] != blocks[1] {
		t.Errorf("page 0 should hold blocks[0],blocks[1]")
	}
	if len(buckets[1].blocks) != 1 || buckets[1].blocks[0] != blocks[2] {
		t.Errorf("page 1 should hold blocks[2]")
	}
	// Page 1's top must be block 3's original Y (so it shifts to local Y 0).
	if buckets[1].top != blocks[2].Y {
		t.Errorf("page 1 top = %.2f, want block[2].Y = %.2f", buckets[1].top, blocks[2].Y)
	}
}

func TestPaginateForcedBreakBefore(t *testing.T) {
	const w = 400
	src := `<html><body>` +
		`<div style="height:20px;margin:0">a</div>` +
		`<div style="height:20px;margin:0;page-break-before:always">b</div>` +
		`</body></html>`

	blocks := measuredBlockHeights(t, src, w)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	// A page tall enough for both: only the forced break should split them.
	pageH := blocks[0].H + blocks[1].H + 100

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, pageH)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("expected 2 pages from forced break-before, got %d", len(pages.Pages))
	}
}

func TestPaginateForcedBreakAfter(t *testing.T) {
	const w = 400
	src := `<html><body>` +
		`<div style="height:20px;margin:0;break-after:page">a</div>` +
		`<div style="height:20px;margin:0">b</div>` +
		`</body></html>`

	blocks := measuredBlockHeights(t, src, w)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	pageH := blocks[0].H + blocks[1].H + 100

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, pageH)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("expected 2 pages from forced break-after, got %d", len(pages.Pages))
	}
}

// hasBackgroundColor reports whether any item on the page is a BackgroundKind fill of
// color c (used to locate a float's painted background on a given page).
func hasBackgroundColor(p layout.Page, c color.RGBA) bool {
	for _, it := range p.Items {
		if it.Kind == layout.BackgroundKind && it.Rule.Color == c {
			return true
		}
	}
	return false
}

// TestPaginateDistributesFloatToItsPage is a regression test for the float-pagination
// fix: a float inside a section forced onto a later page must paint on THAT page, not
// ride page 0 (the prior deferral). A tall block fills page 0; a second block, forced
// onto page 1 by break-before, contains a left float with a unique background color.
// The float's background must appear on page 1 — and NOT on page 0 — and at a small
// page-LOCAL Y (it is shifted into page 1's frame, not left at its full-document Y).
func TestPaginateDistributesFloatToItsPage(t *testing.T) {
	const w = 400
	floatColor := color.RGBA{0xcc, 0x33, 0x33, 0xff}
	src := `<html><body>` +
		`<div style="height:60px;margin:0">page zero</div>` +
		`<div style="margin:0;break-before:page">` +
		`<div style="float:left;width:50px;height:50px;margin:0;background:#cc3333"></div>` +
		`<p style="margin:0">beside the float</p>` +
		`</div></body></html>`

	blocks := measuredBlockHeights(t, src, w)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 top-level blocks, got %d", len(blocks))
	}
	pageH := blocks[0].H + blocks[1].H + 100 // fits both; only the forced break splits

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, pageH)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages.Pages))
	}
	if hasBackgroundColor(pages.Pages[0], floatColor) {
		t.Error("float background painted on page 0; it must be distributed to its section's page")
	}
	if !hasBackgroundColor(pages.Pages[1], floatColor) {
		t.Fatal("float background missing from page 1 (the float was dropped or stranded on page 0)")
	}
	// The float must be shifted into page 1's local frame: its Y is near the page top,
	// not at its full-document Y (which is past the first page's height).
	var floatY float64 = -1
	for _, it := range pages.Pages[1].Items {
		if it.Kind == layout.BackgroundKind && it.Rule.Color == floatColor {
			floatY = it.Rule.YPt
			break
		}
	}
	if floatY < 0 || floatY > pageH/2 {
		t.Errorf("float page-local Y = %.2f, want a small value near the page top (shifted into page 1's frame)", floatY)
	}
}

func TestPaginateSingleBlockFits(t *testing.T) {
	const w = 400
	src := `<html><body><div style="height:50px;margin:0">x</div></body></html>`

	blocks := measuredBlockHeights(t, src, w)
	pageH := blocks[0].H + 500 // way more than needed

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, pageH)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages.Pages))
	}
	if pages.Pages[0].HeightPt != pageH {
		t.Errorf("page HeightPt = %.2f, want %.2f", pages.Pages[0].HeightPt, pageH)
	}
}

func TestPaginateOverTallBlock(t *testing.T) {
	const w = 400
	src := `<html><body><div style="height:500px;margin:0">x</div></body></html>`

	blocks := measuredBlockHeights(t, src, w)
	pageH := blocks[0].H / 2 // block is twice as tall as the page

	var logs []string
	logf := func(format string, args ...any) {
		logs = append(logs, format)
	}

	root := buildRoot(t, src, logf)
	pages, err := New(nil, nil, logf).LayoutPaged(context.Background(), root, w, pageH)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 1 {
		t.Fatalf("over-tall block must stay on 1 page (no phantom page), got %d", len(pages.Pages))
	}
	if len(pages.Pages[0].Items) == 0 {
		t.Errorf("over-tall page should still flatten to items")
	}
	found := false
	for _, l := range logs {
		if strings.Contains(l, "taller than page") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a 'taller than page' log, got %v", logs)
	}
}

// countBackgrounds returns how many BackgroundKind items on the page have color c.
func countBackgrounds(p layout.Page, c color.RGBA) int {
	n := 0
	for i := range p.Items {
		if p.Items[i].Kind == layout.BackgroundKind && p.Items[i].Rule.Color == c {
			n++
		}
	}
	return n
}

// TestPaginatePageCBAbsStaysOnItsBandPage: a page-CB absolute box near the TOP (top:0)
// belongs to page 0's band, so it must paint on page 0 — exactly once, not duplicated onto
// later pages. (The distribution-by-Y onto a LATER page is covered by
// TestPaginateAbsPageCBDistributesByY.)
func TestPaginatePageCBAbsStaysOnItsBandPage(t *testing.T) {
	const w = 400
	absColor := color.RGBA{7, 7, 7, 255}
	// An absolute box with a distinctive background, plus three tall in-flow blocks
	// that span two pages.
	src := `<html><body>` +
		`<div style="position:absolute;top:0;left:0;width:10px;height:10px;background-color:rgb(7,7,7)"></div>` +
		`<div style="height:100px;margin:0">a</div>` +
		`<div style="height:100px;margin:0">b</div>` +
		`<div style="height:100px;margin:0">c</div>` +
		`</body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, 250)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages.Pages))
	}
	if got := countBackgrounds(pages.Pages[0], absColor); got != 1 {
		t.Errorf("page 0 should paint the absolute box once, got %d", got)
	}
	if got := countBackgrounds(pages.Pages[1], absColor); got != 0 {
		t.Errorf("page 1 must NOT duplicate the absolute box, got %d", got)
	}
}

// TestPaginateAbsPageCBDistributesByY pins item 1 (abs distribution): a page-CB absolute
// box whose top falls in a LATER page's band must paint on THAT page (shifted to its local
// Y), not ride page 0. With pageH=250 and three 200pt blocks each block gets its own page
// (two 200pt blocks would be 400 > 250), so the bands are page0 [0,200), page1 [200,400),
// page2 [400,…). top:300px lands in page 1, at local Y = 300 - 200 = 100. Mutation-verify:
// revert the abs branch in splitPositionedByPage (assign page 0, no shift) and the box
// paints on page 0 at Y ≈ 300 instead.
func TestPaginateAbsPageCBDistributesByY(t *testing.T) {
	const w = 400
	const pageH = 250
	absColor := color.RGBA{7, 7, 7, 255}
	src := `<html><body style="margin:0">` +
		`<div style="position:absolute;top:300px;left:0;width:10px;height:10px;background-color:rgb(7,7,7)"></div>` +
		`<div style="height:200px;margin:0">a</div>` +
		`<div style="height:200px;margin:0">b</div>` +
		`<div style="height:200px;margin:0">c</div>` +
		`</body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, pageH)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(pages.Pages))
	}
	// Not on page 0, not on page 2.
	if got := countBackgrounds(pages.Pages[0], absColor); got != 0 {
		t.Errorf("page 0 must NOT paint the abs box (it belongs on page 1), got %d", got)
	}
	if got := countBackgrounds(pages.Pages[2], absColor); got != 0 {
		t.Errorf("page 2 must NOT paint the abs box, got %d", got)
	}
	// Once on page 1, at local Y = 300 - page-1 top (200) = 100.
	ys := backgroundsY(pages.Pages[1], absColor)
	if len(ys) != 1 {
		t.Fatalf("page 1 must paint the abs box exactly once, got Ys %v", ys)
	}
	if ys[0] < 95 || ys[0] > 105 {
		t.Errorf("abs box painted at Y=%.1f on page 1, want ~100 (300 - page-1 top 200)", ys[0])
	}
}

// TestPaginateFixedRepeatsOnEveryPage pins item 1 (fixed repeat): a position:fixed box is
// positioned against the viewport, so it must paint on EVERY page at the same on-page
// coordinates. Here a fixed box at top:10px with three 200pt blocks ⇒ 3 pages, and the box
// paints once per page, each at local Y ≈ 10. Mutation-verify: revert the fixed branch
// (route by Y like abs) and it paints on page 0 only.
func TestPaginateFixedRepeatsOnEveryPage(t *testing.T) {
	const w = 400
	const pageH = 250
	fixColor := color.RGBA{3, 5, 7, 255}
	src := `<html><body style="margin:0">` +
		`<div style="position:fixed;top:10px;left:0;width:10px;height:10px;background-color:rgb(3,5,7)"></div>` +
		`<div style="height:200px;margin:0">a</div>` +
		`<div style="height:200px;margin:0">b</div>` +
		`<div style="height:200px;margin:0">c</div>` +
		`</body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, pageH)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(pages.Pages))
	}
	for i := range pages.Pages {
		ys := backgroundsY(pages.Pages[i], fixColor)
		if len(ys) != 1 {
			t.Fatalf("page %d must paint the fixed box exactly once, got Ys %v", i, ys)
		}
		if ys[0] < 5 || ys[0] > 15 {
			t.Errorf("fixed box on page %d painted at Y=%.1f, want ~10 (same on-page Y every page)", i, ys[0])
		}
	}
}

// TestPaginateFixedWithChildRepeatsIdentically checks that a fixed box's whole SUBTREE
// repeats per page: the fixed box and a nested child both paint at their viewport-relative Y
// on every page. Because a fixed box is positioned against the viewport (its frag.Y is
// already page-local), the same read-only fragment is shared across pages without a shift,
// so every page shows the identical box + child.
func TestPaginateFixedWithChildRepeatsIdentically(t *testing.T) {
	const w = 400
	const pageH = 250
	outerColor := color.RGBA{11, 13, 17, 255}
	innerColor := color.RGBA{19, 23, 29, 255}
	src := `<html><body style="margin:0">` +
		`<div style="position:fixed;top:10px;left:0;width:40px;height:40px;background-color:rgb(11,13,17)">` +
		`<div style="height:10px;margin:5px;background-color:rgb(19,23,29)"></div>` +
		`</div>` +
		`<div style="height:200px;margin:0">a</div>` +
		`<div style="height:200px;margin:0">b</div>` +
		`</body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, pageH)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages.Pages))
	}
	for i := range pages.Pages {
		outer := backgroundsY(pages.Pages[i], outerColor)
		if len(outer) != 1 || outer[0] < 5 || outer[0] > 15 {
			t.Errorf("page %d outer fixed box Ys=%v, want one at ~10", i, outer)
		}
		inner := backgroundsY(pages.Pages[i], innerColor)
		// Inner sits at fixed-box top (10) + its own 5px margin = ~15.
		if len(inner) != 1 || inner[0] < 10 || inner[0] > 20 {
			t.Errorf("page %d inner child Ys=%v, want one at ~15 (the fixed subtree repeats identically)", i, inner)
		}
	}
}

// TestPaginateRelativeBlockPaginatesNormally pins the fix for the holistic-review
// bug: a top-level position:relative block is in flow (so it is bucketed) but is also
// lifted into the positioned layer for painting. Its positioned entry must be routed
// to the page its block landed on — otherwise the block vanishes from its real page
// and ghosts on page 0. Here the relative block lands on page 1 (the first block fills
// page 0), so its background must appear on page 1 and NOT on page 0.
func TestPaginateRelativeBlockPaginatesNormally(t *testing.T) {
	const w = 400
	relColor := color.RGBA{180, 40, 40, 255}
	src := `<html><body>` +
		`<div style="height:200px;margin:0;background-color:rgb(120,120,120)">a</div>` +
		`<div style="height:60px;margin:0;position:relative;background-color:rgb(180,40,40)">rel</div>` +
		`</body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, 250)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("expected 2 pages (block a fills page 0, rel block on page 1), got %d", len(pages.Pages))
	}
	if got := countBackgrounds(pages.Pages[0], relColor); got != 0 {
		t.Errorf("page 0 must NOT ghost the relative block, got %d", got)
	}
	if got := countBackgrounds(pages.Pages[1], relColor); got != 1 {
		t.Errorf("page 1 must paint the relative block once, got %d", got)
	}
}

// backgroundsY returns the YPt of every BackgroundKind item with color c on the page.
func backgroundsY(p layout.Page, c color.RGBA) []float64 {
	var ys []float64
	for i := range p.Items {
		if p.Items[i].Kind == layout.BackgroundKind && p.Items[i].Rule.Color == c {
			ys = append(ys, p.Items[i].Rule.YPt)
		}
	}
	return ys
}

// countBorders returns how many BorderKind items on the page have color c.
func countBorders(p layout.Page, c color.RGBA) int {
	n := 0
	for i := range p.Items {
		if p.Items[i].Kind == layout.BorderKind && p.Items[i].Border.Color == c {
			n++
		}
	}
	return n
}

// borderEdgeOnPage reports whether the page has a BorderKind item of the given Side and
// color whose strip intersects the visible page band [0, pageH).
func borderEdgeOnPage(p layout.Page, side layout.EdgeSide, c color.RGBA, pageH float64) bool {
	for i := range p.Items {
		if p.Items[i].Kind != layout.BorderKind {
			continue
		}
		b := p.Items[i].Border
		if b.Side != side || b.Color != c {
			continue
		}
		// Visible if the strip's Y-extent overlaps [0, pageH).
		if b.YPt < pageH && b.YPt+b.HPt > 0 {
			return true
		}
	}
	return false
}

// minBorderY returns the smallest YPt among BorderKind items of color c (math.Inf if none).
func minBorderY(p layout.Page, c color.RGBA) float64 {
	min := math.Inf(1)
	for i := range p.Items {
		if p.Items[i].Kind == layout.BorderKind && p.Items[i].Border.Color == c {
			if p.Items[i].Border.YPt < min {
				min = p.Items[i].Border.YPt
			}
		}
	}
	return min
}

// TestPaginateAbsChildOfRelativeBlockLandsAtCorrectY is the C1 regression test: an
// absolutely-positioned descendant of a top-level position:relative block must follow
// that block to its real page AND land at the correct local Y (the relative block is
// shifted to local Y 0, so an abs child at top:5px paints at ~5pt). The bug was that
// shiftFragment did not recurse Positioned, leaving the abs child at its page-space Y
// (~205pt). Mutation-verify: revert shiftFragmentExtras' Positioned recursion and this
// FAILS (the abs background lands at ~205, not ~5). The existing relative-block test
// omits a nested abs child, so it never exercised this path.
func TestPaginateAbsChildOfRelativeBlockLandsAtCorrectY(t *testing.T) {
	const w = 400
	absColor := color.RGBA{7, 7, 7, 255}
	src := `<html><body style="margin:0">` +
		`<div style="height:200px;margin:0">filler</div>` +
		`<div style="height:100px;margin:0;position:relative">` +
		`<div style="position:absolute;top:5px;left:5px;width:20px;height:20px;background-color:rgb(7,7,7)"></div>` +
		`</div></body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, 250)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("expected 2 pages (filler fills page 0, relative block on page 1), got %d", len(pages.Pages))
	}
	// The abs child rides its relative block onto page 1 and must NOT appear on page 0.
	if ys := backgroundsY(pages.Pages[0], absColor); len(ys) != 0 {
		t.Errorf("page 0 must not paint the abs child, got Ys %v", ys)
	}
	ys := backgroundsY(pages.Pages[1], absColor)
	if len(ys) != 1 {
		t.Fatalf("page 1 must paint the abs child exactly once, got Ys %v", ys)
	}
	// top:5px relative to the relative block, which is at local Y 0 on page 1 => ~5pt.
	if ys[0] < 0 || ys[0] > 20 {
		t.Errorf("abs child painted at Y=%.1f on page 1, want ~5pt (top of page); the C1 bug put it at ~205pt", ys[0])
	}
}

// TestCollapsedTableGridLinesHugCellsBelowContent pins a PRE-EXISTING (non-pagination)
// bug uncovered while fixing C2: a border-collapse:collapse table placed below other
// content emitted its grid lines (Collapsed strips) at table-LOCAL coordinates (~Y 0),
// detached from its cells (~Y 50) — because shiftFragment moved the table fragment and
// its cells but NOT the Collapsed strips. The existing html-table-collapse golden masked
// it (its table is flush at the top, where the shift is ~0). Mutation-verify: revert the
// Collapsed loop in shiftFragmentExtras and the strips' minimum Y stays ~0 while the
// cells are at ~50, so this FAILS.
func TestCollapsedTableGridLinesHugCellsBelowContent(t *testing.T) {
	const w = 300
	borderColor := color.RGBA{0xc0, 0, 0, 255}
	src := `<html><head><style>td{border:3px solid rgb(192,0,0);padding:6px}` +
		`table{border-collapse:collapse}</style></head><body style="margin:0">` +
		`<div style="height:50px;margin:0">filler</div>` +
		`<table><tr><td>a</td><td>b</td></tr><tr><td>c</td><td>d</td></tr></table></body></html>`

	root := buildRoot(t, src, nil)
	frag := New(nil, nil, nil).layoutTree(context.Background(), root, w)
	if frag == nil {
		t.Fatal("layoutTree returned nil")
	}
	page := frag.Page(w, frag.Y+frag.H)
	if n := countBorders(page, borderColor); n == 0 {
		t.Fatalf("expected collapsed grid lines, got 0")
	}
	// The filler is 50pt tall, so the table (and thus its grid lines) starts at Y >= 50.
	// The bug left the strips at table-local Y (~0), well above the cells.
	minY := minBorderY(page, borderColor)
	if minY < 45 {
		t.Errorf("collapsed grid lines start at Y=%.1f, want >= ~50 (below the 50pt filler); the bug left them at ~0pt detached from the cells", minY)
	}
}

// TestClipRectFollowsBoxBelowContent pins a second PRE-EXISTING (non-pagination) bug
// uncovered while fixing C2: an overflow:hidden box placed below other content clipped
// its content to a rect at the wrong Y (the box's build-frame origin ~0, not its final
// position ~50) — because shiftFragment moved the box fragment but NOT its ClipRect. The
// existing html-overflow-hidden golden masked it (its box is flush at the top). Mutation-
// verify: revert the ClipRect lines in shiftFragmentExtras and the emitted ClipPush rect
// stays at Y~0 while the box is at Y~50, so this FAILS.
func TestClipRectFollowsBoxBelowContent(t *testing.T) {
	const w = 240
	src := `<html><body style="margin:0">` +
		`<div style="height:50px;margin:0">filler</div>` +
		`<div style="height:40px;overflow:hidden;margin:0">` +
		`<div style="height:200px;margin:0">tall</div>` +
		`</div></body></html>`

	root := buildRoot(t, src, nil)
	frag := New(nil, nil, nil).layoutTree(context.Background(), root, w)
	if frag == nil {
		t.Fatal("layoutTree returned nil")
	}
	page := frag.Page(w, frag.Y+frag.H)
	var clipY float64 = -1
	for i := range page.Items {
		if page.Items[i].Kind == layout.ClipPushKind {
			clipY = page.Items[i].Rule.YPt
			break
		}
	}
	if clipY < 0 {
		t.Fatal("expected a ClipPush item for the overflow:hidden box")
	}
	// The overflow box sits below the 50pt filler, so its clip rect must be at Y ~50.
	if clipY < 45 {
		t.Errorf("ClipPush rect at Y=%.1f, want ~50 (below the 50pt filler); the bug left it at ~0pt", clipY)
	}
}

// TestPaginateRelativeOverflowAbsClipFollowsToPage is a C2 regression combining a
// position:relative + overflow:hidden top-level block (paginated to a later page) that
// itself contains an absolutely-positioned descendant. The relative+overflow box's clip
// rect and its abs child must both follow to the page the block lands on, at local
// coordinates. The bug class: shiftFragment shifted neither ClipRect nor Positioned.
func TestPaginateRelativeOverflowAbsClipFollowsToPage(t *testing.T) {
	const w = 400
	absColor := color.RGBA{9, 9, 9, 255}
	src := `<html><body style="margin:0">` +
		`<div style="height:200px;margin:0">filler</div>` +
		`<div style="height:100px;margin:0;position:relative;overflow:hidden">` +
		`<div style="position:absolute;top:8px;left:8px;width:20px;height:20px;background-color:rgb(9,9,9)"></div>` +
		`</div></body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, 250)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages.Pages))
	}
	// The abs child is a CB-owned descendant of the overflow box; it must paint on page 1
	// inside the clip, near the top (top:8px against the block at local Y 0).
	ys := backgroundsY(pages.Pages[1], absColor)
	if len(ys) != 1 {
		t.Fatalf("page 1 must paint the abs child once, got Ys %v", ys)
	}
	if ys[0] < 0 || ys[0] > 20 {
		t.Errorf("abs child painted at Y=%.1f on page 1, want ~8pt; the bug put it at ~208pt", ys[0])
	}
}

// TestPaginateNestedForcedBreakBeforePropagates pins the bundle's item 2: a forced
// break-before on content at the LEADING edge of a top-level block (a nested div that is
// the block's first in-flow content) is PROPAGATED to that top-level block, so the block
// starts a new page. Here the second top-level block's only child carries
// break-before:page; with a page tall enough that nothing else would split, the propagated
// break splits the document into 2 pages. Mutation-verify: drop leadingEdgeForcedBreakBefore
// from effectiveBreaks and this yields 1 page.
func TestPaginateNestedForcedBreakBeforePropagates(t *testing.T) {
	const w = 400
	src := `<html><body style="margin:0">` +
		`<div style="height:20px;margin:0">a</div>` +
		`<div style="margin:0"><div style="height:20px;margin:0;break-before:page">b</div></div>` +
		`</body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, 1000)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Errorf("a leading-edge nested forced break-before must propagate and split: got %d pages, want 2", len(pages.Pages))
	}
}

// TestPaginateNestedForcedBreakAfterPropagates is the break-after mirror: a forced
// break-after on content at the TRAILING edge of a top-level block (a nested div that is
// the block's last in-flow content) propagates to the block, so the NEXT top-level block
// starts a new page. Mutation-verify: drop trailingEdgeForcedBreakAfter and this yields 1
// page.
func TestPaginateNestedForcedBreakAfterPropagates(t *testing.T) {
	const w = 400
	src := `<html><body style="margin:0">` +
		`<div style="margin:0"><div style="height:20px;margin:0;break-after:page">a</div></div>` +
		`<div style="height:20px;margin:0">b</div>` +
		`</body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, 1000)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Errorf("a trailing-edge nested forced break-after must propagate and split: got %d pages, want 2", len(pages.Pages))
	}
}

// TestPaginateMidBlockForcedBreakStillWarns keeps the honest deferral for the genuinely
// hard case: a forced break on content in the MIDDLE of a top-level block (neither the
// first nor last in-flow child) cannot be honored without splitting the block (mid-box
// fragmentation, out of scope). It must NOT split AND must log a one-time mid-block
// warning. Here a wrapper holds two children; the SECOND has break-before:page (not the
// leading edge), so it is mid-block.
func TestPaginateMidBlockForcedBreakStillWarns(t *testing.T) {
	const w = 400
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, format) }

	src := `<html><body style="margin:0">` +
		`<div style="margin:0">` +
		`<div style="height:20px;margin:0">first</div>` +
		`<div style="height:20px;margin:0;break-before:page">second</div>` +
		`</div></body></html>`

	root := buildRoot(t, src, logf)
	pages, err := New(nil, nil, logf).LayoutPaged(context.Background(), root, w, 1000)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 1 {
		t.Errorf("a mid-block forced break must NOT split (bounded scope): got %d pages, want 1", len(pages.Pages))
	}
	found := false
	for _, l := range logs {
		if strings.Contains(l, "mid-block") && strings.Contains(l, "not honored") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a one-time mid-block forced-break warning, got logs %v", logs)
	}
}

// TestPaginateTopLevelForcedBreakDoesNotWarn guards the C3 warning against false
// positives: a forced break on a TOP-LEVEL block is honored (splits) and must NOT log
// the nested-break warning.
func TestPaginateTopLevelForcedBreakDoesNotWarn(t *testing.T) {
	const w = 400
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, format) }

	src := `<html><body style="margin:0">` +
		`<div style="height:20px;margin:0">a</div>` +
		`<div style="height:20px;margin:0;break-before:page">b</div>` +
		`</body></html>`

	root := buildRoot(t, src, logf)
	pages, err := New(nil, nil, logf).LayoutPaged(context.Background(), root, w, 1000)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Errorf("top-level forced break must split: got %d pages, want 2", len(pages.Pages))
	}
	for _, l := range logs {
		if strings.Contains(l, "nested") {
			t.Errorf("a top-level forced break must not emit the nested-break warning, got %q", l)
		}
	}
}

// TestPaginateNestedRelativeBlockFollowsToPage pins F-B (adversarial-review finding): a
// position:relative block NESTED under a static wrapper, whose in-flow position lands on
// a later page, must paint on THAT page — not vanish. The bug was that splitPositionedByPage
// only routed top-level bucket blocks, so a nested relative entry (bubbled to root.Positioned
// but shifted to a later page's local Y via its ancestor's subtree) painted on page 0 at an
// off-page Y and disappeared. Mutation-verify: revert splitPositionedByPage to map only the
// top-level blocks (drop the subtree mark) and the block paints on neither page.
func TestPaginateNestedRelativeBlockFollowsToPage(t *testing.T) {
	const w = 400
	relColor := color.RGBA{180, 40, 40, 255}
	// A tall filler fills page 0; a static wrapper holds a nested relative block that lands
	// on page 1.
	src := `<html><body style="margin:0">` +
		`<div style="height:300px;margin:0">filler</div>` +
		`<div style="margin:0"><div style="height:60px;margin:0;position:relative;background-color:rgb(180,40,40)">nested-rel</div></div>` +
		`</body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, 250)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("expected 2 pages (filler page 0, nested-rel on page 1), got %d", len(pages.Pages))
	}
	if got := countBackgrounds(pages.Pages[0], relColor); got != 0 {
		t.Errorf("page 0 must NOT ghost the nested relative block, got %d", got)
	}
	ys := backgroundsY(pages.Pages[1], relColor)
	if len(ys) != 1 {
		t.Fatalf("page 1 must paint the nested relative block exactly once (it must not vanish), got Ys %v", ys)
	}
	// On page 1 the block sits at its local Y (filler is 300, page 0 is 250 tall, so the
	// wrapper+block start at page-space ~300 → local ~50 on page 1). Must be on-page (< 250).
	if ys[0] < 0 || ys[0] >= 250 {
		t.Errorf("nested relative block painted at Y=%.1f on page 1, want an on-page Y in [0,250)", ys[0])
	}
}

// TestPaginateBodyBorderFragments pins C4: the html/body border must FRAGMENT across
// pages — the TOP edge only on page 0, the BOTTOM edge only on the last page, and the
// LEFT/RIGHT side edges on every page (clipped to the band). The bug was that the
// per-page wrapper clone kept full-document geometry, so the body border painted
// identically on every page (a spurious top edge at Y 0 on pages ≥1, full-height sides).
// Mutation-verify: remove the shiftFragmentSelf(&pageBody,...) call and the top edge
// reappears on pages ≥1, so this FAILS.
func TestPaginateBodyBorderFragments(t *testing.T) {
	const w = 400
	const pageH = 250
	blue := color.RGBA{0, 0, 255, 255}
	// A body with a 10px blue border and three 200pt blocks => 3 pages.
	src := `<html><body style="border:10px solid rgb(0,0,255);margin:0">` +
		`<div style="height:200px;margin:0">a</div>` +
		`<div style="height:200px;margin:0">b</div>` +
		`<div style="height:200px;margin:0">c</div>` +
		`</body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, pageH)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(pages.Pages))
	}
	last := len(pages.Pages) - 1

	// Top edge: ON page 0, OFF every later page.
	if !borderEdgeOnPage(pages.Pages[0], layout.EdgeTop, blue, pageH) {
		t.Errorf("page 0 must paint the body TOP border")
	}
	for i := 1; i < len(pages.Pages); i++ {
		if borderEdgeOnPage(pages.Pages[i], layout.EdgeTop, blue, pageH) {
			t.Errorf("page %d must NOT paint the body top border (it belongs on page 0 only)", i)
		}
	}
	// Bottom edge: OFF every page but the last, ON the last.
	for i := 0; i < last; i++ {
		if borderEdgeOnPage(pages.Pages[i], layout.EdgeBottom, blue, pageH) {
			t.Errorf("page %d must NOT paint the body bottom border (it belongs on the last page)", i)
		}
	}
	if !borderEdgeOnPage(pages.Pages[last], layout.EdgeBottom, blue, pageH) {
		t.Errorf("last page must paint the body BOTTOM border")
	}
	// Side edges: ON every page.
	for i := range pages.Pages {
		if !borderEdgeOnPage(pages.Pages[i], layout.EdgeLeft, blue, pageH) {
			t.Errorf("page %d must paint the body LEFT border (sides span every page)", i)
		}
		if !borderEdgeOnPage(pages.Pages[i], layout.EdgeRight, blue, pageH) {
			t.Errorf("page %d must paint the body RIGHT border (sides span every page)", i)
		}
	}
}

func TestLayoutPagedZeroHeightIsSingleTall(t *testing.T) {
	const w = 400
	src := threeBlocks(100)

	root1 := buildRoot(t, src, nil)
	paged, err := New(nil, nil, nil).LayoutPaged(context.Background(), root1, w, 0)
	if err != nil {
		t.Fatalf("LayoutPaged(...,0): %v", err)
	}
	root2 := buildRoot(t, src, nil)
	single, err := New(nil, nil, nil).Layout(context.Background(), root2, w)
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}

	if len(paged.Pages) != 1 {
		t.Fatalf("pageH<=0 must yield 1 page, got %d", len(paged.Pages))
	}
	if len(single.Pages) != 1 {
		t.Fatalf("Layout must yield 1 page, got %d", len(single.Pages))
	}
	if paged.Pages[0].HeightPt != single.Pages[0].HeightPt {
		t.Errorf("pageH<=0 height = %.2f, want content height %.2f (same as Layout)",
			paged.Pages[0].HeightPt, single.Pages[0].HeightPt)
	}
	if paged.Pages[0].WidthPt != single.Pages[0].WidthPt {
		t.Errorf("pageH<=0 width = %.2f, want %.2f", paged.Pages[0].WidthPt, single.Pages[0].WidthPt)
	}
}

func TestIsForcedBreak(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"page", true},
		{"always", true},
		{"left", true},
		{"right", true},
		{"recto", true},
		{"verso", true},
		{"auto", false},
		{"avoid", false},
		{"avoid-page", false},
		{"", false},
		{"junk", false},
	}
	for _, c := range cases {
		if got := isForcedBreak(c.v); got != c.want {
			t.Errorf("isForcedBreak(%q) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestBucketBlocks(t *testing.T) {
	nolog := func(string, ...any) {}

	// Build raw fragments with explicit Y/H and optional break styles.
	mk := func(y, h float64, before, after string) *Fragment {
		f := &Fragment{Y: y, H: h}
		if before != "" || after != "" {
			f.Box = &cssbox.Box{}
			f.Box.Style.BreakBefore = before
			f.Box.Style.BreakAfter = after
		}
		return f
	}

	t.Run("empty input", func(t *testing.T) {
		got := bucketBlocks(nil, 100, 0, nolog)
		if len(got) != 1 || len(got[0].blocks) != 0 || got[0].top != 0 {
			t.Fatalf("empty input should yield one empty bucket{top:0}, got %+v", got)
		}
	})

	t.Run("plain overflow split", func(t *testing.T) {
		b0 := mk(0, 60, "", "")
		b1 := mk(60, 60, "", "")  // 0..120 > 100 => new page
		b2 := mk(120, 30, "", "") // on page 1: 120..150, page 1 top=60 => 90 <= 100 ok
		got := bucketBlocks([]*Fragment{b0, b1, b2}, 100, 0, nolog)
		if len(got) != 2 {
			t.Fatalf("expected 2 buckets, got %d", len(got))
		}
		if len(got[0].blocks) != 1 || got[0].blocks[0] != b0 || got[0].top != 0 {
			t.Errorf("bucket 0 wrong: %+v", got[0])
		}
		if len(got[1].blocks) != 2 || got[1].blocks[0] != b1 || got[1].blocks[1] != b2 || got[1].top != 60 {
			t.Errorf("bucket 1 wrong: top=%.0f blocks=%d", got[1].top, len(got[1].blocks))
		}
	})

	t.Run("forced-before", func(t *testing.T) {
		b0 := mk(0, 10, "", "")
		b1 := mk(10, 10, "page", "") // fits, but forced onto a new page
		got := bucketBlocks([]*Fragment{b0, b1}, 1000, 0, nolog)
		if len(got) != 2 {
			t.Fatalf("expected 2 buckets, got %d", len(got))
		}
		if got[1].blocks[0] != b1 || got[1].top != 10 {
			t.Errorf("forced-before bucket wrong: top=%.0f", got[1].top)
		}
	})

	t.Run("forced-after", func(t *testing.T) {
		b0 := mk(0, 10, "", "always") // forces a break after it
		b1 := mk(10, 10, "", "")
		got := bucketBlocks([]*Fragment{b0, b1}, 1000, 0, nolog)
		if len(got) != 2 {
			t.Fatalf("expected 2 buckets, got %d", len(got))
		}
		if got[0].blocks[0] != b0 || len(got[0].blocks) != 1 {
			t.Errorf("bucket 0 should hold only b0")
		}
		if got[1].blocks[0] != b1 || got[1].top != 10 {
			t.Errorf("bucket 1 should hold b1 at top=10, got top=%.0f", got[1].top)
		}
	})

	t.Run("forced-after with a gap below uses the next block's own Y as the page top", func(t *testing.T) {
		// b1 starts at Y=50, well below b0's bottom (10) — e.g. a 40pt top margin. The
		// page top for b1's page must be b1.Y (50), NOT a provisional b0.Y+b0.H (10);
		// otherwise b1 would shift to local Y=40 instead of 0.
		b0 := mk(0, 10, "", "page") // forces a break after it
		b1 := mk(50, 10, "", "")
		got := bucketBlocks([]*Fragment{b0, b1}, 1000, 0, nolog)
		if len(got) != 2 {
			t.Fatalf("expected 2 buckets, got %d", len(got))
		}
		if got[1].top != b1.Y {
			t.Errorf("page 1 top = %.0f, want b1.Y = %.0f (not the previous block's bottom)", got[1].top, b1.Y)
		}
	})

	t.Run("exact fit boundary stays", func(t *testing.T) {
		// Two blocks whose combined bottom == pageH exactly: strict > means they stay.
		b0 := mk(0, 50, "", "")
		b1 := mk(50, 50, "", "") // bottom 100 == pageH 100 => NOT overflow
		got := bucketBlocks([]*Fragment{b0, b1}, 100, 0, nolog)
		if len(got) != 1 {
			t.Fatalf("exact-fit should be 1 page, got %d", len(got))
		}
		if len(got[0].blocks) != 2 {
			t.Errorf("both blocks should be on page 0, got %d", len(got[0].blocks))
		}
	})

	t.Run("forced-before on first block is no leading empty page", func(t *testing.T) {
		b0 := mk(0, 10, "page", "")
		b1 := mk(10, 10, "", "")
		got := bucketBlocks([]*Fragment{b0, b1}, 1000, 0, nolog)
		if len(got) != 1 {
			t.Fatalf("forced-before on first block must not emit a leading empty page; got %d buckets", len(got))
		}
		if len(got[0].blocks) != 2 {
			t.Errorf("both blocks should be on the single page, got %d", len(got[0].blocks))
		}
	})

	t.Run("forced-after on last block is no trailing empty page", func(t *testing.T) {
		b0 := mk(0, 10, "", "")
		b1 := mk(10, 10, "", "page")
		got := bucketBlocks([]*Fragment{b0, b1}, 1000, 0, nolog)
		if len(got) != 1 {
			t.Fatalf("forced-after on last block must not emit a trailing empty page; got %d buckets", len(got))
		}
	})
}

// mkWithMargin builds a fragment with explicit Y/H and a used top margin (a Box carrying
// MarginTop in px), so the bucketBlocks margin-retention path can be exercised in isolation.
func mkWithMargin(y, h, marginTopPx float64) *Fragment {
	f := &Fragment{Y: y, H: h, Box: &cssbox.Box{}}
	f.Box.Style.MarginTop = gcss.Length{Value: marginTopPx, Unit: gcss.UnitPx}
	return f
}

// TestBucketBlocksRetainsMarginAtUnforcedBreak pins item 3: at an UNFORCED (overflow) break,
// the block's own leading top margin is RETAINED — the new page's top is pulled up by the
// margin so the block lands at local Y == its margin (not flush at 0). b1 has a 30pt top
// margin and overflows to page 1; page 1's top must be b1.Y - 30. Mutation-verify: drop the
// `cur.top = b.Y - usedTopMargin(...)` line and page 1's top is b1.Y (margin collapsed to 0).
func TestBucketBlocksRetainsMarginAtUnforcedBreak(t *testing.T) {
	nolog := func(string, ...any) {}
	const cbWidth = 400
	const marginTop = 30
	// b0 fills most of the page; b1 (with a 30pt top margin) overflows to page 1.
	b0 := &Fragment{Y: 0, H: 80}
	b1 := mkWithMargin(80+marginTop, 40, marginTop) // border-box top = 110
	got := bucketBlocks([]*Fragment{b0, b1}, 100, cbWidth, nolog)
	if len(got) != 2 {
		t.Fatalf("expected 2 buckets (b1 overflows), got %d", len(got))
	}
	wantTop := b1.Y - marginTop // 110 - 30 = 80
	if got[1].top != wantTop {
		t.Errorf("unforced-break page top = %.1f, want b1.Y - marginTop = %.1f (the leading margin must be retained)", got[1].top, wantTop)
	}
}

// TestBucketBlocksForcedBreakTruncatesMargin guards the forced/unforced split for item 3: at
// a FORCED break the leading margin is TRUNCATED (CSS), so the page top stays at the block's
// border-box Y even when it has a top margin. b1 has a 30pt top margin AND break-before:page,
// on a page tall enough that nothing would overflow — only the forced break splits it; its
// page top must be b1.Y (not b1.Y - margin).
func TestBucketBlocksForcedBreakTruncatesMargin(t *testing.T) {
	nolog := func(string, ...any) {}
	const cbWidth = 400
	const marginTop = 30
	b0 := &Fragment{Y: 0, H: 20}
	b1 := mkWithMargin(20+marginTop, 20, marginTop) // border-box top = 50
	b1.Box.Style.BreakBefore = "page"
	got := bucketBlocks([]*Fragment{b0, b1}, 1000, cbWidth, nolog)
	if len(got) != 2 {
		t.Fatalf("expected 2 buckets (forced break), got %d", len(got))
	}
	if got[1].top != b1.Y {
		t.Errorf("forced-break page top = %.1f, want b1.Y = %.1f (margin truncated at a forced break)", got[1].top, b1.Y)
	}
}

// TestPaginateRetainedMarginPaintsBlockDown is the end-to-end companion to the unit test: a
// block that overflows to a new page with a top margin must paint DOWN by that margin on its
// page (local Y ≈ margin), not flush at the top.
func TestPaginateRetainedMarginPaintsBlockDown(t *testing.T) {
	const w = 400
	const pageH = 250
	blockColor := color.RGBA{90, 110, 130, 255}
	// A 200pt filler fills page 0; the second block has a 40px top margin and overflows to
	// page 1, where it must land at local Y ≈ 40.
	src := `<html><body style="margin:0">` +
		`<div style="height:200px;margin:0">filler</div>` +
		`<div style="height:60px;margin-top:40px;margin-bottom:0;margin-left:0;margin-right:0;background-color:rgb(90,110,130)">m</div>` +
		`</body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, pageH)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages.Pages))
	}
	ys := backgroundsY(pages.Pages[1], blockColor)
	if len(ys) != 1 {
		t.Fatalf("page 1 must paint the margined block once, got Ys %v", ys)
	}
	if ys[0] < 35 || ys[0] > 45 {
		t.Errorf("margined block painted at Y=%.1f on page 1, want ~40 (retained top margin)", ys[0])
	}
}
