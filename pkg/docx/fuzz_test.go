package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
)

// FuzzOpenBytes exercises the whole DOCX reader: the OPC container, the
// relationship graph, document.xml, and the style cascade.
//
// The property under test is that OpenBytes returns. A malformed package may
// fail with any error; what it may not do is panic, recurse without bound, or
// size an allocation from a number it read out of the file.
//
// The document is walked after opening because the model is built lazily in
// places, and a defect that only fires when the tree is consumed would
// otherwise hide behind a successful open.
func FuzzOpenBytes(f *testing.F) {
	// testdata/gen/docx imports this package, so its fixtures cannot seed an
	// in-package fuzz target (import cycle). The hand-built packages below cover
	// the container and cascade shapes that matter; the golden tests already
	// exercise the well-formed fixtures.
	for _, s := range docxSeeds() {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := OpenBytes(context.Background(), data)
		if err != nil || doc == nil {
			return
		}
		walkBlocks(doc.Body, 0)
	})
}

// walkBlocks visits the parsed model, bounding its own depth so this helper
// cannot be the thing that overflows -- the parser's bounds are under test, not
// the walker's recursion.
func walkBlocks(blocks []Block, depth int) {
	if depth > 500 {
		return
	}
	for _, b := range blocks {
		if b.Paragraph != nil {
			for range b.Paragraph.Content {
			}
		}
		if b.Table != nil {
			for _, row := range b.Table.Rows {
				for _, cell := range row.Cells {
					walkBlocks(cell.Blocks, depth+1)
				}
			}
		}
	}
}

// docxSeeds returns hand-built packages aimed at the container and the
// relationship graph, which a mutator reaches slowly from a valid .docx because
// every part name has to stay consistent with [Content_Types].xml.
func docxSeeds() [][]byte {
	pkg := func(parts map[string]string) []byte {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		for name, body := range parts {
			w, err := z.Create(name)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte(body))
		}
		_ = z.Close()
		return buf.Bytes()
	}
	const ctypes = `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`
	const relType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"

	var out [][]byte
	// A relationship pointing at itself, and one pointing nowhere.
	out = append(out, pkg(map[string]string{
		"[Content_Types].xml": ctypes,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="` + relType + `" Target="_rels/.rels"/></Relationships>`,
	}))
	out = append(out, pkg(map[string]string{
		"[Content_Types].xml": ctypes,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="` + relType + `" Target="nope.xml"/></Relationships>`,
	}))
	// Deeply nested tables: the model walk recurses per nesting level.
	out = append(out, pkg(map[string]string{
		"[Content_Types].xml": ctypes,
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			strings.Repeat(`<w:tbl><w:tr><w:tc>`, 20000) +
			`<w:p><w:r><w:t>x</w:t></w:r></w:p>` +
			strings.Repeat(`</w:tc></w:tr></w:tbl>`, 20000) + `</w:body></w:document>`,
	}))
	// Counts read straight from the markup.
	out = append(out, pkg(map[string]string{
		"[Content_Types].xml": ctypes,
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:tbl><w:tr><w:tc><w:tcPr><w:gridSpan w:val="2000000000"/></w:tcPr><w:p/></w:tc></w:tr></w:tbl>` +
			`</w:body></w:document>`,
	}))
	out = append(out, pkg(map[string]string{
		"[Content_Types].xml": ctypes,
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:pPr><w:numPr><w:ilvl w:val="2000000000"/><w:numId w:val="1"/></w:numPr></w:pPr></w:p>` +
			`</w:body></w:document>`,
	}))
	// A style that inherits from itself: the basedOn cascade is a chain walk.
	out = append(out, pkg(map[string]string{
		"[Content_Types].xml": ctypes,
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:pPr><w:pStyle w:val="A"/></w:pPr></w:p></w:body></w:document>`,
		"word/styles.xml":     `<?xml version="1.0"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:style w:styleId="A"><w:basedOn w:val="A"/></w:style></w:styles>`,
	}))
	// Structural degenerates.
	out = append(out, pkg(map[string]string{"[Content_Types].xml": ctypes}))
	out = append(out, []byte("PK\x03\x04 not really a zip"), []byte{})
	return out
}
