package raster

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // register JPEG decoder for DCTDecode XObjects
	"math"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/pdf/content"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Options configures page rendering.
type Options struct {
	// DPI is the output resolution; PDF user space is 72 units/inch, so the scale
	// is DPI/72. Defaults to 72 when zero or negative.
	DPI float64
	// Background fills the page before drawing. Defaults to opaque white. Use a
	// transparent color for an alpha page.
	Background color.Color
	// Logf, if set, receives debug messages for unsupported features.
	Logf func(string, ...any)
}

// RenderPage rasterizes a single page to an *image.RGBA at the requested DPI,
// honoring the page's /Rotate. It runs the content interpreter against the
// raster Device. Unsupported features (e.g. text without a font backend yet)
// are skipped with a debug log rather than failing the render.
//
// The context is checked before the (potentially expensive) interpretation
// begins so batch callers can cancel promptly.
func RenderPage(ctx context.Context, pg *pdf.Page, opts Options) (*image.RGBA, error) {
	if pg == nil {
		return nil, fmt.Errorf("raster: nil page")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dpi := opts.DPI
	if dpi <= 0 {
		dpi = 72
	}
	scale := dpi / 72

	// Page size in points, then pixels. /Rotate swaps dimensions for 90/270.
	wpt := pg.MediaBox.Width()
	hpt := pg.MediaBox.Height()
	if !isFinitePositive(wpt) || !isFinitePositive(hpt) {
		return nil, fmt.Errorf("raster: invalid MediaBox %gx%g", wpt, hpt)
	}
	// Compute dimensions in float64, then validate before casting to int, so an
	// attacker-controlled MediaBox or DPI cannot overflow int or trigger a
	// multi-gigapixel allocation.
	fW := math.Ceil(wpt * scale)
	fH := math.Ceil(hpt * scale)
	if !isFinitePositive(fW) || !isFinitePositive(fH) {
		return nil, fmt.Errorf("raster: degenerate scaled size %gx%g", fW, fH)
	}
	const maxPixels = 1 << 27 // ~134M px (e.g. ~11600² or A0 @ 300dpi), generous but bounded
	if fW*fH > float64(maxPixels) {
		return nil, fmt.Errorf("raster: page too large (%.0fx%.0f px exceeds %d-pixel cap; lower DPI)", fW, fH, maxPixels)
	}
	pxW := int(fW)
	pxH := int(fH)
	if pg.Rotate == 90 || pg.Rotate == 270 {
		pxW, pxH = pxH, pxW
	}
	if pxW <= 0 || pxH <= 0 {
		return nil, fmt.Errorf("raster: degenerate page size %dx%d", pxW, pxH)
	}

	img := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	bg := opts.Background
	if bg == nil {
		bg = color.White
	}
	fillBackground(img, bg)

	dev := New(img)
	dev.SetLogf(opts.Logf)
	if pg.Rotate != 0 && pg.Rotate != 90 && pg.Rotate != 180 && pg.Rotate != 270 && opts.Logf != nil {
		opts.Logf("raster: unexpected /Rotate %d treated as 0", pg.Rotate)
	}
	base := pageMatrix(pg, scale, wpt, hpt)

	content0, err := pg.ContentBytes()
	if err != nil {
		// No content (or undecodable): a blank page is a valid result.
		if opts.Logf != nil {
			opts.Logf("raster: page content unavailable: %v", err)
		}
		return img, nil
	}

	res := &pageResources{doc: pg.Doc(), dict: pg.Resources, logf: opts.Logf}
	interp := content.New(pg.Doc(), dev, res, base, content.Options{
		Logf:   opts.Logf,
		MaxOps: 5_000_000,
	})
	if err := interp.Run(content0); err != nil {
		return nil, fmt.Errorf("raster: interpreting page: %w", err)
	}
	return img, nil
}

// isFinitePositive reports whether v is a finite, strictly positive number.
func isFinitePositive(v float64) bool {
	return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v)
}

// pageMatrix builds the transform from PDF user space (points, y-up, origin
// bottom-left) to device pixels (y-down, origin top-left) at the given scale,
// applying the page's /Rotate. wpt/hpt are the unrotated page size in points.
//
// It is the composition: scale → rotate clockwise by /Rotate about the origin →
// translate the rotated content back into the positive quadrant → flip y into
// the top-left device frame. Each branch is written out directly (rather than
// composed via Mul) so the six coefficients are easy to verify against a known
// point.
func pageMatrix(pg *pdf.Page, scale, wpt, hpt float64) render.Matrix {
	w, h := wpt*scale, hpt*scale // page size in pixels (unrotated)
	switch pg.Rotate {
	case 90:
		// User (x,y) → device (h_px - y_scaled? ) : 90° CW then y-flip.
		// Verified: user (0,0)→(0,0); (wpt,0)→(0,w); (0,hpt)→(h,0).
		return render.Matrix{A: 0, B: scale, C: scale, D: 0, E: 0, F: 0}
	case 180:
		// 180°: user (0,0)→(w,0) bottom-left maps to top-right then flips.
		// Verified: (0,0)→(w,0); (wpt,0)→(0,0); (0,hpt)→(w,h).
		return render.Matrix{A: -scale, B: 0, C: 0, D: scale, E: w, F: 0}
	case 270:
		// Verified: (0,0)→(h,w); (wpt,0)→(h,0); (0,hpt)→(0,w).
		return render.Matrix{A: 0, B: -scale, C: -scale, D: 0, E: h, F: w}
	default: // 0°
		// Plain scale + y-flip. Verified: (0,0)→(0,h); (wpt,0)→(w,h); (0,hpt)→(0,0).
		return render.Matrix{A: scale, B: 0, C: 0, D: -scale, E: 0, F: h}
	}
}

// pageResources resolves fonts, image XObjects, and form XObjects from a page's
// /Resources. The same type backs nested form resources (see Form), so form
// content resolves names against its own /Resources, falling back to the page's.
type pageResources struct {
	doc  *pdf.Document
	dict pdf.Dict
	logf func(string, ...any)
}

// Font resolves a font resource by name to a GlyphSource. Unsupported or
// malformed fonts (non-embedded base-14, classic Type1, non-Identity Type0
// CMaps) return nil, so the interpreter advances the cursor without drawing.
func (r *pageResources) Font(name string) content.GlyphSource {
	fonts := r.doc.GetDict(r.dict["Font"])
	if fonts == nil {
		return nil
	}
	fontDict := r.doc.GetDict(fonts[pdf.Name(name)])
	if fontDict == nil {
		return nil
	}
	src, err := font.New(r.doc, fontDict, r.logf)
	if err != nil {
		if r.logf != nil {
			r.logf("raster: font %q: %v", name, err)
		}
		return nil
	}
	return src
}

// Form resolves a form XObject by name to its decoded content stream, its scoped
// resources, and its /Matrix. It returns ok=false if name is not a form XObject
// or its content cannot be decoded, so the interpreter skips it gracefully. Per
// the PDF spec a form without its own /Resources inherits the page's, so the
// child pageResources falls back to this dict.
func (r *pageResources) Form(name string) ([]byte, content.Resources, render.Matrix, bool) {
	xobjs := r.doc.GetDict(r.dict["XObject"])
	if xobjs == nil {
		return nil, nil, render.Identity, false
	}
	s := r.doc.GetStream(xobjs[pdf.Name(name)])
	if s == nil {
		return nil, nil, render.Identity, false
	}
	if sub, _ := r.doc.GetName(s.Dict["Subtype"]); sub != "Form" {
		return nil, nil, render.Identity, false
	}
	data, _, err := r.doc.DecodedStream(s)
	if err != nil {
		if r.logf != nil {
			r.logf("raster: form %q: %v", name, err)
		}
		return nil, nil, render.Identity, false
	}

	// A form's /Resources is optional; fall back to the page's so names resolve.
	childDict := r.doc.GetDict(s.Dict["Resources"])
	if childDict == nil {
		childDict = r.dict
	}
	child := &pageResources{doc: r.doc, dict: childDict, logf: r.logf}

	return data, child, formMatrix(r.doc, s.Dict["Matrix"]), true
}

// formMatrix reads a form XObject's /Matrix (six numbers) into a render.Matrix,
// returning identity when absent or malformed (the PDF default).
func formMatrix(doc *pdf.Document, o pdf.Object) render.Matrix {
	arr := doc.GetArray(o)
	if len(arr) != 6 {
		return render.Identity
	}
	var v [6]float64
	for i, e := range arr {
		f, ok := pdf.Number(doc.Resolve(e))
		if !ok {
			return render.Identity
		}
		v[i] = f
	}
	return render.Matrix{A: v[0], B: v[1], C: v[2], D: v[3], E: v[4], F: v[5]}
}

func (r *pageResources) Image(name string) (image.Image, bool) {
	xobjs := r.doc.GetDict(r.dict["XObject"])
	if xobjs == nil {
		return nil, false
	}
	s := r.doc.GetStream(xobjs[pdf.Name(name)])
	if s == nil {
		return nil, false
	}
	if sub, _ := r.doc.GetName(s.Dict["Subtype"]); sub != "Image" {
		return nil, false
	}
	img, err := decodeImageXObject(r.doc, s)
	if err != nil {
		if r.logf != nil {
			r.logf("raster: image %q: %v", name, err)
		}
		return nil, false
	}
	return img, true
}

// decodeImageXObject turns an image XObject stream into an image.Image, handling
// the two paths the core corpus exercises: DCTDecode (baseline JPEG via the
// stdlib decoder) and raw FlateDecode DeviceRGB 8-bit samples.
func decodeImageXObject(doc *pdf.Document, s *pdf.Stream) (image.Image, error) {
	data, imgFilter, err := doc.DecodedStream(s)
	if err != nil {
		return nil, err
	}
	w, _ := doc.GetInt(s.Dict["Width"])
	h, _ := doc.GetInt(s.Dict["Height"])
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("bad dimensions %dx%d", w, h)
	}

	switch imgFilter {
	case "DCTDecode":
		img, _, derr := image.Decode(bytes.NewReader(data))
		if derr != nil {
			return nil, fmt.Errorf("jpeg decode: %w", derr)
		}
		return img, nil
	case "":
		// Raw samples. First pass supports 8-bit DeviceRGB (3 bytes/pixel).
		bpc, _ := doc.GetInt(s.Dict["BitsPerComponent"])
		cs, _ := doc.GetName(s.Dict["ColorSpace"])
		if bpc != 8 || cs != "DeviceRGB" {
			return nil, fmt.Errorf("unsupported raw image: %d bpc, /%s", bpc, cs)
		}
		if len(data) < w*h*3 {
			return nil, fmt.Errorf("short sample data: %d < %d", len(data), w*h*3)
		}
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				i := (y*w + x) * 3
				img.SetRGBA(x, y, color.RGBA{R: data[i], G: data[i+1], B: data[i+2], A: 0xFF})
			}
		}
		return img, nil
	default:
		return nil, fmt.Errorf("unsupported image filter %s", imgFilter)
	}
}
