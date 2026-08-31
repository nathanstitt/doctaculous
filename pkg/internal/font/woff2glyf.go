package font

import (
	"encoding/binary"
	"fmt"
)

// WOFF2 transformed-glyf reconstruction (W3C WOFF2 §5.1 "Transformed glyf table"
// and §5.2 coordinate triplet encoding). A transformed glyf table replaces the
// raw sfnt glyf+loca with a header followed by seven (optionally eight)
// back-to-back sub-streams; reconstructGlyf rebuilds a standard sfnt glyf table
// (and the matching loca) so the downstream truetype parser sees ordinary glyph
// records. The decoding is byte-for-byte faithful to the spec and the well-known
// fontTools reference (`woff2._decodeTriplets`), so outlines round-trip exactly.

// standard composite-glyph component flags (OpenType "glyf" spec) used to walk a
// composite glyph's variable-length component list in the compositeStream.
const (
	compArg1And2AreWords   = 0x0001
	compWeHaveAScale       = 0x0008
	compMoreComponents     = 0x0020
	compWeHaveXAndYScale   = 0x0040
	compWeHaveTwoByTwo     = 0x0080
	compWeHaveInstructions = 0x0100
)

// standard simple-glyph point flags emitted into the rebuilt sfnt glyph.
const (
	sfntOnCurve      = 0x01
	sfntXShortVector = 0x02
	sfntYShortVector = 0x04
	sfntXSameOrPos   = 0x10 // if X is short: positive sign; if X is long: same as previous (dx=0)
	sfntYSameOrPos   = 0x20
)

// read255UInt16 decodes a WOFF2 255UInt16 (variable-length uint16) from the front
// of b, returning the value and the number of bytes consumed (W3C WOFF2 §3.1).
// Lead bytes: 253 => two big-endian bytes follow; 254 => one byte + 506; 255 =>
// one byte + 253; 0..252 => the literal value.
func read255UInt16(b []byte) (uint16, int, error) {
	const (
		wordCode         = 253
		oneMoreByteCode2 = 254 // + 506 (== 2*253)
		oneMoreByteCode1 = 255 // + 253
	)
	if len(b) < 1 {
		return 0, 0, fmt.Errorf("%w: 255UInt16 truncated", ErrInvalidWOFF)
	}
	switch code := b[0]; code {
	case wordCode:
		if len(b) < 3 {
			return 0, 0, fmt.Errorf("%w: 255UInt16 word truncated", ErrInvalidWOFF)
		}
		return uint16(b[1])<<8 | uint16(b[2]), 3, nil
	case oneMoreByteCode2:
		if len(b) < 2 {
			return 0, 0, fmt.Errorf("%w: 255UInt16 +506 truncated", ErrInvalidWOFF)
		}
		return uint16(b[1]) + 506, 2, nil
	case oneMoreByteCode1:
		if len(b) < 2 {
			return 0, 0, fmt.Errorf("%w: 255UInt16 +253 truncated", ErrInvalidWOFF)
		}
		return uint16(b[1]) + 253, 2, nil
	default:
		return uint16(code), 1, nil
	}
}

// withSign returns mag when positive, else -mag. mag is a non-negative magnitude.
func withSign(positive bool, mag int) int {
	if positive {
		return mag
	}
	return -mag
}

// decodeTriplet decodes one simple-glyph point delta (dx, dy) from a flag byte's
// low 7 bits plus up to four data bytes taken from the front of p, returning the
// deltas and the number of data bytes consumed (W3C WOFF2 §5.2). This is a direct
// port of the fontTools reference decoder. The sign convention: the X delta is
// positive when bit 0 of flag is set, the Y delta positive when bit 1 is set.
func decodeTriplet(flag byte, p []byte) (dx, dy, n int, err error) {
	f := int(flag & 0x7f)
	// nBytes = data bytes consumed (the spec's "Byte Count" minus the flag byte).
	var nBytes int
	switch {
	case f < 84:
		nBytes = 1
	case f < 120:
		nBytes = 2
	case f < 124:
		nBytes = 3
	default:
		nBytes = 4
	}
	if len(p) < nBytes {
		return 0, 0, 0, fmt.Errorf("%w: glyphStream truncated decoding point", ErrInvalidWOFF)
	}
	xPos := f&1 != 0 // X positive when bit 0 set
	yPos := f&2 != 0 // Y positive when bit 1 set
	switch {
	case f < 10: // dy only, 1 data byte, 8-bit Y, x=0
		dx = 0
		dy = withSign(xPos, ((f&14)<<7)+int(p[0]))
	case f < 20: // dx only, 1 data byte, 8-bit X, y=0
		dx = withSign(xPos, (((f-10)&14)<<7)+int(p[0]))
		dy = 0
	case f < 84: // 1 data byte, 4-bit X and 4-bit Y
		b0 := f - 20
		b1 := int(p[0])
		dx = withSign(xPos, 1+(b0&0x30)+(b1>>4))
		dy = withSign(yPos, 1+((b0&0x0c)<<2)+(b1&0x0f))
	case f < 120: // 2 data bytes, 8-bit X and 8-bit Y
		b0 := f - 84
		dx = withSign(xPos, 1+((b0/12)<<8)+int(p[0]))
		dy = withSign(yPos, 1+(((b0%12)>>2)<<8)+int(p[1]))
	case f < 124: // 3 data bytes, 12-bit X and 12-bit Y
		b2 := int(p[1])
		dx = withSign(xPos, (int(p[0])<<4)+(b2>>4))
		dy = withSign(yPos, ((b2&0x0f)<<8)+int(p[2]))
	default: // 4 data bytes, 16-bit X and 16-bit Y
		dx = withSign(xPos, (int(p[0])<<8)+int(p[1]))
		dy = withSign(yPos, (int(p[2])<<8)+int(p[3]))
	}
	return dx, dy, nBytes, nil
}

// glyfStreams holds the seven decoded sub-streams of a transformed glyf table,
// each advanced independently as glyphs are reconstructed.
type glyfStreams struct {
	nContour    []byte // numGlyphs × int16
	nPoints     []byte // 255UInt16 per contour, simple glyphs only
	flags       []byte // 1 byte per simple-glyph point
	glyph       []byte // triplet coordinate data + per-glyph instructionLength
	composite   []byte // raw component records for composite glyphs
	bbox        []byte // bbox bitmap + explicit bbox values
	instruction []byte // instruction bytecode for all glyphs that have it
}

// reconstructGlyf parses a WOFF2 transformed glyf table and rebuilds a standard
// sfnt glyf table plus its matching loca table. Returns the two table bodies.
// Malformed input yields an ErrInvalidWOFF (never a panic).
func reconstructGlyf(transformed []byte) (glyf, loca []byte, err error) {
	const headerLen = 36
	if len(transformed) < headerLen {
		return nil, nil, fmt.Errorf("%w: transformed glyf header truncated", ErrInvalidWOFF)
	}
	optionFlags := binary.BigEndian.Uint16(transformed[2:])
	numGlyphs := int(binary.BigEndian.Uint16(transformed[4:]))
	indexFormat := binary.BigEndian.Uint16(transformed[6:])
	sizes := [7]uint32{
		binary.BigEndian.Uint32(transformed[8:]),  // nContour
		binary.BigEndian.Uint32(transformed[12:]), // nPoints
		binary.BigEndian.Uint32(transformed[16:]), // flag
		binary.BigEndian.Uint32(transformed[20:]), // glyph
		binary.BigEndian.Uint32(transformed[24:]), // composite
		binary.BigEndian.Uint32(transformed[28:]), // bbox
		binary.BigEndian.Uint32(transformed[32:]), // instruction
	}

	off := headerLen
	subs := make([][]byte, 7)
	for i, sz := range sizes {
		end := off + int(sz)
		if end < off || end > len(transformed) {
			return nil, nil, fmt.Errorf("%w: transformed glyf sub-stream %d out of range", ErrInvalidWOFF, i)
		}
		subs[i] = transformed[off:end]
		off = end
	}
	s := glyfStreams{
		nContour:    subs[0],
		nPoints:     subs[1],
		flags:       subs[2],
		glyph:       subs[3],
		composite:   subs[4],
		bbox:        subs[5],
		instruction: subs[6],
	}
	if len(s.nContour) < numGlyphs*2 {
		return nil, nil, fmt.Errorf("%w: nContourStream too short", ErrInvalidWOFF)
	}

	// The bbox sub-stream begins with a bitmap of 4*ceil(numGlyphs/32) bytes, one
	// bit per glyph (glyph 0 = MSB of byte 0), flagging glyphs with an explicit
	// bbox; the flagged glyphs' bbox values (4 × int16) follow in order.
	bboxBitmapLen := 4 * ((numGlyphs + 31) / 32)
	if len(s.bbox) < bboxBitmapLen {
		return nil, nil, fmt.Errorf("%w: bboxStream bitmap truncated", ErrInvalidWOFF)
	}
	bboxBitmap := s.bbox[:bboxBitmapLen]
	bboxValues := s.bbox[bboxBitmapLen:]

	// The optional overlapSimpleBitmap (one bit per glyph, same packing) trails the
	// instruction stream when optionFlags bit 0 is set. It carries the per-glyph
	// OVERLAP_SIMPLE hint; we only need to confirm it is present and in bounds.
	if optionFlags&0x0001 != 0 {
		if off+bboxBitmapLen > len(transformed) {
			return nil, nil, fmt.Errorf("%w: overlapSimpleBitmap truncated", ErrInvalidWOFF)
		}
	}

	var glyfBuf []byte
	offsets := make([]uint32, numGlyphs+1)
	for gid := 0; gid < numGlyphs; gid++ {
		nc := int(int16(binary.BigEndian.Uint16(s.nContour[gid*2:])))
		var rec []byte
		switch {
		case nc == 0:
			rec = nil // empty glyph: no data, loca[gid]==loca[gid+1]
		case nc > 0:
			rec, err = s.reconstructSimpleGlyph(gid, nc, bboxBitmap, &bboxValues)
		default: // nc < 0 (== -1): composite glyph
			rec, err = s.reconstructCompositeGlyph(gid, bboxBitmap, &bboxValues)
		}
		if err != nil {
			return nil, nil, err
		}
		offsets[gid] = uint32(len(glyfBuf))
		glyfBuf = append(glyfBuf, rec...)
		// Each glyph record is padded to a 2-byte boundary in the sfnt glyf table.
		if len(glyfBuf)%2 != 0 {
			glyfBuf = append(glyfBuf, 0)
		}
	}
	offsets[numGlyphs] = uint32(len(glyfBuf))

	loca = buildLoca(offsets, indexFormat)
	return glyfBuf, loca, nil
}

// reconstructSimpleGlyph rebuilds one simple-glyph sfnt record (nc > 0 contours):
// endPtsOfContours, instructionLength + instructions, point flags (emitted
// uncompressed), and delta-encoded x/y coordinates, with the bounding box taken
// from the bbox sub-stream when flagged or computed from the points otherwise.
func (s *glyfStreams) reconstructSimpleGlyph(gid, nc int, bboxBitmap []byte, bboxValues *[]byte) ([]byte, error) {
	// Per-contour point counts (255UInt16) → cumulative endPtsOfContours.
	endPts := make([]uint16, nc)
	total := 0
	for c := 0; c < nc; c++ {
		v, n, err := read255UInt16(s.nPoints)
		if err != nil {
			return nil, err
		}
		s.nPoints = s.nPoints[n:]
		total += int(v)
		if total > 0xFFFF {
			return nil, fmt.Errorf("%w: simple glyph point count overflow", ErrInvalidWOFF)
		}
		endPts[c] = uint16(total - 1)
	}
	nPoints := total

	if len(s.flags) < nPoints {
		return nil, fmt.Errorf("%w: flagStream truncated", ErrInvalidWOFF)
	}
	pointFlags := s.flags[:nPoints]
	s.flags = s.flags[nPoints:]

	// Decode point deltas from the glyph stream into absolute coordinates.
	xs := make([]int, nPoints)
	ys := make([]int, nPoints)
	onCurve := make([]bool, nPoints)
	x, y := 0, 0
	for i := 0; i < nPoints; i++ {
		fb := pointFlags[i]
		onCurve[i] = fb&0x80 == 0
		dx, dy, n, err := decodeTriplet(fb, s.glyph)
		if err != nil {
			return nil, err
		}
		s.glyph = s.glyph[n:]
		x += dx
		y += dy
		xs[i] = x
		ys[i] = y
	}

	// instructionLength (255UInt16 from glyphStream) + that many instruction bytes
	// copied from the instruction stream.
	instrLen, n, err := read255UInt16(s.glyph)
	if err != nil {
		return nil, err
	}
	s.glyph = s.glyph[n:]
	if len(s.instruction) < int(instrLen) {
		return nil, fmt.Errorf("%w: instructionStream truncated", ErrInvalidWOFF)
	}
	instructions := s.instruction[:instrLen]
	s.instruction = s.instruction[instrLen:]

	xMin, yMin, xMax, yMax, err := s.glyphBBox(gid, bboxBitmap, bboxValues, xs, ys, nPoints)
	if err != nil {
		return nil, err
	}

	return emitSimpleGlyph(nc, endPts, instructions, pointFlags, xs, ys, xMin, yMin, xMax, yMax), nil
}

// glyphBBox returns the glyph's bounding box: the explicit value from the bbox
// sub-stream when this glyph's bitmap bit is set, otherwise computed from the
// decoded points (an all-zero box for a point-less glyph). It consumes 4 int16
// from bboxValues when an explicit box is present.
func (s *glyfStreams) glyphBBox(gid int, bboxBitmap []byte, bboxValues *[]byte, xs, ys []int, nPoints int) (xMin, yMin, xMax, yMax int16, err error) {
	if bboxBitSet(bboxBitmap, gid) {
		bv := *bboxValues
		if len(bv) < 8 {
			return 0, 0, 0, 0, fmt.Errorf("%w: bboxStream values truncated", ErrInvalidWOFF)
		}
		xMin = int16(binary.BigEndian.Uint16(bv[0:]))
		yMin = int16(binary.BigEndian.Uint16(bv[2:]))
		xMax = int16(binary.BigEndian.Uint16(bv[4:]))
		yMax = int16(binary.BigEndian.Uint16(bv[6:]))
		*bboxValues = bv[8:]
		return xMin, yMin, xMax, yMax, nil
	}
	if nPoints == 0 {
		return 0, 0, 0, 0, nil
	}
	loX, loY, hiX, hiY := xs[0], ys[0], xs[0], ys[0]
	for i := 1; i < nPoints; i++ {
		if xs[i] < loX {
			loX = xs[i]
		}
		if xs[i] > hiX {
			hiX = xs[i]
		}
		if ys[i] < loY {
			loY = ys[i]
		}
		if ys[i] > hiY {
			hiY = ys[i]
		}
	}
	return clampInt16(loX), clampInt16(loY), clampInt16(hiX), clampInt16(hiY), nil
}

// reconstructCompositeGlyph rebuilds one composite-glyph sfnt record. The
// component records in the compositeStream are byte-identical to standard sfnt
// composite components and are copied through verbatim; when any component has the
// WE_HAVE_INSTRUCTIONS flag, an instructionLength (255UInt16 from glyphStream)
// and that many instruction bytes (from the instruction stream) are appended. A
// composite glyph always carries an explicit bbox in the bbox sub-stream.
func (s *glyfStreams) reconstructCompositeGlyph(gid int, bboxBitmap []byte, bboxValues *[]byte) ([]byte, error) {
	start := s.composite
	haveInstructions := false
	consumed := 0
	for {
		if len(s.composite) < 4 {
			return nil, fmt.Errorf("%w: compositeStream truncated", ErrInvalidWOFF)
		}
		flags := binary.BigEndian.Uint16(s.composite[0:])
		// component = flags(2) + glyphIndex(2) + args + optional transform.
		compLen := 4
		if flags&compArg1And2AreWords != 0 {
			compLen += 4
		} else {
			compLen += 2
		}
		switch {
		case flags&compWeHaveAScale != 0:
			compLen += 2
		case flags&compWeHaveXAndYScale != 0:
			compLen += 4
		case flags&compWeHaveTwoByTwo != 0:
			compLen += 8
		}
		if len(s.composite) < compLen {
			return nil, fmt.Errorf("%w: compositeStream component truncated", ErrInvalidWOFF)
		}
		if flags&compWeHaveInstructions != 0 {
			haveInstructions = true
		}
		s.composite = s.composite[compLen:]
		consumed += compLen
		if flags&compMoreComponents == 0 {
			break
		}
	}
	components := start[:consumed]

	if !bboxBitSet(bboxBitmap, gid) {
		return nil, fmt.Errorf("%w: composite glyph %d missing explicit bbox", ErrInvalidWOFF, gid)
	}
	bv := *bboxValues
	if len(bv) < 8 {
		return nil, fmt.Errorf("%w: bboxStream values truncated", ErrInvalidWOFF)
	}
	bbox := bv[:8]
	*bboxValues = bv[8:]

	// numberOfContours(-1) + bbox + components [+ instructionLength + instructions].
	rec := make([]byte, 0, 10+len(components)+2)
	rec = appendInt16(rec, -1)
	rec = append(rec, bbox...)
	rec = append(rec, components...)
	if haveInstructions {
		instrLen, n, err := read255UInt16(s.glyph)
		if err != nil {
			return nil, err
		}
		s.glyph = s.glyph[n:]
		if len(s.instruction) < int(instrLen) {
			return nil, fmt.Errorf("%w: instructionStream truncated (composite)", ErrInvalidWOFF)
		}
		rec = appendUint16(rec, instrLen)
		rec = append(rec, s.instruction[:instrLen]...)
		s.instruction = s.instruction[instrLen:]
	}
	return rec, nil
}

// emitSimpleGlyph serializes a standard sfnt simple-glyph record. Point flags are
// emitted uncompressed (one byte per point, no repeat run-length), and each
// coordinate uses the short (1-byte) form when its delta fits in a signed byte,
// else the long (2-byte) form — the standard encoding the truetype parser reads.
func emitSimpleGlyph(nc int, endPts []uint16, instructions, pointFlags []byte, xs, ys []int, xMin, yMin, xMax, yMax int16) []byte {
	nPoints := len(xs)
	buf := make([]byte, 0, 10+nc*2+2+len(instructions)+nPoints*5)
	buf = appendInt16(buf, int16(nc))
	buf = appendInt16(buf, xMin)
	buf = appendInt16(buf, yMin)
	buf = appendInt16(buf, xMax)
	buf = appendInt16(buf, yMax)
	for _, e := range endPts {
		buf = appendUint16(buf, e)
	}
	buf = appendUint16(buf, uint16(len(instructions)))
	buf = append(buf, instructions...)

	// Build per-point flag bytes and the x/y delta encodings together so the flag's
	// short/sign bits agree with the coordinate stream that follows.
	flagsOut := make([]byte, nPoints)
	var xCoords, yCoords []byte
	prevX, prevY := 0, 0
	for i := 0; i < nPoints; i++ {
		var fb byte
		if pointFlags[i]&0x80 == 0 {
			fb |= sfntOnCurve
		}
		dx := xs[i] - prevX
		dy := ys[i] - prevY
		prevX, prevY = xs[i], ys[i]

		switch {
		case dx == 0:
			fb |= sfntXSameOrPos // long form, X unchanged: set SAME, omit bytes
		case dx >= -255 && dx <= 255:
			fb |= sfntXShortVector
			if dx > 0 {
				fb |= sfntXSameOrPos // short form positive
				xCoords = append(xCoords, byte(dx))
			} else {
				xCoords = append(xCoords, byte(-dx))
			}
		default:
			xCoords = appendInt16(xCoords, int16(dx))
		}

		switch {
		case dy == 0:
			fb |= sfntYSameOrPos
		case dy >= -255 && dy <= 255:
			fb |= sfntYShortVector
			if dy > 0 {
				fb |= sfntYSameOrPos
				yCoords = append(yCoords, byte(dy))
			} else {
				yCoords = append(yCoords, byte(-dy))
			}
		default:
			yCoords = appendInt16(yCoords, int16(dy))
		}
		flagsOut[i] = fb
	}
	buf = append(buf, flagsOut...)
	buf = append(buf, xCoords...)
	buf = append(buf, yCoords...)
	return buf
}

// buildLoca builds an sfnt loca table from per-glyph offsets. indexFormat 0 emits
// the short form (uint16 of offset/2, valid because every record is 2-byte
// aligned); indexFormat 1 emits the long form (uint32).
func buildLoca(offsets []uint32, indexFormat uint16) []byte {
	if indexFormat == 0 {
		out := make([]byte, 0, len(offsets)*2)
		for _, o := range offsets {
			out = appendUint16(out, uint16(o/2))
		}
		return out
	}
	out := make([]byte, 0, len(offsets)*4)
	for _, o := range offsets {
		out = appendUint32(out, o)
	}
	return out
}

// bboxBitSet reports whether glyph gid's bit is set in the bbox bitmap (glyph 0 is
// the most-significant bit of byte 0).
func bboxBitSet(bitmap []byte, gid int) bool {
	byteIdx := gid / 8
	if byteIdx >= len(bitmap) {
		return false
	}
	return bitmap[byteIdx]&(0x80>>(uint(gid)%8)) != 0
}

// clampInt16 saturates an int to the int16 range (defensive: real glyph coordinates
// are well within range, but a malformed stream must not wrap).
func clampInt16(v int) int16 {
	switch {
	case v > 32767:
		return 32767
	case v < -32768:
		return -32768
	default:
		return int16(v)
	}
}

func appendUint16(b []byte, v uint16) []byte { return append(b, byte(v>>8), byte(v)) }
func appendInt16(b []byte, v int16) []byte   { return appendUint16(b, uint16(v)) }
func appendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
