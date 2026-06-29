# HTML Positioning (relative / absolute / fixed) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement CSS `position: relative | absolute | fixed` in the HTML layout engine — `relative` offsets a box at paint time (flow unchanged); `absolute`/`fixed` take a box out of flow and position it against its containing block (nearest positioned ancestor, or the page); positioned boxes paint in their own layer after in-flow content (a minimal stacking pass). `z-index` and the offsets are parsed now; full z-index sorting is deferred (5b-2).

**Architecture:** All positioning logic lives in `pkg/layout/css`; the shared inline core (`pkg/layout/inline`) is **untouched** (no new inline primitive — unlike floats). A new `positioning.go` holds the pure geometry (`relativeOffset`, `absRect`). `block.go` threads a `posCB` (containing-block **owner**: ancestor `*Fragment` + `*cssbox.Box`, or a page sentinel) and a `posCtx` (the abs-pos collection); abs/fixed boxes are collected in the in-flow pass and resolved in a **second pass** once containing-block geometry is final. `fragment.go` generalizes 5a's phase-split `AppendItems` into a stacking pass (decorations → floats → in-flow content → **positioned layer**), skipping `IsPositioned` children in the in-flow phases; a `relative` box's offset is applied as a **translate over its flattened item range** (NOT via `translateFragment`/`shiftFragment`, which don't recurse the positioned layer).

**Tech Stack:** Go (stdlib only — no new dependency); `pkg/css` (cascade), `pkg/layout/cssbox` (box tree, already has `Position`), `pkg/layout/css` (layout engine), `pkg/layout/paint` (unchanged), `pkg/doctaculous` (golden + WPT reftest harness).

**Spec:** `docs/superpowers/specs/2026-06-24-html-positioning-design.md`. Read its "How positioning threads through layout", "Two-pass abs-pos", "The stacking pass", and "Relative offset & descendant CB" sections — those carry the load-bearing decisions the spec review nailed down.

**Process reminders (from the handover, hold throughout):**
- You are on branch `feat/html-positioning`. Do **NOT** checkout/stash/switch branches; do **NOT** commit (the controller commits). Do not `git stash`.
- Run `go`/`golangci-lint`/`gofmt` with the **sandbox disabled** (sandbox blocks the Go build cache + TLS). Lint **specific packages**, not the repo root; run `gofmt -l` on changed packages separately (golangci-lint here does not gofmt). **Decline modernize hints** (`max()`/`min()`/`slices.*`/range-over-int) — the codebase uses explicit `if x < y { x = y }` clamps and indexed loops intentionally. The repo uses **no** `//nolint`. golangci-lint **does** flag `if !(a && b)` (QF1001 — write `if a>=b || b>=c`) and an **unused unexported field** (so every new field must be read in this PR).
- Editor diagnostics lag; trust `go build`/`go test`. After any review subagent, `find . -name 'zz_*' -delete` and confirm `git status` is clean.
- **The zero-value `Length` trap:** a `cssbox.ComputedStyle`/`Box` literal that omits `Width`/`MaxWidth` reads as explicit `0` (`{0,UnitPx}`), NOT `auto`/`none`. Test fixtures built as raw structs (not via `blockBox`) must set `Width`/`Height`/`MaxWidth`/`MaxHeight` to `UnitAuto`. The new offset `Top`/`Right`/`Bottom`/`Left` fields default to `UnitAuto` (= CSS `auto`) — a raw-struct fixture that wants an offset sets it explicitly; one that wants no offset must leave it `UnitAuto`, NOT zero `{0,UnitPx}` (which would mean `top:0`, a real offset).
- **Test the FLAG COMBINATIONS, not each flag alone** (5a's worst miss): `position:relative; float:left`, `position:absolute; float:left`, `position:relative` on an inline-block, a z-indexed abs-pos inside a relative parent. **Eyeball every golden PNG** (the controller, via Read — the implementer can't see it).
- **Eyeball every new/changed golden PNG in the PR.** Confirm no pre-existing golden changed (`git status --short pkg/doctaculous/testdata/ pkg/render/raster/testdata/` shows only new files). The stacking-pass change is high-risk for silently reordering existing pages — non-positioned pages MUST stay byte-identical.

---

## File Structure

**New files:**
- `pkg/layout/css/positioning.go` — `relativeOffset`, `absRect`, and the per-axis over-constrained solve helper. One responsibility: positioning geometry (the analogue of `floats.go`).
- `pkg/layout/css/positioning_test.go` — unit tests for the geometry in isolation.
- `pkg/doctaculous/testdata/wpt/css21-normal-flow/abs-pos.html` + `abs-pos-ref.html` — a reftest pair (abs-pos box == statically-placed box at the same coords).
- `pkg/doctaculous/testdata/wpt/css21-normal-flow/relative-offset.html` + `relative-offset-ref.html` — a reftest pair (relative offset == static box at the shifted position).

**Modified files:**
- `pkg/css/cascade.go` — add `Position`/`Top`/`Right`/`Bottom`/`Left`/`ZIndex`/`ZIndexAuto` to `ComputedStyle`, `initialStyle`, `applyDeclaration` (NOT `inheritFrom`).
- `pkg/css/cascade_test.go` — position/offset/z-index parse + cascade + not-inherited tests.
- `pkg/layout/css/build.go` — wire `positionOf`; force `Float=none` under abs/fixed; generalize `applyFloatBlockify`→`applyBlockify` (float OR abs/fixed); a `relative`-inline-only-text no-op note.
- `pkg/layout/css/build_test.go` — positionOf wiring, blockify, and `position`×`float` precedence tests.
- `pkg/layout/css/fragment.go` — `Fragment.IsPositioned`/`RelOffsetX`/`RelOffsetY`/`IsStackingContext`/`Positioned`; generalize `AppendItems` into the stacking pass; `translateItems` helper; skip `IsPositioned` in the in-flow walkers.
- `pkg/layout/css/fragment_test.go` — stacking-pass paint-order + byte-identical + relative-offset-translate tests.
- `pkg/layout/css/block.go` — thread `posCB`/`posCtx` through `layoutTree`/`layoutBlock`/`layoutInterior`/`layoutBlockChildren`; collect abs/fixed; handle `relative` (record offset + positioned-layer attach); the two-pass resolve in `Layout`/`layoutTree`; `placeFloat` stamps a relative offset for a positioned float; `establishesStackingContext`; CB-owner plumbing.
- `pkg/layout/css/*_test.go` — fragment-geometry + flag-combination + paint-coordinate tests (new file `positioning_layout_test.go`).
- `pkg/doctaculous/html_golden_test.go` — two new `htmlGoldens` entries (`position-relative`, `position-absolute`).
- `pkg/doctaculous/wpt_reftest_test.go` — two new `wptReftests` entries.
- `CLAUDE.md` — move positioning out of the §6 TODO into Done (final task, when the PR lands).

---

## Task 1: Add `position`, offsets, and `z-index` to the CSS cascade

**Files:**
- Modify: `pkg/css/cascade.go` (struct ~line 75 after `Clear`; `initialStyle` ~line 243; `applyDeclaration` ~line 357 after the `clear` case)
- Test: `pkg/css/cascade_test.go`

`position`/`top`/`right`/`bottom`/`left`/`z-index` are **not** CSS-inherited, so they are added to `ComputedStyle`, `initialStyle`, and `applyDeclaration` — but **NOT** to `inheritFrom`.

- [ ] **Step 1: Write the failing test**

Add to `pkg/css/cascade_test.go`:

```go
// TestInitialPosition: the cascade defaults position to "static", the offsets to
// auto, and z-index to auto.
func TestInitialPosition(t *testing.T) {
	cs := initialStyle()
	if cs.Position != "static" {
		t.Errorf("initial position = %q, want static", cs.Position)
	}
	for name, l := range map[string]Length{"top": cs.Top, "right": cs.Right, "bottom": cs.Bottom, "left": cs.Left} {
		if l.Unit != UnitAuto {
			t.Errorf("initial %s = %+v, want UnitAuto", name, l)
		}
	}
	if !cs.ZIndexAuto {
		t.Errorf("initial z-index not auto (ZIndexAuto=%v)", cs.ZIndexAuto)
	}
}

// TestApplyPosition: each valid position keyword is accepted; an invalid one is
// dropped (leaving the prior value).
func TestApplyPosition(t *testing.T) {
	for _, kw := range []string{"static", "relative", "absolute", "fixed"} {
		cs := initialStyle()
		applyDeclaration(&cs, Declaration{Property: "position", Value: kw})
		if cs.Position != kw {
			t.Errorf("position %q not applied, got %q", kw, cs.Position)
		}
	}
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "position", Value: "sticky"}) // unsupported here
	if cs.Position != "static" {
		t.Errorf("position after unsupported keyword = %q, want static preserved", cs.Position)
	}
}

// TestApplyOffsets: top/right/bottom/left parse as lengths; auto is accepted.
func TestApplyOffsets(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "top", Value: "10px"})
	applyDeclaration(&cs, Declaration{Property: "left", Value: "20px"})
	applyDeclaration(&cs, Declaration{Property: "right", Value: "auto"})
	if cs.Top.Unit != UnitPx || cs.Top.Value != 10 {
		t.Errorf("top = %+v, want 10px", cs.Top)
	}
	if cs.Left.Unit != UnitPx || cs.Left.Value != 20 {
		t.Errorf("left = %+v, want 20px", cs.Left)
	}
	if cs.Right.Unit != UnitAuto {
		t.Errorf("right = %+v, want UnitAuto", cs.Right)
	}
}

// TestApplyZIndex: an integer z-index is parsed (ZIndexAuto=false); "auto" stays
// auto; a non-integer is dropped.
func TestApplyZIndex(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "z-index", Value: "5"})
	if cs.ZIndexAuto || cs.ZIndex != 5 {
		t.Errorf("z-index 5: got ZIndex=%d ZIndexAuto=%v, want 5/false", cs.ZIndex, cs.ZIndexAuto)
	}
	applyDeclaration(&cs, Declaration{Property: "z-index", Value: "-2"})
	if cs.ZIndexAuto || cs.ZIndex != -2 {
		t.Errorf("z-index -2: got ZIndex=%d ZIndexAuto=%v, want -2/false", cs.ZIndex, cs.ZIndexAuto)
	}
	cs.ZIndex, cs.ZIndexAuto = 7, false
	applyDeclaration(&cs, Declaration{Property: "z-index", Value: "auto"})
	if !cs.ZIndexAuto {
		t.Errorf("z-index auto: ZIndexAuto=%v, want true", cs.ZIndexAuto)
	}
	cs2 := initialStyle()
	applyDeclaration(&cs2, Declaration{Property: "z-index", Value: "1.5"}) // non-integer dropped
	if !cs2.ZIndexAuto {
		t.Errorf("z-index 1.5 should be dropped, ZIndexAuto=%v", cs2.ZIndexAuto)
	}
}

// TestPositionNotInherited: position/offsets/z-index are not inherited.
func TestPositionNotInherited(t *testing.T) {
	parent := initialStyle()
	parent.Position = "relative"
	parent.Top = Length{Value: 10, Unit: UnitPx}
	parent.ZIndex, parent.ZIndexAuto = 3, false
	child := inheritFrom(parent)
	if child.Position != "static" {
		t.Errorf("position inherited: got %q, want static", child.Position)
	}
	if child.Top.Unit != UnitAuto {
		t.Errorf("top inherited: got %+v, want UnitAuto", child.Top)
	}
	if !child.ZIndexAuto {
		t.Errorf("z-index inherited: ZIndexAuto=%v, want true (auto)", child.ZIndexAuto)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/css -run 'TestInitialPosition|TestApplyPosition|TestApplyOffsets|TestApplyZIndex|TestPositionNotInherited' -v`
Expected: FAIL — `cs.Position`/`cs.Top`/`cs.ZIndex` undefined (compile error).

- [ ] **Step 3: Add the struct fields**

In `pkg/css/cascade.go`, in `ComputedStyle` after the `Clear` field (~line 78), add:

```go

	// Position is the CSS position value: "static" (default) | "relative" |
	// "absolute" | "fixed". Not inherited. The box generator maps it to
	// cssbox.PositionKind.
	Position string
	// Top/Right/Bottom/Left are the positioning offset properties (CSS 9.3.2),
	// UnitAuto = "auto" (the initial value). Not inherited. Meaningful only on a
	// positioned box (relative: paint offset; absolute/fixed: placement against
	// the containing block).
	Top, Right, Bottom, Left Length
	// ZIndex is the stack level of a positioned box; ZIndexAuto models the "auto"
	// initial value (ZIndex is read only when ZIndexAuto is false). Not inherited.
	// Parsed now; the minimal stacking pass does not yet sort on it (positioned
	// boxes paint in document order) — full z-index ordering is a later slice.
	ZIndex     int
	ZIndexAuto bool
```

- [ ] **Step 4: Set initial values**

In `initialStyle()` (~line 243), add to the returned literal:

```go
		Position:   "static", // CSS initial position
		Top:        Length{Unit: UnitAuto},
		Right:      Length{Unit: UnitAuto},
		Bottom:     Length{Unit: UnitAuto},
		Left:       Length{Unit: UnitAuto},
		ZIndexAuto: true, // CSS initial z-index is auto
```

(`inheritFrom` is NOT touched — these are not inherited. Add a one-line comment in `inheritFrom` near the existing "keep this set in sync" note is unnecessary; the doc comment on the fields says "Not inherited".)

- [ ] **Step 5: Parse the declarations**

In `applyDeclaration` (~line 357, after the `clear` case), add:

```go
	case "position":
		switch d.Value {
		case "static", "relative", "absolute", "fixed":
			cs.Position = d.Value
		}
	case "top":
		setLength(&cs.Top, d.Value)
	case "right":
		setLength(&cs.Right, d.Value)
	case "bottom":
		setLength(&cs.Bottom, d.Value)
	case "left":
		setLength(&cs.Left, d.Value)
	case "z-index":
		applyZIndex(cs, d.Value)
```

`setLength` already handles `auto` → `UnitAuto` (via `parseLength`). Confirm: read `parseLength` to verify `auto` yields `Length{Unit: UnitAuto}` (it does — the offset fields' initial is UnitAuto and `right:auto` must round-trip). Add the `z-index` helper at the bottom of the file near `setMaxLength`:

```go
// applyZIndex parses a z-index value: "auto" sets ZIndexAuto; an integer sets
// ZIndex (ZIndexAuto=false). A non-integer value is dropped, leaving the prior
// value. (Parsed now for the cascade; the minimal stacking pass does not yet sort
// on it.)
func applyZIndex(cs *ComputedStyle, val string) {
	if val == "auto" {
		cs.ZIndexAuto = true
		return
	}
	n, ok := parseInt(val)
	if !ok {
		return
	}
	cs.ZIndex, cs.ZIndexAuto = n, false
}

// parseInt parses an optionally-signed base-10 integer, returning ok=false for any
// non-integer (including empty, a float, or trailing junk). Used for z-index.
func parseInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	neg := false
	i := 0
	if s[0] == '+' || s[0] == '-' {
		neg = s[0] == '-'
		i = 1
		if i == len(s) {
			return 0, false
		}
	}
	n := 0
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}
```

(`strings` is already imported in cascade.go.)

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./pkg/css -run 'TestInitialPosition|TestApplyPosition|TestApplyOffsets|TestApplyZIndex|TestPositionNotInherited' -v` → PASS.

- [ ] **Step 7: Full package + lint/gofmt**

Run: `go test ./pkg/css`, `gofmt -l pkg/css`, `golangci-lint run ./pkg/css`. All clean. (No modernize hints introduced — `parseInt` uses an indexed loop on purpose.)

---

## Task 2: Wire `positionOf` and `position`×`float` precedence in box generation

**Files:**
- Modify: `pkg/layout/css/build.go` (`generate` ~line 81-85; `floatOf`/`applyFloatBlockify`/`positionOf` ~lines 142-181)
- Test: `pkg/layout/css/build_test.go`

Implements: `positionOf` maps the keyword; `position:absolute|fixed` forces `Float=none` (CSS 9.7); blockify generalizes to "float OR abs/fixed".

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/build_test.go` (read the file first for its existing helpers/imports — there is a `gcss` alias and a tiny-HTML build harness):

```go
// TestPositionOf: positionOf maps each keyword.
func TestPositionOf(t *testing.T) {
	cases := map[string]cssbox.PositionKind{
		"static": cssbox.PosStatic, "relative": cssbox.PosRelative,
		"absolute": cssbox.PosAbsolute, "fixed": cssbox.PosFixed, "": cssbox.PosStatic,
	}
	for kw, want := range cases {
		cs := gcss.ComputedStyle{Position: kw}
		if got := positionOf(cs); got != want {
			t.Errorf("positionOf(%q) = %v, want %v", kw, got, want)
		}
	}
}

// TestAbsPositionForcesFloatNone: an absolutely/fixed-positioned element computes
// float to none (CSS 9.7), so it is NOT placed as a float.
func TestAbsPositionForcesFloatNone(t *testing.T) {
	root := buildHTML(t, `<div style="position:absolute; float:left; width:10px; height:10px"></div>`)
	abs := firstElementBox(t, root) // helper: descend to the styled div's box
	if abs.Position != cssbox.PosAbsolute {
		t.Fatalf("position = %v, want PosAbsolute", abs.Position)
	}
	if abs.Float != cssbox.FloatNone {
		t.Errorf("abs-pos box Float = %v, want FloatNone (CSS 9.7)", abs.Float)
	}
}

// TestRelativeFloatKeepsBoth: a relative+float box stays a float AND positioned.
func TestRelativeFloatKeepsBoth(t *testing.T) {
	root := buildHTML(t, `<div style="position:relative; float:left; width:10px; height:10px"></div>`)
	b := firstElementBox(t, root)
	if b.Float != cssbox.FloatLeft {
		t.Errorf("Float = %v, want FloatLeft (relative does not override float)", b.Float)
	}
	if b.Position != cssbox.PosRelative {
		t.Errorf("Position = %v, want PosRelative", b.Position)
	}
}

// TestAbsInlineBlockifies: an inline element that is absolutely positioned
// blockifies (CSS 9.7), like a float.
func TestAbsInlineBlockifies(t *testing.T) {
	root := buildHTML(t, `<span style="position:absolute; width:10px; height:10px"></span>`)
	b := firstElementBox(t, root)
	if !b.Kind.IsBlockLevel() {
		t.Errorf("abs-pos inline did not blockify: Kind=%v", b.Kind)
	}
}
```

If `buildHTML`/`firstElementBox` helpers don't already exist in `build_test.go`, add minimal ones (parse HTML bytes via the same path `Build` uses, then walk to the first non-anonymous element box). Read the existing test file to reuse whatever harness is there — the floats slice added similar wiring tests, so a pattern likely exists.

- [ ] **Step 2: Run to verify it fails** — `go test ./pkg/layout/css -run 'TestPositionOf|TestAbsPositionForcesFloatNone|TestRelativeFloatKeepsBoth|TestAbsInlineBlockifies' -v` → FAIL (positionOf still a stub returning PosStatic; float not forced).

- [ ] **Step 3: Implement `positionOf`**

Replace the `positionOf` stub (~line 179-181):

```go
// positionOf maps a computed style to a PositionKind.
func positionOf(cs gcss.ComputedStyle) cssbox.PositionKind {
	switch cs.Position {
	case "relative":
		return cssbox.PosRelative
	case "absolute":
		return cssbox.PosAbsolute
	case "fixed":
		return cssbox.PosFixed
	default:
		return cssbox.PosStatic
	}
}
```

- [ ] **Step 4: Force float:none under abs/fixed; generalize blockify**

In `generate` (~line 81-85), after `b.Position = positionOf(cs)` and before `applyFloatBlockify`:

```go
	b.Float = floatOf(cs)
	b.Position = positionOf(cs)
	// CSS 9.7: an absolutely/fixed-positioned element computes float to none, so
	// it is taken out of flow as positioned, not placed as a float.
	if b.Position == cssbox.PosAbsolute || b.Position == cssbox.PosFixed {
		b.Float = cssbox.FloatNone
	}
	applyBlockify(b, cs) // CSS 9.7: a float OR an abs/fixed box blockifies an inline-level box
```

Rename `applyFloatBlockify` → `applyBlockify` and change its trigger to "float OR abs/fixed position". Update its doc comment and the guard:

```go
// applyBlockify promotes an inline-level box to block-level when it is floated or
// absolutely/fixed positioned (CSS 2.1 §9.7: both compute display to a block-level
// value). [... keep the rest of the existing doc about preserving Formatting and the
// BoxReplaced guard ...]
func applyBlockify(b *cssbox.Box, cs gcss.ComputedStyle) {
	floated := floatOf(cs) != cssbox.FloatNone
	posKind := positionOf(cs)
	absPos := posKind == cssbox.PosAbsolute || posKind == cssbox.PosFixed
	if !floated && !absPos {
		return
	}
	if b.Kind == cssbox.BoxReplaced {
		return // replaced stays replaced; replaced sizing handles block-level
	}
	if b.Kind.IsInlineLevel() {
		b.Kind, b.Display = cssbox.BoxBlock, cssbox.DisplayBlock
	}
}
```

Note: because abs/fixed forces `b.Float=FloatNone` just above, `applyBlockify` re-reads `cs.Float` (still "left") — but that is fine: blockify only cares whether the element *should* be block-level, and an abs/fixed element should regardless of the now-cleared float. (Reading `cs` not `b` keeps it a pure function of the style. The float-clear on `b` is for the *layout* path, not blockify.)

- [ ] **Step 5: Run the tests** — the 4 new tests PASS. Also run `go test ./pkg/layout/css -run 'TestFloat'` to confirm the existing float blockify tests still pass under the renamed `applyBlockify` (grep for `applyFloatBlockify` usages first and update any test that referenced the old name).

- [ ] **Step 6: Lint/gofmt** — `gofmt -l pkg/layout/css`, `golangci-lint run ./pkg/layout/css`. Clean.

---

## Task 3: The positioning geometry (`positioning.go`)

**Files:**
- New: `pkg/layout/css/positioning.go`
- Test: `pkg/layout/css/positioning_test.go`

Implements the pure geometry from the spec's "The positioning geometry" section. Read spec lines ~129-180 for the exact formulas (this task transcribes them).

- [ ] **Step 1: Write the failing tests** (`pkg/layout/css/positioning_test.go`)

```go
package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// relBox builds a box with the given position offsets (auto where omitted) and a
// font size for em resolution. Offsets default to UnitAuto (CSS auto), NOT zero —
// the zero-value Length trap.
func relBox(top, right, bottom, left gcss.Length, fontSizePt float64) *cssbox.Box {
	auto := gcss.Length{Unit: gcss.UnitAuto}
	st := gcss.ComputedStyle{Position: "relative", Top: auto, Right: auto, Bottom: auto, Left: auto, FontSizePt: fontSizePt}
	if top.Unit != 0 || top.Value != 0 { st.Top = top }
	if right.Unit != 0 || right.Value != 0 { st.Right = right }
	if bottom.Unit != 0 || bottom.Value != 0 { st.Bottom = bottom }
	if left.Unit != 0 || left.Value != 0 { st.Left = left }
	return &cssbox.Box{Kind: cssbox.BoxBlock, Style: st}
}

func px(v float64) gcss.Length { return gcss.Length{Value: v, Unit: gcss.UnitPx} }

// TestRelativeOffsetTopLeft: top/left produce a positive (down, right) shift.
func TestRelativeOffsetTopLeft(t *testing.T) {
	b := relBox(px(10), gcss.Length{}, gcss.Length{}, px(20), 16)
	dx, dy := relativeOffset(b, 200, 100)
	if dx != 20 || dy != 10 {
		t.Errorf("relativeOffset = (%v,%v), want (20,10)", dx, dy)
	}
}

// TestRelativeOffsetBottomRight: with top/left auto, bottom/right produce a
// negative (up, left) shift.
func TestRelativeOffsetBottomRight(t *testing.T) {
	b := relBox(gcss.Length{}, px(5), px(8), gcss.Length{}, 16)
	dx, dy := relativeOffset(b, 200, 100)
	if dx != -5 || dy != -8 {
		t.Errorf("relativeOffset = (%v,%v), want (-5,-8)", dx, dy)
	}
}

// TestRelativeOffsetTopWins: top wins over bottom, left wins over right (CSS 9.4.3).
func TestRelativeOffsetTopWins(t *testing.T) {
	b := relBox(px(10), px(5), px(8), px(20), 16)
	dx, dy := relativeOffset(b, 200, 100)
	if dx != 20 || dy != 10 {
		t.Errorf("over-constrained relativeOffset = (%v,%v), want (20,10) (left/top win)", dx, dy)
	}
}

// TestRelativeOffsetPercent: top% resolves against cbH, left% against cbW.
func TestRelativeOffsetPercent(t *testing.T) {
	pct := func(v float64) gcss.Length { return gcss.Length{Value: v, Unit: gcss.UnitPercent} }
	b := relBox(pct(10), gcss.Length{}, gcss.Length{}, pct(50), 16)
	dx, dy := relativeOffset(b, 200, 100) // left 50% of 200 = 100; top 10% of 100 = 10
	if dx != 100 || dy != 10 {
		t.Errorf("percent relativeOffset = (%v,%v), want (100,10)", dx, dy)
	}
}

// TestAbsRectLeftTop: a box at left:10 top:20 with a fixed size lands at the CB
// origin + offsets.
func TestAbsRectLeftTop(t *testing.T) {
	cb := rect{x: 100, y: 50, w: 400, h: 300}
	st := gcss.ComputedStyle{Position: "absolute",
		Top: px(20), Left: px(10), Right: gcss.Length{Unit: gcss.UnitAuto}, Bottom: gcss.Length{Unit: gcss.UnitAuto},
		Width: px(40), Height: px(30), MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto}, FontSizePt: 16}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Style: st}
	ed := usedEdges(b, cb.w)
	bb, contentW := absRect(b, ed, cb)
	if bb.x != 110 || bb.y != 70 { // 100+10, 50+20
		t.Errorf("absRect left/top = (%v,%v), want (110,70)", bb.x, bb.y)
	}
	if contentW != 40 {
		t.Errorf("absRect contentW = %v, want 40", contentW)
	}
}

// TestAbsRectRightBottom: a box at right:10 bottom:20 with a fixed size lands
// against the far edges.
func TestAbsRectRightBottom(t *testing.T) {
	cb := rect{x: 100, y: 50, w: 400, h: 300}
	st := gcss.ComputedStyle{Position: "absolute",
		Top: gcss.Length{Unit: gcss.UnitAuto}, Left: gcss.Length{Unit: gcss.UnitAuto}, Right: px(10), Bottom: px(20),
		Width: px(40), Height: px(30), MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto}, FontSizePt: 16}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Style: st}
	ed := usedEdges(b, cb.w)
	bb, _ := absRect(b, ed, cb)
	// border-box left = cb.x + cb.w - right - mR - borderBoxW = 100+400-10-0-40 = 450
	// border-box top  = cb.y + cb.h - bottom - mB - borderBoxH = 50+300-20-0-30 = 300
	if bb.x != 450 || bb.y != 300 {
		t.Errorf("absRect right/bottom = (%v,%v), want (450,300)", bb.x, bb.y)
	}
}

// TestAbsRectLeftRightAutoWidth: left+right specified with width:auto derives the
// width from the offsets.
func TestAbsRectLeftRightAutoWidth(t *testing.T) {
	cb := rect{x: 0, y: 0, w: 400, h: 300}
	st := gcss.ComputedStyle{Position: "absolute",
		Top: px(0), Left: px(30), Right: px(50), Bottom: gcss.Length{Unit: gcss.UnitAuto},
		Width: gcss.Length{Unit: gcss.UnitAuto}, Height: px(30),
		MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto}, FontSizePt: 16}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Style: st}
	ed := usedEdges(b, cb.w)
	bb, contentW := absRect(b, ed, cb)
	// content width = cb.w - left - right - insetsX - mL - mR = 400-30-50-0-0-0 = 320
	if contentW != 320 || bb.x != 30 {
		t.Errorf("absRect L+R auto width: contentW=%v x=%v, want 320/30", contentW, bb.x)
	}
}

// TestAbsRectAllAutoStatic: all-auto offsets place the box at the CB top-left
// (static approximation).
func TestAbsRectAllAutoStatic(t *testing.T) {
	cb := rect{x: 100, y: 50, w: 400, h: 300}
	auto := gcss.Length{Unit: gcss.UnitAuto}
	st := gcss.ComputedStyle{Position: "absolute",
		Top: auto, Left: auto, Right: auto, Bottom: auto,
		Width: px(40), Height: px(30), MaxWidth: auto, MaxHeight: auto, FontSizePt: 16}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Style: st}
	ed := usedEdges(b, cb.w)
	bb, _ := absRect(b, ed, cb)
	if bb.x != 100 || bb.y != 50 {
		t.Errorf("absRect all-auto = (%v,%v), want CB top-left (100,50)", bb.x, bb.y)
	}
}
```

NOTE on the `relBox` helper's `if top.Unit != 0 ...` guard: it lets a test pass `gcss.Length{}` to mean "leave auto". This is deliberate — see the zero-value trap reminder. Simplify if the implementer prefers explicit per-field construction.

- [ ] **Step 2: Run to verify it fails** — compile error (`relativeOffset`, `absRect`, `rect` undefined).

- [ ] **Step 3: Implement `positioning.go`**

```go
package css

import (
	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// rect is an axis-aligned rectangle in page space (points, Y-down): (x,y) is the
// top-left, w/h the extent. Used for the abs-pos containing block and the resolved
// border box.
type rect struct{ x, y, w, h float64 }

// relativeOffset resolves a relatively-positioned box's paint-time offset (dx,dy)
// from its top/right/bottom/left against the containing-block dimensions cbW
// (left/right percentages) and cbH (top/bottom percentages). CSS 9.4.3
// over-constrained resolution: left wins over right, top wins over bottom; an auto
// offset contributes 0. The offset shifts only the painted position — flow is
// unchanged (the engine applies it at flatten time).
func relativeOffset(b *cssbox.Box, cbW, cbH float64) (dx, dy float64) {
	fs := b.Style.FontSizePt
	dx = axisRelative(b.Style.Left, b.Style.Right, fs, cbW)
	dy = axisRelative(b.Style.Top, b.Style.Bottom, fs, cbH)
	return dx, dy
}

// axisRelative resolves one axis of a relative offset: the start offset (left/top)
// wins; if it is auto, the negated end offset (right/bottom) applies; if both are
// auto, 0.
func axisRelative(start, end gcss.Length, fontSizePt, pctBasis float64) float64 {
	if v, isAuto := resolveLen(start, fontSizePt, pctBasis); !isAuto {
		return v
	}
	if v, isAuto := resolveLen(end, fontSizePt, pctBasis); !isAuto {
		return -v
	}
	return 0
}

// absRect resolves an absolutely/fixed-positioned box's used border-box rectangle
// and used content width against the containing-block content rect cb, given the
// box's resolved edges ed (auto margins already 0). Offsets are measured from cb to
// the box's MARGIN edge, so margin terms are explicit. Implements the supported
// subset of CSS 10.3.7/10.6.4 (see the spec's "The positioning geometry"):
// over-constrained cases resolve start-edge-wins; all-auto offsets approximate the
// static position (cb top-left); width:auto with both left+right derives the width.
func absRect(b *cssbox.Box, ed edges, cb rect) (border rect, contentW float64) {
	fs := b.Style.FontSizePt
	insetsX := ed.bL + ed.bR + ed.pL + ed.pR
	insetsY := ed.bT + ed.bB + ed.pT + ed.pB
	borderBox := b.Style.BoxSizing == "border-box"

	// Resolve specified width/height (content-box terms) if not auto.
	wVal, wAuto := resolveLen(b.Style.Width, fs, cb.w)
	if !wAuto && borderBox {
		wVal -= insetsX
	}
	hVal, hAuto := resolveLen(b.Style.Height, fs, cb.h)
	if !hAuto && borderBox {
		hVal -= insetsY
	}
	// Fallback width for the no-constraint case: containing-block fill for a
	// non-replaced box (replaced boxes are handled by the replaced path before
	// reaching here in pass 2; if a replaced box does reach here its intrinsic
	// width is already in b.Style.Width or resolved upstream).
	fillW := cb.w - ed.mL - ed.mR - insetsX
	if fillW < 0 {
		fillW = 0
	}

	bx, cw := axisAbs(cb.x, cb.w, b.Style.Left, b.Style.Right, fs, ed.mL, ed.mR, insetsX, wVal, wAuto, fillW)
	by, _ := axisAbs(cb.y, cb.h, b.Style.Top, b.Style.Bottom, fs, ed.mT, ed.mB, insetsY, hVal, hAuto, hVal)

	bw := cw + insetsX
	bh := hVal + insetsY
	if hAuto {
		// Height is content-derived: the caller lays out the interior and sets the
		// real height; absRect returns a provisional 0-content height here. The
		// two-pass caller recomputes by/bh once the interior height is known when
		// only `top` (not bottom) is specified. (When both top+bottom are specified
		// with height:auto, the derived height below wins.)
		if !isAuto2(b.Style.Top, fs) && !isAuto2(b.Style.Bottom, fs) {
			// both specified: derive height from offsets.
			topV, _ := resolveLen(b.Style.Top, fs, cb.h)
			botV, _ := resolveLen(b.Style.Bottom, fs, cb.h)
			bh = cb.h - topV - botV - ed.mT - ed.mB
			if bh < 0 {
				bh = 0
			}
		} else {
			bh = insetsY // content height filled in by the caller
		}
	}
	return rect{x: bx, y: by, w: bw, h: bh}, cw
}

// axisAbs resolves one axis (horizontal with cb.x/cb.w, or vertical with cb.y/cb.h)
// of an abs-pos box: returns the border-box start coordinate and the used content
// SIZE on that axis. startOff/endOff are the offset Lengths (left/right or
// top/bottom); mStart/mEnd the margins; insets the border+padding on the axis;
// sizeVal/sizeAuto the box's own content size; fillSize the fallback content size
// when the size is auto and at most one offset is set.
func axisAbs(cbStart, cbExtent float64, startOff, endOff gcss.Length, fontSizePt, mStart, mEnd, insets, sizeVal float64, sizeAuto bool, fillSize float64) (borderStart, contentSize float64) {
	sVal, sAuto := resolveLen(startOff, fontSizePt, cbExtent)
	eVal, eAuto := resolveLen(endOff, fontSizePt, cbExtent)

	switch {
	case !sAuto && !eAuto && sizeAuto:
		// start+end specified, size auto: derive content size from the offsets.
		cs := cbExtent - sVal - eVal - mStart - mEnd - insets
		if cs < 0 {
			cs = 0
		}
		return cbStart + sVal + mStart, cs
	case !sAuto:
		// start specified (size definite, or end ignored): place against the start edge.
		size := sizeVal
		if sizeAuto {
			size = fillSize
		}
		return cbStart + sVal + mStart, size
	case sAuto && !eAuto:
		// end specified, start auto: place against the far edge.
		size := sizeVal
		if sizeAuto {
			size = fillSize
		}
		borderBoxSize := size + insets
		return cbStart + cbExtent - eVal - mEnd - borderBoxSize, size
	default:
		// all auto: static-position approximation (cb start edge).
		size := sizeVal
		if sizeAuto {
			size = fillSize
		}
		return cbStart + mStart, size
	}
}

// isAuto2 reports whether a length is the auto keyword (helper for absRect's
// both-specified height-derivation guard).
func isAuto2(l gcss.Length, fontSizePt float64) bool {
	_, a := resolveLen(l, fontSizePt, 0)
	return a
}
```

NOTE for the implementer: the `absRect` height-auto handling above is intentionally conservative — pass 2 lays out the interior to get the real content height when height is auto and only `top` is set, then sets the fragment `H`. Keep the `absRect` return for the definite-height and both-offsets cases; let the two-pass caller (Task 5) finalize the auto-content-height + top-only case after the interior pass. Cross-check the math against spec lines ~159-180 and adjust comments to match the final code. The `_ = cw` style unused-var care: ensure every returned value is used (golangci `unused`).

- [ ] **Step 4: Run the tests** → all PASS. Add any missing case the spec lists.

- [ ] **Step 5: Lint/gofmt** — `gofmt -l pkg/layout/css`, `golangci-lint run ./pkg/layout/css`. Clean. **No** `min`/`max` — the `if x < 0 { x = 0 }` clamps stay explicit.

---

## Task 4: The stacking pass in `fragment.go`

**Files:**
- Modify: `pkg/layout/css/fragment.go` (`Fragment` struct ~line 24-47; `AppendItems` ~line 106-128; `appendDecorations` ~line 141; `appendContent` ~line 164)
- Test: `pkg/layout/css/fragment_test.go`

Implements the spec's "The stacking pass" section. Read spec lines ~266-360. **This is the load-bearing paint change — non-positioned pages must stay byte-identical.**

- [ ] **Step 1: Write the failing tests** (add to `pkg/layout/css/fragment_test.go`; read the file first for its existing fragment-construction helpers — 5a added `TestAppendItemsFloatPaintsOwnSubtree` and friends, reuse that style)

```go
// TestAppendItemsNonPositionedByteIdentical: a tree with NO positioned boxes
// produces the exact same item slice with the stacking pass as the 5a 3-phase pass.
// (Guards the "non-positioned pages byte-identical" invariant.) Build a BFC root
// with a background, a normal child with a background + a glyph line, and assert the
// item KINDS/coords are exactly decorations-then-content order.
func TestAppendItemsNonPositionedByteIdentical(t *testing.T) {
	// root (BFC, stacking context) bg; child bg + one glyph.
	child := &Fragment{X: 0, Y: 20, W: 100, H: 30, Background: color.RGBA{1, 1, 1, 255}}
	child.Lines = []LineFragment{{BaselineY: 35, Glyphs: []GlyphFragment{{Outline: dummyOutline(), X: 5, SizePt: 12, Color: color.RGBA{0, 0, 0, 255}}}}}
	root := &Fragment{X: 0, Y: 0, W: 100, H: 60, Background: color.RGBA{2, 2, 2, 255}, IsBFC: true, IsStackingContext: true, Children: []*Fragment{child}}
	items := root.AppendItems(nil)
	// Expect: root bg, child bg, child glyph — backgrounds before glyph (decorations
	// before content), 3 items.
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	if items[0].Kind != layout.BackgroundKind || items[1].Kind != layout.BackgroundKind || items[2].Kind != layout.GlyphKind {
		t.Errorf("item kinds = %v/%v/%v, want bg/bg/glyph", items[0].Kind, items[1].Kind, items[2].Kind)
	}
}

// TestAppendItemsPositionedPaintsLast: a positioned child paints AFTER an in-flow
// sibling's content (positioned layer is last). Build a root with two children: a
// normal child (bg) and a positioned child (bg, IsPositioned, on the root's
// Positioned slice and excluded from Children-content). Assert the positioned bg
// comes last.
func TestAppendItemsPositionedPaintsLast(t *testing.T) {
	normal := &Fragment{X: 0, Y: 0, W: 50, H: 20, Background: color.RGBA{1, 1, 1, 255}}
	posChild := &Fragment{X: 0, Y: 0, W: 50, H: 20, Background: color.RGBA{9, 9, 9, 255}, IsPositioned: true, IsStackingContext: true}
	root := &Fragment{X: 0, Y: 0, W: 100, H: 40, IsBFC: true, IsStackingContext: true,
		Children:   []*Fragment{normal, posChild}, // posChild in Children (skipped in-flow) ...
		Positioned: []*Fragment{posChild},          // ... and in the positioned layer.
	}
	items := root.AppendItems(nil)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (normal bg, positioned bg)", len(items))
	}
	if items[len(items)-1].Rule.Color != (color.RGBA{9, 9, 9, 255}) {
		t.Errorf("last item is not the positioned bg; positioned layer not last")
	}
}

// TestAppendItemsRelativeOffsetTranslatesRange: a relative fragment's RelOffsetX/Y
// shifts ALL of its emitted items (and its subtree's) by the offset.
func TestAppendItemsRelativeOffsetTranslatesRange(t *testing.T) {
	rel := &Fragment{X: 10, Y: 10, W: 30, H: 30, Background: color.RGBA{5, 5, 5, 255},
		IsPositioned: true, IsStackingContext: true, RelOffsetX: 5, RelOffsetY: 7}
	root := &Fragment{X: 0, Y: 0, W: 100, H: 100, IsBFC: true, IsStackingContext: true,
		Children: []*Fragment{rel}, Positioned: []*Fragment{rel}}
	items := root.AppendItems(nil)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	// rel's background border box is X=10,Y=10; with the +5/+7 offset it must paint at 15/17.
	if items[0].Rule.XPt != 15 || items[0].Rule.YPt != 17 {
		t.Errorf("relative bg at (%v,%v), want (15,17) (offset applied)", items[0].Rule.XPt, items[0].Rule.YPt)
	}
}
```

(Use whatever glyph-outline dummy the file already has; `dummyOutline()` is a placeholder name.)

- [ ] **Step 2: Run to verify it fails** — compile error (`IsPositioned`/`Positioned`/`RelOffsetX`/`IsStackingContext` undefined).

- [ ] **Step 3: Add the Fragment fields**

In `pkg/layout/css/fragment.go`, after the `Floats` field (~line 46):

```go
	// IsPositioned marks a fragment produced by a positioned box (relative,
	// absolute, or fixed). The stacking pass lifts such a fragment out of the
	// in-flow decoration/content passes and paints it in the positioned layer
	// instead (CSS 2.1 Appendix E). For a relative box (which IS in flow) this
	// moves only its painting; its in-flow space stays reserved.
	IsPositioned bool
	// RelOffsetX/RelOffsetY is a relatively-positioned box's paint-time offset
	// (CSS 9.4.3). Applied as a translate over the fragment's flattened item range
	// when the positioned layer paints it (NOT by shiftFragment/translateFragment,
	// which do not recurse Positioned). Zero for absolute/fixed (their position is
	// baked into the fragment coordinates by the abs-pos pass).
	RelOffsetX, RelOffsetY float64
	// IsStackingContext marks a fragment that establishes a stacking context (the
	// root and every positioned box). Such a fragment owns the Appendix E phase
	// ordering for its subtree, ending with its positioned layer.
	IsStackingContext bool
	// Positioned holds the fragments of positioned descendants painted in this
	// stacking context's positioned layer (after in-flow content). Kept separate
	// from Children so in-flow tree order is untouched; a descendant in Positioned
	// is skipped in the in-flow passes (IsPositioned) so it paints exactly once.
	// In the minimal cut these paint in document order (no z-index sort).
	Positioned []*Fragment
```

- [ ] **Step 4: Generalize `AppendItems` into the stacking pass**

Replace the `IsBFC` branch of `AppendItems` (~line 107-114) so a stacking-context fragment runs the 4-phase order. Keep the existing non-BFC branch but add the `IsPositioned` child skip. The new shape:

```go
func (f *Fragment) AppendItems(dst []layout.Item) []layout.Item {
	if f.IsStackingContext || f.IsBFC {
		// Stacking context (root / positioned box) OR a BFC (inline-block): paint in
		// CSS 2.1 Appendix E order — own decorations are emitted by the in-flow
		// decoration walker starting at f; floats; in-flow content; then the
		// positioned layer. (A plain BFC that is not a stacking context has an empty
		// Positioned slice, so the positioned phase is a no-op and the order reduces
		// to the 5a sequence — preserving byte-identical output for non-positioned
		// pages.)
		dst = f.appendDecorations(dst) // in-flow backgrounds + borders (skip floats, nested BFCs, positioned)
		for _, fl := range f.Floats {  // the float layer
			start := len(dst)
			dst = fl.AppendItems(dst)
			// A positioned float (float:left; position:relative) carries a relative
			// offset; apply it to the float's emitted range, exactly like the
			// positioned layer below. A non-positioned float has zero offset, so this
			// is a guarded no-op — preserving byte-identical output for 5a float pages.
			if fl.RelOffsetX != 0 || fl.RelOffsetY != 0 {
				translateItems(dst, start, fl.RelOffsetX, fl.RelOffsetY)
			}
		}
		dst = f.appendContent(dst) // in-flow inline content + images (skip floats, positioned)
		for _, pf := range f.Positioned { // the positioned layer (document order; minimal z-index)
			start := len(dst)
			dst = pf.AppendItems(dst)
			if pf.RelOffsetX != 0 || pf.RelOffsetY != 0 {
				translateItems(dst, start, pf.RelOffsetX, pf.RelOffsetY)
			}
		}
		return dst
	}
	// Non-BFC, non-stacking fragment: paint self, then recurse (normal tree order),
	// skipping floated AND positioned children (painted by the owning stacking
	// context's float / positioned layers).
	dst = f.appendSelfDecorations(dst)
	dst = f.appendSelfContent(dst)
	for _, c := range f.Children {
		if c.IsFloat || c.IsPositioned {
			continue
		}
		dst = c.AppendItems(dst)
	}
	return dst
}
```

Add the `IsPositioned` skip to `appendDecorations` and `appendContent`:

- In `appendDecorations` (~line 144): change `if c.IsFloat || c.IsBFC {` to `if c.IsFloat || c.IsBFC || c.IsPositioned {`.
- In `appendContent` (~line 167): change `if c.IsFloat {` to `if c.IsFloat || c.IsPositioned {` (keep the separate `if c.IsBFC { atomic }` branch *after* this skip — but note a fragment can be both `IsBFC` and `IsPositioned`; the `IsPositioned` skip must win so a positioned inline-block is not also painted atomically in-flow. Order the checks so `IsPositioned` is checked first).

Add the `translateItems` helper near the bottom of the file:

```go
// translateItems shifts every item in dst[start:] by (dx,dy), mutating their XPt/YPt
// in place. It applies a relatively-positioned fragment's paint offset to the items
// the fragment (and its subtree, incl. any abs-pos descendant on its Positioned
// layer) just emitted via AppendItems — so the whole positioned subtree rides the
// relative shift. Every coordinate-bearing item kind carries XPt/YPt
// (Background/Border/Glyph/Image), so a uniform per-item translate is exact. This
// keeps AppendItems a pure read of the fragment tree: only the freshly-appended dst
// items are moved, never a Fragment.
func translateItems(dst []layout.Item, start int, dx, dy float64) {
	for i := start; i < len(dst); i++ {
		switch dst[i].Kind {
		case layout.BackgroundKind:
			dst[i].Rule.XPt += dx
			dst[i].Rule.YPt += dy
		case layout.BorderKind:
			dst[i].Border.XPt += dx
			dst[i].Border.YPt += dy
		case layout.GlyphKind:
			dst[i].Glyph.XPt += dx
			dst[i].Glyph.YPt += dy
		case layout.ImageKind:
			dst[i].Image.XPt += dx
			dst[i].Image.YPt += dy
		}
	}
}
```

**Verify the item field names** against `pkg/layout/page.go` (the spec review cited `XPt`/`YPt` on `RuleItem`/`BorderItem`/`GlyphItem`/`ImageItem` ~lines 113-146) — read it and match exactly. Update the `AppendItems` doc comment to describe the 4-phase stacking order and that the float-phase seam is now the stacking seam (point the deferred 5b-2 z-index work at the `Positioned`-layer loop).

- [ ] **Step 5: Run the tests** → the 3 new tests PASS, and **all existing fragment tests (5a float paint) still pass** (`go test ./pkg/layout/css -run 'TestAppendItems'`). If a 5a test breaks, the byte-identical invariant is violated — fix the ordering, don't update the test.

- [ ] **Step 6: Lint/gofmt** — clean.

---

## Task 5: Thread positioning through block layout + the two-pass resolve (`block.go`)

**Files:**
- Modify: `pkg/layout/css/block.go` (`Layout` ~line 47; `layoutTree` ~line 73; `layoutBlock` ~line 112; `layoutInterior` ~line 246; `layoutBlockChildren` ~line 309; `placeFloat` ~line 388; `establishesNewBFC` ~line 482; shift helpers ~line 680)
- Test: `pkg/layout/css/positioning_layout_test.go` (new)

This is the largest task. Read spec sections "How positioning threads through layout", "Two-pass abs-pos", and "Relative offset & descendant CB" (spec lines ~182-260) — and read the EXACT current `block.go` signatures (this plan's line numbers above). Implement incrementally; build after each sub-step.

**The threading model (mirrors the 5a `fc`/`bandOriginY` threading):**
- Add a `posCtx *positionedContext` and a `posCB posCBOwner` to `layoutBlock`, `layoutInterior`, `layoutBlockChildren` (and seed them in `layoutTree`). `positionedContext` collects deferred abs/fixed boxes; `posCBOwner` names the current abs-pos containing block.

- [ ] **Step 1: Define the collection + owner types** (in `block.go` or a small section of `positioning.go`)

```go
// posCBOwner names the containing block for absolutely-positioned descendants: the
// nearest positioned-ancestor fragment + its box (whose CONTENT box is the CB,
// derived by deflating the final border box), or the page sentinel (isPage) for the
// ICB / fixed CB. A fragment POINTER is captured (not a rect) because the ancestor
// is shifted into final page position as the recursion unwinds; pass 2 reads it
// after that, so coordinates are final. (See the spec: "How positioning threads".)
type posCBOwner struct {
	frag   *Fragment
	box    *cssbox.Box
	isPage bool
}

// deferredAbs is one collected absolutely/fixed-positioned box awaiting pass 2. Its
// stacking-context owner is NOT stored — it is derived in pass 2 from cb (cb.isPage ?
// root : cb.frag), since every positioned box is its own stacking context.
type deferredAbs struct {
	box *cssbox.Box
	cb  posCBOwner // its containing-block owner
}

// positionedContext accumulates deferred abs/fixed boxes during the in-flow pass for
// resolution in the abs-pos pass. Per-Layout-call mutable state threaded by pointer
// through one goroutine; never escapes the call (concurrency-safe, like floatContext).
type positionedContext struct {
	deferred []deferredAbs
}
```

**Attachment strategy (DECIDED — reuse the `bfcFloats` return-and-bubble idiom, do NOT pre-allocate a fragment shell or run a separate tree walk).** The plan review established the cleanest way to attach a positioned descendant to its nearest stacking-context ancestor's `Positioned` slice, given that `layoutBlock` builds a box's own fragment *after* its interior. It mirrors exactly how 5a surfaces a nested BFC's floats (`interior.bfcFloats`, set in `layoutInterior` at block.go:281-283, consumed in `layoutBlock` at block.go:218-221):

- The `interior` struct gains a field **`pendingPositioned []*Fragment`** — relative-positioned fragments built in this box's interior that have **not yet** found their stacking-context owner (analogous to `bfcFloats`, but for the positioned layer).
- A **`relative`** child's fragment (built in flow, already in `Children`) is appended to the interior's `pendingPositioned` by `layoutBlockChildren` (NOT attached to any `Positioned` yet — its owner may be an ancestor not yet built).
- In `layoutBlock`, after the box's own `frag` is built: if `establishesStackingContext(b)` (i.e. `b` is positioned), `b` **consumes** the pending list onto its own `frag.Positioned` (those descendants' nearest SC is `b`). Otherwise (static box) it **bubbles** the pending list up via its own returned `interior.pendingPositioned`, so an ancestor stacking context consumes it. The page root (always a stacking context) consumes whatever reaches it in `layoutTree`.
- An **abs/fixed** box is collected into `posCtx.deferred` (out of flow); its `Positioned` attachment happens in **pass 2** (`resolveAbsolute`), where the owner is derived from `cb`.

This satisfies the invariant — a positioned box attaches to its nearest enclosing stacking context, whether that is its immediate (positioned) parent, a positioned ancestor several levels up (bubbling through static boxes), or the root — using only the proven `bfcFloats` mechanism. No early-shell allocation, no struct-literal rewrite, no O(n) post-pass. The transitive case (a relative box nested inside an abs box) works because pass 2 lays the abs subtree out with the same `layoutBlock`, which runs the same bubble-and-consume for *its* interior (the abs box is a stacking context, so it consumes its interior's pending relatives onto its own `Positioned`).

- [ ] **Step 2: Add `establishesStackingContext`** (near `establishesNewBFC`)

```go
// establishesStackingContext reports whether b establishes a CSS stacking context.
// In the supported subset: any positioned box (relative/absolute/fixed). The page
// root is treated as a stacking context by layoutTree directly. (Full CSS also
// includes opacity<1, transforms, etc. — none modeled yet.)
func establishesStackingContext(b *cssbox.Box) bool {
	return b.Position != cssbox.PosStatic
}
```

- [ ] **Step 3: Thread the params + seed in `layoutTree`**

Add `posCtx *positionedContext, posCB posCBOwner` as trailing params to `layoutBlock`, `layoutInterior`, `layoutBlockChildren` (after the existing `fc *floatContext`). In `layoutTree`:

```go
func (e *Engine) layoutTree(ctx context.Context, root *cssbox.Box, viewportW float64) *Fragment {
	if root == nil {
		return nil
	}
	fc := &floatContext{cbLeft: 0, cbRight: viewportW}
	posCtx := &positionedContext{}
	pageCB := posCBOwner{isPage: true}
	res := e.layoutBlock(ctx, root, viewportW, 0, 0, 0, fc, posCtx, pageCB)
	if res.frag != nil {
		res.frag.IsBFC = true
		res.frag.IsStackingContext = true // the ICB establishes the root stacking context
		if res.frag.Floats == nil {
			res.frag.Floats = fc.floats2frags()
		}
	}
	// PASS 2: resolve abs/fixed boxes now that the page height and all ancestor
	// fragments are final.
	pageH := 0.0
	if res.frag != nil {
		pageH = res.frag.Y + res.frag.H
	}
	e.resolveAbsolute(ctx, posCtx, res.frag, viewportW, pageH)
	return res.frag
}
```

`Layout` already calls `layoutTree` then computes `contentH = frag.Y + frag.H`; that still holds (pass 2 attaches abs-pos fragments to `Positioned` layers; an abs-pos box does NOT extend the page height in this slice — document that, matching the float non-enclosure decision). If an abs-pos box positioned past the page bottom should grow the page, that is deferred (note it).

- [ ] **Step 4: Collect abs/fixed and handle relative in `layoutBlockChildren`**

In `layoutBlockChildren`, the per-child loop currently checks `child.Float != FloatNone` first (~line 334). Add positioning handling. **Order matters** (abs/fixed is already float-none by box-gen, so the float branch won't catch it):

**4a. Collect an abs/fixed child** (out of flow) into `posCtx.deferred`. It does not advance the cursor, collapse margins, or join `Children`:

```go
		// Absolutely/fixed-positioned child: out of flow. Collect for pass 2.
		if child.Position == cssbox.PosAbsolute || child.Position == cssbox.PosFixed {
			cb := posCB
			if child.Position == cssbox.PosFixed {
				cb = posCBOwner{isPage: true} // fixed: the page (viewport)
			}
			posCtx.deferred = append(posCtx.deferred, deferredAbs{box: child, cb: cb})
			continue // no SC owner stored; pass 2 derives it from cb
		}
```

**4b. Stamp a relative child + add it to the interior's `pendingPositioned`** (the bubble list). Lay it out in flow exactly as today; after `shiftFragment` places its fragment, mark it positioned and append it to a local `pendingPositioned` slice that `layoutBlockChildren` returns on its `interior`:

```go
		// (after the non-float, non-abs child fragment is laid out and shifted into place:)
		if child.Position == cssbox.PosRelative {
			dx, dy := relativeOffset(child, contentW, 0) // cbH basis: see note (px/em offsets ignore it)
			res.frag.IsPositioned = true
			res.frag.IsStackingContext = true
			res.frag.RelOffsetX, res.frag.RelOffsetY = dx, dy
			pendingPositioned = append(pendingPositioned, res.frag) // bubbles to the nearest SC
		}
```

Declare `var pendingPositioned []*Fragment` at the top of `layoutBlockChildren` and return it on the `interior` (add the field — see 4c). Note `cbH` for `relativeOffset`: top/bottom percentages resolve against the containing block (parent content box) height, which may be auto/unknown here; per the spec's deferral pass `0` (px/em offsets ignore the basis; a `%` top/bottom degrades to 0 — documented). The `IsBFC` flag on the fragment is set later by `layoutBlock` only if `b` establishes a BFC; a `relative` box is a stacking context but not necessarily a BFC, and `AppendItems` takes the stacking branch on `IsStackingContext || IsBFC`, so a non-BFC relative box still paints in stacking order.

**4c. Add the bubble field to `interior` and thread it through `layoutInterior`.** In the `interior` struct (block.go:231-238), add:

```go
	pendingPositioned []*Fragment // relative-positioned descendants awaiting their stacking-context owner (bubbles up like bfcFloats)
```

`layoutBlockChildren` returns it (`interior{..., pendingPositioned: pendingPositioned}`). `layoutInterior` (block.go:246) must propagate it from the `layoutBlockChildren`/`layoutInline` result up to its own return (it already returns the `interior` it builds; ensure the field is carried — for the `InlineFC` case there are no block-level relative children to bubble from the IFC, so it stays nil there; relative inline atoms are out of scope per the spec).

**4d. Consume-or-bubble in `layoutBlock`.** After `frag` is built (block.go:205) and the existing `if establishesNewBFC(b) { frag.IsBFC = true; frag.Floats = in.bfcFloats }` block (block.go:218-221), add the positioned consume/bubble — and mark the stacking-context flag:

```go
	if establishesStackingContext(b) {
		frag.IsStackingContext = true
	}
	// Consume the interior's pending relatives if this box is their nearest stacking
	// context; otherwise bubble them up via this block's own result so an ancestor
	// stacking context (ultimately the root) consumes them.
	if establishesStackingContext(b) {
		frag.Positioned = append(frag.Positioned, in.pendingPositioned...)
	}
```

…and `layoutBlock` must surface the un-consumed pending list to ITS caller. Since `layoutBlock` returns a `blockResult` (frag + margins), and the bubbling happens through `layoutBlockChildren`→`layoutInterior`→`layoutBlock`→(parent's `layoutBlockChildren`), the cleanest plumbing is: `blockResult` gains a `pendingPositioned []*Fragment` field; `layoutBlock` sets it to `in.pendingPositioned` when `b` is NOT a stacking context (bubble), or `nil` when it consumed them; the parent's `layoutBlockChildren` collects each child's `res.pendingPositioned` into its own `pendingPositioned` slice. Concretely, in the parent loop after `out = append(out, res.frag)`:

```go
		pendingPositioned = append(pendingPositioned, res.pendingPositioned...)
```

So a relative box nested under static ancestors bubbles up `blockResult.pendingPositioned` at each static level until a stacking-context ancestor (or the root) consumes it. **The root** consumes whatever reaches it: in `layoutTree`, after `res.frag.IsStackingContext = true`, add `res.frag.Positioned = append(res.frag.Positioned, res.pendingPositioned...)`.

Add `pendingPositioned []*Fragment` to `blockResult` (block.go:96-100) with a doc comment, and ensure `layoutBlockReplaced` returns it as nil (a replaced box has no in-flow children). **This field is read** (consumed by the parent), satisfying golangci `unused`.

**Why this is correct + concurrency-safe:** it is the exact shape of `bfcFloats` (a per-interior slice surfaced up one level and consumed at the BFC owner), extended to bubble through multiple static levels. No fragment is built early; no fragment pointer is shared before it exists; the relative fragment already exists (built in flow) and is only *referenced* from a `Positioned` slice (the same aliasing 5a uses for floats — later shifts propagate automatically). The `pendingPositioned` slices live only within the single `Layout` goroutine's recursion.

- [ ] **Step 5: Implement `resolveAbsolute` (pass 2)**

```go
// resolveAbsolute lays out and positions every deferred absolutely/fixed-positioned
// box (pass 2), now that the page and all ancestor fragments are final. For each: it
// resolves the containing-block CONTENT rect from the captured owner, lays out the
// box's own subtree as an independent block (its own fresh floatContext; the SAME
// posCtx so nested abs-pos descendants are appended to this loop and resolved
// transitively), computes the used border-box rect via absRect, translates the
// fragment there, marks it positioned + stacking context, and appends it to the
// owning stacking context's Positioned layer.
func (e *Engine) resolveAbsolute(ctx context.Context, posCtx *positionedContext, root *Fragment, viewportW, pageH float64) {
	pageRect := rect{x: 0, y: 0, w: viewportW, h: pageH}
	// Iterate by index: laying out a deferred box may APPEND more deferred boxes to
	// posCtx.deferred (a transitive abs-pos descendant collected by its layoutBlock),
	// which this same loop then resolves. The index walk (not range) picks them up.
	for i := 0; i < len(posCtx.deferred); i++ {
		d := posCtx.deferred[i]
		cb := e.resolveCBRect(d.cb, pageRect)
		ed := usedEdges(d.box, cb.w)
		border, contentW := absRect(d.box, ed, cb)

		// Lay out the box's subtree as a block at the CB CONTENT width, at a
		// provisional origin (originX = cb.x, marginTopEdgeY = cb.y, bandOriginY 0),
		// with a FRESH floatContext (its floats are self-contained) and the SAME
		// posCtx (so its own abs-pos descendants are collected for this loop). The
		// posCB owner for its interior is the box ITSELF (it is a positioned ancestor /
		// new CB): pass {frag: <its own frag, after build>, box: d.box}. Since the
		// frag isn't built before layoutBlock returns, thread a placeholder and let the
		// box's own layoutBlock set IsStackingContext + consume its interior's pending
		// relatives onto frag.Positioned (the same consume-or-bubble as pass 1). Use a
		// dedicated layoutBlock entry that knows its posCB is itself:
		childFC := &floatContext{cbLeft: cb.x, cbRight: cb.x + cb.w}
		res := e.layoutBlock(ctx, d.box, cb.w, cb.x, cb.y, 0, childFC, posCtx, posCBOwner{box: d.box} /* frag set post-build inside */)
		frag := res.frag
		if frag == nil {
			continue
		}
		// Move the provisional fragment to its resolved border-box origin.
		translateFragment(frag, border.x-frag.X, border.y-frag.Y)
		// Auto-height positioned by top only: absRect left a provisional border height;
		// the laid-out fragment's own H (content-derived) is authoritative. (When
		// height or top+bottom fixed the height, absRect's value already stands and
		// layoutBlock produced the same H.)
		frag.IsPositioned = true
		frag.IsStackingContext = true
		frag.RelOffsetX, frag.RelOffsetY = 0, 0 // abs/fixed bake position into coords

		// Attach to the owning stacking context's Positioned layer: the root for a
		// page CB, else the nearest-positioned-ancestor fragment (which is itself a
		// stacking context).
		owner := root
		if !d.cb.isPage && d.cb.frag != nil {
			owner = d.cb.frag
		}
		owner.Positioned = append(owner.Positioned, frag)
	}
}
```

Two threading details the implementer must honor:

1. **The abs box is its own posCB + SC owner for its interior.** When `layoutBlock` lays out an abs box `d.box`, its interior's relative descendants must attach to `d.box`'s fragment, and its abs descendants must record `d.box`'s fragment as their CB owner. Since `establishesStackingContext(d.box)` is true (it is positioned) and — with the `establishesNewBFC` change in Step 4e — it also establishes a BFC, `layoutBlock`'s existing consume-or-bubble (Step 4d) consumes the interior's pending relatives onto `frag.Positioned` automatically. For a nested abs descendant's CB owner to point at `d.box`'s fragment, `layoutBlockChildren` must pass `posCBOwner{frag: <d.box's frag>, box: d.box}` to its children — but `d.box`'s frag is built after its interior. Resolve exactly as the consume-or-bubble does: the nested abs is collected in pass-1-style with `cb = posCB` (the owner passed into `layoutBlock`), and here we pass `posCBOwner{box: d.box}` with a nil frag; **after** `layoutBlock` returns `frag`, back-fill any deferred entry whose `cb.box == d.box && cb.frag == nil` with `frag`. Simplest concrete rule: in `resolveAbsolute`, after building `frag`, set `d`'s own children's CB: iterate the newly-appended `posCtx.deferred[len-before:]` and set `.cb.frag = frag` where `.cb.box == d.box`. **Cleaner:** make `posCBOwner` carry the box only during collection and resolve `frag` at use — but the ancestor frag is needed for `resolveCBRect`. The robust implementation: capture `before := len(posCtx.deferred)` before the `layoutBlock` call, then `for j := before; j < len(posCtx.deferred); j++ { if posCtx.deferred[j].cb.box == d.box { posCtx.deferred[j].cb.frag = frag } }`. This wires nested-abs CBs to the just-built frag. Implement this back-fill; cover it with a transitive-abs test (an abs box inside an abs box, the inner positioned against the outer).

2. **`establishesNewBFC` must include abs/fixed** (Step 4e below), so an abs box isolates its float context and surfaces its `bfcFloats` — otherwise an abs box containing a float orphans it.

Add `resolveCBRect`:

```go
// resolveCBRect turns a captured posCBOwner into the containing-block CONTENT rect in
// final page coordinates: the page rect for the page sentinel, else the ancestor
// fragment's final border box deflated by the ancestor box's border+padding.
func (e *Engine) resolveCBRect(o posCBOwner, pageRect rect) rect {
	if o.isPage || o.frag == nil {
		return pageRect
	}
	ed := usedEdges(o.box, o.frag.W) // border+padding to deflate; cbWidth basis = border-box W (approx)
	return rect{
		x: o.frag.X + ed.bL + ed.pL,
		y: o.frag.Y + ed.bT + ed.pT,
		w: o.frag.W - ed.bL - ed.bR - ed.pL - ed.pR,
		h: o.frag.H - ed.bT - ed.bB - ed.pT - ed.pB,
	}
}
```

(The `usedEdges(o.box, o.frag.W)` percentage basis is approximate for `%` border/padding — acceptable; border/padding are rarely `%`. Note it.)

- [ ] **Step 4e (do before Step 5 runs): `establishesNewBFC` includes abs/fixed**

In `establishesNewBFC` (block.go:482-484), extend so an absolutely/fixed-positioned box also establishes a BFC (it isolates floats and margin-collapsing), matching CSS:

```go
func establishesNewBFC(b *cssbox.Box) bool {
	if b.Position == cssbox.PosAbsolute || b.Position == cssbox.PosFixed {
		return true
	}
	return b.Display == cssbox.DisplayInlineBlock || b.Float != cssbox.FloatNone
}
```

Update its doc comment to add abs/fixed to the list of BFC triggers. **Verify this does not change any existing golden** — no current fixture has an abs/fixed box (positioning is new), so the change is inert for existing pages. (A `relative` box does NOT establish a BFC — only abs/fixed/float/inline-block do.)

**Known limitations to DOCUMENT** (log + note; spec already lists these as deferred): a `bottom`-only auto-height abs box is positioned against a provisional zero height (only the `top`-only auto-height case is finalized from the interior); abs width/height are NOT min/max-clamped (the spec mentions clamping but the minimal cut skips it). Both degrade without panic; add a one-line `logf` where detectable and a code comment.

- [ ] **Step 6: `placeFloat` stamps a relative offset for a positioned float**

The float-layer paint translate is ALREADY in Task 4 Step 4's `AppendItems` (the `Floats` loop applies `RelOffsetX/Y` per float). This step only stamps that offset onto a positioned float's fragment. In `placeFloat` (~block.go:388), after `res.frag.IsFloat = true`:

```go
	res.frag.IsFloat = true
	if child.Position == cssbox.PosRelative {
		dx, dy := relativeOffset(child, cbWidth, 0) // cbH basis ~0 (px/em offsets unaffected)
		res.frag.IsPositioned = true
		res.frag.RelOffsetX, res.frag.RelOffsetY = dx, dy
	}
```

(A `float:left; position:relative` box is placed at the float edge by `placeFloat`, painted via the `Floats` layer, and shifted by the relative offset there — it is NOT additionally added to a `Positioned` slice. An abs/fixed box never reaches `placeFloat` because box-gen forces its `Float` to none, Task 2.)

- [ ] **Step 7: Write the layout/flag-combination tests** (`pkg/layout/css/positioning_layout_test.go`)

Cover (read spec "Tests" lines ~431-444 for the full list):
- `TestRelativeBoxInFlowUnchangedButFlagged` — a relative box's fragment X/Y is its in-flow position; `IsPositioned` true; `RelOffsetX/Y` correct; a following sibling's Y is unchanged (no reflow).
- `TestAbsoluteRemovedFromFlow` — an abs box between two normal blocks: the second normal block stacks directly under the first (as if the abs box were absent).
- `TestAbsoluteAgainstRelativeAncestor` — `relative` container at some page Y, `absolute` child `top:0;left:0` lands at the container's content-box origin (NOT the page origin).
- `TestFixedAgainstPage` — a `fixed` child `top:0;left:0` lands at page (0,0)+margins regardless of ancestors.
- `TestAbsoluteNoAncestorIsPage` — an abs box with no positioned ancestor lands against the page.
- `TestPositionedFloatPlacedAndOffset` — `float:left; position:relative; top:5; left:5`: the float fragment is at the float edge, `IsFloat`, with `RelOffsetX/Y == (5,5)`.
- `TestAbsFloatCollectedNotFloated` — `position:absolute; float:left`: `Float==FloatNone`, box is in `posCtx.deferred`, NOT in the float layer.
- `TestRelativeParentAbsChildPaintCoords` — the spec's load-bearing test: render via `AppendItems` (or the engine's flatten) and assert the abs child's painted item coords = parent in-flow content origin + parent relative offset. (This exercises the flattened-range translate end-to-end.)

These build small `cssbox.Box` trees directly (remember the zero-value Length trap: set Width/Height/MaxWidth/MaxHeight and the offset fields explicitly) and call `e.layoutTree(...)` / `Engine.Layout`, then assert fragment geometry or flattened items. Reuse helpers from `block_test.go`/`floats_layout_test.go` (read them).

- [ ] **Step 8: Run all `pkg/layout/css` tests** — `go test ./pkg/layout/css`. All green, including the 5a float tests and the Task 4 fragment tests. `go test -race ./pkg/layout/css`.

- [ ] **Step 9: Lint/gofmt** — `gofmt -l pkg/layout/css`, `golangci-lint run ./pkg/layout/css`. Clean (explicit clamps; every new field read; no `if !(...)`).

---

## Task 6: Golden images + WPT reftests + end-to-end

**Files:**
- Modify: `pkg/doctaculous/html_golden_test.go` (add two `htmlGoldens`)
- Modify: `pkg/doctaculous/wpt_reftest_test.go` (add two `wptReftests`)
- New: the four reftest HTML files under `pkg/doctaculous/testdata/wpt/css21-normal-flow/`
- Goldens (generated): `pkg/doctaculous/testdata/golden/html-position-relative.png`, `html-position-absolute.png`

- [ ] **Step 1: Add the goldens to `htmlGoldens`** (read the slice ~line 21-163 first; mirror the `float-figure` entry style)

```go
	{
		// position:relative shifts a box at paint time WITHOUT moving its neighbors:
		// three inline-block swatches in a row, the middle one relatively offset
		// down+right. Eyeball: the middle box visibly overlaps/shifts while the first
		// and third hold their in-flow positions (the third does NOT slide left into
		// the offset box's vacated space — relative reserves it).
		name:       "position-relative",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .row span { display: inline-block; width: 60px; height: 60px; }
  .a { background: #cc3333; }
  .b { background: #33aa33; position: relative; top: 20px; left: 20px; }
  .c { background: #3355cc; }
</style></head><body>
  <div class="row"><span class="a"></span><span class="b"></span><span class="c"></span></div>
</body></html>`,
	},
	{
		// position:absolute pins a child to a corner of its relatively-positioned
		// container, painted ABOVE the container's own content. Eyeball: the small
		// box sits at the container's top-right corner, on top of the container's
		// background/text.
		name:       "position-absolute",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .box { position: relative; width: 200px; height: 120px; background: #dddddd; }
  .pin { position: absolute; top: 0; right: 0; width: 40px; height: 40px; background: #cc3333; }
</style></head><body>
  <div class="box">Container text<div class="pin"></div></div>
</body></html>`,
	},
```

- [ ] **Step 2: Author the reftest pairs** (read `testdata/wpt/css21-normal-flow/float-left.html` + `-ref.html` for the exact pattern; keep them minimal, `body{margin:0}`)

`abs-pos.html` (test): a `relative` container with an `absolute` child at `top:30;left:40` of fixed size.
`abs-pos-ref.html` (reference): the same container with the child placed by `margin-top:30;margin-left:40` in normal flow at the same size (a static box at the identical coords). They must rasterize identically. **Author carefully** so the abs child's CB content origin == the static child's flow origin (account for the container's border/padding — keep both 0 for simplicity).

`relative-offset.html` (test): a box offset by `position:relative; top:T; left:L`.
`relative-offset-ref.html` (reference): the same box at the shifted position via `margin-top:T; margin-left:L` with `position:static`. Per the spec's note, author so the reserved in-flow space difference doesn't affect compared pixels (e.g. the offset box is the only painted content and the reference reserves matching space, OR the page bounds match and the only differing pixels are within tolerance). If a clean pixel-equivalence is hard (the relative version reserves the un-offset space, changing page height), make the box small and the offset small, and verify the bounds match; if they can't match, **drop this reftest and rely on the `TestRelativeParentAbsChildPaintCoords` + golden instead** (document the drop in a NOTE comment, exactly as 5a dropped `float-row`).

- [ ] **Step 3: Register the reftests** in `wptReftests`:

```go
	{"abs-pos", 240, "an absolute box at top/left inside a relative container == a static box at the same coords", nil},
	{"relative-offset", 240, "a relative offset == the same box placed at the shifted position with margins", nil},
```

(Drop `relative-offset` here if Step 2 couldn't make the bounds match — keep `abs-pos`.)

- [ ] **Step 4: Generate the goldens** — `go test ./pkg/doctaculous -run TestHTMLGolden -update` (sandbox disabled). Then **the controller eyeballs every new PNG** via the Read tool: confirm `position-relative` shows the middle box shifted down-right with neighbors in place, and `position-absolute` shows the red pin at the container's top-right over the grey. **Confirm no pre-existing golden changed:** `git status --short pkg/doctaculous/testdata/ pkg/render/raster/testdata/` shows ONLY the two new `html-position-*.png` files (and the new reftest HTML). If any existing golden shows as modified, the stacking pass reordered a non-positioned page — STOP and fix.

- [ ] **Step 5: Run the full doctaculous + reftest suite** — `go test ./pkg/doctaculous` (TestHTMLGolden, TestWPTReftests, TestDOCXGolden all green). `go test -race ./...`.

- [ ] **Step 6: Lint/gofmt** the changed test files — clean.

---

## Task 7: Full verification + CLAUDE.md

- [ ] **Step 1: Whole-repo verification** (sandbox disabled):
  - `go build ./...`
  - `go test ./...` (all packages green)
  - `go test -race ./...` (concurrency clean — the new `positionedContext` is per-call, like `floatContext`)
  - `gofmt -l pkg/css pkg/layout/css pkg/layout/cssbox pkg/doctaculous` (no output)
  - `golangci-lint run ./pkg/css ./pkg/layout/css ./pkg/layout/cssbox ./pkg/doctaculous` (clean)
  - `find . -name 'zz_*' -delete` and `git status` (clean tree; only intended new/changed files)

- [ ] **Step 2: Update CLAUDE.md** — move **positioning** out of the §6 TODO into the Done section. Add a Done bullet under the HTML rendering entries, mirroring the floats bullet's style, summarizing: `position:relative` paint-time offset; `absolute`/`fixed` two-pass out-of-flow placement against the nearest positioned ancestor / page; the minimal stacking pass (positioned layer after floats + in-flow content, document order); `position` overrides `float` (CSS 9.7); offsets + `z-index` parsed (z-index not yet sorted — deferred); the `positioning.go` geometry; the new goldens/reftests. In §6's TODO, update the "positioning" line to "done" and re-point the remaining sub-list (overflow, tables, web fonts, …) — note the **deferred**: full Appendix E z-index sort (negative/numeric), precise static-position solve, `margin:auto` centering, relative-on-text-inline. Keep wording tight and factual (the controller writes this from the spec's "Deferred" + "Goal" sections).

- [ ] **Step 3: Two-stage review** (per task during execution AND a holistic final): spec-review then code-quality-review on the assembled diff, fix findings, re-verify, propagate any fix back into the spec/plan. Then finish the branch (PR per the handover).

---

## Verification checklist (definition of done)

- [ ] `position: static|relative|absolute|fixed`, `top/right/bottom/left`, `z-index` parse + cascade (not inherited).
- [ ] `relative` offsets at paint time; flow + siblings unchanged; box reserves its un-offset space.
- [ ] `absolute`/`fixed` taken out of flow (siblings unaffected); positioned against nearest positioned ancestor's content box / the page; two-pass resolution against final geometry.
- [ ] Positioned boxes paint in their own layer after in-flow content (minimal stacking pass); non-positioned pages **byte-identical** to 5a.
- [ ] `position:absolute|fixed` forces `float:none`; `position:relative; float:left` stays a float AND carries the relative offset.
- [ ] Flag combinations tested (positioned float, abs+float, positioned inline-block, z-indexed abs in relative parent).
- [ ] Geometry unit tests (`relativeOffset`, `absRect`) green; layout/flag-combination/paint-coordinate tests green.
- [ ] Two new goldens eyeballed; two reftests (or one + documented drop) green; no pre-existing golden changed.
- [ ] `go test ./...`, `go test -race ./...`, `gofmt -l`, `golangci-lint` all clean; no `zz_*` files; tree clean.
- [ ] CLAUDE.md updated (positioning → Done; deferrals noted).
- [ ] Degradations covered by tests/logs: non-auto z-index (document order), all-auto abs-pos (static approx), relative-on-text-inline (no-op).
