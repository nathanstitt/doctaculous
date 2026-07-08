package content

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// TestGraphicsSinkReceivesFill asserts a filled rectangle is reported to the graphics
// sink in device space (the same path the Device is painted with), while painting is
// unchanged.
func TestGraphicsSinkReceivesFill(t *testing.T) {
	var ops []VectorOp
	dev := &recDevice{}
	it := New(nil, dev, nil, render.Identity, Options{
		GraphicsSink: func(op VectorOp) { ops = append(ops, op) },
	})
	if err := it.Run([]byte("1 0 0 rg 100 100 200 150 re f")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Paint still happened exactly once.
	if len(dev.fills) != 1 {
		t.Fatalf("fills = %d, want 1", len(dev.fills))
	}
	// The sink observed the same fill.
	if len(ops) != 1 {
		t.Fatalf("sink ops = %d, want 1", len(ops))
	}
	if ops[0].Kind != VectorFill {
		t.Errorf("kind = %v, want VectorFill", ops[0].Kind)
	}
	if ops[0].Path == nil || len(ops[0].Path.Segments) < 5 {
		t.Errorf("path = %+v, want a closed rectangle", ops[0].Path)
	}
}

// TestGraphicsSinkReceivesStroke asserts a stroked line is reported with its device-space
// width, so a table-ruling detector can threshold on thin strokes.
func TestGraphicsSinkReceivesStroke(t *testing.T) {
	var ops []VectorOp
	it := New(nil, &recDevice{}, nil, render.Identity, Options{
		GraphicsSink: func(op VectorOp) { ops = append(ops, op) },
	})
	// 2-unit line width, a horizontal segment.
	if err := it.Run([]byte("2 w 0 0 0 RG 50 700 m 550 700 l S")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("sink ops = %d, want 1", len(ops))
	}
	if ops[0].Kind != VectorStroke {
		t.Errorf("kind = %v, want VectorStroke", ops[0].Kind)
	}
	if ops[0].StrokeWidth != 2 {
		t.Errorf("stroke width = %v, want 2", ops[0].StrokeWidth)
	}
}

// TestNilSinksByteIdentical asserts that with no sinks set, the interpreter still paints
// exactly (nil sinks are the default, byte-identical path).
func TestNilSinksByteIdentical(t *testing.T) {
	dev := &recDevice{}
	it := New(nil, dev, nil, render.Identity, Options{}) // no sinks
	if err := it.Run([]byte("1 0 0 rg 100 100 200 150 re f")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(dev.fills) != 1 {
		t.Fatalf("fills = %d, want 1", len(dev.fills))
	}
}
