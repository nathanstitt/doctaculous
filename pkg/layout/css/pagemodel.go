package css

import (
	"context"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// defaultMarkInset is the bleed-band width synthesized when @page marks are requested
// without an explicit bleed, so the marks have room outside the trim box (~6pt).
const defaultMarkInset = 8 // px-as-pt (≈6pt)

// PagedConfig is the resolved paged-media configuration handed to LayoutPagedDoc by
// the doctaculous backend: whether to paginate at all, a fallback page size (from
// WithPageSize / the Letter default), an explicit-size flag (so an API size overrides
// an @page size), and the parsed @page rules (for size, margins, and margin boxes).
//
// The fields combine like CSS Paged Media + the API override rule:
//   - Paged false  → a single tall page (LayoutPagedDoc delegates to Layout).
//   - FallbackW/H  → the page size when no @page `size` applies (the WithPageSize
//     value, else Letter).
//   - ExplicitSize → the caller passed WithPageSize: its size wins over any @page
//     `size` (but @page margins/margin-boxes still apply).
//   - Pages        → the document's @page rules (may be empty).
type PagedConfig struct {
	Paged        bool
	FallbackW    float64
	FallbackH    float64
	ExplicitSize bool
	Pages        gcss.Stylesheet
}

// pageGeom is one page's resolved geometry: the full page rect size and the content
// box (origin + size) the page's blocks occupy after the @page margins are applied.
// The margin boxes (running headers/footers) are resolved separately at flatten time.
type pageGeom struct {
	pageW, pageH       float64 // full page size
	marginL, marginT   float64 // content-box origin offset (left/top margins)
	contentW, contentH float64 // content box size (page minus margins)
	used               gcss.UsedPage
	bleed              float64 // @page bleed: the trim→media-box inset on each side (0 = none)
}

// mediaW / mediaH are the page BITMAP size: the trim box (pageW/pageH) plus bleed on
// each side. With no bleed they equal pageW/pageH (byte-identical).
func (g pageGeom) mediaW() float64 { return g.pageW + 2*g.bleed }
func (g pageGeom) mediaH() float64 { return g.pageH + 2*g.bleed }

// marksRequested reports whether @page asked for crop and/or cross marks.
func (g pageGeom) marksRequested() bool { return g.used.Marks != "" }

// resolvePageGeom resolves the geometry for page index i (named `name`) from a
// PagedConfig: the page size (API explicit size > @page size > fallback) and the
// content box after @page margins. blank marks an empty (forced-break) page.
func (pc PagedConfig) resolvePageGeom(i int, name string, blank bool) pageGeom {
	up := pc.Pages.ResolvePage(i, name, blank)
	g := pageGeom{pageW: pc.FallbackW, pageH: pc.FallbackH, used: up}
	if up.HasSize && !pc.ExplicitSize {
		g.pageW, g.pageH = up.WidthPt, up.HeightPt
	}
	if up.HasRule {
		g.marginL, g.marginT = up.MarginLeft, up.MarginTop
		g.contentW = g.pageW - up.MarginLeft - up.MarginRight
		g.contentH = g.pageH - up.MarginTop - up.MarginBottom
	}
	if g.contentW <= 0 {
		g.contentW = g.pageW
	}
	if g.contentH <= 0 {
		g.contentH = g.pageH
	}
	g.bleed = up.Bleed
	if g.bleed == 0 && up.Marks != "" {
		g.bleed = defaultMarkInset // marks need room to draw outside the trim box
	}
	return g
}

// LayoutPagedDoc lays out root paginated per a PagedConfig: it resolves the page size
// and margins from the document's @page rules (combined with any API override) and
// fragments the document into pages, placing each page's content inside the @page
// margin box and emitting the @page margin boxes (running headers/footers) on each
// page.
//
// When cfg.Paged is false it delegates to Layout (a single tall page) — the
// byte-identical path. Otherwise the LAYOUT width is page 0's content-box width (the
// page width minus its horizontal @page margins, or the fallback width when there is
// no @page rule), so existing geometry is preserved while content is inset by the
// margins. It never panics (the same page-boundary recover as Layout).
func (e *Engine) LayoutPagedDoc(ctx context.Context, root *cssbox.Box, cfg PagedConfig) (pages *layout.Pages, err error) {
	if !cfg.Paged {
		return e.Layout(ctx, root, cfg.FallbackW) // single tall page (handles canvas bg)
	}
	defer func() {
		if r := recover(); r != nil {
			e.logf("css layout: recovered from panic: %v", r)
			g := cfg.resolvePageGeom(0, "", false)
			pages = &layout.Pages{Pages: []layout.Page{{WidthPt: g.pageW, HeightPt: g.pageH}}}
			err = nil
		}
	}()

	// CSS background propagation (same as LayoutPaged): lift the root/body background
	// onto every page's canvas and clear it from the source box before layoutTree.
	canvasBG := propagateCanvasBackground(root)
	defer func() {
		if pages != nil {
			pages.CanvasBackground = canvasBG
		}
	}()

	// The layout width is page 0's content-box width: the document lays out once at
	// that width, then each page's blocks are inset by their own @page margins. (Pages
	// almost always share a size; a per-page size change via named pages reflows only
	// its inset, not the layout width — a documented bound.)
	g0 := cfg.resolvePageGeom(0, "", false)
	frag := e.layoutTree(ctx, root, g0.contentW)
	if frag == nil {
		return &layout.Pages{Pages: []layout.Page{{WidthPt: g0.pageW, HeightPt: g0.pageH}}}, nil
	}
	return e.paginateDoc(frag, cfg), nil
}

// paginateDoc fragments the laid-out root fragment into pages whose size and content
// inset come from the document's @page rules (via cfg). It mirrors paginate (same
// between-blocks bucketing, positioned/float distribution, wrapper fragmentation, and —
// once components 3/4 land — keep-together and line splitting) but resolves each page's
// geometry through cfg.resolvePageGeom and emits the @page margin boxes. The content
// height used for bucketing is page 0's content box (page height minus @page vertical
// margins); per-page size variation via named pages adjusts only the inset.
func (e *Engine) paginateDoc(root *Fragment, cfg PagedConfig) *layout.Pages {
	g0 := cfg.resolvePageGeom(0, "", false)
	body := bodyFragment(root)
	if body == nil || len(body.Children) == 0 {
		page := root.Page(g0.pageW, g0.pageH)
		page.Items = e.appendMarginBoxes(page.Items, g0, 0, 1, pageStrings{})
		return &layout.Pages{Pages: []layout.Page{page}}
	}

	warnMidBlockForcedBreaks(body.Children, e.logf)

	cbWidth := contentWidth(body, g0.contentW)
	buckets := bucketBlocks(body.Children, g0.contentH, cbWidth, e.logf)

	// Pull page 0's top up to include the wrapper's own top border/background (same as
	// paginate) so a body top border shows on page 0.
	if len(buckets) > 0 {
		top := buckets[0].top
		if t := wrapperDecorationTop(root, top); t < top {
			top = t
		}
		if t := wrapperDecorationTop(body, top); t < top {
			top = t
		}
		buckets[0].top = top
	}

	perPagePos := splitPositionedByPage(root, buckets)
	perPageFloats := splitFloatsByPage(root, buckets)

	// Per-page geometry: page i's size + content inset, resolved from @page (named
	// pages are a follow-up; "" name today). The content height drives no further
	// bucketing here (already bucketed at g0.contentH); the inset positions content.
	geomFn := func(i int, _ pageBucket) pageGeom {
		return cfg.resolvePageGeom(i, "", false)
	}
	pages := e.assemblePages(root, body, buckets, perPagePos, perPageFloats, geomFn)
	return &layout.Pages{Pages: pages}
}
