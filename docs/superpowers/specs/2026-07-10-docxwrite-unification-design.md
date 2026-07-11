# docxwrite unification: build *docx.Document, serialize via docx.Write

Third of three PRs making pkg/docx the one supported DOCX read+write model (PR 1: reader
fidelity, `2026-07-10-docx-model-fidelity-design.md`; PR 2: the model writer,
`2026-07-10-docx-model-writer-design.md`). `pkg/render/docxwrite` no longer accumulates
WordprocessingML strings and assembles its own zip package — its cssbox walk now BUILDS a
`*docx.Document` (paragraphs/runs/hyperlinks/tables/drawings with `DefaultStyles()` +
`AddImage`) and serializes by calling `docx.Write`: ONE OPC emitter for the whole repo.

## What changed

- **Public API unchanged**: `docxwrite.Write(ctx, root, w, opts)` + `Options` exactly as
  before; pkg/doctaculous's `WriteDOCX` is untouched.
- **Walk → model, not XML.** The block walk appends `docx.Block`s to a sink slice (the
  body, or a table cell's `Blocks` — the recursion the string emitter faked by nesting
  markup). Headings/Quote/Caption/CodeBlock/HorizontalRule/ListParagraph ride on the same
  pStyle ids; emphasis/link chrome are direct `RunProps`; inline code carries the CodeChar
  rStyle + mono `Family`; hyperlinks become `ParaChild.Hyperlink` with `Target` set
  (docx.Write allocates the deduped external rels); lists become `HasNum`/`NumID`/`ILvl`
  paragraphs over a constructed `docx.Numbering` (same instance-per-ordered-list scheme);
  tables become `docx.Table` with grid widths, `GridSpan`, explicit `VMerge` chains,
  per-cell `Borders`/`Shading`, `IsHeader` rows, `LayoutFixed`; images embed via
  `doc.AddImage` + `ParaChild.Drawing`. Code-block runs are built parse-shaped (text OR
  break per run), so the constructed model round-trips literally.
- **Image callback → pending queue.** `boxwalk.CollectRuns`'s image callback returns a
  string literal; the writer now queues the model child (Drawing, or the alt-text Run on
  degrade) and returns a marker — `paraChildren` pops one queued child per marker run, in
  order. The shared boxwalk seam is untouched.
- **Deleted**: opc.go (zip assembly), styles.go (styles.xml emitter — `DefaultStyles()`
  is the same table), numbering.go (numbering.xml emitter — replaced by a `docx.Numbering`
  builder in list.go), xml.go (escapers). Net −520/+408 lines across the refactor.

## pkg/docx additive model extensions (facts the old XML carried that the model lacked)

Each parsed + written + covered by extended modelCore fixtures (round-trip stays green);
none is consumed by rendering, so all raster goldens are untouched:

- **`ParagraphProps.Borders *BoxBorders`** (w:pBdr, four edges; between/bar unmodeled) —
  `DefaultStyles()`'s HorizontalRule now carries its visible bottom rule (single, sz 6,
  D8DEE4). Merged in the style cascade's `mergePara` for consistency.
- **`NumLevel.IndentLeft/Hanging`** (+Has flags; w:lvl > w:pPr > w:ind) — Word's
  per-level nested-list indentation (720·(lvl+1) left, 360 hanging, as before).
- **`TableProps.LayoutFixed`** (w:tblLayout type="fixed") — the grid widths stay
  authoritative in Word instead of auto-fit.

## Semantics: 1:1, not byte-identical

The parity matrix (html→docx→md ≡ html→md), TestMarkdownToDOCXAndBack, TestPDFToDOCX, and
every behavior-level docxwrite unit test pass UNCHANGED. Output bytes differ (rel-id
allocation order, per-segment `xml:space`, omitted empty tcPr): the `docxout-basic.png` and
`docxout-htmldoc-p1.png` goldens are byte-identical; `htmldoc.docx.md` changed only a
media rel id (`rId3`→`rId1` — images allocate before the structural rels now).

One deliberate behavior change, pinned by a new test (TestLinkedImageKeepsImage): a
drawing inside a hyperlink is emitted BESIDE the link group (the model's `Hyperlink` holds
runs only). The old emitter nested it in `w:hyperlink`, where our own reader dropped the
drawing entirely — the image now survives the round trip; the link target on the image
itself is dropped (logged degrade).
