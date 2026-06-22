package filter

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
)

// flateDecode inflates zlib- or raw-deflate-compressed data. Most PDFs use a
// zlib header; some malformed producers omit it, so we fall back to raw deflate.
//
// Truncated streams (io.ErrUnexpectedEOF / io.EOF after partial output) are
// tolerated: the partial bytes are returned with no error, since many real-world
// PDFs have a stream whose declared Length is slightly short. Any other
// decompression error is returned so the caller (page-boundary recovery) can
// decide how to degrade.
func flateDecode(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if zr, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
		out, rerr := io.ReadAll(zr)
		_ = zr.Close()
		if rerr == nil || isTruncation(rerr) {
			return out, nil
		}
		// A genuine zlib error: fall through to raw deflate in case the header was
		// spurious, but remember the error to report if that also fails.
		if raw, rawErr := rawDeflate(data); rawErr == nil {
			return raw, nil
		}
		return nil, fmt.Errorf("flate: %w", rerr)
	}
	// No valid zlib header: try raw deflate.
	return rawDeflate(data)
}

func rawDeflate(data []byte) ([]byte, error) {
	fr := flate.NewReader(bytes.NewReader(data))
	out, rerr := io.ReadAll(fr)
	_ = fr.Close()
	if rerr != nil && !isTruncation(rerr) {
		return nil, fmt.Errorf("flate(raw): %w", rerr)
	}
	return out, nil
}

func isTruncation(err error) bool {
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}
