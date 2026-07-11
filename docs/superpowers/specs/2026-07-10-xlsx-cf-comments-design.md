# pkg/xlsx conditional formats + cell notes

Fourth of five PRs on the calc adoption path. Conditional formatting is where the
preservation architecture pays off directly: calc edits the rule kinds it models and carries
the rest opaquely — here the opaque token is the VERBATIM `<cfRule>` XML, lossless by
construction (data bars, color scales, icon sets ride through byte-faithfully rather than as
a struct projection).

## Conditional formatting (cf.go)

- **Model**: `ConditionalFormatting{Ranges, Rules}` / `CFRule{Type, Operator, Formulas, Text,
  Priority, StopIfTrue, Style *Style, Raw []byte}` — Style is the resolved dxf; Raw the
  verbatim element.
- **Read** — one code path for BOTH views: `parseCF` walks the raw-fidelity tree, so
  `Workbook.Sheets[i].CondFmts` and the editor's `ConditionalFormats()` agree and Raw is
  byte-faithful everywhere. dxfIds resolve through the styles part's `<dxfs>` (parsed with
  the same named font/fill/border XML shapes as the main tables — refactored to share).
- **Write** — `SetConditionalFormats` replaces the sheet's blocks wholesale (the
  authoritative-save shape): a rule with Raw re-emits verbatim with only its priority
  renumbered; a typed rule synthesizes its element, minting (and deduping) a dxf from Style;
  priorities renumber 1..N across the sheet; blocks insert at the worksheet schema position
  via a placeholder anchor; an empty set removes everything.

## Cell notes (comments.go)

Classic notes with the full package wiring Excel requires:

- **Model**: `Comment{Row, Col, Author, Text}` — 1-based A1-space (a positioned annotation,
  matching the editor's coordinates, documented against the 0-based read grid).
- **Read**: the sheet rels' comments part → authors + commentList (plain and rich text
  concatenated); surfaced on `Workbook.Sheets[i].Comments` and the editor's `Comments()`.
- **Write**: `SetComment` (replace-at-cell semantics) / `RemoveComment` regenerate the
  comments part AND its legacy VML drawing wholesale from the note list — both are mechanical
  projections of it. First use wires everything: the comments part, the VML part, the sheet's
  rels (created if absent), the `[Content_Types]` Override + vml Default, and the sheet's
  `legacyDrawing` reference.
- Two editor-core fixes fell out: `sheetRelTarget` reads CURRENT bytes (a rel added earlier
  in the session resolves), and a new `File.setPart` replaces a part's content wholesale
  through the dirty machinery — an original zip entry can now be regenerated (previously the
  byte-verbatim copy of the original silently won over an added-map replacement).

## Tests (cf_comments_test.go)

CF read (typed fields, dxf resolution, Raw fidelity for a dataBar the editor does not model,
editor/reader agreement); the authoritative set (typed rule with a minted dxf + the opaque
dataBar re-emitted verbatim with renumbered priority; clearing removes all); notes round trip
(create-on-fresh-workbook with full wiring asserted part-by-part, reader + editor agreement,
replace-at-cell, remove, second-session edits); and surgical preservation (a note edit leaves
the other sheet/styles/workbook parts byte-identical).
