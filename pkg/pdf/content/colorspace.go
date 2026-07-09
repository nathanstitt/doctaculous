package content

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

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
	// csPattern is the /Pattern color space. Under it, sc/scn carry a pattern
	// name rather than color components; the fill becomes a pattern (a shading
	// pattern sets a shading fill source, see gstate.fillShading).
	csPattern
)

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
		return render.GrayToRGBA(get(0))
	case deviceRGB:
		return render.RGBToRGBA(get(0), get(1), get(2))
	case deviceCMYK:
		return render.CMYKToRGBA(get(0), get(1), get(2), get(3))
	default:
		// Approximate by count.
		switch len(comps) {
		case 1:
			return render.GrayToRGBA(get(0))
		case 4:
			return render.CMYKToRGBA(get(0), get(1), get(2), get(3))
		default:
			return render.RGBToRGBA(get(0), get(1), get(2))
		}
	}
}
