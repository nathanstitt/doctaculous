package draw

import (
	"math"
	"testing"
)

// placeVertical lays out one <text> and returns its placed glyphs.
func placeVertical(t *testing.T, attrs, body string) []placedGlyph {
	t.Helper()
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 400"
	  ><text x="100" y="50" font-size="20" `+attrs+`>`+body+`</text></svg>`)
	placed := New(doc).layoutText(firstText(t, doc))
	if len(placed) == 0 {
		t.Fatalf("no glyphs placed for attrs %q", attrs)
	}
	return placed
}

// The pen walks DOWN the page in a vertical writing mode: every glyph shares one X and
// each advances past the last in Y. This is the structural claim the whole feature
// rests on — SVG lays text out itself rather than through the CSS inline layer, so none
// of the CSS-side work reached it until now.
func TestVerticalTextAdvancesDownThePage(t *testing.T) {
	placed := placeVertical(t, `writing-mode="vertical-rl" text-orientation="upright"`, "Wax")
	if len(placed) != 3 {
		t.Fatalf("got %d glyphs, want 3", len(placed))
	}
	for i := range placed {
		if !placed[i].vertical {
			t.Errorf("glyph %d is not marked vertical", i)
		}
		if d := placed[i].penX - placed[0].penX; d > 0.01 || d < -0.01 {
			t.Errorf("glyph %d penX = %v, want %v (all glyphs share the vertical baseline)",
				i, placed[i].penX, placed[0].penX)
		}
	}
	for i := 1; i < len(placed); i++ {
		if placed[i].penY <= placed[i-1].penY {
			t.Errorf("glyph %d penY = %v does not advance past glyph %d penY = %v",
				i, placed[i].penY, i-1, placed[i-1].penY)
		}
	}
}

// A horizontal <text> is byte-identical to what it was: the pen walks X, nothing is
// marked vertical, and no glyph is rotated. This is the control that keeps the change
// additive for every existing document.
func TestHorizontalTextUnchanged(t *testing.T) {
	placed := placeVertical(t, "", "Wax")
	for i := range placed {
		if placed[i].vertical {
			t.Errorf("glyph %d of an ordinary <text> is marked vertical", i)
		}
		if placed[i].rotateRad != 0 {
			t.Errorf("glyph %d of an ordinary <text> is rotated by %v", i, placed[i].rotateRad)
		}
		if placed[i].penY != placed[0].penY {
			t.Errorf("glyph %d penY = %v, want %v (a horizontal line shares one baseline)",
				i, placed[i].penY, placed[0].penY)
		}
	}
	for i := 1; i < len(placed); i++ {
		if placed[i].penX <= placed[i-1].penX {
			t.Errorf("horizontal glyph %d did not advance in X", i)
		}
	}
}

// An UPRIGHT glyph advances by the font's VERTICAL metric — one em for a face with no
// vhea, uniform across letters of different widths. A SIDEWAYS one advances by its
// horizontal extent, because it is lying on its side. Confusing the two spaces sideways
// Latin one em per letter, which renders as legible fixed-pitch text and reads as
// intentional rather than as a bug, so both halves are pinned.
func TestVerticalAdvancePerOrientation(t *testing.T) {
	const sizePt = 20

	up := placeVertical(t, `writing-mode="vertical-rl" text-orientation="upright"`, "Wax")
	for i := range up {
		if d := up[i].advance - sizePt; d > 0.01 || d < -0.01 {
			t.Errorf("upright glyph %d advance = %v, want %v (one em)", i, up[i].advance, float64(sizePt))
		}
	}

	side := placeVertical(t, `writing-mode="vertical-rl" text-orientation="sideways"`, "Wax")
	if len(side) != 3 {
		t.Fatalf("got %d sideways glyphs, want 3", len(side))
	}
	// W and x have visibly different widths, so their advances must differ.
	if side[0].advance == side[2].advance {
		t.Errorf("sideways W and x share advance %v; a recumbent glyph advances by its "+
			"own width, which differs between them", side[0].advance)
	}
	// And the sideways run is shorter overall, since Latin is narrower than an em.
	upEnd := up[len(up)-1].penY + up[len(up)-1].advance
	sideEnd := side[len(side)-1].penY + side[len(side)-1].advance
	if sideEnd >= upEnd {
		t.Errorf("sideways run ends at %v, not shorter than the upright %v", sideEnd, upEnd)
	}
}

// text-orientation decides the glyph turn, exactly as it does on the CSS side — the
// classifier is shared (inline.GlyphRotation), so this asserts the wiring reached SVG
// rather than re-testing the classification itself.
func TestVerticalTextOrientation(t *testing.T) {
	for _, tc := range []struct {
		orient string
		want   float64
	}{
		{"upright", 0},
		{"sideways", math.Pi / 2},
		{"mixed", math.Pi / 2}, // Latin rotates under mixed
		{"", math.Pi / 2},      // mixed is the initial value
	} {
		attrs := `writing-mode="vertical-rl"`
		if tc.orient != "" {
			attrs += ` text-orientation="` + tc.orient + `"`
		}
		placed := placeVertical(t, attrs, "Wax")
		for i := range placed {
			if d := placed[i].rotateRad - tc.want; d > 1e-9 || d < -1e-9 {
				t.Errorf("text-orientation=%q glyph %d rotation = %v, want %v",
					tc.orient, i, placed[i].rotateRad, tc.want)
			}
		}
	}
}

// A character's own rotate= COMPOSES with the writing mode's orientation rather than
// replacing it. SVG defines rotate as an additional turn of the glyph about its origin,
// so in a vertical run the two add: a sideways glyph with rotate="90" ends up half
// turned. Replacing instead would silently drop the writing mode for any run that also
// used rotate.
func TestCharacterRotateComposesWithOrientation(t *testing.T) {
	placed := placeVertical(t, `writing-mode="vertical-rl" text-orientation="sideways" rotate="90"`, "Wax")
	want := math.Pi/2 + math.Pi/2
	for i := range placed {
		if d := placed[i].rotateRad - want; d > 1e-9 || d < -1e-9 {
			t.Errorf("glyph %d rotation = %v, want %v (orientation + the character's own rotate)",
				i, placed[i].rotateRad, want)
		}
	}
	// The horizontal case must be unaffected: rotate alone, no orientation added.
	h := placeVertical(t, `rotate="90"`, "Wax")
	for i := range h {
		if d := h[i].rotateRad - math.Pi/2; d > 1e-9 || d < -1e-9 {
			t.Errorf("horizontal glyph %d rotation = %v, want just the character's rotate", i, h[i].rotateRad)
		}
	}
}

// text-anchor aligns a chunk along its own INLINE axis, which is Y in a vertical run.
// Shifting X instead — which the anchor pass did before it was made axis-aware — moves
// a vertical chunk sideways off its baseline while leaving it hanging past its start
// point, so both the shifted axis and the unshifted one are asserted.
func TestVerticalTextAnchorShiftsAlongY(t *testing.T) {
	start := placeVertical(t, `writing-mode="vertical-rl" text-orientation="upright" text-anchor="start"`, "Wax")
	mid := placeVertical(t, `writing-mode="vertical-rl" text-orientation="upright" text-anchor="middle"`, "Wax")
	end := placeVertical(t, `writing-mode="vertical-rl" text-orientation="upright" text-anchor="end"`, "Wax")

	// The cross axis must NOT move: all three sit on the same vertical baseline.
	for _, tc := range []struct {
		name string
		got  []placedGlyph
	}{{"middle", mid}, {"end", end}} {
		if d := tc.got[0].penX - start[0].penX; d > 0.01 || d < -0.01 {
			t.Errorf("text-anchor=%s moved the chunk in X (%v vs %v); a vertical chunk "+
				"anchors along Y", tc.name, tc.got[0].penX, start[0].penX)
		}
	}

	// The inline axis must move, by half the run for middle and the whole run for end.
	extent := (start[len(start)-1].penY + start[len(start)-1].advance) - start[0].penY
	if d := (start[0].penY - mid[0].penY) - extent/2; d > 0.01 || d < -0.01 {
		t.Errorf("text-anchor=middle shifted Y by %v, want %v (half the run)",
			start[0].penY-mid[0].penY, extent/2)
	}
	if d := (start[0].penY - end[0].penY) - extent; d > 0.01 || d < -0.01 {
		t.Errorf("text-anchor=end shifted Y by %v, want %v (the whole run)",
			start[0].penY-end[0].penY, extent)
	}
}

// writing-mode establishes the mode for a whole <text>, so a declaration on a <tspan>
// inside one is ignored — but the property still INHERITS, so an ancestor <g> carrying
// it does apply. The two rules pull in opposite directions and a fix for one broke the
// other during development, so both are pinned together.
//
// This matters more here than the spec wording suggests: style is resolved per
// CHARACTER, so without the tspan rule a mid-run declaration silently turns just the
// glyphs it covers through a right angle. resvg's on-tspan.svg and inheritance.svg are
// the fixtures that caught each half.
func TestWritingModeAppliesToTextNotTspan(t *testing.T) {
	// On a tspan: ignored, the run stays horizontal.
	onTspan := placeVertical(t, "", `<tspan writing-mode="vertical-rl">Wax</tspan>`)
	for i := range onTspan {
		if onTspan[i].vertical {
			t.Errorf("glyph %d went vertical from a writing-mode on a <tspan>; the property "+
				"establishes the mode for the whole <text>", i)
		}
	}

	// Inherited from an ancestor <g>: honoured.
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 400"
	  ><g writing-mode="vertical-rl"><text x="100" y="50" font-size="20">Wax</text></g></svg>`)
	inherited := New(doc).layoutText(firstText(t, doc))
	if len(inherited) == 0 {
		t.Fatal("no glyphs placed")
	}
	for i := range inherited {
		if !inherited[i].vertical {
			t.Errorf("glyph %d did not inherit the writing-mode from its ancestor <g>", i)
		}
	}
}

// dx/dy and absolute x/y keep their PHYSICAL meaning in a vertical run — SVG defines
// them as user-space coordinates, not logical ones — so a dy still moves the pen down
// and an absolute x still resets the cross axis. The pen walk shares that code with the
// horizontal path, and this pins that sharing is correct rather than accidental.
func TestVerticalHonoursPhysicalDxDy(t *testing.T) {
	base := placeVertical(t, `writing-mode="vertical-rl" text-orientation="upright"`, "Wax")
	shifted := placeVertical(t, `writing-mode="vertical-rl" text-orientation="upright" dx="7"`, "Wax")

	if d := (shifted[0].penX - base[0].penX) - 7; d > 0.01 || d < -0.01 {
		t.Errorf("dx=7 moved the vertical pen by %v in X, want 7 (dx is physical)",
			shifted[0].penX-base[0].penX)
	}

	dyShifted := placeVertical(t, `writing-mode="vertical-rl" text-orientation="upright" dy="7"`, "Wax")
	if d := (dyShifted[0].penY - base[0].penY) - 7; d > 0.01 || d < -0.01 {
		t.Errorf("dy=7 moved the vertical pen by %v in Y, want 7",
			dyShifted[0].penY-base[0].penY)
	}
}
