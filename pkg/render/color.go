package render

import "image/color"

// Clamp8 maps a component in [0,1] to an 8-bit value, clamping out-of-range.
func Clamp8(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	default:
		return uint8(v*255 + 0.5)
	}
}

// GrayToRGBA converts a DeviceGray component to RGBA.
func GrayToRGBA(g float64) color.RGBA {
	v := Clamp8(g)
	return color.RGBA{v, v, v, 0xFF}
}

// RGBToRGBA converts DeviceRGB components to RGBA.
func RGBToRGBA(r, g, b float64) color.RGBA {
	return color.RGBA{Clamp8(r), Clamp8(g), Clamp8(b), 0xFF}
}

// CMYKToRGBA converts DeviceCMYK components to RGBA with the naive
// (1-c)(1-k) device conversion (no ICC).
func CMYKToRGBA(c, m, y, k float64) color.RGBA {
	return color.RGBA{Clamp8((1 - c) * (1 - k)), Clamp8((1 - m) * (1 - k)), Clamp8((1 - y) * (1 - k)), 0xFF}
}
