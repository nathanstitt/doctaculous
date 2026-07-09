package content

import (
	"fmt"
	"math"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// hypot is math.Hypot, aliased for brevity in the glyph-placement math.
func hypot(x, y float64) float64 { return math.Hypot(x, y) }

// fontIdentity returns an opaque per-font key used to group glyphs into runs. The
// GlyphSource pointer is stable for the life of a page's interpretation (fonts are
// resolved once and cached by the resource provider), so its address distinguishes
// fonts without the interpreter needing any font-format knowledge.
func fontIdentity(src GlyphSource) string { return fmt.Sprintf("%p", src) }

// setFont handles "Tf": /Name size.
func (it *Interpreter) setFont(operands []pdf.Object) {
	name := nameOperand(operands)
	it.gs.text.fontSize = num(operands, len(operands)-1)
	if name == "" || it.res == nil {
		it.gs.text.font = nil
		return
	}
	src := it.res.Font(name)
	if src == nil {
		it.logf("content: font %q not found", name)
		it.gs.text.font = nil
		return
	}
	it.gs.text.font = &loadedFont{src: src}
}

// textMove handles Td/TD: translate the line matrix and reset the text matrix.
func (it *Interpreter) textMove(tx, ty float64) {
	it.gs.text.lineMatrix = render.Translate(tx, ty).Mul(it.gs.text.lineMatrix)
	it.gs.text.matrix = it.gs.text.lineMatrix
}

// textNextLine handles T*: move down by the leading.
func (it *Interpreter) textNextLine() {
	it.textMove(0, -it.gs.text.leading)
}

// showText handles Tj: draw a single show-string and advance the text matrix.
func (it *Interpreter) showText(s []byte) {
	if s == nil {
		return
	}
	it.drawGlyphs(s)
}

// showTextArray handles TJ: an array of strings and numeric adjustments. A number
// adjusts the cursor by -n/1000 * fontSize * hScale (in text space).
func (it *Interpreter) showTextArray(operands []pdf.Object) {
	arr, ok := lastArray(operands)
	if !ok {
		return
	}
	for _, e := range arr {
		switch v := e.(type) {
		case pdf.String:
			it.drawGlyphs([]byte(v))
		case pdf.Integer, pdf.Real:
			adj, _ := pdf.Number(v)
			tx := -adj / 1000 * it.gs.text.fontSize * it.gs.text.hScale
			it.advanceText(tx)
		}
	}
}

// drawGlyphs decodes a show-string into glyphs, draws each, and advances the
// text matrix per the PDF text-space advance formula.
func (it *Interpreter) drawGlyphs(s []byte) {
	ts := &it.gs.text
	if ts.font == nil {
		// No font: still advance by an approximate width so layout doesn't collapse.
		it.advanceText(float64(len(s)) * 0.5 * ts.fontSize * ts.hScale)
		return
	}
	glyphs := ts.font.src.DecodeString(s)
	for _, g := range glyphs {
		it.drawGlyph(g)
		// Advance: (w0*fontSize + charSpacing + wordSpacing?) * hScale.
		w0 := g.Width * ts.fontSize
		adv := (w0 + ts.charSpacing)
		if g.IsSpace {
			adv += ts.wordSpacing
		}
		it.advanceText(adv * ts.hScale)
	}
}

// renderingMatrix is the text-rendering matrix in force for the next glyph:
// glyph space scaled by font size / horizontal scale / rise, through the text
// matrix, through the CTM. drawGlyph (painting) and emitTextGlyph (extraction
// capture) MUST use the same TRM or captured positions desync from paint.
func (it *Interpreter) renderingMatrix() render.Matrix {
	ts := &it.gs.text
	return render.Matrix{
		A: ts.fontSize * ts.hScale, B: 0,
		C: 0, D: ts.fontSize,
		E: 0, F: ts.rise,
	}.Mul(ts.matrix).Mul(it.gs.ctm)
}

// drawGlyph renders one glyph's outline (if any) through the text rendering matrix
// and CTM, honoring the PDF text render mode (Tr): 0 fill, 1 stroke, 2 fill+stroke,
// 3 invisible, 4 fill+clip, 5 stroke+clip, 6 fill+stroke+clip, 7 clip. The paint
// component is applied exactly — a stroke mode strokes the glyph outline with the
// stroke color/line params instead of filling it, and mode 2/6 do both. The CLIP
// component of modes 4–7 is a documented deferral: the glyph outlines are not
// accumulated into the text clip applied at ET (so modes 4–6 still paint, and mode 7
// paints nothing — both never crash). Mode 3 (and the no-outline case) paint nothing.
func (it *Interpreter) drawGlyph(g Glyph) {
	ts := &it.gs.text
	// Report the glyph to a text-extraction sink before the paint-mode gates. This is
	// intentionally emitted even for render mode 3 (invisible text) and glyphs with no
	// outline: an invisible OCR text layer over a scanned image is exactly the text an
	// extractor wants, and a missing-outline glyph still carries a rune and advance.
	// The sink never affects painting (the gates below are unchanged).
	if it.textSink != nil {
		it.emitTextGlyph(g)
	}
	if g.Outline == nil || ts.renderMode == 3 {
		return
	}
	fill := textModeFills(ts.renderMode)
	stroke := textModeStrokes(ts.renderMode)
	if !fill && !stroke {
		return // mode 7 (clip-only): no paint in this slice
	}
	// Text rendering matrix: scale by fontSize/hScale/rise, then text matrix, CTM.
	trm := it.renderingMatrix()

	out := transformOutline(g.Outline, trm)
	if fill {
		c := withAlpha(it.gs.fill, it.gs.fillAlpha)
		it.dev.FillGlyph(out, render.FillColor{R: c.R, G: c.G, B: c.B, A: c.A}, it.gs.blendMode)
	}
	if stroke {
		w := it.gs.lineWidth * it.gs.ctm.ScaleFactor()
		if w <= 0 {
			w = 1
		}
		it.dev.Stroke(out, render.StrokePaint{
			Color:      withAlpha(it.gs.stroke, it.gs.strokeAlpha),
			BlendMode:  it.gs.blendMode,
			Width:      w,
			Cap:        it.gs.lineCap,
			Join:       it.gs.lineJoin,
			MiterLimit: it.gs.miterLimit,
			DashArray:  it.scaledDash(),
			DashPhase:  it.gs.dashPhase * it.gs.ctm.ScaleFactor(),
		})
	}
}

// emitTextGlyph reports glyph g to the text sink in device space. It derives the glyph
// origin, effective font size, and advance from renderingMatrix, the same TRM drawGlyph
// paints with, so a captured glyph sits exactly where it is painted. The origin is the
// pen position (0,0 in text space) mapped through the TRM; the effective size and
// advance are the lengths of the size and advance vectors mapped through the TRM's
// linear part, so page scaling and non-uniform CTMs are honored.
func (it *Interpreter) emitTextGlyph(g Glyph) {
	ts := &it.gs.text
	trm := it.renderingMatrix()
	ox, oy := trm.Apply(0, 0)
	// Effective size = |TRM · (0,1)| measured from the origin (the y basis length);
	// advance = |TRM · (advEm, 0)| (the x basis scaled by the em advance).
	sx, sy := trm.Apply(0, 1)
	size := hypot(sx-ox, sy-oy)
	ax, ay := trm.Apply(g.Width, 0)
	adv := hypot(ax-ox, ay-oy)
	var fontID string
	if ts.font != nil {
		fontID = fontIdentity(ts.font.src)
	}
	it.textSink(TextGlyph{
		Rune:    g.Rune,
		X:       ox,
		Y:       oy,
		SizePt:  size,
		Advance: adv,
		IsSpace: g.IsSpace,
		FontID:  fontID,
	})
}

// textModeFills reports whether a text render mode paints a fill (modes 0, 2, 4, 6).
func textModeFills(mode int) bool {
	switch mode {
	case 0, 2, 4, 6:
		return true
	}
	return false
}

// textModeStrokes reports whether a text render mode paints a stroke (modes 1, 2, 5, 6).
func textModeStrokes(mode int) bool {
	switch mode {
	case 1, 2, 5, 6:
		return true
	}
	return false
}

// advanceText translates the text matrix by tx text-space units along the
// current text direction.
func (it *Interpreter) advanceText(tx float64) {
	it.gs.text.matrix = render.Translate(tx, 0).Mul(it.gs.text.matrix)
}

// transformOutline maps a glyph outline (em units, y up) through m into device
// space.
func transformOutline(p *render.Path, m render.Matrix) *render.Path {
	out := &render.Path{Segments: make([]render.Segment, 0, len(p.Segments))}
	for _, seg := range p.Segments {
		switch seg.Kind {
		case render.MoveTo:
			x, y := m.Apply(seg.P0.X, seg.P0.Y)
			out.MoveTo(x, y)
		case render.LineTo:
			x, y := m.Apply(seg.P0.X, seg.P0.Y)
			out.LineTo(x, y)
		case render.CubeTo:
			x0, y0 := m.Apply(seg.P0.X, seg.P0.Y)
			x1, y1 := m.Apply(seg.P1.X, seg.P1.Y)
			x2, y2 := m.Apply(seg.P2.X, seg.P2.Y)
			out.CubeTo(x0, y0, x1, y1, x2, y2)
		case render.Close:
			out.Close()
		}
	}
	return out
}

func lastArray(operands []pdf.Object) (pdf.Array, bool) {
	for i := len(operands) - 1; i >= 0; i-- {
		if a, ok := operands[i].(pdf.Array); ok {
			return a, true
		}
	}
	return nil, false
}
