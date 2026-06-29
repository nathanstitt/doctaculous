package font

import (
	"errors"
	"testing"
)

// TestRead255UInt16 covers the four WOFF2 255UInt16 forms (W3C WOFF2 §3.1) plus a
// truncated word.
func TestRead255UInt16(t *testing.T) {
	cases := []struct {
		name    string
		in      []byte
		want    uint16
		wantN   int
		wantErr bool
	}{
		{"literal", []byte{42}, 42, 1, false},
		{"word 253", []byte{253, 0x12, 0x34}, 0x1234, 3, false},
		{"plus253 255", []byte{255, 10}, 263, 2, false}, // 253 + 10
		{"plus506 254", []byte{254, 10}, 516, 2, false}, // 506 + 10
		{"truncated word", []byte{253, 0x12}, 0, 0, true},
		{"empty", []byte{}, 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, n, err := read255UInt16(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("read255UInt16(%v) = (%d, %d, nil), want error", tc.in, got, n)
				}
				if !errors.Is(err, ErrInvalidWOFF) {
					t.Fatalf("error = %v, want ErrInvalidWOFF", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("read255UInt16(%v) error: %v", tc.in, err)
			}
			if got != tc.want || n != tc.wantN {
				t.Fatalf("read255UInt16(%v) = (%d, %d), want (%d, %d)", tc.in, got, n, tc.want, tc.wantN)
			}
		})
	}
}

// TestDecodeTriplet checks one representative flag in each byte-count class (W3C
// WOFF2 §5.2): the data-byte count per class, the decoded (dx, dy) magnitudes
// (verified against the fontTools reference decoder this code ports), and the
// X-sign(bit0)/Y-sign(bit1) convention. Flags 0/10/20/84/120/124 have both sign
// bits clear, so their deltas are negative (the spec's default "−" sign rows).
func TestDecodeTriplet(t *testing.T) {
	cases := []struct {
		name           string
		flag           byte
		data           []byte
		wantDx, wantDy int
		wantN          int
	}{
		// f<10: dy only (1 byte), dx=0, both signs clear -> dy negative.
		{"flag0 dy-only", 0, []byte{5}, 0, -5, 1},
		// f<20: dx only (1 byte), dy=0.
		{"flag10 dx-only", 10, []byte{7}, -7, 0, 1},
		// f<84: 4-bit x + 4-bit y (1 byte); flag 20, data 0 -> dx=dy=-(1+0+0)=-1.
		{"flag20 4bit", 20, []byte{0x00}, -1, -1, 1},
		// f<120: 8-bit x + 8-bit y (2 bytes); flag 84, data {3,4} -> -(1+3), -(1+4).
		{"flag84 8bit", 84, []byte{3, 4}, -4, -5, 2},
		// f<124: 12-bit x + 12-bit y (3 bytes); flag 120, data {0x01,0x23,0x45}:
		// dx = -((0x01<<4) + (0x23>>4)) = -(16+2) = -18
		// dy = -(((0x23&0x0f)<<8) + 0x45) = -((3<<8)+0x45) = -837
		{"flag120 12bit", 120, []byte{0x01, 0x23, 0x45}, -18, -837, 3},
		// else: 16-bit x + 16-bit y (4 bytes); flag 124, data {0x01,0x02,0x03,0x04}:
		// dx = -0x0102 = -258, dy = -0x0304 = -772.
		{"flag124 16bit", 124, []byte{0x01, 0x02, 0x03, 0x04}, -258, -772, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dx, dy, n, err := decodeTriplet(tc.flag, tc.data)
			if err != nil {
				t.Fatalf("decodeTriplet(%d, %v) error: %v", tc.flag, tc.data, err)
			}
			if n != tc.wantN {
				t.Fatalf("byte count = %d, want %d", n, tc.wantN)
			}
			if dx != tc.wantDx || dy != tc.wantDy {
				t.Fatalf("decodeTriplet(%d, %v) = (dx=%d, dy=%d), want (dx=%d, dy=%d)",
					tc.flag, tc.data, dx, dy, tc.wantDx, tc.wantDy)
			}
		})
	}
}

// TestDecodeTripletSignBits verifies bit0 (X sign) and bit1 (Y sign) flip the
// delta sign. Flag 3 (= flag 0 + both sign bits, still the dy-only class) makes
// the Y delta positive; the high bits ((f&14)<<7) also fold into the magnitude.
func TestDecodeTripletSignBits(t *testing.T) {
	// flag 3: f<10 dy-only class, X-sign bit set but dx is forced 0, Y-sign set.
	// dy = +(((3&14)<<7) + p[0]) = +((2<<7) + 9) = +(256 + 9) = 265.
	dx, dy, n, err := decodeTriplet(3, []byte{9})
	if err != nil {
		t.Fatalf("decodeTriplet(3): %v", err)
	}
	if dx != 0 || dy != 265 || n != 1 {
		t.Fatalf("decodeTriplet(3, {9}) = (dx=%d, dy=%d, n=%d), want (0, 265, 1)", dx, dy, n)
	}

	// flag 13: f<20 dx-only class with X-sign bit (bit0) set -> positive dx.
	// dx = +((((13-10)&14)<<7) + p[0]) = +((2<<7) + 1) = +257; dy = 0.
	dx, dy, n, err = decodeTriplet(13, []byte{1})
	if err != nil {
		t.Fatalf("decodeTriplet(13): %v", err)
	}
	if dx != 257 || dy != 0 || n != 1 {
		t.Fatalf("decodeTriplet(13, {1}) = (dx=%d, dy=%d, n=%d), want (257, 0, 1)", dx, dy, n)
	}

	// flag 23: f<84 4-bit class with both sign bits set -> positive dx and dy.
	// b0 = 23-20 = 3; data 0x00 -> dx = +(1+(3&0x30)+0) = 1, dy = +(1+((3&0x0c)<<2)+0) = 1.
	dx, dy, n, err = decodeTriplet(23, []byte{0x00})
	if err != nil {
		t.Fatalf("decodeTriplet(23): %v", err)
	}
	if dx != 1 || dy != 1 || n != 1 {
		t.Fatalf("decodeTriplet(23, {0}) = (dx=%d, dy=%d, n=%d), want (1, 1, 1)", dx, dy, n)
	}
}

// TestDecodeTripletTruncated proves each byte-count class errors (not panics) when
// the data slice is one byte short.
func TestDecodeTripletTruncated(t *testing.T) {
	cases := []struct {
		name string
		flag byte
		data []byte // one byte short for the class
	}{
		{"1-byte class empty", 0, []byte{}},
		{"2-byte class short", 84, []byte{1}},
		{"3-byte class short", 120, []byte{1, 2}},
		{"4-byte class short", 124, []byte{1, 2, 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := decodeTriplet(tc.flag, tc.data)
			if err == nil {
				t.Fatalf("decodeTriplet(%d, %v) = nil error, want truncation error", tc.flag, tc.data)
			}
			if !errors.Is(err, ErrInvalidWOFF) {
				t.Fatalf("error = %v, want ErrInvalidWOFF", err)
			}
		})
	}
}
