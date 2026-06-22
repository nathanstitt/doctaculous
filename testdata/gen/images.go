package gen

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
)

// RotatedPDF returns a single-page text PDF whose page carries a /Rotate entry
// (one of 0, 90, 180, 270). It exercises the page-tree /Rotate inheritance and
// the rasterizer's rotation handling. Out-of-range values are normalized by the
// parser, so callers may pass any multiple of 90.
func RotatedPDF(degrees int) []byte {
	b := newBuilder()
	font := b.addObject(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`)
	content := []byte("BT /F1 24 Tf 72 700 Td (Rotated page) Tj ET")
	contentNum := b.addStream("", content)

	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	pageBody := fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Rotate %d "+
			"/Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>",
		pagesNum, degrees, font, contentNum)
	page := b.addObject(pageBody)
	if page != pageNum {
		panic("gen: page object number mismatch in RotatedPDF")
	}
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}

// ImagePDF returns a single-page PDF that draws a small FlateDecode-compressed
// RGB image XObject scaled to fill most of the page. The samples are raw
// 8-bit-per-component DeviceRGB, so it exercises the Flate filter plus image
// drawing without any JPEG dependency.
func ImagePDF() []byte {
	const w, h = 4, 4
	// A 4x4 checkerboard of red and blue, 3 bytes/pixel.
	samples := make([]byte, 0, w*h*3)
	for y := range h {
		for x := range w {
			if (x+y)%2 == 0 {
				samples = append(samples, 0xFF, 0x00, 0x00) // red
			} else {
				samples = append(samples, 0x00, 0x00, 0xFF) // blue
			}
		}
	}
	return buildImagePage(w, h, zlibCompress(samples),
		"/Filter /FlateDecode /ColorSpace /DeviceRGB /BitsPerComponent 8")
}

// JPEGImagePDF returns a single-page PDF whose image XObject is DCTDecode
// (baseline JPEG) data, exercising the DCTDecode path that defers to image/jpeg.
func JPEGImagePDF() []byte {
	const w, h = 16, 16
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if (x/4+y/4)%2 == 0 {
				img.Set(x, y, color.RGBA{R: 0xFF, A: 0xFF})
			} else {
				img.Set(x, y, color.RGBA{B: 0xFF, A: 0xFF})
			}
		}
	}
	var jbuf bytes.Buffer
	if err := jpeg.Encode(&jbuf, img, &jpeg.Options{Quality: 90}); err != nil {
		panic("gen: jpeg encode: " + err.Error())
	}
	return buildImagePage(w, h, jbuf.Bytes(),
		"/Filter /DCTDecode /ColorSpace /DeviceRGB /BitsPerComponent 8")
}

// GrayImagePDF returns a single-page PDF drawing an 8-bit DeviceGray image: a
// horizontal gradient from black to white. Exercises the DeviceGray decode path.
func GrayImagePDF() []byte {
	const w, h = 8, 8
	samples := make([]byte, 0, w*h)
	for y := range h {
		_ = y
		for x := range w {
			samples = append(samples, byte(x*255/(w-1)))
		}
	}
	return buildImagePage(w, h, zlibCompress(samples),
		"/Filter /FlateDecode /ColorSpace /DeviceGray /BitsPerComponent 8")
}

// CMYKImagePDF returns a single-page PDF drawing an 8-bit DeviceCMYK image whose
// four quadrants are cyan, magenta, yellow, and black. Exercises CMYK→RGB.
func CMYKImagePDF() []byte {
	const w, h = 8, 8
	samples := make([]byte, 0, w*h*4)
	for y := range h {
		for x := range w {
			var c, m, yc, k byte
			switch {
			case x < w/2 && y < h/2:
				c = 0xFF // cyan
			case x >= w/2 && y < h/2:
				m = 0xFF // magenta
			case x < w/2 && y >= h/2:
				yc = 0xFF // yellow
			default:
				k = 0xFF // black
			}
			samples = append(samples, c, m, yc, k)
		}
	}
	return buildImagePage(w, h, zlibCompress(samples),
		"/Filter /FlateDecode /ColorSpace /DeviceCMYK /BitsPerComponent 8")
}

// IndexedImagePDF returns a single-page PDF drawing a 4-bit Indexed image over a
// DeviceRGB base with a small palette. Exercises Indexed lookup + sub-byte
// (4-bit) sample unpacking.
func IndexedImagePDF() []byte {
	const w, h = 8, 8
	// Palette: 0=red, 1=green, 2=blue, 3=white (DeviceRGB, 3 bytes/entry).
	palette := []byte{
		0xFF, 0x00, 0x00,
		0x00, 0xFF, 0x00,
		0x00, 0x00, 0xFF,
		0xFF, 0xFF, 0xFF,
	}
	// 4-bit indices, two per byte (MSB first), rows byte-aligned (w=8 → 4 bytes).
	samples := make([]byte, 0, w*h/2)
	for y := range h {
		for x := 0; x < w; x += 2 {
			hi := byte((x + y) % 4)
			lo := byte((x + 1 + y) % 4)
			samples = append(samples, hi<<4|lo)
		}
	}
	dict := fmt.Sprintf(
		"/Filter /FlateDecode /ColorSpace [ /Indexed /DeviceRGB 3 < %X > ] /BitsPerComponent 4",
		palette)
	return buildImagePage(w, h, zlibCompress(samples), dict)
}

// SMaskImagePDF returns a single-page PDF drawing an opaque blue DeviceRGB image
// with a DeviceGray /SMask whose left half is opaque (255) and right half is
// transparent (0). Exercises soft-mask alpha: the right half must let the white
// page show through.
func SMaskImagePDF() []byte {
	const w, h = 8, 8
	rgb := make([]byte, 0, w*h*3)
	for y := range h {
		_ = y
		for x := range w {
			_ = x
			rgb = append(rgb, 0x00, 0x00, 0xFF) // solid blue
		}
	}
	maskSamples := make([]byte, 0, w*h)
	for y := range h {
		_ = y
		for x := range w {
			if x < w/2 {
				maskSamples = append(maskSamples, 0xFF) // opaque
			} else {
				maskSamples = append(maskSamples, 0x00) // transparent
			}
		}
	}
	return buildImageWithSMaskPage(w, h, zlibCompress(rgb), zlibCompress(maskSamples))
}

// CCITTImagePDF returns a single-page PDF whose image XObject is a Group 4
// (CCITTFaxDecode, K<0) bilevel image: a black frame around a white interior on
// an 8x8 grid. Exercises the CCITT filter feeding the 1-bpc DeviceGray path.
func CCITTImagePDF() []byte {
	const w, h = 8, 8
	rows := make([][]bool, h)
	for y := range rows {
		row := make([]bool, w)
		for x := range row {
			// Black border (the frame), white interior.
			row[x] = x == 0 || y == 0 || x == w-1 || y == h-1
		}
		rows[y] = row
	}
	enc := encodeG4Frame(rows, w)
	dict := fmt.Sprintf(
		"/Filter /CCITTFaxDecode /DecodeParms << /K -1 /Columns %d /Rows %d >> "+
			"/ColorSpace /DeviceGray /BitsPerComponent 1", w, h)
	return buildImagePage(w, h, enc, dict)
}

// ImageMaskPDF returns a single-page PDF drawing a 1-bit /ImageMask stencil under
// a green fill color. The mask's left half is "paint" (sample 0) and right half
// is "don't paint" (sample 1), so the result is a green left half with the white
// page showing through the right. Exercises the ImageMask stencil path.
func ImageMaskPDF() []byte {
	const w, h = 8, 8
	// 1 bpp, rows byte-aligned (w=8 → 1 byte/row). Left 4 px = 0 (paint), right
	// 4 px = 1 (transparent): bit pattern 0000_1111 = 0x0F.
	samples := make([]byte, 0, h)
	for range h {
		samples = append(samples, 0x0F)
	}

	b := newBuilder()
	imgNum := b.addStream(fmt.Sprintf(
		" /Type /XObject /Subtype /Image /Width %d /Height %d "+
			"/ImageMask true /BitsPerComponent 1 /Filter /FlateDecode", w, h),
		zlibCompress(samples))

	// Set a green fill, then draw the stencil scaled to 400x400.
	content := []byte("0 1 0 rg q 400 0 0 400 100 200 cm /Im0 Do Q")
	contentNum := b.addStream("", content)

	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	page := b.addObject(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>",
		pagesNum, imgNum, contentNum))
	if page != pageNum {
		panic("gen: page object number mismatch in ImageMaskPDF")
	}
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}

// buildImagePage assembles a one-page PDF that paints a single image XObject
// (with the given width/height, stream data, and image-dict extras) scaled to
// 400x400 user units near the page origin.
func buildImagePage(w, h int, data []byte, imgDictExtra string) []byte {
	b := newBuilder()

	imgNum := len(b.offsets)
	dict := fmt.Sprintf(" /Type /XObject /Subtype /Image /Width %d /Height %d %s",
		w, h, imgDictExtra)
	got := b.addStream(dict, data)
	if got != imgNum {
		panic("gen: image object number mismatch")
	}

	// Content: scale the unit image space to 400x400 and draw it.
	content := []byte("q 400 0 0 400 100 200 cm /Im0 Do Q")
	contentNum := b.addStream("", content)

	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	pageBody := fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>",
		pagesNum, imgNum, contentNum)
	page := b.addObject(pageBody)
	if page != pageNum {
		panic("gen: page object number mismatch in buildImagePage")
	}
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}

// InlineImagePDF returns a one-page PDF that paints a 2×2 DeviceRGB image using
// an inline image (BI...ID...EI) in the content stream, scaled to 400×400. The
// four pixels are red, green, blue, white — distinct, saturated colors so the
// rasterized output is unambiguously non-blank. The raw sample bytes deliberately
// have no filter, exercising the abbreviated-key inline path end to end.
func InlineImagePDF() []byte {
	b := newBuilder()

	// 2x2 RGB, 8 bpc: 12 bytes, row-major, rows padded to byte boundary (already
	// byte-aligned here). Colors: (R,G / B,W).
	samples := []byte{
		0xFF, 0x00, 0x00, 0x00, 0xFF, 0x00, // row 0: red, green
		0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, // row 1: blue, white
	}
	var content []byte
	content = append(content, []byte("q 400 0 0 400 100 200 cm\nBI /W 2 /H 2 /CS /RGB /BPC 8 ID ")...)
	content = append(content, samples...)
	content = append(content, []byte(" EI\nQ")...)

	contentNum := b.addStream("", content)
	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	pageBody := fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] "+
			"/Resources << >> /Contents %d 0 R >>",
		pagesNum, contentNum)
	page := b.addObject(pageBody)
	if page != pageNum {
		panic("gen: page object number mismatch in InlineImagePDF")
	}
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}

// buildImageWithSMaskPage assembles a one-page PDF painting a DeviceRGB image
// whose /SMask is a separate DeviceGray image, both w×h, scaled to 400x400.
func buildImageWithSMaskPage(w, h int, rgbData, maskData []byte) []byte {
	b := newBuilder()

	maskNum := b.addStream(fmt.Sprintf(
		" /Type /XObject /Subtype /Image /Width %d /Height %d "+
			"/Filter /FlateDecode /ColorSpace /DeviceGray /BitsPerComponent 8", w, h), maskData)

	imgNum := b.addStream(fmt.Sprintf(
		" /Type /XObject /Subtype /Image /Width %d /Height %d "+
			"/Filter /FlateDecode /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask %d 0 R",
		w, h, maskNum), rgbData)

	content := []byte("q 400 0 0 400 100 200 cm /Im0 Do Q")
	contentNum := b.addStream("", content)

	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	page := b.addObject(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>",
		pagesNum, imgNum, contentNum))
	if page != pageNum {
		panic("gen: page object number mismatch in buildImageWithSMaskPage")
	}
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}
