# DOCX → cssbox convergence (retire the flat engine)

**Status:** design
**Date:** 2026-07-02
**Sub-project:** the "converge later" item from `2026-06-23-html-rendering-design.md` §2.2 (there
labelled "sub-project 10"). This is the deferred DOCX reconciliation: it re-points DOCX lowering off
the flat `box.Document` model onto the recursive `cssbox` tree, then deletes the flat engine so **one
recursive engine drives every reflow format**.

## 1. Why now

When the HTML renderer began, DOCX was kept on its existing **flat** path deliberately — migrating a
shipping feature onto brand-new engine code on day one was rejected in favor of *late* convergence
with the DOCX goldens as a safety net (§2.2/§2.3 of the HTML design). The gate was: converge once the
CSS engine matches the flat engine on **normal flow + tables + lists**. The CSS engine now has all of
that and far more, so the gate is reachable and the flat engine is now pure duplicated risk.

## 2. Current state (the two paths today)

```
DOCX:  docx.Document ──▶ style.Resolver ──▶ lower.Document ──▶ box.Document ──▶ layout.Engine ─┐
                          (cascade)          (pkg/docx/lower)   (flat model)     (flow.go)      ├─▶ *layout.Pages ─▶ raster / pdfwrite
HTML:  html.Document ──▶ css cascade  ──▶ css.Build ──────────▶ cssbox.Box ───▶ css.Engine ────┘
                                            (box gen)          (recursive)      (block.go)
```

Both paths converge on `*layout.Pages`, consumed identically by `reflowRenderer`
(`pkg/doctaculous/reflow_backend.go`) for both raster and the PDF writer. **Only the middle
differs.** After convergence, DOCX joins the lower rail:

```
DOCX:  docx.Document ──▶ style.Resolver ──▶ docxcssbox.Lower ──▶ cssbox.Box ──▶ css.Engine ──▶ *layout.Pages
```

### 2.1 What the flat model actually carries

`box.Document` (`pkg/layout/box/box.go`, ~99 lines) is small and paragraph-only:

- **PageGeometry**: width/height + four margins, in points.
- **Block** (paragraph): `Inlines`, `Align` (left/center/right/justify), `LineHeight`
  (Auto×mult / Exact / AtLeast), `SpaceBefore/After`, `IndentLeft/Right`, `FirstLine` (incl. hanging),
  `BreakBefore`.
- **Inline** (run): `Text`, `FaceRef{Family,Bold,Italic}`, `SizePt`, `Color`, `Underline`, `ForceBreak`
  (hard break).

There are **no lists, tables, or images** in the flat model — those were never added (they sit in the
DOCX TODO). So the migration surface is exactly: paragraphs, runs, and page geometry. Everything the
flat model expresses maps cleanly onto `cssbox.Box` + `css.ComputedStyle`.

## 3. Approach (decided)

**Direct `cssbox` synthesis.** A new DOCX→cssbox lowering builds `*cssbox.Box` nodes directly and
writes resolved values into `css.ComputedStyle` fields. **No HTML serialization, no CSS re-parse.**
`cssbox` was designed for exactly this ("box generation produces it from HTML+CSS today, and DOCX
lowering converges onto it later" — package doc). `ComputedStyle` is already a *resolved* struct of
concrete values (`TextAlign string`, `Bold bool`, `MarginTop Length`, `FontSizePt float64`, …), so the
existing `style.Resolver` cascade output drops straight into it. Rejected alternative: emitting an
HTML string + stylesheet and calling `css.Build` — it forces DOCX concepts through HTML semantics,
adds a lossy round-trip, and couples DOCX to `pkg/html` for no benefit.

The existing `style.Resolver` (docDefaults → basedOn chain → direct cascade, with cycle guard) is
**kept unchanged** — it already produces `EffectiveParagraph`/`EffectiveRun`. Only the *sink* changes:
`box.Block`/`box.Inline` become `cssbox.Box`/`ComputedStyle`.

## 4. The mapping (flat model → cssbox + ComputedStyle)

Each DOCX paragraph becomes a **block box** whose children are **text boxes** (one per run). The
document root is a single block box (a synthetic `<body>` analogue) holding the paragraph blocks;
page geometry is threaded through `css.Engine.Layout` as it is for HTML (viewport width from the page
content box; page height via the paged path).

| Flat (`box`) | cssbox / ComputedStyle target |
|---|---|
| `Document.Page` (pt geometry) | `css.Engine.Layout` viewport width = content width; paged height from margins (see §4.2) |
| `Block` | `cssbox.Box{Kind: BoxBlock, Display: DisplayBlock, Formatting: InlineFC}` |
| `Block.Align` | `Style.TextAlign` = `"left"|"center"|"right"|"justify"` |
| `Block.SpaceBefore/AfterPt` | `Style.MarginTop/MarginBottom` = `Length{Px, pt}` |
| `Block.IndentLeft/RightPt` | `Style.MarginLeft/MarginRight` (block indent) |
| `Block.FirstLinePt` | `Style.TextIndent` (**new field — see §5**) |
| `Block.BreakBefore` | `Style.BreakBefore = "page"` |
| `Block.LineHeight` | `Style.LineHeight` (Length) — **mode gap, see §5** |
| `Inline` | `cssbox.Box{Kind: BoxText, Text: run.Text}` under a styled inline, or text box carrying run style |
| `Inline.Face/Size/Color/Bold/Italic` | `Style.FontFamily/FontSizePt/Color/Bold/Italic` |
| `Inline.Underline` | `Style.TextDecorationLine = "underline"` |
| `Inline.ForceBreak` (hard break) | a forced line break in the IFC — **mechanism gap, see §5** |

### 4.1 Run styling

A run's style lives on the text run, not the paragraph. Two options that both work with the current
IFC: (a) wrap each run's text box in a `BoxInline` carrying the run's `ComputedStyle`, or (b) put the
style directly on the `BoxText`. We use **(a) inline-wrapping** because it matches how the HTML box
generator feeds `gatherInlineRuns` (styled inline → text), so the IFC path is exercised identically
and there is no special-casing of styled `BoxText`. Runs with identical resolved style within a
paragraph may share one inline wrapper (an optimization, not required for correctness).

### 4.2 Page geometry

The flat engine reads geometry from `box.Document.Page`. The CSS engine takes a viewport width and
(for pages) a page height. DOCX has a single section (today), so: lay out at width = `pageW −
marginL − marginR`, paginate at height = `pageH − marginT − marginB`, and offset the page origin by
the top/left margins at raster time exactly as the flat `page.go` does. We reuse `WithPageSize`-style
paged layout (`LayoutPaged`) rather than the single-tall-page path, since DOCX has always paginated.

## 5. Engine vocabulary gaps (the only real risk)

Four flat-model concepts have no exact `ComputedStyle` equivalent today. Each needs a small,
*additive* engine addition (byte-identical for HTML, which never sets them):

1. **`LineHeight` Exact / AtLeast modes.** `box.LineHeight` has three modes (Auto×mult, Exact,
   AtLeast); `ComputedStyle.LineHeight` is a single `Length` with `UnitAuto`="normal". CSS `line-height`
   only models Auto (normal) and a fixed multiple/length — it has **no "at least" mode**. Fix: add the
   mode to the resolved line-height used by the IFC (`effectiveLineHeight`), fed by a new
   `ComputedStyle` field (e.g. `LineHeightMin Length` for AtLeast) or a small `LineHeightMode` enum
   parallel to the flat one. AtLeast is the DOCX default `lineRule` and must be preserved.
2. **The DOCX auto multiplier (×1.15).** The flat engine's `lineHeight()` applies ~1.15 for
   `LineHeightAuto`. The CSS engine's `autoLineHeight` was deliberately changed (fidelity fix E5) to
   `(ascent+descent)×1.15` *without* the font line gap. Confirm the DOCX auto path lands on the same
   number, or thread the DOCX multiplier explicitly. **This is the most likely source of golden
   diffs** and is expected (see §7).
3. **`FirstLinePt` / text-indent and hanging indent.** `ComputedStyle` has no `text-indent`. Add
   `TextIndent Length` (signed; negative = hanging) and honor it in the IFC's first-line placement.
   This is a genuinely missing CSS property (`text-indent`) worth having for HTML too.
4. **Hard line break (`ForceBreak`).** The flat IFC treats `ForceBreak` as a forced break. The CSS
   IFC breaks on `<br>` / preserved `\n` (via `white-space`). Simplest faithful mapping: lower a
   `ForceBreak` run to a text box whose content is a preserved newline in a `white-space: pre-line`
   context, **or** add an explicit hard-break box kind the IFC already understands. We use whichever
   the current inline core exposes for `<br>` (a hard-break run), so DOCX reuses the HTML mechanism.

These four are the complete gap list — the flat model carries nothing else. Each addition is gated by
"HTML output stays byte-identical" (the existing HTML/DOCX-independent goldens prove it).

## 6. Package changes

- **New:** `pkg/docx/cssbox` (or `pkg/docx/lower` repurposed) — `Lower(d, resolver) *cssbox.Box`.
  Lives outside `pkg/docx` to avoid the `pkg/docx/style` import cycle (same reason the flat `lower`
  package does).
- **Changed:** `pkg/doctaculous/reflow_backend.go` `docxDocument` — build a `cssbox.Box`, run
  `css.Engine.Layout` (paged) instead of `layout.Engine`. Output stays `*layout.Pages`, so
  `reflowRenderer`, raster, and `WritePDF` are untouched.
- **Additive engine:** `pkg/css` `ComputedStyle` gains `TextIndent` and the line-height "at least"
  vocabulary; `pkg/layout/css` IFC honors them.
- **Deleted (same PR, after goldens pass — §7):** `pkg/layout/box/`, `pkg/layout/flow.go`,
  `pkg/layout/flow_test.go`, the flat-only parts of `pkg/layout/page.go` and `pkg/layout/paint`, and
  the old `pkg/docx/lower` if a new package supersedes it. `layout.New`/`layout.Engine`/`Layout(...,
  box.Document)` go away. Anything the CSS engine still needs from `pkg/layout` (the `Pages`/`Item`
  types, `paint`) stays.

## 7. Acceptance gate (decided)

The four `docx-*` goldens (`docx-paragraph`, `docx-styled`, `docx-justify`, `docx-multipage` in
`pkg/doctaculous/testdata/golden/`) are the **regression oracle**, but the two engines will **not** be
pixel-identical (different line-height math, margin handling). Gate: **regenerate + eyeball each.** A
golden diff is treated as expected engine evolution — regenerate the `docx-*` PNGs, eyeball every
changed one in the PR, and accept if the rendering is correct or better. We do **not** bug-compatibly
replicate flat-engine quirks into the shared engine. (This matches the project's stated golden
discipline: an unexplained diff is a regression; an explained, eyeballed one is the new baseline.)

All existing **HTML** goldens/reftests and the **non-DOCX** corpus must stay **byte-identical** — the
engine additions in §5 are gated on that.

## 8. Flat-engine deletion (decided)

Delete the flat engine **in the same PR**, after the `docx-*` goldens are regenerated and approved.
The original design mandates deletion; doing it together avoids a lingering dead second engine and a
two-step limbo. Sequence within the PR: (1) engine vocab additions + new DOCX lowering, (2) re-point
`docxDocument`, (3) regenerate/eyeball `docx-*` goldens, (4) delete flat code once green.

## 9. Testing

- **Unit:** new `pkg/docx/cssbox` lowering tests — a paragraph/run/geometry fixture asserts the
  produced `cssbox.Box` tree (kinds, `ComputedStyle` fields), mirroring the existing `lower_test.go`
  structural assertions.
- **Engine unit:** `text-indent` (incl. hanging), line-height "at least", and hard-break tests in
  `pkg/layout/css`, each asserting HTML byte-identity where the feature is unused.
- **Golden:** `TestGolden`/`docx-*` regenerated and eyeballed (§7). The DOCX fixtures
  (`testdata/gen/docx`) are unchanged — same input, new engine.
- **Byte-identity:** an explicit assertion that a representative HTML page renders identically
  before/after the §5 additions (the additions are inert unless the new fields are set).
- **`-race`:** the DOCX path now shares the CSS engine's concurrent page fan-out; run `go test -race`.

## 10. Out of scope

- **DOCX lists/tables/images** (the DOCX TODO). Convergence is a *migration of existing DOCX features*
  onto the shared engine; it does not add new DOCX features. But once converged, those features are
  built **once** in the CSS engine (which already has tables/lists/images) and DOCX lowering just
  emits the corresponding `cssbox` — which is the entire payoff of converging. Adding them is
  follow-up work, unblocked by this.
- **Multi-section DOCX** (per-section geometry, headers/footers) — still single-section, as today.
- Any change to the PDF pipeline, `render.Device`, `pkg/font`, or the shared inline core.
