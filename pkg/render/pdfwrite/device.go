package pdfwrite

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"

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
	images   []pendingImage   // images referenced this page (assembled later)
	shadings []pendingShading // native /Shading dicts referenced this page (assembled later)
	logf     func(string, ...any)

	// extGStates holds every distinct /ExtGState dict this page has emitted,
	// in first-use order; extGStateNames maps a state back to its already
	// assigned resource name so identical states (e.g. a hundred shapes at
	// the same alpha) share ONE resource instead of one each. See emitGState.
	extGStates     []pendingExtGState
	extGStateNames map[extGState]string

	// clipStack tracks each Save()'s clip rectangle so Restore can pop back to it.
	// clipRect is the current clip's device-space bounding box (not the exact
	// path shape — PDF's own W/W* operators still enforce the precise clip at
	// view time). It exists only so FillShading knows how large a region to
	// rasterize; every other device method ignores it. A nil rect means
	// "unclipped" (bounds fall back to the full page box).
	clipRect  *clipBounds
	clipStack []*clipBounds

	shadingLogged bool // true once a fidelity note has been logged for this device
	groupLogged   bool // true once a fidelity note has been logged for group pass-through
}

// clipBounds is an axis-aligned device-space rectangle in PDF points.
type clipBounds struct {
	minX, minY, maxX, maxY float64
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
	g := extGState{hasFillAlpha: true, fillAlpha: float64(paint.Color.A) / 255, blendMode: paint.BlendMode}
	needsScope := g.needed()
	if needsScope {
		d.buf.WriteString("q\n")
		d.emitGState(g)
	}
	d.setFillColor(paint.Color.R, paint.Color.G, paint.Color.B)
	d.writePath(p)
	if paint.Rule == render.EvenOdd {
		d.buf.WriteString("f*\n")
	} else {
		d.buf.WriteString("f\n")
	}
	if needsScope {
		d.buf.WriteString("Q\n")
	}
}

func (d *pageDevice) Stroke(p *render.Path, paint render.StrokePaint) {
	if p == nil || p.Empty() {
		return
	}
	g := extGState{hasStrokeAlpha: true, strokeAlpha: float64(paint.Color.A) / 255, blendMode: paint.BlendMode}
	needsScope := g.needed()
	if needsScope {
		d.buf.WriteString("q\n")
		d.emitGState(g)
	}
	d.setStrokeColor(paint.Color.R, paint.Color.G, paint.Color.B)
	fmt.Fprintf(&d.buf, "%s w\n", formatReal(paint.Width))
	fmt.Fprintf(&d.buf, "%d J\n", capCode(paint.Cap))
	fmt.Fprintf(&d.buf, "%d j\n", joinCode(paint.Join))
	miter := paint.MiterLimit
	if miter < 1 {
		miter = 10 // PDF default miter limit; mirrors pkg/render/raster/stroke.go.
	}
	fmt.Fprintf(&d.buf, "%s M\n", formatReal(miter))
	d.writeDash(paint.DashArray, paint.DashPhase)
	d.writePath(p)
	d.buf.WriteString("S\n")
	if needsScope {
		d.buf.WriteString("Q\n")
	}
}

// capCode maps a render.LineCap to the PDF line-cap style operand (PDF 1.7 §8.4.3.3).
func capCode(c render.LineCap) int {
	switch c {
	case render.RoundCap:
		return 1
	case render.SquareCap:
		return 2
	default:
		return 0 // ButtCap
	}
}

// joinCode maps a render.LineJoin to the PDF line-join style operand (PDF 1.7 §8.4.3.4).
func joinCode(j render.LineJoin) int {
	switch j {
	case render.RoundJoin:
		return 1
	case render.BevelJoin:
		return 2
	default:
		return 0 // MiterJoin
	}
}

// writeDash emits the "d" operator for a dash pattern. A nil/empty array, or one
// whose entries are all non-positive, means a solid line: PDF represents that as an
// empty dash array rather than an array of zeros (which is undefined per spec), so
// either case is normalized to "[] 0 d".
func (d *pageDevice) writeDash(dashes []float64, phase float64) {
	solid := len(dashes) == 0
	if !solid {
		solid = true
		for _, v := range dashes {
			if v > 0 {
				solid = false
				break
			}
		}
	}
	if solid {
		d.buf.WriteString("[] 0 d\n")
		return
	}
	d.buf.WriteString("[")
	for i, v := range dashes {
		if i > 0 {
			d.buf.WriteString(" ")
		}
		d.buf.WriteString(formatReal(v))
	}
	fmt.Fprintf(&d.buf, "] %s d\n", formatReal(phase))
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
	d.Fill(outline, render.FillPaint{Color: colorFromFill(c), BlendMode: blend})
}

func (d *pageDevice) DrawImage(img image.Image, ctm render.Matrix, alpha float64, blend string) {
	if img == nil {
		return
	}
	name := fmt.Sprintf("Im%d", len(d.images))
	d.images = append(d.images, pendingImage{name: name, img: img, ctm: ctm})
	g := extGState{hasFillAlpha: true, fillAlpha: alpha, blendMode: blend}
	d.buf.WriteString("q\n")
	d.emitGState(g)
	m := ctm
	fmt.Fprintf(&d.buf, "%s %s %s %s %s %s cm\n",
		formatReal(m.A), formatReal(m.B), formatReal(m.C), formatReal(m.D), formatReal(m.E), formatReal(m.F))
	fmt.Fprintf(&d.buf, "/%s Do\n", name)
	d.buf.WriteString("Q\n")
}

// FillShading paints shader through the current clip. When shader implements
// render.ShadingDescriber and describes a shading this writer can express
// natively (opaque stops, SpreadPad — see canEmitShading), it emits a real
// PDF /Shading dictionary painted with the `sh` operator: a native, resolution-
// independent vector output. Otherwise it falls back to rasterizing shader into
// an RGBA image XObject (the original behavior), which stays correct for
// alpha and reflect/repeat spreads that PDF's /Shading cannot express natively.
func (d *pageDevice) FillShading(s render.Shader, ctm render.Matrix, blend string) {
	if s == nil {
		return
	}
	if d.tryNativeShading(s, ctm, blend) {
		return
	}
	d.rasterizeShading(s, ctm, blend)
}

// tryNativeShading attempts the vector path: type-assert s for
// render.ShadingDescriber, and if it describes a shading canEmitShading
// accepts, emit a /Shading dictionary and paint it with `sh`. Returns false
// (having emitted nothing) whenever the vector path is not available, so the
// caller falls back to rasterizing; it logs once per page WHY, so a caller
// using WithLogf can see the fidelity decision.
func (d *pageDevice) tryNativeShading(s render.Shader, ctm render.Matrix, blend string) bool {
	describer, ok := s.(render.ShadingDescriber)
	if !ok {
		d.logShadingFallback("shader does not implement ShadingDescriber")
		return false
	}
	desc, ok := describer.DescribeShading()
	if !ok {
		d.logShadingFallback("DescribeShading declined this instance (e.g. a mesh shading)")
		return false
	}
	reason, ok := canEmitShading(desc)
	if !ok {
		d.logShadingFallback(reason)
		return false
	}

	name := fmt.Sprintf("Sh%d", len(d.shadings))
	d.shadings = append(d.shadings, pendingShading{name: name, dict: buildShadingDict(desc)})

	// The shape is already clipped by the time FillShading is called (PushClip
	// emitted W/W* n); paint the shading under that clip with the CTM applied,
	// balancing q/Q around the state change exactly like DrawImage does. Per
	// spec `sh` is not modulated by constant alpha (/ca) — only the blend mode
	// applies (ISO 32000-1 §8.7.4.3), so only /BM is set here.
	g := extGState{blendMode: blend}
	d.buf.WriteString("q\n")
	d.emitGState(g)
	m := ctm
	fmt.Fprintf(&d.buf, "%s %s %s %s %s %s cm\n",
		formatReal(m.A), formatReal(m.B), formatReal(m.C), formatReal(m.D), formatReal(m.E), formatReal(m.F))
	fmt.Fprintf(&d.buf, "/%s sh\n", name)
	d.buf.WriteString("Q\n")
	return true
}

// logShadingFallback logs (once per page) why FillShading fell back to
// rasterizing instead of emitting a native /Shading dictionary. A no-op when
// no logf was configured.
func (d *pageDevice) logShadingFallback(reason string) {
	if d.logf == nil || d.shadingLogged {
		return
	}
	d.shadingLogged = true
	d.logf("pdfwrite: shading rasterized into an image (%s)", reason)
}

// rasterizeShading samples shader into an RGBA image over the current clip's
// bounding box and draws it through DrawImage — the fallback path for a
// shading that tryNativeShading declined (opaque test failed, non-pad spread,
// or a Shader with no description at all). Rasterizing at 1 image pixel per
// PDF point (this device's own unit; pdfwrite carries no other DPI notion)
// keeps the image sized to the shape rather than the whole page, and is sharp
// enough that a gradient does not visibly band once placed back into vector
// page space.
func (d *pageDevice) rasterizeShading(s render.Shader, ctm render.Matrix, blend string) {
	inv, ok := invertMatrix(ctm)
	if !ok {
		return // singular CTM: shading space is degenerate, nothing to sample
	}
	b := d.clipRect
	if b == nil {
		// No active clip: fall back to the page box (the sh operator is normally
		// clipped first, so this is the rare/defensive case, not the common path).
		b = &clipBounds{0, 0, d.wPt, d.hPt}
	}
	b = intersectClipBounds(b, &clipBounds{0, 0, d.wPt, d.hPt})
	minX, minY := int(math.Floor(b.minX)), int(math.Floor(b.minY))
	maxX, maxY := int(math.Ceil(b.maxX)), int(math.Ceil(b.maxY))
	w, h := maxX-minX, maxY-minY
	if w <= 0 || h <= 0 {
		return
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	any := false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Sample at the pixel center in page space, then map back through the
			// inverse CTM into shading space — the same convention the raster
			// backend uses (pkg/render/raster/shading.go).
			px, py := float64(minX+x)+0.5, float64(minY+y)+0.5
			ux, uy := inv.Apply(px, py)
			c, paint := s.ColorAt(ux, uy)
			if !paint {
				continue
			}
			img.SetRGBA(x, y, c)
			any = true
		}
	}
	if !any {
		return
	}
	// DrawImage maps the image's unit square [0,1]x[0,1] into device space; place
	// it exactly over [minX,maxX]x[minY,maxY] in page space.
	place := render.Scale(float64(w), float64(h)).Mul(render.Translate(float64(minX), float64(minY)))
	d.DrawImage(img, place, 1, blend)
}

// invertMatrix returns the inverse of an affine matrix, ok=false if singular.
// pkg/render/raster has its own private copy of this same math; pdfwrite needs
// its own because Matrix carries no Invert method and the two packages do not
// share an internal geometry helper package.
func invertMatrix(m render.Matrix) (render.Matrix, bool) {
	det := m.A*m.D - m.B*m.C
	if det > -1e-12 && det < 1e-12 {
		return render.Matrix{}, false
	}
	id := 1 / det
	return render.Matrix{
		A: m.D * id, B: -m.B * id, C: -m.C * id, D: m.A * id,
		E: (m.C*m.F - m.D*m.E) * id, F: (m.B*m.E - m.A*m.F) * id,
	}, true
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
	// Track the intersected bounding box (not the exact shape) so FillShading
	// knows how large a region to rasterize; the PDF W/W* operators above still
	// enforce the precise clip at view time regardless of this approximation.
	if minX, minY, maxX, maxY, ok := p.Bounds(); ok {
		d.clipRect = intersectClipBounds(d.clipRect, &clipBounds{minX, minY, maxX, maxY})
	}
}

// intersectClipBounds intersects two clip rectangles; a nil operand means
// "unclipped" and is ignored. Returns nil if the result is empty.
func intersectClipBounds(a, b *clipBounds) *clipBounds {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	r := &clipBounds{
		minX: max(a.minX, b.minX),
		minY: max(a.minY, b.minY),
		maxX: min(a.maxX, b.maxX),
		maxY: min(a.maxY, b.maxY),
	}
	if r.minX >= r.maxX || r.minY >= r.maxY {
		return &clipBounds{} // empty
	}
	return r
}

// BeginGroup is a STUB: this writer does not yet emit transparency Form
// XObjects (a later PR — see the SVG groups/clip/mask design), so it treats a
// group as transparent pass-through. Children between BeginGroup and EndGroup
// paint directly onto the page exactly as if the group were absent; nothing
// is dropped, but group opacity/blend/mask has no effect (each child keeps
// painting as its own separate operation, so overlapping children under a
// group's opacity will double-darken at the overlap until real transparency
// groups land). Logs once per device so callers using WithLogf see the
// fidelity gap.
func (d *pageDevice) BeginGroup() {
	if d.logf != nil && !d.groupLogged {
		d.groupLogged = true
		d.logf("pdfwrite: groups not yet composited as PDF transparency groups; painting children directly (opacity/mask ignored)")
	}
}

// EndGroup is the pass-through counterpart to BeginGroup: see its doc for the
// current fidelity limitation. alpha, blendMode, and mask are accepted for
// interface conformance but not yet applied.
func (d *pageDevice) EndGroup(alpha float64, blendMode string, mask render.GroupMask) {}

func (d *pageDevice) Save() {
	d.buf.WriteString("q\n")
	d.clipStack = append(d.clipStack, d.clipRect)
}

func (d *pageDevice) Restore() {
	d.buf.WriteString("Q\n")
	if n := len(d.clipStack); n > 0 {
		d.clipRect = d.clipStack[n-1]
		d.clipStack = d.clipStack[:n-1]
	} else {
		d.clipRect = nil
	}
}

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
