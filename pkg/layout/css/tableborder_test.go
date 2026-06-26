package css

import (
	"image/color"
	"testing"
)

func cb(width float64, style string, owner edgeOwner) collapsedBorder {
	return collapsedBorder{width: width, style: style, color: color.RGBA{0, 0, 0, 255}, owner: owner}
}

func TestCollapseHiddenWins(t *testing.T) {
	got := resolveCollapsedEdge(cb(2, "solid", ownerCell), cb(10, "hidden", ownerRow))
	if got.style != "hidden" {
		t.Errorf("hidden must win and suppress; got %q", got.style)
	}
	got2 := resolveCollapsedEdge(cb(2, "solid", ownerCell), cb(10, "solid", ownerRow))
	if got2.width != 10 {
		t.Errorf("wider wins; got %v", got2.width)
	}
}

func TestCollapseStyleRankAndOwnerTie(t *testing.T) {
	if resolveCollapsedEdge(cb(4, "dashed", ownerCell), cb(4, "solid", ownerRow)).style != "solid" {
		t.Error("solid should beat dashed at equal width")
	}
	if resolveCollapsedEdge(cb(4, "solid", ownerRow), cb(4, "double", ownerTable)).style != "double" {
		t.Error("double should beat solid at equal width")
	}
	got := resolveCollapsedEdge(cb(4, "solid", ownerCell), cb(4, "solid", ownerTable))
	if got.owner != ownerCell {
		t.Error("cell owner should beat table owner at equal width+style")
	}
	// none loses to any real border
	if resolveCollapsedEdge(cb(0, "none", ownerCell), cb(2, "solid", ownerTable)).style != "solid" {
		t.Error("a real border beats none")
	}
	// two none borders resolve to none (no edge emitted)
	if got := resolveCollapsedEdge(cb(0, "none", ownerCell), cb(0, "none", ownerTable)); got.style != "none" {
		t.Errorf("none+none should resolve to a none border (no strip emitted); got style=%q width=%v", got.style, got.width)
	}
}
