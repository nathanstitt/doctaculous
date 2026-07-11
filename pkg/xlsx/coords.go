package xlsx

import (
	"fmt"
	"strconv"
	"strings"
)

// The exported coordinate helpers are 1-based (the spreadsheet convention:
// A1 is row 1, column 1). The Workbook grid remains 0-based.

// ColumnName converts a 1-based column number to its letter name (1 → "A",
// 27 → "AA"). Column numbers < 1 yield "".
func ColumnName(col int) string {
	if col < 1 {
		return ""
	}
	var buf [8]byte
	i := len(buf)
	for col > 0 {
		col--
		i--
		buf[i] = byte('A' + col%26)
		col /= 26
	}
	return string(buf[i:])
}

// CellRef converts 1-based (row, col) to an A1 reference ("B7"). Out-of-range
// inputs yield "".
func CellRef(row, col int) string {
	if row < 1 || col < 1 {
		return ""
	}
	return ColumnName(col) + strconv.Itoa(row)
}

// ParseCellRef converts an A1 reference (with or without $ anchors) to
// 1-based (row, col).
func ParseCellRef(ref string) (row, col int, err error) {
	s := strings.ReplaceAll(strings.TrimSpace(ref), "$", "")
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch >= 'a' && ch <= 'z' {
			ch -= 'a' - 'A'
		}
		if ch < 'A' || ch > 'Z' {
			break
		}
		col = col*26 + int(ch-'A'+1)
		i++
	}
	if i == 0 || i == len(s) {
		return 0, 0, fmt.Errorf("xlsx: bad cell reference %q", ref)
	}
	row, aerr := strconv.Atoi(s[i:])
	if aerr != nil || row < 1 {
		return 0, 0, fmt.Errorf("xlsx: bad cell reference %q", ref)
	}
	return row, col, nil
}

// Range is a rectangular cell range, 1-based inclusive.
type Range struct {
	StartRow, StartCol, EndRow, EndCol int
}

// ParseRange parses "A1:B4" (or a single cell "A1") into a Range, normalizing
// so Start <= End on both axes.
func ParseRange(ref string) (Range, error) {
	from, to, ok := strings.Cut(strings.TrimSpace(ref), ":")
	if !ok {
		to = from
	}
	r1, c1, err := ParseCellRef(from)
	if err != nil {
		return Range{}, err
	}
	r2, c2, err := ParseCellRef(to)
	if err != nil {
		return Range{}, err
	}
	if r2 < r1 {
		r1, r2 = r2, r1
	}
	if c2 < c1 {
		c1, c2 = c2, c1
	}
	return Range{StartRow: r1, StartCol: c1, EndRow: r2, EndCol: c2}, nil
}

// String renders the range as "A1:B4" (a single cell renders as "A1").
func (r Range) String() string {
	if r.StartRow == r.EndRow && r.StartCol == r.EndCol {
		return CellRef(r.StartRow, r.StartCol)
	}
	return CellRef(r.StartRow, r.StartCol) + ":" + CellRef(r.EndRow, r.EndCol)
}
