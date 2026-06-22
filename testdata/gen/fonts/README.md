# Test fonts

Real font programs used as fixtures for embedded-font parsing and glyph
rendering. All are popular and under permissive (MIT-compatible) licenses, in
keeping with the project's pure-Go / liberally-licensed constraints.

Three are TrueType (`glyf`/`loca`, quadratic outlines, sfnt `0x00010000`) and one
is OpenType-CFF (`CFF ` table, Type 2 cubic charstrings, sfnt `OTTO`) so the
parser/interpreter exercises both outline flavors PDFs embed.

| File                      | Family       | Outlines     | sfnt        | License                                  |
| ------------------------- | ------------ | ------------ | ----------- | ---------------------------------------- |
| `DejaVuSans.ttf`          | DejaVu Sans  | glyf/loca    | `0x00010000`| Bitstream Vera (permissive) + public domain — `LICENSE-DejaVu.txt` |
| `Roboto-Regular.ttf`      | Roboto       | glyf/loca    | `0x00010000`| Apache-2.0 — `LICENSE-Roboto.txt`        |
| `Inconsolata-Regular.ttf` | Inconsolata  | glyf/loca    | `0x00010000`| SIL OFL 1.1 — `LICENSE-Inconsolata.txt`  |
| `SourceSans3-Regular.otf` | Source Sans 3| CFF          | `OTTO`      | SIL OFL 1.1 — `LICENSE-SourceSans3.txt`  |

All four licenses permit redistribution within this repo. SIL OFL forbids selling
the fonts *on their own* (not a concern here) and reserves font names on
modification — we ship them verbatim. Each license file is committed alongside
its font.

## Sources

- DejaVu Sans — https://github.com/dejavu-fonts/dejavu-fonts
- Roboto — https://github.com/google/fonts (apache/roboto)
- Inconsolata — https://github.com/google/fonts (ofl/inconsolata)
- Source Sans 3 — https://github.com/adobe-fonts/source-sans

Verify the outline flavor of any file with:

```sh
# first 4 bytes: 00010000 / 'true' => TrueType glyf; 'OTTO' (4f54544f) => CFF
xxd -p -l 4 <file>
```
