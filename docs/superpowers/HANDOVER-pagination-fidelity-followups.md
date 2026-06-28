# Handover — pagination fidelity follow-ups (the "deferred / approximate / documented-as-wrong" backlog)

**Status:** Sub-project 12 (**pagination — fixed-height page fragmentation**) shipped as **PR #16
(`feat/html-pagination`), stacked on `feat/html-openurl` (#15)**. It paginates a document into fixed-height
pages via `WithPageSize(w,h)`, breaking **between top-level in-flow block fragments** and at forced
`break-before`/`break-after: page|always`. After it shipped, a **dedicated audit** (a hard "what did we
quietly get wrong or drop" pass) surfaced a backlog of fidelity gaps — one **real visual bug**, its latent
root cause, and several **under-documented or documented-as-wrong** divergences. This doc scopes them as the
next task.

This is **not** a new feature slice — it is a **correctness + honesty pass** over the shipped pagination
engine. Several items are small (doc fixes, a one-line helper); two are real rendering bugs that need a code
fix + a regression test. Pick them up as one focused PR (stacked on `feat/html-pagination`, or on `main` if
the #2–#16 stack has merged — confirm with `git merge-base --is-ancestor feat/html-pagination main && echo
merged || echo stacked`).

**Spec/code under audit:** `docs/superpowers/specs/2026-06-28-html-pagination-design.md`;
`pkg/layout/css/paginate.go` (the pass), `pkg/css/cascade.go` (the break cascade),
`pkg/doctaculous/html_backend.go` (`WithPageSize`). The CLAUDE.md "HTML rendering — pagination" Done bullet +
"Pagination fidelity follow-ups" paragraph are the current (partly inaccurate — see C1/C4/C5) source of truth.

---

## ⚠️ Process reminders (carried across sub-projects 1–12 — these earned their keep)

- **Branch hygiene:** you are on your follow-up branch. Do NOT checkout/stash/switch branches; do NOT commit
  unless asked; **scope every `git add` to the exact files** (NEVER `git add -A`/`.`). Delete any
  `zz_*`/`*probe*` scratch before finishing (`find . -name 'zz_*' -o -name '*probe*'` empty + `git status`
  clean).
- **Sandbox blocks the Go build cache** — run `go`/`gofmt`/`golangci-lint` with `dangerouslyDisableSandbox:
  true`. A sandboxed command fails with cache/"no go files" errors that are NOT real; re-run disabled.
- **Editor diagnostics are PHANTOM/LAGGING** — after edits the panel shows stale "undefined"/"redeclared"
  errors AND phantom `zz_*`/`*probe*` files (in packages you never touched). Trust
  `go build`/`go test`/`golangci-lint`/`find`, NOT the panel. (In sub-project 12 every review round triggered
  phantom `zz_probe*` panel entries that did not exist on disk; ground truth was green every time.)
- **NO `//nolint`; repo declines "modernize" hints** (`min`/`max`/`slices`/`maps`/range-over-int) — explicit
  loops/clamps, `sort.SliceStable`. `golangci-lint` does NOT run gofmt — run `gofmt -l` separately. The
  `unused` linter is enforced.
- **Mutation-verify every regression test** — a fix-pinning test must FAIL on the unfixed code. Sub-project 12
  caught all three of its review bugs this way; the C1 bug below escaped precisely because its test
  (`TestPaginateRelativeBlockPaginatesNormally`) used a relative block with **no nested positioned child**, so
  it never exercised the broken path.
- **Render real pages + eyeball at milestones** — every visible bug in this project was caught by rendering a
  page, not by reading code. For each fix below, render the repro to `$TMPDIR` and have the controller eyeball
  it.

---

## The backlog (priority order)

### C1 (REAL BUG — fix first) — an abs/fixed descendant of a *paginated* `position:relative` block lands at the wrong Y

**What:** Sub-project 12 routes a top-level `position:relative` block to the page its flow position lands on
(`splitPositionedByPage` in `paginate.go`), and shifts it to local Y 0 via `shiftFragments`. But
`shiftFragment` (`pkg/layout/css/block.go:1178`) recurses `Children` and `Floats` — **not `Positioned`**. So
the relative block's *abs-positioned descendants* (held in the block's own `.Positioned` slice) are left at
their original page-space Y. The abs child paints on the **right page but the wrong position** (offset by the
page-top amount).

**Repro** (`OpenHTMLBytes(html, WithPageSize(400, 250))`):
```html
<div style="height:200px;margin:0">filler</div>
<div style="height:100px;margin:0;position:relative">
  <div style="position:absolute;top:5px;left:5px;width:20px;height:20px;background:#070707"></div>
</div>
```
The relative block lands on page 1 at local Y 0; the black abs child paints at device **y≈428** (≈205pt =
200 filler + 5) instead of the browser-correct **≈5pt**. Fragment dump:
`body.Children[1].Positioned[0].Y == 205` and survives `shiftFragments(..., -200)` unchanged.

**Also a documentation contradiction:** CLAUDE.md/spec currently claim abs descendants of a relative block
"ride the first page at their original Y." They actually **follow the relative block to its real page** (good)
but at the **un-shifted Y** (bad). Fix the behavior, then fix the claim.

**Fix (couples with C2):** make `shiftFragment` also translate `f.Positioned` (recurse), OR in `paginate`
shift a routed relative entry's positioned subtree by `-bucket.top`. The clean fix is C2 (below) — extend
`shiftFragment` to move *every* page-space field it owns. Add a regression test with a **nested abs child**
(the case the existing test omits): assert the abs child's painted Y is ~5pt on its page, mutation-verified.

### C2 (latent root cause of C1) — `shiftFragment` silently omits `Positioned`, `ClipRect`, `Collapsed`, and `PositionedInfo.ClipChain`

**What:** `shiftFragment` (`block.go:1178`) shifts `Y`, `Lines[].BaselineY`, `Image.CY`, `Children`, and
`Floats`. It does **not** touch these page-space fields on a `*Fragment`:
- `Positioned []*Fragment` (`fragment.go:83`) — causes **C1**.
- `ClipRect rect` (`fragment.go:102`) — a paginated `overflow:hidden` relative block's clip rect would be
  wrong. Today this is *masked* because the engine stores a clipping box's own `ClipRect.y` relative to itself
  (0), not page-space-absolute — so it happens to render right. Verify this assumption holds before relying on
  it; if any clip rect is page-space, it breaks.
- `Collapsed []layout.BorderItem` (`fragment.go:90`, page-space) — a `border-collapse:collapse` **table** that
  paginates to a later page would mis-emit its grid lines (not separately reproduced; same root cause —
  reproduce it: a tall collapsed table that overflows to page 2, check the grid lines land on the page).
- `PositionedInfo[].ClipChain []rect` (`fragment.go:128`) — a clip-escape chain captured in page-space.

**Fix:** extend `shiftFragment` to shift every page-space rect it owns (`Positioned` recursively, `ClipRect`,
`Collapsed` strips' Y, each `PositionedInfo.ClipChain` rect). This is the **single correct fix** that
resolves C1 and pre-empts the table/clip variants. Tests: the C1 nested-abs test + a paginated collapsed-table
test + a paginated relative+`overflow:hidden`+abs-descendant test. **Caution:** `shiftFragment` is also used
by the non-pagination layout path (the flat-frame→page-space shift); confirm those callers always pass a
fragment whose `Positioned`/`ClipRect` are *already* in the frame being shifted (they should be — the shift is
applied once, before the positioned/clip data is consumed), so extending it does not double-shift. Read every
`shiftFragment`/`shiftFragments` caller (`block.go`, and confirm `translateFragment` in `inline.go:410` /
`shiftCellContent` in `table.go:850` are separate and unaffected) before changing it.

### C3 — a forced `break-before`/`break-after` on a NESTED (non-top-level) block is silently ignored

**What:** The pass only walks `body.Children`. A `break-before:page` on a div *inside* another div is not a
top-level child, so it is dropped — **no break, no log, no note**. A browser breaks the ancestor too.

**Repro:** `<div><div style="break-before:page">x</div></div>` with a huge page height ⇒ **1 page** (browser:
2). The common authoring pattern `.page-break { break-before: page }` on a nested element is a silent no-op.

**Fix (bounded — this is genuinely deferred, just be HONEST about it):** the bounded scope is "between
top-level blocks only," which is fine — but it must not be *silent*. Cheapest: a one-time `logf` when a forced
break is seen on a non-top-level box (a shallow tree scan during `paginate`, or check `body.Children`'s
descendants). Plus an explicit spec/CLAUDE.md line: "a forced break on a nested (non-top-level-body-child)
block is not honored in this slice." (Honoring it properly = propagating the break up to the nearest top-level
ancestor, a real follow-up.)

### C4 — the `<body>`/`<html>` BORDER paints at full-document geometry on every page (background is benign; border is visibly wrong)

**What:** The per-page wrapper is a **shallow clone** of `root`/`body` (`paginate.go:74-75`) whose `Children`
are swapped but whose own `X/Y/W/H`/`Border`/`Background` are **not** shifted or re-sized per page. So the body
border box is emitted at full-document geometry **identically on every page**.

**Repro** (`<body style="border:10px solid blue">` + three 200pt blocks, `WithPageSize(400,250)` ⇒ 3 pages):
the body border box is `400×620` (full content height) on **all 3 pages** — a spurious top edge at Y=0 on
pages ≥1, side borders spanning the whole document height on every page, the bottom edge off-screen. The body
**background** `400×620` also overdraws every page but is **clipped by the page bitmap** so it looks fine; the
**border** does not.

**Current doc is inaccurate:** spec/CLAUDE.md admit only that the "**background**/border still paints per page
(a documented approximation)" — they do NOT disclose that the **border** is at full-document geometry,
unshifted, so it duplicates/mis-draws.

**Decision needed (controller deferred this — pick one):**
1. **Suppress the body/html border when paginating** (keep the background): on a multi-page document, drop the
   wrapper's border edges. Cheap, removes the artifact; a correctly-fragmented border becomes a follow-up.
2. **Fragment the border properly:** top edge on page 0, bottom on the last page, sides on every page clipped
   to the page band. More correct, real fragmentation work.
3. **Document only:** correct the spec/CLAUDE.md to state the body border paints at full-document geometry on
   every page as a known artifact, and leave it.

(Recommended: option 1 for this pass — it eliminates a visible wrong, is bounded, and defers the real
per-page-border fragmentation honestly.)

### C5 — vertical `margin-top` dropped at an unforced page break; the "logged once" claim is imprecise

**Two small items:**
- **Margin drop at an *overflow* break.** `bucketBlocks` sets `cur.top = b.Y` (border-box top), collapsing any
  `margin-top` gap to 0 at the new page — block lands at local Y 0. At a **forced** break this matches CSS
  (margins truncate at a forced break); at an **unforced/overflow** break CSS *retains* the leading margin, so
  this silently diverges. Reasonable simplification, but **undocumented** — add a spec/CLAUDE.md line, or (more
  work) retain the margin on an overflow break.
- **"Logged once" is inaccurate.** The over-tall-block degradation log (`paginate.go:193`) fires **once per
  over-tall block**, not once total (two over-tall blocks log twice). Spec + CLAUDE.md both say "logged once."
  Bounded (not spam), but correct the wording to "logged per over-tall block."

### C6 (trivial) — stale doc comment on `Fragment.Page`

`pkg/layout/css/fragment.go:549`: "This is the single-tall-page output model; **real pagination is a later
sub-project.**" Pagination has landed; `Page` is exactly what `paginate` calls per bucket. **Fix:** delete the
stale sentence (the rest of the comment is fine).

### C7 (trivial) — test reinvents `strings.Contains`

`pkg/layout/css/paginate_test.go:199` `containsTallerThanPage` hand-rolls a substring scan. **Fix:** replace
the helper's body (or the call site) with `strings.Contains(l, "taller than page")`. (The repo does NOT
decline stdlib `strings` — this is not a "modernize" hint, just a needless loop.)

---

## Items already correctly deferred + graceful (the audit CONFIRMED these — do NOT re-litigate)

These are noted here so the next session does not waste time re-checking what the audit already cleared
(bucket **B** of the audit):

- `break-inside: avoid` / `break-*: avoid` — parsed, not acted on, no crash (the keep-together block still
  splits/overflows). `isForcedBreak` correctly returns false for `avoid`/`avoid-page`.
- `@page` rule — parsed and ignored cleanly; page size comes only from `WithPageSize`.
- `break-before: column`/`region` — treated as non-forced (the graceful default).
- Over-tall block — exactly 1 page, overflows, clipped by the `pageH` bitmap, flattens non-empty (logged, see
  C5).
- Top-level float / abs / fixed box — rides page 0 only, not duplicated onto later pages.
- `WithPageSize` **width side-effect** (sets layout width to `w`, not just height) — **already documented** in
  the option doc comment AND CLAUDE.md; not a gap.
- No blank pages — the `len(cur.blocks)>0` guards hold across all forced-break combinations.
- **z-index across pages is correct** — `splitPositionedByPage` preserves document order per page and each
  page re-runs the stable `(zKey, document-order)` sort; verified with cross-page z-indexed relative blocks.
- `pageH<=0` passthrough, strict-`>` exact-fit, page-`top`-from-first-block, recover-at-boundary — all verified
  correct.

## Larger deferrals (NOT part of this fidelity pass — they are real future slices)

From CLAUDE.md "Pagination fidelity follow-ups" — these are genuine new work, out of scope for a
correctness/honesty pass: **mid-line / mid-table-row / mid-flex-or-grid-item splitting**; **widows/orphans**;
**`break-inside: avoid` enforcement** (keep-together); **per-page distribution of absolute/fixed** content
(today rides page 0; `fixed` does not repeat on every page); **`@page` size/margins/named pages**; **running
headers/footers**. EPUB (sub-project 13) wants none of these to start.

---

## Suggested scope for the follow-up PR

A single focused PR titled something like "pagination fidelity: paginate positioned subtrees + honesty pass":

1. **C2** (extend `shiftFragment` to shift all page-space fields) ⇒ **fixes C1**. Regression tests:
   nested-abs-in-paginated-relative (the C1 repro), paginated collapsed-table grid lines, paginated
   relative+overflow+abs. Mutation-verify each.
2. **C4** per the controller's decision (recommend: suppress body/html border when paginating + a test that no
   spurious border item is emitted on pages ≥1).
3. **C3** (log + document the nested-forced-break drop).
4. **C5 / C6 / C7** (doc + comment + test-helper corrections) — trivial, fold in.
5. **Reconcile CLAUDE.md + the spec** with the *actual* behavior after the fixes (the current C1/C4/C5 claims
   are wrong; correct them).

**Verification:** whole repo green / race-clean / lint-0; render the C1 and C4 repros to `$TMPDIR` and eyeball;
confirm the existing golden corpus is **byte-identical** (none of these fixes should change a non-paginated
page or the existing `html-paginate-p{0,1}` goldens — if a golden changes, eyeball it and justify it).
Architecture unchanged: still a post-pass; `render.Device`/PDF/DOCX/inline core untouched; no new dependency.
