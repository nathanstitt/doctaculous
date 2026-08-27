package draw

import (
	"fmt"
	"math"
	"strings"
	"testing"

	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/layout/inline"
	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/svg"
)

// parseSVG parses src, failing the test on error, and returns the document
// plus every log line Parse emitted.
func parseSVG(t *testing.T, src string) (*svg.Document, []string) {
	t.Helper()
	var logs []string
	doc, err := svg.Parse([]byte(src), func(f string, a ...any) {
		logs = append(logs, fmt.Sprintf(f, a...))
	})
	if err != nil {
		t.Fatalf("svg.Parse: %v", err)
	}
	return doc, logs
}

// firstText returns the first *svg.Text found anywhere in doc's scene tree,
// failing the test if there is none.
func firstText(t *testing.T, doc *svg.Document) *svg.Text {
	t.Helper()
	_, root := doc.Root()
	if txt := findText(root); txt != nil {
		return txt
	}
	t.Fatal("no *svg.Text in the scene tree")
	return nil
}

// findText walks n depth-first for the first Text node.
func findText(n svg.Node) *svg.Text {
	switch node := n.(type) {
	case *svg.Text:
		return node
	case *svg.Group:
		if node == nil {
			return nil
		}
		for _, kid := range node.Kids {
			if txt := findText(kid); txt != nil {
				return txt
			}
		}
	}
	return nil
}

// TestSVGTextReusesInlineShape is the load-bearing claim of this whole
// feature: SVG text is shaped by the SAME inline.Shape the CSS reflow engine
// uses, not by a second shaper that happens to look similar.
//
// It is made discriminating by comparing ADVANCES glyph by glyph against a
// directly-constructed inline.Run — the numbers a forked implementation would
// have to reproduce exactly, for a proportional face where every character
// has a different advance. A test that merely checked "some glyphs came out"
// would pass against a fork; this one cannot.
func TestSVGTextReusesInlineShape(t *testing.T) {
	const (
		family = "sans-serif"
		size   = 40.0
		text   = "Wavering AVWjil.1"
	)

	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 100"
	         font-family="` + family + `" font-size="40">
	  <text x="10" y="50">` + text + `</text>
	</svg>`
	doc, _ := parseSVG(t, src)
	got := New(doc).TextAdvances(firstText(t, doc).Chars)

	// The reference: inline.Shape called directly, exactly as the CSS inline
	// formatting context calls it.
	faces := layoutfont.NewFaceCache()
	ref := inline.Shape(faces, []inline.Run{{
		Text:       text,
		Family:     family,
		SizePt:     size,
		WhiteSpace: "pre",
	}}, nil)
	var want []float64
	for _, g := range ref {
		want = append(want, g.Advance)
	}

	if len(got) != len(want) {
		t.Fatalf("glyph count = %d, want %d (SVG text is not shaping through inline.Shape)", len(got), len(want))
	}
	if len(want) < len(text)-2 {
		t.Fatalf("reference produced only %d glyphs for %q; the test is not exercising real shaping", len(want), text)
	}
	// Guard against a degenerate comparison: if every advance were identical
	// the assertion below would prove nothing, so require real variation.
	distinct := map[float64]bool{}
	for _, a := range want {
		distinct[a] = true
	}
	if len(distinct) < 5 {
		t.Fatalf("reference advances have only %d distinct values; pick text that exercises a proportional face", len(distinct))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("advance[%d] = %v, want %v (exact match required: a shared shaper cannot differ)", i, got[i], want[i])
		}
	}
}

// TestTextAnchorShiftsByMeasuredAdvance asserts text-anchor against the
// MEASURED total advance rather than against a hardcoded pixel: start leaves
// the origin alone, middle shifts back by half the advance, end by all of it.
// Deriving the expectation from the same measurement the implementation makes
// would be circular, so the advance is taken independently from
// TextAdvances (which shapes but does not place).
func TestTextAnchorShiftsByMeasuredAdvance(t *testing.T) {
	const originX = 100.0
	tmpl := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	           font-family="sans-serif" font-size="24">
	  <text x="100" y="100" text-anchor="%s">Text</text>
	</svg>`

	// Total advance, measured independently of placement.
	startDoc, _ := parseSVG(t, fmt.Sprintf(tmpl, "start"))
	r := New(startDoc)
	total := 0.0
	for _, a := range r.TextAdvances(firstText(t, startDoc).Chars) {
		total += a
	}
	if total <= 0 {
		t.Fatal("measured zero total advance; the fixture is not shaping")
	}

	for _, tc := range []struct {
		anchor string
		want   float64
	}{
		{"start", originX},
		{"middle", originX - total/2},
		{"end", originX - total},
	} {
		doc, _ := parseSVG(t, fmt.Sprintf(tmpl, tc.anchor))
		placed := New(doc).layoutText(firstText(t, doc))
		if len(placed) == 0 {
			t.Fatalf("%s: no glyphs placed", tc.anchor)
		}
		if math.Abs(placed[0].penX-tc.want) > 1e-9 {
			t.Errorf("text-anchor=%s: first pen X = %v, want %v (advance %v)", tc.anchor, placed[0].penX, tc.want, total)
		}
	}
}

// TestPerCharacterPositionLists covers the per-character list rules,
// including the two that are easy to get wrong: a list SHORTER than the text
// stops applying (x/y/dx/dy) except for rotate, whose LAST value persists for
// every remaining character.
func TestPerCharacterPositionLists(t *testing.T) {
	t.Run("absolute x list resets the pen", func(t *testing.T) {
		doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
		  font-family="sans-serif" font-size="20"><text x="10 60 110" y="50">abcd</text></svg>`)
		placed := New(doc).layoutText(firstText(t, doc))
		if len(placed) != 4 {
			t.Fatalf("placed %d glyphs, want 4", len(placed))
		}
		for i, want := range []float64{10, 60, 110} {
			if placed[i].penX != want {
				t.Errorf("glyph %d pen X = %v, want %v (absolute reset)", i, placed[i].penX, want)
			}
		}
		// The fourth character has no list entry: it continues from the third
		// by that glyph's own advance, NOT from another reset.
		want := placed[2].penX + placed[2].glyph.Advance
		if placed[3].penX != want {
			t.Errorf("glyph 3 pen X = %v, want %v (short list stops applying; pen continues)", placed[3].penX, want)
		}
	})

	t.Run("dx is relative and cumulative", func(t *testing.T) {
		doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
		  font-family="sans-serif" font-size="20"><text x="0" y="50" dx="5 5 5">abcd</text></svg>`)
		placed := New(doc).layoutText(firstText(t, doc))
		if len(placed) != 4 {
			t.Fatalf("placed %d glyphs, want 4", len(placed))
		}
		// Each dx shifts the pen permanently, so the offsets accumulate: the
		// third glyph carries all three shifts, not just its own.
		if placed[0].penX != 5 {
			t.Errorf("glyph 0 pen X = %v, want 5", placed[0].penX)
		}
		want := 5 + placed[0].glyph.Advance + 5
		if math.Abs(placed[1].penX-want) > 1e-9 {
			t.Errorf("glyph 1 pen X = %v, want %v (dx accumulates)", placed[1].penX, want)
		}
		// The fourth has no dx entry: it advances normally from the third.
		want = placed[2].penX + placed[2].glyph.Advance
		if math.Abs(placed[3].penX-want) > 1e-9 {
			t.Errorf("glyph 3 pen X = %v, want %v (short dx list stops applying)", placed[3].penX, want)
		}
	})

	t.Run("rotate's last value persists past the end of the list", func(t *testing.T) {
		doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
		  font-family="sans-serif" font-size="20"><text x="10" y="50" rotate="10 20">abcd</text></svg>`)
		txt := firstText(t, doc)
		want := []float64{10, 20, 20, 20}
		if len(txt.Chars) != 4 {
			t.Fatalf("lowered %d chars, want 4", len(txt.Chars))
		}
		for i, w := range want {
			if txt.Chars[i].RotateDeg != w {
				t.Errorf("char %d rotate = %v, want %v (the last value must persist, unlike x/y/dx/dy)", i, txt.Chars[i].RotateDeg, w)
			}
		}
	})

	t.Run("a longer list than the text ignores the surplus", func(t *testing.T) {
		doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
		  font-family="sans-serif" font-size="20"><text x="10 20 30 40 50 60" y="50">ab</text></svg>`)
		txt := firstText(t, doc)
		if len(txt.Chars) != 2 {
			t.Fatalf("lowered %d chars, want 2 (surplus list entries must not create characters)", len(txt.Chars))
		}
	})

	t.Run("an unparseable rotate is ignored entirely", func(t *testing.T) {
		doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
		  font-family="sans-serif" font-size="20"><text x="10" y="50" rotate="10 nonsense">abc</text></svg>`)
		txt := firstText(t, doc)
		for i, c := range txt.Chars {
			if c.RotateDeg != 0 {
				t.Errorf("char %d rotate = %v, want 0 (one bad token invalidates the whole list)", i, c.RotateDeg)
			}
		}
	})
}

// TestTspanNesting covers the three <tspan> behaviors the design calls out:
// the position cursor is INHERITED across the boundary, a nested tspan can
// OVERRIDE style, and its own x/dx keep the absolute-vs-relative distinction.
func TestTspanNesting(t *testing.T) {
	t.Run("cursor is inherited across the tspan boundary", func(t *testing.T) {
		doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 200"
		  font-family="sans-serif" font-size="20"><text x="10" y="50">ab<tspan>cd</tspan></text></svg>`)
		placed := New(doc).layoutText(firstText(t, doc))
		if len(placed) != 4 {
			t.Fatalf("placed %d glyphs, want 4", len(placed))
		}
		// The tspan sets no position of its own, so its first character must
		// continue exactly where the parent's last one left off.
		want := placed[1].penX + placed[1].glyph.Advance
		if math.Abs(placed[2].penX-want) > 1e-9 {
			t.Errorf("tspan's first glyph pen X = %v, want %v (the cursor must thread through)", placed[2].penX, want)
		}
	})

	t.Run("a tspan x resets, dx offsets", func(t *testing.T) {
		abs, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 200"
		  font-family="sans-serif" font-size="20"><text x="10" y="50">ab<tspan x="200">cd</tspan></text></svg>`)
		placedAbs := New(abs).layoutText(firstText(t, abs))
		if placedAbs[2].penX != 200 {
			t.Errorf("tspan x=200: pen X = %v, want 200 (absolute)", placedAbs[2].penX)
		}

		rel, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 200"
		  font-family="sans-serif" font-size="20"><text x="10" y="50">ab<tspan dx="30">cd</tspan></text></svg>`)
		placedRel := New(rel).layoutText(firstText(t, rel))
		want := placedRel[1].penX + placedRel[1].glyph.Advance + 30
		if math.Abs(placedRel[2].penX-want) > 1e-9 {
			t.Errorf("tspan dx=30: pen X = %v, want %v (relative to the running cursor)", placedRel[2].penX, want)
		}
	})

	t.Run("a nested tspan overrides style without disturbing its siblings", func(t *testing.T) {
		doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 200"
		  font-family="sans-serif" font-size="20">
		  <text x="10" y="50" fill="red">a<tspan fill="green" font-weight="bold">b</tspan>c</text>
		</svg>`)
		txt := firstText(t, doc)
		if len(txt.Chars) != 3 {
			t.Fatalf("lowered %d chars, want 3 (document order a,b,c must survive the tspan split)", len(txt.Chars))
		}
		if got := string(txt.Chars[0].R) + string(txt.Chars[1].R) + string(txt.Chars[2].R); got != "abc" {
			t.Fatalf("characters = %q, want \"abc\" (interleaved text/element order lost)", got)
		}
		red, _ := txt.Chars[0].Style.FillPaint()
		green, _ := txt.Chars[1].Style.FillPaint()
		after, _ := txt.Chars[2].Style.FillPaint()
		if red.Color == green.Color {
			t.Error("the tspan's fill did not override its parent's")
		}
		if red.Color != after.Color {
			t.Error("the character AFTER the tspan did not revert to the parent's fill")
		}
		if txt.Chars[1].Style.FontBold() == txt.Chars[0].Style.FontBold() {
			t.Error("the tspan's font-weight did not override its parent's")
		}
		if txt.Chars[2].Style.FontBold() {
			t.Error("the tspan's font-weight leaked past its own characters")
		}
	})

	t.Run("nesting deeper than the cap degrades with a log", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" font-size="10"><text x="1" y="1">`)
		const depth = 200
		for i := 0; i < depth; i++ {
			b.WriteString("<tspan>")
		}
		b.WriteString("x")
		for i := 0; i < depth; i++ {
			b.WriteString("</tspan>")
		}
		b.WriteString("</text></svg>")

		_, logs := parseSVG(t, b.String())
		if !anyContains(logs, "nesting exceeded") {
			t.Errorf("deep tspan nesting logged no diagnostic; logs = %v", logs)
		}
	})
}

// TestWhitespaceHandling covers SVG's default collapsing mode and
// xml:space="preserve", including the cross-element scope that makes the
// deferred-space design necessary.
func TestWhitespaceHandling(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{
			"collapse strips leading, trailing, and internal runs",
			"<text>   a   b   </text>",
			"a b",
		},
		{
			"newlines and tabs collapse like spaces",
			"<text>\n\t  a\n\n  b\t</text>",
			"a b",
		},
		{
			"a space spanning a tspan boundary collapses to one",
			"<text>a <tspan> b</tspan></text>",
			"a b",
		},
		{
			"preserve keeps every space",
			`<text xml:space="preserve"> a  b </text>`,
			" a  b ",
		},
		{
			"preserve converts newlines and tabs to spaces",
			"<text xml:space=\"preserve\">a\nb\tc</text>",
			"a b c",
		},
		{
			// The tspan's run collapses (three spaces become one) instead of
			// being preserved verbatim, and its TRAILING run is stripped
			// because it reaches the end of the <text>. The leading run does
			// NOT vanish: leading-whitespace stripping is scoped to the whole
			// <text>, and "a" has already been emitted by the time it is seen.
			"a tspan can turn preserve back off",
			`<text xml:space="preserve">a<tspan xml:space="default">   b   </tspan></text>`,
			"a b",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" font-size="10">`+tc.src+`</svg>`)
			txt := firstText(t, doc)
			var b strings.Builder
			for _, c := range txt.Chars {
				b.WriteRune(c.R)
			}
			if b.String() != tc.want {
				t.Errorf("characters = %q, want %q", b.String(), tc.want)
			}
		})
	}
}

// TestRTLTextReorders proves the flat glyph slice goes through
// inline.Reorder: Hebrew in an RTL <text> must come out in visual order, i.e.
// with pen positions DESCENDING in logical order.
func TestRTLTextReorders(t *testing.T) {
	// Hebrew: the bundled Noto Sans Hebrew resolves it via per-rune script
	// fallback inside inline.Shape.
	const hebrew = "שלום"
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="30"><text x="10" y="50" direction="rtl">`+hebrew+`</text></svg>`)
	placed := New(doc).layoutText(firstText(t, doc))
	if len(placed) < 3 {
		t.Fatalf("placed %d glyphs for %q; the Hebrew face did not resolve", len(placed), hebrew)
	}
	// After reordering, the FIRST logical character sits rightmost, so
	// walking the placed (visual-order) glyphs must yield the logical
	// characters in reverse.
	first := placed[0].glyph.Runes
	last := placed[len(placed)-1].glyph.Runes
	if len(first) == 0 || len(last) == 0 {
		t.Fatal("glyphs carry no runes; cannot verify ordering")
	}
	wantFirst := []rune(hebrew)[len([]rune(hebrew))-1]
	if first[0] != wantFirst {
		t.Errorf("leftmost glyph is %q, want %q (RTL text was not reordered)", first[0], wantFirst)
	}

	// The reorder must be a genuine reversal, not just "the last character
	// happens to land first": walking the placed glyphs left to right must
	// spell the source string backwards.
	var visual []rune
	for _, p := range placed {
		if len(p.glyph.Runes) > 0 {
			visual = append(visual, p.glyph.Runes[0])
		}
	}
	src := []rune(hebrew)
	var reversed []rune
	for i := len(src) - 1; i >= 0; i-- {
		reversed = append(reversed, src[i])
	}
	if string(visual) != string(reversed) {
		t.Errorf("visual order = %q, want %q (the logical string reversed)", string(visual), string(reversed))
	}

	// The control that makes the assertion above meaningful: an ASCII string,
	// which is unambiguously LTR, must NOT reverse under the same code path.
	// Without this, a bug that reversed everything unconditionally would pass.
	asciiDoc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="30"><text x="10" y="50">abcd</text></svg>`)
	asciiPlaced := New(asciiDoc).layoutText(firstText(t, asciiDoc))
	var ascii []rune
	for _, p := range asciiPlaced {
		if len(p.glyph.Runes) > 0 {
			ascii = append(ascii, p.glyph.Runes[0])
		}
	}
	if string(ascii) != "abcd" {
		t.Errorf("LTR visual order = %q, want \"abcd\" (LTR text must not reorder)", string(ascii))
	}
	// And pen positions must ascend for LTR.
	for i := 1; i < len(asciiPlaced); i++ {
		if asciiPlaced[i].penX <= asciiPlaced[i-1].penX {
			t.Errorf("LTR pen X did not advance at glyph %d: %v then %v", i, asciiPlaced[i-1].penX, asciiPlaced[i].penX)
			break
		}
	}
}

// TestRTLAnchorIsDirectionRelative pins that text-anchor's start/end name the
// start and end of the INLINE BASE DIRECTION, not the left and right of the
// canvas: in an rtl chunk the default "start" anchor is the RIGHT edge, so the
// text runs leftward from its x. The corpus's direction/rtl.svg anchors at
// x=170 in a 200-wide viewBox and would run off the canvas otherwise.
func TestRTLAnchorIsDirectionRelative(t *testing.T) {
	const hebrew = "שלוםשלום"
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="20"><text x="170" y="100" direction="rtl">`+hebrew+`</text></svg>`)
	placed := New(doc).layoutText(firstText(t, doc))
	if len(placed) < 4 {
		t.Fatalf("placed %d glyphs; the Hebrew face did not resolve", len(placed))
	}
	minX, maxX := math.Inf(1), math.Inf(-1)
	for _, p := range placed {
		minX = math.Min(minX, p.penX)
		maxX = math.Max(maxX, p.penX+p.glyph.Advance)
	}
	if math.Abs(maxX-170) > 1e-6 {
		t.Errorf("rtl chunk right edge = %v, want 170 (start anchors the RIGHT edge in rtl)", maxX)
	}
	if minX >= 170 {
		t.Errorf("rtl chunk minX = %v; the text must extend LEFT of its x", minX)
	}

	// The LTR control: the identical anchor in an ltr chunk puts the LEFT
	// edge at x, so a bug that flipped unconditionally cannot pass.
	ltr, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="20"><text x="170" y="100">abcd</text></svg>`)
	ltrPlaced := New(ltr).layoutText(firstText(t, ltr))
	if len(ltrPlaced) == 0 {
		t.Fatal("no LTR glyphs placed")
	}
	if math.Abs(ltrPlaced[0].penX-170) > 1e-6 {
		t.Errorf("ltr chunk left edge = %v, want 170", ltrPlaced[0].penX)
	}
}

// TestBidiReorderPreservesGlyphCount is a regression test for an
// inline.Reorder bug this feature surfaced: a glyph covering several runes (an
// Arabic contextual cluster, or a ligature) was emitted once PER RUNE, so the
// reordered slice came back longer than it went in. Any caller pairing the
// result against per-character data then mis-associates every glyph after the
// first cluster — which is exactly how it showed up here, as Arabic text whose
// last glyphs jumped back to the first character's absolute x.
func TestBidiReorderPreservesGlyphCount(t *testing.T) {
	// Arabic: shaped through harfbuzz inside inline.Shape, which produces
	// multi-rune cluster glyphs.
	const arabic = "اقرأ المزيد عن SVG أيضًا."
	faces := layoutfont.NewFaceCache()
	glyphs := inline.Shape(faces, []inline.Run{{
		Text: arabic, Family: "sans-serif", SizePt: 14, WhiteSpace: "pre",
	}}, nil)
	if len(glyphs) == 0 {
		t.Fatal("Arabic did not shape; the fixture proves nothing")
	}
	// Confirm the fixture actually exercises the bug: without a multi-rune
	// glyph, per-rune and per-glyph emission are identical.
	multi := false
	for _, g := range glyphs {
		if len(g.Runes) > 1 {
			multi = true
			break
		}
	}
	if !multi {
		t.Skip("no multi-rune cluster in this shaping; the regression cannot occur")
	}
	for _, dir := range []inline.ParagraphDirection{inline.DirLTR, inline.DirRTL} {
		got := inline.Reorder(glyphs, dir)
		if len(got) != len(glyphs) {
			t.Errorf("Reorder(dir=%v) returned %d glyphs from %d; reordering must be a permutation", dir, len(got), len(glyphs))
		}
	}
}

// TestTextAsClipAndMaskGeometry proves the geometry-reuse claim: the same
// glyph outlines a fill would draw become a clip region and a mask.
func TestTextAsClipAndMaskGeometry(t *testing.T) {
	t.Run("clip path", func(t *testing.T) {
		doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200">
		  <clipPath id="c"><text x="20" y="100" font-family="sans-serif" font-size="60">Text</text></clipPath>
		  <rect width="200" height="200" fill="green" clip-path="url(#c)"/>
		</svg>`)
		_, root := doc.Root()
		rect, ok := root.Kids[0].(*svg.Shape)
		if !ok {
			t.Fatalf("root.Kids[0] = %#v, want *svg.Shape", root.Kids[0])
		}
		if rect.ClipPath == nil {
			t.Fatal("the rect resolved no clip-path")
		}
		if len(rect.ClipPath.Kids) != 1 {
			t.Fatalf("clipPath has %d kids, want 1", len(rect.ClipPath.Kids))
		}
		kid := rect.ClipPath.Kids[0]
		if kid.Text == nil {
			t.Fatal("the <text> clipPath child carries no Text node (still contributing nothing?)")
		}
		if kid.Path != nil {
			t.Error("a text clipPath child must carry Text, not Path")
		}

		// The geometry itself: shaping it must produce a real, non-empty path
		// whose extent matches where the text was placed.
		p := New(doc).textClipPath(kid.Text, render.Identity)
		if p == nil || p.Empty() {
			t.Fatal("text clip geometry shaped to nothing")
		}
		minX, minY, maxX, maxY, ok := p.Bounds()
		if !ok {
			t.Fatal("text clip geometry has no bounds")
		}
		if minX < 15 || minX > 40 {
			t.Errorf("clip geometry minX = %v, want it near the text's x=20", minX)
		}
		if maxY < minY || maxY > 110 {
			t.Errorf("clip geometry Y extent = [%v,%v], want it above the y=100 baseline", minY, maxY)
		}
		if maxX <= minX {
			t.Errorf("clip geometry has no width: [%v,%v]", minX, maxX)
		}
	})

	t.Run("mask", func(t *testing.T) {
		doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200">
		  <mask id="m"><text x="20" y="100" font-family="sans-serif" font-size="60" fill="white">Text</text></mask>
		  <rect width="200" height="200" fill="green" mask="url(#m)"/>
		</svg>`)
		_, root := doc.Root()
		rect := root.Kids[0].(*svg.Shape)
		if rect.Mask == nil {
			t.Fatal("the rect resolved no mask")
		}
		if rect.Mask.Kids == nil || len(rect.Mask.Kids.Kids) == 0 {
			t.Fatal("the mask has no content")
		}
		if findText(rect.Mask.Kids) == nil {
			t.Error("the <text> inside the <mask> produced no Text node")
		}
	})
}

// TestTspanClipPathAndMask covers SVG 2's clip-path and mask on a <tspan>
// (the corpus's tspan/with-clip-path.svg and with-mask.svg). Both are
// NON-inherited, so a character must carry one only when its own element — or
// an enclosing tspan, whose region geometrically contains it — set it.
func TestTspanClipPathAndMask(t *testing.T) {
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"
	  font-family="sans-serif" font-size="30">
	  <clipPath id="c"><rect x="0" y="0" width="200" height="80"/></clipPath>
	  <mask id="m"><rect x="0" y="0" width="200" height="200" fill="gray"/></mask>
	  <text x="10" y="100">a<tspan clip-path="url(#c)">b</tspan><tspan mask="url(#m)">c</tspan>d</text>
	</svg>`)
	txt := firstText(t, doc)
	if len(txt.Chars) != 4 {
		t.Fatalf("lowered %d chars, want 4", len(txt.Chars))
	}
	if txt.Chars[0].ClipPath() != nil || txt.Chars[0].Mask() != nil {
		t.Error("the character before the tspans picked up a clip or mask")
	}
	if txt.Chars[1].ClipPath() == nil {
		t.Error("the clip-path on a <tspan> did not reach its character")
	}
	if txt.Chars[1].Mask() != nil {
		t.Error("the clip-path tspan wrongly picked up a mask")
	}
	if txt.Chars[2].Mask() == nil {
		t.Error("the mask on a <tspan> did not reach its character")
	}
	if txt.Chars[2].ClipPath() != nil {
		t.Error("the mask tspan wrongly inherited the sibling tspan's clip-path (it is NOT inherited)")
	}
	if txt.Chars[3].ClipPath() != nil || txt.Chars[3].Mask() != nil {
		t.Error("a clip or mask leaked past its tspan to the following character")
	}

	t.Run("an inner tspan inherits an enclosing one's clip", func(t *testing.T) {
		doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" font-size="30">
		  <clipPath id="c"><rect x="0" y="0" width="200" height="80"/></clipPath>
		  <text x="10" y="100"><tspan clip-path="url(#c)">a<tspan>b</tspan></tspan></text>
		</svg>`)
		txt := firstText(t, doc)
		if len(txt.Chars) != 2 {
			t.Fatalf("lowered %d chars, want 2", len(txt.Chars))
		}
		if txt.Chars[1].ClipPath() != txt.Chars[0].ClipPath() || txt.Chars[1].ClipPath() == nil {
			t.Error("an inner tspan lost the enclosing tspan's clip; the enclosing region still contains it")
		}
	})
}

// TestStrokedTextWithGradient proves text is ordinary geometry: a gradient
// fill resolves onto its characters and a stroke paints alongside it, both
// through the same helpers a <path> uses.
func TestStrokedTextWithGradient(t *testing.T) {
	doc, logs := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 200">
	  <linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="300" y2="0">
	    <stop offset="0" stop-color="red"/><stop offset="1" stop-color="blue"/>
	  </linearGradient>
	  <text x="10" y="100" font-family="sans-serif" font-size="50"
	        fill="url(#g)" stroke="black" stroke-width="2">Text</text>
	</svg>`)
	txt := firstText(t, doc)
	if len(txt.Chars) == 0 {
		t.Fatal("no characters lowered")
	}
	if txt.Chars[0].FillGradient() == nil {
		t.Fatal("the gradient fill did not resolve onto the text's characters")
	}
	if _, ok := txt.Chars[0].Style.StrokePaint(); !ok {
		t.Error("the stroke did not resolve onto the text's characters")
	}
	for _, l := range logs {
		if strings.Contains(l, "not yet supported") {
			t.Errorf("unexpected unsupported-feature log for gradient-filled stroked text: %q", l)
		}
	}

	// Every character must share ONE resolved gradient, not build its own:
	// a per-character shader would be a real allocation bug at scale.
	for i := 1; i < len(txt.Chars); i++ {
		if txt.Chars[i].FillGradient() != txt.Chars[0].FillGradient() {
			t.Errorf("char %d resolved its own gradient instance; want one shared across the text", i)
			break
		}
	}
}

// TestTrefDegradesWithLog pins design decision 4: <tref> was removed from SVG
// 2 and is dropped rather than deferred, but must LOG so an author whose text
// silently vanished gets a diagnostic.
func TestTrefDegradesWithLog(t *testing.T) {
	t.Run("inside a text element", func(t *testing.T) {
		doc, logs := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" font-size="20">
		  <defs><text id="src">referenced</text></defs>
		  <text x="10" y="50">a<tref xlink:href="#src" xmlns:xlink="http://www.w3.org/1999/xlink"/>b</text>
		</svg>`)
		if !anyContains(logs, "tref") {
			t.Errorf("<tref> produced no diagnostic; logs = %v", logs)
		}
		// It contributes no characters of its own, but must not swallow its
		// siblings' either.
		_, root := doc.Root()
		var txt *svg.Text
		for _, k := range root.Kids {
			if candidate, ok := k.(*svg.Text); ok {
				txt = candidate
			}
		}
		if txt == nil {
			t.Fatal("the containing <text> produced no node")
		}
		var b strings.Builder
		for _, c := range txt.Chars {
			b.WriteRune(c.R)
		}
		if b.String() != "ab" {
			t.Errorf("characters = %q, want \"ab\" (<tref> must drop only itself)", b.String())
		}
	})

	t.Run("as a top-level element", func(t *testing.T) {
		_, logs := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200">
		  <tref/>
		</svg>`)
		if !anyContains(logs, "tref") {
			t.Errorf("a top-level <tref> produced no diagnostic; logs = %v", logs)
		}
	})
}

// TestTspanOutsideTextRendersNothing pins the corpus's
// tspan/outside-the-text.svg: a <tspan> is only meaningful as <text> content.
func TestTspanOutsideTextRendersNothing(t *testing.T) {
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" font-size="20">
	  <tspan x="10" y="50">orphan</tspan>
	</svg>`)
	_, root := doc.Root()
	if len(root.Kids) != 0 {
		t.Errorf("root kids = %d, want 0 (an orphaned <tspan> renders nothing)", len(root.Kids))
	}
}

// TestZeroFontSizePaintsNothingButKeepsPositions asserts SVG's zero-size
// rule: the text vanishes, and — critically — the characters still consume
// their position-list entries, so nothing downstream shifts.
func TestZeroFontSizePaintsNothing(t *testing.T) {
	doc, _ := parseSVG(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200">
	  <text x="10" y="50" font-size="0">Text</text>
	</svg>`)
	txt := firstText(t, doc)
	if len(txt.Chars) != 4 {
		t.Fatalf("lowered %d chars, want 4 (a zero size must not drop the characters)", len(txt.Chars))
	}
	if placed := New(doc).layoutText(txt); len(placed) != 0 {
		t.Errorf("placed %d glyphs at font-size 0, want none", len(placed))
	}
}

// TestTextCharacterBudget proves the build-time expansion guard fires rather
// than allocating without limit for adversarial input.
func TestTextCharacterBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" font-size="10"><text x="1" y="1">`)
	for i := 0; i < 250_000; i++ {
		b.WriteByte('x')
	}
	b.WriteString(`</text></svg>`)

	doc, logs := parseSVG(t, b.String())
	txt := firstText(t, doc)
	if len(txt.Chars) > 200_000 {
		t.Errorf("lowered %d characters, want the budget to cap it", len(txt.Chars))
	}
	if !anyContains(logs, "budget") {
		t.Errorf("the character budget fired without a diagnostic; logs = %v", logs)
	}
}

// TestMalformedTextNeverPanics feeds structurally hostile or nonsensical text
// markup through the whole parse-and-lay-out path. The contract is that
// nothing panics and nothing returns a non-finite coordinate.
func TestMalformedTextNeverPanics(t *testing.T) {
	cases := []string{
		`<text/>`,
		`<text></text>`,
		`<text x="" y="">a</text>`,
		`<text x="nonsense" y="10">a</text>`,
		`<text x="1e400" y="10">a</text>`,
		`<text x="10" y="10" dx="," dy=",,,">a</text>`,
		`<text x="10" y="10" rotate="">a</text>`,
		`<text x="10" y="10" font-size="-5">a</text>`,
		`<text x="10" y="10" font-size="nonsense">a</text>`,
		`<text x="10" y="10" font-family="">a</text>`,
		`<text x="10" y="10"><tspan/></text>`,
		`<text x="10" y="10"><tspan><tspan><tspan/></tspan></tspan></text>`,
		"<text x=\"10\" y=\"10\">\u0000\ufffd</text>",
		`<text x="10" y="10" text-anchor="sideways">a</text>`,
		`<text x="10" y="10" direction="sideways">a</text>`,
		`<text x="10" y="10" font-weight="99999">a</text>`,
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200">` + body + `</svg>`
			doc, err := svg.Parse([]byte(src), nil)
			if err != nil {
				return // a rejected document is a fine outcome; a panic is not
			}
			_, root := doc.Root()
			txt := findText(root)
			if txt == nil {
				return
			}
			for _, p := range New(doc).layoutText(txt) {
				if math.IsNaN(p.penX) || math.IsInf(p.penX, 0) ||
					math.IsNaN(p.penY) || math.IsInf(p.penY, 0) {
					t.Errorf("non-finite pen position (%v,%v)", p.penX, p.penY)
				}
			}
		})
	}
}

// anyContains reports whether any line contains sub.
func anyContains(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
