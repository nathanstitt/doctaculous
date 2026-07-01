package css

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// buildWithRunning parses src and builds the box tree plus the running-element map.
func buildWithRunning(t *testing.T, src string) (*cssbox.Box, map[string]*cssbox.Box) {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	root, _, _, running, err := BuildWithFontsPagesRunning(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("BuildWithFontsPagesRunning: %v", err)
	}
	return root, running
}

func TestRunningElementExcludedFromFlow(t *testing.T) {
	src := `<html><body>
		<header style="position:running(head)">My Header</header>
		<p>Body paragraph</p>
	</body></html>`
	root, running := buildWithRunning(t, src)

	// The running header is NOT an in-flow child anywhere in the in-flow tree.
	var assertNoRunning func(b *cssbox.Box)
	assertNoRunning = func(b *cssbox.Box) {
		for _, c := range b.Children {
			if c.RunningName == "head" {
				t.Errorf("running element should be excluded from in-flow children")
			}
			assertNoRunning(c)
		}
	}
	assertNoRunning(root)

	// It WAS collected by name, with its position kind set.
	rb, ok := running["head"]
	if !ok {
		t.Fatalf("running element 'head' should be collected")
	}
	if rb.Position != cssbox.PosRunning {
		t.Errorf("collected box Position = %v, want PosRunning", rb.Position)
	}
	if rb.RunningName != "head" {
		t.Errorf("collected box RunningName = %q, want \"head\"", rb.RunningName)
	}
}
