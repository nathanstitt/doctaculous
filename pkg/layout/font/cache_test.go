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
