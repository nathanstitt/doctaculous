package content

import (
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// contentTokenizer yields a stream of operands (pdf.Object) and operators. An
// operator is returned as a pdf.Name with isOp=true. Operands include numbers,
// strings, names, booleans, null, arrays, and inline dictionaries.
//
// It is a thin layer over pdf.ParseContentToken, which exposes the package's
// object grammar for content streams.
type contentTokenizer struct {
	p *pdf.ContentScanner
}

func newContentTokenizer(src []byte) *contentTokenizer {
	return &contentTokenizer{p: pdf.NewContentScanner(src)}
}

// next returns the next operand or operator. At EOF it returns (nil, false, nil).
func (t *contentTokenizer) next() (obj pdf.Object, isOp bool, err error) {
	o, op, ok, e := t.p.Next()
	if e != nil {
		return nil, false, fmt.Errorf("content token: %w", e)
	}
	if !ok {
		return nil, false, nil
	}
	if op != "" {
		return pdf.Name(op), true, nil
	}
	return o, false, nil
}
