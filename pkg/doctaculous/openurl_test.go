package doctaculous

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// A document served over (loopback) HTTP with a relative <link> stylesheet and a
// relative <img> renders without error and produces a single-page Document. The
// styled box proves the CSS loaded; the image proves the <img> decoded. This is
// the OpenURL smoke test: it proves the HTTP loader is wired through the pipeline.
func TestOpenURLRendersRemoteResources(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head>
			<link rel="stylesheet" href="style.css">
			</head><body><div class="card">Hi</div><img src="quad.png"></body></html>`))
	})
	mux.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte(`body{margin:0}.card{width:120px;height:40px;background:#cce5ff}`))
	})
	mux.HandleFunc("/quad.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(quadPNG(40)) // reuse the golden-test helper
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	doc, err := OpenURL(srv.URL + "/index.html")
	if err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	if doc == nil {
		t.Fatal("OpenURL returned nil document")
	}
	if doc.PageCount() != 1 {
		t.Errorf("PageCount = %d, want 1", doc.PageCount())
	}
}

// OpenURL rejects a non-http(s) scheme with ErrUnsupportedScheme (so callers can
// branch on it) and an empty URL with a clear error, both BEFORE any fetch.
func TestOpenURLRejectsBadInput(t *testing.T) {
	_, err := OpenURL("file:///etc/passwd")
	if !errors.Is(err, ErrUnsupportedScheme) {
		t.Errorf("file: scheme err = %v, want ErrUnsupportedScheme", err)
	}
	if _, err := OpenURL(""); err == nil {
		t.Error("OpenURL(\"\") returned nil error, want an error")
	}
}

// A 404 on a sub-resource (the <img> and the <link>) must degrade: the page still
// renders (placeholder image / no stylesheet), no error, no panic.
func TestOpenURLSubResource404Degrades(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head>
			<link rel="stylesheet" href="missing.css">
			</head><body><p>text</p><img src="missing.png"></body></html>`))
	})
	// No handlers for missing.css / missing.png → 404.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	doc, err := OpenURL(srv.URL + "/index.html")
	if err != nil {
		t.Fatalf("OpenURL degraded to an error, want graceful render: %v", err)
	}
	if doc == nil || doc.PageCount() != 1 {
		t.Fatal("want a single-page document despite 404 sub-resources")
	}
}

// A failed DOCUMENT fetch (the URL itself 404s) is a hard error — the document is
// mandatory, unlike a sub-resource.
func TestOpenURLDocument404Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	if _, err := OpenURL(srv.URL + "/nope.html"); err == nil {
		t.Fatal("OpenURL of a 404 document returned nil error, want an error")
	}
}

// Rendering a document via the HTTP loader must produce the identical raster to
// rendering it via an in-memory MapLoader with the same bytes — proving the HTTP
// path is a transparent byte source (no pixel difference), so the existing goldens
// cover its output without a new golden.
func TestOpenURLMatchesMapLoaderRender(t *testing.T) {
	const doc = `<!DOCTYPE html><html><head>
		<link rel="stylesheet" href="s.css">
		</head><body><div class="card">Same</div><img src="q.png"></body></html>`
	const css = `body{margin:0}.card{width:120px;height:40px;background:#cce5ff;border:3px solid #036}`
	png40 := quadPNG(40)

	// (a) Render over loopback HTTP via OpenURL.
	mux := http.NewServeMux()
	mux.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(doc))
	})
	mux.HandleFunc("/s.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte(css))
	})
	mux.HandleFunc("/q.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png40)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	httpDoc, err := OpenURL(srv.URL + "/index.html")
	if err != nil {
		t.Fatalf("OpenURL: %v", err)
	}

	// (b) Render the same bytes via an in-memory MapLoader.
	loader := resource.MapLoader{
		"s.css": {Data: []byte(css), ContentType: "text/css"},
		"q.png": {Data: png40, ContentType: "image/png"},
	}
	memDoc, err := OpenHTMLBytes([]byte(doc), WithResourceLoader(loader))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}

	httpPNG := rasterToPNG(t, httpDoc)
	memPNG := rasterToPNG(t, memDoc)
	if !bytes.Equal(httpPNG, memPNG) {
		t.Errorf("HTTP-loader render differs from MapLoader render (%d vs %d bytes)", len(httpPNG), len(memPNG))
	}
}

// rasterToPNG renders page 0 of doc at the golden DPI and returns its PNG bytes.
func rasterToPNG(t *testing.T, doc *Document) []byte {
	t.Helper()
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("image is %T, want *image.RGBA", img)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}
