# HTML rendering — replaced content + images (sub-project 4)

**Status:** Implemented. Branch `feat/html-replaced-images` (off `feat/html-block-inline-flow`).
Roadmap §5 row 4 of `docs/superpowers/specs/2026-06-23-html-rendering-design.md`. Predecessors:
sub-project 1 (CSS parse+cascade), 2 (box generation), 3 (block+inline normal flow,
`2026-06-23-html-block-inline-flow-design.md`).

## Goal

Make an `<img>` **decode → size → paint**. Before this slice, box generation recorded `<img>` as a
`cssbox.BoxReplaced` carrying `src`/`width`/`height`/`alt`, but nothing decoded or painted it — it
rendered as empty space. This slice decodes PNG/JPEG/GIF (Go stdlib, **no new dependency**) at layout
time through the existing `pkg/resource.ResourceLoader`, applies the CSS replaced-element sizing
algorithm (intrinsic size + aspect-ratio derivation, clamped by min/max), paints via the existing
`render.Device.DrawImage` seam, and degrades gracefully (sized placeholder + log) when an image is
missing or undecodable.

**Scope (agreed): "core + inline fidelity".** Core = decode, intrinsic + aspect-ratio sizing, all
`object-fit` modes, paint, graceful degradation, and block / inline / inline-block placement. Plus the
two carried-forward inline deferrals from sub-project 3: inline-block/replaced **horizontal margins**,
and proper atomic-box **baseline / line-box ascent**.

## Architecture / seams

- **Decode at layout time, not box-gen time.** `cssbox` stays pure and read-only (no image bytes, no
  `image.Image`); the engine holds the loader + ctx. `ReplacedContent` carries only source facts.
- **`pkg/layout/css/image.go`** — `imageCache`: resolves a `src` to a `decodedImage{img, w, h, ok}`
  through the loader, caching every result (including misses), mirroring `pkg/layout/font` `FaceCache`
  (sync.Mutex; negative results cached). Decoder chosen by content type, falling back to `image.Decode`
  format sniffing (the blank imports `image/png|jpeg|gif` register the formats). A nil loader, a
  not-found ref, an unsupported format, or malformed bytes all degrade to `ok:false` + a `logf` line.
  A **transient** `context.Canceled`/`DeadlineExceeded` miss is NOT cached (so a later live-context
  render can still succeed); permanent misses are cached.
- **`pkg/layout/css/replaced.go`** — the replaced-element sizing algorithm and the fragment builder
  shared by the inline and block paths.
- **`pkg/layout/css/fragment.go`** — `Fragment.Image *ImageContent{Img; CX,CY,CW,CH; Fit}` carries the
  decoded image + its content-box rect (in the fragment's own frame, so it shifts with the fragment via
  the updated `shiftFragment`/`translateFragment`). `AppendItems` emits a `layout.ImageKind` item.
- **`pkg/layout/page.go`** — `ImageKind`, `ImageItem{Img; XPt,YPt,WPt,HPt (content box); Fit}`, and a
  format-neutral `ObjectFit` enum (like `BorderStyle`).
- **`pkg/layout/paint/paint.go`** — `paintImage` maps the image's unit square upright into the content
  box and calls `DrawImage`, honoring `object-fit`.
- **`pkg/css`** — additive `ObjectFit` on `ComputedStyle` (`fill` default; parse mirrors `box-sizing`).

## The sizing algorithm (`replacedUsedSize`, CSS 2.1 §10.3.2/§10.6.2)

Returns the used **content-box** size in points, feeding both the inline atom path and the block path.

1. **Specified width/height** (`specifiedReplacedLen`): the CSS length if not auto (px/pt 1:1, em vs
   font size, % vs basis), else the presentational `width`/`height` attribute (`attrPx`, integer px).
   **CSS wins over the attribute.** The width axis has a percentage basis (the containing block / inline
   content width); the **height axis has no basis** in this single-axis model, so a percentage height
   (and percentage min/max-height) is treated as **auto/none**, not resolved against a zero basis. (This
   was a review finding: passing `basis=0` to the resolver returned an explicit `0`, squashing the
   image — fixed by a `hasBasis` flag.)
2. **Intrinsic size** from the decoded image (px→pt 1:1); a failed decode leaves none.
3. **Used-size cases:** both specified → use them; only width → `h = w·ih/iw` (0 if no intrinsic);
   only height → `w = h·iw/ih`; neither → intrinsic (0×0 if no image).
4. **Clamp** each axis by min/max (`clampMaxMin`, the same max-then-min-then-floor primitive the block
   width algorithm uses; factored out of `resolveContentWidth`). Per-axis clamp after ratio derivation;
   the ratio-preserving min/max step (CSS 10.4) is a deferred refinement.

## Paint matrix + object-fit (load-bearing — verified with throwaway adversarial tests)

`render.Device.DrawImage` maps the image's unit square `[0,1]²` through `ctm` into device space and
samples the source row from `(1-v)` (v-flip: image top row at v=1). The reflow page→device matrix is
`Scale(scale,scale)`, Y-down, no flip. To render the image upright in content box `(cx,cy,cw,ch)`:

```
Mimg = Scale(cw, -ch).Mul(Translate(cx, cy+ch))   // {A:cw, D:-ch, E:cx, F:cy+ch}
ctm  = Mimg.Mul(mat)                               // then page→device
dev.DrawImage(img, ctm, 1, "")
```

At v=1 → y=cy (top); at v=0 → y=cy+ch (bottom) — upright. A throwaway test painting a 4-distinct-corner
image confirmed red top-left, etc.

**object-fit** (`fitDest`): `fill` = whole content box (stretch). `contain` = scale by the smaller
axis ratio, centered (letterbox). `cover` = scale by the larger axis ratio, centered, **clipped** to
the content box (`Save`/`PushClip`/`Restore`). `none` = intrinsic, centered, clipped. `scale-down` =
`min(none, contain)`. The clip is pushed only when the fitted rect overflows the box (epsilon-guarded).

## Inline vs block placement

- **Inline / inline-block** (`gatherInlineRuns`): the `BoxReplaced` case is matched **before** the
  `DisplayInlineBlock` case, so an `<img style="display:inline-block">` (which is `BoxReplaced` +
  `DisplayInlineBlock`) is sized as a replaced box, not laid out as an empty inline-block container. The
  fragment (built by `replacedFragment`, deflating the border box by the box's own padding+border to
  the image content rect) is carried as an atom (`atomicRunFor`).
- **Block** (`layoutBlockReplaced`, branched off `layoutBlock`): a `display:block` `<img>` is sized by
  the replaced algorithm — `width:auto` uses the **intrinsic width** (not the containing-block fill a
  normal block gets) — honoring explicit width, box-sizing, padding, border, margins, min/max. No
  margin collapsing (a replaced box has no in-flow children).

## Inline fidelity

- **Horizontal margins on atoms** (`atomicRunFor` + `layoutInline`): the atom's advance is
  `marginL + borderWidth + marginR`; `AtomicItem.MarginLeftPt` records the left margin so placement
  offsets the kept fragment's border box past it. Applied to both replaced and inline-block atoms.
- **Atom baseline / line-box ascent** (`pkg/layout/inline` `MakeLine`, closing its line.go TODO): an
  atom contributes its above-baseline (`BaselinePt`) and below-baseline (`HeightPt-BaselinePt`) extent
  to **separate** `Line.AtomAscentPt`/`AtomDescentPt` fields, kept apart from the text
  `AscentPt`/`DescentPt`. `ascentOfLine` places the baseline at `max(text ascent, atom ascent)` (so a
  tall image drops the baseline below it, no overflow above the line top), while `autoLineHeight`'s
  "normal" leading multiplier (×1.15) applies only to the **text** metrics — so an all-inline-block row
  gets line height == atom height, not atom×1.15. A fixed `line-height` shorter than a tall atom still
  reserves the atom's extent (the `atomicLineExtent` floor in `effectiveLineHeight`). DOCX emits no
  atoms, so this is inert for the flat engine (DOCX goldens unchanged).

## Degradation

An undecodable / 404 / missing-`src` / unsupported-format (SVG, WebP, or — until the HTTP loader lands
— anything needing network) image draws nothing but **reserves its box** at the CSS/attr size (a sized
placeholder), logs via `e.logf`, and never aborts the page. Recovery is at the page boundary
(`Engine.Layout` already recovers from panics).

## Tests

- `pkg/css` — `object-fit` parse/cascade (initial + each keyword + invalid dropped).
- `pkg/layout/inline` — `MakeLine` atom metrics kept separate from text metrics.
- `pkg/layout/paint` — orientation (real raster pixels), and `fitDest` geometry per object-fit mode via
  a `DrawImage`-ctm-recording device (contain letterboxes, cover clips, none centers, nil draws
  nothing).
- `pkg/layout/css` — fragment-geometry (explicit attr size, CSS-over-attr, intrinsic, aspect-ratio both
  directions, min/max clamp, percentage-height-as-auto, block/inline/inline-block sizing, content-rect
  insets), degradation (missing src / not found / undecodable, asserting placeholder + log), inline
  fidelity (margins shift the next glyph; tall image raises ascent / bottom-aligned), and the
  transient-cancellation cache behavior. Hermetic: tiny PNG/JPEG/GIF encoded in-test, served via
  `resource.MapLoader`.
- `pkg/doctaculous` — two `html-image-*` golden PNGs (eyeballed: image upright + correctly sized; the
  object-fit golden shows fill/contain/cover) and an `img-vs-div` WPT reftest (a stretched solid `<img>`
  == a `<div>` of the same size + background). `TestDOCXGolden`/existing `TestHTMLGolden`/`TestWPTReftests`
  stay green; `go test -race ./...` clean.

## Deferred (carried to the next handover)

`object-position`; the ratio-preserving min/max sizing step (CSS 10.4); a percentage `height` basis on
replaced elements; CSS `background-image`; full `vertical-align` keyword set (only the atom baseline
mechanics landed); `margin:auto` centering. Remote `<img src>` URLs arrive with the HTTP
`ResourceLoader` slice.

## Review fixes folded in

The two-stage review (spec + code-quality) found and fixed: (1) the **percentage-height-vs-zero-basis**
bug (squashed images to 0; now treated as auto via `hasBasis`); (2) caching a **transient context
cancellation** as a permanent decode miss (now not cached); (3) a stale `ReplacedContent` doc comment
(now states decoding is intentionally at layout time, box stays decode-free).
