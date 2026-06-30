package css

import (
	"context"
	"testing"
)

// wrappedParagraph builds a body with a leading spacer block of height spacerPx, then a
// paragraph (width wPx, line-height lhPx) holding `words` copies of "word " so it wraps
// to several lines. The spacer pushes the paragraph down so a page boundary falls inside
// it.
func wrappedParagraph(spacerPx, wPx, lhPx, words int) string {
	para := ""
	for i := 0; i < words; i++ {
		para += "word "
	}
	return `<html><body>` +
		`<div style="height:` + itoa(spacerPx) + `px;margin:0">s</div>` +
		`<p style="margin:0;width:` + itoa(wPx) + `px;font-size:16px;line-height:` + itoa(lhPx) + `px">` +
		para + `</p>` +
		`</body></html>`
}

// TestWidowsOrphansSplitsParagraph drives the line splitter through bucketBlocks with a
// real wrapped paragraph straddling a page boundary, asserting the paragraph is split
// (its head on page 0, a tail on page 1) and both pieces honor the 2-line minimums.
func TestWidowsOrphansSplitsParagraph(t *testing.T) {
	const w = 400
	// Spacer 60pt, then a 6-line paragraph (line-height 20 ⇒ 120pt tall) starting at
	// y≈60. Page height 140 ⇒ first page fits the spacer + ~4 lines of the paragraph.
	src := wrappedParagraph(60, 120, 20, 18)
	blocks := measuredBlockHeights(t, src, w)
	if len(blocks) != 2 {
		t.Fatalf("want 2 top-level blocks (spacer, paragraph), got %d", len(blocks))
	}
	para := blocks[1]
	if len(para.Lines) < 5 {
		t.Fatalf("paragraph should wrap to several lines, got %d", len(para.Lines))
	}
	nLines := len(para.Lines)
	t.Logf("paragraph: %d lines, Y=%.1f H=%.1f", nLines, para.Y, para.H)

	const pageH = 140
	buckets := bucketBlocks(blocks, pageH, w, nolog)
	if len(buckets) < 2 {
		t.Fatalf("expected the paragraph to split across ≥2 pages, got %d page(s)", len(buckets))
	}

	// Count paragraph-derived line fragments per page (head + tail are clones of para,
	// identified by the paragraph's distinctive 120px width — the spacer is full-width).
	// Sum across all pages == original line count (no line lost or duplicated).
	isParaPiece := func(b *Fragment) bool { return len(b.Lines) > 0 && b.W < 200 }
	total := 0
	headLines, tailLines := 0, 0
	for pi, bk := range buckets {
		for _, b := range bk.blocks {
			if isParaPiece(b) {
				total += len(b.Lines)
				if pi == 0 {
					headLines = len(b.Lines)
				} else if tailLines == 0 {
					tailLines = len(b.Lines)
				}
			}
		}
	}
	if total != nLines {
		t.Errorf("line conservation: pages hold %d paragraph lines, original had %d", total, nLines)
	}
	if headLines < 2 {
		t.Errorf("orphans: head has %d lines, want ≥2", headLines)
	}
	if tailLines < 2 {
		t.Errorf("widows: tail has %d lines, want ≥2", tailLines)
	}
}

// TestWidowsParagraphSplitsAcrossThreePages proves the splitter is iterative: a very
// tall paragraph (more lines than two pages hold) fragments across three pages.
func TestWidowsParagraphSplitsAcrossThreePages(t *testing.T) {
	const w = 400
	// No spacer; a ~15-line paragraph (line-height 20 ⇒ 300pt). Page height 120 holds
	// ~6 lines, so it needs 3 pages.
	src := wrappedParagraph(0, 120, 20, 45)
	blocks := measuredBlockHeights(t, src, w)
	para := blocks[len(blocks)-1]
	if len(para.Lines) < 12 {
		t.Fatalf("want a tall paragraph (≥12 lines), got %d", len(para.Lines))
	}
	buckets := bucketBlocks(blocks, 120, w, nolog)
	if len(buckets) < 3 {
		t.Errorf("tall paragraph should split across ≥3 pages, got %d", len(buckets))
	}
	// Every page after page 0 starts with a paragraph tail whose top edge is suppressed
	// (so its content sits flush at the page top after the shift).
	for pi := 1; pi < len(buckets); pi++ {
		lead := buckets[pi].blocks[0]
		if len(lead.Lines) == 0 {
			t.Errorf("page %d should lead with a paragraph tail (has lines)", pi)
		}
	}
}

var _ = context.Background
