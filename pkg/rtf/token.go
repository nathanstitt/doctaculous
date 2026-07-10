package rtf

// tokenKind classifies one RTF token.
type tokenKind int

const (
	tokEOF tokenKind = iota
	tokGroupOpen
	tokGroupClose
	// tokControl is a control word (\b, \fs24) or control symbol (\~, \*).
	tokControl
	// tokText is a run of document text (escapes already resolved by the
	// tokenizer where they are pure text: \\, \{, \}).
	tokText
	// tokHexByte is a \'hh escaped byte (the cp1252 code the converter maps).
	tokHexByte
)

// token is one lexical RTF item.
type token struct {
	kind tokenKind
	// word is the control word/symbol name ("b", "fs", "*", "~").
	word string
	// param is the control word's numeric parameter; hasParam marks presence.
	param    int
	hasParam bool
	// text is the tokText content or the tokHexByte value (one byte).
	text string
	b    byte
}

// tokenizer walks raw RTF bytes.
type tokenizer struct {
	src []byte
	pos int
}

// next returns the next token. Malformed input never panics: stray bytes are
// text, a truncated escape is dropped at EOF.
func (tz *tokenizer) next() token {
	if tz.pos >= len(tz.src) {
		return token{kind: tokEOF}
	}
	ch := tz.src[tz.pos]
	switch ch {
	case '{':
		tz.pos++
		return token{kind: tokGroupOpen}
	case '}':
		tz.pos++
		return token{kind: tokGroupClose}
	case '\\':
		return tz.control()
	case '\r', '\n':
		// Raw newlines are insignificant in RTF (writers wrap freely).
		tz.pos++
		return tz.next()
	default:
		return tz.text()
	}
}

// control reads a control word or symbol after the backslash.
func (tz *tokenizer) control() token {
	tz.pos++ // consume the backslash
	if tz.pos >= len(tz.src) {
		return token{kind: tokEOF}
	}
	ch := tz.src[tz.pos]

	// Control symbols: a single non-alphabetic character.
	if !isAlpha(ch) {
		tz.pos++
		switch ch {
		case '\\', '{', '}':
			return token{kind: tokText, text: string(ch)}
		case '\'': // \'hh — a code-page byte
			if tz.pos+1 < len(tz.src) {
				hi, ok1 := hexVal(tz.src[tz.pos])
				lo, ok2 := hexVal(tz.src[tz.pos+1])
				if ok1 && ok2 {
					tz.pos += 2
					return token{kind: tokHexByte, b: hi<<4 | lo}
				}
			}
			return tz.next() // malformed escape: drop
		case '\r', '\n':
			// \<newline> is a \par alias some producers emit.
			return token{kind: tokControl, word: "par"}
		default:
			return token{kind: tokControl, word: string(ch)}
		}
	}

	// Control word: letters, optional signed integer parameter, optional
	// single trailing space (consumed as part of the word).
	start := tz.pos
	for tz.pos < len(tz.src) && isAlpha(tz.src[tz.pos]) {
		tz.pos++
	}
	word := string(tz.src[start:tz.pos])

	tok := token{kind: tokControl, word: word}
	if tz.pos < len(tz.src) && (tz.src[tz.pos] == '-' || isDigit(tz.src[tz.pos])) {
		neg := false
		if tz.src[tz.pos] == '-' {
			neg = true
			tz.pos++
		}
		n := 0
		for tz.pos < len(tz.src) && isDigit(tz.src[tz.pos]) {
			n = n*10 + int(tz.src[tz.pos]-'0')
			tz.pos++
		}
		if neg {
			n = -n
		}
		tok.param, tok.hasParam = n, true
	}
	if tz.pos < len(tz.src) && tz.src[tz.pos] == ' ' {
		tz.pos++ // the delimiter space belongs to the control word
	}
	return tok
}

// text reads a run of plain text up to the next delimiter.
func (tz *tokenizer) text() token {
	start := tz.pos
	for tz.pos < len(tz.src) {
		switch tz.src[tz.pos] {
		case '{', '}', '\\', '\r', '\n':
			return token{kind: tokText, text: string(tz.src[start:tz.pos])}
		}
		tz.pos++
	}
	return token{kind: tokText, text: string(tz.src[start:tz.pos])}
}

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// cp1252High maps the 0x80–0x9F range where Windows-1252 diverges from
// Latin-1 (the rest of the byte range maps 1:1 to the same code points).
var cp1252High = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

// cp1252Rune converts a code-page byte to its rune.
func cp1252Rune(b byte) rune {
	if b >= 0x80 && b <= 0x9F {
		return cp1252High[b-0x80]
	}
	return rune(b)
}
