package filter

import (
	"image"
	"math"
)

// ColorMatrix is feColorMatrix's 5x4 matrix in row-major order: twenty values
// mapping (R, G, B, A, 1) to each of the four output channels.
//
//	R' = m[0]·R  + m[1]·G  + m[2]·B  + m[3]·A  + m[4]
//	G' = m[5]·R  + m[6]·G  + m[7]·B  + m[8]·A  + m[9]
//	B' = m[10]·R + m[11]·G + m[12]·B + m[13]·A + m[14]
//	A' = m[15]·R + m[16]·G + m[17]·B + m[18]·A + m[19]
type ColorMatrix [20]float32

// IdentityColorMatrix is the matrix that leaves every channel unchanged — the
// value an feColorMatrix with no usable `values` resolves to, which is why the
// corpus's without-attributes and matrix-without-values fixtures render the
// source untouched rather than blanking it.
var IdentityColorMatrix = ColorMatrix{
	1, 0, 0, 0, 0,
	0, 1, 0, 0, 0,
	0, 0, 1, 0, 0,
	0, 0, 0, 1, 0,
}

// SaturateMatrix returns the matrix for type="saturate" with coefficient s,
// using the spec's exact coefficients rather than an approximation.
//
// s is NOT clamped to [0,1]. The spec's grammar says it should be, but the
// corpus pins the opposite: saturate-with-a-large-coefficient (values="10")
// and saturate-with-negative-coefficient both render as the raw matrix
// evaluates, with only the FINAL channel values clamped. Clamping s here would
// render both fixtures as an ordinary saturate and silently miss them.
func SaturateMatrix(s float32) ColorMatrix {
	return ColorMatrix{
		0.213 + 0.787*s, 0.715 - 0.715*s, 0.072 - 0.072*s, 0, 0,
		0.213 - 0.213*s, 0.715 + 0.285*s, 0.072 - 0.072*s, 0, 0,
		0.213 - 0.213*s, 0.715 - 0.715*s, 0.072 + 0.928*s, 0, 0,
		0, 0, 0, 1, 0,
	}
}

// HueRotateMatrix returns the matrix for type="hueRotate" with the angle in
// DEGREES, using the spec's exact formulation.
//
// The matrix is the spec's constant term plus cos and sin weighted terms; at
// 0 degrees it reduces exactly to the identity (cos=1, sin=0 makes the three
// terms sum to the identity), which is the cheapest check that the constants
// were transcribed correctly.
func HueRotateMatrix(degrees float64) ColorMatrix {
	rad := degrees * math.Pi / 180
	c := float32(math.Cos(rad))
	s := float32(math.Sin(rad))
	return ColorMatrix{
		0.213 + c*0.787 - s*0.213, 0.715 - c*0.715 - s*0.715, 0.072 - c*0.072 + s*0.928, 0, 0,
		0.213 - c*0.213 + s*0.143, 0.715 + c*0.285 + s*0.140, 0.072 - c*0.072 - s*0.283, 0, 0,
		0.213 - c*0.213 - s*0.787, 0.715 - c*0.715 + s*0.715, 0.072 + c*0.928 + s*0.072, 0, 0,
		0, 0, 0, 1, 0,
	}
}

// LuminanceToAlphaMatrix returns the matrix for type="luminanceToAlpha": the
// colour channels are zeroed and the alpha becomes the input's luminance.
//
// The coefficients are the SVG 1.1 filter luminance weights (0.2125, 0.7154,
// 0.0721), which are ITU-R BT.709's — deliberately NOT the 0.3/0.59/0.11 set
// PDF's blend functions use for the same-sounding quantity. Substituting one
// for the other shifts the result subtly and uniformly, which is hard to spot
// by eye and easy to introduce by reaching for whichever constant is nearest
// to hand.
var LuminanceToAlphaMatrix = ColorMatrix{
	0, 0, 0, 0, 0,
	0, 0, 0, 0, 0,
	0, 0, 0, 0, 0,
	0.2125, 0.7154, 0.0721, 0, 0,
}

// ApplyColorMatrix applies m to every pixel of in, clipped to subregion — the
// feColorMatrix primitive.
//
// It operates on UN-PREMULTIPLIED (straight) values, which is the OPPOSITE of
// what feGaussianBlur and feComposite do and is stated explicitly by the spec.
// The reason is that the matrix mixes channels: with premultiplied input a
// pixel's colour is already scaled by its alpha, so a coefficient that should
// move red into green would move alpha-scaled red instead, and the result of
// a saturate on a semi-transparent pixel would depend on its opacity. Buffer
// already stores straight alpha, so this primitive needs NO conversion — which
// is exactly why getting it backwards is easy: the bug would be ADDING a
// premultiply, not forgetting one.
//
// Each output channel is clamped to [0,1]. The matrix is unconstrained (a
// non-normalized matrix, a large saturate coefficient, or a negative one can
// all drive a channel out of range), and the corpus asserts the clamped
// result.
func ApplyColorMatrix(in *Buffer, m ColorMatrix, subregion image.Rectangle) *Buffer {
	space := LinearRGB
	if in != nil {
		space = in.Space
	}
	out := NewBuffer(subregion, space)
	if in == nil {
		return out
	}
	for y := subregion.Min.Y; y < subregion.Max.Y; y++ {
		for x := subregion.Min.X; x < subregion.Max.X; x++ {
			r, g, b, a := in.At(x, y)
			nr := clamp01(m[0]*r + m[1]*g + m[2]*b + m[3]*a + m[4])
			ng := clamp01(m[5]*r + m[6]*g + m[7]*b + m[8]*a + m[9])
			nb := clamp01(m[10]*r + m[11]*g + m[12]*b + m[13]*a + m[14])
			na := clamp01(m[15]*r + m[16]*g + m[17]*b + m[18]*a + m[19])
			if na == 0 && nr == 0 && ng == 0 && nb == 0 {
				continue
			}
			out.Set(x, y, nr, ng, nb, na)
		}
	}
	return out
}

// clamp01 clamps one channel into [0,1].
func clamp01(v float32) float32 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}
