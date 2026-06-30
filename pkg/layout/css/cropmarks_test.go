package css

import (
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
)

func countRules(items []layout.Item) int {
	n := 0
	for _, it := range items {
		if it.Kind == layout.BackgroundKind || it.Kind == layout.RuleKind {
			n++
		}
	}
	return n
}

func TestAppendCropMarks(t *testing.T) {
	// Trim 200x300, bleed 10 ⇒ media 220x320, trim box at [10,10]..[210,310].
	g := pageGeom{pageW: 200, pageH: 300, bleed: 10}
	g.used.Marks = "crop"
	var items []layout.Item
	items = appendCropMarks(items, g)
	// Crop marks: 2 per corner × 4 corners = 8 thin rules.
	if got := countRules(items); got != 8 {
		t.Errorf("crop marks rule count = %d, want 8 (2 per corner)", got)
	}
	// Every mark is black.
	for _, it := range items {
		if it.Rule.Color != (color.RGBA{0, 0, 0, 255}) {
			t.Errorf("mark color = %v, want black", it.Rule.Color)
		}
	}
	// All marks lie within the bleed band (outside the trim box [10,10]..[210,310]):
	// each mark rect must be entirely in the 10px border ring.
	for _, it := range items {
		r := it.Rule
		inTrim := r.XPt >= 10 && r.YPt >= 10 && r.XPt+r.WPt <= 210 && r.YPt+r.HPt <= 310
		if inTrim {
			t.Errorf("crop mark %+v lies inside the trim box; should be in the bleed band", r)
		}
	}
}

func TestAppendCrossMarks(t *testing.T) {
	g := pageGeom{pageW: 200, pageH: 300, bleed: 10}
	g.used.Marks = "cross"
	var items []layout.Item
	items = appendCropMarks(items, g)
	// Cross marks: a plus (2 rules) at each of the 4 edge midpoints = 8 rules.
	if got := countRules(items); got != 8 {
		t.Errorf("cross marks rule count = %d, want 8", got)
	}
}

func TestNoMarksNoItems(t *testing.T) {
	g := pageGeom{pageW: 200, pageH: 300, bleed: 10} // Marks == ""
	var items []layout.Item
	items = appendCropMarks(items, g)
	if len(items) != 0 {
		t.Errorf("no marks requested ⇒ no items, got %d", len(items))
	}
}
