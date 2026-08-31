// Package content interprets a PDF page content stream. It tokenizes the stream
// into operands and operators, maintains the graphics-state stack (CTM, colors,
// clip, text state), and dispatches drawing operations to a render.Device.
//
// The interpreter is backend-agnostic: it never rasterizes directly, so the same
// interpretation can drive a bitmap backend, an SVG backend, or others.
package content
