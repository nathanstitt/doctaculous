// Package cssbox lowers a parsed DOCX document into the recursive cssbox tree the
// CSS layout engine consumes, replacing the flat pkg/docx/lower + pkg/layout/box
// path. It resolves each paragraph and run through the DOCX style cascade and emits
// concrete css.ComputedStyle values, so nothing DOCX-specific crosses the boundary.
// It lives outside pkg/docx to avoid an import cycle with pkg/docx/style.
package cssbox

import (
	"image/color"
	"strconv"
	"strings"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// PageGeometry is the DOCX section geometry in points, carried alongside the box
// tree (the cssbox tree itself is geometry-free; the engine takes width/height as
// layout inputs).
type PageGeometry struct {
	PageWidthPt, PageHeightPt                                float64
	MarginTopPt, MarginBottomPt, MarginLeftPt, MarginRightPt float64
}

// ContentWidthPt is the page width minus left/right margins (the layout viewport).
func (g PageGeometry) ContentWidthPt() float64 {
	return g.PageWidthPt - g.MarginLeftPt - g.MarginRightPt
}

// ContentHeightPt is the page height minus top/bottom margins (the pagination band).
func (g PageGeometry) ContentHeightPt() float64 {
	return g.PageHeightPt - g.MarginTopPt - g.MarginBottomPt
}

// Geometry resolves a document's section geometry into points. A nil document
// yields the zero geometry.
func Geometry(d *docx.Document) PageGeometry {
	if d == nil {
		return PageGeometry{}
	}
	s := d.Section
	return PageGeometry{
		PageWidthPt:    s.PageW.Points(),
		PageHeightPt:   s.PageH.Points(),
		MarginTopPt:    s.MarginTop.Points(),
		MarginBottomPt: s.MarginBottom.Points(),
		MarginLeftPt:   s.MarginLeft.Points(),
		MarginRightPt:  s.MarginRight.Points(),
	}
}

// Lower converts a parsed DOCX document into a cssbox tree the CSS layout engine
// consumes. The tree mirrors HTML's nesting — an outer root block (the <html>
// analogue) with a single body block child (the <body> analogue) that holds the
// paragraph blocks — because the paged engine locates the document's top-level
// blocks as root.Children[last].Children (its bodyFragment lookup). A nil document
// or resolver yields the empty root/body pair rather than panicking. Page geometry
// is obtained separately via Geometry(d).
func Lower(d *docx.Document, r *style.Resolver) *lcssbox.Box {
	newWrapper := func() *lcssbox.Box {
		st := gcss.InitialStyle()
		st.Display = "block" // match the box-level DisplayBlock (Style.Display is unread by layout, but reads clearly)
		return &lcssbox.Box{
			Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayBlock, Formatting: lcssbox.BlockFC,
			Style: st,
		}
	}
	root := newWrapper()
	body := newWrapper()
	root.Children = []*lcssbox.Box{body}
	if d == nil || r == nil {
		return root
	}
	body.Children = lowerBlocks(d.Body, r, d.Numbering, d.Rels, newListCounter())
	return root
}

// RunningHeaderName / RunningFooterName are the synthetic running-element names
// under which a DOCX section's default header/footer are keyed, referenced from
// the synthesized @page margin boxes via element(name).
const (
	RunningHeaderName = "docxheader"
	RunningFooterName = "docxfooter"
)

// LowerRunning lowers the document's default header and footer (if any) into
// running-element boxes keyed by RunningHeaderName/RunningFooterName, for the
// paged engine's @page margin boxes to paint on every page. Returns an empty map
// when the section has no header/footer — the byte-identical path (no margin box
// is synthesized, so element() never fires).
func LowerRunning(d *docx.Document, r *style.Resolver) map[string]*lcssbox.Box {
	out := map[string]*lcssbox.Box{}
	if d == nil {
		return out
	}
	if hf := headerFooterFor(d.Section.HeaderRefDefault, d.Headers); hf != nil {
		out[RunningHeaderName] = runningBox(hf, r, d.Numbering)
	}
	if hf := headerFooterFor(d.Section.FooterRefDefault, d.Footers); hf != nil {
		out[RunningFooterName] = runningBox(hf, r, d.Numbering)
	}
	return out
}

// headerFooterFor looks up a header/footer by ref id, returning nil when the ref
// is empty or unresolved.
func headerFooterFor(refID string, m map[string]*docx.HeaderFooter) *docx.HeaderFooter {
	if refID == "" || m == nil {
		return nil
	}
	return m[refID]
}

// runningBox lowers a header/footer's blocks into a single block box (the running
// element the margin box paints).
func runningBox(hf *docx.HeaderFooter, r *style.Resolver, num *docx.Numbering) *lcssbox.Box {
	box := &lcssbox.Box{
		Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayBlock, Formatting: lcssbox.BlockFC,
		Style: gcss.InitialStyle(),
	}
	box.Children = lowerBlocks(hf.Blocks, r, num, nil, newListCounter())
	return box
}

// lowerBlocks lowers a sequence of DOCX blocks (paragraphs, list items, tables).
// num is the document's numbering (may be nil); rels resolves hyperlink relationship
// ids to their target URLs for the conversion path (may be nil — headers/footers pass
// nil, so their links carry no Href); ctr threads list-counter state.
func lowerBlocks(blocks []docx.Block, r *style.Resolver, num *docx.Numbering, rels map[string]docx.Relationship, ctr *listCounter) []*lcssbox.Box {
	var out []*lcssbox.Box
	for i := 0; i < len(blocks); {
		blk := blocks[i]
		switch {
		case blk.Paragraph != nil && blk.Paragraph.Props.HasNum && num != nil:
			// A run of consecutive numbered paragraphs is one list: group it under
			// a container box (nested by ilvl) so the conversion writers see the
			// same shape HTML lists produce — without a container, list items are
			// loose siblings of ordinary paragraphs, which flattens nesting and
			// makes any mixed body misread as a single list.
			j := i
			for j < len(blocks) && blocks[j].Paragraph != nil && blocks[j].Paragraph.Props.HasNum {
				j++
			}
			out = append(out, lowerListRun(blocks[i:j], r, num, rels, ctr))
			i = j
		case blk.Paragraph != nil:
			out = append(out, lowerParagraph(blk.Paragraph, r, rels)...)
			i++
		case blk.Table != nil:
			out = append(out, lowerTable(blk.Table, r, num, rels))
			i++
		default:
			i++
		}
	}
	return out
}

// lowerParagraph resolves a paragraph's effective formatting and lowers its runs
// into a block box (an inline formatting context) whose children are styled text-run
// boxes. A page break inside a run splits the paragraph into two blocks, the second
// carrying break-before:page so flow continues onto a new page; a line/column break
// lowers to a preserved-newline text box (the IFC hard-break mechanism).
func lowerParagraph(p *docx.Paragraph, r *style.Resolver, rels map[string]docx.Relationship) []*lcssbox.Box {
	eff := r.EffectiveParagraph(p.Props)
	semTag, headingLvl := paragraphSemantics(p.Props.StyleID)
	newBlock := func() *lcssbox.Box {
		return &lcssbox.Box{
			Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayBlock,
			Formatting: lcssbox.InlineFC, Style: paragraphStyle(eff),
			SemTag: semTag, HeadingLvl: headingLvl,
		}
	}
	var blocks []*lcssbox.Box
	var floats []*lcssbox.Box
	cur := newBlock()
	for _, child := range effectiveContent(p.Content) {
		if child.Hyperlink != nil {
			href := hyperlinkURL(child.Hyperlink, rels)
			for _, run := range child.Hyperlink.Runs {
				if run.Text == "" {
					continue
				}
				er := r.EffectiveRun(p.Props, run.Props)
				cur.Children = append(cur.Children, linkTextBox(run.Text, er, cur.Style, href))
			}
			continue
		}
		if child.Drawing != nil {
			if fk := drawingFloat(child.Drawing); fk != lcssbox.FloatNone {
				// An anchored square-wrap drawing floats: emit it as a block-level
				// sibling BEFORE the paragraph block so its band narrows the
				// paragraph's lines (the engine places floats in block flow).
				fb := drawingBox(child.Drawing, cur.Style)
				fb.Display = lcssbox.DisplayBlock
				fb.Style.Display = "block"
				fb.Float = fk
				floats = append(floats, fb)
				continue
			}
			cur.Children = append(cur.Children, drawingBox(child.Drawing, cur.Style))
			continue
		}
		if child.Run == nil {
			continue
		}
		run := *child.Run
		if run.FootnoteRef > 0 {
			er := r.EffectiveRun(p.Props, run.Props)
			cur.Children = append(cur.Children, footnoteMarker(run.FootnoteRef, er, cur.Style))
			continue
		}
		if run.EndnoteRef > 0 {
			// Endnote references render exactly like footnote markers (the note
			// body lives at document end; the in-text marker is the same shape).
			er := r.EffectiveRun(p.Props, run.Props)
			cur.Children = append(cur.Children, footnoteMarker(run.EndnoteRef, er, cur.Style))
			continue
		}
		switch run.Break {
		case docx.BreakPage:
			blocks = append(blocks, cur)
			cur = newBlock()
			cur.Style.BreakBefore = "page"
			continue
		case docx.BreakLine, docx.BreakColumn:
			cur.Children = append(cur.Children, hardBreakBox(cur.Style))
			continue
		}
		if run.Text == "" {
			continue
		}
		er := r.EffectiveRun(p.Props, run.Props)
		cur.Children = append(cur.Children, runTextBox(run.Text, er, cur.Style))
	}
	blocks = append(blocks, cur)
	return append(floats, blocks...)
}

// drawingFloat maps an anchored drawing's wrap facts onto a CSS float: square
// wrap aligned left/right floats (text wraps beside it); every other anchor
// mode degrades to inline flow.
func drawingFloat(dr *docx.Drawing) lcssbox.FloatKind {
	if !dr.Anchored || dr.WrapKind != "square" {
		return lcssbox.FloatNone
	}
	switch dr.AlignH {
	case "left":
		return lcssbox.FloatLeft
	case "right":
		return lcssbox.FloatRight
	}
	return lcssbox.FloatNone
}

// effectiveContent flattens a paragraph's tracked changes into the document's
// FINAL state — Word's "No Markup" view, and the natural reading of "the
// document as it will be": insertion content is included in place, deletion
// content is excluded, comment range markers vanish (they are anchors, not
// content), and comment reference runs (which carry no text) are dropped.
func effectiveContent(children []docx.ParaChild) []docx.ParaChild {
	// Fast path: no revision/comment constructs → the slice as-is.
	plain := true
	for i := range children {
		if children[i].Revision != nil || children[i].CommentStart != nil ||
			children[i].CommentEnd != nil || (children[i].Run != nil && children[i].Run.CommentRef > 0) {
			plain = false
			break
		}
	}
	if plain {
		return children
	}
	var out []docx.ParaChild
	for _, ch := range children {
		switch {
		case ch.Revision != nil:
			if ch.Revision.Kind == docx.RevisionInsert {
				out = append(out, effectiveContent(ch.Revision.Content)...)
			}
			// A delete's content is excluded: it is no longer part of the document.
		case ch.CommentStart != nil, ch.CommentEnd != nil:
		case ch.Run != nil && ch.Run.CommentRef > 0:
		default:
			out = append(out, ch)
		}
	}
	return out
}

// paragraphStyle maps a resolved DOCX paragraph onto a block ComputedStyle: alignment,
// space-before/after → vertical margins, left/right indent → horizontal margins,
// first-line indent → text-indent (signed; negative = hanging), page-break-before, and
// the line-spacing rule (auto/exact/atLeast) via applyLineHeight.
func paragraphStyle(eff style.EffectiveParagraph) gcss.ComputedStyle {
	// Start from the CSS initial values so Width/Height/MaxWidth carry auto/none (a
	// bare literal would leave them {0, UnitPx} = an explicit 0px, collapsing the
	// block to zero size), then overlay the paragraph's resolved formatting.
	cs := gcss.InitialStyle()
	cs.Display = "block"
	cs.TextAlign = alignString(eff.Justify)
	if eff.PageBreakBefore {
		cs.BreakBefore = "page" // paragraph-level w:pageBreakBefore (distinct from a mid-run page break)
	}
	cs.MarginTop = pt(eff.SpaceBeforePt)
	cs.MarginBottom = pt(eff.SpaceAfterPt)
	cs.MarginLeft = pt(eff.IndentLeftPt)
	cs.MarginRight = pt(eff.IndentRightPt)
	cs.TextIndent = pt(eff.FirstLinePt) // negative = hanging
	applyLineHeight(&cs, eff)
	return cs
}

// runTextBox lowers a run's text into an inline text box, inheriting the paragraph's
// block-level context (line-height/text-align/indent) so the IFC sees it, then
// overlaying the run's resolved character formatting.
func runTextBox(text string, er style.EffectiveRun, para gcss.ComputedStyle) *lcssbox.Box {
	cs := para // inherit block-level context (line-height/text-align) for the IFC
	cs.Display = "inline"
	cs.WhiteSpace = "normal" // run text collapses spaces normally
	cs.FontFamily = er.Family
	cs.Bold = er.Bold
	cs.Italic = er.Italic
	cs.FontSizePt = er.SizePt
	cs.Color = er.Color
	if er.Underline {
		cs.TextDecorationLine = "underline"
	} else {
		cs.TextDecorationLine = "none"
	}
	if er.Strike {
		cs.TextDecorationLine = "line-through" // wins over underline when both set (rare)
	}
	switch er.VertAlign {
	case docx.VertAlignSuperscript:
		cs.VerticalAlign = "super"
		cs.FontSizePt = er.SizePt * 0.75
	case docx.VertAlignSubscript:
		cs.VerticalAlign = "sub"
		cs.FontSizePt = er.SizePt * 0.75
	}
	if er.HasShd {
		cs.BackgroundColor = er.Shd
	}
	if er.HasHighlight {
		cs.BackgroundColor = er.Highlight // a highlight paints over run shading
	}
	if er.Caps || er.SmallCaps {
		cs.TextTransform = "uppercase" // small-caps approximated as uppercase (true small-caps needs synthesized glyphs)
	}
	box := &lcssbox.Box{Kind: lcssbox.BoxText, Text: text, Style: cs, Display: lcssbox.DisplayInline}
	// A CodeChar character-style reference marks inline code for the conversion
	// path (the visual identity — the monospace family — rides on direct rPr).
	if key := strings.ToLower(strings.ReplaceAll(er.StyleID, " ", "")); key == "codechar" || key == "code" {
		box.SemTag = "code"
	}
	return box
}

// footnoteMarker renders a footnote reference as a superscript number. The note
// text itself is placed by the (deferred) footnote-collection pass; here we show
// the in-text marker so the reference is visible and copyable.
func footnoteMarker(id int, er style.EffectiveRun, para gcss.ComputedStyle) *lcssbox.Box {
	box := runTextBox(strconv.Itoa(id), er, para)
	box.Style.VerticalAlign = "super"
	// Superscripts render smaller; approximate with 0.75em of the run size.
	box.Style.FontSizePt = er.SizePt * 0.75
	return box
}

// linkTextBox lowers a hyperlink run's text into an inline box styled as a link:
// the run's own formatting, overlaid with link blue + underline (the HTML a:link
// UA default). href is the resolved target URL (may be "" for an internal anchor or
// an unresolvable rel); it is carried as SemTag "a" + Href for the conversion path and
// ignored by the visual backends.
func linkTextBox(text string, er style.EffectiveRun, para gcss.ComputedStyle, href string) *lcssbox.Box {
	box := runTextBox(text, er, para)
	box.Style.Color = color.RGBA{R: 0x00, G: 0x00, B: 0xEE, A: 0xFF}
	box.Style.TextDecorationLine = "underline"
	box.SemTag = "a"
	box.Href = href
	return box
}

// hyperlinkURL resolves a hyperlink's target URL for the conversion path. It prefers
// the pre-resolved Target, then looks up RelID in rels (external relationships carry a
// URL Target). An internal-anchor link (no RelID, only Anchor) or an unresolvable id
// yields "", so the writer degrades to plain text.
func hyperlinkURL(h *docx.Hyperlink, rels map[string]docx.Relationship) string {
	if h == nil {
		return ""
	}
	if h.Target != "" {
		return h.Target
	}
	if h.RelID != "" && rels != nil {
		if rel, ok := rels[h.RelID]; ok {
			return rel.Target
		}
	}
	return ""
}

// docxHeadingStyles maps Word's built-in style ids to a SemTag role. Heading1..Heading9
// are handled separately (they derive a numeric level); this covers the fixed roles.
// Keys are the canonical camel-case style ids Word emits; matching is done after
// stripping spaces and lowercasing so "Heading 1"/"heading1" display forms also match.
var docxHeadingStyles = map[string]string{
	"title":        "h1",
	"subtitle":     "h2",
	"quote":        "blockquote",
	"intensequote": "blockquote",
	// CodeBlock/HorizontalRule are the docxwrite writer's carriers for the pre and
	// hr semantics (Word has no built-in equivalents); mapping them back closes
	// the round trip, and a real Word doc using such style names reads sensibly.
	"codeblock":      "pre",
	"horizontalrule": "hr",
}

// paragraphSemantics derives a SemTag and heading level from a paragraph's style id.
// DOCX heading semantics live in the named style ("Heading1", "Title", ...), not in the
// resolved font size, so this is the only place a heading level is recoverable. It
// returns ("", 0) for an ordinary paragraph. HeadingN with N>6 clamps to 6 (Markdown's
// maximum depth).
func paragraphSemantics(styleID string) (semTag string, level int) {
	key := strings.ToLower(strings.ReplaceAll(styleID, " ", ""))
	if key == "" {
		return "", 0
	}
	if strings.HasPrefix(key, "heading") {
		if n, err := strconv.Atoi(key[len("heading"):]); err == nil && n >= 1 {
			if n > 6 {
				n = 6
			}
			return "h" + strconv.Itoa(n), n
		}
	}
	if role, ok := docxHeadingStyles[key]; ok {
		return role, 0
	}
	return "", 0
}

// hardBreakBox lowers a DOCX line/column break to a preserved-newline text box. Only
// the break box carries white-space:pre-line (so its '\n' becomes a hard break in the
// IFC); the text runs stay "normal" so their spaces collapse normally.
func hardBreakBox(para gcss.ComputedStyle) *lcssbox.Box {
	cs := para
	cs.Display = "inline"
	cs.WhiteSpace = "pre-line" // a preserved '\n' becomes a hard break in the IFC
	return &lcssbox.Box{Kind: lcssbox.BoxText, Text: "\n", Style: cs, Display: lcssbox.DisplayInline}
}

// alignString maps a DOCX Justify onto the CSS text-align keyword.
func alignString(j docx.Justify) string {
	switch j {
	case docx.JustifyCenter:
		return "center"
	case docx.JustifyRight:
		return "right"
	case docx.JustifyBoth:
		return "justify"
	default:
		return "left"
	}
}

// pt builds a point-valued Length.
func pt(v float64) gcss.Length { return gcss.Length{Value: v, Unit: gcss.UnitPt} }

// applyLineHeight maps DOCX line spacing onto LineHeight (auto/exact) and
// LineHeightMin (atLeast). CRITICAL: for auto, set an explicit UnitAuto or a real em
// multiple — NEVER leave LineHeight zero-valued ({0,UnitPx}), which resolves to a
// literal line height of 0 (every DOCX line would collapse to height 0).
func applyLineHeight(cs *gcss.ComputedStyle, eff style.EffectiveParagraph) {
	if !eff.HasLine {
		cs.LineHeight = gcss.Length{Unit: gcss.UnitAuto} // metrics-based default
		return
	}
	switch eff.LineRule {
	case docx.LineRuleExact:
		cs.LineHeight = pt(eff.LineValue) // EffectiveParagraph converts exact/atLeast to points
	case docx.LineRuleAtLeast:
		cs.LineHeight = gcss.Length{Unit: gcss.UnitAuto} // natural height, floored by the min
		cs.LineHeightMin = pt(eff.LineValue)
	default: // auto: LineValue in 240ths of a line
		if mult := eff.LineValue / 240; mult > 0 {
			cs.LineHeight = gcss.Length{Value: mult, Unit: gcss.UnitEm}
		} else {
			cs.LineHeight = gcss.Length{Unit: gcss.UnitAuto}
		}
	}
}
