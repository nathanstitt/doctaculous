package rtf

import (
	"strings"
	"testing"
)

// FuzzToHTML exercises the whole RTF converter: the tokenizer, the control-word
// dispatch, the group/destination stack, and the HTML emitter.
//
// ToHTML has no partial-failure mode -- the RTF resilience rule is that unknown
// control words are skipped and unknown {\*} destinations ignored -- so almost
// every input is expected to succeed. The property under test is that it
// RETURNS: no panic, no unbounded recursion, and no output sized by a number
// read out of the document.
//
// The last of those is not hypothetical here: \ilvl reached the emitter
// unbounded and made a 34-byte file produce output forever (see maxListLevel).
func FuzzToHTML(f *testing.F) {
	for _, s := range rtfSeeds() {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// A nil logger is the common caller shape and the one that historically
		// hid degradations, so fuzz it rather than the instrumented path.
		_, _ = ToHTML(data, nil)
	})
}

// rtfSeeds returns documents covering the control words that drive allocation
// or nesting, plus the structural edge cases a mutator is unlikely to find on
// its own.
func rtfSeeds() []string {
	return []string{
		`{\rtf1 hello}`,
		`{\rtf1\ansi\deff0{\fonttbl{\f0 Times;}}\f0\fs24 text\par}`,
		`{\rtf1{\colortbl;\red255\green0\blue0;}\cf1 red\par}`,

		// Counts that size work.
		`{\rtf1{\ls1\ilvl2000000000 x\par}}`,
		`{\rtf1{\ls1\ilvl-5 x\par}}`,
		`{\rtf1\paperw2000000000\paperh2000000000 x\par}`,
		`{\rtf1\li2000000000 x\par}`,
		`{\rtf1\fs2000000000 x\par}`,
		`{\rtf1\ucr2000000000\u65 x\par}`,
		`{\rtf1\u-2000000000 x\par}`,

		// Group nesting: the destination stack is the recursion here.
		`{\rtf1` + strings.Repeat("{", 50000) + strings.Repeat("}", 50000) + `}`,
		`{\rtf1` + strings.Repeat("{", 50000),
		`{\rtf1` + strings.Repeat(`{\*\unknown `, 10000) + `x`,

		// Tables: \trowd/\cell drive their own accumulation.
		`{\rtf1\trowd\cellx1000\cellx2000 a\cell b\cell\row}`,
		`{\rtf1\trowd` + strings.Repeat(`\cellx1000`, 10000) + ` a\cell\row}`,
		`{\rtf1` + strings.Repeat(`\trowd\cellx100 a\cell\row`, 5000) + `}`,

		// Pictures: the data: URI path decodes attacker-controlled bytes.
		`{\rtf1{\pict\pngblip 89504e470d0a1a0a}}`,
		`{\rtf1{\pict\jpegblip ffd8ffe0}}`,
		`{\rtf1{\pict\pngblip ` + strings.Repeat("41", 10000) + `}}`,
		`{\rtf1{\pict\picw2000000000\pich2000000000\pngblip 89}}`,

		// Hex and unicode escapes.
		`{\rtf1 \'e9\'e8\'ff}`,
		`{\rtf1 \'zz}`,
		`{\rtf1 \u233\'3f x}`,

		// Structural degenerates.
		`{\rtf1}`,
		`{}`,
		`{\rtf1`,
		`}`,
		``,
		`not rtf at all`,
		`{\rtf1\` + strings.Repeat("a", 10000) + ` x}`, // absurd control word
	}
}
