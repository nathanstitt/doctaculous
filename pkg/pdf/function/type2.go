package function

import (
	"fmt"
	"math"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// expFunc is a Type 2 (exponential interpolation) function. It has a single
// input x and computes, per output component j:
//
//	out[j] = C0[j] + x^N * (C1[j] - C0[j])
//
// where x is first clamped to /Domain.
type expFunc struct {
	domain []float64 // [x0 x1]
	rng    []float64 // optional /Range (2n values), may be nil
	c0, c1 []float64 // n values each
	n      float64   // exponent
}

func parseExponential(doc *pdf.Document, dict pdf.Dict) (Func, error) {
	domain, err := requireFloatArray(doc, dict, "Domain")
	if err != nil {
		return nil, err
	}
	if len(domain) < 2 {
		return nil, fmt.Errorf("function: Type 2 /Domain needs 2 values, got %d", len(domain))
	}

	n, ok := pdf.Number(resolve(doc, dict["N"]))
	if !ok {
		return nil, fmt.Errorf("function: Type 2 missing or invalid /N")
	}

	// /C0 defaults to [0.0], /C1 to [1.0]; they must have equal length.
	c0 := floatArray(doc, dict["C0"])
	if c0 == nil {
		c0 = []float64{0.0}
	}
	c1 := floatArray(doc, dict["C1"])
	if c1 == nil {
		c1 = []float64{1.0}
	}
	if len(c0) != len(c1) {
		return nil, fmt.Errorf("function: Type 2 /C0 (%d) and /C1 (%d) length mismatch", len(c0), len(c1))
	}

	return &expFunc{
		domain: domain[:2],
		rng:    floatArray(doc, dict["Range"]),
		c0:     c0,
		c1:     c1,
		n:      n,
	}, nil
}

func (f *expFunc) Eval(in []float64) []float64 {
	x := f.domain[0]
	if len(in) > 0 {
		x = in[0]
	}
	x = clamp(x, f.domain[0], f.domain[1])

	xn := powN(x, f.n)
	out := make([]float64, len(f.c0))
	for j := range f.c0 {
		out[j] = f.c0[j] + xn*(f.c1[j]-f.c0[j])
	}
	clampToRange(out, f.rng)
	return out
}

func (f *expFunc) NumOutputs() int { return len(f.c0) }

// powN computes x^n guarding against NaN. For the common N==1 it is exact; for
// x==0 it yields 0 (n>0) or follows math.Pow otherwise.
func powN(x, n float64) float64 {
	if n == 1 {
		return x
	}
	v := math.Pow(x, n)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}
