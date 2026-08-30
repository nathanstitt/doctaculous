# SVG `<text>` (PR 6 of 8) — Design

**Date:** 2026-08-26
**Status:** Approved design (autonomous), pending implementation
**Parent spec:** [2026-08-25-svg-support-design.md](2026-08-25-svg-support-design.md)
**Base branch:** `feat/svg-text`, stacked on `feat/svg-use-markers` (PR #107)

## Goal

Render SVG `<text>` as **vector outlines** — fillable, strokeable, and usable as
clip/mask geometry — reusing the existing shaping core rather than building a
second text stack.

The resvg corpus carries **356 text fixtures**, the largest tranche in the
suite. That is more than one reviewable PR can carry, so this PR takes the
core and defers two self-contained subsystems that are genuinely new work.

## What the survey established

The repo already has almost everything needed, and the critical fact is that
**`inline.Shape` is a pure function that is NOT fused to line-breaking**
(`pkg/layout/inline/shape.go:132`). The CSS engine calls `Shape` and then
separately calls `BreakNextWrap`. SVG `<text>` does not wrap, so it calls
`Shape` and simply walks the flat `[]Glyph` accumulating `Advance` — skipping
`Break`/`MakeLine`/`Place` entirely.

Reused directly, with no new seams: `inline.Shape`; Arabic harfbuzz shaping and
per-rune script fallback (both automatic *inside* `Shape`); `inline.Reorder` for
bidi (standalone on a flat glyph slice); `Face.Glyph`/`Face.Outline` for
glyph→`*render.Path`; and `layout/font.FaceCache` for family resolution.

## Decisions taken (autonomous, with rationale)

### 1. SVG text emits `FillGlyph`/`Stroke`, never `DrawGlyph`

`pdfwrite`'s `DrawGlyph` emits PDF `Tj` text operators
(`pkg/render/pdfwrite/device.go:186`). That is right for reflowed DOCX/HTML —
it embeds a real font and keeps the text selectable — but it is the wrong model
for SVG, which needs:

- independent fill **and** stroke paint on the same glyph,
- arbitrary per-glyph transforms (`rotate`, `<textPath>` tangent frames),
- glyphs participating in clip paths and masks as ordinary geometry.

PDF text operators express none of those cleanly. SVG text therefore goes
through `FillGlyph`/`Stroke` with the transformed outline — the same path both
backends already agree on, and the same `paintFill`/`paintStroke` helpers
`draw.Renderer.paintShape` uses for shapes.

**The cost is honest and must be stated:** SVG text in PDF output is vector
outlines, not selectable/searchable text. That is the correct trade for
fidelity here, and PR 8 may revisit it for the simple fill-only case.

### 2. `letter-spacing`/`word-spacing` land here even though CSS lacks them

The survey found these implemented **nowhere** in the repo — not in `pkg/css`,
not in `pkg/layout/css`. 19 fixtures need them. Since SVG applies them as a
post-shaping advance adjustment on a flat glyph slice, implementing them in the
SVG walk is small and self-contained. Implementing them for CSS reflow is a
separate, larger job (they interact with line-breaking and justification) and is
explicitly **not** in scope.

Recorded so the asymmetry is deliberate, not an oversight: after this PR,
`letter-spacing` works in SVG and silently does nothing in HTML/DOCX.

### 3. Deferred to PR 6b: `<textPath>` (44 fixtures) and `writing-mode` (25)

Both are entirely new subsystems, not adaptations:

- **`<textPath>`** needs arc-length parameterization of a `render.Path` plus
  per-glyph tangent frames. Nothing in the repo walks a path by arc length.
  PR 5's `render.Vertices` computes tangents at *vertices*, not at arbitrary
  distance along a curve, so it is a starting point but not a solution.
- **`writing-mode`** needs vertical metrics — `vhea`/`vmtx` table reading that
  `pkg/font` does not do — plus a vertical advance model throughout. Every
  metric in the codebase is horizontal-only today.

  > **SUPERSEDED.** Both halves of this have since shipped: `pkg/font` exposes
  > vertical metrics (`Face.GlyphVAdvance`), and the CSS path lays out a
  > vertical line. The premise that this was a "genuinely new subsystem" was
  > wrong — the metrics were already reachable through a dependency the repo
  > consumed. Left as written because it records the reasoning at the time;
  > `docs/SVG.md` carries the current status, which is that SVG's own text
  > placement still needs wiring to the model that now exists.

Shipping either badly would be worse than deferring it. Both degrade
gracefully: `<textPath>` renders its text on a straight baseline with a logged
warning, `writing-mode` renders horizontally with a logged warning. Neither
silently produces wrong-looking output without a diagnostic.

### 4. `<tref>` is dropped, not deferred

11 fixtures. `<tref>` was **removed from SVG 2** and is unimplemented in every
current browser. Implementing it would be gold-plating a dead feature. It goes
to `unsupportedElements` with a log line, and its fixtures are not vendored.

### 5. Baseline attributes: implement the common set, log the rest

`alignment-baseline` (19), `dominant-baseline` (21), and `baseline-shift` (22)
total 62 fixtures. The values that appear in real documents — `auto`, `middle`,
`central`, `hanging`, `text-before-edge`, `text-after-edge`, `alphabetic`, plus
`baseline-shift`'s `sub`/`super`/length/percentage — are computable from
`Face.Metrics()` (`pkg/font/family.go:227`), which already exposes
ascent/descent/lineGap.

The remainder (`ideographic`, `mathematical`, and the glyph-orientation
attributes) need OS/2 and BASE table data `pkg/font` does not parse. Those
degrade to `alphabetic` with a warn-once.

## Architecture

A new `pkg/svg/text.go` (parse/lower) and `pkg/svg/draw/text.go` (paint),
mirroring the shape path exactly:

**Parse time** — `<text>`/`<tspan>` lower to a `Text` scene node holding
resolved `Style` plus a flat list of positioned runs. The `x`/`y`/`dx`/`dy`/
`rotate` attributes are **per-character lists** in SVG, so lowering resolves
them into per-glyph adjustments. Nested `<tspan>`s inherit and can override,
so lowering walks the tree carrying an inherited position cursor.

**Paint time** — `draw.paintText` calls `inline.Shape` once per run, walks the
flat glyph slice accumulating advances, applies letter/word-spacing and the
per-character `dx`/`dy`/`rotate`, resolves `text-anchor` by measuring total
advance and shifting the origin, then for each glyph transforms the outline and
calls the existing `paintFill`/`paintStroke` helpers.

`text-anchor` (13 fixtures) needs total advance **before** placing, so the walk
is two-pass: measure, then place. `textLength`/`lengthAdjust` (16 fixtures) use
the same measurement to compute a per-glyph advance correction.

Text as clip/mask geometry falls out for free: the clip walk already accepts
`<text>` per the SVG child allowlist (PR 4 built the allowlist and left text
out pending this PR), and glyph outlines are ordinary `*render.Path` values.

## Out of scope

- `<textPath>` and `writing-mode` (decision 3) — PR 6b.
- `<tref>` (decision 4) — dropped.
- `letter-spacing`/`word-spacing` for CSS reflow (decision 2) — SVG only.
- Kerning: no GPOS kerning-pair pass exists for simple scripts today
  (complex scripts get it via harfbuzz). Pre-existing gap, not widened here.
- `font-size-adjust` (1 fixture), `font-kerning` (3), `text-rendering` (5) —
  hints that do not change geometry in a pure-Go rasterizer; logged.

## Testing

- Shaping reuse: an SVG `<text>` and an equivalent HTML run produce identical
  glyph advances — proves `Shape` is genuinely shared, not forked.
- `text-anchor` start/middle/end against measured total advance.
- Per-character `x`/`y`/`dx`/`dy`/`rotate` lists, including the SVG rule that a
  short list applies to the first N characters and the rest inherit.
- `<tspan>` nesting: inherited position cursor, style override, and the
  absolute-vs-relative distinction.
- Text as a clip path and as a mask — the geometry-reuse claim.
- Bidi: an RTL `<text>` reorders (reusing `inline.Reorder`), proving the flat
  glyph slice works without `MakeVisualLine`.
- Stroke on text with a paint server, proving text is ordinary geometry.
- Every golden compared against **resvg's reference PNGs**, not eyeballed —
  that sweep has found real bugs in every PR of this series, including one in
  PR 5 that a subagent had reported as passing.
- Deferred features must log and degrade, and that must be covered by a test.
- No pre-existing golden may move.

**Font-availability caveat:** many resvg text fixtures specify fonts this repo
does not bundle. Fixtures whose reference PNG cannot be matched because of font
substitution are vendored only where the *geometry* claim (anchor, spacing,
positioning) is still verifiable; the rest are skipped with a recorded reason
rather than committing a golden that locks in a substituted-font rendering as
if it were correct.
