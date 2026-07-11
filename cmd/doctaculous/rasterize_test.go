package main

import (
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
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
	got := reorderArgs([]string{"in.pdf", "--out", "o.png", "--dpi", "150"}, rasterizeValueFlags)
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

// TestRasterizeCmdHTMLPaginatesByDefault asserts an HTML document taller than one
// US-Letter page renders to MULTIPLE numbered pages by default (page-size defaults to
// "letter"), and that --page-size tall opts into a single tall page.
func TestRasterizeCmdHTMLPaginatesByDefault(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "tall.html")
	// ~120 stacked blocks force the content well past a single Letter page.
	body := ""
	for i := 0; i < 120; i++ {
		body += `<p style="height:40px;margin:0">line</p>`
	}
	html := `<!DOCTYPE html><html><head><style>body{margin:0}</style></head><body>` + body + `</body></html>`
	if err := os.WriteFile(in, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}

	// Default (no --page-size): paginates → more than one page.
	pat := filepath.Join(dir, "def-%d.png")
	if err := rasterizeCmd([]string{in, "--out", pat, "--pages", "all", "--dpi", "72"}); err != nil {
		t.Fatalf("rasterizeCmd default: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "def-2.png")); err != nil {
		t.Errorf("default should paginate to >1 page; def-2.png missing: %v", err)
	}

	// --page-size tall: one tall page only.
	tallOut := filepath.Join(dir, "tall-%d.png")
	if err := rasterizeCmd([]string{in, "--out", tallOut, "--pages", "all", "--dpi", "72", "--page-size", "tall"}); err != nil {
		t.Fatalf("rasterizeCmd tall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tall-1.png")); err != nil {
		t.Errorf("tall-1.png missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tall-2.png")); err == nil {
		t.Error("--page-size tall should produce only one page, but tall-2.png exists")
	}
}

// TestRasterizeCmdRejectsBadPageSize asserts an unknown --page-size errors.
func TestRasterizeCmdRejectsBadPageSize(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "x.html")
	if err := os.WriteFile(in, []byte("<p>x</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := rasterizeCmd([]string{in, "--out", filepath.Join(dir, "o.png"), "--page-size", "a4"})
	if err == nil {
		t.Fatal("expected an error for --page-size a4")
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
	// --page-size tall forces the single-tall-page path (the default is now "letter",
	// which paginates); a one-line document is one tall page either way.
	if err := rasterizeCmd([]string{srv.URL, "--out", out, "--pages", "all", "--dpi", "72", "--page-size", "tall"}); err != nil {
		t.Fatalf("rasterizeCmd(URL): %v", err)
	}
	// A single tall page → page-1.png must exist and be non-empty.
	got := filepath.Join(filepath.Dir(out), "page-1.png")
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("expected rendered %s: %v", got, err)
	}
	if info.Size() == 0 {
		t.Errorf("rendered %s is empty", got)
	}
}

// TestRasterizeCmdFitSizing exercises --max-width/--max-height end to end: an
// unset --dpi means pure fit (the letter page fills the 480x360 box at 279x360),
// while an explicit --dpi acts as a resolution ceiling.
func TestRasterizeCmdFitSizing(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "letter.pdf")
	if err := os.WriteFile(in, gen.TextPDF(), 0o644); err != nil {
		t.Fatal(err)
	}

	fit := filepath.Join(dir, "fit.png")
	if err := rasterizeCmd([]string{in, "--out", fit, "--max-width", "480", "--max-height", "360", "--bundled-fonts"}); err != nil {
		t.Fatalf("rasterizeCmd fit: %v", err)
	}
	if w, h := decodePNGSize(t, fit); w != 279 || h != 360 {
		t.Errorf("fit render = %dx%d, want 279x360", w, h)
	}

	// An explicit --dpi is a ceiling: 72 DPI on a 612x792pt page is 612x792 px,
	// well inside a huge box — no upscale to fill it.
	capped := filepath.Join(dir, "capped.png")
	if err := rasterizeCmd([]string{in, "--out", capped, "--max-width", "10000", "--max-height", "10000", "--dpi", "72", "--bundled-fonts"}); err != nil {
		t.Fatalf("rasterizeCmd capped: %v", err)
	}
	if w, h := decodePNGSize(t, capped); w != 612 || h != 792 {
		t.Errorf("dpi-ceiling render = %dx%d, want 612x792", w, h)
	}

	// Negative values are rejected.
	if err := rasterizeCmd([]string{in, "--out", fit, "--max-width", "-1"}); err == nil {
		t.Error("expected an error for --max-width -1")
	}
}

// decodePNGSize decodes the PNG at path and returns its pixel dimensions.
func decodePNGSize(t *testing.T, path string) (w, h int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only file
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return img.Bounds().Dx(), img.Bounds().Dy()
}
