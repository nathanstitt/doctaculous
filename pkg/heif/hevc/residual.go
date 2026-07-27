package hevc

// residual_coding (spec 7.3.8.11 syntax, 9.3.4.2.5-9.3.4.2.9 context
// derivations): last significant position, per-sub-block significance maps,
// greater1/greater2 flags, sign hiding, and Golomb-Rice remaining levels.

// sigCtxIdxMap4x4 (spec 9.3.4.2.5) for 4x4 blocks, indexed (yC<<2)+xC.
var sigCtxIdxMap4x4 = [16]int{0, 1, 4, 5, 2, 3, 4, 5, 6, 6, 8, 8, 7, 7, 8, 8}

// decodeResidual parses one transform block's coefficients into block
// (raster order, stride n). log2Size in [2,5]; scanIdx: 0 diag, 1 horiz,
// 2 vert. Returns whether transform_skip_flag was decoded as 1.
func (d *sliceDecoder) decodeResidual(block []int32, log2Size, cIdx, scanIdx int, bypass bool) (transformSkip bool, err error) {
	n := 1 << log2Size
	c := &d.cabac

	if d.pps.transformSkipEnabled && !bypass && log2Size == 2 {
		ctx := ctxTransformSkipLuma
		if cIdx > 0 {
			ctx = ctxTransformSkipChroma
		}
		transformSkip = c.decodeBin(d.ctx, ctx) == 1
	}

	// Last significant coefficient position (9.3.4.2.3).
	var ctxOffset, ctxShift int
	if cIdx == 0 {
		ctxOffset = 3*(log2Size-2) + (log2Size-1)>>2
		ctxShift = (log2Size + 1) >> 2
	} else {
		ctxOffset = 15
		ctxShift = log2Size - 2
	}
	maxPrefix := (log2Size << 1) - 1
	lastX := 0
	for lastX < maxPrefix && c.decodeBin(d.ctx, ctxLastSigXPrefix+ctxOffset+(lastX>>ctxShift)) == 1 {
		lastX++
	}
	lastY := 0
	for lastY < maxPrefix && c.decodeBin(d.ctx, ctxLastSigYPrefix+ctxOffset+(lastY>>ctxShift)) == 1 {
		lastY++
	}
	if lastX > 3 {
		suffixLen := (lastX >> 1) - 1
		lastX = (2 + lastX&1) << suffixLen
		lastX += int(c.decodeBypassBits(suffixLen))
	}
	if lastY > 3 {
		suffixLen := (lastY >> 1) - 1
		lastY = (2 + lastY&1) << suffixLen
		lastY += int(c.decodeBypassBits(suffixLen))
	}
	if scanIdx == 2 {
		lastX, lastY = lastY, lastX
	}

	// Scan setup: positions within a 4x4 sub-block and the sub-block grid.
	subScan := diagScan[4]
	sbScan := diagScan[n>>2]
	switch scanIdx {
	case 1:
		subScan = horizScan[4]
		sbScan = horizScan[n>>2]
	case 2:
		subScan = vertScan[4]
		sbScan = vertScan[n>>2]
	}
	numSb := (n >> 2) * (n >> 2)

	// Locate the last coefficient in scan terms.
	lastSbX, lastSbY := lastX>>2, lastY>>2
	lastSbIdx := -1
	for i, p := range sbScan {
		if int(p.x) == lastSbX && int(p.y) == lastSbY {
			lastSbIdx = i
			break
		}
	}
	lastPosInSb := -1
	for i, p := range subScan {
		if int(p.x) == lastX&3 && int(p.y) == lastY&3 {
			lastPosInSb = i
			break
		}
	}
	if lastSbIdx < 0 || lastPosInSb < 0 {
		return false, errInvalidStream("last significant position")
	}
	dbg("  TB cIdx=%d n=%d scan=%d last=(%d,%d) tskip=%v", cIdx, n, scanIdx, lastX, lastY, transformSkip)

	codedSb := make([]bool, numSb)
	codedSb[0] = true
	codedSb[lastSbIdx] = true

	// greater1Ctx of the previously processed sub-block, for ctxSet
	// derivation (9.3.4.2.6).
	prevGreater1Ctx := -1

	for sb := lastSbIdx; sb >= 0; sb-- {
		sbX, sbY := int(sbScan[sb].x), int(sbScan[sb].y)
		inferSbDcSig := false
		if sb != lastSbIdx && sb != 0 {
			// coded_sub_block_flag context (9.3.4.2.4): right/below flags.
			csbfCtx := 0
			if sbX+1 < n>>2 && sbGet(codedSb, sbScan, sbX+1, sbY) {
				csbfCtx = 1
			}
			if sbY+1 < n>>2 && sbGet(codedSb, sbScan, sbX, sbY+1) {
				csbfCtx = 1
			}
			ctx := ctxCodedSubBlock + csbfCtx
			if cIdx > 0 {
				ctx += 2
			}
			codedSb[sb] = c.decodeBin(d.ctx, ctx) == 1
			inferSbDcSig = codedSb[sb]
		}
		if !codedSb[sb] {
			continue
		}

		// Significance map.
		startPos := 15
		if sb == lastSbIdx {
			startPos = lastPosInSb - 1
		}
		var sigPos []int // scan positions (in subScan index) with nonzero coeffs, decreasing
		if sb == lastSbIdx {
			sigPos = append(sigPos, lastPosInSb)
		}
		for pos := startPos; pos >= 0; pos-- {
			xP, yP := int(subScan[pos].x), int(subScan[pos].y)
			xC, yC := (sbX<<2)+xP, (sbY<<2)+yP
			if pos == 0 && inferSbDcSig && len(sigPos) == 0 {
				sigPos = append(sigPos, 0)
				continue
			}
			ctx := d.sigCoeffCtx(xC, yC, xP, yP, sbX, sbY, log2Size, cIdx, scanIdx, codedSb, sbScan, n)
			if c.decodeBin(d.ctx, ctx) == 1 {
				sigPos = append(sigPos, pos)
			}
		}
		if len(sigPos) == 0 {
			continue
		}

		// ctxSet for greater1 (9.3.4.2.6).
		ctxSet := 0
		if sb > 0 && cIdx == 0 {
			ctxSet = 2
		}
		if prevGreater1Ctx == 0 {
			ctxSet++
		}
		greater1Ctx := 1

		numGt1 := min(8, len(sigPos))
		gt1 := make([]bool, len(sigPos))
		firstGt1 := -1
		for i := range numGt1 {
			ctx := ctxCoeffAbsGt1 + ctxSet*4 + min(3, greater1Ctx)
			if cIdx > 0 {
				ctx += 16
			}
			if c.decodeBin(d.ctx, ctx) == 1 {
				gt1[i] = true
				greater1Ctx = 0
				if firstGt1 < 0 {
					firstGt1 = i
				}
			} else if greater1Ctx > 0 && greater1Ctx < 3 {
				greater1Ctx++
			} else if greater1Ctx >= 3 {
				greater1Ctx++
			}
		}
		prevGreater1Ctx = greater1Ctx

		gt2 := false
		if firstGt1 >= 0 {
			ctx := ctxCoeffAbsGt2 + ctxSet
			if cIdx > 0 {
				ctx += 4
			}
			gt2 = c.decodeBin(d.ctx, ctx) == 1
		}

		// Sign hiding decision (7.4.9.11): first and last significant scan
		// positions within the sub-block.
		firstSigScan := sigPos[len(sigPos)-1] // smallest scan index
		lastSigScan := sigPos[0]
		signHidden := d.pps.signDataHiding && !bypass && lastSigScan-firstSigScan > 3

		numSigns := len(sigPos)
		if signHidden {
			numSigns--
		}
		signs := c.decodeBypassBits(numSigns)

		// Remaining levels.
		levels := make([]int32, len(sigPos))
		cRice := 0
		sumAbs := int64(0)
		for i := range sigPos {
			base := int32(1)
			if i < 8 && gt1[i] {
				base = 2
				if i == firstGt1 && gt2 {
					base = 3
				}
			}
			var threshold int32 = 1
			if i < 8 {
				threshold = 2
				if i == firstGt1 {
					threshold = 3
				}
			}
			level := base
			if base == threshold {
				rem, ok := d.decodeCoeffRemaining(cRice)
				if !ok {
					return false, errInvalidStream("coefficient level")
				}
				level += rem
				if int64(level) > 3*(1<<cRice) && cRice < 4 {
					cRice++
				}
			}
			levels[i] = level
			sumAbs += int64(level)
		}

		// Apply signs and store. sigPos is in decreasing scan order; the
		// sign bits were coded in the same order, MSB first.
		bit := numSigns - 1
		for i, pos := range sigPos {
			xC := (sbX << 2) + int(subScan[pos].x)
			yC := (sbY << 2) + int(subScan[pos].y)
			v := levels[i]
			if i == len(sigPos)-1 && signHidden {
				if sumAbs&1 == 1 {
					v = -v
				}
			} else {
				if signs>>uint(bit)&1 == 1 {
					v = -v
				}
				bit--
			}
			block[yC*n+xC] = v
		}
		if c.failed() {
			return false, errInvalidStream("residual data")
		}
	}
	return transformSkip, nil
}

// sbGet reads the coded flag of the sub-block at grid position (x, y).
func sbGet(coded []bool, scan []scanPos, x, y int) bool {
	for i, p := range scan {
		if int(p.x) == x && int(p.y) == y {
			return coded[i]
		}
	}
	return false
}

// sigCoeffCtx derives the sig_coeff_flag context index (9.3.4.2.5).
func (d *sliceDecoder) sigCoeffCtx(xC, yC, xP, yP, sbX, sbY, log2Size, cIdx, scanIdx int,
	codedSb []bool, sbScan []scanPos, n int) int {

	var sigCtx int
	switch {
	case log2Size == 2:
		sigCtx = sigCtxIdxMap4x4[(yP<<2)+xP]
	case xC+yC == 0:
		sigCtx = 0
	default:
		prevCsbf := 0
		if sbX+1 < n>>2 && sbGet(codedSb, sbScan, sbX+1, sbY) {
			prevCsbf |= 1
		}
		if sbY+1 < n>>2 && sbGet(codedSb, sbScan, sbX, sbY+1) {
			prevCsbf |= 2
		}
		switch prevCsbf {
		case 0:
			switch {
			case xP+yP == 0:
				sigCtx = 2
			case xP+yP < 3:
				sigCtx = 1
			default:
				sigCtx = 0
			}
		case 1:
			switch yP {
			case 0:
				sigCtx = 2
			case 1:
				sigCtx = 1
			default:
				sigCtx = 0
			}
		case 2:
			switch xP {
			case 0:
				sigCtx = 2
			case 1:
				sigCtx = 1
			default:
				sigCtx = 0
			}
		default:
			sigCtx = 2
		}
		if cIdx == 0 {
			if sbX+sbY > 0 {
				sigCtx += 3
			}
			if log2Size == 3 {
				if scanIdx == 0 {
					sigCtx += 9
				} else {
					sigCtx += 15
				}
			} else {
				sigCtx += 21
			}
		} else {
			if log2Size == 3 {
				sigCtx += 9
			} else {
				sigCtx += 12
			}
		}
	}
	if cIdx > 0 {
		sigCtx += 27
	}
	return ctxSigCoeffFlag + sigCtx
}

// decodeCoeffRemaining parses coeff_abs_level_remaining (9.3.3.13):
// truncated Rice prefix with escape to exp-Golomb.
func (d *sliceDecoder) decodeCoeffRemaining(cRice int) (int32, bool) {
	c := &d.cabac
	prefix := 0
	for prefix < 32 && c.decodeBypass() == 1 {
		prefix++
	}
	if prefix >= 32 || c.failed() {
		return 0, false
	}
	if prefix <= 3 {
		return int32(prefix)<<cRice + int32(c.decodeBypassBits(cRice)), true
	}
	suffixLen := prefix - 3 + cRice
	if suffixLen > 30 {
		return 0, false
	}
	suffix := c.decodeBypassBits(suffixLen)
	return (int32(1)<<(prefix-3)+3-1)<<cRice + int32(suffix), true
}

// errInvalidStream is a helper for malformed-bitstream errors.
func errInvalidStream(what string) error {
	return &streamError{what: what}
}

type streamError struct{ what string }

func (e *streamError) Error() string { return "hevc: invalid bitstream: malformed " + e.what }
func (e *streamError) Unwrap() error { return ErrInvalid }
