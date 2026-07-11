// Package epub generates deterministic .epub fixtures for tests, mirroring
// the other format generators.
package epub

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"time"
)

// Builder assembles a minimal valid EPUB 3 container.
type Builder struct {
	title    string
	chapters []chapter
	css      string
	media    []mediaPart
}

type chapter struct{ name, xhtml string }

type mediaPart struct {
	name string
	data []byte
}

// New returns an empty book builder.
func New() *Builder { return &Builder{title: "Fixture Book"} }

// SetTitle sets the dc:title.
func (b *Builder) SetTitle(t string) *Builder { b.title = t; return b }

// SetCSS adds a stylesheet part (OPS/styles.css) chapters may link.
func (b *Builder) SetCSS(css string) *Builder { b.css = css; return b }

// AddChapter appends a spine document (full XHTML) under OPS/<name>.
func (b *Builder) AddChapter(name, xhtml string) *Builder {
	b.chapters = append(b.chapters, chapter{name: name, xhtml: xhtml})
	return b
}

// AddMedia registers a resource part under OPS/<name>.
func (b *Builder) AddMedia(name string, data []byte) *Builder {
	b.media = append(b.media, mediaPart{name: name, data: data})
	return b
}

// Bytes serializes the container deterministically. Per OCF, the mimetype
// entry comes first and is STORED (uncompressed).
func (b *Builder) Bytes() []byte {
	var manifest, spine strings.Builder
	for i, c := range b.chapters {
		fmt.Fprintf(&manifest, `<item id="c%d" href="%s" media-type="application/xhtml+xml"/>`+"\n", i+1, c.name)
		fmt.Fprintf(&spine, `<itemref idref="c%d"/>`+"\n", i+1)
	}
	if b.css != "" {
		manifest.WriteString(`<item id="css" href="styles.css" media-type="text/css"/>` + "\n")
	}
	for i, m := range b.media {
		fmt.Fprintf(&manifest, `<item id="m%d" href="%s" media-type="application/octet-stream"/>`+"\n", i+1, m.name)
	}
	opf := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:identifier id="uid">urn:uuid:00000000-0000-0000-0000-000000000000</dc:identifier>
<dc:title>%s</dc:title>
<dc:language>en</dc:language>
</metadata>
<manifest>
%s</manifest>
<spine>
%s</spine>
</package>
`, b.title, manifest.String(), spine.String())

	type part struct {
		name   string
		data   []byte
		stored bool
	}
	parts := []part{
		{"mimetype", []byte("application/epub+zip"), true},
		{"META-INF/container.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
<rootfiles><rootfile full-path="OPS/package.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>
`), false},
		{"OPS/package.opf", []byte(opf), false},
	}
	for _, c := range b.chapters {
		parts = append(parts, part{"OPS/" + c.name, []byte(c.xhtml), false})
	}
	if b.css != "" {
		parts = append(parts, part{"OPS/styles.css", []byte(b.css), false})
	}
	for _, m := range b.media {
		parts = append(parts, part{"OPS/" + m.name, m.data, false})
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	stamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range parts {
		method := zip.Deflate
		if p.stored {
			method = zip.Store
		}
		f, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: method, Modified: stamp})
		if err != nil {
			panic(err) // deterministic in-memory build
		}
		if _, err := f.Write(p.data); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
