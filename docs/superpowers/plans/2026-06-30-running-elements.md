# `element()` Running Elements — Implementation Sub-Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use checkbox (`- [ ]`).

**Goal:** Support CSS GCPM `position: running(name)` + `content: element(name)`: a live, styled element is taken out of normal flow and re-placed (with full styling — borders, backgrounds, images, nested layout) into a `@page` margin box on every page. This is the richest running-header/footer mechanism — formatted markup in the margin band, not just text. Replaces the deferred "Task 11b" of `2026-06-30-paged-media-deferrals.md` — the owner chose to implement it.

**Architecture:** A box with `position: running(name)` is taken out of normal flow at box generation (it generates no in-flow fragment), and its source `cssbox.Box` subtree is collected by name. At layout time, each running element's subtree is laid out ONCE (a self-contained block at a provisional width) into a fragment. A `@page` margin box whose `content` is `element(name)` paints that fragment — re-placed into the margin-box rect via `translateFragment` + the existing `Fragment.AppendItems` flatten (the same painter the page content uses), so the running element keeps all its styling. The capture is per-document (one fragment per running name, shared across pages — read-only, like a `position:fixed` box).

**Tech Stack:** Go stdlib only. `pkg/css` (parse `running()` + `element()`), `pkg/layout/cssbox` (a `PosRunning` kind), `pkg/layout/css` (build-time collection, layout-time capture, margin-box placement). Golden test.

**Key facts (verified):**
- `ComputedStyle.Position` stores the raw `position` value string; `positionOf(cs)` (`pkg/layout/css/build.go:301`) maps it to a `cssbox.PositionKind` (`PosStatic/Relative/Absolute/Fixed`).
- Box generation: `generate` sets `b.Position = positionOf(cs)`; an abs/fixed box is taken out of flow.
- `@page` margin box `content` is resolved by `resolveMarginContentWithStrings` (`pkg/layout/css/marginbox.go`); `appendMarginBoxes` emits text via `appendMarginText`. The margin-box rect is `marginRect{x,y,w,h}`.
- `Fragment.AppendItems(dst)` flattens a fragment subtree to `[]layout.Item`; `translateFragment(f, dx, dy)` moves a fragment + descendants.
- A running element is out of flow, so it must NOT appear in `body.Children` (the bucketer's blocks) — exactly like an abs box is lifted out.

---

## Scope bound (what counts as "running")

- Only an element whose computed `position` is `running(name)` is a running element. It is removed from normal flow entirely (no in-flow space reserved, unlike `relative`).
- `content: element(name)` in a `@page` margin box paints the captured running element for `name`. If no running element was captured for `name`, the margin box is empty (graceful).
- The running element is laid out at the **margin-box content width** of the FIRST margin box that references it (a documented simplification — a running element referenced by margin boxes of different widths uses the first; re-layout per width is a follow-up). It repeats identically on every page (the same captured fragment), like `string()` but as a fragment.
- DEFERRED within this feature (signed sub-deferral): a running element that itself must paginate or whose content changes per page (`element(name, first|last)` position keywords); per-page re-layout at different margin widths. These degrade to "the single captured fragment on every page."

---

## File Structure

| File | Responsibility | Tasks |
|---|---|---|
| `pkg/css/cascade.go` | parse `position: running(name)` → `RunningName`; `content: element(name)` recognized | 1 |
| `pkg/layout/cssbox/box.go` | `PosRunning` kind + `RunningName` on Box | 2 |
| `pkg/layout/css/build.go` | map `running()`; collect running boxes by name; exclude from flow | 2 |
| `pkg/layout/css/runningelement.go` (new) | capture (lay out) running elements; place in a margin box | 3 |
| `pkg/layout/css/marginbox.go` | `element(name)` branch → paint the captured fragment | 4 |
| `pkg/doctaculous/pagedmedia_golden_test.go` | formatted running-header golden | 4 |
| `docs/paged-media-deferral-signoffs.md` | sub-deferral row | 4 |

---

## Task 1: Parse `position: running(name)` + recognize `element()`

**Files:**
- Modify: `pkg/css/cascade.go` (`ComputedStyle.RunningName`; the `position` arm)
- Test: `pkg/css/cascade_test.go` or a new `pkg/css/running_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/css/running_test.go`:

```go
package css

import "testing"

func TestParsePositionRunning(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "position", Value: "running(header)"})
	if cs.Position != "running" {
		t.Errorf("Position = %q, want \"running\"", cs.Position)
	}
	if cs.RunningName != "header" {
		t.Errorf("RunningName = %q, want \"header\"", cs.RunningName)
	}
	// A normal position leaves RunningName empty.
	cs2 := initialStyle()
	applyDeclaration(&cs2, Declaration{Property: "position", Value: "absolute"})
	if cs2.RunningName != "" {
		t.Errorf("absolute RunningName = %q, want empty", cs2.RunningName)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run (dangerouslyDisableSandbox: true): `go test ./pkg/css/ -run TestParsePositionRunning -v`
Expected: FAIL ("cs.RunningName undefined").

- [ ] **Step 3: Add `RunningName` + parse `running()`**

In `pkg/css/cascade.go`, add to `ComputedStyle` (near `Position`):

```go
	// RunningName is the name from `position: running(name)` (CSS GCPM): the box is
	// removed from normal flow and re-placed into a @page margin box via element(name).
	// "" when position is not running(). Not inherited.
	RunningName string
```

Find the `case "position":` arm. It currently stores recognized keywords. Extend it to recognize `running(name)`:

```go
	case "position":
		v := strings.TrimSpace(d.Value)
		if name, ok := parseRunning(v); ok {
			cs.Position = "running"
			cs.RunningName = name
		} else {
			switch v {
			case "static", "relative", "absolute", "fixed":
				cs.Position = v
				cs.RunningName = ""
			}
		}
```

Add the helper (in cascade.go or a small parse file):

```go
// parseRunning parses a `running(name)` position value, returning the name and ok=true.
// ok is false for any non-running() value.
func parseRunning(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "running(") || !strings.HasSuffix(v, ")") {
		return "", false
	}
	name := strings.TrimSpace(v[len("running(") : len(v)-1])
	if name == "" {
		return "", false
	}
	return strings.ToLower(name), true
}
```

NOTE: verify the existing `position` arm's exact shape first and merge, don't duplicate. If it currently is `switch d.Value { case "static": ... }`, replace it wholesale with the above (which also trims).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/css/ -run TestParsePositionRunning -v`
Expected: PASS.

- [ ] **Step 5: Run the css suite**

Run: `go test ./pkg/css/`
Expected: ok (RunningName defaults "", not inherited — no existing test perturbed).

- [ ] **Step 6: Commit**

```bash
gofmt -w pkg/css/cascade.go pkg/css/running_test.go
git add pkg/css/cascade.go pkg/css/running_test.go
git commit -m "feat(css): parse position: running(name) for running elements"
```

---

## Task 2: `PosRunning` kind + collect running boxes, exclude from flow

**Files:**
- Modify: `pkg/layout/cssbox/box.go` (`PosRunning` + `RunningName`)
- Modify: `pkg/layout/css/build.go` (`positionOf`; collect + exclude)
- Test: `pkg/layout/css/build_test.go` (or a new running test in pkg/layout/css)

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/running_build_test.go`:

```go
package css

import (
	"context"
	"testing"
)

func TestRunningElementExcludedFromFlow(t *testing.T) {
	src := `<html><body>
		<header style="position:running(head)">My Header</header>
		<p>Body paragraph</p>
	</body></html>`
	doc := parseHTMLForTest(t, src) // use the existing test helper that parses + builds
	root, running := buildWithRunning(t, doc)
	// The running header is NOT an in-flow child of body.
	body := lastChildBox(root) // body
	for _, c := range body.Children {
		if c.Box != nil && c.Box.RunningName == "head" {
			t.Errorf("running element should be excluded from body's in-flow children")
		}
	}
	// It WAS collected by name.
	if _, ok := running["head"]; !ok {
		t.Errorf("running element 'head' should be collected")
	}
}
```

NOTE to implementer: adapt to the actual test helpers in pkg/layout/css (there is a `buildRoot(t, src, logf)` in paginate_test.go and `BuildWithFontsAndPages`). You likely need a NEW build entry that also returns the running map (Step 3). Write the test against THAT entry. If a helper named differently exists, use it; the assertion is what matters: running box excluded from flow + collected by name.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestRunningElementExcludedFromFlow -v`
Expected: FAIL (no collection mechanism; running box still in flow).

- [ ] **Step 3: Add the kind + collection**

In `pkg/layout/cssbox/box.go`, add to the `PositionKind` const block:

```go
	// PosRunning is `position: running(name)` (CSS GCPM): the box is removed from normal
	// flow and re-placed into a @page margin box via content: element(name).
	PosRunning
```

and add to `Box`:

```go
	// RunningName is the running()-element name when Position == PosRunning ("" otherwise).
	RunningName string
```

In `pkg/layout/css/build.go`:
- extend `positionOf` to map `"running"` → `cssbox.PosRunning`.
- in `generate`, when a box's `cs.Position == "running"`, set `b.RunningName = cs.RunningName` and `b.Position = cssbox.PosRunning`; the box must be EXCLUDED from its parent's children (like display:none / out-of-flow) AND recorded in a collection keyed by name.

The collection needs to flow out of box generation. Add a `BuildWithRunning` (or extend `BuildWithFontsAndPages` to also return `map[string]*cssbox.Box`). The simplest: a `runningElements map[string]*cssbox.Box` accumulated during `generate` (passed by pointer / on a build context). A running box is generated (its subtree built) but NOT appended to its parent — instead stored in the map under its `RunningName` (last one wins if duplicated).

NOTE to implementer: examine how `generate` returns/threads state. It currently returns a `*cssbox.Box` (nil for display:none). The cleanest non-invasive approach: thread a `*map[string]*cssbox.Box` (or a small `*buildState`) through `generate`/`BuildWithFontsAndPages`, and when a child is a running element, build its subtree, store it in the map, and SKIP appending it to the parent's children. Mirror exactly how display:none children are skipped (they return nil and are not appended) — but here you keep the built subtree in the map.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/layout/css/ -run TestRunningElementExcludedFromFlow -v`
Expected: PASS.

- [ ] **Step 5: Full suite + lint**

Run: `go test ./pkg/layout/css/ ./pkg/css/ && golangci-lint run ./pkg/layout/css/ ./pkg/css/`
Expected: ok / 0 issues. (No existing doc uses running(); excluded-from-flow only triggers for running boxes, so existing trees are unchanged.)

- [ ] **Step 6: Commit**

```bash
gofmt -w pkg/layout/cssbox/box.go pkg/layout/css/build.go pkg/layout/css/running_build_test.go
git add pkg/layout/cssbox/box.go pkg/layout/css/build.go pkg/layout/css/running_build_test.go
git commit -m "feat(css): collect position:running elements out of normal flow"
```

---

## Task 3: Capture (lay out) running elements + place into a margin box

**Files:**
- Create: `pkg/layout/css/runningelement.go`
- Modify: the engine/`PagedConfig` to carry the running map + capture them
- Test: `pkg/layout/css/runningelement_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/runningelement_test.go`:

```go
package css

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func TestCaptureRunningElement(t *testing.T) {
	// A running element with text lays out into a fragment at the given width.
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, RunningName: "h"}
	box.Children = []*cssbox.Box{{Kind: cssbox.BoxText, Text: "Header"}}
	e := New(nil, nil, nil)
	frag := e.captureRunningElement(context.Background(), box, 200)
	if frag == nil {
		t.Fatalf("captureRunningElement returned nil")
	}
	if frag.W <= 0 {
		t.Errorf("captured fragment has no width")
	}
}

func TestPlaceRunningElement(t *testing.T) {
	// Placing a captured fragment in a margin rect translates it to the rect origin and
	// emits items.
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	box.Children = []*cssbox.Box{{Kind: cssbox.BoxText, Text: "X"}}
	e := New(nilFaces(t), nil, nil) // a face cache so text shapes; see note
	frag := e.captureRunningElement(context.Background(), box, 200)
	r := marginRect{x: 50, y: 10, w: 200, h: 40}
	var items []layout.Item
	items = e.placeRunningElement(items, frag, r)
	if len(items) == 0 {
		t.Errorf("placeRunningElement emitted no items")
	}
}
```

NOTE: `nilFaces`/`nil` — use a real face cache (`layoutfont.NewFaceCache()`) where glyphs must be produced, mirroring marginbox_test.go's `layoutfont.NewFaceCache()` usage. Adjust imports.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css/ -run 'TestCaptureRunningElement|TestPlaceRunningElement' -v`
Expected: FAIL ("undefined: captureRunningElement").

- [ ] **Step 3: Implement capture + placement**

Create `pkg/layout/css/runningelement.go`:

```go
package css

import (
	"context"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// captureRunningElement lays a running element's box subtree out as a self-contained
// block at width w (the margin-box content width), returning its fragment. The fragment
// is in its own local space (origin ~0,0). Returns nil if layout produces nothing.
func (e *Engine) captureRunningElement(ctx context.Context, box *cssbox.Box, w float64) *Fragment {
	// A running element is an independent block formatting context: lay it out like the
	// root of a tiny document at width w. Reuse layoutTree by wrapping it if needed, or
	// call the block layout entry directly.
	frag := e.layoutTree(ctx, wrapAsRoot(box), w)
	if frag == nil {
		return nil
	}
	// The running element is the (single) in-flow child of the synthesized root/body.
	if b := bodyFragment(frag); b != nil && len(b.Children) > 0 {
		return b.Children[0]
	}
	return frag
}

// wrapAsRoot wraps a single box as an html>body>box tree so layoutTree (which expects a
// document root) can lay it out. (If layoutTree already accepts an arbitrary block root,
// call it directly and drop this.)
func wrapAsRoot(box *cssbox.Box) *cssbox.Box {
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Children: []*cssbox.Box{box}}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Children: []*cssbox.Box{body}}
}

// placeRunningElement paints a captured running-element fragment into margin rect r: it
// clones the fragment, translates it so its top-left sits at (r.x, r.y), flattens it via
// AppendItems, and appends the items. The fragment is shared read-only across pages, so a
// per-call shallow clone (translateFragment mutates) is required.
func (e *Engine) placeRunningElement(items []layout.Item, frag *Fragment, r marginRect) []layout.Item {
	if frag == nil {
		return items
	}
	clone := cloneFragmentShallow(frag) // see note
	translateFragment(clone, r.x-clone.X, r.y-clone.Y)
	return clone.AppendItems(items)
}
```

NOTE to implementer:
- Check whether `layoutTree` accepts a bare block root; if so, skip `wrapAsRoot`. Inspect `layoutTree` and `generate`'s root expectations.
- `cloneFragmentShallow`: AppendItems + translateFragment must not corrupt the shared captured fragment (it's painted on every page). Either deep-clone the fragment subtree, OR (simpler) re-capture per page (call captureRunningElement once per page — cheap for a small header). The PRAGMATIC choice: capture ONCE, then for each page do a fresh `translateFragment` from a known baseline and AppendItems, then translate back — but that's error-prone. SAFEST: capture once and deep-clone per placement. If no deep-clone helper exists, write a minimal one that copies the fragment struct + recursively its Children/Lines slices (enough for AppendItems). Document the approach.
- Simplest robust alternative that avoids cloning: store the running element's BOX (not fragment) and call captureRunningElement fresh for each page placement (layout is idempotent and cheap). Then translateFragment+AppendItems operate on a fresh fragment each time. PREFER THIS if cloning is fiddly: `placeRunningElementBox(items, box, r, w)` that captures then places. Pick whichever is cleaner and document it.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/layout/css/ -run 'TestCaptureRunningElement|TestPlaceRunningElement' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/layout/css/runningelement.go pkg/layout/css/runningelement_test.go
git add pkg/layout/css/runningelement.go pkg/layout/css/runningelement_test.go
git commit -m "feat(css): capture + place running-element fragments in a margin box"
```

---

## Task 4: Wire `element(name)` into margin boxes + golden

**Files:**
- Modify: `pkg/layout/css/marginbox.go` (`appendMarginBoxes` → handle element())
- Modify: `pkg/layout/css/pagemodel.go` (thread the running map + captures into assembly)
- Modify: `pkg/doctaculous/html_backend.go` (carry running map from build into PagedConfig)
- Test: golden in `pkg/doctaculous/pagedmedia_golden_test.go`
- Modify: `docs/paged-media-deferral-signoffs.md`

- [ ] **Step 1: Thread the running map end-to-end**

`BuildWithFontsAndPages` (Task 2) now yields `map[string]*cssbox.Box` of running elements. Carry it into `PagedConfig` (add a `Running map[string]*cssbox.Box` field) in `pkg/doctaculous/html_backend.go`'s `htmlDocument`, and into the engine's paginate path so `appendMarginBoxes` can reach it. Add `Running map[string]*cssbox.Box` to `PagedConfig` and pass `cfg.Running` into `appendMarginBoxes` (extend its signature once more, or stash on the Engine for the paginate pass — pick the cleaner; the codebase passes geometry explicitly, so a param is consistent).

- [ ] **Step 2: Handle `element(name)` in `appendMarginBoxes`**

In `appendMarginBoxes`, before resolving text content, detect a `content: element(name)` value. If present and `running[name]` exists, place the running element fragment into the box rect (via `placeRunningElement`/`placeRunningElementBox`) instead of text. A small parser:

```go
// marginElementName returns the name from a `content: element(name)` value, or "" if the
// content is not an element() reference.
func marginElementName(content string) string {
	c := strings.TrimSpace(content)
	if !strings.HasPrefix(c, "element(") || !strings.HasSuffix(c, ")") {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(c[len("element("):len(c)-1]))
}
```

In the `appendMarginBoxes` loop, for each margin box `mb`: if `name := marginElementName(mb.Content); name != "" && running[name] != nil`, compute the box rect and place the running element; `continue` (skip the text path).

- [ ] **Step 3: Add the end-to-end golden**

Add to `pagedMediaGoldens`:

```go
	{
		// CSS GCPM running ELEMENT: a styled <div> (border + background, not just text) is
		// position:running(brand) and re-placed into the @top-center margin box on every
		// page via content: element(brand). Eyeball: a bordered, colored header band with
		// text appears centered in the top margin of BOTH pages (formatted markup, not a
		// plain string).
		name:    "running-element",
		wantPgs: 2,
		html: `<!DOCTYPE html><html><head><style>
  @page { size: 360px 240px; margin: 48px 20px; @top-center { content: element(brand) } }
  body { margin: 0 }
  .brand { position: running(brand); background: #224488; color: #fff; border: 2px solid #112244; width: 200px }
  .blk { height: 180px }
</style></head><body>
  <div class="brand">DOCTACULOUS</div>
  <div class="blk" style="background:#fdd">page one</div>
  <div class="blk" style="background:#dfd">page two</div>
</body></html>`,
	},
```

- [ ] **Step 4: Generate + EYEBALL**

Run: `go test ./pkg/doctaculous/ -run TestHTMLPagedMediaGolden -update`
Restore any re-encoded pre-existing PNG (keep only the two new ones). READ `html-running-element-p0.png` and `html-running-element-p1.png`:
- BOTH pages show, centered in the top margin band, the blue bordered "DOCTACULOUS" header band (with its border + background + white text) — i.e. a FORMATTED element, not plain text.
- The `.brand` div does NOT also appear in the body flow (it was removed from normal flow).
If the brand appears in the body instead of the margin, or shows as unstyled text, STOP and report.

- [ ] **Step 5: Confirm existing goldens unchanged + full verify**

Run: `go test ./pkg/layout/css/ ./pkg/doctaculous/ && golangci-lint run ./pkg/layout/css/ ./pkg/css/`
Expected: ok / 0 issues. Existing goldens unchanged (no doc uses running()).

- [ ] **Step 6: Sub-deferral ledger row**

Append to the "## Deferred (owner-signed)" table:

```markdown
| 4 | element(name) POSITION KEYWORDS + per-page-varying running elements | content: element(name) places the captured running element identically on every page; element(name, first|last|start) and a running element whose content varies per page are not modeled (the single captured fragment repeats). Per-margin-width re-layout also uses the first referencing box's width. | Nathan | 2026-06-30 |
```

- [ ] **Step 7: Commit**

```bash
gofmt -w pkg/layout/css/marginbox.go pkg/layout/css/pagemodel.go pkg/doctaculous/html_backend.go
git add pkg/layout/css/marginbox.go pkg/layout/css/pagemodel.go pkg/doctaculous/html_backend.go pkg/doctaculous/pagedmedia_golden_test.go pkg/doctaculous/testdata/golden/html-running-element-p0.png pkg/doctaculous/testdata/golden/html-running-element-p1.png docs/paged-media-deferral-signoffs.md
git commit -m "feat(css): content: element() — paint running elements in @page margin boxes"
```

---

## What this sub-plan deliberately does NOT do (each degrades gracefully)

- **`element(name, first|last|start)`** position keywords (the single captured fragment repeats) — signed sub-deferral.
- **Per-page-varying running elements** (content that changes per page) — the capture is once, shared.
- **Per-margin-width re-layout** (a running element referenced by margin boxes of different widths uses the first width) — documented.
- **A running element that itself overflows a margin band** (clipped by the page bitmap) — same degradation as over-tall content elsewhere.
