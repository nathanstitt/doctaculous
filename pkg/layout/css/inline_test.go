package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// The inline tests assert numeric glyph/atomic positions after a full Layout (not
// just the layoutInline hook), which locks down the coordinate frame: block layout
// shifts inline line baselines into page space, so a glyph's final X/Y must come out
// correct through the whole pipeline. Fixtures use `reset` (from block_test.go) to
// zero element margins so the paragraph's content-left is unambiguous; the default
// "serif" family resolves to a bundled substitute, so glyphs actually shape.

// absf is the absolute value of a float64 (avoids importing math for one call).
func absf(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// autoBlockStyle is the minimal computed style for a synthetic block container in
// the IFC tests: an auto-width, auto-height block (matching the cascade defaults a
// real element would carry, which the zero-value Length would otherwise read as
// width:0/height:0 — see isAnonymous/resolveContentWidth).
func autoBlockStyle() gcss.ComputedStyle {
	return gcss.ComputedStyle{
		Display:    "block",
		FontSizePt: 16,
		Width:      gcss.Length{Unit: gcss.UnitAuto},
		Height:     gcss.Length{Unit: gcss.UnitAuto},
		MaxWidth:   gcss.Length{Unit: gcss.UnitAuto},
		MaxHeight:  gcss.Length{Unit: gcss.UnitAuto},
	}
}

// firstLineWithGlyphs returns the first LineFragment of frag that has at least one
// glyph, or fails. It tolerates leading empty lines (none expected today, but keeps
// the assertions robust).
func firstLineWithGlyphs(t *testing.T, frag *Fragment) LineFragment {
	t.Helper()
	for _, ln := range frag.Lines {
		if len(ln.Glyphs) > 0 {
			return ln
		}
	}
	t.Fatalf("fragment %q has no line with glyphs (lines=%d)", frag.DebugTag, len(frag.Lines))
	return LineFragment{}
}

// TestInlineSingleLine: a short paragraph in a wide viewport lays out one line whose
// first glyph sits at the paragraph content-left (== p.X, no padding/border) and
// whose baseline is inside the paragraph's border box.
func TestInlineSingleLine(t *testing.T) {
	p := layoutOne(t, reset+`<p>Hello world</p>`, 1000)
	if len(p.Lines) != 1 {
		t.Fatalf("paragraph lines = %d, want 1", len(p.Lines))
	}
	ln := firstLineWithGlyphs(t, p)
	if len(ln.Glyphs) == 0 {
		t.Fatal("first line has no glyphs")
	}
	if got := ln.Glyphs[0].X; got != p.X {
		t.Errorf("first glyph X = %v, want content-left %v (p.X, no padding/border)", got, p.X)
	}
	if ln.BaselineY <= p.Y || ln.BaselineY >= p.Y+p.H {
		t.Errorf("baseline Y = %v, want within (%v, %v)", ln.BaselineY, p.Y, p.Y+p.H)
	}
}

// TestInlineWrapping: text too wide for a narrow content width breaks onto multiple
// lines; the second line resets to the content-left and sits a line-height below.
func TestInlineWrapping(t *testing.T) {
	// A 60pt-wide content box (width:60px on the p) forces wrapping of several words.
	p := layoutOne(t, reset+`<p style="width:60px">Hello wonderful wrapping world here</p>`, 1000)
	if len(p.Lines) < 2 {
		t.Fatalf("wrapped lines = %d, want >= 2", len(p.Lines))
	}
	l0, l1 := p.Lines[0], p.Lines[1]
	if len(l0.Glyphs) == 0 || len(l1.Glyphs) == 0 {
		t.Fatalf("expected glyphs on the first two lines, got %d and %d", len(l0.Glyphs), len(l1.Glyphs))
	}
	// Both lines start at the content-left (left alignment, wrap reset).
	if l0.Glyphs[0].X != p.X {
		t.Errorf("line 0 first glyph X = %v, want %v", l0.Glyphs[0].X, p.X)
	}
	if l1.Glyphs[0].X != p.X {
		t.Errorf("line 1 first glyph X = %v, want %v (wrap reset to content-left)", l1.Glyphs[0].X, p.X)
	}
	// Line 1 sits a line-height below line 0 (auto line-height for 16pt serif).
	dy := l1.BaselineY - l0.BaselineY
	if dy <= 0 {
		t.Fatalf("line 1 baseline %v not below line 0 baseline %v", l1.BaselineY, l0.BaselineY)
	}
	// The gap is the natural auto line height ((ascent+descent+gap)×1.15) for a 16pt
	// face: larger than the font size, and within a sane band (not pinning exact face
	// metrics). Every line uses the same auto height, so consecutive strides match.
	if dy <= 16 || dy > 80 {
		t.Errorf("inter-baseline gap = %v, want a plausible auto line height (> font size, < 80)", dy)
	}
	if len(p.Lines) >= 3 {
		if dy2 := p.Lines[2].BaselineY - p.Lines[1].BaselineY; absf(dy2-dy) > 1e-6 {
			t.Errorf("auto line stride not constant: %v then %v", dy, dy2)
		}
	}
}

// TestInlineTextAlignCenterRight: centered/right text shifts the first glyph right of
// the left-aligned control by half / all of the slack.
func TestInlineTextAlignCenterRight(t *testing.T) {
	left := layoutOne(t, reset+`<p>word</p>`, 1000)
	center := layoutOne(t, reset+`<p style="text-align:center">word</p>`, 1000)
	right := layoutOne(t, reset+`<p style="text-align:right">word</p>`, 1000)

	lx := firstLineWithGlyphs(t, left).Glyphs[0].X
	cx := firstLineWithGlyphs(t, center).Glyphs[0].X
	rx := firstLineWithGlyphs(t, right).Glyphs[0].X

	if !(cx > lx) {
		t.Errorf("center first glyph X = %v, want > left X = %v", cx, lx)
	}
	if !(rx > cx) {
		t.Errorf("right first glyph X = %v, want > center X = %v", rx, cx)
	}
	// Right alignment pushes the word to the right edge: rx is roughly 2× the center
	// offset from the left edge (center slack is half the right slack).
	leftEdge := left.X
	if (rx-leftEdge) < 2*(cx-leftEdge)-1 || (rx-leftEdge) > 2*(cx-leftEdge)+1 {
		t.Errorf("right offset %v not ~2× center offset %v from content-left", rx-leftEdge, cx-leftEdge)
	}
}

// TestInlineTextAlignFromAnonBlock (handover note 1): a bare text node before a
// block sibling is wrapped in an anonymous block whose own Style is zero-valued. Its
// alignment must come from the inherited text-align of its content, not the (empty)
// anon-block Style. The containing div is centered, so the anon block's line is too.
func TestInlineTextAlignFromAnonBlock(t *testing.T) {
	body := layoutBody(t, reset+`<div style="text-align:center">text before<p>block</p></div>`, 1000)
	div := body.Children[0]
	// div children: [anon-block(text), p(block)]. Find the anon block (the one with
	// inline lines / glyphs); it is the first child.
	if len(div.Children) < 1 {
		t.Fatalf("div has %d children, want >= 1 (anon block + p)", len(div.Children))
	}
	anon := div.Children[0]
	if anon.DebugTag != "anon-block" {
		t.Fatalf("first div child DebugTag = %q, want anon-block", anon.DebugTag)
	}
	ln := firstLineWithGlyphs(t, anon)
	// Centered => first glyph X is strictly right of the content-left (anon.X).
	if ln.Glyphs[0].X <= anon.X {
		t.Errorf("anon-block first glyph X = %v, want > content-left %v (centered via inherited align)", ln.Glyphs[0].X, anon.X)
	}
}

// TestInlineLineHeightExact: an explicit line-height in px sets the exact baseline
// stride between wrapped lines.
func TestInlineLineHeightExact(t *testing.T) {
	p := layoutOne(t, reset+`<p style="width:60px;line-height:40px">two lines of wrapping text here</p>`, 1000)
	if len(p.Lines) < 2 {
		t.Fatalf("lines = %d, want >= 2 to measure stride", len(p.Lines))
	}
	dy := p.Lines[1].BaselineY - p.Lines[0].BaselineY
	if dy != 40 {
		t.Errorf("baseline stride = %v, want 40 (line-height:40px)", dy)
	}
}

// TestInlineAnonBlockLineHeight (regression): a bare text node before a block
// sibling is wrapped in an anonymous block whose own Style is zero-valued, so its
// LineHeight reads as Length{0, UnitPx}. effectiveLineHeight must NOT take that
// literally (height 0) — it must treat an anonymous box's line-height as auto, like
// isAnonymous does for width/height. Otherwise every wrapped line stacks on one
// baseline (stride 0) and the anon block contributes zero height to its parent.
//
// A 60px-wide div forces the bare text to wrap to multiple lines; we assert the anon
// block has >= 2 lines, a positive *constant* inter-baseline stride matching the auto
// line height (metrics × 1.15 at the inherited 16px), and a positive own height.
func TestInlineAnonBlockLineHeight(t *testing.T) {
	body := layoutBody(t, reset+`<div style="width:60px">word word word word word word<p>x</p></div>`, 1000)
	div := body.Children[0]
	if len(div.Children) < 1 {
		t.Fatalf("div has %d children, want >= 1 (anon block + p)", len(div.Children))
	}
	anon := div.Children[0]
	if anon.DebugTag != "anon-block" {
		t.Fatalf("first div child DebugTag = %q, want anon-block", anon.DebugTag)
	}
	// The wrapping bare text must produce at least two line fragments to measure a
	// stride at all (six "word"s in a 60px box wrap several times).
	if len(anon.Lines) < 2 {
		t.Fatalf("anon-block lines = %d, want >= 2 (wrapping bare text)", len(anon.Lines))
	}
	// Consecutive baselines differ by a positive amount ≈ the auto line height. With
	// the bug (h == 0) every baseline would be identical, so stride would be 0.
	stride := anon.Lines[1].BaselineY - anon.Lines[0].BaselineY
	if stride <= 0 {
		t.Fatalf("inter-baseline stride = %v, want > 0 (anon line-height must be auto, not 0)", stride)
	}
	// The auto line height for a 16px face: larger than the font size, within a sane
	// band (same band TestInlineWrapping validates; not pinning exact face metrics).
	if stride <= 16 || stride > 80 {
		t.Errorf("anon-block auto stride = %v, want a plausible auto line height (> 16, < 80)", stride)
	}
	// Auto height is uniform: every line uses the same stride.
	for i := 2; i < len(anon.Lines); i++ {
		if dy := anon.Lines[i].BaselineY - anon.Lines[i-1].BaselineY; absf(dy-stride) > 1e-6 {
			t.Errorf("anon-block stride not constant: %v then %v (line %d)", stride, dy, i)
		}
	}
	// The anon block contributes real height to its parent (zero with the bug).
	if anon.H <= 0 {
		t.Errorf("anon-block fragment H = %v, want > 0 (wrapped lines give it height)", anon.H)
	}
}

// TestInlineEmptyParagraph: an empty paragraph produces no glyphs and ~no inline
// height, and never panics.
func TestInlineEmptyParagraph(t *testing.T) {
	p := layoutOne(t, reset+`<p></p>`, 1000)
	for _, ln := range p.Lines {
		if len(ln.Glyphs) > 0 {
			t.Errorf("empty paragraph produced glyphs: %d", len(ln.Glyphs))
		}
	}
	// With no inline content the content height is 0, so the (auto-height) border box
	// collapses to zero height.
	if p.H != 0 {
		t.Errorf("empty paragraph border-box H = %v, want 0", p.H)
	}
}

// --- inline-block atomics ---
//
// NOTE on reachability: box generation now classifies an inline-block as inline-LEVEL
// for its parent's formatting-context partitioning (isBlockLevelOuter in anon.go),
// so a container holding only inline-blocks establishes an INLINE formatting context
// and the inline-blocks flow through layoutInline via the public Build path — see
// TestInlineBlockFlowsInlineEndToEnd below, which drives Build -> Engine.Layout. The
// unit test here still constructs the IFC box directly so it pins the atomic path's
// numeric behavior in isolation (independent of box-gen), which keeps the contract
// the end-to-end test relies on under tight, localized assertions.

// inlineBlockChild builds a minimal inline-block box of the given content size.
func inlineBlockChild(w, h float64) *cssbox.Box {
	return &cssbox.Box{
		Kind:       cssbox.BoxBlock,
		Display:    cssbox.DisplayInlineBlock,
		Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{
			Display:    "inline-block",
			FontSizePt: 16,
			Width:      gcss.Length{Value: w, Unit: gcss.UnitPx},
			Height:     gcss.Length{Value: h, Unit: gcss.UnitPx},
			MaxWidth:   gcss.Length{Unit: gcss.UnitAuto}, // "none"
			MaxHeight:  gcss.Length{Unit: gcss.UnitAuto},
		},
	}
}

// TestInlineBlockAtomics: two inline-blocks in an inline formatting context lay out
// as two atomic child fragments on one line, advancing the line by their widths and
// each carrying its own border-box size. Exercises layoutInline's atomic path by
// constructing the IFC box directly (see the NOTE above).
func TestInlineBlockAtomics(t *testing.T) {
	ifc := &cssbox.Box{
		Kind:       cssbox.BoxBlock,
		Display:    cssbox.DisplayBlock,
		Formatting: cssbox.InlineFC,
		Style:      autoBlockStyle(),
		Children: []*cssbox.Box{
			inlineBlockChild(50, 30),
			inlineBlockChild(50, 30),
		},
	}
	e := New(nil, nil)
	res := e.layoutBlock(context.Background(), ifc, 1000, 0, 0)
	frag := res.frag

	if len(frag.Children) != 2 {
		t.Fatalf("inline-block atomic children = %d, want 2", len(frag.Children))
	}
	a, b := frag.Children[0], frag.Children[1]
	// Each is 50×30 (content-box width == border-box width here; no padding/border).
	for i, c := range []*Fragment{a, b} {
		if c.W != 50 || c.H != 30 {
			t.Errorf("atomic %d size = %vx%v, want 50x30", i, c.W, c.H)
		}
	}
	// They sit side by side ~50 apart, both on the same line (same Y).
	if b.X-a.X != 50 {
		t.Errorf("atomic X gap = %v, want 50 (line advance)", b.X-a.X)
	}
	if a.Y != b.Y {
		t.Errorf("atomics on one line should share Y: %v vs %v", a.Y, b.Y)
	}
	// First atomic sits at the content-left (origin 0 here).
	if a.X != 0 {
		t.Errorf("first atomic X = %v, want 0 (content-left)", a.X)
	}
	// The IFC box's content height is at least the atomic height (one 30pt line).
	if frag.H < 30 {
		t.Errorf("IFC border-box H = %v, want >= 30 (atomic line height)", frag.H)
	}
	// Bottom-aligned baseline: each atomic's bottom rests on the line baseline.
	if got := a.Y + a.H; got != frag.Lines[0].BaselineY {
		t.Errorf("atomic bottom = %v, want on baseline %v", got, frag.Lines[0].BaselineY)
	}
}

// TestInlineBlockFlowsInlineEndToEnd drives two inline-blocks through the PUBLIC
// path (Build -> Engine.Layout) and asserts they flow inline: two 50×30 atomic
// fragments side by side ~50 apart on ONE line. This proves the Task-6 atomic path
// is now REACHABLE from box generation (the container becomes InlineFC because an
// inline-block is inline-level outer; see the NOTE above and anon.go).
func TestInlineBlockFlowsInlineEndToEnd(t *testing.T) {
	const cell = "display:inline-block;width:50px;height:30px"
	div := layoutOne(t, reset+`<div><span style="`+cell+`">a</span><span style="`+cell+`">b</span></div>`, 1000)

	// The div established an inline FC, so its inline-blocks came back as atomic
	// child fragments (not stacked block children).
	if len(div.Children) != 2 {
		t.Fatalf("inline-block atomic children = %d, want 2: div has %d lines", len(div.Children), len(div.Lines))
	}
	a, b := div.Children[0], div.Children[1]
	for i, c := range []*Fragment{a, b} {
		if c.W != 50 || c.H != 30 {
			t.Errorf("atomic %d size = %vx%v, want 50x30", i, c.W, c.H)
		}
	}
	// Side by side ~50 apart on one line (same Y), the first at the content-left.
	if b.X-a.X != 50 {
		t.Errorf("atomic X gap = %v, want 50 (laid out inline on one line)", b.X-a.X)
	}
	if a.Y != b.Y {
		t.Errorf("atomics should share a line (same Y): %v vs %v", a.Y, b.Y)
	}
	if a.X != div.X {
		t.Errorf("first atomic X = %v, want div content-left %v", a.X, div.X)
	}
	// One line, tall enough for the 30pt atomics.
	if len(div.Lines) != 1 {
		t.Errorf("div lines = %d, want 1 (both inline-blocks on one line)", len(div.Lines))
	}
	if div.H < 30 {
		t.Errorf("div border-box H = %v, want >= 30 (atomic line height)", div.H)
	}
}

// TestInlineReplacedFromAttrs: a replaced inline box (img) with width/height
// presentation attributes advances the line by that size even with no decoded image.
// Exercised directly (img reaches the IFC only when it is the inline content of a
// block, e.g. wrapped with text; here we assert the size resolution helper path via
// the IFC).
func TestInlineReplacedFromAttrs(t *testing.T) {
	ifc := &cssbox.Box{
		Kind:       cssbox.BoxBlock,
		Display:    cssbox.DisplayBlock,
		Formatting: cssbox.InlineFC,
		Style:      autoBlockStyle(),
		Children: []*cssbox.Box{
			{
				Kind:     cssbox.BoxReplaced,
				Display:  cssbox.DisplayInline,
				Style:    gcss.ComputedStyle{Display: "inline", FontSizePt: 16, Width: gcss.Length{Unit: gcss.UnitAuto}, Height: gcss.Length{Unit: gcss.UnitAuto}},
				Replaced: &cssbox.ReplacedContent{Tag: "img", Attrs: map[string]string{"width": "40", "height": "20"}},
			},
		},
	}
	e := New(nil, nil)
	lines, h, atomics := e.layoutInline(context.Background(), ifc, 1000, 0, 0)
	// A replaced box has no own Fragment here (only inline-block builds one), so it
	// contributes no atomic child fragment but still advances/sizes the line.
	if len(atomics) != 0 {
		t.Errorf("replaced atomics = %d, want 0 (no fragment for a bare replaced leaf)", len(atomics))
	}
	if len(lines) != 1 {
		t.Fatalf("replaced lines = %d, want 1", len(lines))
	}
	// The line height covers the 20pt image (ascent fallback to atomic height).
	if h < 20 {
		t.Errorf("replaced inline height = %v, want >= 20", h)
	}
}

// TestInlineGlyphColorPropagates: a colored paragraph emits glyphs in that color,
// confirming the run carries inherited color into the shaped glyph fragment.
func TestInlineGlyphColorPropagates(t *testing.T) {
	p := layoutOne(t, reset+`<p style="color:rgb(10,20,30)">hi</p>`, 1000)
	ln := firstLineWithGlyphs(t, p)
	c := ln.Glyphs[0].Color
	if c.R != 10 || c.G != 20 || c.B != 30 || c.A != 255 {
		t.Errorf("glyph color = %v, want rgb(10,20,30) opaque", c)
	}
}
