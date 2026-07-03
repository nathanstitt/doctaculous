# DOCX Fidelity Pass Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the DOCX feature gap — parse and render tables, lists, images, hyperlinks, headers/footers, footnotes, multi-section geometry, and richer run properties — by extending the DOCX parse model and lowering it into the existing `cssbox` layout engine.

**Architecture:** DOCX lowering already targets `cssbox` (the CSS layout engine that HTML uses), so this is overwhelmingly a **parse-and-lower** project, not a layout-engine one. Each feature (1) extends the recursive `pkg/docx` parse model, (2) resolves any new OPC part via the existing relationship machinery, and (3) lowers into the `cssbox.Box` subtree HTML box-generation already emits (same `Display`, `ColSpan`/`RowSpan`, `Marker`, `Replaced` fields). The layout engine's table algorithm, list markers, and replaced-image sizing take over from there.

**Tech Stack:** Go (stdlib `encoding/xml`, `archive/zip`, `image/*`); `pkg/docx` (parse), `pkg/docx/style` (cascade), `pkg/docx/cssbox` (lower), `pkg/layout/cssbox` (box model), `pkg/layout/css` (engine), `pkg/resource` (image loading), `testdata/gen/docx` (fixtures), `pkg/doctaculous` (goldens).

**Delivery:** Six phases, each its own branch off `main` → PR merged on green CI. Phase 1 is a pure refactor (goldens byte-identical); Phases 2–6 each add one feature cluster with a new generated fixture + golden. Every phase must keep existing DOCX goldens unchanged for documents that don't use the new feature.

**Out of scope (per spec, do NOT implement):** **bidi/RTL** (`w:bidi`/`w:rtl`) — the engine has no bidi support anywhere; DOCX stays LTR (parsed-and-ignored if encountered, logged). **Embedded fonts** (`word/fonts/*` de-obfuscation, `fontTable.xml`) — the existing family-name+style → bundled-substitute resolution already picks a reasonable face (Calibri→TeX Gyre Heros, etc.); real weighted bold/italic faces remain the separate roadmap item 4. Neither part is resolved or parsed by any task here.

**Spec:** `docs/superpowers/specs/2026-07-02-docx-fidelity-design.md`

---

## File Structure

**Phase 1 — recursive model refactor:**
- Modify `pkg/docx/model.go` — `Block` gains `Table`; add `Table`/`TableRow`/`TableCell`/`*Props` types; `Paragraph.Runs []Run` → `Paragraph.Content []ParaChild`; add `ParaChild`, `Hyperlink`, `Drawing`.
- Modify `pkg/docx/parse.go` — `parseParagraph` builds `[]ParaChild`; helpers unchanged.
- Modify `pkg/docx/cssbox/lower.go` — `lowerParagraph` iterates `Content` instead of `Runs`.

**Phase 2 — tables:**
- Modify `pkg/docx/parse.go` — add `parseTbl`/`parseTr`/`parseTc`/`parseTblGrid`/`parseTblPr`/`parseTcPr`; `parseBody` handles `w:tbl`.
- Modify `pkg/docx/model.go` — flesh out `TableProps`/`RowProps`/`CellProps` (borders, shading, width, valign).
- Create `pkg/docx/cssbox/table.go` — `lowerTable` → `DisplayTable` subtree.
- Modify `pkg/docx/cssbox/lower.go` — `Lower` dispatches `blk.Table`.
- Modify `testdata/gen/docx/fixtures.go` — add `docx-table`, `docx-table-spans` fixtures.

**Phase 3 — lists/numbering:**
- Create `pkg/docx/numbering.go` — `Numbering` model + `parseNumbering`.
- Modify `pkg/docx/parse.go` / `parsePackage` — resolve+parse `numbering.xml`; parse `w:numPr`.
- Modify `pkg/docx/model.go` — `ParagraphProps` gains `NumID`/`ILvl`.
- Create `pkg/docx/cssbox/list.go` — `lowerListItem` → `DisplayListItem` + `Marker`.
- Modify `testdata/gen/docx/docx.go` — `Builder.SetNumbering`; `fixtures.go` add `docx-list`.

**Phase 4 — images + hyperlinks:**
- Modify `pkg/docx/parse.go` — parse `w:hyperlink`, `w:drawing`→`a:blip`; expose rels map on `Document`.
- Modify `pkg/docx/model.go` — `Document.Rels`, `Document.Media`.
- Create `pkg/docx/cssbox/media.go` — a `resource.ResourceLoader` over `Document.Media`.
- Modify `pkg/docx/cssbox/lower.go` — thread loader; lower `Hyperlink`/`Drawing`.
- Modify `pkg/doctaculous/reflow_backend.go` — pass the media loader into the engine.
- Modify `testdata/gen/docx/docx.go` — `Builder.AddMedia`; `fixtures.go` add `docx-image`, `docx-hyperlink`.

**Phase 5 — parts/sections:**
- Modify `pkg/docx/model.go` — `Document.Sections []Section`, `Headers`/`Footers`, `Footnotes`.
- Modify `pkg/docx/parse.go` / `parsePackage` — resolve header*/footer*/footnotes parts; collect all `sectPr`.
- Modify `pkg/docx/cssbox/lower.go` + `pkg/doctaculous/reflow_backend.go` — margin-band content, per-section geometry.
- Modify `testdata/gen/docx/docx.go` + `fixtures.go` — `docx-header-footer`, `docx-multisection`, `docx-footnote`.

**Phase 6 — run/paragraph properties:**
- Modify `pkg/docx/model.go` — `RunProps` gains strike/vertAlign/highlight/caps/smallCaps/underline-style.
- Modify `pkg/docx/parse.go` `applyRPrChild` + `pkg/docx/style/style.go` `EffectiveRun`/`mergeRun`.
- Modify `pkg/docx/cssbox/lower.go` `runTextBox` — map to `ComputedStyle`.
- Modify `testdata/gen/docx/fixtures.go` — `docx-run-props`.

---

## Phase 1 — Recursive block model refactor

**Branch:** `git checkout main && git pull && git checkout -b docx-fidelity-1-model`

**Goal:** Turn `docx.Block` into a sum type (`Paragraph | Table`) and turn `Paragraph.Runs []Run` into `Paragraph.Content []ParaChild` (so a paragraph can hold runs, hyperlink groups, and drawings). **No new OOXML is parsed and no goldens change** — this is pure plumbing that later phases fill in. The `w:tbl`/`w:hyperlink`/`w:drawing` elements are still skipped by the parser; only the model and the run-iteration in the parser + lowering change shape.

### Task 1.1: Add the recursive model types

**Files:**
- Modify: `pkg/docx/model.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/docx/model_recursive_test.go`:

```go
package docx

import "testing"

// TestParagraphContentHoldsRuns verifies the new Content slice replaces Runs and
// that a bare run round-trips through a ParaChild.
func TestParagraphContentHoldsRuns(t *testing.T) {
	p := Paragraph{Content: []ParaChild{{Run: &Run{Text: "hi"}}}}
	if len(p.Content) != 1 {
		t.Fatalf("Content len = %d, want 1", len(p.Content))
	}
	if p.Content[0].Run == nil || p.Content[0].Run.Text != "hi" {
		t.Fatalf("Content[0].Run = %+v, want Run{Text:hi}", p.Content[0].Run)
	}
}

// TestBlockHoldsTable verifies Block can carry a table.
func TestBlockHoldsTable(t *testing.T) {
	b := Block{Table: &Table{Rows: []TableRow{{Cells: []TableCell{{}}}}}}
	if b.Paragraph != nil {
		t.Fatalf("Paragraph = %+v, want nil", b.Paragraph)
	}
	if b.Table == nil || len(b.Table.Rows) != 1 {
		t.Fatalf("Table = %+v, want 1 row", b.Table)
	}
}

// TestTableCellHoldsBlocks verifies cell content recursion (cells hold blocks).
func TestTableCellHoldsBlocks(t *testing.T) {
	c := TableCell{Blocks: []Block{{Paragraph: &Paragraph{}}}}
	if len(c.Blocks) != 1 || c.Blocks[0].Paragraph == nil {
		t.Fatalf("cell blocks = %+v, want 1 paragraph block", c.Blocks)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run 'TestParagraphContentHoldsRuns|TestBlockHoldsTable|TestTableCellHoldsBlocks' -v`
Expected: FAIL — `undefined: ParaChild`, `p.Content undefined`, `undefined: Table`.

- [ ] **Step 3: Add the types to model.go**

In `pkg/docx/model.go`, replace the `Block` and `Paragraph` type declarations and add the new types. Replace:

```go
// Block is a top-level flow item. For now only paragraphs are modeled; tables and
// other block types are added in later phases.
type Block struct {
	// Paragraph is set for a w:p block.
	Paragraph *Paragraph
}

// Paragraph is a w:p: a sequence of runs sharing paragraph-level properties.
type Paragraph struct {
	Props ParagraphProps
	Runs  []Run
}
```

with:

```go
// Block is a top-level flow item: exactly one field is non-nil. A paragraph
// (w:p) or a table (w:tbl).
type Block struct {
	// Paragraph is set for a w:p block.
	Paragraph *Paragraph
	// Table is set for a w:tbl block.
	Table *Table
}

// Paragraph is a w:p: a sequence of inline children (runs, hyperlink groups,
// drawings) sharing paragraph-level properties.
type Paragraph struct {
	Props   ParagraphProps
	Content []ParaChild
}

// ParaChild is one inline-level member of a paragraph's content: exactly one
// field is non-nil. A bare Run, a Hyperlink group wrapping runs, or a Drawing
// (an embedded image).
type ParaChild struct {
	Run       *Run
	Hyperlink *Hyperlink
	Drawing   *Drawing
}

// Hyperlink is a w:hyperlink: a group of runs that link to Target (an external
// URL resolved from the r:id relationship) or Anchor (an internal bookmark).
// Later phases populate it; Phase 1 only declares it.
type Hyperlink struct {
	Target string
	Anchor string
	Runs   []Run
}

// Drawing is a w:drawing carrying an embedded image: RelID references the image
// part via the document relationships; WidthEMU/HeightEMU are the extent (914400
// EMU = 1in). Later phases populate it; Phase 1 only declares it.
type Drawing struct {
	RelID             string
	WidthEMU          int64
	HeightEMU         int64
}

// Table is a w:tbl: a column grid plus rows. Props carries table-level borders,
// shading, width, and alignment. Later phases populate Props; Phase 1 declares
// the shape.
type Table struct {
	Grid  []Twips // w:tblGrid column widths
	Rows  []TableRow
	Props TableProps
}

// TableRow is a w:tr. Props carries row height and header/split flags.
type TableRow struct {
	Cells []TableCell
	Props RowProps
}

// TableCell is a w:tc. Blocks holds the cell's content (paragraphs, nested
// tables — the recursion). GridSpan is w:gridSpan (horizontal span; default 1).
// VMerge records vertical merging (row spanning). Props carries cell borders,
// shading, width, and vertical alignment.
type TableCell struct {
	Blocks   []Block
	GridSpan int
	VMerge   VMergeKind
	Props    CellProps
}

// VMergeKind classifies a cell's w:vMerge state.
type VMergeKind int

const (
	// VMergeNone means the cell is not vertically merged.
	VMergeNone VMergeKind = iota
	// VMergeRestart begins a vertical merge (w:vMerge val="restart" or a bare
	// w:vMerge with no val on the first row of a span).
	VMergeRestart
	// VMergeContinue continues the merge above (w:vMerge val="continue").
	VMergeContinue
)

// TableProps holds table-level properties (w:tblPr). Fields are populated in the
// tables phase.
type TableProps struct {
	Borders  BoxBorders
	Shading  Shading
	WidthPct int  // w:tblW type="pct" (in fiftieths of a percent per OOXML); 0 = unset
	WidthDxa Twips // w:tblW type="dxa"; 0 = unset
	Justify  Justify
}

// RowProps holds row-level properties (w:trPr). Populated in the tables phase.
type RowProps struct {
	IsHeader bool  // w:tblHeader
	HeightDxa Twips // w:trHeight
}

// CellProps holds cell-level properties (w:tcPr). Populated in the tables phase.
type CellProps struct {
	Borders  BoxBorders
	Shading  Shading
	WidthDxa Twips    // w:tcW type="dxa"; 0 = unset
	VAlign   CellVAlign
}

// CellVAlign is a cell's vertical alignment (w:vAlign).
type CellVAlign int

const (
	VAlignTop CellVAlign = iota
	VAlignCenter
	VAlignBottom
)

// BoxBorders holds the four edge borders of a table or cell. Populated in the
// tables phase.
type BoxBorders struct {
	Top, Bottom, Left, Right Border
}

// Border is one edge border (w:tblBorders/w:tcBorders child). SizeEighthPt is
// w:sz in eighths of a point. None is true when style="nil"/"none".
type Border struct {
	None        bool
	SizeEighthPt int
	Color       color.RGBA
	HasColor    bool
}

// Shading is a cell/table background fill (w:shd). HasFill distinguishes an
// explicit fill from "unset"/"auto".
type Shading struct {
	Fill    color.RGBA
	HasFill bool
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run 'TestParagraphContentHoldsRuns|TestBlockHoldsTable|TestTableCellHoldsBlocks' -v`
Expected: PASS. (The package will NOT build overall yet — `parse.go`/`lower.go` still reference `p.Runs`. That is fixed in the next tasks. Run the targeted test only; `go build ./pkg/docx` still fails here.)

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/model.go pkg/docx/model_recursive_test.go
git commit -m "docx: add recursive block model (Table, ParaChild, Hyperlink, Drawing)"
```

### Task 1.2: Re-point the parser onto Content

**Files:**
- Modify: `pkg/docx/parse.go:161-200` (`parseParagraph`)

The parser currently appends to `p.Runs`. `parseRun` still returns `[]Run` (unchanged); the paragraph now wraps each into a `ParaChild{Run: ...}`. `w:hyperlink`/`w:drawing` remain skipped (`dec.Skip()`) — they light up in Phase 4.

- [ ] **Step 1: Write the failing test**

Add to `pkg/docx/parse_test.go` (or a new `pkg/docx/parse_content_test.go`):

```go
package docx

import "testing"

// TestParseParagraphFillsContent verifies runs land in Paragraph.Content (not the
// removed Runs field) after the refactor.
func TestParseParagraphFillsContent(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>alpha</w:t></w:r><w:r><w:t> beta</w:t></w:r></w:p>
</w:body></w:document>`)
	if len(doc.Body) != 1 || doc.Body[0].Paragraph == nil {
		t.Fatalf("body = %+v, want 1 paragraph block", doc.Body)
	}
	c := doc.Body[0].Paragraph.Content
	if len(c) != 2 {
		t.Fatalf("Content len = %d, want 2", len(c))
	}
	if c[0].Run == nil || c[0].Run.Text != "alpha" {
		t.Fatalf("Content[0] = %+v, want Run{alpha}", c[0])
	}
	if c[1].Run == nil || c[1].Run.Text != " beta" {
		t.Fatalf("Content[1] = %+v, want Run{ beta}", c[1])
	}
}
```

If `mustParse` does not already exist in the test file, add this helper alongside the test:

```go
// mustParse parses a document.xml body string into a Document, failing the test
// on error. It wraps the bytes in a minimal OPC package via the gen builder is
// overkill here; parse the document part directly.
func mustParse(t *testing.T, documentXML string) *Document {
	t.Helper()
	doc := &Document{Section: defaultSection()}
	if err := parseDocument([]byte(documentXML), doc); err != nil {
		t.Fatalf("parseDocument: %v", err)
	}
	return doc
}
```

(Before adding `mustParse`, grep: `grep -n "func mustParse" pkg/docx/*_test.go`. If it exists with a different signature, reuse it and adapt the test body instead of redefining.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run TestParseParagraphFillsContent -v`
Expected: FAIL to compile — `p.Runs` still referenced elsewhere means the package does not build; also `Content` not filled.

- [ ] **Step 3: Update parseParagraph**

In `pkg/docx/parse.go`, in `parseParagraph`, change the `w:r` case. Replace:

```go
			case "r":
				runs, err := parseRun(dec)
				if err != nil {
					return nil, nil, err
				}
				p.Runs = append(p.Runs, runs...)
```

with:

```go
			case "r":
				runs, err := parseRun(dec)
				if err != nil {
					return nil, nil, err
				}
				for i := range runs {
					r := runs[i]
					p.Content = append(p.Content, ParaChild{Run: &r})
				}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run TestParseParagraphFillsContent -v`
Expected: still FAILS to build if `lower.go` references `p.Runs` — that is fixed in Task 1.3. If you are running the whole package, expect the build error to point at `pkg/docx/cssbox/lower.go`. Proceed to Task 1.3; the two form one atomic refactor. To confirm just the parser compiles in isolation: `go build ./pkg/docx` — Expected: PASS (pkg/docx no longer references Runs).

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/parse.go pkg/docx/parse_test.go
git commit -m "docx: parse runs into Paragraph.Content"
```

### Task 1.3: Re-point the lowering onto Content

**Files:**
- Modify: `pkg/docx/cssbox/lower.go:86-115` (`lowerParagraph`)

`lowerParagraph` ranges `p.Runs`; it must range `p.Content`, unwrapping the `Run` (a `ParaChild` whose `Run` is nil — a hyperlink/drawing — is skipped in Phase 1, handled in Phase 4).

- [ ] **Step 1: Write the failing test**

Add to `pkg/docx/cssbox/lower_test.go`:

```go
// TestLowerParagraphReadsContent verifies lowering consumes Paragraph.Content and
// still produces one styled text box per run.
func TestLowerParagraphReadsContent(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{
			Content: []docx.ParaChild{{Run: &docx.Run{Text: "hello"}}},
		}}},
	}
	r := style.NewResolver(d, nil)
	root := Lower(d, r)
	// root -> body -> paragraph block -> text box
	body := root.Children[len(root.Children)-1]
	if len(body.Children) != 1 {
		t.Fatalf("body children = %d, want 1 paragraph block", len(body.Children))
	}
	para := body.Children[0]
	if len(para.Children) != 1 || para.Children[0].Text != "hello" {
		t.Fatalf("paragraph children = %+v, want one text box 'hello'", para.Children)
	}
}
```

(Confirm the existing imports in `lower_test.go` include `docx` and `style`; grep `grep -n "docx/style" pkg/docx/cssbox/lower_test.go`. If the test file has no imports block yet, model it on the existing tests in that file.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx/cssbox -run TestLowerParagraphReadsContent -v`
Expected: FAIL to build — `p.Runs` undefined in `lower.go`.

- [ ] **Step 3: Update lowerParagraph**

In `pkg/docx/cssbox/lower.go`, in `lowerParagraph`, change the loop. Replace:

```go
	for _, run := range p.Runs {
		switch run.Break {
```

with:

```go
	for _, child := range p.Content {
		if child.Run == nil {
			// Hyperlink groups and drawings are lowered in the images+hyperlinks
			// phase; a bare run is all Phase 1 handles.
			continue
		}
		run := *child.Run
		switch run.Break {
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx/cssbox -run TestLowerParagraphReadsContent -v`
Expected: PASS. Now the whole module builds again: `go build ./...` — Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/cssbox/lower.go pkg/docx/cssbox/lower_test.go
git commit -m "docx/cssbox: lower Paragraph.Content (skip hyperlink/drawing until phase 4)"
```

### Task 1.4: Prove the refactor is behavior-preserving (goldens byte-identical)

**Files:** none modified — verification only.

- [ ] **Step 1: Run the full DOCX golden suite**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -v`
Expected: PASS for all four fixtures (`paragraph`, `styled`, `justify`, `multipage`) — **no golden regenerated, no pixel diff**. If any fails, the refactor changed behavior; fix before proceeding (do NOT run `-update`).

- [ ] **Step 2: Run the whole test suite + vet + lint**

Run: `go test ./... && go vet ./... && golangci-lint run`
Expected: all pass. (The four new tests from 1.1–1.3 pass; every existing test unchanged.)

- [ ] **Step 3: Run the race detector**

Run: `go test -race ./pkg/docx/... ./pkg/doctaculous`
Expected: PASS, no data races.

- [ ] **Step 4: Open the PR**

```bash
git push -u origin docx-fidelity-1-model
gh pr create --title "docx: recursive block model refactor (phase 1 of DOCX fidelity)" \
  --body "Foundation for DOCX tables/lists/images/hyperlinks. Block becomes a Paragraph|Table sum type; Paragraph.Runs becomes Paragraph.Content []ParaChild. No new OOXML parsed; all DOCX goldens byte-identical. Spec: docs/superpowers/specs/2026-07-02-docx-fidelity-design.md"
```

Wait for green CI + merge before starting Phase 2.

---

## Phase 2 — Tables

**Branch:** `git checkout main && git pull && git checkout -b docx-fidelity-2-tables`

**Goal:** Parse `w:tbl` (grid, rows, cells, `gridSpan`, `vMerge`, borders, shading, cell margins, width, alignment) and lower it into a `cssbox` `DisplayTable` subtree. The engine's existing anonymous-table-box fixup, column-width solve, colspan/rowspan distribution, and cell-content recursion do the layout. Add `docx-table` and `docx-table-spans` fixtures + goldens.

### Task 2.1: Parse the table grid, rows, and cells (structure only)

**Files:**
- Modify: `pkg/docx/parse.go` (add `parseTbl`/`parseTr`/`parseTc`/`parseTblGrid`; wire `w:tbl` into `parseBody`)

Start with structure (grid + rows + cells + `gridSpan`/`vMerge` + cell content recursion); borders/shading/width come in Task 2.2. Cell content recursion reuses the existing body-block parsing: a cell's children are parsed exactly like the document body (paragraphs and nested tables), so factor the block-dispatch out of `parseBody` into a shared `parseBlockChild`.

- [ ] **Step 1: Write the failing test**

Add `pkg/docx/parse_table_test.go`:

```go
package docx

import "testing"

func TestParseTableGridRowsCells(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:tbl>
  <w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="3000"/></w:tblGrid>
  <w:tr>
    <w:tc><w:p><w:r><w:t>A1</w:t></w:r></w:p></w:tc>
    <w:tc><w:p><w:r><w:t>B1</w:t></w:r></w:p></w:tc>
  </w:tr>
  <w:tr>
    <w:tc><w:tcPr><w:gridSpan w:val="2"/></w:tcPr><w:p><w:r><w:t>span</w:t></w:r></w:p></w:tc>
  </w:tr>
</w:tbl>
</w:body></w:document>`)
	if len(doc.Body) != 1 || doc.Body[0].Table == nil {
		t.Fatalf("body = %+v, want 1 table block", doc.Body)
	}
	tb := doc.Body[0].Table
	if len(tb.Grid) != 2 || tb.Grid[0] != 2000 || tb.Grid[1] != 3000 {
		t.Fatalf("grid = %v, want [2000 3000]", tb.Grid)
	}
	if len(tb.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(tb.Rows))
	}
	if len(tb.Rows[0].Cells) != 2 {
		t.Fatalf("row0 cells = %d, want 2", len(tb.Rows[0].Cells))
	}
	// cell content recursion: the cell holds a paragraph block with text "A1".
	c := tb.Rows[0].Cells[0]
	if len(c.Blocks) != 1 || c.Blocks[0].Paragraph == nil {
		t.Fatalf("cell A1 blocks = %+v, want 1 paragraph", c.Blocks)
	}
	if got := c.Blocks[0].Paragraph.Content[0].Run.Text; got != "A1" {
		t.Fatalf("cell A1 text = %q, want A1", got)
	}
	// gridSpan
	if got := tb.Rows[1].Cells[0].GridSpan; got != 2 {
		t.Fatalf("span cell GridSpan = %d, want 2", got)
	}
}

func TestParseTableVMerge(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:tbl>
  <w:tr><w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr><w:p/></w:tc></w:tr>
  <w:tr><w:tc><w:tcPr><w:vMerge w:val="continue"/></w:tcPr><w:p/></w:tc></w:tr>
</w:tbl>
</w:body></w:document>`)
	tb := doc.Body[0].Table
	if tb.Rows[0].Cells[0].VMerge != VMergeRestart {
		t.Fatalf("row0 VMerge = %v, want restart", tb.Rows[0].Cells[0].VMerge)
	}
	if tb.Rows[1].Cells[0].VMerge != VMergeContinue {
		t.Fatalf("row1 VMerge = %v, want continue", tb.Rows[1].Cells[0].VMerge)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run 'TestParseTableGridRowsCells|TestParseTableVMerge' -v`
Expected: FAIL — `w:tbl` is currently skipped, so `doc.Body[0].Table` is nil (actually `len(doc.Body)==0`).

- [ ] **Step 3a: Factor block dispatch out of parseBody**

In `pkg/docx/parse.go`, in `parseBody`, replace the `switch t.Name.Local` block's `"p"`/`"sectPr"`/`default` cases with a call to a shared helper. Replace:

```go
			switch t.Name.Local {
			case "p":
				p, sect, err := parseParagraph(dec)
				if err != nil {
					return err
				}
				doc.Body = append(doc.Body, Block{Paragraph: p})
				// A sectPr inside the last paragraph's pPr is the section for that
				// run of content; for a single-section document it is the whole doc.
				if sect != nil {
					doc.Section = *sect
				}
			case "sectPr":
				sect, err := parseSectPr(dec)
				if err != nil {
					return err
				}
				doc.Section = sect
			default:
				if err := dec.Skip(); err != nil {
					return fmt.Errorf("%w: body: %v", ErrMalformedXML, err)
				}
			}
```

with:

```go
			switch t.Name.Local {
			case "sectPr":
				sect, err := parseSectPr(dec)
				if err != nil {
					return err
				}
				doc.Section = sect
			default:
				blk, sect, err := parseBlockChild(dec, t)
				if err != nil {
					return err
				}
				if blk != nil {
					doc.Body = append(doc.Body, *blk)
				}
				if sect != nil {
					doc.Section = *sect
				}
			}
```

Then add the shared helper (place it just after `parseBody`):

```go
// parseBlockChild dispatches a block-level start element (w:p or w:tbl) shared by
// the body and by table cells. It returns the parsed block (nil for an element it
// skips) and any sectPr found in a paragraph's pPr (a section boundary; nil in a
// cell context). start is the already-read start element.
func parseBlockChild(dec *xml.Decoder, start xml.StartElement) (*Block, *SectionProps, error) {
	switch start.Name.Local {
	case "p":
		p, sect, err := parseParagraph(dec)
		if err != nil {
			return nil, nil, err
		}
		return &Block{Paragraph: p}, sect, nil
	case "tbl":
		tb, err := parseTbl(dec)
		if err != nil {
			return nil, nil, err
		}
		return &Block{Table: tb}, nil, nil
	default:
		if err := dec.Skip(); err != nil {
			return nil, nil, fmt.Errorf("%w: block: %v", ErrMalformedXML, err)
		}
		return nil, nil, nil
	}
}
```

- [ ] **Step 3b: Add the table parsers**

Add to `pkg/docx/parse.go` (near the other parse functions):

```go
// parseTbl consumes a w:tbl into a Table: its grid, rows, and (Task 2.2) props.
func parseTbl(dec *xml.Decoder) (*Table, error) {
	tb := &Table{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: tbl: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: tbl: %v", ErrMalformedXML, err)
				}
				continue
			}
			switch t.Name.Local {
			case "tblGrid":
				grid, err := parseTblGrid(dec)
				if err != nil {
					return nil, err
				}
				tb.Grid = grid
			case "tblPr":
				props, err := parseTblPr(dec)
				if err != nil {
					return nil, err
				}
				tb.Props = props
			case "tr":
				row, err := parseTr(dec)
				if err != nil {
					return nil, err
				}
				tb.Rows = append(tb.Rows, row)
			default:
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: tbl: %v", ErrMalformedXML, err)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "tbl" {
				return tb, nil
			}
		}
	}
}

// parseTblGrid reads the w:gridCol widths of a w:tblGrid.
func parseTblGrid(dec *xml.Decoder) ([]Twips, error) {
	var grid []Twips
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: tblGrid: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "gridCol" {
				if v, ok := wAttrInt(t, "w"); ok {
					grid = append(grid, Twips(v))
				}
			}
			if err := dec.Skip(); err != nil {
				return nil, fmt.Errorf("%w: tblGrid: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "tblGrid" {
				return grid, nil
			}
		}
	}
}

// parseTr consumes a w:tr into a TableRow.
func parseTr(dec *xml.Decoder) (TableRow, error) {
	var row TableRow
	for {
		tok, err := dec.Token()
		if err != nil {
			return row, fmt.Errorf("%w: tr: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return row, fmt.Errorf("%w: tr: %v", ErrMalformedXML, err)
				}
				continue
			}
			switch t.Name.Local {
			case "trPr":
				props, err := parseTrPr(dec)
				if err != nil {
					return row, err
				}
				row.Props = props
			case "tc":
				cell, err := parseTc(dec)
				if err != nil {
					return row, err
				}
				row.Cells = append(row.Cells, cell)
			default:
				if err := dec.Skip(); err != nil {
					return row, fmt.Errorf("%w: tr: %v", ErrMalformedXML, err)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "tr" {
				return row, nil
			}
		}
	}
}

// parseTc consumes a w:tc into a TableCell, recursing into its block content.
func parseTc(dec *xml.Decoder) (TableCell, error) {
	cell := TableCell{GridSpan: 1}
	for {
		tok, err := dec.Token()
		if err != nil {
			return cell, fmt.Errorf("%w: tc: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return cell, fmt.Errorf("%w: tc: %v", ErrMalformedXML, err)
				}
				continue
			}
			switch t.Name.Local {
			case "tcPr":
				props, span, vmerge, err := parseTcPr(dec)
				if err != nil {
					return cell, err
				}
				cell.Props = props
				if span > 0 {
					cell.GridSpan = span
				}
				cell.VMerge = vmerge
			default:
				blk, _, err := parseBlockChild(dec, t)
				if err != nil {
					return cell, err
				}
				if blk != nil {
					cell.Blocks = append(cell.Blocks, *blk)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "tc" {
				return cell, nil
			}
		}
	}
}
```

Add minimal stubs for the props parsers (fleshed out in Task 2.2), so this task compiles and the structure tests pass:

```go
// parseTblPr consumes a w:tblPr. Task 2.2 reads borders/shading/width/jc; this
// stub skips the body so structural parsing works first.
func parseTblPr(dec *xml.Decoder) (TableProps, error) {
	var props TableProps
	return props, skipElement(dec, "tblPr")
}

// parseTrPr consumes a w:trPr (row height / header flag in Task 2.2).
func parseTrPr(dec *xml.Decoder) (RowProps, error) {
	var props RowProps
	return props, skipElement(dec, "trPr")
}

// parseTcPr consumes a w:tcPr, returning cell props plus the gridSpan and vMerge
// state (both needed for structure, so they are read here, not deferred).
func parseTcPr(dec *xml.Decoder) (props CellProps, gridSpan int, vmerge VMergeKind, err error) {
	for {
		tok, terr := dec.Token()
		if terr != nil {
			return props, gridSpan, vmerge, fmt.Errorf("%w: tcPr: %v", ErrMalformedXML, terr)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "gridSpan":
					if v, ok := wAttrInt(t, "val"); ok {
						gridSpan = v
					}
				case "vMerge":
					vmerge = parseVMerge(t)
				}
			}
			if serr := dec.Skip(); serr != nil {
				return props, gridSpan, vmerge, fmt.Errorf("%w: tcPr: %v", ErrMalformedXML, serr)
			}
		case xml.EndElement:
			if t.Name.Local == "tcPr" {
				return props, gridSpan, vmerge, nil
			}
		}
	}
}

// parseVMerge maps a w:vMerge element to a VMergeKind. A bare w:vMerge with no val
// (or val="restart") begins a merge; val="continue" continues it.
func parseVMerge(e xml.StartElement) VMergeKind {
	switch wVal(e) {
	case "continue":
		return VMergeContinue
	default: // "restart" or empty
		return VMergeRestart
	}
}

// skipElement consumes tokens until the matching end element of name, discarding
// them. It is used by props stubs that do not yet read their body.
func skipElement(dec *xml.Decoder, name string) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrMalformedXML, name, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := dec.Skip(); err != nil {
				return fmt.Errorf("%w: %s: %v", ErrMalformedXML, name, err)
			}
		case xml.EndElement:
			if t.Name.Local == name {
				return nil
			}
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run 'TestParseTableGridRowsCells|TestParseTableVMerge' -v`
Expected: PASS. Also confirm nothing else broke: `go test ./pkg/docx`.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/parse.go pkg/docx/parse_table_test.go
git commit -m "docx: parse w:tbl structure (grid, rows, cells, gridSpan, vMerge, cell recursion)"
```

### Task 2.2: Parse table/cell borders, shading, width, and vAlign

**Files:**
- Modify: `pkg/docx/parse.go` (flesh out `parseTblPr`/`parseTrPr`/`parseTcPr`; add `parseBorders`/`parseShd`/`parseTblW`)

- [ ] **Step 1: Write the failing test**

Add to `pkg/docx/parse_table_test.go`:

```go
func TestParseTableProps(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:tbl>
  <w:tblPr>
    <w:tblW w:type="dxa" w:w="5000"/>
    <w:jc w:val="center"/>
    <w:tblBorders>
      <w:top w:sz="8" w:color="FF0000"/>
      <w:bottom w:sz="8" w:color="FF0000"/>
    </w:tblBorders>
  </w:tblPr>
  <w:tr>
    <w:tc>
      <w:tcPr>
        <w:tcW w:type="dxa" w:w="2500"/>
        <w:vAlign w:val="center"/>
        <w:shd w:fill="EEEEEE"/>
      </w:tcPr>
      <w:p><w:r><w:t>c</w:t></w:r></w:p>
    </w:tc>
  </w:tr>
</w:tbl>
</w:body></w:document>`)
	tb := doc.Body[0].Table
	if tb.Props.WidthDxa != 5000 {
		t.Fatalf("table WidthDxa = %d, want 5000", tb.Props.WidthDxa)
	}
	if tb.Props.Justify != JustifyCenter {
		t.Fatalf("table Justify = %v, want center", tb.Props.Justify)
	}
	if tb.Props.Borders.Top.None || tb.Props.Borders.Top.SizeEighthPt != 8 {
		t.Fatalf("table top border = %+v, want sz 8", tb.Props.Borders.Top)
	}
	if !tb.Props.Borders.Top.HasColor || tb.Props.Borders.Top.Color.R != 0xFF {
		t.Fatalf("table top border color = %+v, want red", tb.Props.Borders.Top)
	}
	cell := tb.Rows[0].Cells[0]
	if cell.Props.WidthDxa != 2500 {
		t.Fatalf("cell WidthDxa = %d, want 2500", cell.Props.WidthDxa)
	}
	if cell.Props.VAlign != VAlignCenter {
		t.Fatalf("cell VAlign = %v, want center", cell.Props.VAlign)
	}
	if !cell.Props.Shading.HasFill || cell.Props.Shading.Fill.R != 0xEE {
		t.Fatalf("cell shading = %+v, want #EEEEEE", cell.Props.Shading)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run TestParseTableProps -v`
Expected: FAIL — the props stubs skip their bodies, so every field is zero.

- [ ] **Step 3: Flesh out the props parsers**

In `pkg/docx/parse.go`, replace the `parseTblPr`, `parseTrPr`, and `parseTcPr` stubs. `parseTcPr` keeps returning `(props, gridSpan, vmerge, err)`; only its body-reading grows. Replace `parseTblPr`:

```go
// parseTblPr reads w:tblPr: table width (w:tblW), alignment (w:jc), and borders
// (w:tblBorders) / shading (w:shd).
func parseTblPr(dec *xml.Decoder) (TableProps, error) {
	var props TableProps
	for {
		tok, err := dec.Token()
		if err != nil {
			return props, fmt.Errorf("%w: tblPr: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "tblW":
					applyTblW(&props.WidthDxa, &props.WidthPct, t)
				case "jc":
					props.Justify = parseJustify(wVal(t))
				case "tblBorders":
					b, err := parseBorders(dec, "tblBorders")
					if err != nil {
						return props, err
					}
					props.Borders = b
					continue
				case "shd":
					props.Shading = parseShd(t)
				}
			}
			if err := dec.Skip(); err != nil {
				return props, fmt.Errorf("%w: tblPr: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "tblPr" {
				return props, nil
			}
		}
	}
}
```

Replace `parseTrPr`:

```go
// parseTrPr reads w:trPr: the header-row flag (w:tblHeader) and row height
// (w:trHeight).
func parseTrPr(dec *xml.Decoder) (RowProps, error) {
	var props RowProps
	for {
		tok, err := dec.Token()
		if err != nil {
			return props, fmt.Errorf("%w: trPr: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "tblHeader":
					props.IsHeader = parseOnOff(wVal(t))
				case "trHeight":
					if v, ok := wAttrInt(t, "val"); ok {
						props.HeightDxa = Twips(v)
					}
				}
			}
			if err := dec.Skip(); err != nil {
				return props, fmt.Errorf("%w: trPr: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "trPr" {
				return props, nil
			}
		}
	}
}
```

Replace `parseTcPr`'s inner `switch` to also read width/vAlign/borders/shading (keep the gridSpan/vMerge cases):

```go
// parseTcPr consumes a w:tcPr, returning cell props plus the gridSpan and vMerge
// state.
func parseTcPr(dec *xml.Decoder) (props CellProps, gridSpan int, vmerge VMergeKind, err error) {
	for {
		tok, terr := dec.Token()
		if terr != nil {
			return props, gridSpan, vmerge, fmt.Errorf("%w: tcPr: %v", ErrMalformedXML, terr)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "gridSpan":
					if v, ok := wAttrInt(t, "val"); ok {
						gridSpan = v
					}
				case "vMerge":
					vmerge = parseVMerge(t)
				case "tcW":
					var pct int
					applyTblW(&props.WidthDxa, &pct, t)
				case "vAlign":
					props.VAlign = parseVAlign(wVal(t))
				case "tcBorders":
					b, berr := parseBorders(dec, "tcBorders")
					if berr != nil {
						return props, gridSpan, vmerge, berr
					}
					props.Borders = b
					continue
				case "shd":
					props.Shading = parseShd(t)
				}
			}
			if serr := dec.Skip(); serr != nil {
				return props, gridSpan, vmerge, fmt.Errorf("%w: tcPr: %v", ErrMalformedXML, serr)
			}
		case xml.EndElement:
			if t.Name.Local == "tcPr" {
				return props, gridSpan, vmerge, nil
			}
		}
	}
}
```

Add the shared helpers:

```go
// applyTblW reads a w:tblW / w:tcW measurement. type="dxa" is twips; type="pct"
// is fiftieths of a percent. Only one of dxa/pct is set.
func applyTblW(dxa *Twips, pct *int, e xml.StartElement) {
	typ, _ := wAttr(e, "type")
	v, ok := wAttrInt(e, "w")
	if !ok {
		return
	}
	switch typ {
	case "pct":
		*pct = v
	default: // "dxa" or unspecified
		*dxa = Twips(v)
	}
}

// parseVAlign maps a w:vAlign value to a CellVAlign.
func parseVAlign(val string) CellVAlign {
	switch val {
	case "center":
		return VAlignCenter
	case "bottom":
		return VAlignBottom
	default:
		return VAlignTop
	}
}

// parseShd reads a w:shd fill into a Shading. fill="auto" or absent yields no
// fill (HasFill false).
func parseShd(e xml.StartElement) Shading {
	fill, _ := wAttr(e, "fill")
	if c, ok := parseColor(fill); ok {
		return Shading{Fill: c, HasFill: true}
	}
	return Shading{}
}

// parseBorders reads a w:tblBorders / w:tcBorders element's four edges. name is
// the wrapping element's local name (so the loop knows its end tag).
func parseBorders(dec *xml.Decoder, name string) (BoxBorders, error) {
	var b BoxBorders
	for {
		tok, err := dec.Token()
		if err != nil {
			return b, fmt.Errorf("%w: %s: %v", ErrMalformedXML, name, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				border := parseBorder(t)
				switch t.Name.Local {
				case "top":
					b.Top = border
				case "bottom":
					b.Bottom = border
				case "left", "start":
					b.Left = border
				case "right", "end":
					b.Right = border
				}
			}
			if err := dec.Skip(); err != nil {
				return b, fmt.Errorf("%w: %s: %v", ErrMalformedXML, name, err)
			}
		case xml.EndElement:
			if t.Name.Local == name {
				return b, nil
			}
		}
	}
}

// parseBorder reads one border edge element (w:sz eighths-of-a-point, w:color,
// w:val style). val="nil"/"none" marks the edge as no-border.
func parseBorder(e xml.StartElement) Border {
	var bd Border
	if v := wVal(e); v == "nil" || v == "none" {
		bd.None = true
	}
	if v, ok := wAttrInt(e, "sz"); ok {
		bd.SizeEighthPt = v
	}
	if c, ok := parseColor(mustColorAttr(e)); ok {
		bd.Color = c
		bd.HasColor = true
	}
	return bd
}

// mustColorAttr returns the w:color attribute value, or "" if absent.
func mustColorAttr(e xml.StartElement) string {
	v, _ := wAttr(e, "color")
	return v
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run 'TestParseTableProps|TestParseTableGridRowsCells|TestParseTableVMerge' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/parse.go pkg/docx/parse_table_test.go
git commit -m "docx: parse table/cell borders, shading, width, alignment, vAlign"
```

### Task 2.3: Lower a table into a cssbox DisplayTable subtree

**Files:**
- Create: `pkg/docx/cssbox/table.go`
- Modify: `pkg/docx/cssbox/lower.go` (dispatch `blk.Table` in `Lower`)

The lowering mirrors HTML box-generation (`pkg/layout/css/build.go:378-397`): a table box is `Kind:BoxBlock, Display:DisplayTable, Formatting:TableFC`; a row is `DisplayTableRow, TableFC`; a cell is `DisplayTableCell, BlockFC` with `ColSpan`/`RowSpan`. Cell content lowers recursively through the same block dispatch as the body. Borders/shading/width map onto the cell/table `ComputedStyle` (`BorderTopWidth`/`BorderTopStyle`/`BorderTopColor` per edge, `BackgroundColor`, `Width`). `vMerge` becomes `RowSpan`: a `VMergeRestart` cell's `RowSpan` = 1 + the count of `VMergeContinue` cells below it in the same grid column; `VMergeContinue` cells are dropped (the engine's rowspan handling covers their area).

- [ ] **Step 1: Write the failing test**

Create `pkg/docx/cssbox/table_test.go`:

```go
package cssbox

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func lowerDoc(t *testing.T, d *docx.Document) *lcssbox.Box {
	t.Helper()
	return Lower(d, style.NewResolver(d, nil))
}

func TestLowerTableStructure(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Body: []docx.Block{{Table: &docx.Table{
			Grid: []docx.Twips{2000, 3000},
			Rows: []docx.TableRow{
				{Cells: []docx.TableCell{
					{GridSpan: 1, Blocks: []docx.Block{{Paragraph: paraWith("A1")}}},
					{GridSpan: 1, Blocks: []docx.Block{{Paragraph: paraWith("B1")}}},
				}},
			},
		}}},
	}
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	if len(body.Children) != 1 {
		t.Fatalf("body children = %d, want 1 table", len(body.Children))
	}
	tbl := body.Children[0]
	if tbl.Display != lcssbox.DisplayTable {
		t.Fatalf("table Display = %v, want DisplayTable", tbl.Display)
	}
	if len(tbl.Children) != 1 || tbl.Children[0].Display != lcssbox.DisplayTableRow {
		t.Fatalf("table child = %+v, want one DisplayTableRow", tbl.Children)
	}
	row := tbl.Children[0]
	if len(row.Children) != 2 {
		t.Fatalf("row cells = %d, want 2", len(row.Children))
	}
	cell := row.Children[0]
	if cell.Display != lcssbox.DisplayTableCell {
		t.Fatalf("cell Display = %v, want DisplayTableCell", cell.Display)
	}
	// cell content recursion: a paragraph block holding a text box "A1".
	if len(cell.Children) != 1 || len(cell.Children[0].Children) != 1 || cell.Children[0].Children[0].Text != "A1" {
		t.Fatalf("cell content = %+v, want paragraph->text A1", cell.Children)
	}
}

func TestLowerTableVMergeToRowSpan(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Body: []docx.Block{{Table: &docx.Table{
			Grid: []docx.Twips{2000},
			Rows: []docx.TableRow{
				{Cells: []docx.TableCell{{GridSpan: 1, VMerge: docx.VMergeRestart, Blocks: []docx.Block{{Paragraph: paraWith("m")}}}}},
				{Cells: []docx.TableCell{{GridSpan: 1, VMerge: docx.VMergeContinue, Blocks: []docx.Block{{Paragraph: paraWith("")}}}}},
			},
		}}},
	}
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	tbl := body.Children[0]
	if len(tbl.Children) != 2 {
		t.Fatalf("table rows = %d, want 2", len(tbl.Children))
	}
	// row 0 keeps its cell with RowSpan 2; row 1's continue cell is dropped.
	if got := tbl.Children[0].Children[0].RowSpan; got != 2 {
		t.Fatalf("restart cell RowSpan = %d, want 2", got)
	}
	if n := len(tbl.Children[1].Children); n != 0 {
		t.Fatalf("continue row cells = %d, want 0 (dropped)", n)
	}
}

// paraWith is a test helper building a one-run paragraph (empty text -> no run).
func paraWith(text string) *docx.Paragraph {
	p := &docx.Paragraph{}
	if text != "" {
		p.Content = []docx.ParaChild{{Run: &docx.Run{Text: text}}}
	}
	return p
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx/cssbox -run 'TestLowerTableStructure|TestLowerTableVMergeToRowSpan' -v`
Expected: FAIL — `Lower` skips `blk.Table` (nil-paragraph blocks are `continue`d), so `body.Children` is empty.

- [ ] **Step 3a: Create pkg/docx/cssbox/table.go**

```go
package cssbox

import (
	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// lowerTable lowers a DOCX table into a cssbox DisplayTable subtree the CSS table
// layout engine consumes. Rows become DisplayTableRow boxes; cells become
// DisplayTableCell boxes carrying ColSpan (w:gridSpan) and RowSpan (derived from
// w:vMerge). Cell content lowers recursively via lowerBlocks. Borders, shading,
// and width map onto the table/cell ComputedStyle.
func lowerTable(tb *docx.Table, r *style.Resolver) *lcssbox.Box {
	table := &lcssbox.Box{
		Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayTable, Formatting: lcssbox.TableFC,
		Style: tableStyle(tb.Props, tb.Grid),
	}
	rowSpans := computeRowSpans(tb)
	for ri, row := range tb.Rows {
		rowBox := &lcssbox.Box{
			Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayTableRow, Formatting: lcssbox.TableFC,
			Style: gcss.InitialStyle(),
		}
		col := 0
		for ci, cell := range row.Cells {
			span := cell.GridSpan
			if span < 1 {
				span = 1
			}
			if cell.VMerge == docx.VMergeContinue {
				// Covered by the restart cell's RowSpan; drop it (advance the grid
				// column so a later cell in the row still lands correctly).
				col += span
				continue
			}
			cellBox := &lcssbox.Box{
				Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayTableCell, Formatting: lcssbox.BlockFC,
				Style:   cellStyle(cell.Props),
				ColSpan: span,
				RowSpan: rowSpans[cellKey{ri, ci}],
			}
			cellBox.Children = lowerBlocks(cell.Blocks, r)
			rowBox.Children = append(rowBox.Children, cellBox)
			col += span
		}
		table.Children = append(table.Children, rowBox)
	}
	return table
}

// cellKey identifies a cell by (rowIndex, cellIndexInRow) for the rowspan map.
type cellKey struct{ row, cell int }

// computeRowSpans resolves each restart cell's RowSpan = 1 + the number of
// continue cells directly below it in the same visual grid column. It tracks the
// running grid column of each cell (honoring gridSpan) so a continue cell is
// matched to the restart above it by column, per OOXML vMerge semantics.
func computeRowSpans(tb *docx.Table) map[cellKey]int {
	spans := map[cellKey]int{}
	// active maps a grid column -> the (row,cell) of the restart cell currently
	// open in that column.
	active := map[int]cellKey{}
	for ri, row := range tb.Rows {
		col := 0
		for ci, cell := range row.Cells {
			span := cell.GridSpan
			if span < 1 {
				span = 1
			}
			switch cell.VMerge {
			case docx.VMergeRestart:
				active[col] = cellKey{ri, ci}
				spans[cellKey{ri, ci}] = 1
			case docx.VMergeContinue:
				if k, ok := active[col]; ok {
					spans[k]++
				}
			}
			col += span
		}
	}
	return spans
}

// tableStyle maps table-level props onto a block ComputedStyle: width (dxa ->
// pt), border-collapse (DOCX tables collapse borders like Word's default grid),
// the four table borders, and background shading.
func tableStyle(p docx.TableProps, grid []docx.Twips) gcss.ComputedStyle {
	cs := gcss.InitialStyle()
	cs.Display = "table"
	cs.BorderCollapse = "collapse"
	switch {
	case p.WidthDxa > 0:
		cs.Width = pt(docx.Twips(p.WidthDxa).Points())
	case p.WidthPct > 0:
		cs.Width = gcss.Length{Value: float64(p.WidthPct) / 50, Unit: gcss.UnitPct}
	}
	applyBorders(&cs, p.Borders)
	if p.Shading.HasFill {
		cs.BackgroundColor = p.Shading.Fill
	}
	return cs
}

// cellStyle maps cell props onto a block ComputedStyle: width, vertical-align,
// borders, and shading.
func cellStyle(p docx.CellProps) gcss.ComputedStyle {
	cs := gcss.InitialStyle()
	cs.Display = "table-cell"
	if p.WidthDxa > 0 {
		cs.Width = pt(docx.Twips(p.WidthDxa).Points())
	}
	cs.VerticalAlign = vAlignString(p.VAlign)
	applyBorders(&cs, p.Borders)
	if p.Shading.HasFill {
		cs.BackgroundColor = p.Shading.Fill
	}
	return cs
}

// applyBorders maps a BoxBorders onto the per-edge ComputedStyle border fields.
// A no-border edge is left at the initial "none"; an edge with a size becomes a
// solid border whose width is sz/8 pt (OOXML w:sz is eighths of a point).
func applyBorders(cs *gcss.ComputedStyle, b docx.BoxBorders) {
	applyEdge(&cs.BorderTopWidth, &cs.BorderTopStyle, &cs.BorderTopColor, b.Top)
	applyEdge(&cs.BorderBottomWidth, &cs.BorderBottomStyle, &cs.BorderBottomColor, b.Bottom)
	applyEdge(&cs.BorderLeftWidth, &cs.BorderLeftStyle, &cs.BorderLeftColor, b.Left)
	applyEdge(&cs.BorderRightWidth, &cs.BorderRightStyle, &cs.BorderRightColor, b.Right)
}

func applyEdge(width *gcss.Length, style *string, col *color.RGBA, e docx.Border) {
	if e.None || e.SizeEighthPt == 0 {
		return
	}
	*width = pt(float64(e.SizeEighthPt) / 8)
	*style = "solid"
	if e.HasColor {
		*col = e.Color
	}
}

// vAlignString maps a CellVAlign onto the CSS vertical-align keyword.
func vAlignString(v docx.CellVAlign) string {
	switch v {
	case docx.VAlignCenter:
		return "middle"
	case docx.VAlignBottom:
		return "bottom"
	default:
		return "top"
	}
}
```

Add `"image/color"` to `table.go`'s imports (the `applyEdge` `*color.RGBA` parameter).

- [ ] **Step 3b: Add lowerBlocks + dispatch the table in Lower**

In `pkg/docx/cssbox/lower.go`, replace the body loop in `Lower`. Replace:

```go
	for _, blk := range d.Body {
		if blk.Paragraph == nil {
			continue
		}
		body.Children = append(body.Children, lowerParagraph(blk.Paragraph, r)...)
	}
	return root
```

with:

```go
	body.Children = lowerBlocks(d.Body, r)
	return root
```

Then add `lowerBlocks` (place it right after `Lower`):

```go
// lowerBlocks lowers a sequence of DOCX blocks (paragraphs and tables) into
// cssbox boxes. It is shared by the document body and by table cells (cell
// content recursion).
func lowerBlocks(blocks []docx.Block, r *style.Resolver) []*lcssbox.Box {
	var out []*lcssbox.Box
	for _, blk := range blocks {
		switch {
		case blk.Paragraph != nil:
			out = append(out, lowerParagraph(blk.Paragraph, r)...)
		case blk.Table != nil:
			out = append(out, lowerTable(blk.Table, r))
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx/cssbox -run 'TestLowerTableStructure|TestLowerTableVMergeToRowSpan|TestLowerParagraphReadsContent' -v`
Expected: PASS. Then `go build ./...` — no errors.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/cssbox/table.go pkg/docx/cssbox/lower.go pkg/docx/cssbox/table_test.go
git commit -m "docx/cssbox: lower w:tbl into a DisplayTable subtree (spans, borders, shading)"
```

### Task 2.4: Add table fixtures + goldens

**Files:**
- Modify: `testdata/gen/docx/fixtures.go` (add `docx-table` + `docx-table-spans` to `Core`)

- [ ] **Step 1: Add the fixtures**

In `testdata/gen/docx/fixtures.go`, add two entries to the `Core` slice (after the `multipage` entry):

```go
	{
		Name:  "table",
		Desc:  "a 2x2 bordered table with shaded header cells",
		Pages: 1,
		Build: tableDocx,
	},
	{
		Name:  "table-spans",
		Desc:  "a table exercising gridSpan (colspan) and vMerge (rowspan)",
		Pages: 1,
		Build: tableSpansDocx,
	},
```

Then add the builders at the end of the file:

```go
// tblCell wraps cell content XML in a w:tc with optional tcPr (raw XML).
func tblCell(tcPr, content string) string {
	inner := ""
	if tcPr != "" {
		inner = "<w:tcPr>" + tcPr + "</w:tcPr>"
	}
	return "<w:tc>" + inner + content + "</w:tc>"
}

// tableDocx builds a 2x2 table: a shaded, bold header row over a body row, with a
// 0.5pt black grid (sz=4 eighths-of-a-point ≈ 0.5pt on every edge).
func tableDocx() []byte {
	borders := `<w:tblBorders>` +
		`<w:top w:val="single" w:sz="4" w:color="000000"/>` +
		`<w:bottom w:val="single" w:sz="4" w:color="000000"/>` +
		`<w:left w:val="single" w:sz="4" w:color="000000"/>` +
		`<w:right w:val="single" w:sz="4" w:color="000000"/>` +
		`<w:insideH w:val="single" w:sz="4" w:color="000000"/>` +
		`<w:insideV w:val="single" w:sz="4" w:color="000000"/>` +
		`</w:tblBorders>`
	tblPr := `<w:tblPr><w:tblW w:type="dxa" w:w="8000"/>` + borders + `</w:tblPr>`
	grid := `<w:tblGrid><w:gridCol w:w="4000"/><w:gridCol w:w="4000"/></w:tblGrid>`
	hdrShd := `<w:shd w:fill="D9E2F3"/>`
	row1 := "<w:tr>" +
		tblCell(hdrShd, para("", "", "Name")) +
		tblCell(hdrShd, para("", "", "Score")) + "</w:tr>"
	row2 := "<w:tr>" +
		tblCell("", para("", "", "Alice")) +
		tblCell("", para("", "", "42")) + "</w:tr>"
	tbl := "<w:tbl>" + tblPr + grid + row1 + row2 + "</w:tbl>"
	doc := docOpen + para("", "", "A table:") + tbl + docClose
	return New().SetDocument(doc).Bytes()
}

// tableSpansDocx builds a table where the first body cell spans two columns
// (gridSpan) and a header cell spans two rows (vMerge restart/continue).
func tableSpansDocx() []byte {
	borders := `<w:tblBorders>` +
		`<w:top w:val="single" w:sz="4" w:color="333333"/>` +
		`<w:bottom w:val="single" w:sz="4" w:color="333333"/>` +
		`<w:left w:val="single" w:sz="4" w:color="333333"/>` +
		`<w:right w:val="single" w:sz="4" w:color="333333"/>` +
		`<w:insideH w:val="single" w:sz="4" w:color="333333"/>` +
		`<w:insideV w:val="single" w:sz="4" w:color="333333"/>` +
		`</w:tblBorders>`
	tblPr := `<w:tblPr><w:tblW w:type="dxa" w:w="8000"/>` + borders + `</w:tblPr>`
	grid := `<w:tblGrid><w:gridCol w:w="2666"/><w:gridCol w:w="2667"/><w:gridCol w:w="2667"/></w:tblGrid>`
	// Row 1: a vMerge-restart cell in col 0, then two normal cells.
	row1 := "<w:tr>" +
		tblCell(`<w:vMerge w:val="restart"/>`, para("", "", "Merged")) +
		tblCell("", para("", "", "B")) +
		tblCell("", para("", "", "C")) + "</w:tr>"
	// Row 2: the vMerge-continue cell (col 0, covered), then a gridSpan=2 cell.
	row2 := "<w:tr>" +
		tblCell(`<w:vMerge w:val="continue"/>`, para("", "", "")) +
		tblCell(`<w:gridSpan w:val="2"/>`, para("", "", "Spans two columns")) + "</w:tr>"
	tbl := "<w:tbl>" + tblPr + grid + row1 + row2 + "</w:tbl>"
	doc := docOpen + tbl + docClose
	return New().SetDocument(doc).Bytes()
}
```

Note the fixtures include `w:insideH`/`w:insideV` in `tblBorders`; the parser ignores those two edges (only top/bottom/left/right/start/end are read) — that is fine, they degrade gracefully and the outer grid still renders. The engine's `border-collapse` draws the inter-cell lines from the per-cell edges.

- [ ] **Step 2: Generate the goldens**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -update`
Expected: writes `pkg/doctaculous/testdata/golden/docx-table.png` and `docx-table-spans.png`, logs "updated ...".

- [ ] **Step 3: Eyeball the goldens**

Open both PNGs (`pkg/doctaculous/testdata/golden/docx-table.png`, `docx-table-spans.png`). Verify by eye:
- `docx-table`: a 2-column table, header row shaded light blue, thin black grid lines on all cell edges, "Name"/"Score" over "Alice"/"42".
- `docx-table-spans`: the "Merged" cell is one tall cell spanning both rows in column 0; "Spans two columns" occupies the full width of columns 2–3 in row 2.

If either looks wrong, the bug is in parsing/lowering — fix it, do NOT accept a broken golden.

- [ ] **Step 4: Run the golden test for real (no -update)**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -v`
Expected: PASS for all six fixtures (four original byte-identical + two new).

- [ ] **Step 5: Commit**

```bash
git add testdata/gen/docx/fixtures.go pkg/doctaculous/testdata/golden/docx-table.png pkg/doctaculous/testdata/golden/docx-table-spans.png
git commit -m "docx: table fixtures + goldens (grid, shading, colspan, rowspan)"
```

### Task 2.5: Verify + PR

- [ ] **Step 1: Full suite + vet + lint + race**

Run: `go test ./... && go vet ./... && golangci-lint run && go test -race ./pkg/docx/... ./pkg/doctaculous`
Expected: all pass. Confirm the four original DOCX goldens are unchanged (`git status` shows only the two new PNGs added, none modified).

- [ ] **Step 2: Open the PR**

```bash
git push -u origin docx-fidelity-2-tables
gh pr create --title "docx: table layout (phase 2 of DOCX fidelity)" \
  --body "Parse w:tbl (grid, rows, cells, gridSpan, vMerge, borders, shading, width) and lower into a cssbox DisplayTable subtree — the CSS table engine does the layout. New docx-table + docx-table-spans fixtures/goldens; the four existing DOCX goldens are byte-identical. Spec: docs/superpowers/specs/2026-07-02-docx-fidelity-design.md"
```

Wait for green CI + merge before Phase 3.

---

## Phase 3 — Lists / numbering

**Branch:** `git checkout main && git pull && git checkout -b docx-fidelity-3-lists`

**Goal:** Resolve `numbering.xml`, parse `w:numPr` (numId/ilvl) on paragraphs, and lower a numbered/bulleted paragraph into a `cssbox` `DisplayListItem` box carrying a resolved `Marker`. Counter state (the sequence 1, 2, 3 …) is resolved at lowering time by walking the document in order. Add a `docx-list` fixture + golden.

**Design note on counters:** OOXML numbering is document-wide and stateful (a `numId` counts up across all paragraphs referencing it, per level). The cssbox `Marker` is a *resolved string* ("1. ", "• "), so the lowering must compute counter values itself (the CSS counter engine is not driven from DOCX). A small `listCounter` walks paragraphs in document order, incrementing per (numId, ilvl) and resetting deeper levels when a shallower one advances.

### Task 3.1: Model + parse numbering.xml

**Files:**
- Create: `pkg/docx/numbering.go` (model + `parseNumbering`)
- Modify: `pkg/docx/parse.go` `parsePackage` (resolve + parse the numbering part)
- Modify: `pkg/docx/model.go` (`Document.Numbering *Numbering`)

OOXML numbering has two layers: `w:num` instances (a `numId` → `abstractNumId`) and `w:abstractNum` definitions (per-level `w:numFmt` format + `w:lvlText` pattern). Model both; resolve `numId` → level format at lookup time.

- [ ] **Step 1: Write the failing test**

Create `pkg/docx/numbering_test.go`:

```go
package docx

import "testing"

func TestParseNumbering(t *testing.T) {
	n, err := parseNumbering([]byte(`<?xml version="1.0"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0">
    <w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="&#8226;"/></w:lvl>
    <w:lvl w:ilvl="1"><w:numFmt w:val="decimal"/><w:lvlText w:val="%2."/></w:lvl>
  </w:abstractNum>
  <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`))
	if err != nil {
		t.Fatalf("parseNumbering: %v", err)
	}
	lvl0, ok := n.Level(1, 0)
	if !ok {
		t.Fatalf("Level(1,0) not found")
	}
	if lvl0.Format != NumFmtBullet {
		t.Fatalf("lvl0 format = %v, want bullet", lvl0.Format)
	}
	lvl1, ok := n.Level(1, 1)
	if !ok || lvl1.Format != NumFmtDecimal {
		t.Fatalf("lvl1 = %+v, want decimal", lvl1)
	}
	if lvl1.Text != "%2." {
		t.Fatalf("lvl1 text = %q, want %%2.", lvl1.Text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run TestParseNumbering -v`
Expected: FAIL — `undefined: parseNumbering`, `NumFmtBullet`, etc.

- [ ] **Step 3: Create pkg/docx/numbering.go**

```go
package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// Numbering is the parsed word/numbering.xml: w:num instances (numId ->
// abstractNumId) plus w:abstractNum definitions (per-level format/text). It is
// read-only after Open.
type Numbering struct {
	// numToAbstract maps a w:numId to its w:abstractNumId.
	numToAbstract map[int]int
	// abstract maps an abstractNumId to its levels by ilvl.
	abstract map[int]map[int]NumLevel
}

// NumLevel is one list level's marker definition.
type NumLevel struct {
	Format NumFmt
	// Text is the w:lvlText pattern (e.g. "%1.", "•"). %N is replaced by level N's
	// current counter value when the marker is formatted.
	Text string
}

// NumFmt is a w:numFmt list-marker format.
type NumFmt int

const (
	NumFmtDecimal NumFmt = iota
	NumFmtBullet
	NumFmtLowerRoman
	NumFmtUpperRoman
	NumFmtLowerLetter
	NumFmtUpperLetter
	NumFmtNone
)

// Level resolves a (numId, ilvl) to its level definition. ok is false when the
// numId or level is unknown.
func (n *Numbering) Level(numID, ilvl int) (NumLevel, bool) {
	if n == nil {
		return NumLevel{}, false
	}
	absID, ok := n.numToAbstract[numID]
	if !ok {
		return NumLevel{}, false
	}
	levels, ok := n.abstract[absID]
	if !ok {
		return NumLevel{}, false
	}
	lvl, ok := levels[ilvl]
	return lvl, ok
}

// parseNumbering parses a word/numbering.xml part.
func parseNumbering(data []byte) (*Numbering, error) {
	n := &Numbering{
		numToAbstract: map[int]int{},
		abstract:      map[int]map[int]NumLevel{},
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: numbering: %v", ErrMalformedXML, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != wNS {
			continue
		}
		switch se.Name.Local {
		case "abstractNum":
			id, _ := wAttrInt(se, "abstractNumId")
			levels, err := parseAbstractNum(dec)
			if err != nil {
				return nil, err
			}
			n.abstract[id] = levels
		case "num":
			id, _ := wAttrInt(se, "numId")
			abs, err := parseNumInstance(dec)
			if err != nil {
				return nil, err
			}
			n.numToAbstract[id] = abs
		}
	}
	return n, nil
}

// parseAbstractNum reads a w:abstractNum's levels keyed by ilvl.
func parseAbstractNum(dec *xml.Decoder) (map[int]NumLevel, error) {
	levels := map[int]NumLevel{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: abstractNum: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "lvl" {
				ilvl, _ := wAttrInt(t, "ilvl")
				lvl, err := parseLvl(dec)
				if err != nil {
					return nil, err
				}
				levels[ilvl] = lvl
				continue
			}
			if err := dec.Skip(); err != nil {
				return nil, fmt.Errorf("%w: abstractNum: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "abstractNum" {
				return levels, nil
			}
		}
	}
}

// parseLvl reads one w:lvl (numFmt + lvlText).
func parseLvl(dec *xml.Decoder) (NumLevel, error) {
	var lvl NumLevel
	for {
		tok, err := dec.Token()
		if err != nil {
			return lvl, fmt.Errorf("%w: lvl: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "numFmt":
					lvl.Format = parseNumFmt(wVal(t))
				case "lvlText":
					lvl.Text = wVal(t)
				}
			}
			if err := dec.Skip(); err != nil {
				return lvl, fmt.Errorf("%w: lvl: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "lvl" {
				return lvl, nil
			}
		}
	}
}

// parseNumInstance reads a w:num, returning its abstractNumId.
func parseNumInstance(dec *xml.Decoder) (int, error) {
	abs := -1
	for {
		tok, err := dec.Token()
		if err != nil {
			return abs, fmt.Errorf("%w: num: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "abstractNumId" {
				if v, ok := wAttrInt(t, "val"); ok {
					abs = v
				}
			}
			if err := dec.Skip(); err != nil {
				return abs, fmt.Errorf("%w: num: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "num" {
				return abs, nil
			}
		}
	}
}

// parseNumFmt maps a w:numFmt value to a NumFmt.
func parseNumFmt(val string) NumFmt {
	switch val {
	case "bullet":
		return NumFmtBullet
	case "lowerRoman":
		return NumFmtLowerRoman
	case "upperRoman":
		return NumFmtUpperRoman
	case "lowerLetter":
		return NumFmtLowerLetter
	case "upperLetter":
		return NumFmtUpperLetter
	case "none":
		return NumFmtNone
	default:
		return NumFmtDecimal
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run TestParseNumbering -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/numbering.go pkg/docx/numbering_test.go
git commit -m "docx: model + parse word/numbering.xml (abstractNum levels, num instances)"
```

### Task 3.2: Resolve numbering.xml + parse w:numPr

**Files:**
- Modify: `pkg/docx/parse.go` (`parsePackage` resolves numbering; `applyPPrChild` handles `w:numPr`)
- Modify: `pkg/docx/model.go` (`Document.Numbering`; `ParagraphProps` gains `NumID`/`ILvl`/`HasNum`)

- [ ] **Step 1: Write the failing test**

Add to `pkg/docx/numbering_test.go`:

```go
func TestParseNumPr(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:pPr><w:numPr><w:ilvl w:val="1"/><w:numId w:val="3"/></w:numPr></w:pPr><w:r><w:t>item</w:t></w:r></w:p>
</w:body></w:document>`)
	pp := doc.Body[0].Paragraph.Props
	if !pp.HasNum {
		t.Fatalf("HasNum = false, want true")
	}
	if pp.NumID != 3 || pp.ILvl != 1 {
		t.Fatalf("numPr = (numId %d, ilvl %d), want (3, 1)", pp.NumID, pp.ILvl)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run TestParseNumPr -v`
Expected: FAIL — `pp.HasNum` undefined; `w:numPr` skipped.

- [ ] **Step 3a: Extend the model**

In `pkg/docx/model.go`, add to the `Document` struct (after `Styles`):

```go
	// Numbering holds the parsed word/numbering.xml (list definitions), or nil if
	// the document has no numbering part.
	Numbering *Numbering
```

Add to `ParagraphProps` (after `PageBreakBefore`):

```go
	// NumID/ILvl are the list membership from w:numPr (numId + ilvl); HasNum marks
	// them set. A paragraph with HasNum lowers to a list item.
	NumID   int
	ILvl    int
	HasNum  bool
```

- [ ] **Step 3b: Parse w:numPr**

In `pkg/docx/parse.go`, add a `numPr` case to `applyPPrChild`. Because `w:numPr` has children (`w:ilvl`, `w:numId`), it can't be handled by the self-closing `applyPPrChild`; handle it in `parsePPr` like `sectPr`. In `parsePPr`, the loop currently calls `applyPPrChild` then checks for `sectPr`. Add a `numPr` branch. Replace:

```go
			if t.Name.Space == wNS {
				applyPPrChild(&props, t)
				if t.Name.Local == "sectPr" {
					s, err := parseSectPr(dec)
					if err != nil {
						return props, nil, err
					}
					sect = &s
					continue
				}
			}
```

with:

```go
			if t.Name.Space == wNS {
				applyPPrChild(&props, t)
				switch t.Name.Local {
				case "sectPr":
					s, err := parseSectPr(dec)
					if err != nil {
						return props, nil, err
					}
					sect = &s
					continue
				case "numPr":
					applyNumPr(&props, dec)
					continue
				}
			}
```

Add the `applyNumPr` helper near `applyPPrChild`:

```go
// applyNumPr reads a w:numPr's w:ilvl and w:numId children into the paragraph's
// list membership. A numPr with a numId (even without an explicit ilvl, which
// defaults to 0) marks the paragraph as a list item.
func applyNumPr(props *ParagraphProps, dec *xml.Decoder) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "ilvl":
					if v, ok := wAttrInt(t, "val"); ok {
						props.ILvl = v
					}
				case "numId":
					if v, ok := wAttrInt(t, "val"); ok {
						props.NumID = v
						props.HasNum = true
					}
				}
			}
			_ = dec.Skip()
		case xml.EndElement:
			if t.Name.Local == "numPr" {
				return
			}
		}
	}
}
```

- [ ] **Step 3c: Resolve the numbering part in parsePackage**

In `pkg/docx/parse.go`, in `parsePackage`, after the styles block (before `return doc, nil`), add:

```go
	// Numbering part: prefer the relationship target, fall back to the convention.
	numName := resolveNumberingPart(pkg, mainName)
	if data, ok := pkg.part(numName); ok {
		num, err := parseNumbering(data)
		if err != nil {
			return nil, err
		}
		doc.Numbering = num
	}
```

Add the resolver near `resolveStylesPart`:

```go
// resolveNumberingPart finds the numbering part name via the main document's
// relationships, falling back to word/numbering.xml.
func resolveNumberingPart(pkg *pkgReader, mainName string) string {
	const numType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"
	rels := pkg.relsForByType(mainName, numType)
	if rels != "" {
		return rels
	}
	return "word/numbering.xml"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run 'TestParseNumPr|TestParseNumbering' -v`
Expected: PASS. Full package: `go test ./pkg/docx`.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/parse.go pkg/docx/model.go pkg/docx/numbering_test.go
git commit -m "docx: parse w:numPr + resolve word/numbering.xml part"
```

### Task 3.3: Lower list paragraphs into DisplayListItem boxes

**Files:**
- Create: `pkg/docx/cssbox/list.go` (`listCounter`, `markerText`, `lowerListParagraph`)
- Modify: `pkg/docx/cssbox/lower.go` (thread a `listCounter` through `lowerBlocks`; route list paragraphs)

A paragraph with `HasNum` becomes a `DisplayListItem` box with a resolved `Marker`. The engine renders a `Marker` by prepending its `Text` as the item's leading inline child (per the cssbox `Marker` doc comment), so we set `Marker: &lcssbox.MarkerContent{Text: "1. ", Outside: true}` and otherwise build the item like a paragraph block. Counter state is held in a `listCounter` that the top-level `lowerBlocks` walk threads through (so numbering counts across the document; nested cells get a fresh counter — DOCX numbering rarely spans into cells, and a per-cell counter is the graceful choice).

- [ ] **Step 1: Write the failing test**

Create `pkg/docx/cssbox/list_test.go`:

```go
package cssbox

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func numberedDoc() *docx.Document {
	num, _ := docxParseNumbering()
	return &docx.Document{
		Section:   docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Numbering: num,
		Body: []docx.Block{
			listItemBlock(1, 0, "first"),
			listItemBlock(1, 0, "second"),
		},
	}
}

func listItemBlock(numID, ilvl int, text string) docx.Block {
	return docx.Block{Paragraph: &docx.Paragraph{
		Props:   docx.ParagraphProps{NumID: numID, ILvl: ilvl, HasNum: true},
		Content: []docx.ParaChild{{Run: &docx.Run{Text: text}}},
	}}
}

func TestLowerDecimalListNumbersIncrement(t *testing.T) {
	d := numberedDoc()
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	if len(body.Children) != 2 {
		t.Fatalf("body children = %d, want 2 list items", len(body.Children))
	}
	i0, i1 := body.Children[0], body.Children[1]
	if i0.Display != lcssbox.DisplayListItem {
		t.Fatalf("item0 Display = %v, want DisplayListItem", i0.Display)
	}
	if i0.Marker == nil || i0.Marker.Text != "1. " {
		t.Fatalf("item0 Marker = %+v, want '1. '", i0.Marker)
	}
	if i1.Marker == nil || i1.Marker.Text != "2. " {
		t.Fatalf("item1 Marker = %+v, want '2. '", i1.Marker)
	}
}

func TestLowerBulletListMarker(t *testing.T) {
	// numId 2 -> a bullet abstract; build a numbering with a bullet level.
	num, _ := docxParseBulletNumbering()
	d := &docx.Document{
		Section:   docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Numbering: num,
		Body:      []docx.Block{listItemBlock(2, 0, "bulleted")},
	}
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	if got := body.Children[0].Marker.Text; got != "• " {
		t.Fatalf("bullet marker = %q, want '• '", got)
	}
}
```

Add the parse helpers at the bottom of `list_test.go` (they build test `Numbering` via the exported parser used in Task 3.1):

```go
func docxParseNumbering() (*docx.Numbering, error) {
	return docx.ParseNumberingForTest([]byte(`<?xml version="1.0"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl></w:abstractNum>
  <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`))
}

func docxParseBulletNumbering() (*docx.Numbering, error) {
	return docx.ParseNumberingForTest([]byte(`<?xml version="1.0"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="&#8226;"/></w:lvl></w:abstractNum>
  <w:num w:numId="2"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`))
}
```

Because `parseNumbering` is unexported, add a tiny exported test shim to `pkg/docx/numbering.go`:

```go
// ParseNumberingForTest exposes parseNumbering to external test packages
// (pkg/docx/cssbox). It is not part of the stable API.
func ParseNumberingForTest(data []byte) (*Numbering, error) { return parseNumbering(data) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx/cssbox -run 'TestLowerDecimalListNumbersIncrement|TestLowerBulletListMarker' -v`
Expected: FAIL — list paragraphs currently lower as plain paragraphs (no `Marker`, `Display` is `DisplayBlock`).

- [ ] **Step 3a: Create pkg/docx/cssbox/list.go**

```go
package cssbox

import (
	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// listCounter tracks per-(numId, ilvl) counter values as paragraphs are lowered
// in document order. Advancing a level resets all deeper levels (CSS/Word list
// nesting semantics).
type listCounter struct {
	// counts[numID][ilvl] = current value at that level.
	counts map[int]map[int]int
}

func newListCounter() *listCounter { return &listCounter{counts: map[int]map[int]int{}} }

// next increments the (numID, ilvl) counter, resets deeper levels, and returns
// the new value at ilvl.
func (c *listCounter) next(numID, ilvl int) int {
	m := c.counts[numID]
	if m == nil {
		m = map[int]int{}
		c.counts[numID] = m
	}
	m[ilvl]++
	for deeper := range m {
		if deeper > ilvl {
			delete(m, deeper)
		}
	}
	return m[ilvl]
}

// lowerListParagraph lowers a numbered paragraph into a DisplayListItem box with
// a resolved marker. It reuses lowerParagraph for the item's inline content, then
// overlays list-item display + the marker. A page break inside the paragraph
// (multiple blocks) keeps the marker only on the first block.
func lowerListParagraph(p *docx.Paragraph, r *style.Resolver, num *docx.Numbering, ctr *listCounter) []*lcssbox.Box {
	blocks := lowerParagraph(p, r)
	if len(blocks) == 0 {
		return blocks
	}
	first := blocks[0]
	first.Display = lcssbox.DisplayListItem
	first.Style.Display = "list-item"
	first.Marker = &lcssbox.MarkerContent{Text: markerText(p.Props, num, ctr), Outside: true}
	return blocks
}

// markerText resolves a paragraph's list marker string ("1. ", "• ", "a. ").
// The counter is advanced for numbered formats; a bullet uses the lvlText glyph
// verbatim. An unknown numId falls back to a bullet.
func markerText(pp docx.ParagraphProps, num *docx.Numbering, ctr *listCounter) string {
	lvl, ok := num.Level(pp.NumID, pp.ILvl)
	if !ok {
		return "• "
	}
	switch lvl.Format {
	case docx.NumFmtBullet:
		glyph := lvl.Text
		if glyph == "" {
			glyph = "•"
		}
		return glyph + " "
	case docx.NumFmtNone:
		return ""
	default:
		val := ctr.next(pp.NumID, pp.ILvl)
		return formatMarker(lvl, val)
	}
}

// formatMarker substitutes the level's counter value into the lvlText pattern.
// OOXML lvlText uses %N placeholders (N = 1-based level); we resolve %(ilvl+1)
// with the current value formatted per the level's numFmt, and append a trailing
// space so the marker reads as "1. ". Other %M placeholders (parent levels) are
// dropped in this slice (multi-level "1.2." numbering is a follow-up).
func formatMarker(lvl docx.NumLevel, value int) string {
	num := gcss.FormatCounter(value, cssListStyle(lvl.Format))
	// Replace the first %N run in the pattern with the formatted number; keep the
	// literal suffix (e.g. the "." in "%1.").
	out := replaceFirstPlaceholder(lvl.Text, num)
	if out == "" {
		out = num + "."
	}
	return out + " "
}

// replaceFirstPlaceholder replaces the first "%<digit>" token in pattern with num,
// leaving surrounding literals intact. If there is no placeholder, returns "".
func replaceFirstPlaceholder(pattern, num string) string {
	for i := 0; i+1 < len(pattern); i++ {
		if pattern[i] == '%' && pattern[i+1] >= '0' && pattern[i+1] <= '9' {
			return pattern[:i] + num + pattern[i+2:]
		}
	}
	return ""
}

// cssListStyle maps a DOCX NumFmt onto the CSS list-style-type keyword that
// pkg/css.FormatCounter understands.
func cssListStyle(f docx.NumFmt) string {
	switch f {
	case docx.NumFmtLowerRoman:
		return "lower-roman"
	case docx.NumFmtUpperRoman:
		return "upper-roman"
	case docx.NumFmtLowerLetter:
		return "lower-alpha"
	case docx.NumFmtUpperLetter:
		return "upper-alpha"
	default:
		return "decimal"
	}
}
```

- [ ] **Step 3b: Thread the counter through lowering**

In `pkg/docx/cssbox/lower.go`, update `lowerBlocks` to carry the numbering + counter and route list paragraphs. Replace the whole `lowerBlocks` func:

```go
// lowerBlocks lowers a sequence of DOCX blocks (paragraphs, list items, tables).
// num is the document's numbering (may be nil); ctr threads list-counter state.
func lowerBlocks(blocks []docx.Block, r *style.Resolver, num *docx.Numbering, ctr *listCounter) []*lcssbox.Box {
	var out []*lcssbox.Box
	for _, blk := range blocks {
		switch {
		case blk.Paragraph != nil:
			if blk.Paragraph.Props.HasNum && num != nil {
				out = append(out, lowerListParagraph(blk.Paragraph, r, num, ctr)...)
			} else {
				out = append(out, lowerParagraph(blk.Paragraph, r)...)
			}
		case blk.Table != nil:
			out = append(out, lowerTable(blk.Table, r, num))
		}
	}
	return out
}
```

(Note: `lowerTable` returns a single `*lcssbox.Box`, so the table case appends it directly — no `...` spread, unlike the paragraph cases which return slices.)

Update the `Lower` call site. Replace:

```go
	body.Children = lowerBlocks(d.Body, r)
	return root
```

with:

```go
	body.Children = lowerBlocks(d.Body, r, d.Numbering, newListCounter())
	return root
```

Update `lowerTable`'s signature in `pkg/docx/cssbox/table.go` to accept and forward `num` (a fresh counter per cell). Change its signature line:

```go
func lowerTable(tb *docx.Table, r *style.Resolver, num *docx.Numbering) *lcssbox.Box {
```

Inside `lowerTable`, change the cell-content recursion call so nested list paragraphs in a cell get a counter. Replace:

```go
			cellBox.Children = lowerBlocks(cell.Blocks, r)
```

with:

```go
			cellBox.Children = lowerBlocks(cell.Blocks, r, num, newListCounter())
```

(Update `table_test.go`'s direct `lowerTable` calls if any — the tests call `Lower`, not `lowerTable`, so no change needed.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx/cssbox -run 'TestLowerDecimalListNumbersIncrement|TestLowerBulletListMarker|TestLowerTableStructure' -v`
Expected: PASS. Then `go build ./...`.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/cssbox/list.go pkg/docx/cssbox/lower.go pkg/docx/cssbox/table.go pkg/docx/cssbox/list_test.go pkg/docx/numbering.go
git commit -m "docx/cssbox: lower numbered/bulleted paragraphs into DisplayListItem with markers"
```

### Task 3.4: Builder.SetNumbering + list fixture + golden

**Files:**
- Modify: `testdata/gen/docx/docx.go` (`Builder.SetNumbering`; write the part + rel + content-type)
- Modify: `testdata/gen/docx/fixtures.go` (add `docx-list`)

- [ ] **Step 1: Extend the Builder to write a numbering part**

In `testdata/gen/docx/docx.go`, add a `numberingXML` field and setter, and write the part. Change the `Builder` struct:

```go
type Builder struct {
	documentXML  string
	stylesXML    string
	numberingXML string
}
```

Add the setter after `SetStyles`:

```go
// SetNumbering sets the word/numbering.xml part. When empty, no numbering part is
// written.
func (b *Builder) SetNumbering(xml string) *Builder {
	b.numberingXML = xml
	return b
}
```

In `Bytes`, write the numbering part and its rel. After the styles write block, add:

```go
	if b.numberingXML != "" {
		write("word/numbering.xml", b.numberingXML)
	}
```

Update `contentTypes` and `docRels` to take the numbering flag. Change the `write("[Content_Types].xml", ...)` and `write("word/_rels/document.xml.rels", ...)` calls:

```go
	write("[Content_Types].xml", contentTypes(b.stylesXML != "", b.numberingXML != ""))
	write("_rels/.rels", rootRels)
	write("word/_rels/document.xml.rels", docRels(b.stylesXML != "", b.numberingXML != ""))
```

Change `contentTypes`:

```go
func contentTypes(withStyles, withNumbering bool) string {
	s := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`
	if withStyles {
		s += `
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>`
	}
	if withNumbering {
		s += `
  <Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>`
	}
	return s + "\n</Types>"
}
```

Change `docRels`:

```go
func docRels(withStyles, withNumbering bool) string {
	s := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	if withStyles {
		s += `
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`
	}
	if withNumbering {
		s += `
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>`
	}
	return s + "\n</Relationships>"
}
```

- [ ] **Step 2: Add the list fixture**

In `testdata/gen/docx/fixtures.go`, add to `Core`:

```go
	{
		Name:  "list",
		Desc:  "an ordered (decimal) list and an unordered (bullet) list",
		Pages: 1,
		Build: listDocx,
	},
```

Add the builder + helper:

```go
// listItem wraps text in a paragraph carrying a w:numPr (numId + ilvl).
func listItem(numID, ilvl int, text string) string {
	return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + itoa(ilvl) + `"/><w:numId w:val="` + itoa(numID) + `"/></w:numPr></w:pPr>` +
		`<w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

func listDocx() []byte {
	doc := docOpen +
		para("", "", "Ordered:") +
		listItem(1, 0, "First item") +
		listItem(1, 0, "Second item") +
		listItem(1, 0, "Third item") +
		para("", "", "Unordered:") +
		listItem(2, 0, "Bullet one") +
		listItem(2, 0, "Bullet two") +
		docClose
	return New().SetDocument(doc).SetNumbering(listNumbering).Bytes()
}

// listNumbering defines a decimal list (numId 1) and a bullet list (numId 2).
const listNumbering = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0">
    <w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl>
  </w:abstractNum>
  <w:abstractNum w:abstractNumId="1">
    <w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="&#8226;"/></w:lvl>
  </w:abstractNum>
  <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
  <w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num>
</w:numbering>`
```

Add the `itoa` helper if the file lacks one (grep `grep -n "func itoa" testdata/gen/docx/*.go`; if absent):

```go
// itoa is strconv.Itoa; a tiny local alias keeps the fixture strings readable.
func itoa(n int) string { return strconv.Itoa(n) }
```

and add `"strconv"` to the `fixtures.go` imports (it currently imports only `"strings"`).

- [ ] **Step 3: Generate + eyeball the golden**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -update`
Then open `pkg/doctaculous/testdata/golden/docx-list.png`. Verify: "Ordered:" followed by "1. First item / 2. Second item / 3. Third item", then "Unordered:" with "• Bullet one / • Bullet two", each marker in the left gutter with hanging indent.

- [ ] **Step 4: Run the golden test for real**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -v`
Expected: PASS for all seven fixtures; the six prior goldens byte-identical.

- [ ] **Step 5: Commit**

```bash
git add testdata/gen/docx/docx.go testdata/gen/docx/fixtures.go pkg/doctaculous/testdata/golden/docx-list.png
git commit -m "docx: list fixture + golden (decimal + bullet); Builder.SetNumbering"
```

### Task 3.5: Verify + PR

- [ ] **Step 1: Full suite + vet + lint + race**

Run: `go test ./... && go vet ./... && golangci-lint run && go test -race ./pkg/docx/... ./pkg/doctaculous`
Expected: all pass; prior DOCX goldens unchanged.

- [ ] **Step 2: Open the PR**

```bash
git push -u origin docx-fidelity-3-lists
gh pr create --title "docx: lists / numbering (phase 3 of DOCX fidelity)" \
  --body "Resolve word/numbering.xml, parse w:numPr, and lower numbered/bulleted paragraphs into cssbox DisplayListItem boxes with resolved markers (decimal/roman/alpha/bullet). New docx-list fixture/golden; prior goldens byte-identical. Spec: docs/superpowers/specs/2026-07-02-docx-fidelity-design.md"
```

Wait for green CI + merge before Phase 4.

---

## Phase 4 — Images + hyperlinks

**Branch:** `git checkout main && git pull && git checkout -b docx-fidelity-4-media`

**Goal:** Parse `w:hyperlink` (→ a `Hyperlink` ParaChild, target resolved via document rels) and `w:drawing` (→ a `Drawing` ParaChild referencing an image part). Lower a hyperlink into an inline link box and a drawing into a `BoxReplaced` image, decoded through a `resource.MapLoader` built from the document's `word/media/*` parts. Add `docx-hyperlink` + `docx-image` fixtures + goldens.

### Task 4.1: Parse w:hyperlink + expose document rels

**Files:**
- Modify: `pkg/docx/parse.go` (parse `w:hyperlink` inside a paragraph; collect the document part's rels)
- Modify: `pkg/docx/model.go` (`Document.Rels map[string]Relationship`)

A `w:hyperlink` carries an `r:id` (relationship to an external URL) or a `w:anchor` (internal bookmark). Resolving `r:id` → URL needs the document part's relationships, which the parser does not currently keep (only styles/numbering are resolved by type). Collect the full rels map at parse time and store it on `Document`.

- [ ] **Step 1: Write the failing test**

Add `pkg/docx/parse_hyperlink_test.go`:

```go
package docx

import "testing"

func TestParseHyperlinkStructure(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>
<w:p>
  <w:r><w:t>See </w:t></w:r>
  <w:hyperlink r:id="rId5"><w:r><w:t>the site</w:t></w:r></w:hyperlink>
</w:p>
</w:body></w:document>`)
	c := doc.Body[0].Paragraph.Content
	if len(c) != 2 {
		t.Fatalf("content = %d, want 2 (run + hyperlink)", len(c))
	}
	if c[0].Run == nil || c[0].Run.Text != "See " {
		t.Fatalf("content[0] = %+v, want run 'See '", c[0])
	}
	h := c[1].Hyperlink
	if h == nil {
		t.Fatalf("content[1].Hyperlink = nil, want a hyperlink")
	}
	if h.RelID() != "rId5" {
		t.Fatalf("hyperlink relID = %q, want rId5", h.RelID())
	}
	if len(h.Runs) != 1 || h.Runs[0].Text != "the site" {
		t.Fatalf("hyperlink runs = %+v, want ['the site']", h.Runs)
	}
}
```

Note: the parser stores the raw `r:id` on the `Hyperlink`; the URL is resolved from `Document.Rels` at lowering time (Task 4.2). To keep the model self-contained, store the rel id in the existing `Target` field's raw form via a helper. Simpler: add a `relID` field. Adjust the `Hyperlink` type (Phase 1 declared `Target`/`Anchor`/`Runs`): add an unexported `relID` plus an exported `RelID()` accessor, OR store the id in `Target` pre-resolution. This plan uses a dedicated field — update the type in Step 3a.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run TestParseHyperlinkStructure -v`
Expected: FAIL — `w:hyperlink` is skipped by `parseParagraph`'s default case; `Hyperlink.RelID` undefined.

- [ ] **Step 3a: Extend the model**

In `pkg/docx/model.go`, update the `Hyperlink` type (replace the Phase 1 declaration):

```go
// Hyperlink is a w:hyperlink: a group of runs linking to an external URL
// (resolved from RelID via the document relationships) or an internal Anchor
// (bookmark). Target is populated at lowering time from RelID; the parser sets
// only relID + Anchor + Runs.
type Hyperlink struct {
	relID  string
	Anchor string
	Target string
	Runs   []Run
}

// RelID returns the r:id relationship id referencing the external target, or "".
func (h *Hyperlink) RelID() string { return h.relID }

// SetRelID sets the relationship id (used by the parser).
func (h *Hyperlink) SetRelID(id string) { h.relID = id }
```

Add to the `Document` struct (after `Numbering`):

```go
	// Rels maps a relationship id (r:id) to its target for the main document part
	// (external hyperlink URLs, image parts). Empty if the document has no rels.
	Rels map[string]Relationship
```

Add the `Relationship` type (near `Styles`):

```go
// Relationship is one document relationship (Id -> Target), with the external
// flag set when TargetMode="External" (hyperlinks to URLs).
type Relationship struct {
	ID       string
	Target   string
	External bool
}
```

- [ ] **Step 3b: Parse the hyperlink element**

In `pkg/docx/parse.go`, add a `w:hyperlink` case to `parseParagraph`'s inner switch (alongside `pPr`/`r`). Add:

```go
			case "hyperlink":
				h, err := parseHyperlink(dec, t)
				if err != nil {
					return nil, nil, err
				}
				p.Content = append(p.Content, ParaChild{Hyperlink: h})
```

Add the parser:

```go
// parseHyperlink consumes a w:hyperlink: its runs plus the r:id / w:anchor
// attributes. start is the already-read start element (carrying the attributes).
func parseHyperlink(dec *xml.Decoder, start xml.StartElement) (*Hyperlink, error) {
	h := &Hyperlink{}
	if id, ok := rAttr(start, "id"); ok {
		h.SetRelID(id)
	}
	if anchor, ok := wAttr(start, "anchor"); ok {
		h.Anchor = anchor
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: hyperlink: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "r" {
				runs, err := parseRun(dec)
				if err != nil {
					return nil, err
				}
				h.Runs = append(h.Runs, runs...)
				continue
			}
			if err := dec.Skip(); err != nil {
				return nil, fmt.Errorf("%w: hyperlink: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "hyperlink" {
				return h, nil
			}
		}
	}
}

// rNS is the officeDocument relationships namespace (the r: prefix).
const rNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

// rAttr returns an r-namespaced attribute by local name (e.g. r:id).
func rAttr(e xml.StartElement, local string) (string, bool) {
	for _, a := range e.Attr {
		if a.Name.Local == local && (a.Name.Space == rNS || a.Name.Space == "") {
			return a.Value, true
		}
	}
	return "", false
}
```

- [ ] **Step 3c: Collect the document rels in parsePackage**

In `pkg/docx/parse.go`, in `parsePackage`, after resolving numbering, collect the full rels map for the main part:

```go
	doc.Rels = pkg.allRels(mainName)
```

Add the `allRels` method near `relsForByType`:

```go
// allRels returns every relationship for a source part, keyed by id, with targets
// resolved relative to the part's directory for internal (package) targets and
// left verbatim for external ones.
func (p *pkgReader) allRels(partName string) map[string]Relationship {
	partName = cleanPart(partName)
	dir, base := splitPart(partName)
	relsName := joinPart(dir, "_rels", base+".rels")
	data, ok := p.part(relsName)
	if !ok {
		return nil
	}
	var doc struct {
		Rels []struct {
			ID         string `xml:"Id,attr"`
			Target     string `xml:"Target,attr"`
			TargetMode string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	out := make(map[string]Relationship, len(doc.Rels))
	for _, r := range doc.Rels {
		external := r.TargetMode == "External"
		target := r.Target
		if !external {
			target = joinPart(dir, r.Target)
		}
		out[r.ID] = Relationship{ID: r.ID, Target: target, External: external}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run TestParseHyperlinkStructure -v`
Expected: PASS. Full package: `go test ./pkg/docx`.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/parse.go pkg/docx/model.go pkg/docx/parse_hyperlink_test.go
git commit -m "docx: parse w:hyperlink + collect document relationships"
```

### Task 4.2: Lower a hyperlink into an inline link box

**Files:**
- Modify: `pkg/docx/cssbox/lower.go` (handle `child.Hyperlink` in the paragraph loop)

For the raster/PDF render path, a hyperlink's runs render as link-styled text: the resolved URL is not painted, but the text is colored (`#0000EE`) and underlined so the link reads as a link (matching the HTML `a:link` UA default). The URL survives on the `docx` model (`Document.Rels` + `Hyperlink.RelID`) for the future DOCX→HTML/Markdown converter, which reads the model, not this cssbox tree.

- [ ] **Step 1: Write the failing test**

Add to `pkg/docx/cssbox/lower_test.go`:

```go
func TestLowerHyperlinkStylesRuns(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{Content: []docx.ParaChild{
			{Run: &docx.Run{Text: "see "}},
			{Hyperlink: linkWith("the site")},
		}}}},
	}
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	para := body.Children[0]
	// two inline text boxes: "see " (default) and "the site" (link-styled).
	if len(para.Children) != 2 {
		t.Fatalf("paragraph inline children = %d, want 2", len(para.Children))
	}
	link := para.Children[1]
	if link.Text != "the site" {
		t.Fatalf("link text = %q, want 'the site'", link.Text)
	}
	if link.Style.TextDecorationLine != "underline" {
		t.Fatalf("link decoration = %q, want underline", link.Style.TextDecorationLine)
	}
	if link.Style.Color.B != 0xEE || link.Style.Color.R != 0x00 {
		t.Fatalf("link color = %+v, want #0000EE", link.Style.Color)
	}
}

func linkWith(text string) *docx.Hyperlink {
	h := &docx.Hyperlink{Runs: []docx.Run{{Text: text}}}
	h.SetRelID("rId5")
	return h
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx/cssbox -run TestLowerHyperlinkStylesRuns -v`
Expected: FAIL — `child.Hyperlink` is skipped (Phase 1's `if child.Run == nil { continue }`), so the paragraph has one child.

- [ ] **Step 3: Handle the hyperlink in lowerParagraph**

In `pkg/docx/cssbox/lower.go`, in `lowerParagraph`, replace the skip-and-unwrap block. Replace:

```go
	for _, child := range p.Content {
		if child.Run == nil {
			// Hyperlink groups and drawings are lowered in the images+hyperlinks
			// phase; a bare run is all Phase 1 handles.
			continue
		}
		run := *child.Run
		switch run.Break {
```

with:

```go
	for _, child := range p.Content {
		if child.Hyperlink != nil {
			for _, run := range child.Hyperlink.Runs {
				if run.Text == "" {
					continue
				}
				er := r.EffectiveRun(p.Props, run.Props)
				cur.Children = append(cur.Children, linkTextBox(run.Text, er, cur.Style))
			}
			continue
		}
		if child.Drawing != nil {
			// Drawings (images) are lowered in Task 4.4.
			continue
		}
		if child.Run == nil {
			continue
		}
		run := *child.Run
		switch run.Break {
```

Add the `linkTextBox` helper (near `runTextBox` in `lower.go`):

```go
// linkTextBox lowers a hyperlink run's text into an inline box styled as a link:
// the run's own formatting, overlaid with link blue + underline (the HTML a:link
// UA default). The URL is not carried on the cssbox tree; it survives on the docx
// model for the conversion path.
func linkTextBox(text string, er style.EffectiveRun, para gcss.ComputedStyle) *lcssbox.Box {
	box := runTextBox(text, er, para)
	box.Style.Color = color.RGBA{R: 0x00, G: 0x00, B: 0xEE, A: 0xFF}
	box.Style.TextDecorationLine = "underline"
	return box
}
```

Add `"image/color"` to `lower.go`'s imports if not already present (check the import block).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx/cssbox -run TestLowerHyperlinkStylesRuns -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/cssbox/lower.go pkg/docx/cssbox/lower_test.go
git commit -m "docx/cssbox: lower hyperlinks as link-styled inline text"
```

### Task 4.3: Parse w:drawing + collect media parts

**Files:**
- Modify: `pkg/docx/parse.go` (parse `w:drawing`→`wp:extent`+`a:blip@r:embed`; collect `word/media/*` bytes)
- Modify: `pkg/docx/model.go` (`Document.Media map[string][]byte`)
- Modify: `pkg/docx/zip.go` (a helper to list media part names)

A `w:drawing` nests `wp:inline`/`wp:anchor` → `wp:extent` (cx/cy in EMU) and `a:graphic`→`pic:pic`→`pic:blipFill`→`a:blip` (`r:embed`=rel id → image part). The parser walks by *local name only* (the `wp:`/`a:`/`pic:` namespaces differ) since local names are unambiguous here.

- [ ] **Step 1: Write the failing test**

Add `pkg/docx/parse_drawing_test.go`:

```go
package docx

import "testing"

func TestParseDrawing(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>
<w:p><w:r><w:drawing>
  <wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">
    <wp:extent cx="914400" cy="457200"/>
    <a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
      <a:graphicData>
        <pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">
          <pic:blipFill><a:blip r:embed="rId7"/></pic:blipFill>
        </pic:pic>
      </a:graphicData>
    </a:graphic>
  </wp:inline>
</w:drawing></w:r></w:p>
</w:body></w:document>`)
	c := doc.Body[0].Paragraph.Content
	if len(c) != 1 || c[0].Drawing == nil {
		t.Fatalf("content = %+v, want one drawing", c)
	}
	dr := c[0].Drawing
	if dr.RelID != "rId7" {
		t.Fatalf("drawing RelID = %q, want rId7", dr.RelID)
	}
	if dr.WidthEMU != 914400 || dr.HeightEMU != 457200 {
		t.Fatalf("drawing extent = (%d, %d), want (914400, 457200)", dr.WidthEMU, dr.HeightEMU)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run TestParseDrawing -v`
Expected: FAIL — `w:drawing` is skipped inside `parseRun`'s default case.

- [ ] **Step 3a: Parse w:drawing inside parseRun**

In `pkg/docx/parse.go`, `parseRun` currently returns `[]Run`. A drawing is not a `Run` — it must reach the paragraph's `Content` as a `ParaChild{Drawing:...}`. The cleanest change: have `parseRun` also return any drawings it encountered, and have `parseParagraph` append them. Change `parseRun`'s signature and the `w:drawing` handling.

Change `parseRun`'s signature and return:

```go
func parseRun(dec *xml.Decoder) ([]Run, []*Drawing, error) {
```

Add a `drawings` accumulator at the top of `parseRun` (next to `out`):

```go
	var drawings []*Drawing
```

Add a `case "drawing":` to `parseRun`'s inner switch (before `default`):

```go
			case "drawing":
				dr, err := parseDrawing(dec)
				if err != nil {
					return nil, nil, err
				}
				if dr != nil {
					drawings = append(drawings, dr)
				}
```

Change every `return` in `parseRun` to include `drawings` and a nil-error / error triple. Specifically the error returns become `return nil, nil, fmt.Errorf(...)` and the final success return becomes:

```go
		case xml.EndElement:
			if t.Name.Local == "r" {
				flushText()
				return out, drawings, nil
			}
```

- [ ] **Step 3b: Update parseParagraph + parseHyperlink callers of parseRun**

In `parseParagraph`, the `case "r":` now gets three return values. Replace:

```go
			case "r":
				runs, err := parseRun(dec)
				if err != nil {
					return nil, nil, err
				}
				for i := range runs {
					r := runs[i]
					p.Content = append(p.Content, ParaChild{Run: &r})
				}
```

with:

```go
			case "r":
				runs, drawings, err := parseRun(dec)
				if err != nil {
					return nil, nil, err
				}
				for i := range runs {
					r := runs[i]
					p.Content = append(p.Content, ParaChild{Run: &r})
				}
				for _, dr := range drawings {
					p.Content = append(p.Content, ParaChild{Drawing: dr})
				}
```

In `parseHyperlink`, the `parseRun` call also gets three values (a drawing inside a hyperlink is rare; drop it). Replace:

```go
			if t.Name.Space == wNS && t.Name.Local == "r" {
				runs, err := parseRun(dec)
				if err != nil {
					return nil, err
				}
				h.Runs = append(h.Runs, runs...)
				continue
			}
```

with:

```go
			if t.Name.Space == wNS && t.Name.Local == "r" {
				runs, _, err := parseRun(dec)
				if err != nil {
					return nil, err
				}
				h.Runs = append(h.Runs, runs...)
				continue
			}
```

- [ ] **Step 3c: Add parseDrawing**

Add to `pkg/docx/parse.go`:

```go
// parseDrawing consumes a w:drawing, extracting the image extent (wp:extent
// cx/cy in EMU) and the blip's relationship id (a:blip r:embed). It walks by
// local name (the wp:/a:/pic: namespaces are distinct but the local names are
// unambiguous). Returns nil if no blip is found (an unsupported drawing shape).
func parseDrawing(dec *xml.Decoder) (*Drawing, error) {
	dr := &Drawing{}
	hasBlip := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: drawing: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "extent":
				if v, ok := attrInt64(t, "cx"); ok {
					dr.WidthEMU = v
				}
				if v, ok := attrInt64(t, "cy"); ok {
					dr.HeightEMU = v
				}
			case "blip":
				if id, ok := rAttr(t, "embed"); ok {
					dr.RelID = id
					hasBlip = true
				}
			}
		case xml.EndElement:
			if t.Name.Local == "drawing" {
				if !hasBlip {
					return nil, nil
				}
				return dr, nil
			}
		}
	}
}

// attrInt64 returns an int64-valued attribute by local name (EMU extents exceed
// int range on 32-bit, so use int64).
func attrInt64(e xml.StartElement, local string) (int64, bool) {
	for _, a := range e.Attr {
		if a.Name.Local == local {
			n, err := strconv.ParseInt(strings.TrimSpace(a.Value), 10, 64)
			if err != nil {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}
```

Note: `parseDrawing` does not call `dec.Skip()` inside the loop — it walks every descendant token so it can reach the nested `extent`/`blip` at any depth. It terminates on the matching `drawing` end element.

- [ ] **Step 3d: Collect media parts in parsePackage**

In `pkg/docx/model.go`, add to `Document` (after `Rels`):

```go
	// Media maps an image part name (e.g. "word/media/image1.png") to its raw
	// bytes, for drawings to decode. Empty if the document embeds no media.
	Media map[string][]byte
```

In `pkg/docx/parse.go`, in `parsePackage`, after `doc.Rels = ...`, add:

```go
	doc.Media = pkg.mediaParts()
```

In `pkg/docx/zip.go`, add a method on `pkgReader`:

```go
// mediaParts returns every word/media/* part keyed by its part name.
func (p *pkgReader) mediaParts() map[string][]byte {
	var out map[string][]byte
	for name := range p.files {
		if strings.HasPrefix(name, "word/media/") {
			if data, ok := p.part(name); ok {
				if out == nil {
					out = map[string][]byte{}
				}
				out[name] = data
			}
		}
	}
	return out
}
```

Add `"strings"` to `zip.go`'s imports if absent (check the import block; `p.files` is the map of part-name→bytes — confirm the field name with `grep -n "files" pkg/docx/zip.go` and adapt if it differs).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run TestParseDrawing -v`
Expected: PASS. Full package + prior tests: `go test ./pkg/docx`.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/parse.go pkg/docx/model.go pkg/docx/zip.go pkg/docx/parse_drawing_test.go
git commit -m "docx: parse w:drawing (extent + blip) + collect word/media parts"
```

### Task 4.4: Media loader + lower a drawing into a replaced image

**Files:**
- Create: `pkg/docx/cssbox/media.go` (`MediaLoader(d)` → a `resource.MapLoader` keyed by rel id)
- Modify: `pkg/docx/cssbox/lower.go` (lower `child.Drawing` into a `BoxReplaced`)
- Modify: `pkg/doctaculous/reflow_backend.go` (`docxDocument` passes the media loader to the engine)

The engine decodes a replaced image via `e.images.get(ctx, b.Replaced.Attrs["src"])` through the engine's `ResourceLoader`. So: the drawing's `src` is set to its **rel id** (e.g. `"rId7"`), and `MediaLoader` maps each rel id → the bytes of the media part its relationship targets. EMU extent → point size (914400 EMU = 96px = 72pt at the engine's px==pt scale; actually 914400 EMU = 1 inch = 72pt, so `pt = EMU / 12700`).

- [ ] **Step 1: Write the failing test**

Create `pkg/docx/cssbox/media_test.go`:

```go
package cssbox

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func TestMediaLoaderResolvesRelIDToBytes(t *testing.T) {
	d := &docx.Document{
		Rels:  map[string]docx.Relationship{"rId7": {ID: "rId7", Target: "word/media/image1.png"}},
		Media: map[string][]byte{"word/media/image1.png": []byte("PNGDATA")},
	}
	loader := MediaLoader(d)
	got, _, err := loader.Load(context.Background(), "rId7")
	if err != nil {
		t.Fatalf("Load(rId7): %v", err)
	}
	if string(got) != "PNGDATA" {
		t.Fatalf("Load(rId7) = %q, want PNGDATA", got)
	}
}

func TestLowerDrawingToReplacedImage(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Rels:    map[string]docx.Relationship{"rId7": {ID: "rId7", Target: "word/media/image1.png"}},
		Media:   map[string][]byte{"word/media/image1.png": []byte("PNGDATA")},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{Content: []docx.ParaChild{
			{Drawing: &docx.Drawing{RelID: "rId7", WidthEMU: 914400, HeightEMU: 457200}},
		}}}},
	}
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	para := body.Children[0]
	if len(para.Children) != 1 {
		t.Fatalf("paragraph children = %d, want 1 image", len(para.Children))
	}
	img := para.Children[0]
	if img.Kind != lcssbox.BoxReplaced || img.Replaced == nil {
		t.Fatalf("image box = %+v, want BoxReplaced", img)
	}
	if img.Replaced.Attrs["src"] != "rId7" {
		t.Fatalf("image src = %q, want rId7", img.Replaced.Attrs["src"])
	}
	// 914400 EMU = 72pt width, 457200 = 36pt height.
	if img.Replaced.Attrs["width"] != "72" || img.Replaced.Attrs["height"] != "36" {
		t.Fatalf("image size attrs = %q x %q, want 72 x 36", img.Replaced.Attrs["width"], img.Replaced.Attrs["height"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx/cssbox -run 'TestMediaLoaderResolvesRelIDToBytes|TestLowerDrawingToReplacedImage' -v`
Expected: FAIL — `undefined: MediaLoader`; drawings still skipped in `lowerParagraph`.

- [ ] **Step 3a: Create pkg/docx/cssbox/media.go**

```go
package cssbox

import (
	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// MediaLoader builds an in-memory ResourceLoader that resolves a drawing's
// relationship id (the "src" set on the replaced image box) to the bytes of the
// media part that relationship targets. A document with no media yields an empty
// loader (every Load misses -> the engine paints a placeholder). Content type is
// left "" so the engine sniffs the format from the bytes.
func MediaLoader(d *docx.Document) resource.MapLoader {
	m := resource.MapLoader{}
	if d == nil {
		return m
	}
	for id, rel := range d.Rels {
		if rel.External {
			continue
		}
		if data, ok := d.Media[rel.Target]; ok {
			m[id] = resource.Resource{Data: data}
		}
	}
	return m
}
```

- [ ] **Step 3b: Lower the drawing in lowerParagraph**

In `pkg/docx/cssbox/lower.go`, replace the drawing skip:

```go
		if child.Drawing != nil {
			// Drawings (images) are lowered in Task 4.4.
			continue
		}
```

with:

```go
		if child.Drawing != nil {
			cur.Children = append(cur.Children, drawingBox(child.Drawing, cur.Style))
			continue
		}
```

Add `drawingBox` (in `lower.go`, or a new file — keep it in `media.go` for cohesion). Add to `pkg/docx/cssbox/media.go`:

```go
// emuPerPt is the EMU-to-point conversion (914400 EMU = 1 inch = 72 pt).
const emuPerPt = 12700

// drawingBox lowers a DOCX drawing into an inline replaced image box. src is the
// rel id (resolved by MediaLoader); the extent (EMU) becomes width/height point
// attributes so the CSS replaced-sizing uses them (CSS width/height would
// override, but a DOCX drawing has no CSS). The box inherits the paragraph style
// so it flows inline.
func drawingBox(dr *docx.Drawing, para gcss.ComputedStyle) *lcssbox.Box {
	cs := para
	cs.Display = "inline-block"
	attrs := map[string]string{"src": dr.RelID}
	if dr.WidthEMU > 0 {
		attrs["width"] = strconv.Itoa(int(dr.WidthEMU / emuPerPt))
	}
	if dr.HeightEMU > 0 {
		attrs["height"] = strconv.Itoa(int(dr.HeightEMU / emuPerPt))
	}
	return &lcssbox.Box{
		Kind:     lcssbox.BoxReplaced,
		Display:  lcssbox.DisplayInlineBlock,
		Style:    cs,
		Replaced: &lcssbox.ReplacedContent{Tag: "img", Attrs: attrs},
	}
}
```

Update `media.go` imports to add `gcss "github.com/nathanstitt/doctaculous/pkg/css"`, `lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"`, and `"strconv"`.

- [ ] **Step 3c: Wire the media loader into the engine**

In `pkg/doctaculous/reflow_backend.go`, in `docxDocument`, replace:

```go
	engine := layoutcss.New(faces, resource.MapLoader(nil), nil)
```

with:

```go
	engine := layoutcss.New(faces, docxcssbox.MediaLoader(d), nil)
```

(`docxcssbox` and `resource` are already imported; if `resource` becomes unused after this change, remove it from the imports — run `goimports -w pkg/doctaculous/reflow_backend.go`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx/cssbox -run 'TestMediaLoaderResolvesRelIDToBytes|TestLowerDrawingToReplacedImage' -v`
Expected: PASS. Then `go build ./...`.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/cssbox/media.go pkg/docx/cssbox/lower.go pkg/doctaculous/reflow_backend.go pkg/docx/cssbox/media_test.go
git commit -m "docx/cssbox: lower drawings into replaced images via a media ResourceLoader"
```

### Task 4.5: Builder.AddMedia + image & hyperlink fixtures + goldens

**Files:**
- Modify: `testdata/gen/docx/docx.go` (`Builder.AddMedia` + `Builder.AddRel`; write media parts + rels + content types)
- Modify: `testdata/gen/docx/fixtures.go` (add `docx-hyperlink`, `docx-image`)

The image fixture needs a rel (`rId7` → `word/media/image1.png`), the media bytes, and content-type defaults for `png`. The hyperlink fixture needs an external rel (`rId5` → a URL). Extend the Builder to accept both.

- [ ] **Step 1: Extend the Builder for media + arbitrary rels**

In `testdata/gen/docx/docx.go`, add fields and setters. Extend the struct:

```go
type Builder struct {
	documentXML  string
	stylesXML    string
	numberingXML string
	media        map[string][]byte  // part name -> bytes (e.g. "word/media/image1.png")
	extraRels    []rel              // additional document relationships
}

// rel is one extra relationship to emit into document.xml.rels.
type rel struct {
	id, typ, target, mode string
}
```

Add setters after `SetNumbering`:

```go
// AddMedia registers an image part (e.g. "media/image1.png", stored under
// word/) with the given bytes and its file extension's content-type default.
func (b *Builder) AddMedia(partName string, data []byte) *Builder {
	if b.media == nil {
		b.media = map[string][]byte{}
	}
	b.media["word/"+partName] = data
	return b
}

// AddRel adds a document relationship (id -> target). mode is "" for an internal
// part or "External" for a URL.
func (b *Builder) AddRel(id, typ, target, mode string) *Builder {
	b.extraRels = append(b.extraRels, rel{id: id, typ: typ, target: target, mode: mode})
	return b
}
```

In `Bytes`, write the media parts. After the numbering write block:

```go
	for name, data := range b.media {
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: fixedModTime})
		_, _ = w.Write(data)
	}
```

Update `contentTypes` to include a `png`/`jpeg` default when media is present. Change its call and signature to also take `hasMedia bool`:

```go
	write("[Content_Types].xml", contentTypes(b.stylesXML != "", b.numberingXML != "", len(b.media) > 0))
```

and in `contentTypes`, add a `withMedia bool` param and, when set, append PNG + JPEG defaults before the closing tag:

```go
	if withMedia {
		s += `
  <Default Extension="png" ContentType="image/png"/>
  <Default Extension="jpeg" ContentType="image/jpeg"/>
  <Default Extension="jpg" ContentType="image/jpeg"/>`
	}
```

Update `docRels` to emit the extra rels. Change its call to pass `b.extraRels`:

```go
	write("word/_rels/document.xml.rels", docRels(b.stylesXML != "", b.numberingXML != "", b.extraRels))
```

and in `docRels`, add a `extra []rel` param and append each before the closing tag:

```go
	for _, r := range extra {
		mode := ""
		if r.mode != "" {
			mode = ` TargetMode="` + r.mode + `"`
		}
		s += `
  <Relationship Id="` + r.id + `" Type="` + r.typ + `" Target="` + r.target + `"` + mode + `/>`
	}
```

- [ ] **Step 2: Add the fixtures**

In `testdata/gen/docx/fixtures.go`, add to `Core`:

```go
	{
		Name:  "hyperlink",
		Desc:  "a paragraph with an external hyperlink (link-styled text)",
		Pages: 1,
		Build: hyperlinkDocx,
	},
	{
		Name:  "image",
		Desc:  "a paragraph with an embedded PNG drawing",
		Pages: 1,
		Build: imageDocx,
	},
```

Add the builders (and a tiny-PNG helper) at the end of `fixtures.go`:

```go
const relHyperlink = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
const relImage = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"

func hyperlinkDocx() []byte {
	p := `<w:p>` +
		`<w:r><w:t xml:space="preserve">Visit </w:t></w:r>` +
		`<w:hyperlink r:id="rId5"><w:r><w:t>the project</w:t></w:r></w:hyperlink>` +
		`<w:r><w:t xml:space="preserve"> for more.</w:t></w:r>` +
		`</w:p>`
	doc := docOpenR + p + docClose
	return New().SetDocument(doc).
		AddRel("rId5", relHyperlink, "https://example.com/", "External").
		Bytes()
}

func imageDocx() []byte {
	p := `<w:p><w:r><w:drawing>` +
		`<wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">` +
		`<wp:extent cx="1828800" cy="914400"/>` +
		`<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData>` +
		`<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">` +
		`<pic:blipFill><a:blip r:embed="rId7"/></pic:blipFill>` +
		`</pic:pic></a:graphicData></a:graphic></wp:inline>` +
		`</w:drawing></w:r></w:p>`
	doc := docOpenR + para("", "", "An embedded image:") + p + docClose
	return New().SetDocument(doc).
		AddMedia("media/image1.png", tinyPNG(96, 48)).
		AddRel("rId7", relImage, "media/image1.png", "").
		Bytes()
}

// tinyPNG builds a solid-color PNG of the given pixel size for image fixtures.
func tinyPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill := color.RGBA{R: 0x33, G: 0x88, B: 0xCC, A: 0xFF}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, fill)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
```

Add a `docOpenR` constant that declares the `r:` namespace (the existing `docOpen` does not; hyperlinks/drawings need it). In `fixtures.go`, next to `docOpen`:

```go
// docOpenR is docOpen plus the officeDocument relationships namespace, needed by
// fixtures using r:id (hyperlinks, drawings).
const docOpenR = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`
```

Add imports to `fixtures.go`: `"bytes"`, `"image"`, `"image/color"`, `"image/png"` (alongside the existing `"strings"`/`"strconv"`).

- [ ] **Step 3: Generate + eyeball the goldens**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -update`
Open `pkg/doctaculous/testdata/golden/docx-hyperlink.png` (verify "Visit the project for more." with "the project" blue + underlined) and `docx-image.png` (verify a blue rectangle ~144×72pt below "An embedded image:").

- [ ] **Step 4: Run the golden test for real**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -v`
Expected: PASS for all nine fixtures; prior goldens byte-identical.

- [ ] **Step 5: Commit**

```bash
git add testdata/gen/docx/docx.go testdata/gen/docx/fixtures.go pkg/doctaculous/testdata/golden/docx-hyperlink.png pkg/doctaculous/testdata/golden/docx-image.png
git commit -m "docx: hyperlink + image fixtures/goldens; Builder.AddMedia/AddRel"
```

### Task 4.6: Verify + PR

- [ ] **Step 1: Full suite + vet + lint + race**

Run: `go test ./... && go vet ./... && golangci-lint run && go test -race ./pkg/docx/... ./pkg/doctaculous`
Expected: all pass; prior DOCX goldens unchanged.

- [ ] **Step 2: Open the PR**

```bash
git push -u origin docx-fidelity-4-media
gh pr create --title "docx: images + hyperlinks (phase 4 of DOCX fidelity)" \
  --body "Parse w:hyperlink (target via rels) and w:drawing (extent + blip), lower into link-styled inline text and BoxReplaced images decoded through a media ResourceLoader built from word/media/*. New docx-hyperlink + docx-image fixtures/goldens; prior goldens byte-identical. Spec: docs/superpowers/specs/2026-07-02-docx-fidelity-design.md"
```

Wait for green CI + merge before Phase 5.

---

## Phase 5 — Parts / sections (headers, footers, multi-section, footnotes)

**Branch:** `git checkout main && git pull && git checkout -b docx-fidelity-5-parts`

**Goal:** Parse and render headers/footers (via the paged engine's running-element + `@page` margin-box mechanism), multiple sections (per-section geometry), and footnotes (references + note text). Add `docx-header-footer`, `docx-multisection`, `docx-footnote` fixtures + goldens.

**Verified API basis:** `PagedConfig.Running map[string]*cssbox.Box` (a name → box map; a `@page` margin box with `content: element(name)` paints `Running[name]` on every page) and `ComputeMarginBox` are real, documented entry points (`pkg/layout/css/marginbox.go`, `pkg/layout/css/build.go:59`). DOCX headers/footers lower into `*cssbox.Box` values, are keyed under synthetic names (`docxheader`/`docxfooter`), and `docxPageSheet` synthesizes `@top-center`/`@bottom-center { content: element(...) }`.

### Task 5.1: Parse header/footer parts + their references

**Files:**
- Create: `pkg/docx/parts.go` (`HeaderFooter` model + `parseHdrFtr`)
- Modify: `pkg/docx/parse.go` (`parseSectPr` records `headerReference`/`footerReference` r:ids; `parsePackage` resolves + parses the referenced parts)
- Modify: `pkg/docx/model.go` (`SectionProps` gains header/footer rel ids; `Document.Headers`/`Footers` maps)

A `w:sectPr` references headers/footers by `r:id` + `w:type` (default/even/first). The referenced part (`header1.xml` etc.) is a body of paragraphs. Parse it with the shared `parseBlockChild` loop (it's a `w:hdr`/`w:ftr` root containing the same block content as the body).

- [ ] **Step 1: Write the failing test**

Add `pkg/docx/parts_test.go`:

```go
package docx

import "testing"

func TestParseHeaderPart(t *testing.T) {
	hf, err := parseHdrFtr([]byte(`<?xml version="1.0"?>
<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:p><w:r><w:t>Page header</w:t></w:r></w:p>
</w:hdr>`), "hdr")
	if err != nil {
		t.Fatalf("parseHdrFtr: %v", err)
	}
	if len(hf.Blocks) != 1 || hf.Blocks[0].Paragraph == nil {
		t.Fatalf("header blocks = %+v, want 1 paragraph", hf.Blocks)
	}
	if got := hf.Blocks[0].Paragraph.Content[0].Run.Text; got != "Page header" {
		t.Fatalf("header text = %q, want 'Page header'", got)
	}
}

func TestParseSectPrHeaderReference(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>
<w:p><w:r><w:t>body</w:t></w:r></w:p>
<w:sectPr>
  <w:headerReference w:type="default" r:id="rId10"/>
  <w:footerReference w:type="default" r:id="rId11"/>
  <w:pgSz w:w="12240" w:h="15840"/>
</w:sectPr>
</w:body></w:document>`)
	if doc.Section.HeaderRefDefault != "rId10" {
		t.Fatalf("header ref = %q, want rId10", doc.Section.HeaderRefDefault)
	}
	if doc.Section.FooterRefDefault != "rId11" {
		t.Fatalf("footer ref = %q, want rId11", doc.Section.FooterRefDefault)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run 'TestParseHeaderPart|TestParseSectPrHeaderReference' -v`
Expected: FAIL — `undefined: parseHdrFtr`; `SectionProps.HeaderRefDefault` undefined; `headerReference` skipped in `parseSectPr`.

- [ ] **Step 3a: Extend the model**

In `pkg/docx/model.go`, add to `SectionProps` (after `Header, Footer, Gutter`):

```go
	// HeaderRefDefault/FooterRefDefault are the r:ids of the default header/footer
	// parts referenced by this section (w:headerReference/w:footerReference
	// type="default"), or "" when none. (even/first variants are a follow-up.)
	HeaderRefDefault string
	FooterRefDefault string
```

Add to `Document` (after `Media`):

```go
	// Headers/Footers map a header/footer part's relationship id to its parsed
	// content, for a section's HeaderRefDefault/FooterRefDefault to resolve.
	Headers map[string]*HeaderFooter
	Footers map[string]*HeaderFooter
```

- [ ] **Step 3b: Create pkg/docx/parts.go**

```go
package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// HeaderFooter is a parsed header or footer part (w:hdr / w:ftr): a sequence of
// block-level content (paragraphs, tables) rendered in the page margin band.
type HeaderFooter struct {
	Blocks []Block
}

// parseHdrFtr parses a header/footer part. root is the expected root local name
// ("hdr" or "ftr"); the body content uses the same block grammar as w:body.
func parseHdrFtr(data []byte, root string) (*HeaderFooter, error) {
	hf := &HeaderFooter{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: %s: %v", ErrMalformedXML, root, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != wNS || se.Name.Local != root {
			continue
		}
		// Consume the root's children with the shared block dispatch.
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("%w: %s: %v", ErrMalformedXML, root, err)
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Space != wNS {
					if err := dec.Skip(); err != nil {
						return nil, fmt.Errorf("%w: %s: %v", ErrMalformedXML, root, err)
					}
					continue
				}
				blk, _, err := parseBlockChild(dec, t)
				if err != nil {
					return nil, err
				}
				if blk != nil {
					hf.Blocks = append(hf.Blocks, *blk)
				}
			case xml.EndElement:
				if t.Name.Local == root {
					return hf, nil
				}
			}
		}
	}
	return hf, nil
}
```

- [ ] **Step 3c: Record the references in parseSectPr**

In `pkg/docx/parse.go`, in `parseSectPr`, add cases to the inner switch (alongside `pgSz`/`pgMar`):

```go
				case "headerReference":
					if typ, _ := wAttr(t, "type"); typ == "default" || typ == "" {
						if id, ok := rAttr(t, "id"); ok {
							sect.HeaderRefDefault = id
						}
					}
				case "footerReference":
					if typ, _ := wAttr(t, "type"); typ == "default" || typ == "" {
						if id, ok := rAttr(t, "id"); ok {
							sect.FooterRefDefault = id
						}
					}
```

- [ ] **Step 3d: Resolve header/footer parts in parsePackage**

In `pkg/docx/parse.go`, in `parsePackage`, after `doc.Media = ...`, add:

```go
	doc.Headers, doc.Footers = resolveHeadersFooters(pkg, doc.Rels)
```

Add the resolver:

```go
// resolveHeadersFooters parses every header/footer part referenced by the
// document relationships, keyed by relationship id. Header and footer parts are
// distinguished by relationship type.
func resolveHeadersFooters(pkg *pkgReader, rels map[string]Relationship) (headers, footers map[string]*HeaderFooter) {
	const hdrType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
	const ftrType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer"
	// Need the rel types, which allRels does not retain; re-read the doc rels with
	// types here by scanning the map's targets against the parts. Simpler: the
	// relationships file carries the Type; add it to Relationship.
	for id, rel := range rels {
		switch rel.relType {
		case hdrType:
			if data, ok := pkg.part(rel.Target); ok {
				if hf, err := parseHdrFtr(data, "hdr"); err == nil {
					if headers == nil {
						headers = map[string]*HeaderFooter{}
					}
					headers[id] = hf
				}
			}
		case ftrType:
			if data, ok := pkg.part(rel.Target); ok {
				if hf, err := parseHdrFtr(data, "ftr"); err == nil {
					if footers == nil {
						footers = map[string]*HeaderFooter{}
					}
					footers[id] = hf
				}
			}
		}
	}
	return headers, footers
}
```

This needs the relationship **type** on `Relationship`. In `pkg/docx/model.go`, add an unexported `relType string` field to `Relationship`:

```go
type Relationship struct {
	ID       string
	Target   string
	External bool
	relType  string
}
```

And in `allRels` (`pkg/docx/parse.go`), capture it: change the assembly to `out[r.ID] = Relationship{ID: r.ID, Target: target, External: external, relType: r.Type}` and add `Type string \`xml:"Type,attr"\`` to the inline struct's `Rels` element.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run 'TestParseHeaderPart|TestParseSectPrHeaderReference' -v`
Expected: PASS. Full package: `go test ./pkg/docx`.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/parts.go pkg/docx/parse.go pkg/docx/model.go pkg/docx/parts_test.go
git commit -m "docx: parse header/footer parts + section references"
```

### Task 5.2: Lower headers/footers as running elements + wire the paged config

**Files:**
- Modify: `pkg/docx/cssbox/lower.go` (add `LowerRunning(d, r)` → the running-element map)
- Modify: `pkg/doctaculous/reflow_backend.go` (`docxDocument` builds the running map; `docxPageSheet` emits margin boxes referencing them)

The section's default header/footer parts lower into `*cssbox.Box` blocks keyed as `"docxheader"`/`"docxfooter"`. `docxPageSheet` gains `@top-center { content: element(docxheader) }` and `@bottom-center { content: element(docxfooter) }` only when the section actually has a header/footer (so a headerless doc stays byte-identical — `element()` never fires and no margin box is emitted).

- [ ] **Step 1: Write the failing test**

Add to `pkg/docx/cssbox/lower_test.go`:

```go
func TestLowerRunningBuildsHeaderFooterBoxes(t *testing.T) {
	hdr := &docx.HeaderFooter{Blocks: []docx.Block{{Paragraph: paraWith("H")}}}
	ftr := &docx.HeaderFooter{Blocks: []docx.Block{{Paragraph: paraWith("F")}}}
	d := &docx.Document{
		Section: docx.SectionProps{
			PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440,
			HeaderRefDefault: "rIdH", FooterRefDefault: "rIdF",
		},
		Headers: map[string]*docx.HeaderFooter{"rIdH": hdr},
		Footers: map[string]*docx.HeaderFooter{"rIdF": ftr},
	}
	r := style.NewResolver(d, nil)
	running := LowerRunning(d, r)
	if running["docxheader"] == nil {
		t.Fatalf("running[docxheader] = nil, want a box")
	}
	if running["docxfooter"] == nil {
		t.Fatalf("running[docxfooter] = nil, want a box")
	}
	// header box holds the header paragraph -> text "H".
	hb := running["docxheader"]
	if len(hb.Children) == 0 {
		t.Fatalf("header box has no children")
	}
}

func TestLowerRunningEmptyWhenNoHeaderFooter(t *testing.T) {
	d := &docx.Document{Section: docx.SectionProps{PageW: 12240, PageH: 15840}}
	running := LowerRunning(d, style.NewResolver(d, nil))
	if len(running) != 0 {
		t.Fatalf("running = %v, want empty (byte-identical path)", running)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx/cssbox -run 'TestLowerRunningBuildsHeaderFooterBoxes|TestLowerRunningEmptyWhenNoHeaderFooter' -v`
Expected: FAIL — `undefined: LowerRunning`.

- [ ] **Step 3a: Add LowerRunning**

Add to `pkg/docx/cssbox/lower.go`:

```go
// RunningHeaderName / RunningFooterName are the synthetic running-element names
// under which a DOCX section's default header/footer are keyed, referenced from
// the synthesized @page margin boxes via element(name).
const (
	RunningHeaderName = "docxheader"
	RunningFooterName = "docxfooter"
)

// LowerRunning lowers the document's default header and footer (if any) into
// running-element boxes keyed by RunningHeaderName/RunningFooterName, for the
// paged engine's @page margin boxes to paint on every page. Returns an empty map
// when the section has no header/footer — the byte-identical path (no margin box
// is synthesized, so element() never fires).
func LowerRunning(d *docx.Document, r *style.Resolver) map[string]*lcssbox.Box {
	out := map[string]*lcssbox.Box{}
	if d == nil {
		return out
	}
	if hf := headerFooterFor(d.Section.HeaderRefDefault, d.Headers); hf != nil {
		out[RunningHeaderName] = runningBox(hf, r, d.Numbering)
	}
	if hf := headerFooterFor(d.Section.FooterRefDefault, d.Footers); hf != nil {
		out[RunningFooterName] = runningBox(hf, r, d.Numbering)
	}
	return out
}

// headerFooterFor looks up a header/footer by ref id, returning nil when the ref
// is empty or unresolved.
func headerFooterFor(refID string, m map[string]*docx.HeaderFooter) *docx.HeaderFooter {
	if refID == "" || m == nil {
		return nil
	}
	return m[refID]
}

// runningBox lowers a header/footer's blocks into a single block box (the running
// element the margin box paints).
func runningBox(hf *docx.HeaderFooter, r *style.Resolver, num *docx.Numbering) *lcssbox.Box {
	box := &lcssbox.Box{
		Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayBlock, Formatting: lcssbox.BlockFC,
		Style: gcss.InitialStyle(),
	}
	box.Children = lowerBlocks(hf.Blocks, r, num, newListCounter())
	return box
}
```

- [ ] **Step 3b: Wire the running map + margin boxes into docxDocument**

In `pkg/doctaculous/reflow_backend.go`, in `docxDocument`, build the running map and pass it. Replace:

```go
	pages, err := engine.LayoutPagedDoc(ctx, root, layoutcss.PagedConfig{
		Paged:        true,
		FallbackW:    geom.PageWidthPt, // full page; @page size/margins refine below
		FallbackH:    geom.PageHeightPt,
		ExplicitSize: false, // let the synthesized @page size apply
		Pages:        docxPageSheet(geom),
	})
```

with:

```go
	running := docxcssbox.LowerRunning(d, resolver)
	pages, err := engine.LayoutPagedDoc(ctx, root, layoutcss.PagedConfig{
		Paged:        true,
		FallbackW:    geom.PageWidthPt, // full page; @page size/margins refine below
		FallbackH:    geom.PageHeightPt,
		ExplicitSize: false, // let the synthesized @page size apply
		Pages:        docxPageSheet(geom, running),
		Running:      running,
	})
```

- [ ] **Step 3c: Emit margin boxes in docxPageSheet**

In `pkg/doctaculous/reflow_backend.go`, change `docxPageSheet` to take the running map and append margin boxes when the header/footer running elements exist:

```go
func docxPageSheet(g docxcssbox.PageGeometry, running map[string]*lcssbox.Box) gcss.Stylesheet {
	px := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "px" }
	var mb strings.Builder
	if running[docxcssbox.RunningHeaderName] != nil {
		mb.WriteString(" @top-center { content: element(" + docxcssbox.RunningHeaderName + ") }")
	}
	if running[docxcssbox.RunningFooterName] != nil {
		mb.WriteString(" @bottom-center { content: element(" + docxcssbox.RunningFooterName + ") }")
	}
	css := fmt.Sprintf("@page { size: %s %s; margin: %s %s %s %s%s }",
		px(g.PageWidthPt), px(g.PageHeightPt),
		px(g.MarginTopPt), px(g.MarginRightPt), px(g.MarginBottomPt), px(g.MarginLeftPt),
		mb.String())
	return gcss.Parse(css)
}
```

Add imports to `reflow_backend.go` if missing: `"strings"` and `lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"` (check the import block; `docxcssbox` is already imported). Verify with `grep -n "layout/cssbox\|\"strings\"" pkg/doctaculous/reflow_backend.go`.

Note: confirm the `@page` grammar the parser accepts places margin-box blocks *inside* the `@page { ... }` braces (as written). If `pkg/css/pagesize.go`'s `@page` parser expects nested margin-box rules with their own braces, this string form is already how the HTML side authors them — cross-check one existing `@page` with a margin box in the test corpus: `grep -rn "@top-center\|@bottom-center" testdata/ pkg/ | grep -v _test`. Match that exact syntax.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx/cssbox -run 'TestLowerRunning' -v`
Expected: PASS. Then `go build ./...`.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/cssbox/lower.go pkg/doctaculous/reflow_backend.go pkg/docx/cssbox/lower_test.go
git commit -m "docx/cssbox: lower headers/footers as running elements into @page margin boxes"
```

### Task 5.3: Collect all sections (multi-section geometry)

**Files:**
- Modify: `pkg/docx/model.go` (`Document.Sections []SectionProps`)
- Modify: `pkg/docx/parse.go` (`parseBody` appends every sectPr, not just the last)

Today `parseBody` overwrites `doc.Section` each time it sees a `sectPr` (body-level or in a paragraph's pPr), so only the last section survives. Collect them all in order. Keep `doc.Section` (the last/body section) for byte-identical single-section behavior; add `doc.Sections` for the full list. **This task lands the model + parsing only**; a paragraph-run split across differing page sizes is a large paged-engine change deferred with a logged note (documents with one body section — the overwhelming majority — are unaffected).

- [ ] **Step 1: Write the failing test**

Add to `pkg/docx/parse_test.go` (or a new `parse_sections_test.go`):

```go
func TestParseCollectsAllSections(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:pPr><w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr></w:pPr><w:r><w:t>sec1</w:t></w:r></w:p>
<w:p><w:r><w:t>sec2 body</w:t></w:r></w:p>
<w:sectPr><w:pgSz w:w="15840" w:h="12240"/></w:sectPr>
</w:body></w:document>`)
	if len(doc.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(doc.Sections))
	}
	if doc.Sections[0].PageW != 12240 {
		t.Fatalf("section0 width = %d, want 12240 (portrait)", doc.Sections[0].PageW)
	}
	if doc.Sections[1].PageW != 15840 {
		t.Fatalf("section1 width = %d, want 15840 (landscape)", doc.Sections[1].PageW)
	}
	// doc.Section stays the last (body) section for byte-identical single-section behavior.
	if doc.Section.PageW != 15840 {
		t.Fatalf("doc.Section width = %d, want 15840", doc.Section.PageW)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run TestParseCollectsAllSections -v`
Expected: FAIL — `doc.Sections` undefined; only `doc.Section` is set.

- [ ] **Step 3a: Extend the model**

In `pkg/docx/model.go`, add to `Document` (after `Section`):

```go
	// Sections lists every section's geometry in document order (each terminating
	// w:sectPr, body-level or in a paragraph's pPr). doc.Section remains the last
	// (body) section. Single-section documents have len(Sections)==1.
	Sections []SectionProps
```

- [ ] **Step 3b: Append each section in parseBody**

In `pkg/docx/parse.go`, `parseBody`, both places that set `doc.Section` must also append. In the `parseBlockChild` branch, replace:

```go
				if sect != nil {
					doc.Section = *sect
				}
```

with:

```go
				if sect != nil {
					doc.Section = *sect
					doc.Sections = append(doc.Sections, *sect)
				}
```

In the `case "sectPr":` branch, replace:

```go
			case "sectPr":
				sect, err := parseSectPr(dec)
				if err != nil {
					return err
				}
				doc.Section = sect
```

with:

```go
			case "sectPr":
				sect, err := parseSectPr(dec)
				if err != nil {
					return err
				}
				doc.Section = sect
				doc.Sections = append(doc.Sections, sect)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run TestParseCollectsAllSections -v`
Expected: PASS. Full package: `go test ./pkg/docx`.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/model.go pkg/docx/parse.go pkg/docx/parse_test.go
git commit -m "docx: collect all sections in document order (multi-section geometry)"
```

### Task 5.4: Parse footnotes + render reference markers

**Files:**
- Create: `pkg/docx/footnotes.go` (`Footnotes` model + `parseFootnotes`)
- Modify: `pkg/docx/parse.go` (parse `w:footnoteReference` in a run; resolve `footnotes.xml`)
- Modify: `pkg/docx/model.go` (`Run.FootnoteRef int`; `Document.Footnotes`)
- Modify: `pkg/docx/cssbox/lower.go` (render a footnote reference as a superscript marker)

Full footnote *placement* (collecting notes to the page bottom) is a paged-engine feature beyond this pass; this task renders the in-text **reference marker** (a superscript number) and parses the note text onto the model (so the conversion path can emit it). The note text rendered at page bottom is a logged deferral.

- [ ] **Step 1: Write the failing test**

Add `pkg/docx/footnotes_test.go`:

```go
package docx

import "testing"

func TestParseFootnoteReference(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>claim</w:t></w:r><w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:footnoteReference w:id="2"/></w:r></w:p>
</w:body></w:document>`)
	// The footnoteReference run carries FootnoteRef=2.
	var found bool
	for _, c := range doc.Body[0].Paragraph.Content {
		if c.Run != nil && c.Run.FootnoteRef == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no run with FootnoteRef=2 in %+v", doc.Body[0].Paragraph.Content)
	}
}

func TestParseFootnotesPart(t *testing.T) {
	fn, err := parseFootnotes([]byte(`<?xml version="1.0"?>
<w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:footnote w:id="2"><w:p><w:r><w:t>A note.</w:t></w:r></w:p></w:footnote>
</w:footnotes>`))
	if err != nil {
		t.Fatalf("parseFootnotes: %v", err)
	}
	note, ok := fn.Note(2)
	if !ok || len(note.Blocks) != 1 {
		t.Fatalf("Note(2) = %+v, ok=%v, want 1 block", note, ok)
	}
	if got := note.Blocks[0].Paragraph.Content[0].Run.Text; got != "A note." {
		t.Fatalf("note text = %q, want 'A note.'", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run 'TestParseFootnoteReference|TestParseFootnotesPart' -v`
Expected: FAIL — `Run.FootnoteRef` undefined; `undefined: parseFootnotes`.

- [ ] **Step 3a: Extend the model**

In `pkg/docx/model.go`, add to `Run` (after `Break`):

```go
	// FootnoteRef is the id of a footnote this run references (w:footnoteReference
	// w:id); 0 = none. Such a run has no text; it renders as a superscript marker.
	FootnoteRef int
```

Add to `Document` (after `Footers`):

```go
	// Footnotes holds the parsed word/footnotes.xml (note id -> content), or nil.
	Footnotes *Footnotes
```

- [ ] **Step 3b: Create pkg/docx/footnotes.go**

```go
package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// Footnotes is the parsed word/footnotes.xml: note id -> block content.
type Footnotes struct {
	byID map[int]*HeaderFooter // reuse HeaderFooter's Blocks container
}

// Note returns a footnote's content by id.
func (f *Footnotes) Note(id int) (*HeaderFooter, bool) {
	if f == nil {
		return nil, false
	}
	n, ok := f.byID[id]
	return n, ok
}

// parseFootnotes parses a word/footnotes.xml part. Separator/continuation notes
// (negative or special ids) are parsed like any other; the lowering ignores ids
// it has no reference for.
func parseFootnotes(data []byte) (*Footnotes, error) {
	f := &Footnotes{byID: map[int]*HeaderFooter{}}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: footnotes: %v", ErrMalformedXML, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != wNS || se.Name.Local != "footnote" {
			continue
		}
		id, _ := wAttrInt(se, "id")
		note := &HeaderFooter{}
		if err := fillBlocksUntil(dec, "footnote", &note.Blocks); err != nil {
			return nil, err
		}
		f.byID[id] = note
	}
	return f, nil
}

// fillBlocksUntil consumes block content until the named end element, appending
// to blocks. Shared by footnote parsing.
func fillBlocksUntil(dec *xml.Decoder, end string, blocks *[]Block) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrMalformedXML, end, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return fmt.Errorf("%w: %s: %v", ErrMalformedXML, end, err)
				}
				continue
			}
			blk, _, err := parseBlockChild(dec, t)
			if err != nil {
				return err
			}
			if blk != nil {
				*blocks = append(*blocks, *blk)
			}
		case xml.EndElement:
			if t.Name.Local == end {
				return nil
			}
		}
	}
}
```

- [ ] **Step 3c: Parse the footnote reference in a run**

In `pkg/docx/parse.go`, `parseRun`, add a case (before `default`):

```go
			case "footnoteReference":
				if id, ok := wAttrInt(t, "id"); ok {
					flushText()
					out = append(out, Run{Props: props, FootnoteRef: id})
				}
				if err := dec.Skip(); err != nil {
					return nil, nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
```

In `parsePackage`, after `doc.Headers, doc.Footers = ...`, resolve footnotes:

```go
	if data, ok := pkg.part(resolveByType(pkg, mainName,
		"http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes",
		"word/footnotes.xml")); ok {
		fn, err := parseFootnotes(data)
		if err != nil {
			return nil, err
		}
		doc.Footnotes = fn
	}
```

Add a small generic `resolveByType` helper next to `resolveStylesPart` (or reuse it — it duplicates the styles/numbering pattern; refactor both to call it if you like, but that is optional and out of scope):

```go
// resolveByType returns the part name for the first relationship of relType on
// the main part, falling back to fallback.
func resolveByType(pkg *pkgReader, mainName, relType, fallback string) string {
	if rels := pkg.relsForByType(mainName, relType); rels != "" {
		return rels
	}
	return fallback
}
```

- [ ] **Step 3d: Render the reference marker in lowering**

In `pkg/docx/cssbox/lower.go`, in `lowerParagraph`'s run loop, handle a footnote-reference run (it has no text). After the `run := *child.Run` line and before the `switch run.Break`, add:

```go
		if run.FootnoteRef > 0 {
			er := r.EffectiveRun(p.Props, run.Props)
			cur.Children = append(cur.Children, footnoteMarker(run.FootnoteRef, er, cur.Style))
			continue
		}
```

Add the helper:

```go
// footnoteMarker renders a footnote reference as a superscript number. The note
// text itself is placed by the (deferred) footnote-collection pass; here we show
// the in-text marker so the reference is visible and copyable.
func footnoteMarker(id int, er style.EffectiveRun, para gcss.ComputedStyle) *lcssbox.Box {
	box := runTextBox(strconv.Itoa(id), er, para)
	box.Style.VerticalAlign = "super"
	// Superscripts render smaller; approximate with 0.75em of the run size.
	box.Style.FontSizePt = er.SizePt * 0.75
	return box
}
```

Add `"strconv"` to `lower.go`'s imports if absent.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run 'TestParseFootnoteReference|TestParseFootnotesPart' && go test ./pkg/docx/cssbox`
Expected: PASS; `go build ./...` clean.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/footnotes.go pkg/docx/parse.go pkg/docx/model.go pkg/docx/cssbox/lower.go pkg/docx/footnotes_test.go
git commit -m "docx: parse footnotes + render superscript reference markers"
```

### Task 5.5: Parts fixtures + goldens

**Files:**
- Modify: `testdata/gen/docx/docx.go` (`Builder.AddPart` — write an arbitrary part + content-type override)
- Modify: `testdata/gen/docx/fixtures.go` (add `docx-header-footer`, `docx-multisection`, `docx-footnote`)

The Builder's `AddRel` already exists (Phase 4). Add a generic `AddPart(name, contentType, xml)` to write header/footer/footnotes parts with their content-type overrides.

- [ ] **Step 1: Extend the Builder for arbitrary parts**

In `testdata/gen/docx/docx.go`, add a field + setter and write them. Extend the struct:

```go
	parts []part // arbitrary extra parts (headers/footers/footnotes)
```

```go
// part is one arbitrary OPC part with its content-type override.
type part struct {
	name, contentType, xml string
}

// AddPart registers an extra part under word/ (e.g. "header1.xml") with its XML
// and content-type override.
func (b *Builder) AddPart(name, contentType, xml string) *Builder {
	b.parts = append(b.parts, part{name: "word/" + name, contentType: contentType, xml: xml})
	return b
}
```

In `Bytes`, write them (after the media loop):

```go
	for _, p := range b.parts {
		write(p.name, p.xml)
	}
```

Update `contentTypes` to append each part's override. Change its call to pass `b.parts`:

```go
	write("[Content_Types].xml", contentTypes(b.stylesXML != "", b.numberingXML != "", len(b.media) > 0, b.parts))
```

and in `contentTypes`, add `parts []part` and append before the closing tag:

```go
	for _, p := range parts {
		s += `
  <Override PartName="/` + p.name + `" ContentType="` + p.contentType + `"/>`
	}
```

- [ ] **Step 2: Add the fixtures**

In `testdata/gen/docx/fixtures.go`, add to `Core`:

```go
	{
		Name:  "header-footer",
		Desc:  "a default header and footer rendered in the page margins",
		Pages: 1,
		Build: headerFooterDocx,
	},
	{
		Name:  "footnote",
		Desc:  "a footnote reference (superscript marker) with a parsed note",
		Pages: 1,
		Build: footnoteDocx,
	},
```

(Multi-section rendering across differing page sizes is deferred per Task 5.3; a `docx-multisection` golden is omitted — the section collection is covered by the unit test `TestParseCollectsAllSections`. Add the golden fixture when the paged engine gains per-section reflow.)

Add the builders + content-type constants:

```go
const ctHeader = "application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"
const ctFooter = "application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"
const ctFootnotes = "application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml"
const relHeader = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
const relFooter = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer"
const relFootnotes = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes"

// docCloseHF is docClose with header/footer references in the section.
const docCloseHF = `<w:sectPr>` +
	`<w:headerReference w:type="default" r:id="rId10"/>` +
	`<w:footerReference w:type="default" r:id="rId11"/>` +
	`<w:pgSz w:w="12240" w:h="15840"/>` +
	`<w:pgMar w:top="1440" w:bottom="1440" w:left="1440" w:right="1440" w:header="720" w:footer="720"/>` +
	`</w:sectPr></w:body></w:document>`

func headerFooterDocx() []byte {
	doc := docOpenR +
		para("", "", "Body text between a header and a footer.") +
		docCloseHF
	hdr := `<?xml version="1.0"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		para("", "center", "DOCUMENT HEADER") + `</w:hdr>`
	ftr := `<?xml version="1.0"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		para("", "center", "Page footer") + `</w:ftr>`
	return New().SetDocument(doc).
		AddPart("header1.xml", ctHeader, hdr).
		AddPart("footer1.xml", ctFooter, ftr).
		AddRel("rId10", relHeader, "header1.xml", "").
		AddRel("rId11", relFooter, "footer1.xml", "").
		Bytes()
}

func footnoteDocx() []byte {
	p := `<w:p><w:r><w:t xml:space="preserve">A claim needing a citation</w:t></w:r>` +
		`<w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:footnoteReference w:id="2"/></w:r></w:p>`
	doc := docOpenR + p + docClose
	notes := `<?xml version="1.0"?><w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:footnote w:id="2">` + para("", "", "The supporting citation.") + `</w:footnote>` +
		`</w:footnotes>`
	return New().SetDocument(doc).
		AddPart("footnotes.xml", ctFootnotes, notes).
		AddRel("rId12", relFootnotes, "footnotes.xml", "").
		Bytes()
}
```

- [ ] **Step 3: Generate + eyeball the goldens**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -update`
Open `pkg/doctaculous/testdata/golden/docx-header-footer.png` (verify "DOCUMENT HEADER" centered in the top margin, "Page footer" centered in the bottom margin, body between) and `docx-footnote.png` (verify a superscript "2" after the claim text).

If the header/footer do NOT appear in the margins, the `@page` margin-box syntax in `docxPageSheet` is wrong — cross-check against an existing HTML `@page` margin-box test (Task 5.2 Step 3c note) and fix before accepting.

- [ ] **Step 4: Run the golden test for real**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -v`
Expected: PASS for all eleven fixtures; prior goldens byte-identical.

- [ ] **Step 5: Commit**

```bash
git add testdata/gen/docx/docx.go testdata/gen/docx/fixtures.go pkg/doctaculous/testdata/golden/docx-header-footer.png pkg/doctaculous/testdata/golden/docx-footnote.png
git commit -m "docx: header/footer + footnote fixtures/goldens; Builder.AddPart"
```

### Task 5.6: Verify + PR

- [ ] **Step 1: Full suite + vet + lint + race**

Run: `go test ./... && go vet ./... && golangci-lint run && go test -race ./pkg/docx/... ./pkg/doctaculous`
Expected: all pass; prior DOCX goldens unchanged.

- [ ] **Step 2: Open the PR**

```bash
git push -u origin docx-fidelity-5-parts
gh pr create --title "docx: headers/footers, multi-section, footnotes (phase 5 of DOCX fidelity)" \
  --body "Parse header/footer parts (rendered via the paged engine's @page margin-box + running-element mechanism), collect all section geometry, and parse footnotes (superscript reference markers + note text on the model). New docx-header-footer + docx-footnote fixtures/goldens; prior goldens byte-identical. Deferred (logged): per-section reflow across differing page sizes, footnote text placement at page bottom. Spec: docs/superpowers/specs/2026-07-02-docx-fidelity-design.md"
```

Wait for green CI + merge before Phase 6.

---

## Phase 6 — Run/paragraph properties (with engine rendering)

**Branch:** `git checkout main && git pull && git checkout -b docx-fidelity-6-runprops`

**Goal:** Parse the remaining run properties (strikethrough, super/subscript, highlight, caps/small-caps) onto the model AND extend the CSS engine to render the three it does not model today — **line-through** (text-decoration), **text-transform** (caps/small-caps), and **real sub/super vertical shifting** — then map DOCX run props onto them. Highlight → `background-color` (inline background). Add `docx-run-props` fixture + golden.

**Parsed-and-deferred (rendered later, each logged):** **underline styles/color** — the parser already reads `w:u` on/off (Phase 0 baseline); its *style* (double/dotted/wavy) and *color* are captured onto the model in Task 6.4 but rendered as a plain underline until the CSS decoration model widens beyond the underline/line-through keyword. **Tab-stop definitions** (`w:tabs`) — captured onto `ParagraphProps` in Task 6.4 for the conversion path, but the inline core advances a `\t` to fixed 8-column stops today (per the `white-space` slice), so custom stop positions are a deferred inline-core change. **small-caps** renders as uppercase (true small capitals need synthesized glyphs — a font follow-up). These are parsed (so DOCX→HTML/Markdown gets them) but not fully rendered; each logs a debug note.

**Engine work is real here.** Tasks 6.1–6.3 extend `pkg/css` (new `ComputedStyle` fields / decoration keyword) + `pkg/layout/inline` (glyph flags) + `pkg/layout/css` (paint), each **byte-identical for existing HTML/DOCX** (a new flag defaults off; the initial value is unchanged). Tasks 6.4–6.5 do the DOCX parse + cascade + lowering. The shared inline core changes are additive (a new opaque glyph flag, mirroring the existing `Underline` flag exactly).

### Task 6.1: Engine — render text-decoration: line-through

**Files:**
- Modify: `pkg/css/value.go` `parseTextDecorationLine` (recognize `line-through`)
- Modify: `pkg/css/cascade.go` (document the widened `TextDecorationLine` values)
- Modify: `pkg/layout/inline/shape.go` (`Run.Strike`/`Glyph.Strike` flags, mirroring `Underline`)
- Modify: `pkg/layout/css/fragment.go` (`Glyph.Strike`; `appendStrikes` mirroring `appendUnderlines`; call it where `appendUnderlines` is called)
- Modify: `pkg/layout/css/inline.go` (set `Run.Strike` from the box's `TextDecorationLine == "line-through"`, where `Run.Underline` is set)

`TextDecorationLine` is a single string today ("none"/"underline"). To carry both underline and line-through, keep it a string but allow the value `"line-through"` (and, minimally, treat the two as independent flags at the glyph level). Since a run rarely has both, model `TextDecorationLine` as the winning keyword and add a separate `Strike` glyph flag driven by it.

- [ ] **Step 1: Write the failing test**

Add `pkg/layout/css/strike_test.go`:

```go
package css

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// TestLineThroughEmitsRule renders a line-through span and asserts a RuleKind item
// is emitted at roughly mid-glyph (above the baseline), mirroring underline.
func TestLineThroughEmitsRule(t *testing.T) {
	items := renderInlineHTML(t, `<span style="text-decoration:line-through">struck</span>`)
	var rules int
	for _, it := range items {
		if it.Kind == layout.RuleKind {
			rules++
		}
	}
	if rules == 0 {
		t.Fatalf("no RuleKind emitted for line-through text")
	}
}
```

If a helper like `renderInlineHTML` does not exist in the css test package, add a minimal one (grep first: `grep -rn "func renderInlineHTML\|func layoutHTMLString" pkg/layout/css/*_test.go`). If absent, model it on an existing fragment/paint test in that package that builds a `cssbox` tree and calls the engine; adapt the assertion to that harness. (Reuse the existing harness rather than inventing a parallel one.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestLineThroughEmitsRule -v`
Expected: FAIL — `line-through` parses to "none" (per `value.go:36`), so no strike rule is emitted.

- [ ] **Step 3a: Recognize line-through in the parser**

In `pkg/css/value.go`, in `parseTextDecorationLine`, add the keyword. Replace:

```go
		if f == "underline" {
			return "underline"
		}
```

with:

```go
		if f == "underline" {
			return "underline"
		}
		if f == "line-through" {
			return "line-through"
		}
```

Update the `TextDecorationLine` doc comment in `pkg/css/cascade.go` to note "underline" | "line-through" | "none" are the modeled values.

- [ ] **Step 3b: Add the Strike glyph flag through the inline core**

In `pkg/layout/inline/shape.go`, add a `Strike bool` field to `Run` (next to `Underline`, line ~48) and to `Glyph` (next to `Underline`, line ~90), each with a doc comment mirroring `Underline`. In the glyph-construction line (~148), add `Strike: r.Strike` to the `base := Glyph{...}` literal.

- [ ] **Step 3c: Carry Strike onto the fragment glyph + paint it**

In `pkg/layout/css/fragment.go`, add a `Strike bool` field to the fragment `Glyph` (next to `Underline`, line ~207). Wherever glyphs are built from `inline.Glyph` (the shaping-result copy), copy `Strike` alongside `Underline` (grep `grep -n "Underline:" pkg/layout/css/*.go` to find the copy site and mirror it).

Add `appendStrikes` next to `appendUnderlines`:

```go
// appendStrikes emits text-decoration:line-through rules for one line: one thin
// rule per run of consecutive struck glyphs, centered vertically on the glyph
// (approximately the x-height midpoint above the baseline). Mirrors
// appendUnderlines but at a mid-glyph Y rather than below the baseline.
func appendStrikes(dst []layout.Item, ln *LineFragment) []layout.Item {
	i := 0
	for i < len(ln.Glyphs) {
		if g := &ln.Glyphs[i]; !g.Strike || g.Outline == nil {
			i++
			continue
		}
		x0 := ln.Glyphs[i].X
		x1 := ln.Glyphs[i].X + ln.Glyphs[i].AdvancePt
		size := ln.Glyphs[i].SizePt
		col := ln.Glyphs[i].Color
		for i++; i < len(ln.Glyphs) && ln.Glyphs[i].Strike && ln.Glyphs[i].Outline != nil; i++ {
			x1 = ln.Glyphs[i].X + ln.Glyphs[i].AdvancePt
			if ln.Glyphs[i].SizePt > size {
				size = ln.Glyphs[i].SizePt
			}
		}
		thickness := size * 0.06
		if thickness < 1 {
			thickness = 1
		}
		// Strike sits ~0.3em above the baseline (near the x-height center).
		yMid := ln.BaselineY - size*0.30
		if x1 > x0 {
			dst = append(dst, layout.Item{
				Kind: layout.RuleKind,
				Rule: layout.RuleItem{XPt: x0, YPt: yMid, WPt: x1 - x0, HPt: thickness, Color: col},
			})
		}
	}
	return dst
}
```

Find the call site of `appendUnderlines` (grep `grep -n "appendUnderlines(" pkg/layout/css/*.go`) and add an `appendStrikes` call right after it, threading the same `dst`/`ln`:

```go
	dst = appendUnderlines(dst, ln)
	dst = appendStrikes(dst, ln)
```

- [ ] **Step 3d: Drive Run.Strike from the box style**

In `pkg/layout/css/inline.go`, find where `Run.Underline` is set from the box's `TextDecorationLine` (grep `grep -n "Underline" pkg/layout/css/inline.go`). Alongside it, set:

```go
		Strike: st.TextDecorationLine == "line-through",
```

(matching how `Underline: st.TextDecorationLine == "underline"` is set — adapt to the exact struct-literal or assignment form there).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestLineThroughEmitsRule -v`
Expected: PASS. Then confirm HTML byte-identity: `go test ./pkg/doctaculous -run 'TestHTMLGolden|TestDOCXGolden'` — all unchanged (no existing content uses line-through).

- [ ] **Step 5: Commit**

```bash
git add pkg/css/value.go pkg/css/cascade.go pkg/layout/inline/shape.go pkg/layout/css/fragment.go pkg/layout/css/inline.go pkg/layout/css/strike_test.go
git commit -m "css: render text-decoration: line-through (Strike glyph flag + paint)"
```

### Task 6.2: Engine — render text-transform (uppercase/lowercase/capitalize)

**Files:**
- Modify: `pkg/css/cascade.go` (`TextTransform string` field, inherited; parse `text-transform`)
- Modify: `pkg/layout/css/inline.go` (apply the transform to a text box's string before shaping)

`text-transform` alters the *rendered* text, so the cleanest, most localized implementation transforms the run's string at the point the inline formatter turns a `BoxText` into an `inline.Run`. `small-caps` (a DOCX `w:smallCaps`) is approximated as `uppercase` in this pass (true small-caps needs synthesized small capitals — a font/paint follow-up, logged); `w:caps` maps to `uppercase`.

- [ ] **Step 1: Write the failing test**

Add `pkg/layout/css/texttransform_test.go`:

```go
package css

import (
	"strings"
	"testing"
)

// TestTextTransformUppercase asserts an uppercased box renders glyphs for the
// uppercased text (the fragment's text carries the transformed string).
func TestTextTransformUppercase(t *testing.T) {
	got := renderInlineText(t, `<span style="text-transform:uppercase">hello</span>`)
	if !strings.Contains(got, "HELLO") {
		t.Fatalf("rendered text = %q, want to contain HELLO", got)
	}
	if strings.Contains(got, "hello") {
		t.Fatalf("rendered text = %q, still contains lowercase 'hello'", got)
	}
}
```

`renderInlineText` should return the concatenated `Runes` of the laid-out glyphs (or the text boxes' resolved strings). Grep for an existing helper that extracts rendered text (`grep -rn "func.*renderInlineText\|Runes\|\.Text" pkg/layout/css/*_test.go`); reuse or add a minimal one that walks the fragment tree collecting `BoxText` strings post-transform. If the engine transforms at shaping time (not on the box), assert on the shaped glyph runes instead.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestTextTransformUppercase -v`
Expected: FAIL — `text-transform` is unmodeled, so text stays "hello".

- [ ] **Step 3a: Add the property**

In `pkg/css/cascade.go`:
- Add the field (near `TextDecorationLine`): `// TextTransform: "none" (initial) | "uppercase" | "lowercase" | "capitalize". Inherited.` then `TextTransform string`.
- In the inherited-copy block (where `cs.TextDecorationLine = parent.TextDecorationLine` is, ~line 413): add `cs.TextTransform = parent.TextTransform`.
- In `InitialStyle`/the initial-values literal (~line 449): add `TextTransform: "none",`.
- In the declaration switch (where `case "text-decoration-line"` / similar are handled, ~line 586): add:

```go
	case "text-transform":
		switch d.Value {
		case "uppercase", "lowercase", "capitalize", "none":
			cs.TextTransform = d.Value
		}
```

- [ ] **Step 3b: Apply the transform before shaping**

In `pkg/layout/css/inline.go`, find where a `BoxText`'s `Text` becomes an `inline.Run` text string (grep `grep -n "inline.Run{\|Text:.*b.Text\|\.Text" pkg/layout/css/inline.go`). Apply the transform there:

```go
	runText := applyTextTransform(b.Text, st.TextTransform)
	// ... use runText where b.Text was used to build the inline.Run
```

Add the helper (in `inline.go`):

```go
// applyTextTransform applies the CSS text-transform to a run's text. "capitalize"
// uppercases the first letter of each whitespace-separated word; small-caps is
// handled upstream by mapping to uppercase.
func applyTextTransform(s, transform string) string {
	switch transform {
	case "uppercase":
		return strings.ToUpper(s)
	case "lowercase":
		return strings.ToLower(s)
	case "capitalize":
		return capitalizeWords(s)
	default:
		return s
	}
}

// capitalizeWords uppercases the first rune of each run of letters, leaving the
// rest unchanged (a pragmatic approximation of CSS "capitalize").
func capitalizeWords(s string) string {
	var b strings.Builder
	prevLetter := false
	for _, r := range s {
		if !prevLetter && unicode.IsLetter(r) {
			b.WriteRune(unicode.ToUpper(r))
		} else {
			b.WriteRune(r)
		}
		prevLetter = unicode.IsLetter(r)
	}
	return b.String()
}
```

Add `"strings"` and `"unicode"` to `inline.go`'s imports if absent.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestTextTransformUppercase -v`
Expected: PASS. HTML/DOCX byte-identity: `go test ./pkg/doctaculous -run 'TestHTMLGolden|TestDOCXGolden'` — unchanged (nothing uses text-transform yet).

- [ ] **Step 5: Commit**

```bash
git add pkg/css/cascade.go pkg/layout/css/inline.go pkg/layout/css/texttransform_test.go
git commit -m "css: render text-transform (uppercase/lowercase/capitalize)"
```

### Task 6.3: Engine — render sub/superscript vertical shift

**Files:**
- Modify: `pkg/layout/inline/shape.go` (a per-run baseline-shift input `Run.BaselineShiftPt` carried onto glyphs)
- Modify: `pkg/layout/css/inline.go` (compute the shift from `VerticalAlign` sub/super + apply)

`vertical-align: super`/`sub` shifts a run's glyphs up/down relative to the line baseline. The simplest correct-enough model: a per-run baseline shift (points) applied to the glyph Y when placing. Super ≈ +0.33em, sub ≈ −0.20em (browser-ish). The font size is typically already reduced by the caller (DOCX footnote/superscript sets 0.75em); this task adds only the vertical offset.

- [ ] **Step 1: Write the failing test**

Add `pkg/layout/css/superscript_test.go`:

```go
package css

import "testing"

// TestSuperscriptShiftsGlyphUp asserts a superscript run's glyphs sit above the
// surrounding baseline (smaller Y in top-left origin space).
func TestSuperscriptShiftsGlyphUp(t *testing.T) {
	baseY := firstGlyphBaselineY(t, `<span>x</span>`)
	supY := firstGlyphBaselineY(t, `<span style="vertical-align:super; font-size:75%">x</span>`)
	if !(supY < baseY) {
		t.Fatalf("superscript baseline Y = %.2f, want < normal %.2f", supY, baseY)
	}
}
```

`firstGlyphBaselineY` returns the Y of the first laid-out glyph's baseline. Grep for an existing geometry helper (`grep -rn "BaselineY\|\.Y\b" pkg/layout/css/*_test.go`) and reuse the harness that already inspects glyph positions; adapt the accessor.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestSuperscriptShiftsGlyphUp -v`
Expected: FAIL — `vertical-align: super` is not applied (inline.go:307 notes it is unhandled), so both baselines match.

- [ ] **Step 3a: Add a baseline-shift input to the inline core**

In `pkg/layout/inline/shape.go`, add `BaselineShiftPt float64` to `Run` (a positive value raises the run) with a doc comment, and to `Glyph`. In the glyph-construction line (~148), carry it: `BaselineShiftPt: r.BaselineShiftPt`. Zero (the default) preserves current behavior for every caller.

- [ ] **Step 3b: Apply the shift when placing glyphs**

In `pkg/layout/inline/break.go` or wherever a glyph's line Y/baseline is finalized (grep `grep -n "BaselineY\|baseline\|\.Y =" pkg/layout/inline/*.go` and `pkg/layout/css/inline.go`), subtract the shift from the glyph's Y (top-left origin: up = smaller Y). If the placement lives in `pkg/layout/css` (the engine positions glyphs after `inline.Place`), apply it there instead:

```go
	g.Y -= g.BaselineShiftPt // super (positive) raises the glyph; sub (negative) lowers it
```

Locate the single site where a glyph's final Y is assigned from the line baseline and apply the shift once there.

- [ ] **Step 3c: Compute the shift from vertical-align**

In `pkg/layout/css/inline.go`, where the `inline.Run` is built for a text box, set the shift from the box style:

```go
	Run{ /* ...existing fields... */ BaselineShiftPt: baselineShiftPt(st) }
```

Add the helper:

```go
// baselineShiftPt returns the baseline shift (points, positive = up) for a box's
// vertical-align: super/sub. Other values (baseline/top/middle/bottom) yield 0
// (their line-box effects are handled elsewhere / deferred). The shift scales with
// the run's font size.
func baselineShiftPt(st gcss.ComputedStyle) float64 {
	switch st.VerticalAlign {
	case "super":
		return st.FontSizePt * 0.33
	case "sub":
		return -st.FontSizePt * 0.20
	default:
		return 0
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestSuperscriptShiftsGlyphUp -v`
Expected: PASS. Byte-identity: `go test ./pkg/doctaculous -run 'TestHTMLGolden|TestDOCXGolden'` — unchanged (no existing content sets super/sub; the DOCX footnote fixture from Phase 5 DID set `VerticalAlign:"super"`, so its golden WILL now shift — regenerate it: `go test ./pkg/doctaculous -run TestDOCXGolden -update` and eyeball `docx-footnote.png` shows the "2" raised. Note this in the commit).

- [ ] **Step 5: Commit**

```bash
git add pkg/layout/inline/shape.go pkg/layout/inline/break.go pkg/layout/css/inline.go pkg/layout/css/superscript_test.go pkg/doctaculous/testdata/golden/docx-footnote.png
git commit -m "css: render vertical-align super/sub as a baseline shift (updates docx-footnote golden)"
```

### Task 6.4: Parse the Tier-3 run properties

**Files:**
- Modify: `pkg/docx/model.go` (`RunProps` gains Strike/VertAlign/Highlight/Caps/SmallCaps + Has* flags)
- Modify: `pkg/docx/parse.go` `applyRPrChild` (read `w:strike`/`w:dstrike`/`w:vertAlign`/`w:highlight`/`w:caps`/`w:smallCaps`)

- [ ] **Step 1: Write the failing test**

Add `pkg/docx/parse_runprops_test.go`:

```go
package docx

import "testing"

func TestParseTier3RunProps(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:rPr>
  <w:strike/>
  <w:vertAlign w:val="superscript"/>
  <w:highlight w:val="yellow"/>
  <w:caps/>
</w:rPr><w:t>styled</w:t></w:r></w:p>
</w:body></w:document>`)
	rp := doc.Body[0].Paragraph.Content[0].Run.Props
	if !rp.HasStrike || !rp.Strike {
		t.Fatalf("Strike = %v (has %v), want true", rp.Strike, rp.HasStrike)
	}
	if rp.VertAlign != VertAlignSuperscript {
		t.Fatalf("VertAlign = %v, want superscript", rp.VertAlign)
	}
	if !rp.HasHighlight || rp.Highlight.R != 0xFF || rp.Highlight.G != 0xFF || rp.Highlight.B != 0x00 {
		t.Fatalf("Highlight = %+v (has %v), want yellow", rp.Highlight, rp.HasHighlight)
	}
	if !rp.HasCaps || !rp.Caps {
		t.Fatalf("Caps = %v (has %v), want true", rp.Caps, rp.HasCaps)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx -run TestParseTier3RunProps -v`
Expected: FAIL — the new `RunProps` fields don't exist; the elements are ignored.

- [ ] **Step 3a: Extend RunProps**

In `pkg/docx/model.go`, add to `RunProps` (after `Family`):

```go
	// Strike is w:strike/w:dstrike (strikethrough); HasStrike marks it set.
	Strike, HasStrike bool
	// VertAlign is w:vertAlign (baseline/superscript/subscript).
	VertAlign VertAlign
	// Highlight is the w:highlight color; HasHighlight marks it set.
	Highlight    color.RGBA
	HasHighlight bool
	// Caps/SmallCaps are w:caps/w:smallCaps; Has* mark them set.
	Caps, HasCaps           bool
	SmallCaps, HasSmallCaps bool
	// UnderlineStyle is the w:u val (e.g. "single","double","dotted","wave"), kept
	// for the conversion path; rendering treats any non-none as a plain underline.
	UnderlineStyle string
	// UnderlineColor is the w:u color; HasUnderlineColor marks it set.
	UnderlineColor    color.RGBA
	HasUnderlineColor bool
```

Also capture the underline *style* and *color* in the existing `w:u` handler. In `pkg/docx/parse.go`, `applyRPrChild`, extend the `case "u":` block (it currently sets only `Underline`/`HasUnderline`) to also record the style/color:

```go
	case "u":
		val := wVal(e)
		props.Underline = val != "none" && val != ""
		props.HasUnderline = true
		if val != "" && val != "none" {
			props.UnderlineStyle = val
		}
		if c, ok := parseColor(mustColorAttr(e)); ok {
			props.UnderlineColor = c
			props.HasUnderlineColor = true
		}
```

(Replace the whole existing `case "u":` block with the above. `mustColorAttr` was added in Task 2.2.)

And add a tab-stops field to `ParagraphProps` for the conversion path. In `pkg/docx/model.go`, add to `ParagraphProps` (after the `NumID`/`ILvl`/`HasNum` block from Phase 3):

```go
	// TabStops are the w:tabs stop positions (twips from the margin) with their
	// alignment, captured for the conversion path. Rendering uses fixed 8-column
	// stops today (custom positions are a deferred inline-core change).
	TabStops []TabStop
```

Add the `TabStop` type (near `ParagraphProps`):

```go
// TabStop is one w:tab definition inside w:tabs: a position (twips) and its
// alignment (left/center/right/decimal). Val "clear" removes an inherited stop.
type TabStop struct {
	PosTwips Twips
	Align    string // "left" (default), "center", "right", "decimal", "clear"
}
```

Parse `w:tabs` in `parsePPr` (it has `w:tab` children, so it needs a small sub-loop like `numPr`). In `pkg/docx/parse.go`, add a `tabs` branch to `parsePPr`'s switch alongside `numPr`:

```go
				case "tabs":
					applyTabs(&props, dec)
					continue
```

Add the helper near `applyNumPr`:

```go
// applyTabs reads a w:tabs element's w:tab children into the paragraph's tab
// stops (position in twips + alignment).
func applyTabs(props *ParagraphProps, dec *xml.Decoder) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "tab" {
				var ts TabStop
				if v, ok := wAttrInt(t, "pos"); ok {
					ts.PosTwips = Twips(v)
				}
				if a, ok := wAttr(t, "val"); ok {
					ts.Align = a
				}
				props.TabStops = append(props.TabStops, ts)
			}
			_ = dec.Skip()
		case xml.EndElement:
			if t.Name.Local == "tabs" {
				return
			}
		}
	}
}
```


Add the `VertAlign` type (near `Justify`):

```go
// VertAlign is a run's vertical alignment (w:vertAlign).
type VertAlign int

const (
	VertAlignBaseline VertAlign = iota
	VertAlignSuperscript
	VertAlignSubscript
)
```

- [ ] **Step 3b: Read them in applyRPrChild**

In `pkg/docx/parse.go`, add cases to `applyRPrChild`'s switch (after the existing `rFonts` case):

```go
	case "strike", "dstrike":
		props.Strike = parseOnOff(wVal(e))
		props.HasStrike = true
	case "vertAlign":
		switch wVal(e) {
		case "superscript":
			props.VertAlign = VertAlignSuperscript
		case "subscript":
			props.VertAlign = VertAlignSubscript
		default:
			props.VertAlign = VertAlignBaseline
		}
	case "highlight":
		if c, ok := highlightColor(wVal(e)); ok {
			props.Highlight = c
			props.HasHighlight = true
		}
	case "caps":
		props.Caps = parseOnOff(wVal(e))
		props.HasCaps = true
	case "smallCaps":
		props.SmallCaps = parseOnOff(wVal(e))
		props.HasSmallCaps = true
```

Add the `highlightColor` helper (w:highlight is a named color, not hex):

```go
// highlightColor maps a w:highlight named color to an RGBA. Unknown names yield
// ok=false. These are the 16 WordprocessingML highlight names.
func highlightColor(name string) (color.RGBA, bool) {
	m := map[string]color.RGBA{
		"yellow":      {R: 0xFF, G: 0xFF, B: 0x00, A: 0xFF},
		"green":       {R: 0x00, G: 0xFF, B: 0x00, A: 0xFF},
		"cyan":        {R: 0x00, G: 0xFF, B: 0xFF, A: 0xFF},
		"magenta":     {R: 0xFF, G: 0x00, B: 0xFF, A: 0xFF},
		"blue":        {R: 0x00, G: 0x00, B: 0xFF, A: 0xFF},
		"red":         {R: 0xFF, G: 0x00, B: 0x00, A: 0xFF},
		"darkBlue":    {R: 0x00, G: 0x00, B: 0x8B, A: 0xFF},
		"darkCyan":    {R: 0x00, G: 0x8B, B: 0x8B, A: 0xFF},
		"darkGreen":   {R: 0x00, G: 0x64, B: 0x00, A: 0xFF},
		"darkMagenta": {R: 0x8B, G: 0x00, B: 0x8B, A: 0xFF},
		"darkRed":     {R: 0x8B, G: 0x00, B: 0x00, A: 0xFF},
		"darkYellow":  {R: 0x80, G: 0x80, B: 0x00, A: 0xFF},
		"darkGray":    {R: 0xA9, G: 0xA9, B: 0xA9, A: 0xFF},
		"lightGray":   {R: 0xD3, G: 0xD3, B: 0xD3, A: 0xFF},
		"black":       {R: 0x00, G: 0x00, B: 0x00, A: 0xFF},
		"white":       {R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
	}
	c, ok := m[name]
	return c, ok
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx -run TestParseTier3RunProps -v`
Expected: PASS. Full package: `go test ./pkg/docx`.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/model.go pkg/docx/parse.go pkg/docx/parse_runprops_test.go
git commit -m "docx: parse strike/vertAlign/highlight/caps/smallCaps run properties"
```

### Task 6.5: Cascade + lower the Tier-3 properties

**Files:**
- Modify: `pkg/docx/style/style.go` (`EffectiveRun` gains the fields; `mergeRun` merges them)
- Modify: `pkg/docx/cssbox/lower.go` `runTextBox` (map onto `ComputedStyle`)

- [ ] **Step 1: Write the failing test**

Add to `pkg/docx/cssbox/lower_test.go`:

```go
func TestLowerTier3RunPropsToStyle(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{Content: []docx.ParaChild{
			{Run: &docx.Run{Text: "x", Props: docx.RunProps{
				Strike: true, HasStrike: true,
				VertAlign:    docx.VertAlignSuperscript,
				Highlight:    colorYellow(),
				HasHighlight: true,
				Caps:         true, HasCaps: true,
			}}},
		}}}},
	}
	root := lowerDoc(t, d)
	tx := root.Children[len(root.Children)-1].Children[0].Children[0]
	if tx.Style.TextDecorationLine != "line-through" {
		t.Fatalf("decoration = %q, want line-through", tx.Style.TextDecorationLine)
	}
	if tx.Style.VerticalAlign != "super" {
		t.Fatalf("vertical-align = %q, want super", tx.Style.VerticalAlign)
	}
	if tx.Style.BackgroundColor.R != 0xFF || tx.Style.BackgroundColor.G != 0xFF || tx.Style.BackgroundColor.B != 0x00 {
		t.Fatalf("background = %+v, want yellow", tx.Style.BackgroundColor)
	}
	if tx.Style.TextTransform != "uppercase" {
		t.Fatalf("text-transform = %q, want uppercase", tx.Style.TextTransform)
	}
}

func colorYellow() color.RGBA { return color.RGBA{R: 0xFF, G: 0xFF, B: 0x00, A: 0xFF} }
```

Add `"image/color"` to `lower_test.go`'s imports if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx/cssbox -run TestLowerTier3RunPropsToStyle -v`
Expected: FAIL — `EffectiveRun` lacks the fields; `runTextBox` doesn't set them.

- [ ] **Step 3a: Extend the cascade**

In `pkg/docx/style/style.go`:

Add to `EffectiveRun` (after `Color`):

```go
	Strike    bool
	VertAlign docx.VertAlign
	Highlight color.RGBA
	HasHighlight bool
	Caps      bool
	SmallCaps bool
```

In `mergeRun`, merge the new toggles (after the color merge):

```go
	if over.HasStrike {
		out.Strike, out.HasStrike = over.Strike, true
	}
	if over.VertAlign != docx.VertAlignBaseline {
		out.VertAlign = over.VertAlign
	}
	if over.HasHighlight {
		out.Highlight, out.HasHighlight = over.Highlight, true
	}
	if over.HasCaps {
		out.Caps, out.HasCaps = over.Caps, true
	}
	if over.HasSmallCaps {
		out.SmallCaps, out.HasSmallCaps = over.SmallCaps, true
	}
```

In `EffectiveRun` (the resolver method), populate the new fields from `merged` (after the existing `if merged.HasColor` block):

```go
	if merged.HasStrike {
		eff.Strike = merged.Strike
	}
	eff.VertAlign = merged.VertAlign
	if merged.HasHighlight {
		eff.Highlight = merged.Highlight
		eff.HasHighlight = true
	}
	if merged.HasCaps {
		eff.Caps = merged.Caps
	}
	if merged.HasSmallCaps {
		eff.SmallCaps = merged.SmallCaps
	}
```

- [ ] **Step 3b: Map onto ComputedStyle in runTextBox**

In `pkg/docx/cssbox/lower.go`, `runTextBox`, after the existing underline block, add:

```go
	if er.Strike {
		cs.TextDecorationLine = "line-through" // wins over underline when both set (rare)
	}
	switch er.VertAlign {
	case docx.VertAlignSuperscript:
		cs.VerticalAlign = "super"
		cs.FontSizePt = er.SizePt * 0.75
	case docx.VertAlignSubscript:
		cs.VerticalAlign = "sub"
		cs.FontSizePt = er.SizePt * 0.75
	}
	if er.HasHighlight {
		cs.BackgroundColor = er.Highlight
	}
	if er.Caps || er.SmallCaps {
		cs.TextTransform = "uppercase" // small-caps approximated as uppercase (logged deferral)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx/cssbox -run TestLowerTier3RunPropsToStyle -v`
Expected: PASS. `go build ./...` clean.

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/style/style.go pkg/docx/cssbox/lower.go pkg/docx/cssbox/lower_test.go
git commit -m "docx: cascade + lower strike/vertAlign/highlight/caps run properties"
```

### Task 6.6: Run-props fixture + golden

**Files:**
- Modify: `testdata/gen/docx/fixtures.go` (add `docx-run-props`)

- [ ] **Step 1: Add the fixture**

In `testdata/gen/docx/fixtures.go`, add to `Core`:

```go
	{
		Name:  "run-props",
		Desc:  "strikethrough, superscript, highlight, and caps runs",
		Pages: 1,
		Build: runPropsDocx,
	},
```

Add the builder + a raw-run helper:

```go
// runRPr builds a run with a raw w:rPr XML fragment and text.
func runRPr(rPr, text string) string {
	return `<w:r><w:rPr>` + rPr + `</w:rPr><w:t xml:space="preserve">` + text + `</w:t></w:r>`
}

func runPropsDocx() []byte {
	p := `<w:p>` +
		runRPr(`<w:strike/>`, "struck ") +
		`<w:r><w:t xml:space="preserve">normal E=mc</w:t></w:r>` +
		runRPr(`<w:vertAlign w:val="superscript"/>`, "2") +
		runRPr(`<w:highlight w:val="yellow"/>`, " highlighted ") +
		runRPr(`<w:caps/>`, "small caps") +
		`</w:p>`
	doc := docOpen + p + docClose
	return New().SetDocument(doc).Bytes()
}
```

- [ ] **Step 2: Generate + eyeball**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -update`
Open `pkg/doctaculous/testdata/golden/docx-run-props.png`. Verify: "struck" has a line through it; "E=mc²" with a raised small 2; "highlighted" on a yellow background; "SMALL CAPS" uppercased.

- [ ] **Step 3: Run the golden test for real**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -v`
Expected: PASS for all twelve fixtures; prior goldens byte-identical (except `docx-footnote`, already updated in Task 6.3).

- [ ] **Step 4: Commit**

```bash
git add testdata/gen/docx/fixtures.go pkg/doctaculous/testdata/golden/docx-run-props.png
git commit -m "docx: run-props fixture + golden (strike, superscript, highlight, caps)"
```

### Task 6.7: Verify + PR

- [ ] **Step 1: Full suite + vet + lint + race**

Run: `go test ./... && go vet ./... && golangci-lint run && go test -race ./...`
Expected: all pass. Confirm HTML goldens unchanged (the engine additions are byte-identical for HTML); only `docx-footnote` + the new `docx-run-props` differ among DOCX goldens.

- [ ] **Step 2: Open the PR**

```bash
git push -u origin docx-fidelity-6-runprops
gh pr create --title "docx: run properties + engine rendering (phase 6 of DOCX fidelity)" \
  --body "Parse strike/vertAlign/highlight/caps/smallCaps run properties, cascade them, and render them: extended the CSS engine with text-decoration:line-through, text-transform, and real sub/super baseline shift (all HTML byte-identical). New docx-run-props fixture/golden; docx-footnote golden updated (superscript now shifts). small-caps approximated as uppercase (logged). Spec: docs/superpowers/specs/2026-07-02-docx-fidelity-design.md"
```

This is the final phase. After merge, the DOCX fidelity pass is complete: tables, lists, images, hyperlinks, headers/footers, multi-section, footnotes, and rich run properties all parse and render, with the `docx` model serving as the semantic hub for the upcoming DOCX→HTML/Markdown conversion sub-project.
