package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/omnidoc/pkg/internal/css"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout/cssbox"
)

// cellWithText builds a minimal table-cell box containing one text run at a known
// font size, styled enough for shaping. Width is explicitly set to UnitAuto to
// match the CSS initial value (the zero-value Length is UnitPx/0 which would
// spuriously pin intrinsic sizing to zero).
func cellWithText(s string) *cssbox.Box {
	st := gcss.ComputedStyle{
		FontSizePt: 16,
		FontFamily: "serif",
		Width:      gcss.Length{Unit: gcss.UnitAuto},
	}
	txt := &cssbox.Box{Kind: cssbox.BoxText, Text: s, Display: cssbox.DisplayInline, Style: st}
	return &cssbox.Box{
		Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
		Formatting: cssbox.InlineFC, Style: st, Children: []*cssbox.Box{txt},
	}
}

func TestMeasureMaxGEMin(t *testing.T) {
	e := New(nil, nil, nil)
	c := cellWithText("Hello world wide")
	mn := e.measureMinContent(context.Background(), c)
	mx := e.measureMaxContent(context.Background(), c)
	if mn <= 0 || mx <= 0 {
		t.Fatalf("non-positive measures min=%v max=%v", mn, mx)
	}
	if mx < mn {
		t.Fatalf("max-content (%v) < min-content (%v)", mx, mn)
	}
}

func TestMeasureMaxIsWholeString(t *testing.T) {
	e := New(nil, nil, nil)
	short := e.measureMaxContent(context.Background(), cellWithText("ab"))
	long := e.measureMaxContent(context.Background(), cellWithText("ab ab ab ab"))
	if long <= short {
		t.Fatalf("max-content should grow with no-wrap content: short=%v long=%v", short, long)
	}
}

func TestMeasureMinIsLongestWord(t *testing.T) {
	e := New(nil, nil, nil)
	a := e.measureMinContent(context.Background(), cellWithText("hi hi hi"))
	b := e.measureMinContent(context.Background(), cellWithText("hi"))
	if a != b {
		t.Fatalf("min-content changed with more short words: %v vs %v", a, b)
	}
	wide := e.measureMinContent(context.Background(), cellWithText("hi enormouslylongword"))
	if wide <= b {
		t.Fatalf("min-content should reflect the longest word: %v vs %v", wide, b)
	}
}

// cellWithTextBreak is cellWithText with the two mid-word-breaking properties set on
// both the cell and its text child (they are inherited, and the text box carries the
// style the run is built from).
func cellWithTextBreak(s, overflowWrap, wordBreak string) *cssbox.Box {
	c := cellWithText(s)
	c.Style.OverflowWrap, c.Style.WordBreak = overflowWrap, wordBreak
	for _, ch := range c.Children {
		ch.Style.OverflowWrap, ch.Style.WordBreak = overflowWrap, wordBreak
	}
	return c
}

// TestMinContentDistinguishesBreakWordFromAnywhere is the single observable difference
// between `overflow-wrap: break-word` and `overflow-wrap: anywhere` (CSS Text 3 §5.5):
// both break lines identically, but only `anywhere` shrinks intrinsic min-content — so a
// flex item or table cell sized from its content stays word-wide under break-word and
// collapses toward one character under anywhere.
//
// Getting this wrong is invisible in ordinary block layout and only shows up as a
// mis-sized track, which is why it is asserted directly rather than through a golden.
func TestMinContentDistinguishesBreakWordFromAnywhere(t *testing.T) {
	const text = "hi enormouslylongword"
	e := New(nil, nil, nil)

	plain := e.measureMinContent(context.Background(), cellWithTextBreak(text, "normal", "normal"))
	breakWord := e.measureMinContent(context.Background(), cellWithTextBreak(text, "break-word", "normal"))
	anywhere := e.measureMinContent(context.Background(), cellWithTextBreak(text, "anywhere", "normal"))
	breakAll := e.measureMinContent(context.Background(), cellWithTextBreak(text, "normal", "break-all"))

	if breakWord != plain {
		t.Errorf("break-word min-content = %v, want %v (unchanged) — break-word must NOT "+
			"affect intrinsic sizing", breakWord, plain)
	}
	if anywhere >= plain {
		t.Errorf("anywhere min-content = %v, want < %v — anywhere MUST shrink min-content",
			anywhere, plain)
	}
	if breakAll >= plain {
		t.Errorf("break-all min-content = %v, want < %v — break-all MUST shrink min-content",
			breakAll, plain)
	}
	// Under anywhere/break-all every cluster is its own unit, so min-content collapses to
	// the widest single character — far below the longest word.
	if anywhere <= 0 {
		t.Errorf("anywhere min-content = %v, want > 0", anywhere)
	}
}

// TestMaxContentUnaffectedByMidWordBreaking: max-content is the unwrapped width, so no
// breaking property may change it.
func TestMaxContentUnaffectedByMidWordBreaking(t *testing.T) {
	const text = "hi enormouslylongword"
	e := New(nil, nil, nil)
	plain := e.measureMaxContent(context.Background(), cellWithTextBreak(text, "normal", "normal"))
	for _, tc := range []struct{ ow, wb string }{
		{"break-word", "normal"}, {"anywhere", "normal"}, {"normal", "break-all"}, {"normal", "keep-all"},
	} {
		got := e.measureMaxContent(context.Background(), cellWithTextBreak(text, tc.ow, tc.wb))
		if got != plain {
			t.Errorf("overflow-wrap:%s word-break:%s max-content = %v, want %v (unchanged)",
				tc.ow, tc.wb, got, plain)
		}
	}
}

// TestMeasureContentCacheHit proves the per-box memo is consulted (not recomputed each
// call): after measuring a box, mutating its content and re-measuring on the SAME engine
// returns the CACHED (pre-mutation) width, while a FRESH engine sees the new width. The
// box tree is immutable during a real layout, so this stale read can't occur in practice
// — it is purely a probe that the cache fires. (If the cache were a no-op, the same-engine
// re-measure would pick up the mutation and the two values would match.)
func TestMeasureContentCacheHit(t *testing.T) {
	e := New(nil, nil, nil)
	c := cellWithText("ab")
	first := e.measureMaxContent(context.Background(), c)

	// Mutate the cell's text to something much wider.
	c.Children[0].Text = "ab ab ab ab ab ab ab ab"
	cachedAgain := e.measureMaxContent(context.Background(), c) // same engine → cache hit
	if cachedAgain != first {
		t.Errorf("same-engine re-measure = %v, want the cached %v (cache not consulted)", cachedAgain, first)
	}

	// A fresh engine has no cache entry and must see the wider, post-mutation width.
	fresh := New(nil, nil, nil).measureMaxContent(context.Background(), c)
	if fresh <= first {
		t.Errorf("fresh-engine measure = %v, want > %v (the mutated, wider content)", fresh, first)
	}
}

// TestMeasureContentCachePopulatesBothSizes confirms a box's cache entry records min and
// max independently (each guarded by its own set flag) and that both are returned
// consistently across repeated calls.
func TestMeasureContentCachePopulatesBothSizes(t *testing.T) {
	e := New(nil, nil, nil)
	c := cellWithText("alpha beta")
	mn1 := e.measureMinContent(context.Background(), c)
	mx1 := e.measureMaxContent(context.Background(), c)
	entry := e.measures[c]
	if entry == nil || !entry.minSet || !entry.maxSet {
		t.Fatalf("cache entry should have both min and max set, got %+v", entry)
	}
	// Repeated calls return the same cached values.
	if mn2 := e.measureMinContent(context.Background(), c); mn2 != mn1 {
		t.Errorf("cached min drifted: %v vs %v", mn2, mn1)
	}
	if mx2 := e.measureMaxContent(context.Background(), c); mx2 != mx1 {
		t.Errorf("cached max drifted: %v vs %v", mx2, mx1)
	}
}
