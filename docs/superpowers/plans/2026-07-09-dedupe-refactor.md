# Duplication Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the verified duplication clusters found in the 2026-07-09 code-quality audit — every semantically identical copy gets one home, with zero behavior change except one deliberate fix (Markdown `<s>`/`<del>` strikethrough via SemTag).

**Architecture:** Three independent branches → PRs off `main`, per project convention. PR 1 extracts the shared cssbox-analysis layer the markdown/htmlwrite writers hand-copied. PR 2 dedupes the drift-prone PDF/font pairs (SFNT builder, page resources, text-rendering matrix, color math). PR 3 is the mechanical batch (flexfix/gridfix merge, builtin min/max, pkg/css and CLI small dedupes).

**Tech Stack:** Pure Go 1.26. Verification per task: `go test` on touched packages; per PR: `gofmt -l`, `go vet ./...`, `golangci-lint run`, `go test -race ./...`, and confirmation that **no golden PNG/PDF changes** (all refactors must be byte-identical; the only behavior change is the Task 1 Markdown fix, locked by a new unit test).

**Ground rules for every task:**
- These are refactors: existing tests are the safety net. The ONLY new test is in Task 1 (the one behavior change).
- Never change a function's semantics while moving it. If a move forces a semantic question, stop and surface it.
- Doc comments move with their functions; drop the now-obsolete "Mirrors …" cross-references.
- Commit after each task with a `refactor:` message.

---

## PR 1 — branch `refactor/writer-shared-walker`

Start: `git checkout main && git pull && git checkout -b refactor/writer-shared-walker`

### Task 1: Shared boxwalk package + Markdown `<s>` fix

The markdown and htmlwrite writers each carry a byte-identical copy of the cssbox
tree-analysis layer (~250 lines). The copies have already drifted: htmlwrite's
`collectRuns` handles `SemTag == "s"` (`pkg/render/htmlwrite/inline.go:76`), markdown's
does not (`pkg/render/markdown/inline.go:69`), so `<s>`/`<del>` whose author CSS
overrides `text-decoration` lose strikethrough in Markdown. The shared copy keeps the
htmlwrite (fuller) behavior — that is the fix.

**Files:**
- Create: `pkg/render/internal/boxwalk/boxwalk.go` (block/list/table structure helpers)
- Create: `pkg/render/internal/boxwalk/inline.go` (inline-run model)
- Modify: `pkg/render/markdown/markdown.go`, `inline.go`, `list.go`, `table.go`
- Test: `pkg/render/markdown/markdown_test.go`

- [x] **Step 1: Write the failing test**

Append to `pkg/render/markdown/markdown_test.go` (uses the existing `renderHTML` helper at the top of that file):

```go
func TestStrikethroughSemantic(t *testing.T) {
	// <s>/<del> must strike via their semantic role even when author CSS overrides
	// the UA line-through (SemTag "s" — previously only honored by htmlwrite).
	got := renderHTML(t, `<html><body><p>old <s style="text-decoration: underline">gone</s> text</p></body></html>`, false)
	want := "old ~~gone~~ text\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	// The styled path (UA line-through) must keep working too.
	got = renderHTML(t, `<html><body><p>a <del>cut</del> word</p></body></html>`, false)
	want = "a ~~cut~~ word\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}
```

- [x] **Step 2: Run it to make sure it fails**

Run: `go test ./pkg/render/markdown -run TestStrikethroughSemantic -v`
Expected: FAIL — first assertion gets `"old gone text\n"` (no `~~`).

- [x] **Step 3: Create `pkg/render/internal/boxwalk`**

`boxwalk.go` — move these functions **verbatim** from `pkg/render/markdown` (they are
byte-identical to the htmlwrite copies; spot-check with diff if unsure), exporting each:

| New name (boxwalk) | Moved from |
|---|---|
| `HasInlineContent`, `IsBlockContainer` | `markdown/markdown.go:198-215` |
| `CollectRows`, `RowIsAllHeader`, `IsHeaderCell`, `CellBoxesOf`, `ClampSpan`, `FilterEmpty` | `markdown/table.go:247-334` |
| `IsListContainer`, `WithoutNestedLists`, `StripMarkerPrefix`, `LeadingCheckbox`, `IsOrderedMarker` | `markdown/list.go:68-164` |

Package doc: `// Package boxwalk holds the format-neutral cssbox tree analysis shared by the markdown and htmlwrite writers.`

`inline.go` — the inline-run model. `CollectRuns` gains an `image` callback (each
writer renders `<img>` differently) and keeps htmlwrite's `case "s"`:

```go
package boxwalk

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// InlineState is the inherited inline styling in force at a point in the walk.
type InlineState struct {
	Bold   bool
	Italic bool
	Strike bool
	Code   bool
	Href   string // non-empty inside a link
}

// StyledRun is a run of text with its resolved inline styling. Literal, when
// non-empty, is pre-formatted output (e.g. an image tag) emitted verbatim without
// escaping or emphasis wrapping; a literal run is never merged with a neighbor.
type StyledRun struct {
	Text    string
	Literal string
	InlineState
}

// CollectRuns walks b's inline subtree, threading the styling state, appending a
// StyledRun for each text leaf. Bold/italic come from the computed style (so DOCX
// bold, which has no <strong> tag, is honored) as well as the SemTag; code and href
// come from SemTag/Href. image renders an <img> replaced box to the writer's
// format (its result is a Literal run).
func CollectRuns(b *cssbox.Box, st InlineState, image func(*cssbox.ReplacedContent) string, out *[]StyledRun) {
	if b.Style.Bold {
		st.Bold = true
	}
	if b.Style.Italic {
		st.Italic = true
	}
	if b.Style.TextDecorationLine == "line-through" {
		st.Strike = true
	}
	switch b.SemTag {
	case "strong":
		st.Bold = true
	case "em":
		st.Italic = true
	case "s":
		st.Strike = true
	case "code":
		st.Code = true
	case "a":
		if b.Href != "" {
			st.Href = b.Href
		}
	}
	if b.Kind == cssbox.BoxText {
		if b.Text != "" {
			*out = append(*out, StyledRun{Text: b.Text, InlineState: st})
		}
		return
	}
	if b.Kind == cssbox.BoxReplaced && b.Replaced != nil && b.Replaced.Tag == "img" {
		*out = append(*out, StyledRun{Literal: image(b.Replaced), InlineState: st})
		return
	}
	for _, c := range b.Children {
		CollectRuns(c, st, image, out)
	}
}

// Coalesce merges adjacent runs with identical styling so a single element split
// into multiple text leaves emits one marker/tag pair.
func Coalesce(runs []StyledRun) []StyledRun {
	var out []StyledRun
	for _, r := range runs {
		if n := len(out); n > 0 && r.Literal == "" && out[n-1].Literal == "" && out[n-1].InlineState == r.InlineState {
			out[n-1].Text += r.Text
			continue
		}
		out = append(out, r)
	}
	return out
}

// CollapseSpaces collapses runs of whitespace to a single space and trims the
// ends, the normal-flow whitespace model for inline content.
func CollapseSpaces(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// RawText concatenates every text leaf under b verbatim (no whitespace
// collapsing), used for <pre> content where whitespace is significant.
func RawText(b *cssbox.Box) string {
	var sb strings.Builder
	var walk func(*cssbox.Box)
	walk = func(n *cssbox.Box) {
		if n.Kind == cssbox.BoxText {
			sb.WriteString(n.Text)
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(b)
	return sb.String()
}
```

- [x] **Step 4: Repoint `pkg/render/markdown`**

Delete every moved declaration from `markdown.go`/`inline.go`/`list.go`/`table.go`
(including `inlineState`, `styledRun`, `collectRuns`, `coalesce`, `collapseSpaces`,
`rawText`) and replace uses with the `boxwalk.` equivalents. Mechanical notes:
- `inlineOpt` becomes: build `[]boxwalk.StyledRun` via `boxwalk.CollectRuns(b, boxwalk.InlineState{}, imageMarkup, &runs)`, then `boxwalk.Coalesce`, then the existing per-run loop (field renames: `r.bold`→`r.Bold`, etc.; `renderRun` takes `boxwalk.StyledRun`).
- `imageMarkup`, `renderRun`, `escapeText`, `escapeURL`, `plainLiteral`, `mdEscaper` stay in markdown (format-specific).
- `buildGrid`, `cellData`, `tableModel`, and the GFM/plain emit stay; they now call `boxwalk.CollectRows`/`ClampSpan`/`IsHeaderCell`/`CellBoxesOf`/`FilterEmpty`.
- List: `itemLines`/`itemMarker` stay; structural helpers come from boxwalk.

- [x] **Step 5: Markdown tests pass (including the new one)**

Run: `go test ./pkg/render/markdown ./pkg/render/internal/boxwalk -v`
Expected: PASS, including `TestStrikethroughSemantic`.

- [x] **Step 6: Commit**

```bash
git add pkg/render/internal/boxwalk pkg/render/markdown
git commit -m "refactor: extract shared boxwalk layer from markdown writer; fix <s> SemTag strikethrough"
```

### Task 2: Repoint htmlwrite onto boxwalk

**Files:**
- Modify: `pkg/render/htmlwrite/html.go`, `inline.go`, `list.go`, `table.go`

- [x] **Step 1: Delete htmlwrite's copies, repoint to boxwalk**

Same mechanical substitution as Task 1 Step 4: delete `inlineState`, `styledRun`,
`collectRuns`, `coalesce`, `collapseSpaces`, `rawText`, `hasInlineContent`,
`isBlockContainer`, `collectRows`, `rowIsAllHeader`, `isHeaderCell`, `cellBoxesOf`,
`clampSpan`, `filterEmpty`, `isListContainer`, `withoutNestedLists`,
`stripMarkerPrefix`, `leadingCheckbox`, `isOrderedMarker` from htmlwrite; use
`boxwalk.` equivalents. `renderRun`, `imageMarkup`, `escapeText`, `escapeAttr`,
`markerText`, and all `<tag>` emission stay. `inlineOpt` passes htmlwrite's
`imageMarkup` to `boxwalk.CollectRuns`.

- [x] **Step 2: Tests pass; conversion goldens unchanged**

Run: `go test ./pkg/render/htmlwrite ./pkg/render/markdown ./pkg/doctaculous`
Expected: PASS. htmlwrite output must be byte-identical (it already had the `"s"` case).

- [x] **Step 3: Commit**

```bash
git add pkg/render/htmlwrite
git commit -m "refactor: htmlwrite uses shared boxwalk layer"
```

### Task 3: PR 1 checks and PR

- [x] Run: `gofmt -l . && go vet ./... && golangci-lint run && go test -race ./...` — all clean/PASS. `git status` shows no golden changes.
- [x] Push and open a PR titled "Extract shared writer analysis layer (boxwalk)". Description: 2-3 sentences (dedupe + the `<s>` fix). Per user preference: keep it short, no Claude credit.

---

## PR 2 — branch `refactor/pdf-font-dedupe`

Start: `git checkout main && git checkout -b refactor/pdf-font-dedupe`

### Task 4: One SFNT builder in pkg/font

**Files:**
- Modify: `pkg/font/sfntbuild.go`, `pkg/render/pdfwrite/subset.go`, `pkg/font/sfnt.go`

- [x] **Step 1: Export the builder from pkg/font**

In `pkg/font/sfntbuild.go`, add exported `BuildSFNT(flavor uint32, tables map[string][]byte) []byte` — this is `buildSFNTBytes` from `pkg/render/pdfwrite/subset.go:203-246` moved verbatim (rename `sfntTableChecksum`→ the existing `tableChecksum`). Rewrite the internal slice-based `buildSFNT` as a thin converter (same output: both sort by tag bytes):

```go
// buildSFNT reassembles an sfnt from decoded tables; the common tail both the
// WOFF1 and WOFF2 decoders feed their decoded tables into.
func buildSFNT(flavor uint32, tables []sfntTable) []byte {
	m := make(map[string][]byte, len(tables))
	for _, t := range tables {
		m[string(t.tag[:])] = t.data
	}
	return BuildSFNT(flavor, m)
}
```

Also move `parseSFNTTables` (`subset.go:88-110`) into `pkg/font/sfnt.go` as exported `ParseSFNTTables(data []byte) (map[string][]byte, bool)`, verbatim. Leave `sfntHasTable` unchanged (single-tag scan, no allocation — deliberately not unified).

- [x] **Step 2: Delete the pdfwrite copies**

In `subset.go`: delete `buildSFNTBytes`, `sfntTableChecksum`, `parseSFNTTables`; call `font.BuildSFNT` / `font.ParseSFNTTables` (pdfwrite already imports `pkg/font`).

- [x] **Step 3: Verify byte-identical output**

Run: `go test ./pkg/font ./pkg/render/pdfwrite ./pkg/doctaculous`
Expected: PASS (PDF-writer output is deterministic; its tests would catch any byte drift).

- [x] **Step 4: Commit** — `git commit -am "refactor: single SFNT builder/parser in pkg/font"`

### Task 5: Shared PDF page-resource resolution (pkg/pdf/pageres)

**Files:**
- Create: `pkg/pdf/pageres/pageres.go`
- Modify: `pkg/render/raster/page.go:169-263,401,469-483`, `pkg/render/raster/shading.go:220-227`, `pkg/pdf/extract/collect.go:377-495`

- [x] **Step 1: Create the package**

`pkg/pdf/pageres` (imports `pdf`, `pdf/content`, `font`, `render` — no cycles; `font` already imports `content`). Free functions, so each backend keeps its own resource type (raster's must still carry Image/Shading/etc.):

```go
// Package pageres resolves the page-/Resources entries the raster and extract
// backends share: fonts, form XObjects, and their /Matrix and /BBox.
package pageres

// ResolveFont resolves res["Font"][name] to a GlyphSource via font.New.
// provider may be nil (bundled substitutes only). Failures log and return nil.
func ResolveFont(doc *pdf.Document, res pdf.Dict, name string, provider font.Provider, logPrefix string, logf func(string, ...any)) content.GlyphSource

// ResolveForm resolves res["XObject"][name] to a decoded form XObject: its
// content, its child /Resources dict (falling back to res per the spec), its
// /Matrix, and its /BBox. ok=false if name is not a decodable form.
func ResolveForm(doc *pdf.Document, res pdf.Dict, name string, logPrefix string, logf func(string, ...any)) (data []byte, childRes pdf.Dict, m render.Matrix, bbox *[4]float64, ok bool)

// FormMatrix reads a 6-number /Matrix array (Identity when absent/malformed).
func FormMatrix(doc *pdf.Document, o pdf.Object) render.Matrix

// FormBBox reads a 4-number /BBox array (nil when absent/malformed).
func FormBBox(doc *pdf.Document, o pdf.Object) *[4]float64
```

Bodies: move verbatim from `pkg/render/raster/page.go` (`Font` 183-199, `Form` 206-234 minus the child construction, `formMatrix` 469-483, `formBBox` 240-263), with the log calls becoming `logf(logPrefix+": font %q: %v", ...)` / `logf(logPrefix+": form %q: %v", ...)` and every `logf` call nil-guarded (the extract copy guards; the raster copy must too once shared).

- [x] **Step 2: Repoint raster**

`raster.pageResources.Font` → `return pageres.ResolveFont(r.doc, r.dict, name, r.provider, "raster", r.logf)`.
`Form` → call `pageres.ResolveForm(...)`; on ok, wrap `childRes` in `&pageResources{doc: r.doc, dict: childRes, logf: r.logf, provider: r.provider}` and return. Delete raster's `formMatrix`/`formBBox`; `patternMatrix` (page.go:401) and `initFunctionBased`'s inline `/Matrix` parse (`shading.go:220-227`) both become `pageres.FormMatrix(doc, dict["Matrix"])`.

- [x] **Step 3: Repoint extract**

Same shape in `extract/collect.go` with `nil` provider and `"extract"` prefix; delete its `formMatrix`/`formBBox`. The stub methods (`Image`, `Shading`, …) stay.

- [x] **Step 4: Verify**

Run: `go test ./pkg/render/raster ./pkg/pdf/extract ./pkg/doctaculous`
Expected: PASS, zero golden diffs (`git status` on `testdata`/golden dirs is clean).

- [x] **Step 5: Commit** — `git commit -am "refactor: shared page-resource resolution in pkg/pdf/pageres"`

### Task 6: Single text-rendering-matrix build

**Files:** Modify: `pkg/pdf/content/showtext.go:124-128,161-165`

- [x] **Step 1: Extract the method and use it at both sites**

```go
// renderingMatrix is the text-rendering matrix in force for the next glyph:
// glyph space scaled by font size / horizontal scale / rise, through the text
// matrix, through the CTM. drawGlyph (painting) and emitTextGlyph (extraction
// capture) MUST use the same TRM or captured positions desync from paint.
func (it *Interpreter) renderingMatrix() render.Matrix {
	ts := &it.gs.text
	return render.Matrix{
		A: ts.fontSize * ts.hScale, B: 0,
		C: 0, D: ts.fontSize,
		E: 0, F: ts.rise,
	}.Mul(ts.matrix).Mul(it.gs.ctm)
}
```

Replace both inline builds with `trm := it.renderingMatrix()`.

- [x] **Step 2: Verify** — `go test ./pkg/pdf/content ./pkg/render/raster ./pkg/pdf/extract` → PASS.
- [x] **Step 3: Commit** — `git commit -am "refactor: single text-rendering-matrix builder"`

### Task 7: Shared device color math in pkg/render

**Files:**
- Create: `pkg/render/color.go`
- Modify: `pkg/pdf/content/colorspace.go:23-81`, `pkg/render/raster/image.go:235-264`, `pkg/render/raster/blend.go:205-222`

- [x] **Step 1: Add the shared helpers**

`pkg/render/color.go` (naive device conversions — the single place a future colorimetry fix lands):

```go
// Clamp8 maps a component in [0,1] to an 8-bit value, clamping out-of-range.
func Clamp8(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	default:
		return uint8(v*255 + 0.5)
	}
}

// GrayToRGBA converts a DeviceGray component to RGBA.
func GrayToRGBA(g float64) color.RGBA {
	v := Clamp8(g)
	return color.RGBA{v, v, v, 0xFF}
}

// RGBToRGBA converts DeviceRGB components to RGBA.
func RGBToRGBA(r, g, b float64) color.RGBA {
	return color.RGBA{Clamp8(r), Clamp8(g), Clamp8(b), 0xFF}
}

// CMYKToRGBA converts DeviceCMYK components to RGBA with the naive
// (1-c)(1-k) device conversion (no ICC).
func CMYKToRGBA(c, m, y, k float64) color.RGBA {
	return color.RGBA{Clamp8((1 - c) * (1 - k)), Clamp8((1 - m) * (1 - k)), Clamp8((1 - y) * (1 - k)), 0xFF}
}
```

(Confirm exact rounding/edge behavior against the existing `clamp8`/`clamp8f`/`to8` bodies before deleting them — they were verified byte-identical in the audit.)

- [x] **Step 2: Repoint the three copies**

- `content/colorspace.go`: `grayToRGBA`/`cmykToRGBA`/`clamp8` bodies delegate to (or are deleted in favor of) `render.GrayToRGBA`/`render.CMYKToRGBA`/`render.Clamp8`; `colorFromComponents` keeps its switch, arms call the shared funcs.
- `raster/image.go`: `componentsToRGBA` arms call the shared funcs; delete `clamp8f`.
- `raster/blend.go`: delete `to8`; the `compositeBlend` tail uses `render.Clamp8`.

- [x] **Step 3: Verify (hot path — goldens are the check)**

Run: `go test ./pkg/pdf/content ./pkg/render/... ./pkg/doctaculous` → PASS, zero golden diffs.

- [x] **Step 4: Commit** — `git commit -am "refactor: shared device color conversion in pkg/render"`

### Task 8: PR 2 checks and PR

- [x] `gofmt -l . && go vet ./... && golangci-lint run && go test -race ./...` all clean; no golden changes.
- [x] Push; open PR "Dedupe drift-prone PDF/font pairs". Short description, no Claude credit.

---

## PR 3 — branch `refactor/mechanical-dedupe`

Start: `git checkout main && git checkout -b refactor/mechanical-dedupe`

### Task 9: Merge flexfix.go/gridfix.go

**Files:**
- Create: `pkg/layout/css/itemfix.go`
- Delete: `pkg/layout/css/flexfix.go`, `pkg/layout/css/gridfix.go`
- Modify: `pkg/layout/css/build.go` (the `fixupFlex`/`fixupGrid` call sites — grep for them)

- [x] **Step 1: Write the merged fixup**

`itemfix.go`: one walker replacing both files. `containerItems` is `flexItems`
(`flexfix.go:23-63`) moved verbatim with `cssbox.BoxAnonFlexItem` replaced by the
`anonKind` parameter (keep its comments; drop the flex-vs-grid spec-section wording
in favor of "CSS Flexbox §4 / Grid §6"):

```go
// fixupFlexGrid walks the box tree and repairs every flex and grid container's
// children into proper items (CSS Flexbox §4 / Grid §6): contiguous runs of
// inline-level content become one anonymous block-level item; whitespace-only
// text between block-level items is dropped. Called from Build after fixupTables.
func fixupFlexGrid(b *cssbox.Box) {
	for _, c := range b.Children {
		fixupFlexGrid(c)
	}
	switch b.Display {
	case cssbox.DisplayFlex, cssbox.DisplayInlineFlex:
		b.Children = containerItems(b.Children, cssbox.BoxAnonFlexItem)
	case cssbox.DisplayGrid, cssbox.DisplayInlineGrid:
		b.Children = containerItems(b.Children, cssbox.BoxAnonGridItem)
	}
}

// containerItems converts a flex/grid container's raw children into items ...
func containerItems(kids []*cssbox.Box, anonKind cssbox.BoxKind) []*cssbox.Box {
	// body of flexItems, verbatim, with anonKind for the item Kind
}
```

Replace the two `Build` calls (`fixupFlex(root)`, `fixupGrid(root)`) with one `fixupFlexGrid(root)`.

- [x] **Step 2: Verify** — `go test ./pkg/layout/... ./pkg/doctaculous` → PASS, zero golden diffs.
- [x] **Step 3: Commit** — `git commit -am "refactor: merge flexfix/gridfix into one item fixup"`

### Task 10: shiftFragment delegates to translateFragment

**Files:** Modify: `pkg/layout/css/block.go:1309-1331`

- [x] **Step 1:** Replace `shiftFragment`'s body (keep the function — it has ~10 call sites):

```go
// shiftFragment translates one fragment and its descendants by dy. It is
// translateFragment restricted to the block-flow case: block children were
// positioned in page-space X already, so only Y moves.
func shiftFragment(f *Fragment, dy float64) { translateFragment(f, 0, dy) }
```

(`translateFragment` at `inline.go:628` is a strict superset — its dx work is a no-op at dx=0, and its `dx==0 && dy==0` early return is behavior-neutral. `shiftFragmentSelf` is deliberately different; leave it.)

- [x] **Step 2: Verify** — `go test ./pkg/layout/... ./pkg/doctaculous` → PASS, zero golden diffs.
- [x] **Step 3: Commit** — `git commit -am "refactor: shiftFragment delegates to translateFragment"`

### Task 11: Builtin min/max replace hand-rolled helpers

**Files:** Modify: `pkg/layout/css/control.go:150-166,234-239`, `pkg/layout/css/replaced.go:113-115,140-145`, `pkg/layout/css/grid.go:68,93,97-98,537`, `pkg/render/pdfwrite/page.go:306,492`

- [x] **Step 1:** Delete `max2`, `max0`, `maxi`, `maxFloat`; replace call sites: `max2(a,b)`→`max(a,b)`, `max0(v)`→`max(0, v)`, `maxi(0, n-1)`→`max(0, n-1)`, `maxFloat(total, contentH)`→`max(total, contentH)`. (Go 1.26 builtins; keep `clampF`/`clampMaxMin` — they carry real logic.)
- [x] **Step 2: Verify** — `go vet ./... && go test ./pkg/layout/... ./pkg/render/pdfwrite` → PASS.
- [x] **Step 3: Commit** — `git commit -am "refactor: use builtin min/max"`

### Task 12: pkg/css batch

**Files:** Modify: `pkg/css/cascade.go:47-48,1148-1153`, `pkg/css/fontface.go:153-160`, `pkg/css/stringset.go:95-100`, `pkg/css/shorthand.go:338-379` (+ the three `applyDeclaration` dispatch cases), `pkg/css/pagesize.go:75-78,110-131`

- [x] **Step 1: One unquote helper.** Keep `unquote` (fontface.go) but give it the tighter matching-pair body (identical semantics — all three verified equivalent in the audit); delete `trimQuotes` (cascade.go:1148) and `unquoteString` (stringset.go:95); repoint all call sites (13 total — `grep -rn 'trimQuotes(\|unquoteString(' pkg/css`).

```go
// unquote strips one matching pair of surrounding single or double quotes.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
```

- [x] **Step 2: One place-* applier.** Replace `applyPlaceItems`/`applyPlaceContent`/`applyPlaceSelf` (shorthand.go:338-379) with:

```go
// applyPlacePair expands a `place-*: <align> [<justify>]` shorthand into its two
// longhands. One value sets both; two values set align then justify.
func applyPlacePair(cs *ComputedStyle, val, alignProp, justifyProp string) {
	fields := splitComponents(val)
	if len(fields) == 0 || len(fields) > 2 {
		return
	}
	applyDeclaration(cs, Declaration{Property: alignProp, Value: fields[0]})
	j := fields[0]
	if len(fields) == 2 {
		j = fields[1]
	}
	applyDeclaration(cs, Declaration{Property: justifyProp, Value: j})
}
```

Dispatch cases become `applyPlacePair(cs, d.Value, "align-items", "justify-items")` (and content/self equivalents — match the variable names actually used at the dispatch site).

- [x] **Step 3: Page-margin shorthand reuses expandBox** (pagesize.go:110-131):

```go
func parsePageMarginShorthand(value string) (top, right, bottom, left float64, ok bool) {
	t, r, b, l, ok := expandBox(strings.Fields(strings.TrimSpace(value)))
	if !ok {
		return 0, 0, 0, 0, false
	}
	vt, okT := parseAbsLengthPx(t)
	vr, okR := parseAbsLengthPx(r)
	vb, okB := parseAbsLengthPx(b)
	vl, okL := parseAbsLengthPx(l)
	if !okT || !okR || !okB || !okL {
		return 0, 0, 0, 0, false
	}
	return vt, vr, vb, vl, true
}
```

(Keep the original doc comment. Same accept/reject set: empty and 5+ fields fail via expandBox; any non-absolute length fails the whole value.)

- [x] **Step 4: Dead store** (pagesize.go:75-78): `if _, isKeyword := pageSizeKeywords[f]; isKeyword { keyword = f; continue }` — drop the `dims`/`_ = dims` dance.

- [x] **Step 5: Fix the stale inherited-set comment** (cascade.go:47-48). Replace the two-line enumeration with:

```go
// Inherited properties (CSS) carry over from the parent in inheritFrom, which is
// the single source of truth for which fields inherit.
```

- [x] **Step 6: Verify** — `go test ./pkg/css ./pkg/layout/... ./pkg/doctaculous` → PASS, zero golden diffs.
- [x] **Step 7: Commit** — `git commit -am "refactor: pkg/css dedupe batch (unquote, place-*, page margin, stale comment)"`

### Task 13: One CLI reorderArgs

**Files:**
- Create: `cmd/doctaculous/args.go`
- Modify: `cmd/doctaculous/rasterize.go:270-303`, `cmd/doctaculous/topdf.go:113-138`, `cmd/doctaculous/tomd.go:86-107`, plus the callers (`grep -n 'reorderArgs(\|reorderTopdfArgs(\|reorderTomdArgs(' cmd/doctaculous/*.go` — includes `tohtml.go:30`)

- [x] **Step 1:** `args.go` gets the shared function (move rasterize.go's doc comment, generalized):

```go
// reorderArgs moves non-flag arguments after flags so positional inputs may
// appear before flags (flag.Parse stops at the first non-flag token). valueFlags
// names the flags that take their value as a separate token ("--flag value");
// the "--flag=value" form is always safe.
func reorderArgs(args []string, valueFlags map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ { //nolint:intrange // index i is mutated inside the loop
		a := args[i]
		if len(a) > 0 && a[0] == '-' {
			flags = append(flags, a)
			if valueFlags[a] && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	return append(flags, positional...)
}
```

Delete the three per-command functions; each caller passes its own `valueFlags` map (moved from the deleted function into a package-level `var rasterizeValueFlags = map[string]bool{...}` / `topdfValueFlags` / `tomdValueFlags` next to each command, contents unchanged; `tohtml.go` keeps sharing tomd's).

- [x] **Step 2: Verify** — `go build ./cmd/... && go test ./cmd/...` → PASS.
- [x] **Step 3: Commit** — `git commit -am "refactor: single CLI arg reorder helper"`

### Task 14: PDF small fixes

**Files:** Modify: `pkg/pdf/rebuild.go:78-129` (+ its `rebuildXref` callers), `pkg/pdf/extract/tables.go:277,373-376`, `pkg/pdf/content/xobject.go:36-51`

- [x] **Step 1: Merge the backward header scan.** Change `readObjHeaderBackward` to also return the header start (it already computes `numStart`):

```go
// readObjHeaderBackward reads "N G" immediately before the " obj" at objPos,
// also returning the byte offset where the object number starts.
func readObjHeaderBackward(data []byte, objPos int) (num, gen, start int, ok bool) {
	// ... existing body; return num, gen, numStart, true
}
```

Delete `objHeaderStart`; update the `rebuildXref` call sites (grep `objHeaderStart(`) to use the returned `start`.

- [x] **Step 2: Inline `columnIndex`** (tables.go:373-376): replace the single call at tables.go:277 with `sort.SearchFloat64s(bounds, cx)` (verify the wrapper body is exactly that before inlining) and delete the function + its misleading comment.

- [x] **Step 3: tintTransform reuses colorSpaceByName** (xobject.go:42-46) — the device-name list is a copy of `colorSpaceByName`'s:

```go
func (it *Interpreter) tintTransform(operands []pdf.Object) *TintTransform {
	name := nameOperand(operands)
	if name == "" || it.res == nil {
		return nil
	}
	if it.colorSpaceByName(operands) != csOther {
		return nil // device/pattern spaces carry no tint transform
	}
	if t, ok := it.res.ColorSpace(name); ok {
		return t
	}
	return nil
}
```

- [x] **Step 4: Verify** — `go test ./pkg/pdf/... ./pkg/render/raster` → PASS.
- [x] **Step 5: Commit** — `git commit -am "refactor: pdf small dedupes (header scan, columnIndex, tint device names)"`

### Task 15: PR 3 checks and PR

- [x] `gofmt -l . && go vet ./... && golangci-lint run && go test -race ./...` all clean; no golden changes.
- [x] Push; open PR "Mechanical dedupe batch". Short description, no Claude credit.

---

## Explicitly out of scope (audited, deliberately kept as-is)

The CSS top-level splitters, `parseBackgroundPosition` vs `parseObjectPosition`, the `pdf/function` nil-doc dict helpers, the two worker pools, the four path walkers, the four MSB bit readers, the DOCX `Has*` merge/project lists, `edges` accessor methods, and splitting the long layout dispatch functions. Do not "improve" these while executing the tasks above.
