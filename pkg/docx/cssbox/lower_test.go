package cssbox

import (
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// bodyOf returns the body wrapper (root.Children[0]) that holds the paragraph
// blocks. Lower nests the paragraphs under a body block (the <body> analogue) so the
// CSS paged engine's bodyFragment lookup (root.Children[last]) finds the top-level
// block list; the tests reach through it to the paragraph blocks.
func bodyOf(root *lcssbox.Box) *lcssbox.Box {
	if root == nil || len(root.Children) == 0 {
		return &lcssbox.Box{}
	}
	return root.Children[len(root.Children)-1]
}

func TestLowerNilYieldsEmptyBlockRoot(t *testing.T) {
	root := Lower(nil, nil)
	if root == nil {
		t.Fatal("Lower(nil, nil) = nil, want non-nil root")
	}
	if root.Kind != lcssbox.BoxBlock {
		t.Errorf("root.Kind = %v, want BoxBlock", root.Kind)
	}
	// The root wraps a single (empty) body block; the body carries no paragraphs.
	if len(root.Children) != 1 {
		t.Fatalf("root has %d children, want 1 (the body wrapper)", len(root.Children))
	}
	if body := bodyOf(root); len(body.Children) != 0 {
		t.Errorf("body has %d children, want 0", len(body.Children))
	}
}

func TestGeometry(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{
			PageW: 12240, PageH: 15840, // Letter
			MarginTop: 1440, MarginBottom: 1440, MarginLeft: 1440, MarginRight: 1440,
		},
	}
	g := Geometry(d)
	if got := g.ContentWidthPt(); got != 468 {
		t.Errorf("ContentWidthPt() = %v, want 468", got)
	}
	if got := g.ContentHeightPt(); got != 648 {
		t.Errorf("ContentHeightPt() = %v, want 648", got)
	}
	if g.PageHeightPt != 792 {
		t.Errorf("PageHeightPt = %v, want 792", g.PageHeightPt)
	}

	root := Lower(d, style.NewResolver(d, nil))
	if root == nil || root.Kind != lcssbox.BoxBlock {
		t.Errorf("Lower(d, r) root = %+v, want BoxBlock root", root)
	}
}

func TestLowerCenteredBoldRun(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{
			Props: docx.ParagraphProps{Justify: docx.JustifyCenter, HasJustify: true},
			Runs: []docx.Run{
				{Text: "Hi", Props: docx.RunProps{Bold: true, HasBold: true}},
			},
		}}},
	}
	body := bodyOf(Lower(d, style.NewResolver(d, nil)))
	if len(body.Children) != 1 {
		t.Fatalf("body children = %d, want 1", len(body.Children))
	}
	blk := body.Children[0]
	if blk.Kind != lcssbox.BoxBlock {
		t.Errorf("block kind = %v, want BoxBlock", blk.Kind)
	}
	if blk.Formatting != lcssbox.InlineFC {
		t.Errorf("block formatting = %v, want InlineFC", blk.Formatting)
	}
	if blk.Style.TextAlign != "center" {
		t.Errorf("text-align = %q, want center", blk.Style.TextAlign)
	}
	if len(blk.Children) != 1 {
		t.Fatalf("block children = %d, want 1", len(blk.Children))
	}
	tb := blk.Children[0]
	if tb.Kind != lcssbox.BoxText || tb.Text != "Hi" {
		t.Errorf("text box = %+v, want BoxText 'Hi'", tb)
	}
	if !tb.Style.Bold {
		t.Error("text box should be bold")
	}
	if tb.Display != lcssbox.DisplayInline {
		t.Errorf("text box display = %v, want inline", tb.Display)
	}
}

// TestLowerRunPropsMapEndToEnd locks the full run-property mapping onto the text
// box's ComputedStyle: family, size (half-points → points), italic, underline, and
// color. Bold is covered by TestLowerCenteredBoldRun. Without this, only bold was
// asserted and italic/underline/color/family/size could silently stop mapping.
func TestLowerRunPropsMapEndToEnd(t *testing.T) {
	red := color.RGBA{R: 0xC0, G: 0x10, B: 0x20, A: 0xFF}
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{
			Runs: []docx.Run{{Text: "styled", Props: docx.RunProps{
				Italic: true, HasItalic: true,
				Underline: true, HasUnderline: true,
				SizeHalfPts: 28, HasSize: true, // 28 half-points = 14pt
				Color: red, HasColor: true,
				Family: "Times New Roman",
			}}},
		}}},
	}
	body := bodyOf(Lower(d, style.NewResolver(d, nil)))
	tb := body.Children[0].Children[0]
	if tb.Kind != lcssbox.BoxText || tb.Text != "styled" {
		t.Fatalf("text box = %+v, want BoxText 'styled'", tb)
	}
	if !tb.Style.Italic {
		t.Error("italic not mapped")
	}
	if tb.Style.TextDecorationLine != "underline" {
		t.Errorf("text-decoration = %q, want underline", tb.Style.TextDecorationLine)
	}
	if tb.Style.FontSizePt != 14 {
		t.Errorf("font-size = %vpt, want 14 (28 half-points)", tb.Style.FontSizePt)
	}
	if tb.Style.Color != red {
		t.Errorf("color = %+v, want %+v", tb.Style.Color, red)
	}
	if tb.Style.FontFamily != "Times New Roman" {
		t.Errorf("font-family = %q, want Times New Roman", tb.Style.FontFamily)
	}
}

// TestLowerNoUnderlineIsNone confirms a run without underline resolves to the
// explicit "none" (not empty), so it can't accidentally inherit an underline.
func TestLowerNoUnderlineIsNone(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{
			Runs: []docx.Run{{Text: "plain"}},
		}}},
	}
	body := bodyOf(Lower(d, style.NewResolver(d, nil)))
	tb := body.Children[0].Children[0]
	if tb.Style.TextDecorationLine != "none" {
		t.Errorf("text-decoration = %q, want none", tb.Style.TextDecorationLine)
	}
}

func TestLowerHardBreak(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{
			Runs: []docx.Run{
				{Text: "a"},
				{Break: docx.BreakLine},
				{Text: "b"},
			},
		}}},
	}
	body := bodyOf(Lower(d, style.NewResolver(d, nil)))
	if len(body.Children) != 1 {
		t.Fatalf("body children = %d, want 1", len(body.Children))
	}
	blk := body.Children[0]
	if len(blk.Children) != 3 {
		t.Fatalf("block children = %d, want 3", len(blk.Children))
	}
	brk := blk.Children[1]
	if brk.Kind != lcssbox.BoxText || brk.Text != "\n" {
		t.Errorf("break box = %+v, want BoxText '\\n'", brk)
	}
	if brk.Style.WhiteSpace != "pre-line" {
		t.Errorf("break box white-space = %q, want pre-line", brk.Style.WhiteSpace)
	}
	if ws := blk.Children[0].Style.WhiteSpace; ws != "" && ws != "normal" {
		t.Errorf("text run white-space = %q, want normal (not pre-line)", ws)
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
	body := bodyOf(Lower(d, style.NewResolver(d, nil)))
	if len(body.Children) != 2 {
		t.Fatalf("body children = %d, want 2 (split on page break)", len(body.Children))
	}
	if body.Children[1].Style.BreakBefore != "page" {
		t.Errorf("second block BreakBefore = %q, want page", body.Children[1].Style.BreakBefore)
	}
	if body.Children[0].Children[0].Text != "before" || body.Children[1].Children[0].Text != "after" {
		t.Error("page break did not split text correctly")
	}
}

func TestLowerAutoLineHeightIsNotZero(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{
			Runs: []docx.Run{{Text: "x"}},
		}}},
	}
	body := bodyOf(Lower(d, style.NewResolver(d, nil)))
	if len(body.Children) != 1 {
		t.Fatalf("body children = %d, want 1", len(body.Children))
	}
	lh := body.Children[0].Style.LineHeight
	if lh.Unit != gcss.UnitAuto {
		t.Errorf("auto line-height unit = %v (value %v), want UnitAuto (guard against zero-value collapse)", lh.Unit, lh.Value)
	}
}

func TestLowerAlignmentMapping(t *testing.T) {
	cases := []struct {
		j    docx.Justify
		want string
	}{
		{docx.JustifyBoth, "justify"},
		{docx.JustifyRight, "right"},
		{docx.JustifyLeft, "left"},
		{docx.JustifyCenter, "center"},
	}
	for _, c := range cases {
		d := &docx.Document{
			Section: docx.SectionProps{PageW: 12240, PageH: 15840},
			Body: []docx.Block{{Paragraph: &docx.Paragraph{
				Props: docx.ParagraphProps{Justify: c.j, HasJustify: true},
				Runs:  []docx.Run{{Text: "x"}},
			}}},
		}
		body := bodyOf(Lower(d, style.NewResolver(d, nil)))
		if got := body.Children[0].Style.TextAlign; got != c.want {
			t.Errorf("justify %v → %q, want %q", c.j, got, c.want)
		}
	}
}
