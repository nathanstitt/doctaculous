package css

import "math"

// This file is the engine's colour-space conversion layer: the transforms between
// sRGB and the CIE/OKLab families that CSS Color 4 interpolation is defined in.
//
// It exists for color-mix() (see colormix.go), which must interpolate in a NAMED
// space rather than in sRGB — mixing red and blue in oklab gives a muted purple
// (140,83,162) where sRGB gives (128,0,128), and the difference is the whole point of
// the property. The transforms are also what a future lab()/oklch() colour syntax
// would need, so they are kept separate from the parser rather than buried in it.
//
// Every matrix and constant below is from CSS Color Module Level 4 §17 (sample
// conversion code). The values are transcribed from the spec rather than re-derived,
// and the results are pinned against Chrome's own output in colormix_test.go — a
// transposed matrix or a swapped white point produces plausible-looking colours that
// are quietly wrong, which a self-consistent test would never catch.

// linearizeSRGB converts one gamma-encoded sRGB channel in [0,1] to linear-light.
// The sign handling keeps the function odd so out-of-gamut negatives (which arise
// mid-interpolation) round-trip instead of folding to zero.
func linearizeSRGB(c float64) float64 {
	abs := math.Abs(c)
	if abs <= 0.04045 {
		return c / 12.92
	}
	return math.Copysign(math.Pow((abs+0.055)/1.055, 2.4), c)
}

// encodeSRGB is linearizeSRGB's inverse: linear-light to gamma-encoded.
func encodeSRGB(c float64) float64 {
	abs := math.Abs(c)
	if abs <= 0.0031308 {
		return c * 12.92
	}
	return math.Copysign(1.055*math.Pow(abs, 1/2.4)-0.055, c)
}

// linearSRGBToXYZD65 converts linear-light sRGB to CIE XYZ with a D65 white point.
func linearSRGBToXYZD65(r, g, b float64) (x, y, z float64) {
	x = 0.41239079926595934*r + 0.357584339383878*g + 0.1804807884018343*b
	y = 0.21263900587151027*r + 0.715168678767756*g + 0.07219231536073371*b
	z = 0.01933081871559182*r + 0.11919477979462598*g + 0.9505321522496607*b
	return
}

// xyzD65ToLinearSRGB is linearSRGBToXYZD65's inverse.
func xyzD65ToLinearSRGB(x, y, z float64) (r, g, b float64) {
	r = 3.2409699419045226*x - 1.537383177570094*y - 0.4986107602930034*z
	g = -0.9692436362808796*x + 1.8759675015077202*y + 0.04155505740717559*z
	b = 0.05563007969699366*x - 0.20397695888897652*y + 1.0569715142428786*z
	return
}

// D50 and D65 white points (CIE 1931 2-degree observer), used by the Bradford
// adaptation below and as Lab's reference white.
// Only D50 is needed: it is CIE Lab's reference white, and the Lab conversions below
// divide by it. The D65 white point is implicit in the sRGB<->XYZ matrices and the
// Bradford adaptation, which carry it in their coefficients rather than as a vector.
var whiteD50 = [3]float64{0.3457 / 0.3585, 1.0, (1.0 - 0.3457 - 0.3585) / 0.3585}

// xyzD65ToD50 chromatically adapts XYZ from a D65 to a D50 white point (Bradford).
// CIE Lab is defined against D50, so lab()/lch() interpolation must adapt first;
// skipping the adaptation shifts every Lab mix toward blue.
func xyzD65ToD50(x, y, z float64) (float64, float64, float64) {
	return 1.0479298208405488*x + 0.022946793341019088*y - 0.05019222954313557*z,
		0.029627815688159344*x + 0.990434484573249*y - 0.01707382502938514*z,
		-0.009243058152591178*x + 0.015055144896577895*y + 0.7518742899580008*z
}

// xyzD50ToD65 is xyzD65ToD50's inverse.
func xyzD50ToD65(x, y, z float64) (float64, float64, float64) {
	return 0.9554734527042182*x - 0.023098536874261423*y + 0.0632593086610217*z,
		-0.028369706963208136*x + 1.0099954580058226*y + 0.021041398966943008*z,
		0.012314001688319899*x - 0.020507696433477912*y + 1.3303659366080753*z
}

// labEpsilon and labKappa are the CIE Lab piecewise-function constants (216/24389
// and 24389/27), written as the exact rationals the spec uses.
const (
	labEpsilon = 216.0 / 24389.0
	labKappa   = 24389.0 / 27.0
)

// xyzD50ToLab converts D50-adapted XYZ to CIE Lab.
func xyzD50ToLab(x, y, z float64) (l, a, bb float64) {
	f := func(t float64) float64 {
		if t > labEpsilon {
			return math.Cbrt(t)
		}
		return (labKappa*t + 16) / 116
	}
	f0, f1, f2 := f(x/whiteD50[0]), f(y/whiteD50[1]), f(z/whiteD50[2])
	return 116*f1 - 16, 500 * (f0 - f1), 200 * (f1 - f2)
}

// labToXYZD50 is xyzD50ToLab's inverse.
func labToXYZD50(l, a, b float64) (x, y, z float64) {
	f1 := (l + 16) / 116
	f0 := a/500 + f1
	f2 := f1 - b/200

	inv := func(f float64) float64 {
		if c := f * f * f; c > labEpsilon {
			return c
		}
		return (116*f - 16) / labKappa
	}
	f := (l + 16) / 116
	yv := f * f * f
	if l <= labKappa*labEpsilon {
		yv = l / labKappa
	}
	return inv(f0) * whiteD50[0], yv * whiteD50[1], inv(f2) * whiteD50[2]
}

// xyzD65ToOKLab converts D65 XYZ to OKLab (Björn Ottosson's space, adopted by CSS
// Color 4). The cube roots between the two matrices are what make it perceptually
// uniform — and are why an OKLab mix of red and blue is a muted purple rather than
// the vivid magenta an HSL mix gives.
func xyzD65ToOKLab(x, y, z float64) (l, a, b float64) {
	lms0 := 0.8190224379967030*x + 0.3619062600528904*y - 0.1288737815209879*z
	lms1 := 0.0329836539323885*x + 0.9292868615863434*y + 0.0361446663506424*z
	lms2 := 0.0481771893596242*x + 0.2642395317527308*y + 0.6335478284694309*z
	c0, c1, c2 := math.Cbrt(lms0), math.Cbrt(lms1), math.Cbrt(lms2)
	return 0.2104542683093140*c0 + 0.7936177747023054*c1 - 0.0040720430116193*c2,
		1.9779985324311684*c0 - 2.4285922420485799*c1 + 0.4505937096174110*c2,
		0.0259040424655478*c0 + 0.7827717124575296*c1 - 0.8086757549230774*c2
}

// okLabToXYZD65 is xyzD65ToOKLab's inverse.
func okLabToXYZD65(l, a, b float64) (x, y, z float64) {
	c0 := l + 0.3963377773761749*a + 0.2158037573099136*b
	c1 := l - 0.1055613458156586*a - 0.0638541728258133*b
	c2 := l - 0.0894841775298119*a - 1.2914855480194092*b
	lms0, lms1, lms2 := c0*c0*c0, c1*c1*c1, c2*c2*c2
	return 1.2268798758459243*lms0 - 0.5578149944602171*lms1 + 0.2813910456659647*lms2,
		-0.0405757452148008*lms0 + 1.1122868032803170*lms1 - 0.0717110580655164*lms2,
		-0.0763729366746601*lms0 - 0.4214933324022432*lms1 + 1.5869240198367816*lms2
}

// rectToPolar converts a rectangular (Lab/OKLab) pair to the chroma/hue of its polar
// form (LCH/OKLCH). Hue is in degrees, normalized to [0,360).
func rectToPolar(a, b float64) (chroma, hue float64) {
	chroma = math.Hypot(a, b)
	hue = math.Atan2(b, a) * 180 / math.Pi
	if hue < 0 {
		hue += 360
	}
	return
}

// polarToRect is rectToPolar's inverse.
func polarToRect(chroma, hue float64) (a, b float64) {
	rad := hue * math.Pi / 180
	return chroma * math.Cos(rad), chroma * math.Sin(rad)
}

// srgbToHSL converts gamma-encoded sRGB in [0,1] to HSL, with hue in degrees. It
// returns the hue as-is for achromatic colours (chroma 0) so the caller can decide
// whether the hue is meaningful — CSS treats a missing hue as 0 for interpolation.
func srgbToHSL(r, g, b float64) (h, s, l float64) {
	maxV := math.Max(r, math.Max(g, b))
	minV := math.Min(r, math.Min(g, b))
	l = (maxV + minV) / 2
	d := maxV - minV
	if d == 0 {
		return 0, 0, l
	}
	if l > 0.5 {
		s = d / (2 - maxV - minV)
	} else {
		s = d / (maxV + minV)
	}
	switch maxV {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h * 60, s, l
}

// srgbToHWB converts gamma-encoded sRGB to HWB (hue, whiteness, blackness).
func srgbToHWB(r, g, b float64) (h, w, bl float64) {
	h, _, _ = srgbToHSL(r, g, b)
	w = math.Min(r, math.Min(g, b))
	bl = 1 - math.Max(r, math.Max(g, b))
	return
}

// hwbToSRGB is srgbToHWB's inverse. Whiteness and blackness summing to >= 1 collapse
// to the grey their ratio implies, per CSS Color 4 §7.
func hwbToSRGB(h, w, bl float64) (r, g, b float64) {
	if w+bl >= 1 {
		grey := w / (w + bl)
		return grey, grey, grey
	}
	r, g, b = hslToRGBFloat(h, 1, 0.5)
	scale := func(c float64) float64 { return c*(1-w-bl) + w }
	return scale(r), scale(g), scale(b)
}

// hslToRGBFloat is the HSL->RGB conversion in float [0,1], shared by hwbToSRGB and
// the colour-mix result path. hslToRGBA (color.go) is the 8-bit-clamped wrapper the
// parser uses; keeping the float form here avoids a quantize/dequantize round trip
// in the middle of an interpolation.
func hslToRGBFloat(h, s, l float64) (r, g, b float64) {
	h = math.Mod(math.Mod(h, 360)+360, 360) / 360
	if s == 0 {
		return l, l, l
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	hue := func(t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6.0:
			return p + (q-p)*6*t
		case t < 1.0/2.0:
			return q
		case t < 2.0/3.0:
			return p + (q-p)*(2.0/3.0-t)*6
		}
		return p
	}
	return hue(h + 1.0/3.0), hue(h), hue(h - 1.0/3.0)
}
