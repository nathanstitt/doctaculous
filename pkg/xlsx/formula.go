package xlsx

import (
	"strconv"
	"strings"
)

// shiftFormula rewrites the RELATIVE cell references in a shared-formula
// master so it reads correctly at a member cell offset by (dRow, dCol) —
// the same expansion Excel applies when materializing a shared formula.
// $-anchored axes stay fixed; quoted string literals and 'quoted sheet names'
// pass through untouched. The scan is a lexical A1 pass, not a full formula
// parser: a token of letters immediately followed by digits (with optional $
// anchors) that is not part of a longer identifier is a cell reference.
func shiftFormula(src string, dRow, dCol int) string {
	if dRow == 0 && dCol == 0 {
		return src
	}
	var sb strings.Builder
	i := 0
	n := len(src)
	for i < n {
		switch ch := src[i]; ch {
		case '"': // string literal ("" escapes a quote)
			j := i + 1
			for j < n {
				if src[j] == '"' {
					if j+1 < n && src[j+1] == '"' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			sb.WriteString(src[i:j])
			i = j
		case '\'': // quoted sheet name ('' escapes)
			j := i + 1
			for j < n {
				if src[j] == '\'' {
					if j+1 < n && src[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			sb.WriteString(src[i:j])
			i = j
		default:
			if ref, next, ok := scanRef(src, i); ok {
				sb.WriteString(shiftRef(ref, dRow, dCol))
				i = next
				continue
			}
			sb.WriteByte(ch)
			i++
		}
	}
	return sb.String()
}

// scanRef tries to read a cell reference at src[i]: optional $, letters,
// optional $, digits — not preceded or followed by an identifier character
// (so SUM2020 or A1B are not references).
func scanRef(src string, i int) (ref string, next int, ok bool) {
	if i > 0 && isIdentChar(src[i-1]) {
		return "", 0, false
	}
	j := i
	n := len(src)
	if j < n && src[j] == '$' {
		j++
	}
	colStart := j
	for j < n && isLetter(src[j]) {
		j++
	}
	if j == colStart || j-colStart > 3 {
		return "", 0, false
	}
	if j < n && src[j] == '$' {
		j++
	}
	rowStart := j
	for j < n && src[j] >= '0' && src[j] <= '9' {
		j++
	}
	if j == rowStart {
		return "", 0, false
	}
	if j < n && (isIdentChar(src[j]) || src[j] == '(') {
		// A trailing identifier char extends the token (A1B is a name); a "("
		// makes it a function call (LOG10(...)), not a reference.
		return "", 0, false
	}
	return src[i:j], j, true
}

func isLetter(c byte) bool { return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }
func isIdentChar(c byte) bool {
	return isLetter(c) || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '$'
}

// shiftRef offsets one reference's un-anchored axes.
func shiftRef(ref string, dRow, dCol int) string {
	s := ref
	colAnchored := strings.HasPrefix(s, "$")
	if colAnchored {
		s = s[1:]
	}
	k := 0
	for k < len(s) && isLetter(s[k]) {
		k++
	}
	colPart := s[:k]
	s = s[k:]
	rowAnchored := strings.HasPrefix(s, "$")
	if rowAnchored {
		s = s[1:]
	}
	rowNum, err := strconv.Atoi(s)
	if err != nil {
		return ref
	}

	col := 0
	for i := 0; i < len(colPart); i++ {
		c := colPart[i]
		if c >= 'a' {
			c -= 'a' - 'A'
		}
		col = col*26 + int(c-'A'+1)
	}
	if !colAnchored {
		col += dCol
	}
	if !rowAnchored {
		rowNum += dRow
	}
	if col < 1 || rowNum < 1 {
		return "#REF!"
	}

	var out strings.Builder
	if colAnchored {
		out.WriteByte('$')
	}
	out.WriteString(ColumnName(col))
	if rowAnchored {
		out.WriteByte('$')
	}
	out.WriteString(strconv.Itoa(rowNum))
	return out.String()
}
