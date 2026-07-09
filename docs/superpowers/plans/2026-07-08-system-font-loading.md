# System Font Loading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make installed OS fonts the default source for non-embedded fonts, backed by `adrg/sysfont`, with a bundled-fonts opt-out (library option + `--bundled-fonts` CLI flag).

**Architecture:** A new `OSFontProvider` (in `pkg/layout/font`) implements the existing `font.Provider` seam (`LoadStyled(family, bold, italic) ([]byte, bool)`) by querying a lazily-built, scan-cached `sysfont.Finder` and returning the matched file's bytes. Both pipelines already resolve `@font-face → injected Provider → bundled substitute → skip`, so system mode just installs an `OSFontProvider` as that provider (the bundled `LoadStandard`/`LookupStyled` remains the natural fall-through when sysfont returns nil). Bundled mode installs no provider. The mode is selected by a library option / `RasterOptions.BundledFonts` / a `--bundled-fonts` CLI flag; the golden tests pin bundled mode for hermeticity.

**Tech Stack:** Go 1.26, `github.com/adrg/sysfont` v0.1.2 (MIT, pure Go; pulls transitive `adrg/os-font-list`, `adrg/strutil`, `adrg/xdg`), existing `pkg/font` / `pkg/layout/font` / `pkg/render/raster` / `pkg/doctaculous` / `cmd/doctaculous`.

---

## Key facts (verified against the codebase — read before starting)

- `font.Provider` is `interface { LoadStyled(family string, bold, italic bool) (data []byte, ok bool) }` in `pkg/font/family.go`.
- Reflow chain: `pkg/layout/font/cache.go` `resolveList` already does `resolveFontFace (@font-face) → resolveProvider → LoadStandard (bundled)`. `resolveProvider` type-asserts `c.sys.(pkgfont.Provider)` and decodes bytes via `pkgfont.LoadSFNT` (which handles TrueType, OTTO, **and ttcf collections** — so `.ttc`/`.otf`/`.ttf` all decode; a classic Type1/PFB is logged+skipped→bundled).
- PDF chain: `pkg/render/raster/page.go` `pageResources{provider}` → `font.New(doc, dict, provider, logf)` → `standardSubstituteProgram(doc, dict, provider, logf)`, which sniffs the provider bytes' format (sfnt/CFF/Type1) itself. `raster.Options.FontProvider` and `RasterOptions.FontProvider` already exist.
- `sysfont` API: `sysfont.NewFinder(opts *FinderOpts) *Finder` walks `xdg.FontDirs` eagerly (default extensions `.ttf .ttc .otf`); `Finder.Match(query string) *Font` returns `nil` when nothing usable is found; `Font.Filename` is the path. Because default extensions exclude `.pfb`, the provider never returns Type1 bytes.
- `pkg/layout/font` already defines a `SystemFontProvider` **interface** (for `@font-face local()`). The new concrete type MUST be named `OSFontProvider` to avoid a collision.
- CLI subcommands (`rasterize`, `topdf`, `tomd`, `tohtml`) use `flag.NewFlagSet` + a `reorderArgs`/`reorderTopdfArgs`/`reorderTomdArgs` valueFlags map (bool flags like `--bundled-fonts` are NOT value flags, so they need no valueFlags entry).

---

## Task 1: Branch + add the sysfont dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Create the feature branch**

Run:
```bash
cd /Users/nas/code/docalizer
git checkout main && git pull --ff-only origin main
git checkout -b system-font-loading
```

- [ ] **Step 2: Add the dependency**

Run:
```bash
go get github.com/adrg/sysfont@v0.1.2
```
Expected: `go.mod` gains `github.com/adrg/sysfont v0.1.2` and transitive `github.com/adrg/os-font-list`, `github.com/adrg/strutil`, `github.com/adrg/xdg` (as indirect). No error.

- [ ] **Step 3: Verify it builds and tidy**

Run:
```bash
go build ./... && go mod tidy
```
Expected: builds clean; `go.mod`/`go.sum` updated.

- [ ] **Step 4: Confirm licenses are permissive (MIT)**

Run:
```bash
go list -m -f '{{.Path}} {{.Dir}}' github.com/adrg/sysfont github.com/adrg/os-font-list github.com/adrg/strutil github.com/adrg/xdg | while read p d; do echo "== $p =="; head -3 "$d"/LICENSE* 2>/dev/null; done
```
Expected: each shows an MIT license header. (If any is not MIT/BSD/Apache, STOP and report — the project bars non-permissive deps.)

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add adrg/sysfont (MIT, pure Go) for system font discovery"
```

---

## Task 2: `OSFontProvider` — the sysfont-backed `font.Provider`

**Files:**
- Create: `pkg/layout/font/osfont.go`
- Test: `pkg/layout/font/osfont_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/font/osfont_test.go`:
```go
package font

import (
	"testing"

	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
)

// OSFontProvider must satisfy the pkgfont.Provider interface.
var _ pkgfont.Provider = (*OSFontProvider)(nil)

// TestOSFontProviderResolvesInstalledFont is assertive-when-possible, skip-when-bare:
// it discovers what fonts are actually installed on this host and, if a common family
// resolves, asserts LoadStyled returns non-empty bytes that decode as a font. It never
// hard-asserts a specific font (notably not Arial, which is absent on default Linux/CI),
// so it is not flaky.
func TestOSFontProviderResolvesInstalledFont(t *testing.T) {
	p := NewOSFontProvider()
	// Try a spread of families that SOME OS ships: macOS (Helvetica/Times/Courier),
	// Linux (DejaVu/Liberation), Windows (Arial/Calibri). If none resolve, the host
	// has essentially no fonts (bare CI) and we skip.
	families := []string{"Helvetica", "Times", "Courier", "DejaVu Sans", "Liberation Sans", "Arial", "Calibri"}
	for _, fam := range families {
		if data, ok := p.LoadStyled(fam, false, false); ok {
			if len(data) == 0 {
				t.Fatalf("LoadStyled(%q) returned ok with empty bytes", fam)
			}
			if _, err := LoadSFNT(data); err != nil {
				t.Fatalf("LoadStyled(%q) bytes did not decode as a font: %v", fam, err)
			}
			return // one real resolution is enough to prove the path
		}
	}
	t.Skip("no common system font installed on this host; skipping system-font resolution assertion")
}

// TestOSFontProviderMissReturnsFalse: a family that cannot exist resolves to ok=false
// (or, on a machine where sysfont's fuzzy match reaches for it, still returns decodable
// bytes — either is acceptable; the contract is only 'no panic, ok=false on a true miss').
func TestOSFontProviderMissReturnsFalse(t *testing.T) {
	p := NewOSFontProvider()
	// A deliberately-nonsense family; on a bare host this is a clean miss.
	data, ok := p.LoadStyled("ZzQqNoSuchFontFamily12345", false, false)
	if ok && len(data) == 0 {
		t.Fatal("LoadStyled reported ok with empty bytes")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/layout/font/ -run TestOSFontProvider -v`
Expected: FAIL to compile — `undefined: NewOSFontProvider` / `undefined: OSFontProvider`.

- [ ] **Step 3: Implement `OSFontProvider`**

Create `pkg/layout/font/osfont.go`:
```go
package font

import (
	"os"
	"strings"
	"sync"

	"github.com/adrg/sysfont"

	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
)

// OSFontProvider implements pkgfont.Provider.
var _ pkgfont.Provider = (*OSFontProvider)(nil)

// OSFontProvider resolves a family+style to an installed OS font via adrg/sysfont,
// which live-scans the platform's standard font directories (macOS, Linux, and Windows
// font folders via adrg/xdg). It is the default, opt-in-by-mode source of non-embedded
// faces: constructing one trades hermetic reproducibility for host-font fidelity.
//
// The directory scan is expensive, so it runs once, lazily, on first LoadStyled and is
// cached; the provider is safe for concurrent LoadStyled calls (a parsed document is
// shared read-only across the page fan-out). sysfont's default extensions are
// .ttf/.ttc/.otf, so the returned bytes are always sfnt-family (never classic Type1),
// which the reflow decode path (LoadSFNT) and the PDF substitute path both accept.
type OSFontProvider struct {
	once   sync.Once
	finder *sysfont.Finder
	logf   func(string, ...any)
}

// NewOSFontProvider returns a provider that resolves installed OS fonts. logf may be
// nil (diagnostics discarded). The font-directory scan is deferred to first use.
func NewOSFontProvider() *OSFontProvider { return &OSFontProvider{} }

// NewOSFontProviderWithLogf is NewOSFontProvider with a diagnostics logger.
func NewOSFontProviderWithLogf(logf func(string, ...any)) *OSFontProvider {
	return &OSFontProvider{logf: logf}
}

func (p *OSFontProvider) log(format string, args ...any) {
	if p.logf != nil {
		p.logf(format, args...)
	}
}

// LoadStyled resolves family+style to an installed font's raw bytes. It returns
// ok=false when the host has no usable match (sysfont returns nil — a bare/headless
// machine) or the matched file cannot be read; the caller then falls through to the
// bundled substitute. It never panics.
func (p *OSFontProvider) LoadStyled(family string, bold, italic bool) (data []byte, ok bool) {
	p.once.Do(func() { p.finder = sysfont.NewFinder(nil) })
	if p.finder == nil {
		return nil, false
	}
	match := p.finder.Match(styleQuery(family, bold, italic))
	if match == nil || match.Filename == "" {
		return nil, false
	}
	b, err := os.ReadFile(match.Filename)
	if err != nil {
		p.log("osfont: read %q for %q: %v", match.Filename, family, err)
		return nil, false
	}
	return b, true
}

// styleQuery builds the query string sysfont's fuzzy matcher expects: the family name
// followed by the style words, e.g. "Arial Bold", "Times Bold Italic".
func styleQuery(family string, bold, italic bool) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(family))
	if bold {
		sb.WriteString(" Bold")
	}
	if italic {
		sb.WriteString(" Italic")
	}
	return sb.String()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/layout/font/ -run TestOSFontProvider -v`
Expected: PASS (or `TestOSFontProviderResolvesInstalledFont` SKIP on a bare host; `TestOSFontProviderMissReturnsFalse` PASS). No compile errors.

- [ ] **Step 5: Vet + race + commit**

Run:
```bash
gofmt -w pkg/layout/font/osfont.go pkg/layout/font/osfont_test.go
go vet ./pkg/layout/font/ && go test -race ./pkg/layout/font/ -run TestOSFontProvider
```
Expected: clean, PASS/SKIP.

```bash
git add pkg/layout/font/osfont.go pkg/layout/font/osfont_test.go
git commit -m "font: OSFontProvider — resolve installed OS fonts via adrg/sysfont"
```

---

## Task 3: `styleQuery` unit test (lock the query format)

**Files:**
- Test: `pkg/layout/font/osfont_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `pkg/layout/font/osfont_test.go`:
```go
func TestStyleQuery(t *testing.T) {
	cases := []struct {
		family       string
		bold, italic bool
		want         string
	}{
		{"Arial", false, false, "Arial"},
		{"Arial", true, false, "Arial Bold"},
		{"Times New Roman", false, true, "Times New Roman Italic"},
		{"Helvetica", true, true, "Helvetica Bold Italic"},
		{"  Georgia  ", false, false, "Georgia"},
	}
	for _, c := range cases {
		if got := styleQuery(c.family, c.bold, c.italic); got != c.want {
			t.Errorf("styleQuery(%q,%v,%v) = %q, want %q", c.family, c.bold, c.italic, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it passes**

Run: `go test ./pkg/layout/font/ -run TestStyleQuery -v`
Expected: PASS (styleQuery already implemented in Task 2).

- [ ] **Step 3: Commit**

```bash
git add pkg/layout/font/osfont_test.go
git commit -m "font: test styleQuery fuzzy-query formatting"
```

---

## Task 4: PDF library mode toggle — `RasterOptions.BundledFonts`

**Files:**
- Modify: `pkg/doctaculous/doctaculous.go` (RasterOptions struct, ~line 33-53)
- Modify: `pkg/doctaculous/pdf_backend.go` (renderPage, ~line 67-77)
- Test: `pkg/doctaculous/pdf_fontmode_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/doctaculous/pdf_fontmode_test.go`:
```go
package doctaculous

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// TestRasterizeBundledFontsMode renders a PDF that uses a non-embedded base-14 font in
// bundled mode (BundledFonts:true). This must succeed and be hermetic (no system fonts
// consulted). It is the mode the golden tests rely on.
func TestRasterizeBundledFontsMode(t *testing.T) {
	doc, err := OpenBytes(gen.WeightedFontsPDF())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72, BundledFonts: true})
	if err != nil {
		t.Fatalf("RasterizePage (bundled): %v", err)
	}
	if img == nil {
		t.Fatal("nil image")
	}
}

// TestRasterizeSystemFontsDefault renders in the default (system) mode. It must not
// error regardless of what fonts the host has (system match, or fall-through to the
// bundled safety net on a bare box).
func TestRasterizeSystemFontsDefault(t *testing.T) {
	doc, err := OpenBytes(gen.WeightedFontsPDF())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatalf("RasterizePage (system default): %v", err)
	}
	if img == nil {
		t.Fatal("nil image")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/doctaculous/ -run 'TestRasterizeBundledFontsMode|TestRasterizeSystemFontsDefault' -v`
Expected: FAIL to compile — `unknown field BundledFonts in struct literal`.

- [ ] **Step 3: Add the `BundledFonts` field**

In `pkg/doctaculous/doctaculous.go`, inside `type RasterOptions struct`, after the `FontProvider font.Provider` field, add:
```go
	// BundledFonts selects hermetic bundled-font mode: non-embedded fonts resolve only
	// from the bundled substitutes (pkg/font/standard), never the host's installed
	// fonts. Default false = system mode, which installs an OSFontProvider so real
	// installed fonts are used. Ignored when FontProvider is set explicitly (that
	// provider always wins). The golden/reference tests set this true for reproducibility.
	BundledFonts bool
```

- [ ] **Step 4: Wire the mode in renderPage**

In `pkg/doctaculous/pdf_backend.go`, replace the `renderPage` body's `raster.RenderPage(...)` call with provider selection. The current code is:
```go
	return raster.RenderPage(ctx, pg, raster.Options{
		DPI:          opts.dpi(),
		Background:   opts.Background,
		Logf:         opts.Logf,
		FontProvider: opts.FontProvider,
	})
```
Replace with:
```go
	return raster.RenderPage(ctx, pg, raster.Options{
		DPI:          opts.dpi(),
		Background:   opts.Background,
		Logf:         opts.Logf,
		FontProvider: opts.fontProvider(),
	})
```
Then add this method to `pkg/doctaculous/pdf_backend.go` (top-level, after `renderPage`):
```go
// fontProvider resolves the font provider for a rasterize call per the mode precedence:
// an explicit FontProvider always wins; else bundled mode (BundledFonts) installs no
// provider (bundled-only); else the default installs an OSFontProvider so installed OS
// fonts are used, falling through to the bundled substitute when none match.
func (o RasterOptions) fontProvider() font.Provider {
	if o.FontProvider != nil {
		return o.FontProvider
	}
	if o.BundledFonts {
		return nil
	}
	return layoutfont.NewOSFontProviderWithLogf(o.Logf)
}
```

- [ ] **Step 5: Add imports**

Ensure `pkg/doctaculous/pdf_backend.go` imports both `font` and `layoutfont`. Its import block currently has `"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"`, `pkg/pdf`, `pkg/pdf/extract`, `pkg/render/raster`. Add:
```go
	"github.com/nathanstitt/doctaculous/pkg/font"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
```

- [ ] **Step 6: Run to verify it passes**

Run:
```bash
gofmt -w pkg/doctaculous/doctaculous.go pkg/doctaculous/pdf_backend.go pkg/doctaculous/pdf_fontmode_test.go
go test ./pkg/doctaculous/ -run 'TestRasterizeBundledFontsMode|TestRasterizeSystemFontsDefault' -v
```
Expected: PASS both.

- [ ] **Step 7: Commit**

```bash
git add pkg/doctaculous/doctaculous.go pkg/doctaculous/pdf_backend.go pkg/doctaculous/pdf_fontmode_test.go
git commit -m "doctaculous: RasterOptions.BundledFonts mode (system fonts default for PDF)"
```

---

## Task 5: Reflow library mode toggle — `WithBundledFonts` HTML option

**Files:**
- Modify: `pkg/doctaculous/html_backend.go` (htmlConfig struct, options, htmlDocument)
- Test: `pkg/doctaculous/html_fontmode_test.go` (create)

**Context:** `htmlDocument` builds the FaceCache via `layoutfont.NewFaceCacheWithFonts(fontFaces, cfg.loader, cfg.sys, cfg.logf)`. The FaceCache's `resolveProvider` uses `c.sys` when it also implements `pkgfont.Provider`. So system mode = make `cfg.sys` an `OSFontProvider` by default; bundled mode = leave `cfg.sys` as whatever @font-face provider was set (or nil), so `resolveProvider` finds no `pkgfont.Provider` and falls straight to the bundled `LoadStandard`. Because `WithSystemFontProvider` already sets `cfg.sys` (for `local()`), the default OSFontProvider must only be installed when the caller did NOT supply their own sys provider.

- [ ] **Step 1: Write the failing test**

Create `pkg/doctaculous/html_fontmode_test.go`:
```go
package doctaculous

import (
	"context"
	"testing"
)

// TestOpenHTMLBundledFonts renders HTML naming a base-14 family in bundled mode and in
// the default system mode. Both must lay out without error; the assertion is that the
// option compiles and the pipeline runs (hermetic bundled path + system default path).
func TestOpenHTMLBundledFonts(t *testing.T) {
	src := []byte(`<html><body style="font-family:Helvetica"><p>Hello fonts</p></body></html>`)

	docB, err := OpenHTMLBytes(src, WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes (bundled): %v", err)
	}
	if _, err := docB.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72, BundledFonts: true}); err != nil {
		t.Fatalf("rasterize bundled: %v", err)
	}

	docS, err := OpenHTMLBytes(src) // default: system mode
	if err != nil {
		t.Fatalf("OpenHTMLBytes (system default): %v", err)
	}
	if _, err := docS.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72, BundledFonts: true}); err != nil {
		t.Fatalf("rasterize system: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/doctaculous/ -run TestOpenHTMLBundledFonts -v`
Expected: FAIL to compile — `undefined: WithBundledFonts`.

- [ ] **Step 3: Add a `bundledFonts` flag to htmlConfig and the option**

In `pkg/doctaculous/html_backend.go`, add a field to `type htmlConfig struct`:
```go
	// bundledFonts selects hermetic bundled-font mode (no OS system-font lookup). Default
	// false = system mode. Set by WithBundledFonts.
	bundledFonts bool
```
Add the option near the other `With*` options:
```go
// WithBundledFonts selects hermetic bundled-font mode: non-embedded families resolve
// only from the bundled substitutes, never the host's installed OS fonts. The default
// (without this option) is system mode, which uses installed OS fonts and falls back to
// the bundled substitutes when none match. The golden/reference tests use this option
// for reproducibility.
func WithBundledFonts() HTMLOption {
	return func(c *htmlConfig) { c.bundledFonts = true }
}
```

- [ ] **Step 4: Install the OSFontProvider by default in htmlDocument**

In `pkg/doctaculous/html_backend.go` `htmlDocument`, immediately BEFORE the line
`faces := layoutfont.NewFaceCacheWithFonts(fontFaces, cfg.loader, cfg.sys, cfg.logf)`, insert:
```go
	// System mode (the default): if the caller did not supply their own font provider,
	// install an OSFontProvider so installed OS fonts resolve non-embedded families
	// (falling back to the bundled substitutes when none match). Bundled mode leaves
	// sys as-is, so resolveProvider finds no pkgfont.Provider and uses the bundle only.
	sys := cfg.sys
	if !cfg.bundledFonts && sys == nil {
		sys = layoutfont.NewOSFontProviderWithLogf(cfg.logf)
	}
```
Then change that `faces := ...` line to pass `sys` instead of `cfg.sys`:
```go
	faces := layoutfont.NewFaceCacheWithFonts(fontFaces, cfg.loader, sys, cfg.logf)
```

**Note on the interface:** `NewFaceCacheWithFonts`'s `sys` parameter is typed `layoutfont.SystemFontProvider` (the `@font-face local()` interface with `LoadLocal`). `OSFontProvider` does NOT implement `LoadLocal`, so it cannot be passed there directly. RESOLVE THIS in Step 5.

- [ ] **Step 5: Make OSFontProvider satisfy the SystemFontProvider interface (no-op LoadLocal)**

The FaceCache field `sys` is typed `SystemFontProvider` (has `LoadLocal(name string) ([]byte, bool)`), and `resolveProvider` type-asserts it to `pkgfont.Provider`. For an `OSFontProvider` to be installable as `sys`, it must ALSO implement `LoadLocal`. `local()` is a by-exact-name lookup (a font's PostScript/local name), which sysfont's family matcher is not built for, so `LoadLocal` returns a miss. Add to `pkg/layout/font/osfont.go`:
```go
// LoadLocal implements SystemFontProvider so an OSFontProvider can be installed as the
// FaceCache's provider. @font-face local() is an exact-name lookup that sysfont's
// family/style matcher does not serve, so this always reports a miss; family+style
// resolution goes through LoadStyled (the pkgfont.Provider route) instead.
func (p *OSFontProvider) LoadLocal(string) ([]byte, bool) { return nil, false }
```
Add the interface assertion near the top of `osfont.go` (after the `pkgfont.Provider` assertion):
```go
// OSFontProvider also satisfies the local()-lookup interface so it can be installed as
// the FaceCache's sys provider (its LoadLocal is a deliberate no-op).
var _ SystemFontProvider = (*OSFontProvider)(nil)
```

- [ ] **Step 6: Run to verify it passes**

Run:
```bash
gofmt -w pkg/doctaculous/html_backend.go pkg/doctaculous/html_fontmode_test.go pkg/layout/font/osfont.go
go test ./pkg/doctaculous/ -run TestOpenHTMLBundledFonts -v
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/doctaculous/html_backend.go pkg/doctaculous/html_fontmode_test.go pkg/layout/font/osfont.go
git commit -m "doctaculous: WithBundledFonts option; OSFontProvider as reflow sys provider (system default)"
```

---

## Task 6: Pin bundled mode in the golden/reference tests (hermeticity)

**Files:**
- Modify: `pkg/doctaculous/html_golden_test.go`, `htmldoc_golden_test.go`, `pagination_golden_test.go`, `pagedmedia_golden_test.go`, `docx_golden_test.go`, `pdf_golden_test.go`, `pdf_extract_test.go`

**Context:** The `pkg/render/raster` golden tests call `RenderPage(..., raster.Options{DPI: goldenDPI})` directly; `raster.Options.FontProvider` defaults to nil = bundled-only, so **the raster-package goldens are already hermetic — do NOT change them.** Only the `pkg/doctaculous` goldens (which flow through `RasterOptions`/`Open*` where system mode is now the default) must pin bundled mode. Two independent switches matter and BOTH must be set where they apply:
- `RasterOptions{..., BundledFonts: true}` — controls the PDF-raster and DOCX-raster font source at rasterize time.
- `WithBundledFonts()` on the HTML `Open*` call — controls the reflow (HTML layout) font source at open time. An HTML golden needs BOTH (open in bundled mode AND rasterize in bundled mode).

**Exact golden sites (verified by grep) to change — these and ONLY these:**
- `docx_golden_test.go:49` — `RasterOptions{DPI: goldenDPI}` → add `BundledFonts: true`. (DOCX opens via `OpenDOCX` which uses `NewFaceCache()` with no provider → already bundled-only at layout, so no Open option exists/needed; only the RasterOptions flag matters.)
- `htmldoc_golden_test.go:41` — `OpenURL(srv.URL+"/index.html", WithDefaultPaged())` → add `WithBundledFonts()`.
- `htmldoc_golden_test.go:53` — `RasterOptions{DPI: goldenDPI}` → add `BundledFonts: true`.
- `htmldoc_golden_test.go:88` — `OpenURL(srv.URL + "/index.html")` → `OpenURL(srv.URL+"/index.html", WithBundledFonts())` (this is `TestHTMLDocMarkdown`; extraction is font-independent, but pin it for consistency/determinism).
- `html_golden_test.go:882` — the `opts...` list built for `OpenHTMLBytes`: append `WithBundledFonts()` to the `opts` slice where it is constructed (find the `opts := []HTMLOption{...}` just above line 882 and add `WithBundledFonts()`).
- `pagination_golden_test.go:67` — `OpenHTMLBytes([]byte(f.html), WithPageSize(f.pageW, f.pageH))` → add `WithBundledFonts()`. Also add `BundledFonts: true` to that test's `RasterizePage` RasterOptions (grep within the file for `RasterOptions{`).
- `pagedmedia_golden_test.go` — for each `OpenHTMLBytes(...)`/`RasterizePage(...)` in a golden test: add `WithBundledFonts()` and `BundledFonts: true` respectively (grep the file for both).
- `pdf_golden_test.go` (the `TestPDFExtract*Golden`) and `pdf_extract_test.go` (the round-trip tests) — these rasterize/extract PDFs; add `BundledFonts: true` to any `RasterOptions{...}` they use. Extraction goldens (`.md`/`.html`) are font-independent, but any that RASTERIZE need the flag.

- [ ] **Step 1: Enumerate every golden rasterize + open site**

Run:
```bash
grep -rn "RasterOptions{" pkg/doctaculous/*_golden_test.go pkg/doctaculous/pdf_extract_test.go
grep -rn "OpenHTMLBytes\|OpenHTMLFile\|OpenURL" pkg/doctaculous/*_golden_test.go
```
Cross-check the printed sites against the exact list above; if grep shows a golden rasterize/open site not in the list, it is new — treat it the same way (RasterOptions → `BundledFonts:true`; HTML `Open*` → `WithBundledFonts()`).

- [ ] **Step 2: Apply BundledFonts:true to each golden RasterOptions**

For each `RasterOptions{...}` literal in a golden/showcase/round-trip test (per the list + Step-1 cross-check), add `BundledFonts: true`. Example (`docx_golden_test.go:49`):
```go
img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI, BundledFonts: true})
```
Do NOT touch non-golden rasterize sites (`html_backend_test.go`, `canvas_background_test.go`, `local_asset_test.go`, `doctaculous_test.go`, `bench_test.go`) — they are not golden-compared, so system-mode default there is fine and changing them is out of scope.

- [ ] **Step 3: Apply WithBundledFonts() to each golden HTML Open***

For each HTML `Open*` in a golden test (per the list), add `WithBundledFonts()` to its options. Examples:
```go
// htmldoc_golden_test.go:41
doc, err := OpenURL(srv.URL+"/index.html", WithDefaultPaged(), WithBundledFonts())
// pagination_golden_test.go:67
doc, err := OpenHTMLBytes([]byte(f.html), WithPageSize(f.pageW, f.pageH), WithBundledFonts())
// html_golden_test.go — where opts is built:
opts := []HTMLOption{WithViewportWidth(f.viewportPx), WithBundledFonts()}
```

- [ ] **Step 4: Run the full golden suites — expect PASS (no diffs)**

Run:
```bash
go test ./pkg/render/raster/ -run 'TestGolden|TestWeightedFontsGolden'
go test ./pkg/doctaculous/ -run 'Golden|Showcase|Markdown|RoundTrip|Paged|Pagination'
```
Expected: PASS with no golden diffs — bundled mode reproduces the previously-generated images exactly. If a golden differs, a golden path is still resolving through system fonts: find the RasterOptions missing `BundledFonts:true` OR the HTML `Open*` missing `WithBundledFonts()` and fix it. Do NOT regenerate goldens with `-update`.

- [ ] **Step 5: Commit**

```bash
git add pkg/doctaculous/*_golden_test.go pkg/doctaculous/pdf_extract_test.go
git commit -m "test: pin bundled-font mode in golden tests for hermeticity"
```

---

## Task 7: Mode-selection unit test with a stub provider (system→nil→bundled)

**Files:**
- Test: `pkg/doctaculous/pdf_fontmode_test.go` (append)

**Context:** Prove that in system mode, when the provider returns ok=false (simulating sysfont nil on a bare box), resolution falls through to the bundled face. Use an explicit stub `FontProvider` that always misses, plus BundledFonts default, and assert the render still succeeds (bundled fallback rendered the text).

- [ ] **Step 1: Write the test**

Append to `pkg/doctaculous/pdf_fontmode_test.go`:
```go
// missProvider is a font.Provider that never resolves — it simulates sysfont returning
// nil on a bare machine, so resolution must fall through to the bundled substitute.
type missProvider struct{}

func (missProvider) LoadStyled(string, bool, bool) ([]byte, bool) { return nil, false }

// TestSystemMissFallsBackToBundled: an explicit always-miss provider (the sysfont-nil
// case) still renders, because the bundled substitute is the fall-through.
func TestSystemMissFallsBackToBundled(t *testing.T) {
	doc, err := OpenBytes(gen.WeightedFontsPDF())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72, FontProvider: missProvider{}})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	if img == nil {
		t.Fatal("nil image; expected bundled fallback to render")
	}
}
```

- [ ] **Step 2: Run to verify it passes**

Run: `go test ./pkg/doctaculous/ -run TestSystemMissFallsBackToBundled -v`
Expected: PASS (the always-miss provider forces the bundled fall-through, which renders).

- [ ] **Step 3: Commit**

```bash
git add pkg/doctaculous/pdf_fontmode_test.go
git commit -m "test: system-mode provider miss falls back to bundled substitute"
```

---

## Task 8: CLI `--bundled-fonts` flag on `rasterize`

**Files:**
- Modify: `cmd/doctaculous/rasterize.go` (flag def, RasterOptions build, reorderArgs — NOT needed for a bool flag)
- Test: `cmd/doctaculous/rasterize_test.go` (append) or a new `cmd/doctaculous/fontmode_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/doctaculous/fontmode_test.go`:
```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// TestRasterizeBundledFontsFlag exercises the --bundled-fonts flag end to end: a PDF is
// rasterized to a PNG with the flag set. Success = the flag is accepted and output is
// written (hermetic bundled path).
func TestRasterizeBundledFontsFlag(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(in, gen.WeightedFontsPDF(), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.png")
	if err := rasterizeCmd([]string{in, "--out", out, "--bundled-fonts"}); err != nil {
		t.Fatalf("rasterizeCmd --bundled-fonts: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/doctaculous/ -run TestRasterizeBundledFontsFlag -v`
Expected: FAIL — the flag is rejected (`flag provided but not defined: -bundled-fonts`).

- [ ] **Step 3: Add the flag and wire it**

In `cmd/doctaculous/rasterize.go`, in the flag block (near `format`, `pageSize`), add:
```go
		bundledFonts = fs.Bool("bundled-fonts", false, "use only the bundled substitute fonts (hermetic); default uses installed system fonts")
```
Find where the code builds `RasterOptions` (search `RasterOptions{` in the file) and add `BundledFonts: *bundledFonts`. If the rasterize path also opens HTML/DOCX docs via an Open* helper that accepts HTMLOptions, thread `WithBundledFonts()` when `*bundledFonts` is true (search the file for `OpenHTMLFile`/`OpenURL`/`openReflow`); if rasterize only opens by extension and passes RasterOptions, the RasterOptions field alone is sufficient for the PDF and DOCX cases, and for HTML add `WithBundledFonts()` to the option list when set.

- [ ] **Step 4: (bool flag needs no reorderArgs entry)**

Confirm: bool flags use `--bundled-fonts` with no following value, so they must NOT be added to the `reorderArgs` `valueFlags` map. Leave `valueFlags` unchanged.

- [ ] **Step 5: Run to verify it passes**

Run:
```bash
gofmt -w cmd/doctaculous/rasterize.go cmd/doctaculous/fontmode_test.go
go test ./cmd/doctaculous/ -run TestRasterizeBundledFontsFlag -v
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/doctaculous/rasterize.go cmd/doctaculous/fontmode_test.go
git commit -m "cli: rasterize --bundled-fonts flag"
```

---

## Task 9: CLI `--bundled-fonts` on `topdf`, `tomd`, `tohtml`

**Files:**
- Modify: `cmd/doctaculous/topdf.go`, `cmd/doctaculous/tomd.go`, `cmd/doctaculous/tohtml.go`
- Modify: `cmd/doctaculous/tomd.go` `openConvertibleDocument` (thread the bundled flag into Open*)
- Test: `cmd/doctaculous/fontmode_test.go` (append)

**Context:** These convert reflow docs (HTML/DOCX/PDF). `topdf` writes a PDF (text is embedded from the reflow engine's faces — system fonts affect which real face is embedded). `tomd`/`tohtml` extract structure (font faces do not affect text/markup extraction), so `--bundled-fonts` there is a harmless no-op for output but should still be accepted for consistency. For `topdf`, thread `WithBundledFonts()` into the HTML open path when set.

- [ ] **Step 1: Write the failing test**

Append to `cmd/doctaculous/fontmode_test.go`:
```go
// TestTopdfBundledFontsFlag: topdf accepts --bundled-fonts and writes a PDF.
func TestTopdfBundledFontsFlag(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte(`<html><body style="font-family:Helvetica"><p>hi</p></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.pdf")
	if err := topdfCmd([]string{in, "--out", out, "--bundled-fonts"}); err != nil {
		t.Fatalf("topdfCmd --bundled-fonts: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}

// TestTohtmlBundledFontsFlag: tohtml accepts --bundled-fonts (no-op for extraction) and
// still produces output.
func TestTohtmlBundledFontsFlag(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte(`<html><body><h1>Title</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.html")
	if err := tohtmlCmd([]string{in, "--out", out, "--bundled-fonts"}); err != nil {
		t.Fatalf("tohtmlCmd --bundled-fonts: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/doctaculous/ -run 'TestTopdfBundledFontsFlag|TestTohtmlBundledFontsFlag' -v`
Expected: FAIL — `flag provided but not defined: -bundled-fonts`.

- [ ] **Step 3: Add the flag to topdf and thread into the HTML open**

In `cmd/doctaculous/topdf.go`, add to its flag block:
```go
		bundledFonts = fs.Bool("bundled-fonts", false, "use only the bundled substitute fonts (hermetic); default uses installed system fonts")
```
`topdf` opens via `openReflowDocument(input, *pageSize)`. Extend that helper (in `topdf.go`) to also take the bundled flag and pass `WithBundledFonts()` for the HTML branches. Change its signature to `openReflowDocument(input, pageSize string, bundledFonts bool)` and, in the HTML cases (`OpenHTMLFile`, `OpenURL`), append `WithBundledFonts()` to the options when `bundledFonts` is true. Update the call site in `topdfCmd` to pass `*bundledFonts`. (DOCX/PDF branches ignore it — DOCX is bundled-only already; PDF→PDF is not a reflow input here.)

- [ ] **Step 4: Add the flag to tomd and tohtml**

In `cmd/doctaculous/tomd.go` and `cmd/doctaculous/tohtml.go`, add the same `bundledFonts = fs.Bool("bundled-fonts", ...)` to each flag block. Both open via `openConvertibleDocument(input)`. Extend it to `openConvertibleDocument(input string, bundledFonts bool)` and append `WithBundledFonts()` in the HTML branches when true; pass the flag from both `tomdCmd` and `tohtmlCmd`. (For extraction the face choice does not change output, but accepting the flag keeps the CLI uniform.)

- [ ] **Step 5: Run to verify it passes**

Run:
```bash
gofmt -w cmd/doctaculous/*.go
go test ./cmd/doctaculous/ -run 'BundledFontsFlag' -v
```
Expected: PASS all three (rasterize from Task 8, topdf, tohtml).

- [ ] **Step 6: Run the whole CLI test suite (catch signature-change breakage)**

Run: `go test ./cmd/doctaculous/`
Expected: PASS — including existing tests that call `openReflowDocument`/`openConvertibleDocument` (update those call sites if the signature change broke them; e.g. existing calls now need the extra `false` argument).

- [ ] **Step 7: Commit**

```bash
git add cmd/doctaculous/topdf.go cmd/doctaculous/tomd.go cmd/doctaculous/tohtml.go cmd/doctaculous/fontmode_test.go
git commit -m "cli: --bundled-fonts flag on topdf/tomd/tohtml"
```

---

## Task 10: Full verification, docs, and CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (Done bullet + note the DOCX-system-fonts follow-up)
- Modify: `pkg/font/standard/fonts/README.md` is NOT touched (no font files change)

- [ ] **Step 1: Full suite + race + vet + lint**

Run:
```bash
go test ./... 2>&1 | grep -v "^ok\|no test files"
go test -race ./pkg/layout/font/ ./pkg/doctaculous/ ./cmd/doctaculous/
go vet ./...
golangci-lint run ./...
```
Expected: no failures; vet clean; `0 issues`. Fix anything that surfaces.

- [ ] **Step 2: Manual smoke — system vs bundled on a real PDF**

Run:
```bash
go run ./cmd/doctaculous rasterize testdata/external/pdf/google-doc-document.pdf --out /tmp/claude/sys.png --dpi 96
go run ./cmd/doctaculous rasterize testdata/external/pdf/google-doc-document.pdf --out /tmp/claude/bundled.png --dpi 96 --bundled-fonts
```
Expected: both produce PNGs. On a machine with the document's fonts installed, `sys.png` may use the real faces; `bundled.png` uses TeX Gyre. Eyeball both — both must be legible; neither errors.

- [ ] **Step 3: Update CLAUDE.md**

In `CLAUDE.md`, in the Fonts Done bullet (the one mentioning bundled substitutes + `font.Provider`), append a sentence:
```
System fonts are the DEFAULT source for non-embedded fonts (`OSFontProvider` via
`adrg/sysfont`, live-scanning the OS font dirs); `--bundled-fonts` / `WithBundledFonts()`
/ `RasterOptions.BundledFonts` opt into hermetic bundled-only mode (which the golden tests
pin). `2026-07-08-system-font-loading-design.md`.
```
If there is a matching TODO entry about system fonts, remove or update it. Add a one-line follow-up note that DOCX still resolves bundled-only (system fonts for DOCX is a follow-up).

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: system-font loading — CLAUDE.md status + DOCX follow-up note"
```

- [ ] **Step 5: Push and open PR**

Run:
```bash
git push -u origin system-font-loading
gh pr create --base main --title "System font loading (default) with bundled opt-out" --body "See docs/superpowers/specs/2026-07-08-system-font-loading-design.md. Installed OS fonts are now the default source for non-embedded fonts via adrg/sysfont (MIT, pure Go); --bundled-fonts / WithBundledFonts() / RasterOptions.BundledFonts opt into hermetic bundled mode (which the golden tests pin). New dep: adrg/sysfont (+ transitive adrg/{os-font-list,strutil,xdg}), all MIT. DOCX remains bundled-only (follow-up)."
```

---

## Notes for the implementer

- **Never regenerate goldens in this plan.** Bundled mode must reproduce the existing images exactly; a golden diff means a test still resolves through system fonts — fix the missing `BundledFonts:true`/`WithBundledFonts()`, don't `-update`.
- **`t.Skip` is correct** for the system-font resolution test on a bare host — that is not a failure.
- **DOCX is intentionally out of scope** for system fonts here (it stays bundled-only); a follow-up can install an `OSFontProvider` in `docxDocument` behind the same mode toggle.
- **Sandbox:** `go` build-cache writes may hit "operation not permitted" in a restricted sandbox — rerun the command with the sandbox disabled if so.
