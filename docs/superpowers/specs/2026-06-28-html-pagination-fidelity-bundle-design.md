# HTML rendering — pagination fidelity bundle (post-ship follow-up #2)

**Status:** design. A second, bounded correctness pass over the shipped pagination engine
(`pkg/layout/css/paginate.go`), stacked on `feat/pagination-fidelity`. The first fidelity pass
(`2026-06-28-html-pagination-design.md` §"Fidelity-pass update") fixed the C1–C7 audit backlog. This bundle
takes on **three of the "larger deferrals"** that turn out to be implementable **within the existing post-pass
model** (no engine rewrite, no mid-box fragmentation):

1. **Per-page distribution of page-CB absolute/fixed boxes + `position:fixed` repeating on every page.**
2. **Honoring a forced `break-before`/`break-after` on a NESTED (non-top-level) block** (propagate to the
   nearest top-level ancestor).
3. **Retaining a leading `margin-top` at an UNFORCED (overflow) break** (today collapsed to 0).

It does **not** take on the structural deferrals (mid-box / mid-line / mid-table-row / mid-flex-grid splitting,
widows/orphans, `break-inside`, `@page` margins, running headers/footers) — those genuinely break the post-pass
model and remain future slices.

Architecture is unchanged: still a **post-pass** over the finished fragment tree; `render.Device`, the PDF
pipeline, the DOCX pipeline, and the shared inline core are untouched; no new dependency; the `pageH <= 0`
default path stays byte-identical (every change is gated behind a real `WithPageSize` paginated run).

---

## Item 1 — distribute page-CB abs/fixed; `fixed` repeats

### Today

`splitPositionedByPage` (`paginate.go`) partitions `root.Positioned`:

- A `position:relative` block (in flow → bucketed, but also lifted to the positioned layer) is routed to the
  page its in-flow box landed on — directly (it IS a top-level bucket block) or via its nearest top-level
  ancestor block. **Done; keep.**
- **Every other entry** — `position:absolute`/`fixed` whose containing block is the page (these live in
  `root.Positioned`, not `body.Children`) — falls through to **page 0** at its original page-space Y. A box at
  `top:300px` in a 250pt page rides page 0 and paints off the bottom of page 0; `fixed` does not repeat.

(An abs box whose CB is a *positioned ancestor* is NOT in `root.Positioned` — it is in that ancestor's
`.Positioned`, so it already follows the ancestor. Only **page-CB** entries are the gap here.)

### Target

For each entry in `root.Positioned` that is page-CB out-of-flow (not the relative-block alias case):

- **`position:absolute` (page CB):** route it to the page whose vertical band `[pageTop, pageTop+pageH)`
  contains the box's border-box top (`frag.Y`). Assign it to that bucket's `pagePositioned`, and **shift it
  into that page's local frame** by `-bucket.top` (via `shiftFragment`, which the fidelity pass already
  taught to move every page-space field the fragment owns — children, floats, `ClipRect`, `Collapsed`,
  nested `Positioned`, clip chains). A box whose top is above page 0's top or below the last page's band is
  clamped to the first/last page respectively (so it is never dropped). This mirrors how a relative block is
  routed-and-shifted, except the abs box is NOT a bucket block, so the shift must be applied explicitly here.
- **`position:fixed`:** emit the SAME fragment on **every** page, with **no per-page shift**. A fixed box is
  positioned against the viewport, and `resolveAbsolute` already placed it against the page rect
  `{y:0, h:pageH}` — so its `frag.Y` is its offset from the page top (e.g. `top:10px` → `Y 10`), which is the
  correct **page-local** Y on *every* page. Because the flatten only *reads* a fragment and the fixed entry is
  never mutated (the abs shift below touches a different pointer; the per-page block shift touches bucket
  blocks), the same read-only `*Fragment` is shared across all pages — **no clone needed**. The parallel
  `PositionedInfo` entry is duplicated alongside each reference. (A *bottom*-anchored fixed box is positioned
  against the full single-tall height — a known limitation; per-page bottom anchoring is a follow-up.)

### Why Y-band routing is correct here

A page-CB abs box's `frag.Y` is in page/document space (origin at the document top), the same space the
buckets' `top` values live in. The box belongs on the page whose content band contains its top edge — the
same predicate `bucketBlocks` uses for in-flow blocks (`b.Y` ∈ `[cur.top, cur.top+pageH)`), except an abs box
does not *consume* page height (it is out of flow), so it is assigned to a page rather than driving the break.
We assign by the box's top, not its full extent: an abs box taller than a page (or straddling a boundary)
rides the page its top falls on and overflows downward, clipped by that page's bitmap — the same graceful
mid-box degradation in-flow over-tall blocks get. (Per-page *splitting* of an out-of-flow box is out of scope,
same as for in-flow boxes.)

### No cloning needed

Neither path clones a fragment — both rely on the flatten being a pure read:

- **abs (one page):** assigned to exactly one page and shifted once, in place. It is out of flow (not aliased by
  any `Children` entry), so the shift here is the only one it receives.
- **fixed (N pages):** the same read-only `*Fragment` is shared across every page with **no shift** — its
  `frag.Y` is already the per-page-local offset from the viewport top (see above). Because `AppendItems`/`Page`
  only *read* a fragment and nothing mutates the fixed entry (the abs shift touches a different pointer; the
  block shift touches bucket blocks), sharing is safe. This avoids a deep-clone-per-page entirely.

### Edge cases / degradation

- **No paginated abs/fixed:** `root.Positioned` holds only relative aliases (or is empty) ⇒ behavior is exactly
  as today (and the un-paginated path never reaches here). Byte-identical for the existing corpus.
- **abs box with `top:auto` (static position approx):** its `frag.Y` is the static-position approximation
  (containing-block / page top-left ≈ Y 0 today) ⇒ it routes to page 0. Acceptable (the static-position solve
  is itself a documented approximation).
- **abs box above the document / below the last band:** clamped to first / last page (never dropped).
- This does NOT change in-flow bucketing, the relative-block routing, float handling, or the wrapper-border
  fragmentation — all orthogonal.

### Tests

- Re-aimed **`TestPaginatePositionedFirstPageOnly`** → **`TestPaginatePageCBAbsStaysOnItsBandPage`**: a page-CB
  abs box near the top (`top:0`) belongs to page 0's band, so it paints on page 0 exactly once and is NOT
  duplicated. (The OLD test claimed "abs rides page 0 only" as the deferral; this version asserts the *band*
  outcome instead.)
- New **`TestPaginateAbsPageCBDistributesByY`**: an abs box with `top:300px` + three 200pt blocks,
  `WithPageSize(400,250)` ⇒ 3 pages (one block per page, since 2×200 > 250); the abs box paints once on the page
  whose band contains Y=300 (page 1, band `[200,400)`), at local Y = 100. Mutation-verify (revert the abs branch
  ⇒ it lands on page 0).
- New **`TestPaginateFixedRepeatsOnEveryPage`**: a `position:fixed` box at `top:10px` + three 200pt blocks ⇒
  3 pages; the fixed box paints **once per page**, each at local Y ≈ 10. Mutation-verify (route fixed by Y like
  abs ⇒ it paints on page 0 only).
- New **`TestPaginateFixedWithChildRepeatsIdentically`**: a fixed box with a nested child; assert both the box
  and the child paint at their viewport-relative Y on every page (the whole shared subtree repeats identically).

---

## Item 2 — honor a forced break on a nested block (propagate to the top-level ancestor)

### Today

`warnNestedForcedBreaks` scans each top-level block's descendants; if any carries a forced
`break-before`/`break-after`, it **logs once and drops** the break (the bounded pass breaks only between
top-level blocks). `<div><div style="break-before:page">x</div></div>` with a huge page ⇒ 1 page (browser: 2).

### Target

Propagate a nested forced break **up to the nearest top-level ancestor block**, matching how a browser forces
the break on the ancestor when the break point is at the start/end of nested content:

- A forced **`break-before`** anywhere in a top-level block's subtree (the block itself, or a descendant that
  is the **first in-flow content** of the block — i.e. there is no in-flow block-level content before it within
  the top-level block) ⇒ treat the top-level block as having a forced **`break-before`**.
- A forced **`break-after`** anywhere in a top-level block's subtree where the carrier is the **last in-flow
  content** ⇒ treat the top-level block as having a forced **`break-after`**.

`bucketBlocks` reads the break hint per top-level block via `breakBefore(b)`/`breakAfter(b)`. The propagation
plugs in there: compute an **effective** break-before/after per top-level block that also reflects a
"first-descendant forced break-before" / "last-descendant forced break-after". This keeps the bounded model
(still breaks only between top-level blocks) but stops dropping the common authoring pattern
(`.page-break { break-before: page }` on a nested element).

### Scope cut (be honest)

We propagate only the **edge** cases — a forced break-before on content at the **start** of a top-level block,
or break-after on content at the **end**. A forced break-before on content in the **middle** of a top-level
block (which a browser would honor by *splitting* the block) is still **not** honored — that needs mid-box
fragmentation (out of scope). For a mid-block forced break we keep a **one-time log** (the carrier is neither
first nor last in-flow content) so the remaining drop is not silent. This is a strict improvement over today
(start/end nested breaks now work; only true mid-block splits remain deferred, and still logged).

"First/last in-flow content" is determined structurally on the fragment tree: walk the top-level block's
in-flow `Children` (skipping out-of-flow `IsPositioned`/`IsFloat` and anonymous-whitespace fragments); a forced
break-before on the first such child (recursively, its first in-flow child …) is a start break; break-after on
the last is an end break. A break on any other position is mid-block.

### Tests

- New **`TestPaginateNestedForcedBreakBeforePropagates`**: `<div><div style="break-before:page">x</div></div>`
  preceded by a top-level block, huge page ⇒ the second top-level block starts a new page (2 pages). Mutation-
  verify (revert propagation ⇒ 1 page).
- New **`TestPaginateNestedForcedBreakAfterPropagates`**: a top-level block whose **last** descendant has
  `break-after:page`, followed by another top-level block ⇒ the next block is on a new page.
- New **`TestPaginateMidBlockForcedBreakStillWarns`**: a top-level block with two children where the **second**
  (not first) has `break-before:page` ⇒ NOT split (still 1 page across that block) AND a one-time mid-block log
  fires. (Keeps the honest deferral for the genuinely-hard case.)
- Update **`TestPaginateNestedForcedBreakWarns`** / **`TestPaginateTopLevelForcedBreakDoesNotWarn`** as needed:
  the first test's repro is a single nested block that is BOTH the first and last in-flow content of its
  top-level wrapper ⇒ under the new behavior it **propagates** (the wrapper gets a forced break-before) and the
  document splits. Re-aim it to assert propagation (2 pages, no "dropped" log), since "warned-and-dropped" is
  no longer the behavior for an edge break. (The mid-block warn case is covered by the new test above.)

---

## Item 3 — retain a leading margin-top at an unforced break

### Today

`bucketBlocks` sets a fresh page's `top` to the first block's **border-box top** (`cur.top = b.Y`), so the
block lands at local Y 0 — collapsing any leading `margin-top` to 0. Correct at a **forced** break (CSS
truncates margins at a forced break) but at an **unforced/overflow** break CSS **retains** the block's leading
margin as whitespace at the top of the new page.

### Target

Distinguish the two break causes already computed in `bucketBlocks`:

- At a **forced** break (`forcedBefore` true) ⇒ keep `cur.top = b.Y` (margin truncated — unchanged).
- At an **unforced/overflow** break ⇒ set `cur.top = b.Y - usedTopMargin(b)`, where `usedTopMargin` is the
  block's own used top margin (`usedEdges(b.Box, cbWidth).mT`). The block then lands at local Y == its top
  margin, i.e. the leading margin is preserved as whitespace.
- The **very first page** (no preceding break) is unchanged — its `top` is the first block's `b.Y` (the body's
  own padding/border above the first block is handled by `wrapperDecorationTop`, untouched here).

The containing-block width for the used-margin resolution is the body content width — the same basis the block
was laid out against. `bucketBlocks` does not currently receive that width; thread it in (it is
`viewportW` minus the body's horizontal border+padding, available where `paginate` calls `bucketBlocks`), or
recover it from the body fragment. A percentage top margin resolves against it correctly (same as layout).

### Edge cases / degradation

- **Collapsed margins:** the fragment's `b.Y` already reflects margins collapsed against the *preceding* block
  on the previous page. We add back only the block's **own** used top margin, which is the leading whitespace
  CSS shows at the top of the fragmentation container. This is the standard interpretation; a fully precise
  collapse-through model at the break is out of scope (and the simplification is documented).
- **A top margin larger than `pageH`:** clamp the retained margin so `cur.top` does not push the block fully off
  the page (retain at most `pageH - epsilon`? — simpler: retain the margin but the block still overflows
  downward, clipped by the bitmap, exactly like an over-tall block). Keep it simple: no special clamp; document
  that a pathological margin overflows like an over-tall block.
- **Forced break unchanged:** the forced-break unit cases (`forced-before`, `forced-after`, `forced-after with
  a gap`) must stay green — they assert `top == b.Y`, which the forced branch preserves.

### Tests

- New **`TestBucketBlocksRetainsMarginAtUnforcedBreak`** (pure `bucketBlocks`): two blocks, the second with a
  known top margin, sized so the second overflows to page 1; assert page 1's `top == b1.Y - marginTop` (so the
  block lands at local Y == marginTop, not 0). Mutation-verify (revert ⇒ `top == b1.Y`).
- New **`TestBucketBlocksForcedBreakTruncatesMargin`**: the same two blocks but the second has
  `break-before:page`; assert page 1's `top == b1.Y` (margin truncated at the forced break) — guards the
  forced/unforced split.
- An end-to-end `LayoutPaged` assertion that the overflowed block's painted top on its page is ≈ its top
  margin.

---

## Doc reconciliation (part of this PR)

- **CLAUDE.md** "HTML rendering — pagination" Done bullet + "Pagination fidelity follow-ups" paragraph: move
  these three from the deferred list into the done behavior; keep the remaining structural deferrals
  (mid-box/line/row splitting, widows/orphans, `break-inside`, `@page` margins, running headers/footers, and
  the still-deferred **mid-block** forced break).
- **`2026-06-28-html-pagination-design.md`** Deferrals table: rewrite the "Positioned descendants" row
  (abs/fixed now distributed; `fixed` repeats), and add a "Fidelity-pass #2" note for items 2 and 3.
- This spec is the design-of-record for the bundle.

## Verification

Whole repo green / race-clean / lint-0 / gofmt clean. The existing golden/reftest corpus must be
**byte-identical** (no existing call paginates, and the un-paginated path is untouched) — confirm with
`git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` showing no changes. Render an abs/fixed
+ paginated repro and a nested-forced-break repro to `$TMPDIR` and eyeball. Every regression test
mutation-verified (FAILS on the unfixed code).
