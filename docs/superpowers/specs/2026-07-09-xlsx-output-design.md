# XLSX output (pkg/render/xlsxwrite)

**Status:** implemented
**Date:** 2026-07-09
**Author:** Nathan Stitt

## Goal

Complete the spreadsheet family: everything → .xlsx. Like the CSV writer
(`2026-07-09-csv-tsv-io-design.md`) this is a **tables-only** structure writer — a
spreadsheet is a grid — with one worksheet per table. PDF tables recovered by the
lattice/stream extractor land as workbook sheets.

## Design

- **One worksheet per table**, named from the table's caption when present (Excel's
  forbidden characters become spaces, 31-char limit, duplicates gain a numeric suffix)
  else "Table N". Non-table content drops with a logged count; a table-less document
  writes a workbook with one empty sheet (Excel requires at least one) plus a loud log.
- **Cells**: inline strings (`t="inlineStr"` — no sharedStrings table needed); a cell
  whose text is a *clean* number (its General rendering reproduces the text exactly, so
  `007` stays a string) emits as a numeric cell, which is what makes csv → xlsx → csv
  byte-identical and lets spreadsheets compute over converted data. Header-row cells
  (the writers' bold-based detector) reference a bold `xf` in the fixed styles part —
  which is exactly what the XLSX *reader* detects as a header row on the way back.
- **Spans**: via `boxwalk.BuildOccupancyGrid` — the origin cell carries the value,
  covered slots stay absent, and each spanning cell adds a `mergeCells` ref (the native
  representation, unlike CSV's duplicate-content expansion).
- **Sharing with the CSV writer**: the tables-only walk (`boxwalk.CollectTables`) and
  the plain cell-text renderer (`boxwalk.CellPlainText`, mirroring the Markdown
  writer's renderCell) were promoted from csvwrite into the shared boxwalk package.
- **OPC assembly** mirrors docxwrite/the fixture builders: hand-built XML, fixed part
  order and zip timestamps — two writes of the same tree are byte-identical (pinned).
- Wiring per the sibling contract: capability bit, `ConvertOptions.XLSX XLSXOptions`,
  `Document.Write` case, `WriteXLSX` on the `reflowTree` seam (PDF → XLSX works via
  lazy extraction), CLI `--to`/usage text. xlsx → xlsx stays a same-format rejection.
- Known v1 punts: cell alignment/fills are not written back (bold headers only), and
  date-looking strings stay text (typed date cells are a follow-up).

## Testing

Round-trip parity through this repo's own `pkg/xlsx` reader — HTML → WriteXLSX →
reopen → WriteMarkdown equals HTML → WriteMarkdown for simple/colspan/rowspan tables;
multi-table + caption sheet naming (sanitization included); **csv → xlsx → csv
byte-identity**; pdf → xlsx extraction; determinism; the table-less empty-workbook
contract (reopens, logs, no content); conversion-matrix column with reopen
verification for every input; CLI html → xlsx e2e. One-time external check (Excel/
LibreOffice/Google Sheets) is a reviewer step — nothing is installed on the dev
machine.
