package content

import (
	"image"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Resources supplies page resources the interpreter needs: fonts, images,
// nested form XObjects, and extended graphics states. The backend implements it
// so font-program and image decoding stay out of this package.
type Resources interface {
	// Font returns a usable font for the resource name (without leading slash),
	// or nil if it cannot be resolved.
	Font(name string) GlyphSource
	// Image returns a decoded image XObject for the resource name, or ok=false if
	// it is not an image or could not be decoded. fill is the current fill color,
	// used to paint /ImageMask stencils (ignored by ordinary images).
	Image(name string, fill render.FillColor) (img image.Image, ok bool)
	// InlineImage decodes a BI...ID...EI inline image into a drawable image. dict
	// holds the inline image parameters with their keys as written (abbreviated,
	// e.g. W/H/CS/BPC/F/IM); data is the verbatim sample bytes between ID and EI.
	// fill is the current fill color (for /ImageMask). ok=false if the image
	// cannot be decoded.
	InlineImage(dict pdf.Dict, data []byte, fill render.FillColor) (img image.Image, ok bool)
	// Form returns the decoded content bytes and resources of a form XObject, or
	// ok=false if name is not a form XObject. matrix is the form's /Matrix.
	Form(name string) (content []byte, res Resources, matrix render.Matrix, ok bool)
	// ExtGState returns the named entry of the /ExtGState resource dict, or
	// ok=false if it is absent. Only the parameters the interpreter applies are
	// reported (see ExtGStateParams); unsupported entries are flagged so the
	// caller can log graceful degradation.
	ExtGState(name string) (params ExtGStateParams, ok bool)
}

// ExtGStateParams holds the subset of an ExtGState dictionary the interpreter
// understands. Fill/StrokeAlpha come from /ca and /CA. HasUnsupported is true
// when the dict carries entries we do not interpret (a non-Normal /BM blend mode
// or a non-None /SMask), so the caller can emit a degradation log.
type ExtGStateParams struct {
	FillAlpha      float64
	HasFillAlpha   bool
	StrokeAlpha    float64
	HasStrokeAlpha bool
	// BlendMode is the /BM entry (a separable or non-separable PDF blend-mode
	// name), set only when present. "Normal"/"Compatible" mean source-over.
	BlendMode      string
	HasBlendMode   bool
	HasUnsupported bool
}

// Interpreter executes a content stream against a Device.
type Interpreter struct {
	doc    *pdf.Document
	dev    render.Device
	res    Resources
	logf   func(string, ...any)
	maxOps int

	stack []gstate
	gs    gstate

	path    render.Path // current path being constructed (device space)
	pending pendingClip

	// curUserX/curUserY track the current point in user space, needed by the
	// v/y curve operators whose control points reference it.
	curUserX, curUserY float64
}

// pendingClip records a clip requested by W/W* to be applied after the next
// path-painting operator, per the PDF spec.
type pendingClip struct {
	active bool
	rule   render.FillRule
}

// Options configures an Interpreter.
type Options struct {
	// Logf, if set, receives debug messages about unsupported operators. It must
	// be safe for concurrent use if the same Options is shared across goroutines.
	Logf func(string, ...any)
	// MaxOps caps the number of operators executed (0 = no cap), a guard against
	// pathological or hostile content streams.
	MaxOps int
}

// New creates an Interpreter that draws onto dev using res to resolve resources.
// base is the matrix mapping PDF user space to device pixels.
func New(doc *pdf.Document, dev render.Device, res Resources, base render.Matrix, opts Options) *Interpreter {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Interpreter{
		doc:    doc,
		dev:    dev,
		res:    res,
		logf:   logf,
		maxOps: opts.MaxOps,
		gs:     newGState(base),
	}
}

// Run interprets the content bytes, issuing draw calls to the Device. It never
// returns an error for unsupported operators (those are logged and skipped);
// it returns an error only for unrecoverable tokenizer failures.
func (it *Interpreter) Run(content []byte) error {
	return it.run(content, 0)
}

// run executes a content stream at the given form-XObject nesting depth.
func (it *Interpreter) run(content []byte, depth int) error {
	if depth > 16 {
		it.logf("content: form XObject nesting too deep (%d), skipping", depth)
		return nil
	}
	tok := newContentTokenizer(content)
	var operands []pdf.Object
	ops := 0
	for {
		t, isOp, err := tok.next()
		if err != nil {
			// Tokenizer hit malformed bytes; stop gracefully rather than abort.
			it.logf("content: tokenizer stopped: %v", err)
			return nil
		}
		if t == nil && !isOp {
			return nil // EOF
		}
		if !isOp {
			operands = append(operands, t)
			// Bound operand growth defensively.
			if len(operands) > 64 {
				operands = operands[len(operands)-64:]
			}
			continue
		}
		op := t.(pdf.Name) // tokenizer encodes operators as Name
		ops++
		if it.maxOps > 0 && ops > it.maxOps {
			it.logf("content: operator cap (%d) reached, stopping", it.maxOps)
			return nil
		}
		if op == "BI" {
			// Inline image: the body (params + raw samples) is not ordinary tokens,
			// so consume it directly from the scanner and draw it here.
			it.inlineImage(tok)
			operands = operands[:0]
			continue
		}
		it.execute(string(op), operands, depth)
		operands = operands[:0]
	}
}
