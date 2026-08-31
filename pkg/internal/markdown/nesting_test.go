package markdown

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestBlockNestingLimit covers the per-line bound on block-container markers.
//
// The bound exists because goldmark's block parser is quadratic in the nesting
// opened on ONE line. Measured before it: 12,500 "- " markers took 0.49s, 25,000
// took 2.6s, 50,000 took 10.8s -- four times the work for twice the input -- and
// 200,000 markers (a 400 KB file, smaller than many READMEs) did not finish in a
// minute. The cost is in the dependency and lands before this package can do
// anything about it, so declining the input is the only lever.
func TestBlockNestingLimit(t *testing.T) {
	// Just inside the limit still converts.
	atCap := strings.Repeat("- ", maxBlockNesting) + "x\n"
	if _, err := ToHTML([]byte(atCap)); err != nil {
		t.Errorf("%d markers should convert: %v", maxBlockNesting, err)
	}
	// One past it is refused, with a matchable sentinel.
	over := strings.Repeat("- ", maxBlockNesting+1) + "x\n"
	_, err := ToHTML([]byte(over))
	if err == nil {
		t.Fatalf("%d markers was accepted; it should be refused", maxBlockNesting+1)
	}
	if !errors.Is(err, ErrTooDeeplyNested) {
		t.Errorf("error = %v, want it to wrap ErrTooDeeplyNested", err)
	}
}

// TestBlockNestingLimitIsCheap pins the property that matters: refusing must not
// cost what converting would have. The scan exits at the first over-deep line,
// so the work is bounded by where the nesting is, not by the file's size.
func TestBlockNestingLimitIsCheap(t *testing.T) {
	// 200,000 markers: unbounded, this did not finish in 60 seconds.
	src := []byte(strings.Repeat("- ", 200000) + "x\n")
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := ToHTML(src); err == nil {
			t.Error("a 200,000-marker line was accepted")
		}
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("ToHTML did not return: the nesting scan is not short-circuiting")
	}
}

// TestNestingCountsOnlyBlockOpeners guards the counter against over-reach. Each
// of these looks marker-ish at a line start but opens no container, so counting
// it would reject ordinary Markdown.
func TestNestingCountsOnlyBlockOpeners(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"thematic break", strings.Repeat("-", 5000) + "\n"},
		{"setext underline", "title\n" + strings.Repeat("-", 5000) + "\n"},
		{"bold emphasis", strings.Repeat("*", 5000) + "text\n"},
		{"bare digits", strings.Repeat("1", 5000) + "\n"},
		{"ordered without space", strings.Repeat("1.", 5000) + "\n"},
		{"hash run", strings.Repeat("#", 5000) + " heading\n"},
		{"greater-than mid-line", "text " + strings.Repeat("> ", 5000) + "\n"},
		{"code fence", "```\n" + strings.Repeat("- ", 5000) + "\n```\n"},
		{"tilde fence", "~~~\n" + strings.Repeat("> ", 5000) + "\n~~~\n"},
		{"fence with language", "```md\n" + strings.Repeat("- ", 5000) + "\n```\n"},
		{"indented fence", "  ```\n" + strings.Repeat("- ", 5000) + "\n  ```\n"},
		{"unclosed fence", "```\n" + strings.Repeat("- ", 5000) + "\n"},
		{"markers after a closed fence", "```\nx\n```\n\n- a\n  - b\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ToHTML([]byte(c.src)); err != nil {
				t.Errorf("refused ordinary Markdown: %v", err)
			}
		})
	}
}

// TestOrdinaryMarkdownUnaffected is the guard against the bound being too tight:
// these are the shapes real documents take.
func TestOrdinaryMarkdownUnaffected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"simple", "# Title\n\nA paragraph.\n"},
		{"nested list", "- a\n  - b\n    - c\n      - d\n"},
		{"nested quote", "> a\n> > b\n> > > c\n"},
		{"gfm table", "| a | b |\n| --- | --- |\n| 1 | 2 |\n"},
		{"task list", "- [ ] todo\n- [x] done\n"},
		{"long document", strings.Repeat("- item\n", 50000)},
		{"many quotes over lines", strings.Repeat("> quote\n\n", 20000)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := ToHTML([]byte(c.src))
			if err != nil {
				t.Fatalf("ToHTML: %v", err)
			}
			if len(out) == 0 {
				t.Error("empty output")
			}
		})
	}
}

// TestLeadingBlockMarkers pins the counter itself, so a future change to the
// marker rules is visible rather than silently shifting what gets refused.
func TestLeadingBlockMarkers(t *testing.T) {
	cases := []struct {
		line string
		want int
	}{
		{"", 0},
		{"text", 0},
		{"> x", 1},
		{"> > > x", 3},
		{">>>x", 3},
		{"- x", 1},
		{"- - - x", 3},
		{"* + - x", 3},
		{"1. x", 1},
		{"1. 2. 3. x", 3},
		{"1) x", 1},
		{"> - 1. x", 3},
		{"  > x", 1},    // indentation before a marker
		{"---", 0},      // thematic break, not a list
		{"**bold**", 0}, // emphasis, not a list
		{"1.x", 0},      // no space after the dot
		{"1", 0},        // bare digit
		{"x > y", 0},    // not at the line start
	}
	for _, c := range cases {
		if got := leadingBlockMarkers([]byte(c.line)); got != c.want {
			t.Errorf("leadingBlockMarkers(%q) = %d, want %d", c.line, got, c.want)
		}
	}
}
