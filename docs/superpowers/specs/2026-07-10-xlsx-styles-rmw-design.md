# pkg/xlsx style read-modify-write: PatchCellStyle + numFmt allocation

Third of five PRs on the calc adoption path (editor core:
`2026-07-10-xlsx-editor-core-design.md`). This is the patch-not-replace contract a
collaborative spreadsheet's style overlay depends on: only the leaves the application tracks
land; every other facet of the cell's existing style — diagonal borders, indent, text
rotation, protection, theme colors, unknown style XML like a font `scheme` — rides along,
because the patch CLONES the cell's existing style records and edits nodes, never rebinding
through structs.

## API (pkg/xlsx/edit_styles.go)

- **`StylePatch`** — all-pointer leaves mirroring the application-overlay shape:
  `Font{Bold/Italic/Strike *bool, Underline/Name/Color *string, Size *float64}`,
  `Fill{Pattern/Fg/Bg *string}` (`Pattern` pointer-to-"" clears the fill),
  `Alignment{Horizontal/Vertical *string, WrapText *bool}`,
  `Border{Top/Right/Bottom/Left *EdgePatch{Style/Color *string, Clear bool}}` (Clear is the
  explicit edge delete), `NumFmt *string` (pointer-to-"" clears to General/id 0).
- **`PatchCellStyle(row, col, patch)`** — the calc primitive: clone the cell's xf and its
  referenced font/fill/border records, edit only the named leaves, dedupe each record back
  into its table (`xmlpart.Equal` — semantic node comparison, promoted from the test helper),
  retarget the cell's `s=`. An all-nil patch, or an identical-result patch, changes nothing.
- **`SetCellStyle(row, col, Style)`** — the whole-style REPLACEMENT setter (builds records
  from scratch from the full `Style` vocabulary incl. diagonal edges, indexed/theme colors,
  protection; documented as not carrying unmodeled facets — use Patch to overlay).
- **`CellStyle(row, col) Style`** — resolved reads through the possibly-edited styles tree,
  memoized against a mutation generation counter (`resolvedStyles`).
- **Row variants**: `RowStyle`/`PatchRowStyle`/`SetRowStyle`/`ClearRowStyle`
  (`customFormat="1"` + row `s=`).
- **numFmt allocation** (`numFmtIDFor`): exact builtin-table match reuses the builtin id
  (deterministic ascending scan — `"0.00%"` → 10, `"0.00"` → 2); an existing custom entry
  with the same code is reused; else `max(existing, 163)+1` is allocated and appended to
  `<numFmts>` (created at its schema position when absent).

New style records insert at schema positions (CT_Stylesheet / CT_Font / CT_Border orders);
`applyNumberFormat`/`applyFont`/`applyFill`/`applyBorder`/`applyAlignment` flags are set the
way producers expect.

## Tests (edit_styles_test.go)

- **Canary audit** — the calc-parity mirror: every patch leaf applied alone to a fresh
  workbook, asserted through BOTH the editor-side read and a full Save → reader round trip.
- **Clears**: `*""` leaves, border `Clear`, wrap false, numFmt back to id 0.
- **Unmodeled-facet preservation** — the case struct rebinding cannot express: a cell whose
  xf carries diagonal borders, indent=3, textRotation=45, protection, and a font `scheme`
  child keeps ALL of them through a `Font.Bold` patch (asserted via the reader and the raw
  styles XML).
- **Dedupe**: identical patches on two cells share one xf and one font record; re-patching
  the same cell is a no-op.
- **Row styles** end to end; **whole-style setter** across the full vocabulary (builtin
  "0.00" reused as id 2); **custom-code reuse** across allocations (one shared id ≥ 164).
