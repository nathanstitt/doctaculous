package omnidoc

import (
	"bytes"
	"context"
	"errors"
	"image"
	"strings"
	"testing"
	"time"
)

// cancelDoc is a small but non-trivial document: enough blocks and inline text
// that layout does real work, without making the happy-path tests slow.
func cancelDoc() []byte {
	var b strings.Builder
	b.WriteString("<html><body>")
	for i := range 200 {
		b.WriteString("<p style=\"color:black\">paragraph ")
		b.WriteString(strings.Repeat("word ", 20))
		if i%3 == 0 {
			b.WriteString("<span style=\"font-weight:bold\">emphasis</span>")
		}
		b.WriteString("</p>")
	}
	b.WriteString("</body></html>")
	return []byte(b.String())
}

// hugeParagraph is a single block child holding a very large inline run. It is
// the shape that the between-block-children check CANNOT catch: one paragraph is
// one child, so cancellation only works if layoutInline checks per line. It is
// sized so that laying it out takes hundreds of milliseconds — enough to fire a
// cancel partway through and tell that apart from a run to completion.
//
// The repeat count is deliberately no larger than that: the mid-layout test lays
// this out once uncancelled to get its baseline, and at 20000 repeats that
// single layout peaked at ~18 GB RSS under -race, over a CI runner's memory. At
// 500 it is ~600 ms and a few hundred MB, still 30x the test's 20 ms floor.
func hugeParagraph() []byte {
	return []byte("<html><body><p style=\"color:black\">" +
		strings.Repeat("wordy text that must be broken into many many lines ", 500) +
		"</p></body></html>")
}

// TestOpenHTMLBytesContextCancelledBeforeStart: a ctx already cancelled when the
// open begins must fail rather than laying the document out to completion.
func TestOpenHTMLBytesContextCancelledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	doc, err := OpenHTMLBytesContext(ctx, cancelDoc(), WithBundledFonts())
	if err == nil {
		t.Fatalf("OpenHTMLBytesContext with a pre-cancelled ctx returned a document, want an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want one wrapping context.Canceled", err)
	}
	if doc != nil {
		t.Errorf("document = %v, want nil alongside the error", doc)
	}
}

// TestOpenHTMLBytesContextDeadlineExceeded: a deadline that expires during layout
// must surface as context.DeadlineExceeded, not Canceled and not a partial
// document. The huge single paragraph guarantees layout is still running when the
// (already-past) deadline is consulted.
func TestOpenHTMLBytesContextDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := OpenHTMLBytesContext(ctx, hugeParagraph(), WithBundledFonts())
	if err == nil {
		t.Fatalf("OpenHTMLBytesContext with an expired deadline returned a document, want an error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want one wrapping context.DeadlineExceeded", err)
	}
}

// TestOpenHTMLBytesContextCancelMidLayout is the test that actually proves the
// per-line check in layoutInline earns its place: a single enormous paragraph is
// ONE block child, so the pre-existing between-children check never fires. The
// ctx is cancelled from another goroutine after layout is underway, and the open
// must return promptly instead of running to completion.
func TestOpenHTMLBytesContextCancelMidLayout(t *testing.T) {
	data := hugeParagraph()

	// Baseline: how long does this document take to lay out uncancelled? The
	// mid-layout cancel must come back in materially less than this, otherwise
	// "it cancelled" would be indistinguishable from "it finished".
	start := time.Now()
	if _, err := OpenHTMLBytes(data, WithBundledFonts()); err != nil {
		t.Fatalf("uncancelled baseline open: %v", err)
	}
	full := time.Since(start)
	if full < 20*time.Millisecond {
		t.Skipf("baseline layout too fast (%v) to time a mid-layout cancel meaningfully", full)
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel a short way into the layout — long enough that work has definitely
	// begun, short enough that a run-to-completion would be obvious.
	cancelAt := full / 10
	go func() {
		time.Sleep(cancelAt)
		cancel()
	}()

	start = time.Now()
	_, err := OpenHTMLBytesContext(ctx, data, WithBundledFonts())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("open cancelled mid-layout returned a document, want an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want one wrapping context.Canceled", err)
	}
	// The open must stop SHORTLY after the cancel, not merely before the full
	// layout would have finished. Half the baseline is a deliberately loose bound
	// (CI machines are noisy and the checks are strided, not per-rune), but it is
	// tight enough to fail if any single uninterruptible phase — shaping, in
	// particular, which runs before line breaking — stops honoring the context.
	if budget := cancelAt + full/2; elapsed > budget {
		t.Errorf("cancelled open took %v (cancel fired at %v, full layout %v); want under %v — a phase is running to completion without checking ctx",
			elapsed, cancelAt, full, budget)
	}
	t.Logf("baseline layout %v; cancel fired at %v; open returned after %v", full, cancelAt, elapsed)
}

// TestRasterizePageCancelled covers the bug this change fixes: renderPage used to
// take its ctx as `_`, so RasterizePage advertised cancellation it never
// delivered. Both the pre-cancelled and the expired-deadline forms must error.
func TestRasterizePageCancelled(t *testing.T) {
	doc, err := OpenHTMLBytes(cancelDoc(), WithBundledFonts())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{
			name: "cancelled",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			want: context.DeadlineExceeded,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			defer cancel()

			img, err := doc.RasterizePage(ctx, 0, RasterOptions{DPI: 96})
			if err == nil {
				t.Fatalf("RasterizePage returned an image, want an error")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want one wrapping %v", err, tc.want)
			}
			if img != nil {
				t.Errorf("image = %v, want nil alongside the error", img)
			}
		})
	}
}

// TestRasterizePagesCancelled: the batch fan-out reports the context error for
// every page rather than returning images.
func TestRasterizePagesCancelled(t *testing.T) {
	doc, err := OpenHTMLBytes(cancelDoc(), WithBundledFonts(), WithPageSize(LetterWidthPt, LetterHeightPt))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if doc.PageCount() < 2 {
		t.Fatalf("PageCount = %d, want a multi-page document to exercise the fan-out", doc.PageCount())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, res := range doc.RasterizePages(ctx, doc.AllPages(), RasterOptions{DPI: 96}) {
		if res.Err == nil {
			t.Errorf("page %d: err = nil, want a context error", res.Index)
			continue
		}
		if !errors.Is(res.Err, context.Canceled) {
			t.Errorf("page %d: err = %v, want one wrapping context.Canceled", res.Index, res.Err)
		}
	}
}

// TestLiveContextNeverSpuriouslyCancels is the happy path: a live ctx (and a
// deadline with ample headroom) must render normally. A cancellation check that
// fires when it should not would be far worse than one that never fires.
func TestLiveContextNeverSpuriouslyCancels(t *testing.T) {
	data := cancelDoc()

	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{"background", func() (context.Context, context.CancelFunc) {
			return context.Background(), func() {}
		}},
		{"live cancel", func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		}},
		{"generous deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Minute)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			defer cancel()

			doc, err := OpenHTMLBytesContext(ctx, data, WithBundledFonts())
			if err != nil {
				t.Fatalf("open with a live ctx: %v", err)
			}
			img, err := doc.RasterizePage(ctx, 0, RasterOptions{DPI: 96})
			if err != nil {
				t.Fatalf("rasterize with a live ctx: %v", err)
			}
			if img == nil {
				t.Fatal("image = nil, want a rendered page")
			}
			if b := img.Bounds(); b.Dx() <= 0 || b.Dy() <= 0 {
				t.Fatalf("image bounds = %v, want a non-empty page", b)
			}
		})
	}
}

// TestOpenHTMLBytesContextMatchesOpenHTMLBytes pins the hard compatibility
// requirement: threading a live context must not change a single pixel or a
// single page dimension versus the historical no-ctx entry point. This is what
// makes the change safe for every existing caller.
func TestOpenHTMLBytesContextMatchesOpenHTMLBytes(t *testing.T) {
	data := cancelDoc()

	plain, err := OpenHTMLBytes(data, WithBundledFonts(), WithPageSize(LetterWidthPt, LetterHeightPt))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	withCtx, err := OpenHTMLBytesContext(context.Background(), data, WithBundledFonts(), WithPageSize(LetterWidthPt, LetterHeightPt))
	if err != nil {
		t.Fatalf("OpenHTMLBytesContext: %v", err)
	}

	if plain.PageCount() != withCtx.PageCount() {
		t.Fatalf("PageCount: plain %d, ctx %d", plain.PageCount(), withCtx.PageCount())
	}

	for i := range plain.PageCount() {
		a, err := plain.RasterizePage(context.Background(), i, RasterOptions{DPI: 96})
		if err != nil {
			t.Fatalf("plain page %d: %v", i, err)
		}
		b, err := withCtx.RasterizePage(context.Background(), i, RasterOptions{DPI: 96})
		if err != nil {
			t.Fatalf("ctx page %d: %v", i, err)
		}
		if a.Bounds() != b.Bounds() {
			t.Fatalf("page %d bounds: plain %v, ctx %v", i, a.Bounds(), b.Bounds())
		}
		// Compare the raw pixel buffers: identical bytes, not merely similar.
		ra, ok := a.(*image.RGBA)
		if !ok {
			t.Fatalf("page %d: plain image is %T, want *image.RGBA", i, a)
		}
		rb, ok := b.(*image.RGBA)
		if !ok {
			t.Fatalf("page %d: ctx image is %T, want *image.RGBA", i, b)
		}
		if !bytes.Equal(ra.Pix, rb.Pix) {
			t.Fatalf("page %d: pixels differ between OpenHTMLBytes and OpenHTMLBytesContext", i)
		}
	}
}
