package css

import (
	"fmt"
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/omnidoc/pkg/css"
	"github.com/nathanstitt/omnidoc/pkg/layout"
	"github.com/nathanstitt/omnidoc/pkg/layout/cssbox"
)

// shadowsOf walks a fragment tree and returns every fragment's resolved shadow
// list concatenated, so a test can assert on what the cascade + resolver
// produced without knowing which fragment ended up carrying it.
func shadowsOf(f *Fragment) []ShadowSpec {
	var out []ShadowSpec
	var walk func(*Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		out = append(out, f.Shadows...)
		for _, c := range f.Children {
			walk(c)
		}
		for _, c := range f.Floats {
			walk(c)
		}
		for _, c := range f.Positioned {
			walk(c)
		}
	}
	walk(f)
	return out
}

// TestBoxShadowResolvesOntoTheFragment: the declaration reaches the fragment as
// resolved points, with em scaled by the box's own font size and the colour
// defaulted to the box's `color`.
func TestBoxShadowResolvesOntoTheFragment(t *testing.T) {
	for _, tc := range []struct {
		name  string
		style string
		want  []ShadowSpec
	}{
		{"plain offsets", "box-shadow:2px 3px",
			[]ShadowSpec{{OffsetX: 2, OffsetY: 3, Color: color.RGBA{A: 255}}}},
		{"blur and spread", "box-shadow:2px 3px 4px 5px red",
			[]ShadowSpec{{OffsetX: 2, OffsetY: 3, Blur: 4, Spread: 5, Color: color.RGBA{255, 0, 0, 255}}}},
		{"negative spread survives", "box-shadow:0 0 0 -3px red",
			[]ShadowSpec{{Spread: -3, Color: color.RGBA{255, 0, 0, 255}}}},
		{"inset flagged", "box-shadow:inset 3px 0 0 red",
			[]ShadowSpec{{OffsetX: 3, Color: color.RGBA{255, 0, 0, 255}, Inset: true}}},
		{"em resolves against the box font size", "font-size:10px;box-shadow:2em 0",
			[]ShadowSpec{{OffsetX: 20, Color: color.RGBA{A: 255}}}},
		{"omitted colour is currentColor", "color:#00ff00;box-shadow:1px 1px",
			[]ShadowSpec{{OffsetX: 1, OffsetY: 1, Color: color.RGBA{0, 255, 0, 255}}}},
		{"currentColor keyword is the same", "color:#00ff00;box-shadow:1px 1px currentColor",
			[]ShadowSpec{{OffsetX: 1, OffsetY: 1, Color: color.RGBA{0, 255, 0, 255}}}},
		{"an explicit colour wins over color", "color:#00ff00;box-shadow:1px 1px red",
			[]ShadowSpec{{OffsetX: 1, OffsetY: 1, Color: color.RGBA{255, 0, 0, 255}}}},
		{"none leaves nothing", "box-shadow:none", nil},
		{"an invalid value leaves nothing", "box-shadow:2px", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := fmt.Sprintf(`<html><body style="margin:0">`+
				`<div style="height:20px;%s">x</div></body></html>`, tc.style)
			got := shadowsOf(layoutTreeFor(t, src, 200, nil))
			if len(got) != len(tc.want) {
				t.Fatalf("got %d shadows, want %d (%+v)", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("shadow %d =\n got %+v\nwant %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestBoxShadowDropsFullyTransparentShadows: a shadow that cannot paint a pixel
// is discarded at resolve time, so it never costs an offscreen surface or a
// blur pass downstream.
//
// It is driven through the box tree rather than through HTML because no colour
// syntax this cascade parses today can express alpha — `rgba()` and `#rrggbbaa`
// are a separate, tracked gap — so an authored transparent shadow cannot yet be
// written. The drop is nevertheless the correct behaviour and is what an alpha
// colour syntax will land on, so it is pinned here rather than left untested.
func TestBoxShadowDropsFullyTransparentShadows(t *testing.T) {
	b := &cssbox.Box{}
	b.Style = gcss.InitialStyle()
	b.Style.BoxShadow = []gcss.BoxShadow{
		{OffsetY: gcss.Length{Value: 8, Unit: gcss.UnitPx}, Color: color.RGBA{}, HasColor: true},
		{OffsetY: gcss.Length{Value: 9, Unit: gcss.UnitPx}, Color: color.RGBA{A: 255}, HasColor: true},
	}
	e := &Engine{}
	got := e.boxShadows(b)
	if len(got) != 1 || got[0].OffsetY != 9 {
		t.Errorf("got %+v, want only the opaque shadow (the transparent one dropped)", got)
	}
}

// TestBoxShadowNotInherited: a shadow on a parent must not reach its children —
// inheriting it would draw the same decoration at every level of the tree.
func TestBoxShadowNotInherited(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<div style="height:40px;box-shadow:2px 2px red">` +
		`<div style="height:20px">x</div></div></body></html>`
	if got := shadowsOf(layoutTreeFor(t, src, 200, nil)); len(got) != 1 {
		t.Errorf("%d fragments carry a shadow, want exactly 1 (box-shadow must not inherit): %+v", len(got), got)
	}
}

// TestBoxShadowPaintOrder pins CSS Backgrounds 3 §6's placement of the two
// shadow kinds in the box's own decoration sequence:
//
//	outer shadows → background → INSET shadows → border
//
// An outer shadow behind the background (so an opaque box hides the part under
// itself) and an inset shadow above it but below the border (so the border is
// never darkened by the box's own inner shadow) is the whole point of walking
// the list twice; emitting both at one point would get one of them wrong.
func TestBoxShadowPaintOrder(t *testing.T) {
	f := &Fragment{
		X: 0, Y: 0, W: 100, H: 50,
		Background: color.RGBA{1, 1, 1, 255},
		Shadows: []ShadowSpec{
			{OffsetX: 1, Color: color.RGBA{A: 255}},
			{OffsetX: 2, Color: color.RGBA{A: 255}, Inset: true},
		},
	}
	f.Border[layout.EdgeTop] = BorderEdge{Width: 2, Style: layout.BorderSolid, Color: color.RGBA{A: 255}}

	var kinds []layout.ItemKind
	for _, it := range f.AppendItems(nil) {
		kinds = append(kinds, it.Kind)
	}
	want := []layout.ItemKind{
		layout.ShadowKind,     // the outer shadow, behind everything
		layout.BackgroundKind, //
		layout.ShadowKind,     // the inset shadow, over the background
		layout.BorderKind,     // the border, over the inset shadow
	}
	if len(kinds) != len(want) {
		t.Fatalf("got %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("item order = %v, want %v", kinds, want)
		}
	}
	// And the two shadows are the ones expected, not the same one twice.
	items := f.AppendItems(nil)
	if items[0].Shadow.Inset || items[0].Shadow.OffsetX != 1 {
		t.Errorf("item[0] = %+v, want the OUTER shadow", items[0].Shadow)
	}
	if !items[2].Shadow.Inset || items[2].Shadow.OffsetX != 2 {
		t.Errorf("item[2] = %+v, want the INSET shadow", items[2].Shadow)
	}
}

// TestBoxShadowListPaintsFirstOnTop pins the spec's "the first shadow is on
// top". The item stream paints front-to-back last, so the source list must be
// emitted REVERSED — and reversing it in the parser instead would double-flip.
func TestBoxShadowListPaintsFirstOnTop(t *testing.T) {
	f := &Fragment{
		X: 0, Y: 0, W: 100, H: 50,
		Shadows: []ShadowSpec{
			{OffsetX: 1, Color: color.RGBA{A: 255}}, // first in source: paints LAST
			{OffsetX: 2, Color: color.RGBA{A: 255}},
			{OffsetX: 3, Color: color.RGBA{A: 255}},
		},
	}
	items := f.AppendItems(nil)
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 shadows", len(items))
	}
	for i, wantX := range []float64{3, 2, 1} {
		if items[i].Kind != layout.ShadowKind || items[i].Shadow.OffsetX != wantX {
			t.Errorf("item[%d] offsetX = %v, want %v (source order reversed)", i, items[i].Shadow.OffsetX, wantX)
		}
	}
}

// TestBoxShadowBoxIsBorderOrPadding: an OUTER shadow takes the border box's
// shape, an INSET one the padding box's. This is where "inset is not a sign
// flip" first shows up — the two shadows of the same box start from different
// rectangles.
func TestBoxShadowBoxIsBorderOrPadding(t *testing.T) {
	f := &Fragment{
		X: 10, Y: 20, W: 100, H: 50,
		Shadows: []ShadowSpec{
			{Color: color.RGBA{A: 255}},
			{Color: color.RGBA{A: 255}, Inset: true},
		},
	}
	f.Border[layout.EdgeTop] = BorderEdge{Width: 3, Style: layout.BorderSolid}
	f.Border[layout.EdgeLeft] = BorderEdge{Width: 4, Style: layout.BorderSolid}
	f.Border[layout.EdgeBottom] = BorderEdge{Width: 5, Style: layout.BorderSolid}
	f.Border[layout.EdgeRight] = BorderEdge{Width: 6, Style: layout.BorderSolid}

	items := f.AppendItems(nil)
	outer, inset := items[0].Shadow, items[len(items)-1].Shadow
	for _, s := range items {
		if s.Kind == layout.ShadowKind && s.Shadow.Inset {
			inset = s.Shadow
		}
	}
	if outer.XPt != 10 || outer.YPt != 20 || outer.WPt != 100 || outer.HPt != 50 {
		t.Errorf("outer shadow box = (%v,%v,%v,%v), want the BORDER box (10,20,100,50)",
			outer.XPt, outer.YPt, outer.WPt, outer.HPt)
	}
	// The padding box: the border box deflated by 4/3/6/5 (L/T/R/B).
	if inset.XPt != 14 || inset.YPt != 23 || inset.WPt != 90 || inset.HPt != 42 {
		t.Errorf("inset shadow box = (%v,%v,%v,%v), want the PADDING box (14,23,90,42)",
			inset.XPt, inset.YPt, inset.WPt, inset.HPt)
	}
}

// TestBoxShadowRidesARelativeOffset: a relatively-positioned box's paint-time
// offset must move its shadow with it. translateItems is the single place that
// happens, and a kind it does not handle is left behind at the un-offset
// position — visible only as a shadow detached from its box.
func TestBoxShadowRidesARelativeOffset(t *testing.T) {
	items := []layout.Item{{
		Kind:   layout.ShadowKind,
		Shadow: layout.ShadowItem{XPt: 10, YPt: 20, WPt: 30, HPt: 40, OffsetX: 2, Spread: 3},
	}}
	translateItems(items, 0, 7, 8)
	got := items[0].Shadow
	if got.XPt != 17 || got.YPt != 28 {
		t.Errorf("shadow box after translate = (%v,%v), want (17,28)", got.XPt, got.YPt)
	}
	// The shadow's own parameters are relative to its box and must NOT move.
	if got.OffsetX != 2 || got.Spread != 3 || got.WPt != 30 || got.HPt != 40 {
		t.Errorf("translate disturbed the shadow's parameters: %+v", got)
	}
}

// TestBoxShadowlessFragmentEmitsNothingNew is the byte-identity guard: a
// fragment with no shadow must produce exactly the item stream it produced
// before the property existed.
func TestBoxShadowlessFragmentEmitsNothingNew(t *testing.T) {
	f := &Fragment{X: 0, Y: 0, W: 100, H: 50, Background: color.RGBA{1, 1, 1, 255}}
	items := f.AppendItems(nil)
	if len(items) != 1 || items[0].Kind != layout.BackgroundKind {
		t.Errorf("got %d items %v, want exactly one BackgroundKind", len(items), items)
	}
}
