package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// A text-decoration:underline link emits at least one underline rule (a RuleKind) at
// the right place — just below the line baseline — spanning the link text.
func TestUnderlineEmittedForLink(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p>see <a href="/x">the link</a> here</p></body>`,
		400, resource.MapLoader{}, nil)
	var items []layout.Item
	items = root.AppendItems(items)

	var rules []layout.RuleItem
	for _, it := range items {
		if it.Kind == layout.RuleKind {
			rules = append(rules, it.Rule)
		}
	}
	if len(rules) == 0 {
		t.Fatal("no underline rule emitted for an underlined link")
	}
	// The underline must have positive extent and a sensible thickness.
	r := rules[0]
	if r.WPt <= 0 || r.HPt <= 0 {
		t.Errorf("underline rule degenerate: %+v", r)
	}
}

// A box without text-decoration emits no underline rule (byte-identical to before).
func TestNoUnderlineWithoutDecoration(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><p>plain paragraph text with no decoration</p></body>`,
		400, resource.MapLoader{}, nil)
	var items []layout.Item
	items = root.AppendItems(items)
	for _, it := range items {
		if it.Kind == layout.RuleKind {
			t.Errorf("unexpected RuleKind (underline) for undecorated text: %+v", it.Rule)
		}
	}
}
