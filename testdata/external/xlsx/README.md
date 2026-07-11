# External XLSX fixtures (third-party; Apache-2.0 / MPL-2.0 / MIT)

Real-world Excel- and LibreOffice-authored workbooks forming the **preservation
integration corpus** for `pkg/xlsx`: each file must open through the reader,
survive a no-op `Edit`+`Save` **part-for-part byte-identical**, and reopen after a
real cell edit (`external_test.go` in `pkg/xlsx` pins this, plus per-file feature
counts; `pkg/doctaculous`'s external corpus test also converts and rasterizes
them). Producers were verified from each file's `docProps/app.xml` at download
time.

## ⚠️ Licensing — kept separate on purpose

Unlike the rest of this MIT-licensed repository, these are third-party files
under their source projects' licenses, **isolated under `testdata/external/`**
(the same arrangement as the CC-BY-SA PDF corpus next door). They are test
inputs only — never linked into the shipped module — so they don't affect the
library's MIT licensing. The license texts travel with them:

- **Apache-2.0** (`LICENSE-apache-2.0.txt`) — files from the
  [Apache POI](https://github.com/apache/poi) test corpus
  (`test-data/spreadsheet/` at commit `dfa53082b3e`).
- **MPL-2.0** (`LICENSE-MPL-2.0.txt`) — files from the
  [LibreOffice core](https://github.com/LibreOffice/core) Calc test corpus
  (`sc/qa/unit/data/xlsx/` at commit `c8f2e03a4f4`). MPL is a file-level
  copyleft: these two files remain MPL-2.0; do not move them into the main
  source tree or relicense them.
- **MIT** (`LICENSE-open-xml-sdk-mit.txt`) — one file from the
  [dotnet/Open-XML-SDK](https://github.com/dotnet/Open-XML-SDK) test assets
  (`test/DocumentFormat.OpenXml.Tests.Assets/assets/TestDataStorage/O14ISOStrict/Excel/`
  at commit `00967dc871f`).

## The fixtures

| File | Source / license | Producer | Exercises |
| --- | --- | --- | --- |
| `ExcelPivotTableSample.xlsx` | POI / Apache-2.0 | Excel | 2 pivot tables + caches across 3 sheets |
| `NewStyleConditionalFormattings.xlsx` | POI / Apache-2.0 | Excel | 18 CF rules incl. x14 data bars / icon sets / color scales — the opaque-`Raw` passthrough |
| `55406_Conditional_formatting_sample.xlsx` | POI / Apache-2.0 | Excel (Mac) | classic CF from a second producer |
| `WithConditionalFormatting.xlsx` | POI / Apache-2.0 | Excel | CF across 3 sheets |
| `SimpleWithComments.xlsx` | POI / Apache-2.0 | Excel | 3 cell notes + legacy VML |
| `WithVariousData.xlsx` | POI / Apache-2.0 | Excel | mixed data types, 2 notes |
| `Themes.xlsx` | POI / Apache-2.0 | Excel | theme palette |
| `50784-font_theme_colours.xlsx` | POI / Apache-2.0 | Excel | theme + tint font colors (the facet naive editors lose) |
| `autofilter.xlsx` | LibreOffice / MPL-2.0 | **LibreOffice 5.4** | Calc-authored package shape, autofilter |
| `cell-note.xlsx` | LibreOffice / MPL-2.0 | **LibreOffice 25.2** | Calc-authored cell note |
| `filter_type.xlsx` | Open-XML-SDK / MIT | Excel (ISO strict) | the **ISO-strict** OOXML namespaces (`purl.oclc.org`) |
