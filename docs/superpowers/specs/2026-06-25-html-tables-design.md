# HTML rendering — CSS 2.1 §17 table layout (`display: table` / rows / cells) (sub-project 7)

**Branch:** `feat/html-tables` (off `feat/html-zindex-6b`, PR #10 — the tip of the HTML stack; rebase
onto `main` if the stack has merged by start). This slice does **not** build on the stacking/clip
machinery the way 6/6b did — tables are a self-contained layout algorithm above the block formatting
context. It reuses the existing block + inline layout for cell *contents*, the existing
border/background fragment paint for separate borders, and adds a small resolved-edge paint path for
collapsed borders.

**Builds on:** the whole normal-flow pipeline (parse → box-gen → block+inline layout → paint), which is
done. Read CLAUDE.md "Architecture" and the §6 TODO bullet for tables. The handover
(`docs/superpowers/HANDOVER-subproject-7-tables.md`) records the verified foundation; this spec is the
agreed scope and design.

## Goal

Turn the **block fallback** that `layoutInterior` currently uses for a `TableFC` box (it logs
"`TableFC not yet implemented; falling back to block normal flow`" and stacks the children as blocks)
into a **real CSS 2.1 §17 table layout**. After this slice, `<table>` markup — and any
`display:table`/`table-row`/`table-cell` content — lays out as a proper table: columns solved to widths,
rows sized to their tallest cell, cells positioned in a grid, borders and backgrounds painted, spanning
cells honored, captions placed. The degradation log for tables stops firing and becomes real output (the
repo's "a TODO becoming supported just turns the skip into real output" pattern).

This is called out in the roadmap as **"the biggest engine addition"**, and the agreed scope here is the
*ambitious* one (full support, not the smaller fixed-only first cut the handover floated):

### In scope (full support)

- **Box tree + anonymous-table-box fixup (CSS 17.2.1):** `display:table` (the table wrapper),
  `table-row-group`/`table-header-group`/`table-footer-group`, `table-row`, `table-cell`,
  `table-column`/`table-column-group`, `table-caption`. Anonymous table / row / cell boxes are inserted
  to repair a malformed tree (a cell not inside a row, stray content inside a table). This is the table
  analogue of the inline-in-block / block-in-inline fixups already in `pkg/layout/css/anon.go`.
- **Column-width solve, both modes:** **fixed** (CSS 17.5.2.1 — widths from the first row's cells /
  `<col>` / table width, ignoring later content) **and auto** (17.5.2.2 — min/max content widths per
  column, distributed; the harder, more common case). Auto is built on a **new min/max-content
  measurement** capability (see below) that does not exist in the engine today.
- **Row heights + cell layout:** each cell is a block container establishing a BFC; lay its content out
  at the resolved column width; row height = the tallest cell; `vertical-align` (`top`/`middle`/`bottom`/
  `baseline`) positions the cell content within the row band.
- **Spanning cells — full `colspan` + `rowspan`,** including **rowspan height distribution** (a spanning
  cell taller than the sum of its rows distributes the excess across those rows). The grid-slot
  assignment honors both spans.
- **Borders + backgrounds — both `border-collapse` models:** `separate` (the initial value — per-cell
  borders + `border-spacing`) **and** `collapse` (the full CSS 17.6.2 border-conflict-resolution
  algorithm + a resolved-edge paint path).
- **`border-spacing`, `table-layout: auto|fixed`.**
- **Captions** (`<caption>` / `display:table-caption`, `caption-side: top|bottom`) — a block above or
  below the table box.
- **`<col>`/`<colgroup>`** width hints + `span`.
- **Percentage column widths** resolved against a fixed table width **and** an auto table width.
- **Nested tables** (a table inside a cell lays out through the same path).

### Deferred (graceful degradation only — no panic, recover at the page boundary, debug log)

- **RTL / `direction`.** There is **no** `direction`/RTL support anywhere in the engine today, and full
  bidirectional text is a separate cross-cutting effort (it would touch the shared inline core, dwarfing
  the table work). `direction` is **parsed but ignored** downstream (LTR column order always), with a
  debug log when a non-`ltr` value is seen on a table. This is the *only* deferral; everything else
  above is in scope.

**Non-goals (unchanged from the roadmap):** web fonts, flexbox/grid (still block-fallback), networking,
pagination, EPUB — all later slices. Tables do not change any of them.

## Architecture / seams (keep the layers honest — see CLAUDE.md "Architecture")

Tables live entirely in the **reflow engine + box model**. The `render.Device` seam and the PDF pipeline
are **untouched**. The shared inline core (`pkg/layout/inline`) is **untouched** — a cell's *contents*
use the existing block + inline layout; the table algorithm sits above it exactly as the block formatting
context sits above the inline one. Concretely, the slice touches:

- **`pkg/layout/cssbox/box.go`** — add the missing `DisplayKind`s (row-group/header/footer-group,
  column/column-group, caption) and a `BoxKind` for anonymous table parts. Carry `colspan`/`rowspan`/
  `<col span>` onto the box (HTML presentational attributes, not CSS).
- **`pkg/css`** — parse + cascade the table properties onto `ComputedStyle`: `border-collapse`,
  `border-spacing`, `table-layout`, `vertical-align` (keyword set), `caption-side`, `direction` (parsed,
  ignored). Mirrors how `overflow` was wired in 5c.
- **`pkg/html`** — fix the UA stylesheet (`<table>` → `display:table`; add the group/column/caption
  rules); read `colspan`/`rowspan`/`<col span>` from the DOM onto the box (like `<img width/height>`).
- **`pkg/layout/css/build.go` `classifyDisplay`** — map the new display values to `{Kind, Display,
  Formatting}`.
- **`pkg/layout/css/anon.go` + `tablefix.go` (new)** — the anonymous-table-box fixup (CSS 17.2.1),
  called from `normalize` after the existing inline/block fixups.
- **`pkg/layout/css/measure.go` (new)** — the min/max-content measurement capability (a measure-mode
  pass through the real block+inline layout). The prerequisite auto column widths consume.
- **`pkg/layout/css/table.go` (new)** — the table layout algorithm: build the grid, solve column widths,
  lay out each cell at its column width via the existing `layoutBlock`, resolve row heights (incl.
  rowspan distribution), position cells, emit the cell/row/table `Fragment`s. The bulk of the work, and
  the seam `layoutInterior`'s new `case cssbox.TableFC:` calls.
- **`pkg/layout/css/tableborder.go` (new)** — `border-collapse: collapse`: the 17.6.2 conflict
  resolution (build a resolved-edge grid) + the resolved-edge paint primitive on the `Fragment`.
- **`pkg/layout/css/block.go`** — `establishesNewBFC` returns true for a table-cell; the
  `layoutInterior` `switch b.Formatting` gains `case cssbox.TableFC: return e.layoutTable(...)` replacing
  the fallback log.
- **`pkg/layout/css/fragment.go` + `pkg/layout/paint`** — collapsed-border edge segments carried on the
  `Fragment` and stroked at paint time (separate borders need **no** new paint primitive — cells paint
  via the existing background/border path).

### The seam where the fallback is replaced (`block.go` `layoutInterior`)

Today (`block.go` ~479):

```go
switch b.Formatting {
case cssbox.InlineFC:   ...
case cssbox.BlockFC:    in = e.layoutBlockChildren(...)
default: // TableFC / FlexFC / GridFC
    e.logf("css layout: %v not yet implemented; falling back to block normal flow", b.Formatting)
    in = e.layoutBlockChildren(...)
}
```

becomes:

```go
switch b.Formatting {
case cssbox.InlineFC:   ...
case cssbox.BlockFC:    in = e.layoutBlockChildren(...)
case cssbox.TableFC:    in = e.layoutTable(ctx, b, contentW, contentX, childBand, childFC, posCtx, posCB)
default: // FlexFC / GridFC still fall back
    e.logf("css layout: %v not yet implemented; falling back to block normal flow", b.Formatting)
    in = e.layoutBlockChildren(...)
}
```

`layoutTable` returns the same `interior` contract every formatting context returns: `{children:
[...table-part fragments...], contentHeight, leadingMargin: 0, trailingMargin: 0}`. A table establishes a
BFC, so it does **not** collapse margins with its in-flow children — leading/trailing margins are 0 (the
table box's own margins, resolved by `layoutBlock` around it, still collapse with its siblings normally).

### Why this stays concurrency-safe

Nothing changes the existing invariant: the fragment tree is read-only after layout and flattened across
the render fan-out without locks. `layoutTable` runs entirely within one `Layout` goroutine, allocates a
private `tableGrid` that never escapes the call (like `floatContext`/`positionedContext`), and produces
ordinary `Fragment`s. The min/max-content memo is per-table-solve, local to the call. The collapsed-edge
list on a `Fragment` is written during layout and only **read** at flatten/paint, exactly like every
other fragment field.

## Box vocabulary (`pkg/layout/cssbox/box.go`)

### New `DisplayKind`s

```go
const (
    DisplayBlock DisplayKind = iota
    DisplayInline
    DisplayInlineBlock
    DisplayListItem
    DisplayTable
    DisplayTableRowGroup       // NEW: tbody / display:table-row-group
    DisplayTableHeaderGroup    // NEW: thead / display:table-header-group
    DisplayTableFooterGroup    // NEW: tfoot / display:table-footer-group
    DisplayTableRow
    DisplayTableColumn         // NEW: col  / display:table-column
    DisplayTableColumnGroup    // NEW: colgroup / display:table-column-group
    DisplayTableCaption        // NEW: caption / display:table-caption
    DisplayTableCell
    DisplayFlex
    DisplayGrid
    DisplayNone
)
```

(Header/footer groups are kept distinct from a plain row-group because §17 orders them — header rows
first, footer rows last — when flattening the grid; see "Grid construction".)

### New `BoxKind`

```go
    BoxAnonTablePart  // NEW: an anonymous table / row-group / row / cell wrapper inserted by the
                      // anonymous-table-box fixup (CSS 17.2.1). Like BoxAnonBlock/BoxAnonInline, it
                      // carries a zero-value ComputedStyle; its Display/Formatting say which table part
                      // it stands in for. isAnonymous() must treat it as anonymous (auto width/height,
                      // no min/max, zero margins/padding/borders).
```

`BoxAnonTablePart` participates in `IsBlockLevel()` (a table part is block-level for the purposes of the
surrounding flow) and is recognized by `isAnonymous` in `block.go` so box-model resolution gives it the
auto/none defaults rather than reading a zero `ComputedStyle` as "width:0".

### Span attributes on the box

`colspan`/`rowspan` are **HTML presentational attributes**, not CSS. They are read in `pkg/html` from the
DOM (exactly as `<img width/height>` already are) and carried onto the cell box; `<col span>` likewise.
They live on the box as plain fields (default 1), NOT on `ComputedStyle`:

```go
type Box struct {
    ...
    // Table cell span (HTML colspan/rowspan attributes; 1 when absent). Honored only for a
    // DisplayTableCell box. <col span> reuses ColSpan on a DisplayTableColumn box.
    ColSpan int
    RowSpan int
}
```

A zero value reads as 1 in the grid builder (so existing non-table boxes, which never set these, behave
as before).

## CSS properties (`pkg/css` — parse + cascade)

All six land on `ComputedStyle` and cascade, mirroring `overflow` (5c). Unrecognized values degrade
gracefully (the property keeps its initial value; unknown values are skipped by the parser as today).

| Property | Values | Initial | Notes |
|---|---|---|---|
| `border-collapse` | `separate` \| `collapse` | `separate` | inherited (CSS 17.6) |
| `border-spacing` | `<length>` \| `<length> <length>` | `0` | inherited; one value = both axes; only used in `separate` |
| `table-layout` | `auto` \| `fixed` | `auto` | on the table box |
| `vertical-align` | `baseline` \| `top` \| `middle` \| `bottom` (+ `sub`/`super`/`text-top`/`text-bottom` parsed, mapped to baseline for now) | `baseline` | only the table-cell keywords are *acted on* this slice; the inline-atom mechanics from 5b are unchanged |
| `caption-side` | `top` \| `bottom` | `top` | inherited (CSS 17.4.1) |
| `direction` | `ltr` \| `rtl` | `ltr` | inherited; **parsed, ignored** downstream (RTL deferred) — a non-`ltr` value on a table is logged |

`border-spacing` parsing: a single length applies to both horizontal and vertical; two lengths are
horizontal then vertical. Stored as `BorderSpacingH`, `BorderSpacingV` (points).

`vertical-align`: this slice acts on the **table-cell** alignment (top/middle/bottom/baseline). The
keyword set is parsed in full so authored values don't error; `sub`/`super`/`text-*`/length values map to
`baseline` for cell purposes (full inline `vertical-align` remains a documented follow-up, per the
roadmap "full `vertical-align` keyword set" item).

## Box generation & the anonymous-table fixup

### 1. UA stylesheet (`pkg/html/ua.go`)

Two gaps fixed. **(a)** `<table>` currently sits in the **block** group (`display:block`) — so an
authored `<table>` does not even establish a table box. Move it out and give it `display:table`. **(b)**
add the group/column/caption defaults. The new UA rules:

```css
table   { display: table; }
thead   { display: table-header-group; }
tbody   { display: table-row-group; }
tfoot   { display: table-footer-group; }
tr      { display: table-row; }        /* already present */
td, th  { display: table-cell; }       /* already present */
col      { display: table-column; }
colgroup { display: table-column-group; }
caption { display: table-caption; }
th      { font-weight: bold; }          /* already present */
```

**Byte-identical caveat (load-bearing):** giving `<table>` `display:table` changes any *existing* fixture
that used a `<table>` element and relied on the current block fallback. **Verified: there are none** — a
`grep -rln "<table" pkg/doctaculous/` returns nothing, so the byte-identical guard holds cleanly. If a
later rebase introduces one, that change is intentional and must be eyeballed.

### 2. `classifyDisplay` (`pkg/layout/css/build.go`)

Extend the `switch` so the new display values map (the existing `table`/`table-row`/`table-cell` arms are
unchanged):

```go
case "table-row-group":    {BoxBlock, DisplayTableRowGroup,    TableFC}
case "table-header-group": {BoxBlock, DisplayTableHeaderGroup, TableFC}
case "table-footer-group": {BoxBlock, DisplayTableFooterGroup, TableFC}
case "table-column":       {BoxBlock, DisplayTableColumn,       TableFC}
case "table-column-group": {BoxBlock, DisplayTableColumnGroup,  TableFC}
case "table-caption":      {BoxBlock, DisplayTableCaption,      BlockFC}  // a caption is a block container
```

Row-groups and columns carry `TableFC` because they are structural table parts the grid builder consumes
(it reads them, then flattens/drops them from flow). A caption is an ordinary `BlockFC` block container
(its content is normal flow). The `default → block` arm is unchanged, so any *unknown* display still
normalizes to block.

### 3. The anonymous-table-box fixup (`pkg/layout/css/tablefix.go`, new)

CSS 17.2.1 ("Anonymous table objects"). Called from `normalize` (`anon.go`) after the existing
inline-in-block / block-in-inline fixups, walking each `DisplayTable` subtree. Rules applied:

1. **Missing-cell-parent:** a `table-cell` whose parent is not a `table-row` → wrap it (and consecutive
   sibling cells) in **one** anonymous `table-row` (`BoxAnonTablePart`, `DisplayTableRow`).
2. **Missing-row-parent:** a `table-row` (or an anonymous row from step 1) whose parent is not a
   table/row-group → wrap consecutive such rows in **one** anonymous `table-row-group`.
3. **Missing-table-parent:** a `table-row`/row-group whose parent is not a table → wrap in an anonymous
   `table` (defends malformed input; rare).
4. **Non-cell content in a row:** content directly inside a `table-row` that is not a `table-cell` (stray
   text, a `<div>`) → wrap consecutive such content in an anonymous `table-cell` (so it still renders).
5. **Non-row content in a table/row-group:** content directly inside a table/row-group that is not a row
   or row-group (and not a caption/column) → wrap in an anonymous row → cell (steps 1+4 compose).
6. **Whitespace stripping:** whitespace-only text between table parts is dropped (not turned into an
   anonymous cell). (The existing whitespace handling in `normalize` runs first; the fixup additionally
   drops inter-part whitespace boxes the table context makes irrelevant.)

The output is a well-formed `table → (caption?) → (column/column-group)* → row-group → row → cell` tree
whatever the input, so `table.go`'s grid builder can assume structure. Columns and column-groups are kept
as children of the table (not wrapped); the grid builder reads their width hints.

**Tests (`tablefix_test.go`):** assert the repaired tree structurally — a bare `display:table-cell` gets
an anon row + anon row-group; stray text in a `<table>` gets an anon row→cell; whitespace between `<tr>`s
is dropped; a `<caption>` stays a direct child. Mirrors `anon_test.go`.

## The grid model (`pkg/layout/css/table.go`)

After fixup, `layoutTable` walks the table subtree and builds a private intermediate grid — the single
source of truth the width solve, height solve, and border resolution all read.

```go
type tableGrid struct {
    table    *cssbox.Box   // the DisplayTable box
    caption  *cssbox.Box   // optional DisplayTableCaption (first one; others ignored + logged)
    rows     []*gridRow    // in VISUAL order: header-group rows, then body rows, then footer-group rows
    cols     []gridCol     // resolved column count; carries <col>/<colgroup> width hints + percentages
    cells    []*gridCell   // each real (non-anonymous-empty) cell once, at its origin slot
    occupied [][]bool      // rows × cols occupancy (spanned slots marked), built during assignment
    collapse bool          // border-collapse == collapse
    fixed    bool          // table-layout == fixed
    spacingH, spacingV float64 // border-spacing (0 in collapse mode)
}

type gridRow struct {
    box    *cssbox.Box     // the DisplayTableRow box (real or anonymous)
    cells  []*gridCell     // cells ORIGINATING in this row (not spanned into it)
    height float64         // solved (phase 3)
    y      float64         // solved top offset within the table content box (phase 4)
}

type gridCol struct {
    widthHint   length      // from <col>/<colgroup> or a fixed-layout first-row cell (auto if none)
    pct         float64     // percentage width if specified (else <0)
    min, max    float64     // content min/max (auto layout, phase 2)
    width       float64     // solved (phase 2)
    x           float64     // solved left offset within the table content box (phase 4)
}

type gridCell struct {
    box              *cssbox.Box
    row, col         int   // origin slot (top-left), 0-based
    rowSpan, colSpan int   // ≥1, clamped to the grid
    frag             *Fragment // laid out (phase 3)
}
```

### Grid construction (slot assignment)

The standard occupied-slot scan (the CSS / HTML table model):

1. Flatten row-groups into `rows` in **visual order**: all `table-header-group` rows first, then
   `table-row-group` (body) rows in document order, then `table-footer-group` rows last. (A stray
   `table-row` directly under the table — after fixup it is under an anonymous group — is a body row.)
2. Resolve `<col>`/`<colgroup>` into provisional `cols` width hints (a `<colgroup span=N>` or `<col
   span=N>` contributes N columns). This gives a lower bound on the column count.
3. Walk rows top-to-bottom. Maintain `occupied[r][c]`. For each row, walk its originating cells
   left-to-right; place each at the first column `c` where `occupied[row][c]` is false; mark the
   `rowSpan × colSpan` rectangle occupied (so a `rowspan` reserves slots in lower rows). Grow the column
   count when a cell's `col + colSpan` exceeds it. `colSpan` clamps to "remaining columns is at least 1";
   `rowSpan` of 0 (HTML "span to end of group") is treated as "to the last row of its group" then clamped
   to existing rows.

The result: a dense occupancy map, the final column count, and each cell's origin `(row, col)` + clamped
spans. **This map is the single source of truth.**

**Tests:** assert column count derived from spans; that a `rowspan=2` cell at `(0,0)` marks `(1,0)`
occupied so the next row's first cell lands at column 1; clamping of an over-wide colspan; header/footer
visual ordering.

## Min/max-content measurement (`pkg/layout/css/measure.go`, new — the prerequisite)

Auto table layout needs, per cell, its **min-content** width (narrowest without overflow = widest
unbreakable unit) and **max-content** width (widest with no wrapping). This capability does **not** exist
in the engine today (verified: inline-block fills its line via `layoutBlock` at the container width; the
only `intrinsic` code is replaced-image pixel sizing). It is built here as a **measure-mode pass through
the real layout**, so measured widths equal laid-out widths and cannot drift:

```go
// measureMaxContent returns the content width box would occupy with no line wrapping
// (the longest line / the sum of its inline run advances), honoring a specified width
// (which pins both min and max). Runs the real inline/block layout in measure mode.
func (e *Engine) measureMaxContent(ctx context.Context, box *cssbox.Box) float64

// measureMinContent returns the narrowest content width box can take without overflow:
// the widest single unbreakable unit (longest word / atomic inline / replaced intrinsic
// width). Computed by laying out at width 0 and taking the widest resulting line.
func (e *Engine) measureMinContent(ctx context.Context, box *cssbox.Box) float64
```

Implementation: a measure flag threaded into the inline path that **reports the intrinsic width and
skips committing fragments**. The inline core already exposes the needed primitives —
`inline.VisibleWidth(glyphs)` sums advances (→ max-content = the width of the single unbroken line);
`inline.Break(glyphs, 0, 0)` (break at zero width) yields one line per unbreakable unit, whose widest is
min-content. For a cell with block children, min/max-content recurse (a child block's contribution is its
own min/max plus the cell's padding/border); a specified `width` on the cell or a child pins that level.
Replaced children contribute their intrinsic (or specified) width.

Results are **memoized per table solve** (`map[*cssbox.Box]struct{min, max float64}`), so a cell is
measured once even when its column and the table width both query it.

**Tests (`measure_test.go`):** min/max-content for known strings against the bundled font metrics (e.g. a
cell containing "Hello world" — max = width of the whole string, min = width of "world" or "Hello"
whichever is wider); a cell with a specified `width` pins both; a cell with a `<br>` reduces max-content
to the longest segment.

## Column-width solve (`pkg/layout/css/table.go`, phase 2)

Selected by `table-layout`.

### Fixed layout (CSS 17.5.2.1)

Content-independent — widths come from the **first row** + `<col>` + the table `width`:

1. Determine the table's used width: a specified table `width` (clamped to the containing block), else
   the containing block content width. (Deterministic: `width:auto` under fixed layout fills the
   containing block. The CSS 17.5.2.1 corner where the column widths sum to *more* than that — the table
   then grows to the column sum — is handled by clamping the used width up to `Σ resolved column widths`
   if larger; the common authored fixed-layout case specifies a width and never hits it.)
2. For each column, the width is the first of: the originating first-row cell's specified `width`
   (÷ colspan, distributed), the `<col>`/`<colgroup>` width, else auto.
3. Percentages resolve against the table used width.
4. Columns with a fixed width take it; the remaining table width (minus `border-spacing` between columns
   and at the edges) is split **equally** among the auto columns (CSS 17.5.2.1). Later rows' content does
   not affect column widths (cells overflow or clip per their own `overflow`).

### Auto layout (CSS 17.5.2.2) — the common case

1. **Per-column min/max:** for each column, `min` = max over its non-spanning cells of
   `measureMinContent(cell)` (+ the cell's horizontal border/padding); `max` = max of
   `measureMaxContent(cell)`. A cell with a specified `width` pins both its min and max to that width
   (clamped to ≥ its own min-content). A `<col width>` raises the column's min/max toward that hint.
2. **Spanning cells:** a colspan cell contributes its min/max to the **set** of columns it crosses. After
   the non-spanning pass, for each spanning cell compute `spanMin`/`spanMax` (its measured min/max minus
   the inter-column `border-spacing` it covers); if `spanMin` exceeds the sum of the spanned columns'
   current mins, distribute the excess across them (proportional to each column's `max−min`, or evenly
   if all equal); same for max. (The standard "distribute span excess to spanned columns" step.)
3. **Table width:**
   - table `width:auto` → preferred = `Σ column max + spacing`; used width =
     `min(preferred, available)` clamped to ≥ `Σ column min + spacing`, where `available` is the
     containing block content width. (If even the mins exceed `available`, the table overflows at
     `Σ min` — graceful, no spin.)
   - specified table width → that value (clamped ≥ `Σ min + spacing`).
4. **Distribute the table content width** (used width − spacing) across columns: start each at its `min`,
   then hand out the surplus `(content − Σ min)` in proportion to each column's `(max − min)` (CSS's
   guideline distribution). If `Σ max ≤ content`, every column gets its `max` and the extra (if the table
   width is larger) distributes proportionally to `max` (or to the column count if all max are 0).
5. **Percentage columns:** a column with a `%` width gets that percentage of the table content width
   first (clamped to ≥ its min); the remaining content width distributes among the non-% columns by
   step 4. Percentages against an **auto** table width resolve after the table width is fixed in step 3
   (using the resolved width) — this is in scope (not deferred).

**Tests:** fixed layout (equal split of leftover; first-row width wins; later content ignored); auto
layout (a column sized by its longest word at min, by its content at max; surplus distribution; a colspan
raising two columns; a `%` column taking its share); table overflow when mins exceed available.

## Row heights, cell layout, vertical-align (`pkg/layout/css/table.go`, phase 3)

1. **Lay out each cell** as a block via `layoutBlock` at its **resolved column width** = sum of the
   spanned columns' widths + the interior `border-spacing` between them (colspan) − the cell's own
   horizontal border/padding (the column width is the cell's *border-box* width). The cell is a **BFC**
   — `establishesNewBFC` must return true for a `DisplayTableCell` box (today it does not; this is a
   confirmed gap added in this slice). The cell's content height comes back on its fragment.
2. **Row natural height** = the tallest **non-spanning** cell originating in the row (its border-box
   height, honoring a specified cell/row `height` as a minimum).
3. **Rowspan height distribution (full support):** for each rowspan cell, compare its border-box height
   to the sum of its spanned rows' natural heights + the `border-spacing` between them. If the cell is
   taller, distribute the **excess** across the spanned rows (proportional to their current heights, or
   evenly if all zero), raising those rows. (Iterate once top-to-bottom; a second pass is unnecessary for
   the common case and avoided for determinism.)
4. **vertical-align** within the final row band, per cell:
   - `top` (and the default for cells without a baseline) → content box at the row band top;
   - `bottom` → content box bottom-aligned to the row band bottom;
   - `middle` → centered in the row band;
   - `baseline` → cells in a row share the row baseline (the max first-baseline among the row's
     baseline-aligned cells); each baseline cell's content shifts so its first baseline sits on the row
     baseline; non-baseline cells fall back to `top`.
   The cell content fragment is shifted down by the computed offset (the cell border box still spans the
   full row height; only its *content* moves).

**Tests:** row height = tallest cell; a specified cell height as a floor; rowspan excess distributed
across two rows; each `vertical-align` keyword shifts the content fragment correctly; a baseline row
aligns two cells of different font sizes on a shared baseline.

## Positioning + fragment emission (`pkg/layout/css/table.go`, phase 4)

With column widths/x-offsets (prefix sums + `border-spacing`) and row heights/y-offsets known:

1. The **caption** (if any) lays out as a block at the table's used width; `caption-side:top` places it
   above the table box (table content shifts down by the caption height), `bottom` below. The caption's
   height is part of the table wrapper's height.
2. Each **cell fragment** is positioned at `(table content X + col.x, table content Y + row.y)` (+ caption
   offset). For a spanning cell, its width/height span the covered columns/rows.
3. **Row fragments** wrap their cells (a row paints its own background behind its cells, per CSS 17.5.1
   the row background is below the cell backgrounds); **row-group** and **column** backgrounds layer
   likewise (table → column-groups → columns → row-groups → rows → cells, bottom to top — the standard
   six table background layers). For the common case (cell backgrounds + table border) this is just the
   existing background paint at each fragment level.
4. The **table fragment** is the table box's border box; its border + background paint via the existing
   path (separate mode) or the resolved-edge path (collapse mode).
5. Return `interior{children: [...caption?, ...row/cell fragments...], contentHeight: total table height,
   leadingMargin: 0, trailingMargin: 0}`. `layoutBlock` (around the table) handles the table box's own
   margins/borders/padding and its position in the surrounding flow normally.

## Borders (`separate` + `collapse`)

### `border-collapse: separate` (initial value)

Each cell paints its own 4-side border + background through the **existing** `Fragment` border/background
path — **no new paint primitive**. `border-spacing` inserts the horizontal/vertical gaps (between cells,
and between the outermost cells and the table border). The table box paints its own border around the
grid. Every cell renders its background and border regardless of content — the `empty-cells` property
(which can hide an empty cell's border/background) is not modeled; we always render as `empty-cells:show`
(the initial value) and log a non-`show` value (see "Degradation").

### `border-collapse: collapse` (`pkg/layout/css/tableborder.go`, new)

`border-spacing` is ignored; adjacent cells share a single border. Two parts:

**1. Conflict resolution (CSS 17.6.2.1).** For each shared grid edge (every vertical segment between two
horizontally-adjacent cells / a cell and the table edge, and every horizontal segment likewise), pick the
winning border by the precedence:
   1. a `border-style: hidden` on either side **suppresses** the edge entirely (wins over everything);
   2. otherwise the **wider** border wins;
   3. ties broken by **style rank**: `double > solid > dashed > dotted > ridge > outset > groove > inset
      > none` (CSS 17.6.2.1 order);
   4. final tie broken by the element **closer to the cell**: cell > row > row-group > column >
      column-group > table.
The output is a **resolved-edge grid**: one `BorderEdge` (width/style/color) per horizontal segment and
per vertical segment of the grid.

**2. Resolved-edge paint.** Collapsed edges paint **centered on the grid line** (half the resolved width
into each adjacent cell). They are carried as a list of edge segments on the **table** `Fragment`:

```go
// CollapsedEdge is one resolved border segment of a border-collapse:collapse table,
// in page space, centered on the grid line. Stroked at paint time (paint.go).
type CollapsedEdge struct {
    X1, Y1, X2, Y2 float64
    Edge           BorderEdge // resolved width/style/color
}
```

`Fragment` gains `Collapsed []CollapsedEdge` (nil for every non-collapse fragment — so the byte-identical
guard holds for all existing content). `paint.go` strokes them after the table/cell backgrounds. Cell
border-box dimensions in collapse mode account for the half-edges so content positions correctly (a
cell's usable content box is inset by half its resolved edges). Individual cells do **not** paint their
own borders in collapse mode (the resolved edges replace them).

**Tests:** conflict resolution unit tests (hidden suppresses; wider wins; style-rank tie; cell-beats-row
tie) asserting the resolved edge for a constructed adjacency; a collapsed-border golden eyeballed.

## Error handling / degradation (the repo's non-negotiable contract)

No panic on malformed input; recover at the page boundary; debug-log every skip/fallback (CLAUDE.md "Go
practices" + "Testing"). Specifically:

- **`direction: rtl`** on a table → ignored (LTR layout), logged once. The only feature deferral.
- **Malformed grid** — impossible spans, a cell with no columns, an empty `<table>`, a row with no cells
  → clamp/skip; an empty table is a zero-size fragment. Never panic.
- **A cell/caption whose content panics** → caught by the existing `Layout` page-boundary `recover`
  (table layout introduces no new unguarded panic source; the grid builder and solves use bounds-checked
  indexing and clamp spans).
- **`empty-cells` non-`show`, nested-table width edge cases beyond the common path** → render the common
  behavior + log. (Nested tables themselves are in scope and lay out; only exotic width-propagation
  corners degrade.)
- **Unknown table-ish display values** already normalize to block via `classifyDisplay`'s `default`.
- **`border-collapse:collapse` on a degenerate table** (single cell, no shared edges) → just that cell's
  own border resolved against the table edge; no special case.

Each deferral/degradation is covered by a test asserting **no panic + the fallback behavior + the debug
log**, exactly as the overflow/positioning slices did.

## Testing (this project lives or dies on its test corpus)

Every layer gets tests **in the same PR**. The byte-identical guard is load-bearing: tables ADD a layout
mode; **no existing non-table page may change.**

### Unit tests (`pkg/css`, `pkg/html`, `pkg/layout/css`)

- **`pkg/css`** — parse/cascade for the six new properties (`border-collapse`, `border-spacing` 1+2
  lengths, `table-layout`, `vertical-align` keywords, `caption-side`, `direction`).
- **`pkg/html`** — the UA rules yield the right `display` for `table`/`thead`/`tbody`/`tfoot`/`tr`/`td`/
  `th`/`col`/`colgroup`/`caption`; `colspan`/`rowspan`/`<col span>` read from the DOM onto the box.
- **`tablefix_test.go`** — anonymous-box fixup: cell-without-row → anon row(+group); stray text → anon
  cell; whitespace dropped; caption stays a direct child. Structural assertions (like `anon_test.go`).
- **`measure_test.go`** — min/max-content for known strings vs font metrics; specified width pins both;
  `<br>` reduces max-content.
- **`table_layout_test.go`** — fragment-geometry assertions (the pattern every layout slice uses, see
  `overflow_layout_test.go` / `clipescape_layout_test.go`):
  - grid construction (column count from spans; rowspan reserves lower-row slots; colspan clamp;
    header/footer visual order);
  - **fixed** column widths (equal leftover split; first-row width wins; later content ignored);
  - **auto** column widths (min by longest word; max by content; surplus distribution; colspan raising
    columns; `%` column share; overflow when mins exceed available);
  - row heights = tallest cell; specified-height floor; **rowspan height distribution**;
  - cell positions (col x-offsets with spacing; row y-offsets); `vertical-align` shifts; `border-spacing`;
  - **`border-collapse:collapse`** conflict resolution (hidden/wider/style-rank/cell-beats-row) and
    resolved-edge geometry;
  - **flag combinations** (every prior slice's worst miss was an untested flag combination): a table cell
    that is a float container; a `<table>` inside an `overflow:hidden` box; `position:relative`/abs
    content inside a cell; a nested table inside a cell; a table that is itself floated.

### Golden images (`pkg/doctaculous`, `htmlGoldens` + committed PNGs)

A few eyeball-able tables, each a distinct slice (the controller eyeballs **every** new PNG via the Read
tool — the implementer has no image vision; generate with `go test ./pkg/doctaculous -run TestHTMLGolden
-update`):

- `html-table-basic` — a 2×3 grid with per-cell borders + alternating backgrounds (separate).
- `html-table-colspan` — a header cell spanning two columns over a 2-column body.
- `html-table-auto` — an auto-layout table whose columns are sized by their content.
- `html-table-collapse` — a `border-collapse:collapse` grid (shared edges, conflict resolution visible).
- `html-table-caption` — a captioned table (`caption-side:top`).

### WPT-style reftests (`pkg/doctaculous`, `wptReftests` + `NAME.html`/`NAME-ref.html`)

The strongest correctness check — pins the **solved geometry**, not just "renders without panic": a table
laid out == the same cells authored as absolutely-positioned/sized blocks at the table's computed
geometry. At least:

- `table-basic` — a fixed-layout 2×2 grid == 4 positioned blocks at the solved cell rects.
- `table-auto-width` — an auto-layout table == positioned blocks at the content-derived column widths.
- `table-colspan` — a colspan grid == positioned blocks honoring the span.

### Regression / corpus guard

After **each task**, run goldens/reftests **without** `-update` and confirm
`git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` shows **only NEW files**. A
change to an existing golden/reftest means table work leaked into block/inline layout — fix before
proceeding. (Verified: no existing fixture uses `<table>`, so the guard should hold cleanly.) Run
`go test -race ./...` (concurrency is core), `go vet ./...`, `golangci-lint run ./pkg/css/...
./pkg/layout/... ./pkg/html/... ./pkg/doctaculous/...`, and `gofmt -l` on the changed packages.

## Notes for the implementer (lessons carried from #1–#6b — these earned their keep)

- **Sandbox blocks the Go build cache + TLS** — run `go` / `golangci-lint` / `gofmt` (and `gh pr create`,
  `git push` over HTTPS) with the sandbox disabled (`dangerouslyDisableSandbox: true`). `origin` is HTTPS.
- **Editor diagnostics LAG badly** — after a subagent adds a field/file you'll see stale
  "undefined"/"unused"/"redeclared" errors and phantom `zz_*` scratch files. Trust `go build`/`go test`
  and `find . -name 'zz_*'`, not the panel. **Every subagent (and reviewer) must delete any `zz_*`
  throwaway before finishing and confirm `git status` is clean** — make cleanup an explicit instruction
  (in 6b, two review subagents each left a `zz_*`; the controller had to sweep them).
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. **NO
  `//nolint`.** The repo **declines all "modernize" hints** (`max()`/`min()`/`slices.*`/range-over-int) —
  keep explicit `if x < y { x = y }` clamps, indexed `for i := 0; i < n; i++` loops, `sort.SliceStable`.
  golangci-lint flags `if !(a && b)` (QF1001 — write the De Morgan form).
- **Verify against the spec, don't trust the handover/this-doc blindly.** 6b's headline lesson: its
  handover's central premise was **wrong per CSS 2.1 §11.1.1**. **A change that forces you to invert an
  existing, passing test is a red flag — stop and verify the spec (ideally `WebFetch` the W3C §17 text)
  before proceeding.** Table layout has many subtleties (anonymous boxes, width distribution,
  border-collapse conflict resolution); confirm the rule before encoding behavior a test will lock in.
- **The two-stage review (spec-fidelity + code-quality, per task) + a holistic final review** earn their
  keep. Have spec reviewers verify the load-bearing geometry **adversarially** (the column-width solve,
  the span grid, the collapse resolution) with throwaway tests, **and delete the throwaways**.
- **Prefer the simpler mechanism.** The grid/width solve has a clean core; resist over-plumbing before
  the invariant is clear (6b shrank from a threaded flag to a one-line bit once the invariant was
  understood).
- **Update CLAUDE.md when the PR lands** — move tables from the §6 TODO into a new "Done" bullet
  (the table box tree + anon fixup, the fixed+auto width solve, cell/row layout + rowspan distribution,
  both border-collapse models, captions, what goldens/reftests cover, and the single RTL deferral), and
  flip the `layoutInterior` `TableFC` fallback note. Keep Done/TODO the honest source of truth.

## Open questions

None blocking. All scope decisions are resolved (recorded above): full support for fixed+auto widths,
full colspan+rowspan incl. height distribution, both border-collapse models, captions, `<col>` hints,
and percentage widths against both fixed and auto table widths; **only RTL/`direction` is deferred**
(parsed-but-ignored, logged). The min/max-content measurement is folded into this slice as the
prerequisite the auto column solve consumes.
