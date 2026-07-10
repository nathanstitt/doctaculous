# Unified conversion core (format detection, generic Open/Convert, CLI convert)

**Status:** implemented
**Date:** 2026-07-09
**Author:** Nathan Stitt

## Goal

Convert any supported format to any other via one coherent surface — API and CLI — instead
of the ad-hoc assembly that grew per feature: format chosen by which `Open*` you call (no
detection anywhere in the library), one-shot `Convert*` wrappers covering only 5 of ~15
combinations (DOCX had zero), image encoding living only in the CLI, and four subcommands
each re-implementing an extension switch inconsistently (`rasterize` assumed unknown
extensions were PDF; `topdf` rejected PDF with a bespoke message; the others rejected
unknowns).

Product decisions fixed up front:

- **Same-format → same-format is not supported.** The generic path returns a typed
  `ErrSameFormat`. The format-specific writers (e.g. `WriteHTML` on an HTML document, a
  useful normalization round-trip) remain unrestricted — this is policy, not accident, and
  is documented on both sides.
- Markdown input, plain-text input, and DOCX output land as **sibling sub-projects**; this
  core anticipates them so each becomes a one-bit capability flip plus a switch case.

## Design

### Format + capability table (`pkg/doctaculous/format.go`)

`type Format string` with constants (`pdf`, `docx`, `html`, `markdown`, `text`, `png`,
`jpeg`) and three sentinels (`ErrUnknownFormat`, `ErrUnsupportedFormat`, `ErrSameFormat`).
One unexported capability map — which formats are valid inputs, which are valid outputs —
is the single table both the API and the CLI consult, via `ValidInput`/`ValidOutput`/
`CanConvert(from, to)`. Check order is most-informative-first: unknown → input role →
output role → same-format. There are no valid-role-but-invalid-pair cells (PDF reaches
the structure outputs via lazy extraction; every reflow input reaches every writer), so
no pairwise exceptions exist; if one ever appears it becomes an explicit case inside
`CanConvert`. `ParseFormat` maps user-facing names/aliases (md, txt, htm, jpg, plain…);
`FormatFromPath` maps extensions.

Capability bits at this landing: PDF/DOCX/HTML input; PDF/HTML/Markdown/Text/PNG/JPEG
output. Markdown/Text input and DOCX output ship `false` and flip in their PRs.

### Detection (`pkg/doctaculous/detect.go`)

`DetectFormat(data []byte, hint string) Format`, stdlib-only, ordered:

1. **Binary magic — content beats extension** (a real PDF named `report.txt` is a PDF):
   PNG/JPEG signatures; `PK\x03\x04` → `zip.NewReader` central-directory check for
   `word/document.xml` (or `[Content_Types].xml` plus any `word/` part, tolerating a
   rels-redirected main part) → DOCX, other zips fall through; `%PDF-` within the first
   1 KiB (the spec's window for junk-prefixed headers).
2. **Extension hint** (`FormatFromPath(hint)`) — the only signal for `.md`/`.txt`, and it
   deliberately outranks HTML sniffing so a README.md full of raw HTML blocks stays
   Markdown. It also rescues binaries whose magic is damaged (a `.pdf` with >1 KiB of
   junk, which the parser's object-scan rebuild can still open).
3. **WHATWG-style HTML tag sniff** (BOM/whitespace skip, the §7.1 pattern table each
   followed by a tag-terminating byte; `<!--` and `<?xml` accepted bare).
4. `FormatUnknown`. There is deliberately **no "valid UTF-8 ⇒ text" fallback** — random
   binary often decodes, and a silent misdetection is worse than a clean error pointing at
   `OpenAs`/`ConvertOptions.From`/`--from`.

### Generic Open (`pkg/doctaculous/open.go`)

`Open`/`OpenBytes` became sniffing openers (`opts ...OpenOption`, an alias of
`HTMLOption`; reflow-only, ignored by PDF/DOCX). `Open` on `http(s)://` routes to
`OpenURL` — a URL is always fetched as a web page. `OpenAs`/`OpenBytesAs` skip detection
(the `--from` escape hatch). `openDetected` is the one input dispatch, rooting
`DirLoader`/`DiskFontProvider` at the file's directory for HTML inputs exactly as
`OpenHTMLFile` does. Back-compat is strictly additive: PDFs hit the magic first and take
the identical `pdf.Parse` path; non-PDF inputs previously failed with a PDF parse error
and now open. `Document` gains a `format` field + `Format()` accessor, stamped by every
opener — this is what makes the same-format check possible at write time.

### Generic Convert (`pkg/doctaculous/convert.go`)

`Convert(ctx, in, out, ConvertOptions)` / `ConvertFile(ctx, inPath, outPath, opts)` /
`(*Document).Write(ctx, w, to, opts)`. `ConvertOptions` nests the existing per-output
option structs (`PDF`, `Markdown`, `HTMLOut`, `Image`) plus `HTML []HTMLOption` for input
layout and cross-cutting `Logf`/`BundledFonts` — only the nested options matching `To` are
consulted, avoiding an option explosion. `Convert` validates `CanConvert` **before**
opening (fail fast, no layout work). Input options assemble exactly as `ConvertHTMLToPDF`
always did (derived defaults first, caller's `HTML` options last so they win; `PDF.Print`
switches the cascade to print media). `Document.Write` is the one output dispatch and the
same-format gate. The five legacy `ConvertXToY` wrappers are now one-line shims over
`Convert`, pinned byte-identical by test.

### Image output (`pkg/doctaculous/image_backend.go`)

PNG/JPEG are output formats. `ImageOptions{Format, Quality, Page, Raster}`;
`(*Document).WriteImage(ctx, w, index, opts)` plus exported `EncodeImage` (the encode
half) so the CLI's batch fan-out reuses the library's encoding. Convert-to-image writes
exactly **one** page (`Image.Page`, default first) — an `io.Writer` holds one encoded
image; multi-page image output stays a batch concern (`RasterizePages` + one
`EncodeImage` per page, as the CLI does with a `%d` pattern).

### CLI

New `convert` subcommand — `doctaculous convert <in> <out>` with `--from`/`--to`
overrides and the union flag set; image targets reuse the rasterize machinery
(`resolvePages`/`outputPath` + a shared `renderPages` helper). All four legacy openers
collapsed onto one detection-based `openInput`; `rasterize`'s unknown-ext-assumes-PDF wart
is gone (content detection instead — an extension-less real PDF still opens, garbage gets
a format error). `topdf` writes through the generic dispatch so a PDF input gets the
precise `ErrSameFormat`; `tomd`/`tohtml` keep calling their writers directly so the
HTML→HTML normalization round-trip keeps working. `inferCommand` stays as a complement.
A bare `-` (stdout marker) is no longer mis-parsed as a flag by `reorderArgs`.

Fixed in passing: the CLI `topdf --print` flag was silently dropped (print media was only
applied by `ConvertHTMLToPDF`, not the CLI's own opener); `openInput` now threads it
through. `rasterize` gained `--quality` (JPEG quality was hardcoded 90).

## Testing

`format_test.go` pins the full 7×7 matrix against role tables that are the deliberate
expectation (flipping a bit there is part of a format PR). `detect_test.go` covers each
magic, the zip discriminations, HTML sniff variants, extension tiebreakers, and
magic-beats-extension. `open_test.go` covers sniffed opens from mis-extensioned files,
URL routing, and format stamping for every opener. `convert_matrix_test.go` iterates
every (From, To) pair over tiny hermetic fixtures — valid pairs must produce spot-checked
output, invalid pairs must fail with the same sentinel `CanConvert` reports — plus
auto-detect, `Image.Page` selection, `ConvertFile` inference, shim byte-equivalence, and
the same-format policy asymmetry. CLI `convert_test.go` covers the new command
end-to-end plus the rasterize sniffing regression.

## Sibling contract

A new **input** format: flip its input bit in `formatCaps` (and the role table in
`format_test.go`), replace its case in `openDetected`, add detection if any beyond the
extension hint. A new **output** format: flip its output bit, add its case in
`Document.Write`, nest its options struct in `ConvertOptions`. Everything else — CLI
`convert`, `ConvertFile` inference, capability errors — picks the format up from the
table.
