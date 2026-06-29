package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// relBox builds a box with the given position offsets (auto where omitted) and a
// font size for em resolution. Offsets default to UnitAuto (CSS auto), NOT zero —
// the zero-value Length trap.
func relBox(top, right, bottom, left gcss.Length, fontSizePt float64) *cssbox.Box {
	auto := gcss.Length{Unit: gcss.UnitAuto}
	st := gcss.ComputedStyle{Position: "relative", Top: auto, Right: auto, Bottom: auto, Left: auto, FontSizePt: fontSizePt}
	if top.Unit != 0 || top.Value != 0 {
		st.Top = top
	}
	if right.Unit != 0 || right.Value != 0 {
		st.Right = right
	}
	if bottom.Unit != 0 || bottom.Value != 0 {
		st.Bottom = bottom
	}
	if left.Unit != 0 || left.Value != 0 {
		st.Left = left
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Style: st}
}

func px(v float64) gcss.Length { return gcss.Length{Value: v, Unit: gcss.UnitPx} }

// TestRelativeOffsetTopLeft: top/left produce a positive (down, right) shift.
func TestRelativeOffsetTopLeft(t *testing.T) {
	b := relBox(px(10), gcss.Length{}, gcss.Length{}, px(20), 16)
	dx, dy := relativeOffset(b, 200, 100)
	if dx != 20 || dy != 10 {
		t.Errorf("relativeOffset = (%v,%v), want (20,10)", dx, dy)
	}
}

// TestRelativeOffsetBottomRight: with top/left auto, bottom/right produce a
// negative (up, left) shift.
func TestRelativeOffsetBottomRight(t *testing.T) {
	b := relBox(gcss.Length{}, px(5), px(8), gcss.Length{}, 16)
	dx, dy := relativeOffset(b, 200, 100)
	if dx != -5 || dy != -8 {
		t.Errorf("relativeOffset = (%v,%v), want (-5,-8)", dx, dy)
	}
}

// TestRelativeOffsetTopWins: top wins over bottom, left wins over right (CSS 9.4.3).
func TestRelativeOffsetTopWins(t *testing.T) {
	b := relBox(px(10), px(5), px(8), px(20), 16)
	dx, dy := relativeOffset(b, 200, 100)
	if dx != 20 || dy != 10 {
		t.Errorf("over-constrained relativeOffset = (%v,%v), want (20,10) (left/top win)", dx, dy)
	}
}

// TestRelativeOffsetPercent: top% resolves against cbH, left% against cbW.
func TestRelativeOffsetPercent(t *testing.T) {
	pct := func(v float64) gcss.Length { return gcss.Length{Value: v, Unit: gcss.UnitPercent} }
	b := relBox(pct(10), gcss.Length{}, gcss.Length{}, pct(50), 16)
	dx, dy := relativeOffset(b, 200, 100) // left 50% of 200 = 100; top 10% of 100 = 10
	if dx != 100 || dy != 10 {
		t.Errorf("percent relativeOffset = (%v,%v), want (100,10)", dx, dy)
	}
}

// TestAbsRectLeftTop: a box at left:10 top:20 with a fixed size lands at the CB
// origin + offsets.
func TestAbsRectLeftTop(t *testing.T) {
	cb := rect{x: 100, y: 50, w: 400, h: 300}
	st := gcss.ComputedStyle{Position: "absolute",
		Top: px(20), Left: px(10), Right: gcss.Length{Unit: gcss.UnitAuto}, Bottom: gcss.Length{Unit: gcss.UnitAuto},
		Width: px(40), Height: px(30), MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto}, FontSizePt: 16}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Style: st}
	ed := usedEdges(b, cb.w)
	bb, contentW := absRect(b, ed, cb)
	if bb.x != 110 || bb.y != 70 { // 100+10, 50+20
		t.Errorf("absRect left/top = (%v,%v), want (110,70)", bb.x, bb.y)
	}
	if contentW != 40 {
		t.Errorf("absRect contentW = %v, want 40", contentW)
	}
}

// TestAbsRectRightBottom: a box at right:10 bottom:20 with a fixed size lands
// against the far edges.
func TestAbsRectRightBottom(t *testing.T) {
	cb := rect{x: 100, y: 50, w: 400, h: 300}
	st := gcss.ComputedStyle{Position: "absolute",
		Top: gcss.Length{Unit: gcss.UnitAuto}, Left: gcss.Length{Unit: gcss.UnitAuto}, Right: px(10), Bottom: px(20),
		Width: px(40), Height: px(30), MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto}, FontSizePt: 16}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Style: st}
	ed := usedEdges(b, cb.w)
	bb, _ := absRect(b, ed, cb)
	// border-box left = cb.x + cb.w - right - mR - borderBoxW = 100+400-10-0-40 = 450
	// border-box top  = cb.y + cb.h - bottom - mB - borderBoxH = 50+300-20-0-30 = 300
	if bb.x != 450 || bb.y != 300 {
		t.Errorf("absRect right/bottom = (%v,%v), want (450,300)", bb.x, bb.y)
	}
}

// TestAbsRectLeftRightAutoWidth: left+right specified with width:auto derives the
// width from the offsets.
func TestAbsRectLeftRightAutoWidth(t *testing.T) {
	cb := rect{x: 0, y: 0, w: 400, h: 300}
	st := gcss.ComputedStyle{Position: "absolute",
		Top: px(0), Left: px(30), Right: px(50), Bottom: gcss.Length{Unit: gcss.UnitAuto},
		Width: gcss.Length{Unit: gcss.UnitAuto}, Height: px(30),
		MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto}, FontSizePt: 16}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Style: st}
	ed := usedEdges(b, cb.w)
	bb, contentW := absRect(b, ed, cb)
	// content width = cb.w - left - right - insetsX - mL - mR = 400-30-50-0-0-0 = 320
	if contentW != 320 || bb.x != 30 {
		t.Errorf("absRect L+R auto width: contentW=%v x=%v, want 320/30", contentW, bb.x)
	}
}

// TestAbsRectAllAutoStatic: all-auto offsets place the box at the CB top-left
// (static approximation).
func TestAbsRectAllAutoStatic(t *testing.T) {
	cb := rect{x: 100, y: 50, w: 400, h: 300}
	auto := gcss.Length{Unit: gcss.UnitAuto}
	st := gcss.ComputedStyle{Position: "absolute",
		Top: auto, Left: auto, Right: auto, Bottom: auto,
		Width: px(40), Height: px(30), MaxWidth: auto, MaxHeight: auto, FontSizePt: 16}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Style: st}
	ed := usedEdges(b, cb.w)
	bb, _ := absRect(b, ed, cb)
	if bb.x != 100 || bb.y != 50 {
		t.Errorf("absRect all-auto = (%v,%v), want CB top-left (100,50)", bb.x, bb.y)
	}
}
