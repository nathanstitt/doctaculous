package css

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/resource"
	"github.com/nathanstitt/doctaculous/pkg/svg"
)

const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20"/></svg>`

// TestSVGCacheOnlyClaimsTheSVGContentType is the guard that keeps the vector path
// from swallowing raster images. Discrimination is by CONTENT TYPE, never by
// sniffing bytes: an empty content type must fall through to the raster path,
// because otherwise every unrecognized binary blob a document references would be
// fed to an XML parser.
func TestSVGCacheOnlyClaimsTheSVGContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		data        string
		wantOK      bool
		wantWasSVG  bool
	}{
		{"image/svg+xml", "image/svg+xml", testSVG, true, true},
		{"with a charset parameter", "image/svg+xml; charset=utf-8", testSVG, true, true},
		{"mixed case", "Image/SVG+XML", testSVG, true, true},
		// SVG bytes served without a type belong to the raster path, which sniffs
		// and degrades them gracefully. wasSVG must be false so the caller routes
		// them there rather than reporting a broken SVG.
		{"svg bytes, no content type", "", testSVG, false, false},
		{"png content type", "image/png", testSVG, false, false},
		// A ref that IS declared SVG but does not parse: a miss, but wasSVG true,
		// so the caller degrades to an empty vector box instead of handing the
		// bytes to image.Decode and reporting an image failure.
		{"declared svg, unparseable", "image/svg+xml", "\x00not markup", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newSVGCache(resource.MapLoader{"r": {Data: []byte(tc.data), ContentType: tc.contentType}}, nil)
			got := c.get(context.Background(), "r")
			if got.ok != tc.wantOK {
				t.Errorf("ok = %v, want %v", got.ok, tc.wantOK)
			}
			if got.wasSVG != tc.wantWasSVG {
				t.Errorf("wasSVG = %v, want %v", got.wasSVG, tc.wantWasSVG)
			}
			if got.ok && got.doc == nil {
				t.Error("ok=true but doc is nil")
			}
		})
	}
}

// TestSVGCacheFetchesEachRefOnce proves the memo works. Without it, every
// intrinsic-width measurement and every fragment build would re-fetch and
// re-parse the same SVG — and intrinsic measurement runs the box repeatedly for
// table/flex/grid sizing.
func TestSVGCacheFetchesEachRefOnce(t *testing.T) {
	var mu sync.Mutex
	var loads int
	c := newSVGCache(countingLoader{
		inner: resource.MapLoader{"r": {Data: []byte(testSVG), ContentType: "image/svg+xml"}},
		hit:   func() { mu.Lock(); loads++; mu.Unlock() },
	}, nil)
	for range 5 {
		if !c.get(context.Background(), "r").ok {
			t.Fatal("miss")
		}
	}
	if loads != 1 {
		t.Errorf("loaded %d times, want 1", loads)
	}
}

// TestSVGCacheCachesPermanentMissesButNotCancellations mirrors imageCache's
// contract: a broken ref must not be re-fetched on every reference, but a miss
// caused by a cancelled context must not poison the cache — a later render with a
// live context has to be able to succeed.
func TestSVGCacheCachesPermanentMissesButNotCancellations(t *testing.T) {
	var loads int
	loader := countingLoader{
		inner: resource.MapLoader{"broken": {Data: []byte("nope"), ContentType: "image/svg+xml"}},
		hit:   func() { loads++ },
	}

	c := newSVGCache(loader, nil)
	for range 3 {
		c.get(context.Background(), "broken")
	}
	if loads != 1 {
		t.Errorf("a permanent miss was re-fetched: %d loads, want 1", loads)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	c2 := newSVGCache(resource.MapLoader{"r": {Data: []byte(testSVG), ContentType: "image/svg+xml"}}, nil)
	if c2.get(cancelled, "r").ok {
		t.Fatal("a cancelled load reported success")
	}
	if !c2.get(context.Background(), "r").ok {
		t.Error("a transient cancellation miss was cached; a later live-context render must still succeed")
	}
}

// countingLoader wraps a loader and counts Load calls.
type countingLoader struct {
	inner resource.ResourceLoader
	hit   func()
}

func (l countingLoader) Load(ctx context.Context, ref string) ([]byte, string, error) {
	l.hit()
	return l.inner.Load(ctx, ref)
}

// TestSVGCacheRefusesOversizedSource proves the DoS bound. An <img src> is
// untrusted input, and pkg/svg's own budgets (the <use> instantiation budget, the
// <text> character budget) bound EXPANSION, not the size of the source document
// itself — a several-hundred-megabyte SVG would still be built into a scene tree
// before any of them applied.
func TestSVGCacheRefusesOversizedSource(t *testing.T) {
	// A structurally valid SVG padded past the cap with comment bytes.
	head := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><!--`
	tail := `--><rect width="10" height="10"/></svg>`
	huge := head + strings.Repeat("x", maxSVGBytes) + tail

	var logs []string
	c := newSVGCache(
		resource.MapLoader{"big": {Data: []byte(huge), ContentType: "image/svg+xml"}},
		func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) },
	)
	if got := c.get(context.Background(), "big"); got.ok {
		t.Error("an over-cap SVG parsed; the size bound did not apply")
	}
	if len(logs) == 0 || !strings.Contains(strings.Join(logs, "\n"), "over the") {
		t.Errorf("the refusal was silent; want a degradation log. got: %v", logs)
	}
}

// TestSVGIntrinsicUsesRatioNotTheDefault covers the sizing translation in
// isolation: what an embedded SVG contributes to replaced-element sizing.
func TestSVGIntrinsicUsesRatioNotTheDefault(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		wantW  float64
		wantH  float64
		wantOK bool
	}{
		{"explicit size", `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="60"/>`, 120, 60, true},
		{"viewBox only reports the ratio", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"/>`, 100, 50, true},
		{"width plus viewBox derives the height", `<svg xmlns="http://www.w3.org/2000/svg" width="80" viewBox="0 0 4 1"/>`, 80, 20, true},
		{"height plus viewBox derives the width", `<svg xmlns="http://www.w3.org/2000/svg" height="20" viewBox="0 0 4 1"/>`, 80, 20, true},
		// The case the whole accessor exists for: no facts at all, so the CALLER
		// applies 300x150 — svgIntrinsic must not silently supply it.
		{"nothing stated", `<svg xmlns="http://www.w3.org/2000/svg"/>`, 0, 0, false},
		{"width only, no ratio", `<svg xmlns="http://www.w3.org/2000/svg" width="80"/>`, 80, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := svg.Parse([]byte(tc.src), nil)
			if err != nil {
				t.Fatal(err)
			}
			w, h, ok := svgIntrinsic(doc)
			if w != tc.wantW || h != tc.wantH || ok != tc.wantOK {
				t.Errorf("svgIntrinsic = (%g, %g, %v), want (%g, %g, %v)",
					w, h, ok, tc.wantW, tc.wantH, tc.wantOK)
			}
		})
	}
}

// TestFitSceneToLeavesAMatchingBoxUnwrapped proves the scale adapter is applied
// only when it changes something. An unsized <img src="x.svg"> lays out at the
// SVG's own viewport, and in that case the scene must be handed to the painter
// exactly as the standalone SVG frontend hands it over — same object, so the
// drawn output is bit-for-bit identical to the standalone path.
func TestFitSceneToLeavesAMatchingBoxUnwrapped(t *testing.T) {
	doc, err := svg.Parse([]byte(testSVG), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, wrapped := fitSceneTo(doc, 40, 20).(scaledScene); wrapped {
		t.Error("a box matching the SVG's own viewport was wrapped in a scale adapter")
	}
	s, wrapped := fitSceneTo(doc, 80, 20).(scaledScene)
	if !wrapped {
		t.Fatal("a box differing from the SVG's viewport was NOT scaled; the drawing would " +
			"occupy only part of its box")
	}
	if s.srcW != 2 || s.srcH != 1 {
		t.Errorf("scale = (%g, %g), want (2, 1)", s.srcW, s.srcH)
	}
}

// TestInlineSVGCacheParsesEachMarkupOnce proves inline markup is memoized too.
// Inline SVG has no ref and no loader, so it needs its own cache; without one,
// the same subtree is re-parsed by replacedUsedSize, by replacedFragment, and
// once more per intrinsic-width measurement.
func TestInlineSVGCacheParsesEachMarkupOnce(t *testing.T) {
	c := newInlineSVGCache()
	first := c.get(testSVG, nil, nil)
	if !first.ok {
		t.Fatal("inline markup did not parse")
	}
	if second := c.get(testSVG, nil, nil); second.doc != first.doc {
		t.Error("the same markup was parsed twice; the memo is not working")
	}
}

// TestInlineSVGCacheDegradesOnMalformedMarkup covers the untrusted-input contract
// for inline markup: a failed parse is a cached miss, never a panic, and wasSVG
// stays true so the caller reserves an empty vector box rather than routing the
// markup to the raster path.
func TestInlineSVGCacheDegradesOnMalformedMarkup(t *testing.T) {
	c := newInlineSVGCache()
	for _, bad := range []string{"", "<svg", "\x00\x01", "<notsvg/>", `<svg><rect`} {
		got := c.get(bad, nil, nil)
		if got.ok {
			t.Errorf("malformed inline markup %q reported a successful parse", bad)
		}
		if !got.wasSVG {
			t.Errorf("malformed inline markup %q reported wasSVG=false; it would be sent to "+
				"the raster path instead of degrading as a vector box", bad)
		}
	}
}

// TestParseSVGBytesNeverPanics is the blunt no-panic guarantee over adversarial
// byte patterns. svg.Parse is documented total on malformed XML, but this runs on
// untrusted input, so the recover is asserted rather than assumed.
func TestParseSVGBytesNeverPanics(t *testing.T) {
	for _, bad := range [][]byte{
		nil, {}, []byte("<"), []byte("<svg"), []byte("<svg><"), []byte("<!--"),
		[]byte("<?xml"), []byte("\xff\xfe\x00\x00"), []byte(strings.Repeat("<a>", 5000)),
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><use href="#a" id="a"/></svg>`),
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 NaN Inf"/>`),
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("parseSVGBytes panicked on %q: %v", bad, r)
				}
			}()
			parseSVGBytes(bad, "t", nil, nil) //nolint:errcheck // the point is that it returns at all
		}()
	}
}

// TestCloneFloatForPageClonesVectorContent pins a defect that made a
// positioned SVG silently disappear.
//
// cloneFloatForPage produces one clone of a fragment PER PAGE it appears on,
// and translateFragment then shifts each clone's content origin IN PLACE. The
// clone copied Image, Control, and BgImage but not Vector, so every per-page
// clone shared a single *VectorContent and each page's shift compounded onto
// it. Measured on a bottom-anchored position:fixed SVG across three pages, the
// vector landed at y=-480 on all three (want ~220) — entirely off-page, so it
// rendered nowhere at all.
//
// Asserting that the clone's VectorContent is a DISTINCT pointer is what
// catches this: an aliased pointer is the bug, and mutating through one clone
// must not be visible through another.
func TestCloneFloatForPageClonesVectorContent(t *testing.T) {
	orig := &Fragment{Vector: &VectorContent{CX: 10, CY: 20, CW: 30, CH: 40}}

	a := cloneFloatForPage(orig)
	b := cloneFloatForPage(orig)
	if a.Vector == nil || b.Vector == nil {
		t.Fatal("cloneFloatForPage dropped Vector entirely")
	}
	if a.Vector == orig.Vector || a.Vector == b.Vector {
		t.Fatal("cloneFloatForPage aliased VectorContent; per-page shifts would compound onto one shared value")
	}

	// Shifting one page's clone must leave the other page's untouched — the
	// property the aliasing broke.
	a.Vector.CY += 100
	if b.Vector.CY != 20 {
		t.Errorf("shifting one clone moved another: got CY=%v, want 20", b.Vector.CY)
	}
	if orig.Vector.CY != 20 {
		t.Errorf("shifting a clone mutated the original: got CY=%v, want 20", orig.Vector.CY)
	}
}
