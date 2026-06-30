# Paged Media Deferrals — Finish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement every item deferred by the CSS Paged Media PR (#27) so that all `@page` / fragmentation behavior is either fully supported or explicitly, **owner-signed-off** as out of scope — no silent deferrals remain.

**Architecture:** All work is on the reflow side at/above the `Layout`→pages boundary, plus CSS parsing — the same seam the paged-media slice used. The two structural items (mid-box fragmentation of tables/flex/grid; the `string()` running-header machinery) extend the post-pass; the rest are localized reuses of existing helpers (`FormatCounter`, the inline core). The `render.Device` seam, PDF/DOCX pipelines, and shared inline core stay untouched. Each task keeps the byte-identical guard: a document not using the feature renders unchanged.

**Tech Stack:** Go (stdlib only, no new deps); existing `pkg/css`, `pkg/layout/css`, `pkg/doctaculous` packages; golden-image tests via `-update`.

---

## ⚠️ MANDATORY SIGN-OFF POLICY (read first)

**The repository owner (Nathan) must personally sign off on every item that remains deferred at the end of this plan.** This is a hard gate, not a formality:

- A deferral is **only valid** once Nathan has explicitly approved it (in the PR thread, a commit co-message, or `docs/paged-media-deferral-signoffs.md` created in Task 0). "Logged + graceful" is **not** sufficient on its own anymore.
- Every task below that *could* end in a deferral (because the owner decides the cost isn't worth it) has an explicit **SIGN-OFF GATE** step. The worker must STOP at that gate and use `AskUserQuestion` to get an implement-vs-defer decision from Nathan **before** writing a graceful-degradation fallback.
- The default posture is **implement, not defer.** A gate exists so the owner can *choose* to defer a genuinely expensive item — it is not permission to skip work.
- Task 0 creates the sign-off ledger; the final task verifies every deferral in the codebase has a corresponding signed ledger entry, and **fails the plan** if any unsigned `deferred` / `not honored` / `TODO` log line exists in the paged-media code paths.

---

## File Structure

| File | Responsibility | Tasks |
|---|---|---|
| `docs/paged-media-deferral-signoffs.md` | The sign-off ledger: one row per deferral, with owner approval | 0, 12 |
| `pkg/layout/css/marginbox.go` | Margin-box content: counter styles, edge-box geometry | 1, 2 |
| `pkg/css/page.go` | `@page` `marks`/`bleed` recognition (parse-and-record) | 3 |
| `pkg/css/cascade.go` + `pkg/css/stringset.go` (new) | `string-set` property + `content()` capture | 4 |
| `pkg/layout/css/build.go` | Collect `string-set` values per box in document order | 5 |
| `pkg/layout/css/pagemodel.go` + `paginate.go` | Per-page string snapshots; `string()` resolution in margin boxes | 5, 6 |
| `pkg/layout/css/fragmentpage.go` | `break-inside: avoid` chains; mixed block+inline split | 7, 8 |
| `pkg/layout/css/tablepage.go` (new) | Mid-table-row fragmentation | 9 |
| `pkg/layout/css/flexgridpage.go` (new) | Mid-flex-item / mid-grid-item fragmentation | 10 |
| `pkg/layout/css/paginate.go` | Named-page per-page width reflow | 11 |

---

## Task 0: Create the sign-off ledger

**Files:**
- Create: `docs/paged-media-deferral-signoffs.md`

- [ ] **Step 1: Write the ledger skeleton**

Create `docs/paged-media-deferral-signoffs.md` with this exact content:

```markdown
# Paged Media — Deferral Sign-Off Ledger

Every row here is an item the implementation plan
(`docs/superpowers/plans/2026-06-30-paged-media-deferrals.md`) gave the owner the
option to defer rather than implement. A row is VALID only when the **Signed off by**
column names the repository owner (Nathan) AND the **Date** is filled. An item with an
empty sign-off MUST be implemented, not deferred.

The final plan task scans the paged-media code paths for `deferred` / `not honored`
log lines and fails if any lacks a signed row here.

| # | Deferred item | Why deferred | Signed off by | Date |
|---|---|---|---|---|
| _none yet_ | | | | |
```

- [ ] **Step 2: Commit**

```bash
git add docs/paged-media-deferral-signoffs.md
git commit -m "docs: add paged-media deferral sign-off ledger"
```

---

## Task 1: Counter styles beyond decimal in margin boxes

The margin-box content resolver ignores `counter(page, <style>)`'s style argument (always decimal). `FormatCounter(value int, style string)` already exists in `pkg/css/counter_format.go` and handles `lower-roman`/`upper-roman`/`lower-alpha`/`upper-alpha`/`decimal-leading-zero`. Wire it in. **This is a pure implementation task — no sign-off gate.**

**Files:**
- Modify: `pkg/layout/css/marginbox.go:44` (`resolveMarginContent`)
- Test: `pkg/layout/css/marginbox_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/marginbox_test.go`:

```go
func TestResolveMarginContentCounterStyle(t *testing.T) {
	cases := []struct {
		content    string
		page, npag int
		want       string
	}{
		{`counter(page, lower-roman)`, 4, 9, "iv"},
		{`counter(page, upper-roman)`, 4, 9, "IV"},
		{`counter(pages, upper-alpha)`, 1, 3, "C"},
		{`counter(page, decimal-leading-zero)`, 7, 9, "07"},
		{`"p. " counter(page, lower-roman)`, 2, 5, "p. ii"},
		{`counter(page, bogus-style)`, 5, 9, "5"}, // unknown style → decimal fallback
	}
	for _, c := range cases {
		got := resolveMarginContent(c.content, c.page, c.npag)
		if got != c.want {
			t.Errorf("resolveMarginContent(%q,%d,%d) = %q, want %q", c.content, c.page, c.npag, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestResolveMarginContentCounterStyle -v`
Expected: FAIL (roman/alpha/leading-zero come back as bare decimal).

- [ ] **Step 3: Implement the style argument**

In `pkg/layout/css/marginbox.go`, add the import (if not present) of the css package alias already used in the file (`gcss "github.com/nathanstitt/doctaculous/pkg/css"`), then replace the `counter(` arm of `resolveMarginContent` (currently it parses only the name and discards the style) with:

```go
		case strings.HasPrefix(comp, "counter("):
			inner := strings.TrimSuffix(strings.TrimPrefix(comp, "counter("), ")")
			parts := strings.SplitN(inner, ",", 2)
			name := strings.TrimSpace(parts[0])
			style := "decimal"
			if len(parts) == 2 {
				if s := strings.TrimSpace(parts[1]); s != "" {
					style = s
				}
			}
			switch name {
			case "page":
				b.WriteString(gcss.FormatCounter(page, style))
			case "pages":
				b.WriteString(gcss.FormatCounter(pages, style))
			}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css/ -run 'TestResolveMarginContent' -v`
Expected: PASS (both the new style test and the existing `TestResolveMarginContent`).

- [ ] **Step 5: Run the full margin-box + css suites**

Run: `go test ./pkg/layout/css/ ./pkg/css/`
Expected: ok (no regression).

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/marginbox.go pkg/layout/css/marginbox_test.go
git commit -m "feat(css): @page counter() styles (roman/alpha/leading-zero) in margin boxes"
```

---

## Task 2: Per-edge margin-box geometry (top-left / top-center / top-right share a band)

Today each edge box gets the FULL edge span, so `@top-left` and `@top-right` overlap `@top-center`. Implement the CSS Paged Media §8.3.1 distribution: the three boxes of an edge split the band by their content widths, left/right pinned to the corners and center in the middle. **This is a pure implementation task — no sign-off gate** (it is a fidelity improvement with a clear algorithm).

**Files:**
- Modify: `pkg/layout/css/marginbox.go:134` (`marginBoxRect`) and `appendMarginBoxes`
- Test: `pkg/layout/css/marginbox_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/marginbox_test.go`:

```go
func TestMarginBoxRectThreeAcrossTopEdge(t *testing.T) {
	// Page 300x200, margins 20 all sides ⇒ content 260x160 at (20,20). The top edge
	// band is y in [0,20), x in [20,280) (width 260). With all three top boxes present
	// and ~60pt-wide content each, left pins to x=20, right pins to x=280-60=220, center
	// sits at 20+(260-60)/2=120.
	g := pageGeom{pageW: 300, pageH: 200, marginL: 20, marginT: 20, contentW: 260, contentH: 160}
	widths := map[gcss.MarginBoxSlot]float64{
		gcss.MarginTopLeft:   60,
		gcss.MarginTopCenter: 60,
		gcss.MarginTopRight:  60,
	}
	l := marginBoxRectShared(gcss.MarginTopLeft, g, widths)
	c := marginBoxRectShared(gcss.MarginTopCenter, g, widths)
	r := marginBoxRectShared(gcss.MarginTopRight, g, widths)
	if l.x != 20 {
		t.Errorf("top-left x = %.1f, want 20 (pinned left)", l.x)
	}
	if c.x < 119 || c.x > 121 {
		t.Errorf("top-center x = %.1f, want ~120 (centered)", c.x)
	}
	if r.x < 219 || r.x > 221 {
		t.Errorf("top-right x = %.1f, want ~220 (pinned right)", r.x)
	}
	// A lone center box (no left/right) still centers in the full band.
	only := map[gcss.MarginBoxSlot]float64{gcss.MarginTopCenter: 60}
	c2 := marginBoxRectShared(gcss.MarginTopCenter, g, only)
	if c2.x < 119 || c2.x > 121 {
		t.Errorf("lone top-center x = %.1f, want ~120", c2.x)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestMarginBoxRectThreeAcrossTopEdge -v`
Expected: FAIL ("undefined: marginBoxRectShared").

- [ ] **Step 3: Implement `marginBoxRectShared`**

In `pkg/layout/css/marginbox.go`, add a new function (keep the existing `marginBoxRect` for the corners; the shared one handles the three-per-edge boxes and falls back to `marginBoxRect` for corners):

```go
// marginBoxRectShared computes a margin box's rect, distributing each edge's three
// boxes (left/center/right) within the edge band by their measured content widths
// (CSS Paged Media §8.3.1): the leading box pins to the leading corner, the trailing
// box to the trailing corner, the center box centers. boxW maps a present slot to its
// laid-out content width (in points); a slot absent from boxW reserves no space. Corner
// slots delegate to marginBoxRect (their geometry is unaffected by siblings).
func marginBoxRectShared(slot gcss.MarginBoxSlot, g pageGeom, boxW map[gcss.MarginBoxSlot]float64) marginRect {
	band := marginBoxRect(slot, g) // full-span band for this slot's edge (or the corner rect)
	lead, center, trail, horizontal, ok := edgeTriple(slot)
	if !ok {
		return band // a corner: unchanged
	}
	w := boxW[slot]
	switch {
	case horizontal:
		switch slot {
		case lead:
			return marginRect{x: band.x, y: band.y, w: band.w, h: band.h}
		case trail:
			return marginRect{x: band.x + band.w - w, y: band.y, w: band.w, h: band.h}
		case center:
			return marginRect{x: band.x + (band.w-w)/2, y: band.y, w: band.w, h: band.h}
		}
	default: // vertical edge: distribute along Y
		switch slot {
		case lead:
			return marginRect{x: band.x, y: band.y, w: band.w, h: band.h}
		case trail:
			return marginRect{x: band.x, y: band.y + band.h - w, w: band.w, h: band.h}
		case center:
			return marginRect{x: band.x, y: band.y + (band.h-w)/2, w: band.w, h: band.h}
		}
	}
	return band
}

// edgeTriple returns the three slots of slot's edge (lead, center, trail), whether the
// edge is horizontal (top/bottom) vs vertical (left/right), and ok=false for a corner.
func edgeTriple(slot gcss.MarginBoxSlot) (lead, center, trail gcss.MarginBoxSlot, horizontal, ok bool) {
	switch slot {
	case gcss.MarginTopLeft, gcss.MarginTopCenter, gcss.MarginTopRight:
		return gcss.MarginTopLeft, gcss.MarginTopCenter, gcss.MarginTopRight, true, true
	case gcss.MarginBottomLeft, gcss.MarginBottomCenter, gcss.MarginBottomRight:
		return gcss.MarginBottomLeft, gcss.MarginBottomCenter, gcss.MarginBottomRight, true, true
	case gcss.MarginLeftTop, gcss.MarginLeftMiddle, gcss.MarginLeftBottom:
		return gcss.MarginLeftTop, gcss.MarginLeftMiddle, gcss.MarginLeftBottom, false, true
	case gcss.MarginRightTop, gcss.MarginRightMiddle, gcss.MarginRightBottom:
		return gcss.MarginRightTop, gcss.MarginRightMiddle, gcss.MarginRightBottom, false, true
	}
	return 0, 0, 0, false, false
}
```

Note: the box's content WIDTH used for the `w` value is the laid-out text width; the leading box keeps the full band width for its own background/clip but the placement uses `w` only for pinning the trailing/center boxes. For the text glyphs the existing `appendMarginText` already aligns within the rect, so passing the band rect with the correct `x` origin is sufficient — the trailing/center boxes get an `x` shifted by their width.

- [ ] **Step 4: Thread measured widths through `appendMarginBoxes`**

In `pkg/layout/css/marginbox.go`, modify `appendMarginBoxes` so it first measures each box's text width (shape once, take `MakeLine(...).WidthPt`), builds the `boxW` map, then calls `marginBoxRectShared` instead of `marginBoxRect`. Replace the body of the loop in `appendMarginBoxes` with:

```go
	// First pass: resolve each box's text + measure its width (for edge distribution).
	type mbItem struct {
		slot  gcss.MarginBoxSlot
		text  string
		decls []gcss.Declaration
		width float64
	}
	var items2 []mbItem
	boxW := map[gcss.MarginBoxSlot]float64{}
	for _, mb := range g.used.MarginBoxes {
		text := resolveMarginContent(mb.Content, pageIndex+1, pageCount)
		if text == "" {
			continue
		}
		cs := gcss.Stylesheet{}.ComputeMarginBox(mb.Decls, marginBoxBaseStyle())
		run := inline.Run{Text: text, Family: cs.FontFamily, Bold: cs.Bold, Italic: cs.Italic, SizePt: cs.FontSizePt, Color: cs.Color}
		glyphs := inline.Shape(e.faces, []inline.Run{run}, e.logf)
		w := 0.0
		if len(glyphs) > 0 {
			w = inline.MakeLine(glyphs).WidthPt
		}
		boxW[mb.Slot] = w
		items2 = append(items2, mbItem{slot: mb.Slot, text: text, decls: mb.Decls, width: w})
	}
	// Second pass: place each box in its distributed rect.
	for _, it := range items2 {
		r := marginBoxRectShared(it.slot, g, boxW)
		if r.w <= 0 || r.h <= 0 {
			continue
		}
		items = e.appendMarginText(items, it.text, it.decls, r)
	}
	return items
```

- [ ] **Step 5: Run the margin-box tests**

Run: `go test ./pkg/layout/css/ -run 'TestMarginBox|TestResolveMarginContent' -v`
Expected: PASS (the new three-across test and all existing margin tests).

- [ ] **Step 6: Add a golden with all three top boxes**

Add to `pkg/doctaculous/pagedmedia_golden_test.go`'s `pagedMediaGoldens` slice a fixture:

```go
	{
		name:    "page-three-headers",
		wantPgs: 1,
		html: `<!DOCTYPE html><html><head><style>
  @page {
    size: 400px 240px; margin: 40px;
    @top-left { content: "L"; color:#333 }
    @top-center { content: "CENTER"; color:#333 }
    @top-right { content: "R"; color:#333 }
  }
  body { margin: 0 }
</style></head><body><div style="height:160px;background:#cccccc">x</div></body></html>`,
	},
```

- [ ] **Step 7: Generate + eyeball the golden**

Run: `go test ./pkg/doctaculous/ -run TestHTMLPagedMediaGolden -update`
Then Read `pkg/doctaculous/testdata/golden/html-page-three-headers-p0.png` and confirm: "L" pinned top-left, "CENTER" centered, "R" pinned top-right, none overlapping.

- [ ] **Step 8: Commit**

```bash
git add pkg/layout/css/marginbox.go pkg/layout/css/marginbox_test.go pkg/doctaculous/pagedmedia_golden_test.go pkg/doctaculous/testdata/golden/html-page-three-headers-p0.png
git commit -m "feat(css): distribute @page edge margin boxes (left/center/right share the band)"
```

---

## Task 3: `@page` `marks` / `bleed` — recognize and record (no rendering)

The print-production `marks` (crop/cross marks) and `bleed` have no device meaning in the raster model, so they will be DEFERRED — but per policy that deferral must be signed off, AND we still parse-and-record them so they're not silently dropped (a future SVG/PDF print target can read them). **This task has a SIGN-OFF GATE.**

**Files:**
- Modify: `pkg/css/page.go` (`UsedPage`, `applyPageDecls`)
- Test: `pkg/css/page_test.go`
- Modify: `docs/paged-media-deferral-signoffs.md`

- [ ] **Step 1: SIGN-OFF GATE — confirm marks/bleed stay non-rendering**

Use `AskUserQuestion` to ask Nathan:

> "`@page marks`/`bleed` are print-production crop/bleed controls with no raster-output meaning. Options: (a) record them on `UsedPage` but don't render (recommended — they're meaningless without a press target), and sign off the rendering deferral; (b) implement crop-mark drawing into the page bitmap anyway; (c) ignore entirely (don't even parse)."

Proceed per Nathan's answer. The steps below assume (a). If (b), expand this task to draw marks (out of this plan's pre-written scope — write a sub-plan). If (c), skip Steps 2–4 and only record the sign-off.

- [ ] **Step 2: Write the failing test (assuming option a)**

Add to `pkg/css/page_test.go`:

```go
func TestParsePageMarksBleed(t *testing.T) {
	ss := Parse(`@page { size: A4; marks: crop cross; bleed: 6pt }`)
	up := ss.ResolvePage(0, "", false)
	if up.Marks != "crop cross" {
		t.Errorf("Marks = %q, want \"crop cross\"", up.Marks)
	}
	if up.Bleed < 7.9 || up.Bleed > 8.1 { // 6pt → 8px @96
		t.Errorf("Bleed = %.2f, want ~8 (6pt)", up.Bleed)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestParsePageMarksBleed -v`
Expected: FAIL ("up.Marks undefined").

- [ ] **Step 4: Add `Marks`/`Bleed` to `UsedPage` and parse them**

In `pkg/css/page.go`, add to the `UsedPage` struct (after `HasRule`):

```go
	Marks string  // CSS Paged Media `marks` value, recorded but not rendered (no press target)
	Bleed float64 // CSS Paged Media `bleed`, in the px-as-pt scalar; recorded, not rendered
```

And in `applyPageDecls`'s switch, add:

```go
		case "marks":
			up.Marks = strings.TrimSpace(d.Value)
		case "bleed":
			if v, ok := parseAbsLengthPx(d.Value); ok {
				up.Bleed = v
			}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestParsePageMarksBleed -v`
Expected: PASS.

- [ ] **Step 6: Record the sign-off**

Append a row to `docs/paged-media-deferral-signoffs.md` (replace the `_none yet_` row if still present):

```markdown
| 1 | `@page marks` / `bleed` RENDERING (crop/cross marks, bleed area) | No press/print target in the raster model; recorded on `UsedPage.Marks`/`.Bleed` for a future SVG/PDF print backend | Nathan | <DATE Nathan approved> |
```

- [ ] **Step 7: Commit**

```bash
git add pkg/css/page.go pkg/css/page_test.go docs/paged-media-deferral-signoffs.md
git commit -m "feat(css): record @page marks/bleed (signed-off non-rendering deferral)"
```

---

## Task 4: `string-set` property + `content()` capture (parse layer)

`string-set: title content()` is the source of running-header strings. It is not parsed at all today. Add it to the cascade as a captured side value (it doesn't affect box geometry). **Pure implementation task — no gate** (it's a prerequisite for Task 6, which has the gate).

**Files:**
- Create: `pkg/css/stringset.go`
- Modify: `pkg/css/cascade.go` (`ComputedStyle` field + cascade arm)
- Test: `pkg/css/stringset_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/css/stringset_test.go`:

```go
package css

import "testing"

func TestParseStringSet(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "string-set", Value: `title content()`})
	if len(cs.StringSet) != 1 || cs.StringSet[0].Name != "title" {
		t.Fatalf("StringSet = %+v, want one entry named title", cs.StringSet)
	}
	if !cs.StringSet[0].UseContent {
		t.Errorf("entry should use content() (the element's text)")
	}
}

func TestParseStringSetLiteral(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "string-set", Value: `chapter "Ch. " content()`})
	if len(cs.StringSet) != 1 {
		t.Fatalf("want 1 entry, got %d", len(cs.StringSet))
	}
	e := cs.StringSet[0]
	if e.Name != "chapter" || e.Prefix != "Ch. " || !e.UseContent {
		t.Errorf("entry = %+v, want {chapter, prefix \"Ch. \", UseContent}", e)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css/ -run TestParseStringSet -v`
Expected: FAIL ("cs.StringSet undefined").

- [ ] **Step 3: Add the `StringSet` type + field**

Create `pkg/css/stringset.go`:

```go
package css

import "strings"

// StringSetEntry is one `string-set` assignment: it names a CSS string and how to
// build its value when an element matching the owning rule is encountered in document
// order. Only the common forms are modeled: an optional literal Prefix, an optional
// content() (the element's text), and an optional literal Suffix. The page-margin
// string() function reads the most recently set value for a Name on or before a page.
type StringSetEntry struct {
	Name       string
	Prefix     string // literal before content()
	Suffix     string // literal after content()
	UseContent bool   // include the element's text (content())
}

// parseStringSet parses a `string-set` value: "name [<string>|content()]+ [, name ...]".
// Multiple comma-separated assignments are supported. An entry with no recognizable
// parts is dropped. This reuses the content-component splitter shape (quoted strings and
// content() kept intact).
func parseStringSet(value string) []StringSetEntry {
	var out []StringSetEntry
	for _, assign := range strings.Split(value, ",") {
		fields := splitStringSetTokens(strings.TrimSpace(assign))
		if len(fields) < 1 {
			continue
		}
		e := StringSetEntry{Name: strings.ToLower(fields[0])}
		seenContent := false
		for _, tok := range fields[1:] {
			switch {
			case tok == "content()" || tok == "content(text)":
				e.UseContent = true
				seenContent = true
			case len(tok) >= 2 && (tok[0] == '"' || tok[0] == '\''):
				if seenContent {
					e.Suffix += unquoteString(tok)
				} else {
					e.Prefix += unquoteString(tok)
				}
			}
		}
		if e.Name != "" && (e.UseContent || e.Prefix != "" || e.Suffix != "") {
			out = append(out, e)
		}
	}
	return out
}

// splitStringSetTokens splits on whitespace but keeps quoted strings and `content(...)`
// parens intact.
func splitStringSetTokens(s string) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			cur.WriteByte(c)
			if c == quote {
				quote = 0
				flush()
			}
		case c == '"' || c == '\'':
			flush()
			quote = c
			cur.WriteByte(c)
		case c == '(':
			depth++
			cur.WriteByte(c)
		case c == ')':
			depth--
			cur.WriteByte(c)
			if depth == 0 {
				flush()
			}
		case (c == ' ' || c == '\t') && depth == 0:
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

func unquoteString(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
```

In `pkg/css/cascade.go`, add to `ComputedStyle` (near the other paged fields `Page`/`Widows`/`Orphans`):

```go
	// StringSet is the CSS `string-set` assignments on this box (CSS GCPM): name→value
	// builders read in document order to feed the page-margin string() function.
	// Not inherited; initial nil. Read only by the pagination pass's string snapshot.
	StringSet []StringSetEntry
```

And add the cascade arm to `applyDeclaration`'s switch (near the `page` arm):

```go
	case "string-set":
		cs.StringSet = parseStringSet(d.Value)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/css/ -run TestParseStringSet -v`
Expected: PASS.

- [ ] **Step 5: Run the css suite**

Run: `go test ./pkg/css/`
Expected: ok (StringSet is nil-default, not inherited, so no existing test perturbed).

- [ ] **Step 6: Commit**

```bash
git add pkg/css/stringset.go pkg/css/stringset_test.go pkg/css/cascade.go
git commit -m "feat(css): parse string-set + content() (running-header source capture)"
```

---

## Task 5: Per-page string snapshots (box→page string tracking)

To resolve `string(title)` on page *i*, we need the last `string-set: title content()` value seen on or before page *i*. Box generation runs document order; the post-pass knows which top-level block landed on which page. Capture each block's contributed string-set values, then build a per-page running snapshot. **Pure implementation task — no gate.**

**Files:**
- Modify: `pkg/layout/css/fragment.go` (carry `StringSet` from box) — verify the fragment already retains `Box`; if so the value is reachable.
- Modify: `pkg/layout/css/paginate.go` / `pagemodel.go` (build per-page snapshots)
- Create: `pkg/layout/css/stringsnapshot.go`
- Test: `pkg/layout/css/stringsnapshot_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/stringsnapshot_test.go`:

```go
package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// makeStringSetBlock builds a block fragment whose box sets string-set name→text and
// whose text leaf carries `text` (so content() resolves).
func makeStringSetBlock(name, text string, y float64) *Fragment {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	box.Style = gcss.ComputedStyle{StringSet: []gcss.StringSetEntry{{Name: name, UseContent: true}}}
	box.Children = []*cssbox.Box{{Kind: cssbox.BoxText, Text: text}}
	return &Fragment{Y: y, H: 10, Box: box}
}

func TestStringSnapshotPerPage(t *testing.T) {
	// Three blocks set `title` to A, B, C; buckets put A,B on page 0 and C on page 1.
	blocks := []*Fragment{
		makeStringSetBlock("title", "A", 0),
		makeStringSetBlock("title", "B", 20),
		makeStringSetBlock("title", "C", 40),
	}
	buckets := []pageBucket{
		{top: 0, blocks: blocks[:2]},
		{top: 40, blocks: blocks[2:]},
	}
	snaps := buildStringSnapshots(buckets)
	if got := snaps[0]["title"]; got != "B" {
		t.Errorf("page 0 title = %q, want B (last on page 0)", got)
	}
	if got := snaps[1]["title"]; got != "C" {
		t.Errorf("page 1 title = %q, want C", got)
	}
}

func TestStringSnapshotCarriesForward(t *testing.T) {
	// Page 0 sets title=A; page 1 sets nothing ⇒ title stays A (running value).
	blocks := []*Fragment{
		makeStringSetBlock("title", "A", 0),
		{Y: 20, H: 10, Box: &cssbox.Box{Kind: cssbox.BoxBlock}}, // no string-set
	}
	buckets := []pageBucket{
		{top: 0, blocks: blocks[:1]},
		{top: 20, blocks: blocks[1:]},
	}
	snaps := buildStringSnapshots(buckets)
	if got := snaps[1]["title"]; got != "A" {
		t.Errorf("page 1 title = %q, want A (carried forward)", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestStringSnapshot -v`
Expected: FAIL ("undefined: buildStringSnapshots").

- [ ] **Step 3: Implement the snapshot builder**

Create `pkg/layout/css/stringsnapshot.go`:

```go
package css

import "strings"

// buildStringSnapshots returns, for each page bucket, the running CSS string values
// (name → value) in effect at the START of that page: the most recent string-set value
// contributed by any block bucketed on an earlier or the same page, in document order.
// CSS GCPM `string()` (default, == the "first that starts on the page or the carried
// value") is approximated by the running last-set value through the page — adequate for
// the dominant running-header-from-headings pattern. A page with no new setter inherits
// the prior page's values (carried forward).
func buildStringSnapshots(buckets []pageBucket) []map[string]string {
	out := make([]map[string]string, len(buckets))
	running := map[string]string{}
	for i := range buckets {
		// Snapshot the running values as they stand entering this page.
		snap := make(map[string]string, len(running))
		for k, v := range running {
			snap[k] = v
		}
		out[i] = snap
		// Then apply this page's setters (so the NEXT page sees them; a setter on this
		// page updates the running value for subsequent pages, matching a header that
		// reflects the last heading seen up to and including the page).
		for _, b := range buckets[i].blocks {
			applyBlockStringSets(b, running)
		}
	}
	return out
}

// applyBlockStringSets walks a block fragment's subtree in document order, updating
// running with each box's string-set assignments (Prefix + content() text + Suffix).
func applyBlockStringSets(f *Fragment, running map[string]string) {
	if f == nil {
		return
	}
	if f.Box != nil {
		for _, e := range f.Box.Style.StringSet {
			val := e.Prefix
			if e.UseContent {
				val += boxText(f.Box)
			}
			val += e.Suffix
			running[e.Name] = val
		}
	}
	for _, c := range f.Children {
		applyBlockStringSets(c, running)
	}
}

// boxText returns the concatenated text of a box's BoxText leaf descendants (content()).
func boxText(b *cssbox.Box) string {
	if b == nil {
		return ""
	}
	if b.Kind == cssbox.BoxText {
		return b.Text
	}
	var sb strings.Builder
	for _, c := range b.Children {
		sb.WriteString(boxText(c))
	}
	return sb.String()
}
```

The file imports `"strings"` and `"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css/ -run TestStringSnapshot -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/layout/css/stringsnapshot.go pkg/layout/css/stringsnapshot_test.go
git commit -m "feat(css): per-page string-set snapshots for string() resolution"
```

---

## Task 6: Wire `string()` into margin-box content

Now resolve `string(name)` in `@page` margin boxes using the per-page snapshot. **This task has a SIGN-OFF GATE** only for the *exotic* `string(name, first|last|start|first-except)` variants — the default running value is implemented.

**Files:**
- Modify: `pkg/layout/css/marginbox.go` (`resolveMarginContent` → take a snapshot; `appendMarginBoxes` → pass it)
- Modify: `pkg/layout/css/paginate.go` / `pagemodel.go` / `assemblePages` (thread the snapshot per page)
- Test: `pkg/layout/css/marginbox_test.go`
- Modify: `docs/paged-media-deferral-signoffs.md`

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/marginbox_test.go`:

```go
func TestResolveMarginContentString(t *testing.T) {
	snap := map[string]string{"title": "Chapter Two"}
	got := resolveMarginContentWithStrings(`string(title)`, 3, 9, snap)
	if got != "Chapter Two" {
		t.Errorf("string(title) = %q, want \"Chapter Two\"", got)
	}
	// Mixed with a counter.
	got2 := resolveMarginContentWithStrings(`string(title) " — " counter(page)`, 3, 9, snap)
	if got2 != "Chapter Two — 3" {
		t.Errorf("mixed = %q, want \"Chapter Two — 3\"", got2)
	}
	// Unknown string → empty (graceful).
	if r := resolveMarginContentWithStrings(`string(missing)`, 1, 2, snap); r != "" {
		t.Errorf("unknown string = %q, want empty", r)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestResolveMarginContentString -v`
Expected: FAIL ("undefined: resolveMarginContentWithStrings").

- [ ] **Step 3: SIGN-OFF GATE — string() position keywords**

Use `AskUserQuestion` to ask Nathan:

> "`string(name)` (the running value) will be implemented. The CSS GCPM position keywords `string(name, first|last|start|first-except)` select *which* assignment on a page to use (first-on-page, last-on-page, etc.) and need per-page first/last tracking, not just the running value. Options: (a) implement only the default running value now and sign off the position-keyword deferral (recommended); (b) implement the full first/last/start tracking too."

Proceed per the answer. Steps below implement (a)'s default value and, if (b), note that first/last tracking extends `buildStringSnapshots` to record per-page first AND last setters (write a sub-plan for the extra keyword cases).

- [ ] **Step 4: Implement `resolveMarginContentWithStrings`**

In `pkg/layout/css/marginbox.go`, rename the core resolver to take a snapshot and have the old name delegate (so existing callers/tests still pass an implicit empty snapshot):

```go
// resolveMarginContent resolves content with no string() snapshot (counters + literals).
func resolveMarginContent(content string, page, pages int) string {
	return resolveMarginContentWithStrings(content, page, pages, nil)
}

// resolveMarginContentWithStrings additionally resolves string(name) against snap (the
// running CSS strings in effect on this page); an unknown or nil-snap name yields "".
func resolveMarginContentWithStrings(content string, page, pages int, snap map[string]string) string {
	content = strings.TrimSpace(content)
	if content == "" || content == "normal" || content == "none" {
		return ""
	}
	var b strings.Builder
	for _, comp := range splitContentComponents(content) {
		switch {
		case len(comp) >= 2 && (comp[0] == '"' || comp[0] == '\''):
			b.WriteString(unquote(comp))
		case strings.HasPrefix(comp, "counter("):
			// ... existing counter() arm (with the Task 1 style support) ...
		case strings.HasPrefix(comp, "string("):
			name := strings.TrimSpace(strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(comp, "string("), ")"), ",", 2)[0])
			if snap != nil {
				b.WriteString(snap[strings.ToLower(name)])
			}
		}
	}
	return b.String()
}
```

(Keep the counter() arm body from Task 1 verbatim inside the new function.)

- [ ] **Step 5: Thread the snapshot through to `appendMarginBoxes`**

Change `appendMarginBoxes`'s signature to accept the snapshot:

```go
func (e *Engine) appendMarginBoxes(items []layout.Item, g pageGeom, pageIndex, pageCount int, strings map[string]string) []layout.Item {
```

and use `resolveMarginContentWithStrings(mb.Content, pageIndex+1, pageCount, strings)`. Then in `assemblePages` (in `paginate.go`), compute `snaps := buildStringSnapshots(buckets)` once before the page loop and pass `snaps[i]` into the `appendMarginBoxes` call. Update the two other call sites (`paginate` uniform path and `paginateDoc` empty-body path) to pass `nil`.

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./pkg/layout/css/ -run 'TestResolveMarginContent|TestMarginBox' -v`
Expected: PASS (all margin tests).

- [ ] **Step 7: End-to-end golden — running header from headings**

Add to `pagedMediaGoldens`:

```go
	{
		name:    "running-header",
		wantPgs: 2,
		html: `<!DOCTYPE html><html><head><style>
  @page { size: 400px 260px; margin: 36px 20px; @top-left { content: string(sect); color:#555; font-size:11px } }
  body { margin: 0 }
  h2 { string-set: sect content(); font-size:18px; margin:0 }
  .blk { height: 180px }
</style></head><body>
  <h2>Alpha</h2><div class="blk" style="background:#fdd">one</div>
  <h2>Beta</h2><div class="blk" style="background:#dfd">two</div>
</body></html>`,
	},
```

- [ ] **Step 8: Generate + eyeball**

Run: `go test ./pkg/doctaculous/ -run TestHTMLPagedMediaGolden -update`
Read `html-running-header-p0.png` (header "Alpha") and `html-running-header-p1.png` (header "Beta"); confirm each page's top-left header reflects that page's current section.

- [ ] **Step 9: Record the sign-off (for the deferred position keywords, if option a)**

Append to `docs/paged-media-deferral-signoffs.md`:

```markdown
| 2 | `string(name, first|last|start|first-except)` POSITION KEYWORDS | The default running `string(name)` is implemented; position keywords need per-page first/last setter tracking, rarely authored | Nathan | <DATE> |
```

- [ ] **Step 10: Commit**

```bash
git add pkg/layout/css/marginbox.go pkg/layout/css/paginate.go pkg/layout/css/pagemodel.go pkg/layout/css/marginbox_test.go pkg/doctaculous/pagedmedia_golden_test.go pkg/doctaculous/testdata/golden/html-running-header-p0.png pkg/doctaculous/testdata/golden/html-running-header-p1.png docs/paged-media-deferral-signoffs.md
git commit -m "feat(css): resolve string() in @page margin boxes (running headers from string-set)"
```

---

## Task 7: `break-inside: avoid` / `break-*: avoid` chains beyond pairwise

Today keep-together is pairwise (previous block + current). Generalize to keep an arbitrary contiguous run of avoid-bound blocks together when they fit a page. **Pure implementation task — no gate** (clear algorithm).

**Files:**
- Modify: `pkg/layout/css/paginate.go` (`bucketBlocks` keep-together branch)
- Test: `pkg/layout/css/keepwithnext_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/keepwithnext_test.go`:

```go
// TestBreakAvoidChainOfThree: blocks 1,2,3 are avoid-chained (1 break-after:avoid,
// 2 break-after:avoid). A boundary that would split between 2 and 3 must carry the whole
// 1-2-3 chain to the next page when it fits.
func TestBreakAvoidChainOfThree(t *testing.T) {
	const w = 400
	blocks := measuredBlockHeights(t, blocksWithStyles(60, "", "break-after:avoid", "break-after:avoid", ""), w)
	if len(blocks) != 4 {
		t.Fatalf("want 4 blocks, got %d", len(blocks))
	}
	bh := blocks[0].H
	// Page fits ~3.5 blocks; without keep, page 0 = {0,1,2}, page 1 = {3}. Block 3 is
	// chained to 2 (and 2 to 1), so the chain 1-2-3 (3 blocks) moves together: page 0 =
	// {0}, page 1 = {1,2,3}.
	pageH := 3*bh + bh/2
	got := bucketBlocks(blocks, pageH, w, nolog)
	if len(got) != 2 || len(got[0].blocks) != 1 || len(got[1].blocks) != 3 {
		t.Fatalf("chain keep wrong: %d pages sizes %v, want {1},{3}", len(got), bucketSizes(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestBreakAvoidChainOfThree -v`
Expected: FAIL (only the last block, or the pair, moves — not the chain of three).

- [ ] **Step 3: Generalize the keep-together logic**

In `pkg/layout/css/paginate.go`, replace the pairwise keep block (the `if overflow && !forcedBefore && len(cur.blocks) >= 2 && prevBlock != nil && breakAvoidBetween(prevBlock, b)` branch) with a chain-aware version that walks back from the end of `cur.blocks` collecting the maximal avoid-bound run ending at `prevBlock`, and moves the whole run if `run + b` fits a page:

```go
		if overflow && !forcedBefore && len(cur.blocks) >= 2 && prevBlock != nil && breakAvoidBetween(prevBlock, b) {
			// Collect the maximal contiguous run at the end of cur whose internal joins are
			// all avoid-bound AND whose last member is avoid-bound to b (already true).
			runStart := len(cur.blocks) - 1
			for runStart > 0 && breakAvoidBetween(cur.blocks[runStart-1], cur.blocks[runStart]) {
				runStart--
			}
			// Leave at least one block on the current page (else this becomes a plain move).
			if runStart >= 1 {
				run := cur.blocks[runStart:]
				if fitsRunWith(run, b, pageH) {
					moved := append([]*Fragment(nil), run...)
					cur.blocks = cur.blocks[:runStart]
					buckets = append(buckets, cur)
					cur = pageBucket{top: moved[0].Y - usedTopMargin(moved[0], cbWidth)}
					cur.blocks = append(cur.blocks, moved...)
					overflow = false
				} else {
					logf("css pagination: break-*: avoid chain could not be kept together (exceeds a page); breaking")
				}
			}
		}
```

Add the helper near `fitsPair`:

```go
// fitsRunWith reports whether a contiguous run of blocks plus a trailing block b fits on
// one pageH-tall page (first block's top to b's bottom).
func fitsRunWith(run []*Fragment, b *Fragment, pageH float64) bool {
	if len(run) == 0 {
		return b.H <= pageH
	}
	return (b.Y + b.H) - run[0].Y <= pageH
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css/ -run 'TestBreakAvoid|TestBreak.*Keep' -v`
Expected: PASS (chain-of-three plus the existing pairwise tests).

- [ ] **Step 5: Run the full pagination suite**

Run: `go test ./pkg/layout/css/`
Expected: ok.

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/paginate.go pkg/layout/css/keepwithnext_test.go
git commit -m "feat(css): keep break-*: avoid chains together (not just pairwise)"
```

---

## Shared prerequisite for Tasks 8–10 (the fragmentation dispatcher)

Tasks 8, 9, and 10 all route through a single dispatcher, `splitAnyBlockForPage`, that picks the right splitter for a block by its content shape, plus the predicate `hasInFlowBlockChild`. **Whichever of Tasks 8/9/10 the executor runs FIRST must create the dispatcher** (the code for it is given in each task) and repoint the bucketer's two `splitBlockForPage(b, cur.top+pageH, widowsOf(b), orphansOf(b))` call sites (`pkg/layout/css/paginate.go`, the overflow-of-occupied-page branch and the leading-fresh-page branch) to `splitAnyBlockForPage(b, cur.top+pageH, widowsOf(b), orphansOf(b))`. Subsequent tasks then only ADD a dispatch arm. If a given task is deferred via its sign-off gate, the next non-deferred task in the group creates/extends the dispatcher with only the arms it needs. The canonical full dispatcher (all arms present) is:

```go
// splitAnyBlockForPage splits b for the page, choosing the splitter by b's content shape:
// a table breaks between rows, a column-flex/grid breaks between item rows, a block with
// in-flow block children breaks at child boundaries, and a pure-inline block line-splits.
func splitAnyBlockForPage(b *Fragment, pageBottom float64, widows, orphans int) splitResult {
	if b.Box != nil && b.Box.Display == cssbox.DisplayTable {
		return splitTableForPage(b, pageBottom)
	}
	if b.Box != nil && (b.Box.Display == cssbox.DisplayFlex || b.Box.Display == cssbox.DisplayGrid) {
		return splitFlexGridForPage(b, pageBottom)
	}
	if hasInFlowBlockChild(b) {
		return splitMixedBlock(b, pageBottom, widows, orphans)
	}
	return splitBlockForPage(b, pageBottom, widows, orphans)
}

func hasInFlowBlockChild(f *Fragment) bool {
	for _, c := range f.Children {
		if !c.IsFloat && !c.IsPositioned {
			return true
		}
	}
	return false
}
```

A task that defers its splitter must NOT leave its arm calling an undefined function — only include arms whose splitters exist. `inFlowChildren` (used by Tasks 8 and 10) is defined once in Task 8 Step 4; if Task 8 is deferred, Task 10 must add `inFlowChildren` itself (the body is given in Task 8).

---

## Task 8: Line-split a block mixing block children AND inline lines

Today a block with interleaved block children and inline lines is not line-split (placed whole). Implement splitting it by walking its in-flow children/lines in document order and cutting at a child or line boundary. **This task has a SIGN-OFF GATE** — it's moderate complexity and the owner may prefer to keep it deferred.

**Files:**
- Modify: `pkg/layout/css/fragmentpage.go` (`lineSplittable`, new `splitMixedBlock`)
- Test: `pkg/layout/css/fragmentpage_test.go`
- Modify: `docs/paged-media-deferral-signoffs.md` (if deferred)

- [ ] **Step 1: SIGN-OFF GATE — implement vs defer mixed-block splitting**

Use `AskUserQuestion`:

> "A block with BOTH block children and inline lines (e.g. a `<div>` containing a paragraph then a nested `<div>`) currently isn't fragmented — it's placed whole and overflows if too tall. Implementing it means cutting at child/line boundaries (the post-pass must re-derive the in-flow child+line vertical order). Options: (a) implement boundary splitting (recommended for fidelity); (b) keep deferred and sign off (whole-block overflow stays for this shape)."

If (b): skip Steps 2–6, append a signed ledger row (see Step 7 template), commit the ledger, done. If (a): proceed.

- [ ] **Step 2: Write the failing test (option a)**

Add to `pkg/layout/css/fragmentpage_test.go`:

```go
// A mixed block: a 4-line paragraph fragment child, then a block child, both in flow.
// Splitting at the boundary after the paragraph keeps the paragraph on page 0 and moves
// the block child to page 1.
func TestSplitMixedBlock(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	box.Style = gcss.ComputedStyle{Widows: 1, Orphans: 1}
	parent := &Fragment{Y: 0, H: 80, Box: box}
	para := makeLineBlock(0, 10, 4, 1, 1) // 4 lines at y 0..40
	child := &Fragment{Y: 40, H: 40, Box: &cssbox.Box{Kind: cssbox.BoxBlock}}
	parent.Children = []*Fragment{para, child}
	// Page bottom at 45 ⇒ the paragraph (ends 40) fits, the child (40..80) doesn't.
	res := splitMixedBlock(parent, 45, 1, 1)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a mixed split, got head=%v tail=%v", res.head, res.tail)
	}
	if len(res.head.Children) != 1 || res.head.Children[0] != para {
		t.Errorf("head should hold the paragraph only")
	}
	if len(res.tail.Children) != 1 || res.tail.Children[0] != child {
		t.Errorf("tail should hold the block child only")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestSplitMixedBlock -v`
Expected: FAIL ("undefined: splitMixedBlock").

- [ ] **Step 4: Implement `splitMixedBlock` and relax `lineSplittable`**

In `pkg/layout/css/fragmentpage.go`, add a child-boundary splitter that cuts the parent's in-flow children at the last child whose bottom fits the page (a child that is itself a line-splittable paragraph straddling the boundary recurses via `splitBlockForPage`):

```go
// splitMixedBlock splits a block that holds in-flow block children (and possibly its own
// lines, handled separately) at a CHILD boundary: children fully above pageBottom stay in
// the head; the rest go to the tail. A child straddling the boundary that is itself
// line-splittable is recursively split. widows/orphans apply to the parent's own lines
// (rare in a mixed block; treated as the whole-parent constraint). Returns {head:parent}
// if all children fit, {tail:parent} if none fit.
func splitMixedBlock(parent *Fragment, pageBottom float64, widows, orphans int) splitResult {
	inflow := inFlowChildren(parent)
	if len(inflow) == 0 {
		return splitBlockForPage(parent, pageBottom, widows, orphans) // pure-inline fallback
	}
	k := 0
	for i, c := range inflow {
		if c.Y+c.H <= pageBottom+0.5 {
			k = i + 1
		} else {
			break
		}
	}
	if k >= len(inflow) {
		return splitResult{head: parent}
	}
	if k == 0 {
		return splitResult{tail: parent}
	}
	head := *parent
	tail := *parent
	head.Children = append([]*Fragment(nil), inflow[:k]...)
	tail.Children = append([]*Fragment(nil), inflow[k:]...)
	head.H = inflow[k-1].Y + inflow[k-1].H - parent.Y
	tail.Y = inflow[k].Y
	tail.H = (parent.Y + parent.H) - tail.Y
	head.Border[layout.EdgeBottom] = BorderEdge{}
	tail.Border[layout.EdgeTop] = BorderEdge{}
	return splitResult{head: &head, tail: &tail}
}

// inFlowChildren returns f's in-flow (non-float, non-positioned) child fragments.
func inFlowChildren(f *Fragment) []*Fragment {
	var out []*Fragment
	for _, c := range f.Children {
		if !c.IsFloat && !c.IsPositioned {
			out = append(out, c)
		}
	}
	return out
}
```

Then relax `lineSplittable` so a mixed block qualifies, and route to `splitMixedBlock` in the bucketer. Change the bucketer's two `splitBlockForPage(b, ...)` calls to:

```go
res := splitAnyBlockForPage(b, cur.top+pageH, widowsOf(b), orphansOf(b))
```

and add a dispatcher in `fragmentpage.go`:

```go
// splitAnyBlockForPage splits b for the page, choosing the pure-inline line splitter or
// the mixed-block child splitter by b's content shape.
func splitAnyBlockForPage(b *Fragment, pageBottom float64, widows, orphans int) splitResult {
	if hasInFlowBlockChild(b) {
		return splitMixedBlock(b, pageBottom, widows, orphans)
	}
	return splitBlockForPage(b, pageBottom, widows, orphans)
}

func hasInFlowBlockChild(f *Fragment) bool {
	for _, c := range f.Children {
		if !c.IsFloat && !c.IsPositioned {
			return true
		}
	}
	return false
}
```

Update `lineSplittable` to also accept a mixed block (remove the in-flow-block-child disqualifier, keeping the `len(Lines) >= 2 || hasInFlowBlockChild` and not-avoid guards):

```go
func lineSplittable(b *Fragment) bool {
	if b == nil || keptInsideAvoid(b) {
		return false
	}
	return len(b.Lines) >= 2 || hasInFlowBlockChild(b)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/layout/css/ -run 'TestSplit|TestWidows|TestLineSplittable' -v`
Expected: PASS.

- [ ] **Step 6: Run the full suite**

Run: `go test ./pkg/layout/css/ ./pkg/doctaculous/`
Expected: ok (eyeball any golden diff; the showcase may reflow if a mixed block now splits — regenerate + eyeball if so).

- [ ] **Step 7: If deferred (option b), record the sign-off instead**

```markdown
| 3 | MIXED block+inline block FRAGMENTATION | A block with both block children and inline lines is placed whole (overflow if too tall) rather than split at a child/line boundary | Nathan | <DATE> |
```

- [ ] **Step 8: Commit**

```bash
git add pkg/layout/css/fragmentpage.go pkg/layout/css/paginate.go pkg/layout/css/fragmentpage_test.go
git commit -m "feat(css): fragment mixed block+inline blocks at child boundaries"
```

---

## Task 9: Mid-table-row fragmentation

A table taller than a page is placed whole (overflows). Implement breaking a table BETWEEN rows across pages (the dominant case; mid-CELL content splitting stays a sub-deferral). **This task has a SIGN-OFF GATE.**

**Files:**
- Create: `pkg/layout/css/tablepage.go`
- Modify: `pkg/layout/css/fragmentpage.go` (`splitAnyBlockForPage` dispatch to table splitter)
- Test: `pkg/layout/css/tablepage_test.go`
- Modify: `docs/paged-media-deferral-signoffs.md` (for the mid-cell sub-deferral, or full deferral)

- [ ] **Step 1: SIGN-OFF GATE — table fragmentation scope**

Use `AskUserQuestion`:

> "A table taller than a page currently overflows whole. Options for table pagination: (a) break BETWEEN rows (a row rides one page; the table splits at row boundaries, header row optionally repeats) — recommended, covers most real tables; (b) full mid-cell-content splitting too (much larger — a cell's inline content splits across pages); (c) keep deferred and sign off (tables overflow whole). If (a), the mid-cell case is a signed sub-deferral."

Proceed per answer. Steps below implement (a). If (c), skip to Step 7's ledger row.

- [ ] **Step 2: Write the failing test (option a)**

Create `pkg/layout/css/tablepage_test.go`:

```go
package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// makeTable builds a table fragment with rowCount row child fragments of rowH each,
// stacked from y0. A table fragment is identified by its Box.Display == DisplayTable.
func makeTable(y0, rowH float64, rowCount int) *Fragment {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable}
	t := &Fragment{Y: y0, H: float64(rowCount) * rowH, Box: box}
	for i := 0; i < rowCount; i++ {
		rb := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow}
		t.Children = append(t.Children, &Fragment{Y: y0 + float64(i)*rowH, H: rowH, Box: rb})
	}
	return t
}

func TestSplitTableBetweenRows(t *testing.T) {
	// 6 rows of 20pt at y0=0 (table 120pt). Page bottom 65 ⇒ 3 rows fit (bottoms 20/40/60).
	tbl := makeTable(0, 20, 6)
	res := splitTableForPage(tbl, 65)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a table split, got head=%v tail=%v", res.head, res.tail)
	}
	if len(res.head.Children) != 3 {
		t.Errorf("head rows = %d, want 3", len(res.head.Children))
	}
	if len(res.tail.Children) != 3 {
		t.Errorf("tail rows = %d, want 3", len(res.tail.Children))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestSplitTableBetweenRows -v`
Expected: FAIL ("undefined: splitTableForPage").

- [ ] **Step 4: Implement `splitTableForPage`**

Create `pkg/layout/css/tablepage.go`:

```go
package css

import (
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// splitTableForPage splits a table fragment BETWEEN rows at pageBottom: rows fully above
// pageBottom stay in the head table; the rest go to the tail table (its Y moved to the
// first kept row, top border suppressed). Mid-cell content is NOT split (a row rides one
// page). Returns {head:tbl} if all rows fit, {tail:tbl} if the first row alone overflows.
func splitTableForPage(tbl *Fragment, pageBottom float64) splitResult {
	rows := tableRowFragments(tbl)
	if len(rows) == 0 {
		return splitResult{head: tbl}
	}
	k := 0
	for i, r := range rows {
		if r.Y+r.H <= pageBottom+0.5 {
			k = i + 1
		} else {
			break
		}
	}
	if k >= len(rows) {
		return splitResult{head: tbl}
	}
	if k == 0 {
		return splitResult{tail: tbl} // first row taller than the page: move whole, overflow
	}
	head := *tbl
	tail := *tbl
	head.Children = keepRows(tbl, rows[:k])
	tail.Children = keepRows(tbl, rows[k:])
	head.H = rows[k-1].Y + rows[k-1].H - tbl.Y
	tail.Y = rows[k].Y
	tail.H = (tbl.Y + tbl.H) - tail.Y
	head.Border[layout.EdgeBottom] = BorderEdge{}
	tail.Border[layout.EdgeTop] = BorderEdge{}
	// Collapsed border strips, if any, are dropped on the tail's re-split (a documented
	// limitation — the grid is re-derived per page only for the rows it keeps).
	head.Collapsed, tail.Collapsed = nil, nil
	return splitResult{head: &head, tail: &tail}
}

// tableRowFragments returns tbl's DisplayTableRow child fragments in order (recursing
// row groups: thead/tbody/tfoot fragments hold rows).
func tableRowFragments(tbl *Fragment) []*Fragment {
	var out []*Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		for _, c := range f.Children {
			if c.Box != nil && c.Box.Display == cssbox.DisplayTableRow {
				out = append(out, c)
			} else if c.Box != nil && isRowGroup(c.Box.Display) {
				walk(c)
			}
		}
	}
	walk(tbl)
	return out
}

func isRowGroup(d cssbox.DisplayKind) bool {
	return d == cssbox.DisplayTableRowGroup || d == cssbox.DisplayTableHeaderGroup || d == cssbox.DisplayTableFooterGroup
}

// keepRows returns a child slice containing only the given rows (flattening row groups —
// a documented simplification: row-group wrappers are not preserved across the split).
func keepRows(tbl *Fragment, rows []*Fragment) []*Fragment {
	return append([]*Fragment(nil), rows...)
}
```

NOTE to implementer: verify the exact `DisplayKind` constant names (`DisplayTable`, `DisplayTableRow`, `DisplayTableRowGroup`, `DisplayTableHeaderGroup`, `DisplayTableFooterGroup`) against `pkg/layout/cssbox`; adjust if the header/footer group constants differ.

Then add a table dispatch in `splitAnyBlockForPage` (from Task 8) — or, if Task 8 was deferred, add the dispatcher now:

```go
func splitAnyBlockForPage(b *Fragment, pageBottom float64, widows, orphans int) splitResult {
	if b.Box != nil && b.Box.Display == cssbox.DisplayTable {
		return splitTableForPage(b, pageBottom)
	}
	if hasInFlowBlockChild(b) {
		return splitMixedBlock(b, pageBottom, widows, orphans)
	}
	return splitBlockForPage(b, pageBottom, widows, orphans)
}
```

And make `lineSplittable` accept a table:

```go
	return len(b.Lines) >= 2 || hasInFlowBlockChild(b) || (b.Box != nil && b.Box.Display == cssbox.DisplayTable)
```

- [ ] **Step 5: Run tests + full suite**

Run: `go test ./pkg/layout/css/ -run 'TestSplitTable|TestSplit|TestWidows' -v && go test ./pkg/layout/css/ ./pkg/doctaculous/`
Expected: PASS / ok (regenerate + eyeball any showcase golden that reflows).

- [ ] **Step 6: End-to-end golden — a table spanning two pages**

Add a `pagedMediaGoldens` fixture with a tall table on a short page; generate with `-update`; Read both page PNGs and confirm the table breaks cleanly between rows (no row cut in half).

- [ ] **Step 7: Record the mid-cell sub-deferral sign-off**

```markdown
| 4 | MID-CELL table content splitting | Tables break between ROWS (a row rides one page); a single row taller than a page overflows, and a cell's inline content is not split across pages | Nathan | <DATE> |
```

- [ ] **Step 8: Commit**

```bash
git add pkg/layout/css/tablepage.go pkg/layout/css/fragmentpage.go pkg/layout/css/tablepage_test.go pkg/doctaculous/pagedmedia_golden_test.go pkg/doctaculous/testdata/golden/ docs/paged-media-deferral-signoffs.md
git commit -m "feat(css): fragment tables between rows across pages"
```

---

## Task 10: Mid-flex-item / mid-grid-item fragmentation

A flex/grid container taller than a page overflows whole. Implement breaking BETWEEN flex lines / grid rows (items themselves ride one page; mid-item content splitting is a sub-deferral). **This task has a SIGN-OFF GATE.**

**Files:**
- Create: `pkg/layout/css/flexgridpage.go`
- Modify: `pkg/layout/css/fragmentpage.go` (`splitAnyBlockForPage` dispatch)
- Test: `pkg/layout/css/flexgridpage_test.go`
- Modify: `docs/paged-media-deferral-signoffs.md`

- [ ] **Step 1: SIGN-OFF GATE — flex/grid fragmentation scope**

Use `AskUserQuestion`:

> "A flex/grid container taller than a page overflows whole. Note: a SINGLE-LINE flex row or a one-row grid genuinely can't be split (all items share the band) — those must overflow regardless. Options: (a) break a COLUMN-direction flex container and a multi-ROW grid between item rows (items ride one page) — recommended; single-line/row cases overflow as today; (b) keep all flex/grid fragmentation deferred and sign off."

Proceed per answer. If (b), skip to the ledger row (Step 6).

- [ ] **Step 2: Write the failing test (option a)**

Create `pkg/layout/css/flexgridpage_test.go`:

```go
package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// makeGrid builds a grid container with rowCount rows of rowH (one item per row), stacked
// from y0. Items are the direct children at successive Ys.
func makeGrid(y0, rowH float64, rowCount int) *Fragment {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayGrid}
	g := &Fragment{Y: y0, H: float64(rowCount) * rowH, Box: box}
	for i := 0; i < rowCount; i++ {
		ib := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
		g.Children = append(g.Children, &Fragment{Y: y0 + float64(i)*rowH, H: rowH, Box: ib})
	}
	return g
}

func TestSplitGridBetweenRows(t *testing.T) {
	// 6 single-item rows of 20pt; page bottom 65 ⇒ 3 rows fit.
	g := makeGrid(0, 20, 6)
	res := splitFlexGridForPage(g, 65)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split, got head=%v tail=%v", res.head, res.tail)
	}
	if len(res.head.Children) != 3 || len(res.tail.Children) != 3 {
		t.Errorf("rows split %d/%d, want 3/3", len(res.head.Children), len(res.tail.Children))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestSplitGridBetweenRows -v`
Expected: FAIL ("undefined: splitFlexGridForPage").

- [ ] **Step 4: Implement `splitFlexGridForPage`**

Create `pkg/layout/css/flexgridpage.go`. The algorithm groups direct item fragments into vertical "rows" by their Y (items sharing a Y band are one row, indivisible), then cuts between rows at pageBottom:

```go
package css

import "github.com/nathanstitt/doctaculous/pkg/layout"

// splitFlexGridForPage splits a flex (column) or grid container between item ROWS at
// pageBottom: it groups direct item fragments into Y bands (items sharing a band ride one
// page together), keeps bands fully above pageBottom in the head, moves the rest to the
// tail. A single band (single-line flex / one grid row) cannot split → {tail} if it
// overflows the page from the top, else {head}. Mid-item content is not split.
func splitFlexGridForPage(c *Fragment, pageBottom float64) splitResult {
	bands := itemBands(c)
	if len(bands) <= 1 {
		if c.Y+c.H <= pageBottom+0.5 {
			return splitResult{head: c}
		}
		return splitResult{tail: c} // indivisible and overflowing
	}
	k := 0
	for i, band := range bands {
		if band.bottom <= pageBottom+0.5 {
			k = i + 1
		} else {
			break
		}
	}
	if k >= len(bands) {
		return splitResult{head: c}
	}
	if k == 0 {
		return splitResult{tail: c}
	}
	splitY := bands[k].top
	head := *c
	tail := *c
	head.Children = childrenAbove(c, splitY)
	tail.Children = childrenFrom(c, splitY)
	head.H = bands[k-1].bottom - c.Y
	tail.Y = splitY
	tail.H = (c.Y + c.H) - splitY
	head.Border[layout.EdgeBottom] = BorderEdge{}
	tail.Border[layout.EdgeTop] = BorderEdge{}
	return splitResult{head: &head, tail: &tail}
}

type band struct{ top, bottom float64 }

// itemBands groups c's direct in-flow children into vertical bands (a band spans a set of
// children whose Y-extents overlap), sorted top-to-bottom.
func itemBands(c *Fragment) []band {
	var bands []band
	for _, ch := range inFlowChildren(c) {
		t, b := ch.Y, ch.Y+ch.H
		merged := false
		for i := range bands {
			if t < bands[i].bottom && b > bands[i].top { // overlap → same band
				if t < bands[i].top {
					bands[i].top = t
				}
				if b > bands[i].bottom {
					bands[i].bottom = b
				}
				merged = true
				break
			}
		}
		if !merged {
			bands = append(bands, band{top: t, bottom: b})
		}
	}
	// bands are naturally in child order ≈ top-to-bottom for column/grid; a sort by top
	// keeps it robust.
	for a := 1; a < len(bands); a++ {
		for j := a; j > 0 && bands[j-1].top > bands[j].top; j-- {
			bands[j-1], bands[j] = bands[j], bands[j-1]
		}
	}
	return bands
}

func childrenAbove(c *Fragment, splitY float64) []*Fragment {
	var out []*Fragment
	for _, ch := range c.Children {
		if ch.Y < splitY {
			out = append(out, ch)
		}
	}
	return out
}

func childrenFrom(c *Fragment, splitY float64) []*Fragment {
	var out []*Fragment
	for _, ch := range c.Children {
		if ch.Y >= splitY {
			out = append(out, ch)
		}
	}
	return out
}
```

Add the dispatch to `splitAnyBlockForPage`:

```go
	if b.Box != nil && (b.Box.Display == cssbox.DisplayFlex || b.Box.Display == cssbox.DisplayGrid) {
		return splitFlexGridForPage(b, pageBottom)
	}
```

and accept flex/grid in `lineSplittable` (append `|| (b.Box != nil && (b.Box.Display == cssbox.DisplayFlex || b.Box.Display == cssbox.DisplayGrid))`).

NOTE to implementer: verify `DisplayFlex` / `DisplayGrid` constant names in `pkg/layout/cssbox`.

- [ ] **Step 5: Run tests + full suite**

Run: `go test ./pkg/layout/css/ -run 'TestSplitGrid|TestSplit' -v && go test ./pkg/layout/css/ ./pkg/doctaculous/`
Expected: PASS / ok.

- [ ] **Step 6: Record the sign-off (single-line + mid-item sub-deferrals)**

```markdown
| 5 | SINGLE-LINE flex / one-row grid + MID-ITEM splitting | A single-line flex row or one-row grid (items share a band) overflows whole — genuinely indivisible; a flex/grid ITEM's own content is not split across pages | Nathan | <DATE> |
```

- [ ] **Step 7: Commit**

```bash
git add pkg/layout/css/flexgridpage.go pkg/layout/css/fragmentpage.go pkg/layout/css/flexgridpage_test.go docs/paged-media-deferral-signoffs.md
git commit -m "feat(css): fragment column-flex and grid containers between item rows"
```

---

## Task 11: Named-page per-page width reflow

Today the layout width is fixed at page 0's content width; a named page (`@page wide { size: A3 landscape }` selected by `page: wide` on a section) changes only the inset, not the reflow width. Implement re-laying-out a named-page section at its own width. **This task has a SIGN-OFF GATE** — it requires a second layout pass per distinct page size, which is a real cost.

**Files:**
- Modify: `pkg/layout/css/pagemodel.go` (`LayoutPagedDoc`)
- Test: `pkg/doctaculous/pagedmedia_test.go`
- Modify: `docs/paged-media-deferral-signoffs.md`

- [ ] **Step 1: SIGN-OFF GATE — named-page width reflow**

Use `AskUserQuestion`:

> "A `page: wide` section that selects a differently-SIZED named `@page` currently lays out at the default page width and only its margin inset changes (content isn't reflowed to the wide page's width). A correct implementation re-runs layout per distinct page width and stitches the results — a meaningful cost and complexity. Options: (a) implement multi-width reflow (recommended for correctness); (b) keep the single-width approximation and sign off (named pages change size/margins/chrome but not content reflow width)."

Proceed per answer. If (b), append the ledger row below and commit — done. If (a), proceed (this is the largest single task; consider a dedicated sub-plan, as it touches the layout-driver contract).

- [ ] **Step 2 (option a): Write the failing end-to-end test**

Add to `pkg/doctaculous/pagedmedia_test.go` a test that a `page: wide` section's content uses the wide width (assert a full-width block on a wide page is wider than the default-page content width). [Implementer: build the fixture with two sections, the second `page: wide`, and assert the second section's painted block width via the page items.]

- [ ] **Step 3 (option a): Implement multi-width reflow**

In `LayoutPagedDoc`: group the document's top-level blocks by their resolved `page` name → page width; for each distinct width, run `e.layoutTree` at that content width producing a fragment subtree for those blocks; bucket each group against its own page geometry; concatenate the page lists in document order. [Implementer: this restructures `LayoutPagedDoc` from one `layoutTree` call to one per distinct width; preserve the byte-identical single-width path when only one width is used.]

- [ ] **Step 4 (option a): Run tests + full suite**

Run: `go test ./pkg/doctaculous/ ./pkg/layout/css/`
Expected: ok.

- [ ] **Step 5: If deferred (option b), record the sign-off**

```markdown
| 6 | NAMED-PAGE per-page width REFLOW | A `page: name` section selecting a differently-sized @page changes the page size/margins/chrome but content lays out at the default width (not reflowed to the named page's width) | Nathan | <DATE> |
```

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/pagemodel.go pkg/doctaculous/pagedmedia_test.go docs/paged-media-deferral-signoffs.md
git commit -m "feat(css): named-page per-width reflow"   # or: "docs: sign off named-page width-reflow deferral"
```

---

## Task 11b: `element()` running elements (margin-box live content)

`content: element(header)` moves a *live element* (a `position: running()` element) into a margin box — the only running-header mechanism that places actual styled markup (not a string) in the margin. This is a substantial feature (capture a running element's box subtree, then re-place it per page in the margin band). The expected outcome is a **signed deferral**, but the owner must choose. **This task is a SIGN-OFF GATE.**

**Files:**
- Modify: `docs/paged-media-deferral-signoffs.md`
- (If implemented: a new `pkg/layout/css/runningelement.go` — out of this plan's pre-written scope; write a sub-plan.)

- [ ] **Step 1: SIGN-OFF GATE — element() running elements**

Use `AskUserQuestion`:

> "`content: element(name)` with `position: running(name)` relocates a live styled element (not just a string) into a `@page` margin box — the richest running-header mechanism. It needs capturing the running element's box subtree and re-placing it per page. `string()` (Task 6) already covers text-only running headers. Options: (a) keep `element()` deferred and sign off (recommended — `string()` covers the common need; this is a large addition); (b) implement it (write a dedicated sub-plan — outside this plan's scope)."

- [ ] **Step 2: Record the sign-off (option a) or branch to a sub-plan (option b)**

If (a), append to `docs/paged-media-deferral-signoffs.md`:

```markdown
| 7 | `element()` / `position: running()` running ELEMENTS in margin boxes | `string()` (text) running headers are implemented; relocating a live styled element into a margin box is a large addition with no current need | Nathan | <DATE> |
```

If (b), STOP and write `docs/superpowers/plans/2026-06-30-running-elements.md` before proceeding.

- [ ] **Step 3: Commit**

```bash
git add docs/paged-media-deferral-signoffs.md
git commit -m "docs: sign off element() running-element deferral"
```

---

## Task 12: Final verification — no unsigned deferrals remain

Scan the paged-media code paths for any `deferred` / `not honored` / `not honored in this slice` / `TODO` log line or comment, and fail unless every one maps to a signed ledger row. **Pure verification task.**

**Files:**
- Create: `pkg/doctaculous/deferral_audit_test.go`
- Modify: `docs/paged-media-deferral-signoffs.md` (finalize)

- [ ] **Step 1: Write the audit test**

Create `pkg/doctaculous/deferral_audit_test.go`:

```go
package doctaculous

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoUnsignedDeferrals greps the paged-media source files for deferral log lines and
// requires the sign-off ledger to be non-empty and to mention each deferred topic. It is
// a guard that every remaining deferral was explicitly signed off by the owner (per the
// plan's sign-off policy), not silently shipped.
func TestNoUnsignedDeferrals(t *testing.T) {
	ledger, err := os.ReadFile(filepath.Join("..", "..", "docs", "paged-media-deferral-signoffs.md"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	led := string(ledger)
	// Each signed row names "Nathan"; require at least one signed row, and that the
	// ledger no longer contains the "_none yet_" placeholder.
	if strings.Contains(led, "_none yet_") {
		t.Errorf("ledger still has the _none yet_ placeholder — fill in or remove")
	}
	signed := strings.Count(led, "| Nathan |")
	if signed == 0 {
		t.Errorf("no signed-off deferrals in the ledger; every remaining deferral must be signed by the owner")
	}
	// Sanity: the deferral log strings that remain in code must each have a ledger row.
	// (This is a soft check: it lists the deferral markers found so review can confirm.)
	deferRe := regexp.MustCompile(`(?i)(deferred|not honored|not split|overflowing, not splitting)`)
	roots := []string{
		filepath.Join("..", "layout", "css", "marginbox.go"),
		filepath.Join("..", "layout", "css", "fragmentpage.go"),
		filepath.Join("..", "layout", "css", "paginate.go"),
		filepath.Join("..", "layout", "css", "tablepage.go"),
		filepath.Join("..", "layout", "css", "flexgridpage.go"),
		filepath.Join("..", "layout", "css", "pagemodel.go"),
	}
	for _, r := range roots {
		data, err := os.ReadFile(r)
		if err != nil {
			continue // a file a deferred task never created
		}
		for i, line := range strings.Split(string(data), "\n") {
			if deferRe.MatchString(line) && strings.Contains(strings.ToLower(line), "logf") {
				t.Logf("deferral marker %s:%d — confirm a signed ledger row covers it: %s", filepath.Base(r), i+1, strings.TrimSpace(line))
			}
		}
	}
}
```

- [ ] **Step 2: Run the audit**

Run: `go test ./pkg/doctaculous/ -run TestNoUnsignedDeferrals -v`
Expected: PASS, with `t.Logf` lines listing each remaining deferral marker. Review the logged markers against the ledger; if any marker lacks a signed row, STOP and get Nathan's sign-off (add the ledger row) before continuing.

- [ ] **Step 3: Full suite + race + lint**

Run:
```bash
go test ./... && go test -race ./pkg/css/... ./pkg/layout/css/... ./pkg/doctaculous/... && go vet ./... && golangci-lint run ./pkg/css/... ./pkg/layout/css/... ./pkg/doctaculous/...
```
Expected: all ok / 0 issues.

- [ ] **Step 4: Update the roadmap (CLAUDE.md)**

Edit the paged-media Done bullet in `CLAUDE.md` to replace the deferral list with the now-implemented items and the (signed) remaining ones, pointing at `docs/paged-media-deferral-signoffs.md`.

- [ ] **Step 5: Commit**

```bash
git add pkg/doctaculous/deferral_audit_test.go docs/paged-media-deferral-signoffs.md CLAUDE.md
git commit -m "test(css): audit that every remaining paged-media deferral is owner-signed-off"
```

- [ ] **Step 6: Push + PR**

```bash
git push -u origin <branch>
gh pr create --title "Paged media: finish deferred items (table/flex/grid fragmentation, string(), counter styles)" --body "..."
```

---

## Self-Review notes (for the plan author / executor)

- **Sign-off coverage:** Tasks 3, 6, 8, 9, 10, 11 each carry an explicit `AskUserQuestion` SIGN-OFF GATE before any graceful-degradation fallback is written. Tasks 1, 2, 4, 5, 7 are pure implementations with no deferral option. Task 12 enforces the ledger.
- **Verify-before-defer:** No task writes a "deferred" log line without either implementing the feature or routing through a gate + ledger row.
- **Constant-name caveats:** Tasks 9 and 10 include explicit NOTEs to verify `cssbox.Display*` constant names against the codebase before relying on them — do that first in each task.
- **Byte-identical guard:** every task runs the full `pkg/layout/css` + `pkg/doctaculous` suites; any showcase golden reflow must be regenerated AND eyeballed in that task, not deferred to the end.
