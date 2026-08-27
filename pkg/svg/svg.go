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
//
// Opacity carries the container's own (non-inherited) element opacity, in
// [0,1], defaulting to 1. It is intentionally a single float rather than a
// whole Style: Style is shape-paint-shaped (fill/stroke/paint servers) and
// would be misleading on a node that never paints anything itself. A group
// with Opacity < 1 must be composited as a unit (see pkg/svg/draw's use of
// render.Device.BeginGroup/EndGroup) rather than by threading the factor into
// each child's own paint alpha, which would double-darken any overlap between
// children — the exact artifact groups exist to avoid.
type Group struct {
	M       render.Matrix // local transform, applied to Kids
	Opacity float64       // element opacity [0,1]; 1 = fully opaque, no group needed
	Kids    []Node

	// ClipPath is the resolved clip-path="url(#...)" reference on the <g>
	// element itself (or the root), or nil when absent/invalid/"none". See
	// the ClipPath type (clippath.go) for how pkg/svg/draw turns this into a
	// render.Device.BuildClipMask call.
	ClipPath *ClipPath

	// Mask is the resolved mask="url(#...)" reference on the <g> element
	// itself, or nil when absent/invalid/"none". See the Mask type
	// (mask.go) for how pkg/svg/draw turns this into a
	// render.Device.BuildLuminanceMask call. Applied AFTER ClipPath and
	// BEFORE Opacity in the composite order (clip -> mask -> opacity), per
	// the design doc.
	Mask *Mask

	// ViewportClip is a plain axis-aligned [0,w]x[0,h] rect, in the SAME
	// local space M maps FROM (i.e. composed under M exactly like a Shape's
	// Path composes under Shape.M), clipping this group's content to a
	// viewport rectangle — currently only produced for a <symbol>
	// instantiated through <use> with default overflow:hidden (see
	// buildSymbolInstance in use.go). nil means no viewport clip.
	//
	// This is deliberately NOT a *ClipPath: a <symbol>'s viewport is always
	// exactly one axis-aligned rectangle with no units mapping, no unioned
	// children, and no self-reference, so pkg/svg/draw can apply it with a
	// plain Save/PushClip/.../Restore around the group's content — no
	// offscreen BeginGroup/EndGroup compositing pass, and no
	// Device.BuildClipMask rasterization, are needed the way ClipPath's
	// general (arbitrary-geometry, unioned) semantics require. Kept
	// orthogonal to ClipPath/Mask/Opacity (a group can carry any
	// combination) rather than folded into ClipPath, so the common case —
	// an SVG icon sprite sheet instantiating many <symbol>s — stays on the
	// cheap PushClip-only path.
	ViewportClip *render.Path
}

func (*Group) isNode() {}

// Shape is one paintable path with its resolved style (an SVG basic shape:
// rect, circle, ellipse, line, polyline, polygon, or path).
//
// FillGradient/StrokeGradient/FillPattern/StrokePattern carry an
// ALREADY-RESOLVED paint server (a render.Shader, or a tile Group plus
// placement, together with the matrix mapping its local coordinate space
// into this Shape's own user space, i.e. composed before M) rather than a
// url() id: the document index a gradient/pattern id resolves through is
// discarded when Parse returns, and Document must stay a read-only,
// side-table-free value so it can be shared lock-free across the engine's
// parallel page-render fan-out. A field is nil when the corresponding
// fill/stroke does not reference a (successfully resolved) paint server of
// that kind — see Style.FillServer/StrokeServer for why a Style may still
// carry a server id even when resolution fails.
type Shape struct {
	M              render.Matrix // local transform
	Path           *render.Path  // user-space geometry, pre-transform
	Style          Style
	FillGradient   *paintServer  // resolved fill="url(#...)" gradient, or nil
	StrokeGradient *paintServer  // resolved stroke="url(#...)" gradient, or nil
	FillPattern    *patternPaint // resolved fill="url(#...)" pattern, or nil
	StrokePattern  *patternPaint // resolved stroke="url(#...)" pattern, or nil

	// ClipPath is the resolved clip-path="url(#...)" reference on this
	// shape, or nil when absent/invalid/"none". See Group.ClipPath.
	ClipPath *ClipPath

	// Mask is the resolved mask="url(#...)" reference on this shape, or nil
	// when absent/invalid/"none". See Group.Mask.
	Mask *Mask

	// MarkerStart, MarkerMid, MarkerEnd are the resolved marker-start/-mid/
	// -end references in effect at this shape (inherited from an ancestor
	// <g>, or the marker shorthand — see Style.MarkerStartRef/MarkerMidRef/
	// MarkerEndRef), or nil when absent/invalid/"none". ALWAYS nil for an
	// element outside markerableElements (a <circle>/<rect>/ellipse — see
	// buildShape): a resvg-corpus assertion (marker-on-circle.svg etc.)
	// requires those to paint NO markers even when a marker-* property
	// reaches them by inheritance.
	MarkerStart, MarkerMid, MarkerEnd *Marker
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

	b := &sceneBuilder{
		logf:            logf,
		warned:          map[string]bool{},
		vp:              viewport{w: doc.WidthPt, h: doc.HeightPt},
		buildingPattern: map[string]bool{},
		clipMemo:        map[string]*ClipPath{},
		buildingClip:    map[string]bool{},
		maskMemo:        map[string]*Mask{},
		buildingMask:    map[string]bool{},
		buildingUse:     map[string]bool{},
		markerMemo:      map[string]*Marker{},
		buildingMarker:  map[string]bool{},
	}
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
	// The root <svg> element's own opacity attribute (e.g. <svg
	// opacity="0.5">) applies to it just like any other element's, even
	// though buildGroup only walks the root's CHILDREN (the root has no
	// transform/M of its own to carry — viewBox->viewport is doc.rootM,
	// applied separately by pkg/svg/draw). Resolving opacity alone here
	// (rather than the root's full style) keeps every other presentation
	// attribute's inheritance into children exactly as before; only opacity
	// is unreachable without this, since Group had no field to carry it on.
	doc.root.Opacity = rootOpacity(root, ctx)

	return doc, nil
}

// rootOpacity resolves the root <svg> element's own opacity attribute
// (default 1 if absent, invalid, or clamped by applyOpacityProp), without
// resolving or applying any of its other presentation properties.
func rootOpacity(root *element, ctx *cascadeCtx) float64 {
	s := Style{opacity: 1}
	attr := ctx.resolve(root)
	logf := ctx.logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	applyOpacityProp("opacity", &s.opacity, attr, logf)
	return s.opacity
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
	"text":             true,
	"image":            true,
	"filter":           true,
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
//
// clipPath is resolved entirely out-of-band, like the paint servers above:
// a shape/group referencing one via clip-path="url(#...)" carries the
// resolved *ClipPath directly (see resolveClipPath in clippath.go), so the
// <clipPath> element itself must contribute NO scene nodes and must NOT be
// recursed into by the ordinary scene walk (its children are walked
// separately, through the clip-only allowlist in clippath.go, which is
// deliberately stricter than buildGroup's forgiving default — see that
// file's buildClipChild).
//
// mask is likewise resolved entirely out-of-band: a shape/group referencing
// one via mask="url(#...)" carries the resolved *Mask directly (see
// resolveMask in mask.go), so the <mask> element itself must contribute NO
// scene nodes. Unlike clipPath, a <mask>'s children ARE walked through the
// ordinary buildKidsGroup machinery (a mask may contain any paintable
// content, not a shape-only allowlist) — but that walk happens inside
// resolveMask itself, not here, so the main scene walk must still never
// descend into a <mask> a second time.
//
// symbol is likewise never rendered where it appears in the ordinary scene
// walk: it is only ever instantiated through a <use> reference (see
// buildSymbolInstance in use.go), which reaches it directly via idx.ids,
// bypassing buildNode entirely — a bare, unreferenced <symbol> (the
// unused-symbol.svg corpus fixture) must render nothing without logging
// "not yet supported", which moving it here (from unsupportedElements)
// achieves.
//
// marker is likewise never rendered where it appears: it is only ever
// instantiated through a marker-start/-mid/-end/marker reference (see
// resolveMarker in marker.go), reached directly via idx.ids exactly like
// clipPath/mask/symbol above. A bare, unreferenced <marker> must render
// nothing (empty.svg's marker1 IS referenced but has no children, a
// different case — this is about a <marker> the scene walk would otherwise
// stumble into as a plain child element) without logging "not yet
// supported", which is why it moved here from unsupportedElements rather
// than staying there.
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
	"clipPath":       true,
	"mask":           true,
	"symbol":         true,
	"marker":         true,
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

	// clipMemo memoizes resolveClipPath by id: several shapes/groups can
	// reference the same <clipPath>, and each reference resolves the
	// identical *ClipPath value (idempotent, no per-referencer state), so
	// memoizing avoids re-walking a possibly-large clipPath subtree once per
	// referencing element. A memoized ok=false result is recorded via a
	// separate presence check on clipMemo combined with buildingClip's
	// membership (see resolveClipPath) rather than a second map, since a
	// failed resolution has no value worth caching beyond "don't recurse
	// into it again" — buildingClip already provides that during the walk
	// that discovers the failure, and a document referencing an invalid
	// clip-path from many elements is not a realistic perf concern the way
	// gradient/pattern reuse is.
	clipMemo map[string]*ClipPath

	// buildingClip guards against a <clipPath> whose own clip-path
	// attribute, or one of its children's, refers back to a <clipPath>
	// already being resolved somewhere up the current call stack — directly
	// (self-reference) or through a cycle of several clipPaths. Mirrors
	// buildingPattern's shape exactly (see that field's doc comment): an id
	// present here is "in progress" further up the stack, and resolving it
	// again is treated as SVG's own cycle-must-be-an-error rule, degrading
	// to "no clip-path" for the self-referencing property instead of
	// recursing forever.
	buildingClip map[string]bool

	// maskMemo memoizes resolveMask by id, mirroring clipMemo exactly (see
	// that field's doc comment): several elements can reference the same
	// <mask>, and each reference resolves the identical *Mask value.
	maskMemo map[string]*Mask

	// buildingMask guards against a <mask> whose own mask attribute (or,
	// once <use>/<image> can appear inside a mask's content, a descendant's)
	// refers back to a <mask> already being resolved further up the current
	// call stack — directly (self-reference) or through a cycle of several
	// masks. Mirrors buildingClip's shape exactly.
	buildingMask map[string]bool

	// buildingPattern guards against a pattern tile's content referencing
	// (directly, or via its own href chain, or indirectly through a chain of
	// OTHER patterns that eventually loops back to it) the pattern currently
	// being built into a tile Group. This is a SEPARATE cycle-prone graph
	// from followHrefChain's href-chain walk (Task 4): here the cycle runs
	// through buildShape -> resolvePattern -> buildGroup -> buildShape again
	// for a shape inside the tile, not through a single element's href
	// attribute. A pattern id present in this set is currently "in
	// progress" one call further up the Go call stack; resolvePattern
	// treats resolving it again as a cycle and stops, exactly like SVG's own
	// "an indirect cycle must be treated as an error (ignore the
	// fill/stroke)" rule for patternful tiles.
	//
	// This set only fires when a pattern id repeats somewhere on the
	// current chain — it does NOT bound a chain of entirely DISTINCT
	// patterns (p0's tile fills with p1, p1's with p2, p2's with p3, ...),
	// since no id ever recurs there and the membership test never trips.
	// Each level of such a chain multiplies draw calls by its own tile cell
	// count, so left unchecked it is exponential in chain depth. That case
	// is bounded separately, at draw time, by pkg/svg/draw's per-DrawVector
	// pattern-nesting-depth counter (maxPatternNestingDepth) — not here.
	buildingPattern map[string]bool

	// buildingUse guards a <use> against BOTH cycle shapes the design calls
	// out (decision 2): an href chain (<use href="#u2"> where #u2 is itself
	// a <use>, eventually looping back), AND tree recursion (a <use>
	// containing, as a DESCENDANT, another <use> that targets the first —
	// or targets any other ancestor of itself in the DOM). The href-chain
	// case alone would be caught by a helper like followHrefChain; the
	// tree-recursion case is NOT, since the second <use> has no href
	// pointing at the first at all — it is reached by ordinary tree walk,
	// and only "is this id currently being instantiated somewhere up the
	// Go call stack" catches it. Entries are keyed by BOTH the <use>
	// element's own id (if any) and its resolved target's id — see
	// buildUse — mirroring buildingClip/buildingMask/buildingPattern's
	// "id present here is in progress further up the stack" shape exactly,
	// but keyed on two identities per level instead of one since a <use>
	// has both an identity of its own (reachable via tree recursion) and a
	// target identity (reachable via href chain).
	buildingUse map[string]bool

	// useDepth counts <use> instantiation nesting currently on the call
	// stack (a <use> whose target — directly, or through a <symbol>'s
	// content — contains another <use>). buildingUse's cycle guard only
	// fires when an id repeats; a chain of entirely DISTINCT targets
	// (#a uses #b uses #c uses ...) never repeats one, so useDepth is what
	// bounds THAT case via maxUseDepth, mirroring buildingPattern's
	// "cycle guard doesn't bound a distinct-chain" gap and pkg/svg/draw's
	// separate draw-time depth counter for the exact same reason.
	useDepth int

	// useNodes counts <use>/<symbol> instantiations performed so far in this
	// document's build, and is NEVER decremented — it is a whole-document
	// budget, not a call-stack depth like useDepth. useDepth bounds how DEEP
	// the instantiation recursion goes; this bounds how WIDE it gets, which
	// is the dimension the cost actually lives in: a <use> graph where each
	// level references the previous level twice stays comfortably inside
	// maxUseDepth and still expands to millions of nodes. Decrementing on
	// the way back out (the useDepth/buildingUse shape) would reset the
	// count per subtree and let exactly that graph through, so the counter
	// is monotonic for the lifetime of the sceneBuilder. See maxUseNodes.
	useNodes int

	// markerMemo memoizes resolveMarker by id, mirroring clipMemo/maskMemo
	// exactly (see clipMemo's doc comment): several shapes can reference the
	// same <marker>, and each reference resolves the identical *Marker
	// value.
	markerMemo map[string]*Marker

	// buildingMarker guards against a <marker> whose own content draws a
	// shape carrying a marker-*/marker property that refers back to a
	// <marker> already being resolved further up the current call stack —
	// directly (self-reference, recursive-1/2.svg) or through an ancestor
	// <g>'s inherited marker property reaching a shape inside the very
	// <marker> that <g> itself references (recursive-3.svg), or through a
	// document-wide CSS rule reaching the marker's own content
	// (recursive-4.svg). Mirrors buildingClip/buildingMask's shape exactly.
	buildingMarker map[string]bool

	// markerDepth is the number of <marker> content builds currently open on
	// the call stack, incremented by resolveMarker around its buildKidsGroup
	// call. It CANNOT be a plain parameter the way resolveMask's depth is: a
	// nested marker reference re-enters through the ordinary scene walk
	// (buildKidsGroup -> buildNode -> resolveMarkerRef), which has no depth
	// to thread, so the count has to live on the builder. See
	// maxMarkerChainDepth.
	markerDepth int
}

// buildGroup converts el's children into a Group, threading inherited style
// from parentStyle through Style.apply. el itself is not represented in
// the returned Group (its transform, for a <g> or <svg>, is applied by the
// caller as the Group's M); this function only walks el's children. ctx
// carries the cascade (stylesheets + logger) that apply resolves each
// child's attributes against.
func (b *sceneBuilder) buildGroup(el *element, parentStyle Style, ctx *cascadeCtx) *Group {
	return b.buildKidsGroup(el.kids, parentStyle, ctx)
}

// buildKidsGroup is buildGroup's loop body, factored out so a caller that
// already has a []*element slice not sourced from a single element's own
// kids field — a <pattern>'s inherited tile content (patternTileKids), which
// may come from a DIFFERENT element in the href chain than the one being
// resolved — can build a Group from it directly.
func (b *sceneBuilder) buildKidsGroup(kids []*element, parentStyle Style, ctx *cascadeCtx) *Group {
	g := &Group{M: render.Identity, Opacity: 1}
	for _, kid := range kids {
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
	case el.local == "use":
		// st is the <use> element's OWN resolved style (parentStyle already
		// applied above), threaded as the INSTANTIATED target's parentStyle
		// — see buildUse's doc comment for why this, not defaultStyle(), is
		// what makes style-inheritance-1.svg/complex-style-resolving-
		// order.svg resolve correctly.
		return b.buildUse(el, st, ctx)
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
// parsed transform and element opacity. pkg/svg/draw composites Opacity < 1
// via an offscreen group (render.Device.BeginGroup/EndGroup) rather than
// per-child paint alpha, so overlapping children inside the group don't
// double-darken where they overlap.
func (b *sceneBuilder) buildGroupElement(el *element, st Style, ctx *cascadeCtx) Node {
	g := b.buildGroup(el, st, ctx)
	g.M = elementTransform(el, b.logf)
	g.Opacity = st.opacity // already clamped to [0,1] by applyOpacityProp
	if ref, ok := st.ClipPathRef(); ok {
		g.ClipPath = b.resolveClipPathRef(ref)
	}
	if ref, ok := st.MaskRef(); ok {
		g.Mask = b.resolveMaskRef(ref)
	}
	return g
}

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
			b.resolvePaint(id, path, &s.FillGradient, &s.FillPattern)
		}
	}
	if ref, ok := st.StrokeServer(); ok {
		if id, ok := fragmentID(ref); ok {
			b.resolvePaint(id, path, &s.StrokeGradient, &s.StrokePattern)
		}
	}
	if ref, ok := st.ClipPathRef(); ok {
		s.ClipPath = b.resolveClipPathRef(ref)
	}
	if ref, ok := st.MaskRef(); ok {
		s.Mask = b.resolveMaskRef(ref)
	}
	if markerableElements[el.local] {
		// Per SVG2 §11.6.7, marker-start/-mid/-end paint only on path, line,
		// polyline, and polygon — NEVER on a circle/ellipse/rect (rounded or
		// not), even though shapePath has already flattened all of them into
		// an indistinguishable *render.Path by this point. This check must
		// happen before that flattening loses the distinction, which is
		// exactly why it keys off el.local here rather than any property of
		// s.Path. See marker-on-circle.svg/marker-on-rect.svg/
		// marker-on-rounded-rect.svg, which assert NO markers are drawn.
		if ref, ok := st.MarkerStartRef(); ok {
			s.MarkerStart = b.resolveMarkerRef(ref)
		}
		if ref, ok := st.MarkerMidRef(); ok {
			s.MarkerMid = b.resolveMarkerRef(ref)
		}
		if ref, ok := st.MarkerEndRef(); ok {
			s.MarkerEnd = b.resolveMarkerRef(ref)
		}
	}
	return s
}

// resolvePaint resolves id (a fill/stroke url() reference) against b.servers
// and stores the result into *gradOut or *patOut, whichever kind id names.
// Both out-pointers are left nil if id does not resolve to a paintable
// server (unknown id, no stops, a pattern with no usable tile, or a
// degenerate objectBoundingBox) — see resolveGradient/resolvePattern for the
// exact conditions. path is the shape's own PRE-TRANSFORM geometry.
func (b *sceneBuilder) resolvePaint(id string, path *render.Path, gradOut **paintServer, patOut **patternPaint) {
	ps, ok := b.servers.resolve(id)
	if !ok {
		return
	}
	if ps.kind == "pattern" {
		if pp, ok := b.resolvePattern(id, ps, path); ok {
			*patOut = pp
		}
		return
	}
	if g, ok := resolveGradient(id, b.servers, path, b.vp, b.logf); ok {
		*gradOut = &g
	}
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
