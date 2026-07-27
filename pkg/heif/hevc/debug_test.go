package hevc

// Divergence-hunting harness: decodes one fixture with a per-bin CABAC trace
// (HEVC_TRACE_BINLOG writes "kind bin range ctxName state mps" lines that can
// be diffed against a reference decoder's log), syntax-level prints via the
// debugSyntax hook, and a per-8x8-block mismatch map against the committed
// reference YUV. This is how the CABAC table typos and the MPM availability
// bug were found; keep it — it costs nothing unless HEVC_TRACE is set.
//
//	HEVC_TRACE=1 [HEVC_TRACE_FIXTURE=name.hevc] go test -run TestTraceSmallest -v ./pkg/heif/hevc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraceSmallest(t *testing.T) {
	if os.Getenv("HEVC_TRACE") == "" {
		t.Skip("set HEVC_TRACE=1 to dump the decode trace")
	}
	name := os.Getenv("HEVC_TRACE_FIXTURE")
	if name == "" {
		name = "x265-16x16-qp27-nofilt.hevc"
	}
	params, slices := loadStream(t, name)
	rp, err := resolveParamSets(params)
	if err != nil {
		t.Fatal(err)
	}
	var s0 *sps
	var p0 *pps
	for id := range rp.pps {
		s0, p0, _ = rp.lookup(id)
	}
	fmt.Printf("SPS: %dx%d ctbLog2=%d minCb=%d minTb=%d maxTb=%d maxDepthIntra=%d sao=%v strong=%v\n",
		s0.width, s0.height, s0.ctbLog2SizeY, s0.minCbLog2SizeY, s0.minTbLog2Size, s0.maxTbLog2Size,
		s0.maxTransformHierarchyDepthIntra, s0.saoEnabled, s0.strongIntraSmoothing)
	fmt.Printf("PPS: initQP=%d cuQpDelta=%v(%d) tskip=%v sdh=%v wpp=%v cbOff=%d crOff=%d\n",
		p0.initQP, p0.cuQpDeltaEnabled, p0.diffCuQpDeltaDepth, p0.transformSkipEnabled,
		p0.signDataHiding, p0.entropyCodingSync, p0.cbQpOffset, p0.crQpOffset)

	nal := slices[0]
	rbsp, removed := unescapeWithMap(nal.payload)
	hdr, err := parseSliceHeader(rbsp, nal, s0, p0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("slice: qp=%d dataOffset=%d entryPoints=%v sao=%v/%v len(rbsp)=%d\n",
		hdr.sliceQP, hdr.dataOffset, hdr.entryPointOffsets, hdr.saoLuma, hdr.saoChroma, len(rbsp))

	d := newSliceDecoder(s0, p0)
	bins := 0
	var binlog *os.File
	if path := os.Getenv("HEVC_TRACE_BINLOG"); path != "" {
		binlog, _ = os.Create(path)
		defer func() { _ = binlog.Close() }()
	}
	d.cabac.trace = func(ctxIdx int, bin uint32, state, mps uint8, rng, off uint32) {
		bins++
		if binlog != nil {
			kind := "ctx"
			switch ctxIdx {
			case -1:
				kind = "byp"
			case -2:
				kind = "trm"
			}
			_, _ = fmt.Fprintf(binlog, "%s %d %d %s %d %d\n", kind, bin, rng, ctxName(ctxIdx), state, mps)
		}
		if bins <= 5000 && os.Getenv("HEVC_TRACE_BINS") != "" {
			fmt.Printf("bin %3d: ctx %3d (%s) -> %d  [range %3d]\n", bins, ctxIdx, ctxName(ctxIdx), bin, rng)
		}
	}
	debugSyntax = func(format string, args ...any) { fmt.Printf(format+"\n", args...) }
	defer func() { debugSyntax = nil }()
	err = d.decodeSliceSegment(hdr, rbsp, removed)
	fmt.Printf("decode: err=%v totalCtxBins=%d ctus=%d\n", err, bins, d.ctusDecoded)
	if err == nil {
		if os.Getenv("HEVC_NO_DEBLOCK") == "" {
			d.deblockPicture()
		}
		if s0.saoEnabled && os.Getenv("HEVC_NO_SAO") == "" {
			d.applySAO(1)
		}
	}

	// Compare whatever was reconstructed against the reference dump, per
	// 8x8 block, to localize the first spatial divergence.
	refName := strings.TrimSuffix(name, ".hevc") + ".yuv.gz"
	if _, statErr := os.Stat(filepath.Join("..", "..", "..", "testdata", "gen", "heif", "payloads", refName)); statErr == nil {
		w, h := s0.croppedWidth(), s0.croppedHeight()
		refY, refCb, refCr := readReferenceYUV(t, refName, w, h, s0.bitDepthLuma)
		d.frame.applyConformanceWindow(s0)
		blockMap := func(label string, got []uint16, stride int, ref []uint16, pw, ph int) {
			for by := 0; by < ph; by += 8 {
				line := ""
				for bx := 0; bx < pw; bx += 8 {
					bad := 0
					for y := by; y < min(by+8, ph); y++ {
						for x := bx; x < min(bx+8, pw); x++ {
							if got[y*stride+x] != ref[y*pw+x] {
								bad++
							}
						}
					}
					line += fmt.Sprintf("%3d ", bad)
				}
				fmt.Printf("%s bad/8x8: %s\n", label, line)
			}
		}
		blockMap("Y", d.frame.Y, d.frame.YStride, refY, w, h)
		blockMap("Cb", d.frame.Cb, d.frame.CStride, refCb, (w+1)/2, (h+1)/2)
		blockMap("Cr", d.frame.Cr, d.frame.CStride, refCr, (w+1)/2, (h+1)/2)
	}
}

func ctxName(idx int) string {
	switch {
	case idx == ctxSaoMergeFlag:
		return "sao_merge"
	case idx == ctxSaoTypeIdx:
		return "sao_type"
	case idx >= ctxSplitCuFlag && idx < ctxSplitCuFlag+3:
		return "split_cu"
	case idx == ctxCuTransquantBypass:
		return "tqbypass"
	case idx == ctxPartMode:
		return "part_mode"
	case idx == ctxPrevIntraLumaPred:
		return "prev_intra"
	case idx == ctxIntraChromaPred:
		return "chroma_mode"
	case idx >= ctxSplitTransform && idx < ctxSplitTransform+3:
		return "split_tu"
	case idx >= ctxCbfLuma && idx < ctxCbfLuma+2:
		return "cbf_luma"
	case idx >= ctxCbfChroma && idx < ctxCbfChroma+4:
		return "cbf_chroma"
	case idx >= ctxCuQpDeltaAbs && idx < ctxCuQpDeltaAbs+2:
		return "qp_delta"
	case idx == ctxTransformSkipLuma || idx == ctxTransformSkipChroma:
		return "tskip"
	case idx >= ctxLastSigXPrefix && idx < ctxLastSigXPrefix+18:
		return "last_x"
	case idx >= ctxLastSigYPrefix && idx < ctxLastSigYPrefix+18:
		return "last_y"
	case idx >= ctxCodedSubBlock && idx < ctxCodedSubBlock+4:
		return "csbf"
	case idx >= ctxSigCoeffFlag && idx < ctxSigCoeffFlag+42:
		return fmt.Sprintf("sig+%d", idx-ctxSigCoeffFlag)
	case idx >= ctxCoeffAbsGt1 && idx < ctxCoeffAbsGt1+24:
		return fmt.Sprintf("gt1+%d", idx-ctxCoeffAbsGt1)
	case idx >= ctxCoeffAbsGt2 && idx < ctxCoeffAbsGt2+6:
		return "gt2"
	}
	return "?"
}
