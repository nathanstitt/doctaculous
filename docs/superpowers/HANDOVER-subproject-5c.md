# Handover — Sub-project 5c: Overflow clipping (`overflow: hidden/scroll/auto`) + deferred float interactions

**Status:** Not started. Sub-project **5b (positioning)** is DONE on branch `feat/html-positioning`,
PR **#7** open against `feat/html-floats`.
**Next action:** Same flow as #1–#5b — brainstorm → spec (`docs/superpowers/specs/`) → plan
(`docs/superpowers/plans/`) → subagent-driven execution (per task: implement → spec-review →
code-quality-review → fix) → holistic final review → finish branch / PR.

This is the **third and last of three slices** sub-project 5 was split into:
- **5a — floats + clear** ✅ done (PR #6). Spec: `docs/superpowers/specs/2026-06-24-html-floats-design.md`.
- **5b — positioning** ✅ done (PR #7). Spec: `docs/superpowers/specs/2026-06-24-html-positioning-design.md`.
- **5c — overflow clipping** (this doc): `overflow:hidden/scroll/auto`, the **enclosing BFC** it
  establishes, and the float interactions deferred from 5a/5b.

---

## Where we are (the PR stack)

All chained to `main`; retarget each PR up the chain as the one below it merges.

- **#2 CSS parse+cascade** — `feat/css-parse-cascade` → `main`.
- **#3 HTML box generation** — `feat/html-box-generation` → `feat/css-parse-cascade`.
- **#4 block + inline normal flow** — `feat/html-block-inline-flow` → `feat/html-box-generation`.
- **#5 replaced content + images** — `feat/html-replaced-images` → `feat/html-block-inline-flow`.
- **#5a floats + clear** — `feat/html-floats` → `feat/html-replaced-images` (**PR #6**).
- **#5b positioning** — `feat/html-positioning` → `feat/html-floats` (**PR #7**).

If the stack has merged to `main`, branch sub-project 5c off `main`. Otherwise branch **5c off
`feat/html-positioning`** (it builds directly on the stacking-pass + float layer). Tell every subagent
explicitly: you are on `feat/html-overflow`, do NOT checkout/stash/switch branches.

## What sub-project 5b delivered (the foundation 5c builds on)

Design: `docs/superpowers/specs/2026-06-24-html-positioning-design.md` (read its "The stacking pass",
"Two-pass abs-pos", "Relative offset & descendant CB", and "Deferred" sections — and the spec's "Review
fixes folded in"-equivalent is captured in the PR-review history; the load-bearing lessons are below).
All positioning logic lives in `pkg/layout/css`; the shared inline core stayed untouched (positioning
needed no inline primitive).

- **`pkg/layout/css/positioning.go`** — pure geometry: `relativeOffset(b, cbW, cbH)` (CSS 9.4.3 offset,
  left/top win), `absRect(b, ed, cb) (border rect, contentW)` (abs/fixed used border-box rect + content
  width, CSS 10.3.7 subset, via per-axis `axisAbs`), `absWidthIsOffsetConstrained` (the left+right+auto
  width case), `rect{x,y,w,h}`. Unit-tested in `positioning_test.go`.
- **`pkg/layout/css/fragment.go`** — **the seam 5c touches.** `Fragment` gained `IsPositioned bool`,
  `RelOffsetX/RelOffsetY float64`, `IsStackingContext bool`, `Positioned []*Fragment`. `AppendItems` is
  now a **4-phase stacking pass** for an `IsStackingContext || IsBFC` fragment: `appendDecorations` →
  the **float layer** (`Floats`) → `appendContent` → the **positioned layer** (`Positioned`, document
  order). The in-flow walkers skip `IsFloat || IsPositioned` children. A relative offset is applied via
  `translateItems(dst, start, dx, dy)` over the fragment's flattened item range (NOT
  `translateFragment`/`shiftFragment`, which do not recurse `Positioned`). **This is where overflow
  clipping inserts a clip-push/clip-pop around a fragment's emitted item range** (see below).
- **`pkg/layout/css/block.go`** — threads `posCtx *positionedContext` (collects abs/fixed for pass 2)
  and `posCB posCBOwner` (the abs-pos containing-block owner: ancestor `*Fragment` + `*cssbox.Box`, or a
  page sentinel) through `layoutTree`/`layoutBlock`/`layoutInterior`/`layoutBlockChildren` — exactly as
  5a threaded `fc`/`bandOriginY`. Relative children **bubble** to their nearest stacking-context
  ancestor via `interior.pendingPositioned`/`blockResult.pendingPositioned` (mirroring `bfcFloats`).
  `resolveAbsolute` is the **pass-2** abs-pos resolver (index-walk for transitivity, back-fills nested
  abs CB owners). `establishesNewBFC(b)` now returns true for **abs/fixed** as well as inline-block/float.
  `establishesStackingContext(b)` = any positioned box.
- **`pkg/css/cascade.go`** — additive `Position`, `Top`/`Right`/`Bottom`/`Left Length`, `ZIndex int` +
  `ZIndexAuto bool` (all not-inherited). `applyZIndex`/`parseInt` parse z-index (rejects non-integers).
- **`pkg/layout/css/build.go`** — `positionOf(cs)` maps the keyword; `generate` forces `Float=none`
  under abs/fixed (CSS 9.7); `applyBlockify` (renamed from `applyFloatBlockify`) blockifies a floated OR
  abs/fixed inline-level box.
- **`pkg/doctaculous`** — `html-position-relative` + `html-position-absolute` goldens (block-level
  relative offset; abs pin in a relative container) and `abs-pos` + `relative-offset` WPT reftests.

## What sub-project 5c must build (roadmap §6, overflow slice)

`overflow: visible | hidden | scroll | auto` and the **two deferred float interactions** that hinge on
the BFC an `overflow≠visible` box establishes. Today `overflow` is **not parsed at all** (it is not on
`ComputedStyle`), and every box renders without clipping (graceful no-op — the same starting point
floats/positioning had). In rough order:

- **Cascade + box-gen** (`pkg/css`, `pkg/layout/css/build.go`): add `Overflow` (and consider
  `overflow-x`/`overflow-y`; CSS computes a shorthand) to `ComputedStyle`, parsed like the other keyword
  properties (`overflow:visible|hidden|scroll|auto`, others dropped). Carry it onto the box (a new
  `cssbox.Box` field, or read from `Box.Style` at layout time as positioning does). **`overflow≠visible`
  establishes a new BFC** — extend `establishesNewBFC(b)` to return true for it (this is the trigger
  that makes the deferred float-enclosure work fall out). Note: `scroll`/`auto` in this no-scrollbars,
  single-tall-page model **clip exactly like `hidden`** (there is no scroll position and no scrollbar
  chrome) — `scroll`/`auto`/`hidden` all map to "clip to the padding box". Document that approximation.
- **Clipping at paint** (`pkg/layout/css/fragment.go`, `pkg/layout/paint`, `render.Device`): a box with
  `overflow≠visible` **clips its descendants' paint to its padding box** (CSS: the clip rect is the
  padding box, i.e. the border box deflated by border widths). The clean seam is the stacking pass:
  when `AppendItems` flattens a clipping fragment, **push a clip rect, emit the subtree's items, pop the
  clip** — but `layout.Item` is a flat list with no nesting, so 5c needs a way to express "these items
  are clipped to rect R". Two viable designs (the spec/plan picks one):
  1. **Clip items in the flat stream** — add `ClipPushKind`/`ClipPopKind` (or a `Clip *rect` field on
     `Item`) so the painter maintains a clip stack; `AppendItems` emits a push before the clipped
     fragment's items and a pop after. The PDF/raster painters already clip (`W`/`W*` in the PDF
     interpreter, the raster backend has a clip path) — wire `render.Device`'s clip to honor it for the
     reflow path. This is the most faithful and reuses existing raster clipping.
  2. **Pre-clip geometry** — intersect each emitted item's rect with the active clip while flattening
     (drop fully-outside items, shrink partially-outside backgrounds/borders). Simpler, no painter
     change, but cannot clip a glyph mid-cell or a rotated image, and is lossy for partial overlaps.
     Acceptable only if the painter can't take a clip; prefer (1).
  Whichever: the clip must **nest** (an inner clip intersects the outer), follow the **stacking order**
  (a clip applies within its establishing box's stacking context; a positioned descendant that escapes
  the box's containing block is NOT clipped by it — CSS abs-pos clipping is by the nearest
  `overflow≠visible` *ancestor that is also its containing block*), and clip the box's **own** float
  layer and positioned layer too. **Have the spec reviewer verify the clip-vs-stacking interaction**
  with an adversarial test (an abs-pos child of a clipped box whose containing block is OUTSIDE the
  clip must NOT be clipped; an in-flow overflowing child MUST be clipped).
- **Deferred float interactions (fold in now — they hinge on the overflow BFC):**
  1. **Float-height enclosure** — a box with `overflow≠visible` (a BFC) **grows to enclose its floats**
     (CSS 9.4.1 / 10.6.7: a BFC's height includes its floats' bottom). Today a float-only block has
     **zero** content height (5a's documented gap — `block.go` does not extend `contentHeight` for
     floats). 5c: when a box establishes a BFC, its content height is `max(in-flow content bottom, the
     bottom of every float in its `floatContext`)`. The `floatContext` already records float rects;
     surface the max-float-bottom. This is the fix that makes a `<div style="overflow:hidden">` wrap
     its floated children — the canonical "clearfix" replacement. **Add the `float-row`/`float-only`
     golden that 5a had to DROP** (a float-only body rendered a degenerate 1×1 page; with enclosure on
     an `overflow:hidden` wrapper it now has real height) — see 5a's spec "Deferred" + the NOTE in
     `pkg/doctaculous/html_golden_test.go` / `wpt_reftest_test.go`.
  2. **Floats intruding across an `overflow≠visible` BFC boundary** — a sibling BFC (now including an
     `overflow:hidden` box) does **not** overlap an outer float; its line box / border box shortens
     away from the float (CSS 9.5). Today the engine does not shorten a sibling BFC away from an outer
     float (5a documented this). 5c: when laying out an `overflow≠visible` box next to an existing
     float, narrow/offset it past the float band (the float context is queried in the BFC-root frame —
     reuse `leftEdge`/`rightEdge`/`nextDropY`).

Each lands with fixtures + golden/WPT tests, degrades gracefully (already: `overflow` is ignored →
visible/no-clip), and recovers at the page boundary.

## Carried-forward deferrals 5c should fold in or scope out

From 5a's and 5b's specs' "Deferred" sections:
- **Float-height enclosure** and **floats intruding across an `overflow≠visible` BFC boundary** — these
  are **5c's** to fold in (above); they were explicitly grouped here.
- **A float inside a `position:relative` (non-BFC) box does not ride the relative paint offset** (5b
  deferral) — a relative box doesn't establish a BFC, so its float escapes to the enclosing float layer.
  Likely interacts with the clip/BFC work; revisit if a test surfaces it, else leave noted.
- Still open and NOT 5c's concern unless touched: **full z-index stacking** (negative/numeric — the
  positioned layer paints in document order; the seam is the `Positioned` loop in `AppendItems`), the
  **precise static-position solve** for an all-`auto` abs box, abs `width:auto` **shrink-to-fit**, abs
  `margin:auto` centering, a `bottom`-only auto-height abs box, **`position:relative` on any
  inline-level box** (a no-op today — needs inline-box fragments), and the replaced/inline deferrals
  (`object-position`, ratio-preserving min/max, percentage-height basis, `background-image`, full
  `vertical-align`, `margin:auto` centering, the margin-collapse edge cases).

## Process reminders (held across #1–#5b — these earned their keep)

- **Sandbox blocks the Go build cache + TLS** — run `go`/`golangci-lint`/`gofmt` (and `gh pr create`,
  `git push` over HTTPS) with the sandbox disabled. The branch push works under the sandbox only if the
  remote is SSH; this repo's `origin` is HTTPS, so push with sandbox disabled.
- **Editor diagnostics LAG badly** — after a subagent adds a field/file you'll see stale "undefined" /
  "unknown field" / "unused" / "redeclared" errors and phantom `zz_*` scratch files in the diagnostics.
  **Trust `go build`/`go test`, not the panel.** After any review subagent, `find . -name 'zz_*' -delete`
  and confirm `git status` is clean (5a/5b reviewers left scratch files repeatedly; tell every subagent
  explicitly: delete any `zz_*` throwaway before finishing).
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages, not the repo root. The repo uses **NO `//nolint`** and **declines all "modernize" hints**
  (`max()`/`min()`/`slices.*`/range-over-int) — keep explicit `if x < y { x = y }` clamps and indexed
  loops. golangci-lint **does** flag `if !(a && b)` (QF1001 — write `if a>=b || b>=c`) and an **unused
  unexported field/func** (the `unused`/`unusedfunc` checks) — every new field/func must be read/called
  in the same PR.
- **The zero-value `Length` trap** (bit 5a and 5b): a `cssbox.ComputedStyle`/`Box` literal that omits
  `Width`/`MaxWidth` reads as explicit `0` (`{0, UnitPx}`), NOT the cascade's `auto`/`none`; an omitted
  offset reads as a real `0` offset, not `auto`. Test fixtures built as raw structs (not via `blockBox`)
  must set `Width`/`Height`/`MaxWidth`/`MaxHeight`/offsets to `UnitAuto` where they mean auto. 5b added
  `posStyle()`/`posBox()` helpers (in `positioning_layout_test.go`) that default all of these to auto —
  reuse or extend them for overflow fixtures.
- **Test the FLAG COMBINATIONS, not each flag alone** (5a's & 5b's worst-miss class): 5b shipped a
  double-translate bug for `float + position:relative` that the unit tests missed (the float had no
  children); a holistic review caught an abs `left+right+width:auto` sizing bug no layout test covered.
  5c adds an `overflow≠visible` flag that combines with float/abs/relative/BFC — **explicitly test the
  combinations** (a clipped box containing a float; a clipped box containing an abs-pos child whose CB
  is outside; an `overflow:hidden` BFC next to an outer float; a positioned + overflow box), and
  **eyeball every golden** — the golden render caught both 5b paint bugs that unit tests passed through.
- **Eyeball every new/changed golden PNG** in the PR (the controller, via the Read tool — not the
  implementer, who has no image vision). **5b lesson: a "passing" golden can still be WRONG** — 5b's
  first `position-relative` golden used `inline-block` (a documented no-op for relative offset) and
  rendered a flat row that *looked* fine to the test but didn't exercise the feature; eyeballing caught
  it and the fixture was switched to block-level boxes. Author fixtures that actually exercise the
  SUPPORTED path, and verify the eyeball shows the feature.
- **Confirm no pre-existing golden changed** (`git status --short pkg/doctaculous/testdata/
  pkg/render/raster/testdata/` shows only new files). A clip/paint-pass change like 5c's is high-risk
  for silently reordering or clipping existing pages — the no-overflow pages MUST stay byte-identical
  (run the goldens without `-update` and confirm no diff). The float-enclosure change extends content
  height for BFC boxes — verify it does NOT change an existing non-float BFC's height (an inline-block
  with no floats must be unchanged).
- **The two-stage review (spec + code-quality, per task) earns its keep**, and a **holistic final
  review** on the assembled diff is mandatory — in 5b it caught a real sizing bug, two missing
  degradation logs, and a false spec claim that all three earlier reviews passed. Have spec reviewers
  **verify load-bearing geometry/paint-order with throwaway adversarial tests** (for 5c: the
  clip-vs-stacking-context interaction and the float-enclosure height math), and **delete the
  throwaways** (confirm `git status` clean).
- **Subagents implementing a large task** read the exact task lines from the plan file (point them at
  the line range) rather than having the controller re-transcribe; for small tasks paste the full text.
  Threading a new parameter through `layoutBlock` forces a caller update in `inline.go` (the inline-block
  atom path) and its test — expect that mechanical change and tell the implementer it's allowed.
- **Propagate review fixes back into the spec/plan**, and **update CLAUDE.md's Done/TODO** when the PR
  lands (move overflow out of the §6 TODO; the TODO already lists overflow clipping with the two float
  interactions as the next slice).
