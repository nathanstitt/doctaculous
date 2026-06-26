package doctaculous

import (
	"os"
	"path/filepath"
	"testing"

	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// A page whose only font is an @font-face family served by a MapLoader must render
// without error and produce a Document. (The golden test proves the glyphs
// visually; this is the integration smoke test that the chain is wired.)
func TestOpenHTMLWithWebFont(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fonts", "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	html := []byte(`<!DOCTYPE html><html><head><style>
		@font-face { font-family: "Web Face"; src: url(web.ttf) }
		body { margin: 0; font-family: "Web Face"; font-size: 40px }
	</style></head><body>Web font AaGg</body></html>`)
	loader := resource.MapLoader{"web.ttf": {Data: ttf}}
	doc, err := OpenHTMLBytes(html, WithResourceLoader(loader))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	if doc == nil {
		t.Fatal("OpenHTMLBytes returned nil document")
	}
}

// WithSystemFontProvider compiles and is accepted as an option.
func TestWithSystemFontProviderOption(t *testing.T) {
	html := []byte(`<!DOCTYPE html><html><body>hi</body></html>`)
	doc, err := OpenHTMLBytes(html,
		WithSystemFontProvider(layoutfont.DiskFontProvider{Dir: filepath.Join("..", "..", "testdata", "fonts")}))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	if doc == nil {
		t.Fatal("nil document")
	}
}

// A 404 font url + a non-base-14 family: the run degrades (no panic); the document
// still renders. (Text may be blank since no bundled substitute exists for the
// made-up family — the documented graceful skip.)
func TestWebFont404Degrades(t *testing.T) {
	html := []byte(`<!DOCTYPE html><html><head><style>
		@font-face { font-family: "Ghost"; src: url(missing.woff2) }
		p { font-family: "Ghost" }
	</style></head><body><p>nothing to see</p></body></html>`)
	loader := resource.MapLoader{} // 404 for everything
	if _, err := OpenHTMLBytes(html, WithResourceLoader(loader)); err != nil {
		t.Fatalf("OpenHTMLBytes degraded to an error, want graceful render: %v", err)
	}
}

// A corrupt font payload degrades to the bundled fallback (family is a base-14
// alias), no panic.
func TestWebFontCorruptDegrades(t *testing.T) {
	html := []byte(`<!DOCTYPE html><html><head><style>
		@font-face { font-family: "Arial"; src: url(bad.woff2) }
		p { font-family: "Arial" }
	</style></head><body><p>fallback please</p></body></html>`)
	loader := resource.MapLoader{"bad.woff2": {Data: []byte("wOF2 not really a font")}}
	if _, err := OpenHTMLBytes(html, WithResourceLoader(loader)); err != nil {
		t.Fatalf("corrupt web font caused an error, want graceful fallback: %v", err)
	}
}

// Deferred descriptors present but ignored: the font still resolves and renders.
func TestWebFontIgnoredDescriptors(t *testing.T) {
	ttf, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fonts", "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	html := []byte(`<!DOCTYPE html><html><head><style>
		@font-face {
			font-family: "Web Face"; src: url(web.ttf);
			unicode-range: U+0000-00FF; font-display: swap;
			font-variation-settings: "wght" 700;
		}
		p { font-family: "Web Face"; font-size: 30px }
	</style></head><body><p>AaGg</p></body></html>`)
	loader := resource.MapLoader{"web.ttf": {Data: ttf}}
	if _, err := OpenHTMLBytes(html, WithResourceLoader(loader)); err != nil {
		t.Fatalf("ignored descriptors caused an error: %v", err)
	}
}

// A corrupt WOFF1 payload also degrades (bundled fallback, no panic).
func TestWebFontCorruptWOFF1Degrades(t *testing.T) {
	html := []byte(`<!DOCTYPE html><html><head><style>
		@font-face { font-family: "Arial"; src: url(bad.woff) }
		p { font-family: "Arial" }
	</style></head><body><p>x</p></body></html>`)
	loader := resource.MapLoader{"bad.woff": {Data: []byte("wOFF\x00\x01\x00\x00 garbage")}}
	if _, err := OpenHTMLBytes(html, WithResourceLoader(loader)); err != nil {
		t.Fatalf("corrupt WOFF1 caused an error, want graceful fallback: %v", err)
	}
}
