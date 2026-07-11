// Package xlsx is a read-only SpreadsheetML (.xlsx) reader: it extracts each
// visible sheet's CACHED cell values — the last value Excel computed, stored in
// the file — as display strings, along with the presentation facts a document
// conversion needs (bold/italic, fill color, alignment, merged ranges).
// Formulas are not evaluated (the cached <v> is the value), and nothing is
// written. The package is hand-rolled on archive/zip plus streaming
// encoding/xml, mirroring pkg/docx's reader; the dependency alternatives were
// audited and rejected (see docs/superpowers/specs/2026-07-09-xlsx-input-design.md).
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// ErrNotXLSX reports that the input is not a SpreadsheetML package.
var ErrNotXLSX = errors.New("not an xlsx file")

// maxPartSize caps any single decompressed part, mirroring pkg/docx.
const maxPartSize = 256 << 20

// Workbook is a parsed .xlsx: its visible sheets in workbook order.
type Workbook struct {
	// Sheets holds every sheet in the workbook, including hidden ones (the
	// Hidden flag distinguishes them; conversion skips hidden sheets, but the
	// data is there for callers that want it).
	Sheets []Sheet
}

// Sheet is one worksheet's used range as a dense rectangular grid.
type Sheet struct {
	Name   string
	Hidden bool
	// Cells is the used range, row-major, padded rectangular. Empty trailing
	// rows/columns beyond the used range are not represented.
	Cells [][]Cell
	// Merges are the sheet's merged ranges in file order.
	Merges []Merge
}

// Cell is one cell's display value plus the presentation facts conversion uses.
type Cell struct {
	// Text is the display string: the cached value rendered through the cell's
	// number format (dates and times included).
	Text string
	// Bold and Italic come from the cell's font.
	Bold, Italic bool
	// FillRGB is the solid fill color as "RRGGBB", or "" for none.
	FillRGB string
	// Align is the explicit horizontal alignment ("left", "center", "right"),
	// or "" for the format default.
	Align string
}

// Merge is a merged range: the origin cell and its extent (in cells, >= 1).
type Merge struct {
	Row, Col         int
	RowSpan, ColSpan int
}

// Open reads and parses the workbook at path.
func Open(pathName string) (*Workbook, error) {
	data, err := os.ReadFile(pathName)
	if err != nil {
		return nil, err
	}
	return OpenBytes(data)
}

// OpenBytes parses a workbook from an in-memory byte slice.
func OpenBytes(data []byte) (*Workbook, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotXLSX, err)
	}
	pkg := &pkgReader{parts: map[string]*zip.File{}}
	for _, f := range zr.File {
		pkg.parts[cleanPart(f.Name)] = f
	}
	if _, ok := pkg.parts["[Content_Types].xml"]; !ok {
		return nil, fmt.Errorf("%w: missing [Content_Types].xml", ErrNotXLSX)
	}
	return parseWorkbook(pkg)
}

// pkgReader resolves and reads OPC parts.
type pkgReader struct {
	parts map[string]*zip.File
}

// read returns a part's bytes, or nil if absent or oversized.
func (p *pkgReader) read(name string) []byte {
	f, ok := p.parts[cleanPart(name)]
	if !ok {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close() //nolint:errcheck // read-only part
	data, err := io.ReadAll(io.LimitReader(rc, maxPartSize+1))
	if err != nil || len(data) > maxPartSize {
		return nil
	}
	return data
}

// cleanPart normalizes an OPC part name (forward slashes, no leading slash).
func cleanPart(name string) string {
	return strings.TrimPrefix(path.Clean(strings.ReplaceAll(name, `\`, "/")), "/")
}

// relationship is one .rels entry.
type relationship struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
}

// relsOf unmarshals a part's .rels file (dir/_rels/base.rels), keyed by id.
func (p *pkgReader) relsOf(partName string) map[string]relationship {
	dir, base := path.Split(cleanPart(partName))
	data := p.read(dir + "_rels/" + base + ".rels")
	if data == nil {
		return nil
	}
	var doc struct {
		Rels []relationship `xml:"Relationship"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	out := make(map[string]relationship, len(doc.Rels))
	for _, r := range doc.Rels {
		r.Target = joinPart(dir, r.Target)
		out[r.ID] = r
	}
	return out
}

// firstRelOfType returns the first relationship of the given type from a
// parsed rels map... order in a map is unstable, so scan the raw list instead.
func (p *pkgReader) firstRelOfType(partName, relType string) (relationship, bool) {
	dir, base := path.Split(cleanPart(partName))
	data := p.read(dir + "_rels/" + base + ".rels")
	if data == nil {
		return relationship{}, false
	}
	var doc struct {
		Rels []relationship `xml:"Relationship"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return relationship{}, false
	}
	for _, r := range doc.Rels {
		if r.Type == relType {
			r.Target = joinPart(dir, r.Target)
			return r, true
		}
	}
	return relationship{}, false
}

// joinPart resolves a rels target relative to the source part's directory.
func joinPart(dir, target string) string {
	target = strings.ReplaceAll(target, `\`, "/")
	if strings.HasPrefix(target, "/") {
		return cleanPart(target)
	}
	return cleanPart(path.Join(dir, target))
}
