package rtf

import (
	"strings"
	"testing"
	"time"
)

// TestListLevelBounded covers \ilvl, the list nesting depth, which the HTML
// emitter turns into one <ul>/<ol> tag per level.
//
// An unbounded \ilvl is an unbounded write, not a deep list. Measured before the
// clamp: a 30-byte document with \ilvl100000 produced 1.4 MB of markup (a
// 46,000x amplification), and \ilvl2000000000 never finished at all -- from 34
// bytes of input.
func TestListLevelBounded(t *testing.T) {
	cases := []struct {
		name string
		ilvl string
	}{
		{"large", "100000"},
		{"enormous", "2000000000"},
		{"max int32", "2147483647"},
		{"negative", "-5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := `{\rtf1{\ls1\ilvl` + c.ilvl + ` x\par}}`

			done := make(chan string, 1)
			go func() {
				out, err := ToHTML([]byte(src), nil)
				if err != nil {
					done <- ""
					return
				}
				done <- out
			}()
			select {
			case out := <-done:
				// The output must be bounded by the level cap, not by the
				// number the document asked for.
				if n := strings.Count(out, "<ul>") + strings.Count(out, "<ol>"); n > maxListLevel+1 {
					t.Errorf("emitted %d list tags for \\ilvl%s; the cap is %d", n, c.ilvl, maxListLevel)
				}
			case <-time.After(20 * time.Second):
				t.Fatalf("ToHTML did not return for \\ilvl%s: the emitter is unbounded", c.ilvl)
			}
		})
	}
}

// TestOrdinaryListLevelsUnaffected guards the clamp against over-reach. RTF
// allows levels 0-8 and Word exposes nine, so real nesting must round-trip
// exactly.
func TestOrdinaryListLevelsUnaffected(t *testing.T) {
	// Three nested levels, one item each.
	src := `{\rtf1{\ls1\ilvl0 a\par}{\ls1\ilvl1 b\par}{\ls1\ilvl2 c\par}}`
	out, err := ToHTML([]byte(src), nil)
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	// Three levels of nesting means three opening list tags.
	if n := strings.Count(out, "<ul>") + strings.Count(out, "<ol>"); n != 3 {
		t.Errorf("3-level list emitted %d list tags, want 3:\n%s", n, out)
	}
	for _, want := range []string{"a", "b", "c"} {
		if !strings.Contains(out, want) {
			t.Errorf("item %q missing from output:\n%s", want, out)
		}
	}
}
