package css

import (
	"sort"

	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// splitFlexGridForPage splits a flex (column) or grid container between item ROWS at
// pageBottom: it groups direct item fragments into Y bands (items sharing a band ride one
// page together), keeps bands fully above pageBottom in the head, moves the rest to the
// tail. A single band (single-line flex / one grid row) cannot split → {tail} if it
// overflows the page from the top, else {head}. Mid-item content is not split.
func splitFlexGridForPage(c *Fragment, pageBottom float64) splitResult {
	bands := itemBands(c)
	if len(bands) <= 1 {
		if c.Y+c.H <= pageBottom+0.5 {
			return splitResult{head: c}
		}
		return splitResult{tail: c} // indivisible and overflowing
	}
	k := 0
	for i, bd := range bands {
		if bd.bottom <= pageBottom+0.5 {
			k = i + 1
		} else {
			break
		}
	}
	if k >= len(bands) {
		return splitResult{head: c}
	}
	if k == 0 {
		return splitResult{tail: c}
	}
	splitY := bands[k].top
	head := *c
	tail := *c
	head.Children = childrenAbove(c, splitY)
	tail.Children = childrenFrom(c, splitY)
	head.H = bands[k-1].bottom - c.Y
	tail.Y = splitY
	tail.H = (c.Y + c.H) - splitY
	head.Border[layout.EdgeBottom] = BorderEdge{}
	tail.Border[layout.EdgeTop] = BorderEdge{}
	return splitResult{head: &head, tail: &tail}
}

// band is a vertical extent of one flex/grid item row (a set of items whose Y-extents
// overlap), used to find a clean between-rows split point.
type band struct{ top, bottom float64 }

// itemBands groups c's direct in-flow children into vertical bands (a band spans a set of
// children whose Y-extents overlap), sorted top-to-bottom.
func itemBands(c *Fragment) []band {
	var bands []band
	for _, ch := range inFlowChildren(c) {
		t, b := ch.Y, ch.Y+ch.H
		merged := false
		for i := range bands {
			if t < bands[i].bottom-0.5 && b > bands[i].top+0.5 { // overlap → same band
				if t < bands[i].top {
					bands[i].top = t
				}
				if b > bands[i].bottom {
					bands[i].bottom = b
				}
				merged = true
				break
			}
		}
		if !merged {
			bands = append(bands, band{top: t, bottom: b})
		}
	}
	sort.Slice(bands, func(i, j int) bool { return bands[i].top < bands[j].top })
	return bands
}

// childrenAbove returns c's children whose top is above splitY (the head page's rows);
// out-of-flow children (floats/positioned) ride the head with the container.
func childrenAbove(c *Fragment, splitY float64) []*Fragment {
	var out []*Fragment
	for _, ch := range c.Children {
		if ch.Y < splitY-0.5 {
			out = append(out, ch)
		}
	}
	return out
}

// childrenFrom returns c's children whose top is at or below splitY (the tail page's rows).
func childrenFrom(c *Fragment, splitY float64) []*Fragment {
	var out []*Fragment
	for _, ch := range c.Children {
		if ch.Y >= splitY-0.5 {
			out = append(out, ch)
		}
	}
	return out
}
