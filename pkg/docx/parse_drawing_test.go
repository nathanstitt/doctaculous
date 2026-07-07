package docx

import "testing"

func TestParseDrawing(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>
<w:p><w:r><w:drawing>
  <wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">
    <wp:extent cx="914400" cy="457200"/>
    <a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
      <a:graphicData>
        <pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">
          <pic:blipFill><a:blip r:embed="rId7"/></pic:blipFill>
        </pic:pic>
      </a:graphicData>
    </a:graphic>
  </wp:inline>
</w:drawing></w:r></w:p>
</w:body></w:document>`)
	c := doc.Body[0].Paragraph.Content
	if len(c) != 1 || c[0].Drawing == nil {
		t.Fatalf("content = %+v, want one drawing", c)
	}
	dr := c[0].Drawing
	if dr.RelID != "rId7" {
		t.Fatalf("drawing RelID = %q, want rId7", dr.RelID)
	}
	if dr.WidthEMU != 914400 || dr.HeightEMU != 457200 {
		t.Fatalf("drawing extent = (%d, %d), want (914400, 457200)", dr.WidthEMU, dr.HeightEMU)
	}
}
