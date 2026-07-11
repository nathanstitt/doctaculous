package doctaculous

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	genpptx "github.com/nathanstitt/doctaculous/testdata/gen/pptx"
)

const pptxNS = `xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

// pptxSpecimen builds the golden deck: slide 1 carries a title, nested
// bullets, and a picture; slide 2 a table plus a body placeholder that
// INHERITS its frame from the layout; a third, hidden slide must be skipped.
func pptxSpecimen() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 20))
	for y := range 20 {
		for x := range 32 {
			img.Set(x, y, color.RGBA{R: 0x33, G: 0x88, B: 0xCC, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}

	b := genpptx.New().SetSlideSize(9144000, 6858000) // 4:3, 10in × 7.5in
	imgIdx := b.AddMedia("image1.png", buf.Bytes())

	slide1 := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld ` + pptxNS + `><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="2" name="Title"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
<p:spPr><a:xfrm><a:off x="457200" y="274638"/><a:ext cx="8229600" cy="1143000"/></a:xfrm></p:spPr>
<p:txBody><a:bodyPr/><a:p><a:r><a:rPr lang="en" sz="3600" b="1"/><a:t>Quarterly Review</a:t></a:r></a:p></p:txBody></p:sp>
<p:sp><p:nvSpPr><p:cNvPr id="3" name="Body"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>
<p:spPr><a:xfrm><a:off x="457200" y="1600200"/><a:ext cx="5029200" cy="3200400"/></a:xfrm></p:spPr>
<p:txBody><a:bodyPr/>
<a:p><a:pPr lvl="0"><a:buChar char="•"/></a:pPr><a:r><a:t>Revenue grew</a:t></a:r></a:p>
<a:p><a:pPr lvl="1"><a:buChar char="•"/></a:pPr><a:r><a:rPr i="1"/><a:t>across regions</a:t></a:r></a:p>
<a:p><a:pPr lvl="0"><a:buAutoNum type="arabicPeriod"/></a:pPr><a:r><a:t>First priority</a:t></a:r></a:p>
<a:p><a:pPr lvl="0"><a:buAutoNum type="arabicPeriod"/></a:pPr><a:r><a:rPr b="1"/><a:t>Second priority</a:t></a:r></a:p>
<a:p><a:pPr><a:buNone/></a:pPr><a:r><a:rPr sz="1400"/><a:t>A closing plain note in </a:t></a:r><a:r><a:rPr sz="1400"><a:solidFill><a:srgbClr val="C82020"/></a:solidFill></a:rPr><a:t>red</a:t></a:r></a:p>
</p:txBody></p:sp>
<p:pic><p:nvPicPr><p:cNvPr id="4" name="Chart"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr>
<p:blipFill><a:blip r:embed="rId100"/></p:blipFill>
<p:spPr><a:xfrm><a:off x="5943600" y="1600200"/><a:ext cx="2286000" cy="1428750"/></a:xfrm></p:spPr></p:pic>
</p:spTree></p:cSld></p:sld>`

	slide2 := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld ` + pptxNS + `><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="2" name="Inherited"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr>
<p:spPr/>
<p:txBody><a:bodyPr/><a:p><a:r><a:t>Placed by the layout placeholder</a:t></a:r></a:p></p:txBody></p:sp>
<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="3" name="Table"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr>
<p:xfrm><a:off x="457200" y="2286000"/><a:ext cx="5486400" cy="1371600"/></p:xfrm>
<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl>
<a:tr h="370840"><a:tc gridSpan="2"><a:txBody><a:bodyPr/><a:p><a:r><a:rPr b="1"/><a:t>Metric</a:t></a:r></a:p></a:txBody></a:tc><a:tc hMerge="1"><a:txBody><a:bodyPr/><a:p/></a:txBody></a:tc></a:tr>
<a:tr h="370840"><a:tc><a:txBody><a:bodyPr/><a:p><a:r><a:t>Latency</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:bodyPr/><a:p><a:r><a:t>12ms</a:t></a:r></a:p></a:txBody></a:tc></a:tr>
</a:tbl></a:graphicData></a:graphic></p:graphicFrame>
</p:spTree></p:cSld></p:sld>`

	hidden := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld show="0" ` + pptxNS + `><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="2" name="x"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr/>
<p:txBody><a:bodyPr/><a:p><a:r><a:t>HIDDEN SLIDE CONTENT</a:t></a:r></a:p></p:txBody></p:sp>
</p:spTree></p:cSld></p:sld>`

	layout := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldLayout ` + pptxNS + `><p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="2" name="BodyPH"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr>
<p:spPr><a:xfrm><a:off x="914400" y="914400"/><a:ext cx="6858000" cy="914400"/></a:xfrm></p:spPr>
<p:txBody><a:bodyPr/><a:p/></p:txBody></p:sp>
</p:spTree></p:cSld></p:sldLayout>`

	return b.SetLayout(layout).
		AddSlide(slide1, imgIdx).
		AddSlide(slide2).
		AddSlide(hidden).
		Bytes()
}

// TestPPTXGolden renders slide 1 end to end — the PPTX visual entry. Run with
// -update, then eyeball.
func TestPPTXGolden(t *testing.T) {
	doc, err := OpenPPTXBytes(pptxSpecimen(), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenPPTXBytes: %v", err)
	}
	if doc.Format() != FormatPPTX {
		t.Errorf("Format() = %q, want pptx", doc.Format())
	}
	if doc.PageCount() != 2 {
		t.Errorf("PageCount = %d, want 2 (one per visible slide; the hidden slide skipped)", doc.PageCount())
	}
	if w, h, err := doc.PageSize(0); err != nil || w != 720 || h != 540 {
		t.Errorf("PageSize = %g x %g (%v), want 720 x 540 (the deck slide size)", w, h, err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI, BundledFonts: true})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	got, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized image is %T, want *image.RGBA", img)
	}

	dir := filepath.Join("testdata", "golden")
	path := filepath.Join(dir, "pptx-specimen.png")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writePNG(t, path, got)
		t.Logf("updated %s", path)
		return
	}
	want := readPNG(t, path)
	if want == nil {
		t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestPPTXGolden -update", path)
	}
	if diff, n := compareImages(want, got); diff {
		t.Errorf("render differs from golden %s: %d pixels beyond tolerance", path, n)
	}
}

// TestPPTXDetectionAndConvert pins the unified-conversion wiring and the
// reading-order contract for structure writers.
func TestPPTXDetectionAndConvert(t *testing.T) {
	deck := pptxSpecimen()
	doc, err := OpenBytes(deck)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if doc.Format() != FormatPPTX {
		t.Errorf("detected format = %q, want pptx", doc.Format())
	}

	var md bytes.Buffer
	if err := Convert(context.Background(), bytes.NewReader(deck), &md, ConvertOptions{To: FormatMarkdown}); err != nil {
		t.Fatalf("Convert pptx→md: %v", err)
	}
	got := md.String()
	for _, want := range []string{
		"## Quarterly Review",
		"- Revenue grew",
		"1. First priority",
		"**Second priority**",
		// The colspan header cell expands by content duplication (the GFM
		// strategy the markdown writer documents).
		"| **Metric** | **Metric** |",
		"| Latency | 12ms |",
		"Placed by the layout placeholder",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "HIDDEN SLIDE CONTENT") {
		t.Error("hidden slide leaked into the conversion")
	}
	// The title precedes the body in reading order.
	if strings.Index(got, "Quarterly Review") > strings.Index(got, "Revenue grew") {
		t.Error("title should precede body content")
	}
}
