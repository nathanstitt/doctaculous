package css

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// propagateCanvasBackground implements CSS background propagation (CSS Backgrounds
// §3.11.2 / CSS 2.1 §14.2): the background of the root element (<html>) is painted
// over the whole page canvas, not just the root's box. If the root has no
// background, the <body>'s background is used instead (and propagated). The element
// the background is taken from then paints its OWN box with no background, so the
// color is not painted twice.
//
// It mutates root (zeroing the source box's BackgroundColor) and returns the
// propagated canvas color, or a zero (transparent) color when neither the root nor
// the body has a non-transparent background (the caller then keeps the renderer's
// default page fill). root may be nil.
func propagateCanvasBackground(root *cssbox.Box) color.RGBA {
	if root == nil {
		return color.RGBA{}
	}
	// Source per the rule: the root element if it has a background, else the body.
	src := root
	if root.Style.BackgroundColor.A == 0 {
		if body := bodyBox(root); body != nil {
			src = body
		}
	}
	bg := src.Style.BackgroundColor
	if bg.A == 0 {
		return color.RGBA{} // neither root nor body has a background: no propagation
	}
	// The source box no longer paints its own background (the canvas carries it).
	src.Style.BackgroundColor = color.RGBA{}
	return bg
}

// bodyBox returns the <body> box within the root subtree. The HTML builder produces
// the <html> element as the root box with <head> pruned (display:none) and <body> as
// its child, so the body is the root's first block-level child. (cssbox.Box does not
// record its source tag, so this is identified structurally.) Returns nil when there
// is no such child.
func bodyBox(root *cssbox.Box) *cssbox.Box {
	for _, c := range root.Children {
		if c.Kind == cssbox.BoxBlock {
			return c
		}
	}
	return nil
}
