package extract

import (
	"sort"
	"strings"
)

// This file groups captured glyphs into words and lines, following the
// tolerance-based algorithm pdfplumber uses (extract_words): glyphs are sorted
// into reading order, then split into words wherever the horizontal gap between
// adjacent glyphs exceeds a fraction of the font size (or an explicit space
// glyph), and split into lines wherever the baseline jumps beyond a vertical
// tolerance. The tolerances scale with font size so a 24pt heading and an 8pt
// footnote both group correctly.

// word is a run of glyphs with no intra-word gap, on one baseline. text is the
// decoded string; x0/x1 are its left/right edges; y is the baseline; size is the
// representative (first-glyph) font size. bold/italic are true when the whole
// word is bold/italic, used for emphasis in the lowered output.
type word struct {
	text         string
	x0, x1, y    float64
	size         float64
	bold, italic bool
}

// line is a run of words sharing a baseline, left-to-right. x0/x1 are the line's
// horizontal extent; y is the baseline; size is the median glyph size on the line
// (the robust per-line size used for heading classification).
type line struct {
	y, x0, x1 float64
	size      float64
	words     []word
}

// Word/line grouping tolerances, as fractions of the glyph's font size. Sizing
// them to the font (rather than fixed points) makes grouping scale-invariant.
const (
	// wordGapFrac: a horizontal gap wider than this fraction of the font size
	// starts a new word. Measured against this repo's own pdfwrite output (which
	// emits no space glyphs — a space is purely a gap in x between the previous
	// glyph's right edge and the next glyph's origin), intra-word letter gaps sit
	// at ~0em (origins meet the previous right edge, occasionally a hair negative
	// from rounding) while inter-word space gaps land at ~0.25em, jittering
	// between 0.249 and 0.250 from float rounding. pdfplumber's default
	// x_tolerance is likewise ~0.25em. A threshold *at* 0.25em therefore sits
	// exactly on the space width and fires inconsistently, so we set the boundary
	// at 0.15em — safely above intra-word kerning (near 0, and never above ~0.1em
	// even for tightly-kerned faces) yet well below a real space — so genuine
	// spaces always split and kerned letters never do.
	wordGapFrac = 0.15
	// lineGapFrac: a baseline shift larger than this fraction of the font size
	// starts a new line. Half the font size cleanly separates stacked lines while
	// tolerating sub/superscript jitter on one baseline.
	lineGapFrac = 0.5
)

// buildLines groups a page's glyphs into lines of words in reading order. It sorts
// glyphs top-to-bottom then left-to-right, accumulates them into lines by baseline
// proximity, and within each line into words by horizontal gap. Glyphs with rune 0
// (no font mapping) contribute their geometry with a U+FFFD placeholder so column
// structure is preserved even when the text is unmappable. Pure-space glyphs act as
// hard word breaks and are not themselves emitted.
func buildLines(glyphs []glyph) []line {
	if len(glyphs) == 0 {
		return nil
	}
	// Sort into reading order: primarily by baseline (y ascending == top-to-bottom
	// in the y-down device frame), secondarily by x. A small y-band merge happens in
	// the accumulation loop, so exact-equal y is not required here.
	sorted := make([]glyph, len(glyphs))
	copy(sorted, glyphs)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !nearly(sorted[i].y, sorted[j].y, lineTolOf(sorted[i], sorted[j])) {
			return sorted[i].y < sorted[j].y
		}
		return sorted[i].x < sorted[j].x
	})

	var lines []line
	var cur []glyph
	curY := sorted[0].y
	flush := func() {
		if len(cur) == 0 {
			return
		}
		lines = append(lines, assembleLine(cur))
		cur = cur[:0]
	}
	for _, g := range sorted {
		if len(cur) > 0 && !nearly(g.y, curY, lineTolOf(g, cur[len(cur)-1])) {
			flush()
		}
		if len(cur) == 0 {
			curY = g.y
		}
		cur = append(cur, g)
	}
	flush()
	return lines
}

// assembleLine turns one baseline's glyphs (already sorted by x) into a line,
// splitting them into words at horizontal gaps or explicit space glyphs. It also
// computes the line's extent and median glyph size.
func assembleLine(glyphs []glyph) line {
	// Ensure left-to-right order within the line (the outer sort ordered by y then
	// x, but a line-merge may interleave slightly different baselines).
	sort.SliceStable(glyphs, func(i, j int) bool { return glyphs[i].x < glyphs[j].x })

	var words []word
	var sb strings.Builder
	var wx0, wx1, wy, wsize float64
	wbold, witalic := true, true
	started := false
	var prevX1 float64

	endWord := func() {
		if !started || sb.Len() == 0 {
			sb.Reset()
			started = false
			return
		}
		words = append(words, word{
			text:   sb.String(),
			x0:     wx0,
			x1:     wx1,
			y:      wy,
			size:   wsize,
			bold:   wbold,
			italic: witalic,
		})
		sb.Reset()
		started = false
	}

	for _, g := range glyphs {
		if g.isSpace {
			endWord() // an explicit space is a hard word break
			prevX1 = g.x1
			continue
		}
		gap := g.x - prevX1
		if started && gap > wordGapFrac*maxf(g.size, wsize) {
			endWord()
		}
		if !started {
			wx0, wy, wsize = g.x, g.y, g.size
			wbold, witalic = true, true
			started = true
		}
		sb.WriteRune(runeOrPlaceholder(g.r))
		wx1 = g.x1
		wbold = wbold && g.bold
		witalic = witalic && g.italic
		prevX1 = g.x1
	}
	endWord()

	return line{
		y:     lineBaseline(glyphs),
		x0:    lineLeft(words),
		x1:    lineRight(words),
		size:  medianGlyphSize(glyphs),
		words: words,
	}
}

// text returns the line's words joined by single spaces (the reading text of the
// line, with intra-word runs preserved and inter-word gaps normalized to one
// space).
func (l line) text() string {
	parts := make([]string, len(l.words))
	for i, w := range l.words {
		parts[i] = w.text
	}
	return strings.Join(parts, " ")
}

// runeOrPlaceholder maps a captured rune to itself, or to U+FFFD when the font
// gave no mapping (rune 0). This keeps the word's glyph count and geometry intact
// so downstream column/word logic is unaffected by unmappable text.
func runeOrPlaceholder(r rune) rune {
	if r == 0 {
		return '�'
	}
	return r
}

// lineTolOf returns the baseline tolerance for deciding whether two glyphs share a
// line, as lineGapFrac of the larger of their sizes (so a big glyph on a line does
// not wrongly split from its neighbors).
func lineTolOf(a, b glyph) float64 {
	return lineGapFrac * maxf(maxf(a.size, b.size), 1)
}

// nearly reports whether a and b are within tol of each other.
func nearly(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

// maxf returns the larger of two float64s.
func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// lineBaseline returns the representative baseline of a line's glyphs (the median
// y), robust to a stray sub/superscript.
func lineBaseline(glyphs []glyph) float64 {
	ys := make([]float64, len(glyphs))
	for i, g := range glyphs {
		ys[i] = g.y
	}
	return median(ys)
}

// lineLeft/lineRight return the min-left / max-right of a line's words (0 when the
// line has none).
func lineLeft(words []word) float64 {
	if len(words) == 0 {
		return 0
	}
	m := words[0].x0
	for _, w := range words[1:] {
		if w.x0 < m {
			m = w.x0
		}
	}
	return m
}

func lineRight(words []word) float64 {
	if len(words) == 0 {
		return 0
	}
	m := words[0].x1
	for _, w := range words[1:] {
		if w.x1 > m {
			m = w.x1
		}
	}
	return m
}

// medianGlyphSize returns the median font size of a line's glyphs — the robust
// per-line size used to classify headings (a single oversized dropcap does not
// pull the line's size up).
func medianGlyphSize(glyphs []glyph) float64 {
	sizes := make([]float64, 0, len(glyphs))
	for _, g := range glyphs {
		if g.size > 0 {
			sizes = append(sizes, g.size)
		}
	}
	if len(sizes) == 0 {
		return 0
	}
	return median(sizes)
}

// median returns the median of vals (mutating a copy). Returns 0 for an empty
// slice.
func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := make([]float64, len(vals))
	copy(cp, vals)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

// bodyFontSize returns the document body's dominant font size: the median of the
// per-line median sizes, weighted by the number of words on each line (so a
// document with many body lines and a few headings reports the body size). It is
// the reference against which blocks.go classifies a larger line as a heading.
// Returns 0 when there are no lines.
func bodyFontSize(lines []line) float64 {
	var sizes []float64
	for _, l := range lines {
		if l.size <= 0 {
			continue
		}
		// Weight by word count so long paragraph lines dominate over short headings.
		w := len(l.words)
		if w == 0 {
			w = 1
		}
		for i := 0; i < w; i++ {
			sizes = append(sizes, l.size)
		}
	}
	return median(sizes)
}
