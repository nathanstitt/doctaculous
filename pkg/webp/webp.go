// Package webp adds the container checks that golang.org/x/image/webp does
// not make, and registers the WebP decoder with the standard image package.
//
// x/image/webp decodes WebP still images — lossy VP8, lossless VP8L, and the
// extended VP8X container including an alpha plane — but it does not handle
// animation. Its treatment of an animated file is actively misleading:
// DecodeConfig parses the VP8X chunk, ignores the animation flag (the decoder
// declares animationBit and never reads it), and returns the canvas size with
// no error, while Decode walks the RIFF chunks looking for a still frame,
// finds only ANIM/ANMF, runs to io.EOF and reports "webp: invalid format" —
// the same error a corrupt file produces.
//
// A caller cannot tell "this file is fine, I just don't do animation" from
// "these bytes are garbage", so IsAnimated reads the flag x/image ignores and
// ErrAnimated names the case. Decode and DecodeConfig wrap x/image's decoders
// to reject animation up front, so a caller that degrades on error can log
// something true.
//
// Importing this package also registers x/image/webp's decoder with the image
// package, so image.Decode handles WebP toolkit-wide. Note that the sniffing
// path cannot carry the animation check — see the comment on FormatName — so
// code that must distinguish animation calls Decode/DecodeConfig here.
package webp

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"

	xwebp "golang.org/x/image/webp"
)

// ErrAnimated reports a well-formed animated WebP. The toolkit reads still
// images only; an animated file is a deliberate refusal, not a decode failure.
var ErrAnimated = errors.New("webp: animated images are not supported")

// Header sizes and offsets in the RIFF container, per the WebP spec:
// "RIFF" + a 4-byte little-endian file size + "WEBP", then the first chunk's
// 4-byte ID and 4-byte length. VP8X, when present, is always that first chunk
// and its payload opens with a flags byte carrying the animation bit.
const (
	riffHeaderSize = 12 // "RIFF" + size + "WEBP"
	chunkHeaderLen = 8  // 4-byte chunk ID + 4-byte length
	vp8xFlagsIndex = riffHeaderSize + chunkHeaderLen
	animationBit   = 1 << 1
)

// IsAnimated reports whether data is an animated WebP: an extended (VP8X)
// container with the animation bit set in its flags byte. It reports false for
// a still WebP, and for anything that is not a WebP at all — it is a cheap
// header check, not a validator, so a caller still decodes to learn whether
// the bytes are sound.
func IsAnimated(data []byte) bool {
	if len(data) < vp8xFlagsIndex+1 {
		return false
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return false
	}
	if string(data[riffHeaderSize:riffHeaderSize+4]) != "VP8X" {
		return false
	}
	return data[vp8xFlagsIndex]&animationBit != 0
}

// Decode decodes a still WebP image. An animated WebP returns ErrAnimated
// rather than x/image's generic "invalid format", so a caller degrading on
// error can say which of the two happened.
func Decode(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("webp: read: %w", err)
	}
	if IsAnimated(data) {
		return nil, ErrAnimated
	}
	return xwebp.Decode(bytes.NewReader(data))
}

// DecodeConfig decodes the dimensions and color model of a still WebP.
//
// Unlike x/image/webp's DecodeConfig, which returns the canvas size of an
// animated file as though it were decodable, this reports ErrAnimated — so a
// caller that sniffs a config before committing to a decode does not learn the
// size of an image it will then fail to read.
func DecodeConfig(r io.Reader) (image.Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return image.Config{}, fmt.Errorf("webp: read: %w", err)
	}
	if IsAnimated(data) {
		return image.Config{}, ErrAnimated
	}
	return xwebp.DecodeConfig(bytes.NewReader(data))
}

// FormatName is the name x/image/webp registers with the image package, and
// so the string image.Decode and image.DecodeConfig return for any WebP file.
// Callers switching on the sniffed format name compare against this.
const FormatName = "webp"

// Importing x/image/webp above installs its image.RegisterFormat entry as a
// side effect, so image.Decode and image.DecodeConfig handle WebP anywhere in
// the toolkit without a separate blank import.
//
// This package deliberately does NOT register a competing entry. image.sniff
// returns the FIRST registered format whose magic matches and iterates in
// registration order, and an importing package's init always runs after the
// package it imports — so an entry added here could never be reached, whatever
// magic it used. Rejecting animation through the sniffing path is therefore
// not possible by registration; callers that must not be fooled by an animated
// file call Decode/DecodeConfig here directly, or check IsAnimated on the
// bytes before sniffing. imageconv.TranscodeToPNG does the latter, which
// covers the writers that re-encode images through format sniffing.
