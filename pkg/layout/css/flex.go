package css

import (
	"context"
	"math"

	gcss "github.com/nathanstitt/omnidoc/pkg/css"
	"github.com/nathanstitt/omnidoc/pkg/layout/cssbox"
)

// flexItemSizing is the per-item input to the §9.7 flexible-length resolution: the
// purely numeric facts the algorithm needs, with NO layout dependency so the resolver
// is unit-testable in isolation. maxMain < 0 means "no maximum" (CSS `none`).
type flexItemSizing struct {
	base         float64 // flex base size (resolved flex-basis)
	hypothetical float64 // base clamped to [minMain, maxMain] (the hypothetical main size)
	grow         float64 // flex-grow
	shrink       float64 // flex-shrink
	minMain      float64 // used minimum main size (incl. the automatic minimum)
	maxMain      float64 // used maximum main size; <0 = none

	// mainStart/mainEnd and crossStart/crossEnd are the item's resolved margins on
	// each axis, in points. Flex sizing works on OUTER sizes — an item's margins
	// consume main-axis space and offset its cross position — so they belong with the
	// numeric inputs rather than being read separately at placement time, where it
	// would be too late for free-space distribution to have accounted for them.
	//
	// autoMainStart/autoMainEnd record `margin: auto` on the main axis, which absorbs
	// free space (CSS Flexbox §8.1) and is the idiomatic way to push one item to the
	// end of a row.
	mainStart, mainEnd   float64
	crossStart, crossEnd float64
	autoMainStart        bool
	autoMainEnd          bool
}

// outerMain is the item's hypothetical size plus its main-axis margins: the space it
// actually consumes in the line.
func (s flexItemSizing) outerMain(used float64) float64 { return used + s.mainStart + s.mainEnd }

// autoMainCount reports how many `auto` main margins the item has, for free-space
// distribution.
func (s flexItemSizing) autoMainCount() int {
	n := 0
	if s.autoMainStart {
		n++
	}
	if s.autoMainEnd {
		n++
	}
	return n
}

// clampF clamps v to [lo, hi]; hi < 0 means no upper bound.
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		v = lo
	}
	if hi >= 0 && v > hi {
		v = hi
	}
	return v
}

// resolveFlexibleLengths implements CSS Flexbox 9.7 for a single flex line and returns
// each item's used main size, in item order. innerMain is the flex container's inner
// main size; totalGap is the sum of all main-axis gaps between items. The algorithm is
// a multi-pass freeze loop: pick grow vs shrink, freeze inflexible items, then loop
// {distribute proportional to the used factor, clamp to min/max, freeze items that
// violated} until no flexible items remain.
func resolveFlexibleLengths(items []flexItemSizing, innerMain, totalGap float64) []float64 {
	n := len(items)
	target := make([]float64, n)
	frozen := make([]bool, n)

	// 1. Used flex factor: grow if there is surplus, else shrink.
	sumHypo := totalGap
	for i := range items {
		sumHypo += items[i].hypothetical
	}
	growing := sumHypo < innerMain

	// 2. Size inflexible items (freeze) and seed targets at the hypothetical size.
	for i := range items {
		it := items[i]
		target[i] = it.hypothetical
		factor := it.shrink
		if growing {
			factor = it.grow
		}
		switch {
		case factor == 0:
			frozen[i] = true
		case growing && it.base > it.hypothetical:
			frozen[i] = true
		case !growing && it.base < it.hypothetical:
			frozen[i] = true
		default:
			target[i] = it.base // unfrozen items start the loop at their base size
		}
	}

	// 3. Initial free space (frozen at frozen size, unfrozen at base size).
	initialFree := innerMain - totalGap
	for i := range items {
		if frozen[i] {
			initialFree -= target[i]
		} else {
			initialFree -= items[i].base
		}
	}

	viol := make([]int, n) // per-pass min/max violation flags (+1 up, -1 down, 0 none); reused each pass

	// 4. Loop until no unfrozen items remain.
	for {
		// (a) Check for flexible items; exit when all are frozen.
		anyUnfrozen := false
		for i := range items {
			if !frozen[i] {
				anyUnfrozen = true
				break
			}
		}
		if !anyUnfrozen {
			break
		}

		// (b) Remaining free space; with the sub-1 flex-factor-sum adjustment.
		remaining := innerMain - totalGap
		sumFactor := 0.0
		for i := range items {
			if frozen[i] {
				remaining -= target[i]
				continue
			}
			remaining -= items[i].base
			if growing {
				sumFactor += items[i].grow
			} else {
				sumFactor += items[i].shrink
			}
		}
		if sumFactor < 1 {
			scaled := initialFree * sumFactor
			if math.Abs(scaled) < math.Abs(remaining) {
				remaining = scaled
			}
		}

		// (c) Distribute proportional to the used flex factor.
		if growing {
			totalGrow := 0.0
			for i := range items {
				if !frozen[i] {
					totalGrow += items[i].grow
				}
			}
			if totalGrow > 0 {
				for i := range items {
					if !frozen[i] {
						target[i] = items[i].base + remaining*(items[i].grow/totalGrow)
					}
				}
			}
		} else {
			totalScaled := 0.0
			for i := range items {
				if !frozen[i] {
					totalScaled += items[i].shrink * items[i].base
				}
			}
			if totalScaled > 0 {
				for i := range items {
					if !frozen[i] {
						ratio := (items[i].shrink * items[i].base) / totalScaled
						target[i] = items[i].base + remaining*ratio // remaining is negative when shrinking
					}
				}
			}
		}

		// (d) Fix min/max violations; record the total violation sign.
		totalViolation := 0.0
		for i := range viol {
			viol[i] = 0
		}
		for i := range items {
			if frozen[i] {
				continue
			}
			clamped := clampF(target[i], items[i].minMain, items[i].maxMain)
			if clamped > target[i] {
				viol[i] = 1
			} else if clamped < target[i] {
				viol[i] = -1
			}
			totalViolation += clamped - target[i]
			target[i] = clamped
		}

		// (e) Freeze by total-violation sign.
		switch {
		case totalViolation == 0:
			for i := range items {
				frozen[i] = true
			}
		case totalViolation > 0:
			for i := range items {
				if viol[i] == 1 {
					frozen[i] = true
				}
			}
		default:
			for i := range items {
				if viol[i] == -1 {
					frozen[i] = true
				}
			}
		}
	}

	return target
}

// flexAxis maps abstract main/cross sizes and positions onto x/y/width/height for a
// given flex-direction. row*: main = horizontal. column*: main = vertical. The reverse
// directions flip placement along the main axis (handled by the caller via reverseMain).
type flexAxis struct {
	vertical    bool // true for column / column-reverse (main axis is vertical)
	reverseMain bool // true for row-reverse / column-reverse
	// reverseCross flips the cross axis. It is set only for an RTL COLUMN container,
	// where the cross axis is horizontal and therefore direction-sensitive: cross-start
	// is the right edge, so align-items/align-self start and end swap.
	reverseCross bool
	// reverseCrossLines flips the order LINES are stacked in, for flex-wrap:wrap-reverse.
	// It is reverseCross XOR wrap-reverse: an RTL column with wrap-reverse flips the
	// cross axis twice and the two cancel. Set by layoutFlex, which knows the wrap value;
	// axisFor deals only with flex-direction and direction.
	reverseCrossLines bool
}

// axisFor resolves the flex axes from flex-direction and the used `direction`.
//
// For a ROW container the main axis is the inline axis, so RTL simply reverses it —
// which is exactly what reverseMain already expresses. RTL therefore composes with
// row-reverse by XOR: `direction:rtl` + `row-reverse` lays out in LTR-row order (two
// flips cancel). justify-content needs no special handling because justifyOffsets
// works in abstract main-axis terms and the placement loop applies the reverse
// formula uniformly, so flex-start/flex-end/space-around/space-evenly all flip with it.
//
// For a COLUMN container the main axis is vertical and unaffected by direction; the
// CROSS axis is the inline one, so RTL flips that instead (reverseCross).
func axisFor(dir, direction string) flexAxis {
	rtl := direction == "rtl"
	switch dir {
	case "column":
		return flexAxis{vertical: true, reverseCross: rtl}
	case "column-reverse":
		return flexAxis{vertical: true, reverseMain: true, reverseCross: rtl}
	case "row-reverse":
		return flexAxis{reverseMain: !rtl}
	default: // row
		return flexAxis{reverseMain: rtl}
	}
}

// rect builds a page-space border-box rect from main/cross position+size. originMain and
// originCross are the container's content-box origin in page space along each axis.
func (a flexAxis) rect(originMain, originCross, mainPos, crossPos, mainSize, crossSize float64) (x, y, w, h float64) {
	if a.vertical {
		return originCross + crossPos, originMain + mainPos, crossSize, mainSize
	}
	return originMain + mainPos, originCross + crossPos, mainSize, crossSize
}

// layoutFlex lays out a single-line flex container (CSS Flexbox 9) and returns its
// interior (positioned item fragments + the content height). Signature matches
// layoutTable. bandOriginY/fc are reserved for future float interactions (a flex
// container establishes a BFC; floats inside items are self-contained).
func (e *Engine) layoutFlex(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext, posCtx *positionedContext, posCB posCBOwner) interior {
	_ = bandOriginY
	_ = fc
	ax := axisFor(b.Style.FlexDirection, effectiveDirection(b))
	// wrap-reverse stacks the lines from the cross-END rather than the cross-start. It
	// XORs with the RTL cross flip for the same reason reverseMain does on the main
	// axis: an RTL column with wrap-reverse flips the cross axis twice, and two flips
	// cancel.
	ax.reverseCrossLines = ax.reverseCross != (b.Style.FlexWrap == "wrap-reverse")

	// An absolutely- or fixed-positioned child of a flex container is NOT a flex item
	// (CSS Flexbox §4.1): it is out of flow and positioned against the container's
	// padding box, exactly as in a block container. Laying it out as an item instead
	// pinned it to the container's edge and discarded its `left`/`top` — measured, a
	// `left: 300px` child landed at x=0 under `display:flex` and at x=300 under a plain
	// block. Defer them to the same pass a block container uses.
	items := flexItemBoxes(b)
	if posCtx != nil {
		var inflow []*cssbox.Box
		for _, it := range items {
			if it.Position == cssbox.PosAbsolute || it.Position == cssbox.PosFixed {
				cb := posCB
				if it.Position == cssbox.PosFixed {
					cb = posCBOwner{isPage: true}
				}
				posCtx.deferred = append(posCtx.deferred, deferredAbs{box: it, cb: cb})
				continue
			}
			inflow = append(inflow, it)
		}
		items = inflow
	}
	if len(items) == 0 {
		return interior{contentHeight: 0}
	}

	// Container inner main size. For row it is contentW; for column it is the content
	// height if definite, else indefinite (content-sized => no grow/shrink).
	innerMain, mainDefinite := e.flexMainSize(b, contentW, ax)

	// Per-item flex base size + hypothetical main size + used min/max main.
	sizings := make([]flexItemSizing, len(items))
	// availCross is the container's inner CROSS size, used to clamp a column item's
	// auto cross width before its content height is measured against it. For a row the
	// cross axis is vertical and the sizing path never consults it (-1 = no clamp).
	availCross := -1.0
	if ax.vertical {
		availCross = contentW
	}
	for i, it := range items {
		sizings[i] = e.itemSizing(ctx, it, ax, innerMain, availCross)
	}

	// Main-axis gap (column-gap for row, row-gap for column) between adjacent items.
	mainGap := e.flexMainGap(b, ax)

	// §9.3: partition the items into flex lines. nowrap yields exactly one line
	// spanning every item, so the single-line behavior is a special case rather than a
	// separate code path.
	lines := collectLines(sizings, wrapEnabled(b.Style.FlexWrap), innerMain, mainGap, mainDefinite)

	// §9.7 per line: resolve each line's flexible lengths against the container's inner
	// main size. resolveFlexibleLengths is already a per-LINE function, so it needs no
	// change — it is simply called once per line rather than once per container.
	//
	// lineMain is the main extent the reverse-placement formula flips within. For a
	// definite main size that is the container's; for an indefinite one (a column with
	// auto height) the container is sized to its content, so it is the line's own
	// consumed extent.
	lineMain := make([]float64, len(lines))
	for li := range lines {
		ln := &lines[li]
		gaps := mainGap * float64(ln.len()-1)
		if !mainDefinite {
			ln.usedMain = make([]float64, ln.len())
			sum := gaps
			for i := ln.start; i < ln.end; i++ {
				ln.usedMain[i-ln.start] = sizings[i].hypothetical
				sum += ln.usedMain[i-ln.start]
			}
			lineMain[li] = sum
		} else {
			ln.usedMain = resolveFlexibleLengths(sizings[ln.start:ln.end], innerMain, gaps)
			lineMain[li] = innerMain
		}
	}

	// Lay out each item's contents at its used main size; capture its cross size.
	frags := make([]*Fragment, len(items))
	crossSizes := make([]float64, len(items))
	for li := range lines {
		ln := &lines[li]
		for i := ln.start; i < ln.end; i++ {
			frags[i], crossSizes[i] = e.layoutFlexItem(ctx, items[i], ax, ln.usedMain[i-ln.start], availCross)
		}
	}

	// Per-line cross size: the tallest item on the line. For a SINGLE-line container the
	// line instead fills the container's inner cross size when that is DEFINITE (CSS
	// Flexbox §9.4 step 8) — that is what makes align-items:center/flex-end align within
	// the container's extent rather than the tallest item's. The clamp is single-line
	// only: with several lines the leftover cross space is align-content's to distribute,
	// and stretching one line to the whole container would swallow the others.
	// (Still floored at the max item so a too-small definite container does not clip.)
	for li := range lines {
		ln := &lines[li]
		for i := ln.start; i < ln.end; i++ {
			// The line must be tall enough for the item's MARGIN box, not just its
			// border box, or a cross margin would overflow the line it sits in.
			if outer := crossSizes[i] + sizings[i].crossStart + sizings[i].crossEnd; outer > ln.cross {
				ln.cross = outer
			}
		}
	}
	if len(lines) == 1 {
		if def, ok := e.flexCrossSize(b, contentW, ax); ok && def > lines[0].cross {
			lines[0].cross = def
		}
	}

	// Cross-axis gap between lines, and align-content's distribution of the leftover
	// cross space. Both are meaningful only for a multi-line container.
	crossGap := 0.0
	crossLead, crossBetween := 0.0, 0.0
	if len(lines) > 1 {
		crossGap = e.flexCrossGap(b, ax)
		crossLead, crossBetween = e.alignContentOffsets(b, lines, contentW, ax, crossGap)
	}

	// Origins for rect(): the horizontal (x) position must be absolute page space (the
	// content-box left = contentX), while the vertical (y) position must be in the local
	// content-top-0 frame (layoutBlock shifts the interior down by contentTopY afterward).
	// rect maps main→x / cross→y for a row and cross→x / main→y for a column, so the
	// contentX origin belongs to the MAIN axis for a row but the CROSS axis for a column;
	// the other axis (the local-Y one) takes origin 0. Passing a fixed (contentX, 0) would
	// be correct only for rows — for a column it would drop contentX off x and add it to y,
	// misplacing items whenever the container's content box is not at x=0.
	originMain, originCross := contentX, 0.0
	if ax.vertical {
		originMain, originCross = 0, contentX
	}

	// Assign each line its cross-axis offset, then place its items. wrap-reverse stacks
	// the lines from the cross-END, so they are assigned offsets in reverse order.
	crossPos := crossLead
	for li := range lines {
		idx := li
		if ax.reverseCrossLines {
			idx = len(lines) - 1 - li
		}
		lines[idx].crossPos = crossPos
		crossPos += lines[idx].cross + crossGap + crossBetween
	}

	for li := range lines {
		ln := &lines[li]
		// justify-content distributes free MAIN space within each line independently
		// (§9.5), so the leading/between offsets are computed per line.
		consumed := mainGap * float64(ln.len()-1)
		autoMargins := 0
		for i := ln.start; i < ln.end; i++ {
			consumed += sizings[i].outerMain(ln.usedMain[i-ln.start])
			autoMargins += sizings[i].autoMainCount()
		}
		free := lineMain[li] - consumed
		// `margin: auto` on the main axis absorbs ALL free space before
		// justify-content sees any (CSS Flexbox §8.1) — it is how an author pushes one
		// item to the end of a row. With any auto margin present, justify-content has
		// nothing left to distribute, which is the spec's own resolution order.
		autoShare := 0.0
		if autoMargins > 0 && free > 0 {
			autoShare = free / float64(autoMargins)
			free = 0
		}
		leading, between := justifyOffsets(b.Style.JustifyContent, free, ln.len())

		mainPos := leading
		for i := ln.start; i < ln.end; i++ {
			used := ln.usedMain[i-ln.start]
			sz := sizings[i]
			startMargin := sz.mainStart
			if sz.autoMainStart {
				startMargin = autoShare
			}
			endMargin := sz.mainEnd
			if sz.autoMainEnd {
				endMargin = autoShare
			}
			mainPos += startMargin
			align := resolvedAlign(b, items[i])
			itemCross := crossSizes[i]

			// stretch: grow an auto-cross item to the line cross size and relayout its
			// contents at that cross measure (a row item's width is its main size, which
			// is fixed; stretch grows its HEIGHT — pin the fragment height to the line).
			if align == "stretch" && !itemHasDefiniteCross(items[i], ax) {
				// Stretch fills the line LESS the item's own cross margins, so a
				// stretched item with margins does not overflow.
				frags[i], itemCross = e.stretchFlexItem(ctx, items[i], ax, used, ln.cross-sz.crossStart-sz.crossEnd)
			}

			// The cross offset positions the item's MARGIN box, so its own cross-start
			// margin insets it within whatever that alignment produced.
			cp := ln.crossPos + crossOffset(align, ln.cross, itemCross+sz.crossStart+sz.crossEnd, ax) + sz.crossStart
			pos := mainPos
			if ax.reverseMain {
				pos = lineMain[li] - mainPos - used
			}
			placeFlexFragment(frags[i], ax, originMain, originCross, pos, cp, used, itemCross)
			mainPos += used + endMargin + mainGap + between
		}

		// Baseline alignment post-pass, PER LINE (§9.4 baseline groups are per-line).
		// For a ROW container, the line's baseline-aligned items form one group;
		// alignBaselineGroup shifts each DOWN so its first baseline coincides with the
		// group maximum, returning the largest shift as a conservative extra cross
		// extent. Grow the line's cross size so it encloses the shifted items.
		// For a COLUMN container the cross axis is horizontal, so there is no meaningful
		// text baseline and it falls back to flex-start (crossOffset already returns 0).
		// With no baseline-aligned item this returns 0 and nothing shifts.
		if !ax.vertical {
			var group []baselineItem
			for i := ln.start; i < ln.end; i++ {
				group = append(group, baselineItem{frag: frags[i], baseline: resolvedAlign(b, items[i]) == "baseline"})
			}
			if extra := alignBaselineGroup(group); extra > 0 {
				ln.cross += extra
			}
		}
	}
	if ax.vertical {
		// column: log once if any item requests baseline (fallback to flex-start).
		for i := range items {
			if resolvedAlign(b, items[i]) == "baseline" {
				e.logf("css layout: align-items/align-self baseline not supported on column flex; using flex-start")
				break
			}
		}
	}

	// The container's content extent along the BLOCK axis. For a row the lines stack
	// vertically, so it is the sum of the line cross sizes plus the gaps between them;
	// for a column the block axis is the main one, so it is the longest line's extent.
	contentHeight := 0.0
	if ax.vertical {
		for li := range lines {
			if lineMain[li] > contentHeight {
				contentHeight = lineMain[li]
			}
		}
	} else {
		for li := range lines {
			contentHeight += lines[li].cross
		}
		contentHeight += (crossGap + crossBetween) * float64(len(lines)-1)
		contentHeight += crossLead
	}
	// NB: do NOT set interior.intrinsicWidth — that field shrink-to-fits a TABLE box;
	// a flex container fills its containing-block width like a normal block.
	return interior{children: frags, contentHeight: contentHeight}
}

// resolvedAlign returns the effective cross-axis alignment for an item: align-self if it
// is not auto, else the container's align-items. For a row container, baseline triggers
// real first-baseline alignment via alignBaselineGroup (applied as a post-pass in
// layoutFlex after cross-positioning); for a column container baseline falls back to
// flex-start (no meaningful horizontal baseline).
func resolvedAlign(container, item *cssbox.Box) string {
	a := item.Style.AlignSelf
	if a == "" || a == "auto" {
		a = container.Style.AlignItems
	}
	if a == "" {
		a = "stretch"
	}
	return a
}

// crossOffset returns the item's cross-axis position within a line of size lineCross for
// an item of outer cross size itemCross under alignment a (stretch is handled separately
// before this is called, by which point itemCross == lineCross).
//
// ax.reverseCross flips the cross-start edge, which happens for an RTL column container
// (there the cross axis is the inline one). The abstract cross offset is measured from
// cross-start, so flipping means measuring from the other end.
//
// The CSS Box Alignment spellings "start"/"end" are accepted alongside the Flexbox
// "flex-start"/"flex-end": the cascade parses both for align-items/align-self, and
// without these cases "align-items: end" would silently fall through to flex-start.
func crossOffset(a string, lineCross, itemCross float64, ax flexAxis) float64 {
	off := 0.0
	switch a {
	case "flex-end", "end":
		off = lineCross - itemCross
	case "center":
		off = (lineCross - itemCross) / 2
	default: // flex-start, start, stretch; baseline items start here (shifted by the row post-pass)
		off = 0
	}
	if ax.reverseCross {
		return lineCross - itemCross - off
	}
	return off
}

// itemHasDefiniteCross reports whether the item has a definite cross size (so stretch
// does not apply). For a row the cross axis is height; for a column it is width.
func itemHasDefiniteCross(it *cssbox.Box, ax flexAxis) bool {
	l := it.Style.Height
	if ax.vertical {
		l = it.Style.Width
	}
	return l.Unit != gcss.UnitAuto && l.Unit != gcss.UnitPercent
}

// stretchFlexItem re-lays an auto-cross item out to the line cross size and returns its
// new fragment + outer cross size (== lineCross). For a row the main size (width) is
// fixed at usedMain and the height is pinned to lineCross. For a column the main size
// (height) is usedMain and the width (cross) becomes lineCross.
func (e *Engine) stretchFlexItem(ctx context.Context, it *cssbox.Box, ax flexAxis, usedMain, lineCross float64) (*Fragment, float64) {
	if ax.vertical {
		// column: relayout at width = lineCross, height pinned to usedMain.
		pos := &positionedContext{}
		defer withDefiniteHeight(it, usedMain)()
		res := e.layoutBlock(ctx, it, lineCross, 0, 0, 0,
			&floatContext{cbLeft: 0, cbRight: lineCross}, pos, posCBOwner{isPage: true})
		frag := res.frag
		if frag != nil {
			frag.H = usedMain
			consumePendingPositioned(frag, res.pendingPositioned)
			e.resolveAbsolute(ctx, pos, frag, lineCross, usedMain)
		}
		return frag, lineCross
	}
	// row: width = usedMain (the main size); pin height to lineCross.
	pos := &positionedContext{}
	// The stretched height must be DEFINITE before the interior lays out, not pinned
	// onto the fragment afterwards. A flex item whose height comes from stretch (or
	// from its own flex:1) laid its children out as if it were auto-height, so
	// justify-content had nothing to resolve against and packed them at the top —
	// measured: children at y=0 where an explicit height centred them at y=90.
	defer withDefiniteHeight(it, lineCross)()
	res := e.layoutBlock(ctx, it, usedMain, 0, 0, 0,
		&floatContext{cbLeft: 0, cbRight: usedMain}, pos, posCBOwner{isPage: true})
	frag := res.frag
	if frag != nil {
		frag.H = lineCross
		consumePendingPositioned(frag, res.pendingPositioned)
		e.resolveAbsolute(ctx, pos, frag, usedMain, lineCross)
	}
	return frag, lineCross
}

// justifyOffsets returns the leading main offset (before the first item) and the extra
// spacing inserted between adjacent items, for a given justify-content value. freeSpace
// is the leftover main space after used sizes + gaps; n is the item count. Negative
// freeSpace (overflow) is treated as 0 leading / 0 extra for the distributed modes, and
// flex-end/center still shift by the (negative) free space (overflowing the start).
func justifyOffsets(jc string, freeSpace float64, n int) (leading, between float64) {
	if n == 0 {
		return 0, 0
	}
	switch jc {
	case "flex-end":
		return freeSpace, 0
	case "center":
		return freeSpace / 2, 0
	case "space-between":
		if n == 1 || freeSpace < 0 {
			return 0, 0
		}
		return 0, freeSpace / float64(n-1)
	case "space-around":
		if freeSpace < 0 {
			return 0, 0
		}
		unit := freeSpace / float64(n)
		return unit / 2, unit
	case "space-evenly":
		if freeSpace < 0 {
			return 0, 0
		}
		unit := freeSpace / float64(n+1)
		return unit, unit
	default: // flex-start
		return 0, 0
	}
}

// flexItemBoxes returns the in-flow flex item child boxes (the fixup already wrapped
// inline runs + blockified inline-level boxes), sorted by `order` (stable for ties).
func flexItemBoxes(b *cssbox.Box) []*cssbox.Box {
	items := append([]*cssbox.Box(nil), b.Children...)
	// Stable insertion sort by Style.Order: it swaps only on a strict >, so items with
	// equal order keep their document order (CSS Flexbox §5.4 order is stable for ties).
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j-1].Style.Order > items[j].Style.Order; j-- {
			items[j-1], items[j] = items[j], items[j-1]
		}
	}
	return items
}

// flexMainSize returns the container inner main size and whether it is definite.
func (e *Engine) flexMainSize(b *cssbox.Box, contentW float64, ax flexAxis) (float64, bool) {
	if !ax.vertical {
		return contentW, true // row: the content width is always definite here
	}
	// column: main = height; definite only if an explicit non-auto, non-% height is set.
	if b.Style.Height.Unit != gcss.UnitAuto && b.Style.Height.Unit != gcss.UnitPercent {
		h, _ := resolveLen(b.Style.Height, b.Style.FontSizePt, 0)
		return h, true
	}
	return 0, false
}

// flexCrossSize returns the container inner CROSS size and whether it is definite. For a
// ROW container the cross axis is vertical (height) — definite only if an explicit non-auto,
// non-% height is set. For a COLUMN container the cross axis is horizontal (width) — the
// content width, always definite here. (The mirror of flexMainSize across the axes.)
func (e *Engine) flexCrossSize(b *cssbox.Box, contentW float64, ax flexAxis) (float64, bool) {
	if ax.vertical {
		return contentW, true // column: cross = width = content width (definite)
	}
	// row: cross = height; definite only if an explicit non-auto, non-% height is set.
	if b.Style.Height.Unit != gcss.UnitAuto && b.Style.Height.Unit != gcss.UnitPercent {
		h, _ := resolveLen(b.Style.Height, b.Style.FontSizePt, 0)
		if h >= 0 {
			return h, true
		}
	}
	return 0, false
}

// flexMainGap returns the main-axis gap: column-gap for a row, row-gap for a column.
func (e *Engine) flexMainGap(b *cssbox.Box, ax flexAxis) float64 {
	g := b.Style.ColumnGap
	if ax.vertical {
		g = b.Style.RowGap
	}
	v, _ := resolveLen(g, b.Style.FontSizePt, 0)
	if v < 0 {
		v = 0
	}
	return v
}

// flexCrossGap is the gap BETWEEN flex lines: row-gap for a row container (lines
// stack vertically), column-gap for a column container. It mirrors flexMainGap on the
// other axis.
//
// It is only consulted for a multi-line container: with one line there is no
// between-lines gap, which is why the cross gap was previously stored and never read.
func (e *Engine) flexCrossGap(b *cssbox.Box, ax flexAxis) float64 {
	g := b.Style.RowGap
	if ax.vertical {
		g = b.Style.ColumnGap
	}
	v, _ := resolveLen(g, b.Style.FontSizePt, 0)
	if v < 0 {
		v = 0
	}
	return v
}

// wrapEnabled reports whether a flex-wrap value permits multiple lines. The empty
// string is the initial value (nowrap), so a caller that never sets it is unaffected.
func wrapEnabled(v string) bool { return v == "wrap" || v == "wrap-reverse" }

// alignContentOffsets distributes the leftover CROSS space among a multi-line
// container's lines (CSS Flexbox §9.6), returning the leading offset before the first
// line and the extra spacing between adjacent lines.
//
// Under `stretch` the leftover is instead absorbed INTO the lines: each grows by an
// equal share, so the lines together fill the container. That is simpler than grid's
// stretchTracks, which has to filter on which tracks are growable.
//
// NOTE the initial value. ComputedStyle.AlignContent defaults to "start", which is
// grid's convention — but CSS Flexbox's initial `align-content` is `stretch`. Mapping
// happens here rather than by changing the shared default, which grid relies on.
func (e *Engine) alignContentOffsets(b *cssbox.Box, lines []flexLine, contentW float64, ax flexAxis, crossGap float64) (leading, between float64) {
	def, ok := e.flexCrossSize(b, contentW, ax)
	if !ok {
		return 0, 0 // indefinite cross size: the container hugs its lines, no leftover
	}
	total := crossGap * float64(len(lines)-1)
	for i := range lines {
		total += lines[i].cross
	}
	leftover := def - total
	if leftover <= 0 {
		return 0, 0
	}

	value := b.Style.AlignContent
	if value == "" || value == "start" || value == "normal" {
		// The grid-convenient default; for flex the initial value is stretch.
		value = "stretch"
	}
	if value == "stretch" {
		share := leftover / float64(len(lines))
		for i := range lines {
			lines[i].cross += share
		}
		return 0, 0
	}
	return contentOffsets(value, leftover, len(lines))
}

// flexLine is one line of a flex container: a contiguous range of item indices
// [start,end) plus the per-line results that used to be single container-wide values.
//
// A nowrap container has exactly one line spanning every item, so the single-line
// behavior is a special case of this rather than a separate path.
type flexLine struct {
	start, end int       // item index range, [start,end)
	usedMain   []float64 // flexed main size per item in the line (indexed from start)
	cross      float64   // the line's cross size
	crossPos   float64   // the line's cross-axis offset within the container
}

// len returns the number of items on the line.
func (l flexLine) len() int { return l.end - l.start }

// collectLines implements CSS Flexbox §9.3: partition items into flex lines.
//
// For nowrap (the default) every item goes on one line, whatever the widths — the
// container overflows instead. For wrap, items accumulate until adding the next would
// exceed the container's inner main size, counting the gaps between them; an item
// always lands on an empty line even when it alone overflows, so the loop always makes
// progress.
//
// Breaking uses each item's OUTER HYPOTHETICAL main size (its flex base clamped by
// min/max), per the spec — not its flexed size, which is only resolved per line
// afterwards.
//
// An indefinite main size cannot be broken against, so it collapses to a single line:
// the container is sized to its content, so there is no width to overflow.
func collectLines(sizings []flexItemSizing, wrap bool, innerMain, mainGap float64, mainDefinite bool) []flexLine {
	n := len(sizings)
	if !wrap || !mainDefinite || n == 0 {
		return []flexLine{{start: 0, end: n}}
	}
	var lines []flexLine
	start := 0
	used := 0.0
	for i := 0; i < n; i++ {
		// Pack by the OUTER size: an item's margins consume line space, so a line that
		// fits ignoring them may not fit once they are counted.
		h := sizings[i].outerMain(sizings[i].hypothetical)
		next := used + h
		if i > start {
			next += mainGap
		}
		if i > start && next > innerMain {
			lines = append(lines, flexLine{start: start, end: i})
			start = i
			used = h
			continue
		}
		used = next
	}
	return append(lines, flexLine{start: start, end: n})
}

// itemSizing computes a flex item's flex base size, hypothetical main size, and used
// min/max main size: the numeric inputs to resolveFlexibleLengths. The base size comes
// from flexBaseSize (flex-basis: auto/content/percentage/length, CSS Flexbox §9.2); the
// used min/max come from usedMinMaxMain (explicit min/max plus the §4.5 automatic
// minimum); the hypothetical main size is the base clamped to [minMain, maxMain].
func (e *Engine) itemSizing(ctx context.Context, it *cssbox.Box, ax flexAxis, innerMain, availCross float64) flexItemSizing {
	base := e.flexBaseSize(ctx, it, ax, innerMain, availCross)
	minMain, maxMain := e.usedMinMaxMain(ctx, it, ax, availCross)
	sz := flexItemSizing{
		base:         base,
		hypothetical: clampF(base, minMain, maxMain),
		grow:         it.Style.FlexGrow,
		shrink:       it.Style.FlexShrink,
		minMain:      minMain,
		maxMain:      maxMain,
	}
	sz.mainStart, sz.mainEnd, sz.crossStart, sz.crossEnd,
		sz.autoMainStart, sz.autoMainEnd = flexMargins(it, ax, innerMain)
	return sz
}

// flexBaseSize resolves flex-basis to the item's flex base size.
// auto: use the main-size property (width for row, height for column) if it is a
// definite length; otherwise fall through to content (max-content).
// content: measureMaxContent.
// NOTE: for a column container, measureMaxContent returns a width, not a height —
// using it as the column main-axis content base is a documented approximation for
// slice 1; refine in 9b.
func (e *Engine) flexBaseSize(ctx context.Context, it *cssbox.Box, ax flexAxis, innerMain, availCross float64) float64 {
	fb := it.Style.FlexBasis
	switch fb.Unit {
	case gcss.UnitAuto:
		mainLen := it.Style.Width
		if ax.vertical {
			mainLen = it.Style.Height
		}
		if mainLen.Unit != gcss.UnitAuto && mainLen.Unit != gcss.UnitPercent {
			v, _ := resolveLen(mainLen, it.Style.FontSizePt, 0)
			return v
		}
		return e.mainContentSize(ctx, it, ax, availCross)
	case gcss.UnitContent:
		return e.mainContentSize(ctx, it, ax, availCross)
	case gcss.UnitPercent:
		return innerMain * fb.Value / 100
	default:
		v, _ := resolveLen(fb, it.Style.FontSizePt, 0)
		return v
	}
}

// usedMinMaxMain returns the item's used min/max main size. maxMain < 0 = none.
// When min-(width|height) is auto, the automatic minimum size (CSS Flexbox §4.5)
// applies: the item's min-content size, capped by a definite main-size property if
// smaller, and capped by maxMain if maxMain >= 0.
func (e *Engine) usedMinMaxMain(ctx context.Context, it *cssbox.Box, ax flexAxis, availCross float64) (minMain, maxMain float64) {
	minL, maxL := it.Style.MinWidth, it.Style.MaxWidth
	if ax.vertical {
		minL, maxL = it.Style.MinHeight, it.Style.MaxHeight
	}
	// Maximum.
	if maxL.Unit == gcss.UnitAuto {
		maxMain = -1
	} else {
		maxMain, _ = resolveLen(maxL, it.Style.FontSizePt, 0)
	}
	// Minimum: an explicit min resolves directly. min:auto triggers the automatic
	// minimum size (CSS Flexbox §4.5): the min-content size, capped by an explicit main
	// size or max (the spec's min()). For row, the content min size is measureMinContent.
	if minL.Unit == gcss.UnitAuto {
		autoMin := e.mainMinContentSize(ctx, it, ax, availCross)
		// Cap by a definite main size if smaller (a fixed-size item's auto-min is its size).
		mainLen := it.Style.Width
		if ax.vertical {
			mainLen = it.Style.Height
		}
		if mainLen.Unit != gcss.UnitAuto && mainLen.Unit != gcss.UnitPercent {
			if v, _ := resolveLen(mainLen, it.Style.FontSizePt, 0); v < autoMin {
				autoMin = v
			}
		}
		if maxMain >= 0 && maxMain < autoMin {
			autoMin = maxMain
		}
		minMain = autoMin
	} else {
		minMain, _ = resolveLen(minL, it.Style.FontSizePt, 0)
	}
	return minMain, maxMain
}

// mainContentSize is the item's intrinsic MAIN-axis content size, dispatched on the
// axis so the width/height choice is made in exactly one place: a row's main axis is
// horizontal (max-content width), a column's is vertical (content height, measured by
// laying the item out — see measureColumnMainContent).
func (e *Engine) mainContentSize(ctx context.Context, it *cssbox.Box, ax flexAxis, availCross float64) float64 {
	if ax.vertical {
		return e.measureColumnMainContent(ctx, it, availCross)
	}
	return e.measureMaxContent(ctx, it)
}

// mainMinContentSize is the min-content counterpart used for the automatic minimum
// size (CSS Flexbox §4.5). On a column the min-content HEIGHT of a block is its
// height at its widest reasonable measure, which for this engine's purposes is the
// same laid-out height mainContentSize computes: a block's height does not shrink
// further once its width is fixed.
func (e *Engine) mainMinContentSize(ctx context.Context, it *cssbox.Box, ax flexAxis, availCross float64) float64 {
	if ax.vertical {
		return e.measureColumnMainContent(ctx, it, availCross)
	}
	return e.measureMinContent(ctx, it)
}

// columnItemCrossWidth resolves the CROSS-axis (horizontal) measure a column flex
// item is laid out at: its definite width when it has one, else its max-content
// width. availCross, when >= 0, clamps the max-content result to the container's
// inner cross size — an auto-width item must not be measured wider than the
// container it sits in (a paragraph of prose has a max-content width equal to the
// whole unwrapped string, which would otherwise overflow the container by a large
// multiple and, worse, be the width its content height is measured at).
//
// A definite width of 0 is pathological; fall back to max-content rather than
// laying out at zero width.
func (e *Engine) columnItemCrossWidth(ctx context.Context, it *cssbox.Box, availCross float64) float64 {
	if it.Style.Width.Unit != gcss.UnitAuto && it.Style.Width.Unit != gcss.UnitPercent {
		if v, _ := resolveLen(it.Style.Width, it.Style.FontSizePt, 0); v > 0 {
			return v
		}
	}
	w := e.measureMaxContent(ctx, it)
	if availCross >= 0 && w > availCross {
		w = availCross
	}
	return w
}

// measureColumnMainContent returns a column flex item's content HEIGHT — its main-axis
// content contribution — by laying it out at its cross-axis width and reading the
// resulting fragment height.
//
// This is the vertical counterpart of measureMaxContent. It exists because the main
// axis of a column container is vertical, so the intrinsic main size is a height;
// measureMaxContent returns a WIDTH and using it as a column item's main base size
// compares a horizontal measure against a vertical budget. It follows the same
// two-phase pattern grid already uses for row-track sizing (lay the item out at the
// known cross measure, then read back frag.H).
//
// Returns 0 if the item produces no fragment.
func (e *Engine) measureColumnMainContent(ctx context.Context, it *cssbox.Box, availCross float64) float64 {
	w := e.columnItemCrossWidth(ctx, it, availCross)
	pos := &positionedContext{}
	res := e.layoutBlock(ctx, it, w, 0, 0, 0,
		&floatContext{cbLeft: 0, cbRight: w}, pos, posCBOwner{isPage: true})
	if res.frag == nil {
		return 0
	}
	return res.frag.H
}

// layoutFlexItem lays out one flex item's contents at its used main size and returns its
// fragment and its outer cross size. For a vertical (column) axis it delegates to
// layoutFlexItemColumn; for a horizontal (row) axis, used main = content width and the
// fragment height is the cross size.
func (e *Engine) layoutFlexItem(ctx context.Context, it *cssbox.Box, ax flexAxis, usedMain, availCross float64) (*Fragment, float64) {
	if ax.vertical {
		return e.layoutFlexItemColumn(ctx, it, usedMain, availCross) // column axis: cross = width, main = height
	}
	pos := &positionedContext{}
	res := e.layoutBlock(ctx, it, usedMain, 0, 0, 0,
		&floatContext{cbLeft: 0, cbRight: usedMain}, pos, posCBOwner{isPage: true})
	frag := res.frag
	cross := 0.0
	if frag != nil {
		cross = frag.H
		consumePendingPositioned(frag, res.pendingPositioned)
		e.resolveAbsolute(ctx, pos, frag, usedMain, frag.H)
	}
	return frag, cross
}

// layoutFlexItemColumn lays out a column-axis flex item: the used main size is the
// item's HEIGHT, and its width comes from the cross axis (its definite width, else the
// container will stretch/shrink it — for now lay out at the item's own width if definite,
// otherwise at its max-content width as the natural cross size). Returns the fragment and
// its outer cross size (the width). The fragment's height is pinned to usedMain.
func (e *Engine) layoutFlexItemColumn(ctx context.Context, it *cssbox.Box, usedMain, availCross float64) (*Fragment, float64) {
	crossW := e.columnItemCrossWidth(ctx, it, availCross)
	pos := &positionedContext{}
	res := e.layoutBlock(ctx, it, crossW, 0, 0, 0,
		&floatContext{cbLeft: 0, cbRight: crossW}, pos, posCBOwner{isPage: true})
	frag := res.frag
	if frag != nil {
		// Pin the fragment height to the used main size (the flexed height).
		frag.H = usedMain
		consumePendingPositioned(frag, res.pendingPositioned)
		e.resolveAbsolute(ctx, pos, frag, crossW, usedMain)
	}
	return frag, crossW
}

// placeFlexFragment positions a laid-out item fragment at the given main/cross offsets,
// resizing it to (usedMain × crossSize) along the axis and translating its descendants.
func placeFlexFragment(frag *Fragment, ax flexAxis, originMain, originCross, mainPos, crossPos, mainSize, crossSize float64) {
	if frag == nil {
		return
	}
	x, y, w, h := ax.rect(originMain, originCross, mainPos, crossPos, mainSize, crossSize)
	stretchCellFragment(frag, x, y, w, h) // reuse the table helper: sets X/Y/W/H + shifts children
	// A flex item establishes an independent formatting context (CSS Flexbox §4). Mark
	// the fragment a BFC so AppendItems flattens it atomically (decorations → floats →
	// content → positioned), emitting its positioned layer — an abs/fixed descendant of
	// a flex item is otherwise dropped at paint time (it is resolved onto the fragment's
	// Positioned slice, which only the atomic AppendItems path emits).
	frag.IsBFC = true
}

// flexMargins resolves an item's margins onto the flex axes, reporting which main-axis
// margins are `auto`.
//
// Flex layout was previously margin-blind entirely: an item's size and position came
// from its border box, so `margin-top` on a child of a flex column did nothing at all
// while the identical rule on a block child worked. That reads as "the rule did not
// apply", which is the most expensive kind of failure to diagnose.
//
// Percentages resolve against the container's INLINE size on both axes (CSS 2.1 §8.3),
// which is why innerMain is only the right basis for a row container; for a column the
// inline size is the cross measure. The caller passes the main size and this corrects
// for the axis.
func flexMargins(it *cssbox.Box, ax flexAxis, innerMain float64) (mainStart, mainEnd, crossStart, crossEnd float64, autoStart, autoEnd bool) {
	fs := it.Style.FontSizePt
	get := func(l gcss.Length) (float64, bool) {
		if l.Unit == gcss.UnitAuto {
			return 0, true
		}
		v, _ := resolveLen(l, fs, innerMain)
		return v, false
	}
	top, autoTop := get(it.Style.MarginTop)
	right, autoRight := get(it.Style.MarginRight)
	bottom, autoBottom := get(it.Style.MarginBottom)
	left, autoLeft := get(it.Style.MarginLeft)

	if ax.vertical {
		// Column: main is vertical.
		return top, bottom, left, right, autoTop, autoBottom
	}
	return left, right, top, bottom, autoLeft, autoRight
}

// withDefiniteHeight temporarily gives a box a definite height for the duration of one
// layout, returning a function that restores the original.
//
// A flex item's cross size is resolved by the flex algorithm, but its INTERIOR is laid
// out by a nested formatting context that reads the box's own style. Writing the height
// onto the fragment after that layout is too late: anything inside that needs a
// definite height — justify-content, align-items, a percentage height — has already
// resolved against auto.
//
// Mutating the box is safe here because layout is single-threaded and the value is
// restored before the caller returns; the box tree is read-only across the CONCURRENT
// render fan-out, which happens strictly after layout completes.
func withDefiniteHeight(b *cssbox.Box, h float64) func() {
	if b == nil || h < 0 {
		return func() {}
	}
	prev := b.Style.Height
	b.Style.Height = gcss.Length{Value: h, Unit: gcss.UnitPx}
	return func() { b.Style.Height = prev }
}
