package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func blockNamed(name string) *Fragment {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	box.Style = gcss.ComputedStyle{Page: name}
	return &Fragment{Box: box, H: 10}
}

func TestGroupRuns(t *testing.T) {
	// Blocks: "", "", "wide", "wide", "" ⇒ three runs: [0,1] default, [2,3] wide, [4] default.
	blocks := []*Fragment{blockNamed(""), blockNamed(""), blockNamed("wide"), blockNamed("wide"), blockNamed("")}
	runs := groupRuns(blocks)
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3: %+v", len(runs), runs)
	}
	if runs[0].name != "" || runs[0].start != 0 || runs[0].end != 2 {
		t.Errorf("run0 = %+v, want {name:\"\" start:0 end:2}", runs[0])
	}
	if runs[1].name != "wide" || runs[1].start != 2 || runs[1].end != 4 {
		t.Errorf("run1 = %+v, want {name:wide start:2 end:4}", runs[1])
	}
	if runs[2].name != "" || runs[2].start != 4 || runs[2].end != 5 {
		t.Errorf("run2 = %+v, want {name:\"\" start:4 end:5}", runs[2])
	}
}

func TestGroupRunsSingle(t *testing.T) {
	// All same name ⇒ one run spanning everything (the byte-identical default case).
	blocks := []*Fragment{blockNamed(""), blockNamed(""), blockNamed("")}
	runs := groupRuns(blocks)
	if len(runs) != 1 || runs[0].start != 0 || runs[0].end != 3 {
		t.Fatalf("single-name should be one run [0,3); got %+v", runs)
	}
}
