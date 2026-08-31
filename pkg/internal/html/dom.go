package html

import "github.com/nathanstitt/omnidoc/pkg/internal/css"

// DOMNode is the common interface over the owned tree (Element and Text). It is
// produced by Parse and is read-only thereafter. It uses ParentElement (not
// Parent) because *Element's Parent() returns css.Node to satisfy css.Node.
type DOMNode interface {
	// ParentElement returns the containing element, or nil at the root.
	ParentElement() *Element
	// node is unexported so only this package's types satisfy DOMNode.
	node()
}

// Element is an owned HTML element. All of its css.Node data is pre-computed at
// parse time, so the cascade tree-walk does no per-call allocation. *Element
// implements css.Node.
type Element struct {
	tag      string
	id       string
	classes  []string
	attrs    map[string]string
	parent   *Element
	children []DOMNode

	// foreignSrc is the re-serialized markup of a foreign-content (SVG) subtree,
	// set only on the root <svg> element of one. See ForeignSource.
	foreignSrc string
}

// ForeignSource returns the re-serialized markup of an inline foreign-content
// subtree — today, an inline <svg> — and whether this element is the root of one.
// It is "" and false for every ordinary HTML element.
//
// An inline <svg> is NOT lowered into HTML boxes. Its content is SVG, and pkg/svg
// is the single source of truth for parsing it; bridging x/net/html's node type
// into pkg/svg's AST would duplicate that package's whole element and attribute
// construction against a second node type, and every future SVG parser fix would
// then have to land twice. So the subtree is handed back as markup and re-parsed
// by pkg/svg. The extra serialize/reparse is negligible next to parsing the host
// document at all.
//
// The returned markup preserves the camelCase names the HTML parser REPAIRED
// (clipPath, linearGradient, gradientUnits, viewBox, ...): HTML5 foreign content
// lower-cases tag and attribute names during tokenization and then restores the
// SVG spellings via its adjustment tables, and x/net/html's renderer writes back
// the restored names. Losing them would silently break every gradient and clip in
// inline SVG, so it is pinned by a test.
func (e *Element) ForeignSource() (string, bool) {
	return e.foreignSrc, e.foreignSrc != ""
}

func (e *Element) node() {}

// Parent returns the element's parent as a css.Node, or a true nil at the root.
// This is the css.Node implementation; internal tree code uses ParentElement.
func (e *Element) Parent() css.Node {
	if e.parent == nil {
		return nil // true nil interface, so the cascade's root check works
	}
	return e.parent
}

// ParentElement returns the typed parent element, or nil at the root. Used by box
// generation and DOM traversal where the concrete type is wanted.
func (e *Element) ParentElement() *Element { return e.parent }

// Children returns the element's child nodes in document order.
func (e *Element) Children() []DOMNode { return e.children }

// Tag returns the element name. It is already lowercase because x/net/html
// lowercases tag names at parse time, not because css.Node requires it (the
// interface only promises the host format's own casing, matched
// case-insensitively). Implements css.Node.
func (e *Element) Tag() string { return e.tag }

// ID returns the element's id attribute, or "". Implements css.Node.
func (e *Element) ID() string { return e.id }

// Classes returns the element's class list. Implements css.Node.
func (e *Element) Classes() []string { return e.classes }

// Attr returns an attribute value and whether it was present. Implements css.Node.
func (e *Element) Attr(key string) (string, bool) {
	v, ok := e.attrs[key]
	return v, ok
}

// SiblingIndex returns e's 1-based position among its parent's element children
// (from the start and from the end), plus the same restricted to siblings with
// e's tag. Implements css.SiblingIndexer, enabling the structural pseudo-classes.
// The root element counts as the first and only child.
func (e *Element) SiblingIndex() (pos, last, typePos, typeLast int) {
	if e.parent == nil {
		return 1, 1, 1, 1
	}
	total, typeTotal := 0, 0
	for _, c := range e.parent.children {
		el, ok := c.(*Element)
		if !ok {
			continue
		}
		total++
		if el.tag == e.tag {
			typeTotal++
		}
		if el == e {
			pos, typePos = total, typeTotal
		}
	}
	return pos, total - pos + 1, typePos, typeTotal - typePos + 1
}

// Text is an owned character-data node. Data is exported directly (no accessor)
// because Text is a simple value carrier: it has no interface contract and box
// generation reads it as a plain string.
type Text struct {
	// Data is the raw character content of this text node.
	Data   string
	parent *Element
}

func (t *Text) node() {}

// ParentElement returns the text node's parent element.
func (t *Text) ParentElement() *Element { return t.parent }
