package docx

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"strings"
)

// itoa is strconv.Itoa; a tiny local alias keeps the fixture strings readable.
func itoa(n int) string { return strconv.Itoa(n) }

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
	{
		Name:  "table",
		Desc:  "a 2x2 bordered table with shaded header cells",
		Pages: 1,
		Build: tableDocx,
	},
	{
		Name:  "table-spans",
		Desc:  "a table exercising gridSpan (colspan) and vMerge (rowspan)",
		Pages: 1,
		Build: tableSpansDocx,
	},
	{
		Name:  "list",
		Desc:  "an ordered (decimal) list and an unordered (bullet) list",
		Pages: 1,
		Build: listDocx,
	},
	{
		Name:  "hyperlink",
		Desc:  "a paragraph with an external hyperlink (link-styled text)",
		Pages: 1,
		Build: hyperlinkDocx,
	},
	{
		Name:  "image",
		Desc:  "a paragraph with an embedded PNG drawing",
		Pages: 1,
		Build: imageDocx,
	},
	{
		Name:  "header-footer",
		Desc:  "a default header and footer rendered in the page margins",
		Pages: 1,
		Build: headerFooterDocx,
	},
	{
		Name:  "footnote",
		Desc:  "a footnote reference (superscript marker) with a parsed note",
		Pages: 1,
		Build: footnoteDocx,
	},
	{
		Name:  "run-props",
		Desc:  "strikethrough, superscript, highlight, and caps runs",
		Pages: 1,
		Build: runPropsDocx,
	},
	{
		Name: "fidelity",
		Desc: "tracked changes rendered final-state, invisible comment anchors, run shading, " +
			"an endnote marker, a list starting at 5, and a right-floated anchored image",
		Pages: 1,
		Build: fidelityDocx,
	},
	{
		Name: "model-specimen",
		Desc: "a document CONSTRUCTED through the public pkg/docx model and serialized with " +
			"docx.Bytes — the writer-first consumer path — sampling styles, emphasis, shading, " +
			"a hyperlink, a footnote, a tracked change, a start-at-4 list, and a bordered table",
		Pages: 1,
		Build: modelSpecimenDocx,
	},
}

const docOpen = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`

// docOpenR is docOpen plus the officeDocument relationships namespace, needed by
// fixtures using r:id (hyperlinks, drawings).
const docOpenR = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`

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

// cellBorders is a per-cell w:tcBorders fragment drawing a thin border on all four
// edges in the given RRGGBB color. Per-cell borders (unlike the table-level
// w:insideH/w:insideV, which this parser does not read) are honored, so the grid
// lines between cells actually render.
func cellBorders(color string) string {
	edge := func(side string) string {
		return `<w:` + side + ` w:val="single" w:sz="4" w:color="` + color + `"/>`
	}
	return `<w:tcBorders>` + edge("top") + edge("bottom") + edge("left") + edge("right") + `</w:tcBorders>`
}

// tblCell wraps cell content XML in a w:tc with optional tcPr (raw XML).
func tblCell(tcPr, content string) string {
	inner := ""
	if tcPr != "" {
		inner = "<w:tcPr>" + tcPr + "</w:tcPr>"
	}
	return "<w:tc>" + inner + content + "</w:tc>"
}

// tableDocx builds a 2x2 table: a shaded, header row over a body row, with a thin
// black grid drawn from per-cell borders (sz=4 eighths-of-a-point ≈ 0.5pt).
func tableDocx() []byte {
	tblPr := `<w:tblPr><w:tblW w:type="dxa" w:w="8000"/></w:tblPr>`
	grid := `<w:tblGrid><w:gridCol w:w="4000"/><w:gridCol w:w="4000"/></w:tblGrid>`
	hdr := cellBorders("000000") + `<w:shd w:fill="D9E2F3"/>`
	body := cellBorders("000000")
	row1 := "<w:tr>" +
		tblCell(hdr, para("", "", "Name")) +
		tblCell(hdr, para("", "", "Score")) + "</w:tr>"
	row2 := "<w:tr>" +
		tblCell(body, para("", "", "Alice")) +
		tblCell(body, para("", "", "42")) + "</w:tr>"
	tbl := "<w:tbl>" + tblPr + grid + row1 + row2 + "</w:tbl>"
	doc := docOpen + para("", "", "A table:") + tbl + docClose
	return New().SetDocument(doc).Bytes()
}

// tableSpansDocx builds a 3-column table where a header cell spans two rows (vMerge
// restart/continue) in column 0 and a body cell spans two columns (gridSpan) in row 2.
// Per-cell borders make the span geometry visible.
func tableSpansDocx() []byte {
	tblPr := `<w:tblPr><w:tblW w:type="dxa" w:w="8000"/></w:tblPr>`
	grid := `<w:tblGrid><w:gridCol w:w="2666"/><w:gridCol w:w="2667"/><w:gridCol w:w="2667"/></w:tblGrid>`
	bd := cellBorders("333333")
	// Row 1: a vMerge-restart cell in col 0, then two normal cells.
	row1 := "<w:tr>" +
		tblCell(bd+`<w:vMerge w:val="restart"/>`, para("", "", "Merged")) +
		tblCell(bd, para("", "", "B")) +
		tblCell(bd, para("", "", "C")) + "</w:tr>"
	// Row 2: the vMerge-continue cell (col 0, covered), then a gridSpan=2 cell.
	row2 := "<w:tr>" +
		tblCell(bd+`<w:vMerge w:val="continue"/>`, para("", "", "")) +
		tblCell(bd+`<w:gridSpan w:val="2"/>`, para("", "", "Spans two columns")) + "</w:tr>"
	tbl := "<w:tbl>" + tblPr + grid + row1 + row2 + "</w:tbl>"
	doc := docOpen + tbl + docClose
	return New().SetDocument(doc).Bytes()
}

// listItem wraps text in a paragraph carrying a w:numPr (numId + ilvl).
func listItem(numID, ilvl int, text string) string {
	return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + itoa(ilvl) + `"/><w:numId w:val="` + itoa(numID) + `"/></w:numPr></w:pPr>` +
		`<w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

func listDocx() []byte {
	doc := docOpen +
		para("", "", "Ordered:") +
		listItem(1, 0, "First item") +
		listItem(1, 0, "Second item") +
		listItem(1, 0, "Third item") +
		para("", "", "Unordered:") +
		listItem(2, 0, "Bullet one") +
		listItem(2, 0, "Bullet two") +
		docClose
	return New().SetDocument(doc).SetNumbering(listNumbering).Bytes()
}

// listNumbering defines a decimal list (numId 1) and a bullet list (numId 2).
const listNumbering = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0">
    <w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl>
  </w:abstractNum>
  <w:abstractNum w:abstractNumId="1">
    <w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="&#8226;"/></w:lvl>
  </w:abstractNum>
  <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
  <w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num>
</w:numbering>`

const relHyperlink = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
const relImage = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"

func hyperlinkDocx() []byte {
	p := `<w:p>` +
		`<w:r><w:t xml:space="preserve">Visit </w:t></w:r>` +
		`<w:hyperlink r:id="rId5"><w:r><w:t>the project</w:t></w:r></w:hyperlink>` +
		`<w:r><w:t xml:space="preserve"> for more.</w:t></w:r>` +
		`</w:p>`
	doc := docOpenR + p + docClose
	return New().SetDocument(doc).
		AddRel("rId5", relHyperlink, "https://example.com/", "External").
		Bytes()
}

func imageDocx() []byte {
	p := `<w:p><w:r><w:drawing>` +
		`<wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">` +
		`<wp:extent cx="1828800" cy="914400"/>` +
		`<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData>` +
		`<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">` +
		`<pic:blipFill><a:blip r:embed="rId7"/></pic:blipFill>` +
		`</pic:pic></a:graphicData></a:graphic></wp:inline>` +
		`</w:drawing></w:r></w:p>`
	doc := docOpenR + para("", "", "An embedded image:") + p + docClose
	return New().SetDocument(doc).
		AddMedia("media/image1.png", tinyPNG(96, 48)).
		AddRel("rId7", relImage, "media/image1.png", "").
		Bytes()
}

// tinyPNG builds a solid-color PNG of the given pixel size for image fixtures.
func tinyPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill := color.RGBA{R: 0x33, G: 0x88, B: 0xCC, A: 0xFF}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, fill)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

const ctHeader = "application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"
const ctFooter = "application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"
const ctFootnotes = "application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml"
const relHeader = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
const relFooter = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer"
const relFootnotes = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes"

// docCloseHF is docClose with header/footer references in the section.
const docCloseHF = `<w:sectPr>` +
	`<w:headerReference w:type="default" r:id="rId10"/>` +
	`<w:footerReference w:type="default" r:id="rId11"/>` +
	`<w:pgSz w:w="12240" w:h="15840"/>` +
	`<w:pgMar w:top="1440" w:bottom="1440" w:left="1440" w:right="1440" w:header="720" w:footer="720"/>` +
	`</w:sectPr></w:body></w:document>`

func headerFooterDocx() []byte {
	doc := docOpenR +
		para("", "", "Body text between a header and a footer.") +
		docCloseHF
	hdr := `<?xml version="1.0"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		para("", "center", "DOCUMENT HEADER") + `</w:hdr>`
	ftr := `<?xml version="1.0"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		para("", "center", "Page footer") + `</w:ftr>`
	return New().SetDocument(doc).
		AddPart("header1.xml", ctHeader, hdr).
		AddPart("footer1.xml", ctFooter, ftr).
		AddRel("rId10", relHeader, "header1.xml", "").
		AddRel("rId11", relFooter, "footer1.xml", "").
		Bytes()
}

func footnoteDocx() []byte {
	p := `<w:p><w:r><w:t xml:space="preserve">A claim needing a citation</w:t></w:r>` +
		`<w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:footnoteReference w:id="2"/></w:r></w:p>`
	doc := docOpenR + p + docClose
	notes := `<?xml version="1.0"?><w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:footnote w:id="2">` + para("", "", "The supporting citation.") + `</w:footnote>` +
		`</w:footnotes>`
	return New().SetDocument(doc).
		AddPart("footnotes.xml", ctFootnotes, notes).
		AddRel("rId12", relFootnotes, "footnotes.xml", "").
		Bytes()
}

// runRPr builds a run with a raw w:rPr XML fragment and text.
func runRPr(rPr, text string) string {
	return `<w:r><w:rPr>` + rPr + `</w:rPr><w:t xml:space="preserve">` + text + `</w:t></w:r>`
}

const ctEndnotes = "application/vnd.openxmlformats-officedocument.wordprocessingml.endnotes+xml"
const ctComments = "application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"
const relEndnotes = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/endnotes"
const relComments = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments"

// fidelityDocx exercises the reader-fidelity vocabulary end to end: a tracked
// insertion renders, a tracked deletion does not (final-state), comment range
// markers and the reference run are invisible, a run carries w:shd shading, an
// endnote reference shows a superscript marker, an ordered list starts at 5 via
// w:startOverride, and an anchored square-wrap image floats right with the
// following text wrapping beside it.
func fidelityDocx() []byte {
	revisions := `<w:p>` +
		`<w:r><w:t xml:space="preserve">Tracked changes: kept </w:t></w:r>` +
		`<w:ins w:id="1" w:author="Ada" w:date="2026-07-01T00:00:00Z"><w:r><w:t xml:space="preserve">inserted </w:t></w:r></w:ins>` +
		`<w:del w:id="2" w:author="Ada" w:date="2026-07-01T00:00:00Z"><w:r><w:delText xml:space="preserve">DELETED </w:delText></w:r></w:del>` +
		`<w:r><w:t>final.</w:t></w:r>` +
		`</w:p>`
	comments := `<w:p>` +
		`<w:commentRangeStart w:id="3"/>` +
		`<w:r><w:t xml:space="preserve">Commented text reads clean</w:t></w:r>` +
		`<w:commentRangeEnd w:id="3"/>` +
		`<w:r><w:commentReference w:id="3"/></w:r>` +
		`<w:r><w:t xml:space="preserve"> — no anchor artifacts.</w:t></w:r>` +
		`</w:p>`
	shdAndEndnote := `<w:p>` +
		runRPr(`<w:shd w:val="clear" w:fill="FFF2A8"/>`, "shaded run") +
		`<w:r><w:t xml:space="preserve"> and an endnote</w:t></w:r>` +
		`<w:r><w:endnoteReference w:id="1"/></w:r>` +
		`</w:p>`
	listItem := func(text string) string {
		return `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="7"/></w:numPr></w:pPr>` +
			`<w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
	}
	floated := `<w:p><w:r><w:drawing>` +
		`<wp:anchor xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">` +
		`<wp:extent cx="1371600" cy="914400"/>` +
		`<wp:positionH relativeFrom="margin"><wp:align>right</wp:align></wp:positionH>` +
		`<wp:wrapSquare wrapText="bothSides"/>` +
		`<wp:docPr id="1" name="floater" descr="a floated image"/>` +
		`<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData>` +
		`<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">` +
		`<pic:blipFill><a:blip r:embed="rId7"/></pic:blipFill>` +
		`</pic:pic></a:graphicData></a:graphic></wp:anchor>` +
		`</w:drawing></w:r>` +
		`<w:r><w:t xml:space="preserve">` + strings.Repeat("Text wraps beside the floated image. ", 6) + `</w:t></w:r>` +
		`</w:p>`

	doc := docOpenR + revisions + comments + shdAndEndnote +
		listItem("List starts at five") + listItem("and continues at six") +
		floated + docClose

	numbering := `<?xml version="1.0"?><w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl></w:abstractNum>` +
		`<w:num w:numId="7"><w:abstractNumId w:val="0"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="5"/></w:lvlOverride></w:num>` +
		`</w:numbering>`
	endnotes := `<?xml version="1.0"?><w:endnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:endnote w:id="1">` + para("", "", "The endnote body.") + `</w:endnote>` +
		`</w:endnotes>`
	commentsPart := `<?xml version="1.0"?><w:comments xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:comment w:id="3" w:author="Reviewer">` + para("", "", "An invisible comment.") + `</w:comment>` +
		`</w:comments>`

	return New().SetDocument(doc).
		SetNumbering(numbering).
		AddMedia("media/image1.png", tinyPNG(108, 72)).
		AddRel("rId7", relImage, "media/image1.png", "").
		AddPart("endnotes.xml", ctEndnotes, endnotes).
		AddPart("comments.xml", ctComments, commentsPart).
		AddRel("rId13", relEndnotes, "endnotes.xml", "").
		AddRel("rId14", relComments, "comments.xml", "").
		Bytes()
}

func runPropsDocx() []byte {
	p := `<w:p>` +
		runRPr(`<w:strike/>`, "struck ") +
		`<w:r><w:t xml:space="preserve">normal E=mc</w:t></w:r>` +
		runRPr(`<w:vertAlign w:val="superscript"/>`, "2") +
		runRPr(`<w:highlight w:val="yellow"/>`, " highlighted ") +
		runRPr(`<w:caps/>`, "small caps") +
		`</w:p>`
	doc := docOpen + p + docClose
	return New().SetDocument(doc).Bytes()
}
