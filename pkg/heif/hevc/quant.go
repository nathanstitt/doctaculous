package hevc

import (
	"fmt"
	"slices"
)

// Dequantization (spec 8.6.3) and scaling-matrix materialization
// (spec 7.4.5, tables 7-5/7-6).

// levelScale (spec 8.6.3): per qP%6 multiplier.
var levelScale = [6]int64{40, 45, 51, 57, 64, 72}

// Default scaling matrices in raster order. 4x4 is flat 16; the 8x8 tables
// below also seed 16x16/32x32 by 2x/4x sample replication with the DC entry
// overridden (spec 7.4.5).
var defaultScaling8x8Intra = [64]int32{
	16, 16, 16, 16, 17, 18, 21, 24,
	16, 16, 16, 16, 17, 19, 22, 25,
	16, 16, 17, 18, 20, 22, 25, 29,
	16, 16, 18, 21, 24, 27, 31, 36,
	17, 17, 20, 24, 30, 35, 41, 47,
	18, 19, 22, 27, 35, 44, 54, 65,
	21, 22, 25, 31, 41, 54, 70, 88,
	24, 25, 29, 36, 47, 65, 88, 115,
}

var defaultScaling8x8Inter = [64]int32{
	16, 16, 16, 16, 17, 18, 20, 24,
	16, 16, 16, 17, 18, 20, 24, 25,
	16, 16, 17, 18, 20, 24, 25, 28,
	16, 17, 18, 20, 24, 25, 28, 33,
	17, 18, 20, 24, 25, 28, 33, 41,
	18, 20, 24, 25, 28, 33, 41, 54,
	20, 24, 25, 28, 33, 41, 54, 71,
	24, 25, 28, 33, 41, 54, 71, 91,
}

// scalingMatrices holds the materialized m[x][y] factors, raster order, for
// sizes 4/8/16/32 (index by sizeID 0..3) and matrixID 0..5 (intra/inter ×
// Y/Cb/Cr; sizeID 3 defines only matrixIDs 0 and 3).
type scalingMatrices struct {
	m [4][6][]int32
}

// sizeForSizeID maps sizeID to transform size.
func sizeForSizeID(sizeID int) int { return 4 << sizeID }

// materializeScalingLists resolves parsed scaling-list data (or nil for the
// pure defaults) into flat raster matrices.
func materializeScalingLists(sl *scalingListData) (*scalingMatrices, error) {
	out := &scalingMatrices{}
	for sizeID := range 4 {
		step := 1
		if sizeID == 3 {
			step = 3
		}
		for matrixID := 0; matrixID < 6; matrixID += step {
			m, err := resolveMatrix(sl, sizeID, matrixID)
			if err != nil {
				return nil, err
			}
			out.m[sizeID][matrixID] = m
		}
	}
	return out, nil
}

// resolveMatrix produces one raster matrix, following reference chains
// (scaling_list_pred_matrix_id_delta) down to explicit data or the default.
func resolveMatrix(sl *scalingListData, sizeID, matrixID int) ([]int32, error) {
	for range 6 { // reference chains cannot be longer than the matrix count
		if sl == nil || !sl.predMode[sizeID][matrixID] {
			if sl != nil && sl.refDelta[sizeID][matrixID] != 0 {
				matrixID -= int(sl.refDelta[sizeID][matrixID])
				if matrixID < 0 {
					return nil, fmt.Errorf("%w: scaling list reference chain", ErrInvalid)
				}
				continue
			}
			return defaultMatrix(sizeID, matrixID), nil
		}
		return explicitMatrix(sl, sizeID, matrixID), nil
	}
	return nil, fmt.Errorf("%w: scaling list reference cycle", ErrInvalid)
}

// defaultMatrix builds the spec default for one size/matrix.
func defaultMatrix(sizeID, matrixID int) []int32 {
	size := sizeForSizeID(sizeID)
	if sizeID == 0 {
		m := make([]int32, 16)
		for i := range m {
			m[i] = 16
		}
		return m
	}
	base := &defaultScaling8x8Intra
	if matrixID >= 3 {
		base = &defaultScaling8x8Inter
	}
	if sizeID == 1 {
		return slices.Clone(base[:])
	}
	// 16x16 / 32x32: replicate each 8x8 sample; DC stays the default 16
	// (the explicit-data path overrides DC separately).
	factor := size / 8
	m := make([]int32, size*size)
	for y := range size {
		for x := range size {
			m[y*size+x] = base[(y/factor)*8+x/factor]
		}
	}
	m[0] = 16
	return m
}

// explicitMatrix converts one parsed coefficient list (up-right diagonal
// order) into a raster matrix, with replication and DC override for
// 16x16/32x32.
func explicitMatrix(sl *scalingListData, sizeID, matrixID int) []int32 {
	size := sizeForSizeID(sizeID)
	coefs := sl.coeffs[sizeID][matrixID]
	if sizeID == 0 {
		m := make([]int32, 16)
		for i, pos := range diagScan[4] {
			m[int(pos.y)*4+int(pos.x)] = coefs[i]
		}
		return m
	}
	// Coefficients describe an 8x8 grid regardless of actual size.
	var grid [64]int32
	for i, pos := range diagScan[8] {
		grid[int(pos.y)*8+int(pos.x)] = coefs[i]
	}
	if sizeID == 1 {
		return slices.Clone(grid[:])
	}
	factor := size / 8
	m := make([]int32, size*size)
	for y := range size {
		for x := range size {
			m[y*size+x] = grid[(y/factor)*8+x/factor]
		}
	}
	m[0] = sl.dcCoef[sizeID-2][matrixID]
	return m
}

// flatScaling reports the constant factor 16 used when scaling lists are
// disabled (spec 8.6.3: m[x][y] = 16).
const flatScaling = 16

// dequant scales one coefficient block in place (spec 8.6.3). scaling is
// nil for flat scaling; qp is the final Qp' for the component (includes the
// bit-depth offset); log2Size is log2 of the transform size.
func dequant(block []int32, scaling []int32, qp int32, log2Size, bitDepth int) {
	n := 1 << log2Size
	bdShift := bitDepth + log2Size - 5
	round := int64(1) << (bdShift - 1)
	scale := levelScale[qp%6] << uint(qp/6)
	for i, v := range block[:n*n] {
		if v == 0 {
			continue
		}
		m := int64(flatScaling)
		if scaling != nil {
			m = int64(scaling[i])
		}
		d := (int64(v)*m*scale + round) >> bdShift
		block[i] = clip16int64(d)
	}
}

func clip16int64(v int64) int32 {
	if v < -32768 {
		return -32768
	}
	if v > 32767 {
		return 32767
	}
	return int32(v)
}

// chromaQPMapping (spec Table 8-10, 4:2:0): maps the intermediate qPi to
// qPCb/qPCr.
func chromaQPMapping(qPi int32) int32 {
	switch {
	case qPi < 30:
		return qPi
	case qPi > 43:
		return qPi - 6
	default:
		// qPi:  30 31 32 33 34 35 36 37 38 39 40 41 42 43
		// qPc:  29 30 31 32 33 33 34 34 35 35 36 36 37 37
		table := [14]int32{29, 30, 31, 32, 33, 33, 34, 34, 35, 35, 36, 36, 37, 37}
		return table[qPi-30]
	}
}
