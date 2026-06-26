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
