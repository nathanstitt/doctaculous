# Static HTML Form Controls Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render the common HTML form controls (`<input>`, `<button>`, `<textarea>`, `<select>`) as sized, statically-painted native widgets, instead of leaking their content as inline text.

**Architecture:** Treat each control as a replaced leaf box (the `<img>` model). Box generation marks controls `BoxReplaced` with a `ControlKind` + extracted display text; the engine computes an intrinsic size from a per-control table + measured font metrics; and paint emits classic native chrome (recessed fields, raised buttons, checkmarks, dropdown triangle) using existing Background/Border/Glyph/Clip primitives. No interactivity.

**Tech Stack:** Pure Go. Packages: `pkg/layout/cssbox` (box model), `pkg/layout/css` (layout + paint-emit), `pkg/html` (UA stylesheet + parse), `pkg/doctaculous` (golden tests), `pkg/layout/paint` + `pkg/layout` (paint item kinds — unchanged here; we reuse them).

**Design doc:** `docs/superpowers/specs/2026-06-29-html-forms-design.md`

**Guiding principle:** match browser defaults wherever a choice arises.

---

## Background an implementer needs

- **Replaced boxes** are leaf boxes the engine sizes by intrinsics and paints itself (today only `<img>`). The whole flow/sizing machinery (CSS `width`/`height` override intrinsic, `min`/`max`/`box-sizing` clamp, inline-atom vs. block flow, float/position, flex/grid/table participation) already works for replaced boxes — we get it for free.
- Two engine chokepoints handle **all** replaced flow modes:
  - `(*Engine).replacedUsedSize(ctx, b, pctBasis) (w, h float64)` in `pkg/layout/css/replaced.go` — computes used content size; calls `intrinsicSize` for the intrinsic.
  - `(*Engine).replacedFragment(ctx, b, w, h, borderX, borderY, pctBasis) *Fragment` in `pkg/layout/css/replaced.go` — builds the fragment and sets `frag.Image`. Both `layoutBlockReplaced` (block) and the inline path (`inline.go`) call these, so hooking them covers everything.
- **Paint** flattens a fragment to `[]layout.Item`. `(*Fragment).appendSelfContent` (in `pkg/layout/css/fragment.go`) is where `frag.Image` becomes a `layout.ImageKind` item; we add a parallel `frag.Control` branch. `translateItems` (same file) shifts emitted items by a paint-time offset.
- **Paint item kinds** (in `pkg/layout/page.go`): `BackgroundKind` (filled rect via `Item.Rule`), `BorderKind` (one styled edge via `Item.Border`), `GlyphKind` (one glyph via `Item.Glyph`), `ClipPushKind`/`ClipPopKind` (clip rect via `Item.Rule`). `BorderStyle` includes `BorderInset`/`BorderOutset` (3D bevels). No new kinds are needed.
- **Font measurement:** `(*Engine).measureMaxContent(ctx, b)` measures a box's max-content width. For per-character widths we resolve the face and use its `'0'` advance; the engine holds a `*font.FaceCache` at `e.faces` (confirm the field name in Task 4).
- **Never panic** on malformed input; recover is at the page boundary. Unsupported cases skip + `e.logf(...)`.

## File structure

- `pkg/layout/cssbox/box.go` — add `ControlKind` enum; extend `ReplacedContent` with `Control ControlKind` and `Text string`. (Box model vocabulary.)
- `pkg/layout/css/control.go` (NEW) — `classifyControl`, `controlIntrinsicSize`, `ControlContent`, `controlText` extraction, and the chrome paint routine `(*ControlContent).append`. (All control-specific logic in one focused file.)
- `pkg/layout/css/build.go` — call `classifyControl` in `generate`; build control replaced leaves; extract text; suppress children; drop `type=hidden`.
- `pkg/html/ua.go` — UA control rules.
- `pkg/layout/css/replaced.go` — branch `replacedUsedSize` (intrinsic) and `replacedFragment` (set `frag.Control` instead of `frag.Image`) on `b.Replaced.Control`.
- `pkg/layout/css/fragment.go` — add `Fragment.Control *ControlContent`; emit it in `appendSelfContent`; shift it in `translateItems` (its emitted items are already shifted generically — verify).
- Tests: `pkg/layout/css/control_test.go`, additions to `pkg/html` box-gen tests, `pkg/doctaculous` golden + a WPT reftest, and the showcase fixture.

---

## Task 1: ControlKind enum + ReplacedContent fields

**Files:**
- Modify: `pkg/layout/cssbox/box.go`
- Test: `pkg/layout/cssbox/box_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/cssbox/box_test.go`:

```go
func TestReplacedContentCarriesControl(t *testing.T) {
	rc := &ReplacedContent{Tag: "input", Control: CtrlCheckbox, Text: ""}
	if rc.Control != CtrlCheckbox {
		t.Errorf("Control = %v, want CtrlCheckbox", rc.Control)
	}
	// The zero value is CtrlNone (an <img>), so existing replaced content is unchanged.
	img := &ReplacedContent{Tag: "img"}
	if img.Control != CtrlNone {
		t.Errorf("default Control = %v, want CtrlNone", img.Control)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/cssbox -run TestReplacedContentCarriesControl`
Expected: FAIL — `undefined: CtrlCheckbox` / `rc.Control undefined`.

- [ ] **Step 3: Add the enum and fields**

In `pkg/layout/cssbox/box.go`, add near the other kind enums (after `FloatKind`/`PositionKind` declarations):

```go
// ControlKind identifies a form control rendered as a static replaced widget.
// CtrlNone (the zero value) means the replaced box is not a control (e.g. an
// <img>), so existing replaced content is unaffected.
type ControlKind int

const (
	CtrlNone     ControlKind = iota // not a control
	CtrlText                        // text + text-like input types, and bare <input>
	CtrlPassword                    // <input type=password>
	CtrlCheckbox                    // <input type=checkbox>
	CtrlRadio                       // <input type=radio>
	CtrlButton                      // <button>, <input type=submit|button|reset>
	CtrlTextarea                    // <textarea>
	CtrlSelect                      // <select>
)
```

In the `ReplacedContent` struct, add the two fields:

```go
type ReplacedContent struct {
	Tag   string
	Attrs map[string]string
	// Control is the form-control kind for a control widget; CtrlNone for <img>.
	Control ControlKind
	// Text is the control's display text (button label, textarea content, or the
	// selected <option>'s text), extracted at box generation. Empty for <img>.
	Text string
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/cssbox -run TestReplacedContentCarriesControl`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/layout/cssbox/box.go pkg/layout/cssbox/box_test.go
git commit -m "feat(cssbox): ControlKind + ReplacedContent control fields"
```

---

## Task 2: classifyControl dispatch

**Files:**
- Create: `pkg/layout/css/control.go`
- Test: `pkg/layout/css/control_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/control_test.go`:

```go
package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func TestClassifyControl(t *testing.T) {
	cases := []struct {
		tag   string
		typ   string // "" = attribute absent
		want  cssbox.ControlKind
		skip  bool
	}{
		{"input", "", cssbox.CtrlText, false},       // bare input → text
		{"input", "text", cssbox.CtrlText, false},
		{"input", "email", cssbox.CtrlText, false},  // text-like
		{"input", "number", cssbox.CtrlText, false},
		{"input", "search", cssbox.CtrlText, false},
		{"input", "tel", cssbox.CtrlText, false},
		{"input", "url", cssbox.CtrlText, false},
		{"input", "password", cssbox.CtrlPassword, false},
		{"input", "checkbox", cssbox.CtrlCheckbox, false},
		{"input", "radio", cssbox.CtrlRadio, false},
		{"input", "submit", cssbox.CtrlButton, false},
		{"input", "button", cssbox.CtrlButton, false},
		{"input", "reset", cssbox.CtrlButton, false},
		{"input", "hidden", cssbox.CtrlNone, true},  // no box
		{"input", "file", cssbox.CtrlText, false},   // fallback to text field
		{"input", "image", cssbox.CtrlText, false},
		{"input", "color", cssbox.CtrlText, false},  // unknown → text
		{"input", "range", cssbox.CtrlText, false},
		{"button", "", cssbox.CtrlButton, false},
		{"textarea", "", cssbox.CtrlTextarea, false},
		{"select", "", cssbox.CtrlSelect, false},
		{"div", "", cssbox.CtrlNone, false},         // not a control
		{"img", "", cssbox.CtrlNone, false},
	}
	for _, c := range cases {
		attrs := map[string]string{}
		if c.typ != "" {
			attrs["type"] = c.typ
		}
		got, skip := classifyControl(c.tag, attrs)
		if got != c.want || skip != c.skip {
			t.Errorf("classifyControl(%q,type=%q) = (%v,%v), want (%v,%v)",
				c.tag, c.typ, got, skip, c.want, c.skip)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestClassifyControl`
Expected: FAIL — `undefined: classifyControl`.

- [ ] **Step 3: Write classifyControl**

Create `pkg/layout/css/control.go` with:

```go
package css

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// classifyControl maps an element (its lowercased tag + attributes) to the form
// control it renders as. It returns CtrlNone for non-control elements. skip is true
// only for <input type=hidden>, which generates no box at all (matching browsers).
// An unknown or unsupported input type falls back to CtrlText (the browser default),
// so no <input> is ever dropped; type=file/image also fall back to a text field.
func classifyControl(tag string, attrs map[string]string) (kind cssbox.ControlKind, skip bool) {
	switch tag {
	case "textarea":
		return cssbox.CtrlTextarea, false
	case "select":
		return cssbox.CtrlSelect, false
	case "button":
		return cssbox.CtrlButton, false
	case "input":
		switch strings.ToLower(strings.TrimSpace(attrs["type"])) {
		case "hidden":
			return cssbox.CtrlNone, true
		case "password":
			return cssbox.CtrlPassword, false
		case "checkbox":
			return cssbox.CtrlCheckbox, false
		case "radio":
			return cssbox.CtrlRadio, false
		case "submit", "button", "reset":
			return cssbox.CtrlButton, false
		default:
			// text, email, url, tel, search, number, file, image, color, range, date,
			// missing/unknown — all render as a text field.
			return cssbox.CtrlText, false
		}
	}
	return cssbox.CtrlNone, false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestClassifyControl`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/layout/css/control.go pkg/layout/css/control_test.go
git commit -m "feat(css): classifyControl form-control dispatch"
```

---

## Task 3: controlText extraction

**Files:**
- Modify: `pkg/layout/css/control.go`
- Test: `pkg/layout/css/control_test.go`

`controlText` extracts the display text for a control from a parsed HTML element. It walks descendant text. For `<select>` it returns the selected `<option>`'s text (else the first option's). This runs in box generation, so it takes an `*html.Element`.

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/control_test.go`:

```go
import (
	// ...existing imports...
	"github.com/nathanstitt/doctaculous/pkg/html"
)

// firstElement parses src and returns the first element matching tag (depth-first).
func firstElement(t *testing.T, src, tag string) *html.Element {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var find func(n *html.Element) *html.Element
	find = func(n *html.Element) *html.Element {
		if n == nil {
			return nil
		}
		if n.Tag() == tag {
			return n
		}
		for _, c := range n.Children() {
			if ce, ok := c.(*html.Element); ok {
				if r := find(ce); r != nil {
					return r
				}
			}
		}
		return nil
	}
	return find(doc.Root)
}

func TestControlText(t *testing.T) {
	if got := controlText(firstElement(t, `<button>Click Me</button>`, "button"), cssbox.CtrlButton); got != "Click Me" {
		t.Errorf("button text = %q, want %q", got, "Click Me")
	}
	if got := controlText(firstElement(t, "<textarea>line one</textarea>", "textarea"), cssbox.CtrlTextarea); got != "line one" {
		t.Errorf("textarea text = %q, want %q", got, "line one")
	}
	// select: selected option wins.
	sel := `<select><option>One</option><option selected>Two</option></select>`
	if got := controlText(firstElement(t, sel, "select"), cssbox.CtrlSelect); got != "Two" {
		t.Errorf("select text = %q, want %q (selected option)", got, "Two")
	}
	// select: no selected → first option.
	sel2 := `<select><option>Alpha</option><option>Beta</option></select>`
	if got := controlText(firstElement(t, sel2, "select"), cssbox.CtrlSelect); got != "Alpha" {
		t.Errorf("select text = %q, want %q (first option)", got, "Alpha")
	}
	// empty select → empty string, no panic.
	if got := controlText(firstElement(t, `<select></select>`, "select"), cssbox.CtrlSelect); got != "" {
		t.Errorf("empty select text = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestControlText`
Expected: FAIL — `undefined: controlText`.

- [ ] **Step 3: Implement controlText**

Add to `pkg/layout/css/control.go` (add `"github.com/nathanstitt/doctaculous/pkg/html"` to its imports):

```go
// controlText extracts a control's display text from its parsed element. For a
// <select> it returns the selected <option>'s text (the first option with a
// "selected" attribute), else the first option's text, else "". For button and
// textarea it returns the concatenated descendant text, trimmed. For input kinds it
// returns "" (their text comes from the value attribute, handled in box generation).
func controlText(e *html.Element, kind cssbox.ControlKind) string {
	if e == nil {
		return ""
	}
	switch kind {
	case cssbox.CtrlSelect:
		var first *html.Element
		for _, opt := range childElements(e, "option") {
			if first == nil {
				first = opt
			}
			if _, ok := opt.Attr("selected"); ok {
				return strings.TrimSpace(textOf(opt))
			}
		}
		if first != nil {
			return strings.TrimSpace(textOf(first))
		}
		return ""
	case cssbox.CtrlButton, cssbox.CtrlTextarea:
		return strings.TrimSpace(textOf(e))
	default:
		return ""
	}
}

// childElements returns e's direct child elements with the given tag.
func childElements(e *html.Element, tag string) []*html.Element {
	var out []*html.Element
	for _, c := range e.Children() {
		if ce, ok := c.(*html.Element); ok && ce.Tag() == tag {
			out = append(out, ce)
		}
	}
	return out
}

// textOf returns the concatenated text of e's descendant text nodes.
func textOf(e *html.Element) string {
	var b strings.Builder
	var walk func(n *html.Element)
	walk = func(n *html.Element) {
		for _, c := range n.Children() {
			switch cc := c.(type) {
			case *html.Text:
				b.WriteString(cc.Data) // Data is an exported FIELD, not a method
			case *html.Element:
				walk(cc)
			}
		}
	}
	walk(e)
	return b.String()
}
```

CONFIRMED API (verified against `pkg/html/dom.go`): `*html.Element` has `.Tag() string`, `.Attr(key) (string, bool)`, `.Children() []DOMNode`; the text node is `*html.Text` with an exported **field** `Data string` (no accessor). `Children()` returns `[]DOMNode`, so the type switch on `*html.Text` / `*html.Element` is correct.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestControlText`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/layout/css/control.go pkg/layout/css/control_test.go
git commit -m "feat(css): controlText extraction for button/textarea/select"
```

---

## Task 4: controlIntrinsicSize with the non-zero floor

**Files:**
- Modify: `pkg/layout/css/control.go`
- Test: `pkg/layout/css/control_test.go`

This computes the intrinsic `(w, h)` for a control box. It measures the resolved font's `'0'` advance for character widths and applies a per-control minimum on each axis. It takes the engine (for the face cache) and the `*cssbox.Box`.

First confirm the engine's face-cache field name and the Face API:

```
grep -rn "faces\b\|FaceCache\|func New(" pkg/layout/css/*.go | grep -v _test | head
grep -rn "func (.*Face) Glyph\|func (.*Face) Metrics\|advanceEm\|Advance" pkg/font/*.go | head
```

CONFIRMED API (verified): the engine field is `e.faces` (a `*layoutfont.FaceCache`, where `layoutfont` aliases `pkg/layout/font`); resolve with `e.faces.Resolve(family string, style pkgfont.Style) (*pkgfont.Face, bool)` (here `pkgfont`/`font` is `pkg/font`). The `'0'` advance is `face.Glyph('0') (outline *render.Path, advanceEm float64, ok bool)`; multiply `advanceEm * fontSizePt` for points. `face.Metrics() (ascent, descent, lineGap float64)`. `New(faces, loader, logf)` accepts a nil `faces` (builds a fresh cache via `NewFaceCache()`).

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/control_test.go`:

```go
import (
	"context"
	gcss "github.com/nathanstitt/doctaculous/pkg/css"
)

// ctrlBox builds a minimal replaced control box with the given kind, attrs, and a
// default 13px font (the UA control default), for sizing tests.
func ctrlBox(kind cssbox.ControlKind, attrs map[string]string) *cssbox.Box {
	st := gcss.ComputedStyle{
		FontFamily: "sans-serif",
		FontSizePt: 13,
		Width:      gcss.Length{Unit: gcss.UnitAuto},
		Height:     gcss.Length{Unit: gcss.UnitAuto},
		MaxWidth:   gcss.Length{Unit: gcss.UnitAuto},
		MaxHeight:  gcss.Length{Unit: gcss.UnitAuto},
	}
	return &cssbox.Box{
		Kind:     cssbox.BoxReplaced,
		Display:  cssbox.DisplayInlineBlock,
		Style:    st,
		Replaced: &cssbox.ReplacedContent{Tag: "input", Control: kind, Attrs: attrs},
	}
}

func TestControlIntrinsicSizeNonZero(t *testing.T) {
	eng := New(newTestFaceCache(t), nil, nil)
	ctx := context.Background()
	cases := []struct {
		name string
		box  *cssbox.Box
	}{
		{"text-size0", ctrlBox(cssbox.CtrlText, map[string]string{"size": "0"})},
		{"text-bare", ctrlBox(cssbox.CtrlText, nil)},
		{"button-empty", func() *cssbox.Box {
			b := ctrlBox(cssbox.CtrlButton, nil)
			b.Replaced.Text = ""
			return b
		}()},
		{"textarea-bare", ctrlBox(cssbox.CtrlTextarea, nil)},
		{"checkbox", ctrlBox(cssbox.CtrlCheckbox, nil)},
		{"select-empty", ctrlBox(cssbox.CtrlSelect, nil)},
	}
	for _, c := range cases {
		w, h := eng.controlIntrinsicSize(ctx, c.box)
		if w <= 0 || h <= 0 {
			t.Errorf("%s: intrinsic size = (%.1f, %.1f), want both > 0", c.name, w, h)
		}
	}
}

func TestControlIntrinsicSizeScalesWithChars(t *testing.T) {
	eng := New(newTestFaceCache(t), nil, nil)
	ctx := context.Background()
	narrow := eng.widthOf(ctx, ctrlBox(cssbox.CtrlText, map[string]string{"size": "5"}))
	wide := eng.widthOf(ctx, ctrlBox(cssbox.CtrlText, map[string]string{"size": "40"}))
	if !(wide > narrow) {
		t.Errorf("size=40 width %.1f should exceed size=5 width %.1f", wide, narrow)
	}
}
```

Add this tiny helper to `control_test.go` so the char-scaling test reads only the width:

```go
func (e *Engine) widthOf(ctx context.Context, b *cssbox.Box) float64 {
	w, _ := e.controlIntrinsicSize(ctx, b)
	return w
}
```

For `newTestFaceCache`, reuse the existing test helper if one exists; otherwise add:

```go
import lfont "github.com/nathanstitt/doctaculous/pkg/layout/font"

func newTestFaceCache(t *testing.T) *lfont.FaceCache {
	t.Helper()
	return lfont.NewFaceCache() // resolves bundled base-14 (sans-serif → Heros)
}
```

(If `New`'s first parameter type differs, match it — confirm with `grep -n "func New(" pkg/layout/css/*.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestControlIntrinsicSize`
Expected: FAIL — `undefined: controlIntrinsicSize` (and `widthOf`).

- [ ] **Step 3: Implement controlIntrinsicSize**

Add to `pkg/layout/css/control.go` (add imports `"context"`, `gcss "github.com/nathanstitt/doctaculous/pkg/css"`, `"github.com/nathanstitt/doctaculous/pkg/font"`):

```go
// Control chrome metrics, in points (browser-typical defaults).
const (
	ctrlPadX       = 2  // text field internal horizontal padding (each side)
	ctrlPadY       = 1  // text field internal vertical padding (each side)
	ctrlBtnPadX    = 6  // button internal horizontal padding (each side)
	ctrlBorder     = 1  // chrome border thickness (each side)
	ctrlCheckSize  = 13 // checkbox/radio fixed square side
	ctrlSelectTri  = 16 // select dropdown-triangle box width
	// Per-control minimum intrinsic sizes (the non-zero floor).
	ctrlMinTextW   = 120
	ctrlMinTextareaW = 150
	ctrlMinTextareaH = 40
	ctrlMinButtonW = 24
)

// controlIntrinsicSize returns the intrinsic content-box size (points) of a form
// control, measured from its resolved font and floored to a per-control minimum so
// it is NEVER zero on either axis (a degenerate measurement — size=0, empty
// content, an unresolvable font — still yields the standard default control size).
// The engine's replacedUsedSize applies CSS width/height overrides and min/max on
// top, so an explicit author width/height still wins.
func (e *Engine) controlIntrinsicSize(ctx context.Context, b *cssbox.Box) (w, h float64) {
	kind := cssbox.CtrlNone
	if b.Replaced != nil {
		kind = b.Replaced.Control
	}
	fs := b.Style.FontSizePt
	ch := e.charWidth(b)              // one '0' advance in points
	line := e.controlLineHeight(b)   // one line box height in points

	switch kind {
	case cssbox.CtrlCheckbox, cssbox.CtrlRadio:
		return ctrlCheckSize, ctrlCheckSize
	case cssbox.CtrlButton:
		labelW := e.textWidth(ctx, b, b.Replaced.Text)
		w = labelW + 2*ctrlBtnPadX + 2*ctrlBorder
		h = line + 2*ctrlPadY + 2*ctrlBorder
		return max2(w, ctrlMinButtonW), h
	case cssbox.CtrlTextarea:
		cols := attrIntOr(b, "cols", 20)
		rows := attrIntOr(b, "rows", 2)
		w = float64(cols)*ch + 2*ctrlPadX + 2*ctrlBorder
		h = float64(rows)*line + 2*ctrlPadY + 2*ctrlBorder
		return max2(w, ctrlMinTextareaW), max2(h, ctrlMinTextareaH)
	case cssbox.CtrlSelect:
		textW := e.textWidth(ctx, b, b.Replaced.Text)
		w = textW + ctrlSelectTri + 2*ctrlPadX + 2*ctrlBorder
		h = line + 2*ctrlPadY + 2*ctrlBorder
		return max2(w, ctrlMinTextW), h
	default: // CtrlText, CtrlPassword
		size := attrIntOr(b, "size", 20)
		w = float64(size)*ch + 2*ctrlPadX + 2*ctrlBorder
		h = line + 2*ctrlPadY + 2*ctrlBorder
		return max2(w, ctrlMinTextW), h
	}
	_ = fs
}

// charWidth returns the width of one '0' in the control's resolved font (the CSS ch
// unit), in points. Falls back to 0.5em when the face or glyph is unavailable, so a
// width is always produced.
func (e *Engine) charWidth(b *cssbox.Box) float64 {
	fs := b.Style.FontSizePt
	face, ok := e.faces.Resolve(b.Style.FontFamily, styleFor(b))
	if ok && face != nil {
		if _, adv, ok := face.Glyph('0'); ok && adv > 0 {
			return adv * fs
		}
	}
	return 0.5 * fs
}

// controlLineHeight returns one line box height (points) for the control's font.
func (e *Engine) controlLineHeight(b *cssbox.Box) float64 {
	fs := b.Style.FontSizePt
	face, ok := e.faces.Resolve(b.Style.FontFamily, styleFor(b))
	if ok && face != nil {
		asc, desc, _ := face.Metrics()
		if asc+desc > 0 {
			return (asc + desc) * 1.15 * fs
		}
	}
	return 1.2 * fs
}

// textWidth measures the width (points) of s in the control's resolved font.
func (e *Engine) textWidth(ctx context.Context, b *cssbox.Box, s string) float64 {
	fs := b.Style.FontSizePt
	face, ok := e.faces.Resolve(b.Style.FontFamily, styleFor(b))
	if !ok || face == nil {
		return float64(len([]rune(s))) * 0.5 * fs
	}
	total := 0.0
	for _, r := range s {
		if _, adv, ok := face.Glyph(r); ok {
			total += adv * fs
		}
	}
	return total
}

func attrIntOr(b *cssbox.Box, key string, def int) int {
	if b.Replaced == nil {
		return def
	}
	if v, ok := b.Replaced.Attrs[key]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func max2(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
```

Add `"strconv"` to the imports. `styleFor(b)` must map the box's bold/italic to `font.Style`; if such a helper already exists in the package use it, otherwise add:

```go
// styleFor maps a box's computed weight/slant to a font.Style.
func styleFor(b *cssbox.Box) font.Style {
	return font.Style{Bold: b.Style.Bold, Italic: b.Style.Italic}
}
```

(Confirm `ComputedStyle` has `Bold`/`Italic` bools — it does, per `pkg/css/cascade.go`. Confirm `e.faces` and `face.Glyph`/`face.Metrics` names from the Step-2 greps and adjust.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/layout/css -run TestControlIntrinsicSize`
Expected: PASS (both the non-zero and char-scaling tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/layout/css/control.go pkg/layout/css/control_test.go
git commit -m "feat(css): controlIntrinsicSize with non-zero floor"
```

---

## Task 5: Box generation — control replaced leaves

**Files:**
- Modify: `pkg/layout/css/build.go`
- Test: `pkg/layout/css/control_test.go` (box-gen assertions)

Hook `classifyControl` into `generate`, before the existing `replacedTags["img"]` check, so controls become replaced leaves with `Control`/`Text` set, children suppressed, and `type=hidden` dropped. Also snapshot the control attributes (`type`, `value`, `placeholder`, `checked`, `disabled`, `size`, `cols`, `rows`, plus the existing `width`/`height`).

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/control_test.go`:

```go
// buildBox parses src, builds the cssbox tree, and returns the first BoxReplaced
// whose Replaced.Control != CtrlNone (depth-first), or nil.
func buildControlBox(t *testing.T, src string) *cssbox.Box {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var find func(b *cssbox.Box) *cssbox.Box
	find = func(b *cssbox.Box) *cssbox.Box {
		if b == nil {
			return nil
		}
		if b.Kind == cssbox.BoxReplaced && b.Replaced != nil && b.Replaced.Control != cssbox.CtrlNone {
			return b
		}
		for _, c := range b.Children {
			if r := find(c); r != nil {
				return r
			}
		}
		return nil
	}
	return find(root)
}

func TestBuildControlBoxes(t *testing.T) {
	// A checkbox becomes a replaced leaf with no children.
	cb := buildControlBox(t, `<body><input type=checkbox checked></body>`)
	if cb == nil || cb.Replaced.Control != cssbox.CtrlCheckbox {
		t.Fatalf("checkbox not generated as a control replaced box")
	}
	if len(cb.Children) != 0 {
		t.Errorf("control box has %d children, want 0 (leaf)", len(cb.Children))
	}
	if _, ok := cb.Replaced.Attrs["checked"]; !ok {
		t.Errorf("checked attribute not snapshotted")
	}
	// A button carries its label and generates no child boxes (no leakage).
	bt := buildControlBox(t, `<body><button>Go</button></body>`)
	if bt == nil || bt.Replaced.Text != "Go" || len(bt.Children) != 0 {
		t.Errorf("button box = %+v, want Text=Go and no children", bt)
	}
	// A select carries the selected option text and no children.
	sl := buildControlBox(t, `<body><select><option>A</option><option selected>B</option></select></body>`)
	if sl == nil || sl.Replaced.Text != "B" || len(sl.Children) != 0 {
		t.Errorf("select box = %+v, want Text=B and no children", sl)
	}
	// type=hidden generates no box at all.
	hid := buildControlBox(t, `<body><input type=hidden value=x></body>`)
	if hid != nil {
		t.Errorf("hidden input generated a box, want none")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestBuildControlBoxes`
Expected: FAIL — control becomes a normal inline box (text leaks); `cb` may be nil or have children.

- [ ] **Step 3: Wire box generation**

In `pkg/layout/css/build.go`, in `generate`, add BEFORE the `if replacedTags[e.Tag()]` block:

```go
	if kind, skip := classifyControl(e.Tag(), elemAttrs(e)); kind != cssbox.CtrlNone || skip {
		if skip {
			return nil // <input type=hidden>: no box
		}
		b.Kind = cssbox.BoxReplaced
		b.Replaced = &cssbox.ReplacedContent{
			Tag:     e.Tag(),
			Attrs:   controlAttrSnapshot(e),
			Control: kind,
			Text:    controlText(e, kind),
		}
		return b // controls are leaves — no child boxes (prevents text leakage)
	}
```

`classifyControl` takes a `map[string]string`; add a tiny `elemAttrs(e)` that returns the attrs it needs (only `type`):

```go
// elemAttrs returns the attributes classifyControl consults (currently just type).
func elemAttrs(e *html.Element) map[string]string {
	m := map[string]string{}
	if v, ok := e.Attr("type"); ok {
		m["type"] = v
	}
	return m
}
```

Add a control attribute snapshot (superset of `attrSnapshot`, in `build.go`):

```go
// controlAttrSnapshot copies the attributes a form control's sizing/paint consults.
func controlAttrSnapshot(e *html.Element) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"type", "value", "placeholder", "checked", "disabled",
		"size", "cols", "rows", "width", "height"} {
		if v, ok := e.Attr(k); ok {
			out[k] = v
		}
	}
	return out
}
```

CONFIRMED (verified against `pkg/html/dom.go`): `Element.Attr(key) (string, bool)` returns `(value, mapPresence)`, so a valueless boolean attribute like `checked`/`disabled` reports `ok=true` (the parser stores it with an empty value). The presence checks (`_, ok := ...Attrs["checked"]` in paint, `e.Attr("checked")` here) are correct — no `HasAttr` is needed.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestBuildControlBoxes`
Expected: PASS.

- [ ] **Step 5: Run the full css + html + doctaculous suites (no regressions yet, controls not painted)**

Run: `go test ./pkg/layout/css ./pkg/html ./pkg/doctaculous`
Expected: PASS. Controls now reserve a (default-min) box but paint nothing yet — existing goldens unaffected (no fixture has form controls).

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/build.go pkg/layout/css/control.go pkg/layout/css/control_test.go
git commit -m "feat(css): generate form controls as replaced leaves"
```

---

## Task 6: UA stylesheet defaults

**Files:**
- Modify: `pkg/html/ua.go`
- Test: `pkg/layout/css/control_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/control_test.go`:

```go
func TestControlUADefaultsInlineBlock(t *testing.T) {
	b := buildControlBox(t, `<body><input type=text></body>`)
	if b == nil {
		t.Fatal("no control box")
	}
	if b.Display != cssbox.DisplayInlineBlock {
		t.Errorf("input Display = %v, want DisplayInlineBlock (UA default)", b.Display)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestControlUADefaultsInlineBlock`
Expected: FAIL — without UA rules the control defaults to inline (`DisplayInline`).

- [ ] **Step 3: Add UA rules**

In `pkg/html/ua.go`, append to the `uaSource` string (before the closing backtick):

```css
input, textarea, select, button {
	display: inline-block;
	font-size: 13px;
	line-height: normal;
}
textarea { vertical-align: text-bottom; }
input, select, button { vertical-align: baseline; }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestControlUADefaultsInlineBlock`
Expected: PASS.

- [ ] **Step 5: Run the full suite (UA change is global — verify no existing golden shifts)**

Run: `go test ./pkg/doctaculous ./pkg/html ./pkg/layout/...`
Expected: PASS. The new UA rules only match `input`/`textarea`/`select`/`button`, none of which appear in existing fixtures, so goldens are unchanged.

- [ ] **Step 6: Commit**

```bash
git add pkg/html/ua.go pkg/layout/css/control_test.go
git commit -m "feat(html): UA stylesheet defaults for form controls"
```

---

## Task 7: Sizing hook — branch replacedUsedSize to controls

**Files:**
- Modify: `pkg/layout/css/replaced.go`
- Test: `pkg/layout/css/control_test.go`

Make the engine use `controlIntrinsicSize` for a control box (so a control with no CSS width gets its character-count default), while CSS `width`/`height` still override. The cleanest hook: in `replacedUsedSize`, when `b.Replaced.Control != CtrlNone`, get the intrinsic from `controlIntrinsicSize` instead of `intrinsicSize` (the image one). Mirror the existing structure.

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/control_test.go`:

```go
func TestControlUsedSizeDefaultsAndOverride(t *testing.T) {
	eng := New(newTestFaceCache(t), nil, nil)
	ctx := context.Background()

	// No CSS width → character-count default (well above zero).
	def := ctrlBox(cssbox.CtrlText, map[string]string{"size": "10"})
	w, h := eng.replacedUsedSize(ctx, def, 1000)
	if w < ctrlMinTextW-1 || h <= 0 {
		t.Errorf("default text size = (%.1f,%.1f), want char-count width and >0 height", w, h)
	}

	// Explicit CSS width:50px overrides the intrinsic default.
	over := ctrlBox(cssbox.CtrlText, map[string]string{"size": "10"})
	over.Style.Width = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	w2, _ := eng.replacedUsedSize(ctx, over, 1000)
	if w2 != 50 {
		t.Errorf("CSS-width override = %.1f, want 50", w2)
	}

	// Explicit width:0 is honored (deliberate), NOT floored.
	zero := ctrlBox(cssbox.CtrlText, nil)
	zero.Style.Width = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	w3, _ := eng.replacedUsedSize(ctx, zero, 1000)
	if w3 != 0 {
		t.Errorf("explicit width:0 = %.1f, want 0 (author override wins)", w3)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestControlUsedSizeDefaultsAndOverride`
Expected: FAIL — `replacedUsedSize` currently calls the image `intrinsicSize`, which returns `ok=false` for a control (no `src`), so the default width collapses toward 0.

- [ ] **Step 3: Branch replacedUsedSize**

In `pkg/layout/css/replaced.go`, in `replacedUsedSize`, replace the line:

```go
	iw, ih, haveIntrinsic := e.intrinsicSize(ctx, b)
```

with:

```go
	var iw, ih float64
	var haveIntrinsic bool
	if b.Replaced != nil && b.Replaced.Control != cssbox.CtrlNone {
		iw, ih = e.controlIntrinsicSize(ctx, b)
		haveIntrinsic = true
	} else {
		iw, ih, haveIntrinsic = e.intrinsicSize(ctx, b)
	}
```

This makes the control intrinsic feed the same `hasW`/`hasH` switch that already gives CSS width/height priority and treats `width:0` as a specified `0` (so the floor — which lives in `controlIntrinsicSize`, on the intrinsic only — does not override an explicit `0`).

Confirm `cssbox` is imported in `replaced.go` (it is — `b *cssbox.Box`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestControlUsedSizeDefaultsAndOverride`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/layout/css/replaced.go pkg/layout/css/control_test.go
git commit -m "feat(css): size form controls via controlIntrinsicSize"
```

---

## Task 8: Paint — ControlContent fragment + chrome

**Files:**
- Modify: `pkg/layout/css/fragment.go` (add `Fragment.Control` + emit), `pkg/layout/css/replaced.go` (set `frag.Control`), `pkg/layout/css/control.go` (the `ControlContent` type + `append`).
- Test: `pkg/layout/css/control_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/control_test.go` (uses the `layout` package for item kinds and a small render helper):

```go
import "github.com/nathanstitt/doctaculous/pkg/layout"

// renderControlItems lays out a single control and returns its flattened paint items.
func renderControlItems(t *testing.T, src string) []layout.Item {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	eng := New(newTestFaceCache(t), nil, nil)
	frag := eng.layoutTree(context.Background(), root, 400)
	var items []layout.Item
	if frag != nil {
		items = frag.AppendItems(items)
	}
	return items
}

func countKind(items []layout.Item, k layout.ItemKind) int {
	n := 0
	for _, it := range items {
		if it.Kind == k {
			n++
		}
	}
	return n
}

func hasInsetBorder(items []layout.Item) bool {
	for _, it := range items {
		if it.Kind == layout.BorderKind && it.Border.Style == layout.BorderInset {
			return true
		}
	}
	return false
}

func hasOutsetBorder(items []layout.Item) bool {
	for _, it := range items {
		if it.Kind == layout.BorderKind && it.Border.Style == layout.BorderOutset {
			return true
		}
	}
	return false
}

func TestControlPaintChrome(t *testing.T) {
	// Text field: a background fill + inset (sunken) borders.
	tf := renderControlItems(t, `<body><input type=text value=hi></body>`)
	if countKind(tf, layout.BackgroundKind) == 0 || !hasInsetBorder(tf) {
		t.Errorf("text field: want a background + inset borders; got bg=%d inset=%v",
			countKind(tf, layout.BackgroundKind), hasInsetBorder(tf))
	}
	// Button: outset (raised) borders.
	bt := renderControlItems(t, `<body><button>Go</button></body>`)
	if !hasOutsetBorder(bt) {
		t.Errorf("button: want outset borders")
	}
	// Checked checkbox: at least one glyph (the checkmark) OR extra background strokes.
	cbChecked := renderControlItems(t, `<body><input type=checkbox checked></body>`)
	cbEmpty := renderControlItems(t, `<body><input type=checkbox></body>`)
	if !(countKind(cbChecked, layout.GlyphKind)+countKind(cbChecked, layout.BackgroundKind) >
		countKind(cbEmpty, layout.GlyphKind)+countKind(cbEmpty, layout.BackgroundKind)) {
		t.Errorf("checked checkbox should paint more than an empty one (the checkmark)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestControlPaintChrome`
Expected: FAIL — controls reserve a box but paint no chrome yet (no inset/outset borders, no checkmark).

- [ ] **Step 3a: Add the `Fragment.Control` field + paint-emit**

In `pkg/layout/css/fragment.go`, add a field next to `Image`:

```go
	Control    *ControlContent // form-control widget (set for a control replaced box), painted in the content box
```

In `appendSelfContent`, after the existing `if f.Image != nil && f.Image.Img != nil { ... }` block, add:

```go
	if f.Control != nil {
		dst = f.Control.append(dst, f)
	}
```

`translateItems` already shifts every emitted Background/Border/Glyph/Clip item generically (it switches on item kind, not on source), so a `Control`'s emitted items ride a paint-time offset with no extra change. Verify by reading `translateItems` — no edit expected.

- [ ] **Step 3b: Set `frag.Control` in replacedFragment**

In `pkg/layout/css/replaced.go`, in `replacedFragment`, replace the unconditional `Image: &ImageContent{...}` construction so a control gets a `Control` instead. Change:

```go
	img := decodedImageFor(ctx, e, b)
	frag := &Fragment{
		X: borderX, Y: borderY, W: borderW, H: borderH,
		Background: b.Style.BackgroundColor,
		Image: &ImageContent{
			Img: img,
			CX:  contentX, CY: contentY, CW: w, CH: h,
			Fit:  mapObjectFit(b.Style.ObjectFit),
			PosX: b.Style.ObjectPositionX, PosY: b.Style.ObjectPositionY,
		},
		DebugTag: debugTag(b),
	}
```

to:

```go
	frag := &Fragment{
		X: borderX, Y: borderY, W: borderW, H: borderH,
		Background: b.Style.BackgroundColor,
		DebugTag:   debugTag(b),
	}
	if b.Replaced != nil && b.Replaced.Control != cssbox.CtrlNone {
		frag.Control = e.controlContentFor(b, contentX, contentY, w, h)
	} else {
		img := decodedImageFor(ctx, e, b)
		frag.Image = &ImageContent{
			Img: img,
			CX:  contentX, CY: contentY, CW: w, CH: h,
			Fit:  mapObjectFit(b.Style.ObjectFit),
			PosX: b.Style.ObjectPositionX, PosY: b.Style.ObjectPositionY,
		}
	}
```

- [ ] **Step 3c: ControlContent + chrome paint in control.go**

Add to `pkg/layout/css/control.go` (imports: `"image/color"`, `"github.com/nathanstitt/doctaculous/pkg/layout"`, and the `font` import already present):

```go
// ControlContent is a form control's paint payload carried on a Fragment, painted
// in the content box (CX,CY,CW,CH, page space, shifting with the fragment).
type ControlContent struct {
	Kind        cssbox.ControlKind
	Text        string
	Placeholder bool
	Checked     bool
	Disabled    bool
	Face        *font.Face
	FontSizePt  float64
	CX, CY, CW, CH float64
}

// classic-native chrome colors.
var (
	ctrlFieldBG   = color.RGBA{0xff, 0xff, 0xff, 0xff}
	ctrlButtonBG  = color.RGBA{0xdd, 0xdd, 0xdd, 0xff}
	ctrlBevelLite = color.RGBA{0xf0, 0xf0, 0xf0, 0xff}
	ctrlBevelDark = color.RGBA{0x80, 0x80, 0x80, 0xff}
	ctrlText      = color.RGBA{0x10, 0x10, 0x10, 0xff}
	ctrlGray      = color.RGBA{0x99, 0x99, 0x99, 0xff}
	ctrlDisabled  = color.RGBA{0xee, 0xee, 0xee, 0xff}
)

// controlContentFor builds the paint payload for control box b. ctx is unused here
// (the face is resolved synchronously) but kept for symmetry with the image path.
func (e *Engine) controlContentFor(b *cssbox.Box, cx, cy, w, h float64) *ControlContent {
	face, _ := e.faces.Resolve(b.Style.FontFamily, styleFor(b))
	cc := &ControlContent{
		Kind:       b.Replaced.Control,
		Face:       face,
		FontSizePt: b.Style.FontSizePt,
		CX:         cx, CY: cy, CW: w, CH: h,
	}
	_, cc.Checked = b.Replaced.Attrs["checked"]
	_, cc.Disabled = b.Replaced.Attrs["disabled"]
	// Display text: the extracted Text (button/textarea/select) or the value (inputs);
	// fall back to placeholder (gray) when an input has no value.
	switch cc.Kind {
	case cssbox.CtrlButton, cssbox.CtrlTextarea, cssbox.CtrlSelect:
		cc.Text = b.Replaced.Text
	default:
		if v, ok := b.Replaced.Attrs["value"]; ok && v != "" {
			cc.Text = v
		} else if p, ok := b.Replaced.Attrs["placeholder"]; ok {
			cc.Text, cc.Placeholder = p, true
		}
	}
	if cc.Kind == cssbox.CtrlPassword && !cc.Placeholder {
		cc.Text = strings.Repeat("•", len([]rune(cc.Text)))
	}
	return cc
}

// append emits the control's chrome + text as paint items into dst. It uses only
// existing item kinds; the fragment f supplies nothing beyond what cc carries.
func (cc *ControlContent) append(dst []layout.Item, f *Fragment) []layout.Item {
	fill := func(x, y, w, h float64, c color.RGBA) {
		dst = append(dst, layout.Item{Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: x, YPt: y, WPt: w, HPt: h, Color: c}})
	}
	bevel := func(style layout.BorderStyle) {
		for _, s := range [...]layout.EdgeSide{layout.EdgeTop, layout.EdgeRight, layout.EdgeBottom, layout.EdgeLeft} {
			dst = append(dst, layout.Item{Kind: layout.BorderKind, Border: cc.edge(s, style)})
		}
	}
	switch cc.Kind {
	case cssbox.CtrlCheckbox, cssbox.CtrlRadio:
		bg := ctrlFieldBG
		if cc.Disabled {
			bg = ctrlDisabled
		}
		fill(cc.CX, cc.CY, cc.CW, cc.CH, bg)
		bevel(layout.BorderInset)
		if cc.Checked {
			ink := ctrlText
			if cc.Disabled {
				ink = ctrlGray
			}
			if cc.Kind == cssbox.CtrlRadio {
				// Center dot (square approximation — no ellipse primitive).
				d := cc.CW * 0.4
				fill(cc.CX+(cc.CW-d)/2, cc.CY+(cc.CH-d)/2, d, d, ink)
			} else {
				// Checkmark: prefer a ✓ glyph, else two strokes.
				if !cc.appendGlyphCentered(&dst, '✓', ink) {
					// fallback strokes (a simple X-free tick approximation)
					t := cc.CW * 0.12
					fill(cc.CX+cc.CW*0.2, cc.CY+cc.CH*0.5, cc.CW*0.25, t, ink)
					fill(cc.CX+cc.CW*0.4, cc.CY+cc.CH*0.3, t, cc.CH*0.4, ink)
				}
			}
		}
		return dst
	case cssbox.CtrlButton:
		bg := ctrlButtonBG
		if cc.Disabled {
			bg = ctrlDisabled
		}
		fill(cc.CX, cc.CY, cc.CW, cc.CH, bg)
		bevel(layout.BorderOutset)
		cc.appendText(&dst, alignCenter)
		return dst
	default: // text/password/textarea/select fields
		bg := ctrlFieldBG
		if cc.Disabled {
			bg = ctrlDisabled
		}
		fill(cc.CX, cc.CY, cc.CW, cc.CH, bg)
		bevel(layout.BorderInset)
		// Clip text to the content box.
		dst = append(dst, layout.Item{Kind: layout.ClipPushKind,
			Rule: layout.RuleItem{XPt: cc.CX, YPt: cc.CY, WPt: cc.CW, HPt: cc.CH}})
		cc.appendText(&dst, alignLeft)
		dst = append(dst, layout.Item{Kind: layout.ClipPopKind})
		if cc.Kind == cssbox.CtrlSelect {
			// Dropdown triangle at the right edge.
			ink := ctrlText
			if cc.Disabled {
				ink = ctrlGray
			}
			cc.appendTriangle(&dst, ink)
		}
		return dst
	}
}
```

Add the small painters used above to `control.go`. These keep glyph emission simple — one line of text via the face advances (no full inline layout for the single-line fields; textarea wrapping is noted as a refinement below):

```go
type ctrlAlign int

const (
	alignLeft ctrlAlign = iota
	alignCenter
)

// edge returns one border strip rect for side s with the given 3D style, 1pt thick,
// inside the control's border box (the content box is already inset by ctrlBorder).
func (cc *ControlContent) edge(s layout.EdgeSide, style layout.BorderStyle) layout.BorderItem {
	x, y, w, h := cc.CX-ctrlBorder, cc.CY-ctrlBorder, cc.CW+2*ctrlBorder, cc.CH+2*ctrlBorder
	bi := layout.BorderItem{Color: ctrlBevelDark, Style: style, Side: s}
	switch s {
	case layout.EdgeTop:
		bi.XPt, bi.YPt, bi.WPt, bi.HPt = x, y, w, ctrlBorder
	case layout.EdgeBottom:
		bi.XPt, bi.YPt, bi.WPt, bi.HPt = x, y+h-ctrlBorder, w, ctrlBorder
	case layout.EdgeLeft:
		bi.XPt, bi.YPt, bi.WPt, bi.HPt = x, y, ctrlBorder, h
	case layout.EdgeRight:
		bi.XPt, bi.YPt, bi.WPt, bi.HPt = x+w-ctrlBorder, y, ctrlBorder, h
	}
	return bi
}

// appendText emits cc.Text as a single baseline row of glyphs, left- or
// center-aligned within the content box. A nil face or missing glyph is skipped.
func (cc *ControlContent) appendText(dst *[]layout.Item, align ctrlAlign) {
	if cc.Text == "" || cc.Face == nil {
		return
	}
	ink := ctrlText
	if cc.Placeholder {
		ink = ctrlGray
	}
	if cc.Disabled {
		ink = ctrlGray
	}
	asc, _, _ := cc.Face.Metrics()
	baseline := cc.CY + asc*cc.FontSizePt + ctrlPadY
	width := 0.0
	for _, r := range cc.Text {
		if _, adv, ok := cc.Face.Glyph(r); ok {
			width += adv * cc.FontSizePt
		}
	}
	x := cc.CX + ctrlPadX
	if align == alignCenter {
		if extra := cc.CW - width; extra > 0 {
			x = cc.CX + extra/2
		}
	}
	for _, r := range cc.Text {
		outline, adv, ok := cc.Face.Glyph(r)
		if ok && outline != nil {
			*dst = append(*dst, layout.Item{Kind: layout.GlyphKind,
				Glyph: layout.GlyphItem{Outline: outline, XPt: x, YPt: baseline, SizePt: cc.FontSizePt, Color: ink}})
		}
		if ok {
			x += adv * cc.FontSizePt
		}
	}
}

// appendGlyphCentered emits a single glyph centered in the content box; returns
// false (drawing nothing) when the face lacks the glyph, so the caller can fall back.
func (cc *ControlContent) appendGlyphCentered(dst *[]layout.Item, r rune, ink color.RGBA) bool {
	if cc.Face == nil {
		return false
	}
	outline, adv, ok := cc.Face.Glyph(r)
	if !ok || outline == nil {
		return false
	}
	asc, desc, _ := cc.Face.Metrics()
	gw := adv * cc.FontSizePt
	baseline := cc.CY + (cc.CH+(asc-desc)*cc.FontSizePt)/2
	x := cc.CX + (cc.CW-gw)/2
	*dst = append(*dst, layout.Item{Kind: layout.GlyphKind,
		Glyph: layout.GlyphItem{Outline: outline, XPt: x, YPt: baseline, SizePt: cc.FontSizePt, Color: ink}})
	return true
}

// appendTriangle emits a small downward triangle in the select's right-side box,
// drawn as three stacked strokes (a glyph-free approximation that always renders).
func (cc *ControlContent) appendTriangle(dst *[]layout.Item, ink color.RGBA) {
	boxX := cc.CX + cc.CW - ctrlSelectTri
	cxp := boxX + ctrlSelectTri/2
	cyp := cc.CY + cc.CH/2
	for i := 0; i < 3; i++ {
		half := float64(3 - i)
		*dst = append(*dst, layout.Item{Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: cxp - half, YPt: cyp - 2 + float64(i), WPt: 2 * half, HPt: 1, Color: ink}})
	}
}
```

CONFIRMED (verified): `font.Face.Glyph(r) (outline *render.Path, advanceEm float64, ok bool)` and `Metrics() (ascent, descent, lineGap float64)` (`pkg/font/family.go`); `layout.BorderInset` and `layout.BorderOutset` exist (`pkg/layout/page.go`, the 3D sunken/raised bevels). The paint code uses these names as written.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestControlPaintChrome`
Expected: PASS.

- [ ] **Step 5: Run the whole css + doctaculous suite (no regressions)**

Run: `go test ./pkg/layout/... ./pkg/doctaculous ./pkg/html`
Expected: PASS. No existing fixture has controls, so goldens are byte-identical.

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/fragment.go pkg/layout/css/replaced.go pkg/layout/css/control.go pkg/layout/css/control_test.go
git commit -m "feat(css): paint native chrome for form controls"
```

---

## Task 9: Golden image + showcase + roadmap

**Files:**
- Create: golden fixture entry in `pkg/doctaculous/html_golden_test.go`
- Create: `pkg/doctaculous/testdata/golden/html-forms.png` (generated)
- Modify: `testdata/htmldoc/index.html`, `testdata/htmldoc/css/main.css` (showcase section)
- Modify: `pkg/doctaculous/htmldoc_golden_test.go` (page-count constant), regenerate `htmldoc-p*.png`
- Modify: `CLAUDE.md` (status/roadmap)

- [ ] **Step 1: Add a forms golden fixture**

In `pkg/doctaculous/html_golden_test.go`, add an entry to the `htmlGoldens` slice:

```go
	{
		// Form controls: text/password fields, a checked + unchecked checkbox and
		// radio, a button, a textarea, and a select — each painted as a static native
		// widget (recessed fields, raised button, checkmark/dot, dropdown triangle).
		// A disabled field shows the muted chrome. Eyeball that nothing renders as
		// leaked inline text.
		name:       "forms",
		viewportPx: 320,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; font-family: sans-serif; }
  div { margin: 4px; }
</style></head><body>
  <div><input type="text" value="typed"></div>
  <div><input type="text" placeholder="placeholder"></div>
  <div><input type="password" value="secret"></div>
  <div><input type="checkbox" checked> <input type="checkbox"></div>
  <div><input type="radio" checked> <input type="radio"></div>
  <div><button>Submit</button> <input type="submit" value="Go"></div>
  <div><textarea>multi
line</textarea></div>
  <div><select><option>One</option><option selected>Two</option></select></div>
  <div><input type="text" value="off" disabled></div>
</body></html>`,
	},
```

- [ ] **Step 2: Generate and EYEBALL the golden**

Run: `go test ./pkg/doctaculous -run TestHTMLGolden/forms -update`
Then open `pkg/doctaculous/testdata/golden/html-forms.png` and verify: text fields are recessed boxes with their value/placeholder text, the password shows bullets, the checked checkbox shows a checkmark and the checked radio a center dot, the button is raised with a centered label, the textarea shows two lines, the select shows "Two" + a triangle, and the disabled field is muted. No leaked inline text.

- [ ] **Step 3: Run the golden in compare mode**

Run: `go test ./pkg/doctaculous -run TestHTMLGolden/forms`
Expected: PASS.

- [ ] **Step 4: Add a WPT-style reftest**

Create `pkg/doctaculous/testdata/wpt/css21-normal-flow/forms.html`:

```html
<!DOCTYPE html><html><head><style>body{margin:0;font-family:sans-serif}</style></head>
<body><input type="text" value="hi"><button>Go</button></body></html>
```

Create `pkg/doctaculous/testdata/wpt/css21-normal-flow/forms-ref.html` approximating the chrome geometry with plain styled boxes (a recessed-looking field + a raised-looking button), matching the controls' default sizes closely enough for the reftest tolerance. Then add `forms` to the reftest list in `pkg/doctaculous/wpt_reftest_test.go` (follow the existing entries' pattern). Run `go test ./pkg/doctaculous -run TestWPT` and adjust the reference boxes until it passes within tolerance.

NOTE: reftests compare within a per-pixel tolerance; if the native chrome's bevel shading makes an exact box-reference match too tight, this reftest may be dropped in favor of the golden (which is the primary proof). Decide based on the tolerance — the golden is mandatory, the reftest is best-effort.

- [ ] **Step 5: Add the showcase forms section**

In `testdata/htmldoc/index.html`, add before the colophon (a new section after "08 / IMAGE"):

```html
  <!-- ===================== 9 · FORMS ===================== -->
  <section class="section break">
    <span class="kicker">09 / FORMS</span>
    <h2>Form Controls</h2>
    <p class="lede">Static, non-interactive native widgets: fields, buttons,
      checkboxes, radios, a textarea, and a select.</p>

    <div class="formgrid">
      <label>Name <input type="text" value="Ada Lovelace"></label>
      <label>Email <input type="text" placeholder="you@example.com"></label>
      <label>Password <input type="password" value="hunter2"></label>
      <label>Notes <textarea>Two
lines of notes</textarea></label>
      <div class="checks">
        <label><input type="checkbox" checked> Subscribe</label>
        <label><input type="checkbox"> Remember me</label>
        <label><input type="radio" checked> Email</label>
        <label><input type="radio"> SMS</label>
      </div>
      <label>Plan <select><option>Free</option><option selected>Pro</option></select></label>
      <div class="actions"><button>Save</button> <input type="submit" value="Submit"></div>
    </div>
  </section>
```

In `testdata/htmldoc/css/main.css`, add styles (a simple labeled column):

```css
/* ----- forms ----------------------------------------------------------------- */
.formgrid label { display: block; margin-bottom: 10px; font-family: "TeX Gyre Heros", sans-serif; font-size: 13px; }
.formgrid .checks label { display: inline-block; margin-right: 14px; }
.formgrid .actions { margin-top: 8px; }
```

- [ ] **Step 6: Regenerate and EYEBALL the showcase goldens**

The forms section adds a page. Run: `go test ./pkg/doctaculous -run TestHTMLDocShowcase -update`. Note the new page count printed, then update the constant in `pkg/doctaculous/htmldoc_golden_test.go`:

```go
const htmlDocPages = 9 // was 8; the forms section adds a page
```

(Use the actual count the run reports.) Open the new `htmldoc-p*.png` (the forms page and any that shifted) and eyeball them.

- [ ] **Step 7: Run the full suite + gates**

Run:
```bash
go test ./...
go vet ./...
gofmt -l pkg/ cmd/ testdata/htmldoc/
golangci-lint run
go test -race ./pkg/layout/... ./pkg/doctaculous
```
Expected: all pass / no output from gofmt/lint.

- [ ] **Step 8: Update the roadmap**

In `CLAUDE.md`, add a "Done" bullet under the HTML-rendering section summarizing static form-control rendering (controls supported, classic-native chrome, the square-radio + no-CSS-theming limitations), referencing `docs/superpowers/specs/2026-06-29-html-forms-design.md`. Remove forms from any "not handled" note if present.

- [ ] **Step 9: Commit**

```bash
git add pkg/doctaculous/ testdata/htmldoc/ CLAUDE.md
git commit -m "feat: form-control golden, showcase forms section, roadmap"
```

---

## Self-review checklist (completed by plan author)

- **Spec coverage:** §1 box-gen → Tasks 2,3,5; §2 UA → Task 6; §3 sizing+floor → Tasks 4,7; §4 chrome → Task 8; §5 degradation → covered across classifyControl (Task 2: hidden/file/unknown), controlIntrinsicSize floor (Task 4), controlText empty cases (Task 3), text clipping (Task 8); §6 testing+showcase → Task 9. All sections mapped.
- **Type consistency:** `ControlKind`/`CtrlNone` (Task 1) used identically in Tasks 2–8. `controlIntrinsicSize`, `charWidth`, `textWidth`, `controlLineHeight`, `styleFor`, `attrIntOr`, `max2` defined in Task 4 and reused. `ControlContent` + `append`/`edge`/`appendText`/`appendGlyphCentered`/`appendTriangle` all defined in Task 8. `Fragment.Control` defined and consumed in Task 8.
- **API confirmations (all verified against the live code during plan self-review, marked CONFIRMED inline):** `*html.Text.Data` is an exported field (Task 3); `Element.Attr` returns map-presence so valueless boolean attrs report `ok=true` (Tasks 3, 5); `e.faces` is `*layoutfont.FaceCache` with `Resolve(family, pkgfont.Style) (*pkgfont.Face, bool)`, and `New(nil,…)` builds a fresh cache (Task 4); `Face.Glyph`/`Face.Metrics` signatures and `layout.BorderInset`/`BorderOutset` (Tasks 4, 8). No open API risks remain; the one genuinely best-effort item is the optional WPT reftest in Task 9 (the golden is the mandatory proof).
