# Weighted / symbol base-14 fonts + unified font-provider seam

**Status:** implemented
**Date:** 2026-07-08
**Author:** Nathan Stitt

## Goal

Every bold / italic / oblique standard-14 font rendered in the **regular** face — the
collapse was at one chokepoint, `standard.Lookup(name)`, which returned one of three
regular-only bundled faces and ignored weight/slant. This degraded PDF rendering, DOCX /
HTML emphasis, and blocked PDF-extraction weight detection. Fix it by shipping real
weighted faces **and** unifying font resolution behind an injectable provider so a caller
can supply system / directory fonts (including families the bundle has no look-alike for,
e.g. Symbol) ahead of the bundled fallback.

## Resolution model (shared by PDF and reflow)

```
resolve(family, style):
  1. author @font-face          (HTML only — unchanged)
  2. injected font.Provider     (system / dir fonts — NEW for PDF, existing for reflow)
  3. bundled `standard` face     (hermetic default — now weight/slant-aware)
  4. graceful skip
```

The bundled step (3) is what keeps rendering hermetic (goldens/CI never depend on host
fonts); the provider (2) is what makes Symbol/ZapfDingbats and exact-metric real faces
resolvable when a caller points the toolkit at them.

## Design

### Bundled weighted faces (`pkg/font/standard`)

- Added TeX Gyre Heros & Termes `-Bold`/`-Italic`/`-BoldItalic` `.pfb` (GUST/LPPL) and
  `Inconsolata-Bold.ttf` (OFL) — same licenses as the regular faces, fetched from CTAN
  `fonts/tex-gyre/type1` (`qhv*`/`qtm*`) and the googlefonts/Inconsolata release.
- A per-family `family` struct holds the four variants; `pick(bold, italic)` falls back
  to the nearest bundled weight (mono has no upright-italic → reuses upright).
- `LookupStyled(name, bold, italic)` picks the variant; `Lookup(name)` infers the variant
  from the `/BaseFont` name (`styleFromName`) so the PDF simple-font path is auto-fixed.

### Injectable provider seam (`pkg/font`, `pkg/layout/font`)

- `font.Provider interface { LoadStyled(family string, bold, italic bool) ([]byte, bool) }`
  is defined in the **low** `pkg/font` layer so both the PDF path and the reflow engine
  depend on it with no layer inversion.
- `layoutfont.DiskFontProvider` implements it (`LoadStyled` probes `Family-Bold`,
  `Family-Italic`, … base names, degrading to the bare family).

### Reflow path (`pkg/font/family.go`, `pkg/layout/font/cache.go`)

- `LoadStandard(family, style)` now forwards `style` to `LookupStyled` (it previously
  dropped it).
- `FaceCache.resolveList` consults an injected provider (`LoadStyled`, decoded via
  `LoadSFNT`) **between** `@font-face` and the bundled fallback. A plain
  `SystemFontProvider` (LoadLocal-only) that does not implement `Provider` leaves the step
  inert — existing callers unchanged.

### PDF path (`pkg/font/font.go`, `pkg/render/raster/page.go`)

- `font.New(doc, fontDict, provider, logf)` gains a `Provider`. `standardSubstituteProgram`
  derives bold/italic from **both** the `/BaseFont` name and the descriptor `/Flags`
  (ForceBold bit 19, Italic bit 7), strips the style suffix to get the bare family, tries
  `provider.LoadStyled` (format sniffed: sfnt/CFF/Type1), then falls back to
  `standard.LookupStyled`. Graceful skip preserved (Symbol with no provider).
- `raster.Options.FontProvider` threads the provider through `pageResources.Font`.

### Public API (`pkg/doctaculous`)

- `RasterOptions.FontProvider` (PDF rasterize) mirrors reflow's `WithSystemFontProvider`.
  Default nil → bundled-only, hermetic. `extract` passes nil (structure recovery, not
  exact faces).

## Testing

- `standard`: `LookupStyled` + name-inferred `Lookup` per (family × bold × italic),
  mono italic→upright fallback, Symbol miss. Updated the stale `bold→regular` assertions.
- `font` / `layout/font`: styled `LoadStandard` yields a different program than regular;
  `DiskFontProvider.LoadStyled` candidate probing; provider-beats-bundled ordering;
  descriptor-`/Flags` bold selects the bold face; provider resolves Symbol; nil-provider
  keeps the graceful skip.
- **New fixture + golden**: `gen.WeightedFontsPDF` (non-embedded Helvetica /
  Helvetica-Bold / Times-Italic) → `weighted-fonts.png`, eyeballed: bold heavier, italic
  slanted.
- **Golden regeneration**: enabling real bold/italic changed 21 PNG goldens with
  bold/heading text (docx-styled, htmldoc showcase, presentational, paged-media);
  regenerated and **eyeballed** — an intended, reviewed fidelity change. Text-extraction
  (`.md`/`.html`) goldens unchanged (extraction does not yet key on face weight).
- `go test -race`, `go vet`, `golangci-lint` clean.

## Follow-ups

- Bundle an OFL Symbol/ZapfDingbats look-alike for the no-provider hermetic case.
- AFM base-14 metric tables for exact advances when a PDF omits `/Widths`.
- Synthetic emboldening/obliquing for a family lacking a real variant (mono italic).
- Thread the resolved weight/slant into `TextGlyph.Bold/Italic` for PDF-extraction
  emphasis (a distinct extraction-quality TODO).
```
