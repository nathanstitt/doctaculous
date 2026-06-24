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

	// emit finalizes the current line and resets for the next.
	emit := func() {
		lines = append(lines, MakeLine(cur))
		cur = nil
		avail = maxWidthPt
	}

	for _, g := range glyphs {
		if g.Break {
			emit()
			continue
		}
		cur = append(cur, g)
		if VisibleWidth(cur) <= avail {
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
	if len(glyphs) == 0 {
		return nil, nil
	}
	for i := 0; i < len(glyphs); i++ {
		g := glyphs[i]
		if g.Break {
			// Forced break: line is everything before the break glyph; the break glyph
			// itself is consumed (not carried to the next line).
			return glyphs[:i], glyphs[i+1:]
		}
		cur := glyphs[:i+1]
		if VisibleWidth(cur) <= widthPt {
			continue
		}
		// cur now overflows. Break at the last space before the overflow.
		brk := lastSpaceBefore(cur, len(cur)-1)
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

// lastSpaceBefore returns the index of the last space glyph at or before idx, or
// -1 if none.
func lastSpaceBefore(glyphs []Glyph, idx int) int {
	for i := idx; i >= 0; i-- {
		if glyphs[i].Space {
			return i
		}
	}
	return -1
}
