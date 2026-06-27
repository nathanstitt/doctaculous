package css

import (
	"math"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
)

func approxT(a, b float64) bool { return math.Abs(a-b) < 0.01 }

// fixedTrack builds a trackSpec for a fixed px length (minmax(len,len)).
func fixedTrack(px float64) trackSpec {
	return trackSpec{baseFloor: px, maxFixed: px}
}

// flexTrack builds an fr trackSpec (min auto, max Nfr).
func flexTrack(fr float64) trackSpec {
	return trackSpec{maxFixed: -1, isFlex: true, fr: fr, maxIsContent: false}
}

// autoTrack builds an auto trackSpec (content min, content max).
func autoTrack() trackSpec {
	return trackSpec{maxFixed: -1, minIsContent: true, maxIsContent: true, maxIsMaxC: true}
}

func TestTrackFixed(t *testing.T) {
	got := resolveTrackSizes([]trackSpec{fixedTrack(100), fixedTrack(50)}, nil, 300, 0)
	if !approxT(got[0], 100) || !approxT(got[1], 50) {
		t.Fatalf("got %v want [100 50]", got)
	}
}

func TestTrackFrDistribution(t *testing.T) {
	// available 300, two fr tracks 1fr,2fr, no fixed => fr unit = 300/3 = 100 => 100,200.
	got := resolveTrackSizes([]trackSpec{flexTrack(1), flexTrack(2)}, nil, 300, 0)
	if !approxT(got[0], 100) || !approxT(got[1], 200) {
		t.Fatalf("got %v want [100 200]", got)
	}
}

func TestTrackFixedPlusFr(t *testing.T) {
	// available 300, a 100px track + a 1fr track => fr gets 200 => 100,200.
	got := resolveTrackSizes([]trackSpec{fixedTrack(100), flexTrack(1)}, nil, 300, 0)
	if !approxT(got[0], 100) || !approxT(got[1], 200) {
		t.Fatalf("got %v want [100 200]", got)
	}
}

func TestTrackFrWithGap(t *testing.T) {
	// available 310, gap 10 between two 1fr tracks => leftover 300 => 150,150.
	got := resolveTrackSizes([]trackSpec{flexTrack(1), flexTrack(1)}, nil, 310, 10)
	if !approxT(got[0], 150) || !approxT(got[1], 150) {
		t.Fatalf("got %v want [150 150]", got)
	}
}

func TestTrackAutoFromContent(t *testing.T) {
	// two auto tracks, single-span items contributing min/max-content 40 and 90.
	// No free space distribution target here (available exactly fits): each auto track
	// sizes to its item's max-content when there is room; available 200 > 40+90=130 so
	// maximize raises them toward growth limits (max-content) equally up to the limit:
	// track0 limit 40, track1 limit 90 => bases become 40 and 90 (capped at limits),
	// leftover 70 unused (no fr track to absorb it).
	items := []trackItem{
		{start: 0, span: 1, minContent: 40, maxContent: 40},
		{start: 1, span: 1, minContent: 90, maxContent: 90},
	}
	got := resolveTrackSizes([]trackSpec{autoTrack(), autoTrack()}, items, 200, 0)
	if !approxT(got[0], 40) || !approxT(got[1], 90) {
		t.Fatalf("got %v want [40 90]", got)
	}
}

func TestTrackMinmax(t *testing.T) {
	// minmax(100px, 1fr) alone in 300 => base 100, grows via fr to fill => 300.
	tr := trackSpec{baseFloor: 100, maxFixed: -1, isFlex: true, fr: 1}
	got := resolveTrackSizes([]trackSpec{tr}, nil, 300, 0)
	if !approxT(got[0], 300) {
		t.Fatalf("got %v want [300]", got)
	}
}

func TestTrackZeroLeftover(t *testing.T) {
	// available 100, two 100px fixed tracks => overflow; tracks stay at 100 each
	// (no shrink in grid track sizing), no panic.
	got := resolveTrackSizes([]trackSpec{fixedTrack(100), fixedTrack(100)}, nil, 100, 0)
	if !approxT(got[0], 100) || !approxT(got[1], 100) {
		t.Fatalf("got %v want [100 100]", got)
	}
}

func TestTrackSpanDistributesToIntrinsic(t *testing.T) {
	// two auto tracks; a single span-2 item with min-content 100 and no single-span
	// items => the 100 distributes equally => 50,50.
	items := []trackItem{{start: 0, span: 2, minContent: 100, maxContent: 100}}
	got := resolveTrackSizes([]trackSpec{autoTrack(), autoTrack()}, items, 200, 0)
	if !approxT(got[0], 50) || !approxT(got[1], 50) {
		t.Fatalf("got %v want [50 50]", got)
	}
}

// TestMakeTrackSpec covers the cascade->resolver bridge: % resolves against available,
// em against the font size, a bare flex marks isFlex, and the content-kind flags map
// from the sizing functions. This is the contract Task 7 calls into.
func TestMakeTrackSpec(t *testing.T) {
	// minmax(50%, max-content) in a 400px container, 16pt font.
	ts := gcss.TrackSize{
		Min: gcss.SizingFn{Kind: gcss.TrackLength, Len: gcss.Length{Value: 50, Unit: gcss.UnitPercent}},
		Max: gcss.SizingFn{Kind: gcss.TrackMaxContent},
	}
	got := makeTrackSpec(ts, 400, 16)
	if !approxT(got.baseFloor, 200) {
		t.Errorf("baseFloor=%v want 200 (50%% of 400)", got.baseFloor)
	}
	if got.maxFixed >= 0 {
		t.Errorf("maxFixed=%v want <0 (max-content has no fixed max)", got.maxFixed)
	}
	if got.isFlex {
		t.Error("isFlex=true want false (max is max-content, not fr)")
	}
	if !got.maxIsContent || !got.maxIsMaxC {
		t.Errorf("maxIsContent=%v maxIsMaxC=%v want both true", got.maxIsContent, got.maxIsMaxC)
	}

	// A bare 2fr: min auto, max 2fr.
	fr := makeTrackSpec(gcss.TrackSize{
		Min: gcss.SizingFn{Kind: gcss.TrackAuto},
		Max: gcss.SizingFn{Kind: gcss.TrackFlex, Fr: 2},
	}, 400, 16)
	if !fr.isFlex || !approxT(fr.fr, 2) {
		t.Errorf("fr spec isFlex=%v fr=%v want true,2", fr.isFlex, fr.fr)
	}
	if !fr.minIsContent {
		t.Error("fr spec minIsContent=false want true (min is auto)")
	}

	// An em min track exercises the font-size path and the min-content flag.
	em := makeTrackSpec(gcss.TrackSize{
		Min: gcss.SizingFn{Kind: gcss.TrackMinContent},
		Max: gcss.SizingFn{Kind: gcss.TrackLength, Len: gcss.Length{Value: 3, Unit: gcss.UnitEm}},
	}, 0, 10)
	if !approxT(em.maxFixed, 30) {
		t.Errorf("em maxFixed=%v want 30 (3em x 10pt)", em.maxFixed)
	}
	if em.minIsMaxC {
		t.Error("em.minIsMaxC=true want false (min is min-content, not max-content)")
	}
}
