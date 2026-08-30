# Test fonts

Real font programs used as fixtures for embedded-font parsing and glyph
rendering. All are popular and under permissive (MIT-compatible) licenses, in
keeping with the project's pure-Go / liberally-licensed constraints.

These cover the three outline flavors PDFs embed: TrueType (`glyf`/`loca`,
quadratic, sfnt `0x00010000`), OpenType-CFF (`CFF ` table, Type 2 charstrings,
sfnt `OTTO`), and classic Type 1 (PostScript, eexec, PFB `0x8001`).

| File                      | Family       | Outlines     | container   | License                                  |
| ------------------------- | ------------ | ------------ | ----------- | ---------------------------------------- |
| `DejaVuSans.ttf`          | DejaVu Sans  | glyf/loca    | `0x00010000`| Bitstream Vera (permissive) + public domain — `LICENSE-DejaVu.txt` |
| `Roboto-Regular.ttf`      | Roboto       | glyf/loca    | `0x00010000`| Apache-2.0 — `LICENSE-Roboto.txt`        |
| `Inconsolata-Regular.ttf` | Inconsolata  | glyf/loca    | `0x00010000`| SIL OFL 1.1 — `LICENSE-Inconsolata.txt`  |
| `SourceSans3-Regular.otf` | Source Sans 3| CFF          | `OTTO`      | SIL OFL 1.1 — `LICENSE-SourceSans3.txt`  |
| `TeXGyreTermes-Regular.pfb` | TeX Gyre Termes | Type 1 charstrings | PFB `0x8001` | GUST Font License (LPPL-equiv.) — `GUST-FONT-LICENSE.txt` |
| `TeXGyreHeros-Regular.pfb`  | TeX Gyre Heros  | Type 1 charstrings | PFB `0x8001` | GUST Font License (LPPL-equiv.) — `GUST-FONT-LICENSE.txt` |
| `NotoColorEmoji-COLRv1.ttf` | Noto Color Emoji | `COLR` v1 + `CPAL` (layered vector) | `0x00010000` | SIL OFL 1.1 — `LICENSE-NotoColorEmoji.txt` |
| `NotoColorEmoji-CBDT.ttf`   | Noto Color Emoji | `CBDT`/`CBLC` (PNG strikes) | `0x00010000` | SIL OFL 1.1 — `LICENSE-NotoColorEmoji.txt` |

All licenses permit redistribution within this repo. SIL OFL forbids selling the
fonts *on their own* (not a concern here) and reserves font names on modification
— we ship them verbatim. Each license file is committed alongside its font. See
`README-type1.md` for the classic-Type1 details.

## Sources

- DejaVu Sans — https://github.com/dejavu-fonts/dejavu-fonts
- Roboto — https://github.com/google/fonts (apache/roboto)
- Inconsolata — https://github.com/google/fonts (ofl/inconsolata)
- Source Sans 3 — https://github.com/adobe-fonts/source-sans
- TeX Gyre (Termes, Heros) — https://www.gust.org.pl/projects/e-foundry/tex-gyre
- Noto Color Emoji — https://github.com/googlefonts/noto-emoji

The two Noto Color Emoji files are **subsets** cut to five codepoints (4.8 MB and
10.2 MB upstream → 31 KB and 29 KB here), so the colour-font tests can run from
committed fixtures without a 15 MB repo cost. The retained table bytes are the
original ones; see `tools/README.md` for how they are produced and regenerated.

Verify the outline flavor of any file with:

```sh
# first 4 bytes: 00010000 / 'true' => TrueType glyf; 'OTTO' (4f54544f) => CFF
xxd -p -l 4 <file>
```
