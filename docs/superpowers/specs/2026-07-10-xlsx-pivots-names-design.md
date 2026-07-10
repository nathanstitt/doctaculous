# pkg/xlsx pivot tables + defined-names write

Last of the five calc-adoption PRs — the pieces calc's save path needs beyond cells/styles/CF/
notes. Pivots follow Excel's own contract: the DEFINITION round-trips, the computed values do
not — the cache carries `refreshOnLoad="1"` with empty records, so the consumer recomputes on
open (this is also what excelize emits, and what calc relies on today).

## Pivot tables (pivots.go)

- **Model**: `PivotTable{Name, SourceSheet, SourceRange, TargetSheet, Location, Rows, Cols,
  Filters []string, Values []PivotValueField, Row/ColGrandTotals, StyleName}`; axis fields are
  source-column NAMES (resolved against the source range's header row), `PivotValueField{Field,
  Aggregation, DisplayName}` with the ST_DataConsolidateFunction vocabulary ("sum" default,
  "Sum of X"-style display names derived).
- **Read** — `PivotTables()` walks every sheet's rels for pivotTable parts, decodes the
  definition (axes by cacheField index, grand-total attrs defaulting TRUE per schema, style),
  and joins the cache definition (via the pivot part's rels, falling back to the workbook
  `pivotCaches` cacheId) for the source range/sheet and field names.
- **`RemovePivotTables()`** — the clean slate calc's authoritative save rebuilds onto (re-adding
  without removing would duplicate caches, excelize's known trap): drops pivot + cache parts and
  their rels, strips sheet pivot relationships, workbook `pivotCaches` + rels, and every
  content-type Override.
- **`AddPivotTable(pt)`** — full wiring in one call: cache definition (fields from the header
  row; blank headers get positional names), empty records, part rels, workbook `pivotCaches`
  entry + rel (next cacheId/rId past the current max), the table definition (pivotFields with
  axis/dataField marks, row/col/page/data field lists, `PivotStyleLight16` default), the target
  sheet's relationship, and three CT Overrides. Unknown field names are hard errors.
- Editor-core addition: `setPart` clears the deleted mark — a part removed and recreated in one
  session (remove-then-add is exactly calc's shape) emits again; previously Save skipped
  deleted originals even when dirty.

## Defined names

`SetDefinedNames` replaces the workbook's `definedNames` wholesale (schema-ordered insertion;
`localSheetId` from `LocalSheet`, `hidden`); `DefinedNames()` is the editor read view mirroring
the `Workbook.DefinedNames` reader field — calc's namedRanges can round-trip for the first time.

## Tests (pivots_test.go)

Round trip through our own reader (parts/rels/CT/workbook wiring asserted, refreshOnLoad
pinned, axes/values/display names/grand totals/style recovered); remove-then-add in one session
(removal leaves no part, wire, or content type behind; the re-add yields exactly one pivot);
unknown-field error; defined names against both views incl. sheet-local + hidden, replace and
clear. Real-world Excel-authored pivot preservation fixtures remain the standing follow-up
(testdata/external has only PDFs today).
