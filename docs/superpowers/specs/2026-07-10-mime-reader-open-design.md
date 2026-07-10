# MIME vocabulary, stream open, open-time context, text-output cap

Design for the first tinycld-adoption PR (work area A1 of the adoption plan): the input-surface
additions a host service needs to open documents it holds as MIME-typed streams — no filesystem
paths, no extensions — under a timeout, and to extract capped plain text for search indexing.
The concrete consumer is tinycld drive's thumbnail + search pipeline (PocketBase storage hands
out `io.Reader`s; the upload's MIME type is always known; render/extract jobs run in detached
goroutines that today have no cancellation), but every piece is general-purpose API.

## FormatFromMIME + Format.MIME (pkg/doctaculous/format.go)

`FormatFromMIME(mimeType string) Format` normalizes via `mime.ParseMediaType` (parameters
stripped, case-folded; a malformed parameter section falls back to a manual strip-at-`;`) and
looks up a single `mimeFormats` table. Three deliberate design points:

- **Explicit-Unknown rows override the text/* fallback.** An unlisted `text/*` subtype maps to
  FormatText (unknown text renders as plain text — the browser rule), but listed exceptions
  (`text/rtf`) stay Unknown until their frontend lands. Legacy binary Office types
  (`application/msword`, `vnd.ms-excel`, `vnd.ms-powerpoint`, `vnd.ms-word`) are pinned Unknown
  by test: they must never map to their OOXML cousins. HEIC/HEIF (no viable pure-Go decoder),
  `application/zip`, and `application/octet-stream` (generic containers) are Unknown; callers
  compose with content detection: `f := FormatFromMIME(mt); if f == FormatUnknown { f =
  DetectFormat(data, name) }`.
- **Rows flip, they are not added, when a frontend lands** — presentationml → FormatPPTX,
  epub → FormatEPUB, rtf → FormatRTF — the same sibling contract as the capability bits in
  `formatCaps`.
- **DetectFormat is NOT overloaded with MIME.** Its hint parameter feeds `FormatFromPath`, and
  `filepath.Ext("text/plain")` is `".plain"` — silently wrong. A separate function composes.

`Format.MIME()` returns the canonical type (`""` for Unknown); `FormatFromMIME(f.MIME()) == f`
is a tested invariant over every `formatCaps` key.

The capability predicate a host needs (`CanGenerate(mime)`) is the composition
`FormatFromMIME(mt).ValidInput()` — no new helper.

## OpenReader / OpenReaderAs (pkg/doctaculous/open.go)

```go
func OpenReader(ctx context.Context, r io.Reader, opts ...OpenOption) (*Document, error)
func OpenReaderAs(ctx context.Context, f Format, r io.Reader, opts ...OpenOption) (*Document, error)
```

Both fully buffer the reader (every parser needs random access; `Convert` has always done the
same) — documented, with the advice to cap untrusted input upstream. `OpenReaderAs(ctx,
FormatFromMIME(mt), r)` is the expected host call shape. ctx-first mirrors `Convert`.

**Open-time context.** The reflow pipeline was context-capable underneath but every opener
hardcoded `context.Background()`. Plumbing: `openDetected` gains a ctx parameter (existing
Open\*/OpenBytes\* pass Background — behavior identical; `Convert`/`ConvertFile` now pass their
real ctx, a strict improvement); the HTML-family frontends receive it as an unexported
prepended option (`withOpenContext`) so no frontend signature changes; `docxDocument` takes it
directly.

**The boundary contract: a cancelled open errors, never returns a truncated document.** The
layout engine deliberately *degrades* on cancellation (stops adding content, returns partial
output) because its other callers want renderable partial results. At the open boundary that
would silently hand a timed-out caller half a document — so `htmlDocument` and `docxDocument`
check `ctx.Err()` after layout and fail the open. Pinned by test (pre-cancelled ctx →
`context.Canceled` for both HTML and DOCX opens).

Out of scope (noted follow-up): `OpenURL`'s initial fetch still uses Background; exporting a
`WithContext` option for it is deferred.

## MarkdownOptions.MaxBytes (pkg/doctaculous/markdown_backend.go)

`MaxBytes int` — when > 0, output truncates at a UTF-8 rune boundary; the walk completes
(cheaply) with the excess discarded. Implemented as an unexported `truncateWriter` installed
inside `WriteMarkdown`, so `WriteText` and the Convert Markdown/Text targets inherit it and
`pkg/render/markdown` is untouched. The crossing write backs up over a partial rune
(`utf8.RuneStart`); subsequent writes report full length so the walker never sees an error.
Property-tested: for budgets 1..len+100, output is a prefix of the uncapped output, within
budget, valid UTF-8; budget 0 is byte-identical to no cap.

No `ExtractText` convenience: with MaxBytes, a host's extraction is a four-line buffer fill,
and one truncation mechanism beats two.

## Untouched-caller guarantee

No public signature changed; defaults reproduce the exact prior behavior (Background ctx,
uncapped output); all existing goldens and the conversion matrix pass byte-identical.

## Adoption sketch (drive, implemented tinycld-side)

```go
doc, err := doctaculous.OpenReaderAs(ctx, doctaculous.FormatFromMIME(mt), r)
err = doc.WriteImage(ctx, &buf, 0, doctaculous.ImageOptions{Format: doctaculous.FormatJPEG,
    Quality: 85, Raster: doctaculous.RasterOptions{ /* fit sizing lands in A2 */ }})
// search text: doc.WriteText(ctx, &buf, doctaculous.MarkdownOptions{MaxBytes: 50 << 10})
// capability gate: doctaculous.FormatFromMIME(mt).ValidInput()
```

No temp files, no serializing mutex, cancellation end-to-end.
