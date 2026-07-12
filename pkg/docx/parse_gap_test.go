package docx

import "testing"

// TestParseSdtBlockUnwrap verifies a block-level w:sdt (content control) has its
// w:sdtContent unwrapped so the inner paragraphs survive parsing rather than being
// silently dropped.
func TestParseSdtBlockUnwrap(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:p><w:r><w:t>before</w:t></w:r></w:p>
  <w:sdt>
    <w:sdtPr><w:alias w:val="ctl"/></w:sdtPr>
    <w:sdtContent>
      <w:p><w:r><w:t>inside one</w:t></w:r></w:p>
      <w:p><w:r><w:t>inside two</w:t></w:r></w:p>
    </w:sdtContent>
  </w:sdt>
  <w:p><w:r><w:t>after</w:t></w:r></w:p>
</w:body></w:document>`

	d := mustParse(t, doc)
	var texts []string
	for _, b := range d.Body {
		if b.Paragraph == nil {
			continue
		}
		for _, r := range runsOf(b.Paragraph) {
			texts = append(texts, r.Text)
		}
	}
	want := []string{"before", "inside one", "inside two", "after"}
	if len(texts) != len(want) {
		t.Fatalf("got %d paragraphs %v, want %v", len(texts), texts, want)
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Errorf("paragraph %d = %q, want %q", i, texts[i], want[i])
		}
	}
}

// TestParseSdtInlineUnwrap verifies an inline w:sdt inside a paragraph has its
// w:sdtContent runs unwrapped in place.
func TestParseSdtInlineUnwrap(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:p>
    <w:r><w:t xml:space="preserve">a </w:t></w:r>
    <w:sdt>
      <w:sdtPr><w:alias w:val="field"/></w:sdtPr>
      <w:sdtContent><w:r><w:t xml:space="preserve">b </w:t></w:r></w:sdtContent>
    </w:sdt>
    <w:r><w:t>c</w:t></w:r>
  </w:p>
</w:body></w:document>`

	d := mustParse(t, doc)
	if len(d.Body) != 1 || d.Body[0].Paragraph == nil {
		t.Fatalf("expected one paragraph, got %+v", d.Body)
	}
	var got string
	for _, r := range runsOf(d.Body[0].Paragraph) {
		got += r.Text
	}
	if got != "a b c" {
		t.Errorf("inline sdt unwrap = %q, want %q", got, "a b c")
	}
}

// TestParseSdtNested verifies a block sdt nested inside another block sdt fully
// unwraps.
func TestParseSdtNested(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:sdt><w:sdtContent>
    <w:sdt><w:sdtContent>
      <w:p><w:r><w:t>deep</w:t></w:r></w:p>
    </w:sdtContent></w:sdt>
  </w:sdtContent></w:sdt>
</w:body></w:document>`

	d := mustParse(t, doc)
	if len(d.Body) != 1 || d.Body[0].Paragraph == nil {
		t.Fatalf("expected one unwrapped paragraph, got %+v", d.Body)
	}
	if got := runsOf(d.Body[0].Paragraph)[0].Text; got != "deep" {
		t.Errorf("nested sdt text = %q, want %q", got, "deep")
	}
}

// TestParseDrawingTitle verifies wp:docPr @title is parsed into Drawing.Title
// (distinct from @descr → Description).
func TestParseDrawingTitle(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:p><w:r><w:drawing>
    <wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">
      <wp:extent cx="914400" cy="914400"/>
      <wp:docPr id="1" name="Picture 1" descr="alt text" title="the title"/>
      <a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData><pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:blipFill><a:blip r:embed="rId9" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"/></pic:blipFill></pic:pic></a:graphicData></a:graphic>
    </wp:inline>
  </w:drawing></w:r></w:p>
</w:body></w:document>`

	d := mustParse(t, doc)
	var dr *Drawing
	for _, c := range d.Body[0].Paragraph.Content {
		if c.Drawing != nil {
			dr = c.Drawing
		}
	}
	if dr == nil {
		t.Fatal("no drawing parsed")
	}
	if dr.Title != "the title" {
		t.Errorf("Drawing.Title = %q, want %q", dr.Title, "the title")
	}
	if dr.Description != "alt text" {
		t.Errorf("Drawing.Description = %q, want %q", dr.Description, "alt text")
	}
}

// TestParseHighlightName verifies the raw w:highlight val is preserved in
// RunProps.HighlightName (so a consumer can apply its own palette rather than the
// resolved RGBA).
func TestParseHighlightName(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:p><w:r><w:rPr><w:highlight w:val="darkGreen"/></w:rPr><w:t>x</w:t></w:r></w:p>
</w:body></w:document>`

	d := mustParse(t, doc)
	rp := runsOf(d.Body[0].Paragraph)[0].Props
	if !rp.HasHighlight {
		t.Fatal("expected HasHighlight")
	}
	if rp.HighlightName != "darkGreen" {
		t.Errorf("HighlightName = %q, want %q", rp.HighlightName, "darkGreen")
	}
}

// TestWriteHighlightNameValidated verifies the writer emits HighlightName only when
// it is a valid ST_HighlightColor, falling back to remapping the RGBA when a consumer
// supplies a name outside the palette (which would otherwise emit an invalid w:highlight).
func TestWriteHighlightNameValidated(t *testing.T) {
	// A garbage name with an RGBA that maps back to a real palette entry ("yellow").
	doc := &Document{Styles: DefaultStyles(),
		Body: []Block{{Paragraph: &Paragraph{Content: []ParaChild{{Run: &Run{Text: "x",
			Props: RunProps{HasHighlight: true, Highlight: rgba(0xFF, 0xFF, 0),
				HighlightName: "#ff0000"}}}}}}}}
	b, err := Bytes(doc)
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	// Reopen: the emitted highlight must be the valid RGBA-mapped name, so the parser
	// resolves it (a "#ff0000" emitted verbatim would not be a known ST_HighlightColor
	// and would parse back with HasHighlight=false).
	rd, err := OpenBytes(b)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	rp := runsOf(rd.Body[0].Paragraph)[0].Props
	if !rp.HasHighlight {
		t.Fatal("highlight lost on round-trip (invalid name likely emitted verbatim)")
	}
	if rp.HighlightName != "yellow" {
		t.Errorf("HighlightName = %q, want %q (RGBA fallback)", rp.HighlightName, "yellow")
	}
}

// TestVerbatimCharStyle verifies DefaultStyles includes the VerbatimChar
// character style (the inline-code style tinycld emits and reads).
func TestVerbatimCharStyle(t *testing.T) {
	st := DefaultStyles()
	s, ok := st.ByID["VerbatimChar"]
	if !ok {
		t.Fatal("VerbatimChar style missing from DefaultStyles")
	}
	if s.Type != "character" {
		t.Errorf("VerbatimChar type = %q, want character", s.Type)
	}
}

// noteSepOf returns the NoteSep kind of a single-run note's first paragraph, or
// NoteSepNone if the note is empty / not a lone separator run.
func noteSepOf(blocks []Block) NoteSepKind {
	if len(blocks) == 0 || blocks[0].Paragraph == nil {
		return NoteSepNone
	}
	runs := runsOf(blocks[0].Paragraph)
	if len(runs) == 0 {
		return NoteSepNone
	}
	return runs[0].NoteSep
}

// TestNoteSeparatorRoundTrip verifies the reserved separator notes (ids -1 and 0)
// survive a write→parse cycle: the writer emits <w:separator/> /
// <w:continuationSeparator/>, and the parser reads them back into Run.NoteSep so the
// content-less run is not culled on a subsequent write.
func TestNoteSeparatorRoundTrip(t *testing.T) {
	notes := NewNotes()
	notes.ByID[-1] = []Block{{Paragraph: &Paragraph{Content: []ParaChild{{Run: &Run{NoteSep: NoteSepSeparator}}}}}}
	notes.ByID[0] = []Block{{Paragraph: &Paragraph{Content: []ParaChild{{Run: &Run{NoteSep: NoteSepContinuation}}}}}}
	notes.ByID[1] = []Block{{Paragraph: &Paragraph{Content: []ParaChild{{Run: &Run{Text: "body"}}}}}}
	doc := &Document{Styles: DefaultStyles(), Footnotes: notes,
		Body: []Block{{Paragraph: &Paragraph{Content: []ParaChild{{Run: &Run{Text: "x", FootnoteRef: 1}}}}}}}
	b, err := Bytes(doc)
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	rd, err := OpenBytes(b)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if rd.Footnotes == nil {
		t.Fatal("footnotes part lost on round-trip")
	}
	if got := noteSepOf(rd.Footnotes.ByID[-1]); got != NoteSepSeparator {
		t.Errorf("id -1 NoteSep = %v, want NoteSepSeparator", got)
	}
	if got := noteSepOf(rd.Footnotes.ByID[0]); got != NoteSepContinuation {
		t.Errorf("id 0 NoteSep = %v, want NoteSepContinuation", got)
	}
	if len(rd.Footnotes.ByID[1]) == 0 {
		t.Errorf("user footnote 1 lost on round-trip: %+v", rd.Footnotes)
	}
}

// TestParseSdtNoContent verifies a w:sdt with no w:sdtContent (e.g. an unbound
// placeholder control) drains cleanly and contributes no block, rather than
// derailing the surrounding body.
func TestParseSdtNoContent(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:p><w:r><w:t>before</w:t></w:r></w:p>
  <w:sdt><w:sdtPr><w:alias w:val="placeholder"/></w:sdtPr></w:sdt>
  <w:p><w:r><w:t>after</w:t></w:r></w:p>
</w:body></w:document>`

	d := mustParse(t, doc)
	var texts []string
	for _, b := range d.Body {
		if b.Paragraph == nil {
			continue
		}
		texts = append(texts, runsOf(b.Paragraph)[0].Text)
	}
	want := []string{"before", "after"}
	if len(texts) != len(want) || texts[0] != want[0] || texts[1] != want[1] {
		t.Fatalf("got %v, want %v", texts, want)
	}
}

// TestParseSdtBlockTable verifies a block-level w:sdt wrapping a table unwraps to
// the table (not just paragraphs — the block path routes through parseBlockChild's
// tbl case).
func TestParseSdtBlockTable(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:sdt><w:sdtContent>
    <w:tbl>
      <w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr>
    </w:tbl>
  </w:sdtContent></w:sdt>
</w:body></w:document>`

	d := mustParse(t, doc)
	if len(d.Body) != 1 || d.Body[0].Table == nil {
		t.Fatalf("expected one unwrapped table, got %+v", d.Body)
	}
	tbl := d.Body[0].Table
	if len(tbl.Rows) != 1 || len(tbl.Rows[0].Cells) != 1 {
		t.Fatalf("table shape = %d rows, want 1x1", len(tbl.Rows))
	}
	got := runsOf(tbl.Rows[0].Cells[0].Blocks[0].Paragraph)[0].Text
	if got != "cell" {
		t.Errorf("cell text = %q, want %q", got, "cell")
	}
}

// TestParseSdtInlineHyperlink verifies an inline w:sdt wrapping a hyperlink unwraps
// the hyperlink in place (not only bare runs).
func TestParseSdtInlineHyperlink(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:p>
    <w:r><w:t xml:space="preserve">see </w:t></w:r>
    <w:sdt><w:sdtContent>
      <w:hyperlink r:id="rId5"><w:r><w:t>link</w:t></w:r></w:hyperlink>
    </w:sdtContent></w:sdt>
  </w:p>
</w:body></w:document>`

	d := mustParse(t, doc)
	if len(d.Body) != 1 || d.Body[0].Paragraph == nil {
		t.Fatalf("expected one paragraph, got %+v", d.Body)
	}
	var sawHyperlink bool
	for _, c := range d.Body[0].Paragraph.Content {
		if c.Hyperlink != nil {
			sawHyperlink = true
			if got := c.Hyperlink.Runs[0].Text; got != "link" {
				t.Errorf("hyperlink text = %q, want %q", got, "link")
			}
		}
	}
	if !sawHyperlink {
		t.Error("inline sdt did not unwrap its hyperlink")
	}
}
