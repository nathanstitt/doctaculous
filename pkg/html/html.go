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

	"github.com/nathanstitt/doctaculous/pkg/css"
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
