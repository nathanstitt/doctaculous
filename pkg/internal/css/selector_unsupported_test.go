package css

import (
	"fmt"
	"strings"
	"testing"
)

// TestUnsupportedSelectorsAreRecorded is the positive half: every selector
// construct the parser does not implement must be RECORDED when it causes a rule
// to be dropped, naming the construct and quoting what the author wrote.
//
// The gap this closes is not that these selectors fail — failing safe is correct
// and deliberate — but that they failed SILENTLY. parseOneSelector split on
// whitespace, so `svg > path` became three parts with a bogus middle tag ">"
// that could never match, and the author got nothing.
func TestUnsupportedSelectorsAreRecorded(t *testing.T) {
	for _, tc := range []struct {
		name          string
		src           string
		wantConstruct string
		wantSelector  string
	}{
		{"child, spaced", `svg > path { fill: red }`, unsupportedChild, "svg > path"},
		{"child, unspaced", `.icon>path { fill: red }`, unsupportedChild, ".icon>path"},
		{"adjacent sibling", `h1 + p { margin: 0 }`, unsupportedAdjacent, "h1 + p"},
		{"adjacent sibling, unspaced", `h1+p { margin: 0 }`, unsupportedAdjacent, "h1+p"},
		{"general sibling", `h1 ~ p { margin: 0 }`, unsupportedGeneralSibling, "h1 ~ p"},
		{"attribute, presence", `[hidden] { display: none }`, unsupportedAttribute, "[hidden]"},
		{"attribute, equality", `input[type=text] { color: red }`, unsupportedAttribute, "input[type=text]"},
		{"attribute, prefix match", `[class^="cls-"] { fill: blue }`, unsupportedAttribute, `[class^="cls-"]`},
		{"namespace", `svg|rect { fill: red }`, unsupportedNamespace, "svg|rect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sheet := Parse(tc.src)
			if len(sheet.Rules) != 0 {
				t.Errorf("the rule was kept (%d rules); an unrepresentable selector must be dropped, "+
					"never approximated", len(sheet.Rules))
			}
			if len(sheet.Unsupported) != 1 {
				t.Fatalf("Unsupported = %+v, want exactly one record", sheet.Unsupported)
			}
			got := sheet.Unsupported[0]
			if got.Construct != tc.wantConstruct {
				t.Errorf("Construct = %q, want %q", got.Construct, tc.wantConstruct)
			}
			if got.Selector != tc.wantSelector {
				t.Errorf("Selector = %q, want %q", got.Selector, tc.wantSelector)
			}
		})
	}
}

// TestSupportedSelectorsRecordNothing is the half that decides whether this
// feature is worth having. A diagnostic that fires on ordinary, fully-supported
// CSS is noise, and noise is worse than the silence it replaced — an author who
// learns to ignore the line learns to ignore the real one too.
//
// Every selector form the engine DOES implement must come through with a parsed
// rule and an empty Unsupported.
func TestSupportedSelectorsRecordNothing(t *testing.T) {
	for _, src := range []string{
		`p { color: red }`,                   // type
		`.intro { color: red }`,              // class
		`#lead { color: red }`,               // id
		`* { color: red }`,                   // universal
		`div p { color: red }`,               // descendant
		`div.card p.lead { color: red }`,     // descendant with compounds
		`h1, h2, h3 { color: red }`,          // grouping
		`div#a.b.c { color: red }`,           // compound
		`a:link { color: red }`,              // pseudo-class
		`li:first-child { color: red }`,      // structural pseudo-class
		`li:last-child { color: red }`,       //
		`li:only-child { color: red }`,       //
		`li:first-of-type { color: red }`,    //
		`li:nth-child(2n+1) { color: red }`,  // functional structural: contains '+'
		`li:nth-child(-n+3) { color: red }`,  // and a '+' after a '-'
		`li:nth-of-type(odd) { color: red }`, //
		`body > , p { color: red }`,          // a supported selector beside an unsupported one
	} {
		t.Run(src, func(t *testing.T) {
			sheet := Parse(src)
			if len(sheet.Rules) == 0 {
				t.Fatalf("Parse(%q) produced no rules; a supported selector was dropped", src)
			}
			if strings.Contains(src, ">") {
				return // the mixed-group case asserts only that the good half survived
			}
			if len(sheet.Unsupported) != 0 {
				t.Errorf("Parse(%q) reported %+v; a supported selector must record nothing",
					src, sheet.Unsupported)
			}
		})
	}
}

// TestValidCSSIsNeverBlamed is the sharpest edge of the negative half. A
// diagnostic that points at VALID css is worse than one that never fires: it
// sends the author to rewrite a rule that was fine.
//
// `li:nth-last-child(2n + 1)` is the case that nearly got this wrong. The engine
// does not support the spaced An+B form (parseOneSelector splits on whitespace,
// so the '+' lands in a field of its own — CLAUDE.md roadmap item 8, unchanged
// here), but the '+' is part of a functional pseudo's argument, NOT an
// adjacent-sibling combinator. It must be dropped silently, as malformed-for-this
// parser, never reported as a combinator the author did not write.
func TestValidCSSIsNeverBlamed(t *testing.T) {
	for _, src := range []string{
		`li:nth-last-child(2n + 1) { color: red }`,
		`li:nth-child( 2n + 1 ) { color: red }`,
		`li:nth-child(-n + 3) { color: red }`,
	} {
		sheet := Parse(src)
		if len(sheet.Unsupported) != 0 {
			t.Errorf("Parse(%q) reported %+v; the '+' is An+B syntax, not a sibling combinator",
				src, sheet.Unsupported)
		}
	}
}

// TestUnsupportedSelectorIsolatedWithinAGroup checks that one bad selector in a
// comma group neither takes its siblings down nor goes unreported. Both halves
// matter: dropping the whole group would be a regression, and reporting nothing
// would leave the author's `>` rule silently half-applied.
func TestUnsupportedSelectorIsolatedWithinAGroup(t *testing.T) {
	sheet := Parse(`.a > .b, .c { color: red }`)
	if len(sheet.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(sheet.Rules))
	}
	if n := len(sheet.Rules[0].Selectors); n != 1 {
		t.Errorf("the rule kept %d selectors, want 1 (only `.c` is representable)", n)
	}
	if len(sheet.Unsupported) != 1 || sheet.Unsupported[0].Construct != unsupportedChild {
		t.Errorf("Unsupported = %+v, want one child-combinator record", sheet.Unsupported)
	}
}

// TestUnsupportedSelectorsDeduplicate keeps a machine-generated sheet — which is
// exactly where these selectors come from — from retaining a record per rule.
func TestUnsupportedSelectorsDeduplicate(t *testing.T) {
	sheet := Parse(strings.Repeat(`.a > .b { color: red }`+"\n", 50))
	if len(sheet.Unsupported) != 1 {
		t.Errorf("50 identical dropped selectors recorded %d entries, want 1", len(sheet.Unsupported))
	}
}

// TestUnsupportedSelectorsAreCapped bounds the retained records for a sheet with
// many DISTINCT unsupported selectors (dedupe alone would not bound it).
func TestUnsupportedSelectorsAreCapped(t *testing.T) {
	var sb strings.Builder
	for i := range maxUnsupportedSelectors * 3 {
		sb.WriteString(".a" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + " > .b { color: red }\n")
	}
	sheet := Parse(sb.String())
	if len(sheet.Unsupported) > maxUnsupportedSelectors {
		t.Errorf("retained %d records, want at most %d", len(sheet.Unsupported), maxUnsupportedSelectors)
	}
	if len(sheet.Unsupported) == 0 {
		t.Error("retained nothing; the cap swallowed the diagnostic entirely")
	}
}

// TestPseudoElementDropsWithoutADiagnostic draws the line the feature depends
// on: a selector dropped for a reason that is NOT an unimplemented construct
// reports nothing. A pseudo-element must not match a normal element — the drop
// IS the correct behavior, so there is nothing to tell the author.
func TestPseudoElementDropsWithoutADiagnostic(t *testing.T) {
	for _, src := range []string{
		`p::before { content: "x" }`,
		`p:before { content: "x" }`,
		`p:not(.a) { color: red }`,
		`p:is(.a, .b) { color: red }`,
	} {
		sheet := Parse(src)
		if len(sheet.Unsupported) != 0 {
			t.Errorf("Parse(%q) reported %+v; only unimplemented SELECTOR CONSTRUCTS are reportable",
				src, sheet.Unsupported)
		}
	}
}

// TestUnsupportedSelectorsInsideMedia proves the records survive the @media fold
// — an @media block's rules are parsed by a recursive Parse whose result is
// merged, and a naive merge would drop the inner sheet's diagnostics.
func TestUnsupportedSelectorsInsideMedia(t *testing.T) {
	sheet := Parse(`@media print { .a > .b { color: red } }`)
	if len(sheet.Unsupported) != 1 || sheet.Unsupported[0].Construct != unsupportedChild {
		t.Errorf("Unsupported = %+v, want one child-combinator record from inside @media", sheet.Unsupported)
	}
}

// TestResolverReportsUnsupportedSelectorsOnce is the reporting half: NewResolver
// is the first point where every sheet and a logger are in hand, and it must emit
// ONE line per distinct construct across all author sheets.
func TestResolverReportsUnsupportedSelectorsOnce(t *testing.T) {
	var msgs []string
	logf := func(format string, args ...any) {
		msgs = append(msgs, fmt.Sprintf(format, args...))
	}
	sheets := []OriginSheet{
		{Sheet: Parse(`.a > .b { color: red } .c > .d { color: blue }`), Origin: OriginAuthor},
		{Sheet: Parse(`h1 + p { margin: 0 } h2 + p { margin: 0 }`), Origin: OriginAuthor},
	}
	NewResolver(sheets, logf)

	if len(msgs) != 2 {
		t.Fatalf("got %d diagnostics, want 2 (one per distinct construct):\n%s",
			len(msgs), strings.Join(msgs, "\n"))
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{unsupportedChild, unsupportedAdjacent} {
		if !strings.Contains(joined, want) {
			t.Errorf("no diagnostic named %q:\n%s", want, joined)
		}
	}
	// The message must quote the offending selector, or the author has to guess
	// which of their rules it means.
	if !strings.Contains(joined, ".a > .b") {
		t.Errorf("no diagnostic quotes the offending selector:\n%s", joined)
	}
}

// TestResolverIsSilentForSupportedSheets is the negative half at the reporting
// layer, and the one that makes the feature safe to ship: an ordinary stylesheet
// must produce no output at all.
func TestResolverIsSilentForSupportedSheets(t *testing.T) {
	var msgs []string
	logf := func(format string, args ...any) { msgs = append(msgs, fmt.Sprintf(format, args...)) }
	NewResolver([]OriginSheet{
		{Sheet: Parse(`body { margin: 0 } h1, h2 { color: #333 } div p.lead:first-child { font-weight: bold }`),
			Origin: OriginAuthor},
	}, logf)
	if len(msgs) != 0 {
		t.Errorf("a fully-supported stylesheet logged:\n%s", strings.Join(msgs, "\n"))
	}
}

// TestResolverDoesNotBlameTheUAStylesheet keeps the diagnostic pointed at the
// author. A UA sheet is the engine's own, written to what the parser supports;
// reporting one would blame the author for the engine — and would fire on EVERY
// document, since the UA sheet is always present.
func TestResolverDoesNotBlameTheUAStylesheet(t *testing.T) {
	var msgs []string
	logf := func(format string, args ...any) { msgs = append(msgs, fmt.Sprintf(format, args...)) }
	NewResolver([]OriginSheet{
		{Sheet: Parse(`.a > .b { color: red }`), Origin: OriginUA},
	}, logf)
	if len(msgs) != 0 {
		t.Errorf("a UA sheet produced an author-facing diagnostic:\n%s", strings.Join(msgs, "\n"))
	}
}
