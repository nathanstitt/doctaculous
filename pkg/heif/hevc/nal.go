package hevc

import (
	"encoding/binary"
	"fmt"
)

// NAL unit handling: splitting streams into NAL units (both hvcC
// length-prefixed and Annex-B start-code forms), header parsing, and RBSP
// emulation-prevention removal.

// NAL unit type codes (spec Table 7-1) used by intra still decoding.
const (
	nalTrailN    = 0
	nalBlaWLP    = 16
	nalIdrWRadl  = 19
	nalIdrNLP    = 20
	nalCra       = 21
	nalRsvIrap23 = 23
	nalVPS       = 32
	nalSPS       = 33
	nalPPS       = 34
	nalAUD       = 35
	nalEOS       = 36
	nalEOB       = 37
	nalFD        = 38
	nalSEIPrefix = 39
	nalSEISuffix = 40
)

// nalUnit is one NAL unit: parsed header plus the escaped payload (bytes
// after the 2-byte header, emulation prevention still present).
type nalUnit struct {
	typ        uint8
	layerID    uint8
	temporalID uint8 // TemporalId (nuh_temporal_id_plus1 - 1)
	payload    []byte
}

// parseNAL splits a raw NAL unit into header fields and payload.
func parseNAL(nal []byte) (nalUnit, error) {
	if len(nal) < 2 {
		return nalUnit{}, fmt.Errorf("%w: NAL unit shorter than its header", ErrInvalid)
	}
	if nal[0]>>7 != 0 {
		return nalUnit{}, fmt.Errorf("%w: forbidden_zero_bit set", ErrInvalid)
	}
	h := binary.BigEndian.Uint16(nal)
	tidPlus1 := uint8(h & 7)
	if tidPlus1 == 0 {
		return nalUnit{}, fmt.Errorf("%w: nuh_temporal_id_plus1 is zero", ErrInvalid)
	}
	return nalUnit{
		typ:        uint8(h >> 9 & 0x3f),
		layerID:    uint8(h >> 3 & 0x3f),
		temporalID: tidPlus1 - 1,
		payload:    nal[2:],
	}, nil
}

// isSlice reports whether the NAL type carries a coded slice segment
// (VCL NAL, types 0..31).
func (n nalUnit) isSlice() bool { return n.typ < 32 }

// isIRAP reports an intra-random-access picture (IDR/CRA/BLA), the only
// picture kinds an intra-only decoder accepts.
func (n nalUnit) isIRAP() bool { return n.typ >= nalBlaWLP && n.typ <= nalRsvIrap23 }

// isIDR reports an instantaneous decoder refresh picture (no RPS in its
// slice headers).
func (n nalUnit) isIDR() bool { return n.typ == nalIdrWRadl || n.typ == nalIdrNLP }

// SplitNALs splits an hvcC-style stream of [length][NAL] records, as stored
// in an hvc1 item payload. lengthSize is 1, 2, or 4 (hvcC
// lengthSizeMinusOne + 1).
func SplitNALs(data []byte, lengthSize int) ([][]byte, error) {
	return splitLengthPrefixed(data, lengthSize)
}

// splitLengthPrefixed splits an hvcC-style stream of [length][NAL] records.
// lengthSize is 1, 2, or 4 (hvcC lengthSizeMinusOne + 1).
func splitLengthPrefixed(data []byte, lengthSize int) ([][]byte, error) {
	if lengthSize != 1 && lengthSize != 2 && lengthSize != 4 {
		return nil, fmt.Errorf("%w: NAL length size %d", ErrInvalid, lengthSize)
	}
	var out [][]byte
	for off := 0; off < len(data); {
		if len(data)-off < lengthSize {
			return nil, fmt.Errorf("%w: truncated NAL length prefix", ErrInvalid)
		}
		var n int
		for i := range lengthSize {
			n = n<<8 | int(data[off+i])
		}
		off += lengthSize
		if n == 0 || n > len(data)-off {
			return nil, fmt.Errorf("%w: NAL length %d overruns data", ErrInvalid, n)
		}
		out = append(out, data[off:off+n])
		off += n
	}
	return out, nil
}

// splitAnnexB splits a start-code-delimited (00 00 01 / 00 00 00 01) stream.
func splitAnnexB(data []byte) [][]byte {
	var out [][]byte
	start := -1
	i := 0
	for i+2 < len(data) {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			end := i
			// A 4-byte start code owns its leading zero byte.
			if start >= 0 {
				for end > start && data[end-1] == 0 {
					end--
				}
				if end > start {
					out = append(out, data[start:end])
				}
			}
			start = i + 3
			i += 3
			continue
		}
		i++
	}
	if start >= 0 && start < len(data) {
		end := len(data)
		for end > start && data[end-1] == 0 {
			end--
		}
		if end > start {
			out = append(out, data[start:end])
		}
	}
	return out
}

// unescapeWithMap is unescapeRBSP that additionally reports the positions
// (indexes into the escaped input) of every removed emulation byte. WPP/tile
// entry-point offsets are expressed in escaped bytes, so converting them to
// RBSP offsets needs this map.
func unescapeWithMap(p []byte) (rbsp []byte, removed []int) {
	out := make([]byte, 0, len(p))
	zeros := 0
	for i := range p {
		if zeros >= 2 && p[i] == 3 {
			zeros = 0
			removed = append(removed, i)
			continue
		}
		if p[i] == 0 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, p[i])
	}
	return out, removed
}

// escapedToRBSP converts an offset in the escaped payload to the
// corresponding RBSP offset given the removed-byte positions.
func escapedToRBSP(off int, removed []int) int {
	n := 0
	for _, r := range removed {
		if r < off {
			n++
		}
	}
	return off - n
}

// rbspToEscaped is the inverse of escapedToRBSP.
func rbspToEscaped(off int, removed []int) int {
	for _, r := range removed {
		if r <= off {
			off++
		}
	}
	return off
}

// unescapeRBSP removes emulation-prevention bytes (00 00 03 -> 00 00) from a
// NAL payload, yielding the raw byte sequence payload the bit readers parse.
func unescapeRBSP(p []byte) []byte {
	// Fast path: no emulation bytes present.
	hasEP := false
	for i := 2; i < len(p); i++ {
		if p[i] == 3 && p[i-1] == 0 && p[i-2] == 0 {
			hasEP = true
			break
		}
	}
	if !hasEP {
		return p
	}
	out := make([]byte, 0, len(p))
	zeros := 0
	for i := range p {
		if zeros >= 2 && p[i] == 3 {
			zeros = 0
			continue // drop the emulation byte; next byte is data
		}
		if p[i] == 0 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, p[i])
	}
	return out
}
