package layout

import "github.com/nathanstitt/doctaculous/pkg/render"

// shapedGlyph is one positioned-pending glyph: its outline and advance plus the
// styling and metrics needed to emit and measure it. Whitespace glyphs carry an
// advance and no outline and mark a break opportunity.
type shapedGlyph struct {
	outline   *render.Path
	advance   float64 // points
	color     glyphColor
	sizePt    float64
	ascentPt  float64 // face ascent at sizePt, for baseline placement
	descentPt float64
	lineGapPt float64
	isSpace   bool // a break opportunity (and excluded from a line's trailing width)
	hardBreak bool // a hard line break: no glyph, forces a new line
}

// glyphColor is a tiny RGBA holder kept separate from image/color on the hot
// path; converted to color.RGBA at emit time.
type glyphColor struct{ R, G, B, A uint8 }

// line is a sequence of shaped glyphs that fit within the content width, plus the
// metrics needed to place its baseline and advance the pen.
type line struct {
	glyphs    []shapedGlyph
	widthPt   float64 // visible width excluding trailing spaces
	ascentPt  float64 // max ascent over the line's glyphs
	descentPt float64 // max descent
	lineGapPt float64 // max line gap
}

// breakLines greedily splits a paragraph's shaped glyphs into lines that fit the
// content width, breaking at spaces (first-fit). A hardBreak glyph forces a
// break. firstLineWidthPt is the available width for the first line only (so a
// first-line indent that narrows it is honored); subsequent lines use
// maxWidthPt. A single word wider than the available width is placed alone and
// allowed to overflow rather than looping forever.
func breakLines(glyphs []shapedGlyph, maxWidthPt, firstLineWidthPt float64) []line {
	var lines []line
	avail := firstLineWidthPt

	var cur []shapedGlyph // glyphs accumulated for the current line

	// emit finalizes the current line and resets for the next.
	emit := func() {
		lines = append(lines, makeLine(cur))
		cur = nil
		avail = maxWidthPt
	}

	for _, g := range glyphs {
		if g.hardBreak {
			emit()
			continue
		}
		cur = append(cur, g)
		if visibleWidth(cur) <= avail {
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
		lines = append(lines, makeLine(append([]shapedGlyph(nil), keep...)))
		cur = append([]shapedGlyph(nil), rest...)
		avail = maxWidthPt
	}
	if len(cur) > 0 {
		lines = append(lines, makeLine(cur))
	}
	if len(lines) == 0 {
		lines = append(lines, line{})
	}
	return lines
}

// lastSpaceBefore returns the index of the last space glyph at or before idx, or
// -1 if none.
func lastSpaceBefore(glyphs []shapedGlyph, idx int) int {
	for i := idx; i >= 0; i-- {
		if glyphs[i].isSpace {
			return i
		}
	}
	return -1
}

// makeLine builds a line from glyphs, computing its visible width (trailing
// spaces excluded) and max metrics.
func makeLine(glyphs []shapedGlyph) line {
	l := line{glyphs: glyphs, widthPt: visibleWidth(glyphs)}
	for _, g := range glyphs {
		if g.ascentPt > l.ascentPt {
			l.ascentPt = g.ascentPt
		}
		if g.descentPt > l.descentPt {
			l.descentPt = g.descentPt
		}
		if g.lineGapPt > l.lineGapPt {
			l.lineGapPt = g.lineGapPt
		}
	}
	return l
}

// visibleWidth sums advances excluding any trailing spaces (which don't count
// toward whether a line fits or how wide its ink is).
func visibleWidth(glyphs []shapedGlyph) float64 {
	end := len(glyphs)
	for end > 0 && glyphs[end-1].isSpace {
		end--
	}
	w := 0.0
	for i := 0; i < end; i++ {
		w += glyphs[i].advance
	}
	return w
}
