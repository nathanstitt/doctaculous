# CSS Flexbox (single-line) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the block fallback a `display:flex` box currently uses into a real single-line flex layout (`flex-direction`, `flex-basis`/`grow`/`shrink` + `flex` shorthand, `justify-content`, `align-items`/`align-self`, min/max + the automatic minimum, `gap`, `order`, `inline-flex`), reusing the existing block/inline engine for each item's contents.

**Architecture:** A new `pkg/layout/css/flex.go` holds `layoutFlex`, written in abstract main/cross coordinates (one algorithm for row/column/reverses), registered as `case cssbox.FlexFC` in `block.go`'s formatting-context switch (mirroring `layoutTable`). The §9.7 flexible-length resolution is carved out as a pure, dependency-free `resolveFlexibleLengths` for hand-computed unit tests. Flex properties land in `pkg/css` (cascade + shorthands); an anonymous-flex-item fixup lands in `pkg/layout/css/flexfix.go`. The `render.Device` seam, the PDF/DOCX pipelines, and the shared inline core are untouched.

**Tech Stack:** Pure Go. `pkg/css` (hand-written cascade), `pkg/layout/cssbox` (box tree), `pkg/layout/css` (layout engine), `pkg/doctaculous` (golden/reftest harness). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-06-26-html-flexbox-design.md`. CSS Flexbox L1: <https://www.w3.org/TR/css-flexbox-1/>.

---

## Conventions for every task (read once)

- **Branch:** You are on `feat/html-flexbox`. Do NOT checkout/stash/switch branches. Do NOT commit unless a step says to. Before finishing a task, confirm `git status` is clean of scratch files and `find . -name 'zz_*'` is empty — delete any probe/scratch file you created.
- **Sandbox:** Run every `go`, `gofmt`, and `golangci-lint` command with `dangerouslyDisableSandbox: true`. A sandboxed Go/lint command fails with cache/TLS/"no go files to analyze" errors that are NOT real — re-run disabled.
- **Lint:** `golangci-lint` here does NOT gofmt — run `gofmt -l <pkg-dir>` separately. Lint specific packages, not the repo root. NO `//nolint`. The repo declines "modernize" hints: keep explicit `if x < y { x = y }` clamps (not `max`/`min`), indexed `for i := 0; i < n; i++` loops (not range-over-int), `sort.SliceStable`. Write the De Morgan form (not `if !(a && b)`). Use `_ = x.Close()` for ignored errors. The `unused` linter is enforced — only add a struct field/const when a consumer in the SAME task reads it.
- **Editor diagnostics lag** — trust `go build`/`go test`/`find`, not the editor panel (it shows phantom "undefined"/"unused" errors and deleted scratch files).
- **Test harness (layout):** `e := New(nil, nil, nil)` builds an engine; build a `*cssbox.Box` tree directly; `frag := e.layoutTree(context.Background(), body, viewportW)` returns the root `*Fragment`; walk `frag.Children` recursively and assert `.X/.Y/.W/.H` (the border-box rect in page space). Use `DebugTag` / `debugTag(b)` to find a specific fragment.
- **Test harness (cascade):** `cs := initialStyle()`; `applyOne(&cs, "prop", "value")` (helper in `pkg/css/shorthand_test.go`) runs one declaration through `applyDeclaration`; assert `cs.Field`.

---

## File structure

**Created:**
- `pkg/layout/css/flex.go` — `layoutFlex` + the `flexAxis` helper + `resolveFlexibleLengths` + item collection/sizing/alignment.
- `pkg/layout/css/flexfix.go` — `fixupFlex`: anonymous-flex-item wrapping + inline-level item blockification + `order` is read in layout (not here).
- `pkg/layout/css/flex_resolve_test.go` — unit tests for the pure `resolveFlexibleLengths`.
- `pkg/layout/css/flex_layout_test.go` — fragment-geometry tests for `layoutFlex`.
- `pkg/layout/css/flexfix_test.go` — structural tests for the fixup.
- `pkg/doctaculous/testdata/wpt/flex-*.html` + `flex-*-ref.html` — reftest pairs.
- Golden PNGs under `pkg/doctaculous/testdata/golden-html/` (generated; names per Task 12).

**Modified:**
- `pkg/css/value.go` — add `UnitContent` length unit (for `flex-basis: content`).
- `pkg/css/cascade.go` — add flex fields to `ComputedStyle`, their defaults in `initialStyle`, and `applyDeclaration` cases.
- `pkg/css/shorthand.go` — `flex` and `gap` shorthand expansion.
- `pkg/css/cascade_test.go` / `pkg/css/shorthand_test.go` — cascade + shorthand tests.
- `pkg/layout/cssbox/box.go` — add `BoxAnonFlexItem` kind (Task 4, when the fixup reads it).
- `pkg/layout/css/build.go` — call `fixupFlex(root)` after `fixupTables(root)`.
- `pkg/layout/css/block.go` — add `case cssbox.FlexFC: in = e.layoutFlex(...)` to the FC switch.
- `pkg/doctaculous/html_golden_test.go` / `wpt_reftest_test.go` — register new goldens/reftests.
- `CLAUDE.md` — Done bullet + §6 parenthetical (final task).

---

## Task 1: `flex-basis: content` unit + the container/item flex properties on `ComputedStyle`

**Files:**
- Modify: `pkg/css/value.go` (add `UnitContent`)
- Modify: `pkg/css/cascade.go` (`ComputedStyle` fields + `initialStyle` defaults)
- Test: `pkg/css/cascade_test.go`

- [ ] **Step 1: Write the failing test** — append to `pkg/css/cascade_test.go`:

```go
func TestFlexContainerProperties(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "flex-direction", "column")
	applyOne(&cs, "flex-wrap", "wrap")
	applyOne(&cs, "justify-content", "space-between")
	applyOne(&cs, "align-items", "center")
	applyOne(&cs, "column-gap", "12px")
	applyOne(&cs, "row-gap", "8px")
	if cs.FlexDirection != "column" {
		t.Errorf("flex-direction = %q, want column", cs.FlexDirection)
	}
	if cs.FlexWrap != "wrap" {
		t.Errorf("flex-wrap = %q, want wrap", cs.FlexWrap)
	}
	if cs.JustifyContent != "space-between" {
		t.Errorf("justify-content = %q, want space-between", cs.JustifyContent)
	}
	if cs.AlignItems != "center" {
		t.Errorf("align-items = %q, want center", cs.AlignItems)
	}
	if cs.ColumnGap != (Length{12, UnitPx}) {
		t.Errorf("column-gap = %v, want 12px", cs.ColumnGap)
	}
	if cs.RowGap != (Length{8, UnitPx}) {
		t.Errorf("row-gap = %v, want 8px", cs.RowGap)
	}
}

func TestFlexItemProperties(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "flex-grow", "2")
	applyOne(&cs, "flex-shrink", "0")
	applyOne(&cs, "flex-basis", "100px")
	applyOne(&cs, "align-self", "flex-end")
	applyOne(&cs, "order", "-1")
	if cs.FlexGrow != 2 {
		t.Errorf("flex-grow = %v, want 2", cs.FlexGrow)
	}
	if cs.FlexShrink != 0 {
		t.Errorf("flex-shrink = %v, want 0", cs.FlexShrink)
	}
	if cs.FlexBasis != (Length{100, UnitPx}) {
		t.Errorf("flex-basis = %v, want 100px", cs.FlexBasis)
	}
	if cs.AlignSelf != "flex-end" {
		t.Errorf("align-self = %q, want flex-end", cs.AlignSelf)
	}
	if cs.Order != -1 {
		t.Errorf("order = %v, want -1", cs.Order)
	}
}

func TestFlexBasisContentAndAuto(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "flex-basis", "content")
	if cs.FlexBasis.Unit != UnitContent {
		t.Errorf("flex-basis:content unit = %v, want UnitContent", cs.FlexBasis.Unit)
	}
	cs2 := initialStyle()
	if cs2.FlexBasis.Unit != UnitAuto {
		t.Errorf("default flex-basis unit = %v, want UnitAuto", cs2.FlexBasis.Unit)
	}
	if cs2.FlexShrink != 1 {
		t.Errorf("default flex-shrink = %v, want 1", cs2.FlexShrink)
	}
}

func TestFlexUnknownValueIgnored(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "justify-content", "bogus")
	if cs.JustifyContent != "flex-start" {
		t.Errorf("justify-content after bogus = %q, want flex-start (unchanged)", cs.JustifyContent)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/css -run 'TestFlex' -v` (with `dangerouslyDisableSandbox: true`)
Expected: FAIL — `cs.FlexDirection` undefined / `UnitContent` undefined.

- [ ] **Step 3: Add the `UnitContent` unit** — in `pkg/css/value.go`, extend the unit const block:

```go
const (
	UnitPx LengthUnit = iota
	UnitPt
	UnitEm
	UnitPercent
	UnitAuto    // the "auto" keyword, modeled as a length so width/margin can carry it
	UnitContent // the flex-basis "content" keyword (only produced/read by flex-basis)
)
```

- [ ] **Step 4: Add the fields to `ComputedStyle`** — in `pkg/css/cascade.go`, inside `type ComputedStyle struct`, add a grouped block (place near the other layout fields):

```go
	// Flexbox (CSS Flexbox L1). Container properties read on a display:flex box;
	// item properties read on each flex item. Defaults set in initialStyle.
	FlexDirection  string // row | row-reverse | column | column-reverse
	FlexWrap       string // nowrap | wrap | wrap-reverse (only nowrap acted on today)
	JustifyContent string // flex-start | flex-end | center | space-between | space-around | space-evenly
	AlignItems     string // stretch | flex-start | flex-end | center | baseline
	AlignSelf      string // auto | stretch | flex-start | flex-end | center | baseline
	ColumnGap      Length // main-axis gap for row, cross-axis gap for column
	RowGap         Length // cross-axis gap for row, main-axis gap for column
	FlexGrow       float64
	FlexShrink     float64
	FlexBasis      Length // length | percentage | UnitAuto ("auto") | UnitContent ("content")
	Order          int
```

- [ ] **Step 5: Set the defaults in `initialStyle`** — in `pkg/css/cascade.go`'s `initialStyle()`, add to the returned struct literal:

```go
		FlexDirection:  "row",
		FlexWrap:       "nowrap",
		JustifyContent: "flex-start",
		AlignItems:     "stretch",
		AlignSelf:      "auto",
		FlexGrow:       0,
		FlexShrink:     1,
		FlexBasis:      Length{Unit: UnitAuto},
		Order:          0,
		// ColumnGap, RowGap default to the zero Length ({0, UnitPx}) = no gap.
```

- [ ] **Step 6: Add the `applyDeclaration` cases** — in `pkg/css/cascade.go`'s `applyDeclaration` switch, add (keyword properties validate against an allow-set so unknown values are ignored):

```go
	case "flex-direction":
		switch d.Value {
		case "row", "row-reverse", "column", "column-reverse":
			cs.FlexDirection = d.Value
		}
	case "flex-wrap":
		switch d.Value {
		case "nowrap", "wrap", "wrap-reverse":
			cs.FlexWrap = d.Value
		}
	case "justify-content":
		switch d.Value {
		case "flex-start", "flex-end", "center", "space-between", "space-around", "space-evenly":
			cs.JustifyContent = d.Value
		}
	case "align-items":
		switch d.Value {
		case "stretch", "flex-start", "flex-end", "center", "baseline":
			cs.AlignItems = d.Value
		}
	case "align-self":
		switch d.Value {
		case "auto", "stretch", "flex-start", "flex-end", "center", "baseline":
			cs.AlignSelf = d.Value
		}
	case "column-gap":
		if l, ok := parseGapLength(d.Value); ok {
			cs.ColumnGap = l
		}
	case "row-gap":
		if l, ok := parseGapLength(d.Value); ok {
			cs.RowGap = l
		}
	case "flex-grow":
		if v, ok := parseNonNegNumber(d.Value); ok {
			cs.FlexGrow = v
		}
	case "flex-shrink":
		if v, ok := parseNonNegNumber(d.Value); ok {
			cs.FlexShrink = v
		}
	case "flex-basis":
		if l, ok := parseFlexBasis(d.Value); ok {
			cs.FlexBasis = l
		}
	case "order":
		if n, ok := parseInteger(d.Value); ok {
			cs.Order = n
		}
```

- [ ] **Step 7: Add the small parse helpers** — in `pkg/css/cascade.go` (near the other parse helpers), add:

```go
// parseNonNegNumber parses a unitless non-negative number (flex-grow/flex-shrink).
// A negative or non-numeric value yields ok=false (the property keeps its prior value).
func parseNonNegNumber(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// parseInteger parses a signed integer (order). Non-integers yield ok=false.
func parseInteger(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return n, true
}

// parseGapLength parses a row-gap/column-gap value. "normal" is the initial value
// and means zero gap. Lengths/percentages parse normally; "auto" is invalid for gap.
func parseGapLength(s string) (Length, bool) {
	if strings.TrimSpace(s) == "normal" {
		return Length{0, UnitPx}, true
	}
	l, ok := parseLength(newTokenizer(s).next())
	if !ok || l.Unit == UnitAuto {
		return Length{}, false
	}
	return l, true
}

// parseFlexBasis parses a flex-basis value: "auto", "content", or a length/percentage.
func parseFlexBasis(s string) (Length, bool) {
	switch strings.TrimSpace(s) {
	case "auto":
		return Length{Unit: UnitAuto}, true
	case "content":
		return Length{Unit: UnitContent}, true
	}
	l, ok := parseLength(newTokenizer(s).next())
	if !ok || l.Unit == UnitAuto {
		return Length{}, false
	}
	return l, true
}
```

(If `strconv`/`strings` are not yet imported in `cascade.go`, add them — `strconv` is used by `font-size`/`font-weight` parsing already; confirm with `goimports`.)

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./pkg/css -run 'TestFlex' -v` (disable sandbox)
Expected: PASS (all four tests).

- [ ] **Step 9: Format, vet, lint**

Run: `gofmt -l pkg/css && go vet ./pkg/css && golangci-lint run ./pkg/css/...` (disable sandbox)
Expected: no output from `gofmt -l`; vet/lint clean.

- [ ] **Step 10: Commit**

```bash
git add pkg/css/value.go pkg/css/cascade.go pkg/css/cascade_test.go
git commit -m "css: parse flex properties + flex-basis content unit"
```

---

## Task 2: `flex` and `gap` shorthand expansion

**Files:**
- Modify: `pkg/css/shorthand.go`
- Modify: `pkg/css/cascade.go` (dispatch `flex`/`gap` to the expanders in `applyDeclaration`)
- Test: `pkg/css/shorthand_test.go`

- [ ] **Step 1: Write the failing test** — append to `pkg/css/shorthand_test.go`:

```go
func TestFlexShorthandKeywords(t *testing.T) {
	cases := []struct {
		val               string
		grow, shrink      float64
		basisVal          float64
		basisUnit         LengthUnit
	}{
		{"none", 0, 0, 0, UnitAuto},
		{"auto", 1, 1, 0, UnitAuto},
		{"initial", 0, 1, 0, UnitAuto},
		{"1", 1, 1, 0, UnitPercent},     // flex:<number> => <n> 1 0%
		{"2 3", 2, 3, 0, UnitPercent},   // flex:<g> <s> => g s 0%
		{"100px", 1, 1, 100, UnitPx},    // flex:<length> => 1 1 <length>
		{"2 100px", 2, 1, 100, UnitPx},  // flex:<g> <basis> => g 1 basis
		{"2 0 50px", 2, 0, 50, UnitPx},  // flex:<g> <s> <basis>
	}
	for _, c := range cases {
		cs := initialStyle()
		applyOne(&cs, "flex", c.val)
		if cs.FlexGrow != c.grow || cs.FlexShrink != c.shrink {
			t.Errorf("flex:%q grow/shrink = %v/%v, want %v/%v", c.val, cs.FlexGrow, cs.FlexShrink, c.grow, c.shrink)
		}
		if cs.FlexBasis.Unit != c.basisUnit || cs.FlexBasis.Value != c.basisVal {
			t.Errorf("flex:%q basis = %v, want {%v %v}", c.val, cs.FlexBasis, c.basisVal, c.basisUnit)
		}
	}
}

func TestGapShorthand(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "gap", "10px")
	if cs.RowGap != (Length{10, UnitPx}) || cs.ColumnGap != (Length{10, UnitPx}) {
		t.Errorf("gap:10px = row %v col %v, want both 10px", cs.RowGap, cs.ColumnGap)
	}
	cs2 := initialStyle()
	applyOne(&cs2, "gap", "10px 20px")
	if cs2.RowGap != (Length{10, UnitPx}) || cs2.ColumnGap != (Length{20, UnitPx}) {
		t.Errorf("gap:10px 20px = row %v col %v, want 10px/20px", cs2.RowGap, cs2.ColumnGap)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/css -run 'TestFlexShorthand|TestGapShorthand' -v` (disable sandbox)
Expected: FAIL — `flex`/`gap` not handled (basis stays auto, gaps stay 0).

- [ ] **Step 3: Add the expanders** — in `pkg/css/shorthand.go`, add:

```go
// applyFlexShorthand expands the `flex` shorthand into flex-grow/flex-shrink/flex-basis
// (CSS Flexbox 7.2). Keyword forms: none=>0 0 auto, auto=>1 1 auto, initial=>0 1 auto.
// Numeric forms: <g> => g 1 0%; <g> <s> => g s 0%; <length> => 1 1 <length>;
// <g> <basis> => g 1 basis; <g> <s> <basis> => as written. A token is a "basis" if it
// is a length/percentage/auto/content; otherwise it is a number (grow then shrink).
func applyFlexShorthand(cs *ComputedStyle, val string) {
	v := strings.TrimSpace(val)
	switch v {
	case "none":
		cs.FlexGrow, cs.FlexShrink, cs.FlexBasis = 0, 0, Length{Unit: UnitAuto}
		return
	case "auto":
		cs.FlexGrow, cs.FlexShrink, cs.FlexBasis = 1, 1, Length{Unit: UnitAuto}
		return
	case "initial":
		cs.FlexGrow, cs.FlexShrink, cs.FlexBasis = 0, 1, Length{Unit: UnitAuto}
		return
	}
	fields := strings.Fields(v)
	if len(fields) == 0 || len(fields) > 3 {
		return // malformed: leave prior values
	}
	// Defaults per the spec when a component is omitted from a non-keyword flex value:
	// grow 1, shrink 1, basis 0%.
	grow, shrink := 1.0, 1.0
	basis := Length{0, UnitPercent}
	var nums []float64
	var gotBasis bool
	for _, f := range fields {
		if b, ok := parseFlexBasis(f); ok && isBasisToken(f) {
			basis, gotBasis = b, true
			continue
		}
		n, ok := parseNonNegNumber(f)
		if !ok {
			return // unrecognized token: leave prior values
		}
		nums = append(nums, n)
	}
	if len(nums) >= 1 {
		grow = nums[0]
	}
	if len(nums) >= 2 {
		shrink = nums[1]
	}
	_ = gotBasis
	cs.FlexGrow, cs.FlexShrink, cs.FlexBasis = grow, shrink, basis
}

// isBasisToken reports whether a `flex` shorthand token should be read as the basis
// (a length, percentage, or the auto/content keyword) rather than a grow/shrink number.
func isBasisToken(f string) bool {
	switch strings.TrimSpace(f) {
	case "auto", "content":
		return true
	}
	t := newTokenizer(f).next()
	return t.Kind == TokenDimension || t.Kind == TokenPercent
}

// applyGapShorthand expands `gap: <row> [<column>]` (a single value sets both).
func applyGapShorthand(cs *ComputedStyle, val string) {
	fields := strings.Fields(strings.TrimSpace(val))
	if len(fields) == 0 || len(fields) > 2 {
		return
	}
	row, ok := parseGapLength(fields[0])
	if !ok {
		return
	}
	col := row
	if len(fields) == 2 {
		c, ok := parseGapLength(fields[1])
		if !ok {
			return
		}
		col = c
	}
	cs.RowGap, cs.ColumnGap = row, col
}
```

- [ ] **Step 4: Dispatch them** — in `pkg/css/cascade.go`'s `applyDeclaration`, add two cases:

```go
	case "flex":
		applyFlexShorthand(cs, d.Value)
	case "gap":
		applyGapShorthand(cs, d.Value)
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./pkg/css -run 'TestFlexShorthand|TestGapShorthand' -v` (disable sandbox)
Expected: PASS.

- [ ] **Step 6: Full css package test + format/lint**

Run: `go test ./pkg/css && gofmt -l pkg/css && go vet ./pkg/css && golangci-lint run ./pkg/css/...` (disable sandbox)
Expected: all pass; no `gofmt` output.

- [ ] **Step 7: Commit**

```bash
git add pkg/css/shorthand.go pkg/css/cascade.go pkg/css/shorthand_test.go
git commit -m "css: expand flex and gap shorthands"
```

---

## Task 3: The pure `resolveFlexibleLengths` (§9.7) — VERIFY THE SPEC FIRST

> **IMPLEMENTATION GATE — do this before writing any code in this task.** WebFetch <https://www.w3.org/TR/css-flexbox-1/> asking specifically for the verbatim enumerated steps of **§9.7 "Resolving Flexible Lengths"**. The single-page spec is large; if the fetch truncates before §9.7, fetch with a section anchor (`#resolve-flexible-lengths`) or try the dated CRD mirror (`https://www.w3.org/TR/2025/CRD-css-flexbox-1-20251014/`). Confirm the five points below against the spec text and fix the code if the spec differs. **A change that forces you to invert a passing test here is a red flag — re-verify against the spec.** The five load-bearing facts to confirm:
> 1. Used flex factor: grow if `Σ hypothetical < inner main`, else shrink.
> 2. Initial freezing: factor 0; grow & base>hypothetical; shrink & base<hypothetical.
> 3. Remaining-free-space `<1` adjustment: if `Σ unfrozen grow < 1`, use `initial_free × Σgrow` when smaller in magnitude (same for shrink).
> 4. Distribution: grow ∝ `flex-grow`; shrink ∝ `flex-shrink × base` (scaled shrink factor).
> 5. Violation freeze: total>0 → freeze min-violations; total<0 → freeze max-violations; total==0 → freeze all.

**Files:**
- Create: `pkg/layout/css/flex.go` (the `flexLine`/`flexItemSizing` types + `resolveFlexibleLengths` only — the rest of `flex.go` lands in Tasks 7–11)
- Test: `pkg/layout/css/flex_resolve_test.go`

- [ ] **Step 1: Write the failing tests** — create `pkg/layout/css/flex_resolve_test.go`:

```go
package css

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 0.01 }

// sizing builds a flexItemSizing, pre-clamping the hypothetical size with the engine's
// clampF (defined in flex.go, same package). max < 0 means "no maximum".
func sizing(base, grow, shrink, min, max float64) flexItemSizing {
	return flexItemSizing{base: base, hypothetical: clampF(base, min, max), grow: grow, shrink: shrink, minMain: min, maxMain: max}
}

func TestResolveGrowSplitsSurplusByFactor(t *testing.T) {
	// inner=300, three items base 50 each (150 used) => 150 surplus.
	// grow factors 1,2,3 (sum 6) => shares 25,50,75 => 75,100,125.
	items := []flexItemSizing{
		sizing(50, 1, 0, 0, -1),
		sizing(50, 2, 0, 0, -1),
		sizing(50, 3, 0, 0, -1),
	}
	got := resolveFlexibleLengths(items, 300, 0)
	want := []float64{75, 100, 125}
	for i := range want {
		if !approx(got[i], want[i]) {
			t.Errorf("item %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestResolveShrinkSplitsDeficitByScaledFactor(t *testing.T) {
	// inner=100, two items base 80 each (160 used) => deficit 60.
	// shrink 1 each; scaled factor = shrink*base = 80 each (equal) => each shrinks 30 => 50,50.
	items := []flexItemSizing{
		sizing(80, 0, 1, 0, -1),
		sizing(80, 0, 1, 0, -1),
	}
	got := resolveFlexibleLengths(items, 100, 0)
	want := []float64{50, 50}
	for i := range want {
		if !approx(got[i], want[i]) {
			t.Errorf("item %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestResolveShrinkFlooredByMin(t *testing.T) {
	// inner=100, two items base 80; item0 min 70. Deficit 60, equal scaled factors.
	// Naive shrink => 50 each, but item0 floors at 70: it freezes at 70 (violation),
	// remaining deficit (160-100=60; item0 frozen at 70 contributes 70) redistributes
	// onto item1: 100-70=30 => item1=30.
	items := []flexItemSizing{
		sizing(80, 0, 1, 70, -1),
		sizing(80, 0, 1, 0, -1),
	}
	got := resolveFlexibleLengths(items, 100, 0)
	if !approx(got[0], 70) || !approx(got[1], 30) {
		t.Errorf("got %v, want [70 30]", got)
	}
}

func TestResolveGrowClampedByMax(t *testing.T) {
	// inner=300, two items base 50; surplus 200; grow 1 each => +100 each => 150,150.
	// item0 max 120 => freezes at 120 (max violation); its 70 of surplus redistributes
	// to item1 => item1 = 50 + 200 - (120-50) = 50 + 130 = 180.
	items := []flexItemSizing{
		sizing(50, 1, 0, 0, 120),
		sizing(50, 1, 0, 0, -1),
	}
	got := resolveFlexibleLengths(items, 300, 0)
	if !approx(got[0], 120) || !approx(got[1], 180) {
		t.Errorf("got %v, want [120 180]", got)
	}
}

func TestResolveGapConsumesMainSpace(t *testing.T) {
	// inner=300, two items base 50, total gap 100 => surplus = 300-100-100 = 100.
	// grow 1 each => +50 each => 100,100.
	items := []flexItemSizing{
		sizing(50, 1, 0, 0, -1),
		sizing(50, 1, 0, 0, -1),
	}
	got := resolveFlexibleLengths(items, 300, 100)
	if !approx(got[0], 100) || !approx(got[1], 100) {
		t.Errorf("got %v, want [100 100]", got)
	}
}

func TestResolveNoFlexFactorsStayAtBase(t *testing.T) {
	// grow=shrink=0 => inflexible; even with surplus they stay at hypothetical.
	items := []flexItemSizing{
		sizing(50, 0, 0, 0, -1),
		sizing(50, 0, 0, 0, -1),
	}
	got := resolveFlexibleLengths(items, 300, 0)
	if !approx(got[0], 50) || !approx(got[1], 50) {
		t.Errorf("got %v, want [50 50] (inflexible)", got)
	}
}

func TestResolveAllViolateFreezeTogether(t *testing.T) {
	// inner=300, two items base 50, grow 1 each; both max 60 => both want 150 but
	// both clamp to 60 (max violations, total<0 => freeze all max-violations at once).
	items := []flexItemSizing{
		sizing(50, 1, 0, 0, 60),
		sizing(50, 1, 0, 0, 60),
	}
	got := resolveFlexibleLengths(items, 300, 0)
	if !approx(got[0], 60) || !approx(got[1], 60) {
		t.Errorf("got %v, want [60 60]", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css -run TestResolve -v` (disable sandbox)
Expected: FAIL — `flexItemSizing` / `resolveFlexibleLengths` / `clampF` undefined.

- [ ] **Step 3: Create `flex.go` with the types + the pure resolver** — create `pkg/layout/css/flex.go`:

```go
package css

import "math"

// flexItemSizing is the per-item input to the §9.7 flexible-length resolution: the
// purely numeric facts the algorithm needs, with NO layout dependency so the resolver
// is unit-testable in isolation. maxMain < 0 means "no maximum" (CSS `none`).
type flexItemSizing struct {
	base         float64 // flex base size (resolved flex-basis)
	hypothetical float64 // base clamped to [minMain, maxMain] (the hypothetical main size)
	grow         float64 // flex-grow
	shrink       float64 // flex-shrink
	minMain      float64 // used minimum main size (incl. the automatic minimum)
	maxMain      float64 // used maximum main size; <0 = none
}

// clampF clamps v to [lo, hi]; hi < 0 means no upper bound.
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		v = lo
	}
	if hi >= 0 && v > hi {
		v = hi
	}
	return v
}

// resolveFlexibleLengths implements CSS Flexbox 9.7 for a single flex line and returns
// each item's used main size, in item order. innerMain is the flex container's inner
// main size; totalGap is the sum of all main-axis gaps between items. The algorithm is
// a multi-pass freeze loop: pick grow vs shrink, freeze inflexible items, then loop
// {distribute proportional to the used factor, clamp to min/max, freeze items that
// violated} until no flexible items remain.
func resolveFlexibleLengths(items []flexItemSizing, innerMain, totalGap float64) []float64 {
	n := len(items)
	target := make([]float64, n)
	frozen := make([]bool, n)

	// 1. Used flex factor: grow if there is surplus, else shrink.
	sumHypo := totalGap
	for i := range items {
		sumHypo += items[i].hypothetical
	}
	growing := sumHypo < innerMain

	// 2. Size inflexible items (freeze) and seed targets at the hypothetical size.
	for i := range items {
		it := items[i]
		target[i] = it.hypothetical
		factor := it.shrink
		if growing {
			factor = it.grow
		}
		switch {
		case factor == 0:
			frozen[i] = true
		case growing && it.base > it.hypothetical:
			frozen[i] = true
		case !growing && it.base < it.hypothetical:
			frozen[i] = true
		default:
			target[i] = it.base // unfrozen items start the loop at their base size
		}
	}

	// 3. Initial free space (frozen at frozen size, unfrozen at base size).
	initialFree := innerMain - totalGap
	for i := range items {
		if frozen[i] {
			initialFree -= target[i]
		} else {
			initialFree -= items[i].base
		}
	}

	// 4. Loop until no unfrozen items remain.
	for {
		anyUnfrozen := false
		for i := range items {
			if !frozen[i] {
				anyUnfrozen = true
				break
			}
		}
		if !anyUnfrozen {
			break
		}

		// (b) Remaining free space; with the sub-1 flex-factor-sum adjustment.
		remaining := innerMain - totalGap
		sumFactor := 0.0
		for i := range items {
			if frozen[i] {
				remaining -= target[i]
				continue
			}
			remaining -= items[i].base
			if growing {
				sumFactor += items[i].grow
			} else {
				sumFactor += items[i].shrink
			}
		}
		if sumFactor < 1 {
			scaled := initialFree * sumFactor
			if math.Abs(scaled) < math.Abs(remaining) {
				remaining = scaled
			}
		}

		// (c) Distribute proportional to the used flex factor.
		if growing {
			totalGrow := 0.0
			for i := range items {
				if !frozen[i] {
					totalGrow += items[i].grow
				}
			}
			if totalGrow > 0 {
				for i := range items {
					if !frozen[i] {
						target[i] = items[i].base + remaining*(items[i].grow/totalGrow)
					}
				}
			}
		} else {
			totalScaled := 0.0
			for i := range items {
				if !frozen[i] {
					totalScaled += items[i].shrink * items[i].base
				}
			}
			if totalScaled > 0 {
				for i := range items {
					if !frozen[i] {
						ratio := (items[i].shrink * items[i].base) / totalScaled
						target[i] = items[i].base + remaining*ratio // remaining is negative when shrinking
					}
				}
			}
		}

		// (d) Fix min/max violations; record the total violation sign.
		totalViolation := 0.0
		viol := make([]int, n) // +1 = clamped up (min), -1 = clamped down (max), 0 = none
		for i := range items {
			if frozen[i] {
				continue
			}
			clamped := clampF(target[i], items[i].minMain, items[i].maxMain)
			if clamped > target[i] {
				viol[i] = 1
			} else if clamped < target[i] {
				viol[i] = -1
			}
			totalViolation += clamped - target[i]
			target[i] = clamped
		}

		// (e) Freeze by total-violation sign.
		switch {
		case totalViolation == 0:
			for i := range items {
				frozen[i] = true
			}
		case totalViolation > 0:
			for i := range items {
				if viol[i] == 1 {
					frozen[i] = true
				}
			}
		default:
			for i := range items {
				if viol[i] == -1 {
					frozen[i] = true
				}
			}
		}
	}

	return target
}
```

- [ ] **Step 4: (no-op — the code above already imports `math` and the test uses the engine's `clampF`)** Proceed to Step 5.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./pkg/layout/css -run TestResolve -v` (disable sandbox)
Expected: PASS (all seven tests). If any FAIL, re-read §9.7 (the gate) — do NOT "fix" by inverting a test.

- [ ] **Step 6: Format, vet, lint**

Run: `gofmt -l pkg/layout/css && go vet ./pkg/layout/css && golangci-lint run ./pkg/layout/css/...` (disable sandbox)
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add pkg/layout/css/flex.go pkg/layout/css/flex_resolve_test.go
git commit -m "layout/css: pure flexible-length resolver (CSS Flexbox 9.7)"
```

---

## Task 4: `BoxAnonFlexItem` kind + the anonymous-flex-item fixup

**Files:**
- Modify: `pkg/layout/cssbox/box.go` (add `BoxAnonFlexItem`, register in `blockLevel`)
- Create: `pkg/layout/css/flexfix.go`
- Modify: `pkg/layout/css/build.go` (call `fixupFlex(root)` after `fixupTables(root)`)
- Test: `pkg/layout/css/flexfix_test.go`

- [ ] **Step 1: Write the failing test** — create `pkg/layout/css/flexfix_test.go`:

```go
package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func flexContainer(children ...*cssbox.Box) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayFlex,
		Formatting: cssbox.FlexFC, Style: gcss.ComputedStyle{FlexDirection: "row"}, Children: children}
}

func TestFlexFixupWrapsBareText(t *testing.T) {
	txt := &cssbox.Box{Kind: cssbox.BoxText, Text: "hello"}
	fc := flexContainer(txt)
	fixupFlex(fc)
	if len(fc.Children) != 1 {
		t.Fatalf("want 1 child, got %d", len(fc.Children))
	}
	c := fc.Children[0]
	if c.Kind != cssbox.BoxAnonFlexItem {
		t.Errorf("child kind = %v, want BoxAnonFlexItem", c.Kind)
	}
	if len(c.Children) != 1 || c.Children[0] != txt {
		t.Errorf("anon item should wrap the original text box")
	}
}

func TestFlexFixupBlockifiesInlineChild(t *testing.T) {
	span := &cssbox.Box{Kind: cssbox.BoxInline, Display: cssbox.DisplayInline,
		Formatting: cssbox.InlineFC, Children: []*cssbox.Box{{Kind: cssbox.BoxText, Text: "x"}}}
	fc := flexContainer(span)
	fixupFlex(fc)
	if len(fc.Children) != 1 {
		t.Fatalf("want 1 child, got %d", len(fc.Children))
	}
	if !cssbox.BlockLevel(fc.Children[0].Kind) {
		t.Errorf("inline child should be blockified into a block-level flex item")
	}
}

func TestFlexFixupDropsWhitespaceBetweenBlocks(t *testing.T) {
	a := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	ws := &cssbox.Box{Kind: cssbox.BoxText, Text: "   "}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	fc := flexContainer(a, ws, b)
	fixupFlex(fc)
	if len(fc.Children) != 2 {
		t.Fatalf("whitespace between block items should be dropped; want 2 children, got %d", len(fc.Children))
	}
}

func TestFlexFixupLeavesBlockChildren(t *testing.T) {
	a := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	fc := flexContainer(a, b)
	fixupFlex(fc)
	if len(fc.Children) != 2 || fc.Children[0] != a || fc.Children[1] != b {
		t.Errorf("block children should pass through unchanged")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css -run TestFlexFixup -v` (disable sandbox)
Expected: FAIL — `fixupFlex` / `BoxAnonFlexItem` / `cssbox.BlockLevel` undefined.

- [ ] **Step 3: Add the box kind** — in `pkg/layout/cssbox/box.go`, add to the `BoxKind` const block (after `BoxAnonTablePart`):

```go
	// BoxAnonFlexItem is an anonymous block-level flex item wrapping a contiguous run
	// of inline-level content / text inside a flex container (CSS Flexbox 4). Like
	// BoxAnonBlock it carries a zero-value ComputedStyle and establishes a BFC.
	BoxAnonFlexItem
```

And register it in the block-level predicate. The existing predicate is a method/function around line 43; update it to include the new kind. If it is the unexported `blockLevel`, also add an exported wrapper the test uses:

```go
func blockLevel(k BoxKind) bool {
	return k == BoxBlock || k == BoxAnonBlock || k == BoxAnonTablePart || k == BoxAnonFlexItem
}

// BlockLevel reports whether a box kind is block-level. Exported for layout tests.
func BlockLevel(k BoxKind) bool { return blockLevel(k) }
```

(If the existing predicate is already exported or named differently, add `BoxAnonFlexItem` to it and add a `BlockLevel` shim only if absent. Confirm by reading lines 40–50 of `box.go`.)

- [ ] **Step 4: Create the fixup** — create `pkg/layout/css/flexfix.go`:

```go
package css

import "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"

// fixupFlex walks the box tree and repairs every flex container's children into proper
// flex items (CSS Flexbox 4): contiguous runs of inline-level content / text are wrapped
// in an anonymous block-level flex item, inline-level child boxes are blockified, and
// whitespace-only text between block-level items is dropped. Block-level children pass
// through unchanged. Called from Build after fixupTables.
func fixupFlex(b *cssbox.Box) {
	for _, c := range b.Children {
		fixupFlex(c)
	}
	if b.Display == cssbox.DisplayFlex || b.Display == cssbox.DisplayInlineFlex {
		b.Children = flexItems(b.Children)
	}
}

// flexItems converts a flex container's raw children into flex items. A maximal run of
// inline-level boxes becomes one anonymous flex item; a block-level box is kept as its
// own item; whitespace-only text outside an inline run is discarded.
func flexItems(kids []*cssbox.Box) []*cssbox.Box {
	var out []*cssbox.Box
	var run []*cssbox.Box
	flush := func() {
		if len(run) == 0 {
			return
		}
		item := &cssbox.Box{Kind: cssbox.BoxAnonFlexItem, Display: cssbox.DisplayBlock,
			Formatting: cssbox.BlockFC, Children: run}
		out = append(out, item)
		run = nil
	}
	for _, c := range kids {
		switch {
		case isWSText(c) && len(run) == 0:
			// Whitespace between block items collapses away (not part of any run).
			continue
		case cssbox.BlockLevel(c.Kind):
			flush()
			out = append(out, c)
		default:
			// Inline-level box or non-whitespace text: part of the current inline run.
			run = append(run, c)
		}
	}
	flush()
	return out
}
```

(NOTE: `isWSText` already exists in `tablefix.go` — reuse it, do not redefine. Confirm `DisplayInlineFlex` exists in `cssbox`; if box generation does not yet classify `inline-flex`, that wiring is Task 11 — for now `DisplayInlineFlex` only needs to exist as a constant. If it is absent, add it to the `DisplayKind` block in `box.go` in this task, since `fixupFlex` reads it here.)

- [ ] **Step 5: Wire it into `Build`** — in `pkg/layout/css/build.go`, after the `fixupTables(root)` line in both `Build` and `BuildWithFonts` (whichever contains it — confirm; it is in the shared path), add:

```go
	fixupFlex(root) // anonymous FLEX-item fixups (CSS Flexbox 4, flexfix.go)
```

- [ ] **Step 6: Run to verify it passes**

Run: `go test ./pkg/layout/css -run TestFlexFixup -v` (disable sandbox)
Expected: PASS.

- [ ] **Step 7: Confirm no existing test broke + format/lint**

Run: `go test ./pkg/layout/css ./pkg/layout/cssbox && gofmt -l pkg/layout/css pkg/layout/cssbox && golangci-lint run ./pkg/layout/css/... ./pkg/layout/cssbox/...` (disable sandbox)
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add pkg/layout/cssbox/box.go pkg/layout/css/flexfix.go pkg/layout/css/build.go pkg/layout/css/flexfix_test.go
git commit -m "layout/css: anonymous-flex-item fixup + BoxAnonFlexItem kind"
```

---

## Task 5: The axis helper + `layoutFlex` skeleton (row, grow only) wired into the FC switch

This is the first end-to-end slice: a `flex-direction:row` container with growing items lays out through `layoutFlex` and produces positioned fragments. Shrink, basis-resolution detail, justify, align, gap, column, and reverse land in Tasks 6–11.

**Files:**
- Modify: `pkg/layout/css/flex.go` (add `flexAxis`, `layoutFlex`, item collection, base-size + hypothetical sizing, calls `resolveFlexibleLengths`, emits fragments stacked at the main-start with cross = item natural height)
- Modify: `pkg/layout/css/block.go` (FC switch: `case cssbox.FlexFC`)
- Test: `pkg/layout/css/flex_layout_test.go`

- [ ] **Step 1: Write the failing test** — create `pkg/layout/css/flex_layout_test.go`:

```go
package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// flexItemBox builds a block-level flex item with the given fixed cross size (height)
// and flex grow/shrink/basis. width auto so the main size comes from flex.
func flexItemBox(hPx, grow, shrink float64, basis gcss.Length) *cssbox.Box {
	st := gcss.ComputedStyle{
		Width:     gcss.Length{Unit: gcss.UnitAuto},
		Height:    gcss.Length{Value: hPx, Unit: gcss.UnitPx},
		MaxWidth:  gcss.Length{Unit: gcss.UnitAuto},
		MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
		MinWidth:  gcss.Length{0, gcss.UnitPx},
		FlexGrow:  grow, FlexShrink: shrink, FlexBasis: basis,
		AlignSelf: "auto",
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC, Style: st}
}

func flexRow(style gcss.ComputedStyle, items ...*cssbox.Box) *cssbox.Box {
	style.FlexDirection = orDefault(style.FlexDirection, "row")
	style.AlignItems = orDefault(style.AlignItems, "stretch")
	style.JustifyContent = orDefault(style.JustifyContent, "flex-start")
	style.FlexWrap = orDefault(style.FlexWrap, "nowrap")
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayFlex,
		Formatting: cssbox.FlexFC, Style: style, Children: items}
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// flexFrags lays out a flex container inside a body at the given viewport and returns
// the flex item fragments (direct children of the flex container's fragment), in order.
func flexFrags(t *testing.T, container *cssbox.Box, viewport float64) []*Fragment {
	t.Helper()
	e := New(nil, nil, nil)
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC, Children: []*cssbox.Box{container}}
	root := e.layoutTree(context.Background(), body, viewport)
	if root == nil {
		t.Fatal("nil root fragment")
	}
	// The flex container is the body's only child; its fragment children are the items.
	var fc *Fragment
	var find func(f *Fragment)
	find = func(f *Fragment) {
		if f == nil || fc != nil {
			return
		}
		if f.Box != nil && f.Box.Display == cssbox.DisplayFlex {
			fc = f
			return
		}
		for _, c := range f.Children {
			find(c)
		}
	}
	find(root)
	if fc == nil {
		t.Fatal("no flex container fragment found")
	}
	return fc.Children
}

func TestFlexRowGrowDistributesWidth(t *testing.T) {
	// viewport 300, two items, basis 0, grow 1 and 3 => widths 75 and 225, at x 0 and 75.
	a := flexItemBox(40, 1, 1, gcss.Length{0, gcss.UnitPx})
	b := flexItemBox(40, 3, 1, gcss.Length{0, gcss.UnitPx})
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, a, b), 300)
	if len(frags) != 2 {
		t.Fatalf("want 2 item fragments, got %d", len(frags))
	}
	if frags[0].W != 75 || frags[0].X != 0 {
		t.Errorf("item a = x%v w%v, want x0 w75", frags[0].X, frags[0].W)
	}
	if frags[1].W != 225 || frags[1].X != 75 {
		t.Errorf("item b = x%v w%v, want x75 w225", frags[1].X, frags[1].W)
	}
}
```

NOTE: `cssbox.Box` has **no** per-element label field, and `debugTag(b)` returns only a structural kind ("block"/"inline"/…), not a unique name — tests navigate the fragment tree **positionally** (slice index) and assert rects. `flexFrags` returns the flex container's item fragments in visual order, so position in the returned slice IS the item identity. Do not rely on `DebugTag` to tell two block items apart.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css -run TestFlexRowGrow -v` (disable sandbox)
Expected: FAIL — flex still uses the block fallback, so widths won't be 75/225 (likely full-width stacked blocks). `layoutFlex` undefined.

- [ ] **Step 3: Add the axis helper + `layoutFlex`** — first widen `flex.go`'s import block (replace the single `import "math"` from Task 3) to:

```go
import (
	"context"
	"math"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)
```

Then append the following to `pkg/layout/css/flex.go`:

```go
// flexAxis maps abstract main/cross sizes and positions onto x/y/width/height for a
// given flex-direction. row*: main = horizontal. column*: main = vertical. The reverse
// directions flip placement along the main axis (handled by the caller via reverseMain).
type flexAxis struct {
	vertical    bool // true for column / column-reverse (main axis is vertical)
	reverseMain bool // true for row-reverse / column-reverse
}

func axisFor(dir string) flexAxis {
	switch dir {
	case "column":
		return flexAxis{vertical: true}
	case "column-reverse":
		return flexAxis{vertical: true, reverseMain: true}
	case "row-reverse":
		return flexAxis{reverseMain: true}
	default: // row
		return flexAxis{}
	}
}

// rect builds a page-space border-box rect from main/cross position+size. originMain and
// originCross are the container's content-box origin in page space along each axis.
func (a flexAxis) rect(originMain, originCross, mainPos, crossPos, mainSize, crossSize float64) (x, y, w, h float64) {
	if a.vertical {
		return originCross + crossPos, originMain + mainPos, crossSize, mainSize
	}
	return originMain + mainPos, originCross + crossPos, mainSize, crossSize
}

// layoutFlex lays out a single-line flex container (CSS Flexbox 9) and returns its
// interior (positioned item fragments + the content height). Signature matches
// layoutTable. bandOriginY/fc are reserved for future float interactions (a flex
// container establishes a BFC; floats inside items are self-contained).
func (e *Engine) layoutFlex(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext) interior {
	_ = bandOriginY
	_ = fc
	ax := axisFor(b.Style.FlexDirection)
	if b.Style.Direction == "rtl" && !ax.vertical {
		e.logf("css layout: RTL flex rows not supported; laying out LTR")
	}
	if b.Style.FlexWrap == "wrap" || b.Style.FlexWrap == "wrap-reverse" {
		e.logf("css layout: flex-wrap:%s not supported; laying out single-line (nowrap)", b.Style.FlexWrap)
	}

	items := flexItemBoxes(b)
	if len(items) == 0 {
		return interior{contentHeight: 0}
	}

	// Container inner main size. For row it is contentW; for column it is the content
	// height if definite, else indefinite (content-sized => no grow/shrink).
	innerMain, mainDefinite := e.flexMainSize(b, contentW, ax)

	// Per-item flex base size + hypothetical main size + used min/max main.
	sizings := make([]flexItemSizing, len(items))
	for i, it := range items {
		sizings[i] = e.itemSizing(ctx, it, ax, innerMain)
	}

	// Main-axis gap (column-gap for row, row-gap for column) between adjacent items.
	mainGap := e.flexMainGap(b, ax)
	totalGap := mainGap * float64(len(items)-1)

	// If the main size is indefinite (column auto-height), there is no free space:
	// the container is sized to the items, so used main = hypothetical for every item.
	var usedMain []float64
	if !mainDefinite {
		usedMain = make([]float64, len(items))
		sum := totalGap
		for i := range sizings {
			usedMain[i] = sizings[i].hypothetical
			sum += usedMain[i]
		}
		innerMain = sum
	} else {
		usedMain = resolveFlexibleLengths(sizings, innerMain, totalGap)
	}

	// Lay out each item's contents at its used main size; capture its cross size.
	frags := make([]*Fragment, len(items))
	crossSizes := make([]float64, len(items))
	for i, it := range items {
		frags[i], crossSizes[i] = e.layoutFlexItem(ctx, it, ax, usedMain[i])
	}

	// Line cross size = max item outer cross size (clamped to a definite container cross
	// size if set — deferred refinement; for now the max).
	lineCross := 0.0
	for _, cs := range crossSizes {
		if cs > lineCross {
			lineCross = cs
		}
	}

	// Position items along the main axis, packed at main-start (justify-content:flex-start
	// for now; full justify in Task 8, cross alignment in Task 9, reverse in Task 7). The
	// origin is main=contentX (the container's content-left in page space) and cross=0 in
	// the local content-top frame (block layout shifts Y into place). placeFlexFragment
	// maps (main,cross)->(x,y) via the axis and sizes the fragment.
	mainPos := 0.0
	for i := range items {
		placeFlexFragment(frags[i], ax, contentX, 0, mainPos, 0, usedMain[i], crossSizes[i])
		mainPos += usedMain[i] + mainGap
	}

	contentHeight := lineCross
	if ax.vertical {
		contentHeight = mainPos - mainGap // total main extent (sum of items + gaps)
	}
	// NB: do NOT set interior.intrinsicWidth — that field shrink-to-fits a TABLE box;
	// a flex container fills its containing-block width like a normal block.
	return interior{children: frags, contentHeight: contentHeight}
}
```

This skeleton has placeholder cross/justify behavior (flex-start packing, cross=0) that Tasks 6–11 replace. To make it compile and pass the grow test now, also add these helpers (final versions; some bodies are filled minimally and extended later — each extension has its own task + test):

```go
// flexItemBoxes returns the in-flow flex item child boxes (the fixup already wrapped
// inline runs + blockified inline-level boxes), sorted by `order` (stable for ties).
func flexItemBoxes(b *cssbox.Box) []*cssbox.Box {
	var items []*cssbox.Box
	for _, c := range b.Children {
		items = append(items, c)
	}
	// order: stable sort by Style.Order (Task 10 adds the test; the sort is harmless now).
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j-1].Style.Order > items[j].Style.Order; j-- {
			items[j-1], items[j] = items[j], items[j-1]
		}
	}
	return items
}

// flexMainSize returns the container inner main size and whether it is definite.
func (e *Engine) flexMainSize(b *cssbox.Box, contentW float64, ax flexAxis) (float64, bool) {
	if !ax.vertical {
		return contentW, true // row: the content width is always definite here
	}
	// column: main = height; definite only if an explicit non-auto, non-% height is set.
	if b.Style.Height.Unit != gcss.UnitAuto && b.Style.Height.Unit != gcss.UnitPercent {
		h, _ := resolveLen(b.Style.Height, b.Style.FontSizePt, 0)
		return h, true
	}
	return 0, false
}

// flexMainGap returns the main-axis gap: column-gap for a row, row-gap for a column.
func (e *Engine) flexMainGap(b *cssbox.Box, ax flexAxis) float64 {
	g := b.Style.ColumnGap
	if ax.vertical {
		g = b.Style.RowGap
	}
	v, _ := resolveLen(g, b.Style.FontSizePt, 0)
	if v < 0 {
		v = 0
	}
	return v
}

// itemSizing computes a flex item's base size, hypothetical main size, and used min/max
// main size (Task 6 fills basis=auto/content + the automatic minimum; for now: basis
// length/percentage, or 0 when auto/content, with explicit min/max only).
func (e *Engine) itemSizing(ctx context.Context, it *cssbox.Box, ax flexAxis, innerMain float64) flexItemSizing {
	base := e.flexBaseSize(ctx, it, ax, innerMain)
	minMain, maxMain := e.usedMinMaxMain(ctx, it, ax)
	return flexItemSizing{
		base:         base,
		hypothetical: clampF(base, minMain, maxMain),
		grow:         it.Style.FlexGrow,
		shrink:       it.Style.FlexShrink,
		minMain:      minMain,
		maxMain:      maxMain,
	}
}

// flexBaseSize resolves flex-basis to the item's flex base size (Task 6 adds
// auto=>main-size-property and content=>max-content; here: length/% else 0).
func (e *Engine) flexBaseSize(ctx context.Context, it *cssbox.Box, ax flexAxis, innerMain float64) float64 {
	fb := it.Style.FlexBasis
	switch fb.Unit {
	case gcss.UnitAuto, gcss.UnitContent:
		return 0 // refined in Task 6
	case gcss.UnitPercent:
		return innerMain * fb.Value / 100
	default:
		v, _ := resolveLen(fb, it.Style.FontSizePt, 0)
		return v
	}
}

// usedMinMaxMain returns the item's used min/max main size from explicit min-/max-*
// (Task 6 adds the automatic minimum). maxMain < 0 = none.
func (e *Engine) usedMinMaxMain(ctx context.Context, it *cssbox.Box, ax flexAxis) (minMain, maxMain float64) {
	minL, maxL := it.Style.MinWidth, it.Style.MaxWidth
	if ax.vertical {
		minL, maxL = it.Style.MinHeight, it.Style.MaxHeight
	}
	minMain, _ = resolveLen(minL, it.Style.FontSizePt, 0)
	if maxL.Unit == gcss.UnitAuto {
		maxMain = -1
	} else {
		maxMain, _ = resolveLen(maxL, it.Style.FontSizePt, 0)
	}
	return minMain, maxMain
}

// layoutFlexItem lays out one flex item's contents at its used main size and returns its
// fragment and its outer cross size. For row, used main = content width and the fragment
// height is the cross size. (Column axis handling: Task 7.)
func (e *Engine) layoutFlexItem(ctx context.Context, it *cssbox.Box, ax flexAxis, usedMain float64) (*Fragment, float64) {
	if ax.vertical {
		return e.layoutFlexItemColumn(ctx, it, usedMain) // Task 7
	}
	pos := &positionedContext{}
	res := e.layoutBlock(ctx, it, usedMain, 0, 0, 0,
		&floatContext{cbLeft: 0, cbRight: usedMain}, pos, posCBOwner{isPage: true})
	frag := res.frag
	cross := 0.0
	if frag != nil {
		cross = frag.H
		e.resolveAbsolute(ctx, pos, frag, usedMain, frag.H)
	}
	return frag, cross
}

// layoutFlexItemColumn is the column-axis item layout (Task 7 implements it; stubbed to
// the row path for now so the package compiles).
func (e *Engine) layoutFlexItemColumn(ctx context.Context, it *cssbox.Box, usedMain float64) (*Fragment, float64) {
	pos := &positionedContext{}
	res := e.layoutBlock(ctx, it, usedMain, 0, 0, 0,
		&floatContext{cbLeft: 0, cbRight: usedMain}, pos, posCBOwner{isPage: true})
	frag := res.frag
	cross := 0.0
	if frag != nil {
		cross = frag.W
	}
	return frag, cross
}

// placeFlexFragment positions a laid-out item fragment at the given main/cross offsets,
// resizing it to (usedMain × crossSize) along the axis and translating its descendants.
func placeFlexFragment(frag *Fragment, ax flexAxis, originMain, originCross, mainPos, crossPos, mainSize, crossSize float64) {
	if frag == nil {
		return
	}
	x, y, w, h := ax.rect(originMain, originCross, mainPos, crossPos, mainSize, crossSize)
	stretchCellFragment(frag, x, y, w, h) // reuse the table helper: sets X/Y/W/H + shifts children
}
```

Confirm `stretchCellFragment(frag, x, y, w, h)` exists in `table.go:719` (it sets the fragment's rect and translates children to the new origin); it does. The `gcss` alias and `resolveLen(l, fontSizePt, pctBasis)` are already used throughout `pkg/layout/css`.

- [ ] **Step 4: Wire the FC switch** — in `pkg/layout/css/block.go`'s `layoutInterior`, add a case before `default`:

```go
	case cssbox.FlexFC:
		in = e.layoutFlex(ctx, b, contentW, contentX, childBand, childFC)
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./pkg/layout/css -run TestFlexRowGrow -v` (disable sandbox)
Expected: PASS (item a x0 w75, item b x75 w225).

- [ ] **Step 6: Confirm nothing else broke + format/lint**

Run: `go test ./pkg/layout/css && gofmt -l pkg/layout/css && go vet ./pkg/layout/css && golangci-lint run ./pkg/layout/css/...` (disable sandbox)
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add pkg/layout/css/flex.go pkg/layout/css/block.go pkg/layout/css/flex_layout_test.go
git commit -m "layout/css: layoutFlex skeleton (row, grow) wired into the FC switch"
```

---

## Task 6: `flex-basis: auto`/`content` resolution + the automatic minimum size

**Files:**
- Modify: `pkg/layout/css/flex.go` (`flexBaseSize`, `usedMinMaxMain`)
- Test: `pkg/layout/css/flex_layout_test.go`

- [ ] **Step 1: Write the failing tests** — append to `pkg/layout/css/flex_layout_test.go`:

```go
func TestFlexBasisAutoUsesWidth(t *testing.T) {
	// basis auto, width 120 => base 120; no grow/shrink => stays 120 at x0.
	st := gcss.ComputedStyle{
		Width: gcss.Length{120, gcss.UnitPx}, Height: gcss.Length{40, gcss.UnitPx},
		MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
		FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
	}
	item := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, item), 300)
	if frags[0].W != 120 {
		t.Errorf("basis:auto width:120 => w%v, want 120", frags[0].W)
	}
}

func TestFlexAutoMinimumFloorsShrink(t *testing.T) {
	// Two text items, basis auto (=> content size), flex-shrink 1, no explicit min.
	// The container is narrow enough that naive shrink would crush them below their
	// min-content; the automatic minimum must floor each at its min-content width.
	mk := func(text string) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Unit: gcss.UnitAuto}, FontSizePt: 16,
			MinWidth: gcss.Length{Unit: gcss.UnitAuto}, // auto => automatic minimum applies
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			FlexGrow: 0, FlexShrink: 1, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.InlineFC,
			Style: st, Children: []*cssbox.Box{{Kind: cssbox.BoxText, Text: text, Style: st}}}
	}
	a := mk("Wonderful")
	b := mk("Magnificent")
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, a, b), 80) // intentionally too narrow
	// Each item must be at least its min-content width (the longest word), so the two
	// together overflow 80 rather than shrinking to fit. Assert neither is crushed to ~0.
	if frags[0].W < 10 || frags[1].W < 10 {
		t.Errorf("auto-minimum should floor items at min-content; got w %v and %v", frags[0].W, frags[1].W)
	}
}

func TestFlexExplicitMinZeroAllowsFullShrink(t *testing.T) {
	// Same as above but min-width:0 explicitly => items MAY shrink below content.
	mk := func(text string) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Unit: gcss.UnitAuto}, FontSizePt: 16,
			MinWidth: gcss.Length{0, gcss.UnitPx}, // explicit 0 => no automatic minimum
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			FlexGrow: 0, FlexShrink: 1, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.InlineFC,
			Style: st, Children: []*cssbox.Box{{Kind: cssbox.BoxText, Text: text, Style: st}}}
	}
	a := mk("Wonderful")
	b := mk("Magnificent")
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, a, b), 80)
	total := frags[0].W + frags[1].W
	if total > 81 {
		t.Errorf("with min-width:0 items should shrink to fit ~80; total w = %v", total)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css -run 'TestFlexBasisAuto|TestFlexAutoMinimum|TestFlexExplicitMinZero' -v` (disable sandbox)
Expected: `TestFlexBasisAutoUsesWidth` FAILs (base 0 today, not 120); the auto-minimum tests FAIL (no auto-min today).

- [ ] **Step 3: Implement `flex-basis: auto`/`content`** — replace `flexBaseSize` in `flex.go`:

```go
func (e *Engine) flexBaseSize(ctx context.Context, it *cssbox.Box, ax flexAxis, innerMain float64) float64 {
	fb := it.Style.FlexBasis
	switch fb.Unit {
	case gcss.UnitAuto:
		// auto: use the main-size property (width for row, height for column) if it is a
		// definite length; otherwise fall through to content (max-content).
		mainLen := it.Style.Width
		if ax.vertical {
			mainLen = it.Style.Height
		}
		if mainLen.Unit != gcss.UnitAuto && mainLen.Unit != gcss.UnitPercent {
			v, _ := resolveLen(mainLen, it.Style.FontSizePt, 0)
			return v
		}
		return e.measureMaxContent(ctx, it)
	case gcss.UnitContent:
		return e.measureMaxContent(ctx, it)
	case gcss.UnitPercent:
		return innerMain * fb.Value / 100
	default:
		v, _ := resolveLen(fb, it.Style.FontSizePt, 0)
		return v
	}
}
```

(For a column container `measureMaxContent` returns a width, not a height — that is a known approximation for `flex-basis:auto/content` on a column item; the content height is hard to know without laying out. Note it in a code comment: the column main-axis content base falls back to the item's max-content width as a proxy. This matches the spec's "content" being intrinsic size and is acceptable for slice 1; refine in 9b.)

- [ ] **Step 4: Implement the automatic minimum** — replace `usedMinMaxMain` in `flex.go`:

```go
func (e *Engine) usedMinMaxMain(ctx context.Context, it *cssbox.Box, ax flexAxis) (minMain, maxMain float64) {
	minL, maxL := it.Style.MinWidth, it.Style.MaxWidth
	if ax.vertical {
		minL, maxL = it.Style.MinHeight, it.Style.MaxHeight
	}
	// Maximum.
	if maxL.Unit == gcss.UnitAuto {
		maxMain = -1
	} else {
		maxMain, _ = resolveLen(maxL, it.Style.FontSizePt, 0)
	}
	// Minimum: an explicit min resolves directly. min:auto triggers the automatic
	// minimum size (CSS Flexbox 4.5): the min-content size, capped by an explicit main
	// size or max (the spec's min()). For row, the content min size is measureMinContent.
	if minL.Unit == gcss.UnitAuto {
		autoMin := e.measureMinContent(ctx, it)
		// Cap by a definite main size if smaller (a fixed-size item's auto-min is its size).
		mainLen := it.Style.Width
		if ax.vertical {
			mainLen = it.Style.Height
		}
		if mainLen.Unit != gcss.UnitAuto && mainLen.Unit != gcss.UnitPercent {
			if v, _ := resolveLen(mainLen, it.Style.FontSizePt, 0); v < autoMin {
				autoMin = v
			}
		}
		if maxMain >= 0 && maxMain < autoMin {
			autoMin = maxMain
		}
		minMain = autoMin
	} else {
		minMain, _ = resolveLen(minL, it.Style.FontSizePt, 0)
	}
	return minMain, maxMain
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./pkg/layout/css -run 'TestFlexBasisAuto|TestFlexAutoMinimum|TestFlexExplicitMinZero' -v` (disable sandbox)
Expected: PASS. Also re-run `TestFlexRowGrow` to confirm no regression: `go test ./pkg/layout/css -run TestFlexRow -v`.

- [ ] **Step 6: Format/lint**

Run: `gofmt -l pkg/layout/css && go vet ./pkg/layout/css && golangci-lint run ./pkg/layout/css/...` (disable sandbox)
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add pkg/layout/css/flex.go pkg/layout/css/flex_layout_test.go
git commit -m "layout/css: flex-basis auto/content + automatic minimum size"
```

---

## Task 7: `flex-direction: column` + `*-reverse`

**Files:**
- Modify: `pkg/layout/css/flex.go` (`layoutFlexItemColumn`, the reverse placement in `layoutFlex`)
- Test: `pkg/layout/css/flex_layout_test.go`

- [ ] **Step 1: Write the failing tests** — append:

```go
func TestFlexColumnStacksVertically(t *testing.T) {
	// column, two items width 100 height 40 and 60, basis auto, no grow/shrink.
	// They stack vertically: item0 at y0 h40, item1 at y40 h60. Both x0.
	mk := func(w, h float64) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{w, gcss.UnitPx}, Height: gcss.Length{h, gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinHeight: gcss.Length{0, gcss.UnitPx}, MinWidth: gcss.Length{0, gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{FlexDirection: "column"}, mk(100, 40), mk(100, 60)), 300)
	if len(frags) != 2 {
		t.Fatalf("want 2 frags, got %d", len(frags))
	}
	if frags[0].Y != 0 || frags[0].H != 40 {
		t.Errorf("col item0 = y%v h%v, want y0 h40", frags[0].Y, frags[0].H)
	}
	if frags[1].Y != 40 || frags[1].H != 60 {
		t.Errorf("col item1 = y%v h%v, want y40 h60", frags[1].Y, frags[1].H)
	}
}

func TestFlexRowReversePlacesFromEnd(t *testing.T) {
	// row-reverse, viewport 300, two fixed-width items 100 and 50, no grow/shrink.
	// Reverse packs from the main-end: first item's main-start edge is at the right.
	// item0 (100) occupies x[200..300]; item1 (50) occupies x[150..200].
	mk := func(w float64) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{w, gcss.UnitPx}, Height: gcss.Length{40, gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{0, gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{FlexDirection: "row-reverse"}, mk(100), mk(50)), 300)
	if frags[0].X != 200 || frags[0].W != 100 {
		t.Errorf("row-reverse item0 = x%v w%v, want x200 w100", frags[0].X, frags[0].W)
	}
	if frags[1].X != 150 || frags[1].W != 50 {
		t.Errorf("row-reverse item1 = x%v w%v, want x150 w50", frags[1].X, frags[1].W)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css -run 'TestFlexColumn|TestFlexRowReverse' -v` (disable sandbox)
Expected: column FAILs (items not stacked correctly — the stub lays out at width=usedMain which for column is the height) and reverse FAILs (packed from start).

- [ ] **Step 3: Implement the column item layout** — replace `layoutFlexItemColumn` in `flex.go`:

```go
// layoutFlexItemColumn lays out a column-axis flex item: the used main size is the
// item's HEIGHT, and its width comes from the cross axis (its definite width, else the
// container will stretch/shrink it — for now lay out at the item's own width if definite,
// otherwise at its max-content width as the natural cross size). Returns the fragment and
// its outer cross size (the width). The fragment's height is pinned to usedMain.
func (e *Engine) layoutFlexItemColumn(ctx context.Context, it *cssbox.Box, usedMain float64) (*Fragment, float64) {
	// Cross (width) basis: a definite width, else max-content.
	crossW := e.measureMaxContent(ctx, it)
	if it.Style.Width.Unit != gcss.UnitAuto && it.Style.Width.Unit != gcss.UnitPercent {
		if v, _ := resolveLen(it.Style.Width, it.Style.FontSizePt, 0); v > 0 {
			crossW = v
		}
	}
	pos := &positionedContext{}
	res := e.layoutBlock(ctx, it, crossW, 0, 0, 0,
		&floatContext{cbLeft: 0, cbRight: crossW}, pos, posCBOwner{isPage: true})
	frag := res.frag
	if frag != nil {
		// Pin the fragment height to the used main size (the flexed height).
		frag.H = usedMain
		e.resolveAbsolute(ctx, pos, frag, crossW, usedMain)
	}
	return frag, crossW
}
```

- [ ] **Step 4: Implement reverse placement** — in `layoutFlex`, replace the main-axis positioning loop with one that flips for `reverseMain`:

```go
	// Total main extent consumed by items + gaps (used for reverse placement and the
	// column content height).
	consumed := totalGap
	for i := range items {
		consumed += usedMain[i]
	}

	mainPos := 0.0
	for i := range items {
		pos := mainPos
		if ax.reverseMain {
			// Pack from the main-end: the item's main-start edge sits `consumed - mainPos
			// - usedMain[i]` from the container's main-start... but reverse should fill the
			// END of the container (innerMain), so offset by (innerMain - consumed) when the
			// container is larger than the content.
			pos = innerMain - mainPos - usedMain[i]
		}
		placeFlexFragment(frags[i], ax, contentX, 0, pos, 0, usedMain[i], crossSizes[i])
		mainPos += usedMain[i] + mainGap
	}

	contentHeight := lineCross
	if ax.vertical {
		contentHeight = consumed
	}
	return interior{children: frags, contentHeight: contentHeight}
}
```

NOTE the reverse formula uses `innerMain` (the full container main size) so a row-reverse with a 300px container packs the first item flush to x=200..300. Verify against the test's expected x200/x150. For column-reverse the same formula flips top/bottom within `innerMain` (definite) or `consumed` (indefinite — set `innerMain = consumed` earlier in the indefinite branch, which the Task-5 code already does).

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./pkg/layout/css -run 'TestFlexColumn|TestFlexRowReverse' -v` (disable sandbox)
Expected: PASS. Re-run prior flex tests: `go test ./pkg/layout/css -run TestFlex -v`.

- [ ] **Step 6: Format/lint**

Run: `gofmt -l pkg/layout/css && go vet ./pkg/layout/css && golangci-lint run ./pkg/layout/css/...` (disable sandbox)
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add pkg/layout/css/flex.go pkg/layout/css/flex_layout_test.go
git commit -m "layout/css: flex-direction column + row/column-reverse placement"
```

---

## Task 8: `justify-content` (main-axis alignment)

**Files:**
- Modify: `pkg/layout/css/flex.go` (compute leading offset + inter-item spacing from `justify-content`, fold into the placement loop)
- Test: `pkg/layout/css/flex_layout_test.go`

- [ ] **Step 1: Write the failing tests** — append:

```go
// justifyFrags lays out three fixed 50px-wide items in a 300px row with the given
// justify-content and returns their X positions.
func justifyFrags(t *testing.T, jc string) []float64 {
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{50, gcss.UnitPx}, Height: gcss.Length{40, gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{0, gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{JustifyContent: jc}, mk(), mk(), mk()), 300)
	xs := make([]float64, len(frags))
	for i, f := range frags {
		xs[i] = f.X
	}
	return xs
}

func TestJustifyContent(t *testing.T) {
	// 3 items × 50 = 150 used, 150 free in a 300 container.
	cases := []struct {
		jc   string
		want []float64
	}{
		{"flex-start", []float64{0, 50, 100}},
		{"flex-end", []float64{150, 200, 250}},
		{"center", []float64{75, 125, 175}},
		{"space-between", []float64{0, 125, 250}},  // gaps of 75 between
		{"space-around", []float64{25, 125, 225}},  // half-gap 25 at ends, 50 between
		{"space-evenly", []float64{37.5, 125, 212.5}}, // equal 37.5 everywhere
	}
	for _, c := range cases {
		got := justifyFrags(t, c.jc)
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("justify-content:%s item %d X = %v, want %v (all: %v)", c.jc, i, got[i], c.want[i], got)
			}
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css -run TestJustifyContent -v` (disable sandbox)
Expected: FAIL for every value except `flex-start` (only flex-start works today).

- [ ] **Step 3: Implement justify-content** — in `flex.go`, add a helper and use it. Add:

```go
// justifyOffsets returns the leading main offset (before the first item) and the extra
// spacing inserted between adjacent items, for a given justify-content value. freeSpace
// is the leftover main space after used sizes + gaps; n is the item count. Negative
// freeSpace (overflow) is treated as 0 leading / 0 extra for the distributed modes, and
// flex-end/center still shift by the (negative) free space (overflowing the start).
func justifyOffsets(jc string, freeSpace float64, n int) (leading, between float64) {
	if n == 0 {
		return 0, 0
	}
	switch jc {
	case "flex-end":
		return freeSpace, 0
	case "center":
		return freeSpace / 2, 0
	case "space-between":
		if n == 1 || freeSpace < 0 {
			return 0, 0
		}
		return 0, freeSpace / float64(n-1)
	case "space-around":
		if freeSpace < 0 {
			return 0, 0
		}
		unit := freeSpace / float64(n)
		return unit / 2, unit
	case "space-evenly":
		if freeSpace < 0 {
			return 0, 0
		}
		unit := freeSpace / float64(n+1)
		return unit, unit
	default: // flex-start
		return 0, 0
	}
}
```

Then in `layoutFlex`, after computing `usedMain`, `mainGap`, and `consumed`, compute free space and fold the offsets into placement (replace the placement loop from Task 7):

```go
	freeMain := innerMain - consumed
	leading, between := justifyOffsets(b.Style.JustifyContent, freeMain, len(items))

	mainPos := leading
	for i := range items {
		pos := mainPos
		if ax.reverseMain {
			pos = innerMain - mainPos - usedMain[i]
		}
		placeFlexFragment(frags[i], ax, contentX, 0, pos, 0, usedMain[i], crossSizes[i])
		mainPos += usedMain[i] + mainGap + between
	}
```

NOTE: `between` is added on top of `mainGap`, so `gap` and `space-between` compose (the spec stacks them). For `reverseMain`, the `pos = innerMain - mainPos - usedMain[i]` formula already mirrors the leading/between because `mainPos` accumulates them.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/layout/css -run TestJustifyContent -v` (disable sandbox)
Expected: PASS (all six values).

- [ ] **Step 5: Format/lint + full flex re-run**

Run: `go test ./pkg/layout/css -run TestFlex && go test ./pkg/layout/css -run TestJustify && gofmt -l pkg/layout/css && golangci-lint run ./pkg/layout/css/...` (disable sandbox)
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/flex.go pkg/layout/css/flex_layout_test.go
git commit -m "layout/css: justify-content main-axis alignment"
```

---

## Task 9: `align-items` / `align-self` (cross-axis alignment + stretch)

**Files:**
- Modify: `pkg/layout/css/flex.go` (resolve per-item alignment, compute cross position, relayout on stretch)
- Test: `pkg/layout/css/flex_layout_test.go`

- [ ] **Step 1: Write the failing tests** — append:

```go
// alignFrags lays out two items of heights 40 and 80 in a row with the given align-items
// and returns their Y positions and heights. The line cross size is 80 (the taller item).
func alignFrags(t *testing.T, alignItems, alignSelf0 string) []*Fragment {
	mk := func(h float64, self string) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{50, gcss.UnitPx}, Height: gcss.Length{h, gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{0, gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: self,
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	return flexFrags(t, flexRow(gcss.ComputedStyle{AlignItems: alignItems}, mk(40, alignSelf0), mk(80, "auto")), 300)
}

func TestAlignItemsFlexStart(t *testing.T) {
	f := alignFrags(t, "flex-start", "auto")
	if f[0].Y != 0 || f[1].Y != 0 {
		t.Errorf("flex-start: both items at cross-start y0; got y%v, y%v", f[0].Y, f[1].Y)
	}
	if f[0].H != 40 {
		t.Errorf("flex-start short item keeps its height 40; got %v", f[0].H)
	}
}

func TestAlignItemsFlexEnd(t *testing.T) {
	f := alignFrags(t, "flex-end", "auto")
	// line cross 80; short item (40) sits at y = 80-40 = 40.
	if f[0].Y != 40 || f[0].H != 40 {
		t.Errorf("flex-end short item = y%v h%v, want y40 h40", f[0].Y, f[0].H)
	}
	if f[1].Y != 0 {
		t.Errorf("flex-end tall item at y0; got %v", f[1].Y)
	}
}

func TestAlignItemsCenter(t *testing.T) {
	f := alignFrags(t, "center", "auto")
	// short item centered in 80: y = (80-40)/2 = 20.
	if f[0].Y != 20 || f[0].H != 40 {
		t.Errorf("center short item = y%v h%v, want y20 h40", f[0].Y, f[0].H)
	}
}

func TestAlignItemsStretch(t *testing.T) {
	f := alignFrags(t, "stretch", "auto")
	// short item (no definite... it HAS height 40, so stretch does NOT override a definite
	// cross size). Per spec, stretch only applies when cross size is auto. Item0 has a
	// definite height 40 => stays 40 at y0.
	if f[0].H != 40 {
		t.Errorf("stretch with definite height keeps 40; got %v", f[0].H)
	}
}

func TestAlignStretchGrowsAutoHeight(t *testing.T) {
	// An item with auto height stretches to the line cross size.
	short := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{50, gcss.UnitPx}, Height: gcss.Length{Unit: gcss.UnitAuto},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{0, gcss.UnitPx}, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto"}}
	tall := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{50, gcss.UnitPx}, Height: gcss.Length{80, gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{0, gcss.UnitPx}, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto"}}
	f := flexFrags(t, flexRow(gcss.ComputedStyle{AlignItems: "stretch"}, short, tall), 300)
	if f[0].H != 80 {
		t.Errorf("stretch auto-height item should grow to line cross 80; got %v", f[0].H)
	}
}

func TestAlignSelfOverridesAlignItems(t *testing.T) {
	f := alignFrags(t, "flex-start", "center")
	// align-items flex-start but item0 align-self center => y = (80-40)/2 = 20.
	if f[0].Y != 20 {
		t.Errorf("align-self:center overrides align-items:flex-start; y = %v, want 20", f[0].Y)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css -run 'TestAlign' -v` (disable sandbox)
Expected: most FAIL — cross position is always 0 today and stretch is not implemented.

- [ ] **Step 3: Implement cross-axis alignment** — in `flex.go`, add:

```go
// resolvedAlign returns the effective cross-axis alignment for an item: align-self if it
// is not auto, else the container's align-items. baseline is approximated to flex-start.
func resolvedAlign(container, item *cssbox.Box) string {
	a := item.Style.AlignSelf
	if a == "" || a == "auto" {
		a = container.Style.AlignItems
	}
	if a == "" {
		a = "stretch"
	}
	return a
}

// crossOffset returns the item's cross-axis position within a line of size lineCross for
// an item of outer cross size itemCross under alignment a (stretch is handled separately
// before this is called, by which point itemCross == lineCross).
func crossOffset(a string, lineCross, itemCross float64) float64 {
	switch a {
	case "flex-end":
		return lineCross - itemCross
	case "center":
		return (lineCross - itemCross) / 2
	default: // flex-start, stretch, baseline(approx)
		return 0
	}
}

// itemHasDefiniteCross reports whether the item has a definite cross size (so stretch
// does not apply). For a row the cross axis is height; for a column it is width.
func itemHasDefiniteCross(it *cssbox.Box, ax flexAxis) bool {
	l := it.Style.Height
	if ax.vertical {
		l = it.Style.Width
	}
	return l.Unit != gcss.UnitAuto && l.Unit != gcss.UnitPercent
}
```

Then, in `layoutFlex`, after computing `lineCross` and before/within the placement loop, apply stretch (relayout) and cross offsets. Replace the placement section:

```go
	freeMain := innerMain - consumed
	leading, between := justifyOffsets(b.Style.JustifyContent, freeMain, len(items))

	mainPos := leading
	for i := range items {
		align := resolvedAlign(b, items[i])
		itemCross := crossSizes[i]

		// stretch: grow an auto-cross item to the line cross size and relayout its
		// contents at that cross measure (a row item's width is its main size, which is
		// fixed; stretch grows its HEIGHT — pin the fragment height to lineCross).
		if align == "stretch" && !itemHasDefiniteCross(items[i], ax) {
			frags[i], itemCross = e.stretchFlexItem(ctx, items[i], ax, usedMain[i], lineCross)
		}
		if align == "baseline" {
			e.logf("css layout: align-items/align-self baseline not supported; using flex-start")
		}

		crossPos := crossOffset(align, lineCross, itemCross)
		pos := mainPos
		if ax.reverseMain {
			pos = innerMain - mainPos - usedMain[i]
		}
		placeFlexFragment(frags[i], ax, contentX, 0, pos, crossPos, usedMain[i], itemCross)
		mainPos += usedMain[i] + mainGap + between
	}
```

Add the stretch relayout helper:

```go
// stretchFlexItem re-lays an auto-cross item out to the line cross size and returns its
// new fragment + outer cross size (== lineCross). For a row the main size (width) is
// fixed at usedMain and the height is pinned to lineCross. For a column the main size
// (height) is usedMain and the width (cross) becomes lineCross.
func (e *Engine) stretchFlexItem(ctx context.Context, it *cssbox.Box, ax flexAxis, usedMain, lineCross float64) (*Fragment, float64) {
	if ax.vertical {
		// column: relayout at width = lineCross, height pinned to usedMain.
		pos := &positionedContext{}
		res := e.layoutBlock(ctx, it, lineCross, 0, 0, 0,
			&floatContext{cbLeft: 0, cbRight: lineCross}, pos, posCBOwner{isPage: true})
		frag := res.frag
		if frag != nil {
			frag.H = usedMain
			e.resolveAbsolute(ctx, pos, frag, lineCross, usedMain)
		}
		return frag, lineCross
	}
	// row: width = usedMain (the main size); pin height to lineCross.
	pos := &positionedContext{}
	res := e.layoutBlock(ctx, it, usedMain, 0, 0, 0,
		&floatContext{cbLeft: 0, cbRight: usedMain}, pos, posCBOwner{isPage: true})
	frag := res.frag
	if frag != nil {
		frag.H = lineCross
		e.resolveAbsolute(ctx, pos, frag, usedMain, lineCross)
	}
	return frag, lineCross
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/layout/css -run 'TestAlign' -v` (disable sandbox)
Expected: PASS (all align tests).

- [ ] **Step 5: Format/lint + full flex re-run**

Run: `go test ./pkg/layout/css -run 'TestFlex|TestJustify|TestAlign' && gofmt -l pkg/layout/css && golangci-lint run ./pkg/layout/css/...` (disable sandbox)
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/flex.go pkg/layout/css/flex_layout_test.go
git commit -m "layout/css: align-items/align-self cross-axis alignment + stretch"
```

---

## Task 10: `order` + `gap` (main-axis) end-to-end + the byte-identical guard so far

**Files:**
- Modify: `pkg/layout/css/flex.go` (verify `order` sort already wired in `flexItemBoxes`; nothing new if Task 5 included it — add the test)
- Test: `pkg/layout/css/flex_layout_test.go`

- [ ] **Step 1: Write the failing tests** — append:

```go
func TestFlexOrderReorders(t *testing.T) {
	// Three items given DISTINCT widths so position is identifiable by width: in document
	// order width 30 (order 2), 50 (order 0), 70 (order 1). After ordering, visual order
	// is the order-0 item (w50), order-1 (w70), order-2 (w30). With no grow, packed at
	// start: x 0, 50, 120. The returned frags are in visual order, so their widths must be
	// 50, 70, 30 — proving the reorder.
	mk := func(w float64, order int) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{w, gcss.UnitPx}, Height: gcss.Length{40, gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{0, gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto", Order: order,
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, mk(30, 2), mk(50, 0), mk(70, 1)), 300)
	if len(frags) != 3 {
		t.Fatalf("want 3 frags, got %d", len(frags))
	}
	wantW := []float64{50, 70, 30} // visual order after sorting by `order`
	for i, w := range wantW {
		if frags[i].W != w {
			t.Errorf("order position %d width = %v, want %v (widths: %v %v %v)", i, frags[i].W, w, frags[0].W, frags[1].W, frags[2].W)
		}
	}
	if frags[0].X != 0 || frags[1].X != 50 || frags[2].X != 120 {
		t.Errorf("ordered items packed at 0/50/120; got %v/%v/%v", frags[0].X, frags[1].X, frags[2].X)
	}
}

func TestFlexMainGapSpacesItems(t *testing.T) {
	// Two fixed 50px items, column-gap 20 => x0 and x70 (50 + 20 gap).
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{50, gcss.UnitPx}, Height: gcss.Length{40, gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{0, gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{ColumnGap: gcss.Length{20, gcss.UnitPx}}, mk(), mk()), 300)
	if frags[0].X != 0 || frags[1].X != 70 {
		t.Errorf("column-gap:20 => x0,x70; got x%v,x%v", frags[0].X, frags[1].X)
	}
}
```

These tests identify items **positionally by width** (no tag lookup): `flexFrags` returns the flex container's item fragments in visual order, and each item is given a distinct width, so `frags[i].W` proves the post-`order` sequence. The `order` sort itself was already wired into `flexItemBoxes` in Task 5 (the insertion sort by `Style.Order`); this task adds the test that locks it down (and confirms `gap` composes).

- [ ] **Step 2: Run to verify it passes**

Run: `go test ./pkg/layout/css -run 'TestFlexOrder|TestFlexMainGap' -v` (disable sandbox)
Expected: PASS for both (order sort wired in Task 5, gap wired in Task 5). If `TestFlexOrderReorders` FAILS, the Task-5 sort is wrong (e.g. not stable, or comparing the wrong field) — fix `flexItemBoxes`'s sort, do NOT weaken the test.

- [ ] **Step 3: (only if Step 2 failed) fix the `order` sort** — in `flex.go`'s `flexItemBoxes`, confirm the insertion sort is stable and compares `items[j-1].Style.Order > items[j].Style.Order` (strictly greater, so equal orders keep document order). Re-run Step 2.

- [ ] **Step 4: (reserved — Steps 2/3 cover order+gap)**

- [ ] **Step 5: Byte-identical guard — run the full suite + goldens WITHOUT -update**

Run: `go test ./... ` (disable sandbox)
Expected: ALL existing tests pass, including `pkg/doctaculous` goldens/reftests. Then:

Run: `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` (disable sandbox)
Expected: **empty output** (no existing golden/reftest changed — flex has not leaked into block/table). If anything changed, STOP and investigate before proceeding.

- [ ] **Step 6: Format/lint**

Run: `gofmt -l pkg/layout/css && golangci-lint run ./pkg/layout/css/...` (disable sandbox)
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add pkg/layout/css/flex.go pkg/layout/css/flex_layout_test.go
git commit -m "layout/css: flex order + main-axis gap (with byte-identical guard)"
```

---

## Task 11: `inline-flex` (inline-level flex container)

**Files:**
- Modify: `pkg/layout/cssbox/box.go` (confirm/add `DisplayInlineFlex`)
- Modify: `pkg/layout/css/build.go` (classify `display:inline-flex` → `BoxInline`/`DisplayInlineFlex`/`FlexFC`, inline-level outer)
- Modify: `pkg/layout/css/flex.go` or the inline path — ensure an inline-flex box flows as an inline atom (like inline-block) and lays its items out via `layoutFlex`
- Test: `pkg/layout/css/flex_layout_test.go`

- [ ] **Step 1: Read how inline-block is classified + flowed** — Run: `grep -n "inline-block\|DisplayInlineBlock\|InlineFC\|atom" pkg/layout/css/build.go pkg/layout/css/inline.go | head -40` (disable sandbox). Note how `inline-block` becomes an inline-level box that still establishes a BFC and is laid out as an atom in the IFC. `inline-flex` mirrors it but its interior uses `layoutFlex` (it already will, because its `Formatting` is `FlexFC`).

- [ ] **Step 2: Write the failing test** — append to `pkg/layout/css/flex_layout_test.go`:

```go
func TestInlineFlexFlowsInline(t *testing.T) {
	// An inline-flex container with two 30px items sits inline after some text. Assert the
	// flex items lay out (widths 30) — i.e. inline-flex reaches layoutFlex, not a fallback.
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{30, gcss.UnitPx}, Height: gcss.Length{20, gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{0, gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	ifx := &cssbox.Box{Kind: cssbox.BoxInline, Display: cssbox.DisplayInlineFlex, Formatting: cssbox.FlexFC,
		Style: gcss.ComputedStyle{FlexDirection: "row", AlignItems: "stretch", JustifyContent: "flex-start", FlexWrap: "nowrap"},
		Children: []*cssbox.Box{mk(), mk()}}
	e := New(nil, nil, nil)
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{ifx}}
	root := e.layoutTree(context.Background(), body, 300)
	// Find the two 30×20 item fragments anywhere in the tree.
	var items []*Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 30 && f.H == 20 {
			items = append(items, f)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if len(items) != 2 {
		t.Fatalf("want 2 inline-flex item fragments (30x20), got %d", len(items))
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./pkg/layout/css -run TestInlineFlex -v` (disable sandbox)
Expected: FAIL if `DisplayInlineFlex` isn't classified/flowed (items not found or wrong size).

- [ ] **Step 4: Confirm/extend the classification** — In `pkg/layout/css/build.go`, find the `display` classification switch (around line 282 where `case "flex":` lives) and add:

```go
	case "inline-flex":
		b.Kind, b.Display, b.Formatting = cssbox.BoxInline, cssbox.DisplayInlineFlex, cssbox.FlexFC
```

Ensure `cssbox.DisplayInlineFlex` exists (Task 4 may have added it; if not, add it to the `DisplayKind` block in `box.go`). Then confirm the inline formatting context treats an inline-level box that establishes a BFC (`establishesNewBFC`) as an atom — `inline-flex`, like `inline-block`, must return `true` from `establishesNewBFC` and be laid out via `layoutInterior` (which now routes `FlexFC` to `layoutFlex`). Read `establishesNewBFC` and the inline-atom path; if `inline-block` is special-cased by `Display == DisplayInlineBlock`, add `|| b.Display == cssbox.DisplayInlineFlex` there.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./pkg/layout/css -run TestInlineFlex -v` (disable sandbox)
Expected: PASS.

- [ ] **Step 6: Full suite + byte-identical guard + lint**

Run: `go test ./... && git status --short pkg/doctaculous/testdata pkg/render/raster/testdata && gofmt -l pkg/layout/css pkg/layout/cssbox && golangci-lint run ./pkg/layout/css/... ./pkg/layout/cssbox/...` (disable sandbox)
Expected: all pass; testdata status empty.

- [ ] **Step 7: Commit**

```bash
git add pkg/layout/cssbox/box.go pkg/layout/css/build.go pkg/layout/css/flex.go pkg/layout/css/flex_layout_test.go
git commit -m "layout/css: inline-flex (inline-level flex container)"
```

---

## Task 12: Golden images (controller eyeballs)

**Files:**
- Modify: `pkg/doctaculous/html_golden_test.go` (register four flex goldens)
- Create (generated): `pkg/doctaculous/testdata/golden-html/html-flex-*.png` (paths per the harness)

> The implementer has NO image vision. After `-update`, STOP and hand the PNG paths back to the controller, who eyeballs each via the Read tool BEFORE the task is considered done.

- [ ] **Step 1: Register the goldens** — in `pkg/doctaculous/html_golden_test.go`, add four entries to the `htmlGoldens` slice (match the existing entry shape: `name`, `viewportPx`, `html`, optional `loader`):

```go
	{
		name:       "flex-justify-between",
		viewportPx: 320,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .row { display: flex; justify-content: space-between; }
  .box { width: 60px; height: 40px; background: #4477aa; }
</style></head><body>
  <div class="row"><div class="box"></div><div class="box"></div><div class="box"></div></div>
</body></html>`,
	},
	{
		name:       "flex-grow",
		viewportPx: 320,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .row { display: flex; }
  .a { flex: 1 1 0; height: 40px; background: #aa4444; }
  .b { flex: 2 1 0; height: 40px; background: #44aa44; }
</style></head><body>
  <div class="row"><div class="a"></div><div class="b"></div></div>
</body></html>`,
	},
	{
		name:       "flex-align-center",
		viewportPx: 320,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .row { display: flex; align-items: center; height: 100px; background: #eeeeee; }
  .s { width: 50px; height: 30px; background: #4477aa; }
  .t { width: 50px; height: 70px; background: #aa7744; }
</style></head><body>
  <div class="row"><div class="s"></div><div class="t"></div></div>
</body></html>`,
	},
	{
		name:       "flex-column",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .col { display: flex; flex-direction: column; }
  .box { width: 120px; height: 40px; background: #6644aa; }
  .box2 { width: 80px; height: 60px; background: #44aa88; }
</style></head><body>
  <div class="col"><div class="box"></div><div class="box2"></div></div>
</body></html>`,
	},
```

- [ ] **Step 2: Generate the goldens**

Run: `go test ./pkg/doctaculous -run TestHTMLGolden -update` (disable sandbox)
Expected: PASS; four new PNGs created. Capture their exact paths (the harness prints/derives `…/golden-html/flex-*.png` or similar — confirm the directory from the failing-path message in `html_golden_test.go`).

- [ ] **Step 3: STOP — hand the PNG paths to the controller for eyeball review.**

List the four created PNG paths. The controller reads each with the Read tool and confirms:
- `flex-justify-between`: three blue boxes, first flush-left, last flush-right, equal gaps.
- `flex-grow`: two bars filling the width, green twice as wide as red.
- `flex-align-center`: short + tall box vertically centered in the grey strip.
- `flex-column`: two boxes stacked vertically, second narrower.

Do not proceed until the controller approves (or requests fixes). If a render is wrong, fix the layout (a real bug) — do NOT re-`-update` to bless a wrong image.

- [ ] **Step 4: Verify goldens pass without -update**

Run: `go test ./pkg/doctaculous -run TestHTMLGolden` (disable sandbox)
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/doctaculous/html_golden_test.go pkg/doctaculous/testdata
git commit -m "doctaculous: flex golden images (justify/grow/align/column)"
```

---

## Task 13: WPT-style reftests

**Files:**
- Modify: `pkg/doctaculous/wpt_reftest_test.go` (register pairs)
- Create: `pkg/doctaculous/testdata/wpt/flex-justify.html` + `flex-justify-ref.html`, `flex-grow.html` + `flex-grow-ref.html`, `flex-align-center.html` + `flex-align-center-ref.html`, `flex-column.html` + `flex-column-ref.html` (confirm the actual `testdata/wpt` directory from the harness)

> Confirm the reftest fixture directory and naming by reading `wpt_reftest_test.go` (it loads `NAME.html`/`NAME-ref.html` from a fixed dir). Use that exact path.

- [ ] **Step 1: Create the `flex-justify` pair.** `flex-justify.html`:

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .row { display: flex; justify-content: space-between; }
  .box { width: 50px; height: 40px; background: #336699; }
</style></head><body>
  <div class="row"><div class="box"></div><div class="box"></div><div class="box"></div></div>
</body></html>
```

`flex-justify-ref.html` (three abs-positioned boxes at the hand-computed x: viewport 200, 3×50=150 used, 50 free, space-between => x 0, 75, 150):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; position: relative; height: 40px; }
  .box { position: absolute; top: 0; width: 50px; height: 40px; background: #336699; }
</style></head><body>
  <div class="box" style="left: 0;"></div>
  <div class="box" style="left: 75px;"></div>
  <div class="box" style="left: 150px;"></div>
</body></html>
```

- [ ] **Step 2: Create the `flex-grow` pair.** `flex-grow.html`:

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .row { display: flex; }
  .a { flex: 1 1 0; height: 40px; background: #993333; }
  .b { flex: 3 1 0; height: 40px; background: #339933; }
</style></head><body>
  <div class="row"><div class="a"></div><div class="b"></div></div>
</body></html>
```

`flex-grow-ref.html` (viewport 200, grow 1:3 => widths 50 and 150 at x0 and x50):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; position: relative; height: 40px; }
  .box { position: absolute; top: 0; height: 40px; }
</style></head><body>
  <div class="box" style="left: 0; width: 50px; background: #993333;"></div>
  <div class="box" style="left: 50px; width: 150px; background: #339933;"></div>
</body></html>
```

- [ ] **Step 3: Create the `flex-align-center` pair.** `flex-align-center.html`:

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .row { display: flex; align-items: center; height: 100px; }
  .s { width: 50px; height: 30px; background: #224488; }
  .t { width: 50px; height: 70px; background: #884422; }
</style></head><body>
  <div class="row"><div class="s"></div><div class="t"></div></div>
</body></html>
```

`flex-align-center-ref.html` (line cross 100; short box y=(100-30)/2=35; tall y=(100-70)/2=15; x0 and x50):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; position: relative; height: 100px; }
  .box { position: absolute; }
</style></head><body>
  <div class="box" style="left: 0; top: 35px; width: 50px; height: 30px; background: #224488;"></div>
  <div class="box" style="left: 50px; top: 15px; width: 50px; height: 70px; background: #884422;"></div>
</body></html>
```

NOTE: this pair assumes `align-items: center` centers within the line cross size = the container height (100, because the row has an explicit height). Confirm the engine sizes the line cross to the container height when definite; if it sizes to the max item (70) instead, change the ref to center within 70 (short y=(70-30)/2=20, tall y0) and document which behavior the engine implements. (Either is internally consistent; the ref must match the engine.)

- [ ] **Step 4: Create the `flex-column` pair.** `flex-column.html`:

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .col { display: flex; flex-direction: column; }
  .a { width: 120px; height: 40px; background: #553399; }
  .b { width: 80px; height: 60px; background: #339977; }
</style></head><body>
  <div class="col"><div class="a"></div><div class="b"></div></div>
</body></html>
```

`flex-column-ref.html` (stacked: a at y0 h40, b at y40 h60):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .a { width: 120px; height: 40px; background: #553399; }
  .b { width: 80px; height: 60px; background: #339977; }
</style></head><body>
  <div class="a"></div><div class="b"></div>
</body></html>
```

- [ ] **Step 5: Register the reftests** — in `pkg/doctaculous/wpt_reftest_test.go`, add to the `wptReftests` slice:

```go
	{"flex-justify", 200, "justify-content:space-between of three 50px items == abs boxes at 0/75/150", nil},
	{"flex-grow", 200, "a flex-grow 1:3 row == abs boxes of the grown widths (50/150)", nil},
	{"flex-align-center", 200, "align-items:center == abs boxes at the centered cross offsets", nil},
	{"flex-column", 200, "a flex-direction:column == stacked blocks", nil},
```

- [ ] **Step 6: Run the reftests**

Run: `go test ./pkg/doctaculous -run TestWPTReftests -v` (disable sandbox)
Expected: PASS for all four flex pairs. If a pair fails, the diff means the flex layout disagrees with the hand-placed reference — debug the layout (or, for `flex-align-center`, reconcile the ref with the engine's line-cross behavior per Step 3's note). Do NOT loosen the comparison tolerance.

- [ ] **Step 7: Commit**

```bash
git add pkg/doctaculous/wpt_reftest_test.go pkg/doctaculous/testdata
git commit -m "doctaculous: flex WPT reftests (justify/grow/align/column)"
```

---

## Task 14: Degradation tests

**Files:**
- Test: `pkg/layout/css/flex_layout_test.go`

- [ ] **Step 1: Write the degradation tests** — append:

```go
func TestFlexWrapDegradesToNowrap(t *testing.T) {
	// flex-wrap:wrap with three wide items that don't fit: they must stay on one line and
	// overflow (no second row). Assert all three share the same Y and the last overflows.
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{100, gcss.UnitPx}, Height: gcss.Length{40, gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{0, gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{FlexWrap: "wrap"}, mk(), mk(), mk()), 150)
	if len(frags) != 3 {
		t.Fatalf("want 3 frags on one line, got %d", len(frags))
	}
	if frags[0].Y != frags[1].Y || frags[1].Y != frags[2].Y {
		t.Errorf("wrap should degrade to nowrap (one line, same Y); got %v %v %v", frags[0].Y, frags[1].Y, frags[2].Y)
	}
	if frags[2].X != 200 { // 0,100,200 — overflows the 150 viewport (nowrap)
		t.Errorf("third item should overflow at x200; got %v", frags[2].X)
	}
}

func TestFlexEmptyContainerNoPanic(t *testing.T) {
	fc := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayFlex, Formatting: cssbox.FlexFC,
		Style: gcss.ComputedStyle{FlexDirection: "row"}}
	e := New(nil, nil, nil)
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{fc}}
	// Must not panic; produce a (zero-ish height) fragment.
	root := e.layoutTree(context.Background(), body, 300)
	if root == nil {
		t.Fatal("nil root")
	}
}

func TestFlexShrinkZeroOverflows(t *testing.T) {
	// Two 100px items, flex-shrink 0, in a 150 container: they cannot shrink, so they
	// overflow (x0,x100). No panic, no clamp below their size.
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{100, gcss.UnitPx}, Height: gcss.Length{40, gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{0, gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, mk(), mk()), 150)
	if frags[0].W != 100 || frags[1].W != 100 || frags[1].X != 100 {
		t.Errorf("shrink:0 items keep 100 and overflow; got w%v/w%v x%v", frags[0].W, frags[1].W, frags[1].X)
	}
}

func TestFlexBaselineApproximatesFlexStart(t *testing.T) {
	// align-items:baseline (deferred) must not panic; approximate to flex-start (y0).
	f := alignFrags(t, "baseline", "auto")
	if f[0].Y != 0 {
		t.Errorf("baseline approximates flex-start (y0); got %v", f[0].Y)
	}
}
```

- [ ] **Step 2: Run to verify it passes**

Run: `go test ./pkg/layout/css -run 'TestFlexWrap|TestFlexEmpty|TestFlexShrinkZero|TestFlexBaseline' -v` (disable sandbox)
Expected: PASS (these exercise behavior implemented in Tasks 5–9; if `TestFlexWrapDegradesToNowrap`'s overflow x differs, reconcile with the engine's nowrap placement — it should be 0/100/200).

- [ ] **Step 3: Format/lint**

Run: `gofmt -l pkg/layout/css && golangci-lint run ./pkg/layout/css/...` (disable sandbox)
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add pkg/layout/css/flex_layout_test.go
git commit -m "layout/css: flex degradation tests (wrap->nowrap, empty, shrink:0, baseline)"
```

---

## Task 15: Full verification, race, and CLAUDE.md update

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Full test suite + race detector**

Run: `go test ./...` then `go test -race ./pkg/layout/... ./pkg/css/... ./pkg/doctaculous/...` (disable sandbox)
Expected: all pass (race included — flex is read-only after layout, like the rest).

- [ ] **Step 2: Repo-wide format + vet + lint of touched packages**

Run: `gofmt -l pkg/css pkg/layout/css pkg/layout/cssbox pkg/doctaculous && go vet ./... && golangci-lint run ./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...` (disable sandbox)
Expected: no `gofmt` output; vet/lint clean.

- [ ] **Step 3: Final byte-identical guard**

Run: `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` (disable sandbox)
Expected: only the NEW flex goldens/reftests appear (added in Tasks 12–13), no MODIFIED existing baseline.

- [ ] **Step 4: Confirm no scratch files**

Run: `find . -name 'zz_*' -o -name '*probe*' | grep -v testdata` (disable sandbox) and `git status` (disable sandbox)
Expected: empty `find`; clean `git status` (everything committed).

- [ ] **Step 5: Update CLAUDE.md** — Move flexbox from the §6 remaining-slices list into a new "Done" bullet. In the **Done** section (after the web-fonts bullet), add a bullet describing: the flex formatting context; the properties + `flex`/`gap` shorthands; the single-line algorithm scope (direction incl. reverses, basis/grow/shrink via the §9.7 resolver, justify-content, align-items/self incl. stretch, min/max + the automatic minimum, gap, order, inline-flex); that item contents lay out through the existing block/inline path (a flex item is a BFC); what the goldens/reftests cover; and the deferrals (wrap+align-content→nowrap, RTL→LTR, full baseline→flex-start, cross-axis-gap effect). In the **§6 TODO** remaining-slices parenthetical, move flexbox from "today flex/grid fall back to block normal flow" to the done list (leaving grid as the next block-fallback slice), and add the web-font-style "fidelity follow-ups within the existing engine" note listing the 9b deferrals. Keep the Done/TODO the honest source of truth.

Draft Done bullet (adjust wording to match the file's voice):

```markdown
- **HTML rendering — CSS flexbox (single-line, `display: flex`/`inline-flex`)** (`pkg/layout/css/flex.go` +
  `flexfix.go` (new), `pkg/layout/css/build.go`/`block.go` wiring, `pkg/layout/cssbox` `BoxAnonFlexItem` +
  `DisplayInlineFlex`, `pkg/css` flex properties + `flex`/`gap` shorthands + a `UnitContent` length unit;
  covered by flex-property parse/shorthand tests, a pure §9.7 resolver unit suite, fragment-geometry tests,
  anonymous-flex-item fixup tests, the `flex-*` goldens, and the `flex-*` WPT reftests): a `display:flex` box
  now lays out as a **real single-line flex container**, replacing the prior block fallback. Pieces: the flex
  properties on the cascade (`flex-direction`, `flex-wrap`, `justify-content`, `align-items`/`align-self`,
  `flex-grow`/`shrink`/`basis`, `order`, `row-gap`/`column-gap`, the `flex` and `gap` shorthands); the
  **anonymous-flex-item fixup** (`flexfix.go`, CSS Flexbox §4 — wrap inline runs, blockify inline-level items,
  drop inter-item whitespace); an **axis-abstracted `layoutFlex`** (one algorithm for `row`/`column` and the
  reverses via a `flexAxis` mapping); the **flex base size + hypothetical main size** (`flex-basis` length/%/
  `auto`→main-size-property→`content`/`content`→max-content, via the table-slice `measure.go`) with the
  **automatic minimum size** (§4.5, `min-*:auto`→min-content floor on shrink); the **§9.7 flexible-length
  resolution** carved into a pure `resolveFlexibleLengths` (the multi-pass freeze loop — grow ∝ flex-grow,
  shrink ∝ flex-shrink×base, min/max clamping by total-violation sign); **`justify-content`** (all six values,
  composing with `gap`); **`align-items`/`align-self`** cross-axis placement incl. `stretch` (relayout an
  auto-cross item to the line cross size); and **`inline-flex`** (an inline-level flex container flowing as an
  inline atom, like inline-block). Each flex item establishes a **BFC** and lays its contents out through the
  existing block/inline path (`e.layoutBlock`) — the `render.Device` seam, the PDF/DOCX pipelines, and the
  shared inline core (`pkg/layout/inline`) are **untouched**. **Byte-identical:** every page with no flex
  container is unchanged (the existing corpus uses block/inline/table). Degrades gracefully (no panic, logged):
  **`flex-wrap: wrap`/`wrap-reverse`** → single-line (`nowrap`, items overflow), **RTL/`direction`** on a row →
  LTR, **`align-items: baseline`** → `flex-start`, and the **cross-axis gap** (`row-gap` for `row*`) is a no-op
  on a single line. An empty/degenerate flex container is a zero-size fragment. See
  `docs/superpowers/specs/2026-06-26-html-flexbox-design.md`.
```

- [ ] **Step 6: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: record CSS flexbox (single-line) in CLAUDE.md"
```

- [ ] **Step 7: Hand back to the controller** for the holistic final review (a fresh read of the whole diff against the spec) before opening the stacked PR. Flag for the reviewer the highest-risk areas: the §9.7 resolver (verify the freeze/violation math adversarially), the column-axis approximations (`flex-basis:auto/content` proxying to max-content width), and the `flex-align-center` reftest's line-cross assumption.

---

## Self-review notes (for the executor)

- **Spec coverage:** every spec section maps to a task — properties (T1), shorthands (T2), §9.7 resolver (T3), fixup + box kind (T4), the FC switch + skeleton (T5), basis/auto-min (T6), column/reverse (T7), justify (T8), align/stretch (T9), order/gap + byte-identical guard (T10), inline-flex (T11), goldens (T12), reftests (T13), degradation (T14), verification + docs (T15).
- **Known approximations carried (state in PR):** column-axis `flex-basis:auto/content` uses the item's max-content *width* as the main-size proxy; `flex-align-center` line cross size when the container has a definite height vs. max-item — reconcile the ref with the engine in T13.
- **The §9.7 gate (T3) is non-negotiable** — verify against the W3C spec before encoding; do not invert a passing resolver test to make new code compile.
