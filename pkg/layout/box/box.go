// Package box defines the format-neutral input model for the reflow engine. It
// is the contract between document frontends (DOCX today; HTML/EPUB later) and
// the engine in pkg/layout: a frontend's whole job is to lower its native model
// into a box.Document, after which the engine, paint, and raster stages are
// shared and format-agnostic.
//
// The model is deliberately small and presentational. Everything is already
// resolved — styles are flattened, units are points, colors are concrete — so no
// format concepts (pStyle, CSS selectors, twips) leak across the boundary. New
// box-model vocabulary added here (lists, tables, images) becomes available to
// every frontend at once.
package box

import "image/color"

// Document is the complete neutral input to the reflow engine: page geometry plus
// the top-level block flow.
type Document struct {
	Page   PageGeometry
	Blocks []Block
}

// PageGeometry is the page size and margins, all in points (1/72 inch). The
// content area is the page minus its margins.
type PageGeometry struct {
	WidthPt, HeightPt                                        float64
	MarginTopPt, MarginBottomPt, MarginLeftPt, MarginRightPt float64
}

// Align is a paragraph's horizontal alignment.
type Align int

const (
	// AlignLeft is the default ragged-right alignment.
	AlignLeft Align = iota
	// AlignCenter centers each line in the content width.
	AlignCenter
	// AlignRight aligns each line to the right edge.
	AlignRight
	// AlignJustify stretches inter-word space so every line but the last fills the
	// content width.
	AlignJustify
)

// LineHeightMode selects how a Block's line height is computed.
type LineHeightMode int

const (
	// LineHeightAuto multiplies the line's natural font height by Mult.
	LineHeightAuto LineHeightMode = iota
	// LineHeightExact fixes the line height to ValuePt regardless of font.
	LineHeightExact
	// LineHeightAtLeast uses the larger of ValuePt and the natural font height.
	LineHeightAtLeast
)

// LineHeight describes a block's line spacing.
type LineHeight struct {
	Mode    LineHeightMode
	Mult    float64 // multiplier for LineHeightAuto (e.g. 1.15)
	ValuePt float64 // fixed/minimum height for Exact/AtLeast, in points
}

// Block is a paragraph-level box: a sequence of inline spans plus block-level
// presentation. Lists, tables, and images extend this model in later phases.
type Block struct {
	Inlines    []Inline
	Align      Align
	LineHeight LineHeight
	// SpaceBeforePt/SpaceAfterPt are the vertical gaps above/below the block.
	SpaceBeforePt, SpaceAfterPt float64
	// IndentLeftPt/IndentRightPt inset the block from the content edges;
	// FirstLinePt additionally offsets the first line (negative = hanging indent).
	IndentLeftPt, IndentRightPt, FirstLinePt float64
	// BreakBefore forces a page break before this block.
	BreakBefore bool
}

// Inline is a styled run of text. Hard breaks and (later) inline images are
// modeled as inlines so the engine sees them in flow order.
type Inline struct {
	Text      string
	Face      FaceRef
	SizePt    float64
	Color     color.RGBA
	Underline bool
	// ForceBreak makes this inline a hard line break; Text is then ignored.
	ForceBreak bool
}

// FaceRef names a font family and weight/slant. The engine resolves it to a
// concrete face via the font cache, so frontends carry only the request, not the
// font program.
type FaceRef struct {
	Family string
	Bold   bool
	Italic bool
}
