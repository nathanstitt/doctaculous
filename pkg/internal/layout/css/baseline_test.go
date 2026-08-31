package css

import "testing"

func TestFirstBaselineOffsetFromLine(t *testing.T) {
	f := &Fragment{Y: 10, Lines: []LineFragment{{BaselineY: 25}}}
	off, ok := firstBaselineOffset(f)
	if !ok || off != 15 {
		t.Fatalf("off=%v ok=%v want 15,true", off, ok)
	}
}

func TestFirstBaselineOffsetNoBaseline(t *testing.T) {
	f := &Fragment{Y: 0} // no lines, no children
	if _, ok := firstBaselineOffset(f); ok {
		t.Fatal("expected ok=false for baseline-free fragment")
	}
}

func TestAlignBaselineGroupShifts(t *testing.T) {
	// item A baseline at 10 below top, item B baseline at 25 below top => A shifts down 15.
	a := &Fragment{Y: 0, Lines: []LineFragment{{BaselineY: 10}}}
	b := &Fragment{Y: 0, Lines: []LineFragment{{BaselineY: 25}}}
	extra := alignBaselineGroup([]baselineItem{{a, true}, {b, true}})
	if extra != 15 {
		t.Fatalf("extra=%v want 15", extra)
	}
	if a.Y != 15 { // A moved down to align its baseline (now at 25) with B's
		t.Fatalf("a.Y=%v want 15", a.Y)
	}
	if b.Y != 0 { // B is the max baseline; unmoved
		t.Fatalf("b.Y=%v want 0", b.Y)
	}
}

// TestAlignBaselineGroupExactExtra pins I5: the returned extra is the EXACT increase in
// the group's lowest edge, not the largest single shift (which over-expands). A TALL item
// A (H=50, baseline 18) sets the group baseline; a SHORT item B (H=10, baseline 8) shifts
// DOWN by 10 to align — but its shifted bottom (20) is still WELL ABOVE A's bottom (50), so
// the group needs NO extra height. The old "largest shift" value would wrongly return 10.
func TestAlignBaselineGroupExactExtra(t *testing.T) {
	a := &Fragment{Y: 0, H: 50, Lines: []LineFragment{{BaselineY: 18}}}
	b := &Fragment{Y: 0, H: 10, Lines: []LineFragment{{BaselineY: 8}}}
	extra := alignBaselineGroup([]baselineItem{{a, true}, {b, true}})
	// A is the max baseline (unmoved); B shifts down 10. A bottom 50; B bottom 10->20.
	// Group bottom unchanged (50), so extra = 0 (the conservative value would be 10).
	if extra != 0 {
		t.Errorf("extra = %v, want 0 (B's shift stays within A's extent); the conservative value was 10", extra)
	}
	if b.Y != 10 {
		t.Errorf("b.Y = %v, want 10 (shifted to align its baseline with A's)", b.Y)
	}
	if a.Y != 0 {
		t.Errorf("a.Y = %v, want 0 (A is the max baseline, unmoved)", a.Y)
	}
}

// TestAlignBaselineGroupExtraWhenShiftedItemReachesLowest guards the other direction: when
// a shifted item DOES extend past the others, extra is the real overflow. A (H=20, baseline
// 5), B (H=20, baseline 15): B is max baseline; A shifts down 10 -> bottom 30 vs B bottom
// 20, so extra = 10.
func TestAlignBaselineGroupExtraWhenShiftedItemReachesLowest(t *testing.T) {
	a := &Fragment{Y: 0, H: 20, Lines: []LineFragment{{BaselineY: 5}}}
	b := &Fragment{Y: 0, H: 20, Lines: []LineFragment{{BaselineY: 15}}}
	extra := alignBaselineGroup([]baselineItem{{a, true}, {b, true}})
	if extra != 10 {
		t.Errorf("extra = %v, want 10 (A shifts to bottom 30, past B's 20)", extra)
	}
}
