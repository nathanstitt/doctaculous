package css_test

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	layoutcss "github.com/nathanstitt/doctaculous/pkg/layout/css"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

// TestFloatVerticalPositionOracle is the reliable oracle recommended by the handoff:
// it reproduces the FLOAT-showcase scenario (a paginated multi-run document — a
// landscape named page forces paginateRuns — with a float placed after a lede that has
// a bottom border + padding + margin), then reads the PAINTED items (final page space,
// one frame) of the lede background and the figure background and compares them.
//
// Per CSS 9.5 the figure's border-box top may not be higher than the lede's margin-box
// bottom (lede border-box bottom + lede margin-bottom). This test prints both rects so
// we can see the discrepancy, and asserts the correct relationship.
func TestFloatVerticalPositionOracle(t *testing.T) {
	// Colors: lede background = #dde0f0 (light blue-gray), figure background = #c04040 (red).
	// Distinct so filledRectItem can find each in the painted stream.
	src := []byte(`<!DOCTYPE html><html><head><style>
	  @page { size: 816px 1056px; margin: 40px }
	  @page landscape { size: 1056px 816px; margin: 40px }
	  .land { page: landscape }
	  body { border: 6px solid #1a1a18; padding: 28px 36px 36px 36px }
	  .section { margin-bottom: 26px }
	  .break { break-before: page }
	  h2 { margin: 0 0 2px 0; font-size: 26px }
	  .kicker { display: inline-block; background: #d9a521; padding: 2px 8px; margin-bottom: 8px }
	  .lede { border-bottom: 1px solid #c9c2b0; padding-bottom: 10px; margin-bottom: 14px; background: #dde0f0 }
	  .figure { float: left; width: 150px; height: 90px; margin: 4px 18px 8px 0; border: 1px solid #1a1a18; padding: 6px; background: #c04040 }
	</style></head><body>
	  <section class="section break"><h2>First</h2><p>intro section on page 0</p></section>
	  <section class="section break">
	    <span class="kicker">03 / FLOAT</span>
	    <h2>Floats &amp; Clear</h2>
	    <p class="lede">A floated figure with text wrapping beside it, then a cleared block.</p>
	    <div class="figure"></div>
	    <p>This paragraph wraps beside the float.</p>
	  </section>
	  <section class="section break land"><p>landscape section to force paginateRuns</p></section>
	</body></html>`)

	out := layoutShowcaseLike(t, src)

	ledeItem, ledeOK := backgroundItem(out, 0xdd, 0xe0, 0xf0)
	figItem, figOK := backgroundItem(out, 0xc0, 0x40, 0x40)
	if !ledeOK {
		t.Fatal("lede background produced no paint item")
	}
	if !figOK {
		t.Fatal("figure background produced no paint item")
	}

	t.Logf("lede background:   page=%d  y=%.2f  h=%.2f  bottom=%.2f", ledeItem.page, ledeItem.y, ledeItem.h, ledeItem.y+ledeItem.h)
	t.Logf("figure background: page=%d  y=%.2f  h=%.2f  bottom=%.2f", figItem.page, figItem.y, figItem.h, figItem.y+figItem.h)

	if ledeItem.page != figItem.page {
		t.Fatalf("lede and figure painted on different pages: lede=%d figure=%d", ledeItem.page, figItem.page)
	}

	// The lede background covers its border box (background-clip defaults to border-box).
	// So lede border-box bottom = ledeItem.y + ledeItem.h. The lede's margin-box bottom is
	// that + margin-bottom(14). The figure's background covers its border box; the figure's
	// border-box top is figItem.y, and its MARGIN-box top is figItem.y - margin-top(4).
	//
	// CSS 9.5: the figure's margin-box top must be >= the lede's margin-box bottom.
	ledeMarginBottom := ledeItem.y + ledeItem.h + 14
	figMarginTop := figItem.y - 4
	t.Logf("lede margin-box bottom = %.2f ; figure margin-box top = %.2f ; gap = %.2f",
		ledeMarginBottom, figMarginTop, figMarginTop-ledeMarginBottom)

	if figMarginTop < ledeMarginBottom-0.5 {
		t.Errorf("figure sits too high: figure margin-box top %.2f is above the lede margin-box bottom %.2f (by %.2f)",
			figMarginTop, ledeMarginBottom, ledeMarginBottom-figMarginTop)
	}
}

// bgRect is a Background paint item plus the page and geometry it was found on.
type bgRect struct {
	page int
	y, h float64
}

// backgroundItem returns the first BackgroundKind item across all pages whose color
// approximately matches (r,g,b), with its page index and Y/height (final page space).
func backgroundItem(pages *layout.Pages, r, g, b uint8) (bgRect, bool) {
	near := func(a, want uint8) bool {
		if a > want {
			return a-want < 8
		}
		return want-a < 8
	}
	for pi := range pages.Pages {
		for _, it := range pages.Pages[pi].Items {
			if it.Kind == layout.BackgroundKind {
				c := it.Rule.Color
				if near(c.R, r) && near(c.G, g) && near(c.B, b) {
					return bgRect{page: pi, y: it.Rule.YPt, h: it.Rule.HPt}, true
				}
			}
		}
	}
	return bgRect{}, false
}

// layoutShowcaseLike parses src and lays it out through the paginated multi-run path
// (LayoutPagedDoc with @page rules), the same pipeline the htmldoc showcase uses.
func layoutShowcaseLike(t *testing.T, src []byte) *layout.Pages {
	t.Helper()
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
	return out
}
