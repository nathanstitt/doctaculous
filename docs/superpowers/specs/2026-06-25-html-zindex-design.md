# HTML rendering — full CSS 2.1 Appendix E z-index stacking (+ the relative-clip-escape fix) (sub-project 6)

**Branch:** `feat/html-zindex` (off `feat/html-overflow` — it builds directly on the stacking pass +
clip machinery 5c shipped; rebase onto `main` if the stack has merged).
**Builds on:** 5b positioning (`docs/superpowers/specs/2026-06-24-html-positioning-design.md`) and 5c
overflow (`docs/superpowers/specs/2026-06-24-html-overflow-design.md`). Read 5c's "The clip in the
stacking pass" and "Deferred" before this.

## Goal

Turn the **minimal** stacking pass (positioned boxes paint in document order) into the **full** CSS 2.1
Appendix E z-index ordering **within a stacking context**, and fold in the clipping gap 5c deferred (a
positioned descendant of a *non-positioned* `overflow:hidden` box escapes the clip), because the fix needs
the same cross-layer machinery this slice reworks. The clip-escape fix lands for the **relative** case in
this slice; the rarer **absolute/fixed intervening-clip** sub-case is split to a follow-up 6b because it
needs new layout-threading the relative case does not (see "Fold vs. 6b").

Appendix E paint order within a stacking context:

1. the context root's own background + border;
2. **negative-z-index** child stacking contexts (most-negative first) — painted **behind** in-flow content;
3. in-flow, non-positioned block descendants' backgrounds/borders;
4. floats;
5. in-flow, non-positioned inline content;
6. **z-index:auto / z-index:0** positioned descendants (and non-positioned-but-stacking, e.g. `opacity<1`
   — not modeled), in **document order**;
7. **positive-z-index** child stacking contexts (least-positive first).

Today (5c) the engine collapses steps 2 / 6 / 7 into one document-order `appendPositioned` loop *after*
step 5. This slice splits them: a stable `(z-index, document-order)` sort partitions the positioned layer
into negative / zero-or-auto / positive bands, and the negative band moves **before** the context's
decorations.

**Non-goals (unchanged from the roadmap):** stacking facts other than positioned-box z-index (no
`opacity`, `isolation`, `mix-blend-mode`, `transform` stacking contexts — none are modeled); inline-box
z-index (a no-op — inline-level `position:relative` is already a no-op); `z-index` on a non-positioned
box (no effect, per spec — only a positioned box's z-index participates). Tables, web fonts, flexbox/grid,
networking, pagination remain later slices.

## Architecture / seams

All work lives in `pkg/layout/css` (the seam) plus the existing painter clip primitive (reused, not
extended). The shared inline core (`pkg/layout/inline`) stays **untouched** — z-index is a flatten-time
fact, not an inline-layout one. `pkg/css` is untouched too: `ZIndex int` + `ZIndexAuto bool` already
cascade onto `ComputedStyle` (added in 5b, parsed but not sorted on). This slice is where they bite.

Two concrete seams:

- **`pkg/layout/css/fragment.go`** — the paint-order seam. `AppendItems` is restructured to emit the
  z-sorted bands (a new `sortedPositioned()` + `appendBand` replace the single `appendPositioned` loop);
  `Fragment` gains a `Box *cssbox.Box` pointer (the z-index source) and its `PositionedClip []bool` widens
  to a `PositionedInfo []PositionedInfo` parallel slice carrying both the CB-owned bit and an
  intervening-clip chain.
- **`pkg/layout/css/block.go`** — the collection seam. The 4 sites that append to a holder's `Positioned`
  also set the parallel `PositionedInfo` (CB-owned bit, already present as a bool; plus the new clip
  chain accumulated as a descendant bubbles up through clipping boxes). The `logZIndexUnsupported`
  degradation is removed.

### Why this stays concurrency-safe

The fragment tree is read-only after layout and flattened across the render fan-out without locks. The
new `Box *cssbox.Box` pointer aliases the (logically immutable-after-layout) box into the fragment; the
flatten stage only **reads** `Box.Style`, never mutates it, and flatten begins only after layout has
fully completed. The sort mutates the `Positioned` slice **in place during flatten** — see "The sort
must not race" below for why that is still safe (it is made local to each flatten call).

## Data model (`pkg/layout/css/fragment.go`)

### `Box` pointer — the z-index source

```go
// Box is the source cssbox.Box this fragment was produced from, retained so the
// flatten/paint stage can read style-driven paint facts that are not pre-resolved
// onto the fragment — today the stacking z-index (Box.Style.ZIndex/ZIndexAuto),
// later opacity/isolation and SPA-snapshot re-flow. Set after layout; the flatten
// stage only READS it and never mutates it, so the fragment tree stays safe to share
// across the concurrent render fan-out — which holds only because layout has fully
// completed before any flatten begins (there is no incremental relayout in this
// engine yet). A nil Box reads as the initial style (z-index auto): anonymous /
// synthetic fragments and the page root need not set it.
Box *cssbox.Box
```

`fragment.go` gains a `pkg/layout/cssbox` import. `pkg/layout/css` (the package) already depends on
`cssbox` throughout `block.go`, so this is not a new package edge and not cyclic — only `fragment.go`
joining its own package's existing dependency.

**Where it is set:** at each of the 4 sites that build a positioned fragment, set `frag.Box = b` next to
the existing `frag.IsStackingContext = true` (see "Stamp the box" below). Non-positioned fragments may
leave it nil; only the positioned layer reads it for the sort, and the page root never needs a z-index.

### `PositionedInfo` — per-entry clip metadata (replaces `PositionedClip []bool`)

```go
// PositionedInfo parallels Positioned: per-entry clip metadata telling the stacking
// pass how to clip each positioned descendant painted in THIS holder's positioned
// layer. len(PositionedInfo) == len(Positioned) when set; a nil/short slice reads as
// the zero value (CBOwned=false, no clip chain) — the safe default.
type PositionedInfo struct {
    // CBOwned reports that Positioned[i]'s containing block IS this holder fragment
    // (the old PositionedClip bool). A clipping holder paints a CB-owned entry INSIDE
    // its own clip bracket; a non-CB-owned (bubbled-through) entry paints after
    // ClipPop, outside this holder's own clip.
    CBOwned bool
    // ClipChain holds the padding-box rects of every overflow≠visible box the
    // descendant passed THROUGH between itself and this holder, outermost-first. Empty
    // for the common case. When non-empty, the positioned phase brackets THIS entry's
    // emitted item range in a nested ClipPush(rect)…ClipPop for each rect — so a
    // positioned descendant of a non-positioned overflow:hidden box is cut at that
    // box's padding box even though it paints in an ancestor's layer (CSS: every
    // overflow≠visible ancestor between the box and its CB clips it). The holder's OWN
    // clip (when CBOwned) is applied by the bracket, NOT by this chain.
    ClipChain []rect
}
```

`Fragment.Positioned []*Fragment` is unchanged. `Fragment.PositionedClip []bool` is renamed/retyped to
`Fragment.PositionedInfo []PositionedInfo`. The read path (`sortedPositioned()` zips it into the packed
entries; `appendBand` consults `CBOwned`/`ClipChain`) and the 4 write sites (the collectors) update to
the struct; the parallel-length invariant is unchanged.

## The sort + band split (`pkg/layout/css/fragment.go`)

### Sort key

```go
// zIndex returns f's stacking sort inputs. nil Box => the initial value (auto). auto
// and 0 are NOT merged: both sort into the middle band (step 6) where stable order
// preserves document order; keeping them distinct lets the band partition tell a
// genuine z:0 from auto if ever needed, while sorting both as key 0.
func (f *Fragment) zIndex() (z int, auto bool) {
    if f.Box != nil {
        return f.Box.Style.ZIndex, f.Box.Style.ZIndexAuto
    }
    return 0, true
}

// zKey is the numeric sort key: auto and 0 both map to 0 (the middle band).
func (f *Fragment) zKey() int {
    z, auto := f.zIndex()
    if auto {
        return 0
    }
    return z
}
```

### Pack, stable-sort, partition

To avoid permuting the two parallel slices (`Positioned` + `PositionedInfo`) in lockstep, `sortedPositioned()`
first **packs** each entry into a value pair, then sorts and partitions the packed slice:

```go
// positionedEntry pairs a positioned descendant's fragment with its per-entry clip
// metadata (so the z-sort moves the two together without index bookkeeping).
type positionedEntry struct {
    frag *Fragment
    info PositionedInfo
}

// sortedBands is the positioned layer split into its three Appendix-E bands, each in
// stable (z, document) order. negatives paint before the context decorations (step 2),
// middle after content (step 6, z:auto/0 in document order), positives last (step 7).
type sortedBands struct {
    negatives []positionedEntry // zKey < 0
    middle    []positionedEntry // zKey == 0 (auto + explicit 0)
    positives []positionedEntry // zKey > 0
}
```

`sortedPositioned()` builds one `[]positionedEntry` (zipping `Positioned[i]` with `PositionedInfo[i]`,
reading a missing info as the zero value), **stable-sorts** it by `entry.frag.zKey()` ascending — **use
`sort.SliceStable`** (NOT `slices.SortStableFunc` — the repo declines all `slices.*` modernize hints and
`golangci-lint` flags them) — then slices it at the first `zKey()==0` and first `zKey()>0` boundaries into
the three bands. Building a fresh packed slice each call also resolves the race concern (see "The sort
must not race"): the shared `Positioned`/`PositionedInfo` slices are read, never mutated.

### `AppendItems` restructure

The negatives-before-decorations move means the positioned layer can no longer be a single trailing
call. The stacking/BFC branch becomes (helper names below):

**Non-clipping context** (`!f.Clips`):

```
ord := f.sortedPositioned()          // sortedBands{negatives, middle, positives}
emit ord.negatives                   // step 2 — BEHIND decorations
appendDecorations(f)                 // step 3
for fl := range f.Floats { … }       // step 4 (unchanged: RelOffset applied per float)
appendContent(f)                     // step 5
emit ord.middle                      // step 6 — z:auto/0, document order
emit ord.positives                   // step 7
```

**Clipping context** (`f.Clips`) — the bracket placement interleaves with the bands. A descendant in the
negative/middle/positive bands is split by `CBOwned` exactly as 5c split the single loop: CB-owned
entries paint **inside** the bracket, escaped entries **outside**. Crucially the **negative band still
goes before decorations** — and the CB-owned part of it is inside the clip, the escaped part before the
push:

```
ord := f.sortedPositioned()
appendSelfDecorations(f)                       // f's OWN bg+border — UNCLIPPED
emit ord.negatives where !CBOwned (+ ClipChain) // escaped negatives — UNCLIPPED, behind
emit ClipPush(f.ClipRect)
  emit ord.negatives where CBOwned (+ ClipChain) // CB-owned negatives — CLIPPED, behind children
  appendChildDecorations(f)                      // children bg/border — clipped
  for fl := range f.Floats { … }                 // float layer — clipped
  appendContent(f)                               // in-flow content — clipped
  emit ord.middle    where CBOwned (+ ClipChain)
  emit ord.positives where CBOwned (+ ClipChain)
emit ClipPop
emit ord.middle    where !CBOwned (+ ClipChain) // escaped — UNCLIPPED (their CB is an ancestor)
emit ord.positives where !CBOwned (+ ClipChain)
```

> **Note on negative-band ordering relative to a clip.** Within the clip bracket the CB-owned negatives
> are emitted first (before child decorations) so a CB-owned negative-z descendant paints behind the
> clipping box's own in-flow children — correct for a box whose CB *is* the clip. An *escaped* negative
> (CB is an ancestor) paints before the clip push, behind everything the clip contains, which matches
> "negative child stacking contexts paint behind the parent's in-flow content."

This is more emit points than 5c's two `appendPositioned(…, true/false)` calls, but each is the SAME
operation: emit one band's entries, optionally filtered to CB-owned, applying each entry's `RelOffset` and
`ClipChain`. It is factored into one helper:

```go
// appendBand emits one band's positioned entries (already in stable z/document order)
// to dst. When filterCB is true (the clipping path), only entries with
// info.CBOwned == wantCBOwned are emitted; when false (the non-clipping path), all
// entries are emitted and wantCBOwned is ignored. For each emitted entry it emits the
// entry's ClipChain brackets (outer→inner ClipPush), then the entry fragment's own
// AppendItems (with the relative RelOffset applied over the emitted item range), then
// the matching ClipPops in reverse.
func (f *Fragment) appendBand(dst []layout.Item, band []positionedEntry, filterCB, wantCBOwned bool) []layout.Item
```

So `emit ord.negatives` above is `dst = f.appendBand(dst, ord.negatives, false, false)` on the
non-clipping path, and `emit ord.negatives where CBOwned` is
`dst = f.appendBand(dst, ord.negatives, true, true)` on the clipping path — one helper, the bracket
sequence written once.

### The byte-identical guard (load-bearing)

When **every** positioned box in a context is `z:auto` (the entire existing corpus), `zKey()` is 0 for
all, so the negative and positive bands are empty and the middle band is the whole `Positioned` slice in
its **original document order** (stable sort with all-equal keys = identity permutation). The non-clipping
path then reduces to:

```
(empty) ; decorations ; floats ; content ; middle == Positioned in doc order ; (empty)
```

which is **exactly** today's `appendDecorations → floats → appendContent → appendPositioned` stream. The
clipping path likewise reduces to 5c's two-call split (middle CB-owned inside the bracket, middle escaped
after ClipPop), with empty negative/positive bands and empty ClipChains. **Every existing golden/reftest
must stay byte-identical** — verified by running the full corpus without `-update` and confirming no
diff. This is the regression guard for the whole slice.

### The sort must not race

`AppendItems` runs on a fragment tree shared across the render fan-out (the page is rendered once today,
but the contract is concurrent-safe flatten). Sorting `f.Positioned` **in place** would mutate shared
state during flatten. The packing step above is exactly what avoids this: `sortedPositioned()` builds a
**fresh** `[]positionedEntry` per call and sorts *that*, reading `f.Positioned`/`f.PositionedInfo` but
never mutating them — so the shared fragment tree stays read-only and flatten stays race-free. The cost
is one small slice allocation per stacking context per flatten (positioned-descendant counts are small;
the existing flatten already appends freely). The rejected alternative — sorting once at layout-end in
`block.go` before any flatten — would avoid the per-flatten allocation but spread the ordering concern
across two files and mutate the tree at layout-end; the handoff's guidance is "re-point the collection,
not rewrite the pass," i.e. keep ordering in `fragment.go`. If profiling ever shows the per-flatten pack
hurts, the layout-end sort is the fallback — not pre-optimized here.

## Stamp the box + the CB-owned/clip-chain bit (`pkg/layout/css/block.go`)

### Stamp `frag.Box`

At the 4 sites that build a positioned fragment, set `frag.Box = b` (the source box) next to the existing
`frag.IsStackingContext = true`:

1. **root / `layoutTree`** — the ICB root is not itself positioned; its `Box` may stay nil (the root's
   own z-index never participates). Its `Positioned` entries get their `Box` from their own build site.
2. **`layoutBlock`** (a positioned block consuming its interior's pendings, and being collected by its
   parent) — `frag.Box = b` where the positioned frag is built.
3. **`placeFloat`** (a positioned float) — `frag.Box = child`.
4. **`resolveAbsolute`** (abs/fixed) — `frag.Box = d.box`.

Because the existing tests build boxes via `posBox(style, …)` and run real layout, the stamped `Box`
carries the test's `ComputedStyle` (including any z-index set on it) through to the fragment — no
test-helper change is needed to *set* z-index; a test sets `style.ZIndex`/`style.ZIndexAuto` on the box.

### Set `PositionedInfo` at each collector

Each of the 4 append sites currently appends a `PositionedClip` bool parallel to `Positioned`. It now
appends a `PositionedInfo{CBOwned: <the old bool>, ClipChain: <accumulated chain>}`:

- The **`CBOwned`** bit is the existing computation unchanged (e.g. `resolveAbsolute`'s
  `!d.cb.isPage && d.cb.frag != nil && owner == d.cb.frag`; the in-flow/float collectors append
  `CBOwned:false` as they appended `false` today).
- The **`ClipChain`** is the new datum: the padding-box rects of clipping boxes the descendant passed
  through on its way to this holder.

### Accumulating the clip chain (the relative-clip-escape fix)

The chain is collected along the **bubbling** path — the `pendingPositioned` plumbing for relatives and
`posCtx.deferred` for abs/fixed.

- **Relative descendant** (bubbles via `interior.pendingPositioned` → `layoutBlockChildren` →
  `layoutBlock`): when `layoutBlock` returns for a box `b` that **clips** (`clips(b)`) AND does **not**
  establish a stacking context (`!establishesStackingContext(b)`, i.e. `b` is non-positioned), any
  `pendingPositioned` entries its interior produced are, by definition, bubbling *past* `b` (a stacking-
  context `b` would have *consumed* them onto its own `Positioned` in `layoutBlock`, so they would never
  bubble — this is exactly why the gap exists only for a **non-positioned** clip box). Prepend `b`'s
  padding-box rect to each such still-bubbling entry's chain before returning it up. Concretely: the entry
  carries its chain as it bubbles; at each hop *out of* a non-positioned clipping box, the chain grows by
  that box's padding box (outermost box ends up first since each prepend happens as the entry rises).

  The cleanest plumbing (decided): carry the chain **on the bubbling fragment itself** is wrong (the
  fragment is shared output). Instead carry it on the *pending entry* — extend the `pendingPositioned`
  element from a bare `*Fragment` to a small `pendingPos{frag *Fragment, clipChain []rect}` (an internal
  layout type, not a `Fragment` field), so the chain accumulates without touching the fragment tree until
  the entry lands on a holder, at which point the chain is written into that holder's
  `PositionedInfo[i].ClipChain`. (`pendingPositioned []*Fragment` → `pendingPositioned []pendingPos` in
  `blockResult`, `interior`, and `layoutBlockChildren`'s locals.)

- **Absolute/fixed descendant** (deferred to `posCtx.deferred`, resolved in `resolveAbsolute`): a clip
  box between an abs box and its CB is an **ancestor clip that still applies** (CSS). This case needs the
  chain of clipping ancestors between the abs box and its CB, written into the holder's
  `PositionedInfo[i].ClipChain` when `resolveAbsolute` appends the resolved fragment.

  > **Scope note (abs path) — confirmed more invasive.** The deferred record (`deferredAbs{box, cb}` at
  > the `layoutBlockChildren` defer site, `block.go:486`) currently captures only the CB owner; **no list
  > of intervening clipping ancestors is threaded through layout today** (`layoutBlock` →
  > `layoutInterior` → `layoutBlockChildren` thread `posCB`, the CB owner, but not a clip-ancestor
  > stack). So the abs-path chain requires a NEW parameter (a `[]rect` clip-ancestor stack, or the
  > intervening clip boxes) threaded through those three functions and captured into `deferredAbs` at
  > defer time — strictly more work than the relative path, where the chain rides the **already-existing**
  > `pendingPositioned` bubbling. The common, already-supported 5c case — an abs box whose **CB is** the
  > clip box — has an EMPTY chain anyway (the clip is the CB itself, applied by the bracket via
  > `CBOwned`); the chain is non-empty only when a clip box sits **strictly between** the abs box and a
  > higher CB, which is the rare case. **Decision (see "Fold vs. 6b"): the abs intervening-clip sub-case
  > is deferred to 6b** because it needs new layout threading, while the relative case (the one the
  > handoff names and the adversarial test targets) stays in 6.

### Remove the degradation

Delete `logZIndexUnsupported` and its two call sites (`block.go:574`, `block.go:724`). Delete the test
that asserts the log fires (`TestZIndexParsedButNotSortedLogs` + its `containsZIndex` helper in
`positioning_layout_test.go`) — z-index is now honored, so an absence-of-log assertion would be wrong;
replace it with the ordering assertions below.

## Fold vs. 6b — the scope decision

**Decision: fold the RELATIVE clip-escape fix into this slice; defer the ABS intervening-clip sub-case to
6b.** The line is drawn by implementation cost, confirmed against the code (not guessed):

- The **flatten side** of the fix (the `ClipChain` bracket loop in `appendBand`) is small, clean, and
  shared with the z-index emit — near-zero marginal cost once `appendBand` exists. It serves **both** the
  relative and abs cases (it just consumes a `ClipChain`), so it lands fully in 6.
- **Relative case — IN 6.** The `pendingPos{frag, clipChain}` threading rides the **already-existing**
  `pendingPositioned` bubbling (`interior`/`layoutBlockChildren`/`layoutBlock` — the same 3 spots already
  in play). Bounded, additive, common-case-empty-chain (identical to today). This is the case the handoff
  names and the adversarial test (case 8) pins.
- **Abs intervening-clip case — DEFERRED to 6b.** Confirmed above: it needs a NEW clip-ancestor parameter
  threaded through `layoutBlock` → `layoutInterior` → `layoutBlockChildren` and captured into
  `deferredAbs`, because no clip-ancestor stack is threaded through layout today. That is a layout-plumbing
  change disproportionate to the rare case it serves (a clip box strictly between an abs box and a higher
  CB; the common abs case — CB *is* the clip — is already handled by `CBOwned`). Splitting it keeps 6
  focused and its diff reviewable.

So 6 ships: full z-index ordering + the relative-clip-escape fix (flatten machinery complete, relative
collection complete). 6b ships only the abs-path clip-ancestor threading, reusing the `ClipChain`
mechanism 6 already built. The 6b handover records this. The holistic review still confirms the assembled
6 diff is coherent, but the split is **decided here**, not deferred to review.

## Degradation

Every unsupported / edge case degrades silently (no panic; recovery at the page boundary, unchanged):

- **`z-index` on a non-positioned box** — ignored (only positioned boxes are in `Positioned`; a
  non-positioned box never enters the sort). Matches CSS.
- **`z-index` on an inline-level box** — already a no-op (inline `position:relative` is a no-op; such a
  box is not in `Positioned`).
- **Equal z-index** — document order (stable sort). The byte-identical guard depends on this.
- **A degenerate clip rect in a `ClipChain`** (zero area) — the painter already no-ops an empty clip;
  the bracket emits `ClipPush`/`ClipPop` harmlessly.
- **Deeply nested intervening clips** — nested `ClipChain` brackets, bounded by tree depth (the same
  depth guard the recursion already has).
- **The abs intervening-clip sub-case (deferred to 6b)** — an `absolute`/`fixed` descendant whose CB is an
  ancestor *beyond* an intervening `overflow:hidden` box paints in its holder's layer **unclipped** by that
  intervening box (today's behavior, unchanged by 6). This degrades silently: it never panics, and the
  result is exactly what 5c produced for this configuration. **Detecting it even to log it requires the
  clip-ancestor threading that 6b adds** (`resolveAbsolute` has only the CB owner, not the chain of clips
  the box passed through), so 6 neither fixes nor logs it — it is documented here and in the CLAUDE.md
  Done note as a known, scoped gap. (The *relative* analogue IS fully handled in 6.)

The CLAUDE.md degradation notes flip when the PR lands: the positioning + overflow Done bullets' "z-index
parsed but not sorted on" becomes **supported (full Appendix E ordering)**; the overflow bullet's "a
`position:relative` descendant of a non-positioned `overflow:hidden` box escapes the clip" becomes
**clipped (relative case)**, narrowed to note the remaining gap — "the *absolute/fixed* intervening-clip
sub-case is deferred to 6b" — so the Done section stays honest about exactly what is and isn't covered.

## Tests

Unit tests assert the item-stream order via `AppendItems` (z-index is a flatten-time fact), since a
"passing" golden can still be wrong — the goldens are the eyeball confirmation, the unit tests are the
precise order check. **Test the flag COMBINATIONS, not each flag alone** (every slice's worst-miss class).

### Item-stream order unit tests (`pkg/layout/css`, new `zindex_layout_test.go`)

Build via `posBox`/`posStyle` (set `style.ZIndex`/`style.ZIndexAuto`), lay out with `layoutTree`, flatten
with `AppendItems`, and assert RELATIVE order of marker items (each box a distinct background color, found
by `bgIndex` — the 5c helper). Cases:

1. **Negative-z behind in-flow content** — a `position:relative; z-index:-1` box overlapping an in-flow
   block: the negative box's Background index is **before** the in-flow block's Background index. (The
   load-bearing assertion — negatives emit before decorations.)
2. **Positive-z over z:auto** — two abs boxes, one `z-index:2`, one default (auto): the `z:2` Background
   index is **after** the auto box's. (Ascending numeric sort.)
3. **Negative < auto < positive ordering** — three positioned boxes (`z:-1`, auto, `z:1`): Background
   indices strictly increasing in that order, straddling the decoration/content phases.
4. **Stable doc order within a band** — two `z:auto` boxes and two `z:5` boxes interleaved in source:
   within each band the source order is preserved (stable sort).
5. **Byte-identical all-auto** — a page with several positioned boxes, **all** z:auto: assert the item
   stream equals the pre-change stream (golden-corpus guard; also covered by the unchanged committed
   goldens, asserted here directly on the slice).
6. **Z-index inside a clip (CB-owned)** — a `z-index` positioned box whose CB **is** an `overflow:hidden`
   box: its items fall **between** that box's `ClipPush` and `ClipPop` (reuse `clipBoundsReal` — first
   ClipPush / last ClipPop indices), in z-order relative to the clip's other CB-owned positioned content.
7. **Z-indexed float** — a box that is both floated and (its float fragment) painted in the float layer;
   assert the float still paints in step 4 (float layer) and a *separate* z-indexed positioned sibling
   sorts correctly around it. (Float+z is an edge interaction; the unit test is the coverage, no golden
   beyond the combination golden.)
8. **Relative-clip-escape (the fix)** — a `position:relative` child of a **non-positioned**
   `overflow:hidden` box, with an offset that pushes the child **past** the clip edge. Assert the child's
   items are bracketed by a `ClipPush(clipBox.paddingBox)…ClipPop` **even though** the child paints in an
   ancestor's positioned layer (CB-owned=false, ClipChain non-empty). The clip rect equals the clip box's
   padding box. (Adversarial: the offset must take the child outside the clip so an *unclipped* render
   would visibly differ.)

De-Morgan note: write any `if !(a && b)` test condition in the De-Morgan'd form (`if a >= b || …`) —
`golangci-lint` flags `if !(a && b)` (QF1001). Run `golangci-lint` per task, not just `gofmt`.

### Golden images (`pkg/doctaculous`, `htmlGoldens` table + committed PNGs)

Author goldens with **visibly overlapping** boxes of different z so the order is unambiguous to the eye.
Four new fixtures (all four combinations, per the brainstorm):

- **`html-zindex-negative`** — a `z-index:-1` colored box overlapping an in-flow colored block: the
  negative box visibly **behind** the in-flow content (a corner of the in-flow block covers the negative
  box). Proves step 2.
- **`html-zindex-stack`** — three overlapping positioned boxes with `z-index: 1, 2, 3` (and one auto):
  higher z visibly **on top**. Proves ascending numeric sort.
- **`html-zindex-clip`** — a `z-index` positioned box inside an `overflow:hidden` box, clipped at the box
  edge while still respecting z-order against the clip's other content. Proves z-order ∘ clipping.
- **`html-zindex-float`** — a floated swatch and a `z-index` positioned box overlapping it: the positioned
  box paints over (or under, by its z) the float per Appendix E (floats are step 4, the positioned box
  step 6/7). Proves float ∘ z interaction.

Generate with `go test ./pkg/doctaculous -run TestHTMLGolden -update`, then **eyeball every new PNG** (the
controller, via the Read tool — the implementer has no image vision). Each must show the stated stacking
order. **Confirm no pre-existing golden changed** (`git status --short pkg/doctaculous/testdata
pkg/render/raster/testdata` shows only new files).

### WPT-style reftests (`pkg/doctaculous`, `wptReftests` table + `NAME.html`/`NAME-ref.html`)

A reftest proves the engine renders the stacking case **identically** to a reference authored without
relying on the feature under test:

- **`zindex-negative`** — a negative-z box behind an in-flow block, vs. a reference where the same two
  boxes are authored in the paint order that produces the identical pixels (the in-flow block painted
  after, on top).
- **`zindex-order`** — two overlapping positioned boxes whose z-index inverts their document order, vs. a
  reference with the boxes authored in the document order that *matches* the z-order (so no sort needed).
- **`relative-clip-escape`** — a `position:relative` child of a non-positioned `overflow:hidden` box,
  offset past the clip edge, vs. a reference where the child is authored already clipped to the box (e.g.
  the visible portion sized to fit). Proves the fix renders the clipped result. (Covers the **relative**
  case, which is this slice's scope; the abs intervening-clip reftest lands with 6b.)

Add each to the `wptReftests` table with its `viewportPx` and `what`. Reftests render both pages and
assert identical rasterization.

### Regression / corpus guard

- Run the **full** existing golden + reftest suite **without `-update`** and confirm **no diff** — a
  paint-order change is high-risk for silently reordering existing pages. The all-auto byte-identical
  property is the guard.
- `go test -race ./...` (concurrency is core; the sort-a-local-copy decision keeps flatten race-free).
- `gofmt -l`, `go vet ./...`, and `golangci-lint run` on the changed packages
  (`./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...`) — **NO `//nolint`**, no `slices.*`/`max`/`min`
  modernize hints, explicit clamps and indexed loops.

## Notes for the implementer (lessons carried from 5a/5b/5c — these earned their keep)

- **Sandbox blocks the Go build cache + TLS** — run `go`/`golangci-lint`/`gofmt` (and `gh pr create`,
  `git push` over HTTPS) with the sandbox disabled.
- **Editor diagnostics LAG** — after adding `Box`/`PositionedInfo` you'll see stale
  "undefined"/"unused"/"redeclared" errors and phantom `zz_*` files. Trust `go build`/`go test` and
  `find . -name 'zz_*'`. Delete any `zz_*` throwaway before finishing; confirm `git status` clean.
- **The zero-value `Length` trap** — a raw `ComputedStyle`/`Box` literal omitting
  `Width`/`MaxWidth`/offsets reads as explicit `0`, not `auto`/`none`. Reuse `posStyle()`/`posBox()` (set
  z-index on the returned style), `blockBox()`, and the 5c `clipBoundsReal`/`bgIndex` helpers.
- **You are on `feat/html-zindex`** — do NOT checkout/stash/switch branches.
- **Stamp `frag.Box` at ALL 4 collection sites** — a missed site leaves a positioned fragment with nil
  Box, which reads as z:auto (middle band). That is a silent ordering bug for an explicitly z-indexed box,
  not a crash — the unit tests (cases 1–4) catch it because they assert the explicit-z ordering.
- **The byte-identical guard is the most important test** — if any existing golden/reftest changes, the
  sort or the band split broke the all-auto identity. Fix before proceeding.

## Open questions

None blocking. The scope line (relative clip-escape in 6, abs intervening-clip in 6b) is **decided** in
"Fold vs. 6b" above, confirmed against the layout-threading cost in the code. The holistic review confirms
the assembled 6 diff is coherent but does not re-litigate the split.
