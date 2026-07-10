package docxwrite

import (
	"fmt"
	"strings"
)

// stylesXML is the word/styles.xml part: document defaults plus the paragraph
// styles the writer references. The style ids are the round-trip contract with
// the reader's cssbox lowering — HeadingN carries the heading level, Quote the
// blockquote semantic, CodeBlock the pre semantic, HorizontalRule the hr
// semantic — and each style's run properties carry the visual identity (the
// reader has no separate character-style cascade, so visuals ride on the
// paragraph style's rPr or on direct run properties).
func stylesXML() string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri"/><w:sz w:val="22"/></w:rPr></w:rPrDefault>
<w:pPrDefault><w:pPr><w:spacing w:after="160"/></w:pPr></w:pPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
`)
	// Heading sizes mirror the UA heading scale (32/24/19/16/13/11 pt) in
	// half-points so a reopened document renders with the familiar hierarchy.
	for i, sz := range []int{64, 48, 38, 32, 26, 22} {
		fmt.Fprintf(&sb, `<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:before="240" w:after="120"/><w:outlineLvl w:val="%d"/></w:pPr><w:rPr><w:b/><w:sz w:val="%d"/></w:rPr></w:style>`+"\n",
			i+1, i+1, i, sz)
	}
	sb.WriteString(`<w:style w:type="paragraph" w:styleId="Caption"><w:name w:val="caption"/><w:basedOn w:val="Normal"/><w:rPr><w:b/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:pPr><w:ind w:left="720"/></w:pPr><w:rPr><w:color w:val="595959"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:pPr><w:ind w:left="720"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="CodeBlock"><w:name w:val="Code Block"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="0"/></w:pPr><w:rPr><w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/><w:sz w:val="20"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="HorizontalRule"><w:name w:val="Horizontal Rule"/><w:basedOn w:val="Normal"/><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="D8DEE4"/></w:pBdr></w:pPr></w:style>
<w:style w:type="character" w:styleId="CodeChar"><w:name w:val="Code Char"/><w:rPr><w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/></w:rPr></w:style>
<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>
</w:styles>
`)
	return sb.String()
}
