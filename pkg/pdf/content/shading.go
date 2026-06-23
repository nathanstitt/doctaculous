package content

import "github.com/nathanstitt/doctaculous/pkg/pdf"

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
