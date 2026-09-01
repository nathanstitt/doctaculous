package raster

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// Device renders into an *image.RGBA, implementing render.Device. Paths arrive
// already in device space (origin top-left, y down). Nonzero fills use
// golang.org/x/image/vector; even-odd fills use a built-in scanline rasterizer
// (evenOddCoverage); strokes and glyphs are flattened to filled paths. Clipping
// is an alpha-mask intersection tracked on a small state stack.
//
// A Device is not safe for concurrent use; render one page per Device. Separate
// pages render on separate Devices, which is how the page-parallel path stays
// lock-free.
type Device struct {
	img *image.RGBA
	// page is the whole page's extent in device pixels. It equals img.Bounds()
	// for an ordinary Device and is LARGER for a region Device (see NewRegion),
	// where img covers only the sub-rect being painted.
	//
	// Every GEOMETRY decision reads this rather than img.Bounds(): content is
	// positioned in page coordinates, and an effect painted inside the region can
	// legitimately be shaped by pixels outside it. img.Bounds() governs only
	// where pixels may be WRITTEN.
	page image.Rectangle
	clip []*image.Alpha // clip stack; top is the active mask (nil = unclipped)
	logf func(string, ...any)

	// groups is the offscreen-group stack. Each entry records the surface
	// BeginGroup swapped out (to restore on EndGroup) and the clip-stack depth
	// at the time of the call, so a Save/Restore pair inside the group can
	// never pop past the state the group started with (see Restore).
	groups []groupState
}

// groupState is one entry on the group stack: the surface that was active
// before BeginGroup, the clip that was active at that point (applied once at
// composite time in EndGroup, not while painting into the scratch — see
// BeginGroup), and the clip-stack depth to guard on Restore.
type groupState struct {
	img       *image.RGBA
	outerClip *image.Alpha
	clipBase  int
}

// New returns a Device drawing onto img. The caller owns img and reads the
// result after interpretation completes.
func New(img *image.RGBA) *Device {
	return &Device{img: img, page: img.Bounds(), logf: func(string, ...any) {}}
}

// NewRegion returns a Device that paints only part of a page: it may write only
// within region.Bounds(), but reports and reasons about the geometry of the
// whole page, whose extent in device pixels is page.
//
// region must be a sub-image (image.RGBA.SubImage) of a page-sized surface, so
// its Bounds() carry PAGE-ABSOLUTE coordinates rather than starting at the
// origin. Painting a page's full item list through such a Device produces
// exactly the pixels a full-page render would put in that rect — the property
// the whole thing exists for, since the result is composited over a cached
// full-page frame and any difference shows as a seam.
//
// Passing the page extent separately is what makes that true, and is the reason
// this constructor exists rather than callers using New on a sub-image. New
// would report the REGION's size from Size(), and page-space geometry would
// then be measured against a surface claiming to be a few hundred pixels tall:
// every offscreen surface derived from Size — a box-shadow blur, a CSS filter —
// would be clamped to the region, silently losing the part of the effect
// contributed from outside it and landing what remains in the wrong place. That
// is not a subtle error; a shadow measured this way was off by 56/255.
func NewRegion(region *image.RGBA, page image.Rectangle) *Device {
	return &Device{img: region, page: page, logf: func(string, ...any) {}}
}

// WriteBounds reports the region of the page this Device may write, in
// page-absolute device pixels: the whole page for an ordinary Device, the
// sub-rect for a region Device (NewRegion).
//
// It lets an expensive painter skip work whose pixels would be discarded — see
// pkg/internal/layout/paint's box-shadow path, where a region would otherwise
// build and blur every shadow on the page before throwing most of them away.
// It is deliberately NOT part of the render.Device interface: a backend that
// cannot restrict its writes simply does not implement it, and callers treat
// its absence as "everything is writable".
func (d *Device) WriteBounds() image.Rectangle { return d.img.Bounds() }

// SetLogf installs a debug logger that receives messages about approximated or
// unsupported features (e.g. even-odd fills). Safe to call before rendering.
func (d *Device) SetLogf(logf func(string, ...any)) {
	if logf != nil {
		d.logf = logf
	}
}

// Size reports the PAGE's pixel dimensions, which for a region Device
// (NewRegion) is not the size of the surface it may write to.
//
// Callers use this to size offscreen surfaces and to bound page-space geometry,
// and both must reason about the whole page: a region reporting its own size
// would clamp a box-shadow blur or a CSS filter to the region and lose the part
// of the effect contributed from outside it.
func (d *Device) Size() (w, h int) {
	return d.page.Dx(), d.page.Dy()
}

// Fill paints path's interior with paint.Color under the current clip.
func (d *Device) Fill(path *render.Path, paint render.FillPaint) {
	if path == nil || path.Empty() {
		return
	}
	mask := d.rasterizeMask(path, paint.Rule)
	if mask == nil {
		return
	}
	d.compositeBlend(mask, paint.Color, paint.BlendMode)
}

// Stroke renders path's outline honoring caps, joins and dashes. Its
// implementation lives in stroke.go (rasterx-backed).

// FillGlyph fills a glyph outline (device space) with a solid color.
func (d *Device) FillGlyph(outline *render.Path, c render.FillColor, blendMode string) {
	if outline == nil || outline.Empty() {
		return
	}
	mask := d.rasterizeMask(outline, render.NonZero)
	if mask == nil {
		return
	}
	d.compositeBlend(mask, color.RGBA(c), blendMode)
}

// DrawGlyph renders g by filling g.Face's outline for g.GID, transformed into
// device space by g.Transform. Runes and Advance are ignored — they matter only to
// text-emitting backends. This produces pixels identical to the equivalent
// FillGlyph call, so existing goldens are unchanged.
func (d *Device) DrawGlyph(g render.GlyphRef) {
	if g.Face == nil {
		return
	}
	outline := g.Face.Outline(g.GID)
	if outline == nil || outline.Empty() {
		return
	}
	d.FillGlyph(render.TransformPath(outline, g.Transform), g.Color, g.Blend)
}

// DrawImage maps img's unit square through ctm into device space using inverse
// sampling (nearest neighbor), respecting the current clip and blend mode.
func (d *Device) DrawImage(img image.Image, ctm render.Matrix, alpha float64, blendMode string) {
	if img == nil {
		return
	}
	if alpha <= 0 {
		return // fully transparent: nothing to draw
	}
	if alpha > 1 {
		alpha = 1
	}
	sep, isSep, nonsep, isNonsep := lookupBlend(blendMode)
	inv, ok := invert(ctm)
	if !ok {
		return
	}
	// Device-space bounding box of the unit square's four corners.
	minX, minY, maxX, maxY := unitQuadBounds(ctm)
	b := d.img.Bounds()
	x0 := clampInt(int(math.Floor(minX)), b.Min.X, b.Max.X)
	y0 := clampInt(int(math.Floor(minY)), b.Min.Y, b.Max.Y)
	x1 := clampInt(int(math.Ceil(maxX)), b.Min.X, b.Max.X)
	y1 := clampInt(int(math.Ceil(maxY)), b.Min.Y, b.Max.Y)

	sb := img.Bounds()
	clip := d.activeClip()
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			cov := uint32(255)
			if clip != nil {
				if cov = uint32(clip.AlphaAt(x, y).A); cov == 0 {
					continue
				}
			}
			// Map pixel center to unit space, then to source pixels. PDF image
			// space has y up with the image's top row at v=1, so flip v.
			u, v := inv.Apply(float64(x)+0.5, float64(y)+0.5)
			if u < 0 || u >= 1 || v < 0 || v >= 1 {
				continue
			}
			sx := sb.Min.X + int(u*float64(sb.Dx()))
			sy := sb.Min.Y + int((1-v)*float64(sb.Dy()))
			// Guard against float rounding landing on the exclusive max edge.
			sx = clampInt(sx, sb.Min.X, sb.Max.X-1)
			sy = clampInt(sy, sb.Min.Y, sb.Max.Y-1)

			// Straight-alpha source color, folding in the source pixel's own alpha,
			// the constant /ca alpha, and the clip coverage.
			sc := straightRGBA(img.At(sx, sy))
			a := uint32(sc.A) * uint32(alpha*255+0.5) / 255 // source α × constant α
			a = a * cov / 255                               // × clip coverage
			if a == 0 {
				continue
			}
			src := color.RGBA{R: sc.R, G: sc.G, B: sc.B, A: sc.A}
			if isSep || isNonsep {
				dst := d.img.RGBAAt(x, y)
				src = blendSource(dst, src, sep, nonsep, isSep)
			}
			over(d.img, x, y, src, uint8(a))
		}
	}
}

// straightRGBA converts any color to straight-alpha 8-bit RGBA. Go's color
// model returns premultiplied 16-bit values from RGBA(); we un-premultiply so
// over() (which expects straight-alpha) blends correctly.
func straightRGBA(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA() // premultiplied, 16-bit
	if a == 0 {
		return color.RGBA{}
	}
	return color.RGBA{
		R: uint8(r * 0xffff / a >> 8),
		G: uint8(g * 0xffff / a >> 8),
		B: uint8(b * 0xffff / a >> 8),
		A: uint8(a >> 8),
	}
}

// PushClip intersects the current clip with path. Clip masks are sub-rectangle
// sized (see rasterizeMask); a point outside a mask's bounds has zero coverage,
// which correctly means "clipped out".
func (d *Device) PushClip(path *render.Path, rule render.FillRule) {
	if path == nil || path.Empty() {
		return
	}
	next := d.rasterizeMask(path, rule)
	if next == nil {
		// Clip to nothing: an empty mask covering the (empty) intersection.
		next = image.NewAlpha(image.Rectangle{})
	}
	if cur := d.activeClip(); cur != nil {
		next = intersectClips(cur, next)
	}
	if len(d.clip) == 0 {
		d.clip = append(d.clip, next)
	} else {
		d.clip[len(d.clip)-1] = next
	}
}

// Save pushes the current clip onto the stack.
func (d *Device) Save() {
	d.clip = append(d.clip, d.activeClip())
}

// Restore pops the clip stack. It never pops past the clip-stack depth an
// enclosing, still-open BeginGroup left behind (clipBase + 1, accounting for
// the sentinel entry BeginGroup itself pushes) — an interpreter bug (an extra
// Q) inside a group must not corrupt the clip state BeginGroup will restore
// on EndGroup, so this guard is what keeps that promise.
func (d *Device) Restore() {
	base := 0
	if n := len(d.groups); n > 0 {
		base = d.groups[n-1].clipBase + 1
	}
	if len(d.clip) > base {
		d.clip = d.clip[:len(d.clip)-1]
	}
}

// BeginGroup starts an isolated offscreen group. See render.Device for the
// full contract. The scratch surface starts fully transparent black (the
// isolated-group backdrop SVG requires) and is the same size as the current
// surface so every paint method's existing bounds logic keeps working
// unchanged.
//
// The clip that was active when the group opened (e.g. from a clip-path
// applied to the <g> itself) is captured as the group's "outer clip" and then
// suspended (pushed as unclipped) for the duration of the group: children
// paint into the scratch WITHOUT that clip restricting them, and it is
// applied exactly once, at composite time, in EndGroup. Applying it while
// painting AND again at composite would double-count its antialiased edges
// (an edge pixel dimmed once while painting, then dimmed again when the
// already-dimmed scratch pixel is clipped a second time), darkening the clip
// boundary — the seam this primitive exists to avoid.
//
// A clip pushed by a child WITHIN the group (its own PushClip calls) is
// unaffected: those still land on the (suspended-at-the-base) clip stack
// exactly as before and restrict children normally.
func (d *Device) BeginGroup() {
	b := d.img.Bounds()
	outer := d.activeClip()
	d.groups = append(d.groups, groupState{img: d.img, outerClip: outer, clipBase: len(d.clip)})
	// Suspend the outer clip for the scratch's lifetime: push "unclipped" so
	// children painting into the scratch aren't restricted by it twice (see
	// above). Save/Restore inside the group operate on top of this entry and
	// are guarded from popping past it by Restore's clipBase check.
	d.clip = append(d.clip, nil)
	d.img = image.NewRGBA(b) // transparent black: zero value is {0,0,0,0}
}

// EndGroup closes the most recently opened BeginGroup and composites the
// scratch surface onto the restored surface. See render.Device for the full
// contract. An unbalanced call (no matching BeginGroup) is a no-op.
//
// The group's own clip (whatever was active when BeginGroup ran) is applied
// here, once, at composite time — not while painting into the scratch. The
// scratch's per-pixel coverage already reflects each child shape's own
// antialiasing; multiplying by the clip's coverage a second time while
// painting children would double-count the clip's own antialiased edges
// (each child's edge pixels would be dimmed by the clip, then dimmed again
// when the already-dimmed scratch pixel is clipped a second time here),
// darkening the clip boundary. Applying it exactly once, on the flattened
// result, is correct regardless of how many children touched that pixel.
//
// clipMask and softMask are independently optional (see render.Device's doc
// comment on why they arrive separately rather than pre-combined): this
// backend has no reason to treat them differently from each other or from
// the group's own outer clip, since all three are just per-pixel alpha
// multipliers by the time they reach here — it multiplies all that apply.
func (d *Device) EndGroup(alpha float64, blendMode string, clipMask, softMask render.GroupMask) {
	n := len(d.groups)
	if n == 0 {
		return // unbalanced EndGroup: degrade to a no-op, never panic
	}
	scratch := d.img
	g := d.groups[n-1]
	d.groups = d.groups[:n-1]
	d.img = g.img
	// Pop the "unclipped" sentinel BeginGroup pushed, restoring the clip stack
	// to exactly what it was before BeginGroup (clipBase entries), regardless
	// of how many balanced Save/Restore pairs ran inside the group.
	d.clip = d.clip[:g.clipBase]

	if alpha <= 0 {
		return // fully transparent: nothing to composite
	}
	if alpha > 1 {
		alpha = 1
	}
	// The clip in effect when BeginGroup ran is the group's own clip; apply it
	// once here (composite time), alongside the caller-supplied masks.
	clip := g.outerClip

	sep, isSep, nonsep, isNonsep := lookupBlend(blendMode)
	b := scratch.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			src := scratch.RGBAAt(x, y)
			if src.A == 0 {
				continue
			}
			a := uint32(src.A) * uint32(alpha*255+0.5) / 255
			if clip != nil {
				a = a * uint32(clip.AlphaAt(x, y).A) / 255
			}
			if clipMask != nil {
				a = a * uint32(clipMask.AlphaAt(x, y).A) / 255
			}
			if softMask != nil {
				a = a * uint32(softMask.AlphaAt(x, y).A) / 255
			}
			if a == 0 {
				continue
			}
			// scratch pixels are premultiplied (image.RGBA convention); over()
			// wants straight-alpha, so un-premultiply before blending/compositing.
			straight := unpremultiplyRGBA(src)
			out := straight
			if isSep || isNonsep {
				dst := d.img.RGBAAt(x, y)
				out = blendSource(dst, straight, sep, nonsep, isSep)
			}
			over(d.img, x, y, out, uint8(a))
		}
	}
}

// BuildClipMask rasterizes the union of paths into a single coverage mask:
// each path is rasterized separately under ITS OWN rule (via the same
// rasterizeMask every Fill/PushClip call uses), and the masks are combined
// with per-pixel max(), which is exactly set union for coverage values. This
// is what lets a <clipPath> with two non-overlapping children clip to BOTH
// (repeated PushClip would INTERSECT them to nothing — see render.Device's
// doc comment) and what keeps a mixed clip-rule union (one child nonzero,
// another evenodd) and an overlapping-opposite-winding union correct: each
// child is rasterized under its own rule before any combining happens, so
// there is never a single shared rule applied across geometry that
// disagrees about winding.
//
// An empty paths slice, or a slice whose every path is empty/off-canvas,
// returns a non-nil zero-sized mask (bounds Rectangle{}) rather than nil:
// AlphaAt is zero everywhere outside a mask's own bounds, so this correctly
// reads as "covers nothing" to EndGroup, distinct from a nil GroupMask
// ("no restriction") — see GroupMask's doc comment. This is the mechanism
// behind SVG's "an empty clipPath clips its target to nothing" rule.
func (d *Device) BuildClipMask(paths []render.MaskPath) render.GroupMask {
	union := image.NewAlpha(image.Rectangle{})
	for _, mp := range paths {
		if mp.Path == nil || mp.Path.Empty() {
			continue
		}
		child := d.rasterizeMask(mp.Path, mp.Rule)
		if child == nil {
			continue
		}
		union = unionAlphaMax(union, child)
	}
	return union
}

// BuildLuminanceMask renders paint's content into a fresh, fully-transparent
// scratch *image.RGBA the same size as size, then converts the result into a
// GroupMask: see render.Device's doc comment for the luminance-vs-alpha
// formula (alphaOnly selects which).
//
// paint runs against a NEW *Device wrapping the scratch, not d itself: d's
// own clip/group stacks must stay untouched by whatever pkg/svg/draw does
// while painting mask content (a mask's content establishes its own,
// independent paint state — SVG defines no inheritance of the masked
// element's clip into the mask), and giving paint a throwaway Device is the
// simplest way to guarantee that. The scratch device shares d's logf so a
// degradation logged while painting mask content is still surfaced.
func (d *Device) BuildLuminanceMask(size image.Point, alphaOnly bool, paint func(dev render.Device)) render.GroupMask {
	if paint == nil || size.X <= 0 || size.Y <= 0 {
		return image.NewAlpha(image.Rectangle{})
	}
	scratch := New(image.NewRGBA(image.Rectangle{Max: size}))
	scratch.logf = d.logf
	paint(scratch)

	b := scratch.img.Bounds()
	mask := image.NewAlpha(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			mask.SetAlpha(x, y, color.Alpha{A: pixelMaskValue(scratch.img.RGBAAt(x, y), alphaOnly)})
		}
	}
	return mask
}

// RenderOffscreen renders paint's content into a fresh, fully-transparent
// scratch *image.RGBA the same size as size and returns it directly: see
// render.Device's doc comment for why an SVG <filter> needs the pixels
// themselves rather than the coverage mask BuildLuminanceMask derives.
//
// paint runs against a NEW *Device wrapping the scratch, not d itself, for
// exactly the reason BuildLuminanceMask does the same: d's own clip/group
// stacks must stay untouched by whatever the caller paints, and a throwaway
// Device is the simplest way to guarantee that. The scratch device shares
// d's logf so a degradation logged while painting filter content is still
// surfaced.
//
// The returned image is freshly allocated and never aliased by this Device
// afterward, so the caller may transform it in place — which every filter
// primitive does.
func (d *Device) RenderOffscreen(size image.Point, paint func(dev render.Device)) *image.RGBA {
	if paint == nil || size.X <= 0 || size.Y <= 0 {
		return nil
	}
	scratch := New(image.NewRGBA(image.Rectangle{Max: size}))
	scratch.logf = d.logf
	paint(scratch)
	return scratch.img
}

// pixelMaskValue converts one premultiplied *image.RGBA pixel into a mask
// coverage value: the pixel's own alpha directly for mask-type=alpha, or its
// sRGB luminance (Rec. 709 coefficients, on sRGB — NOT linearRGB, see
// render.Device.BuildLuminanceMask's doc comment) times its own alpha for
// the default mask-type=luminance. c is premultiplied (the raw *image.RGBA
// convention this backend's scratch surfaces use), so un-premultiplying
// first is required before the sRGB coefficients mean anything — computing
// luminance directly on premultiplied channels would silently double-count
// alpha (once from premultiplication, once from the explicit "times its own
// alpha" step SVG's masking spec calls for).
func pixelMaskValue(c color.RGBA, alphaOnly bool) uint8 {
	if c.A == 0 {
		return 0
	}
	if alphaOnly {
		return c.A
	}
	straight := unpremultiplyRGBA(c)
	lum := 0.2126*float64(straight.R) + 0.7152*float64(straight.G) + 0.0722*float64(straight.B)
	v := lum * float64(c.A) / 255
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(math.Round(v))
}

// unionAlphaMax returns a new alpha mask covering the union of a and b's
// bounding rectangles, with each pixel's coverage the MAX of a's and b's
// (each treated as zero outside its own bounds). max() is the correct
// combine for "does at least one of these shapes cover this pixel" — a sum
// or product would either overflow/saturate incorrectly or produce an
// intersection instead of a union.
func unionAlphaMax(a, b *image.Alpha) *image.Alpha {
	r := a.Bounds().Union(b.Bounds())
	if r.Empty() {
		return image.NewAlpha(image.Rectangle{})
	}
	out := image.NewAlpha(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			av := a.AlphaAt(x, y).A
			bv := b.AlphaAt(x, y).A
			if bv > av {
				av = bv
			}
			if av != 0 {
				out.SetAlpha(x, y, color.Alpha{A: av})
			}
		}
	}
	return out
}

// unpremultiplyRGBA converts an *image.RGBA pixel (premultiplied alpha, the
// Go image/draw convention) to straight alpha, matching straightRGBA's
// contract for the general color.Color case but avoiding its 16-bit
// round-trip through Color() for the common already-8-bit case.
func unpremultiplyRGBA(c color.RGBA) color.RGBA {
	if c.A == 0 {
		return color.RGBA{}
	}
	if c.A == 255 {
		return c
	}
	a := uint32(c.A)
	return color.RGBA{
		R: uint8(uint32(c.R) * 255 / a),
		G: uint8(uint32(c.G) * 255 / a),
		B: uint8(uint32(c.B) * 255 / a),
		A: c.A,
	}
}

func (d *Device) activeClip() *image.Alpha {
	if len(d.clip) == 0 {
		return nil
	}
	return d.clip[len(d.clip)-1]
}

// rasterizeMask renders path into an alpha coverage mask. The mask is sized to
// the path's device-space bounding box clipped to the image bounds — not the
// whole image — so a page with thousands of small glyphs costs O(Σ glyph areas)
// rather than O(glyphs × image area). The returned mask's Bounds() reflect that
// sub-rectangle; callers (composite, intersect) iterate Bounds() and so stay
// bounded automatically. Returns nil if the path lies entirely off-canvas.
// evenOddSupersample is the number of subscanlines per pixel row the even-odd
// rasterizer averages for vertical anti-aliasing. 4 matches the visual quality of
// the vector backend's nonzero coverage closely enough for the golden tolerance.
const evenOddSupersample = 4

func (d *Device) rasterizeMask(path *render.Path, rule render.FillRule) *image.Alpha {
	pb := pathDeviceBounds(path)
	// Nothing this path covers is writable: skip before rasterizing. On a region
	// Device this is what keeps replaying the page's whole item list affordable —
	// an item belonging elsewhere on the page costs one rectangle test instead of
	// a rasterization whose coverage would be discarded.
	if !pb.Overlaps(d.img.Bounds()) {
		return nil
	}
	// Clipped to the PAGE, not to this Device's surface. On a region Device the
	// two differ, and using the surface would change the mask's GEOMETRY: both
	// rasterizers derive their accumulation buffer and subscanline sampling from
	// the mask rectangle, so a shape truncated at the region edge yields slightly
	// different coverage along that edge than the same shape rasterized whole.
	// That is a 1/255 seam exactly at the boundary the result gets composited
	// across — the one place it would be visible.
	bb := pb.Intersect(d.page)
	if bb.Empty() {
		return nil
	}
	// golang.org/x/image/vector only implements nonzero winding, so even-odd fills
	// go through our own scanline rasterizer (see evenOddCoverage). Nonzero stays
	// on the fast vector path.
	if rule == render.EvenOdd {
		return evenOddCoverage(flattenToPolygons(path), bb, evenOddSupersample)
	}
	r := vector.NewRasterizer(bb.Dx(), bb.Dy())
	replay(r, path, float32(bb.Min.X), float32(bb.Min.Y))
	mask := image.NewAlpha(bb)
	// Draw into the mask. The rasterizer's coordinate space starts at (0,0); the
	// mask's Bounds() start at bb.Min, so draw with that as the destination rect.
	r.Draw(mask, bb, image.Opaque, image.Point{})
	return mask
}

// composite blends src color through the coverage mask (and active clip) onto
// the image using source-over alpha.
func (d *Device) composite(mask *image.Alpha, c color.RGBA) {
	// Intersected with the surface: the mask is rasterized against the whole page
	// (see rasterizeMask), so on a region Device a shape crossing the boundary
	// has most of its mask in pixels this Device cannot write. Walking those
	// would cost the full shape per region and undo the saving.
	b := mask.Bounds().Intersect(d.img.Bounds())
	clip := d.activeClip()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cov := mask.AlphaAt(x, y).A
			if cov == 0 {
				continue
			}
			if clip != nil {
				cov = mulU8(cov, clip.AlphaAt(x, y).A)
				if cov == 0 {
					continue
				}
			}
			a := mulU8(c.A, cov)
			if a == 0 {
				continue
			}
			over(d.img, x, y, c, a)
		}
	}
}

// over blends a straight (non-premultiplied) source color c, at coverage-scaled
// alpha a, onto a premultiplied destination pixel (Go's *image.RGBA convention)
// using source-over. Callers must pass straight-alpha colors.
func over(img *image.RGBA, x, y int, c color.RGBA, a uint8) {
	dst := img.RGBAAt(x, y)
	ia := 255 - uint32(a)
	out := color.RGBA{
		R: uint8((uint32(c.R)*uint32(a) + uint32(dst.R)*ia) / 255),
		G: uint8((uint32(c.G)*uint32(a) + uint32(dst.G)*ia) / 255),
		B: uint8((uint32(c.B)*uint32(a) + uint32(dst.B)*ia) / 255),
		A: uint8(uint32(a) + uint32(dst.A)*ia/255),
	}
	img.SetRGBA(x, y, out)
}

// Fill, draw helpers ---------------------------------------------------------

// fillBackground paints the whole image a solid color (used for opaque page
// backgrounds before interpretation).
func fillBackground(img *image.RGBA, c color.Color) {
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
}

func mulU8(a, b uint8) uint8 { return uint8(uint32(a) * uint32(b) / 255) }

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
