# Changelog

Notable changes per release. This project is pre-1.0: the exported API may still
change between minor versions, and only the latest release gets fixes. That changes
at v1.0.0.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed — BREAKING

- **The module was renamed** from `github.com/nathanstitt/doctaculous` to
  `github.com/nathanstitt/omnidoc`. Existing users must update their import paths;
  there is no compatibility shim.
- **The public API shrank from 16 packages to 7.** `omnidoc`, `docx`, `xlsx`, `pdf`,
  `crop`, `heif`, and `resource` remain public; the engine internals (`css`, `html`,
  `layout`, `font`, `render`, `markdown`, `rtf`, `pptx`, `epub`, and the rest) moved
  under `pkg/internal/`. Nothing an application should have been importing was
  removed, but anything reaching into the engine directly will no longer compile.
  This is the surface a v1.0.0 tag would freeze, which is why it was narrowed first.
- `Convert`, `ConvertFile`, and the `Open*` family take a `context.Context` on the
  paths that do layout work, so a pathological document can be cancelled rather than
  wedging the calling goroutine.

### Added

- **`margin: 0 auto` centres.** Horizontal `auto` margins resolve per CSS 2.1 §10.3.3
  — the most common centering idiom in CSS previously did nothing, silently.
- Vertical writing modes: `writing-mode` and `text-orientation` on the HTML and SVG
  paths, with vertical font metrics.
- SVG output, and WebP encoding.
- Error sentinels (`ErrSheetNotFound`, `ErrNoStructure`, `ErrPageOutOfRange`,
  `ErrTooDeeplyNested`) so callers can branch on failure kinds with `errors.Is`
  instead of matching strings.
- Runnable examples for `Convert`, `ConvertFile`, `Open`, and `RasterizePage`.
- A `-v` flag on the CLI, surfacing the degradation logs the library already emitted.

### Fixed — hardening

A dedicated pass fuzzed every parser and fixed each crash and hang it found. Before
it, several formats could be crashed or hung by a file of a few kilobytes.

- Panics and stack overflows on malformed PDF, XLSX, SVG, HTML, Markdown, RTF, CSS,
  and font input.
- Unbounded allocation and decompression bombs: a 512 MB cap per decoded stream, plus
  an aggregate part budget for DOCX and EPUB archives.
- Integer overflows in image dimensions, spreadsheet cell references, table spans, and
  grid track counts, now checked by division rather than multiplication.
- Cycles in the PDF page tree and object streams.

Three of the defects were in dependencies rather than in this code.

### Fixed — rendering

- Silent degradations that changed output with nothing in the log now warn once:
  `@font-face` `unicode-range` and `font-display`, `position: relative` on a
  non-replaced inline box, and empty-block margin collapse-through.
- Flex content height with margins; overflow clipping with `max-height`.

### Documentation

- Three overlapping backlog documents were consolidated into `docs/BACKLOG.md`,
  holding only outstanding work. Every entry was re-verified against the code, which
  found eight stale claims.
- Added `CONTRIBUTING.md`, `SECURITY.md`, and this file.

## [0.1.1] — 2026-07-29

## [0.1.0] — 2026-07-28

## [0.0.6] — 2026-07-27

Releases up to 0.1.1 predate this changelog; see the git history and the release
notes on GitHub for what they contained.

[Unreleased]: https://github.com/nathanstitt/omnidoc/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/nathanstitt/omnidoc/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/nathanstitt/omnidoc/compare/v0.0.6...v0.1.0
[0.0.6]: https://github.com/nathanstitt/omnidoc/releases/tag/v0.0.6
