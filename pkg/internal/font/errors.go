package font

import "errors"

// Sentinel errors describing why an embedded font could not be turned into a
// usable GlyphSource. Callers (the rasterizer backend) branch on these to log
// and degrade gracefully: a font that cannot be loaded simply draws nothing
// while still advancing the text cursor.
var (
	// ErrUnsupportedFontType is returned for a font /Subtype this package does
	// not handle (e.g. /Type3).
	ErrUnsupportedFontType = errors.New("font: unsupported font subtype")

	// ErrUnsupportedFontProgram is returned when the embedded program is in a
	// form this package cannot parse, such as a classic Type1 /FontFile (PFB) or
	// a bare CFF that could not be wrapped.
	ErrUnsupportedFontProgram = errors.New("font: unsupported embedded font program")

	// ErrUnsupportedCMap is returned for a Type0 /Encoding other than the
	// Identity CMaps (Identity-H/Identity-V).
	ErrUnsupportedCMap = errors.New("font: unsupported Type0 CMap (only Identity-H/V)")

	// ErrNoEmbeddedProgram is returned when a font descriptor carries no embedded
	// program (no FontFile2/FontFile3). Non-embedded (e.g. base-14) fonts are out
	// of scope.
	ErrNoEmbeddedProgram = errors.New("font: no embedded font program (FontFile2/FontFile3)")

	// ErrInvalidWOFF is returned when a WOFF/WOFF2 container is malformed (bad
	// signature, truncated, bad compression, or an unreconstructable table
	// transform). Callers fall back to a bundled substitute.
	ErrInvalidWOFF = errors.New("font: invalid WOFF container")
)
