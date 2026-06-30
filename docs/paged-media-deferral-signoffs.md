# Paged Media — Deferral Sign-Off Ledger

Every row here is an item the implementation plan
(`docs/superpowers/plans/2026-06-30-paged-media-deferrals.md`) gave the owner the
option to defer rather than implement. A row is VALID only when the **Signed off by**
column names the repository owner (Nathan) AND the **Date** is filled. An item with an
empty sign-off MUST be implemented, not deferred.

The final plan task scans the paged-media code paths for `deferred` / `not honored`
log lines and fails if any lacks a signed row here.

## Deferred (owner-signed)

| # | Deferred item | Why deferred | Signed off by | Date |
|---|---|---|---|---|
| 1 | MID-CELL table content splitting | Tables break between ROWS (a row rides one page); a single row taller than a page overflows whole, and a cell's inline content is not split across pages | Nathan | 2026-06-30 |
| 2 | SINGLE-LINE flex / one-row grid + MID-ITEM splitting | A single-line flex row or one-row grid (items share a band) overflows whole — genuinely indivisible; a flex/grid ITEM's own content is not split across pages | Nathan | 2026-06-30 |
| 3 | Positioned/float distribution WITHIN a named-page (different-width) run | Multi-width named-page reflow lays out + paginates per @page width; out-of-flow (abs/fixed/float) content inside a differently-sized named-page section is not distributed per run (rides the run's layout / may drop). The mainline single-width path retains full positioned/float distribution. | Nathan | 2026-06-30 |
| 4 | element(name) POSITION KEYWORDS + per-page-varying running elements | content: element(name) places the captured running element identically on every page; element(name, first|last|start) and a running element whose content varies per page are not modeled (the single captured fragment repeats). Per-margin-width re-layout also uses the first referencing box's width. | Nathan | 2026-06-30 |

## Resolved — implemented, NOT deferred

These were on the original deferral list but the owner chose to implement them, so they
are no longer deferrals (kept here for traceability).

| Item | Resolution | Decided by | Date |
|---|---|---|---|
| `@page marks` / `bleed` rendering | **Implemented** — crop + cross registration marks drawn in the bleed band; the page bitmap grows to the media box (trim + bleed). See `docs/superpowers/plans/2026-06-30-crop-marks.md`. | Nathan | 2026-06-30 |

## Cosmetic sub-deferrals from implemented features

Minor fidelity gaps within an implemented feature. Listed for honesty; sign off only if
the owner wants them closed.

| Item | Note | Signed off by | Date |
|---|---|---|---|
| Circled-cross registration marks | The `cross` mark is drawn as a plain `+`, not the printer's circle-plus registration target. Cosmetic. | _unsigned — pending owner decision_ | |
