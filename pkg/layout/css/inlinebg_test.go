package css

import (
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// backgroundItems returns the BackgroundKind items in the flattened item stream.
func backgroundItems(items []layout.Item) []layout.Item {
	var out []layout.Item
	for _, it := range items {
		if it.Kind == layout.BackgroundKind {
			out = append(out, it)
		}
	}
	return out
}

// An inline <span> with a background paints one BackgroundKind rect behind its text.
// Before this, a non-replaced inline box painted nothing at all: the property parsed
// and cascaded, and the paint step simply had no geometry to draw with, so the common
// highlight idiom was silently dropped.
func TestInlineSpanBackgroundEmitsRect(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p><span style="background-color:#ffe08a">highlighted</span></p></body>`,
		400, resource.MapLoader{}, nil)
	bgs := backgroundItems(root.AppendItems(nil))
	if len(bgs) != 1 {
		t.Fatalf("got %d BackgroundKind items, want 1", len(bgs))
	}
	r := bgs[0].Rule
	if r.WPt <= 0 || r.HPt <= 0 {
		t.Errorf("background rect is empty: %+v", r)
	}
	if want := (color.RGBA{R: 0xff, G: 0xe0, B: 0x8a, A: 0xff}); r.Color != want {
		t.Errorf("background color = %v, want %v", r.Color, want)
	}
}

// Falsifiability control: an undecorated span emits no background, so the assertions
// above are testing the feature rather than a blanket rect behind every line.
func TestNoInlineBackgroundWithoutDeclaration(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p><span>plain</span></p></body>`,
		400, resource.MapLoader{}, nil)
	if bgs := backgroundItems(root.AppendItems(nil)); len(bgs) != 0 {
		t.Errorf("got %d BackgroundKind items for undecorated text, want 0", len(bgs))
	}
}

// Two ADJACENT spans with different backgrounds stay two rects. This is why the
// coalescing keys on inline-box identity (pointer equality) rather than on the color:
// a color comparison would merge same-colored neighbours, and a bare bool — the way
// underline does it — could not tell them apart at all.
func TestAdjacentInlineSpansPaintSeparateRects(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p><span style="background-color:#ffe08a">one</span><span style="background-color:#9ad0f5">two</span></p></body>`,
		400, resource.MapLoader{}, nil)
	bgs := backgroundItems(root.AppendItems(nil))
	if len(bgs) != 2 {
		t.Fatalf("got %d BackgroundKind items, want 2 (one per span)", len(bgs))
	}
	if bgs[0].Rule.Color == bgs[1].Rule.Color {
		t.Error("the two rects share a color; the spans were coalesced")
	}
}

// Two adjacent spans with the SAME background are still two boxes. A color-keyed
// implementation would merge them into one rect; identity keeps them apart.
func TestAdjacentSameColorSpansStaySeparate(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p><span style="background-color:#ffe08a">one</span><span style="background-color:#ffe08a">two</span></p></body>`,
		400, resource.MapLoader{}, nil)
	if bgs := backgroundItems(root.AppendItems(nil)); len(bgs) != 2 {
		t.Fatalf("got %d BackgroundKind items, want 2 (identity, not color, separates them)", len(bgs))
	}
}

// A span crossing a line break paints ONE RECT PER LINE. Coalescing per LineFragment
// is what makes this fall out with no explicit fragmentation bookkeeping — the same
// property that makes underline work across a wrap.
func TestInlineBackgroundSpansLineBreak(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p style="width:80px"><span style="background-color:#ffe08a">wrap me across lines</span></p></body>`,
		400, resource.MapLoader{}, nil)
	bgs := backgroundItems(root.AppendItems(nil))
	if len(bgs) < 2 {
		t.Fatalf("got %d BackgroundKind items, want >= 2 (one per line the span occupies)", len(bgs))
	}
	// Each line's rect must sit at a distinct Y — proof they are per-line fragments
	// rather than one rect emitted repeatedly.
	if bgs[0].Rule.YPt == bgs[1].Rule.YPt {
		t.Errorf("the first two rects share Y=%v; they are not per-line fragments", bgs[0].Rule.YPt)
	}
}

// The background stays CONTINUOUS across the spaces inside a span. Inkless glyphs are
// dropped from the paint stream, so without retaining them for the decoration pass a
// two-word span would paint as two rects with a hole between them.
func TestInlineBackgroundContinuousAcrossSpaces(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p><span style="background-color:#ffe08a">two words</span></p></body>`,
		400, resource.MapLoader{}, nil)
	if bgs := backgroundItems(root.AppendItems(nil)); len(bgs) != 1 {
		t.Fatalf("got %d BackgroundKind items, want 1 continuous rect across the space", len(bgs))
	}
}

// An undecorated inline INSIDE a decorated one is part of its parent's box, not a new
// one, so the parent's rect stays continuous through it.
func TestNestedUndecoratedInlineDoesNotSplitRect(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p><span style="background-color:#ffe08a">a <em>b</em> c</span></p></body>`,
		400, resource.MapLoader{}, nil)
	if bgs := backgroundItems(root.AppendItems(nil)); len(bgs) != 1 {
		t.Fatalf("got %d BackgroundKind items, want 1 (the nested <em> must not split the span)", len(bgs))
	}
}

// The rect is sized to the CONTENT AREA — the tallest ascent and deepest descent among
// its glyphs — so a span mixing font sizes gets a rect tall enough for its largest.
// Per-glyph metrics are what make this work; a per-line height would over-cover and a
// first-glyph height would under-cover.
func TestInlineBackgroundHeightFollowsTallestGlyph(t *testing.T) {
	small := layoutWithLoader(t,
		`<body><p style="font-size:12px"><span style="background-color:#ffe08a">ab</span></p></body>`,
		400, resource.MapLoader{}, nil)
	mixed := layoutWithLoader(t,
		`<body><p style="font-size:12px"><span style="background-color:#ffe08a">a<span style="font-size:40px">B</span></span></p></body>`,
		400, resource.MapLoader{}, nil)
	sm := backgroundItems(small.AppendItems(nil))
	mx := backgroundItems(mixed.AppendItems(nil))
	if len(sm) == 0 || len(mx) == 0 {
		t.Fatal("missing background rects")
	}
	if mx[0].Rule.HPt <= sm[0].Rule.HPt {
		t.Errorf("mixed-size rect height %v is not taller than the uniform one %v", mx[0].Rule.HPt, sm[0].Rule.HPt)
	}
}

// Inline PADDING is part of layout, not just paint: it widens the box's advance so the
// text after a padded span starts past the padding rather than under it. Painting a
// padded rect without reserving the space would draw a background wider than the layout
// agreed to — which is why the padding rides on a real edge glyph.
func TestInlinePaddingReservesSpaceInLayout(t *testing.T) {
	plain := layoutWithLoader(t,
		`<body><p><span style="background-color:#ffe08a">x</span>|</p></body>`,
		400, resource.MapLoader{}, nil)
	padded := layoutWithLoader(t,
		`<body><p><span style="background-color:#ffe08a;padding:0 10px">x</span>|</p></body>`,
		400, resource.MapLoader{}, nil)

	// The trailing "|" must be pushed right by the span's left+right padding.
	lastGlyphX := func(f *Fragment) float64 {
		items := f.AppendItems(nil)
		x := 0.0
		for _, it := range items {
			if it.Kind == layout.GlyphKind && it.Glyph.XPt > x {
				x = it.Glyph.XPt
			}
		}
		return x
	}
	p0, p1 := lastGlyphX(plain), lastGlyphX(padded)
	if p1 <= p0 {
		t.Errorf("padded run's trailing glyph X = %v, want > the unpadded %v (padding did not reserve space)", p1, p0)
	}
	// And the painted rect must be wider by the same padding, so paint and layout agree.
	b0 := backgroundItems(plain.AppendItems(nil))
	b1 := backgroundItems(padded.AppendItems(nil))
	if len(b0) == 0 || len(b1) == 0 {
		t.Fatal("missing background rects")
	}
	if b1[0].Rule.WPt <= b0[0].Rule.WPt {
		t.Errorf("padded rect width %v is not wider than unpadded %v", b1[0].Rule.WPt, b0[0].Rule.WPt)
	}
}

// A uniform solid border on an inline box paints four edge strips around its rect.
func TestInlineBorderEmitsRules(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p><span style="border:2px solid #c0392b">boxed</span></p></body>`,
		400, resource.MapLoader{}, nil)
	var rules int
	for _, it := range root.AppendItems(nil) {
		if it.Kind == layout.RuleKind && it.Rule.Color == (color.RGBA{R: 0xc0, G: 0x39, B: 0x2b, A: 0xff}) {
			rules++
		}
	}
	if rules != 4 {
		t.Errorf("got %d border rules for an inline border, want 4 (one per edge)", rules)
	}
}

// A transparent background declaration paints nothing, so `background-color:transparent`
// does not start emitting rects behind every span that names it.
func TestTransparentInlineBackgroundPaintsNothing(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p><span style="background-color:transparent">plain</span></p></body>`,
		400, resource.MapLoader{}, nil)
	if bgs := backgroundItems(root.AppendItems(nil)); len(bgs) != 0 {
		t.Errorf("got %d BackgroundKind items for a transparent background, want 0", len(bgs))
	}
}
