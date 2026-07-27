package hevc

import (
	"math/rand"
	"slices"
	"testing"
)

// entryByReduction derives one DCT matrix entry independently of the
// production recursion: even indices reduce to the half-size matrix
// (folding the column symmetrically), and odd indices fold the angle
// k*(2j+1) over the size's odd coefficient set. Two different derivations
// agreeing on every entry is the guard against a transcription slip.
func entryByReduction(k, j, n int) int32 {
	for k != 0 && k%2 == 0 {
		if j >= n/2 {
			j = n - 1 - j
		}
		k /= 2
		n /= 2
	}
	if k == 0 {
		return 64
	}
	odd := dctOddCoef[n]
	a := k * (2*j + 1) % (4 * n)
	if a > 2*n {
		a = 4*n - a
	}
	if a < n {
		return odd[(a-1)/2]
	}
	return -odd[(2*n-a-1)/2]
}

func TestDCTMatrixConstruction(t *testing.T) {
	want4 := [4][4]int32{
		{64, 64, 64, 64},
		{83, 36, -36, -83},
		{64, -64, -64, 64},
		{36, -83, 83, -36},
	}
	for k := range 4 {
		for j := range 4 {
			if dctMatrix[4][k][j] != want4[k][j] {
				t.Errorf("M4[%d][%d] = %d, want %d", k, j, dctMatrix[4][k][j], want4[k][j])
			}
		}
	}
	want8Row1 := []int32{89, 75, 50, 18, -18, -50, -75, -89}
	want8Row3 := []int32{75, -18, -89, -50, 50, 89, 18, -75}
	want8Row5 := []int32{50, -89, 18, 75, -75, -18, 89, -50}
	for j := range 8 {
		if dctMatrix[8][1][j] != want8Row1[j] || dctMatrix[8][3][j] != want8Row3[j] || dctMatrix[8][5][j] != want8Row5[j] {
			t.Fatalf("M8 odd rows wrong at column %d", j)
		}
	}
	// Row 1 of each size starts with the odd set itself.
	for _, n := range []int{16, 32} {
		for j := range n / 2 {
			if dctMatrix[n][1][j] != dctOddCoef[n][j] {
				t.Errorf("M%d[1][%d] = %d, want %d", n, j, dctMatrix[n][1][j], dctOddCoef[n][j])
			}
		}
	}
	// Full independent cross-derivation.
	for _, n := range []int{4, 8, 16, 32} {
		for k := range n {
			for j := range n {
				if got, want := dctMatrix[n][k][j], entryByReduction(k, j, n); got != want {
					t.Fatalf("M%d[%d][%d] = %d, independent derivation %d", n, k, j, got, want)
				}
			}
		}
	}
}

// oracleInverse applies the spec's two-stage inverse via direct int64
// matrix products — the independent reference the butterfly path must match
// exactly.
func oracleInverse(block []int32, n, bitDepth int, useDST bool) []int32 {
	get := func(k, j int) int64 {
		if useDST {
			return int64(dstMatrix[k][j])
		}
		return int64(dctMatrix[n][k][j])
	}
	stage := func(src []int32, shift int, vertical bool) []int32 {
		out := make([]int32, n*n)
		round := int64(1) << (shift - 1)
		for y := range n {
			for x := range n {
				var sum int64
				for k := range n {
					if vertical {
						sum += int64(src[k*n+x]) * get(k, y)
					} else {
						sum += int64(src[y*n+k]) * get(k, x)
					}
				}
				out[y*n+x] = clip16(int32((sum + round) >> shift))
			}
		}
		return out
	}
	tmp := stage(block, 7, true)
	return stage(tmp, 20-bitDepth, false)
}

func TestInverseTransformMatchesOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, n := range []int{4, 8, 16, 32} {
		for _, bitDepth := range []int{8, 10} {
			for _, useDST := range []bool{false, true} {
				if useDST && n != 4 {
					continue
				}
				for trial := range 25 {
					block := make([]int32, n*n)
					switch trial {
					case 0: // extremes
						for i := range block {
							if i%2 == 0 {
								block[i] = 32767
							} else {
								block[i] = -32768
							}
						}
					case 1: // DC only
						block[0] = 1024
					default:
						for i := range block {
							block[i] = int32(rng.Intn(65536) - 32768)
						}
					}
					want := oracleInverse(block, n, bitDepth, useDST)
					got := slices.Clone(block)
					inverseTransform(got, n, bitDepth, useDST)
					for i := range want {
						if got[i] != want[i] {
							t.Fatalf("n=%d depth=%d dst=%v trial=%d: sample %d = %d, oracle %d",
								n, bitDepth, useDST, trial, i, got[i], want[i])
						}
					}
				}
			}
		}
	}
}

func TestRescaleTransformSkip(t *testing.T) {
	// bitDepth 8: bdShift 12, so r = (d<<7 + 2048) >> 12.
	block := []int32{1, -1, 100, 0, 32, -32, 5, -5, 0, 0, 0, 0, 0, 0, 0, 16}
	got := slices.Clone(block)
	rescaleTransformSkip(got, 4, 8)
	for i, d := range block {
		want := (d<<7 + 2048) >> 12
		if got[i] != want {
			t.Errorf("sample %d: %d, want %d", i, got[i], want)
		}
	}
}

func TestDiagScanOrder4x4(t *testing.T) {
	want := []scanPos{
		{0, 0}, {0, 1}, {1, 0}, {0, 2}, {1, 1}, {2, 0}, {0, 3}, {1, 2},
		{2, 1}, {3, 0}, {1, 3}, {2, 2}, {3, 1}, {2, 3}, {3, 2}, {3, 3},
	}
	got := diagScan[4]
	if len(got) != 16 {
		t.Fatalf("len = %d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("diag[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	// Every scan covers each position exactly once.
	for _, n := range []int{2, 4, 8} {
		for name, scan := range map[string][]scanPos{"diag": diagScan[n], "horiz": horizScan[n], "vert": vertScan[n]} {
			seen := map[scanPos]bool{}
			for _, p := range scan {
				if seen[p] {
					t.Fatalf("%s scan %d repeats %+v", name, n, p)
				}
				seen[p] = true
			}
			if len(seen) != n*n {
				t.Fatalf("%s scan %d covers %d of %d", name, n, len(seen), n*n)
			}
		}
	}
}
