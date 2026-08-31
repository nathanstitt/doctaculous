package hevc

import "fmt"

// profileTierLevel is the parsed profile_tier_level() structure (spec 7.3.3).
// Only the general (top-layer) values are kept; sub-layer entries are parsed
// to consume their bits and discarded (an intra still has one temporal
// layer).
type profileTierLevel struct {
	profileSpace uint8
	tier         bool
	profileIdc   uint8
	compat       uint32 // general_profile_compatibility_flag[0..31], bit 31-i = flag[i]
	levelIdc     uint8
}

// parsePTL parses profile_tier_level with profilePresentFlag=1.
func parsePTL(r *bitReader, maxSubLayersMinus1 int) profileTierLevel {
	var p profileTierLevel
	p.profileSpace = uint8(r.u(2))
	p.tier = r.flag()
	p.profileIdc = uint8(r.u(5))
	p.compat = r.u(32)
	r.u(4)  // progressive_source, interlaced_source, non_packed, frame_only
	r.u(32) // constraint flags / reserved (43 bits total)
	r.u(11)
	r.u(1) // general_inbld_flag / reserved
	p.levelIdc = uint8(r.u(8))

	profilePresent := make([]bool, maxSubLayersMinus1)
	levelPresent := make([]bool, maxSubLayersMinus1)
	for i := range maxSubLayersMinus1 {
		profilePresent[i] = r.flag()
		levelPresent[i] = r.flag()
	}
	if maxSubLayersMinus1 > 0 {
		for i := maxSubLayersMinus1; i < 8; i++ {
			r.u(2) // reserved_zero_2bits
		}
	}
	for i := range maxSubLayersMinus1 {
		if profilePresent[i] {
			// sub_layer profile block is 88 bits.
			r.u(32)
			r.u(32)
			r.u(24)
		}
		if levelPresent[i] {
			r.u(8)
		}
	}
	return p
}

// check gates on the profile signaling that is authoritative at this level.
// The feature switches that actually determine decodability (chroma format,
// bit depth, range-extension tool flags) live in the SPS/PPS and are gated
// where they are parsed: x265 labels all-intra Main-toolset streams with the
// RExt profile (idc 4), so rejecting on the label alone would refuse
// real-world HEICs.
func (p profileTierLevel) check() error {
	if p.profileSpace != 0 {
		return fmt.Errorf("%w: profile space %d", ErrUnsupported, p.profileSpace)
	}
	// Main (1), Main 10 (2), Main Still Picture (3), RExt (4, gated on SPS
	// features), and High Throughput (5, likewise) are acceptable spaces;
	// scalable/multiview/3D profiles (6+) need layer machinery we reject.
	if p.profileIdc > 5 && p.compat>>26&0x1f == 0 {
		return fmt.Errorf("%w: profile %d", ErrUnsupported, p.profileIdc)
	}
	return nil
}
