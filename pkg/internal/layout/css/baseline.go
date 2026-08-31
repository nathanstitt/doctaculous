package css

// firstBaselineOffset returns the offset from a fragment's TOP (frag.Y) down to its
// first baseline, and ok=true when the fragment has a usable text baseline. The first
// baseline is the first in-flow line box's baseline (LineFragment.BaselineY, which is
// in the same page-space Y frame as frag.Y), or recursively the first in-flow block
// child's first baseline. A fragment with no line boxes and no block children with a
// baseline (e.g. an empty or image-only box) returns ok=false — the caller treats it
// as "synthesized baseline = margin-box bottom" and falls back to start alignment
// (CSS Box Alignment §9.4.3, kept simple).
func firstBaselineOffset(frag *Fragment) (float64, bool) {
	if frag == nil {
		return 0, false
	}
	// Direct inline lines on this fragment.
	if len(frag.Lines) > 0 {
		return frag.Lines[0].BaselineY - frag.Y, true
	}
	// Recurse into in-flow block children (first one with a baseline wins).
	for _, c := range frag.Children {
		if off, ok := firstBaselineOffset(c); ok {
			return (c.Y + off) - frag.Y, true
		}
	}
	return 0, false
}

// baselineItem pairs a fragment with whether it participates in baseline alignment.
type baselineItem struct {
	frag     *Fragment
	baseline bool // true => align this item's first baseline to the group baseline
}

// alignBaselineGroup shifts every participating item DOWN so its first baseline sits
// at the group's max first baseline, and returns the EXACT extra cross (block) size the
// group needs: the increase in its lowest edge, max(bottom after shift) − max(bottom
// before shift) over the participating items — which callers (grid rows, the flex line,
// table rows) grow the cross extent by. (This is tighter than the largest single shift,
// which over-expands when the most-shifted item is not the one reaching lowest — I5.)
// Items with ok=false (no baseline) or
// baseline=false are not shifted and do not affect the group baseline (the spec's
// "no baseline → start" fallback). The shift is applied via translateFragment (moves
// the fragment's own rect + its descendants; note the Positioned layer is NOT moved,
// matching shiftCellContent/placeFlexFragment — an abs/fixed descendant of a shifted
// item is not re-placed, a pre-existing cross-axis-alignment limitation).
func alignBaselineGroup(items []baselineItem) float64 {
	maxBaseline := 0.0
	offs := make([]float64, len(items))
	oks := make([]bool, len(items))
	for i, it := range items {
		if !it.baseline {
			continue
		}
		off, ok := firstBaselineOffset(it.frag)
		offs[i], oks[i] = off, ok
		if ok && off > maxBaseline {
			maxBaseline = off
		}
	}
	// The extra cross size the group needs is the increase in the group's lowest edge:
	// max(bottom after shifting) − max(bottom before shifting), over the participating
	// items. This is the EXACT growth (a tighter value than the largest single shift,
	// which over-expands when the most-shifted item is not the one reaching lowest): an
	// item shifted down by dy whose bottom was already high may not extend the group at
	// all, while a small shift on a tall item can. (I5 refinement.)
	maxInitialBottom, maxFinalBottom := 0.0, 0.0
	seen := false
	for i, it := range items {
		if !it.baseline || !oks[i] {
			continue
		}
		dy := maxBaseline - offs[i]
		if dy < 0 {
			dy = 0
		}
		initialBottom := it.frag.Y + it.frag.H
		finalBottom := initialBottom + dy
		if !seen || initialBottom > maxInitialBottom {
			maxInitialBottom = initialBottom
		}
		if !seen || finalBottom > maxFinalBottom {
			maxFinalBottom = finalBottom
		}
		seen = true
		if dy > 0 {
			translateFragment(it.frag, 0, dy)
		}
	}
	extra := maxFinalBottom - maxInitialBottom
	if extra < 0 {
		extra = 0
	}
	return extra
}
