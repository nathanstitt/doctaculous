package content

import (
	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

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

// drawGlyph renders one glyph's outline (if any) through the text rendering
// matrix and CTM, unless the render mode is invisible (3) or clip-only.
func (it *Interpreter) drawGlyph(g Glyph) {
	ts := &it.gs.text
	if g.Outline == nil || ts.renderMode == 3 || ts.renderMode == 7 {
		return
	}
	// Text rendering matrix: scale by fontSize/hScale/rise, then text matrix, CTM.
	trm := render.Matrix{
		A: ts.fontSize * ts.hScale, B: 0,
		C: 0, D: ts.fontSize,
		E: 0, F: ts.rise,
	}.Mul(ts.matrix).Mul(it.gs.ctm)

	out := transformOutline(g.Outline, trm)
	c := it.gs.fill
	it.dev.FillGlyph(out, render.FillColor{R: c.R, G: c.G, B: c.B, A: c.A})
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
