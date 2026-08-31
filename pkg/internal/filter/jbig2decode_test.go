package filter

import "testing"

// TestDecodeJBIG2Garbage: a non-JBIG2 / truncated payload must return an error, never
// panic (the vendored decoder is wrapped in a recover). This is the graceful-degradation
// contract; a valid-payload test lives in the jbig2 sub-package + the render goldens.
func TestDecodeJBIG2Garbage(t *testing.T) {
	_, err := DecodeJBIG2([]byte("not a jbig2 stream"), nil, 8, 8)
	if err == nil {
		t.Fatal("DecodeJBIG2 on garbage returned nil error; want an error")
	}
}

// TestDecodeJBIG2Empty: empty input errors cleanly (no panic).
func TestDecodeJBIG2Empty(t *testing.T) {
	if _, err := DecodeJBIG2(nil, nil, 8, 8); err == nil {
		t.Fatal("DecodeJBIG2(nil) returned nil error; want an error")
	}
}
