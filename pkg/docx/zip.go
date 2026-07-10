package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

// maxPartSize caps the decompressed size of any single package part. It bounds
// memory use against a zip bomb (a tiny DEFLATE entry that expands without limit)
// while comfortably exceeding any legitimate document.xml/styles.xml.
const maxPartSize = 256 << 20 // 256 MiB

// pkgReader is an opened OOXML package: the ZIP plus a relationship-aware lookup
// of its parts. It exists only during Open; the parsed Document retains no
// reference to the zip bytes.
type pkgReader struct {
	zip   *zip.Reader
	files map[string]*zip.File // part name (no leading slash) -> file
}

// openPackage validates and indexes an OOXML ZIP. It returns ErrNotDocx when the
// bytes are not a ZIP or lack the [Content_Types].xml every OPC package requires.
func openPackage(r io.ReaderAt, size int64) (*pkgReader, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotDocx, err)
	}
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[cleanPart(f.Name)] = f
	}
	if _, ok := files["[Content_Types].xml"]; !ok {
		return nil, fmt.Errorf("%w: no [Content_Types].xml", ErrNotDocx)
	}
	return &pkgReader{zip: zr, files: files}, nil
}

// cleanPart normalizes a ZIP entry name to a package part name: forward slashes,
// no leading slash. ZIP names already use "/", so this mainly strips any leading
// slash a producer might add.
func cleanPart(name string) string {
	return strings.TrimPrefix(name, "/")
}

// part returns the raw bytes of a part by name, or ok=false if absent.
func (p *pkgReader) part(name string) ([]byte, bool) {
	f, ok := p.files[cleanPart(name)]
	if !ok {
		return nil, false
	}
	rc, err := f.Open()
	if err != nil {
		return nil, false
	}
	defer func() { _ = rc.Close() }() // read-only; a close error cannot affect the bytes already read
	// Cap the read at maxPartSize+1 so a zip bomb cannot exhaust memory; a part that
	// hits the cap is treated as absent rather than trusted.
	b, err := io.ReadAll(io.LimitReader(rc, maxPartSize+1))
	if err != nil || len(b) > maxPartSize {
		return nil, false
	}
	return b, true
}

// mediaParts returns every word/media/* part keyed by its part name.
func (p *pkgReader) mediaParts() map[string][]byte {
	return p.partsWithPrefix("word/media/")
}

// partsWithPrefix returns every part under a name prefix keyed by part name,
// or nil when none exist.
func (p *pkgReader) partsWithPrefix(prefix string) map[string][]byte {
	var out map[string][]byte
	for name := range p.files {
		if strings.HasPrefix(name, prefix) {
			if data, ok := p.part(name); ok {
				if out == nil {
					out = map[string][]byte{}
				}
				out[name] = data
			}
		}
	}
	return out
}

// mainDocumentPart locates word/document.xml by following the package
// relationships from /_rels/.rels (the officeDocument relationship), falling back
// to the conventional path. It returns ErrMissingPart if neither resolves to an
// existing part.
func (p *pkgReader) mainDocumentPart() (string, error) {
	const officeDocType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	if data, ok := p.part("_rels/.rels"); ok {
		var doc struct {
			Rels []struct {
				Type   string `xml:"Type,attr"`
				Target string `xml:"Target,attr"`
			} `xml:"Relationship"`
		}
		if err := xml.Unmarshal(data, &doc); err == nil {
			for _, r := range doc.Rels {
				if r.Type == officeDocType {
					name := cleanPart(strings.TrimPrefix(r.Target, "/"))
					if _, ok := p.files[name]; ok {
						return name, nil
					}
				}
			}
		}
	}
	// Fallback to the conventional location.
	if _, ok := p.files["word/document.xml"]; ok {
		return "word/document.xml", nil
	}
	return "", fmt.Errorf("%w: word/document.xml", ErrMissingPart)
}

// Open reads and parses a .docx file from a path.
func Open(filePath string) (*Document, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("docx: open %s: %w", filePath, err)
	}
	return OpenBytes(data)
}

// OpenBytes parses a .docx from an in-memory byte slice. The slice is read but
// not retained.
func OpenBytes(data []byte) (*Document, error) {
	return OpenReaderAt(bytes.NewReader(data), int64(len(data)))
}

// OpenReaderAt parses a .docx from a random-access reader of the given size,
// matching the io.ReaderAt+size convention the PDF parser uses.
func OpenReaderAt(r io.ReaderAt, size int64) (*Document, error) {
	pkg, err := openPackage(r, size)
	if err != nil {
		return nil, err
	}
	return parsePackage(pkg)
}
