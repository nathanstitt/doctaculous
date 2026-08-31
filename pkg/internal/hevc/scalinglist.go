package hevc

import "fmt"

// scaling_list_data (spec 7.3.4). This file parses the syntax; the default
// matrices and materialization into dequant tables land with the transform
// milestone.
//
// Layout: sizeId 0..3 (4x4..32x32); matrixId 0..5 for sizeIds 0..2
// (intra/inter × Y/Cb/Cr) and 0,3 for sizeId 3 (parsed with a step of 3).
type scalingListData struct {
	predMode [4][6]bool   // true = coefficients explicitly coded
	refDelta [4][6]uint32 // when predicted: 0 = default matrix, else copy of matrixId-delta
	dcCoef   [2][6]int32  // sizeIds 2,3: DC coefficient (stored value, 8..247 range per spec)
	coeffs   [4][6][64]int32
}

// parseScalingListData parses the structure at the reader's position.
func parseScalingListData(r *bitReader) (*scalingListData, error) {
	sl := &scalingListData{}
	for sizeID := range 4 {
		step := 1
		if sizeID == 3 {
			step = 3
		}
		for matrixID := 0; matrixID < 6; matrixID += step {
			sl.predMode[sizeID][matrixID] = r.flag()
			if !sl.predMode[sizeID][matrixID] {
				delta := r.ue()
				if delta > uint32(matrixID/step) {
					return nil, fmt.Errorf("%w: scaling list matrix reference", ErrInvalid)
				}
				sl.refDelta[sizeID][matrixID] = delta * uint32(step)
				continue
			}
			coefNum := 64
			if sizeID == 0 {
				coefNum = 16
			}
			nextCoef := int32(8)
			if sizeID > 1 {
				dc := r.se()
				if dc < -7 || dc > 247 {
					return nil, fmt.Errorf("%w: scaling list DC coefficient", ErrInvalid)
				}
				sl.dcCoef[sizeID-2][matrixID] = dc + 8
				nextCoef = dc + 8
			}
			for i := range coefNum {
				delta := r.se()
				if delta < -128 || delta > 127 {
					return nil, fmt.Errorf("%w: scaling list delta coefficient", ErrInvalid)
				}
				nextCoef = (nextCoef + delta + 256) % 256
				sl.coeffs[sizeID][matrixID][i] = nextCoef
			}
		}
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return sl, nil
}
