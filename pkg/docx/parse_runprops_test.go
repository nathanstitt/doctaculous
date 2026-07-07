package docx

import "testing"

func TestParseTier3RunProps(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:rPr>
  <w:strike/>
  <w:vertAlign w:val="superscript"/>
  <w:highlight w:val="yellow"/>
  <w:caps/>
</w:rPr><w:t>styled</w:t></w:r></w:p>
</w:body></w:document>`)
	rp := doc.Body[0].Paragraph.Content[0].Run.Props
	if !rp.HasStrike || !rp.Strike {
		t.Fatalf("Strike = %v (has %v), want true", rp.Strike, rp.HasStrike)
	}
	if rp.VertAlign != VertAlignSuperscript {
		t.Fatalf("VertAlign = %v, want superscript", rp.VertAlign)
	}
	if !rp.HasHighlight || rp.Highlight.R != 0xFF || rp.Highlight.G != 0xFF || rp.Highlight.B != 0x00 {
		t.Fatalf("Highlight = %+v (has %v), want yellow", rp.Highlight, rp.HasHighlight)
	}
	if !rp.HasCaps || !rp.Caps {
		t.Fatalf("Caps = %v (has %v), want true", rp.Caps, rp.HasCaps)
	}
}
