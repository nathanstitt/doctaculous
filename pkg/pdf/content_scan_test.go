package pdf

import (
	"bytes"
	"testing"
)

// TestReadInlineImage parses a BI...ID...EI inline image: the scanner must
// return the (abbreviated-key) dict and the exact raw sample bytes between the
// single whitespace after ID and the EI delimiter, then leave the cursor
// positioned to continue with following operators.
func TestReadInlineImage(t *testing.T) {
	// 2x2 1-component 8bpc image: 4 bytes of samples. The data deliberately
	// contains a byte (0x45='E', 0x49='I') so a naive "search for EI" without
	// length awareness could stop early — we require the proper delimiter rule.
	samples := []byte{0x00, 0x45, 0x49, 0xFF}
	src := []byte("BI /W 2 /H 2 /CS /G /BPC 8 ID ")
	src = append(src, samples...)
	src = append(src, []byte(" EI\nQ")...) // trailing operator to prove resync

	s := NewContentScanner(src)

	// First token must be the BI operator.
	_, op, ok, err := s.Next()
	if err != nil || !ok || op != "BI" {
		t.Fatalf("first token: op=%q ok=%v err=%v, want BI", op, ok, err)
	}

	dict, data, err := s.ReadInlineImage()
	if err != nil {
		t.Fatalf("ReadInlineImage: %v", err)
	}
	if w, _ := dict.GetInt("W"); w != 2 {
		t.Errorf("/W = %d, want 2 (dict=%v)", w, dict)
	}
	if h, _ := dict.GetInt("H"); h != 2 {
		t.Errorf("/H = %d, want 2", h)
	}
	if !bytes.Equal(data, samples) {
		t.Errorf("data = % x, want % x", data, samples)
	}

	// The cursor must resync to the trailing Q operator.
	_, op, ok, err = s.Next()
	if err != nil || !ok || op != "Q" {
		t.Fatalf("after EI: op=%q ok=%v err=%v, want Q", op, ok, err)
	}
}

// TestReadInlineImageMissingEI confirms a truncated inline image (no EI) returns
// an error rather than panicking or looping — the graceful-degradation contract.
func TestReadInlineImageMissingEI(t *testing.T) {
	src := []byte("BI /W 1 /H 1 /CS /G /BPC 8 ID \x00") // data but never terminated
	s := NewContentScanner(src)
	if _, _, _, err := s.Next(); err != nil { // consume BI
		t.Fatalf("Next(BI): %v", err)
	}
	if _, _, err := s.ReadInlineImage(); err == nil {
		t.Error("expected error for inline image missing EI, got nil")
	}
}

// TestReadInlineImageMissingID confirms a BI with no ID terminator errors out
// rather than consuming the rest of the stream as dict pairs.
func TestReadInlineImageMissingID(t *testing.T) {
	src := []byte("BI /W 1 /H 1") // dict never closed by ID
	s := NewContentScanner(src)
	if _, _, _, err := s.Next(); err != nil {
		t.Fatalf("Next(BI): %v", err)
	}
	if _, _, err := s.ReadInlineImage(); err == nil {
		t.Error("expected error for inline image missing ID, got nil")
	}
}

// GetInt is a small dict helper for the test (Dict has no method here); define a
// local accessor to keep the test self-contained.
func (d Dict) GetInt(key string) (int, bool) {
	if v, ok := d[Name(key)].(Integer); ok {
		return int(v), true
	}
	return 0, false
}
