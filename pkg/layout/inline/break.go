package inline

// Break greedily splits a paragraph's shaped glyphs into lines that fit, breaking
// at spaces (first-fit). A Break glyph forces a line break. firstWidthPt is the
// available width for the first line only (honoring a first-line indent that
// narrows it); later lines use maxWidthPt. A single word/atomic wider than the
// available width is placed alone and allowed to overflow rather than looping.
func Break(glyphs []Glyph, maxWidthPt, firstWidthPt float64) []Line {
	var lines []Line
	avail := firstWidthPt

	var cur []Glyph // glyphs accumulated for the current line
	// vw tracks VisibleWidth(cur) incrementally so filling a line of L glyphs is O(L),
	// not O(L²) (VisibleWidth re-sums every advance on each call). total is the sum of
	// ALL advances in cur; trail is the sum of cur's trailing run of spaces; the visible
	// width (trailing spaces excluded, matching VisibleWidth) is total-trail.
	var total, trail float64
	vw := func() float64 { return total - trail }
	addAdvance := func(g Glyph) {
		total += g.Advance
		if g.Space {
			trail += g.Advance
		} else {
			trail = 0
		}
	}
	// recount resets the running width for a freshly-assigned cur (used after a split,
	// where cur becomes a short carried tail). O(len(cur)); the tail is small.
	recount := func() {
		total, trail = 0, 0
		for i := range cur {
			addAdvance(cur[i])
		}
	}

	// emit finalizes the current line and resets for the next.
	emit := func() {
		lines = append(lines, MakeLine(cur))
		cur = nil
		total, trail = 0, 0
		avail = maxWidthPt
	}

	for _, g := range glyphs {
		if g.Break {
			if len(cur) == 0 {
				// An empty forced line (consecutive hard breaks): keep the break glyph
				// itself on the line. It draws nothing (no outline, zero advance) but
				// carries its run's font metrics, giving the blank line a CSS strut
				// height rather than a zero-height collapse.
				cur = append(cur, g)
			}
			emit()
			continue
		}
		cur = append(cur, g)
		addAdvance(g)
		if vw() <= avail {
			continue
		}
		// The line now overflows. Break at the last space before the overflow; if
		// there is none, the single long word stays and overflows.
		brk := lastSpaceBefore(cur, len(cur)-1)
		if brk < 0 {
			// No break opportunity yet (one long word in progress). Keep filling; it
			// will be emitted when a space finally appears or at paragraph end.
			continue
		}
		// Keep [0:brk] on this line; carry the rest (minus the breaking space).
		keep := cur[:brk]
		rest := cur[brk+1:]
		lines = append(lines, MakeLine(append([]Glyph(nil), keep...)))
		cur = append([]Glyph(nil), rest...)
		recount()
		avail = maxWidthPt
	}
	if len(cur) > 0 {
		lines = append(lines, MakeLine(cur))
	}
	if len(lines) == 0 {
		lines = append(lines, Line{})
	}
	return lines
}

// BreakNext greedily takes ONE line from the front of glyphs at the given width and
// returns it plus the unconsumed remainder. It applies the same first-fit rule as
// Break: break at the last space before the line overflows; a single word wider than
// the width is taken alone (overflow); a forced-break glyph (Break) ends the line and
// is consumed. The returned line and rest are subslices of the input. When glyphs is
// empty, line is empty and rest is nil.
//
// BreakNext is the per-line driver the CSS float formatting context uses to break a
// paragraph against a width that varies by vertical position (a float narrows some
// lines but not others). Break remains the whole-paragraph entry point for the
// fixed-width path (DOCX and non-floated HTML). Driving BreakNext repeatedly at a
// fixed width reproduces Break's lines.
func BreakNext(glyphs []Glyph, widthPt float64) (line, rest []Glyph) {
	return BreakNextWrap(glyphs, widthPt, true)
}

// BreakNextWrap is BreakNext with an explicit wrap flag. When wrap is false
// (white-space: nowrap / pre), the line is NOT broken at the available width: it
// extends to the next hard break (Break glyph) or the end of input, overflowing the
// width (the caller's overflow handling, if any, clips it). When wrap is true it
// behaves exactly like BreakNext. This is how the CSS inline formatting context honors
// a non-wrapping white-space value.
func BreakNextWrap(glyphs []Glyph, widthPt float64, wrap bool) (line, rest []Glyph) {
	if len(glyphs) == 0 {
		return nil, nil
	}
	if !wrap {
		// No width breaking: consume up to (and through) the first hard break, else all.
		for i := 0; i < len(glyphs); i++ {
			if glyphs[i].Break {
				if i == 0 {
					// Empty forced line: keep the metrics-carrying break glyph so the
					// blank line gets a strut height (see Break).
					return glyphs[0:1], glyphs[1:]
				}
				return glyphs[:i], glyphs[i+1:]
			}
		}
		return glyphs, nil
	}
	// Track VisibleWidth(glyphs[:i+1]) incrementally (O(1) per glyph) instead of
	// re-summing each iteration, which made filling one line O(L²): total is the sum of
	// all advances in glyphs[:i+1], trail the trailing run of spaces; visible = total-trail.
	var total, trail float64
	for i := 0; i < len(glyphs); i++ {
		g := glyphs[i]
		if g.Break {
			if i == 0 {
				// Empty forced line: keep the metrics-carrying break glyph so the blank
				// line gets a strut height (see Break).
				return glyphs[0:1], glyphs[1:]
			}
			// Forced break: line is everything before the break glyph; the break glyph
			// itself is consumed (not carried to the next line).
			return glyphs[:i], glyphs[i+1:]
		}
		total += g.Advance
		if g.Space {
			trail += g.Advance
		} else {
			trail = 0
		}
		if total-trail <= widthPt {
			continue
		}
		// cur now overflows. Break at the last space before the overflow.
		brk := lastSpaceBefore(glyphs[:i+1], i)
		if brk < 0 {
			// One long word in progress with no break opportunity yet: keep filling
			// until a space appears or the run ends.
			continue
		}
		// Keep [0:brk] on this line; the breaking space at brk is consumed.
		return glyphs[:brk], glyphs[brk+1:]
	}
	// The whole remaining run fits (or is one overlong word): it is the line.
	return glyphs, nil
}

// lastSpaceBefore returns the index of the last break-opportunity space at or before
// idx, or -1 if none. A NoWrap space (from a white-space: nowrap/pre run) is a space
// for width purposes but NOT a break opportunity, so it is skipped here — that keeps a
// nowrap inline span unbroken even inside a wrapping block.
func lastSpaceBefore(glyphs []Glyph, idx int) int {
	for i := idx; i >= 0; i-- {
		if glyphs[i].Space && !glyphs[i].NoWrap {
			return i
		}
	}
	return -1
}
