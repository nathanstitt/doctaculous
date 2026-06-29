# HTML rendering — overflow clipping (`overflow: hidden/scroll/auto`) + deferred float interactions (sub-project 5c)

**Status:** Designed. Branch `feat/html-overflow` (off `feat/html-positioning`, or off `main` if the
stack has merged). Roadmap §6 "overflow clipping" slice. Predecessors: sub-projects 1 (CSS
parse+cascade), 2 (box generation), 3 (block+inline normal flow), 4 (replaced content + images),
5a (floats + clear, `2026-06-24-html-floats-design.md`), 5b (positioning,
`2026-06-24-html-positioning-design.md`). This is the **third and last of three slices** sub-project 5
was split into (5a floats, 5b positioning, 5c overflow). Successors: full z-index stacking, tables, web
fonts, flexbox/grid (see CLAUDE.md roadmap §6).

## Goal

`overflow: visible | hidden | scroll | auto` controls whether a box clips content that overflows its
box, and — for `hidden`/`scroll`/`auto` — establishes a new **block formatting context (BFC)**. This
slice does three things, all hinging on that BFC:

- **Clipping.** A box with `overflow ≠ visible` clips its descendants' paint to its **padding box** (CSS:
  the clip rectangle is the padding box — the border box deflated by the border widths). A child that
  overflows the box is cut at that edge. The box's **own** background and border paint at full size
  (a box does not clip its own border box).
- **Float-height enclosure.** A box that establishes a BFC (now including `overflow ≠ visible`) **grows
  to enclose its floats** (CSS 10.6.7: a BFC's height includes the bottom of every float placed in it).
  This is the standards-compliant mechanism behind the `overflow:hidden` "clearfix" — a wrapper that
  stretches to contain its floated children instead of collapsing to zero height. Today a float-only
  block has **zero** content height (5a's documented gap).
- **Sibling-BFC float avoidance.** A box that establishes a BFC, laid out next to a **pre-existing
  float**, does **not** overlap that float: its whole border box shifts/narrows to sit beside the float
  (CSS 9.5: "the border box of a box that establishes a new BFC must not overlap the margin box of any
  floats in the same BFC"). Today the engine narrows only a normal block's *inline content* around a
  float; a BFC box's border box would slide under it (5a's documented gap).

Before this slice, `overflow` is **not parsed at all** (it is not on `ComputedStyle`), and every box
renders without clipping (a graceful no-op — the same starting point floats and positioning had). This
slice wires `overflow` through the CSS cascade and box generation, establishes a BFC for
`overflow ≠ visible`, implements clipping at paint via a new flat-stream clip primitive, and folds in the
two float interactions that the BFC trigger unlocks.

**Scope (agreed): clipping + float-height enclosure + sibling-BFC float avoidance.**

In:

- `overflow: visible | hidden | scroll | auto` on block-level boxes, parsed onto `ComputedStyle`, mapped
  to a "clips / does not clip" decision at layout time.
- `overflow ≠ visible` **establishes a new BFC** (extends `establishesNewBFC`), the trigger for both
  float interactions below.
- **Clipping at paint**: a clipping box clips its descendants' content, its own float layer, and any
  positioned descendant **whose containing block is the clipping box**, to its **padding box**. Clips
  **nest** (an inner clip intersects the outer). Implemented via two new `layout.Item` kinds
  (`ClipPushKind`/`ClipPopKind`) that drive the painter's existing clip stack
  (`render.Device.Save`/`PushClip`/`Restore`).
- **Float-height enclosure**: a BFC box's content height is `max(in-flow content bottom, the bottom of
  every float in its own BFC)`.
- **Sibling-BFC float avoidance**: a BFC box laid out next to an outer float shifts/narrows its border
  box past the float band (or drops below it when the remaining band is too narrow).

Out (degrade gracefully — clip like `hidden` / lay out approximately, with a `logf` line; none panic):

- **`scroll`/`auto` scroll affordance** — in this no-scrollbars, single-tall-page model there is no
  scroll position and no scrollbar chrome, so `scroll`/`auto`/`hidden` all map to "clip to the padding
  box". Documented + logged when a `scroll`/`auto` box is seen (the clip is exact; only the
  scrollability is dropped).
- **`overflow-x` / `overflow-y`** — only the `overflow` shorthand is modeled (a single `Overflow`
  value). CSS computes the two axes separately, but since every clipping keyword clips identically here
  and the model has no axis-specific scroll, one value suffices. An axis-specific declaration is dropped
  by the cascade (an unrecognized property). Documented.
- **A BFC box that cannot fit beside an outer float** in the remaining band is **dropped below** the
  float (CSS 9.5 fallback) rather than overflowing — logged.
- **`overflow` on an inline-level box / `overflow:clip` / `overflow:visible` with a clip from a
  containing scroll container** — out of scope; only block-level `hidden/scroll/auto` clip. Unknown
  `overflow` values normalize to `visible` (no clip).
- **A `position:relative` (or other positioned) descendant of a *non-positioned* `overflow:hidden` box
  escaping the clip** — a non-positioned clipping box is a BFC but not a stacking context, so a positioned
  descendant bubbles past it and paints unclipped in a higher positioned layer (CSS clips it; we don't
  yet). Out of scope — see "The clip in the stacking pass" and "Deferred". The common in-flow overflow
  case and the abs-pos-with-CB-is-the-clipping-box case ARE clipped.

## Architecture / seams

The work is almost entirely in `pkg/layout/css` (the float interactions, the clip bracketing in the
stacking pass) plus a small, format-neutral painter addition (`pkg/layout` + `pkg/layout/paint`) and a
one-property cascade addition (`pkg/css`). The shared inline core (`pkg/layout/inline`, used by DOCX
too) is **untouched** — overflow needs no inline primitive (unlike floats, which needed `BreakNext`).

- **`pkg/css` (cascade)** — additive `Overflow string` on `ComputedStyle` (`"visible"` default), parsed
  like the other keyword properties (`box-sizing`/`object-fit`/`float`). **Not inherited** (overflow is
  not a CSS-inherited property — kept out of `inheritFrom`).
- **`pkg/layout/cssbox`** — **no new `Box` field.** The engine reads `Box.Style.Overflow` at layout time
  (exactly how positioning reads `Box.Style` offsets). A small predicate `clips(b)` (in `block.go`)
  returns `b.Style.Overflow != "" && b.Style.Overflow != "visible"`.
- **`pkg/layout/css/block.go`** — `establishesNewBFC(b)` returns true when `clips(b)` (the BFC trigger).
  `layoutBlock` folds enclosed floats into a BFC box's content height (enclosure). `layoutBlockChildren`
  shifts/narrows a BFC child past outer floats (sibling avoidance). A clipping fragment is flagged so the
  stacking pass brackets it.
- **`pkg/layout/css/floats.go`** — one new pure query, `maxBottom()` (the largest float bottom in the
  context), feeding enclosure. The sibling-avoidance path reuses the existing
  `leftEdge`/`rightEdge`/`nextDropY`.
- **`pkg/layout/css/fragment.go`** — `Fragment` gains a clip flag + padding-box geometry and a
  per-positioned-child "clip to this container" tag (see "The clip in the stacking pass"). `AppendItems`
  emits `ClipPush`/`ClipPop` around a clipping fragment's contents (and around the CB-owned subset of its
  positioned layer).
- **`pkg/layout` (page.go)** — two new `ItemKind`s, `ClipPushKind` (carries a clip rect in `Item.Rule`)
  and `ClipPopKind` (carries nothing).
- **`pkg/layout/paint/paint.go`** — `PaintPage` maps `ClipPushKind` → `dev.Save()` + `clipRect(...)` and
  `ClipPopKind` → `dev.Restore()`, maintaining a clip stack across the flat item list. Reuses the
  existing `clipRect` helper and the raster backend's nesting clip stack (already driven by
  `object-fit:cover`).

### Why this stays concurrency-safe

No new cross-goroutine shared state. The clip flag and float queries are per-`Layout`-call work threaded
through one goroutine's recursion; the fragment tree is immutable by the time `Layout` returns
(read-only-after-layout). `AppendItems` remains a pure read of the tree — the clip items are *appended*
to `dst`, never stored on a fragment, mirroring how the relative-offset translate mutates only the
freshly-appended `dst` items. The painter's clip stack is per-`PaintPage`-call device state.

## Cascade + box generation (`pkg/css`, `pkg/layout/css/build.go`)

- **`pkg/css`** — `ComputedStyle` gains `Overflow string` (`"visible"` default). Parsed by the existing
  keyword path:

  ```go
  case "overflow":
      switch d.Value {
      case "visible", "hidden", "scroll", "auto":
          cs.Overflow = d.Value
      }
  ```

  Initial value `"visible"` set in `initialStyle()`. **Not** added to `inheritFrom` (not inherited). An
  `overflow-x`/`overflow-y` declaration is an unrecognized property and is dropped (documented
  degradation).
- **Box generation (`build.go`)** — nothing maps `overflow` onto a `cssbox.Box` field; the engine reads
  `Box.Style.Overflow` directly (decision: read from `Box.Style`, like positioning offsets — keeps
  `cssbox.Box` minimal). The box already carries its full `ComputedStyle`. No `positionOf`/`floatOf`-style
  mapping function is needed.

## Clipping at paint (the flat-stream clip primitive)

The painter consumes a **flat** `layout.Item` list with no nesting; expressing "these items are clipped
to rect R" needs a stack discipline in the stream. Two new item kinds bracket a clipped range:

- **`pkg/layout` (page.go):**
  - `ClipPushKind` — pushes a clip rect. It reuses `Item.Rule` (a `RuleItem`, which already holds
    `XPt,YPt,WPt,HPt`) to carry the rectangle; the `Color` field is unused for a clip. No new union field.
  - `ClipPopKind` — pops the most recent clip. Carries nothing.

  Doc comments make explicit that these are paint-control items (not drawing primitives) and that pushes
  and pops are balanced by construction (each push emitted by `AppendItems` has a matching pop from the
  same call).

- **`pkg/layout/paint/paint.go`:** `PaintPage` gains two cases in its item switch:

  ```go
  case layout.ClipPushKind:
      dev.Save()
      clipRect(dev, mat, it.Rule.XPt, it.Rule.YPt, it.Rule.XPt+it.Rule.WPt, it.Rule.YPt+it.Rule.HPt)
  case layout.ClipPopKind:
      dev.Restore()
  ```

  `clipRect` is the existing helper (maps the rect's corners through `mat`, calls `dev.PushClip(p,
  render.NonZero)`). `dev.Save()` snapshots the clip so `dev.Restore()` pops back to the enclosing clip;
  the raster backend's `PushClip` intersects the new rect with the active clip, so **nested clips
  intersect automatically** and pop correctly. A degenerate (zero/negative-area) clip rect makes
  `clipRect` a no-op push — but `Save`/`Restore` still balance, so the stream stays well-formed.

The **non-clipping path emits no clip items**, so a page with no `overflow ≠ visible` box produces a
byte-identical item list to pre-5c (the highest-risk regression — verified by a `reflect.DeepEqual`
test and the no-`-update` golden run).

## The clip in the stacking pass (`fragment.go`)

A clipping fragment `f` (with `overflow ≠ visible`) is, by the BFC rule, **always a BFC**, so its
`AppendItems` takes the `IsStackingContext || IsBFC` branch. The clip must wrap `f`'s **contents** but
must NOT wrap (a) `f`'s **own** background/border (a box does not clip its own border box), nor (b) a
positioned descendant whose containing block is **outside** `f` (the CSS abs-pos clipping rule). New
`Fragment` fields:

```go
// Clips marks a fragment whose box has overflow ≠ visible: the stacking pass brackets
// its contents (descendant decorations, floats, in-flow content, and CB-owned positioned
// descendants) with a ClipPush(ClipRect)/ClipPop pair so they paint clipped to the
// padding box. The fragment's own background/border paint OUTSIDE the bracket.
Clips    bool
ClipRect rect // the padding box (border box deflated by border widths), in page space
```

and, to express the abs-pos escape rule, the positioned layer's CB-owned subset is tagged. The cleanest
encoding (decided): a parallel boolean slice on the owner, or a small wrapper, marking which
`Positioned` entries have **this** fragment as their containing block:

```go
// PositionedClip[i] reports whether Positioned[i]'s containing block is THIS fragment,
// so a clipping fragment wraps that entry in its clip (CSS: overflow clips a positioned
// descendant only when the box is also its containing block). An entry that merely
// bubbled through f (its CB is an ancestor) is NOT clipped by f. len == len(Positioned).
PositionedClip []bool
```

`resolveAbsolute` already chooses the `Positioned` holder as **exactly** the CB owner fragment for a
non-page CB (`owner = d.cb.frag` when `!d.cb.isPage && d.cb.frag != nil`, else `owner = root`). So when it
appends the resolved fragment to `owner.Positioned`, it appends `owner == d.cb.frag` to
`owner.PositionedClip`: `true` means "this entry's containing block IS its holder fragment" (a non-page
CB), `false` means the holder is the root acting for a page/ICB CB (the root never clips). A holder then
clips an entry iff `holder.Clips && PositionedClip[i]` — i.e. the holder has `overflow ≠ visible` **and**
is the entry's containing block. (Because every positioned box establishes a stacking context, an abs
box's CB fragment is always the fragment that holds its `Positioned` entry, so this local check is
sufficient — there is no case where the CB is a non-holder ancestor.)

A **relative** box is recorded on its nearest **stacking-context** ancestor's `Positioned` during the
in-flow pass and appends `PositionedClip=false`. This exposes a **scoped limitation** worth stating
plainly: a non-positioned `overflow:hidden` box is a BFC but **not** a stacking context
(`establishesStackingContext` = positioned only). So a `position:relative` descendant of a *non-positioned*
clipping box bubbles **past** that box to a higher stacking context and paints in *that* context's
positioned layer — **outside** the clipping box's bracket. Real browsers clip such a relative descendant
(the clip applies regardless of stacking), so this is a known gap. It is **out of scope** for this slice
and **deferred** (it needs the clip to reach a descendant that paints in an ancestor's positioned layer —
the same machinery the full-z-index slice reworks). The **supported, tested** clipping cases are: (a) an
in-flow, non-positioned overflowing descendant (the overwhelmingly common case — clipped, inside the
bracket); and (b) an `absolute`/`fixed` descendant **whose containing block is the clipping box** (clipped
via `PositionedClip`). A relative descendant of a *positioned* clipping box (the box is then a stacking
context, so the descendant lands on the box's own `Positioned`) is **not** specially clipped this slice
either — same deferral. The degradation is silent (the relative descendant simply paints unclipped, never
panics); documented in "Deferred". The flag is read only in the positioned-phase loop.

The clipping branch of `AppendItems` (only when `f.Clips`):

```
appendSelfDecorations(f)                       // f's OWN background + border — UNCLIPPED
  emit Item{Kind: ClipPushKind, Rule: rectOf(f.ClipRect)}
  appendDecorations(f)  // recurse children's backgrounds/borders (skips f's own via a flag)
  for fl := range f.Floats { ... }             // f's own float layer — clipped
  appendContent(f)                             // in-flow inline content + images — clipped
  for i, pf := range f.Positioned where PositionedClip[i] { pf.AppendItems(...) }  // CB-owned — clipped
  emit Item{Kind: ClipPopKind}
for i, pf := range f.Positioned where !PositionedClip[i] { pf.AppendItems(...) }   // escaped — UNCLIPPED
```

Two structural details:

1. **`appendDecorations` must skip `f`'s own self-decoration when bracketing**, because
   `appendSelfDecorations(f)` already emitted it (unclipped) before the push. Today `appendDecorations`
   calls `appendSelfDecorations(f)` first, then recurses children. For a clipping fragment, the contents
   bracket needs only the *children's* decorations. Cleanest: factor a `appendChildDecorations(f)` (the
   child-recursion half) and call `appendSelfDecorations(f)` + `appendChildDecorations(f)` separately on
   the clipping path; the non-clipping path keeps calling `appendDecorations(f)` (self + children) as
   today. Non-clipping output is unchanged.
2. **The positioned layer is split** into the CB-owned subset (inside the bracket) and the escaped subset
   (outside). For a **non-clipping** fragment (`!f.Clips`), there is no split — the existing single
   positioned loop runs and `PositionedClip` is never consulted, so the non-positioned/non-clipping path
   is byte-identical.

The non-`Clips`, non-stacking, and non-BFC paths are all untouched.

### Why the abs-pos escape falls out

- A positioned descendant whose CB is an **ancestor** of `f` bubbles past `f` (the in-flow walkers skip
  `IsPositioned`) and lands on the ancestor's `Positioned` — it is painted in the *ancestor's*
  positioned phase, never inside `f`'s bracket. **Not clipped by `f`.** ✓
- A positioned descendant whose CB **is `f`** lands on `f`'s own `Positioned` with `PositionedClip=true`
  and is painted inside `f`'s bracket. **Clipped by `f`.** ✓
- An in-flow, **non-positioned** overflowing descendant is painted by `appendChildDecorations` /
  `appendContent`, inside the bracket. **Clipped.** ✓ (A `position:relative` descendant is `IsPositioned`,
  so it is lifted to a positioned layer rather than painted here — see the scoped limitation above; clipping
  it is deferred.)

This is the one place the slice adds new mechanism beyond "bracket the contents". It is pinned by the
adversarial test matrix below. The matrix's case (ii) — an abs-pos child whose CB is an ancestor outside
the clip → **not** clipped — is the supported, correct escape; the relative-descendant-of-a-non-positioned-
clip gap is **not** tested because it is explicitly deferred (testing would assert the un-clipped output we
intend to fix later).

## Float-height enclosure (`block.go`, `floats.go`)

**The gap (5a):** `layoutBlock` derives `contentHeight` from the in-flow cursor only; floats are
invisible to the cursor, so a box whose only children are floats gets zero content height (the dropped
`float-row` golden / degenerate 1×1 page).

**The fix:** a box that establishes a BFC (now including `overflow ≠ visible`) encloses its floats — its
content height is `max(in-flow content bottom, the bottom of every float in its own BFC)` (CSS 10.6.7).

- **`floats.go`** — add `maxBottom() float64`: the largest `f.y + f.h` over `c.floats`, or 0 if empty
  (in the BFC-root frame the context is queried in). Pure; unit-tested.
- **`block.go` (`layoutInterior`)** — a BFC box already creates a fresh `childFC`. After laying out its
  interior, surface `childFC.maxBottom()` onto `interior` (a new field `floatsBottom float64`) alongside
  the existing `bfcFloats`. The fresh BFC resets the band origin to 0, so `maxBottom()` is already in the
  box's **local content-top-0 frame** — directly comparable to `in.contentHeight`.
- **`block.go` (`layoutBlock`)** — **only when `newBFC`**, fold the floats' bottom into the content
  height after the existing in-flow `contentH` is computed and before `borderH`:

  ```go
  if newBFC && in.floatsBottom > contentH {
      contentH = in.floatsBottom
  }
  ```

  A non-BFC box is unchanged (floats still do not extend it — correct CSS).

**No-op guard (load-bearing — handoff calls it out):** an inline-block (or any BFC box) with **no**
floats has `maxBottom() == 0` and `floatsBottom == 0`, so the `max` never fires — an existing non-float
BFC's height is **unchanged**. Pinned by a test asserting an inline-block-with-no-floats height is
identical to pre-5c.

**Frame correctness:** the enclosure compares `in.floatsBottom` (BFC-root frame, origin 0 for a fresh
BFC) with `in.contentHeight` (the same local frame). A `height:auto` BFC box then derives its border
height from this enclosed content height; a **fixed-height** BFC box keeps its fixed height (the
`resolveFixedHeight` branch already overrides `contentH`), so enclosure does not override an explicit
height — correct CSS (a fixed height that is shorter than the floats still clips them with
`overflow:hidden`).

## Sibling-BFC float avoidance (`block.go`)

**The gap (5a):** an in-flow block's *inline content* narrows around a float (per-line
`leftEdge`/`rightEdge`), but a box that establishes its **own** BFC lays its **border box** out at the
parent content-left at full width — it would overlap an outer float. CSS 9.5 forbids that: the BFC box's
border box sits **beside** the float.

**The fix (in `layoutBlockChildren`, the in-flow-child branch):** when the child **establishes a BFC**
(`establishesNewBFC(child)`, now including `overflow ≠ visible`), query the **parent's** float context
`fc` at the child's band and shift/narrow the child's border box past any intruding float.

- The float context in scope (`fc`) is the **parent's** BFC context — the child gets a *fresh* context
  for its own interior, but it is still **placed within** the parent's float band.
- Before laying the child out, estimate its band height `h` (the child's resolved/provisional height as
  the float code already does) and query at the cursor:
  `left := fc.leftEdge(bandOriginY+startY, h)`, `right := fc.rightEdge(bandOriginY+startY, h)`.
- If a float intrudes (`left > contentX + ε` or `right < contentX + contentW - ε`):
  - **Narrow + shift**: lay the child out at originX `left` with available width `right - left` so its
    border box fits in the gap beside the float.
  - **If the gap can't fit the child's resolved width**, drop the child below the float band:
    `startY = fc.nextDropY(bandOriginY+startY, h) - bandOriginY` (reusing the existing drop helper), then
    re-query at the lower band (full width below the float). Logged as the CSS 9.5 fallback.
- A **non-BFC** in-flow child is **unchanged**: its border box still spans full width (background/border
  slide under the float) and only its inline content narrows via the existing per-Y queries — preserving
  every existing non-BFC golden.

**Composition with enclosure:** an `overflow:hidden` box can both **enclose its own** floats (previous
section) and be **shifted past an outer** float (this section) — the two are independent and compose. The
test matrix covers the combination.

**Provisional-height note:** the band query needs the child's height before layout; the band is coarse
enough that the height feedback is not iterated (documented, consistent with 5a's per-line line-height
approximation). The child's resolved height (or a provisional content height) is used.

## Degradation

Every unsupported case degrades without aborting the page (recovery is at the page boundary —
`Engine.Layout` already recovers from panics):

- `scroll`/`auto` clip exactly like `hidden` (no scroll position / scrollbar chrome) — logged once where
  a `scroll`/`auto` box is seen, so the approximation is visible.
- `overflow-x`/`overflow-y` is an unrecognized property and is dropped (only the `overflow` shorthand is
  modeled) — documented.
- A BFC box that cannot fit beside an outer float in the remaining band is dropped below it (CSS 9.5
  fallback) rather than overflowing — logged.
- An unknown `overflow` value normalizes to `visible` (no clip) — the graceful default.

No new `ErrUnsupported` is introduced — these are quiet approximations, consistent with the engine's
flex/grid/table fallback and the float/positioning degradations.

## Tests

Mirroring sub-projects 3, 4, 5a, & 5b (unit + WPT reftests + eyeballed goldens):

- **`pkg/css`** — `overflow` parse + cascade: each keyword (`visible`/`hidden`/`scroll`/`auto`), the
  initial value (`visible`), an invalid value dropped, and **not inherited** (a child of an
  `overflow:hidden` parent computes `visible`).
- **`pkg/layout/css` (clip geometry / AppendItems):**
  - `clips(b)` and `paddingBox`/`ClipRect` (border box deflated by border widths) unit checks.
  - `establishesNewBFC(b)` is **true** for `overflow:hidden`/`scroll`/`auto`, **false** for `visible`.
  - **AppendItems clip bracket**: a clipping box emits `ClipPushKind` (carrying the padding box) before
    its contents and `ClipPopKind` after; its **own** background/border items fall **before** (outside)
    the push; nested clipping boxes nest their brackets (inner push between outer push and pop).
  - **Byte-identical guard**: a **non-clipping** page's `AppendItems` output is `reflect.DeepEqual` to
    the pre-5c sequence (no clip items emitted).
  - **Adversarial stacking matrix (the load-bearing rule):** (i) an in-flow overflowing child → its items
    fall **inside** the clip bracket; (ii) an abs-pos child **whose CB is an ancestor outside** the
    clipping box → its items fall **outside** the bracket (not clipped); (iii) an abs-pos child **whose
    CB is the clipping box** → **inside** the bracket (clipped); (iv) two nested clipping boxes → the
    inner bracket is nested within the outer.
- **`pkg/layout/css` (float interactions):**
  - **Enclosure**: an `overflow:hidden` box whose only children are floats has height == the floats'
    bottom (not 0); **no-op guard** — an inline-block with **no** floats has its pre-5c height (unchanged).
  - **Sibling avoidance**: an `overflow:hidden` box laid out next to an outer left float is shifted to
    the float's right edge (its border box does not overlap the float's margin box); a **non-BFC** sibling
    in the same position is unchanged (border box still full-width, only inline content narrows); a BFC
    box too wide for the remaining gap **drops below** the float.
  - **Flag combinations** (5a/5b's worst-miss class): a clipped box **containing** a float (clips its
    own float layer); a clipped box that is **itself positioned** (relative/abs); an `overflow:hidden`
    BFC **next to** an outer float that **also encloses** its own floats (avoidance + enclosure together).
- **`pkg/layout/paint`** — `PaintPage` drives `dev.Save()`+`PushClip` on `ClipPushKind` and `dev.Restore()`
  on `ClipPopKind` (record-device assertion of call sequence + the clip rect mapped through `mat`); plus
  a tiny raster test — a box/glyph overflowing a clipped container is cut at the padding-box edge (a
  pixel just outside the padding box is the background color; just inside is the content color).
- **`pkg/layout/inline`** — **unchanged** (overflow adds no inline primitive); existing tests stay green,
  asserting the shared core is untouched.
- **`pkg/doctaculous`** — WPT-style reftests (eyeball-free equivalences) + **goldens** (eyeballed in the
  PR):
  - **golden `overflow-hidden`** — a small `overflow:hidden` box whose child/text overflows; eyeball the
    overflow is cut at the box's padding-box edge (and the box's own border paints at full size).
  - **golden `overflow-nested`** *(if it adds signal)* — a clipping box inside a clipping box, the inner
    content cut by the intersection of both clips.
  - **golden `float-row` (restored)** — three left-floated swatches inside an `overflow:hidden` wrapper
    that now **encloses** them (the wrapper has real height; the swatches sit in a row inside it). This is
    the golden 5a had to drop (a float-only body rendered a degenerate 1×1 page).
  - **reftest `overflow-hidden`** — a clipped overflowing box == a box whose content is authored to fit
    exactly within the padding box (the visible region is pixel-identical; the overflow that the test clips
    is off-page / immaterial in the reference, or the reference simply omits it).
  - **reftest `float-row` (restored)** — the enclosing `overflow:hidden` float wrapper == an
    explicit-height reference rendering the same swatches at the same coordinates.
  - **All existing goldens and reftests stay byte-identical** — non-clipping pages emit no clip items, and
    enclosure is a no-op for non-float BFCs. Verified by running `TestHTMLGolden`/`TestWPTReftests`/
    `TestDOCXGolden` **without** `-update` and confirming no diff, and `git status --short
    pkg/doctaculous/testdata pkg/render/raster/testdata` shows **only new files**.

`go test -race ./...` clean. DOCX is unaffected (the shared inline core is untouched).

New fixtures land in the same PR (per CLAUDE.md testing rules). Each unsupported case above
(`scroll`/`auto` clipping like hidden, the sibling-BFC drop-below fallback) has a test asserting the
graceful behavior.

## Deferred (carried to the next handover)

- **`overflow-x` / `overflow-y`** (axis-specific clipping/scroll) and **`overflow:clip`** — only the
  `overflow` shorthand with `hidden/scroll/auto` is modeled.
- **Scrollbars / scroll position** — `scroll`/`auto` clip like `hidden`; there is no scroll affordance in
  the single-tall-page model.
- **A positioned descendant of a *non-positioned* `overflow:hidden` box is not clipped** — such a box is
  a BFC but not a stacking context, so a `position:relative`/abs descendant bubbles past it to a higher
  positioned layer and paints outside the clip (browsers clip it). The fix needs the clip to reach an item
  range that an ancestor's positioned phase emits — the same seam the full-z-index slice reworks; grouped
  there. The common in-flow overflow case and abs-pos-with-CB-the-clipping-box ARE clipped (see "The clip
  in the stacking pass").
- **A float inside a `position:relative` (non-BFC) box not riding the relative paint offset** (5b
  deferral) — a relative box still does not establish a BFC, so its float escapes to the enclosing float
  layer. Unchanged by this slice unless a test surfaces it; left noted.
- **Full CSS 2.1 Appendix E z-index stacking** (negative/numeric) — positioned boxes still paint in
  document order. `PositionedClip` (this slice) and the positioned-layer loop remain the seam the
  z-index slice re-points at a z-sorted, sub-layered order.
- Still open and NOT this slice's concern unless touched: the **precise static-position solve** for an
  all-`auto` abs box, abs `width:auto` **shrink-to-fit**, abs `margin:auto` centering, a `bottom`-only
  auto-height abs box, **`position:relative` on any inline-level box**, and the replaced/inline deferrals
  (`object-position`, ratio-preserving min/max, percentage-height basis, `background-image`, full
  `vertical-align`, `margin:auto` centering, the margin-collapse edge cases).

## Notes for the implementer (lessons carried from 5a/5b — these earned their keep)

- **Sandbox blocks the Go build cache + TLS** — run `go`/`golangci-lint`/`gofmt` (and `gh pr create`,
  `git push` over HTTPS) with the sandbox disabled. This repo's `origin` is HTTPS, so push with the
  sandbox disabled.
- **Editor diagnostics LAG** — after adding a field/file you'll see stale "undefined"/"unknown
  field"/"unused"/"redeclared" errors and phantom `zz_*` scratch files. Trust `go build`/`go test`, not
  the panel. After any review subagent, `find . -name 'zz_*' -delete` and confirm `git status` is clean.
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages, not the repo root. The repo uses **NO `//nolint`** and **declines all "modernize" hints**
  (`max()`/`min()`/`slices.*`/range-over-int) — keep explicit `if x < y { x = y }` clamps and indexed
  loops. golangci-lint **does** flag `if !(a && b)` (QF1001) and an **unused unexported field/func** —
  every new field/func (e.g. `Clips`, `ClipRect`, `PositionedClip`, `floatsBottom`, `maxBottom`) must be
  read/called in the same PR.
- **The zero-value `Length` trap** — a `cssbox.ComputedStyle`/`Box` literal that omits
  `Width`/`MaxWidth`/offsets reads as explicit `0`, not the cascade's `auto`/`none`. Test fixtures built
  as raw structs must set these to `UnitAuto` where they mean auto. Reuse 5b's `posStyle()`/`posBox()`
  helpers (in `positioning_layout_test.go`); extend them for overflow fixtures.
- **Test the FLAG COMBINATIONS, not each flag alone** — 5b shipped a double-translate bug for
  `float + position:relative` that unit tests missed; a holistic review caught an abs sizing bug no
  layout test covered. 5c's `overflow ≠ visible` flag combines with float/abs/relative/BFC — explicitly
  test the combinations (a clipped box containing a float; a clipped box with an abs-pos child whose CB is
  outside; an `overflow:hidden` BFC next to an outer float; a positioned + overflow box) and **eyeball
  every golden** — the golden render caught both 5b paint bugs that unit tests passed through.
- **Eyeball every new/changed golden PNG** in the PR (the controller, via the Read tool — the implementer
  has no image vision). A "passing" golden can still be WRONG (5b's first `position-relative` golden used
  a documented no-op and didn't exercise the feature). Author fixtures that exercise the SUPPORTED path
  and verify the eyeball shows the feature: the `overflow-hidden` golden must visibly cut overflow; the
  restored `float-row` golden must show the wrapper enclosing its floats.
- **Confirm no pre-existing golden changed** (`git status --short pkg/doctaculous/testdata
  pkg/render/raster/testdata` shows only new files). A clip/paint-pass change is high-risk for silently
  reordering or clipping existing pages — the no-overflow pages MUST stay byte-identical (run the goldens
  without `-update` and confirm no diff). The enclosure change extends content height for BFC boxes —
  verify it does NOT change an existing non-float BFC's height (an inline-block with no floats must be
  unchanged).
- **The two-stage review (spec + code-quality, per task) earns its keep**, and a **holistic final
  review** on the assembled diff is mandatory. Have spec reviewers **verify load-bearing geometry/paint-
  order with throwaway adversarial tests** (for 5c: the clip-vs-stacking abs-pos escape and the
  enclosure height math), and **delete the throwaways** (confirm `git status` clean).
- **Propagate review fixes back into this spec/the plan**, and **update CLAUDE.md's Done/TODO** when the
  PR lands (move overflow clipping + the two float interactions out of the §6 TODO into Done).

## Review fixes folded in

The spec self-review and the per-task two-stage reviews (spec + code-quality) during implementation
surfaced these, all folded into the shipped slice:

1. **The relative-descendant-escapes-a-non-positioned-clip gap** — found during the spec self-review (not
   implementation): a `position:relative`/positioned descendant of a *non-positioned* `overflow:hidden`
   box bubbles past the clip to a higher positioned layer and paints unclipped, because a non-positioned
   clipping box is a BFC but not a stacking context. Browsers clip it. Scoped **out** and documented as a
   deferral (grouped with the full-z-index slice, whose machinery the fix needs); the common in-flow
   overflow case and abs-pos-with-CB-the-clipping-box are clipped correctly. See "The clip in the stacking
   pass" and "Deferred".

2. **The abs-pos clip-escape rule was verified adversarially, end-to-end** — the Task 4 spec reviewer
   built a throwaway fragment tree (CB-owned + escaped positioned descendants) and confirmed the escaped
   one paints after `ClipPop`; the Task 6 reviewer went further, temporarily inverting the production CB
   rule to prove `TestClipAbsChildOutsideCBNotClipped` FAILS when an escaped child is wrongly clipped, then
   reverting. The rule is load-bearing and is now covered both by hand-built-fragment unit tests (Task 4)
   and full-layout tests (Task 6).

3. **The `PositionedClip` parallel-slice invariant** — the Task 4 code reviewer flagged that the parallel
   `PositionedClip`/`Positioned` slices must stay the same length at *every* append site or a CB-owned
   entry could under-clip. The Task 5 review then verified, with an exhaustive grep, that all four
   `Positioned` append sites (`layoutTree`, `layoutBlock` consume, `placeFloat`, `resolveAbsolute`) have an
   adjacent matching `PositionedClip` append and that no other code path mutates either slice.

4. **The sibling-BFC placement loop was proven spin-free** — the Task 8 reviewer traced `nextDropY`
   semantics (returns a float bottom strictly greater than the query Y, else the query Y unchanged) to
   confirm `startY` strictly increases on the only continuing branch, bounded by the finite set of float
   bottoms, and ran a pathological 5000px-child throwaway that terminated immediately.

5. **A QF1001 (De Morgan) lint slip in a test** — the Task 6 test assertions used the plan's `if \!(a && b)`
   form, which golangci-lint v2 flags (QF1001). It was caught during a later task's lint run and rewritten
   to the equivalent De Morgan form (`if a >= b || ...`), preserving the assertions exactly. Lesson: write
   test assertions in De-Morgan'd form in the plan, and run `golangci-lint` (not just `gofmt`) per task —
   the final verification (Task 14) confirmed `golangci-lint run` reports 0 issues across all changed
   packages.

No review changed a load-bearing design decision; the architecture shipped as designed. Every existing
golden and reftest stayed byte-identical (verified by running the goldens without `-update`), and the two
new goldens were eyeballed in review (the `overflow-hidden` golden visibly cuts the oversized child at the
padding-box edge with the border at full size; the restored `float-row` golden shows the `overflow:hidden`
wrapper enclosing its three floated swatches).
