package omnidoc

import (
	"context"
	"image"
	"image/color"
	"strings"
	"testing"
)

// renderGradientBox lays out a single 100x100 box carrying style on a 120x120pt
// page and rasterizes it at 72 DPI, so one point is one pixel and the box
// occupies exactly (0,0)-(100,100). Every sampled coordinate below is therefore
// a point INSIDE the gradient box, in the box's own coordinates.
func renderGradientBox(t *testing.T, style string) *image.RGBA {
	t.Helper()
	src := `<!DOCTYPE html><html><body style="margin:0"><div style="width:100px;height:100px;` +
		style + `"></div></body></html>`
	doc, err := OpenHTMLBytes([]byte(src), WithPageSize(120, 120))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		MaxWidthPx: 120, MaxHeightPx: 120, Background: color.White})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("RasterizePage returned %T, want *image.RGBA", img)
	}
	return rgba
}

// wantPixel asserts a sampled pixel is within tol per channel of the expected
// colour. tol absorbs the rasterizer's own rounding; it is deliberately tight
// (a gradient that is merely "in the right family" would still fail).
func wantPixel(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, tol int, what string) {
	t.Helper()
	got := img.RGBAAt(x, y)
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	if d(got.R, want.R) > tol || d(got.G, want.G) > tol || d(got.B, want.B) > tol {
		t.Errorf("%s: pixel (%d,%d) = %v, want %v (tolerance %d)", what, x, y, got, want, tol)
	}
}

// TestLinearGradientPaintsKnownColours is the pixel-level proof that a gradient
// actually paints, and paints the RIGHT colours in the RIGHT places — not merely
// that something non-white appeared.
func TestLinearGradientPaintsKnownColours(t *testing.T) {
	t.Parallel()
	black := color.RGBA{0, 0, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	mid := color.RGBA{128, 0, 128, 255}

	tests := []struct {
		name   string
		style  string
		checks []struct {
			x, y int
			want color.RGBA
		}
	}{
		{
			// `to right` runs the ramp left to right; the colour must be
			// constant down each column.
			name:  "to right red to blue",
			style: "background:linear-gradient(to right, red, blue)",
			checks: []struct {
				x, y int
				want color.RGBA
			}{
				{1, 50, red}, {50, 50, mid}, {98, 50, blue},
				{1, 5, red}, {1, 95, red}, // constant down the column
				{50, 5, mid}, {50, 95, mid},
			},
		},
		{
			// The default direction is `to bottom`, so the ramp runs top to
			// bottom and each ROW is constant.
			name:  "default direction is to bottom",
			style: "background:linear-gradient(black, white)",
			checks: []struct {
				x, y int
				want color.RGBA
			}{
				{50, 1, black}, {50, 50, color.RGBA{128, 128, 128, 255}}, {50, 98, white},
				{5, 50, color.RGBA{128, 128, 128, 255}},
				{95, 50, color.RGBA{128, 128, 128, 255}},
			},
		},
		{
			// 90deg is `to right`: the two spellings must agree exactly.
			name:  "90deg equals to right",
			style: "background:linear-gradient(90deg, red, blue)",
			checks: []struct {
				x, y int
				want color.RGBA
			}{
				{1, 50, red}, {50, 50, mid}, {98, 50, blue},
			},
		},
		{
			// `to left` reverses it.
			name:  "to left",
			style: "background:linear-gradient(to left, red, blue)",
			checks: []struct {
				x, y int
				want color.RGBA
			}{
				{1, 50, blue}, {98, 50, red},
			},
		},
		{
			// Explicit stop positions compress the ramp: everything before 25%
			// is solid red, everything after 75% solid blue, and the midpoint
			// is the halfway blend.
			name:  "explicit stop positions",
			style: "background:linear-gradient(to right, red 25%, blue 75%)",
			checks: []struct {
				x, y int
				want color.RGBA
			}{
				{5, 50, red}, {20, 50, red},
				{50, 50, mid},
				{80, 50, blue}, {98, 50, blue},
			},
		},
		{
			// Two stops at the same position are a HARD break with no blend.
			name:  "hard colour break",
			style: "background:linear-gradient(to right, red 50%, blue 50%)",
			checks: []struct {
				x, y int
				want color.RGBA
			}{
				{25, 50, red}, {48, 50, red},
				{52, 50, blue}, {75, 50, blue},
			},
		},
		{
			// Three stops with omitted positions spread evenly: red at 0,
			// white at 50%, blue at 100%.
			name:  "three stops spread evenly",
			style: "background:linear-gradient(to right, red, white, blue)",
			checks: []struct {
				x, y int
				want color.RGBA
			}{
				{1, 50, red}, {50, 50, white}, {98, 50, blue},
				{25, 50, color.RGBA{255, 128, 128, 255}}, // halfway red->white
			},
		},
		{
			// A repeating gradient recurs with a 20px period: red for the
			// first half of each period, blue for the second.
			name:  "repeating linear",
			style: "background:repeating-linear-gradient(to right, red 0, red 10px, blue 10px, blue 20px)",
			checks: []struct {
				x, y int
				want color.RGBA
			}{
				{5, 50, red}, {15, 50, blue},
				{25, 50, red}, {35, 50, blue},
				{85, 50, red}, {95, 50, blue},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := renderGradientBox(t, tt.style)
			for _, c := range tt.checks {
				wantPixel(t, img, c.x, c.y, c.want, 10, tt.name)
			}
		})
	}
}

// TestLinearGradientCornerPixels checks the corner case END TO END, where a
// naive 45-degree implementation would visibly differ. On a WIDE box a
// `to bottom right` gradient's first stop colour must appear exactly at the
// top-left corner and the last exactly at the bottom-right, with the
// perpendicular through each corner staying that colour.
func TestLinearGradientCornerPixels(t *testing.T) {
	t.Parallel()
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:100px;height:40px;background:linear-gradient(to bottom right, black 0, white 100%)"></div>` +
		`</body></html>`
	doc, err := OpenHTMLBytes([]byte(src), WithPageSize(120, 60))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		MaxWidthPx: 120, MaxHeightPx: 60, Background: color.White})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba := img.(*image.RGBA)

	// The gradient's start corner is black and its end corner white.
	if c := rgba.RGBAAt(1, 1); c.R > 30 {
		t.Errorf("top-left corner = %v, want near black", c)
	}
	if c := rgba.RGBAAt(98, 38); c.R < 225 {
		t.Errorf("bottom-right corner = %v, want near white", c)
	}

	// The defining property: the OTHER two corners both sit on the gradient
	// line's perpendicular through the centre, so both must be the MIDPOINT
	// grey — and equal to each other. A 45-degree implementation on this 100x40
	// box puts them at visibly different values.
	tr := rgba.RGBAAt(98, 1)
	bl := rgba.RGBAAt(1, 38)
	if d := int(tr.R) - int(bl.R); d > 6 || d < -6 {
		t.Errorf("top-right (%v) and bottom-left (%v) must both be the midpoint grey; "+
			"they differ by %d, which is what a 45-degree gradient line produces on a non-square box",
			tr, bl, d)
	}
	if tr.R < 100 || tr.R > 155 {
		t.Errorf("top-right = %v, want the midpoint grey (~128)", tr)
	}
}

// TestRadialGradientPaintsKnownColours proves the radial path end to end.
func TestRadialGradientPaintsKnownColours(t *testing.T) {
	t.Parallel()
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}

	t.Run("centred ellipse", func(t *testing.T) {
		// A default radial gradient on a SQUARE box is a circle centred in the
		// box, farthest-corner sized.
		img := renderGradientBox(t, "background:radial-gradient(red, blue)")
		wantPixel(t, img, 50, 50, red, 4, "centre")
		// The four points at the same distance from the centre must share a
		// colour — that is what makes it radial.
		up := img.RGBAAt(50, 20)
		down := img.RGBAAt(50, 80)
		left := img.RGBAAt(20, 50)
		right := img.RGBAAt(80, 50)
		for _, c := range []color.RGBA{down, left, right} {
			if int(c.R)-int(up.R) > 4 || int(up.R)-int(c.R) > 4 {
				t.Errorf("equidistant samples differ: %v vs %v", up, c)
			}
		}
		// Farther out is closer to the end colour than nearer in.
		near := img.RGBAAt(60, 50)
		far := img.RGBAAt(95, 50)
		if far.B <= near.B {
			t.Errorf("blue does not increase outward: near=%v far=%v", near, far)
		}
	})

	t.Run("closest-side circle", func(t *testing.T) {
		// Centred on a 100x100 box, closest-side gives radius 50, so the ramp
		// reaches its end colour exactly at each side's midpoint.
		img := renderGradientBox(t, "background:radial-gradient(circle closest-side, red, blue)")
		wantPixel(t, img, 50, 50, red, 4, "centre")
		wantPixel(t, img, 50, 2, blue, 16, "top edge (one radius away)")
		wantPixel(t, img, 2, 50, blue, 16, "left edge (one radius away)")
		// Beyond one radius the pad spread holds the end colour solid, so the
		// CORNERS (farther than the radius) are fully blue.
		wantPixel(t, img, 3, 3, blue, 4, "corner, beyond the ending circle")
	})

	t.Run("explicit centre", func(t *testing.T) {
		// `at 0 0` puts the centre in the top-left corner, so the ramp runs
		// outward from there. farthest-side is used rather than closest-side
		// because the latter's radius at a corner is ZERO — see
		// TestRadialGradientDegenerateRadiusDegradesHonestly.
		img := renderGradientBox(t, "background:radial-gradient(circle farthest-side at 0 0, red, blue)")
		wantPixel(t, img, 1, 1, red, 6, "the centre at the top-left corner")
		if c := img.RGBAAt(90, 90); c.B < 200 {
			t.Errorf("far corner = %v, want near the end colour", c)
		}
	})
}

// TestRadialGradientDegenerateRadiusDegradesHonestly covers the documented
// degradation: an ending shape with a zero radius (here `closest-side` with the
// centre ON a box corner, where the distance to both nearest sides is 0) can
// establish no geometry. Nothing paints, the background COLOUR still shows, and
// the engine says why rather than failing silently.
func TestRadialGradientDegenerateRadiusDegradesHonestly(t *testing.T) {
	t.Parallel()
	logOpt, logged := recordLogf()
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:100px;height:100px;background-color:red;` +
		`background-image:radial-gradient(circle closest-side at 0 0, black, white)"></div>` +
		`</body></html>`
	doc, err := OpenHTMLBytes([]byte(src), WithPageSize(120, 120), logOpt)
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		MaxWidthPx: 120, MaxHeightPx: 120, Background: color.White})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba := img.(*image.RGBA)

	// The background colour still paints across the box.
	wantPixel(t, rgba, 50, 50, color.RGBA{255, 0, 0, 255}, 3,
		"a degenerate gradient must leave the background colour painting")

	// And the skip is LOGGED, so an author whose gradient vanished can find out why.
	var found bool
	for _, m := range logged() {
		if strings.Contains(m, "gradient") && strings.Contains(m, "degenerate") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("a degenerate gradient was skipped without a log; messages were: %v", logged())
	}
}

// TestGradientRespectsBackgroundSize proves a gradient is a background IMAGE:
// background-size changes the gradient's BOX, so the whole ramp compresses into
// the sized tile rather than spanning the element.
func TestGradientRespectsBackgroundSize(t *testing.T) {
	t.Parallel()
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}

	// Half-width, no repeat: the ramp runs red->blue across the LEFT half only;
	// the right half is unpainted (the white page shows through).
	img := renderGradientBox(t,
		"background:linear-gradient(to right, red, blue);background-size:50% 100%;background-repeat:no-repeat")
	wantPixel(t, img, 1, 50, red, 10, "start of the sized tile")
	wantPixel(t, img, 25, 50, color.RGBA{128, 0, 128, 255}, 5, "middle of the sized tile")
	wantPixel(t, img, 48, 50, blue, 10, "end of the sized tile")
	wantPixel(t, img, 75, 50, color.RGBA{255, 255, 255, 255}, 3, "beyond the tile: unpainted")

	// The same size WITH repeat tiles the compressed ramp twice across the box.
	img = renderGradientBox(t,
		"background:linear-gradient(to right, red, blue);background-size:50% 100%")
	wantPixel(t, img, 1, 50, red, 10, "first tile start")
	wantPixel(t, img, 48, 50, blue, 10, "first tile end")
	wantPixel(t, img, 52, 50, red, 15, "second tile start")
	wantPixel(t, img, 98, 50, blue, 10, "second tile end")
}

// TestGradientRespectsBackgroundClipAndOrigin proves the gradient travels the
// same geometry path as a bitmap background: the origin box sizes it and the
// clip box confines it.
func TestGradientRespectsBackgroundClipAndOrigin(t *testing.T) {
	t.Parallel()
	// A 20px border with background-clip:content-box means nothing paints over
	// the border or padding area.
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:60px;height:60px;padding:20px;border:0;` +
		`background:linear-gradient(to right, red, blue);background-clip:content-box"></div>` +
		`</body></html>`
	doc, err := OpenHTMLBytes([]byte(src), WithPageSize(120, 120))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		MaxWidthPx: 120, MaxHeightPx: 120, Background: color.White})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba := img.(*image.RGBA)

	// Inside the padding area (outside the content box) nothing painted.
	if c := rgba.RGBAAt(5, 50); c.R < 250 || c.G < 250 || c.B < 250 {
		t.Errorf("padding area at (5,50) = %v, want unpainted white (clipped to content-box)", c)
	}
	// Inside the content box the gradient painted.
	if c := rgba.RGBAAt(50, 50); c.R > 250 && c.G > 250 && c.B > 250 {
		t.Errorf("content box at (50,50) = %v, want the gradient", c)
	}
}

// TestGradientTransparentStopHasNoDarkBand is the premultiplied-alpha proof at
// the PIXEL level. Fading red to `transparent` over a white page must stay in the
// red/pink family throughout. Straight-alpha interpolation walks the colour
// toward rgba(0,0,0,0) and produces a visibly GREY midpoint, which is the
// artifact this test exists to catch.
func TestGradientTransparentStopHasNoDarkBand(t *testing.T) {
	t.Parallel()
	img := renderGradientBox(t, "background:linear-gradient(to right, red, transparent)")

	for _, x := range []int{10, 25, 50, 75, 90} {
		c := img.RGBAAt(x, 50)
		// Compositing red at alpha a over white gives (255, 255-255a, 255-255a):
		// the RED channel stays saturated the whole way across.
		if c.R < 250 {
			t.Errorf("x=%d: red channel = %d, want ~255. A red channel that falls "+
				"means the colour is being interpolated toward black — straight-alpha "+
				"interpolation rather than premultiplied (pixel %v)", x, c.R, c)
		}
		// Green and blue must be EQUAL (a pure red-over-white composite has no
		// hue shift) and must increase monotonically toward white.
		if int(c.G)-int(c.B) > 2 || int(c.B)-int(c.G) > 2 {
			t.Errorf("x=%d: pixel %v has a hue shift; a red->transparent fade over white must stay pure", x, c)
		}
	}

	// The fade must actually progress: near the start mostly red, near the end
	// nearly white.
	start := img.RGBAAt(5, 50)
	end := img.RGBAAt(95, 50)
	if start.G > 40 {
		t.Errorf("start = %v, want nearly opaque red", start)
	}
	if end.G < 215 {
		t.Errorf("end = %v, want nearly transparent (white showing through)", end)
	}
}

// TestGradientCarriesNoRasterImage guards the resolution-independence claim: a
// CSS gradient must reach the painter as a gradient, never pre-rasterized into a
// bitmap, so a vector backend can emit a native shading.
func TestGradientCarriesNoRasterImage(t *testing.T) {
	t.Parallel()
	doc, err := OpenHTMLBytes([]byte(
		`<html><body style="margin:0"><div style="width:80px;height:40px;` +
			`background:linear-gradient(to right, red, blue)"></div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	bg := oneBgItem(t, doc)
	if bg.Img != nil {
		t.Error("a CSS gradient carried an image.Image; it must never be rasterized in layout")
	}
	if bg.Scene != nil {
		t.Error("a CSS gradient carried a VectorScene; it is neither a bitmap nor an SVG")
	}
	if bg.Gradient == nil {
		t.Fatal("a CSS gradient carried no Gradient")
	}
	if len(bg.Gradient.Stops) != 2 {
		t.Errorf("stops = %d, want 2", len(bg.Gradient.Stops))
	}
	// A gradient has no intrinsic size, so it takes the origin box's.
	if bg.IntrinsicW != 80 || bg.IntrinsicH != 40 {
		t.Errorf("intrinsic size = %vx%v, want the origin box 80x40", bg.IntrinsicW, bg.IntrinsicH)
	}
}

// TestGradientEmitsNativePDFShading proves the vector backend keeps the gradient
// as a real /Shading dictionary rather than stamping a bitmap — the payoff for
// routing CSS gradients through the same seam SVG paint servers use.
func TestGradientEmitsNativePDFShading(t *testing.T) {
	t.Parallel()
	raw := htmlToPDF(t,
		`<html><body><div style="width:80px;height:40px;background:linear-gradient(to right, red, blue)"></div></body></html>`)
	if hasImageXObject(raw) {
		t.Error("a CSS gradient produced an image XObject: it was rasterized rather than " +
			"emitted as a native PDF shading")
	}
	content := pdfStreams(t, raw)
	if !strings.Contains(content, " sh\n") {
		t.Errorf("content stream has no `sh` operator; the gradient did not emit a shading:\n%s", content)
	}
}

// TestMalformedGradientDegradesToBackgroundColour covers the honest-degradation
// requirement: a gradient this engine cannot parse must leave the background
// COLOUR painting rather than dropping the whole declaration or painting
// something invented.
func TestMalformedGradientDegradesToBackgroundColour(t *testing.T) {
	t.Parallel()
	for _, style := range []string{
		// A colour hint, deliberately unsupported.
		"background-color:red;background-image:linear-gradient(black, 30%, white)",
		// A gradient function this engine does not paint.
		"background-color:red;background-image:conic-gradient(black, white)",
		// A single stop is invalid CSS.
		"background-color:red;background-image:linear-gradient(black)",
		// A unitless angle is not an <angle>.
		"background-color:red;background-image:linear-gradient(45, black, white)",
	} {
		img := renderGradientBox(t, style)
		// The background colour still paints across the whole box.
		wantPixel(t, img, 50, 50, color.RGBA{255, 0, 0, 255}, 3,
			"malformed gradient must degrade to the background colour: "+style)
	}
}
