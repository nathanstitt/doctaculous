// Package docx generates minimal, deterministic .docx fixtures for the DOCX
// renderer's tests. Like the PDF generator in testdata/gen it builds fixtures in
// readable Go so a failure localizes to one feature, and the bytes are
// reproducible (ZIP entries use a fixed modification time) so they can back
// byte-level assertions if needed.
package docx

import (
	"archive/zip"
	"bytes"
	"time"
)

// fixedModTime is a constant timestamp stamped on every ZIP entry so generated
// fixtures are byte-for-byte reproducible across runs and machines.
var fixedModTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// Builder assembles a minimal valid .docx (OPC package) in memory. The zero value
// is not usable; start with New. Set the document and (optionally) styles XML,
// then call Bytes.
type Builder struct {
	documentXML string
	stylesXML   string
}

// New returns a Builder seeded with an empty document body.
func New() *Builder {
	return &Builder{}
}

// SetDocument sets the word/document.xml body. The argument is the full XML
// including the <w:document> root.
func (b *Builder) SetDocument(xml string) *Builder {
	b.documentXML = xml
	return b
}

// SetStyles sets the word/styles.xml part. When empty, no styles part is written
// (the renderer then falls back to its built-in defaults).
func (b *Builder) SetStyles(xml string) *Builder {
	b.stylesXML = xml
	return b
}

// Bytes serializes the package to .docx bytes. It writes the required OPC parts:
// [Content_Types].xml, the package and document relationships, the main document,
// and the optional styles part.
func (b *Builder) Bytes() []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, content string) {
		w, _ := zw.CreateHeader(&zip.FileHeader{
			Name:     name,
			Method:   zip.Deflate,
			Modified: fixedModTime,
		})
		_, _ = w.Write([]byte(content))
	}

	write("[Content_Types].xml", contentTypes(b.stylesXML != ""))
	write("_rels/.rels", rootRels)
	write("word/_rels/document.xml.rels", docRels(b.stylesXML != ""))
	write("word/document.xml", b.documentXML)
	if b.stylesXML != "" {
		write("word/styles.xml", b.stylesXML)
	}
	_ = zw.Close()
	return buf.Bytes()
}

func contentTypes(withStyles bool) string {
	s := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`
	if withStyles {
		s += `
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>`
	}
	return s + "\n</Types>"
}

const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

func docRels(withStyles bool) string {
	s := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	if withStyles {
		s += `
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`
	}
	return s + "\n</Relationships>"
}
