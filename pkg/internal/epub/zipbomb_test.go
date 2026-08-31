package epub

import (
	"archive/zip"
	"bytes"
	"fmt"
	"testing"
)

// buildBook assembles a minimal EPUB container holding n filler entries of
// partSize bytes each, alongside the container.xml and OPF a book needs to open.
func buildBook(t *testing.T, n, partSize int) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(name string, body []byte) {
		t.Helper()
		w, err := z.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	add("META-INF/container.xml", []byte(`<?xml version="1.0"?>`+
		`<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">`+
		`<rootfiles><rootfile full-path="c.opf" media-type="application/oebps-package+xml"/>`+
		`</rootfiles></container>`))
	add("c.opf", []byte(`<?xml version="1.0"?>`+
		`<package xmlns="http://www.idpf.org/2007/opf" version="3.0">`+
		`<metadata><dc:title xmlns:dc="http://purl.org/dc/elements/1.1/">T</dc:title></metadata>`+
		`<manifest><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest>`+
		`<spine><itemref idref="c1"/></spine></package>`))
	add("c1.xhtml", []byte(`<html xmlns="http://www.w3.org/1999/xhtml"><body><p>hello</p></body></html>`))
	zeros := make([]byte, partSize)
	for i := range n {
		add(fmt.Sprintf("img%02d.bin", i), zeros)
	}
	if err := z.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// TestOpenBytesTotalBounded covers the aggregate budget. OpenBytes decompresses
// EVERY container entry into one map, so maxPartSize alone does not bound a
// book: N entries each just under the per-part cap multiply. Measured on the
// equivalent OOXML package, 20 compressible parts in a 4 MB file expanded to
// 4.2 GB with every individual part inside its 256 MiB limit.
//
// The budget is lowered for the test rather than building a half-gigabyte
// fixture; the property is that the SUM is bounded.
func TestOpenBytesTotalBounded(t *testing.T) {
	const partSize = 64 << 10
	orig := totalPartBudget
	// Room for the small structural parts and a couple of fillers, not all 20.
	totalPartBudget = 3 * partSize
	defer func() { totalPartBudget = orig }()

	data := buildBook(t, 20, partSize)
	book, err := OpenBytes(data)
	if err != nil {
		// Refusing outright is acceptable; what matters is that it is bounded
		// and returns rather than decompressing everything.
		return
	}
	total := 0
	for _, b := range book.parts {
		total += len(b)
	}
	if total > totalPartBudget {
		t.Errorf("OpenBytes retained %d bytes, past the %d-byte budget", total, totalPartBudget)
	}
}

// TestOrdinaryBookUnaffected guards against the budget being too tight: a normal
// book must open with its chapters intact.
func TestOrdinaryBookUnaffected(t *testing.T) {
	data := buildBook(t, 4, 16<<10) // four 16 KiB images
	book, err := OpenBytes(data)
	if err != nil {
		t.Fatalf("an ordinary book was refused: %v", err)
	}
	if len(book.Chapters) != 1 {
		t.Errorf("chapters = %d, want 1", len(book.Chapters))
	}
	if _, ok := book.Resource("img00.bin"); !ok {
		t.Error("a 16 KiB image was dropped from an ordinary book")
	}
}
