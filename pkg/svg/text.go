package svg

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// maxTspanDepth bounds <tspan> nesting inside one <text>. SVG text nests a
// handful of levels at most in real documents; deeper input is hostile (or
// broken) and is truncated with a log rather than recursed into, mirroring
// maxUseDepth/maxMarkerChainDepth's rationale — the walk here is recursive,
// so an unbounded depth is an unbounded Go stack.
const maxTspanDepth = 64

// maxTextChars bounds the total number of positioned characters one <text>
// element may lower to. Each character costs a TextChar (position, rotation,
// and a per-character style pointer) at build time and a shaped glyph plus a
// transformed outline at paint time, so an adversarial document with a
// multi-megabyte text node would otherwise allocate without limit before any
// draw-call budget (which lives in pkg/svg/draw and never sees build-time
// work) could intervene. Far above any legitimate document: a 200k-character
// <text> is already three orders of magnitude past a plausible label.
const maxTextChars = 200_000

// Text is one <text> element lowered to a scene node: a flat, document-order
// list of positioned characters, each carrying the style resolved at its own
// point in the <text>/<tspan> tree.
//
// The flattening is what makes SVG's positioning model tractable. x/y/dx/dy/
// rotate are per-CHARACTER lists that thread through the whole subtree in
// document order (SVG2 §11.5): a <tspan> inherits the running cursor from its
// parent, may reset it with its own absolute list, and hands the advanced
// cursor back when it ends. Resolving that during the tree walk — here, once,
// at build time — means pkg/svg/draw never has to walk a tree at all: it
// shapes each style run, then places glyph i at Chars[i]'s resolved
// adjustments.
//
// Like every other scene node, a Text is read-only after Parse and shared
// lock-free across the engine's parallel page-render fan-out.
type Text struct {
	// M is the <text> element's own transform attribute.
	M render.Matrix

	// Chars is every character of the <text> subtree, in document order,
	// after whitespace processing. A character with no ink (a space) is kept:
	// it advances the cursor and can carry its own dx/dy.
	Chars []TextChar

	// Anchors marks the index of each text-chunk start. SVG's text-anchor
	// applies per CHUNK, not per <text>: a chunk begins at the first
	// character and at every character that carries an ABSOLUTE x (or y)
	// reset, so <text x="10 50 90"> is three separately-anchored chunks. The
	// slice is strictly increasing and always begins with 0 for a non-empty
	// Chars.
	Anchors []int

	// Lengths are the textLength/lengthAdjust requests found in the subtree,
	// each against the [Start,End) range of Chars its element covered. See
	// TextLength.
	//
	// They are recorded here rather than folded into TextChar because a
	// textLength constrains a RANGE as a whole — it is the one text property
	// whose effect cannot be expressed per character until the range has been
	// shaped and measured, which happens in pkg/svg/draw.
	Lengths []TextLength

	// ClipPath and Mask are the resolved clip-path/mask references on the
	// <text> element itself, or nil when absent. See Group.ClipPath.
	ClipPath *ClipPath
	Mask     *Mask

	// Opacity is the <text> ELEMENT's own (non-inherited) opacity in [0,1],
	// defaulting to 1. It is also folded into each character's Style (that
	// is how the ordinary, unfiltered paint path consumes it, per glyph), so
	// this field is redundant for normal rendering and exists for the one
	// case that cannot recover it from the characters: a FILTER.
	//
	// SVG applies a filter BEFORE opacity, so a filtered <text> must paint
	// its source at full opacity and attenuate the RESULT. The element's own
	// factor cannot be read back off the characters, because opacity is
	// non-inherited: a <tspan opacity="0.25"> inside a <text opacity="0.5">
	// gives its characters 0.25 outright (it REPLACES rather than multiplies
	// — verified against the cascade), so a <text opacity="0.5">
	// containing only that tspan is indistinguishable, character-wise, from
	// a fully opaque <text> containing it. Recording the element's own value
	// here is the only way to keep the two apart.
	Opacity float64

	// Filter is the resolved filter reference on the <text> element itself,
	// or nil when absent. See Group.Filter.
	//
	// A filter region on text uses the REAL placed-glyph extent (see
	// pkg/svg/draw's textUserBounds), never pkg/svg's build-time textBBox
	// estimate: textBBox assumes a half-em per character, which measures up
	// to 2.25x off, and a filter region that wrong visibly clips the
	// filtered result rather than merely shifting a gradient.
	Filter *Filter
}

// TextLength is one element's textLength/lengthAdjust request: the exact
// advance width its character range must occupy, and how the difference from
// the natural width is absorbed.
//
// SVG2 §11.5 defines textLength on both <text> and <tspan>, and the two can
// nest (resvg's on-text-and-tspan.svg puts one on each). Innermost wins for
// the characters they both cover, which the paint pass gets by applying the
// requests outermost-first, exactly as resolvePositions does for the position
// lists.
type TextLength struct {
	// Start and End bound the character range, as indices into Text.Chars.
	Start, End int

	// Target is the requested advance width in user units. It is always >= 0:
	// a negative textLength is invalid per SVG and is dropped at parse time
	// rather than recorded (resvg's negative.svg renders at the natural
	// width).
	Target float64

	// Glyphs reports lengthAdjust="spacingAndGlyphs", which scales the glyph
	// OUTLINES horizontally in addition to the inter-glyph gaps. The default,
	// lengthAdjust="spacing" (Glyphs false), leaves outlines untouched and
	// distributes the whole difference into the gaps.
	Glyphs bool
}

func (*Text) isNode() {}

// TextChar is one positioned character of a Text: its rune, the style in
// effect where it appeared in the <text>/<tspan> tree, and its resolved
// position adjustments.
type TextChar struct {
	// R is the character itself.
	R rune

	// Style is the fully resolved style at this character's position in the
	// tree — its own <tspan>'s, not the <text>'s. Two adjacent characters
	// with different styles start different shaping runs.
	Style Style

	// fillGradient/fillPattern/strokeGradient/strokePattern are the resolved
	// paint servers for this character's style, mirroring Shape's identical
	// fields (see that type's doc comment for why resolution must happen at
	// build time, before Parse discards the document index). All nil for the
	// common solid-color case.
	//
	// Unlike Shape's, these are UNEXPORTED and read through the four
	// accessors below. Shape can expose its fields directly because
	// paintServer/patternPaint are only ever reached from pkg/svg/draw
	// through the `gradient`/`pattern` interfaces there; a struct FIELD of
	// unexported type on an exported struct is a different matter — an
	// out-of-package caller can see it but cannot name its type, which makes
	// the field useless to everyone except by accident. Accessors returning
	// the same interface-satisfying value keep the seam honest.
	fillGradient   *paintServer
	strokeGradient *paintServer
	fillPattern    *patternPaint
	strokePattern  *patternPaint

	// AbsX/AbsY carry an ABSOLUTE position reset from an x=/y= list entry:
	// HasAbsX means the pen's X jumps to AbsX before this character (and a
	// new text chunk begins), independently of Y. A character with neither
	// flag continues from the running pen position.
	AbsX, AbsY       float64
	HasAbsX, HasAbsY bool

	// DX/DY are RELATIVE offsets from a dx=/dy= list entry, applied to the
	// pen after any absolute reset and before the glyph is placed. They
	// permanently shift the pen (they are not per-glyph-only decorations),
	// which is what makes <text dx="20 6 10"> shift each successive
	// character cumulatively.
	DX, DY float64

	// clipPath and mask are the resolved clip-path/mask references on the
	// <tspan> this character came from, or nil. Both are SVG 2 features on a
	// tspan (the resvg corpus's tspan/with-clip-path.svg and with-mask.svg),
	// and both are NON-inherited, so a character carries one only when its
	// own innermost element set it — which is exactly what resolving them
	// per character, off the style already resolved at that point in the
	// tree, gives.
	//
	// The <text> element's OWN clip-path/mask live on Text, not here: they
	// apply to the whole node as a unit, which is a different compositing
	// shape (one group around everything, versus one group per run of
	// characters sharing a reference).
	clipPath *ClipPath
	mask     *Mask

	// RotateDeg is the per-character rotation in DEGREES, applied about the
	// character's own origin on the baseline. Note the SVG asymmetry this
	// encodes: a rotate list SHORTER than the text repeats its LAST value
	// for every remaining character (SVG2 §11.5), unlike x/y/dx/dy, whose
	// short lists simply stop applying. Lowering resolves that here, so
	// every character carries its own final angle.
	RotateDeg float64
}

// GradientPaint is a resolved gradient paint server, as returned by
// TextChar.FillGradient/StrokeGradient. It is an interface rather than the
// concrete type so pkg/svg keeps paintServer unexported while a
// TextChar's resolved paint still reaches pkg/svg/draw — the same
// accessor-surface-only contract Style.FillPaint/StrokePaint follow, and the
// exact method set pkg/svg/draw's own `gradient` interface requires.
type GradientPaint interface {
	// Shader returns the gradient's colour function, in the gradient's own
	// local coordinate space.
	Shader() render.Shader
	// Matrix maps that local space into the painted element's user space
	// (i.e. composed BEFORE the element's own transform).
	Matrix() render.Matrix
}

// PatternPaint is a resolved pattern paint server, as returned by
// TextChar.FillPattern/StrokePattern. See GradientPaint for why this is an
// interface; the method set matches pkg/svg/draw's own `pattern` interface.
type PatternPaint interface {
	// Tile is the pattern's content, painted once per repeated cell.
	Tile() *Group
	// Matrix is the patternTransform, mapping pattern space into the painted
	// element's user space.
	Matrix() render.Matrix
	// Cell reports the tile cell's origin and size in that user space.
	Cell() (x, y, w, h float64)
	// ContentMatrix maps the tile's own content space into cell space.
	ContentMatrix() render.Matrix
}

// FillGradient returns the character's resolved fill gradient, or nil when
// its fill is not a (successfully resolved) gradient reference.
func (c TextChar) FillGradient() GradientPaint {
	if c.fillGradient == nil {
		return nil // typed-nil guard: see the field's doc comment
	}
	return c.fillGradient
}

// StrokeGradient returns the character's resolved stroke gradient, or nil.
func (c TextChar) StrokeGradient() GradientPaint {
	if c.strokeGradient == nil {
		return nil
	}
	return c.strokeGradient
}

// FillPattern returns the character's resolved fill pattern, or nil.
func (c TextChar) FillPattern() PatternPaint {
	if c.fillPattern == nil {
		return nil
	}
	return c.fillPattern
}

// StrokePattern returns the character's resolved stroke pattern, or nil.
func (c TextChar) StrokePattern() PatternPaint {
	if c.strokePattern == nil {
		return nil
	}
	return c.strokePattern
}

// ClipPath returns the resolved clip-path in effect for this character (from
// its own <tspan> or an enclosing one), or nil. It is a *ClipPath rather than
// an interface because ClipPath is already an exported concrete type.
func (c TextChar) ClipPath() *ClipPath { return c.clipPath }

// Mask returns the resolved mask in effect for this character, or nil. See
// ClipPath.
func (c TextChar) Mask() *Mask { return c.mask }

// buildText converts a <text> element into a Text scene node, or nil when it
// contributes nothing (invisible, or no characters after whitespace
// processing). st is the <text>'s own already-resolved style.
func (b *sceneBuilder) buildText(el *element, st Style, ctx *cascadeCtx) Node {
	if !st.visible {
		// visibility:hidden on the <text> itself drops it outright, mirroring
		// buildShape. A <tspan> that re-enables visibility inside a hidden
		// <text> is handled per character below, not here.
		return nil
	}

	// A text-decoration inherited from OUTSIDE the <text> — a <g> or the root
	// — is re-anchored to the <text> element itself before the walk begins.
	//
	// SVG resolves a decoration's paint at the declaring element, but resvg
	// (whose reference PNGs are this corpus's ground truth) treats the <text>
	// as the outermost element a decoration can be declared at: its
	// outside-the-text-element.svg puts text-decoration and fill="green" on a
	// <g> wrapping a fill="black" <text>, and renders a BLACK underline, and
	// style-resolving-2.svg repeats the point with a red <g> around a
	// yellow/green <text>. Both are only explicable if the ancestor's
	// declaration keeps its LINE but adopts the <text>'s paint and metrics.
	st = st.rebaseDecorations()

	// Two baseline properties must not cross the <text> boundary: the
	// accumulated baseline-shift, and a dominant-baseline that arrived from a
	// <g> rather than being written on the <text> itself. See resetBaselines
	// for the dominant-baseline half.
	//
	// For baseline-shift, resvg asserts it three separate ways — inheritance-1 puts
	// baseline-shift="super" directly on a <text> and renders it flush on the
	// baseline; inheritance-4 does the same with a plain <tspan> inside;
	// inheritance-5 adds an explicit baseline-shift="baseline" on that tspan —
	// and all three overlay a red unshifted <text> that the black one must
	// exactly cover. inheritance-3's baseline-shift="inherit" on a <text>
	// inside a <g baseline-shift="super"> lands here too, and is likewise
	// ignored, which is what makes it match its own red reference.
	st = st.resetBaselines()

	tb := &textBuilder{b: b, ctx: ctx}
	tb.walk(el, st, xmlSpaceOf(el, false), 0, nil, nil)
	tb.dropTrailingSpace()
	if len(tb.chars) == 0 {
		return nil
	}
	tb.resolvePositions()
	// After resolvePositions: the objectBoundingBox approximation reads the
	// first absolute x/y, which only exists once the position lists have been
	// applied onto the characters.
	tb.resolveTextPaints()

	t := &Text{
		M:       elementTransform(el, b.logf),
		Chars:   tb.chars,
		Anchors: tb.anchors,
		Lengths: tb.trimLengths(),
		// st is the <text> element's own resolved style, so this is the
		// element's own opacity — see Text.Opacity for why it is recorded
		// separately from the per-character styles that already carry it.
		Opacity: st.opacity,
	}
	if ref, ok := st.ClipPathRef(); ok {
		t.ClipPath = b.resolveClipPathRef(ref)
	}
	if ref, ok := st.MaskRef(); ok {
		t.Mask = b.resolveMaskRef(ref)
	}
	if ref, ok := st.FilterRef(); ok {
		f, ok := b.resolveFilterRef(ref)
		if !ok {
			return nil // not rendered at all; see buildGroupElement
		}
		t.Filter = f
	}
	return t
}

// textBuilder accumulates one <text> element's characters during the subtree
// walk, then resolves the per-character position lists over them.
//
// The two-phase split matters: the x/y/dx/dy/rotate lists on an element apply
// to the characters of THAT element's subtree by index, but a character's
// index is only known once its preceding siblings' text has been collapsed —
// and whitespace collapsing itself depends on what came before across element
// boundaries (a space at the start of a <tspan> collapses away if the
// preceding <tspan> ended with one). So the walk collects characters and
// records each element's lists against the character range it covered, and
// resolvePositions applies them afterward.
type textBuilder struct {
	b   *sceneBuilder
	ctx *cascadeCtx

	// chars is the flat character list built so far.
	chars []TextChar

	// collapsed parallels chars, marking each entry that is a space PRODUCED
	// BY COLLAPSING rather than one preserved verbatim under
	// xml:space="preserve". Only a collapsed space is subject to SVG's
	// trailing-whitespace strip; a preserved one is content. It is a parallel
	// slice rather than a TextChar field because nothing outside this builder
	// needs it — the distinction exists only while the list is being built.
	collapsed []bool

	// lists holds one entry per element in the subtree that carried at least
	// one position list, paired with the character range it covers.
	lists []charRangeLists

	// lengths holds one entry per element in the subtree that carried a valid
	// textLength, paired with the character range it covers. Appended
	// innermost-first (recordTextLength runs after the child recursion
	// returns), so the paint pass iterates in REVERSE to apply outermost
	// first and let the innermost request win — the same ordering trick
	// resolvePositions uses.
	lengths []TextLength

	// anchors is the resolved text-chunk start indices; filled by
	// resolvePositions.
	anchors []int

	// pendingSpace records that a collapsible whitespace run was seen and a
	// single space must be emitted BEFORE the next non-space character —
	// deferred rather than emitted eagerly so a trailing whitespace run at
	// the very end of the <text> collapses away entirely (SVG's default
	// xml:space handling strips leading and trailing space). pendingStyle is
	// the style that space would carry.
	pendingSpace bool
	pendingStyle Style
	// pendingClip/pendingMask are the deferred space's clip-path/mask, kept
	// alongside pendingStyle for the same reason: the space belongs to the
	// element that produced it, not to whichever element happens to supply
	// the next inked character.
	pendingClip *ClipPath
	pendingMask *Mask

	// sawInk records whether any non-space character has been emitted yet, so
	// a LEADING whitespace run is dropped rather than becoming a space.
	sawInk bool

	// truncated records that maxTextChars was hit, so the walk stops
	// appending and logs once.
	truncated bool
}

// charRangeLists pairs one element's position lists with the [start,end)
// range of textBuilder.chars its subtree produced.
type charRangeLists struct {
	start, end int
	x, y       []float64
	dx, dy     []float64
	rotate     []float64
	hasRotate  bool
}

// walk descends one <text>/<tspan> subtree in document order, appending
// characters and recording el's own position lists against the character
// range it covers. st is el's own resolved style; preserveSpace is the
// inherited xml:space flag.
//
// clip and mask are the resolved clip-path/mask in effect for the characters
// emitted here. They are threaded as parameters rather than read off st
// because both are NON-inherited: an inner <tspan> that sets neither must
// carry its ANCESTOR tspan's, since the ancestor's clip geometrically
// contains everything inside it, while st has already reset them to "".
func (tb *textBuilder) walk(el *element, st Style, preserveSpace bool, depth int, clip *ClipPath, mask *Mask) {
	if depth > maxTspanDepth {
		tb.b.warnOnceMsg("text-depth", "svg: <tspan> nesting exceeded 64 levels; deeper content was dropped")
		return
	}

	// A <tspan>'s own clip-path/mask (SVG 2) replace the inherited pair for
	// its subtree; absent, the enclosing one still applies.
	if ref, ok := st.ClipPathRef(); ok {
		clip = tb.b.resolveClipPathRef(ref)
	}
	if ref, ok := st.MaskRef(); ok {
		mask = tb.b.resolveMaskRef(ref)
	}

	start := len(tb.chars)

	for _, c := range el.content {
		if tb.truncated {
			break
		}
		if c.el == nil {
			tb.appendText(c.text, st, preserveSpace, clip, mask)
			continue
		}
		kid := c.el
		if kid.space != svgNS {
			continue // foreign-namespace child: skip silently, like buildNode
		}
		// A collapsible space seen just before a child element belongs to
		// THIS element's character range, not the child's. Emitting it here
		// rather than letting it ride into the child is what keeps a <tspan>'s
		// own x/y list aligned with the child's first REAL character: the
		// corpus's tspan/pseudo-multi-line.svg puts x="40" on three sibling
		// tspans separated by source indentation, and a deferred space landing
		// inside the child would take that x for itself and push the visible
		// text one space-width right on every line but the first.
		//
		// The trailing-space strip this deferral also implements is preserved
		// by dropTrailingSpace, which runs once over the finished list.
		tb.flushPendingSpace()
		switch kid.local {
		case "tspan":
			kidStyle := st.apply(kid, tb.ctx)
			if !kidStyle.display {
				// display:none on a <tspan> removes its characters entirely —
				// they do not advance the cursor either (the corpus's
				// tspan/rotate-and-display-none.svg asserts the following
				// text is NOT shifted by the hidden run).
				continue
			}
			tb.walk(kid, kidStyle, xmlSpaceOf(kid, preserveSpace), depth+1, clip, mask)
		case "tref":
			// Removed from SVG 2 and unimplemented in every current browser
			// (see the design's decision 4): dropped with a log, not deferred.
			tb.b.warnOnce("tref")
		case "textPath":
			// Deferred to a later PR (design decision 3): render its text on
			// the straight baseline rather than dropping it, so the content
			// is still visible and the degradation is diagnosable.
			tb.b.warnOnceMsg("textPath", "svg: <textPath> not yet supported; rendering its text on a straight baseline")
			kidStyle := st.apply(kid, tb.ctx)
			if kidStyle.display {
				tb.walk(kid, kidStyle, xmlSpaceOf(kid, preserveSpace), depth+1, clip, mask)
			}
		case "title", "desc", "metadata":
			// Metadata children of <text> are not rendered content.
		default:
			// Any other element inside <text> (an <a>, or something
			// unrecognized) contributes its text content but nothing else.
			// This mirrors buildNode's forgiving container default rather
			// than silently dropping a wrapper's text.
			kidStyle := st.apply(kid, tb.ctx)
			if kidStyle.display {
				tb.walk(kid, kidStyle, xmlSpaceOf(kid, preserveSpace), depth+1, clip, mask)
			}
		}
	}

	tb.recordLists(el, start, len(tb.chars))
	tb.recordTextLength(el, st, start, len(tb.chars))
}

// recordTextLength parses el's textLength/lengthAdjust attributes and records
// the request against the [start,end) character range el's subtree produced.
//
// textLength is an XML ATTRIBUTE, not a presentation attribute: it never
// participates in the cascade and never inherits (resvg's inherit.svg puts
// textLength="inherit" on a <text> inside a <g textLength="150"> and titles
// the case "Not allowed", expecting the natural width). So it is read straight
// off el.attrs here, not through ctx.resolve.
//
// A negative or unparseable value is dropped entirely, per SVG error handling;
// zero is legal and collapses the range onto a point (resvg's zero.svg).
// st supplies the font-size a percentage resolves against.
func (tb *textBuilder) recordTextLength(el *element, st Style, start, end int) {
	raw, ok := el.attrs["textLength"]
	if !ok || end <= start {
		return
	}
	target, ok := parseFontRelLength(raw, st.FontSizePt())
	if !ok || target < 0 {
		tb.b.logf("svg: ignoring textLength=%q: %s", raw, textLengthReason(ok, target))
		return
	}
	// The same DoS bound letter-spacing takes, for the same reason: a
	// textLength is divided into the inter-glyph gaps, so an unbounded value
	// becomes an unbounded per-glyph pen offset.
	if target > maxSpacingPt {
		target = maxSpacingPt
	}
	tb.lengths = append(tb.lengths, TextLength{
		Start:  start,
		End:    end,
		Target: target,
		Glyphs: strings.TrimSpace(el.attrs["lengthAdjust"]) == "spacingAndGlyphs",
	})
}

// trimLengths clamps every recorded textLength range to the FINAL character
// count and drops any that ended up empty.
//
// The clamp is needed because dropTrailingSpace runs after the walk and can
// shrink tb.chars, leaving a range recorded against characters that no longer
// exist. Reversing here (rather than in the paint pass) puts the slice in
// OUTERMOST-FIRST order, so a consumer applying them in slice order gets the
// innermost request landing last, which is what SVG's nesting rule wants.
func (tb *textBuilder) trimLengths() []TextLength {
	if len(tb.lengths) == 0 {
		return nil
	}
	n := len(tb.chars)
	out := make([]TextLength, 0, len(tb.lengths))
	for i := len(tb.lengths) - 1; i >= 0; i-- {
		l := tb.lengths[i]
		if l.End > n {
			l.End = n
		}
		if l.Start >= l.End {
			continue
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// textLengthReason names why a textLength value was rejected, for the log.
func textLengthReason(parsed bool, v float64) string {
	if !parsed {
		return "unparseable"
	}
	_ = v
	return "negative lengths are invalid"
}

// recordLists parses el's x/y/dx/dy/rotate attributes and records them
// against the [start,end) character range el's subtree produced, if any list
// is present at all. An unparseable list is ignored per SVG error handling
// (parseNumberList already returns nil for a list with any bad token).
func (tb *textBuilder) recordLists(el *element, start, end int) {
	x := textCoordList(el, "x")
	y := textCoordList(el, "y")
	dx := textCoordList(el, "dx")
	dy := textCoordList(el, "dy")
	rotate, hasRotate := textRotateList(el)
	if x == nil && y == nil && dx == nil && dy == nil && !hasRotate {
		return
	}
	tb.lists = append(tb.lists, charRangeLists{
		start: start, end: end,
		x: x, y: y, dx: dx, dy: dy,
		rotate: rotate, hasRotate: hasRotate,
	})
}

// textCoordList parses one of the x/y/dx/dy list attributes into user units.
// Unlike parseNumberList it accepts a UNIT on each entry (the corpus's
// mm-coordinates.svg and em-and-ex-coordinates.svg use them), so each token
// goes through parseLength. A list with any unparseable token is ignored
// entirely, per SVG's error handling for list attributes.
func textCoordList(el *element, name string) []float64 {
	raw, ok := el.attrs[name]
	if !ok {
		return nil
	}
	fields := splitCoordList(raw)
	if len(fields) == 0 {
		return nil
	}
	// Entries past maxTextChars can never be consumed — the lowering walk
	// stops at that many positioned characters — so parsing them would only
	// allocate a slice no one reads. Capping here keeps a pathological list
	// (a multi-million-entry x=) from a large transient allocation without
	// changing behavior for any list a document can actually use.
	if len(fields) > maxTextChars {
		fields = fields[:maxTextChars]
	}
	out := make([]float64, 0, len(fields))
	for _, f := range fields {
		v, ok := parseLength(f, 0)
		if !ok {
			return nil
		}
		out = append(out, v)
	}
	return out
}

// textRotateList parses the rotate list. It reports hasRotate separately from
// a nil slice so an ignored-because-unparseable rotate is distinguishable
// from an absent one — the difference matters because a rotate list, unlike
// x/y/dx/dy, keeps applying its LAST value past the end of the list, and an
// element with rotate="" must not do that.
//
// A single unparseable entry invalidates the whole attribute, matching
// parseNumberList's list-attribute error handling and the corpus's
// rotate-with-an-invalid-angle.svg (which expects NO rotation at all rather
// than a partially-applied list).
func textRotateList(el *element) ([]float64, bool) {
	raw, ok := el.attrs["rotate"]
	if !ok {
		return nil, false
	}
	list := parseNumberList(raw)
	if list == nil {
		return nil, false
	}
	return list, true
}

// splitCoordList splits a coordinate list on commas and whitespace, the same
// separators parseNumberList accepts.
func splitCoordList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
	})
}

// appendText adds one character-data run's characters to the flat list,
// applying whitespace processing.
//
// In the default (non-preserving) mode SVG collapses every run of whitespace —
// including newlines and tabs, which is why the source indentation inside a
// pretty-printed <text> does not become visible space — to a single space,
// and strips leading and trailing whitespace across the WHOLE <text>, not per
// run. That cross-run scope is why the pending-space deferral exists: whether
// a space between two <tspan>s survives depends on what follows it, which is
// not known until the next run arrives.
//
// With xml:space="preserve" every whitespace character is kept, except that
// newlines and tabs still become spaces (SVG2 §11.5: "converted to space
// characters") — only the collapsing and the leading/trailing strip are
// disabled.
func (tb *textBuilder) appendText(s string, st Style, preserveSpace bool, clip *ClipPath, mask *Mask) {
	for _, r := range s {
		if tb.truncated {
			return
		}
		if isXMLSpace(r) {
			if preserveSpace {
				tb.emit(' ', st, clip, mask, false)
				continue
			}
			// Collapsing mode: remember that a space is owed, but only emit
			// it once an inked character follows (and never before the first
			// one), so leading and trailing whitespace vanish.
			if tb.sawInk {
				tb.pendingSpace = true
				tb.pendingStyle = st
				tb.pendingClip, tb.pendingMask = clip, mask
			}
			continue
		}
		if tb.pendingSpace {
			tb.pendingSpace = false
			// Unless a space is already the last character emitted: SVG
			// collapses a whitespace run to ONE space across the whole
			// <text>, including across element boundaries, and
			// flushPendingSpace may already have emitted this run's space
			// when the walk crossed into or out of a <tspan>.
			if !tb.endsWithSpace() {
				tb.emit(' ', tb.pendingStyle, tb.pendingClip, tb.pendingMask, true)
				if tb.truncated {
					return
				}
			}
		}
		tb.sawInk = true
		tb.emit(r, st, clip, mask, false)
	}
}

// flushPendingSpace emits a deferred collapsible space now, if one is owed.
// It is called at an element boundary, where the space's owning range is
// about to change — see the call site in walk.
func (tb *textBuilder) flushPendingSpace() {
	if !tb.pendingSpace {
		return
	}
	tb.pendingSpace = false
	if tb.endsWithSpace() {
		return // the run already contributed its one collapsed space
	}
	tb.emit(' ', tb.pendingStyle, tb.pendingClip, tb.pendingMask, true)
}

// endsWithSpace reports whether the last character emitted is a space, which
// is how a whitespace run spanning an element boundary is kept to the single
// space SVG's collapsing rule allows.
func (tb *textBuilder) endsWithSpace() bool {
	return len(tb.chars) > 0 && tb.chars[len(tb.chars)-1].R == ' '
}

// dropTrailingSpace implements the other half of SVG's default whitespace
// handling: a whitespace run at the very END of a <text> is stripped.
//
// The deferral in appendText handles it for a space never followed by an
// inked character, but flushPendingSpace can now emit one at an element
// boundary that turns out to be the last thing in the <text> — e.g. the
// indentation before a closing </tspan></text> pair. Removing it here, once,
// covers both routes without either needing to predict the future.
func (tb *textBuilder) dropTrailingSpace() {
	tb.pendingSpace = false
	for len(tb.chars) > 0 {
		last := len(tb.chars) - 1
		if tb.chars[last].R != ' ' || !tb.collapsed[last] {
			// Only a COLLAPSED space is strippable. A space that came from an
			// xml:space="preserve" run is content the author asked for, and
			// SVG's trailing-strip rule does not apply to it — the corpus's
			// tspan/xml-space fixtures depend on the distinction.
			return
		}
		tb.chars = tb.chars[:last]
		tb.collapsed = tb.collapsed[:last]
	}
}

// emit appends one character with the given style, resolving that style's
// paint servers, and trips the maxTextChars guard.
func (tb *textBuilder) emit(r rune, st Style, clip *ClipPath, mask *Mask, collapsedSpace bool) {
	if len(tb.chars) >= maxTextChars {
		if !tb.truncated {
			tb.truncated = true
			tb.b.warnOnceMsg("text-budget", "svg: <text> character budget exhausted; remaining characters were dropped")
		}
		return
	}
	tb.chars = append(tb.chars, TextChar{R: r, Style: st, clipPath: clip, mask: mask})
	tb.collapsed = append(tb.collapsed, collapsedSpace)
}

// textBBox is the geometry a text character's paint servers resolve against.
//
// A paint server with the default gradientUnits/patternUnits of
// objectBoundingBox needs the painted element's bounding box, and a text
// chunk's box is not known until shaping has run — which happens in
// pkg/svg/draw, a layer that by design cannot write back into the read-only
// scene graph. Rather than resolve against nothing (which makes
// resolveGradient report a degenerate box and drop the paint entirely,
// leaving gradient-filled text unpainted), the box is approximated from the
// text's own position and font size: a chunk starting at the pen origin,
// one em tall and estimated wide.
//
// The approximation is only ever visible for an objectBoundingBox server on
// text; userSpaceOnUse — the far more common authoring choice for text, and
// the one every corpus fixture in this tranche uses — is exact, since it
// ignores the box completely.
func textBBox(originX, originY, advance, sizePt float64) *render.Path {
	if advance <= 0 {
		advance = sizePt
	}
	p := &render.Path{}
	// One em above the baseline to the descender, the conventional em box.
	p.MoveTo(originX, originY-sizePt*0.8)
	p.LineTo(originX+advance, originY-sizePt*0.8)
	p.LineTo(originX+advance, originY+sizePt*0.2)
	p.LineTo(originX, originY+sizePt*0.2)
	p.Close()
	return p
}

// resolveTextPaints resolves the fill/stroke url() references for every
// DISTINCT style in the character list, writing the result onto each
// character that carries that style.
//
// It runs once after the whole subtree is walked, not per character, for two
// reasons: resolving a gradient allocates (a shader plus its stop table), so
// a 200-character <text> with one gradient fill would otherwise build 200
// identical shaders; and the objectBoundingBox approximation above needs the
// text's overall extent, which only exists once every character is known.
func (tb *textBuilder) resolveTextPaints() {
	if len(tb.chars) == 0 {
		return
	}
	// Approximate the whole <text>'s extent for the objectBoundingBox case.
	// Every character shares it: a per-chunk box would be marginally closer
	// but is still an approximation, and one box keeps a gradient continuous
	// across the text the way an author writing objectBoundingBox expects.
	originX, originY := 0.0, 0.0
	maxSize := 0.0
	for _, c := range tb.chars {
		if c.HasAbsX {
			originX = c.AbsX
			break
		}
	}
	for _, c := range tb.chars {
		if c.HasAbsY {
			originY = c.AbsY
			break
		}
	}
	for _, c := range tb.chars {
		if s := c.Style.fontSizePt; s > maxSize {
			maxSize = s
		}
	}
	// A crude advance estimate: half an em per character is close enough for
	// a bounding box that is already an approximation, and never zero.
	bbox := textBBox(originX, originY, float64(len(tb.chars))*maxSize*0.5, maxSize)

	type resolved struct {
		fillGrad   *paintServer
		strokeGrad *paintServer
		fillPat    *patternPaint
		strokePat  *patternPaint
	}
	memo := map[[2]string]resolved{}
	for i := range tb.chars {
		c := &tb.chars[i]
		fillRef, hasFill := c.Style.FillServer()
		strokeRef, hasStroke := c.Style.StrokeServer()
		if !hasFill && !hasStroke {
			continue
		}
		key := [2]string{fillRef, strokeRef}
		r, ok := memo[key]
		if !ok {
			if hasFill {
				if id, ok := fragmentID(fillRef); ok {
					tb.b.resolvePaint(id, bbox, &r.fillGrad, &r.fillPat)
				}
			}
			if hasStroke {
				if id, ok := fragmentID(strokeRef); ok {
					tb.b.resolvePaint(id, bbox, &r.strokeGrad, &r.strokePat)
				}
			}
			memo[key] = r
		}
		c.fillGradient, c.fillPattern = r.fillGrad, r.fillPat
		c.strokeGradient, c.strokePattern = r.strokeGrad, r.strokePat
	}
}

// resolvePositions applies every recorded element's position lists onto the
// character range it covers, then computes the text-chunk anchor indices.
//
// The SVG rules this implements (SVG2 §11.5, "Text layout — the x, y, dx, dy
// and rotate attributes"):
//
//   - Each list applies to the characters of its own element's subtree by
//     INDEX: entry i goes to the (i+1)-th character in that subtree.
//   - A list SHORTER than the subtree's text simply stops: the remaining
//     characters get no adjustment from that list and continue from the
//     running pen position. rotate is the one exception — its LAST value
//     persists for every remaining character.
//   - A list LONGER than the text has its surplus entries ignored.
//   - x/y are absolute (they reset the pen); dx/dy are relative.
//   - A nested element's own list wins over the ancestor's for the characters
//     they both cover, which falls out of applying ancestors first: the walk
//     appends a child's charRangeLists entry BEFORE its parent's (recordLists
//     runs after the child recursion returns), so iterating in reverse
//     applies outermost-first and lets the innermost write land last.
func (tb *textBuilder) resolvePositions() {
	for i := len(tb.lists) - 1; i >= 0; i-- {
		l := tb.lists[i]
		for j := l.start; j < l.end; j++ {
			k := j - l.start
			c := &tb.chars[j]
			if k < len(l.x) {
				c.AbsX, c.HasAbsX = l.x[k], true
			}
			if k < len(l.y) {
				c.AbsY, c.HasAbsY = l.y[k], true
			}
			if k < len(l.dx) {
				c.DX = l.dx[k]
			}
			if k < len(l.dy) {
				c.DY = l.dy[k]
			}
			if l.hasRotate && len(l.rotate) > 0 {
				// The last-value-persists rule: past the end of the list,
				// every remaining character keeps the final angle.
				if k < len(l.rotate) {
					c.RotateDeg = l.rotate[k]
				} else {
					c.RotateDeg = l.rotate[len(l.rotate)-1]
				}
			}
		}
	}

	// Text chunks: the first character always starts one, and so does every
	// character carrying an absolute x or y reset (SVG2 §11.5 — "a new text
	// chunk is started whenever an absolute position adjustment is made").
	for i := range tb.chars {
		if i == 0 || tb.chars[i].HasAbsX || tb.chars[i].HasAbsY {
			tb.anchors = append(tb.anchors, i)
		}
	}
}

// xmlSpaceOf resolves el's xml:space attribute against the inherited value.
// "preserve" turns preservation on, "default" turns it off, and anything else
// (including the attribute's absence) inherits.
//
// The attribute arrives under the prefixed "xml:space" key, never the bare
// local name: the decoder reports it in the XML namespace, and buildAttrs
// folds it under the prefixed key specifically so it cannot collide with a
// same-named SVG attribute. Looking up the bare "space" here would resurrect
// exactly that collision.
func xmlSpaceOf(el *element, inherited bool) bool {
	v, ok := el.attrs["xml:space"]
	if !ok {
		return inherited
	}
	switch strings.TrimSpace(v) {
	case "preserve":
		return true
	case "default":
		return false
	default:
		return inherited
	}
}

// isXMLSpace reports whether r is one of the four whitespace characters SVG's
// text-processing rules collapse: space, tab, carriage return, and line feed.
// Deliberately narrower than unicode.IsSpace — SVG2 §11.5 names exactly these
// four, and collapsing e.g. U+00A0 NO-BREAK SPACE would defeat its purpose.
func isXMLSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\r' || r == '\n'
}
