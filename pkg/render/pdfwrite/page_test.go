package pdfwrite

import (
	"bytes"
	"context"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// tallTextPage builds a layout.Page heightPt tall with a glyph every 20pt so
// fragmentation has line boxes to break between. All glyphs use the SAME face, so
// the cross-page font de-dup is exercised (one subset serves every page).
func tallTextPage(t testing.TB, heightPt float64) layout.Page {
	face, ok := font.LoadStandard("Helvetica", font.Style{})
	if !ok {
		if t != nil {
			t.Fatal("no Helvetica")
		}
		return layout.Page{}
	}
	gid, _ := face.GID('A')
	outline := face.Outline(gid)
	var items []layout.Item
	for y := 20.0; y < heightPt; y += 20 {
		items = append(items, layout.Item{
			Kind: layout.GlyphKind,
			Glyph: layout.GlyphItem{
				Outline: outline, XPt: 20, YPt: y, SizePt: 12,
				Color: color.RGBA{A: 255}, Face: face, GID: gid, Runes: []rune{'A'},
			},
		})
	}
	return layout.Page{WidthPt: 300, HeightPt: heightPt, Items: items}
}

func TestWriteDocumentPaginatesAndParses(t *testing.T) {
	pages := &layout.Pages{Pages: []layout.Page{tallTextPage(t, 600)}}
	opts := Options{PageWidthPt: 300, PageHeightPt: 200, MarginPt: 0}

	var buf bytes.Buffer
	if err := WriteDocument(context.Background(), &buf, pages, opts); err != nil {
		t.Fatalf("WriteDocument: %v", err)
	}
	doc, err := pdf.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if got := doc.PageCount(); got != 3 {
		t.Fatalf("page count = %d; want 3", got)
	}
}

// TestWriteDocumentDeterministic asserts the parallel render + sequential merge
// produces byte-identical output across runs.
func TestWriteDocumentDeterministic(t *testing.T) {
	pages := &layout.Pages{Pages: []layout.Page{tallTextPage(t, 600)}}
	opts := Options{PageWidthPt: 300, PageHeightPt: 200, MarginPt: 0}

	var a, b bytes.Buffer
	if err := WriteDocument(context.Background(), &a, pages, opts); err != nil {
		t.Fatal(err)
	}
	if err := WriteDocument(context.Background(), &b, pages, opts); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("WriteDocument output not deterministic across runs")
	}
}

// TestWriteDocumentEmbedsOneFontForManyPages asserts the cross-page de-dup: a single
// face used on all pages is embedded once (one /FontFile in the output).
func TestWriteDocumentEmbedsOneFontForManyPages(t *testing.T) {
	pages := &layout.Pages{Pages: []layout.Page{tallTextPage(t, 600)}}
	opts := Options{PageWidthPt: 300, PageHeightPt: 200, MarginPt: 0}
	var buf bytes.Buffer
	if err := WriteDocument(context.Background(), &buf, pages, opts); err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(buf.Bytes(), []byte("/FontFile")); n != 1 {
		t.Fatalf("/FontFile count = %d; want 1 (font de-duped across pages)", n)
	}
}

// glyphPage builds a one-line layout.Page rendering the given runes with the given
// face, at a fixed page size, so a multi-page document can vary glyph order per page
// while SHARING one *font.Face (as the real FaceCache does — the cross-page font
// de-dup keys on the face pointer).
func glyphPage(t testing.TB, face *font.Face, runes []rune, wPt, hPt float64) layout.Page {
	var items []layout.Item
	x := 20.0
	for _, r := range runes {
		gid, ok := face.GID(r)
		if !ok {
			continue
		}
		items = append(items, layout.Item{
			Kind: layout.GlyphKind,
			Glyph: layout.GlyphItem{
				Outline: face.Outline(gid), XPt: x, YPt: 40, SizePt: 20,
				Color: color.RGBA{A: 255}, Face: face, GID: gid, Runes: []rune{r},
			},
		})
		x += 15
	}
	return layout.Page{WidthPt: wPt, HeightPt: hPt, Items: items}
}

// TestMultiPageType1EncodingConsistent is the regression for the multi-page Type1
// scramble: a single Type1 face used on two pages with DIFFERENT first-use glyph
// orders must still map every emitted code to the right glyph. Before the shared-
// embedder pre-pass, each page numbered glyphs independently while the font's
// /Differences used a global numbering, so page 2's text came out garbled. Here the
// two pages use "AB" and "BA"; every page's Tj codes must resolve (via /ToUnicode)
// back to the runes that page drew.
func TestMultiPageType1EncodingConsistent(t *testing.T) {
	// Helvetica -> a Type1 (simple-font) face, the 1-byte-code path that broke. One
	// shared face across both pages, mirroring the real FaceCache.
	face := mustFace(t, "Helvetica")
	if _, kind := face.ProgramBytes(); kind != font.ProgramKindType1 {
		t.Skip("bundled Helvetica is not Type1")
	}
	pages := &layout.Pages{Pages: []layout.Page{
		glyphPage(t, face, []rune{'A', 'B'}, 300, 200),
		glyphPage(t, face, []rune{'B', 'A'}, 300, 200),
	}}
	var buf bytes.Buffer
	if err := WriteDocument(context.Background(), &buf, pages, Options{}); err != nil {
		t.Fatal(err)
	}
	doc, err := pdf.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.PageCount() != 2 {
		t.Fatalf("page count = %d; want 2", doc.PageCount())
	}
	// Font is de-duped to one simple font with one /Differences and one /ToUnicode.
	if n := bytes.Count(buf.Bytes(), []byte("/FontFile")); n != 1 {
		t.Fatalf("/FontFile count = %d; want 1 (one shared Type1 font)", n)
	}
	// Extract the 1-byte Tj codes each page emits. Page 1 draws A,B and page 2 draws
	// B,A; with a shared global code table the code for 'A' must be identical on both
	// pages and likewise for 'B', so page 2's code sequence is page 1's REVERSED. The
	// pre-shared-embedder bug numbered per page, giving both pages [0,1] and thus
	// mapping page 2's first glyph ('B') to the font's glyph 0 ('A') — a scramble.
	p1 := type1TjCodes(t, doc, 0)
	p2 := type1TjCodes(t, doc, 1)
	if len(p1) != 2 || len(p2) != 2 {
		t.Fatalf("expected 2 glyph codes per page; got %v and %v", p1, p2)
	}
	if p1[0] == p1[1] {
		t.Fatalf("page 1 codes not distinct: %v", p1)
	}
	if p2[0] != p1[1] || p2[1] != p1[0] {
		t.Fatalf("codes inconsistent across pages: page1=%v page2=%v; "+
			"want page2 == reversed page1 (same code per letter)", p1, p2)
	}
}

// type1TjCodes returns the sequence of 1-byte hex codes shown by `<NN> Tj` operators
// on page index (the simple-font text path), decoding via the project's own parser.
func type1TjCodes(t testing.TB, doc *pdf.Document, index int) []int {
	t.Helper()
	pg, err := doc.Page(index)
	if err != nil {
		t.Fatalf("page %d: %v", index, err)
	}
	content, err := pg.ContentBytes()
	if err != nil {
		t.Fatalf("page %d content: %v", index, err)
	}
	return parseHexTj(content)
}

// parseHexTj scans a content stream for `<HH> Tj` (2-hex-digit, 1-byte-code) text-
// showing operators and returns the decoded byte values in order.
func parseHexTj(content []byte) []int {
	var codes []int
	for i := 0; i+1 < len(content); i++ {
		if content[i] != '<' {
			continue
		}
		j := i + 1
		for j < len(content) && content[j] != '>' {
			j++
		}
		if j >= len(content) {
			break
		}
		hex := content[i+1 : j]
		// A trailing " Tj" (allowing whitespace) marks a shown string.
		k := j + 1
		for k < len(content) && (content[k] == ' ' || content[k] == '\n' || content[k] == '\r' || content[k] == '\t') {
			k++
		}
		if k+1 < len(content) && content[k] == 'T' && content[k+1] == 'j' && len(hex) == 2 {
			v := 0
			ok := true
			for _, c := range hex {
				v <<= 4
				switch {
				case c >= '0' && c <= '9':
					v |= int(c - '0')
				case c >= 'A' && c <= 'F':
					v |= int(c-'A') + 10
				case c >= 'a' && c <= 'f':
					v |= int(c-'a') + 10
				default:
					ok = false
				}
			}
			if ok {
				codes = append(codes, v)
			}
		}
		i = j
	}
	return codes
}

func mustFace(t testing.TB, family string) *font.Face {
	f, ok := font.LoadStandard(family, font.Style{})
	if !ok {
		t.Fatalf("no %s", family)
	}
	return f
}

func BenchmarkWriteDocument(b *testing.B) {
	pages := &layout.Pages{Pages: []layout.Page{tallTextPage(b, 6000)}}
	opts := Options{PageWidthPt: 612, PageHeightPt: 792}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := WriteDocument(context.Background(), &buf, pages, opts); err != nil {
			b.Fatal(err)
		}
	}
}
