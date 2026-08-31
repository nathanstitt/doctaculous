package css

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// firstGlyphLine returns the first line carrying glyphs anywhere in the tree.
func firstGlyphLine(f *Fragment) *LineFragment {
	var found *LineFragment
	var walk func(*Fragment)
	walk = func(f *Fragment) {
		for li := range f.Lines {
			if len(f.Lines[li].Glyphs) > 0 && found == nil {
				found = &f.Lines[li]
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(f)
	return found
}

// verticalLine lays out one vertical box and returns its line.
func verticalLine(t *testing.T, orient, text string) *LineFragment {
	t.Helper()
	style := "writing-mode:vertical-rl;font-size:24px"
	if orient != "" {
		style += ";text-orientation:" + orient
	}
	root := layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="`+style+`">`+text+`</div></body></html>`, 300, nil)
	ln := firstGlyphLine(root)
	if ln == nil {
		t.Fatalf("no glyphs for orientation %q", orient)
	}
	return ln
}

// `upright` keeps every glyph unrotated — the fixed-pitch stacking a short Latin label
// wants, and what the vertical layout did before text-orientation was parsed.
func TestUprightRotatesNothing(t *testing.T) {
	ln := verticalLine(t, "upright", "NOW")
	for i, g := range ln.Glyphs {
		if g.Rotate != 0 {
			t.Errorf("upright glyph %d has rotation %v, want 0", i, g.Rotate)
		}
	}
}

// `sideways` rotates every glyph a quarter turn clockwise, including glyphs that would
// stay upright under `mixed`.
func TestSidewaysRotatesEverything(t *testing.T) {
	ln := verticalLine(t, "sideways", "NOW")
	for i, g := range ln.Glyphs {
		if d := g.Rotate - math.Pi/2; d > 1e-9 || d < -1e-9 {
			t.Errorf("sideways glyph %d rotation = %v, want pi/2", i, g.Rotate)
		}
	}
}

// `mixed` is the CSS INITIAL value, so a vertical box with no text-orientation at all
// must behave as mixed — Latin rotated. Getting this wrong is invisible on a Latin-only
// page if the default were upright, which is why it is asserted against the explicit
// value rather than only on its own.
func TestMixedIsTheDefault(t *testing.T) {
	implicit := verticalLine(t, "", "NOW")
	explicit := verticalLine(t, "mixed", "NOW")

	if len(implicit.Glyphs) != len(explicit.Glyphs) {
		t.Fatalf("glyph counts differ: %d vs %d", len(implicit.Glyphs), len(explicit.Glyphs))
	}
	for i := range implicit.Glyphs {
		if implicit.Glyphs[i].Rotate != explicit.Glyphs[i].Rotate {
			t.Errorf("glyph %d: implicit rotation %v != explicit mixed %v",
				i, implicit.Glyphs[i].Rotate, explicit.Glyphs[i].Rotate)
		}
	}
	if implicit.Glyphs[0].Rotate == 0 {
		t.Error("the default orientation left Latin upright; mixed must rotate it")
	}
}

// Under `mixed`, an upright script stays upright while Latin rotates — the whole point
// of the value, and the case a Latin-only test cannot see.
//
// NOTE: no bundled face covers CJK, so these glyphs are .notdef — they carry no real
// ink. That does not weaken the assertion: orientation is decided from the glyph's
// RUNES, which survive the .notdef substitution, and the runes are what the classifier
// reads. It does mean this cannot be shown in the visual showcase, which says so rather
// than displaying a row of empty boxes.
func TestMixedKeepsCJKUprightAndRotatesLatin(t *testing.T) {
	cjk := verticalLine(t, "mixed", "漢字") // 漢字
	for i, g := range cjk.Glyphs {
		if g.Rotate != 0 {
			t.Errorf("Han glyph %d rotated by %v; mixed keeps upright scripts upright", i, g.Rotate)
		}
	}

	latin := verticalLine(t, "mixed", "AB")
	for i, g := range latin.Glyphs {
		if g.Rotate == 0 {
			t.Errorf("Latin glyph %d was left upright; mixed rotates it", i)
		}
	}
}

// CJK punctuation and full-width forms are outside stdlib's script tables but must stay
// upright with the text they punctuate. This pins the block ranges uprightRune adds.
func TestMixedKeepsCJKPunctuationUpright(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"ideographic full stop", "。"},
		{"ideographic comma", "、"},
		{"fullwidth A", "Ａ"},
	} {
		ln := verticalLine(t, "mixed", tc.text)
		for i, g := range ln.Glyphs {
			if g.Rotate != 0 {
				t.Errorf("%s: glyph %d rotated by %v, want upright", tc.name, i, g.Rotate)
			}
		}
	}
}

// A rotated glyph advances the pen by its HORIZONTAL extent, because it is lying on its
// side. Using the vertical advance would space sideways Latin one em per letter, which
// is the fixed-pitch look of `upright` — so the two orientations would be
// indistinguishable in spacing despite rendering completely differently.
func TestSidewaysAdvancesByHorizontalExtent(t *testing.T) {
	up := verticalLine(t, "upright", "NOW")
	side := verticalLine(t, "sideways", "NOW")

	// Upright: uniform one em (24pt at font-size:24px).
	for i, g := range up.Glyphs {
		if d := g.AdvancePt - 24; d > 0.01 || d < -0.01 {
			t.Errorf("upright glyph %d advance = %v, want 24 (one em)", i, g.AdvancePt)
		}
	}
	// Sideways: varies with letter width, so N and W must differ.
	if len(side.Glyphs) != 3 {
		t.Fatalf("got %d glyphs, want 3", len(side.Glyphs))
	}
	if side.Glyphs[0].AdvancePt == side.Glyphs[2].AdvancePt {
		t.Errorf("sideways N and W share advance %v; a rotated glyph advances by its width, "+
			"which differs between them", side.Glyphs[0].AdvancePt)
	}
	// And the whole run is shorter than the upright one, since Latin is narrower than an em.
	upEnd := up.Glyphs[2].Y + up.Glyphs[2].AdvancePt
	sideEnd := side.Glyphs[2].Y + side.Glyphs[2].AdvancePt
	if sideEnd >= upEnd {
		t.Errorf("sideways run ends at %v, not shorter than the upright %v", sideEnd, upEnd)
	}
}

// The pen's leading inset depends on orientation. An upright glyph keeps its own
// baseline, so the pen starts one ascent down and the glyph's top clears the content
// edge. A SIDEWAYS glyph has been turned about its origin, so its ascent no longer
// extends upward — insetting by the ascent would push the whole run down by a line.
func TestLeadingInsetFollowsOrientation(t *testing.T) {
	up := verticalLine(t, "upright", "NOW")
	side := verticalLine(t, "sideways", "NOW")

	if up.Glyphs[0].Y <= 0 {
		t.Errorf("upright first glyph Y = %v; it must clear the content top by its ascent", up.Glyphs[0].Y)
	}
	if side.Glyphs[0].Y != 0 {
		t.Errorf("sideways first glyph Y = %v, want 0: a rotated glyph's ascent runs sideways, "+
			"so insetting by it would drop the run by a line", side.Glyphs[0].Y)
	}
}

// A sideways glyph's ascent lands to one side of the baseline rather than above it, so
// the baseline shifts to keep the glyph centred in its box. Measured through the paint
// matrix: at +90 degrees em +Y maps to page +X, so the ascent is to the RIGHT and the
// baseline moves left.
func TestSidewaysShiftsTheBaselineToStayCentred(t *testing.T) {
	up := verticalLine(t, "upright", "NOW")
	side := verticalLine(t, "sideways", "NOW")

	if side.Glyphs[0].X >= up.Glyphs[0].X {
		t.Errorf("sideways baseline X = %v, want left of the upright %v",
			side.Glyphs[0].X, up.Glyphs[0].X)
	}
}

// text-orientation has NO effect in a horizontal writing mode (CSS Writing Modes 4
// §5.1): there is no vertical line for it to orient glyphs within. It must not rotate
// anything, or an ordinary page carrying the declaration would scramble.
func TestTextOrientationDoesNothingHorizontally(t *testing.T) {
	for _, o := range []string{"upright", "sideways", "mixed"} {
		root := layoutTreeFor(t, `<html><body style="margin:0">`+
			`<div style="font-size:24px;text-orientation:`+o+`">NOW</div></body></html>`, 300, nil)
		ln := firstGlyphLine(root)
		if ln == nil {
			t.Fatalf("%s: no glyphs", o)
		}
		if ln.Vertical {
			t.Errorf("%s: a horizontal box produced a vertical line", o)
		}
		for i, g := range ln.Glyphs {
			if g.Rotate != 0 {
				t.Errorf("%s: horizontal glyph %d rotated by %v", o, i, g.Rotate)
			}
		}
	}
}

// The property inherits (CSS Writing Modes 4 §5.1) — the same trap writing-mode hit,
// where inheritFrom silently resets an unregistered field to its initial value instead
// of inheriting it. Asserted through to layout, where it matters.
func TestTextOrientationInherits(t *testing.T) {
	root := layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="writing-mode:vertical-rl;font-size:24px;text-orientation:upright">`+
		`<div><span>NOW</span></div></div></body></html>`, 300, nil)
	ln := firstGlyphLine(root)
	if ln == nil {
		t.Fatal("no glyphs")
	}
	for i, g := range ln.Glyphs {
		if g.Rotate != 0 {
			t.Errorf("glyph %d rotated by %v; the nested box did not inherit upright", i, g.Rotate)
		}
	}
}

// A shrink-to-fit box is sized by the intrinsic measure helpers, which measure the
// HORIZONTAL axis — so a vertical inline-block comes out as wide as its text is long.
// Transposing that touches the measure seam table/grid/flex all share, which is outside
// this phase; it logs rather than silently mis-sizing.
func TestVerticalShrinkToFitIsLogged(t *testing.T) {
	for _, tc := range []struct{ name, style string }{
		{"inline-block", "display:inline-block"},
		{"float", "float:left"},
	} {
		var msgs []string
		logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
		layoutTreeFor(t, `<html><body style="margin:0">`+
			`<div style="writing-mode:vertical-rl;font-size:20px;`+tc.style+`">ABCDEF</div>`+
			`</body></html>`, 300, logf)

		var found bool
		for _, m := range msgs {
			if strings.Contains(m, "shrink-to-fit") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: a vertical shrink-to-fit box did not log; messages: %v", tc.name, msgs)
		}
	}
}

// ...and a plain block-level vertical box must NOT log it: it is sized by its containing
// block, not by the intrinsic measure, and it is the case that actually works.
func TestVerticalBlockDoesNotLogShrinkToFit(t *testing.T) {
	var msgs []string
	logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
	layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="writing-mode:vertical-rl;font-size:20px">ABCDEF</div></body></html>`, 300, logf)

	for _, m := range msgs {
		if strings.Contains(m, "shrink-to-fit") {
			t.Errorf("a block-level vertical box logged the shrink-to-fit gap: %q", m)
		}
	}
}
