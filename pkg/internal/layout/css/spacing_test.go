package css

import (
	"context"
	"math"
	"testing"

	gcss "github.com/nathanstitt/omnidoc/pkg/internal/css"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout/cssbox"
)

// lineEndX returns the pen position just past the last glyph of the first line in the
// tree — the line's advance width as actually laid out. It is the assertion that
// matters for these properties: they change where glyphs SIT, not how many there are,
// so a glyph count (or an ink-pixel count) cannot see them at all.
func lineEndX(t *testing.T, src string, w float64) float64 {
	t.Helper()
	f := layoutHTML(t, src, w)
	got, found := 0.0, false
	var walk func(g *Fragment)
	walk = func(g *Fragment) {
		if g == nil || found {
			return
		}
		if len(g.Lines) > 0 && len(g.Lines[0].Glyphs) > 0 {
			last := g.Lines[0].Glyphs[len(g.Lines[0].Glyphs)-1]
			got, found = last.X+last.AdvancePt, true
			return
		}
		for _, c := range g.Children {
			walk(c)
		}
	}
	walk(f)
	if !found {
		t.Fatalf("no laid-out glyphs for %s", src)
	}
	return got
}

// countLines returns the number of laid-out lines in the first box that has any.
func countSpacedLines(t *testing.T, src string, w float64) int {
	t.Helper()
	f := layoutHTML(t, src, w)
	n, found := 0, false
	var walk func(g *Fragment)
	walk = func(g *Fragment) {
		if g == nil || found {
			return
		}
		if len(g.Lines) > 0 {
			n, found = len(g.Lines), true
			return
		}
		for _, c := range g.Children {
			walk(c)
		}
	}
	walk(f)
	if !found {
		t.Fatalf("no laid-out lines for %s", src)
	}
	return n
}

const lsEps = 1e-6

// TestLetterSpacingWidensTheLaidOutLine is the end-to-end proof that the property
// reaches layout at all: three 30px "I"s must occupy 3×20px more with tracking than
// without.
//
// Note what is asserted and what deliberately is not. The gap report's probe measured
// INK PIXELS, which cannot see this property — spreading three identical glyphs paints
// exactly the same number of pixels wherever they land (measured: 321 both ways). The
// observable effect is geometric, so the assertion is geometric.
func TestLetterSpacingWidensTheLaidOutLine(t *testing.T) {
	base := lineEndX(t, `<div style="font-size:30px">III</div>`, 400)
	tracked := lineEndX(t, `<div style="font-size:30px;letter-spacing:20px">III</div>`, 400)
	if want := base + 3*20; math.Abs(tracked-want) > lsEps {
		t.Errorf("tracked line end = %v, want %v (base %v + 3 characters × 20px)", tracked, want, base)
	}
}

// TestLetterSpacingNegativeTightensTheLine covers the negative half end to end.
func TestLetterSpacingNegativeTightensTheLine(t *testing.T) {
	base := lineEndX(t, `<div style="font-size:30px">III</div>`, 400)
	tight := lineEndX(t, `<div style="font-size:30px;letter-spacing:-3px">III</div>`, 400)
	if want := base - 3*3; math.Abs(tight-want) > lsEps {
		t.Errorf("tightened line end = %v, want %v (base %v − 3 characters × 3px)", tight, want, base)
	}
}

// TestWordSpacingWidensOnlyTheSpace asserts word-spacing adds at the separator and
// nowhere else: "I I" has exactly one space, so the line grows by exactly one value.
func TestWordSpacingWidensOnlyTheSpace(t *testing.T) {
	base := lineEndX(t, `<div style="font-size:30px">I I</div>`, 400)
	spaced := lineEndX(t, `<div style="font-size:30px;word-spacing:20px">I I</div>`, 400)
	if want := base + 20; math.Abs(spaced-want) > lsEps {
		t.Errorf("word-spaced line end = %v, want %v (base %v + ONE separator × 20px)", spaced, want, base)
	}
}

// TestSpacingInheritsFromAnAncestor is the case the removed cascade note called out as
// not working: a value set on an enclosing element must reach the text inside a
// descendant inline.
func TestSpacingInheritsFromAnAncestor(t *testing.T) {
	direct := lineEndX(t, `<div style="font-size:30px;letter-spacing:20px">III</div>`, 400)
	inherited := lineEndX(t, `<div style="letter-spacing:20px"><span style="font-size:30px">III</span></div>`, 400)
	if math.Abs(direct-inherited) > lsEps {
		t.Errorf("inherited tracking gave %v, want %v (same as a direct declaration)", inherited, direct)
	}
}

// TestSpacingNormalResetsAnInheritedValue is how an author cancels tracking on a
// subtree. `normal` must return the run to its untracked width, not merely stop adding
// more.
func TestSpacingNormalResetsAnInheritedValue(t *testing.T) {
	plain := lineEndX(t, `<div style="font-size:30px">III</div>`, 400)
	reset := lineEndX(t, `<div style="font-size:30px;letter-spacing:20px"><span style="letter-spacing:normal">III</span></div>`, 400)
	if math.Abs(plain-reset) > lsEps {
		t.Errorf("letter-spacing:normal gave %v, want %v (the untracked width)", reset, plain)
	}
}

// TestEmSpacingResolvesAgainstTheDescendantsOwnFontSize pins the consequence of
// inheriting the SPECIFIED length rather than a resolved point value: the same
// 0.5em declared on an ancestor tracks a 30px child more widely than a 10px one,
// which is why authors write tracking in em.
func TestEmSpacingResolvesAgainstTheDescendantsOwnFontSize(t *testing.T) {
	big := lineEndX(t, `<div style="letter-spacing:0.5em"><span style="font-size:30px">III</span></div>`, 400)
	bigPlain := lineEndX(t, `<div><span style="font-size:30px">III</span></div>`, 400)
	small := lineEndX(t, `<div style="letter-spacing:0.5em"><span style="font-size:10px">III</span></div>`, 400)
	smallPlain := lineEndX(t, `<div><span style="font-size:10px">III</span></div>`, 400)

	// 0.5em at 30px is 15px per character; at 10px it is 5px per character.
	if want := 3 * 15.0; math.Abs((big-bigPlain)-want) > lsEps {
		t.Errorf("0.5em at 30px added %v, want %v", big-bigPlain, want)
	}
	if want := 3 * 5.0; math.Abs((small-smallPlain)-want) > lsEps {
		t.Errorf("0.5em at 10px added %v, want %v", small-smallPlain, want)
	}
}

// TestLetterSpacingForcesAnEarlierLineBreak proves spacing composes with line breaking
// through the real CSS path (not just the shaper): the same text and box width wrap
// differently once tracked.
func TestLetterSpacingForcesAnEarlierLineBreak(t *testing.T) {
	// Untracked, "aaa bbb" occupies 61.64pt and fits the 120pt box on one line.
	plain := countSpacedLines(t, `<div style="width:120px;font-size:20px">aaa bbb</div>`, 400)
	if plain != 1 {
		t.Fatalf("untracked lines = %d, want 1 (the fixture must fit on one line to be meaningful)", plain)
	}
	// At 20px tracking the same text needs 7 x 20 = 140pt more than it did, so the
	// second word can no longer fit and is pushed down.
	tracked := countSpacedLines(t, `<div style="width:120px;font-size:20px;letter-spacing:20px">aaa bbb</div>`, 400)
	if tracked < 2 {
		t.Errorf("tracked lines = %d, want >= 2: tracking must push the second word to a new line", tracked)
	}
	// The threshold is real, not incidental: 8px tracking still fits (117.64pt of the
	// 120pt box), so the property is not merely "any tracking wraps everything" — the
	// breaker is reading the actual adjusted advances.
	if got := countSpacedLines(t, `<div style="width:120px;font-size:20px;letter-spacing:8px">aaa bbb</div>`, 400); got != 1 {
		t.Errorf("8px-tracked lines = %d, want 1 (117.64pt still fits the 120pt box)", got)
	}
}

// spacedCell builds the measure_test.go cell fixture with letter/word spacing applied,
// so intrinsic sizing can be measured with and without the properties.
func spacedCell(s string, letter, word gcss.Length) *cssbox.Box {
	st := gcss.ComputedStyle{
		FontSizePt:    16,
		FontFamily:    "serif",
		Width:         gcss.Length{Unit: gcss.UnitAuto},
		LetterSpacing: letter,
		WordSpacing:   word,
	}
	txt := &cssbox.Box{Kind: cssbox.BoxText, Text: s, Display: cssbox.DisplayInline, Style: st}
	return &cssbox.Box{
		Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
		Formatting: cssbox.InlineFC, Style: st, Children: []*cssbox.Box{txt},
	}
}

// TestIntrinsicSizingAccountsForSpacing is the requirement that min-content and
// max-content see the added advances. Both are computed from the SHAPED glyphs, so
// folding spacing into Glyph.Advance makes this work with no change to measure.go —
// asserted here rather than assumed, since a table/flex/grid track sized from a
// tracked cell would otherwise be too narrow and clip its own text.
func TestIntrinsicSizingAccountsForSpacing(t *testing.T) {
	e := New(nil, nil, nil)
	ctx := context.Background()
	none := gcss.Length{}
	ls := gcss.Length{Value: 4, Unit: gcss.UnitPx}

	// max-content is the unbroken line: "ab cd" is 5 characters, so it gains 5×4px.
	plainMax := e.measureMaxContent(ctx, spacedCell("ab cd", none, none))
	trackedMax := New(nil, nil, nil).measureMaxContent(ctx, spacedCell("ab cd", ls, none))
	if want := plainMax + 5*4; math.Abs(trackedMax-want) > lsEps {
		t.Errorf("tracked max-content = %v, want %v (plain %v + 5 characters × 4px)", trackedMax, want, plainMax)
	}

	// min-content is the widest unbreakable unit — here the 2-character word "ab",
	// which gains 2×4px. The trailing space of the broken unit is excluded, so the
	// growth is exactly the word's own characters.
	plainMin := New(nil, nil, nil).measureMinContent(ctx, spacedCell("ab cd", none, none))
	trackedMin := New(nil, nil, nil).measureMinContent(ctx, spacedCell("ab cd", ls, none))
	if want := plainMin + 2*4; math.Abs(trackedMin-want) > lsEps {
		t.Errorf("tracked min-content = %v, want %v (plain %v + 2 characters × 4px)", trackedMin, want, plainMin)
	}

	// word-spacing widens the SEPARATOR, so it grows max-content (which contains the
	// space) but must NOT grow min-content (whose widest unit is a word with no space
	// in it). That asymmetry is the discriminating assertion.
	wsLen := gcss.Length{Value: 30, Unit: gcss.UnitPx}
	wsMax := New(nil, nil, nil).measureMaxContent(ctx, spacedCell("ab cd", none, wsLen))
	if want := plainMax + 30; math.Abs(wsMax-want) > lsEps {
		t.Errorf("word-spaced max-content = %v, want %v", wsMax, want)
	}
	wsMin := New(nil, nil, nil).measureMinContent(ctx, spacedCell("ab cd", none, wsLen))
	if math.Abs(wsMin-plainMin) > lsEps {
		t.Errorf("word-spaced min-content = %v, want %v (unchanged: the widest word holds no separator)", wsMin, plainMin)
	}
}

// TestJustifyComposesWithWordSpacing checks the interaction the two properties have by
// construction. Justification distributes a line's slack across its inter-word gaps
// (inline.Place's ExtraPerSpace), and word-spacing has ALREADY been folded into each
// space's advance by the time that slack is computed. So the two compose in the
// spec-correct order — word-spacing widens the space, justification then stretches
// whatever gap remains to fill the line — and a justified line still ends flush at its
// box edge regardless of word-spacing.
func TestJustifyComposesWithWordSpacing(t *testing.T) {
	const w = 200.0
	// Text long enough to WRAP. This matters: a paragraph's last line is never
	// justified (correct CSS), so a fixture that fits on one line would measure the
	// ragged width and prove nothing about justification at all.
	const txt = "aa bb cc dd ee ff gg hh ii jj kk ll mm nn oo pp"

	plain := lineEndX(t, `<div style="width:200px;font-size:14px;text-align:justify">`+txt+`</div>`, 400)
	spaced := lineEndX(t, `<div style="width:200px;font-size:14px;text-align:justify;word-spacing:6px">`+txt+`</div>`, 400)

	// The first (justified) line fills the box exactly in BOTH cases. That is the
	// composition proof: word-spacing has already widened each space before Place
	// computes the remaining slack, so justification absorbs the difference and the
	// line still lands flush. Were word-spacing applied after justification, the
	// spaced line would overrun the box by 6px per gap.
	for _, tc := range []struct {
		name string
		got  float64
	}{{"plain", plain}, {"word-spaced", spaced}} {
		if math.Abs(tc.got-w) > lsEps {
			t.Errorf("%s justified line ends at %v, want exactly %v (flush with the box)", tc.name, tc.got, w)
		}
	}
}

// TestSpacingDoesNotCrossTheInlineSVGBoundary pins a KNOWN, DELIBERATE gap so it cannot
// be mistaken for working. The removed cascade note claimed letter-spacing failed to
// reach an inline <svg> because ComputedStyle had no field to inherit from. That
// diagnosis was wrong: adding the fields did not change the behavior, because inline
// <svg> is REPLACED content — box generation re-serializes the markup and pkg/svg
// re-parses it via svg.Parse(data, logf), which receives markup and a logger and
// nothing else. No computed property crosses that boundary, including color and
// font-family.
//
// The test asserts the gap rather than the fix so that whoever later threads an
// inherited style into svg.Parse sees this fail and updates docs/SVG.md with it.
func TestSpacingDoesNotCrossTheInlineSVGBoundary(t *testing.T) {
	const svg = `<svg width="300" height="60"><text x="0" y="30" font-size="20" fill="black">III</text></svg>`
	plain := layoutHTML(t, `<div>`+svg+`</div>`, 400)
	tracked := layoutHTML(t, `<div style="letter-spacing:20px">`+svg+`</div>`, 400)

	// Compare the laid-out SVG fragment geometry; identical means nothing inherited.
	wide := func(f *Fragment) float64 {
		var max float64
		var walk func(g *Fragment)
		walk = func(g *Fragment) {
			if g == nil {
				return
			}
			if g.X+g.W > max {
				max = g.X + g.W
			}
			for _, c := range g.Children {
				walk(c)
			}
		}
		walk(f)
		return max
	}
	if math.Abs(wide(plain)-wide(tracked)) > lsEps {
		t.Errorf("inline <svg> geometry changed under an inherited letter-spacing (%v vs %v). "+
			"If the HTML->SVG inherited-style boundary was implemented, update docs/SVG.md and this test.",
			wide(plain), wide(tracked))
	}
}
