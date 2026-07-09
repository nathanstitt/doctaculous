package pageres

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// TestFormMatrix pins the all-or-nothing /Matrix parse: a well-formed 6-number
// array yields that matrix, and anything else — absent, wrong length, or any
// non-numeric element — degrades to Identity (the PDF default) rather than a
// partially-zeroed matrix.
func TestFormMatrix(t *testing.T) {
	doc := &pdf.Document{}
	tests := []struct {
		name string
		in   pdf.Object
		want render.Matrix
	}{
		{
			name: "valid six numbers",
			in:   pdf.Array{pdf.Integer(1), pdf.Real(0.5), pdf.Integer(0), pdf.Integer(2), pdf.Real(10.25), pdf.Integer(-3)},
			want: render.Matrix{A: 1, B: 0.5, C: 0, D: 2, E: 10.25, F: -3},
		},
		{
			name: "wrong length",
			in:   pdf.Array{pdf.Integer(1), pdf.Integer(0), pdf.Integer(0), pdf.Integer(1)},
			want: render.Identity,
		},
		{
			// The numeric elements deliberately spell a non-Identity matrix
			// ({A:2, D:2, F:7} if the Name were zeroed), so this case fails under
			// per-element silent-zeroing semantics and genuinely pins the
			// all-or-nothing contract.
			name: "non-numeric element",
			in:   pdf.Array{pdf.Integer(2), pdf.Integer(0), pdf.Integer(0), pdf.Integer(2), pdf.Name("oops"), pdf.Integer(7)},
			want: render.Identity,
		},
		{
			name: "absent",
			in:   nil,
			want: render.Identity,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormMatrix(doc, tt.in); got != tt.want {
				t.Fatalf("FormMatrix(%#v) = %+v; want %+v", tt.in, got, tt.want)
			}
		})
	}
}

// TestFormBBox pins /BBox normalization and graceful degradation: reversed
// corners come back min/max ordered, and an absent or wrong-length array
// yields nil (degrade to no clip).
func TestFormBBox(t *testing.T) {
	doc := &pdf.Document{}

	got := FormBBox(doc, pdf.Array{pdf.Real(100), pdf.Integer(50), pdf.Integer(10), pdf.Real(5)})
	want := [4]float64{10, 5, 100, 50}
	if got == nil || *got != want {
		t.Fatalf("FormBBox(reversed corners) = %v; want %v", got, want)
	}

	if got := FormBBox(doc, pdf.Array{pdf.Integer(0), pdf.Integer(0), pdf.Integer(10)}); got != nil {
		t.Fatalf("FormBBox(3 elements) = %v; want nil", *got)
	}
	if got := FormBBox(doc, nil); got != nil {
		t.Fatalf("FormBBox(nil) = %v; want nil", *got)
	}
}
