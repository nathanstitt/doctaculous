package svgwrite

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/font"
	"github.com/nathanstitt/omnidoc/pkg/render"
)

// render.Device is the seam this package exists to implement; a missing method
// should fail here rather than at a distant call site.
var _ render.Device = (*Device)(nil)

// emit runs paint against a fresh device and returns the serialized document.
func emit(t *testing.T, w, h int, opts Options, paint func(d *Device)) string {
	t.Helper()
	d := New(w, h)
	paint(d)
	var buf bytes.Buffer
	if err := d.WriteTo(&buf, opts); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return buf.String()
}

// squarePath is a simple closed rectangle used across the tests.
func squarePath(x, y, w, h float64) *render.Path {
	p := &render.Path{}
	p.MoveTo(x, y)
	p.LineTo(x+w, y)
	p.LineTo(x+w, y+h)
	p.LineTo(x, y+h)
	p.Close()
	return p
}

// TestOutputIsWellFormedXML is the baseline guarantee: whatever this writer
// emits must parse. A malformed document is worthless no matter how correct
// its geometry, and unbalanced group/clip nesting is the easiest way to
// produce one.
func TestOutputIsWellFormedXML(t *testing.T) {
	got := emit(t, 100, 100, Options{Title: `Fish & <chips>`}, func(d *Device) {
		d.Save()
		d.PushClip(squarePath(0, 0, 50, 50), render.NonZero)
		d.BeginGroup()
		d.Fill(squarePath(5, 5, 10, 10), render.FillPaint{Color: color.RGBA{R: 255, A: 255}})
		d.EndGroup(0.5, "Multiply", nil, nil)
		d.Restore()
	})
	if err := xml.Unmarshal([]byte(got), new(struct {
		XMLName xml.Name
		Inner   []byte `xml:",innerxml"`
	})); err != nil {
		t.Fatalf("emitted document is not well-formed XML: %v\n%s", err, got)
	}
	// The title carries characters that MUST be escaped to stay well-formed.
	if !strings.Contains(got, "Fish &amp; &lt;chips&gt;") {
		t.Errorf("title not XML-escaped:\n%s", got)
	}
}

// TestUnbalancedStateStillProducesValidMarkup covers the Device contract's
// forgiving cases: a stray Restore/EndGroup must not panic, and state left
// open at the end must still close.
func TestUnbalancedStateStillProducesValidMarkup(t *testing.T) {
	got := emit(t, 20, 20, Options{}, func(d *Device) {
		d.Restore()                 // no matching Save
		d.EndGroup(1, "", nil, nil) // no matching BeginGroup
		d.Save()                    // deliberately never restored
		d.PushClip(squarePath(0, 0, 5, 5), render.NonZero)
		d.BeginGroup() // deliberately never ended
		d.Fill(squarePath(1, 1, 2, 2), render.FillPaint{Color: color.RGBA{A: 255}})
	})
	if err := xml.Unmarshal([]byte(got), new(struct {
		XMLName xml.Name
		Inner   []byte `xml:",innerxml"`
	})); err != nil {
		t.Fatalf("unbalanced state produced malformed XML: %v\n%s", err, got)
	}
	// The content painted inside the never-closed group must survive.
	if !strings.Contains(got, "<path") {
		t.Errorf("content inside unclosed group was dropped:\n%s", got)
	}
}

// TestGroupClosesElementsItsChildrenLeftOpen is a regression test for a bug
// that emitted malformed XML: EndGroup value-copied its groupFrame, so the
// closing tags popElem wrote went to the live scratch builder while the body
// was read from a stale snapshot taken before the loop ran.
//
// The case that triggers it — a clip pushed inside a group and left for the
// group to close — is routine in PDF content streams, but the two existing
// well-formedness tests both missed it because they leave elements open only
// at DOCUMENT level, where finish() closes them separately.
func TestGroupClosesElementsItsChildrenLeftOpen(t *testing.T) {
	got := emit(t, 100, 100, Options{}, func(d *Device) {
		d.BeginGroup()
		d.PushClip(squarePath(0, 0, 50, 50), render.NonZero)
		d.Fill(squarePath(5, 5, 10, 10), render.FillPaint{Color: color.RGBA{R: 255, A: 255}})
		d.EndGroup(1, "", nil, nil) // must close the clip's <g> AND its own
	})
	if err := xml.Unmarshal([]byte(got), new(struct {
		XMLName xml.Name
		Inner   []byte `xml:",innerxml"`
	})); err != nil {
		t.Fatalf("clip left open inside a group produced malformed XML: %v\n%s", err, got)
	}
	if open, closed := strings.Count(got, "<g"), strings.Count(got, "</g>"); open != closed {
		t.Errorf("unbalanced groups: %d opened, %d closed:\n%s", open, closed, got)
	}
}

// The same hazard, nested two deep and with a Save in the mix.
func TestNestedGroupsStayBalanced(t *testing.T) {
	got := emit(t, 100, 100, Options{}, func(d *Device) {
		d.BeginGroup()
		d.Save()
		d.PushClip(squarePath(0, 0, 80, 80), render.NonZero)
		d.BeginGroup()
		d.PushClip(squarePath(10, 10, 30, 30), render.EvenOdd)
		d.Fill(squarePath(12, 12, 5, 5), render.FillPaint{Color: color.RGBA{B: 255, A: 255}})
		d.EndGroup(0.5, "", nil, nil)
		d.EndGroup(1, "", nil, nil)
	})
	if err := xml.Unmarshal([]byte(got), new(struct {
		XMLName xml.Name
		Inner   []byte `xml:",innerxml"`
	})); err != nil {
		t.Fatalf("nested groups produced malformed XML: %v\n%s", err, got)
	}
	if open, closed := strings.Count(got, "<g"), strings.Count(got, "</g>"); open != closed {
		t.Errorf("unbalanced groups: %d opened, %d closed:\n%s", open, closed, got)
	}
}

func TestFillEmitsPathWithPaintAttrs(t *testing.T) {
	got := emit(t, 50, 50, Options{}, func(d *Device) {
		d.Fill(squarePath(1, 2, 3, 4), render.FillPaint{
			Color: color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 128},
			Rule:  render.EvenOdd,
		})
	})
	for _, want := range []string{
		`d="M 1 2 L 4 2 L 4 6 L 1 6 Z"`,
		`fill="#112233"`,
		`fill-rule="evenodd"`,
		`fill-opacity=`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// A nonzero fill rule is SVG's initial value; emitting it on every path would
// be pure noise.
func TestNonZeroFillRuleIsOmitted(t *testing.T) {
	got := emit(t, 10, 10, Options{}, func(d *Device) {
		d.Fill(squarePath(0, 0, 1, 1), render.FillPaint{Color: color.RGBA{A: 255}})
	})
	if strings.Contains(got, "fill-rule") {
		t.Errorf("nonzero rule should not emit fill-rule:\n%s", got)
	}
	if strings.Contains(got, "fill-opacity") {
		t.Errorf("opaque fill should not emit fill-opacity:\n%s", got)
	}
}

func TestStrokeEmitsLineStyleAttrs(t *testing.T) {
	got := emit(t, 50, 50, Options{}, func(d *Device) {
		p := &render.Path{}
		p.MoveTo(0, 0)
		p.LineTo(10, 0)
		d.Stroke(p, render.StrokePaint{
			Color: color.RGBA{B: 255, A: 255}, Width: 3,
			Cap: render.RoundCap, Join: render.BevelJoin,
			MiterLimit: 8, DashArray: []float64{2, 1}, DashPhase: 1,
		})
	})
	for _, want := range []string{
		`fill="none"`, `stroke="#0000ff"`, `stroke-width="3"`,
		`stroke-linecap="round"`, `stroke-linejoin="bevel"`,
		`stroke-dasharray="2,1"`, `stroke-dashoffset="1"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// miter-limit applies only to miter joins; this stroke bevels.
	if strings.Contains(got, "stroke-miterlimit") {
		t.Errorf("miterlimit emitted for a bevel join:\n%s", got)
	}
}

// An all-zero dash array means "solid" upstream but makes some viewers render
// nothing, so it must be dropped rather than passed through.
func TestZeroDashArrayIsDropped(t *testing.T) {
	got := emit(t, 10, 10, Options{}, func(d *Device) {
		p := &render.Path{}
		p.MoveTo(0, 0)
		p.LineTo(5, 0)
		d.Stroke(p, render.StrokePaint{Color: color.RGBA{A: 255}, Width: 1, DashArray: []float64{0, 0}})
	})
	if strings.Contains(got, "stroke-dasharray") {
		t.Errorf("zero-sum dash array should be dropped:\n%s", got)
	}
}

// A NaN coordinate would make a viewer discard the whole path, so it is
// clamped at the single formatting choke point.
func TestNonFiniteCoordinatesAreNeutralized(t *testing.T) {
	got := emit(t, 10, 10, Options{}, func(d *Device) {
		p := &render.Path{}
		p.MoveTo(math.Inf(1), math.Inf(-1))
		p.LineTo(math.NaN(), 1)
		d.Fill(p, render.FillPaint{Color: color.RGBA{A: 255}})
	})
	for _, bad := range []string{"NaN", "Inf", "+Inf", "-Inf", "e+"} {
		if strings.Contains(got, bad) {
			t.Errorf("emitted non-finite token %q:\n%s", bad, got)
		}
	}
}

func TestPushClipEmitsClipPathAndNests(t *testing.T) {
	got := emit(t, 60, 60, Options{}, func(d *Device) {
		d.Save()
		d.PushClip(squarePath(0, 0, 30, 30), render.EvenOdd)
		d.Fill(squarePath(5, 5, 5, 5), render.FillPaint{Color: color.RGBA{A: 255}})
		d.Restore()
		// After Restore the clip is gone, so this fill must sit outside the <g>.
		d.Fill(squarePath(40, 40, 5, 5), render.FillPaint{Color: color.RGBA{A: 255}})
	})
	if !strings.Contains(got, "<clipPath") || !strings.Contains(got, `clip-rule="evenodd"`) {
		t.Errorf("clipPath not emitted with its rule:\n%s", got)
	}
	if !strings.Contains(got, "clip-path=\"url(#") {
		t.Errorf("clip reference not emitted:\n%s", got)
	}
	if strings.Count(got, "</g>") != 1 {
		t.Errorf("expected exactly one closed group:\n%s", got)
	}
}

// An empty clipPath clips to NOTHING. Passing paint through would be the
// opposite of correct, so it must still open a hiding group.
func TestEmptyClipHidesContent(t *testing.T) {
	got := emit(t, 10, 10, Options{}, func(d *Device) {
		d.PushClip(&render.Path{}, render.NonZero)
		d.Fill(squarePath(0, 0, 5, 5), render.FillPaint{Color: color.RGBA{A: 255}})
	})
	if !strings.Contains(got, "empty-clip") {
		t.Errorf("empty clip did not restrict content:\n%s", got)
	}
}

func TestEndGroupCarriesOpacityAndBlend(t *testing.T) {
	got := emit(t, 40, 40, Options{}, func(d *Device) {
		d.BeginGroup()
		d.Fill(squarePath(0, 0, 10, 10), render.FillPaint{Color: color.RGBA{G: 255, A: 255}})
		d.EndGroup(0.25, "Multiply", nil, nil)
	})
	if !strings.Contains(got, `opacity="0.25"`) {
		t.Errorf("group opacity missing:\n%s", got)
	}
	if !strings.Contains(got, "mix-blend-mode:multiply") {
		t.Errorf("group blend mode missing:\n%s", got)
	}
}

// A group that painted nothing should not leave an empty <g> behind.
func TestEmptyGroupEmitsNothing(t *testing.T) {
	got := emit(t, 10, 10, Options{}, func(d *Device) {
		d.BeginGroup()
		d.EndGroup(0.5, "", nil, nil)
	})
	if strings.Contains(got, "<g") {
		t.Errorf("empty group should emit no element:\n%s", got)
	}
}

func TestEndGroupEmitsMaskForBothMaskKinds(t *testing.T) {
	mask := image.NewAlpha(image.Rect(0, 0, 4, 4))
	for i := range mask.Pix {
		mask.Pix[i] = 128
	}
	got := emit(t, 20, 20, Options{}, func(d *Device) {
		d.BeginGroup()
		d.Fill(squarePath(0, 0, 4, 4), render.FillPaint{Color: color.RGBA{A: 255}})
		d.EndGroup(1, "", mask, mask)
	})
	// clipMask and softMask are independent restrictions and both apply, so
	// two distinct <mask> definitions must be referenced.
	if n := strings.Count(got, "<mask "); n != 2 {
		t.Errorf("expected 2 mask defs for clip+soft mask, got %d:\n%s", n, got)
	}
	if n := strings.Count(got, "mask=\"url(#"); n != 2 {
		t.Errorf("expected 2 mask references, got %d:\n%s", n, got)
	}
}

// TestMaskUsesAlphaTypeNotLuminance pins the encoding that keeps mask coverage
// exact. A GroupMask is ALREADY final coverage (BuildLuminanceMask reduced the
// content via sRGB Rec. 709), so writing it as gray under the default
// luminance mask-type would make the viewer convert it a second time — in
// linearRGB per SVG 1.1 — turning coverage 128 into 55, an error of 73/255.
// mask-type="alpha" takes the channel verbatim, with no conversion anywhere.
func TestMaskUsesAlphaTypeNotLuminance(t *testing.T) {
	mask := image.NewAlpha(image.Rect(0, 0, 4, 4))
	for i := range mask.Pix {
		mask.Pix[i] = 128 // the worst case for a double conversion
	}
	got := emit(t, 20, 20, Options{}, func(d *Device) {
		d.BeginGroup()
		d.Fill(squarePath(0, 0, 4, 4), render.FillPaint{Color: color.RGBA{A: 255}})
		d.EndGroup(1, "", nil, mask)
	})
	if !strings.Contains(got, `mask-type="alpha"`) {
		t.Errorf("mask must declare mask-type=\"alpha\"; luminance would re-convert coverage:\n%s", got)
	}
}

// The coverage has to live in the alpha channel to match mask-type="alpha",
// with opaque white RGB so a viewer that ignores mask-type still reads full
// coverage rather than none.
func TestAlphaToImageEncodesCoverageInAlpha(t *testing.T) {
	m := image.NewAlpha(image.Rect(0, 0, 2, 2))
	m.SetAlpha(0, 0, color.Alpha{A: 128})
	m.SetAlpha(1, 0, color.Alpha{A: 0})
	img := alphaToImage(m)
	if img == nil {
		t.Fatal("alphaToImage returned nil for a valid mask")
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if a>>8 != 128 {
		t.Errorf("coverage not in alpha: got a=%d, want 128", a>>8)
	}
	// Premultiplied by alpha=128, so white RGB reads back as ~128 each.
	if r != a || g != a || b != a {
		t.Errorf("RGB should be opaque white (premultiplied to alpha): got %d,%d,%d a=%d", r, g, b, a)
	}
	if _, _, _, a0 := img.At(1, 0).RGBA(); a0 != 0 {
		t.Errorf("zero coverage should be fully transparent, got a=%d", a0)
	}
}

func TestDrawImageEmitsDataURI(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	got := emit(t, 30, 30, Options{}, func(d *Device) {
		d.DrawImage(img, render.Scale(10, 10).Mul(render.Translate(5, 5)), 0.5, "")
	})
	for _, want := range []string{
		`<image`, `data:image/png;base64,`, `preserveAspectRatio="none"`, `opacity="0.5"`,
		// The placement carries an extra Y flip on top of the caller's CTM:
		// PDF image space puts the top row at v=1, an SVG <image> at y=0, so
		// scale(10,10)+translate(5,5) must come out mirrored and re-offset.
		// Without it every image renders upside down — a bug the round-trip
		// test caught and this assertion pins down.
		`transform="matrix(10 0 0 -10 5 15)"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestGlyphOutlinesAreDefinedOnce pins the <defs>/<use> hoisting. Glyph
// outlines dominate a text page's byte count, and a repeated letter must not
// re-emit its curves — writing them inline makes the file grow with character
// count rather than with alphabet size.
func TestGlyphOutlinesAreDefinedOnce(t *testing.T) {
	face, ok := font.LoadStandard("Helvetica", font.Style{})
	if !ok {
		t.Skip("bundled Helvetica substitute unavailable")
	}
	gid, ok := face.GID('m')
	if !ok {
		t.Skip("no glyph for 'm'")
	}
	const n = 25
	got := emit(t, 400, 100, Options{}, func(d *Device) {
		for i := range n {
			d.DrawGlyph(render.GlyphRef{
				Face: face, GID: gid, Runes: []rune{'m'},
				Transform: render.Scale(12, -12).Mul(render.Translate(float64(i)*14, 50)),
				Color:     render.FillColor{A: 255},
			})
		}
	})
	if c := strings.Count(got, "<use "); c != n {
		t.Errorf("expected %d <use> references, got %d", n, c)
	}
	// One definition serves all occurrences, at any position or size.
	if c := strings.Count(got, `<path id=`); c != 1 {
		t.Errorf("expected 1 hoisted outline definition, got %d:\n%s", c, got)
	}
}

// A glyph's color must reach the reference, since the hoisted definition is
// shared by every occurrence and so cannot carry one.
func TestGlyphUseCarriesItsOwnFill(t *testing.T) {
	face, ok := font.LoadStandard("Helvetica", font.Style{})
	if !ok {
		t.Skip("bundled Helvetica substitute unavailable")
	}
	gid, _ := face.GID('A')
	got := emit(t, 100, 100, Options{}, func(d *Device) {
		for _, c := range []render.FillColor{{R: 255, A: 255}, {B: 255, A: 255}} {
			d.DrawGlyph(render.GlyphRef{
				Face: face, GID: gid, Transform: render.Scale(12, -12).Mul(render.Translate(10, 50)),
				Color: c,
			})
		}
	})
	if !strings.Contains(got, `fill="#ff0000"`) || !strings.Contains(got, `fill="#0000ff"`) {
		t.Errorf("shared glyph definition did not take per-use colors:\n%s", got)
	}
	if strings.Contains(got, `<path id=`) && strings.Contains(got, `<path id="g1" d="" `) {
		t.Errorf("hoisted definition should carry geometry only:\n%s", got)
	}
}

func TestDrawGlyphEmitsOutlinePath(t *testing.T) {
	face, ok := font.LoadStandard("Helvetica", font.Style{})
	if !ok {
		t.Skip("bundled Helvetica substitute unavailable")
	}
	gid, ok := face.GID('A')
	if !ok {
		t.Skip("no glyph for 'A'")
	}
	got := emit(t, 100, 100, Options{}, func(d *Device) {
		d.DrawGlyph(render.GlyphRef{
			Face: face, GID: gid, Runes: []rune{'A'},
			// Em space is Y-up, device space Y-down, hence the negative scale.
			Transform: render.Scale(20, -20).Mul(render.Translate(10, 50)),
			Color:     render.FillColor{A: 255},
		})
	})
	if !strings.Contains(got, "<path") {
		t.Fatalf("glyph produced no path:\n%s", got)
	}
	// A glyph must never emit <text>: the bundled faces cannot be embedded via
	// @font-face, so <text> would render with an arbitrary substitute.
	if strings.Contains(got, "<text") {
		t.Errorf("glyph emitted <text> rather than an outline:\n%s", got)
	}
}

// TestGlyphCarriesSourceCharacters covers the only thing keeping outline text
// machine-readable: the glyph's source runes. Without it the document is a
// picture of words that no screen reader or scraper can recover.
func TestGlyphCarriesSourceCharacters(t *testing.T) {
	face, ok := font.LoadStandard("Helvetica", font.Style{})
	if !ok {
		t.Skip("bundled Helvetica substitute unavailable")
	}
	gid, _ := face.GID('A')
	got := emit(t, 100, 100, Options{}, func(d *Device) {
		d.DrawGlyph(render.GlyphRef{
			Face: face, GID: gid, Runes: []rune{'<'}, // must be escaped
			Transform: render.Scale(20, -20).Mul(render.Translate(10, 50)),
			Color:     render.FillColor{A: 255},
		})
	})
	if !strings.Contains(got, `aria-label="&lt;"`) {
		t.Errorf("glyph did not carry escaped source characters:\n%s", got)
	}
	// A <title> child would turn every glyph into a hover tooltip.
	if strings.Contains(got, "<title>") {
		t.Errorf("glyph should not emit a <title> child:\n%s", got)
	}
}

// A glyph with no outline (whitespace, or a face missing the GID) must emit
// nothing rather than an empty path element.
func TestDrawGlyphWithNoOutlineEmitsNothing(t *testing.T) {
	got := emit(t, 10, 10, Options{}, func(d *Device) {
		d.DrawGlyph(render.GlyphRef{Face: emptyFace{}, GID: 1, Transform: render.Scale(10, -10)})
	})
	if strings.Contains(got, "<path") {
		t.Errorf("outline-less glyph emitted a path:\n%s", got)
	}
}

type emptyFace struct{}

func (emptyFace) Outline(uint16) *render.Path { return nil }

func TestBackgroundAndSizing(t *testing.T) {
	got := emit(t, 612, 792, Options{Background: color.White}, func(d *Device) {})
	for _, want := range []string{
		`width="612" height="792"`, `viewBox="0 0 612 792"`, `fill="#ffffff"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// Transparent is the default: an SVG composited over an unknown backdrop must
// not carry an assumed white rectangle.
func TestNoBackgroundByDefault(t *testing.T) {
	got := emit(t, 10, 10, Options{}, func(d *Device) {})
	if strings.Contains(got, "<rect") {
		t.Errorf("default output should have no background rect:\n%s", got)
	}
}

func TestSizeReportsDimensions(t *testing.T) {
	d := New(320, 240)
	if w, h := d.Size(); w != 320 || h != 240 {
		t.Errorf("Size() = %d,%d want 320,240", w, h)
	}
}
