package pptx

import (
	"testing"

	genpptx "github.com/nathanstitt/doctaculous/testdata/gen/pptx"
)

const ns = `xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

// TestParsePresentation covers the parser directly: slide size, hidden-slide
// skipping, title flags, run formatting, EMU frames, and layout-placeholder
// inheritance.
func TestParsePresentation(t *testing.T) {
	slide := `<?xml version="1.0"?>
<p:sld ` + ns + `><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="2" name="T"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
<p:spPr><a:xfrm><a:off x="1270000" y="635000"/><a:ext cx="2540000" cy="1270000"/></a:xfrm></p:spPr>
<p:txBody><a:bodyPr/><a:p><a:r><a:rPr b="1" i="1" sz="2400"><a:solidFill><a:srgbClr val="aa0011"/></a:solidFill></a:rPr><a:t>Title text</a:t></a:r></a:p></p:txBody></p:sp>
<p:sp><p:nvSpPr><p:cNvPr id="3" name="B"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr>
<p:spPr/>
<p:txBody><a:bodyPr/><a:p><a:pPr lvl="1"><a:buChar char="•"/></a:pPr><a:r><a:t>inherited frame</a:t></a:r></a:p></p:txBody></p:sp>
</p:spTree></p:cSld></p:sld>`
	hidden := `<?xml version="1.0"?>
<p:sld show="0" ` + ns + `><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="2" name="x"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr/>
<p:txBody><a:bodyPr/><a:p><a:r><a:t>secret</a:t></a:r></a:p></p:txBody></p:sp>
</p:spTree></p:cSld></p:sld>`
	layout := `<?xml version="1.0"?>
<p:sldLayout ` + ns + `><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="9" name="ph"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr>
<p:spPr><a:xfrm><a:off x="635000" y="1905000"/><a:ext cx="5080000" cy="2540000"/></a:xfrm></p:spPr>
<p:txBody><a:bodyPr/><a:p/></p:txBody></p:sp>
</p:spTree></p:cSld></p:sldLayout>`

	data := genpptx.New().SetSlideSize(9144000, 6858000).SetLayout(layout).
		AddSlide(slide).AddSlide(hidden).Bytes()
	pres, err := OpenBytes(data)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if pres.SlideWPt != 720 || pres.SlideHPt != 540 {
		t.Errorf("slide size = %g x %g, want 720 x 540", pres.SlideWPt, pres.SlideHPt)
	}
	if len(pres.Slides) != 1 {
		t.Fatalf("slides = %d, want 1 (hidden skipped)", len(pres.Slides))
	}
	shapes := pres.Slides[0].Shapes
	if len(shapes) != 2 {
		t.Fatalf("shapes = %d, want 2", len(shapes))
	}

	title := shapes[0]
	if !title.IsTitle {
		t.Error("first shape should be the title placeholder")
	}
	if title.XPt != 100 || title.YPt != 50 || title.WPt != 200 || title.HPt != 100 {
		t.Errorf("title frame = %g,%g %gx%g, want 100,50 200x100", title.XPt, title.YPt, title.WPt, title.HPt)
	}
	run := title.Paragraphs[0].Runs[0]
	if !run.Bold || !run.Italic || run.SizePt != 24 || run.ColorRGB != "AA0011" || run.Text != "Title text" {
		t.Errorf("title run = %+v", run)
	}

	body := shapes[1]
	if body.XPt != 50 || body.YPt != 150 || body.WPt != 400 || body.HPt != 200 {
		t.Errorf("inherited frame = %g,%g %gx%g, want the layout's 50,150 400x200", body.XPt, body.YPt, body.WPt, body.HPt)
	}
	para := body.Paragraphs[0]
	if para.Level != 1 || para.Bullet != "char" {
		t.Errorf("paragraph = %+v, want lvl 1 char bullet", para)
	}
}
