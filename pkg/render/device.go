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

	// Save and Restore manage the clip/state stack so the interpreter's q/Q
	// operators can be mirrored by the backend where clip state lives.
	Save()
	Restore()
}

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
