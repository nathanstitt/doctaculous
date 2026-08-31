package hevc

// CTU decoding: coding_quadtree (spec 7.3.8.4), coding_unit (7.3.8.5, intra
// only), intra mode signaling with MPM derivation (8.4.2), transform_tree /
// transform_unit (7.3.8.8/7.3.8.10), and per-TU reconstruction.

const (
	intraPlanar = 0
	intraDC     = 1
	intraAngV   = 26
	intraAngH   = 10
)

// codingQuadtree recursively decodes the quadtree below one CTU.
func (d *sliceDecoder) codingQuadtree(x0, y0, log2CbSize, depth int) error {
	s := d.sps
	cbSize := 1 << log2CbSize
	inside := x0+cbSize <= s.width && y0+cbSize <= s.height

	split := false
	if inside && log2CbSize > s.minCbLog2SizeY {
		// split_cu_flag context from neighbor depths (9.3.4.2.2).
		ctxInc := 0
		if d.availableAt(x0-1, y0) && d.ctDepth4x4(x0-1, y0) > depth {
			ctxInc++
		}
		if d.availableAt(x0, y0-1) && d.ctDepth4x4(x0, y0-1) > depth {
			ctxInc++
		}
		split = d.cabac.decodeBin(d.ctx, ctxSplitCuFlag+ctxInc) == 1
	} else {
		split = log2CbSize > s.minCbLog2SizeY
	}

	if d.pps.cuQpDeltaEnabled && log2CbSize >= s.ctbLog2SizeY-d.pps.diffCuQpDeltaDepth {
		qgMask := ^((1 << (s.ctbLog2SizeY - d.pps.diffCuQpDeltaDepth)) - 1)
		qgX, qgY := x0&qgMask, y0&qgMask
		if qgX != d.qgX || qgY != d.qgY {
			d.qgX, d.qgY = qgX, qgY
			d.cuQpDeltaCoded = false
			d.cuQpDeltaVal = 0
			d.qpYPrev = d.lastCuQpY
		}
	}

	if split {
		half := cbSize >> 1
		for i := range 4 {
			x1 := x0 + half*(i&1)
			y1 := y0 + half*(i>>1)
			if x1 >= s.width || y1 >= s.height {
				continue
			}
			if err := d.codingQuadtree(x1, y1, log2CbSize-1, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return d.codingUnit(x0, y0, log2CbSize, depth)
}

// codingUnit decodes one intra CU.
func (d *sliceDecoder) codingUnit(x0, y0, log2CbSize, depth int) error {
	s, p := d.sps, d.pps
	cbSize := 1 << log2CbSize

	d.markCtDepth(x0, y0, cbSize, depth)

	bypass := false
	if p.transquantBypassEnabled {
		bypass = d.cabac.decodeBin(d.ctx, ctxCuTransquantBypass) == 1
	}
	d.cuBypass = bypass

	partNxN := false
	if log2CbSize == s.minCbLog2SizeY {
		if d.cabac.decodeBin(d.ctx, ctxPartMode) == 0 {
			partNxN = true
		}
	}

	if s.pcmEnabled && !partNxN &&
		log2CbSize >= s.pcmMinCbLog2 && log2CbSize <= s.pcmMaxCbLog2 {
		if d.cabac.decodeTerminate() == 1 {
			return d.decodePCM(x0, y0, log2CbSize)
		}
	}

	// Intra luma modes: all prev flags first, then per-PU mpm/rem.
	nPb := 1
	pbSize := cbSize
	if partNxN {
		nPb = 4
		pbSize = cbSize >> 1
	}
	prev := make([]bool, nPb)
	for i := range nPb {
		prev[i] = d.cabac.decodeBin(d.ctx, ctxPrevIntraLumaPred) == 1
	}
	modes := make([]int, nPb)
	for i := range nPb {
		xPb := x0 + pbSize*(i&1)
		yPb := y0 + pbSize*(i>>1)
		cand := d.mpmCandidates(xPb, yPb)
		if prev[i] {
			idx := 0
			if d.cabac.decodeBypass() == 1 {
				idx = 1 + int(d.cabac.decodeBypass())
			}
			modes[i] = cand[idx]
		} else {
			rem := int(d.cabac.decodeBypassBits(5))
			// Sort the three candidates ascending, then bump rem past
			// each candidate <= rem (spec 8.4.2 step 3).
			c := cand
			if c[0] > c[1] {
				c[0], c[1] = c[1], c[0]
			}
			if c[0] > c[2] {
				c[0], c[2] = c[2], c[0]
			}
			if c[1] > c[2] {
				c[1], c[2] = c[2], c[1]
			}
			for _, cm := range c {
				if rem >= cm {
					rem++
				}
			}
			modes[i] = rem
		}
		dbg("CU(%d,%d,%d) PU%d: prev=%v cand=%v mode=%d", x0, y0, cbSize, i, prev[i], cand, modes[i])
		d.markIntraMode(xPb, yPb, pbSize, modes[i])
	}
	d.puModes = modes
	d.puSize = pbSize

	// Chroma mode (4:2:0: one for the CU, derived from luma PU 0).
	lumaForChroma := modes[0]
	if d.cabac.decodeBin(d.ctx, ctxIntraChromaPred) == 0 {
		d.chromaMode = lumaForChroma
	} else {
		idx := d.cabac.decodeBypassBits(2)
		table := [4]int{intraPlanar, intraAngV, intraAngH, intraDC}
		m := table[idx]
		if m == lumaForChroma {
			m = 34
		}
		d.chromaMode = m
	}
	dbg("CU(%d,%d,%d) chromaMode=%d bypass=%v", x0, y0, cbSize, d.chromaMode, bypass)

	intraSplit := 0
	if partNxN {
		intraSplit = 1
	}
	maxDepth := s.maxTransformHierarchyDepthIntra + intraSplit
	d.cuX, d.cuY, d.cuLog2 = x0, y0, log2CbSize
	return d.transformTree(x0, y0, x0, y0, log2CbSize, 0, 0, maxDepth, true, true)
}

// mpmCandidates derives the three most-probable luma modes (8.4.2).
func (d *sliceDecoder) mpmCandidates(xPb, yPb int) [3]int {
	candA, candB := intraDC, intraDC
	if d.modeAvailableAt(xPb-1, yPb) {
		candA = d.intraMode4x4(xPb-1, yPb)
	}
	// The above neighbor must sit inside the same CTB row of CTBs.
	if d.modeAvailableAt(xPb, yPb-1) && (yPb-1)>>d.sps.ctbLog2SizeY == yPb>>d.sps.ctbLog2SizeY {
		candB = d.intraMode4x4(xPb, yPb-1)
	}
	if candA == candB {
		if candA < 2 {
			return [3]int{intraPlanar, intraDC, intraAngV}
		}
		return [3]int{candA, 2 + (candA+29)%32, 2 + (candA-2+1)%32}
	}
	out := [3]int{candA, candB, 0}
	switch {
	case candA != intraPlanar && candB != intraPlanar:
		out[2] = intraPlanar
	case candA != intraDC && candB != intraDC:
		out[2] = intraDC
	default:
		out[2] = intraAngV
	}
	return out
}

// decodePCM reads raw samples (7.3.8.7): CABAC terminates, the bitstream
// aligns, fixed-width samples follow, and CABAC re-initializes.
func (d *sliceDecoder) decodePCM(x0, y0, log2CbSize int) error {
	s := d.sps
	r := d.cabac.r
	for !r.byteAligned() {
		if r.u(1) != 0 {
			return errInvalidStream("PCM alignment")
		}
	}
	size := 1 << log2CbSize
	shiftY := s.bitDepthLuma - s.pcmBitDepthLuma
	for y := range size {
		for x := range size {
			v := r.u(s.pcmBitDepthLuma) << shiftY
			d.frame.Y[(y0+y)*d.frame.YStride+x0+x] = uint16(v)
		}
	}
	shiftC := s.bitDepthChroma - s.pcmBitDepthChroma
	half := size >> 1
	for _, plane := range []([]uint16){d.frame.Cb, d.frame.Cr} {
		for y := range half {
			for x := range half {
				v := r.u(s.pcmBitDepthChroma) << shiftC
				plane[(y0/2+y)*d.frame.CStride+x0/2+x] = uint16(v)
			}
		}
	}
	if err := r.err(); err != nil {
		return err
	}
	d.markIntraMode(x0, y0, size, intraDC)
	d.markDecoded(x0, y0, size, size)
	d.setCuQP(x0, y0, size, d.deriveQPY())
	if x0%8 == 0 && x0 > 0 {
		for yy := y0; yy < y0+size && yy < s.height; yy += 4 {
			d.vertEdge[d.idx4(x0, yy)] = true
		}
	}
	if y0%8 == 0 && y0 > 0 {
		for xx := x0; xx < x0+size && xx < s.width; xx += 4 {
			d.horEdge[d.idx4(xx, y0)] = true
		}
	}
	if s.pcmLoopFilterDisable {
		d.markNoFilter(x0, y0, size)
	}
	return d.cabac.init(r)
}

// transformTree walks the residual quadtree (7.3.8.8).
func (d *sliceDecoder) transformTree(x0, y0, xBase, yBase, log2Size, depth, blkIdx, maxDepth int, parentCbfCb, parentCbfCr bool) error {
	s := d.sps
	intraSplitHere := d.puSize < 1<<d.cuLog2 && depth == 0

	var split bool
	switch {
	case log2Size > s.maxTbLog2Size:
		split = true
	case intraSplitHere:
		split = true
	case log2Size == s.minTbLog2Size || depth >= maxDepth:
		// A forced split (log2 > MaxTb) can leave depth beyond
		// MaxTrafoDepth, so the presence condition is >=, not ==.
		split = false
	default:
		split = d.cabac.decodeBin(d.ctx, ctxSplitTransform+5-log2Size) == 1
	}

	cbfCb, cbfCr := parentCbfCb, parentCbfCr
	if log2Size > 2 {
		if depth == 0 || parentCbfCb {
			cbfCb = d.cabac.decodeBin(d.ctx, ctxCbfChroma+depth) == 1
		} else {
			cbfCb = false
		}
		if depth == 0 || parentCbfCr {
			cbfCr = d.cabac.decodeBin(d.ctx, ctxCbfChroma+depth) == 1
		} else {
			cbfCr = false
		}
	}

	if split {
		half := 1 << (log2Size - 1)
		for i := range 4 {
			x1 := x0 + half*(i&1)
			y1 := y0 + half*(i>>1)
			if err := d.transformTree(x1, y1, x0, y0, log2Size-1, depth+1, i, maxDepth, cbfCb, cbfCr); err != nil {
				return err
			}
		}
		return nil
	}

	ctx := 0
	if depth == 0 {
		ctx = 1
	}
	cbfLuma := d.cabac.decodeBin(d.ctx, ctxCbfLuma+ctx) == 1
	return d.transformUnit(x0, y0, xBase, yBase, log2Size, blkIdx, cbfLuma, cbfCb, cbfCr)
}

// transformUnit decodes residuals and reconstructs one TU (7.3.8.10).
func (d *sliceDecoder) transformUnit(x0, y0, xBase, yBase, log2Size, blkIdx int, cbfLuma, cbfCb, cbfCr bool) error {
	p := d.pps
	chromaHere := log2Size > 2
	chromaLast := log2Size == 2 && blkIdx == 3

	if cbfLuma || cbfCb || cbfCr {
		if p.cuQpDeltaEnabled && !d.cuQpDeltaCoded {
			d.cuQpDeltaCoded = true
			// cu_qp_delta_abs: TR prefix (first bin ctx 0, rest ctx 1)
			// then EG0 suffix, then a bypass sign.
			if d.cabac.decodeBin(d.ctx, ctxCuQpDeltaAbs) == 1 {
				prefix := 1
				for prefix < 5 && d.cabac.decodeBin(d.ctx, ctxCuQpDeltaAbs+1) == 1 {
					prefix++
				}
				val := int32(prefix)
				if prefix == 5 {
					// Exp-Golomb order 0 suffix.
					k := 0
					for d.cabac.decodeBypass() == 1 {
						k++
						if k > 30 {
							return errInvalidStream("cu_qp_delta")
						}
					}
					val = 5 + int32(1)<<k - 1 + int32(d.cabac.decodeBypassBits(k))
				}
				if d.cabac.decodeBypass() == 1 {
					val = -val
				}
				// CuQpDeltaVal range per spec 7.4.9.10.
				qpBdOffsetY := int32(6 * (d.sps.bitDepthLuma - 8))
				if val < -(26+qpBdOffsetY/2) || val > 25+qpBdOffsetY/2 {
					return errInvalidStream("cu_qp_delta value")
				}
				d.cuQpDeltaVal = val
			}
		}
	}

	// Record deblocking edges: every TU boundary aligned to the 8x8 luma
	// grid is a filtered edge in all-intra content (CU and PU boundaries
	// coincide with TU boundaries).
	n := 1 << log2Size
	if x0%8 == 0 && x0 > 0 {
		for yy := y0; yy < y0+n && yy < d.sps.height; yy += 4 {
			d.vertEdge[d.idx4(x0, yy)] = true
		}
	}
	if y0%8 == 0 && y0 > 0 {
		for xx := x0; xx < x0+n && xx < d.sps.width; xx += 4 {
			d.horEdge[d.idx4(xx, y0)] = true
		}
	}
	if d.cuBypass {
		d.markNoFilter(x0, y0, n)
	}

	// The luma QP for this CU becomes final at the first coded TU; setting
	// it per TU is idempotent for the rest.
	qpY := d.deriveQPY()
	d.setCuQP(d.cuX, d.cuY, 1<<d.cuLog2, qpY)

	// Luma.
	lumaMode := d.puModeAt(x0, y0)
	if cbfLuma {
		if err := d.reconstructBlock(x0, y0, log2Size, 0, lumaMode, qpY); err != nil {
			return err
		}
	} else {
		d.predictOnly(x0, y0, log2Size, 0, lumaMode)
	}
	d.markDecoded(x0, y0, 1<<log2Size, 1<<log2Size)

	// Chroma.
	if chromaHere {
		if err := d.reconstructChroma(x0, y0, log2Size-1, cbfCb, cbfCr, qpY); err != nil {
			return err
		}
	} else if chromaLast {
		// At 4x4 luma TUs the chroma cbfs were parsed at the parent level
		// and inherited down; the single 4x4 chroma pair covers the whole
		// parent area.
		if err := d.reconstructChroma(xBase, yBase, log2Size, cbfCb, cbfCr, qpY); err != nil {
			return err
		}
	}
	return nil
}
