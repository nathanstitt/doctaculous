package svg

import (
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// defaultWidth and defaultHeight are the CSS replaced-element default size
// used when an <svg> root has neither a usable width/height attribute nor a
// viewBox to derive one from (CSS Images §2.2 / SVG2 §5.1.2).
const (
	defaultWidth  = 300.0
	defaultHeight = 150.0
)

// Node is one scene-graph entry: a Group or a Shape. It is implemented only
// by types in this package; the sealed method keeps it a closed sum type for
// pkg/svg/draw's type switch.
type Node interface {
	isNode()
}

// Group is a container of child nodes sharing a local transform (an SVG <g>,
// the <svg> root's own children, or an unrecognized SVG-namespace element
// treated as a forgiving container).
type Group struct {
	M    render.Matrix // local transform, applied to Kids
	Kids []Node
}

func (*Group) isNode() {}

// Shape is one paintable path with its resolved style (an SVG basic shape:
// rect, circle, ellipse, line, polyline, polygon, or path).
//
// FillGradient/StrokeGradient carry an ALREADY-RESOLVED paint server (a
// render.Shader plus the matrix mapping its local coordinate space into this
// Shape's own user space, i.e. composed before M) rather than a url() id:
// the document index a gradient id resolves through is discarded when Parse
// returns, and Document must stay a read-only, side-table-free value so it
// can be shared lock-free across the engine's parallel page-render fan-out.
// Either field is nil when the corresponding fill/stroke does not reference
// a (successfully resolved) gradient — see Style.FillServer/StrokeServer for
// why a Style may still carry a server id even when resolution fails or a
// pattern is referenced (patterns are not yet resolved into either field).
type Shape struct {
	M              render.Matrix // local transform
	Path           *render.Path  // user-space geometry, pre-transform
	Style          Style
	FillGradient   *paintServer // resolved fill="url(#...)" gradient, or nil
	StrokeGradient *paintServer // resolved stroke="url(#...)" gradient, or nil
}

func (*Shape) isNode() {}

// Document is a parsed SVG document, read-only after Parse and safe for
// concurrent use (shared lock-free across the engine's parallel page-render
// fan-out).
type Document struct {
	// WidthPt/HeightPt is the resolved viewport size in layout points
	// (1 SVG user unit = 1 CSS px = 1 layout pt).
	WidthPt, HeightPt float64

	root  *Group        // children of <svg>, transforms resolved
	rootM render.Matrix // viewBox->viewport mapping (Identity without viewBox)
}

// Root returns the scene root group and the viewBox->viewport matrix, for
// pkg/svg/draw. The returned group tree must not be mutated: it may be
// shared across concurrent renders of the same Document.
func (d *Document) Root() (rootM render.Matrix, root *Group) {
	return d.rootM, d.root
}

// Parse parses an SVG document into a read-only Document. logf (nil ok)
// receives one debug line per skipped-or-degraded feature encountered while
// building the scene; each unsupported element name is logged at most once
// regardless of how many times it appears.
func Parse(data []byte, logf func(string, ...any)) (*Document, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}

	root, err := parseXML(data, logf)
	if err != nil {
		return nil, err
	}

	doc := &Document{}
	vb, hasVB := resolveViewBox(root, logf)
	doc.WidthPt, doc.HeightPt = resolveSize(root, vb, hasVB)

	if hasVB {
		doc.rootM = viewBoxMatrix(vb, doc.WidthPt, doc.HeightPt, root.attrs["preserveAspectRatio"])
	} else {
		doc.rootM = render.Identity
	}

	b := &sceneBuilder{logf: logf, warned: map[string]bool{}, vp: viewport{w: doc.WidthPt, h: doc.HeightPt}}
	if hasVB {
		// Gradient userSpaceOnUse coordinates live in the same user-unit
		// space as the element geometry they paint — i.e. viewBox space when
		// a viewBox is present, not the post-rootM viewport pixel space — so
		// percentage resolution must use the viewBox extent in that case.
		b.vp = viewport{w: vb.W, h: vb.H}
	}
	b.idx = buildIndex(root, b.warnOnceMsg)
	b.servers = newPaintServerResolver(b.idx, logf)
	ctx := &cascadeCtx{idx: b.idx, logf: logf}
	doc.root = b.buildGroup(root, defaultStyle(), ctx)

	return doc, nil
}

// resolveViewBox parses the root <svg>'s viewBox attribute, if present. An
// invalid viewBox (parseViewBox rejects it: wrong field count, or a
// non-positive extent) is logged and treated as absent, per the invariant
// documented on viewBoxMatrix — it must never be called with a viewBox that
// parseViewBox did not accept, since it divides by the extent unchecked.
func resolveViewBox(root *element, logf func(string, ...any)) (viewBox, bool) {
	s, ok := root.attrs["viewBox"]
	if !ok {
		return viewBox{}, false
	}
	vb, ok := parseViewBox(s)
	if !ok {
		logf("svg: ignoring viewBox=%q: unparseable or non-positive extent", s)
		return viewBox{}, false
	}
	return vb, true
}

// resolveSize implements the CSS/SVG replaced-element sizing cascade for the
// root <svg>:
//
//  1. width/height attributes present and parse to an absolute (non-percentage)
//     value > 0: use them.
//  2. Missing or percentage width/height: take it from the viewBox extent.
//  3. No viewBox either: 300x150 (the CSS replaced-element default).
//  4. Only one dimension resolved by 1-3 above and a viewBox is present:
//     derive the other from the viewBox aspect ratio.
func resolveSize(root *element, vb viewBox, hasVB bool) (w, h float64) {
	w, wOK := absoluteLength(root.attrs["width"])
	h, hOK := absoluteLength(root.attrs["height"])

	switch {
	case wOK && hOK:
		// Rule 1: both attributes resolved to an absolute value.
	case hasVB && wOK && !hOK && vb.W != 0:
		// Rule 4: width known, derive height from the viewBox aspect ratio.
		h = w * (vb.H / vb.W)
	case hasVB && hOK && !wOK && vb.H != 0:
		// Rule 4: height known, derive width from the viewBox aspect ratio.
		w = h * (vb.W / vb.H)
	default:
		// Rule 2/3: fall back to the viewBox extent per missing dimension,
		// or the CSS replaced-element default when there is no viewBox.
		if !wOK {
			if hasVB {
				w = vb.W
			} else {
				w = defaultWidth
			}
		}
		if !hOK {
			if hasVB {
				h = vb.H
			} else {
				h = defaultHeight
			}
		}
	}
	return w, h
}

// absoluteLength parses an SVG length attribute and reports ok=true only
// when it is present, syntactically valid, non-percentage, and > 0 — the
// bar sizing rule 1 sets for "use the attribute value" (a percentage, a
// non-positive value, or a missing/unparseable attribute all fall through
// to the viewBox/default rules instead).
func absoluteLength(s string) (float64, bool) {
	if s == "" || hasPercentSuffix(s) {
		return 0, false
	}
	v, ok := parseLength(s, 0)
	if !ok || v <= 0 {
		return 0, false
	}
	return v, true
}

// hasPercentSuffix reports whether a trimmed length string ends in "%".
func hasPercentSuffix(s string) bool {
	for i := len(s) - 1; i >= 0; i-- {
		switch s[i] {
		case ' ', '\t', '\n', '\r', '\f':
			continue
		case '%':
			return true
		default:
			return false
		}
	}
	return false
}

// unsupportedElements are SVG-namespace elements this PR recognizes but does
// not yet implement. Each is skipped with one debug log line per element
// name per document (see sceneBuilder.warned) rather than being silently
// dropped or treated as an unknown forgiving container, since silently
// recursing into e.g. an unsupported container's children would misrender
// them as shapes/groups.
var unsupportedElements = map[string]bool{
	"use":              true,
	"symbol":           true,
	"text":             true,
	"image":            true,
	"clipPath":         true,
	"mask":             true,
	"filter":           true,
	"marker":           true,
	"switch":           true,
	"foreignObject":    true,
	"svg":              true, // nested <svg>: viewport-establishing, not yet supported
	"animate":          true,
	"animateTransform": true,
	"animateMotion":    true,
	"animateColor":     true,
	"set":              true,
}

// skippedElements are SVG-namespace elements that are recognized structural
// or metadata containers with no visual output of their own; they are
// dropped without any log line.
//
// defs is walked but not emitted: buildIndex's pre-pass already descended
// into it (and every other SVG-namespace element, regardless of "display")
// to collect its <style> sheets and its ids into docIndex, so a
// <defs><style>...</style></defs> stylesheet still reaches the cascade. The
// scene walk handled here is the second, separate pass that turns elements
// into paintable Nodes, and it must NOT recurse into defs' children: they
// are only reachable via a later <use> reference (not yet supported), so
// painting them directly into the visible tree here would be wrong.
//
// style is skipped here for the same "walked elsewhere, not painted here"
// reason: buildIndex's pre-pass already consumed every <style> element's
// text into docIndex.sheets, so by the time the scene walk reaches one,
// there is nothing left for it to do but produce zero Nodes.
//
// linearGradient, radialGradient, and pattern are paint servers: fully
// supported (Style.FillServer/StrokeServer + the document index resolve
// them out-of-band), but they contribute nothing to the scene walk
// themselves — a shape that references one is painted using the resolved
// paint server, not by the gradient/pattern element appearing as a node.
// stop is a gradient's child and must be skipped for the same reason:
// without an explicit entry here it would fall to buildNode's forgiving
// "unknown element" default and get painted directly into the visible
// scene at document coordinates, since Go map membership provides no
// transitive "skip my children too" behavior.
var skippedElements = map[string]bool{
	"defs":           true,
	"style":          true,
	"title":          true,
	"desc":           true,
	"metadata":       true,
	"linearGradient": true,
	"radialGradient": true,
	"pattern":        true,
	"stop":           true,
}

// shapeElements are the SVG basic shapes shapePath knows how to convert.
var shapeElements = map[string]bool{
	"rect":     true,
	"circle":   true,
	"ellipse":  true,
	"line":     true,
	"polyline": true,
	"polygon":  true,
	"path":     true,
}

// sceneBuilder walks the parsed element tree into a scene graph, tracking
// per-document state: the log sink, the set of unsupported element names
// already warned about (so ten <text> elements produce one log line, not
// ten), and the whole-document index (stylesheets, ids, defs) that the
// cascade resolves against. idx is built once in Parse, before buildGroup's
// walk begins, and is discarded along with the sceneBuilder when Parse
// returns — it never reaches Document.
type sceneBuilder struct {
	logf    func(string, ...any)
	warned  map[string]bool
	idx     *docIndex
	servers *paintServerResolver // gradient/pattern href-chain resolver, built once from idx
	vp      viewport             // current viewport size, for userSpaceOnUse percentage resolution
}

// buildGroup converts el's children into a Group, threading inherited style
// from parentStyle through Style.apply. el itself is not represented in
// the returned Group (its transform, for a <g> or <svg>, is applied by the
// caller as the Group's M); this function only walks el's children. ctx
// carries the cascade (stylesheets + logger) that apply resolves each
// child's attributes against.
func (b *sceneBuilder) buildGroup(el *element, parentStyle Style, ctx *cascadeCtx) *Group {
	g := &Group{M: render.Identity}
	for _, kid := range el.kids {
		if n := b.buildNode(kid, parentStyle, ctx); n != nil {
			g.Kids = append(g.Kids, n)
		}
	}
	return g
}

// buildNode converts one element (and, for a container, its subtree) into a
// scene Node, or nil when the element contributes nothing to the scene
// (foreign namespace, display:none, a skipped/metadata element, or a
// degenerate shape).
//
// display:none MUST short-circuit here, before any recursion: Style.apply
// copies the parent's display flag verbatim and never resets it for a child
// that doesn't set its own "display" (whether from a presentation attribute
// or, now that the cascade is wired in, a stylesheet rule), so a
// display:none *subtree* is only correctly hidden if the walker itself never
// descends into it. Checking display only at the element where it was set,
// and then still recursing into children (which would each report their
// own, possibly "shown", display value) would incorrectly reveal a
// display:none parent's descendants — including a child that sets
// display:inline on itself, which must stay hidden along with the rest of
// the parent's subtree.
func (b *sceneBuilder) buildNode(el *element, parentStyle Style, ctx *cascadeCtx) Node {
	if el.space != svgNS {
		return nil // foreign-namespace element: skip silently
	}

	st := parentStyle.apply(el, ctx)
	if !st.display {
		return nil // display:none: drop the element and its entire subtree
	}

	switch {
	case el.local == "g":
		return b.buildGroupElement(el, st, ctx)
	case shapeElements[el.local]:
		return b.buildShape(el, st)
	case el.local == "linearGradient", el.local == "radialGradient", el.local == "pattern", el.local == "stop":
		// Paint servers (and a gradient's <stop> children) are fully
		// supported, but resolved out-of-band through the document index —
		// see Style.FillServer/StrokeServer — not by appearing as scene
		// nodes. This case is listed explicitly, ahead of and independent of
		// skippedElements table membership, so a <pattern>'s tile children
		// (e.g. <rect>) can NEVER fall through to the forgiving default
		// below and get painted directly into the visible scene at document
		// coordinates.
		return nil
	case skippedElements[el.local]:
		return nil
	case unsupportedElements[el.local]:
		b.warnOnce(el.local)
		return nil
	default:
		// Unknown SVG-namespace element: log once and recurse into its
		// children as a plain container (the HTML-style forgiving default),
		// so an unrecognized-but-benign wrapper doesn't hide its contents.
		b.warnOnce(el.local)
		return b.buildGroup(el, st, ctx)
	}
}

// buildGroupElement converts a <g> element into a Group carrying its own
// parsed transform.
//
// Group has no field to carry st.opacity forward (see the doc comment on
// groupOpacityWarnKey): a <g opacity="..."> below 1 is therefore silently
// dropped by the scene graph today. Per-paint alpha through a group's
// children would produce a plausible-but-wrong render (overlapping children
// would each dim independently, causing seams/double-darkening) rather than
// an honestly-flat one, so true compositing is deferred to a later PR. This
// still must not fail silently: warn once per document instead.
func (b *sceneBuilder) buildGroupElement(el *element, st Style, ctx *cascadeCtx) Node {
	if st.opacity < 1 {
		b.warnOnceMsg(groupOpacityWarnKey, "svg: <g opacity> not yet composited; group opacity ignored")
	}
	g := b.buildGroup(el, st, ctx)
	g.M = elementTransform(el, b.logf)
	return g
}

// groupOpacityWarnKey is the warnOnce key for the group-opacity degradation
// notice, distinct from any element's local name (warnOnce's usual key) so
// it can never collide with an actual element name logged elsewhere.
const groupOpacityWarnKey = " group-opacity"

// buildShape converts a basic-shape element into a Shape, or nil when
// shapePath reports the shape degenerate (zero/negative extent) or
// visibility:hidden drops it. A fill or stroke that references a gradient
// (Style.FillServer/StrokeServer) is resolved here, against path's
// PRE-TRANSFORM geometry (the objectBoundingBox definition), and the result
// is stored on the Shape rather than the "#id" alone — see the Shape doc
// comment on why resolution must happen now, before Parse discards the
// document index.
func (b *sceneBuilder) buildShape(el *element, st Style) Node {
	if !st.visible {
		// visibility:hidden drops the shape outright in this PR: visibility
		// is inherited and re-enabling it on a child is a later concern, but
		// a shape has no children, so there is nothing to re-enable here.
		return nil
	}
	path := shapePath(el, b.logf)
	if path == nil {
		return nil
	}
	s := &Shape{
		M:     elementTransform(el, b.logf),
		Path:  path,
		Style: st,
	}
	if ref, ok := st.FillServer(); ok {
		if id, ok := fragmentID(ref); ok {
			if ps, ok := resolveGradient(id, b.servers, path, b.vp, b.logf); ok {
				s.FillGradient = &ps
			}
		}
	}
	if ref, ok := st.StrokeServer(); ok {
		if id, ok := fragmentID(ref); ok {
			if ps, ok := resolveGradient(id, b.servers, path, b.vp, b.logf); ok {
				s.StrokeGradient = &ps
			}
		}
	}
	return s
}

// elementTransform parses el's transform attribute (absent -> Identity).
// A malformed transform list invalidates the whole attribute per SVG error
// handling; that is logged and Identity is used instead of dropping the
// element.
func elementTransform(el *element, logf func(string, ...any)) render.Matrix {
	s, ok := el.attrs["transform"]
	if !ok || s == "" {
		return render.Identity
	}
	m, ok := parseTransform(s)
	if !ok {
		logf("svg: ignoring transform=%q on <%s>: unparseable", s, el.local)
		return render.Identity
	}
	return m
}

// warnOnce logs the "not yet supported" line for name the first time it is
// seen in this document, and is a no-op on subsequent calls for the same
// name.
func (b *sceneBuilder) warnOnce(name string) {
	b.warnOnceMsg(name, fmt.Sprintf("svg: <%s> not yet supported (skipped)", name))
}

// warnOnceMsg logs msg the first time it is requested under key in this
// document, and is a no-op on subsequent calls for the same key. It
// generalizes warnOnce's per-element-name dedup to any other per-document
// degradation notice (e.g. group opacity) that needs the same "once, not
// once per occurrence" bookkeeping but a different message shape.
func (b *sceneBuilder) warnOnceMsg(key, msg string) {
	if b.warned[key] {
		return
	}
	b.warned[key] = true
	b.logf("%s", msg)
}
