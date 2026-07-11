package font

import (
	"unicode/utf16"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// This file parses a font's /ToUnicode CMap into a code→rune map. Per PDF
// 32000-1 §9.10.2 the ToUnicode CMap is the authoritative code→Unicode source
// for text extraction, taking precedence over any encoding-derived mapping —
// it is the only mapping that works for subset fonts whose codes are GIDs
// (Type0/Identity-H) or arbitrary reassigned byte codes (symbolic simple
// fonts with a /Differences encoding of subset glyph names).

// parseToUnicode returns the code→rune map from fontDict's /ToUnicode CMap
// stream, or nil when the font has none (or the stream is undecodable) —
// callers then fall back to their encoding-derived mapping.
func parseToUnicode(doc *pdf.Document, fontDict pdf.Dict) map[int]rune {
	s := doc.GetStream(fontDict["ToUnicode"])
	if s == nil {
		return nil
	}
	data, _, err := doc.DecodedStream(s)
	if err != nil {
		return nil
	}
	return parseToUnicodeCMap(data)
}

// parseToUnicodeCMap tokenizes CMap source and collects the bfchar/bfrange
// sections. The CMap grammar between beginbfchar/endbfchar (and the range
// forms) is plain PDF-object syntax, so the content-stream scanner tokenizes
// it directly; everything outside those sections (codespace ranges, the
// CIDInit boilerplate) is skipped. A malformed tail aborts the scan but keeps
// the entries parsed so far. Returns nil when no mapping was recovered.
func parseToUnicodeCMap(data []byte) map[int]rune {
	const (
		modeNone = iota
		modeBFChar
		modeBFRange
	)
	out := map[int]rune{}
	sc := pdf.NewContentScanner(data)
	mode := modeNone
	var operands []pdf.Object
	for {
		obj, op, ok, err := sc.Next()
		if err != nil || !ok {
			break
		}
		if op == "" {
			operands = append(operands, obj)
			switch {
			case mode == modeBFChar && len(operands) == 2:
				addBFChar(out, operands)
				operands = operands[:0]
			case mode == modeBFRange && len(operands) == 3:
				addBFRange(out, operands)
				operands = operands[:0]
			}
			continue
		}
		switch op {
		case "beginbfchar":
			mode = modeBFChar
		case "beginbfrange":
			mode = modeBFRange
		default:
			mode = modeNone
		}
		operands = operands[:0]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// addBFChar records one bfchar entry: <srcCode> <utf16be>.
func addBFChar(out map[int]rune, ops []pdf.Object) {
	src, ok1 := ops[0].(pdf.String)
	dst, ok2 := ops[1].(pdf.String)
	if !ok1 || !ok2 {
		return
	}
	code := codeInt([]byte(src))
	if code < 0 {
		return
	}
	if r := firstRuneUTF16BE([]byte(dst)); r != 0 {
		out[code] = r
	}
}

// addBFRange records one bfrange entry: <lo> <hi> followed by either a single
// UTF-16BE string (whose last code unit increments across the range) or an
// array of per-code UTF-16BE strings.
func addBFRange(out map[int]rune, ops []pdf.Object) {
	loS, ok1 := ops[0].(pdf.String)
	hiS, ok2 := ops[1].(pdf.String)
	if !ok1 || !ok2 {
		return
	}
	lo, hi := codeInt([]byte(loS)), codeInt([]byte(hiS))
	// The 64k cap matches the widest real code space (2-byte CIDs) and keeps a
	// corrupt range from ballooning the map.
	if lo < 0 || hi < lo || hi-lo > 0xFFFF {
		return
	}
	switch dst := ops[2].(type) {
	case pdf.String:
		units := utf16Units([]byte(dst))
		if len(units) == 0 {
			return
		}
		for c := lo; c <= hi; c++ {
			u := append([]uint16(nil), units...)
			u[len(u)-1] += uint16(c - lo)
			if rs := utf16.Decode(u); len(rs) > 0 && rs[0] != 0 {
				out[c] = rs[0]
			}
		}
	case pdf.Array:
		for i, o := range dst {
			if lo+i > hi {
				break
			}
			s, ok := o.(pdf.String)
			if !ok {
				continue
			}
			if r := firstRuneUTF16BE([]byte(s)); r != 0 {
				out[lo+i] = r
			}
		}
	}
}

// codeInt interprets a big-endian source code of 1..4 bytes, returning -1 for
// an empty or oversized code so callers skip the entry.
func codeInt(b []byte) int {
	if len(b) == 0 || len(b) > 4 {
		return -1
	}
	v := 0
	for _, c := range b {
		v = v<<8 | int(c)
	}
	return v
}

// utf16Units splits big-endian bytes into UTF-16 code units (a trailing odd
// byte is dropped).
func utf16Units(b []byte) []uint16 {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
	}
	return units
}

// firstRuneUTF16BE decodes b as UTF-16BE (surrogate pairs honored) and returns
// the first code point, or 0 when empty. content.Glyph carries a single rune,
// so a multi-code-point target (a ligature expansion like "fi") keeps only its
// first character.
func firstRuneUTF16BE(b []byte) rune {
	rs := utf16.Decode(utf16Units(b))
	if len(rs) == 0 {
		return 0
	}
	return rs[0]
}
