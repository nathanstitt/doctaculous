package docx

import "testing"

func TestParseFootnoteReference(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>claim</w:t></w:r><w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:footnoteReference w:id="2"/></w:r></w:p>
</w:body></w:document>`)
	// The footnoteReference run carries FootnoteRef=2.
	var found bool
	for _, c := range doc.Body[0].Paragraph.Content {
		if c.Run != nil && c.Run.FootnoteRef == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no run with FootnoteRef=2 in %+v", doc.Body[0].Paragraph.Content)
	}
}

func TestParseFootnotesPart(t *testing.T) {
	fn, err := parseNotes([]byte(`<?xml version="1.0"?>
<w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:footnote w:id="2"><w:p><w:r><w:t>A note.</w:t></w:r></w:p></w:footnote>
</w:footnotes>`), "footnote")
	if err != nil {
		t.Fatalf("parseNotes: %v", err)
	}
	blocks, ok := fn.Note(2)
	if !ok || len(blocks) != 1 {
		t.Fatalf("Note(2) = %+v, ok=%v, want 1 block", blocks, ok)
	}
	if got := blocks[0].Paragraph.Content[0].Run.Text; got != "A note." {
		t.Fatalf("note text = %q, want 'A note.'", got)
	}
}

func TestParseEndnotesPart(t *testing.T) {
	en, err := parseNotes([]byte(`<?xml version="1.0"?>
<w:endnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:endnote w:id="3"><w:p><w:r><w:t>An endnote.</w:t></w:r></w:p></w:endnote>
</w:endnotes>`), "endnote")
	if err != nil {
		t.Fatalf("parseNotes: %v", err)
	}
	blocks, ok := en.Note(3)
	if !ok || len(blocks) != 1 {
		t.Fatalf("Note(3) = %+v, ok=%v, want 1 block", blocks, ok)
	}
	if got := blocks[0].Paragraph.Content[0].Run.Text; got != "An endnote." {
		t.Fatalf("endnote text = %q, want 'An endnote.'", got)
	}
}
