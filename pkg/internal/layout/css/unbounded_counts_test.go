package css

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nathanstitt/omnidoc/pkg/internal/html"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout/cssbox"
)

// TestUnboundedCountsTerminate is the regression guard for the counts a document
// can name but not afford. Each case below hung the engine indefinitely before
// the bounds landed: the table grid and the grid occupancy map both materialize
// one entry per covered slot, so an unbounded count in the markup is an
// unbounded allocation, not a large layout.
//
// The assertion is termination, not a particular geometry -- these are malformed
// documents and any finite answer is acceptable.
//
// The budget is deliberately not "whatever finishes eventually". Two of these
// cases do terminate unclamped and would pass any generous timeout while still
// being a denial of service: repeat(200000000, 1px) took 13.4s before the track
// ceiling, and 50 max-span cells took 64s before the rowspan clip. Every case
// here completes in under 0.1s once bounded, so a few seconds is ~2 orders of
// magnitude of headroom for a loaded CI runner while still failing loudly if a
// bound is removed.
func TestUnboundedCountsTerminate(t *testing.T) {
	const budget = 5 * time.Second

	manyCells := "<table><tr>" +
		strings.Repeat(`<td colspan="1000" rowspan="65534">x</td>`, 50) +
		"</tr></table>"

	cases := []struct {
		name string
		html string
	}{
		{"colspan", `<table><tr><td colspan="900000000">x</td></tr></table>`},
		{"rowspan", `<table><tr><td rowspan="900000000">x</td></tr></table>`},
		{"colspan and rowspan", `<table><tr><td colspan="900000000" rowspan="900000000">x</td></tr></table>`},
		{"colgroup span", `<table><colgroup span="900000000"></colgroup><tr><td>x</td></tr></table>`},
		// Every cell at the clamped maximum: the per-attribute clamp alone does
		// not bound this, because the cost is per cell. The rowspan clip to the
		// real row count is what makes it cheap.
		{"many cells at max span", manyCells},
		{"grid-row end line", `<div style="display:grid"><div style="grid-row: 1 / 500000000">x</div></div>`},
		{"grid-column end line", `<div style="display:grid"><div style="grid-column: 1 / 500000000">x</div></div>`},
		{"grid-row span", `<div style="display:grid"><div style="grid-row: span 500000000">x</div></div>`},
		{"negative grid line", `<div style="display:grid"><div style="grid-row: -500000000 / 1">x</div></div>`},
		{"repeat single track", `<div style="display:grid;grid-template-columns:repeat(200000000, 1px)"><div>x</div></div>`},
		{"repeat multi track", `<div style="display:grid;grid-template-columns:repeat(200000000, 1px 2px 3px)"><div>x</div></div>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				layoutHTML(t, c.html, 600)
			}()
			select {
			case <-done:
			case <-time.After(budget):
				// The layout loops have no cancellation point, so the goroutine
				// keeps running; fail and let the test binary tear it down.
				t.Fatalf("layout still running after %v: an unbounded count reached the grid", budget)
			}
		})
	}
}

// TestSpanAttributesClamp pins the HTML ceilings on the span attributes
// (HTML 4.9.11-12: colspan and <col span> clamp to 1000, rowspan to 65534),
// and that ordinary values are untouched.
func TestSpanAttributesClamp(t *testing.T) {
	cases := []struct {
		name string
		attr string
		max  int
	}{
		{"colspan", "colspan", maxColSpan},
		{"rowspan", "rowspan", maxRowSpan},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, in := range []struct {
				val  string
				want int
			}{
				{"1", 1},
				{"7", 7},
				{"0", 1},   // HTML clamps up to 1
				{"-5", 1},  // ditto
				{"abc", 1}, // non-numeric
				{"900000000", c.max},
				// A digit run long enough to defeat Atoi entirely still has to
				// land on a usable value rather than propagating an error.
				{strings.Repeat("9", 400), 1},
			} {
				got := spanAttrFor(t, c.attr, in.val)
				if got != in.want {
					t.Errorf("%s=%q resolved to %d, want %d", c.attr, truncate(in.val), got, in.want)
				}
			}
		})
	}
}

// spanAttrFor builds a one-cell table carrying attr=val and reports the span the
// box tree resolved, which is what the grid then consumes.
func spanAttrFor(t *testing.T, attr, val string) int {
	t.Helper()
	doc, err := html.Parse([]byte(`<table><tr><td ` + attr + `="` + val + `">x</td></tr></table>`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var span int
	var walk func(b *cssbox.Box)
	walk = func(b *cssbox.Box) {
		if b == nil {
			return
		}
		if b.Display == cssbox.DisplayTableCell {
			if attr == "rowspan" {
				span = b.RowSpan
			} else {
				span = b.ColSpan
			}
			return
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)
	if span == 0 {
		t.Fatalf("no table cell found in the built tree for %s=%q", attr, truncate(val))
	}
	return span
}

func truncate(s string) string {
	if len(s) > 20 {
		return s[:20] + "..."
	}
	return s
}
