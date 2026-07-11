package docxwrite

import (
	"fmt"
	"strings"
)

// numberingXML is the word/numbering.xml part: one bullet abstract definition
// (glyph rotating by depth) shared by every bullet list, one decimal abstract
// definition, and one w:num instance per ordered list encountered — numbering
// restarts per instance both in the reader (per-numId counters) and in Word
// (startOverride, which the reader ignores but Word requires).
func numberingXML(orderedLists int) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
`)
	// Abstract 0: bullets. The glyph rotates •/◦/▪ by depth, matching the UA
	// list rendering.
	sb.WriteString(`<w:abstractNum w:abstractNumId="0">`)
	bullets := []string{"•", "◦", "▪"}
	for lvl := 0; lvl < 9; lvl++ {
		fmt.Fprintf(&sb, `<w:lvl w:ilvl="%d"><w:numFmt w:val="bullet"/><w:lvlText w:val="%s"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr></w:lvl>`,
			lvl, bullets[lvl%len(bullets)], 720*(lvl+1))
	}
	sb.WriteString("</w:abstractNum>\n")
	// Abstract 1: decimal. The %N placeholder is per-level, as the reader's
	// single-placeholder substitution expects.
	sb.WriteString(`<w:abstractNum w:abstractNumId="1">`)
	for lvl := 0; lvl < 9; lvl++ {
		fmt.Fprintf(&sb, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%%%d."/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr></w:lvl>`,
			lvl, lvl+1, 720*(lvl+1))
	}
	sb.WriteString("</w:abstractNum>\n")

	// Instance 1 = bullets; instances 2..N+1 = one per ordered list.
	fmt.Fprintf(&sb, `<w:num w:numId="%d"><w:abstractNumId w:val="0"/></w:num>`+"\n", bulletNumID)
	for i := 1; i <= orderedLists; i++ {
		fmt.Fprintf(&sb, `<w:num w:numId="%d"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`+"\n", bulletNumID+i)
	}
	sb.WriteString("</w:numbering>\n")
	return sb.String()
}
