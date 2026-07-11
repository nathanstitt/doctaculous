// Package docx is a WordprocessingML (.docx) document model with a reader and
// a writer: Open/OpenBytes/OpenReaderAt parse a package into the exported
// model; Write/Bytes serialize a model back to a complete package. It is a
// SUPPORTED PUBLIC API consumed outside this repository — changes from here
// are additive only.
//
// The round-trip contract: Parse(Write(doc)) ≡ doc over the modeled
// vocabulary, with these documented normalizations (pinned by the write
// tests):
//
//   - a Target-only external Hyperlink gains an allocated relationship id;
//   - a Run carries text OR a break/reference (the parser splits combined
//     runs into that shape, so parse-shaped documents round-trip literally);
//   - a table cell always holds at least one block (an empty paragraph is
//     added on write — OOXML requires it);
//   - a zero SectionProps means Word's Letter defaults (what the parser
//     substitutes and the writer emits);
//   - a Style with an empty Type is a paragraph style.
//
// The modeled vocabulary: paragraphs (style/justify/spacing/indent/borders/
// tabs/frames/drop caps/page breaks/numbering), runs (bold/italic/underline/
// strike/size/color/family/highlight/shading/caps/vertAlign/character style),
// hyperlinks, inline and anchored drawings (extent, wrap, alignment, alt
// text), tables (grid, spans, vertical merges, borders with style names,
// shading, header rows, widths, fixed layout), tracked changes (w:ins/w:del
// containers and rPr/pPr/tcPr property-change before-states, cell ins/del
// marks), comments (part + range markers + reference runs), footnotes and
// endnotes, headers and footers, multi-section geometry, numbering
// definitions (formats, level text, level indents, start/override values),
// the style table, embedded media, and verbatim customXml parts
// (Document.ExtraParts — the escape hatch for app-owned data). Unknown
// constructs are dropped at parse time; the writer fails with
// ErrInvalidDocument rather than silently dropping content it cannot
// represent.
package docx
