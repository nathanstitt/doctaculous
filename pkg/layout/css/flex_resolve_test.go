package css

import (
	"math"
	"testing"
)

func approxF(a, b float64) bool { return math.Abs(a-b) < 0.01 }

// sizing builds a flexItemSizing, pre-clamping the hypothetical size with the engine's
// clampF (defined in flex.go, same package). max < 0 means "no maximum".
func sizing(base, grow, shrink, min, max float64) flexItemSizing {
	return flexItemSizing{base: base, hypothetical: clampF(base, min, max), grow: grow, shrink: shrink, minMain: min, maxMain: max}
}

func TestResolveGrowSplitsSurplusByFactor(t *testing.T) {
	// inner=300, three items base 50 each (150 used) => 150 surplus.
	// grow factors 1,2,3 (sum 6) => shares 25,50,75 => 75,100,125.
	items := []flexItemSizing{
		sizing(50, 1, 0, 0, -1),
		sizing(50, 2, 0, 0, -1),
		sizing(50, 3, 0, 0, -1),
	}
	got := resolveFlexibleLengths(items, 300, 0)
	want := []float64{75, 100, 125}
	for i := range want {
		if !approxF(got[i], want[i]) {
			t.Errorf("item %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestResolveShrinkSplitsDeficitByScaledFactor(t *testing.T) {
	// inner=100, two items base 80 each (160 used) => deficit 60.
	// shrink 1 each; scaled factor = shrink*base = 80 each (equal) => each shrinks 30 => 50,50.
	items := []flexItemSizing{
		sizing(80, 0, 1, 0, -1),
		sizing(80, 0, 1, 0, -1),
	}
	got := resolveFlexibleLengths(items, 100, 0)
	want := []float64{50, 50}
	for i := range want {
		if !approxF(got[i], want[i]) {
			t.Errorf("item %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestResolveShrinkFlooredByMin(t *testing.T) {
	// inner=100, two items base 80; item0 min 70. Deficit 60, equal scaled factors.
	// Naive shrink => 50 each, but item0 floors at 70: it freezes at 70 (violation),
	// remaining deficit (160-100=60; item0 frozen at 70 contributes 70) redistributes
	// onto item1: 100-70=30 => item1=30.
	items := []flexItemSizing{
		sizing(80, 0, 1, 70, -1),
		sizing(80, 0, 1, 0, -1),
	}
	got := resolveFlexibleLengths(items, 100, 0)
	if !approxF(got[0], 70) || !approxF(got[1], 30) {
		t.Errorf("got %v, want [70 30]", got)
	}
}

func TestResolveGrowClampedByMax(t *testing.T) {
	// inner=300, two items base 50; surplus 200; grow 1 each => +100 each => 150,150.
	// item0 max 120 => freezes at 120 (max violation); its 70 of surplus redistributes
	// to item1 => item1 = 50 + 200 - (120-50) = 50 + 130 = 180.
	items := []flexItemSizing{
		sizing(50, 1, 0, 0, 120),
		sizing(50, 1, 0, 0, -1),
	}
	got := resolveFlexibleLengths(items, 300, 0)
	if !approxF(got[0], 120) || !approxF(got[1], 180) {
		t.Errorf("got %v, want [120 180]", got)
	}
}

func TestResolveGapConsumesMainSpace(t *testing.T) {
	// inner=300, two items base 50, total gap 100 => surplus = 300-100-100 = 100.
	// grow 1 each => +50 each => 100,100.
	items := []flexItemSizing{
		sizing(50, 1, 0, 0, -1),
		sizing(50, 1, 0, 0, -1),
	}
	got := resolveFlexibleLengths(items, 300, 100)
	if !approxF(got[0], 100) || !approxF(got[1], 100) {
		t.Errorf("got %v, want [100 100]", got)
	}
}

func TestResolveNoFlexFactorsStayAtBase(t *testing.T) {
	// grow=shrink=0 => inflexible; even with surplus they stay at hypothetical.
	items := []flexItemSizing{
		sizing(50, 0, 0, 0, -1),
		sizing(50, 0, 0, 0, -1),
	}
	got := resolveFlexibleLengths(items, 300, 0)
	if !approxF(got[0], 50) || !approxF(got[1], 50) {
		t.Errorf("got %v, want [50 50] (inflexible)", got)
	}
}

func TestResolveAllViolateFreezeTogether(t *testing.T) {
	// inner=300, two items base 50, grow 1 each; both max 60 => both want 150 but
	// both clamp to 60 (max violations, total<0 => freeze all max-violations at once).
	items := []flexItemSizing{
		sizing(50, 1, 0, 0, 60),
		sizing(50, 1, 0, 0, 60),
	}
	got := resolveFlexibleLengths(items, 300, 0)
	if !approxF(got[0], 60) || !approxF(got[1], 60) {
		t.Errorf("got %v, want [60 60]", got)
	}
}
