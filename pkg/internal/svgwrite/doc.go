// Package svgwrite is the SVG backend: a render.Device that serializes paint
// operations to SVG markup instead of rasterizing them.
//
// It is the vector sibling of pkg/render/raster and the structural twin of
// pkg/render/pdfwrite — all three implement the same render.Device seam, so
// this package works for every input format without knowing which one it is.
// The PDF content interpreter (pkg/pdf/content), the reflow paint layer
// (pkg/layout/paint), and the SVG painter (pkg/svg/draw) all drive a Device,
// so all three reach SVG output through this one implementation.
//
// # Why vector rather than an embedded bitmap
//
// Wrapping a rasterized page in a single <image> would be far less code, but it
// would discard exactly what SVG exists for: resolution independence, real
// geometry, and text a tool can find. The primitives happen to line up almost
// exactly — render.Path is MoveTo/LineTo/CubeTo/Close, which is SVG's M/L/C/Z
// with no arcs or quadratics to convert; render.Matrix is SVG's
// matrix(a b c d e f) positionally; device space is already top-left-origin and
// Y-down, matching SVG's default user space with no page flip. So the vector
// path costs little more than the bitmap one and preserves everything.
//
// Rasterization is still used, but only where a Device operation has no
// faithful SVG expression: BuildLuminanceMask, RenderOffscreen, and a gradient
// whose Shader cannot describe itself. Those embed a bitmap because the
// alternative is dropping the content.
//
// # Structural difference from pdfwrite
//
// A PDF content stream is a flat operator sequence where `q`/`Q` push and pop
// state in place. SVG is a tree: the same nesting has to be expressed as
// opened and closed <g> elements. That makes Save/Restore and PushClip
// structural here rather than incidental — every clip and every saved state
// opens an element that must be closed in the right order, and the writer
// tracks that nesting explicitly (see the elems stack in device.go). Groups reuse
// pdfwrite's buffer-swap idea: BeginGroup redirects output to a scratch
// buffer, EndGroup pops it and wraps the result in a <g> carrying the group's
// opacity, blend mode, clip and mask.
//
// # Text
//
// Glyphs are emitted as <path> outlines rather than <text>. The pipeline
// carries enough identity for real text on the reflow path (render.GlyphRef
// has Face, GID and Runes), but the bundled substitute faces are Type1 .pfb,
// which browsers cannot load through @font-face, and the repo has no
// WOFF/WOFF2 encoder — so <text> would render with whatever the viewer
// happened to substitute, which is not faithful. Outlines render identically
// everywhere. Each glyph carries an aria-label holding the source characters
// it stands for, so the text is still recoverable by a screen reader or a
// scraper even though it is not selectable. See docs/SVG.md for the deferred
// embedded-font mode.
package svgwrite
