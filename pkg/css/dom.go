package css

// Node is the minimal read-only view of a DOM element the cascade matches
// selectors against. pkg/html implements it later (sub-project 2); pkg/css does
// not import pkg/html, so the layering stays one-directional. A nil Parent marks
// the root.
type Node interface {
	// Tag is the lowercased element name (e.g. "div"). Empty for non-elements.
	Tag() string
	// ID is the element's id attribute, or "" if absent.
	ID() string
	// Classes is the element's class list (already split on whitespace).
	Classes() []string
	// Parent is the element's parent, or nil at the root.
	Parent() Node
	// Attr returns an attribute value and whether it was present.
	Attr(key string) (string, bool)
}

// SiblingIndexer is an optional extension of Node that enables the structural
// pseudo-classes (:first-child, :nth-child(), :nth-of-type(), …). All values are
// 1-based positions among the parent's ELEMENT children (text nodes do not
// count): pos from the first sibling, last from the last sibling, and
// typePos/typeLast the same restricted to siblings sharing the element's tag. A
// root element reports 1 for all four. A Node that does not implement the
// interface never matches a structural pseudo-class (the selector is inert, not
// an error).
type SiblingIndexer interface {
	SiblingIndex() (pos, last, typePos, typeLast int)
}
