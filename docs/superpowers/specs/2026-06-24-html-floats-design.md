# HTML rendering — floats + clear (sub-project 5a)

**Status:** Implemented. Branch `feat/html-floats` (off `feat/html-replaced-images`,
or off `main` if the stack has merged). Roadmap §6 "floats + positioning", **floats slice only** —
positioning (relative/absolute/fixed + z-index) and `overflow` clipping are deliberately split into
their own later slices (5b, 5c). Predecessors: sub-projects 1 (CSS parse+cascade), 2 (box generation),
3 (block+inline normal flow, `2026-06-23-html-block-inline-flow-design.md`), 4 (replaced content +
images, `2026-06-24-html-replaced-images-design.md`).

## Goal

`float: left | right` takes a box out of normal flow and shifts it to the left or right edge of its
containing block at the box's current vertical position; in-flow content then shortens to avoid the
placed float — **line boxes** wrap beside it (text flows around a figure) and the **available width of
subsequent in-flow block boxes** narrows to the float band. `clear: left | right | both` moves a box
below the bottom of preceding floats. Multiple floats on the same side **stack** horizontally and
**wrap to a new row** when the next one does not fit. Floats paint in their own CSS layer: after in-flow
block backgrounds/borders but before in-flow inline content.

Before this slice, the box already records `Float`/`Position` (`pkg/layout/cssbox`) but box generation
hard-codes them to `FloatNone`/`PosStatic` (the `floatOf`/`positionOf` stubs in `pkg/layout/css/build.go`),
and the layout engine ignores both — every box lays out in normal flow. This slice wires `float`/`clear`
through the CSS cascade and box generation, then implements float placement and avoidance in the CSS
layout engine (`pkg/layout/css`). The shared inline core (`pkg/layout/inline`, used by DOCX too) stays
**float-agnostic** — it gains one additive, DOCX-inert primitive (`BreakNext`, one greedy line at a
given width) but no float concept, and DOCX keeps calling the existing fixed-width `Break` unchanged.

**Scope (agreed): "core + float-aware block width", with the float paint layer built now.**

In:

- `float: left | right | none` and `clear: left | right | both | none` on block-level boxes.
- A single float taken out of flow, positioned by its **margin box** at the containing block edge.
- **Line boxes** in an inline formatting context shorten around floats, re-querying the available
  width per line (so a float indents the first N lines of a paragraph, then lines below the float's
  bottom return to full width).
- **Float-aware block width**: a subsequent in-flow block box's line-box content placement narrows to
  the float band when floats intrude into its vertical range.
- Multiple floats on the same side **stack** and **wrap to a new row**; opposite-side floats narrow
  from both edges.
- `clear` drops a box below the bottom of all floats matching the cleared side(s).
- The **float paint layer** (CSS 2.1 Appendix E painting order within a stacking context): in-flow
  block decorations → floats → in-flow inline content.

Out (degrade gracefully — laid out approximately / in normal flow, with a `logf` line; none panic):

- **Float wider than its containing block** — placed at the edge and allowed to overflow the band
  (it does not wrap); subsequent floats clear below it.
- **Shrink-to-fit width for `float` + `width:auto`** — approximated by the normal resolved width
  (which for the common `<img>`/explicit-width/`%`-width figure is correct); a true min-content /
  max-content shrink-fit solve is deferred and documented.
- **Floats intruding across an explicit BFC boundary** — a nested BFC (inline-block today;
  `overflow≠visible` with slice 5c) does not inherit its parent's float context, and the engine does
  not yet shorten a sibling BFC away from an outer float (CSS does). Documented; revisited in 5c.
- **`float` on an inline-level box** — CSS computes `display` to a block-level value; box generation
  blockifies a floated inline so it lays out as a float (see "Box generation").
- **Negative-margin float overlap tricks**; **`clear` interaction with margin collapsing / clearance**
  (the precise clearance algorithm of CSS 8.3.1) — `clear` here simply lowers the margin top edge to
  clear the floats; the carried margin-collapse edge case stays deferred.

## Architecture / seams

The work is almost entirely in `pkg/layout/css`. New machinery:

- **`pkg/layout/css/floats.go` (new)** — the `floatContext`: a mutable record of placed float
  rectangles for **one** block formatting context, plus the pure geometry queries
  (`leftEdge`/`rightEdge`/`place`/`clearY`). This is the load-bearing math; it is unit-tested in
  isolation with adversarial cases.
- **`pkg/layout/css/block.go`** — `layoutBlockChildren` threads a `*floatContext`: a floated child is
  placed out of flow (does not advance the in-flow cursor) and collected for paint; an in-flow block
  child narrows its content placement to the float band; a `clear`ed child starts below the cleared
  floats. `layoutBlock`/`layoutInterior`/`layoutTree` gain the `*floatContext` parameter (a new BFC
  root creates a fresh context; in-flow blocks pass theirs down).
- **`pkg/layout/css/inline.go`** — `layoutInline` drives line-by-line breaking against the
  float-narrowed width: it re-queries `floatContext` at each line's pen-Y, breaks **one** line of the
  remaining glyphs at that width (via the new `inline.BreakNext`), and offsets the line's `StartX` to
  the narrowed left edge.
- **`pkg/layout/inline/break.go`** — a new `BreakNext(glyphs, widthPt) (line, rest []Glyph)` primitive:
  one greedy first-fit line at `widthPt` plus the unconsumed remainder, sharing the exact break logic
  (`lastSpaceBefore`, single-overlong-word overflow) the existing `Break` uses. `Break` is unchanged in
  signature and behavior (DOCX and the non-float HTML path keep using it); `BreakNext` is the per-line
  driver the float IFC needs. Adding it to the shared core keeps the line-breaking algorithm in **one**
  place rather than duplicating it in `pkg/layout/css`.
- **`pkg/layout/css/fragment.go`** — `Fragment.IsFloat bool` (set when the box floated) and
  `Fragment.Floats []*Fragment` on a BFC-root fragment (the floats placed in that BFC, collected for
  the float paint layer). `AppendItems` is refactored into paint phases (see "Float paint layer").
- **`pkg/css`** — additive `Float string` and `Clear string` on `ComputedStyle`, parsed like the other
  keyword properties (`box-sizing`/`object-fit`); the cascade already routes single-keyword properties.
- **`pkg/layout/css/build.go`** — `floatOf`/`positionOf` stop being stubs: `floatOf` maps
  `cs.Float`; box generation blockifies a floated inline-level element (CSS `display` computation).

### Why the float context is per-BFC mutable state, and why that is concurrency-safe

The `floatContext` is the one piece of mutable state during a single `Engine.Layout` call. The project's
concurrency contract (CLAUDE.md) is that a parsed/laid-out document is read-only **after** `Layout`
returns, so it can be shared across the per-page render fan-out without locks. The fan-out is *across
pages*; each page is a separate `Layout` call running on one goroutine. A `floatContext` is created
inside `Layout`, threaded by pointer through that single goroutine's recursion, and never escapes it —
the fragment tree it helps build is fully materialized and immutable by the time `Layout` returns. So
this introduces no shared mutable state across goroutines. (The engine's only cross-call shared state
remains the face cache and image cache, both already concurrent.)

## The float context (`pkg/layout/css/floats.go`)

A float is positioned by its **margin box** (CSS 9.5: the float's margin edges touch the containing
block's content edge, or the preceding float's margin edge). The context records margin-box rects in
page space:

```go
type floatBox struct {
    side       cssbox.FloatKind // FloatLeft | FloatRight
    x, y, w, h float64          // margin-box rectangle, page space (points, Y-down)
    frag       *Fragment        // the laid-out fragment (border box inside this margin box), for paint
}

type floatContext struct {
    cbLeft, cbRight float64    // containing block CONTENT-box left/right edges (the band floats sit within)
    floats          []floatBox // floats placed in this BFC, in placement order
}
```

Geometry (all pure functions of `cbLeft`/`cbRight` + `floats`, so directly unit-testable):

- **`leftEdge(y, h) float64`** — `cbLeft`, pushed right by every **left** float whose vertical band
  `[f.y, f.y+f.h)` overlaps `[y, y+h)`: the result is `max(cbLeft, max over overlapping left floats of
  f.x+f.w)`. Used by both the IFC (per line box) and the block stacker (per in-flow block).
- **`rightEdge(y, h) float64`** — symmetric: `cbRight`, pulled left by overlapping **right** floats:
  `min(cbRight, min over overlapping right floats of f.x)`.
- **`place(side, w, h, y) floatBox`** — places a `w×h` (margin-box) float on `side` at the lowest
  `y' ≥ y` where it fits between `leftEdge`/`rightEdge` at that band. The fit test: a left float fits at
  `y'` when `leftEdge(y',h) + w ≤ rightEdge(y',h)`. If it does not fit at `y` (another float on that
  side occupies the band, and the remaining width is too small), it is lowered to the bottom of the
  shallowest blocking float and retried — this is the **wrap-to-new-row** behavior. A left float is
  positioned with its left margin edge at `leftEdge(y',h)`; a right float with its right margin edge at
  `rightEdge(y',h)`. The placed rect is appended to `floats` and returned. **Overflow case**: if `w`
  exceeds the full band width `cbRight-cbLeft`, the float is placed at the edge at `y` (allowed to
  overflow the opposite edge) rather than looping — documented degradation.
- **`clearY(clear string, y) float64`** — the lowest `y` at or below `y` that clears the named side(s):
  `max(y, max over matching floats of f.y+f.h)`. `clear:both` matches all floats; `left`/`right` match
  that side only; `none` returns `y`.

The placement loop is bounded (each iteration lowers `y'` past at least one float; at most `len(floats)`
iterations) so it cannot spin. Malformed inputs (NaN/negative sizes) cannot arise — sizes come from the
already-clamped box model — but the fit test and the loop bound are written defensively (no panic).

## How floats thread through block layout (`block.go`)

`layoutBlock`, `layoutInterior`, `layoutBlockChildren`, and `layoutTree` gain a `*floatContext`
parameter. `layoutTree` (the ICB) creates the root context: `&floatContext{cbLeft: 0, cbRight:
viewportW}`. A box that establishes a **new BFC** (an inline-block today; `establishesNewBFC`) creates a
**fresh** context for its interior rather than inheriting the parent's (floats do not leak into or out
of a nested BFC).

In `layoutBlockChildren`, for each child:

1. **Floated child** (`child.Float != FloatNone`): lay it out as a block (`layoutBlock`) to get its
   border-box size, expand to its **margin box** (`w = mL+borderW+mR`, `h = mT+borderH+mB`), and call
   `floatCtx.place(side, marginW, marginH, currentY)` where `currentY` is the in-flow cursor (the float
   cannot rise above the content that precedes it). Shift the laid-out fragment to the placed margin
   box's content-box origin, set `frag.IsFloat = true`, and record `frag` on both the placed `floatBox`
   and (at the BFC root) the `Floats` slice. The float **does not advance** the in-flow `y` cursor and
   is **not** appended to the in-flow `Children` (it paints via the float layer, positioned in page
   space already). Margin collapsing skips floated children (a float does not collapse with siblings).
2. **In-flow block child**: before laying it out, compute the float-narrowed band at the current cursor
   — `left = floatCtx.leftEdge(y, provisionalH)`, `right = floatCtx.rightEdge(y, provisionalH)`. Per
   CSS, the block's **border box** still spans the full containing block (its background/border slide
   under the float); only its **line-box content** avoids the float. The model: lay the in-flow block
   out at the full containing-block width as today (so its border box is unchanged), but pass the
   narrowed band into its interior so its **inline content and in-flow block descendants** avoid the
   float. Concretely the child's own `floatContext` (same BFC) already carries the floats; the child's
   IFC and block stacker re-query it at their own Y, so narrowing falls out of the per-Y queries without
   special-casing the border box. (A block child that itself establishes a new BFC is the documented
   exception that does not narrow — deferred to 5c.)
3. **`clear`ed child** (`child.Style.Clear` set): raise the child's starting margin-top edge to
   `floatCtx.clearY(clear, y)` before placing it, then continue normal stacking from there.

The in-flow cursor and margin-collapse bookkeeping are otherwise unchanged from the current
`layoutBlockChildren`: floats are simply invisible to the vertical cursor, and in-flow boxes consult the
context for horizontal narrowing.

## How floats narrow the inline formatting context (`inline.go`)

Today `layoutInline` shapes all runs, calls `inline.Break(glyphs, contentW, contentW)` once (a single
fixed wrap width), then positions the returned lines. Floats make the available width **vary by
vertical position**, so the wrap width must be recomputed per line.

The change adds one additive primitive to the shared core (`inline.BreakNext`) and drives the narrowing
from the CSS IFC:

- Shape the runs once (`inline.Shape`) — shaping is width-independent.
- Break **incrementally**: instead of one `Break` over all glyphs, maintain a cursor over the shaped
  glyphs and, for each line, query `availLeft = floatCtx.leftEdge(penY, lineH)` and `availRight =
  floatCtx.rightEdge(penY, lineH)`, call `inline.BreakNext(remaining, availRight-availLeft)` to take
  **one** line plus the remainder, position that line with `StartX` offset to `availLeft`, advance
  `penY` by the line height, and repeat with the remainder until the glyphs are exhausted. A line-height
  estimate is needed before the line is measured (to size the band query); use the box's resolved line
  height (the float bands are coarse enough that the line-height feedback loop is not iterated —
  documented).
- When no float overlaps the current band, `leftEdge`/`rightEdge` return the full content box and the
  per-line `BreakNext` reproduces exactly what one `Break(glyphs, contentW, contentW)` produces (same
  greedy first-fit, same widths, same `StartX`) — so non-floated pages are byte-for-byte unchanged.
  (To guarantee this, `BreakNext` reuses `Break`'s break decision; the float IFC path is taken only when
  the box actually has floats in its band, so the common no-float path can even keep calling `Break`
  directly — an implementation choice the plan settles.)

`BreakNext` lives in the shared core (`break.go`) precisely so the greedy first-fit / space-break /
overlong-word-overflow logic is **not** duplicated in `pkg/layout/css`; `Break`'s signature and behavior
are preserved for DOCX and the non-float HTML path.

## The float paint layer (`fragment.go` — stacking order)

CSS 2.1 Appendix E painting order within a stacking context: in-flow block backgrounds/borders →
**floats** → in-flow inline content / atomics → (positioned descendants, slice 5b). `AppendItems` is
strict tree order today (each box fully painted, then the next), which would paint a float entirely
before or after a sibling rather than in the float layer.

`AppendItems` is refactored into **phase passes**, sequenced by the BFC-root fragment:

- `appendBlockDecorations(dst)` — recurse the in-flow subtree emitting only **backgrounds + borders**,
  **skipping** any subtree whose fragment `IsFloat` (floats are not painted in this phase).
- `appendFloats(dst)` — emit every fragment in this BFC's `Floats` slice **fully** (its own
  `AppendItems`, all phases, recursively — a float's interior is self-contained).
- `appendInlineContent(dst)` — recurse the in-flow subtree again emitting **glyphs, images, inline
  atomics**, again skipping `IsFloat` subtrees.

A fragment that establishes a **new BFC** (root, inline-block) owns this three-phase sequencing for its
own `Floats`; it paints as a single atom inside its parent's phases (its internal float layering is
self-contained — a float does not escape its BFC). A non-BFC fragment keeps recursing normally inside
each phase. `Floats` is kept **separate** from `Children` so in-flow tree order is untouched and the
phasing reads cleanly.

This is intentionally the seam **slice 5b (positioning) generalizes**: positioning replaces the two
in-flow phases + float phase with the full CSS Appendix E 7-layer z-index ordering, and `Floats` becomes
one input (the "non-positioned floats" layer) to the positioned-descendant collection. The phase
factoring is written so 5b extends it (more phases, z-index sort) rather than rewriting it. This is noted
inline in the code so the next slice picks it up.

**Load-bearing, flagged for the spec reviewer:** the phase boundaries (a float paints behind a later
sibling's *background*? in front of its *text*?) and the nested-BFC-as-atom rule. The reviewer verifies
the order with an adversarial overlap test (a float positioned over a later sibling that has both a
background and text: the float must sit above the sibling's background and below — i.e. painted before —
the sibling's text, per Appendix E).

## Box generation (`build.go`, `pkg/css`)

- **`pkg/css`** — `ComputedStyle` gains `Float string` (`"none"` default) and `Clear string`
  (`"none"` default), parsed by the existing keyword-property path (mirroring `box-sizing`/`object-fit`:
  recognized values pass through, unknown values are dropped/ignored).
- **`floatOf(cs)`** maps `cs.Float`: `"left"`→`FloatLeft`, `"right"`→`FloatRight`, else `FloatNone`.
  `positionOf` stays a stub (positioning is slice 5b).
- **Blockification**: CSS computes `display` to a block-level value on a floated element. Box generation,
  when `floatOf(cs) != FloatNone` and the element's display is inline-level (`inline`/`inline-block`),
  classifies it as a **block-level** box (`BoxBlock`, `DisplayBlock`) so it lays out as a float. A
  floated `<img>` stays `BoxReplaced` (replaced + block-level, sized by the replaced algorithm) — the
  replaced sizing already handles a block-level replaced box.

## Degradation

Every unsupported case degrades without aborting the page (recovery is at the page boundary —
`Engine.Layout` already recovers from panics): a float wider than its band overflows the opposite edge
(logged); `float:auto`-width approximates the resolved width; a floated inline blockifies; floats do not
cross an explicit BFC boundary (logged where detectable). No new `ErrUnsupported` is introduced — these
are quiet approximations, consistent with how the engine already handles flex/grid/table fallback.

## Tests

Mirroring sub-projects 3 & 4 (unit + WPT reftests + eyeballed goldens):

- **`pkg/css`** — `float`/`clear` parse + cascade: each keyword, initial (`none`), and an invalid value
  dropped.
- **`pkg/layout/css/floats_test.go`** — the float-context geometry in isolation (adversarial, throwaway
  where useful): `leftEdge`/`rightEdge` with non-overlapping vs. overlapping bands, opposite-side floats
  narrowing from both edges, `place` stacking two floats on one side then wrapping a third to a new row,
  an overflow-wide float, and `clearY` for `left`/`right`/`both`/`none`. These pin the load-bearing math.
- **`pkg/layout/inline`** — `BreakNext`: a single greedy line at a width plus the correct remainder;
  driving it repeatedly at a fixed width reproduces `Break`'s lines (equivalence); the overlong-word
  case overflows alone; an empty remainder terminates. This guards the "non-floated pages unchanged"
  claim at the primitive level.
- **`pkg/layout/css`** — fragment-geometry assertions: a left float places at the content-box left and
  the following text's first lines start at the float's right edge then return to full width below it;
  a right float symmetric; two stacked floats; a wrapped third; `clear:left` drops a block below the
  float; float-aware narrowing of a following in-flow block's inline content; a floated `<img>` sized by
  the replaced algorithm; a floated inline blockifies. Plus a **paint-order** assertion (via
  `AppendItems` over a float overlapping a later sibling: the float's items fall between the sibling's
  background/border items and its glyph items).
- **`pkg/layout/paint`** — unchanged path (floats reuse the existing background/border/glyph/image
  items); no new paint primitive. (If the overlap test wants pixels, a tiny golden covers it.)
- **`pkg/doctaculous`** — WPT-style reftests (a left-floated box + wrapped text == the same content at
  hand-computed positions; a multi-float row) and **goldens** (eyeballed in the PR): a figure-with-
  wrapping-text page and a multi-float row. `TestDOCXGolden` and the existing `TestHTMLGolden` /
  `TestWPTReftests` stay green (DOCX unaffected — the shared inline core is untouched; non-floated HTML
  is byte-for-byte identical). `go test -race ./...` clean.

New fixtures land in the same PR (per CLAUDE.md testing rules). Each unsupported case above has a test
asserting the graceful degradation (overflow-wide float, floated inline blockified).

## Deferred (carried to the next handover)

- **Positioning** (relative/absolute/fixed + z-index + the full Appendix E stacking pass) — slice 5b.
- **`overflow` clipping** (and floats intruding across an `overflow≠visible` BFC boundary) — slice 5c.
- **True shrink-to-fit** (min-content/max-content) width for `float:auto`-width.
- **Clearance** per CSS 8.3.1 (the margin-collapse interaction of `clear`); negative-margin float
  overlap; floats narrowing a sibling that establishes its own BFC.
- **Float-height enclosure** — a block does not yet grow to enclose its floats (CSS: a BFC, or
  `clear`ed content, encloses floats). Consequence found during implementation: a block whose **only**
  children are floats has zero in-flow content height (so a float-only `<body>` renders a degenerate
  1×1 page — which is why the planned `float-row` golden/reftest was dropped in favor of geometry-test
  coverage). Enclosure lands with the `overflow` slice (`overflow≠visible` establishes a BFC that
  encloses floats), so it is grouped there.
- The replaced-content and inline/flow deferrals carried from sub-project 4 (`object-position`,
  ratio-preserving min/max, percentage-height basis, `background-image`, full `vertical-align`,
  `margin:auto` centering) remain open.

## Review fixes folded in

The two-stage review (spec + code-quality, per task) found and fixed, beyond comment/test-accuracy
nits:

1. **A float-placement test-assertion error in the plan** — `TestPlaceStacksThenWraps` expected the
   third (wrapping) float at `y=40`, but with two different-height floats it drops twice (past both
   bottoms) to `y=60`. The algorithm was correct; the expected value was wrong. Fixed in the test and
   the plan.
2. **The IFC float-narrowing missed a containing-block clamp** — the float context spans the whole BFC,
   so a non-BFC box narrower than its BFC (e.g. a `width:200px` `<p>` in a 1280px page) would lay text
   to the BFC width. Fixed by clamping the per-line float edges to the box's own content box (a no-op
   for full-width boxes — goldens unchanged — and harmless to float-narrowing, since an intruding
   float's edge is already inside the box).
3. **Floats painted nothing end-to-end** (caught by generating the `float-figure` golden) — a real
   float is `IsBFC && IsFloat`, so its `AppendItems` took the BFC branch and called the phase walkers
   on itself, which early-returned on `IsFloat`. Fixed by moving the `IsFloat`-skip from "skip `f`
   itself" to "skip floated **children**", so a float paints its own subtree as its BFC's paint root.
   Pinned by `TestAppendItemsFloatPaintsOwnSubtree` (the prior phase test used a non-BFC float and
   missed the combination). Lesson for the positioning slice: test the `IsBFC && IsFloat` (and future
   `IsBFC && positioned`) **combination**, not each flag alone.
4. **Test-fixture zero-value-`Length` trap** — a `cssbox` style literal that omits `Width`/`MaxWidth`
   reads as an explicit `0` (not the cascade's `auto`/`none`), squashing a box to 0×0. The `blockBox`
   test helper now defaults `Width`/`Height`/`MaxWidth`/`MaxHeight` to `UnitAuto`, matching `initialStyle()`.
