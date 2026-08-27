package draw

import (
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/render/raster"
	"github.com/nathanstitt/doctaculous/pkg/svg"
)

const filterHdr = `xmlns="http://www.w3.org/2000/svg"`

// renderFilterSVG renders src and also returns every degradation line the
// renderer logged, so a test can assert on the log as well as the pixels.
func renderFilterSVG(t *testing.T, src string, w, h int) (*image.RGBA, []string) {
	t.Helper()
	doc, err := svg.Parse([]byte(src), func(f string, a ...any) { t.Logf(f, a...) })
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	stddraw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	dev := raster.New(img)

	var logs []string
	r := New(doc)
	r.Logf = func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	r.DrawVector(dev, render.Identity)
	return img, logs
}

// TestFeFloodFillsTheDefaultRegion proves the -10%/120% default region end to
// end: a 40x40 rect at (30,30) has a bbox of 40 units, so the flood must
// bleed 4 units past each edge — visible at (27,50), outside the rect but
// inside the region.
func TestFeFloodFillsTheDefaultRegion(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="100" height="100">
	  <filter id="f"><feFlood flood-color="rgb(0,0,255)"/></filter>
	  <rect x="30" y="30" width="40" height="40" fill="red" filter="url(#f)"/>
	</svg>`
	img, _ := renderFilterSVG(t, src, 100, 100)

	// Inside the rect: flooded blue, with the red source discarded (feFlood
	// ignores its input entirely).
	if c := img.RGBAAt(50, 50); c.B < 250 || c.R > 5 {
		t.Errorf("center = %+v, want opaque blue (the flood replaces the source)", c)
	}
	// Between the rect edge (30) and the region edge (26): still flooded.
	if c := img.RGBAAt(27, 50); c.B < 250 || c.R > 5 {
		t.Errorf("pixel at x=27 = %+v, want blue: the default region bleeds 10%% (4 units) past the bbox", c)
	}
	// Past the region edge: untouched white.
	if c := img.RGBAAt(20, 50); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("pixel at x=20 = %+v, want white: it is outside the -10%%/120%% region", c)
	}
}

// TestFeFloodExplicitRegionClipsExactly confirms an explicit userSpaceOnUse
// region bounds the flood precisely, so region math errors surface as a
// wrong-sized block rather than a subtle shade.
func TestFeFloodExplicitRegionClipsExactly(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="100" height="100">
	  <filter id="f" filterUnits="userSpaceOnUse" x="20" y="20" width="30" height="30">
	    <feFlood flood-color="rgb(0,0,255)"/>
	  </filter>
	  <rect x="0" y="0" width="100" height="100" fill="red" filter="url(#f)"/>
	</svg>`
	img, _ := renderFilterSVG(t, src, 100, 100)

	if c := img.RGBAAt(35, 35); c.B < 250 {
		t.Errorf("inside the region = %+v, want blue", c)
	}
	for _, p := range [][2]int{{10, 35}, {60, 35}, {35, 10}, {35, 60}} {
		if c := img.RGBAAt(p[0], p[1]); c.R != 255 || c.G != 255 || c.B != 255 {
			t.Errorf("pixel (%d,%d) = %+v, want white — outside the explicit region", p[0], p[1], c)
		}
	}
}

// TestFeFloodOpacityReachesOutput confirms flood-opacity survives the whole
// pipeline (buffer, color conversion, composite) rather than being lost at
// one of the conversions.
func TestFeFloodOpacityReachesOutput(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="60" height="60">
	  <filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="60" height="60">
	    <feFlood flood-color="rgb(0,0,255)" flood-opacity="0.5"/>
	  </filter>
	  <rect x="0" y="0" width="60" height="60" filter="url(#f)"/>
	</svg>`
	img, _ := renderFilterSVG(t, src, 60, 60)
	c := img.RGBAAt(30, 30)
	// Half-opacity blue over a white page: red and green fall to ~128,
	// blue stays 255.
	if c.B < 250 {
		t.Errorf("blue channel = %d, want ~255", c.B)
	}
	if c.R < 110 || c.R > 145 {
		t.Errorf("red channel = %d, want ~128 (half-transparent blue over white)", c.R)
	}
}

// TestFeOffsetShiftsRenderedContent proves feOffset moves real rasterized
// pixels by exactly dx/dy, in the correct DIRECTION on both axes.
//
// The y direction is the one worth pinning: the compositing path maps the
// result through PDF image space, which runs y-up, so an implementation that
// forgets the flip produces a vertically mirrored result that still looks
// like a plausible offset.
func TestFeOffsetShiftsRenderedContent(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="100" height="100">
	  <filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
	    <feOffset dx="20" dy="30"/>
	  </filter>
	  <rect x="10" y="10" width="20" height="20" fill="rgb(0,0,255)" filter="url(#f)"/>
	</svg>`
	img, _ := renderFilterSVG(t, src, 100, 100)

	// The rect moved from (10..30, 10..30) to (30..50, 40..60).
	if c := img.RGBAAt(40, 50); c.B < 250 || c.R > 5 {
		t.Errorf("shifted position (40,50) = %+v, want blue", c)
	}
	if c := img.RGBAAt(20, 20); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("original position (20,20) = %+v, want white — the content moved away", c)
	}
	// A mirrored result would land here instead; assert it does not.
	if c := img.RGBAAt(40, 10); c.B > 250 && c.R < 5 {
		t.Error("content appears above the source: the y offset looks mirrored (PDF image-space flip not undone)")
	}
}

// TestFeOffsetNegative confirms negative dx/dy move content up and left.
func TestFeOffsetNegative(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="100" height="100">
	  <filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
	    <feOffset dx="-20" dy="-20"/>
	  </filter>
	  <rect x="50" y="50" width="20" height="20" fill="rgb(0,0,255)" filter="url(#f)"/>
	</svg>`
	img, _ := renderFilterSVG(t, src, 100, 100)
	if c := img.RGBAAt(40, 40); c.B < 250 || c.R > 5 {
		t.Errorf("(40,40) = %+v, want blue after a -20,-20 shift", c)
	}
	if c := img.RGBAAt(60, 60); c.R != 255 {
		t.Errorf("(60,60) = %+v, want white — the content moved away", c)
	}
}

// TestFeOffsetClampsAtTheRegionEdge confirms content shifted past the filter
// region is clipped rather than wrapping around to the opposite edge.
func TestFeOffsetClampsAtTheRegionEdge(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="100" height="100">
	  <filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
	    <feOffset dx="200" dy="0"/>
	  </filter>
	  <rect x="10" y="40" width="20" height="20" fill="rgb(0,0,255)" filter="url(#f)"/>
	</svg>`
	img, _ := renderFilterSVG(t, src, 100, 100)
	for x := 0; x < 100; x += 5 {
		if c := img.RGBAAt(x, 50); c.B > 250 && c.R < 5 {
			t.Fatalf("blue found at x=%d after shifting far past the region; content wrapped instead of being clipped", x)
		}
	}
}

// TestColorInterpolationFiltersChangesOutput is the DISCRIMINATING color
// space test at the render level: the SAME operation, in linearRGB versus
// sRGB, must produce DIFFERENT pixels, and the default must be linearRGB.
//
// The operation has to MIX colors for the spaces to diverge at all. A plain
// feFlood does not: it converts an authored color into the working space and
// straight back out, which is the identity in 8 bits no matter which space is
// chosen, so a flood-based test passes even with the conversion wired
// backwards or omitted. A FRACTIONAL feOffset does mix — it bilinearly
// interpolates across the source's edge — and interpolating light-linear
// values gives a measurably different midpoint than interpolating
// gamma-encoded ones. That midpoint is what this samples.
func TestColorInterpolationFiltersChangesOutput(t *testing.T) {
	// A white-to-black edge is where the two spaces disagree most: the
	// linear midpoint of 0 and 1 encodes to sRGB ~188, while the sRGB
	// midpoint is ~128.
	renderSpace := func(space string) color.RGBA {
		attr := ""
		if space != "" {
			attr = ` color-interpolation-filters="` + space + `"`
		}
		src := `<svg ` + filterHdr + ` width="40" height="40">
		  <filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="40" height="40"` + attr + `>
		    <feOffset dx="0.5" dy="0"/>
		  </filter>
		  <g filter="url(#f)">
		    <rect x="0" y="0" width="20" height="40" fill="rgb(255,255,255)"/>
		    <rect x="20" y="0" width="20" height="40" fill="rgb(0,0,0)"/>
		  </g>
		</svg>`
		img, _ := renderFilterSVG(t, src, 40, 40)
		// The half-pixel shift puts the blended edge column at x=20.
		return img.RGBAAt(20, 20)
	}

	def := renderSpace("")
	lin := renderSpace("linearRGB")
	srgb := renderSpace("sRGB")

	t.Logf("default=%+v linearRGB=%+v sRGB=%+v", def, lin, srgb)

	if def != lin {
		t.Errorf("the default (%+v) differs from explicit linearRGB (%+v); linearRGB IS the SVG default, "+
			"unlike everything else in this engine", def, lin)
	}
	if lin == srgb {
		t.Fatal("linearRGB and sRGB produced identical pixels; the color space is not reaching the pixel math")
	}
	// Direction matters, not just difference: averaging in LINEAR space and
	// encoding back to sRGB yields a LIGHTER midpoint than averaging the
	// gamma-encoded values. A test asserting only inequality would pass with
	// the two spaces swapped.
	if lin.R <= srgb.R {
		t.Errorf("linearRGB midpoint (%d) is not lighter than the sRGB one (%d); the spaces look swapped",
			lin.R, srgb.R)
	}
}

// TestUnsupportedPrimitiveRendersUnfilteredAndLogs is the deferral test the
// task requires at the RENDER level: an unimplemented primitive must leave
// the element visible and emit a log naming it. A silently empty result — the
// failure mode this guards — would make the element vanish with no
// diagnostic.
func TestUnsupportedPrimitiveRendersUnfilteredAndLogs(t *testing.T) {
	for _, name := range []string{
		"feTurbulence", "feConvolveMatrix", "feDiffuseLighting", "feSpecularLighting",
		"feMorphology", "feImage", "feTile", "feComponentTransfer", "feDisplacementMap",
		"feGaussianBlur", "feBlend", "feComposite", "feColorMatrix", "feMerge", "feDropShadow",
	} {
		t.Run(name, func(t *testing.T) {
			src := `<svg ` + filterHdr + ` width="60" height="60">
			  <filter id="f"><` + name + `/></filter>
			  <rect x="10" y="10" width="40" height="40" fill="rgb(0,0,255)" filter="url(#f)"/>
			</svg>`
			img, logs := renderFilterSVG(t, src, 60, 60)

			if c := img.RGBAAt(30, 30); c.B < 250 || c.R > 5 {
				t.Errorf("center = %+v, want the element painted UNFILTERED (blue), not dropped", c)
			}
			found := false
			for _, l := range logs {
				if strings.Contains(l, "not supported") {
					found = true
				}
			}
			if !found {
				t.Errorf("no degradation logged for %s; logs = %v", name, logs)
			}
		})
	}
}

// TestFilterOnTextUsesRealGlyphBounds is the textUserBounds seam test.
//
// The filter region is objectBoundingBox, so it is computed from the text's
// bounding box. pkg/svg's build-time textBBox estimates a half em per
// character, which for this string is far narrower than the real advance;
// using it would place the region's right edge well short of the glyphs and
// visibly CLIP the flood. Filling the region with an opaque color and probing
// near the text's true right extent detects that directly.
func TestFilterOnTextUsesRealGlyphBounds(t *testing.T) {
	const src = `<svg ` + filterHdr + ` width="300" height="80">
	  <filter id="f"><feFlood flood-color="rgb(0,0,255)"/></filter>
	  <text x="10" y="50" font-family="sans-serif" font-size="30" filter="url(#f)">WWWWWWWW</text>
	</svg>`
	img, _ := renderFilterSVG(t, src, 300, 80)

	// Find the flood's horizontal extent.
	minX, maxX := -1, -1
	for x := 0; x < 300; x++ {
		c := img.RGBAAt(x, 40)
		if c.B > 250 && c.R < 5 {
			if minX < 0 {
				minX = x
			}
			maxX = x
		}
	}
	if minX < 0 {
		t.Fatal("the filter produced no output over the text at all")
	}
	t.Logf("flood spans x=%d..%d", minX, maxX)

	// Eight 'W' glyphs at 30px are far wider than the half-em-per-character
	// estimate (which would predict ~8*15 = 120 units plus bleed). The real
	// advance is well past 200.
	if maxX < 200 {
		t.Errorf("flood right edge at x=%d is too narrow for eight 30px 'W' glyphs; "+
			"the region looks like it came from the half-em textBBox estimate rather than real glyph bounds", maxX)
	}
}

// TestEnormousFilterRegionIsClampedNotAllocated is the memory-bound test: a
// filter region vastly larger than the canvas must not allocate a buffer
// proportional to the REGION, and must still render correctly.
//
// The guard is structural rather than a cap that trips: the region is
// intersected with the part of the canvas it could possibly reach BEFORE any
// buffer is allocated, so a crafted `width="400000"` costs the same handful
// of pixels a sane region would. A test that only asserted "it did not
// crash" would pass with an unbounded allocation that merely happened to
// succeed, so this asserts the visible OUTPUT is correct too — proving the
// clamp preserved the region rather than discarding it.
//
// If the clamp is ever removed, this test allocates ~1.6e11 pixels and the
// run dies, which is exactly the signal wanted.
func TestEnormousFilterRegionIsClampedNotAllocated(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="80" height="80">
	  <filter id="f" filterUnits="userSpaceOnUse" x="-100000" y="-100000" width="400000" height="400000">
	    <feFlood flood-color="rgb(0,255,0)"/>
	  </filter>
	  <rect x="10" y="10" width="60" height="60" fill="rgb(0,0,255)" filter="url(#f)"/>
	</svg>`
	img, _ := renderFilterSVG(t, src, 80, 80)

	// The region covers the whole canvas, so the flood must reach every
	// corner: the clamp trimmed the region to the canvas without shrinking
	// what actually paints.
	for _, p := range [][2]int{{2, 2}, {40, 40}, {77, 77}} {
		if c := img.RGBAAt(p[0], p[1]); c.G < 250 || c.R > 5 {
			t.Errorf("pixel (%d,%d) = %+v, want the green flood covering the clamped region", p[0], p[1], c)
		}
	}
}

// TestFilterPixelCapDegradesUnfiltered covers the allocation cap itself, on
// the path where clamping to the canvas cannot save it: a huge CANVAS with a
// region to match. The element must still paint (unfiltered) and log, never
// vanish.
func TestFilterPixelCapDegradesUnfiltered(t *testing.T) {
	if maxFilterPixels > 16<<20 {
		t.Skip("cap too large to exercise cheaply")
	}
	side := 3000 // 9M pixels, above the 4M cap
	src := `<svg ` + filterHdr + ` width="3000" height="3000">
	  <filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="3000" height="3000">
	    <feFlood flood-color="rgb(0,255,0)"/>
	  </filter>
	  <rect x="100" y="100" width="2800" height="2800" fill="rgb(0,0,255)" filter="url(#f)"/>
	</svg>`
	img, logs := renderFilterSVG(t, src, side, side)

	if c := img.RGBAAt(1500, 1500); c.B < 250 || c.R > 5 {
		t.Errorf("center = %+v, want the element painted UNFILTERED (blue) once the pixel cap trips", c)
	}
	found := false
	for _, l := range logs {
		if strings.Contains(l, "filter region") {
			found = true
		}
	}
	if !found {
		t.Errorf("the pixel cap did not log; logs = %v", logs)
	}
}

// TestFilterDegradesWhenBackendCannotRasterize covers the pdfwrite path
// without importing it: a Device whose RenderOffscreen returns nil (the
// documented degradation for a backend with no pixel buffer) must still
// leave the element painted, and must log.
func TestFilterDegradesWhenBackendCannotRasterize(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="60" height="60">
	  <filter id="f"><feFlood flood-color="rgb(0,255,0)"/></filter>
	  <rect x="10" y="10" width="40" height="40" fill="rgb(0,0,255)" filter="url(#f)"/>
	</svg>`
	doc, err := svg.Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 60, 60))
	stddraw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	dev := &noOffscreenDevice{Device: raster.New(img)}

	var logs []string
	r := New(doc)
	r.Logf = func(f string, a ...any) { logs = append(logs, f) }
	r.DrawVector(dev, render.Identity)

	if c := img.RGBAAt(30, 30); c.B < 250 || c.R > 5 {
		t.Errorf("center = %+v, want the element painted UNFILTERED (blue) when the backend cannot rasterize", c)
	}
	found := false
	for _, l := range logs {
		if strings.Contains(l, "cannot rasterize offscreen") {
			found = true
		}
	}
	if !found {
		t.Errorf("no log for the no-raster degradation; logs = %v", logs)
	}
	if dev.painted == 0 {
		t.Error("the source was never painted onto the real device")
	}
}

// noOffscreenDevice models a vector backend (pdfwrite): it can draw, but
// RenderOffscreen reports that it has no pixel buffer, exactly as
// render.Device documents.
type noOffscreenDevice struct {
	render.Device
	painted int
}

func (d *noOffscreenDevice) RenderOffscreen(image.Point, func(render.Device)) *image.RGBA {
	return nil
}

func (d *noOffscreenDevice) Fill(p *render.Path, paint render.FillPaint) {
	d.painted++
	d.Device.Fill(p, paint)
}

// TestFilterNeverPanicsOnDegenerateInput sweeps malformed and degenerate
// filters through the whole renderer, upholding the never-panic-on-malformed
// -input rule at the layer that allocates buffers.
func TestFilterNeverPanicsOnDegenerateInput(t *testing.T) {
	cases := []string{
		`<filter id="f"/>`,
		`<filter id="f"><feFlood/></filter>`,
		`<filter id="f" x="0" y="0" width="0" height="0"><feFlood/></filter>`,
		`<filter id="f" width="-1"><feFlood/></filter>`,
		`<filter id="f"><feOffset dx="NaN" dy="bogus"/></filter>`,
		`<filter id="f"><feFlood width="0" height="0"/></filter>`,
		`<filter id="f" filterUnits="userSpaceOnUse" x="1e30" y="1e30" width="1e30" height="1e30"><feFlood/></filter>`,
		`<filter id="f"><feOffset in="nothing"/><feOffset in="alsoNothing"/></filter>`,
	}
	for i, def := range cases {
		src := `<svg ` + filterHdr + ` width="50" height="50">` + def +
			`<rect x="5" y="5" width="40" height="40" fill="blue" filter="url(#f)"/></svg>`
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("case %d panicked: %v\n%s", i, r, def)
				}
			}()
			renderFilterSVG(t, src, 50, 50)
		}()
	}
}

// TestFilterOnRotatedElementProducesRotatedOutput pins the filter-space
// change of basis: a filter on a rotated element must produce a ROTATED
// result, not the axis-aligned bounding box of one.
//
// An implementation that flooded the region's device-space AABB (the natural
// first attempt) paints an upright rectangle here, which this detects by
// probing a corner that lies inside the AABB but outside the rotated region.
func TestFilterOnRotatedElementProducesRotatedOutput(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="200" height="200">
	  <filter id="f" filterUnits="userSpaceOnUse" x="50" y="50" width="100" height="100">
	    <feFlood flood-color="rgb(0,0,255)"/>
	  </filter>
	  <g transform="rotate(45 100 100)">
	    <rect x="50" y="50" width="100" height="100" fill="red" filter="url(#f)"/>
	  </g>
	</svg>`
	img, _ := renderFilterSVG(t, src, 200, 200)

	// Center of the rotated square: inside either way.
	if c := img.RGBAAt(100, 100); c.B < 250 {
		t.Fatalf("center = %+v, want blue", c)
	}
	// A corner of the UNROTATED region. After a 45-degree rotation about the
	// center this lies outside the diamond, so it must be untouched.
	if c := img.RGBAAt(56, 56); c.B > 250 && c.R < 5 {
		t.Error("the region corner is filled: the filter produced an axis-aligned rect instead of a rotated one")
	}
	// A point on the diamond's horizontal extreme, outside the unrotated
	// region's left edge — filled only if the result really is rotated.
	if c := img.RGBAAt(38, 100); c.B < 250 {
		t.Errorf("(38,100) = %+v, want blue: the rotated region reaches further left than the unrotated one", c)
	}
}
