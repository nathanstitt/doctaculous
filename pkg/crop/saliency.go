package crop

import (
	"image"
	"math"
	"runtime"
	"sync"
)

// Default scoring weights. Edge energy dominates; skin only nudges.
//
// On skin-tone bias: published YCbCr skin ranges are largely tuned on lighter
// skin and systematically underweight darker skin tones. Two mitigations here
// are load-bearing — the Cb/Cr box in scoreMap is deliberately wide and
// luma-independent, so it does not discriminate on brightness, and
// defaultSkinWeight sits below defaultEdgeWeight so skin nudges the crop rather
// than deciding it. Do not raise the skin weight above the edge weight.
const (
	defaultEdgeWeight       = 1.0
	defaultSaturationWeight = 0.3
	defaultSkinWeight       = 0.5
	defaultCenterWeight     = 0.3
)

// candidateStepFraction is the sliding stride, as a fraction of the window's
// smaller side. Finer than this buys no visible improvement and costs time.
const candidateStepFraction = 16

// saliencyRect scores every candidate window and returns the highest.
func saliencyRect(img image.Image, win image.Point, opts Options) image.Rectangle {
	b := img.Bounds()
	score := scoreMap(img, opts)
	sum := newIntegral(score, b.Dx(), b.Dy())

	step := win.X
	if win.Y < step {
		step = win.Y
	}
	step /= candidateStepFraction
	if step < 1 {
		step = 1
	}

	type candidate struct {
		rect  image.Rectangle
		score float64
	}
	// Ties resolve toward the centre, so seed with the centered window. Every
	// worker seeds from this same immutable value; nothing reads `best` outside
	// the mutex.
	seed := candidate{rect: anchorWindow(b, win, StrategyCenter)}
	seed.score = sum.mean(seed.rect.Sub(b.Min))
	best := seed

	var mu sync.Mutex
	var wg sync.WaitGroup
	maxY := b.Dy() - win.Y
	rows := make(chan int)

	workers := runtime.GOMAXPROCS(0)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Seed from the immutable `seed`, NOT from the shared `best`:
			// reading `best` here would race with the locked writes below.
			local := seed
			for oy := range rows {
				for ox := 0; ox <= b.Dx()-win.X; ox += step {
					r := image.Rectangle{
						Min: image.Pt(ox, oy),
						Max: image.Pt(ox+win.X, oy+win.Y),
					}
					s := sum.mean(r)
					// Strictly greater keeps the centred seed on ties.
					if s > local.score {
						local = candidate{rect: r.Add(b.Min), score: s}
					}
				}
			}
			mu.Lock()
			// Strictly greater again, and workers all start from the same seed,
			// so the merge order cannot change the result: the reduction is
			// deterministic regardless of goroutine scheduling.
			if local.score > best.score {
				best = local
			}
			mu.Unlock()
		}()
	}
	for oy := 0; oy <= maxY; oy += step {
		rows <- oy
	}
	close(rows)
	wg.Wait()

	return best.rect
}

// scoreMap builds the per-pixel saliency score, row-major, len = w*h.
func scoreMap(img image.Image, opts Options) []float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	// resolve honours an explicit 0, so a caller can disable a term outright.
	ew := resolve(opts.Weights.Edge, defaultEdgeWeight)
	sw := resolve(opts.Weights.Saturation, defaultSaturationWeight)
	kw := resolve(opts.Weights.Skin, defaultSkinWeight)
	cw := resolve(opts.Weights.Center, defaultCenterWeight)

	// Pass 1: luma, saturation and skin, per pixel.
	luma := make([]float64, w*h)
	flat := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r16, g16, b16, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			r, g, bl := float64(r16>>8), float64(g16>>8), float64(b16>>8)
			i := y*w + x

			// Rec. 601 luma, matching image/color's YCbCr conversion.
			yy := 0.299*r + 0.587*g + 0.114*bl
			luma[i] = yy

			maxc := math.Max(r, math.Max(g, bl))
			minc := math.Min(r, math.Min(g, bl))
			var sat float64
			if maxc > 0 {
				sat = (maxc - minc) / maxc
			}

			cb := -0.169*r - 0.331*g + 0.5*bl + 128
			cr := 0.5*r - 0.419*g - 0.081*bl + 128
			var skin float64
			// Deliberately wide and luma-independent; see the bias note above.
			if cb >= 77 && cb <= 133 && cr >= 133 && cr <= 177 {
				skin = 1
			}

			flat[i] = sw*sat + kw*skin
		}
	}

	// Pass 2: Sobel magnitude on luma, plus the centre prior.
	cxf, cyf := float64(w-1)/2, float64(h-1)/2
	maxDist := math.Hypot(cxf, cyf)
	out := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			var edge float64
			if x > 0 && y > 0 && x < w-1 && y < h-1 {
				gx := luma[i-w+1] + 2*luma[i+1] + luma[i+w+1] -
					luma[i-w-1] - 2*luma[i-1] - luma[i+w-1]
				gy := luma[i+w-1] + 2*luma[i+w] + luma[i+w+1] -
					luma[i-w-1] - 2*luma[i-w] - luma[i-w+1]
				// Normalise to roughly [0,1]: max |g| is 4*255.
				edge = math.Hypot(gx, gy) / 1020
			}
			var center float64
			if maxDist > 0 {
				d := math.Hypot(float64(x)-cxf, float64(y)-cyf) / maxDist
				center = 1 - d
			}
			out[i] = ew*edge + flat[i] + cw*center
		}
	}
	return out
}

// integral is a summed-area table giving O(1) rectangle means.
type integral struct {
	w, h int
	sum  []float64 // (w+1)*(h+1), zero-padded first row and column
}

func newIntegral(score []float64, w, h int) *integral {
	s := make([]float64, (w+1)*(h+1))
	for y := 0; y < h; y++ {
		var rowSum float64
		for x := 0; x < w; x++ {
			rowSum += score[y*w+x]
			s[(y+1)*(w+1)+(x+1)] = s[y*(w+1)+(x+1)] + rowSum
		}
	}
	return &integral{w: w, h: h, sum: s}
}

// mean returns the average score over r, which must be in [0,w)×[0,h).
func (in *integral) mean(r image.Rectangle) float64 {
	x0, y0, x1, y1 := r.Min.X, r.Min.Y, r.Max.X, r.Max.Y
	stride := in.w + 1
	total := in.sum[y1*stride+x1] - in.sum[y0*stride+x1] -
		in.sum[y1*stride+x0] + in.sum[y0*stride+x0]
	area := float64(r.Dx() * r.Dy())
	if area == 0 {
		return 0
	}
	return total / area
}
