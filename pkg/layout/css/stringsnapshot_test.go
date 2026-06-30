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
	if got := snaps[0].Last["title"]; got != "B" {
		t.Errorf("page 0 title = %q, want B (last on page 0)", got)
	}
	if got := snaps[1].Last["title"]; got != "C" {
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
	if got := snaps[1].Last["title"]; got != "A" {
		t.Errorf("page 1 title = %q, want A (carried forward)", got)
	}
}

func TestStringSnapshotFirstStart(t *testing.T) {
	// Page 0 sets title to A then B (two setters); page 1 sets nothing; page 2 sets C.
	a := makeStringSetBlock("title", "A", 0)
	b := makeStringSetBlock("title", "B", 20)
	c := makeStringSetBlock("title", "C", 60)
	buckets := []pageBucket{
		{top: 0, blocks: []*Fragment{a, b}},
		{top: 40, blocks: []*Fragment{{Y: 40, H: 10, Box: &cssbox.Box{Kind: cssbox.BoxBlock}}}},
		{top: 60, blocks: []*Fragment{c}},
	}
	snaps := buildStringSnapshots(buckets)
	// Page 0: start "" , first "A", last "B".
	if snaps[0].stringValue("title", "first") != "A" {
		t.Errorf("p0 first = %q, want A", snaps[0].stringValue("title", "first"))
	}
	if snaps[0].stringValue("title", "") != "B" {
		t.Errorf("p0 last = %q, want B", snaps[0].stringValue("title", ""))
	}
	if snaps[0].stringValue("title", "start") != "" {
		t.Errorf("p0 start = %q, want empty", snaps[0].stringValue("title", "start"))
	}
	// Page 1: no setter -> first falls back to carried B, last B, start B.
	if snaps[1].stringValue("title", "first") != "B" {
		t.Errorf("p1 first = %q, want B (carried)", snaps[1].stringValue("title", "first"))
	}
	if snaps[1].stringValue("title", "first-except") != "" {
		t.Errorf("p1 first-except = %q, want empty (no setter)", snaps[1].stringValue("title", "first-except"))
	}
	if snaps[1].stringValue("title", "start") != "B" {
		t.Errorf("p1 start = %q, want B", snaps[1].stringValue("title", "start"))
	}
	// Page 2: start B, first C, last C.
	if snaps[2].stringValue("title", "start") != "B" {
		t.Errorf("p2 start = %q, want B", snaps[2].stringValue("title", "start"))
	}
	if snaps[2].stringValue("title", "first") != "C" {
		t.Errorf("p2 first = %q, want C", snaps[2].stringValue("title", "first"))
	}
}
