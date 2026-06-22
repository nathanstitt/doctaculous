# External PDF fixtures (third-party, CC-BY-SA-4.0)

These five real-world PDFs are an **integration-test corpus** for the parser and
rasterizer — moderately complex documents from a spread of producers, chosen to
exercise both the parsing layer (xref tables vs. xref/object streams, varied
producers, rewritten structures) and the rendering layer (multipage, images,
composite fonts, page rotation).

## ⚠️ Licensing — kept separate on purpose

Unlike the rest of this MIT-licensed repository, these files come from
[py-pdf/sample-files](https://github.com/py-pdf/sample-files) and are licensed
**CC-BY-SA-4.0** (Creative Commons Attribution-ShareAlike). ShareAlike is a
copyleft term, so these files are **isolated under `testdata/external/`** and are
**not** covered by the repo's MIT license. The full license text travels with
them in `LICENSE-py-pdf-sample-files.txt`. Do not move these into the main source
tree or relicense them. They are test inputs only — never linked into the
shipped module — so they don't affect the library's MIT licensing.

Attribution: PDF samples © the py-pdf/sample-files contributors, CC-BY-SA-4.0.

## The fixtures

| File | Producer | Structure (verified) | Exercises |
| --- | --- | --- | --- |
| `pdflatex-4-pages.pdf` | pdfTeX 1.40.23 | **xref stream + ObjStm** | Multi-page parse via compressed object streams; the parallel render path |
| `multicolumn.pdf` | pdfTeX 1.40.21 | **xref stream + ObjStm** | Dense multicolumn text/vector layout (3p); text + path rendering, golden images |
| `imagemagick-images.pdf` | ImageMagick | classic xref table | 6 pages, 6 images — multipage image-decode path |
| `google-doc-document.pdf` | Skia/PDF (Chrome / Google Docs) | classic xref table | Type0/CIDFontType2 + Type3 composite fonts, 10 images; a non-TeX producer |
| `cropped-rotated-scaled.pdf` | pypdf (rewritten) | classic xref table | All four `/Rotate` values (0/90/180/270), cropping/scaling; rotated-render + rewritten structure. **Also carries blend state** (`/BM /Multiply`, `/ca 0.5`) — see note below |
| `pdflatex-forms.pdf` | pdfTeX 1.40.23 | classic xref table | **AcroForm** interactive fields (`Tx` text, `Btn` button/checkbox) + the Form XObject appearance streams that back them |
| `libreoffice-form.pdf` | LibreOffice 6.4 | classic xref table | **AcroForm** with the widest field coverage (`Tx`, `Btn`, `Ch` choice/dropdown) + Form XObjects, from a different producer |

Deliberately excluded from py-pdf/sample-files: the encrypted fixture
(`005-…-password`, encryption is out of scope for v1) and the 117-page GeoTopo
(too heavy for hermetic/fast tests).

## Note: blend modes / transparency are ignored (v1 out of scope)

`cropped-rotated-scaled.pdf` applies `/BM /Multiply` and `/ca 0.5` (50% fill
alpha) through the ExtGState `gs` operator. **v1 does not interpret blend modes
or transparency** (CLAUDE.md "Out of scope"), so the renderer skips that state
and logs `content: /ExtGState (gs) not applied`. The page still rasterizes — but
its golden image will look as if the Multiply/alpha were never there. Do **not**
read that golden as "blending works"; it is the graceful-degradation baseline.
`TestExternalBlendingDegradesGracefully` (in `pkg/doctaculous`) locks this in: it
asserts the page renders without error AND that the skip is reported via `Logf`.

When transparency moves in scope, add a dedicated *generated* fixture in
`testdata/gen` with known overlapping `/BM /Multiply` + `/ca` regions so the
expected output is controlled and hermetic, rather than relying on this file.

(`google-doc-document.pdf` contains `/BM /Normal` + `/ca 1` — the no-op opaque
defaults — and `/SMask` image alpha, but no meaningful blending. The three
pdfTeX/ImageMagick files use no transparency at all.)

## Note: "xforms" — two distinct PDF features, both covered

The corpus covers the two things "xforms" can mean:

- **Form XObjects** (`/Subtype /Form`, reusable content streams invoked with the
  `Do` operator) — a core *render* path the interpreter must handle. Exercised by
  `cropped-rotated-scaled.pdf` (Form-XObject-heavy) and the appearance streams in
  both form files below.
- **AcroForm interactive fields** (text/checkbox/dropdown widgets) — *structure*
  the parser must traverse; the widgets themselves are annotations, out of scope
  to render in v1. `pdflatex-forms.pdf` covers `Tx`/`Btn`; `libreoffice-form.pdf`
  adds `Ch` (choice). Both still rasterize their page content without error — the
  interactive widgets degrade gracefully (page draws; field UI is not painted).
  `pdflatex-forms.pdf` in fact renders an essentially blank page because its page
  text uses an embedded font program format that is out of scope for v1
  (`ErrUnsupportedFontProgram`); that is expected, not a regression.

When AcroForm widget rendering moves in scope, prefer a *generated* fixture in
`testdata/gen` with known field geometry so expected output stays hermetic.

## Verifying

```sh
# header + EOF sanity
head -c 8 <file>.pdf      # => %PDF-1.x
tail -c 16 <file>.pdf     # => ...%%EOF
```

The two pdfTeX files have **no classic `xref` keyword** — their page objects live
inside object streams, so a naive `grep /Type /Page` finds zero pages. That is
intentional: it is the xref-stream/ObjStm path the parser must handle.
