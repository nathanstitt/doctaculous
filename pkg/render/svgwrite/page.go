package svgwrite

import (
	"fmt"
	"image/color"
	"io"
	"strings"
)

// Options configures SVG output.
type Options struct {
	// Background fills the page before drawing. nil leaves the page
	// transparent, which is the useful default for a vector format — unlike a
	// rasterized page, an SVG composited onto an unknown backdrop should not
	// carry an assumed white rectangle.
	Background color.Color
	// Title, when non-empty, becomes the root <title>, which is what assistive
	// technology and browser tabs read.
	Title string
	// Logf receives degradation diagnostics (nil -> discarded).
	Logf func(string, ...any)
}

// WriteTo serializes everything painted into d as a complete SVG document.
//
// It closes any elements still open, so a caller that unbalanced Save/PushClip
// still gets well-formed markup rather than a truncated tree.
func (d *Device) WriteTo(out io.Writer, opts Options) error {
	body := d.finish()

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	// width/height in user units plus a matching viewBox: the document has an
	// intrinsic size, and still scales cleanly when a consumer overrides it.
	// No xmlns:xlink: this writer emits the plain SVG2 href everywhere, so the
	// legacy namespace would be declared and never used.
	fmt.Fprintf(&sb,
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`+"\n",
		d.wPx, d.hPx, d.wPx, d.hPx)
	if opts.Title != "" {
		fmt.Fprintf(&sb, "<title>%s</title>\n", escapeText(opts.Title))
	}
	if defs := d.defs.String(); defs != "" {
		sb.WriteString("<defs>\n")
		sb.WriteString(defs)
		sb.WriteString("</defs>\n")
	}
	if opts.Background != nil {
		if hex, alpha := colorAttr(rgbaOf(opts.Background)); alpha > 0 {
			fmt.Fprintf(&sb, `<rect width="%d" height="%d" fill=%q`, d.wPx, d.hPx, hex)
			writeOpacity(&sb, "fill-opacity", alpha)
			sb.WriteString("/>\n")
		}
	}
	sb.WriteString(body)
	sb.WriteString("</svg>\n")

	if _, err := io.WriteString(out, sb.String()); err != nil {
		return fmt.Errorf("svgwrite: write: %w", err)
	}
	return nil
}

// finish closes any still-open elements and returns the page content.
//
// Unbalanced state is closed rather than reported: the Device contract is
// forgiving about unbalanced Restore/EndGroup, and emitting truncated markup
// would fail to parse — a far worse outcome than silently completing the tree.
func (d *Device) finish() string {
	for len(d.groups) > 0 {
		d.EndGroup(1, "", nil, nil)
	}
	for len(d.elems) > 0 {
		d.popElem()
	}
	return d.root.String()
}

// rgbaOf converts any color.Color to the 8-bit RGBA the writer emits.
// color.Color reports 16-bit premultiplied channels, so this un-premultiplies
// before narrowing — otherwise a half-transparent color would darken.
func rgbaOf(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
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
