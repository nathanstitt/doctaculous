package filter

import (
	"image"
	"image/color"
)

// Buffer is one filter primitive's pixel result: a float32 RGBA raster in
// STRAIGHT (non-premultiplied) alpha, in the color space Space records.
//
// Three deliberate departures from *image.RGBA, each forced by the filter
// spec:
//
//   - float32, not uint8. A filter graph chains primitives, and rounding to 8
//     bits between every stage compounds visibly (the linear/sRGB round trip
//     alone loses a bit in the dark end, before any primitive runs). Keeping
//     the intermediates in float also lets a primitive legitimately produce
//     out-of-range values that a later stage brings back into range.
//   - STRAIGHT alpha, not premultiplied. SVG defines every primitive on
//     non-premultiplied values, and the difference is not academic: a
//     partially transparent pixel's color channels are scaled by alpha in
//     premultiplied form, so offsetting or blurring premultiplied pixels
//     silently darkens toward transparent edges.
//   - An explicit origin (Rect). A filter region rarely starts at the device
//     origin, and primitives (feOffset especially) must know where their
//     pixels actually live on the device to composite back correctly.
//
// Pix is row-major, 4 float32 per pixel (R, G, B, A), Stride floats per row.
type Buffer struct {
	Pix    []float32
	Stride int
	Rect   image.Rectangle
	// Space records which color space Pix currently holds, so a conversion
	// is never applied twice or skipped. Primitives that are color-space
	// sensitive assert on it rather than assuming.
	Space ColorSpace
}

// NewBuffer allocates a fully transparent Buffer covering r in the given
// space. A degenerate (empty) r yields a Buffer with no pixels, which every
// primitive handles as "nothing to do" rather than a special case.
func NewBuffer(r image.Rectangle, space ColorSpace) *Buffer {
	if r.Dx() < 0 || r.Dy() < 0 {
		r = image.Rectangle{Min: r.Min, Max: r.Min}
	}
	return &Buffer{
		Pix:    make([]float32, 4*r.Dx()*r.Dy()),
		Stride: 4 * r.Dx(),
		Rect:   r,
		Space:  space,
	}
}

// Bounds reports the buffer's device-space extent.
func (b *Buffer) Bounds() image.Rectangle { return b.Rect }

// At returns the straight-alpha RGBA at device-space (x, y), or zeros
// (transparent black) outside the buffer — the SVG-correct value for any
// pixel a filter reads beyond its input's extent, which is what makes
// out-of-range reads in feOffset and every future kernel primitive safe by
// construction rather than by an explicit bounds test at each call site.
func (b *Buffer) At(x, y int) (r, g, bl, a float32) {
	if b == nil || !(image.Point{X: x, Y: y}).In(b.Rect) {
		return 0, 0, 0, 0
	}
	i := b.index(x, y)
	return b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3]
}

// Set writes the straight-alpha RGBA at device-space (x, y), ignoring a
// write outside the buffer.
func (b *Buffer) Set(x, y int, r, g, bl, a float32) {
	if b == nil || !(image.Point{X: x, Y: y}).In(b.Rect) {
		return
	}
	i := b.index(x, y)
	b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = r, g, bl, a
}

// index returns the Pix offset of device-space pixel (x, y), which the caller
// must have already confirmed is inside Rect.
func (b *Buffer) index(x, y int) int {
	return (y-b.Rect.Min.Y)*b.Stride + (x-b.Rect.Min.X)*4
}

// FromRGBA converts a rasterized device image into a filter Buffer over
// region: it un-premultiplies each pixel (image.RGBA is premultiplied; filters
// are defined on straight alpha) and converts into space.
//
// region selects the part of src the filter operates on and becomes the
// result's Rect, so a filter region smaller than the device does not carry
// the whole canvas through every primitive. Pixels of region outside src stay
// transparent black.
func FromRGBA(src *image.RGBA, region image.Rectangle, space ColorSpace) *Buffer {
	out := NewBuffer(region, space)
	if src == nil {
		return out
	}
	clipped := region.Intersect(src.Bounds())
	for y := clipped.Min.Y; y < clipped.Max.Y; y++ {
		for x := clipped.Min.X; x < clipped.Max.X; x++ {
			c := src.RGBAAt(x, y)
			if c.A == 0 {
				continue // already transparent black
			}
			r, g, b := unpremultiply(c)
			if space == LinearRGB {
				out.Set(x, y,
					float32(srgbToLinear8(r)), float32(srgbToLinear8(g)), float32(srgbToLinear8(b)),
					float32(c.A)/255)
				continue
			}
			out.Set(x, y,
				float32(r)/255, float32(g)/255, float32(b)/255, float32(c.A)/255)
		}
	}
	return out
}

// ToRGBA converts a filter Buffer back into a premultiplied *image.RGBA,
// converting out of b.Space into sRGB and clamping every channel into range.
//
// The result's bounds are b.Rect, so the caller composites it at that
// device-space position.
func (b *Buffer) ToRGBA() *image.RGBA {
	if b == nil {
		return image.NewRGBA(image.Rectangle{})
	}
	out := image.NewRGBA(b.Rect)
	for y := b.Rect.Min.Y; y < b.Rect.Max.Y; y++ {
		for x := b.Rect.Min.X; x < b.Rect.Max.X; x++ {
			r, g, bl, a := b.At(x, y)
			if a <= 0 {
				continue
			}
			if a > 1 {
				a = 1
			}
			var r8, g8, b8 uint8
			if b.Space == LinearRGB {
				r8, g8, b8 = linearToSRGB8(float64(r)), linearToSRGB8(float64(g)), linearToSRGB8(float64(bl))
			} else {
				r8, g8, b8 = clamp8(float64(r)*255), clamp8(float64(g)*255), clamp8(float64(bl)*255)
			}
			// Back to premultiplied, image.RGBA's convention.
			a8 := clamp8(float64(a) * 255)
			out.SetRGBA(x, y, color.RGBA{
				R: mul8(r8, a8), G: mul8(g8, a8), B: mul8(b8, a8), A: a8,
			})
		}
	}
	return out
}

// ConvertTo converts b in place into space, doing nothing when it is already
// there. This is what lets a graph mix primitives that disagree about
// color-interpolation-filters: each primitive's input is brought into that
// primitive's own space before it runs.
func (b *Buffer) ConvertTo(space ColorSpace) {
	if b == nil || b.Space == space {
		return
	}
	for i := 0; i+3 < len(b.Pix); i += 4 {
		if space == LinearRGB {
			b.Pix[i] = float32(srgbToLinear(float64(b.Pix[i])))
			b.Pix[i+1] = float32(srgbToLinear(float64(b.Pix[i+1])))
			b.Pix[i+2] = float32(srgbToLinear(float64(b.Pix[i+2])))
		} else {
			b.Pix[i] = float32(linearToSRGB(float64(b.Pix[i])))
			b.Pix[i+1] = float32(linearToSRGB(float64(b.Pix[i+1])))
			b.Pix[i+2] = float32(linearToSRGB(float64(b.Pix[i+2])))
		}
	}
	b.Space = space
}

// unpremultiply recovers straight 8-bit color channels from a premultiplied
// image.RGBA pixel. c.A is known non-zero by the caller.
func unpremultiply(c color.RGBA) (r, g, b uint8) {
	if c.A == 255 {
		return c.R, c.G, c.B
	}
	div := func(v uint8) uint8 {
		n := (uint32(v)*255 + uint32(c.A)/2) / uint32(c.A)
		if n > 255 {
			return 255
		}
		return uint8(n)
	}
	return div(c.R), div(c.G), div(c.B)
}

// mul8 multiplies two 8-bit values as fractions of 255, rounding to nearest —
// the premultiplication step ToRGBA applies on the way out.
func mul8(v, a uint8) uint8 {
	return uint8((uint32(v)*uint32(a) + 127) / 255)
}
