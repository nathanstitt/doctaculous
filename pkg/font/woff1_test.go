package font

import (
	"os"
	"reflect"
	"testing"
)

func TestDecodeWOFF1RoundTrips(t *testing.T) {
	ttf, err := os.ReadFile(fixturePath(t, "webfont.ttf"))
	if err != nil {
		t.Fatalf("read ttf: %v", err)
	}
	woff, err := os.ReadFile(fixturePath(t, "webfont.woff"))
	if err != nil {
		t.Fatalf("read woff: %v", err)
	}
	// The WOFF must decode to a Face whose 'A' outline matches the bare TTF's 'A'.
	bare, err := LoadSFNT(ttf)
	if err != nil {
		t.Fatalf("LoadSFNT(ttf): %v", err)
	}
	got, err := LoadSFNT(woff)
	if err != nil {
		t.Fatalf("LoadSFNT(woff): %v", err)
	}
	assertSameGlyph(t, bare, got, 'A')
}

// assertSameGlyph fails unless rune r decodes to the same advance AND the exact
// same outline geometry in both faces. It is the round-trip ground-truth check
// shared by the WOFF1/WOFF2 tests: comparing the full segment list (not just
// outline presence) is what proves a container/transform decoder reconstructed the
// real coordinates, so a wrong triplet or sfnt-rebuild can't slip past.
func assertSameGlyph(t *testing.T, want, got *Face, r rune) {
	t.Helper()
	wOut, wAdv, wOK := want.Glyph(r)
	gOut, gAdv, gOK := got.Glyph(r)
	if wOK != gOK || wAdv != gAdv {
		t.Fatalf("glyph %q mismatch: want ok=%v adv=%v, got ok=%v adv=%v", r, wOK, wAdv, gOK, gAdv)
	}
	if (wOut == nil) != (gOut == nil) {
		t.Fatalf("glyph %q outline presence differs: want nil=%v, got nil=%v", r, wOut == nil, gOut == nil)
	}
	if wOut != nil && !reflect.DeepEqual(wOut.Segments, gOut.Segments) {
		t.Fatalf("glyph %q outline geometry differs:\n want %d segs: %+v\n got  %d segs: %+v",
			r, len(wOut.Segments), wOut.Segments, len(gOut.Segments), gOut.Segments)
	}
}

func TestDecodeWOFF1RejectsTruncated(t *testing.T) {
	_, err := decodeWOFF1([]byte("wOFF\x00\x01")) // signature only, header truncated
	if err == nil {
		t.Fatal("decodeWOFF1(truncated) = nil error, want a typed error")
	}
}
