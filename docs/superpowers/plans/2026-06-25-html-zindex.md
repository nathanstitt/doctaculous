# Full CSS 2.1 Appendix E z-index stacking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the minimal document-order positioned-paint pass with full CSS 2.1 Appendix E z-index ordering (negative behind in-flow content, numeric sort within a stacking context), and clip a `position:relative` descendant of a non-positioned `overflow:hidden` box that today escapes the clip.

**Architecture:** All ordering logic stays in `pkg/layout/css/fragment.go` (the flatten seam). `Fragment` gains a `Box *cssbox.Box` pointer (the z-index source, read at flatten time) and its `PositionedClip []bool` widens to a `PositionedInfo []PositionedInfo` parallel slice carrying the CB-owned bit plus a `ClipChain []rect`. `AppendItems` packs the positioned layer into `[]positionedEntry`, stable-sorts by z (`sort.SliceStable`), partitions into negative/middle/positive bands, and emits negatives **before** the context decorations. The relative-clip-escape fix rides the existing `pendingPositioned` bubbling (the chain grows as an entry bubbles out of a non-positioned clipping box). The abs/fixed intervening-clip sub-case is deferred to a follow-up 6b (it needs new layout-threading; see the spec).

**Tech Stack:** Go (current stable; no new deps). Tests: standard `testing`, golden PNGs via `go test ./pkg/doctaculous -run TestHTMLGolden -update`, WPT-style reftests.

**Spec:** `docs/superpowers/specs/2026-06-25-html-zindex-design.md` — read "The sort + band split", "Fold vs. 6b", and "Tests" before starting.

**Branch:** You are on `feat/html-zindex`. Do NOT checkout/stash/switch branches.

**Process reminders (held across #1–#5c — these earned their keep):**
- **Sandbox blocks the Go build cache + TLS** — run `go`/`golangci-lint`/`gofmt` (and `gh`, `git push` over HTTPS) with the sandbox disabled.
- **Editor diagnostics LAG** — after adding `Box`/`PositionedInfo` you'll see stale "undefined"/"unused"/"redeclared" errors and phantom `zz_*` files. Trust `go build`/`go test` and `find . -name 'zz_*'`. Delete any `zz_*` throwaway before finishing; confirm `git status` clean.
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific packages (`./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...`), not the repo root. NO `//nolint`; the repo declines all `slices.*`/`max`/`min`/range-over-int modernize hints (keep `sort.SliceStable`, explicit `if x < y { x = y }` clamps, indexed loops). golangci-lint **does** flag `if !(a && b)` (QF1001 — write the De Morgan form) and an unused unexported field/func — so write test conditions in De-Morgan'd form, and run `golangci-lint` per task, not just `gofmt`.
- **The zero-value `Length` trap** — a raw `ComputedStyle`/`Box` literal omitting `Width`/`MaxWidth`/offsets reads as explicit `0`, not `auto`/`none`. Reuse `posStyle()`/`posBox()` (set z-index on the returned style), `blockBox()`, and the 5c `clipBoundsReal`/`bgIndex` helpers.

---

## File structure

- **`pkg/layout/css/fragment.go`** (modify) — add `Box *cssbox.Box`; rename `PositionedClip []bool` → `PositionedInfo []PositionedInfo` + the `PositionedInfo` struct; add `positionedEntry`, `sortedBands`, `zIndex()`, `zKey()`, `sortedPositioned()`, `appendBand()`; restructure `AppendItems`; delete `appendPositioned`.
- **`pkg/layout/css/block.go`** (modify) — stamp `frag.Box` at the 4 collection sites; change the 4 `PositionedClip` append sites to `PositionedInfo`; change the relative-bubbling type `[]*Fragment` → `[]pendingPos` (in `blockResult`, `interior`, `layoutBlockChildren`, `layoutTree`, `placeFloat`, `resolveAbsolute`) and grow the chain in `layoutBlock`; delete `logZIndexUnsupported` + its 2 call sites.
- **`pkg/layout/css/zindex_layout_test.go`** (create) — item-stream order unit tests (8 cases).
- **`pkg/layout/css/positioning_layout_test.go`** (modify) — delete `TestZIndexParsedButNotSortedLogs` + `containsZIndex`.
- **`pkg/doctaculous/html_golden_test.go`** (modify) — 4 new `htmlGoldens` entries.
- **`pkg/doctaculous/testdata/golden/html-zindex-*.png`** (create, via `-update`).
- **`pkg/doctaculous/wpt_reftest_test.go`** (modify) — 3 new `wptReftests` entries.
- **`pkg/doctaculous/testdata/wpt/css21-normal-flow/{zindex-negative,zindex-order,relative-clip-escape}{,-ref}.html`** (create).
- **`CLAUDE.md`** (modify, in the final task) — flip the z-index degradation notes to supported.

---

## Task 1: Add the `Box` pointer and stamp it at the 4 collection sites

This is the foundation: the z-index source on the fragment. No paint behavior changes yet (the sort lands in Task 3), so the whole existing corpus stays byte-identical — that is the test for this task.

**Files:**
- Modify: `pkg/layout/css/fragment.go` (the `Fragment` struct, ~line 24)
- Modify: `pkg/layout/css/block.go` (4 sites: `layoutBlock` ~331, `placeFloat` ~638, `resolveAbsolute` ~737, and note `layoutTree`'s root needs none)
- Test: `pkg/layout/css/zindex_layout_test.go` (create)

- [ ] **Step 1: Write the failing test** — a positioned fragment carries its source box.

Create `pkg/layout/css/zindex_layout_test.go`:

```go
package css

import (
	"context"
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// (Later tasks append tests to this file and one — Task 4's all-auto byte-identical
// test — needs the "github.com/nathanstitt/doctaculous/pkg/layout" import; add it to
// this block when that test is added. Adding an import before its first use is a real
// "imported and not used" compile error, NOT the diagnostics lag, so add imports with
// the code that uses them.)

// findByBox walks a fragment tree (Children + Positioned + Floats) and returns
// the first fragment whose Box == target, or nil.
func findByBox(f *Fragment, target *cssbox.Box) *Fragment {
	if f == nil {
		return nil
	}
	if f.Box == target {
		return f
	}
	for _, c := range f.Children {
		if g := findByBox(c, target); g != nil {
			return g
		}
	}
	for _, p := range f.Positioned {
		if g := findByBox(p, target); g != nil {
			return g
		}
	}
	for _, fl := range f.Floats {
		if g := findByBox(fl, target); g != nil {
			return g
		}
	}
	return nil
}

// TestPositionedFragmentCarriesBox: a relatively-positioned box's fragment retains a
// pointer to its source cssbox.Box (so the flatten z-sort can read Box.Style.ZIndex).
func TestPositionedFragmentCarriesBox(t *testing.T) {
	eng := New(nil, nil, nil)
	relStyle := posStyle()
	relStyle.Height = gcss.Length{Value: 20, Unit: gcss.UnitPx}
	relStyle.BackgroundColor = color.RGBA{1, 2, 3, 255}
	rel := posBox(relStyle, cssbox.PosRelative)
	root := posBox(posStyle(), cssbox.PosStatic, rel)

	frag := eng.layoutTree(context.Background(), root, 200)
	got := findByBox(frag, rel)
	if got == nil {
		t.Fatal("relative fragment not found in tree")
	}
	if got.Box != rel {
		t.Errorf("frag.Box = %p, want the source box %p", got.Box, rel)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css -run TestPositionedFragmentCarriesBox -v`
Expected: FAIL — `frag.Box` is nil (field doesn't exist yet → compile error `got.Box undefined`).

- [ ] **Step 3: Add the `Box` field to `Fragment`**

In `pkg/layout/css/fragment.go`, add the import and the field. The import block (top of file) currently is:

```go
import (
	"image"
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
)
```

Add `cssbox`:

```go
import (
	"image"
	"image/color"
	"sort"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render"
)
```

(`sort` is added now too; it is used in Task 3. If Go complains "imported and not used" between tasks, that is the lag — it is used by the end. To keep Task 1 compiling on its own, you may omit `sort` here and add it in Task 3; either is fine.)

In the `Fragment` struct, after the `DebugTag` field (~line 31), add:

```go
	// Box is the source cssbox.Box this fragment was produced from, retained so the
	// flatten/paint stage can read style-driven paint facts that are not pre-resolved
	// onto the fragment — today the stacking z-index (Box.Style.ZIndex/ZIndexAuto),
	// later opacity/isolation and SPA-snapshot re-flow. Set after layout; the flatten
	// stage only READS it and never mutates it, so the fragment tree stays safe to
	// share across the concurrent render fan-out — which holds only because layout has
	// fully completed before any flatten begins (there is no incremental relayout in
	// this engine yet). A nil Box reads as the initial style (z-index auto):
	// anonymous/synthetic fragments and the page root need not set it.
	Box *cssbox.Box
```

- [ ] **Step 4: Stamp `frag.Box` at the 3 positioned-fragment build sites**

In `pkg/layout/css/block.go`:

(a) `layoutBlock` — at the stacking-context consume site (~line 331-332). Currently:

```go
	if establishesStackingContext(b) {
		frag.IsStackingContext = true
```

Change to:

```go
	if establishesStackingContext(b) {
		frag.IsStackingContext = true
		frag.Box = b
```

(b) `placeFloat` — where the float fragment is finalized. After `res.frag.IsFloat = true` (~line 624) add:

```go
	res.frag.IsFloat = true
	res.frag.Box = child
```

(c) `resolveAbsolute` — at the abs/fixed finalize (~line 725-726). Currently:

```go
		frag.IsPositioned = true
		frag.IsStackingContext = true
```

Change to:

```go
		frag.IsPositioned = true
		frag.IsStackingContext = true
		frag.Box = d.box
```

> Note: a relatively-positioned box's fragment is built by `layoutBlock` for `b` (it goes through the stacking-context branch in (a), because `establishesStackingContext` is true for any positioned box including relative). So (a) covers the relative case the test exercises. The `placeFloat`/`resolveAbsolute` stamps cover positioned floats and abs/fixed.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/layout/css -run TestPositionedFragmentCarriesBox -v`
Expected: PASS.

- [ ] **Step 6: Verify the whole corpus is byte-identical (no paint change yet)**

Run: `go test ./pkg/layout/css ./pkg/doctaculous`
Expected: PASS (no golden/reftest changed — `Box` is read by nothing yet).

- [ ] **Step 7: Commit**

```bash
git add pkg/layout/css/fragment.go pkg/layout/css/block.go pkg/layout/css/zindex_layout_test.go
git commit -m "css/layout: retain source Box on Fragment (z-index source)"
```

---

## Task 2: Widen `PositionedClip []bool` to `PositionedInfo []PositionedInfo`

A mechanical, behavior-preserving retype: the bool becomes a struct field `CBOwned`, and a `ClipChain []rect` is added (unused until Task 4). The corpus stays byte-identical.

**Files:**
- Modify: `pkg/layout/css/fragment.go` (the field ~line 90, the doc, `appendPositioned` reader ~230)
- Modify: `pkg/layout/css/block.go` (4 append sites: ~93, ~334, ~636, ~736)

- [ ] **Step 1: Add the `PositionedInfo` struct and retype the field**

In `pkg/layout/css/fragment.go`, replace the `PositionedClip []bool` field (the whole block ~lines 82-90) with:

```go
	// PositionedInfo parallels Positioned: per-entry clip metadata telling the stacking
	// pass how to clip each positioned descendant painted in THIS holder's positioned
	// layer. len(PositionedInfo) == len(Positioned) when set; a nil/short slice reads as
	// the zero value (CBOwned=false, no clip chain) — the safe default, consulted only on
	// a clipping fragment.
	PositionedInfo []PositionedInfo
```

Add the struct (place it right after the `Fragment` struct, before `ImageContent`):

```go
// PositionedInfo is one entry of a Fragment's PositionedInfo slice (parallel to
// Positioned): how to clip the matching positioned descendant when it paints in this
// holder's positioned layer.
type PositionedInfo struct {
	// CBOwned reports that Positioned[i]'s containing block IS this holder fragment.
	// A clipping holder paints a CB-owned entry INSIDE its own clip bracket; a
	// non-CB-owned (bubbled-through) entry paints after ClipPop, outside this holder's
	// own clip.
	CBOwned bool
	// ClipChain holds the padding-box rects of every overflow≠visible box the descendant
	// passed THROUGH between itself and this holder, outermost-first. Empty for the
	// common case. When non-empty, the positioned phase brackets THIS entry's emitted
	// item range in a nested ClipPush(rect)…ClipPop for each rect — so a positioned
	// descendant of a non-positioned overflow:hidden box is cut at that box's padding box
	// even though it paints in an ancestor's layer (CSS: every overflow≠visible ancestor
	// between the box and its CB clips it). The holder's OWN clip (when CBOwned) is
	// applied by the bracket, NOT by this chain.
	ClipChain []rect
}
```

- [ ] **Step 2: Update the `appendPositioned` reader**

In `pkg/layout/css/fragment.go`, `appendPositioned` (~line 230-245) reads `f.PositionedClip`. Update the body:

```go
func (f *Fragment) appendPositioned(dst []layout.Item, onlyCBOwned bool) []layout.Item {
	for i, pf := range f.Positioned {
		if f.Clips {
			owned := i < len(f.PositionedInfo) && f.PositionedInfo[i].CBOwned
			if owned != onlyCBOwned {
				continue
			}
		}
		start := len(dst)
		dst = pf.AppendItems(dst)
		if pf.RelOffsetX != 0 || pf.RelOffsetY != 0 {
			translateItems(dst, start, pf.RelOffsetX, pf.RelOffsetY)
		}
	}
	return dst
}
```

(This is interim — Task 3 replaces `appendPositioned` with `appendBand`. Keeping it correct here keeps the build green between tasks.)

- [ ] **Step 3: Update the 4 append sites in `block.go`**

Each site appends a bool today; make it append a `PositionedInfo`.

(a) `layoutTree` (~lines 92-94):

```go
		for range res.pendingPositioned {
			res.frag.PositionedInfo = append(res.frag.PositionedInfo, PositionedInfo{CBOwned: false})
		}
		res.frag.Positioned = append(res.frag.Positioned, res.pendingPositioned...)
```

(b) `layoutBlock` (~lines 333-335):

```go
		for range in.pendingPositioned {
			frag.PositionedInfo = append(frag.PositionedInfo, PositionedInfo{CBOwned: false})
		}
		frag.Positioned = append(frag.Positioned, in.pendingPositioned...)
```

(c) `placeFloat` (~lines 635-637):

```go
	for range res.pendingPositioned {
		res.frag.PositionedInfo = append(res.frag.PositionedInfo, PositionedInfo{CBOwned: false})
	}
	res.frag.Positioned = append(res.frag.Positioned, res.pendingPositioned...)
```

(d) `resolveAbsolute` (~lines 735-738):

```go
		if owner != nil {
			cbOwned := !d.cb.isPage && d.cb.frag != nil && owner == d.cb.frag
			owner.PositionedInfo = append(owner.PositionedInfo, PositionedInfo{CBOwned: cbOwned})
			owner.Positioned = append(owner.Positioned, frag)
		}
```

- [ ] **Step 4: Build and run the full suite**

Run: `go build ./... && go test ./pkg/layout/css ./pkg/doctaculous`
Expected: PASS — pure rename/retype, no behavior change, corpus byte-identical.

- [ ] **Step 5: Lint the changed packages**

Run (sandbox disabled): `gofmt -l pkg/layout/css pkg/doctaculous && go vet ./pkg/layout/css/... && golangci-lint run ./pkg/layout/css/...`
Expected: no output (clean).

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/fragment.go pkg/layout/css/block.go
git commit -m "css/layout: widen PositionedClip bool to PositionedInfo{CBOwned,ClipChain}"
```

---

## Task 3: The z-sort, band split, and `AppendItems` restructure (the load-bearing task)

This is the genuinely tricky task — give it the most care. It adds the stable sort, the three bands, and moves the negative band before decorations. The **byte-identical guard** (all-auto pages unchanged) is the safety net; verify it before committing. It also removes the `logZIndexUnsupported` degradation.

**Files:**
- Modify: `pkg/layout/css/fragment.go` (add `positionedEntry`, `sortedBands`, `zIndex`, `zKey`, `sortedPositioned`, `appendBand`; rewrite `AppendItems`; delete `appendPositioned`)
- Modify: `pkg/layout/css/block.go` (delete `logZIndexUnsupported` ~852-861 and its 2 call sites ~574, ~724)
- Modify: `pkg/layout/css/positioning_layout_test.go` (delete `TestZIndexParsedButNotSortedLogs` + `containsZIndex`)
- Test: `pkg/layout/css/zindex_layout_test.go` (add ordering cases)

- [ ] **Step 1: Write the failing tests** — the four core ordering assertions.

Append to `pkg/layout/css/zindex_layout_test.go` (the package + imports already exist from Task 1; add `"github.com/nathanstitt/doctaculous/pkg/layout"` to its imports if not present):

```go
// zfill returns a posStyle with a px height, a background color, and a z-index. Using
// px offsets (top/left) places the boxes so their backgrounds overlap in page space,
// making paint order observable via bgIndex.
func zfill(h float64, bg color.RGBA, top, left float64, z int, auto bool) gcss.ComputedStyle {
	s := posStyle()
	s.Height = gcss.Length{Value: h, Unit: gcss.UnitPx}
	s.Width = gcss.Length{Value: h, Unit: gcss.UnitPx}
	s.BackgroundColor = bg
	s.Top = gcss.Length{Value: top, Unit: gcss.UnitPx}
	s.Left = gcss.Length{Value: left, Unit: gcss.UnitPx}
	s.ZIndex = z
	s.ZIndexAuto = auto
	return s
}

// TestZNegativeBehindInFlowContent: a position:relative; z-index:-1 box paints BEFORE
// (behind) an in-flow sibling block's background. (The load-bearing assertion —
// negatives emit before decorations.)
func TestZNegativeBehindInFlowContent(t *testing.T) {
	eng := New(nil, nil, nil)
	negBG := color.RGBA{10, 0, 0, 255}
	flowBG := color.RGBA{0, 20, 0, 255}

	neg := posBox(zfill(40, negBG, 0, 0, -1, false), cssbox.PosRelative)
	flow := posBox(func() gcss.ComputedStyle {
		s := posStyle()
		s.Height = gcss.Length{Value: 40, Unit: gcss.UnitPx}
		s.BackgroundColor = flowBG
		return s
	}(), cssbox.PosStatic)
	root := posBox(posStyle(), cssbox.PosStatic, neg, flow)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	ni, fi := bgIndex(items, negBG), bgIndex(items, flowBG)
	if ni < 0 || fi < 0 {
		t.Fatalf("missing backgrounds: neg=%d flow=%d", ni, fi)
	}
	if ni >= fi {
		t.Errorf("negative-z background at %d must paint BEFORE in-flow content at %d", ni, fi)
	}
}

// TestZPositiveOverAuto: a z-index:2 abs box paints AFTER (over) a z:auto abs box.
func TestZPositiveOverAuto(t *testing.T) {
	eng := New(nil, nil, nil)
	autoBG := color.RGBA{0, 0, 30, 255}
	posBG := color.RGBA{40, 0, 0, 255}

	a := posBox(zfill(40, autoBG, 0, 0, 0, true), cssbox.PosAbsolute)
	p := posBox(zfill(40, posBG, 10, 10, 2, false), cssbox.PosAbsolute)
	cont := posBox(func() gcss.ComputedStyle { s := posStyle(); s.Height = gcss.Length{Value: 80, Unit: gcss.UnitPx}; return s }(), cssbox.PosRelative, a, p)
	root := posBox(posStyle(), cssbox.PosStatic, cont)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	ai, pi := bgIndex(items, autoBG), bgIndex(items, posBG)
	if ai < 0 || pi < 0 {
		t.Fatalf("missing backgrounds: auto=%d pos=%d", ai, pi)
	}
	if pi <= ai {
		t.Errorf("z:2 background at %d must paint AFTER z:auto at %d", pi, ai)
	}
}

// TestZNegAutoPositiveOrder: three positioned boxes z=-1, auto, z=1 paint in strictly
// increasing index order, straddling the decoration/content phases.
func TestZNegAutoPositiveOrder(t *testing.T) {
	eng := New(nil, nil, nil)
	negBG := color.RGBA{11, 0, 0, 255}
	midBG := color.RGBA{0, 11, 0, 255}
	posBG := color.RGBA{0, 0, 11, 255}

	neg := posBox(zfill(40, negBG, 0, 0, -1, false), cssbox.PosRelative)
	mid := posBox(zfill(40, midBG, 5, 5, 0, true), cssbox.PosRelative)
	pos := posBox(zfill(40, posBG, 10, 10, 1, false), cssbox.PosRelative)
	root := posBox(posStyle(), cssbox.PosStatic, neg, mid, pos)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	ni, mi, pi := bgIndex(items, negBG), bgIndex(items, midBG), bgIndex(items, posBG)
	if ni < 0 || mi < 0 || pi < 0 {
		t.Fatalf("missing backgrounds: neg=%d mid=%d pos=%d", ni, mi, pi)
	}
	if ni >= mi || mi >= pi {
		t.Errorf("want neg(%d) < mid(%d) < pos(%d)", ni, mi, pi)
	}
}

// TestZStableWithinBand: two z:5 boxes keep document order (stable sort), and two
// z:auto boxes keep document order, even interleaved in source.
func TestZStableWithinBand(t *testing.T) {
	eng := New(nil, nil, nil)
	a5 := color.RGBA{50, 1, 0, 255}
	b5 := color.RGBA{50, 2, 0, 255}
	aA := color.RGBA{0, 1, 50, 255}
	bA := color.RGBA{0, 2, 50, 255}

	// Source order: a5, aAuto, b5, bAuto.
	x1 := posBox(zfill(30, a5, 0, 0, 5, false), cssbox.PosRelative)
	x2 := posBox(zfill(30, aA, 4, 4, 0, true), cssbox.PosRelative)
	x3 := posBox(zfill(30, b5, 8, 8, 5, false), cssbox.PosRelative)
	x4 := posBox(zfill(30, bA, 12, 12, 0, true), cssbox.PosRelative)
	root := posBox(posStyle(), cssbox.PosStatic, x1, x2, x3, x4)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	i1, i2, i3, i4 := bgIndex(items, a5), bgIndex(items, aA), bgIndex(items, b5), bgIndex(items, bA)
	// Within z:5: a5 before b5. Within z:auto: aAuto before bAuto.
	if i1 >= i3 {
		t.Errorf("z:5 stable order broken: a5(%d) should precede b5(%d)", i1, i3)
	}
	if i2 >= i4 {
		t.Errorf("z:auto stable order broken: aAuto(%d) should precede bAuto(%d)", i2, i4)
	}
	// And the auto band (middle) paints after the z:5 band? No — z:5 is positive, auto
	// is middle, so auto paints BEFORE positive. Assert that too.
	if i2 >= i1 || i4 >= i1 {
		t.Errorf("z:auto (middle) must paint before z:5 (positive): auto=%d,%d pos=%d,%d", i2, i4, i1, i3)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./pkg/layout/css -run 'TestZNegativeBehindInFlowContent|TestZPositiveOverAuto|TestZNegAutoPositiveOrder|TestZStableWithinBand' -v`
Expected: FAIL — today positioned boxes paint in document order after content (no z-sort, negatives not moved before decorations).

- [ ] **Step 3: Add the sort + bands + helpers to `fragment.go`**

Add (after the `translateItems` func, near the bottom of `fragment.go`):

```go
// positionedEntry pairs a positioned descendant's fragment with its per-entry clip
// metadata, so the z-sort moves the two together without index bookkeeping.
type positionedEntry struct {
	frag *Fragment
	info PositionedInfo
}

// sortedBands is a fragment's positioned layer split into the three CSS 2.1 Appendix E
// z-index bands, each in stable (z, document) order. negatives paint BEFORE the
// context's decorations (step 2, behind in-flow content); middle paints after content
// (step 6, z:auto and z:0 in document order); positives paint last (step 7).
type sortedBands struct {
	negatives []positionedEntry // zKey < 0
	middle    []positionedEntry // zKey == 0 (auto + explicit 0)
	positives []positionedEntry // zKey > 0
}

// zIndex returns f's stacking sort inputs from its source box. A nil Box reads as the
// initial value (z-index auto).
func (f *Fragment) zIndex() (z int, auto bool) {
	if f.Box != nil {
		return f.Box.Style.ZIndex, f.Box.Style.ZIndexAuto
	}
	return 0, true
}

// zKey is f's numeric stacking sort key: auto and explicit 0 both map to 0 (the middle
// band), so they sort together and stable order preserves document order among them.
func (f *Fragment) zKey() int {
	z, auto := f.zIndex()
	if auto {
		return 0
	}
	return z
}

// sortedPositioned packs f's positioned layer into a fresh []positionedEntry (zipping
// Positioned[i] with PositionedInfo[i], a missing info read as the zero value),
// STABLE-sorts it by zKey ascending (document order — the slice's existing order —
// breaks ties), and splits it into the three z-bands. Building a fresh slice each call
// keeps f.Positioned/f.PositionedInfo read-only, so the shared fragment tree stays safe
// to flatten concurrently. When every entry is z:auto (the entire pre-z-index corpus),
// the negative/positive bands are empty and middle is the entries in their original
// document order — so AppendItems reduces to the prior document-order pass and output
// stays byte-identical.
func (f *Fragment) sortedPositioned() sortedBands {
	n := len(f.Positioned)
	if n == 0 {
		return sortedBands{}
	}
	entries := make([]positionedEntry, n)
	for i, pf := range f.Positioned {
		var info PositionedInfo
		if i < len(f.PositionedInfo) {
			info = f.PositionedInfo[i]
		}
		entries[i] = positionedEntry{frag: pf, info: info}
	}
	sort.SliceStable(entries, func(a, b int) bool {
		return entries[a].frag.zKey() < entries[b].frag.zKey()
	})
	// Partition at the first zKey>=0 and first zKey>0 boundaries.
	negEnd := 0
	for negEnd < n && entries[negEnd].frag.zKey() < 0 {
		negEnd++
	}
	midEnd := negEnd
	for midEnd < n && entries[midEnd].frag.zKey() == 0 {
		midEnd++
	}
	return sortedBands{
		negatives: entries[:negEnd],
		middle:    entries[negEnd:midEnd],
		positives: entries[midEnd:],
	}
}

// appendBand emits one band's positioned entries (already in stable z/document order)
// to dst. When filterCB is true (the clipping path), only entries whose
// info.CBOwned == wantCBOwned are emitted; when false (the non-clipping path), all
// entries are emitted and wantCBOwned is ignored. For each emitted entry it brackets
// the entry's item range in its ClipChain (outer→inner ClipPush … inner→outer ClipPop)
// and applies the relative RelOffset over the emitted range.
func (f *Fragment) appendBand(dst []layout.Item, band []positionedEntry, filterCB, wantCBOwned bool) []layout.Item {
	for _, e := range band {
		if filterCB && e.info.CBOwned != wantCBOwned {
			continue
		}
		for _, r := range e.info.ClipChain { // outermost first
			dst = append(dst, layout.Item{Kind: layout.ClipPushKind, Rule: layout.RuleItem{
				XPt: r.x, YPt: r.y, WPt: r.w, HPt: r.h,
			}})
		}
		start := len(dst)
		dst = e.frag.AppendItems(dst)
		if e.frag.RelOffsetX != 0 || e.frag.RelOffsetY != 0 {
			translateItems(dst, start, e.frag.RelOffsetX, e.frag.RelOffsetY)
		}
		for range e.info.ClipChain {
			dst = append(dst, layout.Item{Kind: layout.ClipPopKind})
		}
	}
	return dst
}
```

- [ ] **Step 4: Rewrite `AppendItems` to use the bands**

Replace the whole `AppendItems` body (the `if f.IsStackingContext || f.IsBFC { … }` block, ~lines 161-214) with:

```go
func (f *Fragment) AppendItems(dst []layout.Item) []layout.Item {
	if f.IsStackingContext || f.IsBFC {
		ord := f.sortedPositioned()
		if f.Clips {
			// Clipping context. Own decorations paint UNCLIPPED. Then: escaped negatives
			// (CB is an ancestor) unclipped & behind; the clip bracket wraps CB-owned
			// negatives (behind the children), child decorations, the float layer, in-flow
			// content, and the CB-owned middle+positive bands; escaped middle+positive
			// paint after ClipPop (unclipped — their CB is an ancestor).
			dst = f.appendSelfDecorations(dst)
			dst = f.appendBand(dst, ord.negatives, true, false) // escaped negatives, unclipped
			dst = append(dst, layout.Item{Kind: layout.ClipPushKind, Rule: layout.RuleItem{
				XPt: f.ClipRect.x, YPt: f.ClipRect.y, WPt: f.ClipRect.w, HPt: f.ClipRect.h,
			}})
			dst = f.appendBand(dst, ord.negatives, true, true) // CB-owned negatives, clipped
			dst = f.appendChildDecorations(dst)
			for _, fl := range f.Floats {
				start := len(dst)
				dst = fl.AppendItems(dst)
				if fl.RelOffsetX != 0 || fl.RelOffsetY != 0 {
					translateItems(dst, start, fl.RelOffsetX, fl.RelOffsetY)
				}
			}
			dst = f.appendContent(dst)
			dst = f.appendBand(dst, ord.middle, true, true)    // CB-owned middle, clipped
			dst = f.appendBand(dst, ord.positives, true, true) // CB-owned positives, clipped
			dst = append(dst, layout.Item{Kind: layout.ClipPopKind})
			dst = f.appendBand(dst, ord.middle, true, false)    // escaped middle, unclipped
			dst = f.appendBand(dst, ord.positives, true, false) // escaped positives, unclipped
			return dst
		}
		// Non-clipping stacking context / BFC: negatives BEFORE decorations, then the
		// 3-phase in-flow sequence, then middle, then positives.
		dst = f.appendBand(dst, ord.negatives, false, false)
		dst = f.appendDecorations(dst)
		for _, fl := range f.Floats {
			start := len(dst)
			dst = fl.AppendItems(dst)
			if fl.RelOffsetX != 0 || fl.RelOffsetY != 0 {
				translateItems(dst, start, fl.RelOffsetX, fl.RelOffsetY)
			}
		}
		dst = f.appendContent(dst)
		dst = f.appendBand(dst, ord.middle, false, false)
		dst = f.appendBand(dst, ord.positives, false, false)
		return dst
	}
	// Non-BFC, non-stacking fragment: unchanged.
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

- [ ] **Step 5: Delete the now-unused `appendPositioned`**

Remove the entire `appendPositioned` func (~lines 216-245) from `fragment.go` — `appendBand` replaces it. (golangci-lint flags an unused unexported func, so it MUST go.)

- [ ] **Step 6: Update the `AppendItems` doc comment**

Replace the doc block above `AppendItems` (the paragraph describing the 4-phase order and the "positioned-layer loop is the seam the deferred full-z-index slice extends" note, ~lines 134-160) with a description of the new band order. Suggested:

```go
// AppendItems appends f's drawing primitives, and its descendants', to dst in CSS 2.1
// Appendix E paint order, returning the extended slice. For a fragment that establishes
// a stacking context (IsStackingContext — the root and every positioned box) OR a block
// formatting context (IsBFC — inline-blocks and floats), the positioned layer is split
// by z-index into three bands (sortedPositioned): NEGATIVE z paints BEFORE the context's
// in-flow decorations (Appendix E step 2, behind in-flow content); then in-flow block
// decorations, the float layer, and in-flow inline content/images (steps 3–5, each
// skipping floated AND positioned subtrees); then the MIDDLE band (z:auto / z:0 in
// document order, step 6); then the POSITIVE band (step 7). The sort is STABLE so equal
// keys keep document order — a context whose positioned boxes are all z:auto produces
// the same stream as the prior document-order pass (byte-identical for the existing
// corpus). A plain BFC that is not a stacking context has an empty positioned layer, so
// all three bands are empty and the order reduces to decorations → floats → content. A
// non-BFC, non-stacking fragment paints self then recurses children (skipping floated
// and positioned children), unchanged.
//
// A clipping fragment (Clips) brackets its CONTENTS — children's decorations, floats,
// in-flow content, the CB-owned subset of each band — with a ClipPush(ClipRect)/ClipPop
// pair (its own background/border paint outside it). CB-owned negatives paint inside the
// bracket behind the children; escaped entries (CB is an ancestor) paint outside it. An
// entry carrying a ClipChain (a positioned descendant that bubbled through an
// overflow≠visible box on its way to this holder) is itself bracketed by that chain's
// rects, so it is clipped to the intervening box even when it paints in this layer.
//
// A relatively-positioned entry carries a paint-time RelOffset, applied via
// translateItems over its freshly-flattened range. AppendItems never mutates the
// fragment tree (the sort packs a local copy; only appended dst items are translated),
// so it is safe on a tree shared across the render fan-out.
```

- [ ] **Step 7: Remove the `logZIndexUnsupported` degradation**

In `pkg/layout/css/block.go`:
- Delete the call `e.logZIndexUnsupported(child)` at ~line 574 (in `layoutBlockChildren`, the relative branch).
- Delete the call `e.logZIndexUnsupported(d.box)` at ~line 724 (in `resolveAbsolute`).
- Delete the whole `logZIndexUnsupported` func (~lines 852-861).

- [ ] **Step 8: Delete the obsolete degradation test**

In `pkg/layout/css/positioning_layout_test.go`, delete `TestZIndexParsedButNotSortedLogs` (~lines 505-530) and the `containsZIndex` helper (~lines 532-end-of-func). (They assert the now-removed log fires.)

- [ ] **Step 9: Run the new ordering tests**

Run: `go test ./pkg/layout/css -run 'TestZNegativeBehindInFlowContent|TestZPositiveOverAuto|TestZNegAutoPositiveOrder|TestZStableWithinBand' -v`
Expected: PASS.

- [ ] **Step 10: Verify the byte-identical guard — the whole corpus unchanged**

Run: `go test ./pkg/layout/css ./pkg/doctaculous`
Expected: PASS. Then confirm no golden/reftest file changed:

Run: `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata`
Expected: NO output (no committed image changed). If any golden changed, the sort or band split broke the all-auto identity — STOP and fix before continuing.

- [ ] **Step 11: Race + lint**

Run (sandbox disabled): `go test -race ./pkg/layout/css ./pkg/doctaculous && gofmt -l pkg/layout/css && go vet ./pkg/layout/css/... && golangci-lint run ./pkg/layout/css/...`
Expected: all clean.

- [ ] **Step 12: Commit**

```bash
git add pkg/layout/css/fragment.go pkg/layout/css/block.go pkg/layout/css/positioning_layout_test.go pkg/layout/css/zindex_layout_test.go
git commit -m "css/layout: full Appendix E z-index ordering (negative/auto/positive bands)"
```

---

## Task 4: The relative-clip-escape fix (clip chain via pendingPos)

A `position:relative` descendant of a **non-positioned** `overflow:hidden` box bubbles past the clip and paints unclipped today. Carry the clipping box's padding box on the bubbling entry so the descendant is clipped where it paints (an ancestor's layer). The chain rides the existing `pendingPositioned` plumbing — change its element type from `*Fragment` to a `pendingPos{frag, clipChain}`, and grow the chain in `layoutBlock` when `b` clips and is non-positioned (its `frag.ClipRect` is already computed by then).

**Files:**
- Modify: `pkg/layout/css/block.go` (the `pendingPos` type; `blockResult`, `interior`, `layoutBlockChildren` locals; the consume/bubble sites; the chain-growth in `layoutBlock`; `placeFloat`/`layoutTree`/`resolveAbsolute` consumers)
- Test: `pkg/layout/css/zindex_layout_test.go` (the adversarial escape test)

- [ ] **Step 1: Write the failing test** — the adversarial relative-clip-escape case.

Append to `pkg/layout/css/zindex_layout_test.go` (uses `clipBoundsReal` + `bgIndex` from `overflow_layout_test.go`, same package):

```go
// TestRelativeChildOfNonPositionedClipIsClipped: a position:relative child of a
// NON-positioned overflow:hidden box, offset so it would spill past the clip edge,
// must still be clipped to the clip box's padding box — its background paints BETWEEN a
// ClipPush(clipRect) and a ClipPop even though the child bubbles to an ancestor's
// positioned layer (it is not the clip box's CB). The adversarial part: the offset
// pushes it past the edge, so an UNclipped render would paint outside the box.
func TestRelativeChildOfNonPositionedClipIsClipped(t *testing.T) {
	eng := New(nil, nil, nil)
	childBG := color.RGBA{77, 0, 0, 255}

	// Relative child, offset down+right past the clip box edge.
	childStyle := zfill(60, childBG, 40, 40, 0, true) // z:auto; big offset
	child := posBox(childStyle, cssbox.PosRelative)

	// Non-positioned overflow:hidden clip box, small, containing the child.
	clipStyle := posStyle() // static (NON-positioned) → BFC but not a stacking context
	clipStyle.Width = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	clipStyle.Height = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	clipStyle.Overflow = "hidden"
	clip := posBox(clipStyle, cssbox.PosStatic, child)
	root := posBox(posStyle(), cssbox.PosStatic, clip)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	push, pop := clipBoundsReal(items)
	ci := bgIndex(items, childBG)
	if push < 0 || pop < 0 {
		t.Fatalf("expected a clip bracket; push=%d pop=%d", push, pop)
	}
	if ci < 0 {
		t.Fatal("child background not painted")
	}
	if ci <= push || ci >= pop {
		t.Errorf("relative child background at %d must be INSIDE the clip bracket (push=%d, pop=%d)", ci, push, pop)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css -run TestRelativeChildOfNonPositionedClipIsClipped -v`
Expected: FAIL — today the child bubbles to the root's positioned layer and paints AFTER the clip box's ClipPop (ci > pop), unclipped.

- [ ] **Step 3: Add the `pendingPos` type and retype the bubbling channel**

In `pkg/layout/css/block.go`, add the type (near `deferredAbs`, ~line 147):

```go
// pendingPos is a relatively-positioned descendant bubbling toward its nearest
// stacking-context ancestor, plus the clip chain it has accumulated. clipChain holds
// the padding-box rects of the non-positioned overflow≠visible boxes it has bubbled OUT
// OF so far (outermost last appended, but stored outermost-first — see the prepend in
// layoutBlock). When the entry lands on a holder's Positioned, clipChain becomes that
// entry's PositionedInfo.ClipChain, so the descendant is clipped to those boxes even
// though it paints in an ancestor's layer (CSS: every overflow≠visible ancestor between
// a box and its containing block clips it).
type pendingPos struct {
	frag      *Fragment
	clipChain []rect
}
```

Change the field types:
- `blockResult.pendingPositioned` (~line 126): `[]*Fragment` → `[]pendingPos`.
- `interior.pendingPositioned` (~line 372): `[]*Fragment` → `[]pendingPos`.

- [ ] **Step 4: Build the pending entries in `layoutBlockChildren`**

In `layoutBlockChildren` (`pkg/layout/css/block.go`):
- The local declaration (~line 454): `pendingPositioned []*Fragment` → `pendingPositioned []pendingPos`.
- The relative-child append (~lines 569-576). Currently:

```go
		if child.Position == cssbox.PosRelative {
			dx, dy := relativeOffset(child, contentW, 0)
			res.frag.IsPositioned = true
			res.frag.IsStackingContext = true
			res.frag.RelOffsetX, res.frag.RelOffsetY = dx, dy
			e.logZIndexUnsupported(child)
			pendingPositioned = append(pendingPositioned, res.frag)
		}
		// Bubble up any relative descendants the child did not consume ...
		pendingPositioned = append(pendingPositioned, res.pendingPositioned...)
```

becomes (the `logZIndexUnsupported` line was already deleted in Task 3):

```go
		if child.Position == cssbox.PosRelative {
			dx, dy := relativeOffset(child, contentW, 0)
			res.frag.IsPositioned = true
			res.frag.IsStackingContext = true
			res.frag.RelOffsetX, res.frag.RelOffsetY = dx, dy
			pendingPositioned = append(pendingPositioned, pendingPos{frag: res.frag})
		}
		// Bubble up any relative descendants the child did not consume (already carrying
		// their own clip chains from deeper clipping boxes).
		pendingPositioned = append(pendingPositioned, res.pendingPositioned...)
```

- The return (~line 586): `interior{… pendingPositioned: pendingPositioned}` — type now matches, no text change needed.

- [ ] **Step 5: Consume-or-bubble in `layoutBlock`, growing the chain past a non-positioned clip**

In `layoutBlock` (`pkg/layout/css/block.go`), the positioning block (~lines 330-348). Currently:

```go
	bubble := in.pendingPositioned
	if establishesStackingContext(b) {
		frag.IsStackingContext = true
		frag.Box = b
		for range in.pendingPositioned {
			frag.PositionedInfo = append(frag.PositionedInfo, PositionedInfo{CBOwned: false})
		}
		frag.Positioned = append(frag.Positioned, in.pendingPositioned...)
		bubble = nil
		for j := deferredBefore; j < len(posCtx.deferred); j++ {
			if posCtx.deferred[j].cb.box == b && posCtx.deferred[j].cb.frag == nil {
				posCtx.deferred[j].cb.frag = frag
			}
		}
	}
```

becomes:

```go
	bubble := in.pendingPositioned
	if establishesStackingContext(b) {
		frag.IsStackingContext = true
		frag.Box = b
		for _, pp := range in.pendingPositioned {
			frag.PositionedInfo = append(frag.PositionedInfo, PositionedInfo{CBOwned: false, ClipChain: pp.clipChain})
			frag.Positioned = append(frag.Positioned, pp.frag)
		}
		bubble = nil
		for j := deferredBefore; j < len(posCtx.deferred); j++ {
			if posCtx.deferred[j].cb.box == b && posCtx.deferred[j].cb.frag == nil {
				posCtx.deferred[j].cb.frag = frag
			}
		}
	} else if frag.Clips {
		// b is a NON-positioned (not a stacking context) overflow≠visible box: any
		// relative descendant bubbling past it is clipped to b's padding box even though
		// it will paint in an ancestor's layer (CSS). Prepend b's clip rect (already
		// computed above as frag.ClipRect) to each still-bubbling entry's chain, so the
		// outermost box ends up first as the entry rises. This is the relative-clip-escape
		// fix; the abs/fixed intervening-clip analogue is deferred to 6b.
		grown := make([]pendingPos, len(bubble))
		for i, pp := range bubble {
			grown[i] = pendingPos{frag: pp.frag, clipChain: prependRect(frag.ClipRect, pp.clipChain)}
		}
		bubble = grown
	}
```

Add the small helper (near the other rect helpers, or just below this function):

```go
// prependRect returns a new slice with r at the front of chain (outermost-first order).
// A fresh slice is returned so sibling entries do not alias one backing array.
func prependRect(r rect, chain []rect) []rect {
	out := make([]rect, 0, len(chain)+1)
	out = append(out, r)
	out = append(out, chain...)
	return out
}
```

- [ ] **Step 6: Update the three other consumers to the new element type**

These now consume `[]pendingPos` instead of `[]*Fragment`:

(a) `layoutTree` (~lines 92-95). Currently:

```go
		for range res.pendingPositioned {
			res.frag.PositionedInfo = append(res.frag.PositionedInfo, PositionedInfo{CBOwned: false})
		}
		res.frag.Positioned = append(res.frag.Positioned, res.pendingPositioned...)
```

becomes:

```go
		for _, pp := range res.pendingPositioned {
			res.frag.PositionedInfo = append(res.frag.PositionedInfo, PositionedInfo{CBOwned: false, ClipChain: pp.clipChain})
			res.frag.Positioned = append(res.frag.Positioned, pp.frag)
		}
```

(b) `placeFloat` (~lines 635-638). Currently:

```go
	for range res.pendingPositioned {
		res.frag.PositionedInfo = append(res.frag.PositionedInfo, PositionedInfo{CBOwned: false})
	}
	res.frag.Positioned = append(res.frag.Positioned, res.pendingPositioned...)
```

becomes:

```go
	for _, pp := range res.pendingPositioned {
		res.frag.PositionedInfo = append(res.frag.PositionedInfo, PositionedInfo{CBOwned: false, ClipChain: pp.clipChain})
		res.frag.Positioned = append(res.frag.Positioned, pp.frag)
	}
```

> Note: a float's interior relatives were already moved by `translateFragment(res.frag, …)`. Their `clipChain` rects, however, are in the float's pre-translation local page frame. In practice a relative descendant of a float that ALSO bubbles out of a non-positioned clip box inside the float is a rare nesting; the chain rects come from `frag.ClipRect` which is in the same page frame the translate later shifts. **Known limitation (document, do not fix here):** a clip chain captured inside a float is not re-translated by `placeFloat`'s `translateFragment` (that call predates the chain). This matches the existing 5b/5c deferral "a float inside a position:relative box not riding the relative paint offset" family. Add a one-line `e.logf` if a non-empty chain is seen here, so it is visible:

```go
	for _, pp := range res.pendingPositioned {
		if len(pp.clipChain) > 0 {
			e.logf("css layout: clip chain on a relative descendant inside a float is not re-translated (approximate)")
		}
		res.frag.PositionedInfo = append(res.frag.PositionedInfo, PositionedInfo{CBOwned: false, ClipChain: pp.clipChain})
		res.frag.Positioned = append(res.frag.Positioned, pp.frag)
	}
```

(c) `resolveAbsolute` does NOT consume `pendingPositioned` (it consumes `posCtx.deferred`); its append site is unchanged from Task 2 (it appends a single `PositionedInfo{CBOwned: cbOwned}` with no chain — the abs intervening-clip chain is the 6b deferral).

- [ ] **Step 7: Run the escape test**

Run: `go test ./pkg/layout/css -run TestRelativeChildOfNonPositionedClipIsClipped -v`
Expected: PASS — the child background now falls between ClipPush and ClipPop.

- [ ] **Step 8: Re-run all z-index tests + the corpus byte-identical guard**

Run: `go test ./pkg/layout/css ./pkg/doctaculous`
Expected: PASS. Then:

Run: `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata`
Expected: NO output — the chain is empty for every existing page (no relative-under-non-positioned-clip case in the corpus), so all goldens/reftests stay byte-identical.

- [ ] **Step 9: Add the two remaining unit-test cases (z-inside-clip CB-owned, byte-identical all-auto)**

These pin spec test cases #5 and #6 at the item-stream level (the z-inside-clip and z-indexed-float combinations are *also* golden-tested in Task 5; this gives the clip∘z case a precise numeric assertion). First add `"github.com/nathanstitt/doctaculous/pkg/layout"` to the import block of `pkg/layout/css/zindex_layout_test.go` (the byte-identical test below uses `[]layout.Item`). Then append:

```go
// TestZIndexInsideClipOrdersWithinBracket: two absolutely-positioned boxes whose
// containing block IS an overflow:hidden box paint INSIDE the clip bracket, ordered by
// z-index (the z:2 box after the z:1 box, both between ClipPush and ClipPop).
func TestZIndexInsideClipOrdersWithinBracket(t *testing.T) {
	eng := New(nil, nil, nil)
	underBG := color.RGBA{0, 0, 60, 255}
	overBG := color.RGBA{60, 0, 0, 255}

	under := posBox(zfill(60, underBG, 10, 10, 1, false), cssbox.PosAbsolute)
	over := posBox(zfill(60, overBG, 30, 30, 2, false), cssbox.PosAbsolute)
	clipStyle := posStyle() // position:relative + overflow:hidden => the abs boxes' CB and a clip
	clipStyle.Width = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	clipStyle.Height = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	clipStyle.Overflow = "hidden"
	clip := posBox(clipStyle, cssbox.PosRelative, under, over)
	root := posBox(posStyle(), cssbox.PosStatic, clip)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	push, pop := clipBoundsReal(items)
	ui, oi := bgIndex(items, underBG), bgIndex(items, overBG)
	if push < 0 || pop < 0 || ui < 0 || oi < 0 {
		t.Fatalf("missing items: push=%d pop=%d under=%d over=%d", push, pop, ui, oi)
	}
	// Both inside the bracket, and z:2 (over) after z:1 (under). De-Morgan'd condition
	// (golangci-lint QF1001 forbids if !(a && b)).
	if ui <= push || ui >= pop || oi <= push || oi >= pop {
		t.Errorf("both abs boxes must paint inside the clip bracket: under=%d over=%d (push=%d pop=%d)", ui, oi, push, pop)
	}
	if oi <= ui {
		t.Errorf("z:2 box at %d must paint after z:1 box at %d", oi, ui)
	}
}

// TestZIndexAllAutoByteIdentical: a page whose positioned boxes are ALL z:auto produces
// the SAME item stream regardless of the sort (the stable sort is the identity on equal
// keys). Asserted by comparing the all-auto stream to the stream with the boxes given
// EXPLICIT z-index:0 (also the middle band, same document order) — they must be equal,
// proving auto and explicit-0 both land in document order with no reordering.
func TestZIndexAllAutoByteIdentical(t *testing.T) {
	eng := New(nil, nil, nil)
	build := func(z int, auto bool) []layout.Item {
		a := posBox(zfill(40, color.RGBA{1, 0, 0, 255}, 0, 0, z, auto), cssbox.PosRelative)
		b := posBox(zfill(40, color.RGBA{0, 1, 0, 255}, 5, 5, z, auto), cssbox.PosRelative)
		c := posBox(zfill(40, color.RGBA{0, 0, 1, 255}, 10, 10, z, auto), cssbox.PosRelative)
		root := posBox(posStyle(), cssbox.PosStatic, a, b, c)
		return eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	}
	autoItems := build(0, true)
	zeroItems := build(0, false)
	if len(autoItems) != len(zeroItems) {
		t.Fatalf("item count differs: auto=%d zero=%d", len(autoItems), len(zeroItems))
	}
	for i := range autoItems {
		if autoItems[i].Kind != zeroItems[i].Kind {
			t.Errorf("item %d kind differs: auto=%v zero=%v", i, autoItems[i].Kind, zeroItems[i].Kind)
		}
	}
}
```

Run: `go test ./pkg/layout/css -run 'TestZIndexInsideClipOrdersWithinBracket|TestZIndexAllAutoByteIdentical' -v`
Expected: PASS.

- [ ] **Step 10: Race + lint**

Run (sandbox disabled): `go test -race ./pkg/layout/css ./pkg/doctaculous && gofmt -l pkg/layout/css && go vet ./pkg/layout/css/... && golangci-lint run ./pkg/layout/css/...`
Expected: all clean.

- [ ] **Step 11: Commit**

```bash
git add pkg/layout/css/block.go pkg/layout/css/zindex_layout_test.go
git commit -m "css/layout: clip a relative descendant escaping a non-positioned overflow box"
```

---

## Task 5: Golden images + WPT reftests

Eyeball-able coverage of the four flag combinations plus reftest equivalences. Author goldens with visibly overlapping boxes of different z so the order is unambiguous.

**Files:**
- Modify: `pkg/doctaculous/html_golden_test.go` (4 entries in `htmlGoldens`)
- Create: `pkg/doctaculous/testdata/golden/html-zindex-{negative,stack,clip,float}.png` (via `-update`)
- Modify: `pkg/doctaculous/wpt_reftest_test.go` (3 entries in `wptReftests`)
- Create: `pkg/doctaculous/testdata/wpt/css21-normal-flow/{zindex-negative,zindex-order,relative-clip-escape}{,-ref}.html`

- [ ] **Step 1: Add the 4 golden fixtures**

In `pkg/doctaculous/html_golden_test.go`, add these entries to the `htmlGoldens` slice (before the closing `}` of the slice literal). Each uses `body{margin:0}` per the file's convention:

```go
	{
		// Negative z-index: a box with z-index:-1 paints BEHIND in-flow content. The
		// in-flow green block overlaps the (red) negative box; green must cover red.
		name:       "zindex-negative",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .neg { position: relative; z-index: -1; width: 120px; height: 120px; background: #cc2222; }
  .flow { width: 120px; height: 60px; background: #22aa22; margin-top: -60px; }
</style></head><body>
  <div class="neg"></div>
  <div class="flow"></div>
</body></html>`,
	},
	{
		// Positive z-index ordering: three overlapping absolutely-positioned boxes with
		// z-index 1/2/3; the higher z paints on top. Blue(3) over green(2) over red(1).
		name:       "zindex-stack",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .wrap { position: relative; height: 160px; }
  .box { position: absolute; width: 90px; height: 90px; }
  .r { left: 10px;  top: 10px;  background: #cc2222; z-index: 1; }
  .g { left: 40px;  top: 40px;  background: #22aa22; z-index: 2; }
  .b { left: 70px;  top: 70px;  background: #2244cc; z-index: 3; }
</style></head><body>
  <div class="wrap">
    <div class="box r"></div>
    <div class="box g"></div>
    <div class="box b"></div>
  </div>
</body></html>`,
	},
	{
		// z-index inside a clip: an absolutely-positioned z-index box whose containing
		// block is an overflow:hidden box is clipped to that box AND ordered by z against
		// the clip's other content. The orange box spills past the clip edge but is cut.
		name:       "zindex-clip",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .clip { position: relative; overflow: hidden; width: 100px; height: 100px; background: #dddddd; }
  .under { position: absolute; left: 10px; top: 10px; width: 80px; height: 80px; background: #2244cc; z-index: 1; }
  .over  { position: absolute; left: 40px; top: 40px; width: 120px; height: 120px; background: #ee8822; z-index: 2; }
</style></head><body>
  <div class="clip">
    <div class="under"></div>
    <div class="over"></div>
  </div>
</body></html>`,
	},
	{
		// z-index ∘ float: a left float (step 4) and a positive-z positioned box (step 7)
		// overlap; the positioned box paints OVER the float per Appendix E.
		name:       "zindex-float",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .wrap { position: relative; height: 140px; }
  .fl { float: left; width: 90px; height: 90px; background: #22aa22; }
  .ov { position: absolute; left: 40px; top: 30px; width: 90px; height: 90px; background: #cc2222; z-index: 1; }
</style></head><body>
  <div class="wrap">
    <div class="fl"></div>
    <div class="ov"></div>
  </div>
</body></html>`,
	},
```

- [ ] **Step 2: Generate the goldens**

Run (sandbox disabled): `go test ./pkg/doctaculous -run TestHTMLGolden -update`
Expected: PASS; 4 new PNGs created under `pkg/doctaculous/testdata/golden/`.

- [ ] **Step 3: Eyeball every new golden** (the CONTROLLER does this via the Read tool — the implementer subagent has no image vision; the implementer reports the files are generated and STOPS for review)

The controller Reads each PNG and confirms:
- `html-zindex-negative.png` — the green block visibly COVERS the red box where they overlap (negative-z behind).
- `html-zindex-stack.png` — blue on top of green on top of red, in that stacking order.
- `html-zindex-clip.png` — the orange box is CUT at the clip box's edge (not spilling), and over the blue box where they overlap.
- `html-zindex-float.png` — the red positioned box paints OVER the green float where they overlap.

If any is wrong, the ordering is wrong — fix Task 3/4 before proceeding.

- [ ] **Step 4: Confirm no pre-existing golden changed**

Run: `git status --short pkg/doctaculous/testdata/golden`
Expected: ONLY the 4 new `html-zindex-*.png` as added files; no existing PNG modified.

- [ ] **Step 5: Add the 3 WPT reftest pairs**

Create each pair under `pkg/doctaculous/testdata/wpt/css21-normal-flow/`.

`zindex-negative.html`:

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .neg { position: relative; z-index: -1; width: 120px; height: 120px; background: #3366cc; }
  .flow { width: 120px; height: 60px; background: #cc3333; margin-top: -60px; }
</style></head><body>
  <div class="neg"></div>
  <div class="flow"></div>
</body></html>
```

`zindex-negative-ref.html` (no z-index; document order already paints the red block last/on top):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .neg { width: 120px; height: 120px; background: #3366cc; }
  .flow { width: 120px; height: 60px; background: #cc3333; margin-top: -60px; }
</style></head><body>
  <div class="neg"></div>
  <div class="flow"></div>
</body></html>
```

`zindex-order.html` (z-index inverts document order: the first box has higher z, so it paints on top):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .wrap { position: relative; height: 140px; }
  .box { position: absolute; width: 100px; height: 100px; }
  .top    { left: 0;   top: 0;   background: #3366cc; z-index: 2; }
  .bottom { left: 30px; top: 30px; background: #cc3333; z-index: 1; }
</style></head><body>
  <div class="wrap">
    <div class="box top"></div>
    <div class="box bottom"></div>
  </div>
</body></html>
```

`zindex-order-ref.html` (document order matches the z-order — bottom first, top last — so no sort is needed to get the same pixels):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .wrap { position: relative; height: 140px; }
  .box { position: absolute; width: 100px; height: 100px; }
  .bottom { left: 30px; top: 30px; background: #cc3333; }
  .top    { left: 0;   top: 0;   background: #3366cc; }
</style></head><body>
  <div class="wrap">
    <div class="box bottom"></div>
    <div class="box top"></div>
  </div>
</body></html>
```

`relative-clip-escape.html` (a relative child offset past a non-positioned overflow:hidden box's edge — must be clipped):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .clip { overflow: hidden; width: 80px; height: 80px; background: #dddddd; }
  .child { position: relative; left: 30px; top: 30px; width: 90px; height: 90px; background: #cc3333; }
</style></head><body>
  <div class="clip">
    <div class="child"></div>
  </div>
</body></html>
```

`relative-clip-escape-ref.html` (the child authored already clipped: the visible portion is the part inside the 80×80 box from (30,30) — a 50×50 red square at (30,30) on the grey box):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .clip { width: 80px; height: 80px; background: #dddddd; position: relative; }
  .visible { position: absolute; left: 30px; top: 30px; width: 50px; height: 50px; background: #cc3333; }
</style></head><body>
  <div class="clip">
    <div class="visible"></div>
  </div>
</body></html>
```

- [ ] **Step 6: Register the reftests**

In `pkg/doctaculous/wpt_reftest_test.go`, add to the `wptReftests` slice (before its closing `}`):

```go
	{"zindex-negative", 200, "a negative z-index box paints behind in-flow content (== the boxes authored in paint order)", nil},
	{"zindex-order", 200, "z-index inverts document paint order (== the boxes authored in z-order)", nil},
	{"relative-clip-escape", 200, "a relative child of a non-positioned overflow:hidden box is clipped to it (== the visible portion authored to fit)", nil},
```

- [ ] **Step 7: Run the reftests**

Run (sandbox disabled): `go test ./pkg/doctaculous -run TestWPTReftests -v`
Expected: PASS for all three new pairs (test and reference rasterize identically).

> If `relative-clip-escape` fails by a few pixels at the clip seam, check the child's used size against the clip box — the reference's 50×50 visible square assumes the child's border box starts at (30,30) inside an 80×80 box (so 80−30 = 50 visible per axis). Adjust the reference rectangle to match the engine's exact clip if anti-aliasing at the edge differs, but the shapes must coincide.

- [ ] **Step 8: Full suite + lint**

Run (sandbox disabled): `go test ./pkg/doctaculous && gofmt -l pkg/doctaculous && golangci-lint run ./pkg/doctaculous/...`
Expected: all clean.

- [ ] **Step 9: Commit**

```bash
git add pkg/doctaculous/html_golden_test.go pkg/doctaculous/wpt_reftest_test.go pkg/doctaculous/testdata/golden/html-zindex-*.png pkg/doctaculous/testdata/wpt/css21-normal-flow/zindex-*.html pkg/doctaculous/testdata/wpt/css21-normal-flow/relative-clip-escape*.html
git commit -m "doctaculous: z-index + relative-clip-escape goldens and WPT reftests"
```

---

## Task 6: Final verification + CLAUDE.md update

Whole-repo verification (race, lint, full corpus) and flip the documentation degradation notes.

**Files:**
- Modify: `CLAUDE.md` (the positioning + overflow Done bullets)

- [ ] **Step 1: Whole-repo test + race**

Run (sandbox disabled): `go test ./... && go test -race ./pkg/layout/... ./pkg/doctaculous/...`
Expected: PASS. (A `-race` run on the touched packages proves the sort-a-local-copy decision keeps flatten race-free.)

- [ ] **Step 2: Whole-tree lint + gofmt**

Run (sandbox disabled): `gofmt -l pkg/css pkg/layout pkg/doctaculous && go vet ./... && golangci-lint run ./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...`
Expected: no output.

- [ ] **Step 3: Confirm no stray throwaway files and a clean tree**

Run: `find . -name 'zz_*' -print` (expected: nothing) and `git status --short` (expected: only the intended changes; no untracked scratch).

- [ ] **Step 4: Update CLAUDE.md — flip the z-index degradation notes**

In `CLAUDE.md`, in the **HTML rendering — positioning** Done bullet, change the degradation note. Find:

```
Degrades gracefully: `z-index` is parsed but **not yet sorted on** (positioned boxes paint in document
order — full Appendix E negative/numeric z-index ordering deferred);
```

Replace with:

```
`z-index` is now honored: full Appendix E negative/numeric ordering within a stacking context
(positioned boxes sorted by (z-index, document order); negatives paint behind in-flow content);
```

In the **HTML rendering — overflow clipping** Done bullet, find the deferred-gap note:

```
a **`position:relative` (or other positioned) descendant of a *non-positioned* `overflow:hidden` box
escapes the clip** (such a box is a BFC but not a stacking context, so the descendant bubbles to a higher
positioned layer — browsers clip it; deferred with full z-index);
```

Replace with:

```
a **`position:relative` descendant of a *non-positioned* `overflow:hidden` box is now clipped** to that
box (its clip rides the descendant's bubble to the ancestor's positioned layer); the *absolute/fixed*
intervening-clip sub-case (a clip box strictly between an abs box and a higher containing block) remains
deferred to a follow-up (6b);
```

In the **TODO §6** list, remove the now-done z-index clause and narrow it to the 6b remnant. The current text (CLAUDE.md ~lines 328-336) reads:

```
parse/layout slice with its own fixtures + golden/WPT tests: **full z-index stacking** (negative
`z-index` painting behind in-flow content, and numeric `z-index` ordering within a stacking context —
`z-index` is parsed now; the minimal stacking pass paints positioned boxes in document order, and the
positioned-layer loop in `AppendItems` is the seam this slice re-points at a z-sorted, sub-layered
order; **also fold in here**: clipping a `position:relative`/positioned descendant of a *non-positioned*
`overflow:hidden` box — today such a descendant bubbles past the clip to a higher positioned layer and
paints unclipped, because a non-positioned clipping box is a BFC but not a stacking context; the fix
needs the clip to reach an item range an ancestor's positioned phase emits, the same machinery this
slice reworks); **tables**; **web fonts** (`@font-face` + WOFF/WOFF2); **flexbox** then **grid** (today
```

Replace that span (from "parse/layout slice" through the line ending "**flexbox** then **grid** (today") with:

```
parse/layout slice with its own fixtures + golden/WPT tests: **the abs/fixed intervening-clip escape**
(sub-project 6b — full z-index stacking and the *relative* clip-escape landed; the remaining gap is an
`absolute`/`fixed` descendant whose containing block is an ancestor *beyond* an intervening
`overflow:hidden` box, which still paints unclipped by that box — the `ClipChain` flatten machinery
exists, but capturing the chain for an abs box needs clip-ancestor threading through layout that the
relative path did not; see `docs/superpowers/HANDOVER-subproject-6b-abs-clip-escape.md`); **tables**;
**web fonts** (`@font-face` + WOFF/WOFF2); **flexbox** then **grid** (today
```

(Keep the rest of the §6 list — OpenURL, pagination, EPUB, and the positioning/replaced/inline follow-ups — unchanged.)

Add a new **Done** bullet summarizing this slice (mirroring the style of the existing HTML bullets), e.g.:

```
- **HTML rendering — full z-index stacking** (`pkg/layout/css/fragment.go` sort/bands, `block.go`
  clip-chain bubble; covered by `zindex_layout_test.go` item-stream order tests, the `html-zindex-*`
  goldens, and the `zindex-negative`/`zindex-order`/`relative-clip-escape` WPT reftests): the positioned
  layer is z-sorted into CSS 2.1 Appendix E bands — negative-z paints **behind** in-flow content,
  z:auto/0 in document order, positive-z last — via a stable `(z-index, document-order)` sort
  (`sortedPositioned`/`appendBand`), with all-auto pages byte-identical to the prior document-order pass.
  Folds in the relative-clip-escape fix: a `position:relative` descendant of a *non-positioned*
  `overflow:hidden` box is clipped to that box even though it paints in an ancestor's positioned layer
  (the clip rect rides the descendant's bubble as a `ClipChain`). The `Fragment` now retains its source
  `cssbox.Box` (the z-index source, read at flatten time). Deferred to 6b: the *absolute/fixed*
  intervening-clip sub-case (needs new clip-ancestor threading through layout). See
  `docs/superpowers/specs/2026-06-25-html-zindex-design.md`.
```

- [ ] **Step 5: Commit the docs**

```bash
git add CLAUDE.md
git commit -m "docs: record full z-index stacking in CLAUDE.md (Done + 6b deferral)"
```

- [ ] **Step 6: Write the 6b handover**

Create `docs/superpowers/HANDOVER-subproject-6b-abs-clip-escape.md` capturing the one deferred item: the abs/fixed intervening-clip sub-case. It must note: the flatten machinery (`ClipChain` on `PositionedInfo`, `appendBand`'s bracket loop) already exists and is reused; 6b only adds threading a clip-ancestor stack through `layoutBlock` → `layoutInterior` → `layoutBlockChildren`, captured into `deferredAbs`, written into the holder's `PositionedInfo[i].ClipChain` in `resolveAbsolute`. Reference the adversarial test pattern (a `position:absolute` child whose CB is an ancestor beyond an intervening `overflow:hidden` box, offset past the clip). Mirror the structure of `HANDOVER-subproject-6-zindex.md`.

```bash
git add docs/superpowers/HANDOVER-subproject-6b-abs-clip-escape.md
git commit -m "docs: handover for sub-project 6b (abs/fixed intervening-clip escape)"
```

- [ ] **Step 7: Holistic final review**

Dispatch a holistic review on the assembled diff (`git diff main...feat/html-zindex`, or the merge-base of the stack) per the project's two-stage + holistic review process. Have the reviewer verify: (1) the byte-identical guard (no existing golden/reftest changed); (2) the load-bearing negative-before-decorations order; (3) the relative-clip-escape clipping; (4) no `//nolint`, no `slices.*`, De-Morgan'd conditions; (5) the abs intervening-clip case is correctly and visibly deferred (no false claim of support). Reviewers probing with throwaway tests must delete them and confirm `git status` clean.

- [ ] **Step 8: Finish the branch**

Per `superpowers:finishing-a-development-branch`: open PR #9 `feat/html-zindex` → `feat/html-overflow` (or retarget up the stack as lower PRs merge). Keep the PR description short; do not credit Claude. Note: pushes over HTTPS need the sandbox disabled.

---

## Notes carried into execution

- **The byte-identical guard is the single most important check** (Steps 3.10, 4.8): if any existing golden/reftest changes, the sort or band split broke the all-auto identity. The stable sort + the empty-band reduction is what protects the corpus.
- **Eyeball every new golden** (Step 5.3) — a "passing" golden can still be visually wrong; only the controller (Read tool) can confirm the stacking order.
- **The abs intervening-clip case is OUT of scope** — do not thread clip-ancestor state through layout for it; that is 6b. The relative case (rides existing bubbling) is in scope.
- **Test flag COMBINATIONS** (Task 3/4 cases): negative-behind-content, positive-over-auto, neg<auto<pos, stable-within-band, z-inside-clip (via the golden), z-indexed-float (via the golden), relative-clip-escape.
