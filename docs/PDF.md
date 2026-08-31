# PDF — status and open work

Shipped features are inventoried in [../FEATURES.md](../FEATURES.md). This file holds only
what is NOT done. Each item lands with a new fixture/test + showcase entry in the same PR;
unsupported cases already degrade gracefully, so a TODO becoming supported just turns that
skip into real output.

## Parsing and decoding

- **Remaining scan filter** — JPX/JPEG2000 only (`pkg/internal/filter/filter.go`, `ErrUnsupported`); no
  viable pure-Go decoder exists (JBIG2 shipped via a vendored Apache-2.0 decoder — see FEATURES.md).

- **Encryption follow-ups** — non-empty user/owner passwords (no password API today), per-stream
  `/Crypt` overrides, `/Perms` validation.

## Rendering

- **Shadings / gradients (remaining)** — tiling patterns (PatternType 1; skipped + logged),
  higher-fidelity Coons/tensor patches (Types 6/7, currently bilinear-corner), luminosity soft
  masks (`/SMask` in ExtGState), and transparency groups.

## Fonts

- **Base-14 residuals** — weighted/slanted substitutes now ship (see FEATURES.md); a caller-supplied
  `FontProvider` resolves Symbol/ZapfDingbats and exact-metric faces. Remaining, low-value: a bundled
  OFL Symbol look-alike for the no-provider case, AFM tables for exact base-14 advances when a PDF
  omits `/Widths`, and synthetic emboldening/obliquing for a family missing a real variant.

## Writing

- **Fuller paged-media in the PDF-writer path** — carry the CSS Paged Media features into
  `pkg/internal/pdfwrite`.

## Extraction

- **PDF-extraction quality** — the PDF → Markdown/HTML path ships (`pkg/internal/extract`); the top lifts
  are **ToUnicode CMap parsing** (Type0/CID text — CJK / subsetted fonts currently yield `Rune==0`),
  font weight/slant through `GlyphSource` (emphasis + weight-based heading detection), and
  scanned-PDF OCR.
