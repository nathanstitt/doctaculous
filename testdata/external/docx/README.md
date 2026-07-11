# External DOCX fixtures (third-party; Apache-2.0 / MPL-2.0)

Real-world Word- and LibreOffice-authored documents forming the **save-cycle
integration corpus** for `pkg/docx`: each file must parse, and `Parse ∘ Write`
must be a fixed point (model-equal reparse, byte-identical second write) — the
contract `write_idempotence_test.go` pins over this directory, exactly as it
does over the generated corpus. `pkg/doctaculous`'s external corpus test also
converts and rasterizes them. Producers were verified from each file's
`docProps/app.xml` at download time.

## ⚠️ Licensing — kept separate on purpose

Unlike the rest of this MIT-licensed repository, these are third-party files
under their source projects' licenses, **isolated under `testdata/external/`**
(the same arrangement as the CC-BY-SA PDF corpus next door). They are test
inputs only — never linked into the shipped module — so they don't affect the
library's MIT licensing. The license texts travel with them:

- **Apache-2.0** (`LICENSE-apache-2.0.txt`) — files from the
  [Apache POI](https://github.com/apache/poi) test corpus
  (`test-data/document/` at commit `dfa53082b3e`).
- **MPL-2.0** (`LICENSE-MPL-2.0.txt`) — files from the
  [LibreOffice core](https://github.com/LibreOffice/core) Writer test corpus
  (`sw/qa/extras/ooxmlexport/data/` at commit `c8f2e03a4f4`). MPL is a
  file-level copyleft: these four files remain MPL-2.0; do not move them into
  the main source tree or relicense them.

## The fixtures

| File | Source / license | Producer | Exercises |
| --- | --- | --- | --- |
| `groupshape-trackedchanges.docx` | LibreOffice / MPL-2.0 | Word | real `w:ins`/`w:del` tracked changes (4 revisions) in a group shape |
| `redline-range-comment.docx` | LibreOffice / MPL-2.0 | Word | a revision and a comment on the same range |
| `UnknownStyleInRedline.docx` | LibreOffice / MPL-2.0 | **LibreOffice 6.4** | Writer-authored redlines referencing an unknown style |
| `CommentReply.docx` | LibreOffice / MPL-2.0 | **LibreOffice 7.6** | Writer-authored threaded comment replies |
| `comment.docx` | POI / Apache-2.0 | **LibreOffice 6.0** | Writer-authored comment (in POI's Apache-licensed corpus) |
| `endnotes.docx` | POI / Apache-2.0 | Word | 5 endnotes |
| `footnotes.docx` | POI / Apache-2.0 | Word | 5 footnotes |
| `ComplexNumberedLists.docx` | POI / Apache-2.0 | Word | deep multi-level numbering |
| `NumberingWOverrides.docx` | POI / Apache-2.0 | Word | numbering level overrides |
| `headerFooter.docx` | POI / Apache-2.0 | Word (Mac) | headers/footers from a second producer |
| `FieldCodes.docx` | POI / Apache-2.0 | Word | field codes |
| `Styles.docx` | POI / Apache-2.0 | Word | style-cascade-heavy document |
| `TestTableCellAlign.docx` | POI / Apache-2.0 | Word | table cell properties/alignment |
