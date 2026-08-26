package svg

import (
	"reflect"
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
	// No id/class attributes: zero values, not empty non-nil slices.
	if len(root.kids) > 0 && root.kids[0].kids[0].id == "" && root.kids[0].kids[0].classes == nil {
		_ = 0 // structure asserted above; this documents the nil-vs-empty contract
	}
	plain, _ := parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`), nil)
	if r := plain.kids[0]; r.id != "" || r.classes != nil {
		t.Errorf("attribute-less element: id=%q classes=%v, want \"\" and nil", r.id, r.classes)
	}
}
