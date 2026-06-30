# Presentational HTML attribute hints — design (final pass)

**Goal:** Render legacy "presentational" HTML attributes — `bgcolor`, `text`, `<font
color>`, `width`/`height` on table parts, `align`, `valign`, `cellspacing`,
`cellpadding`, `border`, `bordercolor`, `hspace`/`vspace`, `nowrap`, `<ol type/start>` /
`<li value>`, `background`, `link`/`vlink`/`alink`, `align=left|right`→float — by
mapping each to its CSS property as a **presentational hint**: a cascade tier that sits
*above* the UA stylesheet but *below* all author CSS (HTML §15 "presentational hints"),
so an explicit CSS rule or inline `style=""` always wins.

This is the **final pass** of the HTML-rendering program: the four prerequisite engine
features it depended on (white-space, list markers + counters, background-image, link
pseudo-classes) have all landed, so every attribute now has a real CSS target. It is
**additive and byte-identical** for any document with no presentational attributes (the
hint tier contributes no declarations, so the cascade output is unchanged).

It is what unblocks real-world pages like Hacker News, which colors its layout with
`bgcolor` on a `<table>` (not CSS) and uses `align`/`valign` dozens of times.

## Background (what exists — see the recon)

- The cascade (`pkg/css/cascade.go`) has two origins: `OriginUA`, `OriginAuthor`. The
  resolver (`Compute`) collects matched declarations as `{decl, origin, spec, order}`,
  sorts by `normalRank(origin)` → specificity → source order (then a separate important
  pass), and applies. `normalRank`: UA=0, author=1. `Compute(n, parentStyle)` has the
  `Node` (so `n.Attr(...)` is available).
- Box generation (`pkg/layout/css/build.go generate`) computes each element's style via
  `r.Compute(c, cs)` and reads a few structural attributes (`attrSpan` for
  colspan/rowspan/span; `attrSnapshot`/`controlAttrSnapshot` for img/control). No
  generic attribute→property mapping exists.
- Every target `ComputedStyle` field already exists: `BackgroundColor`, `Color`,
  `Width`/`Height` (`Length`), `TextAlign`, `Float`, `VerticalAlign`, `BorderSpacingH/V`
  (`float64` points), `Padding*`/`Margin*` (`Length`), `BackgroundImage`,
  `Border*Width/Color/Style`, `WhiteSpace`, `ListStyleType`, and `CounterReset`/
  `CounterSet` (`[]CounterOp`) for `<ol start>`/`<li value>`.
- `Node.Attr(key)` (keys lowercased at parse) and `Node.Parent()` are available, so a
  cell can climb to its ancestor `<table>` to read `cellpadding`/`border`/`valign`.

## Architecture

Two pieces, both in `pkg/css` (so hints are part of the cascade, getting the origin
ordering right for free), plus a small wiring note:

1. **A presentational-hint cascade tier.** Add `OriginPresentationalHint` to the
   `Origin` enum, ranked between UA and author in the normal pass
   (`UA-normal < hint < author-normal`); hints never carry `!important`. In `Compute`,
   after gathering the UA/author matched declarations and before sorting, derive the
   element's presentational-hint declarations (from `n`'s attributes) and append them as
   `matched` entries with `origin: OriginPresentationalHint` and zero specificity. The
   single existing sort then orders everything correctly: a hint beats UA-normal, loses
   to any author-normal or author-`!important` rule and to inline `style`.
2. **Hint derivation** (`pkg/css/hints.go`, new): a pure function
   `presentationalHints(n Node) []Declaration` that switches on `n.Tag()` and reads the
   known attributes for that tag, producing `Declaration{Property, Value}` values in the
   *same string form the normal parser accepts* (so they flow through `applyDeclaration`
   unchanged — e.g. `bgcolor="#abc"` → `{Property:"background-color", Value:"#abc"}`,
   `width="50%"` → `{Property:"width", Value:"50%"}`, `align="center"` →
   `{Property:"text-align", Value:"center"}`). Table→cell propagation (cellpadding,
   border, valign on a row/group) is done by a `<td>`/`<th>` climbing `n.Parent()` to its
   nearest `<table>` and reading that table's attributes. This keeps everything in the
   per-node cascade with no downward push and no box-gen tree change.

The legacy color/length attribute syntaxes need a small tolerant parser (see below);
otherwise hints reuse the existing value parsers via the declaration round-trip.

## The hint map (per tag → declarations)

Resolved by `presentationalHints(n)`. Each row is "attribute on element → declaration(s)".

### Color
- `bgcolor` on `body/table/tr/td/th/col/colgroup` → `background-color: <color>`.
- `text` on `body` → `color: <color>`.
- `<font color=...>` → `color: <color>`; `<font face=...>` → `font-family: <face>`;
  `<font size=...>` → a coarse `font-size` (the legacy 1–7 / ±N scale mapped to px).
- `bordercolor` on `table/td/th/tr` → `border-color: <color>` (all four edges).
- `link`/`vlink`/`alink` on `body` → these style descendant links; modeled by emitting,
  for a `body` with `link`, hint declarations that the cascade can't target by selector
  (a hint is per-element). **Approach:** handle `link`/`vlink` specially — a `body[link]`
  contributes nothing to the body itself; instead, when an `<a href>` element resolves
  its hints, it climbs to the `body` and, if `body` has `link=`, emits `color: <link>`
  (and `vlink` is inert, like `:visited`). This keeps it in the per-node model.

Legacy color values: a 3/6-digit hex with **or without** `#`, or a named color. A
tolerant `parseLegacyColor` normalizes `bgcolor="cc0000"` → `#cc0000` before handing the
value to the color parser; an unrecognized value yields no declaration.

### Dimensions
- `width`/`height` on `table/td/th/col/colgroup` → `width`/`height: <len>`. A bare
  number is px (`width="120"` → `120px`); a `%` suffix is a percentage
  (`width="50%"` → `50%`). `<hr width>` likewise.
- `<img width/height>` already feed replaced sizing via `Replaced.Attrs`; **leave that
  path as the source of truth** to avoid double-application — do NOT also emit width/
  height hints for `<img>` (documented; replaced.go reads `Attrs["width"]`). Table-part
  width/height is the new coverage.

### Alignment
- `align` on `td/th/tr/col/p/div/h1–h6/caption` → `text-align: left|right|center|justify`
  (the keyword maps directly; `align="middle"`/other invalid → no declaration).
- `align=left|right` on `img/table` → `float: left|right` (NOT text-align — the dual
  meaning). `align=center` on `table` → centering via `margin-left/right: auto` (a hint
  pair) since a table is not text-aligned by its parent's `text-align` reliably; for
  `img` `align=center` is not a standard value (ignored).
- `valign` on `td/th` → `vertical-align: top|middle|bottom|baseline`. `valign` on
  `tr/thead/tbody/tfoot/col/colgroup` propagates to the cells (a `<td>` reads its
  ancestor row/section/table `valign` when its own is absent).
- `<center>` is an element, handled at box-gen/UA level (UA rule `center { display:block;
  text-align:center }`), not a hint — add the UA rule.

### Table chrome
- `cellspacing` on `table` → `border-spacing: <n>px` (sets `BorderSpacingH/V`). Read by
  the table layout already.
- `cellpadding` on `table` → `padding: <n>px` on **every cell**: a `<td>`/`<th>` reads
  its ancestor table's `cellpadding` and emits the padding hint.
- `border` on `table` → `border: <n>px outset` on the table AND `border: 1px inset` on
  every cell (HTML's rule: any non-zero `border` gives cells a 1px border). `border="0"`
  → no border (the common "suppress chrome" use). A `<td>`/`<th>` reads the ancestor
  table's `border` to decide its 1px cell border.
- `nowrap` on `td/th` → `white-space: nowrap`.
- `frame`/`rules` on `table` → **deferred** (which-edges-show is niche; logged).

### Image / inline spacing
- `hspace` on `img` → `margin-left`/`margin-right: <n>px`; `vspace` → `margin-top`/
  `margin-bottom: <n>px`.
- `border` on `img` → `border: <n>px solid` (the image's border color is its `color`).

### Body layout
- `background` on `body/td` → `background-image: url(<value>)` (now that
  `background-image` decodes + paints). The URL resolves through the same loader.
- `marginwidth`/`marginheight`/`topmargin`/`leftmargin` on `body` → body `margin`
  (`marginwidth`→left/right, `marginheight`→top/bottom; `leftmargin`/`topmargin` the IE
  spellings). **Deferred** unless trivial (low value; logged).

### Lists
- `<ol type=1|a|A|i|I>` / `<ul type=disc|circle|square>` → `list-style-type: <mapped>`
  (`1`→decimal, `a`→lower-alpha, `A`→upper-alpha, `i`→lower-roman, `I`→upper-roman).
  `<li type=...>` likewise on the item.
- `<ol start=N>` → `counter-reset: list-item <N-1>` (the UA sheet resets list-item to 0
  and each `<li>` increments by 1, so starting at N means reset to N-1).
- `<li value=N>` → `counter-set: list-item <N>` (sets the item's number; subsequent
  siblings continue from it — matching CSS, since the increment is applied before the
  marker reads the value... ordering verified in implementation).
- `<ol reversed>` → **deferred** (the counter walk only increments; reversed needs new
  machinery; logged).

## Error handling / degradation

- An attribute with an unparseable value (a bad color, a non-numeric width) yields **no
  declaration** for that attribute — the element renders as if the attribute were absent.
  No panic.
- An author CSS rule or inline `style` for the same property always wins (the hint tier
  is below author-normal). A hint always beats the UA default.
- Deferred attributes (`frame`/`rules`, body margins, `reversed`) are simply not mapped
  (no declaration), logged once where useful.
- Hints are derived only for elements that have at least one recognized attribute, so the
  common case (no presentational attributes) adds nothing to the cascade — byte-identical.

## Tests

- **Cascade precedence** (`pkg/css/hints_test.go` / `cascade_test.go`, via `fakeNode`
  with `attrs`): a `<td bgcolor=red>` gets `BackgroundColor` red; `<td bgcolor=red
  style="background-color:blue">` is blue (inline wins); an author `td{background-color:
  green}` rule beats the hint; the hint beats a UA `td{background-color:...}` rule. The
  full origin-interaction matrix mirrored from the existing origin tests.
- **Per-attribute derivation** (`hints_test.go`): each attribute → expected declaration,
  including legacy color (`bgcolor="cc0000"`→`#cc0000`), `width="50%"`→percent,
  `align=left` on `<img>`→`float:left` vs on `<td>`→`text-align:left`, `border="0"`→no
  cell border, `<ol type=a>`→`lower-alpha`, `<ol start=3>`→`counter-reset list-item 2`.
- **Table propagation** (`pkg/layout/css/build_test.go`): `<table cellpadding=8>` gives
  each `<td>` 8px padding; `<table border=1>` gives the table a border and each cell a
  1px border; `valign` on `<tr>` reaches its cells.
- **Box-gen integration** (`build_test.go`): a `<table bgcolor>` / `<td width>` build
  produces boxes with the mapped `Style.*`.
- **HTML goldens + WPT reftests + showcase**: a `html-presentational` golden (a legacy
  `bgcolor`/`align`/`cellpadding`/`border` table), a `bgcolor-vs-css` reftest (a
  `bgcolor` table == the same table styled with CSS `background-color`), and a
  "14 / LEGACY ATTRIBUTES" showcase section (a classic attribute-styled table, a
  `<font color>`, an `align=right` floated image, an `<ol type=I start=3>`). Regenerate
  the paginated goldens; eyeball.
- **The HN smoke check**: `rasterize https://news.ycombinator.com/` renders with its
  `#f6f6ef` background (manual, not a committed test — note in the PR).

## Out of scope (deferred, each renders as if absent)

`frame`/`rules` on tables; body margin attributes (`marginwidth`/`topmargin`/…);
`<ol reversed>`; `alink` (active-link, no interactivity); `<font size>` exact legacy
metric fidelity (a coarse map only); `<marquee>`/`<blink>` and other obsolete elements.
