package css

import "testing"

func TestParsePositionRunning(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "position", Value: "running(header)"})
	if cs.Position != "running" {
		t.Errorf("Position = %q, want \"running\"", cs.Position)
	}
	if cs.RunningName != "header" {
		t.Errorf("RunningName = %q, want \"header\"", cs.RunningName)
	}
	// A normal position leaves RunningName empty.
	cs2 := initialStyle()
	applyDeclaration(&cs2, Declaration{Property: "position", Value: "absolute"})
	if cs2.RunningName != "" {
		t.Errorf("absolute RunningName = %q, want empty", cs2.RunningName)
	}
}
