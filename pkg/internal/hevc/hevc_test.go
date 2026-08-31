package hevc

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// loadStream reads a committed Annex-B payload and splits it into NAL units
// grouped by type.
func loadStream(t testing.TB, name string) (params ParamSets, slices []nalUnit) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "gen", "heif", "payloads", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	for _, raw := range splitAnnexB(data) {
		n, err := parseNAL(raw)
		if err != nil {
			t.Fatalf("%s: parseNAL: %v", name, err)
		}
		switch n.typ {
		case nalVPS:
			params.VPS = append(params.VPS, raw)
		case nalSPS:
			params.SPS = append(params.SPS, raw)
		case nalPPS:
			params.PPS = append(params.PPS, raw)
		default:
			if n.isSlice() {
				slices = append(slices, n)
			}
		}
	}
	return params, slices
}

// parseFixture resolves parameter sets and the first slice header of a
// committed payload.
func parseFixture(t *testing.T, name string) (*sps, *pps, *sliceHeader) {
	t.Helper()
	params, slices := loadStream(t, name)
	rp, err := resolveParamSets(params)
	if err != nil {
		t.Fatalf("%s: resolveParamSets: %v", name, err)
	}
	if len(slices) == 0 {
		t.Fatalf("%s: no slice NALs", name)
	}
	if !slices[0].isIRAP() {
		t.Fatalf("%s: first slice NAL type %d is not IRAP", name, slices[0].typ)
	}
	// Peek the PPS id the slice references: it is the ue(v) right after
	// first_slice flag (+1 conditional bit for IRAP), so just try every
	// PPS we have — fixtures carry exactly one.
	var s0 *sps
	var p0 *pps
	for id := range rp.pps {
		s0, p0, err = rp.lookup(id)
		if err != nil {
			t.Fatalf("%s: lookup: %v", name, err)
		}
	}
	h, err := parseSliceHeader(unescapeRBSP(slices[0].payload), slices[0], s0, p0)
	if err != nil {
		t.Fatalf("%s: parseSliceHeader: %v", name, err)
	}
	return s0, p0, h
}

func TestParseX265Fixtures(t *testing.T) {
	cases := []struct {
		name          string
		width, height int // cropped output dims
		bitDepth      int
		ctbLog2       int
		sao           bool
		wpp           bool
		signHide      bool
		transformSkip bool
		tqBypass      bool
		scalingListOn bool
	}{
		// Note two x265 behaviors the expectations encode: WPP is signaled
		// only when the picture has enough CTU rows to benefit (so most
		// small fixtures carry entropy_coding_sync=0 despite being
		// encoded with default settings), and the CTU size shrinks to
		// 16 px for tiny frames.
		{"x265-64x64-qp27.hevc", 64, 64, 8, 6, true, false, true, false, false, false},
		{"x265-64x64-qp27-nofilt.hevc", 64, 64, 8, 6, false, false, true, false, false, false},
		{"x265-64x64-qp27-10bit.hevc", 64, 64, 10, 6, true, false, true, false, false, false},
		{"x265-64x64-qp27-nowpp.hevc", 64, 64, 8, 6, true, false, true, false, false, false},
		{"x265-64x64-qp27-nosdh.hevc", 64, 64, 8, 6, true, false, false, false, false, false},
		{"x265-64x64-qp27-tskip.hevc", 64, 64, 8, 6, true, false, true, true, false, false},
		{"x265-64x64-qp27-lossless.hevc", 64, 64, 8, 6, true, false, true, false, true, false},
		{"x265-64x64-qp27-scaling.hevc", 64, 64, 8, 6, true, false, true, false, false, true},
		{"x265-64x64-qp27-ctu16.hevc", 64, 64, 8, 4, true, true, true, false, false, false},
		{"x265-30x22-qp27.hevc", 30, 22, 8, 4, true, false, true, false, false, false},
		{"x265-96x80-qp12.hevc", 96, 80, 8, 6, true, false, true, false, false, false},
		{"x265-512x512-qp27.hevc", 512, 512, 8, 6, true, true, true, false, false, false},
		{"x265-16x16-qp40-nofilt.hevc", 16, 16, 8, 4, false, false, true, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, p, h := parseFixture(t, tc.name)
			if got, want := s.croppedWidth(), tc.width; got != want {
				t.Errorf("cropped width = %d, want %d", got, want)
			}
			if got, want := s.croppedHeight(), tc.height; got != want {
				t.Errorf("cropped height = %d, want %d", got, want)
			}
			if s.bitDepthLuma != tc.bitDepth {
				t.Errorf("bit depth = %d, want %d", s.bitDepthLuma, tc.bitDepth)
			}
			if s.chromaFormat != 1 {
				t.Errorf("chroma format = %d, want 1", s.chromaFormat)
			}
			if s.ctbLog2SizeY != tc.ctbLog2 {
				t.Errorf("ctbLog2SizeY = %d, want %d", s.ctbLog2SizeY, tc.ctbLog2)
			}
			if s.saoEnabled != tc.sao {
				t.Errorf("saoEnabled = %v, want %v", s.saoEnabled, tc.sao)
			}
			if p.entropyCodingSync != tc.wpp {
				t.Errorf("entropyCodingSync = %v, want %v", p.entropyCodingSync, tc.wpp)
			}
			if p.signDataHiding != tc.signHide {
				t.Errorf("signDataHiding = %v, want %v", p.signDataHiding, tc.signHide)
			}
			if p.transformSkipEnabled != tc.transformSkip {
				t.Errorf("transformSkipEnabled = %v, want %v", p.transformSkipEnabled, tc.transformSkip)
			}
			if p.transquantBypassEnabled != tc.tqBypass {
				t.Errorf("transquantBypassEnabled = %v, want %v", p.transquantBypassEnabled, tc.tqBypass)
			}
			if s.scalingListEnabled != tc.scalingListOn {
				t.Errorf("scalingListEnabled = %v, want %v", s.scalingListEnabled, tc.scalingListOn)
			}
			if !h.firstSlice {
				t.Error("first slice header not marked first_slice_segment_in_pic")
			}
			if h.sliceQP < 0 || h.sliceQP > 51 {
				t.Errorf("slice QP %d out of range", h.sliceQP)
			}
			if h.dataOffset <= 0 {
				t.Errorf("dataOffset = %d", h.dataOffset)
			}
		})
	}
}

func TestConformanceWindow30x22(t *testing.T) {
	s, _, _ := parseFixture(t, "x265-30x22-qp27.hevc")
	if s.width != 32 || s.height != 24 {
		t.Fatalf("coded size = %dx%d, want 32x24", s.width, s.height)
	}
	if s.confWin != [4]uint32{0, 1, 0, 1} {
		t.Fatalf("conformance window = %v, want [0 1 0 1]", s.confWin)
	}
}

func TestWPPEntryPoints(t *testing.T) {
	// 64x64 with 16-px CTUs has 4 CTB rows; WPP puts each row in its own
	// substream, so the first slice carries 3 entry-point offsets.
	_, p, h := parseFixture(t, "x265-64x64-qp27-ctu16.hevc")
	if !p.entropyCodingSync {
		t.Fatal("expected WPP enabled")
	}
	if len(h.entryPointOffsets) != 3 {
		t.Fatalf("entry points = %d, want 3", len(h.entryPointOffsets))
	}
	for i, off := range h.entryPointOffsets {
		if off == 0 {
			t.Errorf("entry point %d has zero size", i)
		}
	}
}

func TestScalingListFixture(t *testing.T) {
	s, _, _ := parseFixture(t, "x265-64x64-qp27-scaling.hevc")
	if !s.scalingListEnabled {
		t.Fatal("scaling lists not enabled")
	}
	// x265 --scaling-list default signals sps_scaling_list_data_present=0
	// (use default matrices) or explicit data; both are acceptable — the
	// point is that parsing consumed the syntax correctly, which the
	// overall SPS parse (alignment, later fields) already proves.
}

func TestSplitAnnexB(t *testing.T) {
	stream := []byte{
		0, 0, 0, 1, 0x40, 0x01, 0xAA, // 4-byte start code
		0, 0, 1, 0x42, 0x01, 0xBB, 0xCC, // 3-byte start code
		0, 0, 0, 1, 0x44, 0x01, 0xDD,
	}
	nals := splitAnnexB(stream)
	if len(nals) != 3 {
		t.Fatalf("got %d NALs, want 3", len(nals))
	}
	want := [][]byte{
		{0x40, 0x01, 0xAA},
		{0x42, 0x01, 0xBB, 0xCC},
		{0x44, 0x01, 0xDD},
	}
	for i := range want {
		if !bytes.Equal(nals[i], want[i]) {
			t.Errorf("NAL %d = %x, want %x", i, nals[i], want[i])
		}
	}
}

func TestSplitLengthPrefixed(t *testing.T) {
	data := []byte{0, 3, 0x40, 0x01, 0xAA, 0, 2, 0x42, 0x01}
	nals, err := splitLengthPrefixed(data, 2)
	if err != nil {
		t.Fatalf("splitLengthPrefixed: %v", err)
	}
	if len(nals) != 2 || !bytes.Equal(nals[0], []byte{0x40, 0x01, 0xAA}) || !bytes.Equal(nals[1], []byte{0x42, 0x01}) {
		t.Fatalf("got %x", nals)
	}
	if _, err := splitLengthPrefixed([]byte{0, 5, 1}, 2); !errors.Is(err, ErrInvalid) {
		t.Fatalf("overrun: want ErrInvalid, got %v", err)
	}
	if _, err := splitLengthPrefixed(data, 3); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad length size: want ErrInvalid, got %v", err)
	}
}

func TestUnescapeRBSP(t *testing.T) {
	cases := []struct{ in, want []byte }{
		{[]byte{0, 0, 3, 1}, []byte{0, 0, 1}},
		{[]byte{0, 0, 3, 0, 0, 3, 2}, []byte{0, 0, 0, 0, 2}},
		{[]byte{1, 2, 3, 4}, []byte{1, 2, 3, 4}},
		{[]byte{0, 0, 1}, []byte{0, 0, 1}}, // no escape present: unchanged
		{nil, nil},
	}
	for _, tc := range cases {
		if got := unescapeRBSP(tc.in); !bytes.Equal(got, tc.want) {
			t.Errorf("unescape(%x) = %x, want %x", tc.in, got, tc.want)
		}
	}
}

func TestBitReader(t *testing.T) {
	// 0b10110011 0b01000000: u(3)=5, flag=1, ue: 0011 0... -> zeros=2 ->
	// wait, verify with direct construction instead.
	r := newBitReader([]byte{0b10110011, 0b01000000}, "test")
	if v := r.u(3); v != 0b101 {
		t.Fatalf("u(3) = %b", v)
	}
	if !r.flag() {
		t.Fatal("flag")
	}
	// Remaining bits: 0011 0100 0000. ue: two zeros, then 1, suffix 10 ->
	// (1<<2 - 1) + 2 = 5.
	if v := r.ue(); v != 5 {
		t.Fatalf("ue = %d, want 5", v)
	}
	if err := r.err(); err != nil {
		t.Fatal(err)
	}
	// se mapping: ue k -> (-1)^(k+1) * ceil(k/2).
	se := newBitReader([]byte{0b10100110, 0b01000000}, "test")
	if v := se.se(); v != 0 { // ue=0
		t.Fatalf("se#1 = %d, want 0", v)
	}
	if v := se.se(); v != 1 { // ue=1
		t.Fatalf("se#2 = %d, want 1", v)
	}
	if v := se.se(); v != -1 { // ue=2
		t.Fatalf("se#3 = %d, want -1", v)
	}
	// Overrun is sticky.
	r2 := newBitReader([]byte{0xff}, "test")
	r2.u(8)
	if r2.u(1); r2.err() == nil {
		t.Fatal("overrun not detected")
	}
}

func TestRejectInterSlice(t *testing.T) {
	// Hand-build a degenerate non-I slice header: P slice (type 1) in a
	// TRAIL_R NAL must be rejected as unsupported, not misparsed.
	s := &sps{picSizeCtbs: 1, maxPocLsbBits: 8}
	p := &pps{initQP: 26}
	// first_slice=1, pps_id ue=0 (bit 1), slice_type ue=1 (P) -> bits 010,
	// poc_lsb 8 bits, st_rps_sps_flag... parse hits slice type first.
	r := []byte{0b11010000, 0x00, 0x80}
	nal := nalUnit{typ: nalTrailN + 1} // TRAIL_R
	_, err := parseSliceHeader(r, nal, s, p)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported for P slice, got %v", err)
	}
}

func FuzzParseSPS(f *testing.F) {
	for _, name := range []string{"x265-64x64-qp27.hevc", "x265-64x64-qp27-10bit.hevc", "x265-64x64-qp27-scaling.hevc"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "gen", "heif", "payloads", name))
		if err != nil {
			continue
		}
		for _, raw := range splitAnnexB(data) {
			if n, err := parseNAL(raw); err == nil && n.typ == nalSPS {
				f.Add(unescapeRBSP(n.payload))
			}
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseSPS(data)
	})
}

func FuzzParsePPS(f *testing.F) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "gen", "heif", "payloads", "x265-64x64-qp27.hevc"))
	if err == nil {
		for _, raw := range splitAnnexB(data) {
			if n, err := parseNAL(raw); err == nil && n.typ == nalPPS {
				f.Add(unescapeRBSP(n.payload))
			}
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parsePPS(data)
	})
}

func FuzzParseSliceHeader(f *testing.F) {
	params, slices := ParamSets{}, []nalUnit(nil)
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "gen", "heif", "payloads", "x265-64x64-qp27-ctu16.hevc"))
	if err == nil {
		for _, raw := range splitAnnexB(data) {
			n, err := parseNAL(raw)
			if err != nil {
				continue
			}
			switch n.typ {
			case nalSPS:
				params.SPS = append(params.SPS, raw)
			case nalPPS:
				params.PPS = append(params.PPS, raw)
			default:
				if n.isSlice() {
					slices = append(slices, n)
				}
			}
		}
	}
	for _, sl := range slices {
		f.Add(unescapeRBSP(sl.payload))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		s := &sps{picSizeCtbs: 16, maxPocLsbBits: 8, saoEnabled: true, numShortTermRPS: 2, stRPSDeltaPocs: []int{1, 2}, longTermPresent: true, numLongTermSPS: 1}
		p := &pps{initQP: 26, dependentSliceSegments: true, entropyCodingSync: true, sliceChromaQpOffsets: true, deblockControlPresent: true, deblockOverrideEnabled: true, sliceHeaderExtension: true, numExtraSliceHeaderBits: 1}
		_, _ = parseSliceHeader(data, nalUnit{typ: nalCra}, s, p)
	})
}
