package extract

import (
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/pdf/content"
)

// TestLowerPageKeepsAllBlocksWithTable is the regression for the "content dropped"
// bug: a page with several non-table blocks (a heading, a paragraph, a second
// heading) plus a lattice table plus a list must lower into a tree that keeps EVERY
// block, in reading order — not just the table, and not just the trailing list.
//
// The failure mode this guards against was two-fold: (1) the table box was hoisted
// to the front of the output rather than spliced in at its own vertical position,
// and (2) the list items were emitted as flat siblings of the headings/paragraphs,
// so the downstream Markdown/HTML writers (whose boxwalk.IsListContainer treats any
// box with a list-item child as a list) rendered only the list and silently dropped
// every other sibling. We assert both the ordering and that list items are nested
// under a list container so no sibling can be dropped.
func TestLowerPageKeepsAllBlocksWithTable(t *testing.T) {
	var g []glyph
	// Heading (24pt) at the top.
	g = append(g, sizedGlyphs("Report", 0, 20, 14, 24)...)
	// Body paragraph (10pt).
	g = append(g, sizedGlyphs("Intro paragraph text", 0, 60, 6, 10)...)
	// Second heading (24pt) below the paragraph.
	g = append(g, sizedGlyphs("Details", 0, 100, 14, 24)...)

	// A lattice table lower down: columns at x=0,50,100; rows at y=200,220,240.
	xs := []float64{0, 50, 100}
	ys := []float64{200, 220, 240}
	rules := gridRules(xs, ys)
	// Words in each cell (two rows × two cols). sizedGlyphs so they carry a size.
	g = append(g, sizedGlyphs("A", 20, 210, 6, 10)...)
	g = append(g, sizedGlyphs("B", 70, 210, 6, 10)...)
	g = append(g, sizedGlyphs("C", 20, 230, 6, 10)...)
	g = append(g, sizedGlyphs("D", 70, 230, 6, 10)...)

	// An ordered list below the table.
	g = append(g, sizedGlyphs("1.", 0, 300, 6, 10)...)
	g = append(g, sizedGlyphs("First item", 20, 300, 6, 10)...)
	g = append(g, sizedGlyphs("2.", 0, 320, 6, 10)...)
	g = append(g, sizedGlyphs("Second item", 20, 320, 6, 10)...)

	pc := &pageContent{glyphs: g, rules: rules, width: 600, height: 800}
	boxes := lowerPage(pc, nil)

	// Collect the visible text of every top-level box (recursively), plus a note of
	// which structural kinds appear, so we can assert nothing was dropped.
	var order []string
	var sawTable, sawListContainer bool
	for _, b := range boxes {
		switch {
		case b.Display == cssbox.DisplayTable:
			sawTable = true
			order = append(order, "TABLE")
		case isListContainerTest(b):
			sawListContainer = true
			order = append(order, "LIST:"+boxText(b))
		default:
			order = append(order, boxText(b))
		}
	}

	joined := strings.Join(order, " | ")
	// Every non-table block must survive.
	for _, want := range []string{"Report", "Intro paragraph text", "Details", "First item", "Second item"} {
		if !strings.Contains(joined, want) {
			t.Errorf("lowered output dropped %q; got: %s", want, joined)
		}
	}
	if !sawTable {
		t.Errorf("table box missing from lowered output; got: %s", joined)
	}
	if !sawListContainer {
		t.Errorf("list items were not nested under a list container (writers would drop siblings); got: %s", joined)
	}

	// Reading order: the two headings and paragraph precede the table, which precedes
	// the list.
	idx := func(sub string) int { return strings.Index(joined, sub) }
	inOrder := idx("Report") < idx("Intro paragraph text") &&
		idx("Intro paragraph text") < idx("Details") &&
		idx("Details") < idx("TABLE") &&
		idx("TABLE") < idx("First item")
	if !inOrder {
		t.Errorf("blocks out of reading order; got: %s", joined)
	}
}

// TestLowerListItemKeepsFullText is the regression for the "marker eats a letter"
// bug: the first content character of an ordered list item must survive. The item
// "1. Review ..." must lower to text "Review ..." (full word), with Marker "1. ".
func TestLowerListItemKeepsFullText(t *testing.T) {
	b := block{
		kind:   blockListItem,
		marker: "1. ",
		lines: []line{{
			y: 10, size: 10,
			words: []word{
				{text: "1.", x0: 0, x1: 8, y: 10, size: 10},
				{text: "Review", x0: 12, x1: 50, y: 10, size: 10},
				{text: "anomalies", x0: 54, x1: 100, y: 10, size: 10},
			},
		}},
	}
	box := lowerListItem(b)
	if box.Marker == nil || box.Marker.Text != "1. " {
		t.Fatalf("marker = %+v, want '1. '", box.Marker)
	}
	// The item text (after the marker child) must retain the full first word.
	text := boxText(box)
	if !strings.Contains(text, "Review") {
		t.Errorf("list item lost its leading letter: text = %q, want to contain %q", text, "Review")
	}
	if strings.Contains(text, "eview") && !strings.Contains(text, "Review") {
		t.Errorf("list item text %q has a truncated first word", text)
	}
}

// TestIsWhitespaceGlyphKeepsRemappedCode32 is the root-cause regression for the
// "marker eats a letter" bug: a subset font remaps byte code 32 to a real glyph
// (e.g. "R"). The interpreter still flags that glyph IsSpace (code 32 drives PDF
// word-spacing), but because it decodes to a visible rune it is real content and
// must NOT be treated as a whitespace word break (which would drop it).
func TestIsWhitespaceGlyphKeepsRemappedCode32(t *testing.T) {
	// A genuine space: flagged and decodes to a space rune.
	if !isWhitespaceGlyph(content.TextGlyph{Rune: ' ', IsSpace: true}) {
		t.Error("a real space glyph should be treated as whitespace")
	}
	// An unmapped glyph flagged as space (rune 0): still a break (degrade gracefully).
	if !isWhitespaceGlyph(content.TextGlyph{Rune: 0, IsSpace: true}) {
		t.Error("an unmapped code-32 glyph should be treated as whitespace")
	}
	// A subset font remapping code 32 to "R": flagged IsSpace but a visible letter —
	// must be kept as content.
	if isWhitespaceGlyph(content.TextGlyph{Rune: 'R', IsSpace: true}) {
		t.Error("code 32 remapped to a visible letter must NOT be dropped as whitespace")
	}
	// A never-flagged glyph is trivially not whitespace.
	if isWhitespaceGlyph(content.TextGlyph{Rune: 'e', IsSpace: false}) {
		t.Error("a non-space glyph must not be whitespace")
	}
}

// boxText concatenates every text leaf under b (space-joined, collapsed), for
// assertions that content survived lowering.
func boxText(b *cssbox.Box) string {
	var parts []string
	var walk func(*cssbox.Box)
	walk = func(n *cssbox.Box) {
		if n.Kind == cssbox.BoxText {
			if t := strings.TrimSpace(n.Text); t != "" {
				parts = append(parts, t)
			}
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(b)
	return strings.Join(parts, " ")
}

// isListContainerTest mirrors the writers' boxwalk.IsListContainer for test assertions:
// a box (not itself a list item) whose direct children include a list item.
func isListContainerTest(b *cssbox.Box) bool {
	if b.Display == cssbox.DisplayListItem {
		return false
	}
	for _, c := range b.Children {
		if c.Display == cssbox.DisplayListItem {
			return true
		}
	}
	return false
}
