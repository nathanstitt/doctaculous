package filter

import (
	"errors"
	"fmt"
)

// ErrCCITT is the sentinel wrapped by all CCITTFaxDecode errors, so callers can
// branch on a malformed fax stream.
var ErrCCITT = errors.New("ccittfax")

// ccittDecode decodes a CCITTFaxDecode stream into packed 1-bit-per-pixel sample
// rows (MSB-first within each byte), ready to be consumed as a 1-bpc bilevel
// image. It honors /K (K<0 = Group 4/T.6, K=0 = Group 3 1D, K>0 = Group 3 2D),
// /Columns, /Rows, /BlackIs1, /EncodedByteAlign and /EndOfBlock.
//
// The decoder builds, for each row, a list of "changing element" positions (the
// pixel columns where colour flips), starting from an imaginary all-white row.
// From those it packs the bilevel output: by default a black pixel is bit 0 and
// white is bit 1 (PDF /BlackIs1 = false); /BlackIs1 = true inverts that.
//
// Malformed or truncated input never panics: it returns an error wrapping
// ErrCCITT. Whatever complete rows were decoded before the fault are discarded
// (the caller treats a filter error as an undecodable stream).
func ccittDecode(data []byte, p Params) ([]byte, error) {
	cp := p.CCITT
	if cp == nil {
		// No DecodeParms: apply the PDF defaults for a CCITT stream.
		cp = &CCITTParams{Columns: 1728, EndOfBlock: true}
	}
	columns := cp.Columns
	if columns <= 0 {
		columns = 1728
	}
	if columns > 1<<20 {
		return nil, fmt.Errorf("%w: implausible /Columns %d", ErrCCITT, columns)
	}

	d := &ccittDecoder{
		br:       bitReaderMSB{data: data},
		columns:  columns,
		rows:     cp.Rows,
		k:        cp.K,
		byteAlin: cp.EncodedByteAlign,
		eob:      cp.EndOfBlock,
	}
	return d.decode(cp.BlackIs1)
}

type ccittDecoder struct {
	br       bitReaderMSB
	columns  int
	rows     int
	k        int
	byteAlin bool
	eob      bool
}

// decode runs the row loop and returns packed 1-bpp output.
func (d *ccittDecoder) decode(blackIs1 bool) ([]byte, error) {
	rowBytes := (d.columns + 7) / 8
	var out []byte
	// ref is the reference line's changing elements; the first row references an
	// imaginary all-white line (a single change at column count).
	ref := []int{d.columns, d.columns}

	rowsDecoded := 0
	for d.rows <= 0 || rowsDecoded < d.rows {
		if d.byteAlin {
			d.br.align()
		}
		// Detect end-of-data / EOFB before attempting a row when unbounded.
		if d.rows <= 0 && d.atEnd() {
			break
		}

		cur, err := d.decodeRow(ref)
		if err != nil {
			if errors.Is(err, errCCITTEnd) {
				break
			}
			return nil, err
		}
		out = append(out, packRow(cur, d.columns, rowBytes, blackIs1)...)
		ref = cur
		rowsDecoded++
	}

	if rowsDecoded == 0 {
		return nil, fmt.Errorf("%w: no rows decoded", ErrCCITT)
	}
	return out, nil
}

// errCCITTEnd signals a clean end of image (EOFB / out of data at a row boundary).
var errCCITTEnd = errors.New("ccitt end")

// atEnd reports whether the bitstream is exhausted or sitting on an EOFB pattern,
// at a point where no further row should be decoded.
func (d *ccittDecoder) atEnd() bool {
	if d.br.exhausted() {
		return true
	}
	// EOFB for G4 is two EOL (000000000001) codes; a single EOL (used in G3) is
	// also treated as a terminator here. Peek without consuming on failure.
	save := d.br
	if d.peekEOL() {
		// Consume any run of EOL codes (and the second half of EOFB).
		for d.peekEOL() {
			d.consumeEOL()
		}
		return true
	}
	d.br = save
	return false
}

// peekEOL reports whether the next bits form an EOL code (eleven 0s then a 1)
// without consuming them.
func (d *ccittDecoder) peekEOL() bool {
	save := d.br
	zeros := 0
	for {
		b, ok := d.br.bit()
		if !ok {
			d.br = save
			return false
		}
		if b == 0 {
			zeros++
			if zeros > 64 { // runaway
				d.br = save
				return false
			}
			continue
		}
		d.br = save
		return zeros >= 11
	}
}

func (d *ccittDecoder) consumeEOL() {
	for {
		b, ok := d.br.bit()
		if !ok {
			return
		}
		if b == 1 {
			return
		}
	}
}

// decodeRow decodes one scan line against the reference line ref, returning the
// current line's changing elements (terminated by two sentinels at d.columns).
func (d *ccittDecoder) decodeRow(ref []int) ([]int, error) {
	// G3 1D rows may be prefixed by an EOL; skip it.
	if d.k == 0 {
		d.skipEOL()
		return d.decode1D(ref)
	}
	if d.k < 0 {
		return d.decode2D(ref)
	}
	// K>0 (G3 2D): each row begins with an EOL followed by a 1-bit tag selecting
	// 1D (1) or 2D (0) coding for that row.
	d.skipEOL()
	tag, ok := d.br.bit()
	if !ok {
		return nil, errCCITTEnd
	}
	if tag == 1 {
		return d.decode1D(ref)
	}
	return d.decode2D(ref)
}

// skipEOL consumes a single leading EOL code if present.
func (d *ccittDecoder) skipEOL() {
	if d.peekEOL() {
		d.consumeEOL()
	}
}

// decode1D decodes a one-dimensional (modified Huffman) scan line. ref is unused
// but accepted so the row dispatch is uniform.
func (d *ccittDecoder) decode1D(_ []int) ([]int, error) {
	var changes []int
	a0 := 0
	white := true // each line starts with a white run
	for a0 < d.columns {
		run, err := d.readRun(white)
		if err != nil {
			return nil, err
		}
		a0 += run
		if a0 > d.columns {
			a0 = d.columns
		}
		changes = append(changes, a0)
		white = !white
	}
	changes = append(changes, d.columns, d.columns)
	return changes, nil
}

// decode2D decodes a two-dimensional (modified READ, T.6) scan line against ref.
func (d *ccittDecoder) decode2D(ref []int) ([]int, error) {
	var changes []int
	a0 := -1
	white := true
	for a0 < d.columns {
		mode, err := d.readMode()
		if err != nil {
			return nil, err
		}
		b1, b2 := findB1B2(ref, a0, white, d.columns)
		switch mode {
		case modePass:
			// a0 advances to b2; colour unchanged, no changing element recorded.
			a0 = b2
		case modeHorizontal:
			// Two runs of the current then opposite colour.
			start := a0
			if start < 0 {
				start = 0
			}
			r1, err := d.readRun(white)
			if err != nil {
				return nil, err
			}
			r2, err := d.readRun(!white)
			if err != nil {
				return nil, err
			}
			a1 := clampCol(start+r1, d.columns)
			a2 := clampCol(a1+r2, d.columns)
			changes = append(changes, a1, a2)
			a0 = a2
		case modeV0, modeVR1, modeVR2, modeVR3, modeVL1, modeVL2, modeVL3:
			a1 := b1 + verticalOffset(mode)
			a1 = clampCol(a1, d.columns)
			if a1 < 0 {
				a1 = 0
			}
			changes = append(changes, a1)
			a0 = a1
			white = !white
		case modeEOL:
			return nil, errCCITTEnd
		default:
			return nil, fmt.Errorf("%w: bad 2D mode", ErrCCITT)
		}
		if len(changes) > 2*d.columns+4 {
			return nil, fmt.Errorf("%w: 2D row overflow", ErrCCITT)
		}
	}
	changes = append(changes, d.columns, d.columns)
	return changes, nil
}

func clampCol(v, cols int) int {
	if v > cols {
		return cols
	}
	if v < 0 {
		return 0
	}
	return v
}

// findB1B2 locates b1 (the first changing element on the reference line to the
// right of a0 with colour opposite to a0's) and b2 (the next change after b1).
// ref holds changing-element columns in ascending order; the colour to the left
// of ref[0] is white, so ref[i] flips to colour (i even ⇒ becomes black).
func findB1B2(ref []int, a0 int, a0White bool, cols int) (b1, b2 int) {
	// ref[i] is the column of the i-th colour transition on the reference line,
	// counting from an all-white start: even i is a transition into black, odd i
	// a transition into white. b1 is the first transition strictly right of a0
	// whose colour is opposite to a0 — i.e. a transition into black when a0 is
	// white (even i), into white when a0 is black (odd i).
	wantEven := a0White
	i := 0
	for i < len(ref) && ref[i] <= a0 {
		i++
	}
	// Advance i to the first change > a0 with the required parity.
	if (i%2 == 0) != wantEven {
		i++
	}
	if i < len(ref) {
		b1 = ref[i]
	} else {
		b1 = cols
	}
	if i+1 < len(ref) {
		b2 = ref[i+1]
	} else {
		b2 = cols
	}
	if b1 > cols {
		b1 = cols
	}
	if b2 > cols {
		b2 = cols
	}
	return b1, b2
}

// packRow packs a line's changing elements into rowBytes of MSB-first 1-bpp
// output. changes is ascending column positions where colour flips, starting
// white. With blackIs1 false, white pixels are 1 bits and black pixels 0 bits.
func packRow(changes []int, cols, rowBytes int, blackIs1 bool) []byte {
	row := make([]byte, rowBytes)
	// whiteBit / blackBit are the output bit values for each colour.
	var whiteBit, blackBit byte = 1, 0
	if blackIs1 {
		whiteBit, blackBit = 0, 1
	}
	col := 0
	white := true
	for _, c := range changes {
		if c > cols {
			c = cols
		}
		bit := whiteBit
		if !white {
			bit = blackBit
		}
		if bit == 1 {
			for x := col; x < c; x++ {
				row[x>>3] |= 0x80 >> uint(x&7)
			}
		}
		col = c
		white = !white
		if col >= cols {
			break
		}
	}
	// Any trailing pixels keep the last colour already written (white default = 1).
	if col < cols {
		bit := whiteBit
		if !white {
			bit = blackBit
		}
		if bit == 1 {
			for x := col; x < cols; x++ {
				row[x>>3] |= 0x80 >> uint(x&7)
			}
		}
	}
	return row
}

// verticalOffset maps a vertical mode to its signed column offset from b1.
func verticalOffset(mode int) int {
	switch mode {
	case modeV0:
		return 0
	case modeVR1:
		return 1
	case modeVR2:
		return 2
	case modeVR3:
		return 3
	case modeVL1:
		return -1
	case modeVL2:
		return -2
	case modeVL3:
		return -3
	}
	return 0
}
