package inline

import (
	pkgfont "github.com/nathanstitt/omnidoc/pkg/font"
	layoutfont "github.com/nathanstitt/omnidoc/pkg/layout/font"
)

// Ellipsis truncation for text-overflow and -webkit-line-clamp.
//
// Both properties need the same operation: given a line that is too wide (or a line
// that has more text after it), drop trailing glyphs until an ellipsis fits, then
// append the ellipsis. Keeping it here, next to shaping, is what lets it work in
// glyph units — the advances are already resolved, so truncation is exact rather than
// a re-measure of a substring.

// EllipsisRune is the character CSS specifies for text-overflow: ellipsis. Browsers
// use U+2026 HORIZONTAL ELLIPSIS when the font has it.
const EllipsisRune = '…'

// TruncateWithEllipsis returns line truncated to fit within widthPt with an ellipsis
// glyph appended, and reports whether it changed anything.
//
// It removes whole glyphs from the end until the ellipsis fits, so the cut never lands
// mid-glyph. Trailing whitespace is dropped before the ellipsis (browsers do not leave
// a gap between the last word and the ellipsis), and the ellipsis inherits the styling
// of the last glyph it follows so it matches the text it truncates.
//
// The degenerate case is deliberate: when even the ellipsis alone does not fit, CSS
// Overflow 3 §5 still requires it to be rendered, so the result is the lone ellipsis
// rather than an empty line. A caller that clips will then show part of it, which is
// what a browser does in an 8px-wide box.
//
// ok=false means the line already fits and was returned untouched, so the caller can
// skip the copy.
func TruncateWithEllipsis(line []Glyph, widthPt float64, ell Glyph) ([]Glyph, bool) {
	return truncate(line, widthPt, ell, false)
}

// AppendEllipsis puts an ellipsis at the end of a line that ALREADY FITS, making room
// by dropping trailing glyphs only if the ellipsis itself does not fit beside them.
//
// It is what -webkit-line-clamp needs and TruncateWithEllipsis cannot serve: the final
// line of a clamp is usually a perfectly good full-width line, and the ellipsis marks
// the text that was cut AFTER it rather than text that overflowed this line. Calling
// TruncateWithEllipsis there returns the line untouched, because by its own measure
// nothing overflows.
func AppendEllipsis(line []Glyph, widthPt float64, ell Glyph) ([]Glyph, bool) {
	return truncate(line, widthPt, ell, true)
}

// truncate is the shared implementation. force appends the ellipsis even when the
// line already fits (the clamp case); otherwise a fitting line is returned untouched.
func truncate(line []Glyph, widthPt float64, ell Glyph, force bool) ([]Glyph, bool) {
	if !force && VisibleWidth(line) <= widthPt {
		return line, false
	}
	// Trailing whitespace is dropped FIRST, before any fitting decision. Two reasons,
	// and the order matters: "foo …" reads as a gap that browsers cut, and — subtly —
	// VisibleWidth already excludes trailing spaces, so measuring a candidate cut with
	// them still attached compares two different quantities and lets the loop stop
	// while spaces remain.
	n := len(line)
	for n > 0 && line[n-1].Space {
		n--
	}
	// Walk back from there, tracking the width of what remains, until the ellipsis
	// fits beside it. Summing once and subtracting is O(n) rather than the O(n^2) a
	// re-measure per candidate cut would cost on a long line. Each removal re-strips
	// whitespace, so a cut landing mid-space does not leave one behind either.
	total := 0.0
	for i := 0; i < n; i++ {
		total += line[i].Advance
	}
	for n > 0 {
		if total+ell.Advance <= widthPt {
			break
		}
		n--
		total -= line[n].Advance
		for n > 0 && line[n-1].Space {
			n--
			total -= line[n].Advance
		}
	}
	out := make([]Glyph, 0, n+1)
	out = append(out, line[:n]...)
	if n > 0 {
		// Match the truncated text's styling rather than the run's first glyph: a
		// line ending in a differently-coloured or larger span should get an ellipsis
		// that belongs to it.
		out = append(out, styleEllipsisLike(ell, line[n-1]))
	} else {
		out = append(out, ell)
	}
	return out, true
}

// styleEllipsisLike copies the paint-relevant styling of the glyph the ellipsis
// follows onto the ellipsis, keeping its own outline, advance, and font identity.
func styleEllipsisLike(ell, prev Glyph) Glyph {
	ell.Color = prev.Color
	ell.BaselineShiftPt = prev.BaselineShiftPt
	ell.InlineBox = prev.InlineBox
	return ell
}

// ShapeEllipsis builds the single ellipsis glyph for a run's style, or ok=false when
// the run's face has no glyph for it (a font without U+2026). The caller then leaves
// the line untruncated rather than substituting a wrong character — a hard cut is
// honest, a stray box glyph is not.
func ShapeEllipsis(faces *layoutfont.FaceCache, family string, style pkgfont.Style, sizePt float64, col Color) (Glyph, bool) {
	face, ok := faces.Resolve(family, style)
	if !ok {
		return Glyph{}, false
	}
	outline, advEm, ok := face.Glyph(EllipsisRune)
	if !ok {
		return Glyph{}, false
	}
	asc, desc, gap := face.Metrics()
	// The GID rides along so a text-emitting backend (the PDF writer) can embed and
	// map the ellipsis like any other glyph, rather than dropping it from the
	// extracted text.
	gid, _ := face.GID(EllipsisRune)
	return Glyph{
		GID:       gid,
		Outline:   outline,
		Advance:   advEm * sizePt,
		SizePt:    sizePt,
		Color:     col,
		AscentPt:  asc * sizePt,
		DescentPt: desc * sizePt,
		LineGapPt: gap * sizePt,
		Face:      face,
		Runes:     []rune{EllipsisRune},
	}, true
}
