package html

import "github.com/nathanstitt/doctaculous/pkg/css"

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

// Tag returns the lowercased element name. Implements css.Node.
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
