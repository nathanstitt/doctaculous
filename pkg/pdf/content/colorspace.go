package content

import "image/color"

// colorSpace is a simplified color-space model sufficient for v1: the device
// spaces plus a fallback for everything else (treated by component count).
type colorSpace int

const (
	deviceGray colorSpace = iota
	deviceRGB
	deviceCMYK
	// csOther is any color space we approximate by component count (ICCBased,
	// CalRGB, Lab, Separation, etc.). The interpreter maps components to RGB as
	// best it can.
	csOther
)

// grayToRGBA converts a single gray component (0..1) to RGBA.
func grayToRGBA(g float64) color.RGBA {
	v := clamp8(g)
	return color.RGBA{v, v, v, 255}
}

// rgbToRGBA converts r,g,b components (0..1) to RGBA.
func rgbToRGBA(r, g, b float64) color.RGBA {
	return color.RGBA{clamp8(r), clamp8(g), clamp8(b), 255}
}

// cmykToRGBA converts c,m,y,k components (0..1) to RGBA using the naive
// device conversion r = (1-c)(1-k).
func cmykToRGBA(c, m, y, k float64) color.RGBA {
	r := (1 - c) * (1 - k)
	g := (1 - m) * (1 - k)
	b := (1 - y) * (1 - k)
	return color.RGBA{clamp8(r), clamp8(g), clamp8(b), 255}
}

// colorFromComponents maps a slice of components to RGBA according to cs. It is
// tolerant of wrong counts (pads/truncates) so malformed operands never panic.
func colorFromComponents(cs colorSpace, comps []float64) color.RGBA {
	get := func(i int) float64 {
		if i < len(comps) {
			return comps[i]
		}
		return 0
	}
	switch cs {
	case deviceGray:
		return grayToRGBA(get(0))
	case deviceRGB:
		return rgbToRGBA(get(0), get(1), get(2))
	case deviceCMYK:
		return cmykToRGBA(get(0), get(1), get(2), get(3))
	default:
		// Approximate by count.
		switch len(comps) {
		case 1:
			return grayToRGBA(get(0))
		case 4:
			return cmykToRGBA(get(0), get(1), get(2), get(3))
		default:
			return rgbToRGBA(get(0), get(1), get(2))
		}
	}
}

func clamp8(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	default:
		return uint8(v*255 + 0.5)
	}
}
