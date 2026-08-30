package heif

import (
	"image"

	"github.com/nathanstitt/omnidoc/pkg/heif/hevc"
)

// Colour conversion (CICP nclx signaling) and orientation transforms.
//
// The decoded planes carry limited- or full-range YCbCr with a matrix given
// by the colr property or the bitstream VUI. Chroma is upsampled by sample
// replication (nearest neighbour), which is deterministic and matches the
// common still-image viewers' behaviour closely enough for 4:2:0 content.

// matrixCoeffs holds fixed-point (x65536) YCbCr->RGB coefficients.
type matrixCoeffs struct {
	crToR, cbToG, crToG, cbToB int32
}

var (
	coeffs601  = matrixCoeffs{91881, -22554, -46802, 116130}  // BT.601
	coeffs709  = matrixCoeffs{103206, -12276, -30679, 121608} // BT.709
	coeffs2020 = matrixCoeffs{96639, -10790, -37444, 123305}  // BT.2020 NCL
)

// pickCoeffs resolves the conversion matrix from the container's colr
// property (preferred) or the bitstream VUI.
func pickCoeffs(it *item, f *hevc.Frame) (matrixCoeffs, bool) {
	matrix := uint8(2)
	fullRange := false
	if it != nil && it.colr != nil && it.colr.colourType == "nclx" {
		matrix = uint8(it.colr.matrix)
		fullRange = it.colr.fullRange
	} else if f != nil && f.HasColourDescription {
		matrix = f.MatrixCoefficients
		fullRange = f.FullRange
	} else if f != nil {
		fullRange = f.FullRange
	}
	switch matrix {
	case 1:
		return coeffs709, fullRange
	case 9, 10:
		return coeffs2020, fullRange
	case 2:
		// Unspecified: follow the primaries when given, else BT.601.
		primaries := uint8(2)
		if it != nil && it.colr != nil && it.colr.colourType == "nclx" {
			primaries = uint8(it.colr.primaries)
		} else if f != nil && f.HasColourDescription {
			primaries = f.ColourPrimaries
		}
		if primaries == 1 {
			return coeffs709, fullRange
		}
		return coeffs601, fullRange
	default:
		return coeffs601, fullRange
	}
}

// toImage converts composed planes (+optional alpha) to an image.Image,
// honouring range, matrix, bit depth, and the irot/imir/clap properties.
func toImage(p *decodedPlanes, alpha []uint16, alphaDepth int, it *item) image.Image {
	co, fullRange := pickCoeffs(it, p.frame)
	w, h := p.width, p.height
	bd := p.bitDepth
	maxVal := int32(1)<<bd - 1

	// Range expansion parameters (fixed point x65536).
	var yScale, cScale, yOff int32
	if fullRange {
		yScale, cScale, yOff = 65536, 65536, 0
	} else {
		// Limited range: luma 16..235 (x2^(bd-8)), chroma 16..240.
		yScale = 65536 * 255 / 219
		cScale = 65536 * 255 / 224
		yOff = 16 << (bd - 8)
	}
	half := int32(1) << (bd - 1)
	// Fold the chroma range scale into the matrix coefficients once, so the
	// per-sample products stay within int32.
	co.crToR = int32(int64(co.crToR) * int64(cScale) >> 16)
	co.cbToG = int32(int64(co.cbToG) * int64(cScale) >> 16)
	co.crToG = int32(int64(co.crToG) * int64(cScale) >> 16)
	co.cbToB = int32(int64(co.cbToB) * int64(cScale) >> 16)

	deep := bd > 8 || alphaDepth > 8
	var out8 *image.NRGBA
	var out16 *image.NRGBA64
	if deep {
		out16 = image.NewNRGBA64(image.Rect(0, 0, w, h))
	} else {
		out8 = image.NewNRGBA(image.Rect(0, 0, w, h))
	}

	clamp := func(v int32) int32 {
		if v < 0 {
			return 0
		}
		if v > maxVal {
			return maxVal
		}
		return v
	}

	for y := 0; y < h; y++ {
		cy := y / 2
		for x := 0; x < w; x++ {
			yv := int32(p.y[y*p.yStride+x])
			cb := int32(p.cb[cy*p.cStride+x/2]) - half
			cr := int32(p.cr[cy*p.cStride+x/2]) - half
			yl := (yv - yOff) * yScale
			r := clamp((yl + cr*co.crToR + 32768) >> 16)
			g := clamp((yl + cb*co.cbToG + cr*co.crToG + 32768) >> 16)
			b := clamp((yl + cb*co.cbToB + 32768) >> 16)
			a := maxVal
			if alpha != nil {
				a = int32(alpha[y*w+x])
				if alphaDepth != bd {
					// Rescale alpha to the output depth.
					a = a * maxVal / (int32(1)<<alphaDepth - 1)
				}
			}
			if deep {
				shift := 16 - bd
				i := out16.PixOffset(x, y)
				px := out16.Pix[i : i+8 : i+8]
				put16 := func(o int, v int32) {
					vv := uint16(v << shift)
					// Replicate high bits so full scale maps to 0xffff.
					vv |= vv >> bd
					px[o] = uint8(vv >> 8)
					px[o+1] = uint8(vv)
				}
				put16(0, r)
				put16(2, g)
				put16(4, b)
				put16(6, a)
			} else {
				i := out8.PixOffset(x, y)
				px := out8.Pix[i : i+4 : i+4]
				px[0] = uint8(r)
				px[1] = uint8(g)
				px[2] = uint8(b)
				px[3] = uint8(a)
			}
		}
	}
	if deep {
		return applyOrientation16(out16, it)
	}
	return applyOrientation8(out8, it)
}

// orientation returns clap crop rectangle and rotation/mirror settings.
func cropRect(it *item, w, h int) image.Rectangle {
	r := image.Rect(0, 0, w, h)
	if it == nil || it.clap == nil {
		return r
	}
	cl := it.clap
	cw, okW := wholeFraction(cl.widthN, cl.widthD)
	chh, okH := wholeFraction(cl.heightN, cl.heightD)
	if !okW || !okH || cw == 0 || chh == 0 || int(cw) > w || int(chh) > h {
		return r
	}
	// Offsets are relative to the image centre; integral offsets only.
	offX := 0
	if cl.horizD != 0 && cl.horizN%int32(cl.horizD) == 0 {
		offX = int(cl.horizN / int32(cl.horizD))
	}
	offY := 0
	if cl.vertD != 0 && cl.vertN%int32(cl.vertD) == 0 {
		offY = int(cl.vertN / int32(cl.vertD))
	}
	x0 := (w-int(cw))/2 + offX
	y0 := (h-int(chh))/2 + offY
	crop := image.Rect(x0, y0, x0+int(cw), y0+int(chh))
	if crop.In(r) {
		return crop
	}
	return r
}

// applyOrientation8 applies clap, then imir, then irot for NRGBA images.
// HEIF renders transformative properties in their ipco association order;
// in practice writers use crop -> mirror -> rotate.
func applyOrientation8(img *image.NRGBA, it *item) image.Image {
	crop := cropRect(it, img.Rect.Dx(), img.Rect.Dy())
	if crop != img.Rect {
		img = img.SubImage(crop).(*image.NRGBA)
	}
	if it == nil {
		return img
	}
	mirror := it.imir != nil
	rot := int(it.irot)
	if !mirror && rot == 0 {
		return img
	}
	sw, sh := img.Rect.Dx(), img.Rect.Dy()
	dw, dh := sw, sh
	if rot == 1 || rot == 3 {
		dw, dh = sh, sw
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := range sh {
		for x := range sw {
			sx, sy := x, y
			if mirror {
				if *it.imir == 0 {
					sx = sw - 1 - x
				} else {
					sy = sh - 1 - y
				}
			}
			dx, dy := mapRot(rot, sw, sh, x, y)
			si := img.PixOffset(img.Rect.Min.X+sx, img.Rect.Min.Y+sy)
			di := dst.PixOffset(dx, dy)
			copy(dst.Pix[di:di+4], img.Pix[si:si+4])
		}
	}
	return dst
}

// applyOrientation16 is applyOrientation8 for NRGBA64.
func applyOrientation16(img *image.NRGBA64, it *item) image.Image {
	crop := cropRect(it, img.Rect.Dx(), img.Rect.Dy())
	if crop != img.Rect {
		img = img.SubImage(crop).(*image.NRGBA64)
	}
	if it == nil {
		return img
	}
	mirror := it.imir != nil
	rot := int(it.irot)
	if !mirror && rot == 0 {
		return img
	}
	sw, sh := img.Rect.Dx(), img.Rect.Dy()
	dw, dh := sw, sh
	if rot == 1 || rot == 3 {
		dw, dh = sh, sw
	}
	dst := image.NewNRGBA64(image.Rect(0, 0, dw, dh))
	for y := range sh {
		for x := range sw {
			sx, sy := x, y
			if mirror {
				if *it.imir == 0 {
					sx = sw - 1 - x
				} else {
					sy = sh - 1 - y
				}
			}
			dx, dy := mapRot(rot, sw, sh, x, y)
			si := img.PixOffset(img.Rect.Min.X+sx, img.Rect.Min.Y+sy)
			di := dst.PixOffset(dx, dy)
			copy(dst.Pix[di:di+8], img.Pix[si:si+8])
		}
	}
	return dst
}

// mapRot maps a source position (x, y) in an sw×sh image to its destination
// under an anti-clockwise rotation of rot*90 degrees.
func mapRot(rot, sw, sh, x, y int) (int, int) {
	switch rot {
	case 1: // 90 deg anti-clockwise
		return y, sw - 1 - x
	case 2:
		return sw - 1 - x, sh - 1 - y
	case 3:
		return sh - 1 - y, x
	default:
		return x, y
	}
}
