package svg

import (
	"image/color"
	"reflect"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// applyAttrs applies attrs to base with no stylesheet context (presentation
// attributes only), the shape every pre-cascade test used.
func applyAttrs(base Style, attrs map[string]string) Style {
	return base.apply(&element{attrs: attrs}, nil)
}

func TestStyleApply(t *testing.T) {
	base := defaultStyle()

	s := applyAttrs(base, map[string]string{"fill": "red", "stroke": "#00f", "stroke-width": "2"})
	if !s.hasFill || s.fillRGBA() != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("fill = %+v", s.fillRGBA())
	}
	if !s.hasStroke || s.strokeRGBA() != (color.RGBA{0, 0, 255, 255}) || s.strokeWidth != 2 {
		t.Errorf("stroke = %+v w=%g", s.strokeRGBA(), s.strokeWidth)
	}

	// Inheritance: child without attrs keeps parent paint; opacity does NOT inherit.
	parent := applyAttrs(base, map[string]string{"fill": "green", "opacity": "0.5"})
	child := parent.apply(&element{}, nil)
	if child.fillRGBA() != (color.RGBA{0, 128, 0, 255}) {
		t.Errorf("inherited fill = %+v", child.fillRGBA())
	}
	if parent.opacity != 0.5 || child.opacity != 1 {
		t.Errorf("opacity parent=%g child=%g", parent.opacity, child.opacity)
	}

	// fill-opacity multiplies down the chain: 0.5 * 0.5 = 0.25 -> A=64.
	p := applyAttrs(base, map[string]string{"fill-opacity": "0.5"})
	c := p.apply(&element{attrs: map[string]string{"fill-opacity": "0.5"}}, nil)
	// SVG fill-opacity is NOT multiplicative on inherit — the child's own value
	// REPLACES the inherited one (it's an inherited property, not a compositing
	// product). A=128 for both.
	if p.fillRGBA().A != 128 || c.fillRGBA().A != 128 {
		t.Errorf("fill-opacity p=%d c=%d, want 128,128", p.fillRGBA().A, c.fillRGBA().A)
	}

	// currentColor follows color; none kills fill.
	s = applyAttrs(base, map[string]string{"color": "teal", "fill": "currentColor"})
	if s.fillRGBA() != (color.RGBA{0, 128, 128, 255}) {
		t.Errorf("currentColor = %+v", s.fillRGBA())
	}
	s = applyAttrs(base, map[string]string{"fill": "none"})
	if s.hasFill {
		t.Error("fill none kept")
	}

	// Bad value ignored (parent kept), dasharray parsed, evenodd mapped.
	s = applyAttrs(base, map[string]string{"fill": "notacolor", "fill-rule": "evenodd",
		"stroke-dasharray": "4 2", "visibility": "hidden"})
	if s.fillRGBA() != (color.RGBA{0, 0, 0, 255}) || s.fillRule != render.EvenOdd {
		t.Errorf("bad fill / rule: %+v %v", s.fillRGBA(), s.fillRule)
	}
	if !reflect.DeepEqual(s.dashes, []float64{4, 2}) || s.visible {
		t.Errorf("dash/vis = %v %v", s.dashes, s.visible)
	}
}

// TestStylePaintServer covers fill/stroke="url(#id)" reference retention:
// the bare form, the fallback-color form, a malformed (unterminated) url(),
// and inheritance of the reference to a child with no fill of its own.
func TestStylePaintServer(t *testing.T) {
	base := defaultStyle()

	// Bare reference: id recorded, no fallback color applied, and FillPaint
	// reports ok=false (a still-unresolved url() with no fallback paints
	// nothing, not the inherited solid fill — see applyPaint).
	s := applyAttrs(base, map[string]string{"fill": "url(#g)"})
	id, ok := s.FillServer()
	if !ok || id != "#g" {
		t.Errorf("FillServer = %q, %v; want #g, true", id, ok)
	}
	if _, ok := s.FillPaint(); ok {
		t.Error("FillPaint reported ok=true for a url() reference with no fallback color")
	}

	// Reference + fallback color: both recorded.
	s = applyAttrs(base, map[string]string{"fill": "url(#g) red"})
	id, ok = s.FillServer()
	if !ok || id != "#g" {
		t.Errorf("FillServer = %q, %v; want #g, true", id, ok)
	}
	if !s.hasFill || s.fillRGBA() != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("fallback fill = %+v hasFill=%v", s.fillRGBA(), s.hasFill)
	}

	// Same for stroke.
	s = applyAttrs(base, map[string]string{"stroke": "url(#s) blue"})
	id, ok = s.StrokeServer()
	if !ok || id != "#s" {
		t.Errorf("StrokeServer = %q, %v; want #s, true", id, ok)
	}
	if !s.hasStroke || s.strokeRGBA() != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("fallback stroke = %+v hasStroke=%v", s.strokeRGBA(), s.hasStroke)
	}

	// Malformed (unterminated) url(): degrades safely, no panic, no reference recorded.
	var logged bool
	s = base.apply(&element{attrs: map[string]string{"fill": "url(#g"}},
		&cascadeCtx{logf: func(string, ...any) { logged = true }})
	if _, ok := s.FillServer(); ok {
		t.Error("malformed url() recorded a server reference")
	}
	if !logged {
		t.Error("malformed url() did not log")
	}

	// Inheritance: a child with no fill of its own keeps the parent's reference.
	parent := applyAttrs(base, map[string]string{"fill": "url(#grad1)"})
	child := parent.apply(&element{}, nil)
	id, ok = child.FillServer()
	if !ok || id != "#grad1" {
		t.Errorf("inherited FillServer = %q, %v; want #grad1, true", id, ok)
	}
}

// TestStylePaintServerNoFallbackDoesNotLeakInheritedFill regression-tests a
// bug where a still-unresolved url() reference with no fallback color (e.g.
// an unknown id, or a <pattern> with zero usable children) painted the
// INHERITED solid fill/stroke instead of nothing: applyPaint's url() branch
// used to leave *has untouched on a no-fallback reference, so a child
// inheriting the default black fill (or any ancestor's explicit color) would
// paint that color even though FillServer/StrokeServer correctly reported a
// server reference in play. FillPaint/StrokePaint's own doc comments already
// specified ok=false for this case; the fix makes the code match them.
func TestStylePaintServerNoFallbackDoesNotLeakInheritedFill(t *testing.T) {
	base := defaultStyle() // hasFill=true, black; hasStroke=false

	// fill: default style already has hasFill=true (black) — a bare url()
	// reference must override that to ok=false, not inherit it forward.
	s := applyAttrs(base, map[string]string{"fill": "url(#p)"})
	if _, ok := s.FillPaint(); ok {
		t.Error("FillPaint reported ok=true for an unresolved url() fill with no fallback (leaked the inherited black fill)")
	}

	// stroke: give the parent an explicit stroke color first, then override
	// with a bare url() reference — must still clear ok, not keep the
	// parent's stroke color as an implicit fallback.
	withStroke := applyAttrs(base, map[string]string{"stroke": "green"})
	s = applyAttrs(withStroke, map[string]string{"stroke": "url(#p)"})
	if _, ok := s.StrokePaint(); ok {
		t.Error("StrokePaint reported ok=true for an unresolved url() stroke with no fallback (leaked the parent's green stroke)")
	}

	// An unparseable fallback color is a whole-value CSS error: the property
	// keeps its PRIOR value (here, the parent's explicit red fill), not "no
	// paint" and not the url()'s reference either.
	withFill := applyAttrs(base, map[string]string{"fill": "red"})
	s = applyAttrs(withFill, map[string]string{"fill": "url(#p) not-a-color"})
	if _, ok := s.FillServer(); ok {
		t.Error("FillServer recorded a reference despite an unparseable fallback (whole value should be ignored)")
	}
	fp, ok := s.FillPaint()
	if !ok || fp.Color.R != 255 || fp.Color.G != 0 || fp.Color.B != 0 {
		t.Errorf("FillPaint = %+v, %v; want the prior red fill preserved (unparseable fallback ignores the whole value)", fp, ok)
	}
}
