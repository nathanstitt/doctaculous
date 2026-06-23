# CSS Parse + Cascade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a pure-Go, hand-written CSS engine (`pkg/css`) that tokenizes and parses CSS, matches
selectors against a DOM with correct specificity, and runs the cascade + inheritance to produce a
`ComputedStyle` per element — with no rendering, no layout, and no font work.

**Architecture:** This is sub-project 1 of the HTML-rendering program (see
`docs/superpowers/specs/2026-06-23-html-rendering-design.md`). It is the dependency-free root: it
consumes a minimal DOM-node interface and emits computed styles. It mirrors the existing DOCX style
cascade (`pkg/docx/style`): parse rules once, then resolve each node's effective properties
lowest-to-highest layer with unset-inherits semantics — but ordered by CSS specificity + source
order. Four cohesive files: tokenizer+parser, selectors, typed values, cascade.

**Tech Stack:** Pure Go (stdlib only — no new dependencies in this sub-project). `image/color` for
concrete colors, matching the box model. Standard `testing` table tests, hermetic, no network.

**Scope of the CSS property subset (this sub-project):** the cascade machinery is property-agnostic,
but `ComputedStyle` carries a deliberately minimal, real set chosen to be exactly what normal-flow
rendering (sub-project 3) will consume: `display`, `color`, `background-color`, `font-family`,
`font-size`, `font-weight`, `font-style`, `line-height`, `text-align`, the four `margin-*`, the four
`padding-*`, the four `border-*-width`/`-style`/`-color`, and `width`/`height`. Selectors handled:
type, universal (`*`), class, id, descendant combinator, and grouping (`,`). Everything outside this
set parses and is retained as raw declarations on the rule (so later sub-projects extend
`ComputedStyle` without re-parsing) but does not populate a typed computed field yet. This boundary
is stated in package docs so unhandled-but-parsed properties read as expected, not as a bug.

---

## File Structure

```
pkg/css/
   doc.go         package doc: what the package does, the cascade model, the property-subset boundary
   token.go       the tokenizer: CSS Syntax §4 token stream (idents, strings, numbers, dims, hash, delim, ...)
   token_test.go
   parse.go       the parser: token stream → Stylesheet (rules: selectors + declarations); @-rule skipping
   parse_test.go
   selector.go    selector model, parsing, specificity, and matching against a DOM Node
   selector_test.go
   value.go       typed property values: Length (px/pt/em/%/number), Color, keyword; parsing from tokens
   value_test.go
   cascade.go     ComputedStyle, the Resolver (collect → sort by specificity/order → apply → inherit → compute)
   cascade_test.go
   dom.go         the minimal Node interface the cascade matches against (so pkg/css doesn't import pkg/html)
```

**Why `dom.go` defines an interface, not a struct:** `pkg/css` must not depend on `pkg/html`
(that's sub-project 2 and would invert the layering). The cascade matches selectors against anything
satisfying a tiny `Node` interface; `pkg/html` implements it later. Tests in this sub-project use a
small in-file fake DOM.

---

## Task 1: Package skeleton and DOM interface

**Files:**
- Create: `pkg/css/doc.go`
- Create: `pkg/css/dom.go`
- Test: `pkg/css/dom_test.go`

- [ ] **Step 1: Write the failing test**

```go
// pkg/css/dom_test.go
package css

import "testing"

// fakeNode is the in-test DOM used throughout pkg/css tests.
type fakeNode struct {
	tag     string
	id      string
	classes []string
	parent  *fakeNode
	attrs   map[string]string
}

func (n *fakeNode) Tag() string             { return n.tag }
func (n *fakeNode) ID() string              { return n.id }
func (n *fakeNode) Classes() []string       { return n.classes }
func (n *fakeNode) Parent() Node            { if n.parent == nil { return nil }; return n.parent }
func (n *fakeNode) Attr(k string) (string, bool) { v, ok := n.attrs[k]; return v, ok }

func TestFakeNodeSatisfiesNode(t *testing.T) {
	var _ Node = (*fakeNode)(nil) // compile-time assertion the interface matches
	n := &fakeNode{tag: "p", id: "lead", classes: []string{"intro"}}
	if n.Tag() != "p" || n.ID() != "lead" || len(n.Classes()) != 1 {
		t.Fatalf("fakeNode accessors wrong: %+v", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestFakeNodeSatisfiesNode`
Expected: FAIL — `undefined: Node` (the interface doesn't exist yet).

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/css/dom.go
package css

// Node is the minimal read-only view of a DOM element the cascade matches
// selectors against. pkg/html implements it later (sub-project 2); pkg/css does
// not import pkg/html, so the layering stays one-directional. A nil Parent marks
// the root.
type Node interface {
	// Tag is the lowercased element name (e.g. "div"). Empty for non-elements.
	Tag() string
	// ID is the element's id attribute, or "" if absent.
	ID() string
	// Classes is the element's class list (already split on whitespace).
	Classes() []string
	// Parent is the element's parent, or nil at the root.
	Parent() Node
	// Attr returns an attribute value and whether it was present.
	Attr(key string) (string, bool)
}
```

```go
// pkg/css/doc.go
// Package css is a pure-Go, hand-written CSS engine: it tokenizes and parses CSS
// (CSS Syntax Level 3), matches selectors against a DOM with correct specificity,
// and runs the cascade and inheritance to compute the effective style of each
// element. It performs no layout and no rendering — it emits a ComputedStyle that
// the layout engine (pkg/layout/css) consumes.
//
// Scope boundary: the cascade is property-agnostic, but ComputedStyle carries a
// deliberately minimal typed property set (the normal-flow subset: display,
// color, background-color, the font-* group, line-height, text-align, margin/
// padding/border, width/height). Properties outside that set are parsed and kept
// as raw declarations on each rule so later sub-projects can extend ComputedStyle
// without re-parsing, but they do not yet populate a typed computed field. That
// is expected, not a gap.
//
// Like the rest of the toolkit, this package never panics on malformed input:
// unrecognized tokens, rules, and declarations are skipped (and optionally logged)
// so a single bad rule cannot discard a whole stylesheet.
package css
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestFakeNodeSatisfiesNode`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/css/doc.go pkg/css/dom.go pkg/css/dom_test.go
git commit -m "Add pkg/css skeleton and DOM Node interface"
```

---

## Task 2: Tokenizer — idents, whitespace, delimiters

**Files:**
- Create: `pkg/css/token.go`
- Test: `pkg/css/token_test.go`

- [ ] **Step 1: Write the failing test**

```go
// pkg/css/token_test.go
package css

import "testing"

func tokenKinds(src string) []TokenKind {
	var ks []TokenKind
	tz := newTokenizer(src)
	for {
		t := tz.next()
		if t.Kind == TokenEOF {
			break
		}
		ks = append(ks, t.Kind)
	}
	return ks
}

func TestTokenizeIdentsAndDelims(t *testing.T) {
	got := tokenKinds("div , .x")
	want := []TokenKind{TokenIdent, TokenWhitespace, TokenComma, TokenWhitespace, TokenDelim, TokenIdent}
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kind[%d] = %v, want %v (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestTokenizeIdentValue(t *testing.T) {
	tz := newTokenizer("margin-top")
	tok := tz.next()
	if tok.Kind != TokenIdent || tok.Text != "margin-top" {
		t.Fatalf("got %v %q, want Ident \"margin-top\"", tok.Kind, tok.Text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestTokenize`
Expected: FAIL — `undefined: newTokenizer` / `TokenKind`.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/css/token.go
package css

// TokenKind enumerates the CSS token types this engine recognizes. It is a
// pragmatic subset of CSS Syntax §4 sufficient for selectors and declarations.
type TokenKind int

const (
	TokenEOF TokenKind = iota
	TokenWhitespace
	TokenIdent      // a name: div, margin-top, red
	TokenHash       // #name  (id selector / hex color)
	TokenString     // "..." or '...'
	TokenNumber     // 12, 1.5, -3
	TokenDimension  // 12px, 1.5em
	TokenPercent    // 50%
	TokenDelim      // a single significant char: . > : * etc.
	TokenColon      // :
	TokenSemicolon  // ;
	TokenComma      // ,
	TokenLBrace     // {
	TokenRBrace     // }
	TokenLParen     // (
	TokenRParen     // )
)

// Token is one lexical unit. Text holds the token's source text (for Ident/String
// the decoded value; for Dimension the numeric+unit text); Num and Unit are set
// for Number/Dimension/Percent.
type Token struct {
	Kind TokenKind
	Text string
	Num  float64
	Unit string
}

type tokenizer struct {
	src string
	pos int
}

func newTokenizer(src string) *tokenizer { return &tokenizer{src: src} }

func (t *tokenizer) next() Token {
	if t.pos >= len(t.src) {
		return Token{Kind: TokenEOF}
	}
	c := t.src[t.pos]
	switch {
	case isWhitespace(c):
		start := t.pos
		for t.pos < len(t.src) && isWhitespace(t.src[t.pos]) {
			t.pos++
		}
		return Token{Kind: TokenWhitespace, Text: t.src[start:t.pos]}
	case c == ',':
		t.pos++
		return Token{Kind: TokenComma, Text: ","}
	case isNameStart(c):
		return t.readIdent()
	default:
		t.pos++
		return Token{Kind: TokenDelim, Text: string(c)}
	}
}

func (t *tokenizer) readIdent() Token {
	start := t.pos
	for t.pos < len(t.src) && isNameChar(t.src[t.pos]) {
		t.pos++
	}
	return Token{Kind: TokenIdent, Text: t.src[start:t.pos]}
}

func isWhitespace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
func isNameStart(c byte) bool {
	return c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}
func isNameChar(c byte) bool { return isNameStart(c) || (c >= '0' && c <= '9') }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestTokenize`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/css/token.go pkg/css/token_test.go
git commit -m "Add CSS tokenizer: idents, whitespace, delimiters"
```

---

## Task 3: Tokenizer — hashes, strings, numbers, dimensions, percentages

**Files:**
- Modify: `pkg/css/token.go`
- Test: `pkg/css/token_test.go`

- [ ] **Step 1: Write the failing test**

```go
// add to pkg/css/token_test.go
func TestTokenizeHashStringNumberDim(t *testing.T) {
	tz := newTokenizer(`#lead "hi" 12 1.5em 50% -3px`)
	type exp struct {
		k    TokenKind
		text string
		num  float64
		unit string
	}
	want := []exp{
		{TokenHash, "lead", 0, ""},
		{TokenWhitespace, " ", 0, ""},
		{TokenString, "hi", 0, ""},
		{TokenWhitespace, " ", 0, ""},
		{TokenNumber, "12", 12, ""},
		{TokenWhitespace, " ", 0, ""},
		{TokenDimension, "1.5em", 1.5, "em"},
		{TokenWhitespace, " ", 0, ""},
		{TokenPercent, "50%", 50, "%"},
		{TokenWhitespace, " ", 0, ""},
		{TokenDimension, "-3px", -3, "px"},
	}
	for i, w := range want {
		tok := tz.next()
		if tok.Kind != w.k || tok.Text != w.text || tok.Num != w.num || tok.Unit != w.unit {
			t.Fatalf("token[%d] = {%v %q %v %q}, want {%v %q %v %q}",
				i, tok.Kind, tok.Text, tok.Num, tok.Unit, w.k, w.text, w.num, w.unit)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestTokenizeHashStringNumberDim`
Expected: FAIL — hashes/strings/numbers tokenize wrong (currently `#` is a Delim, digits unhandled).

- [ ] **Step 3: Write minimal implementation**

**Ordering matters:** these cases MUST be inserted *before* the existing `case isNameStart(c)`.
`isNameStart('-')` is true, so a `-`-prefixed number like `-1px` would otherwise be swallowed as an
ident; placing the `c == '-' && next-is-digit/dot` case first makes `-1px` tokenize as a number.
(A lone `-` followed by a non-name char still falls through to `isNameStart`→ident rather than the
spec's `TokenDelim`; that is an accepted simplification — a bare `-` delimiter is not meaningful in
the declaration/selector subset this engine parses, and `--custom-props` still work via the
hyphen-then-hyphen ident path.)

```go
// in pkg/css/token.go, extend next()'s switch BEFORE the isNameStart case:
//	case c == '#':
//		t.pos++
//		id := t.readName()
//		return Token{Kind: TokenHash, Text: id}
//	case c == '"' || c == '\'':
//		return t.readString(c)
//	case c == '-' && t.pos+1 < len(t.src) && (isDigit(t.src[t.pos+1]) || t.src[t.pos+1] == '.'):
//		return t.readNumeric()
//	case isDigit(c):
//		return t.readNumeric()
//	case c == '.' && t.pos+1 < len(t.src) && isDigit(t.src[t.pos+1]):
//		return t.readNumeric()
//
// NOTE: digit and ".+digit" are SEPARATE cases on purpose. A combined
// `case isDigit(c) || c == '.'` is wrong: a lone "." (a class-selector marker,
// e.g. ".x") would then start a number instead of staying a TokenDelim that the
// selector parser needs. A "." begins a number only when a digit follows.
//
// and add these helpers:

func (t *tokenizer) readName() string {
	start := t.pos
	for t.pos < len(t.src) && isNameChar(t.src[t.pos]) {
		t.pos++
	}
	return t.src[start:t.pos]
}

func (t *tokenizer) readString(quote byte) Token {
	t.pos++ // opening quote
	start := t.pos
	for t.pos < len(t.src) && t.src[t.pos] != quote {
		t.pos++
	}
	s := t.src[start:t.pos]
	if t.pos < len(t.src) {
		t.pos++ // closing quote
	}
	return Token{Kind: TokenString, Text: s}
}

func (t *tokenizer) readNumeric() Token {
	start := t.pos
	if t.src[t.pos] == '-' {
		t.pos++
	}
	for t.pos < len(t.src) && (isDigit(t.src[t.pos]) || t.src[t.pos] == '.') {
		t.pos++
	}
	numText := t.src[start:t.pos]
	num := parseFloat(numText)
	if t.pos < len(t.src) && t.src[t.pos] == '%' {
		t.pos++
		return Token{Kind: TokenPercent, Text: numText + "%", Num: num, Unit: "%"}
	}
	if t.pos < len(t.src) && isNameStart(t.src[t.pos]) {
		unit := t.readName()
		return Token{Kind: TokenDimension, Text: numText + unit, Num: num, Unit: unit}
	}
	return Token{Kind: TokenNumber, Text: numText, Num: num}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// parseFloat parses a CSS number, returning 0 on malformed input (never panics).
func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
```

Add `import "strconv"` at the top of `token.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestTokenize`
Expected: PASS (both tokenizer tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/css/token.go pkg/css/token_test.go
git commit -m "Tokenize hashes, strings, numbers, dimensions, percentages"
```

---

## Task 4: Tokenizer — comments and colon/semicolon/braces/parens

**Files:**
- Modify: `pkg/css/token.go`
- Test: `pkg/css/token_test.go`

- [ ] **Step 1: Write the failing test**

```go
// add to pkg/css/token_test.go
func TestTokenizeCommentsAndPunctuation(t *testing.T) {
	// A comment is skipped entirely; punctuation gets its own kinds.
	got := tokenKinds("a /* note */ : ; { } ( )")
	want := []TokenKind{
		TokenIdent, TokenWhitespace, TokenWhitespace, TokenColon, TokenWhitespace,
		TokenSemicolon, TokenWhitespace, TokenLBrace, TokenWhitespace, TokenRBrace,
		TokenWhitespace, TokenLParen, TokenWhitespace, TokenRParen,
	}
	if len(got) != len(want) {
		t.Fatalf("kinds = %v (len %d), want len %d", got, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kind[%d] = %v, want %v (all %v)", i, got[i], want[i], got)
		}
	}
}

// TestTokenizeCommentEdgeCases locks in graceful degradation for malformed
// comments (project rule: degradation paths must be covered by a test).
func TestTokenizeCommentEdgeCases(t *testing.T) {
	if got := tokenKinds("/* no end"); len(got) != 0 {
		t.Errorf("unterminated comment kinds = %v, want none (consumed to EOF)", got)
	}
	if got := tokenKinds("/*"); len(got) != 0 {
		t.Errorf(`bare "/*" kinds = %v, want none`, got)
	}
	tz := newTokenizer("/")
	tok := tz.next()
	if tok.Kind != TokenDelim || tok.Text != "/" {
		t.Errorf(`lone "/" = {%v %q}, want {Delim "/"}`, tok.Kind, tok.Text)
	}
	got := tokenKinds("/*a*//*b*/x") // consecutive comments all skipped
	if len(got) != 1 || got[0] != TokenIdent {
		t.Errorf(`"/*a*//*b*/x" kinds = %v, want [Ident]`, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestTokenize`
Expected: FAIL — `:` `;` `{` `}` `(` `)` come back as Delim, and `/* */` is not skipped.

- [ ] **Step 3: Write minimal implementation**

Comment skipping must be ITERATIVE, not recursive. Restructure `next()` so its body sits inside a
`for` loop and a comment is skipped with `continue` (NOT `t.skipComment(); return t.next()` — that
recurses one stack frame per comment, so a pathological run of millions of `/**/` could exhaust the
goroutine stack, a fatal error). Re-read `c := t.src[t.pos]` inside the loop after `skipComment`
advances `pos`:

```go
func (t *tokenizer) next() Token {
	for {
		if t.pos >= len(t.src) {
			return Token{Kind: TokenEOF}
		}
		c := t.src[t.pos]
		if c == '/' && t.pos+1 < len(t.src) && t.src[t.pos+1] == '*' {
			t.skipComment()
			continue
		}
		switch {
		// ... all the existing cases (whitespace, comma, #, string, numerics,
		//     isNameStart, default) — each returns, so the loop only re-iterates
		//     after a comment is skipped — PLUS the six punctuation cases below.
		}
	}
}
```

Add the six punctuation cases into that switch, before the `default`:
```go
//	case c == ':':
//		t.pos++
//		return Token{Kind: TokenColon, Text: ":"}
//	case c == ';':
//		t.pos++
//		return Token{Kind: TokenSemicolon, Text: ";"}
//	case c == '{':
//		t.pos++
//		return Token{Kind: TokenLBrace, Text: "{"}
//	case c == '}':
//		t.pos++
//		return Token{Kind: TokenRBrace, Text: "}"}
//	case c == '(':
//		t.pos++
//		return Token{Kind: TokenLParen, Text: "("}
//	case c == ')':
//		t.pos++
//		return Token{Kind: TokenRParen, Text: ")"}

func (t *tokenizer) skipComment() {
	t.pos += 2 // consume /*
	for t.pos+1 < len(t.src) {
		if t.src[t.pos] == '*' && t.src[t.pos+1] == '/' {
			t.pos += 2
			return
		}
		t.pos++
	}
	t.pos = len(t.src) // unterminated comment: consume to EOF
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/`
Expected: PASS (all tokenizer tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/css/token.go pkg/css/token_test.go
git commit -m "Tokenize comments and structural punctuation"
```

---

## Task 5: Typed values — Length

**Files:**
- Create: `pkg/css/value.go`
- Test: `pkg/css/value_test.go`

- [ ] **Step 1: Write the failing test**

```go
// pkg/css/value_test.go
package css

import "testing"

func TestParseLength(t *testing.T) {
	cases := []struct {
		in       string
		val      float64
		unit     LengthUnit
		ok       bool
	}{
		{"12px", 12, UnitPx, true},
		{"1.5em", 1.5, UnitEm, true},
		{"50%", 50, UnitPercent, true},
		{"0", 0, UnitPx, true},   // unitless zero is a length of 0
		{"10pt", 10, UnitPt, true},
		{"auto", 0, UnitAuto, true},
		{"red", 0, UnitPx, false}, // not a length
	}
	for _, c := range cases {
		got, ok := parseLength(newTokenizer(c.in).next())
		if ok != c.ok {
			t.Fatalf("parseLength(%q) ok = %v, want %v", c.in, ok, c.ok)
		}
		if ok && (got.Value != c.val || got.Unit != c.unit) {
			t.Fatalf("parseLength(%q) = {%v %v}, want {%v %v}", c.in, got.Value, got.Unit, c.val, c.unit)
		}
	}
}
```

Note: the `auto` case needs the parser to accept an Ident token; `parseLength` takes a single token
and special-cases the `auto` keyword.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestParseLength`
Expected: FAIL — `undefined: parseLength` / `Length`.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/css/value.go
package css

// LengthUnit is the unit of a CSS length value.
type LengthUnit int

const (
	UnitPx LengthUnit = iota
	UnitPt
	UnitEm
	UnitPercent
	UnitAuto // the "auto" keyword, modeled as a length so width/margin can carry it
)

// Length is a CSS length value: a magnitude plus its unit. Percentages and the
// "auto" keyword are represented here too so a single type covers width/height/
// margin/padding values. Resolution to absolute points (resolving em/% against a
// context) happens in the layout engine, not here.
type Length struct {
	Value float64
	Unit  LengthUnit
}

// parseLength interprets a single token as a length. A unitless 0 is a valid
// zero length; the "auto" keyword yields UnitAuto. ok is false for tokens that
// are not lengths (e.g. a color keyword).
func parseLength(tok Token) (Length, bool) {
	switch tok.Kind {
	case TokenDimension:
		switch tok.Unit {
		case "px":
			return Length{tok.Num, UnitPx}, true
		case "pt":
			return Length{tok.Num, UnitPt}, true
		case "em", "rem":
			return Length{tok.Num, UnitEm}, true
		default:
			return Length{}, false
		}
	case TokenPercent:
		return Length{tok.Num, UnitPercent}, true
	case TokenNumber:
		if tok.Num == 0 {
			return Length{0, UnitPx}, true
		}
		return Length{}, false // non-zero unitless is not a valid length
	case TokenIdent:
		if tok.Text == "auto" {
			return Length{0, UnitAuto}, true
		}
	}
	return Length{}, false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestParseLength`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/css/value.go pkg/css/value_test.go
git commit -m "Add CSS Length value type and parsing"
```

---

## Task 6: Typed values — Color (named, #hex, rgb())

**Files:**
- Modify: `pkg/css/value.go`
- Test: `pkg/css/value_test.go`

- [ ] **Step 1: Write the failing test**

```go
// add to pkg/css/value_test.go
import "image/color"

func TestParseColor(t *testing.T) {
	cases := []struct {
		in   string
		want color.RGBA
		ok   bool
	}{
		{"#000000", color.RGBA{0, 0, 0, 255}, true},
		{"#fff", color.RGBA{255, 255, 255, 255}, true},
		{"#ff0000", color.RGBA{255, 0, 0, 255}, true},
		{"red", color.RGBA{255, 0, 0, 255}, true},
		{"white", color.RGBA{255, 255, 255, 255}, true},
		{"transparent", color.RGBA{0, 0, 0, 0}, true},
		{"rgb(0,128,255)", color.RGBA{0, 128, 255, 255}, true},
		{"notacolor", color.RGBA{}, false},
	}
	for _, c := range cases {
		got, ok := parseColor(newTokenizer(c.in))
		if ok != c.ok {
			t.Fatalf("parseColor(%q) ok = %v, want %v", c.in, ok, c.ok)
		}
		if ok && got != c.want {
			t.Fatalf("parseColor(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
```

Note: `parseColor` takes the *tokenizer* (not one token) because `rgb(...)` and `#hex` may span
multiple tokens / need the hash text.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestParseColor`
Expected: FAIL — `undefined: parseColor`.

- [ ] **Step 3: Write minimal implementation**

```go
// add to pkg/css/value.go
import (
	"image/color"
	"strconv"
	"strings"
)

// namedColors is the minimal CSS named-color set this sub-project recognizes.
// Extend as needed; unknown names fail parseColor (the declaration is dropped).
var namedColors = map[string]color.RGBA{
	"black":       {0, 0, 0, 255},
	"white":       {255, 255, 255, 255},
	"red":         {255, 0, 0, 255},
	"green":       {0, 128, 0, 255},
	"blue":        {0, 0, 255, 255},
	"gray":        {128, 128, 128, 255},
	"silver":      {192, 192, 192, 255},
	"transparent": {0, 0, 0, 0},
}

// parseColor reads a color value from the tokenizer: a #hex hash, an rgb(r,g,b)
// function, or a named color. ok is false for anything unrecognized, so the
// caller drops the declaration.
func parseColor(tz *tokenizer) (color.RGBA, bool) {
	tok := tz.next()
	switch tok.Kind {
	case TokenHash:
		return parseHex(tok.Text)
	case TokenIdent:
		if tok.Text == "rgb" {
			return parseRGBFunc(tz)
		}
		c, ok := namedColors[strings.ToLower(tok.Text)]
		return c, ok
	}
	return color.RGBA{}, false
}

func parseHex(h string) (color.RGBA, bool) {
	switch len(h) {
	case 3:
		r := hexNibble(h[0])
		g := hexNibble(h[1])
		b := hexNibble(h[2])
		if r < 0 || g < 0 || b < 0 {
			return color.RGBA{}, false
		}
		return color.RGBA{uint8(r*16 + r), uint8(g*16 + g), uint8(b*16 + b), 255}, true
	case 6:
		r, err1 := strconv.ParseUint(h[0:2], 16, 8)
		g, err2 := strconv.ParseUint(h[2:4], 16, 8)
		b, err3 := strconv.ParseUint(h[4:6], 16, 8)
		if err1 != nil || err2 != nil || err3 != nil {
			return color.RGBA{}, false
		}
		return color.RGBA{uint8(r), uint8(g), uint8(b), 255}, true
	}
	return color.RGBA{}, false
}

func hexNibble(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// parseRGBFunc parses the remainder of rgb(r,g,b) after the "rgb" ident, with the
// tokenizer positioned at the "(".
func parseRGBFunc(tz *tokenizer) (color.RGBA, bool) {
	if tz.next().Kind != TokenLParen {
		return color.RGBA{}, false
	}
	var comps [3]uint8
	for i := 0; i < 3; i++ {
		// skip whitespace, read a number
		tok := nextNonWhitespace(tz)
		if tok.Kind != TokenNumber {
			return color.RGBA{}, false
		}
		comps[i] = clampByte(tok.Num)
		if i < 2 {
			if nextNonWhitespace(tz).Kind != TokenComma {
				return color.RGBA{}, false
			}
		}
	}
	if nextNonWhitespace(tz).Kind != TokenRParen {
		return color.RGBA{}, false
	}
	return color.RGBA{comps[0], comps[1], comps[2], 255}, true
}

func nextNonWhitespace(tz *tokenizer) Token {
	for {
		t := tz.next()
		if t.Kind != TokenWhitespace {
			return t
		}
	}
}

func clampByte(f float64) uint8 {
	if f < 0 {
		return 0
	}
	if f > 255 {
		return 255
	}
	return uint8(f)
}
```

Remove the now-duplicate `import "strconv"` if Task 3 already added it to a different file — each
file has its own imports, so `value.go` needs its own `strconv`, `strings`, `image/color`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestParseColor`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/css/value.go pkg/css/value_test.go
git commit -m "Add CSS color parsing: named, hex, rgb()"
```

---

## Task 7: Selector model, parsing, and specificity

**Files:**
- Create: `pkg/css/selector.go`
- Test: `pkg/css/selector_test.go`

- [ ] **Step 1: Write the failing test**

```go
// pkg/css/selector_test.go
package css

import "testing"

func TestSelectorSpecificity(t *testing.T) {
	cases := []struct {
		in   string
		spec Specificity // {ids, classes, types}
	}{
		{"*", Specificity{0, 0, 0}},
		{"div", Specificity{0, 0, 1}},
		{".intro", Specificity{0, 1, 0}},
		{"#lead", Specificity{1, 0, 0}},
		{"div.intro", Specificity{0, 1, 1}},
		{"div p", Specificity{0, 0, 2}},
		{"#lead .intro p", Specificity{1, 1, 1}},
	}
	for _, c := range cases {
		sels := parseSelectorList(c.in)
		if len(sels) != 1 {
			t.Fatalf("parseSelectorList(%q) = %d selectors, want 1", c.in, len(sels))
		}
		if sels[0].Specificity() != c.spec {
			t.Fatalf("%q specificity = %v, want %v", c.in, sels[0].Specificity(), c.spec)
		}
	}
}

func TestParseSelectorGroup(t *testing.T) {
	sels := parseSelectorList("h1, h2, .title")
	if len(sels) != 3 {
		t.Fatalf("got %d selectors, want 3", len(sels))
	}
}
```

(Delete the empty `TestParseSelectorSpecificity` placeholder line — it's shown only to mark where the
file starts; the real tests are the two below it.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestSelector`
Expected: FAIL — `undefined: parseSelectorList` / `Specificity`.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/css/selector.go
package css

import "strings"

// Specificity is a CSS specificity triple (a,b,c): id count, class count, type
// count. Compared field-by-field, a dominates b dominates c.
type Specificity struct {
	IDs, Classes, Types int
}

// Less reports whether s is lower specificity than o.
func (s Specificity) Less(o Specificity) bool {
	if s.IDs != o.IDs {
		return s.IDs < o.IDs
	}
	if s.Classes != o.Classes {
		return s.Classes < o.Classes
	}
	return s.Types < o.Types
}

// simpleSelector matches a single element: an optional type, plus any number of
// class and id qualifiers. A universal "*" sets neither type nor qualifiers.
type simpleSelector struct {
	tag     string // "" means any (universal or qualifier-only)
	id      string
	classes []string
}

// Selector is a sequence of simpleSelectors joined by descendant combinators,
// read left (ancestor) to right (subject). The last element is the subject the
// selector matches.
type Selector struct {
	parts []simpleSelector
}

// Specificity sums the selector's parts.
func (s Selector) Specificity() Specificity {
	var sp Specificity
	for _, p := range s.parts {
		if p.id != "" {
			sp.IDs++
		}
		sp.Classes += len(p.classes)
		if p.tag != "" {
			sp.Types++
		}
	}
	return sp
}

// parseSelectorList parses a comma-separated selector group into individual
// Selectors. Whitespace between simple selectors is a descendant combinator.
// Parsing is total: a malformed group is skipped rather than erroring, so one bad
// selector cannot void a rule's other selectors.
func parseSelectorList(src string) []Selector {
	var out []Selector
	for _, group := range strings.Split(src, ",") {
		sel, ok := parseOneSelector(strings.TrimSpace(group))
		if ok {
			out = append(out, sel)
		}
	}
	return out
}

func parseOneSelector(src string) (Selector, bool) {
	fields := strings.Fields(src) // descendant combinator = whitespace
	if len(fields) == 0 {
		return Selector{}, false
	}
	var sel Selector
	for _, f := range fields {
		ss, ok := parseSimple(f)
		if !ok {
			return Selector{}, false
		}
		sel.parts = append(sel.parts, ss)
	}
	return sel, true
}

// parseSimple parses one compound simple selector like "div.intro#lead" or "*".
func parseSimple(f string) (simpleSelector, bool) {
	var ss simpleSelector
	if f == "*" {
		return ss, true
	}
	i := 0
	// leading type selector
	for i < len(f) && f[i] != '.' && f[i] != '#' {
		i++
	}
	ss.tag = strings.ToLower(f[:i])
	for i < len(f) {
		marker := f[i]
		i++
		start := i
		for i < len(f) && f[i] != '.' && f[i] != '#' {
			i++
		}
		name := f[start:i]
		if name == "" {
			return simpleSelector{}, false
		}
		switch marker {
		case '.':
			ss.classes = append(ss.classes, name)
		case '#':
			ss.id = name
		}
	}
	return ss, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestSelector` and `go test ./pkg/css/ -run TestParseSelectorGroup`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/css/selector.go pkg/css/selector_test.go
git commit -m "Add CSS selector parsing and specificity"
```

---

## Task 8: Selector matching against a DOM

**Files:**
- Modify: `pkg/css/selector.go`
- Test: `pkg/css/selector_test.go`

- [ ] **Step 1: Write the failing test**

```go
// add to pkg/css/selector_test.go
func TestSelectorMatch(t *testing.T) {
	// Tree: div#main > p.intro
	div := &fakeNode{tag: "div", id: "main"}
	p := &fakeNode{tag: "p", classes: []string{"intro"}, parent: div}

	mustMatch := func(sel string, n *fakeNode, want bool) {
		sels := parseSelectorList(sel)
		got := sels[0].Matches(n)
		if got != want {
			t.Fatalf("%q matches %s#%s.%v = %v, want %v", sel, n.tag, n.id, n.classes, got, want)
		}
	}
	mustMatch("p", p, true)
	mustMatch("p.intro", p, true)
	mustMatch("p.missing", p, false)
	mustMatch("div p", p, true)        // descendant
	mustMatch("#main p", p, true)      // descendant via id
	mustMatch("p p", p, false)         // no matching ancestor
	mustMatch("div", p, false)         // subject must be the node itself
	mustMatch("*", p, true)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestSelectorMatch`
Expected: FAIL — `Selector` has no method `Matches`.

- [ ] **Step 3: Write minimal implementation**

```go
// add to pkg/css/selector.go

// Matches reports whether the selector matches node n. The last part must match
// n itself; earlier parts must each match some ancestor, in order (descendant
// combinator). Matching walks ancestors greedily from the subject outward.
func (s Selector) Matches(n Node) bool {
	if len(s.parts) == 0 {
		return false
	}
	last := len(s.parts) - 1
	if !s.parts[last].matches(n) {
		return false
	}
	// Match remaining parts (right-to-left) against ancestors.
	cur := n.Parent()
	i := last - 1
	for i >= 0 {
		matched := false
		for cur != nil {
			if s.parts[i].matches(cur) {
				cur = cur.Parent()
				matched = true
				break
			}
			cur = cur.Parent()
		}
		if !matched {
			return false
		}
		i--
	}
	return true
}

// matches reports whether a single simple selector matches node n.
func (ss simpleSelector) matches(n Node) bool {
	if ss.tag != "" && ss.tag != n.Tag() {
		return false
	}
	if ss.id != "" && ss.id != n.ID() {
		return false
	}
	for _, c := range ss.classes {
		if !hasClass(n.Classes(), c) {
			return false
		}
	}
	return true
}

func hasClass(have []string, want string) bool {
	for _, c := range have {
		if c == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestSelectorMatch`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/css/selector.go pkg/css/selector_test.go
git commit -m "Add CSS selector matching against a DOM"
```

---

## Task 9: Parser — declarations

**Files:**
- Create: `pkg/css/parse.go`
- Test: `pkg/css/parse_test.go`

- [ ] **Step 1: Write the failing test**

```go
// pkg/css/parse_test.go
package css

import "testing"

func TestParseDeclarations(t *testing.T) {
	decls := parseDeclarations("color: red; margin-top: 10px; ; bogus")
	// The empty declaration and the value-less "bogus" are dropped.
	if len(decls) != 2 {
		t.Fatalf("got %d declarations, want 2: %+v", len(decls), decls)
	}
	if decls[0].Property != "color" || decls[0].Value != "red" {
		t.Fatalf("decl[0] = %+v, want {color red}", decls[0])
	}
	if decls[1].Property != "margin-top" || decls[1].Value != "10px" {
		t.Fatalf("decl[1] = %+v, want {margin-top 10px}", decls[1])
	}
}

func TestParseDeclarationImportant(t *testing.T) {
	decls := parseDeclarations("color: red !important")
	if len(decls) != 1 || !decls[0].Important || decls[0].Value != "red" {
		t.Fatalf("decl = %+v, want {color red important=true}", decls)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestParseDeclaration`
Expected: FAIL — `undefined: parseDeclarations`.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/css/parse.go
package css

import "strings"

// Declaration is one property: value pair from a rule body, with the !important
// flag. Value is the raw value text (trimmed); typed interpretation happens in
// the cascade so unknown properties are retained losslessly.
type Declaration struct {
	Property  string
	Value     string
	Important bool
}

// parseDeclarations parses a rule body (the text between { and }) into
// declarations. Malformed declarations (no colon, empty property, empty value)
// are skipped so one bad declaration cannot void the rest.
func parseDeclarations(body string) []Declaration {
	var out []Declaration
	for _, chunk := range strings.Split(body, ";") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		colon := strings.IndexByte(chunk, ':')
		if colon < 0 {
			continue
		}
		prop := strings.TrimSpace(chunk[:colon])
		val := strings.TrimSpace(chunk[colon+1:])
		if prop == "" || val == "" {
			continue
		}
		important := false
		if i := strings.LastIndex(strings.ToLower(val), "!important"); i >= 0 {
			important = true
			val = strings.TrimSpace(val[:i])
		}
		if val == "" {
			continue
		}
		out = append(out, Declaration{Property: strings.ToLower(prop), Value: val, Important: important})
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestParseDeclaration`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/css/parse.go pkg/css/parse_test.go
git commit -m "Add CSS declaration parsing with !important"
```

---

## Task 10: Parser — rules and stylesheet (with @-rule skipping)

**Files:**
- Modify: `pkg/css/parse.go`
- Test: `pkg/css/parse_test.go`

- [ ] **Step 1: Write the failing test**

```go
// add to pkg/css/parse_test.go
func TestParseStylesheet(t *testing.T) {
	src := `
		/* comment */
		h1, .title { color: red; font-size: 24px; }
		p { margin-top: 10px }
		@media print { p { color: black } }   /* whole at-rule skipped */
	`
	sheet := Parse(src)
	if len(sheet.Rules) != 2 {
		t.Fatalf("got %d rules, want 2 (the @media block is skipped): %+v", len(sheet.Rules), sheet.Rules)
	}
	// First rule has 2 selectors and 2 declarations.
	if len(sheet.Rules[0].Selectors) != 2 {
		t.Fatalf("rule[0] selectors = %d, want 2", len(sheet.Rules[0].Selectors))
	}
	if len(sheet.Rules[0].Declarations) != 2 {
		t.Fatalf("rule[0] declarations = %d, want 2", len(sheet.Rules[0].Declarations))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestParseStylesheet`
Expected: FAIL — `undefined: Parse` / `Stylesheet`.

- [ ] **Step 3: Write minimal implementation**

```go
// add to pkg/css/parse.go

// Rule is a style rule: a selector group plus its declarations.
type Rule struct {
	Selectors    []Selector
	Declarations []Declaration
}

// Stylesheet is a parsed CSS document: an ordered list of style rules. Source
// order is preserved because the cascade uses it as a tie-breaker.
type Stylesheet struct {
	Rules []Rule
}

// Parse parses a CSS stylesheet. It is total: malformed rules and unsupported
// at-rules are skipped (their block consumed) rather than aborting the parse, so
// a single bad construct cannot discard the sheet. Comments are stripped by the
// tokenizer's awareness; here we scan the raw source for rule boundaries using a
// brace-matching pass that is comment- and string-tolerant.
func Parse(src string) Stylesheet {
	var sheet Stylesheet
	s := &ruleScanner{src: src}
	for {
		prelude, body, ok := s.nextRule()
		if !ok {
			break
		}
		prelude = strings.TrimSpace(prelude)
		if prelude == "" {
			continue
		}
		if strings.HasPrefix(prelude, "@") {
			continue // unsupported at-rule: block already consumed by the scanner
		}
		sels := parseSelectorList(prelude)
		if len(sels) == 0 {
			continue
		}
		sheet.Rules = append(sheet.Rules, Rule{
			Selectors:    sels,
			Declarations: parseDeclarations(body),
		})
	}
	return sheet
}

// ruleScanner walks the source returning (prelude, body) pairs for each top-level
// {...} block. It skips /* */ comments and is tolerant of quotes so braces inside
// strings/comments do not confuse boundary detection.
type ruleScanner struct {
	src string
	pos int
}

func (s *ruleScanner) nextRule() (prelude, body string, ok bool) {
	start := s.pos
	for s.pos < len(s.src) {
		switch {
		case s.atComment():
			s.skipComment()
		case s.src[s.pos] == '{':
			prelude = s.src[start:s.pos]
			s.pos++ // consume {
			body = s.readBody()
			return prelude, body, true
		default:
			s.pos++
		}
	}
	return "", "", false
}

// readBody returns the text up to the matching close brace, consuming it, and
// handling one level of nesting (so an at-rule block like @media{ p{} } is fully
// consumed even though we then discard it).
func (s *ruleScanner) readBody() string {
	start := s.pos
	depth := 0
	for s.pos < len(s.src) {
		switch {
		case s.atComment():
			s.skipComment()
		case s.src[s.pos] == '{':
			depth++
			s.pos++
		case s.src[s.pos] == '}':
			if depth == 0 {
				body := s.src[start:s.pos]
				s.pos++ // consume }
				return body
			}
			depth--
			s.pos++
		default:
			s.pos++
		}
	}
	return s.src[start:s.pos]
}

func (s *ruleScanner) atComment() bool {
	return s.pos+1 < len(s.src) && s.src[s.pos] == '/' && s.src[s.pos+1] == '*'
}

func (s *ruleScanner) skipComment() {
	s.pos += 2
	for s.pos+1 < len(s.src) {
		if s.src[s.pos] == '*' && s.src[s.pos+1] == '/' {
			s.pos += 2
			return
		}
		s.pos++
	}
	s.pos = len(s.src)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/`
Expected: PASS (all parser tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/css/parse.go pkg/css/parse_test.go
git commit -m "Add CSS rule and stylesheet parsing with at-rule skipping"
```

---

## Task 11: ComputedStyle and inheritance defaults

**Files:**
- Create: `pkg/css/cascade.go`
- Test: `pkg/css/cascade_test.go`

- [ ] **Step 1: Write the failing test**

```go
// pkg/css/cascade_test.go
package css

import (
	"image/color"
	"testing"
)

func TestInitialComputedStyle(t *testing.T) {
	cs := initialStyle()
	if cs.Display != "inline" { // CSS initial value of display is inline
		t.Fatalf("initial display = %q, want inline", cs.Display)
	}
	if cs.Color != (color.RGBA{0, 0, 0, 255}) {
		t.Fatalf("initial color = %v, want black", cs.Color)
	}
	if cs.FontSizePt != 16 { // 16px default medium, expressed in px-as-pt placeholder
		t.Fatalf("initial font-size = %v, want 16", cs.FontSizePt)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestInitialComputedStyle`
Expected: FAIL — `undefined: initialStyle` / `ComputedStyle`.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/css/cascade.go
package css

import "image/color"

// ComputedStyle is the resolved style of one element: the normal-flow property
// subset this sub-project supports, with every value concrete. Lengths remain in
// their CSS unit here (px/pt/em/%); the layout engine resolves em/% to absolute
// points against a containing context. Raw, unrecognized declarations are not on
// this struct — they are retained on the Rule for later sub-projects.
type ComputedStyle struct {
	Display string // "block" | "inline" | "none" | "list-item" | raw value

	Color           color.RGBA
	BackgroundColor color.RGBA // zero-alpha means transparent / not set

	FontFamily string
	FontSizePt float64 // resolved to an absolute size (px treated 1:1 as pt for now)
	Bold       bool
	Italic     bool
	LineHeight Length // UnitAuto = "normal"

	TextAlign string // "left" | "right" | "center" | "justify"

	MarginTop, MarginRight, MarginBottom, MarginLeft     Length
	PaddingTop, PaddingRight, PaddingBottom, PaddingLeft Length

	BorderTopWidth, BorderRightWidth, BorderBottomWidth, BorderLeftWidth Length
	BorderTopColor, BorderRightColor, BorderBottomColor, BorderLeftColor color.RGBA
	BorderTopStyle, BorderRightStyle, BorderBottomStyle, BorderLeftStyle string

	Width, Height Length // UnitAuto = "auto"
}

// initialStyle returns a ComputedStyle holding the CSS initial values, used as
// the base for the root element before any rule or inheritance is applied.
func initialStyle() ComputedStyle {
	black := color.RGBA{0, 0, 0, 255}
	return ComputedStyle{
		Display:     "inline",
		Color:       black,
		FontFamily:  "serif",
		FontSizePt:  16,
		LineHeight:  Length{Unit: UnitAuto},
		TextAlign:   "left",
		Width:       Length{Unit: UnitAuto},
		Height:      Length{Unit: UnitAuto},
		MarginTop:   Length{Unit: UnitPx},
		MarginRight: Length{Unit: UnitPx},
		// remaining margins/paddings default to zero px (the zero value of Length is {0,UnitPx})
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestInitialComputedStyle`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/css/cascade.go pkg/css/cascade_test.go
git commit -m "Add ComputedStyle struct and CSS initial values"
```

---

## Task 12: Applying a single declaration to a ComputedStyle

**Files:**
- Modify: `pkg/css/cascade.go`
- Test: `pkg/css/cascade_test.go`

- [ ] **Step 1: Write the failing test**

```go
// add to pkg/css/cascade_test.go
func TestApplyDeclaration(t *testing.T) {
	cs := initialStyle()
	apply := func(prop, val string) { applyDeclaration(&cs, Declaration{Property: prop, Value: val}) }

	apply("display", "block")
	apply("color", "red")
	apply("background-color", "#ffffff")
	apply("font-weight", "bold")
	apply("font-style", "italic")
	apply("text-align", "center")
	apply("margin-top", "10px")
	apply("width", "50%")

	if cs.Display != "block" {
		t.Errorf("display = %q", cs.Display)
	}
	if cs.Color != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("color = %v", cs.Color)
	}
	if cs.BackgroundColor != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("background = %v", cs.BackgroundColor)
	}
	if !cs.Bold {
		t.Errorf("bold not set")
	}
	if !cs.Italic {
		t.Errorf("italic not set")
	}
	if cs.TextAlign != "center" {
		t.Errorf("text-align = %q", cs.TextAlign)
	}
	if cs.MarginTop != (Length{10, UnitPx}) {
		t.Errorf("margin-top = %v", cs.MarginTop)
	}
	if cs.Width != (Length{50, UnitPercent}) {
		t.Errorf("width = %v", cs.Width)
	}
}

func TestApplyUnknownPropertyIgnored(t *testing.T) {
	cs := initialStyle()
	before := cs
	applyDeclaration(&cs, Declaration{Property: "transform", Value: "rotate(5deg)"})
	if cs != before {
		t.Fatalf("unknown property changed the computed style")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestApply`
Expected: FAIL — `undefined: applyDeclaration`.

- [ ] **Step 3: Write minimal implementation**

```go
// add to pkg/css/cascade.go

// applyDeclaration interprets one declaration and writes it onto cs. Properties
// outside the supported normal-flow subset are ignored (left for later
// sub-projects). Malformed values are dropped, leaving the prior value intact.
func applyDeclaration(cs *ComputedStyle, d Declaration) {
	switch d.Property {
	case "display":
		cs.Display = d.Value
	case "color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.Color = c
		}
	case "background-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BackgroundColor = c
		}
	case "font-family":
		cs.FontFamily = firstFamily(d.Value)
	case "font-size":
		if l, ok := parseLength(newTokenizer(d.Value).next()); ok && l.Unit != UnitAuto {
			cs.FontSizePt = l.Value // px:pt 1:1 for now; em/% resolution is the engine's job
		}
	case "font-weight":
		cs.Bold = d.Value == "bold" || d.Value == "700" || d.Value == "800" || d.Value == "900"
	case "font-style":
		cs.Italic = d.Value == "italic" || d.Value == "oblique"
	case "line-height":
		if l, ok := parseLength(newTokenizer(d.Value).next()); ok {
			cs.LineHeight = l
		} else if d.Value == "normal" {
			cs.LineHeight = Length{Unit: UnitAuto}
		}
	case "text-align":
		switch d.Value {
		case "left", "right", "center", "justify":
			cs.TextAlign = d.Value
		}
	case "margin-top":
		setLength(&cs.MarginTop, d.Value)
	case "margin-right":
		setLength(&cs.MarginRight, d.Value)
	case "margin-bottom":
		setLength(&cs.MarginBottom, d.Value)
	case "margin-left":
		setLength(&cs.MarginLeft, d.Value)
	case "padding-top":
		setLength(&cs.PaddingTop, d.Value)
	case "padding-right":
		setLength(&cs.PaddingRight, d.Value)
	case "padding-bottom":
		setLength(&cs.PaddingBottom, d.Value)
	case "padding-left":
		setLength(&cs.PaddingLeft, d.Value)
	case "width":
		setLength(&cs.Width, d.Value)
	case "height":
		setLength(&cs.Height, d.Value)
	case "border-top-width":
		setLength(&cs.BorderTopWidth, d.Value)
	case "border-right-width":
		setLength(&cs.BorderRightWidth, d.Value)
	case "border-bottom-width":
		setLength(&cs.BorderBottomWidth, d.Value)
	case "border-left-width":
		setLength(&cs.BorderLeftWidth, d.Value)
	case "border-top-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderTopColor = c
		}
	case "border-right-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderRightColor = c
		}
	case "border-bottom-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderBottomColor = c
		}
	case "border-left-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderLeftColor = c
		}
	case "border-top-style":
		cs.BorderTopStyle = d.Value
	case "border-right-style":
		cs.BorderRightStyle = d.Value
	case "border-bottom-style":
		cs.BorderBottomStyle = d.Value
	case "border-left-style":
		cs.BorderLeftStyle = d.Value
	}
	// default: unsupported property — ignored on purpose.
}

// setLength parses val as a length and writes it to dst when valid.
func setLength(dst *Length, val string) {
	if l, ok := parseLength(newTokenizer(val).next()); ok {
		*dst = l
	}
}

// firstFamily returns the first family name from a font-family list, stripping
// quotes and whitespace (e.g. `"Helvetica Neue", Arial` → `Helvetica Neue`).
func firstFamily(val string) string {
	for _, part := range splitComma(val) {
		part = trimQuotes(strings.TrimSpace(part))
		if part != "" {
			return part
		}
	}
	return val
}
```

Add `import "strings"` to `cascade.go`, and these tiny helpers (place in `cascade.go`):

```go
func splitComma(s string) []string { return strings.Split(s, ",") }

func trimQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestApply`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/css/cascade.go pkg/css/cascade_test.go
git commit -m "Apply CSS declarations onto ComputedStyle (normal-flow subset)"
```

---

## Task 13: The Resolver — cascade order, specificity, inheritance

**Files:**
- Modify: `pkg/css/cascade.go`
- Test: `pkg/css/cascade_test.go`

- [ ] **Step 1: Write the failing test**

```go
// add to pkg/css/cascade_test.go
func TestCascadeSpecificityAndInheritance(t *testing.T) {
	src := `
		p { color: green; font-size: 12px; }
		.intro { color: blue; }
		#lead { color: red; }
	`
	sheet := Parse(src)
	r := NewResolver(sheet, nil)

	body := &fakeNode{tag: "body"}
	// <p id="lead" class="intro"> inside <body>
	p := &fakeNode{tag: "p", id: "lead", classes: []string{"intro"}, parent: body}

	cs := r.Compute(p, initialStyle())
	// #lead (id) beats .intro (class) beats p (type): red wins.
	if cs.Color != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("color = %v, want red (id wins)", cs.Color)
	}
	// font-size only set by the p rule: 12.
	if cs.FontSizePt != 12 {
		t.Errorf("font-size = %v, want 12", cs.FontSizePt)
	}
}

func TestCascadeInheritsColorButNotMargin(t *testing.T) {
	src := `div { color: blue; margin-top: 20px; }`
	sheet := Parse(src)
	r := NewResolver(sheet, nil)

	div := &fakeNode{tag: "div"}
	child := &fakeNode{tag: "span", parent: div}

	divStyle := r.Compute(div, initialStyle())
	childStyle := r.Compute(child, divStyle) // parent's computed style is the inheritance base

	// color inherits:
	if childStyle.Color != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("child color = %v, want inherited blue", childStyle.Color)
	}
	// margin does NOT inherit: child keeps the initial 0.
	if childStyle.MarginTop != (Length{0, UnitPx}) {
		t.Errorf("child margin-top = %v, want 0 (not inherited)", childStyle.MarginTop)
	}
}

func TestCascadeImportantWins(t *testing.T) {
	src := `
		#lead { color: red; }
		p { color: green !important; }
	`
	sheet := Parse(src)
	r := NewResolver(sheet, nil)
	p := &fakeNode{tag: "p", id: "lead"}
	cs := r.Compute(p, initialStyle())
	// !important beats higher specificity.
	if cs.Color != (color.RGBA{0, 128, 0, 255}) {
		t.Errorf("color = %v, want green (!important wins over id)", cs.Color)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestCascade`
Expected: FAIL — `undefined: NewResolver` / `(*Resolver).Compute`.

- [ ] **Step 3: Write minimal implementation**

```go
// add to pkg/css/cascade.go
import "sort"  // add to the cascade.go import block

// Resolver computes the ComputedStyle of any node against a parsed stylesheet.
// Build one per stylesheet with NewResolver; it is read-only after construction
// and safe for concurrent use. logf may be nil.
type Resolver struct {
	sheet Stylesheet
	logf  func(string, ...any)
}

// NewResolver builds a Resolver over a parsed stylesheet.
func NewResolver(sheet Stylesheet, logf func(string, ...any)) *Resolver {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Resolver{sheet: sheet, logf: logf}
}

// inheritedProperties are the subset that inherit from parent to child per CSS.
// Non-inherited properties (margin, padding, border, width, height, display,
// background) reset to their initial value on each element.
//
// We implement inheritance by starting each element's computed style from a base
// derived from the parent's computed style: inherited fields are copied, the rest
// reset to initial. matchedDecls then overlay the element's own cascade.

// Compute returns node n's ComputedStyle. parentStyle is the already-computed
// style of n's parent (use initialStyle() for the root). The cascade is: start
// from the inheritance base, then apply every matching declaration in increasing
// (specificity, source-order) order, with !important declarations applied last.
func (r *Resolver) Compute(n Node, parentStyle ComputedStyle) ComputedStyle {
	cs := inheritFrom(parentStyle)

	type matched struct {
		decl  Declaration
		spec  Specificity
		order int
	}
	var normal, important []matched

	order := 0
	for ri := range r.sheet.Rules {
		rule := &r.sheet.Rules[ri]
		spec, ok := bestMatch(rule.Selectors, n)
		if !ok {
			continue
		}
		for _, d := range rule.Declarations {
			m := matched{decl: d, spec: spec, order: order}
			if d.Important {
				important = append(important, m)
			} else {
				normal = append(normal, m)
			}
			order++
		}
	}

	less := func(a, b matched) bool {
		if a.spec.Less(b.spec) {
			return true
		}
		if b.spec.Less(a.spec) {
			return false
		}
		return a.order < b.order // later source order wins, so sort ascending and apply in order
	}
	sort.SliceStable(normal, func(i, j int) bool { return less(normal[i], normal[j]) })

	// 1. normal author rules, lowest to highest (specificity, then source order).
	for _, m := range normal {
		applyDeclaration(&cs, m.decl)
	}

	// 2. inline style="" attribute is inserted here in Task 14 (it outranks all
	//    normal rules; its !important declarations are appended to `important`).

	// 3. !important declarations overlay last so they always win. Sorting happens
	//    here — after the inline block so inline-important is included.
	sort.SliceStable(important, func(i, j int) bool { return less(important[i], important[j]) })
	for _, m := range important {
		applyDeclaration(&cs, m.decl)
	}
	return cs
}

// bestMatch returns the highest specificity among a rule's selectors that match
// n, and whether any matched.
func bestMatch(sels []Selector, n Node) (Specificity, bool) {
	var best Specificity
	found := false
	for _, s := range sels {
		if s.Matches(n) {
			if !found || best.Less(s.Specificity()) {
				best = s.Specificity()
				found = true
			}
		}
	}
	return best, found
}

// inheritFrom builds an element's base style: inherited properties carry over
// from the parent's computed style; everything else resets to initial.
func inheritFrom(parent ComputedStyle) ComputedStyle {
	cs := initialStyle()
	// Inherited properties (CSS): color, font-*, line-height, text-align.
	cs.Color = parent.Color
	cs.FontFamily = parent.FontFamily
	cs.FontSizePt = parent.FontSizePt
	cs.Bold = parent.Bold
	cs.Italic = parent.Italic
	cs.LineHeight = parent.LineHeight
	cs.TextAlign = parent.TextAlign
	return cs
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestCascade`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/css/cascade.go pkg/css/cascade_test.go
git commit -m "Add CSS cascade Resolver: specificity, source order, inheritance, !important"
```

---

## Task 14: Inline style attribute (highest non-important origin)

**Files:**
- Modify: `pkg/css/cascade.go`
- Test: `pkg/css/cascade_test.go`

- [ ] **Step 1: Write the failing test**

```go
// add to pkg/css/cascade_test.go
func TestInlineStyleAttributeWins(t *testing.T) {
	src := `#lead { color: red; }`
	sheet := Parse(src)
	r := NewResolver(sheet, nil)
	// style="color: green" must beat the id rule (inline style has higher origin).
	p := &fakeNode{tag: "p", id: "lead", attrs: map[string]string{"style": "color: green"}}
	cs := r.Compute(p, initialStyle())
	if cs.Color != (color.RGBA{0, 128, 0, 255}) {
		t.Errorf("color = %v, want green (inline style wins)", cs.Color)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestInlineStyleAttributeWins`
Expected: FAIL — inline `style=""` is currently ignored, so red (the id rule) wins.

- [ ] **Step 3: Write minimal implementation**

```go
Task 13 left a marked insertion point in `Compute` between step 1 (apply `normal`) and step 3 (sort
+ apply `important`). Replace that comment marker:

```go
	// 2. inline style="" attribute is inserted here in Task 14 (it outranks all
	//    normal rules; its !important declarations are appended to `important`).
```

with the inline-style handling — a pure insertion, no other code in `Compute` changes:

```go
	// 2. inline style="" attribute. Its normal declarations overlay all normal
	//    rules regardless of their specificity; its !important declarations join
	//    the important set with an outsized specificity so inline-important is the
	//    strongest author origin (matching the CSS cascade origin order). Because
	//    the `important` slice is sorted in step 3 (below, after this block), the
	//    appended entries are included in that sort — no re-sorting needed.
	if styleAttr, ok := n.Attr("style"); ok {
		for _, d := range parseDeclarations(styleAttr) {
			if d.Important {
				important = append(important, matched{decl: d, spec: Specificity{IDs: 1 << 20}, order: order})
				order++
				continue
			}
			applyDeclaration(&cs, d)
		}
	}
```

This works because Task 13 deliberately deferred the `important` sort to *after* this marker, so the
inline `!important` declarations appended here are sorted and applied together with the author
`!important` declarations in step 3.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/`
Expected: PASS (all cascade tests, including the earlier ones).

- [ ] **Step 5: Commit**

```bash
git add pkg/css/cascade.go pkg/css/cascade_test.go
git commit -m "Honor inline style attribute in the CSS cascade"
```

---

## Task 15: Whole-package integration test, vet, lint, and roadmap update

**Files:**
- Create: `pkg/css/integration_test.go`
- Modify: `CLAUDE.md` (status/roadmap)

- [ ] **Step 1: Write the failing test**

```go
// pkg/css/integration_test.go
package css

import (
	"image/color"
	"testing"
)

// TestEndToEndCascade exercises parse → resolver → compute on a small realistic
// sheet and DOM, the way sub-project 2 (box generation) will call this package.
func TestEndToEndCascade(t *testing.T) {
	src := `
		body { font-family: Arial; font-size: 16px; color: #222222; }
		h1 { font-size: 32px; font-weight: bold; }
		.note { color: gray; background-color: #eeeeee; padding-left: 8px; }
		p { margin-top: 1em; line-height: 1.5; }
	`
	sheet := Parse(src)
	r := NewResolver(sheet, nil)

	body := &fakeNode{tag: "body"}
	bodyCS := r.Compute(body, initialStyle())

	h1 := &fakeNode{tag: "h1", parent: body}
	h1CS := r.Compute(h1, bodyCS)
	if h1CS.FontSizePt != 32 || !h1CS.Bold {
		t.Errorf("h1 = {size %v bold %v}, want {32 true}", h1CS.FontSizePt, h1CS.Bold)
	}
	// font-family inherits from body:
	if h1CS.FontFamily != "Arial" {
		t.Errorf("h1 font-family = %q, want inherited Arial", h1CS.FontFamily)
	}

	p := &fakeNode{tag: "p", classes: []string{"note"}, parent: body}
	pCS := r.Compute(p, bodyCS)
	if pCS.Color != (color.RGBA{128, 128, 128, 255}) {
		t.Errorf("p.note color = %v, want gray", pCS.Color)
	}
	if pCS.BackgroundColor != (color.RGBA{0xee, 0xee, 0xee, 255}) {
		t.Errorf("p.note background = %v", pCS.BackgroundColor)
	}
	if pCS.MarginTop != (Length{1, UnitEm}) {
		t.Errorf("p margin-top = %v, want 1em", pCS.MarginTop)
	}
	if pCS.LineHeight != (Length{1.5, UnitPx}) && pCS.LineHeight.Value != 1.5 {
		t.Errorf("p line-height = %v, want 1.5", pCS.LineHeight)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (or passes cleanly)**

Run: `go test ./pkg/css/ -run TestEndToEndCascade`
Expected: PASS if Tasks 1–14 are correct. If it FAILS, fix the offending unit before proceeding —
this test is the acceptance gate for the sub-project. (A unitless `line-height: 1.5` parses as a
`TokenNumber` with non-zero value, which `parseLength` rejects; the assertion tolerates either the
number form or a future unitless-multiplier form. If you prefer, extend `parseLength` to accept a
unitless number as `Length{1.5, UnitNumber}` and add `UnitNumber`; that is an allowed refinement but
not required to pass.)

- [ ] **Step 3: Run the full quality gate**

Run:
```bash
gofmt -l pkg/css/        # expect: no output (all formatted)
go vet ./pkg/css/        # expect: no output
go test ./pkg/css/ -v    # expect: all PASS
go test ./...            # expect: the rest of the module still passes (no cross-package breakage)
```
Expected: all clean. Fix anything that is not (per project policy, fix all vet/lint findings
regardless of origin).

- [ ] **Step 4: Update the roadmap in CLAUDE.md**

In `CLAUDE.md`, under "Status & roadmap", move CSS parsing/cascade from pending toward done. Add a
bullet under the reflow "Done" area noting: *"CSS engine (`pkg/css`): hand-written tokenizer +
parser, selector matching (type/universal/class/id/descendant/grouping) with specificity, and the
cascade (specificity + source order + inheritance + !important + inline style) producing a
ComputedStyle for the normal-flow property subset. No layout/rendering yet — consumed by box
generation (next)."* And in TODO item 6 (HTML/EPUB), note that the CSS parse+cascade layer is the
first landed slice.

- [ ] **Step 5: Commit**

```bash
git add pkg/css/integration_test.go CLAUDE.md
git commit -m "Add pkg/css integration test; record CSS parse+cascade in roadmap"
```

---

## Self-Review notes (for the implementer)

- **Spec coverage:** This plan implements sub-project 1 of the design
  (`docs/superpowers/specs/2026-06-23-html-rendering-design.md` §5, row 1): the hand-written CSS
  tokenizer/parser, selectors + specificity, and cascade + inheritance + computed values, with no
  rendering or layout (§3 `pkg/css`). The `Node` interface keeps `pkg/css` from importing `pkg/html`
  (§3 note). Graceful degradation (§6) is realized as skip-on-malformed in the tokenizer, parser,
  declaration parsing, and `applyDeclaration` (unknown properties ignored). No new dependency is
  added (§9 — CSS is hand-written).
- **Deferred by design:** pseudo-classes/elements, attribute selectors, child/sibling combinators,
  `@media`/`@font-face` application, shorthand expansion (`margin: 1px 2px`), `em`/`%` *resolution*
  (the engine's job), and unitless `line-height` multipliers beyond the optional refinement in
  Task 15. These belong to later sub-projects per §10 and are intentionally absent.
- **Type consistency check:** `Token`/`TokenKind` (Task 2–4), `Length`/`LengthUnit` (Task 5),
  `parseColor` taking a `*tokenizer` (Task 6), `Selector`/`Specificity`/`Matches` (Task 7–8),
  `Declaration`/`Rule`/`Stylesheet`/`Parse` (Task 9–10), `ComputedStyle`/`initialStyle`/
  `applyDeclaration` (Task 11–12), `Resolver`/`NewResolver`/`Compute` (Task 13–14) — names are used
  consistently across tasks. `Compute` takes the parent's `ComputedStyle` as the inheritance base in
  every test that calls it.
