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
	Footnotes *Notes
	// Endnotes holds the parsed word/endnotes.xml (note id -> content), or nil.
	Endnotes *Notes
	// Comments holds the parsed word/comments.xml keyed by comment id, or nil.
	// A comment's in-text range is marked by ParaChild.CommentStart/CommentEnd
	// and its reference run by Run.CommentRef.
	Comments map[int]*Comment
	// ExtraParts holds package parts outside the modeled vocabulary that must
	// survive a read-modify-write cycle, keyed by part name (currently every
	// customXml/* part). The writer emits them verbatim; callers may add or
	// replace entries to carry app-specific data (the map itself is the one
	// deliberately caller-mutable field on an otherwise read-only Document).
	ExtraParts map[string][]byte
	// Section is the document's (single, for now) section geometry, taken from the
	// body-level w:sectPr. It is never nil after Open; a default Letter page is
	// substituted when the document declares none.
	Section SectionProps
	// Sections lists every section's geometry in document order (each terminating
	// w:sectPr, body-level or in a paragraph's pPr). doc.Section remains the last
	// (body) section. Single-section documents have len(Sections)==1.
	Sections []SectionProps
}

// Comment is one word/comments.xml entry: its identity/attribution plus the
// comment's block content. The in-text anchor is separate (CommentStart/
// CommentEnd markers + a Run.CommentRef reference run).
type Comment struct {
	ID       int
	Author   string
	Initials string
	// Date is the w:date value verbatim (ISO 8601 in practice).
	Date string
	Body []Block
}

// Relationship is one document relationship (Id -> Target), with the external
// flag set when TargetMode="External" (hyperlinks to URLs). Type is the
// relationship type URI, kept so a writer can re-emit the rels part faithfully.
type Relationship struct {
	ID       string
	Target   string
	External bool
	Type     string
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
// field is non-nil. A bare Run, a Hyperlink group wrapping runs, a Drawing
// (an embedded image), a Revision wrapper (w:ins / w:del), or a comment range
// marker (w:commentRangeStart / w:commentRangeEnd).
type ParaChild struct {
	Run       *Run
	Hyperlink *Hyperlink
	Drawing   *Drawing
	Revision  *Revision
	// CommentStart / CommentEnd mark a comment's anchored range. They are
	// position markers, not containers, because a range may cross run and
	// hyperlink boundaries. Markers found INSIDE a w:hyperlink are hoisted at
	// parse time: a start to just before the link, an end to just after (the
	// range grows outward to cover the whole link); the reverse direction is
	// expressible without a model change by splitting the link in two.
	CommentStart *CommentMark
	CommentEnd   *CommentMark
}

// Revision is a tracked change: a w:ins (insertion) or w:del (deletion)
// wrapping inline content. It is a true container in OOXML — it can wrap
// hyperlinks and nest (an insertion inside a moved range) — so it is modeled
// as a ParaChild wrapper rather than a per-run annotation, preserving the
// grouping and the revision's identity exactly once. The FINAL document state
// is: Insert content included, Delete content excluded (rendering follows
// that — Word's "No Markup" view).
type Revision struct {
	Kind RevisionKind
	// ID is the w:id revision identifier.
	ID     int
	Author string
	// Date is the w:date value verbatim (ISO 8601 in practice).
	Date    string
	Content []ParaChild
}

// RevisionKind classifies a Revision.
type RevisionKind int

const (
	// RevisionInsert is a w:ins tracked insertion.
	RevisionInsert RevisionKind = iota
	// RevisionDelete is a w:del tracked deletion (its runs carry w:delText).
	RevisionDelete
)

// CommentMark is a comment range boundary (w:commentRangeStart/End): the id
// keys into Document.Comments.
type CommentMark struct {
	ID int
}

// RevisionMark is a revision annotation without content of its own — a table
// cell's w:cellIns / w:cellDel, and the identity half of a properties change.
type RevisionMark struct {
	ID     int
	Author string
	Date   string
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
// EMU = 1in).
type Drawing struct {
	RelID     string
	WidthEMU  int64
	HeightEMU int64
	// Anchored is true for a floating wp:anchor drawing (false = wp:inline).
	Anchored bool
	// WrapKind is the anchored drawing's text-wrap mode: "square",
	// "topAndBottom", "tight", "through", or "none" ("" for inline drawings).
	WrapKind string
	// AlignH is the anchored drawing's horizontal alignment from
	// wp:positionH/wp:align ("left", "right", "center"), or "" when positioned
	// by offset.
	AlignH string
	// Description is the wp:docPr descr attribute (alt text), or "".
	Description string
	// Title is the wp:docPr title attribute, or "". Distinct from Description:
	// Word exposes both a title and a longer description in the alt-text dialog.
	Title string
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
	// Ins / Del are the cell's tracked-change marks (w:cellIns / w:cellDel in
	// tcPr): the cell was inserted/deleted as part of a table-row revision.
	// Rendering shows the final state (marks are identity only).
	Ins *RevisionMark
	Del *RevisionMark
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
	// LayoutFixed is w:tblLayout type="fixed": the grid column widths are
	// authoritative (consumers skip the auto-fit algorithm). false = auto.
	LayoutFixed bool
}

// RowProps holds row-level properties (w:trPr). Populated in the tables phase.
type RowProps struct {
	IsHeader  bool  // w:tblHeader
	HeightDxa Twips // w:trHeight
}

// CellProps holds cell-level properties (w:tcPr).
type CellProps struct {
	Borders  BoxBorders
	Shading  Shading
	WidthDxa Twips // w:tcW type="dxa"; 0 = unset
	VAlign   CellVAlign
	// TcPrChange is the tracked change of the cell's properties (w:tcPrChange):
	// Previous holds the BEFORE state; the fields above are the after state.
	TcPrChange *CellPropsChange
}

// CellPropsChange is a tracked cell-properties change (w:tcPrChange).
type CellPropsChange struct {
	Mark     RevisionMark
	Previous CellProps
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

// BoxBorders holds the four edge borders of a table, cell, or paragraph.
type BoxBorders struct {
	Top, Bottom, Left, Right Border
}

// Border is one edge border (w:tblBorders/w:tcBorders child). SizeEighthPt is
// w:sz in eighths of a point. None is true when style="nil"/"none". Style is
// the ST_Border style name as written ("single", "dashed", "dotted", "double",
// ...), or "" when the edge is unset/none — rendering treats every non-none
// style as a solid rule, but the name round-trips for conversion.
type Border struct {
	None         bool
	Style        string
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
	// EndnoteRef mirrors FootnoteRef for w:endnoteReference (Document.Endnotes).
	EndnoteRef int
	// CommentRef is the id of a comment this run references (w:commentReference).
	// Comment ids number from 0 (both Word and LibreOffice start there), so
	// presence rides on HasCommentRef, not on a zero sentinel. A reference run
	// has no text and renders as nothing — the comment itself lives in
	// Document.Comments.
	CommentRef    int
	HasCommentRef bool
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

// VertAlign is a run's vertical alignment (w:vertAlign).
type VertAlign int

const (
	VertAlignBaseline VertAlign = iota
	VertAlignSuperscript
	VertAlignSubscript
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
	// Borders are the paragraph borders (w:pBdr top/left/bottom/right), or nil
	// when unset. The between/bar edges are not modeled. Rendering ignores
	// paragraph borders today; the field round-trips for conversion (the
	// HorizontalRule style carries its visible bottom rule here).
	Borders *BoxBorders
	// NumID/ILvl are the list membership from w:numPr (numId + ilvl); HasNum marks
	// them set. A paragraph with HasNum lowers to a list item.
	NumID  int
	ILvl   int
	HasNum bool
	// TabStops are the w:tabs stop positions (twips from the margin) with their
	// alignment, captured for the conversion path. Rendering uses fixed 8-column
	// stops today (custom positions are a deferred inline-core change).
	TabStops []TabStop
	// Frame is the paragraph's text frame (w:framePr) — most notably a drop cap.
	// Rendering degrades a framed paragraph to normal flow; conversion reads
	// Frame.DropCap.
	Frame *FramePr
	// PPrChange is the tracked change of the paragraph's properties
	// (w:pPrChange): Previous holds the BEFORE state; this struct's own fields
	// are the after state.
	PPrChange *ParaPropsChange
	// SectPr is the section break this paragraph carries (its pPr's w:sectPr),
	// or nil. The geometry is also appended to Document.Sections; the paragraph
	// attachment point is kept so a writer can re-emit multi-section documents
	// in place.
	SectPr *SectionProps
}

// ParaPropsChange is a tracked paragraph-properties change (w:pPrChange).
type ParaPropsChange struct {
	Mark     RevisionMark
	Previous ParagraphProps
}

// FramePr is a paragraph text frame (w:framePr). DropCap is the headline use:
// "drop" (inside the text) or "margin"; Lines is the drop height in lines. The
// remaining fields capture the frame's geometry for fidelity.
type FramePr struct {
	DropCap string
	Lines   int
	Wrap    string
	HAnchor string
	VAnchor string
	W, H    Twips
	HSpace  Twips
}

// TabStop is one w:tab definition inside w:tabs: a position (twips) and its
// alignment (left/center/right/decimal). Val "clear" removes an inherited stop.
type TabStop struct {
	PosTwips Twips
	Align    string // "left" (default), "center", "right", "decimal", "clear"
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
	// Strike is w:strike/w:dstrike (strikethrough); HasStrike marks it set.
	Strike, HasStrike bool
	// VertAlign is w:vertAlign (baseline/superscript/subscript).
	VertAlign VertAlign
	// Highlight is the w:highlight color; HasHighlight marks it set.
	Highlight    color.RGBA
	HasHighlight bool
	// HighlightName is the raw w:highlight val (e.g. "yellow", "darkGreen"), kept
	// for the conversion path so a consumer can apply its own name→color palette
	// instead of the resolved RGBA. "" when the highlight is unset.
	HighlightName string
	// Caps/SmallCaps are w:caps/w:smallCaps; Has* mark them set.
	Caps, HasCaps           bool
	SmallCaps, HasSmallCaps bool
	// UnderlineStyle is the w:u val (e.g. "single","double","dotted","wave"), kept
	// for the conversion path; rendering treats any non-none as a plain underline.
	UnderlineStyle string
	// UnderlineColor is the w:u color; HasUnderlineColor marks it set.
	UnderlineColor    color.RGBA
	HasUnderlineColor bool
	// StyleID is the referenced character style (w:rStyle val), or "" if none.
	// Only the identity is modeled (there is no character-style formatting
	// cascade); the conversion path uses it to recover run semantics (e.g. the
	// CodeChar style marks inline code).
	StyleID string
	// Shd is the run's background shading (w:shd in rPr) — the character-level
	// background a textStyle backgroundColor maps to.
	Shd Shading
	// RPrChange is the tracked change of the run's properties (w:rPrChange):
	// Previous holds the BEFORE state; this struct's own fields are the after
	// state (what Word shows with markup off).
	RPrChange *RunPropsChange
}

// RunPropsChange is a tracked run-properties change (w:rPrChange).
type RunPropsChange struct {
	Mark     RevisionMark
	Previous RunProps
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
