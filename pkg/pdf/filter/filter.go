package filter

import (
	"errors"
	"fmt"
)

// ErrUnsupported is returned (wrapped) when a stream uses a filter this package
// does not implement.
var ErrUnsupported = errors.New("unsupported filter")

// Params holds the optional DecodeParms for a single filter stage. Zero values
// mean "not specified" and the decoder uses PDF defaults.
type Params struct {
	// Predictor and its companions apply to Flate/LZW (PNG/TIFF predictors).
	Predictor        int // 1 = none, 2 = TIFF, 10..15 = PNG
	Colors           int // default 1
	BitsPerComponent int // default 8
	Columns          int // default 1
	// EarlyChange applies to LZWDecode. nil means unspecified (PDF default 1);
	// a non-nil pointer carries an explicit 0 or 1.
	EarlyChange *int
}

// earlyChange returns the effective LZW EarlyChange value, applying the PDF
// default of 1 when unspecified.
func (p Params) earlyChange() int {
	if p.EarlyChange == nil {
		return 1
	}
	return *p.EarlyChange
}

// Stage is one filter in a chain: a filter name (without leading slash) plus its
// decode parameters.
type Stage struct {
	Name   string
	Params Params
}

// Decode applies a chain of filters in order to raw stream bytes and returns the
// decoded data. DCTDecode (JPEG) and similar image-only filters are NOT decoded
// here; see [IsImageFilter] — callers handle those at image-draw time.
func Decode(raw []byte, stages []Stage) ([]byte, error) {
	data := raw
	for i, st := range stages {
		out, err := decodeStage(data, st)
		if err != nil {
			return nil, fmt.Errorf("filter %d (%s): %w", i, st.Name, err)
		}
		data = out
	}
	return data, nil
}

func decodeStage(data []byte, st Stage) ([]byte, error) {
	switch st.Name {
	case "FlateDecode", "Fl":
		out, err := flateDecode(data)
		if err != nil {
			return nil, err
		}
		return applyPredictor(out, st.Params)
	case "LZWDecode", "LZW":
		out, err := lzwDecode(data, st.Params)
		if err != nil {
			return nil, err
		}
		return applyPredictor(out, st.Params)
	case "ASCIIHexDecode", "AHx":
		return asciiHexDecode(data)
	case "ASCII85Decode", "A85":
		return ascii85Decode(data)
	case "RunLengthDecode", "RL":
		return runLengthDecode(data)
	case "DCTDecode", "DCT", "JPXDecode", "JBIG2Decode", "CCITTFaxDecode", "CCF":
		// Image-only filters: not decoded here.
		return nil, fmt.Errorf("%w: %s is an image filter; decode at draw time", ErrUnsupported, st.Name)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, st.Name)
	}
}

// IsImageFilter reports whether the named filter is an image codec that Decode
// deliberately leaves encoded (handled at image-draw time).
func IsImageFilter(name string) bool {
	switch name {
	case "DCTDecode", "DCT", "JPXDecode", "JBIG2Decode", "CCITTFaxDecode", "CCF":
		return true
	}
	return false
}
