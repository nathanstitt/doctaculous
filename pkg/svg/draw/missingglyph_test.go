package draw

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/render"
	"github.com/nathanstitt/omnidoc/pkg/svg"
)

// drawSVGCollectingLogs parses src and DRAWS it, returning everything the
// renderer logged. Drawing is the point: the shaper runs at draw time, so a
// helper that only parses (like parseSVG) cannot see a text degradation at all —
// which is exactly why this gap survived as long as it did.
func drawSVGCollectingLogs(t *testing.T, src string) []string {
	t.Helper()
	doc, err := svg.Parse([]byte(src), func(string, ...any) {})
	if err != nil {
		t.Fatalf("svg.Parse: %v", err)
	}
	var logs []string
	r := NewWithLogf(doc, func(f string, a ...any) {
		logs = append(logs, fmt.Sprintf(f, a...))
	})
	r.DrawVector(discardDevice{}, render.Identity)
	return logs
}

// discardDevice satisfies render.Device and draws nothing: these tests assert on
// the diagnostics, not on pixels.
type discardDevice struct{ render.Device }

func (discardDevice) DrawGlyph(render.GlyphRef)                        {}
func (discardDevice) FillGlyph(*render.Path, render.FillColor, string) {}
func (discardDevice) Fill(*render.Path, render.FillPaint)              {}
func (discardDevice) Stroke(*render.Path, render.StrokePaint)          {}

// A rune no bundled face can map draws .notdef — an empty box — and MUST say so.
// It did not: the renderer's Logf was nil at every construction site, so SVG text
// that could not be shaped rendered as a column of tofu in complete silence,
// while the identical text through the CSS path logged once per rune.
//
// This is the regression test for that asymmetry. It draws rather than parses,
// because the shaper runs at draw time.
func TestMissingGlyphIsReported(t *testing.T) {
	// U+6F22 and U+5B57 are Han; no bundled face covers CJK.
	logs := drawSVGCollectingLogs(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100">
	  <text x="10" y="50" font-size="30">&#28450;&#23383;</text></svg>`)

	var got []string
	for _, l := range logs {
		if strings.Contains(l, "no glyph") {
			got = append(got, l)
		}
	}
	if len(got) != 2 {
		t.Fatalf("got %d missing-glyph diagnostics, want 2 (one per distinct rune); logs = %v", len(got), logs)
	}
	// The code point is what a reader needs to act on, so it has to be named.
	joined := strings.Join(got, "\n")
	for _, want := range []string{"U+6F22", "U+5B57"} {
		if !strings.Contains(joined, want) {
			t.Errorf("diagnostics do not name %s:\n%s", want, joined)
		}
	}
}

// Text that shapes cleanly must NOT log. A diagnostic on every ordinary document
// is noise, and noise is what stops the real ones from being read.
func TestShapeableTextDoesNotReport(t *testing.T) {
	logs := drawSVGCollectingLogs(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100">
	  <text x="10" y="50" font-size="30">Text</text></svg>`)

	for _, l := range logs {
		if strings.Contains(l, "no glyph") {
			t.Errorf("ordinary Latin text produced a missing-glyph diagnostic: %q", l)
		}
	}
}

// A nil logger must stay silent and must not panic: New (rather than NewWithLogf)
// is still a supported construction, and every degradation path has to tolerate
// it. Drawing unshapeable text through it is the case that would crash if a log
// site forgot its nil check.
func TestNilLoggerIsSilentNotFatal(t *testing.T) {
	doc, err := svg.Parse([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100">
	  <text x="10" y="50" font-size="30">&#28450;</text></svg>`), func(string, ...any) {})
	if err != nil {
		t.Fatalf("svg.Parse: %v", err)
	}
	New(doc).DrawVector(discardDevice{}, render.Identity)              // Logf nil
	NewWithLogf(doc, nil).DrawVector(discardDevice{}, render.Identity) // explicitly nil
}

// familyWithFallback appends the generic only when it is not already there. The
// string is quoted verbatim in the missing-glyph diagnostic, and SVG's own
// initial font-family IS sans-serif — so before this, an element stating no
// family reported `family "sans-serif, sans-serif"`, which reads as two distinct
// families having been tried and failed, in the very message meant to explain
// the tofu.
func TestFamilyFallbackDoesNotDuplicate(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "sans-serif"},
		{"sans-serif", "sans-serif"},
		{"serif", "serif, sans-serif"},
		{"Noto Sans", "Noto Sans, sans-serif"},
		{"Foo, sans-serif", "Foo, sans-serif"},
		// A generic that merely APPEARS in the list, but is not last, still gets
		// the terminal fallback: the chain has to end with something that
		// resolves, and only the final entry guarantees that.
		{"sans-serif, Foo", "sans-serif, Foo, sans-serif"},
	} {
		if got := familyWithFallback(tc.in); got != tc.want {
			t.Errorf("familyWithFallback(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ...and the deduplication is visible in the diagnostic itself, which is the
// only reason it matters.
func TestMissingGlyphNamesTheFamilyOnce(t *testing.T) {
	logs := drawSVGCollectingLogs(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100">
	  <text x="10" y="50" font-size="30">&#28450;</text></svg>`)

	for _, l := range logs {
		if strings.Contains(l, "sans-serif, sans-serif") {
			t.Errorf("diagnostic names the fallback family twice: %q", l)
		}
	}
}
