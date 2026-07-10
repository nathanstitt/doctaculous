# CSV/TSV input & output

**Status:** implemented
**Date:** 2026-07-09
**Author:** Nathan Stitt

## Goal

The spreadsheet family's first slice, in both directions: CSV/TSV → everything (PDF,
DOCX, HTML, Markdown, text, images) and everything → CSV/TSV. Two more siblings of the
unified conversion core (`2026-07-09-unified-conversion-core-design.md`).

## Design

### Input (`pkg/doctaculous/csv_frontend.go` — no new package, no new deps)

`OpenCSV`/`OpenCSVFile`/`OpenCSVBytes` + `OpenTSV*` siblings. Stdlib `encoding/csv`
tuned for real-world files: `FieldsPerRecord = -1` (ragged rows tolerated, padded to
the widest row so the table stays rectangular), `LazyQuotes = true`, UTF-8 BOM
stripped, CRLF handled by the reader. The rows synthesize an HTML `<table>` — **first
row as the header row** (`<th>`), the convention that makes Markdown/DOCX table output
read correctly — with a ruled collapsed-border stylesheet (the Markdown frontend's
table look), then flow through `OpenHTMLBytes` like every other reflow frontend (all
`HTMLOption`s apply). CSV and TSV are distinct `Format`s (the delimiter is semantic),
so csv ⇄ tsv are real conversions; both are extension-only in detection (no content
magic — the hint step already outranks HTML sniffing, same as md/txt).

### Output (`pkg/render/csvwrite`)

A tables-only structure writer: **a spreadsheet is a grid, so the document's tables
are the content.** Tables emit in document order separated by one blank line;
non-table blocks are dropped with a logged count, and a table-less document produces
empty output plus a loud log (honest, not surprising-empty). Spanned cells are
duplicated into every covered slot via `boxwalk.BuildOccupancyGrid` (CSV is
rectangular — the GFM strategy); cell text renders exactly like the Markdown writer's
`renderCell` (per-block collapsed inline strings joined by a space, images contribute
alt text). Quoting comes free from stdlib `encoding/csv.Writer`; `Comma: '\t'` is TSV.
`WriteCSV`/`WriteTSV` gate on the `reflowTree` seam, so **PDF → CSV extracts the
tables the lattice/stream recognizer recovers** — the standout path, pinned by test.

### Wiring

`FormatCSV`/`FormatTSV` (+ the `FormatXLSX` enum, both bits still false until its
reader/writer PRs), `ParseFormat`/`FormatFromPath` entries, `openDetected` cases via
`openReflowFrontend`, `ConvertOptions.CSV CSVOptions`, `Document.Write` cases, CLI
usage/`--from`/`--to` text, and `inferCommand`: `.csv/.tsv/.xlsx` outputs route to the
`convert` verb (new dispatch case in `run()`).

## Testing

Exact-string csv → Markdown table; parsing quirks (quoted commas, embedded newlines,
ragged padding, BOM, CRLF, literal markup); csv ⇄ tsv byte round trip; writer units
(span duplication, multi-table separation, TSV delimiter, multi-block cells, no-tables
and dropped-prose logging); PDF → CSV lattice extraction incl. prose-dropped logging;
conversion-matrix rows/columns (tables-only outputs from table-less fixtures are
empty by design); `csv-specimen` raster golden (eyeballed); CLI csv→md, html→csv, and
inference tests.
