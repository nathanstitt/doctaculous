package function

import (
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// sampledFunc is a Type 0 (sampled) function. It stores a multidimensional table
// of n-component samples and evaluates by encoding the m inputs into sample
// coordinates, multilinearly interpolating between the 2^m surrounding samples,
// and decoding the result onto /Range.
//
// The 1-input case (m == 1, used by shadings) is the critical path and is fully
// interpolated. The general m-dimensional case is also implemented via a
// multilinear (2^m-corner) blend.
type sampledFunc struct {
	domain []float64 // 2m
	rng    []float64 // 2n
	size   []int     // m: samples per input dimension
	bps    int       // bits per sample
	encode []float64 // 2m
	decode []float64 // 2n

	m       int
	n       int
	samples []uint32 // flat table: total*n values, in [0, 2^bps-1]
	maxVal  float64  // 2^bps - 1
	strides []int    // per-dimension stride into samples (in units of samples, not components)
}

func parseSampled(doc *pdf.Document, obj pdf.Object) (Func, error) {
	stream, ok := resolve(doc, obj).(*pdf.Stream)
	if !ok {
		return nil, fmt.Errorf("function: Type 0 must be a stream")
	}
	dict := stream.Dict

	domain, err := requireFloatArray(doc, dict, "Domain")
	if err != nil {
		return nil, err
	}
	rng, err := requireFloatArray(doc, dict, "Range")
	if err != nil {
		return nil, err
	}
	if len(domain)%2 != 0 || len(rng)%2 != 0 || len(domain) == 0 || len(rng) == 0 {
		return nil, fmt.Errorf("function: Type 0 /Domain (%d) or /Range (%d) malformed", len(domain), len(rng))
	}
	m := len(domain) / 2
	n := len(rng) / 2

	sizeArr := getArray(doc, dict["Size"])
	if len(sizeArr) != m {
		return nil, fmt.Errorf("function: Type 0 /Size has %d entries, want m=%d", len(sizeArr), m)
	}
	size := make([]int, m)
	total := 1
	for i, e := range sizeArr {
		s, ok := pdf.IntValue(resolve(doc, e))
		if !ok || s < 1 {
			return nil, fmt.Errorf("function: Type 0 /Size[%d] invalid", i)
		}
		size[i] = s
		total *= s
	}

	bps, ok := getInt(doc, dict["BitsPerSample"])
	if !ok || !validBPS(bps) {
		return nil, fmt.Errorf("function: Type 0 invalid /BitsPerSample %d", bps)
	}

	// /Encode default: [0 (Size_0-1) 0 (Size_1-1) ...]
	encode := floatArray(doc, dict["Encode"])
	if encode == nil {
		encode = make([]float64, 2*m)
		for i := 0; i < m; i++ {
			encode[2*i] = 0
			encode[2*i+1] = float64(size[i] - 1)
		}
	}
	if len(encode) != 2*m {
		return nil, fmt.Errorf("function: Type 0 /Encode has %d values, want 2m=%d", len(encode), 2*m)
	}

	// /Decode default: equal to /Range.
	decode := floatArray(doc, dict["Decode"])
	if decode == nil {
		decode = append([]float64(nil), rng...)
	}
	if len(decode) != 2*n {
		return nil, fmt.Errorf("function: Type 0 /Decode has %d values, want 2n=%d", len(decode), 2*n)
	}

	data, _, err := doc.DecodedStream(stream)
	if err != nil {
		return nil, fmt.Errorf("function: Type 0 decode stream: %w", err)
	}

	// Unpack total*n samples of bps bits each, MSB-first big-endian, with no
	// per-row byte padding (samples are a single continuous bit stream).
	count := total * n
	samples := make([]uint32, count)
	br := sampleReader{data: data, bps: bps}
	for i := 0; i < count; i++ {
		samples[i] = br.next()
	}
	if br.overran {
		return nil, fmt.Errorf("function: Type 0 sample data too short for %d samples at %d bits", count, bps)
	}

	// Row-major strides: index = sum(coord[i] * stride[i]); dimension 0 varies
	// fastest per the PDF spec ("first dimension varying fastest").
	strides := make([]int, m)
	stride := 1
	for i := 0; i < m; i++ {
		strides[i] = stride
		stride *= size[i]
	}

	return &sampledFunc{
		domain:  domain,
		rng:     rng,
		size:    size,
		bps:     bps,
		encode:  encode,
		decode:  decode,
		m:       m,
		n:       n,
		samples: samples,
		maxVal:  float64((uint64(1) << uint(bps)) - 1),
		strides: strides,
	}, nil
}

func validBPS(b int) bool {
	switch b {
	case 1, 2, 4, 8, 12, 16, 24, 32:
		return true
	default:
		return false
	}
}

func (f *sampledFunc) NumOutputs() int { return f.n }

func (f *sampledFunc) Eval(in []float64) []float64 {
	// Encode each input into a (fractional) sample coordinate in its dimension.
	e := make([]float64, f.m)
	for i := 0; i < f.m; i++ {
		x := f.domain[2*i]
		if i < len(in) {
			x = in[i]
		}
		x = clamp(x, f.domain[2*i], f.domain[2*i+1])
		// Map domain -> encode range, then clamp to [0, Size_i-1].
		ev := interp(x, f.domain[2*i], f.domain[2*i+1], f.encode[2*i], f.encode[2*i+1])
		ev = clamp(ev, 0, float64(f.size[i]-1))
		e[i] = ev
	}

	// Multilinear interpolation over the 2^m surrounding grid corners.
	raw := make([]float64, f.n)
	lo := make([]int, f.m)
	frac := make([]float64, f.m)
	for i := 0; i < f.m; i++ {
		l := int(e[i])
		if l >= f.size[i]-1 {
			l = f.size[i] - 1
			if l > 0 {
				l-- // keep room for the upper corner unless the dim is size 1
			}
		}
		lo[i] = l
		frac[i] = e[i] - float64(l)
		if f.size[i] == 1 {
			frac[i] = 0
		}
	}

	corners := 1 << uint(f.m)
	for c := 0; c < corners; c++ {
		weight := 1.0
		base := 0
		for i := 0; i < f.m; i++ {
			upper := (c>>uint(i))&1 == 1
			coord := lo[i]
			if upper {
				if f.size[i] == 1 {
					// no upper neighbor; this corner duplicates the lower one but with
					// weight (1-frac)=1, and the upper-weight branch contributes 0.
					weight = 0
					break
				}
				coord = lo[i] + 1
				weight *= frac[i]
			} else {
				weight *= 1 - frac[i]
			}
			base += coord * f.strides[i]
		}
		if weight == 0 {
			continue
		}
		off := base * f.n
		for j := 0; j < f.n; j++ {
			raw[j] += weight * float64(f.samples[off+j])
		}
	}

	// Decode each component: sample/maxVal in [0,1] mapped onto /Decode, then
	// clamped to /Range.
	out := make([]float64, f.n)
	for j := 0; j < f.n; j++ {
		s := raw[j] / f.maxVal
		out[j] = interp(s, 0, 1, f.decode[2*j], f.decode[2*j+1])
	}
	clampToRange(out, f.rng)
	return out
}

// sampleReader unpacks consecutive bps-bit unsigned samples from a continuous
// big-endian, MSB-first bit stream (the Type 0 sample packing). Unlike an image
// row reader it does not pad to byte boundaries between samples.
type sampleReader struct {
	data    []byte
	bps     int
	bytePos int
	bitPos  int // 0..7, MSB-first within the current byte
	overran bool
}

// next returns the next sample (0 .. 2^bps-1). Reading past the end sets overran
// and yields 0 for the missing bits.
func (r *sampleReader) next() uint32 {
	var v uint32
	for i := 0; i < r.bps; i++ {
		v <<= 1
		if r.bytePos < len(r.data) {
			bit := (r.data[r.bytePos] >> uint(7-r.bitPos)) & 1
			v |= uint32(bit)
		} else {
			r.overran = true
		}
		r.bitPos++
		if r.bitPos == 8 {
			r.bitPos = 0
			r.bytePos++
		}
	}
	return v
}
