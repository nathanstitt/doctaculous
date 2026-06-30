package css

import (
	"image/color"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// markThickness is the hairline width of a printed registration mark (px-as-pt).
const markThickness = 0.5

// markLength is the length of a crop mark line (along the trim edge), in px-as-pt.
const markLength = 6

// markGap is the gap between the trim box edge and the near end of a crop mark, so the
// marks sit clear of the artwork (standard print practice), in px-as-pt.
const markGap = 2

// appendCropMarks appends the @page registration marks for page geometry g to items,
// drawn as thin black filled rectangles in the bleed band OUTSIDE the trim box. It
// honors "crop" (corner trim marks) and "cross" (edge-midpoint registration pluses);
// "crop cross" draws both. A no-op when g requests no marks. All coordinates are in the
// MEDIA-box frame (the trim box occupies [bleed,bleed]..[bleed+pageW,bleed+pageH]).
func appendCropMarks(items []layout.Item, g pageGeom) []layout.Item {
	marks := strings.Fields(g.used.Marks)
	wantCrop, wantCross := false, false
	for _, m := range marks {
		switch m {
		case "crop":
			wantCrop = true
		case "cross":
			wantCross = true
		}
	}
	if !wantCrop && !wantCross {
		return items
	}
	b := g.bleed
	// Trim box corners in media space.
	left, top := b, b
	right, bottom := b+g.pageW, b+g.pageH

	rule := func(x, y, w, h float64) {
		items = append(items, layout.Item{
			Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: x, YPt: y, WPt: w, HPt: h, Color: color.RGBA{0, 0, 0, 255}},
		})
	}

	if wantCrop {
		// Each corner: one horizontal mark and one vertical mark, both in the bleed band,
		// offset from the trim edges by markGap, length markLength toward the page edge.
		// Top-left corner.
		rule(left-markGap-markLength, top, markLength, markThickness) // horizontal, left of corner
		rule(left, top-markGap-markLength, markThickness, markLength) // vertical, above corner
		// Top-right.
		rule(right+markGap, top, markLength, markThickness)
		rule(right-markThickness, top-markGap-markLength, markThickness, markLength)
		// Bottom-left.
		rule(left-markGap-markLength, bottom-markThickness, markLength, markThickness)
		rule(left, bottom+markGap, markThickness, markLength)
		// Bottom-right.
		rule(right+markGap, bottom-markThickness, markLength, markThickness)
		rule(right-markThickness, bottom+markGap, markThickness, markLength)
	}

	if wantCross {
		// A plus centered on each edge midpoint, drawn in the bleed band straddling the
		// page edge. Plus arm length markLength, centered.
		cx := (left + right) / 2
		cy := (top + bottom) / 2
		half := markLength / 2.0
		// midTop: centered at (cx, top-bleed/2) — in the top bleed band.
		midY := top - b/2
		rule(cx-half, midY-markThickness/2, markLength, markThickness)
		rule(cx-markThickness/2, midY-half, markThickness, markLength)
		// midBottom.
		midY = bottom + b/2
		rule(cx-half, midY-markThickness/2, markLength, markThickness)
		rule(cx-markThickness/2, midY-half, markThickness, markLength)
		// midLeft.
		midX := left - b/2
		rule(midX-half, cy-markThickness/2, markLength, markThickness)
		rule(midX-markThickness/2, cy-half, markThickness, markLength)
		// midRight.
		midX = right + b/2
		rule(midX-half, cy-markThickness/2, markLength, markThickness)
		rule(midX-markThickness/2, cy-half, markThickness, markLength)
	}
	return items
}
