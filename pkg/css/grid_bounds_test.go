package css

import "testing"

// TestRepeatCountBounded pins the expanded-track ceiling. repeat() materializes
// every track it names, so an unbounded count is an allocation rather than a
// layout: repeat(200000000, 1px) hung the engine before this clamp.
func TestRepeatCountBounded(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"single inner track", "repeat(200000000, 1px)"},
		{"three inner tracks", "repeat(200000000, 1px 2px 3px)"},
		{"just past the ceiling", "repeat(100001, 1px)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tl, ok := parseTrackList(c.val)
			if !ok {
				t.Fatalf("parseTrackList(%q) ok=false", c.val)
			}
			tracks := tl.Expand(0)
			if len(tracks) > maxGridTracks {
				t.Errorf("%q expanded to %d tracks, past the %d ceiling",
					c.val, len(tracks), maxGridTracks)
			}
			if len(tracks) == 0 {
				t.Errorf("%q expanded to nothing; the leading tracks should survive", c.val)
			}
		})
	}
}

// TestRepeatCountUnderCeilingExact checks the clamp does not disturb ordinary
// track lists, which is the case every real stylesheet hits.
func TestRepeatCountUnderCeilingExact(t *testing.T) {
	tl, ok := parseTrackList("repeat(3, 10px 20px)")
	if !ok {
		t.Fatal("parseTrackList ok=false")
	}
	tracks := tl.Expand(0)
	if len(tracks) != 6 {
		t.Fatalf("len(tracks)=%d want 6", len(tracks))
	}
	if tracks[0].Min.Len.Value != 10 || tracks[1].Min.Len.Value != 20 {
		t.Errorf("tracks = %+v, want the 10px/20px pattern intact", tracks[:2])
	}
}

// TestGridLineBounded covers explicit line numbers and span counts. Placement
// fills one occupancy entry per covered cell, so `grid-row: 1 / 500000000` was
// 500 million map inserts. The sign is preserved: a negative line counts back
// from the end of the explicit grid, so it has to clamp to -maxGridTracks
// rather than to zero (line 0 is invalid).
func TestGridLineBounded(t *testing.T) {
	cases := []struct {
		in   string
		kind LineKind
		want int
	}{
		{"500000000", LineNum, maxGridTracks},
		{"-500000000", LineNum, -maxGridTracks},
		{"span 500000000", LineSpan, maxGridTracks},
		// Ordinary values pass through untouched.
		{"3", LineNum, 3},
		{"-2", LineNum, -2},
		{"span 4", LineSpan, 4},
	}
	for _, c := range cases {
		got, ok := parseGridLine(c.in)
		if !ok {
			t.Errorf("parseGridLine(%q) ok=false", c.in)
			continue
		}
		if got.Kind != c.kind || got.N != c.want {
			t.Errorf("parseGridLine(%q) = kind %v N %d, want kind %v N %d",
				c.in, got.Kind, got.N, c.kind, c.want)
		}
	}
}
