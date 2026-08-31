package css

import (
	"image/color"
	"math"

	gcss "github.com/nathanstitt/omnidoc/pkg/internal/css"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout/cssbox"
)

// boxShadows resolves a box's parsed `box-shadow` list into the per-fragment
// specs the flatten stage carries, or nil when the box has no shadow.
//
// Everything context-dependent is resolved HERE, at layout time, for the same
// reason drop-shadow()'s colour is (see filterChain): an omitted colour means
// the box's own `color` property, which only the cascade knows, and an em
// length means the box's own font size. The painter therefore receives plain
// points and a concrete RGBA and needs neither a CSS parser nor a route back to
// the style tree.
//
// A shadow whose colour is FULLY TRANSPARENT is dropped. It paints nothing by
// definition, and dropping it keeps a `box-shadow: 0 0 0 10px transparent` from
// costing an offscreen surface and a blur pass to produce no pixels.
func (e *Engine) boxShadows(b *cssbox.Box) []ShadowSpec {
	if b == nil || len(b.Style.BoxShadow) == 0 {
		return nil
	}
	fs := b.Style.FontSizePt
	var out []ShadowSpec
	for _, s := range b.Style.BoxShadow {
		c := shadowColor(s, b)
		if c.A == 0 {
			continue
		}
		spec := ShadowSpec{
			OffsetX: shadowLen(s.OffsetX, fs),
			OffsetY: shadowLen(s.OffsetY, fs),
			Blur:    shadowLen(s.Blur, fs),
			Spread:  shadowLen(s.Spread, fs),
			Color:   c,
			Inset:   s.Inset,
		}
		// A negative blur cannot get here (parseBoxShadow rejects the whole
		// declaration), but an em length times a negative font size could in a
		// hand-built box tree, and a negative sigma is meaningless downstream.
		if spec.Blur < 0 {
			spec.Blur = 0
		}
		out = append(out, spec)
	}
	return out
}

// shadowLen resolves one shadow length to points. A percentage is not a valid
// <length> for box-shadow and is rejected at parse time, so the zero percentage
// basis passed here can never be consulted.
func shadowLen(l gcss.Length, fontSizePt float64) float64 {
	v, _ := resolveLen(l, fontSizePt, 0)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// shadowColor resolves one shadow's colour against b.
//
// An OMITTED colour, or the `currentColor` keyword, means the element's own
// `color` property — NOT black. This mirrors dropShadowColor exactly, and for
// the same reason: the two properties share the spec's rule that an unstated
// shadow colour is currentColor.
func shadowColor(s gcss.BoxShadow, b *cssbox.Box) color.RGBA {
	if s.HasColor {
		return s.Color
	}
	// The cascade initializes `color` to opaque black (pkg/css's initialStyle),
	// so a fully-zero value here can only come from a hand-built box that never
	// went through it. Treat that as unset rather than as a legitimately
	// transparent shadow, which would silently render nothing.
	cur := b.Style.Color
	if cur == (color.RGBA{}) {
		cur = color.RGBA{A: 255}
	}
	return cur
}

// shadowItem builds the flattenable item for one of f's shadows, deriving the
// shadow box from the fragment's CURRENT geometry.
//
// The shadow box differs by kind and this is the whole reason inset is not a
// sign flip:
//
//   - an OUTER shadow takes the BORDER box's shape, and paints outside it;
//   - an INSET shadow takes the PADDING box's shape, and paints inside it.
//
// Deriving it here rather than storing it means a pagination shift or a
// fragment split moves the shadow with the box for free — see Fragment.Shadows.
//
// BORDER-RADIUS INTEGRATION POINT. A rounded box's shadow must follow its
// corners. This branch is the single place the shadow's shape is chosen, and
// layout.ShadowItem's rectangle is the only geometry the painter reads, so
// adding radii means: (a) carry the box's four corner radii onto ShadowItem
// here — the OUTER shadow uses the border box's own radii, the INSET one uses
// those radii deflated by the border widths, per CSS Backgrounds 3 §5.3; and
// (b) teach pkg/layout/paint's shadowOutline (which today emits a plain rect)
// to emit the rounded path. Nothing else in the shadow pipeline is
// shape-aware: the spread inflate, the blur and the inset complement all
// operate on whatever shape that one helper produces.
func (f *Fragment) shadowItem(s ShadowSpec) layout.ShadowItem {
	x, y, w, h := f.X, f.Y, f.W, f.H
	if s.Inset {
		// The padding box: the border box deflated by the used border widths.
		bT, bR, bB, bL := f.Border[layout.EdgeTop].Width, f.Border[layout.EdgeRight].Width,
			f.Border[layout.EdgeBottom].Width, f.Border[layout.EdgeLeft].Width
		x, y = x+bL, y+bT
		w, h = w-bL-bR, h-bT-bB
	}
	return layout.ShadowItem{
		XPt: x, YPt: y, WPt: w, HPt: h,
		OffsetX: s.OffsetX, OffsetY: s.OffsetY,
		Blur: s.Blur, Spread: s.Spread,
		Color: s.Color, Inset: s.Inset,
	}
}
