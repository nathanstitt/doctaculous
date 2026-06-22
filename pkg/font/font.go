// Package font turns embedded PDF font programs into glyph outlines. It parses
// the supported font dictionaries (simple TrueType, CFF, and classic Type1, plus
// Type0 composite fonts), reads the embedded FontFile/FontFile2/FontFile3
// programs with github.com/benoitkugler/textlayout, and exposes them through the
// content.GlyphSource interface so the content interpreter can draw text without
// knowing any font-format details.
//
// Unsupported fonts (non-embedded base-14, non-Identity Type0 CMaps, Type3)
// return a typed error; callers degrade gracefully by drawing nothing while still
// advancing the text cursor.
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
	case "TrueType", "Type1", "MMType1":
		prog, err := embeddedSimpleProgram(doc, fontDict)
		if err != nil {
			return nil, err
		}
		return newSimpleFont(doc, fontDict, prog)
	case "Type0":
		return newType0Font(doc, fontDict)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedFontType, string(subtype))
	}
}

// embeddedSimpleProgram parses the embedded program for a simple font from the
// FontDescriptor: FontFile2 (TrueType), FontFile3 (bare CFF/Type1C or OpenType),
// or the classic FontFile (Type1, eexec).
func embeddedSimpleProgram(doc *pdf.Document, fontDict pdf.Dict) (*program, error) {
	desc := doc.GetDict(fontDict["FontDescriptor"])
	if desc == nil {
		return nil, ErrNoEmbeddedProgram
	}
	if s := doc.GetStream(desc["FontFile2"]); s != nil {
		b, _, derr := doc.DecodedStream(s)
		if derr != nil {
			return nil, fmt.Errorf("%w: FontFile2: %v", ErrUnsupportedFontProgram, derr)
		}
		return parseProgram(b, progTrueType)
	}
	if s := doc.GetStream(desc["FontFile3"]); s != nil {
		b, _, derr := doc.DecodedStream(s)
		if derr != nil {
			return nil, fmt.Errorf("%w: FontFile3: %v", ErrUnsupportedFontProgram, derr)
		}
		sub, _ := doc.GetName(s.Dict["Subtype"])
		if sub == "OpenType" {
			return parseProgram(b, progTrueType)
		}
		return parseProgram(b, progCFF)
	}
	if s := doc.GetStream(desc["FontFile"]); s != nil {
		b, _, derr := doc.DecodedStream(s)
		if derr != nil {
			return nil, fmt.Errorf("%w: FontFile: %v", ErrUnsupportedFontProgram, derr)
		}
		return parseProgram(b, progType1)
	}
	return nil, ErrNoEmbeddedProgram
}
