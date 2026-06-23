package gen

import "fmt"

// This file generates the Phase-A shading fixtures: axial (Type 2), radial
// (Type 3), and function-based (Type 1) gradients painted with the `sh` operator.
// Each clips `sh` to a rectangle first (re W n) so the gradient fills a bounded
// region rather than the whole page, which keeps the goldens legible and
// exercises the clip → FillShading path.

// ShadingAxialPDF returns a single-page PDF that paints an axial (Type 2) shading
// with the `sh` operator: a red→blue ramp along the diagonal from (100,100) to
// (500,500), clipped to that 400×400 box. /Extend [true true] colors the region
// beyond the axis endpoints solid red / solid blue.
func ShadingAxialPDF() []byte {
	shading := "<< /ShadingType 2 /ColorSpace /DeviceRGB " +
		"/Coords [100 100 500 500] /Domain [0 1] " +
		"/Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> " +
		"/Extend [true true] >>"
	resources := fmt.Sprintf("<< /Shading << /Sh1 %s >> >>", shading)
	// Clip to the axis-aligned box, then paint the shading across the clip.
	content := []byte("q 100 100 400 400 re W n /Sh1 sh Q")
	return buildSinglePage(content, resources)
}

// ShadingRadialPDF returns a single-page PDF that paints a radial (Type 3)
// shading: concentric circles centered at (300,300) growing from radius 0 to 200,
// ramping green→yellow, clipped to the bounding box of the outer circle.
func ShadingRadialPDF() []byte {
	shading := "<< /ShadingType 3 /ColorSpace /DeviceRGB " +
		"/Coords [300 300 0 300 300 200] /Domain [0 1] " +
		"/Function << /FunctionType 2 /Domain [0 1] /C0 [0 1 0] /C1 [1 1 0] /N 1 >> " +
		"/Extend [false true] >>"
	resources := fmt.Sprintf("<< /Shading << /Sh1 %s >> >>", shading)
	content := []byte("q 100 100 400 400 re W n /Sh1 sh Q")
	return buildSinglePage(content, resources)
}

// ShadingFunctionPDF returns a single-page PDF that paints a function-based
// (Type 1) shading: a 2-D color field over the domain [0 1]×[0 1], mapped onto a
// 400×400 box via the shading /Matrix (scale 400, translate 100). The Type 4
// PostScript function maps (x,y) → (r,g,b) = (x, 0, y), so the box shows a smooth
// two-axis field: black at (0,0), red along +x, blue along +y, magenta at (1,1).
func ShadingFunctionPDF() []byte {
	b := newBuilder()

	// Type-4 (PostScript calculator) function as a stream. Entry stack: x y.
	//   0     -> x y 0
	//   exch  -> x 0 y     (outputs read bottom→top: r=x, g=0, b=y)
	fnNum := b.addStream(
		" /FunctionType 4 /Domain [0 1 0 1] /Range [0 1 0 1 0 1]",
		[]byte("{ 0 exch }"))

	shading := fmt.Sprintf(
		"<< /ShadingType 1 /ColorSpace /DeviceRGB /Domain [0 1 0 1] "+
			"/Matrix [400 0 0 400 100 100] /Function %d 0 R >>", fnNum)
	resources := fmt.Sprintf("<< /Shading << /Sh1 %s >> >>", shading)

	contentNum := b.addStream("", []byte("q 100 100 400 400 re W n /Sh1 sh Q"))

	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	page := b.addObject(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Resources %s /Contents %d 0 R >>",
		pagesNum, resources, contentNum))
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}

// ShadingPatternPDF returns a single-page PDF that fills a path with a shading
// pattern (PatternType 2): a /Pattern resource wrapping an axial red→blue shading
// is selected as the fill "color" via `/Pattern cs /P1 scn`, then a diamond path
// is filled with `f`. The gradient must fill the path (clipped to it), not the
// whole page — locking down scn pattern resolution and the fillPath shading
// branch. The pattern /Matrix is identity, so the shading axis is in page space.
func ShadingPatternPDF() []byte {
	pattern := "<< /PatternType 2 /Matrix [1 0 0 1 0 0] " +
		"/Shading << /ShadingType 2 /ColorSpace /DeviceRGB " +
		"/Coords [150 150 450 450] /Domain [0 1] " +
		"/Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> " +
		"/Extend [true true] >> >>"
	resources := fmt.Sprintf("<< /Pattern << /P1 %s >> >>", pattern)
	// Select the Pattern color space, set the shading pattern, fill a diamond.
	content := []byte(
		"/Pattern cs /P1 scn " +
			"300 150 m 450 300 l 300 450 l 150 300 l h f",
	)
	return buildSinglePage(content, resources)
}
