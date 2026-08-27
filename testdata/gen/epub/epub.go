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

	coverName  string
	coverStyle CoverStyle
}

type chapter struct{ name, xhtml string }

type mediaPart struct {
	name      string
	data      []byte
	mediaType string
}

// CoverStyle selects which OPF convention a fixture declares its cover with.
type CoverStyle int

const (
	// CoverNone declares no cover at all.
	CoverNone CoverStyle = iota
	// CoverEPUB3 declares the cover with the EPUB 3 manifest item property
	// properties="cover-image".
	CoverEPUB3
	// CoverEPUB2 declares the cover with the EPUB 2 de-facto metadata entry
	// <meta name="cover" content="itemID">.
	CoverEPUB2
	// CoverBoth declares it BOTH ways, as many real EPUB 3 files do for reader
	// compatibility.
	CoverBoth
)

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

// AddMedia registers a resource part under OPS/<name>, manifested as
// application/octet-stream.
func (b *Builder) AddMedia(name string, data []byte) *Builder {
	return b.AddMediaTyped(name, data, "application/octet-stream")
}

// AddMediaTyped registers a resource part under OPS/<name> with an explicit
// manifest media-type. Use it when the type is load-bearing (a cover image, say,
// whose type the renderer reads).
func (b *Builder) AddMediaTyped(name string, data []byte, mediaType string) *Builder {
	b.media = append(b.media, mediaPart{name: name, data: data, mediaType: mediaType})
	return b
}

// SetCover marks an already-added media part (by its AddMedia/AddMediaTyped
// name) as the book's cover image, declared with the given OPF convention.
// Calling it with CoverNone, or with a name no media part matches, leaves the
// book coverless.
func (b *Builder) SetCover(name string, style CoverStyle) *Builder {
	b.coverName, b.coverStyle = name, style
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
	coverID := ""
	for i, m := range b.media {
		mt := m.mediaType
		if mt == "" {
			mt = "application/octet-stream"
		}
		id := fmt.Sprintf("m%d", i+1)
		props := ""
		if m.name == b.coverName && b.coverStyle != CoverNone {
			coverID = id
			if b.coverStyle == CoverEPUB3 || b.coverStyle == CoverBoth {
				props = ` properties="cover-image"`
			}
		}
		fmt.Fprintf(&manifest, `<item id="%s" href="%s" media-type="%s"%s/>`+"\n", id, m.name, mt, props)
	}
	coverMeta := ""
	if coverID != "" && (b.coverStyle == CoverEPUB2 || b.coverStyle == CoverBoth) {
		coverMeta = fmt.Sprintf(`<meta name="cover" content="%s"/>`+"\n", coverID)
	}
	opf := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:identifier id="uid">urn:uuid:00000000-0000-0000-0000-000000000000</dc:identifier>
<dc:title>%s</dc:title>
<dc:language>en</dc:language>
%s</metadata>
<manifest>
%s</manifest>
<spine>
%s</spine>
</package>
`, b.title, coverMeta, manifest.String(), spine.String())

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
