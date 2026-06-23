# HTML Frontend + Box Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn HTML + CSS bytes into a correct, well-formed recursive `cssbox` tree (no pixels), verified by structural assertions.

**Architecture:** A new HTML frontend (`pkg/html`) wraps `golang.org/x/net/html` into an owned DOM that implements the existing `pkg/css` `Node` interface and collects `<style>`/`<link>`/`style=""`. A new neutral box model (`pkg/layout/cssbox`) is the long-lived layout contract. Box generation (`pkg/layout/css`) drives the `pkg/css` cascade in a single recursive descent, storing the `ComputedStyle` on each box, and normalizes the tree with anonymous-box fixups. A `pkg/resource` seam supplies stylesheets for `<link>` (hermetic loaders only — no HTTP). Two small additive changes to `pkg/css` (root handling + origin-aware cascade) support all of this.

**Tech Stack:** Go 1.26, `golang.org/x/net/html` (BSD, new dep), the existing `pkg/css` package.

**Design source of truth:** `docs/superpowers/specs/2026-06-23-html-box-generation-design.md`.

---

## Conventions for every task

- **Build cache / network sandbox:** `go test`, `go build`, `go vet`, and `go get` may fail under the
  command sandbox with `operation not permitted` on `~/Library/Caches/go-build`, or TLS errors to
  the Go proxy. When that happens, rerun the **same** command with the sandbox disabled. This is a
  known environment quirk from sub-project 1, not a code problem.
- **`golangci-lint run` from the repo root** reports pre-existing typecheck errors in the untracked
  `agent/skills/.../examples/` stray directory. Those are not part of the module — ignore them. Lint
  the specific packages you touch (e.g. `golangci-lint run ./pkg/html/...`) to avoid the noise.
- **Module path** is `github.com/nathanstitt/doctaculous`. All imports use that prefix.
- **Commit after every task** (the final step of each task). Use the message shown.
- **TDD throughout:** write the failing test, watch it fail, implement minimally, watch it pass.

---

## File structure (what gets created/modified)

**New packages**
- `pkg/resource/loader.go` — `ResourceLoader` interface, `Resource`, `ErrNotFound`, `MapLoader`, `DirLoader`.
- `pkg/resource/loader_test.go` — loader round-trips, not-found, ctx cancellation.
- `pkg/layout/cssbox/box.go` — `Box`, `BoxKind`, `DisplayKind`, `FormattingContext`, `FloatKind`, `PositionKind`, `ReplacedContent`, predicates.
- `pkg/layout/cssbox/box_test.go` — type-level invariants / predicates.
- `pkg/html/dom.go` — owned DOM node types (`Element`, `Text`, `DOMNode`); `*Element` implements `css.Node`.
- `pkg/html/dom_test.go` — `css.Node` conformance, accessors.
- `pkg/html/html.go` — `Parse`, the `x/net/html` walk, `<style>`/`<link>`/`style=""` collection, `Document`.
- `pkg/html/html_test.go` — parse + collection + malformed-input tests.
- `pkg/html/ua.go` — `UAStylesheet` (the minimal UA default stylesheet).
- `pkg/html/ua_test.go` — UA sheet parses and yields expected display defaults.
- `pkg/layout/css/build.go` — `Build`, the recursive descent driving the cascade, `<link>` resolution.
- `pkg/layout/css/anon.go` — anonymous-box normalization (inline-in-block, block-in-inline, whitespace).
- `pkg/layout/css/build_test.go` — box-tree structural assertions (the substantive suite).
- `pkg/layout/css/anon_test.go` — anonymous-box fixup assertions.

**Modified**
- `pkg/css/cascade.go` — add `Origin`, `OriginSheet`; change `NewResolver` to the origin-aware form; add `ComputeRoot`; thread origin through the cascade sort.
- `pkg/css/cascade_test.go` and `pkg/css/integration_test.go` — update to the new `NewResolver` signature; add origin-ordering and `ComputeRoot` tests.
- `go.mod` / `go.sum` — add `golang.org/x/net`.
- `CLAUDE.md` — architecture note + "Done" roadmap entry (final task).

---

## Task 1: Add the `golang.org/x/net/html` dependency

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `pkg/html/probe_test.go` (temporary smoke test, deleted in this task's last step)

- [ ] **Step 1: Add the dependency**

The module is already in the cache at v0.43.0 (matches the existing `x/image`/`x/text` line). Add it:

Run: `go get golang.org/x/net@v0.43.0`
Expected: `go.mod` gains `golang.org/x/net v0.43.0`; `go.sum` updated. (If the sandbox blocks the proxy, rerun with the sandbox disabled.)

- [ ] **Step 2: Write a temporary smoke test proving the dep is importable and parses**

Create `pkg/html/probe_test.go`:

```go
package html

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestXNetHTMLImportable(t *testing.T) {
	doc, err := xhtml.Parse(strings.NewReader("<p>hi</p>"))
	if err != nil {
		t.Fatalf("xhtml.Parse: %v", err)
	}
	if doc == nil || doc.Type != xhtml.DocumentNode {
		t.Fatalf("expected a document node, got %+v", doc)
	}
}
```

- [ ] **Step 3: Run the smoke test**

Run: `go test ./pkg/html/ -run TestXNetHTMLImportable -v`
Expected: PASS (proves the dep resolves and parses). Rerun with the sandbox disabled if the build cache is blocked.

- [ ] **Step 4: Delete the temporary smoke test**

Run: `rm pkg/html/probe_test.go`
(The real tests in later tasks supersede it; we keep the dep change.)

- [ ] **Step 5: Verify the module is tidy**

Run: `go mod tidy` then `go build ./...`
Expected: no errors; `golang.org/x/net` remains a direct require. Rerun with the sandbox disabled if needed.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum
git commit -m "Add golang.org/x/net dependency for the HTML frontend"
```

---

## Task 2: `pkg/css` — origin-aware cascade + root handling

This is the additive `pkg/css` change. The only non-test caller of `css.NewResolver` is `pkg/css`'s own tests (verified: `pkg/docx/style.NewResolver` is a *different* function and is unaffected).

**Files:**
- Modify: `pkg/css/cascade.go`
- Modify: `pkg/css/cascade_test.go`, `pkg/css/integration_test.go`

- [ ] **Step 1: Write failing tests for origin ordering and `ComputeRoot`**

Add to `pkg/css/cascade_test.go`:

```go
func TestOriginUALosesToAuthorAcrossSpecificity(t *testing.T) {
	// UA: a type selector sets display:inline (high-ish specificity for UA).
	ua := Parse(`div { display: block; color: red; }`)
	// Author: a *less* specific path still wins over UA because author origin
	// outranks UA origin regardless of specificity.
	author := Parse(`div { color: green; }`)
	r := NewResolver([]OriginSheet{
		{Sheet: ua, Origin: OriginUA},
		{Sheet: author, Origin: OriginAuthor},
	}, nil)
	cs := r.ComputeRoot(&fakeNode{tag: "div"})
	if cs.Display != "block" {
		t.Errorf("display = %q, want block (from UA, no author override)", cs.Display)
	}
	if (cs.Color != color.RGBA{0, 128, 0, 255}) {
		t.Errorf("color = %v, want green (author beats UA)", cs.Color)
	}
}

func TestUAImportantBeatsAuthorNormal(t *testing.T) {
	ua := Parse(`p { color: red !important; }`)
	author := Parse(`p { color: green; }`)
	r := NewResolver([]OriginSheet{
		{Sheet: ua, Origin: OriginUA},
		{Sheet: author, Origin: OriginAuthor},
	}, nil)
	cs := r.ComputeRoot(&fakeNode{tag: "p"})
	if (cs.Color != color.RGBA{255, 0, 0, 255}) {
		t.Errorf("color = %v, want red (UA !important beats author normal)", cs.Color)
	}
}

func TestComputeRootUsesInitialBase(t *testing.T) {
	r := NewResolver([]OriginSheet{{Sheet: Parse(``), Origin: OriginAuthor}}, nil)
	cs := r.ComputeRoot(&fakeNode{tag: "html"})
	if cs.FontSizePt != 16 || cs.Color != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("root base = {size %v color %v}, want initial {16 black}", cs.FontSizePt, cs.Color)
	}
}
```

- [ ] **Step 2: Run the new tests to verify they fail to compile**

Run: `go test ./pkg/css/ -run 'TestOrigin|TestUAImportant|TestComputeRoot' -v`
Expected: FAIL — build error: `OriginSheet`, `OriginUA`, `OriginAuthor`, `ComputeRoot` undefined.

- [ ] **Step 3: Add the origin types and new constructor in `cascade.go`**

In `pkg/css/cascade.go`, add near the top (after the `inlineImportantIDs` const):

```go
// Origin is a cascade origin. CSS orders declarations by origin first:
// UA-normal < author-normal < author-important < UA-important. Origin is the
// outermost cascade key, dominating specificity and source order.
type Origin int

const (
	// OriginUA is the user-agent default stylesheet.
	OriginUA Origin = iota
	// OriginAuthor is page-supplied CSS: <style>, <link>, and style="".
	OriginAuthor
)

// OriginSheet pairs a parsed stylesheet with its cascade origin.
type OriginSheet struct {
	Sheet  Stylesheet
	Origin Origin
}
```

Replace the `Resolver` struct's `sheet Stylesheet` field and `NewResolver`:

```go
// Resolver computes the ComputedStyle of any node against parsed stylesheets
// tagged by origin. Build one with NewResolver; it is read-only after
// construction and safe for concurrent use. logf may be nil.
type Resolver struct {
	sheets []OriginSheet
	logf   func(string, ...any)
}

// NewResolver builds a Resolver over origin-tagged stylesheets. Sheets may be
// given in any order; the cascade applies origin/specificity/source-order rules.
func NewResolver(sheets []OriginSheet, logf func(string, ...any)) *Resolver {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Resolver{sheets: sheets, logf: logf}
}
```

- [ ] **Step 4: Add `ComputeRoot` and thread origin through `Compute`'s cascade**

In `pkg/css/cascade.go`, add `ComputeRoot` (keeps `initialStyle()` unexported):

```go
// ComputeRoot returns the ComputedStyle of a root element (one with no parent),
// using the CSS initial values as the inheritance base. Box generation calls
// this for the document root, then threads each result down to children via
// Compute, so callers never need the CSS initial values themselves.
func (r *Resolver) ComputeRoot(n Node) ComputedStyle {
	return r.Compute(n, initialStyle())
}
```

Rewrite the body of `Compute` so the `matched` record carries origin, the rule loop ranges over all
sheets, and `less` compares origin first. Replace the existing `Compute` body's collection + sort
with:

```go
func (r *Resolver) Compute(n Node, parentStyle ComputedStyle) ComputedStyle {
	cs := inheritFrom(parentStyle)

	type matched struct {
		decl   Declaration
		origin Origin
		spec   Specificity
		order  int
	}
	var normal, important []matched

	order := 0
	for si := range r.sheets {
		origin := r.sheets[si].Origin
		sheet := &r.sheets[si].Sheet
		for ri := range sheet.Rules {
			rule := &sheet.Rules[ri]
			spec, ok := bestMatch(rule.Selectors, n)
			if !ok {
				continue
			}
			for _, d := range rule.Declarations {
				m := matched{decl: d, origin: origin, spec: spec, order: order}
				if d.Important {
					important = append(important, m)
				} else {
					normal = append(normal, m)
				}
				order++
			}
		}
	}

	// normalRank/importantRank place each origin on the unified cascade ladder so
	// the same `less` works for both passes:
	//   UA-normal(0) < author-normal(1) < author-important(2) < UA-important(3)
	normalRank := func(o Origin) int {
		if o == OriginAuthor {
			return 1
		}
		return 0 // UA
	}
	importantRank := func(o Origin) int {
		if o == OriginUA {
			return 3
		}
		return 2 // author
	}

	lessBy := func(rank func(Origin) int) func(a, b matched) bool {
		return func(a, b matched) bool {
			ra, rb := rank(a.origin), rank(b.origin)
			if ra != rb {
				return ra < rb
			}
			if a.spec.Less(b.spec) {
				return true
			}
			if b.spec.Less(a.spec) {
				return false
			}
			return a.order < b.order
		}
	}

	// 1. normal declarations, lowest to highest.
	sort.SliceStable(normal, func(i, j int) bool { return lessBy(normalRank)(normal[i], normal[j]) })
	for _, m := range normal {
		applyDeclaration(&cs, m.decl)
	}

	// 2. inline style="" (author origin). Normal inline declarations overlay all
	//    normal rules; inline !important joins the important set with an outsized
	//    specificity and author origin.
	if styleAttr, ok := n.Attr("style"); ok {
		for _, d := range parseDeclarations(styleAttr) {
			if d.Important {
				important = append(important, matched{
					decl: d, origin: OriginAuthor,
					spec: Specificity{IDs: inlineImportantIDs}, order: order,
				})
				order++
				continue
			}
			applyDeclaration(&cs, d)
		}
	}

	// 3. important declarations overlay last.
	sort.SliceStable(important, func(i, j int) bool { return lessBy(importantRank)(important[i], important[j]) })
	for _, m := range important {
		applyDeclaration(&cs, m.decl)
	}
	return cs
}
```

Update the doc comment on `Compute` to mention origin and to point root callers at `ComputeRoot`.

- [ ] **Step 5: Update existing `pkg/css` tests to the new signature**

In `pkg/css/cascade_test.go` and `pkg/css/integration_test.go`, every existing
`NewResolver(sheet, nil)` becomes the origin form, and root `Compute(n, initialStyle())` calls become
`ComputeRoot(n)` where the node is a root. Mechanically:

- `r := NewResolver(sheet, nil)` → `r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)`
- `r := NewResolver(Parse(src), nil)` → `r := NewResolver([]OriginSheet{{Sheet: Parse(src), Origin: OriginAuthor}}, nil)`
- `r.Compute(root, initialStyle())` → `r.ComputeRoot(root)` (for nodes with no parent)
- Leave `r.Compute(child, parentCS)` calls (non-root) unchanged.

The `initialStyle()` calls that build a bare base for direct field assertions (e.g.
`cs := initialStyle()` at the top of a test) stay as-is — `initialStyle()` is still in-package.

- [ ] **Step 6: Run the whole `pkg/css` suite**

Run: `go test ./pkg/css/ -v`
Expected: PASS, including the three new origin/root tests. Rerun with the sandbox disabled if the build cache is blocked.

- [ ] **Step 7: Vet + lint the package**

Run: `go vet ./pkg/css/ && golangci-lint run ./pkg/css/...`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add pkg/css/cascade.go pkg/css/cascade_test.go pkg/css/integration_test.go
git commit -m "css: origin-aware cascade (UA below author) + ComputeRoot for the root base"
```

---

## Task 3: `pkg/resource` — the loader seam

**Files:**
- Create: `pkg/resource/loader.go`, `pkg/resource/loader_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/resource/loader_test.go`:

```go
package resource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMapLoaderLoadsRegistered(t *testing.T) {
	l := MapLoader{"theme.css": {Data: []byte("p{color:red}"), ContentType: "text/css"}}
	data, ct, err := l.Load(context.Background(), "theme.css")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "p{color:red}" || ct != "text/css" {
		t.Errorf("got (%q,%q)", data, ct)
	}
}

func TestMapLoaderNotFound(t *testing.T) {
	l := MapLoader{}
	_, _, err := l.Load(context.Background(), "missing.css")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestLoaderHonorsCancellation(t *testing.T) {
	l := MapLoader{"a.css": {Data: []byte("x"), ContentType: "text/css"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := l.Load(ctx, "a.css"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDirLoaderServesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.css"), []byte("a{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := DirLoader{Base: dir}
	data, ct, err := l.Load(context.Background(), "s.css")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "a{}" || ct != "text/css" {
		t.Errorf("got (%q,%q)", data, ct)
	}
}

func TestDirLoaderMissingIsNotFound(t *testing.T) {
	l := DirLoader{Base: t.TempDir()}
	if _, _, err := l.Load(context.Background(), "nope.css"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/resource/ -v`
Expected: FAIL — package/identifiers undefined.

- [ ] **Step 3: Implement the loader**

Create `pkg/resource/loader.go`:

```go
// Package resource defines the seam by which a document's external references
// (stylesheets via <link>, images and fonts later) are resolved to bytes. The
// library will ship an HTTP-backed loader for the public URL path in a later
// sub-project; this package currently provides only hermetic loaders so no layer
// below the public API touches the network and all tests stay offline.
package resource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotFound is returned (wrapped) when a loader cannot find a ref, so callers
// can distinguish "absent" from "broken" and degrade gracefully.
var ErrNotFound = errors.New("resource not found")

// ResourceLoader resolves a ref (a URL or path string) to its bytes and content
// type. Implementations must honor ctx cancellation.
type ResourceLoader interface {
	Load(ctx context.Context, ref string) (data []byte, contentType string, err error)
}

// Resource is one entry in a MapLoader.
type Resource struct {
	Data        []byte
	ContentType string
}

// MapLoader is an in-memory ResourceLoader keyed by exact ref string. It is the
// primary hermetic loader for tests.
type MapLoader map[string]Resource

// Load implements ResourceLoader.
func (m MapLoader) Load(ctx context.Context, ref string) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	r, ok := m[ref]
	if !ok {
		return nil, "", fmt.Errorf("%q: %w", ref, ErrNotFound)
	}
	return r.Data, r.ContentType, nil
}

// DirLoader is a ResourceLoader that serves files from a base directory, with
// content type inferred from the file extension. For fixtures kept on disk.
type DirLoader struct {
	Base string
}

// Load implements ResourceLoader.
func (d DirLoader) Load(ctx context.Context, ref string) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(filepath.Join(d.Base, ref))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("%q: %w", ref, ErrNotFound)
		}
		return nil, "", fmt.Errorf("read %q: %w", ref, err)
	}
	return data, contentTypeByExt(ref), nil
}

// contentTypeByExt returns a minimal content type from a ref's extension; "" if
// unknown (callers that care can sniff, but this sub-project only needs CSS).
func contentTypeByExt(ref string) string {
	switch strings.ToLower(filepath.Ext(ref)) {
	case ".css":
		return "text/css"
	case ".html", ".htm":
		return "text/html"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./pkg/resource/ -v`
Expected: PASS (all five tests).

- [ ] **Step 5: Vet + lint**

Run: `go vet ./pkg/resource/ && golangci-lint run ./pkg/resource/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add pkg/resource/
git commit -m "resource: ResourceLoader seam with hermetic MapLoader/DirLoader"
```

---

## Task 4: `pkg/layout/cssbox` — the box tree type

**Files:**
- Create: `pkg/layout/cssbox/box.go`, `pkg/layout/cssbox/box_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/layout/cssbox/box_test.go`:

```go
package cssbox

import "testing"

func TestBoxKindPredicates(t *testing.T) {
	cases := []struct {
		k                  BoxKind
		blockLvl, inlineLvl bool
	}{
		{BoxBlock, true, false},
		{BoxAnonBlock, true, false},
		{BoxInline, false, true},
		{BoxAnonInline, false, true},
		{BoxText, false, true},
		{BoxReplaced, false, true}, // a bare <img> is inline-level by default
	}
	for _, c := range cases {
		if got := c.k.IsBlockLevel(); got != c.blockLvl {
			t.Errorf("%v.IsBlockLevel() = %v, want %v", c.k, got, c.blockLvl)
		}
		if got := c.k.IsInlineLevel(); got != c.inlineLvl {
			t.Errorf("%v.IsInlineLevel() = %v, want %v", c.k, got, c.inlineLvl)
		}
	}
}

func TestLeafBoxesHaveNoChildren(t *testing.T) {
	// Documents the contract that text/replaced boxes are leaves.
	for _, k := range []BoxKind{BoxText, BoxReplaced} {
		b := &Box{Kind: k}
		if len(b.Children) != 0 {
			t.Errorf("%v leaf unexpectedly has children", k)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/layout/cssbox/ -v`
Expected: FAIL — identifiers undefined.

- [ ] **Step 3: Implement the box type**

Create `pkg/layout/cssbox/box.go`:

```go
// Package cssbox defines the recursive, format-neutral box tree that the CSS
// layout engine consumes. It is the long-lived contract between frontends and
// the engine: box generation (pkg/layout/css) produces it from HTML+CSS today,
// and DOCX lowering converges onto it later. Everything needed for layout is
// resolved onto the box (the computed style and structural intent); the box has
// no back-reference to its source document, so it is independent of any
// frontend. A built tree is read-only and may be shared across the render
// fan-out without locks.
package cssbox

import "github.com/nathanstitt/doctaculous/pkg/css"

// BoxKind is the structural role of a box.
type BoxKind int

const (
	// BoxBlock is a block-level box generated by an element (display:block,
	// list-item, table, flex, grid, ...).
	BoxBlock BoxKind = iota
	// BoxInline is an inline-level box generated by an element (display:inline).
	BoxInline
	// BoxAnonBlock is an anonymous block box wrapping inline-level children of a
	// block container that also has block-level children.
	BoxAnonBlock
	// BoxAnonInline is an anonymous inline box produced when an inline box is
	// split around a block-level descendant (block-in-inline).
	BoxAnonInline
	// BoxReplaced is a replaced element (e.g. <img>): a leaf sized by intrinsics
	// in a later sub-project.
	BoxReplaced
	// BoxText is a run of text: a leaf.
	BoxText
)

// IsBlockLevel reports whether the kind participates in a block formatting
// context as a block-level box.
func (k BoxKind) IsBlockLevel() bool {
	return k == BoxBlock || k == BoxAnonBlock
}

// IsInlineLevel reports whether the kind participates in an inline formatting
// context as an inline-level box.
func (k BoxKind) IsInlineLevel() bool {
	return k == BoxInline || k == BoxAnonInline || k == BoxText || k == BoxReplaced
}

// DisplayKind is the normalized display of a box, preserved even for layout
// modes not yet implemented (flex/grid/table parts), so later sub-projects add
// their layout algorithm without changing box generation.
type DisplayKind int

const (
	DisplayBlock DisplayKind = iota
	DisplayInline
	DisplayInlineBlock
	DisplayListItem
	DisplayTable
	DisplayTableRow
	DisplayTableCell
	DisplayFlex
	DisplayGrid
	// DisplayNone is never emitted as a box (display:none subtrees are pruned);
	// it exists so a DisplayKind can round-trip the value if needed.
	DisplayNone
)

// FormattingContext is the context a box establishes for its children.
type FormattingContext int

const (
	// BlockFC: children are block-level (the default for a block box with
	// block-level children).
	BlockFC FormattingContext = iota
	// InlineFC: children are inline-level (a block box whose children are all
	// inline-level establishes this).
	InlineFC
	// TableFC, FlexFC, GridFC are established by table/flex/grid boxes; their
	// layout arrives in later sub-projects, but the intent is recorded now.
	TableFC
	FlexFC
	GridFC
)

// FloatKind is a box's float value.
type FloatKind int

const (
	FloatNone FloatKind = iota
	FloatLeft
	FloatRight
)

// PositionKind is a box's positioning scheme.
type PositionKind int

const (
	PosStatic PositionKind = iota
	PosRelative
	PosAbsolute
	PosFixed
)

// ReplacedContent holds the facts about a replaced element. In this sub-project
// it carries only the source facts (no decoded image); a later sub-project adds
// the decoded image and intrinsic size.
type ReplacedContent struct {
	Tag   string            // e.g. "img"
	Attrs map[string]string // src, width, height, alt, ...
}

// Box is a node of the recursive box tree. It is read-only after construction.
type Box struct {
	Kind     BoxKind
	Style    css.ComputedStyle // resolved style; inherited/zero for anonymous boxes
	Children []*Box

	// Text is set only for Kind == BoxText.
	Text string
	// Replaced is set only for Kind == BoxReplaced.
	Replaced *ReplacedContent

	// Layout-intent hints derived from Style at generation time:
	Display    DisplayKind
	Formatting FormattingContext
	Float      FloatKind
	Position   PositionKind
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./pkg/layout/cssbox/ -v`
Expected: PASS.

- [ ] **Step 5: Vet + lint**

Run: `go vet ./pkg/layout/cssbox/ && golangci-lint run ./pkg/layout/cssbox/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/cssbox/
git commit -m "cssbox: the recursive format-neutral box tree type"
```

---

## Task 5: `pkg/html` — the owned DOM node types

**Files:**
- Create: `pkg/html/dom.go`, `pkg/html/dom_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/html/dom_test.go`:

```go
package html

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/css"
)

func TestElementSatisfiesCSSNode(t *testing.T) {
	var _ css.Node = (*Element)(nil) // compile-time assertion
}

func TestElementAccessors(t *testing.T) {
	root := &Element{tag: "body"}
	el := &Element{
		tag:     "p",
		id:      "lead",
		classes: []string{"intro", "note"},
		attrs:   map[string]string{"style": "color:red", "id": "lead"},
		parent:  root,
	}
	root.children = []DOMNode{el}

	if el.Tag() != "p" || el.ID() != "lead" {
		t.Errorf("tag/id = %q/%q", el.Tag(), el.ID())
	}
	if len(el.Classes()) != 2 || el.Classes()[0] != "intro" {
		t.Errorf("classes = %v", el.Classes())
	}
	if v, ok := el.Attr("style"); !ok || v != "color:red" {
		t.Errorf("Attr(style) = %q,%v", v, ok)
	}
	if _, ok := el.Attr("missing"); ok {
		t.Error("Attr(missing) should be absent")
	}
	if el.Parent() != css.Node(root) {
		t.Error("Parent() should return the root element as a css.Node")
	}
}

func TestRootParentIsNil(t *testing.T) {
	root := &Element{tag: "html"}
	if root.Parent() != nil { // css.Node form: true nil at root
		t.Error("root Parent() must be nil (the cascade's initial-values signal)")
	}
	if root.ParentElement() != nil {
		t.Error("root ParentElement() must be nil")
	}
}

func TestTextNodeParent(t *testing.T) {
	p := &Element{tag: "p"}
	txt := &Text{Data: "hi", parent: p}
	if txt.ParentElement() != p { // Text exposes only the typed accessor
		t.Error("text node parent wrong")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/html/ -run 'TestElement|TestRoot|TestText' -v`
Expected: FAIL — identifiers undefined.

- [ ] **Step 3: Implement the DOM node types**

**Design note before you write this:** `css.Node` requires `Parent() css.Node`. A method can only
have one signature, so `*Element`'s exported `Parent()` returns `css.Node` (to satisfy `css.Node`),
and a separate `ParentElement() *Element` gives box generation the typed parent it needs for internal
traversal. `Parent()` must return a **true nil** at the root — returning a typed `(*Element)(nil)`
would be a non-nil `css.Node` interface and break the cascade's `Parent() == nil` root check. The
shared `DOMNode` interface therefore uses `ParentElement()`, not `Parent()`.

Create `pkg/html/dom.go` (this is the final form — write it as shown):

```go
package html

import "github.com/nathanstitt/doctaculous/pkg/css"

// DOMNode is the common interface over the owned tree (Element and Text). It is
// produced by Parse and is read-only thereafter. It uses ParentElement (not
// Parent) because *Element's Parent() returns css.Node to satisfy css.Node.
type DOMNode interface {
	// ParentElement returns the containing element, or nil at the root.
	ParentElement() *Element
	// node is unexported so only this package's types satisfy DOMNode.
	node()
}

// Element is an owned HTML element. All of its css.Node data is pre-computed at
// parse time, so the cascade tree-walk does no per-call allocation. *Element
// implements css.Node.
type Element struct {
	tag      string
	id       string
	classes  []string
	attrs    map[string]string
	parent   *Element
	children []DOMNode
}

func (e *Element) node() {}

// Parent returns the element's parent as a css.Node, or a true nil at the root.
// This is the css.Node implementation; internal tree code uses ParentElement.
func (e *Element) Parent() css.Node {
	if e.parent == nil {
		return nil // true nil interface, so the cascade's root check works
	}
	return e.parent
}

// ParentElement returns the typed parent element, or nil at the root. Used by box
// generation and DOM traversal where the concrete type is wanted.
func (e *Element) ParentElement() *Element { return e.parent }

// Children returns the element's child nodes in document order.
func (e *Element) Children() []DOMNode { return e.children }

// Tag returns the lowercased element name. Implements css.Node.
func (e *Element) Tag() string { return e.tag }

// ID returns the element's id attribute, or "". Implements css.Node.
func (e *Element) ID() string { return e.id }

// Classes returns the element's class list. Implements css.Node.
func (e *Element) Classes() []string { return e.classes }

// Attr returns an attribute value and whether it was present. Implements css.Node.
func (e *Element) Attr(key string) (string, bool) {
	v, ok := e.attrs[key]
	return v, ok
}

// Text is an owned character-data node.
type Text struct {
	Data   string
	parent *Element
}

func (t *Text) node() {}

// ParentElement returns the text node's parent element.
func (t *Text) ParentElement() *Element { return t.parent }
```

- [ ] **Step 4: (No separate test change needed)**

The Step 1 test already uses `Parent()` for the `css.Node` form (`el.Parent() != css.Node(root)`,
`root.Parent() != nil`) and `ParentElement()` for the typed checks, matching the accessors from
Step 3. Proceed to running it.

- [ ] **Step 5: Run to verify pass**

Run: `go test ./pkg/html/ -run 'TestElement|TestRoot|TestText' -v`
Expected: PASS, including the `var _ css.Node = (*Element)(nil)` assertion.

- [ ] **Step 6: Vet + lint**

Run: `go vet ./pkg/html/ && golangci-lint run ./pkg/html/...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add pkg/html/dom.go pkg/html/dom_test.go
git commit -m "html: owned DOM node types implementing css.Node"
```

---

## Task 6: `pkg/html` — the UA default stylesheet

**Files:**
- Create: `pkg/html/ua.go`, `pkg/html/ua_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/html/ua_test.go`:

```go
package html

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/css"
)

// resolve builds a resolver from the UA sheet alone and computes a tag's style.
func uaStyle(tag string) css.ComputedStyle {
	r := css.NewResolver([]css.OriginSheet{{Sheet: UAStylesheet, Origin: css.OriginUA}}, nil)
	return r.ComputeRoot(&fakeElem{tag: tag})
}

func TestUADisplayDefaults(t *testing.T) {
	blocks := []string{"div", "p", "h1", "h6", "section", "ul", "ol", "table", "blockquote"}
	for _, tag := range blocks {
		if d := uaStyle(tag).Display; d != "block" {
			t.Errorf("%s display = %q, want block", tag, d)
		}
	}
	if d := uaStyle("li").Display; d != "list-item" {
		t.Errorf("li display = %q, want list-item", d)
	}
	for _, tag := range []string{"head", "script", "style", "title"} {
		if d := uaStyle(tag).Display; d != "none" {
			t.Errorf("%s display = %q, want none", tag, d)
		}
	}
}

func TestUAHeadingSizes(t *testing.T) {
	h1 := uaStyle("h1")
	if !h1.Bold {
		t.Error("h1 should be bold by UA default")
	}
	// h1 font-size should be larger than the 16pt initial.
	if h1.FontSizePt <= 16 {
		t.Errorf("h1 font-size = %v, want > 16", h1.FontSizePt)
	}
}
```

Add a tiny in-package fake element for UA tests (mirrors the `pkg/css` fakeNode). Create it once here;
later tasks reuse it. Append to `pkg/html/ua_test.go`:

```go
// fakeElem is a minimal css.Node for UA-sheet tests (no real DOM needed).
type fakeElem struct {
	tag     string
	id      string
	classes []string
	parent  css.Node
}

func (f *fakeElem) Tag() string                   { return f.tag }
func (f *fakeElem) ID() string                    { return f.id }
func (f *fakeElem) Classes() []string             { return f.classes }
func (f *fakeElem) Parent() css.Node              { return f.parent }
func (f *fakeElem) Attr(string) (string, bool)    { return "", false }
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/html/ -run TestUA -v`
Expected: FAIL — `UAStylesheet` undefined.

- [ ] **Step 3: Implement the UA stylesheet**

Create `pkg/html/ua.go`:

```go
package html

import "github.com/nathanstitt/doctaculous/pkg/css"

// uaSource is the minimal user-agent default stylesheet. It is the lowest cascade
// origin (OriginUA) and supplies the display defaults and a few presentational
// defaults that make HTML render as HTML; without it every element would be
// display:inline (the CSS initial value). It is intentionally small and grows as
// later sub-projects need more defaults.
const uaSource = `
html, body, div, p, section, article, header, footer, nav, main, aside,
ul, ol, blockquote, pre, table, form, figure, figcaption, hr, h1, h2, h3, h4, h5, h6 {
	display: block;
}
li { display: list-item; }
tr { display: table-row; }
td, th { display: table-cell; }
head, title, meta, link, style, script { display: none; }

h1 { font-size: 32px; font-weight: bold; margin-top: 21px; margin-bottom: 21px; }
h2 { font-size: 24px; font-weight: bold; margin-top: 20px; margin-bottom: 20px; }
h3 { font-size: 19px; font-weight: bold; margin-top: 18px; margin-bottom: 18px; }
h4 { font-size: 16px; font-weight: bold; margin-top: 21px; margin-bottom: 21px; }
h5 { font-size: 13px; font-weight: bold; margin-top: 22px; margin-bottom: 22px; }
h6 { font-size: 11px; font-weight: bold; margin-top: 24px; margin-bottom: 24px; }
p, blockquote { margin-top: 16px; margin-bottom: 16px; }
th { font-weight: bold; }
`

// UAStylesheet is the parsed user-agent default stylesheet, cascaded at
// css.OriginUA below all author styles.
var UAStylesheet = css.Parse(uaSource)
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./pkg/html/ -run TestUA -v`
Expected: PASS.

- [ ] **Step 5: Vet + lint**

Run: `go vet ./pkg/html/ && golangci-lint run ./pkg/html/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add pkg/html/ua.go pkg/html/ua_test.go
git commit -m "html: minimal user-agent default stylesheet"
```

---

## Task 7: `pkg/html` — `Parse` (the x/net/html walk + collection)

**Files:**
- Create: `pkg/html/html.go`, `pkg/html/html_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/html/html_test.go`:

```go
package html

import (
	"strings"
	"testing"
)

func TestParseBuildsOwnedTree(t *testing.T) {
	doc, err := Parse([]byte(`<html><body><p id="x" class="a b">hi</p></body></html>`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Root.Tag() != "html" {
		t.Fatalf("root = %q, want html", doc.Root.Tag())
	}
	// html > body > p > text "hi"
	body := firstChildElement(doc.Root, "body")
	if body == nil {
		t.Fatal("no body")
	}
	p := firstChildElement(body, "p")
	if p == nil {
		t.Fatal("no p")
	}
	if p.ID() != "x" || len(p.Classes()) != 2 || p.Classes()[1] != "b" {
		t.Errorf("p id/classes = %q/%v", p.ID(), p.Classes())
	}
	if p.ParentElement() != body {
		t.Error("p parent should be body")
	}
	// text child
	var txt *Text
	for _, c := range p.Children() {
		if tn, ok := c.(*Text); ok {
			txt = tn
		}
	}
	if txt == nil || strings.TrimSpace(txt.Data) != "hi" {
		t.Errorf("text child = %+v", txt)
	}
}

func TestParseCollectsStyleSheets(t *testing.T) {
	doc, err := Parse([]byte(`<html><head><style>p{color:red}</style><style>div{color:blue}</style></head><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.StyleSheets) != 2 {
		t.Fatalf("got %d stylesheets, want 2", len(doc.StyleSheets))
	}
	// document order preserved: first sheet has the p rule.
	if len(doc.StyleSheets[0].Rules) != 1 || doc.StyleSheets[0].Rules[0].Declarations[0].Value != "red" {
		t.Errorf("first sheet = %+v", doc.StyleSheets[0])
	}
}

func TestParseCollectsLinkRefs(t *testing.T) {
	doc, err := Parse([]byte(`<html><head><link rel="stylesheet" href="a.css"><link rel="icon" href="favicon.ico"></head><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.LinkRefs) != 1 || doc.LinkRefs[0] != "a.css" {
		t.Errorf("LinkRefs = %v, want [a.css] (only rel=stylesheet)", doc.LinkRefs)
	}
}

func TestParseLeavesInlineStyleOnElement(t *testing.T) {
	doc, err := Parse([]byte(`<html><body><p style="color:red">x</p></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	body := firstChildElement(doc.Root, "body")
	p := firstChildElement(body, "p")
	if v, ok := p.Attr("style"); !ok || v != "color:red" {
		t.Errorf("inline style = %q,%v", v, ok)
	}
}

func TestParseMalformedDoesNotPanic(t *testing.T) {
	// Unclosed tags, stray text — x/net/html recovers; we must not panic.
	inputs := []string{
		`<html><body><p>open`,
		`<div><span></div></span>`,
		``,
		`just text no tags`,
		`<<<>>>`,
	}
	for _, in := range inputs {
		doc, err := Parse([]byte(in))
		if err != nil {
			t.Errorf("Parse(%q) errored: %v", in, err)
			continue
		}
		if doc.Root == nil {
			t.Errorf("Parse(%q) gave nil root", in)
		}
	}
}

// firstChildElement returns the first direct child element with the given tag.
func firstChildElement(e *Element, tag string) *Element {
	for _, c := range e.Children() {
		if el, ok := c.(*Element); ok && el.Tag() == tag {
			return el
		}
	}
	return nil
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/html/ -run TestParse -v`
Expected: FAIL — `Parse` / `Document` undefined.

- [ ] **Step 3: Implement `Parse` and `Document`**

Create `pkg/html/html.go`:

```go
// Package html is the HTML frontend: it parses HTML bytes (via
// golang.org/x/net/html) into an owned, read-only DOM that implements the
// pkg/css Node interface, and collects the stylesheets the cascade needs
// (<style> contents, <link rel=stylesheet> hrefs, and inline style=""). It does
// no layout and no rendering; box generation (pkg/layout/css) consumes its
// output.
package html

import (
	"bytes"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/nathanstitt/doctaculous/pkg/css"
)

// Document is the result of parsing an HTML document: the owned DOM root plus the
// stylesheets discovered while walking it. It is read-only after Parse.
type Document struct {
	// Root is the <html> element.
	Root *Element
	// StyleSheets are parsed <style> contents in document order (order is a
	// cascade tie-breaker).
	StyleSheets []css.Stylesheet
	// LinkRefs are the hrefs of <link rel=stylesheet>, unresolved. Box generation
	// resolves them through a resource.ResourceLoader.
	LinkRefs []string
}

// Parse parses HTML bytes into an owned DOM Document. It is total on the kinds of
// malformed input x/net/html recovers from (unclosed tags, stray text): such
// input yields a valid-but-quirky tree, never a panic. An error is returned only
// for input the underlying parser cannot read at all.
func Parse(data []byte) (*Document, error) {
	root, err := xhtml.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	doc := &Document{}
	// xhtml.Parse returns a DocumentNode; find the <html> element under it.
	htmlNode := findElement(root, "html")
	if htmlNode == nil {
		// x/net/html always synthesizes <html>, but guard anyway: build an empty root.
		doc.Root = &Element{tag: "html"}
		return doc, nil
	}
	doc.Root = buildElement(htmlNode, nil, doc)
	return doc, nil
}

// buildElement converts an x/net/html element node (and its subtree) into an
// owned *Element, collecting stylesheets/links into doc as it goes.
func buildElement(n *xhtml.Node, parent *Element, doc *Document) *Element {
	el := &Element{
		tag:    n.Data, // x/net/html lowercases HTML tag names
		parent: parent,
		attrs:  make(map[string]string, len(n.Attr)),
	}
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		el.attrs[key] = a.Val
		switch key {
		case "id":
			el.id = a.Val
		case "class":
			el.classes = strings.Fields(a.Val)
		}
	}

	// Collect <style> text and <link rel=stylesheet> hrefs.
	switch el.tag {
	case "style":
		if css := textContent(n); css != "" {
			doc.StyleSheets = append(doc.StyleSheets, cssParse(css))
		}
	case "link":
		if strings.EqualFold(el.attrs["rel"], "stylesheet") {
			if href := el.attrs["href"]; href != "" {
				doc.LinkRefs = append(doc.LinkRefs, href)
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case xhtml.ElementNode:
			el.children = append(el.children, buildElement(c, el, doc))
		case xhtml.TextNode:
			el.children = append(el.children, &Text{Data: c.Data, parent: el})
		}
	}
	return el
}

// findElement returns the first element node with the given (lowercased) tag in a
// depth-first walk of an x/net/html tree.
func findElement(n *xhtml.Node, tag string) *xhtml.Node {
	if n.Type == xhtml.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

// textContent returns the concatenated text of an element's direct text children
// (sufficient for <style>, whose content is a single text node).
func textContent(n *xhtml.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

// cssParse is a tiny indirection so the collection code reads clearly; it parses
// a stylesheet string via pkg/css.
func cssParse(src string) css.Stylesheet { return css.Parse(src) }
```

Note the local variable `css` shadows the package in the `style` case — rename it to avoid the
collision. Use:

```go
	case "style":
		if sheetSrc := textContent(n); strings.TrimSpace(sheetSrc) != "" {
			doc.StyleSheets = append(doc.StyleSheets, css.Parse(sheetSrc))
		}
```

and delete the `cssParse` helper (call `css.Parse` directly).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./pkg/html/ -run TestParse -v`
Expected: PASS (all five Parse tests).

- [ ] **Step 5: Run the whole `pkg/html` package**

Run: `go test ./pkg/html/ -v`
Expected: PASS (DOM, UA, and Parse tests together).

- [ ] **Step 6: Vet + lint**

Run: `go vet ./pkg/html/ && golangci-lint run ./pkg/html/...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add pkg/html/html.go pkg/html/html_test.go
git commit -m "html: Parse HTML into the owned DOM, collecting <style>/<link>/style"
```

---

## Task 8: `pkg/layout/css` — box generation (the recursive descent)

This builds the box tree **without** anonymous-box fixups yet (Task 9 adds those). The display→kind
mapping, `display:none` pruning, `<img>` replaced leaves, `<link>` resolution, and cascade threading
all land here.

**Files:**
- Create: `pkg/layout/css/build.go`, `pkg/layout/css/build_test.go`

- [ ] **Step 1: Write the failing tests (pre-normalization tree shape)**

Create `pkg/layout/css/build_test.go`:

```go
package css

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// build is a test helper: parse HTML, run Build with an optional loader.
func build(t *testing.T, src string, loader resource.ResourceLoader) *cssbox.Box {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, loader, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return root
}

// find returns the first box (depth-first) whose element style has the given
// tag-driven display, matched loosely by walking for a kind+display combo. For
// precise lookups, tests assert structurally on Children.
func firstByDisplay(b *cssbox.Box, d cssbox.DisplayKind) *cssbox.Box {
	if b.Display == d {
		return b
	}
	for _, c := range b.Children {
		if got := firstByDisplay(c, d); got != nil {
			return got
		}
	}
	return nil
}

func TestBuildMapsDisplay(t *testing.T) {
	root := build(t, `<html><body><div>x</div><span>y</span></body></html>`, nil)
	if root.Display != cssbox.DisplayBlock { // html is block per UA
		t.Errorf("root display = %v, want block", root.Display)
	}
	div := firstByDisplay(root, cssbox.DisplayBlock)
	if div == nil || div.Kind != cssbox.BoxBlock {
		t.Fatalf("expected a block box for div")
	}
	// span is inline
	var span *cssbox.Box
	var walk func(*cssbox.Box)
	walk = func(b *cssbox.Box) {
		if b.Kind == cssbox.BoxInline {
			span = b
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)
	if span == nil {
		t.Error("expected an inline box for span")
	}
}

func TestBuildPrunesDisplayNone(t *testing.T) {
	// head (display:none via UA) must not appear in the box tree.
	root := build(t, `<html><head><title>t</title></head><body><p>hi</p></body></html>`, nil)
	var sawNone bool
	var walk func(*cssbox.Box)
	walk = func(b *cssbox.Box) {
		if b.Display == cssbox.DisplayNone {
			sawNone = true
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)
	if sawNone {
		t.Error("display:none subtree should be pruned, not emitted")
	}
	// the head should have produced no child under html (only body).
	if len(root.Children) != 1 {
		t.Errorf("html should have 1 child (body) after pruning head, got %d", len(root.Children))
	}
}

func TestBuildAuthorOverridesUA(t *testing.T) {
	// author makes div inline, overriding the UA block default.
	root := build(t, `<html><head><style>div{display:inline}</style></head><body><div>x</div></body></html>`, nil)
	var div *cssbox.Box
	var walk func(*cssbox.Box)
	walk = func(b *cssbox.Box) {
		if b.Style.Display == "inline" && b.Kind == cssbox.BoxInline {
			div = b
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)
	if div == nil {
		t.Error("author display:inline should override UA block for div")
	}
}

func TestBuildReplacedImg(t *testing.T) {
	root := build(t, `<html><body><img src="a.png" alt="a"></body></html>`, nil)
	img := firstByKind(root, cssbox.BoxReplaced)
	if img == nil || img.Replaced == nil {
		t.Fatal("expected a replaced box for img")
	}
	if img.Replaced.Tag != "img" || img.Replaced.Attrs["src"] != "a.png" {
		t.Errorf("replaced facts = %+v", img.Replaced)
	}
}

func TestBuildFlexPreservedAsDisplay(t *testing.T) {
	root := build(t, `<html><head><style>div{display:flex}</style></head><body><div>x</div></body></html>`, nil)
	flex := firstByDisplay(root, cssbox.DisplayFlex)
	if flex == nil {
		t.Fatal("flex display should be preserved (not normalized to block)")
	}
	if flex.Formatting != cssbox.FlexFC {
		t.Errorf("flex box formatting = %v, want FlexFC", flex.Formatting)
	}
	if flex.Kind != cssbox.BoxBlock { // flex container is block-level
		t.Errorf("flex container kind = %v, want BoxBlock", flex.Kind)
	}
}

func TestBuildUnknownDisplayNormalizesToBlock(t *testing.T) {
	root := build(t, `<html><head><style>div{display:wobble}</style></head><body><div>x</div></body></html>`, nil)
	var found *cssbox.Box
	var walk func(*cssbox.Box)
	walk = func(b *cssbox.Box) {
		if b.Style.Display == "wobble" {
			found = b
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)
	if found == nil {
		t.Fatal("div not found")
	}
	if found.Display != cssbox.DisplayBlock || found.Kind != cssbox.BoxBlock {
		t.Errorf("unknown display = (%v,%v), want (block, BoxBlock)", found.Display, found.Kind)
	}
}

func TestBuildResolvesLinkSheet(t *testing.T) {
	loader := resource.MapLoader{
		"theme.css": {Data: []byte(`p{color:red}`), ContentType: "text/css"},
	}
	root := build(t, `<html><head><link rel="stylesheet" href="theme.css"></head><body><p>x</p></body></html>`, loader)
	// Find the p box (the parent of the text run "x") and check it got its color
	// from the linked stylesheet.
	pBox := parentOfText(root, "x")
	if pBox == nil {
		t.Fatal("p box not found")
	}
	if pBox.Style.Color.R != 255 || pBox.Style.Color.G != 0 {
		t.Errorf("p color = %v, want red from linked sheet", pBox.Style.Color)
	}
}

func TestBuildMissingLinkDegrades(t *testing.T) {
	// No loader / missing ref: build still succeeds with UA + inline styles.
	root := build(t, `<html><head><link rel="stylesheet" href="missing.css"></head><body><p>x</p></body></html>`, resource.MapLoader{})
	if root == nil {
		t.Fatal("Build should succeed despite a missing link")
	}
}

// firstByKind returns the first box (depth-first) with the given kind, or nil.
func firstByKind(b *cssbox.Box, k cssbox.BoxKind) *cssbox.Box {
	if b.Kind == k {
		return b
	}
	for _, c := range b.Children {
		if got := firstByKind(c, k); got != nil {
			return got
		}
	}
	return nil
}

// parentOfText returns the box whose direct child is a text run equal to text.
func parentOfText(b *cssbox.Box, text string) *cssbox.Box {
	for _, c := range b.Children {
		if c.Kind == cssbox.BoxText && c.Text == text {
			return b
		}
		if got := parentOfText(c, text); got != nil {
			return got
		}
	}
	return nil
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/layout/css/ -v`
Expected: FAIL — `Build` undefined.

- [ ] **Step 3: Implement `Build` (recursive descent, no anon fixups yet)**

Create `pkg/layout/css/build.go`:

```go
// Package css is the box-generation stage: it walks an html.Document, drives the
// pkg/css cascade per element, and emits a cssbox tree. Box generation stores the
// computed style on each box and normalizes the tree with anonymous-box fixups,
// so the layout engine receives a well-formed tree (a block container's children
// are either all block-level or all inline-level). It produces no pixels.
package css

import (
	"context"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// replacedTags are elements treated as replaced content (leaf boxes carrying
// their attributes; no decoded media in this sub-project).
var replacedTags = map[string]bool{"img": true}

// Build generates a cssbox tree from a parsed HTML document. loader resolves
// <link rel=stylesheet> refs (may be nil → links skipped); logf receives
// degradation messages (may be nil). It never panics on malformed input: a
// recover at the entry boundary returns whatever tree was built so far.
func Build(ctx context.Context, doc *html.Document, loader resource.ResourceLoader, logf func(string, ...any)) (root *cssbox.Box, err error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	defer func() {
		if r := recover(); r != nil {
			logf("box generation recovered from panic: %v", r)
			if root == nil {
				root = &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
			}
			err = nil
		}
	}()

	sheets := assembleSheets(ctx, doc, loader, logf)
	resolver := gcss.NewResolver(sheets, logf)

	root = generate(doc.Root, resolver, resolver.ComputeRoot(doc.Root))
	normalize(root) // Task 9 fills this in; defined as a no-op stub until then.
	return root, nil
}

// assembleSheets returns the origin-ordered sheets: the UA sheet first, then the
// document's <style> sheets and any resolvable <link> sheets (all author).
func assembleSheets(ctx context.Context, doc *html.Document, loader resource.ResourceLoader, logf func(string, ...any)) []gcss.OriginSheet {
	sheets := []gcss.OriginSheet{{Sheet: html.UAStylesheet, Origin: gcss.OriginUA}}
	for _, s := range doc.StyleSheets {
		sheets = append(sheets, gcss.OriginSheet{Sheet: s, Origin: gcss.OriginAuthor})
	}
	if loader != nil {
		for _, ref := range doc.LinkRefs {
			data, _, err := loader.Load(ctx, ref)
			if err != nil {
				logf("link stylesheet %q: %v (skipped)", ref, err)
				continue
			}
			sheets = append(sheets, gcss.OriginSheet{Sheet: gcss.Parse(string(data)), Origin: gcss.OriginAuthor})
		}
	}
	return sheets
}

// generate recursively builds the box for element e (whose computed style is cs)
// and its descendants. Returns nil for a display:none subtree.
func generate(e *html.Element, r *gcss.Resolver, cs gcss.ComputedStyle) *cssbox.Box {
	if cs.Display == "none" {
		return nil
	}

	b := &cssbox.Box{Style: cs}
	classifyDisplay(b, cs.Display)
	b.Float = floatOf(cs)
	b.Position = positionOf(cs)

	if replacedTags[e.Tag()] {
		b.Kind = cssbox.BoxReplaced
		b.Replaced = &cssbox.ReplacedContent{Tag: e.Tag(), Attrs: attrSnapshot(e)}
		return b // replaced elements are leaves
	}

	for _, child := range e.Children() {
		switch c := child.(type) {
		case *html.Element:
			childCS := r.Compute(c, cs)
			if cb := generate(c, r, childCS); cb != nil {
				b.Children = append(b.Children, cb)
			}
		case *html.Text:
			if t := makeTextBox(c.Data, cs); t != nil {
				b.Children = append(b.Children, t)
			}
		}
	}
	return b
}

// makeTextBox creates a text box for raw character data, or nil if the data is
// empty. Whitespace collapsing/stripping is applied during normalization
// (Task 9); here we keep the raw text but skip a wholly-empty string.
func makeTextBox(data string, parent gcss.ComputedStyle) *cssbox.Box {
	if data == "" {
		return nil
	}
	return &cssbox.Box{Kind: cssbox.BoxText, Text: data, Style: parent, Display: cssbox.DisplayInline}
}

// attrSnapshot copies an element's attributes for a ReplacedContent.
func attrSnapshot(e *html.Element) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"src", "alt", "width", "height"} {
		if v, ok := e.Attr(k); ok {
			out[k] = v
		}
	}
	return out
}

// floatOf maps a computed style to a FloatKind. The float property is not on
// ComputedStyle's normal-flow subset yet, so this reads it from a raw lookup when
// available; today it returns FloatNone (extended when float lands in pkg/css).
func floatOf(_ gcss.ComputedStyle) cssbox.FloatKind { return cssbox.FloatNone }

// positionOf maps a computed style to a PositionKind. Like floatOf, position is
// not yet on ComputedStyle's subset; returns PosStatic until it is.
func positionOf(_ gcss.ComputedStyle) cssbox.PositionKind { return cssbox.PosStatic }
```

Create the display classifier in the same package (`pkg/layout/css/build.go`, appended):

```go
// classifyDisplay sets the box's Kind, Display, and Formatting from a computed
// display string. Recognized layout modes not yet implemented (flex/grid/table)
// keep their true DisplayKind/FormattingContext; the layout engine does the
// block fallback later. Genuinely unknown values normalize to block.
func classifyDisplay(b *cssbox.Box, display string) {
	switch display {
	case "inline":
		b.Kind, b.Display, b.Formatting = cssbox.BoxInline, cssbox.DisplayInline, cssbox.InlineFC
	case "inline-block":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayInlineBlock, cssbox.BlockFC
	case "list-item":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayListItem, cssbox.BlockFC
	case "table":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTable, cssbox.TableFC
	case "table-row":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableRow, cssbox.TableFC
	case "table-cell":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayTableCell, cssbox.BlockFC
	case "flex":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayFlex, cssbox.FlexFC
	case "grid":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayGrid, cssbox.GridFC
	case "block":
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayBlock, cssbox.BlockFC
	default:
		// unknown display value → block normal flow.
		b.Kind, b.Display, b.Formatting = cssbox.BoxBlock, cssbox.DisplayBlock, cssbox.BlockFC
	}
}
```

Add a temporary `normalize` no-op stub so this task compiles and passes on its own (Task 9 replaces
it). Append to `pkg/layout/css/build.go`:

```go
// normalize applies anonymous-box fixups and whitespace handling to the tree.
// Implemented in anon.go (Task 9); declared here as the call site. Until anon.go
// lands, this stub leaves the tree unchanged.
func normalize(_ *cssbox.Box) {}
```

(When Task 9 lands, delete this stub and implement `normalize` in `anon.go`.)

- [ ] **Step 4: Run to verify pass**

Run: `go test ./pkg/layout/css/ -v`
Expected: PASS (all Build tests). The `firstByDisplay`/walk helpers may report a div block before the
inline span — assertions only check existence, so order doesn't matter.

- [ ] **Step 5: Vet + lint**

Run: `go vet ./pkg/layout/css/ && golangci-lint run ./pkg/layout/css/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/build.go pkg/layout/css/build_test.go
git commit -m "layout/css: box generation — recursive descent driving the cascade"
```

---

## Task 9: `pkg/layout/css` — anonymous-box normalization + whitespace

Replace the `normalize` stub with real fixups: inline-in-block wrapping, block-in-inline splitting,
and whitespace handling.

**Files:**
- Create: `pkg/layout/css/anon.go`, `pkg/layout/css/anon_test.go`
- Modify: `pkg/layout/css/build.go` (delete the `normalize` stub)

- [ ] **Step 1: Write the failing tests**

Create `pkg/layout/css/anon_test.go`:

```go
package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func TestInlineInBlockWrapsRuns(t *testing.T) {
	// A block div with mixed children: text, a block child, more text. The two
	// inline runs must each be wrapped in an anonymous block.
	root := build(t, `<html><body><div>before<p>para</p>after</div></body></html>`, nil)
	div := root.Children[0].Children[0] // html > body > div
	// div children should be: [AnonBlock(before), Block(p), AnonBlock(after)]
	if len(div.Children) != 3 {
		t.Fatalf("div has %d children, want 3 (anon, p, anon): %s", len(div.Children), dump(div))
	}
	if div.Children[0].Kind != cssbox.BoxAnonBlock {
		t.Errorf("child 0 = %v, want BoxAnonBlock", div.Children[0].Kind)
	}
	if div.Children[1].Kind != cssbox.BoxBlock {
		t.Errorf("child 1 = %v, want BoxBlock (the p)", div.Children[1].Kind)
	}
	if div.Children[2].Kind != cssbox.BoxAnonBlock {
		t.Errorf("child 2 = %v, want BoxAnonBlock", div.Children[2].Kind)
	}
	// the anon block wraps the text run
	if len(div.Children[0].Children) != 1 || div.Children[0].Children[0].Text != "before" {
		t.Errorf("anon block 0 should wrap text 'before': %s", dump(div.Children[0]))
	}
}

func TestAllInlineChildrenNotWrapped(t *testing.T) {
	// A block with only inline children needs no anonymous blocks.
	root := build(t, `<html><body><p>just <span>inline</span> text</p></body></html>`, nil)
	p := root.Children[0].Children[0] // html>body>p
	for _, c := range p.Children {
		if c.Kind == cssbox.BoxAnonBlock {
			t.Errorf("all-inline block should not get anonymous blocks: %s", dump(p))
		}
	}
}

func TestBlockInInlineSplitsInline(t *testing.T) {
	// An inline span containing a block div: the span splits around the block.
	root := build(t, `<html><body><div><span>a<div>B</div>c</span></div></body></html>`, nil)
	outer := root.Children[0].Children[0] // html>body>div(outer)
	// After block-in-inline, outer's children should contain a block (from the
	// split-out inner div) flanked by anonymous boxes carrying the inline pieces.
	var sawBlock bool
	for _, c := range outer.Children {
		if c.Kind == cssbox.BoxBlock && len(c.Children) > 0 && c.Children[0].Text == "B" {
			sawBlock = true
		}
	}
	if !sawBlock {
		t.Errorf("block inside inline should break out to a block-level box: %s", dump(outer))
	}
	// The outer block's children must satisfy the all-block-or-all-inline
	// invariant: since a block broke out, every child must be block-level.
	for _, c := range outer.Children {
		if !c.Kind.IsBlockLevel() {
			t.Errorf("after block-in-inline split, all children must be block-level: %s", dump(outer))
		}
	}
}

func TestWhitespaceBetweenBlocksDropped(t *testing.T) {
	// Whitespace-only text between block children must not create anon blocks.
	root := build(t, "<html><body><div><p>a</p>\n   <p>b</p></div></body></html>", nil)
	div := root.Children[0].Children[0]
	if len(div.Children) != 2 {
		t.Errorf("div should have 2 block children (no anon from inter-block whitespace), got %d: %s", len(div.Children), dump(div))
	}
	for _, c := range div.Children {
		if c.Kind == cssbox.BoxAnonBlock {
			t.Errorf("inter-block whitespace should be dropped, not wrapped: %s", dump(div))
		}
	}
}

func TestInternalWhitespaceCollapsed(t *testing.T) {
	root := build(t, "<html><body><p>a    b\t\nc</p></body></html>", nil)
	p := root.Children[0].Children[0]
	// the text run should collapse runs of whitespace to single spaces.
	var text string
	for _, c := range p.Children {
		if c.Kind == cssbox.BoxText {
			text += c.Text
		}
	}
	if text != "a b c" {
		t.Errorf("collapsed text = %q, want %q", text, "a b c")
	}
}

// --- test helpers ---

// dump renders a box subtree compactly for failure messages.
func dump(b *cssbox.Box) string {
	return dumpIndent(b, 0)
}

func dumpIndent(b *cssbox.Box, depth int) string {
	pad := ""
	for i := 0; i < depth; i++ {
		pad += "  "
	}
	s := pad + kindName(b.Kind)
	if b.Kind == cssbox.BoxText {
		s += " " + quote(b.Text)
	}
	s += "\n"
	for _, c := range b.Children {
		s += dumpIndent(c, depth+1)
	}
	return s
}

func kindName(k cssbox.BoxKind) string {
	switch k {
	case cssbox.BoxBlock:
		return "Block"
	case cssbox.BoxInline:
		return "Inline"
	case cssbox.BoxAnonBlock:
		return "AnonBlock"
	case cssbox.BoxAnonInline:
		return "AnonInline"
	case cssbox.BoxReplaced:
		return "Replaced"
	case cssbox.BoxText:
		return "Text"
	}
	return "?"
}

func quote(s string) string { return "\"" + s + "\"" }
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/layout/css/ -run 'TestInline|TestAll|TestBlockIn|TestWhitespace|TestInternal' -v`
Expected: FAIL — the stub `normalize` does nothing, so anonymous boxes/whitespace handling are absent.

- [ ] **Step 3: Delete the stub and implement `normalize` in `anon.go`**

In `pkg/layout/css/build.go`, delete the temporary `normalize` stub function.

Create `pkg/layout/css/anon.go`:

```go
package css

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// normalize rewrites the tree so every box satisfies the layout invariant: a
// block container's children are either all block-level or all inline-level.
// It runs three passes per box, bottom-up:
//  1. block-in-inline: split an inline box that contains a block-level
//     descendant so the block breaks out to block level.
//  2. whitespace: collapse internal whitespace in text runs and drop runs that
//     are entirely whitespace adjacent to block boundaries.
//  3. inline-in-block: wrap maximal runs of inline-level children of a block
//     container (that also has block-level children) in anonymous block boxes.
func normalize(b *cssbox.Box) {
	// Recurse first (bottom-up) so children are already normalized.
	for _, c := range b.Children {
		normalize(c)
	}
	b.Children = splitBlockInInline(b.Children)
	b.Children = handleWhitespace(b.Children, b)
	if b.Kind.IsBlockLevel() {
		b.Children = wrapInlineRuns(b.Children, b)
	}
}

// splitBlockInInline lifts block-level boxes out of inline boxes. For each inline
// child that contains block-level descendants, it is replaced by a sequence:
// the inline pieces before the block (as an anonymous inline box), the block
// itself (promoted to this level), then the inline pieces after, etc. Inline
// children with no block descendant are left unchanged.
func splitBlockInInline(children []*cssbox.Box) []*cssbox.Box {
	var out []*cssbox.Box
	for _, c := range children {
		if c.Kind == cssbox.BoxInline && containsBlockLevel(c) {
			out = append(out, splitOneInline(c)...)
			continue
		}
		out = append(out, c)
	}
	return out
}

// containsBlockLevel reports whether any direct child of b is block-level.
func containsBlockLevel(b *cssbox.Box) bool {
	for _, c := range b.Children {
		if c.Kind.IsBlockLevel() {
			return true
		}
	}
	return false
}

// splitOneInline splits a single inline box around its block-level children,
// producing a flat slice of block-level boxes and anonymous-inline boxes that
// carry the inline fragments. The inline's own style is copied onto each
// anonymous-inline fragment so styling is preserved.
func splitOneInline(inline *cssbox.Box) []*cssbox.Box {
	var out []*cssbox.Box
	var run []*cssbox.Box
	flush := func() {
		if len(run) == 0 {
			return
		}
		out = append(out, &cssbox.Box{
			Kind:       cssbox.BoxAnonInline,
			Style:      inline.Style,
			Display:    cssbox.DisplayInline,
			Formatting: cssbox.InlineFC,
			Children:   run,
		})
		run = nil
	}
	for _, c := range inline.Children {
		if c.Kind.IsBlockLevel() {
			flush()
			out = append(out, c) // promote the block to this level
			continue
		}
		run = append(run, c)
	}
	flush()
	return out
}

// wrapInlineRuns wraps maximal runs of inline-level children in anonymous block
// boxes, but only when the container also has at least one block-level child.
// If all children are inline-level, they are left as-is (the block establishes an
// inline formatting context directly).
func wrapInlineRuns(children []*cssbox.Box, parent *cssbox.Box) []*cssbox.Box {
	hasBlock := false
	for _, c := range children {
		if c.Kind.IsBlockLevel() {
			hasBlock = true
			break
		}
	}
	if !hasBlock {
		return children // all inline: no anonymous blocks needed
	}

	var out []*cssbox.Box
	var run []*cssbox.Box
	flush := func() {
		if len(run) == 0 {
			return
		}
		out = append(out, &cssbox.Box{
			Kind:       cssbox.BoxAnonBlock,
			Display:    cssbox.DisplayBlock,
			Formatting: cssbox.InlineFC, // an anon block holds inline content
			Children:   run,
		})
		run = nil
	}
	for _, c := range children {
		if c.Kind.IsBlockLevel() {
			flush()
			out = append(out, c)
			continue
		}
		run = append(run, c)
	}
	flush()
	return out
}

// handleWhitespace collapses internal whitespace in text runs and drops text
// boxes that are entirely collapsible whitespace when they sit adjacent to a
// block boundary (so inter-block whitespace does not create spurious anonymous
// blocks). Non-whitespace text has its internal whitespace runs collapsed to a
// single space.
func handleWhitespace(children []*cssbox.Box, parent *cssbox.Box) []*cssbox.Box {
	// First collapse internal whitespace in every text box.
	for _, c := range children {
		if c.Kind == cssbox.BoxText {
			c.Text = collapseWS(c.Text)
		}
	}
	// Then drop whitespace-only text boxes adjacent to block-level siblings (or at
	// the edges of a block container).
	parentIsBlockContainer := parent.Kind.IsBlockLevel()
	var out []*cssbox.Box
	for i, c := range children {
		if c.Kind == cssbox.BoxText && isAllWS(c.Text) {
			if parentIsBlockContainer && adjacentToBlock(children, i) {
				continue // drop inter-block whitespace
			}
		}
		out = append(out, c)
	}
	return out
}

// adjacentToBlock reports whether the child at index i has a block-level neighbor
// or is at an edge of the slice (treating container edges as block boundaries
// when the container is a block container).
func adjacentToBlock(children []*cssbox.Box, i int) bool {
	if i == 0 || i == len(children)-1 {
		return true
	}
	prevBlock := children[i-1].Kind.IsBlockLevel()
	nextBlock := children[i+1].Kind.IsBlockLevel()
	return prevBlock || nextBlock
}

// collapseWS collapses runs of ASCII whitespace to a single space, preserving a
// single leading/trailing space if present (CSS white-space:normal semantics for
// the structural purposes of this sub-project).
func collapseWS(s string) string {
	var b strings.Builder
	inWS := false
	for _, r := range s {
		if isWSRune(r) {
			if !inWS {
				b.WriteByte(' ')
				inWS = true
			}
			continue
		}
		b.WriteRune(r)
		inWS = false
	}
	return b.String()
}

func isAllWS(s string) bool {
	for _, r := range s {
		if !isWSRune(r) {
			return false
		}
	}
	return true
}

func isWSRune(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
}
```

Note on `TestInternalWhitespaceCollapsed`: the input `a    b\t\nc` collapses to `a b c` only if the
leading char is non-space (it is) — `collapseWS` turns each whitespace run into one space, giving
`a b c`. Good. If a text run has leading/trailing whitespace that should be trimmed at block edges,
that is the drop pass's job; internal single spaces are kept.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./pkg/layout/css/ -v`
Expected: PASS — both the Task 8 Build tests and the Task 9 anon/whitespace tests.

If `TestInlineInBlockWrapsRuns` fails on child count, dump the tree (the test prints `dump(div)`) and
verify: text "before" and "after" become `BoxText` inline-level children flanking the `p` block;
`wrapInlineRuns` wraps each in a `BoxAnonBlock`. The whitespace pass must not have dropped "before"/
"after" (they are non-whitespace, so they survive).

- [ ] **Step 5: Vet + lint**

Run: `go vet ./pkg/layout/css/ && golangci-lint run ./pkg/layout/css/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/anon.go pkg/layout/css/anon_test.go pkg/layout/css/build.go
git commit -m "layout/css: anonymous-box fixups + whitespace handling"
```

---

## Task 10: Full-tree integration test + race + repo-wide gates

**Files:**
- Create: `pkg/layout/css/integration_test.go`

- [ ] **Step 1: Write an end-to-end structural test**

Create `pkg/layout/css/integration_test.go`:

```go
package css

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// TestEndToEndBoxTree exercises a realistic document through parse → cascade →
// box generation → normalization, asserting the overall tree shape the layout
// engine (sub-project 3) will consume.
func TestEndToEndBoxTree(t *testing.T) {
	src := `<!doctype html>
<html>
  <head>
    <title>t</title>
    <style>
      .lead { color: rgb(10, 20, 30); }
      em { display: inline; }
    </style>
    <link rel="stylesheet" href="ext.css">
  </head>
  <body>
    <h1>Title</h1>
    <p class="lead">Hello <em>world</em>, this is text.</p>
    <div>before<p>nested</p>after</div>
    <img src="pic.png" alt="pic">
  </body>
</html>`

	loader := resource.MapLoader{
		"ext.css": {Data: []byte(`h1 { color: rgb(1,2,3); }`), ContentType: "text/css"},
	}
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, loader, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// head was display:none → pruned; html has exactly one child (body).
	if len(root.Children) != 1 {
		t.Fatalf("html children = %d, want 1 (body): %s", len(root.Children), dump(root))
	}
	body := root.Children[0]

	// body children: h1 (block), p (block), div (block), img (replaced).
	if len(body.Children) != 4 {
		t.Fatalf("body children = %d, want 4: %s", len(body.Children), dump(body))
	}

	h1 := body.Children[0]
	if h1.Kind != cssbox.BoxBlock || !h1.Style.Bold || h1.Style.FontSizePt <= 16 {
		t.Errorf("h1 wrong: kind=%v bold=%v size=%v", h1.Kind, h1.Style.Bold, h1.Style.FontSizePt)
	}
	// h1 color from the linked sheet (author) overriding UA:
	if h1.Style.Color.R != 1 || h1.Style.Color.G != 2 || h1.Style.Color.B != 3 {
		t.Errorf("h1 color = %v, want rgb(1,2,3) from ext.css", h1.Style.Color)
	}

	p := body.Children[1]
	if p.Kind != cssbox.BoxBlock {
		t.Errorf("p kind = %v, want block", p.Kind)
	}
	if p.Style.Color.R != 10 || p.Style.Color.G != 20 || p.Style.Color.B != 30 {
		t.Errorf("p.lead color = %v, want rgb(10,20,30)", p.Style.Color)
	}
	// p has all-inline content → no anonymous blocks.
	for _, c := range p.Children {
		if c.Kind == cssbox.BoxAnonBlock {
			t.Errorf("p should have no anon blocks: %s", dump(p))
		}
	}

	div := body.Children[2]
	// div has mixed content → [AnonBlock, Block(nested p), AnonBlock].
	if len(div.Children) != 3 ||
		div.Children[0].Kind != cssbox.BoxAnonBlock ||
		div.Children[1].Kind != cssbox.BoxBlock ||
		div.Children[2].Kind != cssbox.BoxAnonBlock {
		t.Errorf("div anon-wrapping wrong: %s", dump(div))
	}

	img := body.Children[3]
	if img.Kind != cssbox.BoxReplaced || img.Replaced == nil || img.Replaced.Attrs["src"] != "pic.png" {
		t.Errorf("img replaced box wrong: %+v", img.Replaced)
	}
}
```

- [ ] **Step 2: Run the integration test**

Run: `go test ./pkg/layout/css/ -run TestEndToEndBoxTree -v`
Expected: PASS. If a child count is off, the failure prints the tree via `dump`; reconcile against the
expected shape in the comments.

- [ ] **Step 3: Run every new package together**

Run: `go test ./pkg/css/... ./pkg/html/... ./pkg/resource/... ./pkg/layout/cssbox/... ./pkg/layout/css/...`
Expected: PASS across all five.

- [ ] **Step 4: Run the full suite with the race detector**

Run: `go test -race ./...`
Expected: PASS, no data races. (Rerun with the sandbox disabled if the build cache is blocked.) This
covers the read-only-after-build invariant for the DOM and box tree.

- [ ] **Step 5: Repo-wide vet + lint of touched packages**

Run: `go vet ./... && golangci-lint run ./pkg/css/... ./pkg/html/... ./pkg/resource/... ./pkg/layout/...`
Expected: clean (ignore the unrelated untracked `agent/skills/.../examples/` typecheck noise if it
appears from a root-level lint).

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/integration_test.go
git commit -m "layout/css: end-to-end box-tree integration test + race coverage"
```

---

## Task 11: CLAUDE.md update + PR notes

Per the design (§10) and the overarching spec (§2.2), record the architectural evolution and the new
dependency.

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Revise the Architecture reflow note**

In `CLAUDE.md`, find the Architecture paragraph that says a new reflow format is "just a parse+lower
frontend producing `box.Document`". Replace that sentence with a two-tier description. Use this text
(adjust surrounding wording to fit the paragraph):

```
**Reflowable documents** today use a flat box model (`pkg/layout/box.Document`) that serves the DOCX
frontend and the current reflow engine. The HTML frontend introduces a second, recursive
format-neutral model — `pkg/layout/cssbox` — that the forthcoming CSS layout engine consumes; a
reflow frontend is a parse+lower step producing one of these box models (DOCX → `box.Document` today;
HTML → `cssbox`). These converge later: a dedicated sub-project re-points DOCX lowering onto `cssbox`
and retires the flat model, so one recursive engine drives every reflow format. Font outlines for
both pipelines come from `pkg/font`; `pkg/layout/font` caches them.
```

- [ ] **Step 2: Add a "Done" roadmap entry for this sub-project**

In `CLAUDE.md`'s "Status & roadmap" → "Done" section, after the CSS parse+cascade entry, add:

```
- **HTML frontend — parse + box generation** (`pkg/html`, `pkg/layout/cssbox`, `pkg/layout/css`,
  `pkg/resource`; unit-tested by structural assertions, no rendering yet): parse HTML via
  `golang.org/x/net/html` into an owned DOM implementing the `pkg/css` `Node` interface, collect
  `<style>`/`<link>`/inline `style=""`, and generate a recursive `cssbox` tree by driving the CSS
  cascade per element. Includes a minimal user-agent default stylesheet (cascaded below author rules
  via a new origin-aware cascade in `pkg/css`), anonymous-box fixups (inline-in-block wrapping and
  block-in-inline splitting), whitespace handling, and `display:none` pruning. `<img>` becomes a
  replaced leaf box (no decoding yet). External `<link>` stylesheets resolve through a
  `pkg/resource.ResourceLoader` seam with hermetic in-memory/testdata loaders (no HTTP yet). This is
  the second landed slice of the HTML reflow frontend (sub-project 2); layout + paint of the box tree
  is next. See `docs/superpowers/specs/2026-06-23-html-box-generation-design.md`.
```

- [ ] **Step 3: Update the TODO roadmap entry for HTML**

In `CLAUDE.md`'s TODO section, item 6 ("New reflow frontends — HTML…"), update the trailing note so it
reflects that box generation has landed. Change the last sentence to:

```
The CSS parse+cascade layer (`pkg/css`) and the HTML parse + box-generation layer (`pkg/html`,
`pkg/layout/cssbox`, `pkg/layout/css`, `pkg/resource`) are the first landed slices of the HTML
frontend; the layout engine that turns the `cssbox` tree into positioned fragments and paints it comes
next.
```

- [ ] **Step 4: Verify the build still passes (docs-only change, sanity check)**

Run: `go build ./...`
Expected: no errors (CLAUDE.md is not compiled, but confirm nothing else drifted).

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: record HTML box-generation slice; two-tier reflow architecture note"
```

- [ ] **Step 6: Open the PR (when the user asks)**

Per the user's CLAUDE.md, keep the PR description short and do not credit Claude. The PR must record:
- the new dependency `golang.org/x/net/html` (BSD, pure Go) and its reason (HTML5 tokenizer + tree
  builder for the HTML frontend);
- that `pkg/css`'s `NewResolver`/cascade gained origin awareness and a `ComputeRoot` root entry point;
- that this sub-project produces no pixels (box tree only), verified by structural assertions.

Base the PR on the correct branch per the handover (this branch is stacked; rebase onto updated `main`
if PR #2 has merged).

---

## Self-review checklist (completed during plan authoring)

- **Spec coverage:** `pkg/html` owned DOM + collection (Tasks 5–7); UA sheet (Task 6); `pkg/resource`
  seam + hermetic loaders (Task 3); `pkg/layout/cssbox` rich box (Task 4); box generation incl. both
  anonymous-box fixups + whitespace + `display:none` + flex/grid-preserved + replaced `<img>` +
  `<link>` resolution (Tasks 8–9); `pkg/css` root handling + origin cascade (Task 2); error/recover +
  read-only/concurrency via `-race` (Tasks 8, 10); structural-assertion testing, no goldens/WPT
  (every test task); CLAUDE.md update + dep note (Task 11). All spec sections map to a task.
- **Placeholder scan:** no TBD/TODO; every code step shows complete code; the one intentional stub
  (`normalize` in Task 8) is explicitly created and then deleted/replaced in Task 9, with both steps
  spelled out.
- **Type consistency:** `Build(ctx, *html.Document, resource.ResourceLoader, logf)` is used
  identically in Tasks 8, 9 (tests), and 10. `cssbox.Box` fields (`Kind`, `Style`, `Children`, `Text`,
  `Replaced`, `Display`, `Formatting`, `Float`, `Position`) match across Tasks 4, 8, 9, 10.
  `css.NewResolver([]css.OriginSheet, logf)` / `css.OriginUA` / `css.OriginAuthor` / `ComputeRoot`
  match across Tasks 2, 6, 8. The `Parent() css.Node` vs `ParentElement() *Element` split is defined
  in Task 5 and consumed consistently in Tasks 7–9.
```