package pdf

// ContentScanner tokenizes a PDF content stream into operands and operators. It
// reuses the object grammar (numbers, strings, names, arrays, inline dicts) and
// reports bare keywords as operators. It is used by the content interpreter
// package; exposing it here avoids duplicating the lexer.
type ContentScanner struct {
	p *objParser
}

// NewContentScanner returns a scanner over a content stream's bytes.
func NewContentScanner(src []byte) *ContentScanner {
	return &ContentScanner{p: newObjParser(src)}
}

// Next returns the next item. For an operand it returns (obj, "", true, nil).
// For an operator it returns (nil, opName, true, nil). At EOF it returns
// (nil, "", false, nil). Operators true/false/null are returned as operand
// objects (Boolean/Null), matching their meaning in content streams.
func (s *ContentScanner) Next() (obj Object, op string, ok bool, err error) {
	for {
		o, op, ok, err, retry := s.next1()
		if retry {
			continue // skip a stray delimiter without growing the stack
		}
		return o, op, ok, err
	}
}

// next1 returns one token. retry=true means an unexpected delimiter was skipped
// and the caller should call again (handled by the loop in Next).
func (s *ContentScanner) next1() (obj Object, op string, ok bool, err error, retry bool) {
	t, err := s.p.take()
	if err != nil {
		return nil, "", false, err, false
	}
	switch t.kind {
	case tokEOF:
		return nil, "", false, nil, false
	case tokInteger:
		// Content streams have no indirect references, so an integer is always a
		// number operand (do not invoke the "N G R" lookahead).
		return Integer(int64(t.num)), "", true, nil, false
	case tokReal:
		return Real(t.num), "", true, nil, false
	case tokString:
		return String(t.val), "", true, nil, false
	case tokName:
		return Name(t.val), "", true, nil, false
	case tokArrayOpen:
		arr, err := s.p.parseArray()
		if err != nil {
			return nil, "", false, err, false
		}
		return arr, "", true, nil, false
	case tokDictOpen:
		d, err := s.p.parseDictOrStream()
		if err != nil {
			return nil, "", false, err, false
		}
		return d, "", true, nil, false
	case tokKeyword:
		switch string(t.val) {
		case "true":
			return Boolean(true), "", true, nil, false
		case "false":
			return Boolean(false), "", true, nil, false
		case "null":
			return Null{}, "", true, nil, false
		default:
			return nil, string(t.val), true, nil, false
		}
	default:
		// Skip unexpected delimiters (e.g. stray ']' or '>>') without aborting.
		return nil, "", false, nil, true
	}
}
