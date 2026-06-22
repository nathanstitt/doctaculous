package content

import (
	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// num returns operands[i] as a float64, or 0 if missing/non-numeric.
func num(operands []pdf.Object, i int) float64 {
	if i < 0 || i >= len(operands) {
		return 0
	}
	f, _ := pdf.Number(operands[i])
	return f
}

// intnum returns operands[i] as an int, or 0 if missing/non-numeric.
func intnum(operands []pdf.Object, i int) int {
	return int(num(operands, i))
}

// nums returns the last n numeric operands as a slice of length n. Using the
// trailing operands tolerates leading junk and matches how color operators read
// their arguments.
func nums(operands []pdf.Object, n int) []float64 {
	out := make([]float64, n)
	start := len(operands) - n
	for i := range n {
		idx := start + i
		if idx >= 0 && idx < len(operands) {
			f, _ := pdf.Number(operands[idx])
			out[i] = f
		}
	}
	return out
}

// numericOperands returns all numeric operands in order (ignoring trailing names
// such as the pattern name in scn).
func numericOperands(operands []pdf.Object) []float64 {
	var out []float64
	for _, o := range operands {
		if f, ok := pdf.Number(o); ok {
			out = append(out, f)
		}
	}
	return out
}

// strOperand returns the last string operand, or nil.
func strOperand(operands []pdf.Object) []byte {
	for i := len(operands) - 1; i >= 0; i-- {
		if s, ok := operands[i].(pdf.String); ok {
			return []byte(s)
		}
	}
	return nil
}

// nameOperand returns the first name operand, or "".
func nameOperand(operands []pdf.Object) string {
	for _, o := range operands {
		if n, ok := o.(pdf.Name); ok {
			return string(n)
		}
	}
	return ""
}

// matrixFromOperands reads six numeric operands as an affine matrix [a b c d e f].
func matrixFromOperands(operands []pdf.Object) (render.Matrix, bool) {
	if len(operands) < 6 {
		return render.Matrix{}, false
	}
	v := nums(operands, 6)
	return render.Matrix{A: v[0], B: v[1], C: v[2], D: v[3], E: v[4], F: v[5]}, true
}
