package function

import (
	"math"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// approxEqual reports whether a and b are within tol.
func approxEqual(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// assertOut checks that got matches want within tol, element-wise.
func assertOut(t *testing.T, got, want []float64, tol float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("output length = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i := range want {
		if !approxEqual(got[i], want[i], tol) {
			t.Fatalf("output[%d] = %g, want %g (got %v, want %v)", i, got[i], want[i], got, want)
		}
	}
}

const tol = 1e-9

// --- Type 2 ---

func TestType2Linear(t *testing.T) {
	dict := pdf.Dict{
		"FunctionType": pdf.Integer(2),
		"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		"C0":           pdf.Array{pdf.Real(1), pdf.Real(0), pdf.Real(0)},
		"C1":           pdf.Array{pdf.Real(0), pdf.Real(0), pdf.Real(1)},
		"N":            pdf.Integer(1),
	}
	f, err := Parse(nil, dict)
	if err != nil {
		t.Fatal(err)
	}
	if f.NumOutputs() != 3 {
		t.Fatalf("NumOutputs = %d, want 3", f.NumOutputs())
	}
	assertOut(t, f.Eval([]float64{0}), []float64{1, 0, 0}, tol)
	assertOut(t, f.Eval([]float64{1}), []float64{0, 0, 1}, tol)
	assertOut(t, f.Eval([]float64{0.5}), []float64{0.5, 0, 0.5}, tol)
}

func TestType2Exponent(t *testing.T) {
	dict := pdf.Dict{
		"FunctionType": pdf.Integer(2),
		"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		"C0":           pdf.Array{pdf.Real(0)},
		"C1":           pdf.Array{pdf.Real(1)},
		"N":            pdf.Integer(2),
	}
	f, err := Parse(nil, dict)
	if err != nil {
		t.Fatal(err)
	}
	// x^2 interpolation: out = 0 + x^2*(1-0).
	assertOut(t, f.Eval([]float64{0.5}), []float64{0.25}, tol)
	assertOut(t, f.Eval([]float64{0.3}), []float64{0.09}, tol)
}

func TestType2Defaults(t *testing.T) {
	// No /C0 or /C1: defaults [0.0] and [1.0].
	dict := pdf.Dict{
		"FunctionType": pdf.Integer(2),
		"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		"N":            pdf.Integer(1),
	}
	f, err := Parse(nil, dict)
	if err != nil {
		t.Fatal(err)
	}
	assertOut(t, f.Eval([]float64{0.25}), []float64{0.25}, tol)
}

func TestType2ClampDomain(t *testing.T) {
	dict := pdf.Dict{
		"FunctionType": pdf.Integer(2),
		"Domain":       pdf.Array{pdf.Real(0), pdf.Real(1)},
		"C0":           pdf.Array{pdf.Real(0)},
		"C1":           pdf.Array{pdf.Real(1)},
		"N":            pdf.Integer(1),
	}
	f, _ := Parse(nil, dict)
	// Inputs outside [0,1] clamp.
	assertOut(t, f.Eval([]float64{-5}), []float64{0}, tol)
	assertOut(t, f.Eval([]float64{5}), []float64{1}, tol)
}

// --- Type 3 ---

func type2SubDict(c0, c1 []float64) pdf.Dict {
	toArr := func(vs []float64) pdf.Array {
		a := make(pdf.Array, len(vs))
		for i, v := range vs {
			a[i] = pdf.Real(v)
		}
		return a
	}
	return pdf.Dict{
		"FunctionType": pdf.Integer(2),
		"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		"C0":           toArr(c0),
		"C1":           toArr(c1),
		"N":            pdf.Integer(1),
	}
}

func TestType3Stitching(t *testing.T) {
	// Two linear subfunctions stitched at 0.5.
	// f0: 0->[0], 1->[1]  (black to red would be 3-comp; keep 1-comp here)
	// f1: 0->[1], 1->[0]
	sub0 := type2SubDict([]float64{0}, []float64{1})
	sub1 := type2SubDict([]float64{1}, []float64{0})
	dict := pdf.Dict{
		"FunctionType": pdf.Integer(3),
		"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		"Functions":    pdf.Array{sub0, sub1},
		"Bounds":       pdf.Array{pdf.Real(0.5)},
		// Encode each subdomain back to [0,1].
		"Encode": pdf.Array{pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1)},
	}
	f, err := Parse(nil, dict)
	if err != nil {
		t.Fatal(err)
	}
	// Left subdomain [0,0.5) maps to [0,1] then f0(t)=t.
	assertOut(t, f.Eval([]float64{0}), []float64{0}, tol)
	assertOut(t, f.Eval([]float64{0.25}), []float64{0.5}, tol) // (0.25-0)/(0.5-0)=0.5
	// Right subdomain [0.5,1] maps to [0,1] then f1(t)=1-t.
	assertOut(t, f.Eval([]float64{0.5}), []float64{1}, tol)    // (0.5-0.5)/(1-0.5)=0 -> f1(0)=1
	assertOut(t, f.Eval([]float64{0.75}), []float64{0.5}, tol) // (0.75-0.5)/0.5=0.5 -> f1(0.5)=0.5
	assertOut(t, f.Eval([]float64{1}), []float64{0}, tol)      // f1(1)=0
}

// TestType3DepthGuard pins the recursion-depth guard added for the adversarial-review
// finding that a Type 3 (stitching) function whose /Functions reference each other (or
// itself, via an indirect ref) recursed until the goroutine stack overflowed — a
// crafted-PDF crash. Deeply nesting Type 3 dicts past maxFunctionDepth must return an
// error rather than overflowing. (A direct deep nest reproduces the unbounded-recursion
// shape without needing an indirect cycle, which would require a full *pdf.Document.)
func TestType3DepthGuard(t *testing.T) {
	// Build a chain of nested Type 3 functions deeper than the guard limit.
	leaf := type2SubDict([]float64{0}, []float64{1})
	cur := pdf.Object(leaf)
	for i := 0; i < maxFunctionDepth+5; i++ {
		cur = pdf.Dict{
			"FunctionType": pdf.Integer(3),
			"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"Functions":    pdf.Array{cur},
			"Bounds":       pdf.Array{}, // k-1 = 0 for a single subfunction
			"Encode":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		}
	}
	_, err := Parse(nil, cur)
	if err == nil {
		t.Fatal("expected an error for over-deep Type 3 nesting (cyclic-reference guard), got nil")
	}
}

func TestType3EncodeMapping(t *testing.T) {
	// Single subfunction but encode maps [0,1] -> [0,0.5], so f0 only sees half.
	sub0 := type2SubDict([]float64{0}, []float64{1})
	dict := pdf.Dict{
		"FunctionType": pdf.Integer(3),
		"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		"Functions":    pdf.Array{sub0},
		"Bounds":       pdf.Array{},
		"Encode":       pdf.Array{pdf.Real(0), pdf.Real(0.5)},
	}
	f, err := Parse(nil, dict)
	if err != nil {
		t.Fatal(err)
	}
	// input 1 maps via encode to 0.5 -> f0(0.5)=0.5
	assertOut(t, f.Eval([]float64{1}), []float64{0.5}, tol)
	assertOut(t, f.Eval([]float64{0}), []float64{0}, tol)
}

// --- Type 0 ---

func TestType0Sampled1D(t *testing.T) {
	// /Size [2], BitsPerSample 8, 3 components per sample: a 2-entry gradient
	// from (255,0,0) to (0,0,255). Decode maps 0..255 -> 0..1.
	raw := []byte{
		255, 0, 0, // sample 0
		0, 0, 255, // sample 1
	}
	stream := &pdf.Stream{
		Dict: pdf.Dict{
			"FunctionType":  pdf.Integer(0),
			"Domain":        pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"Range":         pdf.Array{pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1)},
			"Size":          pdf.Array{pdf.Integer(2)},
			"BitsPerSample": pdf.Integer(8),
		},
		Raw: raw,
	}
	f, err := Parse(&pdf.Document{}, stream)
	if err != nil {
		t.Fatal(err)
	}
	if f.NumOutputs() != 3 {
		t.Fatalf("NumOutputs = %d, want 3", f.NumOutputs())
	}
	assertOut(t, f.Eval([]float64{0}), []float64{1, 0, 0}, 1e-6)
	assertOut(t, f.Eval([]float64{1}), []float64{0, 0, 1}, 1e-6)
	assertOut(t, f.Eval([]float64{0.5}), []float64{0.5, 0, 0.5}, 1e-6)
	assertOut(t, f.Eval([]float64{0.25}), []float64{0.75, 0, 0.25}, 1e-6)
}

func TestType0Sampled4bit(t *testing.T) {
	// /Size [2], BitsPerSample 4, 1 component: samples 0x0 and 0xF packed in one
	// byte (0000 1111). Decode default = Range [0 1].
	raw := []byte{0x0F}
	stream := &pdf.Stream{
		Dict: pdf.Dict{
			"FunctionType":  pdf.Integer(0),
			"Domain":        pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"Range":         pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"Size":          pdf.Array{pdf.Integer(2)},
			"BitsPerSample": pdf.Integer(4),
		},
		Raw: raw,
	}
	f, err := Parse(&pdf.Document{}, stream)
	if err != nil {
		t.Fatal(err)
	}
	assertOut(t, f.Eval([]float64{0}), []float64{0}, 1e-6)
	assertOut(t, f.Eval([]float64{1}), []float64{1}, 1e-6)
	assertOut(t, f.Eval([]float64{0.5}), []float64{0.5}, 1e-6)
}

func TestType0Sampled2D(t *testing.T) {
	// 2-D: /Size [2 2], 1 component, 8 bits. Samples in row-major with dim0
	// fastest: (0,0)=0, (1,0)=255, (0,1)=255, (1,1)=0  (a checkerboard-ish ramp).
	raw := []byte{0, 255, 255, 0}
	stream := &pdf.Stream{
		Dict: pdf.Dict{
			"FunctionType":  pdf.Integer(0),
			"Domain":        pdf.Array{pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1)},
			"Range":         pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"Size":          pdf.Array{pdf.Integer(2), pdf.Integer(2)},
			"BitsPerSample": pdf.Integer(8),
		},
		Raw: raw,
	}
	f, err := Parse(&pdf.Document{}, stream)
	if err != nil {
		t.Fatal(err)
	}
	// Corners.
	assertOut(t, f.Eval([]float64{0, 0}), []float64{0}, 1e-6)
	assertOut(t, f.Eval([]float64{1, 0}), []float64{1}, 1e-6)
	assertOut(t, f.Eval([]float64{0, 1}), []float64{1}, 1e-6)
	assertOut(t, f.Eval([]float64{1, 1}), []float64{0}, 1e-6)
	// Center: bilinear average of {0,1,1,0}/255 = 0.5.
	assertOut(t, f.Eval([]float64{0.5, 0.5}), []float64{0.5}, 1e-6)
}

// --- Type 4 ---

func newType4(t *testing.T, src string, rng pdf.Array) Func {
	t.Helper()
	return newType4Dom(t, src, rng, pdf.Array{pdf.Integer(-100), pdf.Integer(100)})
}

func newType4Dom(t *testing.T, src string, rng, domain pdf.Array) Func {
	t.Helper()
	stream := &pdf.Stream{
		Dict: pdf.Dict{
			"FunctionType": pdf.Integer(4),
			"Domain":       domain,
			"Range":        rng,
		},
		Raw: []byte(src),
	}
	f, err := Parse(&pdf.Document{}, stream)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return f
}

func TestType4Arithmetic(t *testing.T) {
	rng := pdf.Array{pdf.Integer(0), pdf.Integer(100)}
	f := newType4(t, "{ 2 mul }", rng)
	assertOut(t, f.Eval([]float64{3}), []float64{6}, tol)

	f2 := newType4(t, "{ dup mul }", rng)
	assertOut(t, f2.Eval([]float64{4}), []float64{16}, tol)
}

func TestType4IfElse(t *testing.T) {
	rng := pdf.Array{pdf.Integer(0), pdf.Integer(1)}
	f := newType4(t, "{ 0.5 gt { 1 } { 0 } ifelse }", rng)
	assertOut(t, f.Eval([]float64{0.75}), []float64{1}, tol)
	assertOut(t, f.Eval([]float64{0.25}), []float64{0}, tol)
}

func TestType4If(t *testing.T) {
	// If input > 0.5, double it; else leave it. Output range [0,2].
	rng := pdf.Array{pdf.Integer(0), pdf.Integer(2)}
	f := newType4(t, "{ dup 0.5 gt { 2 mul } if }", rng)
	assertOut(t, f.Eval([]float64{0.75}), []float64{1.5}, tol)
	assertOut(t, f.Eval([]float64{0.25}), []float64{0.25}, tol)
}

func TestType4StackOps(t *testing.T) {
	rng := pdf.Array{pdf.Integer(-10), pdf.Integer(10)}
	// exch then sub: input 7 -> push 2, exch -> stack [2,7]... test roll/index.
	f := newType4(t, "{ 3 exch sub }", rng) // 3 - x
	assertOut(t, f.Eval([]float64{1}), []float64{2}, tol)

	// index: duplicate the input via 0 index, then add.
	f2 := newType4(t, "{ 0 index add }", rng) // x + x
	assertOut(t, f2.Eval([]float64{3}), []float64{6}, tol)
}

func TestType4MultiOutput(t *testing.T) {
	// Two outputs: x and 1-x.
	rng := pdf.Array{pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1)}
	f := newType4(t, "{ dup 1 exch sub }", rng)
	if f.NumOutputs() != 2 {
		t.Fatalf("NumOutputs = %d, want 2", f.NumOutputs())
	}
	assertOut(t, f.Eval([]float64{0.3}), []float64{0.3, 0.7}, tol)
}

func TestType4Trig(t *testing.T) {
	rng := pdf.Array{pdf.Integer(-2), pdf.Integer(2)}
	f := newType4(t, "{ 90 sin }", rng) // sin(90 deg)=1, input ignored
	assertOut(t, f.Eval([]float64{0}), []float64{1}, 1e-9)
}

func TestType4RangeClamp(t *testing.T) {
	// Output clamped to range.
	rng := pdf.Array{pdf.Integer(0), pdf.Integer(1)}
	f := newType4(t, "{ 5 mul }", rng)
	assertOut(t, f.Eval([]float64{1}), []float64{1}, tol) // 5 clamped to 1
}

// --- Array of functions ---

func TestArrayOfFunctions(t *testing.T) {
	mk := func(c0, c1 float64) pdf.Dict {
		return pdf.Dict{
			"FunctionType": pdf.Integer(2),
			"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"C0":           pdf.Array{pdf.Real(c0)},
			"C1":           pdf.Array{pdf.Real(c1)},
			"N":            pdf.Integer(1),
		}
	}
	arr := pdf.Array{mk(1, 0), mk(0, 0), mk(0, 1)}
	f, err := Parse(nil, arr)
	if err != nil {
		t.Fatal(err)
	}
	if f.NumOutputs() != 3 {
		t.Fatalf("NumOutputs = %d, want 3", f.NumOutputs())
	}
	assertOut(t, f.Eval([]float64{0.5}), []float64{0.5, 0, 0.5}, tol)
}

// --- Malformed inputs ---

func TestMalformed(t *testing.T) {
	cases := map[string]pdf.Object{
		"nil":             nil,
		"unknown-type":    pdf.Dict{"FunctionType": pdf.Integer(99), "Domain": pdf.Array{pdf.Integer(0), pdf.Integer(1)}},
		"missing-type":    pdf.Dict{"Domain": pdf.Array{pdf.Integer(0), pdf.Integer(1)}},
		"type2-no-N":      pdf.Dict{"FunctionType": pdf.Integer(2), "Domain": pdf.Array{pdf.Integer(0), pdf.Integer(1)}},
		"type2-no-domain": pdf.Dict{"FunctionType": pdf.Integer(2), "N": pdf.Integer(1)},
		"type0-not-stream": pdf.Dict{
			"FunctionType": pdf.Integer(0),
			"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"Range":        pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		},
		"type3-bad-bounds": pdf.Dict{
			"FunctionType": pdf.Integer(3),
			"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"Functions":    pdf.Array{type2SubDict([]float64{0}, []float64{1})},
			"Bounds":       pdf.Array{pdf.Real(0.5)}, // should be empty for k=1
			"Encode":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		},
		"not-a-function": pdf.Integer(42),
		"empty-array":    pdf.Array{},
	}
	for name, obj := range cases {
		t.Run(name, func(t *testing.T) {
			f, err := Parse(&pdf.Document{}, obj)
			if err == nil {
				t.Fatalf("expected error, got func %v", f)
			}
		})
	}
}

func TestType0ShortStream(t *testing.T) {
	// Declares 2 samples of 3 comps at 8 bits = 6 bytes, but provides 3.
	stream := &pdf.Stream{
		Dict: pdf.Dict{
			"FunctionType":  pdf.Integer(0),
			"Domain":        pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"Range":         pdf.Array{pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1), pdf.Integer(0), pdf.Integer(1)},
			"Size":          pdf.Array{pdf.Integer(2)},
			"BitsPerSample": pdf.Integer(8),
		},
		Raw: []byte{1, 2, 3},
	}
	if _, err := Parse(&pdf.Document{}, stream); err == nil {
		t.Fatal("expected error for short sample stream")
	}
}

func TestType4BadBraces(t *testing.T) {
	stream := &pdf.Stream{
		Dict: pdf.Dict{
			"FunctionType": pdf.Integer(4),
			"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"Range":        pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		},
		Raw: []byte("{ 2 mul "), // missing closing brace
	}
	if _, err := Parse(&pdf.Document{}, stream); err == nil {
		t.Fatal("expected error for unbalanced braces")
	}
}
