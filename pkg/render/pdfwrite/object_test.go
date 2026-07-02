package pdfwrite

import (
	"bytes"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// TestSerializeMinimalPDFParses builds a tiny PDF (catalog + 1 page, no content)
// and re-parses it with the project's own parser as the oracle.
func TestSerializeMinimalPDFParses(t *testing.T) {
	w := newWriter()

	pages := w.alloc()
	page := w.alloc()
	catalog := w.alloc()

	w.put(catalog, Dict{"Type": Name("Catalog"), "Pages": pages})
	w.put(pages, Dict{
		"Type":  Name("Pages"),
		"Kids":  Array{page},
		"Count": Int(1),
	})
	w.put(page, Dict{
		"Type":     Name("Page"),
		"Parent":   pages,
		"MediaBox": Array{Int(0), Int(0), Int(612), Int(792)},
	})
	w.setRoot(catalog)

	var buf bytes.Buffer
	if err := w.serialize(&buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}

	doc, err := pdf.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("pkg/pdf failed to parse our output: %v", err)
	}
	if got := doc.PageCount(); got != 1 {
		t.Fatalf("page count = %d; want 1", got)
	}
}

// TestSerializeStreamFlateRoundTrips asserts addStream flate-encodes its content
// and declares the filter.
func TestSerializeStreamFlateRoundTrips(t *testing.T) {
	w := newWriter()
	content := []byte("BT /F1 12 Tf (hi) Tj ET")
	sid := w.addStream(Dict{}, content)
	if sid == 0 {
		t.Fatal("addStream returned zero id")
	}
	// Keep the stream reachable so serialize doesn't error on an unfilled object.
	page := w.alloc()
	pages := w.alloc()
	catalog := w.alloc()
	w.put(page, Dict{
		"Type":     Name("Page"),
		"Parent":   pages,
		"MediaBox": Array{Int(0), Int(0), Int(612), Int(792)},
		"Contents": sid,
	})
	w.put(pages, Dict{"Type": Name("Pages"), "Kids": Array{page}, "Count": Int(1)})
	w.put(catalog, Dict{"Type": Name("Catalog"), "Pages": pages})
	w.setRoot(catalog)

	var buf bytes.Buffer
	if err := w.serialize(&buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("/Filter")) {
		t.Fatal("stream not marked with a /Filter")
	}
	if bytes.Contains(buf.Bytes(), content) {
		t.Fatal("stream content stored uncompressed (raw bytes present)")
	}
	// And it must still parse.
	if _, err := pdf.Parse(buf.Bytes()); err != nil {
		t.Fatalf("pkg/pdf failed to parse output with stream: %v", err)
	}
}
