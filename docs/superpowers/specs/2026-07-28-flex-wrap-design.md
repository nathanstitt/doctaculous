# Multi-line flexbox — `flex-wrap` + `align-content` (2026-07-28)

## What shipped

`flex-wrap: wrap` / `wrap-reverse`, `align-content`, the cross-axis gap between
lines, and the `flex-flow` shorthand. Backlog H1 (*Large*) and H5.

Sequenced deliberately **after** the RTL project, so the per-line placement loop was
written direction-aware once rather than LTR-only and reopened. That was the whole
point of the sequencing decision recorded in backlog A1.

## Shape of the change

One line is now a special case of N, not a separate path. `collectLines` (§9.3)
partitions items into `flexLine` values holding an index range plus the per-line
results that used to be container-wide singletons; `nowrap` yields exactly one line
spanning every item.

`resolveFlexibleLengths` (§9.7) needed **no change at all** — it was already written
and documented as a per-line function, so it is simply called once per line.

### The five things that were single-line

1. **The `flexCrossSize` clamp** (§9.4 step 8) makes a line fill a definite container
   cross size. That is correct *only* when single-line: with several lines the
   leftover is align-content's to distribute, and stretching one line to the whole
   container would swallow the others. Now gated on `len(lines) == 1`.
2. **`contentHeight`** sums the line cross sizes plus the gaps between them for a
   row; for a column the block axis is the main one, so it is the longest line.
3. **The placement loop** gained a per-line cross offset and its own
   `mainPos`/leading/between — justify-content distributes within each line
   independently (§9.5), not across the container.
4. **The `innerMain` overwrite** for an indefinite main size was load-bearing for the
   reverse-placement formula. It is now `lineMain[li]`, per line.
5. **The baseline post-pass** built one group over all items; §9.4 baseline groups
   are per-line.

## `align-content` and the initial-value trap

`ComputedStyle.AlignContent` defaults to `"start"` — grid's convention — but **CSS
Flexbox's initial `align-content` is `stretch`**. Mapping happens at the flex use
site rather than by changing the shared default, which grid relies on. Without it,
multi-line content packs to the cross-start and leaves the container's tail empty.

Distribution reuses `contentOffsets` from `grid.go`, which already normalizes both
the grid `start`/`end` and flex `flex-start`/`flex-end` spellings. Flex's `stretch`
is simpler than grid's `stretchTracks`: the leftover is shared equally into every
line's cross size, with no need to filter on which are growable.

The **cross gap** (`flexCrossGap`) is the mirror of `flexMainGap` on the other axis.
It was previously computed and never read, which was correct for one line — there is
no between-lines gap — and is now consulted only when `len(lines) > 1`.

## `wrap-reverse` composes by XOR

`wrap-reverse` stacks lines from the cross-END. It XORs with the RTL cross flip for
exactly the reason `reverseMain` does on the main axis: an RTL column with
`wrap-reverse` flips the cross axis twice, and two flips cancel.

`axisFor` deals only with flex-direction and direction, so `reverseCrossLines` is set
by `layoutFlex`, which knows the wrap value.

## Pagination came free, and it is tested

`splitFlexGridForPage` is **geometry-driven** — it groups placed child fragments into
overlapping Y bands and never inspects `flex-direction` or `flex-wrap`. A wrapped row
produces one band per line, so it paginates between lines with no change to
`flexgridpage.go` at all.

That is worth a test precisely *because* nothing in the pagination code says "flex
line": a future refactor could lose the property without any local signal.
`TestSplitWrappedFlexBetweenLines` pins it, and the nowrap indivisible case still
holds.

## Testing

Ten unit tests covering line breaking, an over-wide lone item (which must still get
its own line rather than looping), multiple items per line, per-line
justify-content, the cross gap, align-content stretch/center/space-between,
wrap-reverse line order, and pagination.

**Every per-line change is mutation-verified independently**, and they fail
*distinct* test sets — disabling the cross gap fails only the cross-gap test,
dropping the wrap-reverse flip only the wrap-reverse test, skipping align-content
stretch only the stretch test. That is what shows each piece is separately necessary
rather than masked by one broad assertion.

`TestFlexWrapDegradesToNowrap` inverted into `TestFlexWrapBreaksLines`, plus an
explicit `TestFlexNowrapStillOverflows` so a regression that silently re-enables
wrapping for `nowrap` fails loudly.

Also: a `flex-wrap` WPT reftest, and showcase §04 gains wrap+gap and align-content
demos.

**Two showcase fixtures were initially wrong and the render caught it** — I sized the
items for a narrower measure than the page actually has, so four 150px items sat
happily on one ~730px line and the "wrap" demo demonstrated nothing. Widened to 230px
and 420px so they genuinely exceed the measure. The engine was right both times; the
fixtures were not.

## Known limits

- **Column + wrap breaks against the container height.** Now sound, because H4 fixed
  the vertical content measurement this depends on — a column item's hypothetical
  main size is a real content height rather than a max-content width. Before that fix
  this would have broken at arbitrary points.
- **No `align-content: space-evenly` distinction from `space-around`** beyond what
  `justifyOffsets` already implements.
- **Mid-line fragmentation** — a single line taller than the page still moves whole
  rather than splitting its items, unchanged from the nowrap behavior.
