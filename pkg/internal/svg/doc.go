// Package svg parses Scalable Vector Graphics documents into a read-only scene
// graph rendered through render.Device by pkg/svg/draw. It is the engine's SVG
// frontend: standalone .svg documents open through it, and (in later slices)
// inline <svg> in HTML and <img src="*.svg"> resolve through it. Unsupported
// features degrade gracefully: the element or attribute is skipped with a
// debug log and the rest of the document still renders.
package svg
