# Handover — Sub-project 5: Floats + positioning

**Status:** Not started. Sub-project 4 (replaced content + images) is DONE on branch
`feat/html-replaced-images` (off `feat/html-block-inline-flow`).
**Next action:** Same flow as #1–#4 — brainstorm → spec (`docs/superpowers/specs/`) → plan
(`docs/superpowers/plans/`) → subagent-driven execution (per task: implement → spec-review →
code-quality-review → fix) → holistic final review → finish branch / PR.

---

## Where we are (the PR stack)

- **#1 CSS parse+cascade** — `feat/css-parse-cascade`.
- **#2 HTML box generation** — `feat/html-box-generation` → `feat/css-parse-cascade`.
- **#3 block + inline normal flow** — `feat/html-block-inline-flow` → `feat/html-box-generation`.
- **#4 replaced content + images** — `feat/html-replaced-images` → `feat/html-block-inline-flow`.
  Retarget each PR up the chain as the one below it merges; if the stack has merged to `main`, branch
  sub-project 5 off `main`.

## What sub-project 4 delivered (the foundation #5 builds on)

Design: `docs/superpowers/specs/2026-06-24-html-replaced-images-design.md`.

`<img>` now decodes → sizes → paints. New/changed since #3:

- **`pkg/layout/css/image.go`** — `imageCache`: decode PNG/JPEG/GIF (stdlib, no new dep) via the
  `pkg/resource.ResourceLoader`, cached per-engine (mirrors `FaceCache`; caches misses; skips caching
  transient ctx-cancellation misses). Sniffs format when content type is absent.
- **`pkg/layout/css/replaced.go`** — `replacedUsedSize` (CSS §10.3.2/§10.6.2: intrinsic + aspect-ratio
  derivation, per-axis min/max clamp via the shared `clampMaxMin`), `replacedFragment` (the shared
  fragment builder), `layoutBlockReplaced` (block-level `<img>`, `width:auto`→intrinsic), `mapObjectFit`.
- **`pkg/layout/css/fragment.go`** — `Fragment.Image *ImageContent{Img; CX,CY,CW,CH; Fit}`; the shift
  helpers move the content rect with the fragment; `AppendItems` emits `layout.ImageKind`.
- **`pkg/layout/page.go`** — `ImageKind`, `ImageItem`, the format-neutral `ObjectFit` enum.
- **`pkg/layout/paint/paint.go`** — `paintImage`: the unit-square→content-box matrix (`Mimg =
  Scale(cw,-ch)·Translate(cx,cy+ch)`, composed with the page matrix) + `object-fit` (`fitDest`;
  cover/oversized clip to the box).
- **`pkg/layout/css/inline.go`** — replaced sizing in the IFC (matched before inline-block), atom
  **horizontal margins** (`atomicRunFor`/`MarginLeftPt`), atom **baseline/ascent** consumption.
- **`pkg/layout/inline` (`line.go`/`shape.go`)** — `MakeLine` records atom ascent/descent in separate
  `AtomAscentPt`/`AtomDescentPt` fields (kept apart from text metrics so the line-height leading
  multiplier doesn't scale atom box heights); `AtomicItem.MarginLeftPt`.
- **`pkg/css`** — additive `ObjectFit` on `ComputedStyle`.
- **`pkg/doctaculous`** — `html-image-basic`/`html-image-object-fit` goldens, `img-vs-div` WPT reftest;
  the golden/reftest harness now threads an optional `resource.ResourceLoader` per fixture.

## What sub-project 5 must build (roadmap §6)

**Floats + positioning:** `float`/`clear`, `position: relative`/`absolute`/`fixed`, z-order, and
`overflow` clipping. The box already records `Float`/`Position` (`pkg/layout/cssbox`), so box-gen likely
needs little change; the work is in the CSS layout engine (`pkg/layout/css`):

- **Floats** — a float is taken out of normal flow, shifted to the left/right edge of its containing
  block, and line boxes shorten around it; `clear` moves a box below preceding floats. This touches the
  block stacker and the IFC (line boxes must avoid float rects). The biggest engine addition.
- **Positioning** — `relative` offsets a box from its normal-flow position (paint-time shift, flow
  unchanged); `absolute`/`fixed` take it out of flow and position against the nearest positioned
  ancestor / viewport. Establishes the need for a **stacking order** (z-index) at paint/flatten time —
  the current `AppendItems` is strict tree order; positioned/floated boxes need a later paint pass.
- **`overflow` clipping** — `overflow:hidden`/`scroll`/`auto` clip descendants to the box (a `PushClip`
  the paint side already supports; the fragment tree needs to carry the clip rect).

Each lands with fixtures + golden/WPT tests, degrades gracefully (already: `Position`/`Float` are
recorded but ignored, so boxes lay out in normal flow today), and recovers at the page boundary.

## Deferred follow-ups carried from #4 (pick up where relevant)

- **`object-position`** — the focal-point offset of a replaced image within its content box.
- **Ratio-preserving min/max** (CSS 10.4) — today min/max clamp per-axis after aspect-ratio derivation.
- **Percentage `height` basis on replaced elements** — today a basis-less percentage height is treated
  as auto.
- **CSS `background-image`** — decode + tile/position a background image (reuses `imageCache`).
- **Full `vertical-align`** — only the atom baseline mechanics landed; the keyword set
  (top/middle/text-top/super/sub/…) is open.
- **`margin:auto` horizontal centering** — still computes to 0 (deferred since #3).
- **Margin-collapse edge cases** — empty-block collapse-through, clearance (interacts with floats here),
  `min-height` interaction.

## Process reminders (held across #1–#4)

- **Sandbox blocks the Go build cache + TLS** — run `go`/`golangci-lint`/`gofmt` with the sandbox
  disabled. **Editor diagnostics lag** (stale errors, phantom `zz_*` scratch files after subagents
  write/delete) — trust `go build`/`go test`, not the panel. After any review subagent, `find . -name
  'zz_*' -delete` and confirm `git status` is clean (sub-project 4's spec-review left scratch files).
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages, not the repo root. **Decline modernize hints** (`max()`/`min()`/`slices.*`/range-over-int) —
  the codebase intentionally uses explicit clamps.
- **The two-stage review earns its keep.** In #4 it caught a real percentage-height sizing bug and a
  cache-poisoning smell. Have spec reviewers verify load-bearing math with throwaway adversarial tests
  (for #5: the float-avoidance geometry and the stacking/paint order).
- **Eyeball every new/changed golden PNG** in the PR (the controller, not just the implementer).
- **Propagate review fixes back into the spec/plan**, and **update CLAUDE.md's Done/TODO** when the PR
  lands (move floats+positioning out of the §6 TODO).
