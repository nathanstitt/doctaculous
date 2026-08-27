package inline

import (
	"context"
	"reflect"
	"strings"
	"testing"

	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

// TestShapeContextCancelledReturnsEarly: shaping a large run under an
// already-cancelled context must stop early rather than shaping every rune.
// Shaping runs BEFORE line breaking, so without this check the CSS engine's
// per-line cancellation cannot fire until the whole paragraph is shaped.
func TestShapeContextCancelledReturnsEarly(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	run := Run{Text: strings.Repeat("shaped text ", 5000), Family: "serif", SizePt: 12}

	full := Shape(faces, []Run{run}, nil)
	if len(full) == 0 {
		t.Fatal("uncancelled Shape produced no glyphs")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := ShapeContext(ctx, faces, []Run{run}, nil)

	if len(got) >= len(full) {
		t.Fatalf("cancelled ShapeContext produced %d glyphs, want fewer than the full %d", len(got), len(full))
	}
	// The stride bounds how much work happens after cancellation: at most one
	// stride of runes, so a fully-cancelled shape must not get anywhere near the
	// whole run.
	if len(got) > shapeCancelStride {
		t.Errorf("cancelled ShapeContext produced %d glyphs, want at most one stride (%d)", len(got), shapeCancelStride)
	}
}

// TestShapeContextLiveMatchesShape is the compatibility pin: with a live context
// ShapeContext must produce exactly what the historical Shape produces, glyph for
// glyph. Shape itself delegates here, so a divergence would change every
// document's rendering.
func TestShapeContextLiveMatchesShape(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	runs := []Run{
		{Text: "Hello world", Family: "serif", SizePt: 12},
		{Break: true},
		{Text: "second\tline", Family: "monospace", SizePt: 10, WhiteSpace: "pre"},
		{Text: "مرحبا", Family: "serif", SizePt: 14},
	}

	want := Shape(faces, runs, nil)
	got := ShapeContext(context.Background(), faces, runs, nil)

	if len(got) != len(want) {
		t.Fatalf("glyph count = %d, want %d", len(got), len(want))
	}
	// DeepEqual rather than ==: a Glyph carries a []rune, so it is not comparable.
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("glyph %d differs:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}
