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
	// Numbering holds the parsed word/numbering.xml (list definitions), or nil if
	// the document has no numbering part.
	Numbering *Numbering
	// Rels maps a relationship id (r:id) to its target for the main document part
	// (external hyperlink URLs, image parts). Empty if the document has no rels.
	Rels map[string]Relationship
	// Media maps an image part name (e.g. "word/media/image1.png") to its raw
	// bytes, for drawings to decode. Empty if the document embeds no media.
	Media map[string][]byte
	// Headers/Footers map a header/footer part's relationship id to its parsed
	// content, for a section's HeaderRefDefault/FooterRefDefault to resolve.
	Headers map[string]*HeaderFooter
	Footers map[string]*HeaderFooter
	// Footnotes holds the parsed word/footnotes.xml (note id -> content), or nil.
	Footnotes *Footnotes
	// Section is the document's (single, for now) section geometry, taken from the
	// body-level w:sectPr. It is never nil after Open; a default Letter page is
	// substituted when the document declares none.
	Section SectionProps
	// Sections lists every section's geometry in document order (each terminating
	// w:sectPr, body-level or in a paragraph's pPr). doc.Section remains the last
	// (body) section. Single-section documents have len(Sections)==1.
	Sections []SectionProps
}

// Relationship is one document relationship (Id -> Target), with the external
// flag set when TargetMode="External" (hyperlinks to URLs).
type Relationship struct {
	ID       string
	Target   string
	External bool
	relType  string
}

// Block is a top-level flow item: exactly one field is non-nil. A paragraph
// (w:p) or a table (w:tbl).
type Block struct {
	// Paragraph is set for a w:p block.
	Paragraph *Paragraph
	// Table is set for a w:tbl block.
	Table *Table
}

// Paragraph is a w:p: a sequence of inline children (runs, hyperlink groups,
// drawings) sharing paragraph-level properties.
type Paragraph struct {
	Props   ParagraphProps
	Content []ParaChild
}

// ParaChild is one inline-level member of a paragraph's content: exactly one
// field is non-nil. A bare Run, a Hyperlink group wrapping runs, or a Drawing
// (an embedded image).
type ParaChild struct {
	Run       *Run
	Hyperlink *Hyperlink
	Drawing   *Drawing
}

// Hyperlink is a w:hyperlink: a group of runs linking to an external URL
// (resolved from RelID via the document relationships) or an internal Anchor
// (bookmark). The parser sets RelID + Anchor + Runs.
type Hyperlink struct {
	// RelID is the r:id relationship id referencing the external target, or "".
	RelID  string
	Anchor string
	// Target is the resolved external URL. It is RESERVED for the conversion path
	// (DOCX→HTML/markdown), which will resolve RelID through Document.Rels; the
	// current parse+render path does not populate it (render styles the link inline
	// and reads only Runs).
	Target string
	Runs   []Run
}

// Drawing is a w:drawing carrying an embedded image: RelID references the image
// part via the document relationships; WidthEMU/HeightEMU are the extent (914400
// EMU = 1in). Later phases populate it; Phase 1 only declares it.
type Drawing struct {
	RelID     string
	WidthEMU  int64
	HeightEMU int64
}

// Table is a w:tbl: a column grid plus rows. Props carries table-level borders,
// shading, width, and alignment. Later phases populate Props; Phase 1 declares
// the shape.
type Table struct {
	Grid  []Twips // w:tblGrid column widths
	Rows  []TableRow
	Props TableProps
}

// TableRow is a w:tr. Props carries row height and header/split flags.
type TableRow struct {
	Cells []TableCell
	Props RowProps
}

// TableCell is a w:tc. Blocks holds the cell's content (paragraphs, nested
// tables — the recursion). GridSpan is w:gridSpan (horizontal span; default 1).
// VMerge records vertical merging (row spanning). Props carries cell borders,
// shading, width, and vertical alignment.
type TableCell struct {
	Blocks   []Block
	GridSpan int
	VMerge   VMergeKind
	Props    CellProps
}

// VMergeKind classifies a cell's w:vMerge state.
type VMergeKind int

const (
	// VMergeNone means the cell is not vertically merged.
	VMergeNone VMergeKind = iota
	// VMergeRestart begins a vertical merge (w:vMerge val="restart" or a bare
	// w:vMerge with no val on the first row of a span).
	VMergeRestart
	// VMergeContinue continues the merge above (w:vMerge val="continue").
	VMergeContinue
)

// TableProps holds table-level properties (w:tblPr). Fields are populated in the
// tables phase.
type TableProps struct {
	Borders  BoxBorders
	Shading  Shading
	WidthPct int   // w:tblW type="pct" (in fiftieths of a percent per OOXML); 0 = unset
	WidthDxa Twips // w:tblW type="dxa"; 0 = unset
	Justify  Justify
}

// RowProps holds row-level properties (w:trPr). Populated in the tables phase.
type RowProps struct {
	IsHeader  bool  // w:tblHeader
	HeightDxa Twips // w:trHeight
}

// CellProps holds cell-level properties (w:tcPr). Populated in the tables phase.
type CellProps struct {
	Borders  BoxBorders
	Shading  Shading
	WidthDxa Twips // w:tcW type="dxa"; 0 = unset
	VAlign   CellVAlign
}

// CellVAlign is a cell's vertical alignment (w:vAlign).
type CellVAlign int

const (
	// VAlignTop is w:vAlign val="top" (the default) — cell content aligns to the top.
	VAlignTop CellVAlign = iota
	// VAlignCenter is w:vAlign val="center" — cell content is vertically centered.
	VAlignCenter
	// VAlignBottom is w:vAlign val="bottom" — cell content aligns to the bottom.
	VAlignBottom
)

// BoxBorders holds the four edge borders of a table or cell. Populated in the
// tables phase.
type BoxBorders struct {
	Top, Bottom, Left, Right Border
}

// Border is one edge border (w:tblBorders/w:tcBorders child). SizeEighthPt is
// w:sz in eighths of a point. None is true when style="nil"/"none".
type Border struct {
	None         bool
	SizeEighthPt int
	Color        color.RGBA
	HasColor     bool
}

// Shading is a cell/table background fill (w:shd). HasFill distinguishes an
// explicit fill from "unset"/"auto".
type Shading struct {
	Fill    color.RGBA
	HasFill bool
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
	// FootnoteRef is the id of a footnote this run references (w:footnoteReference
	// w:id); 0 = none. Such a run has no text; it renders as a superscript marker.
	FootnoteRef int
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
	// NumID/ILvl are the list membership from w:numPr (numId + ilvl); HasNum marks
	// them set. A paragraph with HasNum lowers to a list item.
	NumID  int
	ILvl   int
	HasNum bool
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
	// HeaderRefDefault/FooterRefDefault are the r:ids of the default header/footer
	// parts referenced by this section (w:headerReference/w:footerReference
	// type="default"), or "" when none. (even/first variants are a follow-up.)
	HeaderRefDefault string
	FooterRefDefault string
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
