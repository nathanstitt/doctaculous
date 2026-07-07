package docx

import "testing"

func TestParseCollectsAllSections(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:pPr><w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr></w:pPr><w:r><w:t>sec1</w:t></w:r></w:p>
<w:p><w:r><w:t>sec2 body</w:t></w:r></w:p>
<w:sectPr><w:pgSz w:w="15840" w:h="12240"/></w:sectPr>
</w:body></w:document>`)
	if len(doc.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(doc.Sections))
	}
	if doc.Sections[0].PageW != 12240 {
		t.Fatalf("section0 width = %d, want 12240 (portrait)", doc.Sections[0].PageW)
	}
	if doc.Sections[1].PageW != 15840 {
		t.Fatalf("section1 width = %d, want 15840 (landscape)", doc.Sections[1].PageW)
	}
	// doc.Section stays the last (body) section for byte-identical single-section behavior.
	if doc.Section.PageW != 15840 {
		t.Fatalf("doc.Section width = %d, want 15840", doc.Section.PageW)
	}
}
