# Web-font test fixtures — provenance

These fixtures are hermetic test inputs for the HTML web-fonts sub-project
(`@font-face` + WOFF/WOFF2 decoding). All three font files
(`webfont.ttf`, `webfont.woff`, `webfont.woff2`) are derived from a single
ground-truth TTF so the decode/golden tests can assert they produce identical
glyphs.

## Font

- **Name:** Pacifico (Regular)
- **Author:** Vernon Adams / The Pacifico Project Authors
- **Copyright:** Copyright 2018 The Pacifico Project Authors
  (<https://github.com/googlefonts/Pacifico>)
- **License:** SIL Open Font License, Version 1.1 (OFL-1.1) —
  <https://openfontlicense.org/> — full text shipped alongside as `OFL.txt`
  (required by the OFL).
- **Why this font:** Pacifico is a brush-script face whose letterforms are
  obviously distinct from the project's bundled base-14 substitutes
  (TeX Gyre Heros/Termes, Inconsolata). A golden image therefore proves the
  downloaded web font is actually used, not silently replaced by a fallback.

## Source URLs (Google Fonts GitHub mirror, OFL-licensed)

- TTF: <https://github.com/google/fonts/raw/main/ofl/pacifico/Pacifico-Regular.ttf>
- OFL: <https://github.com/google/fonts/raw/main/ofl/pacifico/OFL.txt>

Both downloaded 2026-06-26.

## Glyph coverage (after subsetting)

The committed `webfont.ttf` is a **subset** of the upstream font, reduced to keep
the fixture small (upstream Pacifico-Regular is ~329 KB / 1533 glyphs; the subset
is ~11 KB / 54 glyphs). The subset contains exactly:

- **U+0020** SPACE
- **U+0041–U+005A** A–Z (26 uppercase ASCII letters)
- **U+0061–U+007A** a–z (26 lowercase ASCII letters)

= 53 cmap entries (54 glyphs incl. `.notdef`). This covers every letter the
decode/golden tests probe (e.g. runs containing `A`, `a`, `g`, `W`, `F`). Verified
with `fontTools.ttLib.TTFont(...).getBestCmap()`.

The WOFF1 and WOFF2 files are generated **from this same subset TTF**, so all three
share identical glyph outlines and metrics. `webfont.woff2` uses the standard
**glyf transform** (`glyf`/`loca` transformVersion 0 — the default for fonttools
`compress`).

## Exact commands

Environment: a dedicated Python venv with `fonttools` + `brotli`
(`brotli` is required for WOFF2 `compress`):

```sh
python3 -m venv "$TMPDIR/webfont_venv"
"$TMPDIR/webfont_venv/bin/python" -m pip install fonttools brotli
# fonttools 4.63.0, brotli 1.2.0
```

Download the upstream TTF and the OFL license:

```sh
curl -fsSL -o Pacifico-Regular.ttf \
  https://github.com/google/fonts/raw/main/ofl/pacifico/Pacifico-Regular.ttf
curl -fsSL -o OFL.txt \
  https://github.com/google/fonts/raw/main/ofl/pacifico/OFL.txt
```

Subset the TTF to space + A–Z + a–z → `webfont.ttf`:

```sh
python -m fontTools.subset Pacifico-Regular.ttf \
  --output-file=webfont.ttf \
  --unicodes=U+0020,U+0041-005A,U+0061-007A \
  --layout-features='' --no-hinting --desubroutinize
```

Generate WOFF1 from the subset TTF → `webfont.woff`:

```sh
python -c "from fontTools.ttLib import TTFont; f=TTFont('webfont.ttf'); f.flavor='woff'; f.save('webfont.woff')"
```

Generate WOFF2 (glyf transform, the default) from the subset TTF → `webfont.woff2`:

```sh
python -m fontTools.ttLib.woff2 compress -o webfont.woff2 webfont.ttf
```

## Verification (magic bytes / sizes)

| file           | size (bytes) | first 4 bytes | `file(1)` says                     |
|----------------|-------------:|---------------|------------------------------------|
| `webfont.ttf`  |       10 768 | `00 01 00 00` | TrueType Font data                 |
| `webfont.woff` |        7 312 | `wOFF`        | Web Open Font Format               |
| `webfont.woff2`|        6 064 | `wOF2`        | Web Open Font Format (Version 2)   |
| `OFL.txt`      |        4 389 | (ASCII text)  | SIL Open Font License 1.1          |

All three font files round-trip back to a valid `TTFont` with the same 53-entry
cmap, confirming the WOFF/WOFF2 encodings are sound.

## Composite-glyph fixtures (`composite.ttf`, `composite.woff2`)

The Latin subset above contains only **simple** glyphs, so the WOFF2 glyf-transform
decoder's composite-glyph reconstruction path (`reconstructCompositeGlyph`) was never
exercised by a fixture. `composite.ttf` and `composite.woff2` close that gap.

`composite.ttf` is `webfont.ttf` (the OFL Pacifico subset above) with **one extra
glyph added**: a synthetic **composite** glyph mapped to **U+0040 ('@')** that
references two glyphs already present in the subset — component `A` at the origin and
component `B` translated by (300, 100) font units. No new outlines were copied in; the
composite is built entirely from outlines already in the OFL-licensed subset, so the
file remains an OFL-1.1 derivative of Pacifico (same `OFL.txt` applies). `webfont.ttf`
has 54 glyphs; `composite.ttf` has 55 (the added composite).

`composite.woff2` is `composite.ttf` compressed with the standard **glyf transform**
(fonttools `compress`, the default), so decoding it drives the composite-glyph branch
of the transform reconstruction.

### Exact commands

Using the same `fonttools` + `brotli` venv as above (fontTools 4.63.0, brotli 1.2.0),
run a small build script that, against `webfont.ttf`:

1. constructs a composite `Glyph` (`numberOfContours = -1`) with two
   `GlyphComponent`s — `A` at (0, 0) and `B` at (300, 100), `ARGS_ARE_XY_VALUES`;
2. appends it to the glyph order, adds an `hmtx` entry, maps U+0040 to it in every
   Unicode cmap subtable, and recomputes its bbox (`recalcBounds`);
3. saves `composite.ttf`; then
4. `fontTools.ttLib.woff2.compress("composite.ttf", "composite.woff2")`.

(The `post` table is format 3.0, so the added glyph carries no stored name; on reload
fontTools auto-names it `at` from its cmap entry. The Go decoder resolves '@' via the
cmap, so the name is immaterial.)

### Verification

| file             | size (bytes) | first 4 bytes | notes                                   |
|------------------|-------------:|---------------|-----------------------------------------|
| `composite.ttf`  |       10 804 | `00 01 00 00` | 55 glyphs; glyph at U+0040 is composite |
| `composite.woff2`|        6 124 | `wOF2`        | glyf transform; composite intact        |

Both round-trip back to a `TTFont` whose U+0040 glyph is composite with 2 components
(`A`@(0,0), `B`@(300,100)); the Go round-trip test
(`TestDecodeWOFF2CompositeGlyphRoundTrips`) asserts the WOFF2-reconstructed composite
outline matches the bare TTF's exactly and is non-empty.
