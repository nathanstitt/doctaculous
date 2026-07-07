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
	documentXML  string
	stylesXML    string
	numberingXML string
	media        map[string][]byte // part name -> bytes (e.g. "word/media/image1.png")
	extraRels    []rel             // additional document relationships
}

// rel is one extra relationship to emit into document.xml.rels.
type rel struct {
	id, typ, target, mode string
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

// SetNumbering sets the word/numbering.xml part. When empty, no numbering part is
// written.
func (b *Builder) SetNumbering(xml string) *Builder {
	b.numberingXML = xml
	return b
}

// AddMedia registers an image part (e.g. "media/image1.png", stored under
// word/) with the given bytes and its file extension's content-type default.
func (b *Builder) AddMedia(partName string, data []byte) *Builder {
	if b.media == nil {
		b.media = map[string][]byte{}
	}
	b.media["word/"+partName] = data
	return b
}

// AddRel adds a document relationship (id -> target). mode is "" for an internal
// part or "External" for a URL.
func (b *Builder) AddRel(id, typ, target, mode string) *Builder {
	b.extraRels = append(b.extraRels, rel{id: id, typ: typ, target: target, mode: mode})
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

	write("[Content_Types].xml", contentTypes(b.stylesXML != "", b.numberingXML != "", len(b.media) > 0))
	write("_rels/.rels", rootRels)
	write("word/_rels/document.xml.rels", docRels(b.stylesXML != "", b.numberingXML != "", b.extraRels))
	write("word/document.xml", b.documentXML)
	if b.stylesXML != "" {
		write("word/styles.xml", b.stylesXML)
	}
	if b.numberingXML != "" {
		write("word/numbering.xml", b.numberingXML)
	}
	for name, data := range b.media {
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: fixedModTime})
		_, _ = w.Write(data)
	}
	_ = zw.Close()
	return buf.Bytes()
}

func contentTypes(withStyles, withNumbering, withMedia bool) string {
	s := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`
	if withStyles {
		s += `
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>`
	}
	if withNumbering {
		s += `
  <Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>`
	}
	if withMedia {
		s += `
  <Default Extension="png" ContentType="image/png"/>
  <Default Extension="jpeg" ContentType="image/jpeg"/>
  <Default Extension="jpg" ContentType="image/jpeg"/>`
	}
	return s + "\n</Types>"
}

const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

func docRels(withStyles, withNumbering bool, extra []rel) string {
	s := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	if withStyles {
		s += `
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`
	}
	if withNumbering {
		s += `
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>`
	}
	for _, r := range extra {
		mode := ""
		if r.mode != "" {
			mode = ` TargetMode="` + r.mode + `"`
		}
		s += `
  <Relationship Id="` + r.id + `" Type="` + r.typ + `" Target="` + r.target + `"` + mode + `/>`
	}
	return s + "\n</Relationships>"
}
