package css

import (
	"context"
	"math"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
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
func (e *Engine) layoutFlex(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext) interior {
	_ = bandOriginY
	_ = fc
	ax := axisFor(b.Style.FlexDirection, effectiveDirection(b))
	if b.Style.FlexWrap == "wrap" || b.Style.FlexWrap == "wrap-reverse" {
		e.logf("css layout: flex-wrap:%s not supported; laying out single-line (nowrap)", b.Style.FlexWrap)
	}

	items := flexItemBoxes(b)
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
	totalGap := mainGap * float64(len(items)-1)

	// If the main size is indefinite (column auto-height), there is no free space:
	// the container is sized to the items, so used main = hypothetical for every item.
	var usedMain []float64
	if !mainDefinite {
		usedMain = make([]float64, len(items))
		sum := totalGap
		for i := range sizings {
			usedMain[i] = sizings[i].hypothetical
			sum += usedMain[i]
		}
		// For column-reverse, innerMain must equal the content extent so the reverse
		// formula (innerMain - mainPos - usedMain[i]) flips within the content bounds.
		innerMain = sum
	} else {
		usedMain = resolveFlexibleLengths(sizings, innerMain, totalGap)
	}

	// Lay out each item's contents at its used main size; capture its cross size.
	frags := make([]*Fragment, len(items))
	crossSizes := make([]float64, len(items))
	for i, it := range items {
		frags[i], crossSizes[i] = e.layoutFlexItem(ctx, it, ax, usedMain[i], availCross)
	}

	// Line cross size: for a single-line flex container the line's cross size is the
	// container's inner cross size when that is DEFINITE (CSS Flexbox §9.4 step 8 — the
	// line stretches to fill), else the max item's outer cross size. A definite cross size
	// is what makes align-items:center/flex-end align within the container's extent rather
	// than the tallest item's. (lineCross is still floored at the max item size so an item
	// taller than a too-small definite container is not clipped by the line metric.)
	lineCross := 0.0
	for _, cs := range crossSizes {
		if cs > lineCross {
			lineCross = cs
		}
	}
	if def, ok := e.flexCrossSize(b, contentW, ax); ok && def > lineCross {
		lineCross = def
	}

	// Total main extent consumed by items + gaps (used for reverse placement and the
	// column content height).
	consumed := totalGap
	for i := range items {
		consumed += usedMain[i]
	}

	// Distribute free main space according to justify-content.
	freeMain := innerMain - consumed
	leading, between := justifyOffsets(b.Style.JustifyContent, freeMain, len(items))

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

	// Position items along the main axis, applying cross-axis alignment per item.
	// For reverse directions, item i sits at (innerMain - mainPos - usedMain[i]) so it
	// packs from the main-end. The leading and between offsets accumulate in mainPos
	// identically in both cases.
	mainPos := leading
	for i := range items {
		align := resolvedAlign(b, items[i])
		itemCross := crossSizes[i]

		// stretch: grow an auto-cross item to the line cross size and relayout its
		// contents at that cross measure (a row item's width is its main size, which is
		// fixed; stretch grows its HEIGHT — pin the fragment height to lineCross).
		if align == "stretch" && !itemHasDefiniteCross(items[i], ax) {
			frags[i], itemCross = e.stretchFlexItem(ctx, items[i], ax, usedMain[i], lineCross)
		}

		crossPos := crossOffset(align, lineCross, itemCross, ax)
		pos := mainPos
		if ax.reverseMain {
			pos = innerMain - mainPos - usedMain[i]
		}
		placeFlexFragment(frags[i], ax, originMain, originCross, pos, crossPos, usedMain[i], itemCross)
		mainPos += usedMain[i] + mainGap + between
	}

	// Baseline alignment post-pass: for a ROW container, items with baseline alignment
	// form one group. alignBaselineGroup shifts each participating item DOWN so its first
	// baseline coincides with the group maximum, returning the largest downward shift as a
	// conservative extra cross extent. Grow lineCross by extra so the line encloses the
	// shifted items. For a COLUMN container, the cross axis is horizontal and there is no
	// meaningful text baseline — baseline falls back to flex-start (crossOffset already
	// returns 0 for baseline, which is correct for column). When no item is baseline-aligned,
	// alignBaselineGroup returns 0 and nothing shifts (byte-identical for non-baseline flex).
	if !ax.vertical {
		var group []baselineItem
		for i := range items {
			group = append(group, baselineItem{frag: frags[i], baseline: resolvedAlign(b, items[i]) == "baseline"})
		}
		extra := alignBaselineGroup(group)
		if extra > 0 {
			lineCross += extra
		}
	} else {
		// column: log once if any item requests baseline (fallback to flex-start).
		for i := range items {
			if resolvedAlign(b, items[i]) == "baseline" {
				e.logf("css layout: align-items/align-self baseline not supported on column flex; using flex-start")
				break
			}
		}
	}

	contentHeight := lineCross
	if ax.vertical {
		contentHeight = consumed
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

// itemSizing computes a flex item's flex base size, hypothetical main size, and used
// min/max main size: the numeric inputs to resolveFlexibleLengths. The base size comes
// from flexBaseSize (flex-basis: auto/content/percentage/length, CSS Flexbox §9.2); the
// used min/max come from usedMinMaxMain (explicit min/max plus the §4.5 automatic
// minimum); the hypothetical main size is the base clamped to [minMain, maxMain].
func (e *Engine) itemSizing(ctx context.Context, it *cssbox.Box, ax flexAxis, innerMain, availCross float64) flexItemSizing {
	base := e.flexBaseSize(ctx, it, ax, innerMain, availCross)
	minMain, maxMain := e.usedMinMaxMain(ctx, it, ax, availCross)
	return flexItemSizing{
		base:         base,
		hypothetical: clampF(base, minMain, maxMain),
		grow:         it.Style.FlexGrow,
		shrink:       it.Style.FlexShrink,
		minMain:      minMain,
		maxMain:      maxMain,
	}
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
