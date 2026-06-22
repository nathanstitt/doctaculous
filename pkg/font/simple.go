package font

import (
	"golang.org/x/image/font/sfnt"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/pdf/content"
)

// simpleFont is a GlyphSource for a simple (single-byte) font: a /TrueType font
// with an embedded FontFile2, or a /Type1 font with an embedded FontFile3 (CFF).
// Each byte of a show-string is one glyph. Code→glyph is resolved by mapping the
// code to a Unicode rune via the font's encoding and then through the embedded
// program's cmap.
type simpleFont struct {
	prog     *program
	toGID    [256]sfnt.GlyphIndex
	toRune   [256]rune
	toName   [256]string  // glyph name per code, for CFF charset (name→GID) lookup
	width    [256]float64 // em units; only valid where hasWidth is set
	hasWidth [256]bool
	missing  float64 // /MissingWidth in em units
}

// newSimpleFont builds a simpleFont from a resolved font dictionary. data is the
// decoded embedded program; isBareCFF marks a FontFile3 Type1C program.
func newSimpleFont(doc *pdf.Document, fontDict pdf.Dict, data []byte, isBareCFF bool) (*simpleFont, error) {
	prog, err := parseProgram(data, isBareCFF)
	if err != nil {
		return nil, err
	}
	f := &simpleFont{prog: prog}
	f.buildEncoding(doc, fontDict)
	f.buildWidths(doc, fontDict)
	f.resolveGIDs()
	return f, nil
}

// buildEncoding fills toRune[code] from the font's /Encoding: a base encoding
// (named, defaulting to WinAnsi) overlaid with any /Differences.
func (f *simpleFont) buildEncoding(doc *pdf.Document, fontDict pdf.Dict) {
	base := encWinAnsi
	var differences pdf.Array

	switch enc := doc.Resolve(fontDict["Encoding"]).(type) {
	case pdf.Name:
		base = baseEncodingByName(string(enc))
	case pdf.Dict:
		if name, ok := doc.GetName(enc["BaseEncoding"]); ok {
			base = baseEncodingByName(string(name))
		}
		differences = doc.GetArray(enc["Differences"])
	}

	for c := range 256 {
		r := codeToRune(base, byte(c))
		f.toRune[c] = r
		f.toName[c] = runeToGlyphName(r) // canonical name for CFF charset lookup
	}

	// Apply /Differences: integers set the running code; names assign a glyph
	// name to the current code, then the code increments.
	code := 0
	for _, item := range differences {
		switch v := doc.Resolve(item).(type) {
		case pdf.Integer:
			code = int(v)
		case pdf.Real:
			code = int(v)
		case pdf.Name:
			if code >= 0 && code < 256 {
				f.toName[code] = string(v)
				f.toRune[code] = glyphNameToRune(string(v))
			}
			code++
		}
	}
}

// buildWidths fills width[code] from /Widths (indexed by code-/FirstChar) and
// records /MissingWidth. Widths are 1000-unit glyph space, normalized to em.
func (f *simpleFont) buildWidths(doc *pdf.Document, fontDict pdf.Dict) {
	if desc := doc.GetDict(fontDict["FontDescriptor"]); desc != nil {
		if mw, ok := pdf.Number(doc.Resolve(desc["MissingWidth"])); ok {
			f.missing = mw / 1000
		}
	}
	first, _ := doc.GetInt(fontDict["FirstChar"])
	widths := doc.GetArray(fontDict["Widths"])
	for i, wObj := range widths {
		code := first + i
		if code < 0 || code >= 256 {
			continue
		}
		if w, ok := pdf.Number(doc.Resolve(wObj)); ok {
			f.width[code] = w / 1000
			f.hasWidth[code] = true
		}
	}
}

// resolveGIDs precomputes code→GID. For a bare-CFF program (which has a
// name→GID charset but no usable cmap) it maps code→glyphname→GID; otherwise it
// maps code→rune→GID through the embedded program's cmap.
func (f *simpleFont) resolveGIDs() {
	if f.prog.nameToGID != nil {
		for c := range 256 {
			if gid, ok := f.prog.nameToGID[f.toName[c]]; ok {
				f.toGID[c] = gid
			}
		}
		return
	}
	var b sfnt.Buffer
	for c := range 256 {
		r := f.toRune[c]
		if r == 0 {
			continue
		}
		gid, err := f.prog.sf.GlyphIndex(&b, r)
		if err == nil {
			f.toGID[c] = gid
		}
	}
}

// widthOf returns the advance for code in em units: PDF /Widths if present, else
// /MissingWidth, else the embedded program's own advance.
func (f *simpleFont) widthOf(b *sfnt.Buffer, code byte) float64 {
	if f.hasWidth[code] {
		return f.width[code]
	}
	if f.missing != 0 {
		return f.missing
	}
	if w, ok := f.prog.advanceEm(b, f.toGID[code]); ok {
		return w
	}
	return 0
}

// DecodeString implements content.GlyphSource: one glyph per byte.
func (f *simpleFont) DecodeString(s []byte) []content.Glyph {
	glyphs := make([]content.Glyph, 0, len(s))
	var b sfnt.Buffer
	for _, c := range s {
		gid := f.toGID[c]
		outline := f.prog.outline(&b, gid)
		glyphs = append(glyphs, content.Glyph{
			Code:    int(c),
			Width:   f.widthOf(&b, c),
			Rune:    f.toRune[c],
			IsSpace: c == 0x20,
			Outline: outline,
		})
	}
	return glyphs
}
