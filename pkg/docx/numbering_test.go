package docx

import "testing"

func TestParseNumbering(t *testing.T) {
	n, err := parseNumbering([]byte(`<?xml version="1.0"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0">
    <w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="&#8226;"/></w:lvl>
    <w:lvl w:ilvl="1"><w:numFmt w:val="decimal"/><w:lvlText w:val="%2."/></w:lvl>
  </w:abstractNum>
  <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`))
	if err != nil {
		t.Fatalf("parseNumbering: %v", err)
	}
	lvl0, ok := n.Level(1, 0)
	if !ok {
		t.Fatalf("Level(1,0) not found")
	}
	if lvl0.Format != NumFmtBullet {
		t.Fatalf("lvl0 format = %v, want bullet", lvl0.Format)
	}
	lvl1, ok := n.Level(1, 1)
	if !ok || lvl1.Format != NumFmtDecimal {
		t.Fatalf("lvl1 = %+v, want decimal", lvl1)
	}
	if lvl1.Text != "%2." {
		t.Fatalf("lvl1 text = %q, want %%2.", lvl1.Text)
	}
}

func TestParseNumPr(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:pPr><w:numPr><w:ilvl w:val="1"/><w:numId w:val="3"/></w:numPr></w:pPr><w:r><w:t>item</w:t></w:r></w:p>
</w:body></w:document>`)
	pp := doc.Body[0].Paragraph.Props
	if !pp.HasNum {
		t.Fatalf("HasNum = false, want true")
	}
	if pp.NumID != 3 || pp.ILvl != 1 {
		t.Fatalf("numPr = (numId %d, ilvl %d), want (3, 1)", pp.NumID, pp.ILvl)
	}
}
