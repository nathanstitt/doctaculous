package docx

import "testing"

func TestParseHyperlinkStructure(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>
<w:p>
  <w:r><w:t>See </w:t></w:r>
  <w:hyperlink r:id="rId5"><w:r><w:t>the site</w:t></w:r></w:hyperlink>
</w:p>
</w:body></w:document>`)
	c := doc.Body[0].Paragraph.Content
	if len(c) != 2 {
		t.Fatalf("content = %d, want 2 (run + hyperlink)", len(c))
	}
	if c[0].Run == nil || c[0].Run.Text != "See " {
		t.Fatalf("content[0] = %+v, want run 'See '", c[0])
	}
	h := c[1].Hyperlink
	if h == nil {
		t.Fatalf("content[1].Hyperlink = nil, want a hyperlink")
	}
	if h.RelID != "rId5" {
		t.Fatalf("hyperlink relID = %q, want rId5", h.RelID)
	}
	if len(h.Runs) != 1 || h.Runs[0].Text != "the site" {
		t.Fatalf("hyperlink runs = %+v, want ['the site']", h.Runs)
	}
}
