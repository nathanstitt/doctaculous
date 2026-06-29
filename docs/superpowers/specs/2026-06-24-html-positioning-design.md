# HTML rendering — positioning (relative / absolute / fixed) (sub-project 5b)

**Status:** Designed. Branch `feat/html-positioning` (off `feat/html-floats`, or off `main` if the
stack has merged). Roadmap §6 "positioning" slice. Predecessors: sub-projects 1 (CSS parse+cascade),
2 (box generation), 3 (block+inline normal flow), 4 (replaced content + images), 5a (floats + clear,
`2026-06-24-html-floats-design.md`). Successor: 5c (overflow clipping) and a later 5b-2 (full CSS 2.1
Appendix E z-index stacking sort — see "Deferred").

## Goal

`position: relative | absolute | fixed` moves a box from the position it would occupy in normal flow:

- **`relative`** — the box stays in normal flow (it still occupies its in-flow space, and siblings are
  unaffected), but its **painted** position is shifted by `top`/`right`/`bottom`/`left`. It also becomes
  a **positioned box**, so it paints in a later CSS painting step than normal-flow content, and it is the
  **containing block** for any absolutely-positioned descendant.
- **`absolute`** — the box is **taken out of flow** (siblings flow as if it were not there) and
  positioned by `top`/`right`/`bottom`/`left` against its **containing block**: the content box of the
  nearest **positioned ancestor** (any ancestor whose `position` is not `static`), or the **initial
  containing block** (the page) if there is none.
- **`fixed`** — like `absolute` but the containing block is the **viewport**. In this toolkit's
  single-tall-page model the viewport is the whole page, so `fixed` is positioned against the page
  rectangle (it does not "stick" on scroll — there is no scroll).

Before this slice, the box already records `Position` (`pkg/layout/cssbox` `PositionKind`:
`PosStatic/PosRelative/PosAbsolute/PosFixed`), but box generation hard-codes `positionOf` to
`PosStatic` (the stub in `pkg/layout/css/build.go`) and the layout engine ignores it — every box lays
out in normal flow. This is the same graceful no-op starting point floats had in 5a. This slice wires
`position` + the offset properties + `z-index` through the CSS cascade and box generation, then
implements relative offsetting and out-of-flow absolute/fixed placement in the CSS layout engine
(`pkg/layout/css`), and **generalizes the float paint phase split** into a stacking pass that paints
positioned boxes in their own layer.

**Scope (agreed): relative + absolute/fixed + MINIMAL z-index.**

In:

- `position: relative | absolute | fixed | static` on block-level boxes (and inline-level, via
  blockification of an abs/fixed box — see "Box generation").
- `top` / `right` / `bottom` / `left` offset properties (lengths + `auto`) and `z-index` (an integer or
  `auto`) parsed onto `ComputedStyle`. `z-index` is **carried but not yet used to sort** (deferred).
- **`relative`**: paint-time offset of the box (and its whole subtree) by the resolved offsets, with
  flow unchanged. `top` wins over `bottom`, `left` wins over `right` (CSS 9.4.3 over-constrained
  resolution; an `auto` offset is 0).
- **`absolute` / `fixed`**: taken out of flow; the box does not affect its siblings' flow and is not
  part of the in-flow vertical cursor. Positioned against the containing-block rectangle (nearest
  positioned ancestor's **content box**, or the page) using `top`/`left`/`right`/`bottom` and the box's
  used width/height. Laid out in a **second pass** (see "Two-pass abs-pos").
- A **minimal stacking pass**: positioned boxes (relative, absolute, fixed) paint in their own layer,
  **after** in-flow block decorations, floats, and in-flow inline content within their stacking context
  — generalizing 5a's phase-split `AppendItems`. A positioned box (and the root) establishes a
  **stacking context** so it (and its positioned descendants) paint as a self-contained unit in the
  parent's order.

Out (degrade gracefully — laid out in normal flow / approximately, with a `logf` line; none panic):

- **Full CSS 2.1 Appendix E z-index ordering** — `z-index` integers are parsed but NOT sorted on:
  positioned boxes paint in **document order** within their stacking context's positioned layer
  (equivalent to every positioned box being `z-index:auto`/`0`). **Negative** z-index (painting a
  positioned box *behind* in-flow content) and **numeric** z-index ordering are deferred to a 5b-2
  slice; the paint seam is kept clean so that slice only re-points the positioned-layer collection at a
  z-sorted order. This is the agreed "minimal z-index" cut. Documented + logged when a non-auto
  `z-index` is seen so the degradation is visible.
- **Static-position fallback for an over-constrained / all-`auto` abs-pos box** — when all of `top`,
  `bottom` (and `left`/`right`) are `auto`, CSS places the box at its **static position** (where it
  would have been in flow). A precise static-position solve (which requires knowing where the box's
  hypothetical in-flow box would sit) is approximated: an all-`auto`-offset abs-pos box is placed at the
  containing block's content-box **top-left** (a documented approximation; the common case — an abs-pos
  box with at least one explicit offset — is exact). `margin:auto` on an abs-pos box (CSS 10.3.7
  centering) stays at 0 (the existing `usedEdges` auto→0 behavior).
- **Percentage offsets against an auto-height containing block** — a `%` `top`/`bottom` resolves against
  the containing block height; when that height is auto (content-derived) the resolution still uses the
  resolved content height (known by the second pass), which is correct for our model; a `%` against a
  still-indeterminate height degrades to 0.
- **Abs-pos `width:auto` shrink-to-fit** — an abs-pos box with `width:auto` and both `left`+`right`
  specified gets `width = cb.width - left - right` (the constraint solve); with `width:auto` and at most
  one of `left`/`right` it uses the normal resolved width (containing-block fill for a non-replaced box,
  intrinsic for a replaced box) rather than the CSS shrink-to-fit (min/max-content) width — the same
  shrink-to-fit approximation floats already make, documented.

## Architecture / seams

The work is almost entirely in `pkg/layout/css`, mirroring how 5a's float logic was contained there.
The shared inline core (`pkg/layout/inline`) is **untouched** — positioning needs no new inline
primitive (unlike floats, which needed `BreakNext`). New machinery:

- **`pkg/css` (cascade)** — additive `Position string`, `Top`/`Right`/`Bottom`/`Left Length`, and a
  `ZIndex` (`int` + an `auto` flag) on `ComputedStyle`, parsed like the other keyword/length properties
  (`float`/`object-fit`/the offsets are lengths like `width`). **Not inherited** (position and offsets
  are not CSS-inherited properties).
- **`pkg/layout/css/build.go`** — `positionOf(cs)` stops being a stub: it maps `cs.Position` to a
  `PositionKind`. An absolutely/fixed-positioned element **blockifies** (CSS 9.7, like a float), reusing
  the `applyFloatBlockify` pattern (renamed/extended to `applyPositionBlockify` or a shared
  `applyBlockify`). The offsets and z-index are carried onto the box (new fields on `cssbox.Box`, or read
  from `Box.Style` at layout time — see "Box-level data").
- **`pkg/layout/css/positioning.go` (new)** — the positioning geometry, the analogue of `floats.go`:
  pure helpers that resolve a `relative` box's paint offset, an `absolute`/`fixed` box's used rectangle
  against a containing-block rect, and the over-constrained offset resolution (top-vs-bottom,
  left-vs-right). Unit-tested in isolation with adversarial cases.
- **`pkg/layout/css/block.go`** — threads a **containing-block rectangle for abs-pos** down the layout
  recursion (the `posCB`, analogous to how `bandOriginY`/`fc` thread for floats): each positioned box
  updates it for its subtree. In-flow layout **collects** abs/fixed boxes (deferred, not laid out
  in-line) and records each with the `posCB` in scope; relative boxes lay out normally but carry their
  offset for the paint pass.
- **`pkg/layout/css/fragment.go`** — `Fragment` gains positioning fields (`IsPositioned bool`,
  `RelOffsetX/Y float64`, `IsStackingContext bool`, and a `Positioned []*Fragment` layer analogous to
  `Floats`). `AppendItems` generalizes the float phase split into the **stacking pass** (decorations →
  floats → in-flow content → **positioned layer**), with a positioned/stacking-context fragment painting
  as a self-contained unit.

### Box-level data (where offsets / z-index live)

The offsets and z-index are needed at layout time. Two options: add fields to `cssbox.Box`, or read them
from `Box.Style` (the `ComputedStyle` is already carried on every box). **Decision: read from
`Box.Style`** at layout time (the engine already reads `Box.Style` for width/margins/etc.), and add only
the already-present `Position PositionKind` hint (which box-gen sets via `positionOf`). This keeps
`cssbox.Box` minimal and consistent with how the engine consumes the rest of the style. `cssbox.Box`
already has `Position`; no new `cssbox.Box` field is required. (If a later need arises to denormalize
offsets onto the box, that is a mechanical change — but the style is sufficient now.)

### Why the abs-pos collection is concurrency-safe

Like the `floatContext`, the abs-pos collection is per-`Layout`-call mutable state threaded by pointer
through one goroutine's recursion; it never escapes the call, and the fragment tree it helps build is
immutable by the time `Layout` returns. The project's concurrency contract (a laid-out document is
read-only after `Layout`) is preserved: the fan-out is across pages, each a separate single-goroutine
`Layout`. No new cross-goroutine shared state.

## The positioning geometry (`pkg/layout/css/positioning.go`)

Pure functions, directly unit-testable:

- **`relativeOffset(b, cbW, cbH) (dx, dy float64)`** — resolves a `relative` box's paint offset from
  `top`/`right`/`bottom`/`left` against the containing block dimensions (percentages resolve against
  `cbW` for left/right, `cbH` for top/bottom). CSS 9.4.3: `left` wins over `right` (`dx = left` if
  `left` is not auto, else `dx = -right` if `right` is not auto, else 0); `top` wins over `bottom`
  (`dy = top` if not auto, else `-bottom` if not auto, else 0). A `relative` box's offset shifts only
  its painted position, never the flow.
- **`absRect(...)`** — resolves an `absolute`/`fixed` box's **used border-box rectangle** and its used
  **content width** (for the interior pass), given the box `b`, its resolved edges `ed` (margins/borders/
  paddings, with auto margins already 0), and the containing-block content rect `cb` (x, y, w, h in page
  space). **All offsets are measured from `cb` to the box's MARGIN edge**, so every formula carries the
  margin term explicitly (margins are 0 in the common case, but `margin-left:5px` on an abs-pos box is
  honored — `usedEdges` resolves explicit margins). Let `insetsX = ed.bL+ed.bR+ed.pL+ed.pR` (border-box =
  content + insetsX) and `outerX = ed.mL + ed.bL+ed.bR+ed.pL+ed.pR + ed.mR` (the margin-box width given a
  content width). The horizontal solve (CSS 10.3.7, the supported subset):
  - **`left` and `right` both specified, `width:auto`** (shrink to fit the offsets): margin-box width =
    `cb.w - left - right`; content width = `cb.w - left - right - outerX + (ed.mL+ed.mR)` =
    `cb.w - left - right - insetsX - ed.mL - ed.mR`; border-box width = content + `insetsX`;
    **border-box left** = `cb.x + left + ed.mL`.
  - **`left` specified, `width` specified** (or `right:auto`): border-box left = `cb.x + left + ed.mL`;
    content width = used `width` (de-`insetsX`'d under `border-box` sizing, as `resolveContentWidth`
    does); `right` is ignored (not over-constrained the other way in this subset).
  - **`right` specified, `left:auto`, `width` specified**: border-box width = content + `insetsX`;
    **border-box left** = `cb.x + cb.w - right - ed.mR - borderBoxW`.
  - **all of `left`/`right`/`width` auto**: border-box left = `cb.x + ed.mL` (static-position
    approximation, documented); content width = the normal resolved width (containing-block fill for a
    non-replaced box, intrinsic for a replaced box).

  The **vertical solve** is structurally parallel but its two height cases differ and are written
  separately (do NOT collapse them):
  - **`top` and `bottom` both specified, `height:auto`**: border-box height = `cb.h - top - bottom -
    ed.mT - ed.mB` (derive height from the offsets); border-box top = `cb.y + top + ed.mT`.
  - **`top` specified, `height` definite-or-content**: border-box top = `cb.y + top + ed.mT`; height is
    the content-derived (auto) or fixed height; `bottom` ignored.
  - **`bottom` specified, `top:auto`, height definite-or-content**: border-box top = `cb.y + cb.h -
    bottom - ed.mB - borderBoxH`.
  - **`top`/`bottom`/`height` all auto**: border-box top = `cb.y + ed.mT` (static approximation); height
    content-derived.

  `absRect` returns the border-box rect **plus** the used content width, so pass 2 lays the interior out
  at the resolved width (height then derives from the interior unless it was offset- or fixed-determined).
- **Over-constrained resolution** is a small shared helper used by both axes so the top/bottom and
  left/right case logic is written once. Its signature takes the axis's `cbStart`, `cbExtent`, the two
  offsets (with auto flags), the two margin terms, the insets, and the box's own definite size (auto
  flag), and returns `(borderBoxStart, contentSize)` — the four cases above map onto one parameterized
  solve per axis.

All inputs come from the already-clamped box model; the helpers are written defensively (no panic on
NaN/negative, which cannot arise).

## How positioning threads through layout (`block.go`)

`layoutBlock`, `layoutInterior`, `layoutBlockChildren`, and `layoutTree` gain a **`posCtx`** parameter
(a pointer to the abs-pos collection) and a **`posCB`** describing the current containing block for
absolutely-positioned descendants. **`posCB` is NOT a value rect computed during the in-flow pass** — the
reviewer confirmed that would capture the positioned ancestor's *provisional* coordinates before it is
shifted into final page position (the engine lays out at provisional origins and shifts via
`shiftFragment` as the recursion unwinds, mutating fragments in place — `block.go` `shiftFragment`).
Capturing a rect snapshot in pass 1 would read stale coordinates in pass 2.

Instead, `posCB` carries **the containing-block owner**: a pointer to the nearest-positioned-ancestor
`*Fragment` **and** its `*cssbox.Box` (or a `isPageCB` sentinel for the ICB/`fixed` case). The fragment
pointer is safe because pass 2 runs after the **full** pass-1 unwinding, so the ancestor fragment's
border box (`X,Y,W,H`) is final and in-place. The `*cssbox.Box` is needed because a `Fragment` stores only
its **border** box; the abs-pos containing block is the ancestor's **content** box, derived in pass 2 by
deflating the final border box by the ancestor's border+padding (`usedEdges(ancestorBox, …)`). For the
page CB (`fixed`, or an abs-pos box with no positioned ancestor) the sentinel resolves to the final page
rect (`{0, 0, viewportW, pageHeight}`), known after pass 1.

`layoutTree` (the ICB) seeds `posCB` to the page sentinel and an empty `posCtx`. A box that establishes a
positioned containing block (`position != static`) passes its **own** `(fragment, box)` as the `posCB` for
its interior; a static box passes its `posCB` through unchanged.

In `layoutBlockChildren` / `layoutBlock`, per box:

1. **`relative` box** — laid out **in normal flow exactly as today** (it occupies its in-flow space).
   After its fragment is built, mark `frag.IsPositioned = true`, compute its `RelOffsetX/Y` via
   `relativeOffset`, and record it as a stacking context (it establishes one). The offset is **applied at
   paint time** by the stacking pass (so the in-flow cursor and sibling positions are unaffected — the
   box reserves its un-offset space). A `relative` box is **also** a positioned containing block for
   abs-pos descendants: it passes its own `(fragment, box)` as the `posCB` owner to its interior (the
   abs-pos descendant resolves against the box's *in-flow* content box in pass 2 and then rides the same
   paint-time relative shift, which is equivalent to CSS resolving against the offset box — see "Relative
   offset & descendant CB" below).
2. **`absolute` / `fixed` box** — **not laid out in flow**: it does not advance the in-flow cursor, does
   not collapse margins, and is not appended to the in-flow `Children`. Instead it is **recorded** on the
   `posCtx` together with the `posCB` owner currently in scope (for `fixed`, the page sentinel rather than
   the nearest-positioned-ancestor owner), plus the stacking-context fragment that owns its paint. The
   second pass lays it out and positions it (see "Two-pass abs-pos"). Siblings flow as if the box were
   absent.
3. **`static` box** (the common case) — unchanged.

A box that establishes a positioned containing block (`position != static`) passes its **own**
`(fragment, box)` as the `posCB` owner to its interior; a static box passes the inherited `posCB` through
unchanged.

### Relative offset & descendant CB (the subtle bit, flagged for review)

A `relative` box's offset is applied to its whole painted subtree at paint time. Its abs-pos descendants
are positioned against the box's **in-flow** content rect during layout, then ride the **same**
paint-time shift. This is equivalent to CSS positioning them against the box's *offset* position, because
the descendant and the containing block move together by the identical delta — **but only if the shift
actually reaches the abs-pos descendant**, which lives on the box's `Positioned` layer, not its
`Children`. The reviewer confirmed the naive "pre-shift the fragment via `translateFragment`" approach is
a **bug**: `translateFragment`/`shiftFragment` recurse `Children` and `Floats` but **not** `Positioned`,
so they would move the relative box's in-flow subtree while leaving an abs-pos descendant un-shifted (off
by the relative offset).

**Mechanism (decided — do NOT reuse `translateFragment`/`shiftFragment` for the relative offset):** the
relative offset is applied as a **uniform translate over the flattened item range** the positioned
fragment emits. When the stacking pass paints a positioned fragment whose `RelOffsetX/Y` is nonzero, it
records the length of `dst` before flattening that fragment, calls the fragment's `AppendItems`, then adds
`(dx, dy)` to the `XPt`/`YPt` of every `layout.Item` appended in that range. Every coordinate-bearing item
(`BackgroundKind`, `BorderKind`, `GlyphKind`, `ImageKind`) carries `XPt`/`YPt`, so a per-item translate
shifts the **entire** emitted subtree — including any abs-pos descendant's items, which were emitted by
that same `AppendItems` call (the descendant is on the relative box's `Positioned` layer, painted in the
relative box's own positioned phase). This keeps `AppendItems` a pure read of the tree (the translate
mutates only the freshly-appended `dst` items, never the fragment) and guarantees the descendant rides the
shift. A small helper `translateItems(dst, start, dx, dy)` does the range translate.

The spec reviewer **verifies this with an adversarial test**: a `relative` parent offset by `(top:10,
left:20)` containing an `absolute` child at `(top:0, left:0)` — the child must paint at the parent's
offset content-box origin (i.e. the parent's in-flow content origin **plus** (20, 10)). The test asserts
the child's painted item coordinates, not just the fragment geometry, since the offset is applied at
flatten time.

## Two-pass abs-pos (`block.go` / `Engine.Layout`)

CSS lays out absolutely-positioned boxes after their containing block's size is known. The engine
therefore runs **two passes**:

1. **In-flow pass** — `layoutTree` lays out the normal-flow tree exactly as today (now also collecting
   abs/fixed boxes onto `posCtx`, each tagged with its `posCB` owner and its stacking-context owner).
   Floats and relative boxes are handled within this pass (relative = normal flow + recorded offset;
   floats unchanged from 5a). After this pass the **page height is known** (the root fragment's bottom),
   so the page rect (the ICB and the `fixed` CB) is final, **and every captured ancestor fragment has been
   shifted into its final page position** (shifts happen as the recursion unwinds), so reading an ancestor
   fragment pointer now yields final coordinates.
2. **Abs-pos pass** — for each collected abs/fixed box: resolve its containing-block **content** rect from
   the recorded `posCB` owner (deflate the ancestor fragment's final border box by `usedEdges(ancestorBox,
   …)` border+padding; or the final page rect for the page sentinel); lay out the abs-pos box's **own
   subtree** as an independent block at the resolved width (its interior is a fresh BFC; it gets its own
   `posCtx` so *its* abs-pos descendants are collected and resolved transitively); compute its used
   border-box rect via `absRect` against that CB content rect; and translate the laid-out fragment there.
   Mark `frag.IsPositioned = true` and `frag.IsStackingContext = true`. Append the resulting fragment to
   the recorded stacking-context owner's `Positioned` layer (the nearest-positioned-ancestor fragment, or
   the root fragment if none) so it paints in the positioned layer.

The collection records, per abs-pos box: the `*cssbox.Box`; the `posCB` owner `(ancestorFrag *Fragment,
ancestorBox *cssbox.Box, isPageCB bool)` (NOT a rect snapshot — see "How positioning threads through
layout"); and the stacking-context owner fragment that paints it. Because pass 2 runs after the full
pass-1 unwinding, every ancestor fragment's geometry is final and in place.

**Transitive abs-pos** (an abs-pos box containing another abs-pos box) falls out naturally: laying out an
abs-pos box's subtree in pass 2 runs the same collect-then-resolve for its own descendants (a nested
mini two-pass), since an abs-pos box is itself a positioned ancestor / new CB.

## The stacking pass (`fragment.go` — generalizing the float phase split)

5a refactored `AppendItems` into CSS 2.1 Appendix E phases for a BFC fragment: in-flow block
decorations → floats → in-flow inline content. 5b generalizes this into the **stacking pass** by adding
the positioned layer as the final phase within a stacking context:

For a fragment that establishes a **stacking context** (`IsStackingContext` — the root, and every
positioned box), `AppendItems` paints in this order (the supported subset of Appendix E):

1. the stacking context root's own background + border (`appendSelfDecorations`);
2. *(deferred: negative-z-index stacking contexts — not collected in the minimal cut)*;
3. in-flow block backgrounds/borders of the subtree (`appendDecorations`, skipping floats, nested BFCs,
   AND `IsPositioned` descendants);
4. the **float layer** (`Floats`, unchanged from 5a);
5. in-flow inline content / images / atomics (`appendContent`, skipping floats, painting nested BFCs
   atomically, AND skipping `IsPositioned` descendants);
6. the **positioned layer** (`Positioned`) — each positioned descendant's fragment painted **fully**
   (its own `AppendItems`, recursively — a positioned box is itself a stacking context here), in
   **document order** (the minimal-z-index cut: no z-sort). This subsumes z-index:0/auto positioned
   descendants;
7. *(deferred: positive-z-index stacking contexts — folded into step 6 in the minimal cut).*

**Skip predicate (decided):** steps 3 and 5 skip a child when `c.IsFloat || c.IsBFC || c.IsPositioned`
(decorations) / `c.IsFloat || c.IsPositioned` (content, with `IsBFC` painted atomically as today). The
predicate is the per-fragment **`IsPositioned`** flag (true for relative + absolute + fixed) — **not**
`IsStackingContext`: a `relative` box is positioned-and-in-flow and must be lifted out of the in-flow
passes even though (in full CSS) a `z-index:auto` relative box is not strictly a stacking context. Using
`IsPositioned` also keeps the non-positioned path identical: on a page with no positioned boxes no child
carries the flag, so every walker behaves exactly as in 5a.

A `relative` box's `RelOffsetX/Y` is applied in step 6 via the **flattened-item-range translate**
described under "Relative offset & descendant CB": the positioned-layer driver records `len(dst)`, calls
`pf.AppendItems(dst)`, then translates the appended range by the fragment's `(RelOffsetX, RelOffsetY)`.
This is the decided mechanism — **not** `translateFragment`/`shiftFragment` (which do not recurse
`Positioned` and would drop the offset on abs-pos descendants). `AppendItems` itself stays a pure read;
only the freshly-appended `dst` items are translated. (An abs/fixed fragment has zero relative offset, so
the range translate is a no-op for it — its position is already baked into its fragment coordinates by
pass 2.)

**Where positioned descendants are collected.** Like `Floats`, the `Positioned` slice is kept **separate**
from `Children` so in-flow tree order is untouched. A positioned box (any `IsPositioned`: relative, abs,
fixed) is recorded on the `Positioned` slice of its nearest **stacking-context ancestor** fragment, and —
because the in-flow walkers skip `IsPositioned` children — is **not** painted in the in-flow
decoration/content passes of any fragment between it and that ancestor. So it paints exactly once, in the
positioned layer. For a `relative` box (which IS in flow), this means its painting moves to the positioned
layer while its in-flow space stays reserved (the cursor already advanced during layout). A `relative`
box is recorded on its nearest stacking-context ancestor's `Positioned` slice during the in-flow pass (it
already has a fragment); an abs/fixed box is appended to its stacking-context owner's `Positioned` slice
in pass 2.

This is the seam the **deferred 5b-2 (full z-index)** slice extends: step 6 gains a z-index sort and steps
2/7 split the positioned layer into negative / zero / positive sub-layers. The phase factoring is written
so that slice re-points the collection, not rewrites the pass. Noted inline in the code.

**Load-bearing, flagged for the spec reviewer** (exactly as 5a flagged the float layer): the layer
ordering. The reviewer verifies with an **adversarial overlap test** — a positioned box overlapping a
float overlapping in-flow text: the positioned box must paint **above** the float, which paints above the
in-flow text's background and below... — assert the exact item order via `AppendItems` (positioned items
come after both the float items and the in-flow content items). Plus the relative-parent/abs-child CB
test above.

### Keeping non-positioned pages byte-identical

The minimal stacking cut must reproduce today's paint order for any page with **no positioned boxes**. On
such a page no child carries `IsPositioned`, so the in-flow walkers' added `|| c.IsPositioned` skip never
fires (they behave exactly as in 5a), and every BFC fragment's `Positioned` slice is empty so the
appended positioned phase emits nothing. The root is already an `IsStackingContext`-equivalent in 5a-terms
(it owns the float phases); flagging it `IsStackingContext` and appending an always-empty positioned phase
after content is a no-op for non-positioned pages. The reviewer verified this empirically: the proposed
4-phase `AppendItems` is byte-identical (same item count, `reflect.DeepEqual`) to the current 3-phase one
on a representative non-positioned tree. **Every existing golden and reftest must stay byte-identical** —
verified by running the goldens without `-update` and confirming no diff (the handover calls this out as
the highest risk of a paint-pass change).

## Box generation (`build.go`, `pkg/css`)

- **`pkg/css`** — `ComputedStyle` gains `Position string` (`"static"` default), `Top`/`Right`/`Bottom`/
  `Left Length` (`UnitAuto` default = the `auto` initial value), and `ZIndex` (modeled as `ZIndex int`
  + `ZIndexAuto bool`, default `auto`). Parsed by the existing keyword/length paths:
  `position` accepts `static|relative|absolute|fixed` (others dropped); the offsets parse as lengths
  (reusing `setLength`, with `auto` → `UnitAuto`); `z-index` accepts `auto` or an integer (a non-integer
  dropped). **Not added to `inheritFrom`** (not inherited).
- **`positionOf(cs)`** maps `cs.Position`: `"relative"`→`PosRelative`, `"absolute"`→`PosAbsolute`,
  `"fixed"`→`PosFixed`, else `PosStatic`.
- **`position` overrides `float` (CSS 9.7):** when `position` is `absolute` or `fixed`, `float` computes
  to `none`. Box generation enforces this precedence — in `generate`, after setting `b.Position`, if the
  box is abs/fixed-positioned it forces `b.Float = FloatNone` (so the abs-pos box is collected as
  out-of-flow positioned, **not** placed by the float path). Without this, `floatOf` (independent of
  position) would leave `Float=FloatLeft` and the layout engine's float branch — which fires before the
  positioned handling — would mis-place the box as a float. This is a flag-combination the reviewer
  flagged; it lands with a `build_test.go` case (`position:absolute; float:left` ⇒ `Float==FloatNone,
  Position==PosAbsolute`).
- **`position:relative` + `float`:** relative does **not** override float, so a `float:left;
  position:relative` box stays a float (placed out of flow at the float edge) **and** is positioned
  (carries a relative paint offset). Mechanism: `placeFloat` computes `relativeOffset` for the box and
  stamps `RelOffsetX/Y` + `IsPositioned` on the float fragment; the float-layer paint
  (`appendFloats`/the positioned-layer translate) applies the offset to the float fragment's emitted item
  range, exactly like a positioned in-flow box. The float fragment is NOT additionally placed on a
  `Positioned` slice (it paints via the `Floats` layer); the relative offset rides along via the same
  flattened-range translate applied when emitting that float. A `build_test.go` + geometry test cover the
  combination (`float:left; position:relative; top:5; left:5` ⇒ placed at the float edge **plus** (5,5)
  at paint).
- **Blockification**: CSS computes `display` to a block-level value on an absolutely/fixed-positioned
  element (CSS 9.7), exactly as for a float. Box generation, when the element is abs/fixed-positioned and
  inline-level, classifies it as block-level — reusing the `applyFloatBlockify` mechanism (generalized to
  cover "float OR abs/fixed position", e.g. renamed `applyBlockify`). A `relative` inline box is **not**
  blockified (relative positioning does not change `display`); it stays inline. A positioned `<img>`
  stays `BoxReplaced` (replaced sizing handles a block-level replaced box), like a floated `<img>`.
- **`position:relative` on an inline-level box is a no-op this slice** — relative offset takes effect
  only on **block-level** boxes. Two reasons: (a) a `position:relative` text-only inline box
  (`<span style="position:relative">text</span>`) has no fragment of its own (inline-box decoration is
  deferred — the IFC contributes only the box's text leaves' glyphs, `inline.go`), so there is nothing to
  carry `RelOffsetX/Y`; (b) an inline **atom** (`inline-block`/replaced) DOES get a fragment, but it is
  positioned on the line by the inline formatting context via `translateFragment` (which moves the atom's
  border box but does not consult `RelOffsetX/Y` or lift the atom into a positioned layer), so a relative
  offset on it is also dropped. Both paint in normal flow without the offset (no panic). Block-level
  relative positioning is exact. This limitation is removed when the IFC honors a relative atom's offset
  (a later inline-fidelity slice); the goldens use block-level boxes to exercise relative positioning.

## Degradation

Every unsupported case degrades without aborting the page (recovery is at the page boundary):
- a non-auto `z-index` is parsed but not sorted on (positioned boxes paint in document order) — logged so
  the limitation is visible;
- an all-`auto`-offset abs-pos box is placed at its containing block's top-left (static-position
  approximation) — logged;
- `position:relative` on any **inline-level box** (text-only inline OR an inline-block/replaced atom) is
  a no-op — relative offset takes effect only on block-level boxes (see Box generation);
- `margin:auto` on an abs-pos box stays 0 (no centering);
- an abs-pos `width:auto` without both `left`+`right` uses the normal resolved width (shrink-to-fit
  approximation), consistent with floats.
No new `ErrUnsupported` is introduced — these are quiet approximations, consistent with the engine's
flex/grid/table fallback and the float degradations.

## Tests

Mirroring sub-projects 3, 4, & 5a (unit + WPT reftests + eyeballed goldens):

- **`pkg/css`** — `position`/`top`/`right`/`bottom`/`left`/`z-index` parse + cascade: each `position`
  keyword, the initial values (`static`, offsets `auto`, z-index `auto`), an invalid `position` dropped,
  an integer + `auto` z-index, and a non-integer z-index dropped. Plus: position/offsets/z-index are
  **not inherited** (a child without its own `position` is `static` even under a positioned parent).
- **`pkg/layout/css/positioning_test.go`** — the positioning geometry in isolation (adversarial,
  throwaway where useful): `relativeOffset` for top/left, bottom/right (negative), the top-wins /
  left-wins over-constrained cases, and `auto`→0; `absRect` for left+top, right+bottom (placed against
  the far edges), `left`+`right`+`width:auto` (width = solve), and the all-`auto` static approximation.
  These pin the load-bearing math.
- **`pkg/layout/css`** — fragment-geometry assertions: a `relative` box's in-flow **fragment** is
  unchanged (its `X,Y` are the un-offset in-flow position; the offset is paint-time) but it is flagged
  `IsPositioned` with the right `RelOffsetX/Y` (and its in-flow siblings are NOT shifted); an `absolute`
  box is removed from flow (its siblings stack as if it were absent) and lands at the
  containing-block-relative rect; an abs-pos box positioned against a `relative` ancestor lands at that
  ancestor's content box (NOT the page); a `fixed` box lands against the page; an abs-pos box with no
  positioned ancestor lands against the page (ICB). Plus the **flag-combination** cases the handover
  demands: a **positioned float** (`position:relative; float:left` — placed at the float edge, `IsFloat`,
  carrying a `RelOffsetX/Y`), a **positioned inline-block**, a **`position:absolute; float:left`**
  (`Float` forced to none, collected as abs-pos), and a **z-indexed abs-pos inside a relative parent**.
- **`pkg/layout/css` paint-order / paint-coordinate assertions** (via `AppendItems`, since the relative
  offset and the stacking order are flatten-time, not fragment-geometry, facts): (a) a positioned box
  overlapping a float overlapping in-flow text — positioned items fall **after** both the float items and
  the in-flow content items; (b) a **non-positioned** page's `AppendItems` output is **byte-identical**
  (`reflect.DeepEqual`) to the 5a 3-phase sequence; (c) the **relative-parent / abs-child** test — a
  `relative` parent offset `(top:10,left:20)` with an `absolute` child at `(0,0)`: the child's emitted
  item coordinates land at the parent's in-flow content origin **+ (20,10)** (proving the abs descendant
  rides the parent's paint-time relative shift via the flattened-range translate); (d) a
  `position:relative; top:5; left:5` box's emitted items are all shifted by exactly (5,5) vs. its in-flow
  fragment coordinates.
- **`pkg/layout/inline`** — unchanged (positioning adds no inline primitive); the existing tests stay
  green, asserting the shared core is untouched.
- **`pkg/layout/paint`** — unchanged path (positioned boxes reuse the existing
  background/border/glyph/image items; the relative offset is a translate). No new paint primitive.
- **`pkg/doctaculous`** — WPT-style reftests (eyeball-free equivalences) + **goldens** (eyeballed in the
  PR):
  - **golden `position-relative`** — a normal-flow row of boxes with one `position:relative; top/left`
    offset, eyeballing that it visibly shifts while its neighbors hold their place.
  - **golden `position-absolute`** — a `position:relative` container with an `position:absolute` child
    pinned to a corner (e.g. `top:0; right:0`), eyeballing the child sits at the container corner and is
    painted above the container's content.
  - **reftest `abs-pos`** — an `absolute` box at `top:T; left:L` inside a `relative` container ==
    a `static` box of the same size placed at the same coordinates by margins (a self-consistent
    equivalence our engine satisfies).
  - **reftest `relative-offset`** — a `relative` box offset by `(top:T, left:L)` == the same box placed
    at the shifted position with `position:static` + margins (paint-equivalence; flow space differs but
    the painted box matches — authored so the reserved in-flow space is off-page / immaterial to the
    compared pixels, or the reference reserves matching space).

  `TestDOCXGolden`, the existing `TestHTMLGolden`, and `TestWPTReftests` **stay green** (DOCX unaffected
  — the shared inline core is untouched; non-positioned HTML is **byte-for-byte identical** — verified by
  the no-diff golden run). `go test -race ./...` clean.

New fixtures land in the same PR (per CLAUDE.md testing rules). Each unsupported case above (all-`auto`
abs-pos static approximation, non-auto z-index document-order) has a test asserting the graceful
degradation.

## Deferred (carried to the next handover)

- **Full CSS 2.1 Appendix E z-index stacking** (5b-2): negative-`z-index` positioned boxes painting
  behind in-flow content, and numeric `z-index` ordering within a stacking context. `z-index` is parsed
  now; only the sort is deferred. The paint seam is factored so this re-points the positioned-layer
  collection at a z-sorted, sub-layered order.
- **Precise static-position solve** for an all-`auto`-offset abs-pos box (the hypothetical-in-flow-box
  position); today it approximates to the containing block's top-left.
- **A float inside a `position:relative` (non-BFC) box does not ride the relative paint offset** — a
  relative box does not establish a BFC, so its float escapes to the enclosing BFC's float layer and is
  not part of the relative box's flattened-range translate (CSS would shift it with the subtree). A
  corner case (no panic); revisit alongside relative-on-inline / the stacking-context work.
- **`margin:auto` centering** for abs-pos boxes (CSS 10.3.7) and for in-flow blocks (the carried
  `margin:auto` deferral) — both still stub to 0.
- **`overflow` clipping** (and the float-enclosure / float-across-BFC interactions) — slice 5c.
- The replaced-content and inline/flow deferrals carried from sub-projects 4 & 5a (`object-position`,
  ratio-preserving min/max, percentage-height basis, `background-image`, full `vertical-align`,
  shrink-to-fit width, clearance) remain open.
