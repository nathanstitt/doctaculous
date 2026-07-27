package heif

import (
	"encoding/binary"
	"errors"
	"testing"
)

func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

func TestScanBoxes(t *testing.T) {
	box := func(typ string, payload []byte) []byte {
		out := be32(uint32(8 + len(payload)))
		out = append(out, typ...)
		return append(out, payload...)
	}
	data := append(box("ftyp", []byte("heicABCD")), box("mdat", []byte{1, 2, 3})...)
	boxes, err := scanBoxes(data)
	if err != nil {
		t.Fatalf("scanBoxes: %v", err)
	}
	if len(boxes) != 2 || boxes[0].typ != "ftyp" || boxes[1].typ != "mdat" {
		t.Fatalf("got %+v", boxes)
	}
	if string(boxes[0].payload) != "heicABCD" || len(boxes[1].payload) != 3 {
		t.Fatalf("payloads wrong: %q %v", boxes[0].payload, boxes[1].payload)
	}
}

func TestScanBoxesLargesize(t *testing.T) {
	// size==1 with 64-bit largesize.
	payload := []byte{9, 8, 7}
	data := be32(1)
	data = append(data, "mdat"...)
	data = binary.BigEndian.AppendUint64(data, uint64(16+len(payload)))
	data = append(data, payload...)
	boxes, err := scanBoxes(data)
	if err != nil {
		t.Fatalf("scanBoxes: %v", err)
	}
	if len(boxes) != 1 || boxes[0].typ != "mdat" || len(boxes[0].payload) != 3 {
		t.Fatalf("got %+v", boxes)
	}
}

func TestScanBoxesSizeZero(t *testing.T) {
	// size==0 extends to end of buffer.
	data := be32(0)
	data = append(data, "mdat"...)
	data = append(data, []byte{1, 2, 3, 4, 5}...)
	boxes, err := scanBoxes(data)
	if err != nil {
		t.Fatalf("scanBoxes: %v", err)
	}
	if len(boxes) != 1 || len(boxes[0].payload) != 5 {
		t.Fatalf("got %+v", boxes)
	}
}

func TestScanBoxesMalformed(t *testing.T) {
	cases := map[string][]byte{
		"truncated header": {0, 0, 0},
		"size too small":   append(be32(4), "mdat"...),
		"overruns buffer":  append(be32(100), "mdat"...),
		"truncated 64-bit": append(be32(1), "mdat"...),
		"bad 64-bit size":  append(append(be32(1), "mdat"...), binary.BigEndian.AppendUint64(nil, 8)...),
		"64-bit overruns":  append(append(be32(1), "mdat"...), binary.BigEndian.AppendUint64(nil, 1<<40)...),
	}
	for name, data := range cases {
		if _, err := scanBoxes(data); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
}

func TestCursorStickyError(t *testing.T) {
	c := newCursor([]byte{0x12, 0x34}, "test")
	if v := c.u16(); v != 0x1234 {
		t.Fatalf("u16 = %#x", v)
	}
	if v := c.u32(); v != 0 {
		t.Fatalf("overrun u32 = %#x, want 0", v)
	}
	// After failure every read keeps returning zero and err() reports once.
	if v := c.u8(); v != 0 {
		t.Fatalf("post-failure u8 = %d", v)
	}
	if err := c.err(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestCursorUint(t *testing.T) {
	c := newCursor([]byte{0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 7}, "test")
	if v := c.uint(4); v != 5 {
		t.Fatalf("uint(4) = %d", v)
	}
	if v := c.uint(0); v != 0 {
		t.Fatalf("uint(0) = %d", v)
	}
	if v := c.uint(8); v != 7 {
		t.Fatalf("uint(8) = %d", v)
	}
	if err := c.err(); err != nil {
		t.Fatalf("err = %v", err)
	}
	if c.uint(9); c.err() == nil {
		t.Fatal("uint(9) should fail")
	}
}

func TestCursorCString(t *testing.T) {
	c := newCursor([]byte("abc\x00def"), "test")
	if s := c.cstring(); s != "abc" {
		t.Fatalf("cstring = %q", s)
	}
	// Missing terminator consumes the rest.
	if s := c.cstring(); s != "def" {
		t.Fatalf("cstring = %q", s)
	}
	if c.remaining() != 0 {
		t.Fatalf("remaining = %d", c.remaining())
	}
}
