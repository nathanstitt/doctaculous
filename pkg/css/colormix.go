package css

import (
	"image/color"
	"math"
	"strings"
)

// color-mix() — CSS Color Module Level 5 §3. Mixes two colours in a named
// interpolation space, e.g. `color-mix(in oklab, red 30%, blue)`.
//
// The space matters and is not a formality: mixing red and blue gives (128,0,128) in
// srgb, (188,0,188) in srgb-linear, (255,0,255) in hsl, and (140,83,162) in oklab.
// Every expected value in colormix_test.go was captured from Chrome rather than
// derived here, because a transposed conversion matrix produces colours that look
// plausible and are quietly wrong.
//
// Implemented: srgb, srgb-linear, hsl, hwb, lab, lch, oklab, oklch, xyz, xyz-d50,
// xyz-d65, plus all four hue-interpolation modes (shorter/longer/increasing/
// decreasing) for the polar spaces. An unknown space makes the whole value fail to
// parse, so the declaration is dropped per CSS error handling and the previous value
// stands — which is what a browser does.

// mixSpace identifies a color-mix() interpolation space.
type mixSpace int

const (
	mixSRGB mixSpace = iota
	mixSRGBLinear
	mixHSL
	mixHWB
	mixLab
	mixLCH
	mixOKLab
	mixOKLCH
	mixXYZD65
	mixXYZD50
)

// polar reports whether the space interpolates hue on a circle, which needs a
// hue-interpolation mode and wraps at 360 degrees.
func (s mixSpace) polar() bool {
	return s == mixHSL || s == mixHWB || s == mixLCH || s == mixOKLCH
}

// hueMode is the CSS Color 4 §12.4 hue-interpolation method.
type hueMode int

const (
	hueShorter hueMode = iota // the default: take the short way around
	hueLonger
	hueIncreasing
	hueDecreasing
)

// lookupMixSpace maps a CSS colour-space keyword to a mixSpace.
func lookupMixSpace(s string) (mixSpace, bool) {
	switch s {
	case "srgb":
		return mixSRGB, true
	case "srgb-linear":
		return mixSRGBLinear, true
	case "hsl":
		return mixHSL, true
	case "hwb":
		return mixHWB, true
	case "lab":
		return mixLab, true
	case "lch":
		return mixLCH, true
	case "oklab":
		return mixOKLab, true
	case "oklch":
		return mixOKLCH, true
	// Bare "xyz" is a synonym for xyz-d65 (CSS Color 4 §10.2).
	case "xyz", "xyz-d65":
		return mixXYZD65, true
	case "xyz-d50":
		return mixXYZD50, true
	}
	return 0, false
}

// lookupHueMode maps the words before "hue" in an interpolation method.
func lookupHueMode(s string) (hueMode, bool) {
	switch s {
	case "shorter":
		return hueShorter, true
	case "longer":
		return hueLonger, true
	case "increasing":
		return hueIncreasing, true
	case "decreasing":
		return hueDecreasing, true
	}
	return 0, false
}

// parseColorMix parses and evaluates a color-mix() value, returning the mixed colour
// in 8-bit sRGB. ok is false for any malformed input — an unknown space, a missing
// second colour, an unparseable component, or percentages that sum to zero — so the
// caller drops the declaration.
func parseColorMix(s string) (color.RGBA, bool) {
	f, ok := parseColorMixFloat(s)
	if !ok {
		return color.RGBA{}, false
	}
	return f.toRGBA(), true
}

// parseColorMixFloat is parseColorMix's unquantized core. It exists so a NESTED
// color-mix() argument stays in float instead of round-tripping through 8 bits at
// each level — see floatRGBA.
func parseColorMixFloat(s string) (floatRGBA, bool) {
	inner, ok := trimFunc(s, "color-mix")
	if !ok {
		return floatRGBA{}, false
	}
	parts := splitTopLevelCommas(inner)
	if len(parts) != 3 {
		return floatRGBA{}, false
	}
	space, hue, ok := parseInterpolationMethod(parts[0])
	if !ok {
		return floatRGBA{}, false
	}
	c1, p1, has1, ok := parseMixComponent(parts[1])
	if !ok {
		return floatRGBA{}, false
	}
	c2, p2, has2, ok := parseMixComponent(parts[2])
	if !ok {
		return floatRGBA{}, false
	}

	// CSS Color 5 §3.2 percentage normalization. Omitted percentages fill the
	// remainder; if only one is given the other is 100%-p. When both are given and
	// they do not sum to 100%, the weights are normalized AND the result's alpha is
	// scaled by their sum — which is why `red 20%, blue 20%` yields alpha 0.4 rather
	// than an opaque half-and-half.
	// A given percentage must be in [0,100]; outside that the value is invalid rather
	// than clamped ("red 150%, blue" is rejected by browsers, not treated as 100%).
	if (has1 && (p1 < 0 || p1 > 100)) || (has2 && (p2 < 0 || p2 > 100)) {
		return floatRGBA{}, false
	}
	alphaScale := 1.0
	switch {
	case !has1 && !has2:
		p1, p2 = 50, 50
	case has1 && !has2:
		p2 = 100 - p1
	case !has1 && has2:
		p1 = 100 - p2
	default:
		sum := p1 + p2
		// Both zero leaves no colour to mix; the value is invalid.
		if sum == 0 {
			return floatRGBA{}, false
		}
		if sum != 100 {
			// Under 100% additionally scales the result's alpha, which is why
			// "red 20%, blue 20%" is a half-and-half at alpha 0.4 rather than opaque.
			alphaScale = math.Min(sum/100, 1)
			p1, p2 = p1/sum*100, p2/sum*100
		}
	}
	t := p2 / 100 // weight of the SECOND colour

	return mixColorsF(c1, c2, t, space, hue, alphaScale), true
}

// floatRGBA is a colour kept in unquantized sRGB [0,1] plus alpha. color-mix()
// evaluates in this form and quantizes only at the very end, so a NESTED mix does not
// round-trip through 8 bits at each level: Chrome resolves
// color-mix(in srgb, color-mix(in srgb, red, blue), white) to (191,128,191), which an
// intermediate quantization turns into (192,128,192) — the inner 127.5 rounds to 128
// before the outer average sees it.
type floatRGBA struct {
	R, G, B, A float64
}

// toRGBA clamps and quantizes to the 8-bit colour the rest of the engine uses.
func (f floatRGBA) toRGBA() color.RGBA {
	return color.RGBA{
		R: clamp8(f.R * 255),
		G: clamp8(f.G * 255),
		B: clamp8(f.B * 255),
		A: clamp8(f.A * 255),
	}
}

// floatFromRGBA widens an 8-bit colour into the float form.
func floatFromRGBA(c color.RGBA) floatRGBA {
	return floatRGBA{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: float64(c.A) / 255}
}

// mixColors interpolates c1 and c2 in the given space, with t the weight of c2.
//
// Interpolation is PREMULTIPLIED by alpha (CSS Color 4 §12.3): each colour's
// components are scaled by its own alpha before mixing and the result is divided by
// the mixed alpha. That is why mixing 50%-alpha red with opaque blue gives
// (85,0,170) rather than the (128,0,128) an unpremultiplied mix would produce — the
// more transparent colour contributes proportionally less hue.
func mixColorsF(c1, c2 floatRGBA, t float64, space mixSpace, hue hueMode, alphaScale float64) floatRGBA {
	a1, a2 := c1.A, c2.A
	comp1 := toMixSpace(c1, space)
	comp2 := toMixSpace(c2, space)

	outA := a1*(1-t) + a2*t
	var out [3]float64
	for i := range out {
		v1, v2 := comp1[i], comp2[i]
		// The hue channel is an angle: interpolate it on the circle, and never
		// premultiply it (alpha-weighting an angle is meaningless).
		if space.polar() && i == hueChannel(space) {
			out[i] = interpolateHue(v1, v2, t, hue)
			continue
		}
		if outA == 0 {
			out[i] = v1*(1-t) + v2*t
			continue
		}
		out[i] = (v1*a1*(1-t) + v2*a2*t) / outA
	}

	res := fromMixSpace(out, space)
	res.A = clampFloat(outA*alphaScale, 0, 1)
	return res
}

// hueChannel returns the index of the hue component in a polar space's triple.
func hueChannel(space mixSpace) int {
	switch space {
	case mixHSL, mixHWB:
		return 0 // h, s, l  /  h, w, b
	case mixLCH, mixOKLCH:
		return 2 // l, c, h
	}
	return -1
}

// interpolateHue interpolates two hue angles per CSS Color 4 §12.4. The modes differ
// only in how the endpoints are adjusted before a plain lerp: shorter takes the
// <=180-degree arc, longer the >=180 one, increasing forces an ascending sweep and
// decreasing a descending one.
func interpolateHue(h1, h2, t float64, mode hueMode) float64 {
	h1 = normalizeHue(h1)
	h2 = normalizeHue(h2)
	d := h2 - h1
	switch mode {
	case hueShorter:
		if d > 180 {
			h2 -= 360
		} else if d < -180 {
			h2 += 360
		}
	case hueLonger:
		if d > 0 && d < 180 {
			h2 -= 360
		} else if d > -180 && d <= 0 {
			h2 += 360
		}
	case hueIncreasing:
		if d < 0 {
			h2 += 360
		}
	case hueDecreasing:
		if d > 0 {
			h2 -= 360
		}
	}
	return normalizeHue(h1*(1-t) + h2*t)
}

// normalizeHue folds an angle into [0,360).
func normalizeHue(h float64) float64 { return math.Mod(math.Mod(h, 360)+360, 360) }

// toMixSpace converts an 8-bit sRGB colour into the interpolation space's triple.
func toMixSpace(c floatRGBA, space mixSpace) [3]float64 {
	r, g, b := c.R, c.G, c.B
	switch space {
	case mixSRGB:
		return [3]float64{r, g, b}
	case mixHSL:
		h, s, l := srgbToHSL(r, g, b)
		return [3]float64{h, s, l}
	case mixHWB:
		h, w, bl := srgbToHWB(r, g, b)
		return [3]float64{h, w, bl}
	}
	lr, lg, lb := linearizeSRGB(r), linearizeSRGB(g), linearizeSRGB(b)
	if space == mixSRGBLinear {
		return [3]float64{lr, lg, lb}
	}
	x, y, z := linearSRGBToXYZD65(lr, lg, lb)
	switch space {
	case mixXYZD65:
		return [3]float64{x, y, z}
	case mixXYZD50:
		x50, y50, z50 := xyzD65ToD50(x, y, z)
		return [3]float64{x50, y50, z50}
	case mixLab, mixLCH:
		x50, y50, z50 := xyzD65ToD50(x, y, z)
		l, aa, bb := xyzD50ToLab(x50, y50, z50)
		if space == mixLab {
			return [3]float64{l, aa, bb}
		}
		ch, hh := rectToPolar(aa, bb)
		return [3]float64{l, ch, hh}
	default: // mixOKLab, mixOKLCH
		l, aa, bb := xyzD65ToOKLab(x, y, z)
		if space == mixOKLab {
			return [3]float64{l, aa, bb}
		}
		ch, hh := rectToPolar(aa, bb)
		return [3]float64{l, ch, hh}
	}
}

// fromMixSpace converts an interpolation-space triple back to 8-bit sRGB, clamping
// out-of-gamut results into range (CSS gamut mapping is not modeled; a clamp is what
// the raster backends can express).
func fromMixSpace(v [3]float64, space mixSpace) floatRGBA {
	var r, g, b float64
	switch space {
	case mixSRGB:
		r, g, b = v[0], v[1], v[2]
	case mixHSL:
		r, g, b = hslToRGBFloat(v[0], clampFloat(v[1], 0, 1), clampFloat(v[2], 0, 1))
	case mixHWB:
		r, g, b = hwbToSRGB(v[0], clampFloat(v[1], 0, 1), clampFloat(v[2], 0, 1))
	default:
		var x, y, z float64
		switch space {
		case mixSRGBLinear:
			return floatRGBA{R: encodeSRGB(v[0]), G: encodeSRGB(v[1]), B: encodeSRGB(v[2]), A: 1}
		case mixXYZD65:
			x, y, z = v[0], v[1], v[2]
		case mixXYZD50:
			x, y, z = xyzD50ToD65(v[0], v[1], v[2])
		case mixLab, mixLCH:
			l, aa, bb := v[0], v[1], v[2]
			if space == mixLCH {
				aa, bb = polarToRect(v[1], v[2])
			}
			x50, y50, z50 := labToXYZD50(l, aa, bb)
			x, y, z = xyzD50ToD65(x50, y50, z50)
		default: // mixOKLab, mixOKLCH
			l, aa, bb := v[0], v[1], v[2]
			if space == mixOKLCH {
				aa, bb = polarToRect(v[1], v[2])
			}
			x, y, z = okLabToXYZD65(l, aa, bb)
		}
		lr, lg, lb := xyzD65ToLinearSRGB(x, y, z)
		r, g, b = encodeSRGB(lr), encodeSRGB(lg), encodeSRGB(lb)
	}
	return floatRGBA{R: r, G: g, B: b, A: 1}
}

// clamp8 rounds a 0..255-scaled float into a byte.
func clamp8(v float64) uint8 {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}

// parseInterpolationMethod parses the "in <space> [<mode> hue]" first argument.
func parseInterpolationMethod(s string) (mixSpace, hueMode, bool) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	if len(fields) < 2 || fields[0] != "in" {
		return 0, 0, false
	}
	space, ok := lookupMixSpace(fields[1])
	if !ok {
		return 0, 0, false
	}
	switch len(fields) {
	case 2:
		return space, hueShorter, true
	case 4:
		// "<mode> hue" is only meaningful for a polar space.
		if fields[3] != "hue" || !space.polar() {
			return 0, 0, false
		}
		mode, ok := lookupHueMode(fields[2])
		if !ok {
			return 0, 0, false
		}
		return space, mode, true
	}
	return 0, 0, false
}

// parseMixComponent parses one "<color> [<percentage>]" argument. The percentage may
// precede or follow the colour, per the grammar.
func parseMixComponent(s string) (c floatRGBA, pct float64, hasPct bool, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return floatRGBA{}, 0, false, false
	}
	// Find a top-level percentage token; whatever remains is the colour. Scanning for
	// it rather than splitting on space keeps `rgb(0 0 0 / 50%) 25%` working, whose
	// colour contains both spaces and a percent sign.
	depth := 0
	fieldStart := -1
	var colourParts []string
	for i := 0; i <= len(s); i++ {
		atEnd := i == len(s)
		var ch byte
		if !atEnd {
			ch = s[i]
		}
		switch {
		case !atEnd && ch == '(':
			depth++
		case !atEnd && ch == ')':
			depth--
		}
		isSep := atEnd || (depth == 0 && (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '\f'))
		if !isSep {
			if fieldStart < 0 {
				fieldStart = i
			}
			continue
		}
		if fieldStart >= 0 {
			field := s[fieldStart:i]
			if v, isPct := parsePercent(field); isPct && !hasPct {
				pct, hasPct = v*100, true
			} else {
				colourParts = append(colourParts, field)
			}
			fieldStart = -1
		}
	}
	if len(colourParts) == 0 {
		return floatRGBA{}, 0, false, false
	}
	joined := strings.Join(colourParts, " ")
	// A nested color-mix() is evaluated in FLOAT and handed back unquantized, so the
	// intermediate never round-trips through 8 bits (see floatRGBA).
	if strings.HasPrefix(strings.ToLower(joined), "color-mix") {
		f, ok := parseColorMixFloat(joined)
		if !ok {
			return floatRGBA{}, 0, false, false
		}
		return f, pct, hasPct, true
	}
	rgba, ok := ParseColorValue(joined)
	if !ok {
		return floatRGBA{}, 0, false, false
	}
	return floatFromRGBA(rgba), pct, hasPct, true
}

// trimFunc strips a "name(...)" wrapper, returning the inner text. The match on the
// name is case-insensitive; ok is false when s is not that function call.
func trimFunc(s, name string) (string, bool) {
	t := strings.TrimSpace(s)
	if len(t) <= len(name)+1 || !strings.EqualFold(t[:len(name)], name) {
		return "", false
	}
	if t[len(name)] != '(' || t[len(t)-1] != ')' {
		return "", false
	}
	return t[len(name)+1 : len(t)-1], true
}

// splitTopLevelCommas splits on commas that are not inside parentheses, so a nested
// rgb(1, 2, 3) argument stays one part.
func splitTopLevelCommas(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}
