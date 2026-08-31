package draw

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/raster"
	"github.com/nathanstitt/omnidoc/pkg/internal/svg"
	"github.com/nathanstitt/omnidoc/pkg/render"
)

// textExtent returns the placed run's total advance (last pen edge minus first
// pen origin) and the placed glyphs, for the single <text> in src.
//
// It measures the SAME quantity the anchor pass and the decoration pass read,
// which is what makes an assertion on it discriminating: a letter-spacing that
// moved glyphs without changing the recorded advance would leave every
// downstream consumer wrong, and this catches it.
func textExtent(t *testing.T, src string) (float64, []placedGlyph) {
	t.Helper()
	doc, _ := parseSVG(t, src)
	placed := New(doc).layoutText(firstText(t, doc))
	if len(placed) == 0 {
		t.Fatal("no glyphs placed")
	}
	// The pen SPAN — first origin to the last glyph's advance edge — not the
	// ink bounding box. They differ whenever glyphs overlap, which a
	// textLength smaller than the natural width makes them do: with negative
	// gaps an interior glyph sticks out further right than the last one, so a
	// max-based measure would report a width the pen never walked.
	last := len(placed) - 1
	return placed[last].penX + placed[last].advance - placed[0].penX, placed
}

const spacingTmpl = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
  font-family="sans-serif" font-size="20"><text x="10" y="100"%s>%s</text></svg>`

// TestLetterSpacingWidensByExactlyTheGaps pins the one detail SVG 1.1 and CSS
// Text 3 disagree on: whether the spacing is added after the LAST glyph too.
//
// resvg — whose reference PNGs are this corpus's ground truth — follows CSS and
// adds it only BETWEEN glyphs, and states so in
// letter-spacing/filter-bbox.svg's own <desc>. So an n-glyph run must widen by
// exactly (n-1)*spacing, not n*spacing. The test asserts the exact arithmetic
// rather than "wider than before", which would pass for either rule.
func TestLetterSpacingWidensByExactlyTheGaps(t *testing.T) {
	const text = "Text"
	base, basePlaced := textExtent(t, fmt.Sprintf(spacingTmpl, "", text))
	n := len(basePlaced)
	if n != len(text) {
		t.Fatalf("shaped %d glyphs for %q; the arithmetic below assumes one glyph per character", n, text)
	}

	for _, spacing := range []float64{3, -1.5, 12} {
		got, placed := textExtent(t, fmt.Sprintf(spacingTmpl,
			fmt.Sprintf(" letter-spacing=%q", fmt.Sprint(spacing)), text))
		want := base + float64(n-1)*spacing
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("letter-spacing=%v: total advance = %v, want %v (base %v + %d gaps)", spacing, got, want, base, n-1)
		}
		// And the gaps really are between glyphs, not folded into the first
		// or last: every consecutive pen delta grows by exactly `spacing`.
		for i := 1; i < len(placed); i++ {
			gotStep := placed[i].penX - placed[i-1].penX
			wantStep := basePlaced[i].penX - basePlaced[i-1].penX + spacing
			if math.Abs(gotStep-wantStep) > 1e-9 {
				t.Errorf("letter-spacing=%v: step %d = %v, want %v", spacing, i, gotStep, wantStep)
			}
		}
	}
}

// TestLetterSpacingAddsNoTrailingSpace is the discriminating half of the rule
// above, isolated: the LAST glyph's advance must be untouched, so the run's
// right edge sits flush with the final glyph exactly as it does with no
// spacing at all. An implementation adding the spacing after every glyph
// (SVG 1.1's literal wording) fails only this assertion.
func TestLetterSpacingAddsNoTrailingSpace(t *testing.T) {
	_, base := textExtent(t, fmt.Sprintf(spacingTmpl, "", "Text"))
	_, spaced := textExtent(t, fmt.Sprintf(spacingTmpl, ` letter-spacing="30"`, "Text"))
	if len(base) != len(spaced) || len(base) == 0 {
		t.Fatalf("glyph counts differ: %d vs %d", len(base), len(spaced))
	}
	last := len(base) - 1
	if base[last].advance != spaced[last].advance {
		t.Errorf("last glyph advance = %v with spacing, %v without; letter-spacing must not extend the trailing edge",
			spaced[last].advance, base[last].advance)
	}
}

// TestWordSpacingAddsAtEachSpace asserts word-spacing widens by exactly one
// increment per SPACE character and is inert on a run with none — the
// distinction from letter-spacing, which counts inter-glyph gaps instead.
func TestWordSpacingAddsAtEachSpace(t *testing.T) {
	const text = "a b c" // two spaces
	base, _ := textExtent(t, fmt.Sprintf(spacingTmpl, "", text))
	for _, ws := range []float64{7, -2} {
		got, _ := textExtent(t, fmt.Sprintf(spacingTmpl,
			fmt.Sprintf(" word-spacing=%q", fmt.Sprint(ws)), text))
		want := base + 2*ws
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("word-spacing=%v: total advance = %v, want %v (base %v + 2 spaces)", ws, got, want, base)
		}
	}

	// No spaces: the property must change nothing at all, however large.
	noSpaceBase, _ := textExtent(t, fmt.Sprintf(spacingTmpl, "", "abc"))
	noSpaceGot, _ := textExtent(t, fmt.Sprintf(spacingTmpl, ` word-spacing="100"`, "abc"))
	if math.Abs(noSpaceGot-noSpaceBase) > 1e-9 {
		t.Errorf("word-spacing on a space-free run changed the advance: %v vs %v", noSpaceGot, noSpaceBase)
	}
}

// TestSpacingPercentageResolvesAgainstFontSize covers the unit handling: a
// percentage on letter-spacing is a fraction of the element's OWN font-size,
// not of the viewport or of the UA default. 25% of 20pt must equal a literal 5.
func TestSpacingPercentageResolvesAgainstFontSize(t *testing.T) {
	pct, _ := textExtent(t, fmt.Sprintf(spacingTmpl, ` letter-spacing="25%"`, "Text"))
	abs, _ := textExtent(t, fmt.Sprintf(spacingTmpl, ` letter-spacing="5"`, "Text"))
	if math.Abs(pct-abs) > 1e-9 {
		t.Errorf("letter-spacing=25%% of 20pt gave advance %v; letter-spacing=5 gave %v", pct, abs)
	}
}

// TestSpacingIsPerCharacterStyle asserts a <tspan>'s own letter-spacing
// applies to ITS characters only, leaving the surrounding text at the parent's
// value — resvg's mixed-spacing.svg. The check is arithmetic: the run must
// widen by the inner value across the tspan's gaps and the outer value across
// the rest, which distinguishes real per-character resolution from taking the
// <text>'s value once for the whole node.
func TestSpacingIsPerCharacterStyle(t *testing.T) {
	base, basePlaced := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  font-family="sans-serif" font-size="20"><text x="10" y="100">abcdef</text></svg>`)
	if len(basePlaced) != 6 {
		t.Fatalf("shaped %d glyphs, want 6", len(basePlaced))
	}
	got, _ := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  font-family="sans-serif" font-size="20"><text x="10" y="100" letter-spacing="2"
	  >ab<tspan letter-spacing="10">cd</tspan>ef</text></svg>`)

	// Each gap takes the value of the character BEFORE it, and the final glyph
	// carries none: a|b=2, b|c=2, c|d=10, d|e=10, e|f=2, f|end=0.
	want := base + 2 + 2 + 10 + 10 + 2
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("mixed letter-spacing: total advance = %v, want %v (base %v)", got, want, base)
	}
}

const lengthTmpl = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
  font-family="sans-serif" font-size="20"><text x="10" y="100"%s>%s</text></svg>`

// TestTextLengthHitsTheRequestedAdvance asserts the whole point of the
// property: whatever the natural width, the placed run occupies EXACTLY the
// requested advance. Both lengthAdjust modes must satisfy it, both above and
// below the natural width.
func TestTextLengthHitsTheRequestedAdvance(t *testing.T) {
	natural, _ := textExtent(t, fmt.Sprintf(lengthTmpl, "", "Text"))
	if natural <= 0 {
		t.Fatal("zero natural width")
	}
	for _, adjust := range []string{"", ` lengthAdjust="spacing"`, ` lengthAdjust="spacingAndGlyphs"`} {
		for _, target := range []float64{natural * 3, natural / 3, 150} {
			attrs := fmt.Sprintf(` textLength=%q%s`, fmt.Sprint(target), adjust)
			got, _ := textExtent(t, fmt.Sprintf(lengthTmpl, attrs, "Text"))
			if math.Abs(got-target) > 1e-6 {
				t.Errorf("textLength=%v%s: advance = %v, want exactly %v (natural %v)", target, adjust, got, target, natural)
			}
		}
	}
}

// TestTextLengthZero pins the degenerate-but-legal case: textLength="0"
// collapses the run onto a point. resvg's zero.svg renders the glyphs stacked
// on top of one another rather than dropping them, so the placement must still
// produce every glyph.
func TestTextLengthZero(t *testing.T) {
	got, placed := textExtent(t, fmt.Sprintf(lengthTmpl, ` textLength="0"`, "Text"))
	if len(placed) != 4 {
		t.Fatalf("placed %d glyphs, want 4 (a zero textLength must not drop them)", len(placed))
	}
	if math.Abs(got) > 1e-6 {
		t.Errorf("textLength=0: advance = %v, want 0", got)
	}
}

// TestTextLengthNegativeIsIgnored covers SVG's error handling: a negative
// textLength is invalid and the text renders at its natural width, NOT
// mirrored, collapsed, or dropped (resvg's negative.svg).
func TestTextLengthNegativeIsIgnored(t *testing.T) {
	natural, _ := textExtent(t, fmt.Sprintf(lengthTmpl, "", "Text"))
	got, _ := textExtent(t, fmt.Sprintf(lengthTmpl, ` textLength="-50"`, "Text"))
	if math.Abs(got-natural) > 1e-9 {
		t.Errorf("negative textLength: advance = %v, want the natural %v", got, natural)
	}
}

// TestTextLengthSingleCharacter covers the degenerate range. A one-glyph
// range has NO interior gap for lengthAdjust="spacing" to distribute into, so
// the request cannot be satisfied and the glyph keeps its natural advance.
//
// It asserts the OUTCOME, not the mechanism. Two things independently produce
// it: applyTextLengths' explicit n <= 0 guard, and the loop below it, whose
// i == hi-1 branch zeroes the only gap index a one-glyph range has before the
// (target-natural)/0 value could ever be stored. So the guard is
// defence-in-depth rather than the thing standing between this fixture and a
// non-finite pen position — deleting it would leave this test passing. The
// finiteness check stays because it is the property that actually matters, and
// TestSpacingAndLengthSurviveHostileInput sweeps it far more widely.
func TestTextLengthSingleCharacter(t *testing.T) {
	natural, _ := textExtent(t, fmt.Sprintf(lengthTmpl, "", "T"))
	got, placed := textExtent(t, fmt.Sprintf(lengthTmpl, ` textLength="150"`, "T"))
	if len(placed) != 1 {
		t.Fatalf("placed %d glyphs, want 1", len(placed))
	}
	if math.IsNaN(placed[0].penX) || math.IsInf(placed[0].penX, 0) {
		t.Fatalf("single-character textLength produced a non-finite pen X: %v", placed[0].penX)
	}
	if math.Abs(got-natural) > 1e-9 {
		t.Errorf("single-character textLength=spacing: advance = %v, want the natural %v", got, natural)
	}

	// spacingAndGlyphs CAN satisfy it, because it scales the glyph itself.
	scaled, _ := textExtent(t, fmt.Sprintf(lengthTmpl, ` textLength="150" lengthAdjust="spacingAndGlyphs"`, "T"))
	if math.Abs(scaled-150) > 1e-6 {
		t.Errorf("single-character spacingAndGlyphs: advance = %v, want 150", scaled)
	}
}

// TestLengthAdjustSpacingAndGlyphsScalesOutlines is the assertion that
// separates the two lengthAdjust modes on GEOMETRY, not on total advance —
// both modes hit the same total, so an advance-only test would pass for a
// broken spacingAndGlyphs that merely opened gaps.
//
// It measures the actual painted OUTLINE width of the first glyph: under
// "spacing" it must be unchanged, and under "spacingAndGlyphs" it must be
// stretched by target/natural.
func TestLengthAdjustSpacingAndGlyphsScalesOutlines(t *testing.T) {
	firstGlyphWidth := func(attrs string) float64 {
		t.Helper()
		doc, _ := parseSVG(t, fmt.Sprintf(lengthTmpl, attrs, "Text"))
		placed := New(doc).layoutText(firstText(t, doc))
		if len(placed) == 0 || placed[0].glyph.Outline == nil {
			t.Fatal("no outline on the first glyph")
		}
		dp := render.TransformPath(placed[0].glyph.Outline, placed[0].matrix(render.Identity))
		x0, _, x1, _, ok := dp.Bounds()
		if !ok {
			t.Fatal("first glyph outline has no bounds")
		}
		return x1 - x0
	}

	natural, _ := textExtent(t, fmt.Sprintf(lengthTmpl, "", "Text"))
	const target = 200.0
	scale := target / natural

	base := firstGlyphWidth("")
	spacing := firstGlyphWidth(fmt.Sprintf(` textLength="%v" lengthAdjust="spacing"`, target))
	both := firstGlyphWidth(fmt.Sprintf(` textLength="%v" lengthAdjust="spacingAndGlyphs"`, target))

	if math.Abs(spacing-base) > 1e-6 {
		t.Errorf("lengthAdjust=spacing changed the glyph outline width: %v, want the natural %v", spacing, base)
	}
	if math.Abs(both-base*scale) > 1e-3 {
		t.Errorf("lengthAdjust=spacingAndGlyphs glyph width = %v, want %v (natural %v scaled by %v)", both, base*scale, base, scale)
	}
	if math.Abs(both-base) < 1e-6 {
		t.Error("lengthAdjust=spacingAndGlyphs left the outline untouched; it must scale the glyphs, not only the gaps")
	}
}

// TestNestedSpacingAndGlyphsKeepsOutlineAndAdvanceInStep is the nesting case
// the non-nested lengthAdjust test cannot reach.
//
// Requests are applied outermost-first, so an inner spacingAndGlyphs request
// sees advances the outer one already scaled and its own factor composes on
// top. Both the advance AND the outline scale must compound; assigning xScale
// instead of multiplying it leaves the outline carrying only the inner factor
// while the advance carries the product, and the glyphs then render several
// times too narrow inside their own advance boxes.
//
// The assertion is the INVARIANT, not either number on its own: whatever the
// two requests are, a glyph's outline must have been scaled by exactly the
// same factor as its advance. That is what a desynchronized pair violates and
// a correct one cannot.
func TestNestedSpacingAndGlyphsKeepsOutlineAndAdvanceInStep(t *testing.T) {
	const inner = "Text"
	// The natural, unadjusted per-glyph advances, to derive the true factor.
	refDoc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  font-family="sans-serif" font-size="20"><text x="10" y="100"><tspan>`+inner+`</tspan></text></svg>`)
	ref := New(refDoc).layoutText(firstText(t, refDoc))
	if len(ref) != len(inner) {
		t.Fatalf("reference shaped %d glyphs, want %d", len(ref), len(inner))
	}

	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  font-family="sans-serif" font-size="20"><text x="10" y="100" textLength="300"
	  lengthAdjust="spacingAndGlyphs"><tspan textLength="20"
	  lengthAdjust="spacingAndGlyphs">`+inner+`</tspan></text></svg>`)
	got := New(doc).layoutText(firstText(t, doc))
	if len(got) != len(ref) {
		t.Fatalf("nested run shaped %d glyphs, want %d", len(got), len(ref))
	}

	for i := range got {
		if ref[i].advance <= 0 {
			continue // a zero-advance glyph cannot express a ratio
		}
		advanceRatio := got[i].advance / ref[i].advance
		if math.Abs(got[i].xScale-advanceRatio) > 1e-9 {
			t.Errorf("glyph %d: outline scaled %vx but its advance scaled %vx; the two must compound identically (an inner spacingAndGlyphs must MULTIPLY the outer scale, not replace it)",
				i, got[i].xScale, advanceRatio)
		}
	}

	// And the inner request still wins on the total, which is what proves the
	// two requests were genuinely composed rather than one being dropped.
	last := len(got) - 1
	if total := got[last].penX + got[last].advance - got[0].penX; math.Abs(total-20) > 1e-6 {
		t.Errorf("nested total advance = %v, want the inner tspan's 20", total)
	}
}

// TestTextLengthOnTspanWinsOverText covers the nesting rule: an inner
// <tspan>'s textLength governs its own characters, overriding an outer
// request that also covers them (resvg's on-text-and-tspan.svg).
func TestTextLengthOnTspanWinsOverText(t *testing.T) {
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  font-family="sans-serif" font-size="20"><text textLength="500"
	  ><tspan x="10" y="100" textLength="50">Text</tspan></text></svg>`)
	placed := New(doc).layoutText(firstText(t, doc))
	if len(placed) == 0 {
		t.Fatal("no glyphs placed")
	}
	last := len(placed) - 1
	if got := placed[last].penX + placed[last].advance - placed[0].penX; math.Abs(got-50) > 1e-6 {
		t.Errorf("advance = %v, want the tspan's 50 (not the text's 500)", got)
	}
}

const baselineTmpl = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
  font-family="sans-serif" font-size="40"><text x="10" y="100"%s>Text</text></svg>`

// TestBaselineKeywordsShiftInTheRightDirection asserts each supported keyword
// against the face's OWN metrics, taken from the shaped glyph rather than
// hardcoded — so the test states the RULE (hanging drops the baseline by a
// full ascent, text-after-edge raises it by the descent) rather than a number
// that would silently stop meaning anything if the bundled face changed.
func TestBaselineKeywordsShiftInTheRightDirection(t *testing.T) {
	// The unshifted reference, and the metrics every expectation derives from.
	doc, _ := parseSVG(t, fmt.Sprintf(baselineTmpl, ""))
	ref := New(doc).layoutText(firstText(t, doc))
	if len(ref) == 0 {
		t.Fatal("no glyphs placed")
	}
	g := ref[0].glyph
	baseY := ref[0].penY
	if g.AscentPt <= 0 || g.DescentPt <= 0 {
		t.Fatalf("face exposed no usable metrics (ascent %v, descent %v); the expectations below would be vacuous", g.AscentPt, g.DescentPt)
	}

	for _, tc := range []struct {
		prop, value string
		want        float64 // downward offset from baseY
	}{
		{"dominant-baseline", "auto", 0},
		{"dominant-baseline", "alphabetic", 0},
		{"dominant-baseline", "hanging", g.AscentPt},
		{"dominant-baseline", "text-before-edge", g.AscentPt},
		{"dominant-baseline", "text-after-edge", -g.DescentPt},
		{"dominant-baseline", "central", (g.AscentPt - g.DescentPt) / 2},
		{"dominant-baseline", "middle", g.SizePt * 0.25},
		{"alignment-baseline", "hanging", g.AscentPt},
		{"alignment-baseline", "before-edge", g.AscentPt},
		{"alignment-baseline", "after-edge", -g.DescentPt},
		{"alignment-baseline", "central", (g.AscentPt - g.DescentPt) / 2},
		// The two that DEGRADE: no OS/2 or BASE table is parsed, so both fall
		// back to the alphabetic baseline rather than being approximated.
		{"dominant-baseline", "ideographic", 0},
		{"dominant-baseline", "mathematical", 0},
	} {
		name := tc.prop + "=" + tc.value
		attrs := fmt.Sprintf(" %s=%q", tc.prop, tc.value)
		d, _ := parseSVG(t, fmt.Sprintf(baselineTmpl, attrs))
		placed := New(d).layoutText(firstText(t, d))
		if len(placed) == 0 {
			t.Fatalf("%s: no glyphs placed", name)
		}
		if got := placed[0].penY - baseY; math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: baseline moved %v (down positive), want %v", name, got, tc.want)
		}
	}

	// And the directions are genuinely opposite, so a sign error cannot hide.
	if g.AscentPt <= 0 || -g.DescentPt >= 0 {
		t.Fatal("the ascent/descent expectations above do not have opposite signs; the test is not discriminating")
	}
}

// TestAlignmentBaselineWinsOverDominantBaseline pins the precedence: with both
// set, alignment-baseline decides (resvg's
// dominant-baseline/different-alignment-baseline-on-tspan.svg).
func TestAlignmentBaselineWinsOverDominantBaseline(t *testing.T) {
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="40"><text x="10" y="100"
	  dominant-baseline="hanging" alignment-baseline="alphabetic">Text</text></svg>`)
	placed := New(doc).layoutText(firstText(t, doc))
	if len(placed) == 0 {
		t.Fatal("no glyphs placed")
	}
	if placed[0].penY != 100 {
		t.Errorf("penY = %v, want 100: alignment-baseline=alphabetic must beat dominant-baseline=hanging", placed[0].penY)
	}
}

// TestAlignmentBaselineBaselineDefersRatherThanResetting is the subtle half of
// the keyword set: "baseline" means "use my parent box's dominant baseline",
// NOT "use the alphabetic baseline". resvg's
// dominant-baseline/alignment-baseline=baseline-on-tspan.svg renders the tspan
// FLUSH with its middle-baselined siblings, which only happens if the keyword
// defers. A mapping of "baseline" onto "alphabetic" fails here and nowhere
// else.
func TestAlignmentBaselineBaselineDefersRatherThanResetting(t *testing.T) {
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="40"><text x="10" y="100" dominant-baseline="middle"
	  >ab<tspan alignment-baseline="baseline">cd</tspan></text></svg>`)
	placed := New(doc).layoutText(firstText(t, doc))
	if len(placed) != 4 {
		t.Fatalf("placed %d glyphs, want 4", len(placed))
	}
	if placed[0].penY != placed[3].penY {
		t.Errorf("tspan baseline %v differs from its parent's %v; alignment-baseline=baseline must defer, not reset to alphabetic",
			placed[3].penY, placed[0].penY)
	}
	if placed[0].penY == 100 {
		t.Error("dominant-baseline=middle did not move the baseline at all; the test is not discriminating")
	}
}

// TestBaselineShiftAccumulatesThroughNesting is the cumulative-shift claim: a
// shift inside a shift ADDS (SVG2 11.10.2), so two nested 20% shifts land at
// 40%, not 20%. It also pins the "baseline" keyword's behaviour, which
// contributes zero WITHOUT resetting the accumulation — resvg's
// nested-with-baseline-1.svg renders 20%+baseline+20% exactly on top of
// 20%+20%.
func TestBaselineShiftAccumulatesThroughNesting(t *testing.T) {
	const size = 40.0
	penYOf := func(src string) float64 {
		t.Helper()
		doc, _ := parseSVG(t, src)
		placed := New(doc).layoutText(firstText(t, doc))
		if len(placed) == 0 {
			t.Fatal("no glyphs placed")
		}
		return placed[len(placed)-1].penY
	}
	wrap := func(body string) string {
		return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
		  font-family="sans-serif" font-size="40"><text x="10" y="100">` + body + `</text></svg>`
	}

	single := penYOf(wrap(`<tspan baseline-shift="20%">T</tspan>`))
	if want := 100 - 0.20*size; math.Abs(single-want) > 1e-9 {
		t.Fatalf("one 20%% shift: penY = %v, want %v (a percentage resolves against font-size, positive = up)", single, want)
	}

	nested := penYOf(wrap(`<tspan baseline-shift="20%"><tspan baseline-shift="20%">T</tspan></tspan>`))
	if want := 100 - 0.40*size; math.Abs(nested-want) > 1e-9 {
		t.Errorf("two nested 20%% shifts: penY = %v, want %v (they must ADD, not replace)", nested, want)
	}

	// "baseline" contributes zero but does not reset what is already there.
	withBaseline := penYOf(wrap(`<tspan baseline-shift="20%"><tspan baseline-shift="baseline"><tspan baseline-shift="20%">T</tspan></tspan></tspan>`))
	if math.Abs(withBaseline-nested) > 1e-9 {
		t.Errorf("20%%+baseline+20%%: penY = %v, want the same %v as 20%%+20%%", withBaseline, nested)
	}
}

// TestBaselineShiftSubAndSuperGoOppositeWays asserts the two keywords move in
// opposite directions from the unshifted baseline — the one property of them
// that is not a magic number, and the one a sign error would break.
func TestBaselineShiftSubAndSuperGoOppositeWays(t *testing.T) {
	penYOf := func(shift string) float64 {
		t.Helper()
		doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
		  font-family="sans-serif" font-size="40"><text x="10" y="100"><tspan baseline-shift="`+
			shift+`">T</tspan></text></svg>`)
		placed := New(doc).layoutText(firstText(t, doc))
		if len(placed) == 0 {
			t.Fatal("no glyphs placed")
		}
		return placed[0].penY
	}
	sub, super := penYOf("sub"), penYOf("super")
	if !(super < 100) {
		t.Errorf("super moved the baseline to %v; it must go UP (below 100 in SVG's Y-down space)", super)
	}
	if !(sub > 100) {
		t.Errorf("sub moved the baseline to %v; it must go DOWN", sub)
	}
}

// TestBaselineShiftOnTextItselfIsIgnored pins resvg's behaviour, which the
// suite asserts three separate ways (baseline-shift/inheritance-1, -4 and -5):
// a baseline-shift written on the <text> element does NOT move its text. The
// property only takes effect from a <tspan> inward.
func TestBaselineShiftOnTextItselfIsIgnored(t *testing.T) {
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="40"><text x="10" y="100" baseline-shift="super">Text</text></svg>`)
	placed := New(doc).layoutText(firstText(t, doc))
	if len(placed) == 0 {
		t.Fatal("no glyphs placed")
	}
	if placed[0].penY != 100 {
		t.Errorf("penY = %v, want 100: baseline-shift on the <text> itself must not move it", placed[0].penY)
	}
}

// decorationSpy is a render.Device that records every Fill it receives, so a
// test can assert what colour a decoration rule was painted in.
type decorationSpy struct {
	render.Device
	fills []struct {
		bounds [4]float64
		color  color.RGBA
	}
	drawGlyphCalls int
}

func (d *decorationSpy) Fill(p *render.Path, paint render.FillPaint) {
	x0, y0, x1, y1, ok := p.Bounds()
	if !ok {
		return
	}
	d.fills = append(d.fills, struct {
		bounds [4]float64
		color  color.RGBA
	}{[4]float64{x0, y0, x1, y1}, paint.Color})
}

// FillGlyph swallows glyph outlines so d.fills holds ONLY decoration rules:
// paintGlyphFill routes a nonzero-winding glyph fill through FillGlyph, and a
// decoration rule always goes through Fill, so the two never mix. That
// separation is what lets the assertions below count rules exactly.
func (d *decorationSpy) FillGlyph(*render.Path, render.FillColor, string) {}

func (d *decorationSpy) DrawGlyph(render.GlyphRef) { d.drawGlyphCalls++ }

// renderToSpy renders src's whole scene through a decorationSpy and returns
// it. The spy wraps a real raster device so every other operation behaves
// normally — only Fill/FillGlyph/DrawGlyph are observed.
func renderToSpy(t *testing.T, src string) *decorationSpy {
	t.Helper()
	doc, _ := parseSVG(t, src)
	img := image.NewRGBA(image.Rect(0, 0, 400, 400))
	spy := &decorationSpy{Device: raster.New(img)}
	New(doc).DrawVector(spy, render.Identity)
	return spy
}

// TestDecorationPaintComesFromTheDeclaringElement is the subtlest rule in this
// tranche, and the one every naive implementation gets wrong: a
// text-decoration paints in the fill of the element that DECLARED it, not of
// the descendant characters it covers.
//
// The fixture is deliberately discriminating — the declaring <text> is RED and
// the covered <tspan> is BLUE, two colours neither of which is a default — so
// an implementation that took the covered character's paint produces blue and
// fails, and one that took some inherited default produces black and fails.
// resvg's style-resolving-1.svg and style-resolving-3.svg assert both
// directions of this.
func TestDecorationPaintComesFromTheDeclaringElement(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}

	spy := renderToSpy(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="40"><text x="10" y="100" fill="red"
	  text-decoration="underline"><tspan fill="blue">Text</tspan></text></svg>`)

	if len(spy.fills) != 1 {
		t.Fatalf("got %d filled rules, want exactly 1 underline", len(spy.fills))
	}
	if got := spy.fills[0].color; got != red {
		t.Errorf("underline painted %v, want the DECLARING <text>'s red %v", got, red)
	}
	if spy.fills[0].color == blue {
		t.Error("underline took the covered <tspan>'s blue; it must take the declaring element's paint")
	}

	// The reverse: declared on the tspan, painted in the tspan's colour even
	// though the <text> around it is red.
	spy = renderToSpy(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="40"><text x="10" y="100" fill="red"
	  ><tspan fill="blue" text-decoration="underline">Text</tspan></text></svg>`)
	if len(spy.fills) != 1 {
		t.Fatalf("got %d filled rules, want exactly 1", len(spy.fills))
	}
	if got := spy.fills[0].color; got != blue {
		t.Errorf("underline painted %v, want the declaring <tspan>'s blue %v", got, blue)
	}
}

// TestDecorationOutsideTheTextElementAdoptsTheTextsPaint pins the one place
// the "declaring element" rule bends: a decoration inherited from an ANCESTOR
// of the <text> keeps its line but adopts the <text>'s paint.
//
// resvg's outside-the-text-element.svg is the discriminating case — a green
// <g> wrapping a black <text> renders a BLACK underline — and
// style-resolving-2.svg repeats it with a red <g> around a yellow <text>.
func TestDecorationOutsideTheTextElementAdoptsTheTextsPaint(t *testing.T) {
	spy := renderToSpy(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="40"><g fill="green" text-decoration="underline"
	  ><text x="10" y="100" fill="black">Text</text></g></svg>`)
	if len(spy.fills) != 1 {
		t.Fatalf("got %d filled rules, want exactly 1", len(spy.fills))
	}
	black := color.RGBA{0, 0, 0, 255}
	green := color.RGBA{0, 128, 0, 255}
	if got := spy.fills[0].color; got != black {
		t.Errorf("underline painted %v, want the <text>'s black %v (not the <g>'s %v)", got, black, green)
	}
}

// TestDecorationSpansTheWholeDeclaringElement asserts the EXTENT rule: a
// decoration declared on a <text> covers every character underneath it,
// crossing <tspan> boundaries, rather than stopping at the first style change.
func TestDecorationSpansTheWholeDeclaringElement(t *testing.T) {
	spy := renderToSpy(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="40"><text x="10" y="100" text-decoration="underline"
	  >ab<tspan fill="blue">cd</tspan>ef</text></svg>`)
	if len(spy.fills) != 1 {
		t.Fatalf("got %d filled rules, want 1 spanning the whole <text> (a decoration must not split at a tspan boundary)", len(spy.fills))
	}
	// And it really reaches the last character: measure the undecorated run's
	// extent and require the rule to cover it.
	total, placed := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="40"><text x="10" y="100">abcdef</text></svg>`)
	_ = placed
	b := spy.fills[0].bounds
	if got := b[2] - b[0]; math.Abs(got-total) > 1.0 {
		t.Errorf("underline width = %v, want ~%v (the whole run)", got, total)
	}
}

// TestDecorationLinesSitInTheRightPlaces asserts underline, overline, and
// line-through land below, above, and across the text respectively — the
// direction check that a swapped offset would break.
func TestDecorationLinesSitInTheRightPlaces(t *testing.T) {
	const baseline = 100.0
	for _, tc := range []struct {
		line string
		want string // "below", "above", "across"
	}{
		{"underline", "below"},
		{"overline", "above"},
		{"line-through", "across"},
	} {
		spy := renderToSpy(t, fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
		  font-family="sans-serif" font-size="40"><text x="10" y="100" text-decoration=%q>Text</text></svg>`, tc.line))
		if len(spy.fills) != 1 {
			t.Fatalf("%s: got %d rules, want 1", tc.line, len(spy.fills))
		}
		top, bot := spy.fills[0].bounds[1], spy.fills[0].bounds[3]
		switch tc.want {
		case "below":
			if !(top > baseline) {
				t.Errorf("underline top = %v, want below the baseline %v", top, baseline)
			}
		case "above":
			if !(bot < baseline-10) {
				t.Errorf("overline bottom = %v, want well above the baseline %v", bot, baseline)
			}
		case "across":
			if !(bot < baseline && top > baseline-30) {
				t.Errorf("line-through spans %v..%v, want it crossing the glyphs just above the baseline %v", top, bot, baseline)
			}
		}
	}
}

// TestDecorationThicknessScalesWithTheDeclaringFontSize pins resvg's
// style-resolving-4.svg, which puts font-size:200 on the DECLARING tspan and
// font-size:48 on the text it covers, and expects a thick rule. A thickness
// read off the covered characters instead would give a thin one.
func TestDecorationThicknessScalesWithTheDeclaringFontSize(t *testing.T) {
	thickness := func(declaringSize string) float64 {
		t.Helper()
		spy := renderToSpy(t, fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 400"
		  font-family="sans-serif"><text x="10" y="200"><tspan font-size="%s" text-decoration="underline"
		  ><tspan font-size="20">Text</tspan></tspan></text></svg>`, declaringSize))
		if len(spy.fills) != 1 {
			t.Fatalf("font-size %s: got %d rules, want 1", declaringSize, len(spy.fills))
		}
		return spy.fills[0].bounds[3] - spy.fills[0].bounds[1]
	}
	thin, thick := thickness("20"), thickness("200")
	if !(thick > thin*5) {
		t.Errorf("declaring font-size 200 gave thickness %v, 20 gave %v; the rule must scale with the DECLARING size", thick, thin)
	}
}

// TestDecorationNoneClearsAnInheritedDecoration covers the off switch: a
// descendant writing text-decoration="none" drops the ancestor's line.
func TestDecorationNoneClearsAnInheritedDecoration(t *testing.T) {
	spy := renderToSpy(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="40"><text x="10" y="100" text-decoration="underline"
	  ><tspan text-decoration="none">Text</tspan></text></svg>`)
	if len(spy.fills) != 0 {
		t.Errorf("got %d rules, want 0: text-decoration:none must clear the inherited underline", len(spy.fills))
	}
}

// TestDecorationsStillNeverUseDrawGlyph guards the invariant this whole
// feature rests on (see paintText's doc comment): SVG text — decorations
// included — goes through Fill/FillGlyph/Stroke, never Device.DrawGlyph, which
// pdfwrite implements as a PDF text-showing operator that cannot express a
// per-glyph transform or an independent stroke.
func TestDecorationsStillNeverUseDrawGlyph(t *testing.T) {
	spy := renderToSpy(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="40"><text x="10" y="100" rotate="10"
	  text-decoration="underline overline line-through" letter-spacing="4"
	  textLength="150" lengthAdjust="spacingAndGlyphs">Text</text></svg>`)
	if spy.drawGlyphCalls != 0 {
		t.Errorf("DrawGlyph called %d times; SVG text must always go through Fill/FillGlyph/Stroke", spy.drawGlyphCalls)
	}
}

// TestDegradedPropertiesLogAndDegrade covers the four properties that ship as
// an honest no-op rather than an approximation, asserting BOTH halves: the
// diagnostic fires, and the geometry is genuinely unchanged (so nothing is
// quietly half-applied).
func TestDegradedPropertiesLogAndDegrade(t *testing.T) {
	for _, tc := range []struct {
		attr, value, wantLog string
	}{
		{"font-stretch", "extra-condensed", "font-stretch"},
		{"font-stretch", "narrower", "font-stretch"},
		{"font-variant", "small-caps", "font-variant"},
		{"kerning", "10%", "kerning"},
		{"font-kerning", "none", "font-kerning"},
		{"dominant-baseline", "ideographic", "dominant-baseline"},
		{"dominant-baseline", "mathematical", "dominant-baseline"},
		{"alignment-baseline", "ideographic", "alignment-baseline"},
	} {
		name := tc.attr + "=" + tc.value
		src := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
		  font-family="sans-serif" font-size="40"><text x="10" y="100" %s=%q>Text</text></svg>`, tc.attr, tc.value)
		doc, logs := parseSVG(t, src)

		found := false
		for _, l := range logs {
			if strings.Contains(l, tc.wantLog) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: no log line mentioning %q; a degradation must be diagnosable. Logs: %v", name, tc.wantLog, logs)
		}

		// Geometry unchanged: same glyph count, same advances, same baseline
		// as the property-free render.
		refDoc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
		  font-family="sans-serif" font-size="40"><text x="10" y="100">Text</text></svg>`)
		ref := New(refDoc).layoutText(firstText(t, refDoc))
		got := New(doc).layoutText(firstText(t, doc))
		if len(got) != len(ref) {
			t.Errorf("%s: %d glyphs, want the undegraded %d", name, len(got), len(ref))
			continue
		}
		for i := range ref {
			if got[i].penX != ref[i].penX || got[i].penY != ref[i].penY || got[i].advance != ref[i].advance {
				t.Errorf("%s: glyph %d moved to (%v,%v) adv %v, want the unchanged (%v,%v) adv %v",
					name, i, got[i].penX, got[i].penY, got[i].advance, ref[i].penX, ref[i].penY, ref[i].advance)
				break
			}
		}
	}
}

// TestFontShorthandSetsEveryLonghand covers the `font` shorthand: it must set
// size, family, style, and weight together, and an incomplete value (no size
// or no family) must apply NOTHING rather than half of itself.
func TestFontShorthandSetsEveryLonghand(t *testing.T) {
	shorthand, _ := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  ><text x="10" y="100" font="bold italic 40px sans-serif">Text</text></svg>`)
	longhand, _ := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  ><text x="10" y="100" font-weight="bold" font-style="italic" font-size="40px"
	  font-family="sans-serif">Text</text></svg>`)
	if math.Abs(shorthand-longhand) > 1e-9 {
		t.Errorf("font shorthand advance = %v, longhands = %v; they must resolve identically", shorthand, longhand)
	}

	// Incomplete: no family. The inherited 16pt default must survive intact.
	plain, _ := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  font-family="sans-serif"><text x="10" y="100">Text</text></svg>`)
	partial, _ := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  font-family="sans-serif"><text x="10" y="100" font="40px">Text</text></svg>`)
	if math.Abs(partial-plain) > 1e-9 {
		t.Errorf("incomplete font shorthand changed the advance (%v vs %v); an invalid shorthand must apply nothing", partial, plain)
	}
}

// TestFontShorthandResetsUnspecifiedLonghands pins the CSS Cascade §3 rule
// that makes a shorthand a shorthand: it sets EVERY longhand it covers, and a
// slot the value does not name goes to that longhand's INITIAL value — not to
// the inherited one.
//
// The fixture is discriminating because the surrounding <g> sets both weight
// and style: an implementation that seeds from the inherited state (as this
// one did) renders bold italic, where CSS requires upright regular. Both
// vendored text/font/ fixtures spell style and weight out explicitly, which is
// exactly why no golden reaches this.
func TestFontShorthandResetsUnspecifiedLonghands(t *testing.T) {
	// The wanted result, stated independently: a plain upright regular run at
	// the shorthand's size and family.
	want, _ := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  ><text x="10" y="100" font-size="40px" font-family="sans-serif">Text</text></svg>`)

	// The same shorthand, inside a <g> that sets the two longhands it must
	// reset. The inherited bold/italic must NOT survive.
	got, _ := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  ><g font-weight="bold" font-style="italic"><text x="10" y="100"
	  font="40px sans-serif">Text</text></g></svg>`)

	if math.Abs(got-want) > 1e-9 {
		t.Errorf("font=\"40px sans-serif\" inside a bold italic <g> gave advance %v, want the upright regular %v (a shorthand resets every longhand it covers to its initial value)", got, want)
	}

	// Guard against a vacuous comparison: bold really must measure differently
	// here, or the assertion above would hold for a broken implementation too.
	boldRun, _ := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  ><text x="10" y="100" font-size="40px" font-family="sans-serif"
	  font-weight="bold" font-style="italic">Text</text></svg>`)
	if math.Abs(boldRun-want) < 1e-9 {
		t.Fatal("the bold italic face measures identically to the regular one; this test cannot discriminate")
	}

	// A keyword the shorthand DOES name still applies, so the reset is not
	// just "ignore every keyword".
	italicOnly, _ := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  ><g font-weight="bold"><text x="10" y="100" font="italic 40px sans-serif">Text</text></g></svg>`)
	italicRef, _ := textExtent(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  ><text x="10" y="100" font-size="40px" font-family="sans-serif" font-style="italic">Text</text></svg>`)
	if math.Abs(italicOnly-italicRef) > 1e-9 {
		t.Errorf("font=\"italic 40px sans-serif\" inside a bold <g> gave %v, want the italic-regular %v (the named keyword applies; the unnamed weight resets)", italicOnly, italicRef)
	}
}

// TestFontShorthandResetClearsStaleDegradationFlags is the other half of the
// reset: the two properties this engine tracks only as degradation flags
// (font-variant, font-stretch) must reset alongside the ones it resolves, so a
// shorthand cannot leave an ancestor's stale diagnostic attached to text that
// no longer requests it. An INVALID shorthand must not reset them either,
// since it applies nothing at all.
func TestFontShorthandResetClearsStaleDegradationFlags(t *testing.T) {
	styleOf := func(src string) svg.Style {
		t.Helper()
		doc, _ := parseSVG(t, src)
		txt := firstText(t, doc)
		if len(txt.Chars) == 0 {
			t.Fatal("no characters lowered")
		}
		return txt.Chars[0].Style
	}

	cleared := styleOf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  ><g font-stretch="condensed" font-variant="small-caps"><text x="10" y="100"
	  font="40px sans-serif">Text</text></g></svg>`)
	if cleared.FontStretchIgnored() || cleared.FontVariantIgnored() {
		t.Errorf("a valid font shorthand left stale degradation flags (stretch=%v variant=%v); it must reset every longhand it covers",
			cleared.FontStretchIgnored(), cleared.FontVariantIgnored())
	}

	// Invalid (no family): nothing applies, so the ancestor's flags survive.
	kept := styleOf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  ><g font-stretch="condensed" font-variant="small-caps"><text x="10" y="100"
	  font="40px">Text</text></g></svg>`)
	if !kept.FontStretchIgnored() || !kept.FontVariantIgnored() {
		t.Errorf("an INVALID font shorthand reset the inherited degradation flags (stretch=%v variant=%v); it must apply nothing at all",
			kept.FontStretchIgnored(), kept.FontVariantIgnored())
	}
}

// writing-mode resolves to one vocabulary the renderer can switch on, and a vertical
// value is now HONORED rather than degraded — so none of them may log.
//
// This replaces a test asserting the opposite (every vertical value flagged as ignored,
// with a diagnostic), which was correct while the engine had no vertical advance model
// and is exactly what this change retires.
func TestWritingModeResolves(t *testing.T) {
	styleOf := func(src string) (svg.Style, []string) {
		t.Helper()
		doc, logs := parseSVG(t, src)
		txt := firstText(t, doc)
		if len(txt.Chars) == 0 {
			t.Fatal("no characters lowered")
		}
		return txt.Chars[0].Style, logs
	}

	for _, tc := range []struct{ mode, want string }{
		// SVG 1.1's tb/tb-rl are top-to-bottom with lines stacking right-to-left,
		// which is exactly vertical-rl; they normalize onto it so the renderer has
		// one set of strings to branch on.
		{"tb", "vertical-rl"},
		{"tb-rl", "vertical-rl"},
		{"vertical-rl", "vertical-rl"},
		{"vertical-lr", "vertical-lr"},
		// Horizontal: the initial value plus the deprecated SVG 1.1 spellings.
		{"horizontal-tb", ""},
		{"lr", ""},
		{"lr-tb", ""},
		{"rl", ""},
		{"rl-tb", ""},
	} {
		st, logs := styleOf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
		  ><text x="10" y="100" writing-mode="` + tc.mode + `">Text</text></svg>`)
		if got := st.WritingMode(); got != tc.want {
			t.Errorf("writing-mode=%q resolved to %q, want %q", tc.mode, got, tc.want)
		}
		if want := tc.want != ""; st.Vertical() != want {
			t.Errorf("writing-mode=%q: Vertical() = %v, want %v", tc.mode, st.Vertical(), want)
		}
		// Nothing here is a degradation any more, so nothing may log — a stale
		// diagnostic that contradicts the rendering is its own bug.
		if anyContains(logs, "writing-mode") {
			t.Errorf("writing-mode=%q logged despite being supported; logs = %v", tc.mode, logs)
		}
	}
}

// sideways-rl/sideways-lr are real CSS values but distinct modes this engine does not
// model. They must be REPORTED rather than folded into vertical-rl: quietly treating
// them as a mode the author did not ask for is the failure this project's rules exist
// to prevent.
func TestUnmodelledWritingModeIsReported(t *testing.T) {
	for _, mode := range []string{"sideways-rl", "sideways-lr", "bogus"} {
		doc, logs := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
		  ><text x="10" y="100" writing-mode="`+mode+`">Text</text></svg>`)
		txt := firstText(t, doc)
		if len(txt.Chars) == 0 {
			t.Fatal("no characters lowered")
		}
		if st := txt.Chars[0].Style; st.Vertical() {
			t.Errorf("writing-mode=%q was treated as vertical; it is a mode this engine "+
				"does not model and must not be folded into one", mode)
		}
		if !anyContains(logs, "writing-mode") {
			t.Errorf("writing-mode=%q was dropped without a diagnostic; logs = %v", mode, logs)
		}
	}
}

// text-orientation resolves alongside writing-mode, with mixed as the initial value
// (stored as "") and sideways-right as the CSS Writing Modes 3 alias of sideways.
func TestTextOrientationResolves(t *testing.T) {
	for _, tc := range []struct{ val, want string }{
		{"mixed", ""},
		{"upright", "upright"},
		{"sideways", "sideways"},
		{"sideways-right", "sideways"},
	} {
		doc, logs := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
		  ><text x="10" y="100" writing-mode="vertical-rl" text-orientation="`+tc.val+`">Text</text></svg>`)
		txt := firstText(t, doc)
		if len(txt.Chars) == 0 {
			t.Fatal("no characters lowered")
		}
		if got := txt.Chars[0].Style.TextOrientation(); got != tc.want {
			t.Errorf("text-orientation=%q resolved to %q, want %q", tc.val, got, tc.want)
		}
		if anyContains(logs, "text-orientation") {
			t.Errorf("text-orientation=%q logged despite being supported; logs = %v", tc.val, logs)
		}
	}

	// An unrecognized value falls back to mixed AND says so.
	doc, logs := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  ><text x="10" y="100" text-orientation="use-glyph-orientation">Text</text></svg>`)
	txt := firstText(t, doc)
	if got := txt.Chars[0].Style.TextOrientation(); got != "" {
		t.Errorf("an unrecognized text-orientation resolved to %q, want the mixed default", got)
	}
	if !anyContains(logs, "text-orientation") {
		t.Errorf("an unrecognized text-orientation was dropped without a diagnostic; logs = %v", logs)
	}
}

// Both properties INHERIT: the SVG cascade copies the parent Style wholesale, so a
// value on <text> must reach a <tspan> inside it. Pinned because the render path reads
// the style at each CHARACTER, so a break here would leave a tspan laying out
// horizontally in the middle of a vertical run.
func TestWritingModeAndOrientationInherit(t *testing.T) {
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"
	  ><text x="10" y="100" writing-mode="vertical-rl" text-orientation="upright"><tspan>Text</tspan></text></svg>`)
	txt := firstText(t, doc)
	if len(txt.Chars) == 0 {
		t.Fatal("no characters lowered")
	}
	st := txt.Chars[0].Style
	if !st.Vertical() {
		t.Error("a tspan did not inherit its text's vertical writing-mode")
	}
	if st.TextOrientation() != "upright" {
		t.Errorf("tspan text-orientation = %q, want inherited upright", st.TextOrientation())
	}
}

// TestSpacingAndLengthSurviveHostileInput is the adversarial sweep: enormous,
// tiny, non-finite, and malformed values must degrade rather than producing a
// non-finite pen position (which the rasterizer turns into unbounded work) or
// panicking.
func TestSpacingAndLengthSurviveHostileInput(t *testing.T) {
	for _, attrs := range []string{
		`letter-spacing="1e300"`,
		`letter-spacing="-1e300"`,
		`word-spacing="1e308"`,
		`textLength="1e300"`,
		`textLength="1e300" lengthAdjust="spacingAndGlyphs"`,
		`letter-spacing="NaN"`,
		`letter-spacing="Inf"`,
		`baseline-shift="1e300"`,
		`baseline-shift="-1e300%"`,
		`textLength="abc"`,
		`textLength=""`,
		`letter-spacing="10q"`,
		`textLength="0" lengthAdjust="spacingAndGlyphs"`,
		`dominant-baseline="   HANGING   "`,
		`text-decoration="underline,,overline  line-through"`,
		`font="  "`,
	} {
		t.Run(attrs, func(t *testing.T) {
			src := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
			  font-family="sans-serif" font-size="20"><text x="10" y="100" %s>Text</text></svg>`, attrs)
			doc, _ := parseSVG(t, src)
			placed := New(doc).layoutText(firstText(t, doc))
			for i := range placed {
				p := &placed[i]
				for _, v := range []float64{p.penX, p.penY, p.advance, p.xScale, p.rotateRad} {
					if math.IsNaN(v) || math.IsInf(v, 0) {
						t.Fatalf("glyph %d has a non-finite placement value %v (pen %v,%v adv %v scale %v)",
							i, v, p.penX, p.penY, p.advance, p.xScale)
					}
					if math.Abs(v) > 1e7 {
						t.Fatalf("glyph %d placement value %v is past every sane bound; the DoS clamp did not hold", i, v)
					}
				}
			}
			// And it must still render without panicking.
			New(doc).DrawVector(&decorationSpy{Device: raster.New(image.NewRGBA(image.Rect(0, 0, 200, 200)))}, render.Identity)
		})
	}
}
