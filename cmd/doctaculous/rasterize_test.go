package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolvePagesSingle(t *testing.T) {
	got, err := resolvePages("", 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{1}) { // 1-based 2 -> 0-based 1
		t.Errorf("got %v, want [1]", got)
	}
}

func TestResolvePagesRange(t *testing.T) {
	got, err := resolvePages("1-3,5", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 1, 2, 4}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolvePagesReversedRange(t *testing.T) {
	got, err := resolvePages("3-1", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Errorf("got %v, want [0 1 2]", got)
	}
}

func TestResolvePagesOutOfRange(t *testing.T) {
	for _, spec := range []struct {
		rng  string
		page int
	}{
		{"", 9},
		{"0", 1},
		{"4-7", 1},
		{"abc", 1},
	} {
		if _, err := resolvePages(spec.rng, spec.page, 5); err == nil {
			t.Errorf("resolvePages(%q,%d,5) expected error", spec.rng, spec.page)
		}
	}
}

func TestResolvePagesDedup(t *testing.T) {
	got, err := resolvePages("1-3,2-4", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 1, 2, 3} // pages 1,2,3,4 deduped, first-seen order
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestOutputPath(t *testing.T) {
	if got := outputPath("page-%d.png", 4); got != "page-5.png" {
		t.Errorf("%%d output = %q, want page-5.png", got)
	}
	if got := outputPath("out.png", 0); got != "out.png" {
		t.Errorf("no-%%d output = %q, want out.png", got)
	}
	// A %d pattern with a single page still substitutes (no literal "%d" in the name).
	if got := outputPath("page-%d.png", 0); got != "page-1.png" {
		t.Errorf("single-page %%d output = %q, want page-1.png", got)
	}
}

func TestReorderArgs(t *testing.T) {
	// Input before flags should be moved to the end.
	got := reorderArgs([]string{"in.pdf", "--out", "o.png", "--dpi", "150"})
	want := []string{"--out", "o.png", "--dpi", "150", "in.pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reorderArgs = %v, want %v", got, want)
	}
}

func TestIsHTTPURL(t *testing.T) {
	cases := map[string]bool{
		"http://example.com":    true,
		"https://example.com/p": true,
		"https://x/file.pdf":    true, // a URL ending in .pdf is still a URL
		"page.html":             false,
		"in.pdf":                false,
		"/abs/path.html":        false,
		"file:///etc/hosts":     false,
		"ftp://host/x":          false,
		"":                      false,
		"httpsomething.html":    false, // not an http(s):// scheme
	}
	for input, want := range cases {
		if got := isHTTPURL(input); got != want {
			t.Errorf("isHTTPURL(%q) = %v, want %v", input, got, want)
		}
	}
}

// TestRasterizeCmdURL drives the rasterize subcommand end to end against a loopback
// HTTP server serving HTML, proving an http:// URL input is fetched (OpenURL) and
// rendered to a PNG. Hermetic via net/http/httptest.
func TestRasterizeCmdURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body style="margin:0">` +
			`<p style="color:black">Hello from a URL</p></body></html>`))
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "page-%d.png")
	if err := rasterizeCmd([]string{srv.URL, "--out", out, "--pages", "all", "--dpi", "72"}); err != nil {
		t.Fatalf("rasterizeCmd(URL): %v", err)
	}
	// A single tall page (no --page-size) → page-1.png must exist and be non-empty.
	got := filepath.Join(filepath.Dir(out), "page-1.png")
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("expected rendered %s: %v", got, err)
	}
	if info.Size() == 0 {
		t.Errorf("rendered %s is empty", got)
	}
}
