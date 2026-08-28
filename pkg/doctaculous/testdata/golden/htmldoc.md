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

**19 / CUSTOM PROPERTIES**

## Custom properties and `var()`

The palette below is declared _once_ on `:root` and reached by every swatch through `var()`. Custom properties inherit, so a value set at the document root is visible to any descendant without being redeclared — the pattern almost every themed stylesheet is built on.

var(--brand)

var(--accent)

var(--muted)

var(--absent, #7a5cff)

var(--indirect)

scoped override

Look for: the last two swatches. _Indirect_ resolves `--indirect: var(--brand)` — a custom property whose own value is another `var()`, substituted recursively. _Scoped override_ redeclares `--brand` on the swatch itself, so the same `var(--brand)` reference resolves differently there than in the first swatch: custom properties cascade and inherit exactly like any other property, and the nearest declaration wins.

Substitution happens between the cascade and value parsing, so `var()` works in _any_ property, shorthands included — the rule below draws its whole border from one variable. A reference that cannot be resolved is _invalid at computed‑value time_, which is not the same as a syntax error: rather than leaving an earlier declaration showing, the property falls back to its inherited or initial value, as though `unset` had been specified.

border: var(--rule)

**20 / COLOUR**

## Alpha-bearing colour values

The engine has one CSS Color 4 colour grammar, shared by the HTML cascade and the SVG parser. Every swatch below straddles a pale field and a dark block, so an alpha channel that is honoured looks _different on each side_ while one that is dropped looks the same on both.

#4f9cff _(opaque reference)_

rgba(79,156,255,0.35)

#4f9cff59

#4bf6

hsl(214,100%,65%)

hsla(214,100%,65%,0.35)

rgb(79 156 255 / 35%)

hsl(214deg 100% 65% / 35%)

rebeccapurple

border rgba(176,48,48,0.5)

**Aa**

color rgba(20,30,45,0.4)

transparent _(valid, paints nothing)_

rgb(nope,0,0) _(dropped → red stands)_

Look for: `rgba(79,156,255,0.35)`, `#4f9cff59` and `rgb(79 156 255 / 35%)` are the SAME colour at the same alpha spelled three ways — 0.35 and 35 % and the hex byte `59` all resolve to an alpha of 89 — so those three swatches must be pixel-identical. The two `hsl` swatches sit one step away at (77,154,255) rather than (79,156,255): that is the honest result of converting HSL to RGB and rounding, not a parsing error. `#4bf6` is deliberately a _different_ colour (#44bbff at alpha 102) to show the four-digit form expanding each nibble. Every alpha-bearing swatch must read pale over the light field and muted-blue over the dark block; a swatch that looks the same on both halves has lost its alpha, and a swatch that is not there at all means the value failed to parse and the declaration was dropped.

The last two swatches are the error cases, and they use the `background-color` longhand deliberately. `transparent` is a _valid_ colour and correctly paints nothing. The malformed `rgb(nope,0,0)` is dropped per CSS whole-declaration error handling, so the earlier red declaration stays in force and the swatch renders solid red — a blank swatch there would mean the engine had wrongly accepted the bad value and painted transparent black. Through the `background` _shorthand_ the same bad value behaves differently, because that expander resets each sub-property before classifying components and tolerates a component it cannot classify; that divergence lives in the shorthand, not in the colour grammar.

Alpha survives the whole pipeline because the paint stack carries a live alpha channel: parsing produces an RGBA, the painter hands it to the device unchanged, and the rasteriser composites it over whatever it lands on. In PDF output the same colours are emitted through the writer's own colour handling rather than being pre-blended, so the text above stays selectable and the fills stay vector.

**21 / MISSING GLYPHS**

## Characters No Font Can Draw

No bundled face covers emoji or Devanagari, and none ever will — the bundle is a handful of permissively‑licensed Latin, Hebrew and Arabic substitutes. A character none of them maps now draws `.notdef`, the “tofu” box, exactly as a browser does.

Nine weather emoji. Every one is unmappable, so every one is a box:

🌈🌤🌥🌦🌧🌨🌩🌪🌫

The box is not a placeholder pasted over the line — it is a glyph, and it shapes like one. Below, unmappable characters sit inline with text that resolves normally, and the correct text keeps its own metrics on either side:

Latin, then Devanagari कखग, then Latin again.

Whether the mark is drawn by the font or by us depends on the font. A face that ships its own `.notdef` gets _its_ glyph — DejaVu draws a hollow box, Noto draws a box containing the code point's hex digits. The bundled TeX Gyre substitutes ship a `.notdef` that is _blank_, so for the boxes above the geometry is synthesized. Each distinct missing character is also reported once through the layout log, which is the half that makes a font gap diagnosable rather than merely visible.

Invisible characters are deliberately excluded. A no‑break space, a zero‑width joiner or a variation selector draws no ink even in a font that maps it, so giving it a box would invent a mark the author never wrote. The sentence you are reading contains a U+202F NARROW NO‑BREAK SPACE that no bundled face maps; it renders as space, and nothing is logged, because nothing is wrong.

Look for: the boxes have a side bearing, so they read as separate marks rather than one bar, and they sit on the baseline with the text around them. This matters more than it looks. Before `.notdef`, an unmappable character rendered as _nothing_ — and because the surrounding text was untouched, a page with a font gap looked like a page with a _layout_ bug. It is worse when only some characters of a set are missing: the report behind this section was a board carrying only DejaVu and Liberation, where three of nine weather emoji drew and six vanished.

**22 / GRADIENTS**

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

**23 / GRADIENTS, CONTINUED**

## Repeating gradients, and the `background-*` properties

A gradient is a generated _image_, so it answers to the same sizing, tiling and positioning properties a bitmap does. These swatches are the proof: the same declarations that place a `url()` background place a gradient one.

repeating, 16px period

repeating at 45deg

repeating radial

background-size: 40px

60% size, centered, no-repeat

Look for: a repeating gradient takes its _stop range_ as the period, so the ramp tiles rather than stretching once. The last two swatches show a gradient really is a background image — `background-size` resizes its box, and the result tiles or is placed by `background-position` exactly as a bitmap would.

**24 / BORDER RADIUS**

## Rounded Corners

Every form of `border-radius`: circular and elliptical corners, the four per‑corner longhands, percentage radii, the overlap‑correction rule, and rounded borders with their true inner curve.

none

no radius

8px

border-radius: 8px

pill

border-radius: 999px

50%

border-radius: 50%

30/12

border-radius: 30px / 12px

4 vals

2px 10px 20px 10px

Look for: the `50%` tile is a true ellipse rather than a circle, because its box is wider than it is tall — a percentage resolves the horizontal radius against the box's _width_ and the vertical one against its _height_. The `30px / 12px` tile shows the same asymmetry written explicitly with the slash form, and the last tile gives all four corners a different radius in one declaration.

thin

2px border, 14px radius

thick

10px border, 14px radius

inner

12px border, 6px radius

over

radius 200px, corrected

Look for: the inner curve. A rounded border is not the outer path stroked — the inner edge's radius is the outer radius _minus_ the border width, so the thick tile's hole is far less round than its outside, and in the third tile the border is thicker than the radius, which squares the inner corner completely while the outside stays round. The last tile asks for a 200px radius on a box far smaller than that; the corners cannot overlap, so every radius scales down by one shared factor until they exactly meet.

The overlap correction (CSS Backgrounds 3 §5.1) is what makes an over‑large radius degrade gracefully rather than producing self‑crossing arcs: each side compares its length against the sum of the two radii meeting along it, and the _smallest_ of those ratios scales all eight radius components at once. That single shared factor is why `border-radius: 999px` on the pill above resolves to exactly half the box's height on every corner.

Backgrounds and borders both follow the rounded outline, and a background _image_ is clipped to it. In PDF output the corners stay genuinely curved: the writer emits the same cubic curve path natively, so a rounded box remains vector rather than being flattened to a picture. A rounded border is filled as one ring in a single colour, so a dashed or multi‑coloured rounded border paints solid — the engine logs that substitution rather than making it silently.

**25 / MID-WORD BREAKING**

## The `overflow-wrap` & `word-break` Properties

Where a line may break _inside_ a word. Normal wrapping only breaks at spaces, so a single long token — a URL, a hash, a compound noun — runs straight past the edge of its box. These two properties add opportunities between characters, and they differ in when they take them.

#### The problem — one unbreakable token

https://example.com/very/long/path/to/a/private/calendar/feed.ics

Default `overflow-wrap:normal`: the token has no space in it, so there is nowhere to break and it overflows the box on a single line.

#### `overflow-wrap: break-word` — break only as a last resort

https://example.com/very/long/path/to/a/private/calendar/feed.ics

The same token now breaks between characters and stays inside the box. The legacy alias `word-wrap:break-word` is accepted for the same effect.

#### Last resort vs. eager — the distinction that matters

wrap this sesquipedalian

`break-word` moves a word that _fits on a line of its own_ down whole, breaking it only when it cannot fit anywhere.

wrap this sesquipedalian

`break-all` is eager: every character boundary is an ordinary opportunity, so the word is chopped at the line edge regardless.

#### `word-break: keep-all` — suppress mid‑word breaking

https://example.com/very/long/path/to/a/private/calendar/feed.ics

`keep-all` forbids breaking the affected text, so it overrides `overflow-wrap` and the token overflows again.

Every mid‑word break lands on a grapheme‑cluster boundary (UAX #29), so a combining mark is never separated from its base letter and an emoji ZWJ sequence or a flag is never split in half. `overflow-wrap:anywhere` breaks in the same places as `break-word` but additionally shrinks a box's intrinsic _min‑content_ width, which is visible in flex and grid sizing rather than in the line breaking itself.

**26 / BOX-SHADOW**

## CSS `box-shadow`

Every tile below is identical — a flat fill inside a hairline border — under a different `box-shadow` declaration. Unlike a `filter`, a shadow is not computed from the box's pixels: it is the box's own _shape_, displaced, grown and blurred, so it costs nothing to draw and stays sharp at any resolution.

none

6px 6px

6px 6px 8px

0 0 0 5px

8px 8px 4px −4px

5px 5px 4px (currentColor)

inset 6px 6px 6px

inset 5px 0 0

inset 0 0 0 5px

three offsets, comma‑separated

outer + inset in one list

Look for: the four arguments separating cleanly. _offset_ moves the shape; _blur_ softens its edge over a distance equal to the radius, centred on the edge; _spread_ grows the shape on all four sides before that blur; and a _negative_ spread shrinks it — the −4px swatch casts a visibly narrower shadow than its 8px offset alone would. The swatch with no colour written takes the element's own `color`.

The `inset` keyword is a genuinely different rendering, not a sign flip. An outer shadow fills the region _outside_ the border box — so a tile with a transparent background shows a ring, never a filled blob — while an inset shadow fills the part of the _padding_ box its own shape does not cover, and can never escape the box however far it is offset. That is why an inset spread runs the other way: growing the shape shrinks the lit interior, which _thickens_ the band. The last two swatches show a comma‑separated list, where the first shadow in the list paints on top of the ones after it, and where an outer and an inset shadow coexist because they sit in different slots of the box's paint order — outer behind the background, inset over the background but under the border.

A shadow with no blur is a plain vector fill, so the common patterns — a hard offset, a spread ring, an `inset` colour spine — stay fully vector in PDF output. A _blurred_ shadow needs an offscreen raster surface, which a PDF writer does not have and PDF has no operator for; there it degrades to the same shadow with a hard edge, at the same place and size, rather than rasterising the page or dropping the decoration.

**27 / TRACKING**

## The `letter-spacing` & `word-spacing` Properties

Extra space added _between_ characters and _at_ word separators. Both are inherited, both accept negative lengths, and both are folded into each glyph's advance during shaping — so line breaking, intrinsic sizing and alignment all see the adjusted widths without knowing the properties exist. `normal` resets an inherited value, and an `em` length inherits as _specified_, re‑resolving against each descendant's own font size.

#### `letter-spacing` — positive tracks, negative tightens

Tracking changes the texture of a line.

The control: `letter-spacing:normal`.

Tracking changes the texture of a line.

`letter-spacing:3px` — every character gains 3px of advance.

Tracking changes the texture of a line.

`letter-spacing:-0.5px` — a negative value is legal and tightens. Advances are floored at zero, so an extreme negative value overlaps glyphs rather than producing the negative advances the line breaker cannot represent.

#### `word-spacing` — separators only

Only the gaps between words grow here.

`word-spacing:10px` widens U+0020 and U+00A0 and nothing else, so the words themselves are untouched.

#### Where the spacing lands at the end of a line

flush right

flush right

Both boxes are right‑aligned. CSS Text 3 words `letter-spacing` as spacing _between_ characters, but every shipping browser adds it after _every_ character including the last — which is why the tracked line stops one tracking‑width short of the right edge. This engine matches the browsers. SVG deliberately does _not_: SVG 1.1's wording adds no trailing gap, so `pkg/svg` keeps its trailing edge flush. Two specs, not an inconsistency.

**28 / TRACKING, CONTINUED**

## Tracking changes where lines break

Spacing is folded into each glyph's advance during shaping, so everything downstream sees the adjusted widths without knowing the properties exist. Line breaking is the visible proof.

tracking moves words

tracking moves words

The same text in the same box. The spacing is folded into each glyph's advance, so the greedy line breaker sees the wider run and pushes a word down with no change to the breaker itself — the same mechanism that makes _min‑content_ and _max‑content_ account for tracking.

**29 / UNRESOLVABLE FONT FAMILIES**

## Text always renders, even with no matching font

A `font-family` naming only families the host does not have used to render _nothing at all_ — the run was skipped, so the page came out an empty box. Every line below names at least one unavailable family; all of them draw.

A family that exists nowhere.

The whole list is unresolvable, so it degrades to the bundled serif and is logged once. Previously: no ink whatsoever.

An absent family, then a generic.

The generic keyword still terminates the list the way CSS says it should — this line was always fine, and is the control proving the fallback above did not simply replace normal resolution.

**Bold** and _italic_ survive the substitution.

The fallback is style‑aware: the bundled bold and italic faces are selected, rather than every run collapsing to one regular face. The weight and slant here come from `<strong>` and `<em>` via the UA stylesheet — see §30.

A related failure sat one layer down. The OS font matcher never reports a miss — asked for a family it does not have, it confidently returns some other installed font. A request for `DejaVu Sans` came back as Lucida Grande, and `Roboto`, `IBM Plex Mono` and a deliberately nonexistent name all returned the identical Arial Unicode bytes. Each resolved face is now checked against the family its own `name` table declares, so a wrong answer becomes an honest miss and falls through to the bundled face above.

**30 / INLINE EMPHASIS DEFAULTS**

## Semantic emphasis tags carry their own weight and slant

These tags need no author CSS: the user‑agent stylesheet gives them a weight or a slant, the way a browser does. Without those rules the markup still parsed and still converted to Markdown correctly — it was only _invisible_ in every rasterized format.

**strong** and **b** are bold. _em_, _i_, _cite_, _var_ and _dfn_ are italic. u and ins are underlined.

Each tag resolves to exactly the same computed style as its CSS equivalent (`font-weight:bold`, `font-style:italic`, `text-decoration:underline`) — the UA rules are the only difference between this line and unstyled text.

** A bold line where strong is nested inside.**

Nesting does not flatten: `<strong>` inside already‑bold text stays bold. The HTML spec spells these defaults `bolder` and `smaller`, but this engine's `font-weight` is the binary bold/normal its four‑style bundled families can express and its `font-size` takes a length, so neither relative keyword parses — they would be dropped as invalid and the emphasis would stay invisible. The sheet uses `bold`, the honest spelling of what the renderer can do, and a test pins that choice.

`<small>` and `<big>` are deliberately absent rather than given a hardcoded pixel size, which would be wrong at every font‑size but the default. `<mark>` is absent too: its default is a _background_, and backgrounds do not paint on non‑replaced inline boxes here — an inline `<span>` with an explicit `background-color` paints nothing, while the same span at `display:inline-block` paints. A `<mark>` rule would cascade and then do nothing, which reads as support; it belongs with the inline‑background work instead.
