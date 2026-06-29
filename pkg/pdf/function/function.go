// Package function implements the PDF Function objects of ISO 32000-1 §7.10.
//
// A PDF function maps m input values (bounded by the function's /Domain) to n
// output values (bounded, where applicable, by its /Range). Functions are used
// by shadings and other constructs to map a parametric value to a color or
// other quantity. This package implements the four standard function types:
//
//   - Type 0: Sampled functions (a stream of multidimensional samples).
//   - Type 2: Exponential interpolation functions.
//   - Type 3: Stitching functions (a sequence of subfunctions).
//   - Type 4: PostScript calculator functions.
//
// It also accepts an array of n single-output functions, combining them into a
// single n-output Func (a common shading idiom: /Function [f0 f1 f2]).
//
// The package never panics on malformed input: Parse and the evaluators return
// errors so callers can degrade gracefully.
package function

import (
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// Func evaluates a PDF function: it maps m input values to n output values.
//
// Implementations clamp inputs to the function's /Domain and clamp outputs to
// its /Range (where the type defines a range). Eval must not panic; on a
// malformed evaluation it returns a best-effort result of the correct length
// (typically zeros).
type Func interface {
	// Eval maps inputs (length m) to outputs (length n). Inputs are clamped to
	// the function's domain. The returned slice has length NumOutputs() when that
	// is known.
	Eval(in []float64) []float64
	// NumOutputs reports n, the number of output components, or -1 if unknown.
	NumOutputs() int
}

// Parse builds a Func from a /Function object. The object may be:
//
//   - a function dictionary or stream (FunctionType 0, 2, 3, or 4), or
//   - an array of single-output functions, combined into one n-output Func.
//
// Indirect references are resolved through doc. A nil doc is tolerated for
// fully direct objects. Malformed or unsupported inputs return an error.
func Parse(doc *pdf.Document, obj pdf.Object) (Func, error) {
	return parseDepth(doc, obj, 0)
}

// maxFunctionDepth bounds the nesting of a Type 3 (stitching) or array function whose
// subfunctions are themselves functions. It guards against a self- or mutually-referential
// /Functions array (reachable via an indirect reference) that would otherwise recurse
// until the goroutine stack overflows — a crafted-PDF crash. A real document nests at most
// a handful of levels.
const maxFunctionDepth = 32

// parseDepth is Parse with a recursion-depth guard threaded through the two nesting forms
// (Type 3 subfunctions and function arrays).
func parseDepth(doc *pdf.Document, obj pdf.Object, depth int) (Func, error) {
	if depth > maxFunctionDepth {
		return nil, fmt.Errorf("function: nesting too deep (>%d); cyclic /Functions reference?", maxFunctionDepth)
	}
	resolved := resolve(doc, obj)
	if resolved == nil {
		return nil, fmt.Errorf("function: nil function object")
	}

	// An array of functions: each element is a 1-output function; the combined
	// function outputs one component per element.
	if arr, ok := resolved.(pdf.Array); ok {
		return parseArray(doc, arr, depth)
	}

	dict := getDict(doc, resolved)
	if dict == nil {
		return nil, fmt.Errorf("function: object is not a dict, stream, or array (%T)", resolved)
	}

	ft, ok := getInt(doc, dict["FunctionType"])
	if !ok {
		return nil, fmt.Errorf("function: missing or invalid /FunctionType")
	}

	switch ft {
	case 0:
		return parseSampled(doc, resolved)
	case 2:
		return parseExponential(doc, dict)
	case 3:
		return parseStitching(doc, dict, depth)
	case 4:
		return parsePostScript(doc, resolved)
	default:
		return nil, fmt.Errorf("function: unsupported /FunctionType %d", ft)
	}
}

// arrayFunc combines n single-output functions into one n-output function. Each
// member receives the same input vector and contributes its first output.
type arrayFunc struct {
	members []Func
}

func parseArray(doc *pdf.Document, arr pdf.Array, depth int) (Func, error) {
	if len(arr) == 0 {
		return nil, fmt.Errorf("function: empty function array")
	}
	members := make([]Func, len(arr))
	for i, e := range arr {
		f, err := parseDepth(doc, e, depth+1)
		if err != nil {
			return nil, fmt.Errorf("function: array element %d: %w", i, err)
		}
		members[i] = f
	}
	return &arrayFunc{members: members}, nil
}

func (f *arrayFunc) Eval(in []float64) []float64 {
	out := make([]float64, len(f.members))
	for i, m := range f.members {
		r := m.Eval(in)
		if len(r) > 0 {
			out[i] = r[0]
		}
	}
	return out
}

func (f *arrayFunc) NumOutputs() int { return len(f.members) }
