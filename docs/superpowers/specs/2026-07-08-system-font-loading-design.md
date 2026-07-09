# System font loading (default) with a bundled opt-out

**Status:** design approved, ready for implementation plan
**Date:** 2026-07-08
**Author:** Nathan Stitt

## Context

The toolkit currently resolves non-embedded fonts (a standard-14 `/BaseFont`, or a CSS
family) only from bundled substitutes (`pkg/font/standard`: TeX Gyre + Inconsolata) or a
caller-supplied `DiskFontProvider` pointed at a directory. There is no way to use the
fonts actually installed on the host machine, which is what most users expect when they
render a document — a PDF naming Helvetica or a DOCX in Calibri should use the real
installed face when it exists.

This adds **system-font discovery as the default**, backed by the pure-Go, MIT-licensed
`github.com/adrg/sysfont`, which live-scans the platform's standard font directories
(via `adrg/xdg`, covering macOS `~/Library/Fonts` + `/Library/Fonts` +
`/System/Library/Fonts`, Linux `~/.fonts` + `~/.local/share/fonts` + `/usr/share/fonts`,
and the Windows Fonts folder) and fuzzy-matches a family+style query to an installed
file. A bundled opt-out preserves today's hermetic behavior for reproducible rendering
(the golden tests) and bare/headless machines.

## Goals / non-goals

- **Goal:** zero-config use of installed system fonts by default, cross-platform
  (verified: macOS, Linux, Windows), pure Go, no CGo.
- **Goal:** a bundled-fonts opt-out (library option + CLI flag) for hermetic rendering.
- **Non-goal:** font *installation*, web-font downloading beyond today's `@font-face`
  path, or building our own metadata-based matcher (we accept sysfont's
  filename/catalog-driven matching).

## Design

### Two mutually-exclusive font-source modes

| Mode | Resolution chain |
| --- | --- |
| **System (default)** | `@font-face → OSFontProvider (sysfont, trusted) → bundled fallback only if sysfont returns nil → skip` |
| **Bundled (opt-out)** | `@font-face → bundled TeX Gyre substitute → skip` (sysfont never consulted) |

- **System mode** trusts whatever `sysfont.Finder.Match` returns — no confidence gate.
  sysfont draws its matches from the fonts *actually discovered on disk*, so on a
  populated machine an unfound family maps to a sensible installed relative, and it
  returns `nil` only when the machine has essentially no usable font (a bare container /
  headless CI). On that `nil`, we fall through to the bundled face as a safety net so
  text still renders — this never overrides a real system match and only fires on a
  near-empty box.
- **Bundled mode** is the existing behavior, unchanged, and is what the golden / reftest
  harnesses pin so their output is reproducible regardless of the host's installed fonts.

Both modes feed the *existing* `font.Provider` seam (`LoadStyled(family, bold, italic)
([]byte, bool)`) that already threads through the reflow `FaceCache` and PDF
`RasterOptions.FontProvider` — no change to the resolution seam itself, only to which
provider chain is installed and in what order relative to the bundled fallback.

### `OSFontProvider` (`pkg/layout/font/osfont.go`, new file)

A concrete `font.Provider`. (Named `OSFontProvider`, not `SystemFontProvider`, because
`pkg/layout/font` already defines a `SystemFontProvider` *interface* for `@font-face
local()` — the names must not collide.)

```go
// OSFontProvider resolves a family+style to an installed OS font via adrg/sysfont,
// which live-scans the platform's standard font directories. The directory scan runs
// once, lazily, on first use and is cached; the provider is safe for concurrent
// LoadStyled calls across the page fan-out.
type OSFontProvider struct {
    once   sync.Once
    finder *sysfont.Finder
}

func NewOSFontProvider() *OSFontProvider
func (p *OSFontProvider) LoadStyled(family string, bold, italic bool) ([]byte, bool)
```

`LoadStyled`:
1. Lazily build+scan the finder (`sync.Once`).
2. Compose a query from the request — `family` plus style words (`"Arial Bold"`,
   `"Times Italic"`) — the form sysfont's matcher expects.
3. `font := finder.Match(query)`. If `font == nil` → return `ok=false` (bare-box path).
4. `os.ReadFile(font.Filename)`; on error → log + `ok=false` (never panic — project rule).
   A `.ttc` collection is returned as-is (the downstream loader selects the face); if
   that proves unreliable in practice, log + `ok=false` rather than return a wrong face.
5. Return the bytes, `ok=true`.

### Mode selection & public surface

A small mode toggle chooses which provider chain the pipelines install. Default =
system. Bundled mode is exposed as:

- **Library (reflow):** a `WithBundledFonts()` HTML option that selects bundled mode
  (default is system, i.e. an `OSFontProvider` is installed as the resolver's provider
  ahead of the bundled fallback).
- **Library (PDF):** a `RasterOptions.BundledFonts bool` (default false = system). Precedence,
  highest first: (1) an explicitly-set `RasterOptions.FontProvider` always wins — a caller who
  supplies their own provider (a `DiskFontProvider`, or a pre-built `OSFontProvider`) gets exactly
  that, and `BundledFonts` is ignored; (2) else if `BundledFonts == true`, bundled-only (no provider
  installed); (3) else (the default) the renderer installs an `OSFontProvider`.
- **CLI:** a `--bundled-fonts` flag on the document-consuming subcommands (`rasterize`,
  `topdf`, `tomd`, `tohtml`) that flips to bundled mode.

The bundled fallback-on-`nil` in system mode is wired where the provider chain is
assembled (the reflow `FaceCache.resolveList` already falls through to the bundled
`LoadStandard`; the PDF `standardSubstituteProgram` already falls through to
`standard.LookupStyled`) — so "system → nil → bundled" is the natural existing fall-through
once the `OSFontProvider` is the installed provider and returns `ok=false` on `nil`.

### Dependencies

`github.com/adrg/sysfont` and its transitive deps `github.com/adrg/os-font-list`,
`github.com/adrg/strutil`, `github.com/adrg/xdg` — all MIT and pure Go. Recorded with
rationale in the PR per the project's dependency policy.

## Testing

- **Hermeticity preserved:** the golden / reftest harnesses pin **bundled mode**
  explicitly, so every existing golden (raster PDF, docx-*, html-*, htmldoc-*, pdfx-*)
  is unchanged and reproducible regardless of the CI machine's fonts. This is the single
  behavior the whole test corpus depends on.
- **`OSFontProvider` unit tests (assertive-when-possible, never flaky):** build the
  provider, discover what is actually installed via the finder; if a common family
  resolves on this host, assert `LoadStyled` returns non-empty bytes that parse as a font
  and (where determinable) match the requested style; if the host is bare (finder lists
  nothing usable), `t.Skip`. No hard dependency on any single font — notably **not** Arial
  (a Microsoft font absent from default Linux/CI), which would make the test flaky/skipped
  on the very platform we care about.
- **Mode-selection tests:** default resolves via the system provider; `--bundled-fonts` /
  `WithBundledFonts` / `RasterOptions.BundledFonts` resolves via the bundle; a stubbed
  `OSFontProvider` returning `ok=false` (simulating sysfont `nil`) falls back to the
  bundled face in system mode.
- **CLI tests:** `--bundled-fonts` is accepted and switches modes on each subcommand.
- `go test -race ./...`, `go vet ./...`, `golangci-lint run` clean.

## Follow-ups

- Metadata-based matching (read `name`/`OS2` tables ourselves) if sysfont's
  filename/catalog matching proves too weak on macOS (`.ttc`, opaque filenames) — a
  larger, dependency-free alternative deferred unless a concrete case demands it.
- Threading the resolved system face's real metrics/weight into PDF extraction (a
  separate extraction-quality item).
