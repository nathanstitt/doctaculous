package html

import (
	"strings"
	"testing"
)

// findByTag returns the first descendant element with the given tag, or nil.
func findByTag(e *Element, tag string) *Element {
	if e == nil {
		return nil
	}
	if e.Tag() == tag {
		return e
	}
	for _, c := range e.Children() {
		if child, ok := c.(*Element); ok {
			if f := findByTag(child, tag); f != nil {
				return f
			}
		}
	}
	return nil
}

// parseSVGElement parses HTML and returns the <svg> element from the owned DOM.
func parseSVGElement(t *testing.T, src string) *Element {
	t.Helper()
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	el := findByTag(doc.Root, "svg")
	if el == nil {
		t.Fatal("no <svg> element in the owned DOM")
	}
	return el
}

// TestInlineSVGCamelCaseSurvivesTheRoundTrip is the load-bearing test for the
// re-serialize decision. HTML5 tokenization lower-cases every tag and attribute
// name in foreign content, and the tree construction stage then REPAIRS the SVG
// spellings from its adjustment tables (clippath -> clipPath, lineargradient ->
// linearGradient, gradientunits -> gradientUnits, viewbox -> viewBox, ...).
//
// pkg/svg is a case-SENSITIVE XML parser. If those repairs were lost on the way
// back out to markup, it would see <clippath> and <lineargradient>, fail to match
// them as gradient/clip definitions, and quietly render a picture with every
// gradient and clip missing — with no error, anywhere. So the round trip is
// pinned here at the layer that performs it.
func TestInlineSVGCamelCaseSurvivesTheRoundTrip(t *testing.T) {
	// Written in lower case on purpose, as an HTML author legitimately may: it is
	// the HTML parser that restores the canonical spellings.
	el := parseSVGElement(t, `<body><svg viewbox="0 0 10 10">`+
		`<defs>`+
		`<lineargradient id="g" gradientunits="userSpaceOnUse" x1="0"><stop offset="0"/></lineargradient>`+
		`<radialgradient id="r" gradienttransform="scale(2)"/>`+
		`<clippath id="c" clippathunits="userSpaceOnUse"><rect width="5" height="5"/></clippath>`+
		`<textpath href="#p"/>`+
		`<foreignobject/>`+
		`</defs>`+
		`<text textlength="3" lengthadjust="spacing">x</text>`+
		`<feGaussianBlur stddeviation="2"/>`+
		`</svg></body>`)

	src, ok := el.ForeignSource()
	if !ok {
		t.Fatal("ForeignSource() reported no foreign content for an inline <svg>")
	}

	for _, want := range []string{
		"viewBox=", "<linearGradient", "gradientUnits=", "<radialGradient",
		"gradientTransform=", "<clipPath", "clipPathUnits=", "<textPath",
		"<foreignObject", "textLength=", "lengthAdjust=", "stdDeviation=",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("re-serialized markup is missing the camelCase form %q; the HTML "+
				"parser's foreign-content name repair was lost.\ngot: %s", want, src)
		}
	}
	// The lower-cased spellings must be GONE, not merely accompanied. A parser
	// that emitted both would still match, so assert the negative too.
	for _, gone := range []string{"<lineargradient", "<clippath", "gradientunits=", "textlength="} {
		if strings.Contains(src, gone) {
			t.Errorf("re-serialized markup still contains the lower-cased form %q\ngot: %s", gone, src)
		}
	}
}

// TestInlineSVGSourceGetsAnXMLNSDeclaration proves the namespace declaration is
// reinstated. Inline SVG in HTML is allowed to omit xmlns entirely (the HTML
// parser infers the namespace from foreign-content rules), but pkg/svg is an XML
// parser and rejects a root that is not <svg> in the SVG namespace — so without
// this, the overwhelmingly common hand-written inline <svg> would never parse.
func TestInlineSVGSourceGetsAnXMLNSDeclaration(t *testing.T) {
	el := parseSVGElement(t, `<body><svg width="10" height="10"><circle r="5"/></svg></body>`)
	src, _ := el.ForeignSource()
	if !strings.Contains(src, `xmlns="http://www.w3.org/2000/svg"`) {
		t.Errorf("no xmlns declaration added; pkg/svg would reject this as not-SVG:\n%s", src)
	}
}

// TestInlineSVGSourceKeepsAnExistingXMLNS proves the declaration is added only
// when missing, so markup that already carries one is not given a duplicate
// attribute (which is an XML well-formedness error).
func TestInlineSVGSourceKeepsAnExistingXMLNS(t *testing.T) {
	el := parseSVGElement(t, `<body><svg xmlns="http://www.w3.org/2000/svg" width="10"><circle r="5"/></svg></body>`)
	src, _ := el.ForeignSource()
	if n := strings.Count(src, "xmlns="); n != 1 {
		t.Errorf("got %d xmlns declarations, want exactly 1:\n%s", n, src)
	}
}

// TestInlineSVGSourceDeclaresXlinkWhenUsed proves an xlink:-prefixed attribute
// gets its prefix declared. An undeclared namespace prefix is a hard XML syntax
// error, so a perfectly ordinary legacy <use xlink:href="#id"> would otherwise
// make the whole inline SVG fail to parse.
func TestInlineSVGSourceDeclaresXlinkWhenUsed(t *testing.T) {
	el := parseSVGElement(t, `<body><svg width="10"><use xlink:href="#a"/></svg></body>`)
	src, _ := el.ForeignSource()
	if !strings.Contains(src, `xmlns:xlink="http://www.w3.org/1999/xlink"`) {
		t.Errorf("xlink: prefix used but not declared; the markup is not well-formed XML:\n%s", src)
	}
	if !strings.Contains(src, "xlink:href=") {
		t.Errorf("the xlink:href attribute did not survive re-serialization:\n%s", src)
	}
}

// TestInlineSVGSourceOmitsXlinkWhenUnused keeps the serialized markup as close to
// the source as possible: no gratuitous declaration on the overwhelming majority
// of SVG that never uses the legacy prefix.
func TestInlineSVGSourceOmitsXlinkWhenUnused(t *testing.T) {
	el := parseSVGElement(t, `<body><svg width="10"><use href="#a"/></svg></body>`)
	src, _ := el.ForeignSource()
	if strings.Contains(src, "xmlns:xlink") {
		t.Errorf("added an xlink declaration to markup that never uses it:\n%s", src)
	}
}

// TestInlineSVGChildrenAreNotOwnedDOMElements proves box generation cannot
// recurse into the SVG: the owned Element is a LEAF. Before this change,
// buildElement ignored Namespace entirely and walked <circle>/<path> into the
// owned tree, where they generated meaningless HTML block boxes.
func TestInlineSVGChildrenAreNotOwnedDOMElements(t *testing.T) {
	el := parseSVGElement(t, `<body><svg width="10"><circle r="5"/><path d="M0 0"/><text>hi</text></svg></body>`)
	if n := len(el.Children()); n != 0 {
		t.Errorf("the <svg> element has %d owned children, want 0 (its subtree is SVG, not HTML)", n)
	}
	if findByTag(el, "circle") != nil || findByTag(el, "path") != nil {
		t.Error("SVG children leaked into the owned HTML DOM")
	}
}

// TestInlineSVGStyleIsNotCollectedAsAHostStylesheet proves an SVG-internal
// <style> stays inside the SVG rather than being hoisted into the HOST
// document's cascade — where its selectors would match HTML elements and restyle
// the page. pkg/svg runs its own cascade over the subtree, so the rules are not
// lost; they are just scoped correctly.
func TestInlineSVGStyleIsNotCollectedAsAHostStylesheet(t *testing.T) {
	doc, err := Parse([]byte(`<body><p>hi</p><svg><style>rect { fill: red } p { color: lime }</style><rect/></svg></body>`))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(doc.StyleSheets); n != 0 {
		t.Errorf("collected %d host stylesheets from inside an inline <svg>; SVG-internal "+
			"rules must not enter the HTML cascade", n)
	}
	src, _ := findByTag(doc.Root, "svg").ForeignSource()
	if !strings.Contains(src, "rect { fill: red }") {
		t.Errorf("the SVG's own <style> did not survive re-serialization:\n%s", src)
	}
}

// TestOrdinaryElementsHaveNoForeignSource pins the negative: nothing about
// ordinary HTML changes. ForeignSource is a foreign-content-only signal, so every
// existing element continues to generate boxes exactly as before.
func TestOrdinaryElementsHaveNoForeignSource(t *testing.T) {
	doc, err := Parse([]byte(`<body><p>hi</p><div><img src="a.png"></div></body>`))
	if err != nil {
		t.Fatal(err)
	}
	var check func(*Element)
	check = func(e *Element) {
		if src, ok := e.ForeignSource(); ok {
			t.Errorf("<%s> reported foreign source %q", e.Tag(), src)
		}
		for _, c := range e.Children() {
			if child, isEl := c.(*Element); isEl {
				check(child)
			}
		}
	}
	check(doc.Root)
}
