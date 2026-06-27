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
// at the group's max first baseline, and returns a CONSERVATIVE extra cross (block)
// size for the group: the largest downward shift applied. This is a safe upper bound,
// not the exact extra height — it can slightly over-expand the row/line when a shifted
// item is shorter than its baseline distance — which callers (grid rows, the flex line,
// table rows) grow the cross extent by. Items with ok=false (no baseline) or
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
	extra := 0.0
	for i, it := range items {
		if !it.baseline || !oks[i] {
			continue
		}
		dy := maxBaseline - offs[i]
		if dy > 0 {
			translateFragment(it.frag, 0, dy)
			if dy > extra {
				extra = dy
			}
		}
	}
	return extra
}
