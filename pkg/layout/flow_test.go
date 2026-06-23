package layout

import (
	"context"
	"image/color"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/box"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

// letterPage is an 8.5x11in page with 1in margins, in points.
func letterPage() box.PageGeometry {
	return box.PageGeometry{
		WidthPt: 612, HeightPt: 792,
		MarginTopPt: 72, MarginBottomPt: 72, MarginLeftPt: 72, MarginRightPt: 72,
	}
}

func textBlock(text string, sizePt float64, align box.Align) box.Block {
	return box.Block{
		Align:      align,
		LineHeight: box.LineHeight{Mode: box.LineHeightAuto, Mult: 1.15},
		Inlines: []box.Inline{{
			Text:   text,
			Face:   box.FaceRef{Family: "Arial"},
			SizePt: sizePt,
			Color:  color.RGBA{A: 0xff},
		}},
	}
}

func TestLayoutSingleParagraphOnePage(t *testing.T) {
	eng := New(layoutfont.NewFaceCache(), nil)
	doc := box.Document{Page: letterPage(), Blocks: []box.Block{
		textBlock("Hello world, this is a short paragraph.", 12, box.AlignLeft),
	}}
	pages, err := eng.Layout(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(pages.Pages))
	}
	if n := countGlyphs(pages.Pages[0]); n == 0 {
		t.Fatal("page has no glyphs")
	}
	assertWithinContent(t, pages.Pages[0], letterPage())
}

func TestLayoutLongDocumentPaginates(t *testing.T) {
	eng := New(layoutfont.NewFaceCache(), nil)
	// Enough paragraphs to overflow a Letter page at 12pt.
	para := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 6)
	var blocks []box.Block
	for i := 0; i < 40; i++ {
		blocks = append(blocks, textBlock(para, 12, box.AlignLeft))
	}
	doc := box.Document{Page: letterPage(), Blocks: blocks}
	pages, err := eng.Layout(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages.Pages) < 2 {
		t.Fatalf("pages = %d, want >= 2 (should paginate)", len(pages.Pages))
	}
	for i, p := range pages.Pages {
		if countGlyphs(p) == 0 {
			t.Errorf("page %d is empty", i)
		}
		assertWithinContent(t, p, letterPage())
	}
}

func TestLayoutPageBreakBefore(t *testing.T) {
	eng := New(layoutfont.NewFaceCache(), nil)
	b1 := textBlock("First page.", 12, box.AlignLeft)
	b2 := textBlock("Second page.", 12, box.AlignLeft)
	b2.BreakBefore = true
	doc := box.Document{Page: letterPage(), Blocks: []box.Block{b1, b2}}
	pages, err := eng.Layout(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("pages = %d, want 2 (explicit break)", len(pages.Pages))
	}
}

func TestLayoutAlignmentShiftsX(t *testing.T) {
	eng := New(layoutfont.NewFaceCache(), nil)
	page := letterPage()
	leftDoc := box.Document{Page: page, Blocks: []box.Block{textBlock("word", 12, box.AlignLeft)}}
	rightDoc := box.Document{Page: page, Blocks: []box.Block{textBlock("word", 12, box.AlignRight)}}

	lp, _ := eng.Layout(context.Background(), leftDoc)
	rp, _ := eng.Layout(context.Background(), rightDoc)

	leftX := firstGlyphX(lp.Pages[0])
	rightX := firstGlyphX(rp.Pages[0])
	if !(rightX > leftX) {
		t.Errorf("right-aligned first glyph x=%v should exceed left-aligned x=%v", rightX, leftX)
	}
}

func TestLayoutCancellation(t *testing.T) {
	eng := New(layoutfont.NewFaceCache(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doc := box.Document{Page: letterPage(), Blocks: []box.Block{textBlock("x", 12, box.AlignLeft)}}
	if _, err := eng.Layout(ctx, doc); err == nil {
		t.Error("want cancellation error")
	}
}

// TestLayoutOversizedFirstLineIndent guards the firstW clamp: a first-line
// indent wider than the content area must not drive the line-breaker to put one
// word per line (which would balloon the page count). The paragraph should still
// break greedily on the available width.
func TestLayoutOversizedFirstLineIndent(t *testing.T) {
	eng := New(layoutfont.NewFaceCache(), nil)
	b := textBlock(strings.Repeat("word ", 30), 12, box.AlignLeft)
	// Content width is 612-144=468pt; a 1000pt first-line indent makes firstW
	// negative before the clamp.
	b.FirstLinePt = 1000
	doc := box.Document{Page: letterPage(), Blocks: []box.Block{b}}
	pages, err := eng.Layout(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages.Pages) != 1 {
		t.Fatalf("pages = %d, want 1 (30 short words must not spill across pages)", len(pages.Pages))
	}
	// 30 words fitting ~10 per 468pt line at 12pt is a handful of lines, not 30.
	// Use distinct baselines as a proxy for line count.
	lines := map[float64]struct{}{}
	for _, it := range pages.Pages[0].Items {
		if it.Kind == GlyphKind {
			lines[it.Glyph.YPt] = struct{}{}
		}
	}
	if len(lines) > 10 {
		t.Errorf("paragraph broke into %d lines; clamp should keep it to a few (one-word-per-line regression)", len(lines))
	}
}

// --- helpers ---

func countGlyphs(p Page) int {
	n := 0
	for _, it := range p.Items {
		if it.Kind == GlyphKind {
			n++
		}
	}
	return n
}

func firstGlyphX(p Page) float64 {
	for _, it := range p.Items {
		if it.Kind == GlyphKind {
			return it.Glyph.XPt
		}
	}
	return -1
}

func assertWithinContent(t *testing.T, p Page, g box.PageGeometry) {
	t.Helper()
	top := g.MarginTopPt
	bottom := g.HeightPt - g.MarginBottomPt
	for _, it := range p.Items {
		if it.Kind != GlyphKind {
			continue
		}
		// The baseline must sit within (a small slack above the bottom for descenders
		// is fine; assert the baseline itself is above the content bottom).
		if it.Glyph.YPt < top || it.Glyph.YPt > bottom+2 {
			t.Errorf("glyph baseline y=%v outside content band [%v,%v]", it.Glyph.YPt, top, bottom)
			return
		}
	}
}
