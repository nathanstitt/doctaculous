package filter

import (
	"image"
	"image/color"
	"testing"
)

// mustOpaque builds a fully opaque RGBA, for readable test fixtures.
func mustOpaque(r, g, b uint8) color.RGBA {
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

// TestFloodFillsExactlyTheSubregion is the region-math proof feFlood exists
// to provide: every pixel inside the subregion carries the flood color, and
// the buffer extends no further, so a region-math error shows up as a
// wrong-sized result rather than a subtle shade.
func TestFloodFillsExactlyTheSubregion(t *testing.T) {
	region := image.Rect(3, 5, 9, 11)
	out := Flood(region, 0.25, 0.5, 0.75, 1, SRGB)

	if out.Bounds() != region {
		t.Fatalf("flood bounds = %v, want %v", out.Bounds(), region)
	}
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			r, g, b, a := out.At(x, y)
			if r != 0.25 || g != 0.5 || b != 0.75 || a != 1 {
				t.Fatalf("pixel (%d,%d) = %v %v %v %v, want the flood color", x, y, r, g, b, a)
			}
		}
	}
	// Just outside every edge must read transparent.
	for _, p := range [][2]int{{2, 5}, {9, 5}, {3, 4}, {3, 11}} {
		if _, _, _, a := out.At(p[0], p[1]); a != 0 {
			t.Errorf("pixel (%d,%d) outside the region has alpha %v, want 0", p[0], p[1], a)
		}
	}
}

// TestFloodAppliesOpacity confirms flood-opacity reaches the alpha channel
// rather than being dropped or applied to the color channels.
func TestFloodAppliesOpacity(t *testing.T) {
	out := Flood(image.Rect(0, 0, 2, 2), 1, 0, 0, 0.5, SRGB)
	r, g, b, a := out.At(0, 0)
	if a != 0.5 {
		t.Errorf("alpha = %v, want 0.5 (flood-opacity)", a)
	}
	if r != 1 || g != 0 || b != 0 {
		t.Errorf("color = %v %v %v, want the unmodified flood color (straight alpha, not premultiplied)", r, g, b)
	}
}

// TestFloodFullyTransparentIsEmpty confirms a zero flood-opacity produces a
// clear buffer rather than opaque black.
func TestFloodFullyTransparentIsEmpty(t *testing.T) {
	out := Flood(image.Rect(0, 0, 2, 2), 1, 1, 1, 0, SRGB)
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if _, _, _, a := out.At(x, y); a != 0 {
				t.Fatalf("pixel (%d,%d) alpha = %v, want 0", x, y, a)
			}
		}
	}
}

// TestOffsetShiftsByExactlyDxDy pins feOffset's translation for whole-pixel
// offsets in all four directions, including NEGATIVE values (which an
// implementation using unsigned arithmetic or clamping too early gets wrong).
func TestOffsetShiftsByExactlyDxDy(t *testing.T) {
	region := image.Rect(0, 0, 10, 10)
	in := NewBuffer(region, SRGB)
	in.Set(5, 5, 1, 0, 0, 1)

	cases := []struct {
		name            string
		dx, dy          float64
		wantX, wantY    int
		shouldBePresent bool
	}{
		{"positive", 2, 3, 7, 8, true},
		{"negative", -2, -3, 3, 2, true},
		{"only dx", 4, 0, 9, 5, true},
		{"only dy", 0, -5, 5, 0, true},
		{"zero", 0, 0, 5, 5, true},
		// Shifted entirely outside the subregion: the pixel is gone, and
		// nothing wraps around to the other edge.
		{"off the edge", 20, 0, 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := Offset(in, c.dx, c.dy, region)
			found := 0
			var fx, fy int
			for y := region.Min.Y; y < region.Max.Y; y++ {
				for x := region.Min.X; x < region.Max.X; x++ {
					if _, _, _, a := out.At(x, y); a > 0 {
						found++
						fx, fy = x, y
					}
				}
			}
			if !c.shouldBePresent {
				if found != 0 {
					t.Fatalf("offset (%v,%v) left %d pixels at (%d,%d); want the content shifted out entirely",
						c.dx, c.dy, found, fx, fy)
				}
				return
			}
			if found != 1 {
				t.Fatalf("offset (%v,%v) produced %d lit pixels, want exactly 1", c.dx, c.dy, found)
			}
			if fx != c.wantX || fy != c.wantY {
				t.Errorf("offset (%v,%v) moved the pixel to (%d,%d), want (%d,%d)", c.dx, c.dy, fx, fy, c.wantX, c.wantY)
			}
		})
	}
}

// TestOffsetPreservesColorAndAlpha confirms a translation moves values
// UNCHANGED — an implementation that resampled a whole-pixel offset, or that
// interpolated premultiplied values without dividing back out, would shift
// the color.
func TestOffsetPreservesColorAndAlpha(t *testing.T) {
	region := image.Rect(0, 0, 6, 6)
	in := NewBuffer(region, SRGB)
	in.Set(1, 1, 0.2, 0.4, 0.6, 0.8)

	out := Offset(in, 2, 2, region)
	r, g, b, a := out.At(3, 3)
	if r != 0.2 || g != 0.4 || b != 0.6 || a != 0.8 {
		t.Errorf("moved pixel = %v %v %v %v, want the source values unchanged", r, g, b, a)
	}
}

// TestOffsetFractionalResamples pins the sub-pixel behavior the resvg
// fractional-offset fixture requires: a half-pixel shift spreads a hard edge
// across two pixels with the coverage the offset implies, instead of snapping.
func TestOffsetFractionalResamples(t *testing.T) {
	region := image.Rect(0, 0, 6, 1)
	in := NewBuffer(region, SRGB)
	in.Set(2, 0, 1, 1, 1, 1)

	out := Offset(in, 0.5, 0, region)
	_, _, _, a2 := out.At(2, 0)
	_, _, _, a3 := out.At(3, 0)
	if a2 <= 0 || a3 <= 0 {
		t.Fatalf("a half-pixel offset did not spread coverage: alpha at x=2 is %v, at x=3 is %v", a2, a3)
	}
	if d := a2 - a3; d > 1e-6 || d < -1e-6 {
		t.Errorf("a half-pixel offset should split coverage evenly; got %v and %v", a2, a3)
	}
	if total := a2 + a3; total < 0.99 || total > 1.01 {
		t.Errorf("resampling lost or gained coverage: total alpha %v, want ~1", total)
	}
}

// TestOffsetClipsToSubregion confirms a primitive's output never escapes its
// own subregion even when the shifted content would land outside it.
func TestOffsetClipsToSubregion(t *testing.T) {
	in := NewBuffer(image.Rect(0, 0, 10, 10), SRGB)
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			in.Set(x, y, 1, 1, 1, 1)
		}
	}
	sub := image.Rect(2, 2, 5, 5)
	out := Offset(in, 0, 0, sub)
	if out.Bounds() != sub {
		t.Fatalf("bounds = %v, want the subregion %v", out.Bounds(), sub)
	}
	if _, _, _, a := out.At(6, 6); a != 0 {
		t.Errorf("a pixel outside the subregion is lit (alpha %v)", a)
	}
}

// TestOffsetNilInputIsEmpty confirms a missing input degrades to a clear
// buffer rather than panicking — the never-panic-on-malformed-input rule.
func TestOffsetNilInputIsEmpty(t *testing.T) {
	out := Offset(nil, 3, 3, image.Rect(0, 0, 4, 4))
	if out == nil {
		t.Fatal("Offset(nil, ...) returned nil; want an empty buffer")
	}
	if _, _, _, a := out.At(1, 1); a != 0 {
		t.Errorf("alpha = %v, want 0", a)
	}
}

// TestAlphaOnlyZeroesColor pins the SourceAlpha implicit input: alpha
// survives, color goes to black. A implementation that returned the input
// unchanged, or that zeroed alpha instead, would fail here.
func TestAlphaOnlyZeroesColor(t *testing.T) {
	in := NewBuffer(image.Rect(0, 0, 2, 2), SRGB)
	in.Set(0, 0, 0.9, 0.8, 0.7, 0.6)

	out := AlphaOnly(in)
	r, g, b, a := out.At(0, 0)
	if a != 0.6 {
		t.Errorf("alpha = %v, want the input's 0.6 preserved", a)
	}
	if r != 0 || g != 0 || b != 0 {
		t.Errorf("color = %v %v %v, want black", r, g, b)
	}
}

// TestCropRestrictsBounds confirms Crop trims to the requested rect and keeps
// the pixels that survive.
func TestCropRestrictsBounds(t *testing.T) {
	in := NewBuffer(image.Rect(0, 0, 8, 8), SRGB)
	in.Set(1, 1, 1, 0, 0, 1)
	in.Set(5, 5, 0, 1, 0, 1)

	out := Crop(in, image.Rect(4, 4, 8, 8))
	if out.Bounds() != image.Rect(4, 4, 8, 8) {
		t.Fatalf("bounds = %v", out.Bounds())
	}
	if _, _, _, a := out.At(1, 1); a != 0 {
		t.Error("a pixel outside the crop survived")
	}
	if _, g, _, a := out.At(5, 5); a != 1 || g != 1 {
		t.Error("a pixel inside the crop was lost")
	}
}

// TestBufferOutOfBoundsReadsTransparent pins the contract every kernel
// primitive relies on for its edge behavior: reading outside a buffer yields
// transparent black rather than panicking or clamping to the edge pixel.
func TestBufferOutOfBoundsReadsTransparent(t *testing.T) {
	b := NewBuffer(image.Rect(2, 2, 4, 4), SRGB)
	b.Set(2, 2, 1, 1, 1, 1)
	for _, p := range [][2]int{{-100, -100}, {1, 2}, {4, 4}, {1000, 1000}} {
		r, g, bl, a := b.At(p[0], p[1])
		if r != 0 || g != 0 || bl != 0 || a != 0 {
			t.Errorf("At(%d,%d) = %v %v %v %v, want transparent black", p[0], p[1], r, g, bl, a)
		}
	}
	// A write outside the bounds must be dropped, not corrupt a neighbor.
	b.Set(100, 100, 1, 1, 1, 1)
	if _, _, _, a := b.At(2, 2); a != 1 {
		t.Error("an out-of-bounds write disturbed an in-bounds pixel")
	}
}

// TestNewBufferDegenerateRect confirms an empty or inverted rect yields a
// usable zero-pixel buffer instead of panicking on a negative allocation.
func TestNewBufferDegenerateRect(t *testing.T) {
	for _, r := range []image.Rectangle{
		{},
		image.Rect(5, 5, 5, 5),
		{Min: image.Point{X: 10, Y: 10}, Max: image.Point{X: 2, Y: 2}},
	} {
		b := NewBuffer(r, SRGB)
		if b == nil {
			t.Fatalf("NewBuffer(%v) returned nil", r)
		}
		if _, _, _, a := b.At(0, 0); a != 0 {
			t.Errorf("NewBuffer(%v) has a lit pixel", r)
		}
	}
}
