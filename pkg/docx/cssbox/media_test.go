package cssbox

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/docx"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func TestMediaLoaderResolvesRelIDToBytes(t *testing.T) {
	d := &docx.Document{
		Rels:  map[string]docx.Relationship{"rId7": {ID: "rId7", Target: "word/media/image1.png"}},
		Media: map[string][]byte{"word/media/image1.png": []byte("PNGDATA")},
	}
	loader := MediaLoader(d)
	got, _, err := loader.Load(context.Background(), "rId7")
	if err != nil {
		t.Fatalf("Load(rId7): %v", err)
	}
	if string(got) != "PNGDATA" {
		t.Fatalf("Load(rId7) = %q, want PNGDATA", got)
	}
}

func TestLowerDrawingToReplacedImage(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Rels:    map[string]docx.Relationship{"rId7": {ID: "rId7", Target: "word/media/image1.png"}},
		Media:   map[string][]byte{"word/media/image1.png": []byte("PNGDATA")},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{Content: []docx.ParaChild{
			{Drawing: &docx.Drawing{RelID: "rId7", WidthEMU: 914400, HeightEMU: 457200}},
		}}}},
	}
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	para := body.Children[0]
	if len(para.Children) != 1 {
		t.Fatalf("paragraph children = %d, want 1 image", len(para.Children))
	}
	img := para.Children[0]
	if img.Kind != lcssbox.BoxReplaced || img.Replaced == nil {
		t.Fatalf("image box = %+v, want BoxReplaced", img)
	}
	if img.Replaced.Attrs["src"] != "rId7" {
		t.Fatalf("image src = %q, want rId7", img.Replaced.Attrs["src"])
	}
	// 914400 EMU = 72pt width, 457200 = 36pt height.
	if img.Replaced.Attrs["width"] != "72" || img.Replaced.Attrs["height"] != "36" {
		t.Fatalf("image size attrs = %q x %q, want 72 x 36", img.Replaced.Attrs["width"], img.Replaced.Attrs["height"])
	}
}

// TestDrawingBoxIgnoresParagraphIndent guards against the double-indent bug: an
// image in an indented paragraph must NOT inherit the paragraph's block indent as
// its own inline-block margins (the paragraph margin already positions the line box;
// re-applying it to the atom would indent the image twice).
func TestDrawingBoxIgnoresParagraphIndent(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Rels:    map[string]docx.Relationship{"rId7": {ID: "rId7", Target: "word/media/image1.png"}},
		Media:   map[string][]byte{"word/media/image1.png": []byte("PNGDATA")},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{
			Props: docx.ParagraphProps{IndentLeft: 720, HasIndentLeft: true},
			Content: []docx.ParaChild{
				{Drawing: &docx.Drawing{RelID: "rId7", WidthEMU: 914400, HeightEMU: 457200}},
			},
		}}},
	}
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	para := body.Children[0]
	if len(para.Children) != 1 {
		t.Fatalf("paragraph children = %d, want 1 image", len(para.Children))
	}
	img := para.Children[0]
	if img.Kind != lcssbox.BoxReplaced || img.Replaced == nil {
		t.Fatalf("image box = %+v, want BoxReplaced", img)
	}
	// The image's own margin must be the CSS initial (zero) value, NOT the 720-twip
	// (0.5in = 36pt) paragraph indent.
	if img.Style.MarginLeft != (gcss.Length{}) {
		t.Fatalf("image MarginLeft = %+v, want zero (initial); paragraph indent leaked", img.Style.MarginLeft)
	}
}
