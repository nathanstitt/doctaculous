package content

import "github.com/nathanstitt/doctaculous/pkg/render"

// loadedFont is a font resolved for the current page, wrapping a GlyphSource the
// backend provides. The interpreter uses it to decode text bytes into glyphs,
// advance the text cursor, and obtain outlines to draw.
type loadedFont struct {
	src GlyphSource
}

// GlyphSource abstracts a usable font. Implementations (provided by the
// rasterizer or other backends) handle font-program parsing and encoding so the
// content interpreter stays free of font-format details.
type GlyphSource interface {
	// DecodeString splits a PDF show-string into glyphs in display order. Simple
	// fonts yield one glyph per byte; composite (Type0) fonts may consume two
	// bytes per glyph.
	DecodeString(s []byte) []Glyph
}

// Glyph is one positioned glyph produced from a show-string.
type Glyph struct {
	// Code is the character/CID code (for debugging and width lookup).
	Code int
	// Width is the glyph advance in text space (1/1000 em already divided to em
	// units, i.e. a 500-unit glyph is 0.5).
	Width float64
	// Rune is the best-effort Unicode mapping, if known (0 if unknown).
	Rune rune
	// IsSpace marks the single-byte space (code 32) for word-spacing application.
	IsSpace bool
	// Outline is the glyph outline in em units (1 em = 1.0), y up. It may be nil
	// if the source has no outline (e.g. a missing glyph), in which case the
	// interpreter advances the cursor but draws nothing.
	Outline *render.Path
}
