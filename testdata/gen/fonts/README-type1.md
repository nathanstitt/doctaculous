# Standalone font fixtures

Raw font programs for unit-testing the font parser (`pkg/font`) directly, outside
of any PDF. These are the *font program* formats PDFs embed via the
FontDescriptor's `/FontFile`, `/FontFile2`, and `/FontFile3` streams.

## Files

| File | Format | PDF embedding | License |
| --- | --- | --- | --- |
| `TeXGyreTermes-Regular.pfb` | **Classic Type 1** (PostScript, Type 1 charstrings, PFB) | `/FontFile` | GUST Font License (LPPL-equiv.) |
| `TeXGyreHeros-Regular.pfb` | **Classic Type 1** (PostScript, Type 1 charstrings, PFB) | `/FontFile` | GUST Font License (LPPL-equiv.) |

Termes is a Times-like serif; Heros is a Helvetica-like sans — two distinct
designs from the same foundry so glyph/charstring differences are exercised.

### What "Type 1" means here

PDFs use "Type1" loosely for two unrelated things; only the **classic** one is here:

- **Classic Type 1** — PostScript font program, Type 1 charstrings, eexec-encrypted
  private dict, distributed as `.pfb`/`.pfa`, embedded via `/FontFile`. **These
  files.** `pkg/font` parses these via `github.com/benoitkugler/textlayout`
  (`fonts/type1`); `TeXGyreTermes-Regular.pfb` backs the generated `embedded-type1`
  fixture (`gen.EmbeddedType1PDF`).
- **"Type1C" / CFF** — a bare CFF table (Type 2 charstrings) embedded via
  `/FontFile3 /Subtype Type1C`. Despite the name this is **not** a classic Type 1
  program; `pkg/font` parses it via the same library (`fonts/type1C`). Use an
  OTF/CFF font (e.g. an OpenType-CFF `.otf`) to exercise that path, not a `.pfb`.

### Verified

Both files are genuine PFB Type 1 programs, confirmed by:

```sh
# PFB segment marker (0x80 0x01) at byte 0:
xxd -l 2 TeXGyreTermes-Regular.pfb        # => 8001
# PostScript clearmark + Type 1 markers:
strings TeXGyreTermes-Regular.pfb | grep -E '%!PS-AdobeFont-1.0|/FontType 1|eexec'
```

They carry the `%!PS-AdobeFont-1.0` header, `/FontType 1`, and an `eexec`-encrypted
section — and are **not** CFF/OpenType (no `OTTO`/`CFF ` signature).

## Licensing

GUST Font License — an instance of the LaTeX Project Public License (LPPL),
genuinely permissive (free use, modification, and redistribution; renaming
requested on modification). MIT-compatible for committing as test data. Full text
in `GUST-FONT-LICENSE.txt`.

Source: TeX Gyre collection, GUST e-foundry, via CTAN (`tex-gyre.tds.zip`).
https://www.gust.org.pl/projects/e-foundry/tex-gyre
