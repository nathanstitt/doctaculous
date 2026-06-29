# Fidelity backlog — the complete inventory (goal: fix ALL of these)

**Goal (user directive):** fix **every** fidelity issue in the engine — HTML/CSS, PDF, and DOCX-render — until
the only remaining gaps are the explicitly out-of-scope items. EPUB is **out of scope** (descoped this session;
see CLAUDE.md out-of-scope note). This doc is the authoritative checklist; CLAUDE.md is the prose source of
truth and stays in sync. Each fix lands with a fixture/test in the same PR, mutation-verified, byte-identical
corpus except intended golden changes (which get eyeballed).

Status legend: ☐ open · ◐ in progress · ☑ done (move the prose to CLAUDE.md "Done" when ☑).

---

## A. Cross-cutting (highest leverage — unblocks several modes at once)

- ☐ **A1. RTL / bidi (`direction`)** — *Large.* The engine has **no** `direction`/bidi support anywhere. It is
  the **sole** deferral in tables, flexbox, AND grid (each logs "laying out LTR"), and also affects
  inline/block text order. One sub-project unblocks all three modes + general inline. **Touches:** the inline
  core (`pkg/layout/inline`), `pkg/layout/css` block/inline/table/flex/grid, `pkg/css` `direction`/`unicode-bidi`.
  Sequence this either first (so the per-mode RTL items below become free) or last (after the cheaper per-mode
  fixes). Decision needed.

---

## B. HTML/CSS — visible bugs (have repros; fix first within HTML)

- ☑ **B1. `display:block` on `<img>`/replaced ignored (F-E)** — *DONE.* `isBlockLevelOuter` now returns the
  replaced box's outer level from its display (`isBlockLevelOuterDisplay`), and the block stacker's child guard
  accepts a block-level replaced box (`isBlockLevelReplaced`) — so a `display:block <img>` stacks as a block
  (dispatched to `layoutBlockReplaced`). Tests: `TestReplacedBlockStacksAsBlock` (3 stacked children,
  mutation-verified on both fix sites) + `block-img` WPT reftest (discriminating: inline-block then a block img
  == authored stacked, fails when reverted). Goldens `html-image-basic`/`html-image-object-fit` regenerated +
  eyeballed (the block imgs now stack with their vertical margins, which they did not before).
- ☑ **B2. inline-block with text bottom-aligned, not baseline (F-F)** — *DONE.* `atomicRunFor` now sets
  `BaselinePt` to the distance from the atom's border-box top to its **last in-flow line box's baseline**
  (`lastInFlowLineBaseline`), per CSS 2.1 §10.8.1 — falling back to the bottom margin edge (`frag.H`) for a
  replaced atom (no line box), an `overflow≠visible` inline-block, or an empty one. Tests:
  `TestInlineBlockTextBaseline` (mutation-verified — the bug misaligns by ~32pt) + the `html-inline-block-baseline`
  golden (eyeballed: "BOX" aligns with "before"/"after" on one line). Empty inline-blocks keep bottom alignment
  (`TestInlineBlockAtomics` unchanged). NOTE: surfaced two orthogonal pre-existing bugs (E4 shrink-to-fit, E5
  line-gap inflation) — filed below; B2 itself is correct and the existing corpus is byte-identical.

## C. HTML/CSS — positioning fidelity

- ☐ **C1. Precise static-position solve** for an all-`auto`-offset abs box (today approximates to the CB
  top-left). *Deferred (Medium).* Needs capturing each abs box's hypothetical in-flow position at collection
  time (`layoutBlockChildren`) and threading it onto `deferredAbs` — touches the core positioning path; the
  CB-top-left approximation is documented + logged. Lower frequency (all-auto abs is uncommon).
- ☑ **C2. abs `width:auto` shrink-to-fit** — *DONE.* A `width:auto` abs box not pinned by both left+right (and
  not replaced) now sizes to `min(max(min-content, available), max-content)` via `absShrinkToFitWidth` (the
  memoized measure helpers), threaded into BOTH `absRect`'s auto-width fallback (so a right-anchored box's left
  edge is consistent) and the interior layout width. Tests: `TestAbsAutoWidthShrinksToFit` +
  `TestAbsRightAnchoredShrinkToFitPosition` (mutation-verified). + `html-abs-fidelity` golden (eyeballed).
- ☑ **C3. abs `margin:auto` centering** — *DONE.* `distributeAbsMargins`: when an abs box's left+right (or
  top+bottom) and size are all definite (over-constrained) and the margins are auto, the leftover space splits
  evenly (centering) or goes to a single auto margin (CSS 10.3.7). Tests: `TestAbsMarginAuto…` (mutation-
  verified) + the `html-abs-fidelity` golden (the blue box centers).
- ☐ **C4. percentage `top`/`bottom` against an auto-height CB.** *Deferred (Medium).* The engine resolves
  vertical percentages against the CB's computed height; an auto-height CB's height does not account for the
  out-of-flow box — a documented edge case, low value.
- ☐ **C5. `bottom`-only auto-height abs box** — *Deferred (Medium).* Needs a shrink-to-fit HEIGHT (the C2
  machinery but vertical); the engine measures content WIDTH, not height (the same single-axis limitation D3 /
  flex / grid / table vertical-content sizing all share). Documented + logged.
- ☐ **C6. `position:relative` on a text-only inline box** — *Deferred (Medium–large).* Structural: inline
  (`BoxInline`) elements generate no fragment (their glyphs flatten into the parent's lines), so there is
  nothing to carry a `RelOffset`. Needs inline-box fragments or a per-run offset on `LineFragment`. A no-op
  today (block-level relative is exact; atomic inline-block/replaced relative is a separate known no-op).

## D. HTML/CSS — replaced content

- ☑ **D1. `object-position`** — *DONE.* `object-position` parsed (keywords + percentages, 1–2 values) into
  `ObjectPositionX/Y` fractions (initial 0.5/0.5); threaded onto `ImageItem`/`ImageContent` and applied in
  `fitDest` (`positioned` helper) so the fitted image shifts within the content box for contain/none/scale-down.
  Tests: `TestObjectPosition` (parse), `TestPaintImageObjectPosition` (paint), `html-object-position` golden
  (eyeballed: top-left/center/bottom-right). Default 0.5 reproduces the prior centering → corpus byte-identical.
- ☑ **D2. ratio-preserving min/max sizing (CSS 10.4)** — *DONE.* When the used size came from the intrinsic
  ratio (≥1 axis auto, ratio available, not both pinned), `constrainRatio` applies the §10.4
  constraint-violation table (a single violated bound scales the other axis to preserve the ratio; conflicting
  bounds clamp independently). Both-dims-explicit falls back to per-axis clamping (ratio already broken). Tests:
  `TestReplacedRatioPreserving…` + the both-explicit guard (mutation-verified).
- ☐ **D3. percentage `height` basis on replaced elements** — *Deferred (structural).* Needs a definite
  containing-block HEIGHT threaded through the layout chain; the engine is fundamentally width/single-axis
  (a percentage height resolves against a 0 basis → treated as auto today, logged). A broad plumbing change;
  low frequency. Documented in `replaced.go`.
- ☐ **D4. CSS `background-image` decode** — *Deferred (feature slice).* A whole new path: parse
  `background-image: url()`, decode via the resource loader, carry on the fragment, and paint with
  `background-repeat`/`-position`/`-size`. Medium–large; its own slice. (Today `background` keeps color only;
  `url()` is dropped.)

## E. HTML/CSS — general inline / flow

- ☐ **E1. Full `vertical-align` keyword set** (only atom-baseline mechanics landed: `sub`/`super`/`top`/
  `middle`/`bottom`/`text-top`/`text-bottom`/`%`/length). *Medium–large.* (Overlaps B2.)
- ☐ **E2. `margin:auto` horizontal centering** (block-level). *Small–medium.*
- ☐ **E3. Margin-collapse edge cases** — empty-block collapse-through, clearance, `min-height` interaction. *Medium.*
- ☑ **E4. inline-block (auto width) shrink-to-fit** — *DONE.* `inlineBlockCBWidth` computes the CSS 10.3.9
  shrink-to-fit width (`min(max(min-content, available), max-content)`, via the memoized measure helpers) and
  the inline-block atom is laid out against it, so an `width:auto` inline-block wraps its content instead of
  filling the line. A specified/percentage width still resolves normally. Tests:
  `TestInlineBlockShrinkToFit` (mutation-verified) + `TestInlineBlockSpecifiedWidthHonored`. No existing golden
  changed (no prior golden had an auto-width inline-block with text); the `inline-block-baseline` golden was
  switched to auto-width to demonstrate it.
- ☑ **E5. line-gap metric inflated (→ inline-block height inflation)** — *DONE.* Root cause: the bundled TeX
  Gyre faces report an anomalous **hhea line gap of ~1.3–1.4 em**, and `autoLineHeight` added it then multiplied
  by 1.15 — ballooning a 16px line to ~49pt. Fix: `autoLineHeight = (ascent+descent)×cssDefaultLineMult` — the
  gap is dropped (browsers compute "normal" from ascent/descent, not a runaway hhea gap); this gives
  browser-comparable ~1.2–1.65 em for the three bundled families. Tests: `TestAutoLineHeightExcludesLineGap` +
  `TestParagraphLineHeightReasonable` (mutation-verified — the bug gives a 49pt inter-line gap). **9 goldens
  regenerated + eyeballed** (paragraphs/tables/float-figure: proper line spacing now, ~2× tighter — a major
  visible improvement).
- ☑ **E6. `font` shorthand size not applied** — *DONE.* `expandFont` parses the CSS 2.1 `font` shorthand
  (`[style||variant||weight]? size[/line-height] family`) into longhands: size (length or absolute-size
  keyword), family, weight/style, inline line-height; an invalid shorthand (no size or no family) is dropped.
  Wired as `case "font"` in `applyDeclaration`. Tests: `TestFontShorthandSizeFamily` /`…InlineLineHeight`
  /`…InvalidDropped` (mutation-verified).

## F. HTML/CSS — tables

- ☐ **F1. RTL/`direction`** (column order) — *covered by A1.*
- ☑ **F2. Six background layers** — *DONE.* Column-group, column, row-group, and row backgrounds now paint
  behind the cells in CSS 17.5.1 order (table → col-groups → cols → row-groups → rows → cells). `gridCol` carries
  its `<col>` box; `tableGrid` retains col-group/row-group spans; `backgroundLayers` emits a background fragment
  per layer (rect = union of the layer's final positioned cells, robust to the baseline grow), prepended behind
  the cells. Tests: `TestTableBackgroundLayers` (emission + paint order, mutation-verified) + the
  `html-table-bg-layers` golden (eyeballed: column tint behind cells, row stripes on top where they overlap).
  Non-styled tables stay byte-identical (no layer → nothing emitted).
- ☑ **F3. `empty-cells` property** — *DONE.* `EmptyCells` added to `ComputedStyle` (inherited, parsed); in
  separate-borders mode an empty cell (`isEmptyCellFragment`) with `empty-cells:hide` has its background +
  border suppressed after layout (slot/sizing unchanged). Tests: `TestEmptyCellsHide…`/`…Show…`
  (mutation-verified). Collapse mode unaffected (correct).
- ☑ **F4. percentage `<col>` width with no cells** — *DONE (already correct).* A `<col width="N%">` reserves
  its share in both auto and fixed layout even with no originating cell (verified by probe; `addColumnHint`
  creates the column with its `pct` and the percentage-distribution path honors it). Locked by
  `TestPercentColumnWithNoCells` (auto + fixed). No code change needed.
- ☑ **F5. 3D collapse border styles** — *DONE.* New `BorderStyle` values `BorderOutset/Inset/Ridge/Groove`;
  the paint layer renders them as bevels (`edge3DColor`: lit top/left, darkened bottom/right; ridge/groove
  split the strip across its thickness). BOTH `mapBorderStyle` (normal borders — which previously rendered 3D
  styles as nothing) and `borderStyleToLayout` (collapse — previously solid) map them. Tests:
  `TestPaintBorderOutset…`/`…Ridge…` (mutation-verified), `TestMapBorderStyle` updated, new
  `html-border-3d-styles` golden (eyeballed: outset/inset/ridge/groove bevels correct).
- ☑ **F6. percentage-column basis differs fixed vs auto** — *DONE.* `solveFixedWidths` now resolves a
  percentage column width against `contentW - spacing` (the width available to columns), matching
  `solveAutoWidths`, instead of the full `contentW` — so a % column is no longer over-sized by the
  border-spacing amount. Test: `TestFixedPercentColumnBasisExcludesSpacing` (mutation-verified). Auto-column
  distribution unchanged (the spacing was already subtracted there).
- ☑ **F7. `buildCollapsedBorders` O(cells²)** — *DONE.* The occupancy scan now stores `*gridCell` per slot
  (retained as `tableGrid.cellMap`); `cellAt` is an O(1) map lookup instead of a per-neighbor linear scan.
  Behavior-preserving (byte-identical corpus).
- ☐ **F8. rowspan cell whose *spanned-into* row grows from baseline does not re-grow.** *Deferred (localized).*
  Needs the cross-row baseline re-solve the table design deliberately avoids; documented limitation in
  `table.go`. Low value, high complexity — kept deferred.

## G. HTML/CSS — web fonts

- ☐ **G1. synthetic bold/oblique** for a `@font-face` family supplying one variant (note: bundled substitutes
  ship regular-only — see PDF item J4). *Medium.*
- ☐ **G2. `unicode-range` subsetting** (captured-but-ignored; whole face used for every rune). *Medium.*
- ☐ **G3. `font-display`** (ignored). *Small* (no async in synchronous layout; likely a documented no-op kept).
- ☐ **G4. variable-font axes** (`font-variation-settings` → default instance). *Large.*
- ☐ **G5. `local()` beyond `DiskFontProvider`** (no OS font-store enumeration). *Medium* (platform-specific).
- ☐ **G6. content-addressed fetch cache** (FaceCache keyed `(family,style)`; one file fetched per style). *Small (perf).*

## H. HTML/CSS — flexbox

- ☐ **H1. multi-line flex** (`flex-wrap: wrap`/`wrap-reverse` + `align-content`) — the big one. *Large.*
- ☐ **H2. RTL/`direction`** on a row — *covered by A1.*
- ☑ **H3. line cross size clamped to a definite container cross size** — *DONE.* For a single-line flex
  container the line cross size is now the container's DEFINITE inner cross size when set (`flexCrossSize`),
  so `align-items:center`/`flex-end` align within the container's extent (e.g. a fixed row height), not the
  tallest item's. (Floored at the max item so a too-small container doesn't clip an item.) Tests:
  `TestFlexAlignCenterUsesDefiniteHeight` (mutation-verified); the `flex-align-center` golden + WPT reftest
  reference corrected to the browser-accurate centered offsets (eyeballed); the column-stretch unit test
  updated (an auto-width column item correctly stretches to the container width).
- ☐ **H4. column `flex-basis: auto`/`content` height** (max-content width proxy today). *Deferred (Medium)* —
  same vertical-content-measurement limitation as I4/D3.
- ☐ **H5. `flex-grow`/`shrink` cross-axis gap factors** (revisit with multi-line). *Small (with H1).*
- ☑ **H6. column-container `align-items: baseline`** — *CONFIRMED CORRECT (no fix).* CSS Flexbox §9.4.3:
  baseline self-alignment in a column flex container resolves to `flex-start` (there is no cross-axis text
  baseline). The engine already falls back to `flex-start` and logs it — spec-compliant, not a gap.

## I. HTML/CSS — grid

- ☐ **I1. named-LINE placement** (`grid-column: start/end` referencing `[name]`s; today parsed-and-ignored).
  *Deferred (Medium)* — needs new machinery: the tokenizer doesn't emit bracket tokens, `TrackList` has no
  named-line storage, and `placeItems` has no `LineName` resolution. 3+ interconnected changes.
- ☐ **I2. flow-axis-locked auto-placement** (definite flow-axis line + auto cross axis honors span, ignores
  start line). *Deferred (Small)* — a documented, non-overlapping simplification (`grid_place.go` scans the
  locked line from 0 rather than continuing the sparse cursor); niche, intentional.
- ☐ **I3. RTL/`direction`** — *covered by A1.*
- ☐ **I4. row-track content-height width-proxy** (`measureMaxContent` returns WIDTH for a ROW track). *Medium*
  (shared root cause with H4, F-rowspan — vertical content sizing).
- ☑ **I5. conservative baseline-group extra** — *DONE.* `alignBaselineGroup` now returns the EXACT extra cross
  size (`max(bottom after shift) − max(bottom before shift)` over participants), not the largest single shift —
  so a row/line is no longer over-expanded when the most-shifted item doesn't reach lowest. Tests:
  `TestAlignBaselineGroupExactExtra` + the reaches-lowest guard (mutation-verified). Corpus byte-identical
  (existing baseline tests have the tallest item also most-shifted, where the values coincide).
- ☐ **I6. rowspan cell whose spanned-into row grows from baseline** — *same as F8.*
- ☐ **I7. `subgrid`** (→ `none`). *Large.*
- ☐ **I8. `repeat(auto-fill/auto-fit)` empty-track collapse approximate.** *Medium.*

## J. PDF — wrong output (not crashes; have triggers) — ALL DONE

- ☑ **J1. Separation/DeviceN `scn` colors inverted** — *DONE.* A new `Resources.ColorSpace(name)` resolves a
  named Separation/DeviceN space to a `TintTransform` (parsing the tint `/Function` + alternate-space component
  count); the graphics state carries `fillTint`/`strokeTint` (set by `cs`/`CS`), and `sc`/`scn`/`SC`/`SCN`
  evaluate the tint through it (`resolveColorN`) → alternate components → device color, instead of mistaking a
  1-comp full-ink tint for gray (white). Tests: `TestSeparationTintTransform`/`…Stroke` (interpreter) +
  `TestRenderSeparationColor` (end-to-end, real `function.Parse`); mutation-verified at both layers. New fixture
  `gen.SeparationColorPDF`.
- ☑ **J2. Form XObject `/BBox` clip never applied** — *DONE.* `Resources.Form` now also returns the `/BBox`;
  `doXObject` clips to the BBox rectangle (all 4 corners through the form CTM, so rotation/skew are correct)
  before running the form's content (`clipFormBBox`), per ISO 32000 §8.10.1. A missing/malformed BBox degrades
  to no clip. Test: `TestRenderFormBBoxClip` (end-to-end, mutation-verified). New fixture `gen.FormBBoxClipPDF`.
- ☑ **J3. DCTDecode (JPEG) ignores `/Decode`** — *DONE.* `applyDCTDecode` applies a non-identity `/Decode` to a
  decoded JPEG in its native space (CMYK before the RGB conversion; RGB otherwise), so an Adobe CMYK JPEG's
  `/Decode [1 0 …]` inverts correctly. Tests: `TestApplyDCTDecodeInvertsCMYK`/`…InvertsRGB`/`…IdentityUnchanged`
  (mutation-verified).
- ☑ **J4. Text render modes 1/2/4/5/6** — *DONE.* `drawGlyph` now honors the render mode: fill (0/2/4/6),
  stroke (1/2/5/6 — strokes the glyph outline with the stroke color/line params), invisible (3), clip-only (7,
  no paint). The CLIP accumulation of modes 4–7 is a documented deferral (glyph outlines are not gathered into
  the text clip applied at ET; modes 4–6 still paint, 7 paints nothing — never crashes). Tests:
  `TestShowTextStrokeMode`/`…FillStrokeMode`/`…ClipOnlyMode` (mutation-verified).

## K. PDF — feature gaps (TODO 1–4; "unsupported" → real output)

- ☐ **K1. JBIG2 + JPX/JPEG2000 scan filters** (`pkg/pdf/filter/filter.go`, today `ErrUnsupported`). *Large.*
- ☐ **K2. Tiling patterns (PatternType 1)** (today skipped+logged). *Medium.*
- ☐ **K3. Higher-fidelity Coons/tensor patches (Types 6/7)** — bicubic boundary vs the current bilinear-corner
  approximation. *Medium.*
- ☐ **K4. Luminosity soft masks (`/SMask` in ExtGState) + transparency groups.** *Large.*
- ☐ **K5. Encryption: non-empty user/owner passwords, per-stream `/Crypt` overrides, `/Perms` validation.** *Medium.*
- ☐ **K6. Base-14 weights & symbol fonts** — bold/italic/oblique map to regular (affects DOCX too); Symbol &
  ZapfDingbats have no substitute (skipped). Bundle weighted faces + symbol look-alikes + AFM widths. *Medium–large.*
  (Resolving K6 also unblocks G1's "substitutes ship regular-only" caveat.)

## L. PDF — perf nits (lower priority, in scope as fidelity-of-performance)

- ☐ **L1. Per-glyph paint allocations** (`paint.go transformPath` + `device.go rasterizeMask`, ~3/glyph). Only
  worth a transformed-glyph cache if profiling shows paint dominating. *Small–medium.*
- ☐ **L2. `over()` straight-vs-premultiplied alpha** (`device.go`) — latent fragility if a transparent page bg
  is ever introduced; not a live bug. *Small (document or harden).*

## M. DOCX features (reflow frontend — TODO 5)

These are missing *features* (graceful skips today), arguably "fidelity" of DOCX rendering. Confirm with user
whether DOCX feature-completeness is in the "ALL fidelity issues" scope or a separate track.

- ☐ **M1. lists/numbering** (`numbering.xml`, counters, marker glyphs). *Large.*
- ☐ **M2. tables** (`w:tbl`, grid + col-width solve, spans, cell recursion). *Large.*
- ☐ **M3. images** (`w:drawing`→`a:blip`, decode, EMU placement). *Medium.*
- ☐ **M4. headers/footers + multi-section.** *Medium.*
- ☐ **M5. embedded fonts** (de-obfuscate `word/fonts/*` — also fixes bold/italic). *Medium.*

## N. Pagination — structural deferrals (need real fragmentation; larger)

- ☐ **N1. Mid-box / mid-line / mid-table-row / mid-flex-or-grid-item splitting.** *Large* (the big one; threads
  the split into the block stacker or a fragmentation-aware relayout — breaks the post-pass model).
- ☐ **N2. Widows/orphans + `break-inside: avoid` + `break-*: avoid`.** *Medium–large* (depends on N1).
- ☐ **N3. Honoring a genuinely MID-BLOCK forced break on a nested block** (edge breaks now propagate). *Medium* (depends on N1).
- ☐ **N4. Per-page float distribution.** *Medium.*
- ☐ **N5. Per-page bottom-anchored `fixed`** (per-page `resolveAbsolute` height). *Medium.*
- ☐ **N6. CSS paged media: `@page` size/margins/named pages + running headers/footers.** *Large.*

---

## Suggested execution order (proposal — confirm)

1. **Quick visible wins:** B1, B2 (+ E1 since it overlaps B2). Small PRs, eyeball-verifiable.
2. **PDF wrong-output:** J1, J2, J3, J4 — bounded, each a clear trigger + test; no layout risk.
3. **Per-mode CSS fidelity, cheapest first:** D1, F2, F3, F4, F5, F6, F7, F8, I1, I2, I5, I8, C3, C4, H3, D2, D3, C1, C2.
4. **The vertical-content-sizing cluster** (shared root cause): H4, I4 (the `measureMaxContent` width-proxy).
5. **RTL/bidi (A1):** resolves F1/H2/I3 + general inline at once. (Could move earlier if you prefer.)
6. **Big layout additions:** H1 (multi-line flex), C6/E1 (inline-box fragments + full vertical-align), G2/G4 (font
   subsetting/variable), I7 (subgrid).
7. **PDF features:** K2, K3, K5, K6, then K1, K4 (largest).
8. **Pagination structural:** N1 (unlocks N2/N3), N4, N5, N6.
9. **Perf:** F7 (done in step 3), G6, L1, L2.
10. **DOCX features (if in scope):** M1–M5.

Open scope questions for the user:
- **DOCX feature-completeness (M1–M5):** part of "ALL fidelity," or a separate track?
- **RTL/bidi (A1) timing:** before the per-mode RTL items (makes them free) or after the cheap fixes?
- **Batch size / PR cadence:** one big branch with many commits, or a stream of small stacked PRs?
