package omnidoc

import (
	"testing"
	"time"
)

// TestOpenHTMLBytesNonFiniteNoHang guards the README's central promise --
// "unsupported constructs degrade rather than crash" -- at the public entry
// point, for the class of input that broke it.
//
// Each of these hung forever before the non-finite rejection in pkg/css: an
// infinite flex-grow makes the free-space distribution loop in layout/css never
// converge. The hang happens inside OpenHTMLBytes, during layout, so it is not
// reachable by a RasterizePage context -- there is no deadline a caller could
// have set to escape it. The smallest reproducer was 66 bytes.
//
// The bound is generous on purpose: this asserts termination, not speed.
func TestOpenHTMLBytesNonFiniteNoHang(t *testing.T) {
	cases := []struct {
		name string
		html string
	}{
		{"flex-grow inf", `<div style="display:flex"><div style="flex-grow:inf">a</div></div>`},
		{"flex-grow infinity", `<div style="display:flex"><div style="flex-grow:infinity">a</div></div>`},
		{"flex-grow +Inf", `<div style="display:flex"><div style="flex-grow:+Inf">a</div></div>`},
		{"flex-grow nan", `<div style="display:flex"><div style="flex-grow:nan">a</div></div>`},
		{"flex-shrink inf", `<div style="display:flex"><div style="flex-shrink:inf">a</div></div>`},
		// Same overflow arriving as digits: the tokenizer never spells "inf",
		// but 400 nines still exceed float64 range.
		{"flex-grow digit overflow", `<div style="display:flex"><div style="flex-grow:` +
			"9999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999" +
			"9999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999" +
			"9999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999" +
			"9999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999" +
			`">a</div></div>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				// The return value does not matter; not returning does.
				_, _ = OpenHTMLBytes([]byte(c.html))
			}()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				// The layout loop cannot be interrupted, so the goroutine is
				// left running: fail the test and let the run tear it down.
				t.Fatal("OpenHTMLBytes did not return: the layout loop is not converging")
			}
		})
	}
}
