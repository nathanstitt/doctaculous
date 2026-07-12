# pkg/docx model-level writer: Write/Bytes + the round-trip contract

Second of three PRs making pkg/docx a supported public read+write DOCX document model (PR 1:
reader fidelity, `2026-07-10-docx-model-fidelity-design.md`; PR 3: unify pkg/render/docxwrite
onto this writer). This is the API the tinycld text server builds its save path on: construct a
`*docx.Document` from their ProseMirror tree, `docx.Write` it — replacing wordZero, their
marker-based run rewriting, hyperlink rel post-processing, and zip injection.

## API

- **`Write(w io.Writer, doc *Document) error` / `Bytes(doc) ([]byte, error)`** in pkg/docx
  itself (encoding/json symmetry — one package owns both directions and the round-trip
  invariant; the package stays stdlib-only). Deterministic: fixed part order and zip
  timestamps, sorted iteration of every map — equal models produce byte-identical packages.
  doc is read-only during Write.
- **`ErrInvalidDocument`** — hard failure for content the writer cannot represent faithfully
  (a Drawing whose relationship resolves to no media part, a Hyperlink naming an unknown rel
  id, a Revision with an unknown kind, a nil document). Never a silent drop: a caller's save
  cycle must not lose data quietly.
- **Construction helpers**: `DefaultStyles()` (the curated table whose ids the cssbox lowering
  recovers semantics from — HeadingN/Quote/CodeBlock/HorizontalRule/CodeChar/Hyperlink — with
  docxwrite's visual identities), `(*Document).AddImage(name, data) relID`, plus PR 1's
  `NewNotes`/`NewNumbering`.
- **Package doc (doc.go)** declares the vocabulary and the stability promise: additive-only
  from here — the API is consumed externally.

## Emission decisions

- Direct string assembly with explicit escapers (the repo's serializer idiom — encoding/xml
  cannot emit the prefixed `<w:p>` form real consumers expect). Property children follow
  ECMA-376 schema order (pStyle → pageBreakBefore → framePr → numPr → tabs → spacing → ind →
  jc → sectPr → pPrChange; the rPr and tcPr equivalents likewise).
- **Relationships**: the document's own rels are re-emitted verbatim (ids preserved, internal
  targets relativized against word/); structural rels (styles/numbering/footnotes/endnotes/
  comments/customXml) are ensured by type, allocating fresh ids only when absent; a
  Target-only hyperlink gets an allocated external rel deduped per URL. Header/footer part
  names come from the referencing rel (or are synthesized for hand-built maps).
- **Tabs**: the parser folds `w:tab` into `"\t"`, so the writer maps tab characters back to
  `<w:tab/>`; `xml:space="preserve"` is emitted per segment when whitespace is significant.
- **delText**: run text inside a `w:del` revision emits `w:delText` (deletion-ness lives on
  the wrapper, exactly mirroring the parser).
- **Drawings** emit the full Word-required scaffold (docPr/nvPicPr/spPr, unique ids);
  anchored drawings emit positionH alignment + the wrap element (square/topAndBottom/tight/
  through/none).
- **Zero SectionProps** means "unspecified" and emits Word's Letter defaults — the same
  defaults the parser substitutes, so hand-built documents produce a valid page.
- **highlight**: `RunProps.HighlightName` (the raw parsed `w:val`, e.g. `darkGreen`) is
  preferred when set, round-tripping the exact token; a hand-built doc that sets only the RGBA
  falls back to the fixed 16-name palette, emitting only on an exact RGBA match.
- **note separators**: the two reserved footnote/endnote notes (ids -1 and 0) carry a
  `Run.NoteSep` instead of text; the writer emits `<w:separator/>` / `<w:continuationSeparator/>`
  and the parser reads them back, so a content-less separator run is a round-trip fixed point
  rather than being culled by writeRun's empty-run guard.

## The round-trip contract (pinned in write_test.go + write_idempotence_test.go)

`Parse(Write(doc)) ≡ doc` over the vocabulary, with the documented normalizations (doc.go):
allocated hyperlink ids (links compare by URL), parse-shaped runs (text OR break/reference —
the parser splits combined runs), at-least-one-block cells, zero-section defaults, empty style
Type = paragraph.

- **modelCore**: 15 constructed fixtures, one per feature family — paragraph props, run
  toggles (incl. explicit-off), hostile text shapes (escapes, tabs, leading/trailing space),
  hyperlinks (external dedup + internal anchor), revisions (nested, delete), property changes,
  comments, foot/endnotes, frames, tables (spans, vMerge chains, border styles, tracked cell
  changes), numbering (start + override), drawings (inline + anchored float via AddImage),
  DefaultStyles, multi-section with headers/footers, ExtraParts — each DeepEqual after one
  normalization pass.
- **Randomized sweep**: 200 seeded documents (hostile text pool, random props, nested
  revisions, comment ranges) through the same assertion — catches escaping/ordering bugs
  fixture tests miss.
- **Idempotence on parsed documents** (external test package — the fixture builder imports
  pkg/docx for the model-specimen fixture, so an in-package import would cycle): for every
  gen/docx Core fixture, Parse∘Write is a fixed point AND the second write is byte-identical
  to the first; every parsed relationship survives verbatim. (No real-world .docx corpus
  exists under testdata/external yet — it holds only PDFs; the gen corpus is the sweep.)
- **Determinism**: two writes of each modelCore fixture are byte-identical.
- **Visual**: the `model-specimen` core fixture builds a document through the public model
  (styles, emphasis, shading, hyperlink, footnote, tracked change, start-at-4 list, bordered
  table) and serializes with `docx.Bytes` — the golden locks model → Write → Parse → cascade →
  lower → raster end to end (eyeballed).

## Adoption notes (text server)

bootstrap: `docx.OpenBytes` → walk the model into their ProseMirror tree. flush: build a
Document (DefaultStyles + AddImage with their resolver +
`ExtraParts["customXml/tinycld-suggestions.xml"]` for app-owned suggestion metadata) →
`docx.Write`. Their blank.docx is `docx.Bytes` of a one-empty-paragraph document.
