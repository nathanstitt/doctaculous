package hevc

import (
	"fmt"
	"math/bits"
)

// Slice segment header (spec 7.3.6.1), restricted to I slices: any P or B
// slice type is rejected before pixel work, so the reference-picture and
// prediction-weight branches only ever consume bits for CRA headers' unused
// RPS signaling.
type sliceHeader struct {
	firstSlice     bool
	dependent      bool
	segmentAddress int // CTB address in raster scan
	ppsID          uint32

	saoLuma   bool
	saoChroma bool

	sliceQP    int32
	cbQpOffset int32 // slice-level extra offset
	crQpOffset int32

	deblockDisabled        bool
	betaOffsetDiv2         int32
	tcOffsetDiv2           int32
	loopFilterAcrossSlices bool

	entryPointOffsets []uint32 // byte offsets between substreams (already +1 adjusted)

	dataOffset int // byte offset of slice data (CABAC start) within the RBSP
}

const sliceTypeI = 2

// parseSliceHeader parses the header of a coded slice segment NAL.
func parseSliceHeader(rbsp []byte, nal nalUnit, s *sps, p *pps) (*sliceHeader, error) {
	r := newBitReader(rbsp, "slice header")
	h := &sliceHeader{loopFilterAcrossSlices: p.loopFilterAcrossSlices}
	h.firstSlice = r.flag()
	if nal.isIRAP() {
		r.flag() // no_output_of_prior_pics_flag
	}
	h.ppsID = r.ue()
	if !h.firstSlice {
		if p.dependentSliceSegments {
			h.dependent = r.flag()
		}
		addrBits := ceilLog2(s.picSizeCtbs)
		h.segmentAddress = int(r.u(addrBits))
		if h.segmentAddress >= s.picSizeCtbs {
			return nil, fmt.Errorf("%w: slice segment address %d", ErrInvalid, h.segmentAddress)
		}
	}

	// Defaults inherited from the PPS; a dependent segment also inherits
	// everything below from the preceding independent segment (the decoder
	// applies that; here the fields just keep their zero values).
	h.deblockDisabled = p.deblockDisabled
	h.betaOffsetDiv2 = p.betaOffsetDiv2
	h.tcOffsetDiv2 = p.tcOffsetDiv2

	if !h.dependent {
		for range p.numExtraSliceHeaderBits {
			r.flag() // slice_reserved_flag
		}
		sliceType := r.ue()
		if sliceType != sliceTypeI {
			return nil, fmt.Errorf("%w: slice type %d (intra-only decoder)", ErrUnsupported, sliceType)
		}
		if p.outputFlagPresent {
			r.flag() // pic_output_flag
		}
		if !nal.isIDR() {
			r.u(s.maxPocLsbBits) // slice_pic_order_cnt_lsb
			if !r.flag() {       // short_term_ref_pic_set_sps_flag
				// A slice-local set (index == num sets) may predict
				// from the SPS sets, so the counts matter.
				deltaPocs := make([]int, s.numShortTermRPS+1)
				copy(deltaPocs, s.stRPSDeltaPocs)
				if err := parseShortTermRPS(r, int(s.numShortTermRPS), int(s.numShortTermRPS), deltaPocs); err != nil {
					return nil, err
				}
			} else if s.numShortTermRPS > 1 {
				r.u(ceilLog2(int(s.numShortTermRPS)))
			}
			if s.longTermPresent {
				numSPS := uint32(0)
				if s.numLongTermSPS > 0 {
					numSPS = r.ue()
				}
				numPics := r.ue()
				if numSPS+numPics > 64 {
					return nil, fmt.Errorf("%w: long-term picture count", ErrInvalid)
				}
				for i := uint32(0); i < numSPS+numPics; i++ {
					if i < numSPS {
						if s.numLongTermSPS > 1 {
							r.u(ceilLog2(int(s.numLongTermSPS)))
						}
					} else {
						r.u(s.maxPocLsbBits) // poc_lsb_lt
						r.flag()             // used_by_curr_pic_lt_flag
					}
					if r.flag() { // delta_poc_msb_present_flag
						r.ue()
					}
				}
			}
			if s.temporalMvpEnabled {
				r.flag() // slice_temporal_mvp_enabled_flag
			}
		}
		if s.saoEnabled {
			h.saoLuma = r.flag()
			h.saoChroma = r.flag()
		}
		// P/B-only syntax (ref lists, weights, merge candidates) cannot
		// appear: slice_type is I.
		h.sliceQP = p.initQP + r.se()
		if h.sliceQP < -12 || h.sliceQP > 51 {
			return nil, fmt.Errorf("%w: slice QP %d", ErrInvalid, h.sliceQP)
		}
		if p.sliceChromaQpOffsets {
			h.cbQpOffset = r.se()
			h.crQpOffset = r.se()
		}
		if p.deblockControlPresent {
			override := false
			if p.deblockOverrideEnabled {
				override = r.flag()
			}
			if override {
				h.deblockDisabled = r.flag()
				if !h.deblockDisabled {
					h.betaOffsetDiv2 = r.se()
					h.tcOffsetDiv2 = r.se()
				}
			}
		}
		if p.loopFilterAcrossSlices && (h.saoLuma || h.saoChroma || !h.deblockDisabled) {
			h.loopFilterAcrossSlices = r.flag()
		}
	}

	if p.tilesEnabled || p.entropyCodingSync {
		n := r.ue()
		if n > uint32(s.picSizeCtbs) {
			return nil, fmt.Errorf("%w: %d entry points", ErrInvalid, n)
		}
		if n > 0 {
			lenBits := int(r.ue()) + 1
			if lenBits > 32 {
				return nil, fmt.Errorf("%w: entry point offset length %d", ErrInvalid, lenBits)
			}
			h.entryPointOffsets = make([]uint32, n)
			for i := range h.entryPointOffsets {
				h.entryPointOffsets[i] = r.u(lenBits) + 1
			}
		}
	}
	if p.sliceHeaderExtension {
		n := r.ue()
		if n > 256 {
			return nil, fmt.Errorf("%w: slice header extension length", ErrInvalid)
		}
		for range n {
			r.u(8)
		}
	}
	// byte_alignment(): alignment_bit_equal_to_one then zeros.
	if !r.flag() {
		return nil, fmt.Errorf("%w: missing slice header alignment bit", ErrInvalid)
	}
	for !r.byteAligned() {
		if r.u(1) != 0 {
			return nil, fmt.Errorf("%w: nonzero slice header alignment bit", ErrInvalid)
		}
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	h.dataOffset = r.byteOffset()
	return h, nil
}

// ceilLog2 returns ceil(log2(n)) for n >= 1.
func ceilLog2(n int) int {
	if n <= 1 {
		return 0
	}
	return bits.Len(uint(n - 1))
}
