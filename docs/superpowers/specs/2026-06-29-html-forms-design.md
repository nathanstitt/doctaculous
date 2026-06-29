# HTML rendering — static form controls

**Date:** 2026-06-29
**Status:** Design (approved, pre-implementation)
**Sub-project:** HTML-rendering roadmap — static form-control rendering

## Problem

The HTML engine renders no form controls. `<input>`, `<button>`, `<textarea>`,
`<select>` have no UA box rules (only `form { display:block }`) and no replaced
handling, so they default to `display:inline` with no box. The result: a form
collapses to a single line of leaked inline text — a `<button>`'s label and a
`<textarea>`'s / `<select><option>`'s text render as run-together prose, while
`<input>` (void) renders nothing. There is no field chrome, no sizing, no states.

This sub-project adds **static, non-interactive rendering** of the common form
controls: each control is sized like a browser would size it and painted with
classic native chrome (recessed fields, raised buttons, checkmarks, a dropdown
triangle). There is **no interactivity** — this is a faithful static snapshot of a
form, consistent with the toolkit's rasterization focus and the explicitly
out-of-scope "interactive AcroForm widget rendering" on the PDF side.

Guiding principle throughout: **match browser defaults** wherever a choice arises
(default sizes, UA styling, metrics, type fallbacks).

## Scope

In scope — these controls:

- `<input type=text>` and bare `<input>`, plus the text-like types
  `email`/`url`/`tel`/`search`/`number` (all rendered as a text field).
- `<input type=password>` (text field; value rendered as bullets).
- `<input type=checkbox>` and `<input type=radio>`.
- `<button>`, `<input type=submit>`, `<input type=button>`, `<input type=reset>`.
- `<textarea>`.
- `<select>` (shows the selected — or first — `<option>`; non-`multiple`).

States/attributes honored (the browser-default essentials):

- `checked` → checkbox checkmark, radio filled dot.
- `value` (text/submit inputs) and `<button>` label → rendered text.
- `placeholder` → shown gray when there is no `value`.
- `disabled` → muted (gray) chrome + text.
- `type` dispatch → selects the widget; unknown types fall back to a text field.

Out of scope (degrade gracefully, see Error Handling):

- All interactivity (focus, click, edit, dropdown expansion, scrolling).
- `type=file`, `type=image`, `type=color`, `type=range`, `type=date`/etc. — fall
  back to a text field (or, for `hidden`, generate no box).
- `multiple` on `<select>` (renders as a single-line select).
- `readonly`, `minlength`/`maxlength` visual hints, validation styling.
- Full CSS form-control theming where author `border`/`background` *replaces* the
  native chrome (we paint native chrome + allow author background/border to paint
  via the normal box path around it; see UA Stylesheet).
- Round radio buttons — no ellipse primitive exists in the paint layer, so radios
  render as a small square with a center dot (documented approximation).

## Approach

**Extend the existing `BoxReplaced` path** (the `<img>` model), rather than adding a
new box kind or doing UA-CSS only. A control becomes a replaced leaf, exactly like
an image: box generation marks it `BoxReplaced`, the engine computes an intrinsic
size (from a per-control table + measured font metrics instead of decoding an
image), and paint emits widget chrome (instead of an image) into the content box.

Why this approach: it reuses the entire replaced machinery for free — the CSS-size-
overrides-intrinsic rule, `min`/`max`/`box-sizing` clamping, inline-atom vs.
block-level flow, float/position, and participation in flex/grid/table layout (the
container-positioned-descendant fixes landed this session). The `<img>` path already
proves every one of those behaviors. The change is isolated to one new file
(`control.go`) plus small hooks in box generation, the fragment, and paint-emit.

Rejected alternatives: a dedicated `BoxControl` kind (duplicates replaced
sizing/flow and touches the BFC/flex/grid/table dispatch in many places, for the
same visual result — violates YAGNI); UA-stylesheet-only (cannot do checkmarks,
triangles, dots, placeholder-gray, disabled-muting, or character-count sizing, and
cannot suppress `<option>` leakage — fails the browser-faithful bar).

## Design

### 1. Box generation (controls → replaced leaves)

`pkg/layout/css/build.go` and `pkg/layout/cssbox/box.go`.

A `ControlKind` enum names the controls:

```go
type ControlKind int
const (
    CtrlNone ControlKind = iota // not a control (an image, or a non-replaced box)
    CtrlText        // text + text-like input types, and bare <input>
    CtrlPassword
    CtrlCheckbox
    CtrlRadio
    CtrlButton      // <button>, <input type=submit|button|reset>
    CtrlTextarea
    CtrlSelect
)
```

`classifyControl(tag, attrs) (ControlKind, skip bool)` maps an element to its kind:
`input` dispatches on `type` (default/unknown → `CtrlText`; `checkbox`/`radio`/
`password`/`submit`/`button`/`reset` → their kinds; `hidden` → `skip=true`;
`file`/`image` → `CtrlText`); `button`/`textarea`/`select` → their kinds.

`ReplacedContent` gains two fields (it stays a pure source-facts struct — no layout
state, consistent with its existing contract):

```go
type ReplacedContent struct {
    Tag   string
    Attrs map[string]string
    Control ControlKind // CtrlNone for <img> (the zero value), so images are unchanged
    Text    string      // display text: button label / textarea content / selected option
}
```

When `classifyControl` returns a control kind, box generation:

- marks the box `BoxReplaced` with `ReplacedContent{Tag, Attrs, Control, Text}`;
- extracts `Text` by walking descendant **text** nodes (button label, textarea
  content; for `<select>`, the selected `<option>`'s text, else the first
  option's) — non-text children are ignored;
- generates **no child boxes** (the control is a leaf) — this fixes the
  `<option>`/textarea text leakage.

`skip=true` (`type=hidden`) generates no box at all. `<img>` is unchanged
(`Control == CtrlNone`).

### 2. UA stylesheet defaults

`pkg/html/ua.go` gains browser-default control rules:

```css
input, textarea, select, button {
    display: inline-block;
    font-size: 13px;            /* ~ browser form-control default */
    line-height: normal;
}
textarea { vertical-align: text-bottom; }
input, select, button { vertical-align: baseline; }
```

- `display: inline-block` makes each control a replaced **inline atom** by default,
  so `<label> <input>` flows on one line. `display:block`/flex-item/grid-item still
  work via the cascade (replaced boxes already support block-level flow).
- **No borders/padding in the UA sheet.** The native chrome (sunken field border,
  raised button bevel, internal padding) is drawn by the widget paint routine, not
  via CSS borders — because controls are *replaced* elements whose appearance is
  intrinsic (an `<img>` doesn't get its pixels from CSS either). Author CSS
  `border`/`background`, if set, paints via the normal fragment box path
  around/behind the widget. Full CSS theming that *replaces* native chrome is out of
  scope.

### 3. Intrinsic sizing

New file `pkg/layout/css/control.go`. `controlIntrinsicSize(ctx, b) (w, h float64)`
parallels the image `intrinsicSize`; the engine's existing `replacedUsedSize` then
applies CSS `width`/`height` overrides, `min`/`max`, and `box-sizing` — so supplying
intrinsics is all that is needed, and the CSS-overrides rule falls out for free.

Character widths are measured from the control's **resolved font** (resolved
`font-family`/`font-size` via the face cache, using the `'0'` advance — the CSS `ch`
unit, the browser convention), reusing the same measurement path tables/inline use.

Per-control intrinsics (padded by chrome metrics `padX≈2pt`, `padY≈1pt`,
`border≈1pt`, unless noted):

- **Text / password:** `w = (size_attr | 20) × ch + 2·padX + 2·border`;
  `h = lineHeight + 2·padY + 2·border`.
- **Textarea:** `w = (cols | 20) × ch + …`; `h = (rows | 2) × lineHeight + …`.
- **Button:** shrink-to-fit `Text` + `padX≈6pt`; `h` as a text field.
- **Checkbox / radio:** fixed `13pt × 13pt` (browser default), font-independent.
- **Select:** `w = selectedOptionText × ch + triangleBox(≈16pt) + padding`; `h` as a
  text field.

**Non-zero-size invariant.** `controlIntrinsicSize` returns
`max(measured, perControlMinimum)` on **each axis**, with a per-control minimum
(text/select ≈ 120–150pt wide × one line tall; textarea ≈ 150×40pt; button a few ch
× one line; checkbox/radio inherently 13×13). So a degenerate measurement —
`size=0`, empty content, an unresolvable font, `<button></button>` — always yields
the standard default control size. The floor is part of the sizing function itself,
not a fallback branch.

The floor protects the **intrinsic default** only. An explicit author CSS `width:0`
/ `height:0` is a deliberate choice and still wins (browsers honor it), because the
floor is applied to the intrinsic value that `replacedUsedSize` consumes, not to an
explicit specified size.

### 4. Widget chrome painting

A `Fragment.Control *ControlContent` field (parallel to `Fragment.Image`) carries
paint inputs:

```go
type ControlContent struct {
    Kind        ControlKind
    Text        string
    Checked     bool
    Disabled    bool
    Placeholder bool        // Text is a placeholder → render gray
    Face        *font.Face
    FontSizePt  float64
    // box geometry is the Fragment's content-box X/Y/W/H
}
```

`appendSelfContent` (where `Fragment.Image` becomes an `ImageKind` item) gains a
parallel branch: `if f.Control != nil { dst = f.Control.append(dst, f) }`. The
control's `append` emits **existing** paint primitives — no new Item kinds:
`BackgroundKind` (fills), `BorderKind` (incl. the 3D `inset`/`outset` bevels),
`GlyphKind` (text), and `ClipPushKind`/`ClipPopKind` (overflow clipping).

Per-kind chrome (classic native):

- **Text / password / textarea / select:** white background + four `inset`
  (sunken) border edges → recessed field. Text painted as glyphs, clipped to the
  content box. Password → bullets (`•`). Placeholder → gray. Textarea → text wrapped
  across lines (reuse the inline breaker), top-aligned, clipped. Select → a small
  downward triangle (▼) in a right-side box, drawn as a ▼ glyph with a fallback to
  three short stacked `BackgroundKind` strokes when the glyph is unavailable
  (mirroring the checkmark's glyph-with-stroke-fallback).
- **Button / submit / reset:** light-gray background + four `outset` (raised) border
  edges → raised button. Label glyphs centered.
- **Checkbox:** small white `inset` box; if `Checked`, a checkmark (✓ glyph, falling
  back to two short `BackgroundKind` strokes if the glyph is unavailable).
- **Radio:** small `inset` box (square approximation — no ellipse primitive); if
  `Checked`, a small dark center `BackgroundKind` square (the dot).
- **Disabled** (any kind): muted palette — gray fill and gray text.

The square-radio approximation is the one non-faithful point; adding ellipse
primitives to the paint layer + raster backend is a larger cross-cutting change left
as a future enhancement.

### 5. Error handling & degradation

All within the engine's never-panic, degrade-gracefully contract (recover at the
page boundary; unsupported features skip + debug-log):

- Unknown/unsupported `type` (`color`, `range`, `date`, …) → `CtrlText` showing any
  `value` (browser fallback); logged once.
- `type=hidden` → no box. `type=file`/`image` → text-field fallback + debug log.
- Unresolvable font → the intrinsic floor still sizes the box; text simply is not
  painted (existing glyph-skip behavior). No panic.
- Malformed/empty content (`<select>` with no `<option>`, empty `<button>`, empty
  `<textarea>`) → empty chrome at the minimum size; never a zero box.
- Quirky markup (`<button>` containing `<img>`, `<select>` with non-`<option>`
  children) → text extraction takes descendant text only; the control is a leaf, so
  no box leakage.
- Overflowing text → clipped to the content box, not spilled.

### 6. Testing & showcase integration

- `pkg/layout/css/control_test.go` — `classifyControl` dispatch (every type → right
  kind; unknown → text; hidden → skip; file/image → text); `controlIntrinsicSize`
  (character-count widths scale with `size`/`cols`/`rows` and font-size; the
  non-zero floor invariant for `size=0` / empty content / unresolvable font /
  `<button></button>`; explicit CSS `width:0` still wins).
- `pkg/html` box-generation tests — controls become `BoxReplaced` leaves with the
  right `Control`/`Text`; children suppressed (no leakage); `type=hidden` → no box.
- A paint-level test — chrome emits the expected item kinds (checked checkbox →
  checkmark; button → `outset` borders; text field → `inset` borders + clipped
  text).
- Golden images (`pkg/doctaculous`) — a new small `html-forms-*` golden: a form with
  every control in both states (checked/unchecked, with/without value, placeholder,
  disabled, a `<select>`, a `<textarea>`). Generated via `-update`, eyeballed.
- A WPT-style `forms` reftest — controls vs. a reference built from plain styled
  boxes matching the chrome geometry, locking layout/positioning independent of
  exact pixels.
- Showcase — a new "09 / FORMS" section in `testdata/htmldoc/index.html` (+
  `main.css`) showing a realistic labeled form, exercising the feature through the
  full HTTP→pagination path. The `htmldoc-p*` goldens are regenerated and
  re-eyeballed; page count likely grows by one.

## Regression safety

Every page with no form controls stays **byte-identical**: controls are a new
`BoxReplaced` variant gated on `Control != CtrlNone`, so the `<img>` path and all
existing fixtures/goldens/reftests are untouched. The `render.Device` seam, the PDF
and DOCX pipelines, and the shared inline core are not modified — only the HTML
box-generation, replaced-sizing, fragment, and paint-emit paths change.

## Files

- `pkg/layout/cssbox/box.go` — `ControlKind`, extend `ReplacedContent`.
- `pkg/layout/css/build.go` — `classifyControl`, control-leaf box generation + text
  extraction.
- `pkg/html/ua.go` — UA control rules.
- `pkg/layout/css/control.go` (new) — `controlIntrinsicSize`, `ControlContent`, the
  chrome paint routine.
- `pkg/layout/css/fragment.go` — `Fragment.Control` field + `appendSelfContent`
  branch + translate handling.
- Tests as in §6, plus the showcase fixture updates.

## Future enhancements (not in this sub-project)

- Ellipse/rounded primitive in the paint layer + raster backend → round radios and
  `border-radius` generally.
- Full CSS form-control theming (author border/background replacing native chrome).
- `multiple` select (multi-line list box), `readonly`, validation styling.
- File/image/color/range/date widget chrome.
