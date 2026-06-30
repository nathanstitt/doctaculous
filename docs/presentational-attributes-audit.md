# Presentational HTML attributes — support audit

Date: 2026-06-29

Legacy HTML "presentational attributes" set styling via element attributes rather
than CSS. The HTML spec (§15.3 "Presentational hints") defines how each maps to a
CSS property: the UA applies it as an author-level-zero hint, so an explicit CSS
rule always wins. Our cascade today reads **only CSS** plus a few attributes
(`<img width/height>`, table `colspan`/`rowspan`/`span`, and the form-control
attributes). Everything below that we don't handle renders with no effect.

This audit lists the presentational attributes likely encountered in the wild, what
each maps to, whether we support it, and a rough priority. All the CSS *targets*
already exist in the engine (`background-color`, `border-spacing`, `vertical-align`,
`text-align`, `width`/`height`, `border`, `padding`, `object-fit`), so adding these
is attribute→CSS mapping in box generation — no new layout machinery.

## Already supported

| Attribute | Elements | Maps to / behavior |
|---|---|---|
| `width`, `height` | `<img>` (+ form `size`/`cols`/`rows`) | replaced intrinsic size; CSS wins |
| `colspan`, `rowspan` | `<td>`/`<th>` | table cell spans |
| `span` | `<col>`/`<colgroup>` | column span |
| `style` | all | inline CSS (full cascade) |
| `href`, `rel` | `<link>` | stylesheet resolution |
| `src` | `<img>` | image source |
| `type`, `value`, `checked`, `disabled`, `placeholder`, `selected` | form controls | control kind/state |

## NOT supported — presentational attributes

Priority: **P1** = very common in real-world/legacy pages, clear visual impact;
**P2** = common; **P3** = niche or deprecated-rare.

### Color (P1)

| Attribute | Elements | Maps to | Notes |
|---|---|---|---|
| `bgcolor` | `<body>`, `<table>`, `<tr>`, `<td>`, `<th>`, `<col>` | `background-color` | **The HN case.** Extremely common in legacy/email-style HTML. |
| `text` | `<body>` | `color` | body default text color |
| `color` | `<font>` | `color` | `<font>` is obsolete but still appears |
| `bordercolor` | `<table>`, `<td>`, `<tr>` | border color | IE-era; less common but seen |
| `link`, `vlink`, `alink` | `<body>` | link colors (`:link`/`:visited`) | needs link pseudo-classes (not modeled); low value |

### Dimensions (P1–P2)

| Attribute | Elements | Maps to | Notes |
|---|---|---|---|
| `width` | `<table>`, `<td>`, `<th>`, `<col>`, `<hr>` | `width` (px or %) | **P1** — table/column widths are everywhere in legacy layout |
| `height` | `<table>`, `<td>`, `<th>`, `<tr>` | `height` | **P1** alongside width |

We map `width`/`height` for `<img>` only today; extending to table parts is the
high-value gap.

### Alignment (P1)

| Attribute | Elements | Maps to | Notes |
|---|---|---|---|
| `align` | `<td>`, `<th>`, `<tr>`, `<table>`, `<p>`, `<div>`, `<h1>`–`<h6>`, `<col>` | `text-align` (left/right/center/justify) — and for `<table>`/`<img>` `align=left|right` → `float` | **P1** — HN uses `align` 30×. Note the dual meaning. |
| `valign` | `<td>`, `<th>`, `<tr>`, `<col>` | `vertical-align` (top/middle/bottom) | **P1** — HN uses `valign` 60×; we support cell vertical-align already |
| `align=left/right` | `<img>`, `<table>` | `float: left/right` | floats an image/table; common in articles |
| `align=center` | `<div>`/legacy | centering | maps to text-align/centering |
| `<center>` element | — | `text-align:center` + block | not an attribute but same family; obsolete-common |

### Table chrome (P2)

| Attribute | Elements | Maps to | Notes |
|---|---|---|---|
| `cellspacing` | `<table>` | `border-spacing` | **P2** — common in legacy tables (HN uses it) |
| `cellpadding` | `<table>` | cell `padding` (applies to every cell) | **P2** — common; needs propagation to cells |
| `border` | `<table>` | table + cell borders (border=N → 1px outer + 1px cells, roughly) | **P2** — HN uses `border="0"`; mostly seen as 0 to suppress |
| `nowrap` | `<td>`, `<th>` | `white-space: nowrap` | **P3** — we don't model `white-space` yet, so deferred |
| `frame`, `rules` | `<table>` | which borders show | **P3** — rare |

### Image/inline spacing (P2–P3)

| Attribute | Elements | Maps to | Notes |
|---|---|---|---|
| `hspace`, `vspace` | `<img>` | horizontal/vertical `margin` | **P3** — old; occasional |
| `border` | `<img>` | border width | **P3** — old |
| `align` | `<img>` | `float` (left/right) or vertical-align (top/middle/bottom) | **P2** — the float case matters for article images |

### Body layout (P2)

| Attribute | Elements | Maps to | Notes |
|---|---|---|---|
| `background` | `<body>`, `<td>` | `background-image` (URL) | **P2**, but we don't decode CSS `background-image` yet (D4 deferral) — so deferred until that lands |
| `marginwidth`, `marginheight`, `topmargin`, `leftmargin` | `<body>` | body `margin`/`padding` | **P3** — IE/Netscape-era |

### Lists (P3)

| Attribute | Elements | Maps to | Notes |
|---|---|---|---|
| `type` | `<ol>`, `<ul>`, `<li>` | `list-style-type` | needs list markers (not rendered yet) — deferred |
| `start`, `value` | `<ol>`, `<li>` | counter start/value | needs list numbering — deferred |

## Agreed plan (2026-06-29)

Decision: implement the **missing engine features (P3 prerequisites) FIRST**, then do
**all** presentational-attribute mapping (P1 + P2 + P3) together at the end, so every
attribute has a real CSS target when the mapping lands. Several "P3" items
(`white-space:nowrap`, list `type`/`start` markers, `background` image) are vital in
the wild, not niche — they were under-served by the earlier fidelity pass and are
treated as first-class here.

Order:

1. **Engine feature — `white-space`** (`normal` collapse, `nowrap`, `pre`, `pre-wrap`).
   Touches the shared inline core (shaping/breaking). Enables the `nowrap` attribute.
2. **Engine feature — list markers** (`list-style-type`/`-position`/`-image`, marker
   box generation, ordered-list numbering). Box generation + paint. Enables `<ol>`/
   `<ul>`/`<li>` `type`/`start`/`value`.
3. **Engine feature — CSS `background-image`** (decode + paint; position/repeat). Paint.
   Enables the `background` attribute (and closes the D4 deferral).
4. **Engine feature — link pseudo-classes** (`:link`/`:visited`). Selector matching.
   Enables `link`/`vlink`/`alink` (lowest value).
5. **Presentational-attribute mapping (P1 + P2 + P3)** — one pass applying every
   attribute below as a below-cascade hint (CSS wins). Now unblocked by 1–4.

Each of 1–4 is its own brainstorm → spec → plan → PR. Step 5 is the final PR.

## Recommended implementation order (within the final attribute-mapping pass)

1. **P1 color**: `bgcolor` (body/table/tr/td/th/col → `background-color`), `text` (body → `color`), `<font color>`. *(The HN fix.)*
2. **P1 dimensions**: `width`/`height` on table parts (`<table>`/`<td>`/`<th>`/`<col>`) → `width`/`height`.
3. **P1 alignment**: `align` → `text-align` (and `align=left|right` on `<img>`/`<table>` → `float`); `valign` → `vertical-align`.
4. **P2 table chrome**: `cellspacing` → `border-spacing`, `cellpadding` → cell padding, `border` → table/cell borders.
5. **P2 image**: `<img align>` → float, `hspace`/`vspace` → margins, `bordercolor`.
6. **Deferred (need missing features)**: `background` attr (needs `background-image`), `nowrap` (needs `white-space`), list `type`/`start` (need list markers), `link`/`vlink` (need link pseudo-classes).

## Implementation note

These are best applied in box generation as **presentational hints**: resolve the
attribute to a property value and inject it at author-origin priority *below* the
real cascade, so an explicit CSS rule (or inline `style=`) always overrides the
attribute (per the spec). The existing per-element attribute reads in
`pkg/layout/css/build.go` (e.g. `attrSpan` for colspan) are the model.
