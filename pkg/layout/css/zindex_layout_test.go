package css

import (
	"context"
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// findByBox walks a fragment tree (Children + Positioned + Floats) and returns
// the first fragment whose Box == target, or nil.
func findByBox(f *Fragment, target *cssbox.Box) *Fragment {
	if f == nil {
		return nil
	}
	if f.Box == target {
		return f
	}
	for _, c := range f.Children {
		if g := findByBox(c, target); g != nil {
			return g
		}
	}
	for _, p := range f.Positioned {
		if g := findByBox(p, target); g != nil {
			return g
		}
	}
	for _, fl := range f.Floats {
		if g := findByBox(fl, target); g != nil {
			return g
		}
	}
	return nil
}

// TestPositionedFragmentCarriesBox: a relatively-positioned box's fragment retains a
// pointer to its source cssbox.Box (so the flatten z-sort can read Box.Style.ZIndex).
func TestPositionedFragmentCarriesBox(t *testing.T) {
	eng := New(nil, nil, nil)
	relStyle := posStyle()
	relStyle.Height = gcss.Length{Value: 20, Unit: gcss.UnitPx}
	relStyle.BackgroundColor = color.RGBA{1, 2, 3, 255}
	rel := posBox(relStyle, cssbox.PosRelative)
	root := posBox(posStyle(), cssbox.PosStatic, rel)

	frag := eng.layoutTree(context.Background(), root, 200)
	got := findByBox(frag, rel)
	if got == nil {
		t.Fatal("relative fragment not found in tree")
	}
	if got.Box != rel {
		t.Errorf("frag.Box = %p, want the source box %p", got.Box, rel)
	}
}
