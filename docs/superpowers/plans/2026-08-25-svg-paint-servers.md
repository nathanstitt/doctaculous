# SVG Paint Servers (PR 3 of 8) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** `fill="url(#g)"` / `stroke="url(#g)"` render real gradients (and patterns) instead of degrading to no-paint.

**Architecture:** PDF-free shader constructors over the existing (already PDF-separable) axial/radial math; a paint-server resolver in `pkg/svg` that runs during `Parse` and bakes resolved servers into the scene; `Save`→`PushClip`→`FillShading`→`Restore` at draw time, following `pkg/pdf/content/paths.go:70-79`. Spec: `docs/superpowers/specs/2026-08-25-svg-paint-servers-design.md`.

**Tech Stack:** Go stdlib plus existing `pkg/render`, `pkg/svg`, `pkg/pdf/function`. **No new dependencies.**

## Global Constraints

- Pure Go, no new dependencies. Never panic on malformed input.
- **No pre-existing golden PNG may change.** The engine has ~161 SVG goldens plus HTML/DOCX/PDF suites; a moved golden means a regression.
- Scene graph read-only after `Parse`; `docIndex` is discarded when `Parse` returns, so paint servers must be fully resolved during parse.
- Unsupported constructs degrade with a warn-once debug log via `warnOnceMsg`.
- All exported identifiers documented. gofmt/vet/golangci-lint clean.
- Conventional commits, no Claude/AI/session mentions.
- Branch `feat/svg-paint-servers` (created, stacked on `feat/svg-styling`).

## Environment notes (carried from PRs 1-2 — save the rediscovery)

- `go` commands need `dangerouslyDisableSandbox: true`.
- Run `go test ./...` **unsandboxed**; sandboxed runs give spurious httptest/loopback EOF failures.
- **Never** create scratch `.go` files in the repo; use the session scratchpad. Scratch leaked into `pkg/svg` four times across PRs 1-2.
- A golden always matches itself on first generation — **visually inspect every new PNG**. That step found two real bugs in PR 1 and one in PR 2.
- When asked for a discrimination check, RUN it and report observed output; "the logic is deterministic" is not evidence.

---

### Task 1: `(*render.Path).Bounds()` with true curve extrema

**Files:** Modify `pkg/render/path.go`; test `pkg/render/path_test.go`.

**Produces:** `func (p *Path) Bounds() (minX, minY, maxX, maxY float64, ok bool)` — the TIGHT geometry bbox. For `CubeTo`, solve the derivative's roots in t (quadratic per axis) and include only extrema with t in (0,1), plus both endpoints. `ok=false` for an empty path or one with only a `MoveTo`.

Why tight matters: `raster.pathDeviceBounds` bounds the CONTROL HULL and pads a pixel. A circle is four cubics whose control hull is ~10% larger per axis, so a bbox-unit gradient on a circle would be visibly offset and mis-scaled.

- [ ] **Step 1:** Write a failing test. Assert: a rect path's bounds are exact; a single cubic that bulges outside its endpoints reports the BULGE extent, not the control points (use a curve whose control points sit well outside the actual curve, and assert bounds are tighter than the control hull); a full circle approximated by four cubics has bounds within 1e-9 of the true circle; an empty path reports ok=false; a `MoveTo`-only path reports ok=false.
- [ ] **Step 2:** Run — expect FAIL (undefined).
- [ ] **Step 3:** Implement. Careful with the quadratic solve: handle the degenerate linear case (a==0) and clamp t to (0,1).
- [ ] **Step 4:** `go test ./pkg/render/...` — whole package.
- [ ] **Step 5:** Commit `feat(render): tight path bounding box with curve extrema`.

---

### Task 2: PDF-free shader constructors + spreadMethod

**Files:** Modify `pkg/render/raster/shading.go`; test `pkg/render/raster/shading_test.go` (append).

**Produces:**
```go
type Spread int
const (SpreadPad Spread = iota; SpreadReflect; SpreadRepeat)
func NewAxialShader(x0, y0, x1, y1 float64, fn function.Func, spread Spread) render.Shader
func NewRadialShader(fx, fy, fr, cx, cy, cr float64, fn function.Func, spread Spread) render.Shader
```

`SpreadPad` MUST preserve today's exact behavior (the existing `extend` two-bool path), so no PDF golden moves. `SpreadReflect`/`SpreadRepeat` are new math in the `sval` clamp (`shading.go:263`) and the radial `consider` closure (`shading.go:299`).

- [ ] **Step 1:** Failing test — construct each shader directly and sample `ColorAt` at known points. For reflect: t beyond 1 mirrors back (t=1.25 gives the same color as t=0.75). For repeat: t=1.25 gives the same as t=0.25. For pad: t=1.25 gives the t=1 color. Model on the existing `linRamp`/`wantRGB` helpers at `shading_test.go:22,45`.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Implement. Keep `ColorAt`/`atAxial`/`atRadial` structure; add the spread fold before the domain map.
- [ ] **Step 4:** `go test ./pkg/render/...` AND `go test -count=1 ./pkg/doctaculous -run Golden` — **PDF goldens must not move** (they exercise the pad path).
- [ ] **Step 5:** Commit `feat(raster): PDF-free shader constructors with spreadMethod`.

---

### Task 3: Stop parsing and the gradient ramp

**Files:** Create `pkg/svg/stops.go`, `pkg/svg/stops_test.go`.

**Produces:** parse `<stop>` children into a ramp implementing `function.Func` (1 in, 3 out).
- `offset` clamps to [0,1]; missing offset defaults 0; invalid offset (unparseable) → 0.
- Offsets must be non-decreasing: a stop whose offset is less than its predecessor's takes the predecessor's offset (spec rule).
- `stop-color` (default black) and `stop-opacity` (default 1) come **through the CSS cascade**, not raw attributes — stops can be styled by a stylesheet, and the corpus tests that.
- Zero stops → the paint server is "none" (shape not painted). One stop → a solid color of that stop.

- [ ] **Step 1:** Failing test covering: normal ramp, out-of-order offsets, missing/invalid offsets, zero/one stop, `stop-opacity`, and a stop styled via a `<style>` rule.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** `go test ./pkg/svg`.
- [ ] **Step 5:** Commit `feat(svg): gradient stop parsing and color ramp`.

---

### Task 4: href chain resolution with cycle detection

**Files:** Create `pkg/svg/paintserver.go`, `pkg/svg/paintserver_test.go`.

**Produces:** resolution of a gradient/pattern element (plus its `href` chain) into a resolved description.
- Attribute inheritance: per-attribute, first-defined-wins walking the chain.
- Stop inheritance: **all-or-nothing** — a gradient with no stops of its own takes the entire stop list from the nearest ancestor that has stops.
- Cross-type is legal (`linearGradient` href-ing a `radialGradient`); a non-gradient target is a no-op.
- **Cycle detection in the walker**: a visited set plus a depth cap. `docIndex` has NO cycle awareness and the parse-time `maxElementDepth` does NOT cover the reference graph. Write this as a reusable helper — PR 5 (`use`/`symbol`) needs the same machinery.
- Memoize resolved servers on `sceneBuilder`.
- Look up through `idx.ids` (NOT `defs`) — a gradient is referenceable from anywhere.

- [ ] **Step 1:** Failing test: attribute inheritance incl. complex order, stops-from-ancestor, cross-type, non-gradient target, a 2-cycle (a→b→a) and a self-cycle (a→a) both terminating without hanging, and a chain deeper than the cap.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** `go test ./pkg/svg` and `go test -race ./pkg/svg`.
- [ ] **Step 5:** Commit `feat(svg): gradient href inheritance with cycle detection`.

---

### Task 5: Style carries a paint-server reference

**Files:** Modify `pkg/svg/style.go` (`applyPaint` ~line 156, `Style` ~line 21, `FillPaint`/`StrokePaint` ~line 376); test `pkg/svg/style_test.go` (append).

**Produces:** `Style` retains the `url(#id)` fragment plus SVG's optional fallback color (`fill="url(#g) red"`), and exposes `FillServer() (id string, ok bool)` / `StrokeServer()`. `applyPaint` stores the id instead of logging-and-dropping; resolution happens later in the scene builder, which holds the index.

Update the `FillPaint`/`StrokePaint` doc comments — they currently cite "an unsupported url() paint-server reference" as an `ok=false` cause.

- [ ] **Step 1:** Failing test: `fill="url(#g)"` records the id; `fill="url(#g) red"` records both; a malformed `url(` degrades safely; inheritance carries the reference to children.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** `go test ./pkg/svg/...`.
- [ ] **Step 5:** Commit `feat(svg): retain paint-server references on resolved style`.

---

### Task 6: Scene dispatch — the forgiving-container trap

**Files:** Modify `pkg/svg/svg.go` (`unsupportedElements` ~193, `skippedElements` ~232, `buildNode` ~296); test `pkg/svg/svg_test.go` (append).

**THE TRAP:** `buildNode`'s `default` branch recurses into unknown elements as a forgiving container. Merely deleting `pattern` from `unsupportedElements` would paint its tile children **directly into the visible scene at document coordinates** — a loud misrender. The replacement must be explicit and total.

**Produces:** `linearGradient`/`radialGradient`/`pattern` move to `skippedElements`; `stop` is ADDED to `skippedElements`; the scene walk contributes zero nodes for all four even though they are now fully supported (they are resolved out-of-band through the index).

- [ ] **Step 1:** Failing test: a document with a `<pattern>` containing a `<rect>` produces a scene with NO extra shape (assert kid counts, not just pixels); `<stop>` produces no node and no "not yet supported" log; gradients produce no node.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** `go test ./pkg/svg/...` and the SVG goldens.
- [ ] **Step 5:** Commit `feat(svg): route paint-server elements out of the scene walk`.

---

### Task 7: Resolve servers into the scene and paint gradients

**Files:** Modify `pkg/svg/svg.go` (scene build), `pkg/svg/draw/draw.go`; add to `pkg/svg/paintserver.go`; tests in both packages.

**Produces:** a `Shape` carries a resolved paint server (a `render.Shader` plus the unit system and its transform), and `draw` paints it: `Save` → `PushClip(devicePath, rule)` → `FillShading(shader, ctm, blend)` → `Restore`.

CTM composition: objectBoundingBox→user (`Translate(minX,minY) × Scale(w,h)` from Task 1's `Bounds()` on the PRE-transform path), then `gradientTransform`, then `Shape.M`, then the accumulated matrix.

Degenerate bbox (a horizontal `<line>`, zero width or height) means the gradient is not rendered — make that explicit rather than relying on `invert` failing.

**Gradient strokes:** there is no `PushClipStroke`. Check whether `pkg/render/raster/stroke.go` exposes a stroke-to-outline conversion. If yes, use it. If not, `stroke="url(#g)"` degrades to the fallback color (else no stroke) with a warn-once log — document it, test it, and note it for a follow-up. Gradient FILLS are the priority.

- [ ] **Step 1:** Failing test: a linear gradient fill renders a left-to-right color change (sample pixels at 25%/50%/75%); a radial gradient renders center-out; objectBoundingBox and userSpaceOnUse both correct; `gradientTransform` applies; a degenerate bbox does not panic.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** `go test ./pkg/svg/... ./pkg/render/...`, `-race`, and the full golden suite.
- [ ] **Step 5:** Commit `feat(svg): render gradient paint servers`.

---

### Task 8: pdfwrite FillShading — rasterize into an XObject

**Files:** Modify `pkg/render/pdfwrite/device.go` (`FillShading` ~line 200); test `pkg/render/pdfwrite/device_test.go` (append).

**Why:** it is a no-op stub today, so SVG gradients → PDF would render NOTHING — silently worse than the honest "no fill" they replace. Emitting a real shading dictionary is better but blocked: `render.Shader` is an opaque `ColorAt`-only interface, so pdfwrite cannot recover coordinates and stops. PR 4 already opens pdfwrite for transparency groups and is the right home for the vector path.

**Produces:** `FillShading` samples the shader over the current clip's bounds into an RGBA image and draws it through the existing working `DrawImage` path. Log once that the gradient was rasterized (a fidelity note, not an error). Resolution should follow the device's existing DPI notion rather than a hardcoded constant.

- [ ] **Step 1:** Failing test: an SVG with a gradient converted to PDF produces a content stream containing an image XObject (today it produces nothing); a solid-fill document is byte-identical to before (no XObject introduced where none belongs).
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** `go test ./pkg/render/... ./pkg/doctaculous` and ALL goldens.
- [ ] **Step 5:** Commit `feat(pdfwrite): rasterize shadings into an image XObject`.

---

### Task 9: Patterns (may be deferred — decide during implementation)

**Files:** `pkg/svg/paintserver.go`, `pkg/svg/draw/draw.go`, tests.

**Produces:** `<pattern>` renders its tile to an offscreen image once, then paints it tiled across the shape's clip via `DrawImage`. Handles `patternUnits`, `patternContentUnits` (common case), `patternTransform`, and tile `viewBox`.

**Explicit deferral option:** if the offscreen-tile work proves large or the scene-within-a-scene recursion fights the read-only-after-Parse invariant, STOP and report — gradients alone are a coherent, shippable PR, and patterns move to their own follow-up. Record the decision either way; do not half-ship a pattern implementation.

- [ ] **Step 1:** Failing test: a pattern fill tiles across a rect; `patternTransform` applies; a pattern with no children renders nothing.
- [ ] **Step 2-5:** As above; commit `feat(svg): render pattern paint servers` (or report the deferral).

---

### Task 10: resvg corpus tranche + goldens

**Files:** `testdata/svg/resvg/paint-servers/**`, README, goldens.

The upstream clone is at `<scratchpad>/resvg-suite` at the pinned commit `d8e064337faf01bc5a9579187a56dbdbe3eacc72`; `tests/paint-servers/` has 149 `.svg` files (38 linear, 45 radial, 31 pattern, 32 stop, 3 stop-color/opacity).

- [ ] **Step 1:** Curate — INSPECT each candidate's contents; exclude anything needing `<use>`, `<symbol>`, text, mask, clip, filters, or `<image>`, and (if Task 9 deferred) all pattern fixtures. Record every exclusion with a reason.
- [ ] **Step 2:** Generate goldens with `-update`.
- [ ] **Step 3:** **VIEW EVERY NEW PNG** with the Read tool against the source's `<title>`/intent. Many resvg fixtures use green=correct/red=wrong — report which use it and what you saw. A red golden is a probable bug: investigate before committing.
- [ ] **Step 4:** Update the corpus README with a PR 3 section and per-item exclusions.
- [ ] **Step 5:** Verify no pre-existing golden moved; commit `test(svg): resvg paint-servers tranche with goldens`.

---

### Task 11: Verification + docs

- [ ] **Step 1:** `go test ./...` UNSANDBOXED.
- [ ] **Step 2:** `go test -race ./pkg/svg/... ./pkg/render/... ./pkg/doctaculous`, `go vet ./...`, `golangci-lint run`, `gofmt -l . | grep -v jbig2` (must be empty).
- [ ] **Step 3:** Confirm NO pre-existing golden moved (`git diff --stat <base> HEAD -- '*.png'` — every entry must be `Bin 0 -> N`). State it explicitly.
- [ ] **Step 4:** CLI smoke: an SVG with a linear gradient, a radial gradient, and (if shipped) a pattern; rasterize to PNG and VIEW it; convert to PDF and confirm non-trivial output.
- [ ] **Step 5:** FEATURES.md — gradients ship; update the not-yet list. Note the stroke-gradient status honestly.
- [ ] **Step 6:** Commit docs; draft the PR description into the report (do NOT open the PR).
