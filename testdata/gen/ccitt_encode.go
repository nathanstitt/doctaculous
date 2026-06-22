package gen

// A minimal CCITT Group 4 (T.6) encoder used only to produce hermetic test
// fixtures. It emits horizontal and vertical (V0..V3) modes — a valid subset of
// T.6 — terminated by an EOFB (two EOL codes). It deliberately duplicates a few
// small run-length tables rather than depending on the filter package, keeping
// the gen package free of import cycles.

type ceBitWriter struct {
	out   []byte
	cur   byte
	nBits int
}

func (w *ceBitWriter) write(val, n int) {
	for i := n - 1; i >= 0; i-- {
		w.cur = (w.cur << 1) | byte((val>>i)&1)
		w.nBits++
		if w.nBits == 8 {
			w.out = append(w.out, w.cur)
			w.cur, w.nBits = 0, 0
		}
	}
}

func (w *ceBitWriter) bytes() []byte {
	out := w.out
	if w.nBits > 0 {
		out = append(out, w.cur<<(8-w.nBits))
	}
	return out
}

type ceRun struct {
	bits, len, run int
}

// Terminating codes (runs 0..63) and a few makeup codes, white then black. These
// match ITU-T T.4; fixtures here use only short runs (<64), but makeup codes are
// included so wider fixtures could be added later.
var ceWhiteTerm = []ceRun{
	{0x35, 8, 0}, {0x7, 6, 1}, {0x7, 4, 2}, {0x8, 4, 3}, {0xB, 4, 4}, {0xC, 4, 5},
	{0xE, 4, 6}, {0xF, 4, 7}, {0x13, 5, 8}, {0x14, 5, 9}, {0x7, 5, 10}, {0x8, 5, 11},
	{0x8, 6, 12}, {0x3, 6, 13}, {0x34, 6, 14}, {0x35, 6, 15}, {0x2A, 6, 16}, {0x2B, 6, 17},
	{0x27, 7, 18}, {0xC, 7, 19}, {0x8, 7, 20}, {0x17, 7, 21}, {0x3, 7, 22}, {0x4, 7, 23},
	{0x28, 7, 24}, {0x2B, 7, 25}, {0x13, 7, 26}, {0x24, 7, 27}, {0x18, 7, 28}, {0x2, 8, 29},
	{0x3, 8, 30}, {0x1A, 8, 31}, {0x1B, 8, 32}, {0x12, 8, 33}, {0x13, 8, 34}, {0x14, 8, 35},
	{0x15, 8, 36}, {0x16, 8, 37}, {0x17, 8, 38}, {0x28, 8, 39}, {0x29, 8, 40}, {0x2A, 8, 41},
	{0x2B, 8, 42}, {0x2C, 8, 43}, {0x2D, 8, 44}, {0x4, 8, 45}, {0x5, 8, 46}, {0xA, 8, 47},
	{0xB, 8, 48}, {0x52, 8, 49}, {0x53, 8, 50}, {0x54, 8, 51}, {0x55, 8, 52}, {0x24, 8, 53},
	{0x25, 8, 54}, {0x58, 8, 55}, {0x59, 8, 56}, {0x5A, 8, 57}, {0x5B, 8, 58}, {0x4A, 8, 59},
	{0x4B, 8, 60}, {0x32, 8, 61}, {0x33, 8, 62}, {0x34, 8, 63},
}

var ceBlackTerm = []ceRun{
	{0x37, 10, 0}, {0x2, 3, 1}, {0x3, 2, 2}, {0x2, 2, 3}, {0x3, 3, 4}, {0x3, 4, 5},
	{0x2, 4, 6}, {0x3, 5, 7}, {0x5, 6, 8}, {0x4, 6, 9}, {0x4, 7, 10}, {0x5, 7, 11},
	{0x7, 7, 12}, {0x4, 8, 13}, {0x7, 8, 14}, {0x18, 9, 15}, {0x17, 10, 16}, {0x18, 10, 17},
	{0x8, 10, 18}, {0x67, 11, 19}, {0x68, 11, 20}, {0x6C, 11, 21}, {0x37, 11, 22}, {0x28, 11, 23},
	{0x17, 11, 24}, {0x18, 11, 25}, {0xCA, 12, 26}, {0xCB, 12, 27}, {0xCC, 12, 28}, {0xCD, 12, 29},
	{0x68, 12, 30}, {0x69, 12, 31}, {0x6A, 12, 32}, {0x6B, 12, 33}, {0xD2, 12, 34}, {0xD3, 12, 35},
	{0xD4, 12, 36}, {0xD5, 12, 37}, {0xD6, 12, 38}, {0xD7, 12, 39}, {0x6C, 12, 40}, {0x6D, 12, 41},
	{0xDA, 12, 42}, {0xDB, 12, 43}, {0x54, 12, 44}, {0x55, 12, 45}, {0x56, 12, 46}, {0x57, 12, 47},
	{0x64, 12, 48}, {0x65, 12, 49}, {0x52, 12, 50}, {0x53, 12, 51}, {0x24, 12, 52}, {0x37, 12, 53},
	{0x38, 12, 54}, {0x27, 12, 55}, {0x28, 12, 56}, {0x58, 12, 57}, {0x59, 12, 58}, {0x2B, 12, 59},
	{0x2C, 12, 60}, {0x5A, 12, 61}, {0x66, 12, 62}, {0x67, 12, 63},
}

func ceWriteRun(w *ceBitWriter, run int, white bool) {
	term := ceBlackTerm
	if white {
		term = ceWhiteTerm
	}
	// Fixtures use runs < 64 only.
	for _, e := range term {
		if e.run == run {
			w.write(e.bits, e.len)
			return
		}
	}
}

func ceChanges(row []bool, cols int) []int {
	var ch []int
	white := true
	for x := 0; x < cols; x++ {
		black := x < len(row) && row[x]
		isWhite := !black
		if isWhite != white {
			ch = append(ch, x)
			white = isWhite
		}
	}
	return append(ch, cols, cols)
}

func ceFindB1B2(ref []int, a0 int, a0White bool, cols int) (int, int) {
	wantEven := a0White
	i := 0
	for i < len(ref) && ref[i] <= a0 {
		i++
	}
	if (i%2 == 0) != wantEven {
		i++
	}
	b1, b2 := cols, cols
	if i < len(ref) {
		b1 = ref[i]
	}
	if i+1 < len(ref) {
		b2 = ref[i+1]
	}
	if b1 > cols {
		b1 = cols
	}
	if b2 > cols {
		b2 = cols
	}
	return b1, b2
}

func ceAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// encodeG4Frame encodes rows (true = black) as a Group 4 stream terminated by
// EOFB, using vertical and horizontal modes.
func encodeG4Frame(rows [][]bool, cols int) []byte {
	w := &ceBitWriter{}
	ref := []int{cols, cols}
	for _, row := range rows {
		cur := ceChanges(row, cols)
		a0, white, ci := -1, true, 0
		for a0 < cols {
			a1 := cols
			if ci < len(cur) {
				a1 = cur[ci]
			}
			b1, b2 := ceFindB1B2(ref, a0, white, cols)
			switch {
			case b2 < a1:
				w.write(0b0001, 4) // pass
				a0 = b2
			case ceAbs(a1-b1) <= 3:
				switch a1 - b1 {
				case 0:
					w.write(0b1, 1)
				case 1:
					w.write(0b011, 3)
				case 2:
					w.write(0b000011, 6)
				case 3:
					w.write(0b0000011, 7)
				case -1:
					w.write(0b010, 3)
				case -2:
					w.write(0b000010, 6)
				case -3:
					w.write(0b0000010, 7)
				}
				a0 = a1
				white = !white
				ci++
			default:
				start := a0
				if start < 0 {
					start = 0
				}
				a2 := cols
				if ci+1 < len(cur) {
					a2 = cur[ci+1]
				}
				w.write(0b001, 3)
				ceWriteRun(w, a1-start, white)
				ceWriteRun(w, a2-a1, !white)
				a0 = a2
				ci += 2
			}
		}
		ref = cur
	}
	w.write(1, 12) // EOFB part 1
	w.write(1, 12) // EOFB part 2
	return w.bytes()
}
