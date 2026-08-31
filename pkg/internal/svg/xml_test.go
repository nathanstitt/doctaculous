package svg

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseXML(t *testing.T) {
	src := []byte(`<?xml version="1.0"?>
<!-- c --><svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"
     width="10" viewBox="0 0 10 10">
  <g fill="red"><rect x="1" width="4" height="4"/></g>
  <a xlink:href="#x"/>
</svg>`)
	root, err := parseXML(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if root.local != "svg" || root.space != svgNS {
		t.Fatalf("root = %s:%s", root.space, root.local)
	}
	if root.attrs["width"] != "10" || root.attrs["viewBox"] != "0 0 10 10" {
		t.Errorf("attrs = %v", root.attrs)
	}
	if len(root.kids) != 2 || root.kids[0].local != "g" {
		t.Fatalf("kids = %+v", root.kids)
	}
	rect := root.kids[0].kids[0]
	if rect.local != "rect" || rect.attrs["x"] != "1" {
		t.Errorf("rect = %+v", rect)
	}
	if root.kids[1].attrs["href"] != "#x" {
		t.Errorf("xlink:href not folded: %v", root.kids[1].attrs)
	}

	// Foreign-namespace subtree is kept in the tree with its namespace intact
	// (the scene builder skips it); truncated input keeps the parsed prefix.
	var logged bool
	root, err = parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="3" height="3"/><g>`),
		func(string, ...any) { logged = true })
	if err != nil || len(root.kids) < 1 {
		t.Fatalf("truncated: %v %+v", err, root)
	}
	if !logged {
		t.Error("truncation not logged")
	}
	// No <svg> root at all is an error.
	if _, err := parseXML([]byte(`<html><body/></html>`), nil); err == nil {
		t.Error("non-svg accepted")
	}
	// viewBox attribute name is case-sensitive: viewbox must NOT match.
	root, _ = parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewbox="0 0 1 1"/>`), nil)
	if _, ok := root.attrs["viewBox"]; ok {
		t.Error("case-insensitive attr match")
	}
}

func TestElementParentAndClasses(t *testing.T) {
	root, err := parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg" id="root">
	  <g class="a  b"><rect id="r" class="c"/></g>
	</svg>`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if root.parent != nil {
		t.Error("root parent should be nil")
	}
	if root.id != "root" {
		t.Errorf("root id = %q", root.id)
	}
	g := root.kids[0]
	if g.parent != root {
		t.Error("g.parent != root")
	}
	if !reflect.DeepEqual(g.classes, []string{"a", "b"}) {
		t.Errorf("g classes = %v, want [a b] (split on runs of whitespace)", g.classes)
	}
	rect := g.kids[0]
	if rect.parent != g || rect.parent.parent != root {
		t.Error("rect parent chain broken")
	}
	if rect.id != "r" || !reflect.DeepEqual(rect.classes, []string{"c"}) {
		t.Errorf("rect id=%q classes=%v", rect.id, rect.classes)
	}
	plain, _ := parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`), nil)
	if r := plain.kids[0]; r.id != "" || r.classes != nil {
		t.Errorf("attribute-less element: id=%q classes=%v, want \"\" and nil", r.id, r.classes)
	}
	// A whitespace-only class attribute has no usable tokens, so it must be
	// treated the same as an absent class attribute: nil, not []string{}.
	blank, _ := parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect class="   "/></svg>`), nil)
	if r := blank.kids[0]; r.classes != nil {
		t.Errorf("whitespace-only class: classes=%#v, want nil", r.classes)
	}
}

// assertParentChain recursively verifies that every element in the tree
// rooted at e has a coherent parent pointer: e.parent is either nil (only
// permitted for the true root, indicated by want being nil) or exactly the
// element that holds e in its kids slice.
func assertParentChain(t *testing.T, e *element, want *element) {
	t.Helper()
	if e.parent != want {
		t.Errorf("element <%s> parent = %p, want %p", e.local, e.parent, want)
	}
	for _, k := range e.kids {
		assertParentChain(t, k, e)
	}
}

// TestTruncatedParentChain covers the truncation-recovery (unwind) attach
// path specifically: a document with unclosed tags never reaches the normal
// EndElement pop for its still-open elements, so this exercises the second
// of the two attachment points that must both backfill element.parent.
func TestTruncatedParentChain(t *testing.T) {
	var logged bool
	root, err := parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><g><rect/>`),
		func(string, ...any) { logged = true })
	if err != nil {
		t.Fatal(err)
	}
	if !logged {
		t.Error("truncation not logged")
	}
	if root == nil || root.local != "svg" {
		t.Fatalf("root = %+v", root)
	}
	if len(root.kids) != 1 || root.kids[0].local != "g" {
		t.Fatalf("root.kids = %+v", root.kids)
	}
	g := root.kids[0]
	if len(g.kids) != 1 || g.kids[0].local != "rect" {
		t.Fatalf("g.kids = %+v", g.kids)
	}
	assertParentChain(t, root, nil)
}

// TestDepthCapParentChain covers the other unwind trigger: nesting beyond
// maxElementDepth. The still-open elements on the stack are attached via the
// same unwind path as truncation, so the parent chain must stay coherent
// down to whatever depth was actually reached before the cap tripped.
func TestDepthCapParentChain(t *testing.T) {
	var opening strings.Builder
	opening.WriteString(`<svg xmlns="http://www.w3.org/2000/svg">`)
	for i := 0; i < maxElementDepth+10; i++ {
		opening.WriteString("<g>")
	}

	var loggedCap bool
	root, err := parseXML([]byte(opening.String()), func(f string, a ...any) {
		if strings.Contains(f, "nesting exceeds") {
			loggedCap = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !loggedCap {
		t.Error("expected depth cap to be logged")
	}
	assertParentChain(t, root, nil)

	depth := 0
	for n := root; len(n.kids) > 0; n = n.kids[0] {
		depth++
	}
	if depth == 0 || depth >= maxElementDepth+10 {
		t.Errorf("depth = %d, want a value truncated below the %d opened tags", depth, maxElementDepth+10)
	}
}
