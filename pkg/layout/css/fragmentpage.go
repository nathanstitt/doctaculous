package css

import (
	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// lineSplittable reports whether a top-level block fragment can be fragmented at a line
// boundary for widows/orphans: it must establish an inline formatting context (have
// Lines), carry no break-inside: avoid, and hold no block child fragments interleaved
// with its lines (a pure-inline-content block — the dominant paragraph case). A block
// mixing block children and lines (an anonymous-block fixup) is NOT line-split here; it
// falls back to whole-block placement (overflow if too tall), because the post-pass does
// not re-derive block/line interleave order. Floats/positioned children are out of flow
// and do not disqualify the block (its in-flow content is still pure inline).
func lineSplittable(b *Fragment) bool {
	if b == nil || len(b.Lines) < 2 || keptInsideAvoid(b) {
		return false
	}
	for _, c := range b.Children {
		if !c.IsFloat && !c.IsPositioned {
			return false // an in-flow block child interleaves with the lines
		}
	}
	return true
}

// splitResult is the outcome of attempting to split a block across a page boundary.
type splitResult struct {
	head *Fragment // the part staying on the current page (nil if the block moves whole)
	tail *Fragment // the part flowing to the next page (nil if the whole block fits)
}

// splitBlockForPage decides how a line-splittable block b is placed when the current
// page's content bottom is pageBottom (page space). It honors CSS widows/orphans:
//
//   - If every line fits (b's bottom ≤ pageBottom), it does not split: {head: b}.
//   - It finds k = the number of leading lines that fit above pageBottom, then clamps k
//     by orphans (≥ orphans lines must remain on this page) and widows (≥ widows lines
//     must carry to the next page). If the clamps cannot both be met (the block is too
//     short, n < widows+orphans, or fewer than orphans lines fit), the block moves whole
//     to the next page: {tail: b}.
//   - Otherwise it splits into head (lines [0,k)) and tail (lines [k,n)), each a shallow
//     clone of b with its line subset, the split-side border/padding suppressed, and the
//     tail's lines/height shifted so the tail's content starts at the tail block's top.
//
// b must be lineSplittable. The split is page-space only (it partitions Lines and clones
// the Fragment struct, sharing glyph outlines — read-only *render.Path); no relayout.
func splitBlockForPage(b *Fragment, pageBottom float64, widows, orphans int) splitResult {
	n := len(b.Lines)
	ed := blockEdges(b)
	contentTop := b.Y + ed.bT + ed.pT
	lh := lineHeightOf(b)

	// k = lines whose bottom fits above pageBottom. Line i occupies
	// [contentTop+i*lh, contentTop+(i+1)*lh] (uniform line height — the common case).
	k := 0
	for i := 0; i < n; i++ {
		lineBottom := contentTop + float64(i+1)*lh
		if lineBottom <= pageBottom+0.5 { // small tolerance for fp
			k = i + 1
		} else {
			break
		}
	}
	if k >= n {
		return splitResult{head: b} // everything fits; no split
	}

	if widows < 1 {
		widows = 1
	}
	if orphans < 1 {
		orphans = 1
	}
	// Widows: the tail (n-k lines) must have ≥ widows lines; pull lines back if needed.
	if n-k < widows {
		k = n - widows
	}
	// Orphans: the head (k lines) must have ≥ orphans lines. If that is impossible
	// (k < orphans after the widows clamp, or the block can't satisfy both because
	// n < widows+orphans), move the whole block to the next page.
	if k < orphans {
		return splitResult{tail: b}
	}

	head := splitHead(b, k, ed, lh)
	tail := splitTail(b, k, ed, lh)
	return splitResult{head: head, tail: tail}
}

// splitHead builds the head fragment: a shallow clone of b keeping lines [0,k), its
// height shrunk to end just below line k-1, and its BOTTOM border/padding suppressed
// (CSS box-decoration-break: slice — a box split across a break does not repeat the
// break-side edge). The head stays on the current page (its Y is unchanged).
func splitHead(b *Fragment, k int, ed edges, lh float64) *Fragment {
	h := *b
	h.Lines = append([]LineFragment(nil), b.Lines[:k]...)
	// New border-box height: top edge + k lines of content + bottom edge, but with the
	// bottom border/padding suppressed (slice), so just top edge + k*lh.
	h.H = ed.bT + ed.pT + float64(k)*lh
	h.Border[layout.EdgeBottom] = BorderEdge{} // suppress the split-side edge
	// The head has no children of its own beyond out-of-flow ones; an out-of-flow child
	// (float/abs) stays with the head (it was positioned in this block's space). Keep
	// Children as-is — they are rare on a paragraph and ride the head page.
	return &h
}

// splitTail builds the tail fragment: a shallow clone of b keeping lines [k,n) AT THEIR
// ORIGINAL page-space positions, its TOP border/padding suppressed, and its border-box
// Y moved DOWN to the top of the first kept line so the fragment invariant (lines sit at
// Y + topEdge + i*lh + ascent) still holds. The kept lines are NOT moved — only Y and H
// change — so a recursive re-split of the tail (when it itself overflows the next page)
// recomputes a consistent contentTop. The bucketer shifts the whole tail to the next
// page's local frame via the usual per-page shift.
func splitTail(b *Fragment, k int, ed edges, lh float64) *Fragment {
	tl := *b
	n := len(b.Lines)
	tl.Lines = append([]LineFragment(nil), b.Lines[k:]...)
	m := n - k // tail line count
	// The first kept line's band top is the original content top + k*lh.
	firstKeptTop := b.Y + ed.bT + ed.pT + float64(k)*lh
	// With the top edge suppressed, the tail's content box starts at its border-box top
	// plus only its padding-top, so set Y so content top == firstKeptTop.
	tl.Y = firstKeptTop - ed.pT
	// Tail border box: top edge SUPPRESSED (slice) — no bT/pT above the content — then
	// m lines, then the original bottom edge.
	tl.H = ed.pT + float64(m)*lh + ed.pB + ed.bB
	tl.Border[layout.EdgeTop] = BorderEdge{} // suppress the split-side edge
	return &tl
}

// blockEdges returns a block fragment's resolved top/bottom border + padding edges,
// reading them from its source Box (the border widths are also on the fragment, but
// padding is not, so we resolve from the Box for both to stay consistent). A nil Box
// yields zero edges (an anonymous block — its content top is its border-box top).
func blockEdges(b *Fragment) edges {
	if b == nil || b.Box == nil {
		return edges{}
	}
	// The block's edges resolve against its containing-block width; for top/bottom
	// border+padding only the box's own values matter (percentages on vertical padding
	// resolve against the CB WIDTH, but a paragraph rarely uses %; pass the block's own
	// width as a reasonable basis). usedEdges needs a CB width — use b.W (close enough
	// for the vertical edges in the common case).
	return usedEdges(b.Box, b.W)
}

// lineHeightOf returns the uniform line-height (baseline-to-baseline spacing) of a
// multi-line block: the delta between its first two line baselines. For a 1-line block
// (not line-split) it falls back to the block's content height. This is exact for the
// common uniform-line-height paragraph; a block with mixed per-line heights uses the
// first delta as an approximation (documented).
func lineHeightOf(b *Fragment) float64 {
	if len(b.Lines) >= 2 {
		lh := b.Lines[1].BaselineY - b.Lines[0].BaselineY
		if lh > 0 {
			return lh
		}
	}
	ed := blockEdges(b)
	h := b.H - ed.bT - ed.bB - ed.pT - ed.pB
	if h > 0 {
		return h
	}
	return b.H
}
