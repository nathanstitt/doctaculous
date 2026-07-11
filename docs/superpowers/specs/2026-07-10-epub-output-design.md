# EPUB output writer (pkg/render/epubwrite)

Plan area E2 — the write half of EPUB support, completing the any⇄any matrix's last format
pair. Built ON the htmlwrite serializer (EPUB content documents ARE XHTML), so the trip is
essentially lossless — the parity bar is the strict one: html→epub→reopen→md ≡ html→md.

## Package shape

Deterministic EPUB 3: the spec-required stored (uncompressed) `mimetype` as the FIRST zip
entry, `META-INF/container.xml`, `OEBPS/content.opf` (dc:title from `Options.Title`, else the
first `<h1>`, else "Document"; fixed `dcterms:modified` stamp for determinism), `nav.xhtml`
TOC from the chapter titles, one XHTML content document per chapter, embedded images as
manifest items. Fixed timestamps and part order.

- **Chapter split at `<h1>`** over the flattened block sequence (the same
  transparent-wrapper walk the structure writers use); content before the first h1 — or a
  document with no h1 — is a single untitled chapter ("Chapter N" in the TOC).
- **Content documents**: htmlwrite in its new **XHTML mode** (`Options.XHTML`: `<hr/>`,
  `<br/>`, `<img .../>` self-close) in Fragment form, wrapped in the XHTML scaffold. The
  htmlwrite extension is byte-identical for existing callers.
- **Images**: htmlwrite's new `Options.ImageSrc` hook rewrites each `<img>` src as it
  serializes — a data: URI stays INLINE (it round-trips verbatim, which is what makes image
  parity exact); other references fetch through the source's resource loader and embed as
  deduped `images/imgN.ext` manifest items (the reader resolves them against the OPF dir).
  No loader / failed fetch → the reference is kept as-is + logged.

## Wiring

`FormatEPUB` output bit flips; `WriteEPUB` + `EPUBOptions` + `ConvertHTMLToEPUB`;
`ConvertOptions.EPUB`; `Write` dispatch; CLI help. EPUB joins the convert matrix as BOTH
input (a gen-built chapter) and output (reopen-verified: the emitted stored-mimetype entry is
exactly what `classifyOPC` detects). **This completes the any⇄any format table: all 13
formats are now both inputs and outputs.**

## Tests

`TestEPUBWriteRoundTripParity` — 17 construct cases (headings, chapter split, preamble,
emphasis, code, links, lists incl. nested, quote, code block, rule, unicode, tables incl.
colspan/caption, a data-URI image), each pinned to EXACT Markdown equality with the direct
conversion. Package-shape pins (stored mimetype first, container/OPF/nav/chapter parts,
first-heading title, nav links, chapters ⇒ pages). md→epub→md content loop. `epubout-basic`
golden.
