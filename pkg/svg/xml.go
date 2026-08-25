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
			el := &element{
				space: t.Name.Space,
				local: t.Name.Local,
				attrs: buildAttrs(t.Attr),
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
				parent := stack[len(stack)-1]
				parent.kids = append(parent.kids, el)
			}

		case xml.CharData:
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.text += string(t)
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
		parent.kids = append(parent.kids, child)
		s = s[:len(s)-1]
	}
	if len(s) == 1 {
		*root = s[0]
	}
	*stack = nil
}
