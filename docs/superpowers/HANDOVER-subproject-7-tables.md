# Handover — Sub-project 7: CSS table layout (`display: table` / rows / cells)

**Status:** Not started. The HTML renderer's normal-flow pipeline (parse → box-gen → block+inline
layout → paint), floats, positioning, overflow clipping, full z-index stacking, and the 6b clip-escape
sub-cases are all **DONE** (see CLAUDE.md "Done"). The last slice merged is **6b** (PR #10, stacked on
PR #9 `feat/html-zindex`). Tables is the **next** roadmap item (CLAUDE.md §6) and is called out there as
"the biggest engine addition."

**Next action:** Same flow as the prior slices — brainstorm → spec (`docs/superpowers/specs/`) → plan
(`docs/superpowers/plans/`) → subagent-driven execution (per task: implement → spec-review →
code-quality-review → fix) → holistic final review → finish branch / stacked PR. Tables is large enough
that a written spec + plan **are** warranted here (unlike 6b, which was small enough to implement directly).

---

## The PR stack — where to branch from

The whole HTML pipeline ships as a deep chain of stacked PRs, none merged to `main` yet:

```
main ← #2 css-parse-cascade ← #3 box-generation ← #4 block-inline-flow ← #5 replaced-images
     ← #6 floats(5a) ← #7 positioning(5b) ← #8 overflow(5c) ← #9 z-index(6) ← #10 zindex-6b
```

If the stack has merged to `main` by the time you start, branch tables off `main`. Otherwise branch
**off `feat/html-zindex-6b`** (the tip, PR #10). Name it e.g. `feat/html-tables`. Tell every subagent:
you are on `feat/html-tables`, do NOT checkout/stash/switch branches, do NOT commit unless asked.

## Scope (propose this in the spec; adjust in brainstorm)

CSS 2.1 §17 **fixed + auto table layout** for the common case. Target what real documents use:

- **The table box tree + anonymous-table-box fixup (CSS 17.2.1).** `display: table` (the table wrapper),
  `table-row`, `table-cell`, and the row-group/column display types. Browsers insert **anonymous** table,
  row, and cell boxes to repair a malformed tree (e.g. a `table-cell` not inside a `table-row` gets an
  anonymous row; arbitrary content inside a `table` gets wrapped). This is the table analogue of the
  inline-in-block / block-in-inline fixups already in `pkg/layout/css/anon.go`.
- **The column-width solve.** Distribute the table's used width across columns. **Fixed layout** (CSS
  17.5.2.1): widths from the first row's cells / `<col>` / table width, ignoring later content.
  **Auto layout** (17.5.2.2): min/max content widths per column, distributed — the harder, more common
  case. Decide in brainstorm whether to ship fixed-only first (smaller) or both.
- **Row heights + cell layout.** Each cell is a block container (it establishes a BFC); lay its content
  out at the resolved column width, take the row height as the tallest cell, baseline-align or top-align
  per `vertical-align`.
- **Spanning cells** (`colspan` / `rowspan`) — at least parse + honor in the grid (the grid-slot
  assignment is the fiddly part). Decide in brainstorm whether full rowspan height distribution is in or
  deferred.
- **Borders + backgrounds.** `border-collapse: separate` (the initial value) first — per-cell borders +
  `border-spacing`. `border-collapse: collapse` (shared edges, conflict resolution) is a likely **defer**
  (call it out).
- **Captions** (`<caption>` / `display: table-caption`) — likely a small add (a block above/below the
  table box); decide in brainstorm.

**Likely deferrals (state them in the spec's Degradation section, with a graceful fallback each):**
`border-collapse: collapse`, percentage column widths against an auto table width, `table-layout: fixed`
vs `auto` nuances beyond the common case, vertical-align baseline across a row (vs top), nested tables
fidelity, `<col>`/`<colgroup>` width precedence edge cases, and RTL/`direction`. Each deferral must keep
the existing **degrade-gracefully** contract (no panic; recover at the page boundary).

## What already exists (the foundation — grounded in the code, verified for this handover)

The vocabulary and the seam are already in place; tables is mostly a **layout algorithm**, not new plumbing.

- **`pkg/layout/cssbox/box.go`** already defines the display kinds `DisplayTable`, `DisplayTableRow`,
  `DisplayTableCell` and the formatting context `TableFC` (with a comment: "their layout arrives in later
  sub-projects, but the intent is recorded now"). **Gap:** there is **no** `DisplayTableRowGroup` /
  `DisplayTableColumn` / `DisplayTableCaption` yet — you will add the display kinds (and likely a
  `BoxKind` for anonymous table parts, paralleling `BoxAnonBlock`/`BoxAnonInline`) this slice needs.
- **`pkg/layout/css/build.go` `classifyDisplay`** already maps: `display:table`→`{BoxBlock, DisplayTable,
  TableFC}`, `table-row`→`{BoxBlock, DisplayTableRow, TableFC}`, `table-cell`→`{BoxBlock, DisplayTableCell,
  BlockFC}` (a cell is a block container — correct), and `flex`/`grid` to their FCs. **Gaps:**
  `table-row-group`/`table-header-group`/`table-footer-group`/`table-column`/`table-column-group`/
  `table-caption` are NOT mapped (they fall to the `default → block` arm). Extend `classifyDisplay`.
- **`pkg/html/ua.go`** (the UA stylesheet) maps `tr → display:table-row` and `td, th → display:table-cell`
  (and `th { font-weight: bold }`). **Two gaps to fix here:** (1) `<table>` is currently in the **block**
  group (`display:block`), NOT `display:table` — so an authored `<table>` does not even establish a table
  box today; (2) `thead`/`tbody`/`tfoot` → `table-row-group` family, `col`/`colgroup` → `table-column`
  family, and `caption` → `table-caption` are absent. Add these UA rules so real `<table>` markup
  produces the right box tree without the author writing `display:` by hand.
- **`pkg/layout/css/block.go` `layoutInterior`** is where the **block fallback** lives today: the
  `switch b.Formatting` has a `default:` arm (covering `TableFC`/`FlexFC`/`GridFC`) that logs
  "`%v not yet implemented; falling back to block normal flow`" and lays the children out as a block. This
  is the seam tables replaces: add a `case cssbox.TableFC:` that calls a new `layoutTable(...)`. When you
  do, the degradation log for tables stops firing and becomes real table output (the same "TODO becoming
  supported just turns the skip into real output" pattern the repo uses).
- **`pkg/layout/css/anon.go`** holds the existing anonymous-box fixups (`normalize` — inline-in-block
  wrapping, block-in-inline splitting, whitespace handling). The **anonymous table-box fixup** (CSS
  17.2.1) is the natural sibling and likely belongs here (or a new `tablefix.go` it calls). `normalize`
  is called from `build.go` after box generation.
- **`pkg/layout/css/fragment.go`** — the `Fragment` tree + `AppendItems` flatten. A table produces normal
  `Fragment`s (cells are block fragments with backgrounds/borders); **no new paint primitive is expected**
  — cells paint via the existing background/border/content path. If `border-collapse: collapse` is
  attempted (probably deferred), it would need shared-edge handling; separate borders need none.
- **The cell is a BFC.** `establishesNewBFC` should return true for a table-cell (a cell establishes a
  BFC). Check/extend `establishesNewBFC` in `block.go` (today it covers inline-block/float/abs/overflow);
  a table-cell box must isolate its margins/floats like the other BFC establishers.

## Architecture fit (keep the layers honest — see CLAUDE.md "Architecture")

Tables live entirely in the reflow engine + box model — **the `render.Device` seam and the PDF pipeline
are untouched**, and the shared inline core (`pkg/layout/inline`) is untouched (a cell's *contents* use
the existing block+inline layout; the table algorithm sits above it, like the block formatting context
sits above the inline one). Concretely:

- **`pkg/layout/cssbox`** — add the missing `DisplayKind`s (+ any anonymous table `BoxKind`).
- **`pkg/layout/css/build.go` + `anon.go`** — extend `classifyDisplay`; add the anonymous-table-box fixup.
- **`pkg/html/ua.go`** — add the table UA rules (fix `<table>` → `display:table`, add the groups/caption).
- **`pkg/layout/css/table.go` (new)** — the table layout algorithm: build the row/column grid from the
  box tree, solve column widths, lay out each cell at its column width via the existing
  `layoutBlock`/`layoutInterior`, resolve row heights, position cells, and emit the cell/row/table
  `Fragment`s. This is the bulk of the work and the seam `layoutInterior`'s new `TableFC` case calls.
- **`pkg/css`** — the table properties (`border-collapse`, `border-spacing`, `table-layout`,
  `vertical-align`, `colspan`/`rowspan` are HTML attributes not CSS — see note below) need to land on
  `ComputedStyle` if not already there. **Check `pkg/css` first**: `vertical-align` is partially there
  (the atom-baseline mechanics from 5b/replaced); `border-collapse`/`border-spacing`/`table-layout` are
  likely NOT parsed yet — add them to the cascade (parse + longhand if needed), mirroring how `overflow`
  was wired in 5c. `colspan`/`rowspan`/`<col span>` are **HTML presentational attributes**, read in
  `pkg/html` from the DOM (like `<img width/height>` already are) and carried onto the box, not CSS.

## Testing (this project lives or dies on its test corpus — see CLAUDE.md "Testing")

Every layer gets tests **in the same PR** (new feature ⇒ new fixture + test). For tables specifically:

- **Box-gen / anon-fixup unit tests** (`pkg/layout/css`): a malformed table tree (cell without a row,
  stray text in a table) produces the correct anonymous boxes; `<table>`/`tr`/`td` markup yields the right
  `Display`/`Formatting`. Assert the box tree structurally, as the existing `anon_test.go` /
  `build_test.go` do.
- **Layout unit tests** (`pkg/layout/css`, new `table_layout_test.go`): the column-width solve (fixed and
  auto), row heights = tallest cell, cell positions, `colspan`/`rowspan` grid assignment, `border-spacing`.
  Assert fragment geometry directly (the pattern every layout slice uses — see
  `overflow_layout_test.go`, `clipescape_layout_test.go`). Test the **flag combinations** (a table cell
  that is also a float container; a table inside an `overflow:hidden`; a positioned cell content) — every
  prior slice's worst-miss class was an untested flag combination.
- **Golden images** (`pkg/doctaculous`, `htmlGoldens` table + committed PNGs): a few eyeball-able tables —
  a simple 2×3 grid with borders + backgrounds; a table with a `colspan`; an auto-layout table where
  content sizes the columns. Generate with `go test ./pkg/doctaculous -run TestHTMLGolden -update`, then
  **eyeball every new PNG** (the controller, via the Read tool — the implementer has no image vision).
- **WPT-style reftests** (`pkg/doctaculous`, `wptReftests` table + `NAME.html`/`NAME-ref.html`): a table
  laid out == the same cells authored as positioned/sized blocks at the table's computed geometry. This is
  the strongest correctness check (it pins the actual solved geometry, not just "renders without panic").
- **Byte-identical guard.** Tables ADD a new layout mode; **no existing non-table page should change**.
  After each task, `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` must show only
  NEW files (run goldens/reftests WITHOUT `-update`). A change to an existing golden means the table work
  leaked into block layout — fix before proceeding. (The one expected exception: if you add a
  `display:table` UA rule for `<table>`, any *existing* fixture that used a `<table>` element and relied on
  the current block fallback would change — grep the existing `htmlGoldens`/reftests for `<table>` first;
  if any exist, that change is intentional and must be eyeballed, not a regression. As of 6b there are
  none, so the guard should hold cleanly.)
- **Degradation tests.** Each deferral (`border-collapse: collapse`, percentage widths, etc.) must
  degrade gracefully and be covered by a test asserting no panic + the fallback behavior + the debug log,
  exactly as the overflow/positioning slices did.

## Process reminders (carried across #1–#6b — these earned their keep)

- **Sandbox blocks the Go build cache + TLS** — run `go` / `golangci-lint` / `gofmt` (and `gh pr create`,
  `git push` over HTTPS) with the sandbox disabled (`dangerouslyDisableSandbox: true`). This repo's
  `origin` is HTTPS.
- **Editor diagnostics LAG badly** — after a subagent adds a field/file you'll see stale
  "undefined"/"unused"/"redeclared" errors and **phantom `zz_*` scratch files** that no longer exist on
  disk. Trust `go build` / `go test` and `find . -name 'zz_*'`, not the panel. Tell every subagent (and
  reviewer) to delete any `zz_*` throwaway before finishing and confirm `git status` is clean; reviewers
  that probe with throwaway tests must clean them up. (In 6b, two parallel review subagents each left a
  `zz_*` file; the controller had to sweep them — make cleanup an explicit instruction.)
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages (`./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...`), not the repo root. **NO `//nolint`**;
  the repo **declines all "modernize" hints** (`max()`/`min()`/`slices.*`/range-over-int) — keep explicit
  `if x < y { x = y }` clamps, indexed `for i := 0; i < n; i++` loops, and `sort.SliceStable`.
  golangci-lint flags `if !(a && b)` (QF1001 — write the De Morgan form). Run `golangci-lint` per task,
  not just `gofmt`.
- **Verify against the spec, don't trust the handover blindly.** 6b's headline lesson: the 6b handover's
  central premise (an "abs/fixed intervening-clip" that needed clipping) was **wrong per CSS 2.1 §11.1.1**
  — implementing it would have been a regression. The implementer caught it by reading the actual spec
  (W3C text + the predecessor design doc) when a change required inverting a passing test. **A change that
  forces you to invert an existing, passing test is a red flag — stop and verify the spec before
  proceeding.** Table layout (CSS §17) has many such subtleties (anonymous boxes, width distribution,
  border-collapse conflict resolution); confirm the rule, ideally with a `WebFetch` of the W3C spec, before
  encoding behavior a test will lock in.
- **The two-stage review (spec-fidelity + code-quality, per task) + a holistic final review** earn their
  keep. Have spec reviewers verify the load-bearing geometry adversarially (the column-width solve, the
  span grid) with throwaway tests, and delete the throwaways. In 6b, the parallel reviewers independently
  found a real correctness gap (a too-narrow scoping) that the controller then closed with a simpler,
  more-correct fix — the review caught it, not the original implementation.
- **Prefer the simpler mechanism.** 6b's fix shrank from a threaded `directChild` flag to a one-line
  `CBOwned: frag.Clips` once the invariant was understood (a clipping positioned box clips *every* relative
  descendant it consumes). For tables, watch for the same: the grid/width solve has a clean core; resist
  over-plumbing before the invariant is clear.
- **Update CLAUDE.md when the PR lands** — move tables from the §6 TODO into a new "Done" bullet
  (describing the table box tree, the width solve, cell/row layout, what's covered by goldens/reftests,
  and the deferrals), and flip the `layoutInterior` `TableFC` fallback note. Keep the Done/TODO the honest
  source of truth for what is and isn't covered.

## Open questions to resolve in brainstorm (not blocking the start)

- **Fixed-only first, or fixed + auto?** Auto layout (min/max content widths) is the common real-world
  case but the harder algorithm. A fixed-only first PR is smaller and shippable; auto could be a follow-up.
- **rowspan height distribution** — full (distribute a spanning cell's height across the rows it covers)
  or grid-assignment-only first?
- **`border-collapse`** — almost certainly defer `collapse`; ship `separate` (the initial value). Confirm.
- **Captions** — in this slice or a small follow-up?
- **How much `<col>`/`<colgroup>`** — width hints only, or full column-group styling?

None of these block branching and writing the spec; they shape its scope. The recommendation: **ship
`border-collapse: separate`, fixed + auto column widths for the common case, colspan honored in the grid,
rowspan grid-assignment (height distribution deferrable), captions as a small add — and defer
border-collapse, percentage-width-against-auto, and RTL.** Pin the exact line in the spec.
