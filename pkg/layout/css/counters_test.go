package css

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func buildTree(t *testing.T, src string) *cssbox.Box {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return root
}

// markers returns the marker text of every list-item box, in document order.
func markers(b *cssbox.Box) []string {
	var out []string
	var walk func(x *cssbox.Box)
	walk = func(x *cssbox.Box) {
		if x == nil {
			return
		}
		if x.Display == cssbox.DisplayListItem && x.Marker != nil {
			out = append(out, x.Marker.Text)
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(b)
	return out
}

func TestListItemMarkers(t *testing.T) {
	// Unordered list → disc bullets.
	ul := markers(buildTree(t, "<body><ul><li>a</li><li>b</li><li>c</li></ul></body>"))
	if len(ul) != 3 || ul[0] != "• " {
		t.Errorf("ul markers = %q, want three '• '", ul)
	}
	// Ordered list → 1. 2. 3.
	ol := markers(buildTree(t, "<body><ol><li>a</li><li>b</li><li>c</li></ol></body>"))
	want := []string{"1. ", "2. ", "3. "}
	for i, m := range ol {
		if m != want[i] {
			t.Errorf("ol marker %d = %q, want %q", i, m, want[i])
		}
	}
	// Two sibling ordered lists each restart at 1.
	two := markers(buildTree(t, "<body><ol><li>a</li><li>b</li></ol><ol><li>x</li></ol></body>"))
	if len(two) != 3 || two[0] != "1. " || two[1] != "2. " || two[2] != "1. " {
		t.Errorf("sibling ol markers = %q, want 1.,2.,1.", two)
	}
	// list-style-type:none → no marker.
	none := markers(buildTree(t, `<body><ul style="list-style-type:none"><li>a</li></ul></body>`))
	if len(none) != 0 {
		t.Errorf("list-style:none markers = %q, want none", none)
	}
	// upper-roman ordered list.
	rom := markers(buildTree(t, `<body><ol style="list-style-type:upper-roman"><li>a</li><li>b</li><li>c</li><li>d</li></ol></body>`))
	if len(rom) != 4 || rom[3] != "IV. " {
		t.Errorf("roman markers = %q, want ...IV.", rom)
	}
}

// Whitespace (newlines/indentation) between <li> elements must NOT restart the list
// numbering. A text node inherits the parent <ol>'s style, which carries the UA
// counter-reset: list-item; if that reset is honored on the text node, every item
// restarts at 1. The list counter must accumulate across the whitespace siblings.
func TestListNumberingIgnoresInterItemWhitespace(t *testing.T) {
	got := markers(buildTree(t, "<body><ol>\n  <li>a</li>\n  <li>b</li>\n  <li>c</li>\n</ol></body>"))
	want := []string{"1. ", "2. ", "3. "}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("spaced ol markers = %q, want %q (restart-per-item if 1,1,1)", got, want)
	}
}

// content: counters(item, ".") on nested ordered lists joins the scope chain.
func TestCountersNestedJoin(t *testing.T) {
	// A counter-reset on a box opens a new scope for its descendants; counters() joins
	// all active values up the chain (nested-list numbering 1, 1.1).
	tree := buildTree(t, `<head><style>
	.l { counter-reset: c }
	.i { counter-increment: c }
	.n { content: counters(c, ".") }
	</style></head><body><div class="l"><div class="i"><span class="n"></span><div class="l"><div class="i"><span class="n"></span></div></div></div></div></body>`)
	var texts []string
	var walk func(x *cssbox.Box)
	walk = func(x *cssbox.Box) {
		if x == nil {
			return
		}
		if len(x.Style.Content) > 0 {
			for _, c := range x.Children {
				if c.Kind == cssbox.BoxText {
					texts = append(texts, c.Text)
					break
				}
			}
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(tree)
	if len(texts) != 2 || texts[0] != "1" || texts[1] != "1.1" {
		t.Errorf("counters() join = %q, want [1 1.1]", texts)
	}
}

// generatedTexts returns the text of the first synthetic child of every box carrying a
// `content` value, in document order — i.e. the rendered counter()/counters() output.
func generatedTexts(root *cssbox.Box) []string {
	var texts []string
	var walk func(x *cssbox.Box)
	walk = func(x *cssbox.Box) {
		if x == nil {
			return
		}
		if len(x.Style.Content) > 0 {
			for _, c := range x.Children {
				if c.Kind == cssbox.BoxText {
					texts = append(texts, c.Text)
					break
				}
			}
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(root)
	return texts
}

// An element that both increments a counter AND renders it via `content` must count
// only once: the synthetic content child must not re-apply the parent's counter ops.
func TestCounterIncrementWithContentCountsOnce(t *testing.T) {
	tree := buildTree(t, `<head><style>
	.wrap { counter-reset: c }
	.item { counter-increment: c; content: counter(c) }
	</style></head><body><div class="wrap">`+
		`<div class="item"></div><div class="item"></div><div class="item"></div>`+
		`</div></body>`)
	got := generatedTexts(tree)
	want := []string{"1", "2", "3"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("counter with content = %q, want %q (double-increment if 2,4,6)", got, want)
	}
}

// A counter-reset's scope reaches the resetting element's FOLLOWING SIBLINGS (CSS Lists
// §4.4): a flat counter-reset on one element must be visible to later siblings that
// increment/read it — the classic <h2 counter-reset> / <h3 counter-increment> pattern.
func TestCounterResetVisibleToFollowingSiblings(t *testing.T) {
	tree := buildTree(t, `<head><style>
	.reset { counter-reset: c 5 }
	.show  { content: counter(c) }
	.inc   { counter-increment: c }
	</style></head><body>`+
		`<div class="reset"></div>`+ // c := 5, scoped to following siblings
		`<div class="inc"></div>`+ // c := 6
		`<div class="show"></div>`+ // reads 6
		`</body>`)
	got := generatedTexts(tree)
	if len(got) != 1 || got[0] != "6" {
		t.Errorf("following-sibling reset = %q, want [6] (got fresh 1 if scope popped early)", got)
	}
}
