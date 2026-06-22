package pdf

import "fmt"

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

// ReadInlineImage parses the body of an inline image after the BI operator has
// been returned by Next. It reads the abbreviated key/value pairs up to the ID
// keyword, then captures the raw sample bytes up to the EI delimiter, and leaves
// the scanner positioned to continue with the operators that follow.
//
// The returned dict uses the keys as written (abbreviated, e.g. /W, /H, /CS, /F,
// /BPC, /IM, /D); the caller is responsible for normalizing them. The data is
// the verbatim (still-encoded) bytes between the single whitespace after ID and
// the EI delimiter.
//
// Per the spec the sample data starts after exactly one whitespace byte
// following ID and ends at EI preceded by whitespace and followed by whitespace
// or EOF. Because the data is arbitrary binary, EI is only honored at a token
// boundary (whitespace/delimiter on both sides), so a literal "EI" inside the
// samples does not end the image prematurely.
func (s *ContentScanner) ReadInlineImage() (Dict, []byte, error) {
	dict := Dict{}
	for {
		t, err := s.p.take()
		if err != nil {
			return nil, nil, err
		}
		switch t.kind {
		case tokEOF:
			return nil, nil, fmt.Errorf("pdf: inline image: missing ID")
		case tokKeyword:
			if string(t.val) == "ID" {
				data, err := s.readInlineData()
				if err != nil {
					return nil, nil, err
				}
				return dict, data, nil
			}
			// true/false/null can legitimately be values; handle below as objects.
			return nil, nil, fmt.Errorf("pdf: inline image: unexpected keyword %q before ID", t.val)
		case tokName:
			key := Name(t.val)
			val, err := s.p.parseObject()
			if err != nil {
				return nil, nil, err
			}
			dict[key] = val
		default:
			return nil, nil, fmt.Errorf("pdf: inline image: expected key name, got token kind %d", t.kind)
		}
	}
}

// readInlineData captures raw bytes from just after ID to the EI delimiter. The
// lexer cursor sits right after the ID keyword. Exactly one whitespace byte
// separates ID from the data; the data ends at a whitespace-delimited EI.
func (s *ContentScanner) readInlineData() ([]byte, error) {
	src := s.p.lex.src
	pos := s.p.lex.pos
	// Skip the single mandatory whitespace byte after ID.
	if pos < len(src) && isWhitespace(src[pos]) {
		pos++
	}
	start := pos
	for pos < len(src) {
		// Look for "EI" at a token boundary: preceded by whitespace and followed by
		// whitespace or EOF. This avoids stopping on an "EI" that is part of the
		// binary sample data.
		if src[pos] == 'E' && pos+1 < len(src) && src[pos+1] == 'I' &&
			pos > start && isWhitespace(src[pos-1]) &&
			(pos+2 == len(src) || isWhitespace(src[pos+2]) || isDelimiter(src[pos+2])) {
			end := pos - 1 // exclude the whitespace immediately before EI
			s.p.lex.pos = pos + 2
			return src[start:end], nil
		}
		pos++
	}
	return nil, fmt.Errorf("pdf: inline image: missing EI")
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
