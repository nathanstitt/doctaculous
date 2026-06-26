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
