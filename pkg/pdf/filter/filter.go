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

	// CCITT fields apply to CCITTFaxDecode (the /DecodeParms of a fax stream).
	// Zero values mean "not specified" and the decoder uses PDF defaults
	// (K=0, Columns=1728, Rows=0/unbounded, BlackIs1=false, EncodedByteAlign=false,
	// EndOfBlock=true). They are only consulted for the CCITTFaxDecode stage.
	CCITT *CCITTParams
}

// CCITTParams holds the CCITTFaxDecode decode parameters (PDF 32000-1 Table 11).
type CCITTParams struct {
	// K selects the coding scheme: K<0 = pure two-dimensional (Group 4 / T.6),
	// K=0 = one-dimensional (Group 3 1D / T.4), K>0 = mixed 1D/2D (Group 3 2D).
	K int
	// Columns is the image width in pixels (default 1728).
	Columns int
	// Rows is the image height in pixels; 0 means "decode until EOD/end of data".
	Rows int
	// BlackIs1 inverts the default sense: when false (default) a 0 bit in the
	// output is black and a 1 bit is white; when true, 1 is black.
	BlackIs1 bool
	// EncodedByteAlign pads each encoded row to the next byte boundary.
	EncodedByteAlign bool
	// EndOfBlock indicates an end-of-block pattern terminates the stream; when
	// true (default) the decoder may stop on EOFB even before Rows are produced.
	EndOfBlock bool
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
	case "CCITTFaxDecode", "CCF":
		// CCITT fax decodes to packed 1-bpp sample rows, which the generic image
		// decoder then unpacks as a 1-bpc DeviceGray/bilevel image.
		return ccittDecode(data, st.Params)
	case "DCTDecode", "DCT", "JPXDecode", "JBIG2Decode":
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
	case "DCTDecode", "DCT", "JPXDecode", "JBIG2Decode":
		return true
	}
	return false
}
