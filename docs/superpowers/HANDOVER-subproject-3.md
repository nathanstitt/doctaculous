# Handover — Sub-project 3: Block + inline normal flow (first pixels ★)

**Status:** Not started. Sub-project 2 (HTML frontend + box generation) is DONE and in **PR #3**.
**Next action:** Resume the same flow used for sub-projects 1 & 2 — `superpowers:brainstorming` for
sub-project 3, then `writing-plans`, then `subagent-driven-development` (implement → spec-review →
code-quality-review → fix per task, then a holistic final review).

---

## Where we are

- **Sub-project 1 (CSS parse + cascade)** — `pkg/css`. In **PR #2**
  (`feat/css-parse-cascade` → `main`). **Not yet merged.**
- **Sub-project 2 (HTML frontend + box generation)** — DONE, in **PR #3**
  (`feat/html-box-generation` → `feat/css-parse-cascade`, i.e. stacked on #2).
  Branch is pushed. The PR is intentionally based on `feat/css-parse-cascade` so its diff is only
  sub-project 2; **retarget PR #3 to `main` once PR #2 merges.**
- **Stacking note:** `feat/html-box-generation` currently contains BOTH sub-projects' commits (sub-1
  is not in `main` yet). When you start sub-project 3, branch off `feat/html-box-generation` (or off
  `main` after #2 + #3 merge). Decide the base when you get there; if #2/#3 have merged to `main`,
  just branch from `main`.

## What sub-project 2 delivered (the foundation #3 builds on)

Design: `docs/superpowers/specs/2026-06-23-html-box-generation-design.md`.
Overarching roadmap: `docs/superpowers/specs/2026-06-23-html-rendering-design.md` (this is §5 row 3).

Sub-project 2 produces a **correct, normalized `cssbox` tree** from HTML+CSS — **no pixels**. New code:

- **`pkg/html`** — `Parse([]byte) (*Document, error)` wraps `golang.org/x/net/html` into an owned DOM
  (`*Element`/`*Text`) that implements `pkg/css`'s `Node` interface. Collects `<style>` (parsed),
  `<link rel=stylesheet>` hrefs (unresolved), and inline `style=""` (left on the element). Ships
  `UAStylesheet` (a minimal user-agent default sheet: display defaults, heading sizes/margins).
- **`pkg/layout/cssbox`** — the recursive, format-neutral `Box` type (the long-lived layout contract).
- **`pkg/layout/css`** — box generation: `Build(ctx, *html.Document, resource.ResourceLoader, logf)
  (*cssbox.Box, error)`. Drives the cascade per element (root via `ComputeRoot`, children threading
  parent style), maps display → box kind, prunes `display:none`, resolves `<link>` sheets via the
  loader, and **normalizes** the tree (anonymous-box fixups + whitespace). Recovers at the entry
  boundary (never panics to the caller).
- **`pkg/resource`** — `ResourceLoader` interface + hermetic `MapLoader`/`DirLoader` (no HTTP yet).
- **`pkg/css` changes** — origin-aware cascade (`Origin`/`OriginSheet`, `NewResolver([]OriginSheet,
  logf)`) so UA defaults sit below author rules; `ComputeRoot(n)` for the root inheritance base
  (keeps `initialStyle()` unexported).

All verified by **structural assertions** (no goldens), `go test -race ./...` green across all
packages, vet/lint/gofmt clean.

## What sub-project 3 must build (roadmap §5 row 3 — the first-pixels milestone ★)

From the overarching design, sub-project 3 is **Block + inline normal flow**:

1. **Inline-core extraction (first task):** extract the shared inline-layout core into
   `pkg/layout/inline` (`shape.go` styled runs → shaped glyphs; `break.go` the greedy line-breaker;
   `line.go` line metrics/justification/alignment). The EXISTING flat DOCX engine (`pkg/layout`) is
   refactored to call this extracted core — **DOCX golden images must stay unchanged** = proof the
   extraction is behavior-preserving. (This is the `◆`/shared step; do it first so both engines share
   one line-breaker.)
2. **`pkg/layout/css` (the CSS layout engine):** consume the `cssbox` tree → positioned fragments.
   - `block.go` — block formatting context (normal flow, margin collapsing).
   - `inline.go` — inline formatting context (line boxes; calls `pkg/layout/inline`).
   - `fragment.go` — the positioned fragment tree (paint input), read-only after layout.
3. **Paint:** extend `pkg/layout/paint` (don't fork it) with background, border (4 sides + styles),
   and the text/inline item kinds — all fills/glyphs/images on `render.Device`.
4. **Public API:** `OpenHTML(path)` / `OpenHTMLBytes(data, opts...)` returning the same `*Document`
   the rest of the toolkit rasterizes (so it reuses the existing `RasterOptions`/`renderPage`). This
   is where the pipeline first produces a real rendered page.
5. **Tests:** the first **WPT reftest slice** (CSS2.1 normal flow — reference-comparison, not
   committed goldens) PLUS hand-authored fixtures with box/fragment position assertions PLUS a couple
   of committed golden PNGs (eyeballed). All in the same PR.

Output model (per overarching §7.4): lay out at a fixed viewport width, flow to content height,
rasterize as a **single tall image** by default. Pagination/`@page` is sub-project 11.

## The exact seam #3 consumes: `cssbox.Box`

`pkg/layout/cssbox/box.go` (read it). The fields #3's layout engine reads:

```go
type Box struct {
    Kind       BoxKind            // Block / Inline / AnonBlock / AnonInline / Replaced / Text
    Style      css.ComputedStyle  // resolved style (font/color/margins/padding/border/width/height...)
    Children   []*Box
    Text       string             // set only for Kind==BoxText
    Replaced   *ReplacedContent   // set only for Kind==BoxReplaced (Tag + Attrs; NO decoded image yet)
    Display    DisplayKind        // normalized display (Block/Inline/InlineBlock/Table*/Flex/Grid/...)
    Formatting FormattingContext  // BlockFC / InlineFC / TableFC / FlexFC / GridFC
    Float      FloatKind          // FloatNone today (float lands in pkg/css later → sub-project 5)
    Position   PositionKind       // PosStatic today (position lands later → sub-project 5)
}
```

**Invariant the tree guarantees** (relied on by BFC/IFC): every block container's children are
**either all block-level or all inline-level** (`Kind.IsBlockLevel()` / `Kind.IsInlineLevel()`
partition all kinds). Anonymous boxes (`BoxAnonBlock`/`BoxAnonInline`) are already inserted to make
this hold, including the nested block-in-inline case.

**`Formatting` is authoritative for the block-vs-inline FC choice.** It is reconciled in box-gen's
normalize pass against the *final* child composition: a block whose children are all inline-level
reports `InlineFC`, otherwise `BlockFC`; a real `<p>` and an anonymous block holding inline runs
agree. Table/flex/grid keep their keyword-derived FC. So #3's layout dispatch can switch on
`Formatting` directly.

## Carried-forward notes from sub-project 2's reviews (things #3 should know)

These were flagged during sub-project 2's reviews and consciously deferred to the layout engine —
handle them in #3 rather than rediscovering them:

1. **Anonymous block boxes carry a zero-value `Style`** (by design;
   `pkg/layout/css/anon.go`). The real inherited values that matter for an inline formatting context
   — notably `color` and `text-align` — live on the box's **text-leaf children**, not on the anon
   block. When #3 resolves line-box alignment for an anonymous block establishing an IFC, read
   `text-align` from the content/children, not from the (zero) anon-block `Style`.
2. **Text boxes (`BoxText`) carry the parent element's full computed `Style`**, but only the
   *inherited* fields (font family/size, bold/italic, color, line-height, text-align) are meaningful.
   The parent's box-level fields (width, margins, borders) are present on a text box's `Style` but are
   **not** meaningful for a text leaf — do not read them. (`Style.Display` on a text box is forced to
   `"inline"`.) See the doc comment on `makeTextBox` in `pkg/layout/css/build.go`.
3. **Replaced content has no intrinsic size yet.** `BoxReplaced` carries only `{Tag, Attrs}` (e.g.
   `img src/width/height/alt`) — **no decoded image**. Image decode + intrinsic sizing is
   sub-project 4. For #3, a replaced box can be laid out from its `width`/`height` attrs/style if
   present, or treated as a zero/placeholder box; full replaced-element sizing is #4.
4. **`Float`/`Position` are always `FloatNone`/`PosStatic`** today (the `floatOf`/`positionOf` stubs
   in `build.go` return constants because `float`/`position` aren't on `ComputedStyle` yet). Real
   float/positioning is sub-project 5. #3 lays out normal flow only; don't build float logic.
5. **Degradation contract (overarching §6):** unsupported layout mode → **fall back to block normal
   flow** so children still lay out and paint. Recognized-but-unimplemented display (flex/grid/table)
   arrives at #3 as its true `Display`/`Formatting` — #3 should block-fallback those at the *layout*
   stage (they were deliberately NOT normalized to block in generation). Recover at the page boundary;
   never panic on malformed input.

## ⚠️ Known design question to resolve early in #3

The inline-core extraction (task 1) refactors the **shipping** flat DOCX engine to call a new
`pkg/layout/inline`. The DOCX golden images are the regression oracle — they must stay byte-identical
(within the existing tolerance) through the extraction. Plan this carefully: extract, point the flat
engine at the extracted core, run the DOCX goldens, and only then build the CSS IFC on top of the same
core. If the extraction can't preserve DOCX goldens, stop and rethink the boundary.

## Dependencies note

No new dependency is strictly required for #3 (normal-flow layout + paint reuse existing `pkg/font`,
`pkg/render`, `pkg/render/raster`). Web fonts (WOFF/WOFF2, the one new dep `andybalholm/brotli`) are
sub-project 7. If you vendor a WPT subset for the reftest bar, it's BSD-3 — note provenance + license
in the PR per CLAUDE.md.

## Process reminder (what worked for #1 and #2)

Brainstorm → spec (`docs/superpowers/specs/`) → plan (`docs/superpowers/plans/`) →
subagent-driven execution (per task: implement → spec-review → code-quality-review → fix) → holistic
final review → finish branch (PR). Notes from #2:

- **The command sandbox blocks the Go build cache** (`operation not permitted` on
  `~/Library/Caches/go-build`) and TLS to the proxy — rerun `go`/`golangci-lint`/`gofmt` with the
  sandbox disabled. The editor **diagnostics panel lags** (shows stale "undefined" / "x/net not in
  go.mod" errors after subagents write files) — trust `go build`/`go test`, not the panel.
- **`golangci-lint` here does NOT enable a gofmt linter** — run `gofmt -l <pkgs>` separately; a gofmt
  miss won't be caught by lint. Lint specific packages (not the repo root, which trips on the
  untracked `agent/skills/.../examples/` stray dir).
- **The two-stage review earns its keep:** in #2 it caught a non-discriminating cascade test, a gofmt
  miss, inverted UA heading margins, and (in the holistic pass) a `Formatting` field that didn't match
  the spec for all-inline blocks. Have spec reviewers write throwaway adversarial tests (e.g. prove an
  invariant across many inputs, mutation-check that a test actually fails when the feature breaks),
  then delete them.
- **Propagate review fixes back into the spec/plan** so they stay authoritative.
