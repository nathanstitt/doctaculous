// Package hevc decodes intra-coded HEVC (H.265) still pictures in pure Go.
//
// It is the codec layer beneath pkg/heif and knows nothing about ISOBMFF
// boxes or image.Image: input is raw parameter-set and slice NAL units,
// output is a planar YCbCr frame. Only the tools used by intra still
// pictures are implemented — there is no inter prediction, motion
// compensation, or reference-picture management. Supported streams are
// 4:2:0, 8- or 10-bit, using the Main/Main 10/Main Still Picture toolset;
// support is gated on the actual feature switches in the parameter sets
// (chroma format, bit depth, range-extension flags), not on the signaled
// profile label, because real encoders (x265) label all-intra streams with
// the RExt Main Intra profile while using only Main-profile tools.
package hevc

import "errors"

// ErrInvalid reports a malformed bitstream.
var ErrInvalid = errors.New("hevc: invalid bitstream")

// ErrUnsupported reports a well-formed stream using features outside the
// intra-only 4:2:0 8/10-bit scope (inter slices, other chroma formats,
// range-extension coding tools, scalable/multiview layers).
var ErrUnsupported = errors.New("hevc: unsupported")

// ParamSets carries raw parameter-set NAL units (with NAL headers, without
// start codes or length prefixes), as stored in an hvcC configuration record.
type ParamSets struct {
	VPS [][]byte
	SPS [][]byte
	PPS [][]byte
}
