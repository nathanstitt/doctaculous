package font

import (
	"encoding/binary"
	"image/color"
)

// COLR/CPAL: colour glyphs as layered outlines.
//
// A colour glyph has no outline of its own — its base glyph is usually empty — and is
// instead a LIST OF LAYERS, each a normal glyph id plus a palette colour. Rendering
// one is therefore the existing outline path repeated per layer with a fill colour,
// which is why this decodes to []ColorLayer rather than to an image: the result stays
// resolution-independent and composes with transforms, PDF vector output, and the
// rasterizer alike.
//
// Two table versions exist. v0 is a flat (baseGlyph -> layer range) mapping and is what
// this file implements fully. v1 adds a paint GRAPH — gradients, transforms, composites
// — reached through a separate BaseGlyphList; colrV1Layers below flattens the subset of
// that graph this engine can express (solid fills and nested layer lists), and reports
// the rest as unsupported so the caller degrades to the monochrome outline rather than
// painting something wrong.
//
// Offsets are validated on every read. A colour table is untrusted input reached from a
// document, and a malformed one must degrade to "no colour glyph" rather than panic.

// ColorLayer is one layer of a colour glyph: an outline glyph id and the colour to
// fill it with. A layer whose Foreground is true takes the text colour instead of a
// palette entry (COLR's 0xFFFF sentinel), so an icon font can follow the surrounding
// text.
type ColorLayer struct {
	GID        uint16
	Color      color.RGBA
	Foreground bool
	// XX..DY is the layer's full 2x3 affine transform in the outline's own font units
	// (applied before the em scale). COLR v1 positions and orients parts of a glyph
	// with transform paints, and real emoji use the whole range: a translation to move
	// a reused part, a mirror (xx = -1) to build a symmetric glyph from one half, and
	// genuine rotation+scale for things like a party popper's streamers.
	//
	// Carrying the full matrix rather than a translation+flip is what lets those last
	// ones paint at all: an earlier version modelled only offset and flip and had to
	// refuse every rotated layer, which threw away whole glyphs.
	//
	// The zero value means "no transform" — XX/YY are 0 there and read as 1 by
	// Transform — so a v0 layer needs no initialization.
	XX, YX, XY, YY float64
	DX, DY         float64

	// Gradient, when non-nil, fills the layer with a gradient instead of Color. COLR
	// v1 defines linear, radial, and sweep fills; the first two map onto the render
	// layer's existing axial/radial shading, and a layer carrying one is painted by
	// clipping to its outline and shading through it.
	Gradient *ColorGradient
}

// ColorGradient is a COLR v1 gradient fill, in FONT UNITS (the same space as the
// outline it fills).
type ColorGradient struct {
	// Radial is false for a linear gradient (P0->P1 with a rotation point P2), true
	// for a radial one (circle C0/R0 to circle C1/R1).
	Radial bool
	// X0..R1 are the geometry. For a linear gradient only X0,Y0,X1,Y1 and the
	// rotation point X2,Y2 are meaningful; for radial, the two circles.
	X0, Y0, X1, Y1, X2, Y2 float64
	R0, R1                 float64
	// Stops are the colour stops, sorted by Offset in [0,1].
	Stops []GradientStop
	// Extend is the CSS/OpenType spread mode outside [0,1]: "pad", "repeat", or
	// "reflect".
	Extend string
}

// GradientStop is one colour stop of a ColorGradient.
type GradientStop struct {
	Offset float64
	Color  color.RGBA
}

// Transform reports the layer's affine as (xx, yx, xy, yy, dx, dy), normalizing the
// zero value to the identity so a caller can apply it unconditionally.
func (l ColorLayer) Transform() (xx, yx, xy, yy, dx, dy float64) {
	xx, yx, xy, yy = l.XX, l.YX, l.XY, l.YY
	if xx == 0 && yx == 0 && xy == 0 && yy == 0 {
		xx, yy = 1, 1
	}
	return xx, yx, xy, yy, l.DX, l.DY
}

// IsIdentity reports whether the layer has no transform, so a caller can skip the
// matrix build for the common case.
func (l ColorLayer) IsIdentity() bool {
	return l.XX == 0 && l.YX == 0 && l.XY == 0 && l.YY == 0 && l.DX == 0 && l.DY == 0
}

// colrTable is the parsed COLR table: the v0 base-glyph records and layer list, plus
// the raw bytes and v1 offsets for the paint-graph walk.
type colrTable struct {
	version   uint16
	baseV0    []byte // numBaseGlyphRecords * 6
	layersV0  []byte // numLayerRecords * 4
	numBase   int
	numLayers int

	raw         []byte // whole table, for the v1 offsets below
	baseListV1  uint32
	layerListV1 uint32
}

// cpalTable is the parsed CPAL palette table.
type cpalTable struct {
	entriesPerPalette int
	colors            []color.RGBA // numPaletteEntries * numPalettes, palette-major
}

// parseCOLR reads the COLR table header. It returns ok=false for a truncated or
// unrecognized table so the caller falls back to monochrome.
func parseCOLR(b []byte) (*colrTable, bool) {
	if len(b) < 14 {
		return nil, false
	}
	t := &colrTable{version: be16(b, 0), raw: b}
	t.numBase = int(be16(b, 2))
	baseOff := be32(b, 4)
	layerOff := be32(b, 8)
	t.numLayers = int(be16(b, 12))

	if !within(b, baseOff, t.numBase*6) || !within(b, layerOff, t.numLayers*4) {
		return nil, false
	}
	t.baseV0 = b[baseOff : int(baseOff)+t.numBase*6]
	t.layersV0 = b[layerOff : int(layerOff)+t.numLayers*4]

	if t.version >= 1 {
		if len(b) < 30 {
			return nil, false
		}
		t.baseListV1 = be32(b, 14)
		t.layerListV1 = be32(b, 18)
	}
	return t, true
}

// parseCPAL reads the CPAL palette table.
func parseCPAL(b []byte) (*cpalTable, bool) {
	if len(b) < 12 {
		return nil, false
	}
	entries := int(be16(b, 2))
	numPalettes := int(be16(b, 4))
	numRecords := int(be16(b, 6))
	recordsOff := be32(b, 8)
	if entries <= 0 || numPalettes <= 0 || !within(b, recordsOff, numRecords*4) {
		return nil, false
	}
	// Palettes index into the shared colour-record array; only the first palette is
	// used here (CPAL v1 adds light/dark variants, which need a document-level
	// preference this engine does not model).
	if len(b) < 12+2*numPalettes {
		return nil, false
	}
	firstIndex := int(be16(b, 12))
	if firstIndex+entries > numRecords {
		return nil, false
	}
	out := make([]color.RGBA, entries)
	for i := range out {
		o := int(recordsOff) + (firstIndex+i)*4
		// CPAL stores BGRA, not RGBA.
		out[i] = color.RGBA{B: b[o], G: b[o+1], R: b[o+2], A: b[o+3]}
	}
	return &cpalTable{entriesPerPalette: entries, colors: out}, true
}

// colorAt returns palette entry i, or ok=false when out of range.
func (p *cpalTable) colorAt(i uint16) (color.RGBA, bool) {
	if p == nil || int(i) >= len(p.colors) {
		return color.RGBA{}, false
	}
	return p.colors[i], true
}

// layersFor returns the colour layers of base glyph gid, or ok=false when the glyph is
// not a colour glyph (the caller then draws it as a normal monochrome outline).
func (t *colrTable) layersFor(gid uint16, pal *cpalTable) ([]ColorLayer, bool) {
	if t == nil {
		return nil, false
	}
	if l, ok := t.colrV0Layers(gid, pal); ok {
		return l, true
	}
	if t.version >= 1 {
		return t.colrV1Layers(gid, pal)
	}
	return nil, false
}

// colrV0Layers resolves the flat v0 mapping. The base-glyph records are sorted by
// glyph id, so this binary-searches rather than scanning — a document repeating one
// emoji resolves it per occurrence.
func (t *colrTable) colrV0Layers(gid uint16, pal *cpalTable) ([]ColorLayer, bool) {
	lo, hi := 0, t.numBase-1
	for lo <= hi {
		mid := (lo + hi) / 2
		g := be16(t.baseV0, mid*6)
		switch {
		case g < gid:
			lo = mid + 1
		case g > gid:
			hi = mid - 1
		default:
			first := int(be16(t.baseV0, mid*6+2))
			count := int(be16(t.baseV0, mid*6+4))
			if first+count > t.numLayers {
				return nil, false
			}
			out := make([]ColorLayer, 0, count)
			for i := first; i < first+count; i++ {
				out = append(out, layerRecord(t.layersV0, i, pal))
			}
			return out, len(out) > 0
		}
	}
	return nil, false
}

// layerRecord decodes one v0 LayerRecord: a glyph id plus a palette index, where
// 0xFFFF means "use the text colour".
func layerRecord(layers []byte, i int, pal *cpalTable) ColorLayer {
	g := be16(layers, i*4)
	idx := be16(layers, i*4+2)
	if idx == 0xFFFF {
		return ColorLayer{GID: g, Foreground: true}
	}
	c, ok := pal.colorAt(idx)
	if !ok {
		// A palette index the table does not have: fall back to the text colour rather
		// than to an arbitrary colour, so the glyph still reads as intended.
		return ColorLayer{GID: g, Foreground: true}
	}
	return ColorLayer{GID: g, Color: c}
}

// be16, be32, and within are bounds-checked big-endian readers. Every COLR/CPAL read
// goes through them: the tables are untrusted document input, and an out-of-range
// offset must degrade rather than panic.
func be16(b []byte, o int) uint16 {
	if o < 0 || o+2 > len(b) {
		return 0
	}
	return binary.BigEndian.Uint16(b[o:])
}

func be32(b []byte, o int) uint32 {
	if o < 0 || o+4 > len(b) {
		return 0
	}
	return binary.BigEndian.Uint32(b[o:])
}

// within reports whether [off, off+n) lies inside b.
func within(b []byte, off uint32, n int) bool {
	return n >= 0 && int(off) >= 0 && int(off)+n <= len(b)
}
