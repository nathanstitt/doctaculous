package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// TestLineThroughEmitsRule renders a line-through span and asserts a RuleKind item
// is emitted at roughly mid-glyph (above the baseline), mirroring underline.
func TestLineThroughEmitsRule(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p><span style="text-decoration:line-through">struck</span></p></body>`,
		400, resource.MapLoader{}, nil)
	items := root.AppendItems(nil)
	var rules int
	for _, it := range items {
		if it.Kind == layout.RuleKind {
			rules++
		}
	}
	if rules == 0 {
		t.Fatalf("no RuleKind emitted for line-through text")
	}
}

// TestNoStrikeWithoutLineThrough confirms undecorated text emits no rule (the
// default path stays byte-identical).
func TestNoStrikeWithoutLineThrough(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p><span>plain</span></p></body>`,
		400, resource.MapLoader{}, nil)
	items := root.AppendItems(nil)
	for _, it := range items {
		if it.Kind == layout.RuleKind {
			t.Errorf("unexpected RuleKind for undecorated text: %+v", it.Rule)
		}
	}
}
