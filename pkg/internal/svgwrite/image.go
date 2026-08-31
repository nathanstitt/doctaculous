package svgwrite

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// pngDataURI encodes img as a base64 "data:image/png" URI for an <image>
// href, or returns ok=false when encoding fails.
//
// PNG is the only choice here: it is lossless (a JPEG round-trip would corrupt
// the mask and filter results this feeds) and carries an alpha channel, which
// masks require. Encoding goes through the standard library rather than
// omnidoc.EncodeImage because pkg/render backends must not import pkg/omnidoc —
// that would invert the layer order (see docs/ARCHITECTURE.md).
func pngDataURI(img image.Image) (string, bool) {
	if img == nil {
		return "", false
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return "", false
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString("data:image/png;base64,")
	sb.WriteString(base64.StdEncoding.EncodeToString(buf.Bytes()))
	return sb.String(), true
}

// alphaToImage converts a coverage mask into an image carrying that coverage
// in its ALPHA channel, for use in a <mask mask-type="alpha">.
//
// Coverage must not be written as gray levels under SVG's default luminance
// mask-type, even though "white keeps, black removes" makes that the obvious
// encoding. A GroupMask is ALREADY final coverage — BuildLuminanceMask reduced
// the mask content to one channel using sRGB Rec. 709 (see
// render.Device.BuildLuminanceMask, and pkg/svg/mask.go for this engine's
// deliberate choice of sRGB over SVG 1.1's linearRGB). Re-encoding that number
// as rgb(Y,Y,Y) makes the viewer compute luminance a SECOND time, and SVG 1.1
// specifies that conversion in linearRGB, so a spec-strict viewer re-linearizes
// a value that is already in the engine's chosen space: coverage 128 arrives as
// 55, an error of 73/255, and the mask reads far too dark.
//
// mask-type="alpha" avoids the round trip rather than trying to steer it: the
// alpha channel is taken verbatim, with no color-space conversion in any
// viewer, so the emitted mask means exactly what the engine computed. The RGB
// channels are set to white so that a viewer ignoring mask-type (falling back
// to luminance) still reads full coverage where alpha is full, degrading to a
// too-permissive mask rather than an invisible one.
func alphaToImage(m *image.Alpha) image.Image {
	if m == nil {
		return nil
	}
	b := m.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil
	}
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.SetNRGBA(x, y, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: m.AlphaAt(x, y).A})
		}
	}
	return out
}

// escapeText escapes a string for use as XML character data.
//
// This guards the places document content reaches the markup as text: each
// glyph's aria-label, and the root <title>. A document containing "a < b"
// would otherwise emit markup that no XML parser accepts, and pkg/svg would
// reject the file this writer just produced.
func escapeText(s string) string {
	if !strings.ContainsAny(s, "<>&\"'") {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch r {
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '&':
			sb.WriteString("&amp;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&apos;")
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
