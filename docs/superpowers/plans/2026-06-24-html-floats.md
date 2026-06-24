# HTML Floats + Clear Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement CSS `float`/`clear` in the HTML layout engine — a float is taken out of normal flow to a containing-block edge, in-flow line boxes and block content narrow around it, `clear` drops below floats, multiple floats stack and wrap, and floats paint in their own CSS layer.

**Architecture:** All float logic lives in `pkg/layout/css`; the shared inline core (`pkg/layout/inline`, also used by DOCX) gains only one additive primitive (`BreakNext`). A per-BFC `floatContext` records placed float margin-box rects and answers pure geometry queries (`leftEdge`/`rightEdge`/`place`/`clearY`) that both the block stacker and the inline formatting context consult per vertical band. Paint order follows CSS 2.1 Appendix E (block decorations → floats → inline content) via a phase-split `AppendItems`.

**Tech Stack:** Go (stdlib only — no new dependency); `pkg/css` (cascade), `pkg/layout/cssbox` (box tree), `pkg/layout/css` (layout engine), `pkg/layout/inline` (shared shaper/breaker), `pkg/layout/paint`, `pkg/doctaculous` (golden + WPT reftest harness).

**Spec:** `docs/superpowers/specs/2026-06-24-html-floats-design.md`.

**Process reminders (from the handover, hold throughout):**
- Run `go`/`golangci-lint`/`gofmt` with the **sandbox disabled** (sandbox blocks the Go build cache + TLS). Lint **specific packages**, not the repo root; run `gofmt -l` on changed packages separately (golangci-lint here does not gofmt). Decline modernize hints (`max()`/`min()`/`slices.*`/range-over-int) — the codebase uses explicit clamps intentionally.
- Editor diagnostics lag; trust `go build`/`go test`. After any review subagent, `find . -name 'zz_*' -delete` and confirm `git status` is clean.
- Eyeball every new/changed golden PNG in the PR.

---

## File Structure

**New files:**
- `pkg/layout/css/floats.go` — the `floatContext` type and its geometry (`leftEdge`, `rightEdge`, `place`, `clearY`). One clear responsibility: float-rect bookkeeping + avoidance geometry.
- `pkg/layout/css/floats_test.go` — unit tests for the geometry in isolation.
- `pkg/doctaculous/testdata/wpt/css21-normal-flow/float-left.html` + `float-left-ref.html` — a WPT-style reftest pair.
- `pkg/doctaculous/testdata/wpt/css21-normal-flow/float-row.html` + `float-row-ref.html` — a multi-float reftest pair.

**Modified files:**
- `pkg/css/cascade.go` — add `Float`/`Clear` to `ComputedStyle`, `initialStyle`, and `applyDeclaration`.
- `pkg/css/cascade_test.go` — `float`/`clear` parse/cascade tests.
- `pkg/layout/inline/break.go` — add `BreakNext` (one greedy line + remainder), shared logic with `Break`.
- `pkg/layout/inline/break_test.go` (may be new) — `BreakNext` tests incl. `Break` equivalence.
- `pkg/layout/css/build.go` — wire `floatOf`; blockify a floated inline-level element.
- `pkg/layout/css/build_test.go` — float wiring / blockify tests (file may already exist; otherwise add).
- `pkg/layout/css/fragment.go` — `Fragment.IsFloat`, `Fragment.Floats`; phase-split `AppendItems`.
- `pkg/layout/css/block.go` — thread `*floatContext` through `layoutTree`/`layoutBlock`/`layoutInterior`/`layoutBlockChildren`; place floated children; `clear`; narrow in-flow content.
- `pkg/layout/css/inline.go` — thread `*floatContext` into `layoutInline`; per-line `BreakNext` driving at the float-narrowed band.
- `pkg/layout/css/*_test.go` — fragment-geometry + paint-order assertions (new test file `floats_layout_test.go`).
- `pkg/doctaculous/html_golden_test.go` — two new `htmlGoldens` entries (figure + multi-float row).
- `pkg/doctaculous/wpt_reftest_test.go` — two new `wptReftests` entries.
- `CLAUDE.md` — move floats out of the §6 TODO into Done (final task, when the PR lands).

---

## Task 1: Add `float` and `clear` to the CSS cascade

**Files:**
- Modify: `pkg/css/cascade.go` (struct ~line 67, `initialStyle` ~line 252, `applyDeclaration` ~line 342)
- Test: `pkg/css/cascade_test.go`

`float`/`clear` are **not** CSS-inherited, so they are added to `ComputedStyle`, `initialStyle`, and `applyDeclaration` — but NOT to `inheritFrom`.

- [ ] **Step 1: Write the failing test**

Add to `pkg/css/cascade_test.go`:

```go
// TestInitialFloatClear: the cascade defaults float and clear to "none".
func TestInitialFloatClear(t *testing.T) {
	cs := initialStyle()
	if cs.Float != "none" {
		t.Errorf("initial float = %q, want none", cs.Float)
	}
	if cs.Clear != "none" {
		t.Errorf("initial clear = %q, want none", cs.Clear)
	}
}

// TestApplyFloatClear: each valid float/clear keyword is accepted; an invalid one
// is dropped (leaving the prior value).
func TestApplyFloatClear(t *testing.T) {
	for _, kw := range []string{"left", "right", "none"} {
		cs := initialStyle()
		applyDeclaration(&cs, Declaration{Property: "float", Value: kw})
		if cs.Float != kw {
			t.Errorf("float %q not applied, got %q", kw, cs.Float)
		}
	}
	for _, kw := range []string{"left", "right", "both", "none"} {
		cs := initialStyle()
		applyDeclaration(&cs, Declaration{Property: "clear", Value: kw})
		if cs.Clear != kw {
			t.Errorf("clear %q not applied, got %q", kw, cs.Clear)
		}
	}
	// Invalid values are dropped, default preserved.
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "float", Value: "center"})
	if cs.Float != "none" {
		t.Errorf("float after invalid keyword = %q, want none preserved", cs.Float)
	}
	applyDeclaration(&cs, Declaration{Property: "clear", Value: "all"})
	if cs.Clear != "none" {
		t.Errorf("clear after invalid keyword = %q, want none preserved", cs.Clear)
	}
}

// TestFloatNotInherited: float/clear are not inherited (a child without its own
// float defaults to none even if the parent floats).
func TestFloatNotInherited(t *testing.T) {
	parent := initialStyle()
	parent.Float = "left"
	parent.Clear = "both"
	child := inheritFrom(parent)
	if child.Float != "none" || child.Clear != "none" {
		t.Errorf("float/clear inherited: got float=%q clear=%q, want none/none", child.Float, child.Clear)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/css -run 'TestInitialFloatClear|TestApplyFloatClear|TestFloatNotInherited' -v`
Expected: FAIL — `cs.Float`/`cs.Clear` undefined (compile error).

- [ ] **Step 3: Add the struct fields**

In `pkg/css/cascade.go`, in `ComputedStyle` after the `ObjectFit` field (~line 71):

```go
	// Float is the CSS float value: "none" (default) | "left" | "right". Not
	// inherited. The box generator maps it to cssbox.FloatKind.
	Float string
	// Clear is the CSS clear value: "none" (default) | "left" | "right" | "both".
	// Not inherited. The layout engine lowers a cleared box below matching floats.
	Clear string
```

- [ ] **Step 4: Set the initial values**

In `initialStyle()` after `ObjectFit: "fill",` (~line 252):

```go
		Float:       "none", // CSS initial float
		Clear:       "none", // CSS initial clear
```

- [ ] **Step 5: Handle the declarations**

In `applyDeclaration`, after the `object-fit` case (~line 342):

```go
	case "float":
		switch d.Value {
		case "left", "right", "none":
			cs.Float = d.Value
		}
	case "clear":
		switch d.Value {
		case "left", "right", "both", "none":
			cs.Clear = d.Value
		}
```

- [ ] **Step 6: Update the ComputedStyle doc comment**

The `ComputedStyle` doc comment lists which properties are inherited. `Float`/`Clear` are NOT inherited; no change to the inherited list is needed, but confirm the comment still reads correctly (it enumerates inherited properties — float/clear are absent, which is correct). No edit required unless the comment claims completeness of the property list.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./pkg/css -run 'TestInitialFloatClear|TestApplyFloatClear|TestFloatNotInherited' -v`
Expected: PASS.

- [ ] **Step 8: Full package test + gofmt**

Run: `go test ./pkg/css && gofmt -l pkg/css`
Expected: tests PASS; `gofmt -l` prints nothing.

- [ ] **Step 9: Commit**

```bash
git add pkg/css/cascade.go pkg/css/cascade_test.go
git commit -m "css: parse float and clear properties"
```

---

## Task 2: Wire `floatOf` and blockify floated inlines in box generation

**Files:**
- Modify: `pkg/layout/css/build.go` (`floatOf` ~line 142; `generate` ~line 76; `classifyDisplay` ~line 152)
- Test: `pkg/layout/css/build_test.go` (create if absent)

CSS computes `display` to a block-level value on a floated element (CSS 2.1 §9.7). So box generation: (1) maps `cs.Float` to `FloatKind`; (2) when a float is present and the element's display is inline-level, classifies it block-level.

- [ ] **Step 1: Write the failing test**

Create or append `pkg/layout/css/build_test.go`:

```go
package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// TestFloatOf maps the computed float keyword to a FloatKind.
func TestFloatOf(t *testing.T) {
	cases := map[string]cssbox.FloatKind{
		"none":  cssbox.FloatNone,
		"left":  cssbox.FloatLeft,
		"right": cssbox.FloatRight,
		"":      cssbox.FloatNone, // unset/zero string
	}
	for in, want := range cases {
		if got := floatOf(gcss.ComputedStyle{Float: in}); got != want {
			t.Errorf("floatOf(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestBlockifyFloatedInline: a floated inline-level element classifies as a
// block-level box (CSS 9.7), so the layout engine lays it out as a float.
func TestBlockifyFloatedInline(t *testing.T) {
	cs := gcss.ComputedStyle{Display: "inline", Float: "left"}
	b := &cssbox.Box{Style: cs}
	classifyDisplay(b, cs.Display)
	// Pre-blockify it is inline; applyFloatBlockify promotes it.
	applyFloatBlockify(b, cs)
	if !b.Kind.IsBlockLevel() {
		t.Errorf("floated inline: Kind=%v not block-level", b.Kind)
	}
	if b.Display != cssbox.DisplayBlock {
		t.Errorf("floated inline: Display=%v, want DisplayBlock", b.Display)
	}
}

// TestNoBlockifyWithoutFloat: an inline element with no float stays inline.
func TestNoBlockifyWithoutFloat(t *testing.T) {
	cs := gcss.ComputedStyle{Display: "inline", Float: "none"}
	b := &cssbox.Box{Style: cs}
	classifyDisplay(b, cs.Display)
	applyFloatBlockify(b, cs)
	if b.Kind != cssbox.BoxInline {
		t.Errorf("non-floated inline: Kind=%v, want BoxInline", b.Kind)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run 'TestFloatOf|TestBlockify|TestNoBlockify' -v`
Expected: FAIL — `applyFloatBlockify` undefined; `floatOf` currently ignores its argument and returns FloatNone (so `TestFloatOf` fails on the left/right cases).

- [ ] **Step 3: Implement `floatOf`**

Replace the stub in `pkg/layout/css/build.go` (~line 142):

```go
// floatOf maps a computed style's float keyword to a FloatKind. An empty or
// unrecognized value is FloatNone.
func floatOf(cs gcss.ComputedStyle) cssbox.FloatKind {
	switch cs.Float {
	case "left":
		return cssbox.FloatLeft
	case "right":
		return cssbox.FloatRight
	default:
		return cssbox.FloatNone
	}
}
```

- [ ] **Step 4: Implement `applyFloatBlockify` and call it in `generate`**

Add the helper near `classifyDisplay` in `build.go`:

```go
// applyFloatBlockify promotes a floated inline-level box to block-level, per CSS
// 2.1 §9.7 (a float computes display to a block-level value). A floated <img>
// stays BoxReplaced (block-level replaced); a floated display:inline/inline-block
// element becomes a block box so the layout engine lays it out as a float. Boxes
// that are already block-level, and non-floated boxes, are unchanged.
func applyFloatBlockify(b *cssbox.Box, cs gcss.ComputedStyle) {
	if floatOf(cs) == cssbox.FloatNone {
		return
	}
	if b.Kind == cssbox.BoxReplaced {
		return // replaced stays replaced; replaced sizing handles block-level
	}
	if b.Kind.IsInlineLevel() {
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayBlock, b.Formatting
	}
}
```

Note: `Formatting` is preserved (an inline element's children are inline, so it keeps `InlineFC`; the box is now a block-level box establishing an inline formatting context — exactly what a `<span style="float:left">text</span>` needs).

In `generate` (~line 82-84), after `classifyDisplay(b, cs.Display)`:

```go
	classifyDisplay(b, cs.Display)
	b.Float = floatOf(cs)
	b.Position = positionOf(cs)
	applyFloatBlockify(b, cs) // CSS 9.7: a float blockifies an inline-level box
```

(The existing lines set `b.Float`/`b.Position`; add the `applyFloatBlockify` call after them. Note `applyFloatBlockify` reads `cs` directly so it is order-independent of the `b.Float` assignment.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/layout/css -run 'TestFloatOf|TestBlockify|TestNoBlockify' -v`
Expected: PASS.

- [ ] **Step 6: Full package test + gofmt**

Run: `go test ./pkg/layout/css && gofmt -l pkg/layout/css`
Expected: PASS; `gofmt -l` silent. (The engine still ignores `b.Float` at layout time — that arrives in later tasks — so existing layout tests are unaffected.)

- [ ] **Step 7: Commit**

```bash
git add pkg/layout/css/build.go pkg/layout/css/build_test.go
git commit -m "css/build: map float keyword and blockify floated inlines"
```

---

## Task 3: The float context — geometry queries (`floats.go`)

**Files:**
- Create: `pkg/layout/css/floats.go`
- Create: `pkg/layout/css/floats_test.go`

This is the load-bearing geometry. Build it standalone and test it adversarially **before** wiring it into layout. All four methods are pure functions of `cbLeft`/`cbRight` + the `floats` slice.

- [ ] **Step 1: Write the failing tests**

Create `pkg/layout/css/floats_test.go`:

```go
package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

const eps = 1e-6

func approx(a, b float64) bool { d := a - b; return d < eps && d > -eps }

// newCtx makes a context spanning [left,right].
func newCtx(left, right float64) *floatContext {
	return &floatContext{cbLeft: left, cbRight: right}
}

// TestEdgesNoFloats: with no floats, the edges are the containing block edges.
func TestEdgesNoFloats(t *testing.T) {
	c := newCtx(0, 200)
	if l := c.leftEdge(0, 20); !approx(l, 0) {
		t.Errorf("leftEdge no floats = %v, want 0", l)
	}
	if r := c.rightEdge(0, 20); !approx(r, 200) {
		t.Errorf("rightEdge no floats = %v, want 200", r)
	}
}

// TestLeftFloatNarrowsLeftEdge: a left float pushes the left edge to its right
// margin edge within its vertical band, and only within it.
func TestLeftFloatNarrowsLeftEdge(t *testing.T) {
	c := newCtx(0, 200)
	c.floats = []floatBox{{side: cssbox.FloatLeft, x: 0, y: 0, w: 60, h: 50}}
	if l := c.leftEdge(0, 20); !approx(l, 60) {
		t.Errorf("leftEdge inside band = %v, want 60", l)
	}
	if l := c.leftEdge(60, 20); !approx(l, 0) { // below the float's bottom (y=50)
		t.Errorf("leftEdge below band = %v, want 0", l)
	}
	if r := c.rightEdge(0, 20); !approx(r, 200) { // a left float doesn't move the right edge
		t.Errorf("rightEdge with left float = %v, want 200", r)
	}
}

// TestRightFloatNarrowsRightEdge mirrors the left case.
func TestRightFloatNarrowsRightEdge(t *testing.T) {
	c := newCtx(0, 200)
	c.floats = []floatBox{{side: cssbox.FloatRight, x: 150, y: 0, w: 50, h: 50}}
	if r := c.rightEdge(0, 20); !approx(r, 150) {
		t.Errorf("rightEdge inside band = %v, want 150", r)
	}
	if r := c.rightEdge(60, 20); !approx(r, 200) {
		t.Errorf("rightEdge below band = %v, want 200", r)
	}
}

// TestOppositeFloatsNarrowBoth: a left and a right float narrow from both edges in
// the overlapping band.
func TestOppositeFloatsNarrowBoth(t *testing.T) {
	c := newCtx(0, 200)
	c.floats = []floatBox{
		{side: cssbox.FloatLeft, x: 0, y: 0, w: 40, h: 30},
		{side: cssbox.FloatRight, x: 160, y: 0, w: 40, h: 30},
	}
	if l := c.leftEdge(0, 20); !approx(l, 40) {
		t.Errorf("leftEdge = %v, want 40", l)
	}
	if r := c.rightEdge(0, 20); !approx(r, 160) {
		t.Errorf("rightEdge = %v, want 160", r)
	}
}

// TestPlaceStacksThenWraps: two left floats fit side by side; a third that doesn't
// fit wraps to a new row below the shallower of the two.
func TestPlaceStacksThenWraps(t *testing.T) {
	c := newCtx(0, 200)
	f1 := c.place(cssbox.FloatLeft, 80, 40, 0) // left edge: x=0
	if !approx(f1.x, 0) || !approx(f1.y, 0) {
		t.Fatalf("f1 = %+v, want x=0 y=0", f1)
	}
	f2 := c.place(cssbox.FloatLeft, 80, 60, 0) // next to f1: x=80
	if !approx(f2.x, 80) || !approx(f2.y, 0) {
		t.Fatalf("f2 = %+v, want x=80 y=0", f2)
	}
	// f3 width 80 cannot fit at y=0 (remaining 200-160=40 < 80). It drops past f1's
	// bottom (y=40), but f2 (height 60) still overlaps the band at y=40, so the
	// remaining width is still only 40 — it must drop again past f2's bottom (y=60),
	// where both floats have cleared and the full 200 is available. So f3 lands at
	// x=0, y=60 (a TWO-step wrap, because the two floats have different heights).
	f3 := c.place(cssbox.FloatLeft, 80, 30, 0)
	if !approx(f3.x, 0) || !approx(f3.y, 60) {
		t.Fatalf("f3 = %+v, want x=0 y=60", f3)
	}
}

// TestPlaceRightStacks: right floats stack from the right edge leftward.
func TestPlaceRightStacks(t *testing.T) {
	c := newCtx(0, 200)
	f1 := c.place(cssbox.FloatRight, 50, 40, 0) // right margin edge at 200 -> x=150
	if !approx(f1.x, 150) || !approx(f1.y, 0) {
		t.Fatalf("f1 = %+v, want x=150 y=0", f1)
	}
	f2 := c.place(cssbox.FloatRight, 50, 40, 0) // next left of f1 -> x=100
	if !approx(f2.x, 100) || !approx(f2.y, 0) {
		t.Fatalf("f2 = %+v, want x=100 y=0", f2)
	}
}

// TestPlaceOverflowWide: a float wider than the band is placed at the edge at the
// requested y (allowed to overflow) rather than looping forever.
func TestPlaceOverflowWide(t *testing.T) {
	c := newCtx(0, 100)
	f := c.place(cssbox.FloatLeft, 250, 30, 10)
	if !approx(f.x, 0) || !approx(f.y, 10) {
		t.Fatalf("overflow-wide float = %+v, want x=0 y=10", f)
	}
}

// TestPlaceNotAboveY: a float never rises above the requested y (content order).
func TestPlaceNotAboveY(t *testing.T) {
	c := newCtx(0, 200)
	f := c.place(cssbox.FloatLeft, 50, 20, 100)
	if f.y < 100-eps {
		t.Errorf("float placed at y=%v, want >= 100", f.y)
	}
}

// TestClearY drops to the bottom of the matching floats.
func TestClearY(t *testing.T) {
	c := newCtx(0, 200)
	c.floats = []floatBox{
		{side: cssbox.FloatLeft, x: 0, y: 0, w: 40, h: 30},   // bottom 30
		{side: cssbox.FloatRight, x: 160, y: 0, w: 40, h: 70}, // bottom 70
	}
	if y := c.clearY("left", 0); !approx(y, 30) {
		t.Errorf("clearY(left) = %v, want 30", y)
	}
	if y := c.clearY("right", 0); !approx(y, 70) {
		t.Errorf("clearY(right) = %v, want 70", y)
	}
	if y := c.clearY("both", 0); !approx(y, 70) {
		t.Errorf("clearY(both) = %v, want 70", y)
	}
	if y := c.clearY("none", 0); !approx(y, 0) {
		t.Errorf("clearY(none) = %v, want 0", y)
	}
	if y := c.clearY("both", 90); !approx(y, 90) { // already below all floats
		t.Errorf("clearY(both, 90) = %v, want 90", y)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/layout/css -run 'TestEdges|TestLeftFloat|TestRightFloat|TestOpposite|TestPlace|TestClearY' -v`
Expected: FAIL — `floatContext`, `floatBox`, and methods undefined.

- [ ] **Step 3: Implement `floats.go`**

Create `pkg/layout/css/floats.go`:

```go
package css

import "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"

// floatBox is one placed float's margin-box rectangle in page space (points,
// Y-down), tagged with its side and carrying its laid-out fragment for the float
// paint layer. Floats are positioned by their margin box (CSS 9.5): the margin
// edges touch the containing block's content edge or a preceding float's margin
// edge.
type floatBox struct {
	side       cssbox.FloatKind // FloatLeft | FloatRight
	x, y, w, h float64          // margin-box rectangle
	frag       *Fragment        // the laid-out fragment (border box inside this margin box)
}

// floatContext records the floats placed in ONE block formatting context and
// answers the avoidance geometry the block stacker and inline formatting context
// query per vertical band. cbLeft/cbRight are the containing block's content-box
// left/right edges — the band floats sit within. It is mutable state local to a
// single Engine.Layout goroutine (a fresh context per BFC); it never escapes that
// goroutine, so it does not violate the read-only-after-Layout concurrency
// contract.
type floatContext struct {
	cbLeft, cbRight float64
	floats          []floatBox
}

// overlaps reports whether band [y, y+h) intersects float f's vertical extent.
// A zero-height query band still intersects a float that contains its y.
func (f floatBox) overlaps(y, h float64) bool {
	return y < f.y+f.h && y+h > f.y
}

// leftEdge returns the left content boundary in band [y, y+h): cbLeft pushed right
// by the right margin edge of every left float overlapping the band.
func (c *floatContext) leftEdge(y, h float64) float64 {
	edge := c.cbLeft
	for i := range c.floats {
		f := c.floats[i]
		if f.side == cssbox.FloatLeft && f.overlaps(y, h) {
			if right := f.x + f.w; right > edge {
				edge = right
			}
		}
	}
	return edge
}

// rightEdge returns the right content boundary in band [y, y+h): cbRight pulled
// left by the left margin edge of every right float overlapping the band.
func (c *floatContext) rightEdge(y, h float64) float64 {
	edge := c.cbRight
	for i := range c.floats {
		f := c.floats[i]
		if f.side == cssbox.FloatRight && f.overlaps(y, h) {
			if f.x < edge {
				edge = f.x
			}
		}
	}
	return edge
}

// place positions a w×h (margin-box) float on side at the lowest y' >= y where it
// fits between the current left/right edges, appends it to the context, and returns
// the placed floatBox (frag still nil — the caller sets it). A float wider than the
// whole band is placed at the edge at y (allowed to overflow) rather than looping.
// The loop is bounded: each retry lowers y' past at least one blocking float.
func (c *floatContext) place(side cssbox.FloatKind, w, h, y float64) floatBox {
	bandW := c.cbRight - c.cbLeft
	for {
		left := c.leftEdge(y, h)
		right := c.rightEdge(y, h)
		if w > bandW || right-left >= w {
			// Fits (or is wider than the whole band -> overflow at the edge).
			var x float64
			if side == cssbox.FloatRight {
				x = right - w
			} else {
				x = left
			}
			fb := floatBox{side: side, x: x, y: y, w: w, h: h}
			c.floats = append(c.floats, fb)
			return fb
		}
		// Doesn't fit at y: drop to the bottom of the shallowest float whose band
		// overlaps [y, y+h) (the next opportunity for more width).
		next := c.nextDropY(y, h)
		if next <= y {
			// No lower opportunity (shouldn't happen given the fit test, but guard
			// against a spin): place at the edge at y.
			var x float64
			if side == cssbox.FloatRight {
				x = right - w
			} else {
				x = left
			}
			fb := floatBox{side: side, x: x, y: y, w: w, h: h}
			c.floats = append(c.floats, fb)
			return fb
		}
		y = next
	}
}

// nextDropY returns the smallest float bottom strictly greater than y among floats
// overlapping band [y, y+h); if none, returns y (caller guards against a spin).
func (c *floatContext) nextDropY(y, h float64) float64 {
	best := y
	for i := range c.floats {
		f := c.floats[i]
		if !f.overlaps(y, h) {
			continue
		}
		if bottom := f.y + f.h; bottom > y && (best == y || bottom < best) {
			best = bottom
		}
	}
	return best
}

// clearY returns the lowest y at or below the input y that clears the named side(s):
// "left"/"right" clear that side, "both" clears all floats, "none" returns y.
func (c *floatContext) clearY(clear string, y float64) float64 {
	if clear == "none" || clear == "" {
		return y
	}
	out := y
	for i := range c.floats {
		f := c.floats[i]
		match := clear == "both" ||
			(clear == "left" && f.side == cssbox.FloatLeft) ||
			(clear == "right" && f.side == cssbox.FloatRight)
		if match {
			if bottom := f.y + f.h; bottom > out {
				out = bottom
			}
		}
	}
	return out
}

// floats2frags returns the fragments of the placed floats, in placement order, for
// the BFC owner to attach to its fragment's Floats slice (the float paint layer).
// nil-frag entries are skipped. This is also why floatBox carries frag: the geometry
// records each placed float's laid-out fragment so the paint layer can collect them.
func (c *floatContext) floats2frags() []*Fragment {
	if len(c.floats) == 0 {
		return nil
	}
	out := make([]*Fragment, 0, len(c.floats))
	for i := range c.floats {
		if c.floats[i].frag != nil {
			out = append(out, c.floats[i].frag)
		}
	}
	return out
}
```

**Why `floats2frags` is in this task** (not deferred to Task 6 where it is first *called*): the `floatBox.frag` field would otherwise be unread at this commit, and `golangci-lint`'s `unused` check flags an unexported struct field with no reads — which fails CI (CLAUDE.md requires a clean lint). The codebase uses **no** `//nolint` suppressions, so rather than introduce one, this read-side helper lives with the geometry type it belongs to and keeps the commit lint-clean. Task 6 (`placeFloat`) supplies the write side and the first call.

Add a test exercising it (append to `floats_test.go`):

```go
// TestFloats2Frags returns the placed floats' fragments in order, skipping nil.
func TestFloats2Frags(t *testing.T) {
	c := newCtx(0, 200)
	if got := c.floats2frags(); got != nil {
		t.Errorf("empty context floats2frags = %v, want nil", got)
	}
	fa, fb := &Fragment{X: 1}, &Fragment{X: 2}
	c.floats = []floatBox{
		{side: cssbox.FloatLeft, frag: fa},
		{side: cssbox.FloatRight, frag: nil}, // skipped
		{side: cssbox.FloatLeft, frag: fb},
	}
	got := c.floats2frags()
	if len(got) != 2 || got[0] != fa || got[1] != fb {
		t.Errorf("floats2frags = %v, want [fa fb] (nil skipped, order preserved)", got)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/layout/css -run 'TestEdges|TestLeftFloat|TestRightFloat|TestOpposite|TestPlace|TestClearY|TestFloats2Frags' -v`
Expected: PASS (all geometry cases incl. stacking, wrap, overflow-wide, clear, floats2frags).

- [ ] **Step 5: vet + gofmt + full package + lint (the lint matters — frag must be read)**

Run: `go test ./pkg/layout/css && go vet ./pkg/layout/css && gofmt -l pkg/layout/css && golangci-lint run ./pkg/layout/css/...`
Expected: PASS; vet clean; `gofmt -l` silent; **golangci-lint clean** (no "field frag is unused" — `floats2frags` reads it).

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/floats.go pkg/layout/css/floats_test.go
git commit -m "css: float context geometry (leftEdge/rightEdge/place/clearY)"
```

---

## Task 4: `BreakNext` — one greedy line at a width (shared inline core)

**Files:**
- Modify: `pkg/layout/inline/break.go`
- Test: `pkg/layout/inline/break_test.go` (create if absent)

The float IFC needs to break a paragraph one line at a time at a width that varies per line. `Break` consumes the whole slice; `BreakNext` returns one line + the remainder, sharing the greedy/space-break logic. `Break` is unchanged (DOCX and the non-float path keep using it).

- [ ] **Step 1: Write the failing test**

Create or append `pkg/layout/inline/break_test.go`:

```go
package inline

import "testing"

// mkGlyphs builds a glyph run from (advance, isSpace, isBreak) triples. Each glyph
// has a unit advance unless given; spaces are marked so the breaker can break there.
func mkRun(words []string, advancePerChar, spaceAdvance float64) []Glyph {
	var gs []Glyph
	for wi, w := range words {
		if wi > 0 {
			gs = append(gs, Glyph{Advance: spaceAdvance, Space: true})
		}
		for range w {
			gs = append(gs, Glyph{Advance: advancePerChar})
		}
	}
	return gs
}

// TestBreakNextOneLine: BreakNext returns the glyphs that fit on one line plus the
// remainder, breaking at the last space before overflow.
func TestBreakNextOneLine(t *testing.T) {
	// "aa bb cc" with char advance 10, space 10: widths 20/10/20/10/20.
	gs := mkRun([]string{"aa", "bb", "cc"}, 10, 10)
	// Width 45 fits "aa bb" (20+10+20=50 visible? "aa"+" "+"bb" = 20+10+20=50 > 45;
	// so only "aa" fits at 45: visible 20 <= 45, "aa "=30, +"b"=40, +"b"=50 > 45 ->
	// break at the space after "aa").
	line, rest := BreakNext(gs, 45)
	if got := VisibleWidth(line); got != 20 {
		t.Errorf("line visible width = %v, want 20 (\"aa\")", got)
	}
	if len(rest) == 0 {
		t.Fatalf("rest is empty, want \"bb cc\" remainder")
	}
	// The remainder should start at "bb" (the breaking space is consumed).
	if rest[0].Space {
		t.Errorf("rest[0] is a space; the breaking space should be consumed")
	}
}

// TestBreakNextEquivalence: repeatedly calling BreakNext at a fixed width yields the
// same lines as one Break call at that width (the non-float path is unchanged).
func TestBreakNextEquivalence(t *testing.T) {
	gs := mkRun([]string{"alpha", "beta", "gamma", "delta", "epsilon"}, 8, 8)
	const w = 80

	want := Break(gs, w, w)

	var got []Line
	rest := gs
	for len(rest) > 0 {
		var line []Glyph
		line, rest = BreakNext(rest, w)
		got = append(got, MakeLine(line))
	}
	if len(got) != len(want) {
		t.Fatalf("BreakNext produced %d lines, Break produced %d", len(got), len(want))
	}
	for i := range want {
		if !floatEq(got[i].WidthPt, want[i].WidthPt) {
			t.Errorf("line %d width: BreakNext %v vs Break %v", i, got[i].WidthPt, want[i].WidthPt)
		}
		if len(got[i].Glyphs) != len(want[i].Glyphs) {
			t.Errorf("line %d glyph count: BreakNext %d vs Break %d", i, len(got[i].Glyphs), len(want[i].Glyphs))
		}
	}
}

func floatEq(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }

// TestBreakNextOverlongWord: a single word wider than the width is taken alone
// (overflows) and the remainder is empty.
func TestBreakNextOverlongWord(t *testing.T) {
	gs := mkRun([]string{"superlongword"}, 10, 10) // 130 wide
	line, rest := BreakNext(gs, 50)
	if len(line) != len(gs) {
		t.Errorf("overlong word: line has %d glyphs, want all %d", len(line), len(gs))
	}
	if len(rest) != 0 {
		t.Errorf("overlong word: rest has %d glyphs, want 0", len(rest))
	}
}

// TestBreakNextForcedBreak: a Break glyph ends the line; the remainder continues
// after it.
func TestBreakNextForcedBreak(t *testing.T) {
	gs := []Glyph{
		{Advance: 10}, {Advance: 10}, // "aa"
		{Break: true},
		{Advance: 10}, {Advance: 10}, // "bb"
	}
	line, rest := BreakNext(gs, 1000) // width large; only the forced break splits
	if len(line) != 2 {
		t.Errorf("forced break: line has %d glyphs, want 2", len(line))
	}
	if len(rest) != 2 {
		t.Errorf("forced break: rest has %d glyphs, want 2 (after the break glyph)", len(rest))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/inline -run 'TestBreakNext' -v`
Expected: FAIL — `BreakNext` undefined.

- [ ] **Step 3: Implement `BreakNext`**

Add to `pkg/layout/inline/break.go` (after `Break`):

```go
// BreakNext greedily takes ONE line from the front of glyphs at the given width and
// returns it plus the unconsumed remainder. It applies the same first-fit rule as
// Break: break at the last space before the line overflows; a single word wider than
// the width is taken alone (overflow); a forced-break glyph (Break) ends the line and
// is consumed. The returned line and rest are independent slices into the input (the
// rest is a subslice; the line is copied only when a mid-run break occurs). When
// glyphs is empty, line is empty and rest is nil.
//
// BreakNext is the per-line driver the CSS float formatting context uses to break a
// paragraph against a width that varies by vertical position (a float narrows some
// lines but not others). Break remains the whole-paragraph entry point for the
// fixed-width path (DOCX and non-floated HTML). Driving BreakNext repeatedly at a
// fixed width reproduces Break's lines.
func BreakNext(glyphs []Glyph, widthPt float64) (line, rest []Glyph) {
	if len(glyphs) == 0 {
		return nil, nil
	}
	for i := 0; i < len(glyphs); i++ {
		g := glyphs[i]
		if g.Break {
			// Forced break: line is everything before the break glyph; the break glyph
			// itself is consumed (not carried to the next line).
			return glyphs[:i], glyphs[i+1:]
		}
		cur := glyphs[:i+1]
		if VisibleWidth(cur) <= widthPt {
			continue
		}
		// cur now overflows. Break at the last space before the overflow.
		brk := lastSpaceBefore(cur, len(cur)-1)
		if brk < 0 {
			// One long word in progress with no break opportunity yet: keep filling
			// until a space appears or the run ends.
			continue
		}
		// Keep [0:brk] on this line; the breaking space at brk is consumed.
		return glyphs[:brk], glyphs[brk+1:]
	}
	// The whole remaining run fits (or is one overlong word): it is the line.
	return glyphs, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/layout/inline -run 'TestBreakNext' -v`
Expected: PASS — incl. the `Break` equivalence test.

- [ ] **Step 5: Full inline package test (guard the shared core)**

Run: `go test ./pkg/layout/inline && go vet ./pkg/layout/inline && gofmt -l pkg/layout/inline`
Expected: PASS (existing `Break`/`Shape`/`MakeLine` tests still green); vet clean; gofmt silent.

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/inline/break.go pkg/layout/inline/break_test.go
git commit -m "inline: add BreakNext (one greedy line at a width) for the float IFC"
```

---

## Task 5: Fragment float flags + phase-split `AppendItems` (paint layer)

**Files:**
- Modify: `pkg/layout/css/fragment.go`
- Test: `pkg/layout/css/floats_layout_test.go` (create)

Add `IsFloat` and `Floats` to `Fragment`, and refactor `AppendItems` into three phases sequenced by a BFC-root fragment: block decorations (skip floats) → floats → inline content (skip floats). At this task the layout engine does not yet populate `Floats`/`IsFloat`, so existing goldens are unaffected; the test drives `AppendItems` on a hand-built tree to lock the paint order.

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/floats_layout_test.go`:

```go
package css

import (
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// solidGlyph returns a fragment line with one fillable glyph (so the inline phase
// emits a GlyphKind item). The outline is a tiny non-empty path.
func glyphLine(y float64) LineFragment {
	return LineFragment{BaselineY: y, Glyphs: []GlyphFragment{{Outline: unitGlyph(), X: 0, SizePt: 10, Color: color.RGBA{0, 0, 0, 255}}}}
}

// TestAppendItemsFloatPaintOrder: a float overlapping a later in-flow sibling paints
// AFTER the sibling's background/border but BEFORE its glyphs (CSS Appendix E).
func TestAppendItemsFloatPaintOrder(t *testing.T) {
	// BFC root with: [0] a float (background only), [1] an in-flow sibling with a
	// background AND a glyph. Expected item order:
	//   sibling background, (sibling has no border), float background, sibling glyph.
	floatFrag := &Fragment{X: 0, Y: 0, W: 50, H: 50, Background: color.RGBA{255, 0, 0, 255}, IsFloat: true}
	sibling := &Fragment{X: 0, Y: 0, W: 200, H: 80, Background: color.RGBA{0, 255, 0, 255}, Lines: []LineFragment{glyphLine(20)}}

	root := &Fragment{
		X: 0, Y: 0, W: 200, H: 80,
		Children: []*Fragment{sibling}, // the float is NOT an in-flow child
		Floats:   []*Fragment{floatFrag},
		IsBFC:    true,
	}

	items := root.AppendItems(nil)

	// Find the index of each kind/color of interest.
	idxSiblingBg, idxFloatBg, idxGlyph := -1, -1, -1
	for i := range items {
		switch items[i].Kind {
		case layout.BackgroundKind:
			if items[i].Rule.Color == (color.RGBA{0, 255, 0, 255}) {
				idxSiblingBg = i
			}
			if items[i].Rule.Color == (color.RGBA{255, 0, 0, 255}) {
				idxFloatBg = i
			}
		case layout.GlyphKind:
			idxGlyph = i
		}
	}
	if idxSiblingBg < 0 || idxFloatBg < 0 || idxGlyph < 0 {
		t.Fatalf("missing items: siblingBg=%d floatBg=%d glyph=%d (items=%d)", idxSiblingBg, idxFloatBg, idxGlyph, len(items))
	}
	if idxSiblingBg >= idxFloatBg || idxFloatBg >= idxGlyph { // De Morgan of !(a<b && b<c); golangci-lint QF1001
		t.Errorf("paint order wrong: siblingBg=%d floatBg=%d glyph=%d; want siblingBg < floatBg < glyph",
			idxSiblingBg, idxFloatBg, idxGlyph)
	}
}

// TestAppendItemsNoFloatsUnchanged: a BFC root with no floats still emits its own
// background before a child's (tree order within the background layer).
func TestAppendItemsNoFloatsUnchanged(t *testing.T) {
	child := &Fragment{X: 0, Y: 0, W: 100, H: 20, Background: color.RGBA{0, 0, 255, 255}}
	root := &Fragment{X: 0, Y: 0, W: 100, H: 40, Background: color.RGBA{200, 200, 200, 255}, Children: []*Fragment{child}, IsBFC: true}

	items := root.AppendItems(nil)
	if len(items) != 2 || items[0].Kind != layout.BackgroundKind || items[1].Kind != layout.BackgroundKind {
		t.Fatalf("unexpected items %+v", items)
	}
	// Root background paints before the child's.
	if items[0].Rule.Color != (color.RGBA{200, 200, 200, 255}) {
		t.Errorf("root background not first")
	}
}

// TestAppendItemsBlockBgBeforeInlineContent: per CSS 2.1 Appendix E, ALL in-flow
// block-level backgrounds/borders paint (step 4) before ANY in-flow inline content
// (step 7), even across nesting. So a nested block's background paints before its
// parent's text. This is the intended layered order, NOT a regression — this test
// pins it so the phase-split is validated beyond the trivial two-background case.
func TestAppendItemsBlockBgBeforeInlineContent(t *testing.T) {
	// Parent block: a background + a text glyph; contains a nested block with its own
	// background. Expected order: parent bg, nested bg (both in the bg layer, tree
	// order), THEN parent glyph (content layer).
	nested := &Fragment{X: 0, Y: 0, W: 50, H: 10, Background: color.RGBA{0, 0, 255, 255}}
	parent := &Fragment{
		X: 0, Y: 0, W: 100, H: 40,
		Background: color.RGBA{200, 200, 200, 255},
		Lines:      []LineFragment{glyphLine(8)},
		Children:   []*Fragment{nested},
		IsBFC:      true,
	}
	items := parent.AppendItems(nil)

	idxParentBg, idxNestedBg, idxGlyph := -1, -1, -1
	for i := range items {
		switch items[i].Kind {
		case layout.BackgroundKind:
			if items[i].Rule.Color == (color.RGBA{200, 200, 200, 255}) {
				idxParentBg = i
			}
			if items[i].Rule.Color == (color.RGBA{0, 0, 255, 255}) {
				idxNestedBg = i
			}
		case layout.GlyphKind:
			idxGlyph = i
		}
	}
	if idxParentBg < 0 || idxNestedBg < 0 || idxGlyph < 0 {
		t.Fatalf("missing items: parentBg=%d nestedBg=%d glyph=%d", idxParentBg, idxNestedBg, idxGlyph)
	}
	if idxParentBg >= idxNestedBg || idxNestedBg >= idxGlyph { // De Morgan; golangci-lint QF1001
		t.Errorf("Appendix E order violated: parentBg=%d nestedBg=%d glyph=%d; want parentBg < nestedBg < glyph",
			idxParentBg, idxNestedBg, idxGlyph)
	}
}

// TestAppendItemsNestedBFCAtomic: a nested BFC child (e.g. an inline-block) paints
// as a single atom in the OUTER BFC's content phase — its own background and its
// own float paint together (the inner float between the inner bg and inner content),
// NOT split into the outer BFC's decoration/float/content phases. Concretely: the
// outer BFC has a float; the inner BFC (a child) has its own background and its own
// float. The outer float must NOT be interleaved with the inner BFC's internals.
func TestAppendItemsNestedBFCAtomic(t *testing.T) {
	outerFloat := &Fragment{X: 0, Y: 0, W: 20, H: 20, Background: color.RGBA{255, 0, 0, 255}, IsFloat: true}

	innerFloat := &Fragment{X: 100, Y: 0, W: 10, H: 10, Background: color.RGBA{0, 255, 0, 255}, IsFloat: true}
	innerBFC := &Fragment{
		X: 90, Y: 0, W: 60, H: 40,
		Background: color.RGBA{0, 0, 255, 255}, // inner bg
		Lines:      []LineFragment{glyphLine(10)},
		Floats:     []*Fragment{innerFloat},
		IsBFC:      true,
	}

	root := &Fragment{
		X: 0, Y: 0, W: 200, H: 60,
		Children: []*Fragment{innerBFC},
		Floats:   []*Fragment{outerFloat},
		IsBFC:    true,
	}

	items := root.AppendItems(nil)

	// Locate the outer float (red bg) and the inner BFC's items (blue bg, green float
	// bg, glyph). The inner BFC's three items must be contiguous-in-order AFTER the
	// outer float (the outer content phase), proving the inner BFC painted atomically.
	idxOuterFloat, idxInnerBg, idxInnerFloat, idxInnerGlyph := -1, -1, -1, -1
	for i := range items {
		switch items[i].Kind {
		case layout.BackgroundKind:
			switch items[i].Rule.Color {
			case color.RGBA{255, 0, 0, 255}:
				idxOuterFloat = i
			case color.RGBA{0, 0, 255, 255}:
				idxInnerBg = i
			case color.RGBA{0, 255, 0, 255}:
				idxInnerFloat = i
			}
		case layout.GlyphKind:
			idxInnerGlyph = i
		}
	}
	if idxOuterFloat < 0 || idxInnerBg < 0 || idxInnerFloat < 0 || idxInnerGlyph < 0 {
		t.Fatalf("missing items: outerFloat=%d innerBg=%d innerFloat=%d innerGlyph=%d",
			idxOuterFloat, idxInnerBg, idxInnerFloat, idxInnerGlyph)
	}
	// Outer float paints before the inner BFC atom (outer float layer precedes outer
	// content phase, which is where the inner BFC paints).
	if idxOuterFloat > idxInnerBg {
		t.Errorf("outer float (%d) painted after inner BFC bg (%d); want before", idxOuterFloat, idxInnerBg)
	}
	// Inner BFC paints atomically and in its own Appendix-E order: inner bg, then its
	// float, then its glyph — contiguous, with nothing else between.
	if idxInnerBg >= idxInnerFloat || idxInnerFloat >= idxInnerGlyph { // De Morgan; golangci-lint QF1001
		t.Errorf("inner BFC internal order wrong: bg=%d float=%d glyph=%d; want bg<float<glyph",
			idxInnerBg, idxInnerFloat, idxInnerGlyph)
	}
}
```

Add a tiny non-empty glyph outline helper (if one is not already available in the package's tests). Append to the same file:

```go
// unitGlyph returns a minimal non-empty render.Path so a GlyphFragment emits a
// GlyphKind item (AppendItems skips a nil outline).
func unitGlyph() *render.Path {
	p := &render.Path{}
	p.MoveTo(0, 0)
	p.LineTo(1, 0)
	p.LineTo(1, 1)
	p.Close()
	return p
}
```

…and add the import `"github.com/nathanstitt/doctaculous/pkg/render"`. (If `render.Path`'s builder methods differ, mirror how `pkg/layout/paint` or existing css tests build a path — check `pkg/render` for the exact `MoveTo`/`LineTo`/`Close` signatures; adjust to a non-empty path.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run 'TestAppendItemsFloat|TestAppendItemsNoFloats' -v`
Expected: FAIL — `Fragment.IsFloat`, `Fragment.Floats`, `Fragment.IsBFC` undefined.

- [ ] **Step 3: Add the fragment fields**

In `pkg/layout/css/fragment.go`, in the `Fragment` struct (after `Image`):

```go
	// IsFloat marks a fragment produced by a floated box. The float paint phases
	// skip such subtrees during the in-flow passes and paint them in the float pass
	// instead (CSS 2.1 Appendix E).
	IsFloat bool
	// IsBFC marks a fragment that establishes a block formatting context (the page
	// root and inline-blocks). Such a fragment owns the float-layer paint sequencing
	// for the floats placed in its BFC (held in Floats); a non-BFC fragment recurses
	// normally within each phase.
	IsBFC bool
	// Floats holds the fragments of floats placed in this fragment's BFC, painted in
	// their own layer (after in-flow block decorations, before in-flow inline
	// content). Set only on an IsBFC fragment. Kept separate from Children so in-flow
	// tree order is untouched.
	Floats []*Fragment
```

- [ ] **Step 4: Refactor `AppendItems` into phases**

Replace the body of `AppendItems` in `fragment.go`. The new structure: a BFC-root fragment runs the three phases; a non-BFC fragment paints itself + recurses (the old behavior, but skipping float subtrees so a stray float inside a non-BFC subtree still defers correctly — in practice floats attach at the BFC root).

```go
// AppendItems appends f's drawing primitives, and its descendants', to dst in CSS
// paint order and returns the extended slice. For a fragment that establishes a
// block formatting context (IsBFC), the order follows CSS 2.1 Appendix E within the
// context: in-flow block backgrounds/borders, then floats (Floats), then in-flow
// inline content / images / atomics — each phase skipping floated subtrees in the
// in-flow passes. A non-BFC fragment paints its own background/border/inline/image
// then recurses into its children (normal-flow tree order), which is what the BFC
// phases call per in-flow subtree.
//
// AppendItems only reads the fragment tree; it does not mutate it, so it is safe to
// call on a tree shared across the render fan-out.
func (f *Fragment) AppendItems(dst []layout.Item) []layout.Item {
	if f.IsBFC {
		dst = f.appendDecorations(dst) // in-flow backgrounds + borders (skip floats)
		for _, fl := range f.Floats {  // the float layer
			dst = fl.AppendItems(dst)
		}
		dst = f.appendContent(dst) // in-flow inline content + images (skip floats)
		return dst
	}
	// Non-BFC fragment: paint self, then recurse (normal tree order). This is the
	// per-subtree behavior the BFC phases invoke; it is unchanged from the original
	// single-pass AppendItems except that a floated subtree is skipped (the BFC root
	// paints it via Floats instead).
	dst = f.appendSelfDecorations(dst)
	dst = f.appendSelfContent(dst)
	for _, c := range f.Children {
		if c.IsFloat {
			continue
		}
		dst = c.AppendItems(dst)
	}
	return dst
}

// appendDecorations recurses the in-flow subtree emitting only backgrounds and
// borders, skipping floated subtrees (painted in the float layer) and NESTED BFC
// subtrees (an inline-block / new-BFC box paints as a single atom in the content
// phase via its own AppendItems — its internal block/float/inline layering is
// self-contained, so it must not be flattened into this BFC's decoration layer).
func (f *Fragment) appendDecorations(dst []layout.Item) []layout.Item {
	if f.IsFloat {
		return dst
	}
	dst = f.appendSelfDecorations(dst)
	for _, c := range f.Children {
		if c.IsBFC {
			continue // painted whole in the content phase (atomic)
		}
		dst = c.appendDecorations(dst)
	}
	return dst
}

// appendContent recurses the in-flow subtree emitting glyphs, images, and inline
// atomics, skipping floated subtrees. A NESTED BFC child (inline-block / new BFC)
// is painted here as a single atom via its full AppendItems — running its own
// decoration → float → content phases as a self-contained unit (CSS paints an
// atomic inline / BFC as one item in step 7), rather than being split across this
// BFC's phases.
func (f *Fragment) appendContent(dst []layout.Item) []layout.Item {
	if f.IsFloat {
		return dst
	}
	dst = f.appendSelfContent(dst)
	for _, c := range f.Children {
		if c.IsBFC {
			dst = c.AppendItems(dst) // atomic: its own full phase sequence
			continue
		}
		dst = c.appendContent(dst)
	}
	return dst
}

// appendSelfDecorations emits this fragment's own background then border edges (no
// recursion).
func (f *Fragment) appendSelfDecorations(dst []layout.Item) []layout.Item {
	if f.Background.A > 0 {
		dst = append(dst, layout.Item{
			Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: f.X, YPt: f.Y, WPt: f.W, HPt: f.H, Color: f.Background},
		})
	}
	for _, s := range [...]layout.EdgeSide{layout.EdgeTop, layout.EdgeRight, layout.EdgeBottom, layout.EdgeLeft} {
		e := f.Border[s]
		if e.Width <= 0 || e.Style == layout.BorderNone {
			continue
		}
		dst = append(dst, layout.Item{Kind: layout.BorderKind, Border: f.edgeStrip(s, e)})
	}
	return dst
}

// appendSelfContent emits this fragment's own inline line glyphs then its replaced
// image (no recursion).
func (f *Fragment) appendSelfContent(dst []layout.Item) []layout.Item {
	for li := range f.Lines {
		ln := &f.Lines[li]
		for gi := range ln.Glyphs {
			g := &ln.Glyphs[gi]
			if g.Outline == nil {
				continue
			}
			dst = append(dst, layout.Item{
				Kind: layout.GlyphKind,
				Glyph: layout.GlyphItem{Outline: g.Outline, XPt: g.X, YPt: ln.BaselineY, SizePt: g.SizePt, Color: g.Color},
			})
		}
	}
	if f.Image != nil && f.Image.Img != nil {
		dst = append(dst, layout.Item{
			Kind: layout.ImageKind,
			Image: layout.ImageItem{
				Img: f.Image.Img,
				XPt: f.Image.CX, YPt: f.Image.CY, WPt: f.Image.CW, HPt: f.Image.CH,
				Fit: f.Image.Fit,
			},
		})
	}
	return dst
}
```

**Important ordering subtlety — this is intended CSS, not a regression:** CSS 2.1 Appendix E paints, within a stacking context, **all** in-flow block-level backgrounds/borders (step 4, the element + its in-flow non-positioned block-level descendants in tree order) → **floats** (step 5) → **all** in-flow inline content (step 7). So the BFC path correctly separates the whole subtree's block decorations from its inline content, with floats between. This means a nested block's background paints before its parent's text — which is exactly Appendix E (and matches what a browser does). `TestAppendItemsBlockBgBeforeInlineContent` (added in Step 1) pins this ordering positively.

For the **no-float** BFC root the layered order can differ from the old strict-tree single pass for a *mixed inline+block* subtree (old: parent bg, parent text, child bg; new: parent bg, child bg, parent text). The **new** order is the Appendix-E-correct one. In practice normal-flow boxes do not overlap, so the existing golden pages are pixel-identical either way (Task 9 confirms). If a golden *does* change, inspect it: a change is only expected where a block descendant's box overlaps an ancestor's inline content, and the new order is the correct one — but eyeball it before blessing.

The **non-BFC** path keeps the original per-fragment order (`appendSelfDecorations` + `appendSelfContent` + children) because it is only ever invoked *within* a phase by the BFC root (the decorations pass calls `appendDecorations`, the content pass calls `appendContent`); the self+children form is used only when a fragment is reached outside the phase machinery, which for the current engine does not happen (every tree is rooted at an `IsBFC` fragment). It is kept as a correct fallback.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/layout/css -run 'TestAppendItemsFloat|TestAppendItemsNoFloats' -v`
Expected: PASS.

- [ ] **Step 6: Guard existing layout output**

Run: `go test ./pkg/layout/css && gofmt -l pkg/layout/css`
Expected: PASS. (The root fragment is not yet marked `IsBFC` by the engine — that is wired in Task 6 — so existing fragment-geometry tests are unaffected. The golden/reftest impact is checked in Task 9 after the engine sets `IsBFC`.)

- [ ] **Step 7: Commit**

```bash
git add pkg/layout/css/fragment.go pkg/layout/css/floats_layout_test.go
git commit -m "css: fragment float flags + phase-split AppendItems for the float paint layer"
```

---

## Task 6: Thread the float context through block layout

**Files:**
- Modify: `pkg/layout/css/block.go` (`layoutTree`, `layoutBlock`, `layoutInterior`, `layoutBlockChildren`, `layoutBlockReplaced`)
- Modify: `pkg/layout/css/inline.go` (`layoutInline` signature — pass-through only this task; float-driving is Task 7)
- Test: `pkg/layout/css/floats_layout_test.go` (append)

Thread a `*floatContext` parameter through the block layout functions. `layoutTree` creates the root BFC context and marks the root fragment `IsBFC`. In `layoutBlockChildren`: place a floated child out of flow (collect on the BFC root's `Floats`, set `IsFloat`, do not advance the cursor); lower a `clear`ed child; in-flow blocks pass the SAME context down (a new BFC creates a fresh one). This task wires the plumbing and float PLACEMENT; the IFC narrowing (text wrapping) is Task 7.

- [ ] **Step 1: Write the failing test**

Append to `pkg/layout/css/floats_layout_test.go`:

```go
import (
	// add to the existing import block:
	"context"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// blockBox builds a minimal block box with the given style and children.
func blockBox(style gcss.ComputedStyle, kids ...*cssbox.Box) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: style, Children: kids}
}

// TestFloatPlacedOutOfFlow: a left-floated child div is placed at the content-box
// left, marked IsFloat, collected on the root's Floats, and does NOT advance the
// in-flow cursor (a following in-flow block starts at y=0, beside the float).
func TestFloatPlacedOutOfFlow(t *testing.T) {
	eng := New(nil, nil, nil)

	floatStyle := gcss.ComputedStyle{Display: "block", Float: "left",
		Width:  gcss.Length{Value: 60, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 40, Unit: gcss.UnitPx}}
	floatStyle.Float = "left"
	floated := blockBox(floatStyle)
	floated.Float = cssbox.FloatLeft

	following := blockBox(gcss.ComputedStyle{Display: "block",
		Height: gcss.Length{Value: 30, Unit: gcss.UnitPx}})

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated, following)

	frag := eng.layoutTree(context.Background(), root, 200)
	if frag == nil {
		t.Fatal("nil root fragment")
	}
	if !frag.IsBFC {
		t.Errorf("root fragment not marked IsBFC")
	}
	if len(frag.Floats) != 1 {
		t.Fatalf("root has %d floats, want 1", len(frag.Floats))
	}
	ff := frag.Floats[0]
	if !ff.IsFloat {
		t.Errorf("float fragment not marked IsFloat")
	}
	if ff.X != 0 || ff.Y != 0 {
		t.Errorf("float at (%v,%v), want (0,0)", ff.X, ff.Y)
	}
	// The following in-flow block is a normal child; it should start at y=0 (the
	// float did not consume vertical space).
	if len(frag.Children) != 1 {
		t.Fatalf("root has %d in-flow children, want 1 (the float is not a child)", len(frag.Children))
	}
	if frag.Children[0].Y != 0 {
		t.Errorf("following block Y=%v, want 0 (float out of flow)", frag.Children[0].Y)
	}
}

// TestClearDropsBelowFloat: a clear:left block starts below a preceding left float.
func TestClearDropsBelowFloat(t *testing.T) {
	eng := New(nil, nil, nil)

	floated := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 60, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 40, Unit: gcss.UnitPx}})
	floated.Float = cssbox.FloatLeft

	cleared := blockBox(gcss.ComputedStyle{Display: "block", Clear: "left",
		Height: gcss.Length{Value: 20, Unit: gcss.UnitPx}})

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated, cleared)
	frag := eng.layoutTree(context.Background(), root, 200)

	if len(frag.Children) != 1 {
		t.Fatalf("want 1 in-flow child, got %d", len(frag.Children))
	}
	if frag.Children[0].Y < 40-1e-6 {
		t.Errorf("cleared block Y=%v, want >= 40 (below the float)", frag.Children[0].Y)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run 'TestFloatPlacedOutOfFlow|TestClearDropsBelowFloat' -v`
Expected: FAIL — `layoutTree` does not take a context-of-floats / does not place floats; root not `IsBFC`.

- [ ] **Step 3: Thread the context through the block functions**

In `pkg/layout/css/block.go`:

`layoutTree` — create the root context and mark the root BFC:

```go
func (e *Engine) layoutTree(ctx context.Context, root *cssbox.Box, viewportW float64) *Fragment {
	if root == nil {
		return nil
	}
	fc := &floatContext{cbLeft: 0, cbRight: viewportW}
	// Root BFC: bandOriginY = 0 (its content-box top defines the frame origin).
	res := e.layoutBlock(ctx, root, viewportW, 0, 0, 0, fc)
	if res.frag != nil {
		res.frag.IsBFC = true
		// The root is the BFC owner: collect any floats placed directly in it. (A
		// nested-BFC box collects its own via layoutInterior -> in.bfcFloats.)
		if res.frag.Floats == nil {
			res.frag.Floats = fc.floats2frags()
		}
	}
	return res.frag
}
```

Note the `if res.frag.Floats == nil` guard: `layoutBlock` already set `frag.Floats` for a box that establishes its own BFC (which the root, having no float/inline-block trigger, normally does not — so the root collects here). The guard avoids double-assigning if the root itself were ever BFC-triggering.

(`floats2frags` was already added to `floats.go` in Task 3 — it reads `floatBox.frag`, which Task 3 left lint-clean. This task supplies the write side: `placeFloat` sets `frag` on each placed float, so `floats2frags` now returns real fragments.)

**Signature decision (final — used by Tasks 6, 7, 8):** every layout function gains a `*floatContext` parameter AND a `bandOriginY float64` parameter (the box's content-box top measured in its BFC-root-local frame — used to query the float context in a consistent frame; see Task 8). The root BFC has `bandOriginY = 0`. Introducing both now means no signature changes later. The final signatures are:

```go
func (e *Engine) layoutBlock(ctx context.Context, b *cssbox.Box, cbWidth, originX, marginTopEdgeY, bandOriginY float64, fc *floatContext) blockResult
func (e *Engine) layoutInterior(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext) interior
func (e *Engine) layoutBlockChildren(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext) interior
func (e *Engine) layoutInline(ctx context.Context, b *cssbox.Box, contentW, contentTopY, contentX, bandOriginY float64, fc *floatContext) (lines []LineFragment, height float64, atomics []*Fragment)
```

In **this** task `bandOriginY` is threaded but used only to compute the float-placement Y (`bandOriginY + cursorY`); the IFC's use of it for per-line narrowing is wired in Tasks 7–8. The float-placement tests below pass regardless, because at the root `bandOriginY == 0`.

`layoutBlock` — this is an **edit in place** of the existing function (`pkg/layout/css/block.go:100`), not a rewrite. Make exactly four changes; leave the rest of the body (the ~70 lines of edges/width resolution, margin-collapse, height resolution, and `frag` construction at `block.go:111-196`) **verbatim**:

1. **Signature**: append `, bandOriginY float64, fc *floatContext` before the return type.
2. After the edges/width block computes `ed`/`contentW`/`contentX` (just before the existing `in := e.layoutInterior(...)` call at `block.go:123`), insert the `childBandOrigin` line.
3. **Change the `layoutInterior` call** to pass `childBandOrigin, fc`.
4. After the `frag` is constructed and before `return blockResult{...}` (`block.go:197`), insert the `establishesNewBFC` BFC-attach block.

The edited function, with existing code elided as marked:

```go
func (e *Engine) layoutBlock(ctx context.Context, b *cssbox.Box, cbWidth, originX, marginTopEdgeY, bandOriginY float64, fc *floatContext) blockResult {
	if b.Kind == cssbox.BoxReplaced {
		// A replaced box has no float children; it ignores fc/bandOriginY.
		return e.layoutBlockReplaced(ctx, b, cbWidth, originX, marginTopEdgeY)
	}

	// >>> EXISTING block.go:111-122 VERBATIM: ed := usedEdges(...); contentW :=
	//     resolveContentWidth(...); borderW := ...; contentX := originX+ed.mL+ed.bL+ed.pL <<<

	// CHANGE #2 + #3: the interior's band origin is this box's content-box top in the
	// BFC-root frame. (marginTopEdgeY is passed as 0 by the stacker in the provisional
	// layout; the float context is queried in the BFC-root frame via bandOriginY, and
	// the stacker's later shift repositions in-flow fragments — floats are placed
	// directly in the BFC frame, so they don't need that shift.)
	childBandOrigin := bandOriginY + ed.mT + ed.bT + ed.pT
	in := e.layoutInterior(ctx, b, contentW, contentX, childBandOrigin, fc) // was: (ctx, b, contentW, contentX)

	// >>> EXISTING block.go:130-196 VERBATIM: newBFC/marginTop/leadingGap/contentH/
	//     heightAuto/marginBottom/borderH/borderX/borderY/contentTopY resolution, the
	//     shiftFragments/shiftLines calls, and the frag := &Fragment{...} construction
	//     incl. the four Border edges. <<<

	// CHANGE #4: a box that establishes its own BFC owns its floats' paint layer.
	if establishesNewBFC(b) {
		frag.IsBFC = true
		frag.Floats = in.bfcFloats
	}
	return blockResult{frag: frag, marginTop: marginTop, marginBottom: marginBottom}
}
```

`layoutInterior` — add `bandOriginY` and `fc`; a new BFC builds a fresh context spanning its content box and resets the band origin to 0 (its own frame); otherwise children share the parent's context and band frame. It surfaces a new BFC's placed floats via `interior.bfcFloats`:

```go
func (e *Engine) layoutInterior(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext) interior {
	// A box that establishes a new BFC (inline-block today) isolates floats: its
	// interior gets a fresh context spanning its own content box, and its own band
	// frame (origin 0). Otherwise children share the parent's context and frame, so a
	// float placed by a child is visible to its siblings and the band Y stays in the
	// ancestor BFC-root frame.
	childFC, childBand := fc, bandOriginY
	if establishesNewBFC(b) {
		childFC = &floatContext{cbLeft: contentX, cbRight: contentX + contentW}
		childBand = 0
	}

	var in interior
	switch b.Formatting {
	case cssbox.InlineFC:
		lines, h, atomics := e.layoutInline(ctx, b, contentW, 0, contentX, childBand, childFC)
		in = interior{lines: lines, children: atomics, contentHeight: h}
	case cssbox.BlockFC:
		in = e.layoutBlockChildren(ctx, b, contentW, contentX, childBand, childFC)
	default:
		e.logf("css layout: %v not yet implemented; falling back to block normal flow", b.Formatting)
		in = e.layoutBlockChildren(ctx, b, contentW, contentX, childBand, childFC)
	}

	// A new BFC's floats are self-contained: surface them so layoutBlock attaches them
	// to b's own fragment (the float paint layer for b's BFC).
	if establishesNewBFC(b) {
		in.bfcFloats = childFC.floats2frags()
	}
	return in
}
```

Add the surfacing field to `interior` (block.go):

```go
	bfcFloats []*Fragment // floats placed in this box's OWN BFC (set only when b establishes one)
```

`layoutBlockChildren` — add `fc *floatContext`; handle floated / cleared / in-flow children:

```go
func (e *Engine) layoutBlockChildren(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext) interior {
	var (
		out        []*Fragment
		prevBottom float64
		prevBorder float64
		leading    float64
		trailing   float64
		first      = true
		cursorY    float64 // local content-top-0 Y of the in-flow cursor
	)
	for _, child := range b.Children {
		if err := ctx.Err(); err != nil {
			break
		}
		if !child.Kind.IsBlockLevel() {
			e.logf("css layout: unexpected inline-level child in block formatting context; skipping")
			continue
		}

		if child.Float != cssbox.FloatNone {
			// Place the float in the BFC-root frame at the current in-flow band:
			// bandOriginY (this content box's top in that frame) + cursorY (local).
			e.placeFloat(ctx, child, contentW, contentX, bandOriginY+cursorY, fc)
			continue // a float does not advance the in-flow cursor or collapse margins
		}

		// clear: lower the cursor below the matching floats. clearY is in the BFC-root
		// frame; convert to local by subtracting bandOriginY.
		startY := cursorY
		if child.Style.Clear != "" && child.Style.Clear != "none" {
			if cy := fc.clearY(child.Style.Clear, bandOriginY+cursorY) - bandOriginY; cy > startY {
				startY = cy
			}
		}

		res := e.layoutBlock(ctx, child, contentW, contentX, 0, bandOriginY+startY, fc)

		var borderTop float64
		if first {
			borderTop = startY
			leading = res.marginTop
			first = false
		} else {
			borderTop = prevBorder + collapseMargins(prevBottom, res.marginTop)
			if startY > borderTop {
				borderTop = startY // clearance pushed it down past the collapsed margin
			}
		}
		shiftFragment(res.frag, borderTop-res.marginTop)
		out = append(out, res.frag)

		prevBorder = res.frag.Y + res.frag.H
		prevBottom = res.marginBottom
		trailing = res.marginBottom
		cursorY = prevBorder
	}
	return interior{children: out, contentHeight: prevBorder, leadingMargin: leading, trailingMargin: trailing}
}
```

Note the second `res := e.layoutBlock(...)` call passes `bandOriginY+startY` as the child's `bandOriginY` so a nested in-flow block knows its position in the BFC-root frame (its IFC queries floats there in Task 7). The provisional `marginTopEdgeY` argument stays 0 (the stacker positions via the shift, as today).

**Coordinate-frame model (the load-bearing detail — call it out in a code comment):** the float context is queried in ONE frame per BFC — the **BFC-root-local frame**, whose Y origin is the BFC root's content-box top (page Y 0 for the page root). Every `place`/`leftEdge`/`rightEdge`/`clearY` call passes `bandOriginY + <local Y>`, where `bandOriginY` is the current box's content-top in that frame and `<local Y>` is the local content-top-0 cursor/pen. In-flow fragments are still built in their own local frame and shifted into place by the existing per-child shift; **float fragments are built directly in the BFC-root frame** (they attach to the BFC root's `Floats`, not to a shifted child) — see `placeFloat`. A nested BFC resets `bandOriginY` to 0 and uses its own context (`layoutInterior`).

- [ ] **Step 4: Implement `placeFloat`**

Add to `block.go`:

```go
// placeFloat lays out a floated child and places it in the float context at
// placeY (the in-flow band's Y in the BFC-root-local frame). The child is laid out
// to learn its size, expanded to its margin box, placed via fc.place, and its
// fragment translated to the placed margin box's border-box origin — directly in the
// BFC-root frame, because a float attaches to the BFC root's Floats (the float paint
// layer), not to a shifted in-flow child. The fragment is marked IsFloat and recorded
// on the just-appended floatBox so the BFC owner (layoutTree / layoutInterior) can
// collect it via floats2frags.
func (e *Engine) placeFloat(ctx context.Context, child *cssbox.Box, cbWidth, contentX, placeY float64, fc *floatContext) {
	// Lay the float out (provisional origin) to learn its border-box size. It is laid
	// in its own fresh context if it establishes a BFC (it is block-level; a float
	// itself establishes a BFC for its contents) — layoutBlock handles that via
	// establishesNewBFC. Pass placeY as bandOriginY so any nested float math is framed
	// consistently; the provisional marginTopEdgeY is 0.
	res := e.layoutBlock(ctx, child, cbWidth, contentX, 0, placeY, fc)
	if res.frag == nil {
		return
	}
	ed := usedEdges(child, cbWidth)
	marginW := ed.mL + res.frag.W + ed.mR
	marginH := res.marginTop + res.frag.H + res.marginBottom

	fb := fc.place(child.Float, marginW, marginH, placeY)

	// fb.x/fb.y is the float's MARGIN-box top-left in the BFC-root frame. The border
	// box sits inside it by the left/top margins. Translate the provisional fragment
	// there (X and Y both move; a float's position is absolute in the BFC frame).
	dx := (fb.x + ed.mL) - res.frag.X
	dy := (fb.y + res.marginTop) - res.frag.Y
	translateFragment(res.frag, dx, dy)
	res.frag.IsFloat = true

	// Record the fragment on the just-appended floatBox (fc.place appended it last).
	fc.floats[len(fc.floats)-1].frag = res.frag
}
```

**Important:** `fc.place` appends the floatBox; `placeFloat` then sets `.frag` on the just-appended entry (`fc.floats[len-1]`). Keep these two statements adjacent so the index is correct.

**Note on the float's own contents:** because `establishesNewBFC` (Task: block.go) currently returns true only for `DisplayInlineBlock`, a floated `<div>` does NOT yet establish a new BFC by that predicate, so its interior would share `fc`. A float must establish a BFC for its own contents (CSS 9.7). Extend `establishesNewBFC` to also return true when `b.Float != cssbox.FloatNone`:

```go
func establishesNewBFC(b *cssbox.Box) bool {
	return b.Display == cssbox.DisplayInlineBlock || b.Float != cssbox.FloatNone
}
```

This means `layoutBlock` builds the float with a fresh context (its interior floats don't leak to the outer BFC) and marks the float fragment `IsBFC` — which is correct: the float paints its own contents atomically. Its own `Floats` (rare: a float inside a float) attach to the float's fragment.

- [ ] **Step 5: Update the `layoutInline` signature (pass-through only)**

In `inline.go`, add the `bandOriginY float64` and `fc *floatContext` parameters to `layoutInline` but **do not** use them yet (Task 7 wires the narrowing). This keeps Task 6 compiling:

```go
func (e *Engine) layoutInline(ctx context.Context, b *cssbox.Box, contentW, contentTopY, contentX, bandOriginY float64, fc *floatContext) (lines []LineFragment, height float64, atomics []*Fragment) {
	_, _ = bandOriginY, fc // float-aware narrowing is wired in Task 7
	// ... body unchanged ...
}
```

Update its call site in `layoutInterior` (already shown in Step 3) to pass `childBand` and `childFC`.

- [ ] **Step 6: Update all other call sites of the changed signatures**

`layoutBlock` is also called from `inline.go` `gatherInlineRuns` (the inline-block case: `e.layoutBlock(ctx, child, contentW, 0, 0)`). An inline-block establishes its own BFC, so `layoutBlock` will build it a fresh context internally (via the extended `establishesNewBFC`); pass `bandOriginY = 0` and a fresh `fc` for the atom (it is laid out in its own frame, then positioned on the line by the IFC, which carries its whole subtree including any internal floats via `translateFragment`):

```go
		case child.Display == cssbox.DisplayInlineBlock:
			res := e.layoutBlock(ctx, child, contentW, 0, 0, 0, &floatContext{cbLeft: 0, cbRight: contentW})
```

For the replaced case, `layoutBlockReplaced` is unchanged (a replaced box has no float children); `layoutBlock`'s replaced branch returns before touching `fc`/`bandOriginY`, so it accepts and ignores them.

Search for every changed call and add the arguments:

Run: `grep -rn 'e.layoutBlock(\|e.layoutInterior(\|e.layoutInline(\|e.layoutBlockChildren(' pkg/layout/css/*.go`
Update each to match the final signatures (the `bandOriginY` + `fc` parameters). The complete set of call sites: `layoutTree` (root), `layoutBlock`→`layoutInterior`, `layoutInterior`→`layoutBlockChildren`/`layoutInline`, `layoutBlockChildren`→`layoutBlock` (in-flow child) + `placeFloat`→`layoutBlock` (float), `gatherInlineRuns`→`layoutBlock` (inline-block atom).

- [ ] **Step 7: Run the new tests**

Run: `go test ./pkg/layout/css -run 'TestFloatPlacedOutOfFlow|TestClearDropsBelowFloat' -v`
Expected: PASS.

- [ ] **Step 8: Full package + vet + gofmt + race**

Run: `go test ./pkg/layout/css && go vet ./pkg/layout/css && gofmt -l pkg/layout/css && go test -race ./pkg/layout/css`
Expected: PASS; vet clean; gofmt silent; race clean. Existing fragment-geometry tests still pass (no-float pages place identically; the root is now `IsBFC` but with empty `Floats` the paint order for pure in-flow content is unchanged per Task 5's no-float test).

- [ ] **Step 9: Commit**

```bash
git add pkg/layout/css/block.go pkg/layout/css/floats.go pkg/layout/css/inline.go pkg/layout/css/floats_layout_test.go
git commit -m "css: place floats out of flow + clear in the block stacker"
```

---

## Task 7: Narrow the inline formatting context around floats

**Files:**
- Modify: `pkg/layout/css/inline.go` (`layoutInline`)
- Test: `pkg/layout/css/floats_layout_test.go` (append)

Now make in-flow text wrap around floats: drive line-breaking with `BreakNext` at the per-line float-narrowed width, and offset each line's `StartX` to the narrowed left edge. When no float overlaps a line's band, the result is identical to today.

- [ ] **Step 1: Write the failing test**

Append to `pkg/layout/css/floats_layout_test.go`:

```go
// TestTextWrapsBesideFloat: with a left float occupying the top-left, the first
// line of following text starts at the float's right edge; a line below the float's
// bottom starts back at the content-box left.
func TestTextWrapsBesideFloat(t *testing.T) {
	eng := New(nil, nil, nil)

	// A 60pt-wide, 40pt-tall left float.
	floated := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 60, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 40, Unit: gcss.UnitPx}})
	floated.Float = cssbox.FloatLeft

	// A sibling block of text with enough words to wrap several lines at width 200.
	textStyle := gcss.ComputedStyle{Display: "block", FontFamily: "serif", FontSizePt: 12,
		LineHeight: gcss.Length{Value: 16, Unit: gcss.UnitPx}, Color: color.RGBA{0, 0, 0, 255}}
	para := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.InlineFC, Style: textStyle,
		Children: []*cssbox.Box{{Kind: cssbox.BoxText, Display: cssbox.DisplayInline, Style: textStyle,
			Text: "Many words here that should wrap across several lines beside and below the floated box on the left side."}}}

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated, para)
	frag := eng.layoutTree(context.Background(), root, 200)

	// Find the paragraph fragment (the only in-flow child) and inspect its first line.
	if len(frag.Children) != 1 {
		t.Fatalf("want 1 in-flow child, got %d", len(frag.Children))
	}
	pf := frag.Children[0]
	if len(pf.Lines) < 2 {
		t.Fatalf("paragraph has %d lines, want >= 2 to test wrap", len(pf.Lines))
	}
	// First line's first glyph X should be at/after the float's right edge (60),
	// since the float occupies the top-left band.
	firstX := pf.Lines[0].Glyphs[0].X
	if firstX < 60-1e-6 {
		t.Errorf("first line starts at X=%v, want >= 60 (right of the float)", firstX)
	}
	// The last line should be below the float (baseline > 40), and start back near
	// the content-box left (X < 60). Find a line whose baseline is clearly below 40.
	var belowX float64 = -1
	for i := range pf.Lines {
		if pf.Lines[i].BaselineY > 40+8 { // a line whose top is past the float bottom
			belowX = pf.Lines[i].Glyphs[0].X
			break
		}
	}
	if belowX < 0 {
		t.Fatalf("no line below the float bottom; lines: %d", len(pf.Lines))
	}
	if belowX > 60 {
		t.Errorf("line below the float starts at X=%v, want < 60 (back at the left)", belowX)
	}
}
```

(The exact X depends on font metrics from the bundled face; the assertions use inequalities, not exact positions, so they are robust to metric jitter.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run 'TestTextWrapsBesideFloat' -v`
Expected: FAIL — text currently ignores floats (first line starts at X≈0).

- [ ] **Step 3: Rewrite the line-positioning loop in `layoutInline`**

Replace the body of `layoutInline` that calls `inline.Break` + positions lines (the current Steps 2–5 in inline.go) with a per-line driver. The shape step is unchanged; the break+position becomes incremental:

```go
func (e *Engine) layoutInline(ctx context.Context, b *cssbox.Box, contentW, contentTopY, contentX, bandOriginY float64, fc *floatContext) (lines []LineFragment, height float64, atomics []*Fragment) {
	// 1. Gather inline-level descendants into styled runs (atomics laid out eagerly).
	var runs []inline.Run
	e.gatherInlineRuns(ctx, b, contentW, &runs, &atomics)
	if len(runs) == 0 {
		return nil, 0, nil
	}

	// 2. Shape once (width-independent).
	glyphs := inline.Shape(e.faces, runs, e.logf)
	align := effectiveTextAlign(b)

	// 3. Break + position one line at a time against the float-narrowed band. Float
	//    queries use the BFC-root frame (bandOriginY + the local pen offset); emitted
	//    glyph/line Y stays in the local content-top-0 frame (the block stacker shifts
	//    the whole interior into page space later). Glyph X is absolute page-space
	//    already (availLeft and contentX are absolute), so X is never shifted. When no
	//    float overlaps a line's band, leftEdge/rightEdge return [contentX,
	//    contentX+contentW] and this reproduces a single fixed-width Break.
	penY := contentTopY
	rest := glyphs
	lineHGuess := e.lineHeightGuess(b) // representative band height (see Task 7 Step 4)
	for len(rest) > 0 {
		bandY := bandOriginY + (penY - contentTopY) // this line's Y in the BFC-root frame
		availLeft := fc.leftEdge(bandY, lineHGuess)
		availRight := fc.rightEdge(bandY, lineHGuess)
		avail := availRight - availLeft
		if avail < 1 {
			// Fully blocked band (opposite-side floats meet). Drop below the shallowest
			// float and retry rather than emitting a zero-width line.
			next := fc.nextDropY(bandY, lineHGuess)
			if next > bandY {
				penY += next - bandY
				continue
			}
			// Non-advancing band (shouldn't happen): fall back to full width to avoid a
			// spin.
			availLeft, avail = contentX, contentW
		}

		var lineGlyphs []inline.Glyph
		lineGlyphs, rest = inline.BreakNext(rest, avail)
		line := inline.MakeLine(lineGlyphs)

		lh := e.effectiveLineHeight(b, line)
		baselineY := penY + ascentOfLine(line)

		spaceCount := inline.CountSpaces(line.Glyphs)
		isLast := len(rest) == 0
		// availLeft is the absolute page-space left for this line; avail is its width.
		p := inline.Place(align, availLeft, avail, line.WidthPt, spaceCount, isLast)

		var emitted []GlyphFragment
		x := p.StartX
		for gi := range line.Glyphs {
			g := &line.Glyphs[gi]
			switch {
			case g.Atomic != nil:
				if frag, ok := g.Atomic.Ref.(*Fragment); ok && frag != nil {
					translateFragment(frag, (x+g.Atomic.MarginLeftPt)-frag.X, (baselineY-g.Atomic.BaselinePt)-frag.Y)
				}
			case g.Outline != nil:
				emitted = append(emitted, GlyphFragment{
					Outline: g.Outline, X: x, SizePt: g.SizePt,
					Color: color.RGBA{R: g.Color.R, G: g.Color.G, B: g.Color.B, A: g.Color.A},
				})
			}
			x += g.Advance
			if g.Space {
				x += p.ExtraPerSpace
			}
		}

		lines = append(lines, LineFragment{BaselineY: baselineY, Glyphs: emitted})
		penY += lh
	}

	return lines, penY - contentTopY, atomics
}
```

Note `lineHGuess` is hoisted out of the loop (it depends only on `b`, not the line). The zero-width-band branch advances `penY` by the gap to the next float bottom (in local terms) and retries, so a paragraph squeezed between opposite floats drops to where it fits.

Note: `availLeft` replaces `contentX` as the per-line origin passed to `inline.Place`, and `avail` replaces `contentW` as the available width — so alignment is computed within the narrowed band.

- [ ] **Step 4: Add the `lineHeightGuess` helper**

The float band query needs an estimated line height before the line is measured. Add to `inline.go`:

```go
// lineHeightGuess estimates a line's height for the float band query, before the
// line's glyphs are measured. It uses the box's resolved line-height when fixed
// (px/pt/em); for "normal"/auto it falls back to the font size times the default
// multiplier — a representative value (the float bands are coarse, so an exact
// per-line height is unnecessary, and the actual line height is recomputed once the
// line is built). For an anonymous block it reads the inherited line-height/font-size
// from a text leaf, mirroring effectiveLineHeight.
func (e *Engine) lineHeightGuess(b *cssbox.Box) float64 {
	lh := b.Style.LineHeight
	fs := b.Style.FontSizePt
	if isAnonymous(b) {
		if l, f, ok := firstInlineLineHeight(b); ok {
			lh, fs = l, f
		}
	}
	switch lh.Unit {
	case gcss.UnitPx, gcss.UnitPt:
		return lh.Value
	case gcss.UnitEm:
		return lh.Value * fs
	default:
		if fs <= 0 {
			fs = 12 // defensive: a sane default if the font size is unset
		}
		return fs * cssDefaultLineMult
	}
}
```

- [ ] **Step 5: Run the wrap test**

Run: `go test ./pkg/layout/css -run 'TestTextWrapsBesideFloat' -v`
Expected: PASS.

- [ ] **Step 6: Guard the no-float path (regression check)**

Run: `go test ./pkg/layout/css`
Expected: PASS — the existing inline/fragment-geometry tests (no floats) still pass, because with an empty float context `leftEdge`/`rightEdge` return `[contentX, contentX+contentW]` and `BreakNext` reproduces the old `Break` lines. If any existing test now fails, investigate (it likely means a band-frame or alignment-origin mismatch — `availLeft` must equal `contentX` when no float intrudes).

- [ ] **Step 7: vet + gofmt + race**

Run: `go vet ./pkg/layout/css && gofmt -l pkg/layout/css && go test -race ./pkg/layout/css`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add pkg/layout/css/inline.go pkg/layout/css/floats_layout_test.go
git commit -m "css: narrow inline lines around floats (per-line BreakNext at the float band)"
```

---

## Task 8: Float-aware in-flow block content (narrowing) + degradation tests

**Files:**
- Modify: `pkg/layout/css/block.go` (verify in-flow blocks narrow; add degradation handling)
- Test: `pkg/layout/css/floats_layout_test.go` (append)

By design (spec §"How floats thread through block layout", point 2) an in-flow block's **border box** stays full-width and only its **content** narrows — which already happens, because an in-flow block shares the parent's `fc` and its IFC/child-stacker re-query it at their own Y. This task **verifies** that holds and adds the documented degradation tests (overflow-wide float; floated inline blockified end-to-end).

- [ ] **Step 1: Write the failing/verifying tests**

Append to `pkg/layout/css/floats_layout_test.go`:

```go
// TestInFlowBlockContentNarrowsAfterPrecedingBlock: with a preceding in-flow block
// pushing the cursor down (so bandOriginY != 0 for the following content), a tall
// left float still narrows the FOLLOWING in-flow block's inline text, while the
// block's own border box spans the full width. This exercises the BFC-root-frame
// band query (Task 6/7) — the float's Y and the text's penY are in DIFFERENT local
// frames, reconciled only via bandOriginY.
func TestInFlowBlockContentNarrowsAfterPrecedingBlock(t *testing.T) {
	eng := New(nil, nil, nil)

	// A preceding spacer block: 25pt tall, pushes the cursor to y=25.
	spacer := blockBox(gcss.ComputedStyle{Display: "block",
		Height: gcss.Length{Value: 25, Unit: gcss.UnitPx}})

	// A tall left float that starts at the cursor (y=25) and runs 80pt down.
	floated := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 60, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 80, Unit: gcss.UnitPx}})
	floated.Float = cssbox.FloatLeft

	textStyle := gcss.ComputedStyle{Display: "block", FontFamily: "serif", FontSizePt: 12,
		LineHeight: gcss.Length{Value: 16, Unit: gcss.UnitPx}, Color: color.RGBA{0, 0, 0, 255},
		BackgroundColor: color.RGBA{200, 220, 240, 255}}
	para := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.InlineFC, Style: textStyle,
		Children: []*cssbox.Box{{Kind: cssbox.BoxText, Display: cssbox.DisplayInline, Style: textStyle,
			Text: "Text that wraps beside the tall float on the left edge of the column here."}}}

	// Order: spacer, float (placed at the cursor below the spacer), paragraph.
	root := blockBox(gcss.ComputedStyle{Display: "block"}, spacer, floated, para)
	frag := eng.layoutTree(context.Background(), root, 200)

	// In-flow children are spacer (Y=0) and paragraph (Y=25, beside/below the float).
	if len(frag.Children) != 2 {
		t.Fatalf("want 2 in-flow children (spacer, para), got %d", len(frag.Children))
	}
	pf := frag.Children[1]
	// Border box spans full width even though text is inset.
	if pf.X > 1e-6 || pf.W < 200-1e-6 {
		t.Errorf("in-flow block border-box X=%v W=%v, want X=0 W=200 (border box ignores the float)", pf.X, pf.W)
	}
	// The float occupies y in [25, 105]. The paragraph starts at y=25, so its first
	// line's text must be inset past the float's right edge (60).
	if len(pf.Lines) == 0 || pf.Lines[0].Glyphs[0].X < 60-1e-6 {
		t.Errorf("in-flow block text not inset past the float (first glyph X=%v, want >= 60)",
			func() float64 { if len(pf.Lines) > 0 && len(pf.Lines[0].Glyphs) > 0 { return pf.Lines[0].Glyphs[0].X }; return -1 }())
	}
}

// TestOverflowWideFloatDegrades: a float wider than the container is placed at the
// edge (allowed to overflow), does not loop, and a following clear:both block drops
// below it.
func TestOverflowWideFloatDegrades(t *testing.T) {
	eng := New(nil, nil, nil)

	wide := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 400, Unit: gcss.UnitPx}, // wider than the 200 viewport
		Height: gcss.Length{Value: 30, Unit: gcss.UnitPx}})
	wide.Float = cssbox.FloatLeft

	after := blockBox(gcss.ComputedStyle{Display: "block", Clear: "both",
		Height: gcss.Length{Value: 20, Unit: gcss.UnitPx}})

	root := blockBox(gcss.ComputedStyle{Display: "block"}, wide, after)
	frag := eng.layoutTree(context.Background(), root, 200) // must return (no infinite loop)

	if len(frag.Floats) != 1 {
		t.Fatalf("want 1 float, got %d", len(frag.Floats))
	}
	if frag.Floats[0].X != 0 {
		t.Errorf("overflow-wide float X=%v, want 0 (placed at the edge)", frag.Floats[0].X)
	}
	if frag.Children[0].Y < 30-1e-6 {
		t.Errorf("clear:both block Y=%v, want >= 30 (below the wide float)", frag.Children[0].Y)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail (or pass)**

Run: `go test ./pkg/layout/css -run 'TestInFlowBlockContentNarrowsAfterPrecedingBlock|TestOverflowWideFloatDegrades' -v`
Expected: `TestOverflowWideFloatDegrades` PASS (Task 6 already handles placement + clear). `TestInFlowBlockContentNarrowsAfterPrecedingBlock` is the case that proves the `bandOriginY` frame logic from Tasks 6–7 is correct end to end; if Tasks 6–7 were implemented exactly as written it PASSES — if it FAILS, the band frame is off (see Step 3).

- [ ] **Step 3: Verify the band frame + make `Floats` ride the BFC shift (the nested-BFC detail)**

The narrowing-after-a-preceding-block test exercises the one subtlety Tasks 6–7 introduced: the float is placed in the BFC-root frame at `bandOriginY+cursorY`, and the following paragraph's IFC queries the float at `bandOriginY+penY` in that same frame. If `TestInFlowBlockContentNarrowsAfterPrecedingBlock` passes, the frame is correct — no code change needed here.

One genuinely-new piece remains: a **nested BFC** (an inline-block, or a float, which now establishes a BFC) builds its floats in its own BFC-local frame and attaches them to its own fragment's `Floats`; that fragment is later moved as a unit (a float is moved by `translateFragment` in `placeFloat`/the IFC; a nested in-flow BFC block by `shiftFragment` in the stacker). For its `Floats` to move with it, **`shiftFragment` and `translateFragment` must also recurse into `f.Floats`** (they already recurse `f.Children`, but a float fragment is in `Floats`, not `Children`). Add to both functions:

In `shiftFragment` (block.go), after the `for _, c := range f.Children` loop:

```go
	for _, fl := range f.Floats {
		shiftFragment(fl, dy)
	}
```

In `translateFragment` (inline.go), after its `for _, c := range f.Children` loop:

```go
	for _, fl := range f.Floats {
		translateFragment(fl, dx, dy)
	}
```

For the **root** BFC the floats are already in page space (bandOriginY chain starts at 0 and the root fragment is at origin), so they receive no net shift; this change only matters when a BFC-establishing subtree that *contains* floats is itself repositioned (a floated or inline-block box with its own floated descendants). It is correct and harmless for the common case.

- [ ] **Step 4: Add a nested-BFC float-shift test**

Append (guards the Step 3 change):

```go
// TestNestedBFCFloatRidesShift: a float INSIDE an inline-block (a nested BFC) is
// positioned relative to the inline-block, and moves with it when the inline-block is
// placed on its line. Asserts the inner float's fragment ends up within the
// inline-block's box (not at the page origin).
func TestNestedBFCFloatRidesShift(t *testing.T) {
	eng := New(nil, nil, nil)

	// An inline-block at a non-zero position containing a left float.
	innerFloat := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 20, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 20, Unit: gcss.UnitPx}})
	innerFloat.Float = cssbox.FloatLeft

	ibStyle := gcss.ComputedStyle{Display: "inline-block",
		Width:  gcss.Length{Value: 100, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 40, Unit: gcss.UnitPx}}
	ib := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayInlineBlock, Formatting: cssbox.BlockFC,
		Style: ibStyle, Children: []*cssbox.Box{innerFloat}}

	// Put the inline-block after some leading text so it is not at x=0.
	lead := gcss.ComputedStyle{Display: "block", FontFamily: "serif", FontSizePt: 12, Color: color.RGBA{0, 0, 0, 255}}
	para := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.InlineFC, Style: lead,
		Children: []*cssbox.Box{
			{Kind: cssbox.BoxText, Display: cssbox.DisplayInline, Style: lead, Text: "Hi "},
			ib,
		}}
	root := blockBox(gcss.ComputedStyle{Display: "block"}, para)
	frag := eng.layoutTree(context.Background(), root, 300)

	// Find the inline-block atom (an IsBFC child somewhere in the tree) and confirm it
	// carries its inner float in Floats, positioned within its own box bounds.
	var ibFrag *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f.IsBFC && len(f.Floats) > 0 && f.W == 100 {
			ibFrag = f
		}
		for _, c := range f.Children {
			walk(c)
		}
		for _, fl := range f.Floats {
			walk(fl)
		}
	}
	walk(frag)
	if ibFrag == nil {
		t.Fatal("inline-block fragment with an inner float not found")
	}
	inner := ibFrag.Floats[0]
	// The inner float must sit within the inline-block's border box (it rode the shift).
	if inner.X < ibFrag.X-1e-6 || inner.X+inner.W > ibFrag.X+ibFrag.W+1e-6 {
		t.Errorf("inner float X=%v..%v not within inline-block X=%v..%v (did not ride the shift)",
			inner.X, inner.X+inner.W, ibFrag.X, ibFrag.X+ibFrag.W)
	}
}
```

- [ ] **Step 5: Re-run the narrowing + nested-BFC tests**

Run: `go test ./pkg/layout/css -run 'TestInFlowBlockContentNarrowsAfterPrecedingBlock|TestTextWrapsBesideFloat|TestFloatPlacedOutOfFlow|TestClearDropsBelowFloat|TestOverflowWideFloatDegrades|TestNestedBFCFloatRidesShift' -v`
Expected: all PASS.

- [ ] **Step 6: Add the blockify-end-to-end degradation test**

Append:

```go
// TestFloatedInlineBlockifies: a <span style="float:left"> goes through box
// generation as a block-level float and lays out (placed out of flow), proving the
// CSS 9.7 blockify path reaches layout. Uses the public OpenHTMLBytes path.
func TestFloatedInlineBlockifies(t *testing.T) {
	// This exercises build.go + layout together; if a doctaculous-level helper is
	// heavier than needed, assert via box generation directly:
	// (kept in pkg/layout/css to use generate()/the engine without the full backend)
	// — see build_test.go TestBlockifyFloatedInline for the box-gen half; here assert
	// the engine places it.
	// A minimal floated inline-level box with explicit size:
	sp := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.InlineFC,
		Float: cssbox.FloatLeft,
		Style: gcss.ComputedStyle{Display: "block", Float: "left",
			Width: gcss.Length{Value: 30, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 30, Unit: gcss.UnitPx}}}
	root := blockBox(gcss.ComputedStyle{Display: "block"}, sp)
	frag := New(nil, nil, nil).layoutTree(context.Background(), root, 100)
	if len(frag.Floats) != 1 {
		t.Fatalf("blockified floated inline not placed as a float (Floats=%d)", len(frag.Floats))
	}
}
```

- [ ] **Step 7: Full suite + vet + gofmt + race**

Run: `go test ./pkg/layout/css && go vet ./pkg/layout/css && gofmt -l pkg/layout/css && go test -race ./pkg/layout/css`
Expected: all clean.

- [ ] **Step 8: Commit**

```bash
git add pkg/layout/css/block.go pkg/layout/css/inline.go pkg/layout/css/floats_layout_test.go
git commit -m "css: float-aware in-flow block content via a BFC-relative band frame"
```

---

## Task 9: Goldens + WPT reftests (end-to-end) + eyeball

**Files:**
- Modify: `pkg/doctaculous/html_golden_test.go` (two new `htmlGoldens`)
- Modify: `pkg/doctaculous/wpt_reftest_test.go` (two new `wptReftests`)
- Create: `pkg/doctaculous/testdata/wpt/css21-normal-flow/float-left.html` + `float-left-ref.html`
- Create: `pkg/doctaculous/testdata/wpt/css21-normal-flow/float-row.html` + `float-row-ref.html`
- Create (generated): `pkg/doctaculous/testdata/golden/html-float-figure.png`, `html-float-row.png`

- [ ] **Step 1: Add the golden fixtures**

In `pkg/doctaculous/html_golden_test.go`, append two entries to `htmlGoldens`:

```go
	{
		// A left-floated figure box with paragraph text wrapping beside it, then a
		// cleared block below. Eyeball: text hugs the float's right edge for the first
		// lines, returns to full width below the float, and the cleared block sits
		// under the float.
		name:       "float-figure",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .fig { float: left; width: 70px; height: 60px; background: #cc3333; margin: 0 8px 4px 0; }
  .cap { clear: left; background: #eeeeee; }
</style></head><body>
  <div class="fig"></div>
  <p>This paragraph wraps its text beside the floated red figure box and then continues below it once the lines drop past the figure's bottom edge.</p>
  <div class="cap">A cleared caption sits below the float.</div>
</body></html>`,
	},
	{
		// Three left-floated swatches that stack on one row then wrap the third to a
		// new row when the row is full. Eyeball: two on the first row, one below.
		name:       "float-row",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .sw { float: left; width: 80px; height: 40px; margin: 4px; }
  .a { background: #cc3333; }
  .b { background: #33aa33; }
  .c { background: #3355cc; }
</style></head><body>
  <div class="sw a"></div><div class="sw b"></div><div class="sw c"></div>
</body></html>`,
	},
```

- [ ] **Step 2: Generate and eyeball the goldens**

Run: `go test ./pkg/doctaculous -run TestHTMLGolden -update`
Then open `pkg/doctaculous/testdata/golden/html-float-figure.png` and `html-float-row.png` and **visually confirm**:
- `html-float-figure`: red box top-left; the paragraph's first lines start to the right of it; lower lines span full width; the gray cleared caption is entirely below the red box.
- `html-float-row`: two 80px swatches (red, green) on the top row; the blue one wrapped to a second row at the left.

If either looks wrong, the layout is wrong — fix the engine, do not re-`-update` to bless a bad image.

- [ ] **Step 3: Add the WPT reftest pairs**

Create `pkg/doctaculous/testdata/wpt/css21-normal-flow/float-left.html`:

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .f { float: left; width: 50px; height: 50px; background: #3366cc; }
  .t { background: #ffffff; }
</style></head><body>
  <div class="f"></div>
  <div class="t" style="height: 50px;"></div>
</body></html>
```

Create `float-left-ref.html` — the reference renders the same pixels WITHOUT floats: a 50×50 blue box at the top-left, and the white block's box. Because an in-flow block's border box spans full width under the float, the reference is a blue 50×50 box absolutely matched by positioning the equivalent in normal flow. Use the simplest equivalent that produces identical pixels:

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  /* Reference: a 50x50 blue box on the left; the white area is the background. The
     float's only painted pixels are the blue square at (0,0,50,50). */
  .f { width: 50px; height: 50px; background: #3366cc; }
</style></head><body>
  <div class="f"></div>
</body></html>
```

(The test page's white `.t` block paints no visible pixels over white, so the only ink is the blue square — which the reference reproduces in normal flow. This is a deliberately minimal equivalence: it asserts the float paints at (0,0) with its size, independent of the following block. If the float painting regresses or shifts, the pixels differ.)

Create `float-row.html` (two swatches fit, third wraps) and `float-row-ref.html` (the three swatches at hand-computed absolute-equivalent positions using inline-block or fixed offsets that reproduce 2-on-top-1-below). Keep the reference float-free:

`float-row.html`:
```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .sw { float: left; width: 80px; height: 30px; }
  .a { background: #cc3333; } .b { background: #33aa33; } .c { background: #3355cc; }
</style></head><body>
  <div class="sw a"></div><div class="sw b"></div><div class="sw c"></div>
</body></html>
```

`float-row-ref.html` (reproduce the same pixels with inline-block, which already works and places two-then-wrap by width):
```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .sw { display: inline-block; width: 80px; height: 30px; vertical-align: top; }
  .a { background: #cc3333; } .b { background: #33aa33; } .c { background: #3355cc; }
</style></head><body><div><span class="sw a"></span><span class="sw b"></span><span class="sw c"></span></div></body></html>
```

**Caveat to verify:** inline-block wrapping and float wrapping put the third box on a second row only if the row width (200) holds exactly two 80px boxes plus their layout — confirm the test and reference actually match pixel-for-pixel when you run the suite (Step 5). If inline-block spacing (whitespace between inline-blocks) shifts pixels, remove inter-tag whitespace (as shown) or switch the reference to explicit positioning. If they cannot be made identical, drop `float-row` from the reftests and rely on its golden + the `floats_layout_test.go` geometry assertions (note this in the PR).

- [ ] **Step 4: Register the reftests**

In `pkg/doctaculous/wpt_reftest_test.go`, append to `wptReftests`:

```go
	{"float-left", 200, "a left float paints at the container's top-left, independent of the following in-flow block", nil},
	{"float-row", 200, "two 80px left floats fit one row; a third wraps below (matches inline-block wrapping)", nil},
```

- [ ] **Step 5: Run the doctaculous suite**

Run: `go test ./pkg/doctaculous -run 'TestHTMLGolden|TestWPTReftests' -v`
Expected: PASS. If `float-row` reftest fails on pixel diff, apply the Step 3 caveat (remove whitespace / switch reference / drop it with a note).

- [ ] **Step 6: Full repo test + race + vet + gofmt**

Run (sandbox disabled):
```bash
go test ./... && go test -race ./... && go vet ./... && gofmt -l pkg/css pkg/layout/css pkg/layout/inline pkg/doctaculous
```
Expected: all PASS; gofmt silent. **In particular `TestDOCXGolden` must stay green** (the shared inline core is additive; DOCX still calls `Break`). If a DOCX or other golden changed, investigate — the only intended pixel changes are the two new HTML float goldens.

- [ ] **Step 7: Lint the changed packages**

Run: `golangci-lint run ./pkg/css/... ./pkg/layout/css/... ./pkg/layout/inline/... ./pkg/doctaculous/...`
Expected: clean (decline any modernize hints if they appear — keep explicit clamps).

- [ ] **Step 8: Commit**

```bash
git add pkg/doctaculous/html_golden_test.go pkg/doctaculous/wpt_reftest_test.go pkg/doctaculous/testdata/
git commit -m "doctaculous: float goldens + WPT reftests (figure wrap, multi-float row)"
```

---

## Task 10: Update CLAUDE.md status (when the PR is ready)

**Files:**
- Modify: `CLAUDE.md` (Done section; §6 TODO)

- [ ] **Step 1: Move floats from TODO to Done**

In `CLAUDE.md`:
- Add a **Done** bullet under the HTML rendering entries describing floats + clear (mirroring the prior slices' entries: what packages, what's covered, the spec path `docs/superpowers/specs/2026-06-24-html-floats-design.md`).
- In the §6 TODO "floats + positioning" line, strike floats and leave **positioning** + **overflow clipping** (the remaining slices 5b/5c).

Draft for the Done bullet:

```markdown
- **HTML rendering — floats + clear** (`pkg/layout/css/floats.go`, extended `block.go`/`inline.go`/
  `fragment.go`, `pkg/layout/inline` `BreakNext`, `pkg/css` `float`/`clear`; covered by float-context
  geometry unit tests, fragment-geometry assertions, the `html-float-*` goldens, and `float-*` WPT
  reftests): `float:left/right` takes a box out of flow to the containing-block edge (positioned by its
  margin box); in-flow line boxes and block content narrow around floats via a per-BFC `floatContext`
  (`leftEdge`/`rightEdge`/`place`/`clearY`) the block stacker and IFC query per vertical band; multiple
  floats stack and wrap to a new row; `clear:left/right/both` drops a box below matching floats. Floats
  paint in their own CSS layer (Appendix E: block decorations → floats → inline content) via a
  phase-split `AppendItems`. The shared inline core stays float-agnostic (one additive `BreakNext`
  primitive; DOCX unchanged). Degrades gracefully: an overflow-wide float overflows the edge, `float:auto`
  width approximates the resolved width, floats do not cross a nested BFC boundary (revisited with
  overflow). See `docs/superpowers/specs/2026-06-24-html-floats-design.md`.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: record HTML floats + clear slice in CLAUDE.md status"
```

---

## Self-review checklist (run before handing off to execution)

- [ ] **Spec coverage:** float/clear parse (Task 1) ✓; box-gen wiring + blockify (Task 2) ✓; float context geometry incl. stacking/wrap/overflow/clear (Task 3) ✓; `BreakNext` (Task 4) ✓; paint layer / Appendix E order (Task 5) ✓; out-of-flow placement + clear in the stacker (Task 6) ✓; IFC narrowing (Task 7) ✓; float-aware in-flow block + degradation (Task 8) ✓; goldens + reftests (Task 9) ✓; status update (Task 10) ✓.
- [ ] **Type consistency:** `floatContext`/`floatBox` fields and methods (`leftEdge`/`rightEdge`/`place`/`clearY`/`nextDropY`/`floats2frags`) match across Tasks 3, 6, 8; `Fragment.IsFloat`/`IsBFC`/`Floats` match across Tasks 5, 6, 8; `BreakNext(glyphs, width) (line, rest)` matches across Tasks 4, 7; the **final** threaded signatures (`…, bandOriginY float64, fc *floatContext`) are stated once in Task 6 Step 3 and used verbatim in Tasks 6, 7, 8 — no signature changes after Task 6.
- [ ] **Coordinate frame:** the float context is queried in one frame per BFC (BFC-root-local; `bandOriginY + local Y`); float fragments are built directly in that frame and attach to the BFC root's `Floats`; in-flow fragments keep their local frame and ride the stacker shift. The one new code piece in Task 8 is making `shiftFragment`/`translateFragment` recurse into `Floats` so a repositioned nested-BFC subtree carries its floats. This is the load-bearing detail the spec reviewer verifies with adversarial geometry tests.
- [ ] **No placeholders:** every code step shows complete code; no "TBD"/"handle edge cases".

---

## Execution Handoff

This plan is executed via **subagent-driven development** (REQUIRED SUB-SKILL: superpowers:subagent-driven-development): a fresh subagent per task, then a two-stage review (spec-review + code-quality-review) per task, fix, and proceed — per the handover's prescribed flow. The controller eyeballs every golden PNG (Task 9) and keeps `git status` clean after each review subagent.
