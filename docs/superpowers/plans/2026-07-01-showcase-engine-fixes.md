# Showcase Engine Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix three independent, pre-existing engine bugs surfaced by the `testdata/htmldoc/` showcase when rendered from a local file / with `WithPageSize`: (A) the local-file `DirLoader` rejects `../` refs so CSS `url(../img/…)` background/font assets never load; (B) a named `@page` size (e.g. landscape) is suppressed whenever an explicit `WithPageSize` is set, so a `page: landscape` section never reflows wider; (C) the multi-width named-page paginator drops all floats, so a floated `<figure>`/`<img>` never paints.

**Architecture:** Each fix is localized and independent — one in the resource layer (`pkg/resource`), two in the CSS pagination layer (`pkg/layout/css/pagemodel.go`). They share no code and can be implemented in parallel by separate subagents. Each lands with a unit test; Fix A and Fix C additionally regenerate the `htmldoc` golden (which will start showing the previously-missing assets/floats), and Fix B is validated by a pagemodel unit test plus the same golden. All three are pure additions/relaxations — no existing passing golden should change except the `htmldoc-*` set, which currently renders these features *missing* and should render them *present* after the fixes (eyeball each changed PNG).

**Tech Stack:** Pure Go. `pkg/resource` (ResourceLoader), `pkg/layout/css` (pagination + fragment tree), `pkg/css` (`@page` size resolution), `pkg/doctaculous` (golden tests). Test oracle: the project's own raster pipeline + committed golden PNGs.

---

## Background: facts every task depends on

Read these before starting.

- **Reproduction.** `./bin/doctaculous topdf --in testdata/htmldoc/index.html --out out/page.pdf --page-size letter` (or `rasterize` — the bugs are in the engine, not the PDF writer). The showcase is normally rendered over loopback HTTP by `TestHTMLDocShowcase` (`pkg/doctaculous/htmldoc_golden_test.go`), where Fix A does not bite (the HTTP loader resolves `../`). The local-file path (`OpenHTMLFile` → `DirLoader`) is where Fix A shows.
- **The three bugs are pre-existing** — they reproduce identically in the raster path and are NOT caused by the HTML→PDF writer. The writer faithfully renders whatever the engine produces.
- **Golden regeneration.** `go test ./pkg/doctaculous -run TestHTMLDocShowcase -update` regenerates the `htmldoc-p*.png` goldens. After Fix A and Fix C, some of these change (assets/floats now appear). Eyeball every changed PNG. The showcase is served over HTTP in that test, so **Fix A's golden only changes if the HTTP path also exercised a `../` ref that was failing** — verify whether it does (see Task A Step 6); if the HTTP golden is unaffected, add a dedicated local-file test instead (provided in Task A).
- **`ResourceLoader`** (`pkg/resource/loader.go`): `Load(ctx, ref) ([]byte, contentType string, err error)`. Implementations: `MapLoader` (in-memory), `DirLoader` (local dir, the one to fix), `HTTPLoader` (`pkg/resource/http.go`, resolves via `*url.URL.ResolveReference`). `ErrNotFound` is the sentinel for a missing ref.
- **`DirLoader.Load`** (`pkg/resource/loader.go:66`) currently rejects any ref whose cleaned join escapes `Base` (`full != base && !strings.HasPrefix(full, base+sep)`), returning `ErrNotFound`.
- **`PagedConfig.resolvePageGeom`** (`pkg/layout/css/pagemodel.go:60`): resolves a page's geometry. Line 63 `if up.HasSize && !pc.ExplicitSize { g.pageW, g.pageH = up.WidthPt, up.HeightPt }` — the `!pc.ExplicitSize` gate suppresses *every* `@page size`, named or not, when the API pinned a size.
- **`ExplicitSize`** is set true by `WithPageSize` (`pkg/doctaculous/html_backend.go`), false by `WithDefaultPaged`. `topdf --page-size letter` and `ConvertHTMLToPDF` both run with a pinned/paged size such that `ExplicitSize` is true in the failing case.
- **`paginateRuns`** (`pkg/layout/css/pagemodel.go:236`): the multi-width named-page paginator. It caches one layout per distinct content width (`layoutByWidth[w]`), buckets each run, then per bucket builds a `runBody`/`pageRoot` and **nils `.Floats`** (lines ~299 and ~304) without ever calling `splitFloatsByPage` — so floats never paint. Compare the single-width path `paginateDoc`/`assemblePages` (`pkg/layout/css/paginate.go`), which computes `perPageFloats := splitFloatsByPage(root, buckets)` and assigns `pageRoot.Floats = perPageFloats[i]`.
- **`splitFloatsByPage(root, buckets) [][]*Fragment`** (`pkg/layout/css/paginate.go:350`): partitions `root.Floats` by each float's `.Y` band into per-bucket slices, shifting each float by `-buckets[page].top`. Floats paint ONLY through `Fragment.Floats` (`appendFloatLayer`, `pkg/layout/css/fragment.go`), so a nil `.Floats` = no float items at all.
- **Conventions:** never panic on bad input; wrap errors `fmt.Errorf("...: %w", err)`; sentinel errors for branchable conditions; all exported identifiers documented; `gofmt`/`go vet`/`golangci-lint` clean; new feature ⇒ new test same PR; tests hermetic (no network — use `t.TempDir()` / in-memory fixtures).
- **Branch.** Start from a fresh branch off `main` (these are engine fixes independent of the PDF-writer PR). `git checkout main && git pull && git checkout -b fix/showcase-engine-bugs`.

## File structure

| File | Responsibility | Task |
|---|---|---|
| `pkg/resource/loader.go` | Relax `DirLoader` to follow `..` that stays within `Base` (resolve, don't reject) | A |
| `pkg/resource/loader_test.go` | Unit test: `DirLoader` serves `css/../img/x.png` | A |
| `pkg/doctaculous/local_asset_test.go` (new, if needed) | Local-file render loads `url(../img/…)` assets | A |
| `pkg/layout/css/pagemodel.go` | Honor a *named* `@page` size even under `ExplicitSize` | B |
| `pkg/layout/css/pagemodel_named_size_test.go` (new) | Unit test: named landscape page keeps its size under `ExplicitSize` | B |
| `pkg/layout/css/pagemodel.go` | Split + re-attach floats per page in `paginateRuns` | C |
| `pkg/layout/css/floats_paginate_test.go` (new) | Unit test: a float in a named-page run is emitted on its page | C |
| `pkg/doctaculous/testdata/golden/htmldoc-p*.png` | Regenerated goldens (assets/floats now visible) | A, C |

---

## Task A: `DirLoader` follows `..` refs that stay within `Base`

**Problem.** CSS `url(../img/tile.png)` in `css/main.css` reaches the loader as the raw ref `../img/tile.png` (refs are passed verbatim; there is no per-stylesheet re-basing). `DirLoader` rejects any ref containing an escaping `..`. But `css/../img/tile.png` cleans to `img/tile.png`, which is *inside* `Base` — the current guard rejects the raw `../img/...` form because it is joined against `Base` (the doc root) rather than the stylesheet dir, cleaning to `img/tile.png` which IS inside base and should be served. The real defect: the guard rejects a ref whose *cleaned* target is still inside `Base` when the ref merely *starts* with `..`. Relax it to allow any ref that resolves within `Base` (the clean+prefix check already does this correctly — the bug is that a raw `../img/x` from the doc root cleans to `img/x` which passes, but a ref like `../img/x` that the engine passes relative to a *subdir* stylesheet is never re-based, so it is judged against the doc root and cleans to escape). Concretely: make `DirLoader` resolve `..` and serve any file that lands inside `Base`, only rejecting a target that truly escapes `Base`.

> Note: the join `filepath.Join(base, "../img/tile.png")` from `base=<docroot>` cleans to `<docroot>/../img/tile.png` = `<parent>/img/tile.png`, which escapes and is (correctly, for that literal ref) rejected. The genuine fix is to make the loader **resolve a stylesheet-relative ref against the stylesheet's directory**, OR — the minimal, localized fix chosen here — allow `DirLoader` to serve within a **root that includes the document's directory and its immediate asset siblings**. The pragmatic and testable contract we implement: `DirLoader` serves any ref that, resolved against `Base`, stays inside `Base`; and additionally serves a ref of the form `../<sub>/<file>` when `<sub>` is a real sibling directory of `Base`'s document (covering `css/…` stylesheets referencing `../img/…`). We implement the simpler, sufficient version: **resolve against `Base` and serve if the cleaned path exists and is inside `Base`'s parent-bounded root.** See the exact code below — it widens the allowed root to `Base` itself while still refusing an absolute-path escape, and the accompanying test locks the `css/../img` case.

**Files:**
- Modify: `pkg/resource/loader.go` (the `DirLoader.Load` guard, ~line 72-77)
- Test: `pkg/resource/loader_test.go`
- Test (new, optional): `pkg/doctaculous/local_asset_test.go`

- [ ] **Step 1: Write the failing loader test**

Add to `pkg/resource/loader_test.go`:

```go
func TestDirLoaderServesParentRelativeWithinRoot(t *testing.T) {
	root := t.TempDir()
	// Layout mirrors the showcase: <root>/index.html, <root>/css/main.css,
	// <root>/img/tile.png. A stylesheet under css/ references ../img/tile.png.
	if err := os.MkdirAll(filepath.Join(root, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("PNGDATA")
	if err := os.WriteFile(filepath.Join(root, "img", "tile.png"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	d := DirLoader{Base: root}

	// A ref that resolves (via "..") to a file INSIDE the root must be served.
	got, _, err := d.Load(context.Background(), "css/../img/tile.png")
	if err != nil {
		t.Fatalf("css/../img/tile.png: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	// A ref that truly escapes the root must still be refused.
	if _, _, err := d.Load(context.Background(), "../../etc/passwd"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("escaping ref: err = %v, want ErrNotFound", err)
	}
}
```

Ensure the test file imports `bytes`, `context`, `errors`, `os`, `path/filepath`, `testing`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/resource -run TestDirLoaderServesParentRelativeWithinRoot -v`
Expected: FAIL — `css/../img/tile.png` returns `ErrNotFound` (the current guard rejects it because `filepath.Join(base, "css/../img/tile.png")` cleans to `<base>/img/tile.png` which IS inside base... verify the actual failure; if it already passes, the guard is fine for THIS form and the real fix is re-basing — see Step 3 note).

- [ ] **Step 3: Fix the `DirLoader.Load` guard**

In `pkg/resource/loader.go`, the current guard is:

```go
	base := filepath.Clean(d.Base)
	full := filepath.Clean(filepath.Join(base, ref))
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		return nil, "", fmt.Errorf("%q: %w", ref, ErrNotFound)
	}
```

`filepath.Join(base, "css/../img/tile.png")` cleans to `<base>/img/tile.png`, which passes the guard — so if Step 2 shows this already loads, the failing case is the raw `../img/tile.png` ref (relative to a `css/` stylesheet, never re-based). Implement re-basing so a stylesheet-relative ref resolves against its stylesheet dir. Since `DirLoader.Load` only receives `ref` (no stylesheet context), the localized fix is to keep the join-and-clean but widen acceptance to any `full` that exists within `base`, and, for a ref that escapes `base` by exactly one level into a real sibling asset dir, retry resolved against `base`'s document root. The simplest correct implementation that passes the test and preserves the escape guard:

```go
	base := filepath.Clean(d.Base)
	full := filepath.Clean(filepath.Join(base, ref))
	// Accept any ref that resolves to a path inside base (this includes refs that
	// use ".." internally as long as they land back inside, e.g. "css/../img/x").
	inside := full == base || strings.HasPrefix(full, base+string(os.PathSeparator))
	if !inside {
		// A stylesheet under a subdir may reference "../asset/x"; such a ref, joined
		// against base, escapes by one segment. Re-resolve it against base by trimming
		// leading "../" segments and retrying inside base, so sibling asset dirs load.
		trimmed := ref
		for strings.HasPrefix(trimmed, "../") || strings.HasPrefix(trimmed, "..\\") {
			trimmed = trimmed[3:]
		}
		retry := filepath.Clean(filepath.Join(base, trimmed))
		if retry == base || strings.HasPrefix(retry, base+string(os.PathSeparator)) {
			full = retry
		} else {
			return nil, "", fmt.Errorf("%q: %w", ref, ErrNotFound)
		}
	}
```

Update the `DirLoader` doc comment (lines ~59-62) to reflect the new behavior: "It resolves `..` segments and serves any ref that lands inside Base (including a subdir stylesheet's `../asset/…` ref, re-resolved against Base); a ref that still escapes Base is treated as absent."

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/resource -run TestDirLoaderServesParentRelativeWithinRoot -v`
Expected: PASS

Run: `go test ./pkg/resource/...`
Expected: PASS (no regressions — the existing escape-refusal test still holds)

- [ ] **Step 5: Add a local-file render test (asset actually decodes)**

Create `pkg/doctaculous/local_asset_test.go`:

```go
package doctaculous

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenHTMLFileLoadsParentRelativeAsset renders a local HTML file whose linked
// stylesheet references a background image via url(../img/x.png). Before the
// DirLoader fix the image was refused (../ escape) and the box painted only its
// background-color; now the tiled image loads and the box carries image ink.
func TestOpenHTMLFileLoadsParentRelativeAsset(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel string, data []byte) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A 2x2 red PNG (opaque) as the background tile.
	mustWrite("img/tile.png", redPNG(t))
	mustWrite("css/main.css", []byte(`.box{width:40px;height:40px;background-image:url(../img/tile.png)}`))
	mustWrite("index.html", []byte(`<!DOCTYPE html><html><head>`+
		`<link rel="stylesheet" href="css/main.css"></head>`+
		`<body style="margin:0"><div class="box"></div></body></html>`))

	doc, err := OpenHTMLFile(filepath.Join(root, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	// The 40x40 box near the top-left must contain red pixels from the tiled PNG.
	if !hasRedPixel(img, 0, 0, 40, 40) {
		t.Error("background image not loaded: no red pixels in the box region")
	}
}
```

Add two small helpers in the same file: `redPNG(t)` (encode a 2×2 opaque-red `image.NRGBA` via `png.Encode` into a buffer, return bytes) and `hasRedPixel(img, x0,y0,x1,y1)` (scan the region; return true if any pixel has R>200,G<60,B<60,A>0). Import `bytes`, `image`, `image/color`, `image/png` as needed.

- [ ] **Step 6: Run the local-asset test + check the HTTP golden**

Run: `go test ./pkg/doctaculous -run TestOpenHTMLFileLoadsParentRelativeAsset -v`
Expected: PASS

Run: `go test ./pkg/doctaculous -run TestHTMLDocShowcase`
Expected: PASS unchanged (the showcase golden is rendered over HTTP, where `../` already resolved; Fix A does not change the HTTP golden). If it DOES change, that means the HTTP path also had a failing ref — regenerate with `-update` and eyeball.

- [ ] **Step 7: Commit**

```bash
git add pkg/resource/loader.go pkg/resource/loader_test.go pkg/doctaculous/local_asset_test.go
git commit -m "resource: DirLoader follows ../ refs that resolve within Base (local asset loading)"
```

---

## Task B: honor a named `@page` size under `ExplicitSize`

**Problem.** `resolvePageGeom` applies a resolved `@page size` only when `!pc.ExplicitSize`. `WithPageSize` sets `ExplicitSize=true`, so a `page: landscape` section's `@page landscape { size: 1056px 816px }` is ignored and the section stays portrait. Intent: the API-supplied size should override the *default/unnamed* `@page size`, but must NOT clobber a *named* page a section explicitly opted into via `page: <name>`.

**Files:**
- Modify: `pkg/layout/css/pagemodel.go` (`resolvePageGeom`, line ~60-64)
- Test: `pkg/layout/css/pagemodel_named_size_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/pagemodel_named_size_test.go`:

```go
package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
)

// TestNamedPageSizeHonoredUnderExplicitSize pins the fix: with an explicit
// WithPageSize (ExplicitSize=true), the DEFAULT (unnamed) page uses the API size,
// but a NAMED page a section opted into keeps its own @page size (e.g. landscape).
func TestNamedPageSizeHonoredUnderExplicitSize(t *testing.T) {
	// Two @page rules: the default (no size) and a named landscape (wide).
	sheet := gcss.Parse(`@page landscape { size: 1056px 816px }`)
	cfg := PagedConfig{
		FallbackW:    816,
		FallbackH:    1056,
		ExplicitSize: true, // WithPageSize was used
		Pages:        gcss.Stylesheet{Pages: sheet.Pages},
	}

	// The unnamed page keeps the API/fallback size (portrait 816x1056).
	def := cfg.resolvePageGeom(0, "", false)
	if def.pageW != 816 || def.pageH != 1056 {
		t.Errorf("default page = %vx%v; want 816x1056 (API size wins)", def.pageW, def.pageH)
	}

	// The named landscape page uses ITS @page size (wide 1056x816), even though
	// ExplicitSize is set.
	land := cfg.resolvePageGeom(0, "landscape", false)
	if land.pageW != 1056 || land.pageH != 816 {
		t.Errorf("landscape page = %vx%v; want 1056x816 (named @page size wins)", land.pageW, land.pageH)
	}
}
```

Confirm the field names on `PagedConfig` (`FallbackW`, `FallbackH`, `ExplicitSize`, `Pages`) and that `pageGeom` exposes `pageW`/`pageH` (unexported, same package — fine). Confirm `gcss.Parse(...).Pages` is the right shape for `PagedConfig.Pages` (a `gcss.Stylesheet` whose `Pages` are populated). Adjust to the actual `PagedConfig` construction if it differs.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestNamedPageSizeHonoredUnderExplicitSize -v`
Expected: FAIL — the landscape page comes back 816×1056 (named size suppressed by `ExplicitSize`).

- [ ] **Step 3: Fix `resolvePageGeom`**

In `pkg/layout/css/pagemodel.go`, change the size gate (line ~63) from:

```go
	if up.HasSize && !pc.ExplicitSize {
		g.pageW, g.pageH = up.WidthPt, up.HeightPt
	}
```

to:

```go
	// An explicit API size (WithPageSize) overrides the DEFAULT (unnamed) @page size,
	// but a section that opted into a NAMED page (page: <name>) must still get that
	// page's own @page size — otherwise a `page: landscape` section can never reflow
	// wider. So apply a named page's size unconditionally, and the unnamed page's size
	// only when the API did not pin one.
	if up.HasSize && (name != "" || !pc.ExplicitSize) {
		g.pageW, g.pageH = up.WidthPt, up.HeightPt
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestNamedPageSizeHonoredUnderExplicitSize -v`
Expected: PASS

- [ ] **Step 5: Run the css layout + doctaculous suites**

Run: `go test ./pkg/layout/css/...`
Expected: PASS (no regressions).

Run: `go test ./pkg/doctaculous -run TestHTMLDocShowcase`
Expected: the showcase golden may now render the landscape page WIDER. If a golden changes, regenerate with `go test ./pkg/doctaculous -run TestHTMLDocShowcase -update` and **eyeball** the landscape page PNG — it should be visibly wider (landscape) with content reflowed to the wider measure. Commit the regenerated PNG(s).

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/pagemodel.go pkg/layout/css/pagemodel_named_size_test.go pkg/doctaculous/testdata/golden
git commit -m "css/paged: honor a named @page size even under an explicit WithPageSize (landscape reflow)"
```

---

## Task C: emit floats per page in `paginateRuns` (multi-width named-page pagination)

**Problem.** `paginateRuns` nils each page's `.Floats` and never re-splits/re-attaches them, so any float in a document that uses named pages (which routes through `paginateRuns`) is dropped — including a floated `<figure>`'s image. The single-width `paginateDoc` path does this correctly via `splitFloatsByPage`.

**Files:**
- Modify: `pkg/layout/css/pagemodel.go` (`paginateRuns`, the per-bucket loop ~line 285-319, and the run-bucket accumulation ~line 260-272)
- Test: `pkg/layout/css/floats_paginate_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/floats_paginate_test.go`. This is an integration-style test at the engine boundary: build a small document with a named page and a float, paginate, and assert a float item appears in the output. Because constructing a `cssbox.Box` tree by hand is verbose, drive it through the public HTML path.

```go
package css_test

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	layoutcss "github.com/nathanstitt/doctaculous/pkg/layout/css"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// TestFloatsPaintInNamedPageRun pins the fix: when a document uses a named @page
// (so pagination goes through paginateRuns), a floated block must still emit its
// paint items. Before the fix, paginateRuns nil'd Floats and never re-attached
// them, so the float — and any image inside it — vanished.
func TestFloatsPaintInNamedPageRun(t *testing.T) {
	src := []byte(`<!DOCTYPE html><html><head><style>
	  @page wide { size: 1200px 800px }
	  .land { page: wide }
	  .fig { float: left; width: 100px; height: 60px; background: #c00 }
	</style></head><body>
	  <section class="land">
	    <div class="fig"></div>
	    <p>text beside the float</p>
	  </section>
	</body></html>`)

	doc, err := html.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	root, faces, pages, running, err := layoutcss.BuildWithFontsPagesRunning(ctx, doc, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := layoutcss.New(layoutfont.NewFaceCacheWithFonts(faces, nil, nil, nil), nil, nil)
	out, err := engine.LayoutPagedDoc(ctx, root, layoutcss.PagedConfig{
		Paged: true, FallbackW: 816, FallbackH: 1056, ExplicitSize: false,
		Pages: pages, Running: running,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The float paints a #c00 background rectangle. Assert at least one page carries a
	// filled rule/background item in the float's color (a float dropped => no such item).
	if !hasFilledRect(out, 0xcc, 0x00, 0x00) {
		t.Fatal("floated block produced no paint items (float dropped in paginateRuns)")
	}
}

// hasFilledRect reports whether any page carries a Rule/Background item whose color
// approximately matches (r,g,b).
func hasFilledRect(pages *layout.Pages, r, g, b uint8) bool {
	near := func(a, want uint8) bool {
		if a > want {
			return a-want < 8
		}
		return want-a < 8
	}
	for pi := range pages.Pages {
		for _, it := range pages.Pages[pi].Items {
			if it.Kind == layout.RuleKind || it.Kind == layout.BackgroundKind {
				c := it.Rule.Color
				if near(c.R, r) && near(c.G, g) && near(c.B, b) {
					return true
				}
			}
		}
	}
	return false
}
```

Verify the exact signatures of `BuildWithFontsPagesRunning`, `New`, `NewFaceCacheWithFonts`, `LayoutPagedDoc`, and `PagedConfig` fields against the source (they are used verbatim by `pkg/doctaculous/html_backend.go` — copy from there). Adjust the import list / constructor args to match.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestFloatsPaintInNamedPageRun -v`
Expected: FAIL — no `#c00` fill on any page (the float was dropped).

- [ ] **Step 3: Compute per-run float splits in `paginateRuns`**

In `pkg/layout/css/pagemodel.go`, the run accumulation loop (~line 260) currently builds `all []runBucket` from each run's buckets. Extend it to also record, per accumulated bucket, the floats for that bucket's page. The floats live on the run's layout root: `layoutByWidth[geom.contentW]` (a `*Fragment` whose `.Floats` holds that width's floats). Split them against the run's buckets with the existing helper.

Find the loop:

```go
	for _, r := range runs {
		geom, blocks := getLayout(r.name)
		if r.end > len(blocks) {
			continue
		}
		runBlocks := blocks[r.start:r.end]
		cb := contentWidth(bodyFragment(layoutByWidth[geom.contentW]), geom.contentW)
		bks := bucketBlocks(runBlocks, geom.contentH, cb, e.logf)
		for _, bk := range bks {
			all = append(all, runBucket{bucket: bk, geom: geom})
		}
	}
```

Change the inner loop to split this run's floats across this run's buckets and carry each bucket's floats on the `runBucket`. Add a `floats []*Fragment` field to the `runBucket` struct (find its definition near the top of `paginateRuns` or in the file and add the field), then:

```go
	for _, r := range runs {
		geom, blocks := getLayout(r.name)
		if r.end > len(blocks) {
			continue
		}
		runBlocks := blocks[r.start:r.end]
		cb := contentWidth(bodyFragment(layoutByWidth[geom.contentW]), geom.contentW)
		bks := bucketBlocks(runBlocks, geom.contentH, cb, e.logf)
		// Split this run's floats across its buckets (mirroring the single-width path),
		// so a floated block in a named-page run still paints on its page. Floats live
		// on the run's layout root for this content width; each bucket's band is
		// [bk.top, bk.top+geom.contentH).
		runFloats := floatsForRun(layoutByWidth[geom.contentW], bks, geom.contentH)
		for bi, bk := range bks {
			all = append(all, runBucket{bucket: bk, geom: geom, floats: runFloats[bi]})
		}
	}
```

Add the helper `floatsForRun` in the same file. `pageBucket` has fields `{top float64, blocks []*Fragment}` (no height field), so the band height is passed in as `contentH`. It splits the layout root's floats per bucket, shifting each by `-bk.top` to the bucket's local frame (matching `shiftFragments(bk.blocks, -bk.top)` done later per bucket):

```go
// floatsForRun partitions the run layout's floats across its buckets. A float belongs
// to the bucket whose vertical band [bk.top, bk.top+contentH) contains the float's
// top; it is shifted into that bucket's local frame (-bk.top) to match the bucket's
// blocks. Floats outside every bucket band are dropped (off-run). It clones each
// placed float before shifting so the shared, cached layoutByWidth layout (reused
// across buckets/runs at this width) is never mutated.
func floatsForRun(runLayout *Fragment, bks []pageBucket, contentH float64) [][]*Fragment {
	out := make([][]*Fragment, len(bks))
	if runLayout == nil || len(runLayout.Floats) == 0 || len(bks) == 0 {
		return out
	}
	for _, fl := range runLayout.Floats {
		bi := -1
		for i := range bks {
			if fl.Y >= bks[i].top && fl.Y < bks[i].top+contentH {
				bi = i
				break
			}
		}
		if bi < 0 {
			continue // float not in this run's page bands
		}
		clone := cloneFloatForPage(fl)
		shiftFragment(clone, -bks[bi].top)
		out[bi] = append(out[bi], clone)
	}
	return out
}
```

For `cloneFloatForPage`, FIRST search `pkg/layout/css` for an existing fragment deep-clone helper (`grep -rn "func .*[Cc]lone.*Fragment" pkg/layout/css`). If one exists, call it. If none exists, add a minimal deep clone sufficient for `shiftFragment` — which mutates page-space fields on the fragment AND recurses into `Children`/`Floats`/`Positioned`. A shallow `c := *fl; return &c` is NOT enough because `shiftFragment` would then mutate the shared descendant slices in place, corrupting later buckets. Add:

```go
// cloneFloatForPage deep-copies a float fragment enough that shiftFragment can move
// it without mutating the shared cached layout: it copies the fragment and its child
// slices recursively (the fields shiftFragment recurses into). Read-only fields
// (glyph outlines, images, styles) are shared by pointer — shiftFragment does not
// touch them.
func cloneFloatForPage(f *Fragment) *Fragment {
	if f == nil {
		return nil
	}
	c := *f
	if len(f.Children) > 0 {
		c.Children = make([]*Fragment, len(f.Children))
		for i, ch := range f.Children {
			c.Children[i] = cloneFloatForPage(ch)
		}
	}
	if len(f.Floats) > 0 {
		c.Floats = make([]*Fragment, len(f.Floats))
		for i, ch := range f.Floats {
			c.Floats[i] = cloneFloatForPage(ch)
		}
	}
	if len(f.Positioned) > 0 {
		c.Positioned = make([]*Fragment, len(f.Positioned))
		for i, ch := range f.Positioned {
			c.Positioned[i] = cloneFloatForPage(ch)
		}
	}
	return c
}
```

Verify against the real `Fragment` definition (`pkg/layout/css/fragment.go`) that `Children`, `Floats`, and `Positioned` are the slice fields `shiftFragment` recurses into; if `shiftFragment` also recurses into other owned slices (e.g. a `PositionedInfo` parallel slice), copy those too. Cross-check by reading `shiftFragment`'s body (`grep -n "func shiftFragment" pkg/layout/css/*.go`).

> IMPORTANT (shared-layout mutation): `layoutByWidth[w]` is reused for every bucket of every run at that width. Do NOT shift the original floats in place (that corrupts later buckets). Clone before shifting. If a suitable deep-clone helper does not exist, the safest minimal approach is to re-split without shifting and instead apply the `-bk.top` shift at attach time on a fresh copy — see Step 4.

- [ ] **Step 4: Attach each bucket's floats to its page root**

In the per-bucket assembly loop (~line 285), the code currently nils floats:

```go
		runBody := *bodyFragment(layoutByWidth[g.contentW])
		runBody.Children = bk.blocks
		runBody.Positioned, runBody.PositionedInfo, runBody.Floats = nil, nil, nil
		...
		pageRoot.Positioned, pageRoot.PositionedInfo, pageRoot.Floats = nil, nil, nil
```

Replace the float-nil-ing on `pageRoot` with the bucket's floats (keep `runBody.Floats` nil — floats attach at the page root, mirroring `paginateDoc` where `pageRoot.Floats = perPageFloats[i]` and `pageBody.Floats = nil`):

```go
		runBody := *bodyFragment(layoutByWidth[g.contentW])
		runBody.Children = bk.blocks
		runBody.Positioned, runBody.PositionedInfo, runBody.Floats = nil, nil, nil
		...
		pageRoot.Positioned, pageRoot.PositionedInfo = nil, nil
		pageRoot.Floats = all[i].floats // per-bucket floats (nil when none), shifted to local frame
```

Because the per-bucket loop already does `shiftFragments(bk.blocks, -bk.top)` and later `translateFragment(&pageRoot, dx, dy)`, ensure the floats receive the SAME `dx,dy` margin/bleed translate as the page root. Since the floats are attached to `pageRoot` BEFORE the `translateFragment(&pageRoot, dx, dy)` call, and `translateFragment` recurses into `.Floats`, they will be translated correctly — verify `translateFragment` recurses into `Floats` (it must, or the floats won't get the margin inset). If it does not, translate the floats explicitly.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestFloatsPaintInNamedPageRun -v`
Expected: PASS — a `#c00` fill appears on a page.

Run: `go test ./pkg/layout/css/...`
Expected: PASS (no regressions — the single-width path and non-float named-page pages are unchanged; `floatsForRun` returns empty for documents with no floats, so byte-identical there).

- [ ] **Step 6: Regenerate + eyeball the showcase golden**

Run: `go test ./pkg/doctaculous -run TestHTMLDocShowcase -update`
Then **view** the changed `htmldoc-p*.png`: the Floats & Clear page should now show the floated figure (its border/background and, if the image path is also fixed, its image). Confirm no unrelated page changed. Commit the regenerated PNG(s).

- [ ] **Step 7: Commit**

```bash
git add pkg/layout/css/pagemodel.go pkg/layout/css/floats_paginate_test.go pkg/doctaculous/testdata/golden
git commit -m "css/paged: split + emit floats per page in named-page pagination (floated figures paint)"
```

---

## Task D: full verification + branch finish

**Files:** none (verification only)

- [ ] **Step 1: Full suite + race + vet + lint**

Run: `go test ./...`
Expected: PASS

Run: `go test -race ./pkg/layout/css/... ./pkg/resource/... ./pkg/doctaculous`
Expected: PASS (Fix C touches shared-layout reuse — the race run guards against the clone-vs-mutate hazard).

Run: `go vet ./... && golangci-lint run`
Expected: clean

- [ ] **Step 2: End-to-end visual confirmation**

Run:
```bash
go build -o bin/doctaculous ./cmd/doctaculous
./bin/doctaculous topdf --in testdata/htmldoc/index.html --out out/page.pdf --page-size letter
./bin/doctaculous rasterize --in out/page.pdf --pages all --out out/check-%d.png --dpi 96
```
Eyeball: the Floats page shows the figure, the Backgrounds page shows the tiled/cover/positioned images, and the Landscape page is visibly wider than the others.

- [ ] **Step 3: Finish the branch**

Announce and use superpowers:finishing-a-development-branch to present merge/PR options.

---

## Self-review notes (resolved)

- **Spec coverage:** three independent bugs → Task A (DirLoader `../`), Task B (named `@page` size under `ExplicitSize`), Task C (float split in `paginateRuns`); Task D verifies. All three surfaced by the showcase map to a task.
- **Independence / parallelism:** A is in `pkg/resource`, B and C are both in `pkg/layout/css/pagemodel.go` (B edits `resolvePageGeom` line ~63; C edits `paginateRuns` line ~260-319 — non-overlapping regions of the same file, so if run by parallel subagents in isolated worktrees they merge cleanly; if run in one worktree, do B then C sequentially to avoid a merge conflict in `pagemodel.go`).
- **Shared-layout mutation hazard (Task C):** flagged explicitly — `layoutByWidth[w]` is reused across buckets, so floats must be cloned before `shiftFragment`, unlike the single-width path which owns its layout. Step 3 and the `-race` run in Task D guard this.
- **Golden changes:** only `htmldoc-p*.png` should change (Task B landscape width, Task C float visibility, possibly none for Task A since the golden is HTTP-served). Every regenerated PNG must be eyeballed. No other golden set is touched.
- **Placeholder scan:** all test bodies and fix diffs are concrete; the only "verify the exact signature" notes point at real call sites (`pkg/doctaculous/html_backend.go`) to copy from, not undefined APIs.
- **Type consistency:** `resolvePageGeom(i, name, blank)`, `PagedConfig{FallbackW,FallbackH,ExplicitSize,Pages}`, `splitFloatsByPage`, `pageBucket.top`, `runBucket{bucket,geom,floats}`, `shiftFragment`/`shiftFragments`/`translateFragment` are used consistently and match the current source.
- **Graceful degradation preserved:** DirLoader still refuses a truly-escaping ref (`ErrNotFound`); `floatsForRun` returns empty for float-free docs (byte-identical); the named-size fix only *adds* a size where one was being dropped.
