package hevc

// Context-model layout and initialization (spec 9.3.2.2, tables 9-5..9-32).
// Only initType 0 (I slices) is carried: this decoder rejects P/B slices, so
// the other two columns of every spec table are dead weight.
//
// All contexts live in one flat slice; the ctx* constants are the base
// offsets of each syntax element's context block. Snapshotting for WPP/tile
// re-init is a plain copy of the slice.

// Context block offsets: each base is the previous base plus the previous
// block's context count, spelled out so a block-size mistake cannot silently
// shift every later element.
const (
	ctxSaoMergeFlag        = 0                          // 1 ctx
	ctxSaoTypeIdx          = ctxSaoMergeFlag + 1        // 1
	ctxSplitCuFlag         = ctxSaoTypeIdx + 1          // 3
	ctxCuTransquantBypass  = ctxSplitCuFlag + 3         // 1
	ctxPartMode            = ctxCuTransquantBypass + 1  // 1 (I slices use one context)
	ctxPrevIntraLumaPred   = ctxPartMode + 1            // 1
	ctxIntraChromaPred     = ctxPrevIntraLumaPred + 1   // 1
	ctxSplitTransform      = ctxIntraChromaPred + 1     // 3
	ctxCbfLuma             = ctxSplitTransform + 3      // 2
	ctxCbfChroma           = ctxCbfLuma + 2             // 4
	ctxCuQpDeltaAbs        = ctxCbfChroma + 4           // 2
	ctxTransformSkipLuma   = ctxCuQpDeltaAbs + 2        // 1
	ctxTransformSkipChroma = ctxTransformSkipLuma + 1   // 1
	ctxLastSigXPrefix      = ctxTransformSkipChroma + 1 // 18
	ctxLastSigYPrefix      = ctxLastSigXPrefix + 18     // 18
	ctxCodedSubBlock       = ctxLastSigYPrefix + 18     // 4
	ctxSigCoeffFlag        = ctxCodedSubBlock + 4       // 44 (42 + 2 transform-skip)
	ctxCoeffAbsGt1         = ctxSigCoeffFlag + 44       // 24
	ctxCoeffAbsGt2         = ctxCoeffAbsGt1 + 24        // 6
	numCtxModels           = ctxCoeffAbsGt2 + 6
)

// ctxInitValues holds the initValue for every context at its flat offset,
// initType 0 columns of the spec tables (cross-checked against the HM
// reference decoder's I-slice columns).
var ctxInitValues = buildCtxInitValues()

func buildCtxInitValues() [numCtxModels]uint8 {
	var v [numCtxModels]uint8
	put := func(base int, vals ...uint8) {
		copy(v[base:], vals)
	}
	put(ctxSaoMergeFlag, 153)
	put(ctxSaoTypeIdx, 200)
	put(ctxSplitCuFlag, 139, 141, 157)
	put(ctxCuTransquantBypass, 154)
	put(ctxPartMode, 184)
	put(ctxPrevIntraLumaPred, 184)
	put(ctxIntraChromaPred, 63)
	put(ctxSplitTransform, 153, 138, 138)
	put(ctxCbfLuma, 111, 141)
	put(ctxCbfChroma, 94, 138, 182, 154)
	put(ctxCuQpDeltaAbs, 154, 154)
	put(ctxTransformSkipLuma, 139)
	put(ctxTransformSkipChroma, 139)
	put(ctxLastSigXPrefix,
		110, 110, 124, 125, 140, 153, 125, 127, 140, 109, 111, 143, 127, 111, 79, 108, 123, 63)
	put(ctxLastSigYPrefix,
		110, 110, 124, 125, 140, 153, 125, 127, 140, 109, 111, 143, 127, 111, 79, 108, 123, 63)
	put(ctxCodedSubBlock, 91, 171, 134, 141)
	put(ctxSigCoeffFlag,
		111, 111, 125, 110, 110, 94, 124, 108, 124, 107, 125, 141, 179, 153, 125, 107,
		125, 141, 179, 153, 125, 107, 125, 141, 179, 153, 125, 140, 139, 182, 182, 152,
		136, 152, 136, 153, 136, 139, 111, 136, 139, 111,
		// The two transform-skip significance contexts (42, 43).
		141, 111)
	put(ctxCoeffAbsGt1,
		140, 92, 137, 138, 140, 152, 138, 139, 153, 74, 149, 92,
		139, 107, 122, 152, 140, 179, 166, 182, 140, 227, 122, 197)
	put(ctxCoeffAbsGt2, 138, 153, 136, 167, 152, 152)
	return v
}

// clip3 clamps x into [lo, hi].
func clip3(lo, hi, x int32) int32 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// initCtxModel derives one context's initial state from its initValue and
// the slice QP (spec 9.3.2.2).
func initCtxModel(initValue uint8, qp int32) ctxModel {
	slope := int32(initValue>>4)*5 - 45
	offset := int32(initValue&15)<<3 - 16
	pre := clip3(1, 126, (slope*clip3(0, 51, qp))>>4+offset)
	if pre <= 63 {
		return ctxModel{state: uint8(63 - pre), mps: 0}
	}
	return ctxModel{state: uint8(pre - 64), mps: 1}
}

// newCtxModels builds the full context array for an I slice at sliceQP.
func newCtxModels(sliceQP int32) []ctxModel {
	out := make([]ctxModel, numCtxModels)
	for i, iv := range ctxInitValues {
		out[i] = initCtxModel(iv, sliceQP)
	}
	return out
}

// snapshotCtx copies a context array (WPP row sync / tile re-init points).
func snapshotCtx(ctxs []ctxModel) []ctxModel {
	out := make([]ctxModel, len(ctxs))
	copy(out, ctxs)
	return out
}
