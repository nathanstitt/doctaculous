package cssbox

import "testing"

func TestBoxKindPredicates(t *testing.T) {
	cases := []struct {
		k                   BoxKind
		blockLvl, inlineLvl bool
	}{
		{BoxBlock, true, false},
		{BoxAnonBlock, true, false},
		{BoxInline, false, true},
		{BoxAnonInline, false, true},
		{BoxText, false, true},
		{BoxReplaced, false, true}, // a bare <img> is inline-level by default
	}
	for _, c := range cases {
		if got := c.k.IsBlockLevel(); got != c.blockLvl {
			t.Errorf("%v.IsBlockLevel() = %v, want %v", c.k, got, c.blockLvl)
		}
		if got := c.k.IsInlineLevel(); got != c.inlineLvl {
			t.Errorf("%v.IsInlineLevel() = %v, want %v", c.k, got, c.inlineLvl)
		}
	}
}

func TestLeafBoxesHaveNoChildren(t *testing.T) {
	// Documents the contract that text/replaced boxes are leaves.
	for _, k := range []BoxKind{BoxText, BoxReplaced} {
		b := &Box{Kind: k}
		if len(b.Children) != 0 {
			t.Errorf("%v leaf unexpectedly has children", k)
		}
	}
}
