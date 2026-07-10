# PPTX input (pkg/pptx): PresentationML decks as fixed-size pages

First half of the PPTX format (the plan's area D; the writer `pkg/render/pptxwrite` is D2).
Keeps drive's .pptx thumbnails working after go-fitz retires, and makes decks first-class
conversion inputs (pptx → pdf/md/... follow from the reflow pipeline).

## Reader (pkg/pptx — hand-rolled OPC + streaming XML, mirroring pkg/docx and pkg/xlsx)

Read-only cached content: presentation.xml (slide size in EMU → points, slide id order),
visible slides (a `show="0"` root attribute skips), and per slide the shape tree —
`p:sp` text frames (a:p paragraphs with level/alignment/bullet kind buChar/buAutoNum/buNone,
a:r runs with b/i/sz-hundredths/solidFill srgbClr), `p:pic` pictures (blip rel → media
bytes), `p:graphicFrame` tables (a:tbl with gridSpan/rowSpan and hMerge/vMerge continuation
cells). Shape frames come from `a:xfrm`; a placeholder without one INHERITS its frame through
slide → slideLayout → slideMaster, matched by placeholder type/idx (exact key, then
type-only, then idx-only). Out of scope, degrading gracefully: animations, transitions,
SmartArt, charts, group shapes, and theme-driven styling — the modeled content still
extracts.

## Frontend (OpenPPTX* → positioned HTML)

Each visible slide becomes one fixed-size page: `@page { size: slide; margin: 0 }` +
`WithPageSize(slide)` (a caller's own page size wins), a `position:relative` slide div per
page (`break-before: page` between slides), and each shape an absolutely positioned frame —
the CSS engine's positioning/pagination machinery does the layout. Pictures embed as data:
URIs (the loaderless data:-image support landed with RTF input). Title placeholders render
as `<h2>` headings; bulleted paragraph runs group into nested `<ul>`/`<ol>` (a bullet-kind
switch at the same level closes and reopens the list); tables carry their spans. For the
structure writers, shapes order TITLE-FIRST then top-to-bottom/left-to-right — a converted
deck reads sensibly (pinned: title precedes body in the markdown).

## Wiring

`FormatPPTX` (input-only until D2), `classifyOPC` now returns it for `ppt/`-shaped OPC zips
(the detect test's expectation flipped from Unknown), `.pptx`/`.pptm` extensions, `pptx` in
ParseFormat, the presentationml MIME row flipped from its deliberate-Unknown placeholder,
`openDetected` case, CLI help.

## Tests

pkg/pptx unit: slide size, hidden-slide skipping, title flags, run formatting (bold/italic/
size/color), EMU→pt frames, and layout-placeholder inheritance. pkg/doctaculous:
`pptx-specimen` golden — a two-slide deck (title + nested/numbered bullets + red run +
positioned picture; layout-inherited placeholder + spanned table) rendered and eyeballed;
detection + format stamping; pptx→markdown conversion pinning heading/list/table structure,
reading order, and hidden-slide exclusion. A small real-world .pptx fixture (Excel/Keynote-
produced) remains a follow-up alongside the xlsx real-file corpus.
