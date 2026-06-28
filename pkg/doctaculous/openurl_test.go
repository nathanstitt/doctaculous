package doctaculous

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
