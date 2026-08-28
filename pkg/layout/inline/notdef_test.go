package inline

import (
	"image/color"
	"strings"
	"testing"

	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// missingRune is a code point no bundled face maps and no script fallback covers:
// U+1F327 CLOUD WITH RAIN, one of the weather emoji that vanished on the hardware
// this feature was written for. Using the real character keeps the test honest —
// if a future bundle ever ships emoji, this test starts failing rather than
// silently asserting nothing.
const missingRune = '🌧'

// shapeText is the common setup: shape one run of text in a family that resolves,
// collecting log lines.
func shapeText(t *testing.T, text string) (glyphs []Glyph, logs []string) {
	t.Helper()
	faces := layoutfont.NewFaceCache()
	logf := func(f string, a ...any) { logs = append(logs, f) }
	glyphs = Shape(faces, []Run{{
		Text:   text,
		Family: "Arial",
		SizePt: 12,
		Color:  color.RGBA{A: 0xff},
	}}, logf)
	return glyphs, logs
}

// TestShapeMissingRuneDrawsNotdef is the core of the feature: a rune no available
// font can draw must produce a VISIBLE glyph, not empty space. Before this, the
// rune was dropped — and because the surrounding text was untouched, the result
// read as a layout bug rather than a font problem.
func TestShapeMissingRuneDrawsNotdef(t *testing.T) {
	glyphs, _ := shapeText(t, string(missingRune))

	if len(glyphs) != 1 {
		t.Fatalf("glyphs = %d, want 1 (the rune must not be dropped)", len(glyphs))
	}
	g := glyphs[0]
	if g.Outline == nil || g.Outline.Empty() {
		t.Fatal("the .notdef glyph carries no outline; it would render as empty space — the exact bug this fixes")
	}
	if g.Advance <= 0 {
		t.Errorf("Advance = %v, want > 0 so the box occupies space on the line", g.Advance)
	}
	// Face must be cleared so paint fills the outline directly. Keeping Face+GID 0
	// would have the PDF writer emit the font's own (blank) .notdef, so the box
	// would appear in a raster and vanish in a PDF of the same page.
	if g.Face != nil {
		t.Error("Face is set on a .notdef glyph; a text backend would emit the font's blank glyph 0 instead of the box")
	}
	// Runes IS kept, so bidi sees the real character's class and SVG's
	// glyph-to-character mapping can still locate the box.
	if len(g.Runes) != 1 || g.Runes[0] != missingRune {
		t.Errorf("Runes = %q, want the original rune so bidi and SVG mapping still work", string(g.Runes))
	}
	if g.Space {
		t.Error("a .notdef glyph is marked as a space; it would become a line-break opportunity")
	}
}

// TestShapeMissingRuneWarnsOnce covers the bounded-logging requirement: a document
// with hundreds of unmappable emoji must not emit hundreds of log lines. The
// warning is what makes the degradation diagnosable, so it must fire — exactly
// once per rune.
func TestShapeMissingRuneWarnsOnce(t *testing.T) {
	const n = 50
	glyphs, logs := shapeText(t, strings.Repeat(string(missingRune), n))

	if len(glyphs) != n {
		t.Fatalf("glyphs = %d, want %d (every occurrence draws a box)", len(glyphs), n)
	}
	missingLogs := 0
	for _, l := range logs {
		if strings.Contains(l, "no glyph for") {
			missingLogs++
		}
	}
	if missingLogs != 1 {
		t.Errorf("logged %d missing-glyph lines for %d occurrences of one rune, want exactly 1", missingLogs, n)
	}
}

// TestShapeMissingRuneWarnsPerDistinctRune checks the key is the RUNE, not a single
// global latch: two different missing characters are two different problems and
// each must be reported, or a diagnostic naming one font gap hides the others.
func TestShapeMissingRuneWarnsPerDistinctRune(t *testing.T) {
	_, logs := shapeText(t, "\U0001F327\U0001F328\U0001F327\U0001F328") // rain, snow, repeated

	missingLogs := 0
	for _, l := range logs {
		if strings.Contains(l, "no glyph for") {
			missingLogs++
		}
	}
	if missingLogs != 2 {
		t.Errorf("logged %d lines for 2 distinct missing runes, want 2", missingLogs)
	}
}

// TestShapeMissingRuneLogsCodePointNotCharacter guards a small but real usability
// property: the whole point is that the character does not render, so a log line
// whose diagnostic content is an unprintable box in the reader's terminal helps
// nobody. The code point must appear as text.
func TestShapeMissingRuneLogsCodePointNotCharacter(t *testing.T) {
	_, logs := shapeText(t, string(missingRune))

	for _, l := range logs {
		if strings.Contains(l, "no glyph for") {
			if !strings.Contains(l, "U+%04X") {
				t.Errorf("log format %q does not report the code point; a bare character is unreadable for a glyph that cannot be drawn", l)
			}
			return
		}
	}
	t.Fatal("no missing-glyph line was logged at all")
}

// TestShapeNilLogfWithMissingRune pins Shape's documented never-panic contract on
// the new path. The DOCX engine passes a nil logf, and it renders text with the
// same shaper.
func TestShapeNilLogfWithMissingRune(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{
		Text:   string(missingRune),
		Family: "Arial",
		SizePt: 12,
		Color:  color.RGBA{A: 0xff},
	}}, nil)
	if len(glyphs) != 1 {
		t.Fatalf("glyphs = %d, want 1", len(glyphs))
	}
}

// TestShapeNotdefDoesNotDisturbNeighbours is the byte-identity guard in miniature:
// inserting an unmappable rune between two words must change ONLY that rune's
// contribution. Every surrounding glyph must keep the outline, advance, and font
// identity it had without it — otherwise a font gap would silently reflow correct
// text, which is the failure mode this whole change exists to prevent.
func TestShapeNotdefDoesNotDisturbNeighbours(t *testing.T) {
	before, _ := shapeText(t, "ab")
	after, _ := shapeText(t, "a"+string(missingRune)+"b")

	if len(after) != len(before)+1 {
		t.Fatalf("glyphs = %d, want %d (one extra .notdef and nothing else)", len(after), len(before)+1)
	}
	// after[0] is 'a', after[1] is .notdef, after[2] is 'b'. Compare by VALUE:
	// each Shape call builds fresh Face and Path values, so pointer identity would
	// differ even when nothing about the shaping changed.
	for i, j := range map[int]int{0: 0, 1: 2} {
		got, want := after[j], before[i]
		if got.Advance != want.Advance {
			t.Errorf("neighbour %d: Advance = %v, want %v", i, got.Advance, want.Advance)
		}
		if got.GID != want.GID {
			t.Errorf("neighbour %d: GID = %d, want %d", i, got.GID, want.GID)
		}
		if (got.Face == nil) != (want.Face == nil) {
			t.Errorf("neighbour %d: font identity appeared or disappeared", i)
		}
		if !samePath(got.Outline, want.Outline) {
			t.Errorf("neighbour %d: outline geometry changed", i)
		}
		if got.Space != want.Space || got.SizePt != want.SizePt {
			t.Errorf("neighbour %d: shaping flags changed", i)
		}
	}
}

// samePath compares two outlines by geometry rather than by pointer, since every
// Shape call builds fresh paths.
func samePath(a, b *render.Path) bool {
	if a == nil || b == nil {
		return a == b
	}
	if len(a.Segments) != len(b.Segments) {
		return false
	}
	for i := range a.Segments {
		if a.Segments[i] != b.Segments[i] {
			return false
		}
	}
	return true
}

// TestShapeNotdefAdvanceMatchesTheFace checks that the box's advance comes from
// the run's own face, so line-breaking measures the text at a sane width. A zero
// advance would stack every missing rune at one x-position; an oversized one would
// wrap lines that should fit.
func TestShapeNotdefAdvanceMatchesTheFace(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	face, ok := faces.Resolve("Arial", pkgfont.Style{})
	if !ok {
		t.Fatal("Arial did not resolve")
	}
	_, wantEm := face.NotdefGlyph()

	glyphs, _ := shapeText(t, string(missingRune))
	if got, want := glyphs[0].Advance, wantEm*12; got != want {
		t.Errorf("Advance = %v, want %v (the face's .notdef advance at 12pt)", got, want)
	}
}

// TestShapeUnmappedInvisibleRuneDrawsNothing is the counterweight to the feature:
// a character that draws no ink even in a font that maps it must NOT get a box.
// Turning on .notdef without this distinction sprays tofu through documents that
// render correctly today — this repo's own HTML showcase carries a U+202F NARROW
// NO-BREAK SPACE that no bundled face maps, and it regressed exactly this way
// before invisibleRune was added.
func TestShapeUnmappedInvisibleRuneDrawsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    rune
	}{
		{"narrow no-break space", ' '},
		{"zero width space", '​'},
		{"left-to-right mark", '\u200e'},
		{"ideographic space", '　'},
		{"variation selector 16", '️'},
		{"word joiner", '⁠'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			glyphs, logs := shapeText(t, string(tc.r))
			if len(glyphs) != 1 {
				t.Fatalf("glyphs = %d, want 1", len(glyphs))
			}
			if o := glyphs[0].Outline; o != nil && !o.Empty() {
				t.Error("an invisible character was given a visible .notdef box; it would invent a mark the author never wrote")
			}
			for _, l := range logs {
				if strings.Contains(l, "no glyph for") {
					t.Error("an invisible character was reported as a missing glyph; nothing about the page is wrong")
				}
			}
		})
	}
}

// TestShapeUnmappedInvisibleRuneKeepsAnAdvance checks the other half: the
// invisible character still separates its neighbours rather than collapsing, which
// is what a browser does with an unmapped space variant.
func TestShapeUnmappedInvisibleRuneKeepsAnAdvance(t *testing.T) {
	glyphs, _ := shapeText(t, " ")
	if len(glyphs) != 1 {
		t.Fatalf("glyphs = %d, want 1", len(glyphs))
	}
	if glyphs[0].Advance <= 0 {
		t.Errorf("Advance = %v, want > 0 so the space still separates its neighbours", glyphs[0].Advance)
	}
}

// TestShapeMappedRunesAreUnaffected is the regression guard for the hot path: text
// whose glyphs all resolve must produce exactly what it did before .notdef existed
// — no extra glyphs, no warnings, no changed advances. Every DOCX and PDF golden
// depends on this holding.
func TestShapeMappedRunesAreUnaffected(t *testing.T) {
	glyphs, logs := shapeText(t, "The quick brown fox, 12345!")

	for _, l := range logs {
		if strings.Contains(l, "no glyph") {
			t.Errorf("ordinary Latin text produced a missing-glyph warning: %s", l)
		}
	}
	for i, g := range glyphs {
		if g.Space {
			continue
		}
		if g.Face == nil {
			t.Errorf("glyph %d lost its font identity; a text backend would draw an outline instead of real text", i)
		}
	}
}
