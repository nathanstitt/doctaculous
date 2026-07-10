# DOCX writer (pkg/render/docxwrite)

**Status:** implemented (core; tables + embedded images are the follow-up PR)
**Date:** 2026-07-09
**Author:** Nathan Stitt

## Goal

Make DOCX a conversion **output**: everything → .docx (HTML, Markdown, plain text, and
PDF via extraction), completing the any-to-any matrix from
`2026-07-09-unified-conversion-core-design.md`. Like the Markdown/HTML writers — and
unlike raster/pdfwrite — this is a STRUCTURE writer: it walks the shared cssbox tree
(reusing `pkg/render/internal/boxwalk`) and emits native Word constructs. It is
deliberately not layout-faithful (floats/positioning/geometry degrade to normal flow;
headers/footers and footnotes are out of scope).

## The round-trip contract

Every mapping is chosen so this repo's own DOCX reader reconstructs it — the reader is
the CI-enforced consumer (`docx.OpenBytes` in the writer's own unit tests, plus a parity
matrix: HTML → WriteDOCX → reopen → WriteMarkdown must equal HTML → WriteMarkdown).

| cssbox signal | WordprocessingML | Notes |
|---|---|---|
| `HeadingLvl` 1–6 | `w:pStyle HeadingN` + styles.xml rPr (`w:b`, `w:sz` 64/48/38/32/26/22 half-pt — the UA scale) | level rides on the style id (the reader's `paragraphSemantics`); the rPr carries the visuals |
| bold/italic/strike (via `boxwalk.CollectRuns`) | direct `w:b`/`w:i`/`w:strike` | the reader has no character-style cascade, so direct rPr is the carrier |
| SemTag `code` runs | `w:rStyle CodeChar` + direct monospace `w:rFonts` | reader addition: `w:rStyle` parsed (identity only) and CodeChar → SemTag "code" |
| `Href` runs | `w:hyperlink r:id` + an External relationship (URL-deduped) + direct link chrome (color/underline) | resolves through `Rels[RelID]` on the way back |
| SemTag `blockquote` | per-paragraph `w:pStyle Quote` | reader already maps quote → blockquote; multi-paragraph quotes reopen as adjacent single quotes (documented v1 limit) |
| SemTag `pre` | ONE `w:pStyle CodeBlock` paragraph, lines joined by `w:br` | `w:br` lowers to a preserved newline, so the block round-trips as one fenced block |
| SemTag `hr` | empty `w:pStyle HorizontalRule` paragraph (pBdr bottom for Word) | reader addition: horizontalrule → SemTag "hr" (pBdr itself is unparsed) |
| lists (`IsListContainer`/`Marker`) | `w:numPr` (`ilvl` = depth, ListParagraph style); bullets share numId 1 (one bullet abstract, glyph rotating •/◦/▪); each ordered list gets its own `w:num` over one decimal abstract so numbering restarts (reader counters are per-numId; Word needs the startOverride it emits) | marker prefix stripped from the item's leading run (numbering re-synthesizes it) |
| task-list items | `☐`/`☒` text prefix | one-way (no checkbox control in DOCX); documented |
| page geometry | body-final `w:sectPr` (pt × 20 twips; Letter default, 1in margin default) | |
| tables | follow-up PR (v1 logs + emits cell content as plain paragraphs) | |
| images | follow-up PR (v1 keeps alt text + logs) | |

**XML emission** is direct string assembly with explicit escapers (matching
pdfwrite/htmlwrite): `encoding/xml` cannot emit the prefixed `<w:p>` form real OOXML
consumers expect. `w:t` always carries `xml:space="preserve"`. The OPC container
(`archive/zip`) uses fixed part order and zero timestamps — output is deterministic
(byte-identical writes, pinned by test).

## Reader/lowering changes landed with this

1. **`w:rStyle` parsed** into `RunProps.StyleID` (identity only — no formatting
   cascade), threaded through `mergeRun`/`EffectiveRun`; `CodeChar`/`Code` marks the
   text box SemTag "code".
2. **`docxHeadingStyles` gains `codeblock → pre` and `horizontalrule → hr`.**
3. **List grouping fix (pre-existing defect exposed by the parity tests):** the DOCX
   lowering emitted list items as loose siblings of ordinary paragraphs — no list
   container — so the conversion writers' container detection misread any mixed body (a
   body with a list plus paragraphs dropped the paragraphs from Markdown output, and
   nested lists flattened). `lowerBlocks` now groups a run of consecutive numbered
   paragraphs into a container box, nested by `ilvl` (layout-neutral wrappers: CSS
   initial values, display:block, no spacing — all raster goldens byte-identical). This
   fixes Markdown/HTML conversion of real Word documents, not just the round trip.

## API + CLI

`(d *Document).WriteDOCX(ctx, out, DOCXOptions{PageWidthPt, PageHeightPt, MarginPt,
Logf})`, gated on the `reflowTree` seam (so PDF→DOCX works via lazy extraction);
`ConvertHTMLToDOCX` shim; DOCX output capability bit flipped; `ConvertOptions.DOCX` +
the `Document.Write` case. CLI: new `todocx` subcommand (PDF input first-class),
`convert` gains docx targets, `inferCommand` routes a `.docx` output to todocx.

## Testing

Writer unit tests parse their own output with the real reader and assert the model
(pStyle ids, run props + rStyle, hyperlink rels + External mode, numPr ilvl/numId
allocation, single-paragraph code blocks with break runs, sectPr twips, determinism,
valid empty package). The parity matrix covers headings/emphasis/code/links/bullet/
ordered/nested lists/blockquote/code block/hr; `TestMarkdownToDOCXAndBack` closes the
md→docx→md loop; `TestPDFToDOCX` proves the extraction path; `docxout-basic.png` golden
renders a reopened writer-produced document (eyeballed). The conversion-matrix test now
exercises every →DOCX cell with reopen-verification. CLI todocx/convert tests.

One-time external validation (opening a generated .docx in Word/LibreOffice) is
recorded in the PR — neither is installed on the dev machine, so this is a reviewer
step; the tables/images follow-up repeats it.
