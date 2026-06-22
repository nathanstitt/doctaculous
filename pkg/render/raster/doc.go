// Package raster implements the render.Device interface on top of an
// image.RGBA. It fills paths with golang.org/x/image/vector, strokes with
// github.com/srwiley/rasterx, clips via alpha masks, fills glyph outlines from
// embedded fonts, and blits image XObjects — producing the final bitmap.
package raster
