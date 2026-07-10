# RTF output writer (pkg/render/rtfwrite)

Plan area F2 — the write half of full RTF support (the user-directed "if we support RTF we
should support it in full"). A cssbox STRUCTURE writer over the shared box tree (boxwalk-based,
the Markdown/DOCX shape; not layout-faithful), so every input — HTML, Markdown, text, DOCX,
XLSX/CSV tables, extracted PDF — writes to `.rtf` through one walker.

## The round-trip carriers

Every mapping is chosen so OUR OWN reader (pkg/rtf) reconstructs it — the docxwrite strategy
(its pStyle ids) transposed to RTF, and each carrier is what real word processors emit/read:

- **Block semantics ride on stylesheet names**: `\s1..\s6` "heading N" (+ `\outlinelevelN`, the
  styleless fallback Word also writes), Quote, CodeBlock, HorizontalRule. The reader gained
  stylesheet parsing (previously a skipped destination) and maps those names back to
  `<hN>`/`<blockquote>`/`<pre>`/`<hr>` — which also upgrades real Word files ("heading 1" is
  Word's canonical style name).
- **Lists**: `\ls`/`\ilvl` plus a literal `{\pntext marker\tab}` — bullets share `\ls1`, each
  ordered list gets its own instance (adjacent ordered lists stay separate); the reader now
  CAPTURES the marker (previously dropped) to tell bullet from numbered, strips it from the
  item text, and assembles nested `<ul>`/`<ol>` from the level cascade.
- **Inline code**: the monospace font (`\f1` Courier New). The reader now reads a monospace
  font selection as `<code>` instead of a font-family span (semantic upgrade, pinned).
- **Hyperlinks**: `HYPERLINK` fields with `\cf1\ul` link chrome.
- **Tables**: `\trowd`/`\cellx` rows; `\trhdr` header rows (reader → `<th>`); column and row
  spans expanded by DUPLICATING the spanned cell's content into every covered slot — the
  Markdown/CSV writers' strategy, so the round-tripped grid is identical to the direct
  conversion's. A caption becomes a bold paragraph above the table (the Markdown writer's
  bold-caption-line shape — textual parity without an RTF caption construct).
- **Pictures**: `\pict` png/jpeg blobs with `\picwgoal`/`\pichgoal` (px ×15 twips); data: URIs
  embed WITHOUT a loader (`resource.LoadDataURL`), other refs through the source's resource
  loader, degrade to alt text + log otherwise. A data-URI image round-trips byte-identically.
- **Text**: cp1252-free output — everything non-ASCII as `\uN?` (astral runes as a UTF-16
  surrogate pair), `\line`/`\tab` for newlines/tabs, syntax characters escaped.

Deterministic output (a pure function of the tree). Page geometry from Options (default US
Letter / 1in margins) as `\paperw`-family words.

## Wiring

`FormatRTF` output capability bit flips; `Document.WriteRTF` + `RTFOptions` +
`ConvertHTMLToRTF` (`rtfwrite_backend.go`); `ConvertOptions.RTF`; `Write` dispatch case; CLI
help. RTF joins the convert matrix as BOTH input and output (reopen-verified through our
reader).

## Tests

`TestRTFWriteRoundTripParity` — the core guarantee: for 17 construct cases (headings, emphasis,
code, links, lists incl. nested/adjacent, quote, code block, rule, unicode, tables incl.
colspan/rowspan/caption, a data-URI image), HTML → WriteRTF → reopen → Markdown ≡ HTML →
Markdown directly. Plus md→rtf→md content preservation, pdf→rtf extraction, the `rtfout-basic`
golden (render of a reopened writer-produced doc), writer units (escaping incl. surrogate
pairs, nil-root document, loaderless image degrade), and new reader units (stylesheet
headings + blocks, outline-level fallback, list assembly incl. marker stripping and adjacent
`\ls` splitting, `\trhdr` header rows).
