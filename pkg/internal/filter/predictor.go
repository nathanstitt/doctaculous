package filter

import "fmt"

// applyPredictor reverses a PNG or TIFF predictor applied before compression.
// Predictor 1 (or unset) means no predictor.
func applyPredictor(data []byte, p Params) ([]byte, error) {
	pred := p.Predictor
	if pred == 0 || pred == 1 {
		return data, nil
	}
	colors := p.Colors
	if colors == 0 {
		colors = 1
	}
	bpc := p.BitsPerComponent
	if bpc == 0 {
		bpc = 8
	}
	columns := p.Columns
	if columns == 0 {
		columns = 1
	}

	bytesPerPixel := (colors*bpc + 7) / 8
	rowLen := (colors*bpc*columns + 7) / 8
	if rowLen <= 0 {
		return data, nil
	}

	if pred == 2 {
		return tiffPredictor(data, colors, bpc, columns, rowLen)
	}
	// PNG predictors (10..15): each row is prefixed with a filter-type byte.
	return pngPredictor(data, bytesPerPixel, rowLen)
}

func pngPredictor(data []byte, bpp, rowLen int) ([]byte, error) {
	stride := rowLen + 1 // +1 for the per-row filter tag
	if stride <= 1 {
		return data, nil
	}
	nRows := len(data) / stride
	out := make([]byte, 0, nRows*rowLen)
	prev := make([]byte, rowLen)
	cur := make([]byte, rowLen)
	for r := range nRows {
		off := r * stride
		ft := data[off]
		row := data[off+1 : off+1+rowLen]
		copy(cur, row)
		switch ft {
		case 0: // None
		case 1: // Sub
			for i := bpp; i < rowLen; i++ {
				cur[i] += cur[i-bpp]
			}
		case 2: // Up
			for i := range rowLen {
				cur[i] += prev[i]
			}
		case 3: // Average
			for i := range rowLen {
				var left byte
				if i >= bpp {
					left = cur[i-bpp]
				}
				cur[i] += byte((int(left) + int(prev[i])) / 2)
			}
		case 4: // Paeth
			for i := range rowLen {
				var left, upLeft byte
				if i >= bpp {
					left = cur[i-bpp]
					upLeft = prev[i-bpp]
				}
				cur[i] += paeth(left, prev[i], upLeft)
			}
		default:
			return nil, fmt.Errorf("png predictor: unknown filter type %d", ft)
		}
		out = append(out, cur...)
		copy(prev, cur)
	}
	return out, nil
}

func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa := abs(p - int(a))
	pb := abs(p - int(b))
	pc := abs(p - int(c))
	switch {
	case pa <= pb && pa <= pc:
		return a
	case pb <= pc:
		return b
	default:
		return c
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// tiffPredictor reverses TIFF predictor 2 (horizontal differencing). Only the
// 8-bit case is common; other bit depths are passed through unchanged.
func tiffPredictor(data []byte, colors, bpc, columns, rowLen int) ([]byte, error) {
	if bpc != 8 {
		return data, nil
	}
	out := make([]byte, len(data))
	copy(out, data)
	nRows := len(out) / rowLen
	for r := range nRows {
		row := out[r*rowLen : (r+1)*rowLen]
		for col := 1; col < columns; col++ {
			for c := range colors {
				idx := col*colors + c
				prev := (col-1)*colors + c
				if idx < len(row) && prev < len(row) {
					row[idx] += row[prev]
				}
			}
		}
	}
	return out, nil
}
