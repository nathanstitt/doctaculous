package docx

import "image/color"

// Twips is the fundamental WordprocessingML length unit: one twentieth of a
// point, i.e. 1/1440 inch. Page geometry, margins, indents, and paragraph
// spacing are all expressed in twips.
type Twips int

// Points converts twips to typographic points (1 pt = 20 twips).
func (t Twips) Points() float64 { return float64(t) / 20 }

// Inches converts twips to inches (1 in = 1440 twips).
func (t Twips) Inches() float64 { return float64(t) / 1440 }

// Document is the parsed, read-only WordprocessingML model. After Open it is not
// mutated, so it is safe to share across goroutines (e.g. the per-page render
// fan-out). It deliberately mirrors only the subset of OOXML this renderer
// understands; unknown elements are dropped at parse time.
type Document struct {
	// Body holds the document's block-level content in order.
	Body []Block
	// Styles holds the resolved style table (docDefaults + named styles). It may
	// be nil if the package has no styles part.
	Styles *Styles
	// Section is the document's (single, for now) section geometry, taken from the
	// body-level w:sectPr. It is never nil after Open; a default Letter page is
	// substituted when the document declares none.
	Section SectionProps
}

// Block is a top-level flow item. For now only paragraphs are modeled; tables and
// other block types are added in later phases.
type Block struct {
	// Paragraph is set for a w:p block.
	Paragraph *Paragraph
}

// Paragraph is a w:p: a sequence of runs sharing paragraph-level properties.
type Paragraph struct {
	Props ParagraphProps
	Runs  []Run
}

// Run is a w:r: a span of text sharing run-level (character) properties, plus an
// optional break that follows the text.
type Run struct {
	Props RunProps
	// Text is the concatenation of the run's w:t content, with xml:space="preserve"
	// honored (leading/trailing whitespace kept when declared).
	Text string
	// Break records a w:br within the run (page/column/line); BreakNone when absent.
	Break BreakKind
}

// BreakKind classifies a w:br inside a run.
type BreakKind int

const (
	// BreakNone means the run carries no break.
	BreakNone BreakKind = iota
	// BreakLine is a soft line break (w:br with no type, or type="textWrapping").
	BreakLine
	// BreakPage is a hard page break (w:br type="page").
	BreakPage
	// BreakColumn is a column break (w:br type="column"); treated as a line break
	// until multi-column layout lands.
	BreakColumn
)

// Justify is a paragraph horizontal alignment (w:jc).
type Justify int

const (
	// JustifyLeft is the default left alignment (w:jc "left"/"start" or unset).
	JustifyLeft Justify = iota
	// JustifyCenter centers each line (w:jc "center").
	JustifyCenter
	// JustifyRight right-aligns each line (w:jc "right"/"end").
	JustifyRight
	// JustifyBoth fully justifies all but the last line (w:jc "both"/"distribute").
	JustifyBoth
)

// LineRule selects how the w:spacing w:line value is interpreted (w:lineRule).
type LineRule int

const (
	// LineRuleAuto multiplies the font's natural line height; Line is in 240ths of
	// a line (240 = single). This is Word's default.
	LineRuleAuto LineRule = iota
	// LineRuleExact fixes the line height to exactly Line twips.
	LineRuleExact
	// LineRuleAtLeast sets a minimum line height of Line twips.
	LineRuleAtLeast
)

// ParagraphProps holds the directly-specified paragraph properties (w:pPr). Zero
// values mean "unspecified"; the style cascade fills in inherited defaults.
type ParagraphProps struct {
	// StyleID is the referenced paragraph style (w:pStyle val), or "" if none.
	StyleID string
	// Justify is the alignment; HasJustify distinguishes an explicit JustifyLeft
	// from "unspecified."
	Justify    Justify
	HasJustify bool
	// SpacingBefore/After are the space above/below the paragraph (w:spacing
	// w:before/w:after); HasSpacing* mark them as explicitly set.
	SpacingBefore, SpacingAfter       Twips
	HasSpacingBefore, HasSpacingAfter bool
	// Line/LineRule give the line height (w:spacing w:line/w:lineRule); HasLine
	// marks them set.
	Line     Twips
	LineRule LineRule
	HasLine  bool
	// IndentLeft/Right and FirstLine are paragraph indents (w:ind); Has* mark set.
	IndentLeft, IndentRight, FirstLine          Twips
	HasIndentLeft, HasIndentRight, HasFirstLine bool
	// PageBreakBefore forces the paragraph to start a new page (w:pageBreakBefore).
	PageBreakBefore bool
}

// RunProps holds the directly-specified run (character) properties (w:rPr). Bool
// toggles are paired with Has* so the cascade can tell "off" from "unspecified."
type RunProps struct {
	Bold, Italic, Underline          bool
	HasBold, HasItalic, HasUnderline bool
	// SizeHalfPts is the font size in half-points (w:sz); 0 + !HasSize = unset.
	SizeHalfPts int
	HasSize     bool
	// Color is the text color (w:color RRGGBB); HasColor distinguishes explicit
	// black from unset.
	Color    color.RGBA
	HasColor bool
	// Family is the primary font family (w:rFonts ascii/hAnsi), or "" if unset.
	Family string
}

// SectionProps is the page geometry from a w:sectPr: paper size and margins, all
// in twips.
type SectionProps struct {
	PageW, PageH            Twips
	MarginTop, MarginBottom Twips
	MarginLeft, MarginRight Twips
	Header, Footer, Gutter  Twips
}

// defaultSection is US Letter (8.5in × 11in) with 1in margins — Word's default —
// used when a document declares no w:sectPr or omits fields.
func defaultSection() SectionProps {
	return SectionProps{
		PageW: 12240, PageH: 15840,
		MarginTop: 1440, MarginBottom: 1440,
		MarginLeft: 1440, MarginRight: 1440,
		Header: 720, Footer: 720,
	}
}
