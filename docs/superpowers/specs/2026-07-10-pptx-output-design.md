# PPTX output writer (pkg/render/pptxwrite)

Plan area D2 — the write half of PPTX support (the any⇄any principle). A cssbox STRUCTURE
writer (boxwalk-based, the Markdown/DOCX/RTF shape): a document REFLOWS into slides, it is not
paginated onto them.

## The slide mapping

- **Every `<h1>`/`<h2>` starts a NEW slide** with that heading as the title placeholder
  (`p:ph type="title"` — the reader's `IsTitle`); content before the first heading yields an
  untitled slide; an empty document yields one empty slide.
- The blocks that follow become the slide's body: one text box accumulating paragraphs and
  list items (kind/depth on `a:buChar`/`a:buAutoNum` + `lvl` — exactly what pkg/pptx reads
  back), each table as a native `a:tbl` graphic frame (origin cells carry
  `gridSpan`/`rowSpan`, covered slots are `hMerge`/`vMerge` continuation cells — the reader's
  span model; header-row runs bolded), each image as a `p:pic` media part (deduped; data: URIs
  embed loaderless via `resource.LoadDataURL`, else the source's resource loader, else alt
  text + log). A caption becomes a bold body paragraph above its table.
- Emphasis on `rPr b`/`i`. Geometry: default 16:9 (960×540pt), title at top, body shapes
  stacked top-to-bottom with estimated heights (reading order = y order, which the frontend
  re-sorts by; PowerPoint users can then restyle).
- **Documented degrades (logged)**: h3–h6 → bold body paragraphs (one title kind per slide);
  blockquote/code semantics flatten to plain paragraphs; hyperlink targets drop (text kept —
  pkg/pptx does not model links); hr skipped; over-tall slides overflow their frame (clipped
  visually, content preserved for conversion).

Deterministic OPC (fixed timestamps/order — the testdata/gen/pptx package shape our reader is
already tested against: presentation + minimal master/layout + slides + media; every shape
carries its own transform so nothing needs placeholder inheritance).

## Round-trip contract

Not byte-parity with the direct conversion (the slide model is lossier than DOCX/RTF — h1 and
h2 both become titles, which the frontend reads back as `<h2>`); the pin is per-construct
content + structure fragments: html→pptx→reopen→md preserves slide splits, emphasis markers,
bullet/numbered/nested list shapes, table grids with span duplication, untitled preambles,
embedded images, and the slide COUNT (PageCount == number of h1/h2 + preamble).

Landed with a D1 frontend fix the round trip exposed: `writeBulletRun` closed `</li>` before
opening a nested list, making the sublist a SIBLING of its item — the structure writers
(which look for nested lists inside item children) dropped nested items entirely. The list
now opens inside the still-open `<li>` (the RTF reader's assembly algorithm).

## Wiring

`FormatPPTX` output bit flips; `WritePPTX` + `PPTXOptions` + `ConvertHTMLToPPTX`;
`ConvertOptions.PPTX`; `Write` dispatch; CLI help. PPTX joins the convert matrix as BOTH
input (a gen-built titled deck) and output (reopen-verified through our reader).
`pptxout-basic` golden.
