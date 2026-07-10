// Package xmlpart is the raw-fidelity XML layer the xlsx editor rewrites
// dirty parts through. It pins this repo's usage of github.com/beevik/etree
// (BSD-2, pure Go, zero deps): namespace prefixes stay literal ("x14:cfRule"
// survives — no URI resolution), attribute order is preserved, CDATA/comments/
// PIs/the xml declaration are kept, and nothing is reformatted. The contract
// is SEMANTIC losslessness — unknown elements and attributes survive in order
// — not byte-identical re-serialization (entity and quote normalization may
// differ; no OOXML consumer cares). Untouched parts never pass through here
// at all: the editor copies them byte-verbatim at the zip layer.
package xmlpart

import (
	"fmt"
	"sort"

	"github.com/beevik/etree"
)

// Part is one parsed XML part.
type Part struct {
	doc *etree.Document
}

// Parse reads an XML part. DOCTYPE directives are rejected: they never appear
// in machine-generated OOXML, and refusing them is a cheap XXE hedge.
func Parse(data []byte) (*Part, error) {
	doc := etree.NewDocument()
	doc.ReadSettings = etree.ReadSettings{
		PreserveCData:          true,
		PreserveDuplicateAttrs: true,
	}
	if err := doc.ReadFromBytes(data); err != nil {
		return nil, fmt.Errorf("xmlpart: %w", err)
	}
	for _, tok := range doc.Child {
		if _, isDirective := tok.(*etree.Directive); isDirective {
			return nil, fmt.Errorf("xmlpart: DOCTYPE directives are not allowed in OOXML parts")
		}
	}
	return &Part{doc: doc}, nil
}

// Bytes serializes the part. Serialization is deterministic for a given tree.
func (p *Part) Bytes() ([]byte, error) {
	out, err := p.doc.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("xmlpart: %w", err)
	}
	return out, nil
}

// Root returns the part's root element (nil for a degenerate part).
func (p *Part) Root() *etree.Element { return p.doc.Root() }

// FindChild returns the first direct child of parent with the given LOCAL tag
// name (ignoring any namespace prefix), or nil.
func FindChild(parent *etree.Element, local string) *etree.Element {
	for _, ch := range parent.ChildElements() {
		if localName(ch) == local {
			return ch
		}
	}
	return nil
}

// Children returns the direct children of parent with the given local name.
func Children(parent *etree.Element, local string) []*etree.Element {
	var out []*etree.Element
	for _, ch := range parent.ChildElements() {
		if localName(ch) == local {
			out = append(out, ch)
		}
	}
	return out
}

// localName is an element's tag without its prefix.
func localName(e *etree.Element) string { return e.Tag }

// EnsureChildInOrder returns parent's direct child with the given local name,
// creating it at its schema position when absent. order lists the parent's
// child-element sequence from the OOXML schema; the new element is inserted
// after the last existing child whose name precedes it in order (and before
// everything else), so a part that omits optional elements gains them in a
// place every consumer accepts.
func EnsureChildInOrder(parent *etree.Element, name string, order []string) *etree.Element {
	if el := FindChild(parent, name); el != nil {
		return el
	}
	rank := map[string]int{}
	for i, n := range order {
		rank[n] = i
	}
	myRank, known := rank[name]
	el := etree.NewElement(name)
	if !known {
		parent.AddChild(el)
		return el
	}
	// Find the first existing child that must come AFTER name; insert before it.
	for _, ch := range parent.ChildElements() {
		if r, ok := rank[localName(ch)]; ok && r > myRank {
			parent.InsertChildAt(childTokenIndex(parent, ch), el)
			return el
		}
	}
	parent.AddChild(el)
	return el
}

// childTokenIndex resolves an element's token index within its parent (the
// index InsertChildAt expects — it counts every child token, not only
// elements).
func childTokenIndex(parent *etree.Element, target *etree.Element) int {
	for i, tok := range parent.Child {
		if el, ok := tok.(*etree.Element); ok && el == target {
			return i
		}
	}
	return len(parent.Child)
}

// InsertBefore inserts el immediately before ref among parent's children.
func InsertBefore(parent *etree.Element, el, ref *etree.Element) {
	parent.InsertChildAt(childTokenIndex(parent, ref), el)
}

// Remove deletes el from its parent.
func Remove(parent, el *etree.Element) { parent.RemoveChild(el) }

// Equal compares two elements semantically: prefixed name, attributes as an
// order-insensitive set, text content, and child elements in sequence — the
// dedupe test for style records (two xf/font/fill/border nodes that differ
// only in attribute order are the same record).
func Equal(a, b *etree.Element) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Space != b.Space || a.Tag != b.Tag {
		return false
	}
	if len(a.Attr) != len(b.Attr) {
		return false
	}
	key := func(at etree.Attr) string { return at.Space + ":" + at.Key + "=" + at.Value }
	ak := make([]string, len(a.Attr))
	bk := make([]string, len(b.Attr))
	for i := range a.Attr {
		ak[i] = key(a.Attr[i])
		bk[i] = key(b.Attr[i])
	}
	sort.Strings(ak)
	sort.Strings(bk)
	for i := range ak {
		if ak[i] != bk[i] {
			return false
		}
	}
	if a.Text() != b.Text() {
		return false
	}
	ac, bc := a.ChildElements(), b.ChildElements()
	if len(ac) != len(bc) {
		return false
	}
	for i := range ac {
		if !Equal(ac[i], bc[i]) {
			return false
		}
	}
	return true
}
