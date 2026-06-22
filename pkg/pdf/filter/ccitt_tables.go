package filter

import "fmt"

// bitReaderMSB reads bits MSB-first from a byte slice. It is a small value type
// so the decoder can snapshot and restore position (for lookahead) by copying.
type bitReaderMSB struct {
	data   []byte
	bitPos int // absolute bit index from the start of data
}

// bit returns the next bit (0 or 1) and ok=false when the stream is exhausted.
func (b *bitReaderMSB) bit() (int, bool) {
	if b.bitPos >= len(b.data)*8 {
		return 0, false
	}
	byteIdx := b.bitPos >> 3
	bitIdx := 7 - (b.bitPos & 7)
	v := int((b.data[byteIdx] >> uint(bitIdx)) & 1)
	b.bitPos++
	return v, true
}

// exhausted reports whether no more bits remain.
func (b *bitReaderMSB) exhausted() bool { return b.bitPos >= len(b.data)*8 }

// align advances to the next byte boundary (for /EncodedByteAlign).
func (b *bitReaderMSB) align() {
	if rem := b.bitPos & 7; rem != 0 {
		b.bitPos += 8 - rem
	}
}

// 2D (modified READ) mode identifiers.
const (
	modePass = iota
	modeHorizontal
	modeV0
	modeVR1
	modeVR2
	modeVR3
	modeVL1
	modeVL2
	modeVL3
	modeEOL
)

// modeCode is one entry in the 2D mode Huffman table: a bit pattern of the given
// length mapping to a mode.
type modeCode struct {
	bits int
	len  int
	mode int
}

// modeCodes lists the T.6 2D mode codes (ITU-T T.6 Table 1). Longest-prefix
// matching is unnecessary because the set is prefix-free; we match by trying
// increasing lengths.
var modeCodes = []modeCode{
	{0b1, 1, modeV0},           // V0
	{0b011, 3, modeVR1},        // VR1
	{0b010, 3, modeVL1},        // VL1
	{0b001, 3, modeHorizontal}, // Horizontal
	{0b0001, 4, modePass},      // Pass
	{0b000011, 6, modeVR2},     // VR2
	{0b000010, 6, modeVL2},     // VL2
	{0b0000011, 7, modeVR3},    // VR3
	{0b0000010, 7, modeVL3},    // VL3
	// EOL (000000000001) is handled separately via peekEOL.
}

// readMode reads one 2D mode code. An EOL prefix (>=6 leading zeros) is reported
// as modeEOL so the row decoder can terminate.
func (d *ccittDecoder) readMode() (int, error) {
	var acc, n int
	for n < 14 {
		bit, ok := d.br.bit()
		if !ok {
			if n == 0 {
				return modeEOL, nil // clean end of data at a mode boundary
			}
			return 0, fmt.Errorf("%w: truncated 2D mode", ErrCCITT)
		}
		acc = (acc << 1) | bit
		n++
		for _, mc := range modeCodes {
			if mc.len == n && mc.bits == acc {
				return mc.mode, nil
			}
		}
		// A long run of leading zeros indicates an EOL/EOFB code.
		if acc == 0 && n >= 11 {
			// Consume the terminating 1 bit if present.
			b, ok := d.br.bit()
			if ok && b == 1 {
				return modeEOL, nil
			}
			return modeEOL, nil
		}
	}
	return 0, fmt.Errorf("%w: invalid 2D mode code", ErrCCITT)
}

// huffEntry is one run-length code: a bit pattern of len bits mapping to a run.
type huffEntry struct {
	bits int
	len  int
	run  int
}

// readRun reads a complete run length of the given colour, summing makeup codes
// (>=64) until a terminating code (<64) is read.
func (d *ccittDecoder) readRun(white bool) (int, error) {
	total := 0
	for {
		run, err := d.readRunCode(white)
		if err != nil {
			return 0, err
		}
		total += run
		if run < 64 {
			return total, nil
		}
		// Makeup code: continue accumulating the same colour.
		if total > 1<<24 {
			return 0, fmt.Errorf("%w: run length overflow", ErrCCITT)
		}
	}
}

// readRunCode reads a single terminating or makeup code for the given colour.
func (d *ccittDecoder) readRunCode(white bool) (int, error) {
	table := blackCodes
	if white {
		table = whiteCodes
	}
	var acc, n int
	for n < 14 {
		bit, ok := d.br.bit()
		if !ok {
			return 0, fmt.Errorf("%w: truncated run code", ErrCCITT)
		}
		acc = (acc << 1) | bit
		n++
		if r, ok := table[codeKey{acc, n}]; ok {
			return r, nil
		}
	}
	return 0, fmt.Errorf("%w: invalid run code", ErrCCITT)
}

// codeKey identifies a Huffman code by its bit pattern and length (needed because
// the same numeric value at different lengths is a different code).
type codeKey struct {
	bits int
	len  int
}

// whiteCodes and blackCodes map (bits,len) → run length, built once from the
// T.4 terminating and makeup code tables.
var whiteCodes = buildCodeMap(whiteTerminating, sharedMakeup, whiteMakeup)
var blackCodes = buildCodeMap(blackTerminating, sharedMakeup, blackMakeup)

func buildCodeMap(tables ...[]huffEntry) map[codeKey]int {
	m := make(map[codeKey]int)
	for _, t := range tables {
		for _, e := range t {
			m[codeKey{e.bits, e.len}] = e.run
		}
	}
	return m
}

// whiteTerminating: T.4 terminating white codes for runs 0..63.
var whiteTerminating = []huffEntry{
	{0x35, 8, 0}, {0x7, 6, 1}, {0x7, 4, 2}, {0x8, 4, 3},
	{0xB, 4, 4}, {0xC, 4, 5}, {0xE, 4, 6}, {0xF, 4, 7},
	{0x13, 5, 8}, {0x14, 5, 9}, {0x7, 5, 10}, {0x8, 5, 11},
	{0x8, 6, 12}, {0x3, 6, 13}, {0x34, 6, 14}, {0x35, 6, 15},
	{0x2A, 6, 16}, {0x2B, 6, 17}, {0x27, 7, 18}, {0xC, 7, 19},
	{0x8, 7, 20}, {0x17, 7, 21}, {0x3, 7, 22}, {0x4, 7, 23},
	{0x28, 7, 24}, {0x2B, 7, 25}, {0x13, 7, 26}, {0x24, 7, 27},
	{0x18, 7, 28}, {0x2, 8, 29}, {0x3, 8, 30}, {0x1A, 8, 31},
	{0x1B, 8, 32}, {0x12, 8, 33}, {0x13, 8, 34}, {0x14, 8, 35},
	{0x15, 8, 36}, {0x16, 8, 37}, {0x17, 8, 38}, {0x28, 8, 39},
	{0x29, 8, 40}, {0x2A, 8, 41}, {0x2B, 8, 42}, {0x2C, 8, 43},
	{0x2D, 8, 44}, {0x4, 8, 45}, {0x5, 8, 46}, {0xA, 8, 47},
	{0xB, 8, 48}, {0x52, 8, 49}, {0x53, 8, 50}, {0x54, 8, 51},
	{0x55, 8, 52}, {0x24, 8, 53}, {0x25, 8, 54}, {0x58, 8, 55},
	{0x59, 8, 56}, {0x5A, 8, 57}, {0x5B, 8, 58}, {0x4A, 8, 59},
	{0x4B, 8, 60}, {0x32, 8, 61}, {0x33, 8, 62}, {0x34, 8, 63},
}

// whiteMakeup: white makeup codes for runs 64..1728 (step 64).
var whiteMakeup = []huffEntry{
	{0x1B, 5, 64}, {0x12, 5, 128}, {0x17, 6, 192}, {0x37, 7, 256},
	{0x36, 8, 320}, {0x37, 8, 384}, {0x64, 8, 448}, {0x65, 8, 512},
	{0x68, 8, 576}, {0x67, 8, 640}, {0xCC, 9, 704}, {0xCD, 9, 768},
	{0xD2, 9, 832}, {0xD3, 9, 896}, {0xD4, 9, 960}, {0xD5, 9, 1024},
	{0xD6, 9, 1088}, {0xD7, 9, 1152}, {0xD8, 9, 1216}, {0xD9, 9, 1280},
	{0xDA, 9, 1344}, {0xDB, 9, 1408}, {0x98, 9, 1472}, {0x99, 9, 1536},
	{0x9A, 9, 1600}, {0x18, 6, 1664}, {0x9B, 9, 1728},
}

// blackTerminating: T.4 terminating black codes for runs 0..63.
var blackTerminating = []huffEntry{
	{0x37, 10, 0}, {0x2, 3, 1}, {0x3, 2, 2}, {0x2, 2, 3},
	{0x3, 3, 4}, {0x3, 4, 5}, {0x2, 4, 6}, {0x3, 5, 7},
	{0x5, 6, 8}, {0x4, 6, 9}, {0x4, 7, 10}, {0x5, 7, 11},
	{0x7, 7, 12}, {0x4, 8, 13}, {0x7, 8, 14}, {0x18, 9, 15},
	{0x17, 10, 16}, {0x18, 10, 17}, {0x8, 10, 18}, {0x67, 11, 19},
	{0x68, 11, 20}, {0x6C, 11, 21}, {0x37, 11, 22}, {0x28, 11, 23},
	{0x17, 11, 24}, {0x18, 11, 25}, {0xCA, 12, 26}, {0xCB, 12, 27},
	{0xCC, 12, 28}, {0xCD, 12, 29}, {0x68, 12, 30}, {0x69, 12, 31},
	{0x6A, 12, 32}, {0x6B, 12, 33}, {0xD2, 12, 34}, {0xD3, 12, 35},
	{0xD4, 12, 36}, {0xD5, 12, 37}, {0xD6, 12, 38}, {0xD7, 12, 39},
	{0x6C, 12, 40}, {0x6D, 12, 41}, {0xDA, 12, 42}, {0xDB, 12, 43},
	{0x54, 12, 44}, {0x55, 12, 45}, {0x56, 12, 46}, {0x57, 12, 47},
	{0x64, 12, 48}, {0x65, 12, 49}, {0x52, 12, 50}, {0x53, 12, 51},
	{0x24, 12, 52}, {0x37, 12, 53}, {0x38, 12, 54}, {0x27, 12, 55},
	{0x28, 12, 56}, {0x58, 12, 57}, {0x59, 12, 58}, {0x2B, 12, 59},
	{0x2C, 12, 60}, {0x5A, 12, 61}, {0x66, 12, 62}, {0x67, 12, 63},
}

// blackMakeup: black makeup codes for runs 64..1728 (step 64).
var blackMakeup = []huffEntry{
	{0xF, 10, 64}, {0xC8, 12, 128}, {0xC9, 12, 192}, {0x5B, 12, 256},
	{0x33, 12, 320}, {0x34, 12, 384}, {0x35, 12, 448}, {0x6C, 13, 512},
	{0x6D, 13, 576}, {0x4A, 13, 640}, {0x4B, 13, 704}, {0x4C, 13, 768},
	{0x4D, 13, 832}, {0x72, 13, 896}, {0x73, 13, 960}, {0x74, 13, 1024},
	{0x75, 13, 1088}, {0x76, 13, 1152}, {0x77, 13, 1216}, {0x52, 13, 1280},
	{0x53, 13, 1344}, {0x54, 13, 1408}, {0x55, 13, 1472}, {0x5A, 13, 1536},
	{0x5B, 13, 1600}, {0x64, 13, 1664}, {0x65, 13, 1728},
}

// sharedMakeup: extended makeup codes (runs 1792..2560) shared by both colours.
var sharedMakeup = []huffEntry{
	{0x8, 11, 1792}, {0xC, 11, 1856}, {0xD, 11, 1920}, {0x12, 12, 1984},
	{0x13, 12, 2048}, {0x14, 12, 2112}, {0x15, 12, 2176}, {0x16, 12, 2240},
	{0x17, 12, 2304}, {0x1C, 12, 2368}, {0x1D, 12, 2432}, {0x1E, 12, 2496},
	{0x1F, 12, 2560},
}
