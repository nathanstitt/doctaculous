# Repeated `<thead>` on continuation pages (2026-07-28)

## What shipped

A table split across pages repeats its `<thead>` rows on every continuation, so a
long table keeps its column headings instead of becoming an unlabelled wall of
cells. Measured: a 40-row table over 4 pages went from the header on page 1 only to
the header on all four.

Follow-on to N1c, which made long tables paginate in the first place.

## The header identity has to be recorded early

`buildGrid` flattens `<thead>`, `<tbody>`, and `<tfoot>` into a single `visualRows`
order (header rows first) and then discards the distinction. By the time a fragment
exists, a header cell is **indistinguishable from a body cell** — same
`DisplayTableCell`, no parent link on `cssbox.Box` to walk up. Probing a 40-row
table confirmed it: 41 flat cell children, all `disp=12`.

So the count is captured where it is still known (`g.headerRows`, right where
`visualRows` is assembled) and carried forward as a **Y** rather than a row count:

```
tableGrid.headerRows  →  interior.headerBottom  →  Fragment.HeaderBottom
```

A Y is the right currency because the splitter works in page space and never sees
rows — it recovers bands geometrically. `interior.headerBottom` takes the same
local-to-page-space shift as `collapsedBorders`, immediately beside it, since both
are measured in the interior's content-top-0 frame.

## The re-anchor is the part that is easy to miss

The tail carries a **clone** of the header at its own top. Its `HeaderBottom` is
inherited from the original table by the struct copy, and therefore points at
coordinates the tail no longer has.

That works for the first continuation and then silently stops: when a long table's
tail is split *again*, the stale value fails the `splitY <= HeaderBottom` guard and
no further headers appear. The first implementation had exactly this bug — pages 1
and 2 had the header, pages 3 and 4 did not.

```go
tail.HeaderBottom = tail.Y + headerH
```

Mutation-verified: removing that line fails `TestTableHeaderRepeatsOnEveryPage`,
which is deliberately built with enough rows to force a *third* page.

## Deep clone, not share

The same header appears on every continuation page, and each page shifts its
fragments into a local frame **in place**. Sharing one cell fragment across pages
would have each shift corrupt the others — the same hazard N1b fixed for `BgImage`.

`cloneFloatForPageMap` already does an alias-preserving deep clone; its name is
historical rather than float-specific, so it is reused as-is with a per-cell `seen`
map (header cells share no descendants).

## Making room

A repeated header occupies space the tail did not previously allocate, so the
tail's existing cells are translated down by the header height and the tail's own
height grows to match. Without that the header would paint on top of the first body
row.

## Testing

- **The header appears on every page** of a table long enough to need three or more
  — which is what exercises the re-anchor.
- **A table with no `<thead>` gains nothing** (the no-op path).
- **The header appears exactly once on page 1** — it is repeated onto continuations,
  not re-emitted above itself.
- **A split falling inside the header repeats nothing**, since there is no completed
  header to carry forward.

All three behaviors are mutation-verified.

**One test was initially vacuous and the mutation testing caught it.** The
split-inside-header case was written with an empty child list, so the loop found no
header cells and returned early for a reason unrelated to the guard — it would have
passed against any implementation. It now uses a real header cell and asserts both
directions: no repeat when the split is inside the header, and a repeat when it is
below.

Zero golden churn — no existing fixture had a table long enough to paginate.

## Known limits

- **`<tfoot>` is not repeated.** Footer rows sort to the end of the visual order and
  would need the mirror treatment: cloned onto the *head* of each fragment rather
  than the tail. Less commonly relied upon than a repeated header.
- **Collapsed border strips are still dropped on a split**, so a repeated header in
  a `border-collapse: collapse` table paints without its grid lines. The same
  table-wide limitation both split paths already had.
- **No `break-inside: avoid` interaction.** A header taller than the page is cloned
  regardless, which would loop the content off every page; a header that large is
  pathological, but the case is unguarded.
