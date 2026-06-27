package css

import (
	"context"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// layoutGrid lays out a grid container (CSS Grid §7–§11) and returns its interior
// (positioned item fragments + content height). The signature matches layoutFlex /
// layoutTable. bandOriginY/fc are unused (a grid container is a BFC; item floats are
// self-contained within each item's own block layout).
//
// Phases (the §11 driver, alignment deferred to Task 9 — items size to and sit at the
// start of their area, i.e. the `stretch`/`start` defaults):
//
//  1. expand explicit tracks (columns vs contentW, rows vs a definite height else 0);
//  2. place items (grows the implicit-track counts) and append implicit tracks;
//     3a. size columns from each item's min/max-content contribution;
//     5a. lay out each item at its column-span width, capturing its natural height;
//     3b. size rows from those natural heights;
//  4. compute each track's content-box-relative edge position (gaps folded in);
//     5b/6. place each item into its area (start/start) and emit fragments.
func (e *Engine) layoutGrid(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext) interior {
	_ = bandOriginY
	_ = fc
	if b.Style.Direction == "rtl" {
		e.logf("css layout: RTL grids not supported; laying out LTR")
	}

	items := gridItemBoxes(b) // in-flow children (fixup already wrapped inline runs)
	if len(items) == 0 {
		return interior{contentHeight: 0}
	}

	// Column gap / row gap (px).
	colGap := lenPx(b.Style.ColumnGap, b.Style.FontSizePt)
	rowGap := lenPx(b.Style.RowGap, b.Style.FontSizePt)

	// Phase 1: expand explicit tracks. Columns use contentW (definite). Rows use the
	// container's definite content height if set, else 0 (indefinite => auto-repeat 1).
	rowAvail := definiteHeight(b) // 0 if auto
	colTracks := b.Style.GridTemplateColumns.ExpandGap(contentW, colGap)
	rowTracks := b.Style.GridTemplateRows.ExpandGap(rowAvail, rowGap)

	// Phase 2: place items (grows implicit tracks). dims = explicit counts + areas.
	dims := gridDims{
		explicitCols: len(colTracks),
		explicitRows: len(rowTracks),
		areas:        b.Style.GridTemplateAreas,
	}
	inputs := make([]placementInput, len(items))
	for i, it := range items {
		inputs[i] = placementInput{placement: it.Style.GridPlacement}
	}
	areas, nCols, nRows := placeItems(inputs, dims, b.Style.GridAutoFlow)

	// Extend the track lists with implicit tracks (grid-auto-columns/-rows; default auto).
	colTracks = extendImplicit(colTracks, nCols, b.Style.GridAutoColumns)
	rowTracks = extendImplicit(rowTracks, nRows, b.Style.GridAutoRows)

	// Phase 3a: size columns. Build trackSpecs (% resolved vs contentW) + per-column
	// content contributions (each item's min/max-content distributed to its columns).
	colSpecs := makeTrackSpecs(colTracks, contentW, b.Style.FontSizePt)
	colItems := contributions(ctx, e, items, areas) // column axis (width)
	colSizes := resolveTrackSizes(colSpecs, colItems, contentW, colGap*float64(maxi(0, nCols-1)))

	// Phase 5a: lay out each item at its column-span width; capture its natural height.
	itemW := make([]float64, len(items))
	for i := range items {
		itemW[i] = spanSize(colSizes, areas[i].colStart, areas[i].colSpan(), colGap)
	}
	frags := make([]*Fragment, len(items))
	itemNatH := make([]float64, len(items))
	for i, it := range items {
		frags[i], itemNatH[i] = e.layoutGridItem(ctx, it, itemW[i])
	}

	// Phase 3b: size rows using the laid-out item heights as content contributions.
	rowSpecs := makeTrackSpecs(rowTracks, rowAvail, b.Style.FontSizePt)
	rowItems := rowContributions(areas, itemNatH)
	rowAvailForSize := rowAvail
	if rowAvailForSize <= 0 {
		// Indefinite row container: there is no free space, so size auto rows to their
		// content and give fr rows zero extra. Pass the content sum as the available
		// space so the resolver's maximize step finds no surplus to distribute.
		rowAvailForSize = sumRowContent(rowItems, rowGap, nRows)
	}
	rowSizes := resolveTrackSizes(rowSpecs, rowItems, rowAvailForSize, rowGap*float64(maxi(0, nRows-1)))

	// Phase 4: positions of each track edge (content-box-relative), with gaps. (Content-
	// distribution alignment is applied in Task 9; here tracks start at 0 with gaps.)
	colPos := trackPositions(colSizes, colGap)
	rowPos := trackPositions(rowSizes, rowGap)

	// Phase 5b/6: place each item into its area at start/start (alignment in Task 9) and
	// emit. Page-space origin: x is absolute (contentX); y is local content-top-0 frame
	// (layoutBlock shifts the interior down afterward) — exactly like layoutFlex. A fixed
	// (contentX,0) is correct here because grid axes map directly to x/y (unlike a flex
	// column, whose main axis is y): the column position is the x offset (+contentX) and
	// the row position is the local y offset (no contentX).
	for i := range items {
		ax := colPos[areas[i].colStart] + contentX
		ay := rowPos[areas[i].rowStart] // local frame (no contentX on y)
		aw := itemW[i]
		ah := spanSize(rowSizes, areas[i].rowStart, areas[i].rowSpan(), rowGap)
		placeGridFragment(frags[i], ax, ay, aw, ah) // size to the area (the stretch default)
	}

	// Content height = last row edge + last row size.
	contentHeight := 0.0
	if len(rowPos) > 0 {
		contentHeight = rowPos[len(rowPos)-1] + lastSize(rowSizes)
	}
	return interior{children: frags, contentHeight: contentHeight}
}

// gridItemBoxes returns the grid container's in-flow item boxes. The fixup already
// wrapped inline runs and blockified inline-level children, so these are the items in
// document order. Grid has no `order` reordering in this slice (deferred), so no sort.
func gridItemBoxes(b *cssbox.Box) []*cssbox.Box {
	return append([]*cssbox.Box(nil), b.Children...)
}

// lenPx resolves a gcss.Length to px (against no percentage basis), clamped to >= 0.
// Used for gaps, which are never negative.
func lenPx(l gcss.Length, fontPt float64) float64 {
	v, _ := resolveLen(l, fontPt, 0)
	if v < 0 {
		v = 0
	}
	return v
}

// definiteHeight returns the container's definite content height in px when Height is
// a non-auto, non-percentage length (so the row axis has a definite available size),
// else 0 (indefinite — rows size to content).
func definiteHeight(b *cssbox.Box) float64 {
	h := b.Style.Height
	if h.Unit == gcss.UnitAuto || h.Unit == gcss.UnitPercent {
		return 0
	}
	v, _ := resolveLen(h, b.Style.FontSizePt, 0)
	if v < 0 {
		v = 0
	}
	return v
}

// extendImplicit appends implicit tracks to tracks until it holds n tracks, sizing each
// added track from autoTL (cycling through it). A nil/empty autoTL means a single auto
// track (CSS grid-auto-columns/-rows default). If tracks is already >= n it is returned
// unchanged.
func extendImplicit(tracks []gcss.TrackSize, n int, autoTL []gcss.TrackSize) []gcss.TrackSize {
	if len(tracks) >= n {
		return tracks
	}
	auto := autoTL
	if len(auto) == 0 {
		auto = []gcss.TrackSize{{Min: gcss.SizingFn{Kind: gcss.TrackAuto}, Max: gcss.SizingFn{Kind: gcss.TrackAuto}}}
	}
	for i := len(tracks); i < n; i++ {
		idx := (i - len(tracks)) % len(auto)
		tracks = append(tracks, auto[idx])
	}
	return tracks
}

// makeTrackSpecs maps each parsed gcss.TrackSize to a resolver trackSpec, resolving any
// percentage/length sizing functions against avail (the axis's available content size).
func makeTrackSpecs(tracks []gcss.TrackSize, avail, fontPt float64) []trackSpec {
	specs := make([]trackSpec, len(tracks))
	for i, t := range tracks {
		specs[i] = makeTrackSpec(t, avail, fontPt)
	}
	return specs
}

// contributions builds the column-axis (width) content contribution of each item for
// intrinsic track sizing: each item's min-content / max-content width as a trackItem
// over the columns it spans. Row contributions come AFTER item layout (rowContributions),
// since an item's height depends on the width it was laid out at.
func contributions(ctx context.Context, e *Engine, items []*cssbox.Box, areas []gridArea) []trackItem {
	out := make([]trackItem, len(items))
	for i, it := range items {
		out[i] = trackItem{
			start:      areas[i].colStart,
			span:       areas[i].colSpan(),
			minContent: e.measureMinContent(ctx, it),
			maxContent: e.measureMaxContent(ctx, it),
		}
	}
	return out
}

// rowContributions builds the row-axis content contribution of each item: its laid-out
// natural height (min == max == natH) as a trackItem over the rows it spans.
func rowContributions(areas []gridArea, natH []float64) []trackItem {
	out := make([]trackItem, len(areas))
	for i := range areas {
		out[i] = trackItem{
			start:      areas[i].rowStart,
			span:       areas[i].rowSpan(),
			minContent: natH[i],
			maxContent: natH[i],
		}
	}
	return out
}

// spanSize sums the track sizes over [start, start+span) plus the internal gaps between
// them (span-1 gaps). Out-of-range indices are clamped so a degenerate placement cannot
// panic.
func spanSize(sizes []float64, start, span int, gap float64) float64 {
	if span < 1 {
		span = 1
	}
	total := 0.0
	count := 0
	for i := start; i < start+span && i < len(sizes); i++ {
		if i < 0 {
			continue
		}
		total += sizes[i]
		count++
	}
	if count > 1 {
		total += gap * float64(count-1)
	}
	return total
}

// trackPositions returns each track's content-box-relative leading edge offset (left for
// columns, top for rows): position[i] = Σ(sizes[0..i-1]) + i*gap. The returned slice has
// one entry per track (len(sizes)), so position[i] is the start of track i.
func trackPositions(sizes []float64, gap float64) []float64 {
	pos := make([]float64, len(sizes))
	acc := 0.0
	for i := range sizes {
		pos[i] = acc
		acc += sizes[i] + gap
	}
	return pos
}

// placeGridFragment positions a laid-out item fragment at (x,y) and resizes it to (w,h),
// translating its descendants. Sizing to the area (w,h) is the `stretch` default for both
// axes (item-level alignment is Task 9). Reuses the table cell helper.
func placeGridFragment(frag *Fragment, x, y, w, h float64) {
	if frag == nil {
		return
	}
	stretchCellFragment(frag, x, y, w, h) // sets X/Y/W/H + shifts children
}

// layoutGridItem lays out one grid item's contents at its column-span width w, into its
// own BFC, and returns its fragment and natural border-box height. Mirrors the row case
// of layoutFlexItem: a fresh positioned context per item (resolved against the item's own
// content box), and abs/fixed descendants resolved before the fragment is repositioned.
func (e *Engine) layoutGridItem(ctx context.Context, it *cssbox.Box, w float64) (*Fragment, float64) {
	pos := &positionedContext{}
	res := e.layoutBlock(ctx, it, w, 0, 0, 0,
		&floatContext{cbLeft: 0, cbRight: w}, pos, posCBOwner{isPage: true})
	frag := res.frag
	natH := 0.0
	if frag != nil {
		natH = frag.H
		e.resolveAbsolute(ctx, pos, frag, w, frag.H)
	}
	return frag, natH
}

// maxi returns the larger of a and b (an int helper; the repo declines the max builtin).
func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// lastSize returns the last element of sizes, or 0 when empty.
func lastSize(sizes []float64) float64 {
	if len(sizes) == 0 {
		return 0
	}
	return sizes[len(sizes)-1]
}

// sumRowContent estimates the total row-axis content extent for an indefinite-height
// container: the sum of each row's largest single-span content contribution plus the
// inter-row gaps. It is only used to give the resolver a "no surplus" available size so
// auto rows size to content and fr rows get zero extra. nRows is the total row count.
func sumRowContent(items []trackItem, gap float64, nRows int) float64 {
	if nRows < 1 {
		return 0
	}
	per := make([]float64, nRows)
	for _, it := range items {
		if it.span != 1 {
			continue // multi-span contributions are distributed by the resolver, not summed here
		}
		if it.start < 0 || it.start >= nRows {
			continue
		}
		if it.maxContent > per[it.start] {
			per[it.start] = it.maxContent
		}
	}
	total := 0.0
	for _, p := range per {
		total += p
	}
	if nRows > 1 {
		total += gap * float64(nRows-1)
	}
	return total
}
