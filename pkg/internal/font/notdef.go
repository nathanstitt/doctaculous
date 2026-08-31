package font

import "github.com/nathanstitt/omnidoc/pkg/internal/render"

// Synthesized .notdef ("tofu") box geometry, in em units (Y up, baseline at 0).
// The proportions follow what browsers draw when a font's own .notdef is blank:
// a hollow rectangle roughly the height of a capital letter, inset from the
// glyph's advance so consecutive boxes read as separate marks rather than a
// solid bar.
const (
	notdefAdvanceEm   = 0.55 // advance the synthesized box consumes
	notdefSideBearing = 0.06 // gap between the advance edge and the box wall
	notdefBottomEm    = 0.0  // box sits on the baseline
	notdefTopEm       = 0.70 // box top, ~cap height
	notdefStrokeEm    = 0.05 // wall thickness
)

// NotdefGlyph returns the face's .notdef glyph — the mark to draw for a rune the
// face cannot map — as an outline in em units (Y up) plus its advance in em units.
//
// It follows what browsers do, in the same order:
//
//  1. If the font's own glyph 0 has geometry, use it. Most real fonts ship a
//     .notdef, and drawing the designer's own mark is the faithful result:
//     DejaVu's is a hollow box, Noto's is a box with the code point's hex digits.
//  2. Otherwise synthesize a hollow box. This is not a rare fallback here — every
//     bundled TeX Gyre substitute has a .notdef that is BLANK (glyph 0 carries a
//     non-zero advance but zero contours), so a document rendered against the
//     bundle would still show empty space if we stopped at step 1. Browsers
//     synthesize in exactly this case, which is why the tofu box is a familiar
//     sight in fonts that never drew one.
//
// The advance comes from the font when its .notdef is real, so the box occupies
// the width the designer intended; the synthesized box uses its own advance. Either
// way the advance is non-zero, so a missing rune consumes horizontal space and
// line-breaking sees the text at close to its true width instead of silently
// shrinking it.
//
// The result is always a drawable OUTLINE, never a (face, GID) pair, in both
// branches. That is deliberate: handing a text-emitting backend glyph 0 would have
// the PDF writer embed and emit the font's .notdef, which for every bundled
// substitute is BLANK — so the mark would appear in a raster and vanish in a PDF of
// the same page. Returning geometry gives every backend the same visible mark.
//
// The outline is freshly built per call, so a caller may retain or mutate it.
func (f *Face) NotdefGlyph() (outline *render.Path, advanceEm float64) {
	if o := f.prog.outline(0); o != nil && !o.Empty() {
		adv, _ := f.prog.advanceEm(0)
		if adv <= 0 {
			// A real .notdef outline with no advance would stack every missing
			// rune at one x-position. Fall back to the synthesized advance so the
			// mark still occupies space; the font's own geometry is still drawn.
			adv = notdefAdvanceEm
		}
		return o, adv
	}
	return notdefBoxPath(), notdefAdvanceEm
}

// notdefBoxPath builds the synthesized tofu box: a hollow rectangle drawn as an
// outer contour plus an inner contour wound the opposite way, so the nonzero fill
// rule used for glyph outlines cancels the interior. This mirrors ringPath in
// bullet.go; a stroked path is not an option because the glyph pipeline fills
// outlines and never strokes them.
func notdefBoxPath() *render.Path {
	x0, y0 := notdefSideBearing, notdefBottomEm
	x1, y1 := notdefAdvanceEm-notdefSideBearing, notdefTopEm
	p := &render.Path{}
	// Outer wall, counter-clockwise.
	p.MoveTo(x0, y0)
	p.LineTo(x1, y0)
	p.LineTo(x1, y1)
	p.LineTo(x0, y1)
	p.Close()
	// Inner hole, clockwise, inset by the wall thickness. Guarded because a
	// pathologically thick wall relative to the box would invert the rectangle and
	// fill the mark solid, which reads as a different glyph rather than a tofu box.
	ix0, iy0 := x0+notdefStrokeEm, y0+notdefStrokeEm
	ix1, iy1 := x1-notdefStrokeEm, y1-notdefStrokeEm
	if ix1 > ix0 && iy1 > iy0 {
		p.MoveTo(ix0, iy0)
		p.LineTo(ix0, iy1)
		p.LineTo(ix1, iy1)
		p.LineTo(ix1, iy0)
		p.Close()
	}
	return p
}
