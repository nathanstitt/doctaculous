# XLSX input (pkg/xlsx reader + frontend)

**Status:** implemented
**Date:** 2026-07-09
**Author:** Nathan Stitt

## Goal

Open .xlsx workbooks as conversion inputs: every visible sheet becomes a ruled table
(with the sheet name as a heading when more than one is visible) flowing through the
HTML pipeline — so xlsx → PDF/DOCX/HTML/Markdown/text/images/CSV all follow from the
sibling contract (`2026-07-09-unified-conversion-core-design.md`).

## Parser decision: hand-rolled, no new dependencies

**Cached values only** — Excel stores every formula cell's last-computed `<v>`; that is
what converters ship. No formula evaluation, no writing, no encrypted workbooks. Under
that scope a reader is bounded work, and the dep audit (2026-07-09, via
proxy.golang.org) ruled out the libraries:

- **excelize v2.11.0** (BSD-3): runtime requires `x/crypto` (encrypted workbooks),
  `richardlehane/mscfb` + `msoleps` (OLE compound files), `xuri/efp` (formula parser),
  `xuri/nfp` (number-format parser), `x/image`, `x/net`, `x/text`,
  `tiendc/go-deepcopy`. None are excludable and none are needed for cached-value reads.
- **tealeg/xlsx v3.3.13** (BSD-3): runtime requires an on-disk cell cache
  (`peterbourgon/diskv` + `google/btree`), `shabbyrobe/xmlwriter` (write path),
  `rogpeppe/fastuuid`, and lists profiling/test frameworks in its main require block.
- **xuri/nfp** (BSD-3, zero-dep) only tokenizes number-format codes — it does not render
  values, so it would not remove the hard part. It remains a candidate if full
  custom-format rendering is ever wanted.

House precedent decides it: the dependency-free `pkg/css` parser and the hand-rolled
`pkg/docx` reader, whose zip + streaming-`xml.Decoder` shape `pkg/xlsx` copies.

## Reader (`pkg/xlsx`)

- **Container**: `[Content_Types].xml` presence, workbook part via the `_rels/.rels`
  officeDocument relationship with the conventional `xl/workbook.xml` fallback,
  per-part 256 MiB cap, `ErrNotXLSX` sentinel — all mirroring `pkg/docx/zip.go`.
- **Workbook**: sheet list (name, r:id → part via `xl/_rels/workbook.xml.rels`,
  hidden/veryHidden state), `workbookPr date1904`.
- **Shared strings**: plain `si/t` plus rich-text `r/t` runs concatenated.
- **Styles**: `cellXfs` → numFmtId / fontId / fillId / horizontal alignment; fonts
  (b/i); solid `patternFill` fgColor; custom `numFmts`.
- **Sheets** (streaming decoder): `c/@r` A1 refs → zero-based grid, cell types
  s / str / inlineStr / b / e / numeric, `<f>` ignored (cached `<v>` is the value),
  `mergeCells` ranges. The used range materializes as a dense rectangular grid of
  display cells (Text, Bold, Italic, FillRGB, Align).
- **Values** (`value.go`): dates via builtin numFmtIds 14–22/45–47 plus a
  token-scanning heuristic for custom codes (quoted literals, escapes, and
  color/condition brackets excluded — `[Magenta]0.00` is not a date; elapsed `[h]`
  is); serials convert against the 1899-12-30 epoch (correct for everything after
  1900-03-01 despite the Lotus leap bug) or 1904-01-01 per `date1904`; rendered
  `2006-01-02` / `2006-01-02 15:04` / `15:04:05`. Numbers render as Excel "General"
  (shortest round-trip); percent codes scale ×100 + `%`; other custom codes degrade to
  General (the value is never lost). Booleans TRUE/FALSE; error cells keep their text.
- Hidden **sheets** are flagged (conversion skips them). Hidden rows/columns are
  rendered — a deliberate deviation from the draft plan: hiding is view state, the
  data is real, and dropping rows would shift merge coordinates.

## Frontend (`pkg/doctaculous/xlsx_frontend.go`)

`OpenXLSX`/`OpenXLSXFile`/`OpenXLSXBytes` synthesize HTML per visible sheet: optional
`<h2>` name, a ruled table over the used range, `mergeCells` → native
`colspan`/`rowspan` (origin carries the value, covered slots omitted), inline styles
for bold/italic/fill/alignment. **No first-row header assumption** — spreadsheets have
no header semantics; a bold first row is detected as a header row naturally by the
writers' existing bold-based detector (pinned by test). Detection: the ZIP classifier
generalized to OPC (`word/` → DOCX, `xl/` → XLSX, conventional part or
rels-redirected); `.xlsx`/`.xlsm` extensions.

## Testing

Deterministic fixtures via a new `testdata/gen/xlsx` builder (mirrors the DOCX
generator; no binary .xlsx committed): values / dates / styled / merged / multisheet.
Reader units (A1 refs incl. multi-letter columns, rich strings, cached-formula and
error cells, the date-code table incl. the `[Magenta]` non-date, both epochs, General
formatting); frontend tests (spans in `WriteHTML`, hidden sheets skipped, sheet
headings, bold-first-row-as-header); detection (real fixture, xl-shaped zips, pptx zip
→ Unknown); matrix row (xlsx → everything incl. CSV); `xlsx-specimen` raster golden
(eyeballed); CLI xlsx→md and extension-less sniffed xlsx→csv.
