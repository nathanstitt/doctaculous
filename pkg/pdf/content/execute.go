package content

import (
	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// execute dispatches a single operator with its operands.
func (it *Interpreter) execute(op string, operands []pdf.Object, depth int) {
	switch op {
	// --- graphics state ---
	case "q":
		it.stack = append(it.stack, it.gs.clone())
		it.dev.Save()
	case "Q":
		if n := len(it.stack); n > 0 {
			it.gs = it.stack[n-1]
			it.stack = it.stack[:n-1]
			it.dev.Restore()
		}
	case "cm":
		if m, ok := matrixFromOperands(operands); ok {
			it.gs.ctm = m.Mul(it.gs.ctm)
		}
	case "w":
		it.gs.lineWidth = num(operands, 0)
	case "J":
		it.gs.lineCap = render.LineCap(intnum(operands, 0))
	case "j":
		it.gs.lineJoin = render.LineJoin(intnum(operands, 0))
	case "M":
		it.gs.miterLimit = num(operands, 0)
	case "d":
		it.setDash(operands)
	case "gs":
		it.applyExtGState(operands)

	// --- path construction ---
	case "m":
		it.moveTo(num(operands, 0), num(operands, 1))
	case "l":
		it.lineTo(num(operands, 0), num(operands, 1))
	case "c":
		it.curveTo(operands, 0)
	case "v":
		it.curveV(operands)
	case "y":
		it.curveY(operands)
	case "re":
		it.rect(operands)
	case "h":
		it.path.Close()

	// --- path painting ---
	case "S":
		it.strokePath()
		it.endPath()
	case "s":
		it.path.Close()
		it.strokePath()
		it.endPath()
	case "f", "F":
		it.fillPath(render.NonZero)
		it.endPath()
	case "f*":
		it.fillPath(render.EvenOdd)
		it.endPath()
	case "B", "B*":
		it.fillPath(ruleFor(op))
		it.strokePath()
		it.endPath()
	case "b", "b*":
		it.path.Close()
		it.fillPath(ruleFor(op))
		it.strokePath()
		it.endPath()
	case "n":
		it.endPath()
	case "W":
		it.pending = pendingClip{active: true, rule: render.NonZero}
	case "W*":
		it.pending = pendingClip{active: true, rule: render.EvenOdd}

	// --- color ---
	case "g":
		it.gs.fillCS = deviceGray
		it.gs.fill = colorFromComponents(deviceGray, nums(operands, 1))
	case "G":
		it.gs.strokeCS = deviceGray
		it.gs.stroke = colorFromComponents(deviceGray, nums(operands, 1))
	case "rg":
		it.gs.fillCS = deviceRGB
		it.gs.fill = colorFromComponents(deviceRGB, nums(operands, 3))
	case "RG":
		it.gs.strokeCS = deviceRGB
		it.gs.stroke = colorFromComponents(deviceRGB, nums(operands, 3))
	case "k":
		it.gs.fillCS = deviceCMYK
		it.gs.fill = colorFromComponents(deviceCMYK, nums(operands, 4))
	case "K":
		it.gs.strokeCS = deviceCMYK
		it.gs.stroke = colorFromComponents(deviceCMYK, nums(operands, 4))
	case "cs":
		it.gs.fillCS = it.colorSpaceByName(operands)
	case "CS":
		it.gs.strokeCS = it.colorSpaceByName(operands)
	case "sc", "scn":
		it.gs.fill = colorFromComponents(it.gs.fillCS, numericOperands(operands))
	case "SC", "SCN":
		it.gs.stroke = colorFromComponents(it.gs.strokeCS, numericOperands(operands))

	// --- text ---
	case "BT":
		it.gs.text.matrix = render.Identity
		it.gs.text.lineMatrix = render.Identity
	case "ET":
		// nothing to flush in v1
	case "Tc":
		it.gs.text.charSpacing = num(operands, 0)
	case "Tw":
		it.gs.text.wordSpacing = num(operands, 0)
	case "Tz":
		it.gs.text.hScale = num(operands, 0) / 100
	case "TL":
		it.gs.text.leading = num(operands, 0)
	case "Ts":
		it.gs.text.rise = num(operands, 0)
	case "Tr":
		it.gs.text.renderMode = intnum(operands, 0)
	case "Tf":
		it.setFont(operands)
	case "Td":
		it.textMove(num(operands, 0), num(operands, 1))
	case "TD":
		it.gs.text.leading = -num(operands, 1)
		it.textMove(num(operands, 0), num(operands, 1))
	case "Tm":
		if m, ok := matrixFromOperands(operands); ok {
			it.gs.text.matrix = m
			it.gs.text.lineMatrix = m
		}
	case "T*":
		it.textNextLine()
	case "Tj":
		it.showText(strOperand(operands))
	case "'":
		it.textNextLine()
		it.showText(strOperand(operands))
	case "\"":
		// aw ac string "  — strOperand scans for the string operand regardless of
		// position, so no slicing (which would panic on underflow) is needed.
		it.gs.text.wordSpacing = num(operands, 0)
		it.gs.text.charSpacing = num(operands, 1)
		it.textNextLine()
		it.showText(strOperand(operands))
	case "TJ":
		it.showTextArray(operands)

	// --- XObjects ---
	case "Do":
		it.doXObject(operands, depth)

	// Inline images (BI...ID...EI) are handled in the run loop, which consumes
	// the image body directly from the scanner; they never reach execute.

	default:
		it.logf("content: unsupported operator %q", op)
	}

	// Apply a pending clip after any path-painting operator concluded the path.
	// endPath handles consuming it.
}

func ruleFor(op string) render.FillRule {
	if len(op) > 0 && op[len(op)-1] == '*' {
		return render.EvenOdd
	}
	return render.NonZero
}
