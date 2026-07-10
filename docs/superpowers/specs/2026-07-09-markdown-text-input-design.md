# Markdown & plain-text input frontends

**Status:** implemented
**Date:** 2026-07-09
**Author:** Nathan Stitt

## Goal

Make Markdown (.md) and plain text (.txt) first-class conversion inputs — md/txt →
PDF/HTML/text/images (and md → md round trips) — as the first sibling of the unified
conversion core (`2026-07-09-unified-conversion-core-design.md`): one capability-bit flip
plus one `openDetected` case each.

## Design

### Markdown (`pkg/markdown` + `pkg/doctaculous/markdown_frontend.go`)

- **Parser: goldmark v1.8.2** (MIT, pure Go, **zero transitive deps** — its go.mod
  requires nothing). CommonMark-conformant with the official GFM extensions (tables,
  strikethrough, task lists, autolinks); the de-facto standard (Hugo's engine).
  Alternatives rejected: blackfriday/gomarkdown (pre-CommonMark dialect, weaker
  conformance), hand-rolled subset (the CommonMark spec is hundreds of edge-case
  behaviors — exactly the dep policy's "removes real, risky work" case).
- **Lowering: goldmark → HTML string → the existing HTML pipeline** (`OpenHTMLBytes`),
  not a direct AST→cssbox construction, which would re-implement what box generation
  already does (whitespace collapsing, anonymous-box fixups, marker/counter resolution,
  `applySemantics`). Route (a) inherits tables, lists, images, pagination/@page, every
  `HTMLOption`, and the SemTag/HeadingLvl/Href annotations — which is what makes
  md → open → WriteMarkdown a round trip. Verified plumbing: goldmark task-list
  checkboxes lower via `CtrlCheckbox`/`boxwalk.LeadingCheckbox`; GFM column alignment is
  emitted as an inline `style="text-align:…"` that flows through the cascade and back
  out as `---:`/`:---:`; `<del>` maps to SemTag "s".
- **`html.WithUnsafe()`** (raw HTML passthrough) is on: raw HTML is core CommonMark, and
  the only consumer is this module's own engine — nothing executes.
- **Default stylesheet** (`pkg/markdown/style.go`): a GitHub-ish author-origin `<style>`
  element in the generated document — NOT the UA sheet (that serves all HTML) and NOT
  the resource loader (which stays free for the source's relative image refs). Only
  engine-supported properties.
- **API**: `OpenMarkdown` / `OpenMarkdownFile` (roots `DirLoader` + `DiskFontProvider`
  at the file's dir, exactly like `OpenHTMLFile`, so relative image refs resolve) /
  `OpenMarkdownBytes(data, opts ...HTMLOption)`. Documents stamp `FormatMarkdown`.
- goldmark does not document goroutine safety, so the converter is constructed per call
  (trivial next to a layout).

### Plain text (`pkg/doctaculous/text_frontend.go`, ~40 lines)

Escaped text in a single `<pre>` with `white-space: pre-wrap`: hard line breaks — the
only structure plain text has — are preserved exactly (logs, poems, aligned columns),
while over-long lines still soft-wrap so nothing clips at the page edge. Monospace comes
from the UA sheet; pagination works through normal mid-block line fragmentation; and
because `<pre>` carries SemTag "pre", .txt → .md is one lossless fenced code block and
.txt → .txt is identity. Normalization: strip a UTF-8 BOM, CRLF/CR → LF,
`ToValidUTF8` (U+FFFD). Splitting on blank lines into `<p>`s was rejected: it destroys
hard breaks and invents structure that isn't there. API: `OpenText` / `OpenTextFile` /
`OpenTextBytes`; documents stamp `FormatText`.

### Detection & CLI

Markdown/text have no content magic — the extension hint (or `--from`/`OpenAs`) is the
signal, which the core's `DetectFormat` already orders before HTML sniffing (a README.md
full of raw HTML stays Markdown). Capability bits flipped; `openDetected` gains the two
cases (sharing `openReflowFrontend`, which also now serves HTML). The CLI needed only
usage text and `inferCommand` additions — `convert`, `topdf`, `tomd`, `tohtml`,
`rasterize` all accept .md/.txt through the shared opener automatically.

## Engine fix landed with this (blank-line strut)

The text frontend's golden exposed a pre-existing `white-space` fidelity bug: an empty
forced line (blank line in pre/pre-wrap/pre-line) collapsed to zero height, because an
empty line has no glyph metrics and `line-height: normal` resolved to 0. Browsers give
every line box a strut (CSS 2.1 §10.8.1). Fix at the source, in the inline core: a
preserved newline's break glyph now carries its run's font metrics
(`pkg/layout/inline/shape.go`), and the breakers keep that glyph on an otherwise-empty
forced line (`break.go`: `Break`, `BreakNextWrap` wrap + no-wrap paths) so
`MakeLine`/`autoLineHeight` produce a strut height. Non-empty lines still exclude the
break glyph — every pre-existing golden/reftest is byte-identical. Regression tests at
both levels (`pkg/layout/inline/emptyline_test.go`, `TestTextBlankLinesRender`).

## Testing

`pkg/markdown`: per-construct ToHTML units (headings, emphasis/strike, links/autolinks,
images, blockquote, fenced/inline code, nested/ordered/task lists, hr, aligned tables,
raw-HTML passthrough, escaping). `pkg/doctaculous`: round-trip test over a
construct-covering specimen — every construct survives, and the writer's output is a
**fixed point** (reopening it reproduces it byte-for-byte); md↔HTML parity (a Markdown
document and its equivalent HTML produce identical Markdown); relative-image resolution;
pagination; text identity/escaping/UTF-8/blank-line/pagination tests. Visual entry:
`md-specimen` and `text-pre` goldens (`mdtext_golden_test.go`), generated with
`-update` and eyeballed. Conversion-matrix, CanConvert-matrix, detection, and CLI tests
updated for the two new input bits; CLI md→pdf/html and txt→png end-to-end tests.
