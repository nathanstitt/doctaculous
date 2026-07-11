package css

import "testing"

// sibNode is a minimal Node with a real child list, implementing SiblingIndexer
// the way a real DOM does: positions count element children only.
type sibNode struct {
	tag      string
	parent   *sibNode
	children []*sibNode
}

func (n *sibNode) Tag() string                { return n.tag }
func (n *sibNode) ID() string                 { return "" }
func (n *sibNode) Classes() []string          { return nil }
func (n *sibNode) Attr(string) (string, bool) { return "", false }
func (n *sibNode) Parent() Node {
	if n.parent == nil {
		return nil
	}
	return n.parent
}

func (n *sibNode) SiblingIndex() (pos, last, typePos, typeLast int) {
	if n.parent == nil {
		return 1, 1, 1, 1
	}
	total, typeTotal := 0, 0
	for _, c := range n.parent.children {
		total++
		if c.tag == n.tag {
			typeTotal++
		}
		if c == n {
			pos, typePos = total, typeTotal
		}
	}
	return pos, total - pos + 1, typePos, typeTotal - typePos + 1
}

// family builds a parent with one child element per tag and returns the children.
func family(tags ...string) []*sibNode {
	p := &sibNode{tag: "parent"}
	for _, tag := range tags {
		c := &sibNode{tag: tag, parent: p}
		p.children = append(p.children, c)
	}
	return p.children
}

func TestParseANpB(t *testing.T) {
	cases := []struct {
		src  string
		a, b int
		ok   bool
	}{
		{"even", 2, 0, true},
		{"odd", 2, 1, true},
		{"5", 0, 5, true},
		{"-3", 0, -3, true},
		{"n", 1, 0, true},
		{"+n", 1, 0, true},
		{"-n", -1, 0, true},
		{"2n", 2, 0, true},
		{"2n+1", 2, 1, true},
		{"2n + 1", 2, 1, true},
		{"3n-2", 3, -2, true},
		{"-n+3", -1, 3, true},
		{" ODD ", 2, 1, true},
		{"", 0, 0, false},
		{"x", 0, 0, false},
		{"2n+", 0, 0, false},
		{"2n+-3", 0, 0, false},
		{"n+n", 0, 0, false},
	}
	for _, c := range cases {
		a, b, ok := parseANpB(c.src)
		if ok != c.ok || (ok && (a != c.a || b != c.b)) {
			t.Errorf("parseANpB(%q) = (%d,%d,%v), want (%d,%d,%v)", c.src, a, b, ok, c.a, c.b, c.ok)
		}
	}
}

func TestStructuralPseudoClasses(t *testing.T) {
	// <parent><li/><li/><li/><em/><li/></parent>
	kids := family("li", "li", "li", "em", "li")
	match := func(sel string, n Node) bool {
		t.Helper()
		sels := parseSelectorList(sel)
		if len(sels) != 1 {
			t.Fatalf("parseSelectorList(%q) yielded %d selectors, want 1", sel, len(sels))
		}
		return sels[0].Matches(n)
	}
	cases := []struct {
		sel  string
		node int // index into kids
		want bool
	}{
		{"li:nth-child(1)", 0, true},
		{"li:nth-child(2)", 0, false},
		{"li:nth-child(2)", 1, true},
		{"li:nth-child(even)", 1, true},
		{"li:nth-child(even)", 2, false},
		{"li:nth-child(odd)", 2, true},
		{"li:nth-child(2n+1)", 4, true}, // position 5
		{"li:nth-child(-n+3)", 2, true},
		{"li:nth-child(-n+3)", 4, false},
		{"li:nth-last-child(1)", 4, true},
		{"li:nth-last-child(1)", 3, false},
		{"em:nth-of-type(1)", 3, true},
		{"li:nth-of-type(4)", 4, true}, // 4th li, though 5th child
		{"li:nth-last-of-type(1)", 4, true},
		{"li:first-child", 0, true},
		{"li:first-child", 1, false},
		{"li:last-child", 4, true},
		{"em:last-child", 3, false},
		{"em:first-of-type", 3, true},
		{"em:last-of-type", 3, true},
		{"em:only-of-type", 3, true},
		{"li:only-of-type", 0, false},
		{"li:only-child", 0, false},
	}
	for _, c := range cases {
		if got := match(c.sel, kids[c.node]); got != c.want {
			t.Errorf("%q on child %d = %v, want %v", c.sel, c.node, got, c.want)
		}
	}

	// A lone child matches :only-child and :only-of-type.
	only := family("p")
	for _, sel := range []string{"p:only-child", "p:only-of-type", "p:first-child", "p:last-child"} {
		if !match(sel, only[0]) {
			t.Errorf("%q should match a lone child", sel)
		}
	}
}

func TestStructuralPseudoSpecificityAndFallbacks(t *testing.T) {
	// :nth-child counts at the class level.
	sels := parseSelectorList("tr:nth-child(2)")
	if len(sels) != 1 {
		t.Fatalf("got %d selectors, want 1", len(sels))
	}
	if got := sels[0].Specificity(); got != (Specificity{0, 1, 1}) {
		t.Errorf("specificity = %v, want {0 1 1}", got)
	}

	// A Node that does not implement SiblingIndexer never matches a structural
	// pseudo-class (inert, not an error).
	plain := &fakeNode{tag: "li"}
	for _, sel := range []string{"li:first-child", "li:nth-child(1)"} {
		for _, s := range parseSelectorList(sel) {
			if s.Matches(plain) {
				t.Errorf("%q matched a Node without SiblingIndexer", sel)
			}
		}
	}

	// Unsupported functionals and malformed arguments still drop the selector.
	for _, sel := range []string{":not(.x)", "li:nth-child(", "li:nth-child(2x)", "li:is(a)"} {
		for _, s := range parseSelectorList(sel) {
			if s.Matches(&fakeNode{tag: "li"}) {
				t.Errorf("unsupported/malformed %q must not match", sel)
			}
		}
	}
}
