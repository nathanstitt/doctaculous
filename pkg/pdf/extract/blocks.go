package extract

import (
	"sort"
	"strings"
	"unicode"
)

// This file recovers reading order and block structure from a page's lines.
//
// Reading order uses the XY-cut algorithm (Nagy & Seth, 1984; the recursive
// X-Y-cut / "projection profile cutting" of document image analysis): a region is
// recursively split at the widest whitespace "valley", alternating between a
// vertical cut (splitting columns, left group emitted fully before the right) and a
// horizontal cut (splitting stacked blocks). When no wide-enough valley remains,
// the region's lines are in reading order and are grouped into blocks.
//
// A block is a maximal run of consecutive lines with similar left-alignment and a
// small inter-line gap. Each block is then classified — heading, list item, or
// paragraph — from its line sizes and leading glyphs.

// blockKind is the classified role of a block.
type blockKind int

const (
	blockParagraph blockKind = iota // a run of prose lines
	blockHeading                    // a line/run notably larger than body text
	blockListItem                   // a line beginning with a bullet or ordered marker
)

// block is a classified run of lines in reading order.
type block struct {
	kind   blockKind
	lines  []line
	level  int    // heading level 1..6 (only for blockHeading)
	marker string // the list marker text incl. trailing space, e.g. "- " or "1. " (blockListItem)
}

// XY-cut / grouping tuning.
const (
	// minColGapFrac: a vertical whitespace valley must be at least this fraction of
	// the region width to be taken as a column boundary. Columns are separated by a
	// substantial gutter; a smaller gap is inter-word space, not a column.
	minColGapFrac = 0.05
	// minColGapAbs: and at least this many points wide, so a narrow page does not
	// split on a hair-thin gap.
	minColGapAbs = 18.0
	// paraGapFrac: a vertical gap between consecutive lines larger than this
	// fraction of the line size starts a new block (paragraph break). ~0.6em of
	// leading is normal within a paragraph; a larger gap is a block boundary.
	paraGapFrac = 1.6
)

// orderBlocks recovers reading order over the given lines via XY-cut, then groups
// each leaf region into classified blocks. bodySize is the document body font size
// (from bodyFontSize) used for heading classification. The result is in reading
// order: for a two-column page the entire left column precedes the right.
func orderBlocks(lines []line, width, height, bodySize float64) []block {
	if len(lines) == 0 {
		return nil
	}
	var out []block
	xyCut(lines, width, height, true, func(region []line) {
		out = append(out, groupRegion(region, bodySize)...)
	})
	return out
}

// xyCut recursively splits region at the widest whitespace valley, calling emit on
// each leaf region in reading order. vertical selects the cut axis to try first
// (true = split columns, then recurse with horizontal); alternating axes is the
// classic XY-cut. A region with no qualifying valley on either axis is a leaf.
func xyCut(region []line, width, height float64, vertical bool, emit func([]line)) {
	if len(region) <= 1 {
		emit(region)
		return
	}
	// Try a vertical cut (column split) first when requested, else a horizontal cut.
	if vertical {
		if left, right, ok := cutVertical(region, width); ok {
			// Left column fully before right (reading order for LTR columns).
			xyCut(left, width, height, false, emit)
			xyCut(right, width, height, false, emit)
			return
		}
		// No column gap: fall through to a horizontal split of this region.
		if top, bot, ok := cutHorizontal(region); ok {
			xyCut(top, width, height, true, emit)
			xyCut(bot, width, height, true, emit)
			return
		}
	} else {
		if top, bot, ok := cutHorizontal(region); ok {
			xyCut(top, width, height, true, emit)
			xyCut(bot, width, height, true, emit)
			return
		}
		if left, right, ok := cutVertical(region, width); ok {
			xyCut(left, width, height, false, emit)
			xyCut(right, width, height, false, emit)
			return
		}
	}
	emit(region) // leaf: no qualifying valley on either axis
}

// cutVertical finds the widest vertical whitespace valley across the region's
// horizontal extent and, if it is wide enough to be a column gutter, partitions the
// lines into a left group (entirely left of the valley) and a right group. It
// returns ok=false when no gutter qualifies. The valley is found by scanning the
// sorted list of per-line x-intervals for the largest x-gap not straddled by any
// line.
func cutVertical(region []line, width float64) (left, right []line, ok bool) {
	// Build the union of occupied x-intervals, then find the widest gap between
	// consecutive intervals. Only a gap wider than the column threshold splits.
	type iv struct{ lo, hi float64 }
	ivs := make([]iv, 0, len(region))
	for _, l := range region {
		if l.x1 > l.x0 {
			ivs = append(ivs, iv{l.x0, l.x1})
		}
	}
	if len(ivs) < 2 {
		return nil, nil, false
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i].lo < ivs[j].lo })
	// Sweep to find the widest gap between the running max-right and the next left.
	bestGap, bestAt := 0.0, 0.0
	runMax := ivs[0].hi
	for _, v := range ivs[1:] {
		if gap := v.lo - runMax; gap > bestGap {
			bestGap, bestAt = gap, runMax+gap/2
		}
		if v.hi > runMax {
			runMax = v.hi
		}
	}
	threshold := maxf(minColGapAbs, minColGapFrac*width)
	if bestGap < threshold {
		return nil, nil, false
	}
	for _, l := range region {
		if l.x1 <= bestAt {
			left = append(left, l)
		} else {
			right = append(right, l)
		}
	}
	// Guard against a degenerate split that puts everything on one side.
	if len(left) == 0 || len(right) == 0 {
		return nil, nil, false
	}
	return left, right, true
}

// cutHorizontal splits the region at the largest vertical gap between consecutive
// (baseline-sorted) lines, when that gap is clearly larger than the typical line
// spacing (so we only cut real block boundaries, not every line). It returns
// ok=false when no gap stands out. The typical spacing is the median of adjacent
// gaps; a cut requires a gap at least paraGapFrac× the line size AND well above the
// median, so a uniformly-spaced paragraph is never split mid-way.
func cutHorizontal(region []line) (top, bot []line, ok bool) {
	if len(region) < 2 {
		return nil, nil, false
	}
	ls := make([]line, len(region))
	copy(ls, region)
	sort.Slice(ls, func(i, j int) bool { return ls[i].y < ls[j].y })

	gaps := make([]float64, len(ls)-1)
	for i := 0; i+1 < len(ls); i++ {
		gaps[i] = ls[i+1].y - ls[i].y
	}
	medGap := median(gaps)
	bestGap, bestIdx := 0.0, -1
	for i, g := range gaps {
		if g > bestGap {
			bestGap, bestIdx = g, i
		}
	}
	if bestIdx < 0 {
		return nil, nil, false
	}
	size := ls[bestIdx].size
	if size <= 0 {
		size = medGap
	}
	// Require the widest gap to be both a real paragraph gap (>paraGapFrac of the
	// line size) and materially larger than the median line spacing, so we do not
	// bisect an evenly-set paragraph.
	if bestGap < paraGapFrac*size || bestGap < 1.5*medGap+0.01 {
		return nil, nil, false
	}
	return ls[:bestIdx+1], ls[bestIdx+1:], true
}

// groupRegion turns a leaf region's lines (reading order along y) into classified
// blocks: it walks the baseline-sorted lines, breaking into a new block whenever
// the inter-line gap or the left-indent changes markedly, then classifies each run.
func groupRegion(region []line, bodySize float64) []block {
	if len(region) == 0 {
		return nil
	}
	ls := make([]line, len(region))
	copy(ls, region)
	sort.Slice(ls, func(i, j int) bool { return ls[i].y < ls[j].y })

	var blocks []block
	var cur []line
	flush := func() {
		if len(cur) == 0 {
			return
		}
		blocks = append(blocks, classify(cur, bodySize))
		cur = nil
	}
	for _, l := range ls {
		if len(cur) == 0 {
			cur = append(cur, l)
			continue
		}
		prev := cur[len(cur)-1]
		gap := l.y - prev.y
		size := maxf(prev.size, l.size)
		// A large vertical gap, a leading line that is a list marker, or a shift
		// across the heading/body size boundary starts a new block.
		breakHere := gap > paraGapFrac*size ||
			startsWithMarker(l) ||
			isHeadingSize(l.size, bodySize) != isHeadingSize(prev.size, bodySize)
		if breakHere {
			flush()
		}
		cur = append(cur, l)
	}
	flush()
	return blocks
}

// classify labels a run of lines as a heading, list item, or paragraph. A single
// line notably larger than body text is a heading (its level bucketed by size). A
// run whose first line begins with a bullet or ordered marker is a list item. The
// rest are paragraphs.
func classify(lines []line, bodySize float64) block {
	first := lines[0]
	// Heading: the block's representative size clearly exceeds the body size. Use the
	// max line size in the block so a wrapped 2-line heading still classifies.
	maxSize := 0.0
	for _, l := range lines {
		if l.size > maxSize {
			maxSize = l.size
		}
	}
	if isHeadingSize(maxSize, bodySize) {
		return block{kind: blockHeading, lines: lines, level: headingLevel(maxSize, bodySize)}
	}
	if marker, _, ok := listMarker(first); ok {
		return block{kind: blockListItem, lines: lines, marker: marker}
	}
	return block{kind: blockParagraph, lines: lines}
}

// isHeadingSize reports whether a line size is large enough (relative to the body
// size) to be a heading. A 15%+ jump over body text is the heading threshold; a
// zero/unknown body size never classifies as heading (avoids treating a whole page
// of uniform text as headings).
func isHeadingSize(size, bodySize float64) bool {
	if bodySize <= 0 || size <= 0 {
		return false
	}
	return size >= bodySize*1.15
}

// headingLevel buckets a heading's size ratio (size / bodySize) to an h1..h6 level:
// the larger the text relative to body, the smaller (more important) the level. The
// buckets are chosen so a typical document's size ladder (e.g. 2.0×, 1.5×, 1.25×
// body) maps to h1/h2/h3.
func headingLevel(size, bodySize float64) int {
	if bodySize <= 0 {
		return 2
	}
	ratio := size / bodySize
	switch {
	case ratio >= 2.0:
		return 1
	case ratio >= 1.6:
		return 2
	case ratio >= 1.4:
		return 3
	case ratio >= 1.25:
		return 4
	case ratio >= 1.15:
		return 5
	default:
		return 6
	}
}

// startsWithMarker reports whether a line begins with a list marker (used as a
// block-break signal in groupRegion).
func startsWithMarker(l line) bool {
	_, _, ok := listMarker(l)
	return ok
}

// listMarker inspects a line's leading text for a bullet or ordered-list marker and
// returns the normalized Markdown marker ("- " for bullets, "N. " for ordered),
// whether it is ordered, and ok. Recognized: the bullet glyphs • ‣ · ◦ ▪ – — - *,
// and ordered forms "N." / "N)" / "a." / "a)" / roman "iv." with digits or letters.
func listMarker(l line) (marker string, ordered bool, ok bool) {
	if len(l.words) == 0 {
		return "", false, false
	}
	first := l.words[0].text
	// Bullet: the first word is (or starts with) a bullet glyph.
	if r := firstRune(first); isBulletRune(r) {
		return "- ", false, true
	}
	// Ordered: "N." / "N)" possibly as the whole first word, e.g. "1." or "1)".
	if body, sep, isOrd := splitOrdered(first); isOrd {
		return body + string(sep) + " ", true, true
	}
	return "", false, false
}

// firstRune returns the first rune of s, or 0 for an empty string.
func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// isBulletRune reports whether r is a recognized bullet glyph.
func isBulletRune(r rune) bool {
	switch r {
	case '•', // •
		'‣', // ‣
		'·', // ·
		'◦', // ◦
		'▪', // ▪
		'●', // ●
		'–', // – en dash used as bullet
		'—', // — em dash
		'*':
		return true
	case '-':
		return true
	}
	return false
}

// splitOrdered parses an ordered-list marker token like "1.", "12)", "a.", "iv)".
// It returns the numeral/letter body, the separator rune ('.' or ')'), and whether
// the token is a well-formed ordered marker. The body must be all digits or all
// ASCII letters (covering decimal, alpha, and roman numerals, which are letters).
func splitOrdered(tok string) (body string, sep rune, ok bool) {
	if len(tok) < 2 {
		return "", 0, false
	}
	last := tok[len(tok)-1]
	if last != '.' && last != ')' {
		return "", 0, false
	}
	body = tok[:len(tok)-1]
	if body == "" {
		return "", 0, false
	}
	allDigit := true
	for _, r := range body {
		if !unicode.IsDigit(r) {
			allDigit = false
			break
		}
	}
	// A numeric marker ("1.", "12)") is unambiguous. An alphabetic marker is only a
	// single letter ("a.", "B)") or a Roman numeral ("iv.", "IX)"); any other
	// letter run is prose (e.g. "etc.", "Fig.") and is rejected. This avoids
	// mistaking an abbreviation ending in a period for a list marker.
	if allDigit {
		return body, rune(last), true
	}
	if isAlphaMarker(body) {
		return body, rune(last), true
	}
	return "", 0, false
}

// isAlphaMarker reports whether body is a valid alphabetic list marker: a single
// ASCII letter, or a well-formed Roman numeral (upper or lower case).
func isAlphaMarker(body string) bool {
	if len([]rune(body)) == 1 && isASCIILetter(rune(body[0])) {
		return true
	}
	return isRomanNumeral(body)
}

// isRomanNumeral reports whether s is a non-empty string of Roman-numeral letters
// (case-insensitive). It checks the character set only (not strict numeral form),
// which is sufficient to distinguish a numeral marker from prose while accepting
// the forms lists actually use (i, ii, iii, iv, v, ...).
func isRomanNumeral(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch r {
		case 'i', 'v', 'x', 'l', 'c', 'd', 'm',
			'I', 'V', 'X', 'L', 'C', 'D', 'M':
		default:
			return false
		}
	}
	return true
}

// isASCIILetter reports whether r is an ASCII letter.
func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// blockText returns a block's text with lines joined by spaces (paragraph reflow):
// the recovered running text of the block. For a list item the leading marker word
// is dropped (it is carried on the block's Marker instead).
func (b block) blockText() string {
	var parts []string
	for i, l := range b.lines {
		t := l.text()
		if i == 0 && b.kind == blockListItem {
			t = stripLeadingMarker(t)
		}
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// stripLeadingMarker removes a leading bullet or ordered marker token (and its
// trailing space) from a list item's first line's text.
func stripLeadingMarker(s string) string {
	fields := strings.SplitN(s, " ", 2)
	if len(fields) == 0 {
		return s
	}
	first := fields[0]
	if r := firstRune(first); isBulletRune(r) && len([]rune(first)) == 1 {
		if len(fields) == 2 {
			return strings.TrimSpace(fields[1])
		}
		return ""
	}
	if _, _, ok := splitOrdered(first); ok {
		if len(fields) == 2 {
			return strings.TrimSpace(fields[1])
		}
		return ""
	}
	// A bullet fused to the word (e.g. "•item"): strip a leading bullet rune.
	if r := firstRune(s); isBulletRune(r) {
		return strings.TrimSpace(strings.TrimPrefix(s, string(r)))
	}
	return s
}
