package pdf

import (
	"bytes"
	"fmt"
)

// maxObjectDepth bounds how deeply arrays and dictionaries may nest. Nesting is
// parsed by recursion (parseArray and parseDictOrStream both re-enter through
// parseFromToken), so an unbounded depth is unbounded stack: measured, ~1.2 MB
// of "[" survives but ~1.5 MB raises `fatal error: stack overflow` through
// runtime.throw, which recover() cannot catch by construction. That makes it a
// process kill rather than a failed parse, so the depth has to be refused before
// the stack runs out. 256 is far past any real document -- the deepest structure
// in a PDF is a handful of levels -- and matches the bounded style of the other
// recursive walks here (walkPageTree 64, content interp 16, functions 32).
const maxObjectDepth = 256

// objParser parses PDF objects from a lexer. It supports the "N G R" indirect
// reference form, which requires a small lookahead buffer.
type objParser struct {
	lex *lexer
	// buffered holds peeked tokens not yet consumed.
	buffered []token
	// depth is the current array/dictionary nesting depth, bounded by
	// maxObjectDepth. Held on the parser rather than threaded through every
	// parse call because parseFromToken is re-entered from several places.
	depth int
}

func newObjParser(src []byte) *objParser {
	return &objParser{lex: newLexer(src)}
}

func (p *objParser) peek() (token, error) {
	if len(p.buffered) > 0 {
		return p.buffered[0], nil
	}
	t, err := p.lex.next()
	if err != nil {
		return token{}, err
	}
	p.buffered = append(p.buffered, t)
	return t, nil
}

func (p *objParser) take() (token, error) {
	if len(p.buffered) > 0 {
		t := p.buffered[0]
		p.buffered = p.buffered[1:]
		return t, nil
	}
	return p.lex.next()
}

// pushback returns a token to the front of the buffer.
func (p *objParser) pushback(t token) {
	p.buffered = append([]token{t}, p.buffered...)
}

// parseObject parses a single object value. It resolves the "N G R" reference
// form and "N G obj ... endobj" only at the top level via parseIndirect; here a
// bare integer that is not followed by "G R" is returned as an Integer.
func (p *objParser) parseObject() (Object, error) {
	t, err := p.take()
	if err != nil {
		return nil, err
	}
	return p.parseFromToken(t)
}

func (p *objParser) parseFromToken(t token) (Object, error) {
	switch t.kind {
	case tokEOF:
		return nil, fmt.Errorf("pdf: unexpected EOF while parsing object")
	case tokInteger:
		return p.parseIntegerOrRef(t)
	case tokReal:
		return Real(t.num), nil
	case tokString:
		return String(t.val), nil
	case tokName:
		return Name(t.val), nil
	case tokArrayOpen:
		return p.parseArray()
	case tokDictOpen:
		return p.parseDictOrStream()
	case tokKeyword:
		switch string(t.val) {
		case "true":
			return Boolean(true), nil
		case "false":
			return Boolean(false), nil
		case "null":
			return Null{}, nil
		default:
			return nil, fmt.Errorf("pdf: unexpected keyword %q at offset %d", t.val, t.pos)
		}
	default:
		return nil, fmt.Errorf("pdf: unexpected token (kind %d) at offset %d", t.kind, t.pos)
	}
}

// parseIntegerOrRef handles the ambiguity between an integer and an indirect
// reference "N G R". It looks ahead two tokens.
func (p *objParser) parseIntegerOrRef(first token) (Object, error) {
	t2, err := p.take()
	if err != nil {
		return nil, err
	}
	if t2.kind == tokInteger {
		t3, err := p.take()
		if err != nil {
			return nil, err
		}
		if t3.kind == tokKeyword && string(t3.val) == "R" {
			return Reference{Number: int(first.num), Generation: int(t2.num)}, nil
		}
		// Not a reference: push back t2 and t3 and return the first integer.
		p.pushback(t3)
		p.pushback(t2)
		return Integer(int64(first.num)), nil
	}
	p.pushback(t2)
	return Integer(int64(first.num)), nil
}

func (p *objParser) parseArray() (Object, error) {
	if p.depth >= maxObjectDepth {
		return nil, fmt.Errorf("pdf: array nesting deeper than %d", maxObjectDepth)
	}
	p.depth++
	defer func() { p.depth-- }()

	var arr Array
	for {
		t, err := p.take()
		if err != nil {
			return nil, err
		}
		if t.kind == tokArrayClose {
			return arr, nil
		}
		if t.kind == tokEOF {
			return nil, fmt.Errorf("pdf: unterminated array")
		}
		obj, err := p.parseFromToken(t)
		if err != nil {
			return nil, err
		}
		arr = append(arr, obj)
	}
}

func (p *objParser) parseDictOrStream() (Object, error) {
	if p.depth >= maxObjectDepth {
		return nil, fmt.Errorf("pdf: dictionary nesting deeper than %d", maxObjectDepth)
	}
	p.depth++
	defer func() { p.depth-- }()

	dict := Dict{}
	for {
		t, err := p.take()
		if err != nil {
			return nil, err
		}
		if t.kind == tokDictClose {
			break
		}
		if t.kind == tokEOF {
			return nil, fmt.Errorf("pdf: unterminated dictionary")
		}
		if t.kind != tokName {
			return nil, fmt.Errorf("pdf: expected name key in dictionary at offset %d", t.pos)
		}
		key := Name(t.val)
		val, err := p.parseObject()
		if err != nil {
			return nil, err
		}
		dict[key] = val
	}

	// A dictionary may be followed by "stream"; detect it.
	next, err := p.peek()
	if err != nil {
		return nil, err
	}
	if next.kind == tokKeyword && string(next.val) == "stream" {
		_, _ = p.take() // consume "stream"
		return p.parseStreamBody(dict)
	}
	return dict, nil
}

// parseStreamBody reads raw stream bytes following the "stream" keyword. The
// keyword is followed by CRLF or LF, then Length bytes, then "endstream".
// Because Length may be an indirect reference we can't resolve here, we locate
// the bytes by scanning to "endstream" and let the caller trust /Length when it
// is a literal. The raw bytes are returned still encoded.
func (p *objParser) parseStreamBody(dict Dict) (Object, error) {
	// After "stream" the spec requires CRLF or LF. Sync the lexer position past it.
	src := p.lex.src
	pos := p.lex.pos
	// We may have buffered tokens; for stream bodies there should be none because
	// peek consumed "stream" via take. Defensive check:
	if len(p.buffered) > 0 {
		return nil, fmt.Errorf("pdf: internal: buffered tokens before stream body")
	}
	if pos < len(src) && src[pos] == '\r' {
		pos++
	}
	if pos < len(src) && src[pos] == '\n' {
		pos++
	}
	start := pos

	// Prefer a literal /Length when present and plausible.
	//
	// The bound is written as a subtraction rather than "start+n <= len(src)"
	// because /Length comes from the file: a large enough value makes start+n
	// overflow to a NEGATIVE number, which passes that comparison and then
	// indexes the source slice out of range. Found by fuzzing, as
	// `index out of range [-9223372036854775764]`. Comparing against the
	// remaining length cannot overflow, since both sides are already in range.
	if lenObj, ok := dict["Length"]; ok {
		if n, ok := IntValue(lenObj); ok && n >= 0 && n <= len(src)-start {
			end := start + n
			// Verify "endstream" follows (allowing whitespace). If not, fall back
			// to scanning.
			if hasEndstreamNear(src, end) {
				raw := src[start:end]
				p.lex.pos = skipToAfterEndstream(src, end)
				return &Stream{Dict: dict, Raw: bytes.Clone(raw)}, nil
			}
		}
	}

	// Fallback: scan for "endstream".
	idx := indexEndstream(src, start)
	if idx < 0 {
		return nil, fmt.Errorf("pdf: stream missing endstream (offset %d)", start)
	}
	end := idx
	// Trim a single trailing EOL that precedes endstream.
	if end > start && src[end-1] == '\n' {
		end--
	}
	if end > start && src[end-1] == '\r' {
		end--
	}
	raw := src[start:end]
	p.lex.pos = idx + len("endstream")
	return &Stream{Dict: dict, Raw: bytes.Clone(raw)}, nil
}

// hasEndstreamNear reports whether "endstream" appears at end after optional
// whitespace. end is clamped rather than trusted: callers derive it from a
// file-supplied /Length, and an out-of-range value here is a panic, not a
// wrong answer.
func hasEndstreamNear(src []byte, end int) bool {
	if end < 0 || end > len(src) {
		return false
	}
	i := end
	for i < len(src) && isWhitespace(src[i]) {
		i++
	}
	return bytes.HasPrefix(src[i:], []byte("endstream"))
}

// skipToAfterEndstream returns the offset just past the "endstream" following
// end, or end when none is there. Like hasEndstreamNear it clamps end, which is
// derived from a file-supplied /Length: today it is only reached after that
// function has vetted the same offset, but an indexing panic is too cheap to
// prevent to leave resting on a caller's invariant.
func skipToAfterEndstream(src []byte, end int) int {
	if end < 0 || end > len(src) {
		return end
	}
	i := end
	for i < len(src) && isWhitespace(src[i]) {
		i++
	}
	if bytes.HasPrefix(src[i:], []byte("endstream")) {
		return i + len("endstream")
	}
	return end
}

func indexEndstream(src []byte, from int) int {
	rel := bytes.Index(src[from:], []byte("endstream"))
	if rel < 0 {
		return -1
	}
	return from + rel
}
