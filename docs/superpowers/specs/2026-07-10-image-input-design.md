# PNG/JPEG input: images as single-page documents

The plan's area H — the any⇄any principle applied to the two image formats, which were
output-only. An image opens as a document whose single page is EXACTLY the image's pixel size
(1 px = 1 pt): image→PDF yields a page the image fills edge to edge, image→DOCX/PPTX/EPUB
embed the picture, image→JPEG/PNG transcode through the raster path, and the structurally
lossy pairs keep the documented degrade story (markdown carries the image as a data: URI;
plain text and the tables-only writers produce empty output by design).

## Implementation

`OpenImage*` (pkg/doctaculous/image_frontend.go): `image.DecodeConfig` reads the intrinsic
size and the actual encoding (the FORMAT STAMPS FROM THE BYTES — a JPEG detected while
opening "as PNG" stamps FormatJPEG); the frontend synthesizes a margin-zero page exactly the
image's size with one `<img>` carrying the bytes as a data: URI (the loaderless data:-image
support from the RTF PR), through the reflow pipeline so every option and output follows.
`openDetected` routes FormatPNG/FormatJPEG here (their magic rows already existed in
detectMagic — they simply used to error); the input capability bits flip; same-format
conversion remains a deliberate `ErrSameFormat` on the generic path.

Behavior changes to existing error paths, pinned by updated tests: `Open("x.png")` now
returns a document instead of ErrUnsupportedFormat, and forcing an image format on non-image
bytes fails in the decoder rather than as an unsupported format.

## Tests

Pixel contract (a 40×24 PNG renders at 72 DPI to a 40×24 page whose pixels are the image's),
PageSize equals the pixel size, detection routing, decode-error path; conversion pins for
png→pdf (page exactly 30×20pt), png→jpeg transcode signatures, png→png ErrSameFormat,
png→markdown data-URI carriage; the conversion matrix gains image rows (text output empty by
design for images; tables-only likewise).
