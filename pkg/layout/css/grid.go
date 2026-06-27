package css

import (
	"context"
	"math"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// layoutGrid lays out a grid container (CSS Grid §7–§11) and returns its interior
// (positioned item fragments + content height). The signature matches layoutFlex /
// layoutTable. bandOriginY/fc are unused (a grid container is a BFC; item floats are
// self-contained within each item's own block layout).
//
// Phases (the §11 driver):
//
//  1. expand explicit tracks (columns vs contentW, rows vs a definite height else 0);
//  2. place items (grows the implicit-track counts) and append implicit tracks;
//     3a. size columns from each item's min/max-content contribution;
//     5a. lay out each item at its column-span width, capturing its natural height;
//     3b. size rows from those natural heights;
//  4. compute each track's content-box-relative edge position (gaps folded in);
//     5b/6. resolve per-item alignment (justify-*/align-*), relayout as needed, emit.
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
	// Items are initially laid out at the full area width (which is correct for stretch
	// alignment, and will be relaid out at max-content for non-stretch auto-width items).
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

	// Phase 4: content-distribution alignment + track positioning.
	// Compute leftover on each axis and apply justify-content / align-content.
	colTotalGap := colGap * float64(maxi(0, nCols-1))
	rowTotalGap := rowGap * float64(maxi(0, nRows-1))
	// Leftover is clamped to 0 when the tracks overflow: per CSS Grid §12.1 a
	// content-distribution property has no effect when the tracks plus free space
	// exceed the available space (so center/end do NOT pull tracks negative the way
	// flex's justifyOffsets would — overflow spills off the end only).
	colLeftover := contentW - sumF(colSizes) - colTotalGap
	if colLeftover < 0 {
		colLeftover = 0
	}
	// Row leftover is only meaningful when the container has a definite height.
	rowLeftover := 0.0
	if rowAvail > 0 {
		rowLeftover = rowAvail - sumF(rowSizes) - rowTotalGap
		if rowLeftover < 0 {
			rowLeftover = 0
		}
	}
	// stretch: grow auto-max tracks to absorb the leftover before positioning. Only
	// applies when the content-distribution value is "stretch". NOTE: this is NOT
	// redundant with resolveTrackSizes's maximize step — §11.5 clamps every infinite
	// growth limit to the track's base (content) size before maximize runs, so the
	// resolver cannot grow an auto track past its content; stretchTracks is the SOLE
	// mechanism that expands auto tracks to fill a definite-size container here.
	if b.Style.JustifyContent == "stretch" {
		colSizes = stretchTracks(colSizes, colSpecs, colLeftover)
		colLeftover = contentW - sumF(colSizes) - colTotalGap
		if colLeftover < 0 {
			colLeftover = 0
		}
	}
	if b.Style.AlignContent == "stretch" && rowAvail > 0 {
		rowSizes = stretchTracks(rowSizes, rowSpecs, rowLeftover)
		rowLeftover = rowAvail - sumF(rowSizes) - rowTotalGap
		if rowLeftover < 0 {
			rowLeftover = 0
		}
	}
	colLead, colBetween := contentOffsets(b.Style.JustifyContent, colLeftover, nCols)
	rowLead, rowBetween := contentOffsets(b.Style.AlignContent, rowLeftover, nRows)
	colPos := trackPositionsDist(colSizes, colGap, colLead, colBetween)
	rowPos := trackPositionsDist(rowSizes, rowGap, rowLead, rowBetween)

	// Phase 5b/6: resolve per-item alignment on both axes and emit fragments.
	// Page-space origin: x is absolute (contentX); y is local content-top-0 frame
	// (layoutBlock shifts the interior down afterward) — exactly like layoutFlex.
	for i, it := range items {
		areaLeft := colPos[areas[i].colStart] + contentX
		areaTop := rowPos[areas[i].rowStart] // local frame
		aw := spanSize(colSizes, areas[i].colStart, areas[i].colSpan(), colGap)
		ah := spanSize(rowSizes, areas[i].rowStart, areas[i].rowSpan(), rowGap)

		// Resolve per-item alignment on each axis.
		ji := resolveGridAlign(b.Style.JustifyItems, it.Style.JustifySelf)
		ai := resolveGridAlign(b.Style.AlignItems, it.Style.AlignSelf)

		// --- Inline axis (justify) ---
		// The initial layout was at aw (the area width). For non-stretch alignment on an
		// auto-width item, relayout at max-content to get the shrink-to-fit width.
		relaidOutInline := false
		itemUsedW := itemW[i]
		if ji != "stretch" && gridItemHasAutoWidth(it) {
			mc := e.measureMaxContent(ctx, it)
			if mc < aw {
				itemUsedW = mc
			} else {
				itemUsedW = aw
			}
			// Relayout at the shrink-to-fit width if it changed.
			if math.Abs(itemUsedW-itemW[i]) > 0.01 {
				frags[i], itemNatH[i] = e.layoutGridItem(ctx, it, itemUsedW)
				relaidOutInline = true
			}
		} else if ji == "stretch" {
			// stretch: item fills the area width (already laid out at aw).
			itemUsedW = aw
		}
		// Definite width: Phase 5a already laid the item out at its CSS width; read frag.W.
		if frags[i] != nil && !gridItemHasAutoWidth(it) {
			itemUsedW = frags[i].W
		}

		// inline axis offset within the area.
		var itemX float64
		switch ji {
		case "end":
			itemX = areaLeft + aw - itemUsedW
		case "center":
			itemX = areaLeft + (aw-itemUsedW)/2
		default: // start, stretch, baseline (baseline→start per Task 13)
			itemX = areaLeft
		}

		// --- Block axis (align) ---
		itemUsedH := itemNatH[i]
		if frags[i] != nil {
			itemUsedH = frags[i].H
		}

		if ai == "stretch" && gridItemHasAutoHeight(it) {
			// stretch on auto-height: relayout at itemUsedW (= area width when inline also
			// stretches) so inner content flows at the correct width, then pin height to ah.
			if !relaidOutInline {
				// Not yet relaid out at itemUsedW (e.g. a definite-width item, or
				// stretch-inline at a different width) — relayout so content flows correctly.
				frags[i], _ = e.layoutGridItem(ctx, it, itemUsedW)
			}
			if frags[i] != nil {
				frags[i].H = ah
			}
			itemUsedH = ah
		}

		// block axis offset within the area.
		var itemY float64
		switch ai {
		case "end":
			itemY = areaTop + ah - itemUsedH
		case "center":
			itemY = areaTop + (ah-itemUsedH)/2
		default: // start, stretch, baseline (baseline→start per Task 13)
			itemY = areaTop
		}

		placeGridFragment(frags[i], itemX, itemY, itemUsedW, itemUsedH)
	}

	// Content height = last row edge + last row size.
	contentHeight := 0.0
	if len(rowPos) > 0 {
		contentHeight = rowPos[len(rowPos)-1] + lastSize(rowSizes)
	}
	return interior{children: frags, contentHeight: contentHeight}
}

// resolveGridAlign resolves the effective alignment for a grid item on one axis:
// the item's *-self value if not "auto"/empty/"normal", otherwise the container's
// *-items value. Normalizes CSS Grid spellings to the canonical set:
//   - ""/"auto"/"normal" → "stretch"
//   - "flex-start" → "start"
//   - "flex-end" → "end"
//
// The returned string is one of: "stretch", "start", "end", "center", "baseline".
func resolveGridAlign(containerValue, selfValue string) string {
	a := selfValue
	if a == "" || a == "auto" || a == "normal" {
		a = containerValue
	}
	if a == "" || a == "auto" || a == "normal" {
		a = "stretch"
	}
	// Accept flex-* spellings that may appear when the cascade is shared with flexbox.
	switch a {
	case "flex-start":
		return "start"
	case "flex-end":
		return "end"
	}
	return a
}

// gridItemHasAutoWidth reports whether the item's inline (width) size is auto or
// percentage-based (not a definite length), so stretch/shrink-to-fit applies.
func gridItemHasAutoWidth(it *cssbox.Box) bool {
	u := it.Style.Width.Unit
	return u == gcss.UnitAuto || u == gcss.UnitPercent
}

// gridItemHasAutoHeight reports whether the item's block (height) size is auto or
// percentage-based (not a definite length), so stretch applies on the block axis.
func gridItemHasAutoHeight(it *cssbox.Box) bool {
	u := it.Style.Height.Unit
	return u == gcss.UnitAuto || u == gcss.UnitPercent
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

// trackPositionsDist returns each track's content-box-relative leading edge offset (left
// for columns, top for rows) with content-distribution offsets applied:
// pos[0] = lead; pos[i] = pos[i-1] + sizes[i-1] + gap + between.
// lead is the space before the first track; between is extra space between adjacent tracks.
func trackPositionsDist(sizes []float64, gap, lead, between float64) []float64 {
	pos := make([]float64, len(sizes))
	acc := lead
	for i := range sizes {
		pos[i] = acc
		acc += sizes[i] + gap + between
	}
	return pos
}

// contentOffsets returns the leading offset before the first track and the extra spacing
// between adjacent tracks for a given CSS content-distribution value (justify-content or
// align-content). It handles both the CSS Grid spellings (start/end) and the flex
// spellings (flex-start/flex-end) by normalizing before delegating to justifyOffsets.
// "stretch" is handled separately (by stretchTracks before this is called); if it reaches
// here (no growable tracks), it behaves as "start" (leading=0, between=0).
// leftover is the remaining space after track sizes and gaps; n is the track count.
func contentOffsets(value string, leftover float64, n int) (leading, between float64) {
	// Normalize grid start/end spellings to the flex spellings justifyOffsets expects.
	switch value {
	case "start", "normal":
		value = "flex-start"
	case "end":
		value = "flex-end"
	case "stretch":
		// stretch was handled by stretchTracks; if we reach here, no growable tracks
		// remain and leftover is 0. Behave as start.
		return 0, 0
	}
	return justifyOffsets(value, leftover, n)
}

// stretchTracks grows auto-max tracks to absorb the leftover space for
// align-content/justify-content: stretch. Only tracks whose max sizing function is
// content-based (auto/min-content/max-content) are growable; fr tracks absorb leftover
// via the flex step and are not grown here. If there are no growable tracks, the sizes
// are returned unchanged (the caller will then treat the leftover as start distribution).
func stretchTracks(sizes []float64, specs []trackSpec, leftover float64) []float64 {
	if leftover <= 0 || len(sizes) == 0 {
		return sizes
	}
	// Count growable tracks (auto-max but not flex).
	nGrow := 0
	for i, s := range specs {
		if i >= len(sizes) {
			break
		}
		if s.maxIsContent && !s.isFlex {
			nGrow++
		}
	}
	if nGrow == 0 {
		return sizes
	}
	share := leftover / float64(nGrow)
	out := make([]float64, len(sizes))
	for i := range sizes {
		out[i] = sizes[i]
		if i < len(specs) && specs[i].maxIsContent && !specs[i].isFlex {
			out[i] += share
		}
	}
	return out
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
