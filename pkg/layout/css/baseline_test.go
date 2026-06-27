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
