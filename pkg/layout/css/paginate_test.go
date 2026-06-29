package css

import (
	"context"
	"image/color"
	"testing"

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
	buckets := bucketBlocks(blocks, pageH, func(string, ...any) {})
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
		if containsTallerThanPage(l) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a 'taller than page' log, got %v", logs)
	}
}

func containsTallerThanPage(s string) bool {
	// the message contains the substring "taller than page"
	needle := "taller than page"
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
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

// TestPaginatePositionedFirstPageOnly pins the deferral: an absolute box (which
// rides the root/body wrapper, not the per-page block list) must paint on the FIRST
// page only, not duplicated onto every page. (This slice does not yet distribute
// out-of-flow content across pages.)
func TestPaginatePositionedFirstPageOnly(t *testing.T) {
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
		got := bucketBlocks(nil, 100, nolog)
		if len(got) != 1 || len(got[0].blocks) != 0 || got[0].top != 0 {
			t.Fatalf("empty input should yield one empty bucket{top:0}, got %+v", got)
		}
	})

	t.Run("plain overflow split", func(t *testing.T) {
		b0 := mk(0, 60, "", "")
		b1 := mk(60, 60, "", "")  // 0..120 > 100 => new page
		b2 := mk(120, 30, "", "") // on page 1: 120..150, page 1 top=60 => 90 <= 100 ok
		got := bucketBlocks([]*Fragment{b0, b1, b2}, 100, nolog)
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
		got := bucketBlocks([]*Fragment{b0, b1}, 1000, nolog)
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
		got := bucketBlocks([]*Fragment{b0, b1}, 1000, nolog)
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
		got := bucketBlocks([]*Fragment{b0, b1}, 1000, nolog)
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
		got := bucketBlocks([]*Fragment{b0, b1}, 100, nolog)
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
		got := bucketBlocks([]*Fragment{b0, b1}, 1000, nolog)
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
		got := bucketBlocks([]*Fragment{b0, b1}, 1000, nolog)
		if len(got) != 1 {
			t.Fatalf("forced-after on last block must not emit a trailing empty page; got %d buckets", len(got))
		}
	})
}
