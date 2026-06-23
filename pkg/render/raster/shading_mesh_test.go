package raster

import (
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

func TestMeshBarycentricInterpolation(t *testing.T) {
	// A right triangle with red, green, blue at its corners.
	tri := meshTriangle{v: [3]meshVertex{
		{x: 0, y: 0, c: color.RGBA{255, 0, 0, 255}},
		{x: 10, y: 0, c: color.RGBA{0, 255, 0, 255}},
		{x: 0, y: 10, c: color.RGBA{0, 0, 255, 255}},
	}}
	// At each vertex the color is the pure corner color.
	if c, ok := barycentricColor(&tri, 0, 0); !ok || c != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("corner A = %v ok=%v, want red", c, ok)
	}
	if c, ok := barycentricColor(&tri, 10, 0); !ok || c != (color.RGBA{0, 255, 0, 255}) {
		t.Fatalf("corner B = %v ok=%v, want green", c, ok)
	}
	if c, ok := barycentricColor(&tri, 0, 10); !ok || c != (color.RGBA{0, 0, 255, 255}) {
		t.Fatalf("corner C = %v ok=%v, want blue", c, ok)
	}
	// The centroid blends all three roughly evenly (~85 each).
	c, ok := barycentricColor(&tri, 10.0/3, 10.0/3)
	if !ok {
		t.Fatalf("centroid not painted")
	}
	if !near(c.R, 85, 2) || !near(c.G, 85, 2) || !near(c.B, 85, 2) {
		t.Fatalf("centroid = %v, want ~{85 85 85}", c)
	}
	// A point outside the triangle is not painted.
	if _, ok := barycentricColor(&tri, 9, 9); ok {
		t.Fatalf("(9,9) painted, want outside")
	}
}

// TestNewMeshShaderType4 builds a Type-4 mesh shader from a synthetic stream and
// confirms the decoded triangles interpolate at the corners. The stream is
// byte-aligned (8 bits each) so the packed records are easy to assemble.
func TestNewMeshShaderType4(t *testing.T) {
	// Decode: coords 0..255 → 0..255 user; colors 0..255 → 0..1.
	vtx := func(flag, x, y, r, g, b byte) []byte { return []byte{flag, x, y, r, g, b} }
	var data []byte
	data = append(data, vtx(0, 0, 0, 255, 0, 0)...)     // (0,0) red
	data = append(data, vtx(0, 100, 0, 0, 255, 0)...)   // (100,0) green
	data = append(data, vtx(0, 100, 100, 0, 0, 255)...) // (100,100) blue
	data = append(data, vtx(2, 0, 100, 255, 255, 0)...) // (0,100) yellow, flag 2

	dict := pdf.Dict{
		"ShadingType":       pdf.Integer(4),
		"ColorSpace":        pdf.Name("DeviceRGB"),
		"BitsPerCoordinate": pdf.Integer(8),
		"BitsPerComponent":  pdf.Integer(8),
		"BitsPerFlag":       pdf.Integer(8),
		"Decode": pdf.Array{
			pdf.Integer(0), pdf.Integer(255), pdf.Integer(0), pdf.Integer(255),
			pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1),
		},
	}
	stream := &pdf.Stream{Dict: dict, Raw: data}
	sh, err := newMeshShader(nil, dict, stream)
	if err != nil {
		t.Fatalf("newMeshShader: %v", err)
	}
	// Corner (0,0) → red; (100,0) → green; (0,100) → yellow.
	if c, ok := sh.ColorAt(1, 1); !ok || !near(c.R, 255, 8) || c.B > 8 {
		t.Fatalf("near (0,0) = %v ok=%v, want ~red", c, ok)
	}
	if c, ok := sh.ColorAt(99, 1); !ok || !near(c.G, 255, 8) {
		t.Fatalf("near (100,0) = %v ok=%v, want ~green", c, ok)
	}
	if c, ok := sh.ColorAt(1, 99); !ok || !near(c.R, 255, 8) || !near(c.G, 255, 8) {
		t.Fatalf("near (0,100) = %v ok=%v, want ~yellow", c, ok)
	}
	// Outside the square → not painted.
	if _, ok := sh.ColorAt(200, 200); ok {
		t.Fatalf("(200,200) painted, want outside the mesh")
	}
}

func TestNewMeshShaderTruncatedDoesNotPanic(t *testing.T) {
	// A stream far too short for even one triangle must error gracefully (no
	// triangles decoded), never panic.
	dict := pdf.Dict{
		"ShadingType":       pdf.Integer(4),
		"ColorSpace":        pdf.Name("DeviceRGB"),
		"BitsPerCoordinate": pdf.Integer(16),
		"BitsPerComponent":  pdf.Integer(8),
		"BitsPerFlag":       pdf.Integer(8),
		"Decode": pdf.Array{
			pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1),
			pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1),
		},
	}
	stream := &pdf.Stream{Dict: dict, Raw: []byte{0x00}} // 1 byte: nothing usable
	if _, err := newMeshShader(nil, dict, stream); err == nil {
		t.Fatalf("expected error for truncated mesh, got nil")
	}
}

func TestNewMeshShaderType7DoesNotPanic(t *testing.T) {
	// A tensor-product (Type 7) patch stream is tessellated approximately; this
	// asserts the patch path runs without panicking on a minimal flag-0 patch.
	// 16 control points + 4 colors, all byte-aligned.
	var data []byte
	data = append(data, 0) // flag 0
	// 16 control points (x,y) bytes; place them on a unit-ish grid.
	for i := 0; i < 16; i++ {
		data = append(data, byte((i%4)*80), byte((i/4)*80))
	}
	// 4 corner colors (r,g,b).
	corners := [][3]byte{{255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {255, 255, 0}}
	for _, c := range corners {
		data = append(data, c[0], c[1], c[2])
	}
	dict := pdf.Dict{
		"ShadingType":       pdf.Integer(7),
		"ColorSpace":        pdf.Name("DeviceRGB"),
		"BitsPerCoordinate": pdf.Integer(8),
		"BitsPerComponent":  pdf.Integer(8),
		"BitsPerFlag":       pdf.Integer(8),
		"Decode": pdf.Array{
			pdf.Integer(0), pdf.Integer(255), pdf.Integer(0), pdf.Integer(255),
			pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1),
		},
	}
	stream := &pdf.Stream{Dict: dict, Raw: data}
	sh, err := newMeshShader(nil, dict, stream)
	if err != nil {
		t.Fatalf("type 7 patch: %v", err)
	}
	// Somewhere inside the patch grid must paint a color.
	if _, ok := sh.ColorAt(40, 40); !ok {
		t.Fatalf("type 7 patch painted nothing at (40,40)")
	}
}

func TestNewMeshShaderRejectsBadParams(t *testing.T) {
	dict := pdf.Dict{
		"ShadingType":       pdf.Integer(4),
		"ColorSpace":        pdf.Name("DeviceRGB"),
		"BitsPerCoordinate": pdf.Integer(0), // invalid
		"BitsPerComponent":  pdf.Integer(8),
		"BitsPerFlag":       pdf.Integer(8),
		"Decode":            pdf.Array{pdf.Integer(0), pdf.Integer(1)},
	}
	stream := &pdf.Stream{Dict: dict, Raw: []byte{0, 0, 0, 0, 0, 0}}
	if _, err := newMeshShader(nil, dict, stream); err == nil {
		t.Fatalf("expected error for bad BitsPerCoordinate, got nil")
	}
}
