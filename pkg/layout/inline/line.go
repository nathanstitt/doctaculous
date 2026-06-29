package inline

// Line is a sequence of shaped glyphs that fit within an available width, plus
// the metrics needed to place its baseline and advance the pen.
//
// AscentPt/DescentPt are the TEXT glyph maxima (used for "normal" line-height,
// which applies a leading multiplier to the font metrics). AtomAscentPt/
// AtomDescentPt are the maxima contributed by atomic inline boxes by where they
// rest on the baseline (an atom reaches AtomicItem.BaselinePt above the baseline
// and HeightPt-BaselinePt below it). They are kept separate so the baseline can be
// placed below a tall atom (max of text and atom ascent) WITHOUT the line-height
// leading multiplier being applied to the atom's box height — an atom contributes
// its height directly, not scaled. A line with no atoms has zero atom maxima.
type Line struct {
	Glyphs                      []Glyph
	WidthPt                     float64 // visible width, excludes trailing spaces
	AscentPt, DescentPt         float64 // text glyph maxima
	AtomAscentPt, AtomDescentPt float64 // atomic-box maxima about the baseline
	LineGapPt                   float64
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
// excluded), the text glyph max ascent/descent/line-gap, and — separately — the
// max ascent/descent contributed by atomic inline boxes (Atomic != nil). An atom
// sits with its baseline AtomicItem.BaselinePt below its (margin-box) top, so it
// reaches BaselinePt above the baseline and HeightPt-BaselinePt below it; recording
// those in AtomAscentPt/AtomDescentPt lets the caller drop the baseline below a tall
// atom (e.g. an image taller than the text) without scaling the atom's height by the
// line-height leading multiplier (which applies to the font metrics only).
func MakeLine(glyphs []Glyph) Line {
	l := Line{Glyphs: glyphs, WidthPt: VisibleWidth(glyphs)}
	for i := range glyphs {
		g := &glyphs[i]
		if g.Atomic != nil {
			if asc := g.Atomic.BaselinePt; asc > l.AtomAscentPt {
				l.AtomAscentPt = asc
			}
			if desc := g.Atomic.HeightPt - g.Atomic.BaselinePt; desc > l.AtomDescentPt {
				l.AtomDescentPt = desc
			}
			continue
		}
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
