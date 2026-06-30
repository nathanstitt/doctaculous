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
| _none yet_ | | | | |

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
