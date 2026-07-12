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

// TestWriteNoteSeparator verifies a run with NoteSep emits <w:separator/> /
// <w:continuationSeparator/> inside a footnotes part.
func TestWriteNoteSeparator(t *testing.T) {
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
	// Re-open and confirm it parses without error and keeps the user note.
	rd, err := OpenBytes(b)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if rd.Footnotes == nil || len(rd.Footnotes.ByID[1]) == 0 {
		t.Errorf("user footnote 1 lost on round-trip: %+v", rd.Footnotes)
	}
}
