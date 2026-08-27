package svg

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// svgNS is the SVG XML namespace.
const svgNS = "http://www.w3.org/2000/svg"

// xlinkNS is the XLink namespace used by legacy xlink:href attributes.
const xlinkNS = "http://www.w3.org/1999/xlink"

// maxElementDepth bounds the element nesting depth parseXML will follow. It
// is far beyond anything a real SVG document reaches; hostile input with
// deeper nesting is treated like truncated input (parsed prefix returned,
// logged) rather than risking runaway memory or an unbounded stack via a
// recursive walk. The walk here is iterative regardless, but the cap still
// guards against pathological memory use from millions of nested elements.
const maxElementDepth = 1024

// errNotSVG is wrapped by parseXML when the document's root element exists
// but is not an <svg> element in the SVG namespace.
var errNotSVG = errors.New("not an SVG document")

// element is a namespace-aware XML element in a parsed SVG document tree.
// Attributes are keyed by local name for SVG-namespace and no-namespace
// attributes; a legacy xlink:href attribute folds into the "href" key.
// Foreign-namespace elements are kept in the tree (space holds their
// namespace URI) so later stages can decide whether to skip them.
type element struct {
	space, local string
	attrs        map[string]string
	kids         []*element
	text         string

	// content is el's children and character data INTERLEAVED in document
	// order — the ordering `kids` and `text` between them cannot express,
	// since text concatenates every CharData run in the element regardless of
	// which child elements it fell between. <text>A<tspan>B</tspan>C</text>
	// has kids=[tspan] and text="AC", which is indistinguishable from
	// <text>AC<tspan>B</tspan></text>; SVG text layout needs the real order,
	// because the position cursor threads through the subtree in document
	// order (SVG2 §11.5). Only pkg/svg/text.go reads this; every other
	// consumer keeps using kids/text exactly as before.
	content []contentNode

	// parent is the enclosing element, or nil for the document root. It is
	// backfilled once a child is fully parsed and attached (both the normal
	// end-tag path and the truncation-recovery unwind path set it), so CSS
	// descendant/ancestor combinators can walk upward from any node without
	// the caller having to thread a path down during traversal.
	parent *element

	// id is precomputed from attrs["id"] ("" when absent). CSS ID-selector
	// matching runs once per candidate element per selector; precomputing
	// avoids a map lookup on every match instead of just once at parse time.
	id string

	// classes is attrs["class"] split on runs of whitespace, precomputed for
	// the same reason as id: class-selector matching is repeated per
	// candidate element per selector, so splitting once here is cheaper than
	// re-splitting on every match. nil (not an empty non-nil slice) whenever
	// there are no usable class tokens — the attribute is absent, empty, or
	// whitespace-only — so callers can distinguish "no classes" from having
	// to compare a slice length.
	classes []string
}

// contentNode is one entry in an element's interleaved content list: either a
// child element (el != nil) or a run of character data (el == nil, text
// holding the raw, uncollapsed characters). Exactly one of the two is set.
// See element.content for why the ordering matters.
type contentNode struct {
	el   *element
	text string
}

// parseXML parses an SVG document into a namespace-aware element tree and
// returns its root <svg> element. It uses a lenient (non-strict) XML decoder
// with HTML entity support so real-world SVG (which often carries HTML
// entities like &nbsp;) parses instead of failing outright.
//
// The walk is iterative (an explicit stack, not recursion) so pathologically
// deep nesting cannot exhaust the goroutine stack; nesting beyond
// maxElementDepth is treated like truncation (see below).
//
// If a syntax error occurs after the <svg> root has been established, it is
// reported via logf (which may be nil, treated as a no-op) and parseXML
// returns the partial tree built so far with a nil error — so a truncated or
// slightly malformed real-world document still renders whatever prefix
// parsed cleanly. A syntax error before any root element is found is
// returned as an error. If the first element encountered is not an
// SVG-namespace <svg>, parseXML returns an error wrapping errNotSVG.
func parseXML(data []byte, logf func(string, ...any)) (*element, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}

	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity

	var (
		root     *element
		stack    []*element
		rootSeen bool // the root <svg> StartElement has been observed
	)

	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			if !rootSeen {
				return nil, fmt.Errorf("svg: %w", err)
			}
			logf("svg: malformed XML after byte %d: %v (rendering parsed prefix)", dec.InputOffset(), err)
			unwind(&root, &stack)
			return root, nil
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if !rootSeen {
				if t.Name.Space != svgNS || t.Name.Local != "svg" {
					name := t.Name.Local
					if t.Name.Space != "" {
						name = t.Name.Space + ":" + name
					}
					return nil, fmt.Errorf("svg: root element is <%s>, not <svg>: %w", name, errNotSVG)
				}
				rootSeen = true
			}
			if len(stack) >= maxElementDepth {
				logf("svg: element nesting exceeds %d, truncating (rendering parsed prefix)", maxElementDepth)
				unwind(&root, &stack)
				return root, nil
			}
			attrs := buildAttrs(t.Attr)
			el := &element{
				space:   t.Name.Space,
				local:   t.Name.Local,
				attrs:   attrs,
				id:      attrs["id"],
				classes: splitClasses(attrs["class"]),
			}
			stack = append(stack, el)

		case xml.EndElement:
			if len(stack) == 0 {
				// Unbalanced end tag with no open element; ignore rather
				// than panic on malformed input.
				continue
			}
			el := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				root = el
			} else {
				attach(stack[len(stack)-1], el)
			}

		case xml.CharData:
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.text += string(t)
				// Append to the interleaved list too, MERGING with a trailing
				// text run rather than appending a second one: the decoder can
				// split one logical run across several CharData tokens (an
				// entity reference, a CDATA boundary), and a caller walking
				// content for whitespace collapsing must see the same run
				// boundaries a single token would have produced.
				if n := len(top.content); n > 0 && top.content[n-1].el == nil {
					top.content[n-1].text += string(t)
				} else {
					top.content = append(top.content, contentNode{text: string(t)})
				}
			}
		}
	}

	if root == nil {
		if len(stack) > 0 {
			// EOF while elements were still open: treat as truncation.
			logf("svg: unexpected end of document (rendering parsed prefix)")
			unwind(&root, &stack)
			return root, nil
		}
		return nil, fmt.Errorf("svg: %w", errNotSVG)
	}
	return root, nil
}

// buildAttrs folds a token's attribute list into the map keyed by local
// name that element.attrs uses. xmlns and xmlns:* namespace declarations are
// dropped; xlink:href folds into "href" unless "href" is already set. Other
// foreign-namespace attributes are dropped: they are not part of the SVG
// attribute surface any consumer of element.attrs looks up, and folding them
// by local name would risk an unintended collision with a same-named SVG
// attribute.
func buildAttrs(attrs []xml.Attr) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
			continue
		}
		switch {
		case a.Name.Space == "" || a.Name.Space == svgNS:
			m[a.Name.Local] = a.Value
		case a.Name.Space == xlinkNS && a.Name.Local == "href":
			if _, ok := m["href"]; !ok {
				m["href"] = a.Value
			}
		}
	}
	return m
}

// unwind attaches any still-open elements on the stack to their parents, in
// order from innermost to outermost, so a document that ends abruptly (parse
// error or excessive nesting) still yields the deepest partial tree rather
// than discarding it. The result is written back into *root.
func unwind(root **element, stack *[]*element) {
	s := *stack
	for len(s) > 1 {
		child := s[len(s)-1]
		parent := s[len(s)-2]
		attach(parent, child)
		s = s[:len(s)-1]
	}
	if len(s) == 1 {
		*root = s[0]
	}
	*stack = nil
}

// attach appends child to parent's kids and backfills child.parent. It is
// the single attachment point used by both the normal EndElement pop and the
// truncation-recovery unwind path, so every element that makes it into the
// tree has a coherent parent chain regardless of which path attached it.
func attach(parent, child *element) {
	parent.kids = append(parent.kids, child)
	parent.content = append(parent.content, contentNode{el: child})
	child.parent = parent
}

// splitClasses splits a class attribute value on runs of whitespace,
// returning nil when v has no usable tokens (empty or whitespace-only) so
// callers can distinguish "no class attribute" from "class attribute
// present but empty" — a whitespace-only value is the same case as an empty
// one and must yield the same nil-ness.
func splitClasses(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.Fields(v)
}
