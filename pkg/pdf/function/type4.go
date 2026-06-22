package function

import (
	"fmt"
	"math"
	"strconv"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// psFunc is a Type 4 (PostScript calculator) function. It holds a parsed program
// (a tree of tokens and nested procedure blocks) and evaluates it as a small RPN
// machine. Inputs are pushed in order; after execution the top n stack values
// (in order) are the outputs.
type psFunc struct {
	domain []float64
	rng    []float64
	prog   []psToken
	n      int
}

func parsePostScript(doc *pdf.Document, obj pdf.Object) (Func, error) {
	stream, ok := resolve(doc, obj).(*pdf.Stream)
	if !ok {
		return nil, fmt.Errorf("function: Type 4 must be a stream")
	}
	dict := stream.Dict

	domain, err := requireFloatArray(doc, dict, "Domain")
	if err != nil {
		return nil, err
	}
	rng, err := requireFloatArray(doc, dict, "Range")
	if err != nil {
		return nil, err
	}
	if len(rng) == 0 || len(rng)%2 != 0 {
		return nil, fmt.Errorf("function: Type 4 /Range malformed")
	}

	data, _, err := doc.DecodedStream(stream)
	if err != nil {
		return nil, fmt.Errorf("function: Type 4 decode stream: %w", err)
	}

	toks, err := tokenizePS(data)
	if err != nil {
		return nil, fmt.Errorf("function: Type 4 tokenize: %w", err)
	}
	prog, rest, err := parsePSBlock(toks, true)
	if err != nil {
		return nil, fmt.Errorf("function: Type 4 parse: %w", err)
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("function: Type 4 trailing tokens after program")
	}

	return &psFunc{
		domain: domain,
		rng:    rng,
		prog:   prog,
		n:      len(rng) / 2,
	}, nil
}

func (f *psFunc) NumOutputs() int { return f.n }

func (f *psFunc) Eval(in []float64) []float64 {
	clamped := clampToDomain(in, f.domain)
	st := &psStack{}
	for _, v := range clamped {
		st.push(v)
	}
	if err := execPS(f.prog, st); err != nil {
		// Malformed runtime: return zeros of the right length.
		return make([]float64, f.n)
	}

	out := make([]float64, f.n)
	// The top n values are the outputs; the last pushed value is the last output.
	if st.len() >= f.n {
		base := st.len() - f.n
		for j := 0; j < f.n; j++ {
			out[j] = st.vals[base+j]
		}
	} else {
		// Too few results: fill what we have, leave the rest zero.
		copy(out[f.n-st.len():], st.vals)
	}
	clampToRange(out, f.rng)
	return out
}

// --- tokenizer ---

type psKind int

const (
	psNum psKind = iota
	psOp
	psLBrace
	psRBrace
	psProc // a nested procedure: a resolved block of tokens
)

// psToken is one token. For psNum it carries num; for psOp it carries op; for
// psProc it carries a nested token slice (used as a deferred procedure operand
// for if/ifelse).
type psToken struct {
	kind psKind
	num  float64
	op   string
	proc []psToken
}

// tokenizePS splits PostScript calculator source into flat tokens. It strips the
// outer braces during parsing (parsePSBlock handles nesting). Comments (%) run to
// end of line.
func tokenizePS(data []byte) ([]psToken, error) {
	var toks []psToken
	i := 0
	for i < len(data) {
		c := data[i]
		switch {
		case c == '%':
			for i < len(data) && data[i] != '\n' && data[i] != '\r' {
				i++
			}
		case isPSSpace(c):
			i++
		case c == '{':
			toks = append(toks, psToken{kind: psLBrace})
			i++
		case c == '}':
			toks = append(toks, psToken{kind: psRBrace})
			i++
		default:
			j := i
			for j < len(data) && !isPSSpace(data[j]) && data[j] != '{' && data[j] != '}' && data[j] != '%' {
				j++
			}
			word := string(data[i:j])
			i = j
			if v, err := strconv.ParseFloat(word, 64); err == nil {
				toks = append(toks, psToken{kind: psNum, num: v})
			} else {
				toks = append(toks, psToken{kind: psOp, op: word})
			}
		}
	}
	return toks, nil
}

func isPSSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '\f', 0:
		return true
	}
	return false
}

// parsePSBlock builds a nested program. When outer is true, the first token must
// be '{' and parsing consumes the matching '}', returning the body. Nested '{...}'
// become psProc tokens. It returns the remaining tokens after the block.
func parsePSBlock(toks []psToken, outer bool) ([]psToken, []psToken, error) {
	if len(toks) == 0 || toks[0].kind != psLBrace {
		return nil, nil, fmt.Errorf("expected '{'")
	}
	toks = toks[1:] // consume '{'

	var body []psToken
	for len(toks) > 0 {
		t := toks[0]
		switch t.kind {
		case psRBrace:
			return body, toks[1:], nil
		case psLBrace:
			proc, rest, err := parsePSBlock(toks, false)
			if err != nil {
				return nil, nil, err
			}
			body = append(body, psToken{kind: psProc, proc: proc})
			toks = rest
		default:
			body = append(body, t)
			toks = toks[1:]
		}
	}
	return nil, nil, fmt.Errorf("unbalanced braces: missing '}'")
}

// --- evaluator ---

type psStack struct {
	vals []float64
}

func (s *psStack) push(v float64) { s.vals = append(s.vals, v) }
func (s *psStack) len() int       { return len(s.vals) }

func (s *psStack) pop() (float64, error) {
	if len(s.vals) == 0 {
		return 0, fmt.Errorf("stack underflow")
	}
	v := s.vals[len(s.vals)-1]
	s.vals = s.vals[:len(s.vals)-1]
	return v, nil
}

func (s *psStack) pop2() (a, b float64, err error) {
	b, err = s.pop()
	if err != nil {
		return
	}
	a, err = s.pop()
	return
}

// procStack tracks pending procedure operands (the bodies of {} blocks) so that
// if/ifelse can locate the most recently encountered procedures. Procedures are
// not pushed onto the numeric stack; they are queued here in encounter order and
// consumed by the next control operator.
//
// PostScript semantics put procedures on the operand stack, but since this subset
// only uses procedures as immediate operands of if/ifelse, a small side queue is
// sufficient and avoids polluting the numeric stack with sentinel values.
type psProcQueue struct {
	procs [][]psToken
}

func (q *psProcQueue) push(p []psToken) { q.procs = append(q.procs, p) }
func (q *psProcQueue) pop() ([]psToken, error) {
	if len(q.procs) == 0 {
		return nil, fmt.Errorf("missing procedure operand")
	}
	p := q.procs[len(q.procs)-1]
	q.procs = q.procs[:len(q.procs)-1]
	return p, nil
}

// execPS runs a program body against the stack. Bare procedures (psProc) are
// queued until a control operator (if/ifelse) consumes them.
func execPS(prog []psToken, st *psStack) error {
	q := &psProcQueue{}
	return execBody(prog, st, q)
}

func execBody(prog []psToken, st *psStack, q *psProcQueue) error {
	for _, t := range prog {
		switch t.kind {
		case psNum:
			st.push(t.num)
		case psProc:
			q.push(t.proc)
		case psOp:
			if err := execOp(t.op, st, q); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected token in body")
		}
	}
	return nil
}

const psTrue = 1.0
const psFalse = 0.0

func boolToF(b bool) float64 {
	if b {
		return psTrue
	}
	return psFalse
}

// execOp executes one operator. Comparison/boolean operators use 1.0/0.0 for
// true/false on the numeric stack.
func execOp(op string, st *psStack, q *psProcQueue) error {
	switch op {
	// --- arithmetic ---
	case "add":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(a + b)
	case "sub":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(a - b)
	case "mul":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(a * b)
	case "div":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		if b == 0 {
			st.push(0)
		} else {
			st.push(a / b)
		}
	case "idiv":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		ib := int64(b)
		if ib == 0 {
			st.push(0)
		} else {
			st.push(float64(int64(a) / ib))
		}
	case "mod":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		ib := int64(b)
		if ib == 0 {
			st.push(0)
		} else {
			st.push(float64(int64(a) % ib))
		}
	case "neg":
		a, err := st.pop()
		if err != nil {
			return err
		}
		st.push(-a)
	case "abs":
		a, err := st.pop()
		if err != nil {
			return err
		}
		st.push(math.Abs(a))
	case "sqrt":
		a, err := st.pop()
		if err != nil {
			return err
		}
		if a < 0 {
			a = 0
		}
		st.push(math.Sqrt(a))
	case "sin":
		a, err := st.pop()
		if err != nil {
			return err
		}
		st.push(math.Sin(a * math.Pi / 180))
	case "cos":
		a, err := st.pop()
		if err != nil {
			return err
		}
		st.push(math.Cos(a * math.Pi / 180))
	case "atan":
		num, den, err := st.pop2()
		if err != nil {
			return err
		}
		deg := math.Atan2(num, den) * 180 / math.Pi
		if deg < 0 {
			deg += 360
		}
		st.push(deg)
	case "exp":
		base, e, err := st.pop2()
		if err != nil {
			return err
		}
		v := math.Pow(base, e)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			v = 0
		}
		st.push(v)
	case "ln":
		a, err := st.pop()
		if err != nil {
			return err
		}
		st.push(safeLog(math.Log, a))
	case "log":
		a, err := st.pop()
		if err != nil {
			return err
		}
		st.push(safeLog(math.Log10, a))
	case "cvi", "truncate":
		a, err := st.pop()
		if err != nil {
			return err
		}
		st.push(math.Trunc(a))
	case "cvr":
		// no-op: all values are already real
	case "floor":
		a, err := st.pop()
		if err != nil {
			return err
		}
		st.push(math.Floor(a))
	case "ceiling":
		a, err := st.pop()
		if err != nil {
			return err
		}
		st.push(math.Ceil(a))
	case "round":
		a, err := st.pop()
		if err != nil {
			return err
		}
		st.push(math.Round(a))

	// --- stack ---
	case "dup":
		a, err := st.pop()
		if err != nil {
			return err
		}
		st.push(a)
		st.push(a)
	case "pop":
		if _, err := st.pop(); err != nil {
			return err
		}
	case "exch":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(b)
		st.push(a)
	case "copy":
		nf, err := st.pop()
		if err != nil {
			return err
		}
		nn := int(nf)
		if nn < 0 || nn > st.len() {
			return fmt.Errorf("copy %d out of range", nn)
		}
		base := st.len() - nn
		for i := 0; i < nn; i++ {
			st.push(st.vals[base+i])
		}
	case "index":
		nf, err := st.pop()
		if err != nil {
			return err
		}
		nn := int(nf)
		if nn < 0 || nn >= st.len() {
			return fmt.Errorf("index %d out of range", nn)
		}
		st.push(st.vals[st.len()-1-nn])
	case "roll":
		jf, err := st.pop()
		if err != nil {
			return err
		}
		nf, err := st.pop()
		if err != nil {
			return err
		}
		if err := psRoll(st, int(nf), int(jf)); err != nil {
			return err
		}

	// --- comparison ---
	case "eq":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(boolToF(a == b))
	case "ne":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(boolToF(a != b))
	case "gt":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(boolToF(a > b))
	case "ge":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(boolToF(a >= b))
	case "lt":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(boolToF(a < b))
	case "le":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(boolToF(a <= b))

	// --- boolean / bitwise ---
	case "and":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(float64(int64(a) & int64(b)))
	case "or":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(float64(int64(a) | int64(b)))
	case "xor":
		a, b, err := st.pop2()
		if err != nil {
			return err
		}
		st.push(float64(int64(a) ^ int64(b)))
	case "not":
		a, err := st.pop()
		if err != nil {
			return err
		}
		// Boolean not for 0/1; bitwise complement otherwise.
		switch a {
		case 0:
			st.push(psTrue)
		case 1:
			st.push(psFalse)
		default:
			st.push(float64(^int64(a)))
		}
	case "bitshift":
		a, shift, err := st.pop2()
		if err != nil {
			return err
		}
		iv := int64(a)
		s := int64(shift)
		if s >= 0 {
			st.push(float64(iv << uint(s)))
		} else {
			st.push(float64(iv >> uint(-s)))
		}
	case "true":
		st.push(psTrue)
	case "false":
		st.push(psFalse)

	// --- control ---
	case "if":
		proc, err := q.pop()
		if err != nil {
			return err
		}
		cond, err := st.pop()
		if err != nil {
			return err
		}
		if cond != 0 {
			if err := execBody(proc, st, q); err != nil {
				return err
			}
		}
	case "ifelse":
		procFalse, err := q.pop()
		if err != nil {
			return err
		}
		procTrue, err := q.pop()
		if err != nil {
			return err
		}
		cond, err := st.pop()
		if err != nil {
			return err
		}
		if cond != 0 {
			if err := execBody(procTrue, st, q); err != nil {
				return err
			}
		} else {
			if err := execBody(procFalse, st, q); err != nil {
				return err
			}
		}

	default:
		return fmt.Errorf("unknown operator %q", op)
	}
	return nil
}

func safeLog(fn func(float64) float64, x float64) float64 {
	if x <= 0 {
		return 0
	}
	return fn(x)
}

// psRoll rolls the top n stack elements by j positions (positive = toward the
// top). It is a no-op for n <= 0.
func psRoll(st *psStack, n, j int) error {
	if n < 0 || n > st.len() {
		return fmt.Errorf("roll n=%d out of range", n)
	}
	if n == 0 {
		return nil
	}
	base := st.len() - n
	window := st.vals[base:]
	j = ((j % n) + n) % n // normalize to [0,n)
	if j == 0 {
		return nil
	}
	rolled := make([]float64, n)
	for i := 0; i < n; i++ {
		rolled[(i+j)%n] = window[i]
	}
	copy(window, rolled)
	return nil
}
