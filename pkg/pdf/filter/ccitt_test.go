package filter

import (
	"testing"
)

// --- Test-only CCITT encoders -------------------------------------------------
//
// These mirror the decoder's tables so we can round-trip known bilevel images
// without committing binary fixtures. They implement just enough of T.4/T.6 to
// produce streams the decoder must accept.

// ccittBitWriter writes MSB-first bits.
type ccittBitWriter struct {
	out   []byte
	cur   byte
	nBits int
}

func (w *ccittBitWriter) writeBits(val, n int) {
	for i := n - 1; i >= 0; i-- {
		w.cur = (w.cur << 1) | byte((val>>i)&1)
		w.nBits++
		if w.nBits == 8 {
			w.out = append(w.out, w.cur)
			w.cur, w.nBits = 0, 0
		}
	}
}

func (w *ccittBitWriter) align() {
	if w.nBits > 0 {
		w.out = append(w.out, w.cur<<(8-w.nBits))
		w.cur, w.nBits = 0, 0
	}
}

func (w *ccittBitWriter) bytes() []byte {
	out := w.out
	if w.nBits > 0 {
		out = append(out, w.cur<<(8-w.nBits))
	}
	return out
}

// runEntry looks up the terminating/makeup codes for a run length and colour and
// emits them. Runs >= 64 use makeup + terminating codes.
func (w *ccittBitWriter) writeRun(run int, white bool) {
	term := blackTerminating
	makeup := blackMakeup
	if white {
		term, makeup = whiteTerminating, whiteMakeup
	}
	for run >= 64 {
		// pick the largest makeup <= run (or shared makeup for >=1792)
		best := -1
		var bestE huffEntry
		all := append(append([]huffEntry{}, makeup...), sharedMakeup...)
		for _, e := range all {
			if e.run <= run && e.run > best {
				best = e.run
				bestE = e
			}
		}
		if best <= 0 {
			break
		}
		w.writeBits(bestE.bits, bestE.len)
		run -= best
	}
	for _, e := range term {
		if e.run == run {
			w.writeBits(e.bits, e.len)
			return
		}
	}
	// run<64 always has a terminating code.
}

// encodeG4 encodes rows of pixels (each []bool, true = black) as a Group 4 (T.6)
// stream using only horizontal and V0 modes — sufficient and always valid.
func encodeG4(rows [][]bool, columns int) []byte {
	w := &ccittBitWriter{}
	ref := []int{columns, columns} // imaginary white reference line
	for _, row := range rows {
		cur := changingElements(row, columns)
		encodeRow2D(w, ref, cur, columns)
		ref = cur
	}
	// EOFB: two EOL codes.
	w.writeBits(1, 12)
	w.writeBits(1, 12)
	return w.bytes()
}

// changingElements returns the ascending columns where colour flips, starting
// white, terminated by two sentinels at columns.
func changingElements(row []bool, columns int) []int {
	var ch []int
	white := true // pixel colour: false index 0 is white run
	for x := 0; x < columns; x++ {
		black := x < len(row) && row[x]
		isWhite := !black
		if isWhite != white {
			ch = append(ch, x)
			white = isWhite
		}
	}
	ch = append(ch, columns, columns)
	return ch
}

// encodeRow2D encodes one line using horizontal/vertical modes against ref.
func encodeRow2D(w *ccittBitWriter, ref, cur []int, columns int) {
	a0 := -1
	white := true
	ci := 0 // index into cur's real changing elements
	for a0 < columns {
		// next changing element a1 on current line strictly > a0
		a1 := columns
		if ci < len(cur) {
			a1 = cur[ci]
		}
		b1, b2 := findB1B2(ref, a0, white, columns)
		switch {
		case b2 < a1:
			// Pass mode.
			w.writeBits(0b0001, 4)
			a0 = b2
		case abs(a1-b1) <= 3:
			// Vertical mode.
			d := a1 - b1
			switch d {
			case 0:
				w.writeBits(0b1, 1)
			case 1:
				w.writeBits(0b011, 3)
			case 2:
				w.writeBits(0b000011, 6)
			case 3:
				w.writeBits(0b0000011, 7)
			case -1:
				w.writeBits(0b010, 3)
			case -2:
				w.writeBits(0b000010, 6)
			case -3:
				w.writeBits(0b0000010, 7)
			}
			a0 = a1
			white = !white
			ci++
		default:
			// Horizontal mode: emit two runs.
			start := a0
			if start < 0 {
				start = 0
			}
			a2 := columns
			if ci+1 < len(cur) {
				a2 = cur[ci+1]
			}
			w.writeBits(0b001, 3) // horizontal mode
			w.writeRun(a1-start, white)
			w.writeRun(a2-a1, !white)
			a0 = a2
			ci += 2
		}
	}
}

// encodeG3_1D encodes rows as Group 3 1D (modified Huffman), each row prefixed
// by an EOL code.
func encodeG3_1D(rows [][]bool, columns int) []byte {
	w := &ccittBitWriter{}
	for _, row := range rows {
		w.writeBits(1, 12) // EOL
		ch := changingElements(row, columns)
		prev := 0
		white := true
		for _, c := range ch {
			if c >= columns && prev >= columns {
				break
			}
			run := c - prev
			if c > columns {
				run = columns - prev
			}
			w.writeRun(run, white)
			prev = c
			white = !white
			if prev >= columns {
				break
			}
		}
	}
	w.writeBits(1, 12) // trailing EOL terminates
	return w.bytes()
}

// encodeG3_2D encodes rows as Group 3 2D (K>0): each row is EOL + tag bit, with
// the first row coded 1D (tag 1) and the rest 2D (tag 0) against the prior line.
func encodeG3_2D(rows [][]bool, columns int) []byte {
	w := &ccittBitWriter{}
	ref := []int{columns, columns}
	for i, row := range rows {
		w.writeBits(1, 12) // EOL
		cur := changingElements(row, columns)
		if i == 0 {
			w.writeBits(1, 1) // 1D tag
			prev := 0
			white := true
			for _, c := range cur {
				if c >= columns && prev >= columns {
					break
				}
				run := c - prev
				if c > columns {
					run = columns - prev
				}
				w.writeRun(run, white)
				prev = c
				white = !white
				if prev >= columns {
					break
				}
			}
		} else {
			w.writeBits(0, 1) // 2D tag
			encodeRow2D(w, ref, cur, columns)
		}
		ref = cur
	}
	w.writeBits(1, 12) // trailing EOL
	return w.bytes()
}

// --- Helpers to compare decoded packed output to expected pixels --------------

func unpackRows(data []byte, columns, rows int, blackIs1 bool) [][]bool {
	rowBytes := (columns + 7) / 8
	out := make([][]bool, 0, rows)
	for r := 0; r < rows; r++ {
		line := make([]bool, columns)
		base := r * rowBytes
		for x := 0; x < columns; x++ {
			byteIdx := base + x>>3
			var bit byte
			if byteIdx < len(data) {
				bit = (data[byteIdx] >> uint(7-(x&7))) & 1
			}
			// default (blackIs1=false): bit 0 = black. So black = (bit==0).
			black := bit == 0
			if blackIs1 {
				black = bit == 1
			}
			line[x] = black
		}
		out = append(out, line)
	}
	return out
}

func eqRows(a, b [][]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

// --- Tests --------------------------------------------------------------------

func sampleImage() ([][]bool, int) {
	// 8 columns x 4 rows. Patterns chosen to exercise white/black runs, vertical
	// and horizontal modes.
	const cols = 8
	B, W := true, false
	rows := [][]bool{
		{W, W, B, B, B, W, W, W}, // a middle black bar
		{W, W, B, B, B, W, W, W}, // identical -> vertical modes
		{B, B, B, B, B, B, B, B}, // all black
		{W, W, W, W, W, W, W, W}, // all white
	}
	return rows, cols
}

func TestCCITTGroup4RoundTrip(t *testing.T) {
	rows, cols := sampleImage()
	enc := encodeG4(rows, cols)

	k := -1
	got, err := Decode(enc, []Stage{{
		Name:   "CCITTFaxDecode",
		Params: Params{CCITT: &CCITTParams{K: k, Columns: cols, Rows: len(rows)}},
	}})
	if err != nil {
		t.Fatalf("G4 decode: %v", err)
	}
	dec := unpackRows(got, cols, len(rows), false)
	if !eqRows(dec, rows) {
		t.Fatalf("G4 round-trip mismatch:\n got %v\nwant %v", dec, rows)
	}
}

func TestCCITTGroup4Unbounded(t *testing.T) {
	// No /Rows: decoder must stop on EOFB.
	rows, cols := sampleImage()
	enc := encodeG4(rows, cols)
	got, err := Decode(enc, []Stage{{
		Name:   "CCITTFaxDecode",
		Params: Params{CCITT: &CCITTParams{K: -1, Columns: cols}},
	}})
	if err != nil {
		t.Fatalf("G4 unbounded decode: %v", err)
	}
	dec := unpackRows(got, cols, len(rows), false)
	if !eqRows(dec, rows) {
		t.Fatalf("G4 unbounded mismatch:\n got %v\nwant %v", dec, rows)
	}
}

func TestCCITTBlackIs1(t *testing.T) {
	rows, cols := sampleImage()
	enc := encodeG4(rows, cols)
	got, err := Decode(enc, []Stage{{
		Name:   "CCITTFaxDecode",
		Params: Params{CCITT: &CCITTParams{K: -1, Columns: cols, Rows: len(rows), BlackIs1: true}},
	}})
	if err != nil {
		t.Fatalf("G4 BlackIs1 decode: %v", err)
	}
	dec := unpackRows(got, cols, len(rows), true)
	if !eqRows(dec, rows) {
		t.Fatalf("G4 BlackIs1 mismatch:\n got %v\nwant %v", dec, rows)
	}
}

func TestCCITTGroup3_1D(t *testing.T) {
	rows, cols := sampleImage()
	enc := encodeG3_1D(rows, cols)
	got, err := Decode(enc, []Stage{{
		Name:   "CCITTFaxDecode",
		Params: Params{CCITT: &CCITTParams{K: 0, Columns: cols, Rows: len(rows)}},
	}})
	if err != nil {
		t.Fatalf("G3 1D decode: %v", err)
	}
	dec := unpackRows(got, cols, len(rows), false)
	if !eqRows(dec, rows) {
		t.Fatalf("G3 1D mismatch:\n got %v\nwant %v", dec, rows)
	}
}

func TestCCITTGroup3_2D(t *testing.T) {
	rows, cols := sampleImage()
	enc := encodeG3_2D(rows, cols)
	got, err := Decode(enc, []Stage{{
		Name:   "CCITTFaxDecode",
		Params: Params{CCITT: &CCITTParams{K: 2, Columns: cols, Rows: len(rows)}},
	}})
	if err != nil {
		t.Fatalf("G3 2D decode: %v", err)
	}
	dec := unpackRows(got, cols, len(rows), false)
	if !eqRows(dec, rows) {
		t.Fatalf("G3 2D mismatch:\n got %v\nwant %v", dec, rows)
	}
}

func TestCCITTWideImageMakeupCodes(t *testing.T) {
	// A wide row with a long black run forces makeup codes.
	const cols = 200
	row := make([]bool, cols)
	for x := 10; x < 180; x++ {
		row[x] = true
	}
	rows := [][]bool{row}
	enc := encodeG4(rows, cols)
	got, err := Decode(enc, []Stage{{
		Name:   "CCITTFaxDecode",
		Params: Params{CCITT: &CCITTParams{K: -1, Columns: cols, Rows: 1}},
	}})
	if err != nil {
		t.Fatalf("wide G4 decode: %v", err)
	}
	dec := unpackRows(got, cols, 1, false)
	if !eqRows(dec, rows) {
		t.Fatalf("wide G4 mismatch")
	}
}

func TestCCITTEncodedByteAlign(t *testing.T) {
	rows, cols := sampleImage()
	w := &ccittBitWriter{}
	ref := []int{cols, cols}
	for _, row := range rows {
		cur := changingElements(row, cols)
		encodeRow2D(w, ref, cur, cols)
		ref = cur
		w.align() // byte-align after each row
	}
	w.writeBits(1, 12)
	w.writeBits(1, 12)
	enc := w.bytes()

	got, err := Decode(enc, []Stage{{
		Name: "CCITTFaxDecode",
		Params: Params{CCITT: &CCITTParams{
			K: -1, Columns: cols, Rows: len(rows), EncodedByteAlign: true,
		}},
	}})
	if err != nil {
		t.Fatalf("byte-align decode: %v", err)
	}
	dec := unpackRows(got, cols, len(rows), false)
	if !eqRows(dec, rows) {
		t.Fatalf("byte-align mismatch:\n got %v\nwant %v", dec, rows)
	}
}

func TestCCITTTruncatedNoPanic(t *testing.T) {
	rows, cols := sampleImage()
	enc := encodeG4(rows, cols)
	// Truncate mid-stream and require Rows so the decoder runs past the data.
	trunc := enc[:len(enc)/2]
	_, err := Decode(trunc, []Stage{{
		Name:   "CCITTFaxDecode",
		Params: Params{CCITT: &CCITTParams{K: -1, Columns: cols, Rows: len(rows) + 10}},
	}})
	if err == nil {
		t.Fatal("expected error for truncated G4 stream")
	}
}

func TestCCITTGarbageNoPanic(t *testing.T) {
	garbage := []byte{0xFF, 0xFF, 0xFF, 0xAA, 0x55, 0x00, 0x13, 0x37}
	// Should not panic; either decodes some rows or errors.
	_, _ = Decode(garbage, []Stage{{
		Name:   "CCITTFaxDecode",
		Params: Params{CCITT: &CCITTParams{K: -1, Columns: 16, Rows: 100}},
	}})
}

func TestCCITTDefaultsNoParams(t *testing.T) {
	// With no CCITT params, default Columns=1728 is used; feed an EOFB so the
	// decoder ends cleanly with zero rows -> error (no rows decoded), no panic.
	w := &ccittBitWriter{}
	w.writeBits(1, 12)
	w.writeBits(1, 12)
	_, err := Decode(w.bytes(), []Stage{{Name: "CCITTFaxDecode"}})
	if err == nil {
		t.Fatal("expected 'no rows decoded' error")
	}
}
