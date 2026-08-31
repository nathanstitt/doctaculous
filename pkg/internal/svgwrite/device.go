package svgwrite

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// Device implements render.Device by appending SVG markup for a single page.
//
// Coordinates are written verbatim: device space is already top-left-origin and
// Y-down, which is SVG's default user space, so unlike pdfwrite there is no
// page-level flip to undo.
//
// Not safe for concurrent use; one Device serves one page, matching how the
// page fan-out already works.
type Device struct {
	// buf is the current output target. BeginGroup redirects it to a scratch
	// buffer and EndGroup restores it, so all emit paths write through this
	// one field rather than knowing whether a group is open.
	buf *strings.Builder
	// root is the page-level buffer, held separately so contentTo can restore
	// it and so defs never land inside a group's scratch buffer.
	root strings.Builder
	// defs accumulates <clipPath>, <mask> and gradient definitions, emitted in
	// a single <defs> block ahead of the content. Definitions are page-scoped
	// rather than group-scoped because an id must resolve from anywhere in the
	// document, including from inside a group that has already been closed.
	defs strings.Builder

	wPx, hPx int

	// elems tracks currently-open elements so Restore and EndGroup close
	// exactly what they opened. SVG is a tree, so nesting is structural here;
	// a PDF writer gets this for free from q/Q being flat stream operators.
	elems []elem
	// clipRect is the intersected bounding box of the active clips, or nil for
	// unclipped. The emitted <clipPath> elements enforce the exact shape at
	// view time; this approximation exists only so a shading that must be
	// sampled knows how large a region to cover.
	clipRect *clipBounds
	// groups holds one frame per open BeginGroup.
	groups []*groupFrame

	// glyphIDs maps each (face, glyph) actually used to the id of its
	// em-space outline in defs, so a glyph's geometry is written once no
	// matter how often it appears. Keyed on the face INTERFACE VALUE rather
	// than a name: two faces with the same family can have different outlines
	// (different files, different subsets), and identity is what makes the
	// cached path definitely correct for the glyph being drawn.
	glyphIDs map[glyphKey]string

	// emptyClipDefined tracks whether the shared empty-clip definition has been
	// written. Deliberately not a key in warned; see ensureEmptyClip.
	emptyClipDefined bool

	nextID int
	logf   func(string, ...any)
	// warned deduplicates degradation messages so a page with thousands of
	// affected operations logs each distinct reason once.
	warned map[string]bool
}

// elem is one open element in the nesting stack.
type elem struct {
	// tag is the element name to close, or "" for a stack entry that opened no
	// element (a Save with nothing to express). Keeping a zero-width entry
	// rather than skipping the push is what keeps Save/Restore balanced.
	tag string
	// fromSave marks entries pushed by Save, so Restore knows where to stop
	// and EndGroup can close whatever a group's children left open.
	fromSave bool
	// clip is the clipRect in effect before this entry, restored when it pops.
	clip *clipBounds
}

// glyphKey identifies one glyph outline for deduplication.
type glyphKey struct {
	face render.GlyphFace
	gid  uint16
}

// clipBounds is a device-space rectangle bounding the active clip.
type clipBounds struct{ minX, minY, maxX, maxY float64 }

// intersectClip intersects two clip rectangles; a nil operand means
// "unclipped". An empty result is returned as a zero-area rectangle rather
// than nil, since nil would wrongly mean "no clip at all".
func intersectClip(a, b *clipBounds) *clipBounds {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	r := &clipBounds{
		minX: max(a.minX, b.minX),
		minY: max(a.minY, b.minY),
		maxX: min(a.maxX, b.maxX),
		maxY: min(a.maxY, b.maxY),
	}
	if r.minX >= r.maxX || r.minY >= r.maxY {
		return &clipBounds{}
	}
	return r
}

// groupFrame captures what BeginGroup redirected, so EndGroup can restore it.
//
// scratch is a POINTER, and the stack holds *groupFrame rather than groupFrame.
// Both matter: d.buf points at the open frame's scratch builder, so if the
// builder lived inside the slice, growing d.groups on a nested BeginGroup would
// reallocate the backing array and leave d.buf writing into the abandoned copy
// — the inner group's content would vanish and its closing tags would go
// missing. Holding pointers keeps every frame's address stable for its lifetime.
type groupFrame struct {
	prev     *strings.Builder
	scratch  *strings.Builder
	elemBase int
}

// New returns a Device that renders a page wPx by hPx device units.
//
// The size is used for the root element's width/height/viewBox and to size the
// scratch surfaces masks and filters render into.
func New(wPx, hPx int) *Device {
	d := &Device{wPx: wPx, hPx: hPx, warned: map[string]bool{}}
	d.buf = &d.root
	return d
}

// SetLogf installs a degradation logger, matching raster.Device.SetLogf.
func (d *Device) SetLogf(logf func(string, ...any)) { d.logf = logf }

// warnOnce logs a degradation the first time each distinct message appears.
func (d *Device) warnOnce(key, format string, args ...any) {
	if d.logf == nil || d.warned[key] {
		return
	}
	d.warned[key] = true
	d.logf(format, args...)
}

// Size reports the device's pixel dimensions.
func (d *Device) Size() (int, int) { return d.wPx, d.hPx }

// id mints a document-unique id for a <clipPath>, <mask> or gradient.
func (d *Device) id(prefix string) string {
	d.nextID++
	return fmt.Sprintf("%s%d", prefix, d.nextID)
}

// Fill paints path's interior.
func (d *Device) Fill(path *render.Path, paint render.FillPaint) {
	if path == nil || path.Empty() {
		return
	}
	data := pathData(path)
	if data == "" {
		return
	}
	d.buf.WriteString(`<path d="`)
	d.buf.WriteString(data)
	d.buf.WriteByte('"')
	writeFillAttrs(d.buf, paint)
	d.writeBlend(paint.BlendMode)
	d.buf.WriteString("/>\n")
}

// Stroke paints path's outline.
func (d *Device) Stroke(path *render.Path, paint render.StrokePaint) {
	if path == nil || path.Empty() {
		return
	}
	data := pathData(path)
	if data == "" {
		return
	}
	d.buf.WriteString(`<path d="`)
	d.buf.WriteString(data)
	d.buf.WriteByte('"')
	writeStrokeAttrs(d.buf, paint)
	d.writeBlend(paint.BlendMode)
	d.buf.WriteString("/>\n")
}

// writeBlend appends mix-blend-mode, logging a mode SVG cannot express.
func (d *Device) writeBlend(mode string) {
	if mode == "" || mode == "Normal" || mode == "Compatible" {
		return
	}
	css, ok := blendAttr(mode)
	if !ok {
		d.warnOnce("blend:"+mode, "svgwrite: blend mode %q has no CSS equivalent; painting source-over", mode)
		return
	}
	fmt.Fprintf(d.buf, " style=\"mix-blend-mode:%s\"", css)
}

// FillGlyph fills a glyph outline already in device space.
func (d *Device) FillGlyph(outline *render.Path, c render.FillColor, blendMode string) {
	if outline == nil || outline.Empty() {
		return
	}
	d.Fill(outline, render.FillPaint{
		Color:     toRGBA(c),
		Rule:      render.NonZero, // glyph outlines are nonzero by definition
		BlendMode: blendMode,
	})
}

// DrawGlyph paints one shaped glyph as its outline.
//
// Emitting <path> rather than <text> is deliberate; see the package doc.
//
// The outline is defined ONCE per (face, glyph) in <defs> and referenced with
// <use>, rather than transformed into device space and written out in full at
// every occurrence. Glyph outlines are the bulk of a text page's markup — a
// letter is several hundred bytes of curves — and a document repeats the same
// few dozen glyphs thousands of times, so writing the geometry inline makes
// the file grow with character count instead of with alphabet size. Measured
// on a 4-page text document: 1265 glyphs, zero of them byte-identical inline
// (position is baked into the coordinates) versus a few dozen distinct
// outlines once hoisted.
func (d *Device) DrawGlyph(g render.GlyphRef) {
	if g.Face == nil {
		return
	}
	outline := g.Face.Outline(g.GID)
	if outline == nil || outline.Empty() {
		return // whitespace and missing glyphs have no geometry
	}
	ref, ok := d.glyphDef(g.Face, g.GID, outline)
	if !ok {
		// Could not hoist (degenerate geometry): fall back to an inline path
		// so the glyph still paints.
		d.FillGlyph(render.TransformPath(outline, g.Transform), g.Color, g.Blend)
		return
	}
	hex, alpha := colorAttr(toRGBA(g.Color))
	fmt.Fprintf(d.buf, `<use href="#%s" fill=%q`, ref, hex)
	writeOpacity(d.buf, "fill-opacity", alpha)
	if !isIdentity(g.Transform) {
		fmt.Fprintf(d.buf, " transform=%q", matrixAttr(g.Transform))
	}
	// The source characters this glyph stands for. Outlines carry no text, so
	// without this the document is a picture of words and nothing — search,
	// copy, or a screen reader — can recover what it says. aria-label rather
	// than <title>: a <title> child would make every glyph a hover tooltip in
	// a browser, and would cost an extra element per glyph.
	if label := escapeText(strings.TrimSpace(string(g.Runes))); label != "" {
		fmt.Fprintf(d.buf, " aria-label=%q", label)
	}
	d.writeBlend(g.Blend)
	d.buf.WriteString("/>\n")
}

// glyphDef emits outline once per (face, gid) and returns the id to reference.
//
// The outline is stored in EM space exactly as the face provides it, so the
// per-occurrence <use> carries the full em-space-to-device transform (position,
// size and skew) that GlyphRef.Transform already expresses. That is what makes
// one definition serve every size and position the glyph appears at.
func (d *Device) glyphDef(face render.GlyphFace, gid uint16, outline *render.Path) (id string, ok bool) {
	// GlyphFace is an interface, and using one as a map key panics if the
	// concrete type is unhashable (a face implemented on a slice or map type).
	// Every face in this repo is a pointer, so this is defensive rather than
	// expected — but a panic in a document writer is never an acceptable
	// failure mode, and falling back to an inline path is invisible to output
	// correctness.
	defer func() {
		if recover() != nil {
			id, ok = "", false
		}
	}()
	key := glyphKey{face: face, gid: gid}
	if id, ok := d.glyphIDs[key]; ok {
		return id, true
	}
	data := pathData(outline)
	if data == "" {
		return "", false
	}
	if d.glyphIDs == nil {
		d.glyphIDs = map[glyphKey]string{}
	}
	id = d.id("g")
	fmt.Fprintf(&d.defs, `<path id="%s" d="%s"/>`+"\n", id, data)
	d.glyphIDs[key] = id
	return id, true
}

// DrawImage draws img mapped by ctm.
//
// The Device contract maps the image's unit square through ctm, so the element
// is emitted at width=1 height=1 under a matrix transform. preserveAspectRatio
// is disabled because the caller's ctm already encodes the exact placement,
// including any deliberate non-uniform scale.
//
// The extra flip is NOT redundant with whatever Y flip ctm already carries.
// PDF image space puts the image's TOP row at v=1 (the raster backend samples
// it as `1-v`, see pkg/render/raster/device.go), whereas an SVG <image> puts
// its top row at y=0. Mapping the unit square directly would therefore render
// every image upside down — the ctm's own negative Y scale positions the box
// but does not reorder the rows. Pre-flipping within the unit square converts
// between the two conventions once, here, rather than making every caller
// know about it.
func (d *Device) DrawImage(img image.Image, ctm render.Matrix, alpha float64, blendMode string) {
	href, ok := pngDataURI(img)
	if !ok {
		d.warnOnce("image", "svgwrite: image could not be encoded; skipped")
		return
	}
	place := render.Matrix{A: 1, D: -1, F: 1}.Mul(ctm) // v -> 1-v
	d.buf.WriteString(`<image width="1" height="1" preserveAspectRatio="none" href="`)
	d.buf.WriteString(href)
	d.buf.WriteByte('"')
	if !isIdentity(place) {
		fmt.Fprintf(d.buf, " transform=%q", matrixAttr(place))
	}
	writeOpacity(d.buf, "opacity", alpha)
	d.writeBlend(blendMode)
	d.buf.WriteString("/>\n")
}

// PushClip intersects the current clip with path.
//
// SVG expresses clipping structurally, so this opens a <g clip-path=...> that
// stays open until the matching Restore closes it. Intersection therefore
// falls out of nesting: an inner clip inside an outer one is the intersection,
// exactly as the Device contract requires.
func (d *Device) PushClip(path *render.Path, rule render.FillRule) {
	if path == nil || path.Empty() {
		// A clip to nothing must hide subsequent paint, not pass it through.
		d.ensureEmptyClip()
		d.openElem("g", ` clip-path="url(#empty-clip)"`, false)
		d.clipRect = &clipBounds{}
		return
	}
	id := d.id("clip")
	d.defs.WriteString(`<clipPath id="`)
	d.defs.WriteString(id)
	d.defs.WriteString(`" clipPathUnits="userSpaceOnUse"><path d="`)
	appendPathData(&d.defs, path)
	d.defs.WriteByte('"')
	if r := fillRuleAttr(rule); r != "" {
		fmt.Fprintf(&d.defs, " clip-rule=%q", r)
	}
	d.defs.WriteString("/></clipPath>\n")
	d.openElem("g", fmt.Sprintf(` clip-path="url(#%s)"`, id), false)
	if minX, minY, maxX, maxY, ok := path.Bounds(); ok {
		d.clipRect = intersectClip(d.clipRect, &clipBounds{minX, minY, maxX, maxY})
	}
}

// ensureEmptyClip defines the shared empty clip path once.
//
// The flag is its own field rather than a key in d.warned: that map exists to
// deduplicate LOG messages, and sharing the namespace would mean a future
// warnOnce call with a colliding key silently suppresses this DEFINITION,
// leaving a dangling url(#empty-clip) that viewers treat as no clip at all —
// turning content that must be hidden into content that is shown.
func (d *Device) ensureEmptyClip() {
	if d.emptyClipDefined {
		return
	}
	d.emptyClipDefined = true
	d.defs.WriteString(`<clipPath id="empty-clip"><path d=""/></clipPath>` + "\n")
}

// openElem writes an opening tag and records it, along with the clip state to
// restore when it closes.
func (d *Device) openElem(tag, attrs string, fromSave bool) {
	fmt.Fprintf(d.buf, "<%s%s>\n", tag, attrs)
	d.elems = append(d.elems, elem{tag: tag, fromSave: fromSave, clip: d.clipRect})
}

// popElem closes the top element and restores the clip it captured, reporting
// whether it was a Save boundary.
func (d *Device) popElem() (fromSave bool) {
	e := d.elems[len(d.elems)-1]
	d.elems = d.elems[:len(d.elems)-1]
	if e.tag != "" {
		fmt.Fprintf(d.buf, "</%s>\n", e.tag)
	}
	d.clipRect = e.clip
	return e.fromSave
}

// Save marks a restore point in the element nesting.
//
// Nothing is emitted yet: a Save that is never followed by a clip needs no
// element at all, and emitting an empty <g> for every q in a content stream
// would bloat the output enormously. The zero-tag stack entry records where
// Restore must unwind to.
func (d *Device) Save() {
	d.elems = append(d.elems, elem{fromSave: true, clip: d.clipRect})
}

// Restore closes every element opened since the matching Save.
//
// Per the Device contract it clamps rather than panicking on an unbalanced
// call, and it must not pop past an enclosing BeginGroup — so the unwind stops
// at the group's base.
func (d *Device) Restore() {
	base := 0
	if n := len(d.groups); n > 0 {
		base = d.groups[n-1].elemBase
	}
	for len(d.elems) > base {
		if d.popElem() {
			return
		}
	}
}

// BeginGroup starts an isolated group by redirecting output to a scratch
// buffer, which EndGroup wraps in a <g> carrying the group's compositing.
func (d *Device) BeginGroup() {
	f := &groupFrame{prev: d.buf, scratch: &strings.Builder{}, elemBase: len(d.elems)}
	d.groups = append(d.groups, f)
	d.buf = f.scratch
}

// EndGroup closes the innermost group and composites it.
func (d *Device) EndGroup(alpha float64, blendMode string, clipMask, softMask render.GroupMask) {
	if len(d.groups) == 0 {
		return // unbalanced; mirror Restore's forgiving behavior
	}
	// Close anything the group's children left open, so the scratch content is
	// well-formed before it is wrapped.
	//
	// The frame must stay on the stack until the body is read: popElem writes
	// its closing tags through d.buf, which is this frame's scratch builder,
	// so reading the body before the loop runs would drop every closing tag
	// it wrote — malformed XML for the routine case of a clip left open inside
	// a group, with no error to signal it.
	f := d.groups[len(d.groups)-1]
	for len(d.elems) > f.elemBase {
		d.popElem()
	}
	body := f.scratch.String()
	prev := f.prev
	d.groups = d.groups[:len(d.groups)-1]
	d.buf = prev
	if body == "" {
		return
	}

	var attrs strings.Builder
	writeOpacity(&attrs, "opacity", alpha)
	if css, ok := blendAttr(blendMode); ok {
		fmt.Fprintf(&attrs, " style=\"mix-blend-mode:%s\"", css)
	} else if blendMode != "" && blendMode != "Normal" && blendMode != "Compatible" {
		d.warnOnce("blend:"+blendMode, "svgwrite: blend mode %q has no CSS equivalent; painting source-over", blendMode)
	}
	// clipMask and softMask arrive separately (see render.Device.EndGroup) and
	// both apply when both are set. They are emitted as two nested <mask>
	// references rather than multiplied together here, so each keeps its own
	// semantics and neither is resampled against the other's bounds.
	if id, ok := d.maskDef(clipMask); ok {
		fmt.Fprintf(&attrs, " mask=%q", "url(#"+id+")")
	}
	if id, ok := d.maskDef(softMask); ok {
		// A second mask attribute cannot coexist on one element, so the soft
		// mask goes on a wrapping <g>.
		d.buf.WriteString(`<g mask="url(#` + id + `)">` + "\n")
		defer d.buf.WriteString("</g>\n")
	}
	fmt.Fprintf(d.buf, "<g%s>\n%s</g>\n", attrs.String(), body)
}

// maskDef emits m as a <mask> definition and returns its id.
//
// A nil mask means "no restriction" per the GroupMask contract and yields
// ok=false so no attribute is written.
func (d *Device) maskDef(m render.GroupMask) (string, bool) {
	img := alphaToImage(m)
	if img == nil {
		return "", false
	}
	href, ok := pngDataURI(img)
	if !ok {
		d.warnOnce("mask", "svgwrite: mask could not be encoded; group painted unmasked")
		return "", false
	}
	b := m.Bounds()
	id := d.id("mask")
	// maskUnits="userSpaceOnUse" with the mask's own bounds keeps the pixels
	// on the device grid every other Device method uses. Area outside the
	// mask's bounds is uncovered, which SVG treats as fully masked out —
	// matching the GroupMask contract exactly.
	//
	// mask-type="alpha" is required, not cosmetic: a GroupMask is already
	// final coverage, and the default luminance type would convert it a second
	// time (in linearRGB per SVG 1.1), darkening the mask badly. See
	// alphaToImage, which writes the coverage into the alpha channel to match.
	fmt.Fprintf(&d.defs,
		`<mask id="%s" mask-type="alpha" maskUnits="userSpaceOnUse" x="%s" y="%s" width="%s" height="%s">`,
		id, formatCoord(float64(b.Min.X)), formatCoord(float64(b.Min.Y)),
		formatCoord(float64(b.Dx())), formatCoord(float64(b.Dy())))
	fmt.Fprintf(&d.defs,
		`<image x="%s" y="%s" width="%s" height="%s" preserveAspectRatio="none" href="%s"/></mask>`+"\n",
		formatCoord(float64(b.Min.X)), formatCoord(float64(b.Min.Y)),
		formatCoord(float64(b.Dx())), formatCoord(float64(b.Dy())), href)
	return id, true
}

// toRGBA converts a FillColor to the color.RGBA the paint types carry.
// FillColor exists separately only so glyph fills need not carry a fill rule;
// the channels are identical.
func toRGBA(c render.FillColor) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}
