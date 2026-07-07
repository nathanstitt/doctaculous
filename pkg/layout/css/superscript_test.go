package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// superGlyphAndBaseline lays src out and returns, for the glyph whose runes equal
// want, its effective baseline Y (ln.BaselineY - shift, top-left origin so a super
// shift yields a smaller Y) and the line's own baseline Y. It finds the glyph on a
// line shared with surrounding normal text, so the line baseline is fixed by that
// text and the shift is measured against it — matching how CSS super/sub shifts a run
// relative to the line it sits on.
func superGlyphAndBaseline(t *testing.T, src, want string) (glyphY, lineBaseline float64) {
	t.Helper()
	root := layoutWithLoader(t, src, 400, resource.MapLoader{}, nil)
	found := false
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil || found {
			return
		}
		for li := range f.Lines {
			ln := &f.Lines[li]
			for gi := range ln.Glyphs {
				g := &ln.Glyphs[gi]
				if g.Outline == nil || string(g.Runes) != want {
					continue
				}
				glyphY = ln.BaselineY - g.BaselineShiftPt
				lineBaseline = ln.BaselineY
				found = true
				return
			}
		}
		for _, c := range f.Children {
			walk(c)
			if found {
				return
			}
		}
	}
	walk(root)
	if !found {
		t.Fatalf("no glyph %q found in layout of %q", want, src)
	}
	return glyphY, lineBaseline
}

// TestSuperscriptShiftsGlyphUp asserts a superscript run's glyph sits above the
// surrounding line baseline (smaller Y in top-left origin space) while a plain run's
// glyph sits on it.
func TestSuperscriptShiftsGlyphUp(t *testing.T) {
	// The "2" is superscript; the surrounding "E=mc" and "" fix the line baseline.
	supY, base := superGlyphAndBaseline(t,
		`<body><p>E=mc<span style="vertical-align:super">2</span></p></body>`, "2")
	if !(supY < base) {
		t.Fatalf("superscript glyph Y = %.2f, want < line baseline %.2f", supY, base)
	}
}

// TestSubscriptShiftsGlyphDown asserts a subscript run's glyph sits below the
// surrounding line baseline (larger Y).
func TestSubscriptShiftsGlyphDown(t *testing.T) {
	subY, base := superGlyphAndBaseline(t,
		`<body><p>H<span style="vertical-align:sub">2</span>O</p></body>`, "2")
	if !(subY > base) {
		t.Fatalf("subscript glyph Y = %.2f, want > line baseline %.2f", subY, base)
	}
}

// TestBaselineDefaultUnshifted confirms a run with no vertical-align has zero shift
// (the byte-identical baseline path).
func TestBaselineDefaultUnshifted(t *testing.T) {
	root := layoutWithLoader(t, `<body><p><span>x</span></p></body>`, 400, resource.MapLoader{}, nil)
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		for li := range f.Lines {
			for gi := range f.Lines[li].Glyphs {
				if s := f.Lines[li].Glyphs[gi].BaselineShiftPt; s != 0 {
					t.Errorf("default glyph has non-zero baseline shift %.2f", s)
				}
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
}
