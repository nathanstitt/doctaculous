package content

import "github.com/nathanstitt/doctaculous/pkg/render"

// This file defines the optional capture sinks the interpreter emits to alongside
// painting, for structure-recovery backends (PDF → text / Markdown / HTML extraction).
// Both sinks are nil by default: when unset the interpreter behaves exactly as before
// (byte-identical paint), so the raster and pdfwrite backends are unaffected. They are
// deliberately paint-neutral — the interpreter still issues every Fill/Stroke/FillGlyph
// call it does today; a sink is a read-only observation of the same operations, carrying
// the semantic facts (a glyph's rune, a path's role as a candidate ruling line) that the
// paint seam discards.

// TextGlyph is one shown glyph reported to a TextSink, in device space. It carries what
// text reconstruction needs: the source rune, the glyph's placement and size, and its
// advance. Position is the glyph origin (the pen position on the baseline) mapped
// through the text-rendering matrix and CTM; SizePt is the effective font size in device
// units; Advance is the horizontal advance in device units. Rune is the best-effort
// Unicode mapping (0 when the font provides none, e.g. a Type0/CID font without a
// ToUnicode CMap — such runs are reported with Rune==0 so the extractor can degrade).
type TextGlyph struct {
	Rune    rune    // source character, or 0 if the font gives no mapping
	X, Y    float64 // glyph origin (baseline pen position) in device space
	SizePt  float64 // effective font size in device units
	Advance float64 // horizontal advance in device units
	IsSpace bool    // the single-byte space (code 32)
	FontID  string  // opaque per-font identity for run grouping (font pointer address)
	Bold    bool    // best-effort weight, from the font descriptor when known
	Italic  bool    // best-effort slant, from the font descriptor when known
}

// VectorKind classifies a vector op reported to a GraphicsSink for table-ruling
// detection.
type VectorKind int

const (
	// VectorFill is a filled path (f/F/f*/B). A thin filled rectangle is a common
	// table rule.
	VectorFill VectorKind = iota
	// VectorStroke is a stroked path (S/s/B). A thin stroke is a common table rule.
	VectorStroke
)

// VectorOp is one painted path reported to a GraphicsSink, in device space. Path is the
// same geometry handed to the Device (already CTM-transformed). StrokeWidth is the
// device-space stroke width for a VectorStroke (0 for a fill). A table detector reads
// these to recover ruling lines (thin strokes and thin filled rectangles).
type VectorOp struct {
	Kind        VectorKind
	Path        *render.Path
	StrokeWidth float64
}

// TextSink receives every shown glyph (subject to the render mode painting anything).
// It is called from drawGlyph after the paint call, so painting is unchanged.
type TextSink func(TextGlyph)

// GraphicsSink receives every painted vector path (fill or stroke). It is called from
// fillPath/strokePath after the paint call, so painting is unchanged.
type GraphicsSink func(VectorOp)
