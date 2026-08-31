package css

import (
	"strings"
	"testing"
)

// FuzzParse drives a stylesheet through the whole front end: the brace-matching
// rule scanner, selector parsing, declaration parsing, and the cascade that
// turns them into a ComputedStyle.
//
// Parse documents itself as TOTAL -- "malformed rules and unsupported at-rules
// are skipped (their block consumed) rather than aborting the parse, so a single
// bad construct cannot discard the sheet". There is no error return, so a
// malformed sheet has no way to report failure and every input must simply come
// back. That makes any panic, hang, or unbounded allocation here a defect by
// definition rather than a judgement call.
//
// Cascading is included because parsing alone stops short of the property
// parsers, which is where a value like an infinite flex-grow does its damage.
func FuzzParse(f *testing.F) {
	for _, s := range cssSeeds() {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, src string) {
		sheet := Parse(src)

		// Cascade the sheet onto a small tree so selector matching and every
		// property parser actually run.
		r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)
		root := &fakeNode{tag: "html"}
		body := &fakeNode{tag: "body", parent: root}
		el := &fakeNode{
			tag:     "div",
			id:      "target",
			classes: []string{"a", "b"},
			parent:  body,
			attrs:   map[string]string{"data-x": "1", "lang": "en"},
		}
		rootStyle := r.ComputeRoot(root)
		bodyStyle := r.Compute(body, rootStyle)
		r.Compute(el, bodyStyle)
	})
}

// FuzzParseDeclarations targets the declaration parser directly. It is reachable
// from an inline style="..." attribute, which is a far more common path for
// attacker-controlled CSS than a whole stylesheet.
func FuzzParseDeclarations(f *testing.F) {
	f.Add("color: red; margin: 0 auto")
	f.Add("flex-grow: inf")
	f.Add("grid-template-columns: repeat(200000000, 1px)")
	f.Add("width: calc(100% - " + strings.Repeat("(", 1000) + "1px")
	f.Add("")
	f.Add(";;;;")
	f.Add("background: url(" + strings.Repeat("a", 10000) + ")")

	f.Fuzz(func(t *testing.T, body string) {
		decls := ParseDeclarations(body)
		// Apply them, which is where the per-property parsers run.
		cs := initialStyle()
		for _, d := range decls {
			applyDeclaration(&cs, d)
		}
	})
}

// FuzzParseColorValue targets the colour grammar, which parses from raw text
// rather than the token stream (see tokenizer.rest) and so has its own set of
// edge cases: nested colour functions, relative colours, and colour-mix.
func FuzzParseColorValue(f *testing.F) {
	f.Add("#fff")
	f.Add("rgb(79 156 255 / 35%)")
	f.Add("color-mix(in srgb, red 50%, blue)")
	f.Add("hsl(120deg 50% 50%)")
	f.Add("rgb(" + strings.Repeat("9", 400) + ")")
	f.Add("color-mix(in srgb, " + strings.Repeat("color-mix(in srgb, red, ", 200) + "blue")
	f.Add("")

	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParseColorValue(s)
	})
}

// cssSeeds returns stylesheets aimed at the structural weak points: the
// brace-matching scanner, the value grammars that recurse, and the numeric
// parsers whose results size allocations.
func cssSeeds() []string {
	return []string{
		// Ordinary, so the mutator has valid structure to work from.
		"div { color: red; margin: 0 auto }",
		".a > .b + .c ~ .d e { padding: 1px 2px 3px 4px }",
		"@media screen and (min-width: 600px) { p { font-size: 12pt } }",
		"@font-face { font-family: X; src: url(x.woff2); unicode-range: U+0-7F }",
		"@page :first { margin: 1in }",
		":root { --x: 4px } div { width: var(--x) }",

		// Unbalanced and pathological brace nesting: the rule scanner finds
		// boundaries by brace matching, so this is its worst case.
		strings.Repeat("{", 10000),
		strings.Repeat("}", 10000),
		strings.Repeat("a{", 10000),
		"div {" + strings.Repeat("@media x {", 5000),

		// Unterminated comment: the scanner skips /* */ before matching braces.
		"/*" + strings.Repeat("a", 10000),
		strings.Repeat("/*a*/", 5000) + "div{color:red}",

		// Values that recurse or expand.
		"div { width: calc(" + strings.Repeat("(", 5000) + "1px" + strings.Repeat(")", 5000) + ") }",
		"div { grid-template-columns: repeat(200000000, 1px) }",
		"div { grid-row: 1 / 500000000 }",
		"div { flex-grow: inf }",
		"div { flex-grow: " + strings.Repeat("9", 400) + " }",
		"div { color: color-mix(in srgb, " + strings.Repeat("color-mix(in srgb, red, ", 500) + "blue }",
		"div { background: " + strings.Repeat("linear-gradient(red, ", 500) + "blue }",
		"div { transform: " + strings.Repeat("translate(1px,1px) ", 5000) + "}",

		// Selector shapes: long chains and deep :is()/:not() nesting.
		strings.Repeat("a ", 10000) + "{ color: red }",
		strings.Repeat(":not(", 5000) + "a" + strings.Repeat(")", 5000) + "{ color: red }",
		strings.Repeat("a,", 10000) + "b { color: red }",

		// Empty and degenerate.
		"",
		"{}",
		"@",
		"@media",
		"div{",
	}
}
