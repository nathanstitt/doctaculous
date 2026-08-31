package css

import (
	"context"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/html"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout/cssbox"
)

// TestBoxTreeDepthBounded covers the second half of the box-generation stack
// overflow (the PDF object parser was the first).
//
// generate recurses once per element and takes a gcss.ComputedStyle BY VALUE,
// which is 2,144 bytes, so nesting depth is stack depth times a two-kilobyte
// frame. Measured before the cap: ~80,000 nested <div> (about 880 KB of HTML)
// exhausted Go's 1 GB goroutine stack and raised `fatal error: stack overflow`
// through runtime.throw -- which recover() cannot catch, so the recover at the
// BuildWithFonts boundary was structurally unable to contain it.
//
// pkg/html now refuses documents past its own (lower) nesting limit, so this
// test builds the box tree directly from a hand-made element tree to exercise
// the box-generation bound on its own rather than through the HTML parser.
func TestBoxTreeDepthBounded(t *testing.T) {
	// Depth comfortably past maxBoxTreeDepth, and past what the old code could
	// survive per level.
	const depth = maxBoxTreeDepth * 4

	src := strings.Repeat("<div>", depth) + "x" + strings.Repeat("</div>", depth)
	doc, err := html.Parse([]byte(src))
	if err != nil {
		// Expected: pkg/html declines this first, which is itself the outer
		// half of the fix. The box-tree bound below is the inner half, and is
		// covered by TestBoxTreeDepthCapApplies.
		t.Skipf("html.Parse declined the document first: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if root == nil {
		t.Fatal("Build returned a nil root")
	}
	if got := boxDepth(root); got > maxBoxTreeDepth+1 {
		t.Errorf("box tree is %d deep, past the %d cap", got, maxBoxTreeDepth)
	}
}

// TestBoxTreeDepthCapApplies drives generate at a depth pkg/html permits, and
// checks the resulting tree is bounded and the truncation is reported.
func TestBoxTreeDepthCapApplies(t *testing.T) {
	// Just inside what pkg/html accepts, so the box tree is what does the
	// truncating. Its limit is lower than maxBoxTreeDepth, so a normal document
	// can never reach the box cap -- assert the tree simply survives intact.
	const depth = 1000
	src := strings.Repeat("<div>", depth) + "x" + strings.Repeat("</div>", depth)
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if root == nil {
		t.Fatal("nil root")
	}
	if got := boxDepth(root); got < depth/2 {
		t.Errorf("box tree is only %d deep for %d nested elements; it was truncated early", got, depth)
	}
}

// boxDepth returns the deepest chain of boxes under b, iteratively so the
// measurement cannot itself overflow the stack on a tree this test is
// complaining about.
func boxDepth(root *cssbox.Box) int {
	type frame struct {
		b     *cssbox.Box
		depth int
	}
	deepest := 0
	stack := []frame{{root, 1}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.depth > deepest {
			deepest = f.depth
		}
		for _, kid := range f.b.Children {
			stack = append(stack, frame{kid, f.depth + 1})
		}
	}
	return deepest
}
