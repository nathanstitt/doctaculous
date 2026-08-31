package svg

import (
	"reflect"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/css"
)

func TestCSSNodeAdapter(t *testing.T) {
	root, _ := parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg">
	  <g class="wrap"><linearGradient id="g1" gradientUnits="userSpaceOnUse"/></g>
	</svg>`), nil)
	g := root.kids[0]
	lg := g.kids[0]

	n := &cssNode{el: lg}
	if n.Tag() != "linearGradient" {
		t.Errorf("Tag() = %q, want verbatim camelCase", n.Tag())
	}
	if n.ID() != "g1" {
		t.Errorf("ID() = %q", n.ID())
	}
	if v, ok := n.Attr("gradientUnits"); !ok || v != "userSpaceOnUse" {
		t.Errorf("Attr(gradientUnits) = %q,%v (attribute names are case-sensitive)", v, ok)
	}
	if _, ok := n.Attr("gradientunits"); ok {
		t.Error("Attr matched a lowercased attribute name; SVG attrs are case-sensitive")
	}

	// Parent chain, and the root MUST return an untyped nil interface.
	p := n.Parent()
	if p == nil || p.Tag() != "g" {
		t.Fatalf("Parent() = %v", p)
	}
	if !reflect.DeepEqual(p.Classes(), []string{"wrap"}) {
		t.Errorf("parent classes = %v", p.Classes())
	}
	gp := p.Parent()
	if gp == nil || gp.Tag() != "svg" {
		t.Fatalf("grandparent = %v", gp)
	}
	if root := gp.Parent(); root != nil {
		t.Errorf("root Parent() = %#v, want a nil interface (a typed nil pointer breaks Matches)", root)
	}

	// Selector matching works end to end through the adapter.
	sheet := css.Parse(`g linearGradient { fill: red }`)
	if !sheet.Rules[0].Selectors[0].Matches(n) {
		t.Error("descendant selector did not match through the adapter")
	}
}
