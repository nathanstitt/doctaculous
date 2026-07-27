package hevc

import "fmt"

// resolvedParams holds parsed parameter sets keyed by their IDs, as resolved
// from the raw NALs of a ParamSets (hvcC arrays) or an Annex-B stream.
type resolvedParams struct {
	sps map[uint32]*sps
	pps map[uint32]*pps
}

// resolveParamSets unescapes and parses every SPS/PPS NAL. VPS NALs are
// accepted and ignored — nothing in intra still decoding depends on them.
func resolveParamSets(ps ParamSets) (*resolvedParams, error) {
	out := &resolvedParams{sps: map[uint32]*sps{}, pps: map[uint32]*pps{}}
	for _, raw := range ps.SPS {
		n, err := parseNAL(raw)
		if err != nil {
			return nil, err
		}
		if n.typ != nalSPS {
			return nil, fmt.Errorf("%w: NAL type %d in SPS array", ErrInvalid, n.typ)
		}
		s, err := parseSPS(unescapeRBSP(n.payload))
		if err != nil {
			return nil, err
		}
		out.sps[s.spsID] = s
	}
	for _, raw := range ps.PPS {
		n, err := parseNAL(raw)
		if err != nil {
			return nil, err
		}
		if n.typ != nalPPS {
			return nil, fmt.Errorf("%w: NAL type %d in PPS array", ErrInvalid, n.typ)
		}
		p, err := parsePPS(unescapeRBSP(n.payload))
		if err != nil {
			return nil, err
		}
		out.pps[p.ppsID] = p
	}
	if len(out.sps) == 0 {
		return nil, fmt.Errorf("%w: no SPS", ErrInvalid)
	}
	if len(out.pps) == 0 {
		return nil, fmt.Errorf("%w: no PPS", ErrInvalid)
	}
	return out, nil
}

// lookup returns the SPS+PPS pair a slice referencing ppsID decodes against.
func (rp *resolvedParams) lookup(ppsID uint32) (*sps, *pps, error) {
	p, ok := rp.pps[ppsID]
	if !ok {
		return nil, nil, fmt.Errorf("%w: slice references missing PPS %d", ErrInvalid, ppsID)
	}
	s, ok := rp.sps[p.spsID]
	if !ok {
		return nil, nil, fmt.Errorf("%w: PPS references missing SPS %d", ErrInvalid, p.spsID)
	}
	return s, p, nil
}
