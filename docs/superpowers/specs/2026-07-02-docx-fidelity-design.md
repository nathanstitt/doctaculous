# DOCX fidelity pass — tables, lists, images, hyperlinks, parts & run properties

**Branch:** `feat/docx-fidelity` (off `main`). This is a large, multi-feature sub-project delivered as
**one design spec with a single phased implementation plan** (six ordered phases, each its own
branch → PR off `main` per the project's workflow, merged when CI is green).

**Builds on:** the DOCX reflow frontend (parse → style cascade → lower → layout) and — critically —
the **`cssbox` recursive layout engine**, which already implements the layout side of tables, lists,
floats, images, and positioning for the HTML pipeline. The **DOCX→`cssbox` convergence has landed on
`main`** (PR #32, "feat/docx-cssbox-convergence"): the flat `pkg/layout/box` engine is deleted, and DOCX
now renders through the CSS engine. The lowering lives in **`pkg/docx/cssbox`** (package `cssbox`, entry
point `Lower(d *docx.Document, r *style.Resolver) *cssbox.Box` — placed *outside* `pkg/docx` to avoid an
import cycle with `pkg/docx/style`). It resolves each paragraph/run through the DOCX style cascade and
emits **concrete `css.ComputedStyle` values, so nothing DOCX-specific crosses the boundary** — this pass
extends that same lowering. Read CLAUDE.md "Architecture", the convergence design/plan
(`docs/superpowers/specs/2026-07-02-docx-cssbox-convergence-design.md`,
`docs/superpowers/plans/2026-07-02-docx-cssbox-convergence.md`), and roadmap item 5 ("DOCX features").

**No sequencing dependency remains** — the convergence this pass depended on is already merged. Every
phase branches straight off `main`.

## Goal

Close the DOCX feature gap. Today `pkg/docx` parses only paragraphs + runs (bold/italic/underline,
size/color/family), `pStyle`, justification, spacing, indents, page-break-before, `w:br`/`w:tab`, and
single-section page geometry. A `.docx` containing a **table, a list, an image, or a hyperlink** loses
that structure entirely — `docx.Block` is `struct{ Paragraph *Paragraph }` and cannot represent
anything else, so `w:tbl`/`w:numPr`/`w:drawing`/`w:hyperlink` are dropped or flattened at parse time.

After this pass, a real-world `.docx` renders with its **tables, lists, images, hyperlinks, headers/
footers, footnotes, and richer run formatting** intact, laid out through the same battle-tested
`cssbox` engine the HTML side uses. Each previously-dropped feature becomes real output (the repo's "a
TODO becoming supported just turns the skip into real output" pattern).

## The central strategy: lower into `cssbox`, which already does the layout

The single most important fact shaping this design: **`cssbox` already has every layout capability DOCX
needs.** Its `DisplayKind` enumeration includes `DisplayTable`, `DisplayTableRowGroup`/`HeaderGroup`/
`FooterGroup`, `DisplayTableRow`, `DisplayTableColumn`/`ColumnGroup`, `DisplayTableCaption`,
`DisplayTableCell` (with `ColSpan`/`RowSpan`), `DisplayListItem` (with `Marker`/`MarkerContent`), and
`BoxReplaced` (with `ReplacedContent`) for images. The anonymous-table-box fixup (CSS 17.2.1), the
column-width solve (fixed + auto), colspan/rowspan distribution, cell-content recursion (a cell
establishes a BFC and lays out through the normal block/inline path), list-marker synthesis, and
replaced-image sizing/paint are **all implemented and tested** by the HTML pipeline.

Therefore this sub-project is overwhelmingly a **parse-and-lower** problem, not a layout-engine problem:

1. **Parse** the missing OOXML into a faithful, recursive `pkg/docx` model.
2. **Resolve** the new OPC parts (numbering, media, headers/footers, footnotes, settings) via the
   existing relationship machinery.
3. **Lower** each new structure — extending `pkg/docx/cssbox`'s existing `Lower`/`lowerParagraph`/
   `runTextBox` — into the corresponding `cssbox.Box` subtree with the right `Display`, `ComputedStyle`,
   `Marker`, and `Replaced` fields (concrete `css.ComputedStyle` string values, matching the current
   lowering) — the same subtree HTML box-generation emits.

The layout engine, the anonymous-box fixups, spans, column solve, and cell recursion take over from
there. DOCX tables get the *same* engine HTML tables use, for free.

## Architectural principle: the `docx` parse model is the semantic hub

DOCX → **any** (primarily HTML, and the paused Markdown converter) is the next sub-project after this
one. That imposes a design principle worth stating explicitly now, because it governs where fidelity
must live:

- The **`pkg/docx` parse model is the semantic source of truth.** It retains document *meaning* —
  "this paragraph is Heading 2", "this run is a hyperlink to `href`", "this is a list item at level 1
  of numbering N" — not merely the visual result.
- **Rendering** lowers the `docx` model → `cssbox` (a *layout* model that thinks in `Display` +
  `ComputedStyle`). This path is for rasterizing and the PDF writer.
- **Conversion** (future) lowers the *same* `docx` model → a semantic content tree → HTML/Markdown. It
  reads `pkg/docx` directly and does **not** go through `cssbox` (a layout model cannot cleanly
  reconstruct "Heading 2" from "18pt bold block").
- The two paths are **siblings**: neither is built on the other, and both consume the `docx` model.

Consequence for this spec: the parse model must be **semantic and faithful** (keep `pStyle` IDs, the
resolved heading level, hyperlink targets, list numId/ilvl), not just "enough to place pixels." This
pass already does that; the principle simply makes it a requirement rather than an accident.

## Scope

**In scope** — everything from the DOCX gap audit **except bidi and embedded fonts**:

### Tier 1 — structural blocks (drive the recursive-model refactor)
- **Tables** (full): `w:tbl`, `w:tr`, `w:tc`, `w:tblGrid` (column widths), `gridSpan` (horizontal
  span), `vMerge` restart/continue (vertical span / row spanning), table/row/cell borders (`w:tblBorders`/
  `w:tcBorders`), shading (`w:shd`), cell margins (`w:tblCellMar`), table width/alignment (`w:tblW`/
  `w:jc`), and **cell content recursion** (a cell holds `[]Block` — paragraphs, nested tables, lists).
- **Lists / numbering**: `numbering.xml` (abstract-num + num instances), `w:numPr` (numId/ilvl),
  per-level counters, marker format (`w:numFmt`: decimal/roman/alpha/bullet) and marker text
  (`w:lvlText`), level inheritance, nesting by `ilvl`.
- **Images**: `w:drawing` → `wp:inline`/`wp:anchor` → `a:blip` r:embed → `word/media/*`; EMU extent →
  used size; PNG/JPEG/GIF decode (reusing the replaced-image decode path).
- **Hyperlinks**: `w:hyperlink` (r:id → external target via rels, or w:anchor → internal bookmark),
  wrapping runs; the resolved href is retained on the model.

### Tier 2 — document parts / sections
- **Headers / footers**: `header*.xml` / `footer*.xml` parts (default/even/first), lowered into the
  page margin-band mechanism the paged-media engine already provides.
- **Multi-section**: multiple `w:sectPr` (section breaks), each with its own geometry.
- **Footnotes / endnotes**: `footnotes.xml` / `endnotes.xml` + `w:footnoteReference`.
- **settings.xml**: document-level settings that affect rendering (e.g. default tab stop `w:defaultTabStop`).

### Tier 3 — run / paragraph properties (additive to existing structs + `ComputedStyle` mapping)
- **Strikethrough** (`w:strike` / `w:dstrike`) → `text-decoration: line-through`.
- **Super/subscript** (`w:vertAlign` superscript/subscript) → `vertical-align` + size adjust.
- **Highlight / shading** (`w:highlight` / run `w:shd`) → `background-color`.
- **Caps / small-caps** (`w:caps` / `w:smallCaps`) → `text-transform` / small-caps rendering.
- **Underline styles & color** (currently only on/off is read) → richer `text-decoration`.
- **Tab-stop definitions** (`w:tabs` — the stop positions; the `w:tab` character is already read).

**Out of scope** (explicitly, with rationale):
- **Bidi / RTL** (`w:bidi` / `w:rtl`) — consistent with the engine's project-wide no-bidi stance
  (HTML tables/grid/flex all defer RTL). Parsed-and-ignored, LTR always, logged.
- **Embedded fonts** (`word/fonts/*` de-obfuscation, `fontTable.xml`) — the existing family-name +
  style → close-bundled-substitute resolution already picks a reasonable face (Calibri→TeX Gyre Heros,
  Cambria→Termes, etc.), so documents render acceptably without it. Real weighted (bold/italic) faces
  remain **roadmap item 4** (a font-corpus problem, orthogonal to document structure); until then
  bold/italic map to the regular substitute exactly as today.

## The foundational refactor: a recursive block model

`docx.Block` today is `struct{ Paragraph *Paragraph }` — a block is a paragraph or nothing. Tables (and
cell-content recursion) require a proper recursive sum type. This refactor is **Phase 1** and is the
backbone every Tier-1 feature builds on:

```go
// Block is a top-level flow item: a paragraph or a table (extensible).
type Block struct {
    Paragraph *Paragraph // w:p
    Table     *Table     // w:tbl
}

type Table struct {
    Grid  []Twips     // w:tblGrid column widths
    Rows  []TableRow
    Props TableProps  // borders, shading, cell margins, width, alignment
}

type TableRow struct {
    Cells []TableCell
    Props RowProps    // row height, header-row (tblHeader), can't-split
}

type TableCell struct {
    Blocks   []Block    // ← recursion: cells hold blocks (paragraphs, nested tables, lists)
    GridSpan int        // w:gridSpan (horizontal span; default 1)
    VMerge   VMergeKind // VMergeNone | VMergeRestart | VMergeContinue (vertical span)
    Props    CellProps  // borders, shading, width, vertical-align, cell margins
}
```

Paragraphs also gain inline structure for hyperlinks and images. Rather than a flat `[]Run`, a
paragraph's content becomes a sequence that can carry hyperlink grouping and drawings:

```go
type Paragraph struct {
    Props   ParagraphProps
    Content []ParaChild // runs, hyperlink groups, drawings, in document order
}
```

where `ParaChild` distinguishes a bare `Run`, a `Hyperlink{Target string; Runs []Run}` group, and a
`Drawing{RelID string; ExtentEMU (w,h int64); ...}` (an inline image). List membership is carried on
`ParagraphProps` (`NumID`, `ILvl`) since numbering is a paragraph property (`w:numPr`).

**Phase 1 lands the refactor with no new features** — the parser and `pkg/docx/cssbox`'s existing
`Lower`/`lowerParagraph` are re-expressed over the new `Block` sum type / `Paragraph.Content`, and
**all existing DOCX goldens stay byte-identical**. Tables/lists/images/hyperlinks then fill in the new
vocabulary in later phases.

## The lowering path (DOCX → `cssbox`)

Each new feature lowers to the `cssbox` subtree that HTML box-generation already emits, so the layout
engine sees familiar input:

| DOCX feature | `cssbox` lowering |
|---|---|
| `w:tbl` | `Box{Display: DisplayTable}` → `DisplayTableRow` per row → `DisplayTableCell` per cell (`ColSpan` from `gridSpan`, `RowSpan` from `vMerge` restart/continue runs); `tblGrid` → `DisplayTableColumn` widths; borders/shading/margins → `ComputedStyle`. The existing anonymous-table-box fixup, column solve, and span handling take over. Cell `[]Block` lowers recursively (cells establish a BFC). |
| `w:numPr` + `numbering.xml` | `Box{Display: DisplayListItem, Marker: {glyph/number}}`; counter value resolved from the abstract-num level (format + lvlText); nesting by `ilvl`. Reuses the CSS list-marker paint path. |
| `w:drawing` → `a:blip` | `Box{Kind: BoxReplaced, Replaced: {decoded image}}`, sized from EMU extent (914400 EMU = 1in). Reuses the replaced-image sizing/paint path. |
| `w:hyperlink` | an inline `Box{Kind: BoxInline}` carrying the resolved href (retained for conversion; the raster path may underline+color it, or ignore it). |
| headers / footers | lowered into the paged-media margin-band mechanism (per-section, default/even/first). |
| Tier-3 props | additions to the run-props → `ComputedStyle` mapping, extending the existing `runTextBox` (which already maps family/bold/italic/size/color/underline): strike → `text-decoration: line-through`, `vertAlign` → super/sub, highlight → `background-color`, caps → `text-transform`, etc. Requires widening `style.EffectiveRun` (today only Bold/Italic/Underline/Size/Color/Family) + the parser's `applyRPrChild`. |

**Byte-identity constraint** (CLAUDE.md): every existing DOCX golden stays pixel-identical until the new
lowering emits new vocabulary. New features light up **only** for documents that use them; a
paragraph-only `.docx` produces exactly the `cssbox` tree it does today.

## New OPC parts to resolve

Via the existing relationship mechanism (`zip.go` / `parse.go` `relsForByType`):
`numbering.xml`, `word/media/*` (image binaries), `header*.xml` / `footer*.xml`, `footnotes.xml` /
`endnotes.xml`, `settings.xml`. Each is resolved through the part's `.rels`, falling back to the
conventional path, mirroring how `styles.xml` is located today. (`word/fonts/*` and `fontTable.xml` are
**not** resolved — embedded fonts are out of scope.)

## Phased implementation plan (each phase = fixture + golden + green CI)

1. **Recursive block-model refactor** — `Block` sum type, `Paragraph.Content`/`ParaChild`, cell
   recursion plumbing. **No new features; all existing goldens unchanged.** Re-point lowering onto the
   new types.
2. **Tables** — parse `w:tbl` (grid, rows, cells, `gridSpan`, `vMerge`, borders, shading, margins,
   width/alignment) → lower to `cssbox` table → new generated fixture + `docx-table*` golden. Add a
   table to the DOCX showcase.
3. **Lists / numbering** — resolve `numbering.xml`, parse `w:numPr` → `DisplayListItem` + markers,
   nesting by `ilvl` → fixture + golden.
4. **Images + hyperlinks** — `w:drawing` → media decode → `BoxReplaced`; `w:hyperlink` → rels-resolved
   inline link → fixtures + goldens.
5. **Parts / sections** — headers/footers, multi-section, footnotes/endnotes, settings.xml → fixtures +
   goldens (margin-band content).
6. **Tier-3 properties** — strike, super/subscript, highlight/shading, caps/small-caps, underline
   styles/color, tab-stop definitions → fixture(s) + golden(s).

## Error handling & graceful degradation

Never panic on malformed input (CLAUDE.md). Each unsupported or malformed case degrades and is logged:
- A malformed `w:tbl` (missing grid, ragged rows, dangling `vMerge continue`) yields the best-effort
  grid the anonymous-table-box fixup can repair, or an empty/zero-size table — no crash.
- A missing/undecodable `word/media/*` image degrades to a sized placeholder (reserve the box, paint
  nothing) + debug log — exactly the HTML `<img>` degradation.
- A `w:numPr` referencing an absent `numId`/level falls back to a plain paragraph (or a default bullet)
  + log.
- A hyperlink `r:id` with no matching relationship becomes plain (unlinked) text + log.
- Bidi/RTL and embedded fonts are parsed-and-ignored + logged (documented deferrals).
- Recovery remains at the page boundary (the reflow engine already recovers in `css.Engine`/`layout`).

## Testing (per the project's corpus discipline)

- **Every layer, every feature**: unit tests on the parse model (new structs, `gridSpan`/`vMerge`
  resolution, numbering-level inheritance, rels resolution), and a hermetic, deterministically-generated
  `.docx` fixture per feature in `testdata/gen/docx` (one per feature so failures localize:
  `docx-table`, `docx-table-spans`, `docx-list`, `docx-nested-list`, `docx-image`, `docx-hyperlink`,
  `docx-header-footer`, `docx-footnote`, `docx-multisection`, `docx-run-props`).
- **Golden images** (`pkg/doctaculous` `docx-*`): render each fixture's page(s) and compare to committed
  PNGs; regenerate + eyeball on intentional changes.
- **Showcase**: add tables and lists (at least) to the DOCX visual corpus so they are exercised
  end-to-end.
- **Byte-identity**: assert the existing DOCX goldens are unchanged after Phase 1 (the refactor) and
  after every phase for documents that don't use the new feature.
- Hermetic and fast: no network; fixtures generated in Go (committed real `.docx` allowed only where
  generation is impractical, provenance noted).

## Non-goals / follow-ups

- **Bidi/RTL** and **embedded fonts** (see Scope) — deferred with rationale.
- **DOCX → HTML / Markdown conversion** — the next sub-project; this pass is its foundation (the
  semantic-hub principle). Not built here.
- **Weighted base-14 substitutes** (real bold/italic faces) — roadmap item 4.
- Exact Word layout parity for advanced table features (e.g. `w:tblLayout` fixed vs autofit nuances
  beyond what `cssbox` already models, floating/wrapped tables `w:tblpPr`) — approximated via the
  `cssbox` table algorithm; refinements are follow-ups, each degrading gracefully.
