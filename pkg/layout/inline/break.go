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
