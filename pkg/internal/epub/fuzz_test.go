package epub

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// FuzzOpenBytes exercises the EPUB reader: the ZIP container, container.xml,
// the OPF package document, the manifest/spine graph, and chapter extraction.
//
// The property under test is that OpenBytes returns. A malformed book may fail
// with any error; what it may not do is panic, recurse without bound, or size an
// allocation from a number it read out of the file.
//
// Resources are resolved after opening because the path join (content-dir plus
// a manifest href) is attacker-controlled, and traversal or self-reference only
// shows when it is exercised.
func FuzzOpenBytes(f *testing.F) {
	for _, s := range epubSeeds() {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		book, err := OpenBytes(data)
		if err != nil || book == nil {
			return
		}
		for range book.Chapters {
		}
		for _, ref := range book.StylesheetRefs {
			_, _ = book.Resource(ref)
		}
		if book.CoverHref != "" {
			_, _ = book.Resource(book.CoverHref)
		}
		// Paths a hostile manifest would name.
		for _, ref := range []string{"../../etc/passwd", "/abs", "", "#frag", "a?b"} {
			_, _ = book.Resource(ref)
		}
	})
}

// epubSeeds returns hand-built containers aimed at container.xml, the OPF
// graph, and the href resolution a spine drives.
func epubSeeds() [][]byte {
	book := func(parts map[string]string) []byte {
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
	container := func(opf string) string {
		return `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">` +
			`<rootfiles><rootfile full-path="` + opf + `" media-type="application/oebps-package+xml"/></rootfiles></container>`
	}
	const opfNS = `xmlns="http://www.idpf.org/2007/opf" version="3.0"`

	var out [][]byte
	// A well-formed book, for the mutator to work from.
	out = append(out, book(map[string]string{
		"META-INF/container.xml": container("c.opf"),
		"c.opf": `<?xml version="1.0"?><package ` + opfNS + `><metadata/>` +
			`<manifest><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest>` +
			`<spine><itemref idref="c1"/></spine></package>`,
		"c1.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>hi</p></body></html>`,
	}))
	// The OPF names itself as a spine document.
	out = append(out, book(map[string]string{
		"META-INF/container.xml": container("c.opf"),
		"c.opf": `<?xml version="1.0"?><package ` + opfNS + `><metadata/>` +
			`<manifest><item id="c1" href="c.opf" media-type="application/xhtml+xml"/></manifest>` +
			`<spine><itemref idref="c1"/></spine></package>`,
	}))
	// container.xml pointing at itself, and at nothing.
	out = append(out, book(map[string]string{
		"META-INF/container.xml": container("META-INF/container.xml"),
	}))
	out = append(out, book(map[string]string{
		"META-INF/container.xml": container("missing.opf"),
	}))
	// Path traversal in a manifest href.
	out = append(out, book(map[string]string{
		"META-INF/container.xml": container("OPS/c.opf"),
		"OPS/c.opf": `<?xml version="1.0"?><package ` + opfNS + `><metadata/>` +
			`<manifest><item id="c1" href="../../../etc/passwd" media-type="application/xhtml+xml"/></manifest>` +
			`<spine><itemref idref="c1"/></spine></package>`,
	}))
	// Huge spine and manifest: counts the file controls.
	out = append(out, book(map[string]string{
		"META-INF/container.xml": container("c.opf"),
		"c.opf": `<?xml version="1.0"?><package ` + opfNS + `><metadata/><manifest>` +
			`<item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest><spine>` +
			strings.Repeat(`<itemref idref="c1"/>`, 100000) + `</spine></package>`,
		"c1.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>x</p></body></html>`,
	}))
	out = append(out, book(map[string]string{
		"META-INF/container.xml": container("c.opf"),
		"c.opf": `<?xml version="1.0"?><package ` + opfNS + `><metadata/><manifest>` +
			strings.Repeat(`<item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>`, 100000) +
			`</manifest><spine/></package>`,
	}))
	// A chapter whose markup nests deeply: chapter bodies go on to the HTML
	// pipeline, which has its own bound (see pkg/html), so this checks the
	// handoff rather than the parser.
	out = append(out, book(map[string]string{
		"META-INF/container.xml": container("c.opf"),
		"c.opf": `<?xml version="1.0"?><package ` + opfNS + `><metadata/>` +
			`<manifest><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest>` +
			`<spine><itemref idref="c1"/></spine></package>`,
		"c1.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><body>` +
			strings.Repeat("<div>", 50000) + "x" + strings.Repeat("</div>", 50000) +
			`</body></html>`,
	}))
	// Encrypted books are refused; make sure the check is reached.
	out = append(out, book(map[string]string{
		"META-INF/container.xml":  container("c.opf"),
		"META-INF/encryption.xml": `<?xml version="1.0"?><encryption/>`,
	}))
	// Structural degenerates.
	out = append(out, book(map[string]string{"META-INF/container.xml": "not xml"}))
	out = append(out, book(map[string]string{}))
	out = append(out, []byte("PK\x03\x04 nope"), []byte{})
	return out
}
