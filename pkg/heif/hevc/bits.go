package hevc

import "fmt"

// bitReader reads big-endian bit fields from an RBSP (already unescaped)
// buffer. It has a sticky error: after the first overrun every read returns
// zero and err() reports once, keeping the long parameter-set parsers linear.
type bitReader struct {
	b       []byte
	pos     int // bit position from the start of b
	failed  bool
	context string
}

func newBitReader(b []byte, context string) *bitReader {
	return &bitReader{b: b, context: context}
}

func (r *bitReader) err() error {
	if r.failed {
		return fmt.Errorf("%w: truncated %s", ErrInvalid, r.context)
	}
	return nil
}

// u reads an n-bit unsigned value (0 <= n <= 32).
func (r *bitReader) u(n int) uint32 {
	if r.failed || n < 0 || n > 32 {
		r.failed = true
		return 0
	}
	if r.pos+n > len(r.b)*8 {
		r.failed = true
		return 0
	}
	var v uint32
	for range n {
		byteIdx := r.pos >> 3
		bitIdx := 7 - r.pos&7
		v = v<<1 | uint32(r.b[byteIdx]>>bitIdx&1)
		r.pos++
	}
	return v
}

func (r *bitReader) flag() bool { return r.u(1) == 1 }

// ue reads an unsigned Exp-Golomb value. Values needing more than 32 leading
// zero bits are malformed.
func (r *bitReader) ue() uint32 {
	zeros := 0
	for {
		if r.failed {
			return 0
		}
		if r.pos >= len(r.b)*8 {
			r.failed = true
			return 0
		}
		if r.b[r.pos>>3]>>(7-r.pos&7)&1 == 1 {
			break
		}
		r.pos++
		zeros++
		if zeros > 31 {
			r.failed = true
			return 0
		}
	}
	r.pos++ // the terminating 1 bit
	if zeros == 0 {
		return 0
	}
	suffix := r.u(zeros)
	return (1<<zeros - 1) + suffix
}

// se reads a signed Exp-Golomb value.
func (r *bitReader) se() int32 {
	k := r.ue()
	if k%2 == 0 {
		return -int32(k / 2)
	}
	return int32(k/2 + 1)
}

// byteAligned reports whether the reader is at a byte boundary.
func (r *bitReader) byteAligned() bool { return r.pos&7 == 0 }

// byteOffset returns the current position in whole bytes (only valid when
// byte-aligned).
func (r *bitReader) byteOffset() int { return r.pos >> 3 }
