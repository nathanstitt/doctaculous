package heif

import (
	"encoding/binary"
	"fmt"
)

// ISOBMFF box scanning. The whole file is held in memory (HEIF metadata is
// scattered and iloc extents use absolute file offsets, so random access is
// required); every box payload is a bounded subslice of that buffer, and all
// field reads go through cursor, which can never read out of bounds.

// boxSpan is one parsed box header: its four-character type and payload bytes
// (header excluded). uuid boxes carry the 16-byte usertype at the front of the
// payload slice; callers that care can strip it.
type boxSpan struct {
	typ     string
	payload []byte
}

// scanBoxes splits data into consecutive boxes. It understands 32-bit sizes,
// 64-bit largesize (size==1), and size==0 ("extends to end of enclosing
// container"). A box that overruns data is a truncation error.
func scanBoxes(data []byte) ([]boxSpan, error) {
	var out []boxSpan
	off := 0
	for off < len(data) {
		if len(data)-off < 8 {
			return nil, fmt.Errorf("%w: truncated box header at offset %d", ErrInvalid, off)
		}
		size := int64(binary.BigEndian.Uint32(data[off:]))
		typ := string(data[off+4 : off+8])
		hdr := int64(8)
		switch size {
		case 0:
			size = int64(len(data) - off)
		case 1:
			if len(data)-off < 16 {
				return nil, fmt.Errorf("%w: truncated 64-bit box header at offset %d", ErrInvalid, off)
			}
			large := binary.BigEndian.Uint64(data[off+8:])
			if large < 16 || large > uint64(len(data)-off) {
				return nil, fmt.Errorf("%w: bad 64-bit box size %d at offset %d", ErrInvalid, large, off)
			}
			size = int64(large)
			hdr = 16
		default:
			if size < 8 {
				return nil, fmt.Errorf("%w: bad box size %d at offset %d", ErrInvalid, size, off)
			}
		}
		if size > int64(len(data)-off) {
			return nil, fmt.Errorf("%w: box %q overruns file (size %d at offset %d)", ErrInvalid, typ, size, off)
		}
		out = append(out, boxSpan{typ: typ, payload: data[off+int(hdr) : off+int(size)]})
		off += int(size)
	}
	return out, nil
}

// findBox returns the first box of the given type, or ok=false.
func findBox(boxes []boxSpan, typ string) (boxSpan, bool) {
	for _, b := range boxes {
		if b.typ == typ {
			return b, true
		}
	}
	return boxSpan{}, false
}

// fullBox splits a FullBox payload into version, flags, and the remaining
// bytes.
func fullBox(payload []byte) (version byte, flags uint32, rest []byte, err error) {
	if len(payload) < 4 {
		return 0, 0, nil, fmt.Errorf("%w: truncated FullBox header", ErrInvalid)
	}
	v := binary.BigEndian.Uint32(payload)
	return byte(v >> 24), v & 0x00ffffff, payload[4:], nil
}

// cursor is a bounds-checked big-endian field reader with a sticky error:
// after the first out-of-range read every subsequent read returns zero values
// and the error is reported once via err(). This keeps multi-field box
// parsers linear instead of threading an error through every read.
type cursor struct {
	b       []byte
	off     int
	failed  bool
	context string
}

func newCursor(b []byte, context string) *cursor {
	return &cursor{b: b, context: context}
}

func (c *cursor) err() error {
	if c.failed {
		return fmt.Errorf("%w: truncated %s box", ErrInvalid, c.context)
	}
	return nil
}

func (c *cursor) remaining() int { return len(c.b) - c.off }

func (c *cursor) take(n int) []byte {
	if c.failed || n < 0 || c.remaining() < n {
		c.failed = true
		return nil
	}
	s := c.b[c.off : c.off+n]
	c.off += n
	return s
}

func (c *cursor) u8() uint8 {
	s := c.take(1)
	if s == nil {
		return 0
	}
	return s[0]
}

func (c *cursor) u16() uint16 {
	s := c.take(2)
	if s == nil {
		return 0
	}
	return binary.BigEndian.Uint16(s)
}

func (c *cursor) u32() uint32 {
	s := c.take(4)
	if s == nil {
		return 0
	}
	return binary.BigEndian.Uint32(s)
}

// uint reads an unsigned big-endian integer of size bytes (0, 4, or 8 in
// practice; any 0..8 accepted). size 0 reads nothing and returns 0, matching
// iloc's "field not present" encoding.
func (c *cursor) uint(size int) uint64 {
	if size == 0 {
		return 0
	}
	if size > 8 {
		c.failed = true
		return 0
	}
	s := c.take(size)
	if s == nil {
		return 0
	}
	var v uint64
	for _, by := range s {
		v = v<<8 | uint64(by)
	}
	return v
}

func (c *cursor) fourcc() string {
	s := c.take(4)
	if s == nil {
		return ""
	}
	return string(s)
}

// cstring reads a null-terminated UTF-8 string. A missing terminator consumes
// the rest of the buffer (some writers omit the final NUL on the last field).
func (c *cursor) cstring() string {
	if c.failed {
		return ""
	}
	for i := c.off; i < len(c.b); i++ {
		if c.b[i] == 0 {
			s := string(c.b[c.off:i])
			c.off = i + 1
			return s
		}
	}
	s := string(c.b[c.off:])
	c.off = len(c.b)
	return s
}
