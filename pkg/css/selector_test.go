package css

import "testing"

func TestSelectorSpecificity(t *testing.T) {
	cases := []struct {
		in   string
		spec Specificity // {ids, classes, types}
	}{
		{"*", Specificity{0, 0, 0}},
		{"div", Specificity{0, 0, 1}},
		{".intro", Specificity{0, 1, 0}},
		{"#lead", Specificity{1, 0, 0}},
		{"div.intro", Specificity{0, 1, 1}},
		{"div p", Specificity{0, 0, 2}},
		{"#lead .intro p", Specificity{1, 1, 1}},
	}
	for _, c := range cases {
		sels := parseSelectorList(c.in)
		if len(sels) != 1 {
			t.Fatalf("parseSelectorList(%q) = %d selectors, want 1", c.in, len(sels))
		}
		if sels[0].Specificity() != c.spec {
			t.Fatalf("%q specificity = %v, want %v", c.in, sels[0].Specificity(), c.spec)
		}
	}
}

func TestParseSelectorGroup(t *testing.T) {
	sels := parseSelectorList("h1, h2, .title")
	if len(sels) != 3 {
		t.Fatalf("got %d selectors, want 3", len(sels))
	}
}

func TestSelectorMatch(t *testing.T) {
	// Tree: div#main > p.intro
	div := &fakeNode{tag: "div", id: "main"}
	p := &fakeNode{tag: "p", classes: []string{"intro"}, parent: div}

	mustMatch := func(sel string, n *fakeNode, want bool) {
		sels := parseSelectorList(sel)
		if len(sels) == 0 {
			t.Fatalf("%q: parseSelectorList returned no selectors", sel)
		}
		got := sels[0].Matches(n)
		if got != want {
			t.Fatalf("%q matches %s#%s.%v = %v, want %v", sel, n.tag, n.id, n.classes, got, want)
		}
	}
	mustMatch("p", p, true)
	mustMatch("p.intro", p, true)
	mustMatch("p.missing", p, false)
	mustMatch("div p", p, true)   // descendant
	mustMatch("#main p", p, true) // descendant via id
	mustMatch("p p", p, false)    // no matching ancestor
	mustMatch("div", p, false)    // subject must be the node itself
	mustMatch("*", p, true)
}

func TestSelectorMatchDescendantSkipsLevels(t *testing.T) {
	// Tree: html > div#main > span > p   (p is the subject)
	html := &fakeNode{tag: "html"}
	div := &fakeNode{tag: "div", id: "main", parent: html}
	span := &fakeNode{tag: "span", parent: div}
	p := &fakeNode{tag: "p", parent: span}

	check := func(sel string, want bool) {
		sels := parseSelectorList(sel)
		if len(sels) == 0 {
			t.Fatalf("%q: parseSelectorList returned no selectors", sel)
		}
		if got := sels[0].Matches(p); got != want {
			t.Errorf("%q matches p = %v, want %v", sel, got, want)
		}
	}
	check("div p", true)       // div is a non-direct ancestor (span between) — descendant skips span
	check("#main p", true)     // ancestor by id, two levels up
	check("html p", true)      // top-level ancestor, three levels up
	check("html div p", true)  // three-part chain, all ancestors present in order
	check("span div p", false) // order wrong: div is not a descendant-ancestor of span here
	check("div span", false)   // subject must be p, not span
}

// TestDeferredSelectorsDoNotMisMatch locks in the safe-degradation guarantee for
// selector forms this engine does not yet support (pseudo-classes, child and
// sibling combinators, pseudo-elements, attribute selectors). They must parse
// without panic and, crucially, must NOT match a plain element — degrading to an
// inert non-match rather than wrongly applying styles. If a future tokenizer
// change made ">" or ":" matchable, this test fails instead of silently
// mis-styling pages.
func TestDeferredSelectorsDoNotMisMatch(t *testing.T) {
	// A representative DOM: <ul><li class="x"></li></ul>
	ul := &fakeNode{tag: "ul"}
	li := &fakeNode{tag: "li", classes: []string{"x"}, parent: ul}

	deferred := []struct {
		sel  string
		node *fakeNode
	}{
		{"a:hover", &fakeNode{tag: "a"}},   // pseudo-class must not match a plain <a>
		{"li:first-child", li},             // pseudo-class
		{"ul > li", li},                    // child combinator must not match (only descendant is supported)
		{"li + li", li},                    // adjacent-sibling combinator
		{"li ~ li", li},                    // general-sibling combinator
		{"p::before", &fakeNode{tag: "p"}}, // pseudo-element
		{"li[data-x]", li},                 // attribute selector
	}
	for _, d := range deferred {
		sels := parseSelectorList(d.sel)
		// Parsing must not panic and must yield at most the parsed (garbage) selector;
		// the guarantee is that NONE of the produced selectors match the node.
		for _, s := range sels {
			if s.Matches(d.node) {
				t.Errorf("deferred selector %q wrongly matched <%s>; unsupported selectors must degrade to a non-match, not mis-apply styles", d.sel, d.node.tag)
			}
		}
	}
}

func TestParseSelectorListSkipsMalformed(t *testing.T) {
	// Malformed groups (empty qualifier names) are skipped; valid ones survive.
	sels := parseSelectorList("h1, ., #, p.intro")
	if len(sels) != 2 {
		t.Fatalf("got %d selectors, want 2 (h1 and p.intro; '.' and '#' dropped)", len(sels))
	}
	// Confirm the survivors are the right ones by specificity.
	if sels[0].Specificity() != (Specificity{0, 0, 1}) { // h1
		t.Errorf("sels[0] specificity = %v, want {0 0 1} (h1)", sels[0].Specificity())
	}
	if sels[1].Specificity() != (Specificity{0, 1, 1}) { // p.intro
		t.Errorf("sels[1] specificity = %v, want {0 1 1} (p.intro)", sels[1].Specificity())
	}
}
