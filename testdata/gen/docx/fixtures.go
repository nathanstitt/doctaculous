package docx

import "strings"

// CoreFixture is one canonical DOCX fixture: a stable name, a human description,
// the page count it must lay out to, and a builder for its bytes. It mirrors the
// PDF generator's gen.CoreFixture so the golden test can range over Core
// uniformly.
type CoreFixture struct {
	Name  string
	Desc  string
	Pages int
	Build func() []byte
}

// Bytes returns the fixture's .docx bytes.
func (f CoreFixture) Bytes() []byte { return f.Build() }

// Core is the canonical set of DOCX fixtures exercised by the golden test. Each
// locks down one distinct path through parse → cascade → lower → reflow → raster.
var Core = []CoreFixture{
	{
		Name:  "paragraph",
		Desc:  "a single paragraph of running text that wraps within the margins",
		Pages: 1,
		Build: paragraphDocx,
	},
	{
		Name:  "styled",
		Desc:  "a heading style (basedOn Normal) over body paragraphs",
		Pages: 1,
		Build: styledDocx,
	},
	{
		Name:  "justify",
		Desc:  "a fully-justified paragraph",
		Pages: 1,
		Build: justifyDocx,
	},
	{
		Name:  "multipage",
		Desc:  "enough text to overflow onto a second page",
		Pages: 2,
		Build: multipageDocx,
	},
}

const docOpen = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`

const docClose = `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:bottom="1440" w:left="1440" w:right="1440"/></w:sectPr></w:body></w:document>`

// para wraps text in a paragraph with optional pStyle and jc.
func para(style, jc, text string) string {
	var pPr strings.Builder
	if style != "" || jc != "" {
		pPr.WriteString("<w:pPr>")
		if style != "" {
			pPr.WriteString(`<w:pStyle w:val="` + style + `"/>`)
		}
		if jc != "" {
			pPr.WriteString(`<w:jc w:val="` + jc + `"/>`)
		}
		pPr.WriteString("</w:pPr>")
	}
	return "<w:p>" + pPr.String() + `<w:r><w:t xml:space="preserve">` + text + "</w:t></w:r></w:p>"
}

// paraItalicUnderline builds a paragraph with a single run carrying italic +
// underline run properties, so a golden exercises the run-property mapping
// (w:i/w:u → italic + text-decoration:underline) end to end.
func paraItalicUnderline(text string) string {
	return "<w:p><w:r><w:rPr><w:i/><w:u w:val=\"single\"/></w:rPr>" +
		`<w:t xml:space="preserve">` + text + "</w:t></w:r></w:p>"
}

func paragraphDocx() []byte {
	body := para("", "", "The quick brown fox jumps over the lazy dog, and then the lazy dog jumps right back over the quick brown fox to even the score.")
	return New().SetDocument(docOpen + body + docClose).Bytes()
}

func justifyDocx() []byte {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 4)
	body := para("", "both", text)
	return New().SetDocument(docOpen + body + docClose).Bytes()
}

func styledDocx() []byte {
	doc := docOpen +
		para("Heading1", "", "A Bold Heading") +
		para("", "", "Body text under the heading, set in the default Normal style with ordinary running prose that wraps as needed.") +
		para("", "", "A second body paragraph to show inter-paragraph spacing in action.") +
		paraItalicUnderline("This line is italic and underlined.") +
		docClose
	return New().SetDocument(doc).SetStyles(styledStyles).Bytes()
}

func multipageDocx() []byte {
	var b strings.Builder
	b.WriteString(docOpen)
	// Enough body text to spill onto a second Letter page under the CSS engine
	// (aim for ~1.5–2 pages), so the pagination/parallel path stays exercised.
	line := "The quick brown fox jumps over the lazy dog. "
	for i := 0; i < 40; i++ {
		b.WriteString(para("", "", strings.Repeat(line, 2)))
	}
	b.WriteString(docClose)
	return New().SetDocument(b.String()).Bytes()
}

// styledStyles defines docDefaults plus a Normal style and a Heading1 style based
// on it (larger and bold), to exercise the basedOn cascade end to end.
const styledStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:docDefaults>
    <w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri"/><w:sz w:val="22"/></w:rPr></w:rPrDefault>
    <w:pPrDefault><w:pPr><w:spacing w:after="160"/></w:pPr></w:pPrDefault>
  </w:docDefaults>
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
  <w:style w:type="paragraph" w:styleId="Heading1">
    <w:name w:val="heading 1"/>
    <w:basedOn w:val="Normal"/>
    <w:pPr><w:spacing w:before="240" w:after="0"/></w:pPr>
    <w:rPr><w:b/><w:sz w:val="32"/><w:color w:val="2E74B5"/></w:rPr>
  </w:style>
</w:styles>`
