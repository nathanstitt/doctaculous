# pkg/xlsx reader enrichment: typed values, formulas, full styles, sheet facts

First of five PRs on the tinycld-calc adoption path (the plan's work area C): pkg/xlsx grows
from a display-string reader into the structured read side a spreadsheet application imports
through — while the rendering path stays byte-identical (`Cell.Text` and the conversion goldens
are untouched; the display formatter keeps its deliberate numFmt subset). The
preservation-first EDITOR lands next (C2, `xlsx.Edit`/`New`/`Save` on beevik/etree).

## Model additions (all additive)

- **`Cell.Value`** — typed cached value: `Kind` (Empty/String/Number/Bool/Date/Error) with the
  matching field; a date is a numeric serial whose resolved format is a date/time code (`F`
  keeps the raw serial, `T` the UTC conversion via the shared 1900-Lotus-safe/1904 epoch
  logic, now extracted as `serialToTime`). Fixes the class of bug calc has under excelize
  today, where formatted strings ("1,234.50") silently degrade typed numbers to text.
- **`Cell.Formula`** — the source without "=". **Shared formulas are expanded**: each member
  carries the group master with relative references shifted to its position (`shiftFormula` —
  a lexical A1 pass matching Excel's semantics: `$` anchors fixed, string literals and quoted
  sheet names opaque, a trailing `(` marks a function call not a reference; bare `SUM2020` IS
  a reference, column SUM < XFD).
- **`Cell.StyleID` + `Cell.Style`** — the xf index and the fully resolved `Style`, shared per
  xf: `Font` (bold/italic/strike/underline-with-bare-`<u/>`-as-single/size/name/color), `Fill`
  (patternType + fg/bg), `Alignment` (h/v/wrap/indent/rotation/shrink), `Border` (four edges +
  diagonal with up/down flags; OOXML style names), `NumFmtID` + resolved `NumFmt` pattern,
  `Protection` (OOXML default locked=true when the element appears). `Color` carries whichever
  scheme the file used — RGB, indexed, or theme+tint (visible instead of silently lost).
- **Sheet facts**: `Visibility` (Visible/Hidden/VeryHidden — `Hidden` stays true for both
  hidden states), `TabColorRGB`, `FrozenRows/FrozenCols` (pane state frozen/frozenSplit),
  sparse `RowHeights` (points) / `RowStyles` (customFormat rows) / `ColWidths` (char units,
  `<col>` ranges expanded per column, span-bounded), `DefaultRowHeight/DefaultColWidth`.
- **Workbook facts**: `Date1904`, `DefinedNames` (name, refersTo, localSheetId scope, hidden).
- **Coordinate helpers** (1-based, the spreadsheet convention; the grid stays 0-based):
  `CellRef`, `ParseCellRef` ($-anchor tolerant), `ColumnName`, `Range`/`ParseRange`/`String`.
- **`builtinNumFmtAll`** — the complete ECMA-376 §18.8.30 builtin id table, powering
  `Style.NumFmt` resolution and date typing (and, later, the editor's code→id reuse); the
  display formatter still consults only its original subset so rendered text never changes.

## Tests

`pkg/xlsx/enrich_test.go` over a full-vocabulary generated fixture (the gen/xlsx builder
gained `SetDefinedNames`): typed values incl. the date conversion and style-only padding
cells; plain/shared/anchored formulas (member A4 gets "A4*2" from master "A3*2"); every style
facet incl. theme-color tint, bare-underline default, builtin-table resolution, and the legacy
display facts unchanged; sheet facts (tab color, panes, defaults, sparse dims); defined names;
coordinate helpers; and a shiftFormula table with the lexer edge cases. All existing goldens
and conversion tests pass byte-identical.
