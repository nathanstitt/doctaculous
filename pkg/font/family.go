package font

import (
	"github.com/benoitkugler/textlayout/fonts"

	"github.com/nathanstitt/doctaculous/pkg/font/standard"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Style selects a weight/slant variant of a font family. Bold and Italic combine
// (Bold && Italic selects a bold-italic face where available). Only regular
// weights are currently bundled, so for now Bold/Italic are recorded but resolve
// to the family's regular face — the same documented approximation as the PDF
// standard-14 substitutes (see package standard).
type Style struct {
	Bold   bool
	Italic bool
}

// ProgramKind identifies the embedded font-program format of a Face's bytes, so a
// PDF writer can pick /FontFile2 (TrueType/SFNT), /FontFile3 (bare CFF/OpenType),
// or /FontFile (classic Type1) when embedding.
type ProgramKind int

const (
	// ProgramKindUnknown means the program bytes were not retained.
	ProgramKindUnknown ProgramKind = iota
	// ProgramKindTrueType is an SFNT/TrueType (glyf) program (embed as /FontFile2).
	ProgramKindTrueType
	// ProgramKindCFF is a bare CFF or CFF-flavored OpenType program (/FontFile3).
	ProgramKindCFF
	// ProgramKindType1 is a classic Type1 program (eexec/PFB); embed as /FontFile
	// with the Length1/Length2/Length3 segment sizes.
	ProgramKindType1
)

// Face is a measured, outline-able font face independent of point size. It is the
// reflow engine's view of a font: given a rune it yields a glyph outline (em
// units, Y up) and an advance (em units), and it reports vertical line metrics for
// line-height computation.
//
// A Face is read-only after construction and safe for concurrent use, mirroring
// the underlying *program. Build one with LoadStandard (or, in future, from an
// embedded document font), and cache it — parsing the font program is the
// expensive step.
type Face struct {
	prog  *program
	names map[string]fonts.GID // glyph name → GID, built once for the name route

	progData []byte      // raw program bytes for embedding (nil if not retained)
	progKind ProgramKind // format of progData
}

// LoadStandard returns a Face for a named font family, substituting a bundled
// permissively-licensed look-alike. It resolves the standard-14 names and common
// aliases (Arial, Times New Roman, Courier New, and the Office defaults Calibri
// and Cambria) via standard.Lookup; family matching is case-insensitive and
// tolerant of spaces and a subset prefix. ok is false when no substitute is
// bundled for the family (e.g. Symbol, Wingdings), so the caller skips the run.
//
// style is recorded on the lookup but currently does not change the bundled face
// (only regular weights ship); true weighted/slanted substitutes are a follow-up.
func LoadStandard(family string, style Style) (*Face, bool) {
	sub, ok := standard.Lookup(family)
	if !ok {
		return nil, false
	}
	prog, err := parseProgram(sub.Data, substituteKind(sub.Kind))
	if err != nil {
		return nil, false
	}
	return &Face{
		prog:     prog,
		names:    prog.nameToGID(),
		progData: sub.Data,
		progKind: programKindFromStandard(sub.Kind),
	}, true
}

// programKindFromStandard maps a bundled substitute's Kind to a ProgramKind so the
// PDF writer picks the right /FontFile flavor for the embedded substitute.
func programKindFromStandard(k standard.Kind) ProgramKind {
	switch k {
	case standard.KindTrueType:
		return ProgramKindTrueType
	case standard.KindType1:
		return ProgramKindType1
	default:
		return ProgramKindUnknown
	}
}

// Glyph resolves rune r to its outline and advance. The outline is in em units
// with the Y axis pointing up (1.0 == 1 em); it is nil for a missing, empty, or
// whitespace glyph (a space has an advance but no outline). advanceEm is the
// horizontal advance in em units. ok is false when the face has no glyph mapped
// for r, in which case the caller should skip it (advancing nothing).
func (f *Face) Glyph(r rune) (outline *render.Path, advanceEm float64, ok bool) {
	gid, ok := f.gidForRune(r)
	if !ok {
		// The face has no glyph for r. If r is a list-marker bullet, synthesize its
		// geometry so the marker renders anyway (the bundled substitutes lack ▪ and
		// other bullet code points); browsers likewise paint markers as shapes, not
		// font glyphs. Any other missing rune is skipped by the caller.
		return syntheticBullet(r)
	}
	adv, _ := f.prog.advanceEm(gid)
	return f.prog.outline(gid), adv, true
}

// gidForRune resolves a rune to a glyph, preferring the standard glyph-name route
// (rune → Adobe glyph name → GID) and falling back to the program's own cmap.
// The name route is necessary for the bundled Type1 substitutes (TeX Gyre), whose
// built-in encoding maps some lowercase code points to alternate glyphs; resolving
// by name picks the expected outline, matching how the PDF simple-font path works.
func (f *Face) gidForRune(r rune) (fonts.GID, bool) {
	if name := runeToGlyphName(r); name != "" {
		if gid, ok := f.names[name]; ok && gid != 0 {
			return gid, true
		}
	}
	if gid, ok := f.prog.gidForRune(r); ok {
		return gid, true
	}
	return 0, false
}

// ProgramBytes returns the raw font-program bytes for embedding and their format.
// kind is ProgramKindUnknown (and data nil) when the Face did not retain its
// program (the PDF writer then falls back to drawing outlines).
func (f *Face) ProgramBytes() (data []byte, kind ProgramKind) {
	return f.progData, f.progKind
}

// UnitsPerEm returns the face's units-per-em (always > 0).
func (f *Face) UnitsPerEm() float64 { return f.prog.upm }

// GID resolves rune r to a glyph id, preferring the glyph-name route then the
// program cmap, matching how Glyph resolves outlines. ok is false when the face
// has no glyph for r.
func (f *Face) GID(r rune) (gid uint16, ok bool) {
	g, ok := f.gidForRune(r)
	return uint16(g), ok
}

// Outline returns glyph gid's outline in em units (Y up), or nil if empty. It
// satisfies the render.GlyphRef face contract used by DrawGlyph.
func (f *Face) Outline(gid uint16) *render.Path { return f.prog.outline(fonts.GID(gid)) }

// GlyphAdvance returns gid's horizontal advance in em units (advance/units-per-em),
// for building a PDF /W widths array.
func (f *Face) GlyphAdvance(gid uint16) float64 {
	adv, _ := f.prog.advanceEm(fonts.GID(gid))
	return adv
}

// GlyphName returns gid's PostScript glyph name, or "" if unnamed. A PDF simple
// Type1 font's /Encoding /Differences maps character codes to these names.
func (f *Face) GlyphName(gid uint16) string { return f.prog.gp.glyphName(fonts.GID(gid)) }

// Metrics returns the face's vertical line metrics in em units (Y up): a positive
// ascent above the baseline, a positive descent magnitude below it, and the
// font's suggested extra line gap. The reflow engine derives line height from
// these. Faces whose program exposes no extents fall back to a 0.8/0.2/0
// approximation.
func (f *Face) Metrics() (ascent, descent, lineGap float64) {
	return f.prog.metrics()
}
