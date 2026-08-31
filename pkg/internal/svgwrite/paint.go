package svgwrite

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// colorAttr renders c's RGB as a hex color, and reports its alpha in [0,1]
// separately.
//
// SVG keeps color and opacity in different attributes (fill/fill-opacity),
// unlike render's single premultiplied-capable color.RGBA, so callers pair the
// two. Alpha is returned rather than baked into the color because compositing
// against an unknown backdrop is the viewer's job.
func colorAttr(c color.RGBA) (hex string, alpha float64) {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B), float64(c.A) / 255
}

// writeOpacity appends name="v" when v is not fully opaque. Emitting
// fill-opacity="1" on every element would roughly double the attribute volume
// for no visual difference.
func writeOpacity(sb *strings.Builder, name string, v float64) {
	if v >= 1 {
		return
	}
	if v < 0 {
		v = 0
	}
	fmt.Fprintf(sb, " %s=%q", name, formatCoord(v))
}

// fillRuleAttr renders a fill rule as SVG's fill-rule value. Nonzero is SVG's
// initial value, so it is emitted only when it differs.
func fillRuleAttr(r render.FillRule) string {
	if r == render.EvenOdd {
		return "evenodd"
	}
	return ""
}

// writeFillAttrs appends the fill presentation attributes for paint.
func writeFillAttrs(sb *strings.Builder, paint render.FillPaint) {
	hex, alpha := colorAttr(paint.Color)
	fmt.Fprintf(sb, " fill=%q", hex)
	writeOpacity(sb, "fill-opacity", alpha)
	if rule := fillRuleAttr(paint.Rule); rule != "" {
		fmt.Fprintf(sb, " fill-rule=%q", rule)
	}
}

// writeStrokeAttrs appends the stroke presentation attributes for paint.
//
// Every geometric value is already in device space (render.StrokePaint's
// contract), and the emitted <path> carries no transform of its own, so widths
// and dashes need no scaling here.
func writeStrokeAttrs(sb *strings.Builder, paint render.StrokePaint) {
	hex, alpha := colorAttr(paint.Color)
	fmt.Fprintf(sb, " fill=\"none\" stroke=%q", hex)
	writeOpacity(sb, "stroke-opacity", alpha)

	// SVG's initial stroke-width is 1; emit anything else, including 0 (a
	// deliberate hairline-free stroke the viewer must not default to 1).
	if paint.Width != 1 {
		fmt.Fprintf(sb, " stroke-width=%q", formatCoord(paint.Width))
	}
	if cap := capAttr(paint.Cap); cap != "" {
		fmt.Fprintf(sb, " stroke-linecap=%q", cap)
	}
	if join := joinAttr(paint.Join); join != "" {
		fmt.Fprintf(sb, " stroke-linejoin=%q", join)
	}
	// stroke-miterlimit only affects miter joins, and SVG's initial value is 4.
	if paint.Join == render.MiterJoin && paint.MiterLimit > 0 && paint.MiterLimit != 4 {
		fmt.Fprintf(sb, " stroke-miterlimit=%q", formatCoord(paint.MiterLimit))
	}
	writeDashAttrs(sb, paint.DashArray, paint.DashPhase)
}

// writeDashAttrs appends stroke-dasharray/stroke-dashoffset.
//
// An all-zero dash array means "solid" in PDF but is invalid in SVG (a viewer
// must treat a zero-sum array as solid, and some render nothing), so it is
// dropped rather than emitted.
func writeDashAttrs(sb *strings.Builder, dashes []float64, phase float64) {
	if len(dashes) == 0 {
		return
	}
	var sum float64
	for _, d := range dashes {
		if d < 0 {
			return // a negative dash is invalid; emit solid rather than junk
		}
		sum += d
	}
	if sum <= 0 {
		return
	}
	var da strings.Builder
	for i, d := range dashes {
		if i > 0 {
			da.WriteByte(',')
		}
		da.WriteString(formatCoord(d))
	}
	fmt.Fprintf(sb, " stroke-dasharray=%q", da.String())
	if phase != 0 {
		fmt.Fprintf(sb, " stroke-dashoffset=%q", formatCoord(phase))
	}
}

func capAttr(c render.LineCap) string {
	switch c {
	case render.RoundCap:
		return "round"
	case render.SquareCap:
		return "square"
	default:
		return "" // ButtCap is SVG's initial value
	}
}

func joinAttr(j render.LineJoin) string {
	switch j {
	case render.RoundJoin:
		return "round"
	case render.BevelJoin:
		return "bevel"
	default:
		return "" // MiterJoin is SVG's initial value
	}
}

// blendModes maps PDF /BM blend-mode names to CSS mix-blend-mode keywords.
//
// The two vocabularies cover the same set (pkg/render/blend.go implements it
// once for both the raster backend and SVG's <feBlend>), differing only in
// spelling: PDF uses CamelCase, CSS kebab-case. "Normal" and "Compatible" both
// mean source-over and are absent here so they emit no attribute.
var blendModes = map[string]string{
	"Multiply":   "multiply",
	"Screen":     "screen",
	"Overlay":    "overlay",
	"Darken":     "darken",
	"Lighten":    "lighten",
	"ColorDodge": "color-dodge",
	"ColorBurn":  "color-burn",
	"HardLight":  "hard-light",
	"SoftLight":  "soft-light",
	"Difference": "difference",
	"Exclusion":  "exclusion",
	"Hue":        "hue",
	"Saturation": "saturation",
	"Color":      "color",
	"Luminosity": "luminosity",
}

// blendAttr maps a PDF /BM name to its CSS mix-blend-mode keyword. It reports
// ok=false for source-over (no attribute needed) and for an unrecognized name,
// which the device logs as a degradation.
func blendAttr(name string) (css string, ok bool) {
	if name == "" || name == "Normal" || name == "Compatible" {
		return "", false
	}
	css, ok = blendModes[name]
	return css, ok
}
