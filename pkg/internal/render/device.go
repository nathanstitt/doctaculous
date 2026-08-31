package render

import (
	"image"
	"image/color"
)

// Device is the backend-agnostic drawing target the content interpreter drives.
// All geometry passed to a Device is already in device space (the interpreter
// applies the CTM before calling). Implementations include the raster bitmap
// backend; future backends (e.g. SVG) implement the same interface.
//
// Methods must tolerate degenerate input (empty paths, zero-area images) without
// panicking.
type Device interface {
	// Size reports the device's pixel dimensions.
	Size() (w, h int)

	// Fill paints the interior of path using paint's color and fill rule,
	// intersected with the current clip.
	Fill(path *Path, paint FillPaint)

	// Stroke paints path's outline using paint, intersected with the current clip.
	Stroke(path *Path, paint StrokePaint)

	// DrawImage draws img mapped by ctm. The image's unit square [0,1]×[0,1] is
	// mapped through ctm into device space (PDF image space convention). alpha in
	// [0,1] is the constant fill opacity (ExtGState /ca); 1 is fully opaque.
	// blendMode is the /BM blend mode name ("" or "Normal" = source-over).
	DrawImage(img image.Image, ctm Matrix, alpha float64, blendMode string)

	// FillGlyph fills a single glyph outline (already in device space) with color.
	// The outline uses the nonzero winding rule. blendMode is the /BM blend mode.
	FillGlyph(outline *Path, color FillColor, blendMode string)

	// DrawGlyph paints one shaped glyph placed in device space via g.Transform (em
	// space, Y up, 1 em = 1 unit -> device space). Backends that only rasterize
	// render g.Face's outline for g.GID and may ignore g.Runes and g.Advance;
	// backends that emit text (PDF, text extraction) use g.Runes for a ToUnicode
	// mapping and g.Advance for spacing. g.Blend is the /BM blend mode ("" =
	// Normal), matching FillGlyph.
	DrawGlyph(g GlyphRef)

	// FillShading fills the active clip region by evaluating shader at each device
	// pixel, honoring the active clip and the named blend mode. The device maps each
	// pixel center from device space into shading (user) space via the inverse of
	// ctm, then calls shader.ColorAt; a pixel where ColorAt reports paint=false is
	// left untouched. With no active clip it fills the whole device (the `sh`
	// operator is normally clipped first). blendMode is the /BM blend mode name
	// ("" or "Normal" = source-over).
	FillShading(shader Shader, ctm Matrix, blendMode string)

	// PushClip intersects the current clip with path using rule.
	PushClip(path *Path, rule FillRule)

	// BeginGroup starts an isolated offscreen group: subsequent paint calls
	// (until the matching EndGroup) composite against a fully transparent
	// black backdrop of their own, not against whatever was already painted.
	// This is what lets a group's overall opacity, clip, or mask apply once to
	// the group's flattened result instead of separately to each paint call
	// inside it — the latter double-darkens every overlap between children,
	// which is the artifact groups exist to prevent.
	//
	// BeginGroup/EndGroup calls must nest (like parentheses) and must nest
	// consistently with Save/Restore: a Restore may not pop past the Save
	// depth captured by an enclosing, still-open BeginGroup. Concretely, a
	// backend must track the Save/Restore stack depth at each BeginGroup and
	// refuse (clamp, not panic) any Restore that would cross it before the
	// matching EndGroup runs.
	//
	// A backend that cannot composite offscreen groups (e.g. a not-yet-built
	// vector writer path) must degrade gracefully: treat BeginGroup/EndGroup
	// as transparent pass-through so children still paint directly, just
	// without the isolation, opacity, or mask applied. It must never panic
	// and never drop the children's content.
	BeginGroup()

	// EndGroup closes the most recently opened BeginGroup, compositing the
	// group's offscreen result onto the surface that was active before the
	// matching BeginGroup.
	//
	// alpha in [0,1] scales the group's coverage uniformly (SVG/CSS group
	// opacity; PDF ExtGState /ca applied to the group as a whole). blendMode
	// is the /BM blend mode name ("" or "Normal" = source-over) used when
	// compositing the group onto its backdrop.
	//
	// clipMask and softMask are two DISTINCT, independently-optional per-pixel
	// restrictions, each nil meaning "no restriction of this kind":
	//
	//   - clipMask carries a flattened clipPath union (from BuildClipMask):
	//     boolean coverage — in the clipped region or not, antialiasing aside.
	//   - softMask carries a rendered <mask> luminance/alpha result (from
	//     BuildLuminanceMask): deliberately fractional coverage.
	//
	// Both apply when both are non-nil (their product, matching SVG's
	// clip-then-mask compositing order), but they are passed SEPARATELY
	// rather than pre-combined into one GroupMask by the caller: PDF's native
	// model expresses them as two entirely different constructs (a `W n`
	// clip vs. an ExtGState `/SMask`), so a backend that can represent one or
	// both natively (pdfwrite) needs to see them apart to do so. Pre-combining
	// them into a single opaque GroupMask value, as an earlier revision of
	// this interface did, silently breaks any backend that recognizes a mask
	// by identity rather than by resampling its pixels (pdfwrite's
	// luminosity-soft-mask fast path — see softmask.go's takePendingSoftMask)
	// the moment the two are combined into a new value neither backend
	// allocated. A backend that has no reason to distinguish the two (the
	// raster backend, which resamples both as plain per-pixel alpha) may
	// simply multiply their coverage together, exactly as it would have
	// combined one pre-merged mask.
	//
	// An EndGroup with no matching BeginGroup (unbalanced) must be a no-op,
	// mirroring Restore's forgiving behavior on an empty stack — never panic.
	EndGroup(alpha float64, blendMode string, clipMask, softMask GroupMask)

	// Save and Restore manage the clip/state stack so the interpreter's q/Q
	// operators can be mirrored by the backend where clip state lives.
	Save()
	Restore()

	// BuildClipMask rasterizes the UNION of paths (each already in device
	// space, with its own fill rule) into a single GroupMask suitable for
	// EndGroup: pixel (x,y) has full coverage iff at least one path covers it
	// under its own rule. An empty paths slice (a <clipPath> with no valid
	// children) returns a non-nil, zero-sized/all-zero mask — "covers
	// nothing" — never nil, since nil would mean "no restriction" to
	// EndGroup (see GroupMask), which is the opposite of what an empty
	// clipPath must do (SVG: it clips its target to nothing).
	//
	// This exists because a <clipPath>'s children form a UNION, but
	// PushClip only INTERSECTS: pushing each child as its own clip would
	// render two non-overlapping children as empty. Flattening the union
	// into one mask requires rasterizing — a capability pkg/svg/draw (which
	// holds only a Device, by design; see that package's doc comment)
	// cannot perform itself without importing a concrete backend and
	// inverting the layer dependency. Putting the rasterization here keeps
	// every backend's own coverage rasterizer as the single source of truth
	// for "what pixels does this path with this rule cover" — the raster
	// backend reuses its existing rasterizeMask/max machinery; a backend
	// that cannot build an offscreen mask (a not-yet-built vector writer
	// path) may degrade gracefully, e.g. by unioning device-space bounding
	// boxes into a rectangular approximation, or returning a mask that
	// covers its overall bounds — but must never panic and never return nil.
	BuildClipMask(paths []MaskPath) GroupMask

	// BuildLuminanceMask renders paint's content into a scratch surface the
	// same size as the device, then converts the result into a GroupMask
	// suitable for EndGroup: this is how an SVG <mask> (rendered content,
	// not geometry) turns into a per-pixel alpha multiplier.
	//
	// paint receives a Device to draw into — NOT necessarily the receiver
	// itself, since the backend may hand back a device wrapping a fresh
	// scratch surface — so pkg/svg/draw can paint the mask's subtree through
	// the ordinary render.Device seam without importing a concrete backend
	// (the same layering rule BuildClipMask upholds for clip-path). The
	// scratch starts fully transparent black, matching BeginGroup's
	// isolated-group backdrop, so area the mask content never touches
	// evaluates to "fully masked out" per SVG.
	//
	// alphaOnly selects mask-type: false (the default, "luminance") converts
	// each painted pixel's sRGB color to luminance via the Rec. 709
	// coefficients (0.2126*R + 0.7152*G + 0.0722*B), then multiplies by that
	// pixel's own alpha, per this engine's decision to follow browsers/SVG2/
	// resvg's sRGB default rather than SVG 1.1's linearRGB (see the SVG
	// groups/clip/mask design doc, decision 2). true ("alpha") uses the
	// pixel's own alpha channel directly, ignoring color.
	//
	// A backend that cannot rasterize offscreen (a not-yet-built vector
	// writer path) may degrade gracefully — e.g. by returning nil and
	// logging, which pkg/svg/draw's caller must treat as "no masking" — but
	// must never panic and must never invoke paint with a nil Device.
	BuildLuminanceMask(size image.Point, alphaOnly bool, paint func(dev Device)) GroupMask

	// RenderOffscreen renders paint's content into an isolated, fully
	// transparent surface the same size as the device and hands back its
	// PIXELS, or nil when this backend cannot rasterize offscreen.
	//
	// This is the third member of the BuildClipMask/BuildLuminanceMask
	// family and exists for the same layering reason: an SVG <filter> must
	// read back the rasterized result of the element it filters in order to
	// transform it (blur, offset, recolor), and pkg/svg/draw holds only a
	// Device — it cannot allocate or rasterize a pixel buffer itself
	// without importing a concrete backend and inverting the layer
	// dependency (see that package's doc comment). Keeping the
	// rasterization here leaves each backend's own rasterizer the single
	// source of truth for what pixels a paint call covers.
	//
	// It differs from BuildLuminanceMask ONLY in its return type: a mask
	// collapses color to one coverage channel, which is exactly what a
	// <mask> needs and exactly what a filter must not do — a filter
	// operates on RGBA. paint receives a Device to draw into (NOT
	// necessarily the receiver), the surface starts fully transparent black
	// matching BeginGroup's isolated-group backdrop, and the returned image
	// is owned by the caller, which may modify it in place.
	//
	// The result is in the same device-space pixel grid every other Device
	// method uses, so a filter can composite it back with DrawImage under
	// an identity-scaled placement. Pixels are PREMULTIPLIED alpha, the
	// *image.RGBA convention (see image/color.RGBA), so a filter converting
	// to straight alpha must un-premultiply first.
	//
	// nil is the documented degradation for a backend with no raster
	// surface (pdfwrite), and callers MUST treat it as "filtering is
	// unavailable" — rendering the source unfiltered rather than dropping
	// it, since a nil result carries no content of its own. A backend must
	// never panic and never invoke paint with a nil Device.
	RenderOffscreen(size image.Point, paint func(dev Device)) *image.RGBA
}

// MaskPath is one child shape contributing to a clip-path union: a path
// already in device space, paired with the fill rule (clip-rule) that
// applies to IT ALONE. BuildClipMask combines many of these with max()
// coverage, which is what makes a mixed-clip-rule union (one child nonzero,
// another evenodd) and an overlapping-opposite-winding union both come out
// correct — the two cases naive path concatenation + a single rule breaks.
type MaskPath struct {
	Path *Path
	Rule FillRule
}

// GroupMask expresses a per-pixel alpha multiplier applied when EndGroup
// composites a group, restricted to some region of the device. It is the
// backend-neutral carrier for both a flattened clipPath union (opaque
// coverage: in the union or not) and a rendered luminance mask (graduated
// coverage: the mask content's luminance, times its own alpha) — EndGroup
// does not need to know which one it was given.
//
// A nil GroupMask means "no extra per-pixel restriction" (a plain
// opacity/blend group). AlphaAt(x, y).A of 255 means full coverage, 0 means
// fully masked out. Coordinates are in the same device-space pixel grid
// every other Device method uses. A pixel outside the mask's own bounds is
// treated as zero coverage (masked out entirely), matching how the raster
// backend already treats a clip mask's bounds as an implicit "outside =
// clipped away" — the SVG spec's "transparent outside the mask/clip region"
// requirement falls out of that for free rather than needing a separate
// default-coverage rule.
//
// *image.Alpha is the concrete type every backend can produce and consume
// without new dependencies: the raster backend already builds one for clip
// masks (see pkg/render/raster's rasterizeMask), and a vector backend can
// still accept it (e.g. to derive a luminosity soft mask's sample data)
// without importing pkg/render/raster. GroupMask is a defined pointer type,
// not a bare *image.Alpha, so the Device interface can document its
// composite semantics (this comment) independently of the pixel format, and
// so a future backend-neutral representation could replace the underlying
// type without changing every call site.
type GroupMask = *image.Alpha

// FillColor is the solid color used for glyph fills (kept separate from
// FillPaint so glyph rendering need not carry a fill rule).
type FillColor struct {
	R, G, B, A uint8
}

// GlyphRef is one shaped glyph handed to a Device, carrying enough identity for a
// rasterizing backend (Face+GID outline), a PDF writer (Face+GID embed/subset,
// Runes for ToUnicode), and a future text-extraction backend (Runes+Transform+
// Advance for positioned text). It is format-neutral: both the reflow paint layer
// and the PDF content interpreter can populate it.
type GlyphRef struct {
	Face      GlyphFace // font identity; see GlyphFace
	GID       uint16    // glyph id within Face
	Runes     []rune    // source characters this glyph represents (the cluster)
	Transform Matrix    // em space (Y up) -> device space; position, size, skew
	Advance   float64   // horizontal advance in device units
	Color     FillColor
	Blend     string // /BM blend mode ("" = Normal)
}

// GlyphFace is the minimal view of a font face a Device needs: outline geometry
// for a GID (rasterizer). The concrete type is *font.Face; this interface keeps
// pkg/render from importing pkg/font (which would invert the layer dependency). A
// PDF writer needs more than the outline (program bytes) and type-asserts the
// concrete *font.Face at its own boundary.
type GlyphFace interface {
	// Outline returns gid's outline in em units (Y up), or nil if empty/missing.
	Outline(gid uint16) *Path
}

// Shader evaluates a PDF shading: it maps a point in shading (user) space to the
// color painted there. ok=false means the point lies outside the shading and is
// not extended, so the backdrop is left untouched. The backend builds a Shader
// from a shading dictionary (keeping shading geometry and color math out of the
// content interpreter); FillShading drives it per device pixel.
type Shader interface {
	ColorAt(userX, userY float64) (c color.RGBA, ok bool)
}

// ShadingKind distinguishes the gradient geometries a ShadingDescriber can
// report.
type ShadingKind int

const (
	// ShadingAxial is a linear gradient between two endpoints.
	ShadingAxial ShadingKind = iota
	// ShadingRadial is a gradient between a focal circle and an outer circle.
	ShadingRadial
)

// SpreadMode is what happens to a gradient outside its defining [0,1]
// parameter range.
type SpreadMode int

const (
	// SpreadPad clamps to the nearest endpoint color beyond [0,1]. This is
	// the only mode PDF's /Extend can express natively.
	SpreadPad SpreadMode = iota
	// SpreadReflect mirrors the ramp back and forth beyond [0,1].
	SpreadReflect
	// SpreadRepeat wraps the ramp modulo 1, repeating it beyond [0,1].
	SpreadRepeat
)

// ShadingStop is one color stop on a gradient ramp, in the shape a
// ShadingDescriber reports.
type ShadingStop struct {
	Offset float64    // position in [0,1], non-decreasing across a stop list
	Color  color.RGBA // straight (non-premultiplied) RGBA
}

// ShadingDesc is a backend-recoverable description of a gradient's geometry,
// color ramp, and spread behavior — everything a vector backend (e.g. a PDF
// writer) needs to emit a native `/Shading` dictionary instead of sampling
// the Shader into a raster image.
type ShadingDesc struct {
	Kind ShadingKind
	// Coords holds the geometry for Kind: for ShadingAxial, [0..3] are
	// x0,y0,x1,y1 (the axis endpoints) and [4],[5] are unused; for
	// ShadingRadial, [0..5] are fx,fy,fr,cx,cy,cr (the focal circle followed
	// by the outer circle).
	Coords [6]float64
	Stops  []ShadingStop
	Spread SpreadMode
}

// ShadingDescriber is an OPTIONAL companion to Shader: a Shader that can
// describe its own geometry implements it so a vector backend can emit a
// native shading instead of sampling ColorAt per pixel. It is purely an
// optimization — a backend MUST fall back to ColorAt sampling when a Shader
// does not implement ShadingDescriber (or DescribeShading returns ok=false)
// — so no caller may require it.
//
// ok=false from DescribeShading means "I could describe shadings in
// principle but not this particular instance" (e.g. a mesh shading, or a
// shading whose alpha/spread has no native vector equivalent in the target
// format), letting a describer decline per-instance without the backend
// needing to know its concrete type.
//
// IMPORTANT for anyone wrapping a Shader (e.g. to fold in a constant alpha
// factor): the wrapper MUST implement ShadingDescriber itself and delegate to
// the inner Shader's description, adjusting it as needed (see
// pkg/svg/draw.alphaShader for the pattern). A wrapper that only forwards
// ColorAt silently loses the description — the backend's type-assertion sees
// the wrapper, not the describable Shader underneath, and falls back to
// rasterizing exactly the shadings most likely to need the wrapper (any
// gradient under an opacity).
type ShadingDescriber interface {
	Shader
	// DescribeShading reports this Shader's geometry, ramp, and spread mode.
	// ok=false means this instance cannot be described; callers must fall
	// back to ColorAt sampling.
	DescribeShading() (ShadingDesc, bool)
}
