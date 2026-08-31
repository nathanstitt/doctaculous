package css

import (
	"context"
	"strings"
	"testing"
)

// blocksWithStyles builds N stacked divs of height h px, applying per-index extra
// inline style (e.g. a break-after) so a test can attach break hints to specific
// blocks. styles[i] is appended to block i's style attribute.
func blocksWithStyles(h int, styles ...string) string {
	var b strings.Builder
	b.WriteString(`<html><body>`)
	for i, extra := range styles {
		_ = i
		b.WriteString(`<div style="height:` + itoa(h) + `px;margin:0;` + extra + `">x</div>`)
	}
	return b.String() + `</body></html>`
}

// TestBreakAfterAvoidKeepsPair: three equal blocks, page height fits ~2.5 of them.
// Without a hint, blocks 0+1 share page 0 and block 2 is alone on page 1. With
// break-after: avoid on block 1 (binding 1→2), a break between 1 and 2 is discouraged,
// so block 1 is carried to page 1 to stay with block 2: page 0 = {0}, page 1 = {1,2}.
func TestBreakAfterAvoidKeepsPair(t *testing.T) {
	const w = 400
	// Plain version to measure block height.
	plain := blocksWithStyles(100, "", "", "")
	blocks := measuredBlockHeights(t, plain, w)
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(blocks))
	}
	bh := blocks[0].H
	pageH := 2*bh + bh/2 // fits 2 blocks, not 3

	// Sanity: without the hint, bucketing is {0,1},{2}.
	if got := bucketBlocks(blocks, pageH, w, nolog); len(got) != 2 ||
		len(got[0].blocks) != 2 || len(got[1].blocks) != 1 {
		t.Fatalf("baseline (no hint) buckets wrong: %d pages, sizes %v", len(got), bucketSizes(got))
	}

	// With break-after: avoid on block 1.
	hinted := blocksWithStyles(100, "", "break-after:avoid", "")
	hb := measuredBlockHeights(t, hinted, w)
	got := bucketBlocks(hb, pageH, w, nolog)
	if len(got) != 2 {
		t.Fatalf("hinted: want 2 pages, got %d (sizes %v)", len(got), bucketSizes(got))
	}
	if len(got[0].blocks) != 1 || got[0].blocks[0] != hb[0] {
		t.Errorf("page 0 should hold only block 0; got %d blocks", len(got[0].blocks))
	}
	if len(got[1].blocks) != 2 || got[1].blocks[0] != hb[1] || got[1].blocks[1] != hb[2] {
		t.Errorf("page 1 should hold blocks 1+2 (kept together); got %d blocks", len(got[1].blocks))
	}
}

// TestBreakBeforeAvoidKeepsPair: the same keep, expressed as break-before: avoid on
// block 2 (binding 1→2 from b's side).
func TestBreakBeforeAvoidKeepsPair(t *testing.T) {
	const w = 400
	blocks := measuredBlockHeights(t, blocksWithStyles(100, "", "", ""), w)
	bh := blocks[0].H
	pageH := 2*bh + bh/2

	hinted := blocksWithStyles(100, "", "", "break-before:avoid")
	hb := measuredBlockHeights(t, hinted, w)
	got := bucketBlocks(hb, pageH, w, nolog)
	if len(got) != 2 || len(got[0].blocks) != 1 || len(got[1].blocks) != 2 {
		t.Fatalf("break-before:avoid keep wrong: %d pages, sizes %v", len(got), bucketSizes(got))
	}
}

// TestBreakAvoidDroppedWhenPairTooTall: if the avoid-bound pair is itself taller than a
// page, the avoid is dropped and the plain overflow break stands (page 0 = {0,1},
// page 1 = {2}). Here each block is ~0.6 page tall, so two of them exceed a page.
func TestBreakAvoidDroppedWhenPairTooTall(t *testing.T) {
	const w = 400
	blocks := measuredBlockHeights(t, blocksWithStyles(100, "", "", ""), w)
	bh := blocks[0].H
	pageH := bh + bh*0.7 // fits 1 block but not 2 (a pair can't fit)

	hinted := blocksWithStyles(100, "", "break-after:avoid", "")
	hb := measuredBlockHeights(t, hinted, w)
	got := bucketBlocks(hb, pageH, w, nolog)
	// Each block alone fits; pairs don't. break-after:avoid on block 1 cannot be
	// honored (1+2 too tall), so it breaks normally: {0},{1},{2}.
	if len(got) != 3 {
		t.Fatalf("want 3 pages (avoid dropped, each block alone), got %d (sizes %v)", len(got), bucketSizes(got))
	}
}

// TestBreakAvoidChainOfThree: blocks 1,2,3 are avoid-chained (1 break-after:avoid,
// 2 break-after:avoid). A boundary that would split between 2 and 3 must carry the whole
// 1-2-3 chain to the next page when it fits.
func TestBreakAvoidChainOfThree(t *testing.T) {
	const w = 400
	blocks := measuredBlockHeights(t, blocksWithStyles(60, "", "break-after:avoid", "break-after:avoid", ""), w)
	if len(blocks) != 4 {
		t.Fatalf("want 4 blocks, got %d", len(blocks))
	}
	bh := blocks[0].H
	// Page fits ~3.5 blocks; without keep, page 0 = {0,1,2}, page 1 = {3}. Block 3 is
	// chained to 2 (and 2 to 1), so the chain 1-2-3 (3 blocks) moves together: page 0 =
	// {0}, page 1 = {1,2,3}.
	pageH := 3*bh + bh/2
	got := bucketBlocks(blocks, pageH, w, nolog)
	if len(got) != 2 || len(got[0].blocks) != 1 || len(got[1].blocks) != 3 {
		t.Fatalf("chain keep wrong: %d pages sizes %v, want {1},{3}", len(got), bucketSizes(got))
	}
}

func nolog(string, ...any) {}

func bucketSizes(bs []pageBucket) []int {
	out := make([]int, len(bs))
	for i := range bs {
		out[i] = len(bs[i].blocks)
	}
	return out
}

// keep context import used (buildRoot/measuredBlockHeights take it indirectly).
var _ = context.Background
