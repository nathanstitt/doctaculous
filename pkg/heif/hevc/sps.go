package hevc

import "fmt"

// Sequence parameter set (spec 7.3.2.2). Fields the intra decoder never uses
// (reference-picture sets, temporal MVP, VUI timing) are parsed to consume
// their bits and discarded; everything that steers decoding is kept.
type sps struct {
	spsID          uint32
	ptl            profileTierLevel
	chromaFormat   uint32    // 1 = 4:2:0 (the only supported value)
	width, height  int       // pic_{width,height}_in_luma_samples (pre-crop)
	confWin        [4]uint32 // left, right, top, bottom offsets (chroma units)
	bitDepthLuma   int
	bitDepthChroma int
	maxPocLsbBits  int // log2_max_pic_order_cnt_lsb

	ctbLog2SizeY                    int // CtbLog2SizeY
	minCbLog2SizeY                  int
	minTbLog2Size                   int
	maxTbLog2Size                   int
	maxTransformHierarchyDepthIntra int
	maxTransformHierarchyDepthInter int

	scalingListEnabled bool
	scalingList        *scalingListData // nil = flat default (M4 materializes)

	ampEnabled           bool
	saoEnabled           bool
	pcmEnabled           bool
	pcmBitDepthLuma      int
	pcmBitDepthChroma    int
	pcmMinCbLog2         int
	pcmMaxCbLog2         int
	pcmLoopFilterDisable bool

	numShortTermRPS      uint32
	stRPSDeltaPocs       []int // NumDeltaPocs per SPS short-term RPS (slice headers need the counts)
	longTermPresent      bool
	numLongTermSPS       uint32
	temporalMvpEnabled   bool
	strongIntraSmoothing bool

	// VUI colour signaling (defaults per spec when absent).
	fullRange       bool
	colourPrimaries uint8
	transferChar    uint8
	matrixCoeffs    uint8
	vuiColourDesc   bool

	// Derived sizes.
	ctbSizeY      int
	picWidthCtbs  int
	picHeightCtbs int
	picSizeCtbs   int
}

// croppedWidth/croppedHeight are the output dimensions after the conformance
// window (offsets are in chroma-sample units for 4:2:0, i.e. ×2 luma).
func (s *sps) croppedWidth() int  { return s.width - int(s.confWin[0]+s.confWin[1])*2 }
func (s *sps) croppedHeight() int { return s.height - int(s.confWin[2]+s.confWin[3])*2 }

// parseSPS parses an SPS NAL payload (RBSP-unescaped, header stripped).
func parseSPS(rbsp []byte) (*sps, error) {
	r := newBitReader(rbsp, "SPS")
	s := &sps{}
	r.u(4) // sps_video_parameter_set_id
	maxSubLayersMinus1 := int(r.u(3))
	if maxSubLayersMinus1 > 6 {
		return nil, fmt.Errorf("%w: sps_max_sub_layers_minus1 %d", ErrInvalid, maxSubLayersMinus1)
	}
	r.flag() // sps_temporal_id_nesting_flag
	s.ptl = parsePTL(r, maxSubLayersMinus1)
	if err := s.ptl.check(); err != nil {
		return nil, err
	}
	s.spsID = r.ue()
	s.chromaFormat = r.ue()
	if s.chromaFormat == 3 {
		if r.flag() { // separate_colour_plane_flag
			return nil, fmt.Errorf("%w: separate colour planes", ErrUnsupported)
		}
	}
	if s.chromaFormat != 1 {
		return nil, fmt.Errorf("%w: chroma format idc %d (only 4:2:0)", ErrUnsupported, s.chromaFormat)
	}
	s.width = int(r.ue())
	s.height = int(r.ue())
	// Bound allocations before they happen: a lying header must not be able
	// to demand gigabytes. 16384 per axis and 2^26 luma samples comfortably
	// cover real single-picture HEIC content (~50 MP); larger images arrive
	// as grids of small tiles.
	if s.width <= 0 || s.height <= 0 || s.width > 16384 || s.height > 16384 ||
		int64(s.width)*int64(s.height) > 1<<26 {
		return nil, fmt.Errorf("%w: picture size %dx%d", ErrUnsupported, s.width, s.height)
	}
	if r.flag() { // conformance_window_flag
		for i := range s.confWin {
			s.confWin[i] = r.ue()
		}
	}
	s.bitDepthLuma = int(r.ue()) + 8
	s.bitDepthChroma = int(r.ue()) + 8
	if s.bitDepthLuma > 10 || s.bitDepthChroma > 10 {
		return nil, fmt.Errorf("%w: bit depth %d/%d (only 8/10)", ErrUnsupported, s.bitDepthLuma, s.bitDepthChroma)
	}
	s.maxPocLsbBits = int(r.ue()) + 4
	if s.maxPocLsbBits > 16 {
		return nil, fmt.Errorf("%w: log2_max_pic_order_cnt_lsb %d", ErrInvalid, s.maxPocLsbBits)
	}
	subLayerOrdering := r.flag()
	start := 0
	if subLayerOrdering {
		start = 0
	} else {
		start = maxSubLayersMinus1
	}
	for i := start; i <= maxSubLayersMinus1; i++ {
		r.ue() // sps_max_dec_pic_buffering_minus1
		r.ue() // sps_max_num_reorder_pics
		r.ue() // sps_max_latency_increase_plus1
	}
	s.minCbLog2SizeY = int(r.ue()) + 3
	s.ctbLog2SizeY = s.minCbLog2SizeY + int(r.ue())
	s.minTbLog2Size = int(r.ue()) + 2
	s.maxTbLog2Size = s.minTbLog2Size + int(r.ue())
	s.maxTransformHierarchyDepthInter = int(r.ue())
	s.maxTransformHierarchyDepthIntra = int(r.ue())
	// Spec 7.4.3.2.1 size constraints; log2_min_transform_block_size must be
	// strictly below log2_min_luma_coding_block_size so the NxN transform
	// split of a minimum CU still lands on a legal transform size.
	if s.ctbLog2SizeY < 4 || s.ctbLog2SizeY > 6 || s.minCbLog2SizeY < 3 ||
		s.minTbLog2Size < 2 || s.minTbLog2Size >= s.minCbLog2SizeY ||
		s.maxTbLog2Size > 5 || s.maxTbLog2Size > s.ctbLog2SizeY {
		return nil, fmt.Errorf("%w: coding/transform block size bounds", ErrInvalid)
	}
	s.scalingListEnabled = r.flag()
	if s.scalingListEnabled {
		if r.flag() { // sps_scaling_list_data_present_flag
			sl, err := parseScalingListData(r)
			if err != nil {
				return nil, err
			}
			s.scalingList = sl
		}
	}
	s.ampEnabled = r.flag()
	s.saoEnabled = r.flag()
	s.pcmEnabled = r.flag()
	if s.pcmEnabled {
		s.pcmBitDepthLuma = int(r.u(4)) + 1
		s.pcmBitDepthChroma = int(r.u(4)) + 1
		s.pcmMinCbLog2 = int(r.ue()) + 3
		s.pcmMaxCbLog2 = s.pcmMinCbLog2 + int(r.ue())
		s.pcmLoopFilterDisable = r.flag()
		if s.pcmBitDepthLuma > s.bitDepthLuma || s.pcmBitDepthChroma > s.bitDepthChroma ||
			s.pcmMaxCbLog2 > s.ctbLog2SizeY {
			return nil, fmt.Errorf("%w: PCM parameters", ErrInvalid)
		}
	}
	s.numShortTermRPS = r.ue()
	if s.numShortTermRPS > 64 {
		return nil, fmt.Errorf("%w: %d short-term RPS entries", ErrInvalid, s.numShortTermRPS)
	}
	// Values are never used by intra decoding, but the delta-POC counts
	// must be tracked: CRA slice headers can carry a predicted set whose
	// bit length depends on them.
	s.stRPSDeltaPocs = make([]int, s.numShortTermRPS)
	for i := uint32(0); i < s.numShortTermRPS; i++ {
		if err := parseShortTermRPS(r, int(i), int(s.numShortTermRPS), s.stRPSDeltaPocs); err != nil {
			return nil, err
		}
	}
	s.longTermPresent = r.flag()
	if s.longTermPresent {
		s.numLongTermSPS = r.ue()
		if s.numLongTermSPS > 32 {
			return nil, fmt.Errorf("%w: %d long-term reference pictures", ErrInvalid, s.numLongTermSPS)
		}
		for i := uint32(0); i < s.numLongTermSPS; i++ {
			r.u(s.maxPocLsbBits) // lt_ref_pic_poc_lsb_sps
			r.flag()             // used_by_curr_pic_lt_sps_flag
		}
	}
	s.temporalMvpEnabled = r.flag()
	s.strongIntraSmoothing = r.flag()

	// VUI colour defaults (spec E.3.1: unspecified).
	s.colourPrimaries, s.transferChar, s.matrixCoeffs = 2, 2, 2
	if r.flag() { // vui_parameters_present_flag
		parseVUI(r, s, maxSubLayersMinus1)
	}
	if r.flag() { // sps_extension_present_flag
		rangeExt := r.flag()
		multilayerExt := r.flag()
		threeDExt := r.flag()
		sccExt := r.flag()
		ext4 := r.u(4)
		if multilayerExt || threeDExt || sccExt || ext4 != 0 {
			return nil, fmt.Errorf("%w: SPS extensions", ErrUnsupported)
		}
		if rangeExt {
			// All nine range-extension tool flags alter decoding; any
			// set flag is out of scope.
			var any bool
			for range 9 {
				any = any || r.flag()
			}
			if any {
				return nil, fmt.Errorf("%w: range-extension coding tools", ErrUnsupported)
			}
		}
	}
	if err := r.err(); err != nil {
		return nil, err
	}

	s.ctbSizeY = 1 << s.ctbLog2SizeY
	s.picWidthCtbs = (s.width + s.ctbSizeY - 1) >> s.ctbLog2SizeY
	s.picHeightCtbs = (s.height + s.ctbSizeY - 1) >> s.ctbLog2SizeY
	s.picSizeCtbs = s.picWidthCtbs * s.picHeightCtbs
	if s.width <= 0 || s.height <= 0 || s.width%(1<<s.minCbLog2SizeY) != 0 || s.height%(1<<s.minCbLog2SizeY) != 0 {
		return nil, fmt.Errorf("%w: picture size %dx%d", ErrInvalid, s.width, s.height)
	}
	if s.croppedWidth() <= 0 || s.croppedHeight() <= 0 {
		return nil, fmt.Errorf("%w: conformance window empty", ErrInvalid)
	}
	return s, nil
}

// parseShortTermRPS consumes one st_ref_pic_set entry (spec 7.3.7), tracking
// NumDeltaPocs so predicted sets consume the right number of bits. Values are
// discarded — intra pictures use no reference sets.
func parseShortTermRPS(r *bitReader, idx, total int, numDeltaPocs []int) error {
	interPred := false
	if idx != 0 {
		interPred = r.flag()
	}
	if interPred {
		// idx == total only occurs in slice headers; within the SPS the
		// reference is always the previous set.
		refIdx := idx - 1
		if idx == total {
			delta := int(r.ue()) + 1
			refIdx = idx - delta
		}
		if refIdx < 0 || refIdx >= len(numDeltaPocs) {
			return fmt.Errorf("%w: short-term RPS prediction index", ErrInvalid)
		}
		r.flag() // delta_rps_sign
		r.ue()   // abs_delta_rps_minus1
		n := 0
		for j := 0; j <= numDeltaPocs[refIdx]; j++ {
			usedByCurr := r.flag()
			useDelta := true
			if !usedByCurr {
				useDelta = r.flag()
			}
			if usedByCurr || useDelta {
				n++
			}
		}
		if idx < len(numDeltaPocs) {
			numDeltaPocs[idx] = n
		}
		return nil
	}
	negatives := r.ue()
	positives := r.ue()
	if negatives > 16 || positives > 16 {
		return fmt.Errorf("%w: short-term RPS picture counts", ErrInvalid)
	}
	for i := uint32(0); i < negatives+positives; i++ {
		r.ue()   // delta_poc_s{0,1}_minus1
		r.flag() // used_by_curr_pic_s{0,1}_flag
	}
	if idx < len(numDeltaPocs) {
		numDeltaPocs[idx] = int(negatives + positives)
	}
	return nil
}

// parseVUI consumes vui_parameters (spec E.2.1), keeping only the colour
// signaling the output stage needs.
func parseVUI(r *bitReader, s *sps, maxSubLayersMinus1 int) {
	if r.flag() { // aspect_ratio_info_present_flag
		if r.u(8) == 255 { // EXTENDED_SAR
			r.u(16)
			r.u(16)
		}
	}
	if r.flag() { // overscan_info_present_flag
		r.flag()
	}
	if r.flag() { // video_signal_type_present_flag
		r.u(3) // video_format
		s.fullRange = r.flag()
		if r.flag() { // colour_description_present_flag
			s.vuiColourDesc = true
			s.colourPrimaries = uint8(r.u(8))
			s.transferChar = uint8(r.u(8))
			s.matrixCoeffs = uint8(r.u(8))
		}
	}
	if r.flag() { // chroma_loc_info_present_flag
		r.ue()
		r.ue()
	}
	r.flag()      // neutral_chroma_indication_flag
	r.flag()      // field_seq_flag
	r.flag()      // frame_field_info_present_flag
	if r.flag() { // default_display_window_flag
		r.ue()
		r.ue()
		r.ue()
		r.ue()
	}
	if r.flag() { // vui_timing_info_present_flag
		r.u(32)       // vui_num_units_in_tick
		r.u(32)       // vui_time_scale
		if r.flag() { // vui_poc_proportional_to_timing_flag
			r.ue()
		}
		if r.flag() { // vui_hrd_parameters_present_flag
			parseHRD(r, maxSubLayersMinus1)
		}
	}
	if r.flag() { // bitstream_restriction_flag
		r.flag() // tiles_fixed_structure_flag
		r.flag() // motion_vectors_over_pic_boundaries_flag
		r.flag() // restricted_ref_pic_lists_flag
		r.ue()   // min_spatial_segmentation_idc
		r.ue()   // max_bytes_per_pic_denom
		r.ue()   // max_bits_per_min_cu_denom
		r.ue()   // log2_max_mv_length_horizontal
		r.ue()   // log2_max_mv_length_vertical
	}
}

// parseHRD consumes hrd_parameters (spec E.2.2) with commonInfPresent=1.
func parseHRD(r *bitReader, maxSubLayersMinus1 int) {
	nal := r.flag() // nal_hrd_parameters_present_flag
	vcl := r.flag() // vcl_hrd_parameters_present_flag
	subPicPresent := false
	if nal || vcl {
		subPicPresent = r.flag()
		if subPicPresent {
			r.u(8)   // tick_divisor_minus2
			r.u(5)   // du_cpb_removal_delay_increment_length_minus1
			r.flag() // sub_pic_cpb_params_in_pic_timing_sei_flag
			r.u(5)   // dpb_output_delay_du_length_minus1
		}
		r.u(4) // bit_rate_scale
		r.u(4) // cpb_size_scale
		if subPicPresent {
			r.u(4) // cpb_size_du_scale
		}
		r.u(5) // initial_cpb_removal_delay_length_minus1
		r.u(5) // au_cpb_removal_delay_length_minus1
		r.u(5) // dpb_output_delay_length_minus1
	}
	for i := 0; i <= maxSubLayersMinus1; i++ {
		fixedRate := r.flag() // fixed_pic_rate_general_flag
		if !fixedRate {
			fixedRate = r.flag() // fixed_pic_rate_within_cvs_flag
		}
		lowDelay := false
		if fixedRate {
			r.ue() // elemental_duration_in_tc_minus1
		} else {
			lowDelay = r.flag() // low_delay_hrd_flag
		}
		cpbCnt := uint32(0)
		if !lowDelay {
			cpbCnt = r.ue() // cpb_cnt_minus1
		}
		if cpbCnt > 31 {
			r.failed = true
			return
		}
		for _, present := range []bool{nal, vcl} {
			if !present {
				continue
			}
			for j := uint32(0); j <= cpbCnt; j++ {
				r.ue() // bit_rate_value_minus1
				r.ue() // cpb_size_value_minus1
				if subPicPresent {
					r.ue() // cpb_size_du_value_minus1
					r.ue() // bit_rate_du_value_minus1
				}
				r.flag() // cbr_flag
			}
		}
	}
}
