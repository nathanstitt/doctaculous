package css_test

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	layoutcss "github.com/nathanstitt/doctaculous/pkg/layout/css"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

// TestFloatsPaintInNamedPageRun pins the fix: when a document uses a named @page
// (so pagination goes through paginateRuns), a floated block must still emit its
// paint items. Before the fix, paginateRuns nil'd Floats and never re-attached
// them, so the float — and any image inside it — vanished.
func TestFloatsPaintInNamedPageRun(t *testing.T) {
	src := []byte(`<!DOCTYPE html><html><head><style>
	  @page wide { size: 1200px 800px }
	  .land { page: wide }
	  .fig { float: left; width: 100px; height: 60px; background: #c00 }
	</style></head><body>
	  <section class="land">
	    <div class="fig"></div>
	    <p>text beside the float</p>
	  </section>
	</body></html>`)

	doc, err := html.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	root, faces, pages, running, err := layoutcss.BuildWithFontsPagesRunning(ctx, doc, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := layoutcss.New(layoutfont.NewFaceCacheWithFonts(faces, nil, nil, nil), nil, nil)
	out, err := engine.LayoutPagedDoc(ctx, root, layoutcss.PagedConfig{
		Paged: true, FallbackW: 816, FallbackH: 1056, ExplicitSize: false,
		Pages: pages, Running: running,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasFilledRect(out, 0xcc, 0x00, 0x00) {
		t.Fatal("floated block produced no paint items (float dropped in paginateRuns)")
	}
}

// TestFloatsScopedPerRunSameWidth reproduces the cross-run float-duplication bug and
// pins its fix. Two NON-ADJACENT sections both select the same named wide @page (with a
// default-page section between them), each containing a distinctly-colored float. All
// three sections' top-level floats bubble to one shared page-root BFC, and both wide runs
// resolve to the same @page width — so they share a single cached layout (layoutByWidth[w])
// whose Floats slice holds BOTH colored floats.
//
// Before the fix, floatsForRun consumed that whole shared Floats slice for EACH wide run,
// so the red float (section A) painted once per wide run AND leaked onto section B's pages
// (and vice-versa). After the fix, each float is scoped to its originating run, so each
// color paints exactly once, only on its own section's pages.
func TestFloatsScopedPerRunSameWidth(t *testing.T) {
	src := []byte(`<!DOCTYPE html><html><head><style>
	  @page wide { size: 1200px 800px }
	  .land { page: wide }
	  .figA { float: left; width: 100px; height: 60px; background: #cc0000 }
	  .figB { float: left; width: 100px; height: 60px; background: #00cc00 }
	</style></head><body>
	  <section class="land"><div class="figA"></div><p>section A beside its red float</p></section>
	  <section><p>default-page section between the two wide sections</p></section>
	  <section class="land"><div class="figB"></div><p>section B beside its green float</p></section>
	</body></html>`)

	doc, err := html.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	root, faces, pages, running, err := layoutcss.BuildWithFontsPagesRunning(ctx, doc, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := layoutcss.New(layoutfont.NewFaceCacheWithFonts(faces, nil, nil, nil), nil, nil)
	out, err := engine.LayoutPagedDoc(ctx, root, layoutcss.PagedConfig{
		Paged: true, FallbackW: 816, FallbackH: 1056, ExplicitSize: false,
		Pages: pages, Running: running,
	})
	if err != nil {
		t.Fatal(err)
	}

	redPages := pagesWithFilledRect(out, 0xcc, 0x00, 0x00)
	greenPages := pagesWithFilledRect(out, 0x00, 0xcc, 0x00)

	// Each float paints exactly once across all pages (a float duplicated per run would
	// count 2, not 1).
	if n := countFilledRect(out, 0xcc, 0x00, 0x00); n != 1 {
		t.Errorf("red float (section A) painted %d times across all pages; want exactly 1", n)
	}
	if n := countFilledRect(out, 0x00, 0xcc, 0x00); n != 1 {
		t.Errorf("green float (section B) painted %d times across all pages; want exactly 1", n)
	}

	// The red and green floats must land on DIFFERENT pages (section A vs section B),
	// never sharing a page — i.e. neither color leaks onto the other section's pages.
	if len(redPages) != 1 || len(greenPages) != 1 {
		t.Fatalf("expected each color on exactly one page; red=%v green=%v", redPages, greenPages)
	}
	if redPages[0] == greenPages[0] {
		t.Errorf("red and green floats landed on the same page %d; section floats must not cross runs", redPages[0])
	}
}

// TestFloatRelativeDescendantShiftedOnPage pins the fix for the float-clone alias bug in
// cloneFloatForPage. A position:relative descendant of a float is stored as the SAME
// *Fragment pointer in BOTH the float's Children (painted in flow, skipped at paint) AND
// its Positioned slice (the copy that actually paints); shiftFragmentExtras deliberately
// skips relative Positioned entries because they are already shifted through Children.
//
// The multi-named-page float path (floatsForRun) CLONES each float before shifting it into
// its bucket's local frame. Before the fix the clone split that one aliased pointer into
// two independent clones, so the Positioned-side copy (the painting one) was never shifted
// — it painted at the pre-shift document Y. Here the float lands on a page with a non-zero
// bucket top, so a mis-aliased clone paints the relative descendant far below the page. The
// fix threads an old->new pointer map through the clone so the alias survives; the
// descendant then paints at its correct page-local Y (page top + its relative offset).
func TestFloatRelativeDescendantShiftedOnPage(t *testing.T) {
	src := []byte(`<!DOCTYPE html><html><head><style>
	  @page wide { size: 1200px 300px }
	  .land { page: wide }
	  .b { height: 250px; background: #eeeeee }
	  .fig { float: left; width: 120px; height: 80px; background: #cc0000 }
	  .rel { position: relative; top: 30px; left: 10px; width: 40px; height: 40px; background: #0000cc }
	</style></head><body>
	  <section class="land">
	    <div class="b">block one</div>
	    <div class="b">block two</div>
	    <div class="b">block three</div>
	    <div class="fig"><div class="rel"></div></div>
	    <div class="b">block after</div>
	  </section>
	</body></html>`)

	doc, err := html.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	root, faces, pages, running, err := layoutcss.BuildWithFontsPagesRunning(ctx, doc, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := layoutcss.New(layoutfont.NewFaceCacheWithFonts(faces, nil, nil, nil), nil, nil)
	out, err := engine.LayoutPagedDoc(ctx, root, layoutcss.PagedConfig{
		Paged: true, FallbackW: 816, FallbackH: 1056, ExplicitSize: false,
		Pages: pages, Running: running,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Find the relative descendant's paint item (the blue rect) and the float itself (red).
	// The float lands on a later page (bucket top != 0), so both are shifted into that page's
	// local frame: the red float at Y=0, the blue descendant at Y = float-top + relative-top
	// = 0 + 30. Before the fix the blue rect kept its unshifted document Y (~780), far below
	// the 300px page.
	redItem, redOK := filledRectItem(out, 0xcc, 0x00, 0x00)
	blueItem, blueOK := filledRectItem(out, 0x00, 0x00, 0xcc)
	if !redOK {
		t.Fatal("float (red) produced no paint item")
	}
	if !blueOK {
		t.Fatal("relative descendant (blue) produced no paint item")
	}
	if redItem.page != blueItem.page {
		t.Fatalf("float and its relative descendant painted on different pages: red=%d blue=%d", redItem.page, blueItem.page)
	}
	// The relative descendant must sit just below the float's top (offset 30), i.e. within
	// the page, NOT at its unshifted document Y (which was > the 300px page height).
	if blueItem.y < 0 || blueItem.y > 300 {
		t.Fatalf("relative descendant painted at page-local Y %.1f, outside the 300px page — clone lost the Children<->Positioned alias so the Positioned-side copy was never shifted", blueItem.y)
	}
	if want := redItem.y + 30; blueItem.y != want {
		t.Errorf("relative descendant Y = %.1f; want %.1f (float top %.1f + relative top 30)", blueItem.y, want, redItem.y)
	}
}

// filledRect is a Rule/Background paint item plus the page it was found on.
type filledRect struct {
	page int
	y    float64
}

// filledRectItem returns the first Rule/Background item across all pages whose color
// approximately matches (r,g,b), with the page index it was found on.
func filledRectItem(pages *layout.Pages, r, g, b uint8) (filledRect, bool) {
	near := func(a, want uint8) bool {
		if a > want {
			return a-want < 8
		}
		return want-a < 8
	}
	for pi := range pages.Pages {
		for _, it := range pages.Pages[pi].Items {
			if it.Kind == layout.RuleKind || it.Kind == layout.BackgroundKind {
				c := it.Rule.Color
				if near(c.R, r) && near(c.G, g) && near(c.B, b) {
					return filledRect{page: pi, y: it.Rule.YPt}, true
				}
			}
		}
	}
	return filledRect{}, false
}

// countFilledRect counts Rule/Background items across all pages whose color approximately
// matches (r,g,b).
func countFilledRect(pages *layout.Pages, r, g, b uint8) int {
	near := func(a, want uint8) bool {
		if a > want {
			return a-want < 8
		}
		return want-a < 8
	}
	n := 0
	for pi := range pages.Pages {
		for _, it := range pages.Pages[pi].Items {
			if it.Kind == layout.RuleKind || it.Kind == layout.BackgroundKind {
				c := it.Rule.Color
				if near(c.R, r) && near(c.G, g) && near(c.B, b) {
					n++
				}
			}
		}
	}
	return n
}

// pagesWithFilledRect returns the indices of pages carrying a Rule/Background item whose
// color approximately matches (r,g,b).
func pagesWithFilledRect(pages *layout.Pages, r, g, b uint8) []int {
	near := func(a, want uint8) bool {
		if a > want {
			return a-want < 8
		}
		return want-a < 8
	}
	var out []int
	for pi := range pages.Pages {
		for _, it := range pages.Pages[pi].Items {
			if it.Kind == layout.RuleKind || it.Kind == layout.BackgroundKind {
				c := it.Rule.Color
				if near(c.R, r) && near(c.G, g) && near(c.B, b) {
					out = append(out, pi)
					break
				}
			}
		}
	}
	return out
}

// hasFilledRect reports whether any page carries a Rule/Background item whose color
// approximately matches (r,g,b).
func hasFilledRect(pages *layout.Pages, r, g, b uint8) bool {
	near := func(a, want uint8) bool {
		if a > want {
			return a-want < 8
		}
		return want-a < 8
	}
	for pi := range pages.Pages {
		for _, it := range pages.Pages[pi].Items {
			if it.Kind == layout.RuleKind || it.Kind == layout.BackgroundKind {
				c := it.Rule.Color
				if near(c.R, r) && near(c.G, g) && near(c.B, b) {
					return true
				}
			}
		}
	}
	return false
}
