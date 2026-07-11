package docx

import "testing"

// TestParsePackageFidelityParts covers the package-level plumbing added for
// the public model: endnotes and comments parts, customXml preservation
// (ExtraParts), hyperlink Target resolution through the rels, and the exported
// Relationship.Type.
func TestParsePackageFidelityParts(t *testing.T) {
	pkg := pkgWithParts(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>
<w:p><w:hyperlink r:id="rId5"><w:r><w:t>site</w:t></w:r></w:hyperlink>
<w:r><w:endnoteReference w:id="1"/></w:r></w:p>
</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/x" TargetMode="External"/>
</Relationships>`,
		"word/endnotes.xml": `<?xml version="1.0"?>
<w:endnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:endnote w:id="1"><w:p><w:r><w:t>the endnote</w:t></w:r></w:p></w:endnote>
</w:endnotes>`,
		"word/comments.xml": `<?xml version="1.0"?>
<w:comments xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:comment w:id="2" w:author="Ana"><w:p><w:r><w:t>note this</w:t></w:r></w:p></w:comment>
</w:comments>`,
		"customXml/item1.xml": `<myapp><data/></myapp>`,
	})

	doc, err := parsePackage(pkg)
	if err != nil {
		t.Fatalf("parsePackage: %v", err)
	}

	if blocks, ok := doc.Endnotes.Note(1); !ok || blocks[0].Paragraph.Content[0].Run.Text != "the endnote" {
		t.Errorf("Endnotes.Note(1) = %+v, ok=%v", blocks, ok)
	}
	if c, ok := doc.Comments[2]; !ok || c.Author != "Ana" {
		t.Errorf("Comments[2] = %+v", doc.Comments)
	}
	if got := string(doc.ExtraParts["customXml/item1.xml"]); got != `<myapp><data/></myapp>` {
		t.Errorf("ExtraParts customXml = %q", got)
	}

	// Hyperlink Target resolved through the (external) relationship.
	h := doc.Body[0].Paragraph.Content[0].Hyperlink
	if h == nil || h.Target != "https://example.com/x" {
		t.Errorf("hyperlink = %+v, want resolved Target", h)
	}
	if rel := doc.Rels["rId5"]; rel.Type != "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" {
		t.Errorf("Relationship.Type = %q", rel.Type)
	}
}
