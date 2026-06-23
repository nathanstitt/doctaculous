package lower

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	"github.com/nathanstitt/doctaculous/pkg/layout/box"
)

func TestLowerPageGeometry(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{
			PageW: 12240, PageH: 15840, // Letter
			MarginTop: 1440, MarginBottom: 1440, MarginLeft: 1440, MarginRight: 1440,
		},
	}
	bd := Document(d, style.NewResolver(d, nil))
	if bd.Page.WidthPt != 612 || bd.Page.HeightPt != 792 {
		t.Errorf("page = %vx%v pt, want 612x792", bd.Page.WidthPt, bd.Page.HeightPt)
	}
	if bd.Page.MarginLeftPt != 72 {
		t.Errorf("left margin = %v pt, want 72", bd.Page.MarginLeftPt)
	}
}

func TestLowerParagraphInlinesAndAlign(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{
			Props: docx.ParagraphProps{Justify: docx.JustifyCenter, HasJustify: true},
			Runs: []docx.Run{
				{Text: "Hello ", Props: docx.RunProps{Bold: true, HasBold: true, SizeHalfPts: 24, HasSize: true}},
				{Text: "world", Props: docx.RunProps{}},
			},
		}}},
	}
	bd := Document(d, style.NewResolver(d, nil))
	if len(bd.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(bd.Blocks))
	}
	blk := bd.Blocks[0]
	if blk.Align != box.AlignCenter {
		t.Errorf("align = %v, want center", blk.Align)
	}
	if len(blk.Inlines) != 2 {
		t.Fatalf("inlines = %d, want 2", len(blk.Inlines))
	}
	if blk.Inlines[0].Text != "Hello " || !blk.Inlines[0].Face.Bold {
		t.Errorf("inline 0 = %+v, want bold 'Hello '", blk.Inlines[0])
	}
	if blk.Inlines[0].SizePt != 12 {
		t.Errorf("inline 0 size = %v, want 12pt (24 half-points)", blk.Inlines[0].SizePt)
	}
}

func TestLowerPageBreakSplitsBlocks(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{
			Runs: []docx.Run{
				{Text: "before"},
				{Break: docx.BreakPage},
				{Text: "after"},
			},
		}}},
	}
	bd := Document(d, style.NewResolver(d, nil))
	if len(bd.Blocks) != 2 {
		t.Fatalf("blocks = %d, want 2 (split on page break)", len(bd.Blocks))
	}
	if !bd.Blocks[1].BreakBefore {
		t.Error("second block should carry BreakBefore")
	}
	if bd.Blocks[0].Inlines[0].Text != "before" || bd.Blocks[1].Inlines[0].Text != "after" {
		t.Error("page break did not split text correctly")
	}
}
