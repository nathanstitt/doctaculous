package gen

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"fmt"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Embedded test fonts. These are real, permissively-licensed fonts used only by
// the test corpus (never shipped in any binary). Roboto (Apache-2.0) is a
// TrueType (glyf) program; Source Sans 3 (SIL OFL 1.1) is OpenType/CFF. See the
// LICENSE files alongside them under fonts/.

//go:embed fonts/Roboto-Regular.ttf
var robotoTTF []byte

//go:embed fonts/SourceSans3-Regular.otf
var sourceSansOTF []byte

// EmbeddedTrueTypePDF returns a single-page PDF using a simple /TrueType font
// with an embedded FontFile2 (Roboto) and WinAnsi encoding, drawing a short
// string. It exercises the embedded-TrueType glyph path end to end.
func EmbeddedTrueTypePDF() []byte {
	const text = "Hello"
	b := newBuilder()

	fontFile := b.addStream(fmt.Sprintf(" /Length1 %d", len(robotoTTF)), robotoTTF)
	descNum := b.addObject(fmt.Sprintf(
		"<< /Type /FontDescriptor /FontName /Roboto-Regular /Flags 32 "+
			"/FontBBox [-681 -271 1535 1061] /ItalicAngle 0 /Ascent 1061 /Descent -271 "+
			"/CapHeight 711 /StemV 80 /MissingWidth 500 /FontFile2 %d 0 R >>", fontFile))

	first, widths := simpleWidths(robotoTTF, text)
	fontNum := b.addObject(fmt.Sprintf(
		"<< /Type /Font /Subtype /TrueType /BaseFont /Roboto-Regular "+
			"/FirstChar %d /LastChar %d /Widths %s /Encoding /WinAnsiEncoding "+
			"/FontDescriptor %d 0 R >>",
		first, first+len(widths)-1, widthsArray(widths), descNum))

	content := []byte(fmt.Sprintf("BT /F1 48 Tf 72 680 Td (%s) Tj ET", text))
	return finishFontPage(b, fontNum, content)
}

// EmbeddedType0PDF returns a single-page PDF using a Type0 composite font with
// Identity-H encoding and a CIDFontType2 descendant embedding Roboto
// (FontFile2), with CIDToGIDMap /Identity. The show-string is the 2-byte GIDs of
// a few letters, so it exercises the composite glyph path without a cmap step.
func EmbeddedType0PDF() []byte {
	b := newBuilder()

	fontFile := b.addStream(fmt.Sprintf(" /Length1 %d", len(robotoTTF)), robotoTTF)
	descNum := b.addObject(fmt.Sprintf(
		"<< /Type /FontDescriptor /FontName /Roboto-Regular /Flags 32 "+
			"/FontBBox [-681 -271 1535 1061] /ItalicAngle 0 /Ascent 1061 /Descent -271 "+
			"/CapHeight 711 /StemV 80 /FontFile2 %d 0 R >>", fontFile))

	// CIDFontType2 descendant. DW + a simple W run covering the GIDs we draw.
	gids := runeGIDs(robotoTTF, []rune("Hello"))
	cidFontNum := b.addObject(fmt.Sprintf(
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Roboto-Regular "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> "+
			"/FontDescriptor %d 0 R /CIDToGIDMap /Identity /DW 1000 /W %s >>",
		descNum, cidWidthsArray(robotoTTF, gids)))

	fontNum := b.addObject(fmt.Sprintf(
		"<< /Type /Font /Subtype /Type0 /BaseFont /Roboto-Regular "+
			"/Encoding /Identity-H /DescendantFonts [ %d 0 R ] >>", cidFontNum))

	content := []byte(fmt.Sprintf("BT /F1 48 Tf 72 680 Td <%s> Tj ET", hexGIDs(gids)))
	return finishFontPage(b, fontNum, content)
}

// EmbeddedCFFPDF returns a single-page PDF using a simple /Type1 font with an
// embedded FontFile3 (/Subtype Type1C) — the bare CFF table extracted from
// Source Sans 3 — and WinAnsi encoding. It exercises the bare-CFF wrapper and
// the CFF charset (name→GID) resolution path.
func EmbeddedCFFPDF() []byte {
	const text = "Hello"
	cff := extractCFFTable(sourceSansOTF)
	b := newBuilder()

	fontFile := b.addStream(fmt.Sprintf(" /Subtype /Type1C /Length1 %d", len(cff)), cff)
	descNum := b.addObject(fmt.Sprintf(
		"<< /Type /FontDescriptor /FontName /SourceSans3-Regular /Flags 32 "+
			"/FontBBox [-231 -384 1142 1193] /ItalicAngle 0 /Ascent 1193 /Descent -384 "+
			"/CapHeight 700 /StemV 80 /MissingWidth 500 /FontFile3 %d 0 R >>", fontFile))

	// Metrics come from the full OTF (sfnt parses OTTO/CFF directly); the GIDs
	// match the bare CFF table embedded above.
	first, widths := simpleWidths(sourceSansOTF, text)
	fontNum := b.addObject(fmt.Sprintf(
		"<< /Type /Font /Subtype /Type1 /BaseFont /SourceSans3-Regular "+
			"/FirstChar %d /LastChar %d /Widths %s /Encoding /WinAnsiEncoding "+
			"/FontDescriptor %d 0 R >>",
		first, first+len(widths)-1, widthsArray(widths), descNum))

	content := []byte(fmt.Sprintf("BT /F1 48 Tf 72 680 Td (%s) Tj ET", text))
	return finishFontPage(b, fontNum, content)
}

// finishFontPage assembles the page, pages, and catalog objects for a font
// fixture whose font object is fontNum, with the given content stream, and
// returns the serialized PDF.
func finishFontPage(b *builder, fontNum int, content []byte) []byte {
	contentNum := b.addStream("", content)
	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	page := b.addObject(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>",
		pagesNum, fontNum, contentNum))
	if page != pageNum {
		panic("gen: page object number mismatch in finishFontPage")
	}
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	if pages != pagesNum {
		panic("gen: pages object number mismatch in finishFontPage")
	}
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}

// --- helpers that parse the embedded fonts to build correct dictionaries ---

// mustParse parses font bytes (TrueType or wrapped CFF) for metrics extraction.
func mustParseSFNT(data []byte) *sfnt.Font {
	f, err := sfnt.Parse(data)
	if err != nil {
		panic("gen: parsing embedded font: " + err.Error())
	}
	return f
}

// runeGIDs maps runes to GIDs via a TrueType font's cmap.
func runeGIDs(ttf []byte, runes []rune) []sfnt.GlyphIndex {
	f := mustParseSFNT(ttf)
	var b sfnt.Buffer
	gids := make([]sfnt.GlyphIndex, len(runes))
	for i, r := range runes {
		g, err := f.GlyphIndex(&b, r)
		if err != nil {
			panic("gen: glyph index: " + err.Error())
		}
		gids[i] = g
	}
	return gids
}

// hexGIDs encodes GIDs as a big-endian 2-byte-per-glyph hex string for a PDF
// show-string under Identity-H.
func hexGIDs(gids []sfnt.GlyphIndex) string {
	var sb bytes.Buffer
	for _, g := range gids {
		fmt.Fprintf(&sb, "%04X", uint16(g))
	}
	return sb.String()
}

// emWidth returns a glyph's advance in 1000-unit glyph space from a font.
func emWidth(f *sfnt.Font, gid sfnt.GlyphIndex) int {
	var b sfnt.Buffer
	ppem := fixed.I(int(f.UnitsPerEm()))
	adv, err := f.GlyphAdvance(&b, gid, ppem, 0)
	if err != nil {
		return 500
	}
	return int(float64(adv) / 64.0 / float64(f.UnitsPerEm()) * 1000)
}

// simpleWidths computes the /FirstChar and /Widths (1000-unit) for the WinAnsi
// codes of text, reading advances from the given SFNT program (TrueType or a
// full OpenType/CFF container).
func simpleWidths(program []byte, text string) (first int, widths []int) {
	f := mustParseSFNT(program)
	var b sfnt.Buffer

	lo, hi := 0xFF, 0x00
	for _, r := range text {
		c := int(r) // ASCII text: code == rune for the Latin range used here
		if c < lo {
			lo = c
		}
		if c > hi {
			hi = c
		}
	}
	widths = make([]int, hi-lo+1)
	for c := lo; c <= hi; c++ {
		g, err := f.GlyphIndex(&b, rune(c))
		if err != nil || g == 0 {
			widths[c-lo] = 500
			continue
		}
		widths[c-lo] = emWidth(f, g)
	}
	return lo, widths
}

// widthsArray formats an int slice as a PDF array literal.
func widthsArray(w []int) string {
	var sb bytes.Buffer
	sb.WriteByte('[')
	for i, v := range w {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%d", v)
	}
	sb.WriteByte(']')
	return sb.String()
}

// cidWidthsArray builds a /W array giving each drawn GID its advance, in the
// "c [w]" form (one entry per CID).
func cidWidthsArray(ttf []byte, gids []sfnt.GlyphIndex) string {
	f := mustParseSFNT(ttf)
	var sb bytes.Buffer
	sb.WriteByte('[')
	for i, g := range gids {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%d [%d]", uint16(g), emWidth(f, g))
	}
	sb.WriteByte(']')
	return sb.String()
}

// extractCFFTable returns the raw bytes of the "CFF " table from an OpenType
// (OTTO) font.
func extractCFFTable(otf []byte) []byte {
	if len(otf) < 12 {
		panic("gen: OTF too short")
	}
	n := int(binary.BigEndian.Uint16(otf[4:]))
	for i := 0; i < n; i++ {
		rec := otf[12+16*i:]
		if string(rec[0:4]) == "CFF " {
			o := binary.BigEndian.Uint32(rec[8:])
			l := binary.BigEndian.Uint32(rec[12:])
			return otf[o : o+l]
		}
	}
	panic("gen: no CFF table in OTF")
}
