package css

import (
	"context"
	"image/color"
	"math"
	"testing"
)

// TestNamedPagePositionedLayerPaints is a regression test for the multi-named-page
// positioned-layer drop: paginateRuns nil'd the root positioned layer and (unlike the
// float path) never re-attached it per run, so EVERY position:relative / absolute / fixed
// box in a document that uses a named @page painted nowhere. This reproduced as the
// showcase's section-7 ".stage" (a position:relative block with a pinned absolute badge)
// rendering as a blank gap once the document also contained a landscape named-page section.
//
// The document below has two runs — a DEFAULT-page section carrying a position:relative
// block (with a nested position:absolute descendant), then a WIDE named-page section whose
// presence forces paginateRuns instead of the single-run fast path. Both the relative
// block's background and its nested absolute descendant's background must paint, on the
// relative block's page, at page-LOCAL coordinates (inset by the @page margin).
func TestNamedPagePositionedLayerPaints(t *testing.T) {
	relBG := color.RGBA{230, 224, 210, 255}  // the position:relative block
	badgeBG := color.RGBA{217, 165, 33, 255} // its nested position:absolute descendant

	src := `<html><head><style>
		@page { size: 400px 600px; margin: 20px }
		@page wide { size: 700px 600px; margin: 20px }
		.wide { page: wide }
		div, p, section { margin: 0 }
	</style></head><body>
		<section>
			<p style="height:30px">Lede paragraph.</p>
			<div style="position:relative; height:120px; background:#e6e0d2">
				<div style="position:absolute; top:0; left:0; width:40px; height:40px; background:#d9a521"></div>
				<p>This stage is position:relative.</p>
			</div>
			<p>Caption below the stage.</p>
		</section>
		<section class="wide">
			<p>Wide section content.</p>
		</section>
	</body></html>`

	cfg := pagedConfigFor(`
		@page { size: 400px 600px; margin: 20px }
		@page wide { size: 700px 600px; margin: 20px }
	`, 400, 600, false)
	root := buildRoot(t, src, t.Logf)
	pages, err := New(nil, nil, nil).LayoutPagedDoc(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("LayoutPagedDoc: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("want 2 pages (default section + wide section), got %d", len(pages.Pages))
	}

	// The relative block and its nested absolute badge live on page 0 (the default run).
	rel := firstBackground(pages.Pages[0].Items, relBG)
	if rel == nil {
		t.Fatalf("position:relative block background missing on page 0 (the multi-run positioned-layer drop)")
	}
	badge := firstBackground(pages.Pages[0].Items, badgeBG)
	if badge == nil {
		t.Fatalf("nested position:absolute badge missing on page 0 (it must ride its relative ancestor)")
	}

	// Page-LOCAL geometry: the block is inset by the 20px @page margin (X ≈ 20), and its top
	// sits below the 30px lede (Y ≈ 20 margin + 30 lede = 50). The badge is pinned to the
	// block's top-left corner, so it lands at the same origin (X ≈ 20, Y ≈ 50) — proving it
	// was distributed into the page-local frame, not left at full-document coordinates.
	if math.Abs(rel.XPt-20) > 1 {
		t.Errorf("relative block X = %.1f, want ~20 (@page margin inset)", rel.XPt)
	}
	if math.Abs(rel.YPt-50) > 1.5 {
		t.Errorf("relative block Y = %.1f, want ~50 (margin 20 + lede 30)", rel.YPt)
	}
	if math.Abs(badge.XPt-rel.XPt) > 1 || math.Abs(badge.YPt-rel.YPt) > 1 {
		t.Errorf("badge origin = (%.1f,%.1f), want ~ block origin (%.1f,%.1f)",
			badge.XPt, badge.YPt, rel.XPt, rel.YPt)
	}

	// The positioned layer must NOT leak onto the wide page (page 1).
	if firstBackground(pages.Pages[1].Items, relBG) != nil {
		t.Errorf("relative block leaked onto the wide page (page 1)")
	}
}

// TestNamedPagePageCBAbsoluteDistributes checks the out-of-flow branch of the multi-run
// positioned fix: a page-CB position:absolute box (its containing block is the page, not a
// positioned ancestor) is routed to the page whose band holds its top and shifted into that
// page's local frame — the abs analogue of splitPositionedByPage, in the paginateRuns path.
func TestNamedPagePageCBAbsoluteDistributes(t *testing.T) {
	absBG := color.RGBA{40, 74, 90, 255}

	// A page-CB absolute box declared in the default section, pinned top:80/left:30. With no
	// positioned ancestor its CB is the page; it must paint on page 0 at page-local
	// (X ≈ 30+20 margin = 50, Y ≈ 80+20 margin = 100).
	src := `<html><head><style>
		@page { size: 400px 600px; margin: 20px }
		@page wide { size: 700px 600px; margin: 20px }
		.wide { page: wide }
		div, p, section { margin: 0 }
	</style></head><body>
		<section>
			<p style="height:30px">Default section.</p>
			<div style="position:absolute; top:80px; left:30px; width:50px; height:50px; background:#284a5a"></div>
		</section>
		<section class="wide"><p>Wide section.</p></section>
	</body></html>`

	cfg := pagedConfigFor(`
		@page { size: 400px 600px; margin: 20px }
		@page wide { size: 700px 600px; margin: 20px }
	`, 400, 600, false)
	root := buildRoot(t, src, t.Logf)
	pages, err := New(nil, nil, nil).LayoutPagedDoc(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("LayoutPagedDoc: %v", err)
	}
	abs := firstBackground(pages.Pages[0].Items, absBG)
	if abs == nil {
		t.Fatalf("page-CB position:absolute box missing on page 0 (multi-run positioned drop)")
	}
	if math.Abs(abs.XPt-50) > 1 || math.Abs(abs.YPt-100) > 1 {
		t.Errorf("abs box at (%.1f,%.1f), want ~(50,100) (margin 20 + offset 30/80)", abs.XPt, abs.YPt)
	}
}
