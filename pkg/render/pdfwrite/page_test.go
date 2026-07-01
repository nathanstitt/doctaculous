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
