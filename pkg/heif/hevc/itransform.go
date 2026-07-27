package hevc

// Inverse transforms (spec 8.6.4): the 4-point DST-VII used by 4x4
// intra luma blocks and the 4/8/16/32-point integer DCT-II, applied as two
// 1-D stages (columns then rows) with the spec's exact shifts and 16-bit
// intermediate clipping. Integer-only throughout.
//
// The DCT matrices are built from the published odd coefficient sets via the
// even/odd recursion (even rows of the N-point matrix are the N/2-point
// matrix; odd rows use the size-N odd set). They are NOT rounded cosines —
// several 16/32-point entries deviate from exact rounding by design (e.g.
// 25 where 26 would round), so generating them from cos() would corrupt the
// output. The unit tests rebuild the matrices from the angle-symmetry
// construction as an independent cross-check.

// dctOddCoef holds the odd-row coefficient set per transform size.
var dctOddCoef = map[int][]int32{
	2:  {64},
	4:  {83, 36},
	8:  {89, 75, 50, 18},
	16: {90, 87, 80, 70, 57, 43, 25, 9},
	32: {90, 90, 88, 85, 82, 78, 73, 67, 61, 54, 46, 38, 31, 22, 13, 4},
}

// dctMatrix[n] is the size-n forward matrix M (rows = basis index k); the
// inverse stage computes dst[n] = sum_k src[k] * M[k][n].
var dctMatrix = buildDCTMatrices()

func buildDCTMatrices() map[int][][]int32 {
	out := make(map[int][][]int32)
	var build func(n int) [][]int32
	build = func(n int) [][]int32 {
		if n == 1 {
			return [][]int32{{64}}
		}
		half := build(n / 2)
		odd := dctOddCoef[n]
		m := make([][]int32, n)
		for k := range m {
			m[k] = make([]int32, n)
		}
		for k := range n / 2 {
			for j := range n / 2 {
				// Even rows: the half-size matrix, mirrored symmetrically.
				m[2*k][j] = half[k][j]
				m[2*k][n-1-j] = half[k][j]
			}
			for j := range n / 2 {
				// Odd rows: value at column j has angle (2k+1)(2j+1) in
				// units of pi/n over the odd set; fold by cosine symmetry.
				a := (2*k + 1) * (2*j + 1) % (4 * n)
				if a > 2*n {
					a = 4*n - a // cos(2pi - x) = cos(x)
				}
				var v int32
				if a < n {
					v = odd[(a-1)/2]
				} else {
					v = -odd[(2*n-a-1)/2] // cos(pi - x) = -cos(x)
				}
				m[2*k+1][j] = v
				m[2*k+1][n-1-j] = -v
			}
		}
		return m
	}
	// Size 2 exists only as the recursion base of the even/odd split.
	for _, n := range []int{2, 4, 8, 16, 32} {
		out[n] = build(n)
	}
	return out
}

// dstMatrix is the 4-point DST-VII (spec 8.6.4.1), rows = basis index.
var dstMatrix = [4][4]int32{
	{29, 55, 74, 84},
	{74, 74, 0, -74},
	{84, -29, -74, 55},
	{55, -84, 74, -29},
}

func clip16(v int32) int32 {
	if v < -32768 {
		return -32768
	}
	if v > 32767 {
		return 32767
	}
	return v
}

// inverseTransform1D computes one 1-D inverse stage over a column/row
// vector: dst[n] = (sum_k src[k]*M[k][n] + round) >> shift, clipped to
// 16 bits. src and dst are strided views into the block buffer.
//
// The even/odd split halves the multiply count versus the naive product:
// dst[j] = e[j] + o[j], dst[n-1-j] = e[j] - o[j], with the even part
// computed recursively.
func inverseTransform1D(src []int32, srcStride int, dst []int32, dstStride int, n, shift int) {
	round := int32(1) << (shift - 1)
	vec := make([]int32, n)
	for k := range vec {
		vec[k] = src[k*srcStride]
	}
	tmp := make([]int32, n)
	invColumnFromValues(vec, tmp, n)
	for j := range n {
		dst[j*dstStride] = clip16((tmp[j] + round) >> shift)
	}
}

// invColumnFromValues computes the unshifted inverse 1-D DCT sums via the
// even/odd split: dst[j] = e[j] + o[j], dst[n-1-j] = e[j] - o[j], with the
// even part recursing to the half-size transform.
func invColumnFromValues(src, out []int32, n int) {
	if n == 1 {
		out[0] = src[0] * 64
		return
	}
	half := n / 2
	e := make([]int32, half)
	evenSrc := make([]int32, half)
	for k := range half {
		evenSrc[k] = src[2*k]
	}
	invColumnFromValues(evenSrc, e, half)
	m := dctMatrix[n]
	for j := range half {
		var o int32
		for k := range half {
			o += src[2*k+1] * m[2*k+1][j]
		}
		out[j] = e[j] + o
		out[n-1-j] = e[j] - o
	}
}

// inverseDST1D applies one 1-D inverse DST-VII stage. The source vector is
// gathered before writing because src and dst alias when transforming in
// place.
func inverseDST1D(src []int32, srcStride int, dst []int32, dstStride int, shift int) {
	round := int32(1) << (shift - 1)
	var vec [4]int32
	for k := range vec {
		vec[k] = src[k*srcStride]
	}
	for j := range 4 {
		var sum int32
		for k := range 4 {
			sum += vec[k] * dstMatrix[k][j]
		}
		dst[j*dstStride] = clip16((sum + round) >> shift)
	}
}

// inverseTransform applies the full 2-D inverse transform in place on an
// n×n coefficient block (row-major, stride n): vertical stage with shift 7,
// then horizontal with shift 20-bitDepth (spec 8.6.4.2). useDST selects
// DST-VII (4x4 intra luma).
func inverseTransform(block []int32, n, bitDepth int, useDST bool) {
	const shift1 = 7
	shift2 := 20 - bitDepth
	// Vertical: transform each column.
	for x := range n {
		col := block[x:]
		if useDST {
			inverseDST1D(col, n, col, n, shift1)
		} else {
			inverseTransform1D(col, n, col, n, n, shift1)
		}
	}
	// Horizontal: transform each row.
	for y := range n {
		row := block[y*n:]
		if useDST {
			inverseDST1D(row, 1, row, 1, shift2)
		} else {
			inverseTransform1D(row, 1, row, 1, n, shift2)
		}
	}
}

// rescaleTransformSkip converts dequantized transform-skip coefficients to
// residuals (spec 8.6.4.2 with extended precision off): a left shift of
// tsShift = 5 + log2(nTbS) followed by the transform path's final rounding
// shift. HEVC v1 permits transform skip only on 4x4 blocks (larger sizes are
// a rejected range extension), so tsShift is always 7.
func rescaleTransformSkip(block []int32, n, bitDepth int) {
	bdShift := 20 - bitDepth
	round := int32(1) << (bdShift - 1)
	for i, v := range block[:n*n] {
		block[i] = (v<<7 + round) >> bdShift
	}
}
