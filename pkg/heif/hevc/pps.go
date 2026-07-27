package hevc

import "fmt"

// Picture parameter set (spec 7.3.2.3).
type pps struct {
	ppsID uint32
	spsID uint32

	dependentSliceSegments  bool
	outputFlagPresent       bool
	numExtraSliceHeaderBits int
	signDataHiding          bool
	cabacInitPresent        bool
	initQP                  int32 // 26 + init_qp_minus26
	constrainedIntraPred    bool
	transformSkipEnabled    bool
	cuQpDeltaEnabled        bool
	diffCuQpDeltaDepth      int
	cbQpOffset              int32
	crQpOffset              int32
	sliceChromaQpOffsets    bool
	transquantBypassEnabled bool

	tilesEnabled          bool
	entropyCodingSync     bool // WPP
	numTileColumns        int
	numTileRows           int
	uniformSpacing        bool
	tileColumnWidths      []uint32 // explicit widths in CTBs (non-uniform), len numTileColumns
	tileRowHeights        []uint32
	loopFilterAcrossTiles bool

	loopFilterAcrossSlices bool
	deblockControlPresent  bool
	deblockOverrideEnabled bool
	deblockDisabled        bool
	betaOffsetDiv2         int32
	tcOffsetDiv2           int32
	scalingList            *scalingListData
	scalingListPresent     bool
	log2ParallelMergeLevel int
	sliceHeaderExtension   bool
}

// parsePPS parses a PPS NAL payload (RBSP-unescaped, header stripped).
func parsePPS(rbsp []byte) (*pps, error) {
	r := newBitReader(rbsp, "PPS")
	p := &pps{}
	p.ppsID = r.ue()
	p.spsID = r.ue()
	if p.ppsID > 63 || p.spsID > 15 {
		return nil, fmt.Errorf("%w: parameter set id", ErrInvalid)
	}
	p.dependentSliceSegments = r.flag()
	p.outputFlagPresent = r.flag()
	p.numExtraSliceHeaderBits = int(r.u(3))
	p.signDataHiding = r.flag()
	p.cabacInitPresent = r.flag()
	r.ue() // num_ref_idx_l0_default_active_minus1
	r.ue() // num_ref_idx_l1_default_active_minus1
	p.initQP = 26 + r.se()
	p.constrainedIntraPred = r.flag()
	p.transformSkipEnabled = r.flag()
	p.cuQpDeltaEnabled = r.flag()
	if p.cuQpDeltaEnabled {
		p.diffCuQpDeltaDepth = int(r.ue())
	}
	p.cbQpOffset = r.se()
	p.crQpOffset = r.se()
	if p.cbQpOffset < -12 || p.cbQpOffset > 12 || p.crQpOffset < -12 || p.crQpOffset > 12 {
		return nil, fmt.Errorf("%w: chroma QP offset range", ErrInvalid)
	}
	p.sliceChromaQpOffsets = r.flag()
	weightedPred := r.flag()
	weightedBipred := r.flag()
	_ = weightedPred // only meaningful for P/B slices, which are rejected
	_ = weightedBipred
	p.transquantBypassEnabled = r.flag()
	p.tilesEnabled = r.flag()
	p.entropyCodingSync = r.flag()
	p.numTileColumns, p.numTileRows = 1, 1
	if p.tilesEnabled {
		p.numTileColumns = int(r.ue()) + 1
		p.numTileRows = int(r.ue()) + 1
		if p.numTileColumns > 64 || p.numTileRows > 64 {
			return nil, fmt.Errorf("%w: tile grid %dx%d", ErrInvalid, p.numTileColumns, p.numTileRows)
		}
		p.uniformSpacing = r.flag()
		if !p.uniformSpacing {
			p.tileColumnWidths = make([]uint32, p.numTileColumns)
			for i := range p.numTileColumns - 1 {
				p.tileColumnWidths[i] = r.ue() + 1
			}
			p.tileRowHeights = make([]uint32, p.numTileRows)
			for i := range p.numTileRows - 1 {
				p.tileRowHeights[i] = r.ue() + 1
			}
		}
		p.loopFilterAcrossTiles = r.flag()
	} else {
		p.loopFilterAcrossTiles = true
	}
	p.loopFilterAcrossSlices = r.flag()
	p.deblockControlPresent = r.flag()
	if p.deblockControlPresent {
		p.deblockOverrideEnabled = r.flag()
		p.deblockDisabled = r.flag()
		if !p.deblockDisabled {
			p.betaOffsetDiv2 = r.se()
			p.tcOffsetDiv2 = r.se()
			if p.betaOffsetDiv2 < -6 || p.betaOffsetDiv2 > 6 || p.tcOffsetDiv2 < -6 || p.tcOffsetDiv2 > 6 {
				return nil, fmt.Errorf("%w: deblocking offset range", ErrInvalid)
			}
		}
	}
	p.scalingListPresent = r.flag()
	if p.scalingListPresent {
		sl, err := parseScalingListData(r)
		if err != nil {
			return nil, err
		}
		p.scalingList = sl
	}
	r.flag() // lists_modification_present_flag
	p.log2ParallelMergeLevel = int(r.ue()) + 2
	p.sliceHeaderExtension = r.flag()
	if r.flag() { // pps_extension_present_flag
		rangeExt := r.flag()
		multilayerExt := r.flag()
		threeDExt := r.flag()
		sccExt := r.flag()
		ext4 := r.u(4)
		if multilayerExt || threeDExt || sccExt || ext4 != 0 {
			return nil, fmt.Errorf("%w: PPS extensions", ErrUnsupported)
		}
		if rangeExt {
			if err := parsePPSRangeExtension(r, p); err != nil {
				return nil, err
			}
		}
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return p, nil
}

// parsePPSRangeExtension accepts only the all-defaults form: any enabled
// range-extension tool is out of scope.
func parsePPSRangeExtension(r *bitReader, p *pps) error {
	if p.transformSkipEnabled {
		if v := r.ue(); v != 0 { // log2_max_transform_skip_block_size_minus2
			return fmt.Errorf("%w: transform-skip blocks larger than 4x4", ErrUnsupported)
		}
	}
	if r.flag() { // cross_component_prediction_enabled_flag
		return fmt.Errorf("%w: cross-component prediction", ErrUnsupported)
	}
	if r.flag() { // chroma_qp_offset_list_enabled_flag
		return fmt.Errorf("%w: chroma QP offset lists", ErrUnsupported)
	}
	// log2_sao_offset_scale_{luma,chroma}: non-zero only matters above
	// 10-bit, which is already rejected; still require zero for exactness.
	saoScaleLuma := r.ue()
	saoScaleChroma := r.ue()
	if saoScaleLuma != 0 || saoScaleChroma != 0 {
		return fmt.Errorf("%w: SAO offset scaling", ErrUnsupported)
	}
	return nil
}
