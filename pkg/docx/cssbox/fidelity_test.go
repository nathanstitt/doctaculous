package cssbox

import (
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// lowerBody lowers a one-paragraph document and returns the body's blocks.
func lowerBody(t *testing.T, content []docx.ParaChild, props docx.ParagraphProps) []*lcssbox.Box {
	t.Helper()
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840},
		Body:    []docx.Block{{Paragraph: &docx.Paragraph{Props: props, Content: content}}},
	}
	return bodyOf(Lower(d, style.NewResolver(d, nil))).Children
}

// collectText concatenates every text box under a box tree.
func collectText(boxes []*lcssbox.Box) string {
	var sb strings.Builder
	var walk func(b *lcssbox.Box)
	walk = func(b *lcssbox.Box) {
		sb.WriteString(b.Text)
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range boxes {
		walk(b)
	}
	return sb.String()
}

// TestLowerRevisionsFinalState pins the tracked-changes rendering contract:
// insertion content renders in place, deletion content does not — Word's
// "No Markup" view, the document as it will be.
func TestLowerRevisionsFinalState(t *testing.T) {
	blocks := lowerBody(t, []docx.ParaChild{
		{Run: &docx.Run{Text: "kept "}},
		{Revision: &docx.Revision{Kind: docx.RevisionInsert, Content: []docx.ParaChild{
			{Run: &docx.Run{Text: "added "}},
			{Revision: &docx.Revision{Kind: docx.RevisionInsert, Content: []docx.ParaChild{
				{Run: &docx.Run{Text: "nested "}},
			}}},
		}}},
		{Revision: &docx.Revision{Kind: docx.RevisionDelete, Content: []docx.ParaChild{
			{Run: &docx.Run{Text: "REMOVED"}},
		}}},
		{Run: &docx.Run{Text: "end"}},
	}, docx.ParagraphProps{})
	got := collectText(blocks)
	if got != "kept added nested end" {
		t.Errorf("final-state text = %q, want %q", got, "kept added nested end")
	}
}

// TestLowerCommentsInvisible pins that comment machinery — range markers and
// the reference run — contributes nothing to rendered content.
func TestLowerCommentsInvisible(t *testing.T) {
	blocks := lowerBody(t, []docx.ParaChild{
		{CommentStart: &docx.CommentMark{ID: 1}},
		{Run: &docx.Run{Text: "flagged"}},
		{CommentEnd: &docx.CommentMark{ID: 1}},
		{Run: &docx.Run{CommentRef: 1}},
	}, docx.ParagraphProps{})
	if got := collectText(blocks); got != "flagged" {
		t.Errorf("text = %q, want just %q (comment anchors invisible)", got, "flagged")
	}
}

// TestLowerDropCapDegrades pins that a framed (drop-cap) paragraph lowers as a
// plain paragraph: same box shape as without the frame.
func TestLowerDropCapDegrades(t *testing.T) {
	content := []docx.ParaChild{{Run: &docx.Run{Text: "Drop"}}}
	plain := lowerBody(t, content, docx.ParagraphProps{})
	framed := lowerBody(t, content, docx.ParagraphProps{
		Frame: &docx.FramePr{DropCap: "drop", Lines: 3},
	})
	if len(plain) != len(framed) {
		t.Fatalf("framed paragraph lowers to %d blocks, plain to %d — drop cap must degrade to normal flow", len(framed), len(plain))
	}
	if collectText(framed) != collectText(plain) {
		t.Errorf("framed text %q differs from plain %q", collectText(framed), collectText(plain))
	}
}

// TestLowerEndnoteMarker pins the endnote in-text marker (same superscript
// mechanism as footnotes).
func TestLowerEndnoteMarker(t *testing.T) {
	blocks := lowerBody(t, []docx.ParaChild{
		{Run: &docx.Run{Text: "claim"}},
		{Run: &docx.Run{EndnoteRef: 4}},
	}, docx.ParagraphProps{})
	para := blocks[0]
	if len(para.Children) != 2 {
		t.Fatalf("paragraph children = %d, want text + marker", len(para.Children))
	}
	marker := para.Children[1]
	if marker.Text != "4" || marker.Style.VerticalAlign != "super" {
		t.Errorf("endnote marker = %q valign %q, want superscript '4'", marker.Text, marker.Style.VerticalAlign)
	}
}

// TestLowerRunShading pins run w:shd → text background (with a highlight
// painting over it when both are set).
func TestLowerRunShading(t *testing.T) {
	shd := docx.Shading{HasFill: true}
	shd.Fill.R, shd.Fill.G, shd.Fill.B, shd.Fill.A = 0xFF, 0xEE, 0x00, 0xFF
	blocks := lowerBody(t, []docx.ParaChild{
		{Run: &docx.Run{Text: "shaded", Props: docx.RunProps{Shd: shd}}},
	}, docx.ParagraphProps{})
	box := blocks[0].Children[0]
	if box.Style.BackgroundColor.R != 0xFF || box.Style.BackgroundColor.G != 0xEE {
		t.Errorf("background = %+v, want the run shd fill", box.Style.BackgroundColor)
	}
}

// TestLowerAnchoredDrawingFloats pins the anchored square-wrap drawing → CSS
// float lowering: a float block precedes the paragraph block.
func TestLowerAnchoredDrawingFloats(t *testing.T) {
	blocks := lowerBody(t, []docx.ParaChild{
		{Drawing: &docx.Drawing{RelID: "rId7", WidthEMU: 914400, HeightEMU: 914400,
			Anchored: true, WrapKind: "square", AlignH: "right"}},
		{Run: &docx.Run{Text: "wraps beside"}},
	}, docx.ParagraphProps{})
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want [float, paragraph]", len(blocks))
	}
	if blocks[0].Float != lcssbox.FloatRight || blocks[0].Replaced == nil {
		t.Errorf("first block = %+v, want a right-floated replaced box", blocks[0])
	}
	if collectText(blocks[1:]) != "wraps beside" {
		t.Errorf("paragraph text = %q", collectText(blocks[1:]))
	}

	// An inline (non-anchored) drawing stays inside the paragraph.
	inlineBlocks := lowerBody(t, []docx.ParaChild{
		{Drawing: &docx.Drawing{RelID: "rId7", WidthEMU: 914400, HeightEMU: 914400}},
	}, docx.ParagraphProps{})
	if len(inlineBlocks) != 1 || len(inlineBlocks[0].Children) != 1 {
		t.Errorf("inline drawing should stay in the paragraph: %+v", inlineBlocks)
	}
}

// TestLowerListStart pins that w:start / w:startOverride seed the list counter.
func TestLowerListStart(t *testing.T) {
	num := docx.NewNumbering()
	num.Abstract[0] = map[int]docx.NumLevel{
		0: {Format: docx.NumFmtDecimal, Text: "%1.", Start: 5, HasStart: true},
	}
	num.Instances[1] = docx.NumInstance{AbstractID: 0}
	num.Instances[2] = docx.NumInstance{AbstractID: 0, Overrides: map[int]docx.LevelOverride{0: {Start: 9, HasStart: true}}}

	li := func(numID int, text string) docx.Block {
		return docx.Block{Paragraph: &docx.Paragraph{
			Props:   docx.ParagraphProps{HasNum: true, NumID: numID},
			Content: []docx.ParaChild{{Run: &docx.Run{Text: text}}},
		}}
	}
	d := &docx.Document{
		Section:   docx.SectionProps{PageW: 12240, PageH: 15840},
		Numbering: num,
		Body:      []docx.Block{li(1, "a"), li(1, "b"), li(2, "c")},
	}
	root := Lower(d, style.NewResolver(d, nil))
	text := collectText(bodyOf(root).Children)
	for _, want := range []string{"5. a", "6. b", "9. c"} {
		if !strings.Contains(text, want) {
			t.Errorf("list text %q missing %q (start values must seed the counter)", text, want)
		}
	}
}
