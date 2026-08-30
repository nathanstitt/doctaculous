package css

import (
	"fmt"
	"strings"
	"testing"
)

// countWritingModeWarnings returns the writing-mode degradation lines in msgs.
func countWritingModeWarnings(msgs []string) []string {
	var got []string
	for _, m := range msgs {
		if strings.Contains(m, "writing-mode=") {
			got = append(got, m)
		}
	}
	return got
}

// A vertical writing-mode is not implemented, and saying so is the whole point of this
// path. The property previously did not reach the cascade at all, so an author got a
// silent no-op: a correct stylesheet, a wrong page, and no diagnostic anywhere. Every
// other unsupported case in this engine logs; this one did not.
func TestVerticalWritingModeIsLoggedNotSilent(t *testing.T) {
	for _, wm := range []string{"vertical-rl", "vertical-lr"} {
		var msgs []string
		logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
		src := `<html><body style="margin:0"><div style="writing-mode:` + wm + `">NOW</div></body></html>`
		layoutTreeFor(t, src, 200, logf)

		got := countWritingModeWarnings(msgs)
		if len(got) != 1 {
			t.Errorf("%s logged %d times, want exactly 1; messages: %v", wm, len(got), msgs)
			continue
		}
		if !strings.Contains(got[0], wm) {
			t.Errorf("%s: log line does not name the value: %q", wm, got[0])
		}
	}
}

// horizontal-tb is the initial value and the supported one: it must NOT warn, or the
// diagnostic becomes noise on every ordinary document and stops being read.
func TestHorizontalWritingModeDoesNotWarn(t *testing.T) {
	for _, style := range []string{"", "writing-mode:horizontal-tb", "writing-mode:lr-tb"} {
		var msgs []string
		logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
		src := `<html><body style="margin:0"><div style="` + style + `">NOW</div></body></html>`
		layoutTreeFor(t, src, 200, logf)

		if got := countWritingModeWarnings(msgs); len(got) != 0 {
			t.Errorf("style %q warned about writing-mode: %v", style, got)
		}
	}
}

// Warn-ONCE. writing-mode is inherited, so a vertical container hands the value to
// every descendant box; a per-box log would emit one identical line per element in the
// subtree, which for a real sidebar label (one span per letter) is a page of noise.
func TestVerticalWritingModeWarnsOncePerValue(t *testing.T) {
	var msgs []string
	logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
	src := `<html><body style="margin:0"><div style="writing-mode:vertical-rl">` +
		`<div>N</div><div>O</div><div>W</div><div><span>!</span></div></div></body></html>`
	layoutTreeFor(t, src, 200, logf)

	if got := countWritingModeWarnings(msgs); len(got) != 1 {
		t.Errorf("nested vertical boxes logged %d times, want exactly 1 (warn-once); messages: %v", len(got), got)
	}
}

// The two vertical values are keyed separately, so a document using both reports both
// rather than the first one seen masking the other.
func TestBothVerticalValuesEachWarn(t *testing.T) {
	var msgs []string
	logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
	src := `<html><body style="margin:0">` +
		`<div style="writing-mode:vertical-rl">A</div>` +
		`<div style="writing-mode:vertical-lr">B</div></body></html>`
	layoutTreeFor(t, src, 200, logf)

	got := countWritingModeWarnings(msgs)
	if len(got) != 2 {
		t.Fatalf("two distinct vertical values logged %d lines, want 2; messages: %v", len(got), got)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "vertical-rl") || !strings.Contains(joined, "vertical-lr") {
		t.Errorf("both values should be named across the log lines, got:\n%s", joined)
	}
}

// Phase 0 parses and REPORTS; it deliberately does not change layout. This pins that:
// a vertical value must lay out byte-identically to horizontal until the vertical
// advance model lands, so the degradation is honest rather than half-applied.
func TestVerticalWritingModeDoesNotChangeLayoutYet(t *testing.T) {
	const body = `<div style="width:100px;%s">NOW</div>`
	horiz := layoutTreeFor(t, `<html><body style="margin:0">`+
		fmt.Sprintf(body, "")+`</body></html>`, 200, nil)
	vert := layoutTreeFor(t, `<html><body style="margin:0">`+
		fmt.Sprintf(body, "writing-mode:vertical-rl")+`</body></html>`, 200, nil)

	hb, vb := firstTextBox(horiz), firstTextBox(vert)
	if hb == nil || vb == nil {
		t.Fatal("expected a text-bearing fragment in both trees")
	}
	if hb.W != vb.W || hb.H != vb.H {
		t.Errorf("vertical box = %vx%v, horizontal = %vx%v; phase 0 must not alter layout",
			vb.W, vb.H, hb.W, hb.H)
	}
}

// firstTextBox returns the deepest first fragment carrying line content, which is the
// box whose dimensions the assertions above compare.
func firstTextBox(f *Fragment) *Fragment {
	if f == nil {
		return nil
	}
	if len(f.Lines) > 0 {
		return f
	}
	for _, c := range f.Children {
		if got := firstTextBox(c); got != nil {
			return got
		}
	}
	return nil
}
