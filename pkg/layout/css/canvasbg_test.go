package css

import (
	"context"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
)

func TestPropagateCanvasBackground(t *testing.T) {
	beige := color.RGBA{0xf6, 0xf6, 0xef, 0xff}

	// body background → propagated to the canvas; body box no longer paints it.
	doc, _ := html.Parse([]byte(`<!DOCTYPE html><html><body style="background:#f6f6ef"><p>hi</p></body></html>`))
	root, _ := Build(context.Background(), doc, nil, nil)
	got := propagateCanvasBackground(root)
	if got != beige {
		t.Errorf("propagated = %v, want %v (body background)", got, beige)
	}
	if body := bodyBox(root); body == nil || body.Style.BackgroundColor.A != 0 {
		t.Errorf("body box background should be cleared after propagation, got %v", body.Style.BackgroundColor)
	}

	// html background wins over body background.
	doc2, _ := html.Parse([]byte(`<!DOCTYPE html><html style="background:#112233"><body style="background:#f6f6ef"><p>x</p></body></html>`))
	root2, _ := Build(context.Background(), doc2, nil, nil)
	got2 := propagateCanvasBackground(root2)
	if want := (color.RGBA{0x11, 0x22, 0x33, 0xff}); got2 != want {
		t.Errorf("propagated = %v, want %v (html background wins)", got2, want)
	}

	// No background anywhere → zero (no propagation).
	doc3, _ := html.Parse([]byte(`<!DOCTYPE html><html><body><p>x</p></body></html>`))
	root3, _ := Build(context.Background(), doc3, nil, nil)
	if got3 := propagateCanvasBackground(root3); got3 != (color.RGBA{}) {
		t.Errorf("propagated = %v, want zero (no background)", got3)
	}

	// nil root → zero, no panic.
	if propagateCanvasBackground(nil) != (color.RGBA{}) {
		t.Error("propagateCanvasBackground(nil) should be zero")
	}
}
