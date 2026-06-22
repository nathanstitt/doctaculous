package function

import (
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// resolve follows indirect references via doc when doc is non-nil; otherwise it
// returns the object unchanged (which is correct for fully direct objects).
func resolve(doc *pdf.Document, o pdf.Object) pdf.Object {
	if doc != nil {
		return doc.Resolve(o)
	}
	return o
}

// getDict returns a Dict for o (or a Stream's dict), or nil.
func getDict(doc *pdf.Document, o pdf.Object) pdf.Dict {
	switch v := resolve(doc, o).(type) {
	case pdf.Dict:
		return v
	case *pdf.Stream:
		return v.Dict
	default:
		return nil
	}
}

// getInt resolves o to an int value.
func getInt(doc *pdf.Document, o pdf.Object) (int, bool) {
	return pdf.IntValue(resolve(doc, o))
}

// getArray resolves o to an Array, or nil.
func getArray(doc *pdf.Document, o pdf.Object) pdf.Array {
	if a, ok := resolve(doc, o).(pdf.Array); ok {
		return a
	}
	return nil
}

// floatArray resolves o to an array of float64. It returns nil if o is absent or
// not an array of numbers.
func floatArray(doc *pdf.Document, o pdf.Object) []float64 {
	arr := getArray(doc, o)
	if arr == nil {
		return nil
	}
	out := make([]float64, len(arr))
	for i, e := range arr {
		v, ok := pdf.Number(resolve(doc, e))
		if !ok {
			return nil
		}
		out[i] = v
	}
	return out
}

// requireFloatArray is floatArray that errors when the key is missing/invalid.
func requireFloatArray(doc *pdf.Document, dict pdf.Dict, key pdf.Name) ([]float64, error) {
	v := floatArray(doc, dict[key])
	if v == nil {
		return nil, fmt.Errorf("function: missing or invalid /%s array", key)
	}
	return v, nil
}

// clamp constrains x to [lo, hi]. lo and hi may be given in either order.
func clamp(x, lo, hi float64) float64 {
	if lo > hi {
		lo, hi = hi, lo
	}
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// interp linearly maps x from [xmin,xmax] onto [ymin,ymax]. When the source
// interval is degenerate it returns ymin.
func interp(x, xmin, xmax, ymin, ymax float64) float64 {
	if xmax == xmin {
		return ymin
	}
	return ymin + (x-xmin)*(ymax-ymin)/(xmax-xmin)
}

// clampToDomain clamps each input component to its [Domain[2i], Domain[2i+1]]
// pair. It returns a new slice of length len(domain)/2; extra inputs are ignored
// and missing inputs are treated as the domain minimum.
func clampToDomain(in, domain []float64) []float64 {
	m := len(domain) / 2
	out := make([]float64, m)
	for i := 0; i < m; i++ {
		lo, hi := domain[2*i], domain[2*i+1]
		x := lo
		if i < len(in) {
			x = in[i]
		}
		out[i] = clamp(x, lo, hi)
	}
	return out
}

// clampToRange clamps each output component to its [Range[2j], Range[2j+1]]
// pair, in place. A nil or short range leaves components unclamped.
func clampToRange(out, rng []float64) {
	for j := range out {
		if 2*j+1 < len(rng) {
			out[j] = clamp(out[j], rng[2*j], rng[2*j+1])
		}
	}
}
