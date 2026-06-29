package html

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/css"
)

func TestElementSatisfiesCSSNode(t *testing.T) {
	var _ css.Node = (*Element)(nil) // compile-time assertion
}

func TestElementAccessors(t *testing.T) {
	root := &Element{tag: "body"}
	el := &Element{
		tag:     "p",
		id:      "lead",
		classes: []string{"intro", "note"},
		attrs:   map[string]string{"style": "color:red", "id": "lead"},
		parent:  root,
	}
	root.children = []DOMNode{el}

	if el.Tag() != "p" || el.ID() != "lead" {
		t.Errorf("tag/id = %q/%q", el.Tag(), el.ID())
	}
	if len(el.Classes()) != 2 || el.Classes()[0] != "intro" {
		t.Errorf("classes = %v", el.Classes())
	}
	if v, ok := el.Attr("style"); !ok || v != "color:red" {
		t.Errorf("Attr(style) = %q,%v", v, ok)
	}
	if _, ok := el.Attr("missing"); ok {
		t.Error("Attr(missing) should be absent")
	}
	if el.Parent() != css.Node(root) {
		t.Error("Parent() should return the root element as a css.Node")
	}
	if got := root.Children(); len(got) != 1 || got[0] != DOMNode(el) {
		t.Errorf("root.Children() = %v, want [el]", got)
	}
}

func TestRootParentIsNil(t *testing.T) {
	root := &Element{tag: "html"}
	if root.Parent() != nil { // css.Node form: true nil at root
		t.Error("root Parent() must be nil (the cascade's initial-values signal)")
	}
	if root.ParentElement() != nil {
		t.Error("root ParentElement() must be nil")
	}
}

func TestTextNodeParent(t *testing.T) {
	p := &Element{tag: "p"}
	txt := &Text{Data: "hi", parent: p}
	if txt.ParentElement() != p { // Text exposes only the typed accessor
		t.Error("text node parent wrong")
	}
}
