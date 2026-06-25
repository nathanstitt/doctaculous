# Handover — Sub-project 6b: the deferred clip-escape sub-cases (abs/fixed intervening-clip + positioned-clip-box relative)

**Status:** Not started. Sub-project **6 (full z-index stacking + the *relative* clip-escape)** is DONE on
branch `feat/html-zindex` (**PR #9**, chained on `feat/html-overflow`).
**Next action:** Same flow as #1–#6 — brainstorm → spec (`docs/superpowers/specs/`) → plan
(`docs/superpowers/plans/`) → subagent-driven execution (per task: implement → spec-review →
code-quality-review → fix) → holistic final review → finish branch / PR.

This is a **small** follow-up: it closes the two clip-escape sub-cases sub-project 6 explicitly deferred,
because both need machinery 6 either didn't build (clip-ancestor threading through layout) or scoped out.
The **flatten side is already done** — 6 built `PositionedInfo.ClipChain` + the `appendBand` bracket loop;
6b only needs to **populate** the chain for these two cases. If it turns out trivially small, it could even
fold into a future slice; it is split out so 6 stayed focused and reviewable.

---

## Where we are (the PR stack)

Sub-project 6 shipped on `feat/html-zindex` (off `feat/html-overflow`). If the stack has merged to `main`,
branch 6b off `main`; otherwise branch **6b off `feat/html-zindex`**. Tell every subagent: you are on
`feat/html-zindex-6b` (or whatever you name it), do NOT checkout/stash/switch branches.

## What sub-project 6 delivered (the foundation 6b builds on)

Design: `docs/superpowers/specs/2026-06-25-html-zindex-design.md` (read "Accumulating the clip chain",
"Fold vs. 6b", and the "Degradation" bullets for the two deferred sub-cases). All the relevant code is in
`pkg/layout/css`.

- **`pkg/layout/css/fragment.go`** — `Fragment.Box *cssbox.Box` (z-index source, read at flatten),
  `PositionedInfo{CBOwned bool; ClipChain []rect}` (parallel to `Positioned`), the z-sort
  (`sortedPositioned`/`zKey`/`sortedBands`), and **`appendBand`** — which already brackets an entry's item
  range in its `ClipChain` rects (`ClipPush` outer→inner, then the entry, then `ClipPop`s). **This is the
  flatten machinery 6b reuses unchanged**: populate `ClipChain` and the brackets appear for free.
- **`pkg/layout/css/block.go`** — the relative clip-escape (the case 6 *did* fix): a relative descendant
  bubbling **out of a non-positioned `overflow:hidden` box** grows its chain via `prependRect(frag.ClipRect, …)`
  in `layoutBlock`'s `else if frag.Clips` branch (the `pendingPos{frag, clipChain}` bubbling channel). The
  consume branch (`if establishesStackingContext(b)`) carries each `pp.clipChain` into the holder's
  `PositionedInfo`. `resolveAbsolute` appends abs/fixed entries with `PositionedInfo{CBOwned: …}` and **no
  chain** (the gap 6b closes).

## What sub-project 6b must build

Two deferred sub-cases. Both are "populate a `ClipChain` (or set `CBOwned`) at a collection site so the
existing `appendBand` brackets the entry." Neither touches the sort or the flatten.

### Sub-case A — the abs/fixed intervening-clip escape (the main one)

**The gap:** an `absolute`/`fixed` descendant whose containing block is an ancestor *beyond* an intervening
`overflow:hidden` box. The intervening box's clip should apply (CSS: every `overflow≠visible` ancestor
between a box and its CB clips it), but the abs box paints in its holder's positioned layer **unclipped** by
that box.

**Why 6 couldn't do it:** the deferred record `deferredAbs{box, cb}` (collected at the `layoutBlockChildren`
defer site) captures only the CB owner. **No chain of clipping ancestors is threaded through layout** —
`layoutBlock` → `layoutInterior` → `layoutBlockChildren` thread `posCB` (the CB owner) but not a
clip-ancestor stack. So even *detecting* the case (to log it) needs new threading; 6 left it entirely
untouched.

**Approach to spec:** thread a clip-ancestor stack (`[]rect`, the padding boxes of `overflow≠visible` boxes
on the current layout path) through `layoutBlock`/`layoutInterior`/`layoutBlockChildren`, capture into
`deferredAbs` the sub-chain of clips that sit **strictly between** the abs box and its CB at defer time, and
write that chain into the holder's `PositionedInfo[i].ClipChain` when `resolveAbsolute` appends the resolved
fragment. The common, already-supported case — an abs box whose **CB IS** the clip box — has an EMPTY chain
(the clip is the CB itself, applied by the box's own bracket via `CBOwned`); the chain is non-empty only for
a strictly-intervening clip.

**Adversarial test** the spec reviewer must verify: a `position:absolute` child whose CB is an ancestor
*beyond* an intervening `overflow:hidden` box, with an offset that pushes it past the intervening clip edge
— its items must be bracketed by that clip's `ClipPush`/`ClipPop` (cut at the edge), NOT painted outside.
Contrast: an abs box whose CB *is* the clip box stays on the `CBOwned` path (empty chain), unchanged.

### Sub-case B — the positioned-clip-box relative escape (small, grouped here)

**The gap:** a `position:relative` descendant of a ***positioned*** `overflow:hidden` box. The box is a
stacking context, so the descendant lands on the box's **own** `Positioned` with `CBOwned=false` and paints
in the **escaped band after `ClipPop`**, unclipped. (This is distinct from the relative case 6 fixed, which
is a *non-positioned* clip box where the descendant bubbles *past* the box.) Browsers clip it (the box is
the descendant's CB and clips).

**Why it's its own case:** the descendant never bubbles *out of* the box (the box consumes it), so the
`ClipChain`-grows-on-bubble mechanism never fires. The fix is in the **consume branch** of `layoutBlock`:
when `b` clips AND is the descendant's own containing block, set `CBOwned=true` for that entry (instead of
the blanket `false`) so the box's own `ClipPush`/`ClipPop` bracket clips it — exactly the path an abs box
whose CB is the clip box already takes. The subtlety: the consume branch currently sets `CBOwned:false` for
**all** bubbled relatives because it can't (today) tell which ones have `b` as their CB. A relative box's CB
is its nearest positioned ancestor, which for a *direct* relative child of a positioned clip box IS `b`. Scope
this carefully — a relative grandchild whose nearer positioned ancestor is *between* it and `b` is not `b`'s
CB-owned.

**Adversarial test:** a `position:relative` child of a `position:relative; overflow:hidden` box, offset past
the clip edge — its items must fall inside the box's own clip bracket (cut at the edge), not in the escaped
band after `ClipPop`.

### Also fold in (tiny): the float-internal clip chain

6 logs (`placeFloat`, `(approximate)`) but does not re-translate a `ClipChain` captured inside a float (the
rects are in the float's pre-translation frame; `translateFragment` predates the chain). If 6b touches
`placeFloat`'s chain handling, translate the chain rects by the float's placement delta too. Low priority;
rare nesting.

## Carried-forward deferrals 6b should scope out (NOT 6b's concern unless touched)

From 6's spec "Deferred": the precise static-position solve for an all-`auto` abs box, abs `width:auto`
shrink-to-fit, abs `margin:auto` centering, a `bottom`-only auto-height abs box, `position:relative` on an
inline-level box (needs inline-box fragments), the replaced/inline deferrals (`object-position`,
ratio-preserving min/max, percentage-height basis, `background-image`, full `vertical-align`), and the
bigger slices: **tables**, **web fonts** (`@font-face` + WOFF/WOFF2), **flexbox** then **grid**,
**`OpenURL` + HTTP `ResourceLoader`**, **pagination / paged media**, **EPUB**.

## Process reminders (held across #1–#6 — these earned their keep)

- **Sandbox blocks the Go build cache + TLS** — run `go`/`golangci-lint`/`gofmt` (and `gh pr create`,
  `git push` over HTTPS) with the sandbox disabled. This repo's `origin` is HTTPS.
- **Editor diagnostics LAG badly** — after a subagent adds a field/file you'll see stale
  "undefined"/"unused"/"redeclared" errors and **phantom `zz_*` scratch files** that no longer exist on
  disk. Trust `go build`/`go test` and `find . -name 'zz_*'`, not the panel. Tell every subagent (and
  reviewer) to delete any `zz_*` throwaway before finishing and confirm `git status` clean; reviewers that
  probe with throwaway tests must clean them up.
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages (`./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...`), not the repo root. NO `//nolint`; the
  repo **declines all "modernize" hints** (`max()`/`min()`/`slices.*`/range-over-int) — keep explicit
  `if x < y { x = y }` clamps, indexed loops, and `sort.SliceStable`. golangci-lint flags `if !(a && b)`
  (QF1001 — write the De Morgan form) and an unused unexported field/func — write test conditions
  De-Morgan'd, and **run `golangci-lint` per task, not just `gofmt`**.
- **The byte-identical guard** — the `ClipChain` is empty for every existing page, so the whole corpus must
  stay byte-identical (`git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` empty after
  every task; run goldens/reftests WITHOUT `-update`). 6b's changes only add chains/CBOwned for the two new
  configurations, which no existing fixture exercises — so the guard must still hold. Add a golden/WPT
  reftest for each new clip case (an abs-intervening-clip golden; a positioned-clip-box-relative golden) and
  **eyeball them** (the controller, via the Read tool — the implementer has no image vision).
- **Test the FLAG COMBINATIONS** — an abs box clipped by an intervening box that is ALSO inside a z-indexed
  stacking context; the positioned-clip-box relative case combined with a sibling z-order. Assert paint
  ORDER + clip brackets via `AppendItems` (the item stream). Reuse 5c's `clipBoundsReal`/`bgIndex` and 5b's
  `posBox`/`posStyle` and 6's `zfill` helpers.
- **The two-stage review (spec + code-quality, per task) + a holistic final review** earn their keep — have
  spec reviewers verify the load-bearing clipping adversarially (an abs box past an intervening clip edge
  must be CUT) with throwaway tests, and delete the throwaways.
- **Update CLAUDE.md's Done/TODO** when the PR lands: move the abs/fixed intervening-clip + positioned-clip-
  box sub-cases out of the §6 TODO into the z-index Done bullet (flip "deferred to 6b" to supported).
