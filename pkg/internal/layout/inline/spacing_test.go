package inline

import (
	"image/color"
	"math"
	"testing"

	pkgfont "github.com/nathanstitt/omnidoc/pkg/internal/font"
	layoutfont "github.com/nathanstitt/omnidoc/pkg/internal/layout/font"
)

// shapeSpaced shapes text at 12pt Arial with the given letter/word spacing, returning
// the glyphs. It is the one helper every test in this file builds on.
func shapeSpaced(t *testing.T, text string, ls, ws float64) []Glyph {
	t.Helper()
	return Shape(layoutfont.NewFaceCache(), []Run{{
		Text:            text,
		Family:          "Arial",
		SizePt:          12,
		Color:           color.RGBA{A: 0xff},
		LetterSpacingPt: ls,
		WordSpacingPt:   ws,
	}}, nil)
}

func sumAdvances(glyphs []Glyph) float64 {
	total := 0.0
	for i := range glyphs {
		total += glyphs[i].Advance
	}
	return total
}

const spacingEps = 1e-9

// TestLetterSpacingAddsToEveryGlyphIncludingTheLast pins the TRAILING-SPACING decision,
// which is the part of this property most likely to be "fixed" into a regression later.
//
// CSS Text 3 §8.1 words letter-spacing as spacing "between" characters, which reads as
// n-1 gaps for n characters. Browsers do not implement it that way: Chrome, Firefox and
// Safari all add the tracking after EVERY character unit including the final one, which
// is why a tracked, right-aligned line visibly stops one tracking-width short of the
// right margin. This engine matches the browsers, so N characters gain N × spacing —
// never (N-1) × spacing.
func TestLetterSpacingAddsToEveryGlyphIncludingTheLast(t *testing.T) {
	const ls = 5
	base := shapeSpaced(t, "III", 0, 0)
	spaced := shapeSpaced(t, "III", ls, 0)
	if len(base) != 3 || len(spaced) != 3 {
		t.Fatalf("glyph counts = %d/%d, want 3/3", len(base), len(spaced))
	}
	// Per-glyph: every glyph, including the last, gains exactly ls.
	for i := range base {
		want := base[i].Advance + ls
		if math.Abs(spaced[i].Advance-want) > spacingEps {
			t.Errorf("glyph %d advance = %v, want %v (natural + %v)", i, spaced[i].Advance, want, ls)
		}
	}
	// In total: 3 characters gain 3×ls, NOT 2×ls. That distinction is the whole point.
	gotDelta := sumAdvances(spaced) - sumAdvances(base)
	if want := 3.0 * ls; math.Abs(gotDelta-want) > spacingEps {
		t.Errorf("total delta = %v, want %v (N×spacing, not (N-1)×spacing = %v)", gotDelta, want, 2*ls)
	}
}

// TestNegativeLetterSpacingTightensButNeverGoesNegative covers the other half of the
// value space. A negative tracking is legal CSS and must actually tighten, but no
// glyph's advance may become negative: the greedy breaker accumulates advances and
// assumes the running width never shrinks as glyphs are appended.
func TestNegativeLetterSpacingTightensButNeverGoesNegative(t *testing.T) {
	base := shapeSpaced(t, "III", 0, 0)
	tight := shapeSpaced(t, "III", -2, 0)
	if len(tight) != len(base) {
		t.Fatalf("glyph count changed under negative tracking: %d vs %d", len(tight), len(base))
	}
	if sumAdvances(tight) >= sumAdvances(base) {
		t.Errorf("negative letter-spacing did not tighten: %v >= %v", sumAdvances(tight), sumAdvances(base))
	}
	for i := range tight {
		if want := base[i].Advance - 2; math.Abs(tight[i].Advance-want) > spacingEps {
			t.Errorf("glyph %d advance = %v, want %v", i, tight[i].Advance, want)
		}
	}

	// A tracking more negative than the glyph is wide collapses the advance to ZERO
	// rather than going negative. The outlines still paint at their (now coincident) pen
	// positions and overlap, which is what a browser shows for a huge negative tracking.
	crushed := shapeSpaced(t, "III", -1000, 0)
	for i := range crushed {
		if crushed[i].Advance < 0 {
			t.Errorf("glyph %d advance = %v, want >= 0 (a negative advance breaks the breaker)", i, crushed[i].Advance)
		}
	}
	if got := sumAdvances(crushed); got != 0 {
		t.Errorf("fully crushed line width = %v, want 0", got)
	}
}

// TestWordSpacingAppliesToSeparatorsOnly asserts word-spacing lands on the word
// separators CSS Text 3 §8.2 names (U+0020, U+00A0) and on nothing else — in
// particular that it does NOT behave like a second letter-spacing.
func TestWordSpacingAppliesToSeparatorsOnly(t *testing.T) {
	const ws = 7
	base := shapeSpaced(t, "I I", 0, 0)
	spaced := shapeSpaced(t, "I I", 0, ws)
	if len(base) != 3 || len(spaced) != 3 {
		t.Fatalf("glyph counts = %d/%d, want 3/3", len(base), len(spaced))
	}
	for i := range base {
		delta := spaced[i].Advance - base[i].Advance
		want := 0.0
		if base[i].Space {
			want = ws
		}
		if math.Abs(delta-want) > spacingEps {
			t.Errorf("glyph %d (space=%v) delta = %v, want %v", i, base[i].Space, delta, want)
		}
	}
	// Exactly ONE space in "I I", so the line grows by exactly one ws — not three.
	if got, want := sumAdvances(spaced)-sumAdvances(base), float64(ws); math.Abs(got-want) > spacingEps {
		t.Errorf("total delta = %v, want %v (one separator only)", got, want)
	}
}

// TestWordSpacingAppliesToNoBreakSpace covers U+00A0, the separator that is easy to
// miss because it is NOT a break opportunity. The two predicates deliberately differ:
// a no-break space takes word-spacing but must never become a Space (a break
// opportunity), or `&nbsp;` would stop holding words together.
func TestWordSpacingAppliesToNoBreakSpace(t *testing.T) {
	const ws = 7
	const nbsp = "I I"
	base := shapeSpaced(t, nbsp, 0, 0)
	spaced := shapeSpaced(t, nbsp, 0, ws)
	if len(base) != 3 || len(spaced) != 3 {
		t.Fatalf("glyph counts = %d/%d, want 3/3", len(base), len(spaced))
	}
	if got, want := sumAdvances(spaced)-sumAdvances(base), float64(ws); math.Abs(got-want) > spacingEps {
		t.Errorf("U+00A0 total delta = %v, want %v (nbsp is a word separator)", got, want)
	}
	// ...and it is still not a break opportunity.
	if base[1].Space {
		t.Errorf("U+00A0 was marked Space; a no-break space must not be a break opportunity")
	}
}

// TestSpacingLeavesZeroSpacingRunsByteIdentical is the regression guard for every
// caller that does not use these properties — the DOCX engine, every PDF text path, and
// every HTML page with no tracking. Shaping with the zero value must produce exactly the
// advances it produced before the properties existed.
func TestSpacingLeavesZeroSpacingRunsByteIdentical(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	text := "The quick brown fox jumps."
	plain := Shape(faces, []Run{{Text: text, Family: "Arial", SizePt: 12, Color: color.RGBA{A: 0xff}}}, nil)
	zeroed := shapeSpaced(t, text, 0, 0)
	if len(plain) != len(zeroed) {
		t.Fatalf("glyph counts differ: %d vs %d", len(plain), len(zeroed))
	}
	for i := range plain {
		if plain[i].Advance != zeroed[i].Advance {
			t.Errorf("glyph %d advance = %v, want %v (zero spacing must not perturb anything)",
				i, zeroed[i].Advance, plain[i].Advance)
		}
	}
}

// TestBidiControlsStayZeroWidthUnderTracking guards a case that is invisible until it
// is wrong: a bidi control mark draws nothing and must not gain tracking, or an
// author's directional mark would silently widen the line.
func TestBidiControlsStayZeroWidthUnderTracking(t *testing.T) {
	// U+200E LEFT-TO-RIGHT MARK between two letters.
	glyphs := shapeSpaced(t, "I\u200eI", 5, 0)
	var found bool
	for i := range glyphs {
		if len(glyphs[i].Runes) == 1 && glyphs[i].Runes[0] == '\u200e' {
			found = true
			if glyphs[i].Advance != 0 {
				t.Errorf("bidi control advance = %v, want 0 (not a typographic character unit)", glyphs[i].Advance)
			}
		}
	}
	if !found {
		t.Fatal("the LRM did not survive shaping; the bidi reorder depends on it")
	}
}

// TestLetterSpacingChangesWhereLinesBreak is the interaction the §8 seam note promised
// would be free — VERIFIED here rather than assumed. The breaker reads only Advance, so
// folding spacing into Advance should change break positions with no breaker change.
//
// The text and width are chosen so the two words fit on one line untracked and cannot
// once tracked, which is the only assertion that actually proves spacing reached the
// breaker.
func TestLetterSpacingChangesWhereLinesBreak(t *testing.T) {
	const width = 60.0
	plain := Break(shapeSpaced(t, "aa bb", 0, 0), width, width)
	if len(plain) != 1 {
		t.Fatalf("untracked lines = %d, want 1 (the fixture must fit on one line to be meaningful)", len(plain))
	}
	tracked := Break(shapeSpaced(t, "aa bb", 6, 0), width, width)
	if len(tracked) < 2 {
		t.Errorf("tracked lines = %d, want >= 2: letter-spacing must push the second word down", len(tracked))
	}
}

// TestWordSpacingChangesWhereLinesBreak is the same proof for word-spacing, which
// reaches the breaker only through the space glyph's advance.
func TestWordSpacingChangesWhereLinesBreak(t *testing.T) {
	const width = 34.0
	plain := Break(shapeSpaced(t, "aa bb", 0, 0), width, width)
	if len(plain) != 1 {
		t.Fatalf("untracked lines = %d, want 1", len(plain))
	}
	tracked := Break(shapeSpaced(t, "aa bb", 0, 25), width, width)
	if len(tracked) < 2 {
		t.Errorf("word-spaced lines = %d, want >= 2", len(tracked))
	}
}

// TestTrailingSpacingIsExcludedWithTrailingSpaces checks that the trailing-spacing rule
// composes with VisibleWidth's trailing-space exclusion: a line ending in a space still
// drops that space (and the spacing folded into it) from its measured width, so
// alignment is not thrown off by invisible trailing tracking.
func TestTrailingSpacingIsExcludedWithTrailingSpaces(t *testing.T) {
	withTrail := shapeSpaced(t, "II ", 4, 9)
	noTrail := shapeSpaced(t, "II", 4, 9)
	if got, want := VisibleWidth(withTrail), VisibleWidth(noTrail); math.Abs(got-want) > spacingEps {
		t.Errorf("VisibleWidth with a trailing space = %v, want %v (the trailing space and its spacing are excluded)", got, want)
	}
}

// TestLetterSpacingAdvancesTabStops covers the seam note's explicit warning that
// lineCol tracks tab stops and must include the added spacing. A preserved tab advances
// to the next stop measured from the CURRENT pen position, so if the tracking were left
// out of lineCol the tab would be computed from a pen position that does not exist.
func TestLetterSpacingAdvancesTabStops(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	shape := func(ls float64) []Glyph {
		return Shape(faces, []Run{{
			Text:            "ab\tc",
			Family:          "Arial",
			SizePt:          12,
			Color:           color.RGBA{A: 0xff},
			WhiteSpace:      "pre",
			LetterSpacingPt: ls,
		}}, nil)
	}
	plain, tracked := shape(0), shape(6)
	if len(plain) != len(tracked) {
		t.Fatalf("glyph counts differ: %d vs %d", len(plain), len(tracked))
	}
	// The tab is glyph 2. Tracking moves the pen further along before the tab is
	// reached, so the distance to the next stop must differ from the untracked case —
	// proving lineCol saw the spacing. Had lineCol ignored the tracking, both runs
	// would have computed the tab from the same (wrong) column and matched here.
	if plain[2].Advance == tracked[2].Advance {
		t.Errorf("tab advance unchanged at %v under tracking; lineCol did not include the added spacing", plain[2].Advance)
	}

	// Stronger: the pen must still land on a real tab stop. The tab's own advance
	// carries one letter-spacing increment on top of the stop distance (a tab is a
	// character unit), so subtracting that increment must leave the pen at an exact
	// multiple of the stop interval.
	face, ok := faces.Resolve("Arial", pkgfont.Style{})
	if !ok {
		t.Fatal("Arial did not resolve; the tab-stop interval cannot be computed")
	}
	spaceAdv := 12 * 0.25
	if _, sa, gOK := face.Glyph(' '); gOK {
		spaceAdv = sa * 12
	}
	stop := tabSize * spaceAdv
	penAfterTab := tracked[0].Advance + tracked[1].Advance + tracked[2].Advance - 6
	if rem := math.Mod(penAfterTab, stop); math.Abs(rem) > 1e-6 && math.Abs(rem-stop) > 1e-6 {
		t.Errorf("pen after tab = %v, which is not a multiple of the %v tab stop (remainder %v)", penAfterTab, stop, rem)
	}
}
