Doctaculous

Pure‑Go document toolkit HTML rendering specimen

# The Rendering Feature Tour

A single document that exercises every implemented slice of the HTML / CSS / image pipeline — typography, the box model, floats, flexbox, CSS grid, tables, positioning, overflow, stacking, and decoded images — fetched over HTTP and paginated onto Letter pages.

**01 / TYPE**

## Typography & Inline Flow

Block stacking, inline wrapping, three font families, and inline atoms sharing one baseline.

### Headings cascade from the UA sheet

Body copy is set in `TeX Gyre Termes`, a serif. Headings switch to `TeX Gyre Heros`, a grotesque. This paragraph carries enough text to wrap across several lines at the page measure, so greedy line‑breaking, leading, and left alignment are all visible. Short inline runs such as `display:inline-block` sit on the same line as the surrounding prose.

BADGE

> “Accept interfaces, return concrete types.” A pull‑quote drawn with a gold left rule and the sans family.

#### Alignment

This line is centered.

This line is right‑aligned.

**02 / BOX**

## The Box Model & Borders

Padding, margins, backgrounds, and every border style the engine renders — including the four 3D bevels.

border: outset

border: inset

border: ridge

border: groove

Four `border-style` bevels. Outset reads raised, inset sunken; ridge and groove are their two‑facet cousins.

Line styles: `solid`, `dashed`, `dotted`, `double`.

**03 / FLOAT**

## Floats & Clear

A floated figure with text wrapping beside it, then a cleared block returning to full width.

img/photo.jpg · JPEG

This paragraph wraps its lines along the right edge of the left‑floated figure. The decoded JPEG to the left is a continuous‑tone image, so it exercises the baseline DCT decode path rather than a flat fill. As the text continues past the bottom of the figure box, the lines reclaim the full measure of the column and run edge to edge again, exactly as float behaviour requires. The figure keeps its margin gap so the text never touches the frame.

A `clear:left` block drops below the float and spans the full width beneath it.

**04 / FLEX**

## Flexbox

Proportional growth, space distribution, cross‑axis centering, and multi‑line wrapping with `align-content`.

#### `flex-grow` in 1 : 2 : 3 proportion

#### `justify-content: space-between`

#### `align-items: center`

A short and a tall item, both centered in the strip.

#### `flex-wrap: wrap` with `gap`

Items too wide to share one line break onto the next; the `gap` applies both between items and between the lines.

#### `align-content: space-between` on a taller container

With more cross space than the lines need, `align-content` distributes the surplus between them — here pinning the first and last lines to the container's edges. The default is `stretch`, which instead grows each line to absorb it.

**05 / GRID**

## CSS Grid

Explicit tracks, named template areas, fractional units, spans, and auto‑placement.

#### Named areas & `fr` tracks

A header spanning both columns over a 2fr main / 1fr side row.

#### Auto‑placement with a spanning tile

The gold tile spans two columns; the rest auto‑flow into the grid.

**06 / TABLE**

## Tables

Collapsed borders, a caption, a spanning header, striped rows, and a total rule — then separate borders with column / row background layers.

****Quarterly Ledger****

| Quarter | Region | Units | Revenue |
| --- | --- | --- | --- |
| Q1 | North | 1,204 | $48,160 |
| Q2 | North | 1,560 | $62,400 |
| Q3 | South | 1,028 | $41,120 |
| Total | Total | Total | $151,680 |

| a1 | b1 | c1 | d1 |
| --- | --- | --- | --- |
| a2 | b2 | c2 | d2 |
| a3 | b3 | c3 | d3 |

Separate borders: column 2 carries a tint, alternating rows a stripe; where they cross, the row layer wins.

**07 / LAYER**

## Positioning, Overflow & Stacking

Absolute pinning, relative paint‑time offset, overflow clipping, and z‑index order.

![gold ring badge](img/icon.gif)

This stage is `position:relative`. The gold badge is `position:absolute`, pinned to its top‑right corner and painted above this text. The GIF inside it decodes through the indexed palette path.

The rust tag is `position:relative`, nudged down and right from its in‑flow slot at paint time.

#### `overflow: hidden`

A 320×320 child clipped to the padding box.

#### `z-index` order

Higher z paints on top: gold over slate over rust.

**08 / IMAGE**

## Decoded Images & `object-fit`

One source square fitted three ways into wide frames.

object-fit: contain

object-fit: cover

none + position

The four‑quadrant PNG makes orientation unambiguous: _contain_ letterboxes the whole square, _cover_ fills and crops, and _none_ pins the unscaled image to the bottom‑right.

HEIC (pure‑Go HEVC decode)

PNG reference

The same quad image as an Apple‑encoded HEIC decoded by the in‑tree HEVC decoder, beside its lossless PNG twin — identical layout, near‑identical pixels (HEIC is lossy 4:2:0).

**09 / FORMS**

## Form Controls

Static, non‑interactive native widgets — text fields, buttons, checkboxes, radios, a textarea, and a select — sized and painted with classic chrome.

Full name

Email

Password

Notes

Plan

**Preferences**

Subscribe to updates

Remember this device

Contact by email

Contact by phone

**10 / WHITE-SPACE**

## The `white-space` Property

How runs of spaces, tabs, and newlines are collapsed or preserved, and whether lines wrap.

#### `pre` — preserve everything, no wrap (tab‑aligned)

```
name      role        team
ada       founder     core
grace     compiler    tools
margaret  navigation  apollo
```

Spaces, the literal newlines, and tab stops are preserved; long lines do not wrap (they overflow / are clipped by the box).

#### `pre-wrap` — preserve, but wrap

```
This preformatted block keeps    its    runs of spaces and its
explicit line breaks, but unlike pre it still wraps a line that is too long to fit within the width of its containing box.
```

#### `pre-line` — collapse spaces, keep newlines

Spaces collapse to one, but each newline is kept.

#### `nowrap` — collapse, never wrap

This single line never wraps no matter how narrow the box is — it just runs straight off the right edge and is clipped.

The clip box (`overflow:hidden`) cuts the overflowing line at its edge.

**11 / LISTS**

## Lists & Counters

Bullets and numbers, nested marker rotation, the numbering styles, and CSS counters.

#### Unordered (nested)

- Disc at the top level
- Another item
  - Circle one level in
    - Square two levels in
- Back to disc

#### Ordered styles

1. Decimal one
2. Decimal two

a. Lower-alpha
b. Lower-alpha

I. Roman
II. Roman
III. Roman
IV. Roman

#### Nested numbering with `counters()`

1. First section
  1. Subsection
  2. Subsection
2. Second section

The inner list's `counter-reset` opens a new scope; `counters(list-item, ".")` would join them as 1.1, 1.2 — here the default per-list numbering restarts each level.

**12 / BACKGROUNDS**

## Background Images

A tiled texture, a cover hero, and a positioned badge — `background-image` with repeat, size and position.

#### Tiled texture (`repeat`)

A small seamless tile repeats to fill the panel. The text sits on top of the background, which paints behind the content and inside the padding box.

#### `cover`, centered

The photo scales to cover the box, cropping the overflow.

#### Positioned badge

A single icon, `no-repeat`, pinned bottom-right over a tint.

**13 / LINKS**

## Links

The link pseudo-classes: a UA default, an author `a:link` color, an underline opt-out, and an inert `:visited` rule.

Body copy with a [default hyperlink](https://example.com) — blue and underlined, the user-agent style for an unvisited link.

A [call-to-action link](https://example.com) recolored with `a:link { color }`, keeping its underline.

A [borderless link](https://example.com) with `text-decoration: none` — colored but not underlined.

A `:visited` rule is parsed but never matches in a static render (no browsing history), so [this link](https://example.com) stays the unvisited style — and a bare anchor with no `href`, like this, is not a link at all.

**14 / LEGACY ATTRIBUTES**

## Presentational Attributes

Pre‑CSS styling via HTML attributes — `bgcolor`, `align`, `cellpadding`, `border`, `<font>` — mapped to CSS as below‑cascade hints (real CSS still wins).

The kind of table the early web was built from: colored rows, padded bordered cells, and aligned columns, all set with attributes rather than a stylesheet.

| Component | Language | Lines |
| --- | --- | ---: |
| Parser | Go | 4,210 |
| Layout engine | Go | 11,930 |
| Rasterizer | Go | 6,740 |

An obsolete `<font>` still works: large red text, and an `<ol type="I" start="3">` numbers with the attribute:

III. Third in Roman
IV. Fourth in Roman

**15 / DIRECTION**

## RTL & Bidirectional Layout

`direction: rtl` runs the inline axis right‑to‑left. Tables mirror their column order, flex rows reverse their main axis, grids mirror their tracks, and the direction‑relative `start`/`end` keywords resolve against it.

The table below sets `dir="rtl"`. Column one is the right‑most, and the border‑collapse grid lines mirror with it:

| First | Second | Third |
| --- | --- | --- |
| Rightmost cell | Middle cell | Leftmost cell |

A flex row under `dir="rtl"` packs from the right. The items are authored one‑two‑three in source order, so item one takes the right‑most slot:

One

Two

Three

A grid mirrors its tracks the same way — the `1fr 2fr` ratio is preserved, but the single‑width track is now on the right:

1fr — right track

2fr — left track

Finally, `text-align` takes the logical keywords. Both paragraphs below use `text-align: end`; only their `direction` differs, so _end_ resolves to opposite edges:

LTR — `end` resolves to the right edge

RTL — `end` resolves to the left edge

**15 / DIRECTION**

### Real script

Everything above mirrors _boxes_. Text inside a line is reordered separately, per UAX #9: shaping and line‑breaking run in logical order, and each line is reordered to visual order once the break is chosen. The Hebrew below is authored left‑to‑right in the source and reads right‑to‑left on the page:

שלום עולם

A right‑to‑left phrase inside an otherwise left‑to‑right paragraph reorders in place, and the Latin around it does not move — the bracket pairs mirror too (rule L4):

The greeting (שלום) sits inline.

Arabic additionally needs _contextual_ shaping: a letter takes a different form depending on whether it joins to its neighbours, and some pairs fuse into one glyph. These are shaped through the font’s OpenType tables, so the letters connect rather than standing as isolated forms:

مرحبا بالعالم

**16 / CROP**

## Exact-size Cropping

Filling an exact pixel box, rather than fitting within one.

`--max-width`/`--max-height` fit a render inside a box with aspect preserved, so a 3:2 source can never fill a square target — it comes back 300×200, not 200×200. `ImageOptions.Crop` fills the target instead, discarding what falls outside. The source below is 480×360:

img/crop-source.jpg · 480×360 source

The same square target, placed three ways. _Saliency_ scores candidate windows on edge energy, saturation, skin likelihood and a centre prior — no model, no training data — and picks the highest; the gravities place the window deterministically:

saliency

center

west

The hippo’s head — the most textured part of the frame — sits right of centre, so the saliency window shifts toward it: `(110,0)–(470,360)` against the centre crop’s `(60,0)–(420,360)`. `EncodeImageRect` returns that rectangle, so a caller can record where a smart crop landed and replay it later with `StrategyRect`.

**17 / LANDSCAPE**

## Landscape Reflow

This section selects a wider `@page landscape` via `page: landscape`; its content reflows to the wider measure of a landscape US‑Letter page (1056×816px) instead of the portrait column.

Because the named page is wider, the flex row below stretches edge to edge across the full landscape measure, and the table beneath it reflows to the same wide width — both visibly wider than any portrait page in this specimen. The running chrome (the head at top‑left, the section title at top‑right, and the page counters along the bottom) carries over onto the landscape page band unchanged.

Parser & xref

Content interpreter

Raster backend

Reflow engine

CSS cascade

| Pipeline stage | Package | Input | Output | Notes |
| --- | --- | --- | --- | --- |
| Parse | pkg/pdf | bytes | Document | xref tables & streams, object streams |
| Interpret | pkg/pdf/content | content stream | paint ops | paths, text, images, shadings |
| Reflow | pkg/layout/css | cssbox tree | fragment tree | block / inline / flex / grid / table |
| Raster | pkg/render/raster | paint ops | bitmap | pure‑Go, dependency‑light |

Doctaculous · pure‑Go, MIT‑licensed · rendered from HTTP‑fetched HTML, CSS, fonts and images, paginated onto US‑Letter pages. Set in TeX Gyre Heros & Termes with Inconsolata; wordmark in Pacifico.

**18 / FILTER**

## CSS `filter`

Every swatch below is the _same_ source rendering — a bordered tile with text and two colour bands — under a different `filter` declaration. The effect runs over the box's flattened pixels, so its border, background, text and children are all filtered together.

**none**

unfiltered

**gray**

grayscale(1)

**sepia**

sepia(1)

**sat**

saturate(2.5)

**hue**

hue-rotate(140deg)

**inv**

invert(1)

**bright**

brightness(1.6)

**contr**

contrast(2.2)

**alpha**

opacity(0.4)

**blur**

blur(2px)

**shadow**

drop-shadow(5px 5px 3px)

**chain**

grayscale(1) brightness(1.5) contrast(1.4)

Look for: the two spatial functions. `blur(2px)` and `drop-shadow()` both reach _outside_ the tile's border box — CSS, unlike SVG, does not clip a filter to a region — while the eight colour adjustments recolour each pixel in place. The last swatch chains three functions, which apply left to right: grey first, then brighten that result, then raise its contrast.

The chain runs over the box's flattened pixels in an offscreen surface, which is why a filtered box also establishes a block formatting context and a stacking context: its whole rendering has to compose as one isolated group before the effect can apply. In PDF output the same content paints _unfiltered_ — a PDF writer has no offscreen raster surface and PDF has no filter operator, so the content stays vector and correctly placed rather than being rasterised into a picture of itself.

**19 / GRADIENTS**

## CSS `linear-gradient()` and `radial-gradient()`

A gradient is a generated _background image_, not a colour: it is painted by the same shading engine SVG paint servers use, evaluated per device pixel rather than baked into a bitmap. Every tile below is 116×58 — deliberately not square, because a `to <corner>` gradient's line is only 45° on a square.

### Direction

(none) → to bottom

to right

to left

to top

45deg

0.75turn

to bottom right

to top right

Look for: the two corner swatches. Their bands are _not_ at 45°. CSS angles the gradient line so the perpendicular through each end lands exactly on a corner, which on a 2:1 box makes the line noticeably shallower — and puts the two _other_ corners at precisely the midpoint colour. `0deg` points up and angles run clockwise, so `0.75turn` (270°) matches `to left`.

### Colour stops

four colours, no positions

25% … 75%

two stops at 50% (hard break)

four hard bands

→ transparent

Look for: stops with no position are spread _evenly_ between the ones that have them, so the four-colour swatch changes at 33% and 67% without being told to. Two stops sharing a position give a hard edge with no blend at all — the mechanism behind the four-band swatch. The last swatch fades to `transparent` and stays _red_ the whole way: interpolation runs in premultiplied alpha, which is what keeps a grey band from appearing through the middle of it.

### Radial

ellipse (default)

circle

closest-side

circle farthest-side

circle at 20% 30%

Look for: the default ending shape is an _ellipse_ sized to the farthest corner, so it stretches with the box; adding `circle` makes both radii equal and the rings become round. The sizing keyword picks which box feature the shape must reach — `closest-side` lands the end colour on the nearest edges, so the corners sit beyond the ramp and are solid.

### Repeating, and the background-\* properties

repeating, 16px period

repeating at 45deg

repeating radial

background-size: 40px

60% size, centered, no-repeat

Look for: a repeating gradient takes its _stop range_ as the period, so the ramp tiles rather than stretching once. The last two swatches show a gradient really is a background image — `background-size` resizes its box, and the result tiles or is placed by `background-position` exactly as a bitmap would.
