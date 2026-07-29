# Mid-cell table splitting — N1c (2026-07-28)

## What shipped

A table row taller than the page — including a single-row table, where there is no
row boundary at all — now splits **through its cells** instead of overflowing and
being clipped.

Measured end-to-end: a one-row table holding 300 words went from **1 page with 1200
glyphs and an overflow warning** to **4 pages** carrying 352/384/384/80.

## The estimate was wrong, and worth recording why

The backlog called this *Large* and said it "needs a height-budgeted relayout of a
cell's BFC." That premise does not hold for the common case.

Probing the actual fragment tree showed a cell's content is an **ordinary fragment
spine with `Lines`**, not an opaque nested formatting context:

```
disp=12 (cell) y=0 h=966 lines=0 children=1 splittable=true
  disp=0        y=16 h=934 lines=38 children=0 splittable=true
```

Calling `splitAnyBlockForPage` on the cell directly already worked — 11 lines to the
head, 27 to the tail, **no relayout**. The machinery was in place; `splitTableForPage`
simply never asked.

The lesson generalizes: the earlier claim that the fragment tree "doesn't carry
enough information to resume" has now been wrong twice (once for N1a, once here).
The tree lives in one global page-space coordinate system, which makes re-derivation
cheap by design.

## How it works

`splitTableRowThroughCells` partitions the table's cells three ways: rows entirely
above the straddler go to the head, rows entirely below to the tail, and each cell
**of** the straddling row is split recursively at the page boundary.

**Cells fragment independently.** A cell that cannot break — nothing to break on, or
`break-inside: avoid` — rides the tail whole while its row-mates split. That is the
correct behavior rather than a compromise: CSS fragmentation treats each cell's
content as its own flow, so the tail row simply starts with that cell's full content.

The whole attempt is declined (`ok=false`) only when *no* cell yields both halves, in
which case the caller falls back to the previous behavior: break between whole rows
if a boundary exists, else move whole.

## Between-rows is still preferred

Splitting through cells is the **fallback for a straddling row**, not the primary
strategy. A multi-row table with a usable row boundary still breaks there —
`TestSplitTableBetweenRowsStillPreferred` pins it — because a break at a row boundary
needs no cell fragmentation and produces a cleaner result.

The through-cells path engages exactly when the row at the boundary straddles it,
which includes the previously-hopeless `len(rows) == 1` and `k == 0` cases.

## Testing

- **The headline case** end-to-end through `LayoutPaged`: a single-row table with an
  over-tall cell paginates, every page carries glyphs, and the total glyph count
  confirms nothing was lost.
- **Between-rows still preferred** when a row boundary is available.
- **An unsplittable row still moves whole** rather than being clipped mid-cell.

Mutation-verified: disabling the through-cells path fails the headline test. Zero
golden churn — no existing fixture had an over-tall cell.

## Known limits

- **No repeated `<thead>` on continuation pages.** A table split across pages does
  not repeat its header row, which is what makes long-table pagination genuinely
  readable. Independent of this slice and worth doing next for tables.
- **Collapsed border strips are still dropped on a split** — the collapsed grid is
  computed table-wide and not re-derived per fragment. The same documented limitation
  the between-rows path already had; this slice does not make it worse.
- **A cell whose content is a nested table, flex, or grid container** splits only as
  far as those splitters allow. A flex or grid item taller than the page is still
  indivisible (N1d), so such a cell moves whole.
- **Vertical alignment within a split cell** is not re-solved: a `vertical-align:
  middle` cell's content was centered against the unsplit height.
