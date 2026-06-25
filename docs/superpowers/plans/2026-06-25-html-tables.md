# HTML Table Layout (CSS 2.1 §17) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement CSS 2.1 §17 table layout in the HTML reflow engine so `<table>` markup (and any `display:table`/row/cell content) lays out as a real table — column-width solve (fixed + auto), row heights, cell layout, full colspan/rowspan, both `border-collapse` models, captions — replacing the current block fallback.

**Architecture:** Tables live entirely in the reflow engine + box model (`pkg/layout/css`, `pkg/layout/cssbox`, `pkg/css`, `pkg/html`). A new `pkg/layout/css/table.go` builds a private grid from the box tree, solves column widths (auto layout uses a new min/max-content measurement in `measure.go`), lays each cell out via the existing `layoutBlock`, resolves row heights, and emits ordinary `Fragment`s. `border-collapse:collapse` adds a resolved-edge model (`tableborder.go`) painted by a small new fragment primitive. The `render.Device` seam, the PDF pipeline, and the shared inline core (`pkg/layout/inline`) are **untouched**.

**Tech Stack:** Go (pure, no CGo). Existing engine: `pkg/css` (hand-written CSS cascade), `pkg/html` (`golang.org/x/net/html` DOM), `pkg/layout/cssbox` (box tree), `pkg/layout/css` (layout engine), `pkg/layout/paint` + `pkg/render/raster` (paint/raster), `pkg/doctaculous` (public API + golden/reftest harness).

**Spec:** `docs/superpowers/specs/2026-06-25-html-tables-design.md` (read it first).

**Branch:** `feat/html-tables` (already created off `feat/html-zindex-6b`; the spec is already committed there). Every subagent: you are on `feat/html-tables`, do NOT checkout/stash/switch branches, do NOT commit unless a step says to. Delete any `zz_*` scratch file before finishing and confirm `git status` is clean.

---

## Critical process notes (read before any task)

- **Sandbox blocks the Go build cache + TLS.** Run every `go` / `gofmt` / `golangci-lint` command (and any `git push`/`gh`) with the sandbox **disabled** (`dangerouslyDisableSandbox: true`). A sandboxed `go test` fails with cache/permission errors that are NOT real test failures.
- **Editor diagnostics LAG.** After adding a field/file you may see stale "undefined"/"unused"/"redeclared" errors and phantom `zz_*` files. Trust `go build ./...` / `go test`, not the panel. `find . -name 'zz_*'` and delete any before finishing.
- **`golangci-lint` here does NOT run gofmt.** After each task run BOTH: `gofmt -l <changed dirs>` (must print nothing) and `golangci-lint run ./pkg/css/... ./pkg/layout/... ./pkg/html/... ./pkg/doctaculous/...` (must pass). **NO `//nolint`.** The repo **declines all "modernize" hints**: keep explicit `if x < y { x = y }` clamps (NOT `max()`/`min()`), indexed `for i := 0; i < n; i++` loops (NOT range-over-int), and `sort.SliceStable` (NOT `slices.SortStableFunc`). golangci-lint flags `if !(a && b)` (QF1001) — write the De Morgan form `if !a || !b`.
- **Verify against the CSS spec, don't trust this plan blindly.** Table layout has subtleties. If a step forces you to invert an existing passing test, STOP and verify the rule (WebFetch the W3C CSS 2.1 §17 text) before proceeding.
- **Byte-identical guard (load-bearing).** Tables ADD a layout mode; no existing non-table page may change. After tasks that touch layout/box-gen, run `go test ./pkg/doctaculous/... ./pkg/render/raster/...` (WITHOUT `-update`) and confirm `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` shows ONLY new files. A changed existing golden/reftest means table work leaked into block layout — fix before proceeding.
- **Commit after each task** with the message in its final step. Use `go test ./... ` green + `gofmt -l` clean + `golangci-lint` clean as the gate before committing.

## File structure (decomposition)

| File | New? | Responsibility |
|---|---|---|
| `pkg/layout/cssbox/box.go` | modify | New `DisplayKind`s (row-group/header/footer-group, column/column-group, caption); `BoxAnonTablePart` `BoxKind`; `ColSpan`/`RowSpan` fields. |
| `pkg/css/cascade.go` | modify | Parse + cascade `border-collapse`, `border-spacing` (→ `BorderSpacingH/V`), `table-layout`, `vertical-align`, `caption-side`, `direction`. |
| `pkg/html/ua.go` | modify | UA rules: `<table>`→table, group/column/caption display defaults. |
| `pkg/layout/css/build.go` | modify | `classifyDisplay` maps the new display values; `generate` reads `colspan`/`rowspan`/`<col span>` onto the box. |
| `pkg/layout/css/tablefix.go` | **new** | Anonymous-table-box fixup (CSS 17.2.1), called from `normalize`. |
| `pkg/layout/css/measure.go` | **new** | `measureMinContent`/`measureMaxContent` (measure-mode pass; prerequisite for auto widths). |
| `pkg/layout/css/table.go` | **new** | `layoutTable`: grid build, column-width solve (fixed+auto), cell layout, row heights (incl. rowspan distribution), vertical-align, fragment emission, captions. |
| `pkg/layout/css/tableborder.go` | **new** | `border-collapse:collapse`: 17.6.2 conflict resolution + resolved-edge list. |
| `pkg/layout/css/block.go` | modify | `establishesNewBFC` true for table-cell; `layoutInterior` `case cssbox.TableFC`. |
| `pkg/layout/css/fragment.go` | modify | `Collapsed []CollapsedEdge` on `Fragment`; emit in `AppendItems`. |
| `pkg/layout/paint/paint.go` | modify | Stroke collapsed edges. |
| `pkg/doctaculous/html_golden_test.go` | modify | Table golden fixtures. |
| `pkg/doctaculous/wpt_reftest_test.go` + `testdata/wpt/css21-normal-flow/*.html` | modify/new | Table reftest pairs. |

## Task order & dependencies

Tasks build bottom-up so each lands green:
1. Box vocabulary (cssbox) — types first.
2. CSS properties (pkg/css) — parse/cascade.
3. UA stylesheet + classifyDisplay + span attrs (pkg/html, build.go).
4. Anonymous-table fixup (tablefix.go).
5. Min/max-content measurement (measure.go).
6. Grid construction (table.go part 1).
7. Fixed column-width solve + cell layout + row heights + emission, wired into layoutInterior — **first rendering** (table.go part 2 + block.go).
8. Auto column-width solve (table.go part 3).
9. Spanning: colspan width + rowspan height distribution (table.go part 4).
10. vertical-align (table.go part 5).
11. Captions (table.go part 6).
12. border-collapse:collapse (tableborder.go + fragment.go + paint.go).
13. Golden images.
14. WPT reftests + degradation tests + CLAUDE.md update.

---

### Task 1: Box vocabulary (cssbox)

**Files:**
- Modify: `pkg/layout/cssbox/box.go`
- Test: `pkg/layout/cssbox/box_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

Create/append `pkg/layout/cssbox/box_test.go`:

```go
package cssbox

import "testing"

func TestTableDisplayKinds(t *testing.T) {
	// The new table-part display kinds must be distinct values.
	kinds := []DisplayKind{
		DisplayTable, DisplayTableRowGroup, DisplayTableHeaderGroup,
		DisplayTableFooterGroup, DisplayTableRow, DisplayTableColumn,
		DisplayTableColumnGroup, DisplayTableCaption, DisplayTableCell,
	}
	seen := map[DisplayKind]bool{}
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("duplicate DisplayKind value %d", k)
		}
		seen[k] = true
	}
}

func TestAnonTablePartIsBlockLevelAndAnonymous(t *testing.T) {
	b := &Box{Kind: BoxAnonTablePart}
	if !b.Kind.IsBlockLevel() {
		t.Errorf("BoxAnonTablePart should be block-level for surrounding flow")
	}
	if b.Kind.IsInlineLevel() {
		t.Errorf("BoxAnonTablePart should not be inline-level")
	}
}

func TestSpanFieldsDefaultZero(t *testing.T) {
	// A non-table box never sets spans; the grid builder reads zero as 1.
	b := &Box{Kind: BoxBlock}
	if b.ColSpan != 0 || b.RowSpan != 0 {
		t.Errorf("spans default to zero; got col=%d row=%d", b.ColSpan, b.RowSpan)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/cssbox/ -run 'TestTable|TestAnon|TestSpan' -v`
Expected: FAIL — `undefined: DisplayTableRowGroup` / `BoxAnonTablePart` / `b.ColSpan`.

- [ ] **Step 3: Add the display kinds**

In `pkg/layout/cssbox/box.go`, replace the `DisplayKind` const block with (the new entries inserted in §17 order; values shift but they are never persisted, only compared):

```go
const (
	DisplayBlock DisplayKind = iota
	DisplayInline
	DisplayInlineBlock
	DisplayListItem
	DisplayTable
	DisplayTableRowGroup
	DisplayTableHeaderGroup
	DisplayTableFooterGroup
	DisplayTableRow
	DisplayTableColumn
	DisplayTableColumnGroup
	DisplayTableCaption
	DisplayTableCell
	DisplayFlex
	DisplayGrid
	// DisplayNone is never emitted as a box (display:none subtrees are pruned);
	// it exists so a DisplayKind can round-trip the value if needed.
	DisplayNone
)
```

- [ ] **Step 4: Add the BoxKind**

In the `BoxKind` const block, add after `BoxAnonInline`:

```go
	// BoxAnonTablePart is an anonymous table / row-group / row / cell wrapper
	// inserted by the anonymous-table-box fixup (CSS 17.2.1). Like BoxAnonBlock/
	// BoxAnonInline it carries a zero-value ComputedStyle; its Display/Formatting
	// say which table part it stands in for. isAnonymous() treats it as anonymous.
	BoxAnonTablePart
```

Update `IsBlockLevel` to include it:

```go
func (k BoxKind) IsBlockLevel() bool {
	return k == BoxBlock || k == BoxAnonBlock || k == BoxAnonTablePart
}
```

- [ ] **Step 5: Add the span fields**

In the `Box` struct, after the `Position PositionKind` field, add:

```go
	// ColSpan / RowSpan are the HTML colspan/rowspan presentational attributes,
	// read in pkg/html (like <img width/height>), honored only for a
	// DisplayTableCell box; <col span> reuses ColSpan on a DisplayTableColumn box.
	// Zero means "absent" and reads as 1 in the grid builder, so non-table boxes
	// (which never set these) are unaffected.
	ColSpan int
	RowSpan int
```

- [ ] **Step 6: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/cssbox/ -run 'TestTable|TestAnon|TestSpan' -v`
Expected: PASS.

- [ ] **Step 7: Build the whole module (the enum-shift must not break anything)**

Run (sandbox disabled): `go build ./... && go test ./pkg/layout/... -count=1`
Expected: builds; existing tests PASS (the new enum values are unused elsewhere yet).

- [ ] **Step 8: Format, lint, commit**

Run (sandbox disabled):
```bash
gofmt -l pkg/layout/cssbox/
golangci-lint run ./pkg/layout/cssbox/...
git add pkg/layout/cssbox/box.go pkg/layout/cssbox/box_test.go
git commit -m "cssbox: table display kinds, anon table-part box, cell span fields"
```
Expected: `gofmt -l` prints nothing; lint passes; commit succeeds.

---

### Task 2: CSS properties (parse + cascade)

**Files:**
- Modify: `pkg/css/cascade.go`
- Test: `pkg/css/cascade_test.go` (append)

Properties: `border-collapse` (separate|collapse, inherited), `border-spacing` (1-2 lengths → `BorderSpacingH/V`, inherited), `table-layout` (auto|fixed), `vertical-align` (keyword set), `caption-side` (top|bottom, inherited), `direction` (ltr|rtl, inherited — parsed only).

- [ ] **Step 1: Write the failing test**

Append to `pkg/css/cascade_test.go`:

```go
func TestTableProperties(t *testing.T) {
	sheet := Parse(`
		table { border-collapse: collapse; border-spacing: 4px 8px; table-layout: fixed;
		        caption-side: bottom; direction: rtl; }
		td { vertical-align: middle; }
	`)
	r := NewResolver([]OriginSheet{{Origin: OriginAuthor, Sheet: sheet}}, nil)

	tbl := r.ComputeRoot(testElem("table"))
	if tbl.BorderCollapse != "collapse" {
		t.Errorf("border-collapse = %q, want collapse", tbl.BorderCollapse)
	}
	if tbl.BorderSpacingH != 4 || tbl.BorderSpacingV != 8 {
		t.Errorf("border-spacing = %v,%v want 4,8", tbl.BorderSpacingH, tbl.BorderSpacingV)
	}
	if tbl.TableLayout != "fixed" {
		t.Errorf("table-layout = %q, want fixed", tbl.TableLayout)
	}
	if tbl.CaptionSide != "bottom" {
		t.Errorf("caption-side = %q, want bottom", tbl.CaptionSide)
	}
	if tbl.Direction != "rtl" {
		t.Errorf("direction = %q, want rtl", tbl.Direction)
	}
	td := r.ComputeRoot(testElem("td"))
	if td.VerticalAlign != "middle" {
		t.Errorf("vertical-align = %q, want middle", td.VerticalAlign)
	}
}

func TestTablePropertyDefaults(t *testing.T) {
	cs := initialStyle()
	if cs.BorderCollapse != "separate" {
		t.Errorf("default border-collapse = %q, want separate", cs.BorderCollapse)
	}
	if cs.TableLayout != "auto" {
		t.Errorf("default table-layout = %q, want auto", cs.TableLayout)
	}
	if cs.VerticalAlign != "baseline" {
		t.Errorf("default vertical-align = %q, want baseline", cs.VerticalAlign)
	}
	if cs.CaptionSide != "top" {
		t.Errorf("default caption-side = %q, want top", cs.CaptionSide)
	}
	if cs.Direction != "ltr" {
		t.Errorf("default direction = %q, want ltr", cs.Direction)
	}
}

func TestBorderSpacingSingleValue(t *testing.T) {
	sheet := Parse(`table { border-spacing: 6px; }`)
	r := NewResolver([]OriginSheet{{Origin: OriginAuthor, Sheet: sheet}}, nil)
	tbl := r.ComputeRoot(testElem("table"))
	if tbl.BorderSpacingH != 6 || tbl.BorderSpacingV != 6 {
		t.Errorf("single border-spacing = %v,%v want 6,6", tbl.BorderSpacingH, tbl.BorderSpacingV)
	}
}
```

If `testElem` does not exist in the test package, use the existing helper the file already uses to build a `css.Node` for a tag name (grep `cascade_test.go` for how other tests construct a node, e.g. a `fakeNode`/`elem` helper, and match it). Adjust the three `testElem("...")` calls accordingly.

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/css/ -run 'TestTableProp|TestBorderSpacing' -v`
Expected: FAIL — `tbl.BorderCollapse` undefined.

- [ ] **Step 3: Add the fields to ComputedStyle**

In `pkg/css/cascade.go`, in the `ComputedStyle` struct, after the `ZIndexAuto bool` field add:

```go
	// Table properties (CSS 2.1 §17).
	// BorderCollapse: "separate" (initial) | "collapse". Inherited.
	BorderCollapse string
	// BorderSpacingH/V: the two axes of border-spacing in points (initial 0,0).
	// Inherited; used only in border-collapse:separate.
	BorderSpacingH, BorderSpacingV float64
	// TableLayout: "auto" (initial) | "fixed". On the table box.
	TableLayout string
	// VerticalAlign: "baseline" (initial) | "top" | "middle" | "bottom" (+ sub/
	// super/text-top/text-bottom parsed, mapped to baseline for table-cell purposes).
	VerticalAlign string
	// CaptionSide: "top" (initial) | "bottom". Inherited.
	CaptionSide string
	// Direction: "ltr" (initial) | "rtl". Inherited. Parsed but NOT acted on (RTL
	// deferred); a non-ltr value on a table is logged by the layout engine.
	Direction string
```

- [ ] **Step 4: Add the initial values**

In `initialStyle()`, add to the returned literal (alongside `Overflow: "visible"` etc.):

```go
		BorderCollapse: "separate",
		TableLayout:    "auto",
		VerticalAlign:  "baseline",
		CaptionSide:    "top",
		Direction:      "ltr",
		// BorderSpacingH/V default to 0 (zero value).
```

- [ ] **Step 5: Add the inherited ones to inheritFrom**

In `inheritFrom`, after `cs.TextAlign = parent.TextAlign`, add:

```go
	cs.BorderCollapse = parent.BorderCollapse
	cs.BorderSpacingH = parent.BorderSpacingH
	cs.BorderSpacingV = parent.BorderSpacingV
	cs.CaptionSide = parent.CaptionSide
	cs.Direction = parent.Direction
	// table-layout and vertical-align are NOT inherited (per CSS).
```

- [ ] **Step 6: Add the apply cases**

In `applyDeclaration`'s `switch d.Property`, add these cases (anywhere after the `overflow` case is fine; keep them grouped):

```go
	case "border-collapse":
		switch d.Value {
		case "separate", "collapse":
			cs.BorderCollapse = d.Value
		}
	case "border-spacing":
		applyBorderSpacing(cs, d.Value)
	case "table-layout":
		switch d.Value {
		case "auto", "fixed":
			cs.TableLayout = d.Value
		}
	case "vertical-align":
		switch d.Value {
		case "baseline", "top", "middle", "bottom",
			"sub", "super", "text-top", "text-bottom":
			cs.VerticalAlign = d.Value
		}
	case "caption-side":
		switch d.Value {
		case "top", "bottom":
			cs.CaptionSide = d.Value
		}
	case "direction":
		switch d.Value {
		case "ltr", "rtl":
			cs.Direction = d.Value
		}
```

- [ ] **Step 7: Add the border-spacing helper**

Add (near `applyBoxLengths` / `setLength` in `cascade.go`):

```go
// applyBorderSpacing parses border-spacing: one length sets both axes, two lengths
// set horizontal then vertical. Percentages/auto are invalid here and dropped. A
// malformed value leaves the prior spacing intact.
func applyBorderSpacing(cs *ComputedStyle, value string) {
	tz := newTokenizer(value)
	var lens []Length
	for {
		tok := tz.next()
		if tok.Kind == TokenEOF {
			break
		}
		if tok.Kind == TokenWhitespace {
			continue
		}
		l, ok := parseLength(tok)
		if !ok || l.Unit == UnitAuto || l.Unit == UnitPercent {
			return // invalid component: drop the whole declaration
		}
		lens = append(lens, l)
	}
	switch len(lens) {
	case 1:
		cs.BorderSpacingH = lens[0].Value
		cs.BorderSpacingV = lens[0].Value
	case 2:
		cs.BorderSpacingH = lens[0].Value
		cs.BorderSpacingV = lens[1].Value
	}
}
```

Note: if the tokenizer has no `TokenWhitespace`/`TokenEOF` kinds with those exact names, grep `pkg/css/tokenizer.go` (or wherever `Token`/`newTokenizer` live) for the real kind names and match them; the loop just needs to walk non-whitespace tokens until end. (`em`/`rem`/`pt` values are accepted by `parseLength` and treated 1:1 — the engine resolves em elsewhere; for `border-spacing` treat the raw value as points like the rest of this slice does for lengths on a table.)

- [ ] **Step 8: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/css/ -run 'TestTableProp|TestBorderSpacing' -v`
Expected: PASS.

- [ ] **Step 9: Full pkg/css test (no regressions), format, lint, commit**

Run (sandbox disabled):
```bash
go test ./pkg/css/... -count=1
gofmt -l pkg/css/
golangci-lint run ./pkg/css/...
git add pkg/css/cascade.go pkg/css/cascade_test.go
git commit -m "css: parse+cascade table properties (border-collapse/spacing, table-layout, vertical-align, caption-side, direction)"
```
Expected: all green; commit succeeds.

---

### Task 3: UA stylesheet, classifyDisplay, span attributes

**Files:**
- Modify: `pkg/html/ua.go`
- Modify: `pkg/layout/css/build.go` (`classifyDisplay`, `generate`)
- Test: `pkg/html/ua_test.go` (append or create), `pkg/layout/css/build_test.go` (append)

- [ ] **Step 1: Write the failing UA + display test**

Append to `pkg/layout/css/build_test.go` (this package can drive HTML→box and assert display):

```go
func TestTableUADisplaysAndSpans(t *testing.T) {
	html := `<table><caption>C</caption><colgroup><col span="2"></colgroup>
		<thead><tr><th colspan="2">H</th></tr></thead>
		<tbody><tr><td rowspan="2">A</td><td>B</td></tr></tbody>
		<tfoot><tr><td>F</td></tr></tfoot></table>`
	root := buildBoxTreeForTest(t, html) // see helper note below
	tbl := findByDisplay(root, cssbox.DisplayTable)
	if tbl == nil {
		t.Fatal("no DisplayTable box; <table> UA rule missing")
	}
	if findByDisplay(tbl, cssbox.DisplayTableCaption) == nil {
		t.Error("no caption box")
	}
	if findByDisplay(tbl, cssbox.DisplayTableHeaderGroup) == nil {
		t.Error("no thead/table-header-group box")
	}
	if findByDisplay(tbl, cssbox.DisplayTableFooterGroup) == nil {
		t.Error("no tfoot/table-footer-group box")
	}
	if findByDisplay(tbl, cssbox.DisplayTableColumnGroup) == nil {
		t.Error("no colgroup/table-column-group box")
	}
	col := findByDisplay(tbl, cssbox.DisplayTableColumn)
	if col == nil || col.ColSpan != 2 {
		t.Errorf("col span not read onto box: %+v", col)
	}
	th := findByDisplay(tbl, cssbox.DisplayTableCell)
	if th == nil || th.ColSpan != 2 {
		t.Errorf("th colspan=2 not read; got %+v", th)
	}
	// the rowspan cell
	rs := findCellWithRowSpan(tbl, 2)
	if rs == nil {
		t.Error("td rowspan=2 not read onto a cell box")
	}
}
```

Helper note: grep `build_test.go` for how existing tests turn an HTML string into a box tree (there will be a helper that calls `pkg/html` parse + `generate`/`Build` + `normalize`). Reuse it as `buildBoxTreeForTest`. Add small local helpers in the test file:

```go
func findByDisplay(b *cssbox.Box, d cssbox.DisplayKind) *cssbox.Box {
	if b == nil {
		return nil
	}
	if b.Display == d {
		return b
	}
	for _, c := range b.Children {
		if r := findByDisplay(c, d); r != nil {
			return r
		}
	}
	return nil
}

func findCellWithRowSpan(b *cssbox.Box, n int) *cssbox.Box {
	if b == nil {
		return nil
	}
	if b.Display == cssbox.DisplayTableCell && b.RowSpan == n {
		return b
	}
	for _, c := range b.Children {
		if r := findCellWithRowSpan(c, n); r != nil {
			return r
		}
	}
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestTableUADisplaysAndSpans -v`
Expected: FAIL — no `DisplayTable` box (the `<table>` UA rule still says block) and `ColSpan` is 0.

- [ ] **Step 3: Fix the UA stylesheet**

In `pkg/html/ua.go`, edit `uaSource`: REMOVE `table` from the block-group selector list (line with `... pre, table, form, ...` → drop `table,`), and add the table block after the `td, th` line:

```css
table   { display: table; }
thead   { display: table-header-group; }
tbody   { display: table-row-group; }
tfoot   { display: table-footer-group; }
col      { display: table-column; }
colgroup { display: table-column-group; }
caption { display: table-caption; }
```

(Keep the existing `tr { display: table-row; }`, `td, th { display: table-cell; }`, and `th { font-weight: bold; }` lines.)

- [ ] **Step 4: Extend classifyDisplay**

In `pkg/layout/css/build.go` `classifyDisplay`, add these cases before the `case "block":` arm:

```go
	case "table-row-group":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableRowGroup, cssbox.TableFC
	case "table-header-group":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableHeaderGroup, cssbox.TableFC
	case "table-footer-group":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableFooterGroup, cssbox.TableFC
	case "table-column":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableColumn, cssbox.TableFC
	case "table-column-group":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableColumnGroup, cssbox.TableFC
	case "table-caption":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableCaption, cssbox.BlockFC
```

- [ ] **Step 5: Read the span attributes in generate**

In `pkg/layout/css/build.go` `generate`, after `applyBlockify(b, cs)` (and before the `replacedTags` check), add:

```go
	// HTML presentational span attributes onto table-part boxes (CSS does not carry
	// these). colspan/rowspan apply to a cell; <col span>/<colgroup span> reuse ColSpan.
	switch b.Display {
	case cssbox.DisplayTableCell:
		b.ColSpan = attrSpan(e, "colspan")
		b.RowSpan = attrSpan(e, "rowspan")
	case cssbox.DisplayTableColumn, cssbox.DisplayTableColumnGroup:
		b.ColSpan = attrSpan(e, "span")
	}
```

Add the helper near `attrSnapshot`:

```go
// attrSpan reads an HTML span attribute (colspan/rowspan/span) as a positive
// integer, defaulting to 1 when absent, non-numeric, or < 1 (HTML clamps these to
// at least 1). The box stores the resolved value (never 0) on a span-bearing box.
func attrSpan(e *html.Element, name string) int {
	v, ok := e.Attr(name)
	if !ok {
		return 1
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return 1
	}
	return n
}
```

Ensure `build.go` imports `"strconv"` and `"strings"` (add to the import block if missing).

- [ ] **Step 6: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestTableUADisplaysAndSpans -v`
Expected: PASS.

- [ ] **Step 7: Byte-identical guard + full suite**

Run (sandbox disabled):
```bash
go test ./pkg/html/... ./pkg/layout/... -count=1
go test ./pkg/doctaculous/... -count=1
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
```
Expected: all PASS; the `git status` prints NOTHING (no golden changed — no existing fixture uses `<table>`). If a golden changed, STOP and investigate.

- [ ] **Step 8: Format, lint, commit**

Run (sandbox disabled):
```bash
gofmt -l pkg/html/ pkg/layout/css/
golangci-lint run ./pkg/html/... ./pkg/layout/css/...
git add pkg/html/ua.go pkg/layout/css/build.go pkg/layout/css/build_test.go
git commit -m "html/css: table UA rules, classifyDisplay table parts, read colspan/rowspan/col-span onto box"
```

---

### Task 4: Anonymous-table-box fixup

**Files:**
- Create: `pkg/layout/css/tablefix.go`
- Modify: `pkg/layout/css/anon.go` (call the fixup from `normalize`)
- Test: `pkg/layout/css/tablefix_test.go`

The fixup runs per `DisplayTable` subtree and guarantees a well-formed `table → (caption?) → (column/column-group)* → row-group → row → cell` tree. Implement the common, load-bearing rules (cell-without-row, non-cell-in-row, non-row-in-table, whitespace drop). Keep it focused; do not gold-plate exotic reparenting.

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/tablefix_test.go`:

```go
package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// tcell/trow/ttable build raw (pre-fixup) boxes with a given display.
func tbox(d cssbox.DisplayKind, kids ...*cssbox.Box) *cssbox.Box {
	fc := cssbox.BlockFC
	if d != cssbox.DisplayTableCaption {
		fc = cssbox.TableFC
	}
	if d == cssbox.DisplayTableCell {
		fc = cssbox.BlockFC
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: d, Formatting: fc, Children: kids}
}
func ttext(s string) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxText, Display: cssbox.DisplayInline, Text: s}
}

func TestFixupCellWithoutRow(t *testing.T) {
	// table > cell  =>  table > anon-row-group > anon-row > cell
	tbl := tbox(cssbox.DisplayTable, tbox(cssbox.DisplayTableCell, ttext("X")))
	fixupTable(tbl)
	rg := tbl.Children[0]
	if rg.Display != cssbox.DisplayTableRowGroup || rg.Kind != cssbox.BoxAnonTablePart {
		t.Fatalf("want anon row-group, got display=%v kind=%v", rg.Display, rg.Kind)
	}
	row := rg.Children[0]
	if row.Display != cssbox.DisplayTableRow || row.Kind != cssbox.BoxAnonTablePart {
		t.Fatalf("want anon row, got display=%v kind=%v", row.Display, row.Kind)
	}
	if row.Children[0].Display != cssbox.DisplayTableCell {
		t.Fatalf("cell lost under anon row")
	}
}

func TestFixupStrayTextInRow(t *testing.T) {
	// row > text  =>  row > anon-cell > text
	row := tbox(cssbox.DisplayTableRow, ttext("hi"))
	wrapStrayInRow(row)
	if row.Children[0].Display != cssbox.DisplayTableCell || row.Children[0].Kind != cssbox.BoxAnonTablePart {
		t.Fatalf("stray text not wrapped in anon cell: %+v", row.Children[0])
	}
}

func TestFixupWhitespaceDropped(t *testing.T) {
	// table > "  " between two row-groups: the whitespace text is dropped, not wrapped.
	tbl := tbox(cssbox.DisplayTable,
		tbox(cssbox.DisplayTableRowGroup),
		ttext("   "),
		tbox(cssbox.DisplayTableRowGroup),
	)
	fixupTable(tbl)
	for _, c := range tbl.Children {
		if c.Kind == cssbox.BoxText {
			t.Fatalf("whitespace text survived in table: %q", c.Text)
		}
	}
	if len(tbl.Children) != 2 {
		t.Fatalf("want 2 row-groups after dropping whitespace, got %d", len(tbl.Children))
	}
}

func TestFixupCaptionStaysDirectChild(t *testing.T) {
	tbl := tbox(cssbox.DisplayTable,
		tbox(cssbox.DisplayTableCaption, ttext("cap")),
		tbox(cssbox.DisplayTableRow, tbox(cssbox.DisplayTableCell)),
	)
	fixupTable(tbl)
	if tbl.Children[0].Display != cssbox.DisplayTableCaption {
		t.Fatalf("caption not first child after fixup")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestFixup -v`
Expected: FAIL — `undefined: fixupTable / wrapStrayInRow`.

- [ ] **Step 3: Implement the fixup**

Create `pkg/layout/css/tablefix.go`:

```go
package css

import "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"

// fixupTables walks the whole tree and repairs every DisplayTable subtree per CSS
// 17.2.1 (anonymous table objects), so the grid builder can assume a well-formed
// table > (caption?) > (column/column-group)* > row-group > row > cell structure.
// Called from normalize after the inline/block fixups. Idempotent on a well-formed
// tree.
func fixupTables(b *cssbox.Box) {
	for _, c := range b.Children {
		fixupTables(c)
	}
	if b.Display == cssbox.DisplayTable {
		fixupTable(b)
	}
}

// anonPart builds an anonymous table-part box of the given display with a fitting
// formatting context (a cell is a BlockFC container; structural parts are TableFC).
func anonPart(d cssbox.DisplayKind, kids []*cssbox.Box) *cssbox.Box {
	fc := cssbox.TableFC
	if d == cssbox.DisplayTableCell {
		fc = cssbox.BlockFC
	}
	return &cssbox.Box{Kind: cssbox.BoxAnonTablePart, Display: d, Formatting: fc, Children: kids}
}

func isWSText(b *cssbox.Box) bool {
	return b.Kind == cssbox.BoxText && isAllWS(b.Text)
}

func isRowGroup(d cssbox.DisplayKind) bool {
	return d == cssbox.DisplayTableRowGroup ||
		d == cssbox.DisplayTableHeaderGroup ||
		d == cssbox.DisplayTableFooterGroup
}

func isColumnPart(d cssbox.DisplayKind) bool {
	return d == cssbox.DisplayTableColumn || d == cssbox.DisplayTableColumnGroup
}

// fixupTable repairs the direct children of a table box: captions and column parts
// stay as direct children; row-groups are recursed into (their rows/cells fixed);
// bare rows are gathered into an anonymous row-group; any other content (stray text,
// a stray cell, a block) is gathered into an anonymous row-group > row > cell. Inter-
// part whitespace is dropped.
func fixupTable(tbl *cssbox.Box) {
	var out []*cssbox.Box
	var looseRows []*cssbox.Box // bare table-row children awaiting an anon row-group
	var looseMisc []*cssbox.Box // non-row, non-group, non-caption, non-column children

	flushMisc := func() {
		if len(looseMisc) == 0 {
			return
		}
		// Wrap the misc run as one anonymous row > cell, then queue it as a loose row.
		cell := anonPart(cssbox.DisplayTableCell, looseMisc)
		row := anonPart(cssbox.DisplayTableRow, []*cssbox.Box{cell})
		looseRows = append(looseRows, row)
		looseMisc = nil
	}
	flushRows := func() {
		flushMisc()
		if len(looseRows) == 0 {
			return
		}
		out = append(out, anonPart(cssbox.DisplayTableRowGroup, looseRows))
		looseRows = nil
	}

	for _, c := range tbl.Children {
		switch {
		case isWSText(c):
			// drop inter-part whitespace
		case c.Display == cssbox.DisplayTableCaption:
			flushRows()
			out = append(out, c)
		case isColumnPart(c.Display):
			flushRows()
			out = append(out, c)
		case isRowGroup(c.Display):
			flushRows()
			fixupRowGroup(c)
			out = append(out, c)
		case c.Display == cssbox.DisplayTableRow:
			flushMisc()
			fixupRow(c)
			looseRows = append(looseRows, c)
		case c.Display == cssbox.DisplayTableCell:
			// a bare cell becomes its own row in the loose-row stream
			flushMisc()
			fixupRow(nil) // no-op; cell wrapped next
			looseRows = append(looseRows, anonPart(cssbox.DisplayTableRow, []*cssbox.Box{c}))
		default:
			looseMisc = append(looseMisc, c)
		}
	}
	flushRows()
	tbl.Children = out
}

// fixupRowGroup repairs a row-group: rows are recursed; bare cells/misc are gathered
// into anonymous rows; whitespace dropped.
func fixupRowGroup(rg *cssbox.Box) {
	var out []*cssbox.Box
	var misc []*cssbox.Box
	flushMisc := func() {
		if len(misc) == 0 {
			return
		}
		cell := anonPart(cssbox.DisplayTableCell, misc)
		out = append(out, anonPart(cssbox.DisplayTableRow, []*cssbox.Box{cell}))
		misc = nil
	}
	for _, c := range rg.Children {
		switch {
		case isWSText(c):
			// drop
		case c.Display == cssbox.DisplayTableRow:
			flushMisc()
			fixupRow(c)
			out = append(out, c)
		case c.Display == cssbox.DisplayTableCell:
			flushMisc()
			out = append(out, anonPart(cssbox.DisplayTableRow, []*cssbox.Box{c}))
		default:
			misc = append(misc, c)
		}
	}
	flushMisc()
	rg.Children = out
}

// fixupRow repairs a row: cells stay; any non-cell content (stray text/blocks) is
// wrapped in anonymous cells; whitespace dropped. A nil row is a no-op (used as a
// readability marker at one call site).
func fixupRow(row *cssbox.Box) {
	if row == nil {
		return
	}
	wrapStrayInRow(row)
}

// wrapStrayInRow replaces runs of non-cell children of a row with anonymous cells.
func wrapStrayInRow(row *cssbox.Box) {
	var out []*cssbox.Box
	var run []*cssbox.Box
	flush := func() {
		if len(run) == 0 {
			return
		}
		out = append(out, anonPart(cssbox.DisplayTableCell, run))
		run = nil
	}
	for _, c := range row.Children {
		switch {
		case isWSText(c):
			// drop inter-cell whitespace
		case c.Display == cssbox.DisplayTableCell:
			flush()
			out = append(out, c)
		default:
			run = append(run, c)
		}
	}
	flush()
	row.Children = out
}
```

- [ ] **Step 4: Call the fixup from normalize**

In `pkg/layout/css/anon.go` `normalize`, the function recurses bottom-up then runs the three passes. The table fixup must run on the WHOLE tree once (it recurses itself). The simplest correct wiring: call it from `Build`/the caller of `normalize` right AFTER `normalize`, OR add a top-level guard. Grep `build.go` for where `normalize(root)` is called and add immediately after it:

```go
	normalize(root)
	fixupTables(root)
```

(Do NOT call `fixupTables` from inside `normalize`'s per-box recursion — it does its own whole-subtree walk and would run O(depth) times. One call on the root after normalize is correct and idempotent.)

If `normalize` is called in more than one place, grep for all callers and add `fixupTables` after each, or wrap them in a small `normalizeTree(root)` that does both and update callers. Pick the minimal change that runs `fixupTables(root)` exactly once per tree.

- [ ] **Step 5: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestFixup -v`
Expected: PASS.

- [ ] **Step 6: Byte-identical guard + suite**

Run (sandbox disabled):
```bash
go test ./pkg/layout/... -count=1
go test ./pkg/doctaculous/... -count=1
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
```
Expected: PASS; `git status` prints nothing (fixup only fires on `DisplayTable` subtrees, none exist in current fixtures).

- [ ] **Step 7: Format, lint, commit**

Run (sandbox disabled):
```bash
gofmt -l pkg/layout/css/
golangci-lint run ./pkg/layout/css/...
git add pkg/layout/css/tablefix.go pkg/layout/css/anon.go pkg/layout/css/tablefix_test.go
git commit -m "css/layout: anonymous-table-box fixup (CSS 17.2.1)"
```

---

### Task 5: Min/max-content measurement

**Files:**
- Create: `pkg/layout/css/measure.go`
- Test: `pkg/layout/css/measure_test.go`

Auto column widths need each cell's min-content (widest unbreakable unit) and max-content (no-wrap) width. Build them as a measure-mode pass that reuses the real inline/block layout so measured == laid-out.

The simplest correct construction that reuses existing code: lay the box out via the existing `layoutBlock` at a very large width (→ max-content = the resulting *used content width*, i.e. the widest line), and at width 0 / a tiny width (→ min-content = the widest line when everything wraps to its smallest unit). To avoid relying on `layoutBlock` returning the natural content width (it fills the given width), compute the inline contributions directly from the shaped glyphs for the common leaf case, and recurse for block children.

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/measure_test.go`:

```go
package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// cellWithText builds a minimal table-cell box containing one text run at a known
// font size, styled enough for shaping (font family + size come from defaults).
func cellWithText(s string) *cssbox.Box {
	st := gcss.ComputedStyle{FontSizePt: 16}
	txt := &cssbox.Box{Kind: cssbox.BoxText, Text: s, Display: cssbox.DisplayInline, Style: st}
	return &cssbox.Box{
		Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
		Formatting: cssbox.InlineFC, Style: st, Children: []*cssbox.Box{txt},
	}
}

func TestMeasureMaxGEMin(t *testing.T) {
	e := New(nil, nil, nil)
	c := cellWithText("Hello world wide")
	mn := e.measureMinContent(context.Background(), c)
	mx := e.measureMaxContent(context.Background(), c)
	if mn <= 0 || mx <= 0 {
		t.Fatalf("non-positive measures min=%v max=%v", mn, mx)
	}
	if mx < mn {
		t.Fatalf("max-content (%v) < min-content (%v)", mx, mn)
	}
}

func TestMeasureMaxIsWholeString(t *testing.T) {
	e := New(nil, nil, nil)
	short := e.measureMaxContent(context.Background(), cellWithText("ab"))
	long := e.measureMaxContent(context.Background(), cellWithText("ab ab ab ab"))
	if long <= short {
		t.Fatalf("max-content should grow with no-wrap content: short=%v long=%v", short, long)
	}
}

func TestMeasureMinIsLongestWord(t *testing.T) {
	e := New(nil, nil, nil)
	// min-content is the widest single word; adding more short words must NOT raise it.
	a := e.measureMinContent(context.Background(), cellWithText("hi hi hi"))
	b := e.measureMinContent(context.Background(), cellWithText("hi"))
	if a != b {
		t.Fatalf("min-content changed with more short words: %v vs %v", a, b)
	}
	wide := e.measureMinContent(context.Background(), cellWithText("hi enormouslylongword"))
	if wide <= b {
		t.Fatalf("min-content should reflect the longest word: %v vs %v", wide, b)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestMeasure -v`
Expected: FAIL — `undefined: e.measureMinContent`.

- [ ] **Step 3: Implement measurement**

Create `pkg/layout/css/measure.go`. It gathers the box's inline runs (reusing `gatherInlineRuns`), shapes once, then derives max-content (whole unbroken width) and min-content (widest unbreakable unit via a zero-width break). For block-level children it recurses and combines (max-content of a block = max over children; min-content = max over children) and adds the box's own horizontal border+padding.

```go
package css

import (
	"context"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/layout/inline"
)

// measureMaxContent returns box's max-content width: the width it occupies with no
// line wrapping (CSS 3 sizing; used by auto table layout, CSS 17.5.2.2). For an
// inline-formatting box it is the width of the single unbroken line of all its
// inline content; for a block container it is the widest child's max-content; a
// specified width pins it. The box's own horizontal padding+border is added.
func (e *Engine) measureMaxContent(ctx context.Context, b *cssbox.Box) float64 {
	return e.measureContent(ctx, b, true)
}

// measureMinContent returns box's min-content width: the narrowest width without
// overflow — the widest single unbreakable unit (longest word / atomic inline /
// replaced intrinsic width). Computed by breaking the shaped glyphs at width 0 and
// taking the widest resulting line. The box's own horizontal padding+border is added.
func (e *Engine) measureMinContent(ctx context.Context, b *cssbox.Box) float64 {
	return e.measureContent(ctx, b, false)
}

// measureContent is the shared core; wantMax selects max-content (no wrap) vs
// min-content (everything wraps to its smallest unit). It honors a specified width
// on the box (which pins both), recurses block children, and adds horizontal
// border+padding. It allocates no fragments (measure-only).
func (e *Engine) measureContent(ctx context.Context, b *cssbox.Box, wantMax bool) float64 {
	// A specified, non-auto, non-percentage width pins the content contribution.
	var inner float64
	if w, ok := specifiedContentWidth(b); ok {
		inner = w
	} else if b.Formatting == cssbox.InlineFC {
		inner = e.measureInline(ctx, b, wantMax)
	} else {
		// Block container (or table/flex/grid fallback): combine children.
		for _, c := range b.Children {
			cw := e.measureContent(ctx, c, wantMax) + horizontalEdges(c)
			if cw > inner {
				inner = cw
			}
		}
	}
	return inner
}

// measureInline gathers b's inline runs, shapes them once, and returns either the
// unbroken width (max-content) or the widest zero-width-break line (min-content).
func (e *Engine) measureInline(ctx context.Context, b *cssbox.Box, wantMax bool) float64 {
	var runs []inline.Run
	var atomics []*Fragment // discarded; measure-only
	e.gatherInlineRuns(ctx, b, 1e9, &runs, &atomics)
	if len(runs) == 0 {
		return 0
	}
	glyphs := inline.Shape(e.faces, runs, e.logf)
	if wantMax {
		return inline.VisibleWidth(glyphs)
	}
	// min-content: break at width 0 so each line is a single unbreakable unit; the
	// widest such line is the min-content width.
	widest := 0.0
	rest := glyphs
	for len(rest) > 0 {
		var line []inline.Glyph
		line, rest = inline.BreakNext(rest, 0)
		if len(line) == 0 {
			// BreakNext could not place even one unit at width 0 — force one glyph to
			// avoid a spin (the unit is wider than 0; take its visible width).
			line, rest = rest[:1], rest[1:]
		}
		w := inline.VisibleWidth(line)
		if w > widest {
			widest = w
		}
	}
	return widest
}
```

- [ ] **Step 4: Add the small helpers**

These may already exist under different names — grep `pkg/layout/css/block.go` for `usedEdges`, `resolveContentWidth`, and how a specified width is read. If equivalents exist, call them; otherwise add to `measure.go`:

```go
// specifiedContentWidth returns the box's content-box width if it has a fixed
// (px/pt/em, non-auto, non-percentage) width, accounting for box-sizing. ok is
// false for auto/percentage widths (which do not pin intrinsic sizing).
func specifiedContentWidth(b *cssbox.Box) (float64, bool) {
	w := b.Style.Width
	if w.Unit == gcssUnitAuto() || w.Unit == gcssUnitPercent() {
		return 0, false
	}
	val := w.Value
	// border-box width includes padding+border; subtract them for the content width.
	if b.Style.BoxSizing == "border-box" {
		val -= horizontalEdges(b)
		if val < 0 {
			val = 0
		}
	}
	return val, true
}

// horizontalEdges is the box's left+right padding + border width in points,
// resolving against a zero basis (percentage padding is rare on cells; treat as 0
// for measurement — a documented approximation).
func horizontalEdges(b *cssbox.Box) float64 {
	return lengthPx(b.Style.PaddingLeft) + lengthPx(b.Style.PaddingRight) +
		lengthPx(b.Style.BorderLeftWidth) + lengthPx(b.Style.BorderRightWidth)
}
```

Use the package's existing length resolution. **Before writing the two `gcssUnit*`/`lengthPx` shims, grep `block.go`/`inline.go` for how lengths are turned into px** (there is certainly a helper, e.g. `resolveLen`, `pxOf`, or direct `Unit` comparison against `gcss.UnitPx`). Replace `gcssUnitAuto()/gcssUnitPercent()/lengthPx(...)` with the real predicates/functions (e.g. `b.Style.Width.Unit == gcss.UnitAuto`, and the real px resolver). Do NOT invent new helpers if the package already has them — match existing code. The shims above are placeholders to be replaced with the project's actual length API discovered by grep.

- [ ] **Step 5: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestMeasure -v`
Expected: PASS. If `TestMeasureMinIsLongestWord` is flaky on exact equality, it should still hold because measurement is deterministic for the same glyphs; if the bundled font's space handling differs, adjust the assertion to `a <= b+epsilon` — but first confirm the cause (the inline core's `VisibleWidth` excludes trailing spaces).

- [ ] **Step 6: Format, lint, commit**

Run (sandbox disabled):
```bash
go test ./pkg/layout/css/... -count=1
gofmt -l pkg/layout/css/
golangci-lint run ./pkg/layout/css/...
git add pkg/layout/css/measure.go pkg/layout/css/measure_test.go
git commit -m "css/layout: min/max-content width measurement (prerequisite for auto table layout)"
```

---

### Task 6: Grid construction

**Files:**
- Create: `pkg/layout/css/table.go` (the grid type + builder; layout phases come in later tasks)
- Test: `pkg/layout/css/table_grid_test.go`

Build the private `tableGrid` from a fixed-up table box: flatten row-groups in visual order (header → body → footer), resolve the column count, and assign each cell to its origin slot honoring colspan/rowspan via an occupancy scan.

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/table_grid_test.go`:

```go
package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func cell(colSpan, rowSpan int) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
		Formatting: cssbox.BlockFC, ColSpan: colSpan, RowSpan: rowSpan}
}
func rowOf(cells ...*cssbox.Box) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow,
		Formatting: cssbox.TableFC, Children: cells}
}
func groupOf(d cssbox.DisplayKind, rows ...*cssbox.Box) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: d, Formatting: cssbox.TableFC, Children: rows}
}
func tableOf(kids ...*cssbox.Box) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC, Children: kids}
}

func TestGridColumnCountFromColspan(t *testing.T) {
	tbl := tableOf(groupOf(cssbox.DisplayTableRowGroup,
		rowOf(cell(2, 1), cell(1, 1)), // 3 columns
		rowOf(cell(1, 1), cell(1, 1), cell(1, 1)),
	))
	g := buildGrid(tbl)
	if len(g.cols) \!= 3 {
		t.Fatalf("want 3 columns, got %d", len(g.cols))
	}
	if len(g.rows) \!= 2 {
		t.Fatalf("want 2 rows, got %d", len(g.rows))
	}
}

func TestGridRowspanReservesLowerSlot(t *testing.T) {
	// Row 0: A(rowspan 2) at col 0, B at col 1.
	// Row 1: C must land at col 1 (col 0 reserved by A).
	tbl := tableOf(groupOf(cssbox.DisplayTableRowGroup,
		rowOf(cell(1, 2), cell(1, 1)),
		rowOf(cell(1, 1)),
	))
	g := buildGrid(tbl)
	// Find the cell originating in row 1; it must be at col 1.
	var c *gridCell
	for _, gc := range g.cells {
		if gc.row == 1 {
			c = gc
		}
	}
	if c == nil {
		t.Fatal("no cell originating in row 1")
	}
	if c.col \!= 1 {
		t.Fatalf("row-1 cell should be pushed to col 1 by the rowspan above; got col %d", c.col)
	}
}

func TestGridHeaderFooterVisualOrder(t *testing.T) {
	// Document order: tbody, then thead, then tfoot. Visual order: thead, tbody, tfoot.
	tbl := tableOf(
		groupOf(cssbox.DisplayTableRowGroup, rowOf(cell(1, 1))),       // body  -> visual row 1
		groupOf(cssbox.DisplayTableHeaderGroup, rowOf(cell(1, 1))),    // head  -> visual row 0
		groupOf(cssbox.DisplayTableFooterGroup, rowOf(cell(1, 1))),    // foot  -> visual row 2
	)
	g := buildGrid(tbl)
	if len(g.rows) \!= 3 {
		t.Fatalf("want 3 rows, got %d", len(g.rows))
	}
	if g.rows[0].box.Display \!= cssbox.DisplayTableRow {
		t.Fatalf("rows should be table-row boxes")
	}
	// The header group's row must be first.
	if g.rows[0].box \!= tbl.Children[1].Children[0] {
		t.Errorf("header row not placed first in visual order")
	}
	if g.rows[2].box \!= tbl.Children[2].Children[0] {
		t.Errorf("footer row not placed last in visual order")
	}
}

func TestGridColspanClamp(t *testing.T) {
	// A colspan wider than the table is clamped to the available columns.
	tbl := tableOf(groupOf(cssbox.DisplayTableRowGroup, rowOf(cell(9, 1))))
	g := buildGrid(tbl)
	if len(g.cols) \!= 9 {
		// A single 9-col cell defines a 9-column grid (that is the table's width).
		t.Fatalf("want 9 columns from a colspan-9 cell, got %d", len(g.cols))
	}
	if g.cells[0].colSpan \!= 9 {
		t.Fatalf("colSpan should be 9, got %d", g.cells[0].colSpan)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestGrid -v`
Expected: FAIL — `undefined: buildGrid / gridCell / tableGrid`.

- [ ] **Step 3: Implement the grid types + builder**

Create `pkg/layout/css/table.go` with the grid model and builder (layout phases are added in later tasks; keep those as separate functions to be filled):

```go
package css

import "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"

// tableGrid is the private intermediate the table layout algorithm operates on: the
// row/column grid recovered from a fixed-up table box, the single source of truth
// for the width solve, height solve, and border resolution. It never escapes the
// layoutTable call.
type tableGrid struct {
	table    *cssbox.Box
	caption  *cssbox.Box
	rows     []*gridRow
	cols     []gridCol
	cells    []*gridCell
	collapse bool
	fixed    bool
	spacingH float64
	spacingV float64
}

type gridRow struct {
	box    *cssbox.Box
	cells  []*gridCell // cells ORIGINATING in this row (not spanned into it)
	height float64
	y      float64
}

type gridCol struct {
	hasWidth bool
	width    float64 // specified/hint width (px) when hasWidth
	pct      float64 // percentage width [0..100], or <0 when none (Task 8b)
	min, max float64 // content min/max-content (auto layout, Task 8)
	x        float64 // solved left offset within the table content box
}

type gridCell struct {
	box      *cssbox.Box
	row, col int
	rowSpan  int
	colSpan  int
	frag     *Fragment
}

// buildGrid recovers the grid from a fixed-up table box. It flattens row-groups in
// visual order (header groups, then body row-groups in document order, then footer
// groups), reads <col>/<colgroup> width hints, and assigns each cell to its origin
// slot with an occupancy scan honoring colspan/rowspan.
func buildGrid(tbl *cssbox.Box) *tableGrid {
	g := &tableGrid{
		table:    tbl,
		collapse: tbl.Style.BorderCollapse == "collapse",
		fixed:    tbl.Style.TableLayout == "fixed",
	}
	if \!g.collapse {
		g.spacingH = tbl.Style.BorderSpacingH
		g.spacingV = tbl.Style.BorderSpacingV
	}

	// 1. Collect caption + column hints + rows (visual order).
	var headRows, bodyRows, footRows []*cssbox.Box
	collectRows := func(group *cssbox.Box) []*cssbox.Box {
		var rows []*cssbox.Box
		for _, c := range group.Children {
			if c.Display == cssbox.DisplayTableRow {
				rows = append(rows, c)
			}
		}
		return rows
	}
	for _, c := range tbl.Children {
		switch c.Display {
		case cssbox.DisplayTableCaption:
			if g.caption == nil {
				g.caption = c
			}
		case cssbox.DisplayTableColumn:
			g.addColumnHint(c)
		case cssbox.DisplayTableColumnGroup:
			// A column-group with <col> children contributes each col; an empty one
			// contributes `span` columns of its own hint.
			cols := 0
			for _, cc := range c.Children {
				if cc.Display == cssbox.DisplayTableColumn {
					g.addColumnHint(cc)
					cols++
				}
			}
			if cols == 0 {
				g.addColumnHintN(c, spanOf(c))
			}
		case cssbox.DisplayTableHeaderGroup:
			headRows = append(headRows, collectRows(c)...)
		case cssbox.DisplayTableFooterGroup:
			footRows = append(footRows, collectRows(c)...)
		case cssbox.DisplayTableRowGroup:
			bodyRows = append(bodyRows, collectRows(c)...)
		case cssbox.DisplayTableRow:
			// a bare row (fixup normally wraps these, but be defensive)
			bodyRows = append(bodyRows, c)
		}
	}
	visualRows := make([]*cssbox.Box, 0, len(headRows)+len(bodyRows)+len(footRows))
	visualRows = append(visualRows, headRows...)
	visualRows = append(visualRows, bodyRows...)
	visualRows = append(visualRows, footRows...)

	// 2. Occupancy scan: assign each cell to its origin slot.
	occupied := [][]bool{}
	ensure := func(r, c int) {
		for len(occupied) <= r {
			occupied = append(occupied, make([]bool, len(g.cols)))
		}
		if c >= len(g.cols) {
			// grow column count
			grow := c + 1 - len(g.cols)
			for i := 0; i < grow; i++ {
				g.cols = append(g.cols, gridCol{pct: -1})
			}
			for ri := range occupied {
				for len(occupied[ri]) < len(g.cols) {
					occupied[ri] = append(occupied[ri], false)
				}
			}
		}
	}
	for ri, rb := range visualRows {
		gr := &gridRow{box: rb}
		col := 0
		for _, cb := range rb.Children {
			if cb.Display \!= cssbox.DisplayTableCell {
				continue
			}
			// advance past occupied slots
			ensure(ri, col)
			for col < len(g.cols) && occupied[ri][col] {
				col++
				ensure(ri, col)
			}
			cs := cb.ColSpan
			if cs < 1 {
				cs = 1
			}
			rs := cb.RowSpan
			if rs < 1 {
				rs = 1
			}
			gc := &gridCell{box: cb, row: ri, col: col, colSpan: cs, rowSpan: rs}
			g.cells = append(g.cells, gc)
			gr.cells = append(gr.cells, gc)
			// mark the rectangle occupied
			for dr := 0; dr < rs; dr++ {
				for dc := 0; dc < cs; dc++ {
					ensure(ri+dr, col+dc)
					occupied[ri+dr][col+dc] = true
				}
			}
			col += cs
		}
		g.rows = append(g.rows, gr)
	}

	// 3. Clamp each cell's spans to the final grid extent.
	for _, gc := range g.cells {
		if gc.col+gc.colSpan > len(g.cols) {
			gc.colSpan = len(g.cols) - gc.col
			if gc.colSpan < 1 {
				gc.colSpan = 1
			}
		}
		if gc.row+gc.rowSpan > len(g.rows) {
			gc.rowSpan = len(g.rows) - gc.row
			if gc.rowSpan < 1 {
				gc.rowSpan = 1
			}
		}
	}
	return g
}

// addColumnHint appends one column carrying cb's width hint (a <col> width or pct).
func (g *tableGrid) addColumnHint(cb *cssbox.Box) {
	g.addColumnHintN(cb, spanOf(cb))
}

// addColumnHintN appends n columns carrying cb's width hint.
func (g *tableGrid) addColumnHintN(cb *cssbox.Box, n int) {
	for i := 0; i < n; i++ {
		col := gridCol{pct: -1}
		// A column width hint (px) — percentage handled in the width solve.
		if w, ok := specifiedContentWidth(cb); ok {
			col.hasWidth = true
			col.width = w
		}
		g.cols = append(g.cols, col)
	}
}

// spanOf reads a <col>/<colgroup> span (ColSpan field, ≥1).
func spanOf(cb *cssbox.Box) int {
	if cb.ColSpan < 1 {
		return 1
	}
	return cb.ColSpan
}
```

(The grid types — `tableGrid`, `gridRow`, `gridCol`, `gridCell` — are each defined exactly once in `table.go`, as shown above. Later tasks add the layout-phase methods/functions onto these types; do not redeclare the structs.)

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestGrid -v`
Expected: PASS.

- [ ] **Step 5: Build + vet + lint + commit**

Run (sandbox disabled):
```bash
go build ./... && go test ./pkg/layout/css/... -count=1
gofmt -l pkg/layout/css/
golangci-lint run ./pkg/layout/css/...
git add pkg/layout/css/table.go pkg/layout/css/table_grid_test.go
git commit -m "css/layout: table grid construction (visual-order rows, occupancy scan, span slots)"
```

---

### Task 7: Fixed column widths + cell layout + row heights + emission (FIRST RENDERING)

**Files:**
- Modify: `pkg/layout/css/table.go` (add `layoutTable`, fixed width solve, cell layout, row heights, emission)
- Modify: `pkg/layout/css/block.go` (`establishesNewBFC` for cells; `layoutInterior` `case cssbox.TableFC`)
- Test: `pkg/layout/css/table_layout_test.go`

This task makes a table actually render: solve fixed-layout column widths, lay each cell out, size rows to the tallest cell, position everything, and return the `interior`. Wire it into `layoutInterior`. Auto layout, spanning width/height, vertical-align, captions, and collapse come in later tasks (use top-align + equal/leftover split + a single-cell-per-slot assumption refined later).

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/table_layout_test.go`. It lays out a table via the engine and asserts fragment geometry (mirror `overflow_layout_test.go`'s pattern — grep it for how to build an Engine, lay out a tree, and walk fragments by `DebugTag` or structure):

```go
package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// styledCell builds a table-cell with a fixed width+height and a text leaf, so row
// height and column width are deterministic without depending on font metrics.
func fixedCell(wPx, hPx float64) *cssbox.Box {
	st := gcss.ComputedStyle{
		Width:  gcss.Length{Value: wPx, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: hPx, Unit: gcss.UnitPx},
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
		Formatting: cssbox.BlockFC, Style: st}
}

func TestFixedTableTwoByTwoGeometry(t *testing.T) {
	// A table-layout:fixed table, 2 columns x 2 rows, each cell 50x30, no spacing.
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{TableLayout: "fixed", BorderCollapse: "separate"}
		tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable,
			Formatting: cssbox.TableFC, Style: st}
		rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC}
		for r := 0; r < 2; r++ {
			row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC}
			row.Children = []*cssbox.Box{fixedCell(50, 30), fixedCell(50, 30)}
			rg.Children = append(rg.Children, row)
		}
		tbl.Children = []*cssbox.Box{rg}
		return tbl
	}
	e := New(nil, nil, nil)
	// Wrap the table in a body block so it lays out in a 200px viewport.
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{mk()}}
	frag := e.layoutTree(context.Background(), body, 200)
	if frag == nil {
		t.Fatal("nil fragment")
	}
	// Collect cell fragments (the table's grandchildren that carry a 50x30 border box).
	var cells []*Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 50 && f.H == 30 {
			cells = append(cells, f)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if len(cells) \!= 4 {
		t.Fatalf("want 4 cell fragments 50x30, got %d", len(cells))
	}
	// The table content width should be ~100 (2*50, no spacing). Find the table frag.
	// Row 1 cells sit at y=0, row 2 at y=30; columns at x=0 and x=50 (relative to table).
	// Assert the distinct X/Y positions seen.
	xs := map[float64]bool{}
	ys := map[float64]bool{}
	for _, c := range cells {
		xs[c.X] = true
		ys[c.Y] = true
	}
	if len(xs) \!= 2 || len(ys) \!= 2 {
		t.Fatalf("want 2 distinct column Xs and 2 row Ys; got xs=%v ys=%v", xs, ys)
	}
}

func TestTableRowHeightIsTallestCell(t *testing.T) {
	st := gcss.ComputedStyle{TableLayout: "fixed"}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{fixedCell(40, 20), fixedCell(40, 50)}} // tallest = 50
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: st, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	// Both cells' border boxes are stretched to the 50px row height.
	var heights []float64
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 40 {
			heights = append(heights, f.H)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if len(heights) \!= 2 {
		t.Fatalf("want 2 cells width 40, got %d", len(heights))
	}
	for _, h := range heights {
		if h \!= 50 {
			t.Errorf("cell height should stretch to the 50px row; got %v", h)
		}
	}
}
```

Adjust the `New(...)` call and `layoutTree` usage to the real signatures (grep `block.go` / an existing `*_layout_test.go` — `New(faces, loader, logf)` and `e.layoutTree(ctx, root, viewportW)` are correct per the engine, but confirm the test package can call unexported `layoutTree`; these tests are in `package css`, so they can).

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run 'TestFixedTable|TestTableRowHeight' -v`
Expected: FAIL — `layoutInterior` still falls back to block; cells won't be 50x30 in a grid, and the X/Y distinctness/height assertions fail.

- [ ] **Step 3: establishesNewBFC for a cell**

In `pkg/layout/css/block.go` `establishesNewBFC`, add the cell as a BFC establisher:

```go
func establishesNewBFC(b *cssbox.Box) bool {
	if b.Position == cssbox.PosAbsolute || b.Position == cssbox.PosFixed {
		return true
	}
	if b.Display == cssbox.DisplayTableCell {
		return true // a table cell establishes a BFC (isolates its margins/floats)
	}
	return b.Display == cssbox.DisplayInlineBlock || b.Float \!= cssbox.FloatNone || clips(b)
}
```

- [ ] **Step 4: Wire layoutTable into layoutInterior**

In `pkg/layout/css/block.go` `layoutInterior`, change the `switch b.Formatting` to add the table case (before the `default`):

```go
	case cssbox.TableFC:
		in = e.layoutTable(ctx, b, contentW, contentX, childBand, childFC)
```

(The table establishes its own BFC handling internally and does not need posCtx/posCB threading for this slice's scope — positioned descendants inside a cell are handled by the cell's own `layoutBlock`, which receives a fresh context. If a later step needs posCtx, thread it then.)

- [ ] **Step 5: Implement layoutTable + fixed solve + cell layout + rows + emission**

Append to `pkg/layout/css/table.go`. `contentW` is the table's content-box width (from `layoutInterior`); `contentX` the page-space left of the table content box; `bandOriginY` is the table content top in the BFC frame (used for the local 0-frame the interior returns). Cells/rows are emitted as `Fragment`s positioned in the local content-top-0 frame (like every interior); `layoutBlock` shifts the whole table into page space.

```go
import (
	"context"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// layoutTable is the TableFC entry point (called from layoutInterior). It builds the
// grid, solves column widths, lays out and positions cells/rows, and returns the
// interior (children = row + cell fragments, in the local content-top-0 frame). A
// table establishes a BFC, so leading/trailing margins are 0.
func (e *Engine) layoutTable(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext) interior {
	g := buildGrid(b)
	if g.table.Style.Direction == "rtl" {
		e.logf("css layout: RTL tables not supported; laying out LTR")
	}
	if len(g.rows) == 0 || len(g.cols) == 0 {
		return interior{contentHeight: 0} // empty table: zero-size
	}

	if g.fixed {
		e.solveFixedWidths(g, contentW)
	} else {
		e.solveAutoWidths(ctx, g, contentW) // implemented in Task 8; falls back to fixed-like until then
	}

	// Column x-offsets (left content edge of each column), with border-spacing.
	x := g.spacingH
	for ci := range g.cols {
		g.cols[ci].x = x
		x += g.cols[ci].width + g.spacingH
	}
	tableContentW := x // includes trailing spacing + edges

	// Lay out each cell at its column width, capturing its natural height.
	for _, gc := range g.cells {
		cw := g.cellWidth(gc)
		// Lay the cell out as a block of border-box width cw at a provisional origin;
		// the cell is a BFC. originX/marginTopEdgeY are 0 here (local frame); we move
		// the fragment into place after row heights are known.
		res := e.layoutBlock(ctx, gc.box, cw, 0, 0, 0, &floatContext{cbLeft: 0, cbRight: cw}, &positionedContext{}, posCBOwner{isPage: true})
		gc.frag = res.frag
	}

	// Row natural heights = tallest non-spanning cell originating in the row.
	for _, gr := range g.rows {
		h := 0.0
		for _, gc := range gr.cells {
			if gc.rowSpan == 1 && gc.frag \!= nil && gc.frag.H > h {
				h = gc.frag.H
			}
		}
		gr.height = h
	}
	e.distributeRowspanHeights(g) // Task 9; no-op until implemented

	// Row y-offsets with vertical spacing.
	y := g.spacingV
	for _, gr := range g.rows {
		gr.y = y
		y += gr.height + g.spacingV
	}
	tableContentH := y

	// Position cells: a cell spans columns [col..col+colSpan) and rows
	// [row..row+rowSpan); its border box fills that rectangle. Stretch the cell
	// fragment to the row band height (table cells fill their row).
	var children []*Fragment
	for _, gc := range g.cells {
		if gc.frag == nil {
			continue
		}
		cx := contentX + g.cols[gc.col].x
		cy := gc.rowTop(g)
		cw := g.cellWidth(gc)
		ch := gc.rowBandHeight(g)
		stretchCellFragment(gc.frag, cx, cy, cw, ch)
		children = append(children, gc.frag)
	}

	_ = tableContentW
	return interior{
		children:      children,
		contentHeight: tableContentH,
		leadingMargin: 0,
		trailingMargin: 0,
	}
}

// cellWidth is the border-box width of a cell = sum of its spanned columns' widths +
// the inter-column border-spacing between them.
func (g *tableGrid) cellWidth(gc *gridCell) float64 {
	w := 0.0
	for i := 0; i < gc.colSpan; i++ {
		w += g.cols[gc.col+i].width
	}
	w += float64(gc.colSpan-1) * g.spacingH
	return w
}

// rowTop is a cell's top y (the top of its first spanned row) in the local frame.
func (gc *gridCell) rowTop(g *tableGrid) float64 {
	return g.rows[gc.row].y
}

// rowBandHeight is the total height a cell spans = sum of its rows' heights + the
// inter-row border-spacing between them.
func (gc *gridCell) rowBandHeight(g *tableGrid) float64 {
	h := 0.0
	for i := 0; i < gc.rowSpan; i++ {
		h += g.rows[gc.row+i].height
	}
	h += float64(gc.rowSpan-1) * g.spacingV
	return h
}

// solveFixedWidths implements CSS 17.5.2.1: column widths from the first row's cells
// + <col> hints + the table width, content-independent. Auto columns split the
// leftover table content width equally.
func (e *Engine) solveFixedWidths(g *tableGrid, contentW float64) {
	// table used content width: fill the container (deterministic; see spec).
	used := contentW
	// Seed column widths from the first row's cells (distributing colspan evenly) and
	// from <col> hints already on g.cols.
	if len(g.rows) > 0 {
		for _, gc := range g.rows[0].cells {
			if w, ok := specifiedContentWidth(gc.box); ok {
				per := w / float64(gc.colSpan)
				for i := 0; i < gc.colSpan; i++ {
					if \!g.cols[gc.col+i].hasWidth {
						g.cols[gc.col+i].hasWidth = true
						g.cols[gc.col+i].width = per
					}
				}
			}
		}
	}
	// Sum fixed widths + spacing; split the remainder among auto columns.
	fixedSum := 0.0
	autoCount := 0
	for ci := range g.cols {
		if g.cols[ci].hasWidth {
			fixedSum += g.cols[ci].width
		} else {
			autoCount++
		}
	}
	spacing := g.spacingH * float64(len(g.cols)+1)
	remain := used - fixedSum - spacing
	if remain < 0 {
		remain = 0
	}
	if autoCount > 0 {
		per := remain / float64(autoCount)
		for ci := range g.cols {
			if \!g.cols[ci].hasWidth {
				g.cols[ci].width = per
			}
		}
	} else {
		// No auto columns: if there is leftover, the CSS 17.5.2.1 rule grows the columns
		// proportionally; distribute the remainder across all columns.
		if remain > 0 && fixedSum > 0 {
			for ci := range g.cols {
				g.cols[ci].width += remain * (g.cols[ci].width / fixedSum)
			}
		}
	}
}
```

Add the fragment-positioning helper. **Grep `block.go`/`fragment.go` for an existing fragment-translate helper** (`translateFragment`, `shiftFragment` — there is one, used by the inline atom path). Use it for `stretchCellFragment`:

```go
// stretchCellFragment positions a cell's border-box fragment at (x,y) and stretches
// it to (w,h) — table cells fill their row band. It translates the fragment's whole
// subtree by the delta to (x,y), then sets the cell border box to (w,h). The
// content inside keeps its top-left origin (top-aligned for now; vertical-align in
// Task 10).
func stretchCellFragment(f *Fragment, x, y, w, h float64) {
	translateFragment(f, x-f.X, y-f.Y) // move subtree so f.X,f.Y == x,y
	f.W = w
	f.H = h
}
```

Add stubs for the not-yet-implemented phases so the file builds (filled in later tasks):

```go
// solveAutoWidths is implemented in Task 8. Until then, approximate by treating it
// like fixed layout so a table still renders.
func (e *Engine) solveAutoWidths(ctx context.Context, g *tableGrid, contentW float64) {
	e.solveFixedWidths(g, contentW)
}

// distributeRowspanHeights is implemented in Task 9 (rowspan height distribution).
// Until then it is a no-op (a too-tall rowspan cell overflows its first row band).
func (e *Engine) distributeRowspanHeights(g *tableGrid) {}
```

- [ ] **Step 6: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run 'TestFixedTable|TestTableRowHeight|TestGrid' -v`
Expected: PASS. If `stretchCellFragment`/`translateFragment` names differ, fix to the real helper. If a cell's content height (not the specified 30/50) drives the row, confirm `layoutBlock` honored the cell's fixed `height` — the test cells specify height, so `gc.frag.H` should be 30/50.

- [ ] **Step 7: Byte-identical guard + full suite + race**

Run (sandbox disabled):
```bash
go test ./pkg/layout/... -count=1
go test ./pkg/doctaculous/... -count=1
go test -race ./pkg/layout/css/... -count=1
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
```
Expected: PASS; `git status` prints nothing (no existing fixture is a table).

- [ ] **Step 8: Format, lint, commit**

Run (sandbox disabled):
```bash
gofmt -l pkg/layout/css/
golangci-lint run ./pkg/layout/css/...
git add pkg/layout/css/table.go pkg/layout/css/block.go pkg/layout/css/table_layout_test.go
git commit -m "css/layout: fixed table layout (column widths, cell layout, row heights, emission) wired into layoutInterior"
```

---

### Task 8: Auto column-width solve

**Files:**
- Modify: `pkg/layout/css/table.go` (replace the `solveAutoWidths` stub)
- Test: `pkg/layout/css/table_layout_test.go` (append)

Implement CSS 17.5.2.2: per-column min/max content widths (via Task 5's measurement), table used width, and distribution. Spanning-cell distribution to columns lands in Task 9; this task handles non-spanning cells + the distribution math + percentage columns.

- [ ] **Step 1: Write the failing test**

Append to `pkg/layout/css/table_layout_test.go`:

```go
func TestAutoTableColumnsSizeToContent(t *testing.T) {
	// Two columns: col 0 has narrow text, col 1 has much wider text. Auto layout must
	// give col 1 more width than col 0 (content-driven), and the table must not exceed
	// the viewport.
	mkCell := func(text string) *cssbox.Box {
		st := gcss.ComputedStyle{FontSizePt: 16}
		txt := &cssbox.Box{Kind: cssbox.BoxText, Text: text, Display: cssbox.DisplayInline, Style: st}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
			Formatting: cssbox.InlineFC, Style: st, Children: []*cssbox.Box{txt}}
	}
	st := gcss.ComputedStyle{TableLayout: "auto"}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{mkCell("Hi"), mkCell("A much longer cell of content here")}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: st, Children: []*cssbox.Box{rg}}

	e := New(nil, nil, nil)
	g := buildGrid(tbl)
	e.solveAutoWidths(context.Background(), g, 600)
	if len(g.cols) \!= 2 {
		t.Fatalf("want 2 columns, got %d", len(g.cols))
	}
	if g.cols[1].width <= g.cols[0].width {
		t.Errorf("wider-content column should be wider: col0=%v col1=%v", g.cols[0].width, g.cols[1].width)
	}
	total := g.cols[0].width + g.cols[1].width
	if total > 600+0.5 {
		t.Errorf("table columns (%v) should fit the 600 available width", total)
	}
}

func TestAutoTableSpecifiedWidthPinsColumn(t *testing.T) {
	mkCell := func(w float64) *cssbox.Box {
		st := gcss.ComputedStyle{Width: gcss.Length{Value: w, Unit: gcss.UnitPx}}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
			Formatting: cssbox.BlockFC, Style: st}
	}
	st := gcss.ComputedStyle{TableLayout: "auto"}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{mkCell(120), mkCell(40)}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: st, Children: []*cssbox.Box{rg}}
	e := New(nil, nil, nil)
	g := buildGrid(tbl)
	e.solveAutoWidths(context.Background(), g, 600)
	// With plenty of room, each column should be ~its specified width.
	if g.cols[0].width < 119 || g.cols[0].width > 200 {
		t.Errorf("col0 should be near its 120 spec (with surplus distribution); got %v", g.cols[0].width)
	}
	if g.cols[0].width <= g.cols[1].width {
		t.Errorf("col0 (120 spec) should exceed col1 (40 spec): %v vs %v", g.cols[0].width, g.cols[1].width)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestAutoTable -v`
Expected: with the Task 7 stub (`solveAutoWidths` → `solveFixedWidths`), the content-driven assertion fails (fixed split is equal, so col1 == col0).

- [ ] **Step 3: Replace the solveAutoWidths stub**

In `pkg/layout/css/table.go`, replace the stub with the real algorithm:

```go
// solveAutoWidths implements CSS 17.5.2.2 (automatic table layout): per-column
// min/max content widths, the table used width, and distribution of the used width
// across columns between their min and max. Percentage columns take their share
// first. Spanning-cell contributions are added in distributeSpanWidths (Task 9).
func (e *Engine) solveAutoWidths(ctx context.Context, g *tableGrid, contentW float64) {
	// 1. Per-column min/max from non-spanning cells.
	for ci := range g.cols {
		g.cols[ci].min = 0
		g.cols[ci].max = 0
	}
	for _, gc := range g.cells {
		if gc.colSpan \!= 1 {
			continue
		}
		mn := e.measureMinContent(ctx, gc.box) + horizontalEdges(gc.box)
		mx := e.measureMaxContent(ctx, gc.box) + horizontalEdges(gc.box)
		if w, ok := specifiedContentWidth(gc.box); ok {
			ew := w + horizontalEdges(gc.box)
			if ew > mn {
				mn = ew
			}
			mx = ew
			if mx < mn {
				mx = mn
			}
		}
		col := &g.cols[gc.col]
		if mn > col.min {
			col.min = mn
		}
		if mx > col.max {
			col.max = mx
		}
		// a <col> width hint raises the column toward it
		if col.hasWidth && col.width > col.min {
			col.min = col.width
			if col.max < col.min {
				col.max = col.min
			}
		}
	}
	e.distributeSpanWidths(ctx, g) // Task 9; no-op until then

	// Ensure max >= min on every column.
	for ci := range g.cols {
		if g.cols[ci].max < g.cols[ci].min {
			g.cols[ci].max = g.cols[ci].min
		}
	}

	// 2. Table used content width.
	spacing := g.spacingH * float64(len(g.cols)+1)
	sumMin, sumMax := 0.0, 0.0
	for ci := range g.cols {
		sumMin += g.cols[ci].min
		sumMax += g.cols[ci].max
	}
	avail := contentW - spacing
	if avail < 0 {
		avail = 0
	}
	var used float64
	if w, ok := specifiedContentWidth(g.table); ok {
		used = w - spacing
	} else {
		used = sumMax
		if used > avail {
			used = avail
		}
	}
	if used < sumMin {
		used = sumMin // table overflows rather than shrinking below content minimums
	}

	// 3. Distribute `used` across columns: start at min, hand out the surplus in
	// proportion to (max - min).
	if used <= sumMin || sumMax == sumMin {
		for ci := range g.cols {
			g.cols[ci].width = g.cols[ci].min
		}
		// If used > sumMin but all columns are rigid (sumMax==sumMin), spread the extra
		// evenly so the table reaches `used`.
		if used > sumMin && len(g.cols) > 0 {
			extra := (used - sumMin) / float64(len(g.cols))
			for ci := range g.cols {
				g.cols[ci].width += extra
			}
		}
		return
	}
	surplus := used - sumMin
	flex := sumMax - sumMin
	for ci := range g.cols {
		span := g.cols[ci].max - g.cols[ci].min
		g.cols[ci].width = g.cols[ci].min + surplus*(span/flex)
	}
}
```

Add the span-distribution stub (Task 9 fills it):

```go
// distributeSpanWidths adds spanning cells' min/max contributions to the columns
// they cross (CSS 17.5.2.2). Implemented in Task 9; until then spanning cells do not
// influence column widths (they are laid out at the summed column widths regardless).
func (e *Engine) distributeSpanWidths(ctx context.Context, g *tableGrid) {}
```

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run 'TestAutoTable|TestFixedTable|TestTableRowHeight|TestGrid' -v`
Expected: PASS.

- [ ] **Step 5: Byte-identical guard + suite, format, lint, commit**

Run (sandbox disabled):
```bash
go test ./pkg/layout/... ./pkg/doctaculous/... -count=1
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
gofmt -l pkg/layout/css/
golangci-lint run ./pkg/layout/css/...
git add pkg/layout/css/table.go pkg/layout/css/table_layout_test.go
git commit -m "css/layout: auto table column-width solve (CSS 17.5.2.2 min/max distribution)"
```
Expected: green; `git status` empty.

---

### Task 8b: Percentage column widths

**Files:**
- Modify: `pkg/layout/css/table.go` (read `%` onto `gridCol.pct`; apply in `solveAutoWidths` and `solveFixedWidths`)
- Test: `pkg/layout/css/table_layout_test.go` (append)

The spec puts percentage column widths in scope against BOTH a fixed and an auto table width. A `%` width on a cell (or `<col>`) makes its column claim that percentage of the table content width; the remaining columns share the rest by their min/max distribution. Implement the tractable, well-defined version: each `%` column reserves `pct/100 × tableContentWidth` (clamped to ≥ its min-content), then non-`%` columns distribute the remainder.

- [ ] **Step 1: Write the failing test**

Append to `pkg/layout/css/table_layout_test.go`:

```go
func TestPercentColumnTakesShare(t *testing.T) {
	// col 0: width 25%; col 1: auto with short content. In a 400-wide table, col 0 ≈ 100.
	pctCell := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 25, Unit: gcss.UnitPercent}}}
	autoCell := func() *cssbox.Box {
		st := gcss.ComputedStyle{FontSizePt: 16}
		txt := &cssbox.Box{Kind: cssbox.BoxText, Text: "x", Display: cssbox.DisplayInline, Style: st}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.InlineFC,
			Style: st, Children: []*cssbox.Box{txt}}
	}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{pctCell, autoCell()}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	// A fixed-width table so the percentage basis is unambiguous (400px).
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "auto", Width: gcss.Length{Value: 400, Unit: gcss.UnitPx}},
		Children: []*cssbox.Box{rg}}
	e := New(nil, nil, nil)
	g := buildGrid(tbl)
	e.solveAutoWidths(context.Background(), g, 400)
	// col 0 should be ~100 (25% of 400); allow tolerance for spacing/min clamps.
	if g.cols[0].width < 90 || g.cols[0].width > 110 {
		t.Errorf("25%% column of a 400px table should be ~100; got %v", g.cols[0].width)
	}
	if g.cols[1].width <= 0 {
		t.Errorf("the auto column should still get the remaining width; got %v", g.cols[1].width)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestPercentColumn -v`
Expected: FAIL — `pct` is never read or applied; col 0 is sized by content (~one glyph), not 25%.

- [ ] **Step 3: Read the percentage onto the column**

In `pkg/layout/css/table.go`, add a helper to read a `%` width and call it where columns get their hints. Add:

```go
// pctWidthOf returns a box's width as a percentage [0..100] and true when its width
// is specified in percent; false otherwise. (specifiedContentWidth rejects percent,
// so the percentage basis is read separately here for table columns.)
func pctWidthOf(b *cssbox.Box) (float64, bool) {
	w := b.Style.Width
	if w.Unit == gcss.UnitPercent {
		return w.Value, true
	}
	return 0, false
}
```

Ensure `table.go` imports `gcss "github.com/nathanstitt/doctaculous/pkg/css"` (add if missing). In `solveAutoWidths`, in the per-column non-spanning loop, after computing min/max, capture a cell's percentage onto its column:

```go
		if pct, ok := pctWidthOf(gc.box); ok && pct > g.cols[gc.col].pct {
			g.cols[gc.col].pct = pct
		}
```

Also capture a `<col>` percentage in the grid builder's `addColumnHintN` (in `table.go`): after the `specifiedContentWidth` block, add:

```go
		if pct, ok := pctWidthOf(cb); ok {
			col.pct = pct
		}
```

(`gridCol.pct` is initialized to `-1` meaning "none"; a real percentage is ≥ 0.)

- [ ] **Step 4: Apply percentage columns in the distribution**

In `solveAutoWidths`, BEFORE the final min/max distribution (right after `used` is finalized and before "3. Distribute `used`"), reserve the percentage columns and shrink the pool for the rest:

```go
	// Percentage columns reserve their share of the used width (clamped to >= min).
	// The remaining (non-percentage) columns distribute the leftover by min/max below.
	pctReserved := 0.0
	pctCols := 0
	for ci := range g.cols {
		if g.cols[ci].pct >= 0 {
			want := used * g.cols[ci].pct / 100
			if want < g.cols[ci].min {
				want = g.cols[ci].min
			}
			g.cols[ci].width = want
			pctReserved += want
			pctCols++
		}
	}
	if pctCols > 0 {
		// Re-run the min/max distribution over only the NON-percentage columns against
		// the leftover width.
		leftover := used - pctReserved
		if leftover < 0 {
			leftover = 0
		}
		nMin, nMax := 0.0, 0.0
		for ci := range g.cols {
			if g.cols[ci].pct < 0 {
				nMin += g.cols[ci].min
				nMax += g.cols[ci].max
			}
		}
		if leftover <= nMin || nMax == nMin {
			for ci := range g.cols {
				if g.cols[ci].pct < 0 {
					g.cols[ci].width = g.cols[ci].min
				}
			}
		} else {
			surplus := leftover - nMin
			flex := nMax - nMin
			for ci := range g.cols {
				if g.cols[ci].pct < 0 {
					span := g.cols[ci].max - g.cols[ci].min
					g.cols[ci].width = g.cols[ci].min + surplus*(span/flex)
				}
			}
		}
		return // percentage path is complete; skip the all-columns distribution
	}
```

(When there are no percentage columns this block is a no-op and the existing all-columns distribution runs unchanged — so the Task 8 tests still pass.) For `solveFixedWidths`, add an analogous percentage reservation: a `%` column gets `used × pct/100` before the equal split of the remainder. Insert in `solveFixedWidths` after seeding from the first row and before summing fixed widths:

```go
	for ci := range g.cols {
		if g.cols[ci].pct >= 0 && !g.cols[ci].hasWidth {
			g.cols[ci].hasWidth = true
			g.cols[ci].width = used * g.cols[ci].pct / 100
		}
	}
```

(But `solveFixedWidths` reads `pct` only if the grid builder set it from a `<col>`; a first-row cell `%` is captured by reading `pctWidthOf` in the seeding loop — add that read in the first-row seeding loop too: when a cell has a percentage, set the column `pct`.)

- [ ] **Step 5: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run 'TestPercentColumn|TestAutoTable|TestFixedTable|TestGrid' -v`
Expected: PASS (the new percentage test passes; the existing auto/fixed tests, which use no percentages, are unchanged).

- [ ] **Step 6: Byte-identical guard + suite, format, lint, commit**

Run (sandbox disabled):
```bash
go test ./pkg/layout/... ./pkg/doctaculous/... -count=1
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
gofmt -l pkg/layout/css/
golangci-lint run ./pkg/layout/css/...
git add pkg/layout/css/table.go pkg/layout/css/table_layout_test.go
git commit -m "css/layout: percentage column widths (against fixed + auto table width)"
```

---

### Task 9: Spanning — colspan width distribution + rowspan height distribution

**Files:**
- Modify: `pkg/layout/css/table.go` (`distributeSpanWidths`, `distributeRowspanHeights`)
- Test: `pkg/layout/css/table_layout_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `pkg/layout/css/table_layout_test.go`:

```go
func TestColspanRaisesSpannedColumns(t *testing.T) {
	// Row 0: a single wide cell spanning 2 columns (specified width 200).
	// Row 1: two narrow cells (spec 30 each).
	// Auto layout: the colspan-2 cell's 200 must push the two columns up so they sum
	// to at least ~200, not stay at 30+30.
	wide := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		ColSpan: 2, Style: gcss.ComputedStyle{Width: gcss.Length{Value: 200, Unit: gcss.UnitPx}}}
	narrow := func() *cssbox.Box {
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
			Style: gcss.ComputedStyle{Width: gcss.Length{Value: 30, Unit: gcss.UnitPx}}}
	}
	r0 := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{wide}}
	r1 := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{narrow(), narrow()}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{r0, r1}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "auto"}, Children: []*cssbox.Box{rg}}
	e := New(nil, nil, nil)
	g := buildGrid(tbl)
	e.solveAutoWidths(context.Background(), g, 600)
	sum := g.cols[0].width + g.cols[1].width
	if sum < 190 {
		t.Errorf("colspan-2 width 200 should raise the two columns to ~200; got sum %v", sum)
	}
}

func TestRowspanDistributesExcessHeight(t *testing.T) {
	// Col 0: a rowspan-2 cell of fixed height 100. Col 1: two cells of height 20 each.
	// The two rows must grow so they sum (with spacing 0) to >= 100.
	rs := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		RowSpan: 2, Style: gcss.ComputedStyle{Width: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			Height: gcss.Length{Value: 100, Unit: gcss.UnitPx}}}
	small := func() *cssbox.Box {
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
			Style: gcss.ComputedStyle{Width: gcss.Length{Value: 40, Unit: gcss.UnitPx},
				Height: gcss.Length{Value: 20, Unit: gcss.UnitPx}}}
	}
	r0 := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{rs, small()}}
	r1 := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{small()}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{r0, r1}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed"}, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	// The rowspan cell's border box must be 100 tall (it fills both rows).
	var found bool
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 40 && f.H == 100 {
			found = true
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if \!found {
		t.Errorf("rowspan-2 cell should fill a 100-tall band across both grown rows")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run 'TestColspanRaises|TestRowspanDistrib' -v`
Expected: FAIL — span stubs are no-ops, so columns stay at 30+30 and rows stay at 20+20.

- [ ] **Step 3: Implement distributeSpanWidths**

In `pkg/layout/css/table.go`, replace the `distributeSpanWidths` stub:

```go
// distributeSpanWidths adds spanning cells' min/max to the columns they cross (CSS
// 17.5.2.2): if a span's min/max exceeds the sum of its columns' current min/max,
// the excess is distributed across them (in proportion to each column's current
// max-min, or evenly if all equal). Inter-column border-spacing the span covers is
// excluded from its contribution.
func (e *Engine) distributeSpanWidths(ctx context.Context, g *tableGrid) {
	for _, gc := range g.cells {
		if gc.colSpan == 1 {
			continue
		}
		mn := e.measureMinContent(ctx, gc.box) + horizontalEdges(gc.box)
		mx := e.measureMaxContent(ctx, gc.box) + horizontalEdges(gc.box)
		if w, ok := specifiedContentWidth(gc.box); ok {
			ew := w + horizontalEdges(gc.box)
			if ew > mn {
				mn = ew
			}
			mx = ew
		}
		innerSpacing := float64(gc.colSpan-1) * g.spacingH
		mn -= innerSpacing
		mx -= innerSpacing
		if mn < 0 {
			mn = 0
		}
		if mx < mn {
			mx = mn
		}
		distributeExcess(g, gc.col, gc.colSpan, mn, false)
		distributeExcess(g, gc.col, gc.colSpan, mx, true)
	}
}

// distributeExcess raises the columns [col..col+span) so their summed min (or max,
// when toMax) is at least target, distributing the shortfall in proportion to each
// column's current max-min headroom (evenly if all zero).
func distributeExcess(g *tableGrid, col, span int, target float64, toMax bool) {
	cur := 0.0
	for i := 0; i < span; i++ {
		if toMax {
			cur += g.cols[col+i].max
		} else {
			cur += g.cols[col+i].min
		}
	}
	if cur >= target {
		return
	}
	short := target - cur
	headroom := 0.0
	for i := 0; i < span; i++ {
		headroom += g.cols[col+i].max - g.cols[col+i].min
	}
	for i := 0; i < span; i++ {
		var share float64
		if headroom > 0 {
			share = short * ((g.cols[col+i].max - g.cols[col+i].min) / headroom)
		} else {
			share = short / float64(span)
		}
		if toMax {
			g.cols[col+i].max += share
		} else {
			g.cols[col+i].min += share
			if g.cols[col+i].max < g.cols[col+i].min {
				g.cols[col+i].max = g.cols[col+i].min
			}
		}
	}
}
```

- [ ] **Step 4: Implement distributeRowspanHeights**

In `pkg/layout/css/table.go`, replace the `distributeRowspanHeights` stub:

```go
// distributeRowspanHeights grows the rows a rowspan cell covers so their summed
// height (plus the inter-row border-spacing) is at least the cell's border-box
// height (CSS — a spanning cell's height is distributed across its rows). The excess
// is split in proportion to the rows' current heights, or evenly if all zero. One
// top-to-bottom pass (deterministic; sufficient for the common case).
func (e *Engine) distributeRowspanHeights(g *tableGrid) {
	for _, gc := range g.cells {
		if gc.rowSpan == 1 || gc.frag == nil {
			continue
		}
		cur := 0.0
		for i := 0; i < gc.rowSpan; i++ {
			cur += g.rows[gc.row+i].height
		}
		cur += float64(gc.rowSpan-1) * g.spacingV
		need := gc.frag.H
		if need <= cur {
			continue
		}
		short := need - cur
		// proportional to current heights
		sum := 0.0
		for i := 0; i < gc.rowSpan; i++ {
			sum += g.rows[gc.row+i].height
		}
		for i := 0; i < gc.rowSpan; i++ {
			var share float64
			if sum > 0 {
				share = short * (g.rows[gc.row+i].height / sum)
			} else {
				share = short / float64(gc.rowSpan)
			}
			g.rows[gc.row+i].height += share
		}
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run 'TestColspanRaises|TestRowspanDistrib|TestAutoTable|TestFixedTable|TestGrid' -v`
Expected: PASS.

- [ ] **Step 6: Byte-identical guard + suite + race, format, lint, commit**

Run (sandbox disabled):
```bash
go test ./pkg/layout/... ./pkg/doctaculous/... -count=1
go test -race ./pkg/layout/css/... -count=1
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
gofmt -l pkg/layout/css/
golangci-lint run ./pkg/layout/css/...
git add pkg/layout/css/table.go pkg/layout/css/table_layout_test.go
git commit -m "css/layout: span support — colspan width distribution + rowspan height distribution"
```

---

### Task 10: vertical-align

**Files:**
- Modify: `pkg/layout/css/table.go` (apply vertical-align when positioning cells)
- Test: `pkg/layout/css/table_layout_test.go` (append)

A cell's border box fills its row band (already true). `vertical-align` shifts the cell's CONTENT within that band: top (default), bottom, middle, baseline. For this slice, shift the cell content fragment's children/lines down by the computed offset; baseline aligns row cells on a shared first baseline (max ascent), non-baseline cells top-align.

- [ ] **Step 1: Write the failing test**

Append to `pkg/layout/css/table_layout_test.go`:

```go
func TestVerticalAlignBottomShiftsContent(t *testing.T) {
	// A 60-tall row (forced by a tall sibling). A short cell with vertical-align:bottom
	// must have its inner content fragment near the bottom of the band, not the top.
	tall := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			Height: gcss.Length{Value: 60, Unit: gcss.UnitPx}}}
	// short content: an inner block of height 10, vertical-align bottom.
	innerSt := gcss.ComputedStyle{Height: gcss.Length{Value: 10, Unit: gcss.UnitPx},
		Width: gcss.Length{Value: 20, Unit: gcss.UnitPx}}
	inner := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: innerSt}
	short := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 40, Unit: gcss.UnitPx}, VerticalAlign: "bottom"},
		Children: []*cssbox.Box{inner}}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{tall, short}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed"}, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	// Find the inner 20x10 fragment; its top within the table should be near 50 (60-10),
	// not 0. We assert its Y relative to the short cell's top.
	var inner10, shortCell *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 20 && f.H == 10 {
			inner10 = f
		}
		if f.W == 40 && f.H == 60 {
			// the tall cell, same band height as the short cell
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	// also locate the short cell (W==40, H==60, has a child)
	var walk2 func(f *Fragment)
	walk2 = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 40 && f.H == 60 && len(f.Children) > 0 {
			shortCell = f
		}
		for _, c := range f.Children {
			walk2(c)
		}
	}
	walk2(frag)
	if inner10 == nil || shortCell == nil {
		t.Fatalf("missing fragments inner=%v cell=%v", inner10, shortCell)
	}
	offset := inner10.Y - shortCell.Y
	if offset < 40 {
		t.Errorf("vertical-align:bottom should push 10-tall content near the band bottom (~50); got offset %v", offset)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestVerticalAlignBottom -v`
Expected: FAIL — content is top-aligned (offset ~0).

- [ ] **Step 3: Apply vertical-align in cell positioning**

In `pkg/layout/css/table.go` `layoutTable`, after `stretchCellFragment(gc.frag, cx, cy, cw, ch)` and before appending, apply the content shift. The cell fragment was laid out at its natural content height `natH`; capture it BEFORE stretching. Adjust the cell-layout + positioning loops:

In the cell-layout loop (where `gc.frag = res.frag`), record the natural height:

```go
	type cellNat struct{ natH float64 }
	nat := map[*gridCell]float64{}
	for _, gc := range g.cells {
		cw := g.cellWidth(gc)
		res := e.layoutBlock(ctx, gc.box, cw, 0, 0, 0, &floatContext{cbLeft: 0, cbRight: cw}, &positionedContext{}, posCBOwner{isPage: true})
		gc.frag = res.frag
		if gc.frag \!= nil {
			nat[gc] = gc.frag.H
		}
	}
```

In the positioning loop, compute the vertical offset and shift the cell's CONTENT (its children + lines) — NOT the border box (which fills the band):

```go
	for _, gc := range g.cells {
		if gc.frag == nil {
			continue
		}
		cx := contentX + g.cols[gc.col].x
		cy := gc.rowTop(g)
		cw := g.cellWidth(gc)
		ch := gc.rowBandHeight(g)
		natH := nat[gc]
		stretchCellFragment(gc.frag, cx, cy, cw, ch)
		applyCellVAlign(gc.frag, natH, ch)
		children = append(children, gc.frag)
	}
```

Add the helper (baseline falls back to top for now; per-row shared-baseline is a documented follow-up unless the row's cells are all single-line — keep it simple and correct: top for baseline/top, centered for middle, bottom for bottom):

```go
// applyCellVAlign shifts a cell's content down within its row band per
// vertical-align. natH is the content's natural (pre-stretch) height; bandH the row
// band height. top/baseline keep the content at the band top; middle centers it;
// bottom drops it to the band bottom. The shift moves the fragment's children and
// inline lines, leaving the border box (which fills the band) in place.
func applyCellVAlign(f *Fragment, natH, bandH float64) {
	va := "baseline"
	if f.Box \!= nil {
		va = f.Box.Style.VerticalAlign
	}
	var dy float64
	switch va {
	case "bottom":
		dy = bandH - natH
	case "middle":
		dy = (bandH - natH) / 2
	default:
		dy = 0 // top, baseline (single-line baseline ≈ top here), sub/super/text-* -> top
	}
	if dy <= 0 {
		return
	}
	shiftCellContent(f, dy)
}

// shiftCellContent translates a cell fragment's content (children + inline lines)
// down by dy, without moving the cell's own border box.
func shiftCellContent(f *Fragment, dy float64) {
	for _, c := range f.Children {
		translateFragment(c, 0, dy)
	}
	for i := range f.Lines {
		f.Lines[i].BaselineY += dy
	}
}
```

Confirm `LineFragment` has a `BaselineY` field (it does per `inline.go`); if the line type differs, shift whatever Y field it carries. Confirm `Fragment.Box` is set for the cell — `layoutBlock` sets `frag.Box`? Grep: if `layoutBlock` does NOT set `.Box`, set it on the cell fragment in `layoutTable` (`gc.frag.Box = gc.box`) right after layout so `applyCellVAlign` can read the style. (Setting `.Box` is safe and matches how other fragments carry their source box.)

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run 'TestVerticalAlign|TestFixedTable|TestTableRowHeight' -v`
Expected: PASS.

- [ ] **Step 5: Byte-identical guard + suite, format, lint, commit**

Run (sandbox disabled):
```bash
go test ./pkg/layout/... ./pkg/doctaculous/... -count=1
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
gofmt -l pkg/layout/css/
golangci-lint run ./pkg/layout/css/...
git add pkg/layout/css/table.go pkg/layout/css/table_layout_test.go
git commit -m "css/layout: table-cell vertical-align (top/middle/bottom; baseline≈top)"
```

---

### Task 11: Captions

**Files:**
- Modify: `pkg/layout/css/table.go` (lay out + place the caption)
- Test: `pkg/layout/css/table_layout_test.go` (append)

A `<caption>` lays out as a block at the table width; `caption-side:top` places it above the grid (grid shifts down by the caption height), `bottom` below. Caption height is part of the table interior height.

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestCaptionTopShiftsGridDown(t *testing.T) {
	capSt := gcss.ComputedStyle{Height: gcss.Length{Value: 24, Unit: gcss.UnitPx}}
	caption := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCaption,
		Formatting: cssbox.BlockFC, Style: capSt}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{fixedCell(50, 30)}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed", CaptionSide: "top"},
		Children: []*cssbox.Box{caption, rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	// The cell (50x30) must sit at y >= 24 (below the caption).
	var cellY float64 = -1
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 50 && f.H == 30 {
			cellY = f.Y
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if cellY < 24 {
		t.Errorf("caption-side:top should push the grid below the 24px caption; cell Y=%v", cellY)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestCaptionTop -v`
Expected: FAIL — caption is currently ignored (the grid builder stores `g.caption` but `layoutTable` never lays it out); cell Y is ~0.

- [ ] **Step 3: Lay out + place the caption**

In `pkg/layout/css/table.go` `layoutTable`, after computing column widths/`tableContentW` and BEFORE positioning cells, lay out the caption and compute a `gridDY` (vertical offset applied to all rows):

```go
	// Caption: a block laid out at the table content width, placed above (top) or below.
	var captionFrag *Fragment
	captionH := 0.0
	if g.caption \!= nil {
		res := e.layoutBlock(ctx, g.caption, tableContentW, contentX, 0, 0,
			&floatContext{cbLeft: contentX, cbRight: contentX + tableContentW}, &positionedContext{}, posCBOwner{isPage: true})
		captionFrag = res.frag
		if captionFrag \!= nil {
			captionH = captionFrag.H
		}
	}
	gridDY := 0.0
	if g.caption \!= nil && g.table.Style.CaptionSide \!= "bottom" {
		gridDY = captionH // caption on top: shift the grid down
	}
```

Apply `gridDY` to each row's `y` when positioning cells: change the cell-position `cy` computation to add `gridDY`:

```go
		cy := gc.rowTop(g) + gridDY
```

(rowspan band height is unaffected; only the top offset moves.) After positioning cells, place the caption fragment and extend the interior height:

```go
	gridBottom := tableContentH + gridDY
	if captionFrag \!= nil {
		if g.table.Style.CaptionSide == "bottom" {
			translateFragment(captionFrag, 0, gridBottom-captionFrag.Y) // just below the grid
			children = append(children, captionFrag)
			gridBottom += captionH
		} else {
			translateFragment(captionFrag, 0, 0-captionFrag.Y) // at the very top (local Y 0)
			children = append(children, captionFrag)
		}
	}
```

Change the returned `contentHeight` from `tableContentH` to `gridBottom`.

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run 'TestCaption|TestFixedTable' -v`
Expected: PASS.

- [ ] **Step 5: Byte-identical guard + suite, format, lint, commit**

Run (sandbox disabled):
```bash
go test ./pkg/layout/... ./pkg/doctaculous/... -count=1
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
gofmt -l pkg/layout/css/
golangci-lint run ./pkg/layout/css/...
git add pkg/layout/css/table.go pkg/layout/css/table_layout_test.go
git commit -m "css/layout: table captions (caption-side top/bottom)"
```

---

### Task 12: border-collapse: collapse

**Files:**
- Create: `pkg/layout/css/tableborder.go` (conflict resolution + resolved-edge build)
- Modify: `pkg/layout/css/fragment.go` (`CollapsedEdge`, `Collapsed []CollapsedEdge` field, emit in `AppendItems`)
- Modify: `pkg/layout/paint/paint.go` (stroke collapsed edges)
- Modify: `pkg/layout/css/table.go` (call the collapse resolver; suppress per-cell borders in collapse mode)
- Test: `pkg/layout/css/tableborder_test.go`

Implement CSS 17.6.2: for each shared grid edge pick the winning border (hidden > wider > style-rank > cell>row>rowgroup>col>colgroup>table), build a resolved-edge list painted centered on the grid line. **Before coding the precedence, WebFetch the W3C CSS 2.1 §17.6.2.1 text and confirm the exact style ranking and tie order.**

- [ ] **Step 1: Write the failing conflict-resolution test**

Create `pkg/layout/css/tableborder_test.go` (a focused unit test of the precedence function, independent of full layout):

```go
package css

import (
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
)

func be(width float64, style string, c color.RGBA) gcss.BorderEdgeSpec {
	return gcss.BorderEdgeSpec{Width: width, Style: style, Color: c}
}

func TestCollapseHiddenWins(t *testing.T) {
	a := be(2, "solid", color.RGBA{255, 0, 0, 255})
	b := be(10, "solid", color.RGBA{0, 255, 0, 255})
	bHidden := be(10, "hidden", color.RGBA{0, 255, 0, 255})
	got := resolveCollapsedEdge(a, bHidden)
	if got.Style \!= "hidden" && got.Width \!= 0 {
		t.Errorf("hidden must suppress the edge; got %+v", got)
	}
	// without hidden, the wider wins
	got2 := resolveCollapsedEdge(a, b)
	if got2.Width \!= 10 {
		t.Errorf("wider border should win; got width %v", got2.Width)
	}
}

func TestCollapseStyleRankBreaksTie(t *testing.T) {
	solid := be(4, "solid", color.RGBA{1, 1, 1, 255})
	dashed := be(4, "dashed", color.RGBA{2, 2, 2, 255})
	got := resolveCollapsedEdge(dashed, solid)
	if got.Style \!= "solid" {
		t.Errorf("at equal width, double>solid>dashed>dotted: solid should beat dashed; got %q", got.Style)
	}
	double := be(4, "double", color.RGBA{3, 3, 3, 255})
	got2 := resolveCollapsedEdge(solid, double)
	if got2.Style \!= "double" {
		t.Errorf("double should beat solid at equal width; got %q", got2.Style)
	}
}
```

If `pkg/css` has no exported `BorderEdgeSpec`, define a small local edge struct in `tableborder.go` instead and adapt the test to it (the test lives in `package css` of the layout dir and can use an internal type). The key is the precedence semantics, not the exact type name. Prefer a layout-package-local type `collapsedBorder{width float64; style string; color color.RGBA; rank int}` to avoid touching `pkg/css`.

Revised test using a local type (use THIS version):

```go
package css

import (
	"image/color"
	"testing"
)

func cb(width float64, style string, owner edgeOwner) collapsedBorder {
	return collapsedBorder{width: width, style: style, color: color.RGBA{0, 0, 0, 255}, owner: owner}
}

func TestCollapseHiddenWins(t *testing.T) {
	got := resolveCollapsedEdge(cb(2, "solid", ownerCell), cb(10, "hidden", ownerRow))
	if got.style \!= "hidden" {
		t.Errorf("hidden must win and suppress; got %q", got.style)
	}
	got2 := resolveCollapsedEdge(cb(2, "solid", ownerCell), cb(10, "solid", ownerRow))
	if got2.width \!= 10 {
		t.Errorf("wider wins; got %v", got2.width)
	}
}

func TestCollapseStyleRankAndOwnerTie(t *testing.T) {
	if resolveCollapsedEdge(cb(4, "dashed", ownerCell), cb(4, "solid", ownerRow)).style \!= "solid" {
		t.Error("solid should beat dashed at equal width")
	}
	if resolveCollapsedEdge(cb(4, "solid", ownerRow), cb(4, "double", ownerTable)).style \!= "double" {
		t.Error("double should beat solid at equal width")
	}
	// equal width + equal style: the cell-closer owner wins.
	got := resolveCollapsedEdge(cb(4, "solid", ownerCell), cb(4, "solid", ownerTable))
	if got.owner \!= ownerCell {
		t.Error("cell owner should beat table owner at equal width+style")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run TestCollapse -v`
Expected: FAIL — `undefined: resolveCollapsedEdge / collapsedBorder / edgeOwner`.

- [ ] **Step 3: Implement conflict resolution**

Create `pkg/layout/css/tableborder.go`:

```go
package css

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// edgeOwner ranks the box that contributes a border in the collapse conflict-
// resolution tie-break: a border on the element closer to the cell wins (CSS
// 17.6.2.1). Lower value = closer to the cell = wins ties.
type edgeOwner int

const (
	ownerCell edgeOwner = iota
	ownerRow
	ownerRowGroup
	ownerColumn
	ownerColumnGroup
	ownerTable
)

// collapsedBorder is one candidate border for a shared grid edge.
type collapsedBorder struct {
	width float64
	style string // "none"/"hidden"/"solid"/"dashed"/"dotted"/"double"/...
	color color.RGBA
	owner edgeOwner
}

// styleRank ranks border styles for the collapse tie-break (CSS 17.6.2.1): a wider
// border wins first; at equal width a higher style rank wins. double is highest,
// then solid/dashed/dotted/ridge/outset/groove/inset, then none. (hidden is handled
// separately — it suppresses the edge outright.)
func styleRank(style string) int {
	switch style {
	case "double":
		return 8
	case "solid":
		return 7
	case "dashed":
		return 6
	case "dotted":
		return 5
	case "ridge":
		return 4
	case "outset":
		return 3
	case "groove":
		return 2
	case "inset":
		return 1
	default: // "none" and unknown
		return 0
	}
}

// resolveCollapsedEdge picks the winning border between two adjacent candidates for a
// shared edge, per CSS 17.6.2.1: (1) hidden suppresses the edge; (2) wider wins;
// (3) higher style rank wins; (4) the owner closer to the cell wins. A "none" border
// loses to any real border (rank 0).
func resolveCollapsedEdge(a, b collapsedBorder) collapsedBorder {
	if a.style == "hidden" || b.style == "hidden" {
		return collapsedBorder{style: "hidden"} // suppressed (width 0)
	}
	// none loses to anything with width>0
	if a.style == "none" && b.style \!= "none" {
		return b
	}
	if b.style == "none" && a.style \!= "none" {
		return a
	}
	if a.width \!= b.width {
		if a.width > b.width {
			return a
		}
		return b
	}
	if ra, rb := styleRank(a.style), styleRank(b.style); ra \!= rb {
		if ra > rb {
			return a
		}
		return b
	}
	if a.owner <= b.owner {
		return a
	}
	return b
}

// cellBorder reads a box's border on the given side as a collapsedBorder candidate.
// side is layout.EdgeSide (Top/Right/Bottom/Left). owner ranks the box.
func cellBorder(b *cssbox.Box, side int, owner edgeOwner) collapsedBorder {
	w, style, col := borderEdgeOf(b, side) // see helper note
	return collapsedBorder{width: w, style: style, color: col, owner: owner}
}
```

`borderEdgeOf` reads a box's per-side border width/style/color. **Grep `block.go`/`fragment.go` for how a fragment's `Border [4]BorderEdge` is built from `b.Style.BorderTopWidth/Style/Color`** — reuse that resolution (there is a function that maps a box's border into the fragment). If it's inlined, add a small `borderEdgeOf(b, side)` here mirroring it (px-resolve width; pass style/color through; an EdgeSide→Top/Right/Bottom/Left mapping). Keep it consistent with the separate-border path so collapse and separate read the same widths.

- [ ] **Step 4: Add the resolved-edge build for a table (grid edges → CollapsedEdge list)**

Append to `tableborder.go` a function that walks the grid and produces the page-space edge list. It builds, for each cell, its 4 resolved edges against the neighbor cell (or the table) on each side, centered on the grid line:

```go
// buildCollapsedEdges resolves every shared edge of a border-collapse:collapse table
// into a flat list of page-space segments centered on the grid lines. It is called
// after cell positions are known; cellAt maps a (row,col) slot to the originating
// cell (nil for an empty slot). Each cell contributes its top+left edges (resolved
// against the neighbor above/left); the table's right and bottom outer edges are
// added once. Returns segments in the table's local frame (same as cell fragments).
func (g *tableGrid) buildCollapsedEdges(cellAt func(r, c int) *gridCell) []CollapsedEdge {
	var edges []CollapsedEdge
	// Helper: resolve + emit one segment between cell `in` (owner side) and neighbor.
	// For brevity this builds per-cell top and left borders against neighbors and the
	// table edge; the geometry uses each cell's positioned fragment rect.
	for _, gc := range g.cells {
		if gc.frag == nil {
			continue
		}
		x, y, w, h := gc.frag.X, gc.frag.Y, gc.frag.W, gc.frag.H
		// LEFT edge: neighbor is the cell ending at gc.col-1 in gc's first row, else table.
		left := cellBorder(gc.box, edgeLeft, ownerCell)
		if gc.col == 0 {
			left = resolveCollapsedEdge(left, cellBorder(g.table, edgeLeft, ownerTable))
		} else if nb := cellAt(gc.row, gc.col-1); nb \!= nil {
			left = resolveCollapsedEdge(left, cellBorder(nb.box, edgeRight, ownerCell))
		}
		if left.style \!= "hidden" && left.width > 0 {
			edges = append(edges, CollapsedEdge{X1: x, Y1: y, X2: x, Y2: y + h, Edge: toBorderEdge(left)})
		}
		// TOP edge: neighbor is the cell ending at gc.row-1 in gc's first col, else table.
		top := cellBorder(gc.box, edgeTop, ownerCell)
		if gc.row == 0 {
			top = resolveCollapsedEdge(top, cellBorder(g.table, edgeTop, ownerTable))
		} else if nb := cellAt(gc.row-1, gc.col); nb \!= nil {
			top = resolveCollapsedEdge(top, cellBorder(nb.box, edgeBottom, ownerCell))
		}
		if top.style \!= "hidden" && top.width > 0 {
			edges = append(edges, CollapsedEdge{X1: x, Y1: y, X2: x + w, Y2: y, Edge: toBorderEdge(top)})
		}
	}
	// Outer right + bottom edges of the rightmost/bottom cells.
	for _, gc := range g.cells {
		if gc.frag == nil {
			continue
		}
		x, y, w, h := gc.frag.X, gc.frag.Y, gc.frag.W, gc.frag.H
		if gc.col+gc.colSpan == len(g.cols) {
			r := resolveCollapsedEdge(cellBorder(gc.box, edgeRight, ownerCell), cellBorder(g.table, edgeRight, ownerTable))
			if r.style \!= "hidden" && r.width > 0 {
				edges = append(edges, CollapsedEdge{X1: x + w, Y1: y, X2: x + w, Y2: y + h, Edge: toBorderEdge(r)})
			}
		}
		if gc.row+gc.rowSpan == len(g.rows) {
			bm := resolveCollapsedEdge(cellBorder(gc.box, edgeBottom, ownerCell), cellBorder(g.table, edgeBottom, ownerTable))
			if bm.style \!= "hidden" && bm.width > 0 {
				edges = append(edges, CollapsedEdge{X1: x, Y1: y + h, X2: x + w, Y2: y + h, Edge: toBorderEdge(bm)})
			}
		}
	}
	return edges
}
```

Define `edgeTop/edgeRight/edgeBottom/edgeLeft` to match the project's `layout.EdgeSide` constants (grep `pkg/layout` for `EdgeTop`/`EdgeSide`; use those values, e.g. `const (edgeTop = int(layout.EdgeTop); ...)` or import and use directly). `toBorderEdge(collapsedBorder) BorderEdge` converts to the fragment border-edge type used in paint (grep `fragment.go` for `BorderEdge`'s fields — width/style/color — and map them).

- [ ] **Step 5: Add the Fragment field + paint**

In `pkg/layout/css/fragment.go`, add to the `Fragment` struct (after `PositionedInfo`):

```go
	// Collapsed holds the resolved border-collapse:collapse edge segments for a table
	// fragment (nil for every other fragment — so non-collapse pages are byte-identical).
	// Painted centered on the grid lines after the table/cell backgrounds. In the same
	// page-space frame as the fragment's own border box.
	Collapsed []CollapsedEdge
```

Define the type in `fragment.go` (or `tableborder.go` — put it where `BorderEdge` lives so paint can see it):

```go
// CollapsedEdge is one resolved border segment of a border-collapse:collapse table,
// in page space, centered on the grid line. Stroked by the painter.
type CollapsedEdge struct {
	X1, Y1, X2, Y2 float64
	Edge           BorderEdge
}
```

In `AppendItems` (the flatten), emit the collapsed edges for a fragment that has them, AFTER its background/border and children (so edges paint over the cell backgrounds). Grep `AppendItems` for where a fragment emits its own border; add right after the children are appended for this fragment:

```go
	for i := range f.Collapsed {
		ce := f.Collapsed[i]
		out = append(out, layout.Item{ /* a border/line item — match the existing border item kind */ })
	}
```

Use whatever `layout.Item` representation borders already use (grep how `Fragment.Border` becomes `layout.Item`s — collapsed edges are the same kind of stroked line, just free-standing segments). If borders are emitted as a dedicated item kind with a rect+edge, emit each collapsed edge as a 1-segment border. The exact `layout.Item` shape MUST match the existing border-paint path; do not invent a new item kind unless borders lack a segment form — in which case add a minimal `ClipPush`-style new kind + paint it in Step 6.

- [ ] **Step 6: Paint the collapsed edges**

In `pkg/layout/paint/paint.go`, ensure the item kind emitted in Step 5 strokes a line of the edge's width/style/color centered on (X1,Y1)-(X2,Y2). If you reused the existing border item kind, no paint change is needed. If you added a new kind, stroke it here mirroring the existing border-stroke (solid/dashed/dotted/double) code.

- [ ] **Step 7: Wire collapse into layoutTable + suppress per-cell borders**

In `pkg/layout/css/table.go` `layoutTable`, after positioning all cells, if `g.collapse`, build the edge list and attach it to the TABLE fragment. But `layoutTable` returns an `interior`, not the table fragment — the table fragment is built by `layoutBlock` around the interior. So: carry the edges on the interior and have `layoutBlock`/`layoutInterior` attach them, OR (simpler) attach them to a synthetic child. **Simplest correct approach:** add a field to `interior` and have `layoutBlock` copy it onto the box's fragment.

Add to the `interior` struct (`block.go`):

```go
	collapsedEdges []CollapsedEdge // border-collapse:collapse resolved edges (table only)
```

In `layoutBlock`, after the box's fragment `frag` is constructed from `in`, add:

```go
	if len(in.collapsedEdges) > 0 {
		frag.Collapsed = in.collapsedEdges
	}
```

In `layoutTable`, when `g.collapse`, build and set:

```go
	var collapsed []CollapsedEdge
	if g.collapse {
		// cellAt resolves a slot to its originating cell.
		index := map[[2]int]*gridCell{}
		for _, gc := range g.cells {
			index[[2]int{gc.row, gc.col}] = gc
		}
		cellAt := func(r, c int) *gridCell {
			// find the cell whose origin rectangle covers (r,c)
			for _, gc := range g.cells {
				if r >= gc.row && r < gc.row+gc.rowSpan && c >= gc.col && c < gc.col+gc.colSpan {
					return gc
				}
			}
			return index[[2]int{r, c}]
		}
		collapsed = g.buildCollapsedEdges(cellAt)
		// suppress per-cell borders: clear each cell fragment's own border edges.
		for _, gc := range g.cells {
			if gc.frag \!= nil {
				gc.frag.Border = [4]BorderEdge{}
			}
		}
	}
	...
	return interior{children: children, contentHeight: gridBottom, collapsedEdges: collapsed}
```

(Confirm `Fragment.Border` is a `[4]BorderEdge` — it is per fragment.go; clearing it removes the separate-mode per-cell borders so only the resolved edges paint.)

- [ ] **Step 8: Write a collapse layout smoke test**

Append to `pkg/layout/css/table_layout_test.go`:

```go
func TestCollapseProducesEdgesAndClearsCellBorders(t *testing.T) {
	mkCell := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width:            gcss.Length{Value: 40, Unit: gcss.UnitPx},
			Height:           gcss.Length{Value: 20, Unit: gcss.UnitPx},
			BorderTopWidth:   gcss.Length{Value: 2, Unit: gcss.UnitPx},
			BorderRightWidth: gcss.Length{Value: 2, Unit: gcss.UnitPx},
			BorderBottomWidth: gcss.Length{Value: 2, Unit: gcss.UnitPx},
			BorderLeftWidth:  gcss.Length{Value: 2, Unit: gcss.UnitPx},
			BorderTopStyle:   "solid", BorderRightStyle: "solid",
			BorderBottomStyle: "solid", BorderLeftStyle: "solid",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC, Style: st}
	}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{mkCell(), mkCell()}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed", BorderCollapse: "collapse"},
		Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	// Find the table fragment (the one carrying Collapsed edges).
	var collapsed int
	var cellBorders int
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		collapsed += len(f.Collapsed)
		if f.W == 40 {
			for _, be := range f.Border {
				if be.Width > 0 {
					cellBorders++
				}
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if collapsed == 0 {
		t.Error("collapse mode should produce resolved edge segments")
	}
	if cellBorders \!= 0 {
		t.Errorf("collapse mode should clear per-cell borders; found %d", cellBorders)
	}
}
```

- [ ] **Step 9: Run tests to verify they pass**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run 'TestCollapse' -v && go test ./pkg/layout/paint/... -count=1`
Expected: PASS.

- [ ] **Step 10: Byte-identical guard + full suite + race, format, lint, commit**

Run (sandbox disabled):
```bash
go test ./pkg/layout/... ./pkg/doctaculous/... -count=1
go test -race ./pkg/layout/... -count=1
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
gofmt -l pkg/layout/css/ pkg/layout/paint/
golangci-lint run ./pkg/layout/...
git add pkg/layout/css/tableborder.go pkg/layout/css/fragment.go pkg/layout/css/table.go pkg/layout/css/block.go pkg/layout/paint/paint.go pkg/layout/css/tableborder_test.go pkg/layout/css/table_layout_test.go
git commit -m "css/layout: border-collapse:collapse (CSS 17.6.2 conflict resolution + resolved-edge paint)"
```
Expected: green; `git status` empty (Collapsed is nil for every non-collapse fragment).

---

### Task 13: Golden images

**Files:**
- Modify: `pkg/doctaculous/html_golden_test.go` (append fixtures)
- New (generated): `pkg/doctaculous/testdata/golden/html-table-*.png`

Add eyeball-able table goldens. The CONTROLLER (not a subagent) eyeballs every new PNG via the Read tool — the implementer subagent has no image vision, so an implementer subagent must STOP after generating and hand the PNGs back for review.

- [ ] **Step 1: Append the golden fixtures**

In `pkg/doctaculous/html_golden_test.go`, append these entries to the `htmlGoldens` slice (match the existing struct literal style):

```go
	{
		// A 2x3 table with per-cell borders + alternating row backgrounds (separate
		// borders, default border-spacing). Eyeball: a clean grid, gaps between cells.
		name:       "table-basic",
		viewportPx: 240,
		html: `<\!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-spacing: 4px; }
  td { border: 2px solid #335; padding: 6px; background: #dde; }
  tr:nth-child(2) td { background: #cce; }
</style></head><body>
  <table>
    <tr><td>R1C1</td><td>R1C2</td><td>R1C3</td></tr>
    <tr><td>R2C1</td><td>R2C2</td><td>R2C3</td></tr>
  </table>
</body></html>`,
	},
	{
		// A header cell spanning two columns over a 2-column body. Eyeball: the header
		// stretches across both columns; the body cells sit beneath each half.
		name:       "table-colspan",
		viewportPx: 240,
		html: `<\!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-spacing: 0; }
  td, th { border: 1px solid #444; padding: 6px; }
  th { background: #cura; background: #ccd; }
</style></head><body>
  <table>
    <tr><th colspan="2">Header</th></tr>
    <tr><td>A</td><td>B</td></tr>
  </table>
</body></html>`,
	},
	{
		// Auto layout: columns sized by their content (a short and a long column).
		// Eyeball: the long-text column is visibly wider than the short one.
		name:       "table-auto",
		viewportPx: 300,
		html: `<\!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-spacing: 0; }
  td { border: 1px solid #555; padding: 4px; }
</style></head><body>
  <table>
    <tr><td>Hi</td><td>A considerably longer cell of content</td></tr>
    <tr><td>Yo</td><td>Short</td></tr>
  </table>
</body></html>`,
	},
	{
		// border-collapse:collapse: shared single edges between cells. Eyeball: no gaps,
		// single (not doubled) lines between cells, the wider border winning at shared edges.
		name:       "table-collapse",
		viewportPx: 240,
		html: `<\!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-collapse: collapse; }
  td { border: 2px solid #336; padding: 6px; }
  td.thick { border: 5px solid #933; }
</style></head><body>
  <table>
    <tr><td>A</td><td class="thick">B</td></tr>
    <tr><td>C</td><td>D</td></tr>
  </table>
</body></html>`,
	},
	{
		// A captioned table (caption-side:top). Eyeball: the caption sits above the grid.
		name:       "table-caption",
		viewportPx: 240,
		html: `<\!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-spacing: 0; }
  caption { font-weight: bold; padding: 4px; }
  td { border: 1px solid #444; padding: 6px; }
</style></head><body>
  <table>
    <caption>Quarterly Results</caption>
    <tr><td>Q1</td><td>Q2</td></tr>
  </table>
</body></html>`,
	},
```

NOTE: remove the accidental `background: #cura;` typo line if you copied it — the `table-colspan` th should be just `th { background: #ccd; }`. (Fix it to a single valid declaration.)

- [ ] **Step 2: Generate the PNGs**

Run (sandbox disabled): `go test ./pkg/doctaculous -run TestHTMLGolden -update`
Expected: PASS; new files appear: `git status --short pkg/doctaculous/testdata/golden/` shows `?? html-table-basic.png` (and the other four).

- [ ] **Step 3: Verify the goldens are stable (re-run WITHOUT -update)**

Run (sandbox disabled): `go test ./pkg/doctaculous -run TestHTMLGolden -count=1`
Expected: PASS (the freshly generated goldens match).

- [ ] **Step 4: CONTROLLER eyeballs every new PNG**

This step is performed by the controller (has image vision), NOT an implementer subagent. Read each of the 5 new PNGs and confirm: `table-basic` is a 2×3 grid with gaps + backgrounds; `table-colspan` header spans both columns; `table-auto`'s long column is wider; `table-collapse` has single shared edges with the thick border winning; `table-caption` shows the caption above the grid. If any looks wrong, FIX the layout (not the golden) and regenerate. An implementer subagent reaching this step must STOP and return the PNG paths for review.

- [ ] **Step 5: Commit**

Run (sandbox disabled):
```bash
gofmt -l pkg/doctaculous/
git add pkg/doctaculous/html_golden_test.go pkg/doctaculous/testdata/golden/html-table-basic.png pkg/doctaculous/testdata/golden/html-table-colspan.png pkg/doctaculous/testdata/golden/html-table-auto.png pkg/doctaculous/testdata/golden/html-table-collapse.png pkg/doctaculous/testdata/golden/html-table-caption.png
git commit -m "doctaculous: table golden images (basic/colspan/auto/collapse/caption)"
```

---

### Task 14: WPT reftests, degradation tests, CLAUDE.md update

**Files:**
- Modify: `pkg/doctaculous/wpt_reftest_test.go` (append entries)
- New: `pkg/doctaculous/testdata/wpt/css21-normal-flow/table-*.html` + `*-ref.html`
- Modify: `pkg/layout/css/table_layout_test.go` (degradation + flag-combination tests)
- Modify: `CLAUDE.md` (Done bullet + flip the TableFC fallback note)

- [ ] **Step 1: Write a degradation + flag-combination test**

Append to `pkg/layout/css/table_layout_test.go`:

```go
func TestRTLTableDegradesGracefully(t *testing.T) {
	// direction:rtl is deferred; it must not panic and must still lay out (LTR).
	var logged bool
	logf := func(string, ...any) { logged = true }
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{fixedCell(50, 20), fixedCell(50, 20)}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed", Direction: "rtl"}, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, logf)
	frag := e.layoutTree(context.Background(), body, 200)
	if frag == nil {
		t.Fatal("RTL table should still produce a fragment")
	}
	if \!logged {
		t.Error("RTL table should log a degradation message")
	}
}

func TestEmptyTableNoPanic(t *testing.T) {
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	if f := e.layoutTree(context.Background(), body, 200); f == nil {
		t.Fatal("empty table should produce a (zero-size) fragment, not nil")
	}
}

func TestCellContainingFloat(t *testing.T) {
	// Flag combination: a float inside a table cell must not panic and the cell must
	// still get a border box (the cell is a BFC, so the float is self-contained).
	flt := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Float: cssbox.FloatLeft,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 20, Unit: gcss.UnitPx},
			Height: gcss.Length{Value: 20, Unit: gcss.UnitPx}, Float: "left"}}
	cell := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 60, Unit: gcss.UnitPx}}, Children: []*cssbox.Box{flt}}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{cell}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed"}, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	if f := e.layoutTree(context.Background(), body, 200); f == nil {
		t.Fatal("a cell containing a float should lay out without panic")
	}
}
```

(If `FloatLeft` is set on the box via `b.Float` AND the style `Float:"left"` both — the engine reads `b.Float`; the test sets both to be safe. Match how other float tests build a floated box: grep `floats_*_test.go`.)

- [ ] **Step 2: Run the degradation tests**

Run (sandbox disabled): `go test ./pkg/layout/css/ -run 'TestRTLTable|TestEmptyTable|TestCellContainingFloat' -v`
Expected: PASS.

- [ ] **Step 3: Create the reftest pairs**

Create three pairs under `pkg/doctaculous/testdata/wpt/css21-normal-flow/`. Each TEST file is a table; each REF file authors the same cells as absolutely-positioned/sized blocks at the table's solved geometry. Compute the geometry by hand from the fixed widths/heights so the ref matches.

`table-basic.html` (a fixed 2×2, each cell 50×30, spacing 0, 1px borders → border box 50×30 at (0,0),(50,0),(0,30),(50,30)):

```html
<\!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-spacing: 0; table-layout: fixed; width: 100px; }
  td { width: 50px; height: 30px; padding: 0; border: 0; background: #38c; }
</style></head><body>
  <table>
    <tr><td></td><td></td></tr>
    <tr><td></td><td></td></tr>
  </table>
</body></html>
```

`table-basic-ref.html` (the same four 50×30 swatches placed by block flow — a 2×2 of inline-blocks with no gaps):

```html
<\!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .r { font-size: 0; }
  .c { display: inline-block; width: 50px; height: 30px; background: #38c; vertical-align: top; }
</style></head><body>
  <div class="r"><span class="c"></span><span class="c"></span></div>
  <div class="r"><span class="c"></span><span class="c"></span></div>
</body></html>
```

`table-colspan.html` (row 0: one colspan-2 cell 100×20; row 1: two 50×20 cells):

```html
<\!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-spacing: 0; table-layout: fixed; width: 100px; }
  td { height: 20px; padding: 0; border: 0; background: #6a3; }
  td.wide { width: 100px; }
  td.half { width: 50px; }
</style></head><body>
  <table>
    <tr><td class="wide" colspan="2"></td></tr>
    <tr><td class="half"></td><td class="half"></td></tr>
  </table>
</body></html>
```

`table-colspan-ref.html`:

```html
<\!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .r { font-size: 0; }
  .wide { display: block; width: 100px; height: 20px; background: #6a3; }
  .c { display: inline-block; width: 50px; height: 20px; background: #6a3; vertical-align: top; }
</style></head><body>
  <div class="wide"></div>
  <div class="r"><span class="c"></span><span class="c"></span></div>
</body></html>
```

`table-auto-width.html` + `table-auto-width-ref.html`: an auto-layout table whose two columns are pinned by specified cell widths (so the geometry is deterministic), matched by two inline-block columns of those widths. Author both so they solve identically (e.g. col widths 60 and 120):

```html
<\!-- table-auto-width.html -->
<\!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-spacing: 0; }
  td { height: 24px; padding: 0; border: 0; background: #939; }
  td.a { width: 60px; } td.b { width: 120px; }
</style></head><body>
  <table><tr><td class="a"></td><td class="b"></td></tr></table>
</body></html>
```

```html
<\!-- table-auto-width-ref.html -->
<\!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .r { font-size: 0; }
  .a { display: inline-block; width: 60px; height: 24px; background: #939; vertical-align: top; }
  .b { display: inline-block; width: 120px; height: 24px; background: #939; vertical-align: top; }
</style></head><body>
  <div class="r"><span class="a"></span><span class="b"></span></div>
</body></html>
```

- [ ] **Step 4: Register the reftests**

In `pkg/doctaculous/wpt_reftest_test.go`, append to `wptReftests`:

```go
	{"table-basic", 200, "a fixed 2x2 table == the same cells as sized inline-blocks at the solved rects", nil},
	{"table-colspan", 200, "a colspan-2 header row == a full-width block over two half-width cells", nil},
	{"table-auto-width", 200, "an auto table with specified column widths == inline-blocks of those widths", nil},
```

- [ ] **Step 5: Run the reftests**

Run (sandbox disabled): `go test ./pkg/doctaculous -run 'TestWPT|TestReftest' -v` (match the real reftest test func name — grep `wpt_reftest_test.go` for the `func Test...`).
Expected: PASS — each table renders identically to its block-authored reference. If a pair mismatches, the table geometry differs from the hand-computed reference: inspect (the reftest harness writes diff images or you can add a temporary golden) and reconcile (usually a spacing/border assumption). Adjust the table/ref so the two genuinely match the SOLVED geometry; do not fudge tolerance.

- [ ] **Step 6: Full suite + race + byte-identical guard**

Run (sandbox disabled):
```bash
go test ./... -count=1
go test -race ./pkg/layout/... ./pkg/doctaculous/... -count=1
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
```
Expected: all PASS; `git status` shows ONLY the new table reftest `.html` files (no existing golden/reftest changed).

- [ ] **Step 7: Update CLAUDE.md**

In `CLAUDE.md`:
1. In the `### Done` section, add a new bullet after the z-index/6b bullet describing tables: the table box tree + anonymous-table fixup (CSS 17.2.1); the fixed + auto column-width solve (17.5.2.1/2); cell layout (cell = BFC) + row heights + `vertical-align`; full colspan + rowspan incl. rowspan height distribution; both `border-collapse` models (separate + the 17.6.2 collapse conflict-resolution + resolved-edge paint); `border-spacing`, `table-layout`, captions (`caption-side`), `<col>`/`<colgroup>` hints, percentage widths against fixed + auto table widths; min/max-content measurement (the new `pkg/layout/css/measure.go`); what is covered (the `html-table-*` goldens + `table-*` reftests + unit tests); and the single deferral: RTL/`direction` (parsed-but-ignored, logged). Reference `docs/superpowers/specs/2026-06-25-html-tables-design.md`.
2. In the `### TODO` section item 6, remove "tables" from the list of remaining slices (it is now done); keep the other remaining slices (web fonts, flexbox/grid, OpenURL/HTTP, pagination, EPUB) and the positioning/replaced/inline follow-ups.
3. Update the `layoutInterior` description note if any prose says TableFC falls back to block — it no longer does (only FlexFC/GridFC do).

- [ ] **Step 8: Final commit**

Run (sandbox disabled):
```bash
gofmt -l pkg/layout/css/ pkg/doctaculous/
golangci-lint run ./pkg/layout/... ./pkg/doctaculous/...
git add pkg/doctaculous/wpt_reftest_test.go pkg/doctaculous/testdata/wpt/css21-normal-flow/ pkg/layout/css/table_layout_test.go CLAUDE.md
git commit -m "doctaculous: table WPT reftests + degradation tests; CLAUDE.md tables done"
```

- [ ] **Step 9: Final sweep — no scratch files, clean tree, whole suite green**

Run (sandbox disabled):
```bash
find . -name 'zz_*'
git status --short
go test ./... -count=1
go vet ./...
golangci-lint run ./pkg/css/... ./pkg/layout/... ./pkg/html/... ./pkg/doctaculous/...
gofmt -l pkg/css pkg/html pkg/layout pkg/doctaculous
```
Expected: `find` prints nothing; `git status` clean; full suite PASSES; vet clean; lint clean; gofmt prints nothing. This is the holistic gate before opening the stacked PR.

---

## Post-plan: stacked PR

After all 14 tasks land and the final sweep is green, open the PR (sandbox disabled; `origin` is HTTPS):

```bash
git push -u origin feat/html-tables
gh pr create --base feat/html-zindex-6b --title "HTML: CSS table layout (sub-project 7)" --body "Implements CSS 2.1 §17 table layout: box tree + anonymous-box fixup, fixed+auto column widths, cell/row layout with vertical-align, full colspan/rowspan, border-collapse separate+collapse, captions, col hints, percentage widths. Min/max-content measurement added as the prerequisite. RTL deferred (parsed, logged). New goldens + WPT reftests + unit tests; no existing page changes. Spec: docs/superpowers/specs/2026-06-25-html-tables-design.md"
```

Keep the PR description short; do NOT credit Claude (per user preference). The base is `feat/html-zindex-6b` (the stack tip); if the stack has merged to `main`, base on `main` instead.
