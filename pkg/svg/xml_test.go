package svg

import "testing"

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
