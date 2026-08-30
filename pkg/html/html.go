// Package html is the HTML frontend: it parses HTML bytes (via
// golang.org/x/net/html) into an owned, read-only DOM that implements the
// pkg/css Node interface, and collects the stylesheets the cascade needs
// (<style> contents, <link rel=stylesheet> hrefs, and inline style=""). It does
// no layout and no rendering; box generation (pkg/layout/css) consumes its
// output.
package html

import (
	"bytes"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/nathanstitt/omnidoc/pkg/css"
)

// Document is the result of parsing an HTML document: the owned DOM root plus the
// stylesheets discovered while walking it. It is read-only after Parse.
type Document struct {
	// Root is the <html> element.
	Root *Element
	// StyleSheets are parsed <style> contents in document order (order is a
	// cascade tie-breaker).
	StyleSheets []css.Stylesheet
	// LinkRefs are the hrefs of <link rel=stylesheet>, unresolved. Box generation
	// resolves them through a resource.ResourceLoader.
	LinkRefs []string
	// AuthorSheets are every author-origin stylesheet the document resolved to,
	// in document order — StyleSheets plus the fetched LinkRefs. It is filled in by
	// box generation (which is where <link> hrefs are actually loaded), and exists so
	// the layout engine can cascade the page's CSS into inline <svg> content, which
	// pkg/svg re-parses from serialized markup and would otherwise never see.
	AuthorSheets []css.Stylesheet
}

// Parse parses HTML bytes into an owned DOM Document. It is total on the kinds of
// malformed input x/net/html recovers from (unclosed tags, stray text): such
// input yields a valid-but-quirky tree, never a panic. An error is returned only
// for input the underlying parser cannot read at all.
func Parse(data []byte) (*Document, error) {
	root, err := xhtml.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	doc := &Document{}
	htmlNode := findElement(root, "html")
	if htmlNode == nil {
		// x/net/html always synthesizes <html>, but guard anyway.
		doc.Root = &Element{tag: "html"}
		return doc, nil
	}
	doc.Root = buildElement(htmlNode, nil, doc)
	return doc, nil
}

// buildElement converts an x/net/html element node (and its subtree) into an
// owned *Element, collecting stylesheets/links into doc as it goes.
func buildElement(n *xhtml.Node, parent *Element, doc *Document) *Element {
	el := &Element{
		tag:    n.Data, // x/net/html lowercases HTML tag names
		parent: parent,
		attrs:  make(map[string]string, len(n.Attr)),
	}
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		el.attrs[key] = a.Val
		switch key {
		case "id":
			el.id = a.Val
		case "class":
			el.classes = strings.Fields(a.Val)
		}
	}

	// Foreign content: x/net/html fully implements the HTML5 foreign-content
	// rules, so an inline <svg> arrives as a preserved subtree tagged with
	// Namespace "svg" and its camelCase names already repaired. Capture it as
	// markup and STOP: its children are SVG elements, not HTML ones, and walking
	// them would generate meaningless HTML boxes for <circle>/<path>.
	if n.Namespace == svgNamespace && el.tag == "svg" {
		el.foreignSrc = serializeForeign(n)
		return el
	}

	switch el.tag {
	case "style":
		if sheetSrc := textContent(n); strings.TrimSpace(sheetSrc) != "" {
			doc.StyleSheets = append(doc.StyleSheets, css.Parse(sheetSrc))
		}
	case "link":
		if strings.EqualFold(el.attrs["rel"], "stylesheet") {
			if href := el.attrs["href"]; href != "" {
				doc.LinkRefs = append(doc.LinkRefs, href)
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case xhtml.ElementNode:
			el.children = append(el.children, buildElement(c, el, doc))
		case xhtml.TextNode:
			el.children = append(el.children, &Text{Data: c.Data, parent: el})
		}
	}
	return el
}

// svgNamespace is the namespace x/net/html tags SVG foreign content with. It is
// the short token the parser uses internally ("svg"), not the namespace URI.
const svgNamespace = "svg"

// svgNamespaceURI is the SVG namespace URI, added to a re-serialized subtree when
// the source markup omitted it. Inline SVG in HTML needs no xmlns (the HTML
// parser infers the namespace from foreign-content rules), but pkg/svg is an XML
// parser and rejects a root that is not <svg> in this namespace — so the
// declaration has to be reinstated on the way out.
const svgNamespaceURI = "http://www.w3.org/2000/svg"

// xlinkNamespaceURI is the XLink namespace URI, added when the subtree uses an
// xlink:-prefixed attribute (legacy <use xlink:href>) without declaring it. An
// undeclared prefix makes the whole document a hard XML syntax error, so a
// perfectly ordinary inline <use xlink:href="#id"> would otherwise fail to parse.
const xlinkNamespaceURI = "http://www.w3.org/1999/xlink"

// serializeForeign re-serializes a foreign-content (SVG) subtree to markup that
// pkg/svg can parse, reinstating the xmlns/xmlns:xlink declarations HTML inline
// SVG is allowed to omit. Returns "" if the subtree cannot be rendered, so the
// caller degrades to an empty foreign source rather than malformed markup.
//
// x/net/html's renderer writes tag and attribute names exactly as the node holds
// them, and the parser's foreign-content adjustments already restored the SVG
// spellings, so camelCase survives this round trip unchanged.
func serializeForeign(n *xhtml.Node) string {
	clone := reinstateNamespaces(n)
	var buf bytes.Buffer
	if err := xhtml.Render(&buf, clone); err != nil {
		return ""
	}
	return buf.String()
}

// reinstateNamespaces returns n with any missing xmlns / xmlns:xlink declaration
// added. Only the ROOT node is copied (a shallow copy sharing the child list), so
// the subtree itself is never mutated and the owned DOM stays independent of the
// x/net/html tree it was built from.
func reinstateNamespaces(n *xhtml.Node) *xhtml.Node {
	var hasXMLNS, hasXlinkDecl bool
	for _, a := range n.Attr {
		switch {
		case a.Namespace == "" && a.Key == "xmlns":
			hasXMLNS = true
		case a.Namespace == "xmlns" && a.Key == "xlink":
			hasXlinkDecl = true
		}
	}
	needXlink := !hasXlinkDecl && usesXlink(n)
	if hasXMLNS && !needXlink {
		return n
	}

	clone := *n
	// The clone must not be linked into the original tree: Render walks Parent
	// only for the plaintext-element check, but a clone with live sibling links
	// would render its siblings too.
	clone.Parent, clone.PrevSibling, clone.NextSibling = nil, nil, nil
	clone.Attr = make([]xhtml.Attribute, 0, len(n.Attr)+2)
	if !hasXMLNS {
		clone.Attr = append(clone.Attr, xhtml.Attribute{Key: "xmlns", Val: svgNamespaceURI})
	}
	if needXlink {
		clone.Attr = append(clone.Attr, xhtml.Attribute{Namespace: "xmlns", Key: "xlink", Val: xlinkNamespaceURI})
	}
	clone.Attr = append(clone.Attr, n.Attr...)
	return &clone
}

// usesXlink reports whether any element in the subtree carries an xlink:-prefixed
// attribute, so the declaration is added only when it is actually needed (keeping
// the serialized markup byte-identical to the source for the common case).
func usesXlink(n *xhtml.Node) bool {
	if n.Type == xhtml.ElementNode {
		for _, a := range n.Attr {
			if a.Namespace == "xlink" {
				return true
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if usesXlink(c) {
			return true
		}
	}
	return false
}

// findElement returns the first element node with the given (lowercased) tag in a
// depth-first walk of an x/net/html tree.
func findElement(n *xhtml.Node, tag string) *xhtml.Node {
	if n.Type == xhtml.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

// textContent returns the concatenated text of an element's direct text children
// (sufficient for <style>, whose content is a single text node).
func textContent(n *xhtml.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}
