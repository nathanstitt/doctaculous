package docx

import (
	"fmt"
	"strconv"
	"strings"
)

// documentXML renders word/document.xml: the body blocks followed by the
// body-level section properties.
func (dw *docWriter) documentXML() ([]byte, error) {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`)
	if err := dw.writeBlocks(&sb, dw.doc.Body); err != nil {
		return nil, err
	}
	dw.writeSectPr(&sb, dw.doc.Section)
	sb.WriteString("</w:body></w:document>\n")
	return []byte(sb.String()), nil
}

// writeBlocks renders a block sequence (the shared body grammar: the document
// body, table cells, notes, comments, headers/footers).
func (dw *docWriter) writeBlocks(sb *strings.Builder, blocks []Block) error {
	for i := range blocks {
		switch {
		case blocks[i].Paragraph != nil:
			if err := dw.writeParagraph(sb, blocks[i].Paragraph); err != nil {
				return err
			}
		case blocks[i].Table != nil:
			if err := dw.writeTable(sb, blocks[i].Table); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeParagraph renders a w:p: properties then inline children.
func (dw *docWriter) writeParagraph(sb *strings.Builder, p *Paragraph) error {
	sb.WriteString("<w:p>")
	dw.writePPr(sb, p.Props)
	if err := dw.writeParaChildren(sb, p.Content, false); err != nil {
		return err
	}
	sb.WriteString("</w:p>")
	return nil
}

// writeParaChildren renders inline content; inDelete selects w:delText for run
// text inside a w:del revision.
func (dw *docWriter) writeParaChildren(sb *strings.Builder, children []ParaChild, inDelete bool) error {
	for i := range children {
		ch := children[i]
		switch {
		case ch.Run != nil:
			if err := dw.writeRun(sb, ch.Run, inDelete); err != nil {
				return err
			}
		case ch.Hyperlink != nil:
			if err := dw.writeHyperlink(sb, ch.Hyperlink, inDelete); err != nil {
				return err
			}
		case ch.Drawing != nil:
			if err := dw.writeDrawingRun(sb, ch.Drawing); err != nil {
				return err
			}
		case ch.Revision != nil:
			if err := dw.writeRevision(sb, ch.Revision, inDelete); err != nil {
				return err
			}
		case ch.CommentStart != nil:
			fmt.Fprintf(sb, `<w:commentRangeStart w:id="%d"/>`, ch.CommentStart.ID)
		case ch.CommentEnd != nil:
			fmt.Fprintf(sb, `<w:commentRangeEnd w:id="%d"/>`, ch.CommentEnd.ID)
		}
	}
	return nil
}

// writeRevision renders a w:ins / w:del wrapper with its recursive content.
func (dw *docWriter) writeRevision(sb *strings.Builder, rev *Revision, inDelete bool) error {
	var el string
	switch rev.Kind {
	case RevisionInsert:
		el = "ins"
	case RevisionDelete:
		el = "del"
		inDelete = true
	default:
		return fmt.Errorf("docx: %w: unknown revision kind %d", ErrInvalidDocument, rev.Kind)
	}
	sb.WriteString("<w:" + el)
	writeRevAttrs(sb, RevisionMark{ID: rev.ID, Author: rev.Author, Date: rev.Date})
	sb.WriteString(">")
	if err := dw.writeParaChildren(sb, rev.Content, inDelete); err != nil {
		return err
	}
	sb.WriteString("</w:" + el + ">")
	return nil
}

// writeRevAttrs emits the shared w:id/w:author/w:date revision attributes.
func writeRevAttrs(sb *strings.Builder, m RevisionMark) {
	fmt.Fprintf(sb, ` w:id="%d"`, m.ID)
	if m.Author != "" {
		sb.WriteString(` w:author="` + escXMLAttr.Replace(m.Author) + `"`)
	}
	if m.Date != "" {
		sb.WriteString(` w:date="` + escXMLAttr.Replace(m.Date) + `"`)
	}
}

// writeHyperlink renders a w:hyperlink group.
func (dw *docWriter) writeHyperlink(sb *strings.Builder, h *Hyperlink, inDelete bool) error {
	relID, err := dw.hyperlinkRelID(h)
	if err != nil {
		return err
	}
	sb.WriteString("<w:hyperlink")
	if relID != "" {
		sb.WriteString(` r:id="` + escXMLAttr.Replace(relID) + `"`)
	}
	if h.Anchor != "" {
		sb.WriteString(` w:anchor="` + escXMLAttr.Replace(h.Anchor) + `"`)
	}
	sb.WriteString(">")
	for i := range h.Runs {
		if err := dw.writeRun(sb, &h.Runs[i], inDelete); err != nil {
			return err
		}
	}
	sb.WriteString("</w:hyperlink>")
	return nil
}

// writeRun renders a w:r. A run with no content (text, break, reference, or note
// separator) is omitted — the parser drops such runs, so emitting one would break
// the round-trip fixed point.
func (dw *docWriter) writeRun(sb *strings.Builder, r *Run, inDelete bool) error {
	if r.Text == "" && r.Break == BreakNone && r.FootnoteRef == 0 && r.EndnoteRef == 0 && !r.hasCommentRef() && r.NoteSep == NoteSepNone {
		return nil
	}
	sb.WriteString("<w:r>")
	writeRPr(sb, r.Props)
	switch r.NoteSep {
	case NoteSepSeparator:
		sb.WriteString("<w:separator/>")
	case NoteSepContinuation:
		sb.WriteString("<w:continuationSeparator/>")
	}
	if r.Text != "" {
		writeRunText(sb, r.Text, inDelete)
	}
	switch r.Break {
	case BreakLine:
		sb.WriteString("<w:br/>")
	case BreakPage:
		sb.WriteString(`<w:br w:type="page"/>`)
	case BreakColumn:
		sb.WriteString(`<w:br w:type="column"/>`)
	}
	if r.FootnoteRef != 0 {
		fmt.Fprintf(sb, `<w:footnoteReference w:id="%d"/>`, r.FootnoteRef)
	}
	if r.EndnoteRef != 0 {
		fmt.Fprintf(sb, `<w:endnoteReference w:id="%d"/>`, r.EndnoteRef)
	}
	if r.hasCommentRef() {
		fmt.Fprintf(sb, `<w:commentReference w:id="%d"/>`, r.CommentRef)
	}
	sb.WriteString("</w:r>")
	return nil
}

// hasCommentRef reports whether the run references a comment. HasCommentRef is
// the authoritative signal (comment ids number from 0); a bare non-zero
// CommentRef is honored so a hand-constructed document need not set the flag.
func (r *Run) hasCommentRef() bool { return r.HasCommentRef || r.CommentRef != 0 }

// writeRunText renders run text, mapping tab characters back to <w:tab/> (the
// parser folds w:tab into "\t") and preserving significant whitespace.
func writeRunText(sb *strings.Builder, text string, inDelete bool) {
	el := "w:t"
	if inDelete {
		el = "w:delText"
	}
	for i, seg := range strings.Split(text, "\t") {
		if i > 0 {
			sb.WriteString("<w:tab/>")
		}
		if seg == "" {
			continue
		}
		sb.WriteString("<" + el)
		if strings.TrimSpace(seg) != seg {
			sb.WriteString(` xml:space="preserve"`)
		}
		sb.WriteString(">")
		sb.WriteString(escXMLText.Replace(seg))
		sb.WriteString("</" + el + ">")
	}
}

// writeRPr renders run properties in schema order. withChange controls the
// nested rPrChange (the before-state inside one is CT_RPrOriginal — no nested
// change of its own).
func writeRPr(sb *strings.Builder, p RunProps) { writeRPrDepth(sb, p, true) }

func writeRPrDepth(sb *strings.Builder, p RunProps, withChange bool) {
	var b strings.Builder
	if p.StyleID != "" {
		b.WriteString(`<w:rStyle w:val="` + escXMLAttr.Replace(p.StyleID) + `"/>`)
	}
	if p.Family != "" {
		f := escXMLAttr.Replace(p.Family)
		b.WriteString(`<w:rFonts w:ascii="` + f + `" w:hAnsi="` + f + `"/>`)
	}
	writeToggle(&b, "w:b", p.Bold, p.HasBold)
	writeToggle(&b, "w:i", p.Italic, p.HasItalic)
	writeToggle(&b, "w:caps", p.Caps, p.HasCaps)
	writeToggle(&b, "w:smallCaps", p.SmallCaps, p.HasSmallCaps)
	writeToggle(&b, "w:strike", p.Strike, p.HasStrike)
	if p.HasColor {
		b.WriteString(`<w:color w:val="` + rgbHex(p.Color) + `"/>`)
	}
	if p.HasSize {
		b.WriteString(`<w:sz w:val="` + strconv.Itoa(p.SizeHalfPts) + `"/>`)
	}
	if p.HasHighlight {
		// Prefer the parsed name (round-trips the exact token, e.g. "darkGray"), but
		// only if it is a valid ST_HighlightColor — a consumer-supplied name outside
		// the palette (e.g. a hex color) would produce an invalid w:highlight, so fall
		// back to remapping the resolved RGBA to a palette name for hand-built docs.
		if isHighlightName(p.HighlightName) {
			b.WriteString(`<w:highlight w:val="` + escXMLAttr.Replace(p.HighlightName) + `"/>`)
		} else if name, ok := highlightName(p.Highlight); ok {
			b.WriteString(`<w:highlight w:val="` + name + `"/>`)
		}
	}
	if p.HasUnderline {
		if !p.Underline {
			b.WriteString(`<w:u w:val="none"/>`)
		} else {
			style := p.UnderlineStyle
			if style == "" {
				style = "single"
			}
			b.WriteString(`<w:u w:val="` + escXMLAttr.Replace(style) + `"`)
			if p.HasUnderlineColor {
				b.WriteString(` w:color="` + rgbHex(p.UnderlineColor) + `"`)
			}
			b.WriteString("/>")
		}
	}
	if p.Shd.HasFill {
		b.WriteString(`<w:shd w:val="clear" w:fill="` + rgbHex(p.Shd.Fill) + `"/>`)
	}
	switch p.VertAlign {
	case VertAlignSuperscript:
		b.WriteString(`<w:vertAlign w:val="superscript"/>`)
	case VertAlignSubscript:
		b.WriteString(`<w:vertAlign w:val="subscript"/>`)
	}
	if withChange && p.RPrChange != nil {
		b.WriteString("<w:rPrChange")
		writeRevAttrs(&b, p.RPrChange.Mark)
		b.WriteString(">")
		pre := b.Len()
		writeRPrDepth(&b, p.RPrChange.Previous, false) // self-wraps in <w:rPr> when non-empty
		if b.Len() == pre {
			b.WriteString("<w:rPr/>") // the schema requires the child even for an empty before-state
		}
		b.WriteString("</w:rPrChange>")
	}
	if b.Len() == 0 {
		return
	}
	sb.WriteString("<w:rPr>")
	sb.WriteString(b.String())
	sb.WriteString("</w:rPr>")
}

// writeToggle emits an OOXML boolean toggle: present = on, val="0" = explicit
// off, absent = unspecified.
func writeToggle(sb *strings.Builder, el string, on, has bool) {
	if !has {
		return
	}
	if on {
		sb.WriteString("<" + el + "/>")
	} else {
		sb.WriteString("<" + el + ` w:val="0"/>`)
	}
}

// writePPr renders paragraph properties in schema order.
func (dw *docWriter) writePPr(sb *strings.Builder, p ParagraphProps) {
	dw.writePPrDepth(sb, p, true)
}

func (dw *docWriter) writePPrDepth(sb *strings.Builder, p ParagraphProps, withChange bool) {
	var b strings.Builder
	if p.StyleID != "" {
		b.WriteString(`<w:pStyle w:val="` + escXMLAttr.Replace(p.StyleID) + `"/>`)
	}
	if p.PageBreakBefore {
		b.WriteString(`<w:pageBreakBefore/>`)
	}
	if p.Frame != nil {
		writeFramePr(&b, p.Frame)
	}
	if p.HasNum {
		fmt.Fprintf(&b, `<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, p.ILvl, p.NumID)
	} else if p.ILvl != 0 {
		// A bare level with no instance — Word's own style part does this (its
		// Subtitle style carries ilvl only); preserved without inventing a numId.
		fmt.Fprintf(&b, `<w:numPr><w:ilvl w:val="%d"/></w:numPr>`, p.ILvl)
	}
	if p.Borders != nil {
		writeBorders(&b, "pBdr", *p.Borders)
	}
	if len(p.TabStops) > 0 {
		b.WriteString("<w:tabs>")
		for _, ts := range p.TabStops {
			align := ts.Align
			if align == "" {
				align = "left"
			}
			fmt.Fprintf(&b, `<w:tab w:val="%s" w:pos="%d"/>`, escXMLAttr.Replace(align), int(ts.PosTwips))
		}
		b.WriteString("</w:tabs>")
	}
	if p.HasSpacingBefore || p.HasSpacingAfter || p.HasLine {
		b.WriteString("<w:spacing")
		if p.HasSpacingBefore {
			fmt.Fprintf(&b, ` w:before="%d"`, int(p.SpacingBefore))
		}
		if p.HasSpacingAfter {
			fmt.Fprintf(&b, ` w:after="%d"`, int(p.SpacingAfter))
		}
		if p.HasLine {
			fmt.Fprintf(&b, ` w:line="%d"`, int(p.Line))
			switch p.LineRule {
			case LineRuleExact:
				b.WriteString(` w:lineRule="exact"`)
			case LineRuleAtLeast:
				b.WriteString(` w:lineRule="atLeast"`)
			default:
				b.WriteString(` w:lineRule="auto"`)
			}
		}
		b.WriteString("/>")
	}
	if p.HasIndentLeft || p.HasIndentRight || p.HasFirstLine {
		b.WriteString("<w:ind")
		if p.HasIndentLeft {
			fmt.Fprintf(&b, ` w:left="%d"`, int(p.IndentLeft))
		}
		if p.HasIndentRight {
			fmt.Fprintf(&b, ` w:right="%d"`, int(p.IndentRight))
		}
		if p.HasFirstLine {
			if p.FirstLine < 0 {
				fmt.Fprintf(&b, ` w:hanging="%d"`, -int(p.FirstLine))
			} else {
				fmt.Fprintf(&b, ` w:firstLine="%d"`, int(p.FirstLine))
			}
		}
		b.WriteString("/>")
	}
	if p.HasJustify {
		b.WriteString(`<w:jc w:val="` + justifyVal(p.Justify) + `"/>`)
	}
	if p.SectPr != nil {
		dw.writeSectPr(&b, *p.SectPr)
	}
	if withChange && p.PPrChange != nil {
		b.WriteString("<w:pPrChange")
		writeRevAttrs(&b, p.PPrChange.Mark)
		b.WriteString("><w:pPr>")
		dw.writePPrDepth(&b, p.PPrChange.Previous, false)
		b.WriteString("</w:pPr></w:pPrChange>")
	}
	if b.Len() == 0 {
		return
	}
	// The nested before-state emits its content bare (the caller wraps it in
	// w:pPr); the normal path wraps here.
	if withChange {
		sb.WriteString("<w:pPr>")
		sb.WriteString(b.String())
		sb.WriteString("</w:pPr>")
	} else {
		sb.WriteString(b.String())
	}
}

// writeFramePr emits a w:framePr with its declared attributes.
func writeFramePr(sb *strings.Builder, f *FramePr) {
	sb.WriteString("<w:framePr")
	if f.DropCap != "" {
		sb.WriteString(` w:dropCap="` + escXMLAttr.Replace(f.DropCap) + `"`)
	}
	if f.Lines != 0 {
		fmt.Fprintf(sb, ` w:lines="%d"`, f.Lines)
	}
	if f.W != 0 {
		fmt.Fprintf(sb, ` w:w="%d"`, int(f.W))
	}
	if f.H != 0 {
		fmt.Fprintf(sb, ` w:h="%d"`, int(f.H))
	}
	if f.HSpace != 0 {
		fmt.Fprintf(sb, ` w:hSpace="%d"`, int(f.HSpace))
	}
	if f.Wrap != "" {
		sb.WriteString(` w:wrap="` + escXMLAttr.Replace(f.Wrap) + `"`)
	}
	if f.HAnchor != "" {
		sb.WriteString(` w:hAnchor="` + escXMLAttr.Replace(f.HAnchor) + `"`)
	}
	if f.VAnchor != "" {
		sb.WriteString(` w:vAnchor="` + escXMLAttr.Replace(f.VAnchor) + `"`)
	}
	sb.WriteString("/>")
}

// justifyVal maps a Justify to its w:jc value.
func justifyVal(j Justify) string {
	switch j {
	case JustifyCenter:
		return "center"
	case JustifyRight:
		return "right"
	case JustifyBoth:
		return "both"
	default:
		return "left"
	}
}

// rgbHex renders a color as RRGGBB.
func rgbHex(c interface{ RGBA() (r, g, b, a uint32) }) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%02X%02X%02X", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}

// highlightName maps an RGBA back to its WordprocessingML highlight name —
// the highlight palette is the fixed 16-name set, so only exact matches emit
// (the parser can only ever produce these values).
func highlightName(c interface{ RGBA() (r, g, b, a uint32) }) (string, bool) {
	hex := rgbHex(c)
	for _, n := range []string{"yellow", "green", "cyan", "magenta", "blue", "red",
		"darkBlue", "darkCyan", "darkGreen", "darkMagenta", "darkRed", "darkYellow",
		"darkGray", "lightGray", "black", "white"} {
		if rgba, ok := highlightColor(n); ok && rgbHex(rgba) == hex {
			return n, true
		}
	}
	return "", false
}

// writeSectPr emits section properties: header/footer references, page size,
// and margins. A zero SectionProps means "unspecified" and emits Word's Letter
// defaults (the same defaults the parser substitutes), so a hand-built
// document without explicit geometry produces a valid page.
func (dw *docWriter) writeSectPr(sb *strings.Builder, s SectionProps) {
	if s == (SectionProps{}) {
		s = defaultSection()
	}
	sb.WriteString("<w:sectPr>")
	if s.HeaderRefDefault != "" {
		sb.WriteString(`<w:headerReference w:type="default" r:id="` + escXMLAttr.Replace(s.HeaderRefDefault) + `"/>`)
	}
	if s.FooterRefDefault != "" {
		sb.WriteString(`<w:footerReference w:type="default" r:id="` + escXMLAttr.Replace(s.FooterRefDefault) + `"/>`)
	}
	fmt.Fprintf(sb, `<w:pgSz w:w="%d" w:h="%d"/>`, int(s.PageW), int(s.PageH))
	fmt.Fprintf(sb, `<w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="%d" w:footer="%d"`,
		int(s.MarginTop), int(s.MarginRight), int(s.MarginBottom), int(s.MarginLeft), int(s.Header), int(s.Footer))
	if s.Gutter != 0 {
		fmt.Fprintf(sb, ` w:gutter="%d"`, int(s.Gutter))
	}
	sb.WriteString("/></w:sectPr>")
}

// writeDrawingRun renders a drawing inside its own run — inline (wp:inline) or
// anchored (wp:anchor with position/wrap). The scaffold (docPr/nvPicPr/spPr)
// is what real consumers require; this repo's reader needs only extent + blip.
func (dw *docWriter) writeDrawingRun(sb *strings.Builder, dr *Drawing) error {
	if dr.RelID == "" {
		return fmt.Errorf("docx: %w: drawing with no relationship id", ErrInvalidDocument)
	}
	rel, ok := dw.rels[dr.RelID]
	if !ok {
		return fmt.Errorf("docx: %w: drawing references unknown relationship %q", ErrInvalidDocument, dr.RelID)
	}
	if _, ok := dw.doc.Media[rel.Target]; !ok {
		return fmt.Errorf("docx: %w: drawing relationship %q targets missing media part %q", ErrInvalidDocument, dr.RelID, rel.Target)
	}
	dw.drawings++
	id := dw.drawings
	name := escXMLAttr.Replace(fmt.Sprintf("Picture %d", id))
	desc := escXMLAttr.Replace(dr.Description)
	titleAttr := ""
	if dr.Title != "" {
		titleAttr = fmt.Sprintf(` title="%s"`, escXMLAttr.Replace(dr.Title))
	}

	sb.WriteString("<w:r><w:drawing>")
	if dr.Anchored {
		sb.WriteString(`<wp:anchor distT="0" distB="0" distL="114300" distR="114300" simplePos="0" relativeHeight="0" behindDoc="0" locked="0" layoutInCell="1" allowOverlap="1" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">`)
		sb.WriteString(`<wp:simplePos x="0" y="0"/>`)
		align := dr.AlignH
		if align == "" {
			align = "left"
		}
		sb.WriteString(`<wp:positionH relativeFrom="margin"><wp:align>` + escXMLText.Replace(align) + `</wp:align></wp:positionH>`)
		sb.WriteString(`<wp:positionV relativeFrom="paragraph"><wp:posOffset>0</wp:posOffset></wp:positionV>`)
		fmt.Fprintf(sb, `<wp:extent cx="%d" cy="%d"/>`, dr.WidthEMU, dr.HeightEMU)
		switch dr.WrapKind {
		case "topAndBottom":
			sb.WriteString(`<wp:wrapTopAndBottom/>`)
		case "tight":
			sb.WriteString(`<wp:wrapTight wrapText="bothSides"/>`)
		case "through":
			sb.WriteString(`<wp:wrapThrough wrapText="bothSides"/>`)
		case "none":
			sb.WriteString(`<wp:wrapNone/>`)
		default: // "square" (and unset degrades to square — the common wrap)
			sb.WriteString(`<wp:wrapSquare wrapText="bothSides"/>`)
		}
		fmt.Fprintf(sb, `<wp:docPr id="%d" name="%s" descr="%s"%s/>`, id, name, desc, titleAttr)
	} else {
		sb.WriteString(`<wp:inline distT="0" distB="0" distL="0" distR="0" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">`)
		fmt.Fprintf(sb, `<wp:extent cx="%d" cy="%d"/>`, dr.WidthEMU, dr.HeightEMU)
		fmt.Fprintf(sb, `<wp:docPr id="%d" name="%s" descr="%s"%s/>`, id, name, desc, titleAttr)
	}
	relID := escXMLAttr.Replace(dr.RelID)
	fmt.Fprintf(sb, `<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">`+
		`<a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:nvPicPr><pic:cNvPr id="%d" name="%s"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm>`+
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`+
		`</pic:pic></a:graphicData></a:graphic>`,
		id, name, relID, dr.WidthEMU, dr.HeightEMU)
	if dr.Anchored {
		sb.WriteString("</wp:anchor>")
	} else {
		sb.WriteString("</wp:inline>")
	}
	sb.WriteString("</w:drawing></w:r>")
	return nil
}

// writeTable renders a w:tbl: properties, grid, rows.
func (dw *docWriter) writeTable(sb *strings.Builder, t *Table) error {
	sb.WriteString("<w:tbl>")
	dw.writeTblPr(sb, t.Props)
	if len(t.Grid) > 0 {
		sb.WriteString("<w:tblGrid>")
		for _, w := range t.Grid {
			fmt.Fprintf(sb, `<w:gridCol w:w="%d"/>`, int(w))
		}
		sb.WriteString("</w:tblGrid>")
	}
	for i := range t.Rows {
		if err := dw.writeRow(sb, &t.Rows[i]); err != nil {
			return err
		}
	}
	sb.WriteString("</w:tbl>")
	return nil
}

// writeTblPr renders table-level properties in schema order.
func (dw *docWriter) writeTblPr(sb *strings.Builder, p TableProps) {
	var b strings.Builder
	switch {
	case p.WidthDxa != 0:
		fmt.Fprintf(&b, `<w:tblW w:w="%d" w:type="dxa"/>`, int(p.WidthDxa))
	case p.WidthPct != 0:
		fmt.Fprintf(&b, `<w:tblW w:w="%d" w:type="pct"/>`, p.WidthPct)
	}
	if p.Justify != JustifyLeft {
		b.WriteString(`<w:jc w:val="` + justifyVal(p.Justify) + `"/>`)
	}
	writeBorders(&b, "tblBorders", p.Borders)
	writeShd(&b, p.Shading)
	if p.LayoutFixed {
		b.WriteString(`<w:tblLayout w:type="fixed"/>`)
	}
	if b.Len() == 0 {
		return
	}
	sb.WriteString("<w:tblPr>")
	sb.WriteString(b.String())
	sb.WriteString("</w:tblPr>")
}

// writeRow renders a w:tr.
func (dw *docWriter) writeRow(sb *strings.Builder, row *TableRow) error {
	sb.WriteString("<w:tr>")
	if row.Props.IsHeader || row.Props.HeightDxa != 0 {
		sb.WriteString("<w:trPr>")
		if row.Props.IsHeader {
			sb.WriteString("<w:tblHeader/>")
		}
		if row.Props.HeightDxa != 0 {
			fmt.Fprintf(sb, `<w:trHeight w:val="%d"/>`, int(row.Props.HeightDxa))
		}
		sb.WriteString("</w:trPr>")
	}
	for i := range row.Cells {
		if err := dw.writeCell(sb, &row.Cells[i]); err != nil {
			return err
		}
	}
	sb.WriteString("</w:tr>")
	return nil
}

// writeCell renders a w:tc with its properties and block content. A cell with
// no blocks gets an empty paragraph (OOXML requires at least one block).
func (dw *docWriter) writeCell(sb *strings.Builder, cell *TableCell) error {
	sb.WriteString("<w:tc>")
	dw.writeTcPr(sb, cell, true)
	if len(cell.Blocks) == 0 {
		sb.WriteString("<w:p/>")
	} else if err := dw.writeBlocks(sb, cell.Blocks); err != nil {
		return err
	}
	sb.WriteString("</w:tc>")
	return nil
}

// writeTcPr renders cell properties in schema order.
func (dw *docWriter) writeTcPr(sb *strings.Builder, cell *TableCell, withChange bool) {
	var b strings.Builder
	if cell.Props.WidthDxa != 0 {
		fmt.Fprintf(&b, `<w:tcW w:w="%d" w:type="dxa"/>`, int(cell.Props.WidthDxa))
	}
	if cell.GridSpan > 1 {
		fmt.Fprintf(&b, `<w:gridSpan w:val="%d"/>`, cell.GridSpan)
	}
	switch cell.VMerge {
	case VMergeRestart:
		b.WriteString(`<w:vMerge w:val="restart"/>`)
	case VMergeContinue:
		b.WriteString(`<w:vMerge w:val="continue"/>`)
	}
	writeBorders(&b, "tcBorders", cell.Props.Borders)
	writeShd(&b, cell.Props.Shading)
	switch cell.Props.VAlign {
	case VAlignCenter:
		b.WriteString(`<w:vAlign w:val="center"/>`)
	case VAlignBottom:
		b.WriteString(`<w:vAlign w:val="bottom"/>`)
	}
	if cell.Ins != nil {
		b.WriteString("<w:cellIns")
		writeRevAttrs(&b, *cell.Ins)
		b.WriteString("/>")
	}
	if cell.Del != nil {
		b.WriteString("<w:cellDel")
		writeRevAttrs(&b, *cell.Del)
		b.WriteString("/>")
	}
	if withChange && cell.Props.TcPrChange != nil {
		b.WriteString("<w:tcPrChange")
		writeRevAttrs(&b, cell.Props.TcPrChange.Mark)
		b.WriteString("><w:tcPr>")
		prev := TableCell{GridSpan: 1, Props: cell.Props.TcPrChange.Previous}
		dw.writeTcPrInner(&b, &prev)
		b.WriteString("</w:tcPr></w:tcPrChange>")
	}
	if b.Len() == 0 {
		return
	}
	sb.WriteString("<w:tcPr>")
	sb.WriteString(b.String())
	sb.WriteString("</w:tcPr>")
}

// writeTcPrInner emits a tcPr's children bare (for the tcPrChange before
// state, whose wrapper the caller writes).
func (dw *docWriter) writeTcPrInner(sb *strings.Builder, cell *TableCell) {
	var b strings.Builder
	if cell.Props.WidthDxa != 0 {
		fmt.Fprintf(&b, `<w:tcW w:w="%d" w:type="dxa"/>`, int(cell.Props.WidthDxa))
	}
	writeBorders(&b, "tcBorders", cell.Props.Borders)
	writeShd(&b, cell.Props.Shading)
	switch cell.Props.VAlign {
	case VAlignCenter:
		b.WriteString(`<w:vAlign w:val="center"/>`)
	case VAlignBottom:
		b.WriteString(`<w:vAlign w:val="bottom"/>`)
	}
	sb.WriteString(b.String())
}

// writeBorders emits a four-edge border set; edges that are entirely unset are
// omitted, so an empty set emits nothing.
func writeBorders(sb *strings.Builder, wrapper string, b BoxBorders) {
	var inner strings.Builder
	writeBorderEdge(&inner, "top", b.Top)
	writeBorderEdge(&inner, "left", b.Left)
	writeBorderEdge(&inner, "bottom", b.Bottom)
	writeBorderEdge(&inner, "right", b.Right)
	if inner.Len() == 0 {
		return
	}
	sb.WriteString("<w:" + wrapper + ">")
	sb.WriteString(inner.String())
	sb.WriteString("</w:" + wrapper + ">")
}

// writeBorderEdge emits one border edge; a zero-value Border means unset and
// emits nothing. A None edge emits val="nil"; a set edge emits its style name
// (defaulting to single), size, and color.
func writeBorderEdge(sb *strings.Builder, side string, bd Border) {
	if !bd.None && bd.Style == "" && bd.SizeEighthPt == 0 && !bd.HasColor {
		return
	}
	if bd.None {
		sb.WriteString(`<w:` + side + ` w:val="nil"/>`)
		return
	}
	style := bd.Style
	if style == "" {
		style = "single"
	}
	sb.WriteString(`<w:` + side + ` w:val="` + escXMLAttr.Replace(style) + `"`)
	if bd.SizeEighthPt != 0 {
		fmt.Fprintf(sb, ` w:sz="%d"`, bd.SizeEighthPt)
	}
	if bd.HasColor {
		sb.WriteString(` w:color="` + rgbHex(bd.Color) + `"`)
	}
	sb.WriteString("/>")
}

// writeShd emits a shading fill when set.
func writeShd(sb *strings.Builder, s Shading) {
	if !s.HasFill {
		return
	}
	sb.WriteString(`<w:shd w:val="clear" w:fill="` + rgbHex(s.Fill) + `"/>`)
}
