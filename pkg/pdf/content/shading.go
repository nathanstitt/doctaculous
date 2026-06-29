package content

import "github.com/nathanstitt/doctaculous/pkg/pdf"

// setFillColorN handles "sc"/"scn": set the fill color (and, under the /Pattern
// color space, a shading-pattern fill source). For a pattern name operand it
// resolves the pattern via the backend; a shading pattern (PatternType 2) sets
// gs.fillShading so a later fill paints the shading clipped to the path. Any
// other case sets a solid color and clears a previously set shading source.
func (it *Interpreter) setFillColorN(operands []pdf.Object) {
	if it.gs.fillCS == csPattern {
		name := nameOperand(operands)
		if name != "" && it.res != nil {
			if shader, m, ok := it.res.Pattern(name); ok {
				// Pattern space is relative to the page default coordinate system, so
				// the shader's device transform is patternMatrix × base, captured now.
				it.gs.fillShading = &shadingSource{shader: shader, ctm: m.Mul(it.base)}
				return
			}
			it.logf("content: pattern %q unsupported or not found", name)
		}
		// Unresolved pattern: drop any prior shading source; leave fill as is.
		it.gs.fillShading = nil
		return
	}
	// Ordinary color: clear any shading source and set the solid color (mapping a
	// Separation/DeviceN tint through its transform when one is set).
	it.gs.fillShading = nil
	it.gs.fill = it.resolveColorN(it.gs.fillCS, it.gs.fillTint, numericOperands(operands))
}

// paintShading handles the "sh" operator: it paints the named shading across the
// current clip region. The shading's parameters are evaluated in the current user
// space, so the interpreter hands the backend-built shader and the current CTM to
// the device, which maps each device pixel back into shading space.
//
// Per the spec the `sh` operator is not modulated by the ExtGState constant alpha
// (/ca); only the blend mode applies. Resolution failures (missing or unsupported
// shading) are logged and skipped, never fatal.
func (it *Interpreter) paintShading(operands []pdf.Object) {
	name := nameOperand(operands)
	if name == "" || it.res == nil {
		return
	}
	shader, ok := it.res.Shading(name)
	if !ok {
		it.logf("content: shading %q unsupported or not found", name)
		return
	}
	it.dev.FillShading(shader, it.gs.ctm, it.gs.blendMode)
}
