# Colour-font fixture generation

The two colour-emoji fixtures in the parent directory are **subsets** of upstream
Noto Color Emoji, cut down to five codepoints so they can live in the repo:

| Fixture | Upstream | Colour format | Size |
| --- | --- | --- | --- |
| `NotoColorEmoji-COLRv1.ttf` | [`noto-emoji`](https://github.com/googlefonts/noto-emoji) `fonts/Noto-COLRv1.ttf` | `COLR` v1 + `CPAL` (layered vector) | 4.8 MB → **31 KB** |
| `NotoColorEmoji-CBDT.ttf` | [`noto-emoji`](https://github.com/googlefonts/noto-emoji) `fonts/NotoColorEmoji.ttf` | `CBDT`/`CBLC` (PNG strikes) | 10.2 MB → **29 KB** |

Both are SIL OFL 1.1; the licence ships beside them as `LICENSE-NotoColorEmoji.txt`,
as the OFL requires. They are used verbatim under the reserved name.

Kept codepoints: U+1F600 grinning face, U+1F389 party popper, U+2764 heavy black
heart, U+1F44D thumbs up, U+1F31F glowing star. Between them they exercise flat
`PaintSolid` layers, a `PaintLinearGradient`, a `PaintRadialGradient`, nested
`PaintColrGlyph` references, and `PaintColrLayers` — so the paint-graph walk is
covered by real data rather than by a font we generated ourselves to match our own
reading of the spec.

## Why these are subsets, not the shipped fonts

Upstream is 15 MB for the pair, against a ~1.6 MB fixture directory. The scripts here
cut that to 60 KB while keeping the *original* table bytes for everything they retain
— no re-encoding of outlines, paints, or bitmaps — so what the tests parse is what a
real font contains.

## Regenerating

Fetch the two upstream fonts into this directory, then:

```sh
python3 subset.py     # drop unrelated tables; keep reachable glyphs
python3 colrsub.py    # extract the wanted base glyphs' paint subgraphs from COLR
python3 mkcolr.py     # assemble the COLRv1 font
python3 compact.py    # renumber glyphs densely (shrinks loca/hmtx)
python3 cbdt.py       # rebuild CBLC index subtables over the kept bitmaps
```

No `fonttools` dependency — these are plain-stdlib scripts, in keeping with the
project's pure-Go/no-extra-tooling stance for test data.

## The one thing to be careful about

`colrsub.py` copies paint records by their fixed size and rewrites the offsets inside
them. Those sizes were **verified empirically** — by measuring the gap between
consecutive distinct paint offsets in the real font — not derived by reading the spec.
Three of the hand-derived guesses (formats 2, 12, 14) were wrong, and a wrong size
silently produces a table that parses and paints the wrong thing. If a new paint
format appears in a future upstream font, measure it the same way rather than
trusting a reading.
