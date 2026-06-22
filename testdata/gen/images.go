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
