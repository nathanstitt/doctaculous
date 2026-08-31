package font

import (
	"bytes"
	"image"
	"image/png"
)

// Bitmap colour glyphs: `sbix` (Apple) and `CBDT`/`CBLC` (Google).
//
// Where COLR describes a colour glyph as layered outlines, these tables store a
// rendered PNG per glyph per size — Apple Color Emoji and the original Noto Color
// Emoji both ship this way. There is no outline to scale, so the result is an IMAGE
// the caller draws into the glyph's box, and quality depends on picking a strike near
// the used size.
//
// This is deliberately the second choice: a caller should prefer COLR (vector, scales
// cleanly) and reach here only when the face has no COLR data. FEATURES.md says so
// rather than implying bitmap emoji scale like text.

// ColorBitmap is a decoded colour-glyph image plus the metrics needed to place it.
//
// OriginX is the image's horizontal offset from the pen, and BearingY is the height of
// the image's TOP above the baseline — both in STRIKE PIXELS, the same units the image
// itself is in. PPEM is the strike's design size, so a caller scales everything by
// (em size / PPEM).
//
// BearingY is a top bearing rather than a bottom offset because that is what both
// table families report: sbix's originOffsetY and CBDT's bearingY are measured from
// the baseline to the image's top edge. Storing it as anything else invites the caller
// to add the image height a second time, which puts the glyph an em off the line.
type ColorBitmap struct {
	Img      image.Image
	OriginX  float64
	BearingY float64
	PPEM     float64
	// HasBearing is false when the strike declares no vertical placement (Apple Color
	// Emoji reports 0/0), so the caller falls back to the face's own metrics instead
	// of treating "0" as "top of the image is on the baseline".
	HasBearing bool
}

// sbixTable is the parsed Apple `sbix` table: strikes sorted by ppem.
type sbixTable struct {
	raw     []byte
	strikes []sbixStrike
}

type sbixStrike struct {
	ppem   float64
	offset uint32
}

// cbdtTable is the parsed Google `CBLC` index plus its `CBDT` image data.
type cbdtTable struct {
	cblc, cbdt []byte
	strikes    []cbdtStrike
}

type cbdtStrike struct {
	ppem                  float64
	indexSubTableArrayOff uint32
	numIndexSubTables     uint32
}

// parseSbix reads the `sbix` header. numGlyphs is needed because each strike's glyph
// offsets are an array of numGlyphs+1 entries.
func parseSbix(b []byte, numGlyphs int) (*sbixTable, bool) {
	if len(b) < 8 || numGlyphs <= 0 {
		return nil, false
	}
	n := int(be32(b, 4))
	if n <= 0 || !within(b, 8, n*4) {
		return nil, false
	}
	t := &sbixTable{raw: b}
	for i := 0; i < n; i++ {
		off := be32(b, 8+i*4)
		if !within(b, off, 4) {
			continue
		}
		ppem := float64(be16(b, int(off)))
		if ppem <= 0 {
			continue
		}
		t.strikes = append(t.strikes, sbixStrike{ppem: ppem, offset: off})
	}
	if len(t.strikes) == 0 {
		return nil, false
	}
	return t, true
}

// bitmapFor returns the decoded image for gid at the strike nearest sizePt.
func (t *sbixTable) bitmapFor(gid uint16, numGlyphs int, sizePt float64) (ColorBitmap, bool) {
	s, ok := pickStrike(len(t.strikes), sizePt, func(i int) float64 { return t.strikes[i].ppem })
	if !ok {
		return ColorBitmap{}, false
	}
	// Try the chosen strike, then any other: a glyph need not be present in every
	// strike, and a missing entry there is not a missing glyph.
	order := strikeOrder(len(t.strikes), s)
	for _, i := range order {
		if bm, ok := t.glyphIn(t.strikes[i], gid, numGlyphs); ok {
			return bm, true
		}
	}
	return ColorBitmap{}, false
}

// glyphIn decodes gid's image from one strike, or ok=false when the strike has no data
// for it (an empty offset range) or the payload is not a format this decodes.
func (t *sbixTable) glyphIn(s sbixStrike, gid uint16, numGlyphs int) (ColorBitmap, bool) {
	// GlyphDataOffset[numGlyphs+1] follows the 4-byte strike header.
	base := int(s.offset) + 4
	if !within(t.raw, uint32(base), (numGlyphs+1)*4) {
		return ColorBitmap{}, false
	}
	start := be32(t.raw, base+int(gid)*4)
	end := be32(t.raw, base+int(gid)*4+4)
	if end <= start {
		return ColorBitmap{}, false // no bitmap for this glyph in this strike
	}
	rec := int(s.offset) + int(start)
	recEnd := int(s.offset) + int(end)
	// glyphData: originOffsetX(int16) originOffsetY(int16) graphicType(4) data[]
	if rec+8 > recEnd || recEnd > len(t.raw) {
		return ColorBitmap{}, false
	}
	ox := float64(int16(be16(t.raw, rec)))
	oy := float64(int16(be16(t.raw, rec+2)))
	hasBearing := oy != 0
	if string(t.raw[rec+4:rec+8]) != "png " {
		// jpg/tiff strikes exist in principle; only PNG is decoded, and an unsupported
		// type degrades to the monochrome path rather than to a broken image.
		return ColorBitmap{}, false
	}
	img, err := png.Decode(bytes.NewReader(t.raw[rec+8 : recEnd]))
	if err != nil {
		return ColorBitmap{}, false
	}
	// sbix's originOffsetY is the offset of the image's BOTTOM-left from the pen, so
	// the top bearing is that plus the image height.
	return ColorBitmap{
		Img: img, OriginX: ox,
		BearingY:   oy + float64(img.Bounds().Dy()),
		HasBearing: hasBearing,
		PPEM:       s.ppem,
	}, true
}

// parseCBLC reads the `CBLC` index and pairs it with `CBDT` image data.
func parseCBLC(cblc, cbdt []byte) (*cbdtTable, bool) {
	if len(cblc) < 8 || len(cbdt) < 4 {
		return nil, false
	}
	n := int(be32(cblc, 4))
	if n <= 0 || !within(cblc, 8, n*48) {
		return nil, false
	}
	t := &cbdtTable{cblc: cblc, cbdt: cbdt}
	for i := 0; i < n; i++ {
		r := 8 + i*48
		// BitmapSize: indexSubTableArrayOffset, indexTablesSize, numberOfIndexSubTables,
		// colorRef, then hori/vert metrics, startGlyph, endGlyph, ppemX, ppemY, bitDepth,
		// flags. ppemX sits at +44.
		ppem := float64(cblc[r+44])
		if ppem <= 0 {
			continue
		}
		t.strikes = append(t.strikes, cbdtStrike{
			ppem:                  ppem,
			indexSubTableArrayOff: be32(cblc, r),
			numIndexSubTables:     be32(cblc, r+8),
		})
	}
	if len(t.strikes) == 0 {
		return nil, false
	}
	return t, true
}

// bitmapFor returns the decoded image for gid at the strike nearest sizePt.
func (t *cbdtTable) bitmapFor(gid uint16, sizePt float64) (ColorBitmap, bool) {
	s, ok := pickStrike(len(t.strikes), sizePt, func(i int) float64 { return t.strikes[i].ppem })
	if !ok {
		return ColorBitmap{}, false
	}
	for _, i := range strikeOrder(len(t.strikes), s) {
		if bm, ok := t.glyphIn(t.strikes[i], gid); ok {
			return bm, true
		}
	}
	return ColorBitmap{}, false
}

// glyphIn walks one strike's index subtables for gid and decodes its image.
func (t *cbdtTable) glyphIn(s cbdtStrike, gid uint16) (ColorBitmap, bool) {
	for i := 0; i < int(s.numIndexSubTables); i++ {
		a := int(s.indexSubTableArrayOff) + i*8
		if a+8 > len(t.cblc) {
			return ColorBitmap{}, false
		}
		first, last := be16(t.cblc, a), be16(t.cblc, a+2)
		if gid < first || gid > last {
			continue
		}
		h := int(s.indexSubTableArrayOff) + int(be32(t.cblc, a+4))
		if h+8 > len(t.cblc) {
			return ColorBitmap{}, false
		}
		idxFormat, imgFormat := be16(t.cblc, h), be16(t.cblc, h+2)
		imgDataOff := be32(t.cblc, h+4)
		k := int(gid - first)

		var start, end uint32
		switch idxFormat {
		case 1: // 4-byte offsets, variable-length images
			o := h + 8 + k*4
			if o+8 > len(t.cblc) {
				return ColorBitmap{}, false
			}
			start, end = be32(t.cblc, o), be32(t.cblc, o+4)
		case 2: // constant-size images
			if h+12 > len(t.cblc) {
				return ColorBitmap{}, false
			}
			sz := be32(t.cblc, h+8)
			start = uint32(k) * sz
			end = start + sz
		default:
			return ColorBitmap{}, false
		}
		if end <= start {
			return ColorBitmap{}, false
		}
		return decodeCBDTGlyph(t.cbdt, imgDataOff+start, imgDataOff+end, imgFormat, s.ppem)
	}
	return ColorBitmap{}, false
}

// decodeCBDTGlyph decodes one CBDT record. Formats 17/18/19 carry PNG data after a
// small or big metrics header; the others are monochrome/greyscale bitmaps this does
// not decode (a colour path has no use for them).
func decodeCBDTGlyph(cbdt []byte, start, end uint32, imgFormat uint16, ppem float64) (ColorBitmap, bool) {
	if int(end) > len(cbdt) || end <= start {
		return ColorBitmap{}, false
	}
	rec := cbdt[start:end]
	var ox, oy float64
	var data []byte
	switch imgFormat {
	// smallGlyphMetrics: height, width, bearingX, bearingY, advance. bearingY is the
	// TOP bearing — the image's top edge above the baseline — which is exactly what
	// ColorBitmap.BearingY wants, so it is taken as-is.
	case 17: // smallGlyphMetrics(5) + dataLen(4) + PNG
		if len(rec) < 9 {
			return ColorBitmap{}, false
		}
		ox, oy = float64(int8(rec[2])), float64(int8(rec[3]))
		data = rec[9:]
	case 18: // bigGlyphMetrics(8) + dataLen(4) + PNG
		if len(rec) < 12 {
			return ColorBitmap{}, false
		}
		ox, oy = float64(int8(rec[2])), float64(int8(rec[3]))
		data = rec[12:]
	case 19: // dataLen(4) + PNG, metrics from the strike
		if len(rec) < 4 {
			return ColorBitmap{}, false
		}
		data = rec[4:]
	default:
		return ColorBitmap{}, false
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return ColorBitmap{}, false
	}
	return ColorBitmap{Img: img, OriginX: ox, BearingY: oy, HasBearing: oy != 0, PPEM: ppem}, true
}

// pickStrike chooses the strike closest to sizePt, preferring a LARGER one on a tie so
// the image is downscaled rather than blown up — downscaling loses no detail, upscaling
// invents it.
func pickStrike(n int, sizePt float64, ppemAt func(int) float64) (int, bool) {
	if n == 0 {
		return 0, false
	}
	best, bestScore := 0, 0.0
	for i := 0; i < n; i++ {
		p := ppemAt(i)
		score := p - sizePt
		if score < 0 {
			score = -score * 1.5 // penalize upscaling
		}
		if i == 0 || score < bestScore {
			best, bestScore = i, score
		}
	}
	return best, true
}

// strikeOrder lists strike indices starting at first, then the rest in order, so a
// glyph missing from the preferred strike can still be found in another.
func strikeOrder(n, first int) []int {
	out := make([]int, 0, n)
	out = append(out, first)
	for i := 0; i < n; i++ {
		if i != first {
			out = append(out, i)
		}
	}
	return out
}
