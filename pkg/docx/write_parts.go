package docx

import (
	"fmt"
	"image/color"
	"strings"
)

// wPartOpen builds a part root open tag with the w: and r: namespaces (note
// and header bodies can carry hyperlinks and drawings).
func wPartOpen(root string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<w:` + root + ` xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`
}

// stylesXML renders word/styles.xml from the model's style table.
func (dw *docWriter) stylesXML() string {
	st := dw.doc.Styles
	var sb strings.Builder
	sb.WriteString(wPartOpen("styles"))
	sb.WriteString("<w:docDefaults><w:rPrDefault>")
	writeRPrDepth(&sb, st.DocDefaultRun, false)
	sb.WriteString("</w:rPrDefault><w:pPrDefault>")
	dw.writePPrDepth(&sb, st.DocDefaultPara, true)
	sb.WriteString("</w:pPrDefault></w:docDefaults>")
	for _, id := range sortedKeys(st.ByID) {
		s := st.ByID[id]
		typ := s.Type
		if typ == "" {
			typ = "paragraph"
		}
		sb.WriteString(`<w:style w:type="` + escXMLAttr.Replace(typ) + `"`)
		if s.ID == st.DefaultParaID && typ == "paragraph" {
			sb.WriteString(` w:default="1"`)
		}
		sb.WriteString(` w:styleId="` + escXMLAttr.Replace(s.ID) + `">`)
		if s.Name != "" {
			sb.WriteString(`<w:name w:val="` + escXMLAttr.Replace(s.Name) + `"/>`)
		}
		if s.BasedOn != "" {
			sb.WriteString(`<w:basedOn w:val="` + escXMLAttr.Replace(s.BasedOn) + `"/>`)
		}
		dw.writePPrDepth(&sb, s.Para, true)
		writeRPrDepth(&sb, s.Run, true)
		sb.WriteString("</w:style>")
	}
	sb.WriteString("</w:styles>\n")
	return sb.String()
}

// numberingXML renders word/numbering.xml: abstract definitions then instances.
func (dw *docWriter) numberingXML() string {
	num := dw.doc.Numbering
	var sb strings.Builder
	sb.WriteString(wPartOpen("numbering"))
	for _, absID := range sortedIntKeys(num.Abstract) {
		fmt.Fprintf(&sb, `<w:abstractNum w:abstractNumId="%d">`, absID)
		levels := num.Abstract[absID]
		for _, ilvl := range sortedIntKeys(levels) {
			lvl := levels[ilvl]
			fmt.Fprintf(&sb, `<w:lvl w:ilvl="%d">`, ilvl)
			if lvl.HasStart {
				fmt.Fprintf(&sb, `<w:start w:val="%d"/>`, lvl.Start)
			}
			sb.WriteString(`<w:numFmt w:val="` + numFmtVal(lvl.Format) + `"/>`)
			if lvl.Text != "" {
				sb.WriteString(`<w:lvlText w:val="` + escXMLAttr.Replace(lvl.Text) + `"/>`)
			}
			sb.WriteString("</w:lvl>")
		}
		sb.WriteString("</w:abstractNum>")
	}
	for _, numID := range sortedIntKeys(num.Instances) {
		inst := num.Instances[numID]
		fmt.Fprintf(&sb, `<w:num w:numId="%d"><w:abstractNumId w:val="%d"/>`, numID, inst.AbstractID)
		for _, ilvl := range sortedIntKeys(inst.Overrides) {
			ov := inst.Overrides[ilvl]
			fmt.Fprintf(&sb, `<w:lvlOverride w:ilvl="%d">`, ilvl)
			if ov.HasStart {
				fmt.Fprintf(&sb, `<w:startOverride w:val="%d"/>`, ov.Start)
			}
			sb.WriteString("</w:lvlOverride>")
		}
		sb.WriteString("</w:num>")
	}
	sb.WriteString("</w:numbering>\n")
	return sb.String()
}

// numFmtVal maps a NumFmt back to its w:numFmt value.
func numFmtVal(f NumFmt) string {
	switch f {
	case NumFmtBullet:
		return "bullet"
	case NumFmtLowerRoman:
		return "lowerRoman"
	case NumFmtUpperRoman:
		return "upperRoman"
	case NumFmtLowerLetter:
		return "lowerLetter"
	case NumFmtUpperLetter:
		return "upperLetter"
	case NumFmtNone:
		return "none"
	default:
		return "decimal"
	}
}

// notesXML renders a footnotes/endnotes part; root is "footnotes"/"endnotes",
// element "footnote"/"endnote".
func (dw *docWriter) notesXML(n *Notes, root, element string) ([]byte, error) {
	var sb strings.Builder
	sb.WriteString(wPartOpen(root))
	for _, id := range sortedIntKeys(n.ByID) {
		fmt.Fprintf(&sb, `<w:%s w:id="%d">`, element, id)
		if err := dw.writeBlocks(&sb, n.ByID[id]); err != nil {
			return nil, err
		}
		sb.WriteString("</w:" + element + ">")
	}
	sb.WriteString("</w:" + root + ">\n")
	return []byte(sb.String()), nil
}

// commentsXML renders word/comments.xml, comments sorted by id.
func (dw *docWriter) commentsXML() ([]byte, error) {
	var sb strings.Builder
	sb.WriteString(wPartOpen("comments"))
	for _, id := range sortedIntKeys(dw.doc.Comments) {
		c := dw.doc.Comments[id]
		fmt.Fprintf(&sb, `<w:comment w:id="%d"`, c.ID)
		if c.Author != "" {
			sb.WriteString(` w:author="` + escXMLAttr.Replace(c.Author) + `"`)
		}
		if c.Initials != "" {
			sb.WriteString(` w:initials="` + escXMLAttr.Replace(c.Initials) + `"`)
		}
		if c.Date != "" {
			sb.WriteString(` w:date="` + escXMLAttr.Replace(c.Date) + `"`)
		}
		sb.WriteString(">")
		if err := dw.writeBlocks(&sb, c.Body); err != nil {
			return nil, err
		}
		sb.WriteString("</w:comment>")
	}
	sb.WriteString("</w:comments>\n")
	return []byte(sb.String()), nil
}

// hdrFtrXML renders a header/footer part (root "hdr" or "ftr").
func (dw *docWriter) hdrFtrXML(hf *HeaderFooter, root string) ([]byte, error) {
	var sb strings.Builder
	sb.WriteString(wPartOpen(root))
	if hf != nil {
		if err := dw.writeBlocks(&sb, hf.Blocks); err != nil {
			return nil, err
		}
	}
	sb.WriteString("</w:" + root + ">\n")
	return []byte(sb.String()), nil
}

// DefaultStyles returns the curated style table a constructed document should
// start from — the same ids the cssbox lowering recovers semantics from
// (HeadingN → heading level, Quote → blockquote, CodeBlock → pre,
// HorizontalRule → hr, CodeChar → inline code) and the visual identities the
// docxwrite writer established. Callers may add or adjust entries before Write.
func DefaultStyles() *Styles {
	rgb := func(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 0xFF} }
	st := &Styles{
		DocDefaultRun:  RunProps{Family: "Calibri", SizeHalfPts: 22, HasSize: true},
		DocDefaultPara: ParagraphProps{SpacingAfter: 160, HasSpacingAfter: true},
		ByID:           map[string]*Style{},
		DefaultParaID:  "Normal",
	}
	add := func(s *Style) { st.ByID[s.ID] = s }
	add(&Style{ID: "Normal", Name: "Normal", Type: "paragraph"})
	// Heading sizes mirror the UA heading scale (32/24/19/16/13/11 pt) in
	// half-points so a reopened document renders with the familiar hierarchy.
	for i, sz := range []int{64, 48, 38, 32, 26, 22} {
		add(&Style{
			ID: fmt.Sprintf("Heading%d", i+1), Name: fmt.Sprintf("heading %d", i+1),
			Type: "paragraph", BasedOn: "Normal",
			Para: ParagraphProps{SpacingBefore: 240, HasSpacingBefore: true, SpacingAfter: 120, HasSpacingAfter: true},
			Run:  RunProps{Bold: true, HasBold: true, SizeHalfPts: sz, HasSize: true},
		})
	}
	add(&Style{ID: "Caption", Name: "caption", Type: "paragraph", BasedOn: "Normal",
		Run: RunProps{Bold: true, HasBold: true}})
	add(&Style{ID: "Quote", Name: "Quote", Type: "paragraph", BasedOn: "Normal",
		Para: ParagraphProps{IndentLeft: 720, HasIndentLeft: true},
		Run:  RunProps{Color: rgb(0x59, 0x59, 0x59), HasColor: true}})
	add(&Style{ID: "ListParagraph", Name: "List Paragraph", Type: "paragraph", BasedOn: "Normal",
		Para: ParagraphProps{IndentLeft: 720, HasIndentLeft: true}})
	add(&Style{ID: "CodeBlock", Name: "Code Block", Type: "paragraph", BasedOn: "Normal",
		Para: ParagraphProps{SpacingAfter: 0, HasSpacingAfter: true},
		Run:  RunProps{Family: "Courier New", SizeHalfPts: 20, HasSize: true}})
	add(&Style{ID: "HorizontalRule", Name: "Horizontal Rule", Type: "paragraph", BasedOn: "Normal"})
	add(&Style{ID: "CodeChar", Name: "Code Char", Type: "character",
		Run: RunProps{Family: "Courier New"}})
	add(&Style{ID: "Hyperlink", Name: "Hyperlink", Type: "character",
		Run: RunProps{Color: rgb(0x05, 0x63, 0xC1), HasColor: true, Underline: true, HasUnderline: true, UnderlineStyle: "single"}})
	return st
}
