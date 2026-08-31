package svg

// maxHrefChainDepth bounds how many href links a chain walk will follow
// before giving up, independent of cycle detection. A cycle is caught by the
// visited set below in O(1) hops regardless of this cap; the cap instead
// guards a long ACYCLIC chain (a1 -> a2 -> ... -> aN, no repeats) from
// costing unbounded work on a document that chains hundreds of links deep.
// SVG documents in the wild never chain more than a handful of paint
// servers, so this is far beyond any legitimate use, mirroring the spirit of
// xml.go's maxElementDepth for the reference graph rather than the parse
// tree.
const maxHrefChainDepth = 64

// paintServerKinds are the SVG-namespace element types followHrefChain (via
// resolvePaintServer's use of it) treats as paint servers worth inheriting
// attributes/stops/children from. An href target of any other type (a
// <rect>, say) is a no-op per resolvePaintServer's contract.
var paintServerKinds = map[string]bool{
	"linearGradient": true,
	"radialGradient": true,
	"pattern":        true,
}

// followHrefChain walks the href reference chain starting at start, calling
// visit once per element in order (start first), and stops when:
//   - visit returns false (caller-requested early stop), or
//   - the chain runs out (an element with no href, or an href that does not
//     resolve through idx.ids), or
//   - a cycle is detected: the next hop's id has already been visited in
//     this walk, or
//   - the walk has followed maxHrefChainDepth hops.
//
// It is the reusable, cycle-safe reference-graph walker called out in the
// paint-servers design: docIndex.ids is a flat map with no cycle awareness
// of its own, and the parse-time maxElementDepth guard bounds XML nesting
// depth, not the href/xlink:href reference graph, so a document with
// a -> b -> a (or a -> a) would loop forever without this. PR 5's
// <use>/<symbol> reference resolution needs identical machinery for its own
// cycle-prone reference graph and can call this directly: it takes any
// *element with an "href" attribute pointing into idx.ids, with no
// gradient-specific assumptions baked in.
//
// start may be nil (no-op). idx may not be nil.
func followHrefChain(start *element, idx *docIndex, visit func(el *element) bool) {
	if start == nil {
		return
	}
	visited := map[*element]bool{}
	el := start
	for depth := 0; depth < maxHrefChainDepth; depth++ {
		if el == nil || visited[el] {
			return
		}
		visited[el] = true
		if !visit(el) {
			return
		}
		href, ok := el.attrs["href"]
		if !ok {
			return
		}
		id, ok := fragmentID(href)
		if !ok {
			return
		}
		el = idx.ids[id]
	}
}

// fragmentID extracts the id from a same-document URL fragment reference
// ("#foo" -> "foo", ok=true). Any other shape (empty, no leading '#', or a
// non-fragment URL this package doesn't resolve) reports ok=false.
func fragmentID(href string) (string, bool) {
	if len(href) < 2 || href[0] != '#' {
		return "", false
	}
	return href[1:], true
}

// resolvedServer is a gradient or pattern element's fully-inherited
// description: attrs merges every element in its href chain (per-attribute,
// first-defined-wins, nearest-to-referencing-element first), stops is the
// all-or-nothing inherited stop ramp (nil if no element in the chain has
// any), and kids carries a <pattern>'s tile content, also inherited
// all-or-nothing when the referencing pattern itself has no children.
//
// kind is the REFERENCING element's own local name (e.g. "linearGradient"),
// never a target's — cross-type href (a linearGradient href-ing a
// radialGradient) inherits attributes and stops but the resolved server is
// still, and paints as, the kind the caller asked for.
type resolvedServer struct {
	kind  string
	attrs map[string]string
	stops *stopRamp
	kids  []*element
}

// paintServerResolver resolves gradient/pattern ids (looked up through
// idx.ids, never idx.defs — a paint server is referenceable from anywhere in
// the document, not only from inside a <defs> subtree) into a
// *resolvedServer, memoizing by id so that many shapes sharing one gradient
// walk its href chain exactly once. It is built fresh per Parse call (idx is
// discarded when Parse returns) and is not safe for concurrent use — Parse's
// scene walk that owns it is single-threaded.
type paintServerResolver struct {
	idx    *docIndex
	logf   func(string, ...any)
	memo   map[string]*resolvedServer
	memoNo map[string]bool // ids resolved to ok=false, memoized separately from memo's zero value
}

// newPaintServerResolver builds a resolver over idx. logf may be nil (no-op
// logging), matching every other logf parameter in this package.
func newPaintServerResolver(idx *docIndex, logf func(string, ...any)) *paintServerResolver {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &paintServerResolver{
		idx:    idx,
		logf:   logf,
		memo:   map[string]*resolvedServer{},
		memoNo: map[string]bool{},
	}
}

// resolve returns id's fully-inherited paint-server description, or
// ok=false when id is not present in idx.ids or does not name a paint-server
// element (linearGradient/radialGradient/pattern). Results are memoized: a
// second resolve call for the same id returns the identical *resolvedServer
// without re-walking the href chain.
func (r *paintServerResolver) resolve(id string) (*resolvedServer, bool) {
	if ps, ok := r.memo[id]; ok {
		return ps, true
	}
	if r.memoNo[id] {
		return nil, false
	}

	el, ok := r.idx.ids[id]
	if !ok || el.space != svgNS || !paintServerKinds[el.local] {
		r.memoNo[id] = true
		return nil, false
	}

	ps := &resolvedServer{kind: el.local, attrs: map[string]string{}}

	// Attribute inheritance: per-attribute, first-defined-wins walking the
	// chain from the referencing element outward. Each visited element only
	// ever fills in attributes not already recorded by a nearer element, so
	// el's own attributes always win, then its href target's, and so on.
	//
	// Stop and child inheritance are all-or-nothing: the first element in
	// the chain that has any stops (or, for a pattern, any children) wins
	// its ENTIRE list; later elements in the chain are never consulted for
	// those two fields once a source is found, even if el itself has
	// neither.
	stopsFound := false
	kidsFound := false
	followHrefChain(el, r.idx, func(link *element) bool {
		if link.space != svgNS || !paintServerKinds[link.local] {
			// A non-gradient/pattern target contributes nothing: stop
			// walking (matches "a non-gradient target is a no-op").
			return false
		}
		for k, v := range link.attrs {
			if k == "href" {
				continue // the chain link itself, not an inheritable paint attribute
			}
			if _, taken := ps.attrs[k]; !taken {
				ps.attrs[k] = v
			}
		}
		if !stopsFound {
			if ramp, ok := parseStops(link, nil); ok {
				ps.stops = ramp
				stopsFound = true
			}
		}
		if !kidsFound && el.local == "pattern" && len(link.kids) > 0 {
			if kids := patternTileKids(link); len(kids) > 0 {
				ps.kids = kids
				kidsFound = true
			}
		}
		return true
	})

	r.memo[id] = ps
	return ps, true
}

// patternTileKids returns el's SVG-namespace, non-<stop> children — the
// tile content a <pattern> paints, as opposed to bookkeeping children like
// <title>/<desc> or (defensively) a stray <stop> that has no meaning inside
// a pattern. This is a narrow seam for Task 9 (pattern rendering): today it
// is only reached by resolve's all-or-nothing child inheritance and is not
// otherwise consumed, so the filtering here is deliberately conservative
// and may need revisiting once a real pattern renderer exists to say what
// "tile content" should include (e.g. whether <title>/<desc> matter there
// too, which they do not for painting either way).
func patternTileKids(el *element) []*element {
	kids := make([]*element, 0, len(el.kids))
	for _, k := range el.kids {
		if k == nil || k.space != svgNS {
			continue
		}
		kids = append(kids, k)
	}
	return kids
}
