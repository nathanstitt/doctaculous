# pkg/xlsx preservation-first editor core: Edit / New / Save

Second of five PRs on the calc adoption path (reader enrichment landed in
`2026-07-10-xlsx-reader-enrichment-design.md`; styles read-modify-write, conditional formats +
comments, and pivots + defined names follow). The architecture-shaping requirement from calc:
every save OVERLAYS the collaborative document's state onto the ORIGINAL xlsx bytes, and
everything the editor does not model — themes, drawings, x14 extension lists, unknown parts —
must survive verbatim. A from-scratch writer would silently destroy content; excelize (the
incumbent) rebinds touched parts through its structs and drops whatever they omit.

## The three layers

1. **Zip container** (`edit.go`): `Edit(data)` indexes the package; `Save`/`SaveTo` copies
   every untouched part **byte-verbatim** via `zip.Writer.Copy` (raw compressed streams —
   the no-op guarantee is part-for-part byte identity, original order preserved). Only parts
   an edit actually touched re-serialize; added parts append sorted with a fixed timestamp;
   dirty parts keep their original zip timestamps. Reads NEVER dirty: the parse cache
   (`parsed`) is separate from the dirty set, so a read-heavy pass (SheetNames, Cells, ...)
   still saves byte-identically.
2. **`internal/xmlpart`** — the raw-fidelity XML DOM for dirty parts, pinning our usage of
   **github.com/beevik/etree v1.6 (BSD-2, pure Go, zero deps — the new dependency; verified
   in source before adoption)**: namespace prefixes stay literal (`x14:cfRule` survives),
   attribute order and duplicates preserved, CDATA/comments/PIs/xml-decl kept, no
   reformatting; DOCTYPE rejected (never in OOXML; XXE hedge). Contract: SEMANTIC
   losslessness — the keystone test parses → serializes → reparses every part of a fixture
   loaded with unmodeled prefixed content and asserts tree equality; a fuzz target pins
   "malformed input errors, never panics". Schema-order insertion (`EnsureChildInOrder` with
   the CT_Worksheet/CT_Workbook sequences) places elements a part omitted where every
   consumer accepts them.
3. **Typed views** (`edit_sheet.go`): `SheetEdit` lazily parses its part on first MUTATION
   and edits nodes in place.

## Surface (rows/cols 1-based; File is single-goroutine)

- Workbook: `SheetNames`, `Sheet(name)`, `AddSheet` (part + workbook entry + rel + content
  type), `DeleteSheet` / `SetVisibility` (last-visible-sheet guards → `ErrLastVisibleSheet`),
  `MoveSheet`, `Date1904`. `New()` = a deterministic minimal blank workbook (the writer-first
  path; regenerates blank templates).
- Cells: `SetString` (inlineStr — sharedStrings stays untouched, keeping the preservation
  surface minimal), `SetNumber`, `SetBool`, `SetDate` (serial under the workbook's date
  system + ensures a date numFmt by CLONING the cell's xf with builtin id 14/22 — other
  facets ride along; dedups against an identical xf), **`SetFormula(row, col, src, cached
  Value)` — atomic `<f>`+`<v>`**, killing the incumbent's write-order trap; `ClearCell`
  (style kept — the delete-pass shape); `Cell`/`Cells` typed reads (shared strings resolved,
  dates classified through the styles tree — which may already carry this save's edits).
- Geometry/view: `SetMerges` (authoritative replace), `SetFrozen` (pane element with
  topLeftCell/activePane), `SetDimension`, `SetRowHeight`/`ClearRowHeight`,
  `SetColWidth`/`ClearColWidth` — a width set inside an existing `<col min..max>` range
  SPLITS the range so neighbors keep their widths; `SetTabColor`.
- First value/formula edit drops a stale `xl/calcChain.xml` with its content-type override
  and relationship (the Excel-sanctioned response; a stale chain can crash consumers).

## Tests (edit_test.go + xmlpart_test.go)

No-op byte-identity incl. after reads; surgical preservation (one cell edited → every other
part byte-identical; x14 extension payload and unknown row attributes survive inside the
edited part); typed writes round-tripped through the enriched reader; sheet ops with guard
errors; geometry set/read/clear symmetry; col-range splitting; determinism (identical edit
sequences ⇒ identical bytes); calcChain invalidation; New() opens and is deterministic.
Real-world Excel/LibreOffice preservation fixtures remain a follow-up (no such corpus is
committed yet); the generated fixtures carry deliberately unmodeled content in the interim.
