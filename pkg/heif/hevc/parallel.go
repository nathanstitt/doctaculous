package hevc

import (
	"fmt"
	"runtime"
	"sync"
)

// Parallel decoding. Correctness contract: output is byte-identical to the
// sequential path for any worker count (verified by tests). Three levels:
//
//   - WPP streams decode CTB rows concurrently with the standard wavefront
//     lag (row r waits until row r-1 has finished column c+2, which also
//     provides the context-adoption snapshot and orders all cross-row reads
//     through the scheduler's mutex).
//   - Tile streams decode tiles concurrently: CABAC, prediction, and every
//     shared-map region are tile-disjoint by construction.
//   - The loop filters and SAO partition into disjoint bands per pass.
//
// The single-substream, no-tile shape has inherently serial CABAC; it uses
// the sequential path (its filters still parallelize).

// DecodeOptions configures DecodeFrameOpts.
type DecodeOptions struct {
	// Workers bounds the goroutines used for one picture. 0 means
	// runtime.GOMAXPROCS(0); 1 forces fully sequential decoding.
	Workers int
}

type sliceData struct {
	hdr     *sliceHeader
	rbsp    []byte
	removed []int
}

// DecodeFrameOpts is DecodeFrame with explicit options.
func DecodeFrameOpts(ps ParamSets, sliceNALs [][]byte, opts *DecodeOptions) (*Frame, error) {
	workers := 0
	if opts != nil {
		workers = opts.Workers
	}
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}

	rp, err := resolveParamSets(ps)
	if err != nil {
		return nil, err
	}

	var d *sliceDecoder
	var slices []sliceData
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
		// The PPS id lives early in the header: parse against the first
		// PPS to learn it, re-parse against the right pair if several
		// exist (real files carry exactly one).
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
		slices = append(slices, sliceData{hdr: hdr, rbsp: rbsp, removed: removed})
	}
	if d == nil {
		return nil, fmt.Errorf("%w: no slice data", ErrInvalid)
	}

	switch {
	case workers > 1 && d.canParallelWPP(slices):
		err = d.decodeWPPParallel(slices[0], workers)
	case workers > 1 && d.canParallelTiles(slices):
		err = d.decodeTilesParallel(slices[0], workers)
	default:
		for _, sl := range slices {
			if err = d.decodeSliceSegment(sl.hdr, sl.rbsp, sl.removed); err != nil {
				break
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if !d.complete() {
		return nil, fmt.Errorf("%w: picture is missing coded CTUs", ErrInvalid)
	}

	if workers > 1 {
		d.deblockPassParallel(true, workers)
		d.deblockPassParallel(false, workers)
	} else {
		d.deblockPicture()
	}
	if d.sps.saoEnabled {
		d.applySAO(workers)
	}
	d.frame.applyConformanceWindow(d.sps)
	return d.frame, nil
}

// canParallelWPP: one independent whole-picture slice with WPP and at least
// two CTB rows and columns (narrow pictures have no wavefront to exploit).
func (d *sliceDecoder) canParallelWPP(slices []sliceData) bool {
	return len(slices) == 1 && !slices[0].hdr.dependent && slices[0].hdr.segmentAddress == 0 &&
		d.pps.entropyCodingSync && !d.pps.tilesEnabled &&
		d.sps.picHeightCtbs > 1 && d.sps.picWidthCtbs > 1 &&
		len(slices[0].hdr.entryPointOffsets) == d.sps.picHeightCtbs-1
}

// canParallelTiles: one independent whole-picture slice with multiple tiles.
func (d *sliceDecoder) canParallelTiles(slices []sliceData) bool {
	return len(slices) == 1 && !slices[0].hdr.dependent && slices[0].hdr.segmentAddress == 0 &&
		d.pps.tilesEnabled && !d.pps.entropyCodingSync && d.tiles.numTiles > 1 &&
		len(slices[0].hdr.entryPointOffsets) == d.tiles.numTiles-1
}

// clone duplicates the decoder's per-worker mutable state while sharing the
// picture-wide maps and planes.
func (d *sliceDecoder) clone() *sliceDecoder {
	c := *d
	c.cabac = cabacDecoder{}
	c.ctx = nil
	c.wppSaved = nil
	return &c
}

// substreamBounds computes the RBSP byte ranges of each substream from the
// slice header's entry points (offsets count escaped bytes).
func substreamBounds(hdr *sliceHeader, rbsp []byte, removed []int) ([]int, error) {
	bounds := []int{hdr.dataOffset}
	off := rbspToEscaped(hdr.dataOffset, removed)
	for _, sz := range hdr.entryPointOffsets {
		off += int(sz)
		b := escapedToRBSP(off, removed)
		if b > len(rbsp) {
			return nil, errInvalidStream("entry point offsets")
		}
		bounds = append(bounds, b)
	}
	return append(bounds, len(rbsp)), nil
}

// prepWorker initializes a cloned decoder for one substream.
func (w *sliceDecoder) prepWorker(hdr *sliceHeader, sub []byte) error {
	w.hdr = hdr
	w.sliceID = 1
	w.sliceQP = hdr.sliceQP
	w.qpYPrev = hdr.sliceQP
	w.lastCuQpY = hdr.sliceQP
	return w.cabac.init(newBitReader(sub, "substream"))
}

// decodeOneCtb runs the shared per-CTB work (bookkeeping, SAO syntax, the
// coding quadtree) and decodes the end_of_slice_segment_flag.
func (w *sliceDecoder) decodeOneCtb(ctbAddr int) (end uint32, err error) {
	s := w.sps
	col := ctbAddr % s.picWidthCtbs
	row := ctbAddr / s.picWidthCtbs
	w.ctbSlice[ctbAddr] = w.sliceID
	w.curTile = w.tileID(ctbAddr)
	w.curCtbAddr = ctbAddr
	w.ctbDeblockDisabled[ctbAddr] = w.hdr.deblockDisabled
	w.ctbBetaOff[ctbAddr] = w.hdr.betaOffsetDiv2
	w.ctbTcOff[ctbAddr] = w.hdr.tcOffsetDiv2
	w.ctbLFAcrossSlice[ctbAddr] = w.hdr.loopFilterAcrossSlices
	if s.saoEnabled && (w.hdr.saoLuma || w.hdr.saoChroma) {
		w.parseSAO(ctbAddr, col, row)
	}
	w.qgX, w.qgY = -1, -1
	if err := w.codingQuadtree(col<<s.ctbLog2SizeY, row<<s.ctbLog2SizeY, s.ctbLog2SizeY, 0); err != nil {
		return 0, err
	}
	end = w.cabac.decodeTerminate()
	if w.cabac.failed() {
		return 0, errInvalidStream("slice data")
	}
	return end, nil
}

// decodeTilesParallel decodes each tile substream on its own goroutine.
func (d *sliceDecoder) decodeTilesParallel(sl sliceData, workers int) error {
	s := d.sps
	bounds, err := substreamBounds(sl.hdr, sl.rbsp, sl.removed)
	if err != nil {
		return err
	}
	d.sliceID = 1 // all workers share one slice

	sem := make(chan struct{}, workers)
	errs := make([]error, d.tiles.numTiles)
	var wg sync.WaitGroup
	tileStart := 0
	for t := range d.tiles.numTiles {
		// Tile t covers a contiguous tile-scan range.
		count := 0
		for ts := tileStart; ts < s.picSizeCtbs && d.tiles.tileOf[d.tiles.tsToRs[ts]] == t; ts++ {
			count++
		}
		startTs, endTs := tileStart, tileStart+count
		tileStart += count

		wg.Add(1)
		sem <- struct{}{}
		go func(t, startTs, endTs int) {
			defer wg.Done()
			defer func() { <-sem }()
			w := d.clone()
			if err := w.prepWorker(sl.hdr, sl.rbsp[bounds[t]:bounds[t+1]]); err != nil {
				errs[t] = err
				return
			}
			w.ctx = newCtxModels(sl.hdr.sliceQP)
			for ts := startTs; ts < endTs; ts++ {
				end, err := w.decodeOneCtb(d.tiles.tsToRs[ts])
				if err != nil {
					errs[t] = err
					return
				}
				last := ts == endTs-1
				finalCtb := last && t == d.tiles.numTiles-1
				switch {
				case end == 1 && !finalCtb:
					errs[t] = errInvalidStream("early end_of_slice in tile")
					return
				case end == 0 && finalCtb:
					errs[t] = errInvalidStream("missing end_of_slice flag")
					return
				case end == 0 && last:
					if w.cabac.decodeTerminate() != 1 {
						errs[t] = errInvalidStream("end of tile substream")
						return
					}
				}
			}
		}(t, startTs, endTs)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// wppScheduler tracks per-row progress for the wavefront dependency (row r
// may decode column c only after row r-1 finished column c+2). The mutex
// also establishes the happens-before edges for all cross-row map reads.
type wppScheduler struct {
	mu       sync.Mutex
	cond     *sync.Cond
	progress []int // columns completed per row
	failed   bool
}

func (ws *wppScheduler) waitFor(row, cols int) bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	for ws.progress[row] < cols && !ws.failed {
		ws.cond.Wait()
	}
	return !ws.failed
}

func (ws *wppScheduler) advance(row int) {
	ws.mu.Lock()
	ws.progress[row]++
	ws.cond.Broadcast()
	ws.mu.Unlock()
}

func (ws *wppScheduler) fail() {
	ws.mu.Lock()
	ws.failed = true
	ws.cond.Broadcast()
	ws.mu.Unlock()
}

// decodeWPPParallel decodes each CTB row on its own goroutine with the
// wavefront lag.
func (d *sliceDecoder) decodeWPPParallel(sl sliceData, workers int) error {
	s := d.sps
	bounds, err := substreamBounds(sl.hdr, sl.rbsp, sl.removed)
	if err != nil {
		return err
	}
	d.sliceID = 1

	ws := &wppScheduler{progress: make([]int, s.picHeightCtbs)}
	ws.cond = sync.NewCond(&ws.mu)
	// ctxChans[r] delivers the adopted context snapshot to row r (taken by
	// row r-1 after its second CTB).
	ctxChans := make([]chan []ctxModel, s.picHeightCtbs)
	for i := range ctxChans {
		ctxChans[i] = make(chan []ctxModel, 1)
	}

	sem := make(chan struct{}, workers)
	errs := make([]error, s.picHeightCtbs)
	var wg sync.WaitGroup
	for row := range s.picHeightCtbs {
		wg.Add(1)
		sem <- struct{}{}
		go func(row int) {
			defer wg.Done()
			defer func() { <-sem }()
			w := d.clone()
			if err := w.prepWorker(sl.hdr, sl.rbsp[bounds[row]:bounds[row+1]]); err != nil {
				errs[row] = err
				ws.fail()
				return
			}
			if row == 0 {
				w.ctx = newCtxModels(sl.hdr.sliceQP)
			} else {
				// Wait for the snapshot; a scheduler failure closes it.
				if !ws.waitFor(row-1, 2) {
					return
				}
				w.ctx = snapshotCtx(<-ctxChans[row])
			}
			for col := range s.picWidthCtbs {
				if row > 0 && !ws.waitFor(row-1, min(col+2, s.picWidthCtbs)) {
					return
				}
				ctbAddr := row*s.picWidthCtbs + col
				end, err := w.decodeOneCtb(ctbAddr)
				if err != nil {
					errs[row] = err
					ws.fail()
					return
				}
				if col == 1 && row+1 < s.picHeightCtbs {
					ctxChans[row+1] <- snapshotCtx(w.ctx)
				}
				ws.advance(row)
				lastCtb := ctbAddr == s.picSizeCtbs-1
				switch {
				case end == 1 && !lastCtb:
					errs[row] = errInvalidStream("early end_of_slice in WPP row")
					ws.fail()
					return
				case end == 0 && lastCtb:
					errs[row] = errInvalidStream("missing end_of_slice flag")
					ws.fail()
					return
				case end == 0 && col == s.picWidthCtbs-1:
					if w.cabac.decodeTerminate() != 1 {
						errs[row] = errInvalidStream("end of WPP substream")
						ws.fail()
						return
					}
				}
			}
		}(row)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// deblockPassParallel splits one deblocking pass into disjoint 8-aligned
// row bands. Vertical-edge segments only touch their own 4 rows; horizontal
// edges on the 8-grid write rows y-3..y+2, and edges are 8 apart, so
// 8-aligned bands never overlap.
func (d *sliceDecoder) deblockPassParallel(vertical bool, workers int) {
	s := d.sps
	bands := workers
	rows := (s.height + 7) / 8
	if bands > rows {
		bands = rows
	}
	var wg sync.WaitGroup
	for b := range bands {
		y0 := b * rows / bands * 8
		y1 := (b + 1) * rows / bands * 8
		if y1 > s.height {
			y1 = s.height
		}
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			d.deblockRange(vertical, y0, y1)
		}(y0, y1)
	}
	wg.Wait()
}
