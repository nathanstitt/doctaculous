# HTML / DOCX → Markdown & Plain-Text output

**Status:** implemented
**Date:** 2026-07-07
**Author:** Nathan Stitt

## Goal

Convert a reflow document (HTML or DOCX) to GitHub-Flavored Markdown, or to plain text.
The headline requirement is **full high-fidelity tables** — including merged cells. This
is the toolkit's first *text-side* conversion output (previous outputs were pixels and
PDF), and the first that requires the document's **structure**, not its pixels.

## Why a box-tree walker, not a `render.Device`

The existing output seam is `render.Device`, driven by the layout paint pass. It sees
only *positioned glyphs, rules, and images* — a heading is indistinguishable from bold
text, a link has no URL, a table is a set of painted rectangles. Markdown needs the
document's semantic structure, so the markdown backend consumes the shared **`cssbox`
tree** directly (the read-only box model both frontends converge on), one level above
paint. Because HTML box generation and DOCX lowering both produce this tree, **one
walker serves both formats** — proven by a DOCX↔HTML parity test.

## Box-tree semantic annotations

The `cssbox` tree was intentionally *visual-only*: it dropped the three facts a Markdown
writer needs and cannot recover from computed style — heading **level** (an `<h2>` and a
`font-weight:bold; font-size:24px` div are identical after cascade), link **URLs**, and
DOCX **style identity** (heading level lives in the paragraph style id, e.g.
`"Heading2"`, not the resolved size).

Three additive fields were added to `cssbox.Box` (`box.go`), all zero-valued for
existing callers so layout and the raster/PDF backends are byte-identical:

- `SemTag string` — a small closed vocabulary the writer can represent: `h1`..`h6`,
  `p`, `a`, `blockquote`, `pre`, `code`, `em`, `strong`, `s`, `hr`.
- `HeadingLvl int` — 1..6 for a heading, 0 otherwise.
- `Href string` — the resolved link target.

Capture points:
- **HTML** (`pkg/layout/css/build.go`, `applySemantics`): from the source element's tag
  and `href`, alongside the existing colspan/replaced-attr capture.
- **DOCX** (`pkg/docx/cssbox/lower.go`): `paragraphSemantics` maps the paragraph style
  id (`Heading1..9`→`h1..h6` clamped, `Title`→`h1`, `Quote`→`blockquote`, …);
  `hyperlinkURL` resolves the hyperlink `RelID` through `Document.Rels` (the reserved
  `Hyperlink.Target` path) and threads `rels` down the lowering functions.

Bold/italic/strikethrough are read from computed style (`Style.Bold`/`Italic`/
`TextDecorationLine`), so DOCX emphasis — which has no `<strong>` tag — works uniformly.
A UA rule `s, strike, del { text-decoration: line-through }` was added so HTML struck
text also produces the signal.

## The markdown writer (`pkg/render/markdown`)

A tree walker over the finalized `cssbox` tree (markers and anonymous boxes already
resolved). Files: `markdown.go` (block dispatch), `inline.go` (styled-run flattening +
emphasis/links/images), `list.go` (nested lists, task lists), `table.go` (GFM tables).
Plain-text mode is the same walk with all markers/URLs stripped, driven by
`Options.Plain`.

Block constructs: ATX headings, paragraphs, `>` blockquotes (recursive), fenced code
blocks, ordered/unordered/**nested** lists, GFM **task lists** (`- [ ]`/`- [x]` from a
leading `<input type=checkbox>`), thematic breaks (`---`), and tables.

Inline: bold `**`, italic `_`, strikethrough `~~`, inline `` `code` ``, links
`[text](url)`, and images `![alt](src)` (from an `<img>` replaced box). Adjacent runs
sharing style are coalesced; Markdown metacharacters are escaped; heading and header-
cell text suppress the ambient UA bold so it does not wrap the whole line in `**`.

### High-fidelity GFM tables

GFM pipe tables cannot express merged cells, so the decided strategy is **duplicate the
spanned cell's content into every grid slot it covers**: the grid stays rectangular and
no content is lost. `buildGrid` reconstructs the occupancy grid from the
`DisplayTableRow`/`DisplayTableCell` boxes (through row groups), reading
`ColSpan`/`RowSpan` — mirroring the layout table builder's occupancy scan. The first
row (a `<thead>`, a bold-`<th>` row, or, failing those, the first data row promoted with
a logged notice) becomes the GFM header; per-column alignment comes from the cells'
`text-align` (`:---:`, `---:`). A `<caption>` is emitted as a bold line above the table.
Plain-text mode renders the same grid as space-padded columns.

## Public API & CLI

`pkg/doctaculous/markdown_backend.go`, symmetric with the PDF writer:
`ConvertHTMLToMarkdown` / `ConvertHTMLToText` and `(*Document).WriteMarkdown` /
`WriteText` (reflow docs only; a PDF-backed document errors). The reflow renderer now
retains its `cssbox` root so the backend can walk it. CLI: `doctaculous tomd <input>
[--out file.md] [--plain]`, inferred from a `.md`/`.txt` output extension.

## GFM coverage

Produced: headings, bold/italic/strikethrough/inline-code, links, images, blockquotes,
fenced code, ordered/unordered/nested lists, task lists, thematic breaks, and pipe
tables (with span expansion, alignment, caption). Not produced (no clean source signal
or deferred): footnotes, autolinks, definition lists, fenced-block language hints,
`<br>` outside table cells. These become real output when a fixture needs them.

## Testing

- Box-annotation unit tests (HTML `applySemantics`; DOCX `paragraphSemantics` +
  hyperlink resolution, incl. the unresolved-anchor degradation).
- `pkg/render/markdown` per-construct tests.
- `pkg/doctaculous` markdown/text goldens (`md-*.md`/`.txt`) covering every construct and
  the table matrix (simple, colspan, rowspan, combined span, no-header, aligned, caption,
  thead), regenerated with `-update`.
- DOCX↔HTML parity test (same Markdown from both frontends for a heading + table).
- Showcase: `htmldoc.md`/`htmldoc.txt` — the multi-file specimen exported to Markdown and
  text, the text-side counterpart to the raster showcase.
- CLI `tomd` end-to-end tests.
- The full pre-existing raster/PDF/DOCX/HTML golden suite passes unchanged, confirming
  the annotations do not perturb layout.

## Out of scope / follow-ups

- Markdown → HTML (reverse conversion) and PDF → Markdown (needs the deferred PDF
  text-extraction backend).
- A dedicated `<th>` `SemTag` (header detection currently uses UA bold).
- Multi-level DOCX list numbering in markers (shared with the existing list follow-up).
