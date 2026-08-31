// Package pptx is a read-only PresentationML (.pptx) reader: it extracts each
// visible slide's shapes — text frames with their paragraph/run formatting and
// bullet levels, pictures, and tables — with positions resolved through the
// slide → layout → master placeholder inheritance, so a conversion frontend
// can lay each slide out as one fixed-size page. Animations, transitions,
// SmartArt, charts, and theme-driven styling are out of scope and degrade
// gracefully (the content that IS modeled still extracts). The package is
// hand-rolled on archive/zip plus streaming encoding/xml, mirroring pkg/docx
// and pkg/xlsx.
package pptx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
)

// ErrNotPPTX reports that the input is not a PresentationML package.
var ErrNotPPTX = errors.New("not a pptx file")

// maxPartSize caps any single decompressed part, mirroring pkg/docx.
const maxPartSize = 256 << 20

// emuPerPt converts EMU to points (914400 EMU/inch, 72 pt/inch).
const emuPerPt = 12700

// Presentation is a parsed .pptx: slide size plus the visible slides in
// presentation order.
type Presentation struct {
	// SlideWPt/SlideHPt are the slide dimensions in points.
	SlideWPt, SlideHPt float64
	// Slides holds the visible slides (hidden slides are skipped).
	Slides []Slide
}

// Slide is one slide's shapes in z-order (document order — later paints on
// top; the conversion frontend re-sorts for reading order).
type Slide struct {
	Shapes []Shape
}

// ShapeKind classifies a shape.
type ShapeKind int

const (
	// ShapeText is a text frame (a p:sp with a txBody).
	ShapeText ShapeKind = iota
	// ShapePicture is an embedded image (p:pic).
	ShapePicture
	// ShapeTable is a table (p:graphicFrame with an a:tbl).
	ShapeTable
)

// Shape is one positioned slide shape.
type Shape struct {
	Kind ShapeKind
	// XPt/YPt/WPt/HPt are the shape's frame in points, resolved through
	// placeholder inheritance (slide → layout → master) when the shape itself
	// declares no transform. A zero W/H means the source declared none.
	XPt, YPt, WPt, HPt float64
	// IsTitle marks a title/ctrTitle placeholder (the frontend renders it as
	// a heading and orders it first).
	IsTitle bool
	// Paragraphs is the text content (ShapeText).
	Paragraphs []Paragraph
	// Image is the embedded picture (ShapePicture).
	Image ImageRef
	// Table is the cell grid (ShapeTable).
	Table [][]TableCell
}

// Paragraph is one a:p with its level, bullet, and runs.
type Paragraph struct {
	// Level is the indent level (0-based).
	Level int
	// Bullet reports the paragraph's bullet: "" none, "char" a glyph bullet,
	// "auto" an auto-numbered bullet.
	Bullet string
	// Align is the declared alignment ("", "center", "right", "justify").
	Align string
	Runs  []Run
}

// Run is one a:r text run with its direct formatting.
type Run struct {
	Text         string
	Bold, Italic bool
	// SizePt is the font size in points (0 = unset).
	SizePt float64
	// ColorRGB is the solid fill color as "RRGGBB", or "".
	ColorRGB string
}

// ImageRef is an embedded picture's bytes and name.
type ImageRef struct {
	// Data is the media part's raw bytes.
	Data []byte
	// Name is the media part name (e.g. "ppt/media/image1.png").
	Name string
}

// TableCell is one a:tc.
type TableCell struct {
	Paragraphs []Paragraph
	// GridSpan/RowSpan are the cell's spans (>= 1).
	GridSpan, RowSpan int
	// Merged marks a continuation cell covered by another cell's span.
	Merged bool
}

// Open reads and parses the presentation at path.
func Open(pathName string) (*Presentation, error) {
	data, err := os.ReadFile(pathName)
	if err != nil {
		return nil, err
	}
	return OpenBytes(data)
}

// OpenBytes parses a presentation from an in-memory byte slice.
func OpenBytes(data []byte) (*Presentation, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotPPTX, err)
	}
	pkg := &pkgReader{parts: map[string]*zip.File{}}
	for _, f := range zr.File {
		pkg.parts[cleanPart(f.Name)] = f
	}
	if _, ok := pkg.parts["[Content_Types].xml"]; !ok {
		return nil, fmt.Errorf("%w: missing [Content_Types].xml", ErrNotPPTX)
	}
	return parsePresentation(pkg)
}
