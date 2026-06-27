# Handover — Sub-project 10: CSS Grid (`display: grid`)

**Status:** Not started. Sub-project 9 (single-line flexbox) is **DONE** — `display:flex`/`inline-flex` now lays out as a
real single-line flex container (axis-abstracted `layoutFlex`, the §9.7 freeze loop, flex-basis + §4.5 auto-minimum,
justify-content, align-items/self + stretch, order, gap, inline-flex). See CLAUDE.md "Done" (the flexbox bullet) and
`docs/superpowers/specs/2026-06-26-html-flexbox-design.md`. It merged as the tip of the HTML stack:
**PR #13 (`feat/html-flexbox`) on top of #12 (`feat/html-webfonts`)**.

CSS Grid is the **next** roadmap item (CLAUDE.md §6 remaining-slices: "**grid** (today grid falls back to block
normal flow; flexbox and tables are now real layout modes)"). Grid is the closest analogue to **flexbox (sub-project 9)
and tables (sub-project 7)**: a self-contained layout mode above the block formatting context that reuses the existing
block+inline layout for the *contents* of each grid item. Read BOTH of those slices before starting — flexbox is the
freshest template (`docs/superpowers/specs/2026-06-26-html-flexbox-design.md` + its plan + the flexbox Done bullet), and
tables (`docs/superpowers/specs/2026-06-25-html-tables-design.md`) is the other self-contained-layout-mode precedent with
a 2D grid model. The shape of that work (new layout file, box-tree vocabulary, fixups, a golden + reftests, RTL deferred,
the §9.7-style intricate-algorithm verification gate) is the template for this one.

**Next action:** Same flow as every prior slice — brainstorm → spec (`docs/superpowers/specs/`) → plan
(`docs/superpowers/plans/`) → subagent-driven execution (per task: implement → spec-review → code-quality-review → fix)
→ holistic final review → finish branch / stacked PR. Grid is **large** (the CSS Grid track-sizing algorithm is at least
as intricate as flexbox's §9.7 — `fr` units, `minmax()`, `auto`/min-content/max-content tracks, `repeat()`,
auto-placement); a written spec + plan are essential, and the scope decision (how much of the spec to land in slice 1 vs
defer) is the load-bearing brainstorm question.

---

## The PR stack — where to branch from

```
main ← #2 css-parse-cascade ← #3 box-generation ← #4 block-inline-flow ← #5 replaced-images
     ← #6 floats ← #7 positioning ← #8 overflow ← #9 z-index ← #10 zindex-6b
     ← #11 tables ← #12 web-fonts ← #13 flexbox
```

If the stack has merged to `main` by the time you start, branch grid off `main`. Otherwise branch **off
`feat/html-flexbox`** (the tip, PR #13). Name it e.g. `feat/html-grid`. Tell every subagent: you are on
`feat/html-grid`, do NOT checkout/stash/switch branches, do NOT commit unless asked, scope every `git add` to only your
files, and delete any `zz_*`/`*probe*` scratch file before finishing (confirm `git status` clean +
`find . -name 'zz_*'` empty). The per-task reviewers (and the holistic reviewer) WILL write throwaway probe tests — make
deleting them an explicit instruction. **The editor panel showed phantom `zz_*` files AND phantom "undefined: X" errors
all through sub-project 9 even after they were deleted/defined — trust `go test`/`go build`/`find`, NOT the panel.**

### ⚠️ A wrinkle on this branch: out-of-band sub-project A commits
While sub-project 9 was in flight, **5 commits + uncommitted edits for a separate "HTML→PDF writer" sub-project**
(`docs/superpowers/specs/2026-06-26-html-to-pdf-writer-design.md` and `…/plans/2026-06-26-html-to-pdf-writer.md`) landed
on `feat/html-flexbox` out-of-band (committed by the user from a parallel session). They are NOT part of flexbox. The
user said they will split those onto their own branch themselves. **Before you branch grid off `feat/html-flexbox`,
check `git log` / `git status` — if those sub-project A commits/edits are still present, confirm with the user whether
they've been moved**, so grid doesn't inherit unrelated work. (PR #13's description notes this split is pending.)

## Scope (propose this in the spec; the in-vs-deferred split is the key brainstorm decision)

CSS Grid Layout (CSS Grid Level 1, https://www.w3.org/TR/css-grid-1/). Turn the **block fallback** a `GridFC` box
currently uses (it logs "`… not yet implemented; falling back to block normal flow`" and stacks children as blocks —
`pkg/layout/css/block.go`, the `default` case of the formatting-context switch) into a **real grid layout**. The
recommended slice-1 scope (confirm/adjust in brainstorm) — grid is BIG, so the single-line-style "core first, defer the
rest" cut matters even more than it did for flexbox:

**Likely in scope (an explicit, fixed grid — the common case):**
- **The grid container + grid items** — `display: grid` (and `inline-grid`) establishes a grid formatting context; in-flow
  children become grid items (with the usual blockification of inline-level items + anonymous-item handling for stray
  inline runs, the grid analogue of the flexbox `flexfix.go` and the table `tablefix.go`).
- **Explicit track definitions** — `grid-template-columns` / `grid-template-rows` with **lengths, percentages, `fr`
  units, `minmax()`, `auto`, and `repeat()`** (at minimum the fixed-count `repeat(N, …)` form). This is the heart of the
  algorithm (the CSS Grid track-sizing algorithm, §11) and the biggest correctness risk — build it test-first with
  hand-computed expected track sizes. `fr` distribution is the grid analogue of flexbox `flex-grow`; `minmax()` and the
  min-content/max-content track sizing reuse `pkg/layout/css/measure.go` (same as tables/flexbox).
- **Item placement** — `grid-column` / `grid-row` with line numbers and spans (`grid-column: 1 / 3`,
  `grid-column: span 2`), plus **auto-placement** (the auto-placement algorithm §8.5, "sparse" packing at minimum) for
  items without explicit placement.
- **The grid item content** lays out through the existing block/inline path (a grid item establishes a BFC), exactly like
  a flex item and a table cell — reuse `layoutInterior`/`layoutBlock`.
- **Gaps** — `gap`/`row-gap`/`column-gap` (already parsed in the cascade for flexbox! `ColumnGap`/`RowGap` on
  `ComputedStyle` exist — grid reuses them between tracks on BOTH axes, unlike single-line flex which only used one).
- **Alignment** — at minimum `justify-items`/`align-items` (item alignment within its grid area) and the `*-self`
  overrides; `justify-content`/`align-content` (distributing the whole grid when tracks don't fill the container) is a
  reasonable defer if the slice gets too big.

**Likely deferrals (state each in the spec's Degradation section with a graceful fallback):**
- **`grid-template-areas`** (named areas) — a large, self-contained add; defer to 10b. Items placed by line/span +
  auto-placement is a complete slice without it. If deferred, `grid-template-areas` is parsed-and-ignored (logged), and
  `grid-area: name` falls back to auto-placement.
- **`repeat(auto-fill/auto-fit, …)`** — the auto-repeat forms need the container size to resolve the repeat count; the
  fixed-count `repeat(N, …)` is the useful core. Defer auto-fill/auto-fit (logged), or treat as `repeat(1, …)`.
- **`grid-auto-columns`/`grid-auto-rows`** for implicit tracks beyond the explicit grid — at minimum support `auto`
  implicit tracks; the full `grid-auto-flow: column`/`dense` packing can defer (sparse row-flow first).
- **Subgrid** (`subgrid`) — definitely out of scope (CSS Grid Level 2); parse-and-ignore.
- **`justify-content`/`align-content`** (distributing tracks within the container) — defer if the slice is too big
  (treat as start). Item-level `justify-items`/`align-items`/`*-self` is the more important half.
- **RTL / `direction`** — the engine has **no** `direction`/bidi support anywhere (it was the sole table deferral AND a
  flexbox deferral). Column-axis line numbering in RTL is not achievable without bidi — parse `direction` but act LTR,
  logged. Pin this in the spec. (This is now the THIRD slice to defer RTL; the eventual bidi/`direction` sub-project will
  unblock tables, flexbox, and grid together.)
- **Baseline alignment** across grid items with different fonts — `align-items: baseline` can approximate to `start`
  initially (state it), exactly as flexbox did.

The in-vs-deferred line (especially **explicit-grid-first vs template-areas-included**, and **how much of track sizing**)
is THE brainstorm decision. Recommendation: **an explicit fixed grid fully — `grid-template-columns`/`-rows` with
lengths/%/`fr`/`minmax()`/`auto`/`repeat(N)`, line-number + span placement, sparse auto-placement, item alignment,
gaps on both axes — and defer `grid-template-areas`, auto-fill/auto-fit, dense packing, subgrid, content-distribution
alignment, and RTL to 10b.** That mirrors how flexbox landed single-line-first and tables landed the core with RTL
deferred.

## What already exists (the foundation — verify against the code before trusting this)

- **`pkg/layout/css/block.go`** — the `default` case of the formatting-context switch in `layoutInterior` now handles
  ONLY `GridFC` (FlexFC and TableFC have their own cases). It calls `layoutBlockChildren` (block fallback) + a debug log.
  **This is the seam you replace:** add `case cssbox.GridFC: in = e.layoutGrid(...)` next to the existing
  `case cssbox.FlexFC: in = e.layoutFlex(...)` and `case cssbox.TableFC: in = e.layoutTable(...)`. Flexbox and tables are
  the exact precedents. **There is a `TestGridFCFallsBackToBlock` test** (`block_test.go`) asserting the current fallback
  — when grid becomes real, REWRITE that test to assert real grid layout (the flexbox slice did exactly this when it
  replaced the FlexFC fallback; don't just delete the degradation coverage).
- **`pkg/layout/cssbox/box.go`** — `DisplayGrid` and `GridFC` already exist. Box generation already classifies
  `display:grid` → `BoxBlock`/`DisplayGrid`/`GridFC` (`pkg/layout/css/build.go`, the `case "grid":`). So a grid box already
  arrives at the engine with the right `Formatting` — you do not need new box-tree plumbing to *recognize* grid, only to
  *lay it out*. You WILL add grid-item box vocabulary (a `BoxAnonGridItem` kind, mirroring `BoxAnonFlexItem`/
  `BoxAnonTablePart`) + the anonymous-grid-item fixup, and likely `DisplayInlineGrid` for `inline-grid`. **The
  `unused`-linter rule applies: add a struct field/const only when a consumer in the SAME task reads it** (in sub-project
  9 the flexbox box fields had to be added per-consuming-task; in 7 the grid table struct fields likewise).
- **`pkg/layout/css/flexfix.go`** — the **structural template** for `gridfix.go`: it recurses children, then for a flex
  container rewrites `b.Children` (wrap inline runs as anonymous items, blockify inline-level boxes, drop inter-item
  whitespace). The grid fixup is the same shape (CSS Grid §6 box generation). Called from `Build` right after
  `fixupFlex(root)` (which is right after `fixupTables(root)`).
- **`pkg/layout/css/flex.go`** (627 lines — read it as the closest model) — `layoutFlex` shows the full pattern: an axis
  helper, collect items, a **pure track/length resolver carved out for hand-computed unit tests** (`resolveFlexibleLengths`),
  lay out each item's contents via `layoutBlock` into its own BFC capturing natural size, then position via a placement
  helper that sets fragment rects (`placeFlexFragment` → `stretchCellFragment`). Grid's `layoutGrid` mirrors this but in
  2D: a track-sizing pass for columns AND rows (the pure resolver), a placement pass (line/span + auto-placement), then
  item layout + positioning. **Carve the track-sizing algorithm into a pure, dependency-free function** (operating on
  track specs + item min/max-content numbers → resolved track sizes) exactly as flexbox carved §9.7 — it's the highest
  correctness risk and the holistic review will want to adversarially probe it.
- **`pkg/layout/css/table.go`** (`layoutTable`) — the OTHER template: a 2D grid model (`tableGrid`/`buildGrid` with an
  occupancy scan assigning cells to row/column slots honoring colspan/rowspan). Grid's auto-placement + span handling is
  conceptually close to the table occupancy scan — read `buildGrid` for the pattern (do NOT share the code; grid placement
  has its own rules — explicit line numbers, the auto-placement cursor, implicit tracks).
- **`pkg/layout/css/measure.go`** — `measureMinContent`/`measureMaxContent` (a content WIDTH). Reuse for `auto`/
  min-content/max-content track sizing and `minmax()` (same as tables and flexbox). **Known approximation carried from
  flexbox:** for a COLUMN-axis / vertical content size, `measureMaxContent` returns a width, not a height — grid will hit
  the same issue sizing row tracks to content; document it as an approximation (flexbox did, `flex.go` ~line 519).
- **`pkg/css` cascade** — **the gap properties already exist** (`ColumnGap`/`RowGap` on `ComputedStyle`, the `gap`
  shorthand — added for flexbox). But **no grid track/placement properties are parsed** (`grid-template-columns`,
  `grid-template-rows`, `grid-column`, `grid-row`, `grid-auto-flow`, `grid-auto-columns`/`-rows`, `justify-items`,
  `align-items` (exists! shared with flexbox), `justify-self`/`align-self` (align-self exists, justify-self doesn't),
  `justify-content`/`align-content` (justify-content exists), `grid-template-areas`, `grid-area`, the `grid`/`grid-template`
  shorthands — verify each against the cascade). You add the missing ones to `ComputedStyle` + cascade. The track-list
  grammar (`grid-template-columns: 1fr 2fr minmax(100px, 1fr) repeat(3, auto)`) needs a real **track-list parser** — a
  meaningful chunk of `pkg/css` work, more than the flexbox shorthands were. Unknown/unsupported values degrade (skipped),
  as the cascade already does. **`align-items`/`justify-content`/`align-self` are SHARED with flexbox** — grid reads the
  same fields but interprets them per the grid spec; confirm the values grid needs (`start`/`end`/`center`/`stretch`/
  `baseline`, and for content `space-between`/etc.) are parsed (flexbox used `flex-start`/`flex-end`; grid uses
  `start`/`end` — you may need to accept both spellings).
- **`pkg/layout/inline`** (the shared inline core) — **untouched** by flexbox AND tables; grid needs no new inline
  primitive either. Items lay out their contents through the existing path.
- **The `render.Device` seam + the PDF/DOCX pipelines** — untouched. Grid is pure layout above the block FC, producing
  ordinary positioned fragments that paint through the existing fragment/paint path (backgrounds, borders, item contents
  all already paint).

## Architecture fit (keep the layers honest — see CLAUDE.md "Architecture")

Grid lives entirely in the **CSS layout engine** (`pkg/layout/css`) + the **cascade** (`pkg/css`). The `render.Device`
seam, the PDF pipeline, the DOCX pipeline, and the shared inline core are **untouched**. Concretely:
- **`pkg/css`** — add the grid track/placement properties to `ComputedStyle` + cascade + a **track-list parser** + the
  `grid`/`grid-template` shorthands. No layout here. (Gaps already done.)
- **`pkg/layout/cssbox`** — add `BoxAnonGridItem` + (if doing inline-grid) `DisplayInlineGrid`, only when the
  consuming task reads them.
- **`pkg/layout/css`** — the new `grid.go` (`layoutGrid` + the track-sizing algorithm + placement + the pure track
  resolver), an anonymous-grid-item fixup (`gridfix.go`), wired into `normalize`/`Build` and the FC switch in `block.go`.
  Reuse `measure.go` (min/max-content) and `layoutBlock`/`layoutInterior` for item contents.
- **`pkg/layout/css/fragment.go`** — grid items are positioned fragments; confirm the existing flatten/paint path covers
  them (it should — they're block boxes at computed offsets, like flex items and table cells; flexbox needed no
  fragment.go change).

**No new dependencies.** Grid is pure-Go layout math; nothing to add to `go.mod`.

## Testing (this project lives or dies on its test corpus — see CLAUDE.md "Testing")

Every layer gets tests **in the same PR** (new feature ⇒ new fixture + test). Hermetic, no network. Mirror the flexbox
slice's test structure exactly (it worked well):
- **`pkg/css` track-list parse/cascade tests** — `grid-template-columns: 1fr 2fr`, `minmax(100px, 1fr)`,
  `repeat(3, auto)`, `grid-column: 1 / 3`, `grid-column: span 2`, etc. parse to the right `ComputedStyle`. Assert the
  parsed track-list structure directly. **The track-list parser is the biggest new `pkg/css` surface — test it
  thoroughly** (it's more complex than the flexbox shorthands).
- **`pkg/layout/css` grid-geometry unit tests** — the load-bearing layer. Hand-compute expected TRACK sizes and item
  rects for: fixed tracks, percentage tracks, `fr` distribution (the grid analogue of flexbox grow — surplus split by
  `fr`), `minmax()`, `auto`/min-content/max-content tracks (reuse measure.go), `repeat(N)`, line-number placement, span
  placement, auto-placement (sparse), gaps on both axes, and item alignment (`justify-items`/`align-items`/`*-self`,
  incl. `stretch`). Assert fragment rects directly (x/y/w/h) — NOT "an item exists". **Carve the track-sizing algorithm
  into a pure function and unit-test it with hand-computed vectors** (like `resolveFlexibleLengths` — the flexbox holistic
  review specifically valued that the §9.7 resolver was a pure, separately-testable function; the sub-1-fr-sum-style edge
  cases need their own tests).
- **Anonymous-grid-item fixup tests** — inline runs / text inside a grid container become anonymous grid items
  (structural assertions, like the anon-flex-item and anon-table-box tests).
- **Golden images** (`pkg/doctaculous`, `htmlGoldens` + committed PNGs at `testdata/golden/html-<name>.png`) — a few
  eyeball-able grid layouts: a 2×2 fixed grid, an `fr`-distributed grid, a grid with spanning items, an auto-placed grid.
  Generate with `go test ./pkg/doctaculous -run TestHTMLGolden -update`, then **the controller eyeballs every new PNG via
  the Read tool** (the implementer has no image vision — have it STOP after `-update` and hand back the PNG paths). This
  caught real bugs every prior slice — and in flexbox the controller's eyeball confirmed the layout while a golden's
  `align-center` behavior revealed the documented line-cross deferral.
- **WPT-style reftests** (`pkg/doctaculous`, `wptReftests` + `testdata/wpt/css21-normal-flow/NAME.html`/`NAME-ref.html`) —
  grid has GREAT reftest potential: a grid layout == the same boxes placed by hand at the computed positions
  (absolute/inline-block). E.g. a 2×2 fixed grid == four abs boxes at the track offsets; an `fr` grid == divs at the
  distributed widths. Author several — they're the strongest correctness proof for layout math. (Note: the harness reads
  from `testdata/wpt/css21-normal-flow/`, registers via the `wptReftests` slice.)
- **Byte-identical guard.** Grid ADDS a layout mode; **no existing page should change** (every current fixture uses
  block/inline/table/flex, which are unaffected). After each task, run goldens/reftests WITHOUT `-update` and confirm
  `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` shows only NEW files. A changed existing golden
  means grid leaked into another path — fix before proceeding. **Run this as a dedicated checkpoint task** (flexbox Task 10
  did, and it's cheap insurance).
- **Degradation tests.** Each deferral (template-areas ignored, auto-fill→fixed/repeat(1), RTL→LTR, baseline→start,
  dense→sparse, subgrid ignored) degrades gracefully and is covered by a test asserting no panic + the documented fallback
  + the debug log. An empty/degenerate grid container is a zero-size fragment (no panic). **Make sure EVERY deferral has a
  test** — the flexbox holistic review caught that RTL→LTR was logged-but-untested and a test was added; don't repeat that
  gap.

## Process reminders (carried across #1–#9 — these earned their keep, and #9 reaffirmed every one)

- **Sandbox blocks the Go build cache + TLS** — run `go` / `golangci-lint` / `gofmt` (and `gh pr create`, `git push` over
  HTTPS) with `dangerouslyDisableSandbox: true`. A sandboxed `go`/lint command fails with cache/permission/"no go files to
  analyze" errors that are NOT real failures; re-run disabled.
- **Editor diagnostics LAG badly** — after a subagent adds a field/file you'll see stale "undefined"/"unused"/"redeclared"/
  "not in go.mod" errors AND phantom `zz_*`/`*probe*` scratch files that no longer exist on disk. Trust `go build`/`go test`
  and `find . -name 'zz_*'`, not the panel. (Across #9 the panel showed phantom `undefined: flexItemSizing`,
  `undefined: fixupFlex`, `approx redeclared`, and three deleted `zz_probe*` files — all phantom; the real
  `go test`/`golangci-lint` were green every time.)
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific packages
  (`./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...`), not the repo root. **NO `//nolint`**; the repo **declines all
  "modernize" hints** (`max()`/`min()`/`slices.*`/range-over-int/`SplitSeq`) — keep explicit `if x < y { x = y }` clamps,
  indexed `for i := 0; i < n; i++` loops, `sort.SliceStable`. golangci-lint flags `if !(a && b)` (QF1001 — write the De
  Morgan form), bare `x.Close()` (errcheck — `_ = x.Close()`), `S1011` (use `append(dst, src...)` not a copy loop), and
  `ineffassign` (a value assigned then never read — bit the flexbox column `innerMain` until it was made live). The
  `unused` linter IS enforced — a struct field/const you add must be *read* by code in the same PR; defer adding it until
  the consuming task.
- **Verify against the spec + the actual code, don't trust the handover/plan blindly.** Across #9: the plan's Task 4 named
  a non-existent free function `blockLevel` when the real API was the `IsBlockLevel()` METHOD; the plan's test items lacked
  a `FontFamily` so text measured 0-width; the plan assumed `frag.Box` was set unconditionally when it was gated behind
  `establishesStackingContext`; `cssbox.Box` had no per-element label field so the `order` test had to identify items
  positionally. **The implementers caught all of these by reading the actual code.** For grid: **confirm the CSS Grid §11
  track-sizing algorithm steps against the spec (a `WebFetch` of https://www.w3.org/TR/css-grid-1/) before encoding the
  fr-distribution / minmax / content-sizing math** — it has a specific multi-pass structure (resolve intrinsic sizes →
  maximize tracks → expand flexible tracks) that's easy to get subtly wrong, exactly like flexbox §9.7. NOTE: the W3C
  single-page spec is LARGE and `WebFetch`'s summarizer **truncates before reaching the deep algorithm sections** (this
  happened for flexbox §9.7 from three different URLs) — if you can't fetch §11 directly, encode it from known-good
  knowledge and rely on **hand-computed unit tests as the arbiter**, with an explicit instruction: a change that forces
  inverting a passing track-sizing test is a red flag — re-verify the algorithm, don't edit the test.
- **The two-stage review (spec-fidelity + code-quality, per task) + a holistic final review earn their keep.** Across #9
  the per-task reviews caught: a `parseInt`/`parseInteger` duplication, dead `gotBasis`, a stale `itemSizing` comment, a
  stacking-context over-broadening (flagged by BOTH reviewers independently, fixed by decoupling `frag.Box`), and several
  stale comments. **The HOLISTIC review caught a REAL BUG the per-task reviews and the entire 40-test corpus missed:**
  `placeFlexFragment` passed a fixed `(originMain=contentX, originCross=0)`, but the axis helper maps `cross→x` for a
  column, so **column flex items collapsed to x=0 whenever the container wasn't at page x=0** (nested under padding) — every
  golden/reftest/unit test placed the container at x=0, so it was invisible. **Have the holistic reviewer write
  adversarial probes that vary the conditions the unit tests hold fixed** (container position, nesting, both axes, items
  hitting min AND max in one pass, deeply nested grid-in-grid) — and DELETE every probe. **Render real pages at
  milestones** (the controller, via the Read tool) — every visible bug across the project was caught by rendering.
- **Strengthen weak assertions.** Geometry tests must compare actual rects (x/y/w/h), not "an item exists" — a grid bug is
  a wrong *position/size*, which presence checks miss. In #9 a hand-built inline-flex test only checked item existence; the
  reviewer had it assert side-by-side placement. The order test was made to identify items by distinct widths positionally.
- **Prefer the simpler mechanism.** #9 reused `stretchCellFragment` (the table helper) for flex placement, `measure.go` for
  min/max-content, the existing `layoutBlock`/fragment/paint path, and decoupled `frag.Box` rather than over-broadening a
  predicate. For grid: reuse `layoutInterior`/`layoutBlock` for item contents, `measure.go` for track content sizing, the
  table occupancy-scan PATTERN (not code) for placement, the existing fragment/paint path — invent only the track-sizing +
  placement algorithm itself.
- **Scope every commit.** Sub-project A's unrelated commits sat in the working tree the whole time; every flexbox commit was
  explicitly scoped (`git add <specific files>`, never `git add -A`/`git add .`) and `git show --stat` confirmed. Do the
  same — the sub-project A docs may still be dirty.
- **Update CLAUDE.md when the PR lands** — move grid from the §6 remaining-slices list into a new "Done" bullet (the grid FC,
  the properties + track-list parser + shorthands, the algorithm scope, what goldens/reftests cover, and the deferrals —
  template-areas/auto-fill/dense/subgrid/content-alignment/RTL/baseline), and update the §6 done-slices parenthetical +
  add a "Grid fidelity follow-ups" note (mirroring the flexbox/table/positioning follow-up notes). Keep Done/TODO the honest
  source of truth, as #7/#8/#9 did. **List the deferrals the draft Done bullet forgets** — in #9 the line-cross-clamp and
  the column-basis-width-proxy approximations had to be added to the bullet that the plan's draft omitted.

## Open questions to resolve in brainstorm (not blocking the start)

- **Explicit grid first, or template-areas included?** An explicit fixed grid (`grid-template-columns/-rows` + line/span
  placement + auto-placement) is a complete, shippable slice; `grid-template-areas` is a large self-contained add.
  Recommendation: **explicit grid first, template-areas as 10b** — mirrors flexbox landing single-line and tables landing
  the core with RTL deferred.
- **How much of track sizing (§11)?** `fr` + `minmax()` + `auto`/min-content/max-content + `repeat(N)` is the realistic
  core (you have `measure.go`). Defer `repeat(auto-fill/auto-fit)` (needs container-size-driven repeat counts) and the more
  exotic intrinsic sizing edge cases? (Recommendation: land `fr`/`minmax()`/`auto`/`repeat(N)`; defer auto-fill/auto-fit.)
- **Auto-placement: sparse only, or dense too?** Sparse row-flow auto-placement is the default and covers most pages; dense
  packing (`grid-auto-flow: dense`) and column-flow are additive. (Recommendation: sparse row-flow first; defer dense +
  column-flow.)
- **Content-distribution alignment** (`justify-content`/`align-content` distributing tracks within the container) — include
  or defer? Item-level alignment (`justify-items`/`align-items`/`*-self`) is the more important half. (Recommendation:
  item-level first; defer content-distribution.)
- **`inline-grid`** — include (mirrors the `inline-flex` work in #9, which was small: classify + the inline-atom dispatch
  + `isBlockLevelOuter`) or defer to block-level `grid` only first? (Recommendation: include — it's cheap given the
  inline-flex precedent is right there to copy.)
- **`align-items: baseline`** — full cross-baseline participation, or approximate to `start` first? (Recommendation:
  approximate to `start`, like flexbox.)

None block branching and writing the spec; they shape its scope. The recommendation, mirroring flexbox and tables:
**land an explicit fixed grid fully** (container + items + anonymous-item fixup, `grid-template-columns`/`-rows` with
lengths/%/`fr`/`minmax()`/`auto`/`repeat(N)`, line-number + span placement, sparse row-flow auto-placement, item-level
`justify-items`/`align-items`/`*-self` incl. `stretch`, gaps on both axes, item contents through the existing block/inline
path), **with `inline-grid` (cheap, copy inline-flex), and defer `grid-template-areas`, `repeat(auto-fill/auto-fit)`,
dense + column-flow, content-distribution alignment, subgrid, RTL (→LTR), and full baseline (→start)** — each with a
graceful, tested fallback, and an eyeball-verified golden + several reftests. Pin the exact line in the spec.
