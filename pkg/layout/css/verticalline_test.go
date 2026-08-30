package css

import (
	"fmt"
	"strings"
	"testing"
)

// inkBox is the bounding box of a fragment tree's glyph pen origins, in page space.
// It is deliberately built from the LAID-OUT positions rather than from rasterized
// pixels: the claim phase 2 makes is about where layout puts the glyphs, and asserting
// on the box under claim — rather than a page-wide pixel count — is what distinguishes
// "the text turned vertical" from "the text moved and its container grew around it".
// That distinction is exactly what a page-wide count got wrong in the report that
// motivated this work.
type inkBox struct {
	minX, minY, maxX, maxY float64
	n                      int
}

func (b inkBox) w() float64 { return b.maxX - b.minX }
func (b inkBox) h() float64 { return b.maxY - b.minY }

// glyphInk walks every line of every fragment and bounds the pen origins, extending
// each glyph by its advance along the axis it advances on. A glyph's advance is the
// only extent information at this layer; it is the same approximation the decoration
// passes document as adequate.
func glyphInk(f *Fragment) inkBox {
	b := inkBox{minX: 1e18, minY: 1e18, maxX: -1e18, maxY: -1e18}
	var walk func(*Fragment)
	walk = func(f *Fragment) {
		for li := range f.Lines {
			ln := &f.Lines[li]
			for gi := range ln.Glyphs {
				g := &ln.Glyphs[gi]
				if g.Outline == nil {
					continue // whitespace / inline-box edge: no ink
				}
				x, y := g.X, ln.BaselineY+g.Y
				x1, y1 := x, y
				if ln.Vertical {
					y1 = y + g.AdvancePt
				} else {
					x1 = x + g.AdvancePt
				}
				if x < b.minX {
					b.minX = x
				}
				if y < b.minY {
					b.minY = y
				}
				if x1 > b.maxX {
					b.maxX = x1
				}
				if y1 > b.maxY {
					b.maxY = y1
				}
				b.n++
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(f)
	return b
}

// The measurement that proved the gap is the regression test. "NOW" at 24px in a
// 40px-wide box paints a WIDE, SHORT ink box when laid out horizontally — overflowing
// the box it was given. Under writing-mode:vertical-rl it must paint a TALL, NARROW one
// instead. Asserting the aspect flip rather than exact numbers keeps the test honest
// about what is being claimed (the axis changed) without pinning font metrics.
func TestVerticalWritingModeTurnsTheLineVertical(t *testing.T) {
	const body = `<div style="width:40px;font-size:24px;%s">NOW</div>`
	horiz := layoutTreeFor(t, `<html><body style="margin:0">`+
		fmt.Sprintf(body, "")+`</body></html>`, 200, nil)
	vert := layoutTreeFor(t, `<html><body style="margin:0">`+
		fmt.Sprintf(body, "writing-mode:vertical-rl")+`</body></html>`, 200, nil)

	h, v := glyphInk(horiz), glyphInk(vert)
	if h.n != 3 || v.n != 3 {
		t.Fatalf("expected 3 glyphs each, got horizontal=%d vertical=%d", h.n, v.n)
	}
	if h.w() <= h.h() {
		t.Fatalf("horizontal baseline is not wide-and-short (%vx%v); the fixture is wrong", h.w(), h.h())
	}
	if v.h() <= v.w() {
		t.Errorf("vertical ink box = %vx%v, want taller than wide", v.w(), v.h())
	}
	// The vertical extent is 3 glyphs at one em each, NOT the sum of their horizontal
	// widths — vertical advance does not vary with how wide a letter is. Deliberately
	// not asserted equal to the horizontal extent: an earlier version of this test did
	// exactly that and passed only because the pen was (wrongly) advancing by width.
	if d := v.h() - 3*24; d > 0.5 || d < -0.5 {
		t.Errorf("vertical extent = %v, want 72 (3 glyphs x 24pt em)", v.h())
	}
}

// Every glyph on a vertical line shares one inline-axis coordinate (the baseline runs
// down the page) and advances along Y. This is the structural claim underneath the
// aspect-ratio assertion above, pinned directly so a regression names itself.
func TestVerticalLineGlyphsShareXAndAdvanceInY(t *testing.T) {
	root := layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="writing-mode:vertical-rl;font-size:20px">NOW</div></body></html>`, 200, nil)

	var ln *LineFragment
	var walk func(*Fragment)
	walk = func(f *Fragment) {
		for li := range f.Lines {
			if len(f.Lines[li].Glyphs) > 0 && ln == nil {
				ln = &f.Lines[li]
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if ln == nil {
		t.Fatal("no line with glyphs found")
	}
	if !ln.Vertical {
		t.Fatal("line is not marked vertical")
	}
	if len(ln.Glyphs) != 3 {
		t.Fatalf("got %d glyphs, want 3", len(ln.Glyphs))
	}
	for i := range ln.Glyphs {
		if d := ln.Glyphs[i].X - ln.Glyphs[0].X; d > 0.01 || d < -0.01 {
			t.Errorf("glyph %d X = %v, want %v (all glyphs share the vertical baseline)",
				i, ln.Glyphs[i].X, ln.Glyphs[0].X)
		}
	}
	for i := 1; i < len(ln.Glyphs); i++ {
		if ln.Glyphs[i].Y <= ln.Glyphs[i-1].Y {
			t.Errorf("glyph %d Y = %v does not advance past glyph %d Y = %v",
				i, ln.Glyphs[i].Y, i-1, ln.Glyphs[i-1].Y)
		}
	}
}

// The pen must advance by the VERTICAL metric, not the horizontal one. This is the
// assertion that distinguishes real vertical layout from text that merely stacks: a
// vertical advance does not vary with a glyph's width, so N, O and W — visibly
// different widths — must be spaced identically, at one em.
//
// The first version of this feature had no such check and passed while advancing by
// horizontal widths. The aspect-ratio test above could not see it, because the total
// extent came out close either way.
func TestVerticalAdvanceIsTheFontsVerticalMetricNotItsWidth(t *testing.T) {
	const sizePt = 24
	root := layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="writing-mode:vertical-rl;font-size:24px">NOW</div></body></html>`, 200, nil)

	var ln *LineFragment
	var walk func(*Fragment)
	walk = func(f *Fragment) {
		for li := range f.Lines {
			if len(f.Lines[li].Glyphs) > 0 && ln == nil {
				ln = &f.Lines[li]
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if ln == nil || len(ln.Glyphs) != 3 {
		t.Fatal("expected one line of 3 glyphs")
	}

	// N, O and W have different horizontal widths; if any two vertical advances differ,
	// the pen is following the wrong axis.
	for i := range ln.Glyphs {
		if d := ln.Glyphs[i].AdvancePt - sizePt; d > 0.01 || d < -0.01 {
			t.Errorf("glyph %d vertical advance = %v, want %v (one em); a width-derived "+
				"advance would vary per letter", i, ln.Glyphs[i].AdvancePt, float64(sizePt))
		}
	}
	// And the pen positions must follow from them.
	for i := 1; i < len(ln.Glyphs); i++ {
		if d := (ln.Glyphs[i].Y - ln.Glyphs[i-1].Y) - sizePt; d > 0.01 || d < -0.01 {
			t.Errorf("glyphs %d..%d are %v apart, want %v", i-1, i,
				ln.Glyphs[i].Y-ln.Glyphs[i-1].Y, float64(sizePt))
		}
	}
}

// The first glyph's origin sits one ascent below the content top, not on it. A pen
// starting at zero puts the first letter's baseline on the box edge and clips
// everything above it — which on a short label is subtle enough to survive an ink-box
// test while being plainly wrong on the page.
func TestVerticalFirstGlyphClearsTheContentTop(t *testing.T) {
	root := layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="writing-mode:vertical-rl;font-size:24px">NOW</div></body></html>`, 200, nil)

	var ln *LineFragment
	var walk func(*Fragment)
	walk = func(f *Fragment) {
		for li := range f.Lines {
			if len(f.Lines[li].Glyphs) > 0 && ln == nil {
				ln = &f.Lines[li]
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if ln == nil || len(ln.Glyphs) == 0 {
		t.Fatal("no glyphs")
	}

	first := &ln.Glyphs[0]
	if first.Y <= 0 {
		t.Fatalf("first glyph Y = %v; it must clear the content top by its ascent, or the "+
			"top of the letter is clipped", first.Y)
	}
	// It should be the glyph's own ascent, not an arbitrary inset.
	if d := first.Y - first.AscentPt; d > 0.5 || d < -0.5 {
		t.Errorf("first glyph Y = %v, want its ascent %v", first.Y, first.AscentPt)
	}
}

// A vertical box sizes itself along the swapped axes: its used height grows with the
// text's inline extent instead of its width. The container is the box the claim is
// about, so it is the box that gets measured.
func TestVerticalWritingModeGrowsTheBlockAlongY(t *testing.T) {
	const body = `<div id="t" style="font-size:20px;%s">NOWNOW</div>`
	horiz := layoutTreeFor(t, `<html><body style="margin:0">`+
		fmt.Sprintf(body, "")+`</body></html>`, 400, nil)
	vert := layoutTreeFor(t, `<html><body style="margin:0">`+
		fmt.Sprintf(body, "writing-mode:vertical-rl")+`</body></html>`, 400, nil)

	hb, vb := firstTextBox(horiz), firstTextBox(vert)
	if hb == nil || vb == nil {
		t.Fatal("expected a text-bearing fragment in both trees")
	}
	if vb.H <= hb.H {
		t.Errorf("vertical box height = %v, want greater than the horizontal %v", vb.H, hb.H)
	}
}

// horizontal-tb must stay byte-identical. Phase 2 is additive: every existing caller
// takes the same path it did before, and this is the control that keeps it so.
func TestHorizontalLayoutUnchangedByVerticalSupport(t *testing.T) {
	root := layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="width:200px;font-size:16px">Hello world, this is ordinary text.</div>`+
		`</body></html>`, 400, nil)
	var ln *LineFragment
	var walk func(*Fragment)
	walk = func(f *Fragment) {
		for li := range f.Lines {
			if len(f.Lines[li].Glyphs) > 0 && ln == nil {
				ln = &f.Lines[li]
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if ln == nil {
		t.Fatal("no line with glyphs found")
	}
	if ln.Vertical {
		t.Error("an ordinary line is marked vertical")
	}
	for i := range ln.Glyphs {
		if ln.Glyphs[i].Y != 0 {
			t.Errorf("glyph %d has a non-zero Y offset (%v) on a horizontal line", i, ln.Glyphs[i].Y)
		}
	}
	for i := 1; i < len(ln.Glyphs); i++ {
		if ln.Glyphs[i].X < ln.Glyphs[i-1].X {
			t.Errorf("horizontal glyph %d moved backwards in X", i)
		}
	}
}

// Phase 2 lays out a single vertical line and does NOT wrap. A run longer than the
// block extent overflows, and says so — the project's degrade-honestly rule. Silence
// here would look identical to working wrapping.
func TestVerticalOverflowIsLogged(t *testing.T) {
	var msgs []string
	logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
	layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="writing-mode:vertical-rl;height:30px;font-size:20px">`+
		strings.Repeat("NOW", 12)+`</div></body></html>`, 200, logf)

	var found bool
	for _, m := range msgs {
		if strings.Contains(m, "vertical") && strings.Contains(m, "overflow") {
			found = true
		}
	}
	if !found {
		t.Errorf("a vertical run overflowing its block extent did not log; messages: %v", msgs)
	}
}

// Float avoidance is absent, and unlike the other gaps it produces OVERLAPPING ink
// rather than merely missing ink: the baseline is placed in the middle of the full
// content box, so the text is drawn through the float. Measured before this log existed —
// a 60pt float and a vertical run in a 200pt body put the baseline at x=100, inside the
// float. Wrong pixels with no diagnostic is the failure this project's rules exist to
// prevent, so it says so.
func TestVerticalNearAFloatIsLogged(t *testing.T) {
	var msgs []string
	logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
	layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="float:left;width:60px;height:60px"></div>`+
		`<div style="writing-mode:vertical-rl;font-size:20px">NOW</div>`+
		`</body></html>`, 200, logf)

	var found bool
	for _, m := range msgs {
		if strings.Contains(m, "float") && strings.Contains(m, "vertical") {
			found = true
		}
	}
	if !found {
		t.Errorf("a vertical line beside a float did not log; messages: %v", msgs)
	}
}

// ...and a vertical line with NO float must not emit that log, or it becomes noise.
func TestVerticalWithoutAFloatDoesNotLogFloat(t *testing.T) {
	var msgs []string
	logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
	layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="writing-mode:vertical-rl;font-size:20px">NOW</div></body></html>`, 200, logf)

	for _, m := range msgs {
		if strings.Contains(m, "float") {
			t.Errorf("a vertical line with no float logged about floats: %q", m)
		}
	}
}

// The phase 0 "not implemented" warning must be GONE for the case that now works.
// Leaving it would be the mirror of the silent no-op it replaced: a diagnostic that
// contradicts the rendering.
func TestImplementedVerticalModeNoLongerWarnsNotImplemented(t *testing.T) {
	var msgs []string
	logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
	layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="writing-mode:vertical-rl;font-size:20px">NOW</div></body></html>`, 200, logf)

	for _, m := range msgs {
		if strings.Contains(m, "not yet implemented") {
			t.Errorf("vertical-rl still reports itself unimplemented: %q", m)
		}
	}
}
