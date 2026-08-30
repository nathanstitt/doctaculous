package svg

import "github.com/nathanstitt/doctaculous/pkg/css"

// cssNode adapts a parsed SVG *element to css.Node so pkg/css selectors can
// match against the SVG tree. It is a thin, stateless wrapper: it caches
// nothing of its own, reading straight through to el's precomputed id and
// classes (see element in xml.go). Only SVG-namespace elements are ever
// wrapped — the scene builder checks el.space != svgNS and skips foreign
// subtrees before descending — so this adapter does not filter on namespace
// itself.
type cssNode struct {
	el *element
	// hostParent continues the ancestor chain PAST the SVG root into the host
	// document, so a descendant selector written against the page — `#sidebar .icon`,
	// `figure svg rect` — matches an inline <svg>'s children the way CSS says it
	// should. Nil for a standalone SVG document, where the root has no ancestors.
	//
	// Without it the chain terminates at <svg> and such a selector silently never
	// matches, which is worse than not supporting host CSS at all: `.icon` would work
	// while `#sidebar .icon` quietly did nothing.
	hostParent css.Node
}

var _ css.Node = (*cssNode)(nil)

// Tag returns the element's local name verbatim (case-sensitive), e.g.
// "linearGradient" or "clipPath". Unlike HTML, SVG element names are
// case-sensitive; css.Node.Tag is matched case-insensitively by the
// selector engine, so no folding is needed here. Implements css.Node.
func (n *cssNode) Tag() string {
	return n.el.local
}

// ID returns the element's id attribute, or "" if absent. Implements css.Node.
func (n *cssNode) ID() string {
	return n.el.id
}

// Classes returns the element's class list, already split on whitespace.
// Implements css.Node.
func (n *cssNode) Classes() []string {
	return n.el.classes
}

// Parent returns the adapter for the element's parent, or a true nil
// interface at the root. Returning a typed (*cssNode)(nil) here instead
// would make the returned interface non-nil (Go's typed-nil-in-interface
// pitfall), and css.Selector.Matches walks Parent() until it sees nil —
// so a typed nil would infinite-loop instead of terminating.
func (n *cssNode) Parent() css.Node {
	if n.el.parent == nil {
		// At the SVG root, continue into the host document's ancestors when embedded.
		// The nil check keeps the typed-nil pitfall above at bay: hostParent is a
		// css.Node interface that is either genuinely nil or a real host node.
		return n.hostParent
	}
	return &cssNode{el: n.el.parent, hostParent: n.hostParent}
}

// Attr returns an attribute value and whether it was present. SVG attribute
// names are case-sensitive (e.g. "viewBox", "gradientUnits",
// "preserveAspectRatio"), so the key is looked up verbatim, with no
// lowercasing. Implements css.Node.
func (n *cssNode) Attr(key string) (string, bool) {
	v, ok := n.el.attrs[key]
	return v, ok
}
