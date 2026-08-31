package css

import "testing"

// TestCollapsedBordersFollowMarginTop: the collapsed-border grid strips must sit
// on the table's final border box, not marginTop (+table border/padding) above it.
// Regression: the strips were attached in the interior's local frame without the
// contentTopY shift the children get, so a collapsed table with margin-top painted
// its whole grid in the margin band (the htmldoc landscape wideledger artifact).
func TestCollapsedBordersFollowMarginTop(t *testing.T) {
	src := reset + `<div style="display:flex"><div style="height:40px">x</div></div>` +
		`<table style="border-collapse:collapse;margin-top:8px"><tr><td style="border:1px solid #000">a</td></tr></table>`
	body := layoutBody(t, src, 400)
	var find func(f *Fragment) *Fragment
	find = func(f *Fragment) *Fragment {
		if len(f.Collapsed) > 0 {
			return f
		}
		for _, c := range f.Children {
			if r := find(c); r != nil {
				return r
			}
		}
		return nil
	}
	tbl := find(body)
	if tbl == nil {
		t.Fatal("no collapsed-borders fragment")
	}
	minY, maxY := 1e9, -1e9
	for _, b := range tbl.Collapsed {
		if b.YPt < minY {
			minY = b.YPt
		}
		if y := b.YPt + b.HPt; y > maxY {
			maxY = y
		}
	}
	// The topmost strip is centered on the table's top grid line (border-box top):
	// its top edge is Y - width/2. Anything higher means the grid leaked into the
	// margin band.
	if want := tbl.Y - 0.5; absf(minY-want) > 1e-6 {
		t.Errorf("topmost collapsed strip Y = %v, want %v (centered on border-box top %v)", minY, want, tbl.Y)
	}
	if maxY > tbl.Y+tbl.H+0.5+1e-6 {
		t.Errorf("bottommost collapsed strip extends to %v, beyond border-box bottom %v", maxY, tbl.Y+tbl.H)
	}
}
