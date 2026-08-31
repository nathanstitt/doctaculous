package inline

import (
	"testing"

	layoutfont "github.com/nathanstitt/omnidoc/pkg/internal/layout/font"
)

// synth builds one glyph per rune of s, as the shaper would for a single run. No font
// is needed: the reorder operates on Runes, so order can be asserted without any RTL
// glyph coverage (no bundled font has Hebrew or Arabic).
func synth(s string) []Glyph {
	var out []Glyph
	for _, r := range s {
		out = append(out, Glyph{
			Runes:   []rune{r},
			Advance: 10,
			Space:   r == ' ',
		})
	}
	return out
}

// visual renders a glyph slice back to a string, in slice order.
func visual(gs []Glyph) string {
	var out []rune
	for _, g := range gs {
		out = append(out, g.Runes...)
	}
	return string(out)
}

func TestReorderLTRLatinIsNoOp(t *testing.T) {
	// The overwhelmingly common case: an LTR paragraph with no RTL characters must not
	// be touched at all — same order, and the SAME BACKING SLICE (no allocation), which
	// is what keeps Latin-only documents byte-identical to the pre-bidi engine.
	in := synth("hello world")
	out, changed := reorder(in, DirLTR)
	if changed {
		t.Error("LTR Latin text should report no reordering")
	}
	if visual(out) != "hello world" {
		t.Errorf("LTR Latin reordered to %q, want unchanged", visual(out))
	}
	if len(out) != len(in) || &out[0] != &in[0] {
		t.Error("LTR Latin should return the input slice unchanged (no allocation)")
	}
}

func TestReorderRTLRunInLTRParagraph(t *testing.T) {
	// "abc אבג def" — an LTR paragraph with a Hebrew island. The Hebrew reverses in
	// place; the Latin around it stays put.
	in := synth("abc אבג def")
	out, changed := reorder(in, DirLTR)
	if !changed {
		t.Fatal("a Hebrew run in an LTR paragraph must reorder")
	}
	if got, want := visual(out), "abc גבא def"; got != want {
		t.Errorf("reorder = %q, want %q (the Hebrew run reverses, the Latin does not)", got, want)
	}
}

func TestReorderRTLParagraph(t *testing.T) {
	// A wholly Hebrew paragraph reverses entirely.
	in := synth("אבג")
	out, changed := reorder(in, DirRTL)
	if !changed {
		t.Fatal("an RTL paragraph of Hebrew must reorder")
	}
	if got, want := visual(out), "גבא"; got != want {
		t.Errorf("reorder = %q, want %q", got, want)
	}
}

func TestReorderLTRIslandInRTLParagraph(t *testing.T) {
	// "אב english גד" in an RTL paragraph: the Latin island keeps its internal
	// left-to-right order while the Hebrew around it reverses and the runs lay out
	// right-to-left overall.
	in := synth("אב english גד")
	out, changed := reorder(in, DirRTL)
	if !changed {
		t.Fatal("mixed text in an RTL paragraph must reorder")
	}
	got := visual(out)
	// The embedded Latin word must survive intact and readable.
	if !contains(got, "english") {
		t.Errorf("reorder = %q; the embedded LTR word must keep its internal order", got)
	}
	// The Hebrew must have reversed somewhere in the result.
	if contains(got, "אב") && contains(got, "גד") {
		t.Errorf("reorder = %q; the Hebrew runs should be reversed, not verbatim", got)
	}
}

func TestReorderPreservesGlyphCount(t *testing.T) {
	// Whatever the ordering, no glyph may be dropped or duplicated — a mapping bug
	// would otherwise silently lose text.
	for _, s := range []string{
		"abc אבג def",
		"אב english גד",
		"abc (אב) def",
		"12 אבג 34",
		"אבג",
		"plain latin",
	} {
		for _, dir := range []ParagraphDirection{DirLTR, DirRTL} {
			in := synth(s)
			out, _ := reorder(in, dir)
			if len(out) != len(in) {
				t.Errorf("%q dir=%v: got %d glyphs, want %d", s, dir, len(out), len(in))
			}
			if got, want := sortedRunes(visual(out)), sortedRunes(s); got != want {
				t.Errorf("%q dir=%v: glyph MULTISET changed (%q vs %q)", s, dir, got, want)
			}
		}
	}
}

func TestReorderKeepsAtomicsAndBreaks(t *testing.T) {
	// A glyph with no runes (an atomic inline box) participates as a neutral object and
	// must survive the round trip rather than being dropped by the rune mapping.
	in := []Glyph{
		{Runes: []rune{'a'}, Advance: 10},
		{Atomic: &AtomicItem{WidthPt: 20}, Advance: 20},
		{Runes: []rune{'b'}, Advance: 10},
	}
	out, _ := reorder(in, DirLTR)
	if len(out) != 3 {
		t.Fatalf("got %d glyphs, want 3 (the atomic must be preserved)", len(out))
	}
	atomics := 0
	for _, g := range out {
		if g.Atomic != nil {
			atomics++
		}
	}
	if atomics != 1 {
		t.Errorf("got %d atomic glyphs, want exactly 1", atomics)
	}
}

// TestMakeVisualLineMetricsAreLogical pins the subtle part of the split: a line's
// width and justification gap count are LOGICAL properties (they exclude the space
// that ENDS the text), but VisibleWidth/CountSpaces find that space by scanning from
// the end of the slice. In an RTL line the trailing space reorders to the visual
// START, so measuring the reordered slice would count it toward the ink width.
func TestMakeVisualLineMetricsAreLogical(t *testing.T) {
	// Hebrew followed by a trailing space: 3 glyphs of ink + 1 trailing space.
	in := synth("אבג ")
	l := MakeVisualLine(in, DirRTL)
	if got, want := l.WidthPt, 30.0; got != want {
		t.Errorf("WidthPt = %v, want %v (the trailing space must be excluded even though "+
			"it reorders to the visual start)", got, want)
	}
	if len(l.Glyphs) != 4 {
		t.Fatalf("line has %d glyphs, want 4", len(l.Glyphs))
	}
	// And the glyphs themselves ARE in visual order.
	if got := visual(l.Glyphs); got != " גבא" {
		t.Errorf("line glyphs = %q, want %q (visual order)", got, " גבא")
	}
}

func TestHasBidiControl(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"plain ascii", false},
		{"café naïve", false}, // Latin-1 supplement
		{"Ελληνικά", false},   // Greek
		{"Кириллица", false},  // Cyrillic
		{"שלום", true},        // Hebrew
		{"مرحبا", true},       // Arabic
		{"mixed שלום here", true},
		{"\u200fmark", true}, // RLM with no strong RTL letter
	}
	for _, c := range cases {
		if got := hasBidiControl([]rune(c.text)); got != c.want {
			t.Errorf("hasBidiControl(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// sortedRunes returns the runes of s in sorted order, for multiset comparison.
func sortedRunes(s string) string {
	rs := []rune(s)
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j] < rs[j-1]; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
	return string(rs)
}

// TestMirrorGlyphSwapsBracketShape covers rule L4: a bracket inside a right-to-left
// run is DRAWN as its mirror image, while the character it stands for is unchanged.
//
// It shapes through a real bundled face because mirroring must re-resolve the outline
// and GID from the font — the synthetic glyphs the other tests use carry no Face, so
// they exercise ordering but not L4.
func TestMirrorGlyphSwapsBracketShape(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	shaped := Shape(faces, []Run{{Text: "()", Family: "serif", SizePt: 16}}, nil)
	if len(shaped) != 2 {
		t.Fatalf("shaped %d glyphs for %q, want 2", len(shaped), "()")
	}
	open, close := shaped[0], shaped[1]
	if open.Face == nil || close.Face == nil {
		t.Skip("bundled face did not resolve; nothing to mirror")
	}
	if open.GID == close.GID {
		t.Fatal("'(' and ')' resolved to the same GID; the fixture cannot detect mirroring")
	}

	// Mirroring '(' must produce the ')' shape...
	g := open
	mirrorGlyph(&g)
	if g.GID != close.GID {
		t.Errorf("mirrored '(' has GID %d, want %d (the ')' glyph)", g.GID, close.GID)
	}
	// ...while still REPORTING the original character, so a text-emitting backend's
	// /ToUnicode recovers what the author actually wrote.
	if len(g.Runes) != 1 || g.Runes[0] != '(' {
		t.Errorf("mirrored glyph Runes = %q, want %q (the ORIGINAL character)", string(g.Runes), "(")
	}

	// A non-mirrorable character is untouched.
	letters := Shape(faces, []Run{{Text: "a", Family: "serif", SizePt: 16}}, nil)
	if len(letters) == 1 {
		before := letters[0]
		after := before
		mirrorGlyph(&after)
		if after.GID != before.GID {
			t.Errorf("'a' should not mirror; GID changed %d -> %d", before.GID, after.GID)
		}
	}
}

// TestReorderMirrorsBracketsInRTLRun: brackets around an RTL run mirror, brackets in
// the surrounding LTR text do not. Uses real faces so L4 can resolve outlines.
func TestReorderMirrorsBracketsInRTLRun(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	// The RLO makes the following Latin an RTL run, so its bracket must mirror.
	glyphs := Shape(faces, []Run{{Text: "\u202e(a)", Family: "serif", SizePt: 16}}, nil)
	openGID, closeGID := bracketGIDs(t, faces)

	out, changed := reorder(glyphs, DirLTR)
	if !changed {
		t.Fatal("an RLO run must reorder")
	}
	// Visual order reverses to ")a(" in POSITION; each bracket also mirrors in SHAPE,
	// so the first painted bracket carries the '(' glyph and the last the ')' glyph.
	var gids []uint16
	for _, g := range out {
		if g.Face != nil && (len(g.Runes) == 1 && (g.Runes[0] == '(' || g.Runes[0] == ')')) {
			gids = append(gids, g.GID)
		}
	}
	if len(gids) != 2 {
		t.Fatalf("found %d bracket glyphs, want 2", len(gids))
	}
	if gids[0] != openGID || gids[1] != closeGID {
		t.Errorf("bracket GIDs in visual order = %v, want [%d %d] — a ')' reordered to the "+
			"visual start must be DRAWN as '(' (rule L4)", gids, openGID, closeGID)
	}
}

// bracketGIDs returns the GIDs the bundled serif face uses for '(' and ')'.
func bracketGIDs(t *testing.T, faces *layoutfont.FaceCache) (open, close uint16) {
	t.Helper()
	gs := Shape(faces, []Run{{Text: "()", Family: "serif", SizePt: 16}}, nil)
	if len(gs) != 2 || gs[0].Face == nil {
		t.Skip("bundled face did not resolve brackets")
	}
	return gs[0].GID, gs[1].GID
}

// TestScriptFallbackShapesRTL covers the per-rune script fallback: a run whose family
// is Latin still shapes Hebrew and Arabic, because each bundled face covers only its
// own script and the covering face is chosen per RUNE.
//
// Before this existed the shaper dropped any rune its run face could not map, so RTL
// text silently vanished — no error, no log, just missing words.
func TestScriptFallbackShapesRTL(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	cases := []struct {
		name, text string
		wantRunes  int
	}{
		{"hebrew", "אבג", 3},
		{"arabic", "مرحبا", 5},
		{"mixed", "ab אב", 5}, // 2 Latin + the space (which the Latin face maps) + 2 Hebrew
	}
	for _, c := range cases {
		// The run asks for "serif", which resolves to TeX Gyre Termes — no RTL coverage.
		glyphs := Shape(faces, []Run{{Text: c.text, Family: "serif", SizePt: 16}}, nil)
		got := 0
		for _, g := range glyphs {
			if len(g.Runes) > 0 {
				got++
			}
		}
		if got != c.wantRunes {
			t.Errorf("%s: shaped %d rune-carrying glyphs from %q, want %d — RTL runes are "+
				"being dropped instead of falling back to a covering face", c.name, got, c.text, c.wantRunes)
		}
	}
}

// TestScriptFallbackUsesTheCoveringFace: a fallback glyph must carry the face it was
// actually resolved from, not the run's. A GID is only meaningful against its own
// face, so mixing them up would make a text-emitting backend embed the wrong program
// or re-fetch the wrong glyph.
func TestScriptFallbackUsesTheCoveringFace(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{Text: "aא", Family: "serif", SizePt: 16}}, nil)
	if len(glyphs) != 2 {
		t.Fatalf("shaped %d glyphs, want 2", len(glyphs))
	}
	latin, hebrew := glyphs[0], glyphs[1]
	if latin.Face == nil || hebrew.Face == nil {
		t.Fatal("both glyphs should carry a face")
	}
	if latin.Face == hebrew.Face {
		t.Error("the Hebrew glyph carries the run's Latin face; it must carry the covering " +
			"script face it was actually resolved from")
	}
	if len(hebrew.Runes) != 1 || hebrew.Runes[0] != 'א' {
		t.Errorf("Hebrew glyph Runes = %q, want %q", string(hebrew.Runes), "א")
	}
}

// TestArabicContextualShaping is the core slice-4 assertion: an Arabic letter takes a
// different glyph depending on whether it joins to its neighbours, so the same
// character shaped in context must NOT produce the isolated-form glyph.
//
// Rune-at-a-time shaping (the pre-harfbuzz path, still used for simple scripts) can
// only ever emit isolated forms, which is what made Arabic render as disconnected
// letters.
func TestArabicContextualShaping(t *testing.T) {
	faces := layoutfont.NewFaceCache()

	// The same letter alone vs. inside a word.
	alone := Shape(faces, []Run{{Text: "ح", Family: "serif", SizePt: 16}}, nil)
	inWord := Shape(faces, []Run{{Text: "مرحبا", Family: "serif", SizePt: 16}}, nil)
	if len(alone) != 1 {
		t.Fatalf("isolated letter shaped to %d glyphs, want 1", len(alone))
	}
	var medial *Glyph
	for i := range inWord {
		if len(inWord[i].Runes) == 1 && inWord[i].Runes[0] == 'ح' {
			medial = &inWord[i]
			break
		}
	}
	if medial == nil {
		t.Fatal("could not find the ح glyph in the shaped word")
	}
	if medial.GID == alone[0].GID {
		t.Errorf("ح has GID %d both alone and mid-word; contextual shaping is not being "+
			"applied (the letter should take its medial form)", medial.GID)
	}
	if medial.Outline == nil {
		t.Error("the contextual glyph has no outline; it would paint as nothing")
	}
}

// TestArabicClusterAttribution: every source rune must be attributed to exactly one
// glyph. Harfbuzz may map several runes to one glyph (a ligature) or one rune to
// several (a base plus marks), so only the FIRST glyph of a cluster carries its runes
// — otherwise a backend mapping glyphs back to text (the PDF writer's /ToUnicode)
// would duplicate or drop characters.
func TestArabicClusterAttribution(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	for _, text := range []string{"مرحبا", "الله", "ب", "لا"} {
		glyphs := Shape(faces, []Run{{Text: text, Family: "serif", SizePt: 16}}, nil)
		var got []rune
		carriers := 0
		for _, g := range glyphs {
			got = append(got, g.Runes...)
			if len(g.Runes) > 0 {
				carriers++
			}
		}
		if string(got) != text {
			t.Errorf("shaping %q attributed runes %q; every source rune must appear "+
				"exactly once, in order", text, string(got))
		}
		// The runes must be SPREAD across glyphs, not all dumped on one. A single
		// carrier for a multi-letter word means the cluster extents collapsed — which
		// is exactly what happens if clusterRunes assumes an order the shaper is not
		// actually producing. The concatenation check above cannot see that.
		if n := len([]rune(text)); n > 1 && carriers < 2 {
			t.Errorf("shaping %q put every rune on %d glyph(s); cluster attribution has "+
				"collapsed (each cluster should carry its own runes)", text, carriers)
		}
	}
}

// TestArabicShapingKeepsLogicalOrder pins the direction decision. Harfbuzz emits
// VISUAL order for RTL by default; the shaper forces LTR so the whole pipeline stays
// logical up to the single L2 reorder in MakeVisualLine. Shaping in visual order here
// would be reversed a second time and come out backwards.
func TestArabicShapingKeepsLogicalOrder(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{Text: "مرحبا", Family: "serif", SizePt: 16}}, nil)
	var got []rune
	for _, g := range glyphs {
		got = append(got, g.Runes...)
	}
	if string(got) != "مرحبا" {
		t.Errorf("shaped runes = %q, want the LOGICAL source order %q — harfbuzz must be "+
			"driven left-to-right so the L2 pass is the only reorder", string(got), "مرحبا")
	}
}

// TestHebrewIsNotComplexShaped: Hebrew is non-joining, so it stays on the cheap
// per-rune path. This keeps the complex path scoped to scripts that need it.
func TestHebrewIsNotComplexShaped(t *testing.T) {
	for _, r := range []rune{'א', 'ב', 'ג'} {
		if needsComplexShaping(r) {
			t.Errorf("%q should not need complex shaping (Hebrew is non-joining)", r)
		}
	}
	for _, r := range []rune{'م', 'ر', 'ح'} {
		if !needsComplexShaping(r) {
			t.Errorf("%q should need complex shaping (Arabic joins)", r)
		}
	}
	for _, r := range []rune{'a', 'Z', '1', ' '} {
		if needsComplexShaping(r) {
			t.Errorf("%q should not need complex shaping", r)
		}
	}
}
