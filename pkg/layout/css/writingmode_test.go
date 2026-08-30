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

// vertical-lr lays out identically to vertical-rl in this phase: the two differ only in
// which side SUBSEQUENT lines stack from, and only one line is produced. Equating them
// silently would be exactly the half-applied support the degrade-honestly rule exists to
// prevent, so the remaining difference reports itself.
//
// This replaces a phase-0 test asserting that BOTH vertical values reported themselves
// unimplemented — no longer true of vertical-rl, which now lays out.
func TestVerticalLRReportsItsUnimplementedStackingDirection(t *testing.T) {
	var msgs []string
	logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
	src := `<html><body style="margin:0"><div style="writing-mode:vertical-lr">NOW</div></body></html>`
	layoutTreeFor(t, src, 200, logf)

	got := countWritingModeWarnings(msgs)
	if len(got) != 1 {
		t.Fatalf("vertical-lr logged %d times, want exactly 1; messages: %v", len(got), msgs)
	}
	if !strings.Contains(got[0], "vertical-lr") {
		t.Errorf("log line does not name the value: %q", got[0])
	}
}

// vertical-rl is the implemented mode: it must NOT report itself unimplemented. A
// diagnostic that contradicts the rendering is its own bug, and the mirror image of the
// silent no-op phase 0 replaced.
func TestVerticalRLDoesNotWarn(t *testing.T) {
	var msgs []string
	logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
	src := `<html><body style="margin:0"><div style="writing-mode:vertical-rl">NOW</div></body></html>`
	layoutTreeFor(t, src, 200, logf)

	if got := countWritingModeWarnings(msgs); len(got) != 0 {
		t.Errorf("vertical-rl warned about writing-mode: %v", got)
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
// Asserted on vertical-lr, the value that still reports something.
func TestVerticalWritingModeWarnsOncePerValue(t *testing.T) {
	var msgs []string
	logf := func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) }
	src := `<html><body style="margin:0"><div style="writing-mode:vertical-lr">` +
		`<div>N</div><div>O</div><div>W</div><div><span>!</span></div></div></body></html>`
	layoutTreeFor(t, src, 200, logf)

	if got := countWritingModeWarnings(msgs); len(got) != 1 {
		t.Errorf("nested vertical boxes logged %d times, want exactly 1 (warn-once); messages: %v", len(got), got)
	}
}

// The cascade still carries both vertical values distinctly — the parse and inheritance
// work phase 0 did is what phase 2 builds on, so a regression there must not hide behind
// the layout now succeeding.
func TestBothVerticalValuesReachLayout(t *testing.T) {
	for _, wm := range []string{"vertical-rl", "vertical-lr"} {
		root := layoutTreeFor(t, `<html><body style="margin:0">`+
			`<div style="writing-mode:`+wm+`;font-size:20px">NOW</div></body></html>`, 200, nil)

		var vertical bool
		var walk func(*Fragment)
		walk = func(f *Fragment) {
			for li := range f.Lines {
				if f.Lines[li].Vertical {
					vertical = true
				}
			}
			for _, c := range f.Children {
				walk(c)
			}
		}
		walk(root)
		if !vertical {
			t.Errorf("%s did not produce a vertical line", wm)
		}
	}
}

// Inheritance: writing-mode set on a container must reach a descendant's line, which is
// the trap inheritFrom documents. Phase 0 covered this at the cascade; here it is
// asserted through to the laid-out result, where it actually matters.
func TestVerticalWritingModeInheritsToDescendantLines(t *testing.T) {
	root := layoutTreeFor(t, `<html><body style="margin:0">`+
		`<div style="writing-mode:vertical-rl;font-size:20px"><div><span>NOW</span></div></div>`+
		`</body></html>`, 200, nil)

	var vertical, any bool
	var walk func(*Fragment)
	walk = func(f *Fragment) {
		for li := range f.Lines {
			if len(f.Lines[li].Glyphs) > 0 {
				any = true
				if f.Lines[li].Vertical {
					vertical = true
				}
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if !any {
		t.Fatal("no glyph-bearing line found")
	}
	if !vertical {
		t.Error("a nested box did not inherit the container's vertical writing-mode")
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
