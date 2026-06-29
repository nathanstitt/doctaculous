package inline

// Line is a sequence of shaped glyphs that fit within an available width, plus
// the metrics needed to place its baseline and advance the pen.
type Line struct {
	Glyphs              []Glyph
	WidthPt             float64 // visible width, excludes trailing spaces
	AscentPt, DescentPt float64 // maxima over the line's glyphs
	LineGapPt           float64
}

// Align is a line's horizontal alignment within its available width — the core's
// neutral alignment vocabulary. Each engine translates its own model into it.
type Align int

const (
	// AlignLeft is ragged right; the line starts at the line origin.
	AlignLeft Align = iota
	// AlignCenter centers the line in the available width.
	AlignCenter
	// AlignRight aligns the line to the right edge of the available width.
	AlignRight
	// AlignJustify stretches inter-word space so every line but the last fills the
	// available width.
	AlignJustify
)

// Placement is the horizontal placement alignment dictates: the x of the first
// glyph's pen origin and the extra advance after each non-trailing space. An
// emitter walks the line's glyphs from StartX, adding each Advance and, after a
// Space glyph, ExtraPerSpace.
type Placement struct{ StartX, ExtraPerSpace float64 }

// Place computes the Placement for a line of visible width widthPt within
// availWidth, starting at originX, under align. last suppresses justification on
// a paragraph's final line. spaceCount is the inter-word gap count (call
// CountSpaces) used only for AlignJustify.
func Place(align Align, originX, availWidth, widthPt float64, spaceCount int, last bool) Placement {
	p := Placement{StartX: originX}
	switch align {
	case AlignRight:
		p.StartX = originX + (availWidth - widthPt)
	case AlignCenter:
		p.StartX = originX + (availWidth-widthPt)/2
	case AlignJustify:
		if !last && spaceCount > 0 {
			p.ExtraPerSpace = (availWidth - widthPt) / float64(spaceCount)
			if p.ExtraPerSpace < 0 {
				p.ExtraPerSpace = 0
			}
		}
	}
	return p
}

// MakeLine builds a Line from glyphs, computing visible width (trailing spaces
// excluded) and max ascent/descent/line-gap.
func MakeLine(glyphs []Glyph) Line {
	l := Line{Glyphs: glyphs, WidthPt: VisibleWidth(glyphs)}
	// TODO: atomic glyphs carry AscentPt/DescentPt == 0, so AtomicItem.HeightPt/
	// BaselinePt don't yet contribute here. When the CSS inline formatting context
	// emits atomics, fold an atomic's height/baseline into the line-box metrics.
	for _, g := range glyphs {
		if g.AscentPt > l.AscentPt {
			l.AscentPt = g.AscentPt
		}
		if g.DescentPt > l.DescentPt {
			l.DescentPt = g.DescentPt
		}
		if g.LineGapPt > l.LineGapPt {
			l.LineGapPt = g.LineGapPt
		}
	}
	return l
}

// VisibleWidth sums advances excluding any trailing run of spaces (which don't
// count toward whether a line fits or how wide its ink is).
func VisibleWidth(glyphs []Glyph) float64 {
	end := len(glyphs)
	for end > 0 && glyphs[end-1].Space {
		end--
	}
	w := 0.0
	for i := 0; i < end; i++ {
		w += glyphs[i].Advance
	}
	return w
}

// CountSpaces counts space glyphs excluding any trailing run of spaces (the number
// of inter-word gaps eligible for justification).
func CountSpaces(glyphs []Glyph) int {
	end := len(glyphs)
	for end > 0 && glyphs[end-1].Space {
		end--
	}
	n := 0
	for i := 0; i < end; i++ {
		if glyphs[i].Space {
			n++
		}
	}
	return n
}
