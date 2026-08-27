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

	// currentColor follows color; none kills fill; url() degrades to none + log.
	var logged bool
	s = applyAttrs(base, map[string]string{"color": "teal", "fill": "currentColor"})
	if s.fillRGBA() != (color.RGBA{0, 128, 128, 255}) {
		t.Errorf("currentColor = %+v", s.fillRGBA())
	}
	s = applyAttrs(base, map[string]string{"fill": "none"})
	if s.hasFill {
		t.Error("fill none kept")
	}
	s = base.apply(&element{attrs: map[string]string{"fill": "url(#g)"}},
		&cascadeCtx{logf: func(string, ...any) { logged = true }})
	if s.hasFill || !logged {
		t.Errorf("url() fill: hasFill=%v logged=%v", s.hasFill, logged)
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
