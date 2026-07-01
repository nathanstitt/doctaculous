package pdfwrite

import (
	"bytes"
	"fmt"
	"image"
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// pageDevice implements render.Device by appending PDF content-stream operators to a
// buffer for a single page. It emits RAW page-space coordinates (top-left origin, Y
// down); the document assembler prepends one page-level CTM ("1 0 0 -1 0 H cm") that
// flips the whole page into PDF bottom-left/Y-up space. The device never flips per
// coordinate — one flip strategy, applied once at the page level.
type pageDevice struct {
	buf      bytes.Buffer
	wPt, hPt float64
	embed    *fontEmbedder
	images   []pendingImage // images referenced this page (assembled later)
	logf     func(string, ...any)
}

type pendingImage struct {
	name string
	img  image.Image
	ctm  render.Matrix
}

func newPageDevice(wPt, hPt float64) *pageDevice {
	return &pageDevice{wPt: wPt, hPt: hPt, embed: newFontEmbedder()}
}

// newPageDeviceWithEmbedder builds a page device sharing an existing font embedder,
// so a glyph's emit code is consistent across every page (the codes are assigned once
// in a sequential pre-pass and only read here). The shared embedder must already know
// every glyph this device will draw, so the device performs no new assignment — its
// use() calls hit the already-seen path and are safe to run concurrently.
func newPageDeviceWithEmbedder(wPt, hPt float64, embed *fontEmbedder) *pageDevice {
	return &pageDevice{wPt: wPt, hPt: hPt, embed: embed}
}

func (d *pageDevice) Size() (int, int) { return int(d.wPt), int(d.hPt) }

func (d *pageDevice) Fill(p *render.Path, paint render.FillPaint) {
	if p == nil || p.Empty() {
		return
	}
	d.setFillColor(paint.Color.R, paint.Color.G, paint.Color.B)
	d.writePath(p)
	if paint.Rule == render.EvenOdd {
		d.buf.WriteString("f*\n")
	} else {
		d.buf.WriteString("f\n")
	}
}

func (d *pageDevice) Stroke(p *render.Path, paint render.StrokePaint) {
	if p == nil || p.Empty() {
		return
	}
	d.setStrokeColor(paint.Color.R, paint.Color.G, paint.Color.B)
	fmt.Fprintf(&d.buf, "%s w\n", formatReal(paint.Width))
	d.writePath(p)
	d.buf.WriteString("S\n")
}

func (d *pageDevice) DrawGlyph(g render.GlyphRef) {
	face, ok := g.Face.(*font.Face)
	if !ok || face == nil {
		// Unknown face type: fall back to filling the outline.
		d.fillGlyphOutline(g)
		return
	}
	code, embedded := d.embed.use(face, g.GID, g.Runes)
	if !embedded {
		// Non-embeddable program or Type1 code space exhausted: paint the outline.
		if d.logf != nil {
			d.logf("pdfwrite: glyph %d not embeddable as text; drawing outline", g.GID)
		}
		d.fillGlyphOutline(g)
		return
	}
	name := d.embed.resourceName(face)

	// The text matrix is g.Transform verbatim (raw page space); the page-level CTM
	// (assembler) flips the whole page, so no per-coordinate flip here. Font size is
	// 1 because the matrix's linear part already carries the em scale.
	m := g.Transform
	d.setFillColor(g.Color.R, g.Color.G, g.Color.B)
	d.buf.WriteString("BT\n")
	fmt.Fprintf(&d.buf, "/%s 1 Tf\n", name)
	fmt.Fprintf(&d.buf, "%s %s %s %s %s %s Tm\n",
		formatReal(m.A), formatReal(m.B), formatReal(m.C), formatReal(m.D), formatReal(m.E), formatReal(m.F))
	// A TrueType (Identity-H) code is a 2-byte GID; a simple Type1 code is 1 byte.
	if _, kind := face.ProgramBytes(); kind == font.ProgramKindType1 {
		fmt.Fprintf(&d.buf, "<%02X> Tj\n", code&0xFF)
	} else {
		fmt.Fprintf(&d.buf, "<%04X> Tj\n", code)
	}
	d.buf.WriteString("ET\n")
}

// fillGlyphOutline paints g's outline (fallback when the glyph can't be embedded as
// text). It transforms the em-space outline into page space via g.Transform.
func (d *pageDevice) fillGlyphOutline(g render.GlyphRef) {
	if g.Face == nil {
		return
	}
	o := g.Face.Outline(g.GID)
	if o == nil || o.Empty() {
		return
	}
	d.Fill(render.TransformPath(o, g.Transform), render.FillPaint{
		Color: colorFromFill(g.Color),
	})
}

func (d *pageDevice) FillGlyph(outline *render.Path, c render.FillColor, blend string) {
	d.Fill(outline, render.FillPaint{Color: colorFromFill(c)})
}

func (d *pageDevice) DrawImage(img image.Image, ctm render.Matrix, alpha float64, blend string) {
	if img == nil {
		return
	}
	name := fmt.Sprintf("Im%d", len(d.images))
	d.images = append(d.images, pendingImage{name: name, img: img, ctm: ctm})
	d.buf.WriteString("q\n")
	m := ctm
	fmt.Fprintf(&d.buf, "%s %s %s %s %s %s cm\n",
		formatReal(m.A), formatReal(m.B), formatReal(m.C), formatReal(m.D), formatReal(m.E), formatReal(m.F))
	fmt.Fprintf(&d.buf, "/%s Do\n", name)
	d.buf.WriteString("Q\n")
}

func (d *pageDevice) FillShading(s render.Shader, ctm render.Matrix, blend string) {
	// The HTML/DOCX layout engines do not emit shadings; log and skip.
	if d.logf != nil {
		d.logf("pdfwrite: FillShading not supported; skipped")
	}
}

func (d *pageDevice) PushClip(p *render.Path, rule render.FillRule) {
	if p == nil || p.Empty() {
		return
	}
	d.writePath(p)
	if rule == render.EvenOdd {
		d.buf.WriteString("W* n\n")
	} else {
		d.buf.WriteString("W n\n")
	}
}

func (d *pageDevice) Save()    { d.buf.WriteString("q\n") }
func (d *pageDevice) Restore() { d.buf.WriteString("Q\n") }

// writePath emits path construction operators (m/l/c/h) in raw page-space
// coordinates. The page-level Y-flip CTM (prepended by the assembler) maps these to
// PDF bottom-left space, so this device does NOT flip per coordinate.
func (d *pageDevice) writePath(p *render.Path) {
	for _, s := range p.Segments {
		switch s.Kind {
		case render.MoveTo:
			fmt.Fprintf(&d.buf, "%s %s m\n", formatReal(s.P0.X), formatReal(s.P0.Y))
		case render.LineTo:
			fmt.Fprintf(&d.buf, "%s %s l\n", formatReal(s.P0.X), formatReal(s.P0.Y))
		case render.CubeTo:
			fmt.Fprintf(&d.buf, "%s %s %s %s %s %s c\n",
				formatReal(s.P0.X), formatReal(s.P0.Y),
				formatReal(s.P1.X), formatReal(s.P1.Y),
				formatReal(s.P2.X), formatReal(s.P2.Y))
		case render.Close:
			d.buf.WriteString("h\n")
		}
	}
}

func (d *pageDevice) setFillColor(r, g, b uint8) {
	fmt.Fprintf(&d.buf, "%s %s %s rg\n", formatReal(float64(r)/255), formatReal(float64(g)/255), formatReal(float64(b)/255))
}

func (d *pageDevice) setStrokeColor(r, g, b uint8) {
	fmt.Fprintf(&d.buf, "%s %s %s RG\n", formatReal(float64(r)/255), formatReal(float64(g)/255), formatReal(float64(b)/255))
}

// contentStream returns the raw (uncompressed) page content bytes.
func (d *pageDevice) contentStream() []byte { return d.buf.Bytes() }

// fonts returns the page's font embedder (glyphs recorded for embedding).
func (d *pageDevice) fonts() *fontEmbedder { return d.embed }

// colorFromFill widens a render.FillColor to image/color.RGBA for the fill path.
func colorFromFill(c render.FillColor) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}
