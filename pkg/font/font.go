// Package font turns embedded PDF font programs into glyph outlines. It parses
// the supported font dictionaries (simple TrueType and CFF, and Type0 composite
// fonts), reads the embedded FontFile2/FontFile3 programs with
// golang.org/x/image/font/sfnt, and exposes them through the content.GlyphSource
// interface so the content interpreter can draw text without knowing any
// font-format details.
//
// Unsupported fonts (non-embedded base-14, classic Type1 /FontFile, non-Identity
// Type0 CMaps, Type3) return a typed error; callers degrade gracefully by
// drawing nothing while still advancing the text cursor.
package font

import (
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/pdf/content"
)

// New builds a content.GlyphSource from a resolved PDF font dictionary. It
// branches on /Subtype. It returns a typed error for unsupported or malformed
// fonts (see the package's sentinel errors); the rasterizer logs and falls back
// to drawing nothing.
func New(doc *pdf.Document, fontDict pdf.Dict, logf func(string, ...any)) (content.GlyphSource, error) {
	subtype, _ := doc.GetName(fontDict["Subtype"])
	switch subtype {
	case "TrueType":
		data, isBareCFF, err := embeddedSimpleProgram(doc, fontDict)
		if err != nil {
			return nil, err
		}
		return newSimpleFont(doc, fontDict, data, isBareCFF)
	case "Type1", "MMType1":
		// Only an embedded FontFile3 (CFF) is supported; a classic FontFile
		// (Type1/PFB) or a non-embedded base-14 font is out of scope.
		data, isBareCFF, err := embeddedSimpleProgram(doc, fontDict)
		if err != nil {
			return nil, err
		}
		return newSimpleFont(doc, fontDict, data, isBareCFF)
	case "Type0":
		return newType0Font(doc, fontDict)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedFontType, string(subtype))
	}
}

// embeddedSimpleProgram returns the decoded embedded program for a simple font
// and whether it is a bare CFF (FontFile3 Type1C). It reads the FontDescriptor's
// FontFile2 (TrueType) or FontFile3 (CFF/OpenType).
func embeddedSimpleProgram(doc *pdf.Document, fontDict pdf.Dict) (data []byte, isBareCFF bool, err error) {
	desc := doc.GetDict(fontDict["FontDescriptor"])
	if desc == nil {
		return nil, false, ErrNoEmbeddedProgram
	}
	if s := doc.GetStream(desc["FontFile2"]); s != nil {
		b, _, derr := doc.DecodedStream(s)
		if derr != nil {
			return nil, false, fmt.Errorf("%w: FontFile2: %v", ErrUnsupportedFontProgram, derr)
		}
		return b, false, nil
	}
	if s := doc.GetStream(desc["FontFile3"]); s != nil {
		b, _, derr := doc.DecodedStream(s)
		if derr != nil {
			return nil, false, fmt.Errorf("%w: FontFile3: %v", ErrUnsupportedFontProgram, derr)
		}
		sub, _ := doc.GetName(s.Dict["Subtype"])
		return b, sub != "OpenType", nil
	}
	// A classic Type1 /FontFile is present but unsupported; otherwise there is no
	// embedded program at all.
	if doc.GetStream(desc["FontFile"]) != nil {
		return nil, false, ErrUnsupportedFontProgram
	}
	return nil, false, ErrNoEmbeddedProgram
}
