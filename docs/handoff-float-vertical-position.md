# RESOLVED: float vertical position on the FLOAT showcase page (page 3)

**Status:** FIXED. Root cause found; two engine fixes landed with regression tests; the
`htmldoc-p2.png` golden regenerated and eyeballed (the figure now sits cleanly below the lede).
**Branch:** `fix/showcase-engine-bugs`.
**Original symptom:** on the "03 / FLOAT" showcase page, the floated `.figure` (its
`img/photo.jpg`) sat ~a line too **high** — its top overlapped the lede paragraph's bottom
border / the section divider rule, instead of dropping cleanly below the lede.

---

## The actual root cause (NOT what the prior session hypothesized)

The prior session guessed the residual was a **pagination** frame/shift discrepancy
(`pagemodel.go`, `floatsForRun`). That was **wrong**. A fragment-tree oracle (walking the tree
BEFORE pagination) proved the float was already ~a line too high **in layout** — the bug is in
the block formatting context, not pagination. Two distinct defects, both "a float's Y is not
offset by an inset that its in-flow siblings DO get":

### Fix A — a float ignored the collapsed margin that gaps its containing block from a sibling
(`pkg/layout/css/block.go`, `layoutBlockChildren`)

When a block child (e.g. the FLOAT `<section>`) is stacked, it is first laid out against a
**provisional** band origin `bandOriginY + startY` (the previous sibling's border-box bottom, with
no collapsed margin), because its real border top is not known until after layout. Its border top
is then resolved to `borderTop = prevBorder + collapseMargins(prevBottom, marginTop)` and the whole
in-flow fragment is shifted down by `borderTop - marginTop`. **A float placed during that child's
layout attaches to the ancestor BFC's float list — not to the child's fragment — so it does NOT
ride that shift**, and is left too high by exactly the collapsed inter-sibling margin (the showcase's
`.section { margin-bottom: 26px }`, hence ~26px). The `.figure` is inside the *second* section; the
*first* section's margin-bottom is the culprit — which is why a single-section repro looked correct
and the prior session couldn't reproduce it in isolation.

**Fix:** after the in-flow shift, correct any float the child placed into THIS BFC by
`borderTop - startY - child.marginTop` (the gap between the provisional band origin and the child's
resolved content-box top), moving both the avoidance geometry (`floatBox.y`) and the fragment
(`shiftFragment`). New helper `floatContext.shiftFloatsFrom`. Zero for the first/cleared child, so
the common case is byte-identical.

### Fix B — a float in a bordered/padded BFC was not inset by that BFC's own top border+padding
(`pkg/layout/css/block.go`, `layoutBlock`)

A nested BFC's floats (`in.bfcFloats`, placed in the interior's content-top-0 frame) were assigned
to `frag.Floats` WITHOUT the `contentTopY` shift the in-flow children get, so a float in a
bordered/padded BFC painted at the BFC's **border-box** top (its X was inset by placement, only Y was
short). Found while testing Fix A; independent and pre-existing.

**Fix:** `shiftFragments(in.bfcFloats, contentTopY)` before assigning. No golden changed (no existing
golden had a float directly inside a bordered BFC), so it is purely corrective.

### Fix #2 (prior session, now kept + tested): float below its DIRECT sibling's margin-bottom
The `child.Float` branch now advances the float's top past `prevBorder + prevBottom` (CSS 9.5). This
was the prior session's uncommitted edit; it is correct and kept, and now has a regression test
(`TestFloatNotHigherThanPrevSiblingMarginBottom`). It handles the *direct* sibling margin; Fix A
handles the *containing block's* sibling margin — a different path.

---

## Tests

- `pkg/layout/css/floatvpos_test.go` — the core regressions (all verified to FAIL when their fix is
  reverted): `TestFloatNotHigherThanPrevSiblingMarginBottom` (fix #2),
  `TestFloatAfterCollapsedSiblingMargins` (fix A, the exact showcase mechanism),
  `TestFloatByteIdenticalWhenNoCollapse` (no-op guard), `TestShiftFloatsFromGeometryAndFragment`
  (the primitive), `TestFloatInNestedBFCNotDoubleShifted` + `TestFloatInBorderedBFCInsetByOwnPadding`
  (fix B), `TestRightFloatAfterCollapsedMargin` (side-agnostic).
- `pkg/layout/css/floatvpos_oracle_test.go` — a paginated multi-run oracle asserting the figure's
  painted margin-box top ≥ the lede's margin-box bottom.
- `pkg/doctaculous/floatvpos_oracle_test.go` — the SAME assertion over the REAL htmldoc showcase
  (OpenURL + WithDefaultPaged), reading paint items (not pixels) in final page space.
- `pkg/doctaculous/testdata/golden/htmldoc-p2.png` — regenerated; figure now below the lede.

## Lesson from the prior session's flailing
The reliable oracle is the **fragment tree** (`layoutTree` → walk fragments, compare rects in ONE
frame), NOT pixel-scanning. Building it first localized the bug to layout vs. pagination in minutes
and avoided all the frame/pixel confusion the prior session documented.
