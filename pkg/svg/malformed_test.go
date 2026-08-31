package svg

import (
	"strings"
	"testing"
	"time"
)

// Both defects below were found by FuzzParse rather than by reading the code,
// and both escaped the public Parse on a document under 70 bytes.

// TestTextPositionRangeAfterTrailingSpaceStrip covers a <text> whose recorded
// position range outlives the characters it covers.
//
// recordLists stores a half-open [start,end) span over tb.chars, and
// dropTrailingSpace then runs and can shrink that slice -- so resolvePositions
// indexed past the end. The parser already knew about this hazard for the
// textLength ranges (trimLengths exists for exactly this reason and says so);
// the position lists were simply missed.
//
// Found by fuzzing: `<svg><text x="0">0 <A>` panicked with
// `index out of range [1] with length 1`.
func TestTextPositionRangeAfterTrailingSpaceStrip(t *testing.T) {
	const ns = `<svg xmlns="http://www.w3.org/2000/svg">`
	cases := []struct {
		name string
		src  string
	}{
		// The fuzzer's original: an unknown element after a trailing space.
		{"unknown element after trailing space", ns + `<text x="0">0 <A>`},
		// The same shape with the position list on each axis.
		{"y list", ns + `<text y="0">0 <A>`},
		{"dx list", ns + `<text dx="0">0 <A>`},
		{"dy list", ns + `<text dy="0">0 <A>`},
		{"rotate list", ns + `<text rotate="0">0 <A>`},
		// A list longer than the surviving characters.
		{"multi-value list", ns + `<text x="0 1 2 3 4">0 <A>`},
		// The trailing space stripped from inside a tspan.
		{"tspan", ns + `<text x="0 1"><tspan>a </tspan></text></svg>`},
		// Every character stripped: the len==0 guard already covered this, but
		// it belongs in the same table.
		{"all whitespace", ns + `<text x="0"> </text></svg>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A panic here fails the test; before the clamp this indexed out of
			// range straight out of Parse.
			if _, err := Parse([]byte(c.src), nil); err != nil {
				t.Logf("Parse returned %v (acceptable: it must not panic)", err)
			}
		})
	}
}

// TestPathClosepathNoImplicitRepeat covers path data where a closepath is
// followed by numbers.
//
// The command loop treats "a number where a command was expected" as an
// implicit repetition of the previous command. Every other command consumes its
// arguments and advances the scanner, but closepath takes none -- so repeating
// it consumed nothing, and the loop spun forever appending Close(). SVG's path
// grammar has no implicit repetition for closepath, so stopping is both correct
// and terminating.
//
// Found by fuzzing: `<path d="M0 0Z0 0l 0 0">`, 64 bytes, hung inside Parse.
func TestPathClosepathNoImplicitRepeat(t *testing.T) {
	const ns = `<svg xmlns="http://www.w3.org/2000/svg">`
	cases := []struct {
		name string
		d    string
	}{
		{"the fuzzer's case", "M0 0Z0 0l 0 0"},
		{"lowercase z", "M0 0z0 0"},
		{"Z then many numbers", "M0 0Z" + strings.Repeat("0 ", 1000)},
		{"Z then negative", "M0 0Z-1-1"},
		{"Z then decimal", "M0 0Z.5.5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = Parse([]byte(ns+`<path d="`+c.d+`"/></svg>`), nil)
			}()
			select {
			case <-done:
			case <-time.After(20 * time.Second):
				t.Fatal("Parse did not return: closepath is repeating without consuming input")
			}
		})
	}
}

// TestPathClosepathStillWorks is the guard against the fix over-reaching: a
// closepath followed by a real command must still parse, and the subpath it
// closes must still be there.
func TestPathClosepathStillWorks(t *testing.T) {
	const ns = `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">`
	cases := []string{
		"M0 0 L5 0 L5 5 Z",        // a closed triangle
		"M0 0 L5 0 Z M6 6 L9 9 Z", // two closed subpaths
		"M0 0 L5 0 Z L9 9",        // closepath then an explicit command
		"M0 0 L1 1 z m2 2 l1 1 z", // relative forms
	}
	for _, d := range cases {
		doc, err := Parse([]byte(ns+`<path d="`+d+`" fill="black"/></svg>`), nil)
		if err != nil {
			t.Errorf("d=%q: %v", d, err)
			continue
		}
		if doc == nil {
			t.Errorf("d=%q: nil document", d)
			continue
		}
		_, root := doc.Root()
		if root == nil || len(root.Kids) == 0 {
			t.Errorf("d=%q: the path produced no scene node", d)
		}
	}
}
