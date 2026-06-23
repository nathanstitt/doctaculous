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

// Shader evaluates a PDF shading: it maps a point in shading (user) space to the
// color painted there. ok=false means the point lies outside the shading and is
// not extended, so the backdrop is left untouched. The backend builds a Shader
// from a shading dictionary (keeping shading geometry and color math out of the
// content interpreter); FillShading drives it per device pixel.
type Shader interface {
	ColorAt(userX, userY float64) (c color.RGBA, ok bool)
}
