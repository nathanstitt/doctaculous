package hevc

import "slices"

// Sample-adaptive offset (spec 7.3.8.3 syntax, 8.7.3 application). Params
// are parsed per CTB during slice decoding and applied picture-wide after
// deblocking, reading from a frozen copy so filtered neighbors don't feed
// back into the classification.

const (
	saoOff  = 0
	saoBand = 1
	saoEdge = 2
)

// saoCtbParams holds one CTB's SAO parameters; component index 0=Y, 1=Cb,
// 2=Cr.
type saoCtbParams struct {
	typeIdx [3]uint8
	offsets [3][4]int32
	bandPos [3]int
	eoClass [3]int
}

// parseSAO decodes the CTB's SAO syntax (called before the coding quadtree).
func (d *sliceDecoder) parseSAO(ctbAddr, col, row int) {
	s := d.sps
	hdr := d.hdr
	p := &d.ctbSao[ctbAddr]

	// Merge candidates must be in the same slice and tile.
	mergeOK := func(other int) bool {
		return d.ctbSlice[other] == d.sliceID && d.tileID(other) == d.tileID(ctbAddr)
	}
	if col > 0 && mergeOK(ctbAddr-1) {
		if d.cabac.decodeBin(d.ctx, ctxSaoMergeFlag) == 1 {
			*p = d.ctbSao[ctbAddr-1]
			return
		}
	}
	if row > 0 && mergeOK(ctbAddr-s.picWidthCtbs) {
		if d.cabac.decodeBin(d.ctx, ctxSaoMergeFlag) == 1 {
			*p = d.ctbSao[ctbAddr-s.picWidthCtbs]
			return
		}
	}

	maxOffset := int32(1)<<uint(min(s.bitDepthLuma, 10)-5) - 1
	for cIdx := range 3 {
		if cIdx == 0 && !hdr.saoLuma {
			continue
		}
		if cIdx > 0 && !hdr.saoChroma {
			continue
		}
		switch cIdx {
		case 0, 1:
			// sao_type_idx_{luma,chroma}: one context bin, then a bypass.
			if d.cabac.decodeBin(d.ctx, ctxSaoTypeIdx) == 0 {
				p.typeIdx[cIdx] = saoOff
			} else if d.cabac.decodeBypass() == 0 {
				p.typeIdx[cIdx] = saoBand
			} else {
				p.typeIdx[cIdx] = saoEdge
			}
		case 2:
			p.typeIdx[2] = p.typeIdx[1]
		}
		if p.typeIdx[cIdx] == saoOff {
			continue
		}
		var abs [4]int32
		for i := range 4 {
			// sao_offset_abs: TR bypass with cMax = maxOffset.
			v := int32(0)
			for v < maxOffset && d.cabac.decodeBypass() == 1 {
				v++
			}
			abs[i] = v
		}
		if p.typeIdx[cIdx] == saoBand {
			for i := range 4 {
				if abs[i] != 0 && d.cabac.decodeBypass() == 1 {
					abs[i] = -abs[i]
				}
			}
			p.offsets[cIdx] = abs
			p.bandPos[cIdx] = int(d.cabac.decodeBypassBits(5))
			continue
		}
		// Edge offsets: first two positive, last two negative by rule.
		p.offsets[cIdx] = [4]int32{abs[0], abs[1], -abs[2], -abs[3]}
		switch cIdx {
		case 0:
			p.eoClass[0] = int(d.cabac.decodeBypassBits(2))
		case 1:
			p.eoClass[1] = int(d.cabac.decodeBypassBits(2))
		case 2:
			p.eoClass[2] = p.eoClass[1]
		}
	}
}

// applySAO runs SAO over the whole picture, using copies of the deblocked
// planes as classification input.
func (d *sliceDecoder) applySAO() {
	s := d.sps
	srcY := slices.Clone(d.frame.Y)
	srcCb := slices.Clone(d.frame.Cb)
	srcCr := slices.Clone(d.frame.Cr)

	for ctb := range s.picSizeCtbs {
		p := &d.ctbSao[ctb]
		col := ctb % s.picWidthCtbs
		row := ctb / s.picWidthCtbs
		x0 := col << s.ctbLog2SizeY
		y0 := row << s.ctbLog2SizeY
		w := min(s.ctbSizeY, s.width-x0)
		h := min(s.ctbSizeY, s.height-y0)
		if p.typeIdx[0] != saoOff {
			d.saoComponent(d.frame.Y, srcY, d.frame.YStride, x0, y0, w, h,
				s.width, s.height, s.bitDepthLuma, p.typeIdx[0], p.offsets[0], p.bandPos[0], p.eoClass[0])
		}
		for ci := 1; ci <= 2; ci++ {
			if p.typeIdx[ci] == saoOff {
				continue
			}
			dst, src := d.frame.Cb, srcCb
			if ci == 2 {
				dst, src = d.frame.Cr, srcCr
			}
			d.saoComponent(dst, src, d.frame.CStride, x0/2, y0/2, (w+1)/2, (h+1)/2,
				s.width/2, s.height/2, s.bitDepthChroma, p.typeIdx[ci], p.offsets[ci], p.bandPos[ci], p.eoClass[ci])
		}
	}
}

// eoDelta per edge-offset class: sample offsets of the two neighbors.
var eoDelta = [4][2][2]int{
	{{-1, 0}, {1, 0}},  // class 0: horizontal
	{{0, -1}, {0, 1}},  // class 1: vertical
	{{-1, -1}, {1, 1}}, // class 2: 135 degrees
	{{1, -1}, {-1, 1}}, // class 3: 45 degrees
}

// saoComponent applies band or edge offsets to one CTB region of one plane.
func (d *sliceDecoder) saoComponent(dst, src []uint16, stride, x0, y0, w, h, planeW, planeH, bd int,
	typ uint8, offsets [4]int32, bandPos int, eoClass int) {

	maxVal := int32(1)<<bd - 1
	if typ == saoBand {
		shift := bd - 5
		var table [32]int32
		for i := range 4 {
			table[(bandPos+i)&31] = offsets[i]
		}
		for y := y0; y < y0+h; y++ {
			for x := x0; x < x0+w; x++ {
				if d.saoSkip(x, y, planeW) {
					continue
				}
				v := int32(src[y*stride+x])
				dst[y*stride+x] = uint16(clip3(0, maxVal, v+table[v>>shift&31]))
			}
		}
		return
	}
	// Edge offset.
	da, db := eoDelta[eoClass][0], eoDelta[eoClass][1]
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			ax, ay := x+da[0], y+da[1]
			bx, by := x+db[0], y+db[1]
			if ax < 0 || ay < 0 || bx < 0 || by < 0 || ax >= planeW || ay >= planeH || bx >= planeW || by >= planeH {
				continue
			}
			if d.saoSkip(x, y, planeW) {
				continue
			}
			if !d.saoNeighborUsable(x, y, ax, ay, planeW) || !d.saoNeighborUsable(x, y, bx, by, planeW) {
				continue
			}
			c := int32(src[y*stride+x])
			sgn := func(v int32) int32 {
				if v > 0 {
					return 1
				}
				if v < 0 {
					return -1
				}
				return 0
			}
			idx := 2 + sgn(c-int32(src[ay*stride+ax])) + sgn(c-int32(src[by*stride+bx]))
			var off int32
			switch idx {
			case 0:
				off = offsets[0]
			case 1:
				off = offsets[1]
			case 3:
				off = offsets[2]
			case 4:
				off = offsets[3]
			}
			if off != 0 {
				dst[y*stride+x] = uint16(clip3(0, maxVal, c+off))
			}
		}
	}
}

// saoNeighborUsable reports whether an edge-classification neighbor may be
// read: crossing a tile boundary requires loop_filter_across_tiles, and
// crossing a slice boundary requires the current CTB's across-slices flag
// (spec 8.7.3).
func (d *sliceDecoder) saoNeighborUsable(x, y, nx, ny, planeW int) bool {
	s := d.sps
	scale := 1
	if planeW < s.width {
		scale = 2
	}
	lx, ly := x*scale, y*scale
	lnx, lny := nx*scale, ny*scale
	ctb := (ly>>s.ctbLog2SizeY)*s.picWidthCtbs + lx>>s.ctbLog2SizeY
	nctb := (lny>>s.ctbLog2SizeY)*s.picWidthCtbs + lnx>>s.ctbLog2SizeY
	if ctb == nctb {
		return true
	}
	if d.tiles.tileOf[ctb] != d.tiles.tileOf[nctb] && !d.pps.loopFilterAcrossTiles {
		return false
	}
	if d.ctbSlice[ctb] != d.ctbSlice[nctb] && !d.ctbLFAcrossSlice[ctb] {
		return false
	}
	return true
}

// saoSkip reports whether the sample at plane coordinates (x, y) is exempt
// from SAO (PCM/bypass no-filter samples). Chroma positions map through the
// luma-resolution no-filter grid.
func (d *sliceDecoder) saoSkip(x, y, planeW int) bool {
	if d.noFilter == nil {
		return false
	}
	scale := 1
	if planeW < d.sps.width {
		scale = 2
	}
	return d.noFilter[d.idx4(x*scale, y*scale)]
}
