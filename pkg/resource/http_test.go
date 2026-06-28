package resource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// mustURL parses a URL for tests, failing on error.
func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return u
}

func TestHTTPLoaderDataURIBase64(t *testing.T) {
	l := HTTPLoader{Base: mustURL(t, "http://example.com/doc.html")}
	// "p{color:red}" base64 = cHtjb2xvcjpyZWR9
	data, ct, err := l.Load(context.Background(), "data:text/css;base64,cHtjb2xvcjpyZWR9")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "p{color:red}" {
		t.Errorf("data = %q, want p{color:red}", data)
	}
	if ct != "text/css" {
		t.Errorf("contentType = %q, want text/css", ct)
	}
}

func TestHTTPLoaderDataURIPlain(t *testing.T) {
	l := HTTPLoader{Base: mustURL(t, "http://example.com/doc.html")}
	data, ct, err := l.Load(context.Background(), "data:text/plain,hello%20world")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("data = %q, want 'hello world'", data)
	}
	if ct != "text/plain" {
		t.Errorf("contentType = %q, want text/plain", ct)
	}
}

func TestHTTPLoaderDataURINoMediaType(t *testing.T) {
	l := HTTPLoader{Base: mustURL(t, "http://example.com/doc.html")}
	// "data:,A%20brief%20note" — no media type; default is text/plain;charset=US-ASCII.
	data, ct, err := l.Load(context.Background(), "data:,A%20brief%20note")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "A brief note" {
		t.Errorf("data = %q", data)
	}
	if ct != "text/plain;charset=US-ASCII" {
		t.Errorf("contentType = %q, want default text/plain;charset=US-ASCII", ct)
	}
}

func TestHTTPLoaderDataURIEmptyPayload(t *testing.T) {
	l := HTTPLoader{Base: mustURL(t, "http://example.com/doc.html")}
	// A comma is present but the payload is empty: a valid data: URI for zero bytes.
	data, ct, err := l.Load(context.Background(), "data:text/plain,")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("data = %q, want empty", data)
	}
	if ct != "text/plain" {
		t.Errorf("contentType = %q, want text/plain", ct)
	}
}

func TestHTTPLoaderDataURIMalformed(t *testing.T) {
	l := HTTPLoader{Base: mustURL(t, "http://example.com/doc.html")}
	// No comma → not a valid data: URI.
	if _, _, err := l.Load(context.Background(), "data:text/plain;base64"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	// Bad base64 payload.
	if _, _, err := l.Load(context.Background(), "data:text/css;base64,@@@notb64@@@"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad base64 err = %v, want ErrNotFound", err)
	}
}

func TestHTTPLoaderUnsupportedScheme(t *testing.T) {
	l := HTTPLoader{Base: mustURL(t, "http://example.com/doc.html")}
	if _, _, err := l.Load(context.Background(), "ftp://host/file"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound for ftp scheme", err)
	}
}

func TestHTTPLoaderFetchesRelative(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("a{}"))
	}))
	defer srv.Close()
	// Base is .../a/b/doc.html; "../style.css" must resolve to .../a/style.css.
	l := HTTPLoader{Base: mustURL(t, srv.URL+"/a/b/doc.html")}
	data, ct, err := l.Load(context.Background(), "../style.css")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if gotPath != "/a/style.css" {
		t.Errorf("server got path %q, want /a/style.css", gotPath)
	}
	if string(data) != "a{}" || ct != "text/css" {
		t.Errorf("got (%q,%q)", data, ct)
	}
}

func TestHTTPLoaderFetchesAbsoluteRef(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()
	// Base is one server; an absolute ref to the SAME server passes through unchanged.
	l := HTTPLoader{Base: mustURL(t, "http://other.invalid/doc.html")}
	if _, _, err := l.Load(context.Background(), srv.URL+"/abs.png"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if gotPath != "/abs.png" {
		t.Errorf("server got path %q, want /abs.png", gotPath)
	}
}

func TestHTTPLoaderNon2xxIsNotFound(t *testing.T) {
	for _, code := range []int{404, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		l := HTTPLoader{Base: mustURL(t, srv.URL+"/")}
		_, _, err := l.Load(context.Background(), "x.css")
		srv.Close()
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("status %d: err = %v, want ErrNotFound", code, err)
		}
	}
}

func TestHTTPLoaderSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789")) // 10 bytes
	}))
	defer srv.Close()
	// Cap below the body size → ErrNotFound.
	lSmall := HTTPLoader{Base: mustURL(t, srv.URL+"/"), MaxBytes: 5}
	if _, _, err := lSmall.Load(context.Background(), "big"); !errors.Is(err, ErrNotFound) {
		t.Errorf("over-limit err = %v, want ErrNotFound", err)
	}
	// Cap at exactly the body size → success.
	lExact := HTTPLoader{Base: mustURL(t, srv.URL+"/"), MaxBytes: 10}
	data, _, err := lExact.Load(context.Background(), "big")
	if err != nil {
		t.Fatalf("at-limit Load: %v", err)
	}
	if string(data) != "0123456789" {
		t.Errorf("data = %q", data)
	}
}

func TestHTTPLoaderFollowsRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("arrived"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	l := HTTPLoader{Base: mustURL(t, srv.URL+"/")}
	data, _, err := l.Load(context.Background(), "start")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "arrived" {
		t.Errorf("data = %q, want 'arrived' (redirect not followed)", data)
	}
}

func TestHTTPLoaderHonorsContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // block until the client cancels
	}))
	defer srv.Close()
	l := HTTPLoader{Base: mustURL(t, srv.URL+"/")}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := l.Load(ctx, "slow")
	if err == nil {
		t.Fatal("Load returned nil error on a cancelled context, want non-nil")
	}
}
