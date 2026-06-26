# HTML rendering — CSS flexbox (`display: flex`), single-line (sub-project 9)

**Date:** 2026-06-26
**Status:** Design approved; ready for implementation plan.
**Branch:** `feat/html-flexbox` (off `feat/html-webfonts`, PR #12 tip; rebase onto `main` if the stack has merged).
**Spec reference:** CSS Flexible Box Layout Module Level 1 — <https://www.w3.org/TR/css-flexbox-1/>.

## Summary

Turn the **block fallback** a `FlexFC` box currently uses (`pkg/layout/css/block.go`, the `default` case of
the formatting-context switch: it logs `… not yet implemented; falling back to block normal flow` and stacks
children as blocks) into a **real, single-line flex layout** — a new formatting context alongside the block,
inline, and table ones. Flexbox lives entirely in the CSS layout engine (`pkg/layout/css`) and the cascade
(`pkg/css`); the `render.Device` seam, the PDF pipeline, the DOCX pipeline, and the shared inline core
(`pkg/layout/inline`) are **untouched**. A flex container produces ordinary positioned `Fragment`s that paint
through the existing flatten/paint path. **No new dependencies.**

This is sub-project 9 of the HTML-rendering roadmap. It is modeled structurally on **sub-project 7 (tables)**:
a self-contained layout mode above the block formatting context that reuses the existing block+inline layout
for the *contents* of each flex item (a flex item establishes a BFC, exactly like a table cell).

## Scope

**In scope — single-line flexbox, full (`flex-wrap: nowrap`):**

- **Flex container + flex items.** `display: flex` and `display: inline-flex` establish a flex formatting
  context (`FlexFC`). In-flow children become flex items; inline-level children are blockified (CSS Flexbox
  §4). Contiguous runs of inline content / text become **anonymous flex items** (the flex analogue of the
  anonymous-table-box and inline-in-block fixups). `inline-flex` differs only in the container's outer role
  (an inline-level atom that flows like inline-block); item layout is identical.
- **Main/cross axis from `flex-direction`** — `row` / `row-reverse` / `column` / `column-reverse`.
- **`flex-basis` + `flex-grow` + `flex-shrink`** (and the `flex` shorthand) — resolve each item's flex base
  size and hypothetical main size, then distribute free space via the §9.7 multi-pass resolution (grow on
  surplus, shrink on deficit). This is the heart of the algorithm and the biggest correctness risk.
- **Main-axis alignment** — `justify-content`: `flex-start` / `flex-end` / `center` / `space-between` /
  `space-around` / `space-evenly`.
- **Cross-axis alignment** — `align-items` + per-item `align-self`: `stretch` (default) / `flex-start` /
  `flex-end` / `center`. (`baseline` is deferred — see Degradation.)
- **Item min/max sizing + the automatic minimum size** — explicit `min-*`/`max-*` always honored; plus the
  automatic minimum (`min-width:auto`/`min-height:auto` → min-content, CSS Flexbox §4.5), so flex items do
  not shrink below their min-content size unless `min-*: 0` is explicit.
- **`gap` / `column-gap` / `row-gap`** — the **main-axis gap** (`column-gap` for `row*`, `row-gap` for
  `column*`) is inserted between adjacent items and folded into the free-space math. The **cross-axis gap**
  is parsed and stored but a no-op until multi-line (wrap) lands.
- **`order`** — items are sorted by `order` (integer, may be negative) before layout, stable for ties
  (document order).
- **Each flex item's contents lay out through the existing block/inline path** (`e.layoutBlock`), exactly as
  a table cell does — a flex item establishes a BFC for its contents.

**Deferred to sub-project 9b (each with a graceful, tested fallback — see Degradation):**

- **Multi-line flex** (`flex-wrap: wrap` / `wrap-reverse`) + **`align-content`**. `flex-wrap` is parsed but
  acted on as `nowrap` (items overflow rather than wrap), logged.
- **RTL / `direction`.** The engine has no `direction`/bidi support anywhere (it was the sole table deferral
  too). `row-reverse`/`column-reverse` are supported (explicit, no bidi needed); RTL-driven main-start/end in
  `row` is not — `direction` is parsed but acted on as LTR, logged.
- **Full `align-items: baseline` / `align-self: baseline`** cross-baseline participation across items with
  different fonts — approximated to `flex-start`, logged.
- **Cross-axis gap effect** — `row-gap` for `row*` (and `column-gap` for `column*`) is stored but a no-op
  until wrapping lands (correct per spec for a single line).

The in-vs-deferred line mirrors how tables landed the core algorithm with RTL deferred.

## Architecture & layer fit

Flexbox touches five places, in dependency order:

1. **`pkg/css`** (cascade) — add the flex properties to `ComputedStyle`, parse them in `applyOne`
   (`cascade.go`), and add `flex`/`gap` shorthand expansion in `shorthand.go` (mirroring `margin`/`border`).
   Unknown/unsupported values degrade (skipped). No layout here.
2. **`pkg/layout/cssbox`** — `DisplayFlex` and `FlexFC` already exist (`box.go`). Add a `BoxAnonFlexItem`
   anonymous-box kind (mirroring `BoxAnonTablePart`) for wrapped inline runs, and register it in the
   `blockLevel` helper. Per the `unused` linter, a field/kind lands only when a same-PR consumer reads it.
3. **`pkg/layout/css/flexfix.go`** (new) — the anonymous-flex-item fixup: wrap contiguous in-flow inline
   runs/text inside a flex container as anonymous block-level flex items; blockify inline-level child boxes
   (CSS Flexbox §4). Called from `Build` right after `fixupTables`.
4. **`pkg/layout/css/flex.go`** (new) — `layoutFlex(...)`, registered as `case cssbox.FlexFC` in `block.go`'s
   FC switch (replacing the `default` block-fallback for flex; **grid keeps the fallback**). Contains the
   axis-abstracted algorithm + the pure `resolveFlexibleLengths`. Reuses `measure.go` (min/max-content) and
   `e.layoutBlock` (item contents → BFC).
5. **`pkg/layout/css/fragment.go`** — **verify** (do not rewrite) that flex items flatten/paint through the
   existing path. They are block boxes at computed offsets, like table cells, so this should need no change;
   the spec only commits to confirming it.

The `block.go` FC switch becomes:

```go
case cssbox.TableFC: in = e.layoutTable(...)
case cssbox.FlexFC:  in = e.layoutFlex(...)   // NEW
default:             // GridFC only now → block fallback + log (unchanged)
```

**Structure decision (approved):** an **axis-abstracted `layoutFlex`** (the algorithm written once in
abstract main/cross coordinates; `column` is a different axis mapping, `*-reverse` flips placement) **with the
§9.7 flexible-length resolution carved out as a pure, dependency-free function** `resolveFlexibleLengths`
(operates only on base/hypothetical/min/max/grow/shrink numbers — unit-testable with hand-computed vectors).
This directly answers the two highest correctness risks: axis duplication and the §9.7 freeze loop.

## The flex properties (cascade — `pkg/css`)

New `ComputedStyle` fields, parsed in `applyOne`, with `flex` and `gap` expanded in `shorthand.go`.

**Container properties** (read off the flex container box):

| Property          | `ComputedStyle` field      | Values parsed                                                                 | Default      |
|-------------------|----------------------------|-------------------------------------------------------------------------------|--------------|
| `flex-direction`  | `FlexDirection string`     | `row` / `row-reverse` / `column` / `column-reverse`                           | `row`        |
| `flex-wrap`       | `FlexWrap string`          | `nowrap` / `wrap` / `wrap-reverse` (stored; only `nowrap` acted on)           | `nowrap`     |
| `justify-content` | `JustifyContent string`    | `flex-start`/`flex-end`/`center`/`space-between`/`space-around`/`space-evenly` | `flex-start` |
| `align-items`     | `AlignItems string`        | `stretch`/`flex-start`/`flex-end`/`center`/`baseline`                          | `stretch`    |
| `column-gap`      | `ColumnGap Length`         | length/percentage (`normal` → 0)                                              | 0            |
| `row-gap`         | `RowGap Length`            | length/percentage (stored; cross-axis, no-op single-line)                     | 0            |

**Item properties** (read off each flex item box):

| Property      | `ComputedStyle` field | Values                                                         | Default |
|---------------|-----------------------|----------------------------------------------------------------|---------|
| `flex-grow`   | `FlexGrow float64`    | ≥ 0 number                                                     | 0       |
| `flex-shrink` | `FlexShrink float64`  | ≥ 0 number                                                     | 1       |
| `flex-basis`  | `FlexBasis Length`    | length / percentage / `auto` / `content`                       | `auto`  |
| `align-self`  | `AlignSelf string`    | `auto`/`stretch`/`flex-start`/`flex-end`/`center`/`baseline`    | `auto`  |
| `order`       | `Order int`           | integer (may be negative)                                      | 0       |

**Representations:**

- **`flex-basis: content`** is modeled with a **new `LengthUnit` constant `UnitContent`** in `pkg/css/value.go`
  (additive, isolated to flex-basis consumers — existing width/margin handling never produces or reads it, so
  it cannot perturb them). `auto`/percentage/length reuse the existing `Length` units.
- All flex **item** properties are parsed unconditionally (cheap; matches CSS — they compute on every element)
  and only *read* by `layoutFlex`.

**Shorthand expansion (`shorthand.go`):**

- `gap: <a> [<b>]` → `row-gap: <a>; column-gap: <b|a>`.
- `flex` shorthand → `flex-grow` / `flex-shrink` / `flex-basis`:
  - `flex: none` → `0 0 auto`
  - `flex: auto` → `1 1 auto`
  - `flex: initial` → `0 1 auto`
  - `flex: <number>` (e.g. `flex: 1`) → `<number> 1 0%`
  - `flex: <length-or-percentage>` (e.g. `flex: 100px`) → `1 1 <length>`
  - `flex: <grow> <shrink>` → `<grow> <shrink> 0%`
  - `flex: <grow> <basis>` → `<grow> 1 <basis>`
  - `flex: <grow> <shrink> <basis>` → as written

Unknown values for any property are skipped (the cascade's existing degrade behavior), leaving the default.

## The flex algorithm (`pkg/layout/css/flex.go`)

`layoutFlex(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext)
interior` — single-line. Signature matches `layoutTable`. Written in **abstract main/cross coordinates** via a
`flexAxis` helper derived from `flex-direction`, so the sizing/distribution/alignment math is written once.

**The axis helper.** Given `flex-direction`, `flexAxis` maps `main ↔ (width|height)`, `cross ↔ (height|width)`,
and provides start-edge placement that accounts for `row-reverse`/`column-reverse`. For `row*`,
main = horizontal; for `column*`, main = vertical. All sizing below is in main/cross; only the final fragment
rects convert back to x/y/w/h.

**Steps (CSS Flexbox §9, single line):**

1. **Collect items.** Children that are flex items (in-flow; the fixup already blockified inline-level ones
   and wrapped bare inline runs). Sort by `order` (stable for ties → document order). Empty → zero-size
   `interior` (`contentHeight: 0`), no panic.

2. **Container inner main size.** For `row`, the inner main size is `contentW` (already the content-box
   width passed in). For `column`, it is the container's content **height** — definite if `height` is set,
   otherwise indefinite. **Definite main size → use it** (free space can be positive/negative). **Indefinite
   main size** (the common `column` auto-height case) → main size = Σ items' outer hypothetical main sizes +
   gaps (so grow has no effect and shrink does not trigger) — the standard single-line content-sizing rule.

3. **Flex base size + hypothetical main size, per item** (§9.3):
   - **Flex base size** from `flex-basis`: `<length>` → that length (% resolved against container inner main
     size); `auto` → the item's main-size property (`width` for `row`, `height` for `column`) if set, else
     falls through to `content`; `content` → `measureMaxContent` along the main axis.
   - **Hypothetical main size** = flex base size clamped to the item's used min/max main size. The **used
     min** incorporates the **automatic minimum** (§4.5): when `min-(width|height)` is `auto`, the floor is
     the content min size (`measureMinContent`), further capped by an explicit main-size or `max-*` per the
     spec's `min(...)`. Explicit `min-*`/`max-*` are always honored.

4. **Resolve flexible lengths** — the pure
   `resolveFlexibleLengths(items []flexItem, innerMain, totalMainGap float64) []float64` returns each item's
   **used main size**. It implements the §9.7 multi-pass freeze loop:
   1. **Determine the used flex factor.** Sum the items' outer hypothetical main sizes. If that sum is **less
      than** the line's inner main size → free space positive → use **flex-grow** factors; otherwise use
      **flex-shrink** factors.
   2. **Size inflexible items / freeze.** Freeze any item whose used flex factor is 0; when growing, also
      freeze any whose flex base size > its hypothetical main size; when shrinking, also freeze any whose
      flex base size < its hypothetical main size. Set frozen target = hypothetical main size.
   3. **Initial free space** = inner main size − Σ(outer size: frozen at frozen size, unfrozen at base size)
      − total main gap.
   4. **Loop:**
      - **(a) Check for flexible items** — if no unfrozen items remain, exit.
      - **(b) Remaining free space** = inner main size − Σ(outer size: frozen at frozen, unfrozen at base) −
        total main gap. **Special rule:** if the magnitude of the sum of unfrozen `flex-grow` factors is < 1,
        multiply the *initial* free space by that sum; if the result is smaller in magnitude than the
        remaining free space, use it instead. The same rule applies to `flex-shrink` factors < 1.
      - **(c) Distribute free space.** **Grow** (positive): proportional to each unfrozen item's
        `flex-grow`. **Shrink** (negative): proportional to each item's **scaled flex shrink factor** =
        `flex-shrink × flex base size`. Set each unfrozen target = base size ± its share.
      - **(d) Fix min/max violations.** Clamp each unfrozen item's target to its used min/max main size
        (used min includes the automatic minimum). Record each item's violation sign (+ if clamped up by
        min, − if clamped down by max, 0 otherwise).
      - **(e) Freeze by total-violation sign.** Let total = Σ violations. total = 0 → freeze **all** unfrozen
        items at their (now clamped) targets; total > 0 → freeze the **min-violated** items; total < 0 →
        freeze the **max-violated** items.
      - Loop back to (a).
   5. Each item's **used main size** is its (final) target main size.

   **This pure function is unit-tested with hand-computed vectors.** Its loop terminates: each pass either
   exits or freezes ≥ 1 item; with N items it runs ≤ N+1 passes.

   > **Implementation gate (carried as an explicit plan-task instruction):** before encoding steps (b)–(e),
   > **fetch and verify the exact §9.7 sub-step wording against the W3C spec**
   > (<https://www.w3.org/TR/css-flexbox-1/>, §9.7 "Resolving Flexible Lengths"; if the single-page fetch
   > truncates before §9.7, fetch a section-anchored or split view). The freeze/violation structure is easy
   > to get subtly wrong (cf. sub-project 8's WOFF2 header). **A change that forces inverting a passing test
   > is a red flag — stop and re-verify against the spec.**

5. **Lay out each item's contents** at its used main size through
   `e.layoutBlock(ctx, item, <main-as-width>, …, own floatContext, own positionedContext, isPage CB)` — the
   item establishes a **BFC** (the table-cell pattern). For `row`, used-main is the item's content width and
   the returned fragment height is the item's cross (content) size. For `column`, used-main is the item's
   height and width is driven from the cross axis (the item is laid out at the cross-axis width with its main
   extent pinned to used-main) — handled via the axis mapping. Abs/fixed descendants inside an item resolve
   against the item's box now (mirroring the cell path) so they are not dropped.

6. **Line cross size** = max item outer cross size, clamped to the container's min/max cross size (and to a
   definite container cross size if set).

7. **Cross-axis alignment** (`align-items`, overridden by item `align-self` ≠ `auto`):
   - `stretch` (default) → set the item's cross size to the line cross size **and relayout** its contents at
     that cross measure (so content reflows to the stretched box), **unless** the item has a definite cross
     size. The relayout is one extra `layoutBlock` at the stretched cross width (row) — acceptable cost,
     matches how tables relayout cells for row height.
   - `flex-start` / `flex-end` / `center` → position the item within the line's cross extent.
   - `baseline` → **approximated to `flex-start`** (deferred, logged).

8. **Main-axis alignment** (`justify-content`): distribute the line's leftover main free space (inner main
   size − Σ used main sizes − total main gap) per value — `flex-start` / `flex-end` / `center` /
   `space-between` / `space-around` / `space-evenly`. The **main-axis gap** sits between adjacent items and
   is consumed before free-space distribution.

9. **Emit fragments.** Each item becomes a positioned child `Fragment` at its (main, cross) → (x, y) offset
   and (used-main, cross) → (w, h) size. `*-reverse` flips placement along that axis (explicit; no bidi).
   The container `interior.contentHeight` is the cross extent (for `row`) or the main extent (for `column`).
   Everything flows through the existing flatten/paint path (`fragment.go`).

**Edge calls pinned:**

- **`*-reverse`** flips start↔end along that axis; it needs no bidi (explicit). `column` swaps axes. RTL
  `row` main-start/end is deferred → LTR, logged.
- **`stretch` relayout** happens only when the item has no definite cross size and the resolved alignment is
  `stretch`.

## Degradation & error handling

Per the project's non-negotiable rule, unsupported cases **degrade gracefully (skip + debug log), never
panic**; recovery is at the existing page boundary (`layoutFlex` is below it — no new recover needed). Each
deferral has a tested fallback.

| Case | Behavior | Logged |
|------|----------|--------|
| `flex-wrap: wrap` / `wrap-reverse` | Treated as `nowrap` — items stay on one line and overflow rather than wrap. | yes |
| `align-content` | Parsed-and-ignored (only meaningful multi-line). | n/a single-line |
| RTL / `direction: rtl` on a `row` container | Acts LTR (main-start = left). `direction` parsed but not honored (no bidi anywhere). | yes |
| `align-items: baseline` / `align-self: baseline` | Approximated to `flex-start`. | yes |
| cross-axis gap on a single-line container (`row-gap` for `row*`) | Stored, no-op (correct per spec for one line). | no |
| Empty / no-in-flow-children flex container | Zero-size `interior`, no items, no panic. | no |
| Degenerate inputs (all-frozen with negative free space; `flex-basis` %/`min`/`max` against an indefinite main size; `flex-shrink: 0` item wider than container) | Item overflows (no clamp below its min); no spin, no panic — the §9.7 loop is bounded. | — |
| Unknown property value (e.g. `justify-content: foo`) | Skipped by the cascade → property keeps its default. | (cascade's existing behavior) |
| `display: grid` (still unimplemented) | **Unchanged** — keeps the `default` block fallback + its existing log. Flex does not touch grid. | yes (existing) |

**Termination guarantee for §9.7** (the one place an algorithm bug could hang): the freeze loop is
`for len(unfrozen) > 0` and each pass freezes ≥ 1 item (or exits when no violations remain), so it runs
≤ N+1 iterations for N items. A unit test covers an all-violations round (everything freezes at once via the
total-violation rule).

## Testing

Every layer gets tests **in the same PR**. Hermetic, no network. Generated fixtures / inline HTML; goldens are
committed PNGs eyeballed by the controller.

1. **Cascade — `pkg/css` (parse/cascade + shorthand).**
   - Flex properties parse to the right `ComputedStyle` (direction, justify, align-items/self, grow/shrink/
     basis, order, column-gap/row-gap).
   - `flex` shorthand: `flex: 1` → `1 1 0%`; `flex: none` → `0 0 auto`; `flex: auto` → `1 1 auto`;
     `flex: 2 0 100px`; one-length and two-value forms. `gap` → `row-gap` + `column-gap`. Mirrors the existing
     `shorthand_test.go` assertions.
   - Unknown values skipped (degrade).

2. **Flex geometry — `pkg/layout/css` (the load-bearing layer).** Assert **actual fragment rects
   (x/y/w/h)**, hand-computed — not "an item exists" (a flex bug is a wrong position/size).
   - **Pure `resolveFlexibleLengths`** tested directly with hand-computed vectors: pure grow (surplus split by
     grow factor), pure shrink (deficit split by shrink × base, min-content-floored), mixed frozen items, the
     grow<1 / shrink<1 remaining-free-space rule, an all-violations freeze round (total-violation sign), zero
     free space.
   - **`flex-basis`** resolution: length, percentage (vs container main), `auto` → width, `content` →
     max-content.
   - **`justify-content`** each value → correct main offsets.
   - **`align-items`/`align-self`** each value → correct cross placement, incl. `stretch` (item cross = line,
     contents relayout) and `align-self` overriding `align-items`.
   - **`flex-direction: column`** — axes swapped (items stack vertically; main = height).
   - **`*-reverse`** — placement flipped.
   - **Automatic minimum** — `flex-shrink:1` item with no explicit min does not shrink below min-content;
     with `min-width:0` it does.
   - **`gap`** — `column-gap` consumes main space between items (offsets include the gap); **`order`**
     reorders.

3. **Anonymous-flex-item fixup — `pkg/layout/css`.** Structural assertions (like the anon-table-box tests):
   bare text/inline runs inside a flex container become anonymous block-level flex items; inline-level child
   boxes are blockified; whitespace-only runs handled.

4. **Golden images — `pkg/doctaculous` (`htmlGoldens` + committed PNGs).** A few eyeball-able layouts:
   `justify-content: space-between` row; a `flex-grow` distribution; `align-items: center` row of
   different-height items; `flex-direction: column`. Generated with
   `go test ./pkg/doctaculous -run TestHTMLGolden -update`; **the implementer STOPs after `-update` and hands
   back the PNG paths; the controller eyeballs every new PNG via the Read tool** (the implementer has no image
   vision; rendering caught real bugs every prior slice).

5. **WPT-style reftests — `pkg/doctaculous` (`wptReftests` + `NAME.html`/`NAME-ref.html`).** Flex's strongest
   correctness proof — a flex layout == the same boxes placed by hand:
   - `justify-content: space-between` of three fixed-width items == three inline-blocks / abs-positioned at the
     hand-computed x-offsets.
   - `flex-grow` row == divs at the grown widths.
   - `align-items: center` == abs-positioned boxes at the centered y-offsets.
   - `flex-direction: column` == stacked blocks.

6. **Byte-identical guard.** Flex **adds** a layout mode; **no existing page may change** (every current
   fixture uses block/inline/table). After each task, run goldens/reftests **without** `-update` and confirm
   `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` shows only **new** files. A changed
   existing golden means flex leaked into the block/table path — fix before proceeding.

7. **Degradation tests.** Each deferral asserts no-panic + the documented fallback + (where applicable) the
   debug log: `flex-wrap:wrap` → nowrap overflow, RTL → LTR, `align-items:baseline` → flex-start, empty flex
   container → zero-size, an over-shrunk `flex-shrink:0` item overflowing.

## Process reminders (carried from sub-projects #1–#8)

- **Sandbox blocks the Go build cache + TLS** — run `go` / `golangci-lint` / `gofmt` (and `gh`/`git push`)
  with `dangerouslyDisableSandbox: true`. A sandboxed `go`/lint failure with cache/permission/"no go files"
  errors is the sandbox, not a real failure — re-run disabled.
- **Editor diagnostics lag** — after a subagent adds a field/file, stale "undefined"/"unused"/"redeclared"/
  "not in go.mod" errors and **phantom `zz_*`/`*probe*` scratch files** appear that are not on disk. Trust
  `go build`/`go test` and `find . -name 'zz_*'`, not the panel.
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages (`./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...`), not the repo root. **No `//nolint`.**
  The repo **declines all "modernize" hints** (`max()`/`min()`/`slices.*`/range-over-int) — keep explicit
  `if x < y { x = y }` clamps, indexed `for i := 0; i < n; i++` loops, `sort.SliceStable`. golangci-lint flags
  `if !(a && b)` (QF1001 — write the De Morgan form) and bare `x.Close()` (errcheck — `_ = x.Close()`). The
  `unused` linter **is** enforced — a struct field/kind you add must be *read* by code in the same PR; defer
  adding a field until the task that reads it (in sub-project 7 grid fields were removed and re-added per
  consuming task).
- **Verify against the spec + the actual code, don't trust the handover/plan blindly.** Confirm the §9.7
  algorithm steps against the W3C spec before encoding the grow/shrink/align math (see the implementation gate
  above).
- **Branch discipline:** every subagent is on `feat/html-flexbox`; do NOT checkout/stash/switch branches, do
  NOT commit unless asked, and delete any `zz_*`/`*probe*` scratch file before finishing (confirm `git status`
  clean + `find . -name 'zz_*'` empty). The per-task reviewers WILL write throwaway probe tests — deleting
  them is an explicit instruction.
- **Two-stage review per task** (spec-fidelity + code-quality) **+ a holistic final review**; render real
  pages at milestones (the controller, via the Read tool). Strengthen weak assertions — geometry tests compare
  actual rects, not presence. Prefer the simpler mechanism — reuse `layoutInterior`/`layoutBlock`,
  `measure.go`, and the existing fragment/paint path; invent only the flex algorithm itself.
- **Update CLAUDE.md when the PR lands** — move flexbox from the §6 remaining-slices list into a new "Done"
  bullet (the flex FC, the properties + shorthands, the algorithm scope, what goldens/reftests cover, and the
  deferrals — wrap/RTL/baseline/cross-gap), and update the §6 done-slices parenthetical.

## Out of scope (do not gold-plate)

Multi-line/wrap + `align-content` (→ 9b), RTL/bidi, full baseline cross-alignment, grid (still the block
fallback), subgrid, `place-*` shorthands, `aspect-ratio`-driven flex sizing, and any pagination of a flex
container (the default stays a single tall image).
