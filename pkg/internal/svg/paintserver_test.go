package svg

import (
	"image/color"
	"testing"
)

// buildDoc parses src into an element tree and its docIndex, failing the test
// on a parse error. It mirrors the pattern already used in stops_test.go's
// cascade tests, generalized for paintserver_test.go's href-chain fixtures.
func buildDoc(t *testing.T, src string) (*element, *docIndex) {
	t.Helper()
	root, err := parseXML([]byte(src), nil)
	if err != nil {
		t.Fatalf("parseXML: %v", err)
	}
	idx := buildIndex(root, func(string, string) {})
	return root, idx
}

const svgOpen = `<svg xmlns="http://www.w3.org/2000/svg">`

// TestResolvePaintServerAttributeInheritanceSimple verifies a referencing
// gradient with no attribute of its own inherits it from the element its
// href points to.
func TestResolvePaintServerAttributeInheritanceSimple(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<linearGradient id="base" x1="10" y1="20" x2="30" y2="40">
			<stop offset="0" stop-color="red"/>
			<stop offset="1" stop-color="blue"/>
		</linearGradient>
		<linearGradient id="derived" href="#base"/>
	</svg>`)

	b := newPaintServerResolver(idx, nil)
	ps, ok := b.resolve("derived")
	if !ok {
		t.Fatal("resolve reported ok=false")
	}
	if v, ok := ps.attrs["x1"]; !ok || v != "10" {
		t.Errorf("x1 = %q, %v; want \"10\", true", v, ok)
	}
	if v, ok := ps.attrs["x2"]; !ok || v != "30" {
		t.Errorf("x2 = %q, %v; want \"30\", true", v, ok)
	}
}

// TestResolvePaintServerAttributeInheritanceComplexOrder verifies per-attribute,
// first-defined-wins semantics walking a three-link chain: a references b
// references c. Each of a/b/c defines a different, overlapping subset of
// attributes; the winner for each attribute is whichever of {a,b,c} is
// nearest to a (a itself first, else the nearest ancestor that defines it).
// This mirrors resvg's attributes-via-xlink-href-complex-order.svg.
func TestResolvePaintServerAttributeInheritanceComplexOrder(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<linearGradient id="c" x1="1" y1="2" x2="3" y2="4" gradientUnits="userSpaceOnUse">
			<stop offset="0" stop-color="lime"/>
		</linearGradient>
		<linearGradient id="b" href="#c" x1="100" spreadMethod="reflect"/>
		<linearGradient id="a" href="#b" y1="200"/>
	</svg>`)

	b := newPaintServerResolver(idx, nil)
	ps, ok := b.resolve("a")
	if !ok {
		t.Fatal("resolve reported ok=false")
	}
	// a defines y1 itself: wins outright.
	if v := ps.attrs["y1"]; v != "200" {
		t.Errorf("y1 = %q, want \"200\" (a's own value)", v)
	}
	// a doesn't define x1; b (nearest ancestor) does: wins over c's x1=1.
	if v := ps.attrs["x1"]; v != "100" {
		t.Errorf("x1 = %q, want \"100\" (b's value, nearer than c)", v)
	}
	// Neither a nor b defines x2; falls through to c.
	if v := ps.attrs["x2"]; v != "3" {
		t.Errorf("x2 = %q, want \"3\" (c's value)", v)
	}
	// Neither a nor b defines gradientUnits; falls through to c.
	if v := ps.attrs["gradientUnits"]; v != "userSpaceOnUse" {
		t.Errorf("gradientUnits = %q, want \"userSpaceOnUse\" (c's value)", v)
	}
	// Neither a nor c defines spreadMethod; only b does.
	if v := ps.attrs["spreadMethod"]; v != "reflect" {
		t.Errorf("spreadMethod = %q, want \"reflect\" (b's value)", v)
	}
}

// TestResolvePaintServerStopsAllOrNothing verifies that a gradient with no
// <stop> children of its own takes the ENTIRE stop list from the nearest
// ancestor in the chain that has stops, not a per-stop merge. The referencing
// gradient here has zero stops so there is nothing to merge with; the test
// still asserts the whole ramp came from the ancestor, not e.g. a single
// stop or a hybrid.
func TestResolvePaintServerStopsAllOrNothing(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<linearGradient id="base">
			<stop offset="0" stop-color="red"/>
			<stop offset="0.5" stop-color="lime"/>
			<stop offset="1" stop-color="blue"/>
		</linearGradient>
		<linearGradient id="derived" href="#base" x1="5"/>
	</svg>`)

	b := newPaintServerResolver(idx, nil)
	ps, ok := b.resolve("derived")
	if !ok {
		t.Fatal("resolve reported ok=false")
	}
	if ps.stops == nil {
		t.Fatal("stops = nil, want the base gradient's 3-stop ramp")
	}
	if len(ps.stops.stops) != 3 {
		t.Fatalf("len(stops) = %d, want 3 (all-or-nothing from ancestor)", len(ps.stops.stops))
	}
	wantColor(t, evalRGB(t, ps.stops, 0), color.RGBA{255, 0, 0, 255})
	wantColor(t, evalRGB(t, ps.stops, 0.5), color.RGBA{0, 255, 0, 255})
	wantColor(t, evalRGB(t, ps.stops, 1), color.RGBA{0, 0, 255, 255})
}

// TestResolvePaintServerOwnStopsWinOutright verifies a gradient that DOES
// have its own stops never reaches into the href chain for stops at all,
// even though it references a gradient with a different stop list — the
// all-or-nothing rule cuts the other way too.
func TestResolvePaintServerOwnStopsWinOutright(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<linearGradient id="base">
			<stop offset="0" stop-color="red"/>
			<stop offset="1" stop-color="blue"/>
		</linearGradient>
		<linearGradient id="derived" href="#base">
			<stop offset="0" stop-color="black"/>
			<stop offset="1" stop-color="white"/>
		</linearGradient>
	</svg>`)

	b := newPaintServerResolver(idx, nil)
	ps, ok := b.resolve("derived")
	if !ok {
		t.Fatal("resolve reported ok=false")
	}
	if len(ps.stops.stops) != 2 {
		t.Fatalf("len(stops) = %d, want 2 (derived's own stops)", len(ps.stops.stops))
	}
	wantColor(t, evalRGB(t, ps.stops, 0), color.RGBA{0, 0, 0, 255})
	wantColor(t, evalRGB(t, ps.stops, 1), color.RGBA{255, 255, 255, 255})
}

// TestResolvePaintServerCrossType verifies a <linearGradient> may href a
// <radialGradient>: it inherits the attributes it understands (shared ones
// like gradientUnits, gradientTransform, spreadMethod) plus the stops, while
// radial-only geometry (cx/cy/r/fx/fy) is simply present in attrs and
// harmless to carry — resolvePaintServer does not filter by kind, callers
// that only care about linear geometry just won't read cx/cy/r.
func TestResolvePaintServerCrossType(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<radialGradient id="rad" cx="50" cy="50" r="40" gradientUnits="userSpaceOnUse">
			<stop offset="0" stop-color="yellow"/>
			<stop offset="1" stop-color="green"/>
		</radialGradient>
		<linearGradient id="lin" href="#rad" x1="0" y1="0" x2="1" y2="1"/>
	</svg>`)

	b := newPaintServerResolver(idx, nil)
	ps, ok := b.resolve("lin")
	if !ok {
		t.Fatal("resolve reported ok=false")
	}
	if ps.kind != "linearGradient" {
		t.Errorf("kind = %q, want \"linearGradient\" (the referencing element's own type)", ps.kind)
	}
	if v := ps.attrs["gradientUnits"]; v != "userSpaceOnUse" {
		t.Errorf("gradientUnits = %q, want inherited \"userSpaceOnUse\" from the radial ancestor", v)
	}
	if v := ps.attrs["cx"]; v != "50" {
		t.Errorf("cx = %q, want \"50\" (harmlessly inherited even though lin is linear)", v)
	}
	if ps.stops == nil || len(ps.stops.stops) != 2 {
		t.Fatalf("stops not inherited from the radial ancestor: %+v", ps.stops)
	}
	wantColor(t, evalRGB(t, ps.stops, 0), color.RGBA{255, 255, 0, 255})
}

// TestResolvePaintServerNonGradientTargetIsNoOp verifies href-ing a non-
// paint-server element (e.g. a <rect>) inherits nothing: the referencing
// gradient resolves using only its own attributes/stops, as if the href
// were absent.
func TestResolvePaintServerNonGradientTargetIsNoOp(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<rect id="notAGradient" x1="999" width="10" height="10"/>
		<linearGradient id="g" href="#notAGradient" x2="30">
			<stop offset="0" stop-color="red"/>
			<stop offset="1" stop-color="blue"/>
		</linearGradient>
	</svg>`)

	b := newPaintServerResolver(idx, nil)
	ps, ok := b.resolve("g")
	if !ok {
		t.Fatal("resolve reported ok=false")
	}
	if v, ok := ps.attrs["x1"]; ok {
		t.Errorf("x1 = %q, ok=true; want absent — a <rect> target must not contribute attributes", v)
	}
	if v := ps.attrs["x2"]; v != "30" {
		t.Errorf("x2 = %q, want g's own \"30\"", v)
	}
	if ps.stops == nil || len(ps.stops.stops) != 2 {
		t.Fatalf("stops = %+v, want g's own 2 stops (rect contributes none)", ps.stops)
	}
}

// TestResolvePaintServerTwoCycleTerminates verifies a→b→a href cycle
// terminates (does not hang) and still resolves using whatever attributes
// were seen before the cycle was detected, cutting the chain off rather than
// looping forever or panicking. The specific attribute values recovered are
// less important than the hard requirement: this test must complete.
func TestResolvePaintServerTwoCycleTerminates(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<linearGradient id="a" href="#b" x1="1"/>
		<linearGradient id="b" href="#a" y1="2"/>
	</svg>`)

	b := newPaintServerResolver(idx, nil)
	ps, ok := b.resolve("a")
	if !ok {
		t.Fatal("resolve reported ok=false")
	}
	// a's own attribute must still be present.
	if v := ps.attrs["x1"]; v != "1" {
		t.Errorf("x1 = %q, want \"1\" (a's own value, unaffected by the cycle)", v)
	}
	// b's attribute, reached once before the cycle closes, should still be
	// picked up.
	if v := ps.attrs["y1"]; v != "2" {
		t.Errorf("y1 = %q, want \"2\" (b's value, reached once before the cycle closed)", v)
	}
}

// TestResolvePaintServerSelfCycleTerminates verifies a href-ing itself
// terminates immediately rather than hanging.
func TestResolvePaintServerSelfCycleTerminates(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<linearGradient id="a" href="#a" x1="1"/>
	</svg>`)

	b := newPaintServerResolver(idx, nil)
	ps, ok := b.resolve("a")
	if !ok {
		t.Fatal("resolve reported ok=false")
	}
	if v := ps.attrs["x1"]; v != "1" {
		t.Errorf("x1 = %q, want \"1\"", v)
	}
}

// TestResolvePaintServerLongChainBeyondCapTerminates builds a chain longer
// than any reasonable depth cap (200 links) and verifies resolution still
// terminates and returns a usable result rather than hanging or panicking.
// The far end of the chain (beyond the cap) must NOT contribute an
// attribute, proving the cap actually bit rather than the walk quietly
// completing the whole thing anyway.
func TestResolvePaintServerLongChainBeyondCapTerminates(t *testing.T) {
	const n = 200
	var sb []byte
	sb = append(sb, svgOpen...)
	for i := 0; i < n; i++ {
		next := i + 1
		if i == n-1 {
			// Last link in the chain: give it a distinctive attribute and no
			// further href, so we can check whether the walk reached it.
			sb = append(sb, []byte(elemStr(i, "", "farAttr=\"reached\""))...)
			continue
		}
		sb = append(sb, []byte(elemStr(i, hrefOf(next), ""))...)
	}
	sb = append(sb, "</svg>"...)

	_, idx := buildDoc(t, string(sb))
	b := newPaintServerResolver(idx, nil)
	ps, ok := b.resolve("g0")
	if !ok {
		t.Fatal("resolve reported ok=false")
	}
	if v, ok := ps.attrs["farAttr"]; ok {
		t.Errorf("farAttr = %q, ok=true; want the depth cap to have cut the chain off before reaching link %d", v, n-1)
	}
}

// elemStr renders one <linearGradient id="gN" .../> chain link for
// TestResolvePaintServerLongChainBeyondCapTerminates.
func elemStr(i int, href, extra string) string {
	s := `<linearGradient id="g` + itoa(i) + `"`
	if href != "" {
		s += " " + href
	}
	if extra != "" {
		s += " " + extra
	}
	s += "/>"
	return s
}

func hrefOf(i int) string { return `href="#g` + itoa(i) + `"` }

// itoa avoids importing strconv just for test fixture construction.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// TestResolvePaintServerMemoization verifies resolving the same id twice
// through the same resolver returns the identical *resolvedServer pointer
// (not just an equal value), proving the chain was walked once and cached,
// not re-walked on every call.
func TestResolvePaintServerMemoization(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<linearGradient id="g" x1="1">
			<stop offset="0" stop-color="red"/>
			<stop offset="1" stop-color="blue"/>
		</linearGradient>
	</svg>`)

	b := newPaintServerResolver(idx, nil)
	first, ok := b.resolve("g")
	if !ok {
		t.Fatal("resolve reported ok=false")
	}
	second, ok := b.resolve("g")
	if !ok {
		t.Fatal("resolve reported ok=false (second call)")
	}
	if first != second {
		t.Errorf("resolve(\"g\") returned different pointers across calls: %p != %p; want memoized", first, second)
	}
}

// TestResolvePaintServerMissingIDNotFound verifies resolving an id absent
// from idx.ids reports ok=false rather than panicking or returning a
// zero-value server that looks like a valid (but empty) gradient.
func TestResolvePaintServerMissingIDNotFound(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`<rect width="1" height="1"/>`)
	b := newPaintServerResolver(idx, nil)
	if _, ok := b.resolve("nope"); ok {
		t.Error("resolve(\"nope\") reported ok=true for an id not present anywhere in the document")
	}
}

// TestResolvePaintServerLookupUsesIDsNotDefs verifies a gradient declared
// OUTSIDE any <defs> subtree still resolves — the resolver must look up
// through idx.ids, not idx.defs, since idx.defs only contains elements that
// are descendants of a <defs>.
func TestResolvePaintServerLookupUsesIDsNotDefs(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<linearGradient id="loose" x1="7">
			<stop offset="0" stop-color="red"/>
			<stop offset="1" stop-color="blue"/>
		</linearGradient>
		<rect width="1" height="1" fill="url(#loose)"/>
	</svg>`)

	if _, inDefs := idx.defs["loose"]; inDefs {
		t.Fatal("test setup bug: \"loose\" ended up in idx.defs, defeats the point of this test")
	}
	if _, inIDs := idx.ids["loose"]; !inIDs {
		t.Fatal("test setup bug: \"loose\" missing from idx.ids")
	}

	b := newPaintServerResolver(idx, nil)
	ps, ok := b.resolve("loose")
	if !ok {
		t.Fatal("resolve reported ok=false for a gradient outside <defs> — lookup must use idx.ids, not idx.defs")
	}
	if v := ps.attrs["x1"]; v != "7" {
		t.Errorf("x1 = %q, want \"7\"", v)
	}
}

// TestWalkHrefChainReusableHelper exercises followHrefChain directly (the
// reusable cycle-safe walker), independent of paint-server semantics, since
// PR 5 (<use>/<symbol>) needs to adopt the same helper for its own reference
// graph. It verifies: a plain chain visits every link in order, a cycle
// terminates, and a visit function returning false (stop early) is honored.
func TestWalkHrefChainReusableHelper(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<linearGradient id="a" href="#b"/>
		<linearGradient id="b" href="#c"/>
		<linearGradient id="c"/>
	</svg>`)

	start := idx.ids["a"]

	var seen []string
	followHrefChain(start, idx, func(el *element) bool {
		seen = append(seen, el.id)
		return true // keep going
	})
	want := []string{"a", "b", "c"}
	if !stringsEqual(seen, want) {
		t.Errorf("followHrefChain visited %v, want %v", seen, want)
	}

	// Stopping early after the first element must not visit the rest.
	seen = nil
	followHrefChain(start, idx, func(el *element) bool {
		seen = append(seen, el.id)
		return false // stop after first
	})
	if !stringsEqual(seen, []string{"a"}) {
		t.Errorf("followHrefChain with early stop visited %v, want [a]", seen)
	}
}

// TestWalkHrefChainCycleVisitsEachNodeOnce verifies the cycle-safe walker
// visits each element at most once even in a cycle, and terminates.
func TestWalkHrefChainCycleVisitsEachNodeOnce(t *testing.T) {
	_, idx := buildDoc(t, svgOpen+`
		<linearGradient id="a" href="#b"/>
		<linearGradient id="b" href="#a"/>
	</svg>`)

	start := idx.ids["a"]
	var seen []string
	followHrefChain(start, idx, func(el *element) bool {
		seen = append(seen, el.id)
		return true
	})
	if len(seen) > 2 {
		t.Fatalf("followHrefChain visited %d nodes on a 2-cycle, want at most 2 (got %v)", len(seen), seen)
	}
	if !stringsEqual(seen, []string{"a", "b"}) {
		t.Errorf("followHrefChain visited %v, want [a b]", seen)
	}
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
