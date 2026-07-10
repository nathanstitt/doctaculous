# RTF input (pkg/rtf): Rich Text Format through the reflow pipeline

First half of the RTF format (the plan's area F; the writer `pkg/render/rtfwrite` is F2).
User-directed scope: full support, both directions — this PR lands the reader so drive's
search-text extraction keeps covering .rtf uploads after core/textextract retires, and .rtf
becomes a first-class conversion input (rtf → pdf/docx/md/... all follow from the reflow
pipeline).

## Architecture

`pkg/rtf` is a dependency-free hand-rolled reader in the CSV/XLSX-frontend shape: tokenize →
convert to HTML → `OpenHTMLBytes`, so every `HTMLOption` applies and every output format
follows. Two layers:

- **Tokenizer** (`token.go`): control words with signed parameters and the delimiter-space
  rule, control symbols, groups, `\'hh` code-page bytes (full cp1252 high-range table), raw
  newlines skipped. Malformed input never panics; a truncated escape drops.
- **Converter** (`rtf.go`): a group-state stack (character + paragraph formatting scoped by
  `{}`), destination tracking, and HTML block assembly. The RTF resilience rule is the
  degrade story: unknown control words are skipped, unknown `{\*...}` destinations are
  ignored wholesale (pinned: skipped destinations leak nothing).

## Vocabulary

Paragraphs (`\par`, `\pard`, `\line`, alignment `\ql/\qc/\qr/\qj`, indents `\li/\fi`, spacing
`\sb/\sa`); character formatting (`\plain`, `\b/\i/\ul/\ulnone/\strike` with 0-toggles, `\fs`
half-points, `\cf`/`\highlight` via the color table, `\f` via the font table); font and color
tables; special characters (`\bullet`, dashes, smart quotes, `\~`, `\_`, `\-`); Unicode
(`\uN` with `\ucN` fallback skipping — including fallback bytes split across text and `\'hh`
tokens); hyperlink fields (`{\field{\*\fldinst HYPERLINK ...}{\fldrslt ...}}` — the one
ignorable destination the converter understands); tables (`\trowd/\cellx/\intbl/\cell/\row` →
bordered HTML tables, multiple paragraphs per cell); embedded pictures (`\pngblip`/`\jpegblip`
hex → data: URI `<img>` at `\picwgoal/\pichgoal` size; other formats logged + skipped); page
geometry (`\paperw/\paperh/\marg*` → an `@page` rule, RTF's implicit margin defaults applied).

## Cross-cutting fix: data: URIs decode without a loader

RTF pictures embed as data: URIs, which exposed a real engine gap: `imageCache.decode`
required a configured `ResourceLoader` even for data: refs, so a plain HTML file with an
inline image rendered a placeholder. Browser-faithfully, a data: URI carries its own bytes —
`resource.LoadDataURL` (the exported RFC 2397 decoder) now short-circuits in the image cache
before the loader check. Existing goldens are byte-identical (no fixture used loaderless
data: images — they never worked).

## Wiring (the unified-conversion sibling contract)

`FormatRTF` (input-only until F2), `{\rtf` content magic in detectMagic (content-first —
outranks extension), `.rtf` in FormatFromPath, `rtf` in ParseFormat, `application/rtf` +
`text/rtf` MIME rows flipped from their deliberate-Unknown placeholders (canonical MIME
`application/rtf`), `openDetected` case → `OpenRTF*` frontend (converter diagnostics surface
through `WithLogf`), CLI help. A `.rtf` file with non-RTF content now fails with the parser's
precise `ErrNotRTF` instead of a generic detection error (CLI test updated accordingly).

## Tests

pkg/rtf: emphasis/toggles, font/color/size tables, alignment/indents, escapes + Unicode
fallback swallowing, hyperlink fields, table shape + emission order, picture data URIs +
unsupported-format degradation logging, page geometry, unknown-content skipping, escaped
delimiters, ErrNotRTF. pkg/doctaculous: `rtf-specimen` golden (eyeballed: emphasis, colors,
monospace, highlight, centered/hanging-indent lines, smart quotes, hyperlink, bordered 2×2
table, blue embedded picture; CJK renders blank under hermetic bundled fonts — no CJK glyphs
in the bundle, the conversion test proves the text survives), detection + format stamping,
rtf→markdown structure conversion (bold, link, table, unicode), and html→rtf still refusing
until the writer lands.
