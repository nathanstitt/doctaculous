# JBIG2 Image Decoding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render `JBIG2Decode` PDF images by vendoring the pure-Go Apache-2.0 `xiaoqidun/jbig2` decoder and wiring it into the image-decode seam.

**Architecture:** Vendor the library into `pkg/pdf/filter/jbig2/` (no new `go.mod` dep). A new `pkg/pdf/filter.DecodeJBIG2(data, globals, w, h)` wraps the vendored decoder (with a defensive `recover`) and returns a row-major MSB-first 1-bpp buffer. In `decodeImageXObject` (`pkg/render/raster/page.go`), JBIG2 is decoded **before** the ImageMask/raw branches so the decoded 1-bpp bytes flow through the existing 1-bpc-DeviceGray path (and the existing ImageMask stencil path) unchanged.

**Tech Stack:** Go 1.26, vendored `github.com/xiaoqidun/jbig2` v0.0.0-20260709020415-1bb076bd002c (Apache-2.0, pure Go; only dep `golang.org/x/image` which is already ours), existing `pkg/pdf`, `pkg/pdf/filter`, `pkg/render/raster`, `testdata/gen`.

---

## Key facts (verified against the codebase — read before starting)

- `filter.IsImageFilter("JBIG2Decode")` returns **true** (`pkg/pdf/filter/filter.go:115`), so `DecodedStream` leaves JBIG2 undecoded and returns `imageFilter="JBIG2Decode"` with the raw stream bytes. JBIG2 is handled at draw time in `decodeImageXObject`.
- `decodeImageXObject` (`pkg/render/raster/page.go:564`): calls `data, imgFilter, err := doc.DecodedStream(s)`, reads `w`/`h`, then **first** checks `if isImageMask(doc, s.Dict) { return decodeImageMask(data, w, h, ...) }` (line 577, on the still-raw `data`), then a `switch imgFilter` with `case "DCTDecode"`, `case ""` (raw → `resolveImageCS` + `decodeRawImage`), `default: unsupported`.
- **CRITICAL ORDERING:** because the ImageMask check runs before the switch on raw `data`, a JBIG2 `/ImageMask` would otherwise get undecoded bytes. JBIG2 must be decoded and `data` replaced BEFORE the ImageMask check.
- Reusable pieces in `pkg/render/raster`: `resolveImageCS(doc, csObj, bpc, logf) (imageCS, error)` (image.go:40), `imageDecodeArray(doc, o, bpc, cs) []float64` (page.go:734), `decodeRawImage(data, w, h, bpc, cs) (*image.RGBA, error)` (image.go:192), `applySoftMask(doc, s, base, logf)`, `toRGBA`.
- `/JBIG2Globals` lives in the stream's `/DecodeParms` dict as an indirect stream ref. Read it via: `parms := doc.GetDict(s.Dict["DecodeParms"])` then `gStream := doc.GetStream(parms["JBIG2Globals"])`, then `gBytes, _, _ := doc.DecodedStream(gStream)`. (`GetDict` at resolve.go:135, `GetStream` at resolve.go:166.) DecodeParms may also be `/DP`.
- The vendored lib API: `jbig2.NewDecoderWithGlobals(r io.Reader, globals []byte) (*jbig2.Decoder, error)` then `dec.Decode() (image.Image, error)` (single image — a PDF image is one JBIG2 page). The returned image is bilevel; **JBIG2 polarity is set-bit = black**, PDF DeviceGray 1-bpc is **0 = black**, so repacking must INVERT.
- `testdata/gen`: `buildImagePage(w, h, data, imgDictExtra string) []byte` (images.go:235) assembles a one-page PDF painting one image XObject. `CCITTImagePDF` (images.go:177) is the closest template (1-bpp bilevel). `gen.Core` (`testdata/gen/core.go:37`) is `[]CoreFixture{ {Name, Desc, Pages, Build func()[]byte} }`; `image-ccitt` is an entry. The raster golden test (`pkg/render/raster/golden_test.go` `TestGolden`) ranges over `gen.Core` and compares committed PNGs; run with `-update` to (re)generate, then eyeball.
- Sandbox note: `go` build-cache writes and network (`go get`, `curl`, `gh`) may fail with "operation not permitted" under the sandbox — retry those commands with the sandbox disabled.

---

## Task 1: Branch + vendor the library

**Files:**
- Create: `pkg/pdf/filter/jbig2/*.go` (copied), `pkg/pdf/filter/jbig2/LICENSE`, `pkg/pdf/filter/jbig2/NOTICE`, `pkg/pdf/filter/jbig2/README.md`

- [ ] **Step 1: Branch**

Run:
```bash
cd /Users/nas/code/docalizer
git checkout main && git pull --ff-only origin main
git checkout -b jbig2-image-decoding
```
(The design spec commit `dcca285` lives on `main` locally; if `git pull` reports it's not on origin yet, that's fine — it will arrive via this branch. If the branch already contains the spec, skip.)
NOTE: if the spec doc is NOT present after checkout, cherry-pick it: `git cherry-pick dcca285` (or re-create it later in Task 8).

- [ ] **Step 2: Fetch the upstream source at the pinned version into a temp dir**

Run (sandbox disabled — network):
```bash
cd /tmp/claude && rm -rf jbig2src && mkdir jbig2src && cd jbig2src
gh repo clone xiaoqidun/jbig2 . 2>/dev/null || git clone https://github.com/xiaoqidun/jbig2 .
git checkout 1bb076bd002c   # the pinned commit (from pseudo-version v0.0.0-20260709020415-1bb076bd002c)
ls *.go LICENSE NOTICE
```
Expected: ~15 `.go` files plus `LICENSE` and `NOTICE`. If the exact commit hash is unavailable, use the latest `master` and record the actual commit hash you used in the README (Step 5).

- [ ] **Step 3: Copy the .go source + LICENSE/NOTICE into the vendored dir**

Run:
```bash
cd /Users/nas/code/docalizer
mkdir -p pkg/pdf/filter/jbig2
cp /tmp/claude/jbig2src/*.go pkg/pdf/filter/jbig2/
cp /tmp/claude/jbig2src/LICENSE pkg/pdf/filter/jbig2/LICENSE
cp /tmp/claude/jbig2src/NOTICE  pkg/pdf/filter/jbig2/NOTICE
# Drop any *_test.go the upstream ships that reference nonexistent testdata (keep the package buildable/self-contained):
ls pkg/pdf/filter/jbig2/*_test.go 2>/dev/null && echo "NOTE: upstream test files present — see Step 4"
```

- [ ] **Step 4: Ensure the vendored package builds standalone**

The upstream files are `package jbig2` and import only `golang.org/x/image` + stdlib — their import path does NOT change (they don't import each other by full path; same-package files just reference symbols). Verify no file has an absolute self-import:
```bash
grep -rn "xiaoqidun/jbig2" pkg/pdf/filter/jbig2/*.go || echo "no self-imports (good)"
```
If any `*_test.go` was copied and fails to build (missing testdata), delete it — we add our own smoke test in Task 4:
```bash
rm -f pkg/pdf/filter/jbig2/*_test.go
```
Then:
```bash
gofmt -w pkg/pdf/filter/jbig2/*.go
go build ./pkg/pdf/filter/jbig2/
```
Expected: builds clean. If it needs `golang.org/x/image` at a newer version, run `go get golang.org/x/image@latest && go mod tidy` (this is an allowed pre-existing dep) and note the bump.

- [ ] **Step 5: Add the vendored README with provenance**

Create `pkg/pdf/filter/jbig2/README.md`:
```markdown
# Vendored: xiaoqidun/jbig2

A pure-Go, Apache-2.0 JBIG2 (ITU T.88 / ISO-IEC 14492) decoder, vendored into this
repository so the build never depends on the upstream remaining available.

- **Source:** https://github.com/xiaoqidun/jbig2
- **Version:** v0.0.0-20260709020415-1bb076bd002c (commit 1bb076bd002c)
- **License:** Apache-2.0 (see LICENSE and NOTICE in this directory)

We vendor rather than take a `go get` dependency because the upstream is a new,
solo-authored project; vendoring the Apache-2.0 source (which the license permits)
insulates our build and lets us pin/fix it in-tree. Its only external dependency is
`golang.org/x/image`, which the toolkit already uses, so no new module enters `go.mod`.

To update: re-copy the upstream `.go` + `LICENSE` + `NOTICE`, update the version above,
and run the package smoke test (`go test ./pkg/pdf/filter/jbig2/`).
```

- [ ] **Step 6: Confirm no new go.mod module + commit**

Run:
```bash
grep xiaoqidun go.mod && echo "UNEXPECTED: upstream in go.mod — remove it" || echo "good: not a module dep"
go build ./... 2>&1 | head
```
Expected: `good: not a module dep`; build clean. Then:
```bash
git add pkg/pdf/filter/jbig2 go.mod go.sum
git commit -m "vendor: xiaoqidun/jbig2 (pure Go, Apache-2.0) JBIG2 decoder"
```

---

## Task 2: `DecodeJBIG2` — the filter-package entry point

**Files:**
- Create: `pkg/pdf/filter/jbig2decode.go`
- Test: `pkg/pdf/filter/jbig2decode_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/pdf/filter/jbig2decode_test.go`:
```go
package filter

import "testing"

// TestDecodeJBIG2Garbage: a non-JBIG2 / truncated payload must return an error, never
// panic (the vendored decoder is wrapped in a recover). This is the graceful-degradation
// contract; a valid-payload test lives in the jbig2 sub-package + the render goldens.
func TestDecodeJBIG2Garbage(t *testing.T) {
	_, err := DecodeJBIG2([]byte("not a jbig2 stream"), nil, 8, 8)
	if err == nil {
		t.Fatal("DecodeJBIG2 on garbage returned nil error; want an error")
	}
}

// TestDecodeJBIG2Empty: empty input errors cleanly (no panic).
func TestDecodeJBIG2Empty(t *testing.T) {
	if _, err := DecodeJBIG2(nil, nil, 8, 8); err == nil {
		t.Fatal("DecodeJBIG2(nil) returned nil error; want an error")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/pdf/filter/ -run TestDecodeJBIG2 -v`
Expected: FAIL to compile — `undefined: DecodeJBIG2`.

- [ ] **Step 3: Implement `DecodeJBIG2`**

Create `pkg/pdf/filter/jbig2decode.go`:
```go
package filter

import (
	"bytes"
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/pdf/filter/jbig2"
)

// DecodeJBIG2 decodes a PDF JBIG2Decode image stream to a row-major, MSB-first,
// 1-bit-per-pixel buffer sized to width×height — the byte layout decodeRawImage consumes
// for a 1-bpc DeviceGray image. data is the raw JBIG2 stream; globals is the
// /JBIG2Globals stream bytes (nil when absent).
//
// JBIG2 uses set-bit = black; a PDF 1-bpc DeviceGray image uses sample 0 = black. This
// repacks with that inversion (a JBIG2 black pixel becomes bit 0), so downstream sees the
// conventional PDF bilevel image.
//
// It never panics: the vendored (third-party) decoder is wrapped in a recover, and any
// panic or error is returned as an error so the caller can skip the image and log.
func DecodeJBIG2(data, globals []byte, width, height int) (out []byte, err error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("jbig2: bad dimensions %dx%d", width, height)
	}
	defer func() {
		if r := recover(); r != nil {
			out = nil
			err = fmt.Errorf("jbig2: decoder panicked: %v", r)
		}
	}()

	dec, derr := jbig2.NewDecoderWithGlobals(bytes.NewReader(data), globals)
	if derr != nil {
		return nil, fmt.Errorf("jbig2: new decoder: %w", derr)
	}
	img, derr := dec.Decode()
	if derr != nil {
		return nil, fmt.Errorf("jbig2: decode: %w", derr)
	}

	// Repack the decoded bilevel image into MSB-first 1-bpp rows (byte-aligned per row),
	// inverting polarity (JBIG2 black=1 → PDF DeviceGray sample 0 = black).
	rowBytes := (width + 7) / 8
	out = make([]byte, rowBytes*height)
	b := img.Bounds()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// A JBIG2 "on" (black) pixel: gray == 0 (black) in the decoded image.
			// Read luminance; treat dark as black. Bounds-guard against a decoded image
			// smaller than the declared size.
			black := false
			if x < b.Dx() && y < b.Dy() {
				r16, g16, bl16, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				// average < half of 0xffff → dark → black
				if (r16+g16+bl16)/3 < 0x8000 {
					black = true
				}
			}
			// PDF sample: 0 = black. So set the bit to 1 for a WHITE pixel, 0 for black.
			if !black {
				out[y*rowBytes+x/8] |= 0x80 >> uint(x%8)
			}
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/pdf/filter/ -run TestDecodeJBIG2 -v`
Expected: PASS both (garbage/empty return errors; no panic).

- [ ] **Step 5: gofmt, vet, commit**

Run:
```bash
gofmt -w pkg/pdf/filter/jbig2decode.go pkg/pdf/filter/jbig2decode_test.go
go vet ./pkg/pdf/filter/
go test ./pkg/pdf/filter/ -run TestDecodeJBIG2
```
Then:
```bash
git add pkg/pdf/filter/jbig2decode.go pkg/pdf/filter/jbig2decode_test.go
git commit -m "filter: DecodeJBIG2 — decode a PDF JBIG2 image stream to 1-bpp (recover-guarded)"
```

---

## Task 3: Acquire a real JBIG2 payload + the generated fixture

**Files:**
- Create: `testdata/gen/jbig2/generic.jb2` (real payload), `testdata/gen/jbig2/README.md` (provenance)
- Modify: `testdata/gen/images.go` (add `JBIG2ImagePDF`), `testdata/gen/core.go` (add `image-jbig2` entry)
- Test: `pkg/pdf/filter/jbig2/jbig2_test.go` (vendored-package smoke test using the payload)

- [ ] **Step 1: Fetch a small, generic-region JBIG2 payload (Apache-2.0 conformance data)**

Run (sandbox disabled — network). Apache PDFBox's JBIG2 test set ships real `.jb2` files under Apache-2.0. Fetch a couple and keep the SMALLEST that decodes to a sensible image:
```bash
cd /tmp/claude
for f in 002 006; do
  gh api "repos/apache/pdfbox-jbig2/contents/src/test/resources/images/${f}.jb2" --jq '.content' 2>/dev/null | base64 -d > "${f}.jb2"
  echo "${f}.jb2: $(wc -c < ${f}.jb2) bytes"
done
```
These are multi-page symbol-dictionary scans (~150KB) — good for the REAL-WORLD fixture (Task 6), but larger than ideal for the committed generated fixture. For the generated fixture, prefer a SMALL single-image `.jb2`. Options, in order of preference:
  a. If a small (<20KB) generic-region `.jb2` is available in the pdfbox set (check `001`–`010` sizes), use it.
  b. Otherwise, use `002.jb2` but the fixture/golden will be a larger scanned page — acceptable; note the size in the README.

Copy the chosen payload:
```bash
cd /Users/nas/code/docalizer
mkdir -p testdata/gen/jbig2
cp /tmp/claude/002.jb2 testdata/gen/jbig2/generic.jb2   # or the smaller one you chose
```

- [ ] **Step 2: Record provenance**

Create `testdata/gen/jbig2/README.md`:
```markdown
# JBIG2 test payload

`generic.jb2` is a real JBIG2 (ITU T.88) bitstream used as the payload for the hermetic
`image-jbig2` fixture (the PDF *wrapper* is generated by `JBIG2ImagePDF` in
`../images.go`; only this compressed payload is committed, because we have no JBIG2
encoder).

- **Source:** Apache PDFBox JBIG2 ImageIO plugin test resources
  (https://github.com/apache/pdfbox-jbig2, `src/test/resources/images/`).
- **License:** Apache License 2.0.
```

- [ ] **Step 3: Add `JBIG2ImagePDF` to `testdata/gen/images.go`**

The payload is a standalone `.jb2` (file-organization). PDF JBIG2 streams use the
embedded organization, but the vendored decoder auto-probes the organization (it handles
both), so wrapping the raw `.jb2` bytes as the image stream works. Embed the payload with
`go:embed`. Add to `testdata/gen/images.go`:
```go
//go:embed jbig2/generic.jb2
var jbig2Generic []byte

// JBIG2ImagePDF returns a one-page PDF whose single image XObject is JBIG2-compressed
// (/Filter /JBIG2Decode), a 1-bpc DeviceGray bilevel image. The compressed payload is a
// real JBIG2 bitstream (committed under gen/jbig2/, provenance noted there); the PDF
// wrapper is generated here so the fixture is deterministic. The image's declared
// Width/Height must match the JBIG2 page's dimensions.
func JBIG2ImagePDF() []byte {
	// Width/Height of the committed payload's JBIG2 page. If you swap the payload, update
	// these to the new page's dimensions (decode it once to read them, e.g. via the
	// jbig2 smoke test's logging in Task 4).
	const w, h = 2320, 3408
	return buildImagePage(w, h, jbig2Generic,
		"/Filter /JBIG2Decode /ColorSpace /DeviceGray /BitsPerComponent 1")
}
```
Ensure `testdata/gen/images.go` imports `_ "embed"` (or `embed`) at the top — check its existing imports; if `//go:embed` is new to this file, add `"embed"` to the import block (blank import if only used for directives, but a package-level `var x []byte` with `//go:embed` needs the `embed` package imported, blank-form `_ "embed"`).

NOTE: `w, h` MUST equal the payload's JBIG2 page dimensions or the raw-image repack will mis-size. Determine them in Task 4's smoke test (it logs the decoded bounds) and set them here accordingly. If the chosen payload is multi-page, the decoder's `Decode()` returns the FIRST page — set w,h to that page.

- [ ] **Step 4: Register the fixture in `gen.Core`**

In `testdata/gen/core.go`, add after the `image-ccitt` entry:
```go
	{
		Name:  "image-jbig2",
		Desc:  "1-bpp bilevel image via JBIG2Decode (real payload, generated wrapper)",
		Pages: 1,
		Build: JBIG2ImagePDF,
	},
```

- [ ] **Step 5: Build (no golden yet — that's Task 5)**

Run:
```bash
gofmt -w testdata/gen/images.go testdata/gen/core.go
go build ./testdata/gen/
go vet ./testdata/gen/
```
Expected: builds clean.

- [ ] **Step 6: Commit (fixture generator + payload; the golden PNG comes in Task 5)**

```bash
git add testdata/gen/jbig2 testdata/gen/images.go testdata/gen/core.go
git commit -m "gen: image-jbig2 fixture (real JBIG2 payload, generated PDF wrapper)"
```

---

## Task 4: Vendored-package smoke test (also reads the payload's dimensions)

**Files:**
- Create: `pkg/pdf/filter/jbig2/jbig2_test.go`

- [ ] **Step 1: Write the smoke test**

Create `pkg/pdf/filter/jbig2/jbig2_test.go` (in the vendored package; it exercises the real payload and logs its dimensions so Task 3's `w,h` can be confirmed):
```go
package jbig2

import (
	"bytes"
	_ "embed"
	"testing"
)

//go:embed ../../../../testdata/gen/jbig2/generic.jb2
var payload []byte

// TestVendoredDecodeSmoke confirms the vendored decoder builds and decodes the committed
// real JBIG2 payload to a sane image. It also LOGS the decoded dimensions — use these to
// set JBIG2ImagePDF's w,h in testdata/gen/images.go.
func TestVendoredDecodeSmoke(t *testing.T) {
	dec, err := NewDecoder(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	img, err := dec.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	b := img.Bounds()
	t.Logf("decoded first page: %dx%d", b.Dx(), b.Dy())
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Fatalf("decoded image has non-positive size %dx%d", b.Dx(), b.Dy())
	}
}
```

- [ ] **Step 2: Run it and READ THE LOGGED DIMENSIONS**

Run: `go test ./pkg/pdf/filter/jbig2/ -run TestVendoredDecodeSmoke -v`
Expected: PASS, with a log line `decoded first page: WxH`.
**Action:** if the logged `WxH` does NOT match the `const w, h = 2320, 3408` you set in `JBIG2ImagePDF` (Task 3, Step 3), update those constants in `testdata/gen/images.go` to the logged values and re-commit that file (amend Task 3's commit or a fixup commit).

- [ ] **Step 3: Commit**

```bash
gofmt -w pkg/pdf/filter/jbig2/jbig2_test.go
git add pkg/pdf/filter/jbig2/jbig2_test.go
# if you corrected w,h:
git add testdata/gen/images.go 2>/dev/null
git commit -m "jbig2: vendored-package smoke test (decodes the committed payload)"
```

---

## Task 5: Wire JBIG2 into the image-decode seam (decode-before-mask/raw)

**Files:**
- Modify: `pkg/render/raster/page.go` (`decodeImageXObject`, ~line 564-579)

- [ ] **Step 1: Write the wiring**

In `pkg/render/raster/page.go`, in `decodeImageXObject`, insert a JBIG2 decode block AFTER the `w`/`h` validation (line 573) and BEFORE the `isImageMask` check (line 577). Replace:
```go
	w, _ := doc.GetInt(s.Dict["Width"])
	h, _ := doc.GetInt(s.Dict["Height"])
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("bad dimensions %dx%d", w, h)
	}

	// /ImageMask: a 1-bit stencil. Sample 0 paints the fill color, 1 is
	// transparent (default /Decode [0 1]); /Decode [1 0] inverts that.
	if isImageMask(doc, s.Dict) {
		return decodeImageMask(data, w, h, imageMaskInverted(doc, s.Dict), fill)
	}
```
with:
```go
	w, _ := doc.GetInt(s.Dict["Width"])
	h, _ := doc.GetInt(s.Dict["Height"])
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("bad dimensions %dx%d", w, h)
	}

	// JBIG2Decode: decode to a 1-bpp buffer up front and treat the image as a normal
	// 1-bpc bilevel image thereafter. This MUST happen before the /ImageMask and raw
	// branches, which consume `data` directly — a JBIG2 /ImageMask would otherwise get
	// the undecoded stream. On failure the whole image is skipped (returned error →
	// caller logs + draws nothing).
	if imgFilter == "JBIG2Decode" {
		globals := jbig2Globals(doc, s.Dict)
		buf, jerr := filter.DecodeJBIG2(data, globals, w, h)
		if jerr != nil {
			return nil, jerr
		}
		data = buf
		imgFilter = "" // now a raw 1-bpp bilevel image
	}

	// /ImageMask: a 1-bit stencil. Sample 0 paints the fill color, 1 is
	// transparent (default /Decode [0 1]); /Decode [1 0] inverts that.
	if isImageMask(doc, s.Dict) {
		return decodeImageMask(data, w, h, imageMaskInverted(doc, s.Dict), fill)
	}
```

- [ ] **Step 2: Add the `jbig2Globals` helper**

Elsewhere in `pkg/render/raster/page.go` (near the other image helpers), add:
```go
// jbig2Globals returns the decoded bytes of a JBIG2 image's /JBIG2Globals stream (shared
// segment dictionary), or nil when absent. The globals live in the image's /DecodeParms
// (or /DP) dict as an indirect stream reference.
func jbig2Globals(doc *pdf.Document, dict pdf.Dict) []byte {
	parms := doc.GetDict(dict["DecodeParms"])
	if parms == nil {
		parms = doc.GetDict(dict["DP"])
	}
	if parms == nil {
		return nil
	}
	gs := doc.GetStream(parms["JBIG2Globals"])
	if gs == nil {
		return nil
	}
	b, _, err := doc.DecodedStream(gs)
	if err != nil {
		return nil
	}
	return b
}
```

- [ ] **Step 3: Add the `filter` import**

Ensure `pkg/render/raster/page.go` imports `github.com/nathanstitt/doctaculous/pkg/pdf/filter`. Check the import block; if absent, add it. (`pdf` is already imported.)

- [ ] **Step 4: Build + generate the fixture golden**

Run:
```bash
gofmt -w pkg/render/raster/page.go
go build ./...
go test ./pkg/render/raster/ -run TestGolden -update
```
Expected: builds clean; `-update` writes `pkg/render/raster/testdata/golden/image-jbig2.png` (among others). Only `image-jbig2.png` should be new/changed — verify with `git status`.

- [ ] **Step 5: EYEBALL the golden**

Open `pkg/render/raster/testdata/golden/image-jbig2.png` and confirm it shows the decoded scanned image (recognizable content, not noise/blank/inverted). If it is INVERTED (black/white swapped), the polarity handling in `DecodeJBIG2` (Task 2) is backwards — flip the `if !black` to `if black` and regenerate. Record in the commit that you eyeballed it.

- [ ] **Step 6: Verify the golden is stable (no -update)**

Run: `go test ./pkg/render/raster/ -run TestGolden`
Expected: PASS (image-jbig2 included), no diffs.

- [ ] **Step 7: Commit**

```bash
git add pkg/render/raster/page.go pkg/render/raster/testdata/golden/image-jbig2.png
git commit -m "raster: decode JBIG2 images (decode-before-mask/raw); image-jbig2 golden"
```

---

## Task 6: Real-world integration fixture (external corpus)

**Files:**
- Create: `testdata/external/pdf/<name>.pdf` (a small real scanned PDF with embedded JBIG2)
- Modify: `testdata/external/pdf/README.md` (provenance) if one exists

- [ ] **Step 1: Obtain a small real PDF with embedded JBIG2**

Two options (prefer the smallest that renders):
  a. Search the existing `testdata/external/pdf/` — some scanned PDFs may already use JBIG2: `for f in testdata/external/pdf/*.pdf; do grep -laqs "JBIG2Decode" "$f" && echo "$f HAS JBIG2"; done`. If one already exists, no new file is needed — it now renders (previously blank); skip to Step 3.
  b. Otherwise wrap the committed `generic.jb2` payload into a standalone PDF using the generator and save it as an external fixture:
```bash
cd /Users/nas/code/docalizer
cat > /tmp/claude/genjbig2pdf.go <<'EOF'
package main
import ("os"; "github.com/nathanstitt/doctaculous/testdata/gen")
func main(){ os.WriteFile("testdata/external/pdf/jbig2-scan.pdf", gen.JBIG2ImagePDF(), 0o644) }
EOF
go run /tmp/claude/genjbig2pdf.go
```
(Option b reuses the generated wrapper — acceptable as a real-payload integration file. If you can source a genuinely-independent small scanned JBIG2 PDF with a clear license, prefer that and note provenance.)

- [ ] **Step 2: Note provenance**

If `testdata/external/pdf/README.md` exists, add a line for the new file (source + license). If the file is the wrapped Apache-2.0 payload, note that.

- [ ] **Step 3: Verify the external corpus test renders it without error**

Find the external-corpus test:
```bash
grep -rln "external" pkg/doctaculous/*_test.go
go test ./pkg/doctaculous/ -run External -v 2>&1 | tail -20
```
Expected: PASS — the new/existing JBIG2 PDF renders (the test asserts each external file parses + rasterizes without error; a JBIG2 page that previously rendered blank now renders the image). If the external test compares against committed goldens, regenerate per that test's `-update` convention and eyeball.

- [ ] **Step 4: Commit**

```bash
git add testdata/external/pdf/
git commit -m "test: real-world JBIG2 PDF in the external corpus"
```

---

## Task 7: JBIG2 /ImageMask + graceful-degradation tests

**Files:**
- Modify: `testdata/gen/images.go` (add `JBIG2ImageMaskPDF`)
- Test: `pkg/render/raster/jbig2_test.go` (new — ImageMask render + garbage-skip)

- [ ] **Step 1: Add a JBIG2 ImageMask fixture builder**

In `testdata/gen/images.go`, add (reuses the same real payload as a stencil):
```go
// JBIG2ImageMaskPDF returns a one-page PDF drawing the JBIG2 payload as a 1-bit
// /ImageMask stencil in a green fill. It exercises the decode-before-mask ordering: the
// JBIG2 stream must be decoded before the ImageMask branch consumes it.
func JBIG2ImageMaskPDF() []byte {
	const w, h = 2320, 3408 // must match generic.jb2's page dims (see JBIG2ImagePDF)
	b := newBuilder()
	imgNum := b.addStream(fmt.Sprintf(
		" /Type /XObject /Subtype /Image /Width %d /Height %d "+
			"/ImageMask true /BitsPerComponent 1 /Filter /JBIG2Decode", w, h),
		jbig2Generic)
	content := []byte("0 1 0 rg q 400 0 0 400 100 200 cm /Im0 Do Q")
	contentNum := b.addStream("", content)
	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	page := b.addObject(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>",
		pagesNum, imgNum, contentNum))
	if page != pageNum {
		panic("gen: page object number mismatch in JBIG2ImageMaskPDF")
	}
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}
```
(Match `w,h` to the value confirmed in Task 4.)

- [ ] **Step 2: Write the render tests**

Create `pkg/render/raster/jbig2_test.go`:
```go
package raster

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// countInked counts non-white opaque pixels — "something drew".
func countInkedJBIG2(t *testing.T, data []byte) int {
	t.Helper()
	doc, err := pdf.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	img, err := RenderPage(context.Background(), pg, Options{DPI: 36})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.RGBAAt(x, y).RGBA()
			if a > 0 && (r < 0xf000 || g < 0xf000 || bl < 0xf000) {
				n++
			}
		}
	}
	return n
}

// TestJBIG2ImageMaskRenders: a JBIG2-compressed /ImageMask stencil paints in the fill
// color — proving the decode-before-mask ordering (undecoded bytes would draw noise/blank).
func TestJBIG2ImageMaskRenders(t *testing.T) {
	if n := countInkedJBIG2(t, gen.JBIG2ImageMaskPDF()); n == 0 {
		t.Fatal("JBIG2 ImageMask rendered blank; expected the stencil to paint")
	}
}

// TestJBIG2GarbageSkipsGracefully: a page whose JBIG2 image is corrupt must still render
// (the image is skipped, no error/panic escapes to the page).
func TestJBIG2GarbageSkipsGracefully(t *testing.T) {
	doc, err := pdf.Parse(gen.JBIG2GarbagePDF())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	// Must not error or panic — the corrupt image is skipped, the page renders.
	if _, err := RenderPage(context.Background(), pg, Options{DPI: 36}); err != nil {
		t.Fatalf("render should not fail on a skippable bad JBIG2 image: %v", err)
	}
}
```

- [ ] **Step 3: Add the garbage fixture builder**

In `testdata/gen/images.go`, add:
```go
// JBIG2GarbagePDF returns a one-page PDF whose JBIG2 image stream is deliberately corrupt,
// for the graceful-degradation path: the decoder fails, the image is skipped, the page
// still renders. It also draws a text run so the page has other content to render.
func JBIG2GarbagePDF() []byte {
	const w, h = 8, 8
	b := newBuilder()
	imgNum := b.addStream(fmt.Sprintf(
		" /Type /XObject /Subtype /Image /Width %d /Height %d "+
			"/ColorSpace /DeviceGray /BitsPerComponent 1 /Filter /JBIG2Decode", w, h),
		[]byte("this is not a valid jbig2 stream"))
	content := []byte("q 400 0 0 400 100 200 cm /Im0 Do Q BT /F1 24 Tf 72 100 Td (ok) Tj ET")
	contentNum := b.addStream("", content)
	fontNum := b.addObject("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	page := b.addObject(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /Im0 %d 0 R >> /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>",
		pagesNum, imgNum, fontNum, contentNum))
	if page != pageNum {
		panic("gen: page object number mismatch in JBIG2GarbagePDF")
	}
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}
```

- [ ] **Step 4: Run the tests**

Run:
```bash
gofmt -w testdata/gen/images.go pkg/render/raster/jbig2_test.go
go test ./pkg/render/raster/ -run 'TestJBIG2' -v
```
Expected: PASS both — the ImageMask paints (non-zero ink), and the garbage page renders without error.

- [ ] **Step 5: Commit**

```bash
git add testdata/gen/images.go pkg/render/raster/jbig2_test.go
git commit -m "test: JBIG2 /ImageMask render + corrupt-stream graceful skip"
```

---

## Task 8: Full verification + docs

**Files:**
- Modify: `CLAUDE.md`, `README.md`

- [ ] **Step 1: Full suite + race + vet + lint**

Run:
```bash
go test ./... 2>&1 | grep -v '^ok\|no test files'
go test -race ./pkg/pdf/filter/... ./pkg/render/raster/
go vet ./...
golangci-lint run ./...
```
Expected: no failures; vet clean; `0 issues`. Fix anything that surfaces (e.g. an unused `dict` var in Task 3's builder — remove the dead `_ = dict` line).

- [ ] **Step 2: Update CLAUDE.md — move JBIG2 to Done**

In `CLAUDE.md`, in the PDF Filters "Done" bullet, change:
```
CCITTFax (Group 4 / Group 3 1D+2D), DCTDecode (JPEG). JBIG2 and JPX/JPEG2000 pending (`ErrUnsupported`).
```
to:
```
CCITTFax (Group 4 / Group 3 1D+2D), DCTDecode (JPEG), JBIG2 (vendored pure-Go Apache-2.0
decoder, `pkg/pdf/filter/jbig2`). JPX/JPEG2000 pending (`ErrUnsupported`; no viable pure-Go
decoder). `2026-07-09-jbig2-image-decoding-design.md`.
```
In the TODO list, update item 1 ("Remaining scan filters") to remove JBIG2 (JPX/JPEG2000 only). In the approved-deps section, add:
```
- Vendored `github.com/xiaoqidun/jbig2` (Apache-2.0, pure Go — JBIG2 image decode), copied
  into `pkg/pdf/filter/jbig2/` rather than a module dep because it is new/solo-authored;
  see that dir's README. Its only dep is `golang.org/x/image` (already used).
```

- [ ] **Step 3: Update README.md — third-party attribution**

In `README.md`, add (or extend a "Third-party" / "Credits" section):
```markdown
## Third-party / vendored code

- **JBIG2 decoding** is provided by [xiaoqidun/jbig2](https://github.com/xiaoqidun/jbig2)
  (Apache-2.0), a pure-Go JBIG2 decoder vendored into `pkg/pdf/filter/jbig2/`. See that
  directory's `LICENSE`, `NOTICE`, and `README.md`.
```

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs: JBIG2 decoding — CLAUDE.md status + README attribution"
```

- [ ] **Step 5: Push + open PR**

```bash
git push -u origin jbig2-image-decoding
gh pr create --base main --title "JBIG2 image decoding (vendored pure-Go decoder)" --body "Renders JBIG2Decode PDF images by vendoring the pure-Go Apache-2.0 xiaoqidun/jbig2 decoder (spike: pixel-perfect on ITU T.88 conformance data). Wired into decodeImageXObject with decode-before-mask/raw ordering; defensive recover around the vendored call → corrupt streams skip+log, never panic. Tests: generated gen.Core fixture (real payload + generated wrapper), real-world external PDF, JBIG2 /ImageMask, graceful degradation. JPX/JPEG2000 remains pending (no viable pure-Go decoder). See docs/superpowers/specs/2026-07-09-jbig2-image-decoding-design.md."
```

---

## Notes for the implementer

- **Eyeball the image-jbig2 golden** (Task 5 Step 5) — a JBIG2 decode bug most often shows as an inverted or garbled image, which the golden captures but only human review catches. If inverted, flip the polarity in `DecodeJBIG2`.
- **Dimensions must match the payload.** `JBIG2ImagePDF`/`JBIG2ImageMaskPDF`'s `w,h` constants MUST equal the decoded JBIG2 page size (confirmed in Task 4). A mismatch mis-sizes the raw repack.
- **Never regenerate other goldens.** Only `image-jbig2.png` is new; if any other golden changes under `-update`, something leaked — investigate, don't commit it.
- **Vendored code is third-party.** Don't "clean it up" or reformat beyond `gofmt`; keep it a faithful copy so future re-vendoring is a clean diff.
