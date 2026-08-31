package hevc

// Coefficient scan orders (spec 6.5.3-6.5.5). The up-right diagonal scan is
// needed both by residual coding and by scaling-list materialization (the
// coded coefficient lists are stored in this order); horizontal and vertical
// scans are selected by intra mode for 4x4/8x8 blocks.

// scanPos is one (x, y) position within a block.
type scanPos struct{ x, y uint8 }

// diagScanOrder returns the up-right diagonal scan of an n×n block:
// diagonals of constant x+y, each traversed from lower-left to upper-right
// ((0,0), (0,1), (1,0), (0,2), (1,1), (2,0), ...).
func diagScanOrder(n int) []scanPos {
	out := make([]scanPos, 0, n*n)
	for d := 0; d <= 2*(n-1); d++ {
		yStart := min(d, n-1)
		for y := yStart; d-y < n && y >= 0; y-- {
			out = append(out, scanPos{x: uint8(d - y), y: uint8(y)})
		}
	}
	return out
}

// horizScanOrder returns the row-major scan.
func horizScanOrder(n int) []scanPos {
	out := make([]scanPos, 0, n*n)
	for y := range n {
		for x := range n {
			out = append(out, scanPos{x: uint8(x), y: uint8(y)})
		}
	}
	return out
}

// vertScanOrder returns the column-major scan.
func vertScanOrder(n int) []scanPos {
	out := make([]scanPos, 0, n*n)
	for x := range n {
		for y := range n {
			out = append(out, scanPos{x: uint8(x), y: uint8(y)})
		}
	}
	return out
}

// Precomputed scans for the sizes residual coding uses (4x4 sub-blocks and
// the sub-block grids of 4..32 blocks).
var (
	diagScan  = map[int][]scanPos{1: diagScanOrder(1), 2: diagScanOrder(2), 4: diagScanOrder(4), 8: diagScanOrder(8)}
	horizScan = map[int][]scanPos{1: horizScanOrder(1), 2: horizScanOrder(2), 4: horizScanOrder(4), 8: horizScanOrder(8)}
	vertScan  = map[int][]scanPos{1: vertScanOrder(1), 2: vertScanOrder(2), 4: vertScanOrder(4), 8: vertScanOrder(8)}
)
