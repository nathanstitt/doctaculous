package font

import (
	"bytes"
	"fmt"

	"github.com/benoitkugler/textlayout/fonts"
	"github.com/benoitkugler/textlayout/fonts/truetype"
	"github.com/benoitkugler/textlayout/fonts/type1"
	type1c "github.com/benoitkugler/textlayout/fonts/type1C"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// glyphProgram is the small slice of a parsed font program the renderer needs:
// glyph outlines, advances, and the two lookups (rune→GID and name→GID) used to
// resolve PDF codes to glyphs. github.com/benoitkugler/textlayout exposes three
// concrete font types — TrueType/OpenType, classic Type1, and bare CFF — that we
// adapt to this one interface so program.outline and the simple/Type0 callers
// stay format-agnostic.
type glyphProgram interface {
	// upem is the font's units-per-em (>0); outlines are divided by it to reach
	// em space.
	upem() float64
	// segments returns glyph gid's outline as benoitkugler segments (font units,
	// Y up), or nil for a missing/empty glyph.
	segments(gid fonts.GID) []fonts.Segment
	// glyphName returns gid's PostScript glyph name, or "" if unnamed.
	glyphName(gid fonts.GID) string
	// advance returns gid's horizontal advance in font units.
	advance(gid fonts.GID) float64
	// nominalGID maps a rune to a GID via the font's own cmap/encoding, ok=false
	// if absent.
	nominalGID(r rune) (fonts.GID, bool)
	// numGlyphs is the glyph count, used to bound GID lookups.
	numGlyphs() int
	// hExtents returns the font's horizontal ascender, descender, and line gap in
	// font units (ascender positive, descender negative, Y-up convention), ok=false
	// if the program exposes no such metrics (e.g. bare CFF).
	hExtents() (ascender, descender, lineGap float64, ok bool)
}

// program wraps a parsed font program. It is read-only after construction and
// safe to share across goroutines: unlike the previous sfnt-based implementation
// it needs no per-call scratch buffer.
type program struct {
	gp  glyphProgram
	upm float64 // units per em; always > 0
}

// parseProgram parses an embedded font program. kind selects the format:
//
//	progTrueType — FontFile2 (raw SFNT/TrueType) or FontFile3 /Subtype OpenType
//	progCFF      — FontFile3 /Subtype Type1C or CIDFontType0C (bare CFF table)
//	progType1    — classic FontFile (eexec-encrypted Type1, PFB or raw)
//
// A parse failure returns ErrUnsupportedFontProgram so callers degrade
// gracefully.
func parseProgram(data []byte, kind progKind) (*program, error) {
	gp, err := newGlyphProgram(data, kind)
	if err != nil {
		return nil, err
	}
	upm := gp.upem()
	if upm <= 0 {
		upm = 1000 // defensive: a sane default rather than dividing by zero
	}
	return &program{gp: gp, upm: upm}, nil
}

// numGlyphs reports the number of glyphs in the program.
func (p *program) numGlyphs() int { return p.gp.numGlyphs() }

// outline returns glyph gid's outline as a *render.Path in em units with the Y
// axis pointing up, the orientation the content interpreter expects. It returns
// nil for a missing, empty, or non-vector glyph; the interpreter then advances
// the cursor but draws nothing.
//
// benoitkugler already emits coordinates Y-up in font units, so unlike the prior
// sfnt path no Y negation is needed — only division by units-per-em.
func (p *program) outline(gid fonts.GID) *render.Path {
	segs := p.gp.segments(gid)
	if len(segs) == 0 {
		return nil
	}

	out := &render.Path{}
	started := false
	for _, s := range segs {
		switch s.Op {
		case fonts.SegmentOpMoveTo:
			if started {
				out.Close() // close the previous subpath before starting a new one
			}
			pt := p.toEm(s.Args[0])
			out.MoveTo(pt.X, pt.Y)
			started = true
		case fonts.SegmentOpLineTo:
			pt := p.toEm(s.Args[0])
			out.LineTo(pt.X, pt.Y)
		case fonts.SegmentOpCubeTo:
			c1 := p.toEm(s.Args[0])
			c2 := p.toEm(s.Args[1])
			end := p.toEm(s.Args[2])
			out.CubeTo(c1.X, c1.Y, c2.X, c2.Y, end.X, end.Y)
		case fonts.SegmentOpQuadTo:
			// Elevate the quadratic (control q=Args[0], endpoint end=Args[1]) to a
			// cubic. render.Path has no quad segment, so we compute the equivalent
			// cubic control points from the subpath's current point.
			cur := currentPoint(out)
			q := p.toEm(s.Args[0])
			end := p.toEm(s.Args[1])
			c1 := render.Point{X: cur.X + (2.0/3.0)*(q.X-cur.X), Y: cur.Y + (2.0/3.0)*(q.Y-cur.Y)}
			c2 := render.Point{X: end.X + (2.0/3.0)*(q.X-end.X), Y: end.Y + (2.0/3.0)*(q.Y-end.Y)}
			out.CubeTo(c1.X, c1.Y, c2.X, c2.Y, end.X, end.Y)
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

// toEm converts a font-unit point (Y up) into an em-space point (Y up).
func (p *program) toEm(pt fonts.SegmentPoint) render.Point {
	return render.Point{
		X: float64(pt.X) / p.upm,
		Y: float64(pt.Y) / p.upm,
	}
}

// advanceEm returns glyph gid's advance width in em units. It is only a fallback
// for fonts whose PDF dictionary omits widths; when the PDF supplies widths those
// are authoritative and this is not consulted.
func (p *program) advanceEm(gid fonts.GID) (float64, bool) {
	return p.gp.advance(gid) / p.upm, true
}

// metrics returns the font's vertical line metrics in em units (Y up): a positive
// ascent, a positive descent (the magnitude below the baseline), and the suggested
// line gap. When the program exposes no extents (bare CFF) it falls back to the
// common 0.8/0.2/0 approximation so line height is still sane.
func (p *program) metrics() (ascent, descent, lineGap float64) {
	asc, desc, gap, ok := p.gp.hExtents()
	if !ok {
		return 0.8, 0.2, 0
	}
	// Extents are font units with descender negative (Y up); normalize to em and to
	// a positive descent magnitude.
	ascent = asc / p.upm
	descent = -desc / p.upm
	lineGap = gap / p.upm
	if ascent <= 0 && descent <= 0 {
		return 0.8, 0.2, 0
	}
	return ascent, descent, lineGap
}

// nameToGID builds a glyph-name→GID map by walking every glyph's name. Simple
// fonts resolve a PDF code → glyph name (via /Encoding) → GID through this map.
// It is built once per font and cached on the program by the caller.
func (p *program) nameToGID() map[string]fonts.GID {
	n := p.numGlyphs()
	m := make(map[string]fonts.GID, n)
	for gid := 0; gid < n; gid++ {
		if name := p.gp.glyphName(fonts.GID(gid)); name != "" {
			// Keep the first GID for a given name (lowest index wins).
			if _, ok := m[name]; !ok {
				m[name] = fonts.GID(gid)
			}
		}
	}
	return m
}

// gidForRune maps a rune to a GID via the font program's own cmap/encoding.
func (p *program) gidForRune(r rune) (fonts.GID, bool) { return p.gp.nominalGID(r) }

// currentPoint returns the endpoint of the last emitted segment, used to elevate
// quadratics. It assumes a subpath is in progress (a MoveTo has been emitted).
func currentPoint(p *render.Path) render.Point {
	if len(p.Segments) == 0 {
		return render.Point{}
	}
	s := p.Segments[len(p.Segments)-1]
	switch s.Kind {
	case render.MoveTo, render.LineTo:
		return s.P0
	case render.CubeTo:
		return s.P2
	default:
		return render.Point{}
	}
}

// progKind selects the embedded font program format for parseProgram.
type progKind int

const (
	progTrueType progKind = iota // SFNT/TrueType or OpenType (FontFile2/FontFile3 OpenType)
	progCFF                      // bare CFF table (FontFile3 Type1C / CIDFontType0C)
	progType1                    // classic Type1 (FontFile, eexec)
)

// newGlyphProgram parses data with the parser appropriate to kind and adapts it
// to glyphProgram.
func newGlyphProgram(data []byte, kind progKind) (glyphProgram, error) {
	switch kind {
	case progTrueType:
		f, err := parseTrueTypeOrCollection(data)
		if err != nil {
			return nil, wrapProgErr(err)
		}
		return ttProgram{f}, nil
	case progCFF:
		f, err := type1c.Parse(bytes.NewReader(data))
		if err != nil {
			return nil, wrapProgErr(err)
		}
		return cffProgram{f}, nil
	case progType1:
		f, err := type1.Parse(bytes.NewReader(data))
		if err != nil {
			return nil, wrapProgErr(err)
		}
		return &t1Program{f: f, n: -1}, nil
	default:
		return nil, ErrUnsupportedFontProgram
	}
}

// isTrueTypeCollection reports whether data begins with the "ttcf" TrueType Collection
// signature.
func isTrueTypeCollection(data []byte) bool {
	return len(data) >= 4 && data[0] == 't' && data[1] == 't' && data[2] == 'c' && data[3] == 'f'
}

// parseTrueTypeOrCollection parses a TrueType/OpenType program, transparently handling a
// TrueType Collection (.ttc/.otc) by returning its first face. truetype.Parse cannot read
// a collection wrapper, so a collection is loaded via truetype.Load (which the upstream
// package documents as collection-aware) and the first *truetype.Font is used.
func parseTrueTypeOrCollection(data []byte) (*truetype.Font, error) {
	if !isTrueTypeCollection(data) {
		return truetype.Parse(bytes.NewReader(data))
	}
	faces, err := truetype.Load(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if len(faces) == 0 {
		return nil, fmt.Errorf("truetype collection contains no faces")
	}
	f, ok := faces[0].(*truetype.Font)
	if !ok {
		return nil, fmt.Errorf("truetype collection face is %T, not *truetype.Font", faces[0])
	}
	return f, nil
}

// outlineSegments extracts segments from a fonts.GlyphData (the common shape
// returned by faces' GlyphData), or nil if it is not a vector outline.
func outlineSegments(data fonts.GlyphData) []fonts.Segment {
	if o, ok := data.(fonts.GlyphOutline); ok {
		return o.Segments
	}
	return nil
}

// ttProgram adapts *truetype.Font (TrueType/OpenType, incl. embedded CFF tables).
type ttProgram struct{ f *truetype.Font }

func (a ttProgram) upem() float64 { return float64(a.f.Upem()) }
func (a ttProgram) segments(gid fonts.GID) []fonts.Segment {
	return outlineSegments(a.f.GlyphData(gid, 0, 0))
}
func (a ttProgram) glyphName(gid fonts.GID) string      { return a.f.GlyphName(gid) }
func (a ttProgram) advance(gid fonts.GID) float64       { return float64(a.f.HorizontalAdvance(gid)) }
func (a ttProgram) nominalGID(r rune) (fonts.GID, bool) { return a.f.NominalGlyph(r) }
func (a ttProgram) numGlyphs() int                      { return a.f.NumGlyphs }
func (a ttProgram) hExtents() (asc, desc, gap float64, ok bool) {
	return fontHExtents(a.f.FontHExtents())
}

// t1Program adapts a classic *type1.Font. type1.Font implements fonts.Face but
// exposes no glyph count, so numGlyphs is derived by probing GlyphName and cached.
type t1Program struct {
	f *type1.Font
	n int // cached glyph count; -1 until computed
}

func (a *t1Program) upem() float64 { return float64(a.f.Upem()) }
func (a *t1Program) segments(gid fonts.GID) []fonts.Segment {
	return outlineSegments(a.f.GlyphData(gid, 0, 0))
}
func (a *t1Program) glyphName(gid fonts.GID) string      { return a.f.GlyphName(gid) }
func (a *t1Program) advance(gid fonts.GID) float64       { return float64(a.f.HorizontalAdvance(gid)) }
func (a *t1Program) nominalGID(r rune) (fonts.GID, bool) { return a.f.NominalGlyph(r) }
func (a *t1Program) hExtents() (asc, desc, gap float64, ok bool) {
	return fontHExtents(a.f.FontHExtents())
}

// numGlyphs probes GlyphName upward until it stops returning names. Type1 fonts
// have at most 256 encoded glyphs but may carry more in their charstrings; we cap
// the probe generously. The result is cached.
func (a *t1Program) numGlyphs() int {
	if a.n >= 0 {
		return a.n
	}
	const cap = 1 << 16
	n := 0
	for gid := 0; gid < cap; gid++ {
		if a.f.GlyphName(fonts.GID(gid)) == "" {
			break
		}
		n = gid + 1
	}
	a.n = n
	return n
}

// cffProgram adapts a bare-CFF *type1C.Font, whose API differs from fonts.Face:
// it exposes LoadGlyph (returning segments directly) and a synthesized Cmap
// rather than GlyphData/NominalGlyph, and has no advance/upem accessors. Glyph
// access for CFF PDF fonts is by name or GID, so the missing pieces fall back to
// sane defaults (upem 1000; advance from PDF /Widths, which is authoritative).
type cffProgram struct{ f *type1c.Font }

func (a cffProgram) upem() float64 { return 1000 } // CFF charstrings are in 1000-unit space
func (a cffProgram) segments(gid fonts.GID) []fonts.Segment {
	segs, _, err := a.f.LoadGlyph(gid)
	if err != nil {
		return nil
	}
	return segs
}
func (a cffProgram) glyphName(gid fonts.GID) string { return a.f.GlyphName(gid) }
func (a cffProgram) advance(fonts.GID) float64      { return 0 } // PDF /Widths supplies advances
func (a cffProgram) nominalGID(r rune) (fonts.GID, bool) {
	// type1C synthesizes a cmap from the charset; it is always non-nil.
	cmap, _ := a.f.Cmap()
	return cmap.Lookup(r)
}
func (a cffProgram) numGlyphs() int { return a.f.NumGlyphs() }

// hExtents reports no metrics for bare CFF (type1C exposes none); callers fall
// back to upem-derived defaults.
func (a cffProgram) hExtents() (asc, desc, gap float64, ok bool) { return 0, 0, 0, false }

// fontHExtents adapts a benoitkugler fonts.FontExtents result to this package's
// (ascender, descender, lineGap, ok) tuple in font units.
func fontHExtents(ext fonts.FontExtents, ok bool) (asc, desc, gap float64, _ bool) {
	if !ok {
		return 0, 0, 0, false
	}
	return float64(ext.Ascender), float64(ext.Descender), float64(ext.LineGap), true
}

// wrapProgErr tags a parser error as an unsupported font program.
func wrapProgErr(err error) error {
	return fmt.Errorf("%w: %v", ErrUnsupportedFontProgram, err)
}
