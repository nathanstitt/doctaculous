package omnidoc

import (
	"bytes"
	"context"
	"encoding/xml"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
	"github.com/nathanstitt/omnidoc/testdata/gen"
)

// roundTripBlockedBy reports why a fixture cannot be verified by re-reading the
// emitted SVG, or "" when it can.
//
// These are gaps in this toolkit's SVG *reader*, not defects in the writer's
// output: the emitted markup is valid SVG that a browser renders correctly,
// but pkg/internal/svg does not yet consume the feature, so a comparison would measure
// the reader. They are skipped rather than given a loose tolerance, because a
// widened budget would silently also excuse a real writer regression.
//
// Each entry was confirmed by inspecting the emitted markup and locating the
// corresponding reader gap:
//
//   - <image> is unimplemented in pkg/internal/svg — a documented, pre-existing gap
//     (see FEATURES.md, "Not yet ... <image>"). The writer emits a correct
//     <image> with a base64 PNG; the reader draws nothing.
//   - mix-blend-mode is not honored by pkg/internal/svg, so a blended group composites
//     as source-over on the way back in.
//   - PDF-sourced shadings deliberately do not describe themselves
//     (pkg/internal/raster/shading.go's DescribeShading is gated on alphaFromFn),
//     so they take the writer's sampled-<image> fallback and hit the <image>
//     gap above. Gradients from HTML/SVG input DO stay native — asserted by
//     TestHTMLGradientStaysVector.
func roundTripBlockedBy(name string) string {
	switch {
	case strings.HasPrefix(name, "image-"), name == "inline-image":
		return "pkg/internal/svg does not implement <image> (reader gap, not a writer defect)"
	case strings.HasPrefix(name, "blend-"):
		return "pkg/internal/svg does not honor mix-blend-mode (reader gap)"
	case strings.HasPrefix(name, "shading-"):
		return "PDF shadings are not self-describing upstream, so they emit a sampled <image> the reader cannot draw"
	}
	return ""
}

// writeSVG converts page 0 of doc and returns the markup.
func writeSVG(t *testing.T, doc *Document, opts SVGOptions) string {
	t.Helper()
	opts.BundledFonts = true
	var buf bytes.Buffer
	if err := doc.WriteSVG(context.Background(), &buf, 0, opts); err != nil {
		t.Fatalf("WriteSVG: %v", err)
	}
	return buf.String()
}

// rasterizeSVG parses SVG markup back through the toolkit and rasterizes it,
// which is what makes the round-trip test meaningful: the output is validated
// by the project's own parser, not by string matching.
func rasterizeSVG(t *testing.T, markup string) *image.RGBA {
	t.Helper()
	doc, err := OpenSVGBytes([]byte(markup))
	if err != nil {
		t.Fatalf("reopen emitted SVG: %v\n%s", err, head(markup))
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
		DPI: goldenDPI, BundledFonts: true,
	})
	if err != nil {
		t.Fatalf("rasterize emitted SVG: %v", err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterize returned %T, want *image.RGBA", img)
	}
	return rgba
}

func head(s string) string {
	if len(s) > 800 {
		return s[:800] + "\n..."
	}
	return s
}

// TestSVGOutputRoundTripsThroughOwnParser is the core correctness check for the
// SVG backend, and the one it is uniquely able to make: this toolkit both
// writes AND reads SVG, so the emitted document can be fed back through
// pkg/internal/svg, rasterized, and compared against a direct raster of the same source.
//
// That catches whole classes of defect a string assertion cannot — mis-nested
// groups, a clip applied to the wrong subtree, a transform in the wrong order,
// coordinates off by a scale factor — because a wrong document renders wrong
// even when every attribute it contains is individually plausible.
//
// It sweeps gen.Core, skipping the fixtures whose verdict would be about a
// READER gap rather than about this writer (see roundTripBlockedBy).
func TestSVGOutputRoundTripsThroughOwnParser(t *testing.T) {
	for _, f := range gen.Core {
		t.Run(f.Name, func(t *testing.T) {
			if why := roundTripBlockedBy(f.Name); why != "" {
				t.Skip(why)
			}
			doc, err := OpenBytes(f.Bytes())
			if err != nil {
				t.Fatalf("open fixture: %v", err)
			}
			// A direct raster of the source is the reference.
			wantImg, err := doc.RasterizePage(context.Background(), 0, RasterOptions{
				DPI: goldenDPI, BundledFonts: true,
			})
			if err != nil {
				t.Fatalf("rasterize source: %v", err)
			}
			want, ok := wantImg.(*image.RGBA)
			if !ok {
				t.Fatalf("rasterize returned %T", wantImg)
			}

			// goldenDPI is 72, so one point is one pixel and the SVG's user
			// units line up with the reference raster's pixel grid.
			// The reference raster defaults to an opaque white page, so the
			// SVG needs the same backdrop for the comparison to be about
			// content rather than about the default background.
			markup := writeSVG(t, doc, SVGOptions{Background: color.White})
			got := rasterizeSVG(t, markup)

			if want.Bounds() != got.Bounds() {
				t.Fatalf("size mismatch: svg %v, raster %v", got.Bounds(), want.Bounds())
			}
			if differ, beyond := compareImages(want, got); differ {
				total := want.Bounds().Dx() * want.Bounds().Dy()
				t.Errorf("SVG round-trip differs from direct raster: %d/%d pixels beyond tolerance (budget %d)",
					beyond, total, int(maxDifferingFraction*float64(total)))
			}
		})
	}
}

// TestSVGOutputIsWellFormed guards the invariant every other SVG test depends
// on: the emitted document must parse as XML.
func TestSVGOutputIsWellFormed(t *testing.T) {
	for _, f := range gen.Core {
		t.Run(f.Name, func(t *testing.T) {
			doc, err := OpenBytes(f.Bytes())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			markup := writeSVG(t, doc, SVGOptions{})
			var probe struct {
				XMLName xml.Name
				Inner   []byte `xml:",innerxml"`
			}
			if err := xml.Unmarshal([]byte(markup), &probe); err != nil {
				t.Fatalf("emitted SVG is not well-formed: %v\n%s", err, head(markup))
			}
			if probe.XMLName.Local != "svg" {
				t.Errorf("root element = %q, want svg", probe.XMLName.Local)
			}
		})
	}
}

// TestPDFToSVGIsVectorNotBitmap is the point of the whole backend. A PDF
// converted to SVG must come out as real geometry; wrapping a rasterized page
// in a single <image> would satisfy every other test here while throwing away
// resolution independence, which is the reason to emit SVG at all.
func TestPDFToSVGIsVectorNotBitmap(t *testing.T) {
	doc, err := OpenBytes(gen.VectorPDF())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	markup := writeSVG(t, doc, SVGOptions{})
	if n := strings.Count(markup, "<path"); n == 0 {
		t.Errorf("PDF vector content produced no <path> elements:\n%s", head(markup))
	}
	if strings.Contains(markup, "<image") {
		t.Errorf("PDF vector content fell back to a bitmap:\n%s", head(markup))
	}
}

// Text from a PDF must also be vector geometry rather than a rasterized strip.
func TestPDFTextToSVGEmitsOutlines(t *testing.T) {
	doc, err := OpenBytes(gen.TextPDF())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	markup := writeSVG(t, doc, SVGOptions{})
	if n := strings.Count(markup, "<path"); n == 0 {
		t.Errorf("PDF text produced no glyph outlines:\n%s", head(markup))
	}
	if strings.Contains(markup, "<image") {
		t.Errorf("PDF text fell back to a bitmap:\n%s", head(markup))
	}
}

// Scale sets the document's intrinsic size without changing its content, since
// the output is resolution independent.
func TestSVGScaleChangesIntrinsicSize(t *testing.T) {
	doc, err := OpenBytes(gen.VectorPDF())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	one := writeSVG(t, doc, SVGOptions{Scale: 1})
	two := writeSVG(t, doc, SVGOptions{Scale: 2})
	w1, w2 := attrOf(t, one, "width"), attrOf(t, two, "width")
	if w1 == w2 {
		t.Errorf("scale did not change intrinsic width (%q vs %q)", w1, w2)
	}
}

// attrOf pulls a root-element attribute out of emitted markup.
func attrOf(t *testing.T, markup, name string) string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(markup))
	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("no root element carrying %q", name)
		}
		if se, ok := tok.(xml.StartElement); ok {
			for _, a := range se.Attr {
				if a.Name.Local == name {
					return a.Value
				}
			}
			t.Fatalf("root element has no %q attribute", name)
		}
	}
}

// TestConvertToSVGIsWired checks the generic conversion surface, which is what
// the CLI and library callers actually use.
func TestConvertToSVGIsWired(t *testing.T) {
	if !FormatSVG.ValidOutput() {
		t.Fatal("FormatSVG is not a valid output format")
	}
	if err := CanConvert(FormatPDF, FormatSVG); err != nil {
		t.Fatalf("CanConvert(pdf, svg): %v", err)
	}
	// Same-format conversion stays rejected on the generic path.
	if err := CanConvert(FormatSVG, FormatSVG); err == nil {
		t.Error("svg -> svg should be rejected as a same-format conversion")
	}

	var buf bytes.Buffer
	err := Convert(context.Background(), bytes.NewReader(gen.VectorPDF()), &buf, ConvertOptions{
		To: FormatSVG, BundledFonts: true,
	})
	if err != nil {
		t.Fatalf("Convert to SVG: %v", err)
	}
	if !strings.Contains(buf.String(), "<svg") {
		t.Errorf("Convert produced no SVG root:\n%s", head(buf.String()))
	}
}

// TestHTMLGradientStaysVector covers what the round-trip sweep has to skip for
// PDF shadings. A CSS gradient reaches the Device as a self-describing Shader,
// so it must come out as a real <linearGradient> — rasterizing it would throw
// away the resolution independence that justifies emitting SVG.
func TestHTMLGradientStaysVector(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte(
		`<html><body style="margin:0"><div style="width:100px;height:50px;`+
			`background:linear-gradient(to right,#f00,#00f)"></div></body></html>`),
		WithBundledFonts())
	if err != nil {
		t.Fatalf("open html: %v", err)
	}
	markup := writeSVG(t, doc, SVGOptions{})
	if !strings.Contains(markup, "<linearGradient") {
		t.Errorf("CSS gradient did not emit a native <linearGradient>:\n%s", head(markup))
	}
	if strings.Contains(markup, "<image") {
		t.Errorf("CSS gradient fell back to a sampled bitmap:\n%s", head(markup))
	}
	for _, want := range []string{`stop-color="#ff0000"`, `stop-color="#0000ff"`, `gradientUnits="userSpaceOnUse"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("gradient missing %q:\n%s", want, head(markup))
		}
	}
}

// TestPDFImageEmitsImageElement covers the other half of the skipped sweep: the
// writer's <image> output is asserted structurally, since the reader cannot
// draw it back. A PDF raster image must still reach the document.
func TestPDFImageEmitsImageElement(t *testing.T) {
	doc, err := OpenBytes(gen.ImagePDF())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	markup := writeSVG(t, doc, SVGOptions{})
	if !strings.Contains(markup, "<image") {
		t.Fatalf("PDF image produced no <image> element:\n%s", head(markup))
	}
	if !strings.Contains(markup, "data:image/png;base64,") {
		t.Errorf("<image> carries no embedded data URI:\n%s", head(markup))
	}
	// The unit square must be placed by a transform; without one the image
	// would collapse to a single pixel at the origin.
	if !strings.Contains(markup, "transform=\"matrix(") {
		t.Errorf("<image> has no placement transform:\n%s", head(markup))
	}
}

// TestWriteSVGRecoversFromPaintPanic pins the "one bad page can't kill a batch"
// rule for the SVG path: a fault must not escape as a panic, since callers run
// this in loops and worker goroutines where an unrecovered panic is fatal.
//
// Unlike raster.RenderPage, the panic surfaces as an ERROR rather than as
// success-with-partial-output: a partially painted bitmap is still useful and
// visibly incomplete, but a truncated vector document looks exactly like a
// correct sparse one, so silently returning nil would hide the defect.
func TestWriteSVGRecoversFromPaintPanic(t *testing.T) {
	doc := &Document{r: panicRenderer{}, format: FormatPDF}
	var logged int
	var buf bytes.Buffer
	err := doc.WriteSVG(context.Background(), &buf, 0, SVGOptions{
		Logf: func(string, ...any) { logged++ },
	})
	if err == nil {
		t.Fatal("a recovered panic must be reported, not swallowed as success")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("error should name the panic, got: %v", err)
	}
	if logged == 0 {
		t.Error("recovered panic was not logged")
	}
}

// The recovery must not swallow ordinary errors either.
func TestWriteSVGPropagatesRealErrors(t *testing.T) {
	doc, err := OpenBytes(gen.TextPDF())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var buf bytes.Buffer
	if err := doc.WriteSVG(context.Background(), &buf, 99, SVGOptions{BundledFonts: true}); err == nil {
		t.Error("out-of-range page should error, matching the raster path")
	}
}

// panicRenderer is a renderer whose paint pass faults, standing in for a defect
// or a malformed construct that slips a bounds check.
type panicRenderer struct{}

func (panicRenderer) pageCount() int { return 1 }
func (panicRenderer) pageSize(int) (float64, float64, error) {
	return 100, 100, nil
}
func (panicRenderer) renderPage(context.Context, int, RasterOptions) (image.Image, error) {
	return nil, nil
}
func (panicRenderer) paintVector(context.Context, int, render.Device, float64, RasterOptions) error {
	panic("simulated paint fault")
}

// TestCanvasBackgroundPropagatesToSVG covers the browser background-propagation
// rule: a background on <html>/<body> paints the whole canvas, and the raster
// path already gives it precedence over the caller's option. Vector output has
// to agree, or a page that rasterizes green converts to SVG transparent.
func TestCanvasBackgroundPropagatesToSVG(t *testing.T) {
	doc, err := OpenHTMLBytes(
		[]byte(`<html><body style="background:#00cc44">hi</body></html>`), WithBundledFonts())
	if err != nil {
		t.Fatalf("open html: %v", err)
	}
	markup := writeSVG(t, doc, SVGOptions{})
	if !strings.Contains(markup, `fill="#00cc44"`) {
		t.Errorf("CSS canvas background did not reach the SVG:\n%s", head(markup))
	}
}

// With no CSS background the page stays transparent, which is the right
// default for a vector document composited over an unknown backdrop — and
// deliberately differs from rasterization's opaque-white fallback.
func TestNoCanvasBackgroundStaysTransparent(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte(`<html><body>hi</body></html>`), WithBundledFonts())
	if err != nil {
		t.Fatalf("open html: %v", err)
	}
	if markup := writeSVG(t, doc, SVGOptions{}); strings.Contains(markup, "<rect width=") {
		t.Errorf("page without a CSS background should emit no backdrop:\n%s", head(markup))
	}
}

// HTML input exercises the reflow path into the same backend.
func TestHTMLToSVG(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte(
		`<html><body style="margin:0"><div style="width:50px;height:20px;background:#c00"></div>Hello</body></html>`),
		WithBundledFonts())
	if err != nil {
		t.Fatalf("open html: %v", err)
	}
	markup := writeSVG(t, doc, SVGOptions{})
	if !strings.Contains(markup, "<path") {
		t.Errorf("HTML produced no vector content:\n%s", head(markup))
	}
	if err := xml.Unmarshal([]byte(markup), new(struct {
		XMLName xml.Name
		Inner   []byte `xml:",innerxml"`
	})); err != nil {
		t.Fatalf("HTML->SVG is not well-formed: %v", err)
	}
}
