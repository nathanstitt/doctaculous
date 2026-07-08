package font

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// countingLoader wraps a MapLoader and counts Load calls (to prove no re-fetch).
type countingLoader struct {
	inner resource.MapLoader
	calls int32
}

func (c *countingLoader) Load(ctx context.Context, ref string) ([]byte, string, error) {
	atomic.AddInt32(&c.calls, 1)
	return c.inner.Load(ctx, ref)
}

func fontsDir() string { return filepath.Join("..", "..", "..", "testdata", "fonts") }

func TestResolveDownloadedFace(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join(fontsDir(), "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	loader := &countingLoader{inner: resource.MapLoader{"my.ttf": {Data: ttf}}}
	faces := []gcss.FontFace{{Family: "My Face", Sources: []gcss.FontSource{{URL: "my.ttf"}}}}
	c := NewFaceCacheWithFonts(faces, loader, nil, nil)

	face, ok := c.Resolve("My Face", pkgfont.Style{})
	if !ok || face == nil {
		t.Fatalf("Resolve(My Face) ok=%v face=%v, want the downloaded face", ok, face)
	}
	// Second resolve must hit the cache, not re-fetch.
	if _, ok := c.Resolve("My Face", pkgfont.Style{}); !ok {
		t.Fatal("second Resolve(My Face) missed")
	}
	if got := atomic.LoadInt32(&loader.calls); got != 1 {
		t.Fatalf("loader called %d times, want 1 (cached)", got)
	}
}

func TestResolveUnknownFamilyFallsBackToBundled(t *testing.T) {
	c := NewFaceCacheWithFonts(nil, nil, nil, nil)
	// "Arial" is a base-14 alias -> LoadStandard returns a bundled face.
	if _, ok := c.Resolve("Arial", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(Arial) miss, want the bundled substitute")
	}
}

func TestResolveFetchFailureCachesFallback(t *testing.T) {
	// @font-face points at a missing url; family is also a base-14 alias so the
	// fallback succeeds and must be cached (no re-fetch on the 2nd call).
	loader := &countingLoader{inner: resource.MapLoader{}} // empty -> 404
	faces := []gcss.FontFace{{Family: "Arial", Sources: []gcss.FontSource{{URL: "missing.ttf"}}}}
	c := NewFaceCacheWithFonts(faces, loader, nil, nil)

	if _, ok := c.Resolve("Arial", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(Arial w/ bad @font-face) miss, want bundled fallback")
	}
	c.Resolve("Arial", pkgfont.Style{})
	if got := atomic.LoadInt32(&loader.calls); got != 1 {
		t.Fatalf("loader called %d times, want 1 (negative result cached)", got)
	}
}

func TestResolveLocalViaSystemProvider(t *testing.T) {
	// local("webfont") resolves via a DiskFontProvider; no url() needed.
	faces := []gcss.FontFace{{Family: "Local Face", Sources: []gcss.FontSource{{Local: "webfont"}}}}
	c := NewFaceCacheWithFonts(faces, nil, DiskFontProvider{Dir: fontsDir()}, nil)
	if _, ok := c.Resolve("Local Face", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(Local Face) miss, want the local disk font")
	}
}

func TestNewFaceCacheUnchanged(t *testing.T) {
	// The bundled-only constructor must still resolve base-14 and miss unknowns.
	c := NewFaceCache()
	if _, ok := c.Resolve("Arial", pkgfont.Style{}); !ok {
		t.Fatal("NewFaceCache Resolve(Arial) miss, want bundled")
	}
	if _, ok := c.Resolve("Totally Unknown Family XYZ", pkgfont.Style{}); ok {
		t.Fatal("NewFaceCache Resolve(unknown) hit, want miss")
	}
}

// Missing variant: @font-face supplies only regular; a bold request still resolves
// (downloaded face reused as the coarse best match, or bundled fallback) — no panic.
func TestResolveMissingVariantResolves(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join(fontsDir(), "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	loader := &countingLoader{inner: resource.MapLoader{"r.ttf": {Data: ttf}}}
	faces := []gcss.FontFace{{Family: "Display", Sources: []gcss.FontSource{{URL: "r.ttf"}}}}
	c := NewFaceCacheWithFonts(faces, loader, nil, nil)
	if _, ok := c.Resolve("Display", pkgfont.Style{}); !ok {
		t.Fatal("regular miss, want the downloaded face")
	}
	if _, ok := c.Resolve("Display", pkgfont.Style{Bold: true}); !ok {
		t.Fatal("bold miss, want a resolved face (downloaded face reused for the missing variant)")
	}
}

// recordingLoader records every ref requested (to prove which @font-face entry won).
type recordingLoader struct {
	inner resource.MapLoader
	refs  []string
}

func (r *recordingLoader) Load(ctx context.Context, ref string) ([]byte, string, error) {
	r.refs = append(r.refs, ref)
	return r.inner.Load(ctx, ref)
}

func TestResolveBestFirstPicksMatchingStyle(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join(fontsDir(), "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	loader := &recordingLoader{inner: resource.MapLoader{
		"reg.ttf":  {Data: ttf},
		"bold.ttf": {Data: ttf},
	}}
	faces := []gcss.FontFace{
		{Family: "Two", Sources: []gcss.FontSource{{URL: "reg.ttf"}}},
		{Family: "Two", Weight: "bold", Sources: []gcss.FontSource{{URL: "bold.ttf"}}},
	}
	c := NewFaceCacheWithFonts(faces, loader, nil, nil)
	if _, ok := c.Resolve("Two", pkgfont.Style{Bold: true}); !ok {
		t.Fatal("bold resolve miss")
	}
	if _, ok := c.Resolve("Two", pkgfont.Style{}); !ok {
		t.Fatal("regular resolve miss")
	}
	// The bold request must have fetched bold.ttf; the regular request reg.ttf.
	var sawBold, sawReg bool
	for _, ref := range loader.refs {
		if ref == "bold.ttf" {
			sawBold = true
		}
		if ref == "reg.ttf" {
			sawReg = true
		}
	}
	if !sawBold || !sawReg {
		t.Fatalf("expected both bold.ttf and reg.ttf fetched (best-style-first); got refs=%v", loader.refs)
	}
}

// Case/whitespace variants of the same family share one cache entry (one fetch).
func TestResolveFamilyCaseVariantsShareCache(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join(fontsDir(), "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	loader := &countingLoader{inner: resource.MapLoader{"u.ttf": {Data: ttf}}}
	faces := []gcss.FontFace{{Family: "Web Face", Sources: []gcss.FontSource{{URL: "u.ttf"}}}}
	c := NewFaceCacheWithFonts(faces, loader, nil, nil)
	if _, ok := c.Resolve("Web Face", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(Web Face) miss")
	}
	if _, ok := c.Resolve("web face", pkgfont.Style{}); !ok { // different case
		t.Fatal("Resolve(web face) miss")
	}
	if _, ok := c.Resolve("  Web Face  ", pkgfont.Style{}); !ok { // whitespace
		t.Fatal("Resolve(padded) miss")
	}
	if got := atomic.LoadInt32(&loader.calls); got != 1 {
		t.Fatalf("loader called %d times across 3 case/space variants, want 1 (shared cache)", got)
	}
}

// local() with no provider skips to the next src (a url()).
func TestResolveLocalNoProviderFallsToURL(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join(fontsDir(), "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	loader := &countingLoader{inner: resource.MapLoader{"u.ttf": {Data: ttf}}}
	faces := []gcss.FontFace{{Family: "X", Sources: []gcss.FontSource{{Local: "nope"}, {URL: "u.ttf"}}}}
	c := NewFaceCacheWithFonts(faces, loader, nil, nil) // nil provider -> local() can't match
	if _, ok := c.Resolve("X", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(X) miss, want the url() fallback after the local() skip")
	}
}

// A font-family fallback list whose first name(s) resolve to nothing falls through
// to a later candidate (here a generic keyword), instead of skipping the run. This
// is the CSS font-family fallback behavior the cascade now preserves end to end.
func TestResolveFallbackListSkipsUnresolvable(t *testing.T) {
	c := NewFaceCache()
	// Neither name is a bundled alias; "serif" terminates the chain with Termes.
	if _, ok := c.Resolve("TeX Gyre Termes, Nonesuch, serif", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(list ending in serif) miss, want the generic-keyword fallback")
	}
	// With no resolvable candidate at all, the whole list misses (caller skips).
	if _, ok := c.Resolve("Nonesuch One, Nonesuch Two", pkgfont.Style{}); ok {
		t.Fatal("Resolve(all-unresolvable list) hit, want miss")
	}
}

// stubProvider is a SystemFontProvider that also implements pkgfont.Provider, serving
// fixed sfnt bytes per (family, style) so a test can prove the provider route wins over
// the bundled fallback and that style selects the right bytes. LoadLocal is unused here
// (the provider is consulted via the style-aware route, not @font-face local()).
type stubProvider struct {
	regular []byte
	bold    []byte
	calls   []string // records the families requested, for ordering assertions
}

func (s *stubProvider) LoadLocal(name string) ([]byte, bool) { return nil, false }

func (s *stubProvider) LoadStyled(family string, bold, italic bool) ([]byte, bool) {
	s.calls = append(s.calls, family)
	if bold {
		if s.bold == nil {
			return nil, false
		}
		return s.bold, true
	}
	if s.regular == nil {
		return nil, false
	}
	return s.regular, true
}

// An injected Provider resolves a family BEFORE the bundled fallback: even for a
// base-14 alias like Arial (which LoadStandard would resolve), the provider's face wins.
func TestResolveProviderBeatsBundled(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join(fontsDir(), "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	prov := &stubProvider{regular: ttf}
	c := NewFaceCacheWithFonts(nil, nil, prov, nil)

	// Arial IS a bundled alias, so a hit here that consulted the provider proves the
	// provider route runs before LoadStandard.
	if _, ok := c.Resolve("Arial", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(Arial) miss, want the provider face")
	}
	if len(prov.calls) == 0 || prov.calls[0] != "Arial" {
		t.Fatalf("provider not consulted first for Arial; calls=%v", prov.calls)
	}
}

// The provider route is style-aware: a bold request calls LoadStyled with bold=true and
// resolves, distinct from the regular request.
func TestResolveProviderStyleAware(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join(fontsDir(), "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Provider serves only a bold face: a regular request must miss the provider and
	// fall through to the bundled Arial substitute (still ok), a bold request hits it.
	prov := &stubProvider{bold: ttf}
	c := NewFaceCacheWithFonts(nil, nil, prov, nil)

	if _, ok := c.Resolve("Arial", pkgfont.Style{Bold: true}); !ok {
		t.Fatal("Resolve(Arial, bold) miss, want the provider's bold face")
	}
	if _, ok := c.Resolve("Arial", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(Arial, regular) miss, want the bundled fallback after provider skip")
	}
}

// A provider that returns bytes LoadSFNT cannot decode (here, garbage) is logged and
// skipped, and resolution falls through to the bundled substitute rather than failing.
func TestResolveProviderNonSFNTFallsThrough(t *testing.T) {
	prov := &stubProvider{regular: []byte("not a font program")}
	c := NewFaceCacheWithFonts(nil, nil, prov, nil)
	if _, ok := c.Resolve("Arial", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(Arial) miss, want the bundled fallback after the undecodable provider bytes")
	}
}

// plainSystemProvider implements only SystemFontProvider (LoadLocal), NOT pkgfont.Provider,
// so the style-aware provider route's type assertion must skip it entirely.
type plainSystemProvider struct{}

func (plainSystemProvider) LoadLocal(name string) ([]byte, bool) { return nil, false }

// A plain SystemFontProvider (LoadLocal only, no LoadStyled) leaves the provider route
// inert (the type assertion to pkgfont.Provider fails): resolution still works via the
// bundled fallback, unchanged from before this change.
func TestResolvePlainSystemProviderUnchanged(t *testing.T) {
	c := NewFaceCacheWithFonts(nil, nil, plainSystemProvider{}, nil)
	if _, ok := c.Resolve("Arial", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(Arial) miss, want the bundled fallback with a plain (LoadLocal-only) provider")
	}
}

// A downloaded @font-face named later in the fallback list is used when the earlier
// candidates do not resolve — the resolver tries @font-face per candidate, not just
// for the first name.
func TestResolveFallbackListReachesFontFace(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join(fontsDir(), "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	loader := &countingLoader{inner: resource.MapLoader{"d.ttf": {Data: ttf}}}
	faces := []gcss.FontFace{{Family: "Downloaded", Sources: []gcss.FontSource{{URL: "d.ttf"}}}}
	c := NewFaceCacheWithFonts(faces, loader, nil, nil)
	// "Nonesuch" has no bundled substitute and no @font-face; "Downloaded" does.
	if _, ok := c.Resolve("Nonesuch, Downloaded, serif", pkgfont.Style{}); !ok {
		t.Fatal("Resolve(list reaching @font-face) miss, want the downloaded face")
	}
	if got := atomic.LoadInt32(&loader.calls); got != 1 {
		t.Fatalf("loader called %d times, want 1 (the Downloaded candidate fetched once)", got)
	}
	// The whole list string keys one cache entry: a repeat does not re-fetch.
	c.Resolve("Nonesuch, Downloaded, serif", pkgfont.Style{})
	if got := atomic.LoadInt32(&loader.calls); got != 1 {
		t.Fatalf("loader called %d times after repeat, want 1 (list cached)", got)
	}
}
