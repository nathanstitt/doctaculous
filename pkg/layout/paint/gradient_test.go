package paint

import (
	"image/color"
	"math"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/layout"
	"github.com/nathanstitt/omnidoc/pkg/render"
)

// evalRamp is a helper: evaluate the ramp at t and return the straight RGBA it
// produces, rounded to 8-bit channels the way the device would see them.
func evalRamp(stops []layout.GradientStop, t float64) color.RGBA {
	r := &gradientRamp{stops: stops}
	out := r.Eval([]float64{t})
	to8 := func(v float64) uint8 { return uint8(math.Round(v * 255)) }
	return color.RGBA{to8(out[0]), to8(out[1]), to8(out[2]), to8(out[3])}
}

func TestGradientRampLinearInterpolation(t *testing.T) {
	black := color.RGBA{0, 0, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	stops := []layout.GradientStop{{Pos: 0, Color: black}, {Pos: 1, Color: white}}

	for _, tt := range []struct {
		t    float64
		want uint8
	}{
		{0, 0}, {0.25, 64}, {0.5, 128}, {0.75, 191}, {1, 255},
	} {
		got := evalRamp(stops, tt.t)
		if got.R != tt.want || got.G != tt.want || got.B != tt.want {
			t.Errorf("t=%v: got %v, want grey %d", tt.t, got, tt.want)
		}
		if got.A != 255 {
			t.Errorf("t=%v: alpha = %d, want 255", tt.t, got.A)
		}
	}
}

func TestGradientRampHoldsEndpoints(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	// Stops that do not span [0,1]: outside them the ramp holds the endpoint
	// colour solid rather than extrapolating past it.
	stops := []layout.GradientStop{{Pos: 0.25, Color: red}, {Pos: 0.75, Color: blue}}

	if got := evalRamp(stops, 0); got != red {
		t.Errorf("below the first stop: got %v, want %v", got, red)
	}
	if got := evalRamp(stops, -5); got != red {
		t.Errorf("far below the first stop: got %v, want %v", got, red)
	}
	if got := evalRamp(stops, 1); got != blue {
		t.Errorf("above the last stop: got %v, want %v", got, blue)
	}
	if got := evalRamp(stops, 5); got != blue {
		t.Errorf("far above the last stop: got %v, want %v", got, blue)
	}
	// The midpoint between them is the halfway colour.
	if got := evalRamp(stops, 0.5); got.R != 128 || got.B != 128 {
		t.Errorf("midpoint: got %v, want roughly (128,0,128)", got)
	}
}

// TestGradientRampHardStop covers two stops at the SAME position: CSS's
// mechanism for a crisp colour break, which must NOT be smoothed over.
func TestGradientRampHardStop(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	stops := []layout.GradientStop{
		{Pos: 0, Color: red},
		{Pos: 0.5, Color: red},
		{Pos: 0.5, Color: blue},
		{Pos: 1, Color: blue},
	}
	if got := evalRamp(stops, 0.25); got != red {
		t.Errorf("before the break: got %v, want pure red", got)
	}
	if got := evalRamp(stops, 0.75); got != blue {
		t.Errorf("after the break: got %v, want pure blue", got)
	}
	// Just either side of the break the colours must still be pure — no blend
	// band at all.
	if got := evalRamp(stops, 0.4999); got != red {
		t.Errorf("just before the break: got %v, want pure red", got)
	}
	if got := evalRamp(stops, 0.5001); got != blue {
		t.Errorf("just after the break: got %v, want pure blue", got)
	}
}

// TestGradientRampPremultipliedAlpha is the reason this ramp exists separately
// from pkg/svg's straight-alpha one.
//
// Interpolating STRAIGHT RGBA from opaque red to `transparent` (which CSS
// defines as rgba(0,0,0,0)) walks the colour channels toward BLACK, so the
// midpoint is a half-transparent dark red — visible as a grey/black band that no
// browser paints. Premultiplied interpolation keeps the colour at red the whole
// way and fades only the alpha.
func TestGradientRampPremultipliedAlpha(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	transparent := color.RGBA{0, 0, 0, 0} // CSS `transparent`
	stops := []layout.GradientStop{{Pos: 0, Color: red}, {Pos: 1, Color: transparent}}

	for _, tt := range []struct {
		t       float64
		wantA   uint8
		wantMin uint8 // the red channel must stay at least this high
	}{
		{0.25, 191, 250},
		{0.5, 128, 250},
		{0.75, 64, 250},
	} {
		got := evalRamp(stops, tt.t)
		if got.A != tt.wantA {
			t.Errorf("t=%v: alpha = %d, want %d", tt.t, got.A, tt.wantA)
		}
		if got.R < tt.wantMin {
			t.Errorf("t=%v: red = %d, want >= %d — straight-alpha interpolation would darken toward black here",
				tt.t, got.R, tt.wantMin)
		}
		if got.G != 0 || got.B != 0 {
			t.Errorf("t=%v: got %v, want a pure red hue throughout", tt.t, got)
		}
	}

	// A fully transparent endpoint has no colour to recover; it must not produce
	// a NaN or an out-of-range channel.
	end := evalRamp(stops, 1)
	if end.A != 0 {
		t.Errorf("endpoint alpha = %d, want 0", end.A)
	}
}

// TestGradientRampPremultipliedBetweenColours checks that premultiplication does
// not change the OPAQUE case, which is the overwhelming majority of gradients
// and must stay byte-identical to a straight lerp.
func TestGradientRampPremultipliedIsIdentityWhenOpaque(t *testing.T) {
	a := color.RGBA{10, 200, 30, 255}
	b := color.RGBA{240, 20, 130, 255}
	stops := []layout.GradientStop{{Pos: 0, Color: a}, {Pos: 1, Color: b}}
	for _, f := range []float64{0.1, 0.25, 0.5, 0.75, 0.9} {
		got := evalRamp(stops, f)
		want := color.RGBA{
			uint8(math.Round(float64(a.R) + f*(float64(b.R)-float64(a.R)))),
			uint8(math.Round(float64(a.G) + f*(float64(b.G)-float64(a.G)))),
			uint8(math.Round(float64(a.B) + f*(float64(b.B)-float64(a.B)))),
			255,
		}
		// Allow one unit of rounding slack from the premultiply round trip.
		if diff(got.R, want.R) > 1 || diff(got.G, want.G) > 1 || diff(got.B, want.B) > 1 || got.A != 255 {
			t.Errorf("f=%v: got %v, want %v (opaque stops must match a straight lerp)", f, got, want)
		}
	}
}

func diff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func TestGradientRampDegenerate(t *testing.T) {
	// Eval must stay total: no panic and no out-of-range index for an empty or
	// single-stop list, even though the painter refuses both before it gets here.
	if got := (&gradientRamp{}).Eval([]float64{0.5}); len(got) != 4 {
		t.Errorf("empty ramp returned %v", got)
	}
	one := []layout.GradientStop{{Pos: 0.5, Color: color.RGBA{1, 2, 3, 255}}}
	for _, tv := range []float64{-1, 0, 0.5, 1, 2} {
		if got := evalRamp(one, tv); got != (color.RGBA{1, 2, 3, 255}) {
			t.Errorf("single-stop ramp at t=%v: got %v", tv, got)
		}
	}
	// A ramp with no input at all must behave as t=0 rather than panicking.
	if got := (&gradientRamp{stops: one}).Eval(nil); len(got) != 4 {
		t.Errorf("nil input returned %v", got)
	}
}

func TestGradientShaderLinear(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	g := &layout.BackgroundGradient{
		Kind: layout.GradientLinear,
		X0:   0, Y0: 10, X1: 100, Y1: 10,
		Stops: []layout.GradientStop{{Pos: 0, Color: red}, {Pos: 1, Color: blue}},
	}
	sh, m, ok := gradientShader(g)
	if !ok {
		t.Fatal("gradientShader declined a valid linear gradient")
	}
	if m != render.Identity {
		t.Errorf("linear local matrix = %+v, want identity", m)
	}
	// Sample along the axis: pure red at the start, pure blue at the end, an
	// even blend between.
	for _, tt := range []struct {
		x            float64
		wantR, wantB uint8
	}{
		{0, 255, 0}, {50, 128, 128}, {100, 0, 255},
	} {
		c, painted := sh.ColorAt(tt.x, 10)
		if !painted {
			t.Errorf("x=%v: not painted", tt.x)
			continue
		}
		if diff(c.R, tt.wantR) > 1 || diff(c.B, tt.wantB) > 1 {
			t.Errorf("x=%v: got %v, want R=%d B=%d", tt.x, c, tt.wantR, tt.wantB)
		}
	}
	// Off the ends the pad spread holds the endpoint colours.
	if c, _ := sh.ColorAt(-50, 10); c != red {
		t.Errorf("before the start: got %v, want %v", c, red)
	}
	if c, _ := sh.ColorAt(150, 10); c != blue {
		t.Errorf("past the end: got %v, want %v", c, blue)
	}
	// Perpendicular to the axis the colour is constant — that is what makes it
	// a LINEAR gradient.
	cA, _ := sh.ColorAt(50, 10)
	cB, _ := sh.ColorAt(50, 999)
	if cA != cB {
		t.Errorf("colour varies perpendicular to the gradient line: %v vs %v", cA, cB)
	}
}

func TestGradientShaderRepeatSpread(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	// A 20-unit line with a hard break at its midpoint, set to repeat: the
	// pattern must recur every 20 units indefinitely.
	g := &layout.BackgroundGradient{
		Kind: layout.GradientLinear,
		X0:   0, Y0: 0, X1: 20, Y1: 0,
		Repeating: true,
		Stops: []layout.GradientStop{
			{Pos: 0, Color: red}, {Pos: 0.5, Color: red},
			{Pos: 0.5, Color: blue}, {Pos: 1, Color: blue},
		},
	}
	sh, _, ok := gradientShader(g)
	if !ok {
		t.Fatal("gradientShader declined a repeating gradient")
	}
	for period := 0; period < 4; period++ {
		base := float64(period * 20)
		if c, _ := sh.ColorAt(base+5, 0); c != red {
			t.Errorf("period %d, +5: got %v, want red", period, c)
		}
		if c, _ := sh.ColorAt(base+15, 0); c != blue {
			t.Errorf("period %d, +15: got %v, want blue", period, c)
		}
	}
}

func TestGradientShaderRadialEllipse(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	// An ellipse twice as wide as it is tall, centred at (50,25).
	g := &layout.BackgroundGradient{
		Kind: layout.GradientRadial,
		CX:   50, CY: 25, RX: 50, RY: 25,
		Stops: []layout.GradientStop{{Pos: 0, Color: red}, {Pos: 1, Color: blue}},
	}
	sh, m, ok := gradientShader(g)
	if !ok {
		t.Fatal("gradientShader declined a valid radial gradient")
	}
	// The shader evaluates a UNIT circle; the matrix carries the ellipse's
	// scale and centre. Compose it the way the painter does before sampling.
	inv, invOK := invertForTest(m)
	if !invOK {
		t.Fatal("the ellipse matrix must be invertible")
	}
	at := func(x, y float64) color.RGBA {
		ux, uy := inv.Apply(x, y)
		c, _ := sh.ColorAt(ux, uy)
		return c
	}
	if c := at(50, 25); c != red {
		t.Errorf("at the centre: got %v, want red", c)
	}
	// Both ends of BOTH axes sit on the ending ellipse, so both are the last
	// stop's colour — that is the property an ellipse has and a circle does not.
	for _, p := range [][2]float64{{0, 25}, {100, 25}, {50, 0}, {50, 50}} {
		if c := at(p[0], p[1]); c != blue {
			t.Errorf("at (%v,%v) on the ending ellipse: got %v, want blue", p[0], p[1], c)
		}
	}
	// Halfway out along each axis gives the same mid colour on both, which a
	// CIRCULAR interpretation of these radii would not.
	cx := at(75, 25)
	cy := at(50, 37.5)
	if diff(cx.R, cy.R) > 2 || diff(cx.B, cy.B) > 2 {
		t.Errorf("halfway along x (%v) and y (%v) differ; the ellipse scale is wrong", cx, cy)
	}
}

func TestGradientShaderDeclinesDegenerate(t *testing.T) {
	stops := []layout.GradientStop{
		{Pos: 0, Color: color.RGBA{255, 0, 0, 255}},
		{Pos: 1, Color: color.RGBA{0, 0, 255, 255}},
	}
	// A zero-length gradient line has no direction to run along.
	if _, _, ok := gradientShader(&layout.BackgroundGradient{
		Kind: layout.GradientLinear, X0: 5, Y0: 5, X1: 5, Y1: 5, Stops: stops,
	}); ok {
		t.Error("a zero-length gradient line must be declined")
	}
	// A zero or negative radius has no ending shape.
	for _, g := range []*layout.BackgroundGradient{
		{Kind: layout.GradientRadial, CX: 5, CY: 5, RX: 0, RY: 10, Stops: stops},
		{Kind: layout.GradientRadial, CX: 5, CY: 5, RX: 10, RY: 0, Stops: stops},
		{Kind: layout.GradientRadial, CX: 5, CY: 5, RX: -1, RY: 10, Stops: stops},
	} {
		if _, _, ok := gradientShader(g); ok {
			t.Errorf("a degenerate radius (%v,%v) must be declined", g.RX, g.RY)
		}
	}
}

// TestGradientShaderDescribable guards the vector-backend path: the shaders this
// package builds must implement render.ShadingDescriber, or pdfwrite silently
// falls back to rasterizing every CSS gradient instead of emitting a native
// /Shading dictionary.
func TestGradientShaderDescribable(t *testing.T) {
	stops := []layout.GradientStop{
		{Pos: 0, Color: color.RGBA{255, 0, 0, 255}},
		{Pos: 1, Color: color.RGBA{0, 0, 255, 255}},
	}
	for name, g := range map[string]*layout.BackgroundGradient{
		"linear": {Kind: layout.GradientLinear, X0: 0, Y0: 0, X1: 10, Y1: 0, Stops: stops},
		"radial": {Kind: layout.GradientRadial, CX: 5, CY: 5, RX: 5, RY: 5, Stops: stops},
	} {
		sh, _, ok := gradientShader(g)
		if !ok {
			t.Fatalf("%s: gradientShader declined", name)
		}
		d, isDescriber := sh.(render.ShadingDescriber)
		if !isDescriber {
			t.Errorf("%s: shader does not implement ShadingDescriber", name)
			continue
		}
		desc, described := d.DescribeShading()
		if !described {
			t.Errorf("%s: DescribeShading declined", name)
			continue
		}
		if len(desc.Stops) != len(stops) {
			t.Errorf("%s: described %d stops, want %d", name, len(desc.Stops), len(stops))
		}
	}
}

func TestReparameterizeLinear(t *testing.T) {
	// Rescaling a repeating gradient's stop range to [0,1] must move the line
	// with it, or the repetitions land in the wrong place.
	g := &layout.BackgroundGradient{Kind: layout.GradientLinear, X0: 0, Y0: 0, X1: 100, Y1: 0}
	g.Reparameterize(0.2, 0.5)
	if g.X0 != 20 || g.X1 != 50 {
		t.Errorf("line = (%v..%v), want (20..50)", g.X0, g.X1)
	}
	// A radial gradient scales its radii instead.
	r := &layout.BackgroundGradient{Kind: layout.GradientRadial, RX: 40, RY: 20}
	r.Reparameterize(0, 0.5)
	if r.RX != 20 || r.RY != 10 {
		t.Errorf("radii = (%v,%v), want (20,10)", r.RX, r.RY)
	}
}

// invertForTest inverts an affine matrix, mirroring what the raster device does
// to map a device point back into shading space.
func invertForTest(m render.Matrix) (render.Matrix, bool) {
	det := m.A*m.D - m.B*m.C
	if det == 0 {
		return render.Matrix{}, false
	}
	return render.Matrix{
		A: m.D / det, B: -m.B / det,
		C: -m.C / det, D: m.A / det,
		E: (m.C*m.F - m.D*m.E) / det,
		F: (m.B*m.E - m.A*m.F) / det,
	}, true
}
