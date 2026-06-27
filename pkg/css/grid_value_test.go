package css

import "testing"

func TestParseTrackListFixed(t *testing.T) {
	tl, ok := parseTrackList("100px 1fr auto")
	if !ok {
		t.Fatal("parseTrackList ok=false")
	}
	tracks := tl.Expand(0) // 0 = no auto-repeat container size; fixed lists ignore it
	if len(tracks) != 3 {
		t.Fatalf("len(tracks)=%d want 3", len(tracks))
	}
	if tracks[0].Min.Kind != trackLength || tracks[0].Min.Len.Value != 100 || tracks[0].Min.Len.Unit != UnitPx {
		t.Errorf("track0 = %+v want 100px", tracks[0].Min)
	}
	if tracks[1].Max.Kind != trackFlex || tracks[1].Max.Fr != 1 {
		t.Errorf("track1 = %+v want 1fr", tracks[1].Max)
	}
	if tracks[2].Min.Kind != trackAuto || tracks[2].Max.Kind != trackAuto {
		t.Errorf("track2 = %+v want auto/auto", tracks[2])
	}
}

func TestParseTrackListMinmax(t *testing.T) {
	tl, ok := parseTrackList("minmax(100px, 1fr)")
	if !ok {
		t.Fatal("ok=false")
	}
	tracks := tl.Expand(0)
	if len(tracks) != 1 {
		t.Fatalf("len=%d want 1", len(tracks))
	}
	if tracks[0].Min.Kind != trackLength || tracks[0].Min.Len.Value != 100 {
		t.Errorf("min = %+v want 100px", tracks[0].Min)
	}
	if tracks[0].Max.Kind != trackFlex || tracks[0].Max.Fr != 1 {
		t.Errorf("max = %+v want 1fr", tracks[0].Max)
	}
}

func TestParseTrackListRepeatN(t *testing.T) {
	tl, ok := parseTrackList("repeat(3, 50px)")
	if !ok {
		t.Fatal("ok=false")
	}
	tracks := tl.Expand(0)
	if len(tracks) != 3 {
		t.Fatalf("len=%d want 3", len(tracks))
	}
	for i, tr := range tracks {
		if tr.Min.Len.Value != 50 {
			t.Errorf("track %d = %+v want 50px", i, tr)
		}
	}
}

func TestParseTrackListRepeatInner(t *testing.T) {
	// repeat(2, 1fr 2fr) => 1fr 2fr 1fr 2fr.
	tl, _ := parseTrackList("repeat(2, 1fr 2fr)")
	tracks := tl.Expand(0)
	want := []float64{1, 2, 1, 2}
	if len(tracks) != 4 {
		t.Fatalf("len=%d want 4", len(tracks))
	}
	for i, fr := range want {
		if tracks[i].Max.Fr != fr {
			t.Errorf("track %d fr=%v want %v", i, tracks[i].Max.Fr, fr)
		}
	}
}

func TestParseTrackListAutoFill(t *testing.T) {
	// repeat(auto-fill, 100px) in a 350px container with 0 gap => floor(350/100)=3 tracks.
	tl, ok := parseTrackList("repeat(auto-fill, 100px)")
	if !ok {
		t.Fatal("ok=false")
	}
	tracks := tl.Expand(350)
	if len(tracks) != 3 {
		t.Fatalf("auto-fill len=%d want 3", len(tracks))
	}
	// Indefinite container (size 0) => 1 repetition (spec fallback).
	if n := len(tl.Expand(0)); n != 1 {
		t.Fatalf("auto-fill indefinite len=%d want 1", n)
	}
}

func TestParseTrackListBadDegrades(t *testing.T) {
	if _, ok := parseTrackList("nonsense !!!"); ok {
		t.Error("expected ok=false for garbage track list")
	}
}
