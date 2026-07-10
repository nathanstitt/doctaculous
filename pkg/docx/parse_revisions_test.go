package docx

import (
	"testing"
)

const wDoc = `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`

func TestParseInsertRevision(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:p><w:r><w:t>kept </w:t></w:r>
<w:ins w:id="7" w:author="Ada" w:date="2026-07-01T00:00:00Z"><w:r><w:t>added</w:t></w:r></w:ins></w:p>
</w:body></w:document>`)
	content := doc.Body[0].Paragraph.Content
	if len(content) != 2 || content[1].Revision == nil {
		t.Fatalf("content = %+v, want [run, revision]", content)
	}
	rev := content[1].Revision
	if rev.Kind != RevisionInsert || rev.ID != 7 || rev.Author != "Ada" || rev.Date != "2026-07-01T00:00:00Z" {
		t.Errorf("revision = %+v, want insert id=7 author=Ada", rev)
	}
	if len(rev.Content) != 1 || rev.Content[0].Run == nil || rev.Content[0].Run.Text != "added" {
		t.Errorf("revision content = %+v, want one 'added' run", rev.Content)
	}
}

func TestParseDeleteRevisionDelText(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:p><w:del w:id="8" w:author="Bo"><w:r><w:delText xml:space="preserve">gone </w:delText></w:r></w:del></w:p>
</w:body></w:document>`)
	rev := doc.Body[0].Paragraph.Content[0].Revision
	if rev == nil || rev.Kind != RevisionDelete || rev.Author != "Bo" {
		t.Fatalf("revision = %+v, want delete by Bo", rev)
	}
	if rev.Content[0].Run.Text != "gone " {
		t.Errorf("delText = %q, want 'gone ' (space preserved)", rev.Content[0].Run.Text)
	}
}

func TestParseNestedRevisionAndHyperlink(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:p><w:ins w:id="1" w:author="A">
  <w:hyperlink r:id="rId5"><w:r><w:t>linked</w:t></w:r></w:hyperlink>
  <w:ins w:id="2" w:author="B"><w:r><w:t>nested</w:t></w:r></w:ins>
</w:ins></w:p>
</w:body></w:document>`)
	rev := doc.Body[0].Paragraph.Content[0].Revision
	if rev == nil || len(rev.Content) != 2 {
		t.Fatalf("outer revision content = %+v, want [hyperlink, revision]", rev)
	}
	if rev.Content[0].Hyperlink == nil || rev.Content[0].Hyperlink.RelID != "rId5" {
		t.Errorf("first child = %+v, want hyperlink rId5", rev.Content[0])
	}
	inner := rev.Content[1].Revision
	if inner == nil || inner.ID != 2 || inner.Content[0].Run.Text != "nested" {
		t.Errorf("nested revision = %+v", inner)
	}
}

func TestParseRPrChange(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:p><w:r><w:rPr><w:b/><w:rPrChange w:id="3" w:author="Cy" w:date="2026-01-01T00:00:00Z"><w:rPr><w:i/></w:rPr></w:rPrChange></w:rPr><w:t>x</w:t></w:r></w:p>
</w:body></w:document>`)
	run := doc.Body[0].Paragraph.Content[0].Run
	if !run.Props.Bold || !run.Props.HasBold {
		t.Errorf("after state should be bold: %+v", run.Props)
	}
	ch := run.Props.RPrChange
	if ch == nil || ch.Mark.ID != 3 || ch.Mark.Author != "Cy" {
		t.Fatalf("rPrChange = %+v", ch)
	}
	if !ch.Previous.Italic || ch.Previous.Bold {
		t.Errorf("before state should be italic, not bold: %+v", ch.Previous)
	}
}

func TestParsePPrChangeAndFramePr(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:p><w:pPr>
  <w:jc w:val="center"/>
  <w:framePr w:dropCap="drop" w:lines="3" w:wrap="around" w:hAnchor="text"/>
  <w:pPrChange w:id="4" w:author="Di"><w:pPr><w:jc w:val="right"/></w:pPr></w:pPrChange>
</w:pPr><w:r><w:t>D</w:t></w:r></w:p>
</w:body></w:document>`)
	props := doc.Body[0].Paragraph.Props
	if props.Justify != JustifyCenter {
		t.Errorf("after alignment = %v, want center", props.Justify)
	}
	if props.Frame == nil || props.Frame.DropCap != "drop" || props.Frame.Lines != 3 || props.Frame.Wrap != "around" {
		t.Errorf("frame = %+v, want dropCap=drop lines=3", props.Frame)
	}
	if props.PPrChange == nil || props.PPrChange.Mark.ID != 4 || props.PPrChange.Previous.Justify != JustifyRight {
		t.Errorf("pPrChange = %+v, want previous right", props.PPrChange)
	}
}

func TestParseCommentMarkersAndReference(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:p><w:commentRangeStart w:id="11"/><w:r><w:t>flagged</w:t></w:r><w:commentRangeEnd w:id="11"/>
<w:r><w:rPr><w:rStyle w:val="CommentReference"/></w:rPr><w:commentReference w:id="11"/></w:r></w:p>
</w:body></w:document>`)
	content := doc.Body[0].Paragraph.Content
	if len(content) != 4 {
		t.Fatalf("content = %+v, want [start, run, end, ref-run]", content)
	}
	if content[0].CommentStart == nil || content[0].CommentStart.ID != 11 {
		t.Errorf("first = %+v, want CommentStart 11", content[0])
	}
	if content[2].CommentEnd == nil || content[2].CommentEnd.ID != 11 {
		t.Errorf("third = %+v, want CommentEnd 11", content[2])
	}
	if content[3].Run == nil || content[3].Run.CommentRef != 11 {
		t.Errorf("fourth = %+v, want CommentRef 11 run", content[3])
	}
}

func TestParseHyperlinkHoistsCommentMarkers(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:p><w:hyperlink r:id="rId9"><w:commentRangeStart w:id="5"/><w:r><w:t>go</w:t></w:r><w:commentRangeEnd w:id="5"/></w:hyperlink></w:p>
</w:body></w:document>`)
	content := doc.Body[0].Paragraph.Content
	if len(content) != 3 {
		t.Fatalf("content = %+v, want [start, hyperlink, end] (markers hoisted)", content)
	}
	if content[0].CommentStart == nil || content[0].CommentStart.ID != 5 {
		t.Errorf("start not hoisted before the link: %+v", content[0])
	}
	if content[1].Hyperlink == nil || len(content[1].Hyperlink.Runs) != 1 {
		t.Errorf("hyperlink = %+v", content[1])
	}
	if content[2].CommentEnd == nil || content[2].CommentEnd.ID != 5 {
		t.Errorf("end not hoisted after the link: %+v", content[2])
	}
}

func TestParseEndnoteAndCommentReferenceRuns(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:p><w:r><w:t>claim</w:t></w:r><w:r><w:endnoteReference w:id="4"/></w:r></w:p>
</w:body></w:document>`)
	var found bool
	for _, c := range doc.Body[0].Paragraph.Content {
		if c.Run != nil && c.Run.EndnoteRef == 4 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no run with EndnoteRef=4: %+v", doc.Body[0].Paragraph.Content)
	}
}

func TestParseRunShading(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:p><w:r><w:rPr><w:shd w:val="clear" w:fill="FFEE00"/></w:rPr><w:t>shaded</w:t></w:r></w:p>
</w:body></w:document>`)
	props := doc.Body[0].Paragraph.Content[0].Run.Props
	if !props.Shd.HasFill || props.Shd.Fill.R != 0xFF || props.Shd.Fill.G != 0xEE || props.Shd.Fill.B != 0x00 {
		t.Errorf("run shd = %+v, want FFEE00", props.Shd)
	}
}

func TestParseBorderStyleNames(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:tbl><w:tblGrid><w:gridCol w:w="4000"/></w:tblGrid>
<w:tr><w:tc><w:tcPr><w:tcBorders>
  <w:top w:val="dashed" w:sz="8" w:color="FF0000"/>
  <w:bottom w:val="double" w:sz="4"/>
  <w:left w:val="nil"/>
</w:tcBorders></w:tcPr><w:p><w:r><w:t>c</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
</w:body></w:document>`)
	b := doc.Body[0].Table.Rows[0].Cells[0].Props.Borders
	if b.Top.Style != "dashed" || b.Top.None {
		t.Errorf("top = %+v, want style dashed", b.Top)
	}
	if b.Bottom.Style != "double" {
		t.Errorf("bottom = %+v, want style double", b.Bottom)
	}
	if !b.Left.None || b.Left.Style != "" {
		t.Errorf("left = %+v, want None", b.Left)
	}
}

func TestParseCellRevisions(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:tbl><w:tblGrid><w:gridCol w:w="4000"/></w:tblGrid>
<w:tr><w:tc><w:tcPr>
  <w:cellIns w:id="20" w:author="Ed"/>
  <w:tcPrChange w:id="21" w:author="Fi"><w:tcPr><w:vAlign w:val="bottom"/></w:tcPr></w:tcPrChange>
  <w:vAlign w:val="center"/>
</w:tcPr><w:p><w:r><w:t>c</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
</w:body></w:document>`)
	cell := doc.Body[0].Table.Rows[0].Cells[0]
	if cell.Ins == nil || cell.Ins.ID != 20 || cell.Ins.Author != "Ed" {
		t.Errorf("cellIns = %+v", cell.Ins)
	}
	if cell.Props.VAlign != VAlignCenter {
		t.Errorf("after vAlign = %v, want center", cell.Props.VAlign)
	}
	ch := cell.Props.TcPrChange
	if ch == nil || ch.Mark.ID != 21 || ch.Previous.VAlign != VAlignBottom {
		t.Errorf("tcPrChange = %+v, want previous bottom", ch)
	}
}

func TestParseAnchoredDrawing(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:p><w:r><w:drawing>
<wp:anchor xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">
  <wp:extent cx="914400" cy="457200"/>
  <wp:positionH relativeFrom="margin"><wp:align>right</wp:align></wp:positionH>
  <wp:positionV relativeFrom="paragraph"><wp:align>top</wp:align></wp:positionV>
  <wp:wrapSquare wrapText="bothSides"/>
  <wp:docPr id="1" name="pic" descr="a chart"/>
  <a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData>
    <pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">
      <pic:blipFill><a:blip r:embed="rId7"/></pic:blipFill>
    </pic:pic>
  </a:graphicData></a:graphic>
</wp:anchor>
</w:drawing></w:r></w:p>
</w:body></w:document>`)
	var dr *Drawing
	for _, c := range doc.Body[0].Paragraph.Content {
		if c.Drawing != nil {
			dr = c.Drawing
		}
	}
	if dr == nil {
		t.Fatal("no drawing parsed")
	}
	if !dr.Anchored || dr.WrapKind != "square" || dr.AlignH != "right" {
		t.Errorf("drawing = %+v, want anchored square right", dr)
	}
	if dr.Description != "a chart" || dr.RelID != "rId7" || dr.WidthEMU != 914400 {
		t.Errorf("drawing identity = %+v", dr)
	}
}

func TestParseParagraphSectPrAttachment(t *testing.T) {
	doc := mustParse(t, wDoc+`
<w:p><w:pPr><w:sectPr><w:pgSz w:w="16840" w:h="11900"/></w:sectPr></w:pPr><w:r><w:t>end of section</w:t></w:r></w:p>
<w:p><w:r><w:t>next</w:t></w:r></w:p>
</w:body></w:document>`)
	p := doc.Body[0].Paragraph
	if p.Props.SectPr == nil || p.Props.SectPr.PageW != 16840 {
		t.Errorf("paragraph SectPr = %+v, want attached landscape section", p.Props.SectPr)
	}
	if doc.Body[1].Paragraph.Props.SectPr != nil {
		t.Errorf("second paragraph should carry no SectPr")
	}
	if len(doc.Sections) == 0 || doc.Sections[0].PageW != 16840 {
		t.Errorf("Sections = %+v, want the paragraph section recorded", doc.Sections)
	}
}

func TestParseCommentsPart(t *testing.T) {
	cm, err := parseComments([]byte(`<?xml version="1.0"?>
<w:comments xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:comment w:id="11" w:author="Gil" w:initials="G" w:date="2026-02-02T10:00:00Z">
    <w:p><w:r><w:t>Please verify.</w:t></w:r></w:p>
  </w:comment>
</w:comments>`))
	if err != nil {
		t.Fatalf("parseComments: %v", err)
	}
	c, ok := cm[11]
	if !ok || c.Author != "Gil" || c.Initials != "G" || c.Date != "2026-02-02T10:00:00Z" {
		t.Fatalf("comment = %+v", c)
	}
	if len(c.Body) != 1 || c.Body[0].Paragraph.Content[0].Run.Text != "Please verify." {
		t.Errorf("comment body = %+v", c.Body)
	}
}

func TestNumberingStartAndOverride(t *testing.T) {
	num, err := parseNumbering([]byte(`<?xml version="1.0"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0">
    <w:lvl w:ilvl="0"><w:start w:val="5"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl>
  </w:abstractNum>
  <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
  <w:num w:numId="2"><w:abstractNumId w:val="0"/>
    <w:lvlOverride w:ilvl="0"><w:startOverride w:val="9"/></w:lvlOverride>
  </w:num>
</w:numbering>`))
	if err != nil {
		t.Fatalf("parseNumbering: %v", err)
	}
	lvl, ok := num.Level(1, 0)
	if !ok || !lvl.HasStart || lvl.Start != 5 {
		t.Errorf("Level(1,0) = %+v, want start 5", lvl)
	}
	if got := num.StartAt(1, 0); got != 5 {
		t.Errorf("StartAt(1,0) = %d, want 5 (abstract w:start)", got)
	}
	if got := num.StartAt(2, 0); got != 9 {
		t.Errorf("StartAt(2,0) = %d, want 9 (startOverride wins)", got)
	}
	if got := num.StartAt(99, 0); got != 1 {
		t.Errorf("StartAt(unknown) = %d, want the default 1", got)
	}
}
