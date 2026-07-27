package hevc

import (
	"fmt"
	"testing"
)

// parallelFixtures exercise each parallel decode shape.
var parallelFixtures = []string{
	"x265-512x512-qp27.hevc",             // WPP, 8 CTB rows
	"x265-512x512-qp27-nofilt.hevc",      // WPP without loop filters
	"x265-64x64-qp27-ctu16.hevc",         // WPP with 16-px CTUs (4 rows)
	"kvazaar-96x80-qp27-tiles2x2.hevc",   // tiles, one CTB each
	"kvazaar-512x512-qp27-tiles2x2.hevc", // tiles, 16 CTBs each
	"x265-96x80-qp27.hevc",               // no WPP/tiles: sequential parse, parallel filters
}

// decodeWithWorkers decodes a fixture at a given worker count.
func decodeWithWorkers(t testing.TB, name string, workers int) *Frame {
	params, slices := loadStreamTB(t, name)
	frame, err := DecodeFrameOpts(params, slices, &DecodeOptions{Workers: workers})
	if err != nil {
		t.Fatalf("%s workers=%d: %v", name, workers, err)
	}
	return frame
}

// loadStreamTB adapts loadStream output to raw slice NALs.
func loadStreamTB(t testing.TB, name string) (ParamSets, [][]byte) {
	params, units := loadStream(t, name)
	nals := make([][]byte, 0, len(units))
	for _, u := range units {
		nal := make([]byte, 0, len(u.payload)+2)
		h := uint16(u.typ)<<9 | uint16(u.layerID)<<3 | uint16(u.temporalID+1)
		nal = append(nal, byte(h>>8), byte(h))
		nal = append(nal, u.payload...)
		nals = append(nals, nal)
	}
	return params, nals
}

// TestParallelDeterminism: any worker count must reproduce Workers=1 output
// byte for byte.
func TestParallelDeterminism(t *testing.T) {
	for _, name := range parallelFixtures {
		t.Run(name, func(t *testing.T) {
			ref := decodeWithWorkers(t, name, 1)
			for _, workers := range []int{2, 3, 8} {
				got := decodeWithWorkers(t, name, workers)
				for i, plane := range [][]uint16{got.Y, got.Cb, got.Cr} {
					want := [][]uint16{ref.Y, ref.Cb, ref.Cr}[i]
					if len(plane) != len(want) {
						t.Fatalf("workers=%d plane %d length %d vs %d", workers, i, len(plane), len(want))
					}
					for j := range plane {
						if plane[j] != want[j] {
							t.Fatalf("workers=%d plane %d sample %d: %d vs %d", workers, i, j, plane[j], want[j])
						}
					}
				}
			}
		})
	}
}

func benchDecode(b *testing.B, name string, workers int) {
	params, nals := loadStreamTB(b, name)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeFrameOpts(params, nals, &DecodeOptions{Workers: workers}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeWPP(b *testing.B) {
	for _, workers := range []int{1, 4, 8} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			benchDecode(b, "x265-512x512-qp27.hevc", workers)
		})
	}
}

func BenchmarkDecodeTiles(b *testing.B) {
	for _, workers := range []int{1, 4} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			benchDecode(b, "kvazaar-512x512-qp27-tiles2x2.hevc", workers)
		})
	}
}
