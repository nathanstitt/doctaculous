package hevc

// Deblocking filter (spec 8.7.2), specialized for all-intra content: every
// filtered edge has boundary strength 2, so the PU/MV strength derivation
// disappears and only the geometry (TU/CU edges on the 8x8 luma grid)
// remains. The vertical-edge pass runs over the whole picture first, then
// the horizontal pass consumes its output; filters of adjacent edges on the
// 8-sample grid never overlap, so each pass works in place.

// betaTable / tcTable (spec Table 8-11). tc is indexed by Q+2*(bS-1) = Q+2.
var betaTable = [52]int32{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 20, 22, 24,
	26, 28, 30, 32, 34, 36, 38, 40, 42, 44, 46, 48, 50, 52, 54, 56,
	58, 60, 62, 64,
}

var tcTable = [54]int32{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4,
	4, 4, 5, 5, 6, 6, 7, 8, 9, 10, 11, 13, 14, 16, 18, 20, 22, 24,
}

// deblockPicture applies both passes.
func (d *sliceDecoder) deblockPicture() {
	d.deblockPass(true)
	d.deblockPass(false)
}

// deblockPass filters all vertical (vertical=true) or horizontal edges.
func (d *sliceDecoder) deblockPass(vertical bool) {
	s := d.sps
	// Luma: edges on the 8x8 grid, 4-sample segments.
	for y := 0; y < s.height; y += 4 {
		for x := 0; x < s.width; x += 4 {
			if vertical {
				if x == 0 || x%8 != 0 || !d.vertEdge[d.idx4(x, y)] {
					continue
				}
			} else {
				if y == 0 || y%8 != 0 || !d.horEdge[d.idx4(x, y)] {
					continue
				}
			}
			if !d.edgeFilterable(x, y, vertical) {
				continue
			}
			d.filterLumaSegment(x, y, vertical)
			// Chroma edges sit on the 16-luma grid; one 4-luma segment
			// maps to 4 chroma rows/cols handled once per 8-luma run.
			if vertical {
				if x%16 == 0 && y%8 == 0 {
					d.filterChromaSegment(x, y, vertical)
				}
			} else {
				if y%16 == 0 && x%8 == 0 {
					d.filterChromaSegment(x, y, vertical)
				}
			}
		}
	}
}

// edgeFilterable checks the per-CTB disable flag and slice/tile boundary
// filtering rules for the edge at (x, y).
func (d *sliceDecoder) edgeFilterable(x, y int, vertical bool) bool {
	s := d.sps
	ctbQ := (y>>s.ctbLog2SizeY)*s.picWidthCtbs + x>>s.ctbLog2SizeY
	if d.ctbDeblockDisabled[ctbQ] {
		return false
	}
	px, py := x, y
	if vertical {
		px = x - 1
	} else {
		py = y - 1
	}
	ctbP := (py>>s.ctbLog2SizeY)*s.picWidthCtbs + px>>s.ctbLog2SizeY
	if ctbP == ctbQ {
		return true
	}
	if d.ctbSlice[ctbP] != d.ctbSlice[ctbQ] && !d.ctbLFAcrossSlice[ctbQ] {
		return false
	}
	if d.tileID(ctbP) != d.tileID(ctbQ) && !d.pps.loopFilterAcrossTiles {
		return false
	}
	// The P side may also have deblocking disabled for its slice.
	if d.ctbDeblockDisabled[ctbP] {
		return false
	}
	return true
}

// deblockQP returns the average luma QP across the edge at (x,y).
func (d *sliceDecoder) deblockQP(x, y int, vertical bool) int32 {
	px, py := x, y
	if vertical {
		px = x - 1
	} else {
		py = y - 1
	}
	qpP := int32(d.qpMap[d.idx4(px, py)])
	qpQ := int32(d.qpMap[d.idx4(x, y)])
	return (qpP + qpQ + 1) >> 1
}

// ctbOffsets returns the beta/tc offsets of the CTB containing (x, y).
func (d *sliceDecoder) ctbOffsets(x, y int) (int32, int32) {
	s := d.sps
	ctb := (y>>s.ctbLog2SizeY)*s.picWidthCtbs + x>>s.ctbLog2SizeY
	return d.ctbBetaOff[ctb], d.ctbTcOff[ctb]
}

// filterLumaSegment filters one 4-sample luma edge segment (8.7.2.5.3/7).
func (d *sliceDecoder) filterLumaSegment(x, y int, vertical bool) {
	s := d.sps
	plane, stride := d.frame.Y, d.frame.YStride
	bd := s.bitDepthLuma
	maxVal := int32(1)<<bd - 1

	// sample(i, k): i indexes across the edge (-4..3 = p3..q3), k along it.
	at := func(i, k int) int {
		if vertical {
			return (y+k)*stride + x + i
		}
		return (y+i)*stride + x + k
	}
	get := func(i, k int) int32 { return int32(plane[at(i, k)]) }
	set := func(i, k int, v int32) { plane[at(i, k)] = uint16(clip3(0, maxVal, v)) }

	qpL := d.deblockQP(x, y, vertical)
	betaOff, tcOff := d.ctbOffsets(x, y)
	qB := clip3(0, 51, qpL+betaOff*2)
	beta := betaTable[qB] * (1 << (bd - 8))
	qT := clip3(0, 53, qpL+2+tcOff*2) // bS=2 -> +2
	tc := tcTable[qT] * (1 << (bd - 8))
	if beta == 0 && tc == 0 {
		return
	}

	dp0 := abs32(get(-3, 0) - 2*get(-2, 0) + get(-1, 0))
	dp3 := abs32(get(-3, 3) - 2*get(-2, 3) + get(-1, 3))
	dq0 := abs32(get(2, 0) - 2*get(1, 0) + get(0, 0))
	dq3 := abs32(get(2, 3) - 2*get(1, 3) + get(0, 3))
	dpq0 := dp0 + dq0
	dpq3 := dp3 + dq3
	dSum := dpq0 + dpq3
	if dSum >= beta {
		return
	}

	strong := true
	for _, k := range []int{0, 3} {
		dpq := dpq0
		if k == 3 {
			dpq = dpq3
		}
		dSam := 2*dpq < beta>>2 &&
			abs32(get(-4, k)-get(-1, k))+abs32(get(0, k)-get(3, k)) < beta>>3 &&
			abs32(get(-1, k)-get(0, k)) < (5*tc+1)>>1
		if !dSam {
			strong = false
			break
		}
	}

	noP := d.noFilterAt(x, y, vertical, true)
	noQ := d.noFilterAt(x, y, vertical, false)

	for k := range 4 {
		p0, p1, p2, p3 := get(-1, k), get(-2, k), get(-3, k), get(-4, k)
		q0, q1, q2, q3 := get(0, k), get(1, k), get(2, k), get(3, k)
		if strong {
			if !noP {
				set(-1, k, clip3(p0-2*tc, p0+2*tc, (p2+2*p1+2*p0+2*q0+q1+4)>>3))
				set(-2, k, clip3(p1-2*tc, p1+2*tc, (p2+p1+p0+q0+2)>>2))
				set(-3, k, clip3(p2-2*tc, p2+2*tc, (2*p3+3*p2+p1+p0+q0+4)>>3))
			}
			if !noQ {
				set(0, k, clip3(q0-2*tc, q0+2*tc, (p1+2*p0+2*q0+2*q1+q2+4)>>3))
				set(1, k, clip3(q1-2*tc, q1+2*tc, (p0+q0+q1+q2+2)>>2))
				set(2, k, clip3(q2-2*tc, q2+2*tc, (p0+q0+q1+3*q2+2*q3+4)>>3))
			}
			continue
		}
		delta := (9*(q0-p0) - 3*(q1-p1) + 8) >> 4
		if abs32(delta) >= tc*10 {
			continue
		}
		delta = clip3(-tc, tc, delta)
		if !noP {
			set(-1, k, p0+delta)
		}
		if !noQ {
			set(0, k, q0-delta)
		}
		sideThresh := (beta + (beta >> 1)) >> 3
		if dp0+dp3 < sideThresh && !noP {
			dp := clip3(-(tc >> 1), tc>>1, (((p2+p0+1)>>1)-p1+delta)>>1)
			set(-2, k, p1+dp)
		}
		if dq0+dq3 < sideThresh && !noQ {
			dq := clip3(-(tc >> 1), tc>>1, (((q2+q0+1)>>1)-q1-delta)>>1)
			set(1, k, q1+dq)
		}
	}
}

// filterChromaSegment filters the chroma samples across the edge whose luma
// anchor is (x, y): 4 chroma lines for 4:2:0 (8.7.2.5.5).
func (d *sliceDecoder) filterChromaSegment(x, y int, vertical bool) {
	s := d.sps
	bd := s.bitDepthChroma
	maxVal := int32(1)<<bd - 1
	cx, cy := x/2, y/2
	qpL := d.deblockQP(x, y, vertical)
	_, tcOff := d.ctbOffsets(x, y)
	noP := d.noFilterAt(x, y, vertical, true)
	noQ := d.noFilterAt(x, y, vertical, false)

	for ci, plane := range [][]uint16{d.frame.Cb, d.frame.Cr} {
		cQpOff := d.pps.cbQpOffset
		if ci == 1 {
			cQpOff = d.pps.crQpOffset
		}
		qpi := clip3(0, 57, qpL+cQpOff)
		qpc := chromaQPMapping(qpi)
		qT := clip3(0, 53, qpc+2+tcOff*2)
		tc := tcTable[qT] * (1 << (bd - 8))
		if tc == 0 {
			continue
		}
		stride := d.frame.CStride
		at := func(i, k int) int {
			if vertical {
				return (cy+k)*stride + cx + i
			}
			return (cy+i)*stride + cx + k
		}
		for k := range 4 {
			p0 := int32(plane[at(-1, k)])
			p1 := int32(plane[at(-2, k)])
			q0 := int32(plane[at(0, k)])
			q1 := int32(plane[at(1, k)])
			delta := clip3(-tc, tc, ((q0-p0)<<2+p1-q1+4)>>3)
			if !noP {
				plane[at(-1, k)] = uint16(clip3(0, maxVal, p0+delta))
			}
			if !noQ {
				plane[at(0, k)] = uint16(clip3(0, maxVal, q0-delta))
			}
		}
	}
}

// noFilterAt reports whether the P (side=true) or Q side of the edge at
// (x, y) is exempt from in-loop filtering (PCM with the disable flag, or a
// transquant-bypass CU).
func (d *sliceDecoder) noFilterAt(x, y int, vertical, pSide bool) bool {
	if d.noFilter == nil {
		return false
	}
	px, py := x, y
	if pSide {
		if vertical {
			px = x - 1
		} else {
			py = y - 1
		}
	}
	return d.noFilter[d.idx4(px, py)]
}
