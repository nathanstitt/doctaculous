package svg

import (
	"image/color"
	"reflect"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

func TestStyleApply(t *testing.T) {
	el := func(attrs map[string]string) *element { return &element{attrs: attrs} }
	base := defaultStyle()

	s := base.apply(el(map[string]string{"fill": "red", "stroke": "#00f", "stroke-width": "2"}), nil)
	if !s.hasFill || s.fillRGBA() != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("fill = %+v", s.fillRGBA())
	}
	if !s.hasStroke || s.strokeRGBA() != (color.RGBA{0, 0, 255, 255}) || s.strokeWidth != 2 {
		t.Errorf("stroke = %+v w=%g", s.strokeRGBA(), s.strokeWidth)
	}

	// Inheritance: child without attrs keeps parent paint; opacity does NOT inherit.
	parent := base.apply(el(map[string]string{"fill": "green", "opacity": "0.5"}), nil)
	child := parent.apply(el(nil), nil)
	if child.fillRGBA() != (color.RGBA{0, 128, 0, 255}) {
		t.Errorf("inherited fill = %+v", child.fillRGBA())
	}
	if parent.opacity != 0.5 || child.opacity != 1 {
		t.Errorf("opacity parent=%g child=%g", parent.opacity, child.opacity)
	}

	// fill-opacity multiplies down the chain: 0.5 * 0.5 = 0.25 -> A=64.
	p := base.apply(el(map[string]string{"fill-opacity": "0.5"}), nil)
	c := p.apply(el(map[string]string{"fill-opacity": "0.5"}), nil)
	// SVG fill-opacity is NOT multiplicative on inherit — the child's own value
	// REPLACES the inherited one (it's an inherited property, not a compositing
	// product). A=128 for both.
	if p.fillRGBA().A != 128 || c.fillRGBA().A != 128 {
		t.Errorf("fill-opacity p=%d c=%d, want 128,128", p.fillRGBA().A, c.fillRGBA().A)
	}

	// currentColor follows color; none kills fill; url() degrades to none + log.
	var logged bool
	s = base.apply(el(map[string]string{"color": "teal", "fill": "currentColor"}), nil)
	if s.fillRGBA() != (color.RGBA{0, 128, 128, 255}) {
		t.Errorf("currentColor = %+v", s.fillRGBA())
	}
	s = base.apply(el(map[string]string{"fill": "none"}), nil)
	if s.hasFill {
		t.Error("fill none kept")
	}
	s = base.apply(el(map[string]string{"fill": "url(#g)"}), func(string, ...any) { logged = true })
	if s.hasFill || !logged {
		t.Errorf("url() fill: hasFill=%v logged=%v", s.hasFill, logged)
	}

	// Bad value ignored (parent kept), dasharray parsed, evenodd mapped.
	s = base.apply(el(map[string]string{"fill": "notacolor", "fill-rule": "evenodd",
		"stroke-dasharray": "4 2", "visibility": "hidden"}), nil)
	if s.fillRGBA() != (color.RGBA{0, 0, 0, 255}) || s.fillRule != render.EvenOdd {
		t.Errorf("bad fill / rule: %+v %v", s.fillRGBA(), s.fillRule)
	}
	if !reflect.DeepEqual(s.dashes, []float64{4, 2}) || s.visible {
		t.Errorf("dash/vis = %v %v", s.dashes, s.visible)
	}
}
