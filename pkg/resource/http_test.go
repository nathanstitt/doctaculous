package resource

import (
	"context"
	"errors"
	"net/url"
	"testing"
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
