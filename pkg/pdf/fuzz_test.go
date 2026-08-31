package pdf

import (
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/testdata/gen"
)

// FuzzParse exercises the whole open path against arbitrary bytes: xref parsing,
// the brute-force rebuild fallback, encryption setup, the page tree walk, and
// per-page content access.
//
// The property under test is only that Parse returns. A malformed PDF may fail
// with any error -- what it may not do is panic, recurse without bound, or size
// an allocation from a number it read out of the file. Those are the failure
// modes a document toolkit cannot have, because its whole job is eating files it
// did not author, and the worst of them (a stack overflow, an OOM) are raised
// through runtime.throw where the per-page recover() cannot catch them.
//
// This target has already paid for itself: it found the object-stream /N
// allocation (a 504-byte file asking for a 320 GB slice), which reading the code
// had not surfaced.
//
// Seeds: every well-formed core fixture, plus the hand-built hostile shapes
// below. The fixtures give the fuzzer valid structure to mutate; the hostile
// seeds point it directly at the recursion and count-driven allocation sites.
func FuzzParse(f *testing.F) {
	for _, fx := range gen.Core {
		f.Add(fx.Bytes())
	}
	for _, s := range hostileSeeds() {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := Parse(data)
		if err != nil || doc == nil {
			return
		}
		// Walk what the document claims to hold. PageCount comes from the file,
		// so bound the walk rather than trusting it -- a fuzz failure should be
		// a real defect, not this loop running for a week on a huge count.
		n := doc.PageCount()
		if n > 64 {
			n = 64
		}
		for i := range n {
			pg, err := doc.Page(i)
			if err != nil || pg == nil {
				continue
			}
			content, err := pg.ContentBytes()
			if err != nil {
				continue
			}
			// Content streams are their own parser; scan them too.
			scanContent(content)
		}
	})
}

// scanContent drives the content-stream scanner to exhaustion, bounding the
// operator count so a pathological stream cannot spin the fuzz worker forever.
func scanContent(content []byte) {
	s := NewContentScanner(content)
	for range 100000 {
		_, _, ok, err := s.Next()
		if err != nil || !ok {
			return
		}
	}
}

// hostileSeeds returns malformed PDFs aimed at specific structural weaknesses.
// Each is a shape a fuzzer would take a long time to discover on its own but
// which is trivial to write down once the parser has been read.
func hostileSeeds() [][]byte {
	const header = "%PDF-1.7\n"
	seeds := []string{
		// Deeply nested arrays and dictionaries: parseArray and parseDictOrStream
		// recurse through parseFromToken, so nesting depth is stack depth.
		header + "1 0 obj " + strings.Repeat("[", 50000) + strings.Repeat("]", 50000) + " endobj\n",
		header + "1 0 obj " + strings.Repeat("<</K ", 50000) + "null" + strings.Repeat(">>", 50000) + " endobj\n",
		// Unterminated nesting: the same recursion, reached via the error path.
		header + "1 0 obj " + strings.Repeat("[", 50000) + " endobj\n",

		// A page tree that points at itself. The depth cap alone does not stop
		// this: the fan-out is exponential BELOW the cap.
		header + `1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj
2 0 obj <</Type/Pages/Kids[2 0 R 2 0 R]/Count 2>> endobj
trailer <</Root 1 0 R>>
`,
		// Two nodes pointing at each other.
		header + `1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj
2 0 obj <</Type/Pages/Kids[3 0 R 3 0 R]/Count 2>> endobj
3 0 obj <</Type/Pages/Kids[2 0 R 2 0 R]/Count 2>> endobj
trailer <</Root 1 0 R>>
`,
		// Counts and sizes taken straight from the file.
		header + "trailer <</Size 2000000000/Root 1 0 R>>\n",
		header + `1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj
2 0 obj <</Type/Pages/Kids[]/Count 2000000000>> endobj
trailer <</Root 1 0 R>>
`,
		// An object stream whose /N far exceeds what its data can hold. This is
		// the shape the fuzzer found; keeping it as a seed makes the regression
		// explicit rather than a lucky rediscovery.
		header + `1 0 obj <</Type/ObjStm/N 40000000020/First 6/Length 4>> stream
0 0
endstream endobj
trailer <</Root 1 0 R>>
`,
		// An object referring to itself: Resolve has a cap, but the shape is
		// worth keeping in the corpus.
		header + "1 0 obj 1 0 R endobj\ntrailer <</Root 1 0 R>>\n",

		// Structural edge cases that steer the rebuild path.
		header + "trailer <<>>\n",
		header,
		"not a pdf at all",
	}
	out := make([][]byte, 0, len(seeds))
	for _, s := range seeds {
		out = append(out, []byte(s))
	}
	return out
}
