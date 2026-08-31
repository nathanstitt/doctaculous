package hevc

import "fmt"

// CABAC arithmetic decoding core (spec 9.3.4.3). The engine is written as a
// literal transcription of the spec's flowcharts — integer-only, with the
// exact renormalization and state-transition tables — because bit-exact
// output depends on it. Contexts live outside the engine (see ctxinit.go) so
// WPP/tile snapshots copy plain slices.

// rangeTabLPS (spec Table 9-46): LPS range given pStateIdx and the two
// quantizer bits of ivlCurrRange.
var rangeTabLPS = [64][4]uint32{
	{128, 176, 208, 240}, {128, 167, 197, 227}, {128, 158, 187, 216}, {123, 150, 178, 205},
	{116, 142, 169, 195}, {111, 135, 160, 185}, {105, 128, 152, 175}, {100, 122, 144, 166},
	{95, 116, 137, 158}, {90, 110, 130, 150}, {85, 104, 123, 142}, {81, 99, 117, 135},
	{77, 94, 111, 128}, {73, 89, 105, 122}, {69, 85, 100, 116}, {66, 80, 95, 110},
	{62, 76, 90, 104}, {59, 72, 86, 99}, {56, 69, 81, 94}, {53, 65, 77, 89},
	{51, 62, 73, 85}, {48, 59, 69, 80}, {46, 56, 66, 76}, {43, 53, 63, 72},
	{41, 50, 59, 69}, {39, 48, 56, 65}, {37, 45, 54, 62}, {35, 43, 51, 59},
	{33, 41, 48, 56}, {32, 39, 46, 53}, {30, 37, 43, 50}, {29, 35, 41, 48},
	{27, 33, 39, 45}, {26, 31, 37, 43}, {24, 30, 35, 41}, {23, 28, 33, 39},
	{22, 27, 32, 37}, {21, 26, 30, 35}, {20, 24, 29, 33}, {19, 23, 27, 31},
	{18, 22, 26, 30}, {17, 21, 25, 28}, {16, 20, 23, 27}, {15, 19, 22, 25},
	{14, 18, 21, 24}, {14, 17, 20, 23}, {13, 16, 19, 22}, {12, 15, 18, 21},
	{12, 14, 17, 20}, {11, 14, 16, 19}, {11, 13, 15, 18}, {10, 12, 15, 17},
	{10, 12, 14, 16}, {9, 11, 13, 15}, {9, 11, 12, 14}, {8, 10, 12, 14},
	{8, 9, 11, 13}, {7, 9, 11, 12}, {7, 9, 10, 12}, {7, 8, 10, 11},
	{6, 8, 9, 11}, {6, 7, 9, 10}, {6, 7, 8, 9}, {2, 2, 2, 2},
}

// transIdxLPS (spec Table 9-47): next pStateIdx after an LPS decision.
// The MPS transition is min(pStateIdx+1, 62).
var transIdxLPS = [64]uint8{
	0, 0, 1, 2, 2, 4, 4, 5, 6, 7, 8, 9, 9, 11, 11, 12,
	13, 13, 15, 15, 16, 16, 18, 18, 19, 19, 21, 21, 22, 22, 23, 24,
	24, 25, 26, 26, 27, 27, 28, 29, 29, 30, 30, 30, 31, 32, 32, 33,
	33, 33, 34, 34, 35, 35, 35, 36, 36, 36, 37, 37, 37, 38, 38, 63,
}

// ctxModel is one context: probability state index and MPS value.
type ctxModel struct {
	state uint8
	mps   uint8
}

// traceFn is the decode-trace seam: when set on a cabacDecoder every context
// bin decision is reported. Tests diff these lines against reference-decoder
// traces to localize CABAC divergence; production leaves it nil.
type traceFn func(ctxIdx int, bin uint32, state uint8, mps uint8, ivlRange, ivlOffset uint32)

// cabacDecoder is the arithmetic decoding engine over one byte-aligned
// substream.
type cabacDecoder struct {
	r         *bitReader
	ivlRange  uint32
	ivlOffset uint32
	trace     traceFn
}

// initCABAC starts arithmetic decoding at the reader's current position,
// which must be byte-aligned (spec 9.3.1).
func (c *cabacDecoder) init(r *bitReader) error {
	if !r.byteAligned() {
		return fmt.Errorf("%w: CABAC init off byte boundary", ErrInvalid)
	}
	c.r = r
	c.ivlRange = 510
	c.ivlOffset = r.u(9)
	if r.failed || c.ivlOffset >= 510 {
		return fmt.Errorf("%w: CABAC init offset", ErrInvalid)
	}
	return nil
}

// decodeBin decodes one context-coded bin (spec 9.3.4.3.2).
func (c *cabacDecoder) decodeBin(ctxs []ctxModel, ctxIdx int) uint32 {
	ctx := &ctxs[ctxIdx]
	preState := ctx.state
	preMPS := ctx.mps
	qIdx := c.ivlRange >> 6 & 3
	lpsRange := rangeTabLPS[ctx.state][qIdx]
	c.ivlRange -= lpsRange
	var bin uint32
	if c.ivlOffset >= c.ivlRange {
		// LPS path.
		bin = uint32(1 - ctx.mps)
		c.ivlOffset -= c.ivlRange
		c.ivlRange = lpsRange
		if ctx.state == 0 {
			ctx.mps = 1 - ctx.mps
		}
		ctx.state = transIdxLPS[ctx.state]
	} else {
		bin = uint32(ctx.mps)
		if ctx.state < 62 {
			ctx.state++
		}
	}
	// Renormalization (spec 9.3.4.3.3).
	for c.ivlRange < 256 {
		c.ivlRange <<= 1
		c.ivlOffset = c.ivlOffset<<1 | c.r.u(1)
	}
	if c.trace != nil {
		// The trace reports the PRE-decode context state: that is what a
		// reference decoder's log shows, so divergence diffs align.
		c.trace(ctxIdx, bin, preState, preMPS, c.ivlRange, c.ivlOffset)
	}
	return bin
}

// decodeBypass decodes one bypass bin (spec 9.3.4.3.4).
func (c *cabacDecoder) decodeBypass() uint32 {
	c.ivlOffset = c.ivlOffset<<1 | c.r.u(1)
	var bin uint32
	if c.ivlOffset >= c.ivlRange {
		c.ivlOffset -= c.ivlRange
		bin = 1
	}
	if c.trace != nil {
		c.trace(-1, bin, 0, 0, c.ivlRange, c.ivlOffset)
	}
	return bin
}

// decodeBypassBits decodes n bypass bins as an unsigned MSB-first value.
func (c *cabacDecoder) decodeBypassBits(n int) uint32 {
	var v uint32
	for range n {
		v = v<<1 | c.decodeBypass()
	}
	return v
}

// decodeTerminate decodes an end-of-slice / termination bin
// (spec 9.3.4.3.5). A result of 1 means terminate.
func (c *cabacDecoder) decodeTerminate() uint32 {
	c.ivlRange -= 2
	var bin uint32
	if c.ivlOffset >= c.ivlRange {
		bin = 1
	} else {
		for c.ivlRange < 256 {
			c.ivlRange <<= 1
			c.ivlOffset = c.ivlOffset<<1 | c.r.u(1)
		}
	}
	if c.trace != nil {
		c.trace(-2, bin, 0, 0, c.ivlRange, c.ivlOffset)
	}
	return bin
}

// failed reports whether the underlying reader ran out of data (a malformed
// stream: CABAC never legitimately reads past the slice segment).
func (c *cabacDecoder) failed() bool { return c.r.failed }
