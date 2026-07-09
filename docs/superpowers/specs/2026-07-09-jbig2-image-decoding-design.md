# JBIG2 image decoding for the PDF pipeline

**Status:** design approved, ready for implementation plan
**Date:** 2026-07-09
**Author:** Nathan Stitt

## Context

PDFs can compress images with `JBIG2Decode` — a bilevel (1-bit black/white) format
built for **scanned document pages** (ITU T.88 / ISO-IEC 14492). Our `pkg/pdf/filter`
decodes the common filters (Flate, LZW, DCT/JPEG, CCITTFax G3/G4) but treats JBIG2 as an
unsupported image filter: `decodeImageXObject` (`pkg/render/raster/page.go`) hits its
`default: unsupported image filter` case and the image is skipped (blank), the rest of
the page rendering fine. For a scanned PDF — where the JBIG2 image often *is* the whole
page — that means a blank render.

There was no pure-Go, permissively-licensed JBIG2 decoder for a long time (UniPDF is
commercial; the maze.io fork is AGPL; jbig2dec/pdfium are C/C++), which is why this sat
on the TODO. That changed: **`github.com/xiaoqidun/jbig2`** is a new pure-Go, Apache-2.0,
zero-CGo JBIG2 decoder. A spike validated it — a **pixel-perfect decode of real ITU T.88
conformance data** (a scanned US patent, exercising arithmetic-coded generic regions and
symbol-dictionary text regions) — and it exposes `NewDecoderWithGlobals(r io.Reader,
globals []byte)` + `Decode() (image.Image, error)`, which maps exactly onto a PDF JBIG2
stream + its `/JBIG2Globals`. This spec integrates that library to render JBIG2 images.

## Goals / non-goals

- **Goal:** decode `JBIG2Decode` PDF image XObjects (and inline images) to pixels, so
  scanned PDFs render. All JBIG2 region types (generic, refinement, text/symbol,
  halftone) come via the library; we do not special-case them.
- **Non-goal:** JBIG2 *encoding*; a standalone `.jb2`-file public API; JPEG2000/JPX
  (still no viable pure-Go library — explicitly remains pending).

## Constraints honored

Pure Go, no CGo. The library is **Apache-2.0** (MIT-compatible) and its only external
dependency is `golang.org/x/image`, which the project already uses — so no new module
enters `go.mod` (see Vendoring). Never panic on malformed input; degrade gracefully with
a logged skip; cover that behavior with a test.

## Design

### 1. Vendoring & attribution

The library is brand-new and solo-authored (1 star, days old, no upstream tests), so we
**vendor** it rather than take a `go get` dependency — insulating our build from the
upstream disappearing or changing, which Apache-2.0 explicitly permits.

- Copy the ~15 source files into `pkg/pdf/filter/jbig2/` (it is logically a filter). The
  files stay `package jbig2`; their import path becomes
  `github.com/nathanstitt/doctaculous/pkg/pdf/filter/jbig2`. No upstream `go.mod` entry.
- Preserve the upstream `LICENSE` and `NOTICE` verbatim in that directory, and add a
  `README.md` recording provenance: the source repo URL
  (`https://github.com/xiaoqidun/jbig2`), the exact pseudo-version copied
  (`v0.0.0-20260709020415-1bb076bd002c`), and the Apache-2.0 terms.
- **Full credit** in three places: the vendored dir (LICENSE/NOTICE/README), the project
  root `README.md` ("Third-party / vendored code" note linking the source repo), and
  `CLAUDE.md`'s approved-deps list (with the "why vendored" rationale).

### 2. Decode integration (the seam)

A new `pkg/pdf/filter/jbig2decode.go` (in the `filter` package, not the vendored
subpackage) exposes:

```go
// DecodeJBIG2 decodes a PDF JBIG2Decode image stream to a row-major, MSB-first,
// 1-bit-per-pixel buffer of width×height — the same shape decodeRawImage consumes for a
// 1-bpc DeviceGray image. data is the raw JBIG2 stream; globals is the /JBIG2Globals
// stream bytes (nil if absent). It never panics: a decoder panic or error is caught and
// returned as an error, and the caller skips + logs the image.
func DecodeJBIG2(data, globals []byte, width, height int) ([]byte, error)
```

Internally: wrap `jbig2.NewDecoderWithGlobals(bytes.NewReader(data), globals).Decode()`
in `defer/recover`; take the returned bilevel `image.Image` and repack it into the
MSB-first 1-bpp byte layout `decodeRawImage` expects. **Polarity:** JBIG2 uses set-bit =
black; PDF `/DeviceGray` 1-bpc uses 0 = black — so the repack inverts, mirroring what
CCITTFax already does.

Wiring in `decodeImageXObject` (`pkg/render/raster/page.go`). A JBIG2 stream can also be
an `/ImageMask` (a 1-bit stencil), and the pre-existing `isImageMask` branch runs *before*
the filter switch on the still-**undecoded** `data` — so JBIG2 must be decoded *first*,
before either the mask or the raw-image path consumes it. The change:

1. Right after `DecodedStream`, add: `if imgFilter == "JBIG2Decode" { decode to a 1-bpp
   buffer and REPLACE data with it, then clear imgFilter to "" }`. Concretely — read
   `/JBIG2Globals` from `/DecodeParms` (resolve the indirect stream, decode to bytes; nil
   if absent), call `filter.DecodeJBIG2(data, globals, w, h)`; on error return it (caller
   logs + skips); on success set `data = buf` and treat the rest of the function as a
   normal 1-bpc image.
2. The existing `isImageMask` branch then runs on the **decoded** 1-bpp buffer — so a
   JBIG2 `/ImageMask` stencil works correctly (unchanged code, correct input).
3. Otherwise flow falls into the `case ""` (raw) path: a 1-bpc `DeviceGray` image
   (`/ColorSpace` defaults to DeviceGray for bilevel JBIG2), honoring `/Decode` and
   `/SMask` via the existing `decodeRawImage` + `applySoftMask` — yielding `*image.RGBA`.

Placing the JBIG2 decode *before* the mask/raw branches (rather than adding a
`case "JBIG2Decode"` inside the switch) is what makes both the ImageMask and the normal
image cases correct with one insertion. The only JBIG2-specific code is the decode+repack;
everything downstream is shared. Inline images (`decodeInlineImage`) share this path
automatically.

### 3. Error handling

`DecodeJBIG2`'s `defer/recover` converts any panic inside the vendored (untrusted, ~150KB,
un-auditable-in-full) decoder into a returned error. `decodeImageXObject` propagates it;
the existing caller logs and draws nothing for that image, the page otherwise intact —
the standard image-decode-failure contract, plus a tighter recover right at the library
boundary so a crafted/truncated JBIG2 stream can never crash a render goroutine.

## Testing (the "C" plan: generated fixture + real-world PDF)

- **Hermetic generated fixture (primary — localizes failures):** commit a small real
  **generic-region-only** `.jb2` payload (from Apache PDFBox's Apache-2.0 conformance
  set) under `testdata/gen/`, provenance/license noted. Add a `gen.JBIG2ImagePDF()`
  builder that wraps those bytes into a minimal one-page PDF image XObject
  (`/Filter /JBIG2Decode`, `/Width`/`/Height`, `/ColorSpace /DeviceGray`,
  `/BitsPerComponent 1`) — the PDF wrapper is generated deterministically, the JBIG2
  payload is real. Add it to `gen.Core` so it flows through the standard parser-roundtrip
  + golden-render sweep. Commit its golden PNG (eyeballed).
- **`pkg/pdf/filter` unit test:** decode the payload directly via `DecodeJBIG2`; assert
  dimensions and known pixel values.
- **Real-world integration fixture:** commit one small real scanned PDF with embedded
  JBIG2 under `testdata/external/pdf/` (provenance/license in the PR), covered by the
  existing external-corpus render test — proving the full path incl. symbol dictionaries
  and `/JBIG2Globals` on a genuine document.
- **JBIG2 `/ImageMask` test:** a generated PDF fixture with a JBIG2-compressed 1-bit
  stencil (`/ImageMask true`), asserting it paints in the fill color — covering the
  decode-before-mask ordering (the case where undecoded bytes would otherwise reach
  `decodeImageMask`).
- **Graceful-degradation test:** feed a truncated/garbage JBIG2 stream; assert
  `DecodeJBIG2` returns an error (no panic) and the surrounding page still renders (image
  skipped + logged).
- **Vendored-package smoke test:** `pkg/pdf/filter/jbig2` builds and decodes a payload —
  so a future re-vendor of an upstream update surfaces breakage immediately.
- `go test -race ./...`, `go vet ./...`, `golangci-lint run` clean.

### Visual entry

JBIG2 is a PDF-input feature, so its visual entry is the **committed golden PNGs** (the
generated-fixture golden and the real-world PDF render), eyeballed in the PR — consistent
with how CCITT/DCT filters are visually covered by golden images rather than the
`testdata/htmldoc/` HTML showcase.

## Docs

- `CLAUDE.md`: move JBIG2 from TODO to Done under the Filters bullet (vendored pure-Go
  Apache-2.0 decoder); note **JPX/JPEG2000 remains pending** (no viable pure-Go option).
  Add the vendored library to the approved-deps list with its rationale.
- Root `README.md`: third-party attribution note linking the source repo.
- This design spec, committed under `docs/superpowers/specs/`.

## Follow-ups / out of scope

- **JPEG2000 / JPXDecode** — no pure-Go decoder exists; stays pending.
- **OCR of scanned pages** — a decoded JBIG2 image is a bitmap; extracting *text* from a
  scan still needs OCR, a separate arc (relates to the PDF-extraction TODO).
- Upstream-update workflow — a future re-vendor is manual; the smoke test guards it.
