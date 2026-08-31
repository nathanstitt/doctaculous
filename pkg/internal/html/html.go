// Package html is the HTML frontend: it parses HTML bytes (via
// golang.org/x/net/html) into an owned, read-only DOM that implements the
// pkg/css Node interface, and collects the stylesheets the cascade needs
// (<style> contents, <link rel=stylesheet> hrefs, and inline style=""). It does
// no layout and no rendering; box generation (pkg/layout/css) consumes its
// output.
package html

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/nathanstitt/omnidoc/pkg/internal/css"
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

// maxNestingDepth bounds how deeply tags may nest before Parse refuses the
// document.
//
// The bound originally existed because the cost was in the dependency, not here.
// x/net/html resolved a close tag with indexOfElementInScope, which scans the
// open-element stack linearly, making deep nesting quadratic in the number of
// open elements. Measured inside xhtml.Parse at the time: 30,000 nested <div>
// took 3.7s and 60,000 took 15.1s (4x the time for 2x the depth), against ~10ms
// for this package's own walk of the resulting tree. At 200,000 it did not
// finish. That was a denial of service reachable from a ~1 MB file, and it
// happened before this package got control, so it could not be bounded after the
// fact -- only by declining the input.
//
// UPSTREAM HAS SINCE FIXED IT. The quadratic blowup is GO-2026-4440, fixed in
// x/net v0.45.0, and the fix has the same shape as this one: parse.go's
// insertOpenElement panics past 512 open elements, which xhtml.Parse recovers
// into an error. The DoS this guard was built for is gone.
//
// The guard stays, matched to upstream's 512, for two reasons. It keeps
// ErrTooDeeplyNested as the answer a caller gets, rather than an opaque string
// from a dependency that a future version may reword. And it keeps the rejection
// cheap: nestingWithinLimit is a byte scan over the source, so an abusive
// document is turned away before the tokenizer allocates a tree for it.
//
// Keeping the old 4096 would have made this check dead code -- upstream's limit
// fires first, so nothing between it and 4096 could ever reach this one.
//
// The value is 510, not 512, and the difference is not arbitrary: upstream
// counts OPEN ELEMENTS, and the tokenizer synthesizes <html> and <body> around
// the document, so those two occupy stack slots before any authored tag does.
// This constant counts authored nesting in the source bytes. Measured against
// x/net v0.55.0: 510 nested <div> parse, 511 do not.
// TestNestingLimitMatchesUpstream pins that empirically, so a version bump that
// moves upstream's cap fails loudly rather than silently making this dead code.
//
// Either way it is far past real documents; even machine-generated HTML runs to
// tens of levels.
const maxNestingDepth = 510

// ErrTooDeeplyNested is returned by Parse when a document's tag nesting exceeds
// [maxNestingDepth]. It is a distinct sentinel because the document is
// well-formed -- it is refused for cost, not for being unreadable.
var ErrTooDeeplyNested = errors.New("html: tag nesting too deep")

// Parse parses HTML bytes into an owned DOM Document. It is total on the kinds of
// malformed input x/net/html recovers from (unclosed tags, stray text): such
// input yields a valid-but-quirky tree, never a panic. An error is returned only
// for input the underlying parser cannot read at all, or whose nesting exceeds
// [maxNestingDepth] (wrapping [ErrTooDeeplyNested]).
func Parse(data []byte) (*Document, error) {
	if depth, ok := nestingWithinLimit(data); !ok {
		return nil, fmt.Errorf("%w: %d levels exceeds the %d limit", ErrTooDeeplyNested, depth, maxNestingDepth)
	}
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

// nestingWithinLimit reports the deepest tag nesting in data and whether it is
// within maxNestingDepth. It runs the same tokenizer the parser uses, so tag
// recognition (comments, CDATA, script/style content, malformed markup) matches
// exactly what xhtml.Parse would see; only the tree building is skipped.
//
// The pass is linear and cheap -- ~11ms for 200,000 levels, against a parse that
// does not finish -- so it is affordable on every document, not just suspicious
// ones.
//
// The count deliberately ignores the parser's implicit-close rules (a <p> closed
// by a following <p>, say): those can only make the REAL open-element stack
// shallower than this estimate, never deeper. Over-counting risks refusing a
// document the parser would have handled cheaply, which is why the limit is set
// far above real markup rather than tightly.
func nestingWithinLimit(data []byte) (int, bool) {
	z := xhtml.NewTokenizer(bytes.NewReader(data))
	depth, deepest := 0, 0
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			// Includes io.EOF and malformed markup: either way there is no more
			// to count, and a document that fails to tokenize will fail (or
			// recover) identically in the parse below.
			return deepest, deepest <= maxNestingDepth
		case xhtml.StartTagToken:
			name, _ := z.TagName()
			if voidElements[string(name)] {
				continue // never pushed onto the open-element stack
			}
			depth++
			if depth > deepest {
				deepest = depth
				if deepest > maxNestingDepth {
					return deepest, false // no reason to scan the rest
				}
			}
		case xhtml.EndTagToken:
			if depth > 0 {
				depth--
			}
		}
	}
}

// voidElements are the HTML elements that never have content and so are never
// pushed onto the open-element stack (HTML §12.1.2). Counting them as nesting
// would make a long run of <br> look like deep nesting.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
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
