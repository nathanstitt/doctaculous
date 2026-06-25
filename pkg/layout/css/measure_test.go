package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
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
