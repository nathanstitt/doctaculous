package css

import (
	"context"
	"sort"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/layout/inline"
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

// TestInlineAnonBlockInheritedLineHeight (M1 fix): line-height is a CSS *inherited*
// property, so a bare text node before a block sibling — wrapped in an anonymous
// block whose own Style is zero-valued — must honor the line-height INHERITED from
// its containing element, not fall back to the auto default. Here the div sets
// line-height:40px; the anon block's text leaves carry that inherited 40px, so the
// anon block's wrapped lines must stride by exactly 40 (not the metrics×1.15 auto
// height the old code forced for every anonymous box).
//
// On the pre-fix code (which unconditionally used the auto computation for an
// anonymous box) this stride would be ~the 16px serif auto line height (> 16, < 80,
// and not 40), so the `== 40` assertion fails; the fix makes it pass.
func TestInlineAnonBlockInheritedLineHeight(t *testing.T) {
	body := layoutBody(t, reset+`<div style="width:60px;line-height:40px">word word word word word word<p>x</p></div>`, 1000)
	div := body.Children[0]
	if len(div.Children) < 1 {
		t.Fatalf("div has %d children, want >= 1 (anon block + p)", len(div.Children))
	}
	anon := div.Children[0]
	if anon.DebugTag != "anon-block" {
		t.Fatalf("first div child DebugTag = %q, want anon-block", anon.DebugTag)
	}
	// Six "word"s in a 60px box wrap to multiple lines, so we can measure a stride.
	if len(anon.Lines) < 2 {
		t.Fatalf("anon-block lines = %d, want >= 2 (wrapping bare text)", len(anon.Lines))
	}
	// The inherited line-height:40px sets the EXACT inter-baseline stride. With the
	// old auto fallback this would be metrics×1.15 (~the band TestInlineWrapping
	// checks), never exactly 40.
	stride := anon.Lines[1].BaselineY - anon.Lines[0].BaselineY
	if stride != 40 {
		t.Errorf("anon-block inter-baseline stride = %v, want 40 (inherited line-height:40px)", stride)
	}
	// Uniform across every line (the inherited value is constant).
	for i := 2; i < len(anon.Lines); i++ {
		if dy := anon.Lines[i].BaselineY - anon.Lines[i-1].BaselineY; dy != 40 {
			t.Errorf("anon-block stride not constant: 40 then %v (line %d)", dy, i)
		}
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
	e := New(nil, nil, nil)
	res := e.layoutBlock(context.Background(), ifc, 1000, 0, 0, 0, &floatContext{cbLeft: 0, cbRight: 1000}, &positionedContext{}, posCBOwner{isPage: true})
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
	// EMPTY inline-blocks (no in-flow line box) keep the bottom-margin-edge baseline
	// (CSS 2.1 §10.8.1): each atomic's bottom rests on the line baseline. (A text-bearing
	// inline-block aligns its own text baseline instead — see TestInlineBlockTextBaseline.)
	if got := a.Y + a.H; got != frag.Lines[0].BaselineY {
		t.Errorf("atomic bottom = %v, want on baseline %v", got, frag.Lines[0].BaselineY)
	}
}

// TestAutoLineHeightExcludesLineGap pins E5: "normal" line height is
// (ascent+descent)×cssDefaultLineMult and does NOT add the font's line gap. The bundled
// substitute faces report an anomalous ~1.3em hhea gap; adding it (then ×1.15) ballooned a
// 16px line to ~49pt. With a line carrying a large gap, the computed height must depend
// only on ascent+descent. Mutation-verify: re-add LineGapPt to autoLineHeight and the
// height jumps by gap×mult.
func TestAutoLineHeightExcludesLineGap(t *testing.T) {
	// A synthetic line with a big line gap and modest ascent/descent.
	line := inline.Line{AscentPt: 12, DescentPt: 4, LineGapPt: 20}
	got := autoLineHeight(line)
	want := (12.0 + 4.0) * cssDefaultLineMult // gap excluded
	if absf(got-want) > 1e-6 {
		t.Errorf("autoLineHeight = %.3f, want %.3f (ascent+descent only, gap excluded)", got, want)
	}
	// Sanity: the gap-inclusive value would be much larger — guard we didn't regress to it.
	gapInclusive := (12.0 + 4.0 + 20.0) * cssDefaultLineMult
	if absf(got-gapInclusive) < 1e-6 {
		t.Errorf("autoLineHeight still includes the line gap (%.3f)", got)
	}
}

// TestLineHeightMinFloor pins the DOCX lineRule=atLeast floor: a block whose natural
// line box (10pt font metrics) is well under 40pt, given a LineHeightMin of 40pt,
// produces a line height >= 40pt. The SAME block WITHOUT the floor stays under 40pt —
// proving the fixture is valid and it is the floor (not the font metrics) that raises
// the height. HTML never sets LineHeightMin, so this floor is inert on every HTML
// fixture. Exercises effectiveLineHeight directly with a synthetic line (the pattern
// TestAutoLineHeightExcludesLineGap uses for autoLineHeight).
func lineHeightMinBlock(min gcss.Length) *cssbox.Box {
	st := autoBlockStyle()
	st.FontSizePt = 10
	st.LineHeight = gcss.Length{Unit: gcss.UnitAuto} // "normal" -> metrics-based auto height
	st.LineHeightMin = min
	return &cssbox.Box{
		Kind:       cssbox.BoxBlock,
		Display:    cssbox.DisplayBlock,
		Formatting: cssbox.InlineFC,
		Style:      st,
	}
}

func TestLineHeightMinFloor(t *testing.T) {
	e := New(nil, nil, nil)
	// A modest 10pt-face line: ascent+descent well under 40pt, no atomic extent.
	line := inline.Line{AscentPt: 8, DescentPt: 2}

	// Baseline: no floor -> natural (auto) line height, comfortably under 40pt.
	hBase := e.effectiveLineHeight(lineHeightMinBlock(gcss.Length{}), line)
	if hBase <= 0 || hBase >= 40 {
		t.Fatalf("baseline line height = %v, want a positive value well under 40 (fixture invalid otherwise)", hBase)
	}

	// With a 40pt "at least" floor, the same line's height is raised to >= 40.
	hFloored := e.effectiveLineHeight(lineHeightMinBlock(gcss.Length{Value: 40, Unit: gcss.UnitPt}), line)
	if hFloored < 40 {
		t.Errorf("floored line height = %v, want >= 40 (LineHeightMin floor)", hFloored)
	}
}

// TestParagraphLineHeightReasonable is the end-to-end companion: two wrapped lines of
// 16px text must be spaced by a browser-comparable ~1.1–1.4× the font size, NOT ~3× (the
// E5 bug spaced them ~49pt apart). It asserts the gap between consecutive line baselines.
func TestParagraphLineHeightReasonable(t *testing.T) {
	// Narrow width forces a wrap; default font (serif) is the one with the bad gap.
	src := `<p style="margin:0">wrap this sentence onto two lines please now ok</p>`
	root := layoutTreeFor(t, src, 120, nil)
	p := descendToIFC(t, root)
	if len(p.Lines) < 2 {
		t.Fatalf("expected the text to wrap to >=2 lines, got %d", len(p.Lines))
	}
	gap := p.Lines[1].BaselineY - p.Lines[0].BaselineY
	// Default font size is 16px; a sane normal line height is ~18–24pt. The bug gave ~49.
	if gap < 16 || gap > 30 {
		t.Errorf("inter-line baseline gap = %.1f, want a sane ~18–24pt (the E5 bug gave ~49)", gap)
	}
}

// TestInlineBlockShrinkToFit pins E4: an auto-width inline-block with text shrinks to fit
// its content (CSS 10.3.9) instead of filling the parent's content width. "BOX" in a 300pt
// line must produce an inline-block far narrower than 300 (≈ the width of 3 glyphs), sitting
// inline after "before". Mutation-verify: make inlineBlockCBWidth return parentContentW and
// the inline-block fills the line (W≈300, forcing the text onto separate lines).
func TestInlineBlockShrinkToFit(t *testing.T) {
	src := `<p style="margin:0;font:20px monospace">before <span style="display:inline-block;background:#ffdd55">BOX</span> after</p>`
	root := layoutTreeFor(t, src, 300, nil)
	p := descendToIFC(t, root)
	if len(p.Children) != 1 {
		t.Fatalf("want 1 inline-block atom, got %d children", len(p.Children))
	}
	ib := p.Children[0]
	// "BOX" at 20px monospace is roughly 3×12 = ~36pt; certainly far below the 300pt line.
	if ib.W >= 100 {
		t.Errorf("auto-width inline-block W = %.1f, want shrink-to-fit (~36, well under 100); it is filling the line", ib.W)
	}
	if ib.W <= 0 {
		t.Errorf("inline-block W = %.1f, want a positive shrink-to-fit width", ib.W)
	}
	// It flows inline: "before " precedes it, so its X is past the content-left.
	if ib.X <= 0 {
		t.Errorf("inline-block X = %.1f, want > 0 (it sits after 'before ')", ib.X)
	}
	// The whole line fits: with shrink-to-fit, "before BOX after" is one line.
	if len(p.Lines) != 1 {
		t.Errorf("want the content on one line (shrink-to-fit), got %d lines", len(p.Lines))
	}
}

// TestInlineBlockSpecifiedWidthHonored guards E4: a SPECIFIED width on an inline-block is
// still honored exactly (shrink-to-fit applies only to auto width).
func TestInlineBlockSpecifiedWidthHonored(t *testing.T) {
	src := `<p style="margin:0;font:20px monospace">x<span style="display:inline-block;width:80px">B</span></p>`
	root := layoutTreeFor(t, src, 300, nil)
	p := descendToIFC(t, root)
	if len(p.Children) != 1 {
		t.Fatalf("want 1 inline-block atom, got %d", len(p.Children))
	}
	if ib := p.Children[0]; absf(ib.W-80) > 1e-6 {
		t.Errorf("specified-width inline-block W = %.1f, want 80", ib.W)
	}
}

// TestInlineBlockTextBaseline pins B2 (the F-F bug): a vertical-align:baseline
// inline-block WITH text aligns its last line box's baseline with the surrounding line's
// baseline (CSS 2.1 §10.8.1), instead of resting its whole border box on the baseline
// (which dropped the box too low and inflated the line). With matching fonts the
// inline-block's interior text baseline must coincide with the parent paragraph's line
// baseline. Mutation-verify: restore BaselinePt: frag.H and the inline-block's text drops
// far below the surrounding baseline (its top sits on the baseline instead).
func TestInlineBlockTextBaseline(t *testing.T) {
	// "X" then an inline-block containing "y", same font/size, on one line.
	src := `<p style="margin:0;font:16px monospace">X<span style="display:inline-block">y</span></p>`
	root := layoutTreeFor(t, src, 400, nil)
	// root -> body -> p. The p is an IFC with one line; the inline-block is an atomic child.
	p := descendToIFC(t, root)
	if len(p.Lines) == 0 {
		t.Fatalf("paragraph has no line box")
	}
	lineBaseline := p.Lines[0].BaselineY
	if len(p.Children) != 1 {
		t.Fatalf("want 1 atomic child (the inline-block), got %d", len(p.Children))
	}
	ib := p.Children[0]
	by, ok := lastInFlowLineBaseline(ib)
	if !ok {
		t.Fatalf("inline-block should have an in-flow line box (its text 'y')")
	}
	// The inline-block's own text baseline must align with the paragraph's line baseline.
	if d := absf(by - lineBaseline); d > 0.5 {
		t.Errorf("inline-block text baseline %.2f vs line baseline %.2f (delta %.2f); want aligned (~0)", by, lineBaseline, d)
	}
	// And the bug's signature must be ABSENT: the inline-block's TOP must NOT sit on the
	// baseline (that is the bottom-aligned bug — the whole box above the baseline).
	if absf(ib.Y-lineBaseline) < 0.5 {
		t.Errorf("inline-block top sits on the baseline (Y=%.2f ≈ baseline %.2f): the F-F bottom-aligned bug", ib.Y, lineBaseline)
	}
}

// descendToIFC returns the first fragment in root's subtree that has line boxes (an
// inline formatting context), for tests that assert on a paragraph's lines.
func descendToIFC(t *testing.T, root *Fragment) *Fragment {
	t.Helper()
	var find func(f *Fragment) *Fragment
	find = func(f *Fragment) *Fragment {
		if len(f.Lines) > 0 {
			return f
		}
		for _, c := range f.Children {
			if got := find(c); got != nil {
				return got
			}
		}
		return nil
	}
	f := find(root)
	if f == nil {
		t.Fatalf("no IFC fragment (with line boxes) found")
	}
	return f
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
				Kind:    cssbox.BoxReplaced,
				Display: cssbox.DisplayInline,
				Style: gcss.ComputedStyle{
					Display: "inline", FontSizePt: 16,
					Width: gcss.Length{Unit: gcss.UnitAuto}, Height: gcss.Length{Unit: gcss.UnitAuto},
					MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
				},
				Replaced: &cssbox.ReplacedContent{Tag: "img", Attrs: map[string]string{"width": "40", "height": "20"}},
			},
		},
	}
	e := New(nil, nil, nil)
	lines, h, atomics := e.layoutInline(context.Background(), ifc, 1000, 0, 0, 0, &floatContext{cbLeft: 0, cbRight: 1000})
	// A replaced box now builds its own border-box Fragment (carrying the decoded
	// image, or nil for a placeholder when no image decodes — here there is no loader,
	// so the image is nil but the box still reserves its attr-given 40x20 size).
	if len(atomics) != 1 {
		t.Fatalf("replaced atomics = %d, want 1 (the replaced box's fragment)", len(atomics))
	}
	frag := atomics[0]
	if frag.W != 40 || frag.H != 20 {
		t.Errorf("replaced fragment size = %vx%v, want 40x20 (from width/height attrs)", frag.W, frag.H)
	}
	if frag.Image == nil {
		t.Errorf("replaced fragment has no Image content")
	} else if frag.Image.Img != nil {
		t.Errorf("replaced fragment image = %v, want nil (no loader, placeholder)", frag.Image.Img)
	}
	if len(lines) != 1 {
		t.Fatalf("replaced lines = %d, want 1", len(lines))
	}
	// The line height covers the 20pt image (its atom ascent raises the line box).
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

// TestTextIndentPositive: a positive text-indent shifts the FIRST line's start-X to
// the right of a wrapped line's start-X; a wrapped (non-first) line is unindented.
// With no indent the two are equal.
func TestTextIndentPositive(t *testing.T) {
	// Narrow content box so the text wraps to >= 2 lines.
	src := reset + `<p style="width:60px;text-indent:20px">Hello wonderful wrapping world here</p>`
	p := layoutOne(t, src, 1000)
	if len(p.Lines) < 2 {
		t.Fatalf("wrapped lines = %d, want >= 2", len(p.Lines))
	}
	first := firstLineWithGlyphs(t, p).Glyphs[0].X
	wrapped := p.Lines[1].Glyphs[0].X
	if !(first > wrapped) {
		t.Errorf("indented first line X = %v, want > wrapped line X = %v", first, wrapped)
	}
	// The indent is exactly 20px past the wrapped line's (content-left) start.
	if d := first - wrapped; absf(d-20) > 1e-6 {
		t.Errorf("first-line indent = %v, want 20", d)
	}

	// Control: with no indent, first and wrapped lines share the same start-X.
	ctl := layoutOne(t, reset+`<p style="width:60px">Hello wonderful wrapping world here</p>`, 1000)
	if len(ctl.Lines) < 2 {
		t.Fatalf("control wrapped lines = %d, want >= 2", len(ctl.Lines))
	}
	cf := firstLineWithGlyphs(t, ctl).Glyphs[0].X
	cw := ctl.Lines[1].Glyphs[0].X
	if cf != cw {
		t.Errorf("no-indent first X = %v, wrapped X = %v, want equal", cf, cw)
	}
}

// TestTextIndentNegativeHanging: a negative text-indent (hanging indent) shifts the
// FIRST line's start-X to the LEFT of the wrapped lines' start-X.
func TestTextIndentNegativeHanging(t *testing.T) {
	// Give the paragraph left padding so the first line has room to hang leftward
	// without leaving the paragraph box (the wrapped lines start at content-left).
	src := reset + `<p style="width:60px;padding-left:30px;text-indent:-20px">Hello wonderful wrapping world here</p>`
	p := layoutOne(t, src, 1000)
	if len(p.Lines) < 2 {
		t.Fatalf("wrapped lines = %d, want >= 2", len(p.Lines))
	}
	first := firstLineWithGlyphs(t, p).Glyphs[0].X
	wrapped := p.Lines[1].Glyphs[0].X
	if !(first < wrapped) {
		t.Errorf("hanging first line X = %v, want < wrapped line X = %v", first, wrapped)
	}
	if d := wrapped - first; absf(d-20) > 1e-6 {
		t.Errorf("hanging indent = %v, want 20", d)
	}
}

// TestInlineBrForcesLineBreak: <br> lowers to a preserved-newline leaf and must
// split the paragraph onto two lines even in a wide viewport, with the second
// line's baseline strictly below the first (CSS "forced line break").
func TestInlineBrForcesLineBreak(t *testing.T) {
	p := layoutOne(t, reset+`<p>alpha<br>beta</p>`, 1000)
	if len(p.Lines) != 2 {
		t.Fatalf("paragraph lines = %d, want 2 (br must force a break)", len(p.Lines))
	}
	if p.Lines[1].BaselineY <= p.Lines[0].BaselineY {
		t.Errorf("second baseline %v not below first %v", p.Lines[1].BaselineY, p.Lines[0].BaselineY)
	}
	// The break itself contributes no ink: both lines carry glyphs.
	for i, ln := range p.Lines {
		if len(ln.Glyphs) == 0 {
			t.Errorf("line %d has no glyphs", i)
		}
	}
}

// TestInlineBrInsideFlexItem: the same forced break must survive inside a flex
// item's inline content (the htmldoc masthead tagline regression — the break was
// previously collapsed away by the parent-keyed whitespace pass).
func TestInlineBrInsideFlexItem(t *testing.T) {
	flexbox := layoutOne(t, reset+`<div style="display:flex"><p>alpha<br>beta</p></div>`, 1000)
	var lined *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if lined != nil {
			return
		}
		if len(f.Lines) > 0 {
			lined = f
			return
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(flexbox)
	if lined == nil {
		t.Fatal("no line-bearing fragment found under the flex container")
	}
	if len(lined.Lines) != 2 {
		t.Fatalf("flex-item paragraph lines = %d, want 2 (br must force a break)", len(lined.Lines))
	}
}

// TestMapTextAlignDirectionRelative: the physical keywords never flip; start/end
// resolve against the used direction. The initial value is "start", so an
// LTR document is byte-identical to the pre-RTL behavior (everything → AlignLeft).
func TestMapTextAlignDirectionRelative(t *testing.T) {
	cases := []struct {
		align, dir string
		want       inline.Align
	}{
		{"start", "ltr", inline.AlignLeft},
		{"start", "rtl", inline.AlignRight},
		{"end", "ltr", inline.AlignRight},
		{"end", "rtl", inline.AlignLeft},
		// Physical keywords are direction-invariant.
		{"left", "ltr", inline.AlignLeft},
		{"left", "rtl", inline.AlignLeft},
		{"right", "ltr", inline.AlignRight},
		{"right", "rtl", inline.AlignRight},
		{"center", "rtl", inline.AlignCenter},
		{"justify", "rtl", inline.AlignJustify},
		// Empty/unknown/empty-dir fall back to left (the pre-RTL default).
		{"", "ltr", inline.AlignLeft},
		{"bogus", "rtl", inline.AlignLeft},
		{"start", "", inline.AlignLeft},
	}
	for _, c := range cases {
		if got := mapTextAlign(c.align, c.dir); got != c.want {
			t.Errorf("mapTextAlign(%q, %q) = %v, want %v", c.align, c.dir, got, c.want)
		}
	}
}

// TestEffectiveDirectionAnonymousBox is the regression test for the anonymous-box
// trap: an anonymous block carries a ZERO-VALUE ComputedStyle, so its Direction is ""
// and NOT "ltr". Reading b.Style.Direction directly would silently lay the anonymous
// block out LTR inside an RTL subtree. effectiveDirection must recover the inherited
// value from an inline descendant (the cascade copies it onto every one).
func TestEffectiveDirectionAnonymousBox(t *testing.T) {
	anon := &cssbox.Box{
		Kind:       cssbox.BoxAnonBlock,
		Display:    cssbox.DisplayBlock,
		Formatting: cssbox.InlineFC,
		// Style deliberately left zero-valued, as box-gen produces it.
		Children: []*cssbox.Box{{
			Kind:  cssbox.BoxText,
			Style: gcss.ComputedStyle{Direction: "rtl", FontSizePt: 16},
		}},
	}
	if got := effectiveDirection(anon); got != "rtl" {
		t.Errorf("effectiveDirection(anon block in RTL subtree) = %q, want rtl", got)
	}
	if anon.Style.Direction != "" {
		t.Fatal("test premise broken: the anonymous box should have a zero-value Style")
	}

	// A real element box uses its own value.
	real := &cssbox.Box{Kind: cssbox.BoxBlock, Style: gcss.ComputedStyle{Direction: "rtl"}}
	if got := effectiveDirection(real); got != "rtl" {
		t.Errorf("effectiveDirection(element) = %q, want rtl", got)
	}
	// Nothing anywhere carries a direction: resolve to ltr, never "".
	bare := &cssbox.Box{Kind: cssbox.BoxAnonBlock}
	if got := effectiveDirection(bare); got != "ltr" {
		t.Errorf("effectiveDirection(bare) = %q, want ltr", got)
	}
}

// TestEffectiveTextAlignStartUnderRTL: start/end resolve through the same
// anonymous-box recovery as the direction itself.
func TestEffectiveTextAlignStartUnderRTL(t *testing.T) {
	anon := &cssbox.Box{
		Kind:       cssbox.BoxAnonBlock,
		Display:    cssbox.DisplayBlock,
		Formatting: cssbox.InlineFC,
		Children: []*cssbox.Box{{
			Kind:  cssbox.BoxText,
			Style: gcss.ComputedStyle{Direction: "rtl", TextAlign: "start", FontSizePt: 16},
		}},
	}
	if got := effectiveTextAlign(anon); got != inline.AlignRight {
		t.Errorf("text-align:start in an RTL anon block = %v, want AlignRight", got)
	}
}

// bidiGlyphOrder lays out an HTML fragment and returns the emitted glyphs sorted by
// their painted X position, as a string. That is the VISUAL reading order: what a
// reader sees left-to-right on the page.
//
// It asserts on Runes rather than pixels because no bundled font carries Hebrew or
// Arabic coverage (TeX Gyre + Inconsolata), so an RTL glyph has no outline to
// rasterize — but the shaper still records the rune it stood for, and reordering is
// exactly a question of which order those land in.
func bidiGlyphOrder(t *testing.T, bodyHTML string) string {
	t.Helper()
	src := `<!DOCTYPE html><html><head><style>body{margin:0;font-family:serif;font-size:16px}</style></head><body>` +
		bodyHTML + `</body></html>`
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	frag := New(nil, nil, nil).layoutTree(context.Background(), root, 400)

	type placed struct {
		x float64
		r []rune
	}
	var gs []placed
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		for li := range f.Lines {
			for _, g := range f.Lines[li].Glyphs {
				if len(g.Runes) > 0 {
					gs = append(gs, placed{g.X, g.Runes})
				}
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	sort.Slice(gs, func(i, j int) bool { return gs[i].x < gs[j].x })
	var out []rune
	for _, g := range gs {
		out = append(out, g.r...)
	}
	return string(out)
}

// TestBidiEndToEndLTRUnaffected pins that a Latin-only paragraph is painted in source
// order: the reorder must be a no-op for the overwhelmingly common case.
func TestBidiEndToEndLTRUnaffected(t *testing.T) {
	if got, want := bidiGlyphOrder(t, `<p>thequickbrownfox</p>`), "thequickbrownfox"; got != want {
		t.Errorf("painted order = %q, want %q", got, want)
	}
}

// TestBidiEndToEndRTLOverridePaintsReversed drives the reorder with an explicit
// RIGHT-TO-LEFT OVERRIDE over Latin characters, which isolates the reordering machinery
// from font coverage entirely: bidi ordering is decided by character properties.
func TestBidiEndToEndRTLOverridePaintsReversed(t *testing.T) {
	got := bidiGlyphOrder(t, "<p>\u202Eabcdef</p>")
	if want := "fedcba"; got != want {
		t.Errorf("painted order under RLO = %q, want %q (the run must paint reversed)", got, want)
	}
}

// TestBidiEndToEndHebrewIsland: real Hebrew inside an LTR paragraph paints
// right-to-left while the Latin around it does not. This works because the bundled
// Noto Sans Hebrew face is reached through per-rune script fallback — before that, the
// shaper DROPPED every Hebrew rune (no Latin substitute covers the script), so the
// text vanished before the reorder could see it.
func TestBidiEndToEndHebrewIsland(t *testing.T) {
	// Source order is "abc אבג def"; the Hebrew reads reversed on the page. Spaces
	// carry no Runes, so they do not appear in the comparison.
	if got, want := bidiGlyphOrder(t, `<p>abc אבג def</p>`), "abcגבאdef"; got != want {
		t.Errorf("painted order = %q, want %q", got, want)
	}
}

// TestBidiEndToEndHebrewParagraph: a wholly-Hebrew RTL paragraph reverses.
func TestBidiEndToEndHebrewParagraph(t *testing.T) {
	if got, want := bidiGlyphOrder(t, `<p dir="rtl">אבג</p>`), "גבא"; got != want {
		t.Errorf("painted order = %q, want %q", got, want)
	}
}

// TestBidiEndToEndArabicParagraph: Arabic reorders too. NOTE it still renders as
// disconnected ISOLATED forms — contextual shaping needs a cluster model (slice 4) —
// but the ORDER is correct, which is what this asserts.
func TestBidiEndToEndArabicParagraph(t *testing.T) {
	if got, want := bidiGlyphOrder(t, `<p dir="rtl">مرحبا</p>`), "ابحرم"; got != want {
		t.Errorf("painted order = %q, want %q", got, want)
	}
}

// lineRightEdge returns the rightmost x reached by any glyph on ln (its pen origin plus
// its advance) — the line's painted right extent.
func lineRightEdge(ln LineFragment) float64 {
	right := 0.0
	for _, g := range ln.Glyphs {
		if e := g.X + g.AdvancePt; e > right {
			right = e
		}
	}
	return right
}

// TestOverflowWrapBreaksLongToken is the end-to-end motivating case: an unbreakable
// token far wider than its box overflows by default and is broken by overflow-wrap.
func TestOverflowWrapBreaksLongToken(t *testing.T) {
	const token = "AAAAAAAAAAAAAAAAAAAA"

	plain := layoutOne(t, reset+`<p style="width:60px">`+token+`</p>`, 1000)
	if len(plain.Lines) != 1 {
		t.Fatalf("default lines = %d, want 1 (the token overflows unbroken)", len(plain.Lines))
	}
	if got := lineRightEdge(plain.Lines[0]); got <= plain.X+60 {
		t.Fatalf("default right edge = %v, want > %v — the premise is that it OVERFLOWS",
			got, plain.X+60)
	}

	for _, decl := range []string{
		"overflow-wrap:break-word", "word-wrap:break-word", // the legacy alias must work too
		"overflow-wrap:anywhere", "word-break:break-all",
	} {
		p := layoutOne(t, reset+`<p style="width:60px;`+decl+`">`+token+`</p>`, 1000)
		if len(p.Lines) < 2 {
			t.Errorf("%s: lines = %d, want >= 2 (the token must break)", decl, len(p.Lines))
			continue
		}
		for i, ln := range p.Lines {
			if len(ln.Glyphs) == 0 {
				continue
			}
			// Allow a hair of slack for the final glyph's advance vs. its ink.
			if right := lineRightEdge(ln); right > p.X+60+0.5 {
				t.Errorf("%s: line %d right edge = %v, want <= %v (must stay in the box)",
					decl, i, right, p.X+60)
			}
		}
	}
}

// TestOverflowWrapLastResortVsWordBreakEager is the CSS distinction end to end: a word
// that FITS on a line of its own must be moved down whole by overflow-wrap:break-word,
// but chopped at the line edge by word-break:break-all.
func TestOverflowWrapLastResortVsWordBreakEager(t *testing.T) {
	// "AAAAA" at the default 16pt serif is wider than the 60px line ONCE "xx " precedes
	// it, but fits on a line of its own — the exact window in which the two properties
	// disagree. The control below asserts that premise rather than assuming it, since it
	// depends on the bundled face's metrics.
	alone := layoutOne(t, reset+
		`<p style="width:60px;overflow-wrap:break-word">AAAAA</p>`, 1000)
	if len(alone.Lines) != 1 {
		t.Fatalf("premise broken: the token alone takes %d lines, want 1 (it must FIT alone "+
			"for the last-resort rule to be observable)", len(alone.Lines))
	}

	word := layoutOne(t, reset+
		`<p style="width:60px;overflow-wrap:break-word">xx AAAAA</p>`, 1000)
	if len(word.Lines) != 2 {
		t.Errorf("break-word lines = %d, want 2 (the word moves down WHOLE — break-word is "+
			"a last resort and must not split a word that fits alone)", len(word.Lines))
	}
	all := layoutOne(t, reset+
		`<p style="width:60px;word-break:break-all">xx AAAAA</p>`, 1000)
	if len(all.Lines) < 2 {
		t.Fatalf("break-all lines = %d, want >= 2", len(all.Lines))
	}
	// break-all fills the first line to the edge, so its first line carries MORE glyphs
	// than break-word's (which stops after "xx").
	if len(all.Lines[0].Glyphs) <= len(word.Lines[0].Glyphs) {
		t.Errorf("break-all first line has %d glyphs, break-word %d — break-all must fill "+
			"the line eagerly rather than deferring the word",
			len(all.Lines[0].Glyphs), len(word.Lines[0].Glyphs))
	}
}

// TestKeepAllSuppressesOverflowWrapEndToEnd: word-break:keep-all forbids breaking the
// affected text, which overrides overflow-wrap.
func TestKeepAllSuppressesOverflowWrapEndToEnd(t *testing.T) {
	p := layoutOne(t, reset+
		`<p style="width:60px;word-break:keep-all;overflow-wrap:break-word">AAAAAAAAAAAAAAAAAAAA</p>`, 1000)
	if len(p.Lines) != 1 {
		t.Errorf("keep-all lines = %d, want 1 (breaking suppressed)", len(p.Lines))
	}
}

// TestOverflowWrapInheritsToInlineDescendants: the properties are inherited, so setting
// break-word on a container breaks a long token inside a nested <span>.
func TestOverflowWrapInheritsToInlineDescendants(t *testing.T) {
	p := layoutOne(t, reset+
		`<p style="width:60px;overflow-wrap:break-word">x <span>AAAAAAAAAAAAAAAAAAAA</span></p>`, 1000)
	if len(p.Lines) < 3 {
		t.Errorf("lines = %d, want >= 3 (the span's token must break, inheriting the property)",
			len(p.Lines))
	}
}

// TestLongURLBreaksInNarrowPanel is the downstream case this feature exists for: a fetch
// error whose URL is one unbroken token must stay inside its panel rather than running
// off the edge, or the diagnostic is unreadable exactly when it matters.
func TestLongURLBreaksInNarrowPanel(t *testing.T) {
	const msg = `ical(Personal): GET https://calendar.example.com/very/long/path/to/a/private/feed.ics: 401 Unauthorized`
	p := layoutOne(t, reset+
		`<p style="width:200px;overflow-wrap:break-word">`+msg+`</p>`, 1000)
	if len(p.Lines) < 3 {
		t.Fatalf("lines = %d, want >= 3", len(p.Lines))
	}
	for i, ln := range p.Lines {
		if len(ln.Glyphs) == 0 {
			continue
		}
		if right := lineRightEdge(ln); right > p.X+200+0.5 {
			t.Errorf("line %d right edge = %v, want <= %v — the URL escaped the panel",
				i, right, p.X+200)
		}
	}
}
