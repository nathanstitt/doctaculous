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
		out, rerr := readAllBounded(zr)
		_ = zr.Close()
		if errors.Is(rerr, ErrTooLarge) {
			return nil, rerr
		}
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
	out, rerr := readAllBounded(fr)
	_ = fr.Close()
	if errors.Is(rerr, ErrTooLarge) {
		return nil, rerr
	}
	if rerr != nil && !isTruncation(rerr) {
		return nil, fmt.Errorf("flate(raw): %w", rerr)
	}
	return out, nil
}

// readAllBounded is io.ReadAll with a ceiling, for decompressors whose output
// size is controlled by the file rather than by us. Reading through a
// LimitReader means the memory for an over-large stream is never allocated at
// all, which an after-the-fact length check cannot achieve: by the time it could
// run, the allocation has already happened.
//
// One byte past the limit is read so that hitting it exactly is distinguishable
// from a stream that merely ends there.
func readAllBounded(r io.Reader) ([]byte, error) {
	limit := maxDecodedSize
	out, err := io.ReadAll(io.LimitReader(r, limit+1))
	if int64(len(out)) > limit {
		return nil, fmt.Errorf("%w: exceeds the %d-byte limit", ErrTooLarge, limit)
	}
	return out, err
}

func isTruncation(err error) bool {
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}
