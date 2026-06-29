package css

import "testing"

// fakeNode is the in-test DOM used throughout pkg/css tests.
type fakeNode struct {
	tag     string
	id      string
	classes []string
	parent  *fakeNode
	attrs   map[string]string
}

func (n *fakeNode) Tag() string       { return n.tag }
func (n *fakeNode) ID() string        { return n.id }
func (n *fakeNode) Classes() []string { return n.classes }
func (n *fakeNode) Parent() Node {
	if n.parent == nil {
		return nil
	}
	return n.parent
}
func (n *fakeNode) Attr(k string) (string, bool) { v, ok := n.attrs[k]; return v, ok }

func TestFakeNodeSatisfiesNode(t *testing.T) {
	var _ Node = (*fakeNode)(nil) // compile-time assertion the interface matches
	n := &fakeNode{tag: "p", id: "lead", classes: []string{"intro"}}
	if n.Tag() != "p" || n.ID() != "lead" || len(n.Classes()) != 1 {
		t.Fatalf("fakeNode accessors wrong: %+v", n)
	}
}
