package css

import "math"

// flexItemSizing is the per-item input to the §9.7 flexible-length resolution: the
// purely numeric facts the algorithm needs, with NO layout dependency so the resolver
// is unit-testable in isolation. maxMain < 0 means "no maximum" (CSS `none`).
type flexItemSizing struct {
	base         float64 // flex base size (resolved flex-basis)
	hypothetical float64 // base clamped to [minMain, maxMain] (the hypothetical main size)
	grow         float64 // flex-grow
	shrink       float64 // flex-shrink
	minMain      float64 // used minimum main size (incl. the automatic minimum)
	maxMain      float64 // used maximum main size; <0 = none
}

// clampF clamps v to [lo, hi]; hi < 0 means no upper bound.
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		v = lo
	}
	if hi >= 0 && v > hi {
		v = hi
	}
	return v
}

// resolveFlexibleLengths implements CSS Flexbox 9.7 for a single flex line and returns
// each item's used main size, in item order. innerMain is the flex container's inner
// main size; totalGap is the sum of all main-axis gaps between items. The algorithm is
// a multi-pass freeze loop: pick grow vs shrink, freeze inflexible items, then loop
// {distribute proportional to the used factor, clamp to min/max, freeze items that
// violated} until no flexible items remain.
func resolveFlexibleLengths(items []flexItemSizing, innerMain, totalGap float64) []float64 {
	n := len(items)
	target := make([]float64, n)
	frozen := make([]bool, n)

	// 1. Used flex factor: grow if there is surplus, else shrink.
	sumHypo := totalGap
	for i := range items {
		sumHypo += items[i].hypothetical
	}
	growing := sumHypo < innerMain

	// 2. Size inflexible items (freeze) and seed targets at the hypothetical size.
	for i := range items {
		it := items[i]
		target[i] = it.hypothetical
		factor := it.shrink
		if growing {
			factor = it.grow
		}
		switch {
		case factor == 0:
			frozen[i] = true
		case growing && it.base > it.hypothetical:
			frozen[i] = true
		case !growing && it.base < it.hypothetical:
			frozen[i] = true
		default:
			target[i] = it.base // unfrozen items start the loop at their base size
		}
	}

	// 3. Initial free space (frozen at frozen size, unfrozen at base size).
	initialFree := innerMain - totalGap
	for i := range items {
		if frozen[i] {
			initialFree -= target[i]
		} else {
			initialFree -= items[i].base
		}
	}

	viol := make([]int, n) // per-pass min/max violation flags (+1 up, -1 down, 0 none); reused each pass

	// 4. Loop until no unfrozen items remain.
	for {
		// (a) Check for flexible items; exit when all are frozen.
		anyUnfrozen := false
		for i := range items {
			if !frozen[i] {
				anyUnfrozen = true
				break
			}
		}
		if !anyUnfrozen {
			break
		}

		// (b) Remaining free space; with the sub-1 flex-factor-sum adjustment.
		remaining := innerMain - totalGap
		sumFactor := 0.0
		for i := range items {
			if frozen[i] {
				remaining -= target[i]
				continue
			}
			remaining -= items[i].base
			if growing {
				sumFactor += items[i].grow
			} else {
				sumFactor += items[i].shrink
			}
		}
		if sumFactor < 1 {
			scaled := initialFree * sumFactor
			if math.Abs(scaled) < math.Abs(remaining) {
				remaining = scaled
			}
		}

		// (c) Distribute proportional to the used flex factor.
		if growing {
			totalGrow := 0.0
			for i := range items {
				if !frozen[i] {
					totalGrow += items[i].grow
				}
			}
			if totalGrow > 0 {
				for i := range items {
					if !frozen[i] {
						target[i] = items[i].base + remaining*(items[i].grow/totalGrow)
					}
				}
			}
		} else {
			totalScaled := 0.0
			for i := range items {
				if !frozen[i] {
					totalScaled += items[i].shrink * items[i].base
				}
			}
			if totalScaled > 0 {
				for i := range items {
					if !frozen[i] {
						ratio := (items[i].shrink * items[i].base) / totalScaled
						target[i] = items[i].base + remaining*ratio // remaining is negative when shrinking
					}
				}
			}
		}

		// (d) Fix min/max violations; record the total violation sign.
		totalViolation := 0.0
		for i := range viol {
			viol[i] = 0
		}
		for i := range items {
			if frozen[i] {
				continue
			}
			clamped := clampF(target[i], items[i].minMain, items[i].maxMain)
			if clamped > target[i] {
				viol[i] = 1
			} else if clamped < target[i] {
				viol[i] = -1
			}
			totalViolation += clamped - target[i]
			target[i] = clamped
		}

		// (e) Freeze by total-violation sign.
		switch {
		case totalViolation == 0:
			for i := range items {
				frozen[i] = true
			}
		case totalViolation > 0:
			for i := range items {
				if viol[i] == 1 {
					frozen[i] = true
				}
			}
		default:
			for i := range items {
				if viol[i] == -1 {
					frozen[i] = true
				}
			}
		}
	}

	return target
}
