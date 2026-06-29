# HTML Overflow Clipping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `overflow: hidden/scroll/auto` clipping for the HTML CSS layout engine, plus the two float interactions it unlocks — a BFC enclosing its floats' height, and a BFC sibling shortening past an outer float.

**Architecture:** Add `Overflow` to the CSS cascade (not inherited); `overflow≠visible` establishes a BFC (the trigger for both float behaviors). Clipping is expressed as two new flat-stream `layout.Item` kinds (`ClipPushKind`/`ClipPopKind`) emitted by `Fragment.AppendItems` around a clipping fragment's contents, which `PaintPage` maps onto the painter's existing `Save`/`PushClip`/`Restore` clip stack. Float-height enclosure folds `floatContext.maxBottom()` into a BFC box's content height; sibling avoidance shifts/narrows a BFC child past an outer float band.

**Tech Stack:** Go (pure, no CGo). Packages: `pkg/css` (cascade), `pkg/layout` (Item types), `pkg/layout/paint` (painter), `pkg/layout/css` (the CSS layout engine: `block.go`, `floats.go`, `fragment.go`), `pkg/doctaculous` (goldens + WPT reftests). `pkg/render/raster` clip stack is reused unchanged.

**Spec:** `docs/superpowers/specs/2026-06-24-html-overflow-design.md`

---

## Conventions (read before every task)

- **You are on branch `feat/html-overflow`.** Do NOT checkout/stash/switch branches. Do NOT create `zz_*` scratch files; if you make a throwaway test, delete it before finishing and confirm `git status` is clean.
- **Sandbox blocks the Go build cache + TLS.** Run every `go test`/`go build`/`gofmt`/`golangci-lint` command with the sandbox **disabled** (`dangerouslyDisableSandbox: true`). `git commit` is fine sandboxed; `git push` needs sandbox disabled (HTTPS remote).
- **Editor diagnostics LAG.** Trust `go build`/`go test`, not the panel's stale "undefined"/"unused"/"redeclared" errors.
- **The repo declines all "modernize" hints** (`max()`/`min()`/`slices.*`/range-over-int). Keep explicit `if x < y { x = y }` clamps and indexed `for i := 0; i < n; i++` loops. **No `//nolint`.** golangci-lint flags `if !(a && b)` (write `if a>=b || ...`) and any **unused** unexported field/func — every new field/func must be read/called in the same PR.
- **The zero-value `Length` trap.** A `cssbox.ComputedStyle`/`Box` struct literal that omits `Width`/`Height`/`MaxWidth`/`MaxHeight`/offsets reads as an explicit `0`, NOT auto/none. Use the `blockBox()` helper (`floats_layout_test.go:247`) for block fixtures and `posBox()`/`posStyle()` (`positioning_layout_test.go:27`) for positioned fixtures — they default these to auto. A raw literal must set them explicitly.
- **Per-task loop (subagent-driven):** write failing test → run it (see it fail) → implement → run it (see it pass) → run the package's full test suite → `gofmt -l` the changed files → commit. After each task the controller runs a spec-review + code-quality review.

---

## Task 1: Add `overflow` to the CSS cascade

**Files:**
- Modify: `pkg/css/cascade.go` (add field ~line 71; initial value ~line 275; parse case ~line 369)
- Test: `pkg/css/cascade_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `pkg/css/cascade_test.go`:

```go
// TestApplyOverflow: each valid overflow keyword is accepted; an invalid one is
// dropped (the prior value is preserved).
func TestApplyOverflow(t *testing.T) {
	for _, kw := range []string{"visible", "hidden", "scroll", "auto"} {
		cs := initialStyle()
		applyDeclaration(&cs, Declaration{Property: "overflow", Value: kw})
		if cs.Overflow != kw {
			t.Errorf("overflow %q not applied, got %q", kw, cs.Overflow)
		}
	}
	cs := initialStyle()
	cs.Overflow = "hidden"
	applyDeclaration(&cs, Declaration{Property: "overflow", Value: "clip"}) // unsupported
	if cs.Overflow != "hidden" {
		t.Errorf("overflow after invalid keyword = %q, want hidden preserved", cs.Overflow)
	}
}

// TestOverflowInitialVisible: the initial value is "visible".
func TestOverflowInitialVisible(t *testing.T) {
	if cs := initialStyle(); cs.Overflow != "visible" {
		t.Errorf("initial overflow = %q, want visible", cs.Overflow)
	}
}

// TestOverflowNotInherited: overflow is not inherited (a child of an overflow:hidden
// parent computes "visible").
func TestOverflowNotInherited(t *testing.T) {
	parent := initialStyle()
	parent.Overflow = "hidden"
	child := inheritFrom(parent)
	if child.Overflow != "visible" {
		t.Errorf("child overflow = %q, want visible (not inherited)", child.Overflow)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run (sandbox disabled): `go test ./pkg/css -run 'TestApplyOverflow|TestOverflowInitialVisible|TestOverflowNotInherited' -v`
Expected: compile error (`cs.Overflow` undefined) — that counts as failing.

- [ ] **Step 3: Add the `Overflow` field**

In `pkg/css/cascade.go`, in the `ComputedStyle` struct, right after the `ObjectFit string` block (~line 71), add:

```go
	// Overflow is the CSS overflow shorthand: "visible" (default) | "hidden" |
	// "scroll" | "auto". Not inherited. overflow≠visible establishes a block
	// formatting context and clips the box's content to its padding box. In this
	// no-scrollbars single-tall-page model, scroll/auto clip exactly like hidden
	// (there is no scroll position or scrollbar chrome). overflow-x/overflow-y are
	// not modeled (a single shorthand value suffices since every clip keyword clips
	// identically here).
	Overflow string
```

- [ ] **Step 4: Set the initial value**

In `initialStyle()` (~line 275), after the `ObjectFit:   "fill",` line, add:

```go
		Overflow:    "visible", // CSS initial overflow
```

- [ ] **Step 5: Add the parse case**

In `applyDeclaration`, after the `case "object-fit":` block (~line 373, before `case "float":`), add:

```go
	case "overflow":
		switch d.Value {
		case "visible", "hidden", "scroll", "auto":
			cs.Overflow = d.Value
		}
```

(Do NOT add `Overflow` to `inheritFrom` — it is not inherited.)

- [ ] **Step 6: Run tests to verify they pass**

Run (sandbox disabled): `go test ./pkg/css -run 'TestApplyOverflow|TestOverflowInitialVisible|TestOverflowNotInherited' -v`
Expected: PASS. Then `go test ./pkg/css` (whole package) — PASS.

- [ ] **Step 7: gofmt + commit**

```bash
gofmt -l pkg/css/cascade.go pkg/css/cascade_test.go   # expect no output
git add pkg/css/cascade.go pkg/css/cascade_test.go
git commit -m "css: parse overflow (visible/hidden/scroll/auto), not inherited"
```

---

## Task 2: `clips()` predicate + `overflow≠visible` establishes a BFC

**Files:**
- Modify: `pkg/layout/css/block.go` (`establishesNewBFC` ~line 749; add `clips` helper near it)
- Test: `pkg/layout/css/block_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `pkg/layout/css/block_test.go`:

```go
// TestClipsPredicate: clips() is true for overflow hidden/scroll/auto, false for
// visible and the empty string.
func TestClipsPredicate(t *testing.T) {
	mk := func(ov string) *cssbox.Box {
		return &cssbox.Box{Style: gcss.ComputedStyle{Overflow: ov}}
	}
	for _, ov := range []string{"hidden", "scroll", "auto"} {
		if !clips(mk(ov)) {
			t.Errorf("clips(overflow:%q) = false, want true", ov)
		}
	}
	for _, ov := range []string{"visible", ""} {
		if clips(mk(ov)) {
			t.Errorf("clips(overflow:%q) = true, want false", ov)
		}
	}
}

// TestOverflowEstablishesBFC: a box with overflow≠visible establishes a new BFC.
func TestOverflowEstablishesBFC(t *testing.T) {
	if !establishesNewBFC(&cssbox.Box{Style: gcss.ComputedStyle{Overflow: "hidden"}}) {
		t.Errorf("overflow:hidden does not establish a BFC, want it to")
	}
	if establishesNewBFC(&cssbox.Box{Style: gcss.ComputedStyle{Overflow: "visible"}}) {
		t.Errorf("overflow:visible establishes a BFC, want it not to")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestClipsPredicate|TestOverflowEstablishesBFC' -v`
Expected: compile error (`clips` undefined) — failing.

- [ ] **Step 3: Add the `clips` helper**

In `pkg/layout/css/block.go`, immediately above `establishesNewBFC` (~line 741), add:

```go
// clips reports whether b clips its overflow (CSS overflow ≠ visible). hidden,
// scroll, and auto all clip in this model (scroll/auto have no scroll affordance in
// the single-tall-page model, so they clip exactly like hidden). A clipping box also
// establishes a block formatting context (see establishesNewBFC).
func clips(b *cssbox.Box) bool {
	return b.Style.Overflow != "" && b.Style.Overflow != "visible"
}
```

- [ ] **Step 4: Extend `establishesNewBFC`**

In `establishesNewBFC` (~line 749), change the final `return` so a clipping box qualifies. The current body is:

```go
	if b.Position == cssbox.PosAbsolute || b.Position == cssbox.PosFixed {
		return true
	}
	return b.Display == cssbox.DisplayInlineBlock || b.Float != cssbox.FloatNone
```

Replace the last line with:

```go
	return b.Display == cssbox.DisplayInlineBlock || b.Float != cssbox.FloatNone || clips(b)
```

Also update the doc comment of `establishesNewBFC` (~line 741-748): change the clause "overflow≠visible — the other BFC trigger — is not modeled yet" to "an overflow≠visible box does (it clips its content, which requires a BFC)".

- [ ] **Step 5: Run tests to verify they pass**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestClipsPredicate|TestOverflowEstablishesBFC' -v`
Expected: PASS. Then `go test ./pkg/layout/css` — PASS (no existing test should regress; an overflow:hidden box was never used in existing fixtures).

- [ ] **Step 6: gofmt + commit**

```bash
gofmt -l pkg/layout/css/block.go pkg/layout/css/block_test.go   # expect no output
git add pkg/layout/css/block.go pkg/layout/css/block_test.go
git commit -m "css/layout: overflow≠visible establishes a BFC (clips predicate)"
```

---

## Task 3: `ClipPushKind`/`ClipPopKind` item types + painter support

**Files:**
- Modify: `pkg/layout/page.go` (add two `ItemKind` constants ~line 97)
- Modify: `pkg/layout/paint/paint.go` (add two cases to `PaintPage`'s switch ~line 40)
- Test: `pkg/layout/paint/paint_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/paint/paint_test.go` (the `recordDevice` there already records `Fill` calls; extend it to count Save/Restore/PushClip). First, add Save/Restore/PushClip counters to the existing `recordDevice` struct and methods in that file:

In `paint_test.go`, change the `recordDevice` struct to add counters and the three methods to record them:

```go
type recordDevice struct {
	glyphs   []recordedGlyph
	fills    []recordedFill
	saves    int
	restores int
	clips    []*render.Path
}
```

and replace its `PushClip`/`Save`/`Restore` methods with:

```go
func (d *recordDevice) PushClip(p *render.Path, _ render.FillRule) { d.clips = append(d.clips, p) }
func (d *recordDevice) Save()                                      { d.saves++ }
func (d *recordDevice) Restore()                                   { d.restores++ }
```

Then add the test:

```go
// TestPaintClipPushPop: a ClipPushKind item drives Save()+PushClip(rect); a
// ClipPopKind drives Restore(). The pushed clip rect's corners map through the page
// matrix (here a 1:1 scale), so a 10,20,30,40 clip becomes a path at those coords.
func TestPaintClipPushPop(t *testing.T) {
	page := &layout.Page{
		WidthPt: 100, HeightPt: 100,
		Items: []layout.Item{
			{Kind: layout.ClipPushKind, Rule: layout.RuleItem{XPt: 10, YPt: 20, WPt: 30, HPt: 40}},
			{Kind: layout.GlyphKind, Glyph: layout.GlyphItem{Outline: triangle(), XPt: 12, YPt: 22, SizePt: 4, Color: color.RGBA{A: 0xff}}},
			{Kind: layout.ClipPopKind},
		},
	}
	dev := &recordDevice{}
	PaintPage(dev, page, render.Scale(1, 1))

	if dev.saves != 1 || dev.restores != 1 {
		t.Errorf("saves=%d restores=%d, want 1/1", dev.saves, dev.restores)
	}
	if len(dev.clips) != 1 {
		t.Fatalf("pushed %d clips, want 1", len(dev.clips))
	}
	// The clip path should be a 4-corner rectangle. Its bounding box must be
	// [10,20]-[40,60] (x+w, y+h) under the 1:1 matrix.
	minX, minY, maxX, maxY := pathBounds(dev.clips[0])
	if minX != 10 || minY != 20 || maxX != 40 || maxY != 60 {
		t.Errorf("clip bounds = (%v,%v)-(%v,%v), want (10,20)-(40,60)", minX, minY, maxX, maxY)
	}
	if len(dev.glyphs) != 1 {
		t.Errorf("painted %d glyphs, want 1 (between push and pop)", len(dev.glyphs))
	}
}

// pathBounds returns the axis-aligned bounding box of a path's MoveTo/LineTo points.
func pathBounds(p *render.Path) (minX, minY, maxX, maxY float64) {
	first := true
	for _, s := range p.Segments {
		if s.Kind != render.MoveTo && s.Kind != render.LineTo {
			continue
		}
		if first {
			minX, minY, maxX, maxY = s.P0.X, s.P0.Y, s.P0.X, s.P0.Y
			first = false
			continue
		}
		if s.P0.X < minX {
			minX = s.P0.X
		}
		if s.P0.Y < minY {
			minY = s.P0.Y
		}
		if s.P0.X > maxX {
			maxX = s.P0.X
		}
		if s.P0.Y > maxY {
			maxY = s.P0.Y
		}
	}
	return
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/paint -run TestPaintClipPushPop -v`
Expected: compile error (`layout.ClipPushKind` undefined) — failing.

- [ ] **Step 3: Add the item kinds**

In `pkg/layout/page.go`, in the `ItemKind` const block, after `ImageKind` (~line 97), add:

```go
	// ClipPushKind pushes a clip rectangle (Item.Rule carries the rect; Color is
	// unused). The painter saves the clip state and intersects the active clip with
	// the rect, so subsequent items paint clipped until the matching ClipPopKind.
	// Emitted by the CSS layout engine for an overflow≠visible box (its padding box).
	// Not a drawing primitive: it carries no color. Pushes and pops are balanced by
	// construction (every push has a matching pop from the same AppendItems call).
	ClipPushKind
	// ClipPopKind pops the most recent clip pushed by ClipPushKind (the painter
	// restores the prior clip state). Carries no geometry.
	ClipPopKind
```

- [ ] **Step 4: Add the painter cases**

In `pkg/layout/paint/paint.go`, in `PaintPage`'s `switch it.Kind` (~line 40, after the `case layout.ImageKind:` arm), add:

```go
		case layout.ClipPushKind:
			// Save the clip state, then intersect the active clip with the rect (mapped
			// through the page matrix). A degenerate rect makes clipRect a no-op push, but
			// Save/Restore still balance, so the stream stays well-formed.
			dev.Save()
			clipRect(dev, mat, it.Rule.XPt, it.Rule.YPt, it.Rule.XPt+it.Rule.WPt, it.Rule.YPt+it.Rule.HPt)
		case layout.ClipPopKind:
			dev.Restore()
```

- [ ] **Step 5: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/paint -run TestPaintClipPushPop -v`
Expected: PASS. Then `go test ./pkg/layout/paint` — PASS (the `image_test.go` cover test still passes; its `imageRecordDevice` counts pushClip separately and is untouched).

- [ ] **Step 6: gofmt + commit**

```bash
gofmt -l pkg/layout/page.go pkg/layout/paint/paint.go pkg/layout/paint/paint_test.go   # expect no output
git add pkg/layout/page.go pkg/layout/paint/paint.go pkg/layout/paint/paint_test.go
git commit -m "layout/paint: ClipPushKind/ClipPopKind drive the painter clip stack"
```

---

## Task 4: `Fragment` clip fields + the clip bracket in `AppendItems`

This is the load-bearing task. It adds the `Clips`/`ClipRect`/`PositionedClip` fields, factors `appendChildDecorations`, and brackets a clipping fragment's contents (and CB-owned positioned subset) with clip items, leaving its own decorations and escaped positioned descendants unclipped.

**Files:**
- Modify: `pkg/layout/css/fragment.go` (fields ~line 69; `AppendItems` ~line 140; `appendDecorations` ~line 197)
- Test: `pkg/layout/css/fragment_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `pkg/layout/css/fragment_test.go`:

```go
// TestAppendItemsClipBracketsContents: a clipping fragment emits ClipPush(padding box)
// AFTER its own background/border and before its children's content, and ClipPop after
// the content. Its own background is OUTSIDE the bracket.
func TestAppendItemsClipBracketsContents(t *testing.T) {
	child := &Fragment{X: 5, Y: 5, W: 90, H: 200, Background: color.RGBA{2, 2, 2, 255}}
	clip := &Fragment{
		X: 0, Y: 0, W: 100, H: 50, Background: color.RGBA{1, 1, 1, 255},
		IsBFC: true, IsStackingContext: true, Clips: true,
		ClipRect: rect{x: 0, y: 0, w: 100, h: 50},
		Children: []*Fragment{child},
	}
	items := clip.AppendItems(nil)
	// Expect: clip bg (own, unclipped), ClipPush, child bg, ClipPop = 4 items.
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4 (own bg, push, child bg, pop)", len(items))
	}
	if items[0].Kind != layout.BackgroundKind || items[0].Rule.Color != (color.RGBA{1, 1, 1, 255}) {
		t.Errorf("item[0] = %v, want clip's own background (outside the bracket)", items[0].Kind)
	}
	if items[1].Kind != layout.ClipPushKind {
		t.Errorf("item[1] = %v, want ClipPushKind", items[1].Kind)
	}
	if items[2].Kind != layout.BackgroundKind || items[2].Rule.Color != (color.RGBA{2, 2, 2, 255}) {
		t.Errorf("item[2] = %v, want child background (inside the bracket)", items[2].Kind)
	}
	if items[3].Kind != layout.ClipPopKind {
		t.Errorf("item[3] = %v, want ClipPopKind", items[3].Kind)
	}
}

// TestAppendItemsClipRectIsPaddingBox: the ClipPush rect carries the fragment's
// ClipRect (its padding box).
func TestAppendItemsClipRectIsPaddingBox(t *testing.T) {
	clip := &Fragment{
		X: 0, Y: 0, W: 100, H: 50, IsBFC: true, IsStackingContext: true, Clips: true,
		ClipRect: rect{x: 5, y: 6, w: 90, h: 38}, // padding box (e.g. 5px borders)
		Children: []*Fragment{{X: 5, Y: 6, W: 90, H: 100, Background: color.RGBA{2, 2, 2, 255}}},
	}
	items := clip.AppendItems(nil)
	var push *layout.Item
	for i := range items {
		if items[i].Kind == layout.ClipPushKind {
			push = &items[i]
			break
		}
	}
	if push == nil {
		t.Fatal("no ClipPushKind emitted")
	}
	if push.Rule.XPt != 5 || push.Rule.YPt != 6 || push.Rule.WPt != 90 || push.Rule.HPt != 38 {
		t.Errorf("clip rect = (%v,%v,%v,%v), want (5,6,90,38)", push.Rule.XPt, push.Rule.YPt, push.Rule.WPt, push.Rule.HPt)
	}
}

// TestAppendItemsNonClippingByteIdentical: a non-clipping fragment emits NO clip items
// (the byte-identical guard for existing pages).
func TestAppendItemsNonClippingByteIdentical(t *testing.T) {
	child := &Fragment{X: 0, Y: 20, W: 100, H: 30, Background: color.RGBA{1, 1, 1, 255}}
	root := &Fragment{X: 0, Y: 0, W: 100, H: 60, Background: color.RGBA{2, 2, 2, 255}, IsBFC: true, IsStackingContext: true, Children: []*Fragment{child}}
	items := root.AppendItems(nil)
	for i := range items {
		if items[i].Kind == layout.ClipPushKind || items[i].Kind == layout.ClipPopKind {
			t.Fatalf("non-clipping fragment emitted a clip item at %d", i)
		}
	}
	if len(items) != 2 {
		t.Errorf("got %d items, want 2 (root bg, child bg) — no clip items", len(items))
	}
}

// TestAppendItemsClipWrapsCBOwnedPositioned: an abs-pos descendant whose CB IS the
// clipping box (PositionedClip[i]=true) paints INSIDE the bracket; one whose CB is
// outside (PositionedClip[i]=false) paints OUTSIDE (after ClipPop).
func TestAppendItemsClipWrapsCBOwnedPositioned(t *testing.T) {
	owned := &Fragment{X: 0, Y: 0, W: 10, H: 10, Background: color.RGBA{7, 7, 7, 255}, IsPositioned: true, IsStackingContext: true}
	escaped := &Fragment{X: 0, Y: 0, W: 10, H: 10, Background: color.RGBA{8, 8, 8, 255}, IsPositioned: true, IsStackingContext: true}
	clip := &Fragment{
		X: 0, Y: 0, W: 100, H: 50, IsBFC: true, IsStackingContext: true, Clips: true,
		ClipRect:       rect{x: 0, y: 0, w: 100, h: 50},
		Positioned:     []*Fragment{owned, escaped},
		PositionedClip: []bool{true, false},
	}
	items := clip.AppendItems(nil)
	// Find the indices of push, pop, owned bg, escaped bg.
	idx := map[string]int{}
	for i := range items {
		switch {
		case items[i].Kind == layout.ClipPushKind:
			idx["push"] = i
		case items[i].Kind == layout.ClipPopKind:
			idx["pop"] = i
		case items[i].Kind == layout.BackgroundKind && items[i].Rule.Color == (color.RGBA{7, 7, 7, 255}):
			idx["owned"] = i
		case items[i].Kind == layout.BackgroundKind && items[i].Rule.Color == (color.RGBA{8, 8, 8, 255}):
			idx["escaped"] = i
		}
	}
	if !(idx["push"] < idx["owned"] && idx["owned"] < idx["pop"]) {
		t.Errorf("owned positioned bg not inside the bracket: push=%d owned=%d pop=%d", idx["push"], idx["owned"], idx["pop"])
	}
	if !(idx["escaped"] > idx["pop"]) {
		t.Errorf("escaped positioned bg not after ClipPop: escaped=%d pop=%d", idx["escaped"], idx["pop"])
	}
}

// TestAppendItemsClipNests: a clipping fragment inside a clipping fragment nests its
// bracket (inner push between outer push and outer pop).
func TestAppendItemsClipNests(t *testing.T) {
	inner := &Fragment{
		X: 10, Y: 10, W: 50, H: 20, IsBFC: true, IsStackingContext: true, Clips: true,
		ClipRect: rect{x: 10, y: 10, w: 50, h: 20},
		Children: []*Fragment{{X: 10, Y: 10, W: 200, H: 200, Background: color.RGBA{3, 3, 3, 255}}},
	}
	outer := &Fragment{
		X: 0, Y: 0, W: 100, H: 50, IsBFC: true, IsStackingContext: true, Clips: true,
		ClipRect: rect{x: 0, y: 0, w: 100, h: 50},
		Children: []*Fragment{inner},
	}
	items := outer.AppendItems(nil)
	var pushes, pops []int
	for i := range items {
		if items[i].Kind == layout.ClipPushKind {
			pushes = append(pushes, i)
		}
		if items[i].Kind == layout.ClipPopKind {
			pops = append(pops, i)
		}
	}
	if len(pushes) != 2 || len(pops) != 2 {
		t.Fatalf("got %d pushes / %d pops, want 2/2 (nested clips)", len(pushes), len(pops))
	}
	// Nesting: outer push, inner push, inner pop, outer pop.
	if !(pushes[0] < pushes[1] && pushes[1] < pops[0] && pops[0] < pops[1]) {
		t.Errorf("clips not nested: pushes=%v pops=%v", pushes, pops)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestAppendItemsClip|TestAppendItemsNonClipping' -v`
Expected: compile error (`Clips`/`ClipRect`/`PositionedClip` undefined) — failing.

- [ ] **Step 3: Add the `Fragment` fields**

In `pkg/layout/css/fragment.go`, after the `Positioned []*Fragment` field (~line 69, inside the struct), add:

```go

	// Clips marks a fragment whose box has overflow ≠ visible: the stacking pass
	// brackets its contents (descendant decorations, floats, in-flow content, and the
	// CB-owned subset of its positioned layer) with a ClipPush(ClipRect)/ClipPop pair,
	// so they paint clipped to the padding box. The fragment's OWN background/border
	// paint OUTSIDE the bracket (a box does not clip its own border box). A clipping
	// fragment is always a BFC (overflow≠visible establishes one), so AppendItems
	// reaches it via the IsStackingContext||IsBFC branch.
	Clips bool
	// ClipRect is the clip rectangle when Clips is true: the padding box (the border
	// box deflated by the border widths), in page space. Zero when !Clips.
	ClipRect rect
	// PositionedClip parallels Positioned: PositionedClip[i] reports whether
	// Positioned[i]'s containing block is THIS fragment, so a clipping fragment wraps
	// that entry inside its clip (CSS clips a positioned descendant only when the box
	// is also its containing block). An entry that merely bubbled through this fragment
	// (its CB is an ancestor) has PositionedClip[i]=false and paints OUTSIDE the
	// bracket. len(PositionedClip) == len(Positioned) when set; a nil slice means "no
	// entry is CB-owned" (read defensively in the positioned loop). Only consulted on a
	// clipping fragment.
	PositionedClip []bool
```

- [ ] **Step 4: Factor `appendChildDecorations` out of `appendDecorations`**

In `pkg/layout/css/fragment.go`, replace `appendDecorations` (~line 197-209) with a self+children split so the clipping path can emit self-decorations outside the bracket and children inside:

```go
// appendDecorations recurses the in-flow subtree emitting only backgrounds and
// borders: this fragment's own, then its children's (skipping floated, nested-BFC,
// and positioned subtrees — see appendChildDecorations). It is the decoration-phase
// entry for a non-clipping context root.
func (f *Fragment) appendDecorations(dst []layout.Item) []layout.Item {
	dst = f.appendSelfDecorations(dst)
	return f.appendChildDecorations(dst)
}

// appendChildDecorations recurses ONLY f's children's backgrounds/borders (not f's
// own), skipping floated subtrees (painted in the float layer), NESTED BFC subtrees
// (an inline-block / new-BFC box paints as a single atom in the content phase via its
// own AppendItems), and positioned subtrees (painted in the stacking context's
// positioned layer). A clipping fragment calls this between its ClipPush and ClipPop
// so its children's decorations are clipped while its own (already emitted) are not.
func (f *Fragment) appendChildDecorations(dst []layout.Item) []layout.Item {
	for _, c := range f.Children {
		if c.IsFloat || c.IsBFC || c.IsPositioned {
			continue
		}
		dst = c.appendDecorations(dst)
	}
	return dst
}
```

(The doc comment that was on the old `appendDecorations` about "f itself may be a float here" is preserved by the behavior: `appendDecorations` still calls `appendSelfDecorations(f)` first, so a float painting as its own BFC root still paints its own background.)

- [ ] **Step 5: Rewrite the `AppendItems` stacking-context branch to bracket a clipping fragment**

In `pkg/layout/css/fragment.go`, replace the body of the `if f.IsStackingContext || f.IsBFC {` branch in `AppendItems` (~line 141-168) with a version that brackets when `f.Clips`. The new branch:

```go
	if f.IsStackingContext || f.IsBFC {
		if f.Clips {
			// Clipping context: own decorations paint UNCLIPPED, then a clip bracket wraps
			// the children's decorations, the float layer, the in-flow content, and the
			// CB-owned subset of the positioned layer. Escaped positioned descendants (CB
			// outside this box) paint AFTER ClipPop, unclipped.
			dst = f.appendSelfDecorations(dst) // own bg + border — outside the clip
			dst = append(dst, layout.Item{Kind: layout.ClipPushKind, Rule: layout.RuleItem{
				XPt: f.ClipRect.x, YPt: f.ClipRect.y, WPt: f.ClipRect.w, HPt: f.ClipRect.h,
			}})
			dst = f.appendChildDecorations(dst) // children's bg/border — clipped
			for _, fl := range f.Floats {       // the float layer — clipped
				start := len(dst)
				dst = fl.AppendItems(dst)
				if fl.RelOffsetX != 0 || fl.RelOffsetY != 0 {
					translateItems(dst, start, fl.RelOffsetX, fl.RelOffsetY)
				}
			}
			dst = f.appendContent(dst) // in-flow inline content + images — clipped
			dst = f.appendPositioned(dst, true)
			dst = append(dst, layout.Item{Kind: layout.ClipPopKind})
			dst = f.appendPositioned(dst, false)
			return dst
		}
		// Non-clipping stacking context / BFC: the prior 4-phase order (decorations →
		// floats → in-flow content → positioned layer), unchanged.
		dst = f.appendDecorations(dst)
		for _, fl := range f.Floats {
			start := len(dst)
			dst = fl.AppendItems(dst)
			if fl.RelOffsetX != 0 || fl.RelOffsetY != 0 {
				translateItems(dst, start, fl.RelOffsetX, fl.RelOffsetY)
			}
		}
		dst = f.appendContent(dst)
		dst = f.appendPositioned(dst, false)
		return dst
	}
```

Note: the non-clipping path now calls a new `appendPositioned(dst, false)` helper instead of the inline positioned loop. The `onlyCBOwned` parameter selects which subset to emit: `false` from the non-clipping path emits ALL positioned entries (identical to today); from the clipping path, `true` emits only CB-owned and a second call with `false` emits only the rest.

- [ ] **Step 6: Add the `appendPositioned` helper**

In `pkg/layout/css/fragment.go`, add (e.g. just after `AppendItems`):

```go
// appendPositioned emits the fragment's positioned layer (each entry painted fully via
// its own AppendItems, with a relatively-positioned entry's RelOffset applied over its
// emitted range). It paints positioned descendants in document order (the minimal
// z-index cut).
//
// onlyCBOwned selects which subset to emit, and is only meaningful when f.Clips:
//   - f.Clips == false: the whole layer is emitted in one call; onlyCBOwned is ignored.
//     This is the non-clipping path and is byte-identical to the prior single loop.
//   - f.Clips == true: the clipping path calls this TWICE — once with onlyCBOwned=true
//     (inside the clip bracket: entries whose containing block IS f, PositionedClip[i]
//     true) and once with onlyCBOwned=false (after ClipPop: the escaped entries, whose
//     CB is an ancestor). So CB-owned descendants are clipped and escaped ones are not.
//
// A missing/short PositionedClip entry counts as not-CB-owned (false), the safe default.
func (f *Fragment) appendPositioned(dst []layout.Item, onlyCBOwned bool) []layout.Item {
	for i, pf := range f.Positioned {
		if f.Clips {
			owned := i < len(f.PositionedClip) && f.PositionedClip[i]
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

- [ ] **Step 7: Run tests to verify they pass**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestAppendItems' -v`
Expected: PASS for all `TestAppendItems*` including the new clip tests AND the pre-existing `TestAppendItemsNonPositionedByteIdentical`, `TestAppendItemsPositionedPaintsLast`, `TestAppendItemsRelativeOffsetTranslatesRange` (the non-clipping path is unchanged).

- [ ] **Step 8: Run the full package + race + gofmt**

Run (sandbox disabled): `go test ./pkg/layout/css` then `go test -race ./pkg/layout/css`
Expected: PASS.
```bash
gofmt -l pkg/layout/css/fragment.go pkg/layout/css/fragment_test.go   # expect no output
```

- [ ] **Step 9: Commit**

```bash
git add pkg/layout/css/fragment.go pkg/layout/css/fragment_test.go
git commit -m "css/layout: clip bracket in AppendItems (Clips/ClipRect/PositionedClip)"
```

---

## Task 5: Set `Clips`/`ClipRect` in `layoutBlock`; populate `PositionedClip`

Wire the new fragment fields from the box's `overflow`. A clipping box's fragment gets `Clips=true` and `ClipRect`=its padding box. The positioned-layer collectors (in `layoutBlock`'s consume path, `layoutTree`, and `resolveAbsolute`) append the matching `PositionedClip` flag whenever they append to a `Positioned` slice.

**Files:**
- Modify: `pkg/layout/css/block.go` (`layoutBlock` frag construction ~line 273-311; `layoutTree` ~line 92; `resolveAbsolute` ~line 657; `placeFloat` ~line 559)
- Test: `pkg/layout/css/overflow_layout_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `pkg/layout/css/overflow_layout_test.go`:

```go
package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// TestClipFragmentFlaggedWithPaddingBox: an overflow:hidden box's fragment is flagged
// Clips with ClipRect == its padding box (border box deflated by border widths).
func TestClipFragmentFlaggedWithPaddingBox(t *testing.T) {
	eng := New(nil, nil, nil)
	// 100px wide, 50px tall, 5px solid border all sides, overflow:hidden.
	clipStyle := gcss.ComputedStyle{
		Display:  "block",
		Overflow: "hidden",
		Width:    gcss.Length{Value: 100, Unit: gcss.UnitPx},
		Height:   gcss.Length{Value: 50, Unit: gcss.UnitPx},
		BorderTopWidth: gcss.Length{Value: 5, Unit: gcss.UnitPx}, BorderTopStyle: "solid",
		BorderRightWidth: gcss.Length{Value: 5, Unit: gcss.UnitPx}, BorderRightStyle: "solid",
		BorderBottomWidth: gcss.Length{Value: 5, Unit: gcss.UnitPx}, BorderBottomStyle: "solid",
		BorderLeftWidth: gcss.Length{Value: 5, Unit: gcss.UnitPx}, BorderLeftStyle: "solid",
	}
	box := blockBox(clipStyle)
	root := blockBox(gcss.ComputedStyle{Display: "block"}, box)

	frag := eng.layoutTree(context.Background(), root, 200)
	if frag == nil || len(frag.Children) != 1 {
		t.Fatalf("expected 1 child fragment, got %v", frag)
	}
	clip := frag.Children[0]
	if !clip.Clips {
		t.Fatalf("clip fragment not flagged Clips")
	}
	// Border box is X=0,Y=0,W=110,H=60 (content 100x50 + 5px borders each side).
	// Padding box = border box deflated by 5px borders: (5,5,100,50).
	if clip.ClipRect.x != 5 || clip.ClipRect.y != 5 || clip.ClipRect.w != 100 || clip.ClipRect.h != 50 {
		t.Errorf("ClipRect = %+v, want {5 5 100 50} (padding box)", clip.ClipRect)
	}
}

// TestClipAbsChildCBOwnedFlagged: an absolute child of an overflow:hidden positioned
// box is collected on that box's Positioned with PositionedClip=true (the box IS its
// CB), so it will be clipped.
func TestClipAbsChildCBOwnedFlagged(t *testing.T) {
	eng := New(nil, nil, nil)
	absChild := posBox(posStyle(), cssbox.PosAbsolute)
	absChild.Style.Top = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Left = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Width = gcss.Length{Value: 10, Unit: gcss.UnitPx}
	absChild.Style.Height = gcss.Length{Value: 10, Unit: gcss.UnitPx}

	// A relative + overflow:hidden container (positioned => stacking context AND clips).
	contStyle := posStyle()
	contStyle.Overflow = "hidden"
	contStyle.Width = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	contStyle.Height = gcss.Length{Value: 60, Unit: gcss.UnitPx}
	container := posBox(contStyle, cssbox.PosRelative, absChild)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, container)
	frag := eng.layoutTree(context.Background(), root, 200)

	// The container fragment is frag.Children[0]; it owns the abs child on Positioned.
	cont := frag.Children[0]
	if !cont.Clips {
		t.Fatalf("container not flagged Clips")
	}
	if len(cont.Positioned) != 1 {
		t.Fatalf("container Positioned len = %d, want 1", len(cont.Positioned))
	}
	if len(cont.PositionedClip) != 1 || !cont.PositionedClip[0] {
		t.Errorf("PositionedClip = %v, want [true] (abs child's CB is the container)", cont.PositionedClip)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestClipFragmentFlaggedWithPaddingBox|TestClipAbsChildCBOwnedFlagged' -v`
Expected: FAIL (`clip.Clips` is false / `PositionedClip` empty) — the wiring isn't there yet.

- [ ] **Step 3: Set `Clips`/`ClipRect` on the fragment in `layoutBlock`**

In `pkg/layout/css/block.go`, in `layoutBlock`, right after the `frag := &Fragment{...}` literal and its `Border[...]` assignments (~line 284, after the four `frag.Border[...]` lines and before the `if establishesNewBFC(b)` block), add:

```go
	// overflow≠visible: this box clips its content to its padding box (the border box
	// deflated by the border widths). The stacking pass brackets the fragment's
	// contents with a clip; its own background/border paint unclipped.
	if clips(b) {
		frag.Clips = true
		frag.ClipRect = rect{
			x: borderX + ed.bL,
			y: borderY + ed.bT,
			w: borderW - ed.bL - ed.bR,
			h: borderH - ed.bT - ed.bB,
		}
		if frag.ClipRect.w < 0 {
			frag.ClipRect.w = 0
		}
		if frag.ClipRect.h < 0 {
			frag.ClipRect.h = 0
		}
	}
```

- [ ] **Step 4: Populate `PositionedClip` wherever `Positioned` is appended**

There are four append sites. Each must keep `PositionedClip` parallel to `Positioned`.

**(a) `layoutBlock` consume path** (~line 297-299). The current code is:

```go
		frag.IsStackingContext = true
		frag.Positioned = append(frag.Positioned, in.pendingPositioned...)
```

These pending entries are RELATIVE descendants that bubbled up; their CB is not this box in the resolved-abs sense, so they are NOT CB-owned for clip purposes (relative clip is deferred). Append `false` for each:

```go
		frag.IsStackingContext = true
		for range in.pendingPositioned {
			frag.PositionedClip = append(frag.PositionedClip, false)
		}
		frag.Positioned = append(frag.Positioned, in.pendingPositioned...)
```

**(b) `layoutTree` root consume** (~line 92). Current:

```go
		res.frag.Positioned = append(res.frag.Positioned, res.pendingPositioned...)
```

Replace with:

```go
		for range res.pendingPositioned {
			res.frag.PositionedClip = append(res.frag.PositionedClip, false)
		}
		res.frag.Positioned = append(res.frag.Positioned, res.pendingPositioned...)
```

**(c) `placeFloat`** (~line 559). Current:

```go
	res.frag.Positioned = append(res.frag.Positioned, res.pendingPositioned...)
```

Replace with:

```go
	for range res.pendingPositioned {
		res.frag.PositionedClip = append(res.frag.PositionedClip, false)
	}
	res.frag.Positioned = append(res.frag.Positioned, res.pendingPositioned...)
```

**(d) `resolveAbsolute`** (~line 656-658). Current:

```go
		if owner != nil {
			owner.Positioned = append(owner.Positioned, frag)
		}
```

An abs/fixed box's CB IS its owner exactly when `owner == d.cb.frag` (the non-page case). Append the matching flag:

```go
		if owner != nil {
			owner.PositionedClip = append(owner.PositionedClip, !d.cb.isPage && d.cb.frag != nil && owner == d.cb.frag)
			owner.Positioned = append(owner.Positioned, frag)
		}
```

- [ ] **Step 5: Run tests to verify they pass**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestClipFragmentFlaggedWithPaddingBox|TestClipAbsChildCBOwnedFlagged' -v`
Expected: PASS. Then `go test ./pkg/layout/css` — PASS (existing positioning/float tests still green; the `PositionedClip` slices are parallel to the unchanged `Positioned`).

- [ ] **Step 6: gofmt + commit**

```bash
gofmt -l pkg/layout/css/block.go pkg/layout/css/overflow_layout_test.go   # expect no output
git add pkg/layout/css/block.go pkg/layout/css/overflow_layout_test.go
git commit -m "css/layout: set Clips/ClipRect + PositionedClip from overflow"
```

---

## Task 6: The adversarial clip-vs-stacking matrix (end-to-end through layout)

Pins the spec's load-bearing rule with full-layout fixtures (not hand-built fragments): an in-flow overflowing child IS clipped; an abs-pos child whose CB is OUTSIDE the clip is NOT clipped; an abs-pos child whose CB IS the clip box IS clipped. Asserts via the flattened item stream (positions of ClipPush/ClipPop vs the child's items).

**Files:**
- Test: `pkg/layout/css/overflow_layout_test.go` (extend)

- [ ] **Step 1: Write the tests**

Add to `pkg/layout/css/overflow_layout_test.go`. Two small helpers (`clipBoundsReal` finds the first ClipPush / matching ClipPop indices; `bgIndex` finds a colored background's index) plus three test bodies. Add `"image/color"` and `"github.com/nathanstitt/doctaculous/pkg/layout"` to the file's import block.

```go
// TestClipInFlowChildClipped: an in-flow child taller than its overflow:hidden parent
// has its background painted INSIDE the clip bracket (between ClipPush and ClipPop).
func TestClipInFlowChildClipped(t *testing.T) {
	eng := New(nil, nil, nil)
	tall := blockBox(gcss.ComputedStyle{Display: "block",
		Height:          gcss.Length{Value: 200, Unit: gcss.UnitPx},
		BackgroundColor: color.RGBA{2, 2, 2, 255}})
	clipStyle := gcss.ComputedStyle{Display: "block", Overflow: "hidden",
		Height: gcss.Length{Value: 50, Unit: gcss.UnitPx}}
	clip := blockBox(clipStyle, tall)
	root := blockBox(gcss.ComputedStyle{Display: "block"}, clip)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	push, pop := clipBoundsReal(items)
	if push < 0 || pop < 0 {
		t.Fatalf("no clip bracket emitted (push=%d pop=%d)", push, pop)
	}
	bg := bgIndex(items, color.RGBA{2, 2, 2, 255})
	if !(push < bg && bg < pop) {
		t.Errorf("in-flow child bg at %d not inside the clip bracket [%d,%d]", bg, push, pop)
	}
}

// TestClipAbsChildOutsideCBNotClipped: an absolute child whose containing block is an
// OUTER relative ancestor (not the overflow:hidden box) paints OUTSIDE the clip
// bracket (after ClipPop). Structure: relative outer > overflow:hidden middle (static)
// > absolute child. The abs child's CB is the outer relative box, so the middle's clip
// must not clip it.
func TestClipAbsChildOutsideCBNotClipped(t *testing.T) {
	eng := New(nil, nil, nil)
	absChild := posBox(posStyle(), cssbox.PosAbsolute)
	absChild.Style.Top = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Left = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Width = gcss.Length{Value: 10, Unit: gcss.UnitPx}
	absChild.Style.Height = gcss.Length{Value: 10, Unit: gcss.UnitPx}
	absChild.Style.BackgroundColor = color.RGBA{9, 9, 9, 255}

	midStyle := gcss.ComputedStyle{Display: "block", Overflow: "hidden",
		Height: gcss.Length{Value: 50, Unit: gcss.UnitPx}}
	mid := blockBox(midStyle, absChild) // static + overflow:hidden => clips, NOT abs child's CB

	outerStyle := posStyle()
	outerStyle.Width = gcss.Length{Value: 150, Unit: gcss.UnitPx}
	outerStyle.Height = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	outer := posBox(outerStyle, cssbox.PosRelative, mid) // the abs child's CB

	root := blockBox(gcss.ComputedStyle{Display: "block"}, outer)
	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)

	push, pop := clipBoundsReal(items)
	if push < 0 || pop < 0 {
		t.Fatalf("no clip bracket emitted (push=%d pop=%d)", push, pop)
	}
	bg := bgIndex(items, color.RGBA{9, 9, 9, 255})
	if bg < 0 {
		t.Fatalf("abs child bg not found")
	}
	if push < bg && bg < pop {
		t.Errorf("abs child bg at %d is INSIDE the clip bracket [%d,%d]; its CB is outside, must not be clipped", bg, push, pop)
	}
}

// TestClipAbsChildCBOwnedClipped: an absolute child whose CB IS the overflow:hidden box
// (the box is relative + overflow:hidden) paints INSIDE the clip bracket.
func TestClipAbsChildCBOwnedClipped(t *testing.T) {
	eng := New(nil, nil, nil)
	absChild := posBox(posStyle(), cssbox.PosAbsolute)
	absChild.Style.Top = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Left = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	absChild.Style.Width = gcss.Length{Value: 10, Unit: gcss.UnitPx}
	absChild.Style.Height = gcss.Length{Value: 10, Unit: gcss.UnitPx}
	absChild.Style.BackgroundColor = color.RGBA{9, 9, 9, 255}

	contStyle := posStyle()
	contStyle.Overflow = "hidden"
	contStyle.Width = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	contStyle.Height = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	cont := posBox(contStyle, cssbox.PosRelative, absChild)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, cont)
	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)

	push, pop := clipBoundsReal(items)
	bg := bgIndex(items, color.RGBA{9, 9, 9, 255})
	if !(push >= 0 && pop >= 0 && push < bg && bg < pop) {
		t.Errorf("abs child bg at %d not inside the clip bracket [%d,%d]; CB is the clip box, must be clipped", bg, push, pop)
	}
}

// clipBoundsReal: indices of the first ClipPush and the matching ClipPop.
func clipBoundsReal(items []layout.Item) (push, pop int) {
	push, pop = -1, -1
	for i := range items {
		if items[i].Kind == layout.ClipPushKind && push < 0 {
			push = i
		}
		if items[i].Kind == layout.ClipPopKind {
			pop = i
		}
	}
	return
}

// bgIndex: index of the first BackgroundKind item with the given color, or -1.
func bgIndex(items []layout.Item, c color.RGBA) int {
	for i := range items {
		if items[i].Kind == layout.BackgroundKind && items[i].Rule.Color == c {
			return i
		}
	}
	return -1
}
```

(Only the real-typed helpers `clipBoundsReal`/`bgIndex` and the three test bodies go in the file.)

- [ ] **Step 2: Run the tests**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestClipInFlowChildClipped|TestClipAbsChildOutsideCBNotClipped|TestClipAbsChildCBOwnedClipped' -v`
Expected: PASS (Tasks 4 & 5 already implement the behavior; this task only adds coverage).

If `TestClipAbsChildOutsideCBNotClipped` FAILS, that is a real bug in the escape logic — STOP and report it to the controller (do not weaken the test). It means an escaped abs child is being clipped; the fix is in the `PositionedClip` wiring (Task 5 site (d)) or the `appendPositioned` split (Task 4).

- [ ] **Step 3: gofmt + commit**

```bash
gofmt -l pkg/layout/css/overflow_layout_test.go   # expect no output
git add pkg/layout/css/overflow_layout_test.go
git commit -m "css/layout: adversarial clip-vs-stacking matrix (in-flow/escaped/CB-owned)"
```

---

## Task 7: Float-height enclosure

A BFC box's content height grows to enclose its floats. Add `floatContext.maxBottom()`, surface it on `interior.floatsBottom`, and fold it into `contentH` only for a BFC box.

**Files:**
- Modify: `pkg/layout/css/floats.go` (add `maxBottom` ~after `clearY`, line 142)
- Modify: `pkg/layout/css/block.go` (`interior` struct ~line 321; `layoutInterior` ~line 381; `layoutBlock` `contentH` fold ~line 256)
- Test: `pkg/layout/css/floats_test.go` (the `maxBottom` unit) + `pkg/layout/css/floats_layout_test.go` (the enclosure layout test)

- [ ] **Step 1: Write the failing unit test for `maxBottom`**

Add to `pkg/layout/css/floats_test.go`:

```go
// TestMaxBottom: maxBottom returns the largest float bottom (y+h), or 0 for no floats.
func TestMaxBottom(t *testing.T) {
	c := newFloatCtx(0, 200)
	if got := c.maxBottom(); got != 0 {
		t.Errorf("maxBottom (no floats) = %v, want 0", got)
	}
	c.place(cssbox.FloatLeft, 50, 40, 0)  // bottom 40
	c.place(cssbox.FloatLeft, 50, 70, 0)  // stacks beside; bottom 70
	if got := c.maxBottom(); got != 70 {
		t.Errorf("maxBottom = %v, want 70", got)
	}
}
```

(The `newFloatCtx(left, right)` helper exists at `floats_test.go:15`.)

- [ ] **Step 2: Run it to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css -run TestMaxBottom -v`
Expected: compile error (`maxBottom` undefined) — failing.

- [ ] **Step 3: Add `maxBottom`**

In `pkg/layout/css/floats.go`, after `clearY` (~line 142), add:

```go
// maxBottom returns the largest float bottom (f.y + f.h) over all placed floats in
// this context, or 0 if there are none. A box that establishes a BFC uses it to grow
// its content height to enclose its floats (CSS 10.6.7). The value is in the same
// frame the context is queried in (the BFC-root-local frame).
func (c *floatContext) maxBottom() float64 {
	out := 0.0
	for i := range c.floats {
		if bottom := c.floats[i].y + c.floats[i].h; bottom > out {
			out = bottom
		}
	}
	return out
}
```

- [ ] **Step 4: Run it to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css -run TestMaxBottom -v`
Expected: PASS.

- [ ] **Step 5: Write the failing enclosure layout test**

Add to `pkg/layout/css/floats_layout_test.go`:

```go
// TestOverflowEnclosesFloats: an overflow:hidden box whose ONLY children are floats
// grows to enclose them (content height = the floats' bottom), instead of collapsing
// to zero height.
func TestOverflowEnclosesFloats(t *testing.T) {
	eng := New(nil, nil, nil)

	f1 := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 40, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 60, Unit: gcss.UnitPx}})
	f1.Float = cssbox.FloatLeft
	f2 := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 40, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 80, Unit: gcss.UnitPx}})
	f2.Float = cssbox.FloatLeft

	wrap := blockBox(gcss.ComputedStyle{Display: "block", Overflow: "hidden"}, f1, f2)
	root := blockBox(gcss.ComputedStyle{Display: "block"}, wrap)

	frag := eng.layoutTree(context.Background(), root, 200)
	if len(frag.Children) != 1 {
		t.Fatalf("want 1 child (the wrapper), got %d", len(frag.Children))
	}
	wrapFrag := frag.Children[0]
	// The wrapper has no border/padding, so its border-box height == content height ==
	// the tallest float bottom = 80.
	if wrapFrag.H < 80-1e-6 || wrapFrag.H > 80+1e-6 {
		t.Errorf("overflow:hidden wrapper H = %v, want 80 (encloses its floats)", wrapFrag.H)
	}
}

// TestInlineBlockNoFloatsHeightUnchanged: the enclosure change must NOT alter a BFC
// box that has no floats. An inline-block with a fixed-height in-flow child keeps its
// content-derived height (the float enclosure is a no-op: maxBottom()==0).
func TestInlineBlockNoFloatsHeightUnchanged(t *testing.T) {
	eng := New(nil, nil, nil)
	child := blockBox(gcss.ComputedStyle{Display: "block",
		Height: gcss.Length{Value: 30, Unit: gcss.UnitPx}})
	ib := blockBox(gcss.ComputedStyle{Display: "block"}, child)
	ib.Display = cssbox.DisplayInlineBlock // a BFC, no floats
	// Put it in a block so it lays out; inline-block atoms go through layoutBlock too.
	root := blockBox(gcss.ComputedStyle{Display: "block"}, ib)
	frag := eng.layoutTree(context.Background(), root, 200)
	ibFrag := frag.Children[0]
	if ibFrag.H < 30-1e-6 || ibFrag.H > 30+1e-6 {
		t.Errorf("inline-block (no floats) H = %v, want 30 (enclosure is a no-op)", ibFrag.H)
	}
}
```

- [ ] **Step 6: Run them to verify they fail**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestOverflowEnclosesFloats|TestInlineBlockNoFloatsHeightUnchanged' -v`
Expected: `TestOverflowEnclosesFloats` FAILS (wrapper H is 0, floats not enclosed). `TestInlineBlockNoFloatsHeightUnchanged` should PASS already (no floats → no change) — if it fails, the fixture is wrong, fix the fixture.

- [ ] **Step 7: Surface `floatsBottom` on `interior`**

In `pkg/layout/css/block.go`, add a field to the `interior` struct (~line 321-332), after `bfcFloats`:

```go
	// floatsBottom is the bottom of the lowest float placed in this box's OWN BFC (set
	// only when b establishes one), in the box's local content-top-0 frame. layoutBlock
	// folds it into the content height so a BFC box encloses its floats (CSS 10.6.7).
	floatsBottom float64
```

- [ ] **Step 8: Set `floatsBottom` in `layoutInterior`**

In `layoutInterior` (~line 381-383), where it already surfaces `bfcFloats` for a new BFC, extend:

```go
	// A new BFC's floats are self-contained: surface them so layoutBlock attaches them
	// to b's own fragment (the float paint layer for b's BFC).
	if establishesNewBFC(b) {
		in.bfcFloats = childFC.floats2frags()
		in.floatsBottom = childFC.maxBottom()
	}
```

- [ ] **Step 9: Fold `floatsBottom` into `contentH` for a BFC box**

In `layoutBlock` (~line 241-259), the content height is computed across the auto/fixed-height branches. Add the enclosure fold AFTER those branches and the `if contentH < 0` clamp (just before `borderH := contentH + ...` ~line 261):

```go
	// Float-height enclosure (CSS 10.6.7): a box that establishes a BFC grows to enclose
	// its floats — its content height includes the bottom of the lowest float in its own
	// BFC. A non-BFC box does not enclose floats (they stay invisible to its cursor).
	// in.floatsBottom is 0 for a BFC box with no floats, so this is a no-op there.
	if newBFC && in.floatsBottom > contentH {
		contentH = in.floatsBottom
	}
```

NOTE: this must come AFTER the `else { contentH = resolveFixedHeight(...) }` branch, so a fixed-height BFC box that is SHORTER than its floats keeps its fixed height (and clips the overflow). Re-check: a fixed height should NOT be overridden by enclosure. Therefore guard the fold to auto-height only:

```go
	if newBFC && heightAuto && in.floatsBottom > contentH {
		contentH = in.floatsBottom
	}
```

Use the `heightAuto` form (it is already in scope from `heightAuto := isHeightAuto(b)` ~line 243).

- [ ] **Step 10: Run the layout tests to verify they pass**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestOverflowEnclosesFloats|TestInlineBlockNoFloatsHeightUnchanged|TestMaxBottom' -v`
Expected: PASS. Then `go test ./pkg/layout/css` — PASS (no existing float/inline-block test regresses; existing inline-blocks have no floats so enclosure is a no-op).

- [ ] **Step 11: gofmt + commit**

```bash
gofmt -l pkg/layout/css/floats.go pkg/layout/css/floats_test.go pkg/layout/css/block.go pkg/layout/css/floats_layout_test.go   # expect no output
git add pkg/layout/css/floats.go pkg/layout/css/floats_test.go pkg/layout/css/block.go pkg/layout/css/floats_layout_test.go
git commit -m "css/layout: a BFC box encloses its floats' height (overflow clearfix)"
```

---

## Task 8: Sibling-BFC float avoidance

A box that establishes a BFC, laid out next to an existing outer float, shifts/narrows its border box past the float band (or drops below it when the gap is too narrow).

**Files:**
- Modify: `pkg/layout/css/block.go` (`layoutBlockChildren` in-flow-child branch ~line 466-486)
- Test: `pkg/layout/css/floats_layout_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `pkg/layout/css/floats_layout_test.go`:

```go
// TestBFCSiblingShiftsPastFloat: an overflow:hidden box (a BFC) following a left float
// is shifted right to the float's right edge (its border box does not overlap the
// float's margin box), instead of starting at the content-box left under the float.
func TestBFCSiblingShiftsPastFloat(t *testing.T) {
	eng := New(nil, nil, nil)

	floated := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 60, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 80, Unit: gcss.UnitPx}})
	floated.Float = cssbox.FloatLeft

	// A BFC sibling (overflow:hidden) with a fixed height, narrower than the band so it
	// fits beside the float.
	bfc := blockBox(gcss.ComputedStyle{Display: "block", Overflow: "hidden",
		Width:  gcss.Length{Value: 80, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 30, Unit: gcss.UnitPx}})

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated, bfc)
	frag := eng.layoutTree(context.Background(), root, 300)

	// The float is on Floats; the BFC sibling is the in-flow child.
	if len(frag.Children) != 1 {
		t.Fatalf("want 1 in-flow child (the BFC), got %d", len(frag.Children))
	}
	bfcFrag := frag.Children[0]
	// The float's margin box spans x∈[0,60]; the BFC box's border-left must be >= 60.
	if bfcFrag.X < 60-1e-6 {
		t.Errorf("BFC sibling X = %v, want >= 60 (shifted past the float)", bfcFrag.X)
	}
}

// TestNonBFCSiblingUnchangedByFloat: a NORMAL (non-BFC) block following a left float
// keeps its border box at the content-box left (full width slides under the float);
// only its inline content narrows. Border-left stays 0.
func TestNonBFCSiblingUnchangedByFloat(t *testing.T) {
	eng := New(nil, nil, nil)

	floated := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 60, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 80, Unit: gcss.UnitPx}})
	floated.Float = cssbox.FloatLeft

	normal := blockBox(gcss.ComputedStyle{Display: "block",
		Height: gcss.Length{Value: 30, Unit: gcss.UnitPx}})

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated, normal)
	frag := eng.layoutTree(context.Background(), root, 300)
	normalFrag := frag.Children[0]
	if normalFrag.X > 1e-6 {
		t.Errorf("non-BFC sibling X = %v, want 0 (border box slides under the float)", normalFrag.X)
	}
}

// TestBFCSiblingDropsBelowFloatWhenTooWide: a BFC sibling too wide to fit beside the
// float in the remaining band drops BELOW the float (its Y is at/under the float
// bottom), rather than overlapping it.
func TestBFCSiblingDropsBelowFloatWhenTooWide(t *testing.T) {
	eng := New(nil, nil, nil)

	floated := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 200, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 50, Unit: gcss.UnitPx}})
	floated.Float = cssbox.FloatLeft

	// A BFC sibling wider than the remaining band (300 - 200 = 100): 150px wide.
	bfc := blockBox(gcss.ComputedStyle{Display: "block", Overflow: "hidden",
		Width:  gcss.Length{Value: 150, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 30, Unit: gcss.UnitPx}})

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated, bfc)
	frag := eng.layoutTree(context.Background(), root, 300)
	bfcFrag := frag.Children[0]
	if bfcFrag.Y < 50-1e-6 {
		t.Errorf("BFC sibling Y = %v, want >= 50 (dropped below the float)", bfcFrag.Y)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestBFCSibling|TestNonBFCSiblingUnchangedByFloat' -v`
Expected: `TestBFCSiblingShiftsPastFloat` and `TestBFCSiblingDropsBelowFloatWhenTooWide` FAIL (the BFC box currently lays out at X=0, overlapping the float). `TestNonBFCSiblingUnchangedByFloat` PASSES already (the non-BFC path is unchanged) — confirms we don't regress it.

- [ ] **Step 3: Implement the shift/narrow/drop for a BFC child**

In `pkg/layout/css/block.go`, in `layoutBlockChildren`, the in-flow-block branch currently computes `startY` (clear), then calls `layoutBlock` at `contentX`/`contentW` (~line 459-470). Insert, AFTER the `clear` block and BEFORE the `res := e.layoutBlock(...)` call (~line 466), a BFC-avoidance adjustment:

```go
		// Sibling-BFC float avoidance (CSS 9.5): a child that establishes its own BFC
		// must not overlap an outer float in the PARENT's BFC — its whole border box
		// sits beside the float (unlike a normal block, whose border box slides under
		// the float while only its inline content narrows). Narrow + shift the child to
		// the float band at its Y; if it cannot fit, drop it below the float band.
		childOriginX := contentX
		childWidth := contentW
		if establishesNewBFC(child) {
			h := bfcChildBandHeight(child, contentW)
			for {
				left := fc.leftEdge(bandOriginY+startY, h)
				right := fc.rightEdge(bandOriginY+startY, h)
				avail := right - left
				if left <= contentX+1e-6 && right >= contentX+contentW-1e-6 {
					break // no float intrudes at this band: full width, no shift
				}
				if avail >= childWidthFor(child, contentW)+1e-6 || avail >= contentW-1e-6 {
					childOriginX = left
					childWidth = right - left
					break // fits beside the float
				}
				next := fc.nextDropY(bandOriginY+startY, h) - bandOriginY
				if next <= startY {
					// No lower opportunity: lay out beside at the narrowed band (overflow
					// allowed) rather than spinning.
					childOriginX = left
					childWidth = right - left
					e.logf("css layout: overflow≠visible box cannot fit beside a float; placed at the narrowed band")
					break
				}
				startY = next // drop below the float and retry at full width there
			}
		}

		res := e.layoutBlock(ctx, child, childWidth, childOriginX, 0, bandOriginY+startY, fc, posCtx, posCB)
```

IMPORTANT: this replaces the existing `res := e.layoutBlock(ctx, child, contentW, contentX, 0, bandOriginY+startY, fc, posCtx, posCB)` line — change its `contentW`→`childWidth` and `contentX`→`childOriginX` arguments. Leave the rest of the loop (the `borderTop`/shift logic) unchanged — it already positions the child's border top via `res.marginTop`; the `borderTop` math uses `startY` which we may have lowered, so the drop-below works.

- [ ] **Step 4: Add the two small helpers**

In `pkg/layout/css/block.go`, near `resolveContentWidth` (~line 786), add:

```go
// bfcChildBandHeight estimates the vertical extent of a BFC child for the float-band
// query in sibling avoidance, before the child is laid out. It uses the child's
// resolved fixed height when it has one, else a small nonzero probe (1pt) so the band
// query at least samples the child's top row (the band is coarse; the height feedback
// is not iterated — consistent with the per-line line-height approximation in the IFC).
func bfcChildBandHeight(b *cssbox.Box, cbWidth float64) float64 {
	if !isHeightAuto(b) {
		ed := usedEdges(b, cbWidth)
		if h := resolveFixedHeight(b, cbWidth, ed); h > 0 {
			return h
		}
	}
	return 1
}

// childWidthFor returns a BFC child's resolved border-box width for the
// fits-beside-the-float test in sibling avoidance: the width its border box wants when
// laid out at the full containing width (auto fills, fixed is used). It mirrors
// resolveContentWidth + the border/padding insets so the fit test compares like with
// like.
func childWidthFor(b *cssbox.Box, cbWidth float64) float64 {
	ed := usedEdges(b, cbWidth)
	return resolveContentWidth(b, cbWidth, ed) + ed.pL + ed.pR + ed.bL + ed.bR
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestBFCSibling|TestNonBFCSiblingUnchangedByFloat' -v`
Expected: PASS (all three). Then `go test ./pkg/layout/css` — PASS.

- [ ] **Step 6: Race + gofmt + commit**

Run (sandbox disabled): `go test -race ./pkg/layout/css`
Expected: PASS.
```bash
gofmt -l pkg/layout/css/block.go pkg/layout/css/floats_layout_test.go   # expect no output
git add pkg/layout/css/block.go pkg/layout/css/floats_layout_test.go
git commit -m "css/layout: a BFC sibling shifts/narrows past an outer float (CSS 9.5)"
```

---

## Task 9: `scroll`/`auto` degradation log

`scroll`/`auto` clip like `hidden`; log the dropped scroll affordance once where such a box is seen, so the approximation is visible.

**Files:**
- Modify: `pkg/layout/css/block.go` (the `clips(b)` set-up site in `layoutBlock`, Task 5 Step 3)
- Test: `pkg/layout/css/overflow_layout_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/overflow_layout_test.go`:

```go
// TestScrollLogsDegradation: an overflow:scroll box logs that scroll/auto clip like
// hidden (no scroll affordance in this model); overflow:hidden does NOT log.
func TestScrollLogsDegradation(t *testing.T) {
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, format) }
	eng := New(nil, nil, logf)

	scrollBox := blockBox(gcss.ComputedStyle{Display: "block", Overflow: "scroll",
		Width:  gcss.Length{Value: 50, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 50, Unit: gcss.UnitPx}})
	root := blockBox(gcss.ComputedStyle{Display: "block"}, scrollBox)
	eng.layoutTree(context.Background(), root, 200)

	found := false
	for _, l := range logs {
		if strings.Contains(l, "scroll") || strings.Contains(l, "auto") {
			found = true
		}
	}
	if !found {
		t.Errorf("overflow:scroll did not log a degradation; logs=%v", logs)
	}
}
```

Add `"strings"` to the file's imports.

- [ ] **Step 2: Run it to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css -run TestScrollLogsDegradation -v`
Expected: FAIL (no log emitted).

- [ ] **Step 3: Add the log in the `clips(b)` block**

In `pkg/layout/css/block.go`, inside the `if clips(b) {` block added in Task 5 Step 3, after setting `frag.Clips = true` and `frag.ClipRect`, add:

```go
		if b.Style.Overflow == "scroll" || b.Style.Overflow == "auto" {
			e.logf("css layout: overflow:%s clips like hidden (no scroll affordance in the single-tall-page model)", b.Style.Overflow)
		}
```

- [ ] **Step 4: Run it to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css -run TestScrollLogsDegradation -v`
Expected: PASS. Then `go test ./pkg/layout/css` — PASS.

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -l pkg/layout/css/block.go pkg/layout/css/overflow_layout_test.go   # expect no output
git add pkg/layout/css/block.go pkg/layout/css/overflow_layout_test.go
git commit -m "css/layout: log scroll/auto clip-like-hidden degradation"
```

---

## Task 10: Paint-level raster test (clip actually cuts pixels)

Confirm end-to-end that a clipped overflowing child is cut at the padding-box edge in real pixels (not just item ordering).

**Files:**
- Test: `pkg/layout/paint/paint_test.go` (extend; reuse `newRasterPage`)

- [ ] **Step 1: Write the test**

Add to `pkg/layout/paint/paint_test.go`:

```go
// TestClipCutsPixels: a background that extends past a clip rect is painted only
// inside the clip. A 100x100 page, clip rect [0,0,50,50], a background covering the
// whole page: a pixel at (25,25) is the background color; a pixel at (75,75) is the
// white page background (clipped out).
func TestClipCutsPixels(t *testing.T) {
	bg := color.RGBA{0x33, 0x66, 0x99, 0xff}
	page := &layout.Page{
		WidthPt: 100, HeightPt: 100,
		Items: []layout.Item{
			{Kind: layout.ClipPushKind, Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 50, HPt: 50}},
			{Kind: layout.BackgroundKind, Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 100, HPt: 100, Color: bg}},
			{Kind: layout.ClipPopKind},
		},
	}
	img := newRasterPage(100, 100, page)

	if got := img.RGBAAt(25, 25); !isColor(got, bg) {
		t.Errorf("pixel (25,25) = %v, want background %v (inside clip)", got, bg)
	}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	if got := img.RGBAAt(75, 75); !isColor(got, white) {
		t.Errorf("pixel (75,75) = %v, want white %v (clipped out)", got, white)
	}
}
```

(`newRasterPage` and `isColor` already exist in `paint_test.go`.)

- [ ] **Step 2: Run it**

Run (sandbox disabled): `go test ./pkg/layout/paint -run TestClipCutsPixels -v`
Expected: PASS (Task 3 wired the painter; this is end-to-end confirmation). If it FAILS, the painter clip wiring (Task 3) is wrong — investigate before proceeding.

- [ ] **Step 3: Commit**

```bash
gofmt -l pkg/layout/paint/paint_test.go   # expect no output
git add pkg/layout/paint/paint_test.go
git commit -m "layout/paint: raster test — clip cuts pixels at the padding box"
```

---

## Task 11: Goldens — `overflow-hidden` + restored `float-row`

Add eyeball-able golden fixtures. **The controller eyeballs every generated PNG** (the implementer cannot see images).

**Files:**
- Modify: `pkg/doctaculous/html_golden_test.go` (add two entries to `htmlGoldens`)
- Create (via `-update`): `pkg/doctaculous/testdata/golden/html-overflow-hidden.png`, `pkg/doctaculous/testdata/golden/html-float-row.png`

- [ ] **Step 1: Add the golden fixtures**

In `pkg/doctaculous/html_golden_test.go`, append to the `htmlGoldens` slice (after the `position-absolute` entry ~line 198), and REMOVE the `float-row`-omission NOTE comment (~line 154-162) since float-row is now restored:

```go
	{
		// overflow:hidden clips an oversized child to the box's padding box. A 120x70
		// box with a 12px border and overflow:hidden contains a child that is far taller
		// and wider; eyeball that the child (green) is cut at the padding-box edge while
		// the box's own border (navy) paints at full size around it.
		name:       "overflow-hidden",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .clip { width: 120px; height: 70px; border: 12px solid #002255; overflow: hidden; }
  .big { width: 300px; height: 300px; background: #33aa33; }
</style></head><body>
  <div class="clip"><div class="big"></div></div>
</body></html>`,
	},
	{
		// Float-height enclosure (the overflow:hidden "clearfix"): three left-floated
		// swatches inside an overflow:hidden wrapper. Eyeball that the wrapper has real
		// height (encloses the floats) and shows the three swatches in a row — the case
		// 5a had to drop because a non-BFC float-only body collapsed to a 1x1 page.
		name:       "float-row",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .wrap { overflow: hidden; background: #eeeeee; }
  .sw { float: left; width: 60px; height: 60px; }
  .a { background: #cc3333; }
  .b { background: #33aa33; }
  .c { background: #3355cc; }
</style></head><body>
  <div class="wrap"><div class="sw a"></div><div class="sw b"></div><div class="sw c"></div></div>
</body></html>`,
	},
```

- [ ] **Step 2: Generate the goldens**

Run (sandbox disabled): `go test ./pkg/doctaculous -run TestHTMLGolden -update`
Expected: PASS; writes `html-overflow-hidden.png` and `html-float-row.png`.

- [ ] **Step 3: CONTROLLER eyeballs the PNGs**

The controller (not the implementer) reads both PNGs with the Read tool and confirms:
- `html-overflow-hidden.png`: the green child is **cut** at the box's inner (padding) edge; the navy border is intact and full-size; no green spills past the border box.
- `html-float-row.png`: the gray wrapper has visible height enclosing all three swatches; red/green/blue swatches sit in a row.

If either looks wrong, STOP — the fixture or the implementation is wrong; do not commit a bad golden.

- [ ] **Step 4: Confirm no existing golden changed**

Run (sandbox disabled): `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata`
Expected: ONLY the two new files (`html-overflow-hidden.png`, `html-float-row.png`) appear as untracked — no `M` (modified) on any existing golden. If an existing golden changed, the clip/enclosure change leaked into a non-overflow page — STOP and investigate.

Then run the goldens WITHOUT `-update` to confirm they pass:
Run (sandbox disabled): `go test ./pkg/doctaculous -run TestHTMLGolden`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/doctaculous/html_golden_test.go pkg/doctaculous/testdata/golden/html-overflow-hidden.png pkg/doctaculous/testdata/golden/html-float-row.png
git commit -m "doctaculous: overflow-hidden + restored float-row goldens"
```

---

## Task 12: WPT reftests — `overflow-hidden` + `float-row`

Add reference-comparison pairs: a clipped overflowing box == a box authored to fit; an enclosing overflow:hidden float wrapper == an explicit-height reference.

**Files:**
- Create: `pkg/doctaculous/testdata/wpt/css21-normal-flow/overflow-hidden.html` + `-ref.html`
- Create: `pkg/doctaculous/testdata/wpt/css21-normal-flow/float-row.html` + `-ref.html`
- Modify: `pkg/doctaculous/wpt_reftest_test.go` (add two entries; remove the float-row NOTE)

- [ ] **Step 1: Create the `overflow-hidden` pair**

`overflow-hidden.html` (a clipping box with an oversized child):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .clip { width: 100px; height: 40px; overflow: hidden; background: #ffffff; }
  .big { width: 100px; height: 200px; background: #3366cc; }
</style></head><body>
  <div class="clip"><div class="big"></div></div>
</body></html>
```

`overflow-hidden-ref.html` (the visible region authored directly — a 100x40 box of the same color, no overflow):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .clip { width: 100px; height: 40px; background: #ffffff; }
  .big { width: 100px; height: 40px; background: #3366cc; }
</style></head><body>
  <div class="clip"><div class="big"></div></div>
</body></html>
```

(The test's `.big` is clipped to 100x40; the reference's `.big` IS 100x40. The clipped overflow is below y=40, off the visible box, so the two render identically.)

- [ ] **Step 2: Create the `float-row` pair**

`float-row.html` (floats enclosed by overflow:hidden):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .wrap { overflow: hidden; }
  .sw { float: left; width: 50px; height: 50px; background: #3366cc; }
</style></head><body>
  <div class="wrap"><div class="sw"></div><div class="sw"></div><div class="sw"></div></div>
</body></html>
```

`float-row-ref.html` (an explicit-height block with three in-flow inline-block swatches at the same coordinates):

```html
<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .wrap { height: 50px; }
  .sw { display: inline-block; width: 50px; height: 50px; background: #3366cc; }
</style></head><body>
  <div class="wrap"><div class="sw"></div><div class="sw"></div><div class="sw"></div></div>
</body></html>
```

NOTE on the reference: inline-block swatches sit on a baseline-aligned row at the same 50x50 positions as the floats (all same height, starting at x=0,50,100). Both wrappers are 50px tall and 150px of swatches. If a sub-pixel inline-block baseline gap makes them differ, the implementer should instead author the reference with three `float:left` swatches in an `overflow:hidden` wrap of explicit `height:50px` — but try the inline-block reference first and only switch if the reftest fails on a real difference.

- [ ] **Step 3: Register the reftests**

In `pkg/doctaculous/wpt_reftest_test.go`, add to the `wptReftests` slice (after `relative-offset` ~line 56) and REMOVE the float-row NOTE comment (~line 57-63):

```go
	{"overflow-hidden", 200, "an overflow:hidden box clips an oversized child to its box (== a box authored to fit)", nil},
	{"float-row", 200, "an overflow:hidden wrapper encloses its floats (== an explicit-height row of the same swatches)", nil},
```

- [ ] **Step 4: Run the reftests**

Run (sandbox disabled): `go test ./pkg/doctaculous -run TestWPTReftests -v`
Expected: PASS for `overflow-hidden` and `float-row` (and all pre-existing pairs). If `float-row` fails on a real pixel difference, switch its reference to the float-based form per the Step 2 note and re-run.

- [ ] **Step 5: Commit**

```bash
git add pkg/doctaculous/testdata/wpt/css21-normal-flow/overflow-hidden.html pkg/doctaculous/testdata/wpt/css21-normal-flow/overflow-hidden-ref.html pkg/doctaculous/testdata/wpt/css21-normal-flow/float-row.html pkg/doctaculous/testdata/wpt/css21-normal-flow/float-row-ref.html pkg/doctaculous/wpt_reftest_test.go
git commit -m "doctaculous: overflow-hidden + float-row WPT reftests"
```

---

## Task 13: Flag-combination layout tests

The 5a/5b worst-miss class. Test the combinations explicitly: a clipped box containing a float (clips its own float layer); a clipped box that is itself positioned.

**Files:**
- Test: `pkg/layout/css/overflow_layout_test.go` (extend)

- [ ] **Step 1: Write the tests**

Add to `pkg/layout/css/overflow_layout_test.go`:

```go
// TestClipBracketsOwnFloatLayer: a clipping box that ALSO contains a float brackets its
// own float layer (the float's items fall inside the box's ClipPush/ClipPop).
func TestClipBracketsOwnFloatLayer(t *testing.T) {
	eng := New(nil, nil, nil)
	floated := blockBox(gcss.ComputedStyle{Display: "block",
		Width:           gcss.Length{Value: 40, Unit: gcss.UnitPx},
		Height:          gcss.Length{Value: 200, Unit: gcss.UnitPx},
		BackgroundColor: color.RGBA{4, 4, 4, 255}})
	floated.Float = cssbox.FloatLeft

	clip := blockBox(gcss.ComputedStyle{Display: "block", Overflow: "hidden",
		Height: gcss.Length{Value: 50, Unit: gcss.UnitPx}}, floated)
	root := blockBox(gcss.ComputedStyle{Display: "block"}, clip)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	push, pop := clipBoundsReal(items)
	bg := bgIndex(items, color.RGBA{4, 4, 4, 255})
	if !(push >= 0 && pop >= 0 && push < bg && bg < pop) {
		t.Errorf("float bg at %d not inside the clip box's bracket [%d,%d]", bg, push, pop)
	}
}

// TestPositionedClippingBoxStillClips: a box that is BOTH position:relative AND
// overflow:hidden is a stacking context and a clipping box; its in-flow overflowing
// child is still bracketed by a clip.
func TestPositionedClippingBoxStillClips(t *testing.T) {
	eng := New(nil, nil, nil)
	tall := blockBox(gcss.ComputedStyle{Display: "block",
		Height:          gcss.Length{Value: 200, Unit: gcss.UnitPx},
		BackgroundColor: color.RGBA{6, 6, 6, 255}})

	clipStyle := posStyle()
	clipStyle.Overflow = "hidden"
	clipStyle.Height = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	clip := posBox(clipStyle, cssbox.PosRelative, tall)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, clip)
	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	push, pop := clipBoundsReal(items)
	bg := bgIndex(items, color.RGBA{6, 6, 6, 255})
	if !(push >= 0 && pop >= 0 && push < bg && bg < pop) {
		t.Errorf("child bg at %d not inside the positioned+clip box's bracket [%d,%d]", bg, push, pop)
	}
}
```

- [ ] **Step 2: Run the tests**

Run (sandbox disabled): `go test ./pkg/layout/css -run 'TestClipBracketsOwnFloatLayer|TestPositionedClippingBoxStillClips' -v`
Expected: PASS (Tasks 4/5 already cover these paths; this locks the combinations). If `TestPositionedClippingBoxStillClips` fails, a positioned+clip box is taking a wrong branch — investigate the `AppendItems` clip branch precondition (`IsStackingContext || IsBFC` — a positioned clip box is both).

- [ ] **Step 3: gofmt + commit**

```bash
gofmt -l pkg/layout/css/overflow_layout_test.go   # expect no output
git add pkg/layout/css/overflow_layout_test.go
git commit -m "css/layout: flag-combination clip tests (clip+float, clip+positioned)"
```

---

## Task 14: Full-suite verification + lint

**Files:** none (verification only)

- [ ] **Step 1: Full test suite + race**

Run (sandbox disabled): `go test ./...`
Expected: PASS (all packages).
Run (sandbox disabled): `go test -race ./...`
Expected: PASS.

- [ ] **Step 2: Confirm DOCX + existing goldens unchanged**

Run (sandbox disabled): `go test ./pkg/doctaculous -run 'TestDOCXGolden|TestHTMLGolden|TestWPTReftests'`
Expected: PASS. Confirm `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` shows no modified pre-existing golden (only the new ones, already committed).

- [ ] **Step 3: Lint the changed packages**

Run (sandbox disabled): `golangci-lint run ./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...`
Expected: no findings. Common gotchas to fix if flagged: an `if !(a && b)` (rewrite as `if a >= b || ...`); an unused field/func (every new identifier — `clips`, `maxBottom`, `floatsBottom`, `Clips`, `ClipRect`, `PositionedClip`, `appendChildDecorations`, `appendPositioned`, `bfcChildBandHeight`, `childWidthFor` — must be read/called).

- [ ] **Step 4: gofmt the whole change**

Run (sandbox disabled): `gofmt -l pkg/css pkg/layout pkg/doctaculous`
Expected: no output. (If any file lists, `gofmt -w` it and amend the relevant commit.)

- [ ] **Step 5: Find and delete any scratch files**

Run (sandbox disabled): `find . -name 'zz_*' -delete` then `git status`
Expected: clean working tree (everything committed).

---

## Task 15: Documentation — update CLAUDE.md + spec deferrals

**Files:**
- Modify: `CLAUDE.md` (move overflow out of §6 TODO into Done)
- Modify: `docs/superpowers/specs/2026-06-24-html-overflow-design.md` (note any review fixes folded in)

- [ ] **Step 1: Update CLAUDE.md Done section**

In `CLAUDE.md`, add a new bullet to the "Done" list (after the positioning bullet) summarizing the overflow slice: `overflow: hidden/scroll/auto` clipping to the padding box via flat-stream clip items honored by the painter's clip stack; `overflow≠visible` establishes a BFC; float-height enclosure (the clearfix); sibling-BFC float avoidance; scroll/auto clip like hidden; the deferred relative-descendant-escapes-a-non-positioned-clip gap. Reference `docs/superpowers/specs/2026-06-24-html-overflow-design.md`.

- [ ] **Step 2: Update the §6 TODO**

In `CLAUDE.md` §6 (HTML rendering — remaining slices), remove "overflow clipping (...)" from the pending list (it is now Done), keeping the float-enclosure / float-across-BFC sub-items only if any remain (they are done — remove them too), and leave the rest of §6 intact.

- [ ] **Step 3: Record review fixes in the spec**

If the per-task or holistic review changed any decision, add a "Review fixes folded in" section to `docs/superpowers/specs/2026-06-24-html-overflow-design.md` summarizing them (mirroring 5a's spec). If nothing changed, skip.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/superpowers/specs/2026-06-24-html-overflow-design.md
git commit -m "docs: record overflow slice in CLAUDE.md (Done + deferrals)"
```

---

## Done criteria

- `overflow: hidden/scroll/auto` clips a box's content to its padding box; the box's own border paints unclipped; clips nest.
- An abs-pos descendant whose CB is the clipping box is clipped; one whose CB is outside is not.
- A BFC box (incl. `overflow:hidden`) encloses its floats' height; a float-free BFC is unchanged.
- A BFC sibling shifts/narrows past an outer float (or drops below it); a non-BFC sibling is unchanged.
- `scroll`/`auto` clip like hidden and log the dropped affordance.
- New goldens (`overflow-hidden`, `float-row`) eyeballed and correct; all pre-existing goldens/reftests byte-identical; DOCX unaffected.
- `go test -race ./...` clean; `golangci-lint` clean; `gofmt` clean; no `zz_*` scratch files; working tree committed.
