package function

import (
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// stitchFunc is a Type 3 (stitching) function. It partitions its 1-input
// /Domain into k subdomains using /Bounds (k-1 increasing values) and dispatches
// to one of k subfunctions. The input is linearly mapped from its subdomain onto
// the corresponding /Encode pair before the subfunction is evaluated.
type stitchFunc struct {
	domain    []float64 // [x0 x1]
	rng       []float64 // optional /Range, may be nil
	functions []Func    // k subfunctions, each 1-input
	bounds    []float64 // k-1 values
	encode    []float64 // 2k values
}

func parseStitching(doc *pdf.Document, dict pdf.Dict, depth int) (Func, error) {
	domain, err := requireFloatArray(doc, dict, "Domain")
	if err != nil {
		return nil, err
	}
	if len(domain) < 2 {
		return nil, fmt.Errorf("function: Type 3 /Domain needs 2 values, got %d", len(domain))
	}

	fnArr := getArray(doc, dict["Functions"])
	if len(fnArr) == 0 {
		return nil, fmt.Errorf("function: Type 3 missing /Functions")
	}
	k := len(fnArr)
	funcs := make([]Func, k)
	for i, fo := range fnArr {
		// depth+1 guards against a /Functions entry that references this stitching function
		// (directly or via a cycle), which would otherwise recurse until the stack overflows.
		sub, perr := parseDepth(doc, fo, depth+1)
		if perr != nil {
			return nil, fmt.Errorf("function: Type 3 subfunction %d: %w", i, perr)
		}
		funcs[i] = sub
	}

	bounds, err := requireFloatArray(doc, dict, "Bounds")
	if err != nil {
		return nil, err
	}
	if len(bounds) != k-1 {
		return nil, fmt.Errorf("function: Type 3 /Bounds has %d values, want k-1=%d", len(bounds), k-1)
	}

	encode, err := requireFloatArray(doc, dict, "Encode")
	if err != nil {
		return nil, err
	}
	if len(encode) != 2*k {
		return nil, fmt.Errorf("function: Type 3 /Encode has %d values, want 2k=%d", len(encode), 2*k)
	}

	return &stitchFunc{
		domain:    domain[:2],
		rng:       floatArray(doc, dict["Range"]),
		functions: funcs,
		bounds:    bounds,
		encode:    encode,
	}, nil
}

func (f *stitchFunc) Eval(in []float64) []float64 {
	x := f.domain[0]
	if len(in) > 0 {
		x = in[0]
	}
	x = clamp(x, f.domain[0], f.domain[1])

	// Find subdomain i: the smallest index with x < Bounds[i]; the last one if
	// none. Subdomain i spans [lo, hi) where lo/hi come from Domain[0], Bounds…,
	// Domain[1].
	k := len(f.functions)
	i := 0
	for i < len(f.bounds) && x >= f.bounds[i] {
		i++
	}
	if i >= k {
		i = k - 1
	}

	lo := f.domain[0]
	if i > 0 {
		lo = f.bounds[i-1]
	}
	hi := f.domain[1]
	if i < len(f.bounds) {
		hi = f.bounds[i]
	}

	// Map x from [lo,hi) onto the encode interval for this subfunction.
	e := interp(x, lo, hi, f.encode[2*i], f.encode[2*i+1])

	out := f.functions[i].Eval([]float64{e})
	clampToRange(out, f.rng)
	return out
}

func (f *stitchFunc) NumOutputs() int {
	if len(f.rng) > 0 {
		return len(f.rng) / 2
	}
	if len(f.functions) > 0 {
		return f.functions[0].NumOutputs()
	}
	return -1
}
