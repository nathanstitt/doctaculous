package hevc

import "fmt"

// DecodeFrame decodes one intra still picture from its parameter sets and
// slice NAL units (as extracted from an hvcC sample or an Annex-B stream).
// Slices are decoded sequentially; WPP substreams within a slice are decoded
// in order with the spec's context save/restore.
func DecodeFrame(ps ParamSets, sliceNALs [][]byte) (*Frame, error) {
	rp, err := resolveParamSets(ps)
	if err != nil {
		return nil, err
	}

	var d *sliceDecoder
	for _, raw := range sliceNALs {
		nal, err := parseNAL(raw)
		if err != nil {
			return nil, err
		}
		if !nal.isSlice() {
			continue // SEI etc. interleaved with slices
		}
		if !nal.isIRAP() {
			return nil, fmt.Errorf("%w: non-IRAP slice NAL type %d", ErrUnsupported, nal.typ)
		}
		rbsp, removed := unescapeWithMap(nal.payload)
		// The PPS id lives early in the header; parse the header against
		// it after a peek. Headers are parsed twice cheaply: once to get
		// the PPS id (via a throwaway parse against any PPS), once for
		// real. Fixtures carry a single PPS, so read it directly.
		var hdr *sliceHeader
		var s *sps
		var p *pps
		for id := range rp.pps {
			s, p, err = rp.lookup(id)
			if err != nil {
				return nil, err
			}
			hdr, err = parseSliceHeader(rbsp, nal, s, p)
			if err != nil {
				return nil, err
			}
			break
		}
		if len(rp.pps) > 1 {
			s, p, err = rp.lookup(hdr.ppsID)
			if err != nil {
				return nil, err
			}
			hdr, err = parseSliceHeader(rbsp, nal, s, p)
			if err != nil {
				return nil, err
			}
		}
		if d == nil {
			d = newSliceDecoder(s, p)
		} else if d.sps != s {
			return nil, fmt.Errorf("%w: slices reference different SPS", ErrInvalid)
		}
		if err := d.decodeSliceSegment(hdr, rbsp, removed); err != nil {
			return nil, err
		}
	}
	if d == nil {
		return nil, fmt.Errorf("%w: no slice data", ErrInvalid)
	}
	if !d.complete() {
		return nil, fmt.Errorf("%w: picture is missing coded CTUs", ErrInvalid)
	}
	d.deblockPicture()
	if d.sps.saoEnabled {
		d.applySAO()
	}
	d.frame.applyConformanceWindow(d.sps)
	return d.frame, nil
}

// debugSyntax, when non-nil, receives syntax-level decode events. Set only
// by tests (the M-series divergence-hunting harness); nil in production.
var debugSyntax func(format string, args ...any)

func dbg(format string, args ...any) {
	if debugSyntax != nil {
		debugSyntax(format, args...)
	}
}

// sliceDecoder carries all decoding state for one picture.
type sliceDecoder struct {
	sps   *sps
	pps   *pps
	hdr   *sliceHeader
	frame *Frame

	cabac cabacDecoder
	ctx   []ctxModel

	// Per-4x4-block maps in luma coordinates (width4 x height4).
	width4, height4 int
	decoded         []bool  // reconstruction progress (sample availability)
	intraModes      []uint8 // IntraPredModeY for MPM derivation
	modeSet         []bool  // z-scan parse progress: intra mode recorded.
	// MPM availability follows z-scan (parse) order, not reconstruction:
	// inside an NxN CU the later PUs see the earlier PUs' modes even
	// though no sample of the CU is reconstructed yet.
	ctDepths []uint8 // coding-quadtree depth for split_cu ctx
	qpMap    []int8  // QpY per 4x4 for QP prediction

	ctbSlice []int // slice id (by first segment addr) per CTB, for availability
	tiles    *tileInfo

	// Loop-filter state, populated during parsing and consumed by the
	// picture-wide filter passes after all slices decode.
	vertEdge, horEdge  []bool // TU/CU edges at 4x4 granularity
	noFilter           []bool // PCM (loop filter disabled) or bypass samples
	ctbSao             []saoCtbParams
	ctbDeblockDisabled []bool
	ctbBetaOff         []int32
	ctbTcOff           []int32
	ctbLFAcrossSlice   []bool

	scaling *scalingMatrices // nil = flat

	// Current-CU state.
	cuX, cuY, cuLog2 int
	cuBypass         bool
	puModes          []int
	puSize           int
	chromaMode       int

	// QP state.
	qgX, qgY       int
	cuQpDeltaCoded bool
	cuQpDeltaVal   int32
	qpYPrev        int32
	lastCuQpY      int32
	sliceQP        int32

	// WPP: context saved after the second CTB of each row.
	wppSaved []ctxModel

	sliceID     int
	curTile     int
	ctusDecoded int
}

func newSliceDecoder(s *sps, p *pps) *sliceDecoder {
	d := &sliceDecoder{
		sps:     s,
		pps:     p,
		frame:   newFrame(s),
		width4:  (s.width + 3) / 4,
		height4: (s.height + 3) / 4,
	}
	d.decoded = make([]bool, d.width4*d.height4)
	d.intraModes = make([]uint8, d.width4*d.height4)
	d.modeSet = make([]bool, d.width4*d.height4)
	d.ctDepths = make([]uint8, d.width4*d.height4)
	d.qpMap = make([]int8, d.width4*d.height4)
	d.ctbSlice = make([]int, s.picSizeCtbs)
	for i := range d.ctbSlice {
		d.ctbSlice[i] = -1
	}
	d.tiles = buildTileInfo(s, p)
	d.vertEdge = make([]bool, d.width4*d.height4)
	d.horEdge = make([]bool, d.width4*d.height4)
	d.noFilter = make([]bool, d.width4*d.height4)
	d.ctbSao = make([]saoCtbParams, s.picSizeCtbs)
	d.ctbDeblockDisabled = make([]bool, s.picSizeCtbs)
	d.ctbBetaOff = make([]int32, s.picSizeCtbs)
	d.ctbTcOff = make([]int32, s.picSizeCtbs)
	d.ctbLFAcrossSlice = make([]bool, s.picSizeCtbs)
	var err error
	if s.scalingListEnabled {
		sl := s.scalingList
		if p.scalingListPresent {
			sl = p.scalingList
		}
		d.scaling, err = materializeScalingLists(sl)
		if err != nil {
			d.scaling = nil
		}
	}
	return d
}

// decodeSliceSegment decodes one slice segment's CTUs.
func (d *sliceDecoder) decodeSliceSegment(hdr *sliceHeader, rbsp []byte, removed []int) error {
	s, p := d.sps, d.pps
	if !hdr.dependent {
		d.sliceID++
		d.sliceQP = hdr.sliceQP
		d.qpYPrev = hdr.sliceQP
		d.lastCuQpY = hdr.sliceQP
		d.wppSaved = nil // WPP snapshots never cross slice boundaries
	}
	d.hdr = hdr

	// Substream boundaries in RBSP bytes: entry offsets count escaped
	// bytes starting at the first byte of slice data.
	dataStartEsc := rbspToEscaped(hdr.dataOffset, removed)
	bounds := []int{hdr.dataOffset}
	off := dataStartEsc
	for _, sz := range hdr.entryPointOffsets {
		off += int(sz)
		b := escapedToRBSP(off, removed)
		if b > len(rbsp) {
			return errInvalidStream("entry point offsets")
		}
		bounds = append(bounds, b)
	}
	bounds = append(bounds, len(rbsp))
	substream := func(k int) []byte {
		if k+1 >= len(bounds) {
			return nil
		}
		return rbsp[bounds[k]:bounds[k+1]]
	}

	if !hdr.dependent {
		d.ctx = newCtxModels(hdr.sliceQP)
	} else if d.ctx == nil {
		return errInvalidStream("dependent slice segment without predecessor")
	}

	if p.entropyCodingSync && p.tilesEnabled {
		return errInvalidStream("WPP combined with tiles")
	}
	ctbTs := d.tiles.rsToTs[hdr.segmentAddress]
	ctbAddr := hdr.segmentAddress
	firstRow := ctbAddr / s.picWidthCtbs
	sub := 0
	r := newBitReader(substream(sub), "slice data")
	if err := d.cabac.init(r); err != nil {
		return err
	}

	for {
		ctbAddr = d.tiles.tsToRs[ctbTs]
		col := ctbAddr % s.picWidthCtbs
		row := ctbAddr / s.picWidthCtbs
		if row >= s.picHeightCtbs {
			return errInvalidStream("CTU address beyond picture")
		}

		if p.tilesEnabled && ctbTs > d.tiles.rsToTs[hdr.segmentAddress] &&
			d.tileID(ctbAddr) != d.tileID(d.tiles.tsToRs[ctbTs-1]) {
			// Tile start: next substream, fresh contexts, QP predictor
			// reset (spec 9.3.1).
			sub++
			r = newBitReader(substream(sub), "tile substream")
			if r.b == nil {
				return errInvalidStream("missing tile substream")
			}
			if err := d.cabac.init(r); err != nil {
				return err
			}
			d.ctx = newCtxModels(d.sliceQP)
			d.qpYPrev = d.sliceQP
			d.lastCuQpY = d.sliceQP
		}

		if p.entropyCodingSync && col == 0 && ctbAddr != hdr.segmentAddress {
			// New WPP row: next substream, context from the saved
			// snapshot when the above-right CTB exists, else slice-init.
			sub++
			r = newBitReader(substream(sub), "WPP substream")
			if r.b == nil {
				return errInvalidStream("missing WPP substream")
			}
			if err := d.cabac.init(r); err != nil {
				return err
			}
			if s.picWidthCtbs > 1 && d.wppSaved != nil && row > firstRow {
				d.ctx = snapshotCtx(d.wppSaved)
			} else {
				d.ctx = newCtxModels(d.sliceQP)
			}
			d.qpYPrev = d.sliceQP
			d.lastCuQpY = d.sliceQP
		}

		d.ctbSlice[ctbAddr] = d.sliceID
		d.curTile = d.tileID(ctbAddr)
		d.ctbDeblockDisabled[ctbAddr] = hdr.deblockDisabled
		d.ctbBetaOff[ctbAddr] = hdr.betaOffsetDiv2
		d.ctbTcOff[ctbAddr] = hdr.tcOffsetDiv2
		d.ctbLFAcrossSlice[ctbAddr] = hdr.loopFilterAcrossSlices
		if s.saoEnabled && (hdr.saoLuma || hdr.saoChroma) {
			d.parseSAO(ctbAddr, col, row)
		}
		x0 := col << s.ctbLog2SizeY
		y0 := row << s.ctbLog2SizeY
		// Force a fresh quantization group at the CTU boundary.
		d.qgX, d.qgY = -1, -1
		if err := d.codingQuadtree(x0, y0, s.ctbLog2SizeY, 0); err != nil {
			return err
		}
		d.ctusDecoded++

		if p.entropyCodingSync && col == 1 {
			d.wppSaved = snapshotCtx(d.ctx)
		}

		end := d.cabac.decodeTerminate()
		if d.cabac.failed() {
			return errInvalidStream("slice data")
		}
		if end == 1 {
			dbg("end_of_slice at ctb %d: bitpos %d of %d", ctbAddr, d.cabac.r.pos, len(d.cabac.r.b)*8)
			return nil
		}
		if ctbTs == s.picSizeCtbs-1 {
			dbg("terminate=0 at last ctb: bitpos %d of %d", d.cabac.r.pos, len(d.cabac.r.b)*8)
			return errInvalidStream("missing end_of_slice flag")
		}
		if p.entropyCodingSync && col == s.picWidthCtbs-1 {
			// end_of_subset_one_bit: terminates this substream.
			if d.cabac.decodeTerminate() != 1 {
				return errInvalidStream("end of WPP substream")
			}
		}
		if p.tilesEnabled && d.tileID(ctbAddr) != d.tileID(d.tiles.tsToRs[ctbTs+1]) {
			if d.cabac.decodeTerminate() != 1 {
				return errInvalidStream("end of tile substream")
			}
		}
		ctbTs++
	}
}

// complete reports whether every CTB was covered by some slice.
func (d *sliceDecoder) complete() bool {
	for _, s := range d.ctbSlice {
		if s < 0 {
			return false
		}
	}
	return true
}

// --- neighbor maps -------------------------------------------------------

func (d *sliceDecoder) idx4(x, y int) int { return (y>>2)*d.width4 + x>>2 }

// availableAt reports whether the sample at luma position (x, y) is
// decodable context: inside the picture, already reconstructed, and in the
// same slice as the current CTB region (spec 6.4.1, no tiles).
func (d *sliceDecoder) availableAt(x, y int) bool {
	if x < 0 || y < 0 || x >= d.sps.width || y >= d.sps.height {
		return false
	}
	if !d.decoded[d.idx4(x, y)] {
		return false
	}
	ctb := (y>>d.sps.ctbLog2SizeY)*d.sps.picWidthCtbs + x>>d.sps.ctbLog2SizeY
	return d.ctbSlice[ctb] == d.sliceID && d.tileID(ctb) == d.curTile
}

// curTile is maintained by the CTU loop so availability checks can compare
// tiles without recomputing the current CTB address.

// markNoFilter exempts a CU's samples from the in-loop filters.
func (d *sliceDecoder) markNoFilter(x, y, size int) {
	for yy := y; yy < y+size; yy += 4 {
		for xx := x; xx < x+size; xx += 4 {
			if xx < d.sps.width && yy < d.sps.height {
				d.noFilter[d.idx4(xx, yy)] = true
			}
		}
	}
}

func (d *sliceDecoder) markDecoded(x, y, w, h int) {
	for yy := y; yy < y+h; yy += 4 {
		for xx := x; xx < x+w; xx += 4 {
			if xx < d.sps.width && yy < d.sps.height {
				d.decoded[d.idx4(xx, yy)] = true
			}
		}
	}
}

func (d *sliceDecoder) markIntraMode(x, y, size, mode int) {
	for yy := y; yy < y+size; yy += 4 {
		for xx := x; xx < x+size; xx += 4 {
			if xx < d.sps.width && yy < d.sps.height {
				d.intraModes[d.idx4(xx, yy)] = uint8(mode)
				d.modeSet[d.idx4(xx, yy)] = true
			}
		}
	}
}

// modeAvailableAt reports z-scan (parse-order) availability of the block
// covering luma position (x, y) for intra-mode prediction (spec 6.4.1).
func (d *sliceDecoder) modeAvailableAt(x, y int) bool {
	if x < 0 || y < 0 || x >= d.sps.width || y >= d.sps.height {
		return false
	}
	if !d.modeSet[d.idx4(x, y)] {
		return false
	}
	ctb := (y>>d.sps.ctbLog2SizeY)*d.sps.picWidthCtbs + x>>d.sps.ctbLog2SizeY
	return d.ctbSlice[ctb] == d.sliceID && d.tileID(ctb) == d.curTile
}

func (d *sliceDecoder) intraMode4x4(x, y int) int { return int(d.intraModes[d.idx4(x, y)]) }

func (d *sliceDecoder) markCtDepth(x, y, size, depth int) {
	for yy := y; yy < y+size; yy += 4 {
		for xx := x; xx < x+size; xx += 4 {
			if xx < d.sps.width && yy < d.sps.height {
				d.ctDepths[d.idx4(xx, yy)] = uint8(depth)
			}
		}
	}
}

func (d *sliceDecoder) ctDepth4x4(x, y int) int { return int(d.ctDepths[d.idx4(x, y)]) }

// puModeAt returns the luma intra mode of the PU covering (x, y) in the
// current CU.
func (d *sliceDecoder) puModeAt(x, y int) int {
	if len(d.puModes) == 1 {
		return d.puModes[0]
	}
	i := 0
	if x-d.cuX >= d.puSize {
		i |= 1
	}
	if y-d.cuY >= d.puSize {
		i |= 2
	}
	return d.puModes[i]
}

// --- QP derivation (spec 8.6.1) ------------------------------------------

// predictQP returns qPY_PRED for the current quantization group.
func (d *sliceDecoder) predictQP() int32 {
	s := d.sps
	sameCtb := func(x, y int) bool {
		return x>>s.ctbLog2SizeY == d.qgX>>s.ctbLog2SizeY &&
			y>>s.ctbLog2SizeY == d.qgY>>s.ctbLog2SizeY
	}
	qpA := d.qpYPrev
	if d.qgX > 0 && d.availableAt(d.qgX-1, d.qgY) && sameCtb(d.qgX-1, d.qgY) {
		qpA = int32(d.qpMap[d.idx4(d.qgX-1, d.qgY)])
	}
	qpB := d.qpYPrev
	if d.qgY > 0 && d.availableAt(d.qgX, d.qgY-1) && sameCtb(d.qgX, d.qgY-1) {
		qpB = int32(d.qpMap[d.idx4(d.qgX, d.qgY-1)])
	}
	return (qpA + qpB + 1) >> 1
}

// deriveQPY resolves the current CU's final luma QP.
func (d *sliceDecoder) deriveQPY() int32 {
	if !d.pps.cuQpDeltaEnabled {
		return d.sliceQP
	}
	qpBdOffset := int32(6 * (d.sps.bitDepthLuma - 8))
	pred := d.predictQP()
	// Floored modulo: the sum stays non-negative for spec-conformant
	// inputs, but a defensive floor keeps Qp' >= 0 for any parsed values.
	m := 52 + qpBdOffset
	v := (pred + d.cuQpDeltaVal + 52 + 2*qpBdOffset) % m
	if v < 0 {
		v += m
	}
	return v - qpBdOffset
}

// setCuQP records a CU's QpY in the map and as the running predictor.
func (d *sliceDecoder) setCuQP(x, y, size int, qp int32) {
	for yy := y; yy < y+size; yy += 4 {
		for xx := x; xx < x+size; xx += 4 {
			if xx < d.sps.width && yy < d.sps.height {
				d.qpMap[d.idx4(xx, yy)] = int8(qp)
			}
		}
	}
	d.lastCuQpY = qp
}

// --- reconstruction ------------------------------------------------------

// scanForMode selects the coefficient scan (7.4.9.11): mode-dependent for
// 4x4 and luma 8x8 intra blocks.
func scanForMode(mode, log2Size, cIdx int) int {
	if log2Size == 2 || (log2Size == 3 && cIdx == 0) {
		if mode >= 6 && mode <= 14 {
			return 2 // vertical
		}
		if mode >= 22 && mode <= 30 {
			return 1 // horizontal
		}
	}
	return 0
}

// predictInto runs intra prediction for one block into dst.
func (d *sliceDecoder) predictInto(dst []int32, plane []uint16, stride, x, y, n, mode, cIdx int) {
	s := d.sps
	scale := 1
	if cIdx > 0 {
		scale = 2
	}
	avail := func(sx, sy int) bool { return d.availableAt(sx*scale, sy*scale) }
	bd := s.bitDepthLuma
	if cIdx > 0 {
		bd = s.bitDepthChroma
	}
	refs := gatherRefSamples(plane, stride, x, y, n, bd, avail)
	if cIdx == 0 {
		refs = filterRefSamples(refs, mode, n, bd, s.strongIntraSmoothing)
	}
	predictIntra(dst, refs, mode, n, bd, cIdx == 0)
}

// reconstructBlock decodes residuals for one luma TB and writes
// prediction+residual.
func (d *sliceDecoder) reconstructBlock(x, y, log2Size, cIdx, mode int, qpY int32) error {
	n := 1 << log2Size
	scanIdx := scanForMode(mode, log2Size, cIdx)
	block := make([]int32, n*n)
	tskip, err := d.decodeResidual(block, log2Size, cIdx, scanIdx, d.cuBypass)
	if err != nil {
		return err
	}
	// Dequantization uses Qp'Y = QpY + QpBdOffsetY (spec 8.6.1).
	qp := qpY + int32(6*(d.sps.bitDepthLuma-8))
	d.finishBlock(d.frame.Y, d.frame.YStride, x, y, log2Size, cIdx, mode, qp, block, tskip)
	return nil
}

// finishBlock dequantizes/transforms residual levels and adds prediction.
func (d *sliceDecoder) finishBlock(plane []uint16, stride, x, y, log2Size, cIdx, mode int, qp int32, block []int32, tskip bool) {
	s := d.sps
	n := 1 << log2Size
	bd := s.bitDepthLuma
	if cIdx > 0 {
		bd = s.bitDepthChroma
	}

	if !d.cuBypass {
		if d.scaling != nil {
			// Intra matrix IDs: 0=Y, 1=Cb, 2=Cr.
			d.applyDequant(block, log2Size, cIdx, qp, bd)
		} else {
			dequant(block, nil, qp, log2Size, bd)
		}
		if tskip {
			rescaleTransformSkip(block, n, bd)
		} else {
			useDST := cIdx == 0 && log2Size == 2
			inverseTransform(block, n, bd, useDST)
		}
	}

	pred := make([]int32, n*n)
	d.predictInto(pred, plane, stride, x, y, n, mode, cIdx)
	maxVal := int32(1)<<bd - 1
	for yy := range n {
		rowOff := (y + yy) * stride
		for xx := range n {
			v := clip3(0, maxVal, pred[yy*n+xx]+block[yy*n+xx])
			plane[rowOff+x+xx] = uint16(v)
		}
	}
}

// applyDequant runs dequant with the materialized scaling matrix.
func (d *sliceDecoder) applyDequant(block []int32, log2Size, matrixID int, qp int32, bd int) {
	sizeID := log2Size - 2
	m := d.scaling.m[sizeID][matrixID]
	if sizeID == 3 && m == nil {
		m = d.scaling.m[3][0]
	}
	dequant(block, m, qp, log2Size, bd)
}

// predictOnly writes pure prediction for a TB with no coded residual.
func (d *sliceDecoder) predictOnly(x, y, log2Size, cIdx, mode int) {
	n := 1 << log2Size
	plane, stride := d.frame.Y, d.frame.YStride
	switch cIdx {
	case 1:
		plane, stride = d.frame.Cb, d.frame.CStride
	case 2:
		plane, stride = d.frame.Cr, d.frame.CStride
	}
	pred := make([]int32, n*n)
	d.predictInto(pred, plane, stride, x, y, n, mode, cIdx)
	for yy := range n {
		rowOff := (y + yy) * stride
		for xx := range n {
			plane[rowOff+x+xx] = uint16(pred[yy*n+xx])
		}
	}
}

// reconstructChroma handles both chroma TBs at the given luma position and
// chroma log2 size.
func (d *sliceDecoder) reconstructChroma(lumaX, lumaY, log2Chroma int, cbfCb, cbfCr bool, qpY int32) error {
	s, p := d.sps, d.pps
	cx, cy := lumaX>>1, lumaY>>1
	qpBdOffsetC := int32(6 * (s.bitDepthChroma - 8))
	qpFor := func(ppsOff, sliceOff int32) int32 {
		qpi := clip3(-qpBdOffsetC, 57, qpY+ppsOff+sliceOff)
		var qpc int32
		if qpi < 0 {
			qpc = qpi
		} else {
			qpc = chromaQPMapping(qpi)
		}
		return qpc + qpBdOffsetC
	}
	qpCb := qpFor(p.cbQpOffset, d.hdr.cbQpOffset)
	qpCr := qpFor(p.crQpOffset, d.hdr.crQpOffset)

	mode := d.chromaMode
	scanIdx := 0
	if log2Chroma == 2 {
		scanIdx = scanForMode(mode, 2, 1)
	}

	do := func(cIdx int, cbf bool, qp int32, plane []uint16) error {
		if !cbf {
			d.predictOnly(cx, cy, log2Chroma, cIdx, mode)
			return nil
		}
		n := 1 << log2Chroma
		block := make([]int32, n*n)
		tskip, err := d.decodeResidual(block, log2Chroma, cIdx, scanIdx, d.cuBypass)
		if err != nil {
			return err
		}
		stride := d.frame.CStride
		d.finishBlock(plane, stride, cx, cy, log2Chroma, cIdx, mode, qp, block, tskip)
		return nil
	}
	if err := do(1, cbfCb, qpCb, d.frame.Cb); err != nil {
		return err
	}
	return do(2, cbfCr, qpCr, d.frame.Cr)
}
