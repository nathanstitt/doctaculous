package filter

import (
	"image"
	"math"
)

// Flood fills subregion with a single straight-alpha color, ignoring its
// input entirely — the feFlood primitive.
//
// r, g, b are the flood-color's channels in [0,1], already in space (the
// caller converts, since flood-color is authored in sRGB and a linearRGB
// filter must convert it like any other color), and a is flood-opacity times
// the color's own alpha.
//
// The result covers exactly subregion: feFlood is the primitive that proves
// the region math, since every pixel of its output is decided by the region
// alone.
func Flood(subregion image.Rectangle, r, g, b, a float32, space ColorSpace) *Buffer {
	out := NewBuffer(subregion, space)
	if a <= 0 {
		return out // fully transparent flood: leave the buffer clear
	}
	for i := 0; i+3 < len(out.Pix); i += 4 {
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = r, g, b, a
	}
	return out
}

// Offset translates in by (dx, dy) device-space pixels, clipped to subregion —
// the feOffset primitive.
//
// dx/dy are already resolved to device units by the caller (primitiveUnits
// and the element's transform applied), and may be FRACTIONAL. A whole-pixel
// offset takes an exact copy path; a fractional one bilinearly resamples.
//
// Resampling rather than snapping is what the reference renderer does, and
// the corpus pins it to the exact alpha value. In feOffset's
// fractional-offset fixture (dx="20.25" dy="40.7", i.e. 50.625 and 101.75
// device pixels at that fixture's scale) the shifted rect's left edge lands
// at x=100.625, and resvg's reference PNG has alpha 95 there — 0.375 × 255,
// precisely the sub-pixel coverage that survives. Its top edge lands at
// y=151.75 and carries alpha 63 — 0.25 × 255. A snapping implementation
// leaves both pixels fully transparent and loses the antialiased boundary.
//
// Sampling happens on PREMULTIPLIED values even though the buffer holds
// straight alpha: interpolating straight color across an edge where alpha
// falls to zero would weight the transparent pixel's meaningless color
// equally with the opaque one, fringing the edge toward whatever that color
// happens to be. Premultiplying, interpolating, then un-premultiplying is
// the standard fix and is what makes the resampled edge match.
//
// A sample whose source lies outside in reads transparent black (Buffer.At's
// documented out-of-bounds value), which is the edge behavior the spec calls
// for: shifting content in exposes transparency rather than smearing the
// boundary pixel.
func Offset(in *Buffer, dx, dy float64, subregion image.Rectangle) *Buffer {
	space := LinearRGB
	if in != nil {
		space = in.Space
	}
	out := NewBuffer(subregion, space)
	if in == nil {
		return out
	}

	// A whole-pixel offset needs no resampling, and the exact path keeps the
	// overwhelmingly common integer case bit-exact.
	if dx == math.Trunc(dx) && dy == math.Trunc(dy) {
		ix, iy := int(dx), int(dy)
		for y := subregion.Min.Y; y < subregion.Max.Y; y++ {
			for x := subregion.Min.X; x < subregion.Max.X; x++ {
				r, g, b, a := in.At(x-ix, y-iy)
				if a == 0 && r == 0 && g == 0 && b == 0 {
					continue
				}
				out.Set(x, y, r, g, b, a)
			}
		}
		return out
	}

	for y := subregion.Min.Y; y < subregion.Max.Y; y++ {
		for x := subregion.Min.X; x < subregion.Max.X; x++ {
			sx, sy := float64(x)-dx, float64(y)-dy
			fx, fy := math.Floor(sx), math.Floor(sy)
			tx, ty := float32(sx-fx), float32(sy-fy)
			x0, y0 := int(fx), int(fy)

			r00, g00, b00, a00 := premulAt(in, x0, y0)
			r10, g10, b10, a10 := premulAt(in, x0+1, y0)
			r01, g01, b01, a01 := premulAt(in, x0, y0+1)
			r11, g11, b11, a11 := premulAt(in, x0+1, y0+1)

			a := lerp2(a00, a10, a01, a11, tx, ty)
			if a <= 0 {
				continue
			}
			r := lerp2(r00, r10, r01, r11, tx, ty)
			g := lerp2(g00, g10, g01, g11, tx, ty)
			b := lerp2(b00, b10, b01, b11, tx, ty)
			// Back to straight alpha, the convention Buffer stores.
			out.Set(x, y, r/a, g/a, b/a, a)
		}
	}
	return out
}

// premulAt reads one pixel from b as PREMULTIPLIED channels, for
// interpolation. See Offset's doc comment for why premultiplying before
// interpolating is required at a transparent edge.
func premulAt(b *Buffer, x, y int) (r, g, bl, a float32) {
	r, g, bl, a = b.At(x, y)
	return r * a, g * a, bl * a, a
}

// lerp2 bilinearly interpolates the four samples of one channel, with tx/ty
// the fractional position within the sample square.
func lerp2(v00, v10, v01, v11, tx, ty float32) float32 {
	top := v00 + (v10-v00)*tx
	bot := v01 + (v11-v01)*tx
	return top + (bot-top)*ty
}

// Crop restricts in to subregion, returning a buffer covering exactly
// subregion with anything outside it dropped.
//
// Every primitive's output is clipped to its own subregion per SVG (the
// x/y/width/height on the primitive element), so this is applied after any
// primitive whose natural output extends further. It returns in unchanged
// when the bounds already match, so the common no-subregion case costs
// nothing.
func Crop(in *Buffer, subregion image.Rectangle) *Buffer {
	if in == nil {
		return NewBuffer(subregion, LinearRGB)
	}
	if in.Rect == subregion {
		return in
	}
	out := NewBuffer(subregion, in.Space)
	clipped := subregion.Intersect(in.Rect)
	for y := clipped.Min.Y; y < clipped.Max.Y; y++ {
		for x := clipped.Min.X; x < clipped.Max.X; x++ {
			r, g, b, a := in.At(x, y)
			out.Set(x, y, r, g, b, a)
		}
	}
	return out
}

// AlphaOnly returns in's alpha channel with the color channels zeroed — the
// SourceAlpha implicit input.
//
// Black is the correct color for the zeroed channels in BOTH color spaces
// (linear 0 and sRGB 0 are the same value), so this needs no conversion and
// carries in's space through unchanged.
func AlphaOnly(in *Buffer) *Buffer {
	if in == nil {
		return nil
	}
	out := NewBuffer(in.Rect, in.Space)
	for i := 3; i < len(in.Pix); i += 4 {
		out.Pix[i] = in.Pix[i]
	}
	return out
}
