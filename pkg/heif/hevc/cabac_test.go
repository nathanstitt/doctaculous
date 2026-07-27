package hevc

import "testing"

// The engine tests hand-execute the spec flowcharts on paper and assert every
// intermediate value, so a transcription slip in the arithmetic (not just the
// final bin) is caught at its first divergence.

func TestRangeTabSpotValues(t *testing.T) {
	// Spec Table 9-46 corners and interior rows.
	cases := []struct {
		state, q int
		want     uint32
	}{
		{0, 0, 128}, {0, 3, 240}, {1, 0, 128}, {1, 3, 227},
		{8, 1, 116}, {16, 2, 90}, {32, 3, 45}, {47, 0, 12},
		{62, 2, 8}, {63, 0, 2}, {63, 3, 2},
	}
	for _, tc := range cases {
		if got := rangeTabLPS[tc.state][tc.q]; got != tc.want {
			t.Errorf("rangeTabLPS[%d][%d] = %d, want %d", tc.state, tc.q, got, tc.want)
		}
	}
}

func TestTransIdxSpotValues(t *testing.T) {
	cases := map[int]uint8{0: 0, 1: 0, 2: 1, 5: 4, 13: 11, 28: 23, 29: 22, 62: 38, 63: 63}
	for state, want := range cases {
		if got := transIdxLPS[state]; got != want {
			t.Errorf("transIdxLPS[%d] = %d, want %d", state, got, want)
		}
	}
}

func TestCtxInitFormula(t *testing.T) {
	// Worked examples of spec 9.3.2.2 (slope/offset/clip arithmetic,
	// including the arithmetic right shift on a negative product).
	cases := []struct {
		initValue uint8
		qp        int32
		wantState uint8
		wantMPS   uint8
	}{
		{154, 26, 0, 1}, // slope 0, offset 64 -> preCtxState 64
		{154, 0, 0, 1},  // slope 0: QP-independent
		{154, 51, 0, 1},
		{63, 26, 8, 0},   // slope -30: (-780>>4)+104 = -49+104 = 55 -> 63-55
		{200, 26, 15, 1}, // slope 15, offset 48: (15*26>>4)+48 = 24+48 = 72? -> 72-64=8? recomputed below
	}
	// Recompute the last case exactly: initValue 200 = 0b11001000:
	// slope = (200>>4)*5-45 = 12*5-45 = 15; offset = (200&15)<<3-16 = 8<<3-16 = 48.
	// pre = (15*26)>>4 + 48 = 390>>4 + 48 = 24 + 48 = 72 -> state 72-64 = 8, mps 1.
	cases[4].wantState = 8
	for _, tc := range cases {
		got := initCtxModel(tc.initValue, tc.qp)
		if got.state != tc.wantState || got.mps != tc.wantMPS {
			t.Errorf("initCtxModel(%d, qp %d) = {state %d, mps %d}, want {%d, %d}",
				tc.initValue, tc.qp, got.state, got.mps, tc.wantState, tc.wantMPS)
		}
	}
}

func TestCtxInitTableFullyPopulated(t *testing.T) {
	// Every init value in the spec's I-slice columns is non-zero, so a zero
	// slot means a block offset/count mismatch left a gap (or two blocks
	// overlapped and the second overwrote short).
	for i, v := range ctxInitValues {
		if v == 0 {
			t.Errorf("ctxInitValues[%d] is zero: offset table gap", i)
		}
	}
	if numCtxModels != 136 {
		t.Errorf("numCtxModels = %d, want 136", numCtxModels)
	}
}

func newTestDecoder(t *testing.T, data []byte) *cabacDecoder {
	t.Helper()
	c := &cabacDecoder{}
	if err := c.init(newBitReader(data, "test")); err != nil {
		t.Fatalf("init: %v", err)
	}
	return c
}

func TestCABACInit(t *testing.T) {
	c := newTestDecoder(t, []byte{0x00, 0x80, 0x00})
	if c.ivlRange != 510 || c.ivlOffset != 1 {
		t.Fatalf("after init: range %d offset %d, want 510/1", c.ivlRange, c.ivlOffset)
	}
	// Offset 511 (nine 1-bits) is forbidden.
	bad := &cabacDecoder{}
	if err := bad.init(newBitReader([]byte{0xff, 0xff}, "test")); err == nil {
		t.Fatal("offset 511 accepted")
	}
	// Init off a byte boundary is a caller bug surfaced as ErrInvalid.
	r := newBitReader([]byte{0, 0}, "test")
	r.u(3)
	if err := (&cabacDecoder{}).init(r); err == nil {
		t.Fatal("unaligned init accepted")
	}
}

func TestDecodeBypassHandTrace(t *testing.T) {
	// Bytes 00 7F FF: offset starts 0, then 15 one-bits follow.
	// offset doubles+1 each bypass: 1,3,7,...,255 (all < 510, bins 0),
	// then 511 >= 510 -> bin 1, offset 1.
	c := newTestDecoder(t, []byte{0x00, 0x7f, 0xff})
	for i := range 8 {
		if bin := c.decodeBypass(); bin != 0 {
			t.Fatalf("bypass %d = 1, want 0", i)
		}
	}
	if bin := c.decodeBypass(); bin != 1 {
		t.Fatal("bypass 9 = 0, want 1")
	}
	if c.ivlOffset != 1 {
		t.Fatalf("offset after LPS-side bypass = %d, want 1", c.ivlOffset)
	}
	// All-zero payload: bypass bins stay 0 forever.
	z := newTestDecoder(t, make([]byte, 8))
	for i := range 32 {
		if z.decodeBypass() != 0 {
			t.Fatalf("zero-stream bypass %d != 0", i)
		}
	}
	// decodeBypassBits assembles MSB-first.
	b := newTestDecoder(t, []byte{0x00, 0x7f, 0xff})
	if v := b.decodeBypassBits(9); v != 1 {
		t.Fatalf("decodeBypassBits(9) = %d, want 1", v)
	}
}

func TestDecodeBinMPSHandTrace(t *testing.T) {
	// Hand-executed spec flowchart, ctx {state 0, mps 0}, stream 00 80 00:
	// init: range 510, offset 1.
	// bin 1: qIdx 3, LPS 240 -> range 270; 1 < 270: MPS bin 0, state 1; no renorm.
	// bin 2: qIdx 0, LPS 128 -> range 142; MPS bin 0, state 2; renorm to
	//        range 284, offset 2 (next stream bit is 0).
	// bin 3: qIdx 0, LPS 128 -> range 156; MPS bin 0, state 3; renorm to
	//        range 312, offset 4.
	c := newTestDecoder(t, []byte{0x00, 0x80, 0x00})
	ctxs := []ctxModel{{state: 0, mps: 0}}
	type step struct {
		bin, rng, off uint32
		state         uint8
	}
	want := []step{{0, 270, 1, 1}, {0, 284, 2, 2}, {0, 312, 4, 3}}
	for i, w := range want {
		bin := c.decodeBin(ctxs, 0)
		if bin != w.bin || c.ivlRange != w.rng || c.ivlOffset != w.off || ctxs[0].state != w.state {
			t.Fatalf("bin %d: got bin=%d range=%d offset=%d state=%d, want %+v",
				i+1, bin, c.ivlRange, c.ivlOffset, ctxs[0].state, w)
		}
	}
}

func TestDecodeBinLPSHandTrace(t *testing.T) {
	// Stream FD 00: init offset 506. ctx {state 0, mps 0}:
	// qIdx 3, LPS 240 -> range 270; 506 >= 270: LPS bin 1, offset 236,
	// range 240; state 0 flips MPS to 1, transIdxLPS[0] = 0; renorm to
	// range 480, offset 472.
	c := newTestDecoder(t, []byte{0xfd, 0x00})
	ctxs := []ctxModel{{state: 0, mps: 0}}
	if bin := c.decodeBin(ctxs, 0); bin != 1 {
		t.Fatal("LPS bin != 1")
	}
	if ctxs[0].mps != 1 || ctxs[0].state != 0 {
		t.Fatalf("ctx after state-0 LPS = %+v, want mps 1 state 0", ctxs[0])
	}
	if c.ivlRange != 480 || c.ivlOffset != 472 {
		t.Fatalf("range/offset = %d/%d, want 480/472", c.ivlRange, c.ivlOffset)
	}
}

func TestDecodeTerminate(t *testing.T) {
	// Offset 0: 0 < 508 -> not terminated, range 508 needs no renorm.
	c := newTestDecoder(t, []byte{0x00, 0x00})
	if c.decodeTerminate() != 0 {
		t.Fatal("terminate = 1 at offset 0")
	}
	if c.ivlRange != 508 {
		t.Fatalf("range = %d, want 508", c.ivlRange)
	}
	// Offset 508 (FE 00): 508 >= 508 -> terminated.
	e := newTestDecoder(t, []byte{0xfe, 0x00})
	if e.decodeTerminate() != 1 {
		t.Fatal("terminate = 0 at offset 508")
	}
}

func TestSnapshotCtxIsDeepCopy(t *testing.T) {
	orig := newCtxModels(27)
	snap := snapshotCtx(orig)
	snap[ctxSplitCuFlag].state = 61
	snap[ctxSplitCuFlag].mps = 1 - snap[ctxSplitCuFlag].mps
	if orig[ctxSplitCuFlag] == snap[ctxSplitCuFlag] {
		t.Fatal("snapshot shares state with original")
	}
}

func TestTraceHook(t *testing.T) {
	c := newTestDecoder(t, []byte{0x00, 0x80, 0x00})
	var got []int
	c.trace = func(ctxIdx int, bin uint32, state, mps uint8, rng, off uint32) {
		got = append(got, ctxIdx)
	}
	ctxs := newCtxModels(27)
	c.decodeBin(ctxs, ctxSplitCuFlag+2)
	c.decodeBin(ctxs, ctxSaoMergeFlag)
	if len(got) != 2 || got[0] != ctxSplitCuFlag+2 || got[1] != ctxSaoMergeFlag {
		t.Fatalf("trace calls = %v", got)
	}
}

func TestCABACExhaustsInput(t *testing.T) {
	// Draining bypass bins past the buffer must trip the sticky reader
	// error, never loop or panic.
	c := newTestDecoder(t, []byte{0x00, 0x00})
	for range 100 {
		c.decodeBypass()
	}
	if !c.failed() {
		t.Fatal("reader exhaustion not reported")
	}
}
