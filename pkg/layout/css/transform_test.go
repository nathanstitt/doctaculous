package css

import (
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/layout"
	"github.com/nathanstitt/omnidoc/pkg/resource"
)

// transformItems returns the transform brackets emitted for a page.
func transformItems(t *testing.T, style string) []layout.Item {
	t.Helper()
	root := layoutWithLoader(t,
		`<body><div style="width:200px;height:120px"><div style="width:40px;height:20px;`+style+`"></div></div></body>`,
		200, resource.MapLoader{}, nil)
	var out []layout.Item
	for _, it := range root.AppendItems(nil) {
		if it.Kind == layout.TransformPushKind || it.Kind == layout.TransformPopKind {
			out = append(out, it)
		}
	}
	return out
}

// A transform emits a balanced bracket carrying the resolved matrix.
func TestTransformEmitsBalancedBracket(t *testing.T) {
	items := transformItems(t, "transform:translateX(60px)")
	if len(items) != 2 {
		t.Fatalf("got %d transform items, want a push and a pop", len(items))
	}
	if items[0].Kind != layout.TransformPushKind || items[1].Kind != layout.TransformPopKind {
		t.Fatalf("bracket is not push-then-pop: %v, %v", items[0].Kind, items[1].Kind)
	}
	if e := items[0].Transform.E; e < 59.5 || e > 60.5 {
		t.Errorf("matrix E = %v, want 60", e)
	}
}

// An untransformed box emits no bracket at all, so every existing document is
// unaffected and pays nothing.
func TestNoTransformEmitsNoBracket(t *testing.T) {
	if items := transformItems(t, ""); len(items) != 0 {
		t.Errorf("got %d transform items for an untransformed box, want 0", len(items))
	}
	if items := transformItems(t, "transform:none"); len(items) != 0 {
		t.Errorf("got %d transform items for transform:none, want 0", len(items))
	}
}

// The matrix is built about the box's CENTRE (the default transform-origin), so a
// scale grows symmetrically rather than from the top-left.
func TestTransformOriginIsCentre(t *testing.T) {
	items := transformItems(t, "transform:scale(2)")
	if len(items) != 2 {
		t.Fatalf("got %d transform items, want 2", len(items))
	}
	m := items[0].Transform
	// A 40x20 box at (0,0) scaled 2x about its centre (20,10) maps its top-left to
	// (-20,-10): E = 20 - 2*20 = -20, F = 10 - 2*10 = -10.
	if m.E < -20.5 || m.E > -19.5 {
		t.Errorf("matrix E = %v, want -20 (scale about the centre)", m.E)
	}
	if m.F < -10.5 || m.F > -9.5 {
		t.Errorf("matrix F = %v, want -10", m.F)
	}
}

// A percentage translate resolves against the BOX's own border-box size, which the
// cascade cannot know — so it travels unresolved and is finished here.
func TestTransformPercentageResolvesAgainstBox(t *testing.T) {
	items := transformItems(t, "transform:translateX(50%)")
	if len(items) != 2 {
		t.Fatalf("got %d transform items, want 2", len(items))
	}
	if e := items[0].Transform.E; e < 19.5 || e > 20.5 {
		t.Errorf("matrix E = %v, want 20 (50%% of the box's own 40px width)", e)
	}
}

// A transformed box becomes a stacking context and a BFC (CSS Transforms 1 §3). That
// is what lets the bracket wrap its background AND its content: Appendix E otherwise
// emits those in separate phases, so the matrix would move one and not the other.
func TestTransformedBoxIsStackingContextAndBFC(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><div style="width:200px"><div style="width:40px;height:20px;transform:scale(2)"></div></div></body>`,
		200, resource.MapLoader{}, nil)
	var found *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f.W == 40 && f.H == 20 {
			found = f
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if found == nil {
		t.Fatal("transformed fragment not found")
	}
	if !found.IsStackingContext {
		t.Error("a transformed box is not a stacking context")
	}
	if !found.IsBFC {
		t.Error("a transformed box does not establish a BFC")
	}
}

// An ANONYMOUS box carries a zero-value style, whose Transform is all zeros — which is
// NOT the identity matrix. Treating that as a transform made every anonymous box a
// stacking context and a BFC, silently reordering paint and breaking a WPT reftest.
func TestAnonymousBoxIsNotTransformed(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><div style="width:200px">bare text forcing an anonymous block<div>and a block sibling</div></div></body>`,
		200, resource.MapLoader{}, nil)
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f.Box != nil && isAnonymous(f.Box) {
			if transformed(f.Box) {
				t.Error("an anonymous box reported as transformed; its zero-value style is not a matrix")
			}
			if f.IsStackingContext {
				t.Error("an anonymous box became a stacking context")
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
}
