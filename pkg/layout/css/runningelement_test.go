package css

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
)

func TestCaptureRunningElement(t *testing.T) {
	// A running element with text lays out into a fragment at the given width. Build it
	// through the real pipeline so it carries a proper computed style (width auto, font).
	src := `<html><body><div style="position:running(h)">Header</div></body></html>`
	_, running := buildWithRunning(t, src)
	box := running["h"]
	if box == nil {
		t.Fatalf("running element 'h' not collected")
	}
	e := New(nil, nil, nil) // a real face cache so text shapes
	frag := e.captureRunningElement(context.Background(), box, 200)
	if frag == nil {
		t.Fatalf("captureRunningElement returned nil")
	}
	if frag.W <= 0 {
		t.Errorf("captured fragment has no width")
	}
}

func TestPlaceRunningElement(t *testing.T) {
	// Placing a captured fragment in a margin rect translates it to the rect origin and
	// emits items.
	src := `<html><body><div style="position:running(h)">X</div></body></html>`
	_, running := buildWithRunning(t, src)
	box := running["h"]
	if box == nil {
		t.Fatalf("running element 'h' not collected")
	}
	e := New(nil, nil, nil)
	frag := e.captureRunningElement(context.Background(), box, 200)
	if frag == nil {
		t.Fatalf("captureRunningElement returned nil")
	}
	r := marginRect{x: 50, y: 10, w: 200, h: 40}
	var items []layout.Item
	items = e.placeRunningElement(items, frag, r)
	if len(items) == 0 {
		t.Errorf("placeRunningElement emitted no items")
	}
}

func TestPlaceRunningElementBoxIdempotent(t *testing.T) {
	// placeRunningElementBox captures fresh per call, so the shared box is never
	// corrupted: two placements at different rects produce two well-formed item lists.
	src := `<html><body><div style="position:running(h)">Y</div></body></html>`
	_, running := buildWithRunning(t, src)
	box := running["h"]
	if box == nil {
		t.Fatalf("running element 'h' not collected")
	}
	e := New(nil, nil, nil)
	r1 := marginRect{x: 0, y: 0, w: 200, h: 40}
	r2 := marginRect{x: 30, y: 90, w: 200, h: 40}
	items1 := e.placeRunningElementBox(context.Background(), nil, box, r1)
	items2 := e.placeRunningElementBox(context.Background(), nil, box, r2)
	if len(items1) == 0 || len(items2) == 0 {
		t.Fatalf("placeRunningElementBox emitted no items (%d, %d)", len(items1), len(items2))
	}
	if len(items1) != len(items2) {
		t.Errorf("re-capture should be identical: item counts %d != %d", len(items1), len(items2))
	}
}
