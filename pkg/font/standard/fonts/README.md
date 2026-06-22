# Bundled standard-font substitutes

These font programs ship **inside the library** (via `//go:embed` in
`../standard.go`) and are used at runtime to render simple PDF fonts that declare
a standard-14 `/BaseFont` (or a common alias) but embed no font program.

All are permissively licensed and MIT-compatible — no GPL/AGPL.

| File | Substitutes for | Format | License |
| --- | --- | --- | --- |
| `TeXGyreHeros-Regular.pfb` | Helvetica / Arial (all weights → regular) | Classic Type 1 (PFB) | GUST Font License (LPPL-equiv.) — see `GUST-FONT-LICENSE.txt` |
| `TeXGyreTermes-Regular.pfb` | Times-Roman / TimesNewRoman (all weights → regular) | Classic Type 1 (PFB) | GUST Font License (LPPL-equiv.) — see `GUST-FONT-LICENSE.txt` |
| `Inconsolata-Regular.ttf` | Courier / CourierNew (all weights → regular) | TrueType (SFNT) | SIL Open Font License 1.1 — see `LICENSE-Inconsolata.txt` |

## Licensing rationale

- **GUST Font License** is an instance of the LaTeX Project Public License (LPPL):
  free use, modification, and redistribution; renaming on modification is
  *requested but not legally required*. It is genuinely permissive and
  MIT-compatible for shipping inside a library.
- **SIL OFL 1.1** permits embedding, modification, and redistribution; the only
  restriction (cannot be sold on its own) does not affect bundling inside this
  toolkit.

## Coverage / approximations

- Only **regular weights** are bundled. Bold/italic/oblique variants of a family
  map to that family's regular face — an intentional approximation. True
  weight/slant substitutes are a follow-up.
- **Symbol** and **ZapfDingbats** have no bundled look-alike; the library reports
  them unavailable and the caller skips the text (graceful degradation).
- Widths: the PDF's own `/Widths` are preferred when present; otherwise the
  substitute face's own advances approximate them.

Sources: TeX Gyre collection (GUST e-foundry, via CTAN); Inconsolata (The
Inconsolata Project Authors). These are copies of the corresponding files already
committed under `testdata/gen/fonts/`.
