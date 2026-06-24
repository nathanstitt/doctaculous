package css

import "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"

// floatBox is one placed float's margin-box rectangle in page space (points,
// Y-down), tagged with its side and carrying its laid-out fragment for the float
// paint layer. Floats are positioned by their margin box (CSS 9.5): the margin
// edges touch the containing block's content edge or a preceding float's margin
// edge.
type floatBox struct {
	side       cssbox.FloatKind // FloatLeft | FloatRight
	x, y, w, h float64          // margin-box rectangle
	frag       *Fragment        // the laid-out fragment (border box inside this margin box)
}

// floatContext records the floats placed in ONE block formatting context and
// answers the avoidance geometry the block stacker and inline formatting context
// query per vertical band. cbLeft/cbRight are the containing block's content-box
// left/right edges — the band floats sit within. It is mutable state local to a
// single Engine.Layout goroutine (a fresh context per BFC); it never escapes that
// goroutine, so it does not violate the read-only-after-Layout concurrency
// contract.
type floatContext struct {
	cbLeft, cbRight float64
	floats          []floatBox
}

// overlaps reports whether band [y, y+h) intersects float f's vertical extent.
// A zero-height query band still intersects a float that contains its y.
func (f floatBox) overlaps(y, h float64) bool {
	return y < f.y+f.h && y+h > f.y
}

// leftEdge returns the left content boundary in band [y, y+h): cbLeft pushed right
// by the right margin edge of every left float overlapping the band.
func (c *floatContext) leftEdge(y, h float64) float64 {
	edge := c.cbLeft
	for i := range c.floats {
		f := c.floats[i]
		if f.side == cssbox.FloatLeft && f.overlaps(y, h) {
			if right := f.x + f.w; right > edge {
				edge = right
			}
		}
	}
	return edge
}

// rightEdge returns the right content boundary in band [y, y+h): cbRight pulled
// left by the left margin edge of every right float overlapping the band.
func (c *floatContext) rightEdge(y, h float64) float64 {
	edge := c.cbRight
	for i := range c.floats {
		f := c.floats[i]
		if f.side == cssbox.FloatRight && f.overlaps(y, h) {
			if f.x < edge {
				edge = f.x
			}
		}
	}
	return edge
}

// place positions a w×h (margin-box) float on side at the lowest y' >= y where it
// fits between the current left/right edges, appends it to the context, and returns
// the placed floatBox (frag still nil — the caller sets it). A float wider than the
// whole band is placed at the edge at y (allowed to overflow) rather than looping.
// The loop is bounded: each retry lowers y' past at least one blocking float.
func (c *floatContext) place(side cssbox.FloatKind, w, h, y float64) floatBox {
	bandW := c.cbRight - c.cbLeft
	for {
		left := c.leftEdge(y, h)
		right := c.rightEdge(y, h)
		if w > bandW || right-left >= w {
			// Fits (or is wider than the whole band -> overflow at the edge).
			var x float64
			if side == cssbox.FloatRight {
				x = right - w
			} else {
				x = left
			}
			fb := floatBox{side: side, x: x, y: y, w: w, h: h}
			c.floats = append(c.floats, fb)
			return fb
		}
		// Doesn't fit at y: drop to the bottom of the shallowest float whose band
		// overlaps [y, y+h) (the next opportunity for more width).
		next := c.nextDropY(y, h)
		if next <= y {
			// No lower opportunity (shouldn't happen given the fit test, but guard
			// against a spin): place at the edge at y.
			var x float64
			if side == cssbox.FloatRight {
				x = right - w
			} else {
				x = left
			}
			fb := floatBox{side: side, x: x, y: y, w: w, h: h}
			c.floats = append(c.floats, fb)
			return fb
		}
		y = next
	}
}

// nextDropY returns the smallest float bottom strictly greater than y among floats
// overlapping band [y, y+h); if none, returns y (caller guards against a spin).
func (c *floatContext) nextDropY(y, h float64) float64 {
	best := y
	for i := range c.floats {
		f := c.floats[i]
		if !f.overlaps(y, h) {
			continue
		}
		if bottom := f.y + f.h; bottom > y && (best == y || bottom < best) {
			best = bottom
		}
	}
	return best
}

// clearY returns max(y, the lowest edge of all matching floats): the smallest y' >= y
// (furthest down the Y-down page) at which content clears the named side(s).
// "left"/"right" clear that side, "both" clears all floats, "none"/"" returns y.
func (c *floatContext) clearY(clear string, y float64) float64 {
	if clear == "none" || clear == "" {
		return y
	}
	out := y
	for i := range c.floats {
		f := c.floats[i]
		match := clear == "both" ||
			(clear == "left" && f.side == cssbox.FloatLeft) ||
			(clear == "right" && f.side == cssbox.FloatRight)
		if match {
			if bottom := f.y + f.h; bottom > out {
				out = bottom
			}
		}
	}
	return out
}

// floats2frags returns the fragments of the placed floats, in placement order, for
// the BFC owner to attach to its fragment's Floats slice (the float paint layer).
// nil-frag entries are skipped. This is also why floatBox carries frag: the geometry
// records each placed float's laid-out fragment so the paint layer can collect them.
func (c *floatContext) floats2frags() []*Fragment {
	if len(c.floats) == 0 {
		return nil
	}
	out := make([]*Fragment, 0, len(c.floats))
	for i := range c.floats {
		if c.floats[i].frag != nil {
			out = append(out, c.floats[i].frag)
		}
	}
	return out
}
