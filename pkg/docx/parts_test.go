package docx

import "testing"

func TestParseHeaderPart(t *testing.T) {
	hf, err := parseHdrFtr([]byte(`<?xml version="1.0"?>
<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:p><w:r><w:t>Page header</w:t></w:r></w:p>
</w:hdr>`), "hdr")
	if err != nil {
		t.Fatalf("parseHdrFtr: %v", err)
	}
	if len(hf.Blocks) != 1 || hf.Blocks[0].Paragraph == nil {
		t.Fatalf("header blocks = %+v, want 1 paragraph", hf.Blocks)
	}
	if got := hf.Blocks[0].Paragraph.Content[0].Run.Text; got != "Page header" {
		t.Fatalf("header text = %q, want 'Page header'", got)
	}
}

func TestParseSectPrHeaderReference(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>
<w:p><w:r><w:t>body</w:t></w:r></w:p>
<w:sectPr>
  <w:headerReference w:type="default" r:id="rId10"/>
  <w:footerReference w:type="default" r:id="rId11"/>
  <w:pgSz w:w="12240" w:h="15840"/>
</w:sectPr>
</w:body></w:document>`)
	if doc.Section.HeaderRefDefault != "rId10" {
		t.Fatalf("header ref = %q, want rId10", doc.Section.HeaderRefDefault)
	}
	if doc.Section.FooterRefDefault != "rId11" {
		t.Fatalf("footer ref = %q, want rId11", doc.Section.FooterRefDefault)
	}
}
