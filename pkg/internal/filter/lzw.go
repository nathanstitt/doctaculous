package filter

import (
	"fmt"
	"slices"
)

const (
	lzwClear = 256
	lzwEOD   = 257
)

// lzwDecode decodes PDF LZWDecode data. PDF uses MSB-first packing, codes from 9
// to 12 bits, a clear code (256) and end-of-data (257). EarlyChange (default 1)
// makes the code width increase one code earlier than the table would suggest.
func lzwDecode(data []byte, p Params) ([]byte, error) {
	earlyChange := p.earlyChange()

	var out []byte
	br := &bitReader{data: data}

	// Dictionary: entries 0..255 are single bytes; 256/257 are control codes.
	var table [][]byte
	reset := func() {
		table = make([][]byte, 0, 4096)
		for i := range 256 {
			table = append(table, []byte{byte(i)})
		}
		// placeholders for clear & EOD
		table = append(table, nil, nil)
	}
	reset()

	codeWidth := 9
	var prev []byte

	for {
		code, ok := br.read(codeWidth)
		if !ok {
			break
		}
		switch code {
		case lzwEOD:
			return out, nil
		case lzwClear:
			reset()
			codeWidth = 9
			prev = nil
			continue
		}

		var entry []byte
		switch {
		case code < len(table) && table[code] != nil:
			entry = table[code]
		case code == len(table) && prev != nil:
			// KwKwK case: new sequence is prev + prev[0].
			entry = append(slices.Clone(prev), prev[0])
		default:
			return nil, fmt.Errorf("lzw: bad code %d (table size %d)", code, len(table))
		}

		out = append(out, entry...)

		if prev != nil {
			newEntry := append(slices.Clone(prev), entry[0])
			table = append(table, newEntry)
		}
		prev = entry

		// Grow the code width as the table fills (accounting for EarlyChange).
		switch len(table) + earlyChange - 1 {
		case 511:
			codeWidth = 10
		case 1023:
			codeWidth = 11
		case 2047:
			codeWidth = 12
		}
	}
	return out, nil
}

// bitReader reads MSB-first bit groups from a byte slice.
type bitReader struct {
	data   []byte
	bitPos int
}

func (b *bitReader) read(n int) (int, bool) {
	if b.bitPos+n > len(b.data)*8 {
		return 0, false
	}
	val := 0
	for range n {
		byteIdx := b.bitPos / 8
		bitIdx := 7 - (b.bitPos % 8)
		bit := (b.data[byteIdx] >> bitIdx) & 1
		val = (val << 1) | int(bit)
		b.bitPos++
	}
	return val, true
}
