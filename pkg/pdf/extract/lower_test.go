package extract

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// TestLowerPageShape drives lowerPage with a synthetic page (glyphs + rules built
// directly, no interpreter) and asserts the produced boxes match what the
// downstream Markdown/HTML writers expect: a heading box with SemTag/HeadingLvl, a
// paragraph box, a list-item box with a Marker, and a table box in the
// DisplayTable > Row > Cell shape.
func TestLowerPageShape(t *testing.T) {
	var g []glyph
	// A 24pt heading line at the top.
	g = append(g, sizedGlyphs("Title", 0, 60, 14, 24)...)
	// A 10pt paragraph line.
	g = append(g, sizedGlyphs("Some body text", 0, 100, 6, 10)...)
	// A bullet list item further down (large gap so it is its own block).
	g = append(g, mkGlyph('•', 0, 140, 6))
	g = append(g, sizedGlyphs("item one", 12, 140, 6, 10)...)

	pc := &pageContent{glyphs: g, width: 600, height: 800}
	boxes := lowerPage(pc, nil)
	if len(boxes) < 3 {
		t.Fatalf("got %d boxes, want >=3 (heading, paragraph, list item): %d", len(boxes), len(boxes))
	}

	// First box: heading h1.
	if boxes[0].SemTag != "h1" || boxes[0].HeadingLvl != 1 {
		t.Errorf("box0 = SemTag %q lvl %d, want h1/1", boxes[0].SemTag, boxes[0].HeadingLvl)
	}
	// A list-item box must exist with a marker. List items are nested under a
	// synthetic list container (so the downstream writers do not treat the whole body
	// as a list and drop non-item siblings), so search recursively.
	var foundList bool
	var walk func(b *cssbox.Box)
	walk = func(b *cssbox.Box) {
		if b.Display == cssbox.DisplayListItem {
			foundList = true
			if b.Marker == nil || b.Marker.Text != "- " {
				t.Errorf("list marker = %+v, want '- '", b.Marker)
			}
			return
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range boxes {
		walk(b)
	}
	if !foundList {
		t.Error("no DisplayListItem box produced")
	}
}

// TestLowerTableShape verifies a detected table lowers into the exact box shape the
// Markdown table writer's buildGrid consumes.
func TestLowerTableShape(t *testing.T) {
	xs := []float64{0, 50, 100}
	ys := []float64{0, 20, 40}
	rules := gridRules(xs, ys)
	lines := []line{
		{y: 10, x0: 0, x1: 100, words: []word{wordAt("A", 25, 10), wordAt("B", 75, 10)}},
		{y: 30, x0: 0, x1: 100, words: []word{wordAt("C", 25, 30), wordAt("D", 75, 30)}},
	}
	tbl := detect(lines, rules, nil)
	if tbl == nil {
		t.Fatal("detect returned nil")
	}
	box := lowerTable(tbl)
	if box.Display != cssbox.DisplayTable {
		t.Fatalf("table box display = %v, want DisplayTable", box.Display)
	}
	rows := 0
	for _, rowBox := range box.Children {
		if rowBox.Display != cssbox.DisplayTableRow {
			t.Fatalf("row box display = %v, want DisplayTableRow", rowBox.Display)
		}
		rows++
		for _, cellBox := range rowBox.Children {
			if cellBox.Display != cssbox.DisplayTableCell {
				t.Fatalf("cell box display = %v, want DisplayTableCell", cellBox.Display)
			}
			if cellBox.ColSpan < 1 || cellBox.RowSpan < 1 {
				t.Errorf("cell span = %dx%d, want >=1", cellBox.ColSpan, cellBox.RowSpan)
			}
		}
	}
	if rows != 2 {
		t.Errorf("table rows = %d, want 2", rows)
	}
}

// TestLowerNilDocument verifies Lower rejects a nil document rather than panicking.
func TestLowerNilDocument(t *testing.T) {
	if _, err := Lower(nil, nil); err == nil {
		t.Error("Lower(nil) = nil error, want an error")
	}
}

// sizedGlyphs lays out a string on one baseline with a fixed size, each glyph
// `advance` wide with no extra gap.
func sizedGlyphs(s string, x0, y, advance, size float64) []glyph {
	var out []glyph
	x := x0
	for _, r := range s {
		g := mkGlyph(r, x, y, advance)
		g.size = size
		out = append(out, g)
		x += advance
	}
	return out
}
