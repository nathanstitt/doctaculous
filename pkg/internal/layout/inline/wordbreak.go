package inline

// Mid-word line breaking: CSS Text 3 `overflow-wrap` and `word-break`.
//
// The breaker's default opportunity set is whitespace only, so a long unbroken token
// (a URL, a hash, a German compound) fills past its box and overflows. These two
// properties add opportunities INSIDE a word, and they differ in a way that is easy to
// get wrong, so the distinction is modelled explicitly rather than folded into one flag:
//
//   - `word-break: break-all` is EAGER. It makes every inter-character position a break
//     opportunity, on the same footing as a space. A word that would have fitted on the
//     next line by itself still gets chopped at the current line's edge.
//   - `overflow-wrap: break-word` is a LAST RESORT. Normal breaking runs first; only when
//     the resulting line still does not fit — because the word is wider than the whole
//     line box, so no amount of soft-wrapping would help — is the word broken. A word
//     that fits on a line of its own is moved down whole.
//   - `overflow-wrap: anywhere` breaks in the same last-resort position as break-word,
//     but ALSO affects intrinsic sizing: min-content shrinks to the widest grapheme
//     cluster rather than the widest word. `break-word` deliberately does not, which is
//     the entire practical difference between them (a flex item or table cell sized from
//     min-content stays word-wide under break-word and collapses under anywhere).
//
// Every mid-word break lands on a grapheme-cluster boundary (see grapheme.go). CSS Text 3
// §5.3 forbids breaking within a cluster, and the visible failure — a combining accent or
// half a flag emoji stranded at the start of a line — is exactly the kind of thing a
// naive rune-index break produces.

// WordBreakMode is the per-run mid-word break policy: the combination of the CSS
// `overflow-wrap` (a.k.a. `word-wrap`) and `word-break` properties reduced to the
// distinct behaviors the breaker implements. The zero value is WordBreakNormal, so a
// caller that never sets it (the DOCX engine, SVG text) behaves exactly as before.
type WordBreakMode uint8

const (
	// WordBreakNormal breaks only at whitespace: `overflow-wrap: normal` with
	// `word-break: normal`, the CSS initial state.
	WordBreakNormal WordBreakMode = iota
	// WordBreakWord is `overflow-wrap: break-word` — break inside a word only as a last
	// resort, and do NOT let that affect min-content sizing.
	WordBreakWord
	// WordBreakAnywhere is `overflow-wrap: anywhere` — break inside a word as a last
	// resort AND shrink min-content to a single grapheme cluster.
	WordBreakAnywhere
	// WordBreakAll is `word-break: break-all` — every cluster boundary is an ordinary
	// break opportunity, taken greedily like a space, and min-content is one cluster.
	WordBreakAll
	// WordBreakKeepAll is `word-break: keep-all`. For CJK it forbids the implicit
	// between-ideograph break opportunities; since this engine never generates those
	// (its only implicit opportunities are spaces), keep-all is behaviorally identical
	// to normal here and is carried as a distinct value only so that a later CJK
	// opportunity pass has something to honor. It also SUPPRESSES `overflow-wrap`
	// breaking of the affected text, which is the part that is observable today.
	WordBreakKeepAll
)

// eager reports whether the mode creates ordinary (greedy, taken-before-overflow) break
// opportunities at cluster boundaries — true only for `word-break: break-all`.
func (m WordBreakMode) eager() bool { return m == WordBreakAll }

// lastResort reports whether the mode permits breaking a word that does not fit on a
// line by itself. break-all implies it too (it would have broken the word earlier
// anyway, but a line whose very first cluster overflows must still make progress).
func (m WordBreakMode) lastResort() bool {
	return m == WordBreakWord || m == WordBreakAnywhere || m == WordBreakAll
}

// AffectsMinContent reports whether the mode makes min-content collapse to a single
// grapheme cluster. `anywhere` and `break-all` do; `break-word` explicitly does not
// (CSS Text 3 §5.5) — that asymmetry is the whole reason the two are separate values.
//
// Exported because intrinsic sizing lives in the CSS engine (pkg/layout/css), not here:
// the measurer strips the mode from glyphs for which it returns false before breaking at
// width 0, so a break-word box still measures one word wide.
func (m WordBreakMode) AffectsMinContent() bool {
	return m == WordBreakAnywhere || m == WordBreakAll
}

// clusterStart reports whether a mid-word break may be taken immediately BEFORE
// glyphs[i] — that is, whether i begins a new grapheme cluster with respect to the
// preceding glyph. i must be > 0 (a break before the first glyph of the line is not a
// break at all).
//
// Boundary decisions run on the glyphs' source runes (Glyph.Runes), so a complex-script
// cluster that shaping already fused into one glyph is indivisible here as well: it has
// a single glyph and therefore a single candidate position. A glyph with no runes (a
// synthesized marker, an atomic box, whitespace) is treated as its own cluster, which is
// the safe answer — it is never merged into a neighbour's cluster.
func clusterStart(glyphs []Glyph, i int) bool {
	if i <= 0 || i >= len(glyphs) {
		return false
	}
	// An atomic inline box is an unbreakable unit but is itself a boundary on both
	// sides; the same goes for a hard break glyph.
	if glyphs[i].Atomic != nil || glyphs[i-1].Atomic != nil {
		return true
	}
	if glyphs[i].Break || glyphs[i-1].Break {
		return true
	}
	prevRunes, nextRunes := glyphs[i-1].Runes, glyphs[i].Runes
	if len(prevRunes) == 0 || len(nextRunes) == 0 {
		// No rune identity to judge with (whitespace glyphs carry none, and so does a
		// synthesized outline). Treat the join as a boundary: whitespace already is one,
		// and refusing to break at an unknown position would merely forgo an opportunity
		// while pretending to know something we do not.
		return true
	}
	// The two context-sensitive rules need history from BEFORE the previous glyph, so
	// replaying only that glyph's own runes is not enough:
	//
	//   - GB11 asks whether the ZWJ was preceded by Extended_Pictographic Extend*. With
	//     one rune per glyph, the ZWJ and the emoji before it are different glyphs.
	//   - GB12/GB13 need the PARITY of the whole regional-indicator run, so that a flag's
	//     two halves stay together while adjacent flags may be separated. Parity is a
	//     property of the run, not of one glyph.
	//
	// So walk back to the start of the influencing run and replay forward. The scan stops
	// at the first glyph that cannot contribute (no runes, or an atomic/break glyph),
	// which bounds it: only emoji sequences and RI runs extend it at all, and both are
	// short in practice. Everything else terminates after one step.
	start := i - 1
	for start > 0 && contributesGraphemeState(glyphs[start-1]) {
		start--
	}
	var st graphemeState
	var lastC gbClass
	for j := start; j < i; j++ {
		for _, r := range glyphs[j].Runes {
			lastC = graphemeClass(r)
			st.advance(r, lastC)
		}
	}
	next := nextRunes[0]
	return graphemeBoundary(next, lastC, graphemeClass(next), st)
}

// contributesGraphemeState reports whether g can affect the grapheme state a LATER
// boundary decision reads — i.e. whether the backward scan in clusterStart must keep
// going through it. Only two things carry state across a glyph boundary: an emoji ZWJ
// sequence (Extended_Pictographic, Extend, and ZWJ runes) and a regional-indicator run.
// Any other glyph resets the state to zero, so the scan can stop at it.
func contributesGraphemeState(g Glyph) bool {
	if g.Atomic != nil || g.Break || len(g.Runes) == 0 {
		return false
	}
	for _, r := range g.Runes {
		switch graphemeClass(r) {
		case gbExtend, gbZWJ, gbRegionalIndicator:
			continue
		default:
			if isExtendedPictographic(r) {
				continue
			}
			return false
		}
	}
	return true
}

// lastClusterBreakBefore returns the largest index in (0, idx] at which a mid-word break
// may be taken, or -1 if none. It is the eager (`word-break: break-all`) analogue of
// lastSpaceBefore: the caller keeps glyphs[:brk] on the line and carries glyphs[brk:] —
// note that, unlike a space break, NO glyph is consumed, because the break falls between
// two characters that both remain visible.
//
// A break is never taken at or before a NoWrap glyph: a `white-space: nowrap` span must
// stay on one line, and that outranks a mid-word opportunity exactly as it outranks a
// space.
func lastClusterBreakBefore(glyphs []Glyph, idx int) int {
	if idx >= len(glyphs) {
		idx = len(glyphs) - 1
	}
	for i := idx; i > 0; i-- {
		if glyphs[i].NoWrap || glyphs[i-1].NoWrap {
			continue
		}
		if !glyphs[i].WordBreak.eager() {
			continue
		}
		if clusterStart(glyphs, i) {
			return i
		}
	}
	return -1
}

// firstClusterBreakFitting returns the largest index in (0, idx] at which a LAST-RESORT
// mid-word break may be taken (`overflow-wrap: break-word`/`anywhere`), or -1 if none.
// It differs from lastClusterBreakBefore only in which glyphs opt in — the geometry of
// the break is identical — so the two are kept as thin wrappers over one scan to
// guarantee they cannot drift apart.
func firstClusterBreakFitting(glyphs []Glyph, idx int) int {
	if idx >= len(glyphs) {
		idx = len(glyphs) - 1
	}
	for i := idx; i > 0; i-- {
		if glyphs[i].NoWrap || glyphs[i-1].NoWrap {
			continue
		}
		if !glyphs[i].WordBreak.lastResort() {
			continue
		}
		if clusterStart(glyphs, i) {
			return i
		}
	}
	return -1
}
