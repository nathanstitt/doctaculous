# SVG CSS Styling (PR 2 of 8) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add CSS styling to the SVG frontend — `<style>` sheets with real selector matching and specificity, the `style=""` attribute, `class`, and the cascade order `presentation attributes < sheet rules < inline style`.

**Architecture:** Two narrow `pkg/css` changes (case-preserving type selectors; export `ParseDeclarations`), then an SVG-local cascade in `pkg/svg` that reuses `css.Selector` matching but writes into `svg.Style` rather than `css.ComputedStyle`. A document pre-pass collects stylesheets plus an id/defs index that PRs 3–5 will extend. Spec: `docs/superpowers/specs/2026-08-25-svg-styling-design.md`.

**Tech Stack:** Go stdlib, existing `pkg/css` and `pkg/svg`. **No new dependencies.**

## Global Constraints

- Pure Go, no new dependencies. MIT-compatible only.
- Never panic on malformed input. CSS error recovery: a bad declaration drops itself and the rest of the rule still applies; a bad selector drops only its own rule.
- Unsupported constructs degrade with a **warn-once-per-document** debug log via the existing `warnOnceMsg` mechanism (`pkg/svg/svg.go:376`), reachable by callers through `WithLogf`.
- The scene graph and `Document` stay **read-only after `Parse`** (shared lock-free across the page fan-out). The index lives on `sceneBuilder`, which is discarded after `Parse`.
- `css.ComputedStyle` and `applyDeclaration` must NOT gain SVG properties.
- All exported identifiers get doc comments. `gofmt`/`goimports` clean; `go vet ./...` and `golangci-lint run` pass.
- Commits: conventional style, no Claude/AI/session mentions.
- Work on branch `feat/svg-styling` (already created, stacked on `feat/svg-support`).

## Environment notes (learned running PR 1 — save the next implementer the rediscovery)

- **`go` commands need `dangerouslyDisableSandbox: true`** on Bash calls; the Go build cache lives outside the sandbox's writable paths.
- **Run `go test ./...` unsandboxed.** Sandboxed runs produce spurious `httptest`/loopback EOF failures that are sandbox network-policy artifacts, not real failures.
- **Never create scratch `.go` files inside the repo.** Use the session scratchpad. Several PR 1 subagents leaked scratch `_test.go` files into `pkg/svg/` that had to be cleaned up; one nearly got committed.
- **A golden always matches itself on first generation.** Visually inspect every new PNG with the Read tool against the fixture's `<title>`/`<desc>`. This is how PR 1 found two real rendering bugs that all unit tests missed.

## File Structure

```
pkg/css/selector.go            MODIFY  stop lowercasing at parse; case-insensitive compare
pkg/css/dom.go                 MODIFY  Node.Tag() doc comment reflects the real contract
pkg/css/parse.go               MODIFY  export ParseDeclarations
pkg/svg/xml.go                 MODIFY  element gains parent + precomputed id/classes
pkg/svg/cssnode.go             CREATE  css.Node adapter over *element
pkg/svg/index.go               CREATE  docIndex: sheets + ids + defs, built by one pre-pass
pkg/svg/hints.go               CREATE  svgPresentationHints -> []css.Declaration
pkg/svg/cascade.go             CREATE  SVG-local cascade -> resolved property map
pkg/svg/style.go               MODIFY  apply() takes the cascade context; attr closure redirected
pkg/svg/svg.go                 MODIFY  build the index; defs walked-not-emitted; style no longer "unsupported"
pkg/svg/length.go              MODIFY  retarget the stale "PR 2" em/ex comment to the text PR
testdata/svg/resvg/            ADD     structure/style/** tranche + README section
pkg/doctaculous/testdata/golden/svg-resvg/  ADD  goldens for the new tranche
```

Each new `pkg/svg` file gets a sibling `_test.go`.

---

### Task 1: `pkg/css` case-preserving type selectors

**Files:**
- Modify: `pkg/css/selector.go` (`parseSimple` ~line 281; `simpleSelector.matches` ~line 112)
- Modify: `pkg/css/dom.go` (`Node.Tag()` doc comment ~line 9)
- Test: `pkg/css/selector_test.go` (append)

**Interfaces:**
- Produces: type selectors retain their authored case; matching is case-insensitive. No signature changes.

**Why this is safe (verify, don't assume):** HTML nodes always report a lowercase `Tag()` because `x/net/html` lowercases at parse time (`pkg/html/html.go:55`). No `pkg/css` test and no CSS fixture in `testdata/` depends on the parse-time lowercasing. Confirm both before changing anything.

- [ ] **Step 1: Write the failing test** (append to `pkg/css/selector_test.go`)

```go
func TestSelectorTypeCasePreserved(t *testing.T) {
	// A camelCase type selector must match a node reporting that exact name
	// (SVG: linearGradient, clipPath, feGaussianBlur are case-sensitive names).
	sheet := Parse(`linearGradient { fill: red }`)
	if len(sheet.Rules) != 1 || len(sheet.Rules[0].Selectors) != 1 {
		t.Fatalf("parse produced %d rules", len(sheet.Rules))
	}
	sel := sheet.Rules[0].Selectors[0]
	if !sel.Matches(&fakeNode{tag: "linearGradient"}) {
		t.Error("linearGradient selector did not match a linearGradient node")
	}
	// HTML stays case-insensitive: authored case must not matter against a
	// lowercase-reporting HTML node.
	for _, src := range []string{"DIV { color: red }", "Div { color: red }", "div { color: red }"} {
		s := Parse(src)
		if !s.Rules[0].Selectors[0].Matches(&fakeNode{tag: "div"}) {
			t.Errorf("%q did not match an html div node", src)
		}
	}
	// And a lowercase selector still matches a camelCase node (no SVG element
	// names differ only by case, so this collision is harmless and documented).
	lower := Parse(`lineargradient { fill: red }`)
	if !lower.Rules[0].Selectors[0].Matches(&fakeNode{tag: "linearGradient"}) {
		t.Error("case-insensitive match failed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css -run TestSelectorTypeCasePreserved -v`
Expected: FAIL — the camelCase node is not matched, because `parseSimple` lowercased the selector to `lineargradient` while the node reports `linearGradient`.

- [ ] **Step 3: Implement**

In `parseSimple`, replace `ss.tag = strings.ToLower(f[:i])` with `ss.tag = f[:i]` (keep the immediately-following `if ss.tag == "*"` universal-selector handling unchanged).

In `simpleSelector.matches`, replace the exact comparison

```go
	if ss.tag != "" && ss.tag != n.Tag() {
		return false
	}
```

with a case-insensitive one:

```go
	// Type selectors match case-insensitively. HTML tags are already lowercased
	// by the parser, so authored case never mattered there; SVG reports names
	// verbatim (linearGradient, clipPath), and no two SVG element names differ
	// only by case, so folding is safe for both formats.
	if ss.tag != "" && !strings.EqualFold(ss.tag, n.Tag()) {
		return false
	}
```

Update `Node.Tag()`'s doc comment in `pkg/css/dom.go` from "the lowercased element name" to describe the real contract — the element name as the host format reports it (lowercase for HTML, verbatim for SVG), matched case-insensitively.

Check whether `strings` is still used in `selector.go` after removing the `ToLower` call; it is (for `EqualFold` and elsewhere), but confirm rather than assume.

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/css` (WHOLE package — the selector and cascade suites are the regression net here)
Expected: PASS, all of it.

Then run the HTML golden suites, which are the real proof that shared behavior is unchanged:
`go test -count=1 ./pkg/doctaculous -run 'Golden|Showcase'` → PASS, and `git status` must show no `.png` modified.

- [ ] **Step 5: Commit**

```bash
git add pkg/css && git commit -m "feat(css): preserve type selector case, match case-insensitively"
```

---

### Task 2: Export `css.ParseDeclarations`

**Files:**
- Modify: `pkg/css/parse.go` (`parseDeclarations` ~line 170) and its call sites
- Test: `pkg/css/parse_test.go` (append)

**Interfaces:**
- Produces: `func ParseDeclarations(body string) []Declaration` — parses a declaration list (the body of a rule, or a `style=""` attribute value) into declarations, honoring `!important`.

- [ ] **Step 1: Write the failing test**

```go
func TestParseDeclarationsExported(t *testing.T) {
	decls := ParseDeclarations("fill: red; stroke-width: 2px ; bogus ; opacity: .5 !important")
	if len(decls) != 3 {
		t.Fatalf("got %d declarations, want 3 (the malformed one is dropped): %+v", len(decls), decls)
	}
	if decls[0].Property != "fill" || decls[0].Value != "red" {
		t.Errorf("decl 0 = %+v", decls[0])
	}
	if decls[1].Property != "stroke-width" || decls[1].Value != "2px" {
		t.Errorf("decl 1 = %+v", decls[1])
	}
	if decls[2].Property != "opacity" || !decls[2].Important {
		t.Errorf("decl 2 = %+v, want opacity important", decls[2])
	}
	if got := ParseDeclarations(""); len(got) != 0 {
		t.Errorf("empty = %+v", got)
	}
}
```

- [ ] **Step 2: Run** — `go test ./pkg/css -run TestParseDeclarationsExported -v` → FAIL, `undefined: ParseDeclarations`
- [ ] **Step 3: Implement** — rename `parseDeclarations` to `ParseDeclarations`, give it an exported-quality doc comment describing what it parses and that malformed declarations are dropped per CSS error recovery, and update every internal call site (grep `parseDeclarations(` — there are call sites in `parse.go` and `cascade.go`).
- [ ] **Step 4: Run** — `go test ./pkg/css` → PASS (whole package)
- [ ] **Step 5: Commit** — `git add pkg/css && git commit -m "feat(css): export ParseDeclarations for non-HTML frontends"`

---

### Task 3: `element` gains parent, id, and classes

**Files:**
- Modify: `pkg/svg/xml.go` (the `element` struct ~line 34; `buildAttrs` ~line 152; the `EndElement` branch ~line 116)
- Test: `pkg/svg/xml_test.go` (append)

**Interfaces:**
- Produces: `element` gains `parent *element`, `id string`, `classes []string`. `parent` is backfilled as elements are popped; `id`/`classes` are precomputed from `attrs` when the element is built (`id` from `attrs["id"]`; `classes` from `attrs["class"]` split on whitespace, nil when absent).
- Consumed by: Task 4's `css.Node` adapter (needs upward traversal for descendant combinators).

- [ ] **Step 1: Write the failing test**

```go
func TestElementParentAndClasses(t *testing.T) {
	root, err := parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg" id="root">
	  <g class="a  b"><rect id="r" class="c"/></g>
	</svg>`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if root.parent != nil {
		t.Error("root parent should be nil")
	}
	if root.id != "root" {
		t.Errorf("root id = %q", root.id)
	}
	g := root.kids[0]
	if g.parent != root {
		t.Error("g.parent != root")
	}
	if !reflect.DeepEqual(g.classes, []string{"a", "b"}) {
		t.Errorf("g classes = %v, want [a b] (split on runs of whitespace)", g.classes)
	}
	rect := g.kids[0]
	if rect.parent != g || rect.parent.parent != root {
		t.Error("rect parent chain broken")
	}
	if rect.id != "r" || !reflect.DeepEqual(rect.classes, []string{"c"}) {
		t.Errorf("rect id=%q classes=%v", rect.id, rect.classes)
	}
	// No id/class attributes: zero values, not empty non-nil slices.
	if len(root.kids) > 0 && root.kids[0].kids[0].id == "" && root.kids[0].kids[0].classes == nil {
		_ = 0 // structure asserted above; this documents the nil-vs-empty contract
	}
	plain, _ := parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`), nil)
	if r := plain.kids[0]; r.id != "" || r.classes != nil {
		t.Errorf("attribute-less element: id=%q classes=%v, want \"\" and nil", r.id, r.classes)
	}
}
```

- [ ] **Step 2: Run** — `go test ./pkg/svg -run TestElementParentAndClasses -v` → FAIL
- [ ] **Step 3: Implement.** Add the three fields with doc comments. Set `id`/`classes` where the element is constructed from its attributes. Set `parent` where a completed element is attached to its parent — note the parser has TWO attachment points (the normal `EndElement` pop, and the truncation `unwind` path that attaches partial children); set `parent` in both, or in a single shared helper, so a truncated document still has a coherent parent chain. Preserve the existing depth cap and truncation behavior exactly.
- [ ] **Step 4: Run** — `go test ./pkg/svg` (whole package; the existing XML tests are the regression net) → PASS
- [ ] **Step 5: Commit** — `git add pkg/svg && git commit -m "feat(svg): retain parent, id, and class on parsed elements"`

---

### Task 4: `css.Node` adapter (`pkg/svg/cssnode.go`)

**Files:**
- Create: `pkg/svg/cssnode.go`, `pkg/svg/cssnode_test.go`

**Interfaces:**
- Consumes: `element` with `parent`/`id`/`classes` (Task 3).
- Produces: `type cssNode struct{ el *element }` implementing `css.Node`:
  - `Tag() string` → `el.local` VERBATIM (case-sensitive; Task 1 made the matcher fold case).
  - `ID() string` → `el.id`
  - `Classes() []string` → `el.classes`
  - `Parent() css.Node` → the parent's adapter, or a **true nil interface** at the root.
  - `Attr(key string) (string, bool)` → `el.attrs[key]`, no lowercasing (SVG attribute names are case-sensitive: `viewBox`, `gradientUnits`).
  - Include the compile-time assertion `var _ css.Node = (*cssNode)(nil)`, mirroring `pkg/css/dom_test.go:26`'s idiom.

**The nil-interface trap:** `Selector.Matches` walks `n.Parent()` until it is nil. Returning a `(*cssNode)(nil)` typed pointer produces a NON-nil interface and infinite-loops or nil-derefs. `Parent()` must return an untyped `nil` when `el.parent == nil`. The test below asserts this directly.

- [ ] **Step 1: Write the failing test**

```go
func TestCSSNodeAdapter(t *testing.T) {
	root, _ := parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg">
	  <g class="wrap"><linearGradient id="g1" gradientUnits="userSpaceOnUse"/></g>
	</svg>`), nil)
	g := root.kids[0]
	lg := g.kids[0]

	n := &cssNode{el: lg}
	if n.Tag() != "linearGradient" {
		t.Errorf("Tag() = %q, want verbatim camelCase", n.Tag())
	}
	if n.ID() != "g1" {
		t.Errorf("ID() = %q", n.ID())
	}
	if v, ok := n.Attr("gradientUnits"); !ok || v != "userSpaceOnUse" {
		t.Errorf("Attr(gradientUnits) = %q,%v (attribute names are case-sensitive)", v, ok)
	}
	if _, ok := n.Attr("gradientunits"); ok {
		t.Error("Attr matched a lowercased attribute name; SVG attrs are case-sensitive")
	}

	// Parent chain, and the root MUST return an untyped nil interface.
	p := n.Parent()
	if p == nil || p.Tag() != "g" {
		t.Fatalf("Parent() = %v", p)
	}
	if !reflect.DeepEqual(p.Classes(), []string{"wrap"}) {
		t.Errorf("parent classes = %v", p.Classes())
	}
	gp := p.Parent()
	if gp == nil || gp.Tag() != "svg" {
		t.Fatalf("grandparent = %v", gp)
	}
	if root := gp.Parent(); root != nil {
		t.Errorf("root Parent() = %#v, want a nil interface (a typed nil pointer breaks Matches)", root)
	}

	// Selector matching works end to end through the adapter.
	sheet := css.Parse(`g linearGradient { fill: red }`)
	if !sheet.Rules[0].Selectors[0].Matches(n) {
		t.Error("descendant selector did not match through the adapter")
	}
}
```

- [ ] **Step 2: Run** → FAIL
- [ ] **Step 3: Implement** per the interface block. Keep it small; no caching (the scene builder is single-threaded per `Parse`, but the adapter must not introduce mutable state that could outlive it).
- [ ] **Step 4: Run** — `go test ./pkg/svg` → PASS
- [ ] **Step 5: Commit** — `git add pkg/svg && git commit -m "feat(svg): css.Node adapter over parsed elements"`

---

### Task 5: Document index pre-pass (`pkg/svg/index.go`)

**Files:**
- Create: `pkg/svg/index.go`, `pkg/svg/index_test.go`

**Interfaces:**
- Produces:

```go
// docIndex is the whole-document information the scene walk needs up front:
// author stylesheets (which may appear anywhere, including after the elements
// they style), and the id/defs tables that url(#...) references resolve
// through. Built by one pre-order walk before scene building; owned by
// sceneBuilder and discarded when Parse returns.
type docIndex struct {
	sheets []css.Stylesheet   // document order
	ids    map[string]*element
	defs   map[string]*element
}

func buildIndex(root *element, warn func(key, msg string)) *docIndex
```

- `sheets`: every `<style>` element in the SVG namespace, parsed from its text content, in document order. `type` handling: absent, empty, or `text/css` (case-insensitive, parameters ignored) is a sheet; anything else is skipped with a warn-once. An `@import` in the text is skipped with a warn-once (`pkg/css` does not resolve imports; PR 2 does not add a loader).
- `ids`: every SVG-namespace element with a non-empty `id`; FIRST occurrence wins, a duplicate logs warn-once.
- `defs`: elements that are descendants of a `<defs>` and carry an id, keyed by id, so PRs 3–5 resolve references without re-walking. (An element can appear in both maps; that is fine and intentional.)
- The walk descends into `<defs>` (unlike the scene walk) and into every SVG-namespace element regardless of `display`, because a `<style>` inside a hidden subtree still applies.
- `warn` is the injection point for `sceneBuilder.warnOnceMsg`, keeping `buildIndex` testable without a builder.

- [ ] **Step 1: Write the failing test**

```go
func TestBuildIndex(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg">
	  <rect id="early" width="1" height="1"/>
	  <defs>
	    <style>.a { fill: red }</style>
	    <rect id="indefs" width="1" height="1"/>
	  </defs>
	  <style type="text/css">.b { fill: blue }</style>
	  <style type="text/nonsense">.c { fill: lime }</style>
	  <g id="early"/>
	</svg>`)
	root, err := parseXML(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	var warns []string
	idx := buildIndex(root, func(key, msg string) { warns = append(warns, key) })

	// Two usable sheets, in document order; the bad-type one is skipped.
	if len(idx.sheets) != 2 {
		t.Fatalf("sheets = %d, want 2 (defs sheet + text/css sheet)", len(idx.sheets))
	}
	if len(idx.sheets[0].Rules) != 1 || idx.sheets[0].Rules[0].Declarations[0].Value != "red" {
		t.Errorf("first sheet should be the one inside <defs>: %+v", idx.sheets[0].Rules)
	}
	if idx.sheets[1].Rules[0].Declarations[0].Value != "blue" {
		t.Errorf("second sheet wrong: %+v", idx.sheets[1].Rules)
	}

	// ids: first occurrence wins, duplicate warns.
	if idx.ids["early"] == nil || idx.ids["early"].local != "rect" {
		t.Errorf("id 'early' should resolve to the first (rect), got %v", idx.ids["early"])
	}
	if idx.ids["indefs"] == nil {
		t.Error("ids must include elements inside <defs>")
	}
	if idx.defs["indefs"] == nil {
		t.Error("defs table missing the defs child")
	}
	if idx.defs["early"] != nil {
		t.Error("defs table must contain only <defs> descendants")
	}
	if len(warns) < 2 {
		t.Errorf("warns = %v, want at least the bad style type and the duplicate id", warns)
	}
}
```

- [ ] **Step 2: Run** → FAIL
- [ ] **Step 3: Implement.** Read `pkg/svg/svg.go`'s existing `unsupportedElements`/`skippedElements` tables and `warnOnceMsg` before writing, and match their voice. Parse sheet text with `css.Parse`. Detect `@import` by scanning the raw text before parsing (`pkg/css` drops unknown at-rules silently).
- [ ] **Step 4: Run** — `go test ./pkg/svg` → PASS
- [ ] **Step 5: Commit** — `git add pkg/svg && git commit -m "feat(svg): document index of stylesheets, ids, and defs"`

---

### Task 6: SVG presentation hints (`pkg/svg/hints.go`)

**Files:**
- Create: `pkg/svg/hints.go`, `pkg/svg/hints_test.go`

**Interfaces:**
- Produces: `func svgPresentationHints(el *element) []css.Declaration` — every SVG presentation attribute present on `el`, as a `css.Declaration` with `Important: false`. The property name is the attribute name verbatim; the value is the attribute value verbatim (parsing stays in the appliers, exactly as it is today).
- The attribute set is precisely the properties `Style.apply` already handles: `fill`, `fill-opacity`, `fill-rule`, `stroke`, `stroke-opacity`, `stroke-width`, `stroke-linecap`, `stroke-linejoin`, `stroke-miterlimit`, `stroke-dasharray`, `stroke-dashoffset`, `color`, `opacity`, `display`, `visibility`. Define it as one package-level slice/set with a comment tying it to the applier list, so the two cannot silently drift.
- The `style` and `class` attributes are NOT hints (they are the inline-style and selector inputs respectively).

- [ ] **Step 1: Write the failing test**

```go
func TestSVGPresentationHints(t *testing.T) {
	el := &element{attrs: map[string]string{
		"fill": "red", "stroke-width": "2", "class": "x", "style": "fill:blue",
		"id": "n", "d": "M0 0", "width": "10",
	}}
	got := svgPresentationHints(el)
	byProp := map[string]string{}
	for _, d := range got {
		if d.Important {
			t.Errorf("hint %q must not be !important", d.Property)
		}
		byProp[d.Property] = d.Value
	}
	if byProp["fill"] != "red" || byProp["stroke-width"] != "2" {
		t.Errorf("hints = %v", byProp)
	}
	for _, notHint := range []string{"class", "style", "id", "d", "width"} {
		if _, ok := byProp[notHint]; ok {
			t.Errorf("%q must not be a presentation hint", notHint)
		}
	}
	if len(svgPresentationHints(&element{})) != 0 {
		t.Error("no attributes should yield no hints")
	}
	if svgPresentationHints(nil) != nil {
		t.Error("nil element must not panic and should yield nil")
	}
}
```

- [ ] **Step 2: Run** → FAIL
- [ ] **Step 3: Implement.** Read `pkg/css/hints.go` first to match its structure and doc-comment voice — this is the SVG sibling of that file and should read like it.
- [ ] **Step 4: Run** — `go test ./pkg/svg` → PASS
- [ ] **Step 5: Commit** — `git add pkg/svg && git commit -m "feat(svg): presentation attributes as cascade declarations"`

---

### Task 7: The cascade (`pkg/svg/cascade.go`)

**Files:**
- Create: `pkg/svg/cascade.go`, `pkg/svg/cascade_test.go`

**Interfaces:**
- Consumes: `docIndex.sheets`, `cssNode`, `svgPresentationHints`, `css.ParseDeclarations`, `css.Selector.Matches`/`.Specificity()`/`Specificity.Less`.
- Produces:

```go
// cascadeCtx carries the per-document styling inputs through the scene walk.
// A nil ctx (or one with no sheets) makes resolve fall back to presentation
// attributes alone, which is exactly PR 1's behavior.
type cascadeCtx struct {
	idx  *docIndex
	logf func(string, ...any)
}

// resolve computes the winning value for every styling property on el,
// applying the SVG cascade: presentation attributes, then author sheet rules
// by (importance, specificity, source order), then the style="" attribute.
// The returned lookup is what Style.apply reads instead of raw attributes.
func (c *cascadeCtx) resolve(el *element) func(name string) (string, bool)
```

**Ordering rules (this is the heart of the task):**
- Normal declarations rank: presentation hint (0) < author sheet rule (1) < inline `style=""` (2).
- Important declarations all outrank every normal one. Among important: author sheet important < inline important. (There is no UA sheet in the SVG path, so `pkg/css`'s UA-important-wins-everything rule has no analogue here — note that in a comment.)
- Within the same rank, higher specificity wins; ties break by source order (later wins). Sheets are in document order and rules within a sheet in source order.
- Selectors that `pkg/css` could not parse never reach here (they are dropped at parse time and can never mis-match), but log warn-once when a rule contributes zero selectors so an author sees that their `>` or `[attr]` rule was ignored.

- [ ] **Step 1: Write the failing test**

```go
func TestCascadeOrdering(t *testing.T) {
	resolveFor := func(src string, want map[string]string) {
		t.Helper()
		root, err := parseXML([]byte(src), nil)
		if err != nil {
			t.Fatal(err)
		}
		idx := buildIndex(root, func(string, string) {})
		ctx := &cascadeCtx{idx: idx}
		// The <rect> under test is always the last child of the root.
		var target *element
		for _, k := range root.kids {
			if k.local == "rect" {
				target = k
			}
		}
		if target == nil {
			t.Fatalf("no rect in %q", src)
		}
		lookup := ctx.resolve(target)
		for prop, exp := range want {
			got, ok := lookup(prop)
			if !ok || got != exp {
				t.Errorf("%s = %q,%v; want %q\nsrc: %s", prop, got, ok, exp, src)
			}
		}
	}
	const ns = `xmlns="http://www.w3.org/2000/svg"`

	// Sheet rule beats a presentation attribute.
	resolveFor(`<svg `+ns+`><style>rect{fill:blue}</style><rect fill="red"/></svg>`,
		map[string]string{"fill": "blue"})

	// Inline style beats a sheet rule.
	resolveFor(`<svg `+ns+`><style>rect{fill:blue}</style><rect fill="red" style="fill:lime"/></svg>`,
		map[string]string{"fill": "lime"})

	// Presentation attribute survives when nothing else sets that property.
	resolveFor(`<svg `+ns+`><style>rect{stroke:black}</style><rect fill="red"/></svg>`,
		map[string]string{"fill": "red", "stroke": "black"})

	// Specificity: .cls beats bare type even though the type rule is later.
	resolveFor(`<svg `+ns+`><style>.c{fill:lime} rect{fill:blue}</style><rect class="c"/></svg>`,
		map[string]string{"fill": "lime"})

	// Source order breaks a specificity tie: later wins.
	resolveFor(`<svg `+ns+`><style>rect{fill:blue} rect{fill:lime}</style><rect/></svg>`,
		map[string]string{"fill": "lime"})

	// !important in a sheet beats a normal inline style.
	resolveFor(`<svg `+ns+`><style>rect{fill:blue!important}</style><rect style="fill:lime"/></svg>`,
		map[string]string{"fill": "blue"})

	// Inline !important beats sheet !important.
	resolveFor(`<svg `+ns+`><style>rect{fill:blue!important}</style><rect style="fill:lime!important"/></svg>`,
		map[string]string{"fill": "lime"})

	// A descendant selector matches through the parent chain.
	resolveFor(`<svg `+ns+`><style>svg rect{fill:lime}</style><rect fill="red"/></svg>`,
		map[string]string{"fill": "lime"})

	// id beats class.
	resolveFor(`<svg `+ns+`><style>.c{fill:blue} #r{fill:lime}</style><rect id="r" class="c"/></svg>`,
		map[string]string{"fill": "lime"})
}

func TestCascadeNilContextFallsBackToAttributes(t *testing.T) {
	root, _ := parseXML([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="red"/></svg>`), nil)
	var ctx *cascadeCtx
	lookup := ctx.resolve(root.kids[0])
	if v, ok := lookup("fill"); !ok || v != "red" {
		t.Errorf("nil ctx must fall back to presentation attributes; got %q,%v", v, ok)
	}
}
```

- [ ] **Step 2: Run** → FAIL
- [ ] **Step 3: Implement.** Read `pkg/css/cascade.go:290-406` (`Compute`) first — it is the reference implementation for the ordering, and the two rank functions at `:341-360` are the shape to mirror (adapted: no UA origin here). Keep `resolve` allocation-light; it runs once per element.
- [ ] **Step 4: Run** — `go test ./pkg/svg` → PASS
- [ ] **Step 5: Commit** — `git add pkg/svg && git commit -m "feat(svg): CSS cascade over sheets, hints, and inline style"`

---

### Task 8: Wire the cascade into `Style.apply` and the scene walk

**Files:**
- Modify: `pkg/svg/style.go` (`apply` ~line 75)
- Modify: `pkg/svg/svg.go` (`Parse` ~line 87; `sceneBuilder` ~line 241; `buildNode` ~line 278; the `style`/`defs` table entries ~lines 192, 219)
- Modify: `pkg/svg/style_test.go` (9 `apply` call sites)
- Test: `pkg/svg/svg_test.go` (append end-to-end cases)

**Interfaces:**
- Consumes: everything above.
- Produces: `func (parent Style) apply(el *element, ctx *cascadeCtx) Style` — `logf` moves onto `ctx`. The `attr` closure becomes `ctx.resolve(el)` (with the nil-ctx fallback preserving PR 1 behavior). **The twelve `applyXxx` helpers and the fixed resolution order — `color` first so `currentColor` sees the element's own color — are unchanged.**
- `sceneBuilder` gains an `idx *docIndex` field; `Parse` builds the index before `buildGroup`.
- `"style"` is REMOVED from `unsupportedElements` (it is now consumed by the index) and must not be emitted into the scene — it draws nothing.
- `"defs"` stays out of the scene but is now walked by the pre-pass; update its comment to state the walked-not-emitted split explicitly.
- Add a test helper to `style_test.go` so future signature churn is one line:

```go
// applyAttrs applies attrs to base with no stylesheet context (presentation
// attributes only), the shape every pre-cascade test used.
func applyAttrs(base Style, attrs map[string]string) Style {
	return base.apply(&element{attrs: attrs}, nil)
}
```

- [ ] **Step 1: Write the failing test** (append to `pkg/svg/svg_test.go`)

```go
func TestStylesheetReachesTheScene(t *testing.T) {
	const ns = `xmlns="http://www.w3.org/2000/svg"`
	src := []byte(`<svg ` + ns + ` width="20" height="20">
	  <style>.hot { fill: #00ff00 } rect { stroke-width: 3 }</style>
	  <rect class="hot" width="10" height="10" fill="red" stroke="blue"/>
	</svg>`)
	doc, err := Parse(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	if len(root.Kids) != 1 {
		t.Fatalf("root kids = %d (the <style> element must not paint)", len(root.Kids))
	}
	sh, ok := root.Kids[0].(*Shape)
	if !ok {
		t.Fatalf("kid = %#v", root.Kids[0])
	}
	fp, okf := sh.Style.FillPaint()
	if !okf || fp.Color != (color.RGBA{0, 255, 0, 255}) {
		t.Errorf("fill = %+v, want the stylesheet's green over the attribute's red", fp.Color)
	}
	sp, oks := sh.Style.StrokePaint()
	if !oks || sp.Width != 3 {
		t.Errorf("stroke width = %v, want 3 from the sheet", sp.Width)
	}
	if sp.Color != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("stroke color = %+v, want the attribute's blue (sheet did not set it)", sp.Color)
	}
}

func TestStyleElementNoLongerLogsUnsupported(t *testing.T) {
	var logs []string
	_, err := Parse([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><style>rect{fill:red}</style><rect width="1" height="1"/></svg>`),
		func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs {
		if strings.Contains(l, "<style>") && strings.Contains(l, "not yet supported") {
			t.Errorf("style is supported now but still logs unsupported: %q", l)
		}
	}
}
```

- [ ] **Step 2: Run** — `go test ./pkg/svg -run 'Stylesheet|StyleElement' -v` → FAIL
- [ ] **Step 3: Implement.** Change `apply`'s signature, redirect the closure, thread `ctx` through `buildGroup`/`buildNode`/`buildShape`, build the index in `Parse`, and update the two element tables. Update the 9 `style_test.go` call sites to use the new helper. **The `display:none` short-circuit must still happen before recursion** (`svg.go:266-272` explains why) — and now CSS `display:none` from a sheet can reach it too, which is correct.
- [ ] **Step 4: Run** — `go test ./pkg/svg/...` (both packages) and `go test -race ./pkg/svg/...` → PASS. Then `go test ./...` unsandboxed → PASS, including all SVG goldens (PR 1's 148 fixtures must render identically; none of them use CSS, so any diff means the fallback path regressed).
- [ ] **Step 5: Commit** — `git add pkg/svg && git commit -m "feat(svg): resolve style through the CSS cascade"`

---

### Task 9: Retarget the stale `em`/`ex` comment

**Files:**
- Modify: `pkg/svg/length.go` (~line 87)

**Interfaces:** none — comment only.

- [ ] **Step 1: Read** `pkg/svg/length.go`'s `em`/`ex` handling. The comment promises real resolution "until the style cascade lands (PR 2)". PR 2 lands the cascade but deliberately does NOT add `font-size` to `Style` (that is a text-PR concern), so the comment is now misleading.
- [ ] **Step 2: Implement** — reword to state the actual situation: `em`/`ex` use the UA default font metrics (16px / 8px) because `font-size` is not yet part of the resolved style; real resolution arrives with SVG text support. Do not change behavior.
- [ ] **Step 3: Run** — `go test ./pkg/svg` → PASS (no behavior change)
- [ ] **Step 4: Commit** — `git add pkg/svg && git commit -m "docs(svg): retarget the em/ex resolution note to the text slice"`

---

### Task 10: resvg corpus tranche + goldens

**Files:**
- Add: `testdata/svg/resvg/structure/style/**` (and any class/style permutations from other areas)
- Modify: `testdata/svg/resvg/README.md`
- Add: `pkg/doctaculous/testdata/golden/svg-resvg/structure/style/**.png`

**Interfaces:** none — fixtures and goldens. The existing `TestSVGResvgGolden` sweep picks up new files automatically.

- [ ] **Step 1: Clone the upstream suite** into the scratchpad (NOT the repo):

```bash
git clone --depth 1 https://github.com/RazrFalcon/resvg-test-suite "$SCRATCH/resvg-suite"
git -C "$SCRATCH/resvg-suite" rev-parse HEAD   # must match the README's pinned commit
```

The README pins `d8e064337faf01bc5a9579187a56dbdbe3eacc72`. If HEAD differs, still vendor from the PINNED commit (`git checkout <hash>`) so the corpus stays coherent.

- [ ] **Step 2: Curate.** From `tests/structure/style/**` plus class/`style=` permutations elsewhere, copy ONLY files whose every feature PR 2 now supports. **Inspect each file's contents** — grep is a first filter, not the decision.
  - ELIGIBLE: `<style>` elements (with/without `type`), `style=""` attributes, `class`, type/class/id/descendant/grouping selectors, `!important`, specificity and source-order cases, multiple sheets, sheets inside `<defs>`, CDATA-wrapped CSS.
  - EXCLUDE (and record the reason): `@import` (no loader), attribute selectors and `>`/`+`/`~` (unsupported, fail safe), `@media` beyond type-only, `:hover` and other dynamic pseudos, and anything pulling in a not-yet-shipped feature (gradients, `<use>`, text, filters, clip/mask, `<image>`).
  - Target 25–60 files. Preserve the upstream-relative path structure.
- [ ] **Step 3: Generate goldens** — `go test ./pkg/doctaculous -run TestSVGResvgGolden -update`
- [ ] **Step 4: EYEBALL EVERY NEW GOLDEN.** Use the Read tool to view each new PNG and compare it against its source SVG's `<title>`/`<desc>` intent. A golden always matches itself on first generation, so passing proves nothing. PR 1's identical step found two real rendering bugs. If a golden looks wrong: (a) fix the genuine bug, (b) remove the file if it turns out to use an unsupported feature you missed and record the removal, or (c) STOP and report BLOCKED rather than committing a golden you believe is wrong. Report how many you inspected and what you observed — a bare "all looked fine" with no specifics is not an acceptable report.
- [ ] **Step 5: Update the README** — add a `## What shipped in this tranche (PR 2)` section, extend `## Notable exclusions` with per-item reasons, and record any file removed during the eyeball pass. Keep the existing curation-rule statement intact.
- [ ] **Step 6: Verify and commit**

```bash
go test -count=1 ./pkg/doctaculous -run TestSVGResvgGolden    # without -update
go test -race ./pkg/doctaculous -run TestSVGResvgGolden
git status   # only new fixtures/goldens + README; NO pre-existing .png modified
git add testdata/svg pkg/doctaculous && git commit -m "test(svg): resvg CSS styling tranche with goldens"
```

---

### Task 11: Full verification + docs

**Files:**
- Modify: `FEATURES.md` (the SVG entry from PR 1)

- [ ] **Step 1: Full suite** — `go test ./...` UNSANDBOXED. All pass.
- [ ] **Step 2: Race, vet, lint, format**

```bash
go test -race ./pkg/svg/... ./pkg/css/... ./pkg/layout/... ./pkg/doctaculous
go vet ./...
golangci-lint run
gofmt -l . | grep -v '^pkg/pdf/filter/jbig2'   # must print nothing
```

Any failure — including one that looks unrelated — gets diagnosed and fixed at the source. Do not skip, do not merely rerun, do not regenerate goldens.

- [ ] **Step 3: HTML regression check.** Task 1 touched shared selector code. Confirm explicitly that the HTML golden suites are byte-identical: `go test -count=1 ./pkg/doctaculous -run 'Golden|Showcase'` and `git diff --stat -- '*.png'` showing no pre-existing golden modified. State this result in the report.
- [ ] **Step 4: CLI smoke test.** Write an SVG that styles itself through all three origins (a presentation attribute, a `<style>` rule that overrides it, and an inline `style=""` that overrides that), rasterize it, and VIEW the PNG to confirm the winning color actually renders.
- [ ] **Step 5: FEATURES.md** — extend the existing SVG bullet: CSS styling now ships (`<style>` sheets, `style=""`, `class`, selectors with specificity and `!important`). Keep the not-yet list accurate — remove CSS from it, leave gradients/`<use>`/text/clip-mask/filters/`<image>`/inline-in-HTML. Match the file's existing voice.
- [ ] **Step 6: Commit** — `git add FEATURES.md && git commit -m "docs: FEATURES entry for SVG CSS styling"`
- [ ] **Step 7: Draft the PR description** into the report file (do NOT open the PR — the controller does). Keep it short per project rules: what shipped, the `pkg/css` case-sensitivity change and why it is safe for HTML, the corpus tranche, and a pointer to the spec. No AI mentions.

---

## Self-Review (performed while writing)

1. **Spec coverage:** `pkg/css` case sensitivity ✓ (T1), `ParseDeclarations` export ✓ (T2), element parent/id/classes ✓ (T3), `css.Node` adapter ✓ (T4), index pre-pass with sheets+ids+defs ✓ (T5), presentation hints ✓ (T6), cascade with ordering ✓ (T7), wiring + `apply` signature + defs walked-not-emitted ✓ (T8), stale comment ✓ (T9), corpus ✓ (T10), verification + docs ✓ (T11).
2. **Type consistency:** `cascadeCtx` is produced in T7 and consumed in T8; `docIndex` produced in T5, consumed in T7/T8; `cssNode` produced in T4, consumed in T7. `apply`'s new signature is stated identically in T7's context and T8's interface block.
3. **Known judgment calls flagged in-plan:** the nil-interface trap in `Parent()` (T4) is called out with an explicit test because it fails as an infinite loop rather than a clean error; the `svgPresentationHints` attribute list is required to be a single source shared with the applier list (T6) so the two cannot drift; PR 1's 148 CSS-free goldens double as the fallback-path regression net (T8 step 4).
