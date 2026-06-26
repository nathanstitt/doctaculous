package font

import "testing"

func TestDecodeWOFF2RejectsBadSignature(t *testing.T) {
	_, err := decodeWOFF2([]byte("wOF2\x00\x00")) // signature only
	if err == nil {
		t.Fatal("decodeWOFF2(truncated) = nil error, want a typed error")
	}
}

func TestUIntBase128(t *testing.T) {
	// Two base-128 groups: 0xFF 0x08 => (0x7F<<7)|0x08.
	got, n, err := readUIntBase128([]byte{0xFF, 0x08})
	if err != nil {
		t.Fatalf("readUIntBase128: %v", err)
	}
	if got != (0x7F<<7)|0x08 || n != 2 {
		t.Fatalf("readUIntBase128 = %d (n=%d), want %d (n=2)", got, n, (0x7F<<7)|0x08)
	}
}
