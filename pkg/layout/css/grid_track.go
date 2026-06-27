package css

import (
	"math"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
)

// trackSpec is the resolved sizing of one grid track for the pure resolver: the min
// and max sizing functions reduced to numbers where possible. isFlex marks an fr
// track (its growth is handled in the expand-flexible step); fr is its flex factor.
// baseFloor is the track's fixed minimum (a length min sizing function, % already
// resolved against the available space; 0 for intrinsic mins). maxFixed is the
// track's fixed maximum (length max sizing function, % resolved); maxFixed < 0 means
// "no fixed max" (auto/min-content/max-content/fr max => grows by content/flex).
// minIsContent/maxIsContent mark whether the min/max sizing function is content-based
// (auto/min-content => use item min-content; max-content => item max-content).
type trackSpec struct {
	baseFloor    float64 // fixed length min (px), else 0
	maxFixed     float64 // fixed length max (px), else <0 = none
	isFlex       bool
	fr           float64
	minIsContent bool // min sizing fn is auto/min-content/max-content (seed base from content)
	minIsMaxC    bool // min sizing fn is max-content specifically (else min-content)
	maxIsContent bool // max sizing fn is auto/min-content/max-content (limit grows to content)
	maxIsMaxC    bool // max sizing fn is auto/max-content (use max-content for the limit; min-content otherwise)
}

// trackItem is one item's content contribution to the tracks it spans, for intrinsic
// track sizing. start is the 0-based first track it occupies; span the track count;
// minContent/maxContent are the item's content sizes along this axis (px).
type trackItem struct {
	start, span            int
	minContent, maxContent float64
}

// resolveTrackSizes runs the CSS Grid §11 track-sizing algorithm for one axis and
// returns each track's final used size (px), in track order. specs are the tracks
// (already %-resolved against available); items are the content contributions;
// available is the container's content size on this axis (px); gap is the total
// inter-track gap (px). It is PURE: no engine, context, or boxes — only numbers — so
// it is unit-tested with hand-computed vectors. It never panics: a degenerate input
// returns base sizes, and every distribution loop is bounded (each pass either lowers
// a positive remainder by a positive amount or breaks).
func resolveTrackSizes(specs []trackSpec, items []trackItem, available, gap float64) []float64 {
	n := len(specs)
	if n == 0 {
		return nil
	}
	base := make([]float64, n)
	lim := make([]float64, n) // growth limit; math.MaxFloat64 stands in for +inf
	const inf = math.MaxFloat64

	// 1. Initialize base sizes and growth limits (CSS Grid §11.4).
	for i, s := range specs {
		base[i] = s.baseFloor // fixed-length min, else 0 (content seeded in step 2)
		if s.maxFixed >= 0 {
			lim[i] = s.maxFixed
		} else {
			lim[i] = inf // auto/min-content/max-content/fr max => unbounded for now
		}
		// Growth limit must be >= base size.
		if lim[i] < base[i] {
			lim[i] = base[i]
		}
	}

	// 2. Resolve intrinsic track sizes from item content contributions (§11.5).
	//    a. Single-span items: raise each spanned (here: single) track's base to the
	//       item's min-content, and its growth limit to the item's max-content (only
	//       for content-based limits; a fixed-max track is not raised past its max).
	resolveIntrinsicForSpan(specs, base, lim, items, 1)
	//    b. Multi-span items, in increasing span order, distributing extra space across
	//       the spanned intrinsic tracks (CSS Grid §11.5.1 "distribute extra space").
	maxSpan := 1
	for _, it := range items {
		if it.span > maxSpan {
			maxSpan = it.span
		}
	}
	for sp := 2; sp <= maxSpan; sp++ {
		resolveIntrinsicForSpan(specs, base, lim, items, sp)
	}
	// Clamp each growth limit up to its base (a base raised past an inf-or-fixed limit).
	for i := range base {
		if lim[i] < base[i] {
			lim[i] = base[i]
		}
	}
	// §11.5 final step: any track that still has an infinite growth limit gets its
	// growth limit set to its base size, so an intrinsic (auto/content) track cannot be
	// grown past its content size by the maximize step. (Flex tracks ignore lim in the
	// expand-flexible step, so clamping their limit here is harmless.)
	for i := range lim {
		if lim[i] == inf {
			lim[i] = base[i]
		}
	}

	// 3. Maximize tracks: distribute the free space to non-flexible tracks up to their
	//    growth limits, equally (CSS Grid §11.6). Flexible tracks do not participate
	//    here (they grow in step 4).
	freeSpace := available - gap - sumF(base)
	if freeSpace > 0 {
		distributeToLimits(base, lim, specs, freeSpace)
	}

	// 4. Expand flexible tracks (CSS Grid §11.7): compute the fr unit and set each flex
	//    track to max(its base, frUnit * its fr).
	if anyFlex(specs) {
		frUnit := findFrUnit(specs, base, available, gap)
		for i, s := range specs {
			if !s.isFlex {
				continue
			}
			grown := frUnit * s.fr
			if grown > base[i] {
				base[i] = grown
			}
		}
	}

	return base
}

// sumF returns the sum of a float slice.
func sumF(xs []float64) float64 {
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s
}

// anyFlex reports whether any track is flexible (fr).
func anyFlex(specs []trackSpec) bool {
	for _, s := range specs {
		if s.isFlex {
			return true
		}
	}
	return false
}

// resolveIntrinsicForSpan raises bases/limits of intrinsic tracks for every item whose
// span == targetSpan. For span 1 it raises the single track directly; for span>1 it
// distributes the item's content size (beyond the spanned tracks' current bases) across
// the spanned tracks that have an intrinsic (content-based) min sizing function, equally
// (the spec distributes to "tracks with an intrinsic min sizing function"; if none, to
// the spanned tracks that are not fixed on that axis — see distributeExtra). Fixed-base
// tracks in the span absorb their fixed base first.
func resolveIntrinsicForSpan(specs []trackSpec, base, lim []float64, items []trackItem, targetSpan int) {
	for _, it := range items {
		if it.span != targetSpan {
			continue
		}
		// Guard against malformed placement (bug C2): an item whose track range falls
		// outside the track count would index out of range. Skip it (the resolver's
		// contract is that it never panics on degenerate input).
		if it.start < 0 || it.span < 1 || it.start+it.span > len(base) {
			continue
		}
		// --- raise for min-content (base) ---
		spanBase := 0.0
		for k := 0; k < it.span; k++ {
			spanBase += base[it.start+k]
		}
		if it.minContent > spanBase {
			distributeExtra(specs, base, it.start, it.span, it.minContent-spanBase, false)
		}
		// --- raise for max-content (growth limit) ---
		// Seed any infinite growth limit in the span to its track's current base BEFORE
		// summing/distributing (bug C1): otherwise distributeExtra resets the +inf
		// sentinel to 0 and only adds the delta, leaving the limit at maxContent-base
		// instead of maxContent. Seeding to the base means the distributed delta
		// (maxContent - Σbase) lands on top of the base, so the limit reaches maxContent.
		for k := 0; k < it.span; k++ {
			if lim[it.start+k] == math.MaxFloat64 {
				lim[it.start+k] = base[it.start+k]
			}
		}
		spanLim := 0.0
		for k := 0; k < it.span; k++ {
			spanLim += lim[it.start+k]
		}
		if it.maxContent > spanLim {
			distributeExtra(specs, lim, it.start, it.span, it.maxContent-spanLim, true)
		}
	}
}

// distributeExtra adds `extra` to a span's tracks (base or limit array `dst`),
// preferring tracks whose relevant sizing function is intrinsic/content-based. toLimit
// selects which sizing function decides "intrinsic" (max sizing fn for limits, min
// sizing fn for bases). If no track has an intrinsic sizing function, it falls back to
// the spanned tracks that are NOT fixed on the relevant axis (bug I1): a fixed-max
// track's growth limit is its fixed max by definition and must never be raised by
// content, and a fixed-length-min track's base must not be raised past its fixed min by
// the fallback. If that leaves no recipients, the extra is simply not distributed (the
// content genuinely cannot raise any track in the span).
func distributeExtra(specs []trackSpec, dst []float64, start, span int, extra float64, toLimit bool) {
	// Find recipient tracks whose relevant sizing function is content-based.
	var recip []int
	for k := 0; k < span; k++ {
		i := start + k
		intrinsic := specs[i].minIsContent
		if toLimit {
			intrinsic = specs[i].maxIsContent
		}
		if intrinsic {
			recip = append(recip, i)
		}
	}
	if len(recip) == 0 {
		// Fallback: distribute to spanned tracks that are not fixed on this axis.
		for k := 0; k < span; k++ {
			i := start + k
			fixed := !specs[i].minIsContent // fixed-length (or zero) min sizing function
			if toLimit {
				fixed = specs[i].maxFixed >= 0 // fixed-length max sizing function
			}
			if !fixed {
				recip = append(recip, i)
			}
		}
	}
	if len(recip) == 0 {
		return // nothing in the span can absorb the content; leave sizes unchanged
	}
	share := extra / float64(len(recip))
	for _, i := range recip {
		if dst[i] == math.MaxFloat64 {
			dst[i] = 0
		}
		dst[i] += share
	}
}

// distributeToLimits adds freeSpace to non-flex tracks equally but not past each track's
// growth limit (CSS Grid §11.6 maximize). Tracks at their limit drop out; the loop
// repeats with the remainder until no space remains or every non-flex track is capped.
func distributeToLimits(base, lim []float64, specs []trackSpec, freeSpace float64) {
	for freeSpace > 0.0001 {
		// Recipients: non-flex tracks below their growth limit.
		var recip []int
		for i, s := range specs {
			if s.isFlex {
				continue
			}
			if lim[i] == math.MaxFloat64 || base[i] < lim[i] {
				recip = append(recip, i)
			}
		}
		if len(recip) == 0 {
			break
		}
		share := freeSpace / float64(len(recip))
		distributed := 0.0
		for _, i := range recip {
			grow := share
			if lim[i] != math.MaxFloat64 && base[i]+grow > lim[i] {
				grow = lim[i] - base[i]
			}
			base[i] += grow
			distributed += grow
		}
		if distributed <= 0.0001 {
			break
		}
		freeSpace -= distributed
	}
}

// findFrUnit computes the fr unit for the expand-flexible step (CSS Grid §11.7.1): the
// leftover space after non-flex track bases and gaps, divided by the sum of flex
// factors with EACH factor floored at 1 for the division (a track with factor < 1 uses
// 1 in the divisor but still sizes as frUnit × its actual factor, so sub-1 factors do
// not fill the container). The fr unit must be at least the largest (base / fr) among
// flex tracks so no flex track shrinks below its base.
func findFrUnit(specs []trackSpec, base []float64, available, gap float64) float64 {
	leftover := available - gap
	sumFr := 0.0
	for i, s := range specs {
		if s.isFlex {
			factor := s.fr
			if factor < 1 {
				factor = 1 // spec §11.7.1: floor EACH flex factor at 1 for the divisor
			}
			sumFr += factor
		} else {
			leftover -= base[i]
		}
	}
	if sumFr < 1 {
		sumFr = 1 // no flex tracks (or all-zero factors): avoid divide-by-zero
	}
	if leftover < 0 {
		leftover = 0
	}
	frUnit := leftover / sumFr
	// Ensure no flex track would be smaller than its current base.
	for i, s := range specs {
		if s.isFlex && s.fr > 0 {
			need := base[i] / s.fr
			if need > frUnit {
				frUnit = need
			}
		}
	}
	return frUnit
}

// sizingFloor returns the fixed px floor for a min sizing function: the length for a
// TrackLength (% × available/100, em × fontSizePt, px/pt as-is), else 0 (intrinsic
// mins seed from content). A % against an indefinite available (<=0) resolves to 0.
func sizingFloor(fn gcss.SizingFn, available, fontSizePt float64) float64 {
	if fn.Kind != gcss.TrackLength {
		return 0
	}
	return resolveSizingLen(fn.Len, available, fontSizePt)
}

// sizingMax returns the fixed px max for a max sizing function: the length for a
// TrackLength, else -1 (no fixed max — auto/min-content/max-content/fr grow further).
func sizingMax(fn gcss.SizingFn, available, fontSizePt float64) float64 {
	if fn.Kind != gcss.TrackLength {
		return -1
	}
	return resolveSizingLen(fn.Len, available, fontSizePt)
}

// isContentSizing reports whether a sizing function is content-based (auto,
// min-content, or max-content) — i.e. seeded/limited by item content sizes.
func isContentSizing(fn gcss.SizingFn) bool {
	switch fn.Kind {
	case gcss.TrackAuto, gcss.TrackMinContent, gcss.TrackMaxContent:
		return true
	}
	return false
}

// resolveSizingLen reduces a track-length to px. % resolves against available (0 when
// indefinite), em against fontSizePt, px/pt as-is.
func resolveSizingLen(l gcss.Length, available, fontSizePt float64) float64 {
	switch l.Unit {
	case gcss.UnitPx, gcss.UnitPt:
		return l.Value
	case gcss.UnitEm:
		return l.Value * fontSizePt
	case gcss.UnitPercent:
		if available <= 0 {
			return 0
		}
		return l.Value / 100 * available
	default:
		return l.Value
	}
}

// makeTrackSpec reduces a parsed gcss.TrackSize to a trackSpec, resolving percentages
// against available and em lengths via fontSizePt. This is the bridge from the
// cascade's parsed track to the pure resolver's numeric spec.
func makeTrackSpec(t gcss.TrackSize, available, fontSizePt float64) trackSpec {
	return trackSpec{
		baseFloor:    sizingFloor(t.Min, available, fontSizePt),
		maxFixed:     sizingMax(t.Max, available, fontSizePt),
		isFlex:       t.Max.Kind == gcss.TrackFlex,
		fr:           t.Max.Fr,
		minIsContent: isContentSizing(t.Min),
		minIsMaxC:    t.Min.Kind == gcss.TrackMaxContent,
		maxIsContent: isContentSizing(t.Max),
		maxIsMaxC:    t.Max.Kind == gcss.TrackMaxContent || t.Max.Kind == gcss.TrackAuto,
	}
}
