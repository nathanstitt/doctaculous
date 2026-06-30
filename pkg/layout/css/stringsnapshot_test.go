package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// makeStringSetBlock builds a block fragment whose box sets string-set name→text and
// whose text leaf carries `text` (so content() resolves).
func makeStringSetBlock(name, text string, y float64) *Fragment {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	box.Style = gcss.ComputedStyle{StringSet: []gcss.StringSetEntry{{Name: name, UseContent: true}}}
	box.Children = []*cssbox.Box{{Kind: cssbox.BoxText, Text: text}}
	return &Fragment{Y: y, H: 10, Box: box}
}

func TestStringSnapshotPerPage(t *testing.T) {
	// Three blocks set `title` to A, B, C; buckets put A,B on page 0 and C on page 1.
	blocks := []*Fragment{
		makeStringSetBlock("title", "A", 0),
		makeStringSetBlock("title", "B", 20),
		makeStringSetBlock("title", "C", 40),
	}
	buckets := []pageBucket{
		{top: 0, blocks: blocks[:2]},
		{top: 40, blocks: blocks[2:]},
	}
	snaps := buildStringSnapshots(buckets)
	if got := snaps[0]["title"]; got != "B" {
		t.Errorf("page 0 title = %q, want B (last on page 0)", got)
	}
	if got := snaps[1]["title"]; got != "C" {
		t.Errorf("page 1 title = %q, want C", got)
	}
}

func TestStringSnapshotCarriesForward(t *testing.T) {
	// Page 0 sets title=A; page 1 sets nothing ⇒ title stays A (running value).
	blocks := []*Fragment{
		makeStringSetBlock("title", "A", 0),
		{Y: 20, H: 10, Box: &cssbox.Box{Kind: cssbox.BoxBlock}}, // no string-set
	}
	buckets := []pageBucket{
		{top: 0, blocks: blocks[:1]},
		{top: 20, blocks: blocks[1:]},
	}
	snaps := buildStringSnapshots(buckets)
	if got := snaps[1]["title"]; got != "A" {
		t.Errorf("page 1 title = %q, want A (carried forward)", got)
	}
}
