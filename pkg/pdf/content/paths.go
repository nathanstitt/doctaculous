package content

import (
	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// The path operators build it.path in device space by transforming each user-
// space coordinate through the current CTM. Curve control points are transformed
// likewise. We track the current point in user space to implement v/y curves.

func (it *Interpreter) moveTo(x, y float64) {
	it.curUserX, it.curUserY = x, y
	dx, dy := it.gs.ctm.Apply(x, y)
	it.path.MoveTo(dx, dy)
}

func (it *Interpreter) lineTo(x, y float64) {
	it.curUserX, it.curUserY = x, y
	dx, dy := it.gs.ctm.Apply(x, y)
	it.path.LineTo(dx, dy)
}

// curveTo handles "c": three control points (x1 y1 x2 y2 x3 y3) in user space.
func (it *Interpreter) curveTo(operands []pdf.Object, base int) {
	x1, y1 := num(operands, base+0), num(operands, base+1)
	x2, y2 := num(operands, base+2), num(operands, base+3)
	x3, y3 := num(operands, base+4), num(operands, base+5)
	it.appendCubic(x1, y1, x2, y2, x3, y3)
}

// curveV handles "v": first control point is the current point.
func (it *Interpreter) curveV(operands []pdf.Object) {
	x2, y2 := num(operands, 0), num(operands, 1)
	x3, y3 := num(operands, 2), num(operands, 3)
	it.appendCubic(it.curUserX, it.curUserY, x2, y2, x3, y3)
}

// curveY handles "y": second control point equals the endpoint.
func (it *Interpreter) curveY(operands []pdf.Object) {
	x1, y1 := num(operands, 0), num(operands, 1)
	x3, y3 := num(operands, 2), num(operands, 3)
	it.appendCubic(x1, y1, x3, y3, x3, y3)
}

func (it *Interpreter) appendCubic(x1, y1, x2, y2, x3, y3 float64) {
	d1x, d1y := it.gs.ctm.Apply(x1, y1)
	d2x, d2y := it.gs.ctm.Apply(x2, y2)
	d3x, d3y := it.gs.ctm.Apply(x3, y3)
	it.path.CubeTo(d1x, d1y, d2x, d2y, d3x, d3y)
	it.curUserX, it.curUserY = x3, y3
}

// rect handles "re": x y w h. It adds a closed rectangle subpath.
func (it *Interpreter) rect(operands []pdf.Object) {
	x, y := num(operands, 0), num(operands, 1)
	w, h := num(operands, 2), num(operands, 3)
	it.moveTo(x, y)
	it.lineTo(x+w, y)
	it.lineTo(x+w, y+h)
	it.lineTo(x, y+h)
	it.path.Close()
	it.curUserX, it.curUserY = x, y
}

func (it *Interpreter) fillPath(rule render.FillRule) {
	if it.path.Empty() {
		return
	}
	it.dev.Fill(&it.path, render.FillPaint{Color: withAlpha(it.gs.fill, it.gs.fillAlpha), Rule: rule})
}

func (it *Interpreter) strokePath() {
	if it.path.Empty() {
		return
	}
	// Convert the line width from user space to device space using the CTM scale.
	w := it.gs.lineWidth * it.gs.ctm.ScaleFactor()
	if w <= 0 {
		w = 1 // a zero-width line is the thinnest renderable line (1 device px)
	}
	dash := it.scaledDash()
	it.dev.Stroke(&it.path, render.StrokePaint{
		Color:      withAlpha(it.gs.stroke, it.gs.strokeAlpha),
		Width:      w,
		Cap:        it.gs.lineCap,
		Join:       it.gs.lineJoin,
		MiterLimit: it.gs.miterLimit,
		DashArray:  dash,
		DashPhase:  it.gs.dashPhase * it.gs.ctm.ScaleFactor(),
	})
}

// endPath finalizes the current path: it applies any pending W/W* clip, then
// clears the path for the next subpath.
func (it *Interpreter) endPath() {
	if it.pending.active && !it.path.Empty() {
		it.dev.PushClip(&it.path, it.pending.rule)
	}
	it.pending = pendingClip{}
	it.path.Reset()
}

func (it *Interpreter) scaledDash() []float64 {
	if len(it.gs.dashArray) == 0 {
		return nil
	}
	s := it.gs.ctm.ScaleFactor()
	out := make([]float64, len(it.gs.dashArray))
	for i, v := range it.gs.dashArray {
		out[i] = v * s
	}
	return out
}

func (it *Interpreter) setDash(operands []pdf.Object) {
	// operands: [array] phase
	if len(operands) < 2 {
		it.gs.dashArray = nil
		it.gs.dashPhase = 0
		return
	}
	arr, _ := operands[0].(pdf.Array)
	var dashes []float64
	for _, e := range arr {
		if f, ok := pdf.Number(e); ok {
			dashes = append(dashes, f)
		}
	}
	it.gs.dashArray = dashes
	it.gs.dashPhase = num(operands, 1)
}
