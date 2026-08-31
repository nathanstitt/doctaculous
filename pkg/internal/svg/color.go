package svg

import (
	"image/color"

	"github.com/nathanstitt/omnidoc/pkg/internal/css"
)

// parseColorValue parses an SVG/CSS color value: a named color keyword
// (case-insensitive, the full CSS Color 4 table), a hex color (#rgb, #rgba,
// #rrggbb, #rrggbbaa), or an rgb()/rgba()/hsl()/hsla() functional notation
// (comma or space syntax, integer or percentage channels, optional alpha as a
// fourth argument or after a "/"). "url(...)" paint-server references and any
// unrecognized value return ok=false so the caller can degrade gracefully (e.g.
// skip the fill/stroke).
//
// The grammar itself lives in pkg/css (color.go) and is shared with the HTML
// cascade. It originated here, but a colour keyword or an rgba() has exactly one
// correct meaning in either document type, and keeping two implementations had
// already produced a real divergence: values this package accepted were dropped
// outright by the CSS cascade, painting nothing. This thin wrapper remains so
// pkg/svg's call sites read in SVG terms and so the CSS dependency stays at one
// point rather than sprinkled through the style/gradient/filter parsers.
func parseColorValue(s string) (color.RGBA, bool) {
	return css.ParseColorValue(s)
}

// clamp constrains v to [lo,hi]. It is used well beyond colour parsing (opacity,
// gradient offsets, letter/word spacing), so it lives here alongside its
// original caller rather than moving out with the grammar.
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
