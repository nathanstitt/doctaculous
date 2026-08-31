package hevc

// Intra prediction (spec 8.4.4.2): reference-sample gathering with
// substitution, the [1 2 1] and strong-smoothing reference filters, and the
// planar/DC/angular predictors, all integer-exact per spec.

// intraPredAngle (spec Table 8-5), indexed by predModeIntra-2 (modes 2..34).
var intraPredAngle = [33]int32{
	32, 26, 21, 17, 13, 9, 5, 2, 0, -2, -5, -9, -13, -17, -21, -26, -32,
	-26, -21, -17, -13, -9, -5, -2, 0, 2, 5, 9, 13, 17, 21, 26, 32,
}

// invAngle (spec Table 8-6), indexed by predModeIntra-11 (modes 11..25).
var invAngle = [15]int32{
	-4096, -1638, -910, -630, -482, -390, -315, -256,
	-315, -390, -482, -630, -910, -1638, -4096,
}

// refSamples holds the neighbor line for one nTbS×nTbS prediction:
// left[0..2n-1] runs down from (x0-1, y0), corner is (x0-1, y0-1), top runs
// right from (x0, y0-1) for 2n samples.
type refSamples struct {
	left   []int32 // 2n
	corner int32
	top    []int32 // 2n
}

// gatherRefSamples collects and substitutes the reference line for a
// transform block of size n at plane position (x, y). available reports
// whether the sample at plane coordinates (sx, sy) may be used for
// prediction (in-picture, already reconstructed, same slice/tile).
func gatherRefSamples(plane []uint16, stride, x, y, n, bitDepth int,
	available func(sx, sy int) bool) *refSamples {

	r := &refSamples{left: make([]int32, 2*n), top: make([]int32, 2*n)}
	avail := make([]bool, 4*n+1) // bottom-left..up-right in substitution order:
	// index 0..2n-1: left column bottom-up (left[2n-1..0]);
	// 2n: corner; 2n+1..4n: top row left-to-right.
	vals := make([]int32, 4*n+1)

	get := func(sx, sy int) (int32, bool) {
		if !available(sx, sy) {
			return 0, false
		}
		return int32(plane[sy*stride+sx]), true
	}
	for i := range 2 * n {
		// substitution order runs bottom-left upward: left[2n-1-i]
		vals[i], avail[i] = get(x-1, y+2*n-1-i)
	}
	vals[2*n], avail[2*n] = get(x-1, y-1)
	for i := range 2 * n {
		vals[2*n+1+i], avail[2*n+1+i] = get(x+i, y-1)
	}

	// Substitution (spec 8.4.4.2.2): if nothing is available, mid-gray;
	// otherwise the first entry inherits the first available value along
	// the order and every later unavailable entry copies its predecessor.
	anyAvail := false
	for _, a := range avail {
		if a {
			anyAvail = true
			break
		}
	}
	if !anyAvail {
		mid := int32(1) << (bitDepth - 1)
		for i := range vals {
			vals[i] = mid
		}
	} else {
		if !avail[0] {
			for i := 1; i < len(vals); i++ {
				if avail[i] {
					vals[0] = vals[i]
					break
				}
			}
		}
		for i := 1; i < len(vals); i++ {
			if !avail[i] {
				vals[i] = vals[i-1]
			}
		}
	}

	for i := range 2 * n {
		r.left[i] = vals[2*n-1-i]
	}
	r.corner = vals[2*n]
	copy(r.top, vals[2*n+1:])
	return r
}

// filterRefSamples applies the reference-sample filters (spec 8.4.4.2.3)
// when required for the given luma mode/size; chroma is never filtered.
func filterRefSamples(r *refSamples, mode, n, bitDepth int, strongEnabled bool) *refSamples {
	if mode == 1 || n == 4 {
		return r
	}
	minDist := min(abs32(int32(mode-26)), abs32(int32(mode-10)))
	var thresh int32
	switch n {
	case 8:
		thresh = 7
	case 16:
		thresh = 1
	case 32:
		thresh = 0
	default:
		return r
	}
	if minDist <= thresh {
		return r
	}

	out := &refSamples{left: make([]int32, 2*n), top: make([]int32, 2*n)}
	if strongEnabled && n == 32 &&
		abs32(r.corner+r.top[2*n-1]-2*r.top[n-1]) < 1<<(bitDepth-5) &&
		abs32(r.corner+r.left[2*n-1]-2*r.left[n-1]) < 1<<(bitDepth-5) {
		// Strong (bilinear) smoothing.
		out.corner = r.corner
		for i := range 2 * n {
			out.top[i] = ((int32(63-i))*r.corner + int32(i+1)*r.top[2*n-1] + 32) >> 6
			out.left[i] = ((int32(63-i))*r.corner + int32(i+1)*r.left[2*n-1] + 32) >> 6
		}
		out.top[2*n-1] = r.top[2*n-1]
		out.left[2*n-1] = r.left[2*n-1]
		return out
	}
	// [1 2 1] filter.
	out.corner = (r.left[0] + 2*r.corner + r.top[0] + 2) >> 2
	for i := range 2 * n {
		switch i {
		case 2*n - 1:
			out.left[i] = r.left[i]
			out.top[i] = r.top[i]
		case 0:
			out.left[0] = (r.corner + 2*r.left[0] + r.left[1] + 2) >> 2
			out.top[0] = (r.corner + 2*r.top[0] + r.top[1] + 2) >> 2
		default:
			out.left[i] = (r.left[i-1] + 2*r.left[i] + r.left[i+1] + 2) >> 2
			out.top[i] = (r.top[i-1] + 2*r.top[i] + r.top[i+1] + 2) >> 2
		}
	}
	return out
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// predictIntra fills dst (n×n, stride n) for the given mode. isLuma enables
// the DC/vertical/horizontal edge filters (luma, n < 32 only).
func predictIntra(dst []int32, r *refSamples, mode, n, bitDepth int, isLuma bool) {
	switch mode {
	case 0:
		predictPlanar(dst, r, n)
	case 1:
		predictDC(dst, r, n, isLuma)
	default:
		predictAngular(dst, r, mode, n, bitDepth, isLuma)
	}
}

// predictPlanar (spec 8.4.4.2.4).
func predictPlanar(dst []int32, r *refSamples, n int) {
	log2 := log2int(n)
	for y := range n {
		for x := range n {
			dst[y*n+x] = (int32(n-1-x)*r.left[y] + int32(x+1)*r.top[n] +
				int32(n-1-y)*r.top[x] + int32(y+1)*r.left[n] + int32(n)) >> (log2 + 1)
		}
	}
}

// predictDC (spec 8.4.4.2.5) with the luma edge blend for n < 32.
func predictDC(dst []int32, r *refSamples, n int, isLuma bool) {
	var sum int32
	for i := range n {
		sum += r.left[i] + r.top[i]
	}
	dc := (sum + int32(n)) >> (log2int(n) + 1)
	for i := range dst[:n*n] {
		dst[i] = dc
	}
	if isLuma && n < 32 {
		dst[0] = (r.left[0] + 2*dc + r.top[0] + 2) >> 2
		for x := 1; x < n; x++ {
			dst[x] = (r.top[x] + 3*dc + 2) >> 2
		}
		for y := 1; y < n; y++ {
			dst[y*n] = (r.left[y] + 3*dc + 2) >> 2
		}
	}
}

// predictAngular (spec 8.4.4.2.6). Modes >= 18 predict from the top
// reference (extended leftward via invAngle when the angle is negative);
// modes < 18 are the transpose using the left reference.
func predictAngular(dst []int32, r *refSamples, mode, n, bitDepth int, isLuma bool) {
	angle := intraPredAngle[mode-2]
	maxVal := int32(1)<<bitDepth - 1

	// Build the main reference array ref[-n..2n] centered so index 0 is
	// the corner-adjacent sample.
	ref := make([]int32, 3*n+1)
	base := n // ref[base+i] == spec ref[i]
	vertical := mode >= 18
	main, side := r.top, r.left
	if !vertical {
		main, side = r.left, r.top
	}
	ref[base] = r.corner
	for i := 1; i <= 2*n; i++ {
		ref[base+i] = main[i-1]
	}
	if angle < 0 {
		// Extend with side samples projected through invAngle. The
		// deepest index the interpolation ever reads is
		// ((n*angle)>>5)+1, so the loop is strictly greater-than: the
		// spec's nominal last entry is never read, and computing it
		// would project beyond the side array for shallow angles.
		inv := invAngle[mode-11]
		lastIdx := (int32(n) * angle) >> 5
		for i := int32(-1); i > lastIdx; i-- {
			idx := (i*inv + 128) >> 8
			ref[base+int(i)] = valueAt(side, r.corner, int(idx)-1)
		}
	}

	for y := range n {
		for x := range n {
			// Coordinates along the prediction direction: for vertical
			// modes the "row" advances with y; horizontal transposes.
			row, col := y, x
			if !vertical {
				row, col = x, y
			}
			pos := (int32(row+1) * angle) >> 5
			frac := (int32(row+1) * angle) & 31
			var v int32
			if frac == 0 {
				v = ref[base+col+int(pos)+1]
			} else {
				a := ref[base+col+int(pos)+1]
				b := ref[base+col+int(pos)+2]
				v = ((32-frac)*a + frac*b + 16) >> 5
			}
			dst[y*n+x] = v
		}
	}

	// Pure vertical/horizontal edge compensation (luma, n < 32).
	if isLuma && n < 32 {
		switch mode {
		case 26:
			for y := range n {
				dst[y*n] = clip3(0, maxVal, r.top[0]+((r.left[y]-r.corner)>>1))
			}
		case 10:
			for x := range n {
				dst[x] = clip3(0, maxVal, r.left[0]+((r.top[x]-r.corner)>>1))
			}
		}
	}
}

// valueAt reads the side reference with spec indexing where index -1 is the
// corner.
func valueAt(side []int32, corner int32, idx int) int32 {
	if idx < 0 {
		return corner
	}
	return side[idx]
}

func log2int(n int) int {
	l := 0
	for n > 1 {
		n >>= 1
		l++
	}
	return l
}
