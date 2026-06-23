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
	return &Face{prog: prog, names: prog.nameToGID()}, true
}

// Glyph resolves rune r to its outline and advance. The outline is in em units
// with the Y axis pointing up (1.0 == 1 em); it is nil for a missing, empty, or
// whitespace glyph (a space has an advance but no outline). advanceEm is the
// horizontal advance in em units. ok is false when the face has no glyph mapped
// for r, in which case the caller should skip it (advancing nothing).
func (f *Face) Glyph(r rune) (outline *render.Path, advanceEm float64, ok bool) {
	gid, ok := f.gidForRune(r)
	if !ok {
		return nil, 0, false
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

// Metrics returns the face's vertical line metrics in em units (Y up): a positive
// ascent above the baseline, a positive descent magnitude below it, and the
// font's suggested extra line gap. The reflow engine derives line height from
// these. Faces whose program exposes no extents fall back to a 0.8/0.2/0
// approximation.
func (f *Face) Metrics() (ascent, descent, lineGap float64) {
	return f.prog.metrics()
}
