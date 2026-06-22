package font

import (
	"fmt"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// program wraps a parsed sfnt font program together with its units-per-em, which
// is the constant needed to normalize glyph coordinates into em space. A parsed
// program is read-only and safe to share, but each goroutine drawing from it
// must use its own *sfnt.Buffer (LoadGlyph reuses the buffer's backing arrays).
type program struct {
	sf  *sfnt.Font
	upm float64 // units per em; always > 0

	// nameToGID maps glyph names to GIDs for bare-CFF (Type1C/CIDFontType0C)
	// programs, whose charset sfnt does not expose. It is nil for TrueType and
	// OpenType programs, where rune→GID via the cmap is used instead.
	nameToGID map[string]sfnt.GlyphIndex
}

// parseProgram parses an embedded font program. data is the decoded bytes of a
// FontFile2 (raw SFNT/TrueType) or FontFile3 stream. When isBareCFF is set the
// bytes are a bare CFF table (FontFile3 /Subtype Type1C or CIDFontType0C), which
// sfnt cannot parse directly, so they are first wrapped in a minimal OpenType
// container (see wrapBareCFF).
func parseProgram(data []byte, isBareCFF bool) (*program, error) {
	var nameToGID map[string]sfnt.GlyphIndex
	if isBareCFF {
		// Build the name→GID map from the CFF charset before wrapping, since the
		// wrapper's synthesized cmap maps nothing; simple Type1C fonts resolve
		// code→glyphname→GID through this map. A failure here is non-fatal:
		// GID-indexed (Type0) access still works without it.
		if m, err := cffNameToGID(data); err == nil {
			nameToGID = make(map[string]sfnt.GlyphIndex, len(m))
			for name, gid := range m {
				nameToGID[name] = sfnt.GlyphIndex(gid)
			}
		}
		wrapped, err := wrapBareCFF(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnsupportedFontProgram, err)
		}
		data = wrapped
	}
	sf, err := sfnt.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedFontProgram, err)
	}
	upm := float64(sf.UnitsPerEm())
	if upm <= 0 {
		upm = 1000 // defensive: a sane default rather than dividing by zero
	}
	return &program{sf: sf, upm: upm, nameToGID: nameToGID}, nil
}

// numGlyphs reports the number of glyphs in the program.
func (p *program) numGlyphs() int { return p.sf.NumGlyphs() }

// outline loads glyph gid and returns its outline as a *render.Path in em units
// with the Y axis pointing up, the orientation the content interpreter expects.
// It returns nil for a missing, empty, or non-vector glyph; the interpreter then
// advances the cursor but draws nothing.
func (p *program) outline(b *sfnt.Buffer, gid sfnt.GlyphIndex) *render.Path {
	// ppem == UnitsPerEm makes LoadGlyph return coordinates in font units (as
	// 26.6 fixed point). sfnt has already negated Y so its output is Y-down; we
	// negate again below to restore the Y-up em-space outline contract.
	ppem := fixed.I(int(p.sf.UnitsPerEm()))
	segs, err := p.sf.LoadGlyph(b, gid, ppem, nil)
	if err != nil || len(segs) == 0 {
		return nil
	}

	out := &render.Path{}
	var cur render.Point // running current point (start of the next segment)
	started := false
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			if started {
				out.Close() // close the previous subpath before starting a new one
			}
			pt := p.toEm(s.Args[0])
			out.MoveTo(pt.X, pt.Y)
			cur = pt
			started = true
		case sfnt.SegmentOpLineTo:
			pt := p.toEm(s.Args[0])
			out.LineTo(pt.X, pt.Y)
			cur = pt
		case sfnt.SegmentOpCubeTo:
			c1 := p.toEm(s.Args[0])
			c2 := p.toEm(s.Args[1])
			end := p.toEm(s.Args[2])
			out.CubeTo(c1.X, c1.Y, c2.X, c2.Y, end.X, end.Y)
			cur = end
		case sfnt.SegmentOpQuadTo:
			// Elevate the quadratic (control q, endpoint end) from the current
			// point to an equivalent cubic: C1 = P0 + 2/3(q-P0), C2 = end + 2/3(q-end).
			q := p.toEm(s.Args[0])
			end := p.toEm(s.Args[1])
			c1 := render.Point{X: cur.X + (2.0/3.0)*(q.X-cur.X), Y: cur.Y + (2.0/3.0)*(q.Y-cur.Y)}
			c2 := render.Point{X: end.X + (2.0/3.0)*(q.X-end.X), Y: end.Y + (2.0/3.0)*(q.Y-end.Y)}
			out.CubeTo(c1.X, c1.Y, c2.X, c2.Y, end.X, end.Y)
			cur = end
		}
	}
	if started {
		out.Close()
	}
	if out.Empty() {
		return nil
	}
	return out
}

// toEm converts a 26.6 fixed-point font-unit point from sfnt into an em-space
// point with the Y axis pointing up.
func (p *program) toEm(pt fixed.Point26_6) render.Point {
	return render.Point{
		X: (float64(pt.X) / 64.0) / p.upm,
		Y: -(float64(pt.Y) / 64.0) / p.upm,
	}
}

// advanceEm returns glyph gid's advance width in em units. It is only a fallback
// for fonts whose PDF dictionary omits widths; when the PDF supplies widths those
// are authoritative and this is not consulted.
func (p *program) advanceEm(b *sfnt.Buffer, gid sfnt.GlyphIndex) (float64, bool) {
	ppem := fixed.I(int(p.sf.UnitsPerEm()))
	adv, err := p.sf.GlyphAdvance(b, gid, ppem, font.HintingNone)
	if err != nil {
		return 0, false
	}
	return (float64(adv) / 64.0) / p.upm, true
}
