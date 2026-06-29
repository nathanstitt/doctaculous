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
}

func axisFor(dir string) flexAxis {
	switch dir {
	case "column":
		return flexAxis{vertical: true}
	case "column-reverse":
		return flexAxis{vertical: true, reverseMain: true}
	case "row-reverse":
		return flexAxis{reverseMain: true}
	default: // row
		return flexAxis{}
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
	ax := axisFor(b.Style.FlexDirection)
	if b.Style.Direction == "rtl" && !ax.vertical {
		e.logf("css layout: RTL flex rows not supported; laying out LTR")
	}
	if b.Style.FlexWrap == "wrap" || b.Style.FlexWrap == "wrap-reverse" {
		e.logf("css layout: flex-wrap:%s not supported; laying out single-line (nowrap)", b.Style.FlexWrap)
	}

	items := flexItemBoxes(b)
	for i := range items {
		if resolvedAlign(b, items[i]) == "baseline" {
			e.logf("css layout: align-items/align-self baseline not supported; using flex-start")
			break
		}
	}
	if len(items) == 0 {
		return interior{contentHeight: 0}
	}

	// Container inner main size. For row it is contentW; for column it is the content
	// height if definite, else indefinite (content-sized => no grow/shrink).
	innerMain, mainDefinite := e.flexMainSize(b, contentW, ax)

	// Per-item flex base size + hypothetical main size + used min/max main.
	sizings := make([]flexItemSizing, len(items))
	for i, it := range items {
		sizings[i] = e.itemSizing(ctx, it, ax, innerMain)
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
		frags[i], crossSizes[i] = e.layoutFlexItem(ctx, it, ax, usedMain[i])
	}

	// Line cross size = max item outer cross size (clamped to a definite container cross
	// size if set — deferred refinement; for now the max).
	lineCross := 0.0
	for _, cs := range crossSizes {
		if cs > lineCross {
			lineCross = cs
		}
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

		crossPos := crossOffset(align, lineCross, itemCross)
		pos := mainPos
		if ax.reverseMain {
			pos = innerMain - mainPos - usedMain[i]
		}
		placeFlexFragment(frags[i], ax, originMain, originCross, pos, crossPos, usedMain[i], itemCross)
		mainPos += usedMain[i] + mainGap + between
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
// is not auto, else the container's align-items. baseline is approximated to flex-start.
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
func crossOffset(a string, lineCross, itemCross float64) float64 {
	switch a {
	case "flex-end":
		return lineCross - itemCross
	case "center":
		return (lineCross - itemCross) / 2
	default: // flex-start, stretch, baseline(approx)
		return 0
	}
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
func (e *Engine) itemSizing(ctx context.Context, it *cssbox.Box, ax flexAxis, innerMain float64) flexItemSizing {
	base := e.flexBaseSize(ctx, it, ax, innerMain)
	minMain, maxMain := e.usedMinMaxMain(ctx, it, ax)
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
func (e *Engine) flexBaseSize(ctx context.Context, it *cssbox.Box, ax flexAxis, innerMain float64) float64 {
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
		return e.measureMaxContent(ctx, it)
	case gcss.UnitContent:
		return e.measureMaxContent(ctx, it)
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
func (e *Engine) usedMinMaxMain(ctx context.Context, it *cssbox.Box, ax flexAxis) (minMain, maxMain float64) {
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
		autoMin := e.measureMinContent(ctx, it)
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

// layoutFlexItem lays out one flex item's contents at its used main size and returns its
// fragment and its outer cross size. For a vertical (column) axis it delegates to
// layoutFlexItemColumn; for a horizontal (row) axis, used main = content width and the
// fragment height is the cross size.
func (e *Engine) layoutFlexItem(ctx context.Context, it *cssbox.Box, ax flexAxis, usedMain float64) (*Fragment, float64) {
	if ax.vertical {
		return e.layoutFlexItemColumn(ctx, it, usedMain) // column axis: cross = width, main = height
	}
	pos := &positionedContext{}
	res := e.layoutBlock(ctx, it, usedMain, 0, 0, 0,
		&floatContext{cbLeft: 0, cbRight: usedMain}, pos, posCBOwner{isPage: true})
	frag := res.frag
	cross := 0.0
	if frag != nil {
		cross = frag.H
		e.resolveAbsolute(ctx, pos, frag, usedMain, frag.H)
	}
	return frag, cross
}

// layoutFlexItemColumn lays out a column-axis flex item: the used main size is the
// item's HEIGHT, and its width comes from the cross axis (its definite width, else the
// container will stretch/shrink it — for now lay out at the item's own width if definite,
// otherwise at its max-content width as the natural cross size). Returns the fragment and
// its outer cross size (the width). The fragment's height is pinned to usedMain.
func (e *Engine) layoutFlexItemColumn(ctx context.Context, it *cssbox.Box, usedMain float64) (*Fragment, float64) {
	// Cross (width) basis: a definite width, else max-content.
	crossW := e.measureMaxContent(ctx, it)
	if it.Style.Width.Unit != gcss.UnitAuto && it.Style.Width.Unit != gcss.UnitPercent {
		// v > 0: a width:0 column item is pathological; fall back to max-content rather
		// than laying it out at zero width (graceful degradation).
		if v, _ := resolveLen(it.Style.Width, it.Style.FontSizePt, 0); v > 0 {
			crossW = v
		}
	}
	pos := &positionedContext{}
	res := e.layoutBlock(ctx, it, crossW, 0, 0, 0,
		&floatContext{cbLeft: 0, cbRight: crossW}, pos, posCBOwner{isPage: true})
	frag := res.frag
	if frag != nil {
		// Pin the fragment height to the used main size (the flexed height).
		frag.H = usedMain
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
}
