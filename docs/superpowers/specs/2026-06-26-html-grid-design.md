# HTML rendering — CSS Grid (`display: grid`), explicit grid (sub-project 10)

**Date:** 2026-06-26
**Status:** Design approved; ready for implementation plan.
**Branch:** `feat/html-grid` (off `feat/html-flexbox`, PR #13 tip; rebase onto `main` if the stack has merged).
**Spec reference:** CSS Grid Layout Module Level 1 — <https://www.w3.org/TR/css-grid-1/>. The track-sizing
algorithm is §11; auto-placement is §8; box generation is §6; alignment is CSS Box Alignment Level 3
(<https://www.w3.org/TR/css-align-3/>).

## Summary

Turn the **block fallback** a `GridFC` box currently uses (`pkg/layout/css/block.go`, the `default` case of the
formatting-context switch at `layoutInterior`: it logs `… not yet implemented; falling back to block normal
flow` and stacks children as blocks) into a **real grid layout** — a new formatting context alongside the
block, inline, table, and flex ones. Grid lives entirely in the CSS layout engine (`pkg/layout/css`) and the
cascade (`pkg/css`); the `render.Device` seam, the PDF pipeline, and the DOCX pipeline are **untouched**. A
grid container produces ordinary positioned `Fragment`s that paint through the existing flatten/paint path.
**No new dependencies.**

This is sub-project 10 of the HTML-rendering roadmap. It is modeled structurally on **sub-project 9 (flexbox)**
and **sub-project 7 (tables)**: a self-contained layout mode above the block formatting context that reuses the
existing block+inline layout for the *contents* of each grid item (a grid item establishes a BFC, exactly like
a flex item and a table cell). The two closest templates are `pkg/layout/css/flex.go` (the freshest — its pure
`resolveFlexibleLengths` is the model for grid's pure track resolver) and `pkg/layout/css/table.go` (the other
2D-grid model — its `buildGrid` occupancy scan is the *pattern*, not shared code, for grid's auto-placement).

This slice also lands a **cross-cutting baseline-alignment backport**: it builds shared cross-baseline
machinery and wires it into grid **and** retrofits flexbox and table cells, replacing the `flex-start`/top
approximations those two currently ship for `baseline` alignment (see "The baseline backport").

## Scope

This is a **comprehensive explicit-grid slice** — substantially larger than a minimal first cut, by explicit
decision during brainstorming. It lands nearly all of CSS Grid Level 1's *explicit grid*. The two deferrals
are subgrid and RTL.

**In scope:**

- **Grid container + grid items.** `display: grid` and `display: inline-grid` establish a grid formatting
  context (`GridFC`). In-flow children become grid items; inline-level children are blockified (CSS Grid §6).
  Contiguous runs of inline content / text become **anonymous grid items** (the grid analogue of the
  anonymous-flex-item and anonymous-table-box fixups). `inline-grid` differs only in the container's outer
  role (an inline-level atom that flows like inline-block / inline-flex); item layout is identical.
- **Explicit track definitions** — `grid-template-columns` / `grid-template-rows` with **lengths, percentages,
  `fr` units, `minmax(min, max)`, `auto` / `min-content` / `max-content`, `repeat(N, …)`, and
  `repeat(auto-fill, …)` / `repeat(auto-fit, …)`**. This is the heart of the algorithm (the CSS Grid
  track-sizing algorithm, §11) and the biggest correctness risk — built test-first with hand-computed expected
  track sizes.
- **`grid-template-areas`** (named areas) + **`grid-area: <name>`** placement. A `"a a b" / "a a c"` template
  defines a name → rectangle map; `grid-area: name` places an item into that rectangle. `.` is an empty cell.
- **Item placement** — `grid-column` / `grid-row` with line numbers and spans (`grid-column: 1 / 3`,
  `grid-row: span 2`), the `grid-area` 4-line shorthand and the start/end longhands, negative line numbers
  (counted from the end), plus **auto-placement (§8.5) — sparse AND dense packing, row AND column flow**
  (`grid-auto-flow: row | column | dense | row dense | column dense`).
- **Implicit tracks** — items placed or spanning beyond the explicit grid create implicit tracks sized by
  `grid-auto-columns` / `grid-auto-rows` (default `auto`).
- **Gaps** — `gap` / `row-gap` / `column-gap` between tracks on **both** axes (already parsed in the cascade
  for flexbox: `ColumnGap` / `RowGap` on `ComputedStyle`).
- **Item-level alignment** — `justify-items` / `align-items` (item alignment within its grid area) and the
  `justify-self` / `align-self` overrides: `start` / `end` / `center` / `stretch` / `baseline`.
- **Content-distribution alignment** — `justify-content` / `align-content` distributing the whole set of tracks
  within the container when they don't fill it: `start` / `end` / `center` / `space-between` / `space-around`
  / `space-evenly`.
- **Baseline alignment** — real cross-baseline participation for `align-items: baseline` / `*-self: baseline`
  (and `justify-*: baseline` where it degenerates to start in the supported subset), **backported** to
  flexbox and table cells (see "The baseline backport").
- **Each grid item's contents lay out through the existing block/inline path** (`e.layoutBlock`), exactly as a
  flex item and a table cell do — a grid item establishes a BFC for its contents.

**Deferred to sub-project 10b (each with a graceful, tested fallback — see Degradation):**

- **Subgrid** (`grid-template-columns: subgrid` / `grid-template-rows: subgrid`) — CSS Grid Level 2.
  Parsed-and-ignored → treated as `none` (the track list is empty / implicit), logged.
- **RTL / `direction`.** The engine has **no** `direction`/bidi support anywhere (it was the sole table
  deferral AND a flexbox deferral). Column-axis line numbering in RTL is not achievable without bidi —
  `direction` is parsed but acted on as LTR (column line 1 = leftmost), logged. This is now the **third** slice
  to defer RTL; the eventual bidi/`direction` sub-project will unblock tables, flexbox, and grid together.
- **Line names in the track list** (`grid-template-columns: [start] 1fr [mid] 2fr [end]`) — the `[name]`
  tokens are parsed-and-ignored (the track sizes are still honored), logged. Named-line *placement*
  (`grid-column: start / end`) therefore falls through to auto-placement. A small additive follow-up.

The in-vs-deferred line mirrors how flexbox landed (single-line, RTL deferred) and tables landed (the core
with RTL deferred) — but the IN list here is deliberately much larger than either of those first slices.

## Architecture & layer fit

Grid touches these places, in dependency order:

1. **`pkg/css`** (cascade) — add the grid track/placement/alignment properties to `ComputedStyle`, parse them
   in `applyOne` (`cascade.go`), and add the `grid` / `grid-template` / `place-*` shorthand expansion in
   `shorthand.go`. **New parse surface: a real track-list parser** (`grid-template-columns: 1fr 2fr
   minmax(100px,1fr) repeat(3, auto)`) and a **template-areas parser** — a meaningful chunk, more than the
   flexbox shorthands were. Accept **both** alignment spellings (`start`/`end` for grid alongside
   `flex-start`/`flex-end` for flex) so the shared fields serve both modes. Unknown/unsupported values degrade
   (skipped). No layout here.
2. **`pkg/layout/cssbox`** — `DisplayGrid` and `GridFC` already exist (`box.go`). Add a `BoxAnonGridItem`
   anonymous-box kind (mirroring `BoxAnonFlexItem` / `BoxAnonTablePart`) for wrapped inline runs, register it
   in the `IsBlockLevel` helper, and add `DisplayInlineGrid` for `inline-grid`. Per the `unused` linter, a
   field/kind lands only when a same-PR consumer reads it.
3. **`pkg/layout/css/gridfix.go`** (new) — the anonymous-grid-item fixup: wrap contiguous in-flow inline
   runs/text inside a grid container as anonymous block-level grid items; blockify inline-level child boxes
   (CSS Grid §6). Structurally modeled on `flexfix.go`. Called from `Build` right after `fixupFlex(root)`
   (`build.go:65`) (flexfix.go and gridfix.go have since been merged into itemfix.go's fixupFlexGrid).
4. **`pkg/layout/css/grid.go`** (new) — `layoutGrid(...)`, registered as `case cssbox.GridFC` in `block.go`'s
   FC switch (replacing the `default` block-fallback for grid). Contains the six-phase algorithm + the pure
   `resolveTrackSizes`. Reuses `measure.go` (min/max-content) and `e.layoutBlock` (item contents → BFC).
5. **`pkg/layout/css/baseline.go`** (new, shared) — the cross-cutting baseline machinery: `firstBaselineOffset`
   (extract an item's first-baseline offset from its laid-out fragment) and `alignBaselineGroup` (shift a set
   of items so their first baselines coincide). Consumed by `grid.go`, `flex.go` (replacing the `flex-start`
   approximation at `flex.go:242`), and `table.go` (replacing the `vertical-align:baseline`→top at
   `table.go:740`).
6. **`pkg/layout/css/fragment.go`** — **verify** (do not rewrite) that grid items flatten/paint through the
   existing path. They are block boxes at computed offsets, like flex items and table cells, so this should
   need no change; the spec only commits to confirming it (flexbox needed none).

The `block.go` FC switch (`layoutInterior`, currently `block.go:491–511`) becomes:

```go
case cssbox.TableFC: in = e.layoutTable(ctx, b, contentW, contentX, childBand, childFC)
case cssbox.FlexFC:  in = e.layoutFlex(ctx, b, contentW, contentX, childBand, childFC)
case cssbox.GridFC:  in = e.layoutGrid(ctx, b, contentW, contentX, childBand, childFC)   // NEW
default:             // nothing left unimplemented → keep a defensive block fallback + log
```

`establishesNewBFC` (`block.go:941`) gains `DisplayGrid` / `DisplayInlineGrid` (a grid container establishes a
BFC, CSS Grid §2), exactly as the flex line was added there. `isBlockLevelOuter` (`anon.go:28`) gains
`DisplayInlineGrid` (an inline-grid container is inline-level outer, like `inline-flex`/`inline-block`).

**Structure decision (approved):** carve the **track-sizing algorithm into a pure, dependency-free function**
`resolveTrackSizes` (operates only on track sizing functions + per-track content-size contributions +
available space + gap → resolved track sizes), exactly as flexbox carved §9.7 into `resolveFlexibleLengths`.
This directly answers the highest correctness risk (the §11 multi-pass: `fr` distribution, `minmax`,
intrinsic content sizing) and is unit-testable with hand-computed vectors. It is called **once per axis**.

**No new dependencies.** Grid is pure-Go layout math.

## The grid properties (cascade — `pkg/css`)

New `ComputedStyle` fields, parsed in `applyOne`, with shorthands expanded in `shorthand.go`. The gap
properties (`ColumnGap` / `RowGap`, the `gap` shorthand) already exist from flexbox and are reused unchanged.

### Track sizing functions — the representation

A track list is `[]TrackSize`. Each `TrackSize` carries a **min sizing function** and a **max sizing
function** (a single function `f` means `minmax(f, f)`, except `fr` and intrinsic keywords as the spec
defines), plus an optional **flex factor** for `fr`:

```go
type TrackSizeKind int // length | percentage | flex(fr) | auto | min-content | max-content
type SizingFn struct { Kind TrackSizeKind; Len Length; Fr float64 }
type TrackSize struct { Min, Max SizingFn }            // minmax(Min, Max); a bare fn sets both
type TrackList struct { Tracks []TrackSize; Repeat []RepeatRun } // Repeat captures repeat(N|auto-fill|auto-fit, …)
```

`repeat(N, …)` could be expanded at parse time, but `repeat(auto-fill/auto-fit, …)` needs the container's
definite size to compute the repetition count — so the **track list is stored with repeat runs intact and
expanded at layout time** (Phase 1) when the container size is known. (`repeat(N)` expands the same way; the
count is just a constant.)

### Container properties (read off the grid container box)

| Property                | `ComputedStyle` field        | Values parsed                                                                        | Default |
|-------------------------|------------------------------|--------------------------------------------------------------------------------------|---------|
| `grid-template-columns` | `GridTemplateColumns TrackList` | track list (length/%/`fr`/`minmax()`/`auto`/`min-content`/`max-content`/`repeat()`) | none    |
| `grid-template-rows`    | `GridTemplateRows TrackList`    | same grammar                                                                        | none    |
| `grid-template-areas`   | `GridTemplateAreas GridAreas`   | rows of cell-name tokens (`"a a b"`); `.` = empty                                   | none    |
| `grid-auto-columns`     | `GridAutoColumns []TrackSize`   | track sizing function(s) for implicit columns                                       | `auto`  |
| `grid-auto-rows`        | `GridAutoRows []TrackSize`      | track sizing function(s) for implicit rows                                          | `auto`  |
| `grid-auto-flow`        | `GridAutoFlow string`           | `row` / `column` / `row dense` / `column dense` / `dense`                           | `row`   |
| `justify-items`         | `JustifyItems string`           | `start` / `end` / `center` / `stretch` / `baseline`                                 | `stretch` |
| `align-items`           | `AlignItems string`             | `start`/`end`/`center`/`stretch`/`baseline` (shared with flex: also `flex-start`/`flex-end`) | `stretch` |
| `justify-content`       | `JustifyContent string`         | `start`/`end`/`center`/`space-between`/`space-around`/`space-evenly` (shared: also `flex-*`) | `start` |
| `align-content`         | `AlignContent string`           | same value set                                                                     | `start` |
| `row-gap` / `column-gap`| `RowGap` / `ColumnGap Length`   | length/percentage (already parsed)                                                  | 0       |

### Item properties (read off each grid item box)

| Property            | `ComputedStyle` field    | Values                                                                  | Default |
|---------------------|--------------------------|------------------------------------------------------------------------|---------|
| `grid-column-start` | `GridColumnStart GridLine` | `<integer>` (may be negative) / `span <n>` / `auto` / `<name>` (ignored) | `auto`  |
| `grid-column-end`   | `GridColumnEnd GridLine`   | same                                                                    | `auto`  |
| `grid-row-start`    | `GridRowStart GridLine`    | same                                                                    | `auto`  |
| `grid-row-end`      | `GridRowEnd GridLine`      | same                                                                    | `auto`  |
| `grid-area`         | (expands to the four above + name) | `<name>` or `<row-start> / <col-start> / <row-end> / <col-end>`         | `auto`  |
| `justify-self`      | `JustifySelf string`       | `auto`/`start`/`end`/`center`/`stretch`/`baseline`                      | `auto`  |
| `align-self`        | `AlignSelf string`         | `auto`/`start`/`end`/`center`/`stretch`/`baseline` (shared with flex)   | `auto`  |

`GridLine` = `{Auto bool; Span bool; N int; Name string}` (a resolved line index, a span count, `auto`, or a
named line carried-but-ignored).

### Shorthands (`shorthand.go`)

- `gap: <a> [<b>]` → `row-gap`/`column-gap` (done — reused).
- `grid-column: <start> [/ <end>]` → `grid-column-start`/`-end`; `grid-row` likewise.
- `grid-area: <name>` → place by the named area; `grid-area: a / b / c / d` → the four start/end longhands.
- `grid-template: <rows> / <columns>` (and the areas-bearing form `"a b" 1fr "c d" 2fr / auto auto`) →
  `grid-template-rows`/`-columns`/`-areas`.
- `grid: <grid-template>` (the explicit-grid subset) → `grid-template-*`; the auto-flow forms
  (`grid: auto-flow / …`) degrade to setting what they can + a log.
- `place-items: <align> [<justify>]` → `align-items`/`justify-items`; `place-content` → `align-content`/
  `justify-content`; `place-self` → `align-self`/`justify-self`.

Unknown values for any property are skipped (the cascade's existing degrade behavior), leaving the default.

## The grid algorithm (`pkg/layout/css/grid.go`)

`layoutGrid(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext)
interior` — signature matches `layoutTable` / `layoutFlex`. Six phases (CSS Grid §7–§11):

**Phase 1 — Build the explicit + implicit grid (track lists).**
Expand `grid-template-columns` / `grid-template-rows` into ordered `[]TrackSize` track lists:
- `repeat(N, …)` → N copies of the inner list.
- `repeat(auto-fill, …)` / `repeat(auto-fit, …)` → compute the repetition count from the container's definite
  size on that axis (`contentW` for columns; the container's definite content height for rows — indefinite →
  **1 repetition**, the spec's fallback). `auto-fit` additionally marks tracks that end up empty (after
  placement) for collapse (size 0, gaps around them collapse). The column axis is the common case (the
  container inline size is always definite — `contentW`).
- `grid-template-areas` → a `name → {rowStart,rowEnd,colStart,colEnd}` map, and the implied explicit grid
  size (max rows/cols across the area rows).

**Phase 2 — Place the items (§8).** Build an occupancy grid (the *pattern* from `table.go`'s `buildGrid`, not
shared code — grid placement has its own rules). For each item, in document order:
- **Definite placement** (`grid-column`/`grid-row`/`grid-area` resolving to explicit lines or spans, or a
  `grid-area: name` that exists): occupy that rectangle; create implicit tracks (sized by `grid-auto-columns`/
  `-rows`) if it extends past the explicit grid; negative lines count from the end of the explicit grid.
- **Auto-placement** (anything still `auto` on the flow axis): a placement cursor walks in `grid-auto-flow`
  order — **row** (fill across columns, then next row) or **column** (fill down rows, then next column) — in
  **sparse** mode (cursor only advances forward) or **dense** mode (cursor restarts from the grid origin each
  item, backfilling earlier holes). An item with a definite cross-axis position but auto flow-axis position is
  placed in the first available slot in its locked cross track. Spans honored throughout. Items spanning past
  the explicit grid create implicit tracks.

**Phase 3 — Size the tracks (§11) — the pure resolver.**
`resolveTrackSizes(tracks []TrackSize, items []trackItem, available, gap float64) []float64`, called **once per
axis** (columns sized first — the column widths feed each item's block layout, which yields the row-axis
content contributions). `trackItem` = `{start, span int; minContent, maxContent float64}` (an item's content
contribution to the tracks it spans, from `measure.go`). The §11 multi-pass:
1. **Initialize each track's base size and growth limit** from its sizing function: a fixed length/% → base =
   limit = that length (% resolved against `available`); `auto`/`min-content` → base from min-content
   contributions, limit = ∞ (or max-content for `max-content`); `fr` → base 0, limit ∞, flex factor recorded;
   `minmax(a,b)` → base from `a`, limit from `b`.
2. **Resolve intrinsic track sizes** from item contributions: distribute each item's min-content (then
   max-content up to growth limits) to the tracks it spans — **single-span items first**, then multi-span
   items distributed across their spanned intrinsic tracks (the spec's "distribute extra space" sub-routine).
3. **Maximize tracks**: distribute the free space (`available − Σ base − gap`) to non-flexible tracks up to
   their growth limits.
4. **Expand flexible (`fr`) tracks**: the `fr` unit = leftover space (`available − Σ non-flex base − gap`) ÷
   Σ flex factors (with the spec's "≥ 1fr per fr, floor at the track's base" handling); each `fr` track's
   size = max(its base, fr-unit × its flex factor). This is the grid analogue of flex-grow.
5. Each track's **used size** is its final base size (after maximize/expand).

This pure function is the **unit-test arbiter** (hand-computed vectors).

> **Implementation gate (carried as an explicit plan-task instruction):** before encoding Phase 3,
> **fetch and verify the §11 step order against the W3C spec** (<https://www.w3.org/TR/css-grid-1/> §11
> "Grid Sizing" / §11.5 "Resolve Intrinsic Track Sizes" / §11.7 "Expand Flexible Tracks"). The multi-pass
> structure (intrinsic sizes → maximize → expand flexible) is easy to get subtly wrong, exactly like flexbox
> §9.7. **NOTE:** the W3C single-page spec is LARGE and `WebFetch`'s summarizer **truncates before reaching
> §11** (this happened for flexbox §9.7 from three different URLs); if §11 can't be fetched directly, encode it
> from known-good knowledge and rely on **hand-computed unit tests as the arbiter** — a change that forces
> inverting a passing track-sizing test is a red flag; re-verify the algorithm, don't edit the test.

**Phase 4 — Position the tracks + content-distribution alignment.**
Lay tracks end-to-end from the container content-box origin, inserting `column-gap`/`row-gap` between adjacent
tracks. When Σ track sizes + gaps < the container's content size on that axis, **`justify-content`
(column axis) / `align-content` (row axis)** distributes the leftover: `start` (default — no shift), `end`,
`center`, `space-between`/`space-around`/`space-evenly` (extra space inserted between/around tracks, adjusting
each track's position). `stretch` content-distribution (growing auto tracks to fill) is folded into Phase 3's
maximize step where applicable.

**Phase 5 — Lay out + align each item.**
Each item's **grid area** = the rectangle spanning its placed tracks (track positions + sizes + the internal
gaps between spanned tracks). Lay the item's contents via
`e.layoutBlock(ctx, item, <area-width>, …, own floatContext, own positionedContext, item-as-CB)` — the item
establishes a **BFC** (the flex-item / table-cell pattern). Abs/fixed descendants inside an item resolve
against the item's box now (mirroring the cell/flex path) so they are not dropped. Then place the laid-out
content within the grid area per `justify-self`/`align-self` (falling back to `justify-items`/`align-items`):
- `start` / `end` / `center` → position the item's margin box within the area on that axis.
- `stretch` (the default) → set the item's used size to the area size **and relayout** its contents at that
  measure (so content reflows to the stretched box), **unless** the item has a definite size on that axis. The
  relayout is one extra `layoutBlock` (the flex/table precedent).
- `baseline` → via the shared `baseline.go` (see "The baseline backport"): align the item's first baseline to
  the max first baseline of the items sharing its baseline-alignment group (the items in its row for
  `align-*`).

**Phase 6 — Emit fragments.** Each item becomes a positioned child `Fragment` at its (x, y) area offset
(adjusted by alignment) and (w, h) used size. The container `interior.contentHeight` is the total row extent
(last row track bottom). For `inline-grid`, the container flows as an inline atom (like `inline-flex`). Grid
does **not** set `interior.intrinsicWidth` (that field shrink-to-fits a *table* box); a block-level grid takes
the resolved block content width like flex does. **`inline-grid` shrink-to-fit:** an inline-grid's used inline
size is the sum of its resolved column track sizes + gaps (computed in Phase 3/4), reported so the inline atom
sizes to its grid — handled in the inline-atom path, not via `intrinsicWidth`. Everything flows through the
existing flatten/paint path (`fragment.go`).

**Known approximation (carried from flexbox/tables, documented):** `measureMaxContent` returns a content
**width**; sizing **row** tracks to `auto`/min-content/max-content content reuses it as a height proxy — the
same documented approximation flexbox (`flex.go` ~line 519) and tables ship. A true vertical intrinsic-sizing
pass is a cross-cutting follow-up for all three modes, out of scope here.

## The baseline backport (`pkg/layout/css/baseline.go`)

`baseline` cross-axis alignment is currently **deferred in all three layout modes**: flexbox approximates it to
`flex-start` (`flex.go:242–243`, logged) and table cells treat `vertical-align: baseline` as top
(`table.go:740`, "baseline (≈ top here)"). This slice builds the shared machinery once and wires all three.

**The machinery (new `baseline.go`):**

- `firstBaselineOffset(frag *Fragment) (float64, bool)` — the offset from an item's **content-box top** down to
  its **first baseline**. The first in-flow line box's `BaselineY` (the `Fragment`/`LineFragment` already carries
  it — `fragment.go:157`, `BaselineY float64`) minus the content-box top, or recursively the first in-flow
  block child's first baseline. For a baseline-free item (no text / no line boxes), the **synthesized baseline =
  the margin-box bottom** (CSS Box Alignment §9.4.3) → return `ok=false` so the caller falls back to `start`
  (the spec's "first/last baseline … with no baseline → start" behavior, kept simple).
- `alignBaselineGroup(items []baselineItem) (extraCross float64)` — given the items sharing one
  baseline-alignment context (a flex line / a grid row / a table row), find the max `firstBaselineOffset` and
  shift each participating item **down** by `(maxBaseline − itsBaseline)`; report the extra cross size the
  group needs to fit the shifted items (so the line / row / row-band grows to contain them).

**Wiring:**

- **grid.go** — items with `align-self`/`align-items: baseline` that share a grid row form a group →
  `alignBaselineGroup`; the row track's used size grows by the reported extra (re-running Phase 4 positioning
  for rows below).
- **flex.go** — replace the `flex-start` approximation (`flex.go:242–243`): items with `baseline` on the single
  line form a group → `alignBaselineGroup`; the line cross size grows by the reported extra. The `flex-start`
  log is **removed**; the flexbox baseline degradation test flips to asserting real baseline coincidence.
- **table.go** — replace the `vertical-align: baseline`→top branch in `applyCellVAlign` (the `default` case,
  "baseline (≈ top here)"): cells in a row with `vertical-align: baseline` baseline-align their first baselines
  via `alignBaselineGroup`, shifting cell content with the existing `shiftCellContent` helper (which already
  translates a cell's children, floats, and `Lines[i].BaselineY` by `dy`) within the row band; the row height
  grows by the reported extra. This requires lifting the per-cell `applyCellVAlign` shift into a per-row pass
  for the baseline group (top/middle/bottom stay per-cell), since baseline alignment is a cross-cell decision.

**This deletes three deferral approximations in one PR** and converts their degradation tests into positive
baseline assertions. **Table is the most invasive of the three** (cell vertical-align interacts with row-height
solving). The instruction to the implementer is explicit: do all three in this PR; if the table wiring proves
materially gnarlier than grid/flex, **flag it loudly and stop for a decision — do not silently split it out**.

## Degradation & error handling

Per the project's non-negotiable rule, unsupported cases **degrade gracefully (skip + debug log), never
panic**; recovery is at the existing page boundary (`layoutGrid` is below it — no new recover needed). Each
deferral has a tested fallback.

| Case | Behavior | Logged |
|------|----------|--------|
| `subgrid` (`grid-template-*: subgrid`) | Treated as `none` — no inherited tracks; the axis uses implicit `auto` tracks. | yes |
| RTL / `direction: rtl` | Acts LTR (column line 1 = leftmost). `direction` parsed but not honored (no bidi anywhere). | yes |
| Line names in a track list (`[name]`) | Parsed-and-ignored; track sizes still honored. Named-line placement falls through to auto. | yes |
| `repeat(auto-fill/auto-fit, …)` with an **indefinite** container size on that axis | 1 repetition (the spec fallback). | yes |
| `align-items: baseline` / `*-self: baseline` on a **baseline-free** item (no text) | Falls back to `start` (synthesized baseline = item bottom; `firstBaselineOffset` returns `ok=false`). | no (spec behavior) |
| Malformed `grid-template-areas` (ragged rows / non-rectangular named area) | The whole template is ignored (no named areas); items auto-place. | yes |
| Item placement referencing a track count beyond the grid | Implicit tracks created (sized by `grid-auto-*`); never out-of-range. | no (spec behavior) |
| Empty / no-in-flow-children grid container | Zero-size `interior` (`contentHeight: 0`), no items, no panic. | no |
| `fr` track with zero leftover space, all-`auto` tracks with zero content, contradictory spans | Tracks resolve to their base sizes (possibly 0); no spin, no panic — `resolveTrackSizes` is a bounded multi-pass. | — |
| Unknown property value (e.g. `justify-content: foo`) | Skipped by the cascade → property keeps its default. | (cascade's existing behavior) |
| `display: flex` / `table` / block / inline | **Unchanged** — grid does not touch their layout paths (except the deliberate `baseline` backport into flex/table, which is positive new behavior, not degradation). | — |

**Termination guarantee for `resolveTrackSizes`** (the one place an algorithm bug could hang): every phase is a
finite pass over a fixed track/item set (initialize → distribute intrinsic → maximize → expand flexible), with
no unbounded loop — it runs in O(tracks × items) and always terminates. A unit test covers the
zero-leftover-space and all-intrinsic-tracks cases.

## Testing

Every layer gets tests **in the same PR**. Hermetic, no network. Inline HTML / generated fixtures; goldens are
committed PNGs eyeballed by the controller. Mirrors the flexbox slice's test structure (it worked well).

1. **Cascade — `pkg/css` (parse/cascade + shorthand) — the biggest new parse surface.**
   - **Track-list parser** (test thoroughly): `1fr 2fr`, `100px 1fr`, `minmax(100px, 1fr)`, `auto`,
     `min-content max-content`, `repeat(3, auto)`, `repeat(2, 1fr 2fr)`, `repeat(auto-fill, 100px)`,
     `repeat(auto-fit, minmax(100px, 1fr))`, mixed lists. Assert the parsed `TrackList` structure directly.
   - **Template-areas** parser: `"a a b" "a a c"` → the name→rect map + implied grid size; ragged / non-
     rectangular → ignored.
   - **Placement**: `grid-column: 1 / 3`, `grid-row: span 2`, negative lines (`grid-column: 1 / -1`),
     `grid-area: foo`, the 4-line `grid-area: 1 / 1 / 3 / 2`.
   - **Auto-flow**: `grid-auto-flow: row dense`, `column`, `dense`.
   - **Shorthands**: `grid-template`, `place-items`/`place-content`/`place-self`, `grid-column`/`grid-row`.
   - Both alignment spellings (`start` and `flex-start`) parse. Unknown values skipped (degrade).

2. **Grid geometry — `pkg/layout/css` (the load-bearing layer).** Assert **actual fragment rects (x/y/w/h)**,
   hand-computed — not "an item exists" (a grid bug is a wrong position/size).
   - **Pure `resolveTrackSizes`** tested directly with hand-computed vectors: fixed tracks, % tracks, `fr`
     distribution (surplus split by flex factor), `minmax()` (base vs limit), `auto`/min-content/max-content
     (via measure.go contributions), single-span vs multi-span intrinsic distribution, mixed intrinsic+flexible,
     zero-leftover-space, an all-`auto` row.
   - **Placement**: line-number placement, span placement, **auto-placement sparse**, **auto-placement dense**
     (backfilling a hole), **row flow AND column flow**, implicit-track creation, `grid-template-areas`
     placement, negative line numbers.
   - **`repeat`**: `repeat(N)` track count; `repeat(auto-fill, …)` count from a definite container;
     `repeat(auto-fit, …)` empty-track collapse.
   - **Gaps** both axes (offsets include the gaps).
   - **Item alignment**: `justify-items`/`align-items`/`*-self` each value incl. `stretch` (item size = area,
     contents relayout) and `*-self` overriding `*-items`.
   - **Content-distribution**: `justify-content`/`align-content` each value (tracks shifted/spaced within the
     container).
   - **Baseline**: items with `align-items: baseline` in a row → first baselines coincide (assert the shifted
     rects).

3. **Anonymous-grid-item fixup — `pkg/layout/css`.** Structural assertions (like the anon-flex-item / anon-
   table-box tests): bare text/inline runs inside a grid container become anonymous block-level grid items;
   inline-level child boxes are blockified; whitespace-only runs handled.

4. **Baseline backport — `pkg/layout/css`.** The shared `firstBaselineOffset` / `alignBaselineGroup`
   unit-tested directly (incl. the no-baseline → `ok=false` → start fallback). The **three converted
   degradation tests** (grid, flex, table) now assert **real first-baseline coincidence** (rects), not the old
   approximation.

5. **Golden images — `pkg/doctaculous` (`htmlGoldens` + committed PNGs at `testdata/golden/html-<name>.png`).**
   Eyeball-able layouts: a 2×2 fixed grid, an `fr`-distributed grid, a grid with a spanning item, an auto-placed
   grid, a `grid-template-areas` layout. Generated with
   `go test ./pkg/doctaculous -run TestHTMLGolden -update`; **the implementer STOPs after `-update` and hands
   back the PNG paths; the controller eyeballs every new PNG via the Read tool** (the implementer has no image
   vision; rendering caught real bugs every prior slice).

6. **WPT-style reftests — `pkg/doctaculous` (`wptReftests` + `testdata/wpt/css21-normal-flow/NAME.html` /
   `NAME-ref.html`).** Grid's strongest correctness proof — a grid layout == the same boxes placed by hand at
   the computed positions (absolute / inline-block):
   - a 2×2 fixed grid == four abs boxes at the track offsets;
   - an `fr` grid == divs at the distributed widths;
   - a spanning-item grid == abs boxes at the spanned rectangle;
   - a `grid-template-areas` grid == abs boxes at the named-area rectangles.

7. **Byte-identical guard (a dedicated checkpoint task).** Grid **adds** a layout mode; **no existing page may
   change** (every current fixture uses block/inline/table/flex). After each task, run goldens/reftests
   **without** `-update` and confirm `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata`
   shows only **new** files. **Caveat — the baseline backport:** any **existing flex/table golden or reftest
   that exercised `baseline`** legitimately changes (baseline now does something real). The checkpoint task
   first **identifies those up front** (grep the existing fixtures for `baseline`); they are regenerated
   deliberately and eyeballed, and the guard becomes "only new files, **plus** these N named baseline-affected
   goldens, each eyeballed." If no existing golden used `baseline`, the guard stays strict (only new files). A
   changed existing golden that is *not* on the baseline-affected list means grid (or the backport) leaked into
   another path — fix before proceeding.

8. **Degradation tests.** Each deferral asserts no-panic + the documented fallback + (where applicable) the
   debug log: `subgrid` → `none`, RTL → LTR, line-names → ignored, `repeat(auto-fill)` with indefinite size →
   1 repetition, malformed `grid-template-areas` → ignored, empty grid container → zero-size fragment,
   `baseline` on a text-free item → start. **Every deferral gets a test** (the flexbox holistic review caught
   that RTL→LTR was logged-but-untested; don't repeat that gap).

## Process reminders (carried from sub-projects #1–#9)

- **Sandbox blocks the Go build cache + TLS** — run `go` / `golangci-lint` / `gofmt` (and `gh`/`git push`)
  with `dangerouslyDisableSandbox: true`. A sandboxed `go`/lint failure with cache/permission/"no go files"
  errors is the sandbox, not a real failure — re-run disabled.
- **Editor diagnostics lag** — after a subagent adds a field/file, stale "undefined"/"unused"/"redeclared"/
  "not in go.mod" errors and **phantom `zz_*`/`*probe*` scratch files** appear that are not on disk. Trust
  `go build`/`go test` and `find . -name 'zz_*'`, not the panel.
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages (`./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...`), not the repo root. **No `//nolint`.**
  The repo **declines all "modernize" hints** (`max()`/`min()`/`slices.*`/range-over-int/`SplitSeq`) — keep
  explicit `if x < y { x = y }` clamps, indexed `for i := 0; i < n; i++` loops, `sort.SliceStable`.
  golangci-lint flags `if !(a && b)` (QF1001 — write the De Morgan form), bare `x.Close()` (errcheck —
  `_ = x.Close()`), `S1011` (use `append(dst, src...)` not a copy loop), and `ineffassign` (a value assigned
  then never read). The `unused` linter **is** enforced — a struct field/kind you add must be *read* by code in
  the same PR; defer adding a field until the task that reads it.
- **Verify against the spec + the actual code, don't trust the handover/plan blindly.** Confirm the §11
  track-sizing steps against the W3C spec before encoding the `fr`/`minmax`/intrinsic math (see the
  implementation gate above). Across #9 the plan named a non-existent free function (`blockLevel`) when the real
  API was the `IsBlockLevel()` METHOD, and test items lacked a `FontFamily` so text measured 0-width — the
  implementers caught these by reading the actual code.
- **Two-stage review per task** (spec-fidelity + code-quality) **+ a holistic final review**; render real
  pages at milestones (the controller, via the Read tool). The flexbox holistic review caught a REAL BUG the
  per-task reviews and the entire 40-test corpus missed (`placeFlexFragment` collapsed column items to x=0 when
  the container wasn't at page x=0, because every test placed the container at x=0). **Have the holistic
  reviewer write adversarial probes that vary the conditions the unit tests hold fixed** — container position,
  nesting, both axes, items hitting min AND max in one pass, deeply nested grid-in-grid — and **DELETE every
  probe**.
- **Strengthen weak assertions.** Geometry tests must compare actual rects (x/y/w/h), not "an item exists" — a
  grid bug is a wrong *position/size*, which presence checks miss.
- **Prefer the simpler mechanism.** Reuse `layoutInterior`/`layoutBlock` for item contents, `measure.go` for
  track content sizing, the table occupancy-scan **pattern** (not code) for placement, the existing
  fragment/paint path — invent only the track-sizing + placement algorithm itself.
- **Branch discipline:** every subagent is on `feat/html-grid`; do NOT checkout/stash/switch branches, do NOT
  commit unless asked, scope every `git add` to only your files (the sub-project A HTML→PDF-writer docs stay
  dirty/untouched — never `git add -A`/`git add .`), and delete any `zz_*`/`*probe*` scratch file before
  finishing (confirm `git status` clean + `find . -name 'zz_*'` empty). The per-task reviewers WILL write
  throwaway probe tests — deleting them is an explicit instruction.
- **Update CLAUDE.md when the PR lands** — move grid from the §6 remaining-slices list into a new "Done" bullet
  (the grid FC, the properties + track-list/template-areas parsers + shorthands, the algorithm scope, the
  baseline backport, what goldens/reftests cover, and the deferrals — subgrid/RTL/line-names), update the §6
  done-slices parenthetical, add a "Grid fidelity follow-ups" note (mirroring the flexbox/table/positioning
  notes — the row-content height approximation, line names, named-line placement), and **note the baseline
  backport in the flexbox and table follow-up notes** (their `baseline` deferrals are now resolved). **List the
  deferrals the draft Done bullet forgets** — in #9 the line-cross-clamp and column-basis-width-proxy
  approximations had to be added after the fact.

## Out of scope (do not gold-plate)

Subgrid (CSS Grid Level 2 → 10b), RTL/bidi (the cross-cutting bidi sub-project), named-line *placement* (the
`[name]` tokens are parsed-but-ignored), a true vertical intrinsic-sizing pass for content-sized row tracks
(the documented width-as-height-proxy approximation is shared with flex/table), masonry layout, and any
pagination of a grid container (the default stays a single tall image).
