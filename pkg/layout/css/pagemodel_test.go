package css

import (
	"context"
	"image/color"
	"math"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// Test-local geometry constants (the layout package does not import doctaculous/css
// constants; these mirror them). px-as-pt at 96dpi.
const (
	tLetterW = 816.0
	tLetterH = 1056.0
	tA4W     = 210 * 96.0 / 25.4 // A4 width in px@96
	tA4H     = 297 * 96.0 / 25.4
)

// firstBackground returns the first BackgroundKind item with the given color, or nil.
func firstBackground(items []layout.Item, c color.RGBA) *layout.RuleItem {
	for i := range items {
		if items[i].Kind == layout.BackgroundKind && items[i].Rule.Color == c {
			return &items[i].Rule
		}
	}
	return nil
}

// pagedConfigFor builds a PagedConfig from standalone @page CSS (mirroring how the
// backend aggregates @page rules from the document's sheets, but kept explicit here so
// the geometry under test is unambiguous).
func pagedConfigFor(pageCSS string, fallbackW, fallbackH float64, explicit bool) PagedConfig {
	ss := gcss.Parse(pageCSS)
	return PagedConfig{Paged: true, FallbackW: fallbackW, FallbackH: fallbackH, ExplicitSize: explicit, Pages: ss}
}

func TestPagedDocSizeFromAtPage(t *testing.T) {
	// @page size A4 (no WithPageSize): pages must be A4 (794 x 1123 @96), content inset
	// by the 1in margins. The single block (a colored div) paints offset by (1in,1in).
	src := `<html><head><style>
		@page { size: A4; margin: 1in }
	</style></head><body>
		<div style="height:50px;background:rgb(10,20,30);margin:0">x</div>
	</body></html>`
	cfg := pagedConfigFor(`@page { size: A4; margin: 1in }`, tLetterW, tLetterH, false)
	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPagedDoc(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("LayoutPagedDoc: %v", err)
	}
	if len(pages.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages.Pages))
	}
	p := pages.Pages[0]
	wantW, wantH := tA4W, tA4H
	if math.Abs(p.WidthPt-wantW) > 0.5 || math.Abs(p.HeightPt-wantH) > 0.5 {
		t.Errorf("page size = %.1f x %.1f, want A4 %.1f x %.1f", p.WidthPt, p.HeightPt, wantW, wantH)
	}
	bg := firstBackground(p.Items, color.RGBA{10, 20, 30, 255})
	if bg == nil {
		t.Fatalf("colored block background not found on page")
	}
	// The block sits at the content-box origin = (marginLeft, marginTop) = (96, 96).
	if math.Abs(bg.XPt-96) > 1 || math.Abs(bg.YPt-96) > 1 {
		t.Errorf("block painted at (%.1f,%.1f), want inset by 1in margins (96,96)", bg.XPt, bg.YPt)
	}
	// The content width is the page width minus 2in: the block (full-width) is that wide.
	wantContentW := wantW - 192
	if math.Abs(bg.WPt-wantContentW) > 1 {
		t.Errorf("block width = %.1f, want content width %.1f (page - 2in)", bg.WPt, wantContentW)
	}
}

func TestPagedDocExplicitSizeOverridesAtPage(t *testing.T) {
	// WithPageSize (explicit) + @page { size: A4; margin: 1in }: the explicit size wins
	// (Letter), but the @page margins still apply (block inset by 1in).
	src := `<html><head><style>
		@page { size: A4; margin: 1in }
	</style></head><body>
		<div style="height:50px;background:rgb(40,50,60);margin:0">x</div>
	</body></html>`
	cfg := pagedConfigFor(`@page { size: A4; margin: 1in }`, tLetterW, tLetterH, true /* explicit */)
	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPagedDoc(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("LayoutPagedDoc: %v", err)
	}
	p := pages.Pages[0]
	if math.Abs(p.WidthPt-tLetterW) > 0.5 || math.Abs(p.HeightPt-tLetterH) > 0.5 {
		t.Errorf("page size = %.1f x %.1f, want Letter %.0f x %.0f (explicit override)",
			p.WidthPt, p.HeightPt, tLetterW, tLetterH)
	}
	bg := firstBackground(p.Items, color.RGBA{40, 50, 60, 255})
	if bg == nil {
		t.Fatalf("colored block background not found")
	}
	if math.Abs(bg.XPt-96) > 1 || math.Abs(bg.YPt-96) > 1 {
		t.Errorf("block at (%.1f,%.1f), want @page margins still applied (96,96)", bg.XPt, bg.YPt)
	}
}

func TestPagedDocNotPagedIsSingleTall(t *testing.T) {
	// Paged false => delegates to Layout: a single page sized to content height (NOT a
	// fixed page height), even with an @page rule present.
	src := `<html><head><style>@page { size: A4; margin: 1in }</style></head><body>
		<div style="height:50px;margin:0">x</div>
	</body></html>`
	cfg := pagedConfigFor(`@page { size: A4; margin: 1in }`, tLetterW, tLetterH, false)
	cfg.Paged = false
	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPagedDoc(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("LayoutPagedDoc: %v", err)
	}
	if len(pages.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages.Pages))
	}
	// Single-tall: page height is content height, far less than A4's 1123.
	if pages.Pages[0].HeightPt > 500 {
		t.Errorf("not-paged height = %.1f, expected content height (small), not a fixed page", pages.Pages[0].HeightPt)
	}
}

func TestResolvePageGeomBleed(t *testing.T) {
	cfg := pagedConfigFor(`@page { size: 200px 300px; bleed: 10px; marks: crop }`, 200, 300, false)
	g := cfg.resolvePageGeom(0, "", false)
	// Trim box is 200x300; with 10px bleed the MEDIA box (the page bitmap) is 220x320,
	// and the trim box is offset by (10,10).
	if g.mediaW() != 220 || g.mediaH() != 320 {
		t.Errorf("media size = %.0fx%.0f, want 220x320", g.mediaW(), g.mediaH())
	}
	if g.bleed != 10 {
		t.Errorf("bleed = %.1f, want 10", g.bleed)
	}
}

func TestPagedDocBleedShiftsAndMarks(t *testing.T) {
	src := `<html><head><style>
		@page { size: 200px 200px; bleed: 10px; marks: crop }
	</style></head><body>
		<div style="height:120px;margin:0;background:rgb(7,7,7)">x</div>
	</body></html>`
	cfg := pagedConfigFor(`@page { size: 200px 200px; bleed: 10px; marks: crop }`, 200, 200, false)
	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPagedDoc(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("LayoutPagedDoc: %v", err)
	}
	p := pages.Pages[0]
	// The page bitmap is the MEDIA box: 220x220 (trim 200 + 2*10 bleed).
	if p.WidthPt != 220 || p.HeightPt != 220 {
		t.Errorf("page size = %.0fx%.0f, want 220x220 (media box)", p.WidthPt, p.HeightPt)
	}
	// The content block is shifted by (bleed,bleed) = (10,10): its background paints at x≥10.
	bg := firstBackground(p.Items, color.RGBA{7, 7, 7, 255})
	if bg == nil {
		t.Fatalf("content background not found")
	}
	if bg.XPt < 9.5 || bg.YPt < 9.5 {
		t.Errorf("content at (%.1f,%.1f), want shifted by bleed (≥10,≥10)", bg.XPt, bg.YPt)
	}
	// Crop marks present: ≥8 black hairline rules.
	black := color.RGBA{0, 0, 0, 255}
	marks := 0
	for _, it := range p.Items {
		if it.Kind == layout.BackgroundKind && it.Rule.Color == black && it.Rule.HPt <= 1 || (it.Kind == layout.BackgroundKind && it.Rule.Color == black && it.Rule.WPt <= 1) {
			marks++
		}
	}
	if marks < 8 {
		t.Errorf("crop mark rules = %d, want ≥8", marks)
	}
}
