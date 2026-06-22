package raster

import (
	"fmt"
	"image"
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// csKind classifies an image color space by how its samples map to RGB. Indexed
// is handled separately (it carries a palette); everything else is identified by
// component count.
type csKind int

const (
	csGray csKind = iota // 1 component
	csRGB                // 3 components
	csCMYK               // 4 components
	csIndexed
)

// imageCS describes an image's color space: the kind, the number of samples per
// pixel in the stream, and (for Indexed) the resolved palette as straight RGBA.
type imageCS struct {
	kind     csKind
	nComps   int          // samples per pixel in the source stream
	palette  []color.RGBA // Indexed only: entry per index, already converted to RGB
	maxIndex int          // Indexed only: hival
}

// resolveImageCS interprets an image's /ColorSpace, which may be a name
// (/DeviceRGB) or an array ([/ICCBased s], [/Indexed base hival lookup], …). It
// returns a descriptor sufficient to unpack and convert samples. Unknown spaces
// are approximated by component count.
func resolveImageCS(doc *pdf.Document, csObj pdf.Object, bpc int, logf func(string, ...any)) (imageCS, error) {
	switch cs := doc.Resolve(csObj).(type) {
	case pdf.Name:
		return imageCSByName(string(cs)), nil
	case pdf.Array:
		return resolveImageCSArray(doc, cs, bpc, logf)
	default:
		if logf != nil {
			logf("raster: image color space %s unhandled; assuming RGB", pdf.Debug(csObj))
		}
		return imageCS{kind: csRGB, nComps: 3}, nil
	}
}

// imageCSByName maps a device/CIE color-space name to a descriptor by component
// count.
func imageCSByName(name string) imageCS {
	switch name {
	case "DeviceGray", "CalGray", "G":
		return imageCS{kind: csGray, nComps: 1}
	case "DeviceCMYK", "CMYK":
		return imageCS{kind: csCMYK, nComps: 4}
	default: // DeviceRGB, CalRGB, Lab, RGB, and anything else
		return imageCS{kind: csRGB, nComps: 3}
	}
}

// resolveImageCSArray handles the array color-space forms.
func resolveImageCSArray(doc *pdf.Document, arr pdf.Array, bpc int, logf func(string, ...any)) (imageCS, error) {
	if len(arr) == 0 {
		return imageCS{kind: csRGB, nComps: 3}, nil
	}
	family, _ := doc.GetName(arr[0])
	switch family {
	case "ICCBased":
		// [/ICCBased stream]; the stream's /N gives the component count. We do not
		// interpret the ICC profile, falling back to the device space for N.
		if s := doc.GetStream(arr[1]); s != nil {
			if n, ok := doc.GetInt(s.Dict["N"]); ok {
				switch n {
				case 1:
					return imageCS{kind: csGray, nComps: 1}, nil
				case 4:
					return imageCS{kind: csCMYK, nComps: 4}, nil
				default:
					return imageCS{kind: csRGB, nComps: 3}, nil
				}
			}
			// No /N: try the /Alternate space.
			if alt := s.Dict["Alternate"]; alt != nil {
				return resolveImageCS(doc, alt, bpc, logf)
			}
		}
		return imageCS{kind: csRGB, nComps: 3}, nil
	case "Indexed", "I":
		return resolveIndexedCS(doc, arr, logf)
	case "CalRGB", "Lab":
		return imageCS{kind: csRGB, nComps: 3}, nil
	case "CalGray":
		return imageCS{kind: csGray, nComps: 1}, nil
	case "DeviceN":
		// [/DeviceN names alternate tint]; approximate by the number of colorants.
		if names := doc.GetArray(arr[1]); names != nil {
			return imageCS{kind: csKindForComps(len(names)), nComps: len(names)}, nil
		}
		return imageCS{kind: csGray, nComps: 1}, nil
	case "Separation":
		// One colorant; treat as gray (0 = no ink → white, 1 = full ink → black is
		// not exactly right, but a reasonable device approximation).
		return imageCS{kind: csGray, nComps: 1}, nil
	default:
		if logf != nil {
			logf("raster: image color-space family /%s unhandled; assuming RGB", family)
		}
		return imageCS{kind: csRGB, nComps: 3}, nil
	}
}

// resolveIndexedCS builds the palette for an [/Indexed base hival lookup] space.
// Each source sample is an index into the lookup table, which holds (hival+1)
// entries of the base space's components. The palette is pre-converted to RGBA.
func resolveIndexedCS(doc *pdf.Document, arr pdf.Array, logf func(string, ...any)) (imageCS, error) {
	if len(arr) < 4 {
		return imageCS{}, fmt.Errorf("malformed /Indexed color space")
	}
	base, err := resolveImageCS(doc, arr[1], 8, logf)
	if err != nil {
		return imageCS{}, err
	}
	if base.kind == csIndexed {
		return imageCS{}, fmt.Errorf("/Indexed base may not be Indexed")
	}
	hival, _ := doc.GetInt(arr[2])
	if hival < 0 || hival > 255 {
		return imageCS{}, fmt.Errorf("/Indexed hival %d out of range", hival)
	}
	lookup := indexedLookupBytes(doc, arr[3])
	bc := base.nComps
	pal := make([]color.RGBA, hival+1)
	for i := range pal {
		off := i * bc
		comps := make([]float64, bc)
		for c := 0; c < bc; c++ {
			if off+c < len(lookup) {
				comps[c] = float64(lookup[off+c]) / 255
			}
		}
		pal[i] = componentsToRGBA(base.kind, comps)
	}
	return imageCS{kind: csIndexed, nComps: 1, palette: pal, maxIndex: hival}, nil
}

// indexedLookupBytes returns the palette bytes of an /Indexed color space, which
// may be a string object or a stream.
func indexedLookupBytes(doc *pdf.Document, o pdf.Object) []byte {
	switch v := doc.Resolve(o).(type) {
	case pdf.String:
		return []byte(v)
	case *pdf.Stream:
		if data, _, err := doc.DecodedStream(v); err == nil {
			return data
		}
	}
	return nil
}

// csKindForComps picks a kind from a raw component count (for spaces we only
// approximate, like DeviceN).
func csKindForComps(n int) csKind {
	switch n {
	case 1:
		return csGray
	case 4:
		return csCMYK
	default:
		return csRGB
	}
}

// decodeRawImage unpacks raw (non-DCT) image samples into an *image.RGBA using
// the resolved color space and bit depth. Rows are padded to a byte boundary per
// the PDF image model. bpc is bits per component (1/2/4/8/16; 16 is truncated to
// the high byte).
func decodeRawImage(data []byte, w, h, bpc int, cs imageCS) (*image.RGBA, error) {
	if bpc != 1 && bpc != 2 && bpc != 4 && bpc != 8 && bpc != 16 {
		return nil, fmt.Errorf("unsupported BitsPerComponent %d", bpc)
	}
	rowBits := w * cs.nComps * bpc
	rowBytes := (rowBits + 7) / 8
	if len(data) < rowBytes*h {
		return nil, fmt.Errorf("short sample data: %d < %d", len(data), rowBytes*h)
	}
	maxVal := float64((uint32(1) << bpc) - 1)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	comps := make([]float64, cs.nComps)
	for y := 0; y < h; y++ {
		br := bitReader{data: data[y*rowBytes : (y+1)*rowBytes], bpc: bpc}
		for x := 0; x < w; x++ {
			if cs.kind == csIndexed {
				idx := int(br.next())
				var c color.RGBA
				if idx >= 0 && idx < len(cs.palette) {
					c = cs.palette[idx]
				}
				c.A = 0xFF
				img.SetRGBA(x, y, c)
				continue
			}
			for c := 0; c < cs.nComps; c++ {
				comps[c] = float64(br.next()) / maxVal
			}
			rc := componentsToRGBA(cs.kind, comps)
			rc.A = 0xFF
			img.SetRGBA(x, y, rc)
		}
	}
	return img, nil
}

// componentsToRGBA converts color components in [0,1] for a (non-indexed) kind to
// straight-alpha RGB (alpha is set by the caller).
func componentsToRGBA(kind csKind, comps []float64) color.RGBA {
	get := func(i int) float64 {
		if i < len(comps) {
			return comps[i]
		}
		return 0
	}
	switch kind {
	case csGray:
		v := clamp8f(get(0))
		return color.RGBA{v, v, v, 0xFF}
	case csCMYK:
		c, m, yy, k := get(0), get(1), get(2), get(3)
		return color.RGBA{clamp8f((1 - c) * (1 - k)), clamp8f((1 - m) * (1 - k)), clamp8f((1 - yy) * (1 - k)), 0xFF}
	default: // csRGB
		return color.RGBA{clamp8f(get(0)), clamp8f(get(1)), clamp8f(get(2)), 0xFF}
	}
}

// clamp8f maps a component in [0,1] to an 8-bit value, clamping out-of-range.
func clamp8f(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	default:
		return uint8(v*255 + 0.5)
	}
}

// bitReader yields successive bpc-bit samples from a single image row, MSB-first
// within each byte (the PDF image sample order).
type bitReader struct {
	data    []byte
	bpc     int
	bytePos int
	bitPos  int // 0..7, MSB-first
}

// next returns the next sample value (0 .. 2^bpc-1). For bpc 16 it returns the
// high byte only. Reading past the end yields 0.
func (r *bitReader) next() uint32 {
	if r.bpc == 8 {
		v := uint32(0)
		if r.bytePos < len(r.data) {
			v = uint32(r.data[r.bytePos])
		}
		r.bytePos++
		return v
	}
	if r.bpc == 16 {
		v := uint32(0)
		if r.bytePos < len(r.data) {
			v = uint32(r.data[r.bytePos]) // high byte; low byte discarded
		}
		r.bytePos += 2
		return v
	}
	// 1/2/4 bpc: accumulate bits MSB-first.
	var v uint32
	for i := 0; i < r.bpc; i++ {
		v <<= 1
		if r.bytePos < len(r.data) {
			bit := (r.data[r.bytePos] >> uint(7-r.bitPos)) & 1
			v |= uint32(bit)
		}
		r.bitPos++
		if r.bitPos == 8 {
			r.bitPos = 0
			r.bytePos++
		}
	}
	return v
}
