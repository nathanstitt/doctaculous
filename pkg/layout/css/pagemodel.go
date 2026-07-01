package css

import (
	"context"
	"math"

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
	// Running holds the document's out-of-flow running elements (CSS GCPM
	// position:running(name)), keyed by name. A @page margin box whose content is
	// element(name) re-paints Running[name] on every page. Empty/nil when no element
	// uses running() — the byte-identical path (element() never fires).
	Running map[string]*cssbox.Box
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

// resolvePageGeom resolves the geometry for page index i (named `name`) from a
// PagedConfig: the page size (API explicit size > @page size > fallback) and the
// content box after @page margins. blank marks an empty (forced-break) page.
func (pc PagedConfig) resolvePageGeom(i int, name string, blank bool) pageGeom {
	up := pc.Pages.ResolvePage(i, name, blank)
	g := pageGeom{pageW: pc.FallbackW, pageH: pc.FallbackH, used: up}
	// An explicit API size (WithPageSize) overrides the DEFAULT (unnamed) @page size,
	// but a section that opted into a NAMED page (page: <name>) must still get that
	// page's own @page size — otherwise a `page: landscape` section can never reflow
	// wider. So apply a named page's size unconditionally, and the unnamed page's size
	// only when the API did not pin one. "A named page's size" is the size the CSS
	// cascade RESOLVED for that page, which may be INHERITED from an unnamed @page rule
	// (a named lookup folds in unnamed @page declarations — see page.go matchingPageRules),
	// not only a size the named rule declared itself — this is cascade-consistent and
	// intended (a named section inherits the unnamed @page size even under ExplicitSize).
	if up.HasSize && (name != "" || !pc.ExplicitSize) {
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

// pageRun is a maximal consecutive run of top-level blocks sharing a resolved page
// name (and therefore a content width). start/end are indices into body.Children
// (half-open [start,end)).
type pageRun struct {
	name       string
	start, end int
}

// blockPageName returns a block fragment's resolved CSS `page` name ("" = the default,
// un-named page). A nil Box reads as default.
func blockPageName(f *Fragment) string {
	if f == nil || f.Box == nil {
		return ""
	}
	return f.Box.Style.Page
}

// groupRuns partitions blocks into maximal consecutive runs sharing a page name. A page
// name change between adjacent blocks starts a new run (CSS GCPM: a `page` change forces
// a break). Returns one run spanning everything when all blocks share a name (the common
// single-width case).
func groupRuns(blocks []*Fragment) []pageRun {
	var runs []pageRun
	for i := 0; i < len(blocks); {
		name := blockPageName(blocks[i])
		j := i + 1
		for j < len(blocks) && blockPageName(blocks[j]) == name {
			j++
		}
		runs = append(runs, pageRun{name: name, start: i, end: j})
		i = j
	}
	return runs
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

	// Lay out once at the default width to discover the top-level block list + each
	// block's resolved page name (the cssbox tree is the same regardless of width, so the
	// run grouping is width-independent; only the per-run geometry differs).
	g0 := cfg.resolvePageGeom(0, "", false)
	base := e.layoutTree(ctx, root, g0.contentW)
	if base == nil {
		return &layout.Pages{Pages: []layout.Page{{WidthPt: g0.pageW, HeightPt: g0.pageH}}}, nil
	}
	body := bodyFragment(base)
	if body == nil || len(body.Children) == 0 {
		// No top-level blocks: single page (with margin boxes), as paginateDoc did.
		page := base.Page(g0.pageW, g0.pageH)
		page.Items = e.appendMarginBoxes(page.Items, g0, 0, 1, pageStrings{}, cfg.Running)
		return &layout.Pages{Pages: []layout.Page{page}}, nil
	}
	runs := groupRuns(body.Children)
	// Fast path: a single run at the default name ⇒ the existing single-width pipeline,
	// byte-identical. (groupRuns returns one run named "" when no block sets `page`.)
	if len(runs) == 1 && runs[0].name == "" {
		return e.paginateDoc(base, cfg), nil
	}
	return e.paginateRuns(ctx, root, base, cfg, runs), nil
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
		page.Items = e.appendMarginBoxes(page.Items, g0, 0, 1, pageStrings{}, cfg.Running)
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
	pages := e.assemblePages(root, body, buckets, perPagePos, perPageFloats, geomFn, cfg.Running)
	return &layout.Pages{Pages: pages}
}

// runBucket is a bucket plus the page geometry its run resolved to (size/margins/
// marginboxes for that run's named page), so the global assembly can size + inset + chrome
// each page correctly even though pages come from different runs.
type runBucket struct {
	bucket pageBucket
	geom   pageGeom
	// floats holds this bucket's out-of-flow floats, cloned from the run's cached
	// layout and shifted into the bucket's local frame (Y 0). nil when the bucket has
	// no floats — so a float-free document is byte-identical (no float items emitted).
	floats []*Fragment
}

// floatsForRun partitions the run layout's floats across its buckets, mirroring the
// single-width splitFloatsByPage: a float is routed to the bucket whose vertical band
// contains its margin-box top (pageForY — a float above the first band clamps to bucket
// 0, below the last clamps to the last), then shifted into that bucket's local frame
// (-bk.top) to match the bucket's blocks. It CLONES each float before shifting, because
// the run layout (layoutByWidth[w]) is a shared cache reused across every bucket of every
// run at this width — shifting the original in place (as splitFloatsByPage safely does,
// since it owns its layout) would corrupt later buckets. Returns one slice per bucket
// (nil where a bucket has no floats), so a float-free run is byte-identical.
func floatsForRun(runLayout *Fragment, bks []pageBucket) [][]*Fragment {
	out := make([][]*Fragment, len(bks))
	if runLayout == nil || len(runLayout.Floats) == 0 || len(bks) == 0 {
		return out
	}
	for _, fl := range runLayout.Floats {
		bi := pageForY(fl.Y, bks)
		clone := cloneFloatForPage(fl)
		shiftFragment(clone, -bks[bi].top)
		out[bi] = append(out[bi], clone)
	}
	return out
}

// cloneFloatForPage deep-copies a float fragment enough that shiftFragment (and the
// later translateFragment page-inset) can move it WITHOUT mutating the shared cached
// run layout (layoutByWidth[w], reused across buckets/runs at one width). It copies
// every field those two functions mutate in place or recurse into: the child slices
// (Children, Floats, Positioned), the inline line/glyph slices (BaselineY/Glyphs[].X
// are shifted), the pointer contents shiftFragment/translateFragment mutate (Image,
// Control, BgImage — CX/CY/Origin*/Clip* are moved), and the in-place-mutated auxiliary
// slices (Collapsed strips, PositionedInfo[].ClipChain rects). Value-typed fields
// (ClipRect, Border, Background, …) are copied by the struct copy `c := *f`. Genuinely
// read-only fields (glyph Outline paths, decoded images, the source cssbox.Box, styles)
// are shared by pointer — neither shift function touches them.
func cloneFloatForPage(f *Fragment) *Fragment {
	if f == nil {
		return nil
	}
	c := *f
	if len(f.Children) > 0 {
		c.Children = make([]*Fragment, len(f.Children))
		for i, ch := range f.Children {
			c.Children[i] = cloneFloatForPage(ch)
		}
	}
	if len(f.Floats) > 0 {
		c.Floats = make([]*Fragment, len(f.Floats))
		for i, ch := range f.Floats {
			c.Floats[i] = cloneFloatForPage(ch)
		}
	}
	if len(f.Positioned) > 0 {
		c.Positioned = make([]*Fragment, len(f.Positioned))
		for i, ch := range f.Positioned {
			c.Positioned[i] = cloneFloatForPage(ch)
		}
	}
	if len(f.Lines) > 0 {
		c.Lines = make([]LineFragment, len(f.Lines))
		for i, ln := range f.Lines {
			nl := ln
			if len(ln.Glyphs) > 0 {
				nl.Glyphs = make([]GlyphFragment, len(ln.Glyphs))
				copy(nl.Glyphs, ln.Glyphs)
			}
			c.Lines[i] = nl
		}
	}
	if len(f.Collapsed) > 0 {
		c.Collapsed = make([]layout.BorderItem, len(f.Collapsed))
		copy(c.Collapsed, f.Collapsed)
	}
	if len(f.PositionedInfo) > 0 {
		c.PositionedInfo = make([]PositionedInfo, len(f.PositionedInfo))
		for i, pi := range f.PositionedInfo {
			np := pi
			if len(pi.ClipChain) > 0 {
				np.ClipChain = make([]rect, len(pi.ClipChain))
				copy(np.ClipChain, pi.ClipChain)
			}
			c.PositionedInfo[i] = np
		}
	}
	if f.Image != nil {
		img := *f.Image
		c.Image = &img
	}
	if f.Control != nil {
		ctl := *f.Control
		c.Control = &ctl
	}
	if f.BgImage != nil {
		bg := *f.BgImage
		c.BgImage = &bg
	}
	return &c
}

// paginateRuns paginates a document whose top-level blocks resolve to more than one
// named page (different content widths). It lays the document out once per DISTINCT run
// content width, takes each run's block fragments from the layout matching its width,
// buckets each run against its own page geometry, then assembles + numbers all pages
// globally (so counter(page)/string() are document-wide).
func (e *Engine) paginateRuns(ctx context.Context, root *cssbox.Box, base *Fragment, cfg PagedConfig, runs []pageRun) *layout.Pages {
	// Cache one full-document layout per distinct content width.
	layoutByWidth := map[float64]*Fragment{}
	bodyByWidth := map[float64][]*Fragment{}
	getLayout := func(name string) (geom pageGeom, blocks []*Fragment) {
		geom = cfg.resolvePageGeom(0, name, false)
		w := geom.contentW
		if _, ok := layoutByWidth[w]; !ok {
			var frag *Fragment
			if math.Abs(w-contentWidth(bodyFragment(base), cfgFallback(cfg))) < 0.01 {
				frag = base // reuse the base layout when the width matches it
			} else {
				frag = e.layoutTree(ctx, root, w)
			}
			layoutByWidth[w] = frag
			bodyByWidth[w] = nil
			if b := bodyFragment(frag); b != nil {
				bodyByWidth[w] = b.Children
			}
		}
		return geom, bodyByWidth[w]
	}

	var all []runBucket
	for _, r := range runs {
		geom, blocks := getLayout(r.name)
		// Take this run's blocks BY INDEX from the matching-width layout. The cssbox tree
		// is identical across widths, so body.Children indices align run-for-run.
		if r.end > len(blocks) {
			continue // defensive: layout produced fewer blocks (shouldn't happen)
		}
		runBlocks := blocks[r.start:r.end]
		cb := contentWidth(bodyFragment(layoutByWidth[geom.contentW]), geom.contentW)
		bks := bucketBlocks(runBlocks, geom.contentH, cb, e.logf)
		// Split this run's out-of-flow floats across its buckets (cloned + shifted into
		// each bucket's local frame). Without this, floats in a named-page section were
		// dropped entirely (paginateRuns nil'd Floats and never re-attached them).
		runFloats := floatsForRun(layoutByWidth[geom.contentW], bks)
		for bi, bk := range bks {
			all = append(all, runBucket{bucket: bk, geom: geom, floats: runFloats[bi]})
		}
	}
	if len(all) == 0 {
		g0 := cfg.resolvePageGeom(0, "", false)
		return &layout.Pages{Pages: []layout.Page{{WidthPt: g0.pageW, HeightPt: g0.pageH}}}
	}

	// Global string snapshots over the concatenated bucket list.
	buckets := make([]pageBucket, len(all))
	for i := range all {
		buckets[i] = all[i].bucket
	}
	snaps := buildStringSnapshots(buckets)

	pages := make([]layout.Page, 0, len(all))
	for i := range all {
		bk := all[i].bucket
		g := all[i].geom
		// Shift this bucket's blocks to local Y 0 (each run's blocks are in their own
		// layout's page space).
		shiftFragments(bk.blocks, -bk.top)
		// Build a minimal page root carrying just this bucket's blocks. We synthesize a
		// shallow body wrapper so AppendItems flattens the blocks; the per-run base body
		// is reused for its box/styling.
		pageRoot := *base
		runBody := *bodyFragment(layoutByWidth[g.contentW])
		runBody.Children = bk.blocks
		runBody.Positioned, runBody.PositionedInfo, runBody.Floats = nil, nil, nil
		children := make([]*Fragment, len(base.Children))
		copy(children, base.Children)
		children[len(children)-1] = &runBody
		pageRoot.Children = children
		pageRoot.Positioned, pageRoot.PositionedInfo = nil, nil
		// Attach this bucket's floats at the page root (mirroring paginateDoc/assemblePages,
		// which sets pageRoot.Floats and keeps the body wrapper's Floats nil). They are in
		// the bucket's local frame; the translateFragment inset below carries them by the
		// same @page margin/bleed delta (translateFragment recurses into Floats).
		pageRoot.Floats = all[i].floats

		dx, dy := g.marginL+g.bleed, g.marginT+g.bleed
		if dx != 0 || dy != 0 {
			translateFragment(&pageRoot, dx, dy)
		}
		pg := pageRoot.Page(g.mediaW(), g.mediaH())
		before := len(pg.Items)
		pg.Items = e.appendMarginBoxes(pg.Items, g, i, len(all), snaps[i], cfg.Running)
		if g.bleed != 0 {
			translateItems(pg.Items, before, g.bleed, g.bleed)
		}
		pg.Items = appendCropMarks(pg.Items, g)
		pages = append(pages, pg)
	}
	return &layout.Pages{Pages: pages}
}

// cfgFallback returns the content width the base layout was built at (page 0 default
// content width), used to detect when a run's width matches the base layout so it can be
// reused instead of re-laid-out.
func cfgFallback(cfg PagedConfig) float64 {
	return cfg.resolvePageGeom(0, "", false).contentW
}
