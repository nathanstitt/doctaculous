# Mid-block forced breaks — N3 (2026-07-28)

## What shipped

A `break-before` / `break-after` on a nested block that sits at neither its
top-level ancestor's leading nor trailing edge is now **honored**, by splitting that
ancestor at the break position. Previously it was detected, logged once, and
dropped.

Reproduced before fixing: a three-`div` section with `break-before: page` on the
middle one produced **1 page** and the warning; it now produces 2.

## Why this was mostly wiring

N1a did the hard part. The recursive splitter already handles descending a spine,
suppressing edges per level, routing out-of-flow children, and detaching shared
state. N3 needed exactly one new idea:

> **The split Y comes from the author's break, not from the page boundary.**

`splitAtForcedBreak` is a thin wrapper that finds that Y and calls the same
`splitAnyBlockForPage`. Everything else is reused unchanged.

## Two consequences of the split Y being different

**It runs before the overflow logic.** A page-boundary split happens when content
runs out of room; a forced break happens where the author said, which may be well
*above* the boundary. The page is not full — it is simply ending here. So the
forced-break check sits ahead of the `overflow` branch in `bucketBlocks`, and does
not require `len(cur.blocks) > 0`.

**widows/orphans are deliberately not applied.** They express a preference about how
many lines may be stranded at a break. A forced break is not a preference — the
author asked for it — so `splitAtForcedBreak` passes 0/0 rather than the block's
values.

## Finding the break

`midBlockForcedBreakY` walks the in-flow descendants and returns the topmost forced
break that is genuinely interior:

- **Edge breaks are excluded.** A `break-before` on the leading-edge spine, or a
  `break-after` on the trailing-edge spine, is already propagated to the block
  itself by `effectiveBreaks` and handled by the bucketer's existing forced-break
  path. Including them here would make both paths fire on the same break.
  `isLeadingEdgeOf`/`isTrailingEdgeOf` test this by descending the first/last
  in-flow child at each level, mirroring how `effectiveBreaks` finds the same spines.
- **Out-of-flow content is skipped** — a float or positioned box does not force a
  break in the flow.
- **A split at the block's own top or bottom edge is rejected**, since that is not a
  split at all.
- **The first break in document order wins.** Splitting there leaves any later break
  in the tail, which re-enters the bucketer through `queueTail` and is honored on
  the next pass — so N breaks produce N+1 pages without special-casing.

## The warning is gone

`warnMidBlockForcedBreaks` and both of its call sites are deleted. It described a
deferral that no longer exists, and a stale warning is worse than no warning: it
would have told readers the break was ignored while it was in fact being honored.
The `effectiveBreaks` doc comment that referenced it is corrected too.

## Testing

- **The headline case** — a forced break on a nested block splits its ancestor, on a
  page tall enough that nothing overflows, so the split is attributable purely to
  the break.
- **The warning must not fire** after the break is honored — asserted, not assumed.
- **Edge breaks still propagate** rather than splitting, pinning that the two paths
  do not both fire on the same break.

Mutation-verified: disabling the forced-break split fails the headline test. Zero
golden churn — no existing fixture had an interior forced break.

## Known limits

- **A break inside a table cell or flex item is still dropped.** Those need N1c/N1d
  — splitting within one requires a height-budgeted relayout of its formatting
  context, not a fragment partition.
- **`break-before: left`/`right`/`recto`/`verso` are treated as a plain page break.**
  `isForcedBreak` accepts them, but no blank page is inserted to reach the requested
  parity.
- **`break-after` on the deepest node of a spine** resolves to that node's bottom,
  which is correct, but a break-after immediately followed by a break-before on the
  next sibling produces one split rather than two — the right answer, though it
  arrives by the topmost-wins rule rather than by explicit coalescing.
