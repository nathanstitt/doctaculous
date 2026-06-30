# CSS `background-image` — design (HTML engine feature 3/4)

**Goal:** Render CSS raster background images at browser fidelity — decode a
`background-image: url(...)`, position it (`background-position`), tile it
(`background-repeat`), scale it (`background-size`), and confine it to the right box
(`background-origin` / `background-clip`) — painted in CSS paint order (background
color → background image → border → content).

**Scope:** raster bitmaps (PNG/JPEG/GIF — the formats the existing image cache already
decodes). One image per box (no multi-layer comma lists). `background-attachment` is
parsed but only `scroll` is meaningful in the single-tall-page / paginated model
(`fixed` degrades to `scroll`, logged). CSS gradients are **out of scope** (a separate
feature — `background` keeps color + url only). No `background-blend-mode`.

This is feature 3/4 of the autonomous fidelity program (after white-space and
lists+counters; link pseudo-classes is 4/4). It is **additive and byte-identical** for
any document with no `background-image` (the new fragment field is nil, the new item
kind is never emitted).

## Background (what exists — see the recon)

- Background **color** is emitted in `(*Fragment).appendSelfDecorations`
  (`pkg/layout/css/fragment.go`) as a `BackgroundKind` rect over the fragment's
  **border box** (`f.X,Y,W,H`), before the border edges. The painter fills it.
- The `background` shorthand (`pkg/css/shorthand.go applyBackground`) currently keeps
  **color only** and drops `url(...)`. `splitComponents` already groups `url(...)` as
  one component. `takeFunc(s,"url")` + `unquote` (in `fontface.go`) extract the URL.
- The image **decode cache** `imageCache.get(ctx, ref) decodedImage{img,w,h,ok}`
  (`pkg/layout/css/image.go`) is keyed by ref, concurrency-safe, caches misses, and is
  already used by `<img>`. It is reused verbatim for background URLs.
- `render.Device.DrawImage(img, ctm, alpha, blend)` maps the image's unit square
  through `ctm`; it is a **single-draw-into-a-quad** primitive (no built-in tiling).
  `paintImage` (`pkg/layout/paint/paint.go`) draws `<img>` with the matrix recipe
  `Scale(w,-h)·Translate(x,y+h)·pageMat` and clips with `clipRect`+`Save/Restore`.
- A fragment stores only its **border box**; padding/content boxes are derived from the
  four `BorderEdge.Width`s (as `block.go` already does for the overflow clip rect).
- `ClipPushKind`/`clipRect` are available; `translateItems`
  (`fragment.go`) must get a case for any new coordinate-bearing item kind.

## Architecture

Four layers, each independently testable, mirroring how `<img>` already flows:

1. **Cascade (`pkg/css`)** — parse the longhands + extend the shorthand into new
   `ComputedStyle` fields. Pure string→struct; unit-tested in `shorthand_test.go` /
   `cascade_test.go`.
2. **Box/fragment construction (`pkg/layout/css`)** — when a box has a
   `BackgroundImage` ref, decode it via `e.images.get` and attach a resolved
   `BackgroundImageContent` to the `Fragment` (the decoded image + the geometry needed
   to paint it). Unit-tested by fragment-field assertions.
3. **Item emission (`pkg/layout/css/fragment.go`)** — emit a new
   `BackgroundImageKind` item in `appendSelfDecorations`, **between** the background
   color and the border. Add the `translateItems` case. Unit-tested by item-stream
   order assertions.
4. **Paint (`pkg/layout/paint` + `pkg/render`)** — a tiling painter that computes the
   painted image size (`background-size`), the first-tile origin (`background-position`
   within the origin box), the tile step, and loops `DrawImage` over the repeat axes,
   clipped to the `background-clip` box. Unit-tested with a fake `render.Device`
   counting `DrawImage` calls; pixel-tested via HTML goldens + a WPT reftest.

### Why a new item kind (not reuse `ImageItem`)

`ImageItem` has content-box + object-fit semantics (one draw, fitted into the content
box). Backgrounds have a different model: an **origin box**, a **clip box** (which may
differ), a **position** that can be negative / percentage / length, a **tile step**,
and per-axis repeat. Overloading `ImageItem` would muddy both. A distinct
`BackgroundImageItem` keeps each painter simple.

## Data model

### `ComputedStyle` additions (`pkg/css/cascade.go`)

```go
BackgroundImage    string         // resolved url() ref; "" = none
BackgroundRepeat   string         // "repeat" | "repeat-x" | "repeat-y" | "no-repeat" (initial "repeat")
BackgroundPosition BackgroundPos  // x/y as keyword/percentage/length (initial 0% 0%)
BackgroundSize     BackgroundSize // auto | cover | contain | <len/%>{1,2} (initial auto)
BackgroundOrigin   string         // "padding-box" (initial) | "border-box" | "content-box"
BackgroundClip     string         // "border-box" (initial) | "padding-box" | "content-box"
BackgroundAttach   string         // "scroll" (initial) | "fixed" (degraded to scroll)
```

None are CSS-inherited (correct: background properties don't inherit). Initials per
CSS Backgrounds 3. `background-color` stays as-is.

```go
// BackgroundPos is one axis pair of background-position. Each axis is resolved late
// against (paint-area-size − image-size); Kind selects how Value is read.
type BackgroundPos struct {
	X, Y PosComponent
}
// PosComponent is a length-or-percentage position component. Percent in [0,1];
// Length in px. Edge is the reference edge (for the 2/3/4-value "right 10px" forms;
// v1 supports keyword + single %/length, Edge defaults to start).
type PosComponent struct {
	Percent float64 // fraction [0,1] when IsPercent
	LengthPx float64
	IsPercent bool
}

// BackgroundSize selects the painted image size.
type BackgroundSize struct {
	Kind BackgroundSizeKind // Auto | Cover | Contain | Explicit
	W, H BgLength           // for Explicit; each may be Auto
}
type BgLength struct{ Auto bool; Percent float64; IsPercent bool; LengthPx float64 }
```

(Keep the structs minimal; `BackgroundPos`/`PosComponent` reuse the existing
`parseObjectPosition` keyword logic where it overlaps, extended for lengths.)

### `Fragment` addition (`pkg/layout/css/fragment.go`)

```go
// BackgroundImage is the resolved background image to paint behind this fragment's
// content (CSS Backgrounds 3), or nil for a box with no decodable background image.
BackgroundImage *BackgroundImageContent
```

```go
// BackgroundImageContent is a fragment's resolved background image plus everything the
// painter needs, all in page-space points. The origin box is where the image is laid
// out and positioned; the clip box is the paint area it is confined to (the two differ
// when background-clip ≠ background-origin).
type BackgroundImageContent struct {
	Img            image.Image
	IntrinsicW, IntrinsicH float64       // decoded pixel size (>0)
	OriginX, OriginY, OriginW, OriginH float64 // background-origin box
	ClipX, ClipY, ClipW, ClipH float64         // background-clip box
	Size   gcss.BackgroundSize
	Pos    gcss.BackgroundPos
	RepeatX, RepeatY bool
}
```

### `layout.Item` addition (`pkg/layout/page.go`)

```go
BackgroundImageKind // a tiled/positioned background image (Item.BgImage is set)
```

```go
// BackgroundImageItem is a CSS background image to paint behind a box's content. All
// rects are page space (points, Y-down). The painter resolves the painted tile size
// from Size and IntrinsicW/H, the first-tile top-left from Pos within the origin box,
// tiles along RepeatX/RepeatY, and clips every tile to the clip box.
type BackgroundImageItem struct {
	Img image.Image
	IntrinsicW, IntrinsicH float64
	OriginX, OriginY, OriginW, OriginH float64
	ClipX, ClipY, ClipW, ClipH float64
	Size BackgroundSizeResolved // a paint-layer-local copy (no css import in layout)
	PosXFrac, PosYFrac float64  // fallback fractional position (see resolution note)
	PosXPx, PosYPx float64      // length offsets
	PosXIsPct, PosYIsPct bool
	RepeatX, RepeatY bool
}
```

**Layering note:** `pkg/layout` must not import `pkg/css`. So the css `BackgroundSize`
is mapped to a small layout-local struct (`BackgroundSizeResolved{Kind int; W,H ...}`)
when building the item — the same way `ObjectFit` is a layout enum mapped from the css
keyword. The position components likewise map to plain float/bool fields on the item.

## Geometry (the painter)

Given the item, the painter computes, per CSS Backgrounds 3 §3.

1. **Painted tile size** from `background-size`:
   - `auto auto` → intrinsic size (`IntrinsicW × IntrinsicH`).
   - one axis a length/%, the other `auto` → preserve intrinsic ratio.
   - `contain` → scale to fit inside the origin box, preserving ratio (max scale with
     both ≤ box).
   - `cover` → scale to cover the origin box, preserving ratio (min scale with both ≥
     box).
   - explicit `<len/%>` per axis → that size (percent resolved against the origin box
     axis). Degenerate (≤0) → skip (no paint, logged).
2. **First-tile position** from `background-position` within the origin box:
   `pos = (originSize − tileSize) × percentFraction + lengthOffset`, per axis. For
   `repeat`, back the start off by whole tile steps so tiling covers from the clip
   box's leading edge: `start = pos − ceil((pos − clipLead)/step)·step`.
3. **Tile step** = tile size per axis (no `background-repeat: space`/`round` in v1 —
   both degrade to `repeat`, logged; `space`/`round` are a follow-up).
4. **Loop**: `Save()` → `clipRect(clipBox)`; for each tile origin from the start to past
   the clip far edge (one iteration when the axis doesn't repeat), `DrawImage` with the
   `Scale(tileW,-tileH)·Translate(x, y+tileH)·pageMat` recipe; `Restore()`.

A bounded tile-count guard (clip area / tile area, capped) prevents a pathological
1px-tile × huge-box blow-up; over the cap, log and draw up to the cap.

## Paint order

Insert the `BackgroundImageKind` emit in `appendSelfDecorations` between the
`BackgroundKind` (color) append and the border-edge loop. This yields the CSS order
color → image → border → content in every paint mode (the function is the first call in
all `AppendItems` branches). The image's own clip is internal to its painter (it does
**not** use the fragment overflow `ClipPush`), so a box with `overflow:visible` still
confines its background to its background-clip box.

`translateItems` gets `case layout.BackgroundImageKind:` translating all four
rect-origin pairs (Origin*, Clip*) by the relative offset, so a `position:relative` box
with a background image paints correctly when offset.

## Box construction

In `block.go` (and `replaced.go`, `table.go` for cell/replaced coverage), after the
background color is set on the fragment, if `b.Style.BackgroundImage != ""`:

- `d := e.images.get(ctx, b.Style.BackgroundImage)`; if `!d.ok`, skip (degrade: the
  color still paints; debug log — exactly like a missing `<img>`).
- Derive the origin box and clip box from the border box + the four border widths +
  padding (content box = border box inset by border+padding; padding box inset by
  border; border box as-is).
- Attach `&BackgroundImageContent{...}` with the decoded image, intrinsic size, origin
  & clip rects, and the style's size/position/repeat.

`background-repeat` maps to `(RepeatX, RepeatY)`: `repeat`→(t,t),
`repeat-x`→(t,f), `repeat-y`→(f,t), `no-repeat`→(f,f).

## Error handling / degradation

- Undecodable / 404 / missing / unsupported-format image → no background image painted,
  background color (if any) still paints, debug log. Never panics; recovery stays at the
  page boundary.
- `background-attachment: fixed` → treated as `scroll`, logged once.
- `background-repeat: space|round` → treated as `repeat`, logged.
- Gradients in `background-image` (`linear-gradient(...)`) → ignored (no url), color
  kept, logged (these are the separate gradients feature).
- Degenerate computed tile size (≤0) → skip paint, logged.

## Showcase + tests

Per the project rule (every feature lands with tests AND a visual entry):

- **Parse unit tests** (`pkg/css/shorthand_test.go`, `cascade_test.go`): the shorthand
  `background: #eee url(x.png) no-repeat center / cover` → color + image + repeat +
  position + size; each longhand; initials; the gradient/url-less degradations.
- **Geometry/painter unit tests** (`pkg/layout/paint/image_test.go` style, fake
  `render.Device`): `no-repeat` → 1 `DrawImage`; `repeat-x` over a box N tiles wide → N
  calls; `cover`/`contain` painted size; `background-position` first-tile origin; the
  tile-count cap.
- **Fragment/item unit tests** (`pkg/layout/css/fragment_test.go`): `BackgroundImage`
  attached when a decodable url is set; `BackgroundImageKind` emitted **after**
  `BackgroundKind` and **before** `BorderKind`; `translateItems` shifts it.
- **HTML goldens** (`pkg/doctaculous/html_golden_test.go`, `-update`): `html-bg-image`
  (a tiled small swatch), `html-bg-cover` (cover, single), `html-bg-position`
  (no-repeat, bottom-right). Use a `MapLoader` serving a generated PNG, mirroring
  `quadLoader`. Eyeball each.
- **WPT reftest** (`pkg/doctaculous/wpt_reftest_test.go`): `bg-image-vs-color` — a box
  with a `background-image` of a solid swatch renders pixel-identical to a box with the
  equivalent `background-color` (mirrors the `img-vs-div` / `solidSwatchLoader` pair).
- **Showcase**: add a "12 / BACKGROUNDS" section to `testdata/htmldoc/` (a tiled
  texture panel, a `cover` hero box, a positioned no-repeat badge), with an image asset
  under `testdata/htmldoc/img/` (generated by `gen_assets.go`). Regenerate the paginated
  goldens and bump `htmlDocPages`; eyeball the new page.

## Out of scope (follow-ups, each degrades today)

CSS gradients (`linear/radial/conic-gradient`); multiple comma-separated background
layers; `background-repeat: space|round`; `background-attachment: fixed` true viewport
anchoring; `background-blend-mode`; SVG/WebP background sources (the image cache has no
decoder); `image-set()`.
