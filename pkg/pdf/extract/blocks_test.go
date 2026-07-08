package extract

import "testing"

// mkLine builds a line at baseline y spanning [x0,x1] with the given size and a
// single synthetic word covering that extent, for block/order tests.
func mkLine(text string, x0, x1, y, size float64) line {
	return line{
		y: y, x0: x0, x1: x1, size: size,
		words: []word{{text: text, x0: x0, x1: x1, y: y, size: size}},
	}
}

// mkFusedLine builds a single line whose words span a left cluster and a right
// cluster separated by a wide (empty) gutter — the shape assembleLine produces
// when two columns share a baseline. Left words sit in [lx0,lx1], right words in
// [rx0,rx1]; the middle is empty.
func mkFusedLine(lt, rt string, lx0, lx1, rx0, rx1, y, size float64) line {
	return line{
		y: y, x0: lx0, x1: rx1, size: size,
		words: []word{
			{text: lt, x0: lx0, x1: lx1, y: y, size: size},
			{text: rt, x0: rx0, x1: rx1, y: y, size: size},
		},
	}
}

func TestOrderBlocksTwoColumns(t *testing.T) {
	// Left column at x=[0,200], right column at x=[300,500] with a wide gutter.
	// XY-cut must emit the entire left column before the right one.
	var lines []line
	lines = append(lines, mkLine("L1", 0, 200, 100, 10))
	lines = append(lines, mkLine("L2", 0, 200, 112, 10))
	lines = append(lines, mkLine("R1", 300, 500, 100, 10))
	lines = append(lines, mkLine("R2", 300, 500, 112, 10))

	blocks := orderBlocks(lines, 600, 800, 10)
	var order []string
	for _, b := range blocks {
		order = append(order, b.blockText())
	}
	// Left lines (grouped into one paragraph block) before right lines.
	if len(order) < 2 {
		t.Fatalf("got %d blocks, want >=2: %v", len(order), order)
	}
	if order[0] != "L1 L2" {
		t.Errorf("first block = %q, want left column %q", order[0], "L1 L2")
	}
	if order[len(order)-1] != "R1 R2" {
		t.Errorf("last block = %q, want right column %q", order[len(order)-1], "R1 R2")
	}
}

// TestOrderBlocksFusedColumns is the real-bug case: each baseline's left- and
// right-column words are fused into ONE line spanning the full width (as
// assembleLine produces). orderBlocks must split at the gutter and emit both
// left blocks (top-to-bottom) before either right block.
func TestOrderBlocksFusedColumns(t *testing.T) {
	// Two baselines, each a fused line: left [0,200], gutter (200,400), right [400,600].
	lines := []line{
		mkFusedLine("L1", "R1", 0, 200, 400, 600, 100, 10),
		mkFusedLine("L2", "R2", 0, 200, 400, 600, 112, 10),
	}
	blocks := orderBlocks(lines, 600, 800, 10)
	var order []string
	for _, b := range blocks {
		order = append(order, b.blockText())
	}
	if len(order) != 2 {
		t.Fatalf("got %d blocks, want 2 (one per column): %v", len(order), order)
	}
	if order[0] != "L1 L2" {
		t.Errorf("first block = %q, want left column %q read top-to-bottom", order[0], "L1 L2")
	}
	if order[1] != "R1 R2" {
		t.Errorf("second block = %q, want right column %q read top-to-bottom", order[1], "R1 R2")
	}
}

// TestSplitAtGuttersLeavesProseAlone is the NEGATIVE case: a single-column line
// with only normal inter-word spacing must NOT be split.
func TestSplitAtGuttersLeavesProseAlone(t *testing.T) {
	// Five words with ~2pt inter-word gaps at 10pt — ordinary prose, one column.
	l := line{
		y: 100, x0: 0, x1: 152, size: 10,
		words: []word{
			{text: "the", x0: 0, x1: 30, y: 100, size: 10},
			{text: "quick", x0: 32, x1: 62, y: 100, size: 10},
			{text: "brown", x0: 64, x1: 94, y: 100, size: 10},
			{text: "fox", x0: 96, x1: 120, y: 100, size: 10},
			{text: "jumps", x0: 122, x1: 152, y: 100, size: 10},
		},
	}
	got := splitAtGutters(l, 600)
	if len(got) != 1 {
		t.Fatalf("prose line split into %d sub-lines, want 1", len(got))
	}
	if got[0].text() != "the quick brown fox jumps" {
		t.Errorf("prose text changed: %q", got[0].text())
	}
	// And it must not be misordered when passed through orderBlocks.
	blocks := orderBlocks([]line{l}, 600, 800, 10)
	if len(blocks) != 1 || blocks[0].blockText() != "the quick brown fox jumps" {
		t.Errorf("prose line -> %d blocks %v, want 1 unchanged", len(blocks), blocks)
	}
}

func TestClassifyHeading(t *testing.T) {
	// A 24pt line over 10pt body is a heading; 24/10 = 2.4x => h1.
	head := mkLine("Title", 0, 100, 60, 24)
	body := mkLine("body text here", 0, 200, 100, 10)
	blocks := orderBlocks([]line{head, body}, 600, 800, 10)
	if len(blocks) < 2 {
		t.Fatalf("got %d blocks, want >=2", len(blocks))
	}
	if blocks[0].kind != blockHeading {
		t.Fatalf("first block kind = %v, want heading", blocks[0].kind)
	}
	if blocks[0].level != 1 {
		t.Errorf("heading level = %d, want 1", blocks[0].level)
	}
	if blocks[1].kind != blockParagraph {
		t.Errorf("second block kind = %v, want paragraph", blocks[1].kind)
	}
}

func TestClassifyBulletList(t *testing.T) {
	item := line{
		y: 100, x0: 0, x1: 100, size: 10,
		words: []word{
			{text: "•", x0: 0, x1: 5, y: 100, size: 10},
			{text: "first", x0: 10, x1: 40, y: 100, size: 10},
		},
	}
	blocks := orderBlocks([]line{item}, 600, 800, 10)
	if len(blocks) != 1 || blocks[0].kind != blockListItem {
		t.Fatalf("got %+v, want a single list item", blocks)
	}
	if blocks[0].marker != "- " {
		t.Errorf("marker = %q, want %q", blocks[0].marker, "- ")
	}
	if got := blocks[0].blockText(); got != "first" {
		t.Errorf("item text = %q, want %q (marker stripped)", got, "first")
	}
}

func TestClassifyOrderedList(t *testing.T) {
	item := line{
		y: 100, x0: 0, x1: 100, size: 10,
		words: []word{
			{text: "1.", x0: 0, x1: 8, y: 100, size: 10},
			{text: "step", x0: 12, x1: 40, y: 100, size: 10},
		},
	}
	blocks := orderBlocks([]line{item}, 600, 800, 10)
	if len(blocks) != 1 || blocks[0].kind != blockListItem {
		t.Fatalf("got %+v, want a single list item", blocks)
	}
	if blocks[0].marker != "1. " {
		t.Errorf("marker = %q, want %q", blocks[0].marker, "1. ")
	}
	if got := blocks[0].blockText(); got != "step" {
		t.Errorf("item text = %q, want %q", got, "step")
	}
}

func TestParagraphGapSplit(t *testing.T) {
	// Two lines with a small gap = one paragraph; a large gap = two paragraphs.
	tight := []line{
		mkLine("a", 0, 100, 100, 10),
		mkLine("b", 0, 100, 112, 10), // 12pt gap ~ 1.2em: same paragraph
	}
	if got := orderBlocks(tight, 600, 800, 10); len(got) != 1 {
		t.Errorf("tight lines -> %d blocks, want 1", len(got))
	}
	loose := []line{
		mkLine("a", 0, 100, 100, 10),
		mkLine("b", 0, 100, 140, 10), // 40pt gap: separate paragraphs
	}
	if got := orderBlocks(loose, 600, 800, 10); len(got) != 2 {
		t.Errorf("loose lines -> %d blocks, want 2", len(got))
	}
}

func TestListMarkerRejectsProse(t *testing.T) {
	// A word ending in "." that is long prose must not be taken as an ordered marker.
	l := mkLine("etc. more words", 0, 100, 100, 10)
	l.words = []word{
		{text: "etc.", x0: 0, x1: 20, y: 100, size: 10},
		{text: "more", x0: 24, x1: 44, y: 100, size: 10},
	}
	if _, _, ok := listMarker(l); ok {
		t.Errorf("listMarker matched prose token %q", "etc.")
	}
}
